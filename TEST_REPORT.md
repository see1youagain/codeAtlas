# CodeAtlas 测试报告

日期：2026-10-07

## 本次恢复验证

```powershell
go test ./...
go test -race ./...
go vet ./...
go build -o bin/codeatlas.exe ./cmd/server
go build -o bin/codeatlas-eval.exe ./cmd/eval
& 'C:\Program Files\nodejs\npm.cmd' --prefix web run build
```

结果全部通过。前端使用 React 19.3 与 Vite 8.3，生产资源成功输出至 `internal/server/web/dist` 并由 Go Binary 嵌入。

## 自动化覆盖

- 仓库扫描、Module 识别、搜索、读取、路径逃逸与符号链接边界；
- ReAct 工具循环、Citation Gate、非法引用拒绝；
- 会话上下文、调查记忆、Revision/文件哈希/TTL 失效与删除；
- LLM 429/5xx 重试、熔断打开和恢复；
- SQLite Plan、Approval、Execution、Memory、Conversation 持久化；
- Approval Digest、Revision、Scope、Test Suite 绑定与审批收窄；
- 官方 MCP Client 的工具发现和调用；
- 精确替换、新建文件、防覆盖、批准路径限制；
- 裸镜像 Worktree、Baseline Test、Edit、Regression Test、Diff Review、Discard；
- HTTP Server 与记忆接口集成测试。

## 不伪造的结果

- 没有用户模型密钥，因此不声明真实 LLM Case 的正确率、Token 和成本；
- 本报告验证本地代码、竞态与构建，不把未实际运行的外部模型或生产部署标记为通过。
