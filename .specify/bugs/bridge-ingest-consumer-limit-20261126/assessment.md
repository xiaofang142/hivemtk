# Assessment: Bridge 入站消费者越过消息上限

- 来源：二审 §3.32；既有 CH-20261125-09/10、LM-20261125-06。
- 根因：`HandleHTTPIngest` 在完整 scope 校验后立即调用 `deliverInbound`，随后才截断 `req.Messages`。统一投递器会接收超出配置上限的消息，历史保存/实时处理却只接收截断后的消息。
- 修复：保留整批 scope 校验在前，将统一投递移到截断之后，使所有消费者使用同一批消息。
- 文件白名单：`user-server/internal/bridge/handler_http.go`、`user-server/internal/bridge/handler_http_limit_test.go`。
- 验收：真实 HTTP handler + fake dispatcher/history/ingress；201 条消息仅前 200 条进入消费者及响应；超限尾部 scope 冲突整批 400、零投递。无数据库、模型或外部请求；不发布。
