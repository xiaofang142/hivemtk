# Fix Report: 订阅状态接口回源刷新本租户镜像，商户级同步不再代写镜像

- **Slug**: subscription-mirror-stale-after-platform-confirm
- **Fixed**: 2026-11-04
- **Assessment**: [assessment.md](assessment.md)
- **Status**: applied

## Summary

生产事故 ORD179377249492384KKX0：平台已确认收款并把租户 `tz3rmdj` 的
`current_period_end` 顺延到 2026-12-04，但用户端订阅页仍显示 `expired`
（镜像 `fetched_at` 停在 2026-10-27）。

根因有两条，互相叠加：

1. **状态接口只读镜像、从不回源**。镜像按租户一行（`id=tenant_id`），刷新入口
   只有 `POST /api/modules/sync`；后台 10 分钟周期 runner 没有租户绑定，
   `tenant_code` 为空，只服务默认租户。非默认租户的镜像因此只在它自己触发同步
   时才更新 —— 平台确认收款后没有任何通道会让该租户的镜像变新。
2. **商户级同步轮次会写无效镜像**：`tenant_code=''` 的订阅段被写进
   `SystemScope` 落点（默认租户行），把 tenant 1 的真实付费状态覆盖成商户级
   体验期（生产库：tenant 1 实际 active 至 2027-01-17，镜像却是 trial 至 11-04 15:00）。

修法：状态接口按 60s TTL 用**当前租户的 tenant_code** 回源刷新本租户镜像
（失败只 Warn、回落最后已知态）；`Sync` 的商户级轮次不再写订阅镜像。

## Changes

| 文件 | 改动 |
|------|------|
| `user-server/internal/service/subscription.go` | 新增 `Refresh(ctx)`：TTL 内不回源；多租户下解析不出 `tenant_code` 时跳过（禁止把商户级段写进租户行）；回源超时 2s 封顶；平台段缺失不写 |
| `user-server/internal/controller/subscription.go` | `Status` 先 `Refresh`（失败 Warn）再 `Snapshot`，用户刷新订阅页即可自愈 |
| `user-server/internal/service/module_catalog.go` | 订阅镜像写入加 `reqTenantBound` 判据：商户级轮次不写 |
| `user-server/internal/platform/client.go` | 新增 `Client.DoContext`，`doRetry` 接受 ctx（取消/超时传播到 HTTP 请求） |
| `user-server/internal/platform/module_client.go` | 新增 `doWithContext`；`SyncModules` 改用 `doWithContext(ctx, …)`，让 2s 回源封顶真正生效 |

## Diff Highlights

```go
// service/subscription.go
func (s *SubscriptionMirrorService) Refresh(ctx context.Context) error {
	if !config.PlatformEnabled() {
		return nil
	}
	if row, err := s.repo.Get(ctx); err == nil && row != nil && time.Since(row.FetchedAt) < statusRefreshTTL {
		return nil
	}
	code := currentTenantCode(ctx)
	if tenant.IsMultiTenant() && code == "" {
		return fmt.Errorf("订阅镜像回源跳过：多租户下未解析出 tenant_code")
	}
	rc, cancel := context.WithTimeout(ctx, statusRefreshTimeout)
	defer cancel()
	snap, err := platform.NewModuleClient().SyncModules(rc, code)
	...
	return s.SaveFromSnapshot(ctx, snap.Subscription)
}
```

```go
// service/module_catalog.go
-	if snap.Subscription != nil {
+	if snap.Subscription != nil && reqTenantBound {
		if err := NewSubscriptionMirrorService().SaveFromSnapshot(ctx, snap.Subscription); err != nil {
```

## Tests Added or Updated

`user-server/internal/service/subscription_test.go`：

- `TestSubscriptionRefreshSkipsFreshMirror` — TTL 内 0 次出站。
- `TestSubscriptionRefreshStaleMirrorWritesPeriod` — 陈旧镜像回源一次，写回
  `current_period_end`/`fetched_at`/`status=active`（事故现场复现）。
- `TestSubscriptionRefreshFailureKeepsMirror` — 平台 500 返回错误且不覆盖镜像。
- `TestSubscriptionRefreshDisabledIsNoop` — 平台关闭时 no-op。
- `TestSubscriptionRefreshMultiTenantWithoutCodeSkips` — 多租户无 tenant_code
  时跳过回源（防串数据）。

`user-server/internal/service/module_sync_stale_scope_test.go`：

- `TestSync_MerchantLevelRound_MustNotWriteSubscriptionMirror` — 商户级轮次不得
  写订阅镜像（钉住 tenant 1 被商户级数据覆盖的事故）。
- `TestSync_TenantRound_WritesOwnSubscriptionMirror` — 租户级轮次写本租户行
  （正向回归，防止修复把正常链路一起关掉）。

## Deviations from Assessment

无。评估中的「Files likely to change」之外只多改了
`user-server/internal/platform/{client,module_client}.go`：评估假设 `SyncModules`
尊重传入 ctx，实测其内部用 `context.Background()`，2s 封顶不生效；改为把 ctx
透传到 HTTP 请求后才成立（不动则 2s 超时是假的）。
