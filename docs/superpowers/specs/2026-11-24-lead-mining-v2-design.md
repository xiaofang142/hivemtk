# 线索发掘模块 V2 设计（反复头脑风暴 + 对抗审核 + 13 项决策）

**日期**：2026-11-24
**状态**：设计稿（待批准；批准后才进入实施计划）
**输入**：用户「完整线索发掘模块」六组需求 + 四轮 grill 锁定的 13 项决策
**配套调研**（三份均已完成）：
- `docs/research/LEAD-MINING-INDUSTRY-METHODS-2026-11.md`（行业一手调研）
- `docs/research/LEAD-MINING-ADVERSARIAL-AUDIT-2026-11.md`（对抗审核，结论 NO-GO）
- `docs/research/LEAD-MINING-DESIGN-DECISIONS-2026-11.md`（竞品与设计决策矩阵）

**版本**：v1.2（builder check 三轮复审 GO；已修正事实错误并补齐全部 18 项决策的落地路径与验收）

**决策总数**：18 项（D1–D13 四轮 grill + D14–D18 builder review 后追加）

## 1. 意图与动机

前一轮定制任务已把线索发掘从「关键词 + 单次 LLM 判定」升级成完整链路：上报→三路检测→二次判定→归档→通知→客户360→收件箱。但经代码核验与行业对照，仍有三处**结构性缺陷**：

1. **渠道覆盖口径不对**：MessageHub 平台表实际有 **31** 个平台（`message_hub.go:40-92`，含个微 `personal_wx`/QQ `qq`），但 Bridge 线索资格白名单只放行 5 个电商渠道（`douyin_lead_miner.go:208-212`、`bridge_lead_eligibility.go:11`），且非 Bridge 渠道全部走 `MineUnifiedLead` 直写 hub、不经 `Service.Enqueue`。用户明确要求「聚合消息有多少渠道，线索发掘就要有多少渠道」。
2. **群聊归属未定义 + 上下文假聚合**：上一轮我宣称"群聊富化已接线"是错的——`lead_judge_context.go:211-218` 用只含当前一条消息的共享缓存覆盖了 `fetchHistoryDB` 的真实多成员/双向历史，我加的富化又基于错误缓存再覆盖。此外 `persistLead` 仍按发送人建客户（`lead_mining.go:312,504`），与「群=客户」冲突。两处都必须在 P0 修复。
3. **平行客户身份**：发掘链路用 `lm:platform:sender` 旧键 + `GenerateCustomerUnifiedID` 建客户（`lead_mining.go:667-708`），而 OneID 身份服务已在 GEO 口（`geo_routes.go:412-424`）与手动归档口（`clue_archive.go:83`）接线，唯独自动发掘链路没接，客户360 会出现重复档案。

## 2. 目标 / 非目标 / 反目标

**目标**：
- 所有 MessageHub 已聚合渠道都具备线索发掘资格（个微、QQ、企微、飞书、TG 等）。
- 群聊商机以「群=客户」归档，群内各成员角色作为明细和证据保留，统计可聚合。
- 新线索客户统一走 OneID/CustomerIdentity；历史 lm:* 不动、靠兼容读消费。
- 高分线索自动入库、低分线索进 `status=candidate` 待审队列，证据不完整一律待审。
- 删除 Bridge 专用 miner 与通用 miner 双轨，所有渠道入口统一经 `Service.Enqueue`。
- 通知沿用「账号维护人→渠道维护人→租户默认」三级路由，外发脱敏 + 业务码判成败 + 送达记录。

**非目标**：
- 不改原 RBAC 权限模块；不调整已有客户去重算法本身。
- 不新增 `deal_mode` / `side` 字段（用户第 4 轮决策：沿用单一 `lead_role`）。
- 不自动生成跟进记录（只生成建议 + 摘要，记录由人工写）。
- 不一次性迁移历史 lm:* 客户档案。

