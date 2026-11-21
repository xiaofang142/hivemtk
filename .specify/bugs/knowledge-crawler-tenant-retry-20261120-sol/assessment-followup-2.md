# FA-12 第二轮只读补充评估

- 日期：2026-11-20
- 结果：SPEC_CHANGES_REQUIRED，五条 findings 均已独立核实；尚未进入质量审查。
- 原 assessment/fix/test 与上一轮 followup 保留，不覆盖。

## 根因与证据

1. `controller/kb_crawler.go:183-184` 的 SafeGo 入参与真实 RunBatch 调用均为 Background，丢失请求租户。`tenant.OnlyTenant` 还会剥掉 trace，需显式恢复 logger trace。`utils.SafeGoDetached` 可复用，但 callback 必须使用其实际上下文。
2. retry controller 只创建 pending；`pkg/cron/kb_crawler_cron.go` 只消费本轮新建批次。RunBatch 拒绝 running，仓储无条件写 running，缺少原子认领与失效恢复。模型已有 StartedAt/CreatedAt/FinishedAt，可复用 StartedAt 作认领时间及完成栅栏，无需 schema。现有全局 cron 注册已经 tenant.Fanout；`cron.go` 的 SkipIfStillRunning 仅保护本进程单个定时任务，不能代替数据库认领。
3. RunBatch 无论 failed/partial 都推进 LastCrawlAt，ParseSitemap 在抓取前按此筛 lastmod。失败页面即使清 hash 仍可能无法重抓。成功游标仅在完整 succeeded 后推进，并取批次开始时间作为增量水位（避免漏掉批次执行期间的新修改）。
4. crawl/discover 的 ctx 取消和 limiter 失败使用 break，返回 nil。需传播 context error，并用有界、保留租户的清理 ctx 写失败终态；内部处理不能无限延长批次截止。
5. 模型 QASplitEnabled=nil 表示默认开启；processCrawledQA 当前把 nil+无服务当成功，违反默认值。仅显式 false 可跳过，其它测试夹具需明确其 QA 意图。

## 最小修订范围与恢复约束

- controller：捕获请求租户，OnlyTenant + 显式 logger trace + SafeGoDetached；sync/retry 都执行真实 RunBatch。
- repository：GORM 条件 UPDATE 原子认领 pending/过期 running；复用 started_at，按 status+started_at 条件完成；租户由 ctx/显式租户限定。列出当前租户 pending/失效 running 供 cron 消费。
- service：批次执行硬上限 30min，失效阈值 35min（执行上限+5min 清理宽限），只有超出阈值才恢复 running；NULL started_at 使用 created_at 保守判断。完成写需匹配本次 started_at，旧 worker 不能覆盖新认领状态。认领时间以 PostgreSQL 微秒精度保存。处理阶段超时不超过批次剩余时间。
- cron：在既有每租户扫描入口先消费持久 pending/过期 running，再做成功水位驱动的到期调度。复用注册，避免触碰已 staged 的 `pkg/cron/cron.go` 与 `router/service_routes.go`。
- 其它检查：QA processing/final 回写失败、持续状态写失败均不得计成功/写 hash；所有新增行为先实跑可编译的行为 RED，再修改生产代码。

## 测试与安全边界

使用无连接 GORM 回调、假 HTTP/LLM 与确定性取消信号。覆盖真实 controller、既有 cron scanner、RunBatch 入口及认领/栅栏仓储路径；服务重建后从持久批次列表恢复，而非只直调 crawlOne。验证两租户、活跃 running 不被夺取、过期 running 恢复、重复消费只有一个认领者、旧认领无法完成、sitemap 失败后 lastmod 不变仍重抓、取消/超时持久 failed、QA 默认及持续失败。

真实 PostgreSQL 并发/OS kill-restart 仍可 partial，但不据此豁免源码缺口。不得连接业务库、drop/recreate、暂存、commit/push、派生 Agent、修改外层清单。开始前已读取 backend/testing/interaction/AI contract、controller、async helper、模型、仓储与注册。将读取 Postgres 并发查询规则后实现 GORM 条件更新；无 schema 变更。

## 恢复实现期间追加的只读发现（对应修复前）

- 父任务提供独立 PostgreSQL16 合成集群后，新增 opt-in 集成夹具，只连接授权私有 socket/DB/runner，按测试创建唯一 schema、三张最小表，注册真实 tenancyidx callback；不用 NewTestDB，不停止集群。
- QA final UPDATE 0 行当前被当成功；真实两租户文档状态 UPDATE 0 行同样无错误。需先行为 RED，再让相关两个 repository 状态更新方法检查 RowsAffected。
- `rag/retrieval/vectorizer.go:83` 在 EmbedBatch 内创建 Background，`knowledge_base_import.go` 的两处调用均不传 ctx。即使批次已设 30min，embedding 仍脱离截止、租户、trace，破坏基于有界执行时间的过期认领前提。需先经 RunBatch + 有上下文断言的 embedding fixture 得到 RED，再增加兼容的 EmbedBatchContext 入口并迁移知识导入调用；旧接口保持兼容。
- 处理超时后 markDocumentFailed 复用已取消 ctx，真实 GORM 会拒绝写；需在 crawlOne 返回处理失败时做有界独立、固定源租户的失败收尾，持续 DB 故障仍 fail closed 且不计成功/hash。
