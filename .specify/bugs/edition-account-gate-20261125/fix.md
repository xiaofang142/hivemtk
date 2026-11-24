# Bug Fix: 渠道账号统一 edition gate

- **Slug**: edition-account-gate-20261125
- **Fixed**: 2026-11-25T02:05:00+08:00
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

在 `ChannelAccountRepository` 建立统一 edition gate，覆盖读取、识别码解析与写侧；国内渠道码只在 `edition` 包保留单一事实源并以副本导出。opensource/unknown 不再暴露或操作国内/孤儿账号，custom 与国外渠道保持可用。

## Approved Scope

主智能体批准生产文件：

- `user-server/internal/pkg/edition/edition.go`
- `user-server/internal/repository/channel_account.go`

批准测试文件：

- `user-server/internal/repository/edition_channel_account_repository_matrix_test.go`
- `user-server/internal/repository/edition_channel_account_boundary_test.go`
- `user-server/internal/repository/edition_channel_account_write_test.go`
- `user-server/internal/repository/edition_channel_matrix_test.go`
- `user-server/internal/service/edition_channel_account_matrix_test.go`

不改外部在途的 `service/channel_account.go`，也不在 service/dispatch 叠第二套版本判断。

## Changes

| File | Change | Notes |
|------|--------|-------|
| `internal/pkg/edition/edition.go` | modified | 国内码收敛为单一 slice；新增 `DomesticChannelCodes()` 返回副本 |
| `internal/repository/channel_account.go` | modified | 新增 `applyChannelEditionGate` 与写侧渠道校验；所有读写路径统一 gate |
| `internal/repository/edition_channel_account_repository_matrix_test.go` | added | GetByID/List/GetByIDs/两种 GetByIdentifyCode、跨租户、孤儿、国外渠道矩阵 |
| `internal/repository/edition_channel_account_boundary_test.go` | added | 分页前过滤、国内权威码假歧义、混合码回落 |
| `internal/repository/edition_channel_account_write_test.go` | added | Create/Update 迁移/Delete/DeleteByChannel/SetIdentifyCodeByChannel 零影响 |
| `internal/repository/edition_channel_matrix_test.go` | added | ChannelRepository 版本矩阵 + edition 副本不可变 |
| `internal/service/edition_channel_account_matrix_test.go` | added | ListOptions/Get/PickAccount 直传 ChannelID 回归 |

## Diff Highlights

```go
func DomesticChannelCodes() []string {
    return append([]string(nil), domesticChannelCodes...)
}
```

```go
func applyChannelEditionGate(ctx context.Context, q *gorm.DB) *gorm.DB {
    if edition.AllowsContext(ctx, edition.FeatureDomesticChannels) {
        return q
    }
    return q.Where(
        "channel_id IN (SELECT id FROM channels WHERE code NOT IN ?)",
        edition.DomesticChannelCodes(),
    )
}
```

`GetByIdentifyCode` 的账号冗余码、权威识别码表、按渠道反查账号三条查询均在 `Limit(2)` 与歧义判断前应用 gate；`tenant.WithSystemScope` 只影响租户隔离，edition 仍使用原 ctx。

## Tests Added or Updated

- `TestEditionDomesticChannelCodesReturnsCopy`
- `TestEditionChannelRepositoryMatrix`
- `TestEditionChannelAccountRepositoryMatrix`
- `TestEditionChannelAccountBoundaryMatrix`
- `TestEditionChannelAccountWriteGateMatrix`
- `TestEditionChannelAccountAndDispatchMatrix`

## Local Verification

### RED（修复前，clean `934825c2d`）

- repository 读取矩阵：opensource/unknown 在 GetByID/List/GetByIDs/两种识别码路径泄漏；custom/国外渠道通过。
- service 矩阵：ListOptions/Get/PickAccount 直传国内 ChannelID 旁路。
- boundary：分页未先过滤、国内权威码假歧义、国内账号码遮蔽国外权威码。
- write：Create 国内/孤儿放行、Update 禁用目标被改、Delete/批量写影响禁用行。
- `TestEditionDomesticChannelCodesReturnsCopy` 为缺 API 的编译 RED，不作为行为断言。

日志：`check-d-account-repo-red-r2.log`、`check-d-edition-account-dispatch-r3.log`、`check-d-boundary-red-r2.log`、`check-d-write-red-r2.log`、`check-d-edition-copy-red.log`。

### GREEN（clean `934825c2d` + 本修复）

- `go test ./internal/repository ./internal/service -run '^(TestEdition|TestChannelDispatchService_)' -count=1 -v` → rc=0。
- `go test ./internal/repository -count=1 -timeout 10m` → rc=0，`ok ... 29.491s`。
- `go build ./...` → rc=0。
- `go vet ./...` → rc=0。
- `python3 scripts/check-layer-boundary.py` → rc=0，1753 files / 0 新增。
- `bash scripts/check-architecture.sh` → rc=0，仅既有 `wecomCSService` warning。
- service 全包尝试 → rc=1；除 `TestLeadMiningTenant_ConfigCacheAndDebounceAreIsolated` nil-pointer 外，另有 12 个失败用例。主智能体在未修改 `934825c2d` 基线快照重跑该 12 个用例，12/12 同样 FAIL（`main-d-baseline-failures.log`，16.589s）；加上 nil-pointer 基线复现，共 13 个 full-service 失败均为存量，不是本修复引入。

日志：`check-d-all-targeted-green-r2.log`、`check-d-repository-full-r2.log`、`check-d-build.log`、`check-d-vet.log`、`check-d-layer.log`、`check-d-architecture.log`、`check-d-service-full.log`、`check-baseline-service-failing-single.log`。

### 实际工作树（回植后）

- 7 个 scope 文件与 clean 快照逐字节 IDENTICAL；外部 `service/channel_account.go` 未改。
- `go test ./internal/repository ./internal/service -run '^(TestEdition|TestChannelDispatchService_)' -count=1 -v` → rc=0，repository 7.946s / service 5.468s。
- `go build ./...` → rc=0。
- `go vet ./...` → rc=0。
- `python3 scripts/check-layer-boundary.py` → rc=0，1756 files / 0 新增。
- `bash scripts/check-architecture.sh` → rc=0，仅既有 `wecomCSService` warning。
- 当前实际 service/vet 不再被外部 `lead_miner_unified_test.go` 阻断。

日志：`check-actual-d-targeted.log`、`check-actual-d-build.log`、`check-actual-d-vet.log`、`check-actual-d-layer.log`、`check-actual-d-architecture.log`、`d-backport-postcheck.txt`。

## Deviations from Assessment

无。主智能体批准写侧同批收口后，assessment 的两个 Open Question 已标注裁决；生产改动严格限制在批准的两个文件。

## Follow-ups

- 回植实际仓后需重新跑 targeted/build/vet/layer/architecture；实际 service 包仍可能被外部 `lead_miner_unified_test.go` 阻断。
- 存量孤儿账号在 custom 仍可读，符合批准语义；新 Create 一律拒绝孤儿渠道。
- 不 commit/push、不部署、不做 UI/真实外发。
