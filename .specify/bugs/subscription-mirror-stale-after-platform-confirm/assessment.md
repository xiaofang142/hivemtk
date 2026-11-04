# Bug Assessment: 平台确认收款后用户端订阅到期时间不更新

- **Slug**: subscription-mirror-stale-after-platform-confirm
- **Created**: 2026-11-04
- **Source**: pasted text（订单号 ORD179377249492384KKX0）+ 生产库/日志取证
- **Verdict**: valid
- **Severity**: high

## Report (verbatim or summarized)

> ORD179377249492384KKX0
> 订阅续费 已经在平台端确认收款 依旧没有更新订阅到期时间

## Symptom

平台端订单已确认收款并履约成功，但商户端（用户端）订阅状态页/续费墙仍显示旧状态
（`expired`），到期时间停留在续费前的体验期截止点，用户被订阅闸锁在工作台之外。

期望：平台确认收款后，商户端在下次打开订阅页时即看到新的 `current_period_end` 并解除锁定。

## Reproduction

1. 租户 `tz3rmdj`（tenant_id=18）在用户端发起订阅续费 → 用户端向平台下单
   `ORD179377249492384KKX0`（14:08:14，金额 900 分）。
2. 平台运营在管理端「确认收款」→ `MarkPaid` → `ExtendByOrder`：写入
   `subscription_grants` 幂等锚，并把 `tenant_subscriptions(tz3rmdj).current_period_end`
   顺延到 2026-12-04 14:09:12（订单 fulfilled）。
3. 用户在用户端刷新订阅页：`GET /api/subscription/status` 返回
   `{"status":"expired","trial_expires_at":"2026-11-04T10:47:42+08:00","fetched_at":"2026-10-27T12:18:52…","locked":true,"stale":true}`，
   到期时间仍是续费前的值。

生产取证（2026-11-04）：

- 平台库：`orders.status=fulfilled`，`subscription_grants` 有该订单锚，
  `tenant_subscriptions(azbL6ptzRDm3haVkOJE9IHsDU7n0R916, tz3rmdj).current_period_end=2026-12-04 14:09:12`。
- 用户库：`tenant_subscription_mirror(id=18).fetched_at=2026-10-27 12:18:52`，
  `current_period_end=NULL`，`trial_expires_at=2026-11-04 10:47:42`（8 天未刷新）。
- 用户库：`module_orders(ORD179377249492384KKX0).status=fulfilled`（订单页已显示开通）。
- 用户端日志 14:09:25 起持续出现
  `[subscription-mirror] 镜像已陈旧 193h51m0s（fetched_at=2026-10-27T12:18:52+08:00）`。
- 用户端日志 14:55:40（容器重启首轮同步）出现
  `GET …/merchant-api/modules/sync?tenant_code=`（**空租户**），即后台周期同步。

## Suspected Code Paths

- `user-server/internal/service/module_sync_runner.go:48` — 后台周期同步（10min）
  以无租户绑定的 `SystemScope` 上下文启动，`currentTenantCode(ctx)` 为空。
- `user-server/internal/service/module_catalog.go:409` — 只有 `Sync` 会写订阅镜像；
  后台那轮把平台「商户级（tenant_code='')」订阅段写进 `SystemScope` 落点，即
  `tenant_subscription_mirror(id=1)`（默认租户），**不会刷新任何其他租户的镜像行**。
- `user-server/internal/controller/subscription.go:48` — `Status` 只读镜像，从不回源；
  因此「刷新订阅页」这个用户动作无法自愈。
- `user-server/internal/service/subscription.go:198` — 续费下单只写 `module_orders`
  镜像，不刷新订阅镜像；恢复依赖前端 Cashier 的 `paid` 事件轮询（线下确认收款时不存在）。
- 前端 `user-web/src/views/subscription/Wall.vue:161` — 墙页 `load()` 只调
  `subscriptionStatus`，不调 `syncModules`（只有收银台 paid 后的恢复路径才调）。

## Root Cause Hypothesis

**置信度：高。** 订阅镜像按租户一行（`id=tenant_id`），但刷新入口只有
`POST /api/modules/sync`（手工同步 / 订单页待付款轮询 / 收银台 paid 恢复轮询）；
后台 10 分钟周期 runner 没有租户绑定，只把「商户级」订阅段写给默认租户
（tenant 1），对非默认租户既不刷新也不该写。于是：

- 平台侧顺延已成功（数据正确）；
- 目标租户的镜像行停留在最后一次该租户自己触发的同步（本例 2026-10-27）；
- `GET /api/subscription/status` 按设计只读镜像、不回源，刷新页面也无法自愈；
- 线下确认收款的续费没有前端 `paid` 事件，恢复轮询也不会启动。

结果就是「平台已收款、订单已 fulfilled、订阅到期时间却永远不更新」，直到该租户
恰好点一次「同步」按钮。

## Proposed Remediation

**Preferred**：

1. 在 `SubscriptionMirrorService` 增加 `Refresh(ctx)`：平台开启时，若本租户镜像
   `fetched_at` 超过 TTL（60s，避免状态页/横幅每次加载都打平台），则以当前租户
   的 `tenant_code` 调 `SyncModules` 并写回本租户镜像行；平台不可达/未启用时只返回
   错误，调用方继续用旧镜像（锁判定仍是本地化，状态接口不得因回源失败而失败）。
2. `GET /api/subscription/status` 先 `Refresh`（失败仅 Warn）再 `Snapshot`。这样
   平台确认收款后，用户下一次刷新订阅页/打开工作台横幅即可看到新到期时间并解锁，
   不需要手工点「同步」。
3. 同轮修正：`Sync` 的商户级那一轮（无租户绑定）**不写**订阅镜像 —— 商户级订阅段
   属于 `tenant_code=''` 的旧行，写进默认租户镜像会把 tenant 1 的真实付费状态
   覆盖成商户级体验期（生产库中 tenant 1 实际 active 至 2027-01-17，镜像却显示
   trial 至 2026-11-04 15:00）。

**Alternatives**（未采用）：
- 后台 runner 每 10 分钟遍历全部租户逐个同步：能把新鲜度提前到用户请求之前，但
  35+ 租户 × 144 次/天的出站请求与平台负载不成比例，且最坏仍有 10 分钟窗口。
- 平台履约后回调用户端：需要新增入站通道与鉴权，改动面远大于本 bug。

**Files likely to change**：
- `user-server/internal/service/subscription.go`
- `user-server/internal/controller/subscription.go`
- `user-server/internal/service/module_catalog.go`
- `user-server/internal/service/subscription_test.go`

**Tests to add or update**：
- `Refresh` 新鲜镜像（TTL 内）不回源、不写库。
- `Refresh` 陈旧镜像回源并写回 `current_period_end` / `fetched_at`。
- `Refresh` 平台报错时返回错误且不覆盖镜像（状态接口继续可用）。
- `Refresh` 平台关闭时 no-op。
- 商户级 sync 轮次不得写订阅镜像（租户行不被商户级数据覆盖）。

## Risks & Considerations

- 状态接口多一次出站调用：用 60s TTL + 2s 超时封顶；平台不可达时回落到旧镜像
  （状态接口仍 200，最多慢 2s）。
- 镜像刷新仍受「本租户请求」驱动：租户不打开任何页面时不会刷新，但此时也没有
  用户可见的状态需要纠正；闸的本地判定语义不变（fetched_at 仍不参与锁决策）。
- 无数据库迁移、无 API 形状变化；响应字段不变。

## Open Questions

- 无。生产取证已确认平台侧数据正确、用户端镜像陈旧，根因唯一。
