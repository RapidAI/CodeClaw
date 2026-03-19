package main

import (
	"fmt"
	"log"
	"strings"
	"time"
)

// runGreenfield executes the Greenfield mode pipeline:
// task_split → architecture → development → merge → compile → test → document → report
//
// It integrates FeedbackLoop for test failure classification, SwarmNotifier for
// real-time progress notifications, and SwarmReporter for final report generation.
// Pause/cancel checks are performed between each phase.
func (o *SwarmOrchestrator) runGreenfield(run *SwarmRun, req SwarmRunRequest, maxAgents int) error {
	// ---------------------------------------------------------------
	// Phase 1: task_split — prepare project & decompose requirements
	// ---------------------------------------------------------------
	o.setPhase(run, PhaseTaskSplit)

	state, err := o.worktreeMgr.PrepareProject(run.ProjectPath)
	if err != nil {
		return fmt.Errorf("prepare project: %w", err)
	}
	run.ProjectState = state

	var fallbackArchDesign string
	tasks, err := o.taskSplitter.SplitRequirements(req.Requirements, req.TechStack)
	if err != nil {
		// Fallback: try splitting via an architect agent session.
		log.Printf("[SwarmOrchestrator] direct LLM split failed: %v, trying agent-based split", err)
		archOutput, archErr := o.runSingleAgent(run, RoleArchitect, 0, run.ProjectPath, PromptContext{
			ProjectName:  run.ProjectPath,
			TechStack:    req.TechStack,
			Requirements: req.Requirements,
		})
		if archErr == nil {
			fallbackArchDesign = archOutput
			tasks, err = o.taskSplitter.SplitViaAgent(archOutput)
		}
		if err != nil {
			return fmt.Errorf("split requirements: %w", err)
		}
	}
	run.Tasks = tasks
	o.addTimelineEvent(run, "task_split_done",
		fmt.Sprintf("Decomposed requirements into %d sub-tasks", len(tasks)), "")

	if err := o.checkPauseCancelGreenfield(run); err != nil {
		return err
	}

	// ---------------------------------------------------------------
	// Phase 2: architecture — create Architect agent, parse output
	// ---------------------------------------------------------------
	o.setPhase(run, PhaseArchitecture)

	var archDesign string
	if fallbackArchDesign != "" {
		// Reuse the architect output from the fallback task split.
		archDesign = fallbackArchDesign
		o.addTimelineEvent(run, "architect_reused", "Reusing architect output from task split fallback", "")
	} else {
		archDesign, err = o.runArchitectPhase(run, req)
		if err != nil {
			log.Printf("[SwarmOrchestrator] architect phase failed: %v", err)
			// Continue with empty design — developers will work without it
		}
	}

	if err := o.checkPauseCancelGreenfield(run); err != nil {
		return err
	}

	// ---------------------------------------------------------------
	// Phase 3: development — parallel Developer agents
	// ---------------------------------------------------------------
	o.setPhase(run, PhaseDevelopment)

	if err := o.runDeveloperAgents(run, tasks, maxAgents, req.Tool, archDesign); err != nil {
		log.Printf("[SwarmOrchestrator] development phase had errors: %v", err)
	}
	o.addTimelineEvent(run, "development_done",
		fmt.Sprintf("Development phase completed (%d agents)", len(run.Agents)), "")

	if err := o.checkPauseCancelGreenfield(run); err != nil {
		return err
	}

	// ---------------------------------------------------------------
	// Phase 4-5: merge + compile — collect branches, merge in order
	// ---------------------------------------------------------------
	o.setPhase(run, PhaseMerge)

	mergeResult, err := o.runGreenfieldMerge(run, req)
	if err != nil {
		log.Printf("[SwarmOrchestrator] merge phase error: %v", err)
	}

	// Transition to compile phase (already handled within MergeAll)
	o.setPhase(run, PhaseCompile)
	if mergeResult != nil && !mergeResult.Success {
		o.addTimelineEvent(run, "merge_partial",
			fmt.Sprintf("Merged %d/%d branches; failed: %v",
				len(mergeResult.MergedBranches),
				len(mergeResult.MergedBranches)+len(mergeResult.FailedBranches),
				mergeResult.FailedBranches), "")
		_ = o.notifier.NotifyFailure(run, "merge",
			fmt.Sprintf("Failed branches: %v", mergeResult.FailedBranches))
	} else {
		o.addTimelineEvent(run, "compile_success", "All branches merged and compiled successfully", "")
	}

	if err := o.checkPauseCancelGreenfield(run); err != nil {
		return err
	}

	// ---------------------------------------------------------------
	// Phase 6: test — create Tester agent, handle feedback loop
	// ---------------------------------------------------------------
	o.setPhase(run, PhaseTest)

	if err := o.runGreenfieldTest(run, req); err != nil {
		log.Printf("[SwarmOrchestrator] test phase: %v", err)
	}

	if err := o.checkPauseCancelGreenfield(run); err != nil {
		return err
	}

	// ---------------------------------------------------------------
	// Phase 7: document — create Documenter agent
	// ---------------------------------------------------------------
	o.setPhase(run, PhaseDocument)

	o.runGreenfieldDocument(run, req, archDesign)

	if err := o.checkPauseCancelGreenfield(run); err != nil {
		return err
	}

	// ---------------------------------------------------------------
	// Phase 8: report — handled by runPipeline after we return
	// ---------------------------------------------------------------
	o.setPhase(run, PhaseReport)

	// Cleanup worktrees and restore project state
	_ = o.worktreeMgr.CleanupRun(run.ProjectPath, run.ID)
	_ = o.worktreeMgr.RestoreProject(run.ProjectPath, run.ProjectState)

	return nil
}

