# R5：上行消息逐项范围评估

Verdict: valid；Severity: high。

## 根因与已有证据

二次审核合成网络复现：抖音顶层请求中的另一渠道、账号、会话消息仍被中转。服务端仅在消息字段为空时补顶层值，之后使用消息自身字段；顶层认证范围并未封闭消息数组范围。没有证据证明跨租户越权，不扩张结论。

## 二次论证与决策

- 不悄悄覆盖冲突字段继续入库，不逐项失败后仍执行前面合法项。
- 中转和HTTP服务端都在任何消息/历史/账号登记副作用前验证完整批次。
- 渠道按现有别名规则归一后必须一致，账号与认证顶层范围必须一致。
- 顶层会话非空时，各消息有效会话必须匹配；顶层会话为空时，保留同账号下合法多会话批次。
- 空消息字段继续按顶层继承，历史仍属于其父消息，不把历史内容误变成新的实时问答。
- ACK v2允许多个会话的items，不能把上行校验误套到该协议。

## Files likely to change

- `user-web/bridge/src/background/http-relay.js`
- `user-web/bridge/test/http-relay-boundary.test.js`
- `user-server/internal/bridge/handler_http.go`
- 新 `user-server/internal/bridge/handler_http_scope.go`
- 新 `user-server/internal/bridge/handler_http_scope_test.go`
- 本目录fix/test与重建总进度。

## 验证

两端分别先跑跨渠道/账号/限定会话/顶层冲突及混合批次失败测试，确认零网络或零入站、历史、账号登记副作用。合法省略字段、现有渠道别名、空顶层会话的多会话批次、ACK v2多会话对照必须通过。仅合成网络与测试依赖，不调用生产outbox、发送客户或修改生产队列。
