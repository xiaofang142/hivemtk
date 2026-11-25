# Bug Fix: 线索历史、节流与共享摘要按账号隔离

- **Date**: 2026-11-25
- **Assessment**: `assessment.md`（保持原文）
- **Task**: LM-20261125-01

## Root Cause and Changes

历史查询原接口无法表达来源账号边界；节流未含账号，群聊又按发送者拆窗。现在仓储必须显式收到 tenant/platform/account 引用集合及单群类型，拒绝缺失/系统作用域，按 `sent_at DESC, id DESC` 稳定排序。私聊读取双方；无会话 sender 兜底仅匹配无群无会话的双向消息；群读取该账号该群所有已采集成员。

`lead_history_scope.go` 复用 ChannelResolver/ChannelAccount 校验真实租户、渠道和账号。数值 ID、名称、识别码仅在逐项自证唯一指向同一账号后兼容历史行；识别码不是 ACL。已知跨租户/错平台/歧义拒绝；未知非空账号明确降级到同租户同平台的原始账号精确匹配，禁止加载共享摘要。单次分析冻结范围，节流使用 tenant/platform/规范账号/单群/会话（群优先 group_id）。

## Necessary Compatibility Work

最初只给发掘侧摘要加作用域，会使自动回复、线索发现和管理 Snapshot 仍读旧 key，导致共享上下文断开。因此同步迁移 `conversation_context_scope.go`、共享 Service API、auto_reply/lead_discovery/inbox_ingress 和 Controller 的可选平台/单群参数。key 使用平台、单群、真实会话的 SHA-256，保持在现有 varchar(128) 内；账号和租户仍由独立列隔离。旧调用仅在 MessageHub 元数据可唯一证明会话归属时兼容，不回读可能已混合的无作用域旧摘要。

这不是 LEAD-04 的模型提示词接线，也未实现 LM-12 的真实消息 ID/原子更新；不更改员工授权（CRM-04）、客户跨账号归一（LM-04）或耐久尾触发（LM-09）。

## Tests / Fixture Corrections

- 新增真实 PG 仓储、账号别名/节流/历史范围和双模块共享读取回归；迁移受签名和可信账号校验影响的旧测试。
- 原集成测试使用不可自证的账号；改为真实 Channel/ChannelAccount fixture，测试中关闭云端厂商，避免外部 LLM 调用。
- 主智能体验收发现 `Integration_NotLead` 的国内渠道 fixture 被开源版 edition gate 拒绝，属于夹具而非生产隔离缺陷；改用允许的 Telegram，并增加 Judge 确实被调用一次的断言，防止空跑假绿。
- 在 HEAD `2c5c53e3ded742ac0c6ebb1944529aaed18973fd` 的临时归档副本重新运行原症状：同租户 A/B 账号相同 conversation 查询返回两账号，测试按预期失败。此证据是接手后的重新复现，不冒称原子代理完整 TDD 顺序已获证明。

## Verification Boundary

完成判定以 `test.md` 为准。当前生产 build、层级、架构、GORM 和 diff 检查通过；完整服务包的最新测试/vet 被另一会话 `lead_react_test.go` 未同步字段阻断，不能宣称全树通过或据此直接核销。全量 internal 的旧失败与当前失败已分别保留原始 JSONL，未改动其他会话在途源码。

## Execution / Deployment

前半由唯一串行子代理施工；子代理会话存储连续故障后，用户明确授权主智能体直接接手。本轮未修改 OpenCode 配置，未提交、推送、部署或真实外发。保留 ReAct、Sagex、CRM 和前端等原有他人差异。
