# 超大仓库 Agent 读取 / 压缩方案

## 目标

解决以下 3 个问题：

1. 仓库太大，读不完
2. 上下文太长，容易爆
3. 任务只改一点，却把全仓带进来

核心原则：

> 先建索引，再缩范围，再精读，再压缩。

---

## 一、总体流程

建议固定为 6 步：

```text
Step 1. Repo Overview
Step 2. Task Scoping
Step 3. Relevant Module Discovery
Step 4. Focused File Read
Step 5. Context Compression
Step 6. Execution + Incremental Refresh
```

---

## 二、Step 1：Repo Overview

目标：只建立仓库地图，不读实现细节。

### 要读什么

- 顶层目录结构
- 根配置文件
- README 前几段
- 构建文件
- 测试配置
- workspace / monorepo 配置

### 不要读什么

- `node_modules`
- `dist`
- `build`
- `coverage`
- `.git`
- lock file 全文
- 大型 json / yaml 全文
- generated code

### 输出格式

```text
[Repo Overview]
项目类型: monorepo
技术栈: TypeScript + React + Node
包管理: pnpm
顶层目录:
- apps/
- packages/
- tests/
- scripts/
关键文件:
- package.json
- pnpm-workspace.yaml
- turbo.json
- tsconfig.base.json
测试体系:
- vitest
- playwright
```

### 控制要求

- 控制在 200~500 token
- 不超过 1 屏摘要

---

## 三、Step 2：Task Scoping

目标：把任务从“全仓问题”缩成“局部问题”。

### 模板

```text
[Task Scope]
用户目标: 修复 leaderboard 页面的展示问题
改动类型: 前端 UI / 文案 / 配色
可能涉及:
- 页面组件
- 样式 / theme
- 少量测试
明确不涉及:
- 后端服务
- 数据库
- 部署配置
- 训练代码
```

### 关键点

如果不做这一步，agent 很容易把整个仓库都当“潜在相关”。

---

## 四、Step 3：Relevant Module Discovery

目标：找到可能相关模块，但还不读太深。

### 操作方式

优先利用：

- 目录名
- 文件名
- 路由
- 关键字搜索
- import/export 关系
- 测试文件名

### 输出格式

```text
[Relevant Modules]
1. apps/web/pages/leaderboard
   - 排行榜主页面
2. packages/ui/table
   - 通用表格组件
3. packages/theme
   - 颜色与样式 token
4. tests/web/leaderboard
   - 页面相关测试
```

### 控制规则

- 只保留 3~8 个模块
- 超过 8 个说明范围还没收住

---

## 五、Step 4：Focused File Read

目标：只精读最相关文件。

### 文件优先级

#### 第一优先级：骨架文件

- 路由文件
- 页面入口
- controller / handler
- service 接口
- 主测试文件

#### 第二优先级：关键实现

- 直接被调用的组件
- 样式文件
- theme token
- 核心 util

#### 第三优先级：补充细节

- 次级 helper
- config
- mock / fixture

### 文件筛选上限

建议每轮最多精读：

- 5~12 个文件
- 单文件只读必要片段
- 大文件优先读：
  - 文件头
  - 导出接口
  - 核心函数
  - 目标函数附近 100~300 行

不要整文件灌进去。

### 输出格式：文件卡片

```text
[File Card]
文件: apps/web/pages/leaderboard.tsx
职责: 渲染排行榜页面，组装过滤器和表格
关键依赖:
- LeaderboardTable
- useLeaderboardData
- theme colors
关键函数:
- renderLeaderboard()
- buildColumns()
改动风险:
- 配色和 rank badge 在这里挂接
```

---

## 六、Step 5：Context Compression

目标：把读过的源码压成结构化信息。

### 压缩目标

保留 4 类核心信息：

1. `task_scope`
2. `module_summaries`
3. `file_cards`
4. `execution_state`

### 推荐压缩模板

