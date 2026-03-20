package swarm

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// WorktreeManager manages git worktree lifecycle for swarm runs.
type WorktreeManager struct {
	baseDir string
}

// NewWorktreeManager creates a WorktreeManager.
func NewWorktreeManager() *WorktreeManager {
	return &WorktreeManager{}
}

// PrepareProject ensures the project directory has a git repository with at
// least one commit. If there are uncommitted changes, they are stashed.
func (w *WorktreeManager) PrepareProject(projectPath string) (*ProjectState, error) {
	state := &ProjectState{}
	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, fmt.Errorf("resolve project path: %w", err)
	}

	gitDir := filepath.Join(absPath, ".git")
	if info, err := os.Stat(gitDir); err == nil && (info.IsDir() || info.Mode().IsRegular()) {
		state.HadGitRepo = true
	}

	if !state.HadGitRepo {
		if err := swarmRunGit(absPath, "init"); err != nil {
			return nil, fmt.Errorf("git init: %w", err)
		}
	}

	hasCommits := swarmGitHasCommits(absPath)
	state.HadCommits = hasCommits

	if !hasCommits {
		if err := swarmRunGit(absPath, "add", "-A"); err != nil {
			return nil, fmt.Errorf("git add: %w", err)
		}
		if err := swarmRunGit(absPath, "commit", "--allow-empty", "-m", "maclaw: initial commit for swarm"); err != nil {
			return nil, fmt.Errorf("git initial commit: %w", err)
		}
	}

	branch, err := swarmGitCurrentBranch(absPath)
	if err != nil {
		return nil, fmt.Errorf("detect current branch: %w", err)
	}
	state.OriginalBranch = branch

	if swarmGitHasUncommittedChanges(absPath) {
		if err := swarmRunGit(absPath, "stash", "push", "-m", "maclaw-swarm: auto stash before swarm run"); err != nil {
			return nil, fmt.Errorf("git stash: %w", err)
		}
		state.StashCreated = true
	}

	return state, nil
}

// CreateWorktree creates a new git worktree and branch for a swarm agent.
func (w *WorktreeManager) CreateWorktree(projectPath, runID, branchName string) (*WorktreeInfo, error) {
	absPath, err := filepath.Abs(projectPath)
	if err != nil {
		return nil, fmt.Errorf("resolve project path: %w", err)
	}

	parentDir := filepath.Dir(absPath)
	worktreeBase := filepath.Join(parentDir, ".maclaw-workers", runID)

	parts := strings.Split(branchName, "/")
	dirName := parts[len(parts)-1]
	worktreePath := filepath.Join(worktreeBase, dirName)

	if err := os.MkdirAll(worktreeBase, 0o755); err != nil {
		return nil, fmt.Errorf("create worktree base dir: %w", err)
	}

	if err := swarmRunGit(absPath, "worktree", "add", "-b", branchName, worktreePath); err != nil {
		return nil, fmt.Errorf("git worktree add: %w", err)
	}

	return &WorktreeInfo{
		Path:       worktreePath,
		BranchName: branchName,
		RunID:      runID,
	}, nil
}

// RemoveWorktree removes a single worktree.
func (w *WorktreeManager) RemoveWorktree(projectPath, worktreePath string) error {
	absProject, err := filepath.Abs(projectPath)
	if err != nil {
		return fmt.Errorf("resolve project path: %w", err)
	}
	if err := swarmRunGit(absProject, "worktree", "remove", "--force", worktreePath); err != nil {
		_ = os.RemoveAll(worktreePath)
		_ = swarmRunGit(absProject, "worktree", "prune")
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
	_ = swarmRunGit(absProject, "branch", "-D", branchName)
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
	normRunDir := filepath.ToSlash(runDir)

	worktrees := swarmListGitWorktrees(absPath)
	for _, wt := range worktrees {
		if strings.HasPrefix(filepath.ToSlash(wt), normRunDir) {
			_ = swarmRunGit(absPath, "worktree", "remove", "--force", wt)
		}
	}
	_ = swarmRunGit(absPath, "worktree", "prune")

	branches := swarmListGitBranches(absPath)
	prefix := "swarm/" + runID + "/"
	for _, b := range branches {
		if strings.HasPrefix(b, prefix) {
			_ = swarmRunGit(absPath, "branch", "-D", b)
		}
	}

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
	if err := swarmRunGit(absPath, "stash", "pop"); err != nil {
		return fmt.Errorf("git stash pop: %w", err)
	}
	return nil
}

// --- git helper functions (local to swarm package) ---

func swarmRunGit(dir string, args ...string) error {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	_, err := cmd.CombinedOutput()
	return err
}

func swarmRunGitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return strings.TrimSpace(string(out)), err
}

func swarmGitHasCommits(dir string) bool {
	return swarmRunGit(dir, "rev-parse", "HEAD") == nil
}

func swarmGitCurrentBranch(dir string) (string, error) {
	return swarmRunGitOutput(dir, "rev-parse", "--abbrev-ref", "HEAD")
}

func swarmGitHasUncommittedChanges(dir string) bool {
	out, err := swarmRunGitOutput(dir, "status", "--porcelain")
	if err != nil {
		return false
	}
	return len(strings.TrimSpace(out)) > 0
}

func swarmListGitWorktrees(dir string) []string {
	out, err := swarmRunGitOutput(dir, "worktree", "list", "--porcelain")
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

func swarmListGitBranches(dir string) []string {
	out, err := swarmRunGitOutput(dir, "branch", "--format=%(refname)")
	if err != nil {
		return nil
	}
	var branches []string
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			line = strings.TrimPrefix(line, "refs/heads/")
			branches = append(branches, line)
		}
	}
	return branches
}
