# Bug Test: SYS-01 本地门禁关闭验证

- **Slug**: sys01-local-gates-20261120
- **Date**: 2026-11-20
- **Assessment**: [assessment.md](./assessment.md)
- **Fix**: [fix.md](./fix.md)
- **Result**: DONE_WITH_CONCERNS

## Outcome

SYS-01 并发专项带 race 通过；当前本地完整 Go 门、audit、lint、vet、build、层边界和前端单测均通过。关闭的是本轮已复现的本地门禁阻塞，不代表生产或真实页面验收。历史 `inbox-assignment-history-20261120` 报告未覆盖。

后续门禁修复的独立记录：[静态门后续](../sys01-local-gates-20261120-2/test.md)、[裸协程门自测](../sys01-local-gates-20261120-3/test.md)。

## Environment and Evidence

应用根：`/Users/xiaofang/Documents/www/go/hivemtk/hivemtk`。以下未另注 cwd 的命令在应用根执行，Go 直接命令在 `user-server/` 执行，npm 命令在 `user-web/` 执行。

全部日志：`/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/sys01-closure/`。

主测试连接专用容器 `mtk-postgres-test`，127.0.0.1:8217，数据库 user_db_test。Docker 测试账号/密码仅以 capture_output 读取并传入测试子进程，未打印；POSTGRES_TEST_DSN 清空；EMBEDDING_ALLOW_FALLBACK=true 仅用于本地测试。完整 fast 门先跑数据库认证 preflight，非缺少密码的假绿。

补充知识库包验证另外设置 FA12_PG_DSN，指向已有私有 socket `fa12-pg-u0tmyd4x`、55412、fa12_runner、fa12_synthetic；夹具自行建立并清理合成 schema。没有业务数据库或生产连接。

## Reproduction Before Fix

| Command | Exit | Evidence |
|---|---:|---|
| `go test ./internal/aiagent/knowledge/service -run '^(TestFA12Followup_CancelledBatchPersistsFailure|TestCrawlerSink_BatchStateMachine)$' -count=1 -v` | 1 | crawler-red.log；取消已通过，状态机期待旧错误文本 |
| `go test ./internal/service -run '^TestArchServiceNoNewGormDB$' -count=1 -v` | 1 | arch-red.log；knowledge_base.go 事务档 2 > 1 |
| `python3 scripts/check-i18n-coverage.py` | 1 | locale-red.log；唯一 orphan 为旧 minutes 词条 |
| `go test ./internal/aiagent/knowledge/service -run '^TestDeleteDocument_StoreFailurePreservesFile$' -count=1 -v` | 1 | delete-behavior-red.log；旧 service 未调用原子仓储替身，返回旧 Transaction 错误；编译成功后的行为 RED |

## Verification Results

| Command | Exit | Evidence |
|---|---:|---|
| `go test ./internal/aiagent/knowledge/service -run '^(TestDeleteDocument_|TestTxSeam_|TestFA12Followup_|TestCrawlerSink_)' -count=1 -v` | 0 | target-green.log；1.167s；失败保留文件、事务回滚、取消/超时/状态机 |
| `go test ./internal/service -run '^(TestArchServiceNoNewGormDB|TestTxCallbacksAreInTransactionPosition)$' -count=1 -v` | 0 | arch-green.log；692 个生产文件，句柄 0 / 事务 1 / GetDB 0；knowledge_base.go 清零 |
| `python3 scripts/check-i18n-coverage.py` | 0 | locale-green.log；孤儿 0，9 种语言齐整 |
| `go test -race ./internal/repository -run 'TestInboxAssignTx_|TestInboxConversationRepository_AssignTx_AllActions|TestInboxAssignmentRepository_ListAndGroup' -count=1 -v` | 0 | sys01-race.log；6 个顶层用例通过，含并发链、回滚、读取取消、租户隔离；2.149s |
| `make test-go-fast` | 0 | full-fast.log；墙钟 587.89s；数据库 preflight 成功；194 行包结果，0 FAIL；knowledge/service 85.789s，根 service 256.134s |
| `go test ./internal/aiagent/knowledge/service -count=1 -timeout 10m -v`（启用合成 socket PG） | 0 | final-knowledge.log；48.727s；94 个顶层 PASS、0 FAIL、5 SKIP；FA12 和 FA13 合成回归实际执行 |
| `make lint`（第二轮） | 0 | lint-2.log；golangci-lint 2.10.0，0 issues |
| `make audit`（第二轮） | 0 | audit-2.log；静态门通过；本地 API 探针 SKIP，见限制 |
| `go build ./...`（最终实现） | 0 | final-build.log |
| `make vet`（最终实现） | 0 | final-vet.log |
| `python3 scripts/check-layer-boundary.py` | 0 | layer.log；1604 个非测试文件，新增跨层违规 0 |
| `bash scripts/check-architecture.sh` | 0 | architecture.log；10 域，692 个 service 文件；已有 wecomCSService interface 警告，非新增 |
| `bash user-server/scripts/check-gorm-only.sh` | 0 | gorm.log；业务 Exec/Raw 0 |
| `python3 scripts/check-bare-go.py` | 0 | bare-go-green.log；现算/基线均 81，55 个文件 |
| `bash scripts/check-bare-go.test.sh`（修复固定值后） | 0 | bare-go-controls-green.log；8 组 ALL PASS，NEW/STALE/OVER/缺失/格式等反向控制保留 |
| `shellcheck --severity=error scripts/check-bare-go.test.sh` | 0 | shellcheck-changed.log |
| `/bin/bash -n scripts/check-bare-go.test.sh` | 0 | bash32-changed.log；系统 bash 3.2 |
| `npm run lint:check` | 0 | web-lint.log |
| `npm run test` | 0 | web-test.log；110 files、1063 tests passed |
| `git diff --check`（内外层分别执行） | 0 | inner-diff-check.log、outer-diff-check.log |

