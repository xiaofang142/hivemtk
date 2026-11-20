# Bug Assessment: 知识爬虫入索引的租户恢复与失败重试

- **Slug**: knowledge-crawler-tenant-retry-20261120-sol
- **Created**: 2026-11-20
- **Source**: pasted text (FA-12)
- **Verdict**: valid
- **Severity**: high

## Report (verbatim or summarized)

FA-12：知识爬虫异步入索引必须携带租户并支持失败重试。旧文档更新曾吞掉旧分片失效/正文更新错误，并用 `context.Background()` 启动 `ProcessDocumentByID`；导入链路因此可能在 `TENANT_MODE=on` 下拒绝访问，文档停在 pending，而爬虫记录 hash/成功数，同 hash 重抓被跳过。要求保留租户后台任务上下文和持久任务状态，检查每次更新结果，只有索引/QA 达到约定终态才完成。

## Symptom

在租户隔离开启时，爬虫处理链如果丢失租户上下文，会在读写文档时 fail-closed；若调用方仍把该页面视为成功，则内存 hash 会阻止同 hash 重试。当前代码已修复原始 `context.Background()`、吞错、异步未等待和过早记 hash 的主要路径，但 `processContent` 在进入 `processing` 或写入最终 `indexed` 状态失败时仍仅记录日志并继续，持久状态可能与处理结果脱节。

## Reproduction

1. 在 `TENANT_MODE=on` 下，让爬虫以租户 A 的源处理新建或更新页面，并观察处理链使用的上下文。
2. 让分片、向量化或索引失败，观察文档是否持久化为 `failed`，以及爬虫是否记 hash/成功计数。
3. 以同一 hash 再次抓取，并确认失败后不会被 hash 命中跳过。
4. 让文档状态更新写失败，观察 `processContent` 是否把错误向上返回或持久化为可重试的失败状态。
5. 使用租户 B 处理另一页面，并核对两租户的读写没有互串；重启进程后重新抓取同页，确认内存 hash 不阻塞重建。

步骤 1–3、5 的代码证据已有；步骤 4 需要新增受控回归测试。真实数据库两租户/进程重启场景需要专用数据库环境，不能在本地报告中伪造已执行。

## Suspected Code Paths

- `user-server/internal/aiagent/knowledge/service/kb_crawler.go:511-563` — 页面 hash 判定、入库调用、成功计数与 hash 写入顺序；当前仅 `IngestCrawledPage` 成功后记 hash。
- `user-server/internal/aiagent/knowledge/service/kb_crawler.go:565-629` — 变更/新建文档的租户上下文、同步处理和终态核对；当前使用 `tenant.WithTenantID(context.WithoutCancel(ctx), src.TenantID)`，只有 `indexed` 才返回成功。
- `user-server/internal/aiagent/knowledge/service/knowledge_base_import.go:186-275` — `ProcessDocumentByID` 与 `processContent` 的分片、向量、索引和状态推进；处理阶段失败会 mark `failed`，但状态推进写失败只日志后继续。
- `user-server/internal/aiagent/knowledge/repository/kb_crawler.go:170-208` — 爬虫文档与分片持久化入口，所有导出方法接收 `context.Context`，租户隔离由 GORM callback 执行。
- `user-server/internal/aiagent/knowledge/service/kb_crawler_sink_test.go` — 现有爬虫落库/更新回归；依赖会 drop/recreate 的测试库，不适合新增未经授权的跨租户破坏性场景。

## Root Cause Hypothesis

**High confidence.** 原始问题根因是异步处理任务脱离了源租户上下文，且调用方把启动任务当作成功，从而在隔离拒绝时仍记录 hash。该问题的主要爬虫侧修复已存在于当前 HEAD 的 `a436a57b0`：处理上下文恢复 `src.TenantID`、同步等待、终态仅接受 `indexed`、错误向上返回，且 `setHash` 位于成功之后。剩余根因是 `processContent` 的两个状态写失败分支仍吞掉错误；这会使持久任务状态不能可靠表达可恢复失败。

## Proposed Remediation

**Preferred**: 保留现有爬虫修订；在 `processContent` 中把进入 `processing` 的状态写失败视为本次处理失败并返回，把最终 `indexed` 状态写失败也转换为可持久化的 `failed` 状态（同时保留原始错误并向调用方暴露），使爬虫终态核对不会把状态不确定的文档当作成功。`markDocumentFailed` 本身若失败必须记录原始失败原因，不能伪造成功。

补充 DB-free 或受控 fake 回归测试，覆盖租户上下文保留、失败不记 hash/不计成功、同 hash 重抓，以及分片/向量失败的持久 `failed` 状态。利用已有内存 hash 语义说明进程重启后 hash 为空；两租户真实隔离与数据库重启场景仅在专用环境实际运行后标记 verified。

**Files likely to change**:

- `user-server/internal/aiagent/knowledge/service/knowledge_base_import.go`
- `user-server/internal/aiagent/knowledge/service/kb_crawler_sink_test.go` 或同包新增回归测试文件
- `.specify/bugs/knowledge-crawler-tenant-retry-20261120-sol/fix.md`
- `.specify/bugs/knowledge-crawler-tenant-retry-20261120-sol/test.md`

## Tests to add or update

- 受控注入处理链失败时，文档最终为 `failed`，且租户值仍来自任务上下文。
- 爬虫入库失败时不增加成功计数、不写 hash；同 hash 再抓仍调用入库。
- 更新分片失效/正文回写错误向上返回。
- `context.WithoutCancel` 后仍能读取源租户，进程级 hash 清空后同页可再次处理。

## Risks & Considerations

- 现有 `KnowledgeBaseService` 依赖具体仓储/处理器，纯 DB-free 测试需要选择最小受控替身或已有内存实现，不能伪造 GORM 隔离结果。
- 真实数据库测试会使用专用测试库并可能重建表；若环境缺 `POSTGRES_TEST_PASSWORD` 或专用库不可用，应记录环境限制，不得把它写成代码通过。
- QA 拆分是索引成功后的附加动作，当前爬虫调用 `EnqueueDocument` 不返回终态；本报告只把索引 `indexed` 作为爬虫完成判据，QA 端到端需另有任务状态契约才能扩展关闭判据。

## Open Questions

- [NEEDS CLARIFICATION: 若产品要求 QA 任务也必须在 crawler 返回前达到终态，需要明确 QA 持久任务状态与重试协议；当前代码仅在索引成功后入队。]