```text
[Compressed Context]

User Goal
- 修复 / 优化 leaderboard 页面展示

Accepted Constraints
- 仅修改前端
- 不动后端和数据库
- 保持现有接口不变

Relevant Modules
- leaderboard page
- table component
- theme tokens

Key File Cards
- leaderboard.tsx: 页面入口，负责列定义和渲染
- Table.tsx: 通用表格组件，控制行样式
- colors.ts: rank badge 和主题色定义

Current Understanding
- 页面颜色问题主要来自 theme token
- 排名 badge 样式通过 table cell renderer 注入
- 测试主要覆盖排序和渲染，不严格限制具体颜色值

Next Action
- 修改 colors.ts
- 检查 leaderboard.tsx 对 badge 的使用
- 运行相关页面测试
```

### 压缩规则

#### 保留

- 目标
- 决策
- 依赖关系
- 改动点
- 风险点
- 下一步

#### 删除

- 长代码
- 长日志
- 重复报错
- 无关 imports
- 重复说明

---

## 七、Step 6：Execution + Incremental Refresh

目标：改代码时只增量更新上下文。

### 不要做

- 每改一个文件就重新扫全仓

### 要做

只更新：

- 哪些文件已修改
- 哪些理解已确认
- 哪些风险已解除
- 哪些新依赖暴露出来

### 模板

```text
[Execution State]
已修改:
- colors.ts
- leaderboard.tsx

已验证:
- 页面配色已变更
- 本地渲染正常

待验证:
- snapshot test
- dark mode consistency

新增发现:
- rank badge 还有一处样式覆盖在 Table.tsx
```

---

## 八、Token 控制策略

### 推荐预算分配

#### A. 稳定上下文

长期保留：

- 用户偏好
- repo overview
- task scope

预算：300~600 token

#### B. 当前工作上下文

- relevant modules
- file cards
- execution state

预算：600~1500 token

#### C. 临时阅读区

- 当前正在看的源码片段
- 最近一次报错
- 当前测试结果

预算：500~1500 token

### 总控建议

尽量把单轮工作上下文控制在：

- 小任务：1k~3k token
- 中任务：3k~6k token
- 更大任务必须继续摘要

---

## 九、文件读取策略

### 配置文件

只读关键字段：

- scripts
- dependencies
- workspace
- build targets

### README

只读：

- 项目简介
- 启动命令
- 模块说明

前 100~200 行通常够了。

### 源码文件

优先读：

- 文件头注释
- exports
- 类 / 函数签名
- 与任务相关函数

### 测试文件

优先读：

- 测试名
- fixture
- assert
- mock 依赖

### 大日志 / 错误输出

只保留：

```text
错误类型
根因
发生位置
是否已修复
```

---

## 十、Ignore 策略

### 默认忽略目录

```text
node_modules/
dist/
build/
coverage/
.git/
.next/
target/
out/
vendor/
tmp/
.cache/
```

### 默认降级读取文件

```text
package-lock.json
pnpm-lock.yaml
yarn.lock
poetry.lock
Cargo.lock
*.min.js
*.map
generated/*
```

含义：默认只读摘要，不读全文。

---

## 十一、搜索策略

大仓库里，搜索比阅读更重要。

### 先搜这些

- 用户提到的名词
- 页面名 / 接口名 / 错误码
- UI 文案
- 关键组件名
- route path
- function name

### 搜索后再选文件

不要“搜到 100 个结果全读”。

应做一层过滤：

```text
优先级 = 文件名命中 + 路径相关 + 最近被引用 + 接近入口
```

---

## 十二、决策日志机制

为了防止上下文反复膨胀，建议维护一个短的 `decision_log`。

### 模板

```text
[Decision Log]
- 本任务范围限定为前端 leaderboard 页面
- 不修改后端接口
- 配色问题优先检查 theme token，而不是表格逻辑
- 若测试失败，先看 snapshot / style override
```

建议只保留最新 5~10 条。

---

## 十三、常见失败模式

### 失败模式 1：读太多

表现：

- 还没开始改，就上下文爆了

原因：

- 把“理解仓库”当成“读完整仓库”

修复：

- 强制先写 `task_scope`

### 失败模式 2：只读代码，不做摘要

表现：

- 第二轮开始已经忘了前面看过什么

修复：

