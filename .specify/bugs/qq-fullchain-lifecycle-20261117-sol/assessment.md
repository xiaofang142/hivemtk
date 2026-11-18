# QQ全链模拟依赖生命周期评估

Verdict: valid / medium。

## 复现与根因

正式`make test-go`退出2，QQ群聊、单聊与重放去重三项等待本地平台回复超时。日志表明AI已生成，但请求离开本地模拟平台。`setupQQFullchain`在辅助函数内用`defer`恢复QQ模拟API和静默时段装配设置，辅助函数返回后异步回复已不再使用测试配置。

## 论证与决策

只将两个恢复动作登记为`t.Cleanup`，让模拟依赖持续至完整用例结束。保留原验签、消息ID、去重、回复内容和30秒等待预算，不修改生产QQ凭证、免打扰时段或发送逻辑。补充同源延迟队列模型，避免启动消费者访问缺失的夹具表。

## Files likely to change

- `user-server/internal/controller/webhook_qq_fullchain_test.go`：仅辅助函数的两个恢复生命周期及延迟模型前置。
- 本目录`fix.md`、`test.md`与总进度。

## 验证

正式失败作为RED证据；使用专用测试库运行完整QQ全链用例，模拟发送只访问现有本地httptest平台。不得提高超时或把未收到模拟回复改成通过。