**反目标（出现即失败）**：
- 发掘失败影响消息落库或自动回复；通知失败回滚线索。
- 跨租户/跨账号越权看到线索或通知他人。
- 把未发生的跟进动作写成「已跟进」记录。
- 引入第二套客户主数据体系。

## 3. 约束

- 五层架构 Router→Controller→Service→Repository→Model；Service 不持 `*gorm.DB`；SQL 只走 GORM API。
- 响应 `{"code":0,...}`；错误码两条通道不混用；平台调用按业务码判成败。
- 租户 fail-closed；配置/判定/线索/通知路由全带 tenant_id。
- i18n 双语 + 新手引导（新增页面时）；UI 点验逐元素显式操作。
- LLM 判定严格 JSON Schema + `evidence_refs` 机械校验；判据是 `jd.IntentScore` 与 `cfg.MinIntentScore`，`jd.Confidence` 只用于商机转换——不要把意向分误称置信度（builder review F5 纠错）。
- 用户原始需求中的「ReAct」按 D18 在 V2 内实现为工具式取证循环，必须有工具白名单、步数上限、失败可见、关键词/语义 fallback。

## 4. 决策矩阵（13 项，全部用户已确认）

| # | 争议点 | 决策 | 理由 / 来源 |
|---|---|---|---|
| D1 | 渠道覆盖口径 | 以 MessageHub 全平台为准 | 用户决策；`message_hub.go:40-92` 实际 31 平台 |
| D2 | 群聊线索归属 | 按群本身（群=客户） | 用户决策；优先于行业通用「按 sender+role 拆多条」建议（行业报告 §4.3） |
| D3 | 客户身份归一 | 接入 OneID/CustomerIdentity | 用户决策；GEO 口已有 `IdentifyOrCreate`（`geo_routes.go:424`） |
| D4 | 通知路由粒度 | 账号→渠道→租户（已实现） | 用户决策；`lead_notify.go:209+` |
| D5 | 群身份键 | 渠道账号 + 群ID | 用户决策；群名变化不影响、跨账号同名群不串 |
| D6 | 历史 lm:* 处理 | 不动历史，新线索走 OneID | 用户决策；零迁移风险，残留靠现有去重页人工处理 |
| D7 | Bridge 专用 miner | 彻底删除 | 用户决策；避免双轨与白名单漂移 |
| D8 | 跟进内容生成 | LLM 生成建议+摘要，记录人工写 | 用户决策；不造假审计记录 |
| D9 | 群内角色记录 | 群主角色 + 成员角色明细 | 用户决策；统计双维度可聚合 |
| D10 | 非 Bridge 入口 | 全部改走 main Service.Enqueue | 用户决策；统一单路径 |
| D11 | 置信度入库 | 高分自动入库 + 低分 candidate 待审 | 用户决策；行业报告 §3.4 置信度=路由信号 |
| D12 | 证据引用校验失败 | 标证据不完整 + 进待审 | 用户决策；与 `degraded` 语义一致 |
| D13 | 本轮交付边界 | 先出设计+决策文档，再分阶段实现 | 用户决策；本文即交付物 |
| D14 | candidate 转正触发 | 后续高分判定自动转正并通知 | builder review Q1 用户决策；修 `persistLead` 更新分支写 status |
| D15 | 无身份列渠道的 OneID | 加迁移新列（qq/personal_wx/tg/feishu/wecom） | builder review Q2 用户决策；`identity.Identifiers` 现仅 5 字段 |
| D16 | 群客户形态 | customers.name=群名 + 禁止与个人合并 | builder review Q3 用户决策；身份键 `grp:...` 隔离 |
| D17 | 群主角色来源 | 本轮降级为只记成员角色 | builder review Q4 用户决策；成员角色由 LLM 判定写入，群主留空 |
| D18 | ReAct 范围 | **V2 就做 ReAct 工具调用** | builder review Q5 用户主动升级范围（非推荐项）；需工具白名单/步数上限/失败可见/fallback |

## 5. 架构与数据流（目标态）

