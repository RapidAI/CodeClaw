package reposcanner

import (
	"fmt"
	"time"
)

// Scan performs a full repository scan and returns a compressed ScanResult.
// This is the main entry point for the reposcanner package.
func Scan(root string, cfg ScanConfig, llm LLMSummariser) (*ScanResult, error) {
	start := time.Now()

	scanner := NewScanner(root, cfg, llm)

	// Step 1: Walk the repository
	entries, err := scanner.Walk()
	if err != nil {
		return nil, fmt.Errorf("walk repository: %w", err)
	}
	if len(entries) == 0 {
		return nil, fmt.Errorf("no files found in %s", root)
	}

	// Step 2: Build repo overview
	overview := BuildOverview(root, entries)

	// Step 3: Discover modules
	modules := BuildModules(root, entries)

	// Step 4: Read key files and build file cards
	cards := scanner.readFileCards(entries)

	// Step 5: Deep summarisation (optional)
	if cfg.DeepMode && llm != nil {
		cards = DeepSummariseFiles(cards, llm, cfg.LLMConcurrency)
	}

	result := &ScanResult{
		Overview:  overview,
		Modules:   modules,
		KeyFiles:  cards,
		ScannedAt: time.Now(),
		ElapsedMS: time.Since(start).Milliseconds(),
	}

	// Step 6: Compress to markdown
	result.CompressedMD = CompressToMarkdown(result, cfg.TokenBudget)

	return result, nil
}
