# Fix: Webhook 死信并发 replay 双投

- **Slug**: webhook-dead-letter-replay-idempotency-20261125
- **Status**: applied（main independent final verification pending）
- **HEAD**: user `d264d3104177587a87dd3c88e33fbe898536e688`

## Production Changes

### `user-server/internal/service/r48_growth.go`

- `WebhookSubService` 持有 `repository.WebhookDeliveryRepository`，replay/list/record 不再直接访问 `dbstate`/GORM。
- replay 流程：candidate → CAS claim → enabled subscription 查询 → HTTP → 行版本条件完成。
- claim：`dead_letter → replaying`，`attempts + 1` 仅一次，上限 8；新鲜 `replaying` 在 10s lease 内不可恢复。
- completion token：`(ID, TenantID, Attempts, UpdatedAt)`；旧 worker 不能覆盖新 claim。
- 成功写 `success/delivered_at`；HTTP/订阅失败写 `dead_letter/last_error`；DB 错误返回 error。
- `webhookDeliveryScope`：system scope 可跨租户；普通请求只处理当前租户；on 模式缺租户或 tenant 0 fail-closed；off 模式补默认租户。
- `X-Hivemtk-Delivery-ID = sha256(tenant_id || subscription_id || event || exact_body)`，重试/重放稳定。
- 成功与死信持久化失败均输出结构化 error 日志。

### `user-server/internal/app/webhook_delivery_wiring.go`

- cron 显式使用 `tenant.WithSystemScope(context.Background())`。

## Existing HEAD Dependencies Preserved

- `repository/webhook_delivery.go` / `webhook_delivery_test.go`：CAS seam 与仓储测试。
- `repository/growth_repo.go`：`FindEnabledByID`。
- `service/r48_webhook_dead_letter_test.go`：基础死信测试；其当前 string-concat diff 属外部 scanner 工作，未修改、不计入本 lane。

## New Tests

- `service/r48_webhook_replay_error_test.go`：candidate/completion DB 错误可见。
- `service/r48_webhook_scope_persistence_test.go`：tenant 0、system scope、持久化失败日志。

## Boundary

- 无新增 schema/migration；`status/attempts/updated_at` 已足够。
- 未 commit/push/deploy、未真实外发、未 UI。
- crash-after-delivery 仍是 at-least-once + 稳定幂等键，不宣称端到端 exactly-once。
