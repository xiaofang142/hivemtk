# Bug Fix: 商户表模块归属

- **Slug**: merchant-module-ownership-20261121
- **Fixed**: 2026-11-21
- **Assessment**: ./assessment.md
- **Status**: applied

## Summary

在现有集成/回调基座旁，为 merchant_connections、merchant_callback_subscriptions、merchant_callback_events 登记 core 归属。连接修复与可信事件观察不随单一可售业务模块停用。

## Changes

| File | Change | Notes |
|---|---|---|
| user-server/internal/model/module_tables.go | 增加三表归属及原因注释 | 保留已有在途 consent 登记与其余映射 |

## Tests Added or Updated

复用现有完整性和模块有效性测试，未削弱测试或新增镜像测试。

## Local Verification

- 初始全仓回归真实复现 TestEveryModelTableHasModuleOwner 失败，缺三表。
- 修复后第一次 model 包运行遗漏测试库环境变量，返回环境门错误；不计代码失败或通过。
- 清除继承的 POSTGRES_TEST_DSN，从现有 .env 私下导出测试密码并固定 127.0.0.1:8217 后，`go test -json ./internal/model -count=1` 退出0：481 测试/子测试通过，零跳过/失败；原完整性测试通过。
- 原始日志：/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/hivemtk-model-ownership-7nsck3pa/model.jsonl。

## Deviations from Assessment

无。扩展 hooks 为空，无钩子待执行。全仓首次回归仍在运行，其最终结果另按实际输出判断。

## Follow-ups

无新增待办；本缺陷已修复并定向验证，不代表EC-02外部平台验收完成。
