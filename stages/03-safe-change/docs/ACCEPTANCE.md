# 验收标准

- 无写开关、管理令牌或有效审批时不能执行；
- Revision、Digest、路径或测试套件变化使批准失效；
- 修改只发生在独立 Worktree，源仓库保持只读；
- Baseline 和 Regression 均记录结果；
- Diff 超出批准路径时 Reviewer 拒绝；
- 用户可以丢弃执行，不自动 Merge 或 Push。
