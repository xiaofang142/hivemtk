# Bug Assessment: SageX upload tests still treat Bearer token as identify_code

- **Slug**: `sagex-upload-identify-code-test-drift-20261125`
- **Created**: 2026-11-25
- **Source**: pasted text / clean HEAD `2c5c53e3ded742ac0c6ebb1944529aaed18973fd` source review
- **Verdict**: valid
- **Severity**: high（生产协议与文档/模型契约不一致）；test-only 漂移修复本身为 medium
- **Lane**: Bug 车道 assess-only

## Report (summarized)

Clean HEAD `2c5c53e3d` reproduces six old SageX Upload failures: four service tests and two controller tests return `unauthorized`. Review whether the tests still pass a token where production now expects `identify_code`, propose a minimal test-only migration, and separate test defects from production protocol/security decisions.

## Symptom

The effective `/api/integrations/sagex/upload` contract is identify_code-only, but the old tests still model Bearer-token authentication. Positive service tests pass `"tok"` as the identify code without installing a resolver; positive controller tests send only `Authorization: Bearer tok` and omit `identify_code`. They therefore fail before exercising the behavior they were written to verify.

## Reproduction

Already reproduced on detached clean HEAD `2c5c53e3d`; not rerun in this assess-only lane:

```text
go test -p 1 ./internal/service ./internal/controller -run 'SageX' -count=1 -v
```

Result: rc=1.

- Service PASS: `TestSageXUpload_Unauthorized`, `TestSageXUpload_SchemaAndBatchValidation`.
- Service FAIL: `TestSageXUpload_DuplicateInBatchNotAcked`, `TestSageXUpload_IdempotentReceiptReturnsDuplicate`, `TestSageXUpload_MapsQQAndWeChat`, `TestSageXUpload_UnsupportedPackageRejected` — all `sagex: unauthorized`.
- Controller FAIL: `TestSageXUploadController_ReturnsProtocolAck`, `TestSageXUploadController_RealV1Payload` — HTTP 401 `{"error":"unauthorized","schema_version":"1.0"}`.

Evidence: `check-sagex-upload-clean-head2c5-baseline.log`.

## Suspected Code Paths (clean HEAD)

### Tests that should be migrated

- `user-server/internal/service/sagex_upload_test.go:12-52` — fake installation repository is keyed by token; `GetByInstallationID` always returns nil.
- `user-server/internal/service/sagex_upload_test.go:120-124,134,160,179,209` — positive tests pass `"tok"` as the `identifyCode` argument and do not inject a `ChannelResolver`.
- `user-server/internal/service/sagex_upload_test.go:108` — invalid-code test passes only because `"bad"` cannot resolve; it does not define the positive credential contract.
- `user-server/internal/controller/sagex_upload_test.go:63-129` — positive tests set `Authorization: Bearer tok` but send no `identify_code` body field or `X-Channel-Identify-Code` header; they also do not inject a resolver.
- `user-server/internal/controller/sagex_upload_test.go:18-35` — fake repository still exposes token lookup even though production upload does not call it.

### Effective production contract

- `user-server/internal/controller/sagex_upload.go:44-58` — Upload reads `identify_code` from JSON or `X-Channel-Identify-Code`; missing code returns 401. It never reads `Authorization`.
- `user-server/internal/service/sagex_upload.go:202-245` — `HandleUpload` accepts an identify code, resolves it through `ChannelResolver.ResolveByIdentifyCode`, and rejects resolution failures. It never calls `GetByToken`.
- `user-server/internal/service/sagex_upload.go:247-280` — after identify-code resolution, the service selects/creates an installation by tenant + installation ID and processes events.
- `user-server/internal/controller/sagex_upload.go:224` — `extractBearerToken` exists only as an unused helper.
- `user-server/internal/repository/sagex_upload.go:49-63` — `GetByToken(token,status=1)` exists but has no production caller.
- `user-server/internal/service/sagex_upload.go:163` — `GetByToken` remains in the store interface, reinforcing the obsolete token-auth mental model.

### Documentation/model drift

