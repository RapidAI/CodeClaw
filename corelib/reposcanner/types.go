// Package reposcanner provides large repository scanning, indexing, and
// context compression for maclaw's LLM-based document generation pipeline.
package reposcanner

import "time"

// ScanConfig controls scanner behaviour.
type ScanConfig struct {
	// MaxFileReadLines limits how many lines are read per file (head).
	MaxFileReadLines int `json:"max_file_read_lines"`
	// MaxFileSizeBytes skips files larger than this.
	MaxFileSizeBytes int64 `json:"max_file_size_bytes"`
	// ReadConcurrency controls parallel file reads.
	ReadConcurrency int `json:"read_concurrency"`
	// LLMConcurrency controls parallel LLM summarisation calls.
	LLMConcurrency int `json:"llm_concurrency"`
	// DeepMode enables LLM-based file summarisation (slower, richer).
	DeepMode bool `json:"deep_mode"`
	// TokenBudget is the soft upper bound for the compressed output (runes).
	TokenBudget int `json:"token_budget"`
}

// DefaultScanConfig returns sensible defaults.
func DefaultScanConfig() ScanConfig {
	return ScanConfig{
		MaxFileReadLines: 200,
		MaxFileSizeBytes: 2 * 1024 * 1024, // 2 MB
		ReadConcurrency:  8,
		LLMConcurrency:   3,
		DeepMode:         false,
		TokenBudget:      6000,
	}
}

// --- Repo Overview ---

// RepoOverview is the Step-1 output: a high-level map of the repository.
type RepoOverview struct {
	RootPath      string            `json:"root_path"`
	ProjectType   string            `json:"project_type"`
	TechStack     []string          `json:"tech_stack"`
	BuildSystem   string            `json:"build_system"`
	TopDirs       []string          `json:"top_dirs"`
	KeyFiles      []string          `json:"key_files"`
	TestFramework string            `json:"test_framework"`
	TotalFiles    int               `json:"total_files"`
	TotalDirs     int               `json:"total_dirs"`
	LangStats     map[string]int    `json:"lang_stats"` // extension -> file count
	ScannedAt     time.Time         `json:"scanned_at"`
}

// --- Module Card ---

// ModuleCard represents a discovered module / top-level package.
type ModuleCard struct {
	Name       string   `json:"name"`
	Path       string   `json:"path"`
	Role       string   `json:"role"`
	FileCount  int      `json:"file_count"`
	Languages  []string `json:"languages"`
	MainFiles  []string `json:"main_files"`
	Summary    string   `json:"summary"`
}

// --- File Card ---

// FileCard is a compressed representation of a single important file.
type FileCard struct {
	Path          string   `json:"path"`
	Role          string   `json:"role"`
	SizeBytes     int64    `json:"size_bytes"`
	Lines         int      `json:"lines"`
	Language      string   `json:"language"`
	KeySymbols    []string `json:"key_symbols"`
	HeadSnippet   string   `json:"head_snippet"`
	Summary       string   `json:"summary"`
	Priority      int      `json:"priority"` // 1=skeleton, 2=key impl, 3=supplementary
}

// --- Scan Result ---

// ScanResult is the full output of a repository scan.
type ScanResult struct {
	Overview    RepoOverview `json:"overview"`
	Modules     []ModuleCard `json:"modules"`
	KeyFiles    []FileCard   `json:"key_files"`
	CompressedMD string      `json:"compressed_md"`
	ScannedAt   time.Time    `json:"scanned_at"`
	ElapsedMS   int64        `json:"elapsed_ms"`
}

// --- LLM interface ---

// LLMSummariser is called in deep mode to summarise file content.
type LLMSummariser interface {
	Summarise(prompt string) (string, error)
}
