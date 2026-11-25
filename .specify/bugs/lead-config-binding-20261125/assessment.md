# Assessment: 20261125 LEAD-01 / LEAD-02 / LEAD-04 / LM-07 配置与绑定缺口

- **Slug**: lead-config-binding-20261125
- **判据来源**: `../../../../docs/未完成任务清单.md` §3.29（LEAD-20261125-01/02/04）、§3.30（LM-20261125-07）

## Valid 判定

四项判据均 valid，代码现状与判据描述的关键缺口吻合：

- **LEAD-01**：`channel_account.go:180–252` 只把 `DataScope` 写成 JSON 字符串；真实读取在 `customer_permission.go:89` 走 `ChannelAccountAuthorizationRepository.ListByUser`（092 表），该仓储只有读方法，没有面向账号页的选择/保存/回读/撤销配置口。`menu.go:87` 的 leadGovernance 仍限 admin。
- **LEAD-02**：`auto_reply.go:58–66` 仅看 `AutoReplyAgentID > 0`；`webhook_ai.go:209–217` 仍从旧 `ChannelAgentBinding` 读 `AgentContext`。账号侧绑定没有校验 Agent 类型/租户/状态，也没有旧绑定预览→迁移/转向的收口。
- **LEAD-04**：`inbox_ingress.go:833–848` 与 `lead_discovery.go:63–67` 读对方备注只 `logger.Infof`；`lead_judge_context.go:165–232` 只消费 summary/role/hits，未消费 tags/notes；回复触发参数 `TriggerInboundMeta` 也没有共享业务内容字段。
- **LM-07**：`lead_rule.go` 有 account>channel>global 继承，`lead_mining_agent.go` 有 CRUD，但 `lead_discovery.go:Enabled` 只看绑定 ID；`lead_mining.go:395–465` 的 gate/判定/持久化没有一份冻结的规则+Agent+模型版本快照；legacy 单例配置仍与三粒度规则并行取阈值。

## 边界与可行性

- 本轮可改文件严格限定：`channel_account.go`（授权配置 + 回复 Agent 绑定）、`inbox_ingress.go`（共享备注/标签输入）、`lead_judge_context.go`（备注/标签消费）、`lead_rule*.go`、`lead_mining_agent*.go`、新增对应 `_test.go`、新建本 bug 目录三份 md。
- 不改数据库 schema；不改仓库层文件、Controller、Router、`webhook_*.go`、`auto_reply.go`、`lead_discovery.go`、`lead_mining.go` 的 process/persistLead 主体、`conversation_context*.go`。
- 因此：
  - LEAD-01 能闭环「配置保存/回读/撤销 + 权限解析消费 + 两员工两账号负例」，但管理页 UI、Controller 路由与菜单放开不在边界内，记录为余项。
  - LEAD-02 能闭环「账号侧绑定校验 + 真实 AgentContext 解析（模型/提示词可观察）+ 旧绑定预览/收口规则」，但把 resolver 接进 `webhook_ai.go` 的调用点不在边界内，记录为余项。
  - LEAD-04 能闭环「双方真实模型请求输入（回复侧 TriggerInboundMeta、线索侧 system prompt）」的适配与捕获，但把参数落到 `webhook_ai.go` 的 LLM 请求、把线索侧快照接到 `lead_mining.go` process 不在边界内，记录为余项。
  - LM-07 能闭环「有效配置统一解析 + 一次分析冻结快照 + 版本混用检测」的可注入纯函数与测试，但 gate/三路/ReAct/持久化共用同一快照的接线不在边界内，记录为余项。
- 测试策略：优先标准库纯函数 + 仓储接口替身（生产构造器仍用真实仓储），DB 集成用 `testutil.NewTestDB`，`POSTGRES_TEST_*` 从产品仓 `.env` 取、端口 8217、`-p 1 -parallel 1` 单包串行；主 agent 全量回归窗口内先不跑 DB 测试。
