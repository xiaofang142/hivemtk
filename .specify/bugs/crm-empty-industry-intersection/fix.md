# Bug Fix: CRM empty industry intersection fails open

- **Slug**: `crm-empty-industry-intersection`
- **Fixed**: 2026-11-25
- **HEAD**: `2c5c53e3ded742ac0c6ebb1944529aaed18973fd`
- **Assessment**: `./assessment.md`
- **Status**: applied / scoped GREEN / main independently verified (scoped lane)

## Summary

The CRM industry filter conflated “industry unrestricted” with “authorized/requested industry intersection is empty.” An employee authorized for industry A could request B and receive same-account A+B rows from list/export; cross-industry search could reapply permission and return A. The fix adds an explicit `DenyAll` filter state, preserves it across repeated `ToFilter` calls, makes the repository emit `1 = 0` before count/pagination, and applies permission exactly once in cross-industry search.

## Changes

| File | Change | Notes |
|---|---|---|
| `user-server/internal/repository/customer_crm.go` | Added `CustomerScopeFilter.DenyAll`; `applyCustomerScope` returns `1 = 0` first | Zero value remains unrestricted; tenant predicate remains in `ListScoped` |
| `user-server/internal/service/customer_permission.go` | Preserve existing denial; set denial only for non-empty authorized/requested industry sets with empty intersection | Admin, unrestricted-industry, account and owner semantics unchanged |
| `user-server/internal/service/customer_360_master.go` | Put requested industry into base filter and call `ListScoped` once | Removes the double-`ToFilter` path that could refill an empty intersection |
| `user-server/internal/service/customer_permission_test.go` | Added pure filter regressions | Empty intersection, repeated application, partial intersection, admin, unrestricted industry, inherited authorization |
| `user-server/internal/service/customer_industry_scope_test.go` | Added real-PG Service→Repository regressions | List/search/export A→B page 1/2; A→A; partial intersection; admin; unrestricted industry; empty account |
| `user-server/internal/repository/customer_crm_scope_test.go` | Added real-PG repository regressions | `DenyAll` count/rows, ordinary empty industry remains unrestricted, stable pagination, empty-account denial |

## Stage A — behavior RED

Command:

```text
env CI=true AUDIT_CRED_ROOT=/Users/xiaofang/Documents/www/go/hivemtk/hivemtk-user \
  bash /private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/recent-task-audit-20261125/run-user-db-check-cwd.sh \
  go test -p 1 ./internal/service ./internal/repository \
  -run '^(TestCustomerPermissionToFilter_|TestCRM)' -count=1 -v
```

Result: rc=1, 4 expected behavior failures, 11 non-regression passes, 0 SKIP.

- List A→B: `rows=1,total=3` instead of `0/0`.
- Cross-industry A→B: returned A1/A2 instead of empty.
- Export A→B: returned A1+A2+B1 instead of empty.
- Repeated `ToFilter`: empty intersection became `[A]`.

Log: `check-crm-empty-industry-red-r1.log`.

## Stage B — explicit denial contract RED

After adding only the `DenyAll` field and its assertions, but before behavior implementation:

- `ToFilter` left `DenyAll=false`.
- Repository ignored `DenyAll=true` and returned `rows=1,total=2`.

Result: rc=1, 2 expected failures, 0 SKIP.

Log: `check-crm-empty-industry-denyall-red-r1.log`.

## GREEN

Same scoped command after the three-file fix:

- 16/16 top-level PASS at first GREEN; the later-added real-PG partial-intersection case brings the final matrix to 17/17.
- Serial repeated matrix: **17 tests × 3 = 51 PASS, 0 FAIL/SKIP, rc=0**.
- Final serial matrix: **17/17 PASS, 0 FAIL/SKIP, rc=0**.

Logs:

- `check-crm-empty-industry-green-r1.log`
- `check-crm-empty-industry-serial-matrix-r3.log`
- `check-crm-empty-industry-serial-matrix-final.log`

## Tests Added or Updated

