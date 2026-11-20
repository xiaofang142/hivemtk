# Bug Assessment: 实验列表分页元数据与实际查询不一致

- Slug: api-ab-pagination-20261120
- Created: 2026-11-20
- Source: 本轮逐路由 curl，user-web #1
- Verdict: valid
- Severity: medium

## Symptom / Reproduction

独立部署 8401 已登录，GET `/api/ab-experiments?page=-1&page_size=-2` 返回 HTTP 200、code=0、data.page=-1、data.page_size=-2。证据：`docs/verification/api-audit-2026-11-20/user-web/001-off-invalid-page.json`。正常 page=1&page_size=2 返回正常空数组。

## Root Cause

`internal/ops/controller/ab_experiment.go:GetExperimentList` 忽略 Atoi 错误并直接回显未经归一化的入参；`internal/ops/repository/ab_experiment.go:GetAll` 则独立把负页码改成 1、无效页大小改为 20、大页大小限到 200。控制器回显和仓储查询参数发生分裂。

## Proposed Remediation

控制器使用已有 utils.ParsePagination，默认每页 10、上限 200、超限钳制，与现有仓储最大值保持兼容；非法/负数回落默认值。将归一后的参数同时传入 Service 并用于响应，避免两套数值。保持现有 200 成功契约，不新增依赖。补集成回归检查默认/负数/非数字/超限/合法分页的具体 page/page_size、列表长度和 total。

## Files likely to change

- `user-server/internal/ops/controller/ab_experiment.go`
- `user-server/internal/ops/controller/ab_experiment_test.go`
- 本轮 API 清单与证据、此 bug 报告

## Tests to add or update

`TestABExperimentController_GetExperimentList_NormalizedPagination`，通过真实测试库写入 3 条记录，逐场景断言响应 page/page_size 和列表长度；修复前运行证明失败，修复后通过。构建并对独立/SaaS 模式分别 curl 回归。

## Risks & Considerations

仅改变非法分页参数的回落元数据，合法请求/默认每页 10/最大每页 200 保持；不触碰其他会话的前端改动。Go 测试连接固定专用 8217 实例。仓库 `.specify/extensions.yml` hooks 为空。
