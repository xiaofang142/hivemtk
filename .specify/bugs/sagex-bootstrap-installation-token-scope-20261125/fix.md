# Bug Fix: SageX bootstrap installation tenant/token scope

- **Slug**: `sagex-bootstrap-installation-token-scope-20261125`
- **HEAD**: `2c5c53e3ded742ac0c6ebb1944529aaed18973fd`
- **Assessment**: `./assessment.md`
- **Status**: applied / scoped GREEN / main independently verified (scoped lane)

## Summary

`BootstrapInstallation` 现在把 `identify_code` 解析出的 tenant 钉到所有安装读写，显式区分 not-found 与 DB error，并对 existing row 的 tenant/account/status 做 fail-closed 校验。同租户同账号同 installation ID 通过数据库 partial unique index + `CreateIfAbsent` + conflict re-read 保持单行/单 token；跨租户、跨账号、disabled 行不返回 token，也不创建替代行。

## Changes

| File | Change |
|---|---|
| `user-server/internal/service/sagex_upload.go` | 新增 typed bootstrap errors；`tenant.OnlyTenant` 钉住租户；显式 tenant-aware lookup；`CreateIfAbsent` + conflict re-read；ownership/status 校验；ID 最大长度校验；生成 token 后原子创建 |
| `user-server/internal/repository/sagex_upload.go` | 新增 `GetByTenantAndInstallationID`（nil DB fail-closed）与 `CreateIfAbsent`（partial-index `ON CONFLICT DO NOTHING`） |
| `user-server/internal/controller/sagex_upload.go` | bootstrap 输入/服务错误映射为 typed 400/401/403/409/500，稳定 `data.reason`；错误响应不带 token |
| `user-server/internal/model/sagex_upload.go` | 为非空 `(tenant_id, installation_id)` 增加 GORM partial unique index 标签，fresh DB/testutil AutoMigrate 同等生效 |
| `user-server/internal/migration/migrations/v3_115_0_sagex_installation_unique.go` | 新注册迁移：重复数据 preflight fail-closed、建 partial unique index、Down 仅 drop 本 lane index |
| `user-server/internal/migration/migrations/initial_schema.go` | 注册 `v3.115.0`；未触碰未注册 `v3.114.0` |
| `migrations/099_sagex_installation_tenant_unique.sql` | 根目录离线对照 SQL；重复数据 RAISE EXCEPTION，不静默去重 |
| `migrations/README.md` | 更新编号 SQL/注册迁移计数与最大版本 |

## Behavioral boundaries

- `identify_code` 仍是有效 bootstrap 凭证；未引入 Bearer、专用限流、audit 或 UI 改动。
- 空 installation ID 继续按现有 `inst_...` 契约生成；只校验 schema 最大长度 100，不强制 UUID。
- 唯一键是 **tenant-scoped**：租户 A 使用租户 B 的 UUID 时，A 可在自己租户内建立同名行，但绝不会读取/修改 B 行或取得 B token。该行为由真实 PostgreSQL + repository + resolver 回归覆盖；未增加 SystemScope 全局探测或全局唯一约束。
- existing row 的 tenant/account/status 校验仍作为 service 防线；fake repository 回归覆盖“错误 repo 返回异租户行”时必须拒绝且零 create。
- Upload Bearer-token 契约与旧 `SageXUpload_*` 协议测试漂移不在本 lane，未修改。

## Tests added

- Service：tenant 钉住、cross-tenant/cross-account/disabled 零 token 零替代创建、DB error 零 create、同 owner 幂等、unique-conflict re-read、超长 ID、空 ID、invalid identify code、真实 repo 跨租户 UUID 边界。
- Repository：partial unique、跨租户同 UUID 隔离、空 ID 排除、并发单 winner、`CreateIfAbsent` 幂等/并发单 token、nil-DB fail-closed。
- Controller：typed 400/401/403/409/500 + machine reason、错误响应无 token、成功响应 token。
- Migration：注册、partial index、重复数据 fail-closed、幂等、Down 仅删 index、nil DB fail-closed。

## Local verification

- Service bootstrap：11/11 PASS（含真实 PostgreSQL 跨租户用例）。
- Repository bootstrap：5/5 PASS；并发两用例 `-count=3` 全 PASS。
- Controller bootstrap：2 top-level + 10 subtests PASS。
- Migration `v3.115.0`：4/4 PASS。
- `go build ./...`、`go vet ./...`、layer boundary、architecture：rc=0。
- 旧 Upload 协议测试：clean HEAD `2c5c53e3` 与当前工作树同 regex 均为 4 service + 2 controller `unauthorized` 失败，分类为 pre-existing protocol-test drift，留待 `SAGEX-UPLOAD` lane。

## Deviations from assessment

无生产范围扩张。唯一明确边界：按主智能体裁决，tenant-scoped 唯一键允许 A 租户建立自己的同名 installation 行；不将其视为替换 B 行，也不新增全局探测/全局唯一约束。

## Follow-ups

- 主智能体独立复验 Bootstrap scoped gates。
- 旧 Upload identify_code/token 协议测试迁移留给 `SAGEX-UPLOAD` 后续 lane。
- 未 commit/push/deploy/真实外发/UI。