- `user-server/internal/model/sagex_upload.go:5-9` — claims the server must determine tenant/device solely from Bearer token authentication.
- `user-server/internal/model/sagex_upload.go:22-24` — describes `Token` as the upload Bearer credential.
- `sagex/docs/integrations/notification-upload-v1.md:284,289-290` — documents Bearer-token lookup, token-per-installation, rotation, and `status=0` revocation.

## Root Cause Hypothesis

**High confidence.** T059 changed SageX upload authentication to identify_code resolution, but the old unit/controller tests and protocol documentation were not migrated. The test doubles still simulate the previous token lookup path, so they no longer reach the assertions they are meant to protect. The schema-validation test passes accidentally because schema/batch validation occurs before credential resolution.

A second, independent root cause is protocol ambiguity: production behavior is identify_code-only, while model/docs/admin APIs still promise Bearer-token authentication and revocation. That is not a test-only problem and must not be “fixed” by changing tests alone.

## User/Operator Impact

- Six legacy tests are red on clean HEAD, reducing confidence in unrelated SageX upload behavior.
- Developers can mistakenly believe Upload still authenticates by Bearer token because the model, docs, admin APIs, and dead helper say so.
- Operators can rotate or disable an installation token expecting upload access to change, but upload authorization is unaffected.
- A disabled installation can continue uploading because `HandleUpload` never checks `inst.Status`.
- Within one tenant, an existing installation with a non-empty different `AccountID` can continue processing under that original account; the service does not reject account mismatch with the resolved identify code.

## Effective Authentication and Status Semantics

| Concern | Current clean HEAD behavior |
|---|---|
| Primary credential | `identify_code` from JSON or `X-Channel-Identify-Code` |
| Bearer token | Ignored by Upload; `extractBearerToken` unused |
| Tenant resolution | `ChannelResolver.ResolveByIdentifyCode` |
| Installation lookup | Tenant context + client-supplied `installation_id` |
| Missing installation | Auto-created with a generated token and `Status=1` |
| Existing installation status | Not checked; disabled rows still process |
| Existing account mismatch | Not rejected; original `inst.AccountID` remains authoritative |
| Token rotation/revocation | No effect on upload authentication |
| DB update/touch errors | Some are ignored (`AccountID` backfill and `TouchLastSeen`) |

## Proposed Remediation

### A. Minimal test-only migration (approved candidate scope)

Migrate the six old tests to the **current effective identify_code-only behavior** without changing production code or making a protocol decision.

1. Add a test-only valid identify-code fixture and fake `ChannelAccountRepository` that returns a canonical tenant/account for that code.
2. Inject `NewChannelResolverWithRepos(...)` into each positive service/controller fixture.
3. Replace positive service calls that pass `"tok"` with the valid identify-code fixture.
4. Update the service fake installation repository to look up seeded rows by `installation_id` (and tenant where relevant), not only by token. This is required for tests such as `MapsQQAndWeChat`, which expect the seeded `AccountID` to survive.
5. Add `identify_code` to controller request bodies or `X-Channel-Identify-Code` headers; remove `Authorization: Bearer tok` from positive tests so they do not imply token auth.
6. Keep `TestSageXUpload_Unauthorized` as an invalid-identify-code test, and make `SchemaAndBatchValidation` use a valid injected resolver so it cannot pass merely because validation runs before auth.
7. Add explicit negative contract tests:
   - valid Bearer token without identify_code → 401;
   - invalid identify_code with a valid Bearer token → 401;
   - valid identify_code without Bearer token → accepted.
8. Do **not** add assertions that enshrine disabled-installation acceptance, token rotation, or account-mismatch behavior; those belong to the production protocol decision below.

**Files likely to change (test-only)**:

- `user-server/internal/service/sagex_upload_test.go`
- `user-server/internal/controller/sagex_upload_test.go`
- Optional new test helper files under the same two packages

**No production files should change in this test-only lane.**

### B. Separate production protocol/security decision (not approved here)

Choose and document one authoritative contract:

1. **Identify-code-only**:
   - remove or deprecate Bearer token fields/APIs/helpers that imply upload authentication;
   - stop issuing/rotating upload tokens or clearly redefine their purpose;
   - update model/docs/client contracts;
   - decide whether installation rows are only idempotency records and derive account from identify_code every time;
   - enforce installation `Status` if revocation remains supported.
