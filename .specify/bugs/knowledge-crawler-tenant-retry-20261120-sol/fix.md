# Bug Fix: 知识爬虫入索引的租户恢复与失败重试

- **Slug**: knowledge-crawler-tenant-retry-20261120-sol
- **Fixed**: 2026-11-20
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

补齐导入状态写失败、爬虫 QA 未等待终态、查询故障误走新建分支和失败 hash 阻断重试等缺口。处理链绑定源租户并去除 SystemScope；只有索引成功且所需 QA 候选持久化（或合法空产出）后才记 hash/成功数。状态写失败停止后续处理并尝试落 `failed`，失败页支持同 hash 重抓。

原始爬虫主修复来自当前 HEAD `a436a57b0`，本轮未重复改写：旧分片/正文更新错误向上返回；处理同步执行并核对终态；只有 `indexed` 成功后才记 hash/成功计数；新旧文档处理都使用源租户上下文。

## Changes

| File | Change | Notes |
|------|--------|-------|
| `user-server/internal/aiagent/knowledge/service/kb_crawler.go` | modified | 源租户限定、去除 SystemScope、查询错误透传、同步 QA 终态、失败清 hash、running 回写检查、部分失败批次重试。 |
| `user-server/internal/aiagent/knowledge/service/knowledge_base.go` | modified | 增加最小 `kbDocumentStore` 接缝，允许状态处理链使用受控替身测试；生产构造仍注入 `KBDocumentRepository`。 |
| `user-server/internal/aiagent/knowledge/service/knowledge_base_import.go` | modified | 读文档、processing 状态写、indexed 状态写失败均进入可持久化 `failed` 处理；状态写失败后停止处理。上传异步链同步采用该语义。 |
| `user-server/internal/aiagent/knowledge/service/knowledge_base_import_fa12_test.go` | added | 7 个顶层 DB-free 测试，覆盖状态/向量/分片/索引/读取/panic/上传失败及上下文。 |
| `user-server/internal/aiagent/knowledge/service/kb_crawler_retry_test.go` | added | 12 个顶层受控 GORM 测试，覆盖租户、查询/更新故障、QA 终态、同 hash 重试、服务重建及批次。 |
| `.specify/bugs/knowledge-crawler-tenant-retry-20261120-sol/assessment.md` | added | assess 阶段报告。 |
| `.specify/bugs/knowledge-crawler-tenant-retry-20261120-sol/assessment-followup.md` | added | 后续 QA/查询/租户边界的只读评估。 |
| `.specify/bugs/knowledge-crawler-tenant-retry-20261120-sol/fix.md` | added | 本报告。 |
| `.specify/bugs/knowledge-crawler-tenant-retry-20261120-sol/test.md` | added | test 阶段报告。 |
| `.specify/bugs/knowledge-crawler-tenant-retry-20261120-sol/evidence/` | added | 原始导入基线、Go overlay、实际 RED 和最终测试/门禁日志。日志可能被仓库默认 ignore，文件保留在本地。 |

未修改 `user-server/internal/aiagent/knowledge/repository/kb_crawler.go`：仓储方法已经接收并使用 `context.Context`，当前缺口在服务层错误处理，不需要改 SQL/仓储契约。未修改外层仓、清单或其他在途文件。

## Tests Added or Updated

- `TestProcessContent_StateWriteFailurePersistsFailedWithTenant` — processing 状态写失败后持久化 `failed`，并核对每次状态写仍有租户 42。
- `TestProcessContent_FinalStateWriteFailurePersistsFailed` — indexed 状态写失败后持久化 `failed`。
- `TestProcessContent_VectorFailurePersistsFailed` — 向量错误形成持久 `failed`。
- `TestCrawlerProcessingContext_PreservesTenantAfterCancellation` — 请求取消后上下文不再取消且绑定源租户。

## Local Verification

- Commands run: `go test ./internal/aiagent/knowledge/service -run 'TestProcessContent_(StateWriteFailurePersistsFailedWithTenant|FinalStateWriteFailurePersistsFailed)$' -count=1` → initial compile failure, exit 1, missing production test seam (`undefined: newKnowledgeBaseServiceWithDocumentStore`); not behavioral RED. See the sequencing limitation below.
- Commands run: `go test ./internal/aiagent/knowledge/service -run 'Test(ProcessContent_(StateWriteFailurePersistsFailedWithTenant|FinalStateWriteFailurePersistsFailed|VectorFailurePersistsFailed)|CrawlerProcessingContext_PreservesTenantAfterCancellation)$' -count=1` → pass, exit 0.
- Commands run: same focused tests with `-race` → pass, exit 0.
- Commands run: `go build ./...` → pass, exit 0.
- Commands run: `go vet ./...` → pass, exit 0.
- Commands run: `python3 scripts/check-layer-boundary.py` → pass, exit 0.
- Commands run: `bash scripts/check-architecture.sh` → pass, exit 0; one pre-existing warning for `webhook_wecomcs_puller.go` service interface convention.
- Commands run: `bash user-server/scripts/check-gorm-only.sh` → pass, exit 0.
- Commands run: `bash user-server/scripts/check_naked_goroutine.sh` → exit 1 due many pre-existing repository-wide naked goroutines; no new goroutine was added by this fix.
- Commands run: `go test ./internal/aiagent/knowledge/service -count=1` → exit 1 only at the repository PostgreSQL safety gate because `POSTGRES_TEST_PASSWORD`/`POSTGRES_PASSWORD` is absent; output says no connection was attempted. Focused DB-free tests passed in the same package.
- Commands run: `python3 -c "from graphify.watch import _rebuild_code; from pathlib import Path; _rebuild_code(Path('.'))"` → exit 1 because this environment has no importable `graphify` module; no graph update was possible.

