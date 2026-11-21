# Bug Fix: 实验列表分页元数据与实际查询一致

- Slug: api-ab-pagination-20261120
- Fixed: 2026-11-20
- Assessment: ./assessment.md
- Status: applied

## Changes

`GetExperimentList` 使用已有 utils.ParsePagination，默认每页10、最大200、超限钳制，归一值同时用于 Service 查询与响应。补充实际数据库回归，断言默认、负数、非数字、超限及合法第二页的 page/page_size/total/list长度。

## Local Verification

- 修复前 `go test ./internal/ops/controller -run '^TestABExperimentController_GetExperimentList_NormalizedPagination$' -count=1`：3 个子用例失败（负数 -1/-2、非数字 0/0、超限 201）。原始日志在工作区 `.planning/ab-pagination-red.log`。
- 修复后 `go test ./internal/ops/controller -count=1`：PASS，18.635s；日志 `.planning/ab-pagination-green.log`。
- `go vet ./internal/ops/controller/...`：exit 0。
- `go build -o bin/api-audit-pagination.exe ./cmd/api`：exit 0。
- 双模式 HTTP 回归已完成：负值回落 page=1/page_size=10，201 钳制为200；SaaS 两租户各只返回本租户记录，未认证401。证据见本轮清单001-*.json。
- 二次审核补强为205条夹具，实际断言默认10条、上限200条、第二页与末页首条ID，回归PASS（7.969s）。

## Deviations from Assessment

无。
