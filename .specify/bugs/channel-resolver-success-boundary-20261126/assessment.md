# Assessment: 识别码命中后的渠道与租户边界

- 来源：用户二审交叉复核与修复；沿用 CH-20261125-07、LEAD-20261125-03，不新建业务任务。
- 根因：`ResolveByAccountRef` 在识别码命中后直接返回，绕过平台约束及多租户上下文检查；`ResolveByIdentifyCode` 忽略渠道仓储故障/缺失，导致 `ResolveTrusted` 的渠道状态检查也被跳过。`ResolveTenant` 还可能把解析故障吞为已绑定租户回退。
- 风险：已命中的身份仍可能在错误渠道、冲突租户或不可确认的渠道状态下被接受。
- 修复边界：健康未命中继续保留原兼容语义；命中后必须验证账号/渠道标识及租户一致，渠道查询故障传播；账号引用入口要求可信租户，并对所有解析分支执行渠道约束。Webhook 租户解析仅在真正未命中时允许历史兼容。
- 文件 allowlist：
  - `user-server/internal/service/channel_resolver.go`
  - `user-server/internal/service/channel_resolver_boundary_test.go`
- 验证：fake repository 原生产方法回归，含识别码成功、错误平台、渠道故障/缺行/错租户、绑定租户冲突、缺租户、Webhook 不吞故障及正常 legacy miss；复跑既有精确定向闸门/渠道测试。
- 不涉及真实 DB/API、渠道消息、生产部署；不覆盖旧 bug 文档，不修改并行文件。

## 同函数补充边界

名称查询满 50 条时，即使当前页出现一条精确匹配，也不能排除后续页还有同名记录；原满页保护只覆盖“当前页无匹配”。在同一 allowlist 内补充“满页有单条精确匹配仍拒绝宣称唯一”的回归与失败关闭，不扩大查询或引入新消费者。
