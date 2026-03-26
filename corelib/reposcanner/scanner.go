package reposcanner

import (
	"bufio"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
)

// ignoredDirs are always skipped during traversal.
var ignoredDirs = map[string]bool{
	".git":         true,
	".svn":         true,
	".hg":          true,
	"node_modules": true,
	"vendor":       true,
	"dist":         true,
	"build":        true,
	"out":          true,
	"target":       true,
	".next":        true,
	".nuxt":        true,
	"coverage":     true,
	"__pycache__":  true,
	".cache":       true,
	".gocache":     true,
	"tmp":          true,
	".tox":         true,
	".venv":        true,
	"venv":         true,
	"env":          true,
}

// ignoredFiles are skipped by exact name.
var ignoredFiles = map[string]bool{
	"package-lock.json": true,
	"pnpm-lock.yaml":   true,
	"yarn.lock":        true,
	"poetry.lock":      true,
	"Cargo.lock":       true,
	"composer.lock":    true,
	"Gemfile.lock":     true,
	"go.sum":           true,
}

// ignoredExts are skipped by extension (filepath.Ext result).
var ignoredExts = map[string]bool{
	".map":     true,
	".wasm":    true,
	".pyc":     true,
	".class":   true,
	".o":       true,
	".a":       true,
	".so":      true,
	".dll":     true,
	".exe":     true,
	".dylib":   true,
	".jar":     true,
	".zip":     true,
	".tar":     true,
	".gz":      true,
	".png":     true,
	".jpg":     true,
	".jpeg":    true,
	".gif":     true,
	".ico":     true,
	".svg":     true,
	".mp3":     true,
	".mp4":     true,
	".wav":     true,
	".pdf":     true,
	".ttf":     true,
	".woff":    true,
	".woff2":   true,
	".eot":     true,
}

// fileEntry is collected during the walk phase.
type fileEntry struct {
	RelPath   string
	AbsPath   string
	Size      int64
	Extension string
	Dir       string // first-level directory under root
}

// Scanner walks a repository and builds a ScanResult.
type Scanner struct {
	root string
	cfg  ScanConfig
	llm  LLMSummariser // nil in fast mode
}

// NewScanner creates a scanner for the given root path.
func NewScanner(root string, cfg ScanConfig, llm LLMSummariser) *Scanner {
	return &Scanner{root: root, cfg: cfg, llm: llm}
}

// Walk traverses the repository and collects file entries.
func (s *Scanner) Walk() ([]fileEntry, error) {
	var entries []fileEntry

	err := filepath.WalkDir(s.root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil // skip inaccessible
		}
		name := d.Name()

		if d.IsDir() {
			if ignoredDirs[name] {
				return filepath.SkipDir
			}
			return nil
		}

		// skip by name
		if ignoredFiles[name] {
			return nil
		}
		// skip by extension
		ext := strings.ToLower(filepath.Ext(name))
		if ignoredExts[ext] {
			return nil
		}
		// double extension check (.min.js)
		if strings.HasSuffix(strings.ToLower(name), ".min.js") || strings.HasSuffix(strings.ToLower(name), ".min.css") {
			return nil
		}

		info, infoErr := d.Info()
		if infoErr != nil {
			return nil
		}
		if info.Size() > s.cfg.MaxFileSizeBytes {
			return nil
		}

		rel, _ := filepath.Rel(s.root, path)
		rel = filepath.ToSlash(rel)

		topDir := ""
		if idx := strings.Index(rel, "/"); idx > 0 {
			topDir = rel[:idx]
		}

		entries = append(entries, fileEntry{
			RelPath:   rel,
			AbsPath:   path,
			Size:      info.Size(),
			Extension: ext,
			Dir:       topDir,
		})
		return nil
	})

	return entries, err
}

