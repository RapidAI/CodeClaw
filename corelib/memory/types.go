package memory

import "time"

// Category represents the category of a memory entry.
type Category string

const (
	CategoryUserFact            Category = "user_fact"
	CategoryPreference          Category = "preference"
	CategoryProjectKnowledge    Category = "project_knowledge"
	CategoryInstruction         Category = "instruction"
	CategoryConversationSummary Category = "conversation_summary"
	CategorySessionCheckpoint   Category = "session_checkpoint"
)

// Entry represents a single memory record.
type Entry struct {
	ID          string   `json:"id"`
	Content     string   `json:"content"`
	Category    Category `json:"category"`
	Tags        []string `json:"tags"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
	AccessCount int      `json:"access_count"`
}

// BackupInfo describes a single memory backup snapshot.
type BackupInfo struct {
	Name       string `json:"name"`
	CreatedAt  string `json:"created_at"`
	SizeBytes  int64  `json:"size_bytes"`
	EntryCount int    `json:"entry_count"`
}

// CompressResult holds the outcome of a compression run.
type CompressResult struct {
	BackupName      string `json:"backup_name"`
	TotalEntries    int    `json:"total_entries"`
	DedupCount      int    `json:"dedup_count"`
	MergedCount     int    `json:"merged_count"`
	CompressedCount int    `json:"compressed_count"`
	SkippedCount    int    `json:"skipped_count"`
	ErrorCount      int    `json:"error_count"`
	SavedChars      int    `json:"saved_chars"`
}

// CompressorStatus is returned by the status query.
type CompressorStatus struct {
	Running    bool            `json:"running"`
	LastRun    string          `json:"last_run,omitempty"`
	LastResult *CompressResult `json:"last_result,omitempty"`
	LastError  string          `json:"last_error,omitempty"`
}