// checkPauseCancelGreenfield blocks while the run is paused and returns an
// error if the run has been cancelled.
func (o *SwarmOrchestrator) checkPauseCancelGreenfield(run *SwarmRun) error {
	for run.Status == SwarmStatusPaused {
		time.Sleep(time.Second)
	}
	if run.Status == SwarmStatusCancelled {
		return fmt.Errorf("run %s cancelled", run.ID)
	}
	return nil
}

// runArchitectPhase creates an Architect agent, waits for it, and returns
// the architecture design output.
func (o *SwarmOrchestrator) runArchitectPhase(run *SwarmRun, req SwarmRunRequest) (string, error) {
	branchName := fmt.Sprintf("swarm/%s/architect-0", run.ID)
	wt, err := o.worktreeMgr.CreateWorktree(run.ProjectPath, run.ID, branchName)
	if err != nil {
		return "", fmt.Errorf("create architect worktree: %w", err)
	}

	ctx := PromptContext{
		ProjectName:  run.ProjectPath,
		TechStack:    req.TechStack,
		Requirements: req.Requirements,
	}

	agent, err := o.createAgent(run, RoleArchitect, 0, wt.Path, branchName, req.Tool, ctx)
	if err != nil {
		return "", fmt.Errorf("create architect agent: %w", err)
	}

	run.Agents = append(run.Agents, *agent)
	agentIdx := len(run.Agents) - 1

	if err := o.waitForAgent(run, agent, DefaultAgentTimeout); err != nil {
		run.Agents[agentIdx] = *agent // sync status
		return "", fmt.Errorf("architect agent failed: %w", err)
	}

	run.Agents[agentIdx] = *agent // sync status
	_ = o.notifier.NotifyAgentComplete(run, agent)

	archDesign := agent.Output
	o.addTimelineEvent(run, "architect_done", "Architect agent completed design", agent.ID)

	return archDesign, nil
}

// runGreenfieldMerge collects completed developer branches and merges them
// in topological order with optional compile verification.
func (o *SwarmOrchestrator) runGreenfieldMerge(run *SwarmRun, req SwarmRunRequest) (*MergeResult, error) {
	var branches []BranchInfo
	for i, agent := range run.Agents {
		if agent.Role == RoleDeveloper && agent.Status == "completed" {
			branches = append(branches, BranchInfo{
				Name:      agent.BranchName,
				AgentID:   agent.ID,
				TaskIndex: agent.TaskIndex,
				Order:     i,
			})
		}
	}

	if len(branches) == 0 {
		o.addTimelineEvent(run, "merge_skip", "No completed developer branches to merge", "")
		return &MergeResult{Success: true}, nil
	}

	// Determine compile command based on tech stack
	compileCmd := inferCompileCommand(req.TechStack)

	result, err := o.mergeCtrl.MergeAll(run.ProjectPath, branches, compileCmd)
	if err != nil {
		return nil, fmt.Errorf("merge all: %w", err)
	}

	o.addTimelineEvent(run, "merge_done",
		fmt.Sprintf("Merge completed: %d merged, %d failed",
			len(result.MergedBranches), len(result.FailedBranches)), "")

	return result, nil
}

