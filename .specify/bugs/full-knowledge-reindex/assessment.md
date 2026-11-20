# Bug Assessment: 知识库全量重建不完整且吞错

- Slug: full-knowledge-reindex
- Date: 2026-11-20
- Source: 唯一未完成任务清单 FA-13
- Verdict: valid
- Severity: high

## Report

知识库全量重建必须覆盖 1001 条以上文档，每条只处理一次，并让缺失源、空源、向量失败和最终状态写失败成为可查询的失败结果。重建接口的成功响应应代表本次调用已经完成，而不是后台任务尚未执行。

## Reproduction / Root Cause

`user-server/internal/aiagent/knowledge/service/knowledge_service_reindex.go` 当前只调用一次 `List`，硬编码 `PageSize: 1000`，所以超过首批的文档不会进入重建；列表仅按 `created_at DESC` 排序，没有稳定的 ID 断点。`Reindex` 对零分片的缺失/空文件忽略状态写错误并返回 nil；可读文件通过 `async.RunWithTimeout` 异步处理后立即返回。分片路径对失败状态写、最终文档更新失败也忽略错误。

因此调用方无法判断重建是否完成，失败文档可能仍显示为成功或旧状态，超过 1000 条的数据会被静默遗漏。

## Proposed Remediation

在知识库重建服务内增加同步、可返回错误的文档重建核心，保留现有异步导入管线的兼容入口。同步路径必须在源不可读/为空时先写 failed，状态写失败与原始失败同时返回；向量、分片向量保存、索引构建和最终 indexed 状态写入都必须逐层返回错误，并在可行时将失败状态落库。禁止 API 在实际处理前返回“已启动”的成功。

批量重建使用稳定的 `(created_at, id)` 游标分页，固定本次调用的起始边界；同一边界内每条文档只处理一次。调用开始后新增的文档留到下一轮，写入失败或向量失败的文档保留 failed 状态并计入错误结果，不得被分页遗漏。分页查询必须在 repository 层完成，所有查询显式带 context 和租户边界。

## Acceptance Tests

- 1001 条文档全部被处理一次，稳定排序下无重复、无遗漏；调用期间追加的文档遵守冻结边界契约。
- 缺失文件和空文件最终为 `failed`，且状态写失败不再被吞掉。
- 向量服务失败返回错误并将文档标记为 `failed`；修复依赖后可重试。
- 索引构建和最终状态更新失败均返回错误，不能返回成功。
- 成功文档最终为 `indexed`；批量调用返回首个错误但仍尝试完成其余文档，并保留每条文档的可查询状态。
- 控制器只在同步服务返回成功后回成功响应；未向第三方发送任何消息。

## Files Likely to Change

- `user-server/internal/aiagent/knowledge/service/knowledge_service_reindex.go`
- `user-server/internal/aiagent/knowledge/repository/kb_document.go`
- `user-server/internal/aiagent/knowledge/controller/knowledge_workspace.go`（仅在响应语义需要调整时）
- `user-server/internal/aiagent/knowledge/service/*reindex*_test.go`
- `user-server/internal/aiagent/knowledge/repository/*reindex*_test.go`
- `.specify/bugs/full-knowledge-reindex/{fix,test}.md`
- `docs/operations/PROD_TOPOLOGY_CHANGES.md`（发布后）

## Boundaries

本项只修复显式重建路径，不顺带改造普通上传/导入异步管线；若共享代码暴露出独立缺陷，另行建评估。重建使用现有租户上下文和仓储，不增加跨层 SQL。并发新增文档以调用开始时的稳定游标边界为契约，后续调用负责处理新增项。
