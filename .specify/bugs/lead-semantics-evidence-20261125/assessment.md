# Assessment: §3.30 LM-05/06/08/09/10/11/12/16 线索语义与证据补差

- **日期**：2026-11-25
- **来源判据**：`../../docs/未完成任务清单.md` §3.30（工作区根 `hivemtk/docs/未完成任务清单.md` 第 89–238 行），含 20261125 头脑风暴二审修订口径
- **判据性质**：任务清单为静态代码对照 + 待验证反例；本评估只认源码事实与可执行反例，不把清单文字当已复现缺陷
- **Verdict**：**valid**（八项均有当前源码可定位的具体缺口；LM-05 后端可施工部分受文件边界限制只交付纯函数契约，整项保留真机/接线余项）

## 1. 逐项缺口与可判定事实

| 项 | 源码缺口（本轮开工快照） | 施工切片 | 整项可关闭？ |
|---|---|---|---|
| LM-05 | `service/sagex_upload.go:buildMessageEvent` 仅映射 `ev.Message` 单条，`SageXRawNotification.Messages/HistoricMessages`、`ParseStatus`、`TruncatedFields` 原样塞进 `Extra["sagex"]`；SenderType 固定 `customer`。会话键 `sagexConversationID` 用 install+平台+类型+标题，改名/重装会变。 | 新文件提供纯函数：数组 → 规范消息（成员/时间/方向依据/历史-live 标记/源消息 ID/解析与截断标记/覆盖标记），源消息级幂等键；正反例 fixture。 | **否**：`sagex_upload.go` 明确不可改；接 `MessageEvent`/MessageHub、源消息级落库幂等、真机采集能力均需在授权会话另做。 |
| LM-06 | `channelgw.Registry` 已有 MineMode 投影 + `service.LeadMiningChannels()`，但无「每渠道接线 → MessageHub 落库 → 发掘资格 → 独立模块分发」的契约 fixture；旧测试只测注册表四面齐。 | 在允许的新测试文件中按当前注册表生成接线矩阵契约（资格投影、MessageHub 方向/回声、双模块独立闸语义），不要求真实渠道账号。 | **否**：真实渠道账号/TG/飞书/个微/QQ 凭据缺失；公共入站编排 CH-09、模块闸 LEAD-03 属其他会话，本项只交契约 fixture。 |
| LM-08 | `lead_detector_semantic.go` 固定同义词 + `strings.Contains`；`LeadJudgement.LeadRole` 与 `Clue.LeadRole` 仍是单值 `demand/supply/resource/broker/unknown`；没有供需意图、相对上下游、经营身份三轴的证据化表达。 | 在 `lead_detector_semantic.go` 增加纯函数多轴分类（保留多值/未知；否定/引用/转发不误判；同词跨行业保留未知；每轴带消息证据），并把轴结果放进 DetectionHit.Payload；不改 Clue schema/筛选统计。 | **否**：CRM-19 字典、前后端筛选/统计编码是其他任务边界；向量/检索实现需标注集比较后再选。 |
| LM-09 | `Service.Enqueue` 队列满即丢弃；`process` 内 60 秒 `lastJudge` 直接返回；`ProcessBridgeLead` 只做交接；`BridgeLeadWorker` 以 Enqueue 返回即完成，租约过期进 unknown 禁自动重放。 | 在允许的 `lead_mining.go` 片段 + 新文件定义耐久待处理状态机契约（尾触发/最大等待/水位/快照版本/幂等键），保证「仅入队」不等于完成；复用 BridgeLeadTask 思路，不新建第二套消费者。 | **否**：真正改 BridgeLeadTaskRepository/Worker 与消费者所有权超出本轮可修改文件；进程重启对账需 DB 集成与迁移核对。 |
| LM-10 | `persistLead` 用 `platform:sender` / `platform:account:group` 作 `account` 键，`FindByTypeAndAccount` 回查覆盖 summary/evidence/lead_role；同群不同主体/项目/方向会互相覆盖；TOCTOU 回查更新字段更少。 | 在允许的 `persistLead` 去重/分桶部分引入按主体/业务对象/方向的线索键与证据版本追加；迟到结果不覆盖终态；同一群甲乙各买同商品默认两条。 | **否**：商机归属、客户合并/人工重开/终态迁移的 DB schema 与 CRM 任务不在本轮边界；交付为契约 + 纯函数 + 单测。 |
| LM-11 | `lead_discovery.go:Process` 把群发送者一律写 `member`，会覆盖业务角色；`MemberRoles` 是单值 sender→role，群结论可能写成全部成员身份；转述者与买家未分离。 | 在允许的 Process 非闸门部分按证据写成员身份（稳定标识未知则保留 unknown），业务角色由 LM-08 多轴结论写入，普通发言不抹角色；群 `grp:` OneID 不变。 | **否**：SageX 真实成员身份依赖 LM-05 接线与真机；稳定个人客户关联需 LM-04/CRM 合并，保留未知。 |
| LM-12 | `fetchHistoryDB` 产出 `llm.ChatMessage` 丢真实 message_id；`buildLeadJudgeContext` 造 `history_N`；`conversation_context.go` 多处 Get→整体 Upsert，双模块并发丢更新；`saveCompactSummary` 也是整体 Upsert。 | 新文件定义真实消息快照与引用校验纯函数；在允许的文件内把共享上下文的追加改为带去重/版本保护的有界追加，摘要使用版本水位防旧写覆盖；引用必须属于授权快照。 | **否**：完整历史读取链（fetchHistoryDB/buildLeadJudgeContext 不在可修改清单）、权限范围部分禁止改、跨实例事务仍需 DB 集成。 |
| LM-16 | `service/inbox.go:GetMessagesByConversation` 忽略 Hub/Session 查询错误，`ListByConversationContext` 按 sender/receiver 匹配 + 固定 1000 条，群成员消息可能漏；输出 map 无 direction/is_group/group_id/完整性状态；分页是内存切片，无稳定游标。 | 在允许的 `inbox*.go` 契约/完整性范围内补输出字段、来源错误显式状态、稳定游标分页；在允许的 `repository/message_hub_inbox*.go` 增加按真实 conversation/group 聚合的游标查询（不改权限范围）。 | **否**：前端 List.vue 的 isOutbound 推断、UI 展示与真机 1000+ 消息翻阅不在本轮边界；通知缺失需来源能力标记。 |

