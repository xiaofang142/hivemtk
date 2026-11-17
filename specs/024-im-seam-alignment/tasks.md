# Tasks: 024-im-seam-alignment（IM 接入接缝对齐）

**Input**: Design documents from `/specs/024-im-seam-alignment/`
**Prerequisites**: 既有交付 `2f658e7b`（`internal/channelcaps` + 会话生命周期三列）；调研文档 4 篇（见 `spec.md` 来源）
**Constraint**: 每任务独立可回滚；平台接入任务以「触点齐全」为完工定义；所有 Go 命令在 `user-server/` 下执行

图例：P0 阻塞后续 / P1 本轮内 / P2 可延后至下一 spec｜🟡 纯本地可验收｜🔵 需真实账号验收（代码与 fixture 测试仍本地完成）

**任务块字段约定**（每个任务都按这七项写，缺项视为任务未定义）：

| 字段 | 含义 | 判据 |
|---|---|---|
| 规格 | 要交付什么、边界在哪、不做什么 | 能据此判定「做完」与「越界」 |
| 上下文 | 现状代码锚点（`文件:行`）与为什么现在做 | 每条锚点可 `git grep` 命中 |
| 数据结构 | 新增/改动的 Go 类型、表结构、JSON 契约 | 字段名与类型可直接抄进代码 |
| 架构 | 所在层（Router/Controller/Service/Repository/Model）与依赖方向 | 不出现跨层依赖 |
| 数据流向 | 一次调用从入口到落库/发出的完整链条 | 链上每一跳都有函数名 |
| 逻辑图 | 分支/状态/时序的可视化 | 与「数据流向」一致，可直接对照代码 |
| 验收 | 测试名或命令 + 期望结果 | 命令可复制执行 |

---

## 全局数据流向（本 spec 结束后的目标形态）

```mermaid
flowchart TB
  subgraph IN["入站（客户 → 系统）"]
    P["平台回调 / WS 帧"] --> V["Verify 验签"]
    V --> ST["ParsePayload + 渠道解析器<br/>填 EventType"]
    ST --> N["NormalizeEvent<br/>唯一收口点"]
    N --> RT["RouteEngine.Resolve<br/>规则表"]
    RT -->|discard| AUD["审计事件"]
    RT -->|放行| SW{"EventType 分流"}
    SW -->|message.received| HUB["MessageHub 落库<br/>+ InboxConversation 更新"]
    SW -->|edited / deleted| UPD["UpdateContent / MarkInboundRecalled"]
    SW -->|member / bot / reaction| BUS["事件总线"]
    SW -->|interaction.callback| ACK["回 ack，不建消息"]
    HUB --> AI["triggerAIForEvent"]
  end
  subgraph OUT["出站（AI → 客户）"]
    AI --> SO["sendOutbound<br/>横切段 ≤150 行"]
    SO --> R["RenderForChannel<br/>格式 / 拆块 / 卡降级"]
    R --> REG["ChannelSenderOf"]
    REG --> S1["wecom / feishu / telegram / qq"]
    REG --> S2["whatsapp / dingtalk / wechat"]
    REG --> S3["bridge-outbox / 新平台 sender"]
    S1 --> RES["OutboundResult"]
    S2 --> RES
    S3 --> RES
    RES -->|失败| FAIL["Fail(err) → enqueueSendRetry"]
    RES -->|成功| DONE["PlatformMsgID 回写"]
  end
```

---

## Phase 1 出站接缝（P0，平台接入的前置）

- [ ] **T101 出站发送器注册表**（P0｜依赖 —｜S｜🟡）
  - **规格**：新增进程级注册表，把「按渠道 switch」换成「按渠道查 sender」。新增平台 = 新增一个 sender 文件 + 一行 `RegisterChannelSender`，主函数不再改动。本任务零行为变更，只交付骨架与生命周期（后续 T102–T109 逐个搬入）。
  - **上下文**：`internal/service/webhook_outbound.go:607` 的 `sendOutbound` 663 行，内含 7 个 `case`（L685/709/748/792/818/870/951）+ 1 个 `default`（桥接族，L967 起）；平台越多，这个函数回滚面积越大（spec §1 原则 1）。
  - **架构**：Service 层内部组件，只依赖 `model` 与 `WebhookChannel` 类型；被 `sendOutbound` 调用，不被 Controller/Repository 感知。
  - **数据结构**：
    ```go
    type channelSender interface {
        Channel() WebhookChannel
        Send(ctx context.Context, req *OutboundRequest) *OutboundResult
    }
    type OutboundRequest struct {
        Channel     WebhookChannel
        AccountID   string
        Payload     *ParsedPayload
        Content     string
        Cards       []model.RichCard
        Hub         *model.MessageHub
        CardsFallbackCount int
        Fail        func(error) // 已跟踪消息：写 sendErr + outboundSendFailed
        FailUntracked func(error) // 无 hubMsg：只写日志与重试
    }
    type OutboundResult struct{ Sent bool; PlatformMsgID string }
    ```
    注册表：`map[WebhookChannel]channelSender` + `sync.RWMutex`；`RegisterChannelSender` 对空 Channel / 重复注册 `panic`（与 `capregistry.Register`、`channelcaps.Register` 同风格）。
  - **数据流向**：`sendOutbound` → `ChannelSenderOf(ch)` → `sender.Send(ctx, req)` → `*OutboundResult`；失败不返回 error，走 `req.Fail(err)`，保证 `sendErr` 只有一个写入点。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["sendOutbound"] --> B{"ChannelSenderOf"}
      B -->|命中| C["sender.Send"]
      B -->|未注册| D["ErrOutboundChannelUnsupported"]
      C -->|成功| E["OutboundResult{Sent:true}"]
      C -->|失败| F["req.Fail(err) → sendErr"]
    ```
  - **验收**：`go test ./internal/service/ -run TestOutboundRegistry` 绿，覆盖：重复注册 panic、未知渠道返回 `ErrOutboundChannelUnsupported`、注册后取回同一实例、`RegisteredSenders()` 按 channel 字典序稳定、`Fail` 被调用后 `sendErr` 非空。
  - **不做**：不搬任何 case 的函数体（T102–T109），不改 `sendOutbound` 主体的 defer 逻辑（T110）。

- [ ] **T102 wecom sender 搬迁**（P0｜依赖 T101｜S｜🟡）
  - **规格**：把 `case ChannelWeCom` 原样搬成 sender，payload 字段与调用参数一字不改。
  - **上下文**：`webhook_outbound.go:685`——`s.integrations.wecom().SendMessage(ctx, WeComSendRequest{AccountID, ExternalUserID, MsgType:"text", ...})`。
  - **架构**：`service/outbound_sender_wecom.go`，`type wecomSender struct{ svc *WebhookService }`；经 `svc.integrations` 取 integration，不直接持有 client。
  - **数据结构**：
    ```go
    type WeComSendRequest struct {
        AccountID      string
        ExternalUserID string
        MsgType        string // "text"
        Content        string
    }
    ```
  - **数据流向**：`sendOutbound` → `wecomSender.Send` → `svc.integrations.wecom().SendMessage` → 错误时 `req.Fail(err)`。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["wecomSender.Send"] --> B["integrations.wecom()"]
      B --> C["SendMessage(req)"]
      C -->|err| D["req.Fail(err)"]
      C -->|ok| E["Sent=true"]
    ```
  - **验收**：`go test ./internal/service/ -run 'TestWecomSender|TestCardDelivery'` 绿；`TestWecomSender_DelegatesToIntegration` 用 fake integration 断言 `(AccountID, ExternalUserID, MsgType, Content)` 与搬迁前一致。

- [ ] **T103 feishu sender 搬迁**（P0｜依赖 T101｜S｜🟡）
  - **规格**：搬 `case ChannelFeishu`，**保持「正文失败仍继续发卡片」的顺序**；卡片转 interactive JSON 暂留原样（T404 才统一走渲染层）。
  - **上下文**：`webhook_outbound.go:709`——目标为 `p.Sender`（单聊 open_id）或 `hubMsg.GroupID` + `Extra["open_chat_id"]`（群聊）；文本失败先 `markSendFailed`，随后仍逐卡 `SendInteractiveCard`，因此最终可能 `sent=true` 且 `sendErr!=nil`。
  - **架构**：`service/outbound_sender_feishu.go`；sender 只报错不短路，顺序敏感逻辑集中在 `Send` 内。
  - **数据结构**：
    ```go
    type feishuTarget struct { openID string; chatID string } // 群聊用 chatID，单聊用 openID
    ```
  - **数据流向**：`Send` → `feishuCardToInteractiveJSON(card)` → `SendInteractiveCard`；文本步骤错误先 `req.Fail(err)`，卡片步骤继续，任一卡片成功则 `Sent=true`。
  - **逻辑图**：
    ```mermaid
    flowchart TD
      A["Send"] --> B["解析目标 open_id / chat_id"]
      B --> C["发送文本"]
      C -->|失败| D["req.Fail(err)"]
      C -->|成功| E{"有卡片?"}
      D --> E
      E -->|是| F["逐卡 SendInteractiveCard"]
      E -->|否| G["结束"]
      F -->|任一成功| H["Sent=true"]
    ```
  - **验收**：`TestFeishuSender_RichCardPayload`（interactive JSON 快照一致）、`TestFeishuSender_TextFailStillSendsCards`（文本失败后卡片仍被调用）。

- [ ] **T104 telegram sender 搬迁**（P0｜依赖 T101｜M｜🟡）
  - **规格**：搬 `case ChannelTelegram`，含群回复增强；`buildTelegramGroupReply` 留在 `webhook_outbound.go` 供 sender 调用（不一起搬，减少 diff）。
  - **上下文**：`webhook_outbound.go:748`——`ParseUint(accountID)`、`chatID ← hubMsg.ConversationID`、`extractTelegramReplyMetaFromCtx(ctx)` 且 `TriggerReason != "start"` 时 `buildTelegramGroupReply`、`SendMessageEx` 后逐卡 `SendCard`。
  - **架构**：`service/outbound_sender_telegram.go`，依赖 `integrations.tg()`（懒加载）。
  - **数据结构**：
    ```go
    type TelegramReplyMeta struct { TriggerReason string; /* 其余字段沿用既有定义 */ }
    // sendOpts 保留 reply_parameters / parse_mode
    ```
  - **数据流向**：`Send` → 取 meta → 需要时包装正文（@发送者）→ `SendMessageEx(ctx, accID, chatID, content, opts)` → 逐卡 `SendCard`。
  - **逻辑图**：
    ```mermaid
    flowchart TD
      A["Send"] --> B["ParseUint(accountID)"]
      B --> C["chatID = Hub.ConversationID"]
      C --> D{"meta != nil 且 TriggerReason != start"}
      D -->|是| E["buildTelegramGroupReply"]
      D -->|否| F["原文"]
      E --> G["SendMessageEx"]
      F --> G
      G --> H["逐卡 SendCard"]
    ```
  - **验收**：`TestTelegramSender_GroupMention`、`TestTelegramSender_Cards` 绿；行为冻结断言含 `reply_parameters.message_id` 与 `parse_mode`。

