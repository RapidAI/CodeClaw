package commands

import (
	"context"
	"flag"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/RapidAI/CodeClaw/corelib/memory"
)

// RunMemory 执行 memory 子命令。
func RunMemory(args []string, dataDir string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui memory <list|search|save|delete|compress|backup>")
	}
	switch args[0] {
	case "list":
		return memoryList(dataDir, args[1:])
	case "search":
		return memorySearch(dataDir, args[1:])
	case "save":
		return memorySave(dataDir, args[1:])
	case "delete":
		return memoryDelete(dataDir, args[1:])
	case "compress":
		return memoryCompress(dataDir, args[1:])
	case "backup":
		return memoryBackup(dataDir, args[1:])
	default:
		return NewUsageError("unknown memory action: %s", args[0])
	}
}

func openMemoryStore(dataDir string) (*memory.Store, error) {
	path := filepath.Join(dataDir, "memory.json")
	return memory.NewStore(path)
}

func memoryList(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory list", flag.ExitOnError)
	category := fs.String("category", "", "按分类过滤")
	keyword := fs.String("keyword", "", "关键词搜索")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	entries := store.List(memory.Category(*category), *keyword)
	if *jsonOut {
		return PrintJSON(entries)
	}
	if len(entries) == 0 {
		fmt.Println("No memories found.")
		return nil
	}
	fmt.Printf("%-24s %-20s %-12s %s\n", "ID", "CATEGORY", "ACCESS", "CONTENT")
	fmt.Println(strings.Repeat("-", 80))
	for _, e := range entries {
		content := e.Content
		if len(content) > 40 {
			content = content[:37] + "..."
		}
		content = strings.ReplaceAll(content, "\n", " ")
		fmt.Printf("%-24s %-20s %-12d %s\n", e.ID, e.Category, e.AccessCount, content)
	}
	return nil
}

func memorySearch(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory search", flag.ExitOnError)
	category := fs.String("category", "", "按分类过滤")
	keyword := fs.String("keyword", "", "关键词")
	limit := fs.Int("limit", 10, "最大返回条数")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	// 允许直接传关键词作为位置参数
	kw := *keyword
	if kw == "" && fs.NArg() > 0 {
		kw = fs.Arg(0)
	}

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	entries := store.Search(memory.Category(*category), kw, *limit)
	if *jsonOut {
		return PrintJSON(entries)
	}
	if len(entries) == 0 {
		fmt.Println("No memories found.")
		return nil
	}
	fmt.Printf("%-24s %-20s %-12s %s\n", "ID", "CATEGORY", "ACCESS", "CONTENT")
	fmt.Println(strings.Repeat("-", 80))
	for _, e := range entries {
		content := strings.ReplaceAll(e.Content, "\n", " ")
		if len(content) > 40 {
			content = content[:37] + "..."
		}
		fmt.Printf("%-24s %-20s %-12d %s\n", e.ID, e.Category, e.AccessCount, content)
	}
	return nil
}

func memorySave(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory save", flag.ExitOnError)
	content := fs.String("content", "", "记忆内容（必填）")
	category := fs.String("category", "user_fact", "分类")
	tags := fs.String("tags", "", "标签（逗号分隔）")
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if *content == "" {
		return NewUsageError("usage: memory save --content <text> [--category <cat>] [--tags <t1,t2>]")
	}

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	var tagList []string
	if *tags != "" {
		tagList = strings.Split(*tags, ",")
	}

	entry := memory.Entry{
		Content:  *content,
		Category: memory.Category(*category),
		Tags:     tagList,
	}
	if err := store.Save(entry); err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(map[string]string{"status": "saved"})
	}
	fmt.Println("Memory saved.")
	return nil
}

func memoryDelete(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory delete", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	if fs.NArg() == 0 {
		return NewUsageError("usage: memory delete <id>")
	}
	id := fs.Arg(0)

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	if err := store.Delete(id); err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(map[string]string{"id": id, "status": "deleted"})
	}
	fmt.Printf("Memory %s deleted.\n", id)
	return nil
}

func memoryCompress(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory compress", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	// 无 LLM 时仅做 dedup（传 nil LLM）
	compressor := memory.NewCompressor(store, nil, nil)
	result, err := compressor.Compress(context.Background())
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(result)
	}
	fmt.Printf("Memory Compress Result:\n")
	fmt.Printf("  Total entries:  %d\n", result.TotalEntries)
	fmt.Printf("  Dedup removed:  %d\n", result.DedupCount)
	fmt.Printf("  Merged:         %d\n", result.MergedCount)
	fmt.Printf("  Compressed:     %d\n", result.CompressedCount)
	fmt.Printf("  Skipped:        %d\n", result.SkippedCount)
	fmt.Printf("  Errors:         %d\n", result.ErrorCount)
	fmt.Printf("  Saved chars:    %d\n", result.SavedChars)
	if result.BackupName != "" {
		fmt.Printf("  Backup:         %s\n", result.BackupName)
	}
	return nil
}

func memoryBackup(dataDir string, args []string) error {
	if len(args) == 0 {
		return NewUsageError("usage: maclaw-tui memory backup <list|restore|delete>")
	}
	switch args[0] {
	case "list":
		return memoryBackupList(dataDir, args[1:])
	case "restore":
		return memoryBackupRestore(dataDir, args[1:])
	case "delete":
		return memoryBackupDelete(dataDir, args[1:])
	default:
		return NewUsageError("unknown memory backup action: %s", args[0])
	}
}

func memoryBackupList(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory backup list", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "JSON 格式输出")
	fs.Parse(args)

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	compressor := memory.NewCompressor(store, nil, nil)
	backups, err := compressor.ListBackups()
	if err != nil {
		return err
	}
	if *jsonOut {
		return PrintJSON(backups)
	}
	if len(backups) == 0 {
		fmt.Println("No backups found.")
		return nil
	}
	fmt.Printf("%-40s %-22s %-10s %s\n", "NAME", "CREATED", "SIZE", "ENTRIES")
	fmt.Println(strings.Repeat("-", 85))
	for _, b := range backups {
		fmt.Printf("%-40s %-22s %-10d %d\n", b.Name, b.CreatedAt, b.SizeBytes, b.EntryCount)
	}
	return nil
}

func memoryBackupRestore(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory backup restore", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui memory backup restore <backup-name>")
	}
	name := fs.Arg(0)

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	compressor := memory.NewCompressor(store, nil, nil)
	if err := compressor.RestoreBackup(name); err != nil {
		return err
	}
	fmt.Printf("Backup %s restored.\n", name)
	return nil
}

func memoryBackupDelete(dataDir string, args []string) error {
	fs := flag.NewFlagSet("memory backup delete", flag.ExitOnError)
	fs.Parse(args)
	if fs.NArg() == 0 {
		return NewUsageError("usage: maclaw-tui memory backup delete <backup-name>")
	}
	name := fs.Arg(0)

	store, err := openMemoryStore(dataDir)
	if err != nil {
		return err
	}
	defer store.Stop()

	compressor := memory.NewCompressor(store, nil, nil)
	if err := compressor.DeleteBackup(name); err != nil {
		return err
	}
	fmt.Printf("Backup %s deleted.\n", name)
	return nil
}
