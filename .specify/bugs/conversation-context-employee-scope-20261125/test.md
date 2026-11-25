# Bug Verification: 共享上下文员工账号授权

- **Date**: 2026-11-25
- **Verdict**: verified（CRM-20261125-04 限定范围；非全仓全绿声明）
- **Environment**: 专用 PostgreSQL `127.0.0.1:8217`，测试口令仅从测试容器私下转发；无业务库、真实外发或部署。

## Executed Evidence

证据根目录：`/private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/crm04-final-tjaoqawb/`。

| Command | Result | Evidence |
| --- | --- | --- |
| `go test ./internal/controller -run '^TestConversationContext_EmployeeAccountScope$' -count=3 -parallel=1 -json -timeout=10m` | 90 PASS、0 FAIL、0 SKIP，包含父测试；29 个子例重复 3 次 | `controller.log` |
| `git diff --check` | exit 0 | `diff.log` |
| `python3 scripts/check-layer-boundary.py` | exit 0 | `layer.log` |
| `bash scripts/check-architecture.sh` | exit 0 | `architecture.log` |
| `bash user-server/scripts/check-gorm-only.sh` | exit 0 | `gorm.log` |
| `go build ./...`（user-server） | exit 0 | `build.log` |
| `go vet ./...`（user-server） | exit 0 | `vet.log` |

## Coverage / Review

真实数据库和 Controller HTTP 请求覆盖：新旧授权正例、旧新并集、同租户其他账号、空授权、跨租户、管理员和既有全量作用域、缺失/负数/未知/停用员工、缺租户/系统作用域、伪造查询身份、非法授权角色/范围、旧授权撤销、读取时撤权、授权查询故障、识别码非 ACL、同平台/跨平台重名以及明确渠道消歧。

合法读取确实访问上下文表并包含消息、摘要、标签、成员角色和双方备注；拒绝请求既不访问上下文表，也不返回私有数据。内部双模块读写路径未加员工限制，原始泄漏用例已从实际 RED 转为 GREEN。

本轮子代理因会话存储故障不可用，用户已明确授权主智能体直接接手；代码审查由主智能体结合差异和上述真实测试完成，不冒称独立子代理审查。此前全量 internal 回归存在基线/其他在途失败，本报告不将限定通过扩大为全仓通过。
