# FA-12 Cron 重复建单评估

- 日期：2026-11-20
- 来源：质量审查复审对同一 FA-12 的唯一 Important finding；SPEC_APPROVED，上一轮孤儿队列修复已完成。
- 基线：外部提交 `2986a7c54` 及当前保留的 followup-2/3 工作区；不覆盖历史报告、不触碰外层清单。
- Verdict：valid（代码路径已独立核实；真实 synthetic PostgreSQL/scanner RED 已重现，修复后 GREEN 及43个顶层专项 race 测试通过，详见 test-followup-4.md）。
- Severity：high。恢复队列分页后，单次 cron 会为已有持久批次再建并执行一份新批次，造成重复抓取和历史记录；重启/hash 状态变化时可能扩大影响。

## 独立核实

`CrawlerService.ResumeBatches` 只读取固定 ASC/LIMIT 100，并返回该页批次的 `SourceID` 集合。`KBSourceDueScanner` 仅用这个集合跳过 due source；当一个 active、site verified、auto-resync=true（或 nil）、已到期 source 的 pending/过期 running 批次排在前100之外时，其 source 不在集合中，scanner 随后直接 `CreateBatch` 并执行新批次。

上一轮 105 孤儿测试未覆盖此路径：其 helper 创建的源 `AutoResync=false`，不会进入 due 建批分支；它只证明孤儿队列最终可前进，不证明 due source 不重复建单。

## 最小修复方案

在 cron 对每个 due source 创建批次前，经 `CrawlerService` 查询当前租户该 source 是否已有任意 pending/running 批次（running 包括活跃和已过期；已过期项仍由恢复页/后续页消费）。若存在则跳过创建。仓储查询必须使用传入租户 ctx，Service 负责暴露查询，不让 cron 直接碰 repository。

查询发生任何数据库错误时立即返回错误、fail closed；不创建新批次，也不把错误当作“无批次”。保留 `recovered` 集合作为本轮已恢复 source 的防重复保护：即便恢复后批次已变为 failed，当前 scanner 仍不在同一轮再建单；下一轮按正常 due 逻辑处理。

不改恢复 LIMIT、不引入全局源锁、不改变 CAS/finish fencing，不做 schema/migration。该查询只能降低同一租户的重复创建窗口，跨进程并发全局 exactly-once 不在本项要求内。

## Files likely to change

- `user-server/internal/aiagent/knowledge/repository/kb_crawler.go`：新增按 source_id 查询 pending/running 批次的方法。
- `user-server/internal/aiagent/knowledge/service/kb_crawler.go`：暴露仓储查询给 cron，保持 service/repository 分层。
- `user-server/internal/pkg/cron/kb_crawler_cron.go`：CreateBatch 前查询，存在则跳过，查询错误 fail closed。
- `user-server/internal/pkg/cron/kb_crawler_recovery_test.go`：真实 synthetic PostgreSQL scanner RED/GREEN，覆盖分页、due source、租户和暂态查询错误。
- 本目录新增 `fix-followup-4.md`、`test-followup-4.md` 与 `evidence/followup-4-*`；不覆盖已有报告。

## 行为 RED/GREEN 计划

1. 真实 schema 建 105 个前置孤儿批次，再建合法 due source 及其 pending 批次；先软删前置源，跑一次实际 `KBSourceDueScanner`。修复前断言 due source 批次总数从1变2，证明原路径确实重复建单；修复后保持1，第二次 scanner 消费既有批次且仍为1。
2. 仅创建 due source，在 pending/running 存在性查询上注入真实 PostgreSQL SQLSTATE 40001；scanner 返回可识别错误，批次总数保持0，证明查询失败不按“没有批次”建单。
3. 使用两个租户/活跃 running 和 pending，确认查询受 tenant context 限制，不因其它租户的批次阻止当前租户，也不新建覆盖活跃批次。
4. 跑 FA-12 专项 race、build/vet/layer/architecture/GORM/diff；不连接业务数据库，不使用 NewTestDB，不停止/删除授权 PostgreSQL 集群。

## 风险与边界

- 每个 due source 多一次轻量存在性查询；不承诺跨进程 CreateBatch 与查询之间的完全原子性，这超出当前要求且无全局源锁。
- 查询错误返回 scanner，可能使同轮后续源延迟到下一次 cron，属于 fail-closed 预期。
- 真实有效源仍用现有 SSRF 拒绝 loopback 作为确定性失败终态，不代表生产站点/embedding/pgvector 成功链。
