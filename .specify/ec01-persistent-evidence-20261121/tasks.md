# EC-01 implementation tasks

- [x] T001 Add source binding and evidence models, startup registration and idempotent migration. Add core table ownership.
- [x] T002 [US1] Add a tenant-scoped repository reader and a capability resolver. Bind proofs to the exact key, active connection identity/revisions and an independently verified provenance source. Use no production verifier in this slice.
- [x] T003 [US1] Inject the persistent resolver into the existing capability diagnostic route. Keep responses secret-free.
- [x] T004 [US2] Wire bridge submissions, connector and legacy after-sale to persistent lookup. Require the actual execution credential binding and a fresh resource preflight where applicable.
- [x] T005 [US3] Add a transaction-local reader to the existing queue repository. Resolve by actual stored row and preserve old interface compatibility.
- [x] T006 Validate isolation, expiry, revocation, wrong/missing evidence, mixed action rows and the single-connection path against dedicated PostgreSQL. Run targeted tests, compile, vet and static gates.
- [x] T007 Check requirement coverage and record live proof availability as pending.

The checks above cover the local slice only. The dedicated PostgreSQL tests exercise the registered migration, tenant-scoped reads, and mixed-row queue handling with one pooled connection. The default production resolver rejects legacy context evidence and has no production runtime verifier. Generic connector and legacy after-sale do not bind HTTP credentials to a verified connection revision; they remain blocked. Test fixtures are not R evidence. Production acceptance is `pending_live_validation`; repository-wide audit remains blocked on the existing Git-indexed documentation link check.

## Controlled collection continuation

- [x] T008 Add a tenant-scoped transaction writer with connection-version/lifecycle recheck and atomic binding/evidence persistence; reject source relocation and stale observations.
- [x] T009 Add a constructor-injected server-side collection service; validate exact identity and versions, bound observation time, and keep unconfigured production denied.
- [x] T010 Test collection failures, revoked/rotated connections, cross-tenant writes, source conflicts and transactional rollback; run regression/build/vet/lint/static gates and converge.
- [x] T011 Add an independently sourced local proof verifier with current contract and exact identity checks; test forged, simulated, expired and mismatched sources through the real resolver.
- [x] T012 Bind connector write requests to the exact execution connection revision and reauthorize before transport; reject unsafe destinations and redirects with zero-outflow tests.
