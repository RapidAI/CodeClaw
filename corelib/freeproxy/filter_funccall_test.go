package freeproxy

import "testing"

func TestFilterFunctionCalls(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		inBlock bool // initial state
		want    string
		wantIn  bool // expected state after
	}{
		{
			name:  "no_markers",
			input: "hello world",
			want:  "hello world",
		},
		{
			name:  "full_block_removed",
			input: `<|FunctionCallBegin|>[{"name": "set_nickname", "parameters": {"nickname": "安妮"}}]<|FunctionCallEnd|>`,
			want:  "",
		},
		{
			name:  "text_before_block",
			input: `好的<|FunctionCallBegin|>[{"name":"x"}]<|FunctionCallEnd|>`,
			want:  "好的",
		},
		{
			name:  "text_after_block",
			input: `<|FunctionCallBegin|>stuff<|FunctionCallEnd|>继续`,
			want:  "继续",
		},
		{
			name:  "text_around_block",
			input: `前面<|FunctionCallBegin|>中间<|FunctionCallEnd|>后面`,
			want:  "前面后面",
		},
		{
			name:    "begin_only_sets_inblock",
			input:   `hello<|FunctionCallBegin|>partial`,
			want:    "hello",
			wantIn:  true,
		},
		{
			name:    "continue_inblock",
			input:   `still inside`,
			inBlock: true,
			want:    "",
			wantIn:  true,
		},
		{
			name:    "end_marker_resumes",
			input:   `tail<|FunctionCallEnd|>resumed`,
			inBlock: true,
			want:    "resumed",
			wantIn:  false,
		},
		{
			name:  "multiple_blocks",
			input: `a<|FunctionCallBegin|>x<|FunctionCallEnd|>b<|FunctionCallBegin|>y<|FunctionCallEnd|>c`,
			want:  "abc",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			inBlock := tt.inBlock
			got := filterFunctionCalls(tt.input, &inBlock)
			if got != tt.want {
				t.Errorf("filterFunctionCalls(%q) = %q, want %q", tt.input, got, tt.want)
			}
			if inBlock != tt.wantIn {
				t.Errorf("inBlock = %v, want %v", inBlock, tt.wantIn)
			}
		})
	}
}
