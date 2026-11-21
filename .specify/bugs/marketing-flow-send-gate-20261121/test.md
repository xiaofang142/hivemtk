# Bug Verification: FA-20 营销流程短信与邮件动作统一发送闸口

- **Slug**: marketing-flow-send-gate-20261121
- **Tested**: 2026-11-21
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: pass（本地专项与门禁全绿；整体交付口径见「边界」）

## Summary

判定链矩阵（internal/service，6 用例）与 content 层栅栏（4 用例）全部通过，两包 -race 无竞争：未装配 fail-closed、审批拒绝、DNC 命中、静默时段、日上限配额不足均零真实传输调用；放行路径恰好传输一次。流程暂停/停用后，在途执行的发送动作零外发、执行以 failed 收口且带可诊断原因；active 对照组照常发送。既有 `TestMarketingFlow*` 45 PASS / 0 SKIP / 0 FAIL（含 FA-18/19 图行为回归），触达/合规/线索租户回归 18 PASS。全部 8 项静态门 rc=0，lint 0 issues。

关闭判据对照：所有营销动作走同一发送意图与判定链（判定链唯一出口 `enforceMarketingFlowOutboundGates`：DNC → enforceOutboundGates，与 ReachByCustomer 显式手机号/邮箱路径同序同源）✔；假发送器覆盖撤回、暂停（content 层栅栏两用例）、DNC、审批拒绝、静默期、配额不足均断言零外发 ✔；未实际发送短信或邮件 ✔。

## Checks Performed

| Check | Command / Action | Result | Notes |
|---|---|---|---|
| 判定链矩阵 | go test ./internal/service -run 'TestMarketingFlowSendGate_' -count=1 -v | pass | green-gate.log，6 PASS / 0 FAIL，1.335s |
| 判定链 race | 同上 -race | pass | race-gate.log，2.019s 无竞争 |
| content 层栅栏 | go test ./internal/content/service -run 'TestMarketingFlowSendGate_' -count=1 -v | pass | green-content.log，4 PASS（含 paused/inactive 两子例），1.266s |
| content 层 race | 同上 -race | pass | race-content.log，2.230s 无竞争 |
| 营销流程全回归 | go test ./internal/content/service -run 'TestMarketingFlow' -count=1 -v | pass | regression-content.log，45 PASS / 0 SKIP / 0 FAIL（专用快测实例 127.0.0.1:8217，凭据由容器环境读入，未打印不落日志） |
| 触达/合规/线索回归 | go test ./internal/service -run 'TestEnforceCompliance\|TestProactiveReach\|TestReachByCustomer\|TestLeadMiningTenant' -count=1 -v | pass | regression-reach.log，18 PASS |
| 编译 | go build ./... | pass | 14.573s（gates.json） |
| 静态类型/分析 | go vet ./... | pass | 9.852s |
| Lint | golangci-lint run ./...（GOTOOLCHAIN=go1.26.11） | pass | 0 issues（首跑 gofmt 1 处，已修后复跑 rc=0） |
| 层边界 | python3 scripts/check-layer-boundary.py | pass | 0.284s，0 违规 |
| 架构 | bash scripts/check-architecture.sh | pass | 20.540s |
| 现行并发门 | python3 scripts/check-naked-goroutine.py；python3 scripts/check-bare-go.py --repo . | pass | 1.861s / 2.368s |
| GORM-only | bash user-server/scripts/check-gorm-only.sh | pass | 0.871s |
| 格式与差异 | gofmt -l 改动文件；git diff --check 改动文件 | pass | 无输出；rc=0 |
| 源码一致性 | SHA-256 manifest（9 个改动/新增文件） | pass | source-sha256.json |

## Test Mapping（关闭判据 → 用例）

- 撤回/暂停 ⇒ 零外发 → `TestMarketingFlowSendGate_PausedFlowStopsSendsMidFlight`（paused/inactive 两子例；另 TriggerFlow 入口的非 active 拒绝为既有行为）
- DNC ⇒ 零外发 → `TestMarketingFlowSendGate_DNCSendsNothing`（短信+邮箱大小写归一）
- 审批拒绝 ⇒ 零外发 → `TestMarketingFlowSendGate_DeniedApprovalSendsNothing`
- 静默期 ⇒ 零外发 → `TestMarketingFlowSendGate_QuietHoursSendsNothing`
- 配额不足 ⇒ 零外发 → `TestMarketingFlowSendGate_DailyLimitSendsNothing`
- 未装配闸门 fail-closed → `TestMarketingFlowSendGate_NotAttachedFailsClosed`
- 放行恰好一次 → `TestMarketingFlowSendGate_AllowedPathCallsTransportExactlyOnce`
- 邮件 fail-closed/配置传递 → `TestMarketingFlowSendGate_EmailFailsClosedWithoutSender`、`TestMarketingFlowSendGate_EmailPassesConfigToRegisteredSender`

## 边界（不冒充通过的部分）

- 全部为内存替身与本地快测实例；未连接生产库、未发送真实短信/邮件、未验证真实页面。
- 未提交/未推送/未部署；本修复在混合工作树上，未重复全仓 `make test-go-fast`（此前批次已记录该混合树存在其他在途范围失败）。
- 判定链拒绝使动作节点失败（执行 failed）——与同步 AI 工具路径语义一致；自动退避/邮件服务收敛（FA-EMAIL-01/02/03）为独立范围。
- 生产装配（main.go 挂闸门）属启动路径，本次仅静态验证与单测，真实进程装配在链 A 交付时核验。

## Evidence Files

- `assessment.md` / `fix.md` / `gates.json` / `source-sha256.json`
- `green-gate.log` / `race-gate.log` / `green-content.log` / `race-content.log` / `regression-content.log` / `regression-reach.log`
