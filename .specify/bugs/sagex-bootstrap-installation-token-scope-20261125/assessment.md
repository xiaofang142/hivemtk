# Assessment: SageX BootstrapInstallation tenant/token scope

- **Slug**: `sagex-bootstrap-installation-token-scope-20261125`
- **Date**: 2026-11-25
- **HEAD**: `2c5c53e3ded742ac0c6ebb1944529aaed18973fd`
- **Lane**: assess-only
- **Verdict**: valid tenant-boundary defect; current public HTTP path is only partially shielded by the GORM tenant callback
- **Dynamic tests**: not run（主智能体正在独立复验 user A/D/E；本评估只使用源码与 Git 只读证据）

## 1. Conclusion

`BootstrapInstallation` resolves `identify_code` to a trusted tenant/account, but then looks up `installation_id` using the **original request context** and returns the existing row—including its token—without checking the row's tenant, account, or status.

The exact impact depends on runtime context:

| Scenario | Result |
|---|---|
| Public HTTP + `TENANT_MODE=on` + normal tenant registry | `GetByInstallationID` fails closed because the public bootstrap route has no tenant binding. The service **swallows that DB error**, creates a new active row under the `identify_code` tenant using the supplied UUID, and returns a **new token for that tenant**. It does not return the victim tenant's token on this path. |
| `TENANT_MODE=off` while the DB still contains multiple tenant rows | Tenant callbacks are disabled; lookup is global. A valid `identify_code` plus another tenant's installation UUID can return that tenant's row and token. |
| Service called with `SystemScope`, or with a context bound to the victim tenant | Lookup can read the victim row; no tenant/account/status comparison prevents token disclosure. |
| Same tenant + same account + same UUID, public HTTP in on mode | Every retry hits the swallowed missing-tenant error and attempts another create. `installation_id` is only indexed, not unique, so retries mint duplicate rows/tokens instead of being idempotent. |
| Existing row is disabled (`status=0`) | If lookup reaches the existing-row branch, its token is still returned. In public on-mode HTTP, the swallowed lookup error instead creates a new `status=1` duplicate, bypassing revocation. |
| Repository read error | Any error is treated like “not found”; create is attempted and the controller maps the eventual error to `401 bootstrap 失败`, masking infrastructure failures. |

Important impact limiter: the current `/api/integrations/sagex/upload` path does **not consume the Bearer token at all**. `extractBearerToken` is unused and `SageXInstallationStore.GetByToken` has no production caller. Therefore a disclosed token does not currently grant upload access by itself; `identify_code` is the effective credential. This does not make token disclosure acceptable—the token is still a protocol credential, and the implementation contradicts the documented Bearer-token contract.

## 2. Read-only evidence

### 2.1 Public route has no tenant/auth binding

`user-server/internal/router/router.go:770-773`:

```go
sagexGroup := r.Group("/api")
sagexGroup.Use(middleware.InitGuard())
sagexGroup.POST("/integrations/sagex/upload", sagexCtrl.Upload)
sagexGroup.POST("/integrations/sagex/bootstrap", sagexCtrl.Bootstrap)
```

There is no `TenantMiddleware`, JWT middleware, or bootstrap-specific credential middleware. `InitGuard` only checks installation state.

### 2.2 Trusted tenant is resolved, then discarded for lookup

`user-server/internal/service/sagex_upload.go:593-618`:

```go
resolved, err := s.resolver.ResolveByIdentifyCode(ctx, identifyCode)
// ...
if existing, err := s.instRepo.GetByInstallationID(ctx, installationID); err == nil && existing != nil {
    return existing, nil
}
inst := &model.SageXInstallation{
    InstallationID: installationID,
    AccountID:      strconv.FormatUint(uint64(resolved.AccountID), 10),
    TenantID:       resolved.TenantID,
    Status:         1,
}
```

The lookup uses `ctx`, not `tenant.OnlyTenant(ctx, resolved.TenantID)` or an equivalent tenant-scoped context. The existing-row branch performs no `TenantID`, `AccountID`, or `Status` check.

### 2.3 Repository query has no explicit tenant predicate

`user-server/internal/repository/sagex_upload.go:66-78`:

```go
err := r.db.WithContext(ctx).Where("installation_id = ?", installationID).First(&inst).Error
```

Safety depends entirely on the ambient GORM tenant callback. In off mode there is no filter; in on mode an unbound context errors.