```
各渠道上报（Bridge / TG bot / Feishu / QQ channelbot / SageX / webhook）
  → InboxIngress 归一落 MessageHub（唯一事实源，平台以 messageHubPlatforms 为准）
  → Service.Enqueue(hub)（唯一发掘入口，D1/D10）
      → 防抖/幂等（message_id/hash 幂等键，替代纯内存时间窗）
      → 上下文聚合：单聊双向 / 群聊多成员（已有，D2/D9）
      → 三路检测 + 二次 LLM（严格 JSON + evidence_refs 校验，D11/D12）
      → 身份解析：
          单聊 → CustomerIdentity.IdentifyOrCreate（D3/D6）
          群聊 → OneID(tenant+channel+account+group_id)（D2/D5）
      → 归档 Clue：
          score >= min_intent_score → status=new（D11）
          score < 阈值但可判定 → status=candidate（D11/D12）
      → 通知路由：账号→渠道→租户（D4），TG/飞书外发脱敏 + 业务码判成败
      → 跟进：followup_suggestion + 历史摘要由 LLM 生成（D8）
          followup_records 只由人工写入
  → 统一收件箱：单聊按聊天页展示双向消息；群聊按群展示多成员消息（区分渠道/账号）
  → 线索列表/统计：按 lead_role（demand/supply/resource/broker）、渠道/账号/单群聊聚合
```

## 6. 组件级改动设计（待实施计划拆任务）

> 本节只定「改哪里、改成什么」，不含逐行代码；实施计划在批准本设计后按 writing-plans 生成。

### 6.1 渠道资格（D1/D7/D10）

- **唯一真值源 = `channelgw.Default` 注册表**（设计决策调研 §4.2）：给 `ChannelSpec` 增加 `MineMode: none|bridge|direct` 能力位，在 `init()` 里用 `Default.LeadMiningChannels()` 一次性投影到 service/bridge（仿既有 `SetBridgeOutboxChannels`，`registry.go:196-201`）。`service.ValidPlatform` 作为 MessageHub 落库侧的兜底校验保留，不作为发掘资格源。
- 删除 `douyin_lead_miner.go` 与 `bridge/handler_http.go` 两处硬编码 `leadMiningChannels` map；`unsupportedLeadMiningChannels` 降级为**解释性原因表**（仅 API 提示，不做准入）；删除 `RegisterLeadMiningChannel`/`isBridgeLeadMiningChannel` 与 Bridge 专用 miner 封装。
- `channelEnabled`（`lead_mining.go:770-780`）语义改为「默认全纳入、显式关闭」：未配置或空名单 = 所有 `MineMode != none` 渠道参与；租户配置只表达显式关闭某渠道。消除「名单为空=全开」与「名单非空=漏一个就静默关」两种语义并存的风险。
- `LeadAdapterForPlatform` switch（`lead_miner_adapters.go:314-350`）改为注册表工厂 `map[channel]func() ChannelLeadAdapter` + `RegisterLeadAdapter`（重复 panic），default 落 `GenericLeadAdapter` 并记 warn；消除 switch 与新渠道的耦合。
- parity 门扩写（`registry_parity_test.go`）：`Default.Names()` 中每个 `MineMode != none` 渠道必须 ① channelcaps.Known ② 有 LeadAdapter 工厂 ③ bridge 侧投影集合相等 ④ 扩展侧有对应 js 适配器（Experimental 可例外但必须登记）。任何一项缺即红——把「注册了但发掘静默跳过」变成红灯。
- 飞书/TG/channelbot/QQ 等所有入口删除 `MineUnifiedLead` 调用，改为落 MessageHub 后由主 `Service.Enqueue` 统一处理。删除 `lead_miner_unified.go` 及相关渠道专用 adapter 的线索分支（D7/D10）。
- **关键接线（builder review 施工障碍）**：现状只有 3 处 `Enqueue`（`inbox_ingress_persist.go:160`、`chat_visitor.go:514`、`lead_discovery.go:74`）；飞书（`webhook_channel_feishu.go:255`）、企微（`webhook_channel_wecom.go:400`）、TG（`telegram_lead_miner.go:166`）、WhatsApp（`webhook_channel_whatsapp.go:111`）、抖音（`webhook_channel_douyin.go:385,442`）都直接调 `MineUnifiedLead`，其落 hub 函数（如 `webhook_channel_feishu.go:244`）内无 Enqueue。**不能只删调用**：必须先在每条入站落 hub 后注入 `Service.Enqueue`（或在统一 `InboxIngressService` 出口注入），再用表驱动集成测试逐一驱动各渠道 handler 投递 inbound、断言 `lead_mining_judgements` 新增 1 行，全部通过后才删 miner。
- `channelEnabled` 的 legacy `cfg.Channels` 非空白名单仍会挡住 qq/personal_wx（P0-1），必须与配置合并一起改，否则 P0 验收失败。
- 过渡期判定：grep 确认 `MineUnifiedLead`/`DetectUnifiedIntent`/`isBridgeLeadMiningChannel` 生产调用点归零；各渠道 handler→Enqueue 集成测试全绿；parity 门全绿。

