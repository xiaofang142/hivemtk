# Bug Verification: 知识爬虫入索引的租户恢复与失败重试

- **Slug**: knowledge-crawler-tenant-retry-20261120-sol
- **Tested**: 2026-11-20
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: partial

## Summary

最终 19 个 FA-12 顶层回归测试（含 race）和 15 个相邻 DB-free 单元测试通过。受控 GORM 回调和假 HTTP/LLM 验证租户上下文、错误透传、索引/QA 终态、同 hash 重试及重建服务后的恢复，断言 PostgreSQL 打开连接数为 0。真实数据库隔离和操作系统进程中断/重启未验证，结果保持 partial。

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| 初次测试 | 导入状态测试，命令见下文及 fix.md | 流程不合规 | Exit 1 是 undefined seam 编译失败；当时未先得到业务 RED 就修改了导入生产代码。 |
| 导入基线复现 | Go overlay 注入原始 HEAD 导入代码，命令见下文 | RED | Exit 1；实际状态为 indexed/processing，与 failed 断言相悖。属于事后复现，不能补正首次 TDD 顺序。 |
| 爬虫修改前复现 | Lookup/SystemScope/QA、hash/tenant/batch、缺 QA 服务测试，命令见下文 | RED | 三组均在对应生产修改前 exit 1，详情见 fix.md 与本会话实际输出。 |
| 最终 FA-12 回归 | focused pattern，下文完整命令 | pass | Exit 0；19 个顶层测试，含 -race。日志 final-focused-race.log。 |
| 相邻回归 | adjacent pattern，下文完整命令 | pass | Exit 0；15 个顶层单元测试。日志 final-adjacent-regression.log。 |
| 同包全量回归 | `go test ./internal/aiagent/knowledge/service -count=1` | 环境阻断 | 前一轮实际 exit 1；缺少测试密码，安全门明确未发起连接。最终轮未重跑 DB helper。 |
| Build | `cd user-server && go build ./...` | pass | Exit 0. |
| Static type checks | `cd user-server && go vet ./...` | pass | Exit 0. |
| Layer boundary | `python3 scripts/check-layer-boundary.py` | pass | Exit 0; 0 new violations. |
| Architecture | `bash scripts/check-architecture.sh` | pass with existing warning | Exit 0; warning is unrelated `webhook_wecomcs_puller.go`. |
| GORM-only | `bash user-server/scripts/check-gorm-only.sh` | pass | Exit 0. |
| Naked goroutine gate | `bash user-server/scripts/check_naked_goroutine.sh` | not clean | Exit 1 with repository-wide pre-existing hits; this change adds no goroutine. |
| Graph refresh | `python3 -c "from graphify.watch import _rebuild_code; from pathlib import Path; _rebuild_code(Path('.'))"` | 环境阻断 | Exit 1: `ModuleNotFoundError: No module named 'graphify'`，图谱未更新。 |
| Diff whitespace | `git diff --check -- user-server/internal/aiagent/knowledge/service/kb_crawler.go user-server/internal/aiagent/knowledge/service/knowledge_base.go user-server/internal/aiagent/knowledge/service/knowledge_base_import.go` | pass | Exit 0。 |
| 扩展 hooks | 读取 `.specify/extensions.yml` | 无 hooks | `hooks: {}`，无前置/后置命令。 |

## Exact Test Commands

以下命令工作目录为内层仓 `user-server/`；日志保存于本报告相邻的 `evidence/`。Go overlay 只替换构建输入，未回滚工作区文件。

```bash
# 导入原始逻辑业务 RED（exit 1，事后复现）
go test -overlay="$PWD/../.specify/bugs/knowledge-crawler-tenant-retry-20261120-sol/evidence/baseline-overlay.json" ./internal/aiagent/knowledge/service -run 'TestProcessContent_(StateWriteFailurePersistsFailedWithTenant|FinalStateWriteFailurePersistsFailed)$' -count=1

# 对应爬虫生产修改之前分别执行，均 exit 1
go test ./internal/aiagent/knowledge/service -run '^TestFA12Crawler_(LookupErrorDoesNotCreate|SystemScopeIsRemoved|QAFailureRetriesSameHash)$' -count=1
go test ./internal/aiagent/knowledge/service -run '^TestFA12Crawler_(FailureInvalidatesPreviousHash|RejectsMismatchedTenant|BatchPersistenceAndPartialRetry)$' -count=1
go test ./internal/aiagent/knowledge/service -run '^TestFA12Crawler_ExplicitQARequiresService$' -count=1

# 最终 GREEN（exit 0）
go test ./internal/aiagent/knowledge/service -race -run '^Test(FA12Crawler_|ProcessContent_|ProcessDocumentByID_|ProcessDocumentAsync_StateFailureStopsQA|CrawlerProcessingContext_)' -count=1 -v

# 相邻单元回归（exit 0）
go test ./internal/aiagent/knowledge/service -run '^Test(ParseQAOutput|EstimateTokens|ConfigGettersFallBackWithoutReader|ConfigGettersReadInjectedKeys|SetConfigReaderNilIsIdempotent|KnowledgeChunkFromMerchant_.*|DedupeMerchantChunks|Converters_KeepParentStitchedContent|MaxUploadFileSizeSeam|MerchantKnowledgeMaxLenSeam|SplitMarkdownRespectsMaxLen|MerchantBatchMaxFileSizeSeam|ParseBatchFileEnforcesMaxFileSize|ParseBatchFileTxtAutoDetect)$' -count=1 -v
```