### 2.4 On-mode missing-tenant query error is swallowed

`user-server/internal/pkg/tenancyidx/tenant_callback.go:193-219` injects `tenant_id = ?` or fails closed when the context is unbound. `BootstrapInstallation` only accepts `err == nil`; every non-nil error falls through to create.

Create can still succeed because `sagex_installations` has `tenant_id`, and the create callback accepts an unbound context when the row carries an explicit nonzero tenant ID (`tenant_callback.go:445-456`). The new row carries `resolved.TenantID`, so the public on-mode request creates under the resolved tenant despite the failed read.

### 2.5 Schema does not enforce bootstrap idempotency

`user-server/internal/model/sagex_upload.go:18-24`:

```go
InstallationID string `gorm:"type:varchar(100);index" json:"installation_id"`
Token          string `gorm:"type:varchar(200);not null;uniqueIndex" json:"-"`
```

There is no unique `(tenant_id, installation_id)` key. Concurrent or repeated bootstrap calls can create multiple rows/tokens for the same tenant and UUID. `First(&inst)` also cannot detect duplicates.

The token's single-column `uniqueIndex` is subject to `tenancyidx.Normalize`, which normally rebuilds tenant-table unique indexes as `(tenant_id, token)`. That permits the same token string in different tenants, while `GetByToken` uses `SystemScope` and `First` (`repository/sagex_upload.go:49-63`), assuming global token uniqueness. Random generation makes accidental collision unlikely, but admin-supplied token reuse is not prevented.

### 2.6 Status and upload-token boundaries are not enforced

- `BootstrapInstallation` returns an existing row without checking `Status == 1`.
- `HandleUpload` (`service/sagex_upload.go:236-260`) fetches/creates by tenant-scoped installation UUID but never checks `inst.Status`.
- `GetByToken` filters `status=1`, but is unused.
- `controller/sagex_upload.go:224-230` defines `extractBearerToken`; no caller exists.
- `controller/sagex_upload.go:203-221` returns `{"installation": inst, "token": inst.Token}` after every bootstrap and maps every service error to `401`.

## 3. Reproduction conditions

No dynamic test was run during the main verification pause. The following are source-proven conditions for the future TDD lane:

1. **Cross-tenant UUID, on mode**: seed tenant B installation `UUID-B` with token `TOKEN-B`; call public bootstrap with tenant A's valid identify code and `UUID-B`. Expected current behavior: missing-tenant read error is swallowed; tenant A gets a new row/token; `TOKEN-B` is not returned.
2. **Cross-tenant UUID, off mode with residual multi-tenant rows**: same seed/call. Expected current behavior: global lookup returns tenant B row and `TOKEN-B`.
3. **SystemScope/wrong-bound context**: call the service with system scope or tenant-B-bound context plus tenant-A identify code and `UUID-B`. Expected current behavior: victim token can be returned.
4. **Idempotency**: call twice with same tenant/account/UUID in public on mode. Expected current behavior: duplicate rows and different tokens.
5. **Disabled row**: disable the existing row, then bootstrap again. Expected current behavior: token returned in existing-row paths, or a new active duplicate in public on mode.
6. **DB error**: inject repository read error. Expected current behavior: create is attempted; error is not surfaced as infrastructure failure.
7. **Same tenant/different account**: reuse UUID under another account in the same tenant. Expected current behavior: existing row/token can be returned when lookup reaches it; `HandleUpload` can continue using the original `AccountID`.
8. **Concurrency**: concurrent same tenant/account/UUID bootstrap calls. Expected current behavior: multiple rows/tokens because there is no composite unique constraint or atomic create-if-absent.

## 4. Root cause

1. **Trusted identity is not propagated into the data-access context.** The service resolves a tenant but queries with the original public/unbound context.
2. **Read errors are conflated with not-found.** `err == nil && existing != nil` makes every DB/isolation error fall through to minting.
3. **Existing credentials are returned without ownership or lifecycle checks.** Tenant, account, and status are never compared.
4. **Idempotency is only conventional, not enforced.** No `(tenant_id, installation_id)` uniqueness and no atomic upsert/conflict re-read.
5. **Controller error/token contract is too broad.** All failures become `401`; token is returned on every successful branch.
6. **Adjacent protocol drift:** upload ignores Bearer token and installation status, so the documented token/revocation boundary is not implemented.

