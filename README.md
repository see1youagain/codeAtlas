# CodeAtlas

CodeAtlas 是一个面向真实代码仓库的、可自托管的知识问答与受控修改 Agent。它以 `video-platform-microservice` 为首个领域仓库：Agent 不靠模型记忆猜测项目，而是通过只读工具检索当前 Git Revision，回答时返回文件与行号证据；收到修改需求时先调查和规划，只有在摘要、Revision、路径与测试套件共同获批后才开放隔离写能力。

项目名称为 CodeAtlas，提供基于仓库证据的问答与审批后隔离修改能力。

## 为什么不是直接复制上游

项目以 [`ai-agent-interview-guide/project-go`](https://github.com/bcefghj/ai-agent-interview-guide/tree/main/project-go) 为 Go 架构起点，保留了 Agent、LLM、Tool、Repository、HTTP 分层以及 ReAct/Planner/Reflection 的设计思路。上游当前是教学骨架：Milvus、Embedding、文档入库和流式响应存在占位，Dockerfile 依赖的文件也不完整。CodeAtlas 因此进行了面向个人项目的收敛：

- 从“通用企业知识库”改为“代码仓库事实系统”；
- 从 Redis + Milvus + PostgreSQL 三套基础设施改为零外部存储的可运行 MVP；
- 从无边界的通用工具改为仓库根目录内的四个只读工具；
- 加入 Harness 的角色、预算、超时、工具可见性与引用门禁；
- 加入视频微服务专项问题集，而不是用泛化 Demo 证明效果；
- 部署容器只读挂载目标仓库，并移除 Linux Capabilities。

上游来源、版本和许可证见 [THIRD_PARTY_NOTICES.md](./THIRD_PARTY_NOTICES.md)。

## 已实现的薄闭环

```text
用户问题
  → Harness（输入、并发、超时、迭代预算）
  → Researcher（ReAct）
  → repo_summary / list_files / search_code / read_file
  → Reviewer（确定性 Citation Gate）
  → 带 Revision、文件、行号、Trace 的回答

修改需求
  → Architect（Plan-and-Execute 的 Plan 部分）
  → 结构化变更计划 + Evidence
  → Digest/Revision/Scope Approval Gate
  → Runtime 裸镜像创建独立 Worktree
  → Developer 受控编辑
  → QA 基线/回归测试
  → Reviewer 检查 Diff 范围
  → 用户审查或丢弃
```

写流程默认关闭。只有设置 `ENABLE_WRITE=true`、配置管理令牌并由用户批准具体计划后，Developer 才能在 Runtime Worktree 中修改；源仓库始终只读，也不会自动 Merge 或 Push。

## 调查记忆

成功问答会保存有限的仓库调查记录。新问题按词面匹配最多召回 3 条历史线索，并校验实时 Git Revision、已记录证据文件的内容哈希和 30 天有效期；单仓库最多保留 100 条。模型必须重新读取当前代码后引用，记忆不授予修改权限。

Ask/Plan 支持 `session_id` 和 `use_memory`：首次请求可不传会话 ID，响应会返回新 ID；后续请求传回同一 ID，可为模型提供最近 4 轮问答以理解追问。会话记录有效期为 7 天，每会话保留 10 轮；它们仅用于理解上下文，旧引用不算当前证据。`use_memory:false` 同时禁用本次调查记忆及会话记录的读取与写入。

Web 控制台支持记忆开关、新会话、调查记忆查看与删除，以及删除当前会话记录。管理员使用 Bearer Token 调用 `GET /api/v1/memories` 查看、`DELETE /api/v1/memories/{memoryID}` 删除调查记录，或 `DELETE /api/v1/sessions/{sessionID}` 删除会话记录，独立于代码写入开关。新会话不删除已有调查记忆。尚未实现用户偏好、语义检索或经人工确认的经验提炼，也未测量真实模型的效率提升。

## Harness 角色

| 角色 | 当前职责 | 工具权限 |
|---|---|---|
| Researcher | 用 ReAct 搜索、阅读并组织仓库事实 | 四个只读仓库工具 |
| Architect | 将修改诉求转成目标、影响面、步骤、测试、风险与回滚 | 仓库摘要 + 模型 |
| Reviewer | 校验回答非空、引用存在、路径未越界；声明审批边界 | 无模型写权限 |
| Developer | 在隔离 Worktree 执行精确替换或新建文件 | 仅限批准路径，默认关闭 |
| QA | 修改前后执行 Profile 预注册测试套件 | 不能执行模型生成的 Shell |

这里的 Multi-Agent 是 Harness 管理的角色隔离，不是多个 Agent 自由聊天。确定性流程负责权限和完成条件，模型只负责需要语义判断的研究与规划。

## 快速启动

### 本地运行

需要 Go 1.25+ 和一个 OpenAI-compatible Chat Completions API。

```powershell
$env:REPOSITORY_ROOT='E:\path\to\video-platform-microservice'
$env:MODEL_API_KEY='your-key'
$env:MODEL_BASE_URL='https://api.openai.com/v1'
$env:MODEL_NAME='your-model'
$env:ENABLE_WRITE='false' # 需要修改闭环时显式开启
go run ./cmd/server
```

打开 `http://localhost:8080`。如果未配置模型密钥，仓库扫描、健康检查和仓库信息接口仍可用，Ask/Plan 会明确返回配置错误。

### 专项评测

服务启动后，可以对视频微服务问题集执行自动回归。评测器会检查 HTTP 成功、Citation 数量和每个 Case 的必含关键点，并将可审计的 JSON 报告写入 `artifacts/eval-reports/`：

```powershell
go run ./cmd/eval -base-url http://127.0.0.1:8080
# 或
make eval
```

评测失败会返回非零退出码，便于接入 CI。真实模型结果取决于使用者配置的模型，本仓库不会用模拟结果冒充模型评分。

### Docker Compose

```powershell
Copy-Item .env.example .env
# 编辑 .env：设置 HOST_REPOSITORY_PATH、MODEL_API_KEY 和模型参数
docker compose up --build
```

Compose 将宿主机仓库挂载到 `/workspace/repository:ro`，SQLite、裸镜像和 Worktree 写入独立 `/data` Volume。容器根文件系统只读、Drop ALL Capabilities，不挂载 Docker Socket；测试命令只能来自 Project Profile。

## API

| 方法 | 路径 | 说明 |
|---|---|---|
| GET | `/healthz` | 存活检查 |
| GET | `/api/v1/repository` | 仓库、Revision、文件、Go Module 和一级模块信息 |
| POST | `/api/v1/repository/refresh` | 重新扫描仓库元数据 |
| GET | `/api/v1/harness/roles` | 查看角色与能力边界 |
| POST | `/api/v1/ask` | ReAct 证据问答 |
| POST | `/api/v1/plan` | Researcher + Architect 结构化变更计划 |
| POST | `/api/v1/plans/{id}/approve` | 绑定摘要、Revision、路径和测试的审批 |
| POST | `/api/v1/plans/{id}/execute` | 在隔离 Worktree 执行 |
| DELETE | `/api/v1/plans/{id}/executions/{executionId}` | 丢弃 Runtime Worktree |
| POST | `/api/v1/knowledge/refresh` | 生成 Revision 知识影响报告 |
| GET | `/api/v1/memories` | 管理员查看本仓库调查记忆及有效状态 |
| DELETE | `/api/v1/memories/{memoryID}` | 管理员删除调查记录 |
| DELETE | `/api/v1/sessions/{sessionID}` | 管理员删除当前仓库的会话记录 |
| POST | `/mcp` | 公开只读 MCP 工具 |
| POST | `/mcp/admin` | Bearer 保护的管理 MCP 工具 |

请求示例：

```json
{
  "question": "分片上传完成后，视频转码是如何触发的？",
  "use_memory": true
}
```

响应包含 `session_id`、`run_id`、`revision`、`answer`、`citations` 和 `trace`。回答没有有效引用时，Citation Gate 会拒绝输出；编号必须来自本轮证据，行号和代码片段会重新核对。Ask 失败的 Trace 也会保存，可通过 Run Events 接口查阅。

## 目录

```text
cmd/server/                 服务入口
cmd/eval/                   专项 Case 自动评测入口
internal/agent/             ReAct Researcher 与 Architect Planner
internal/harness/           角色、预算、超时和模式边界
internal/repository/        仓库扫描、路径沙箱、检索与读取
internal/tool/              只读工具注册与分发
internal/llm/               OpenAI-compatible 模型适配层
internal/server/            HTTP API 与内嵌 Web Demo
internal/execution/         Developer、QA 与 Reviewer 执行闭环
internal/worktree/          裸镜像和隔离 Worktree
internal/store/             SQLite Plan/Event/Approval/Execution
internal/memory/            调查记忆、会话上下文、过期与召回策略
internal/mcpserver/         官方 SDK Streamable HTTP MCP
web/                        React 控制台
evals/                      视频微服务专项回归问题集
stages/                     分阶段需求、开发、验收和复盘记录
```

## 开发阶段

1. **Stage 01 — Grounding Foundation（已完成）**：真实仓库扫描、Revision、路径边界、文本检索、按行读取。
2. **Stage 02 — Ask MVP（已完成）**：ReAct、模型适配、Harness、引用门禁、Web/API、Trace。
3. **Stage 03 — Safe Change（已完成）**：摘要/Revision 审批、Runtime Worktree、受控编辑、基线/回归测试、Diff Scope Gate。
4. **Stage 04 — Eval & Delivery（已完成）**：React、SQLite、MCP、Docker Compose、专项 Case、真实仓库 Smoke Test；真实模型评分需自行配置密钥。

完整需求见 [PRD](./PRD.md)，架构与边界见 [ARCHITECTURE](./ARCHITECTURE.md)，当前完成度见 [STATUS](./STATUS.md)。实际问题与修复记录在 [开发日志](./docs/DEVELOPMENT_LOG.md)，自动化验证见 [测试报告](./TEST_REPORT.md)，面试准备见 [项目面经](./项目面经.md)。权限与部署边界见 [安全模型](./docs/SECURITY.md)。

## 一句话项目介绍

> CodeAtlas 是一个 Go 实现的 repository-grounded 工程 Agent：用 ReAct 调查代码，用 Harness 隔离五类角色与 MCP 工具权限，用 Revision 绑定审批控制 Worktree 修改，再以独立测试、Diff Scope Gate 和知识影响报告闭合研发流程。
