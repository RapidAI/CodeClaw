package main

// ConflictDetector analyses file dependencies across tasks and groups tasks
// that share files into TaskGroups. Tasks within the same group must execute
// serially; different groups can run in parallel.
type ConflictDetector struct {
	scanner *ProjectScanner
}

// NewConflictDetector creates a ConflictDetector, optionally backed by a
// ProjectScanner for import-level dependency analysis.
func NewConflictDetector(scanner *ProjectScanner) *ConflictDetector {
	return &ConflictDetector{scanner: scanner}
}

// DependencyGraph is an adjacency list mapping file paths to the set of task
// indices that touch them.
type DependencyGraph struct {
	FileToTasks map[string][]int
}

// BuildDependencyGraph constructs a mapping from file path to the task indices
// that list that file in ExpectedFiles.
func (d *ConflictDetector) BuildDependencyGraph(tasks []SubTask) *DependencyGraph {
	g := &DependencyGraph{FileToTasks: make(map[string][]int)}
	for _, t := range tasks {
		for _, f := range t.ExpectedFiles {
			g.FileToTasks[f] = append(g.FileToTasks[f], t.Index)
		}
	}
	return g
}

// DetectConflicts analyses the task list and returns TaskGroups. Two tasks
// that share any expected file (directly or transitively) end up in the same
// group.
func (d *ConflictDetector) DetectConflicts(tasks []SubTask) ([]TaskGroup, error) {
	if len(tasks) == 0 {
		return nil, nil
	}

	// Union-Find to group task indices that share files.
	parent := make(map[int]int)
	for _, t := range tasks {
		parent[t.Index] = t.Index
	}

	var find func(int) int
	find = func(x int) int {
		if parent[x] != x {
			parent[x] = find(parent[x])
		}
		return parent[x]
	}
	union := func(a, b int) {
		ra, rb := find(a), find(b)
		if ra != rb {
			parent[ra] = rb
		}
	}

	// Build file → tasks mapping and union tasks sharing files.
	graph := d.BuildDependencyGraph(tasks)
	for _, indices := range graph.FileToTasks {
		for i := 1; i < len(indices); i++ {
			union(indices[0], indices[i])
		}
	}

	// Collect groups by root.
	groups := make(map[int]*TaskGroup)
	for _, t := range tasks {
		root := find(t.Index)
		if _, ok := groups[root]; !ok {
			groups[root] = &TaskGroup{ID: root}
		}
		groups[root].TaskIndices = append(groups[root].TaskIndices, t.Index)
	}

	// Attach conflict files to each group.
	for file, indices := range graph.FileToTasks {
		if len(indices) > 1 {
			root := find(indices[0])
			groups[root].ConflictFiles = append(groups[root].ConflictFiles, file)
		}
	}

	// Assign sequential IDs and write back GroupID on tasks.
	result := make([]TaskGroup, 0, len(groups))
	idSeq := 0
	rootToID := make(map[int]int)
	for root, g := range groups {
		g.ID = idSeq
		rootToID[root] = idSeq
		idSeq++
		result = append(result, *g)
	}

	// Update GroupID on the original tasks slice.
	for i := range tasks {
		tasks[i].GroupID = rootToID[find(tasks[i].Index)]
	}

	return result, nil
}
