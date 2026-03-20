package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// initBareGitRepo creates a temp dir with a git repo and one commit.
func initBareGitRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	mustRunGit(t, dir, "init")
	mustRunGit(t, dir, "config", "user.email", "test@test.com")
	mustRunGit(t, dir, "config", "user.name", "Test")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("# test"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, dir, "add", "-A")
	mustRunGit(t, dir, "commit", "-m", "init")
	return dir
}

func mustRunGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func TestPrepareProject_ExistingRepoWithCommits(t *testing.T) {
	dir := initBareGitRepo(t)
	wm := NewWorktreeManager()

	state, err := wm.PrepareProject(dir)
	if err != nil {
		t.Fatalf("PrepareProject: %v", err)
	}
	if !state.HadGitRepo {
		t.Error("expected HadGitRepo=true")
	}
	if !state.HadCommits {
		t.Error("expected HadCommits=true")
	}
	if state.StashCreated {
		t.Error("expected StashCreated=false (no uncommitted changes)")
	}
}

func TestPrepareProject_ExistingRepoWithUncommittedChanges(t *testing.T) {
	dir := initBareGitRepo(t)

	// Create an uncommitted file
	if err := os.WriteFile(filepath.Join(dir, "dirty.txt"), []byte("dirty"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, dir, "add", "dirty.txt")

	wm := NewWorktreeManager()
	state, err := wm.PrepareProject(dir)
	if err != nil {
		t.Fatalf("PrepareProject: %v", err)
	}
	if !state.StashCreated {
		t.Error("expected StashCreated=true")
	}
	if state.OriginalBranch == "" {
		t.Error("expected non-empty OriginalBranch")
	}
}

func TestPrepareProject_NoGitRepo(t *testing.T) {
	dir := t.TempDir()
	// Write a file so the initial commit has content
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hello"), 0o644); err != nil {
		t.Fatal(err)
	}

	wm := NewWorktreeManager()
	state, err := wm.PrepareProject(dir)
	if err != nil {
		t.Fatalf("PrepareProject: %v", err)
	}
	if state.HadGitRepo {
		t.Error("expected HadGitRepo=false")
	}
	if state.HadCommits {
		t.Error("expected HadCommits=false")
	}
	// After PrepareProject, the repo should have at least one commit
	if !gitHasCommits(dir) {
		t.Error("expected repo to have commits after PrepareProject")
	}
}

func TestPrepareProject_RepoWithNoCommits(t *testing.T) {
	dir := t.TempDir()
	mustRunGit(t, dir, "init")
	mustRunGit(t, dir, "config", "user.email", "test@test.com")
	mustRunGit(t, dir, "config", "user.name", "Test")

	wm := NewWorktreeManager()
	state, err := wm.PrepareProject(dir)
	if err != nil {
		t.Fatalf("PrepareProject: %v", err)
	}
	if !state.HadGitRepo {
		t.Error("expected HadGitRepo=true")
	}
	if state.HadCommits {
		t.Error("expected HadCommits=false")
	}
	if !gitHasCommits(dir) {
		t.Error("expected repo to have commits after PrepareProject")
	}
}

