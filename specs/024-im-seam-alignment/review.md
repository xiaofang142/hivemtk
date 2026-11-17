# 024 二次审核（头脑风暴式对抗复核）

复核对象：`spec.md`（371 行）、`plan.md`（97 行）、`tasks.md`（1587 行，62 任务）
复核方式：不重复 §七 C1–C19 的「断言是否属实」，改为**换镜头找漏洞**——必要性、正确性、失效模式、可运维、可验证、可交付。
证据口径：全部为本轮直读代码复核（`user-server/` 与 `migrations/`），行号为复核时快照。

结论先行：**三处需要先决策再动手**（R1 范围拆分、R2 身份键形状取单键还是双层键、R7 交互回调一期降级），
**五处属于事实性错误或与既有机制冲突，必须改文档**（R2 索引机制、R3 事件出口、R4 渲染切块、R5 sender 结果不变量、R12 段落自相矛盾），
其余为补强项。**标注「已改」的项已同步改进 `spec.md`／`tasks.md`／`plan.md`。**

---

## A. 阻断级（不改就会返工或上线后静默出错）

### R1 范围：024 同时装「五个接缝」和「四个平台」，大概率永远不落地

- 事实：62 个任务里，B6–B9（wecomcs/OneBot/Slack/wecombot/custom）占 22 个，
  且每个平台在 `tasks.md` 里都是 5 段以上（账号表、验签、解析、出站、门禁）。
- 后果：接缝（B1–B5）是被平台依赖的前置，一旦平台批次延期，接缝成果也无法先发布取收益；
  反过来若先合接缝，四人日量级的平台改动又长期挂在同一个 spec 上，状态从「定稿」变成「长期在制」。
- 建议：拆成
  - **024 接缝**：B1–B5 + B9 的最小门禁（T1201/T1202/T1204），约 18–22 任务；
  - **025 平台接入**：B6–B9，按 wecomcs → OneBot → Slack → wecombot 逐平台独立交付；
  - custom 通用入口留在 025 首位（它是「不写代码也能接平台」的兜底，必须先于四个具名平台）。

### R2 身份键：真实索引是 `(tenant_id, one_id, channel)`，且三条链路必须同改

- 事实一：`tenancyidx.Normalize` 会把所有租户表上的唯一索引**原位重建为 `(tenant_id, 原列…)`**
  （`internal/pkg/tenancyidx/tenancyidx.go:1-26`，且保留 `WHERE` 谓词，L212/L253）。
  所以今天 `customer_channels` 的实际唯一键是 `(tenant_id, one_id, channel)`，
  spec 一、五接缝表写的「`(one_id, channel)`」不精确。
- 事实二：`ON CONFLICT` 的目标是**硬编码列清单**——
  `repository/channel_overview.go:158-169` 写死 `tenant_id/one_id/channel`。
  只改 model tag 不改这里，写入会 `no unique or exclusion constraint matching the ON CONFLICT specification` 直接报错。
- 事实三：GORM `AutoMigrate` **按索引名判存在**，不比较列集合（`tenancyidx` 头注释同口径）。
  只改 tag，旧索引名字还在 → 列不会更新；必须显式 `DROP INDEX` 让下一次 AutoMigrate 以新列重建。
- 事实四：`idx_oneid_channel` 是 tag 产物（`model/customer_channel.go:25-27`），
  `migrations/` 里没有任何 SQL 建过它 → 迁移 065 的 `DROP` 必须容忍「全新库还没建」的情况，且
  `CONCURRENTLY` 不能放在事务里（依赖现有部署方式）。
- 事实五（新发现，比 account_id 更致命）：`UpsertCustomerChannel` 只按 `(one_id, channel)` 定位，
  **同一个平台账号被识别成两个 OneID 时会写出两行**（各指向不同客户）。
  Chatwoot 的锚点是 `(inbox_id, source_id)`——平台账号维度，不是「我们合成的身份」维度。