## Deviations from Assessment

The assessment proposed a narrow fake-capable seam and explicit failure persistence. The implementation follows that plan. The main crawler tenant/hash/error behavior was already present in the current HEAD and was documented rather than duplicated.

### Continued fix review (before final validation)

The first RED run was a compile error, so it is insufficient as behavioral TDD evidence; the final result must include baseline behavior failing assertions. Further source tracing found that `crawlOne` still records hash/success before invoking a tenantless in-memory QA queue (`EnqueueDocument` → `context.Background()`), contrary to the assessment's original FA-12 terminal-state requirement. Also, `FindHTMLDocByTitle` treats every lookup error as not-found, and `WithoutCancel` preserves `SystemScope`, which may bypass tenant filtering. These findings are inside the original FA-12 scope. Continue the fix phase before the next test phase: use controlled GORM callbacks (no database connections) to exercise real repository/service paths; reject lookup errors, remove system scope at tenant boundaries, wait for crawler QA candidate persistence/valid empty output before success, and clear a prior hash when later processing fails. Dedicated live DB coverage remains partial.

## Verification limits

真实 PostgreSQL 两租户、同 hash 重试及真实进程中断/重启未执行；受控存储测试不能替代这些证据。QA 终态契约已在本轮收紧为候选持久化或合法空结果。整体关闭结果仍为 partial，详见 `test.md`。

## Final implementation after follow-up assessment

See `assessment-followup.md` for the read-only reassessment completed before the additional fixes.

- `kb_crawler.go`: every document lookup/write is scoped to the source tenant with SystemScope stripped; missing source tenant in multi-tenant mode and contradictory request/source tenants fail before storage access. Only actual not-found creates a document. Document processing retains its tenant after request cancellation and has a bounded timeout.
- `kb_crawler.go`: crawler QA now calls the existing synchronous `QASplitService.ProcessDocument` under tenant context. Its terminal contract is candidate rows persisted as `pending_review` or a valid empty output. QA processing uses the document's durable processing/failed/indexed states. Explicit QA enabled with missing service fails closed. No crawler QA work uses the tenantless in-memory queue.
- `kb_crawler.go`: hash/success counters advance only after document indexing and required QA succeed. Later failures clear prior successful hashes to prevent content rollbacks from hiding a retry. A batch's running write is checked before HTTP work; partially failed batches can be retried.
- `knowledge_base_import.go`: state-write failures stop processing and attempt durable failed state; both direct-content and file-upload paths are covered. Read failure is also persisted when possible. `markDocumentFailed` retains its existing explicit error log if even the fallback write cannot succeed.
- `knowledge_base.go`: private document repository interface enables a controlled state store in import tests; production constructors still accept the existing concrete repository, with no external API change.
- `knowledge_base_import_fa12_test.go`: controlled import state, vector, empty-chunk, index, read, panic, upload-QA-blocking, and cancellation tests.
- `kb_crawler_retry_test.go`: real repository calls run against replaced GORM query/create/update callbacks and a fake HTTP transport. They prove source-tenant context, update/query failure behavior, same-hash retries, QA failure/persistence/empty/disabled states, failure counters/hash behavior, batch write/retry behavior, and a fresh service/index over retained fixture state. Cleanup asserts zero open PostgreSQL connections.

No production schema, repository SQL, API endpoint, QA publish flow, external account, deployment, outer repository document, or pre-existing source change was modified. No agent, worktree, stage, commit, push, reset, checkout or clean command was invoked by this agent.

### Behavioral RED / GREEN

- First import RED was only a compilation failure for an absent seam. **This did not meet behavioral TDD and is explicitly not claimed as such.** After that initial production edit, running the original HEAD import logic via a Go overlay reproduced the desired failing assertions: processing write failure ended `indexed`; final indexed write failure ended `processing`. See `evidence/behavioral-red.log`. This retrospectively verifies the regression assertions, but cannot retroactively repair the original test-first sequencing gap.
- Before the additional crawler production changes, `go test ... -run '^TestFA12Crawler_(LookupErrorDoesNotCreate|SystemScopeIsRemoved|QAFailureRetriesSameHash)$' -count=1` exited 1 with actual behavior failures (duplicate create on read error, retained/unscoped system context, success despite QA failure). A fixture embedding cardinality issue was corrected and the rerun failed for the intended QA assertion.
- Before additional hash/batch/tenant changes, `go test ... -run '^TestFA12Crawler_(FailureInvalidatesPreviousHash|RejectsMismatchedTenant|BatchPersistenceAndPartialRetry)$' -count=1` exited 1: stale hash retained; storage queried before tenant mismatch rejection; HTTP work proceeded after batch-state write failure; partial retry rejected.
- Before enforcing missing QA-service handling, `go test ... -run '^TestFA12Crawler_ExplicitQARequiresService$' -count=1` exited 1 (`err=nil`, newPages=1, document indexed).
- Final focused regression with `-race` exited 0: 19 top-level tests passed. An additional 15 adjacent DB-free tests passed. Final build/vet/layer/architecture/GORM checks exited 0. Complete commands, results and limits are in `test.md` and `evidence/`.
