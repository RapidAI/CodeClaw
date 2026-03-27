# Maclaw 记忆管理系统 — 技术文档

## 1. 系统概述

Maclaw 的记忆管理系统是一个持久化的长期记忆存储，为 Agent 提供跨会话的知识保留能力。系统采用 JSON 文件持久化、BM25 全文检索、LLM 驱动压缩三大核心技术，支持自动归档、去重合并、LRU 淘汰等生命周期管理。

系统存在两套实现：
- **corelib/memory** — TUI 端使用，集成 BM25 索引
- **gui/memory_store.go** — GUI 端使用，采用 Memory Stream 评分算法（Recency + Importance + Relevance）

两套实现共享相同的数据模型和持久化格式。

---

## 2. 数据模型

### 2.1 Entry 结构

```go
type Entry struct {
    ID          string    `json:"id"`          // 纳秒时间戳 + 随机 hex
    Content     string    `json:"content"`     // 记忆内容
    Category    Category  `json:"category"`    // 分类
    Tags        []string  `json:"tags"`        // 标签（支持项目路径亲和）
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
    AccessCount int       `json:"access_count"` // 访问计数，用于 LRU 和排序
}
```

### 2.2 记忆分类（Category）

| 分类 | 常量 | 说明 | 受保护 |
|------|------|------|--------|
| `self_identity` | CategorySelfIdentity | 自我认知 | ✅ 永不淘汰/压缩 |
| `user_fact` | CategoryUserFact | 用户事实 | ❌ |
| `preference` | CategoryPreference | 用户偏好 | ❌ |
| `project_knowledge` | CategoryProjectKnowledge | 项目知识 | ❌ |
| `instruction` | CategoryInstruction | 用户指令 | ❌ |
| `conversation_summary` | CategoryConversationSummary | 对话摘要 | ❌ |
| `session_checkpoint` | CategorySessionCheckpoint | 会话检查点（仅 TUI） | ❌ |

`self_identity` 是唯一的受保护分类，永远不会被 LRU 淘汰或 LLM 压缩。

---

## 3. 存储层（Store）

### 3.1 持久化机制

- 存储文件：`<dataDir>/memory.json`，JSON 数组格式
- 延迟写入：通过 `saveCh` channel 信号触发，写入前等待 5 秒合并批量修改
- 后台 goroutine `persistLoop()` 监听保存信号
- 损坏恢复：加载失败时自动备份损坏文件（`.corrupt.<timestamp>`），以空记忆启动

```
signalSave() → saveCh (buffered 1) → persistLoop → 5s debounce → flush()
```

### 3.2 容量管理

- 默认上限：**500 条**（`maxItems`）
- 淘汰策略：LRU（Least Recently Used）
  - 受保护分类（`self_identity`）永不淘汰
  - 非保护条目按 `AccessCount` 升序 + `UpdatedAt` 升序排列
  - 淘汰最不活跃的条目直到总数 ≤ maxItems

### 3.3 去重

`Save()` 时如果已存在完全相同 `Content` 的条目，不创建新条目，而是更新已有条目的 `UpdatedAt` 和 `AccessCount`。

---

## 4. 检索系统

### 4.1 TUI 端 — BM25 检索（corelib/memory）