func TestCreateWorktree(t *testing.T) {
	dir := initBareGitRepo(t)
	wm := NewWorktreeManager()

	runID := "test-run-001"
	branchName := "swarm/" + runID + "/developer-0"

	info, err := wm.CreateWorktree(dir, runID, branchName)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	// Verify worktree path is under .maclaw-workers/{runID}/
	parentDir := filepath.Dir(dir)
	expectedBase := filepath.Join(parentDir, ".maclaw-workers", runID)
	if !strings.HasPrefix(info.Path, expectedBase) {
		t.Errorf("worktree path %q does not start with %q", info.Path, expectedBase)
	}

	// Verify the worktree directory exists
	if _, err := os.Stat(info.Path); os.IsNotExist(err) {
		t.Error("worktree directory does not exist")
	}

	// Verify branch name
	if info.BranchName != branchName {
		t.Errorf("expected branch %q, got %q", branchName, info.BranchName)
	}

	// Verify RunID
	if info.RunID != runID {
		t.Errorf("expected RunID %q, got %q", runID, info.RunID)
	}

	// Verify the branch exists in git
	branches := listGitBranches(dir)
	found := false
	for _, b := range branches {
		if b == branchName {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("branch %q not found in git branches: %v", branchName, branches)
	}
}

func TestRemoveWorktreeAndBranch(t *testing.T) {
	dir := initBareGitRepo(t)
	wm := NewWorktreeManager()

	runID := "test-run-002"
	branchName := "swarm/" + runID + "/developer-1"

	info, err := wm.CreateWorktree(dir, runID, branchName)
	if err != nil {
		t.Fatalf("CreateWorktree: %v", err)
	}

	// Remove the worktree and branch
	if err := wm.RemoveWorktreeAndBranch(dir, info.Path, branchName); err != nil {
		t.Fatalf("RemoveWorktreeAndBranch: %v", err)
	}

	// Verify worktree directory is gone
	if _, err := os.Stat(info.Path); !os.IsNotExist(err) {
		t.Error("worktree directory should not exist after removal")
	}

	// Verify branch is gone
	branches := listGitBranches(dir)
	for _, b := range branches {
		if b == branchName {
			t.Errorf("branch %q should not exist after removal", branchName)
		}
	}
}

func TestCleanupRun(t *testing.T) {
	dir := initBareGitRepo(t)
	wm := NewWorktreeManager()

	runID := "test-run-003"

	// Create multiple worktrees
	branches := []string{
		"swarm/" + runID + "/developer-0",
		"swarm/" + runID + "/developer-1",
	}
	var infos []*WorktreeInfo
	for _, b := range branches {
		info, err := wm.CreateWorktree(dir, runID, b)
		if err != nil {
			t.Fatalf("CreateWorktree(%s): %v", b, err)
		}
		infos = append(infos, info)
	}

	// Cleanup the entire run
	if err := wm.CleanupRun(dir, runID); err != nil {
		t.Fatalf("CleanupRun: %v", err)
	}

	// Verify all worktree directories are gone
	for _, info := range infos {
		if _, err := os.Stat(info.Path); !os.IsNotExist(err) {
			t.Errorf("worktree %q should not exist after cleanup", info.Path)
		}
	}

	// Verify all branches are gone
	gitBranches := listGitBranches(dir)
	prefix := "swarm/" + runID + "/"
	for _, b := range gitBranches {
		if strings.HasPrefix(b, prefix) {
			t.Errorf("branch %q should not exist after cleanup", b)
		}
	}

	// Verify the run directory is gone
	parentDir := filepath.Dir(dir)
	runDir := filepath.Join(parentDir, ".maclaw-workers", runID)
	if _, err := os.Stat(runDir); !os.IsNotExist(err) {
		t.Error("run directory should not exist after cleanup")
	}
}

func TestRestoreProject_WithStash(t *testing.T) {
	dir := initBareGitRepo(t)

	// Create a dirty file and stage it
	dirtyFile := filepath.Join(dir, "dirty.txt")
	if err := os.WriteFile(dirtyFile, []byte("dirty content"), 0o644); err != nil {
		t.Fatal(err)
	}
	mustRunGit(t, dir, "add", "dirty.txt")

	wm := NewWorktreeManager()
	state, err := wm.PrepareProject(dir)
	if err != nil {
		t.Fatalf("PrepareProject: %v", err)
	}
	if !state.StashCreated {
		t.Fatal("expected stash to be created")
	}

	// After stash, the dirty file should not be in the working tree
	if gitHasUncommittedChanges(dir) {
		t.Error("expected clean working tree after stash")
	}

	// Restore
	if err := wm.RestoreProject(dir, state); err != nil {
		t.Fatalf("RestoreProject: %v", err)
	}

	// The dirty file should be back
	content, err := os.ReadFile(dirtyFile)
	if err != nil {
		t.Fatalf("read dirty file: %v", err)
	}
	if string(content) != "dirty content" {
		t.Errorf("expected 'dirty content', got %q", string(content))
	}
}

func TestRestoreProject_NoStash(t *testing.T) {
	dir := initBareGitRepo(t)
	wm := NewWorktreeManager()

	state := &ProjectState{StashCreated: false}
	// Should be a no-op
	if err := wm.RestoreProject(dir, state); err != nil {
		t.Fatalf("RestoreProject: %v", err)
	}
}

func TestRestoreProject_NilState(t *testing.T) {
	dir := initBareGitRepo(t)
	wm := NewWorktreeManager()

	// Should be a no-op
	if err := wm.RestoreProject(dir, nil); err != nil {
		t.Fatalf("RestoreProject: %v", err)
	}
}

func TestCreateMultipleWorktrees_SameRun(t *testing.T) {
	dir := initBareGitRepo(t)
	wm := NewWorktreeManager()

	runID := "test-run-multi"
	branches := []string{
		"swarm/" + runID + "/developer-0",
		"swarm/" + runID + "/developer-1",
		"swarm/" + runID + "/developer-2",
	}

	for _, b := range branches {
		info, err := wm.CreateWorktree(dir, runID, b)
		if err != nil {
			t.Fatalf("CreateWorktree(%s): %v", b, err)
		}
		if _, err := os.Stat(info.Path); os.IsNotExist(err) {
			t.Errorf("worktree %q should exist", info.Path)
		}
	}

	// All branches should exist
	gitBranches := listGitBranches(dir)
	for _, expected := range branches {
		found := false
		for _, b := range gitBranches {
			if b == expected {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("branch %q not found", expected)
		}
	}
}