- [ ] **T105 qq sender 搬迁**（P0｜依赖 T101｜M｜🟡）
  - **规格**：搬 `case ChannelQQ`，被动回复窗口注释一并迁移（注释即行为契约）；sender 必须复用进程级 `s.qqIntegration` 单例。
  - **上下文**：`webhook_outbound.go:792`——`SendMessage(ctx, accID, convID, QQOutboundMsgID(hubMsg), content)`；被动窗口 5 分钟 / 5 条，`msg_seq` 自增。
  - **架构**：`service/outbound_sender_qq.go`，只经 `svc.qqIntegration` 访问（不可 `new`，否则 `msg_seq` 归零被平台判重）。
  - **数据结构**：
    ```go
    // 关键：sender 持有 svc，qqIntegration 为懒加载单例
    type qqSender struct{ svc *WebhookService }
    ```
  - **数据流向**：`Send` → `QQOutboundMsgID(hubMsg)`（被动回复锚点）→ `SendMessage` → 平台错误码 → `req.Fail`。
  - **逻辑图**：
    ```mermaid
    sequenceDiagram
      participant SO as sendOutbound
      participant QS as qqSender
      participant QI as qqIntegration(单例)
      SO->>QS: Send(req)
      QS->>QI: SendMessage(acc, conv, msgID, content)
      QI->>QI: msg_seq++
      QI-->>QS: 结果
      QS-->>SO: Sent / Fail
    ```
  - **验收**：`TestQQSender_PassiveWindowMsgSeq`（连续 6 条：前 5 条被动、第 6 条降级或失败，与原实现一致）、`TestQQSender_SingletonSeqMonotonic`。

- [ ] **T106 whatsapp sender 搬迁**（P0｜依赖 T101｜M｜🟡）
  - **规格**：搬 `case ChannelWhatsapp`，含 24h 窗口判定与模板降级；不改查询语义。
  - **上下文**：`webhook_outbound.go:818`——`ensureReposFromDB` → `GetLastInboundByConversation` → `time.Since(last.SentAt) > 24h` → 取参数 `channelbot.whatsapp_fallback_template` / `_lang` → 有模板 `SendTemplateMessage` 并 return；无模板 `CategoryWindowExpired` 失败；否则 `SendMessage`。
  - **架构**：`service/outbound_sender_whatsapp.go`，需持 `svc`（取 repo 与参数中心）。
  - **数据结构**：
    ```go
    type WhatsAppTemplatePayload struct { TemplateName, Language string }
    // 失败：&ChannelError{Channel: ch, Category: CategoryWindowExpired, Retryable: false}
    ```
  - **数据流向**：`Send` → 查最后入站 → 窗口内 `SendMessage`；超窗 → 查模板 → `SendTemplateMessage` / `Fail(window_expired)`。
  - **逻辑图**：
    ```mermaid
    flowchart TD
      A["Send"] --> B["ensureReposFromDB"]
      B --> C["GetLastInboundByConversation"]
      C --> D{"距最后入站 > 24h?"}
      D -->|否| E["SendMessage"]
      D -->|是| F{"fallback_template 非空?"}
      F -->|是| G["SendTemplateMessage"]
      F -->|否| H["Fail(CategoryWindowExpired)"]
    ```
  - **验收**：`TestWhatsappSender_PayloadShape`、`TestWhatsappSender_WindowExpired`、`TestWhatsappSender_WindowExpiredWithTemplate`。

- [ ] **T107 dingtalk sender 搬迁**（P0｜依赖 T101｜M｜🟡）
  - **规格**：搬 `case ChannelDingTalk`。该渠道**不走 integration**：读会话 webhook 直发；域名白名单校验留在 sender 内（属渠道安全边界，不下沉为通用逻辑）。
  - **上下文**：`webhook_outbound.go:870`——`hubMsg.Extra["session_webhook"]`、`session_webhook_expired_at`（秒/毫秒兼容：`>1e12` 视为毫秒）、`loadDingtalkWebhookHostAllowed()`（L1304，仅 `*.dingtalk.com`）、裸 `http.DefaultClient.Do`、按 `errcode` 判成败。
  - **架构**：`service/outbound_sender_dingtalk.go`；不依赖 integration，但依赖 Service 层的白名单加载器。
  - **数据结构**：
    ```go
    // 请求体
    type dingtalkTextMsg struct {
        MsgType string            `json:"msgtype"` // "text"
        Text    map[string]string `json:"text"`    // {"content": ...}
    }
    // 响应体
    type dingtalkResp struct { ErrCode int `json:"errcode"`; ErrMsg string `json:"errmsg"` }
    // Extra 键：session_webhook / session_webhook_expired_at
    ```
  - **数据流向**：`Send` → 校验 Extra → 校验过期 → `url.Parse` + 白名单 → POST → 解析 `errcode` → `Fail`/`Sent`。
  - **逻辑图**：
    ```mermaid
    flowchart TD
      A["Send"] --> B{"session_webhook 存在?"}
      B -->|否| C["Fail(CategoryWindowExpired)"]
      B -->|是| D{"已过期?"}
      D -->|是| C
      D -->|否| E{"host 在白名单?"}
      E -->|否| F["Fail(CategoryBadRequest)，不发请求"]
      E -->|是| G["POST + 解析 errcode"]
      G -->|errcode != 0| H["Fail"]
      G -->|0| I["Sent=true"]
    ```
  - **验收**：`TestDingtalkSender_RejectsNonAllowlistedHost`（断言**零请求**）、`TestDingtalkSender_ExpiredWebhook`、`TestDingtalkSender_ErrcodeNonZero`。

- [ ] **T108 wechat sender 搬迁**（P0｜依赖 T101｜S｜🟡）
  - **规格**：搬 `case ChannelWechat`；保留「`ParseUint` 失败只置 `accID=0`、不早退」的既有宽容行为。
  - **上下文**：`webhook_outbound.go:951`——`wechatIntegration.SendCustomMessage`。
  - **架构**：`service/outbound_sender_wechat.go`。
  - **数据结构**：
    ```go
    // 沿用 integration 既有入参：账号（uint） + openid + content
    ```
  - **数据流向**：`Send` → `integrations.wechat()` → `SendCustomMessage(accID, openID, content)`。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["wechatSender.Send"] --> B["ParseUint(accountID)"]
      B -->|err| C["accID = 0（不早退）"]
      B -->|ok| D["SendCustomMessage"]
      C --> D
    ```
  - **验收**：`TestWechatSender_CustomerServiceMessage`；`TestWechatSender_BadAccountIDDoesNotShortCircuit`。

- [ ] **T109 bridge-outbox sender 搬迁**（P0｜依赖 T101｜M｜🟡）
  - **规格**：搬 `default` 分支（桥接族约 300 行）。**Markdown 清洗不在本任务搬**——它上移到横切段（T110），否则 `enqueueSendRetry` 捕获的 `content` 与今天不一致。
  - **上下文**：`webhook_outbound.go:967` 起——`bridgeOutboxCapable` 守卫、构造 pending `MessageHub`、`isBridgeChannelUndeliverableLocal`、`CreateMessageHubIdempotent`（失败带 `":"+convID` 重试）、`UpsertFromMessage`、SSE 唤醒、`persisted.ID==0 / Status=="failed" / default` 三段口径。
  - **架构**：`service/outbound_sender_bridge.go`，依赖 `messageHubRepo`、`inboxConvRepo`、`GlobalSSEPublisher`（均经 `svc`）。
  - **数据结构**：
    ```go
    // 落库 Extra 键（必须全保留）
    // dm_target, scenario, auto_reply, triggered_by, agent_id, confidence,
    // intent, handler_type, degraded, cards_fallback_count, turn_id, answer_attempt
    // 失败附加：undeliverable_reason, scenario
    ```
  - **数据流向**：`Send` → 守卫 → 建行（幂等）→ `ID==0` 分支 → `UpsertFromMessage` → SSE → `Sent`/`Fail`。
  - **逻辑图**：
    ```mermaid
    flowchart TD
      A["Send"] --> B{"bridgeOutboxCapable?"}
      B -->|否| C["FailUntracked(漂移告警)"]
      B -->|是| D["构造 pending MessageHub"]
      D --> E{"本地不可投递?"}
      E -->|是| F["status=failed + undeliverable_reason"]
      E -->|否| G["CreateMessageHubIdempotent"]
      F --> G
      G --> H{"persisted.ID == 0?"}
      H -->|dup| I["写 :dup 行 + Fail(不可重试)"]
      H -->|其它| J["Fail(可重试)"]
      H -->|有 ID| K{"status == failed?"}
      K -->|是| L["FailUntracked"]
      K -->|否| M["UpsertFromMessage + SSE + Sent"]
    ```
  - **验收**：`go test ./internal/service/ -run 'BridgeOutbox|Outbound'` 全绿；新增 `TestBridgeSender_DuplicateOccupied`、`TestBridgeSender_UndeliverableReason`、`TestBridgeSender_RetryCarriesStrippedContent`。

- [ ] **T110 `sendOutbound` 收口 ≤150 行**（P0｜依赖 T102–T109｜M｜🟡）
  - **规格**：主体只留横切逻辑，顺序固定为：静默时段延迟入队 → `ClaimReply` 去重 → `WithTimeout(utils.MediumTimeout)` → **富卡降级成文本 + `cardsFallbackCount`** → **桥接 Markdown 清洗** → `ChannelSenderOf` 分发 → defer 收尾。未知渠道返回 `ErrOutboundChannelUnsupported`。
  - **上下文**：`webhook_outbound.go:607-1269`；两个 defer 是隐式契约——① **仅当 `!sent`** 时 `ReleaseReply` + cancel；② 仅当 `sendErr != nil && !sent && !isDelayedReplay` 时 `enqueueSendRetry(..., content, ...)`；另有 `ReleaseAIProcessingFlag` 与 `SafeGo(RecheckUnrepliedAndTrigger)`（要求 `ConversationID != ""` 且非重放）。
  - **架构**：Service 层编排函数；不含任何渠道知识。
  - **数据结构**：
    ```go
    // 主体伪代码（顺序即契约）
    if quietHours { return enqueueDelayedOutbound(...) }
    if !claimReply(...) { return false, nil }
    ctx, cancel := context.WithTimeout(ctx, utils.MediumTimeout)
    defer func(){ if !sent { ReleaseReply; cancel() } }()
    content, cardsFallbackCount := renderCardsFallback(channel, content, cards)
    if bridgeOutboxCapable(channel) { content = stripIfNeeded(channel, content) }
    res := ChannelSenderOf(channel).Send(ctx, req)
    defer func(){ if res.Err != nil && !res.Sent && !isDelayedReplay(ctx) { enqueueSendRetry(...) } }()
    ```
  - **数据流向**：入参 → 守卫链 → 渲染/清洗 → sender → defer 收尾（重试/释放/补触发）。
  - **逻辑图**：
    ```mermaid
    flowchart TD
      A["sendOutbound"] --> B["静默时段? → 延迟入队"]
      B --> C["ClaimReply 去重"]
      C --> D["WithTimeout"]
      D --> E["富卡降级 + cardsFallbackCount"]
      E --> F["桥接 Markdown 清洗"]
      F --> G["ChannelSenderOf → Send"]
      G --> H["defer: 重试入队 / ReleaseReply / 释放 AI 标记 / 补触发"]
    ```
  - **验收**：`awk` 统计函数体 ≤150 行；`grep -c 'switch channel' internal/service/webhook_outbound.go` = 0；`go build ./...` 绿；`TestSendOutbound_Bridge_MarkdownAlignedWithManualPath`、`TestSendOutbound_DelayedReplayNoRetry`、`TestSendOutbound_ReleasesClaimOnFailure` 绿。

- [ ] **T111 渠道覆盖门禁测试**（P0｜依赖 T110｜S｜🟡）
  - **规格**：把三方集合钉死：注册表已注册 sender ↔ `webhookInboundCapable` 的 7 个渠道 ↔ `channelgw.Default.Names()`（桥接族）。**扩写既有测试，不另建重复门禁**。
  - **上下文**：`TestBridgeOutboxParityWithWebhookOutbound` 已存在（钉住 registry `OutboundPath=bridge_outbox` 与出站分支，默认分支注释 CH-37b刀3）。
  - **架构**：仅测试文件 `outbound_registry_parity_test.go`（新增断言放这里）。
  - **数据结构**：
    ```go
    // 断言结构：map[channel]bool 三份，逐 keys 比对
    ```
  - **数据流向**：测试直接调用 `RegisteredSenders()`、`webhookInboundCapable`、`channelgw.Default.Names()`，不经 HTTP。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["RegisteredSenders"] --> D{"三向差集为空?"}
      B["webhookInboundCapable(7)"] --> D
      C["channelgw.Default.Names()"] --> D
      D -->|否| E["失败并打印渠道名"]
    ```
  - **验收**：故意注释掉一行注册 → 失败信息含渠道名；恢复 → 绿；`go test ./internal/service/ -run 'Parity'` 通过。