### 6.2 群=客户身份（D2/D5/D9）

- `resolveCustomer` 增加群聊分支：`hub.IsGroup && GroupID != ""` 时，身份键 = `tenant + platform + account_id + group_id`，走 `CustomerIdentity.IdentifyOrCreate`，客户档案 name 用群名（`group_name`）。
- 单聊分支走 OneID 的 `IdentifyOrCreate`（按 platform+sender 的规范化 ID）；`lm:*` 仅作兼容读键（发现旧 lm:* 客户时返回它，但不再新建）。
- 群成员角色：保留 `ConversationContext.MemberRoles` 与 `LeadMiningHit.Payload` 作为成员角色明细；`Clue.EvidenceJSON` 写入 `[角色:发送者]` 富化消息；主 `lead_role` 取判定结论的单值。
- 统计：`GetLeadMiningStats` 的 `by_lead_role` 与 `by_conversation` 已支持；按群的**主角色**（判定结论单值）参与角色统计，成员角色明细供查看详情（D17 已降级：本轮不记群主角色来源）。

### 6.3 待审队列与证据校验（D11/D12）

- `persistLead` 增加分档：按 `jd.IntentScore` 与 `cfg.MinIntentScore` 比较（不是 `Confidence`）；低于阈值但可判定且命中证据非空 → 写 `clues` 且 `status=candidate`。
- **群线索键（builder review 卡点）**：`account` 变量现为 `platform:senderID`（`lead_mining.go:312`），群聊改为 `platform:accountID:groupID` 作用域键，与"群=客户"一致；`GroupID`/`GroupName` 用 `Clue` 现成列。
- **自动转正（D14）**：同一线索再次判定达阈值时，`persistLead` 更新分支必须写 `status`（当前分支 `:561-577` 不写 status，candidate 永远转不了正），由 `clue_lead_mining_actions.go:14-32` 的状态机驱动 new/qualified 并触发通知。
- `buildLeadJudgeSystemPrompt` 的 JSON 输出增加 `evidence_message_ids` 必填；判定后机械校验每个 id 存在于本轮消息集——必须先把历史消息 ID 从伪造的 `history_N`（`lead_judge_context.go:184`）改为真实 `MessageHub.MsgID`，否则校验恒真无意义。失败置 `evidence_complete=false` 并强制 `status=candidate`。
- 前端线索列表加「待审」筛选（复用 `status=candidate`），详情展示证据引用与不完整标记；管理员可确认/忽略。

### 6.4 跟进内容（D8）

