package nlrouter

import (
	"regexp"
	"strings"
	"unicode"
)

// RuleEngine performs keyword and pattern-based intent matching.
// It checks slash commands first (confidence 1.0), then keyword matches (0.8),
// then fuzzy/partial matches (0.6). Returns nil when nothing matches.
type RuleEngine struct {
	slashCommands  []slashRule
	keywordRules   []keywordRule
	toolPattern    *regexp.Regexp
	sessionPattern *regexp.Regexp
	pathPattern    *regexp.Regexp
}

type slashRule struct {
	prefix string
	intent string
	hasArg bool // whether the command expects an argument after the prefix
}

type keywordRule struct {
	keywords []string
	intent   string
}

// NewRuleEngine creates a RuleEngine with all built-in rules.
func NewRuleEngine() *RuleEngine {
	re := &RuleEngine{}
	re.initSlashCommands()
	re.initKeywordRules()
	re.initPatterns()
	return re
}

func (re *RuleEngine) initSlashCommands() {
	re.slashCommands = []slashRule{
		{prefix: "/help", intent: IntentHelp},
		{prefix: "/machines", intent: IntentListMachines},
		{prefix: "/sessions", intent: IntentListSessions},
		{prefix: "/use", intent: IntentUseSession, hasArg: true},
		{prefix: "/exit", intent: IntentExitSession},
		{prefix: "/kill", intent: IntentKillSession, hasArg: true},
		{prefix: "/screenshot", intent: IntentScreenshot},
		{prefix: "/memory", intent: IntentViewMemory},
		{prefix: "/clear_memory", intent: IntentClearMemory},
	}
}

func (re *RuleEngine) initKeywordRules() {
	re.keywordRules = []keywordRule{
		{keywords: []string{"查看设备", "设备列表", "list machines", "show machines"}, intent: IntentListMachines},
		{keywords: []string{"查看会话", "会话列表", "list sessions", "show sessions"}, intent: IntentListSessions},
		{keywords: []string{"会话详情", "session detail"}, intent: IntentSessionDetail},
		{keywords: []string{"切换会话", "使用会话", "use session"}, intent: IntentUseSession},
		{keywords: []string{"启动会话", "新建会话", "launch", "start session"}, intent: IntentLaunchSession},
		{keywords: []string{"发送", "send"}, intent: IntentSendInput},
		{keywords: []string{"中断", "interrupt"}, intent: IntentInterruptSession},
		{keywords: []string{"终止", "kill", "杀掉"}, intent: IntentKillSession},
		{keywords: []string{"截屏", "截图", "screenshot"}, intent: IntentScreenshot},
		{keywords: []string{"帮助", "help"}, intent: IntentHelp},
		{keywords: []string{"退出", "exit"}, intent: IntentExitSession},
		{keywords: []string{"调用工具", "call tool", "mcp"}, intent: IntentCallMCPTool},
		{keywords: []string{"执行技能", "run skill", "skill"}, intent: IntentRunSkill},
		{keywords: []string{"查看记忆", "view memory", "我的偏好"}, intent: IntentViewMemory},
		{keywords: []string{"清除记忆", "clear memory"}, intent: IntentClearMemory},
		{keywords: []string{"保存技能", "沉淀技能", "crystallize"}, intent: IntentCrystallizeSkill},
	}
}

func (re *RuleEngine) initPatterns() {
	// Tool name extraction: "用 claude" / "with claude" / tool names directly
	re.toolPattern = regexp.MustCompile(`(?i)(?:用|with|使用)\s*(claude|codex|gemini|cursor|kilo|opencode)`)
	// Session ID extraction: numeric IDs or short alphanumeric IDs
	re.sessionPattern = regexp.MustCompile(`(?i)(?:会话|session)?\s*#?(\d+|[a-z]\w{2,7})`)
	// Path extraction: paths starting with / or ~
	re.pathPattern = regexp.MustCompile(`((?:/|~/)[\w./-]+)`)
}

