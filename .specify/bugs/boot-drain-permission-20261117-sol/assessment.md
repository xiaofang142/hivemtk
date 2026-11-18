# 启动消费测试的授权会话前置评估

Verdict: valid / medium。

## 复现与根因

正式全量测试中`TestNewWebhookService_DrainsDueDelayedOnBoot`超时。消费者已运行，日志明确拒绝缺少可确认会话权限的任务。旧夹具只建消息、延迟队列和收件箱表，没有提供R1/R2修复后要求的真实同租户、同平台、同账号和发送者的AI会话。

## 论证与决策

为合法启动接线测试明确绑定默认测试租户并建立真实AI会话。继续只调用生产构造入口等待消费，保留10秒预算、出站归属、AI标记和pending状态断言，不绕过权限服务或直接调用派发函数。

## Files likely to change

- `user-server/internal/service/webhook_outbound_boot_drain_test.go`：仅同源模型与合法会话/租户前置。
- 本目录`fix.md`、`test.md`与总进度。

## 验证

正式失败作为RED证据；专用测试库验证启动消费正向，并联合既有延迟权限反向用例确认人工接管、已答和缺会话仍不发送。无生产队列写入或真实外发。
