# Bug Fix: 智能体删除响应码语义与错误原文透出

- Fixed: 2026-11-21
- Assessment: ./assessment.md
- Status: applied

## Changes

`AIAgentController.Delete` 分三档：IsNotFoundError → `response.NotFound(c, "智能体不存在")`（与 Get 同族同文案）；service 业务拒绝文案原样透出（"智能体被 N 个渠道账号绑定，请先解绑"是有意用户可见文案）；其余经 response.Error 兜底剥离内部细节。不再把 gorm 原文带进 data。

## Verification

- 修复前：跨租户 DELETE → 400「删除失败」+ data:"record not found"（028-on-cross-delete.json）。
- 修复后：跨租户 DELETE → 404「智能体不存在」data 无原文（028-on-cross-delete-fixed.json）；不存在 ID → 404（028-off-missing-fixed.json）；本轮无绑定临时 agent 删除 → 200（028-on-delete.json/028-off-delete.json，修复前取证）。
- `go build ./internal/controller`、`go vet ./internal/controller` exit0；既有 `TestAIAgentService_DeleteWithReference` 文案语义不受影响（service 未改）。
