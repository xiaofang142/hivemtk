# Bug Verification: 导入作业员工范围

- **Slug**: import-job-employee-scope-20261125
- **Result**: scoped GREEN（真实 PG）

## RED → GREEN

- 新增 scoped 方法前编译失败（契约不存在）。
- `go test ./internal/service -run 'TestImportScope_' -count=1 -v`：3/3 PASS（约 3.1s），覆盖他人作业执行/报告/下载拒绝、空授权拒绝、逐行账号边界（越权行失败、合法行成功）。
- 组合限定回归：`TestImportScope_|TestCompanyScope_|TestCRMCustomerWrite_|TestCRMEntityWrite_` 全部 PASS。

## 全量 service 回归归属

`go test ./internal/service -count=1 -json` 全量：84 个失败，全部位于 `TestBridgeSubmit_*`/`TestWebhookAIResult_*`/`TestSendOutbound_*` 等 Bridge/Webhook/Outbound 域，属于并行会话在途改动或既有基线，非本轮 CRM 改动引入；本轮 CRM 限定用例 0 失败。日志：`/tmp/crm02_full_service.jsonl`。

## 门禁

`go build ./...` rc=0；限定测试全绿；gofmt clean。
