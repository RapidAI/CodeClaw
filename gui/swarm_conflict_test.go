package main

import (
	"testing"

	"pgregory.net/rapid"
)

// ---------------------------------------------------------------------------
// Unit tests
// ---------------------------------------------------------------------------

func TestDetectConflicts_NoConflict(t *testing.T) {
	cd := NewConflictDetector(nil)
	tasks := []SubTask{
		{Index: 0, Description: "task0", ExpectedFiles: []string{"a.go"}},
		{Index: 1, Description: "task1", ExpectedFiles: []string{"b.go"}},
	}
	groups, err := cd.DetectConflicts(tasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(groups))
	}
}

func TestDetectConflicts_AllConflict(t *testing.T) {
	cd := NewConflictDetector(nil)
	tasks := []SubTask{
		{Index: 0, Description: "task0", ExpectedFiles: []string{"shared.go"}},
		{Index: 1, Description: "task1", ExpectedFiles: []string{"shared.go"}},
		{Index: 2, Description: "task2", ExpectedFiles: []string{"shared.go"}},
	}
	groups, err := cd.DetectConflicts(tasks)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 1 {
		t.Fatalf("expected 1 group, got %d", len(groups))
	}
	if len(groups[0].TaskIndices) != 3 {
		t.Fatalf("expected 3 tasks in group, got %d", len(groups[0].TaskIndices))
	}
}

func TestDetectConflicts_Empty(t *testing.T) {
	cd := NewConflictDetector(nil)
	groups, err := cd.DetectConflicts(nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(groups) != 0 {
		t.Fatalf("expected 0 groups, got %d", len(groups))
	}
}

// ---------------------------------------------------------------------------
// Property tests
// ---------------------------------------------------------------------------

// genSubTasks generates a random list of SubTasks with random file assignments.
func genSubTasks(t *rapid.T) []SubTask {
	n := rapid.IntRange(1, 10).Draw(t, "taskCount")
	filePool := []string{"a.go", "b.go", "c.go", "d.go", "e.go", "f.go", "g.go", "h.go"}
	tasks := make([]SubTask, n)
	for i := 0; i < n; i++ {
		numFiles := rapid.IntRange(1, 4).Draw(t, "numFiles")
		files := make([]string, numFiles)
		for j := 0; j < numFiles; j++ {
			idx := rapid.IntRange(0, len(filePool)-1).Draw(t, "fileIdx")
			files[j] = filePool[idx]
		}
		// Deduplicate
		seen := map[string]bool{}
		deduped := files[:0]
		for _, f := range files {
			if !seen[f] {
				seen[f] = true
				deduped = append(deduped, f)
			}
		}
		tasks[i] = SubTask{
			Index:         i,
			Description:   "task",
			ExpectedFiles: deduped,
		}
	}
	return tasks
}

// Feature: swarm-orchestrator, Property 8: 冲突分组正确性
// For any two SubTasks, if their ExpectedFiles have an intersection, they
// must be in the same TaskGroup.
func TestProperty_ConflictGroupCorrectness(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		tasks := genSubTasks(t)
		cd := NewConflictDetector(nil)
		groups, err := cd.DetectConflicts(tasks)
		if err != nil {
			t.Fatal(err)
		}

		// Build task→group mapping
		taskGroup := make(map[int]int)
		for _, g := range groups {
			for _, idx := range g.TaskIndices {
				taskGroup[idx] = g.ID
			}
		}

		// Check: tasks sharing files must be in the same group
		for i := 0; i < len(tasks); i++ {
			for j := i + 1; j < len(tasks); j++ {
				if filesOverlap(tasks[i].ExpectedFiles, tasks[j].ExpectedFiles) {
					if taskGroup[tasks[i].Index] != taskGroup[tasks[j].Index] {
						t.Fatalf("tasks %d and %d share files but are in different groups", tasks[i].Index, tasks[j].Index)
					}
				}
			}
		}
	})
}

// Feature: swarm-orchestrator, Property 9: 组间无文件冲突
// For any two different TaskGroups, the union of ExpectedFiles of their tasks
// must not overlap.
func TestProperty_NoInterGroupFileConflict(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		tasks := genSubTasks(t)
		cd := NewConflictDetector(nil)
		groups, err := cd.DetectConflicts(tasks)
		if err != nil {
			t.Fatal(err)
		}

		// Build group → files mapping
		taskMap := make(map[int]SubTask)
		for _, task := range tasks {
			taskMap[task.Index] = task
		}

		for i := 0; i < len(groups); i++ {
			filesI := groupFiles(groups[i], taskMap)
			for j := i + 1; j < len(groups); j++ {
				filesJ := groupFiles(groups[j], taskMap)
				if filesOverlap(filesI, filesJ) {
					t.Fatalf("groups %d and %d have overlapping files", groups[i].ID, groups[j].ID)
				}
			}
		}
	})
}

func filesOverlap(a, b []string) bool {
	set := make(map[string]bool, len(a))
	for _, f := range a {
		set[f] = true
	}
	for _, f := range b {
		if set[f] {
			return true
		}
	}
	return false
}

func groupFiles(g TaskGroup, taskMap map[int]SubTask) []string {
	seen := map[string]bool{}
	var result []string
	for _, idx := range g.TaskIndices {
		for _, f := range taskMap[idx].ExpectedFiles {
			if !seen[f] {
				seen[f] = true
				result = append(result, f)
			}
		}
	}
	return result
}