// extToLang maps file extension to language name (package-level, allocated once).
var extToLang = map[string]string{
	".go":    "Go",
	".js":    "JavaScript",
	".ts":    "TypeScript",
	".tsx":   "TypeScript",
	".jsx":   "JavaScript",
	".py":    "Python",
	".rs":    "Rust",
	".java":  "Java",
	".kt":    "Kotlin",
	".c":     "C",
	".cpp":   "C++",
	".h":     "C/C++",
	".cs":    "C#",
	".rb":    "Ruby",
	".php":   "PHP",
	".swift": "Swift",
	".dart":  "Dart",
	".lua":   "Lua",
	".sh":    "Shell",
	".bash":  "Shell",
	".zsh":   "Shell",
	".bat":   "Batch",
	".cmd":   "Batch",
	".ps1":   "PowerShell",
	".sql":   "SQL",
	".html":  "HTML",
	".css":   "CSS",
	".scss":  "SCSS",
	".less":  "LESS",
	".md":    "Markdown",
	".yaml":  "YAML",
	".yml":   "YAML",
	".json":  "JSON",
	".toml":  "TOML",
	".xml":   "XML",
	".proto": "Protobuf",
	".ets":   "ArkTS",
	".mjs":   "JavaScript",
}

// langName maps extension to language name.
func langName(ext string) string {
	if name, ok := extToLang[ext]; ok {
		return name
	}
	if ext == "" {
		return "Unknown"
	}
	return strings.TrimPrefix(ext, ".")
}

// maxLineLen caps individual line length to avoid memory issues with minified files.
const maxLineLen = 4096

// readHead reads the first N lines of a file and returns the head content,
// total line count, and any error. Lines longer than maxLineLen are truncated.
func readHead(path string, maxLines int) (string, int, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()

	var lines []string
	totalLines := 0
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		totalLines++
		if len(lines) < maxLines {
			line := sc.Text()
			if len(line) > maxLineLen {
				line = line[:maxLineLen] + "..."
			}
			lines = append(lines, line)
		}
	}
	return strings.Join(lines, "\n"), totalLines, nil
}

// extractSymbols does a quick regex-free scan for exported symbols.
// It looks for common patterns: func/class/def/export/type/struct/interface.
func extractSymbols(head string, lang string) []string {
	var symbols []string
	seen := make(map[string]bool)
	lines := strings.Split(head, "\n")

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		var sym string

		switch {
		case strings.HasPrefix(trimmed, "func "):
			sym = extractAfter(trimmed, "func ")
		case strings.HasPrefix(trimmed, "type ") && (strings.Contains(trimmed, "struct") || strings.Contains(trimmed, "interface")):
			sym = extractAfter(trimmed, "type ")
		case strings.HasPrefix(trimmed, "class "):
			sym = extractAfter(trimmed, "class ")
		case strings.HasPrefix(trimmed, "def "):
			sym = extractAfter(trimmed, "def ")
		case strings.HasPrefix(trimmed, "export function "):
			sym = extractAfter(trimmed, "export function ")
		case strings.HasPrefix(trimmed, "export class "):
			sym = extractAfter(trimmed, "export class ")
		case strings.HasPrefix(trimmed, "export default function "):
			sym = extractAfter(trimmed, "export default function ")
		case strings.HasPrefix(trimmed, "export interface "):
			sym = extractAfter(trimmed, "export interface ")
		case strings.HasPrefix(trimmed, "pub fn "):
			sym = extractAfter(trimmed, "pub fn ")
		case strings.HasPrefix(trimmed, "pub struct "):
			sym = extractAfter(trimmed, "pub struct ")
		}

		if sym != "" && !seen[sym] {
			seen[sym] = true
			symbols = append(symbols, sym)
		}
		if len(symbols) >= 20 {
			break
		}
	}
	return symbols
}

// extractAfter extracts the identifier after a prefix keyword.
func extractAfter(line, prefix string) string {
	rest := strings.TrimPrefix(line, prefix)
	// strip receiver for Go methods: (r *Receiver) MethodName
	if strings.HasPrefix(rest, "(") {
		if idx := strings.Index(rest, ")"); idx > 0 {
			rest = strings.TrimSpace(rest[idx+1:])
		}
	}
	// take until first non-identifier char
	var b strings.Builder
	for _, r := range rest {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			break
		}
	}
	return b.String()
}

