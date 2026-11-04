# Bug Test: 平台确认收款后订阅到期时间不更新的回归验证

- **Slug**: subscription-mirror-stale-after-platform-confirm
- **Tested**: 2026-11-04
- **Fix**: [fix.md](fix.md)
- **Result**: verified
- **Status**: verified

## Verification Commands

### 1. 编译与静态检查

```bash
cd user-server
go build ./...
go vet ./internal/service/ ./internal/controller/
```

**结果**：均无输出 / 退出码 0 ✅

### 2. 定向回归用例（新增 7 条 + 存量订阅/同步用例）

```bash
cd user-server
POSTGRES_TEST_PORT=8202 POSTGRES_TEST_USER=admin POSTGRES_TEST_PASSWORD="$PW" \
  go test ./internal/service/ -run 'TestSubscription|TestSync|TestModule' -count=1 -timeout 10m
POSTGRES_TEST_PORT=8202 POSTGRES_TEST_USER=admin POSTGRES_TEST_PASSWORD="$PW" \
  go test ./internal/platform/ -count=1
POSTGRES_TEST_PORT=8202 POSTGRES_TEST_USER=admin POSTGRES_TEST_PASSWORD="$PW" \
  go test ./internal/controller/ -count=1 -timeout 5m
```

**结果**：

```
ok  hivemtk-user/internal/service     6.796s
ok  hivemtk-user/internal/platform    1.210s
ok  hivemtk-user/internal/controller  231.798s
```

新增用例清单：

- `TestSubscriptionRefreshSkipsFreshMirror` ✅
- `TestSubscriptionRefreshStaleMirrorWritesPeriod` ✅（事故现场：陈旧镜像 → 回源 → 写回新到期时间）
- `TestSubscriptionRefreshFailureKeepsMirror` ✅
- `TestSubscriptionRefreshDisabledIsNoop` ✅
- `TestSubscriptionRefreshMultiTenantWithoutCodeSkips` ✅
- `TestSync_MerchantLevelRound_MustNotWriteSubscriptionMirror` ✅
- `TestSync_TenantRound_WritesOwnSubscriptionMirror` ✅

### 3. 生产发布 + 真实链路验证（101.35.252.112）

发布：`./deploy/deploy.sh user-server` → 本机与公网健康检查均通过（18:00:33）。

事故订单 `ORD179377249492384KKX0` / 租户 `tz3rmdj`（tenant_id=18）真实验证：

```bash
# 平台权威行
docker exec … psql -d platform_db -c "select status,current_period_end from tenant_subscriptions
  where merchant_key='azbL6ptzRDm3haVkOJE9IHsDU7n0R916' and tenant_code='tz3rmdj';"
# → active | 2026-12-04 14:09:12.408154+08

# 用户端镜像行（发布后由新代码回源写入）
docker exec … psql -d user_db -c "select id,status,current_period_end,fetched_at
  from tenant_subscription_mirror where id=18;"
# → 18 | active | 2026-12-04 14:09:12+08 | 2026-11-04 18:14:44.778345+08
```

用户端日志（18:14:44，客户 IP 171.213.184.220）：

```
商户上报请求: GET https://manageapi.xapptool.cn/merchant-api/modules/sync?tenant_code=tz3rmdj
商户上报请求成功: … 状态码: 200, 耗时: 42.1ms …
api_interaction GET /api/subscription/status status=200 latency_ms=46
  response={"status":"active","current_period_end":"2026-12-04T14:09:12+08:00",
            "trial_expires_at":"2026-11-04T10:47:42+08:00","price_cents":900,…}
```

⇒ 状态接口在用户刷新时自动带 `tenant_code=tz3rmdj` 回源，并返回新到期时间（修复前同接口返回
`status=expired` 且 `fetched_at=2026-10-27`）。

闸解锁验证：同一 IP 后续 `/api/message-hub/list`、`/api/agents/me`、`/api/ws/agent`
均 200（无 4305），工作台恢复可用。

### 4. 商户级同步不再污染默认租户镜像

发布后后台首轮同步已于 18:00 发起（`tenant_code=` 空），`tenant_subscription_mirror(id=1)`
的 `fetched_at` 仍停在发布前的 17:55:40 —— 商户级轮次没有再写镜像行 ✅
（修复前每次周期同步都会把商户级体验期写进 id=1）。

## Residual Notes

- 生产两条镜像行在发布时已按平台权威值做过一次一次性回填（id=18、id=1），
  目的只是让当前在线租户立刻解锁；此后由 `Refresh`/租户级 sync 正常维护。
- 回填前后值已记录在会话与 fix 报告：id=18 旧值 `trial / period=NULL /
  fetched_at=2026-10-27`；id=1 旧值 `expired / period=NULL / fetched_at=17:55:40`
  （商户级体验期误写）。
