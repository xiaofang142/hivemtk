# Bug Test: 工作区提交前 Go 检查失败

- **Slug**: workspace-user-go-gates-20261125
- **Fix**: [fix.md](fix.md)

## Verified

- 模型归属：`TestEveryModelTableHasModuleOwner`、`TestTableOwnersPointToRealModules`、`TestNoTableBelongsToTwoModules` 全部通过。
- 客户旅程：`go test ./internal/service -run '^TestFAJourney_' -count=1` 通过，包含缓存读取故障禁止写入的新增回归。
- Python：`PYTHONDONTWRITEBYTECODE=1 python3 -m unittest scripts/test_api_verify_full_crm.py`，26 tests，OK。
- 协议：反射清单测试及 `python3 user-server/scripts/check-wire-contract.py` 均退出 0；11 个类型的跨仓字段及必填关系一致。
- CRM、重生成与启动消费：`user-go-final-focused-focused.log` 对应专项命令退出 0，覆盖顺序重复确认、当前权限、审计错误返回及缺店铺授权零外发。
- 桥接作用域：`TestBridgeTenant_PreboundContextWithoutIdentifyCode` 修复前在 HTTP/WS 的 system scope 与租户值叠加用例失败，增加明确拒绝后退出 0。日志为 `bridge-prebound-scope-{before,after}.log`。
- 触达品牌：真实同意夹具下，修复前 Telegram 成功路径仍被拒绝；补齐品牌传递后 `go test ./internal/app -count=1 -timeout 180s` 和工具包完整测试均退出 0，见 `reach-brand-green-{app,tooluse}.log`。三个渠道各覆盖品牌缺失、不符、许可撤销时适配器调用数为 0；错误传播用例验证适配器实际被调用。
- 唤醒认证：`go test ./internal/controller -run '^TestBridgeOutboxWakeController_' -count=1 -timeout 180s` 退出 0，包含真实识别码、租户和账号一致的成功路径，以及不一致时在事件流 200 前拒绝。
- 同期部署脚本：`bash -n deploy/deploy-api.sh` 退出 0；`bash deploy/test_gate_test.sh` 为 7 pass、0 fail。未执行发布脚本。

所有日志保存在工作区 `.planning/2026-11-25-workspace-commit/`，日志名不表示旧结果已被覆盖。

## Historical Full Runs

早期完整运行发现迁移漏注册、旧 CRM/出站夹具、触达品牌传递及唤醒认证夹具等问题，均保留原始失败日志。`user-go-current-tests.log` 还记录了构建缓存文件在运行中缺失，导致后半段包无法编译；该次运行不能作为全量通过证据。最终重跑使用独立 `GOCACHE`，不改全局缓存或测试门禁。

## Full Gates

修正后的 Go 全量 build、vet、测试、audit 与 lint 结果待最终汇总，不以专项测试替代。
