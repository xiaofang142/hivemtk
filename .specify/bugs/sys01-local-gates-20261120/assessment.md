# Bug Assessment: SYS-01 本地门禁阻塞

- **Slug**: sys01-local-gates-20261120（由当前授权任务派生的新目录）
- **Created**: 2026-11-20
- **Source**: SYS-01 verification closure / 当前定向复现
- **Verdict**: valid
- **Severity**: medium

## Report and Reproduction

原 SYS-01 历史报告保持原样。使用正在运行的专用 `mtk-postgres-test` 容器、127.0.0.1:8217、仅子进程读取容器测试凭证，分别运行定向用例和 i18n 静态门。

- `go test ./internal/aiagent/knowledge/service -run '^(TestFA12Followup_CancelledBatchPersistsFailure|TestCrawlerSink_BatchStateMachine)$' -count=1 -v` 退出 1：取消用例 PASS；状态机仍期待旧 running 错误，实际为“批次 1 已被认领或不在可执行状态”。
- `go test ./internal/service -run '^TestArchServiceNoNewGormDB$' -count=1 -v` 退出 1：`knowledge_base.go` 事务档 2，大于快照 1。
- `python3 scripts/check-i18n-coverage.py` 退出 1：唯一孤儿为 `自动查询最多持续{minutes}分钟`。

完整日志位于 `/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/sys01-closure/{crawler-red,arch-red,locale-red}.log`。

## Root Cause

1. FA-12 的私有文档替身接口包含 `Transaction(context.Context, func(*gorm.DB) error)`；AST 正确把它和既有 DeleteDocument 事务闭包分别计数。不能提高基线或忽略接口签名。文档/分片 CRUD 本应收口仓储，service 只应接收原子删除结果。
2. 持久队列改用 ClaimBatch CAS，拒绝新鲜 running 批次和读取不存在批次的错误语义已有变化；旧精确文本断言与新行为不一致。测试应验证拒绝后的持久状态无变化，并保留重试、限额、源跃迁场景。
3. BatchImport.vue 已使用 `自动查询最多进行{count}轮,之后可手动刷新`；9 个语言包残留的分钟词条没有消费方。删除这一个死词条，不改扫描规则/基线。

## Proposed Remediation

将文档和分片事务删除移至 `KBDocumentRepository.DeleteWithChunks(ctx,id)`，返回已删除文档供 service 清理文件/索引。私有接口只暴露模型与上下文。保留既有 Transaction 方法以免影响其他调用方；service 的 gorm 使用清零后，仅删除对应棘轮快照行（严格降低）。

**Files likely to change**:

- `user-server/internal/aiagent/knowledge/{service/knowledge_base.go,repository/kb_document.go}`
- `user-server/internal/aiagent/knowledge/service/{knowledge_base_import_fa12_test.go,knowledge_tx_seam_test.go,kb_crawler_sink_test.go}`
- `user-server/internal/service/arch_gorm_guard_test.go`（只删除已清零文件的基线行、更新说明）
- `user-web/src/i18n/locales/{ar,de,en,es,fr,ja,pt,ru,zh}.json`（各删一个死词条）

**Tests to add or update**:

- 先增加受控仓储删除失败测试，证明 service 原样返回仓储错误，文件仍存在，租户上下文和 ID 保留；在旧实现上观察真实 RED 后实现。
- 增加真实专用 PostgreSQL 中第二次删除失败导致文档和分片回滚、文件保留的测试；继续运行级联删除/不存在/成功清理文件原用例。
- 状态机拒绝当前 running 认领并保留数据库状态；不存在错误保留 gorm.ErrRecordNotFound 因果；保留全部原场景。
- 重跑 FA12/FA13 回归、SYS-01 并发专项、静态门与完整 fast Go 门。

## Risks and Constraints

不接触生产或业务数据库；测试库 helper 安全门保持原样。取消测试当前已绿，不为历史失败改实现。旧 SYS-01 三份报告不改写。不提交、不发布、不编辑外层清单。仓储事务中的 SQL/GORM 条件和顺序保持兼容；失败时不得清理文件或索引。
