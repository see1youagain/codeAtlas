# 安全模型

## 权限切流

1. **接入层**：公开 REST/MCP 只有读能力；写 REST 和 `/mcp/admin` 要求 Admin Bearer Token。
2. **Harness 层**：根据 Plan 状态控制角色；未批准时 Developer 不运行。
3. **工具层**：Approval 绑定 Digest、Base Revision、Allowed Paths 和 Profile Test Suites。
4. **Workspace 层**：源仓库只读，写入 Runtime Worktree；路径逃逸和符号链接被拒绝。
5. **容器层**：rootfs 只读、Drop ALL Capabilities、不挂 Docker Socket，Runtime 使用独立 Volume。

## 主要威胁及控制

| 威胁 | 控制 |
|---|---|
| 仓库文档中的 Prompt Injection | 仓库内容只作为不可信 Evidence；不能改变 System Policy 或工具权限 |
| `../`、绝对路径逃逸 | `filepath.Rel` 根边界，写时逐级拒绝 Symlink |
| 任意命令执行 | 不提供 Shell；测试只能选择 Profile 注册的 Command/Args |
| 审批后偷换计划 | SHA-256 Digest + Base Revision；任何不一致都要求 Replan |
| 扩大修改范围 | 审批只能收窄候选路径；Git Diff 再独立计算实际文件 |
| 重复执行 | Plan 状态机 + 进程互斥；崩溃后标记 Failed，不自动重放副作用 |
| 密钥泄露 | API Key/Admin Token 仅环境变量；不进入 Prompt、Trace 和响应 |
| 大输出压垮上下文 | 文件、行数、搜索条数、Diff、测试输出均设上限 |
| 多租户越权 | 当前产品是单仓库/单管理员部署；真正多租户需独立 Workspace、OAuth Scope 和租户级密钥 |

## 不做的危险动作

- 不自动 Merge、Push 或创建 PR。
- 不访问 Docker Socket、宿主机根目录或生产凭据。
- 不自动把模型总结写回正式业务文档。
- 不把模型“认为完成”作为成功条件。
