// Default Audit Program for MaClaw Hub content audit.
// Reads AuditRequest JSON from stdin, performs keyword matching for text,
// and writes AuditResponse JSON to stdout.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
)

type AuditRequest struct {
	Type     string   `json:"type"`
	Content  string   `json:"content"`
	UserID   string   `json:"user_id"`
	Platform string   `json:"platform"`
	Keywords []string `json:"keywords,omitempty"`
}

type AuditResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message,omitempty"`
}

func main() {
	keywordsFile := flag.String("keywords-file", "", "path to keywords file (one keyword per line)")
	flag.Parse()

	// Read request from stdin.
	var req AuditRequest
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		resp := AuditResponse{Code: -1, Message: fmt.Sprintf("invalid input: %v", err)}
		json.NewEncoder(os.Stdout).Encode(resp)
		os.Exit(0)
	}

	// For image and file types, pass through directly.
	if req.Type == "image" || req.Type == "file" {
		json.NewEncoder(os.Stdout).Encode(AuditResponse{Code: 0, Message: "审核通过"})
		return
	}

	// For text type, perform keyword matching.
	keywords := req.Keywords

	// If no keywords in request, try loading from file.
	if len(keywords) == 0 && *keywordsFile != "" {
		loaded, err := loadKeywordsFromFile(*keywordsFile)
		if err == nil {
			keywords = loaded
		}
	}

	// Check content against keywords.
	if len(keywords) > 0 {
		contentLower := strings.ToLower(req.Content)
		for _, kw := range keywords {
			if kw == "" {
				continue
			}
			if strings.Contains(contentLower, strings.ToLower(kw)) {
				resp := AuditResponse{
					Code:    2,
					Message: fmt.Sprintf("命中关键字: %s", kw),
				}
				json.NewEncoder(os.Stdout).Encode(resp)
				return
			}
		}
	}

	// No keyword hit, pass through.
	json.NewEncoder(os.Stdout).Encode(AuditResponse{Code: 0, Message: "审核通过"})
}

func loadKeywordsFromFile(path string) ([]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var keywords []string
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line != "" {
			keywords = append(keywords, line)
		}
	}
	return keywords, scanner.Err()
}