- 建议：两层键
  1. 身份锚（新增，唯一）：`(tenant_id, channel, account_id, channel_user_id)`——一个平台账号只能绑一个 OneID；
  2. 业务键（改造，唯一）：`(tenant_id, one_id, channel, account_id)`——一个客户在一个渠道实例下只有一条绑定。
  两条都用显式 SQL 建、都进 `ON CONFLICT` 改造、迁移前先按 `last_seen_at` 去重（两组各去一次）。
- 附带坑：软删行仍占唯一键；`ON CONFLICT` 命中已删行时 `DoUpdates` 不含 `deleted_at` →
  更新写进一条「看不见的行」。改造时 `DoUpdates` 必须带 `deleted_at = NULL`（或改 `Unscoped` 恢复）。

### R3 「只发事件总线 + 审计」不成立：总线是 best-effort 且会丢

- 事实：`internal/event/bus.go:1-17` 明确写「异步、非阻塞、队列满则丢弃，不阻塞主流程；
  不适用关键业务路径」；只有 `TopicCustomerMessageReceived` 进 critical 队列（L75-77）。
- 后果：按 D2 设计，成员入群/退群、机器人被邀请等事件**唯一**出口是总线 → 队列一满就静默消失，
  而这类事件恰恰是「客户资产归属」的证据。
- 建议：事件分两类处置
  - 需要留证的（`group.member_*`、`bot.*`、`interaction.callback`）：**同步写审计表**
    （现成 `operation_log` / `security_audit`，二者都有 model 与写入 helper），总线只做旁路通知；
  - 纯通知类：仍走总线，但必须新增 topic 常量并登记为 critical 或明确「可丢」。
- 同时定死审计表幂等键：`(tenant_id, channel, channel_msg_id)` 唯一，否则平台重投会重复记账（见 R11）。

### R4 渲染层切块与既有客户端切块重复，且违反 FR006 行为冻结

- 事实：QQ 客户端已经按 `QQMessageMaxLenEffective()` 切块（`channelbot/qq/qq.go:38-66,195`，
  且上限可由 config 注入）。`RenderForChannel` 再切一次 → 双重切分、块数与顺序不可预期，
  且微信/企微客户端内部还有各自的换行策略。
- 事实：FR006 要求「搬迁前后同一组 (channel,payload) 调用序列一致」，而 D4 引入切块是**行为变更**，
  两条要求直接冲突。
- 建议：`RenderForChannel` 只做三件事——按 `OutboundFormat` 去 Markdown / 卡片降级为文本 / 超长**告警**；
  **不切块**，长度上限留在各渠道客户端（已有先例）。`MaxTextLen` 改语义为「超过即打点 `outbound_text_overflow`」，
  若将来要切块，单独立项并写明行为变更与回放测试。

### R5 sender 可以「既没发也没报错」，外层会当成正常

- 事实：`sendOutbound` 的失败口径全靠 `markSendFailed` 副作用；
  现有企微分支的每条早退路径都先 `markSendFailed(...)` 再 `return`（`webhook_outbound.go:685-706`）。
  搬迁成 sender 后，谁漏调 `req.Fail` 谁就制造出 `Sent=false && sendErr==nil`——
  重试不入队、审计不落行、调用方拿到「没发也没错」。
- 建议：注册表在 `Send` 返回后加不变量校验：
  `if !res.Sent && sendErr == nil → req.Fail(&ChannelError{Category: CategoryUnknown, Retryable: true, Raw: "sender reported neither success nor failure"})`，
  并配 `TestSenderOutcomeMustBeExplicit`（构造一个空实现 sender 断言被兜住）。
  另加 `Fail` 的并发契约：必须在 `Send` 返回前同步调用（`-race` 下跑一遍）。

---

## B. 严重（会导致实现期返工，但不阻断设计）

### R6 注册表的依赖注入形状未定，`RegisteredSenders()` 无法静态枚举

- 事实：sender 需要 `s.integrations.*`（lazy singleton，QQ 的 `msg_seq` 必须跨调用存活）、
  `s.messageHubRepo`、`s.inboxConvRepo` 等；这些属于 `WebhookService` 实例。
- 若在 `NewWebhookService` 里逐实例注册，`RegisteredSenders()` 就只能拿到「当前实例的那几个」，
  parity 测试与文档生成会随装配顺序漂移。
