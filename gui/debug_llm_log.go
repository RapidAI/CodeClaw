package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// debugLLMLog writes a debug line to ~/.maclaw/logs/llm_debug.log
func debugLLMLog(format string, args ...interface{}) {
	home, _ := os.UserHomeDir()
	dir := filepath.Join(home, ".maclaw", "logs")
	_ = os.MkdirAll(dir, 0755)
	f, err := os.OpenFile(filepath.Join(dir, "llm_debug.log"), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	msg := fmt.Sprintf(format, args...)
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("15:04:05.000"), msg)
}