- 每读 3~5 个文件就压缩一次

### 失败模式 3：报错日志污染上下文

表现：

- 上下文里一堆 stderr

修复：

- 错误只保留摘要卡片

### 失败模式 4：任务范围失控

表现：

- 改个 UI，最后把 API、DB、infra 全扫了

修复：

- 明确写 `out_of_scope`

---

## 十四、推荐的结构化状态

```json
{
  "repo_overview": {
    "type": "monorepo",
    "stack": ["typescript", "react", "node"],
    "top_dirs": ["apps", "packages", "tests", "scripts"],
    "key_files": ["package.json", "pnpm-workspace.yaml", "turbo.json"]
  },
  "task_scope": {
    "goal": "fix leaderboard presentation",
    "in_scope": [
      "frontend page",
      "theme tokens",
      "related tests"
    ],
    "out_of_scope": [
      "backend api",
      "database",
      "deployment"
    ]
  },
  "relevant_modules": [
    {
      "name": "leaderboard page",
      "path": "apps/web/pages/leaderboard",
      "summary": "main page rendering and column composition"
    }
  ],
  "file_cards": [
    {
      "path": "apps/web/pages/leaderboard.tsx",
      "role": "page entry",
      "risk": "rank badge style wiring"
    }
  ],
  "decision_log": [
    "prioritize theme token investigation",
    "avoid backend exploration"
  ],
  "execution_state": {
    "modified_files": [],
    "validated": [],
    "pending": ["inspect colors.ts"]
  }
}
```

---

## 十五、最小可执行版本

如果现在就要落地，至少做这 7 件事：

1. 先建 `repo overview`
2. 先写 `task scope`
3. 只列 3~8 个 `relevant modules`
4. 只精读 5~12 个文件
5. 每个文件都做 `file card`
6. 每轮结束做 `compressed context`
7. 只保留最近一次错误摘要

---

## 十六、一句话版 SOP

> 大仓库任务不要从代码开始，要从范围开始；不要长期保存源码，要长期保存摘要；不要让 agent 记住一切，要让它记住当前任务真正相关的那部分。

---

# 大仓库任务摘要模板

## 1）主摘要模板

```text
[Task Summary]

## 1. User Goal
- 

## 2. Task Scope
### In Scope
- 
- 

### Out of Scope
- 
- 

## 3. Repo Overview
- 项目类型:
- 技术栈:
- 包管理:
- 顶层目录:
  - 
  - 
- 关键入口:
  - 
  - 

## 4. Relevant Modules
1. 
   - path:
   - role:
   - summary:

2. 
   - path:
   - role:
   - summary:

## 5. Key Files
1. 
   - path:
   - purpose:
   - key symbols:
   - notes:

2. 
   - path:
   - purpose:
   - key symbols:
   - notes:

## 6. Current Understanding
- 
- 
- 

## 7. Decision Log
- 
- 
- 

## 8. Risks / Unknowns
- 
- 
- 

## 9. Execution State
### Done
- 
- 

### In Progress
- 
- 

### Pending
- 
- 

## 10. Validation
- 已验证:
  - 
- 待验证:
  - 

## 11. Artifacts
- 
- 

## 12. Next Action
- 
```

---

## 2）精简版模板

```text
[Compressed Task Summary]

Goal:
- 

Scope:
- in: 
- out: 

Relevant Modules:
- 
- 
- 

Key Files:
- 
- 
- 

Current Understanding:
- 
- 
- 

Decisions:
- 
- 

Progress:
- done: 
- doing: 
- next: 

Risks:
- 
```

---

## 3）Repo Overview 模板

```text
[Repo Overview]

项目类型:
- 

技术栈:
- 

包管理 / 构建:
- 

顶层目录:
- 
- 
- 

关键配置文件:
- 
- 
- 

入口 / 关键启动点:
- 
- 

测试体系:
- 
- 

默认忽略区域:
- node_modules
- dist / build
- coverage
- generated files
- lockfiles (full text)
```

---

## 4）Task Scope 模板