// Match parses text and returns the best matching Intent, or nil if no match.
func (re *RuleEngine) Match(text string) *Intent {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}

	lower := strings.ToLower(text)

	// 1. Slash commands — confidence 1.0
	if strings.HasPrefix(lower, "/") {
		if intent := re.matchSlashCommand(text, lower); intent != nil {
			return intent
		}
	}

	// 2. Keyword matching — confidence 0.8
	if intent := re.matchKeywords(text, lower); intent != nil {
		return intent
	}

	// 3. Fuzzy / partial matching — confidence 0.6
	if intent := re.matchFuzzy(text, lower); intent != nil {
		return intent
	}

	return nil
}

// matchSlashCommand checks for exact slash command matches.
func (re *RuleEngine) matchSlashCommand(raw, lower string) *Intent {
	for _, cmd := range re.slashCommands {
		if !strings.HasPrefix(lower, cmd.prefix) {
			continue
		}

		rest := raw[len(cmd.prefix):]
		// Must be exact match or followed by whitespace
		if rest != "" && !startsWithSpace(rest) {
			continue
		}

		intent := &Intent{
			Name:       cmd.intent,
			Confidence: 1.0,
			Params:     make(map[string]interface{}),
			RawText:    raw,
		}

		if cmd.hasArg {
			arg := strings.TrimSpace(rest)
			if arg != "" {
				intent.Params["id"] = arg
			}
		}

		return intent
	}
	return nil
}

// matchKeywords checks for keyword-based matches.
func (re *RuleEngine) matchKeywords(raw, lower string) *Intent {
	var best *Intent
	var bestLen int

	for _, rule := range re.keywordRules {
		for _, kw := range rule.keywords {
			if !strings.Contains(lower, kw) {
				continue
			}
			// Prefer longer keyword matches for specificity
			if best == nil || len(kw) > bestLen {
				intent := &Intent{
					Name:       rule.intent,
					Confidence: 0.8,
					Params:     make(map[string]interface{}),
					RawText:    raw,
				}
				best = intent
				bestLen = len(kw)
			}
		}
	}

	if best != nil {
		re.extractParams(raw, best)
		return best
	}
	return nil
}

// matchFuzzy performs partial/fuzzy matching for less precise inputs.
func (re *RuleEngine) matchFuzzy(raw, lower string) *Intent {
	// Fuzzy rules: single-character or partial Chinese keywords
	fuzzyMap := map[string]string{
		"设备":   IntentListMachines,
		"会话":   IntentListSessions,
		"machine": IntentListMachines,
		"session": IntentListSessions,
		"截":     IntentScreenshot,
		"帮":     IntentHelp,
	}

	for kw, intentName := range fuzzyMap {
		if strings.Contains(lower, kw) {
			intent := &Intent{
				Name:       intentName,
				Confidence: 0.6,
				Params:     make(map[string]interface{}),
				RawText:    raw,
			}
			re.extractParams(raw, intent)
			return intent
		}
	}
	return nil
}

// extractParams extracts tool names, session IDs, and project paths from text.
func (re *RuleEngine) extractParams(raw string, intent *Intent) {
	// Extract tool name
	if matches := re.toolPattern.FindStringSubmatch(raw); len(matches) > 1 {
		intent.Params["tool"] = strings.ToLower(matches[1])
	}

	// Extract session ID (only for session-related intents)
	switch intent.Name {
	case IntentUseSession, IntentKillSession, IntentSessionDetail:
		if matches := re.sessionPattern.FindStringSubmatch(raw); len(matches) > 1 {
			intent.Params["id"] = matches[1]
		}
	}

	// Extract project path
	if matches := re.pathPattern.FindStringSubmatch(raw); len(matches) > 1 {
		intent.Params["path"] = matches[1]
	}
}

// startsWithSpace returns true if s starts with a whitespace character.
func startsWithSpace(s string) bool {
	for _, r := range s {
		return unicode.IsSpace(r)
	}
	return false
}
