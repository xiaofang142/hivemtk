# Bug Verification: FA-17 线索发掘配置缓存与去抖租户隔离

- **Slug**: lead-mining-tenant-isolation-20261121
- **Tested**: 2026-11-21
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: pass（本地专项与门禁全绿；整体交付口径见「边界」）

## Summary

修后四个专项用例全部通过（含 -race）：A/B 租户交错消息各自读取自身配置且互不去抖；定向 reload 仅失效目标租户且调用方修改不污染缓存；SaaS 模式下无租户归属的消息在读取配置前 fail-closed；去抖键含租户且默认租户兼容。既有租户上下文回归（`TestLeadMiningProcess_BindsTenantFromMessage`、`TestLeadMiningProcess_NoTenantKeepsFailClosed`）继续通过。全部 8 项静态门 rc=0，lint 0 issues。

`TestLeadMining_Integration_*` 四例因 `POSTGRES_TEST_PASSWORD/POSTGRES_PASSWORD` 未配置在发起连接前按门禁失败——这是既有环境约束（与其他在途批次同一口径），不是本修复引入的失败；它们属于原 FA-17 关闭判据中的数据库集成面，保持「缺库=未验」不冒充通过。

## Checks Performed

| Check | Command / Action | Result | Notes |
|---|---|---|---|
| 修后专项（4 用例） | go test ./internal/service -run 'TestLeadMiningTenant_' -count=1 -v | pass | green.log，4 PASS / 0 FAIL，1.341s |
| 专项 race | go test -race ./internal/service -run 'TestLeadMiningTenant_' -count=1 -v | pass | race.log，4 PASS，无 race |
| 既有 lead mining 回归 | go test ./internal/service -run 'TestLeadMining' -count=1 -v | partial | regression.log：6 PASS（含既有 TestLeadMiningProcess_* 两例）+ 4 FAIL 全部为 test password is missing 环境门禁 |
| 编译 | go build ./... | pass | 13.223s（gates.json） |
| 静态类型/分析 | go vet ./... | pass | 10.706s |
| Lint | golangci-lint run ./...（GOTOOLCHAIN=go1.26.11） | pass | 0 issues，1.760s |
| 层边界 | python3 scripts/check-layer-boundary.py | pass | 0.336s，0 违规 |
| 架构 | bash scripts/check-architecture.sh | pass | 18.701s |
| 现行并发门 | python3 scripts/check-naked-goroutine.py；python3 scripts/check-bare-go.py --repo . | pass | 1.520s / 2.261s |
| GORM-only | bash user-server/scripts/check-gorm-only.sh | pass | 0.558s |
| 格式与差异 | gofmt -l 两文件；git diff --check 改动文件 | pass | 无输出；rc=0 |
| 源码一致性 | SHA-256 manifest | pass | source-sha256.json |

## Test Mapping（关闭判据 → 用例）

- 缓存/去抖按租户隔离、A/B 同外部身份互不节流 → `TestLeadMiningTenant_ConfigCacheAndDebounceAreIsolated`
- 热更新仅失效对应租户 + 返回副本防污染 → `TestLeadMiningTenant_TargetedReloadOnlyInvalidatesSelectedTenant`
- 无归属消息不接受（配置读取/Judge/写入前拒绝）→ `TestLeadMiningTenant_MissingTenantRejectedBeforeConfigRead`（另由既有 `TestLeadMiningProcess_NoTenantKeepsFailClosed` 双保险）
- 去抖键携带租户、默认租户兼容键 → `TestLeadMiningTenant_DebounceKeyIncludesTenant`
- 消息行归属绑定上下文 → 既有 `TestLeadMiningProcess_BindsTenantFromMessage` 继续 PASS

## 边界（不冒充通过的部分）

- 数据库集成面（`TestLeadMining_Integration_*` 4 例）需真实 PG 凭据，本轮未跑，保持「不可判断」，待有凭据窗口补验。
- 未提交/未推送/未部署；本修复在混合工作树上，未重复全仓 `make test-go-fast`（此前批次已记录该混合树存在其他在途范围失败，与本修复无交集）。
- 判据中「热更新仅失效对应租户」的配置保存调用方现在仍走全局 reload 兼容语义（assessment Proposed Remediation 已声明保留），定向失效入口已存在并有测试；切换保存路径调用点属后续集成决策，不属于本项缺陷范围。
- 全部为内存替身与单元门禁，未连接生产库/真实渠道/LLM 上游。

## Evidence Files

- `assessment.md` / `fix.md` / `gates.json` / `source-sha256.json`
- `green.log`（专项 4 PASS）/ `race.log`（专项 race）/ `regression.log`（既有回归 + 环境门禁 FAIL 明细）
