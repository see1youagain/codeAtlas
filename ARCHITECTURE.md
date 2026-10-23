# CodeAtlas 技术架构

## 1. 核心判断

CodeAtlas 不是“给 LLM 接几个工具”，而是一个用确定性 Harness 包围概率性 Agent 的系统：

```text
概率性：模型理解问题、选择下一步调查动作、组织答案或计划
确定性：模式、角色、工具可见性、路径、超时、预算、引用、审批和完成条件
```

ReAct 解决“下一步查什么”；Plan-and-Execute 解决“修改任务如何分段”；Multi-Agent 解决“职责和权限如何隔离”；Harness 负责把三者变成可执行规则。

## 2. 运行架构

```text
Browser / API Client
        │
        ▼
HTTP Control Plane
├─ request id / body limit / concurrency
├─ repository metadata / refresh
├─ ask
└─ plan
        │
        ▼
Harness
├─ mode & role policy
├─ run timeout / iteration budget
├─ tool visibility
├─ trace
└─ verification / approval gate
        │
        ├──────────────┐
        ▼              ▼
Researcher(ReAct)   Architect(Plan)
        │
        ▼
Read-only Tool Dispatcher
├─ repo_summary
├─ list_files
├─ search_code
└─ read_file
        │
        ▼
Repository Workspace
├─ root boundary
├─ ignore policy
├─ file/line evidence
└─ immutable revision label
```

## 3. Harness 四层

| 层 | 负责内容 | 当前实现 |
|---|---|---|
| 接入与策略层 | 身份、模式、输入限制、并发、是否允许写 | Ask/Plan；每个实例最多 4 个并发 Run；无写模式 |
| 编排层 | Researcher、Architect、Reviewer 的顺序和状态 | Ask 为 ReAct 循环；Plan 为 Architect → Approval Gate |
| 执行层 | 工具 Schema、路径沙箱、超时、结果截断 | 四个只读工具；批准后增加精确编辑和 Profile 测试 |
| 验证与观测层 | Citation、测试、Diff、Trace、完成定义 | Citation、基线/回归测试、Diff Scope 和 SQLite Event |

角色不是安全边界的文字描述。即使 Prompt 要求 Architect 修改文件，Dispatcher 也没有写工具，调用会被代码拒绝。

## 4. Ask 状态机

```text
START
  → validate_input
  → model_decide
      ├─ action → permission_check → execute_tool → observation ─┐
      │                                                         │
      └─ final  → citation_gate → success                       │
                                                                │
      └──────────────── next iteration ◀────────────────────────┘

任何时刻：timeout / max iterations / invalid JSON / invalid citation → explicit failure
```

每个 Citation 包含 `id/path/start_line/end_line/snippet`。最终答案只能使用 Observation 中出现过的 `[C#]`；Verifier 再检查引用路径仍在仓库边界内。

## 5. Safe Change 状态机（已实现）

```text
requirement
→ investigate(read-only)
→ plan(version + base revision + allowed paths + tests)
→ wait_approval
   ├─ reject → finish
   └─ approve → create_worktree
              → apply_patch
              → format/test
                 ├─ failed within scope/budget → local repair
                 ├─ scope changed → invalidate approval → replan
                 └─ passed → reviewer → diff report → user decision
```

审批令牌必须绑定 `run_id + base_revision + plan_digest + allowed_paths + allowed_commands`。只要基线、计划或范围发生变化，旧审批立即失效。

## 6. 代码检索策略

MVP 不先建向量数据库，原因是代码问题大量依赖精确符号、配置键、Topic、RPC 方法和文件关系。当前组合是：

1. Repo Summary 提供地图；
2. 字面量 Search 定位符号/配置；
3. Read File 获取局部上下文；
4. ReAct 根据 Observation 决定是否继续查找。

后续只有当评测证明“自然语言概念 → 代码符号”召回不足时，才增加 Embedding；向量结果仍必须回源到文件和行号。

## 7. 安全边界

- 路径：拒绝绝对路径和 `..`，所有候选路径经 `filepath.Rel` 验证。
- 数据：忽略 `.git`、依赖、生成代码、构建产物、虚拟环境和超大文件。
- 工具：当前不存在 Shell、写文件、Git Push、网络搜索和数据库工具。
- 部署：目标仓库以只读 Volume 挂载；容器 rootfs 只读；Drop ALL capabilities；不挂 Docker Socket。
- 模型：API Key 仅从环境变量读取，不进入响应、Trace 或 Prompt。
- 输出：无引用答案失败，不使用模型“自评正确”作为完成信号。

## 8. 与上游 project-go 的关系

| 上游设计 | CodeAtlas 处理 |
|---|---|
| Gin HTTP Server | MVP 改为 Go `net/http`，减少依赖并保留 API 分层 |
| Agent Orchestrator/ReAct/Planner/Reflection | 保留思想，重写为仓库 Researcher、Architect、确定性 Reviewer |
| 通用 Search/Calculator/Database | 替换为代码仓库四工具 |
| Redis 短期记忆 | 当前 Run Scratchpad 即任务记忆，不引入外部状态 |
| Milvus RAG | 当前采用精确检索 + 按需读取；是否加向量检索由评测决定 |
| OpenTelemetry | 当前保留结构化 Trace 与 Run ID；OTEL/Langfuse 为可选扩展，不是核心依赖 |
| 文档 ETL | 替换为实时 Repository Workspace；后续做 Revision 增量索引 |

这不是“给上游换名字”，而是保留可讲清的 Agent 架构后，围绕真实视频微服务场景完成一次产品化重构。