完整 fast 门只运行一次，编译后又删除了不可达的旧 persistChunkEmbeddings 包装。此后已重跑完整知识库 service 包、lint、audit、build、vet；未为死代码删除重复整仓长套件。最终收尾另纠正 kb_document.go 的旧 Transaction 注释，行为不变；源码哈希清单记录最终字节。

## Exact Source Scope

本轮修改 18 个源码/门禁文件；其中多份已有父任务脏改，未覆盖其其他内容：

- `user-server/internal/aiagent/knowledge/repository/kb_document.go`
- `user-server/internal/aiagent/knowledge/service/knowledge_base.go`
- `user-server/internal/aiagent/knowledge/service/knowledge_base_import_fa12_test.go`
- `user-server/internal/aiagent/knowledge/service/knowledge_tx_seam_test.go`
- `user-server/internal/aiagent/knowledge/service/kb_crawler_sink_test.go`
- `user-server/internal/aiagent/knowledge/service/knowledge.go`
- `user-server/internal/service/arch_gorm_guard_test.go`
- `scripts/bare-go.baseline`
- `scripts/check-bare-go.test.sh`
- `user-web/src/i18n/locales/ar.json`
- `user-web/src/i18n/locales/de.json`
- `user-web/src/i18n/locales/en.json`
- `user-web/src/i18n/locales/es.json`
- `user-web/src/i18n/locales/fr.json`
- `user-web/src/i18n/locales/ja.json`
- `user-web/src/i18n/locales/pt.json`
- `user-web/src/i18n/locales/ru.json`
- `user-web/src/i18n/locales/zh.json`

新增报告只有三个新 slug 目录各自的 assessment.md / fix.md / test.md；未创建外层跟踪文档或编辑清单。

最终 SHA-256：上述日志目录的 `source-hashes.json`，包含 18 个本轮源码文件及供 SYS-01 独立核验的 3 个既有父任务文件，共 21 项。哈希不是提交身份；复核前应与当前字节比较，工作区仍有其他在途修改。

## Concerns and Limits

1. **环境约束偏离已恢复**：此前为 FA12/FA13 复验启动了原先停止的私有 PG16，偏离最初“不启动该集群”约束。复验完已经停止；private-pg-stop.log 退出 0，private-pg-status.log 退出 3，内容为“没有服务器进程正在运行”。未监听 TCP。不能把启动行为描述为从未发生。
2. **真实推理未覆盖**：127.0.0.1:8208 无推理栈；以下 5 项由既有环境守卫 SKIP，不算 PASS：TestKBImport_PlainTextStillIndexes、TestKBImport_QASplitEnqueuedOnlyWhenRequested、TestKBImport_QASplitRequestedButNotWired、TestProcessDocumentAsync_PlainTextFallbackPreserved、TestRagSearcher_RealVectorSearch。合成 embedding/取消/状态落库测试已执行。
3. **本地 API 探针未执行**：audit-2.log 明确 SKIP 127.0.0.1:8204 无响应；静态 audit 退出 0 不证明在线实例行为。覆盖率门读取已有 profile，未在本轮生成新的全仓覆盖率。
4. **旧辅助脚本仍红**：`bash user-server/scripts/check_naked_goroutine.sh` 退出 1（goroutine.log），简单扫描报告跨域既有站点。未改其规则或跨域迁移；当前正式 audit 的 check-naked-goroutine.py 实测 0 hit，与 bare-go 棘轮 81/81 均通过。两种口径不能混为“所有检查全绿”。
5. **graphify 环境缺失**：实际执行全局要求的 `_rebuild_code(Path('.'))`，退出 1；graphify.log 为 ModuleNotFoundError。没有安装工具或声称图谱已更新。
6. 未做 UI 浏览器测试、真实推理测试、部署或生产验收；未 stage/commit/push，未改数据库安全门或提高任何基线。SYS-01 仍只证明同一会话分配事务的历史链、回滚、取消与租户隔离，不扩张为容量预占或缓存时序保证。

## Parent Acceptance

规格审查 `ses_e06842350ffe9EWOQI8C9FWgFv` 与质量审查 `ses_e067ecccdffeYxDiHXV32R5JZv` 均批准，质量审查无 Critical / Important / Minor。主会话复核 21 项源码哈希无漂移，私有 PG 状态为 3（已停止）。

独立复验：SYS-01 仓储 race 3.033s、知识库删除/事务/取消/状态机 race 2.831s、GORM 架构检查 1.230s，均退出 0，日志为 `sys01-closure/parent-check-{1,2,3}.log`。`go build ./... && go vet ./...`、`make audit`、裸协程门八组自测、`git diff --check` 均通过；前端 110 文件 / 1063 测试通过（83.52s）。全量 Go 结论引用上述完整 fast 日志，未声称主会话重跑长套件。前述跳过项与限制保持不变。

外层清单仅移除 SYS-01 任务行并更新该项当前验收说明；不关闭整体竞品审计或其他任务。未提交、推送或发布。