- 判定输出保留 `followup_suggestion` 与 `summary`（`Clue` 已有字段），落库时写入 Clue——这是单条线索的摘要。
- **历史摘要（reviewer 指出的遗漏）**：用 `ConversationContext.Summary`（已有字段，`conversation_context.go:108-116` 有 setter 但零生产调用）承载「该会话/群的历史总结摘要」，在判定后由 LLM compaction 生成并写入；P0-b 修复上下文覆盖时一并接线，P3 客户360 时间线与线索详情可展示该摘要。验收项：3 轮对话后 `ConversationContext.Summary` 非空且含客户诉求/角色/未决问题。
- 客户360 时间线增加 `lead_mining_hit` 事件（引用来源消息与证据），供维护人写跟进记录时参考。
- 不新增自动生成 `followup_records` 的逻辑（D8：记录人工写）。

### 6.5 通知（D4，基本保持）

- 三级路由已实现，不改路由逻辑；但 `notifyLeadDiscovery`（`lead_mining.go:652-676`）现同时调 `NotifyOwner` 与全局 `NotifyBusiness`（UserID=0 广播），且只在 create 分支调用（`:602`）。**收敛为单一 `NotifyOwner` 出口**，去掉挖掘路径的全局广播；所有新入口（单聊/群聊/待审自动转正）都调用它。
- 群聊通知文案增加群名（复用 `Clue.GroupName`）+ 成员角色摘要；外发仍脱敏（手机号/邮箱掩码），按飞书 code/TG ok 判成败；通知 target 加租户越权校验。
- 待审线索不触发外部通知（在 `notifyLeadDiscovery` 入口抑制，避免骚扰），create 与 candidate→new 转正时才通知。
- 验收：归档 1 条线索统计外发次数期望 1（当前 ≥2）；candidate 创建时外部通知 0。

### 6.6 迁移与兼容

- 新线索群身份键复用 `customers.unified_id` 生成规则增加一种「群主档」类型，格式 `grp:<tenant>:<platform>:<account>:<group_id>`，`customers.name` = 群名（D16），禁止 OneID 规则把群档与个人档合并（身份键前缀隔离 + 规则排除）。**不新增表、不重复补 `CustomerChannel` 身份锚**——设计决策调研实证 `customer_channel.go:22-35` + `migrations/065_customer_channel_identity_keys.sql` 已落 `(tenant_id, one_id, channel, account_id)` 业务键与 `(tenant_id, channel, account_id, channel_user_id)` 身份锚，旧调研说「缺 account_id」已过时。
- **新身份列迁移（D15）**：`identity.Identifiers`（`identity/normalize.go:34-40`）现仅 Phone/Email/WechatOpenID/DouyinOpenID/XiaohongshuID 5 字段，装不下 qq/personal_wx/tg/feishu/wecom 的 sender，`GenerateCustomerUnifiedID` 对这些渠道回退随机 UUID（`model/customer.go:120-167`）。需加迁移新列并扩 `Identifiers` 与归一规则，验收「同 sender 两次消息 unified_id 相同」。
- `Clue` 已有 `lead_role`、`status`、`evidence_json`、`summary`、`followup_suggestion`、`is_group`、`group_id`、`group_name`、`source_channel`、`source_account_id`（`model/clue.go:26-52`），无需迁移。
- 待审队列复用 `status=candidate`，无迁移。
- 会话寻址沿用 `MessageHub` 的 `is_group/group_id/conversation_id` 骨架，收敛到 `(channel_account, channel_user_id)` 身份锚（复用迁移065），不照搬 Chatwoot 的 per-channel contact_inbox 表（设计决策调研 D1）。

### 6.7 ReAct 工具式取证（D18，用户主动升级范围）

用户原始需求明确要求 ReAct 分析消息；builder review Q5 用户选择「V2 就做 ReAct 工具调用」（非推荐项）。设计要点：

