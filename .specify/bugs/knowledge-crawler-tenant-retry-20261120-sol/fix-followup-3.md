# FA-12 孤儿批次恢复队列饥饿修复

- 日期：2026-11-20
- Assessment：`assessment-followup-3.md`
- Status：applied；同一 FA-12，仅完成本次质量返工，交父任务独立复核和原 reviewer 复审。

## 实现

只修改生产文件 `user-server/internal/aiagent/knowledge/service/kb_crawler.go` 的 RunBatch：

1. 区分 GORM ErrRecordNotFound 和其它源查询错误。其它错误通过 `%w` 返回，保持批次原状态和 started_at，下一次恢复扫描可直接重试，不引入额外35分钟等待。
2. 确认源不存在时仍执行现有 ClaimBatch CAS；只有 pending/过期 running 能认领，活跃 running 及其它租户不能认领。
3. 认领后把缺失源作为执行失败，通过原有独立10秒 cleanup context 和 FinishClaimedBatch 保存 failed、finished_at、包含 source_id 的 error_msg。完成写同时匹配租户、running 和 started_at，旧 worker 不得覆盖新令牌。
4. 孤儿终态写成功后返回其失败原因，跳过源更新；终态写失败则返回持久化错误，保持 running，失效后可再次恢复。

未改 repository 的 ASC/LIMIT 100，不扩大窗口、不绕开队列、不新增 schema、全局源锁或依赖。认领前继续读源是有意的最小选择：临时源读取错误不会产生无谓 running 租约。缺失源的失败终态复用同一套生产完成流程。

## 测试文件

扩展 `user-server/internal/pkg/cron/kb_crawler_recovery_test.go`，复用现有授权私有 PostgreSQL16 helper、三张合成表及真实租户 callbacks。未修改已有测试 fixture 的行为。

- OrphanQueueProgress：105个队首孤儿（35 pending、35旧 running、35 NULL started_at旧 running）、活跃孤儿、另一租户孤儿、后续有效批次。通过实际 KBSourceDueScanner 连续扫描；service.ListBatches 查询终态错误；直接 RunBatch 也验证活跃/跨租户保护。
- OrphanSourceQueryTransient：pending/旧 running × 正常/软删源四种组合。源读取 callback 单次注入由真实 PG 执行 DO 得到的 SQLSTATE 40001；检查原错误可识别、状态/令牌不变，再通过实际 ResumeBatches 重试。
- OrphanConcurrentClaim：两 worker 在真实批次读取后同步放行，针对同一孤儿 RunBatch；成功认领和完成写都恰好一次。新增并发使用 utils.SafeGo。
- OrphanFinishRecovery：分别注入真实 PG 完成写错误和完成前的新 started_at；检查有界清理 ctx 的租户/trace、错误返回、running保留、新令牌未覆盖，再经 ResumeBatches 恢复。仅修改自身合成行模拟租约过期。

## 行为证据

生产补丁前，前三项新测试编译通过并实际失败：连续三次 scanner 后孤儿 failed=0/105、有效批次仍 pending、源读取40001被吞成不存在、并发 claims=0/finishes=0。证据 `evidence/followup-3-orphan-red.log`，exit 1。

补丁后四项新测试含 race 全部通过，第一扫描清前100，第二扫描处理剩余5并执行有效批次，第三扫描保持终态。临时读取错误四种组合保留重试，孤儿并发 claims=1/finishes=1，完成故障两种组合恢复。证据 `evidence/followup-3-orphan-green-race.log`，exit 0。

后加的 OrphanFinishRecovery 属补充验证，未声称独立先红。原有历史报告及其 TDD 顺序说明完整保留，不改写。

完整专项 race 回归40个顶层测试（29 service + 11真实 PG/OS），exit 0，见 `evidence/followup-3-regression-race.log`。其余门禁和精确命令见本轮 `test-followup-3.md`。

## Deviations from Assessment

无。未改 optional 的 service fixture/test 文件；只改一个生产文件及一个已有集成测试文件。

## 边界与协作

保持外部 HEAD 2986a7c54，不派生 Agent/worktree，不暂存/提交/推送，不改外层清单、不进入下一任务，不覆盖历史报告或他人改动。不使用 NewTestDB、不停止 PostgreSQL/删目录，只清理各测试自己创建的合成 schema。

有效批次可达以现有 SSRF 防护拒绝 loopback 后的持久 failed 终态为证；未验证真实生产站点/embedding/pgvector。终态持久化持续故障时依赖既有35分钟过期恢复，不能承诺 DB 不可用时已经保存 failed。全局源锁不在本项范围内。
