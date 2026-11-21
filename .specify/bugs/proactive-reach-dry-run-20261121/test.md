# Bug Verification: FA-21 触达预检零副作用

- **Slug**: proactive-reach-dry-run-20261121
- **Tested**: 2026-11-21
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: partial

## Summary

原始问题已用本地发送 spy 实际复现，修后同路径不再复现；手机号、邮箱、两者并存和客户选路均只返回 dry_run，不解析短信发送器、不外发、不消耗发送额度，正式发送仍受退订/审批/合规与适用的客户冷却约束。新增HTTP六场景和race通过。

结果为 partial 是因为完整混合工作树并未通过交付门禁：全量Go七包失败，make audit还有其他会话的索引文档断链。不能据专项通过宣称全仓或线上验收。

## Checks Performed

| Check | Command / Action | Result | Notes |
|---|---|---|---|
| 原始复现 RED | go test ./internal/service -run '^TestProactiveReachDryRun_' -count=1 -v（修前） | reproduced | red.log；显式预检实际调用spy、记sent并耗日额度；虚构账号/无账号假成功成立 |
| 修后专项及既有闸口 | go test ./internal/service ./internal/controller -run 'TestProactiveReachDryRun_|TestProactiveReachAPI_|TestReachGate_|TestEnforceCompliance_|TestReachByCustomer_|TestProactiveReachService_PickChannel' -count=1 -v | pass | green-final.log；service 1.839s、controller 1.358s；51个PASS条目含父项/子项，非51个独立主用例 |
| 新增专项race | go test -race ./internal/service ./internal/controller -run 'TestProactiveReachDryRun_|TestProactiveReachAPI_DryRunNeverSends' -count=1 -v | pass | race.log；service 3.121s、controller 2.802s；25个PASS条目含父项/子项，无race |
| 编译 | go build ./... | pass | build.log，19.036s |
| 静态类型/分析 | go vet ./... | pass | vet.log，23.512s |
| 层边界 | python3 scripts/check-layer-boundary.py | pass | 1626非测试Go、0违规/0新增 |
| 架构 | bash scripts/check-architecture.sh | pass | architecture.log，29.949s |
| GORM-only | bash user-server/scripts/check-gorm-only.sh | pass | gorm-only.log |
| Lint | make lint | pass | golangci-lint 2.10.0，0 issues |
| 现行并发门 | python3 scripts/check-bare-go.py --repo .；python3 scripts/check-naked-goroutine.py | pass | 81/81；2401文件0 hit |
| 旧版裸go扫描 | bash user-server/scripts/check_naked_goroutine.sh | fail | goroutines.log：枚举大量存量含recover/豁免的站点；不能称此命令通过 |
| 总审计 | make audit | fail | audit.log；docs/INDEX.md:57指向本机存在但未暂存integration/PILOT-CONTRACT.md，是其他会话在途变更；本轮未替其stage |
| 完整Go回归 | make test-go-fast | fail | full-go.log；make rc=2；152条ok包结果（含testutil认证预检）、7个失败包、85顶层失败用例；service耗时287.408s；controller整包61.130s通过 |
| 无本修复的失败对照 | go test -overlay baseline-overlay.json ./internal/bridge -run '^TestSendManualMessage_AccountOwnershipDoesNotAuthorizeShop$' -count=1 -v | reproduced | baseline-bridge.log：本轮唯一生产文件改用Git HEAD原版编译，仍expected capability_mapping_missing/got capability_storage_unavailable |
| 格式与差异 | gofmt -l 三个源码文件；两仓针对改动的git diff --check | pass | gofmt无输出；diff-check rc=0 |
| 验证源码一致性 | SHA-256 manifest对照 | pass | source-sha256.json与verification-summary.json，source_drift=[] |

测试连接只使用专用127.0.0.1:8217的mtk-postgres-test；凭据由容器环境读入，不打印、不写日志。全部发送器为本地替身。

## Full Regression Failure Boundary

完整七个失败包为 internal/bridge、internal/capregistry、internal/channelgw、internal/migration/migrations、internal/repository、internal/router、internal/service。

代表断言：bridge和service路径为capability_storage_unavailable/事务回滚；capregistry为能力文档与在途注册表不一致；迁移/仓储/服务含新增merchant_callback提交及水位用例；router为新增merchant-callbacks匿名路由清单与注解契约。上述文件在本轮开始时已修改或新增，本轮未更改。仅对一个bridge代表用例做了原版生产文件overlay对照，不声称85个失败均已逐项完成因果定位。明细可由full-go-summary.json和full-go.log复算。

## Output Excerpts

```text
ok hivemtk-user/internal/service 1.839s
ok hivemtk-user/internal/controller 1.358s
# race
ok hivemtk-user/internal/service 3.121s
ok hivemtk-user/internal/controller 2.802s
# full
FAIL hivemtk-user/internal/service 287.408s
0 issues.
```

## Residual Risks

- 当前分支chore/workspace-consolidate-20261121含大量其他会话差异，未混合stage/提交/推送/发布。
- 未真实发送短信、邮件或渠道消息；未写生产、未进行浏览器页面/生产验收。
- 预检是只读选路预览，按既有契约不执行发送审批或占位，因此不表示正式发送保证获准；最终发送仍重新判定。
- 旧版check_naked_goroutine.sh与当前正式并发门的结果已分别记录，未删除脚本或修改基线来制造绿门。

## Recommendation

FA-21原清单状态更新为“本地修复已验、整体交付待验”，保留开放，不另建任务编号。完整门禁和归属明确的提交/链A交付核验通过后再销项。本轮不以其他会话正在修改的文件为绿门而越界修复，也不将本报告记为生产上线证据。
