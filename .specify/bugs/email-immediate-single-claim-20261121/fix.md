# Bug Fix: 即时邮件与排水 worker 唯一认领（FA-EMAIL-01）

- **Slug**: email-immediate-single-claim-20261121
- **Fixed**: 2026-11-21
- **Status**: applied

## Summary

即时发送此前落库 pending 后由无认领的 goroutine 直投，与此同时排水 worker 可按"pending 且到期"认领同一行再次投递（无需崩溃即构成双投窗口）。现新增 ClaimByIDForDelivery（与 ClaimDueForUpdate 同一事务/FOR UPDATE SKIP LOCKED/写回语义的按 ID 认领原语），即时 goroutine 投递前先认领：行仍 pending 才归它投，已被 worker 认领则让位；认领失败行保持 pending 由排水侧接管。两路共享同一原语 ⇒ 恰好一次投递。

## Changes

| File | Change |
|---|---|
| `internal/repository/email_send.go` | 接口+实现增 ClaimByIDForDelivery（按 ID 原子认领，非 pending 返回 not-ok） |
| `internal/email/service/email_send.go` | 即时 goroutine 改为先 ClaimByIDForDelivery 再 deliverClaimed（状态写回收口同一函数） |
| `internal/email/service/email_immediate_claim_test.go` | 新增 3 用例：阻塞假投递器交错即时/排水轮恰一次、worker 先认领即时让位（原语层）、不存在行静默让位 |

## Local Verification

- 专项 3 用例 + `-race` 通过（green.log / race.log）。
- email 全包回归 ok / 0 FAIL（regression.log）。全仓 build 通过。