- **工具白名单**（只读、fail-visible）：`query_customer_360`（按身份键查客户档案）、`query_historical_leads`（同 sender/conversation 的历史线索）、`query_dictionary`（行业词库/敏感词/上下游信号）。禁止任何写工具、禁止外发工具。
- **步数上限**：最多 3 轮 Thought/Action/Observation；超限强制用已有三路检测证据出结论并标记 `degraded=true`，不无限循环。
- **失败可见**：工具调用超时/拒绝/返回空，写入 `LeadMiningJudgement.result_json` 并在提示标注证据不完整，不静默吞。
- **fallback**：ReAct 失败或不可用时回退到现有「三路检测 + 二次 LLM 判定」主链（已实现），保证发掘不中断。
- **复用基础设施**：`internal/aiagent/llm/react_adapter.go` 已有 ReAct 解析与 `IsReActMode`；判定走 `dispatcher.DispatchStructured` 同一路径，不新建 LLM 客户端。
- **验收**：构造一条需要查历史线索才能判定的指代型消息（如「这个能不能做」），断言 ReAct 循环调用了 `query_historical_leads` 且结论引用了工具返回的证据；再构造工具超时场景，断言回退主链且 `degraded=true`。

## 7. 实施切片（批准后按此顺序拆计划）

| 切片 | 内容 | 独立验收 | 依赖 |
|---|---|---|---|
| P0-a | 合并配置源：删 legacy 判据，三粒度规则为唯一开关/渠道/阈值来源；表单字段补齐并同事务保存（对抗审核 P0-1） | 关 legacy enabled、开账号级规则仍能产出线索；表单字段与规则表双向一致 | 无 |
| P0-b | 修复群聊/单聊上下文覆盖：以 MessageHub history 为权威，共享缓存只按 MsgID 去重合并禁止覆盖；ingress `AppendMessage` 补真实历史；真库测试断言多成员原文（对抗审核 P0-2） | 3 成员群聊夹具断言 jctx.Messages 含 3 个不同 SenderID 与原文 | 无 |
| P0-c | 渠道资格改 channelgw.Default MineMode + parity 门 + 删除 Bridge/通用 miner + 全入口改 Enqueue（D1/D7/D10，对抗审核 P1-1） | 个微/QQ/飞书/TG 消息均能触发主引擎发掘；旧 miner 生产调用点 0 | P0-a（channelEnabled 语义改造必须与配置合并同改） |
| P0-d | compaction / 历史摘要接线（D8）：ConversationContext.Summary 接入 buildLeadJudgeContext | 3 轮对话后 Summary 非空且含诉求/角色/未决问题 | P0-b |
| P1 | 身份新列迁移（D15）：qq/personal_wx/tg/feishu/wecom 身份列 + 归一规则（原 P6 提前，P1 前置） | 同 sender 两次消息 unified_id 相同；跨渠道不串 | P0-b |
| P2 | 群=客户身份 + OneID 接入（D2/D3/D5/D6/D9/D16，对抗审核 P1-3）：群键 grp:...、群名入 customers.name、禁止与个人合并、单聊走 OneID、并发 CAS | 群消息归档为一条群客户线索；跨账号同名群不串、群档不被 OneID 与个人合并；单聊走 OneID 且同 sender 稳定；不新建 lm:*；并发 CAS 丢消息率 0 | P1（身份新列） |
| P3 | 待审队列 + 证据引用机械校验 + evidence_id 真实性（D11/D12，对抗审核 P1-2） | 低分/证据不完整线索 status=candidate 且不外发通知；证据引用均为真实 MsgID | P0-b |
| P4 | 前端待审筛选 + 群聊详情 + 统计角色聚合（D9）+ 历史摘要展示（D8） | 列表/统计按角色、群/单聊、渠道账号展示；线索详情/客户360 展示 ConversationContext.Summary 历史摘要；逐元素点验 | P3/P0-d |
| P5 | 通知单出口收敛 + 文案增强 + 跟进摘要/时间线（D4/D8，对抗审核 P1-4/5） | 通知单出口、三级路由、群名与角色摘要、脱敏；客户360 时间线有发掘事件；无自动跟进记录 | P2 |
| P6 | ReAct 工具式取证（D18）：工具白名单/步数上限/失败可见/fallback | 指代型消息触发 query_historical_leads 且结论引用工具证据；工具超时回退主链且 degraded=true | P0-d/P2 |
| P7 | 台账诚实性 + API 三端验收（对抗审核 P1-6） | not-run 项回退未完成；api_verify_full.py 覆盖新增 API 并全过 | P0~P6 |