## 2. 二审口径对齐

- 来源只覆盖部分消息 **不等于** 本次无有效证据：LM-05/16 用 `source_coverage` + `parse/truncated` 状态表达，不据缺失推断对方未说。
- 候选/负例/降级/技术失败分开：LM-08 多轴保留 unknown，LM-09 区分入队/判定/业务交接检查点，LM-10 不把归档当资格转正。
- 不新建第二套消费者/证据库/outbox：LM-09 复用 BridgeLeadTask 思路，LM-12 复用 ConversationContext。
- 不把角色简化为显示名：LM-08 分供需意图/上下游/经营身份；LM-11 群成员身份与业务角色分离。

## 3. 文件边界（施工前锁定）

- 允许修改（本 agent）：`service/lead_detector_semantic.go`、`service/lead_discovery.go`（除 Enabled/Process 前置闸门）、`service/lead_mining.go`（仅 persistLead/去重/分桶相关）、`service/lead_group_identity.go`、`service/inbox*.go`（仅方向/群契约与完整性状态）、`repository/message_hub_inbox*.go`、`service/lead_compaction.go`、`service/conversation_context.go`（仅原子追加，不改权限范围）、对应新 `_test.go`、本 bugs 目录三份文档。
- 禁止触碰：`sagex_upload.go`、`sagex_upload_test.go`、`controller/sagex_upload*.go`（LM-05 只做纯函数）、`lead_react*.go`、`dispatcher_judge.go`、`router/router.go`、`auto_reply.go`、`customer_*.go`、`company.go`、`followup_record.go`、`channel_*.go`、`outbound_*.go`、`webhook_*.go`、任何已存在 `.specify/bugs` 目录、数据库 schema、sagex/Android 仓。
- 在途文件保护：`lead_mining.go`、`lead_discovery.go`、`conversation_context.go`、`lead_compaction.go`、`message_hub_inbox.go` 已有其他会话未提交改动；本轮只做定点追加，不重写整文件、不回退在途修改。
- 工作区约束：不 commit/push/发布/真机点验；测试库按 `POSTGRES_TEST_PASSWORD` 从产品仓 `.env` 取、端口 8217 专用快测库。

## 4. 施工顺序（RED 先行）

1. 先写各项正反例测试（纯函数或真实 PG），记录 RED 失败输出；
2. 按最小切片实现，逐项跑 `gofmt` + `go build ./...` + 限定 `go test`；
3. 在 fix.md/test.md 记录真实命令/结果，未覆盖判据写清余项。
