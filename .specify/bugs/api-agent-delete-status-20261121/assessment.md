# Bug Assessment: 智能体删除响应码语义与错误原文透出

- Created: 2026-11-21
- Verdict: valid
- Severity: low

## Reproduction（SaaS 8402，B 会话删除租户 A 的 agent 12）

- `DELETE /api/ai-agents/12` → **400 INVALID_PARAM_1001「删除失败」+ data:"record not found"**。
- 同族 `GET /api/ai-agents/4294967295` → 404「智能体不存在」。证据 028-on-cross-delete.json、024-off-missing.json。

## Root Cause

`AIAgentController.Delete` 把 service 全部错误（含 gorm.ErrRecordNotFound）压成 400，并以 `response.Error(c, 400, "删除失败", err.Error())` 把内部错误原文带进 data。违反同族状态码一致性与"错误响应禁止透传内部原文"口径（errhttp.HandleDBError 的 IsNotFoundError→404 通道现成，controller 已 import errhttp）。

## Proposed Remediation

Delete 分三档：`IsNotFoundError` → `response.NotFound(c, "智能体不存在")`（与 Get 文案一致）；service 业务拒绝文案（"智能体被 N 个渠道账号绑定，请先解绑"）是有意用户可见文案，原样透出；其余错误 `response.Error(c, 400, err.Error())` 经既有兜底剥离内部细节。不新增依赖，不改 service。

## Tests

现有 `TestAIAgentService_DeleteWithReference`（有绑定拒绝）语义不变。HTTP 回归：跨租户删除→404「智能体不存在」且 data 无 "record not found"；无绑定临时 agent 删除→200；被绑定删除拒绝文案保留（由既有服务测试覆盖文案语义）。

## Files likely to change

- user-server/internal/controller/ai_agent.go
