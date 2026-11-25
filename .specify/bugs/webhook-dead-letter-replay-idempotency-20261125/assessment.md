# Bug Assessment: Webhook 死信并发 replay 双投

- **Slug**: webhook-dead-letter-replay-idempotency-20261125
- **Created**: 2026-11-25T03:15:00+08:00
- **Source**: E4 hook dead-letter 持久化/列表/幂等 replay 验收
- **Verdict**: valid
- **Severity**: high
- **Mode**: implemented（生产修复已落地；主智能体最终独立复验 pending）

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
- on 模式无租户且无 system scope：fail-closed 报错；显式 tenant 0 同样 fail-closed；off 模式显式补默认租户。

### 5. 稳定 delivery-id

`deliverWebhookOnce` 增加 `X-Hivemtk-Delivery-ID`：

`sha256(tenant_id || subscription_id || event || exact_body)` 的十六进制值。

初次重试与死信 replay 使用相同 body/subscription，因此 header 稳定；receiver 可据此去重。该 header 不是鉴权替代品，HMAC 签名保持。

### 6. Crash-after-delivery 边界

worker 在 receiver 已成功响应后、DB 完成前崩溃时，无法区分“已送达”与“未送达”。lease 过期后会携带同一 delivery-id 重发。系统只能保证 **at-least-once + 稳定幂等键**；真正 exactly-once 外部效果取决于 receiver 对 `X-Hivemtk-Delivery-ID` 的去重实现，不能宣称端到端 exactly-once。

## Exact Files

当前 HEAD `d264d3104177587a87dd3c88e33fbe898536e688` 已包含 repository seam 与基础死信测试；本轮实际生产 diff 仅两文件：

1. `user-server/internal/service/r48_growth.go`
   - 注入 `repository.WebhookDeliveryRepository`；
   - candidate→CAS claim→订阅查询→HTTP→条件完成；
   - 移除 replay/list/record/update 的 direct `dbstate`/GORM；
   - tenant 0 fail-closed；
   - 稳定 delivery-id header；
   - 成功/死信持久化错误结构化日志。
2. `user-server/internal/app/webhook_delivery_wiring.go`
   - cron 显式 `tenant.WithSystemScope`。

本轮新增测试：

3. `user-server/internal/service/r48_webhook_replay_error_test.go`
   - candidate/completion DB 错误可见。
4. `user-server/internal/service/r48_webhook_scope_persistence_test.go`
   - tenant 0 replay/list、system scope、持久化失败日志。

HEAD 已有且未覆盖的依赖：

- `repository/webhook_delivery.go` / `repository/webhook_delivery_test.go`：CAS seam 与 3/3 仓储回归；
- `repository/growth_repo.go`：`FindEnabledByID`；
- `service/r48_webhook_dead_letter_test.go`：基础并发/lease/crash/header 回归；其当前 string-concat diff 是外部 scanner 工作，未修改、不计入本 lane。

## Boundary

- 不新增列/迁移；若实现证明必须新增 owner/lease 列，则停止并返回精确提案；
- 不改 CS06/bridge lanes；
- 未 commit/push/deploy、未真实外发、未 UI。
