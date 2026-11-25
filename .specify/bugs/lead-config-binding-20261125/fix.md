# Fix: 20261125 LEAD-01 / LEAD-02 / LEAD-04 / LM-07

## LEAD-01 账号授权管理接入员工实际数据范围

- `user-server/internal/service/channel_account.go`
  - 新增 `AccountAuthorizationService`（选择/保存/回读/撤销）+ 092 表仓储实现（复用 `BaseRepository`，不改 RBAC/表结构）。
  - 校验：用户/账号必须属当前租户、空授权拒绝、角色白名单 viewer/owner；保存覆盖式逐条写入 `AccountAuthorizationPair`，不合并账号×行业/角色。
  - 撤销即时生效：`CustomerPermissionService.Resolve` 每次读取授权表，撤销后 `RestrictedAccounts` 空集合拒绝全部；两员工两账号按配对隔离，跨账号行业不交叉放行。
- 边界余项：Controller 路由、`views/channels/account-manage.vue` 与 `menu.go leadGovernance` 放开不在本边界，需主 agent 接线后再做 UI 真机验收。

## LEAD-02 回复 Agent 绑定决定真实执行并收口旧绑定

- `user-server/internal/service/channel_account.go`
  - 新增 `ReplyAgentBindingService.Resolve`：以账号侧 `AutoReplyAgentID` 为唯一配置源，读取真实 Agent 并校验存在/启用/租户/类型（sales/customer_service/hybrid），错类型/失效/跨租户拒绝。
  - `ReplyAgentExecutionContext` 捕获真实 Agent ID/模型/系统提示词，换绑可观察变化；`PreviewLegacyReplyBindings` 先预览启用主绑定的迁移映射，标出与账号侧绑定的冲突或转向，`LegacyReplyBindingNotice` 明确旧入口只转向不产生第二套权威。
- 边界余项：`WebhookService.loadAgentForChannel`（webhook_ai.go）接本解析器不在授权文件内，实际 LLM 请求使用新配置需主 agent 接线；intent/NLP、情绪、拟人、转人工链未在本轮真实验证。

## LEAD-04 共享标签和业务备注进入双方实际推理输入

- `user-server/internal/service/lead_judge_context.go`
  - 新增 `BusinessNote`（source/at/fact|suggestion/text）编码/解码与存量兼容；`SharedBusinessInputFromContext` 按 ownNamespace 只读对方备注、保留纯业务标签/摘要、按长度预算裁剪；`PromptBlock` 明确建议非已发生事实。
  - `LeadJudgeContext.SharedBusiness` 与 `buildLeadJudgeSystemPrompt` 消费，线索侧真实模型请求包含对方刚写信息。
- `user-server/internal/service/inbox_ingress.go`
  - 新增 `SetSharedBusinessProvider` 与 `sharedBusinessForEvent`，把共享业务信息加入 `TriggerInboundAI` 的 aiContent（回复侧真实模型输入）；未注入时保持既有行为。
- 边界余项：提供方装配（router）、`lead_mining.go` process 中共享快照接线不在边界；关闭一模块后另一模块读取、跨账号拒绝、群聊多轮纠正的真机链路未覆盖。

## LM-07 发掘 Agent、规则和表单配置进入真实执行

- `user-server/internal/service/lead_rule.go`
  - 新增 `LeadMiningConfigSnapshot` + `LeadMiningConfigResolver`：统一三粒度规则与 legacy 单例配置（阈值/模型/表单要求），规则 account>channel>global 继承且禁用继续向上，全部关闭 fail-closed；绑定 Agent 的启用/类型/模型/提示词进入快照。
  - 每次解析生成版本指纹（包含规则/Agent/模型/提示词等全部执行字段）；`Matches`/`Consume` 供 gate/detection/judge/persistence 共用同一冻结版本，切换后不混用。
- `user-server/internal/service/lead_mining_agent.go` 未改生产逻辑，沿用现有独立 CRUD 作为线索 Agent 配置源。
- 边界余项：`lead_mining.go` process/persistLead 主体不在边界，快照尚未真正接入 gate/三路/ReAct/判定/持久化调用点；排队期配置切换的失效重排策略未实现。

## 通用

- 所有新增测试为标准库 + 可注入替身；未改数据库 schema，未改 Router/Controller/`auto_reply.go`/`webhook_*.go`/`lead_mining.go` process/persistLead 主体/任何已存在 bugs 目录。