```text
[Task Scope]

User Goal:
- 

Change Type:
- bug fix / feature / refactor / docs / infra / UI

In Scope:
- 
- 
- 

Out of Scope:
- 
- 
- 

Constraints:
- 
- 
- 

Success Criteria:
- 
- 
- 
```

---

## 5）Module Card 模板

```text
[Module Card]

Name:
- 

Path:
- 

Role:
- 

Why Relevant:
- 

Main Files:
- 
- 

Dependencies:
- 
- 

Risk Points:
- 
- 

Summary:
- 
```

---

## 6）File Card 模板

```text
[File Card]

Path:
- 

Role / Purpose:
- 

Key Symbols:
- 
- 
- 

Used By:
- 
- 

Depends On:
- 
- 

Relevant Logic:
- 

Potential Change Point:
- 

Risks:
- 

Notes:
- 
```

### 示例

```text
[File Card]

Path:
- apps/web/pages/leaderboard.tsx

Role / Purpose:
- leaderboard 页面入口，负责组装表格列和筛选器

Key Symbols:
- LeaderboardPage
- buildColumns
- renderRankBadge

Used By:
- route /leaderboard

Depends On:
- LeaderboardTable
- useLeaderboardData
- theme/colors

Relevant Logic:
- rank badge 的颜色和表格列渲染在这里接入

Potential Change Point:
- renderRankBadge
- column config

Risks:
- 改颜色可能影响 snapshot test

Notes:
- 当前问题更像样式接入问题，不像数据问题
```

---

## 7）搜索结果压缩模板

```text
[Search Compression]

Query:
- 

Top Matches:
1. 
   - path:
   - why relevant:

2. 
   - path:
   - why relevant:

3. 
   - path:
   - why relevant:

Discarded Areas:
- 
- 

Conclusion:
- 下一步优先查看:
```

---

## 8）错误压缩模板

```text
[Error Summary]

Type:
- 

Where:
- 

Root Cause:
- 

Impact:
- 

Status:
- unresolved / mitigated / fixed

Next Step:
- 
```

### 示例

```text
[Error Summary]

Type:
- module resolution error

Where:
- apps/web/pages/leaderboard.tsx

Root Cause:
- theme/colors 导出名称与引用不一致

Impact:
- 页面构建失败

Status:
- fixed

Next Step:
- rerun related frontend tests
```

---

## 9）阶段性压缩模板

```text
[Iteration Snapshot]

Objective:
- 

What I Read:
- 
- 
- 

What I Learned:
- 
- 
- 

What Changed:
- 
- 

What Failed:
- 
- 

Updated Decisions:
- 
- 

Next Step:
- 
```

---

## 10）执行状态模板

```text
[Execution State]

Modified Files:
- 
- 

Verified:
- 
- 

Pending Verification:
- 
- 

Open Questions:
- 
- 

Blocked By:
- 
```

---

## 11）决策日志模板

```text
[Decision Log]

- 
- 
- 
```

### 示例

```text
[Decision Log]
- 本任务仅处理前端 leaderboard 页面
- 不修改后端接口
- 配色优先从 theme token 层排查
- 如果样式未生效，再看 Table 组件 override
- 非必要不改测试快照
```

---

## 12）风险 / 未知项模板

```text
[Risks / Unknowns]

Known Risks:
- 
- 

Unknowns:
- 
- 

Assumptions:
- 
- 
```

---

## 13）完成态摘要模板

```text
[Completion Summary]

Goal:
- 

Scope:
- 

Files Changed:
- 
- 
- 

Main Changes:
- 
- 
- 

Validation Result:
- 
- 

Remaining Issues:
- 
- 

Artifacts:
- 
- 

Follow-up Suggestions:
- 
- 
```

---

## 14）超短版模板

```text
[Ultra Short Context]
Goal:
Scope:
Relevant:
Key files:
Decisions:
Done:
Risk:
Next:
```

---

## 15）推荐组合用法

### 初始化时

- `Repo Overview`
- `Task Scope`

### 定位时

- `Search Compression`
- `Module Card`

### 精读时

- `File Card`

### 每轮结束

- `Iteration Snapshot`

### 长期保留

