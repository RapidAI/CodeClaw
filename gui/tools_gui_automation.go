package main

import (
	"fmt"
	"log"

	"github.com/RapidAI/CodeClaw/corelib/accessibility"
	"github.com/RapidAI/CodeClaw/corelib/agent"
	"github.com/RapidAI/CodeClaw/corelib/guiautomation"
	"github.com/RapidAI/CodeClaw/corelib/tool"
)

// guiReplayActivityAdapter wraps AgentActivityStore to satisfy guiautomation.GUIActivityUpdater.
type guiReplayActivityAdapter struct {
	store *AgentActivityStore
}

func (a *guiReplayActivityAdapter) UpdateReplay(flowName string, currentStep, totalSteps int, status string) {
	a.store.Update(&AgentActivity{
		Source:      "gui_replay",
		Task:        fmt.Sprintf("GUI 回放: %s", flowName),
		Iteration:   currentStep,
		MaxIter:     totalSteps,
		LastSummary: status,
	})
}

func (a *guiReplayActivityAdapter) ClearReplay() {
	a.store.Clear("gui_replay")
}

// registerGUIAutomationTools registers native GUI automation tools (recording,
// replay, click, type, screenshot) into the gui ToolRegistry.
// loopMgr, activityStore, and statusC enable async background replay.
func registerGUIAutomationTools(registry *ToolRegistry, loopMgr *agent.BackgroundLoopManager, activityStore *AgentActivityStore, statusC chan agent.StatusEvent) {
	// Initialize platform components
	bridge := accessibility.NewBridge()
	inputSim := guiautomation.NewInputSimulator()

	screenshotFn := func() (string, error) {
		return captureDesktopScreenshot()
	}

	matcher := guiautomation.NewImageMatcher(nil, screenshotFn)

	locator := guiautomation.NewElementLocator(bridge, matcher, func(msg string) {
		log.Printf("[gui-locator] %s", msg)
	})

	recorder := guiautomation.NewGUIRecorder(bridge, screenshotFn)

	supervisor := guiautomation.NewGUITaskSupervisor(
		locator, inputSim, screenshotFn, nil, nil,
		func(msg string) { log.Printf("[gui-supervisor] %s", msg) },
	)

	replayer := guiautomation.NewGUIReplayer(supervisor, locator, inputSim)

	// Activity updater for background task list
	var guiActivity guiautomation.GUIActivityUpdater
	if activityStore != nil {
		guiActivity = &guiReplayActivityAdapter{store: activityStore}
	}

	// Register into a temporary corelib registry with background loop support.
	coreReg := tool.NewRegistry()
	guiautomation.RegisterTools(coreReg, recorder, replayer, inputSim, screenshotFn,
		loopMgr, guiActivity, statusC, func(msg string) { log.Printf("[gui-replay] %s", msg) })

	// Bridge all corelib GUI tools into the gui-local registry.
	for _, ct := range coreReg.ListAvailable() {
		gt := RegisteredTool{
			Name:        ct.Name,
			Description: ct.Description,
			Category:    ToolCategory(ct.Category),
			Tags:        ct.Tags,
			Priority:    ct.Priority,
			Status:      RegToolStatus(ct.Status),
			InputSchema: ct.InputSchema,
			Required:    ct.Required,
			Source:      ct.Source,
		}
		if ct.Handler != nil {
			h := ct.Handler
			gt.Handler = func(args map[string]interface{}) string {
				return h(args)
			}
		}
		registry.Register(gt)
	}
}

// captureDesktopScreenshot captures the full desktop as a base64-encoded PNG.
// This is a placeholder that delegates to the existing screenshot infrastructure.
func captureDesktopScreenshot() (string, error) {
	// TODO: Wire to corelib/remote screenshot engine for full multi-monitor capture.
	// For now, return an error indicating the screenshot function needs wiring.
	return "", nil
}