- [ ] **T112 行为冻结对照测试**（P0｜依赖 T110｜M｜🟡）
  - **规格**：搬迁前用旧实现生成 golden，搬迁后逐渠道比对 `(method, args)` 序列；差异必须人工确认后才会重写 golden。
  - **上下文**：T102–T109 期间旧实现仍在（`git show <搬迁前 sha>:internal/service/webhook_outbound.go`），这是最后一次能低成本生成基线。
  - **架构**：测试内在 `service` 包，用 fake integrations 替身；golden 存 `internal/service/testdata/outbound_golden.json`。
  - **数据结构**：
    ```go
    type goldenEntry struct {
        Channel  string            `json:"channel"`
        Payload  map[string]any    `json:"payload"`
        Cards    int               `json:"cards"`
        Calls    []struct{ Method string; Args map[string]any } `json:"calls"`
        Sent     bool              `json:"sent"`
    }
    ```
  - **数据流向**：表驱动用例 → fake 记录调用 → 与 golden 比对 → 不一致输出 diff。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["用例表"] --> B["fake integrations 记录调用"]
      B --> C{"与 golden 相等?"}
      C -->|是| D["通过"]
      C -->|否| E["输出 diff，人工确认后重写 golden"]
    ```
  - **验收**：8 渠道 × 3 组样例全绿；`UPDATE_GOLDEN=1` 仅在人工确认后使用。

---

## Phase 2 事件类型体系（P0）

- [ ] **T201 `EventType` 常量与字段**（P0｜依赖 —｜S｜🟡）
  - **规格**：给 `MessageEvent` 增加一等公民事件类型，并规定空值语义 = `message.received`（存量构建路径零改动即保持旧行为）。
  - **上下文**：`model/message_event.go`（93 行）无该字段；`ParsedPayload.EventType`（`webhook.go:1006`）把 `event_type/event/type/msg_type` 一锅 coalesce，语义混杂，**不能作为类型来源**；`NormalizeEvent`（`inbox_ingress.go:480`）是 `channelbot.ToMessageEvent` 与各 `webhook_channel_*.go` 两条构建路径的唯一收口点。
  - **架构**：Model 层定义类型；`NormalizeEvent`（Service 层）负责归一；解析器（Service）只负责填值。
  - **数据结构**：
    ```go
    const (
        EventTypeMessageReceived      = "message.received"
        EventTypeMessageEdited        = "message.edited"
        EventTypeMessageDeleted       = "message.deleted"
        EventTypeMessageReaction      = "message.reaction"
        EventTypeGroupMemberJoined    = "group.member_joined"
        EventTypeGroupMemberLeft      = "group.member_left"
        EventTypeGroupMemberBanned    = "group.member_banned"
        EventTypeBotInvitedToGroup    = "bot.invited_to_group"
        EventTypeBotRemovedFromGroup  = "bot.removed_from_group"
        EventTypeInteractionCallback  = "interaction.callback"
    )
    // MessageEvent 新增：
    EventType string `json:"event_type"`
    ```
  - **数据流向**：渠道解析器 / `channelbot.ToMessageEvent` → `NormalizeEvent`（空值补 `message.received`）→ 下游分流（T203）。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["channelbot.ToMessageEvent"] --> C["NormalizeEvent"]
      B["webhook_channel_*.go"] --> C
      C -->|空 EventType| D["填 message.received"]
      C -->|显式 EventType| E["保留原值"]
    ```
  - **验收**：`TestNormalizeEvent_DefaultsToReceived`、`TestNormalizeEvent_PreservesExplicitType`；`channelbot/core` 既有测试仍绿。

- [ ] **T202 消息编辑/撤回仓储能力**（P0｜依赖 T201｜M｜🟡）
  - **规格**：补齐「客户编辑消息」与「客户撤回消息」两个窄方法；不改动出站撤回语义。
  - **上下文**：`MessageHub` 已有 `RecalledAt *time.Time` + `RecallError string`（`model/ai_sales_champion.go:71-72`）；`MarkRecalled(ctx, id, at)`（`message_hub_recall.go:25`）**WHERE 带 `direction='outbound'`**，只用于撤我发出的消息；编辑方向的窄方法不存在。
  - **架构**：Repository 层新增两个方法，沿用现有 `GetByMsgID` 的租户/方向约定。
  - **数据结构**：
    ```go
    func (r *MessageHubRepository) UpdateContentByChannelMsgID(
        ctx context.Context, channel, accountID, msgID, content string) (bool, error)
    func (r *MessageHubRepository) MarkInboundRecalledByChannelMsgID(
        ctx context.Context, channel, accountID, msgID string, at time.Time, reason string) (bool, error)
    // MarkInboundRecalled 额外写 Extra.recall_source="platform"；RecallError 复用为原因列
    ```
  - **数据流向**：入站分流（T203）→ 对应仓储方法 → `message_hub` 行更新 → 返回是否命中。
  - **逻辑图**：
    ```mermaid
    flowchart TD
      A["message.edited"] --> B["UpdateContentByChannelMsgID"]
      C["message.deleted"] --> D["MarkInboundRecalledByChannelMsgID"]
      B --> E{"direction 任意，命中即更新"}
      D --> F{"direction='inbound'?"}
      F -->|否| G["不命中，返回 false"]
      F -->|是| H["写 recalled_at / recall_error / recall_source"]
    ```
  - **验收**：`TestMessageHub_UpdateContentByChannelMsgID`、`TestMessageHub_MarkInboundRecalled_RejectsOutboundRow`、`TestMessageHub_MarkInboundRecalled_NotFound`。

- [ ] **T203 入站按事件类型分流**（P0｜依赖 T201/T202｜M｜🟡）
  - **规格**：归一之后先分流，只有 `message.received` 走原「落库 + 触发 AI」路径；其余事件不得污染未读计数与 AI 触发。
  - **上下文**：`HandleIngressMessage`（`inbox_ingress.go:546`）目前无类型分支；`InboxIngressResult`（L129）字段为 `Accepted/HumanLocked/QueuedForAI/SessionID/Reason`。
  - **架构**：Service 层单一分流点；下游分别调用 T202 仓储、事件总线（T204）、交互 ack（T207）。
  - **数据结构**：
    ```go
    // 分流表
    // message.received      → 原路径
    // message.edited        → UpdateContentByChannelMsgID，不触发 AI
    // message.deleted       → MarkInboundRecalledByChannelMsgID，不触发 AI
    // message.reaction      → 只发事件总线
    // group.member_* / bot.*→ 只发事件总线，不建消息
    // interaction.callback  → T207 ack，不建消息
    ```
  - **数据流向**：`HandleIngressMessage` → `NormalizeEvent` → 分流 switch → {仓储 / 总线 / ack / 原路径}。
  - **逻辑图**：
    ```mermaid
    flowchart TD
      A["HandleIngressMessage"] --> B["NormalizeEvent"]
      B --> C{"EventType"}
      C -->|received| D["去重 → 人锁 → 落库 → triggerAI"]
      C -->|edited| E["UpdateContent"]
      C -->|deleted| F["MarkInboundRecalled"]
      C -->|reaction / member / bot| G["事件总线"]
      C -->|interaction.callback| H["ack"]
    ```
  - **验收**：`TestIngress_EditedUpdatesContent`、`TestIngress_DeletedMarksRecall`、`TestIngress_MemberEventDoesNotCreateMessage`、`TestIngress_NonReceivedDoesNotTouchUnread`。

- [ ] **T204 事件总线扩展**（P0｜依赖 T203｜S｜🟡）
  - **规格**：非消息类事件走既有事件总线，新增 4 个 topic；订阅方缺位时不得阻塞入站。
  - **上下文**：`internal/event/types.go` 现有 8 个 topic（`operation.log`、`customer.message.received`、`knowledge.document.changed`、`dialogue.started/ended`、`asset.degraded`、`asset.degrade.warning`、`rag.corpus.updated`）；`Bus` 提供 `Publish/PublishCtx/PublishEventCtx` 与 `StampTenantFromCtx`。
  - **架构**：`internal/event` 扩展；Service 层只调 `PublishEventCtx`。
  - **数据结构**：
    ```go
    const (
        TopicCustomerMessageEdited     = "customer.message.edited"
        TopicCustomerMessageDeleted    = "customer.message.deleted"
        TopicCustomerGroupMemberChange = "customer.group.member.changed"
        TopicCustomerInteractionCallback = "customer.interaction.callback"
    )
    type MessageLifecycleEvent struct {
        TenantID  string
        EventID   string
        Channel   string
        AccountID string
        MsgID     string
        EventType string
        OccurredAt time.Time
        Extra     map[string]any
    }
    ```
  - **数据流向**：分流点 → `PublishEventCtx(ctx, topic, payload)` → 订阅者（审计/统计）。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["分流点"] --> B["PublishEventCtx"]
      B --> C["Bus 队列"]
      C --> D["订阅者（审计/统计）"]
      C -.->|无订阅者| E["丢弃，不阻塞"]
    ```
  - **验收**：`TestEvent_AuditTopicDelivery`（订阅收到且租户正确）、`TestEvent_NoSubscriberDoesNotBlock`。

- [ ] **T205 各渠道解析器填 `EventType`**（P0｜依赖 T201｜M｜🟡）
  - **规格**：把各渠道原生事件映射成统一类型；映射不了的一律落到 `platform.specific` 语义（当前记录为 `message.received` 之外的显式值），**不得静默丢弃**。
  - **上下文**：`webhook_channel_{wecom,feishu,telegram,qq,whatsapp,douyin}.go` 只产出消息；feishu 在 `Event.Message == nil` 时返回 `nil,nil`（`webhook_channel_feishu.go:196`），撤回/成员事件被静默丢弃；telegram 仅 `EditedMessage` 分支（`webhook_channel_telegram.go:268`）。
  - **架构**：各渠道解析器（Service）内映射，只填 `EventType`，不改落库逻辑。
  - **数据结构**：
    ```go
    // 映射表（节选）
    // telegram: edited_message → message.edited; deleted_business_message → message.deleted
    //           message_reaction → message.reaction; new_chat_members → group.member_joined
    // feishu:   im.message.recalled_v1 → message.deleted; im.chat.member.* → group.member_*
    // qq:       GROUP_ADD_ROBOT → bot.invited_to_group; GROUP_DEL_ROBOT → bot.removed_from_group
    // whatsapp: statuses → 不产消息（走 StatusUpdate）
    ```
  - **数据流向**：平台原始 payload → 解析器 → `MessageEvent{EventType}` → `NormalizeEvent`。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["平台 payload"] --> B{"解析器映射"}
      B -->|已知| C["显式 EventType"]
      B -->|未知| D["platform.specific 语义，不丢弃"]
      C --> E["NormalizeEvent"]
      D --> E
    ```
  - **验收**：每渠道一条 fixture 断言 `EventType` 与「不产消息行」；`TestChannelParsers_EventTypeMatrix` 覆盖全表。

