# Fix: Gateway 可信身份与消费回执

- Result: partial（本机适配器修复与回归，不代表真实数据库、设备或所有入口已验收）。
- 变更限于 assessment 白名单中的 handler 与直接测试；保留此前 HTTP ingest 截断修复及并行会话改动。

## 身份链

`HandleGateway → handleGatewayReport → bindBridgeTenant → gatewayMessageToEvent → InboxIngress`。

report 在绑定前后使用同一 `gatewayReportIdentity` 检查：有解析身份时，以 canonical channel/account 为准；显式账号冲突、上下文租户冲突和解析身份缺关键字段，均在分支选择及消费前拒绝。渠道提示仍遵循原解析器的“识别码渠道优先”约定，不新增渠道别名拒绝策略。

builder 先复制诊断 `extra`，再设置可信 `account_id`、`tenant_id`，保持调用者 map 不变。账号字段实际参与 Hub 归属及路由账号选择，因此覆盖路径有真实消费者；现有路由引擎的租户选择已优先使用请求上下文，本修复不据此宣称发现了跨租户路由读漏洞。

## 回执链

`InboxIngress.HandleIngressMessage` 的路由 discard 分支在消息持久化前返回 `Accepted=false, nil`。旧网关仅检查 error，会把这条未接收消息返回 accepted。SageX 契约 `docs/integrations/notification-upload-v1.md` §5 定义 accepted 为可靠持久接收、客户端不再自动发送；§5 同时定义 rejected 为 blocked 且不计入已确认。实际客户端 AckValidator/UploadStore 也识别这些状态。因此不能把“调用未报错”当作“接收成功”。

| 消费者结果 | Gateway ACK |
|---|---|
| 非 nil、Accepted=true、无 error | accepted |
| 非 nil、Accepted=false、无 error | rejected / INGRESS_NOT_ACCEPTED |
| nil、无 error | retry / INGRESS_RESULT_MISSING |
| 任意结果并带 error | retry / INGRESS_FAILED |

失败使用固定非敏感 code，不返回内部错误或路由规则明文。沿用现有 mockHandle seam 测试实际 HTTP 适配器；未注入 mock 的生产路径仍调用原 InboxIngress。

## 保留的验收边界

本轮证明实际 Gin handler 的分支、元数据与回执映射，以及输入冲突的零消费；消费者由 fake 替代，没有验证数据库提交、所有渠道入口、SageX 原生数组归一化、幂等全链路或消息发送。既有 channel-code fallback、账号选择与整体端到端任务不因这组回归而关闭。