基于 `corelib/bm25` 包实现，使用 [gse](https://github.com/go-ego/gse) 进行中英文分词。

**BM25 参数：**
- k1 = 1.2
- b = 0.75

**索引维护：**
- 初始化时 `rebuild()` 全量构建
- 增删改时增量更新（`addEntry` / `removeEntry` / `updateEntry`）
- 索引文档 = `Content + Tags`（拼接后分词）

**Recall 算法（`RecallForProject`）：**

```
优先级层次：
1. self_identity → 无条件召回（不受 token 预算限制）
2. user_fact → 高优先级召回
3. 其他分类 → 按综合评分排序

评分公式：
score = BM25(query, entry)
      + 3.0  (如果 entry.Tags 包含当前项目路径)
      - weeks (如果 age > 7 天，每周扣 1 分)
      + 2.0  (如果是 session_checkpoint 且 age < 24h)

约束：max 20 条，token 预算 2000（按 content_len/4 估算）
```

### 4.2 GUI 端 — Memory Stream 评分（gui/memory_store.go）

采用 Generative Agents 论文中的 Memory Stream 思想，三维评分：

```
score = W_recency × Recency + W_importance × Importance + W_relevance × Relevance
```

**权重常量：**
```go
msDecay       = 0.005  // 时间衰减率（每小时）
msWRecency    = 1.0
msWImportance = 1.0
msWRelevance  = 1.0
```

**各维度计算：**

| 维度 | 公式 | 说明 |
|------|------|------|
| Recency | `exp(-0.005 × hours_since_update)` | 指数衰减，越新越高 |
| Importance | `categoryWeight + log1p(accessCount)` | 分类权重 + 访问频次对数 |
| Relevance | `keywordMatchCount + 3.0(项目亲和)` | 关键词命中数 + 项目标签加分 |

**分类权重表：**

| 分类 | 权重 |
|------|------|
| self_identity | 4.0 |
| instruction | 3.0 |
| preference | 2.0 |
| project_knowledge | 2.0 |
| session_checkpoint | 1.5 |
| conversation_summary | 1.0 |
| 其他 | 1.0 |

**RecallDynamic 约束：** max 15 条，token 预算 1500

---

## 5. 压缩系统（Compressor）

### 5.1 压缩流程

```
Compress()
  ├── 1. createBackup()          — 压缩前自动备份
  ├── 2. dedup()                 — 精确/子串去重
  ├── 3. mergeSemanticDuplicates() — LLM 语义合并（需 LLM）
  └── 4. compressEntry()         — LLM 逐条压缩（需 LLM）
```

### 5.2 去重算法（dedup）

两条记录被判定为重复的条件（`isDuplicateLower`）：
1. 内容完全相同（忽略大小写和首尾空白）
2. 同分类 + 较短内容 ≥ 20 字符 + 一方是另一方的子串

保留策略（`pickLoser`）：
- 优先保留更长的内容
- 长度相同时保留 AccessCount 更高的
- 都相同时保留 UpdatedAt 更新的

### 5.3 LLM 语义合并

- 按分类分组，每批最多 25 条（`mergeBatchSize`）
- 跳过受保护分类
- 通过 system prompt 指导 LLM 输出 JSON 合并指令：
  ```json
  [{"keep": 0, "remove": [1, 2], "merged": "合并后的文本"}]
  ```
- 合并后保留 AccessCount 最高的条目作为存活者
- 合并所有相关条目的 Tags

### 5.4 LLM 内容压缩

- 仅压缩内容 ≥ 200 字符（`minContentLen`）的非保护条目
- 目标：压缩到原长度 50% 以下
- 保留所有关键事实、数字、路径、命令
- 如果压缩结果不比原文短，跳过

### 5.5 后台自动压缩

```go
func (mc *Compressor) loop(ctx context.Context) {
    mc.runOnce(ctx)                    // 启动时立即执行一次
    ticker := time.NewTicker(6 * time.Hour) // 每 6 小时执行
    ...
}
```

- 通过 `memory auto-compress on/off` 控制开关
- 每次执行前刷新 LLM 配置（`LLMConfigRefresher`）
- 执行后发射 `memory:compressed` 事件

---

## 6. 对话归档（Archiver）

将过期对话中的关键信息提取为长期记忆。

**触发条件：**
- 对话条目 ≥ 4 条
- LLM 已配置

**流程：**
1. 将对话格式化为 `[role]: content` 文本
2. 通过 LLM 提取关键信息（用户偏好、决策结论、重要事实、任务进度）
3. 存储为 `conversation_summary` 分类，标签包含 `conversation_summary`、用户 ID、日期

**集成点：** GUI 端 `im_message_handler.go` 在对话过期时自动调用。

---

## 7. 备份管理

- 备份目录：`<dataDir>/memory_backups/`
- 命名格式：`memories_backup_20060102_150405.json`
- 默认保留上限：20 个（可通过 `SetMaxBackups` 调整，最小 8）
- 超出上限时自动删除最旧的备份
- 恢复前会先创建当前状态的备份（安全恢复）
- 备份名校验：禁止路径穿越（`/` 和 `\`）

---

## 8. Agent 工具集成

### 8.1 TUI Agent 工具

Agent 通过 `memory` 工具进行记忆管理：

```json
{
  "name": "memory",
  "parameters": {
    "action": "save | list | search | delete",
    "content": "记忆内容（save 时必填）",
    "category": "user_fact | preference | project_knowledge | instruction",
    "tags": ["标签列表"],
    "keyword": "搜索关键词",
    "id": "记忆 ID（delete 时必填）"
  }
}
```

### 8.2 系统提示注入

TUI Agent 启动时自动注入自我认知记忆：
```go
if si := h.memoryStore.SelfIdentitySummary(600); si != "" {
    identity = "你的自我认知（来自记忆）：" + si
}
```

### 8.3 访问计数

`TouchAccess()` 批量递增指定 ID 的 `AccessCount`，用于跟踪记忆的使用频率，影响 LRU 淘汰和检索排序。

---

## 9. CLI 命令

```
maclaw-tui memory list     [--category <cat>] [--keyword <kw>] [--json]
maclaw-tui memory search   [--category <cat>] [--keyword <kw>] [--limit N] [--json]
maclaw-tui memory save     --content <text> [--category <cat>] [--tags t1,t2] [--json]
maclaw-tui memory delete   <id> [--json]
maclaw-tui memory compress [--json]
maclaw-tui memory backup   list | restore <name> | delete <name>
maclaw-tui memory auto-compress  on | off | status
```

---

## 10. 关键设计决策

| 决策 | 理由 |
|------|------|
| JSON 文件而非数据库 | 简单、可移植、便于备份和调试 |
| 5 秒延迟写入 | 减少高频操作的 I/O 开销 |
| BM25 + gse 分词 | 支持中英文混合检索，无需外部依赖 |
| Memory Stream 三维评分 | 平衡时效性、重要性和相关性 |
| self_identity 永不淘汰 | 保证 Agent 核心人格一致性 |
| 压缩前自动备份 | 防止 LLM 压缩导致信息丢失 |
| 语义合并 + 内容压缩双管齐下 | 既减少条目数又缩短单条长度 |
| 500 条上限 + LRU | 控制内存和检索开销 |

---

## 11. 文件索引

| 文件 | 职责 |
|------|------|
| `corelib/memory/types.go` | 数据类型定义（Entry, Category, CompressResult 等） |
| `corelib/memory/store.go` | TUI 端存储层（持久化、CRUD、Recall、LRU） |
| `corelib/memory/bm25.go` | BM25 索引封装 |
| `corelib/memory/compressor.go` | 压缩器（去重、语义合并、LLM 压缩、备份管理） |
| `corelib/memory/archiver.go` | 对话归档器 |
| `corelib/bm25/bm25.go` | BM25 算法实现（gse 分词） |
| `gui/memory_store.go` | GUI 端存储层（Memory Stream 评分） |
| `tui/commands/memory.go` | CLI 命令实现 |
| `tui/agent_tools.go` | Agent 工具集成（toolMemory） |
| `tui/agent_handler.go` | Agent 处理器（记忆注入系统提示） |
| `gui/im_message_handler.go` | GUI 端对话归档集成 |
