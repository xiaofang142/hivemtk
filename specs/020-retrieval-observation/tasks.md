# 任务清单

- [x] T001 真实PG UUID与阶段/范围日志目标RED。
- [x] T002 typed字段/仓储与阶段测量GREEN（retrieval 1.556秒，另阻塞追踪RED→GREEN）。
- [x] T003 历史表兼容迁移RED/GREEN并注册（migration 0.878秒）。
- [x] T004 观测容量、缺租户、失败/未执行与检索结果不变回归（retrieval 1.826秒、知识仓储0.741秒、迁移0.944秒）。
- [ ] T005 全包回归、类型契约与未覆盖收敛报告；未部署不声称生产收益。

## Phase 1: Convergence

- [x] T006 HIGH：修复兼容追踪直写丢失两秒截止的缺口，按 FR004、FR005 与 plan: 有界超时。范围限定 `internal/pkg/tracing/tracing.go`、新 `tracing_context_test.go`、`internal/aiagent/rag/retrieval/hybrid_search_observation.go/test`。保留原 RecordNode/Publish 接口兼容；新增上下文受控节点写入，检索观测使用它。真实数据库排他锁与租户边界RED→GREEN；追加检索后台两秒截止且释放锁后不补写。完整tracing 2.827秒、retrieval 5.953秒通过；总体门禁/生产效果不由此推断。