- 建议：**两张表**
  - 静态工厂表 `var senderFactories = map[WebhookChannel]func(*WebhookService) channelSender`（编译期可枚举，供 parity/文档）；
  - 实例注册表在 `NewWebhookService` 遍历工厂表构建（供热路径 `ChannelSenderOf`）。
  两者由同一测试校验键集相等。

### R7 `interaction.callback` 一期做不了「答复」

- 事实：平台侧答复回调需要各自凭据——Telegram 要 `callback_query.id` 调 `answerCallbackQuery`、
  飞书要在 HTTP 响应体里回包、企微智能机器人要走 `response_code`。这些都需要**交互上下文**（谁点的哪个按钮）。
- D2 现在写「一期只做答复 + 审计 + 路由留痕」，等于假设上下文可得，但 `MessageEvent` 里没有 callback_id 字段。
- 建议：一期明确降级为「**记录 + 路由留痕 + 平台侧空 ack**」（Telegram 空 `answerCallbackQuery` 即可消除客户端转圈），
  真答复（改卡片/回填表单）排到引入 `channel_interactions` 表之后，写进「需证据触发」。

### R8 wecomcs 的 `sync_msg` 游标没有落点

- 事实：企业微信客服是**游标拉取**模型（`cursor` + `token`，回包带 `next_cursor`），
  与我们其它平台的「推送即处理」不同。spec FR060 只写了「游标拉取」，没有存哪儿、谁拉、多久拉。
- 建议：`wecom_cs_accounts` 增 `sync_cursor varchar(512)` + `cursor_updated_at`；
  拉取 worker 沿用 T602 的 cron 先例（Ticker + `utils.SafeGo` + 成对 start/stop），
  单账号串行、失败退避；`kf` 消息自带 `msgid`，直接复用 msg_id 幂等。

### R9 OneBot 反向 WS 缺宿主定义

- 事实：反向 WS 是**长连接服务端**（我们 accept，OneBot 实现方连进来），
  不是 gin 的请求-响应路径；且需 `Authorization: Bearer <token>` 鉴权、心跳、断线重连后的会话恢复。
- spec 只写「HTTP + 反向 WS 两种接入」。建议补：
  - 独立 `internal/channelbot/onebot` 内的 `WSServer`（`gin` 路由上做 `Upgrade` 或独立 `http.Server`，二者选一并写清）；
  - `self_id → account_id` 连接注册表（多账号并存，同一 self_id 重连要顶掉旧连接）；
  - 生命周期挂到 app 的 start/stop（过 `check-naked-goroutine.sh`）；
  - HTTP 上报路径另需 `X-Signature`（`sha1(secret+body)` 或 token 校验），失败 4xx 不落库。

### R10 新账号表的口令存储与出口脱敏未定

- 事实：现网 `WeComAccount.CorpSecret` 是**明文列**（`model/wecom.go:13-17`），
  靠 controller 出口脱敏兜底（`internal/controller/wecom_secret_mask.go`，配 `wecom_secret_mask_test.go`）。
  另有 `internal/secrets/aesgcm.go` 用于 LLM key 等（`aiagent/llm/provider_crypto.go` 先例）。
- 影响：wecomcs 的 `kf_aes_key`、Slack `signing_secret`、OneBot `access_token`、custom `hmac_secret`
  都是可直接冒充平台的凭据，若沿用明文列，任何 DB 只读账号即可伪造回调。
- 建议（需决策）：新表统一用 `secrets.AESGCM` 加密存储 + 出口脱敏测试；
  存量 `wecom_accounts` 不动（避免扩大范围），但在 spec 里登记为已知偏差。

### R11 路由命中 `discard` 不落库 → 平台重投会重复审计

- 事实：幂等今天靠 `message_hub` 的 `msg_id` 唯一索引；`discard` 明确「不入库」，
  于是同一条消息被平台重投 N 次就审计 N 次，且规则若中途改动会出现「同一 msg 一次丢一次收」。
