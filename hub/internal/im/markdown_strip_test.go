package im

import "testing"

func TestStripMarkdown(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"plain text", "hello world", "hello world"},

		// Bold
		{"bold stars", "this is **bold** text", "this is bold text"},
		{"bold underscores", "this is __bold__ text", "this is bold text"},

		// Italic
		{"italic star", "this is *italic* text", "this is italic text"},
		{"italic underscore", "this is _italic_ text", "this is italic text"},

		// Bold+Italic
		{"bold italic", "***important***", "important"},

		// Strikethrough
		{"strikethrough", "~~removed~~", "removed"},

		// Inline code
		{"inline code", "use `fmt.Println` here", "use fmt.Println here"},

		// Code block fences
		{"code block", "```go\nfmt.Println()\n```", "\nfmt.Println()\n"},

		// Headers
		{"h1", "# Title", "Title"},
		{"h2", "## Subtitle", "Subtitle"},
		{"h3", "### Section", "Section"},

		// Links
		{"link", "[click here](https://example.com)", "click here"},

		// Images
		{"image", "![alt text](https://img.png)", "alt text"},
		{"image empty alt", "![](https://img.png)", ""},

		// Blockquotes
		{"blockquote", "> quoted text", "quoted text"},

		// Horizontal rules
		{"hr dashes", "---", ""},
		{"hr stars", "***", ""},
		{"hr underscores", "___", ""},

		// Combined
		{
			"mixed formatting",
			"## Result\n\n**Status**: `OK`\n> done\n\n---\n\nSee [docs](https://x.com).",
			"Result\n\nStatus: OK\ndone\n\n\n\nSee docs.",
		},

		// Regression: __text__ was producing empty string
		{
			"bold underscore regression",
			"__hello__ world",
			"hello world",
		},

		// Regression: _text_ was eating surrounding whitespace
		{
			"italic underscore preserves context",
			"the _quick_ brown fox",
			"the quick brown fox",
		},

		// Edge: _word_ at line start
		{
			"italic underscore at line start",
			"_hello_ world",
			"hello world",
		},

		// Edge: _word_ at second line start
		{
			"italic underscore at second line start",
			"first line\n_second_ line",
			"first line\nsecond line",
		},

		// Edge: bold+italic ___text___
		{
			"bold italic underscores",
			"___important___",
			"important",
		},

		// Edge: nested bold in italic — best effort
		{
			"nested formatting",
			"this is **bold and *italic* inside**",
			"this is bold and italic inside",
		},

		// Edge: multiple underscores in one line
		{
			"multiple italic underscores",
			"_one_ and _two_ done",
			"one and two done",
		},

		// Edge: underscore in middle of word should NOT strip
		{
			"underscore in word",
			"some_variable_name",
			"some_variable_name",
		},

		// Edge: list items (should pass through)
		{
			"list items",
			"- item one\n- item two",
			"- item one\n- item two",
		},

		// Edge: numbered list
		{
			"numbered list",
			"1. first\n2. second",
			"1. first\n2. second",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := stripMarkdown(tt.in)
			if got != tt.want {
				t.Errorf("stripMarkdown(%q)\n  got:  %q\n  want: %q", tt.in, got, tt.want)
			}
		})
	}
}