- `Compressed Task Summary`
- `Decision Log`
- `Execution State`

### 收尾时

- `Completion Summary`

---

## 16）完整示例

```text
[Compressed Task Summary]

Goal:
- 修复 leaderboard 页面展示风格问题

Scope:
- in: leaderboard 页面、table 组件、theme colors
- out: 后端 API、数据库、部署

Relevant Modules:
- apps/web/pages/leaderboard
- packages/ui/table
- packages/theme

Key Files:
- apps/web/pages/leaderboard.tsx
- packages/ui/table/Table.tsx
- packages/theme/colors.ts

Current Understanding:
- 页面颜色主要由 theme token 决定
- rank badge 样式在 leaderboard.tsx 接入
- Table.tsx 可能存在样式覆盖

Decisions:
- 先改 theme colors
- 若不生效，再查 table override
- 不改后端接口

Progress:
- done: 仓库概览、任务范围、相关模块定位
- doing: 精读 leaderboard.tsx 和 colors.ts
- next: 修改 token 并验证渲染

Risks:
- snapshot test 可能受颜色改动影响
```

---

## 17）最后的使用原则

1. 源码是临时材料，摘要才是长期上下文
2. 每读一个关键文件，就立刻变成 `File Card`
3. 每做一轮工作，就立刻做 `Iteration Snapshot`


---

# 代码落地：corelib/reposcanner 模块

## 模块位置

`corelib/reposcanner/` — 4 个文件：

| 文件 | 职责 |
|------|------|
| `types.go` | 所有结构体定义：ScanConfig, RepoOverview, ModuleCard, FileCard, ScanResult, LLMSummariser 接口 |
| `scanner.go` | 目录遍历 + 文件读取 + 符号提取 + 并发 FileCard 生成 |
| `indexer.go` | Repo Overview 构建 + Module Discovery + 项目类型/技术栈/构建系统/测试框架检测 |
| `compressor.go` | 结构化 Markdown 压缩输出 + Token 预算控制 + LLM 深度摘要 |
| `scan.go` | 主入口 `Scan()` 函数，串联 6 步流程 |

## 对应 mem_tech 流程

| mem_tech 步骤 | 代码实现 |
|---------------|----------|
| Step 1: Repo Overview | `BuildOverview()` in indexer.go |
| Step 2: Task Scoping | 由调用方（agent handler）提供，不在 scanner 内 |
| Step 3: Module Discovery | `BuildModules()` in indexer.go |
| Step 4: Focused File Read | `readFileCards()` in scanner.go |
| Step 5: Context Compression | `CompressToMarkdown()` in compressor.go |
| Step 6: Execution | 由调用方管理增量状态 |

## 使用方式

```go
import "github.com/RapidAI/CodeClaw/corelib/reposcanner"

cfg := reposcanner.DefaultScanConfig()
cfg.DeepMode = true // 启用 LLM 摘要

result, err := reposcanner.Scan("/path/to/repo", cfg, myLLMAdapter)
if err != nil {
    log.Fatal(err)
}

// result.CompressedMD 是压缩后的 Markdown，可直接作为 LLM 上下文
// result.Overview / result.Modules / result.KeyFiles 是结构化数据
```

## 关键设计决策

1. 默认忽略 `.git`、`node_modules`、`vendor`、`dist`、`build`、lockfile 等（与 mem_tech 第十节一致）
2. 大文件（>2MB）自动跳过，每个文件只读前 200 行
3. 文件优先级分 3 档：骨架文件(1) > 关键实现(2) > 补充(3)
4. 最多精读 200 个文件，按优先级排序
5. Token 预算默认 6000 runes，超出时从低优先级文件开始裁剪
6. 快速模式（默认）：纯静态分析，不调 LLM
7. 深度模式：对 priority 1-2 的文件调 LLM 生成 1-2 句摘要
8. 并发：文件读取 8 路，LLM 调用 3 路

## LLMSummariser 接口

调用方需实现：

```go
type LLMSummariser interface {
    Summarise(prompt string) (string, error)
}
```

可以用 `corelib/agent.DoSimpleLLMRequest` 包装实现。