- [ ] **T206 `channelcaps.Manifest.Events`**（P0｜依赖 T205｜S｜🟡）
  - **规格**：能力表从「消息类型」升级到「事件类型」，并让「解析器能产出的事件」必须先在 manifest 声明。
  - **上下文**：`internal/channelcaps/channelcaps.go` 的 `Manifest{Channel,Label,Via,Inbound,Outbound,Features,Note}` 无事件字段；文档由 `Markdown()` 生成、`TestCapabilityDocFreshness` 钉漂移。
  - **架构**：`channelcaps` 为纯声明包（无 service 依赖）；解析器通过测试反向校验声明。
  - **数据结构**：
    ```go
    type Manifest struct {
        // 既有字段...
        Events []string `json:"events"` // 事件类型集合，须取自 T201 常量
    }
    ```
  - **数据流向**：Go 声明 → `Markdown()` → `docs/channel-capabilities.md`（`UPDATE_GOLDEN=1` 刷新）。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["manifests.go"] --> B["Markdown()"]
      B --> C["docs/channel-capabilities.md"]
      B --> D["TestCapabilityDocFreshness"]
      E["解析器事件字符串"] --> F["TestDeclaredEventsCoverParser"]
      A --> F
    ```
  - **验收**：`go test ./internal/channelcaps/` 绿；`UPDATE_GOLDEN=1` 后文档 diff 仅新增事件列。

- [ ] **T207 交互回调最小闭环**（P1｜依赖 T203｜M｜🟡）
  - **规格**：按钮点击类回调必须回 ack（否则客户端转圈/提示失败），但不创建消息、不触发 AI；其余复杂交互留后续 spec。
  - **上下文**：BotMgr 侧 Telegram 用 `answerCallbackQuery`，失效时回文本提示（LangBot 深度剖析 §case2）；HiveMTK 当前无 `interaction.callback` 处理。
  - **架构**：Service 层新增回调处理分支；渠道差异封装在 `channelbot/<x>` 的 `AnswerCallback`。
  - **数据结构**：
    ```go
    type InteractionCallback struct {
        CallbackID string
        AccountID  string
        UserID     string
        Data       string
        Channel    string
    }
    ```
  - **数据流向**：`interaction.callback` → 渠道 `AnswerCallback` → 事件总线（审计）→ 返回 ack 结果。
  - **逻辑图**：
    ```mermaid
    sequenceDiagram
      participant P as 平台
      participant I as Ingress
      participant C as channelbot
      P->>I: callback_query
      I->>C: AnswerCallback(callbackID)
      C-->>P: ack
      I->>I: 发审计事件，不建消息
    ```
  - **验收**：`TestInteractionCallback_AcksAndDoesNotReply`（断言 ack 被调用、消息表零新增、无 AI 触发）。

---

## Phase 3 事件路由（P0）

- [ ] **T301 规则模型 + 迁移 066**（P0｜依赖 T201｜M｜🟡）
  - **规格**：用一张规则表表达「什么事件、命中什么条件、交给谁」；无规则时行为与今天完全一致。
  - **上下文**：现状是硬编码「入站即触发 AI」（`webhook_ai.go:63` `shouldTriggerAI`，无账号级开关的渠道 default 返回 true）。
  - **架构**：Model 层新增实体，必须登记 `internal/pkg/db/migrate.go` 的 `allModels()`，否则 `check_model_migration.py` 红。
  - **数据结构**：
    ```go
    type ChannelRouteRule struct {
        TenantID  string
        ID        uint
        Channel   string // 空 = 全渠道
        AccountID string // 空 = 全账号
        EventType string // 支持通配：* / message.*
        Match     string // JSON：{"extra.x":"v","extra.y":{"$in":["a","b"]}}
        Target    string // discard | pipeline | agent | plugin
        TargetRef string
        Priority  int    // 升序，首个命中生效
        Enabled   bool
        Note      string
    }
    ```
    迁移 `066_channel_route_rules.sql`：`CREATE TABLE IF NOT EXISTS` + 索引 `(tenant_id, channel, enabled, priority)`。
  - **数据流向**：管理 API（T305）→ 表；引擎（T303）读表 → 决策（T304）。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["管理 API"] --> B["channel_route_rules"]
      B --> C["RouteEngine.Resolve"]
      C --> D["RouteDecision"]
    ```
  - **验收**：`python3 scripts/check_model_migration.py` 绿；迁移在空库/有库各跑一次无错。

- [ ] **T302 仓储**（P0｜依赖 T301｜S｜🟡）
  - **规格**：规则读写全部以 tenantID 为第一参，fail-closed；不提供跨租户批量接口。
  - **上下文**：项目惯例见 `repository/message_hub_inbox_conversation.go` 等（租户参数前置、无 ctx 泄漏）。
  - **架构**：Repository 层；不得被 Controller 直接调用。
  - **数据结构**：
    ```go
    func (r *ChannelRouteRuleRepository) ListByTenant(ctx, tenantID string) ([]model.ChannelRouteRule, error)
    func (r *ChannelRouteRuleRepository) ListEnabled(ctx, tenantID, channel, accountID string) ([]model.ChannelRouteRule, error)
    func (r *ChannelRouteRuleRepository) Upsert(ctx, rule *model.ChannelRouteRule) error
    func (r *ChannelRouteRuleRepository) Delete(ctx, tenantID string, id uint) error
    ```
  - **数据流向**：Service → Repository → 表；所有查询带 `tenant_id = ?`。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["Service"] --> B["Repository(tenantID 前置)"]
      B --> C[("channel_route_rules")]
    ```
  - **验收**：`TestChannelRouteRule_TenantIsolation`（A 租户读不到 B 租户规则）。

- [ ] **T303 匹配引擎**（P0｜依赖 T302｜M｜🟡）
  - **规格**：纯函数式决策：事件类型通配 + `Extra` 嵌套路径匹配 + 优先级排序；无命中返回 `(nil, nil)` 表示放行。
  - **上下文**：可参照 LangBot 的 `_match_event_pattern` / `_match_event_filter`（深度剖析 §L3）——通配与嵌套取值分离，非法规则跳过而不报错。
  - **架构**：Service 层纯逻辑（可单测，无 IO）；规则由调用方注入。
  - **数据结构**：
    ```go
    type RouteDecision struct { RuleID uint; Target string; TargetRef string }
    // Match JSON 语法：等值 "extra.a":"v"；集合 {"$in":[...]}；缺省字段 = 不约束
    ```
  - **数据流向**：`Resolve(ctx, tenantID, evt)` → 取规则 → 逐条匹配 → 首个命中 → `RouteDecision`。
  - **逻辑图**：
    ```mermaid
    flowchart TD
      A["Resolve"] --> B["ListEnabled(channel, account)"]
      B --> C["按 Priority 升序遍历"]
      C --> D{"EventType 通配匹配?"}
      D -->|否| C
      D -->|是| E{"Match 全部满足?"}
      E -->|否| C
      E -->|是| F["返回 RouteDecision"]
      C -->|遍历完| G["返回 nil, nil（放行）"]
    ```
  - **验收**：`TestRouteEngine_Wildcard`、`TestRouteEngine_NestedMatch`、`TestRouteEngine_PriorityOrder`、`TestRouteEngine_NoMatchReturnsNil`、`TestRouteEngine_InvalidJSONRuleSkipped`。

- [ ] **T304 接线 + 可观测**（P0｜依赖 T303/T203｜S｜🟡）
  - **规格**：在归一后、`triggerAIForEvent` 前决策；`discard` 不入库不回复但必须留审计；`IngressResult` 记录命中规则。
  - **上下文**：`HandleIngressMessage`（`inbox_ingress.go:546`）→ `triggerAIForEvent`（L709）；`InboxIngressResult`（L129）需加字段。
  - **架构**：Service 层单点接线；审计走 `operation.log`。
  - **数据结构**：
    ```go
    type InboxIngressResult struct {
        // 既有：Accepted / HumanLocked / QueuedForAI / SessionID / Reason
        RouteRuleID uint
    }
    ```
  - **数据流向**：`NormalizeEvent` → `Resolve` → {discard: 审计并返回 / 放行: 原路径}。
  - **逻辑图**：
    ```mermaid
    flowchart TD
      A["NormalizeEvent"] --> B["Resolve"]
      B -->|nil| C["原路径"]
      B -->|discard| D["写 operation.log，返回 Accepted=false"]
      B -->|pipeline/agent/plugin| E["按 TargetRef 分发"]
    ```
  - **验收**：`TestIngress_DiscardRuleStopsPipeline`、`TestIngress_RouteRuleIDRecorded`、`TestIngress_NoRuleKeepsLegacyBehavior`。

- [ ] **T305 规则管理 API**（P1｜依赖 T302｜M｜🟡）
  - **规格**：租户内 CRUD；所有路由必须过权限与租户扫描门禁。
  - **上下文**：路由按域拆分 `internal/router/setup<X>Routes`，在 `router.go` 的 `auth` 组注册；`route_policy_gate_test.go` 与 `cross_tenant_scan_test.go` 会强制检查。
  - **架构**：Router → Controller → Service → Repository，严格五层。
  - **数据结构**：
    ```go
    // GET    /api/channel-route-rules        -> []ChannelRouteRule
    // POST   /api/channel-route-rules
    // PUT    /api/channel-route-rules/:id
    // DELETE /api/channel-route-rules/:id
    // 权限：channel_routes.read / channel_routes.write
    ```
  - **数据流向**：HTTP → Controller（取 tenant）→ Service 校验 → Repository → 审计。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["HTTP"] --> B["RequirePermission"]
      B --> C["Controller"]
      C --> D["Service(校验 + 审计)"]
      D --> E["Repository"]
    ```
  - **验收**：`go test ./internal/controller/ -run TestChannelRouteRuleAPI`（含跨租户 404）；`go test ./internal/router/ -run 'RoutePolicy|CrossTenant'` 绿。

---

## Phase 4 渠道渲染层（P1）

- [ ] **T401 能力表补渲染字段**（P1｜依赖 —｜S｜🟡）
  - **规格**：把「该渠道要什么格式、能吞多长」变成声明式数据，消灭散落的 `if channel == ...`。
  - **上下文**：现状渲染规则散在三处——`channel_card_delivery.go`（`outboundSupportsRichCards` L16 / `cardTextDelivery` L29，**在制品未提交**）、`bridge_outbound_markdown.go`（L58/L93）、`webhook_outbound.go`（`htmlEscape*` L1432/1446）。
  - **架构**：`channelcaps` 纯声明包；不引入 service 依赖。
  - **数据结构**：
    ```go
    type Manifest struct {
        // ...
        OutboundFormat string // plain | markdown | html
        MaxTextLen     int
    }
    ```
  - **数据流向**：声明 → `RenderForChannel`（T402）→ sender。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["manifests.go 声明"] --> B["RenderForChannel"]
      B --> C["sender 拿到已渲染内容"]
    ```
  - **验收**：`TestManifestRenderFieldsNonEmpty`；文档 diff 仅新增两列。

- [ ] **T402 `RenderForChannel`**（P1｜依赖 T401｜M｜🟡）
  - **规格**：一次调用完成「格式转换 + 富卡能力判定 + 超长拆块」；未知渠道返回错误而非静默透传。
  - **上下文**：拆块上限与 Telegram 4096 / 微信 2048 等平台限制对齐；卡片降级文本能力来自 `cardTextDelivery`。
  - **架构**：Service 层纯函数（可单测）；不读写 DB。
  - **数据结构**：
    ```go
    type RenderedOutbound struct { Text string; Cards []model.RichCard; Chunks []string }
    func RenderForChannel(ch string, msgType string, content string, cards []model.RichCard) (RenderedOutbound, error)
    ```
  - **数据流向**：`sendOutbound` → `RenderForChannel` → `OutboundRequest{Content, Cards}`。
  - **逻辑图**：
    ```mermaid
    flowchart TD
      A["输入 content + cards"] --> B{"格式=plain?"}
      B -->|是| C["去 Markdown"]
      B -->|否| D["保留"]
      C --> E{"支持富卡?"}
      D --> E
      E -->|否| F["卡片降级为文本"]
      E -->|是| G["保留卡片"]
      F --> H{"超 MaxTextLen?"}
      G --> H
      H -->|是| I["切块 ≤3 段"]
      H -->|否| J["原样"]
    ```
  - **验收**：`TestRender_PlainStripsMarkdown`、`TestRender_CardFoldedWhenUnsupported`、`TestRender_SplitsLongText`、`TestRender_UnknownChannelErrors`。

- [ ] **T403 旧函数薄包装**（P1｜依赖 T402｜S｜🟡）
  - **规格**：保留旧签名避免大面积改动，内部改调渲染层；`channel_card_delivery.go` 文件本身不动（在制品）。
  - **上下文**：调用点分布在 `webhook_outbound.go` 与测试中。
  - **架构**：Service 层适配器。
  - **数据结构**：
    ```go
    // 签名不变：
    // outboundSupportsRichCards(ch WebhookChannel) bool
    // cardTextDelivery(content string, cards []model.RichCard) (string, error)
    // stripMarkdownForBridgeChannel(ch, msgType) bool
    ```
  - **数据流向**：旧调用点 → 包装函数 → `RenderForChannel`。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["旧调用点"] --> B["薄包装"]
      B --> C["RenderForChannel"]
    ```
  - **验收**：`go test ./internal/service/ -run 'Markdown|CardDelivery'` 全绿。

