# Bug Assessment: Knowledge document deletion exposes a GORM transaction to Service

- **Slug**: service-gorm-transaction-seam-20261120
- **Created**: 2026-11-21
- **Source**: Conversation: “彻底的解决问题啊”; scope expansion explicitly authorized with “授权扩大范围”.
- **Verdict**: valid
- **Severity**: medium

## Symptom and Reproduction

The full root service test package fails its existing architecture gate. Running `go test ./internal/service -run '^TestArchServiceNoNewGormDB$' -count=1` in this isolated worktree reproduces the failure: `knowledge_base.go` has two GORM transaction callback types against its permitted maximum of one. The November 21 reproduction scanned 10 domains / 692 production files, measured 0 handles / 3 callbacks / 0 GetDB calls overall, and failed with the single 2 > 1 violation (package duration 1.055s).

## Root Cause

Confidence: high. `kbDocumentStore.Transaction(context.Context, func(*gorm.DB) error)` exposes a database callback in the service-owned interface; `KnowledgeBaseService.DeleteDocument` also performs document lookup, chunk deletion and document deletion directly inside a second callback. The repository currently forwards a transaction without encapsulating these queries. Merely changing the ratchet allowance would conceal this leak.

## Proposed Remediation

Move the complete atomic database deletion to `KBDocumentRepository.DeleteWithChunks(ctx, id)`, returning the deleted document only after commit. Service calls that domain operation and retains its existing post-commit file and index cleanup. Preserve context propagation, lookup errors, chunk error wrapping, delete order and transaction rollback. Adapt the existing processing-test fake to the domain operation.

### Files likely to change

- `user-server/internal/aiagent/knowledge/repository/kb_document.go`: replace the exposed generic Transaction seam with atomic document/chunk deletion.
- `user-server/internal/aiagent/knowledge/service/knowledge_base.go`: remove database types and queries; call the repository operation.
- `user-server/internal/aiagent/knowledge/service/knowledge_base_import_fa12_test.go`: adapt the existing fake interface implementation.
- `user-server/internal/aiagent/knowledge/service/knowledge_tx_seam_test.go`: pin delegation, error/context propagation, rollback and post-commit cleanup as well as existing cascade/not-found behavior.
- `user-server/internal/service/arch_gorm_guard_test.go`: only remove this file's ratchet exception once its measured count reaches zero, as required by the existing gate's explicit “已清零，请删行” rule. Keep scan logic, assertions and all other allowances unchanged. This tightens the gate; do not raise the current allowance.
- This new bug directory's assessment/fix/test reports; append precise verification to the existing FA-CHANNEL-04 reports and existing tracker rows after verification, without creating an additional adoption task.

### Tests and Verification

Use the existing failing architecture test as the reproduction. Add a service delegation regression before production edits and observe it fail against the current Transaction-based implementation. Preserve the PostgreSQL cascade/not-found/file tests and add chunk-failure and document-failure rollback tests with files/indexes remaining on failure; verify successful index cleanup. Run focused tests, knowledge service/repository/controller packages, the complete root service suite without skips, build, vet, lint, layer boundary and diff checks.

## Risks and Scope

No HTTP/API, model, migration or dependency changes. Do not introduce a new deletion policy or change tenant-mode configuration; continue using the caller context and existing scoped database callbacks. Preserve existing best-effort cleanup semantics after commit. All source edits stay in this isolated worktree; concurrent main-checkout knowledge work is untouched. Production release and real-channel acceptance are outside this local fix and remain pending separate authorization. No commit, push, deploy or destructive operation is authorized.
