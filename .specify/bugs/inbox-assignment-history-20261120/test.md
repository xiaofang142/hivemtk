# Bug Test: 会话改派历史的并发一致性

- **Slug**: inbox-assignment-history-20261120
- **Date**: 2026-11-20
- **Assessment**: [assessment.md](./assessment.md)
- **Fix**: [fix.md](./fix.md)
- **Result**: partial

## Summary

专用 PostgreSQL 测试库中先确认旧实现会产生断裂历史并错误改写取消错误；加入 `FOR UPDATE` 行锁及保留非 not-found 错误后，新增用例和原分配动作回归通过。编译、lint、vet 与层边界检查通过。全仓 audit 与全量 Go 回归未通过：发现一个 locale orphan，以及三个知识爬虫/GORM 棘轮失败；对应知识爬虫代码当前存在其他在途修改，本轮未改动。故仅记录 SYS-01 专项通过，不将全仓门禁写成通过。

## Checks

| Check | Result | Evidence |
|---|---|---|
| 修复前回归：`go test ./internal/repository -run '^TestInboxAssignTx_' -count=1` | FAIL（预期） | `TestInboxAssignTx_ConcurrentHistoryChain` 观察到 `owner-0→owner-1` 与 `owner-0→owner-2`；取消读取被改写为 `conversation not found`。详见 [fix.md](./fix.md)。 |
| 修复后专项：`go test ./internal/repository -run 'TestInboxAssignTx_|TestInboxConversationRepository_AssignTx_AllActions|TestInboxAssignmentRepository_ListAndGroup' -count=1` | PASS | 首次 `0.863s`；最新复跑 `ok hivemtk-user/internal/repository 1.394s` |
| `go build ./...` | PASS | 退出码 0 |
| `make lint` | PASS | `0 issues.`；为解除原有格式门阻断，仅对 `internal/model/order_change_request.go` 做 gofmt，见 [fix.md](./fix.md)。 |
| `make vet` | PASS | 退出码 0 |
| `python3 scripts/check-layer-boundary.py` | PASS | 扫描 1604 个文件，0 violations |
| `make audit` | FAIL | 退出码 2；`scripts/check-i18n-coverage.py` 检出 1 个 `locale_orphans`（门槛为 0）。本轮未修改 locale/i18n 文件。门禁后续检查未运行。 |
| `make test-go-fast` | FAIL | 退出码 2。`internal/aiagent/knowledge/service`：`TestFA12Followup_CancelledBatchPersistsFailure`、`TestCrawlerSink_BatchStateMachine` 失败；`internal/service`：`TestArchServiceNoNewGormDB` 因 `knowledge_base.go` 的事务计数 2 高于快照 1 失败。上述知识爬虫文件属于工作区既有在途修改；本轮未修改这些代码。`internal/repository`（93.195s）、`internal/bridge`（11.607s）、`internal/channelgw`（10.616s）通过。 |
| `git diff --check`（内层与外层仓） | PASS | 两仓均退出码 0 |

## Residual Risks

- 全仓回归与 `make audit` 仍为红，不能据此声称整体仓库验证通过；失败项留待其既有知识爬虫工作线及 locale 基线所有者处理，本项不越界改动。
- 本次修复只保证同一会话的 `AssignTx` 事务前态、更新和历史连续；不证明自动认领资格、跨会话容量预占、缓存跨并发写入顺序或真实客服页面行为。
- 未提交、未推送、未部署；当前工作区有其他在途改动，且用户未要求执行这些不可逆交付动作。

## Recommendation

保留 SYS-01 代码与专项验证结果；全仓验收状态为 partial，不关闭整个跨仓研究任务，不把未通过门禁改写成通过。待其他在途工作线稳定后，由其所有者修复相关回归/棘轮，再按项目门禁重验。
