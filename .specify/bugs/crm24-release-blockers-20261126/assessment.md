# Assessment: CRM-24 release blockers

## Verdict

`confirmed` — the unmodified master baseline and isolated CRM-24 snapshot reproduce the bridge failures and SageX panic. This assessment authorizes only the necessary release blocker repairs described below. CRM-24 permission changes retain their existing separate reports.

## Reproduction and evidence

- Baseline commit: `15376884ae9596d244a7426899bbac355085f262`.
- `TestBindBridgeTenant_ValidIdentifyCodeBindsTenant` loses the account identification code after an unbound channel lookup fails.
- `TestBindBridgeTenant_ChannelFallbackQueryErrorRejected` receives the unresolved-tenant sentinel instead of the injected channel query error.
- Controller and service SageX bootstrap tests panic through an unimplemented embedded `ChannelAccountRepository.Available` method.
- Full `make test-go` ended with exit 2. Besides the genuine failures, later builds lost shared Go cache files. The repaired snapshot will use a private cache.
- `make audit` reports one locale orphan, `多账号聚合`; `make fmt-check` reports 52 unchanged baseline test files.
- Evidence logs are `crm24-release-head-baseline-failures-20261126.log`, `crm24-release-full-tests-20261126.log`, and `crm24-release-quality-20261126.log` in the task's approved temporary directory.

## Root causes

1. `resolveAccountChannel` validates an authoritative account, but reads its channel using the original context. In multi-tenant mode an unbound context cannot read that channel. A system context must also be narrowed after identity resolution, while a conflicting real tenant remains forbidden.
2. Bridge resolution treats every account-resolution error as permission to fall back, then masks fallback operational errors with `ErrBridgeTenantUnresolved`.
3. Older SageX test fakes omit `Available`, matching channel repositories, and nonzero account ChannelID values required by the hardened resolver. Production validation is correct and must remain in place.
4. Menu rendering consumes `menu.<key>`; the nine raw `多账号聚合` translations are obsolete. The checker correctly detects the orphan.
5. Fifty-two baseline test files do not match gofmt output; they require mechanical formatting only.
6. The full bridge suite seeds two Douyin accounts but sends their shared channel identification code. The hardened repository correctly rejects a channel code that identifies multiple accounts. The mock server must send each fixture's existing unique account identification code, including the second account used for isolation assertions.

## Permitted change scope

- `user-server/internal/service/channel_resolver.go`: narrow only the channel read after trusted account resolution, retain edition/cancellation and real tenant conflict checks.
- `user-server/internal/service/channel_resolver_boundary_test.go`: add regression coverage for actual repository tenant isolation, system-scope narrowing, edition preservation, and real tenant conflict rejection.
- `user-server/internal/bridge/tenant_binding.go` and its existing tests: allow fallback only on an unresolved identity and preserve operational errors. Do not broaden account selection or redesign bridge routing.
- SageX fixtures in `internal/service/sagex_bootstrap_test.go`, `internal/service/sagex_upload_test.go`, `internal/controller/sagex_bootstrap_test.go`, `internal/controller/sagex_upload_test.go`, and `internal/controller/sagex_upload_hardening_test.go`: implement complete matching account/channel fakes without weakening assertions or production checks.
- `user-web/src/i18n/locales/{ar,de,en,es,fr,ja,pt,ru,zh}.json`: delete only the obsolete raw `多账号聚合` key; preserve the live menu namespace.
- The exact 52 test-file paths captured by `gofmt -l` in `crm24-blocker-format-paths-20261126.json`: formatting only, except the separately authorized SageX fixture repair.
- `user-server/internal/bridge/handler_http_mock_e2e_test.go`: index mock authentication codes by canonical account ID and use each account's unique code. Preserve both Douyin accounts, all existing assertions and production ambiguity rejection.
- This bug directory and existing CRM-24 verification reports: record concrete test and release evidence.

## Acceptance

- Observe a failing resolver regression before production changes, then pass resolver, bridge, and all SageX regressions against dedicated PostgreSQL where applicable.
- Pass build, audit, lint, vet, formatting, full Go tests, frontend tests/lint, indexed Markdown links, and credential gates on the isolated release snapshot.
- Do not skip failed tests, relax baselines, disable tenant/edition gates, overwrite concurrent work, reset databases, or send the mutating CRM verifier to production.
- Commit the completed result on master, synchronize both official remotes, deploy through the official API release chain, and verify production with authenticated read-only checks.

## Hooks and follow-up

`.specify/extensions.yml` has `auto_execute_hooks: true` and no configured hooks. Continue with the fix and test stages, recording any necessary scope deviation explicitly.

## New-master test-contract scope — 2026-11-26

The completed f030-fixturefix full Go run accounted for all 195 packages and exposed seven failures after upstream contract changes. Necessary additional scope is exactly four test files: menu navigation, repository/controller KB release, and lead mining. Retain original permission fingerprints while explicitly checking the new admin-only channels container, assert the current embedding audit contract, and supply complete detection evidence with rejection coverage for incomplete evidence. Do not weaken production validation or existing business assertions. Ownership is now 69 paths; source provenance and full verification remain mandatory before publication.

## Frontend merge regression — 2026-11-26

The later f030-contractfix run passed complete Go coverage and all 39 mandatory regressions/subcases, but ended RED at the frontend gate (1,332/1,335 passed). Two clue-list cases exceeded the unchanged 15-second limit; the complete unchanged nine-case file passed in focused diagnosis. The third failure proves an actual merge regression: merge 7b1ba7fa retained the analytics-to-system browser relocation that fdc3aca9 deliberately removed, contrary to the backend-menu authority contract in user-web-navigation-content-20261126. Repair only that obsolete relocation block while retaining 8844f572's channel aggregation. Add one combined grouping/channel authorization regression, preserve the original authority assertion, and require a new complete verification snapshot. These two additional frontend paths expand ownership to 71 and require SPA chain D as well as API chain A.

## Verified publication scope — 2026-11-26 14:58 +08:00

All 13 f030-navfix gates completed successfully at 14:58:44; independent verification at 14:58:46 confirms exact source identity, all 195 Go package terminals, all 39 mandatory regressions/subcases, and 147 frontend files with 1,336/1,336 passing cases. Preserve 122 optional Go test skips and audit SKIP limitations; production acceptance is separate. The 71-path owned snapshot is ready for documentation-only master fast-forward, committed-source verification and dual push, followed by official API chain A with tests enabled and SPA chain D. Deployment and CRM24 closure remain pending.