- [ ] **T404 sender 统一走渲染**（P1｜依赖 T402/T403/T101｜M｜🟡）
  - **规格**：渲染只在 `sendOutbound` 做一次，sender 内不再清洗/转义；各 sender 删除内联转换。
  - **上下文**：T109 已把桥接清洗上移；本任务收掉 telegram `htmlEscape*` 等残留。
  - **架构**：横切段统一；sender 只做协议载荷。
  - **数据结构**：
    ```go
    // OutboundRequest.Content 语义变更：已是目标渠道格式（不可再二次转义）
    ```
  - **数据流向**：`sendOutbound` → 渲染 → sender → 平台。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["sendOutbound"] --> B["RenderForChannel（唯一一次）"]
      B --> C["sender：只拼协议载荷"]
      C --> D["平台"]
    ```
  - **验收**：T112 golden 更新为「渲染后内容」并逐渠道人工确认；`TestTelegramSender_GroupMention` 仍绿。

---

## Phase 5 身份键（P1）

- [ ] **T501 去重与换键迁移 065**（P1｜依赖 —｜L｜🟡）
  - **规格**：把 `customer_channels` 唯一键从 `(one_id, channel)` 换成 `(tenant_id, one_id, channel, account_id)`；重复行按最近活跃保留一条，其余软删。
  - **上下文**：`model/customer_channel.go` 现唯一键仅 `idx_oneid_channel(one_id, channel)`，`account_id` 只是普通索引——同一客户在两个公众号/两个企微账号下会互相覆盖（Chatwoot 用 `(inbox_id, source_id)` 避免此类问题）。
  - **架构**：迁移脚本（离线 SQL 部署路径）+ Model tag；不改业务逻辑。
  - **数据结构**：
    ```sql
    -- 065_customer_channel_account_key.sql
    -- 1) account_id 空值统一 ''
    -- 2) 窗口函数按 (tenant_id, one_id, channel, account_id) 分组，last_seen_at DESC 保留 rn=1，其余 deleted_at = now()
    -- 3) CREATE UNIQUE INDEX CONCURRENTLY idx_tenant_oneid_channel_account ...
    -- 4) DROP INDEX CONCURRENTLY idx_oneid_channel
    ```
  - **数据流向**：迁移 → 表结构；应用侧 `ON CONFLICT` 目标随之改变（T502）。
  - **逻辑图**：
    ```mermaid
    flowchart TD
      A["统计重复组"] --> B["窗口函数标记 rn"]
      B -->|rn>1| C["软删"]
      B -->|rn=1| D["保留"]
      C --> E["建新唯一索引 CONCURRENTLY"]
      D --> E
      E --> F["删旧索引 CONCURRENTLY"]
    ```
  - **验收**：含重复数据的测试库连跑两次无错；去重后行数 = 新键 `GROUP BY` 组数；`check_model_migration.py` 绿。

- [ ] **T502 模型与写入路径**（P1｜依赖 T501｜M｜🟡）
  - **规格**：gorm tag 与新键一致；写入用 `ON CONFLICT (tenant_id, one_id, channel, account_id)`。
  - **上下文**：`UpsertByChannelAccount` 现按旧键冲突更新。
  - **架构**：Model + Repository。
  - **数据结构**：
    ```go
    AccountID string `gorm:"type:varchar(64);not null;default:'';uniqueIndex:idx_tenant_oneid_channel_account,priority:4"`
    ```
  - **数据流向**：入站身份识别 → `UpsertByChannelAccount` → 冲突更新。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["入站事件"] --> B["UpsertByChannelAccount"]
      B --> C{"冲突键命中?"}
      C -->|是| D["DO UPDATE"]
      C -->|否| E["INSERT"]
    ```
  - **验收**：`TestCustomerChannel_SameOneIDTwoAccounts`、`TestCustomerChannel_UpsertIdempotent`、`TestCustomerChannel_EmptyAccountNormalized`。

---

## Phase 6 生命周期读侧（P1）

- [ ] **T601 `snoozed` 状态 + 迁移 067**（P1｜依赖 —｜M｜🟡）
  - **规格**：补齐「稍后处理」状态与到期字段；不改现有 `unread/open/assigned/closed` 语义。
  - **上下文**：状态是 `varchar`（非 pg enum，无需改 047），但查询层 `CASE` 映射与 `closed` 过滤在 `repository/message_hub_inbox_conversation_query.go`，必须同步。
  - **架构**：Model + 迁移 + 查询层。
  - **数据结构**：
    ```go
    const InboxStatusSnoozed = "snoozed"
    SnoozedUntil *time.Time `gorm:"index"`
    ReopenCount  int        `gorm:"not null;default:0"`
    ```
    迁移 `067_inbox_conversation_snooze.sql`：`ADD COLUMN IF NOT EXISTS` × 2 + `CREATE INDEX IF NOT EXISTS (tenant_id, snoozed_until)`。
  - **数据流向**：会话操作 → 置 `snoozed` + 到期时间 → 到期 worker（T602）→ 回到 `open`。
  - **逻辑图**：
    ```mermaid
    stateDiagram-v2
      [*] --> unread
      unread --> open
      open --> assigned
      open --> snoozed
      assigned --> snoozed
      snoozed --> open: 到期
      open --> closed
      assigned --> closed
      closed --> open: 新入站重开
    ```
  - **验收**：迁移幂等（跑两次无错）；`TestInboxConversation_SnoozeSetsUntil`；`TestInboxQuery_SnoozedNotInUnreadCount`。

- [ ] **T602 到期重置 worker**（P1｜依赖 T601｜S｜🟡）
  - **规格**：定时把到期 `snoozed` 会话改回 `open`，并发安全（条件更新），可启停。
  - **上下文**：无通用调度器，但存在 `*_cron.go` 形态（`retention_cleanup_cron.go`、`message_trace_cleanup_cron.go`：Ticker + `utils.SafeGo` + 成对启停）；`check-naked-goroutine.sh` 强制 SafeGo。
  - **架构**：Service 层后台任务。
  - **数据结构**：
    ```go
    type inboxSnoozeCron struct { repo *repository.InboxConversationRepository; interval time.Duration }
    ```
  - **数据流向**：Ticker → 条件 UPDATE → 返回受影响行数 → 日志。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["Ticker"] --> B["UPDATE ... WHERE status='snoozed' AND snoozed_until<=now()"]
      B --> C{"rows>0?"}
      C -->|是| D["记日志"]
      C -->|否| E["空转"]
    ```
  - **验收**：`TestSnoozeExpiry_ReopensOnce`（并发两次只重置一次）；`check-naked-goroutine.sh` 绿。

- [ ] **T603 重开计数**（P1｜依赖 T601｜S｜🟡）
  - **规格**：每次「已关闭 → 因新入站重开」计数 +1，用于识别反复回来的客户。
  - **上下文**：`repository/message_hub_inbox_conversation.go` 已有重开分支（closed→unread，L332-336）。
  - **架构**：Repository。
  - **数据结构**：
    ```go
    reopen_count = reopen_count + 1 // gorm.Expr
    ```
  - **数据流向**：`UpsertFromMessage` → 命中 closed 分支 → `reopen_count+1`。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["UpsertFromMessage"] --> B{"原状态=closed?"}
      B -->|是| C["status=unread; reopen_count+1"]
      B -->|否| D["原逻辑"]
    ```
  - **验收**：`TestInboxConversation_ReopenIncrementsCount`。

- [ ] **T604 待首响查询 + API**（P1｜依赖 T601｜M｜🟡）
  - **规格**：提供「客户在等、我们还没回」的筛选，按等待时长升序；已首响或已关闭不计入。
  - **上下文**：`waiting_since` / `first_reply_at` 已在 `2f658e7b` 写入（`UpsertFromMessage` 方向收口）；查询层缺 filter。
  - **架构**：Repository 查询 + Controller 透传。
  - **数据结构**：
    ```sql
    WHERE waiting_since IS NOT NULL AND first_reply_at IS NULL AND closed_at IS NULL
    ORDER BY waiting_since ASC
    ```
  - **数据流向**：HTTP `filter=awaiting_first_reply` → Controller → Repository → 列表。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["GET /inbox?filter=awaiting_first_reply"] --> B["Controller"]
      B --> C["Repository 条件查询"]
      C --> D["按 waiting_since 升序"]
    ```
  - **验收**：`TestInboxQuery_AwaitingFirstReply`（已首响/已关闭样本不计入）、`TestInboxAPI_FilterAwaitingFirstReply`。

- [ ] **T605 首响时长指标**（P2｜依赖 T604｜M｜🟡）
  - **规格**：只读聚合首响时长（p50/p95），按渠道/租户/时间窗；无首响样本排除。
  - **上下文**：Chatwoot 用 `first_reply_created_at` 与 `waiting_since` 计算并广播，本项目只做读侧聚合，不上实时看板。
  - **架构**：Repository 聚合查询 + Controller。
  - **数据结构**：
    ```sql
    percentile_cont(0.5) WITHIN GROUP (ORDER BY EXTRACT(EPOCH FROM (first_reply_at - waiting_since)))
    ```
  - **数据流向**：HTTP 查询 → 聚合 → JSON。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["查询请求"] --> B["按渠道/时间窗过滤"]
      B --> C["计算 p50/p95"]
      C --> D["返回"]
    ```
  - **验收**：`TestFirstReplyMetrics_ExcludesUnanswered`。

