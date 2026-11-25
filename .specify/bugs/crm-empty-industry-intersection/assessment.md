# Bug Assessment: Empty Industry Intersection Fails Open

- **Slug**: crm-empty-industry-intersection
- **Created**: 2026-11-25
- **Source**: Pasted task from `docs/未完成任务清单.md` (§3.29, `CRM-20261125-01`)
- **Verdict**: valid
- **Severity**: high

## Report (verbatim or summarized)

The task requires default-deny behavior when an employee's authorized industry set and requested industry filter have an empty intersection. “Industry unrestricted” must remain distinct from “restricted but empty”; customer listing, cross-industry search, and export must share the behavior. A→B must return no rows or a consistent denial, A→A must still work, and existing administrator and empty-account-authorization behavior must not regress. Add a Service→Repository negative test with pagination/count assertions.

## Symptom

For a user authorized for industry A, requesting industry B produces an empty `Industries` slice in the permission filter. Repository filtering treats an empty slice as “no industry predicate,” so ordinary CRM listing and export can include customers outside the requested industry; cross-industry search can replace the empty intersection with B and return those customers. Expected behavior is no rows (or a consistent denial), with the tenant and account restrictions still applied.

## Reproduction

1. In one tenant, create customers in industries A and B under the same source account; give a non-administrator access to that account and industry A only.
2. Request the normal customer list and export with industry B as the query filter.
3. Request cross-industry search for industry B using the same scope.

The source paths below establish the failure statically; the database-backed scenario was not executed during assessment. In the current flow, list/export can omit the industry predicate after the empty intersection, while cross-industry search can replace the empty slice with B.

## Suspected Code Paths

- `user-server/internal/service/customer_permission.go:159-183` — `ToFilter` intersects requested industries with the authorized set; a disjoint intersection is represented as an empty slice and passed to the repository.
- `user-server/internal/repository/customer_crm.go:39-80` — `applyCustomerScope` only adds `industry IN ...` when the slice is non-empty; `ListScoped` then counts and paginates the widened result.
- `user-server/internal/service/customer_360_master.go:104-127` — `CrossIndustrySearch` treats an empty filtered industry slice as unrestricted and substitutes the requested industry before delegating to `ListScoped`.
- `user-server/internal/service/crm_customer.go:66-78` — normal CRM listing supplies the requested industry through `CustomerPermissionService.ListScoped`.
- `user-server/internal/service/customer_import_export.go:201-257` — export applies `ToFilter` and queries the shared scoped repository, so a disjoint requested industry can likewise be widened.
- `user-server/internal/service/lead_scoped_list.go:47-70` and `user-server/internal/service/customer_dedupe.go:89-110` — additional consumers of the shared filter; preserve their current behavior when no requested industry intersection exists.

## Root Cause Hypothesis

**Confidence: high.** The filter representation conflates two different states: no industry restriction and a restricted query whose authorized/requested intersection is empty. `ToFilter` encodes both as an empty slice, and repository consumers interpret that slice as no filter. `CrossIndustrySearch` independently makes the same assumption when it fills an empty slice with the requested industry. Together these paths fail open at the industry dimension. Tenant and source-account filters remain present, but they do not preserve the required industry boundary.

## Proposed Remediation

**Preferred**: Give `CustomerScopeFilter` an explicit fail-closed state (for example, `DenyAll`) that is distinct from an empty `Industries` slice. Set it when a non-empty authorized industry set and a non-empty requested set have no intersection; preserve an already-set denial through repeated `ToFilter` calls. Make the repository produce an impossible predicate for that state so both its count and paginated rows are empty. Keep empty `Industries` as “no industry predicate,” preserve the existing restricted-empty-account denial, and do not constrain administrator scopes by employee industry grants. Update cross-industry search so it cannot replace or clear an explicit denial. Existing export/list consumers should carry the same filter state through the shared repository path.

**Alternatives** (optional):
- Return a permission error directly from each service entry point on an empty intersection. This duplicates behavior across list, search, and export and risks inconsistent handling; prefer a shared filter/repository state.

**Files likely to change**:
- `user-server/internal/repository/customer_crm.go`
- `user-server/internal/service/customer_permission.go`
- `user-server/internal/service/customer_360_master.go`
- `user-server/internal/repository/customer_crm_scope_test.go` (new)
- `user-server/internal/service/customer_permission_test.go` (new)
- `user-server/internal/service/customer_360_scope_test.go`
- `user-server/internal/service/customer_import_export_test.go` (new)

