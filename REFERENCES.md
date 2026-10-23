# 参考项目与取舍

## 直接代码来源

- [ai-agent-interview-guide/project-go](https://github.com/bcefghj/ai-agent-interview-guide/tree/main/project-go)：Go 分层、ReAct、Planner、Reflection、LLM Router、Tool Registry 和 RAG 结构的起点。CodeAtlas 基于审计结果重写了实际运行链路。

## 设计参考

- [Model Context Protocol 2026-07-28 Specification](https://modelcontextprotocol.io/specification/2026-07-28)：Stateless Core、Streamable HTTP、Header 路由与授权边界。
- [Official MCP Go SDK](https://github.com/modelcontextprotocol/go-sdk)：MCP Server/Client、协议协商、Schema 和传输实现；项目锁定 v1.7.0。
- [OpenHands](https://github.com/OpenHands/OpenHands)：Workspace、Agent Server、事件流和安全沙箱边界。
- [SWE-agent](https://github.com/SWE-agent/SWE-agent)：受限 Agent-Computer Interface，以及 Issue → Patch → Test 的轨迹。
- [aider](https://github.com/Aider-AI/aider)：以 Git/Diff 为中心的人机协作和可审查修改。
- [LangGraph](https://github.com/langchain-ai/langgraph)：可恢复状态图和 Human-in-the-loop 的设计；当前 Go MVP 不引入其 Python 运行时。
- [SWE-bench](https://github.com/SWE-bench/SWE-bench)：固定 Revision、任务、Patch 与测试结果的评测口径。
- [promptfoo](https://github.com/promptfoo/promptfoo)：Stage 04 可选的 Prompt/Harness 行为回归工具。
- [Langfuse](https://github.com/langfuse/langfuse)：Stage 04 可选的模型 Trace 和数据集实验平台。

## 暂不引入

AutoGen/CrewAI 不进入当前关键路径。CodeAtlas 需要的是角色权限隔离，而不是自由对话式 Agent 团队；Researcher、Architect、Reviewer、Developer、QA 均由同一 Harness 的状态和工具能力约束。
