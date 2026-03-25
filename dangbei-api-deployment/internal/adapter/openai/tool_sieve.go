package openai

import (
	"strings"

	"ds2api/internal/util"
)

type toolStreamSieveState struct {
	pending        strings.Builder
	capture        strings.Builder
	capturing      bool
	recentTextTail string
	toolNameSent   bool
	toolName       string
	toolArgsStart  int
	toolArgsSent   int
	toolArgsString bool
	toolArgsDone   bool
}

type toolStreamEvent struct {
	Content        string
	ToolCalls      []util.ParsedToolCall
	ToolCallDeltas []toolCallDelta
}

type toolCallDelta struct {
	Index     int
	Name      string
	Arguments string
}

const toolSieveCaptureLimit = 8 * 1024
const toolSieveContextTailLimit = 256

func (s *toolStreamSieveState) resetIncrementalToolState() {
	s.toolNameSent = false
	s.toolName = ""
	s.toolArgsStart = -1
	s.toolArgsSent = -1
	s.toolArgsString = false
	s.toolArgsDone = false
}

func processToolSieveChunk(state *toolStreamSieveState, chunk string, toolNames []string) []toolStreamEvent {
	if state == nil {
		return nil
	}
	if chunk != "" {
		state.pending.WriteString(chunk)
	}
	events := make([]toolStreamEvent, 0, 2)

	for {
		if state.capturing {
			if state.pending.Len() > 0 {
				state.capture.WriteString(state.pending.String())
				state.pending.Reset()
			}
			if deltas := buildIncrementalToolDeltas(state); len(deltas) > 0 {
				events = append(events, toolStreamEvent{ToolCallDeltas: deltas})
			}
			prefix, calls, suffix, ready := consumeToolCapture(state, toolNames)
			if !ready {
				if state.capture.Len() > toolSieveCaptureLimit {
					content := state.capture.String()
					state.capture.Reset()
					state.capturing = false
					state.resetIncrementalToolState()
					state.noteText(content)
					events = append(events, toolStreamEvent{Content: content})
					continue
				}
				break
			}
			state.capture.Reset()
			state.capturing = false
			state.resetIncrementalToolState()
			if prefix != "" {
				state.noteText(prefix)
				events = append(events, toolStreamEvent{Content: prefix})
			}
			if len(calls) > 0 {
				events = append(events, toolStreamEvent{ToolCalls: calls})
			}
			if suffix != "" {
				state.pending.WriteString(suffix)
			}
			continue
		}

		pending := state.pending.String()
		if pending == "" {
			break
		}
		start := findToolSegmentStart(pending)
		if start >= 0 {
			prefix := pending[:start]
			if prefix != "" {
				state.noteText(prefix)
				events = append(events, toolStreamEvent{Content: prefix})
			}
			state.pending.Reset()
			state.capture.WriteString(pending[start:])
			state.capturing = true
			state.resetIncrementalToolState()
			continue
		}

		safe, hold := splitSafeContentForToolDetection(pending)
		if safe == "" {
			break
		}
		state.pending.Reset()
		state.pending.WriteString(hold)
		state.noteText(safe)
		events = append(events, toolStreamEvent{Content: safe})
	}

	return events
}

func flushToolSieve(state *toolStreamSieveState, toolNames []string) []toolStreamEvent {
	if state == nil {
		return nil
	}
	events := processToolSieveChunk(state, "", toolNames)
	if state.capturing {
		consumedPrefix, consumedCalls, consumedSuffix, ready := consumeToolCapture(state, toolNames)
		if ready {
			if consumedPrefix != "" {
				state.noteText(consumedPrefix)
				events = append(events, toolStreamEvent{Content: consumedPrefix})
			}
			if len(consumedCalls) > 0 {
				events = append(events, toolStreamEvent{ToolCalls: consumedCalls})
			}
			if consumedSuffix != "" {
				state.noteText(consumedSuffix)
				events = append(events, toolStreamEvent{Content: consumedSuffix})
			}
		} else {
			content := state.capture.String()
			if content != "" {
				state.noteText(content)
				events = append(events, toolStreamEvent{Content: content})
			}
		}
		state.capture.Reset()
		state.capturing = false
		state.resetIncrementalToolState()
	}
	if state.pending.Len() > 0 {
		content := state.pending.String()
		state.noteText(content)
		events = append(events, toolStreamEvent{Content: content})
		state.pending.Reset()
	}
	return events
}

