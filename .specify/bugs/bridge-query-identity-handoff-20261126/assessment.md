# Assessment: query 识别码在鉴权与租户解析之间丢失

- 来源：继续深入生产入口，沿用 CH-20261125-07/09/10、LM-20261125-06，不新增编号。
- 调用链：router 的 bridge 组先执行 `BridgeIngressGuard`，再进入实际 handler；handler 的 `bindBridgeTenant → bridgeIdentifyHintFromRequest` 读取同一个 header/query。`InitGuard` 不解析身份。
- 因果假设：query-only 凭据通过鉴权后，guard 删除 query 中的识别码，却未传给下游；下游无法按已验证的识别码解析身份。header-only 原本正常。
- 文件白名单：`user-server/internal/middleware/bridge_ingress_guard.go`、`user-server/internal/middleware/bridge_ingress_guard_test.go`。
- 先用纯 fake 候选源隔离仓储，执行实际 Gin guard，验证 query-only、header-only、同时存在、无效和缺失凭据。不得调用真实 DB 或打印真实凭据。
- 最小修复方向：已验证的识别码规范化到现有内部请求 header，再清理 URL query；不重新接受未验证 query，不改变 header 优先级或鉴权关闭模式。
- 验收边界：证明实际 guard 的凭据交接与拒绝路径，不代表租户仓储或设备端到端通过；不提交、推送或部署。
