# 修复记录

依据[assessment.md](assessment.md)，仅修正QQ全链测试辅助函数的模拟依赖生命周期。

原辅助函数内的两个defer在返回时立即恢复模拟QQ API与静默测试设置，异步发送因此离开本地模拟服务。改为`t.Cleanup`，保持依赖至整个测试结束；增加同源DelayedOutboundReply模型前置。生产QQ代码、凭证、发送策略、验签及30秒等待预算不变。

正式全量三项失败为RED；修复后QQ完整8项通过57.729秒，均使用现有本地httptest平台，不发送真实消息。