**Tests to add or update**:
- Repository-backed Service→Repository case: authorized industry A plus requested B returns no rows and `total == 0`; verify page 1 and a later page, and verify A still returns matching rows.
- Preserve the distinction between an empty `Industries` slice with no industry restriction and an explicit empty-intersection denial.
- Cross-industry search for B under A-only authorization returns no items and zero total; search for A remains successful, including the repeated `ToFilter` path.
- Export by query for B under A-only authorization returns no rows and zero total; export for A remains successful.
- Administrator filtering behavior and the existing restricted-account-with-empty-IDs denial remain unchanged.

## Risks & Considerations

- The explicit denial state must survive `ToFilter` being applied more than once; cross-industry search currently applies it before calling `ListScoped`, which applies it again.
- Do not turn ordinary empty `Industries` into a denial: unrestricted users and queries without an industry filter rely on the current meaning.
- Preserve tenant scoping, account scoping, pagination/count consistency, administrator behavior, and empty-account fail-closed behavior.
- No schema or migration change is indicated; repository-backed tests require the configured PostgreSQL test database.
- Assessment is based on static code-path evidence; runtime reproduction remains for the fix/verification stages.

## Open Questions

- None identified for assessment. Runtime/database verification is still required before the bug can be marked fixed.

---

## 只读复核结论（2026-11-25）

- **HEAD**：`2c5c53e3ded742ac0c6ebb1944529aaed18973fd`（复核前后未漂移）
- **复核方式**：仅阅读 assessment、需求/SDD、Controller→Service→Repository 源码与现有测试；未运行 DB/build/test，未修改生产或测试代码。

### Evidence

1. **原始需求范围明确**
   - 工作区根 `docs/未完成任务清单.md:281-284` 的 `CRM-20261125-01｜P0｜行业筛选交集为空时保持默认拒绝` 于 2026-11-25 登记，直接引用 P06、S02、C12/C17、SDD FR-002/004，并要求列表、跨行业检索、导出共用语义。
   - 产品仓 `.specify/custom-task-crm-lead-channel-20261124/spec.md:24-36,99,109-113` 要求按行业隔离、导出按权限过滤、跨账号行业检索不得突破权限。
   - `docs/定制任务清单.md:67,75,1283-1293` 的 P-06/S-02 同样要求“行业筛选仅在授权数据内生效”“行业过滤不扩大权限”。

2. **列表与导出确实 fail-open**
   - `controller/crm.go:62-77,235-262` 把 `industry` 原样传给 Service。
   - `service/crm_customer.go:66-78` 与 `service/customer_import_export.go:245-256` 把请求行业放入 `CustomerScopeFilter.Industries`。
   - `service/customer_permission.go:159-178,237-247` 对授权行业 A 与请求行业 B 求交集得到空切片；`intersectCRMStrings` 在 `base` 为空时又会把授权行业重新填回。
   - `repository/customer_crm.go:39-61` 仅在行业切片非空时添加 `industry IN ...`；空切片被解释为“无行业条件”。因此 A→B 的列表/导出会返回该账号下 A+B 数据，而不是空结果。

3. **跨行业检索的问题成立，但原 assessment 的症状描述需纠正**
   - `service/customer_360_master.go:116-126` 先调用一次 `ToFilter`（空请求行业 → 授权行业 A），再与请求行业 B 求交集得到空切片，随后 `ListScoped` 又调用一次 `ToFilter`。
   - 第二次 `ToFilter` 看到空 `base.Industries`，通过 `intersectCRMStrings(empty, [A])` 重新填回 A，最终仓库返回 **A 行业数据，不是 B 行业数据**。
   - `len(filter.Industries)==0` 时填入请求行业 B 的分支，只在授权行业本身为空（行业不受限）时触发；那种情况下填入 B 是正确行为。故不能把当前 A→B 描述成“返回未授权 B 数据”。真实缺陷是：空交集被二次应用权限复活，检索返回 A，违反请求筛选与默认拒绝语义。

4. **已有正确范式可参照**
   - 另一条 `service/cross_account_search.go:114-124` 在授权行业与请求行业交集为空时直接返回空结果，说明项目既有语义就是 fail-closed；问题仅在 CRM 共享 filter 路径。

