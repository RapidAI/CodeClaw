package nlrouter

// Intent 表示解析出的用户意图
type Intent struct {
	Name       string                 // 意图名称
	Confidence float64                // 置信度 0.0-1.0
	Params     map[string]interface{} // 提取的参数
	RawText    string                 // 原始文本
	Candidates []Intent               // 候选意图列表
}

// 支持的核心意图常量
const (
	IntentListMachines     = "list_machines"
	IntentListSessions     = "list_sessions"
	IntentSessionDetail    = "session_detail"
	IntentUseSession       = "use_session"
	IntentSendInput        = "send_input"
	IntentInterruptSession = "interrupt_session"
	IntentKillSession      = "kill_session"
	IntentLaunchSession    = "launch_session"
	IntentScreenshot       = "screenshot"
	IntentHelp             = "help"
	IntentExitSession      = "exit_session"
	IntentCallMCPTool      = "call_mcp_tool"
	IntentRunSkill         = "run_skill"
	IntentViewMemory       = "view_memory"
	IntentClearMemory      = "clear_memory"
	IntentCrystallizeSkill = "crystallize_skill"
	IntentUnknown          = "unknown"
)
