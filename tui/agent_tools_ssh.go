package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
	"github.com/RapidAI/CodeClaw/tui/commands"
)

// sshToolDefinitions 返回统一的 SSH 工具定义（单工具，action 分发）。
func sshToolDefinitions() []map[string]interface{} {
	return []map[string]interface{}{
		toolDef("ssh", "SSH 远程服务器管理（connect/exec/list/close）", map[string]interface{}{
			"action":          map[string]interface{}{"type": "string", "description": "操作: connect/exec/list/close"},
			"host":            map[string]interface{}{"type": "string", "description": "远程主机地址（connect 时必填）"},
			"user":            map[string]interface{}{"type": "string", "description": "登录用户名（connect 时必填）"},
			"port":            map[string]interface{}{"type": "integer", "description": "SSH 端口（默认 22）"},
			"auth_method":     map[string]interface{}{"type": "string", "description": "认证方式: key/password/agent（默认 key）"},
			"key_path":        map[string]interface{}{"type": "string", "description": "私钥路径（可选）"},
			"password":        map[string]interface{}{"type": "string", "description": "密码（可选）"},
			"label":           map[string]interface{}{"type": "string", "description": "主机标签（可选，如 prod-web-01）"},
			"initial_command": map[string]interface{}{"type": "string", "description": "连接后立即执行的命令（可选）"},
			"session_id":      map[string]interface{}{"type": "string", "description": "SSH 会话 ID（exec/close 时必填）"},
			"command":         map[string]interface{}{"type": "string", "description": "要执行的命令（exec 时必填）"},
			"wait_seconds":    map[string]interface{}{"type": "integer", "description": "等待输出秒数（exec 时可选，默认 5）"},
		}, []string{"action"}),
	}
}

// toolSSH 统一 SSH 工具入口，按 action 分发。
func (h *TUIAgentHandler) toolSSH(args map[string]interface{}) string {
	action := stringArg(args, "action")
	switch action {
	case "connect":
		return h.sshConnect(args)
	case "exec":
		return h.sshExec(args)
	case "list":
		return h.sshList()
	case "close":
		return h.sshClose(args)
	default:
		return fmt.Sprintf("未知 SSH 操作: %s（支持: connect/exec/list/close）", action)
	}
}

func (h *TUIAgentHandler) sshConnect(args map[string]interface{}) string {
	if h.sshMgr == nil {
		return "错误: SSH 会话管理器未初始化"
	}

	host := stringArg(args, "host")
	user := stringArg(args, "user")
	label := stringArg(args, "label")

	// 支持通过标签名引用预配置主机
	if (host == "" || user == "") && label != "" {
		if entry := h.resolveSSHHostByLabel(label); entry != nil {
			host = entry.Host
			user = entry.User
			if args["port"] == nil && entry.Port > 0 {
				args["port"] = float64(entry.Port)
			}
			if stringArg(args, "auth_method") == "" && entry.AuthMethod != "" {
				args["auth_method"] = entry.AuthMethod
			}
			if stringArg(args, "key_path") == "" && entry.KeyPath != "" {
				args["key_path"] = entry.KeyPath
			}
			label = entry.Label
		}
	}

	if host == "" || user == "" {
		return "错误: connect 需要 host 和 user 参数（或通过 label 引用已配置主机）"
	}

	cfg := remote.SSHHostConfig{
		Host:       host,
		User:       user,
		Port:       intArg(args, "port", 22),
		AuthMethod: stringArg(args, "auth_method"),
		KeyPath:    stringArg(args, "key_path"),
		Password:   stringArg(args, "password"),
		Label:      label,
	}

	spec := remote.SSHSessionSpec{
		HostConfig:     cfg,
		InitialCommand: stringArg(args, "initial_command"),
		Cols:           120,
		Rows:           40,
	}

	session, err := h.sshMgr.Create(spec)
	if err != nil {
		return fmt.Sprintf("SSH 连接失败: %v", err)
	}

	// 等 shell 初始化
	time.Sleep(2 * time.Second)

	preview := strings.Join(session.PreviewTail(20), "\n")

	result := fmt.Sprintf("✅ SSH 连接成功\n会话 ID: %s\n主机: %s\n状态: running",
		session.ID, cfg.SSHHostID())
	if preview != "" {
		result += "\n\n--- 初始输出 ---\n" + preview
	}
	return result
}

