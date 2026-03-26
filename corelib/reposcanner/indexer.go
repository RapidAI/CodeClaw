package reposcanner

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// BuildOverview produces a RepoOverview from collected file entries.
func BuildOverview(root string, entries []fileEntry) RepoOverview {
	langStats := make(map[string]int)
	topDirSet := make(map[string]bool)
	var totalDirs int

	for _, e := range entries {
		lang := langName(e.Extension)
		langStats[lang]++
		if e.Dir != "" {
			topDirSet[e.Dir] = true
		}
	}

	// count directories via a quick walk (only depth 1)
	dirEntries, _ := os.ReadDir(root)
	for _, d := range dirEntries {
		if d.IsDir() && !ignoredDirs[d.Name()] && !strings.HasPrefix(d.Name(), ".") {
			totalDirs++
		}
	}

	topDirs := make([]string, 0, len(topDirSet))
	for d := range topDirSet {
		topDirs = append(topDirs, d)
	}
	sort.Strings(topDirs)

	keyFiles := detectKeyFiles(root)
	projectType := detectProjectType(root, entries)
	stack := detectTechStack(langStats)
	buildSys := detectBuildSystem(root)
	testFw := detectTestFramework(root, entries)

	return RepoOverview{
		RootPath:      root,
		ProjectType:   projectType,
		TechStack:     stack,
		BuildSystem:   buildSys,
		TopDirs:       topDirs,
		KeyFiles:      keyFiles,
		TestFramework: testFw,
		TotalFiles:    len(entries),
		TotalDirs:     totalDirs,
		LangStats:     langStats,
		ScannedAt:     time.Now(),
	}
}

// detectKeyFiles finds important root-level files.
func detectKeyFiles(root string) []string {
	candidates := []string{
		"go.mod", "go.sum",
		"package.json", "tsconfig.json",
		"Cargo.toml",
		"pyproject.toml", "setup.py", "requirements.txt",
		"Makefile", "CMakeLists.txt",
		"Dockerfile", "docker-compose.yml", "docker-compose.yaml",
		"README.md", "README.txt", "README",
		".gitignore",
		"pnpm-workspace.yaml", "turbo.json", "lerna.json",
		"build.gradle", "pom.xml",
	}
	var found []string
	for _, c := range candidates {
		if _, err := os.Stat(filepath.Join(root, c)); err == nil {
			found = append(found, c)
		}
	}
	return found
}

// detectProjectType guesses the project type.
func detectProjectType(root string, entries []fileEntry) string {
	hasGoMod := fileExists(root, "go.mod")
	hasPkg := fileExists(root, "package.json")
	hasCargo := fileExists(root, "Cargo.toml")
	hasPyproject := fileExists(root, "pyproject.toml") || fileExists(root, "setup.py")
	hasPnpmWs := fileExists(root, "pnpm-workspace.yaml")
	hasTurbo := fileExists(root, "turbo.json")
	hasLerna := fileExists(root, "lerna.json")

	// count top-level dirs with their own go.mod / package.json
	subModules := 0
	dirEntries, _ := os.ReadDir(root)
	for _, d := range dirEntries {
		if !d.IsDir() || ignoredDirs[d.Name()] {
			continue
		}
		sub := filepath.Join(root, d.Name())
		if fileExists(sub, "go.mod") || fileExists(sub, "package.json") || fileExists(sub, "Cargo.toml") {
			subModules++
		}
	}

	if hasPnpmWs || hasTurbo || hasLerna || subModules >= 3 {
		return "monorepo"
	}
	if hasGoMod && subModules >= 2 {
		return "go-monorepo"
	}

	switch {
	case hasGoMod:
		return "go-project"
	case hasCargo:
		return "rust-project"
	case hasPyproject:
		return "python-project"
	case hasPkg:
		return "node-project"
	default:
		return "unknown"
	}
}

// detectTechStack returns top languages sorted by file count.
func detectTechStack(langStats map[string]int) []string {
	type kv struct {
		Lang  string
		Count int
	}
	var sorted []kv
	for lang, count := range langStats {
		if lang == "Unknown" || lang == "Markdown" || lang == "JSON" || lang == "YAML" || lang == "TOML" || lang == "XML" {
			continue
		}
		sorted = append(sorted, kv{lang, count})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Count > sorted[j].Count })

	var stack []string
	for i, kv := range sorted {
		if i >= 5 {
			break
		}
		stack = append(stack, kv.Lang)
	}
	return stack
}