// readFileCards reads file content in parallel and produces FileCards.
func (s *Scanner) readFileCards(entries []fileEntry) []FileCard {
	// copy to avoid mutating caller's slice
	sorted := make([]fileEntry, len(entries))
	copy(sorted, entries)

	// sort by priority heuristic: entry files first, then by size ascending
	sort.Slice(sorted, func(i, j int) bool {
		pi := filePriority(sorted[i].RelPath)
		pj := filePriority(sorted[j].RelPath)
		if pi != pj {
			return pi < pj
		}
		return sorted[i].Size < sorted[j].Size
	})

	// cap to reasonable number
	maxFiles := 200
	if len(sorted) > maxFiles {
		sorted = sorted[:maxFiles]
	}

	cards := make([]FileCard, len(sorted))
	sem := make(chan struct{}, s.cfg.ReadConcurrency)
	var wg sync.WaitGroup

	for i, e := range sorted {
		wg.Add(1)
		go func(idx int, fe fileEntry) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			head, totalLines, err := readHead(fe.AbsPath, s.cfg.MaxFileReadLines)
			if err != nil {
				return
			}

			lang := langName(fe.Extension)
			symbols := extractSymbols(head, lang)

			// truncate head snippet for storage (UTF-8 safe)
			snippet := truncateRunes(head, 1000)

			cards[idx] = FileCard{
				Path:        fe.RelPath,
				Role:        guessFileRole(fe.RelPath),
				SizeBytes:   fe.Size,
				Lines:       totalLines,
				Language:    lang,
				KeySymbols:  symbols,
				HeadSnippet: snippet,
				Priority:    filePriority(fe.RelPath),
			}
		}(i, e)
	}
	wg.Wait()

	// filter out zero-value cards (failed reads)
	var result []FileCard
	for _, c := range cards {
		if c.Path != "" {
			result = append(result, c)
		}
	}
	return result
}

// truncateRunes truncates a string to at most maxRunes runes, appending
// a truncation marker if needed. This is UTF-8 safe.
func truncateRunes(s string, maxRunes int) string {
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes]) + "\n... (truncated)"
}

// filePriority assigns 1/2/3 based on path patterns.
func filePriority(rel string) int {
	name := filepath.Base(rel)
	lower := strings.ToLower(name)

	// Priority 1: skeleton / entry files
	skeletonNames := []string{
		"main.go", "main.ts", "main.py", "main.rs", "main.dart", "main.java",
		"app.go", "app.ts", "app.tsx", "app.py",
		"index.ts", "index.js", "index.tsx",
		"mod.rs", "lib.rs",
		"router.go", "routes.go", "router.ts",
		"bootstrap.go", "server.go",
		"go.mod", "package.json", "cargo.toml", "pyproject.toml",
		"makefile", "dockerfile", "docker-compose.yml",
		"readme.md", "readme.txt",
	}
	for _, s := range skeletonNames {
		if lower == s {
			return 1
		}
	}

	// Priority 1: config at root level
	if !strings.Contains(rel, "/") {
		return 1
	}

	// Priority 2: handler / service / controller / types
	keywords := []string{"handler", "service", "controller", "types", "model", "schema", "interface", "api"}
	for _, kw := range keywords {
		if strings.Contains(lower, kw) {
			return 2
		}
	}

	// Priority 3: everything else
	return 3
}

// guessFileRole returns a short role description based on path.
func guessFileRole(rel string) string {
	lower := strings.ToLower(rel)
	name := strings.ToLower(filepath.Base(rel))

	switch {
	case name == "main.go" || name == "main.ts" || name == "main.py" || name == "main.dart":
		return "entry point"
	case name == "go.mod" || name == "package.json" || name == "cargo.toml" || name == "pyproject.toml":
		return "project config"
	case name == "readme.md" || name == "readme.txt":
		return "documentation"
	case name == "dockerfile" || name == "docker-compose.yml":
		return "container config"
	case name == "makefile" || name == "cmakelists.txt":
		return "build config"
	case strings.Contains(lower, "test"):
		return "test"
	case strings.Contains(lower, "handler"):
		return "request handler"
	case strings.Contains(lower, "router") || strings.Contains(lower, "route"):
		return "routing"
	case strings.Contains(lower, "service"):
		return "service logic"
	case strings.Contains(lower, "store") || strings.Contains(lower, "repo"):
		return "data layer"
	case strings.Contains(lower, "types") || strings.Contains(lower, "model"):
		return "type definitions"
	case strings.Contains(lower, "config"):
		return "configuration"
	case strings.Contains(lower, "util") || strings.Contains(lower, "helper"):
		return "utility"
	default:
		return "implementation"
	}
}