5. **现有测试没有覆盖本缺陷**
   - 未找到针对 `CustomerPermissionService.ToFilter`、`CRMCustomerService.List`、`CustomerImportExportService.ExportByQuery`、`CRM360Service.CrossIndustrySearch` 的直接回归测试。
   - `service/customer_360_scope_test.go` 只测 `assertSessionsVisible`，与行业 filter 无关；`service/cross_account_search_test.go` 测的是另一条 `CrossAccountSearchService` 链路。
   - 当前 `CustomerScopeFilter` 无 `DenyAll`/行业受限标志，仓储层也没有对应测试。

### Verdict

- **valid（静态高置信）；runtime not-reproduced / blocked-by-instruction**
  - 列表与导出的越权行业暴露可由源码完整证明，核心缺陷真实存在。
  - 跨行业检索部分需按上面的 Evidence 修正为“二次 `ToFilter` 把空交集复活为 A 并返回 A”，不是“替换为 B 并返回 B”。
  - **severity=high 仍成立**：主要依据是列表/导出把同账号下未授权行业 B 暴露给仅授权 A 的员工；跨行业检索部分是筛选正确性与最小权限违反，但不构成 B 数据泄漏。
  - 本轮按要求未做运行复现，因此不能标记 fixed/runtime-verified；静态证据不足以阻塞“valid”判定，只阻塞运行验收结论。

### Scope

- **in-scope（近两日原始需求范围内）**
  - 对应 2026-11-24 SDD US1 的 FR-002/004/006，以及 2026-11-25 明确登记的 P0 `CRM-20261125-01`。
  - 这是既有行业隔离需求的缺口收口，不是新增需求或范围扩张。
- **最小生产修改面**
  - `user-server/internal/repository/customer_crm.go`
  - `user-server/internal/service/customer_permission.go`
  - `user-server/internal/service/customer_360_master.go`
- **不需要修改**
  - Controller、Router、Model、数据库 schema/migration、UI、RBAC、其他 CRM 写权限/去重/线索/账号授权任务。
- **最小测试面**
  - 新建 `user-server/internal/service/customer_permission_test.go`
  - 新建 `user-server/internal/service/customer_industry_scope_test.go`
  - 新建 `user-server/internal/repository/customer_crm_scope_test.go`
  - 不覆盖现有 `customer_360_scope_test.go`（其主题是会话可见性）。

### Minimal Fix

1. 在 `CustomerScopeFilter` 增加显式 `DenyAll bool`（零值=false，保持“行业不受限”的现有含义），不采用 nil/空切片隐式编码。
2. `ToFilter`：
   - 已有 `DenyAll=true` 时原样保留，后续重复应用不得清除；
   - 非管理员且授权行业非空时，请求行业为空则继承授权行业；请求行业非空则求交集；交集为空则设置 `DenyAll=true`；
   - 账号、负责人、管理员绕过逻辑保持不变。
3. `applyCustomerScope` 首先处理 `DenyAll`，追加固定 `1 = 0`；`ListScoped` 的租户条件仍先保留，确保 count 与分页 rows 同时为 0。
4. `CrossIndustrySearch` 把请求行业先放入 base filter，只调用一次 `permission.ListScoped`；删除当前的“先 `ToFilter`→手工交集→再 `ListScoped`”二次应用路径。
5. 维持“空结果 + HTTP 200”的现有接口形态，不引入新的 403 契约；导出继续写 total=0 审计。

### TDD Cases

**共享真实 PG fixture**：同一租户 `T1`、账号 `acct-1` 下放 2 条 A、1 条 B；`acct-2` 放 1 条 A；租户 `T2` 的 `acct-1` 放 1 条 A。受限员工 scope 为 `AccountIDs=[acct-1]`、`Industries=[A]`。使用 `testutil.NewTestDB` + `tenant.WithTenantID`，后续执行必须确认 0 SKIP。

**先写行为 RED（使用现有公开方法即可编译并失败）**

1. `TestCRMCustomerList_EmptyIndustryIntersection_ReturnsNoRowsAndZeroTotal`
   - A→B，page=1/size=1 与 page=2/size=1 均 `len(rows)==0 && total==0`。
   - 当前会返回 A+B，RED。
2. `TestCRM360CrossIndustrySearch_EmptyIndustryIntersection_ReturnsNoRows`
   - A→B，`Items/CrossTab` 为空、`Total==0`；A→A 正常。
   - 当前 A→B 会返回 A，RED。
3. `TestCRMExportByQuery_EmptyIndustryIntersection_ReturnsNoRows`
   - A→B，`Rows` 为空、`Total==0`、`SensitiveMasked==true`；A→A 正常。
   - 当前会导出 A+B，RED。
