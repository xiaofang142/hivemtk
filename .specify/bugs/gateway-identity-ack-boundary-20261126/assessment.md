# Assessment: Gateway 可信身份与消费结果回执边界

- 来源：用户要求继续深入生产路径论证；沿用 CH-20261125-07/09/10、LM-20261125-06/17，不新增任务编号。
- 根因 1：非 Bridge gateway 的 `gatewayMessageToEvent` 先填可信 `account_id`，再复制客户端 `extra`，后者可覆盖账号；传入的可信 tenantID 未使用，客户端 tenant 元数据可继续下传。账号字段有 Hub/路由消费者，租户上下文优先的消费者已有保护，但适配器本身仍须输出一致身份。
- 根因 2：`handleGatewayReport` 只检查 ingress error，不检查 `InboxIngressResult.Accepted`。路由 discard 会返回 `Accepted=false, nil`，网关却回 accepted；nil result 同样被确认。错误分支把内部 err.Error() 放入 reason。
- 修复：复制诊断字段后固定可信账号与租户；仅明确 Accepted=true 且无 error 才确认；明确未接受回 rejected，错误或结果缺失回 retry，以固定协议 code 表达失败。复用现有 mockHandle seam 覆盖实际 HTTP handler，生产仍调用原 ingress。
- 文件白名单：`user-server/internal/bridge/handler_http.go`、`user-server/internal/bridge/handler_gateway_boundary_test.go`。
- 验收：builder 的伪账号/伪租户不能覆盖可信值，原输入不被改写；真实 Gin report 入口覆盖 accepted、false、nil、error、混合批次，错误细节不外泄。测试仅 fake/httptest，不连接数据库、模型或真实网络；不部署。
- 边界：不据此声称所有入口的可信身份、原生数组归一化或真实数据库持久化已验收；不更改既有协议允许的 legacy duplicate ACK 规则。
- 深入反例：已解析的 `BridgeResolvedIdentity` 在 report 入口只补空字段，非空渠道提示仍可决定消费分支；账号或上下文租户与缓存身份冲突也可能继续消费。同一文件内统一采用解析后的 canonical channel/account，冲突或不完整身份在消费前拒绝；新增实际 Gin 入口正反例验证零消费。