// runGreenfieldTest creates a Tester agent, waits for results, and integrates
// the FeedbackLoop for failure classification and repair strategy.
func (o *SwarmOrchestrator) runGreenfieldTest(run *SwarmRun, req SwarmRunRequest) error {
	// Build feature list from completed agents
	var featureList []string
	for _, agent := range run.Agents {
		if agent.Role == RoleDeveloper && agent.Status == "completed" {
			featureList = append(featureList, fmt.Sprintf("Task %d: %s", agent.TaskIndex, agent.Output))
		}
	}

	testCmd := inferTestCommand(req.TechStack)

	branchName := fmt.Sprintf("swarm/%s/tester-0", run.ID)
	ctx := PromptContext{
		ProjectName:  run.ProjectPath,
		TechStack:    req.TechStack,
		Requirements: req.Requirements,
		TestCommand:  testCmd,
		FeatureList:  strings.Join(featureList, "\n"),
	}

	agent, err := o.createAgent(run, RoleTester, 0, run.ProjectPath, branchName, req.Tool, ctx)
	if err != nil {
		return fmt.Errorf("create tester agent: %w", err)
	}

	run.Agents = append(run.Agents, *agent)
	agentIdx := len(run.Agents) - 1

	if err := o.waitForAgent(run, agent, DefaultAgentTimeout); err != nil {
		run.Agents[agentIdx] = *agent
		return fmt.Errorf("tester agent failed: %w", err)
	}

	run.Agents[agentIdx] = *agent
	_ = o.notifier.NotifyAgentComplete(run, agent)
	o.addTimelineEvent(run, "test_done", "Tester agent completed", agent.ID)

	// Parse test output for failures
	failures := parseTestFailures(agent.Output)
	if len(failures) == 0 {
		o.addTimelineEvent(run, "test_pass", "All tests passed", "")
		return nil
	}

	// Classify failures via FeedbackLoop
	o.addTimelineEvent(run, "test_failures",
		fmt.Sprintf("Found %d test failures, classifying...", len(failures)), "")
	_ = o.notifier.NotifyFailure(run, "test",
		fmt.Sprintf("%d test(s) failed", len(failures)))

	classified, err := o.feedbackLoop.ClassifyFailures(failures)
	if err != nil {
		log.Printf("[SwarmOrchestrator] classify failures: %v", err)
		return nil // don't fail the whole run for classification errors
	}

	// Handle classified failures by type
	return o.handleClassifiedFailures(run, req, classified)
}

// handleClassifiedFailures routes each classified failure to the appropriate
// repair strategy based on its type.
func (o *SwarmOrchestrator) handleClassifiedFailures(run *SwarmRun, req SwarmRunRequest, classified []ClassifiedFailure) error {
	var bugs, featureGaps []ClassifiedFailure
	hasDeviation := false

	for _, cf := range classified {
		switch cf.Type {
		case FailureTypeBug:
			bugs = append(bugs, cf)
		case FailureTypeFeatureGap:
			featureGaps = append(featureGaps, cf)
		case FailureTypeRequirementDeviation:
			hasDeviation = true
			o.addTimelineEvent(run, "requirement_deviation",
				fmt.Sprintf("Requirement deviation: %s — %s", cf.TestName, cf.Reason), "")
		}
	}

	// Handle requirement deviations: pause and wait for user input
	if hasDeviation {
		_ = o.notifier.NotifyWaitingUser(run, "Test failures indicate requirement deviations. Please confirm requirements.")
		o.addTimelineEvent(run, "waiting_user", "Paused for user confirmation on requirement deviations", "")

		run.Status = SwarmStatusPaused
		select {
		case input := <-run.userInputCh:
			run.Status = SwarmStatusRunning
			o.addTimelineEvent(run, "user_input", fmt.Sprintf("User provided input: %s", input), "")
		case <-time.After(24 * time.Hour):
			return fmt.Errorf("timed out waiting for user input on requirement deviations")
		}
	}

	// Handle bugs: trigger maintenance round if feedback loop allows
	if len(bugs) > 0 && o.feedbackLoop.ShouldContinue() {
		o.feedbackLoop.NextRound("bug_fix")
		run.CurrentRound = o.feedbackLoop.Round()
		o.addTimelineEvent(run, "feedback_round",
			fmt.Sprintf("Starting bug fix round %d for %d bugs", run.CurrentRound, len(bugs)), "")

		// Create bug descriptions as tasks for a mini maintenance round
		for _, bug := range bugs {
			bugTask := SubTask{
				Index:       len(run.Tasks),
				Description: fmt.Sprintf("Fix bug: %s — %s", bug.TestName, bug.Reason),
			}
			run.Tasks = append(run.Tasks, bugTask)
		}
	}

	// Handle feature gaps: trigger mini-greenfield if feedback loop allows
	if len(featureGaps) > 0 && o.feedbackLoop.ShouldContinue() {
		o.feedbackLoop.NextRound("feature_gap")
		run.CurrentRound = o.feedbackLoop.Round()
		o.addTimelineEvent(run, "feedback_round",
			fmt.Sprintf("Starting feature gap round %d for %d gaps", run.CurrentRound, len(featureGaps)), "")

		for _, gap := range featureGaps {
			gapTask := SubTask{
				Index:       len(run.Tasks),
				Description: fmt.Sprintf("Implement missing feature: %s — %s", gap.TestName, gap.Reason),
			}
			run.Tasks = append(run.Tasks, gapTask)
		}
	}

	// Sync round history to the run
	run.RoundHistory = o.feedbackLoop.History()

	return nil
}

