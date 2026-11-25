# Bug Verification: SageX bootstrap installation tenant/token scope

- **Slug**: `sagex-bootstrap-installation-token-scope-20261125`
- **HEAD**: `2c5c53e3ded742ac0c6ebb1944529aaed18973fd`
- **Assessment**: `./assessment.md`
- **Fix**: `./fix.md`
- **Result**: main independently verified (scoped lane)

## Summary

Bootstrap 的 tenant 钉住、not-found/DB-error 分流、ownership/status 校验、原子幂等、typed controller errors 与 v3.115.0 partial unique index 均已通过专项回归。真实 PostgreSQL 回归证明：租户 A 的 valid identify_code + 租户 B 的 installation UUID 不会取得 B token 或修改 B 行；A 只在自己租户内建立同名行。

旧 `SageXUpload_*` 协议测试仍把 token 当 identifyCode 使用，在 clean HEAD `2c5c53e3` 与当前工作树产生完全相同的 6 个 `unauthorized` 失败。该问题为 pre-existing protocol-test drift，不属于 Bootstrap lane。

## RED evidence

| Check | Log | Result |
|---|---|---|
| Service bootstrap behaviors | `check-sagex-bootstrap-service-red.log` | rc=1；7 fail / 3 pass，失败点均为目标缺陷 |
| Repository partial unique + concurrency | `check-sagex-bootstrap-repository-red-r2.log` | rc=1；2 fail（重复被接受、12 个并发 winner） |
| Repository atomic seam | `check-sagex-bootstrap-repository-atomic-red.log` | rc=1；2 fail（缺少 `CreateIfAbsent`/tenant lookup seam） |
| Controller typed errors/token boundary | `check-sagex-bootstrap-controller-red-r2.log` | rc=1；typed-error 10 subtests + success token 用例失败 |
| Migration registration/behavior | `check-sagex-bootstrap-migration-red.log` | rc=1；4 fail（v3.115.0 未注册） |
| Repository nil-DB lookup | `check-sagex-bootstrap-repository-nil-db-red-r2.log` | rc=1；nil DB 触发 panic 而非 `ErrInvalidDB` |

## GREEN evidence

| Check | Command / scope | Result | Log |
|---|---|---|---|
| Service bootstrap | `go test ./internal/service -run '^TestSageXBootstrapInstallation' -count=1 -v` | 11/11 PASS | `check-sagex-bootstrap-final-three-packages.log` |
| Real repo cross-tenant UUID | 同上中的 `TestSageXBootstrapInstallationCrossTenantUUIDWithRealRepository` | PASS；B token 未返回、B 行未修改、A 同名行仅 1 条 | `check-sagex-bootstrap-real-repo-cross-tenant.log` |
| Repository bootstrap | `go test ./internal/repository -run '^TestSageXInstallationRepository' -count=1 -v` | 5/5 PASS | `check-sagex-bootstrap-final-three-packages.log` |
| Repository concurrency | 两个并发用例 `-count=3 -v` | 6/6 PASS | `check-sagex-bootstrap-repository-concurrency-r3.log` |
| Controller bootstrap | `go test ./internal/controller -run '^TestSageXBootstrapController' -count=1 -v` | 2 top-level + 10 subtests PASS | `check-sagex-bootstrap-controller-green.log` |
| Migration v3.115.0 | `go test ./internal/migration/migrations -run '^TestSageXInstallationUniqueMigration' -count=1 -v` | 4/4 PASS | `check-sagex-bootstrap-migration-green.log` |
| Three-package scoped gate | `go test -p 1 ./internal/service ./internal/repository ./internal/controller -run '^(TestSageXBootstrap\|TestSageXInstallationRepository)' -count=1 -v` | rc=0；service 11、repository 5、controller 2+10 subtests | `check-sagex-bootstrap-final-three-packages.log` |
| Build | `go build ./...` | rc=0 | `check-sagex-bootstrap-build-final.log` |
| Vet | `go vet ./...` | rc=0 | `check-sagex-bootstrap-vet-final.log` |
| Layer boundary | `python3 scripts/check-layer-boundary.py` | rc=0；1758 files / 0 new violations | `check-sagex-bootstrap-layer.log` |
| Architecture | `bash scripts/check-architecture.sh` | rc=0；仅既有 `wecomCSService` warning | `check-sagex-bootstrap-architecture.log` |

## Pre-existing Upload protocol-test drift

Clean detached HEAD `2c5c53e3ded742ac0c6ebb1944529aaed18973fd`：

```text
go test -p 1 ./internal/service ./internal/controller -run 'SageX' -count=1 -v
```

结果 rc=1，且只有以下 6 个旧协议用例失败：

- Service：`TestSageXUpload_DuplicateInBatchNotAcked`
- Service：`TestSageXUpload_IdempotentReceiptReturnsDuplicate`
- Service：`TestSageXUpload_MapsQQAndWeChat`
- Service：`TestSageXUpload_UnsupportedPackageRejected`
- Controller：`TestSageXUploadController_ReturnsProtocolAck`
- Controller：`TestSageXUploadController_RealV1Payload`

当前工作树同 regex 的 6 个失败与 clean HEAD 完全一致，均为 `sagex: unauthorized` / HTTP 401。根因是旧测试仍把 token 当 identifyCode 传入，而 HEAD 的 `HandleUpload`/Controller 已按 identify_code 解析。证据：

- `check-sagex-upload-clean-head2c5-baseline.log`
- `check-sagex-bootstrap-three-packages-targeted.log`

本 lane 未修改这些旧协议用例；最终 GREEN 命令限定到 Bootstrap/新增回归 regex。旧用例迁移留给 `SAGEX-UPLOAD` 后续 lane。

## Cross-tenant boundary verified

真实 PostgreSQL + repository + resolver 回归确认：

1. tenant A 的 valid identify_code 可解析出 tenant A/account A。
2. tenant B 已存在 `installation_id=shared-db-installation-id`、`token=victim-db-token`。
3. A 使用同 UUID bootstrap 后得到 tenant A 自己的 token，绝不等于 B token。
4. B 行的 token/account/status 保持不变。
5. A 租户内只建立一条同名行；唯一键是 tenant-scoped，不读取/修改 B，也不做 SystemScope 全局探测。

## Residual risks / deferred scope

- Upload Bearer-token 契约与旧协议测试迁移：deferred 到 `SAGEX-UPLOAD`。
- Bootstrap 专用限流/audit、UI/真机、全局 installation UUID 唯一策略：未纳入本 lane。
- 未 commit/push/deploy/真实外发/UI。
- 主智能体独立复验完成：PASS（scoped lane）。证据 `main-sagex-bootstrap-verification-20261125.txt`；脚本 `main-sagex-bootstrap-verification-summary.txt` 记录 2026-11-25 10:35:46–10:37:28 `overall_rc=0`，HEAD 无漂移、11 个关键文件哈希稳定。主验重跑 service 11/11（含真实 PostgreSQL 跨租户 UUID）、repository 5/5、并发 6/6、controller 2+10、migration 4/4、三包 scoped gate、build/vet/layer/architecture 均 rc=0。该结论仅限 Bootstrap scoped acceptance；deferred/boundaries 同上。
