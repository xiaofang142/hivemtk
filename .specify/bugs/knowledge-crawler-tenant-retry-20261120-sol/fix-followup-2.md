# FA-12 规范审查第二轮返工

- 日期：2026-11-20
- 实现交付：**DONE_WITH_CONCERNS**；停留在同一 FA-12，未进入质量审查/下一项。
- 原 assessment/fix/test 与上一轮 followup 保留不覆盖。前轮 test-first 顺序的历史问题仍以原报告为准，不追溯改写。

## 五条 finding 的处理

| Finding | 修订及证据 |
| --- | --- |
| 手动入口丢租户/trace | SyncSource 与 RetryBatch 共用 startBatch，入队时 OnlyTenant、显式恢复 logger trace；SafeGoDetached 的真实 callback ctx 传给 RunBatch。真实 controller integration 在请求返回/取消后观察后台仓储上下文。 |
| 重试 pending 无消费者、running 重启无法恢复 | 原有 KBSourceDueScanner 先经 service.ResumeBatches 读取持久队列，再走原到期逻辑；不改注册/路由装配。仓储条件 UPDATE 原子认领 pending 或过期 running，更新结果必须恰好 1 行。 |
| 失败推进 sitemap lastmod 游标 | 只有 succeeded 推进 LastCrawlAt，取批次开始水位；failed/partially_failed 保留旧水位。RunBatch 失败→重建 service→相同 lastmod 重抓→成功增量过滤的行为测试通过。 |
| 取消/内部 deadline 被当成功 | crawl/discover 与 limiter 等待返回 context error；RunBatch 在终态前再次核对内部 ctx。清理写使用独立有界 ctx。内部预算缩短为 20ms/200ms 的真实 RunBatch 测试保持父 ctx 有效，最终持久 failed。 |
| QA nil 默认语义错误 | 仅显式 false 跳过；nil/true 缺服务均失败。无 QA 的既有 fixture 明确设 false；processing/final/持续失败均不记成功/hash。 |

## 认领和失效协议

- 无 schema/model/migration 变更，复用 started_at。执行预算 30min，失效阈值 35min（含清理宽限），started_at=NULL 时以 created_at 保守判过期。pending 可直接认领。
- ClaimBatch 的状态/失效条件与 UPDATE 在一条语句里；并发认领只允许一个 RowsAffected=1。无需持有跨 HTTP/LLM 的事务锁。
- started_at 按 PostgreSQL 微秒精度保存；FinishClaimedBatch 同时匹配 id、running 和本次 started_at。旧 worker 和其它租户的完成写均为 0 行，不得继续推进源水位。
- 每次扫描最多读取 100 个恢复任务；同轮已恢复的源跳过新建到期批次，其余持久任务在后续扫描继续消费。活跃 running 不可重新认领。
- scanner/ResumeBatches/RunBatch 均限定租户，剥离上游 SystemScope 并保留 trace。真实数据库追加 RED 证明恢复扫描曾继承系统豁免，修订后 GREEN。

## 修复过程中实际发现的两个前提缺口

1. QA final 状态 UPDATE 0 行被当成功；真实跨租户状态 UPDATE 0 行也无错误。先经 crawlOne 和真实 PostgreSQL 两租户测试得到行为 RED，再让 KBCrawlerRepository.UpdateDocumentFields 与 KBDocumentRepository.UpdateStatusFields 检查 RowsAffected，返回 gorm.ErrRecordNotFound。
2. Vectorizer.EmbedBatch 内部使用 Background，导致批次截止/租户/trace 无法进入真实 embedding 调用。先经 RunBatch 得到上下文缺失 RED，再增加兼容的 EmbedBatchContext，并迁移知识导入的两处调用。修订后同一测试进一步暴露取消 ctx 无法写 failed 的 RED；crawlOne 增加独立有界失败收尾后 GREEN。

## 改动文件

生产文件（均在 user-server/）：

- internal/aiagent/knowledge/controller/kb_crawler.go
- internal/aiagent/knowledge/repository/kb_crawler.go
- internal/aiagent/knowledge/repository/kb_document.go
- internal/aiagent/knowledge/service/kb_crawler.go
- internal/aiagent/knowledge/service/knowledge_base_import.go（仅在前轮修复上迁移两处 embedding 调用）
- internal/aiagent/rag/retrieval/vectorizer.go（兼容原 EmbedBatch 接口）
- internal/pkg/cron/kb_crawler_cron.go

测试：新增 internal/aiagent/knowledge/service/kb_crawler_followup_test.go、internal/pkg/cron/kb_crawler_recovery_test.go；扩展前轮 kb_crawler_retry_test.go。新增本目录 assessment-followup-2.md、fix-followup-2.md、test-followup-2.md 和 evidence/followup-2-*。

## 验证与协作边界

最终 36 个专项顶层测试含 race 全部通过；真实 PostgreSQL16 callback、CAS、两租户文档/批次与独立 OS 子进程 kill/recover 有日志证据，详见 test-followup-2.md。build/vet/layer/GORM/差异空白检查退出 0；相邻 33 个单元测试通过。

未碰含他人 staged 改动的 internal/pkg/cron/cron.go、router 装配、外层清单；未运行暂存、commit/push、reset/checkout/clean、worktree、派生 Agent 或发布。只使用父任务授权的私有 socket 合成集群；按测试唯一 schema 创建并清理三张表，不停止/删除集群，不安装 pgvector，不改业务 schema。

## 保留的实际边界

- OS 进程恢复已验证，但恢复后的终态为受控 SSRF 抓取失败；未验证真实站点→实际 embedding/pgvector→QA 的生产端到端链路。
- kill 后仅将本测试自己的 started_at 调整为 1h 前来覆盖失效条件；未实际等待 35min，也未重启 PostgreSQL 服务/整机/完整 API 二进制。
- 整体保证至少一次恢复，不引入 exactly-once。QA 候选成功而文档 final 写失败/进程中断后，仍可能重复生成审核候选；不同批次对同一源的全局串行化不在本次批次 CAS 协议内。
- 依赖需配合 ctx 取消；Go 不能强杀忽略 ctx 的第三方调用。完成 CAS 防止旧 worker 覆盖新批次终态，未为每个外部副作用引入分布式栅栏。
- 持续 DB 故障时 failed 写仍可能失败；调用返回错误、日志记录原因、页面不计成功/hash，恢复依赖持久 pending/过期 running。
- 图谱刷新失败：ModuleNotFoundError: No module named 'graphify'。没有据此跳过源码或测试修订。
