package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WorktreeManager manages git worktree lifecycle: stash → init → worktree creation → cleanup → stash pop.
type WorktreeManager struct {
	baseDir string // parent-level .maclaw-workers/ directory
}

// NewWorktreeManager creates a WorktreeManager. baseDir is typically
// resolved at runtime from the project's parent directory.
func NewWorktreeManager() *WorktreeManager {
	return &WorktreeManager{}
}

// PrepareProject ensures the project directory has a git repository with at
// least one commit. If there are uncommitted changes, they are stashed.
// Returns a ProjectState that can later be passed to RestoreProject.
func (w *WorktreeManager) PrepareProject(projectPath string) (*ProjectState, error) {
	state := &ProjectState{}

	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, fmt.Errorf("resolve project path: %w", err)
	}

	// 1. Detect existing git repo
	gitDir := filepath.Join(absPath, ".git")
	if info, err := os.Stat(gitDir); err == nil && (info.IsDir() || info.Mode().IsRegular()) {
		state.HadGitRepo = true
	}

	// 2. If no git repo, initialise one
	if !state.HadGitRepo {
		if err := runGit(absPath, "init"); err != nil {
			return nil, fmt.Errorf("git init: %w", err)
		}
	}

	// 3. Check if there are any commits
	hasCommits := gitHasCommits(absPath)
	state.HadCommits = hasCommits

	// 4. If no commits, create an initial commit with all current files
	if !hasCommits {
		if err := runGit(absPath, "add", "-A"); err != nil {
			return nil, fmt.Errorf("git add: %w", err)
		}
		if err := runGit(absPath, "commit", "--allow-empty", "-m", "maclaw: initial commit for swarm"); err != nil {
			return nil, fmt.Errorf("git initial commit: %w", err)
		}
	}

	// 5. Record the current branch name
	branch, err := gitCurrentBranch(absPath)
	if err != nil {
		return nil, fmt.Errorf("detect current branch: %w", err)
	}
	state.OriginalBranch = branch

	// 6. Stash uncommitted changes (if any)
	if gitHasUncommittedChanges(absPath) {
		if err := runGit(absPath, "stash", "push", "-m", "maclaw-swarm: auto stash before swarm run"); err != nil {
			return nil, fmt.Errorf("git stash: %w", err)
		}
		state.StashCreated = true
	}

	return state, nil
}

// CreateWorktree creates a new git worktree and branch for a swarm agent.
// The worktree is placed under {project_parent}/.maclaw-workers/{runID}/.
// branchName should follow the format swarm/{runID}/{role}-{taskIndex}.
func (w *WorktreeManager) CreateWorktree(projectPath, runID, branchName string) (*WorktreeInfo, error) {
	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, fmt.Errorf("resolve project path: %w", err)
	}

	parentDir := filepath.Dir(absPath)
	worktreeBase := filepath.Join(parentDir, ".maclaw-workers", runID)

	// Use the last segment of branchName as the directory name
	// e.g. swarm/run123/developer-0 → developer-0
	parts := strings.Split(branchName, "/")
	dirName := parts[len(parts)-1]
	worktreePath := filepath.Join(worktreeBase, dirName)

	// Ensure the parent directory exists
	if err := os.MkdirAll(worktreeBase, 0o755); err != nil {
		return nil, fmt.Errorf("create worktree base dir: %w", err)
	}

	// Create the worktree with a new branch
	if err := runGit(absPath, "worktree", "add", "-b", branchName, worktreePath); err != nil {
		return nil, fmt.Errorf("git worktree add: %w", err)
	}

	return &WorktreeInfo{
		Path:       worktreePath,
		BranchName: branchName,
		RunID:      runID,
	}, nil
}

// RemoveWorktree removes a single worktree and its associated branch.
func (w *WorktreeManager) RemoveWorktree(projectPath, worktreePath string) error {
	absProject, err := filepath.Abs(projectPath)
	if err != nil {
		return fmt.Errorf("resolve project path: %w", err)
	}

	// Remove the worktree (force to handle dirty trees)
	if err := runGit(absProject, "worktree", "remove", "--force", worktreePath); err != nil {
		// If worktree remove fails, try manual cleanup
		_ = os.RemoveAll(worktreePath)
		_ = runGit(absProject, "worktree", "prune")
	}

	return nil
}