## 5. Five-layer recommended fix surface

### Router

- Keep bootstrap public only if `identify_code` is intentionally the bootstrap credential; do not add `TenantMiddleware` before the service resolves it.
- Add a bootstrap-specific rate limit keyed by IP + normalized identify-code hash, with a bounded body limit.
- Keep `InitGuard`; add an audit event that records tenant/account/installation ID and outcome but never token/identify-code raw values.

### Controller

- Validate installation ID format/length and identify-code presence before service calls.
- Introduce typed service errors and map them explicitly: invalid input `400`, invalid/ambiguous credential `401`, disabled/ownership mismatch `403`, idempotency conflict `409`, DB/internal failure `5xx`.
- Never include a token in mismatch, disabled, conflict, or error responses. Return it only after verified creation or an explicitly authenticated reissue.
- Do not log request bodies or tokens.

### Service

- After resolving, create a pinned context with `tenant.OnlyTenant(ctx, resolved.TenantID)` and use it for **all** installation reads/writes.
- Distinguish `(nil, nil)` not-found from repository errors; propagate errors and never create after a read error.
- On existing row, require `TenantID == resolved.TenantID`, `AccountID == resolved.AccountID`, and `Status == 1`; otherwise fail closed with typed errors.
- Make same tenant/account/UUID idempotent: return the same active row/token without another create; handle unique conflict by re-reading and re-validating ownership/status.
- Decide and enforce the upload auth contract: if Bearer token remains part of v1, authenticate it and cross-check token tenant/account/installation against identify-code resolution; reject disabled rows. If identify-code-only is intended, remove the misleading token contract explicitly rather than leaving dead code.

### Repository

- Add explicit tenant-aware lookup such as `GetByTenantAndInstallationID(ctx, tenantID, installationID)` and active-status lookup.
- Add atomic `CreateIfAbsent`/upsert keyed by `(tenant_id, installation_id)` with conflict re-read; avoid ambiguous `First` on duplicate rows.
- Keep token lookup system-scoped only if token uniqueness is global; otherwise require tenant context and reject ambiguity.
- Return distinguishable not-found vs infrastructure errors.

### Model / DB

- Add a partial unique index on `(tenant_id, installation_id) WHERE installation_id <> ''` after duplicate preflight/remediation.
- Enforce token global uniqueness with an explicitly named `global_uniq_*` index, or store a global token hash and look up by hash; do not rely on tenant-scoped token uniqueness plus global `First`.
- Consider a status check constraint (`0/1`) and make empty installation IDs invalid for new bootstrap rows.
- Migration must fail closed on existing duplicates; no silent dedupe that could select the wrong token.

## 6. TDD cases required before fix approval

### Service unit tests (fake repository, context capture)

1. Valid tenant-A identify code + tenant-B UUID never returns tenant-B token; victim row remains unchanged.
2. Lookup context is pinned to resolved tenant A and cannot inherit `SystemScope`.
3. Same tenant/account/UUID repeated call returns same row/token and performs no second create.
4. Same tenant/different account/UUID is rejected with no token and no write.
5. Disabled existing row is rejected; no active duplicate is created.
6. Repository read error is propagated; create count stays zero.
7. Invalid/ambiguous identify code performs no installation lookup/create.
8. Unique conflict on create triggers one tenant-scoped re-read and full ownership/status validation.
9. Blank UUID behavior is explicit and validated.
10. If token auth is retained for upload: missing, wrong, disabled, and cross-tenant tokens are rejected; matching token + identify code + installation UUID succeeds.

### Repository integration tests (serial PostgreSQL)

1. Partial unique index rejects duplicate `(tenant_id, installation_id)`.
2. Same UUID in different tenants remains isolated and yields distinct tokens.
3. Duplicate preflight/migration fails closed on existing duplicates.
4. Token uniqueness behavior matches the chosen global-hash/global-unique design; ambiguous token lookup is impossible.
5. Active/inactive lookups and tenant predicates are explicit.

### Controller tests

1. Cross-tenant/account mismatch response contains no token.
2. Disabled row response contains no token and does not create a replacement.
3. DB error maps to `5xx`, not `401`.
4. Invalid UUID/identify code maps to `400/401` with stable machine reason.
5. Audit/log output contains no raw token or identify code.

## 7. Approval boundary