// runGreenfieldDocument creates a Documenter agent to generate project docs.
func (o *SwarmOrchestrator) runGreenfieldDocument(run *SwarmRun, req SwarmRunRequest, archDesign string) {
	branchName := fmt.Sprintf("swarm/%s/documenter-0", run.ID)
	ctx := PromptContext{
		ProjectName:   run.ProjectPath,
		TechStack:     req.TechStack,
		ProjectStruct: archDesign,
	}

	agent, err := o.createAgent(run, RoleDocumenter, 0, run.ProjectPath, branchName, req.Tool, ctx)
	if err != nil {
		log.Printf("[SwarmOrchestrator] create documenter agent: %v", err)
		return
	}

	run.Agents = append(run.Agents, *agent)
	agentIdx := len(run.Agents) - 1

	if err := o.waitForAgent(run, agent, DefaultAgentTimeout); err != nil {
		run.Agents[agentIdx] = *agent
		log.Printf("[SwarmOrchestrator] documenter agent failed: %v", err)
		return
	}

	run.Agents[agentIdx] = *agent
	_ = o.notifier.NotifyAgentComplete(run, agent)
	o.addTimelineEvent(run, "document_done", "Documenter agent completed", agent.ID)
}

// parseTestFailures extracts test failure information from tester agent output.
// It looks for common test failure patterns in the output text.
func parseTestFailures(output string) []TestFailure {
	if output == "" {
		return nil
	}

	var failures []TestFailure
	lines := strings.Split(output, "\n")
	for _, line := range lines {
		line = strings.TrimSpace(line)
		// Look for common failure patterns: "FAIL:", "--- FAIL:", "FAILED"
		if strings.HasPrefix(line, "--- FAIL:") || strings.HasPrefix(line, "FAIL:") {
			name := strings.TrimPrefix(line, "--- FAIL: ")
			name = strings.TrimPrefix(name, "FAIL: ")
			// Extract test name (first word)
			parts := strings.Fields(name)
			testName := name
			if len(parts) > 0 {
				testName = parts[0]
			}
			failures = append(failures, TestFailure{
				TestName:    testName,
				ErrorOutput: line,
			})
		}
	}
	return failures
}

// inferCompileCommand returns a compile command based on the tech stack string.
func inferCompileCommand(techStack string) string {
	ts := strings.ToLower(techStack)
	switch {
	case strings.Contains(ts, "go"):
		return "go build ./..."
	case strings.Contains(ts, "rust"):
		return "cargo build"
	case strings.Contains(ts, "node") || strings.Contains(ts, "typescript") || strings.Contains(ts, "javascript"):
		return "npm run build"
	case strings.Contains(ts, "python"):
		return "python -m py_compile"
	case strings.Contains(ts, "java"):
		return "mvn compile"
	default:
		return "" // no compile check
	}
}

// inferTestCommand returns a test command based on the tech stack string.
func inferTestCommand(techStack string) string {
	ts := strings.ToLower(techStack)
	switch {
	case strings.Contains(ts, "go"):
		return "go test ./..."
	case strings.Contains(ts, "rust"):
		return "cargo test"
	case strings.Contains(ts, "node") || strings.Contains(ts, "typescript") || strings.Contains(ts, "javascript"):
		return "npm test"
	case strings.Contains(ts, "python"):
		return "pytest"
	case strings.Contains(ts, "java"):
		return "mvn test"
	default:
		return "echo no test command configured"
	}
}
