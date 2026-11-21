# Bug Fix: 绑定写路径 agent 归属闸

- Fixed: 2026-11-21
- Assessment: ./assessment.md
- Status: applied

## Changes

- `repository/agent_kb_binding.go`：新增 `ErrAgentNotFound`/`AgentNotFoundError` 哨兵与 `resolveAgentExists`（查 ai_agents，ErrRecordNotFound → 带ID哨兵；软删与租户隔离 callback 同 KB 校验口径生效）。三个写事务（BindOverwrite/ReplaceByAgent/BatchBindOverwrite）开头接入；BatchBindOverwrite 按 agentID 去重避免重复查询。
- `controller/agent_kb_binding.go`：BatchBind 补 `IsNotFoundError` → 404 透出带 id 文案分支，与 Bind/ReplaceByAgent 同口径。
- 测试：tx 用例 seed 各自 agent（7–11）且 setup 增迁 AIAgent；租户测试新增 `WriteRejectsForeignAgent`（B 对 A 的 agent 三写路径全拒、A 绑定原样保留）。

## Local Verification

- 修复前 HTTP 复现（双租户真实请求）：见 assessment Reproduction 与 021-on-cross-replace-own-kb.json（200 幽灵行）、库内 tenant_id=2/agent_id=12 行。
- 修复后：`go test ./internal/repository -run 'TestAgentKBBinding' -count=1` 结果见 .planning/akb-agent-gate-green.log；`go build ./internal/repository ./internal/controller` exit0。
- 修复后将重构建双模式服务做 HTTP 复测：B 三写路径 404「智能体 12 不存在」、A 绑定行不变。

## Deviations from Assessment

无新增范围；controller BatchBind 404 透出为评估内 controller 文件的最小一致性补充。
