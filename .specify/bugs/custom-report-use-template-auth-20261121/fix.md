# Bug Fix: 复制报表模板沿用读取授权（FA-24）

- **Slug**: custom-report-use-template-auth-20261121
- **Fixed**: 2026-11-21
- **Status**: applied

## Summary

`CustomReportService.UseTemplate` 原先直接委托仓储按 ID 复制任意同租户报表，普通用户可复制读不到的私有报表到名下再读配置。现改为服务层先按 GetReport 同一授权（公开/本人/管理员）判定，拒绝时 `ErrReportForbidden` 且零新行；控制器经 `extractUserContext` 下传 isAdmin。跨租户隔离由既有租户仓储守卫承担。

## Changes

| File | Change |
|---|---|
| `internal/ops/service/custom_report.go` | `UseTemplate` 增加 `isAdmin` 参数与读取授权判定（GetByID → 授权 → 复制） |
| `internal/ops/controller/custom_report.go` | `UseTemplate` 改用 `extractUserContext`，授权随 isAdmin 下传 |
| `internal/ops/service/custom_report_use_template_auth_test.go` | 新增：授权矩阵（拒绝零新行/公开/本人/管理员/不存在） |
| `internal/ops/service/custom_report_test.go` | 既有用例适配新签名（本人公开模板，false） |

## Local Verification

- `go test ./internal/ops/... -run 'CustomReport|TestUseTemplate' -count=1 -v`：41 PASS / 0 FAIL（green.log，含控制器/仓储/服务全回归）。
- 专项 `-race`：通过（race.log）。
- 全仓 build 通过。证据与源码哈希见本目录。
