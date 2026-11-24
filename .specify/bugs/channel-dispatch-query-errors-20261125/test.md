# Bug Verification: channel dispatch query failures are no longer swallowed

- **Slug**: channel-dispatch-query-errors-20261125
- **Tested**: 2026-11-25T01:29:30+08:00
- **Assessment**: ./assessment.md
- **Fix**: ./fix.md
- **Result**: partial

## Summary

原始症状已在冻结 `934825c2d` 快照中复现并验证消失：4 个查询错误传播 RED 转 GREEN，选择矩阵 8/8 通过，build/vet/分层/架构/repository/app 均通过。实际工作树中两个 scoped 文件与快照完全一致且生产 build 通过，但 service 定向测试与 vet 被外部 `lead_miner_unified_test.go` 的缺失符号阻断，因此实际工作树整体结果记为 `partial`，不虚报全绿。

## Checks Performed

| Check | Command / Action | Result | Notes |
|-------|------------------|--------|-------|
| Reproduction (pre-fix) | 冻结快照 `go test ./internal/service -run '^TestChannelDispatchService_' -v` | fail（预期 RED） | channel 错误变 `channel required`；conversation/sticky/preferred 错误降级为 round-robin |
| New / updated tests (post-fix) | 冻结快照同命令 | pass | 5 个错误传播用例 + SelectionMatrix 8/8 |
| Production build（冻结快照） | `go build ./...` | pass | rc=0 |
| Vet（冻结快照） | `go vet ./...` | pass | rc=0 |
| Layer gate（冻结快照） | `python3 scripts/check-layer-boundary.py` | pass | 0 新增 |
| Architecture gate（冻结快照） | `bash scripts/check-architecture.sh` | pass | 1 既有 warning |
| Repository regression（冻结快照） | 两个 channel-dispatch query 测试 | pass | 2/2 |
| App regression（冻结快照） | `TestE2E_ReachPipeline_ChannelDispatch` | pass | rc=0 |
| Scoped file integrity（实际工作树） | `cmp` 两个文件 vs 冻结快照 | pass | 均 IDENTICAL |
| Production build（实际工作树） | `go build ./...` | pass | rc=0 |
| Repository regression（实际工作树） | 两个 channel-dispatch query 测试 | pass | 2/2 |
| App regression（实际工作树） | `TestE2E_ReachPipeline_ChannelDispatch` | pass | rc=0 |
| Layer / architecture（实际工作树） | 两道静态门 | pass | rc=0 / rc=0 |
| Service targeted（实际工作树） | `go test ./internal/service -run '^TestChannelDispatchService_'` | blocked | 外部 `lead_miner_unified_test.go` 缺 4 个符号，包测试编译失败 |
| Vet（实际工作树） | `go vet ./...` | blocked | 同外部测试文件 |

## Output Excerpts

冻结快照 GREEN：

```text
--- PASS: TestChannelDispatchService_ChannelQueryErrorIsNotSwallowed
--- PASS: TestChannelDispatchService_ConversationQueryErrorIsNotSwallowed
--- PASS: TestChannelDispatchService_StickyAccountQueryErrorIsNotSwallowed
--- PASS: TestChannelDispatchService_PreferredAccountQueryErrorIsNotSwallowed
--- PASS: TestChannelDispatchService_RoundRobinListErrorIsNotSwallowed
--- PASS: TestChannelDispatchService_SelectionMatrix
ok hivemtk-user/internal/service
```

实际工作树阻断：

```text
internal/service/lead_miner_unified_test.go:89:14: undefined: MineUnifiedLead
internal/service/lead_miner_unified_test.go:120:19: undefined: MineUnifiedLeadDetailed
internal/service/lead_miner_unified_test.go:155:13: s.ProcessBridge undefined
internal/service/lead_miner_unified_test.go:183:14: undefined: MineUnifiedLeadDetailed
```

## 主智能体独立复验（2026-11-25 01:31–01:32 +08:00）

主智能体在干净补丁快照 `head-934825c2d/` 亲跑：

- service：5 个错误传播测试 + SelectionMatrix 8/8，PASS。
- `go build ./...`、`go vet ./...`，PASS。
- repository 定向：2/2，PASS。
- app dispatch E2E，PASS。
- layer boundary：1753 files / 0 新增，PASS。
- architecture：10 组 PASS，仅既有 `wecomCSService` warning。
- 实际两个 scoped 文件与干净快照 IDENTICAL。
- 实际工作树 production build/repo/app/layer/architecture 也通过；service/vet 仍被外部 `lead_miner_unified_test.go` 缺失符号阻断。

因此本报告保持 `Result: partial`，A 不由本子智能体宣布关闭。

## Residual Risks

- 实际工作树 service 测试/vet 的最终绿灯需等待外部 lead-mining lane 恢复编译；本子智能体未修改该文件。
- 桥接旧契约测试漂移属于 E lane，与本 bug 修复无关。
- 未做 UI、真实外发或生产验收。

## Recommendation

修复在冻结快照中已验证，实际产品文件已安全回植且生产 build 通过。建议主智能体独立复验后在外部 lead-mining 测试恢复编译时补跑实际 service/vet；在此之前保持 `partial`，不宣称实际工作树全绿。
