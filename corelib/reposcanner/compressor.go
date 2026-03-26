package reposcanner

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

// CompressToMarkdown converts a ScanResult into a structured markdown summary
// suitable for LLM consumption. It respects the token budget by trimming
// lower-priority content first.
func CompressToMarkdown(result *ScanResult, budget int) string {
	var b strings.Builder

	// Section 1: Repo Overview (always included)
	b.WriteString("# Repo Overview\n\n")
	ov := result.Overview
	fmt.Fprintf(&b, "- Project Type: %s\n", ov.ProjectType)
	fmt.Fprintf(&b, "- Tech Stack: %s\n", strings.Join(ov.TechStack, ", "))
	fmt.Fprintf(&b, "- Build System: %s\n", ov.BuildSystem)
	if ov.TestFramework != "" {
		fmt.Fprintf(&b, "- Test Framework: %s\n", ov.TestFramework)
	}
	fmt.Fprintf(&b, "- Total Files: %d\n", ov.TotalFiles)
	fmt.Fprintf(&b, "- Total Dirs: %d\n", ov.TotalDirs)
	b.WriteString("- Top Dirs: ")
	b.WriteString(strings.Join(ov.TopDirs, ", "))
	b.WriteString("\n")
	if len(ov.KeyFiles) > 0 {
		b.WriteString("- Key Files: ")
		b.WriteString(strings.Join(ov.KeyFiles, ", "))
		b.WriteString("\n")
	}

	// Language stats (sorted by count descending for deterministic output)
	b.WriteString("\n## Language Distribution\n\n")
	type langCount struct {
		Lang  string
		Count int
	}
	var langCounts []langCount
	for lang, count := range ov.LangStats {
		langCounts = append(langCounts, langCount{lang, count})
	}
	sort.Slice(langCounts, func(i, j int) bool {
		if langCounts[i].Count != langCounts[j].Count {
			return langCounts[i].Count > langCounts[j].Count
		}
		return langCounts[i].Lang < langCounts[j].Lang
	})
	for _, lc := range langCounts {
		fmt.Fprintf(&b, "- %s: %d files\n", lc.Lang, lc.Count)
	}

	// Section 2: Modules
	b.WriteString("\n# Modules\n\n")
	for i, m := range result.Modules {
		fmt.Fprintf(&b, "%d. **%s** (%s)\n", i+1, m.Name, m.Role)
		fmt.Fprintf(&b, "   - Files: %d | Languages: %s\n", m.FileCount, strings.Join(m.Languages, ", "))
		if len(m.MainFiles) > 0 {
			fmt.Fprintf(&b, "   - Main: %s\n", strings.Join(m.MainFiles, ", "))
		}
		if m.Summary != "" {
			fmt.Fprintf(&b, "   - Summary: %s\n", m.Summary)
		}
	}

	// Check budget before adding file cards
	currentLen := len([]rune(b.String()))
	remaining := budget - currentLen
	if remaining < 200 {
		b.WriteString("\n(File cards omitted due to token budget)\n")
		return b.String()
	}

	// Section 3: Key File Cards (priority 1 first, then 2, then 3)
	b.WriteString("\n# Key Files\n\n")
	for _, card := range result.KeyFiles {
		cardText := formatFileCard(card)
		cardLen := len([]rune(cardText))
		if remaining-cardLen < 100 {
			b.WriteString("\n(Remaining file cards omitted due to token budget)\n")
			break
		}
		b.WriteString(cardText)
		remaining -= cardLen
	}

	return b.String()
}

// formatFileCard formats a single FileCard as markdown.
func formatFileCard(c FileCard) string {
	var b strings.Builder
	fmt.Fprintf(&b, "## %s\n", c.Path)
	fmt.Fprintf(&b, "- Role: %s | Language: %s | Lines: %d\n", c.Role, c.Language, c.Lines)
	if len(c.KeySymbols) > 0 {
		syms := c.KeySymbols
		if len(syms) > 10 {
			syms = syms[:10]
		}
		fmt.Fprintf(&b, "- Symbols: %s\n", strings.Join(syms, ", "))
	}
	if c.Summary != "" {
		fmt.Fprintf(&b, "- Summary: %s\n", c.Summary)
	}
	b.WriteString("\n")
	return b.String()
}

// DeepSummariseFiles calls the LLM to summarise key files (priority 1 and 2).
// This is only used in DeepMode.
func DeepSummariseFiles(cards []FileCard, llm LLMSummariser, concurrency int) []FileCard {
	if llm == nil || concurrency <= 0 {
		return cards
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup

	for i := range cards {
		if cards[i].Priority > 2 || cards[i].HeadSnippet == "" {
			continue
		}
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			prompt := fmt.Sprintf(
				"Summarise this source file in 1-2 sentences. Focus on its purpose and key functionality.\n\nFile: %s\nLanguage: %s\n\n```\n%s\n```",
				cards[idx].Path, cards[idx].Language, cards[idx].HeadSnippet,
			)
			summary, err := llm.Summarise(prompt)
			if err == nil && summary != "" {
				cards[idx].Summary = summary
			}
		}(i)
	}
	wg.Wait()
	return cards
}