func (h *TUIAgentHandler) sshExec(args map[string]interface{}) string {
	if h.sshMgr == nil {
		return "错误: SSH 会话管理器未初始化"
	}

	sessionID := stringArg(args, "session_id")
	command := stringArg(args, "command")
	if sessionID == "" || command == "" {
		return "错误: exec 需要 session_id 和 command 参数"
	}

	session, ok := h.sshMgr.Get(sessionID)
	if !ok {
		return fmt.Sprintf("错误: SSH 会话 %s 不存在", sessionID)
	}

	linesBefore := session.LineCount()

	if err := h.sshMgr.WriteInput(sessionID, command); err != nil {
		return fmt.Sprintf("发送命令失败: %v", err)
	}

	waitSec := intArg(args, "wait_seconds", 5)
	if waitSec <= 0 {
		waitSec = 5
	}
	if waitSec > 60 {
		waitSec = 60
	}
	time.Sleep(time.Duration(waitSec) * time.Second)

	newLines, status := session.NewLinesSince(linesBefore)

	output := strings.Join(newLines, "\n")
	if output == "" {
		output = "(无新输出)"
	}
	if len(output) > 8000 {
		output = output[:4000] + "\n... (截断) ...\n" + output[len(output)-4000:]
	}

	return fmt.Sprintf("[%s] 状态: %s\n$ %s\n%s", sessionID, string(status), command, output)
}

func (h *TUIAgentHandler) sshList() string {
	if h.sshMgr == nil {
		return "当前无活跃 SSH 会话（管理器未初始化）"
	}

	sessions := h.sshMgr.List()
	if len(sessions) == 0 {
		return "当前无活跃 SSH 会话"
	}

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("SSH 会话（%d 个）:\n", len(sessions)))
	for _, s := range sessions {
		summary := s.GetSummary()
		sb.WriteString(fmt.Sprintf("  - %s | %s | 状态: %s\n",
			s.ID, summary.HostLabel, summary.Status))
	}

	poolStats := h.sshMgr.Pool().Stats()
	if len(poolStats) > 0 {
		sb.WriteString("连接池:\n")
		for hostID, ref := range poolStats {
			sb.WriteString(fmt.Sprintf("  - %s (引用: %d)\n", hostID, ref))
		}
	}
	return sb.String()
}

func (h *TUIAgentHandler) sshClose(args map[string]interface{}) string {
	if h.sshMgr == nil {
		return "错误: SSH 会话管理器未初始化"
	}

	sessionID := stringArg(args, "session_id")
	if sessionID == "" {
		return "错误: close 需要 session_id 参数"
	}

	if err := h.sshMgr.Kill(sessionID); err != nil {
		return fmt.Sprintf("关闭失败: %v", err)
	}
	return fmt.Sprintf("✅ SSH 会话 %s 已关闭", sessionID)
}

// sshSessionsToJSON 序列化 SSH 会话列表（供其他模块使用）。
func sshSessionsToJSON(sessions []*remote.SSHManagedSession) string {
	summaries := make([]remote.SSHSessionSummary, 0, len(sessions))
	for _, s := range sessions {
		summaries = append(summaries, s.GetSummary())
	}
	data, _ := json.Marshal(summaries)
	return string(data)
}

// loadSSHHosts 从配置中加载预配置的 SSH 主机列表。
func (h *TUIAgentHandler) loadSSHHosts() []corelib.SSHHostEntry {
	store := commands.NewFileConfigStore(commands.ResolveDataDir())
	cfg, err := store.LoadConfig()
	if err != nil {
		return nil
	}
	return cfg.SSHHosts
}

// resolveSSHHostByLabel 根据标签名查找预配置的 SSH 主机。
func (h *TUIAgentHandler) resolveSSHHostByLabel(label string) *corelib.SSHHostEntry {
	hosts := h.loadSSHHosts()
	label = strings.ToLower(strings.TrimSpace(label))
	for i := range hosts {
		if strings.ToLower(hosts[i].Label) == label {
			return &hosts[i]
		}
	}
	// 模糊匹配：标签包含关键词
	for i := range hosts {
		if strings.Contains(strings.ToLower(hosts[i].Label), label) {
			return &hosts[i]
		}
	}
	return nil
}
