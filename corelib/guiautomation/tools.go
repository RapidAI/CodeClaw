package guiautomation

import (
	"encoding/json"
	"fmt"

	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// RegisterTools registers all GUI automation tools into the given registry.
// It wires the recorder, replayer, input simulator, and screenshot function
// into tool handlers that the LLM can invoke.
// loopMgr, activityStore, and statusC are optional — when provided, gui_replay
// runs asynchronously in the background task list.
func RegisterTools(
	registry *tool.Registry,
	recorder *GUIRecorder,
	replayer *GUIReplayer,
	input InputSimulator,
	screenshotFn func() (string, error),
	loopMgr *agent.BackgroundLoopManager,
	activityStore GUIActivityUpdater,
	statusC chan agent.StatusEvent,
	logger func(string),
) {
	tools := []tool.RegisteredTool{
		{
			Name:        "gui_record_start",
			Description: "开始录制 GUI 操作流程。Start recording GUI operations on native desktop applications.",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"gui", "test", "automation", "桌面", "录制"},
			Priority:    5,
			InputSchema: map[string]interface{}{},
			Handler: func(args map[string]interface{}) string {
				if recorder == nil {
					return "GUI 录制器未初始化"
				}
				if err := recorder.Start(); err != nil {
					return fmt.Sprintf("开始录制失败: %s", err)
				}
				return "GUI 录制已开始。请执行操作，完成后调用 gui_record_stop 停止录制。"
			},
		},
		{
			Name:        "gui_record_stop",
			Description: "停止 GUI 录制并保存流程。Stop GUI recording and save the flow with a name and description.",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"gui", "test", "automation", "桌面", "录制"},
			Priority:    5,
			Required:    []string{"name"},
			InputSchema: map[string]interface{}{
				"name":        map[string]interface{}{"type": "string", "description": "流程名称 / Flow name"},
				"description": map[string]interface{}{"type": "string", "description": "流程描述 / Flow description"},
			},
			Handler: func(args map[string]interface{}) string {
				if recorder == nil {
					return "GUI 录制器未初始化"
				}
				name := strArg(args, "name", "")
				if name == "" {
					return "缺少 name 参数"
				}
				desc := strArg(args, "description", "")
				flow, err := recorder.Stop(name, desc)
				if err != nil {
					return fmt.Sprintf("停止录制失败: %s", err)
				}
				return fmt.Sprintf("录制已保存: %s (%d 步)", flow.Name, len(flow.Steps))
			},
		},
		{
			Name:        "gui_replay",
			Description: "重放已录制的 GUI 操作流程（后台异步执行）。Replay a previously recorded GUI flow by name, with optional parameter overrides. Runs asynchronously in background.",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"gui", "test", "automation", "桌面", "重放"},
			Priority:    5,
			Required:    []string{"flow_name"},
			InputSchema: map[string]interface{}{
				"flow_name": map[string]interface{}{"type": "string", "description": "要重放的流程名称 / Flow name to replay"},
				"overrides": map[string]interface{}{"type": "string", "description": "参数替换 JSON，如 {\"username\":\"admin\"} / Override values as JSON string"},
			},
			Handler: func(args map[string]interface{}) string {
				if replayer == nil || recorder == nil {
					return "GUI 重放器未初始化"
				}
				flowName := strArg(args, "flow_name", "")
				if flowName == "" {
					return "缺少 flow_name 参数"
				}
				flow, err := recorder.LoadFlow(flowName)
				if err != nil {
					return fmt.Sprintf("加载流程失败: %s", err)
				}
				var overrides map[string]string
				if raw := strArg(args, "overrides", ""); raw != "" {
					if err := json.Unmarshal([]byte(raw), &overrides); err != nil {
						return fmt.Sprintf("解析 overrides 失败: %s", err)
					}
				}

				// If BackgroundLoopManager is available, run async
				if loopMgr != nil {
					desc := fmt.Sprintf("GUI 回放: %s", flow.Name)
					loopCtx, waitCh := loopMgr.SpawnOrQueue(agent.SlotKindGUI, "", desc, 1)
					if loopCtx != nil {
						bgMgr := &bgGUILoopManagerAdapter{mgr: loopMgr}
						go RunGUIReplayInBackground(loopCtx, flow, overrides, replayer, activityStore, statusC, bgMgr, logger)
						result, _ := json.Marshal(map[string]interface{}{
							"status":  "submitted",
							"task_id": loopCtx.ID,
							"message": fmt.Sprintf("GUI 回放 [%s] 已提交后台执行", flow.Name),
						})
						return string(result)
					}
					// Slot full — queued
					queuePos := loopMgr.QueueLength(agent.SlotKindGUI)
					go func() {
						ctx := <-waitCh
						bgMgr := &bgGUILoopManagerAdapter{mgr: loopMgr}
						RunGUIReplayInBackground(ctx, flow, overrides, replayer, activityStore, statusC, bgMgr, logger)
					}()
					result, _ := json.Marshal(map[string]interface{}{
						"status":         "queued",
						"queue_position": queuePos,
						"message":        fmt.Sprintf("GUI slot 已满，回放 [%s] 已排队（位置 %d）", flow.Name, queuePos),
					})
					return string(result)
				}

				// Fallback: synchronous execution (no BackgroundLoopManager)
				state, err := replayer.Replay(flow, overrides)
				if err != nil {
					errResp := map[string]interface{}{"status": "failed", "error": err.Error()}
					if state != nil {
						errResp["step"] = state.CurrentStep
						errResp["total"] = state.TotalSteps
					}
					result, _ := json.Marshal(errResp)
					return string(result)
				}
				result, _ := json.Marshal(map[string]interface{}{
					"status": state.Status,
					"step":   state.CurrentStep,
					"total":  state.TotalSteps,
				})
				return string(result)
			},
		},
		{
			Name:        "gui_list_flows",
			Description: "列出所有已保存的 GUI 操作流程。List all saved GUI recorded flows.",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"gui", "test", "automation", "桌面"},
			Priority:    5,
			InputSchema: map[string]interface{}{},
			Handler: func(args map[string]interface{}) string {
				if recorder == nil {
					return "GUI 录制器未初始化"
				}
				flows, err := recorder.ListFlows()
				if err != nil {
					return fmt.Sprintf("列出流程失败: %s", err)
				}
				if len(flows) == 0 {
					return "无已保存的 GUI 流程"
				}
				var lines []string
				for _, f := range flows {
					lines = append(lines, fmt.Sprintf("  %s: %s (%d 步, 录制于 %s)",
						f.Name, f.Description, len(f.Steps), f.RecordedAt.Format("2006-01-02 15:04")))
				}
				return fmt.Sprintf("已保存的 GUI 流程 (%d 个):\n%s", len(flows), joinLines(lines))
			},
		},
		{
			Name:        "gui_click",
			Description: "在指定屏幕坐标执行鼠标点击。Click at the specified screen coordinates.",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"gui", "test", "automation", "桌面", "点击"},
			Priority:    5,
			Required:    []string{"x", "y"},
			InputSchema: map[string]interface{}{
				"x": map[string]interface{}{"type": "integer", "description": "X 坐标"},
				"y": map[string]interface{}{"type": "integer", "description": "Y 坐标"},
			},
			Handler: func(args map[string]interface{}) string {
				if input == nil {
					return "输入模拟器未初始化"
				}
				x := intArg(args, "x", 0)
				y := intArg(args, "y", 0)
				if err := input.Click(x, y); err != nil {
					return fmt.Sprintf("点击失败: %s", err)
				}
				return fmt.Sprintf("已点击坐标 (%d, %d)", x, y)
			},
		},
		{
			Name:        "gui_type",
			Description: "在指定坐标点击后输入文本。Click at coordinates then type text.",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"gui", "test", "automation", "桌面", "输入"},
			Priority:    5,
			Required:    []string{"x", "y", "text"},
			InputSchema: map[string]interface{}{
				"x":    map[string]interface{}{"type": "integer", "description": "X 坐标"},
				"y":    map[string]interface{}{"type": "integer", "description": "Y 坐标"},
				"text": map[string]interface{}{"type": "string", "description": "要输入的文本 / Text to type"},
			},
			Handler: func(args map[string]interface{}) string {
				if input == nil {
					return "输入模拟器未初始化"
				}
				x := intArg(args, "x", 0)
				y := intArg(args, "y", 0)
				text := strArg(args, "text", "")
				if text == "" {
					return "缺少 text 参数"
				}
				if err := input.Click(x, y); err != nil {
					return fmt.Sprintf("点击失败: %s", err)
				}
				if err := input.Type(text); err != nil {
					return fmt.Sprintf("输入失败: %s", err)
				}
				return fmt.Sprintf("已在 (%d, %d) 输入 %d 个字符", x, y, len([]rune(text)))
			},
		},
		{
			Name:        "gui_screenshot",
			Description: "截取当前桌面屏幕截图，返回 base64 编码的 PNG。Take a desktop screenshot, returns base64-encoded PNG.",
			Category:    tool.CategoryBuiltin,
			Tags:        []string{"gui", "test", "automation", "桌面", "截图"},
			Priority:    5,
			InputSchema: map[string]interface{}{},
			Handler: func(args map[string]interface{}) string {
				if screenshotFn == nil {
					return "截图功能未初始化"
				}
				data, err := screenshotFn()
				if err != nil {
					return fmt.Sprintf("截图失败: %s", err)
				}
				result, _ := json.Marshal(map[string]interface{}{
					"type":   "image",
					"format": "png",
					"base64": data,
				})
				return string(result)
			},
		},
	}

	for _, t := range tools {
		t.Status = tool.StatusAvailable
		t.Source = "builtin:gui_automation"
		registry.Register(t)
	}
}

// ── arg helpers (same pattern as browser/tools.go) ──

func strArg(args map[string]interface{}, key, fallback string) string {
	if v, ok := args[key].(string); ok && v != "" {
		return v
	}
	return fallback
}

func intArg(args map[string]interface{}, key string, fallback int) int {
	switch v := args[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case json.Number:
		if n, err := v.Int64(); err == nil {
			return int(n)
		}
	}
	return fallback
}

func joinLines(lines []string) string {
	result := ""
	for i, l := range lines {
		if i > 0 {
			result += "\n"
		}
		result += l
	}
	return result
}