This assessment does not authorize production or test changes. Recommended next step: main approves the tenant-scoped idempotency + status/ownership fix and the upload-token contract decision, then a separate TDD lane implements the service/repository/model/controller changes.

## 8. Evidence hashes

```text
7fee215d39bce7925e917f211529260ff110cec1b4ccaa2c3dd76d309c86404d  user-server/internal/service/sagex_upload.go
bd8621c10b51bc1563960b5dfd8db7a74d0d99bd1d5465975352bf854535847d  user-server/internal/controller/sagex_upload.go
87eb7f8f4a5053d367fbbb3b5c8e49de8c256f163fed3160fe12ec81a93959e5  user-server/internal/repository/sagex_upload.go
db4dbf646e768696db545ff90692aaba9981f032ef8eaf127f4f329f6f0404c4  user-server/internal/model/sagex_upload.go
c965bd2d056fc3319b820625c8695a7129c3db641cf7e494096382479442d6e4  user-server/internal/router/router.go
b43c59d9689038e796f2a19b8be7c2a8589f9fd209f4e14676e136e763384c8b  user-server/internal/pkg/tenancyidx/tenant_callback.go
```

## 9. 主智能体裁决（2026-11-25，已批准聚焦范围）

**结论：valid / high。** 原始 `SAGEX-UPLOAD` 契约仍以 identify_code 为有效凭证；本轮不引入 Bearer 鉴权、不移除 identify_code，也不同时改上传协议。

### Approved implementation scope

1. **Service `BootstrapInstallation`**
   - identify_code 解析成功后，用 `tenant.OnlyTenant(ctx, resolved.TenantID)` 作为所有安装读写上下文；不得继承 SystemScope/其他租户上下文。
   - 严格区分 not-found 与 DB error；任何读取错误立即返回，不得继续 create。
   - 已存在安装必须校验 tenant、account、status；同租户同账号同 installation ID 的 active 行只能幂等返回同一行/token，不得再创建。
   - 跨租户、跨账号、disabled 行一律拒绝且不返回 token、不创建替代行。
   - installation ID 去除首尾空白；空值按现有契约生成 `inst_...`；只校验 schema 最大长度，不强制 UUID 格式。

2. **Repository / schema**
   - 增加显式 tenant-aware 查询或等价 seam；not-found 与 DB error 可区分。
   - 用并发安全的 create-if-absent + conflict re-read/upsert 保证同租户同 installation ID 幂等；唯一约束是最终防线。
   - 新 schema 迁移注册为 **v3.115.0**（当前已注册最新为 v3.113.0；v3.114.0 的未注册外部缺口不在本 lane），对非空 `(tenant_id, installation_id)` 建 partial unique index。
   - 迁移必须先检测重复并以明确错误 fail-closed，要求运维修复；不得静默去重/删数据。Down 只回滚本 lane 新增索引。
   - fresh DB/testutil 也必须执行同等约束；根目录 SQL 仅按仓库惯例配套添加。

3. **Controller**
   - 保留 identify_code 请求契约；安装 ID 做非空/长度与 JSON 校验。
   - 使用 typed 400/401/403/409/5xx 映射；DB/内部错误不得再统一伪装为 401。
   - token 只能出现在“已验证的新建或同租户同账号幂等成功”响应中；任何错误响应不得包含 token。

4. **TDD（先 RED 后 GREEN）**
   - cross-tenant token 永不返回；on/off/SystemScope/错误上下文均钉住 resolved tenant。
   - 同租户同账号同 UUID 重试只创建一次并返回同一 token；不同账号拒绝。
   - disabled 行拒绝且不创建替代；读取 DB error 可见且零 create。
   - invalid identify code 不访问安装表；unique-conflict re-read 校验 ownership/status。
   - 空 installation ID 自动生成、超长拒绝；repository 并发/唯一索引/迁移重复失败与 tenant isolation。
   - controller 错误状态/机器 reason 稳定且无 token 泄漏。

### Explicitly deferred

- Upload Bearer-token 契约与 `model/sagex_upload.go` 文档漂移（独立协议 lane）。
- Bootstrap 专用限流/审计、UI/真机矩阵、无关 v3.114.0 注册修复。
- 不修改其他在途功能；不 commit/push/deploy/真实外发/UI。

实施必须先补有效 RED，再只改 assessment 所列五层 scope，完成 serial PostgreSQL/迁移/并发回归后通知主智能体稳定复验。