func splitSafeContentForToolDetection(s string) (safe, hold string) {
	if s == "" {
		return "", ""
	}
	suspiciousStart := findSuspiciousPrefixStart(s)
	if suspiciousStart < 0 {
		return s, ""
	}
	if suspiciousStart > 0 {
		return s[:suspiciousStart], s[suspiciousStart:]
	}
	// If suspicious content starts at position 0, keep holding until we can
	// parse a complete tool JSON block or reach stream flush.
	return "", s
}

func findSuspiciousPrefixStart(s string) int {
	start := -1
	indices := []int{
		strings.LastIndex(s, "{"),
		strings.LastIndex(s, "["),
		strings.LastIndex(s, "```"),
	}
	for _, idx := range indices {
		if idx > start {
			start = idx
		}
	}
	return start
}

func findToolSegmentStart(s string) int {
	if s == "" {
		return -1
	}
	lower := strings.ToLower(s)
	offset := 0
	for {
		keyRel := strings.Index(lower[offset:], "tool_calls")
		if keyRel < 0 {
			return -1
		}
		keyIdx := offset + keyRel
		start := strings.LastIndex(s[:keyIdx], "{")
		if start < 0 {
			start = keyIdx
		}
		if !insideCodeFence(s[:start]) {
			return start
		}
		offset = keyIdx + len("tool_calls")
	}
}

func consumeToolCapture(state *toolStreamSieveState, toolNames []string) (prefix string, calls []util.ParsedToolCall, suffix string, ready bool) {
	captured := state.capture.String()
	if captured == "" {
		return "", nil, "", false
	}
	lower := strings.ToLower(captured)
	keyIdx := strings.Index(lower, "tool_calls")
	if keyIdx < 0 {
		return "", nil, "", false
	}
	start := strings.LastIndex(captured[:keyIdx], "{")
	if start < 0 {
		return "", nil, "", false
	}
	obj, end, ok := extractJSONObjectFrom(captured, start)
	if !ok {
		return "", nil, "", false
	}
	prefixPart := captured[:start]
	suffixPart := captured[end:]
	if insideCodeFence(state.recentTextTail + prefixPart) {
		return captured, nil, "", true
	}
	parsed := util.ParseStandaloneToolCalls(obj, toolNames)
	if len(parsed) == 0 {
		if state.toolNameSent {
			return prefixPart, nil, suffixPart, true
		}
		return captured, nil, "", true
	}
	if state.toolNameSent {
		if len(parsed) > 1 {
			return prefixPart, parsed[1:], suffixPart, true
		}
		return prefixPart, nil, suffixPart, true
	}
	return prefixPart, parsed, suffixPart, true
}

func extractJSONObjectFrom(text string, start int) (string, int, bool) {
	if start < 0 || start >= len(text) || text[start] != '{' {
		return "", 0, false
	}
	depth := 0
	quote := byte(0)
	escaped := false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == '{' {
			depth++
			continue
		}
		if ch == '}' {
			depth--
			if depth == 0 {
				end := i + 1
				return text[start:end], end, true
			}
		}
	}
	return "", 0, false
}