## 8. 验收标准

- 个微（`personal_wx`）与 QQ（`qq`）上报消息能触发发掘并落线索；MessageHub 31 平台全部具备资格。
- 群聊商机归档为一条群客户线索，成员角色明细与证据引用可追溯；统计按主角色聚合。
- 群跨账号不串：两个账号下同名群产生两条独立群客户（`grp:<tenant>:<platform>:<account>:<group_id>` 键不同）；群档不被 OneID 规则与个人客户合并（D5/D16 验收）。
- 新线索客户 `unified_id` 非 `lm:*` 前缀；旧 lm:* 档案只读兼容、不新增；同 sender（含 qq/个微/tg/飞书/wecom）两次消息 unified_id 稳定相同（D15）。
- 低分与证据不完整线索进 `status=candidate`，不触发外部通知；后续高分判定自动转正并通知（D14），人工也可确认或忽略（两条路径都验）。
- `MineUnifiedLead`/`isBridgeLeadMiningChannel` 生产调用点 0；无双轨发掘。
- 通知按账号→渠道→租户路由，外发脱敏、业务码判成败、送达记录完整。
- `go build`/`go vet`/架构门禁/受影响包测试全绿；新增违规 0；UI 逐元素点验留证据。

## 9. 开放风险与推翻条件

- **删除 miner 的影响面**：需 grep 确认飞书/TG/channelbot 的线索入口全部能改走 Enqueue；若某入口无 MessageHub 落库路径，则需先补落库再删 miner（P0 前置检查）。
- **群身份键与 OneID 的冲突**：若现有 OneID 归一规则对群场景有不同假设，需要在 P2 用测试坐实再改，不臆测。
- **待审队列噪音**：若 candidate 量过大，需上调「关注线」阈值或加频控；这是运营参数，不写死。
- **行业建议冲突**：行业报告建议按 `(conversation, sender, role)` 拆多条线索，用户选「群=客户」——本设计以用户决策为准；若后续运营发现群内多角色无法独立跟进，推翻 D2 重开设计。
- **并发丢消息（设计决策调研 §3.2 实证）**：共享上下文 `Messages` 每次 Upsert 全量覆盖且无版本号/CAS（`conversation_context.go:60-62` + `repository/conversation_context.go:64-80`），4 worker 下同会话可能后写覆盖前写。本设计 P2 必须先加版本号 CAS 或改 append 语义，否则群角色明细会丢。
- **摘要压缩未接线（同上）**：`ConversationContext.Summary` 有 setter（`conversation_context.go:108-116`）但 `buildLeadJudgeContext` 零引用。超出 20 条窗口时做 compaction（旧窗口→结构化摘要 + 最近 K 条 + 当前消息），不做大窗口；此项已挂 P0-d 切片，并为 P6 ReAct 的前置（降低工具调用 token 成本）。

## 11. 后续阶段（本设计不做，记录备查）

- 判定状态机字段 `lead_state`（candidate/confirmed/rejected/expired）与 `evidence_refs` 进 `LeadMiningJudgement`，驱动同一线索跨多轮状态而非重复生成。
- 出站 sender 注册表（`outbound_registry.go`）保持插件化，防回归 parity 门。
- 群主角色由渠道上报补 `owner_id/is_owner`（D17 本轮降级，待渠道协议演进后恢复）。

## 10. 对抗审核发现（三路调研已全部回收，NO-GO）

`docs/research/LEAD-MINING-ADVERSARIAL-AUDIT-2026-11.md` 给出明确 **NO-GO**：六组需求中仅第 4 组与第 6(2) 组基本满足，第 1/2/3/5/6 组均有实质缺口。以下发现**推翻了我上一轮（026-lead-mining-gap-close）的部分完成声明**，必须诚实记录：