- [ ] **T606 前端筛选与展示**（P2｜依赖 T604｜M｜🟡）
  - **规格**：收件箱列表新增「待首响」筛选与等待时长徽标。
  - **上下文**：`user-web` 收件箱页；规则 -1 要求逐元素人工点验并留证据。
  - **架构**：前端页面 + API 调用；不改后端契约。
  - **数据结构**：
    ```ts
    type InboxFilter = 'all' | 'unread' | 'awaiting_first_reply'
    // 徽标：now - waiting_since
    ```
  - **数据流向**：筛选状态 → 请求参数 → 列表渲染。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["筛选器"] --> B["query param"]
      B --> C["列表接口"]
      C --> D["行内徽标"]
    ```
  - **验收**：`npm run build` 绿；页面逐元素点验（截图/快照留证）。

---

## Phase 7 企业微信客服 wecomcs（P0 首个新平台，验证接缝）

- [ ] **T701 `channelbot/wecomcs` 协议客户端**（P0｜依赖 T101｜L｜🟡）
  - **规格**：纯协议层：取 token（带缓存）、增量拉消息、发消息、验签。不依赖 service/repository。
  - **上下文**：现有 `channelbot/{telegram,whatsapp,qq}` 都是「构造 + `core.BaseClient` + 错误分类」的形态；`core.SecureEqual` 提供常量时间比较。
  - **架构**：`internal/channelbot/wecomcs`，仅依赖 `channelbot/core`；错误统一 `*wecomcs.Error`（含 retcode/errmsg）。
  - **数据结构**：
    ```go
    type Client struct{ *core.BaseClient }
    type TokenResp struct{ ErrCode int; ErrMsg string; AccessToken string; ExpiresIn int }
    type SyncMsgReq struct{ Cursor, Token, OpenKfID string; Limit int }
    type SyncMsgResp struct{ ErrCode int; NextCursor string; HasMore int; MsgList []SyncMsg }
    type SyncMsg struct{ MsgID, OpenKfID, ExternalUserID, SendTime, Origin int; MsgType string; Text *TextBody }
    func (c *Client) GetToken(ctx context.Context, corpID, secret string) (string, error)
    func (c *Client) SyncMsg(ctx context.Context, req SyncMsgReq) (*SyncMsgResp, error)
    func (c *Client) SendMsg(ctx context.Context, token string, req SendMsgReq) (string, error)
    func VerifySignature(signature, timestamp, nonce, echostr string) bool
    ```
  - **数据流向**：调用方 → Client → 企微 API → 解析 → 返回结构体或 `*Error`。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["调用方"] --> B["Client.GetToken（缓存 5min 余量）"]
      B --> C["SyncMsg（cursor 透传）"]
      C --> D["SendMsg"]
      C --> E["VerifySignature"]
    ```
  - **验收**：`channelbot/wecomcs/wecomcs_test.go` 用 `httptest` 回放：token 二次调用只请求一次、`SyncMsg` 游标原样透传、验签失败返回 false、`retcode != 0` 返回可判定错误。

- [ ] **T702 入站 handler**（P0｜依赖 T701｜L｜🟡）
  - **规格**：回调验签解密 → 拉增量 → 转 `MessageEvent` → 交 `Ingress`；非消息事件走事件总线；同 `msgid` 二次投递不重复入库。
  - **上下文**：仿 `service/webhook_channel_qq.go`（153 行，Op13 challenge 短路 + `ToInbound` + `Ingress` 模板）。
  - **架构**：`service/webhook_channel_wecomcs.go`，实现与其它渠道一致的 `dispatch<X>` + `Verify<X>`。
  - **数据结构**：
    ```go
    // 解密后关键字段：Token / OpenKfId / msgtype / msgid / send_time
    // 事件映射：enter_session / msg_send_fail / session_status_change / reject_customer_msg_switch_change
    ```
  - **数据流向**：回调 → 验签 → 解密 → `SyncMsg(cursor)` → 逐条 `ToInbound` → `Ingress.HandleIngressMessage`。
  - **逻辑图**：
    ```mermaid
    flowchart TD
      A["企微回调"] --> B["VerifySignature"]
      B -->|失败| C["4xx，不落库"]
      B -->|成功| D["解密取 Token/OpenKfId"]
      D --> E["SyncMsg 增量"]
      E --> F{"msgtype"}
      F -->|text/image| G["ToInbound → Ingress"]
      F -->|event| H["事件总线"]
    ```
  - **验收**：fixture 回放（文本/图片/事件）；`TestWecomcs_DuplicateCallbackIdempotent`。

- [ ] **T703 五处登记 + 账号表**（P0｜依赖 T702｜M｜🟡）
  - **规格**：让 webhook 通用链路认得这个渠道：能力判定、拒绝提示、分发、验签、租户解析表；账号表独立（不复用企微应用表）。
  - **上下文**：`webhook.go`：`webhookInboundCapable`(L349)、`webhookInboundRejectHints`(L358-372)、`Verify`(L663)、`dispatchToChannel`(L1130)、`webhookAccountTableByChannel`(L393)；`model/wecom.go:8` 的 `WeComAccount` 无 `kf_token/kf_aes_key/open_kfid`。
  - **架构**：Service 登记 + Model + 迁移 068。
  - **数据结构**：
    ```go
    type WeComCSAccount struct {
        TenantID string
        ID uint
        CorpID, Secret, OpenKfID string
        Token, AESKey string
        AIAgentEnabled bool
        Status int
    } // 表 wecom_cs_accounts（迁移 068_wecom_cs_accounts.sql）
    ```
  - **数据流向**：回调 URL `account_id` → `ResolveTenantByChannelAccount`（查 `wecom_cs_accounts`）→ 失败 404 fail-closed。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["/api/webhook/wecomcs/:id"] --> B["webhookInboundCapable"]
      B --> C["ResolveTenantByChannelAccount → wecom_cs_accounts"]
      C --> D{"命中?"}
      D -->|否| E["404 fail-closed"]
      D -->|是| F["dispatchToChannel"]
    ```
  - **验收**：`TestWebhookCapability_WecomcsAccepted`、`TestWebhookDispatch_Wecomcs`、租户解析失败 4xx 且不落库、`bash scripts/check-enum-consistency.sh` 绿（含 047 + 063 式增量）。

- [ ] **T704 出站 sender**（P0｜依赖 T701/T703｜M｜🟡）
  - **规格**：支持 text/image/link/miniprogram；被动窗口过期返回可判定错误，不静默重试。
  - **上下文**：企微客服被动回复有 48h 限制（超出需主动消息额度），与本项目 `ChannelError` 体系对齐。
  - **架构**：`service/outbound_sender_wecomcs.go`，注册进 T101 注册表。
  - **数据结构**：
    ```go
    type SendMsgReq struct {
        Touser, OpenKfID, MsgType string
        Text *struct{ Content string } `json:"text,omitempty"`
        Image *struct{ MediaID string } `json:"image,omitempty"`
        Link  *struct{ Title, Desc, URL, PicURL string } `json:"link,omitempty"`
    }
    ```
  - **数据流向**：`sendOutbound` → `wecomcsSender.Send` → `Client.SendMsg` → 成功回 `PlatformMsgID`。
  - **逻辑图**：
    ```mermaid
    flowchart TD
      A["Send"] --> B{"窗口内?"}
      B -->|是| C["SendMsg（被动）"]
      B -->|否| D["返回 ChannelError{window_expired}"]
      C --> E{"retcode == 0?"}
      E -->|是| F["Sent + PlatformMsgID"]
      E -->|否| G["Fail(ChannelError)"]
    ```
  - **验收**：`TestWecomcsSender_PassiveVsActive`、`TestWecomcsSender_OutOfWindowReturnsChannelError`。

- [ ] **T705 能力表登记**（P0｜依赖 T704｜S｜🟡）
  - **规格**：新渠道在四处声明齐全：`WebhookChannel` 常量、`model.Channel` 常量、`channelcaps` manifest、`capregistry` 条目。
  - **上下文**：`ChannelType*` 常量与 `047` 枚举由 `check-enum-consistency.sh` 对齐；`channelcaps` 文档由测试生成。
  - **架构**：Model + 声明包。
  - **数据结构**：
    ```go
    channelcaps.Manifest{
      Channel: "wecomcs", Label: "企业微信客服", Via: "webhook",
      Events: []string{EventTypeMessageReceived, EventTypeMessageDeleted, /* 客服专属事件 */},
      Features: []channelcaps.Feature{FeatureGroup},
    }
    ```
  - **数据流向**：声明 → `Markdown()` → `docs/channel-capabilities.md`。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["channelcaps.Register"] --> B["Markdown()"]
      B --> C["docs/channel-capabilities.md"]
    ```
  - **验收**：`TestEveryChannelConstantHasManifest` 绿；能力表新行 `via=webhook`。

- [ ] **T706 全链路测试**（P0｜依赖 T701–T705｜M｜🟡）
  - **规格**：端到端钉住五条不变量：验签失败不落库、同 msgid 幂等、单聊/群聊各一条、未授权租户 403/404、出站窗口错误分类。
  - **上下文**：参照 `internal/controller/webhook_batchc_d03_http_test.go` 的 HTTP 层测试形态。
  - **架构**：测试覆盖 Controller → Service → Repository（真实 PG，DB-gated）。
  - **数据结构**：
    ```go
    // fixture: testdata/wecomcs/{text.json,image.json,event.json}
    ```
  - **数据流向**：HTTP 请求 → 回调 → 落库 → 断言表行数与回复调用。
  - **逻辑图**：
    ```mermaid
    sequenceDiagram
      participant T as 测试
      participant C as Controller
      participant S as Service
      participant R as Repository
      T->>C: POST 回调（签名）
      C->>S: dispatch
      S->>R: 写 message_hub
      S-->>T: 202
      T->>R: 断言行数=1（重投仍 1）
    ```
  - **验收**：`go test ./internal/service/ -run Wecomcs`、`go test ./internal/controller/ -run Wecomcs` 全绿。

- [ ] **T707 真实账号验收**（P0｜依赖 T706｜S｜🔵）
  - **规格**：真实企微客服账号跑通「客户发消息 → 收件箱可见 → AI 回复送达」。
  - **上下文**：需要企业微信后台创建客服账号、配置可信回调域名（外网可达）。
  - **架构**：不涉及代码改动，只验收与记录。
  - **数据结构**：
    ```text
    记录：account_id / open_kfid / 客户 external_userid / 消息 id / 回复 id / 时间戳
    ```
  - **数据流向**：企微 → 回调 → 入库 → AI → 出站 → 企微。
  - **逻辑图**：
    ```mermaid
    sequenceDiagram
      participant U as 客户
      participant W as 企微
      participant H as HiveMTK
      U->>W: 发消息
      W->>H: 回调
      H->>H: 入库 + AI
      H->>W: SendMsg
      W->>U: 收到回复
    ```
  - **验收**：收件箱出现会话；回复在企微客户端可见；记录消息 id 备查。

---

## Phase 8 OneBot v11（P0）

