# 开发记录

Researcher 在有限迭代和工具预算内执行 Thought/Action/Observation。Harness 不信任模型给出的引用，而是将本轮工具证据重新编号并验证。Memory 只召回线索，必须通过 Revision、文件哈希和 TTL 校验，最终仍需读取当前代码。
