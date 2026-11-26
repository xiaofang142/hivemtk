# Fix: CRM-24 release blockers

## Changes

- `service/channel_resolver.go`: derive the channel lookup context from the verified account tenant with `tenant.OnlyTenant`. Preserve the edition and parent cancellation/deadline. A conflicting ordinary tenant remains rejected; a system scope is narrowed to the verified tenant.
- `service/channel_resolver_boundary_test.go`: exercise the real PostgreSQL channel repository with unbound, matching, conflicting and system contexts, including edition, deadline and cancellation checks.
- `bridge/tenant_binding.go`: fall back to channel identification codes only for an unresolved account identity. Preserve operational lookup errors and map invalid or ambiguous identities to the existing bridge errors.
- Complete the five existing SageX test fixtures with explicit repository availability, nonzero channel IDs and matching channel repositories. Keep production validation and existing security assertions.
- Apply gofmt to the exact 52 baseline paths recorded during assessment. Fifty-one paths are formatting only; the overlapping SageX fixture has the separately described fixture correction.
- `bridge/handler_http_mock_e2e_test.go`: map each canonical numeric account ID to its fixture's unique account identification code. The two Douyin accounts and all echo, immediate-return, multi-channel, concurrent and acknowledgement-isolation assertions remain intact.

## Integration and preservation

- Fast-forwarded the isolated `master` from `15376884ae9596d244a7426899bbac355085f262` to `1bb9ead90ad793c1612dbc3b09c1da07e127d71c` and restored the owned changes and index.
- The upstream navigation commit already removes the nine obsolete raw translations. No local frontend source diff remains.
- The original checkout and its concurrent work remain outside this release snapshot. Recovery stash `f18bb1abe5394534f8dcbd2a714dbbc33fbb4790` is retained in the isolated clone.
- No test assertions, security checks, release gates or baselines were relaxed. No database reset or migration was performed.

## Verification status

The resolver regression demonstrated behavioral RED before the production fix. The resolver, bridge and SageX focused run is GREEN across service, bridge and controller packages. The full Go run then exposed six bridge fixture failures; the fixture-only correction passed a full bridge package rerun. Combined evidence covers all 195 packages, with unchanged source fingerprints for the other 194 packages. Build, vet and lint passed. Frontend validation was interrupted by the outer pipeline's one-hour limit and remains incomplete. Commit, push, deployment and production acceptance remain pending.

## New-master contract repairs — 2026-11-26

- The later completed f030-fixturefix Go RED exposed seven contract/fixture failures. Exactly four additional test files changed; production validation and existing business assertions remain intact.
- Navigation retains the original role count/hash and leaf/path fingerprints, excluding only the separately asserted new admin-only channels container. KB repository/controller assertions now cover the current embedding audit contract, including exactly three RealStack audit types.
- Lead-mining test services supply complete detection evidence. Four new incomplete-evidence subcases prove no customer/clue writes for absent, incomplete, degraded or errored evidence.
- Focused verification completed at 14:18:49, exit 0, four packages and 70 passing test events without skips. Ownership is 69 paths; exact test-only repair scope and source hashes are recorded separately. A new complete run uses tag f030-contractfix from 14:19:34; the preceding RED and earlier snapshot results are preserved separately. Commit, push, deployment and acceptance remain pending its full and independent verification.

## Frontend authority restoration — 2026-11-26

- The complete f030-contractfix run ended RED at 14:44:34: all 195 Go packages and 39 mandatory regressions/subcases passed, but frontend tests had three failures. Do not count this snapshot as a complete pass.
- Focused RED at 14:47:42 reproduced both the existing backend-group authority assertion and the new combined channel/grouping regression (25/27 passed). All nine unchanged clue-list cases passed; their two full-run timeouts do not justify changing test thresholds or assertions.
- Removed only the obsolete analytics-to-system browser relocation from projectNavigation. Channel aggregation, action pruning, raw grants, route owners and the original authority assertion remain intact. The combined regression proves grouping preservation, channel aggregation and no additional grants.
- Focused GREEN at 14:48:50: four files, 36 cases, zero failures/pending, exit 0. Evidence: crm24-f030-navigation-focused-green-20261126.{json,log} and native JSON in the approved task temporary directory.
- Ownership expands by user-web/src/utils/menuNavigation.js and user-web/tests/unit/menu_navigation.test.js to 71 paths. A new complete run uses f030-navfix; release now requires API chain A and SPA chain D. No commit, push, deployment or closure is claimed by focused GREEN.

## Complete verification — 2026-11-26 14:58 +08:00

- f030-navfix passed all 13 gates at 14:58:44; independent verification passed at 14:58:46 with unchanged HEAD/source, complete 195-package coverage, 39 mandatory regressions and subcases passed, and exact 147-file frontend coverage with 1,336/1,336 cases passed, zero failures/pending.
- Evidence: crm24-integrated-final-gates-f030-navfix-20261126.json and crm24-integrated-verified-f030-navfix-20261126.json. Prior RED snapshots remain separate. The 122 optional Go test skips and audit SKIPs remain limitations.
- Graph refresh covers all 5,863 tracked supported code files, 58,847 nodes, 98,685 edges and 1,525 communities with source unchanged. Only optional question ranking samples 64 centrality sources (seed 0); the full source graph and communities are preserved. Credential preflight at 14:57:01 passed both gates with zero private occurrences.
- Fresh dual fetch at 15:00 confirms both masters at 0377606da, differing only by nine ledger lines. Publication will preserve all 71 owned paths and exact tested source. Commit, dual push, API/SPA publication and acceptance are still pending this checkpoint.
