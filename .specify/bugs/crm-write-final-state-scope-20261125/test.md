# Bug Verification: 客户及关联实体写入权限

- **Slug**: crm-write-final-state-scope-20261125
- **Date**: 2026-11-25
- **Result**: scoped GREEN（真实 PostgreSQL）；全量回归结果见下方记录

## RED（修复前，真实 PG）

命令：`go test ./internal/service -run 'TestCRMCustomerWrite_|TestCRMEntityWrite_' -count=1 -v`

- `TestCRMCustomerWrite_EmptyAccountScopeCannotCreate`：空授权创建 err=nil（应拒绝）。
- `TestCRMCustomerWrite_OwnerScopeRejectsZeroOwner`：OnlyOwned+owner=0 创建 err=nil（应拒绝）。
- `TestCRMEntityWrite_UnauthorizedAccountRejected`（contact/supplier/service_provider）：挂未授权账号 err=nil（应拒绝）。
- `TestCRMEntityWrite_CrossTenantAndMissingAccountRejected`：挂他租户/不存在账号 err=nil（应拒绝）。
- `TestCRMEntityWrite_UpdateRejectsUnauthorizedAccountPointer`：改挂未授权账号 err=nil（应拒绝）。

## GREEN（修复后）

同一命令全绿：9 个顶层用例 / 11 个断言（含 3 个子用例）全部 PASS，rc=0，约 3.3s。

覆盖：空授权拒绝、owner=0 拒绝、最终写入值校验、三类实体未授权/他租户/不存在账号拒绝、改挂拒绝且旧对象不变、合法创建/管理员正例、跨行业拒绝、授权账号挂接与未提交指针。

## AccountRules 配对补充（主智能体独立复验）

命令：`go test -p 1 ./internal/service -run '^(TestCRMAccountRulePairAllowsWrite|TestCRMEntityWriteAccountRulePairIsApplied|TestCRMEntityWrite_UpdateRejectsAccountRulePairMismatch)$' -count=1 -v`

- RED（修复前，真实 PG，rc=1）：测试正常编译并执行；`AllowsWrite` 10/10 通过；实体正例/管理员通过；跨行业、owner 角色不匹配、显式负责人不匹配 3 个 create 负例与 update 配对不符均 `err=nil>`，证明实体写校验遗漏 AccountRules。日志：`check-crm-account-rule-pair-red2.log`。
- GREEN（修复后，rc=0，1.538s）：配对矩阵 10/10 子用例、实体创建 6/6 子用例、update 拒绝 1/1，全部 PASS。日志：`check-crm-account-rule-pair-green.log`。
- broader：`go test -p 1 ./internal/service -run '^(TestCRM|TestAccountRule|TestCustomerPermission)' -count=1`，rc=0，3.229s；4 个文件共 28 个顶层用例。日志：`check-crm-account-rule-pair-broader.log`。
- 语义：`AccountRules` 不扩大基础 `AccountIDs`；`role=owner` 与显式负责人均按目标客户负责人校验；nil/0 实体账号指针仍表示未提交。

## 静态门（AccountRules 补充轮）

- `gofmt -l`：clean；改动文件 `git diff --check`：rc=0。
- `go build ./...`：rc=0；`go vet ./...`：rc=0。
- `python3 scripts/check-layer-boundary.py`：1770 个生产 Go，新增跨层违规 0。
- `bash user-server/scripts/check-gorm-only.sh`：业务包 `.Exec/.Raw` 0 处。
- `bash scripts/check-architecture.sh`：**rc=0（18:04:21 +08:00 复跑）**，10 组检查全部通过；service 769 个生产 Go、`internal/service` 601 个，service 直取 DB/`GetDB`/SQL 漂移均为 0；仅保留既有 `wecomCSService` interface warning。此前 18:01 的 rc=1 来自并行施工中的 `crm_merge_snapshot.go` 临时直取 DB；外部 worker 已改为注入 `consentRepo`，主智能体读取完整源码确认无 `GetDB` 后复跑转绿。日志：`check-crm-account-rule-pair-architecture-recheck.log`；首次失败读数保留在 `check-crm-account-rule-pair-gates.log`。
- 关键源文件 SHA256：`crm_customer.go=b8b4553e9f6221d07b34d7a9d8971873ecbab6cba7e8bcafb70860f1cf6d4ffd`；`crm_customer_write_scope_test.go=40cfdb0c6f417f880134c9d791cbb2ec282f3be6ecb36c201e8ce0a1c5aae74c`。

## 主智能体最终复验（2026-11-25 18:08 +08:00）

- 外部 build/test 进程清零后，主智能体重新执行 broader CRM/AccountRule/Permission 回归：rc=0，`ok hivemtk-user/internal/service 3.679s`；日志 `check-crm-account-rule-pair-final-test.log`。
- `go build ./...`、`go vet ./...`：均 rc=0（空输出日志 `check-crm-account-rule-pair-final-build.log` / `check-crm-account-rule-pair-final-vet.log`）。
- layer boundary：rc=0，1770 个生产 Go，新增违规 0；architecture：rc=0，10 组通过、769 个 service 生产文件、仅既有 `wecomCSService` warning；GORM-only：rc=0，业务包 0 处 `.Exec/.Raw`。日志分别为 `check-crm-account-rule-pair-final-{layer,architecture,gorm}.log`。
- `gofmt -l` clean，改动文件 `git diff --check` rc=0；两个关键源文件 SHA256 与上方一致，说明复验期间 AccountRules 实现未被外部改动。

## Boundary

仅本机合成数据 + 真实 PG 限定测试；未做 UI 逐元素点验、未做真机/生产/真实外发、未部署/提交。全包回归若受并行会话在途文件影响，按实际输出记录，不冒充全绿。
