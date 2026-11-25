# Bug Verification: CRM empty industry intersection fails open

- **Slug**: `crm-empty-industry-intersection`
- **Tested**: 2026-11-25
- **HEAD**: `2c5c53e3ded742ac0c6ebb1944529aaed18973fd`
- **Assessment**: `./assessment.md`
- **Fix**: `./fix.md`
- **Result**: main independently verified (scoped lane)

## Summary

The original symptom was reproduced against real PostgreSQL before the fix: A→B list/export returned same-account A+B rows, cross-industry search returned A, and repeated `ToFilter` revived A from an empty intersection. After the three-file fix, all three entry points return empty rows/count for A→B on page 1 and page 2, while A→A, partial intersection, administrator, unrestricted-industry and empty-account-denial behavior remain correct. No new full-service failure was introduced.

## Checks Performed

| Check | Command / Action | Result | Notes |
|---|---|---|---|
| Pre-fix reproduction | Scoped real-PG command in `fix.md` | fail as expected | 4 behavior failures; 11 non-regression passes; 0 SKIP |
| Explicit denial RED | `DenyAll` filter + repository contract tests before behavior implementation | fail as expected | `ToFilter` did not set denial; repository ignored denial |
| Post-fix scoped suite | `go test -p 1 -parallel 1 ./internal/service ./internal/repository -run '^(TestCustomerPermissionToFilter_|TestCRM)' -count=1 -v` | pass | 17/17 PASS, 0 FAIL/SKIP, rc=0 |
| Repeated serial matrix | Same command with `-count=3` | pass | 51/51 PASS, 0 FAIL/SKIP, rc=0 |
| Repository full | `go test -p 1 -parallel 1 ./internal/repository -count=1` | pass | rc=0, 37.454s |
| Service full | `go test -p 1 -parallel 1 ./internal/service -count=1` | baseline-only fail | 26 failures, exactly matching prior baseline; current-only=0; CRM failures=0; 0 SKIP |
| Build | `go build ./...` | pass | rc=0 |
| Vet | `go vet ./...` | pass | rc=0 |
| Layer boundary | `python3 scripts/check-layer-boundary.py` | pass | 1758 files / 0 new violations |
| Architecture | `bash scripts/check-architecture.sh` | pass | rc=0; existing `wecomCSService` warning only |
| GORM-only | `bash user-server/scripts/check-gorm-only.sh` | pass | rc=0 |
| Naked-goroutine gate | `bash user-server/scripts/check_naked_goroutine.sh` | pre-existing fail | 62 hits; all identical to HEAD; scope hits=0 |
| Formatting / whitespace | `gofmt -l` + `git diff --check` | pass | clean / rc=0 |

All DB-backed commands used `run-user-db-check-cwd.sh` with `CI=true`, `-p 1 -parallel 1`; scoped and full-service outputs contain 0 SKIP.

## Output Excerpts

Pre-fix RED:

```text
A→B 第 1 页应 rows=0,total=0，实际 rows=1,total=3
A→B 第 1 页应 items=0,cross_tab=0,total=0，实际 ... Industry:A ... Total:2
A→B 导出应 rows=0,total=0,masked=true，实际 ... A1 ... A2 ... B1 ... Total:3
A→B 重复应用权限后仍应为空，实际 [A]
rc=1
```

Post-fix final matrix:

```text
--- PASS: TestCRMCustomerList_EmptyIndustryIntersection_ReturnsNoRowsAndZeroTotal
--- PASS: TestCRM360CrossIndustrySearch_EmptyIndustryIntersection_ReturnsNoRows
--- PASS: TestCRMExportByQuery_EmptyIndustryIntersection_ReturnsNoRows
--- PASS: TestCRMIndustryScope_MatchingIndustryPositive
--- PASS: TestCRMIndustryScope_PartialIntersectionPositive
--- PASS: TestCRMIndustryScope_AdminBypassesIndustryGrant
--- PASS: TestCRMIndustryScope_UnrestrictedIndustryPreservesRequest
--- PASS: TestCRMIndustryScope_EmptyAccountDeniesAll
--- PASS: TestCustomerPermissionToFilter_EmptyIntersectionRemainsEmptyAfterReapplication
--- PASS: TestCRMCustomerRepository_ListScoped_DenyAllReturnsEmptyCountAndRows
PASS
rc=0
```

Full-service attribution:

```text
current_top_level_fail=26
current_top_level_crm_fail=0
current_skip=0
current-only failures: none
baseline-only failures: none
rc=1
```

## Residual Risks

- Full service remains red on the same 26 pre-existing failures; this lane did not expand into them.
- The repository-wide naked-goroutine gate remains red on 62 pre-existing HEAD-identical lines; none is in this lane.
- No UI/HTTP-controller test was added because Controller/Router/Model/schema/UI were explicitly out of scope and unchanged; Service→Repository behavior is covered against real PostgreSQL.
- No commit/push/deploy/real outbound/UI action was performed.
- Main independent verification is still required before the lane is marked independently verified.

## Recommendation

Close the local scoped bug lane as verified after main review. Keep the 26 full-service failures and naked-goroutine baseline in their separate lanes; do not treat them as CRM regressions.

## Main Independent Verification

- **PASS / scoped verified**, 2026-11-25 12:21:38–12:27:52 +08:00.
- HEAD `2c5c53e3ded742ac0c6ebb1944529aaed18973fd` checked before/after every step; no drift.
- Scoped 17/17 PASS; serial ×3 51/51 PASS; repository full rc=0 (36.177s).
- Service full: exactly 26 baseline failures, current-only=0, baseline-only=0, CRM failures=0, SKIP=0.
- build/vet/layer 1758/0/architecture/GORM-only rc=0; architecture only existing wecomCSService warning.
- Naked-goroutine gate remains rc=1 with 62 HEAD-identical baseline hits; extracted hit-set diff rc=0.
- 9 scoped hashes stable; git diff --check rc=0.
- Evidence: `main-crm-empty-industry-verification-20261125.txt`, `main-crm-empty-industry-verification-summary.txt` (overall_rc=0).
