# 实施计划

Go / 既有 PG 答案缓存，无新增表或依赖。遵循既有六阶段、租户 fail-closed、单智能体串行及整体一次提交；不改共享 feature 指针。

## 文件范围

- service/new answer_cache_scope.go/test：配置摘要、纯知识资格、结果资格、有界后台写入。
- service/smart_cs_orchestrator.go：生成前后配置证明、同键 Lookup/Store 和实际阈值。
- service/cs03_knowledge_recall_bridge_test.go、smart_cs_faq_policy_test.go：只按新生产范围准备缓存，保留全部正常命中/旁路/知识版本回源断言。
- service/new smart_cs_answer_cache_scope_test.go：真实编排正常命中、不同代理/能力与配置变化不命中。
- aiagent/llm/new dispatcher_cache_scope.go/test 与 dispatcher.go：独立 bug 评估中的调度配置摘要/响应缓存隔离。
- 本 spec 与总任务记录。

## 决策

拒绝全关缓存与降低语义阈值；拒绝只用静态 Agent.Tools 判无工具；拒绝在命中后再补执行必需的工具或审核。稳定纯知识路线保留快路，其余完整生成，不以命中率作为安全通过依据。
