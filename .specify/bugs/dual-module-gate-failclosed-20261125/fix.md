# Bug Fix: 双模块执行闸门区分未迁移与故障

- **Slug**: dual-module-gate-failclosed-20261125
- **Task**: LEAD-20261125-03

## Changes

- `service/auto_reply.go`：新增 `ModuleGateDecision`（Unmigrated/Enabled/Disabled/Error）与纯函数 `decideModuleGate`；新增统一解析 `resolveModuleGateDecision`（解析/仓储错误 → Error → fail-closed）。
- `AutoReplyService.Enabled`、`LeadDiscoveryService.Enabled`：改走统一闸门，两模块各自按自身 AgentID 判定，解析故障不再被当作未迁移放行。
- 同时补齐并行会话遗留 `module_runtime_filter_test.go` 缺失的 `newModuleDB/rawDB` 辅助函数（新文件 `module_runtime_filter_helpers_test.go`），恢复 service 测试包可编译。

## Boundary

未迁移账号维持既有兼容放行；关闭模块不因故障重开；不扩展到渠道整体停用（CH-05）或配置契约（LM-07）；无发布/外发/提交。
