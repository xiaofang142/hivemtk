# 实施计划

Go/GORM/PostgreSQL；沿用模型AutoMigrate与正式迁移注册链，不改共享feature指针或创建分支，不中途提交。测试只用127.0.0.1:8217专用PG。

## 文件范围

- model/kb_workspace.go：typed兼容字段、nullable阶段时间、操作/范围元数据。
- aiagent/knowledge/repository/knowledge_search_log.go 与新增 knowledge_search_observation.go/test：严格租户typed观测写入。
- aiagent/rag/retrieval/hybrid_searcher.go、新增 hybrid_search_observation.go/test：真实分步时间、范围、哈希及有界观测；不更改结果算法。
- aiagent/rag/retrieval/hybrid_searcher_test.go：只将旧日志表夹具替换为模型同源，保留分片/检索测试。
- migration/migrations/new knowledge_search_observation_migration.go/test 与 initial_schema.go：v3.97.0兼容转换并注册，无破坏性Down。
- 本spec及总任务记录。

## 顺序

1. 用现有SearchIndex和真实PG模型证明UUID日志缺失与计时缺字段（RED）。
2. typed字段、范围写入和各步骤单调耗时（GREEN），缺测保留NULL。
3. 旧bigint表转换/幂等/数字和NULL保留独立RED→GREEN；迁移注册断言。
4. 有界容量/超时/失败观测与缺租户回归，不改变业务结果。
5. 完整retrieval、知识repo和migration回归，门禁及生产效果未验证时partial。
