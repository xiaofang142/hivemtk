# Bug Assessment: 爬取批次缺少租户贯通与完成检查点

- Slug: kb-crawler-recovery
- Created: 2026-11-20
- Source: 唯一未完成清单 FA-12
- Verdict: valid
- Severity: high

## 现象与根因

手动同步创建 pending 批次后以 Background 上下文读取批次，SaaS 租户上下文丢失；重试接口只入队，不启动执行。RunBatch 忽略 running 状态写入失败，取消可返回成功，partial 不可重试。crawlOne 在异步索引与 QA 完成前写内存内容哈希，随后相同内容被当作 unchanged，失败无法重试。

IngestCrawledPage 仅按 URL 查文档，查找失败一概当成不存在，旧分段和文档更新错误被忽略；ProcessDocumentByID 无错误返回，异步 Background 丢租户。QA 使用进程内队列，没有可恢复检查点。现有 cron 只扫描到期自动源，未接管 pending 或过期 running 批次。以上结论来自控制器、服务、仓储、模型和定时装配逐层核验。

## 方案论证

采用现有 PostgreSQL 持久化批次及页面检查点，复用已有异步执行器保留 context values。每个源以租约串行运行，批次声明与完成写入有 token 围栏；cron 接管 pending 和租约过期任务，不依赖用户再次请求。

页面按 tenant/source/URL 定位，显式携带 KB 与文档关联，记录内容与 QA 完成哈希。同步索引与 QA 成功后才记完成；候选行与 QA 检查点在同一仓储事务落库，空产出也记完成，崩溃重试不重复生成。现有索引为内存索引，进程重启不能凭持久哈希宣称索引存在：恢复时重建索引，仅本进程实际成功的内容可跳过。

不采用仅加 tenant context：它仍会遗失重启任务。不采用仅调整哈希写入时机：它不能恢复批次和防止并发重复。新增独立消息中间件会扩大部署依赖，因此使用现有数据库与 cron。

## Files likely to change

- `user-server/internal/model/kb_crawler.go`
- `user-server/internal/model/module_tables.go`
- `user-server/internal/aiagent/knowledge/repository/kb_crawler.go`
- `user-server/internal/aiagent/knowledge/repository/kb_crawler_jobs.go`（新）
- `user-server/internal/aiagent/knowledge/repository/kb_document.go`
- `user-server/internal/aiagent/knowledge/service/kb_crawler.go`
- `user-server/internal/aiagent/knowledge/service/kb_crawler_register.go`
- `user-server/internal/aiagent/knowledge/service/knowledge_base_import.go`
- `user-server/internal/aiagent/knowledge/service/kb_qa_split.go`
- `user-server/internal/aiagent/knowledge/controller/kb_crawler.go`
- `user-server/internal/aiagent/knowledge/controller/kb_crawler_recovery_test.go`（新，异步租户与取消回归）
- `user-server/internal/pkg/cron/kb_crawler_cron.go`
- `user-server/internal/aiagent/knowledge/service/kb_crawler_sink_test.go`
- `user-server/internal/aiagent/knowledge/service/kb_crawler_recovery_test.go`（新）
- `user-server/internal/aiagent/knowledge/repository/kb_crawler_jobs_test.go`（新）
- `migrations/` 对应可重入检查点与租约迁移（如部署迁移路径需要）
- `docs/operations/PROD_TOPOLOGY_CHANGES.md`（实际发布后）
- `scripts/bare-go.baseline`（删除本次已消除的两个裸协程豁免，禁止留陈旧基线）

## 验证与风险

先用专用 PostgreSQL 测试复现租户/写入错误/提前成功，再验证 A/B 隔离、并发声明、租约恢复与旧 token 拒绝、接口取消、更新失败、向量失败、QA 失败、重建服务模拟重启、相同内容重试和候选去重。索引失败不得写成功哈希；状态更新失败必须返回错误，不可只写日志。测试网络与模型响应可使用明确合成信号，不代表真实第三方调用效果。

按后端规范运行定向测试、完整构建/vet、分层门、make audit/lint/vet/test-go，按链 A 发布并验证健康和迁移。保持上传和其他调用方兼容，完整重建超千文档属于后续 FA-13。所有 SQL 在 Repository；Service 只做编排。页面检查点与候选事务必须检查所有更新与受影响行数。
