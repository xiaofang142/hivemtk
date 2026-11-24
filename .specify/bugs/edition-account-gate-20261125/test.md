# Bug Verification: 渠道账号统一 edition gate

- **Slug**: edition-account-gate-20261125
- **Tested**: 2026-11-25T02:09:30+08:00
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: verified

## Summary

原始版本门旁路已在 clean `934825c2d` 快照复现并修复：opensource/unknown 不再通过账号读取、识别码解析、选号或写操作接触国内/孤儿账号；custom 与国外渠道行为保持。回植后实际工作树的 D 定向矩阵、build、vet、layer、architecture 全部通过。full-service 的 13 个失败均在未修改基线同样复现，属存量，不改变本 bug 的 verified 结论。

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| Reproduction (pre-fix) | clean HEAD repository/service/boundary/write 矩阵 | fail（预期 RED） | 版本门旁路、假歧义、分页泄漏、写侧零影响缺失 |
| Edition API | `TestEditionDomesticChannelCodesReturnsCopy` | pass | 返回副本，调用方修改不影响单一事实源 |
| Repository matrix | `TestEditionChannelAccountRepositoryMatrix` | pass | GetByID/List/GetByIDs/两种识别码/跨租户/孤儿/国外渠道 |
| Boundary matrix | `TestEditionChannelAccountBoundaryMatrix` | pass | 分页前 gate、国内权威码不假歧义、混合码回落 |
| Write matrix | `TestEditionChannelAccountWriteGateMatrix` | pass | Create/Update 迁移/Delete/DeleteByChannel/SetIdentifyCodeByChannel |
| Service/dispatch matrix | `TestEditionChannelAccountAndDispatchMatrix` | pass | ListOptions/Get/PickAccount 直传 ChannelID |
| Repository full（clean） | `go test ./internal/repository -count=1 -timeout 10m` | pass | rc=0，`ok ... 29.491s`；主智能体独立复跑 34.464s 亦 PASS |
| Build（clean） | `go build ./...` | pass | rc=0 |
| Vet（clean） | `go vet ./...` | pass | rc=0 |
| Layer gate（clean） | `python3 scripts/check-layer-boundary.py` | pass | 1753 files / 0 新增 |
| Architecture gate（clean） | `bash scripts/check-architecture.sh` | pass | 仅既有 `wecomCSService` warning |
| Backport integrity | 7 个 scope 文件 cmp clean snapshot | pass | 全部 IDENTICAL；外部 `service/channel_account.go` 未改 |
| Targeted（actual） | `go test ./internal/repository ./internal/service -run '^(TestEdition|TestChannelDispatchService_)' -count=1 -v` | pass | rc=0；repository 7.946s / service 5.468s |
| Build（actual） | `go build ./...` | pass | rc=0 |
| Vet（actual） | `go vet ./...` | pass | rc=0 |
| Layer gate（actual） | `python3 scripts/check-layer-boundary.py` | pass | 1756 files / 0 新增 |
| Architecture gate（actual） | `bash scripts/check-architecture.sh` | pass | 仅既有 `wecomCSService` warning |
| Full service（clean） | `go test ./internal/service -count=1 -timeout 30m` | fail（存量） | 13 个失败；基线对照见下 |

## Baseline Comparison

clean 修复树 full-service 失败后，主智能体在**未修改的 `934825c2d` 基线快照**重跑除 lead-mining panic 外的 12 个失败用例：

- `main-d-baseline-failures.log`：12/12 同样 FAIL，16.589s。
- `TestLeadMiningTenant_ConfigCacheAndDebounceAreIsolated`：修复树与基线单测均复现同一 `LeadMiningNotifyRouteRepository` nil-pointer。
- 合计 13 个 full-service 失败均为存量，不是 edition gate 引入；本阶段不修、不碰外部文件。

## Output Excerpts

clean targeted：

```text
PASS
ok hivemtk-user/internal/repository
PASS
ok hivemtk-user/internal/service
```

clean repository full：

```text
ok hivemtk-user/internal/repository 29.491s
rc=0
```

actual targeted：

```text
PASS
ok hivemtk-user/internal/repository 7.946s
PASS
ok hivemtk-user/internal/service 5.468s
rc=0
```

actual build/vet/layer/architecture：均为 rc=0。

## Residual Risks

- full-service 仍有 13 个基线失败，需其他 lane 或专项处理；不影响本版本门修复结论。
- 未做 UI、真实外发、部署或生产验收。
- 存量孤儿账号在 custom 仍可读，符合批准语义；新 Create 一律拒绝孤儿渠道。

## Recommendation

本 bug 的代码修复与回归已验证，建议主智能体独立复验后关闭 D；13 个存量 full-service 失败单独保留，不并入本 bug。
