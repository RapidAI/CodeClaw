package main

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/RapidAI/CodeClaw/corelib"
	"github.com/RapidAI/CodeClaw/corelib/remote"
)

// ---------------------------------------------------------------------------
// SSH tool implementations for GUI IM handler.
// SSH sessions are registered as background tasks (SlotKindSSH) so the user
// can monitor them in the GUI "任务后台" panel without direct interaction.
// ---------------------------------------------------------------------------

// sshMgrOnce guards lazy initialisation of the SSH session manager.
var sshMgrOnce sync.Once

// ensureSSHManager lazily initialises the SSH session manager (thread-safe).
func (h *IMMessageHandler) ensureSSHManager() *remote.SSHSessionManager {
	sshMgrOnce.Do(func() {
		h.sshMgr = remote.NewSSHSessionManager(nil)

		// When an SSH session exits (abnormal disconnect, remote close, etc.)
		// automatically mark the corresponding background loop as completed.
		h.sshMgr.SetOnUpdate(func(sessionID string) {
			if h.bgManager == nil {
				return
			}
			// Check if the session has terminated.
			status, ok := h.sshMgr.GetSessionStatus(sessionID)
			if ok && (status == remote.SessionExited || status == remote.SessionError) {
				h.completeSSHBackgroundLoop(sessionID)
			}
			h.bgManager.NotifyChange()
		})
	})
	return h.sshMgr
}

