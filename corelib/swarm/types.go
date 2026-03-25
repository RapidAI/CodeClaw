package swarm

import (
	"crypto/rand"
	"fmt"
	"time"
)

// SwarmMode 表示 Swarm 运行的操作模式。
type SwarmMode string

const (
	SwarmModeGreenfield  SwarmMode = "greenfield"
	SwarmModeMaintenance SwarmMode = "maintenance"
)

// SwarmStatus 表示 Swarm 运行的生命周期状态。
type SwarmStatus string

const (
	SwarmStatusPending   SwarmStatus = "pending"
	SwarmStatusRunning   SwarmStatus = "running"
	SwarmStatusPaused    SwarmStatus = "paused"
	SwarmStatusCompleted SwarmStatus = "completed"
	SwarmStatusFailed    SwarmStatus = "failed"
	SwarmStatusCancelled SwarmStatus = "cancelled"
)

// SwarmPhase 表示 Swarm 运行的当前执行阶段。
type SwarmPhase string

const (
	PhaseRequirements   SwarmPhase = "requirements"
	PhaseDesign         SwarmPhase = "design"
	PhaseTaskSplit      SwarmPhase = "task_split"
	PhaseArchitecture   SwarmPhase = "architecture"
	PhaseConflictDetect SwarmPhase = "conflict_detect"
	PhaseDevelopment    SwarmPhase = "development"
	PhaseMerge          SwarmPhase = "merge"
	PhaseCompile        SwarmPhase = "compile"
	PhaseTest           SwarmPhase = "test"
	PhaseDocument       SwarmPhase = "document"
	PhaseReport         SwarmPhase = "report"
)

// AgentRole 表示 Swarm Agent 的角色类型。
type AgentRole string

const (
	RoleArchitect  AgentRole = "architect"
	RoleDesigner   AgentRole = "designer"
	RoleDeveloper  AgentRole = "developer"
	RoleTestWriter AgentRole = "test_writer"
	RoleCompiler   AgentRole = "compiler"
	RoleTester     AgentRole = "tester"
	RoleDocumenter AgentRole = "documenter"
)

// FailureType 分类测试失败类型。
type FailureType string

const (
	FailureTypeBug                  FailureType = "bug"
	FailureTypeFeatureGap           FailureType = "feature_gap"
	FailureTypeRequirementDeviation FailureType = "requirement_deviation"
)

// SwarmRun 表示一次 Swarm 执行实例。
type SwarmRun struct {
	ID          string      `json:"run_id"`
	Mode        SwarmMode   `json:"mode"`
	Status      SwarmStatus `json:"status"`
	Phase       SwarmPhase  `json:"phase"`
	ProjectPath string      `json:"project_path"`
	TechStack   string      `json:"tech_stack,omitempty"`
	Tool        string      `json:"tool"`

	Requirements string `json:"requirements,omitempty"`
	DesignDoc    string `json:"design_doc,omitempty"`

	Tasks      []SubTask    `json:"tasks"`
	TaskGroups []TaskGroup  `json:"task_groups,omitempty"`
	Agents     []SwarmAgent `json:"agents"`

	CurrentRound int          `json:"current_round"`
	MaxRounds    int          `json:"max_rounds"`
	RoundHistory []SwarmRound `json:"round_history"`

	ProjectState *ProjectState `json:"project_state,omitempty"`

	Timeline    []TimelineEvent `json:"timeline"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	CompletedAt *time.Time      `json:"completed_at,omitempty"`

	// userInputCh 用于需求偏差确认的用户输入通道。
	UserInputCh chan string `json:"-"`
}

// SwarmAgent 表示 Swarm 中的单个 Agent 实例。
type SwarmAgent struct {
	ID           string    `json:"id"`
	Role         AgentRole `json:"role"`
	SessionID    string    `json:"session_id"`
	TaskIndex    int       `json:"task_index"`
	WorktreePath string    `json:"worktree_path"`
	BranchName   string    `json:"branch_name"`
	Status       string    `json:"status"`
	RetryCount   int       `json:"retry_count"`
	Output       string    `json:"output,omitempty"`
	Error        string    `json:"error,omitempty"`
	StartedAt    *time.Time `json:"started_at,omitempty"`
	CompletedAt  *time.Time `json:"completed_at,omitempty"`
}

// SubTask 表示一个可分配给开发 Agent 的子任务。
type SubTask struct {
	Index              int      `json:"index"`
	Description        string   `json:"description"`
	ExpectedFiles      []string `json:"expected_files"`
	Dependencies       []int    `json:"dependencies"`
	GroupID            int      `json:"group_id"`
	AcceptanceCriteria []string `json:"acceptance_criteria,omitempty"`
	TestFile           string   `json:"test_file,omitempty"`
	TestCode           string   `json:"test_code,omitempty"`
}

// TaskGroup 将共享文件依赖的任务分组。
type TaskGroup struct {
	ID            int      `json:"id"`
	TaskIndices   []int    `json:"task_indices"`
	ConflictFiles []string `json:"conflict_files"`
}

// WorktreeInfo 保存 git worktree 的元数据。
type WorktreeInfo struct {
	Path       string `json:"path"`
	BranchName string `json:"branch_name"`
	RunID      string `json:"run_id"`
}

// ProjectState 记录项目目录在 worktree 操作前的原始状态。
type ProjectState struct {
	HadGitRepo     bool   `json:"had_git_repo"`
	HadCommits     bool   `json:"had_commits"`
	StashCreated   bool   `json:"stash_created"`
	OriginalBranch string `json:"original_branch"`
}

// SwarmRound 记录一轮反馈循环。
type SwarmRound struct {
	Number    int        `json:"number"`
	Reason    string     `json:"reason"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
	Result    string     `json:"result"`
}

