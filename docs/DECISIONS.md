# 架构决策记录

## ADR-001：Go 自研 Harness，而不是同时引入多套 Agent 框架

状态：Accepted。

原因：项目需要展示 Agent Loop、权限、审批和副作用恢复的底层机制。AutoGen/CrewAI/LangGraph 同时进入会造成职责重叠；Go 实现更贴近视频微服务技术栈。

## ADR-002：精确检索优先，向量检索按评测引入

状态：Accepted。

原因：代码问题优先依赖符号、配置键、Topic、RPC 方法和调用位置。向量检索只有在概念召回指标不足时加入，且必须回源到行号证据。

## ADR-003：角色隔离，而不是自由对话 Multi-Agent

状态：Accepted。

原因：Researcher、Architect、Developer、QA、Reviewer 的价值是不同权限和完成条件。自由聊天会增加 Token、循环和责任不清问题。

## ADR-004：源仓库只读 + Runtime 裸镜像 Worktree

状态：Accepted。

原因：兼顾 Worktree 的 Git/Diff 能力与源仓库不被修改的安全边界。

## ADR-005：SQLite 单机持久化

状态：Accepted。

原因：个人可部署项目优先减少基础设施；单进程写入、WAL 和 JSON Payload 足以支撑 Plan/Event/Approval。横向扩容时再迁移 PostgreSQL 和分布式 Lease。

## ADR-006：官方 MCP SDK

状态：Accepted。

原因：MCP 传输、版本协商和 Header 规则变化较快，手写“类 MCP”无法证明互操作性。使用官方 Go SDK并通过官方 Client 集成测试。