// toolSSH is the unified SSH tool entry point, dispatching by action.
func (h *IMMessageHandler) toolSSH(args map[string]interface{}) string {
	action, _ := args["action"].(string)
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

func (h *IMMessageHandler) sshConnect(args map[string]interface{}) string {
	mgr := h.ensureSSHManager()

	host, _ := args["host"].(string)
	user, _ := args["user"].(string)
	label, _ := args["label"].(string)

	// Resolve pre-configured host by label.
	if (host == "" || user == "") && label != "" {
		if entry := h.resolveSSHHostByLabel(label); entry != nil {
			host = entry.Host
			user = entry.User
			if args["port"] == nil && entry.Port > 0 {
				args["port"] = float64(entry.Port)
			}
			if s, _ := args["auth_method"].(string); s == "" && entry.AuthMethod != "" {
				args["auth_method"] = entry.AuthMethod
			}
			if s, _ := args["key_path"].(string); s == "" && entry.KeyPath != "" {
				args["key_path"] = entry.KeyPath
			}
			label = entry.Label
		}
	}

	if host == "" || user == "" {
		return "错误: connect 需要 host 和 user 参数（或通过 label 引用已配置主机）"
	}

	port := 22
	if p, ok := args["port"].(float64); ok && p > 0 {
		port = int(p)
	}

	cfg := remote.SSHHostConfig{
		Host:       host,
		User:       user,
		Port:       port,
		AuthMethod: sshStrArg(args, "auth_method"),
		KeyPath:    sshStrArg(args, "key_path"),
		Password:   sshStrArg(args, "password"),
		Label:      label,
	}

	spec := remote.SSHSessionSpec{
		HostConfig:     cfg,
		InitialCommand: sshStrArg(args, "initial_command"),
		Cols:           120,
		Rows:           40,
	}

	session, err := mgr.Create(spec)
	if err != nil {
		return fmt.Sprintf("SSH 连接失败: %v", err)
	}

	// Register as a background task for GUI monitoring.
	h.registerSSHBackgroundLoop(session, cfg)

	// Wait for shell init.
	time.Sleep(2 * time.Second)

	preview := strings.Join(session.PreviewTail(20), "\n")

	result := fmt.Sprintf("✅ SSH 连接成功\n会话 ID: %s\n主机: %s\n状态: running",
		session.ID, cfg.SSHHostID())
	if preview != "" {
		result += "\n\n--- 初始输出 ---\n" + preview
	}
	return result
}

func (h *IMMessageHandler) sshExec(args map[string]interface{}) string {
	mgr := h.ensureSSHManager()

	sessionID, _ := args["session_id"].(string)
	command, _ := args["command"].(string)
	if sessionID == "" || command == "" {
		return "错误: exec 需要 session_id 和 command 参数"
	}

	session, ok := mgr.Get(sessionID)
	if !ok {
		return fmt.Sprintf("错误: SSH 会话 %s 不存在", sessionID)
	}

	reconnectNote := ""

	// 检查会话是否已断开，如果是则自动重连
	status, _ := mgr.GetSessionStatus(sessionID)
	sessionDead := status == remote.SessionExited || status == remote.SessionError

	if sessionDead {
		if err := mgr.ReconnectByID(sessionID); err != nil {
			return fmt.Sprintf("SSH 会话已断开，自动重连失败: %v", err)
		}
		reconnectNote = "⚠️ 连接已断开并自动重连\n"
		// 等 shell 初始化
		time.Sleep(2 * time.Second)
	}

	linesBefore := session.LineCount()

	if sessionDead {
		// 已经重连过，直接写入
		if err := mgr.WriteInput(sessionID, command); err != nil {
			return fmt.Sprintf("%s发送命令失败: %v", reconnectNote, err)
		}
	} else {
		// 使用带健康检查的写入，支持自动重连
		reconnected, err := mgr.WriteInputChecked(sessionID, command)
		if err != nil {
			return fmt.Sprintf("发送命令失败: %v", err)
		}
		if reconnected {
			reconnectNote = "⚠️ 连接已断开并自动重连\n"
			// 重连后等 shell 初始化，重新获取行号基线
			time.Sleep(2 * time.Second)
			linesBefore = session.LineCount()
		}
	}

	// 智能等待：检测输出稳定而非盲等
	waitSec := 5
	if w, ok := args["wait_seconds"].(float64); ok && w > 0 {
		waitSec = int(w)
	}
	if waitSec > 120 {
		waitSec = 120
	}
	maxWait := time.Duration(waitSec) * time.Second

	newLines, status := mgr.WaitForOutput(sessionID, linesBefore, maxWait)

	output := strings.Join(newLines, "\n")
	if output == "" {
		output = "(无新输出)"
	}
	if len(output) > 8000 {
		output = output[:4000] + "\n... (截断) ...\n" + output[len(output)-4000:]
	}

	// Update background loop iteration count.
	h.bumpSSHLoopIteration(sessionID)

	return fmt.Sprintf("%s[%s] 状态: %s\n$ %s\n%s", reconnectNote, sessionID, string(status), command, output)
}

func (h *IMMessageHandler) sshList() string {
	if h.sshMgr == nil {
		return "当前无活跃 SSH 会话"
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

func (h *IMMessageHandler) sshClose(args map[string]interface{}) string {
	if h.sshMgr == nil {
		return "错误: SSH 会话管理器未初始化"
	}

	sessionID, _ := args["session_id"].(string)
	if sessionID == "" {
		return "错误: close 需要 session_id 参数"
	}

	if err := h.sshMgr.Kill(sessionID); err != nil {
		return fmt.Sprintf("关闭失败: %v", err)
	}

	// Complete the corresponding background loop.
	h.completeSSHBackgroundLoop(sessionID)

	return fmt.Sprintf("✅ SSH 会话 %s 已关闭", sessionID)
}

// ---------------------------------------------------------------------------
// Background loop integration helpers
// ---------------------------------------------------------------------------

// registerSSHBackgroundLoop creates a BackgroundLoopManager entry for an SSH
// session so it appears in the GUI "任务后台" panel.
func (h *IMMessageHandler) registerSSHBackgroundLoop(session *remote.SSHManagedSession, cfg remote.SSHHostConfig) {
	if h.bgManager == nil {
		return
	}
	desc := fmt.Sprintf("SSH: %s", cfg.SSHHostID())
	if cfg.Label != "" {
		desc = fmt.Sprintf("SSH: %s (%s)", cfg.SSHHostID(), cfg.Label)
	}

	ctx := h.bgManager.Spawn(SlotKindSSH, "", desc, 0, nil)
	if ctx != nil {
		ctx.SessionID = session.ID
		ctx.SetState("running")
	}
}

// completeSSHBackgroundLoop marks the background loop as completed when the
// SSH session is closed or disconnected.
func (h *IMMessageHandler) completeSSHBackgroundLoop(sessionID string) {
	if h.bgManager == nil {
		return
	}
	for _, ctx := range h.bgManager.List() {
		if ctx.SlotKind == SlotKindSSH && ctx.SessionID == sessionID {
			h.bgManager.Complete(ctx.ID)
			return
		}
	}
}

// bumpSSHLoopIteration increments the iteration counter of the background
// loop associated with the given SSH session, giving the user a sense of
// activity in the "任务后台" panel.
func (h *IMMessageHandler) bumpSSHLoopIteration(sessionID string) {
	if h.bgManager == nil {
		return
	}
	for _, ctx := range h.bgManager.List() {
		if ctx.SlotKind == SlotKindSSH && ctx.SessionID == sessionID {
			ctx.IncrementIteration()
			h.bgManager.NotifyChange()
			return
		}
	}
}

// ---------------------------------------------------------------------------
// Config helpers
// ---------------------------------------------------------------------------

// resolveSSHHostByLabel looks up a pre-configured SSH host by label.
func (h *IMMessageHandler) resolveSSHHostByLabel(label string) *corelib.SSHHostEntry {
	hosts := h.loadSSHHosts()
	label = strings.ToLower(strings.TrimSpace(label))
	for i := range hosts {
		if strings.ToLower(hosts[i].Label) == label {
			return &hosts[i]
		}
	}
	// Fuzzy fallback: label contains keyword.
	for i := range hosts {
		if strings.Contains(strings.ToLower(hosts[i].Label), label) {
			return &hosts[i]
		}
	}
	return nil
}

func (h *IMMessageHandler) loadSSHHosts() []corelib.SSHHostEntry {
	cfg, err := h.app.LoadConfig()
	if err != nil {
		return nil
	}
	return cfg.SSHHosts
}

// sshStrArg extracts a string from an args map (SSH-tool-specific helper).
func sshStrArg(args map[string]interface{}, key string) string {
	s, _ := args[key].(string)
	return s
}
