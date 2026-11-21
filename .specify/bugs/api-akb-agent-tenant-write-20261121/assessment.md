# Bug Assessment: 绑定写路径缺 agent 归属校验（跨租户播种幽灵绑定）

- Created: 2026-11-21
- Verdict: valid
- Severity: high（多租户 fail-closed 违例；读侧无泄露，写侧可对任意 agent_id 播种）

## Reproduction（SaaS 8402，租户 A=1 / B=2，agent 12 属租户 A，kb 1 属 A、kb 2 属 B）

1. B 会话 `POST /api/agent-kb-bindings {"agent_id":12,"knowledge_base_id":1}` → 404「知识库 1 不存在」（KB 校验生效）。
2. B 会话 `PUT /api/agent-kb-bindings/by-agent/12 {"kb_ids":["2"]}`（B 自己的 kb）→ **200 同步成功**。
3. 库内新增 `tenant_id=2, agent_id=12, kb_id=2` 幽灵行；租户 A 的 (12, kb1) 绑定未被删除/修改。
4. 读侧复核：A 会话 `GET by-agent/12` 只见本租户行；A 会话 `GET by-kb/2` 返回 []——读路径租户过滤良好，无数据泄露。
5. B 会话 `DELETE /api/agent-kb-bindings/12/1` → 404，A 的绑定完好（Unbind 有 CheckExists 探测）。

## Root Cause

`repository.BindOverwrite / ReplaceByAgent / BatchBindOverwrite` 三个写事务只经 `resolveKBType` 校验知识库归属，从不校验智能体归属；`service.Bind / ReplaceByAgent / BatchBind` 也没有 agent 探测（对照：`Unbind` 有 CheckExists 探测所以 404）。违反 C3 多租户 fail-closed（缺归属校验不得放行写）。

## Proposed Remediation

仓储层与 KB 校验同层补 `resolveAgentExists`（查 ai_agents，ErrRecordNotFound → 带ID哨兵 AgentNotFoundError；软删与租户隔离 callback 自动生效），三个写事务开头接入；controller.BatchBind 补 IsNotFoundError 404 透出分支（Bind/Replace 已有）。文案「智能体 {id} 不存在」命中既有 isNotFoundError 文本兜底。

## Tests to add or update

repo 租户测试（akbSetupDB 扩展 migrate AIAgent + agent 夹具）：跨租户 agent 的 BindOverwrite/ReplaceByAgent/BatchBindOverwrite 必须拒绝且不落行、不影响 A 既有绑定；本租户 agent 绑定成功。真实 HTTP 回归：B 的三个写路径全部 404，A 绑定行不变。

## Files likely to change

- user-server/internal/repository/agent_kb_binding.go
- user-server/internal/repository/agent_kb_binding_repo_tenant_test.go
- user-server/internal/controller/agent_kb_binding.go
