package main

import (
	"testing"
	"time"
)

func TestTopicDetector_FirstMessage(t *testing.T) {
	d := newTopicSwitchDetector(nil)
	mem := newConversationMemory()
	defer mem.stop()

	// No history → should always return TopicSame.
	if got := d.detect("你好", "user1", mem); got != TopicSame {
		t.Errorf("first message: got %v, want TopicSame", got)
	}
}

func TestTopicDetector_SameTopic(t *testing.T) {
	d := newTopicSwitchDetector(nil)
	mem := newConversationMemory()
	defer mem.stop()

	// Seed with conversation about Go programming.
	entries := []conversationEntry{
		{Role: "user", Content: "帮我写一个 Go 的 HTTP 服务器"},
		{Role: "assistant", Content: "好的，我来帮你写一个 Go HTTP 服务器"},
		{Role: "user", Content: "加一个路由处理函数"},
		{Role: "assistant", Content: "已添加路由处理"},
		{Role: "user", Content: "Go 的 HTTP 中间件怎么写"},
	}
	mem.save("user1", entries)

	// Same topic message.
	if got := d.detect("Go HTTP 服务器怎么加 TLS", "user1", mem); got != TopicSame {
		t.Errorf("same topic: got %v, want TopicSame", got)
	}
}

func TestTopicDetector_NewTopic(t *testing.T) {
	d := newTopicSwitchDetector(nil)
	// Lower the threshold so BM25 alone can trigger TopicNew without LLM.
	d.bm25NewThreshold = 5.0
	d.bm25SameThreshold = 10.0
	mem := newConversationMemory()
	defer mem.stop()

	// Seed with conversation about cooking.
	entries := []conversationEntry{
		{Role: "user", Content: "红烧肉怎么做好吃"},
		{Role: "assistant", Content: "红烧肉的做法是..."},
		{Role: "user", Content: "五花肉要不要焯水"},
		{Role: "assistant", Content: "建议焯水去腥"},
		{Role: "user", Content: "酱油放多少合适"},
	}
	mem.save("user1", entries)

	// Completely different topic.
	if got := d.detect("量子计算机的工作原理是什么", "user1", mem); got != TopicNew {
		t.Errorf("new topic: got %v, want TopicNew", got)
	}
}

func TestTopicDetector_TooFewTurns(t *testing.T) {
	d := newTopicSwitchDetector(nil)
	mem := newConversationMemory()
	defer mem.stop()

	// Only 2 user turns (below minTurnsForDetection=3).
	entries := []conversationEntry{
		{Role: "user", Content: "你好"},
		{Role: "assistant", Content: "你好！"},
		{Role: "user", Content: "今天天气怎么样"},
	}
	mem.save("user1", entries)

	// Should return TopicSame because too few turns.
	if got := d.detect("帮我写代码", "user1", mem); got != TopicSame {
		t.Errorf("too few turns: got %v, want TopicSame", got)
	}
}

func TestTopicDetector_TimeDecay(t *testing.T) {
	d := newTopicSwitchDetector(nil)
	d.timeDecayMinutes = 0.001 // ~60ms, so any real delay triggers full decay
	d.bm25NewThreshold = 0.5
	mem := newConversationMemory()
	defer mem.stop()

	entries := []conversationEntry{
		{Role: "user", Content: "帮我写一个 Python 脚本"},
		{Role: "assistant", Content: "好的"},
		{Role: "user", Content: "Python 怎么读取 CSV 文件"},
		{Role: "assistant", Content: "用 pandas"},
		{Role: "user", Content: "Python 的 pandas 库怎么用"},
	}
	mem.save("user1", entries)

	// Wait a bit so time decay kicks in.
	time.Sleep(100 * time.Millisecond)

	// Even a somewhat related message should be detected as new topic
	// because time decay makes the adjusted score very low.
	if got := d.detect("Python 的列表推导式", "user1", mem); got != TopicNew {
		t.Errorf("time decay: got %v, want TopicNew", got)
	}
}

func TestBuildQuickSummary(t *testing.T) {
	entries := []conversationEntry{
		{Role: "system", Content: "你是一个助手"},
		{Role: "user", Content: "帮我整理第7课的字幕"},
		{Role: "assistant", Content: "好的"},
		{Role: "user", Content: "把英文翻译成中文"},
	}
	got := buildQuickSummary(entries)
	if got != "对话话题: 把英文翻译成中文" {
		t.Errorf("buildQuickSummary: got %q", got)
	}
}

func TestBuildQuickSummary_Empty(t *testing.T) {
	got := buildQuickSummary(nil)
	if got != "" {
		t.Errorf("empty: got %q, want empty", got)
	}
}

func TestLastAccessTime(t *testing.T) {
	mem := newConversationMemory()
	defer mem.stop()

	// No session → zero time.
	if got := mem.lastAccessTime("nobody"); !got.IsZero() {
		t.Errorf("no session: got %v, want zero", got)
	}

	// After save, should have a recent time.
	mem.save("user1", []conversationEntry{{Role: "user", Content: "hi"}})
	got := mem.lastAccessTime("user1")
	if got.IsZero() {
		t.Error("after save: got zero time")
	}
	if time.Since(got) > time.Second {
		t.Errorf("after save: lastAccess too old: %v", got)
	}
}
