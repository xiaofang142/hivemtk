# 二次调研与决策

1. **只改 CreatedAt**：不能区分未测0，也不能解决会话跨轮拼接，拒绝。
2. **将新字段塞 Output JSON**：避免schema，但旧输出可为字符串/截断JSON，聚合和UI都脆弱，拒绝。
3. **新增可空计时字段与作用域关联**：采用。CreatedAt保留写入时间；操作时间单独可空，Measured显式，历史不补测。
4. **全部追踪正文直接hash**：当前 trace_learning/aggregator.go 依赖 output.reply 和 input.content，会破坏学习；先递归凭证脱敏与合法有界JSON，计时只保存身份/数字。后续正文最小化必须同轮读取合法业务事实源，不能用 RecentHubContent 猜配对。
5. **旧 trace_id 继续作为turn**：NodeTraceID是会话稳定值，拒绝。新的turn由tenant/channel/account/conversation/inbound_identity SHA-256派生，缺范围为空；不改变旧traceId接口。
6. **直接扩大轮询/SSE/缩短输入**：不属于本项，拒绝。浏览器缺步骤埋点仍unknown，严禁用服务端ACK推断点击耗时。

源码：tracing/tracing.go（300ms批次、共享carrier、默认0）；monitor/monitor.go（旧多对多配对）；webhook_ai.go/result.go（重生成漏span）；webhook_outbound.go（持久化后才计时）；inbox_ingress_persist/outbound.go（部分计时起点过晚）。schema由 model.MessageTrace 的 AutoMigrate 管理。