- 建议：`discard` 也写一张轻量去重行（或审计表上加唯一 `(tenant_id, channel, channel_msg_id)`，见 R3），
  保证「同一平台消息只被裁决一次」。

---

## C. 文档一致性与可验证性

### R12 D2 段落自相矛盾

- 现状：`spec.md:110` 写「撤回走 `MarkRecalled`（列已存在）」，紧接着 `:112-115` 又论证
  「`MarkRecalled` 带 `direction='outbound'`，不能拿来标入站撤回」。同一段先给结论再自我否定。
- 处置：`:110` 改为「撤回走入站专用方法 `MarkInboundRecalledByChannelMsgID`」。

### R13 工期与任务量不匹配

- 现状：`plan.md` 合计 21–26 人日；62 个任务里 22 个是平台接入，单 wecomcs（游标+新表+验签+出站+5 测试）
  就很难低于 4 人日。按批粗算：B1 4–5、B2 3–4、B3 4、B4 2、B5 3、B6 5、B7 4–5、B8 3、B9 2–3 ≈ **30–34 人日**。
- 处置：重算工期；若维持 21–26，则必须按 R1 拆分（024 去掉 B6–B9 后约 18–20 人日，才与预算自洽）。

### R14 门禁缺并发维度

- 新增物里至少三处并发面：sender 注册表（读写竞态）、snooze 到期 worker、OneBot WS 连接表。
- 处置：`验收总纲` 与 `plan.md` 的三层验证里加 `go test -race ./internal/service/ ./internal/channelbot/...`，
  并在 T1201 里写明（现有 CI 脚本里没有 `-race`）。

### R15 缺「发送失败可观测」验收

- 事实：`ChannelError` 已有 7 个 `Category`（`service/channel_error.go`），但没有任何指标任务；
  失败只落审计行，运营看不到「某渠道 bad_request 飙高」。
- 处置：补一条任务：按 `(channel, category)` 计数打点 + 一条 `outbound_send_failed_total` 语义的测试。

### R16 `OutboundFormat` 必须取证，不能凭印象填

- 现状：FR030 要求 21 条 manifest 全量补齐 `OutboundFormat/MaxTextLen`，但没有任何「从代码取证」的步骤。
  实际各渠道差异很大（企微 text 内支持换行与有限 markdown、飞书 `post`/`interactive`、钉钉纯 text、bridge 走 DOM 注入）。
- 处置：每个平台的 `OutboundFormat` 必须附「来源锚点」（sender 里处理换行/转义/HTML 的那几行），
  空口填 `markdown` 等于制造新的漂移源。

### R17 tasks.md 单文件 1587 行，并行批次会互相踩

- 现状：B1–B9 分属不同文件，但任务清单在同一个 1587 行文件里；两人并行时几乎必然冲突。
- 处置（可选）：按批拆 `tasks-b1.md`…`tasks-b9.md` 并由 `tasks.md` 索引；
  若坚持单文件，至少在 `plan.md` 的并行边界表里加「tasks.md 同一时间只允许一个人改」。

---

## D. 复核后仍然成立、不需改的判断

1. D1 的「`Send` 不返回 error，失败靠 `Fail` 回调」——理由（单一真值来源 + 飞书先记失败后补成功）经复核成立。
2. D1 把 bridge Markdown 清洗上移横切段——成立且必须做（否则重试投未清洗文本）。
3. D2 不新增 `MessageHub.event_type` 列——成立（编辑/撤回各有窄入口，多一列就是第二真值源）。
4. D5 必须走显式 SQL——成立，且实际约束比 spec 写的更紧（见 R2 四条事实）。
5. 平台顺序 wecomcs → OneBot → Slack → wecombot——按「协议确定性 + 存量客户」排序成立。

---

## E. 建议处置顺序

1. 先决策三件：R1 拆分、R2 身份键形状（单键 vs 双层键）、R7 交互回调降级。
2. 再改文档：R2/R3/R4/R5/R12/R13（事实与冲突类，直接改）、R6/R8/R9/R10/R11/R14–R16（补进任务与门禁）。
3. 最后动工：024 从 B1 开始，025 独立起 spec，等 024 的 B1 合入后再开工。
