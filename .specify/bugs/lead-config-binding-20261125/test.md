# Test: 20261125 LEAD-01 / LEAD-02 / LEAD-04 / LM-07

## 命令与真实结果

工作目录：`/Users/xiaofang/Documents/www/go/hivemtk/hivemtk-user/user-server`

1. 格式：`gofmt -w internal/service/channel_account.go internal/service/channel_account_authorization_config_test.go internal/service/channel_account_reply_binding_test.go internal/service/lead_judge_context.go internal/service/inbox_ingress.go internal/service/lead_shared_business_input_test.go internal/service/lead_rule.go internal/service/lead_mining_config_snapshot_test.go`
2. 构建：`go build ./...` → 退出码 0（多次执行，最后一次在全部实现落盘后通过）。
3. RED 阶段：`go test -p 1 -parallel 1 ./internal/service/ -run 'TestAccountAuthorizationService_|TestCustomerPermissionService_TwoEmployees' -count=1`
   - 编译失败：`undefined: NewAccountAuthorizationServiceWithDeps / AccountAuthorizationPair / ErrAccountAuthorizationInvalid…`（实现未落盘），符合 RED 预期。
4. RED 阶段（LM-07/LEAD-02/04 测试先落盘）：上述包内同名测试一起跑时首先因并行 agent 测试文件中间态（`customer_ai.go`、`channel_master_delete_test.go`、`customer_ai_draft_test.go`、`operation_log_business_test.go`、`report_profile_test.go` 等）编译失败；等待并确认这些文件属其他 agent 边界，不在本任务修复。
5. GREEN 最终（后台任务 exec_a3d569a2，exit 0）：
   ```
   go test -p 1 -parallel 1 ./internal/service/ \
     -run 'TestAccountAuthorizationService_|TestReplyAgentBinding_|TestLegacyReplyBinding_|TestSharedBusinessInput_|TestBusinessNoteEncoding|TestLeadJudgePrompt_|TestInboxTrigger_Carries|TestLeadMiningConfigResolver_|TestLeadMiningAnalysisSnapshot_|TestCustomerPermissionService_TwoEmployees|TestReplyBindingAgentContext_' -count=1
   ok  hivemtk-user/internal/service  2.378s
   ```
   覆盖的核心判据正反例：空授权/跨租户拒绝与配对保留、撤销即时生效与两员工两账号隔离；回复绑定错类型/停用/跨租户/缺失拒绝与真实模型/提示词变化捕获、旧绑定预览冲突/转向/停用过滤；备注编解码与来源/时间/事实或建议属性、长度预算、ownNamespace 过滤、双方模型输入捕获对方刚写信息且建议不当事实；发掘规则继承/关闭/legacy 统一、故障拒绝、快照版本冻结与 gate/detection/judge/persistence 共消费。
6. 早前一次 GREEN（LEAD-01 实现后）：`-run 'TestAccountAuthorizationService_|TestCustomerPermissionService_TwoEmployees'` 首次运行因测试上下文缺租户导致 3 个失败（`账号授权参数错误`），给测试加 `tenant.WithTenantID(ctx, 7)` 后后台任务 exec_06ef48fc exit 0；该失败是测试夹具问题，不是生产逻辑缺陷。

## 未覆盖 / 余项

- 未跑 DB 集成（`testutil.NewTestDB`）；按协调指令未设置 `POSTGRES_TEST_*`（8217）跑真实 PG，092 表仓储的 Create/Delete 路径只在替身层验证。
- 未跑全量 `go test ./internal/...`、`go vet ./...`、架构门禁；并行 agent 测试文件在期间持续变更，限定 GREEN 是最终证据口径。
- 边界外余项见 `fix.md`：LEAD-01 Controller/UI/菜单、LEAD-02 webhook_ai.go 接线、LEAD-04 装配提供方与 lead_mining.go 快照接线、LM-07 gate/三路/ReAct/判定/持久化实际共用快照；这些项因文件边界禁止修改，未宣称整项关闭。