- `TestCRMCustomerList_EmptyIndustryIntersection_ReturnsNoRowsAndZeroTotal`
- `TestCRM360CrossIndustrySearch_EmptyIndustryIntersection_ReturnsNoRows`
- `TestCRMExportByQuery_EmptyIndustryIntersection_ReturnsNoRows`
- `TestCRMIndustryScope_MatchingIndustryPositive`
- `TestCRMIndustryScope_PartialIntersectionPositive`
- `TestCRMIndustryScope_AdminBypassesIndustryGrant`
- `TestCRMIndustryScope_UnrestrictedIndustryPreservesRequest`
- `TestCRMIndustryScope_EmptyAccountDeniesAll`
- `TestCustomerPermissionToFilter_EmptyIntersectionRemainsEmptyAfterReapplication`
- `TestCustomerPermissionToFilter_PartialIntersectionKeepsAllowedIndustry`
- `TestCustomerPermissionToFilter_AdminBypassesIndustryGrant`
- `TestCustomerPermissionToFilter_UnrestrictedIndustryScopePreservesRequest`
- `TestCustomerPermissionToFilter_EmptyRequestInheritsAuthorizedIndustries`
- `TestCRMCustomerRepository_ListScoped_DenyAllReturnsEmptyCountAndRows`
- `TestCRMCustomerRepository_ListScoped_EmptyIndustriesWithoutDenialIsUnrestricted`
- `TestCRMCustomerRepository_ListScoped_IndustryFilterPaginatesWithStableTotal`
- `TestCRMCustomerRepository_ListScoped_RestrictedEmptyAccountsStillDenies`

## Local Verification

| Check | Result | Log |
|---|---|---|
| Scoped GREEN | 16/16 PASS, rc=0 | `check-crm-empty-industry-green-r1.log` |
| Serial matrix ×3 | 51/51 PASS, 0 FAIL/SKIP, rc=0 | `check-crm-empty-industry-serial-matrix-r3.log` |
| Final serial matrix | 17/17 PASS, 0 FAIL/SKIP, rc=0 | `check-crm-empty-industry-serial-matrix-final.log` |
| Repository full | rc=0, 37.454s | `check-crm-empty-industry-repository-full-final.log` |
| Service full | rc=1, 26 baseline-identical failures, current-only=0, CRM failures=0, 0 SKIP | `check-crm-empty-industry-service-full-final.log` |
| `go build ./...` | rc=0 | `check-crm-empty-industry-build-final.log` |
| `go vet ./...` | rc=0 | `check-crm-empty-industry-vet-final.log` |
| Layer boundary | rc=0, 1758 files / 0 new violations | `check-crm-empty-industry-layer-final.log` |
| Architecture | rc=0, only existing `wecomCSService` warning | `check-crm-empty-industry-architecture-final.log` |
| GORM-only | rc=0, 0 business `.Exec`/`.Raw` | `check-crm-empty-industry-gorm-only-final.log` |
| Naked-goroutine repository gate | rc=1, 62 pre-existing hits; all 62 lines identical to HEAD; scope hits=0 | `check-crm-empty-industry-naked-goroutine-final.log`, `crm-empty-industry-naked-goroutine-head-verified.txt` |
| `gofmt -l` | clean | inline final check |
| `git diff --check` | rc=0 | inline final check |

## Baseline Classification

- Full service has exactly the same 26 top-level failures as the SageX-UPLOAD-stage baseline; sorted comparison has current-only=0 and baseline-only=0.
- No `TestCRM*` failure and no SKIP occurred in the full service run.
- The nil-repo panic stack in the full-service log is an expected/captured negative-case log, not a process crash.
- The repository-wide naked-goroutine gate fails on 62 existing lines. Every reported line is byte-identical to HEAD and none belongs to the six CRM scope files. This lane does not expand into that baseline cleanup.

## Deviations from Assessment

None. The implementation follows the approved explicit-denial remediation. A real-PG partial-intersection positive was added in addition to the pure filter case to prove the shared repository path keeps the common industry.

## Follow-ups

- Main independent verification of the CRM scope and baseline classification.
- The 26 full-service failures and repository-wide naked-goroutine baseline remain separate lanes.
- No Controller/Router/Model/schema/UI/v3.114 changes were made.
- No commit/push/deploy/real outbound/UI actions were performed.
