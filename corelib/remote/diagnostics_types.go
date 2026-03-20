package remote

// RemoteToolLaunchProbeResult 工具启动探测结果。
type RemoteToolLaunchProbeResult struct {
	Tool        string `json:"tool"`
	Supported   bool   `json:"supported"`
	Ready       bool   `json:"ready"`
	Message     string `json:"message"`
	CommandPath string `json:"command_path"`
	ProjectPath string `json:"project_path"`
}

// RemoteToolReadiness 工具就绪状态。
type RemoteToolReadiness struct {
	Tool            string   `json:"tool"`
	Ready           bool     `json:"ready"`
	RemoteEnabled   bool     `json:"remote_enabled"`
	ToolInstalled   bool     `json:"tool_installed"`
	ModelConfigured bool     `json:"model_configured"`
	ProjectPath     string   `json:"project_path"`
	ToolPath        string   `json:"tool_path"`
	CommandPath     string   `json:"command_path"`
	HubURL          string   `json:"hub_url"`
	PTYSupported    bool     `json:"pty_supported"`
	PTYMessage      string   `json:"pty_message"`
	SelectedModel   string   `json:"selected_model"`
	SelectedModelID string   `json:"selected_model_id"`
	Issues          []string `json:"issues"`
	Warnings        []string `json:"warnings"`
}

// RemotePTYProbeResult PTY 探测结果。
type RemotePTYProbeResult struct {
	Supported bool   `json:"supported"`
	Ready     bool   `json:"ready"`
	Message   string `json:"message"`
}

// RemoteClaudeLaunchProbeResult is an alias for RemoteToolLaunchProbeResult.
type RemoteClaudeLaunchProbeResult = RemoteToolLaunchProbeResult

// RemoteClaudeReadiness is an alias for RemoteToolReadiness.
type RemoteClaudeReadiness = RemoteToolReadiness
