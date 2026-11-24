# Implementation plan for EC-01 persistence

## Summary

Use the existing exact-cell rules. Store source ownership and cell evidence separately. Resolve authenticated tenant and source at execution, then read the matching binding, connection and evidence. Preserve the existing read-only API and use the existing production initialization and executor constructors.

## Scope and constitution

Repository owns GORM and locking. Capability policy receives sanitized rows through a reader interface. Service and connector apply policy before side effects. No new public endpoint, external operation or production provider registration. The controlled collection continuation below adds an internal proof-reference writer. Production defaults to denied when there is no trusted runtime verifier or the transport cannot prove it uses the resolved connection revision. Leave protected EC-02 and shared startup files intact.

## Data model and contracts

- `capability_source_bindings` has one row per `(tenant_id, source, product, resource_id)`, referencing the exact shop, region and connection ID. Blank, disabled, overwritten or ambiguous targets fail closed.
- `capability_evidences` has one current row per `(tenant_id, shop_id, product, region, action, carrier)`. It records connection, credential and consent versions, contract and documentation versions, evidence expiry, state limits and provenance references. Execution uses current connection scopes. A persisted reference or evidence-kind label alone cannot prove R. State-limited actions require actual resource preflight.
- Production tables are empty on startup and use the existing startup AutoMigrate registration. Module registration is limited to the two table names.
- For queue consumption, resolve ownership first and then lock connection, binding and evidence on the page transaction in that order. Read the binding again under lock. Make no external calls under locks. Keep the original queue interface available.
- Summary uses the authenticated tenant and exact queried key. Connector arguments remain lookup selectors only. Generic HTTP configuration cannot prove execution connection identity, so connector and legacy after-sale retain an explicit transport-binding denial after successful capability evaluation.

## Validation

Use Postgres on the dedicated 8217 instance. Test the default adapter path, exact source and action isolation, forged R, current expiry and revocation, unavailable DB, restart/reload and mixed-row queue decisions with one connection. Run touched-package tests, build/vet and repository static gates. Live evidence and release remain outside this slice.

## Controlled collection continuation

Add `service/capability_collection.go` with a constructor-injected collector, exact identity validation and bounded collection outside locks. Add a repository writer using the existing tables: lock connection, compare current versions/contract/lifecycle/expiry, lock existing source mapping, then upsert binding and evidence atomically. Reject source relocation. No HTTP endpoint, production collector registration or proof-verifier enablement is introduced. Dedicated database tests cover late revocation/version changes, cross-tenant writes and rollback on source conflicts; service tests cover missing collectors and invalid observations.

Add `capregistry/capability_proof.go` as a local-only adapter over constructor-injected proof sources indexed by product and provenance. Recheck the source's current credential contract and exact proof identity; never infer R from the stored reference. No production source registration is added. Test both adapter denials and resolver integration with explicitly fake local sources.

Extend the resolver with `AuthorizeExecution` to compare the execution connection revision against the connection used by the same authorization evaluation. Add a constructor-injected connector request builder for writes, preserving existing unconfigured behavior; validate actual request destination, reject redirect following, and reauthorize the execution identity immediately before HTTP. Test local fake transport success and zero-outflow on stale credentials/revocation/unsafe destinations.