2. **Dual credential (identify_code + Bearer token)**:
   - Controller extracts and forwards the token;
   - service authenticates via `GetByToken`, requires `status=1`, and cross-checks token tenant/account/installation against identify-code resolution;
   - reject missing, wrong, disabled, or cross-tenant tokens;
   - update docs/tests/clients together.
3. **Account/status rules**:
   - reject existing installation account mismatch rather than silently using the old account;
   - define whether disabled installations are terminal;
   - surface DB update/touch errors instead of ignoring them.

This decision must be made in a separate security/protocol lane. The test-only migration should not silently choose it.

## Tests to Add or Update

### Test-only lane

- Six existing failures migrated to valid identify_code fixtures.
- Valid identify code without Bearer succeeds.
- Bearer without identify code fails.
- Invalid identify code fails even when Bearer is present.
- Fake installation lookup is keyed by installation ID so seeded account/status fixtures are actually exercised.
- Existing schema/batch validation still passes for the intended validation reason.

### Future production protocol lane

- Missing/wrong/disabled/cross-tenant token behavior, if Bearer is retained.
- Disabled installation cannot upload, if status revocation is retained.
- Same tenant/different account installation ID is rejected.
- Token rotation invalidates old token immediately or according to an explicit policy.
- Documentation/model/API contract tests match the chosen authentication model.

## Risks & Considerations

- Migrating tests first may make the suite green while leaving the production security ambiguity unresolved; the separate decision must remain visible and tracked.
- Deleting Bearer assertions before a protocol decision could hide a real authentication gap.
- Keeping token fields/APIs while tests assert identify_code-only perpetuates operator confusion.
- The upload route is public apart from `InitGuard`; identify_code is currently the sole effective credential, so its issuance, rotation, entropy, and rate limiting deserve explicit security review.
- No DB, build, test, browser, deployment, or outbound actions were performed in this assess-only lane.

## Open Questions

- [NEEDS CLARIFICATION: Is identify_code-only the intended permanent contract for `/api/integrations/sagex/upload`, or should Bearer token be restored as a second credential?]
- [NEEDS CLARIFICATION: Should `status=0` revoke upload access? If yes, production must enforce installation status.]
- [NEEDS CLARIFICATION: Should an installation ID be reusable across accounts within one tenant, or must account mismatch fail closed?]
- [NEEDS CLARIFICATION: Should Bootstrap continue issuing tokens if upload does not consume them?]
- [NEEDS CLARIFICATION: Should the legacy `/api/integrations/sagex/upload` route be removed in favor of `/api/bridge/gateway`, or remain a supported adapter?]

## Main adjudication (2026-11-25)

The original SAGEX-UPLOAD requirement states that the later protocol retains **identify_code only**. The effective production behavior already matches that direction. Bearer token is therefore **not** a second upload credential, and this lane must not restore token authentication.

Approved sequence and scope:

1. **Test-only migration first (no production changes):** inject a valid fake identify-code resolver; migrate the four service and two controller positive tests; key fake installation lookups by installation ID; add explicit Bearer-only→401, invalid identify_code→401, and valid identify_code without Bearer→accepted regressions. Schema/batch validation tests must use a valid resolver so they cannot pass accidentally before authentication.
2. **Production identify_code-authority hardening after test migration:** use explicit tenant-aware installation lookup and atomic create-if-absent; reject disabled installations; reject same-tenant/different-account installation reuse; propagate account backfill/touch errors instead of swallowing them; validate installation ID schema bounds; map typed disabled/forbidden/conflict failures to protocol errors without token/internal leakage.
3. **Contract cleanup:** update model and SageX upload protocol documentation to state identify_code-only. Remove the unused Upload `extractBearerToken` helper and remove token lookup from the Upload service interface if no production caller remains. Legacy installation token fields/admin/bootstrap responses may remain as deprecated compatibility metadata, but must not be described or tested as upload authentication.
4. **Deferred:** bootstrap/upload rate limit and audit, route removal, real-device/UI acceptance, unrelated v3.114.0 migration registration, and any new Bearer-auth design.

Required gates: failing regressions before production changes; scoped service/controller/repository tests; concurrency where create-if-absent changes; full affected packages; build/vet/layer/architecture; reports and stable-worktree notification for main independent verification.
