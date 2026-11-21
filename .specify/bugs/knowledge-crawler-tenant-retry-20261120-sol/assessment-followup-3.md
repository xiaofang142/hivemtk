# FA-12 孤儿批次恢复队列饥饿评估

- 日期：2026-11-20
- 来源：用户转述 quality reviewer `ses_e0897f5b0ffeaHa8MBydto5FiY` 的唯一 Important finding；同一 FA-12，SPEC_APPROVED。
- 基线：外部会话推进的 HEAD `2986a7c54` 加当前保留的 followup-2 工作区；不执行 Git 写操作。
- Verdict：valid（源码路径独立核实）；运行复现状态：not-run，本评估仅只读源码，行为 RED 在 fix 阶段补充。
- Severity：high。单租户队首 100 个孤儿可永久阻塞其它有效恢复任务。

## 独立核实

`RunBatch` 在 `ClaimBatch` 之前读取源，所有源读取错误统一返回“爬取源不存在”，未修改批次。`GetSourceByID` 使用 GORM First 默认软删除条件；DeleteSource 仅软删除源，不删除批次。`ListResumableBatches` 固定按 id ASC 限制 100，故这些 pending/过期 running 会在每次扫描重复占位。

同时，普通数据库查询错误被错误映射为不存在。修复必须区分 `errors.Is(err, gorm.ErrRecordNotFound)` 与其它错误，不能把临时不可用的源永久终结。

## 最小修复方案

保留认领前读取源：普通读取错误立即返回包装原错误，批次状态/令牌不变，下次可恢复。仅确认源不存在时继续走现有 ClaimBatch CAS，然后通过现有有界 cleanup context 和 FinishClaimedBatch 完成 failed、finished_at、可查询 error_msg；不存在的源不再执行源回写。正常源走原 crawl 路径。

复用原子认领和 started_at 完成栅栏；不改队列分页、不扩大 limit、不跳过产品恢复路径、不引入 schema/全局源锁。活跃 running 即使源已删除也不能认领；并发 worker 只能有一个完成写；完成写失败保持 running 并在租约过期后重试。

## Files likely to change

- `user-server/internal/aiagent/knowledge/service/kb_crawler.go`：区分源读取错误，缺失源进入认领与终态流程。
- `user-server/internal/pkg/cron/kb_crawler_recovery_test.go`：复用真实 PG fixture 增补入口行为测试。
- 必要时 `user-server/internal/aiagent/knowledge/service/kb_crawler_followup_test.go`：仅在现有无连接 fixture 更适合的边界扩展；不以 fixture 改动替代产品修复。
- 本 bug 目录新增 `fix-followup-3.md`、`test-followup-3.md` 与 `evidence/followup-3-*`，不覆盖任何历史报告。

## RED 与回归计划

1. 在独立合成 schema 建至少 100 个队首孤儿（pending、过期 running、NULL started_at）后建有效批次；软删源，经实际 scanner 连续扫描证明有效批次可达且所有孤儿 failed，error_msg 可从 service.ListBatches 查询。
2. 两租户、软删源、活跃 running 保护；直接 RunBatch 同样不能抢占活跃孤儿。
3. 真实查询制造单次 PostgreSQL 查询错误，验证原始错误仍可识别且批次保持可重试，下次恢复成功消费；区别于真实软删除。
4. 并发 orphan 认领计数=1；旧令牌/跨租户完成写无效；终态持久化故障不误报完成，过期后仍可恢复。
5. 专项 race（含既有 PG/OS 恢复）、build/vet/layer/architecture/GORM/diff 与裸 goroutine 检查，记录实际结果。

## 边界

只连接用户授权的 fa12_synthetic 私有 socket，最小合成 schema，沿用原 helper 的身份校验/自身 schema 清理；不使用 NewTestDB、不停止集群、不删除目录。复现可使用受控 SSRF 抓取失败作为有效任务已到达执行终态的证据，不代表生产 embedding/pgvector 成功。图谱报告和 wiki 在当前 graphify-out 下不存在。本目录 `.specify/extensions.yml` hooks 为空。无待澄清项。