// BranchInfo 描述 worktree 分支的合并排序信息。
type BranchInfo struct {
	Name      string `json:"name"`
	AgentID   string `json:"agent_id"`
	TaskIndex int    `json:"task_index"`
	Order     int    `json:"order"`
}

// MergeResult 记录合并所有 worktree 分支的结果。
type MergeResult struct {
	Success        bool     `json:"success"`
	MergedBranches []string `json:"merged_branches"`
	FailedBranches []string `json:"failed_branches"`
	CompileErrors  []string `json:"compile_errors,omitempty"`
}

// TestFailure 描述一个失败的测试用例。
type TestFailure struct {
	TestName    string `json:"test_name"`
	ErrorOutput string `json:"error_output"`
	FilePath    string `json:"file_path,omitempty"`
}

// ClassifiedFailure 扩展 TestFailure，附带 LLM 分类的失败类型。
type ClassifiedFailure struct {
	TestFailure
	Type   FailureType `json:"type"`
	Reason string      `json:"reason"`
}

// SwarmReport 是 Swarm 运行结束后的完整执行报告。
type SwarmReport struct {
	RunID       string      `json:"run_id"`
	Mode        SwarmMode   `json:"mode"`
	Status      SwarmStatus `json:"status"`
	ProjectPath string      `json:"project_path"`
	Statistics  ReportStatistics `json:"statistics"`
	Rounds      []SwarmRound     `json:"rounds"`
	Agents      []AgentRecord    `json:"agents"`
	Timeline    []TimelineEvent  `json:"timeline"`
	OpenIssues  []string         `json:"open_issues,omitempty"`
	CreatedAt   time.Time        `json:"created_at"`
}

// ReportStatistics 保存 Swarm 运行的聚合指标。
type ReportStatistics struct {
	TotalTasks     int `json:"total_tasks"`
	CompletedTasks int `json:"completed_tasks"`
	FailedTasks    int `json:"failed_tasks"`
	TotalRounds    int `json:"total_rounds"`
	LinesAdded     int `json:"lines_added"`
	LinesModified  int `json:"lines_modified"`
	LinesDeleted   int `json:"lines_deleted"`
}

// AgentRecord 记录单个 Agent 的执行记录。
type AgentRecord struct {
	AgentID     string    `json:"agent_id"`
	Role        AgentRole `json:"role"`
	TaskIndex   int       `json:"task_index"`
	Status      string    `json:"status"`
	Duration    float64   `json:"duration_seconds"`
	DiffSummary string    `json:"diff_summary,omitempty"`
}

// TimelineEvent 记录 Swarm 执行时间线中的单个事件。
type TimelineEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Type      string    `json:"type"`
	Message   string    `json:"message"`
	AgentID   string    `json:"agent_id,omitempty"`
	Phase     string    `json:"phase,omitempty"`
}

// SwarmRunRequest 是启动新 Swarm 运行的输入参数。
type SwarmRunRequest struct {
	Mode         SwarmMode      `json:"mode"`
	ProjectPath  string         `json:"project_path"`
	Requirements string         `json:"requirements,omitempty"`
	TechStack    string         `json:"tech_stack,omitempty"`
	TaskInput    *TaskListInput `json:"task_input,omitempty"`
	MaxAgents    int            `json:"max_agents,omitempty"`
	MaxRounds    int            `json:"max_rounds,omitempty"`
	Tool         string         `json:"tool"`
}

// TaskListInput 描述任务列表的来源和内容。
type TaskListInput struct {
	Source string `json:"source"` // "manual", "github", "feishu", "jira"
	Text   string `json:"text,omitempty"`
	URL    string `json:"url,omitempty"`
}

// SwarmRunSummary 是 SwarmRun 的轻量级视图。
type SwarmRunSummary struct {
	ID        string      `json:"run_id"`
	Mode      SwarmMode   `json:"mode"`
	Status    SwarmStatus `json:"status"`
	Phase     SwarmPhase  `json:"phase"`
	TaskCount int         `json:"task_count"`
	Round     int         `json:"current_round"`
	CreatedAt time.Time   `json:"created_at"`
}

// PromptTemplate 将 Agent 角色与其 Go text/template 字符串配对。
type PromptTemplate struct {
	Role     AgentRole
	Template string
}

// PromptContext 提供 prompt 模板渲染所需的变量。
type PromptContext struct {
	ProjectName        string
	TechStack          string
	TaskDesc           string
	ArchDesign         string
	InterfaceDefs      string
	CompileErrors      string
	TestCommand        string
	Requirements       string
	FeatureList        string
	ProjectStruct      string
	APIList            string
	ChangeLog          string
	AcceptanceCriteria string
	TestFile           string
	TestCode           string
}

// TaskVerdict is the result of verifying an agent's output against its task.
type TaskVerdict struct {
	Pass    bool   `json:"pass"`
	Score   int    `json:"score"`   // 0-100
	Reason  string `json:"reason"`
	Missing string `json:"missing"` // what's missing if not pass
}

// DocType 文档类型
type DocType string

const (
	DocTypeRequirements DocType = "requirements" // 需求文档
	DocTypeDesign       DocType = "design"       // 设计文档
	DocTypeTaskPlan     DocType = "task_plan"     // 任务计划
)

// NewSwarmRunID 生成唯一的 Swarm 运行 ID。
func NewSwarmRunID() string {
	var buf [4]byte
	_, _ = rand.Read(buf[:])
	return fmt.Sprintf("swarm_%d_%08x", time.Now().UnixNano(),
		uint32(buf[0])<<24|uint32(buf[1])<<16|uint32(buf[2])<<8|uint32(buf[3]))
}
