# Tasks

User authorization: implement every unfinished EC with simulated dependencies where necessary; no fake live acceptance. Source spec and plan in this directory. Existing hooks: none. Tasks execute serially.

## Phase 1: Sending safety EC-23-A/B and EC-29

- [ ] T001 Reproduce unknown retry/fallback and per-attempt authorization gaps with production pipeline tests.
- [ ] T002 Preserve typed uncertainty and actual provider attempts through response/reservation settlement in reach_send_pipeline.
- [ ] T003 Permit retry/fallback only with explicit unaccepted evidence; recheck current permissions/consent/DNC/policy for each attempt and honor Retry-After.
- [ ] T004 Add outer DryRun branch and layered receipt state without invoking production sender/reserver.

## Phase 2: Identity, audience and contents EC-22/25/26/27

- [ ] T005 Extend existing consent with brand/purpose/outlet provenance and conservative import/merge eligibility.
- [ ] T006 Persist lead source evidence and segregate unproven marketing entries.
- [ ] T007 Persist immutable audience assignments, control groups and identity pressure without resetting history after merge.
- [ ] T008 Bind approved content versions, safe rendering and current benefit/price/stock validity.

## Phase 3: Durable service and campaign execution EC-24/28/29/30

- [ ] T009 Connect an authoritative business event to a deduplicated service notification or internal reminder.
- [ ] T010 Persist existing marketing-flow runs/node attempts, absolute waits, versioned DAG, pause/exit and restart-safe dispatch.
- [ ] T011 Persist outlet policy and native reply ownership observations; missing Amazon configuration freezes new automatic dispatch.

## Phase 4: Reporting and recovery EC-31/32

- [ ] T012 Persist idempotent paid/refund/correction facts and rebuild net amounts per currency with fixed experiment denominators.
- [ ] T013 Integrate recovery epoch/tenant-resource quarantine and retain unknown keys, consent completeness and deletion tombstones.
- [ ] T014 Implement Widget assertion/asset trust gaps through existing boundaries with adversarial tests.
- [ ] T015 Complete existing frontend integration, permissions, translations and onboarding; verify real UI where available.
- [ ] T016 Run affected package tests/build/vet/lint/static gates and converge against every requirement.

## Current evidence

No G3 completion claimed before tests. Live merchant/provider observation remains pending_live_validation; incomplete EC rows stay open.