- [ ] **T801 `channelbot/onebot` 客户端**（P0｜依赖 T101｜L｜🟡）
  - **规格**：支持 HTTP 上报/调用 + token 校验，覆盖四类 `post_type`；只做协议，不含业务。
  - **上下文**：`qq` 包是 QQ 官方机器人（`bot.q.qq.com`，app access token + Ed25519），与 OneBot（NapCat/Lagrange）字段语义完全不同，不能复用；需独立账号表。
  - **架构**：`internal/channelbot/onebot`，仅依赖 `core.BaseClient`。
  - **数据结构**：
    ```go
    type Event struct {
        PostType string `json:"post_type"` // message | notice | request | meta_event
        MessageType string `json:"message_type"` // private | group
        SubType, Time int64
        SelfID, UserID, GroupID int64
        MessageID int64
        RawMessage string
        Message []Segment
        NoticeType, RequestType, MetaEventType string
    }
    type apiResp struct{ Status string; RetCode int; Data json.RawMessage; Echo string }
    func (c *Client) SendPrivateMsg(ctx, selfID, userID int64, msg string) (int64, error)
    func (c *Client) SendGroupMsg(ctx, selfID, groupID int64, msg string) (int64, error)
    ```
  - **数据流向**：入站：上报 JSON → `ParseEvent` → Event；出站：调用 API → `echo` 关联 → 取 `data.message_id`。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["NapCat 上报"] --> B["ParseEvent"]
      B --> C["MessageEvent"]
      D["SendGroupMsg"] --> E["echo 匹配"]
      E --> F["message_id"]
    ```
  - **验收**：`httptest` 回放四种 `post_type`；`echo` 匹配到请求；token 缺失/错误返回可判定错误；`TestOnebot_ParseEventMatrix`。

- [ ] **T802 反向 WS 接入**（P0｜依赖 T801｜L｜🟡）
  - **规格**：提供 `/ws/onebot/:account_id` 让 NapCat 反连；维护连接表；发送优先走 WS，无连接回落 HTTP。
  - **上下文**：`internal/channelgw` 已有 WS 升级与 ping/pong 机制，可复用其帧与连接管理形态。
  - **架构**：Router 入口 + Service 连接管理；不跨层访问 repository。
  - **数据结构**：
    ```go
    type onebotConn struct{ accountID string; conn *websocket.Conn; sendCh chan []byte }
    // 全局：map[string]*onebotConn + RWMutex
    ```
  - **数据流向**：WS 帧 → 解析 → 入站；出站 API 调用 → 若连接在则写 WS，否则 HTTP。
  - **逻辑图**：
    ```mermaid
    flowchart TD
      A["NapCat 反连 /ws/onebot/:id"] --> B["登记连接"]
      B --> C["收帧 → ParseEvent → 入站"]
      D["出站请求"] --> E{"连接存在?"}
      E -->|是| F["写 WS"]
      E -->|否| G["HTTP 调用"]
      F --> H["断线移除连接"]
    ```
  - **验收**：`TestOnebotWS_ConnectSendReceive`、`TestOnebotWS_ReconnectReplacesConn`。

- [ ] **T803 入站 handler + 登记**（P0｜依赖 T801/T802｜L｜🟡）
  - **规格**：message 入站；notice 映射撤回与成员变动；request 只发事件不自动同意。
  - **上下文**：`service/webhook_channel_qq.go` 是渠道模板（`ToInbound` + `Ingress` + 媒体异步持久化）。
  - **架构**：`service/webhook_channel_onebot.go` + 五处登记（能力/提示/分发/验签/账号表）。
  - **数据结构**：
    ```go
    // post_type 映射
    // message(private/group) → message.received
    // notice(group_recall)  → message.deleted
    // notice(group_increase/decrease) → group.member_joined/left
    // request(friend/group) → 事件总线（不自动同意）
    ```
  - **数据流向**：WS/HTTP 上报 → 映射 → `Ingress` 或事件总线。
  - **逻辑图**：
    ```mermaid
    flowchart TD
      A["上报事件"] --> B{"post_type"}
      B -->|message| C["Ingress(EventType=received)"]
      B -->|notice.group_recall| D["Ingress(EventType=deleted)"]
      B -->|notice.member| E["事件总线"]
      B -->|request| F["事件总线，不自动同意"]
    ```
  - **验收**：`TestOnebot_InboundPrivateAndGroup`、`TestOnebot_RecallMapsToMessageDeleted`、`TestOnebot_RequestDoesNotCreateMessage`。

- [ ] **T804 出站 sender**（P0｜依赖 T803｜M｜🟡）
  - **规格**：私聊/群聊选择正确；CQ 码转义防注入；长文切块复用渲染层。
  - **上下文**：OneBot 消息支持 CQ 码，正文里出现 `[CQ:` 会被解析为控制指令，必须转义。
  - **架构**：`service/outbound_sender_onebot.go`，注册进 T101。
  - **数据结构**：
    ```go
    // 转义：'[' → '&#91;'，']' → '&#93;'，'&' → '&amp;'
    ```
  - **数据流向**：`sendOutbound` → 渲染（切块）→ sender → `SendGroupMsg/SendPrivateMsg`。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["Send"] --> B{"IsGroup?"}
      B -->|是| C["SendGroupMsg"]
      B -->|否| D["SendPrivateMsg"]
      A --> E["CQ 转义 + 切块"]
    ```
  - **验收**：`TestOnebotSender_EscapesCQCode`、`TestOnebotSender_GroupVsPrivate`、`TestOnebotSender_SplitsLongText`。

- [ ] **T805 能力表 + 文档**（P0｜依赖 T804｜S｜🟡）
  - **规格**：manifest 声明事件与特征；文档写清 NapCat 接入步骤（反连地址、token、账号配置）。
  - **上下文**：`docs/channel-capabilities.md` 由 `channelcaps.Markdown()` 生成；接入步骤写 `docs/channel-onboarding.md`。
  - **架构**：声明包 + 文档。
  - **数据结构**：
    ```go
    Manifest{Channel:"onebot", Label:"OneBot v11", Via:"webhook+ws",
      Events: []string{received, deleted, group.member_joined, group.member_left},
      Features: []Feature{FeatureGroup, FeatureMention}}
    ```
  - **数据流向**：代码 → 生成文档。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["manifest"] --> B["Markdown()"]
      B --> C["docs/channel-capabilities.md"]
    ```
  - **验收**：`go test ./internal/channelcaps/` 绿；文档链接可达。

- [ ] **T806 真实账号验收**（P0｜依赖 T805｜S｜🔵）
  - **规格**：NapCat/Lagrange 实例 + QQ 号跑通私聊与群 @，断线重连后可继续发。
  - **上下文**：需要一台常驻机器跑协议端；QQ 号有风控风险，仅用于验收。
  - **架构**：无代码改动。
  - **数据结构**：
    ```text
    记录：self_id / group_id / 消息 id / 回复 id / 重连时间
    ```
  - **数据流向**：QQ → NapCat → WS → HiveMTK → 回复。
  - **逻辑图**：
    ```mermaid
    sequenceDiagram
      participant Q as QQ
      participant N as NapCat
      participant H as HiveMTK
      Q->>N: 消息
      N->>H: WS 帧
      H->>N: 发送 API
      N->>Q: 回复
      N--xH: 断线
      N->>H: 重连
    ```
  - **验收**：两条真实消息进收件箱；回复可见；重连后仍可发。

---

## Phase 9 Slack（P1）

- [ ] **T901 `channelbot/slack` 客户端 + 账号表**（P1｜依赖 T101｜M｜🟡）
  - **规格**：Events API 验签、`chat.postMessage`（含 thread）、`conversations.open`；账号表独立。
  - **上下文**：Slack 签名是 `v0={HMAC_SHA256(signing_secret, "v0:{ts}:{body}")}`，且有 5 分钟时间窗；LangBot 亦有 slack-omni 适配器可对照。
  - **架构**：`internal/channelbot/slack` + `model.SlackAccount`（迁移 071）+ 五处登记。
  - **数据结构**：
    ```go
    type SlackAccount struct{ TenantID, TeamID, BotToken, SigningSecret, AppID string; AIAgentEnabled bool; Status int }
    func VerifySignature(signingSecret, timestamp, body, signature string, now time.Time) bool
    func (c *Client) PostMessage(ctx, token, channel, text, threadTS string) (string, error)
    ```
  - **数据流向**：回调 → 验签 → 事件解析；出站 → `PostMessage`。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["Slack 回调"] --> B["VerifySignature（含时间窗）"]
      B -->|失败| C["401"]
      B -->|成功| D["解析事件"]
      E["出站"] --> F["PostMessage(thread_ts)"]
    ```
  - **验收**：`TestSlack_VerifySignature`（含过期时间戳拒绝）、`TestSlack_PostMessageThread`、`check-enum-consistency.sh` 绿。

- [ ] **T902 入站 handler + 登记**（P1｜依赖 T901｜M｜🟡）
  - **规格**：`url_verification` 验签后短路；`app_mention`/`message.im` 入站；编辑/删除映射事件类型；过滤机器人自身消息。
  - **上下文**：Slack 事件带 `event_id`，可用于幂等；`bot_id`/`subtype=bot_message` 必须忽略，否则会自问自答。
  - **架构**：`service/webhook_channel_slack.go` + 五处登记。
  - **数据结构**：
    ```go
    type slackEnvelope struct{ Type, Challenge, EventID string; Event struct{ Type, Subtype, BotID, User, Text, Channel, TS, ThreadTS string } }
    ```
  - **数据流向**：HTTP → 验签 → challenge 短路 / 事件映射 → Ingress。
  - **逻辑图**：
    ```mermaid
    flowchart TD
      A["Slack 回调"] --> B["验签"]
      B --> C{"type=url_verification?"}
      C -->|是| D["回 challenge"]
      C -->|否| E{"bot_id/subtype 机器人?"}
      E -->|是| F["忽略"]
      E -->|否| G["映射事件 → Ingress"]
    ```
  - **验收**：`TestSlack_ChallengeShortCircuit`、`TestSlack_IgnoresBotMessages`、`TestSlack_ThreadReplyConversationKey`。

- [ ] **T903 出站 sender + 能力表**（P1｜依赖 T902｜M｜🟡）
  - **规格**：回复进同一 thread；manifest 声明事件（含编辑/删除/表情回应/成员变动）。
  - **上下文**：thread 键用 `channel + thread_ts`，否则会话会被拆成多条。
  - **架构**：`outbound_sender_slack.go` + manifest + capregistry。
  - **数据结构**：
    ```go
    Manifest{Channel:"slack", Via:"webhook", Events: []string{received, edited, deleted, reaction, group.member_joined, group.member_left}, Features: []Feature{FeatureGroup, FeatureMention}}
    ```
  - **数据流向**：`sendOutbound` → slackSender → `PostMessage(thread_ts=入站 ts)`。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["Send"] --> B{"有 thread_ts?"}
      B -->|是| C["PostMessage(thread)"]
      B -->|否| D["PostMessage(新消息)"]
    ```
  - **验收**：`TestSlackSender_RepliesInThread`；能力表新行。

- [ ] **T904 真实工作区验收**（P1｜依赖 T903｜S｜🔵）
  - **规格**：Slack app（`chat:write`、`app_mentions:read`、`im:history`）+ 公网回调跑通。
  - **上下文**：需要可外网访问的回调地址；workspace 管理员安装 app。
  - **架构**：无代码改动。
  - **数据结构**：
    ```text
    记录：team_id / channel / thread_ts / 消息 id
    ```
  - **数据流向**：Slack → 回调 → 入库 → AI → PostMessage。
  - **逻辑图**：
    ```mermaid
    sequenceDiagram
      participant S as Slack
      participant H as HiveMTK
      S->>H: events API
      H->>S: chat.postMessage
    ```
  - **验收**：@bot 与私聊各一条；thread 内回复正确。

---

## Phase 10 企业微信智能机器人 wecombot（P2）

- [ ] **T1001 回调解密客户端 + 账号表**（P2｜依赖 T101｜M｜🟡）
  - **规格**：AES-CBC 解密（`EncodingAESKey` 43 位 → 32 字节）、签名校验、webhook 推送；账号表独立。
  - **上下文**：与 wecom 应用消息不同，智能机器人走独立回调协议；复用 `wecom` 的签名思路但密钥来源不同。
  - **架构**：`internal/channelbot/wecombot` + `model.WeCombBotAccount`（迁移 072）。
  - **数据结构**：
    ```go
    type WeCombBotAccount struct{ TenantID, Token, AESKey, WebhookURL string; AIAgentEnabled bool; Status int }
    func Decrypt(aesKey, ciphertext string) ([]byte, error)
    ```
  - **数据流向**：回调密文 → 解密 → 明文 JSON → 入站。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["密文回调"] --> B["签名校验"]
      B --> C["AES-CBC 解密"]
      C --> D["明文 JSON"]
    ```
  - **验收**：官方样例密文固定向量解密通过；签名错误用例；`check-enum-consistency.sh` 绿。

- [ ] **T1002 入站 + sender + 能力表**（P2｜依赖 T1001｜L｜🟡）
  - **规格**：`message`/`enter_chat`/`template_card_event`/`feedback` 四类；出站 markdown 消息体。
  - **上下文**：智能机器人支持 markdown 富文本，能力表需声明 `rich_card` 之外的消息格式。
  - **架构**：`webhook_channel_wecombot.go` + `outbound_sender_wecombot.go`。
  - **数据结构**：
    ```go
    type wecombotMsg struct{ MsgType string `json:"msgtype"`; Markdown *struct{ Content string } `json:"markdown,omitempty"` }
    ```
  - **数据流向**：解密事件 → 映射（`enter_chat` → 事件总线）→ Ingress；出站 → `PushMessage`。
  - **逻辑图**：
    ```mermaid
    flowchart TD
      A["解密事件"] --> B{"类型"}
      B -->|message| C["Ingress"]
      B -->|enter_chat| D["事件总线（可触发欢迎语）"]
      B -->|template_card_event| E["事件总线"]
      B -->|feedback| F["事件总线"]
    ```
  - **验收**：`TestWecombot_TemplateCardEvent`、`TestWecombotSender_MarkdownBody`。

---

## Phase 11 通用入口 custom（P1）

- [ ] **T1101 入站契约 + 验签 + 账号表**（P1｜依赖 T203｜M｜🟡）
  - **规格**：给自建/长尾平台一个稳定入口：固定 JSON 契约 + HMAC 签名 + 独立账号表。
  - **上下文**：`ChannelCustom` 常量已存在（`webhook_channel_telegram.go:82`），但无账号表、无验签、无入站适配器，且 `webhookInboundRejectHints` 明确写着「custom 无入站适配器」。
  - **架构**：Service handler + Model（迁移 070）+ 五处登记。
  - **数据结构**：
    ```json
    {
      "event_id": "evt_1",
      "channel_msg_id": "msg_1",
      "conversation_id": "conv_1",
      "sender": {"id": "u1", "name": "张三"},
      "msg_type": "text",
      "content": "你好",
      "media_url": "",
      "timestamp": 1730000000,
      "is_group": false,
      "group_id": ""
    }
    ```
    签名头：`X-Hive-Signature: t=<unix>,v1=<hex(hmac_sha256(secret, t + "." + body))>`，时间窗 5 分钟。
  - **数据流向**：HTTP → 验签 → 契约校验 → `Ingress`。
  - **逻辑图**：
    ```mermaid
    flowchart TD
      A["POST /api/webhook/custom/:id"] --> B["ResolveTenant(账号表)"]
      B --> C["验签 + 时间窗"]
      C -->|失败| D["401，不落库"]
      C -->|成功| E["契约校验"]
      E -->|缺字段| F["400"]
      E -->|通过| G["Ingress"]
    ```
  - **验收**：`TestCustomInbound_SignatureRejects`、`TestCustomInbound_Idempotent`、`TestCustomInbound_ContractValidation`。

- [ ] **T1102 出站回调 sender**（P1｜依赖 T1101｜M｜🟡）
  - **规格**：向账号 `callback_url` POST 回复，同款签名；5xx/超时走既有重试。
  - **上下文**：重试由 `enqueueSendRetry`（`webhook_outbound.go:315`）统一负责，sender 只需返回可重试错误。
  - **架构**：`outbound_sender_custom.go`，注册进 T101。
  - **数据结构**：
    ```json
    {"event_id":"...","conversation_id":"...","content":"...","reply_to":"...","timestamp":1730000000}
    ```
  - **数据流向**：`sendOutbound` → customSender → POST → 状态码/错误 → `Fail`/`Sent`。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["Send"] --> B["签名并 POST callback_url"]
      B -->|2xx| C["Sent"]
      B -->|5xx/超时| D["Fail(可重试)"]
      B -->|4xx| E["Fail(不可重试)"]
    ```
  - **验收**：`TestCustomSender_SignsCallback`、`TestCustomSender_RetryOn5xx`。

- [ ] **T1103 移除拒绝提示 + 四处更新**（P1｜依赖 T1102｜S｜🟡）
  - **规格**：自定义渠道变为可用，清掉所有「不可用」断言。
  - **上下文**：四处：`webhookInboundRejectHints`（custom 行）、`webhookInboundCapable`（无分支）、`Verify`（无分支）、`controller/webhook_batchc_d03_http_test.go:75`（断言返回「无入站适配器」）。
  - **架构**：Service + Controller 测试。
  - **数据结构**：
    ```go
    // 删除：ChannelCustom: "custom 无入站适配器：消息落库后既不进收件箱也不会触发 AI"
    ```
  - **数据流向**：同上链路。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["四处更新"] --> B["能力=可用"]
      B --> C["回调 → 入库 → AI"]
    ```
  - **验收**：`grep -c '不触发 AI' internal/service/webhook.go` = 0；`TestWebhookCapability_CustomAccepted` 绿；`go test ./internal/controller/ -run BatchC` 绿。

---

## Phase 12 门禁与文档收口

- [ ] **T1201 接入触点自动化校验**（P1｜依赖 Phase 7–11｜M｜🟡）
  - **规格**：把「接一个平台要改哪几处」变成可执行检查，漏一处即失败。
  - **上下文**：已有 `check-*.sh` 家族（layer-boundary/model-migration/enum-consistency/naked-goroutine/tenant-ctx-writes），新脚本按同方式接入 CI。
  - **架构**：Python 脚本 + CI 步骤。
  - **数据结构**：
    ```python
    # 触点矩阵：channel × [model 常量, channelcaps manifest, accountTable 行,
    #                       inboundCapable 分支, sender 注册, client 包]
    ```
  - **数据流向**：读源码 → 提取符号 → 逐渠道比对 → 输出缺失清单 → 非零退出。
  - **逻辑图**：
    ```mermaid
    flowchart TD
      A["channelcaps.All()"] --> B["逐渠道查 6 触点"]
      B --> C{"缺失?"}
      C -->|是| D["打印渠道+缺失项，exit 1"]
      C -->|否| E["exit 0"]
    ```
  - **验收**：删任一触点 → 失败并列出具体渠道+缺失项；全量 → 通过。

- [ ] **T1202 `capregistry` ↔ `channelcaps` 一致性测试**（P1｜依赖 T1201｜S｜🟡）
  - **规格**：粗粒度能力矩阵（capregistry）声明的平台必须在细粒度 manifest 中存在；反向不要求（channelcaps 更细）。
  - **上下文**：两套注册表并存是历史现实，短期不合并，先用测试钉住不自相矛盾。
  - **架构**：测试文件，无运行时代码改动。
  - **数据结构**：
    ```go
    // for each capregistry.Entry: channelcaps.Known(entry.Platform) 必须为 true
    ```
  - **数据流向**：测试直接读两包导出 API。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["capregistry.All()"] --> C{"channelcaps.Known?"}
      B["channelcaps.All()"] --> C
      C -->|否| D["失败"]
    ```
  - **验收**：`go test ./internal/capregistry/` 绿。

- [ ] **T1203 文档与索引**（P1｜依赖 T1201｜S｜🟡）
  - **规格**：能力表与接入文档定稿并登记索引。
  - **上下文**：`docs/channel-capabilities.md` 生成式、`docs/channel-onboarding.md` 手写、`docs/INDEX.md` 总索引、`specs/README.md` spec 列表。
  - **架构**：文档层，无代码。
  - **数据结构**：
    ```text
    docs/channel-capabilities.md  ← UPDATE_GOLDEN=1 生成
    docs/channel-onboarding.md    ← 触点清单 + curl 示例
    ```
  - **数据流向**：代码 → 生成 → 提交。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["channelcaps"] --> B["生成能力表"]
      C["触点清单"] --> D["接入文档"]
      B --> E["docs/INDEX.md"]
      D --> E
    ```
  - **验收**：文档生成测试绿；索引表行存在。

- [ ] **T1204 全量门禁**（P1｜依赖全部｜S｜🟡）
  - **规格**：一次跑完所有强制门禁，结果写入 `delivery.md`。
  - **上下文**：命令集合见下方；`check-enum-consistency.sh` 有 2 条既有 warning（`system` 枚举、`intent_major`），新增渠道不得新增 warning。
  - **架构**：CI/本地脚本。
  - **数据结构**：
    ```bash
    cd user-server && go build ./... && go vet ./... \
      && go test ./internal/service/ ./internal/channelbot/... ./internal/channelcaps/ \
                ./internal/channelgw/... ./internal/router/ ./internal/capregistry/ \
      && cd .. && python3 scripts/check-layer-boundary.py \
      && python3 scripts/check_model_migration.py \
      && bash scripts/check-enum-consistency.sh \
      && bash scripts/check-naked-goroutine.sh
    ```
  - **数据流向**：命令 → 输出 → 记录。
  - **逻辑图**：
    ```mermaid
    flowchart LR
      A["go build/vet/test"] --> B["python 门禁"]
      B --> C["bash 门禁"]
      C --> D["delivery.md 记录"]
    ```
  - **验收**：全绿；warning 数量不增。

---

## 交付顺序（批次）

| 批次 | 任务 | 目的 | 可独立发布 |
|---|---|---|---|
| B1 | T101–T112 | 出站接缝，零行为变更 | 是 |
| B2 | T201–T207 | 事件类型，编辑/撤回不再伪装文本 | 是 |
| B3 | T301–T305 | 路由规则，默认行为不变 | 是 |
| B4 | T401–T404、T501–T502 | 渲染统一 + 身份键修正 | 是（T501 需人工确认去重结果） |
| B5 | T601–T606 | 生命周期读侧 | 是 |
| B6 | T701–T707（wecomcs） | 首个新平台，验证接缝 | 是 |
| B7 | T801–T806（OneBot） | QQ 私域缺口 | 是 |
| B8 | T901–T904（Slack）、T1001–T1002（wecombot） | 国外平台 + 智能机器人 | 是 |
| B9 | T1101–T1103、T1201–T1204 | 通用入口 + 门禁收口 | 是 |

外部腿（🔵）汇总：T707 企微客服真实账号、T806 NapCat QQ 号、T904 Slack 工作区。

## 触点矩阵（接一个新平台必须全绿）

| 触点 | 代码位置 | 校验手段 | 漏掉的症状 |
|---|---|---|---|
| 渠道常量 | `model/message_event.go` + `migrations/047_pg_enums.sql`（+063 式增量） | `check-enum-consistency.sh` | 离线部署拒绝未知平台值 |
| 账号模型 | `internal/model/*.go` + `internal/pkg/db/migrate.go` | `check_model_migration.py` | 表不存在，回调 500 |
| 租户解析 | `webhookAccountTableByChannel`（`webhook.go:393`） | `TestWebhookDispatch_<X>` | 回调 404 fail-closed |
| 能力判定 | `webhookInboundCapable`（L349）+ `webhookInboundRejectHints`（L358） | `TestWebhookInbound_RejectHintsStayConsistentWithCapability` | 提示与能力自相矛盾 |
| 验签 | `Verify`（L663） | 每平台 4xx 用例 | 伪造回调可入库 |
| 分发 | `dispatchToChannel`（L1130） | `TestWebhookDispatch_<X>` | 消息静默丢弃 |
| 能力声明 | `channelcaps` manifest + `capregistry` | `TestCapabilityDocFreshness`、T1202 | 能力文档漂移 |
| 出站 | `RegisterChannelSender` + sender 文件 | T111 parity 测试 | 能收不能回 |
| 客户端 | `internal/channelbot/<x>` | 各包 `httptest` 回放 | 协议细节无处可测 |