4. `TestCustomerPermissionToFilter_EmptyIntersectionRemainsEmptyAfterReapplication`
   - 第一次 A→B 得到空行业；再次 `ToFilter` 后必须仍为空/拒绝。
   - 当前第二次会变回 A，RED。

**实现显式拒绝态后补的结构化 GREEN**

5. `TestCRMCustomerRepository_ListScoped_DenyAllReturnsEmptyCountAndRows`
   - 直接传 `DenyAll=true`，page 1/2 均 rows=0、total=0，且不泄漏其他租户数据。
6. `TestCRMCustomerRepository_ListScoped_EmptyIndustriesWithoutDenialIsUnrestricted`
   - `Industries=[] && DenyAll=false` 仍返回该租户/账号下 A+B，防止把普通空切片误改成拒绝。
7. `TestCustomerPermissionToFilter_PartialIntersectionKeepsAllowedIndustry`
   - 授权 [A,C]、请求 [B,C] → [C]，不拒绝。
8. `TestCustomerPermissionToFilter_AdminBypassesIndustryGrant`
   - `AllAccounts=true` 时请求 B 仍返回 B，不被员工行业授权反向收敛。
9. `TestCustomerPermissionToFilter_UnrestrictedIndustryScopePreservesRequest`
   - 授权行业为空、请求 B → B，不拒绝。
10. `TestCRMCustomerRepository_ListScoped_RestrictedEmptyAccountsStillDenies`
    - `RestrictedAccounts=true && SourceAccountIDs=[]` 仍为 rows=0、total=0。

**三入口共同正例**

- A→A：列表/检索/导出均只返回 2 条 A；列表与检索 page 1/2 的 `total` 都保持 2，导出 `Total==2`。
- 管理员 B→B：只返回 B；行业不受限但账号受限的员工 B→B：也只返回 B。
- 空账号授权：继续拒绝全部。

### Verification Limits

- 本轮**未运行** PostgreSQL、`go test`、`go build`、`go vet` 或任何 UI/接口验证。
- 本轮**未修改**生产代码、测试代码、schema 或其他任务文件；仅追加本 assessment。
- 结论可指导后续 TDD 施工，但在主智能体完成独立运行复现与 GREEN 验证前，不得把该项标记为 fixed/closed。

---

## 主智能体裁决（2026-11-25）

- **Verdict**：valid / high，批准进入 assess → fix → test。主智能体已只读核对 `CustomerScopeFilter`、`ToFilter`、`intersectCRMStrings`、`applyCustomerScope`、`ListScoped` 与 `CrossIndustrySearch`，静态根因成立。
- **生产修改面（仅此三个文件）**：
  1. `user-server/internal/repository/customer_crm.go`：`CustomerScopeFilter` 增加 `DenyAll bool`；`applyCustomerScope` 在 `DenyAll` 时优先追加固定 `1 = 0`，使 count 与 rows 同时为空；普通空 `Industries` 仍表示“行业不受限”。
  2. `user-server/internal/service/customer_permission.go`：`ToFilter` 保留已有 `DenyAll`；非管理员、授权行业非空且请求行业非空而交集为空时设置 `DenyAll=true`；管理员与授权行业为空的既有语义不变。
  3. `user-server/internal/service/customer_360_master.go`：把请求行业先放入 base filter，只调用一次 `permission.ListScoped`，删除二次 `ToFilter`/手工交集复活路径；保持 HTTP 200 空结果与既有脱敏/scope 出参。
- **测试面**：新建 `service/customer_permission_test.go`、`service/customer_industry_scope_test.go`、`repository/customer_crm_scope_test.go`。使用真实 PostgreSQL fixture，先写行为 RED，再改生产；覆盖列表/跨行业检索/导出 A→B 的 rows=0/total=0、分页 page1/page2、A→A 正例、部分交集、管理员绕过、行业不受限、空账号拒绝、重复 `ToFilter` 不复活。
- **不在范围**：Controller/Router/Model/schema/migration/UI/RBAC、其他 CRM 写权限/去重/线索任务；不得顺带修复 full-service 既有失败。
- **纪律**：使用 `run-user-db-check-cwd.sh`，DB 测试串行；不得跳过/SKIP；完成 RED→GREEN、fix.md/test.md、build/vet/layer/architecture 与 scoped/full affected tests 后通知主智能体稳定；主智能体独立复验前不得宣称 closed。不得 commit/push/deploy/真实外发/UI。
