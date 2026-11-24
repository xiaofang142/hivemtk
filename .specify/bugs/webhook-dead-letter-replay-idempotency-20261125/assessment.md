# Bug Assessment: Webhook 死信并发 replay 双投

- **Slug**: webhook-dead-letter-replay-idempotency-20261125
- **Created**: 2026-11-25T03:15:00+08:00
- **Source**: E4 hook dead-letter 持久化/列表/幂等 replay 验收
- **Verdict**: valid
- **Severity**: high
- **Mode**: approved for minimal implementation（assessment 已细化；未改生产前等待本记录落盘）

## Report

`ReplayDeadWebhookDeliveries` 的顺序 replay 幂等（成功后第二次不再投递），但两个并发 worker 会同时选中同一 `dead_letter` 行并各投一次，造成外部 hook 双发。

## Reproduction

新增 `user-server/internal/service/r48_webhook_dead_letter_test.go`：

- 持久化/列表：PASS；
- 顺序成功 replay + 二次 no-op：PASS；
- 500 失败保持 dead_letter 并累计 attempts：PASS；
- 并发 replay：FAIL，httptest 收到 2 次请求。

日志：`check-e4-hook-dead-letter-tests.log`。

## Root Cause

`ReplayDeadWebhookDeliveries` 仅用 `status=dead_letter AND attempts<8` 查询，没有 claim/lease/状态原子迁移；两个 worker 都可在对方更新状态前读取同一行，然后分别调用 `deliverWebhookOnce`。Service 还直接持有 `dbstate.Get()` / GORM，查询订阅失败被静默 `continue`，DB 不可用时返回假成功。

## Approved Minimal Design

### 1. 无新增 schema

复用 `webhook_deliveries` 现有列：

- `status`：`dead_letter → replaying → success|dead_letter`；
- `attempts`：每次成功 claim 仅 `+1`，上限 8；
- `updated_at`：claim 时间，同时作为 lease 版本/时间戳；
- `last_error` / `response_code` / `delivered_at`：完成结果。

不新增 owner/lease 列，不需要 migration。

### 2. 原子 claim 与 lease

- HTTP 有效超时 = `utils.ShortTimeout`（5s，client timeout 也是 5s）；
- replay lease = `2 * utils.ShortTimeout`（10s），严格长于单次 HTTP 超时；
- 候选：`attempts < 8 AND (status='dead_letter' OR (status='replaying' AND updated_at < now-lease))`；
- claim 用单条条件 UPDATE 原子完成：`status='replaying'`、`attempts=attempts+1`、`updated_at=now`；
- 只有 `RowsAffected=1` 的 worker 获得投递权；并发 loser 不发送；
- 新鲜 `replaying` 行在 lease 内不可恢复，崩溃残留仅在 lease 过期后重新 claim。

### 3. Claim token 与条件完成

claim 成功后返回行版本 token：`(ID, TenantID, Attempts, UpdatedAt)`。

完成 UPDATE 必须同时匹配：

- `id` / `tenant_id`；
- `status='replaying'`；
- `attempts=claim.Attempts`；
- `updated_at=claim.UpdatedAt`。

因此旧 worker 在崩溃恢复/新 claim 后无法覆盖新状态。成功只写 `success` + `delivered_at`；HTTP/订阅失败写 `dead_letter` + 可见错误；DB/查询错误直接返回 error，不伪装成功。

### 4. 租户作用域

- Controller replay：沿用请求租户 ctx，只认领/完成该租户行；
- Cron replay：`internal/app/webhook_delivery_wiring.go` 显式使用 `tenant.WithSystemScope`，可跨租户处理，但每行完成仍匹配行自身 `tenant_id`；
- on 模式无租户且无 system scope：fail-closed 报错；off 模式显式补默认租户。

### 5. 稳定 delivery-id

`deliverWebhookOnce` 增加 `X-Hivemtk-Delivery-ID`：

`sha256(tenant_id || subscription_id || event || exact_body)` 的十六进制值。

初次重试与死信 replay 使用相同 body/subscription，因此 header 稳定；receiver 可据此去重。该 header 不是鉴权替代品，HMAC 签名保持。

### 6. Crash-after-delivery 边界

worker 在 receiver 已成功响应后、DB 完成前崩溃时，无法区分“已送达”与“未送达”。lease 过期后会携带同一 delivery-id 重发。系统只能保证 **at-least-once + 稳定幂等键**；真正 exactly-once 外部效果取决于 receiver 对 `X-Hivemtk-Delivery-ID` 的去重实现，不能宣称端到端 exactly-once。

## Exact Files

1. 新增 `user-server/internal/repository/webhook_delivery.go`
   - `WebhookDeliveryRepository` seam：Create / List / ListReplayCandidates / ClaimReplay / CompleteReplay；
   - 所有 SQL/GORM 数据访问收口此处，Service 不直接接触 DB。
2. 修改 `user-server/internal/repository/growth_repo.go`
   - 增加 `FindEnabledByID(ctx, id)`，订阅不存在/禁用返回 nil，DB 错误可见。
3. 修改 `user-server/internal/service/r48_growth.go`
   - `WebhookSubService` 注入 delivery repository interface；
   - replay 编排改为 candidate→CAS claim→订阅查询→HTTP→条件完成；
   - 移除 replay/list/record/update 的 direct `dbstate`/GORM；
   - 增加稳定 delivery-id header。
4. 修改 `user-server/internal/app/webhook_delivery_wiring.go`
   - cron 显式 system scope。
5. 修改 `user-server/internal/service/r48_webhook_dead_letter_test.go`
   - 并发单投、stale claim 不能覆盖新 claim、lease 内不恢复/过期恢复、attempts cap、租户隔离、DB 错误可见、稳定 delivery-id。
6. 新增 `user-server/internal/repository/webhook_delivery_test.go`
   - CAS claim/complete 的仓储级边界与 stale token 回归。

## Boundary

- 不新增列/迁移；若实现证明必须新增 owner/lease 列，则停止并返回精确提案；
- 不改 CS06/bridge lanes；
- 未 commit/push/deploy、未真实外发、未 UI。