func buildIncrementalToolDeltas(state *toolStreamSieveState) []toolCallDelta {
	captured := state.capture.String()
	if captured == "" {
		return nil
	}
	lower := strings.ToLower(captured)
	keyIdx := strings.Index(lower, "tool_calls")
	if keyIdx < 0 {
		return nil
	}
	start := strings.LastIndex(captured[:keyIdx], "{")
	if start < 0 {
		return nil
	}
	if insideCodeFence(state.recentTextTail + captured[:start]) {
		return nil
	}
	callStart, ok := findFirstToolCallObjectStart(captured, keyIdx)
	if !ok {
		return nil
	}
	deltas := make([]toolCallDelta, 0, 2)
	if state.toolName == "" {
		name, ok := extractToolCallName(captured, callStart)
		if !ok || name == "" {
			return nil
		}
		state.toolName = name
	}
	if state.toolArgsStart < 0 {
		argsStart, stringMode, ok := findToolCallArgsStart(captured, callStart)
		if ok {
			state.toolArgsString = stringMode
			if stringMode {
				state.toolArgsStart = argsStart + 1
			} else {
				state.toolArgsStart = argsStart
			}
			state.toolArgsSent = state.toolArgsStart
		}
	}
	if !state.toolNameSent {
		if state.toolArgsStart < 0 {
			return nil
		}
		state.toolNameSent = true
		deltas = append(deltas, toolCallDelta{Index: 0, Name: state.toolName})
	}
	if state.toolArgsStart < 0 || state.toolArgsDone {
		return deltas
	}
	end, complete, ok := scanToolCallArgsProgress(captured, state.toolArgsStart, state.toolArgsString)
	if !ok {
		return deltas
	}
	if end > state.toolArgsSent {
		deltas = append(deltas, toolCallDelta{
			Index:     0,
			Arguments: captured[state.toolArgsSent:end],
		})
		state.toolArgsSent = end
	}
	if complete {
		state.toolArgsDone = true
	}
	return deltas
}

func findFirstToolCallObjectStart(text string, keyIdx int) (int, bool) {
	arrStart, ok := findToolCallsArrayStart(text, keyIdx)
	if !ok {
		return -1, false
	}
	i := skipSpaces(text, arrStart+1)
	if i >= len(text) || text[i] != '{' {
		return -1, false
	}
	return i, true
}

func findToolCallsArrayStart(text string, keyIdx int) (int, bool) {
	i := keyIdx + len("tool_calls")
	for i < len(text) && text[i] != ':' {
		i++
	}
	if i >= len(text) {
		return -1, false
	}
	i = skipSpaces(text, i+1)
	if i >= len(text) || text[i] != '[' {
		return -1, false
	}
	return i, true
}

func extractToolCallName(text string, callStart int) (string, bool) {
	valueStart, ok := findObjectFieldValueStart(text, callStart, []string{"name"})
	if !ok || valueStart >= len(text) || text[valueStart] != '"' {
		fnStart, fnOK := findFunctionObjectStart(text, callStart)
		if !fnOK {
			return "", false
		}
		valueStart, ok = findObjectFieldValueStart(text, fnStart, []string{"name"})
		if !ok || valueStart >= len(text) || text[valueStart] != '"' {
			return "", false
		}
	}
	name, _, ok := parseJSONStringLiteral(text, valueStart)
	if !ok {
		return "", false
	}
	return name, true
}

func findToolCallArgsStart(text string, callStart int) (int, bool, bool) {
	keys := []string{"input", "arguments", "args", "parameters", "params"}
	valueStart, ok := findObjectFieldValueStart(text, callStart, keys)
	if !ok {
		fnStart, fnOK := findFunctionObjectStart(text, callStart)
		if !fnOK {
			return -1, false, false
		}
		valueStart, ok = findObjectFieldValueStart(text, fnStart, keys)
		if !ok {
			return -1, false, false
		}
	}
	if valueStart >= len(text) {
		return -1, false, false
	}
	ch := text[valueStart]
	if ch == '{' || ch == '[' {
		return valueStart, false, true
	}
	if ch == '"' {
		return valueStart, true, true
	}
	return -1, false, false
}

