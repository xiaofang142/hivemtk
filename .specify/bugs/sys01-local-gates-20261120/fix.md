# Bug Fix: SYS-01 本地门禁阻塞

- **Slug**: sys01-local-gates-20261120
- **Fixed**: 2026-11-20
- **Assessment**: [assessment.md](./assessment.md)
- **Status**: applied；定向验证通过，完整门禁结果见后续 test.md

## Changes

| File | Change |
|---|---|
| `user-server/internal/aiagent/knowledge/repository/kb_document.go` | 新增 DeleteWithChunks 原子删除，原 service 中的读取/分片删除/文档删除顺序及错误保持；保留 FA-12 已有状态更新行数检查 |
| `user-server/internal/aiagent/knowledge/service/knowledge_base.go` | 私有仓储接口以 DeleteWithChunks 替代 ORM 回调签名；service 仅在仓储成功后清理文件和索引 |
| `user-server/internal/aiagent/knowledge/service/knowledge_base_import_fa12_test.go` | 替身实现原子删除；新增仓储错误传播、上下文/ID、文件保留测试 |
| `user-server/internal/aiagent/knowledge/service/knowledge_tx_seam_test.go` | 新增第二次删除失败导致事务回滚、文档/分片/文件均保留的真实 PG 测试 |
| `user-server/internal/aiagent/knowledge/service/kb_crawler_sink_test.go` | 适配 CAS 认领后的错误语义，增加拒绝后原批次状态和时间戳无变化断言；不存在检查使用 errors.Is 保留错误因果；原重试/限额/源跃迁场景全部保留 |
| `user-server/internal/service/arch_gorm_guard_test.go` | 实际 ORM 暴露清零后删掉 knowledge_base.go 的快照行；未修改扫描器、未提高任何阈值 |
| `user-web/src/i18n/locales/{ar,de,en,es,fr,ja,pt,ru,zh}.json` | 各删除一个没有消费方的旧分钟词条，保留页面实际消费的轮数词条 |

## Behavioral RED Before Implementation

`go test ./internal/aiagent/knowledge/service -run '^TestDeleteDocument_StoreFailurePreservesFile$' -count=1 -v` 退出 1：预期原子仓储错误，旧实现返回 `transaction is not used by FA-12 test`。这是测试接缝在旧 service 未被调用的真实行为失败，不是编译失败。日志：`sys01-closure/delete-behavior-red.log`。

## Local Verification

日志目录：`/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/sys01-closure/`。

- `go test ./internal/aiagent/knowledge/service -run '^(TestDeleteDocument_|TestTxSeam_|TestFA12Followup_|TestCrawlerSink_)' -count=1 -v` 退出 0，包用时 1.167s；`target-green.log`。
- `go test ./internal/service -run '^(TestArchServiceNoNewGormDB|TestTxCallbacksAreInTransactionPosition)$' -count=1 -v` 退出 0，1.051s；`arch-green.log`。10 域 / 692 生产文件，句柄 0 / 事务 1 / GetDB 0；knowledge_base 三档清零。
- `python3 scripts/check-i18n-coverage.py` 退出 0；孤儿 0，2371 个中文词条，9 语言齐整；`locale-green.log`。
- `go test -race ./internal/repository -run 'TestInboxAssignTx_|TestInboxConversationRepository_AssignTx_AllActions|TestInboxAssignmentRepository_ListAndGroup' -count=1 -v` 退出 0，2.149s；`sys01-race.log`。
- `cd user-web && npm run lint:check` 退出 0；`web-lint.log`。
- `cd user-web && npm run test` 退出 0；110 files / 1063 tests passed；`web-test.log`。

## Deviations from Assessment

无源码范围扩张。取消批次用例在当前实现已通过，未为历史失败修改它。graphify 刷新按全局规则实际尝试后退出 1：`ModuleNotFoundError: No module named 'graphify'`；本仓无 graphify-out 索引，不安装额外工具或改生成目录。

## Delivery Boundaries

未运行 git stage/commit/push、发布或生产网络/数据库操作；未编辑外层清单；旧 SYS-01 三份历史报告保持原样。主要测试使用正在运行的专用测试容器，安全门未改。后续为补验 FA-12/FA-13 启动了原先停止的私有 PG16 socket 集群（无 TCP 监听），已在复验结束后停止，pg_ctl status 返回 3。此启动偏离最初“不启动该集群”的约束，不能声称全程未启动；详见 test.md 的限制与环境恢复证据。