// detectBuildSystem guesses the build system.
func detectBuildSystem(root string) string {
	switch {
	case fileExists(root, "Makefile"):
		return "make"
	case fileExists(root, "CMakeLists.txt"):
		return "cmake"
	case fileExists(root, "turbo.json"):
		return "turborepo"
	case fileExists(root, "pnpm-workspace.yaml"):
		return "pnpm"
	case fileExists(root, "build.gradle") || fileExists(root, "build.gradle.kts"):
		return "gradle"
	case fileExists(root, "pom.xml"):
		return "maven"
	case fileExists(root, "Cargo.toml"):
		return "cargo"
	case fileExists(root, "go.mod"):
		return "go"
	case fileExists(root, "package.json"):
		return "npm"
	default:
		return "unknown"
	}
}

// detectTestFramework guesses the test framework.
func detectTestFramework(root string, entries []fileEntry) string {
	// check package.json for test scripts
	if data, err := os.ReadFile(filepath.Join(root, "package.json")); err == nil {
		s := string(data)
		switch {
		case strings.Contains(s, "vitest"):
			return "vitest"
		case strings.Contains(s, "jest"):
			return "jest"
		case strings.Contains(s, "mocha"):
			return "mocha"
		case strings.Contains(s, "playwright"):
			return "playwright"
		}
	}

	// Go: _test.go files
	for _, e := range entries {
		if strings.HasSuffix(e.RelPath, "_test.go") {
			return "go test"
		}
	}
	// Python: pytest
	if fileExists(root, "pytest.ini") || fileExists(root, "conftest.py") {
		return "pytest"
	}
	// Rust
	for _, e := range entries {
		if e.Extension == ".rs" && strings.Contains(e.RelPath, "test") {
			return "cargo test"
		}
	}
	return ""
}

// BuildModules groups files into module cards.
func BuildModules(root string, entries []fileEntry) []ModuleCard {
	groups := make(map[string][]fileEntry)
	for _, e := range entries {
		dir := e.Dir
		if dir == "" {
			dir = "(root)"
		}
		groups[dir] = append(groups[dir], e)
	}

	var modules []ModuleCard
	for dir, files := range groups {
		langSet := make(map[string]bool)
		var mainFiles []string
		for _, f := range files {
			lang := langName(f.Extension)
			langSet[lang] = true
			if filePriority(f.RelPath) == 1 {
				mainFiles = append(mainFiles, f.RelPath)
			}
		}
		langs := make([]string, 0, len(langSet))
		for l := range langSet {
			langs = append(langs, l)
		}
		sort.Strings(langs)

		if len(mainFiles) > 5 {
			mainFiles = mainFiles[:5]
		}

		modules = append(modules, ModuleCard{
			Name:      dir,
			Path:      dir,
			Role:      guessModuleRole(dir),
			FileCount: len(files),
			Languages: langs,
			MainFiles: mainFiles,
		})
	}

	// sort by file count descending, keep top 20
	sort.Slice(modules, func(i, j int) bool {
		return modules[i].FileCount > modules[j].FileCount
	})
	if len(modules) > 20 {
		modules = modules[:20]
	}
	return modules
}

// guessModuleRole returns a short role based on directory name.
func guessModuleRole(dir string) string {
	lower := strings.ToLower(dir)
	switch {
	case lower == "(root)":
		return "project root"
	case strings.Contains(lower, "cmd") || strings.Contains(lower, "bin"):
		return "CLI / entry points"
	case strings.Contains(lower, "internal"):
		return "internal packages"
	case strings.Contains(lower, "pkg") || strings.Contains(lower, "lib") || strings.Contains(lower, "corelib"):
		return "shared library"
	case strings.Contains(lower, "api") || strings.Contains(lower, "httpapi"):
		return "API layer"
	case strings.Contains(lower, "web") || strings.Contains(lower, "frontend"):
		return "web frontend"
	case strings.Contains(lower, "mobile"):
		return "mobile app"
	case strings.Contains(lower, "test"):
		return "tests"
	case strings.Contains(lower, "script"):
		return "scripts / tooling"
	case strings.Contains(lower, "doc"):
		return "documentation"
	case strings.Contains(lower, "deploy") || strings.Contains(lower, "infra"):
		return "deployment / infra"
	case strings.Contains(lower, "build"):
		return "build system"
	case strings.Contains(lower, "gui"):
		return "desktop GUI"
	case strings.Contains(lower, "tui"):
		return "terminal UI"
	case strings.Contains(lower, "hub"):
		return "hub server"
	default:
		return "module"
	}
}

func fileExists(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name))
	return err == nil
}