// RemoveWorktreeAndBranch removes a worktree and deletes its branch.
func (w *WorktreeManager) RemoveWorktreeAndBranch(projectPath, worktreePath, branchName string) error {
	if err := w.RemoveWorktree(projectPath, worktreePath); err != nil {
		return err
	}

	absProject, err := filepath.Abs(projectPath)
	if err != nil {
		return fmt.Errorf("resolve project path: %w", err)
	}

	// Delete the branch (force in case it's not fully merged)
	_ = runGit(absProject, "branch", "-D", branchName)

	return nil
}

// CleanupRun removes all worktrees for a given run and cleans up the run directory.
func (w *WorktreeManager) CleanupRun(projectPath, runID string) error {
	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		return fmt.Errorf("resolve project path: %w", err)
	}

	parentDir := filepath.Dir(absPath)
	runDir := filepath.Join(parentDir, ".maclaw-workers", runID)

	// List all worktrees and remove those belonging to this run.
	// Normalise paths to forward slashes so the prefix check works on Windows
	// where git may return forward-slash paths while filepath.Join uses backslashes.
	normRunDir := filepath.ToSlash(runDir)
	worktrees := listGitWorktrees(absPath)
	for _, wt := range worktrees {
		if strings.HasPrefix(filepath.ToSlash(wt), normRunDir) {
			_ = runGit(absPath, "worktree", "remove", "--force", wt)
		}
	}

	// Prune stale worktree references
	_ = runGit(absPath, "worktree", "prune")

	// Delete branches matching swarm/{runID}/*
	branches := listGitBranches(absPath)
	prefix := "swarm/" + runID + "/"
	for _, b := range branches {
		if strings.HasPrefix(b, prefix) {
			_ = runGit(absPath, "branch", "-D", b)
		}
	}

	// Remove the run directory from disk
	if err := os.RemoveAll(runDir); err != nil {
		return fmt.Errorf("remove run directory: %w", err)
	}

	return nil
}

// RestoreProject restores the project to its pre-swarm state by popping the stash.
func (w *WorktreeManager) RestoreProject(projectPath string, state *ProjectState) error {
	if state == nil || !state.StashCreated {
		return nil
	}

	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		return fmt.Errorf("resolve project path: %w", err)
	}

	if err := runGit(absPath, "stash", "pop"); err != nil {
		return fmt.Errorf("git stash pop: %w", err)
	}

	return nil
}

// ---------------------------------------------------------------------------
// Swarm-specific git helper functions (local operations only, no remote)
// Uses the existing runGit / runGitOutput from remote_workspace.go.
// ---------------------------------------------------------------------------

// gitHasCommits returns true if the repo has at least one commit.
func gitHasCommits(dir string) bool {
	err := runGit(dir, "rev-parse", "HEAD")
	return err == nil
}

// gitCurrentBranch returns the current branch name.
func gitCurrentBranch(dir string) (string, error) {
	branch, err := runGitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD")
	if err != nil {
		return "", err
	}
	return branch, nil
}

// gitHasUncommittedChanges returns true if the working tree has uncommitted changes.
func gitHasUncommittedChanges(dir string) bool {
	out, err := runGitOutput(dir, "status", "--porcelain")
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(out)) > 0
}

// listGitWorktrees returns the paths of all worktrees.
func listGitWorktrees(dir string) []string {
	out, err := runGitOutput(dir, "worktree", "list", "--porcelain")
	if err != nil {
		return nil
	}
	var paths []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "worktree ") {
			paths = append(paths, strings.TrimPrefix(line, "worktree "))
		}
	}
	return paths
}

// listGitBranches returns all local branch names.
func listGitBranches(dir string) []string {
	out, err := runGitOutput(dir, "branch", "--format=%(refname)")
	if err != nil {
		return nil
	}
	var branches []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			// Strip refs/heads/ prefix to get the branch name
			line = strings.TrimPrefix(line, "refs/heads/")
			branches = append(branches, line)
		}
	}
	return branches
}
