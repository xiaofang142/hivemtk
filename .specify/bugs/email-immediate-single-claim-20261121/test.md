# Bug Verification: FA-EMAIL-01 唯一认领

- **Slug**: email-immediate-single-claim-20261121
- **Tested**: 2026-11-21
- **Result**: pass（本地全绿）

## 关闭判据对照

- 「即时与排期共享原子认领/租约」✔（同一 ClaimByIDForDelivery / ClaimDueForUpdate 原语，FOR UPDATE SKIP LOCKED）
- 「不以两个内存锁代替跨副本约束」✔（认领是 DB 行级 CAS，跨副本成立）
- 「阻塞假投递器交错即时 goroutine/后台轮次，接收计数恰为一次」✔（TestEmailImmediateAndWorkerShareSingleClaim：认领中阻塞 + 排水一轮 + 放行，全程计数 1、状态 sent）
- 「双 worker、认领失败和发送结果不明的恢复」→ 认领失败/让位 ✔（两条用例）；发送结果不明的恢复沿既有 ReclaimStaleSending 回捞语义（at-least-once 已在代码注释如实登记），未在本项重做

## Checks

| Check | Command | Result | Notes |
|---|---|---|---|
| 唯一认领专项 | go test ./internal/email/service -run 'TestEmailImmediateAndWorkerShareSingleClaim\|TestEmailImmediateYieldsWhenWorkerClaimed\|TestEmailClaimByIDFailsClosedOnMissingRow' -count=1 -v | pass | green.log 3 PASS |
| 专项 race | 前两用例 -race | pass | race.log |
| email 全包回归 | go test ./internal/email/... -count=1 | pass | regression.log 0 FAIL |

## 边界

- SMTP 后崩溃重投的 at-least-once 语义保持既有登记，不冒充 exactly-once。
- 未连真实 SMTP；未提交/推送/部署。

## Evidence Files

fix.md / green.log / race.log / regression.log / source-sha256.json
