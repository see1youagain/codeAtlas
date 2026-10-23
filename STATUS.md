# CodeAtlas 项目状态

更新时间：2026-10-23

当前版本用于保存开发进度并支持换机继续开发。问答、记忆和审批修改的基础流程已实现，完整执行验收与真实模型评测仍待收尾；本次提交不代表生产就绪。

| 阶段 | 状态 | 已实现 |
|---|---|---|
| 01 Grounding Foundation | 待收尾 | Revision、仓库扫描、路径沙箱、搜索、按行 Citation、Project Profile；工具取消与固定快照待补 |
| 02 Ask & MCP | 基础流程完成 | ReAct、Citation Gate、会话/调查记忆、模型重试与熔断、官方 MCP SDK、Trace；真实效果待评测 |
| 03 Safe Change | 待收尾 | Evidence Plan、Digest/Revision 审批、隔离 Worktree、受控编辑、QA、Diff Gate、Discard |
| 04 Delivery & Eval | 待收尾 | React、SQLite、Docker Compose 配置、专项 Case 与评测 CLI、交付文档 |

## 下一步开发入口

以下缺口来自 2026-10-22 的代码审计，尚未逐项做故障复现：

1. **最终 Diff 验收**：`internal/execution/executor.go` 在回归测试之前计算 Diff 和检查范围；需在测试结束后重新检查，覆盖测试生成或修改文件的情况。
2. **失败修复与测试记录**：最终 QA 失败直接结束，没有返回 Developer 的预算内修复流程；`execution.Tests = postTests` 覆盖了基线结果，需分别保存两组结果。
3. **工具超时**：`internal/tool/dispatcher.go` 仅在执行前检查 5 秒 Context，实际遍历、搜索和读取尚未接收取消信号。
4. **Docker 测试依赖**：最终运行镜像没有 Go，而默认 Profile 使用 `go test ./...`；需补齐受支持的测试运行环境并实跑容器修改闭环。
5. **版本与内容一致性**：调查读取实时工作区，审批比较 Git HEAD，执行从提交创建 Worktree；需明确处理未提交修改，或统一使用固定内容快照。
6. **真实模型评测**：当前评测集为 5 条，主要检查关键词和引用数量；没有可审计的真实模型报告，也没有记忆开启/关闭对照实验。需记录目标仓库 Revision、模型与参数，保留失败案例，再衡量回答质量和记忆效果。

审批过期与撤销、多实例原子状态转换、独立测试运行环境仍是后续增强项。向量检索按评测需要决定，不作为本次保存进度的前置条件。

## 2026-10-22 本地复核

- 仓库内 Go 1.25.0 工具链下，`go test -count=1 ./...`、`go test -race -count=1 ./...`、`go vet ./...` 通过；系统默认 Go 1.23.4 不满足 `go.mod` 要求。
- React Production Build 通过，输出到临时目录；生成资源与当前内嵌资源文件名一致。
- `docker compose config --quiet` 通过；本机 Docker 引擎未启动，未验证容器构建与运行。
- 未运行真实模型评测；已有测试通过不能替代上述待办的故障验证。

换机后安装 Go 1.25+、Node 与所需包管理器，按 README 配置仓库路径和模型参数。本机 `.env`、运行数据库、依赖缓存、编辑器配置和内部学习笔记不随代码同步。

## 2026-10-07 完整性恢复

- 从本地时间机快照 `ef2872e` 恢复 39 个缺失文件；
- 恢复了 Server/Eval 入口、Agent、Harness、Memory、MCP、Store、Worktree、React 源码及内嵌生产资源；
- 保留了现存 `.env`、PRD、架构文档和未跟踪前端文件，没有执行 Reset；
- 修正 `.gitignore`，使项目文档和 `stages/` 可以被 Git 保护；
- 当时记录的验证：`go test ./...`、`go test -race ./...`、`go vet ./...`、双入口构建和 Vite Production Build 均通过。

## 明确限制

- 当前模型适配采用 OpenAI-compatible Chat Completions，没有覆盖所有厂商私有协议差异；
- 执行接口为同步调用，生产化需要队列、Worker Lease 和跨实例协调；
- 检索为确定性文本搜索与按需读取，尚未用向量检索；
- 不自动 Merge、Push、创建 PR，也不访问生产环境；
- 调查记忆需要通过 Revision、文件哈希和 TTL 校验，只作为检索线索，不能替代本轮证据。