## Output Excerpts

```text
--- PASS: TestFA12Crawler_QAFailureRetriesSameHash (0.02s)
--- PASS: TestFA12Crawler_TwoTenantsRetryAndServiceRestart (0.04s)
PASS
ok   hivemtk-user/internal/aiagent/knowledge/service  1.871s
exit=0
```

```text
测试库配置未通过安全门（未发起任何连接）：test password is missing; set POSTGRES_TEST_PASSWORD or POSTGRES_PASSWORD
```

```text
扫描 1604 个非测试 .go ... 跨层违规：命中 0 处
✅ 架构检查通过(可能有 warning,需人工复核)
```

## Closure Evidence

| 关闭判据 | 最终覆盖 | 实际边界 |
|----------|----------|----------|
| 取消请求后保留源租户 | 受控 verified | 背景 ctx 未取消；仓储读写/LLM 上下文保留租户；SystemScope 被去除。 |
| 缺租户/矛盾租户 fail-closed | 受控 verified | TENANT_MODE=on 缺源租户、源/请求租户矛盾均在存储访问前拒绝。 |
| 查询/旧分片失效/正文更新错误不吞 | 受控 verified | 真实 repository 方法经 GORM 回调注入故障；查询故障不新建；依赖恢复后可重抓。 |
| 状态、分片、向量、索引、读取及 panic 失败 | 受控 verified | 形成 failed+原因；状态写失败阻止继续 QA；相同正文可重试。 |
| 失败不计成功、不记 hash | 受控 verified | 向量/QA/更新错误后同 hash 重抓成功；后续失败失效旧 hash；完整成功后才允许跳过。 |
| QA 达到约定终态 | 受控 verified | 候选持久化 pending_review 或合法空产出；候选写失败、LLM 失败、显式开启却未装配服务都不记成功；关闭 QA 不调用 LLM。 |
| 批次状态及重试 | 受控 verified | running 写失败不发 HTTP；partially_failed 可以创建重试批次。 |
| 两租户同 URL | partial | 受控存储验证 TENANT_MODE=on 的租户参数与数据区分；未验证真实 PostgreSQL tenant callback/SQL。 |
| 重启后恢复 | partial | 重建服务/内存索引并保留同一夹具数据，失败文档恢复；未执行 OS 进程 kill/restart、真实持久化或自动恢复调度。 |

## Residual Risks

- `markDocumentFailed` is a best-effort second persistence attempt if the first status write fails; if the database remains unavailable, no code can persist the failure in that moment, and the crawler terminal check will reject the page rather than record hash.
- 真实 PostgreSQL 的同 hash 重抓、跨租户隔离和进程中断/恢复为 not-run；本轮无真实 DB 连接、迁移或表重建。
- QA 候选写入与文档完成状态不是同一事务；候选写成功而完成状态写失败、或中途崩溃后再抓，可能产生重复审核候选。未新增 exactly-once 契约。
- 同步等待 QA 增加每页耗时，受处理超时约束；未做生产延迟/吞吐验证。超时或持续 DB 故障时 failed 回写也可能失败，日志保留错误且不记 hash。
- 首轮导入修复的严格 test-first 顺序未满足；补跑原始逻辑确认断言有效，不能声称全过程 TDD 合规。
- The naked goroutine gate remains red from unrelated existing files and should not be attributed to FA-12.

## Recommendation

修复和受控回归已交付，FA-12 保持 **partial**。真实数据库与进程中断验证缺少证据，不能宣布端到端关闭。本轮保留原有 staged/在途改动，未执行暂存、commit/push、worktree、派生 Agent 或发布。