### P0-1 配置双轨（上线阻塞）
`lead_mining.go:304-310` 的 legacy 单例开关/渠道白名单，与 `lead_judge_context.go:169-179` 的三粒度规则互不相通。运营在规则页配的账号级规则不生效；阈值恒取 legacy。`min_intent_score` 等表单字段与规则表是两份数据。

### P0-2 群聊上下文"假聚合"（上线阻塞）——我上一轮的改动反而加重了它
`lead_judge_context.go:181-188` 从 `fetchHistoryDB` 取到真实双向/多成员历史，但 `:211-218` 用 `ConversationContext.Messages`（只含当前一条消息）**整体覆盖**；我上一轮加的 `:224-239` 群聊富化又基于这份错误缓存再覆盖一次。`lead_discovery.go:56` 的角色硬编码为 `"member"`，所以 `GroupRoleCounts` 最多 1。我上一轮的测试 `lead_judge_context_test.go` 只断言提示字符串包含，**没有验证真实消息内容**，构成假绿。

### 关键 P1（按依赖排序）
- P1-1 个微/QQ 根本不在线索资格白名单（与 D1 决策一致，本设计 P0 已覆盖）。
- P1-2 每条消息 3 次 LLM 调用、证据 ID 用 `history_N` 伪造；ReAct 未实现（用户 D18 决定 V2 实现，见 §6.7/P6；3 次调用问题由 ReAct 循环收敛为受控取证）。
- P1-3 `lm:*` 平行身份仍在（与 D3 决策一致，本设计 P2 已覆盖）。
- P1-4 通知双路径重复 + 两套路由口径 + 脱敏不足 + UserID=0 广播（本设计 §6.5 需加强：收敛为单出口）。
- P1-5 跟进记录从未自动生成 + 历史摘要缺失（与 D8 决策一致，本设计 §6.4 已覆盖）。
- P1-6 `supervision.md:46` 记 T066/T070 not-run，`tasks.md:148,152` 却打勾 `[x]`；`api_verify_full.py` 完全未覆盖新增 API。

### 本设计据此修正

1. **P0 切片前置两块**：P0-a 合并配置源（删 legacy 判据、三粒度规则为唯一开关/渠道/阈值来源、`SaveConfig` 与规则保存同事务）；P0-b 修复群聊上下文覆盖（以 `history` MessageHub 数据为权威，共享缓存只按 MessageID 去重后合并、禁止覆盖；ingress 层 `AppendMessage` 补真实双智能体历史）。
2. **测试真实性门**：所有新增/修改判定测试必须断言**真实消息内容与 SenderID**（3 成员群聊夹具），不得只测提示字符串；`lead_judge_context_test.go` 旧断言降级为补充，另补真库测试。
3. **台账诚实性**：`tasks.md` 中 T066/T070 等 not-run 项回退为未完成，直到真库/API 三端验收证据到位；不再以合成测试全绿冒充端到端通过。
4. **证据 ID 真实性**：`history_N` 伪 ID 改为真实 `MessageHub.MsgID`，与 evidence_refs 机械校验对接。

## 11. 设计决策报告的交叉校验

- 已并入：`channelgw.Default` + `MineMode` + parity 门（§6.1）；`CustomerChannel` 身份锚已补齐勿重复立项、`ConversationContext.Summary` 未接线、CAS 并发缺口（§9/§11）。
- 冲突处理：行业报告建议群聊按 `(conversation,sender,role)` 拆多条，用户选「群=客户」——以用户决策为准（§9）。
- ReAct：builder review Q5 用户主动升级为 V2 范围（D18），已补 §6.7 与 P6 切片；与调研 §3.3「ReAct 用于异常分支取证」一致，但步数与 fallback 必须钉死。

## 12. 后续阶段（本设计不做，记录备查）

---

**Builder check**：按 brainstorming skill，设计稿完成后由独立 reviewer 以 builder-check-prompt 审阅，再决定是否需要补一轮用户问题。