func scanToolCallArgsProgress(text string, start int, stringMode bool) (int, bool, bool) {
	if start < 0 || start > len(text) {
		return 0, false, false
	}
	if stringMode {
		escaped := false
		for i := start; i < len(text); i++ {
			ch := text[i]
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == '"' {
				return i, true, true
			}
		}
		return len(text), false, true
	}
	if start >= len(text) {
		return start, false, false
	}
	if text[start] != '{' && text[start] != '[' {
		return 0, false, false
	}
	depth := 0
	quote := byte(0)
	escaped := false
	for i := start; i < len(text); i++ {
		ch := text[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			quote = ch
			continue
		}
		if ch == '{' || ch == '[' {
			depth++
			continue
		}
		if ch == '}' || ch == ']' {
			depth--
			if depth == 0 {
				return i + 1, true, true
			}
		}
	}
	return len(text), false, true
}

func findObjectFieldValueStart(text string, objStart int, keys []string) (int, bool) {
	if objStart < 0 || objStart >= len(text) || text[objStart] != '{' {
		return 0, false
	}
	depth := 0
	quote := byte(0)
	escaped := false
	for i := objStart; i < len(text); i++ {
		ch := text[i]
		if quote != 0 {
			if escaped {
				escaped = false
				continue
			}
			if ch == '\\' {
				escaped = true
				continue
			}
			if ch == quote {
				quote = 0
			}
			continue
		}
		if ch == '"' || ch == '\'' {
			if depth == 1 {
				key, end, ok := parseJSONStringLiteral(text, i)
				if !ok {
					return 0, false
				}
				j := skipSpaces(text, end)
				if j >= len(text) || text[j] != ':' {
					i = end - 1
					continue
				}
				j = skipSpaces(text, j+1)
				if j >= len(text) {
					return 0, false
				}
				if containsKey(keys, key) {
					return j, true
				}
				i = j - 1
				continue
			}
			quote = ch
			continue
		}
		if ch == '{' {
			depth++
			continue
		}
		if ch == '}' {
			depth--
			if depth == 0 {
				break
			}
		}
	}
	return 0, false
}

func findFunctionObjectStart(text string, callStart int) (int, bool) {
	valueStart, ok := findObjectFieldValueStart(text, callStart, []string{"function"})
	if !ok || valueStart >= len(text) || text[valueStart] != '{' {
		return -1, false
	}
	return valueStart, true
}

func parseJSONStringLiteral(text string, start int) (string, int, bool) {
	if start < 0 || start >= len(text) || text[start] != '"' {
		return "", 0, false
	}
	var b strings.Builder
	escaped := false
	for i := start + 1; i < len(text); i++ {
		ch := text[i]
		if escaped {
			b.WriteByte(ch)
			escaped = false
			continue
		}
		if ch == '\\' {
			escaped = true
			continue
		}
		if ch == '"' {
			return b.String(), i + 1, true
		}
		b.WriteByte(ch)
	}
	return "", 0, false
}

func containsKey(keys []string, value string) bool {
	for _, k := range keys {
		if k == value {
			return true
		}
	}
	return false
}

func skipSpaces(text string, i int) int {
	for i < len(text) {
		switch text[i] {
		case ' ', '\t', '\n', '\r':
			i++
		default:
			return i
		}
	}
	return i
}

func (s *toolStreamSieveState) noteText(content string) {
	if strings.TrimSpace(content) == "" {
		return
	}
	s.recentTextTail = appendTail(s.recentTextTail, content, toolSieveContextTailLimit)
}

func appendTail(prev, next string, max int) string {
	if max <= 0 {
		return ""
	}
	combined := prev + next
	if len(combined) <= max {
		return combined
	}
	return combined[len(combined)-max:]
}

func looksLikeToolExampleContext(text string) bool {
	return insideCodeFence(text)
}

func insideCodeFence(text string) bool {
	if text == "" {
		return false
	}
	return strings.Count(text, "```")%2 == 1
}
