# Bridge 架构总览

> **当前架构（HTTP 三通道）**。本模块不维护 WebSocket 长连接、不使用 SSE；
> 扩展 ↔ 服务端走三条相互独立的 HTTP 通道，详细协议见 [./../bridge.md](../bridge.md) §4。

## 1. 通道划分

| 通道 | 方法 | 端点 | 触发 | 用途 |
| --- | --- | --- | --- | --- |
| A · Uplink | POST | `/api/bridge/ingest` | content script 检测到新消息 / 会话切换 | 上行消息（inbound + history），同时拉取同会话待发 AI 回复 |
| B · Downlink | GET | `/api/bridge/outbox` | background 轮询（`BRIDGE_THREE_CHANNEL.outboxPollIntervalMs=1500ms`） | 拉取待发 AI 回复（网页渠道） |
| C · Ack | POST | `/api/bridge/outbox/ack` | content script 模拟发送成功后 | 标记 `msg_id` 为 `delivered` / `failed` |

三通道参数（`src/core/constants.js` → `BRIDGE_THREE_CHANNEL`）：

```js
{
  uplinkMergeWindowMs: 350,    // 上行合并窗口（毫秒）
  uplinkMaxBatch: 20,          // 上行单批最大消息数
  outboxPollIntervalMs: 1500,  // 下行轮询间隔
  outboxBatchSize: 50,         // 下行单批最大消息数
  ackFlushIntervalMs: 500,     // ack 批量 flush 间隔
  sentCacheMax: 2000,          // 本地已发缓存容量（超界丢最旧）
  sentCacheTtlMs: 86400000,    // 已发缓存 24h 时间界（界外不再拦重发）
  sendOutboundTimeoutMs: 20000,// 下行 send 超时
}
```

## 2. 路由注册（user-server）

`internal/router/router.go`（`bridgeWS` 路由组，仅过 `InitGuard` 中间件）：

```go
bridgeWS.POST("/bridge/ingest",       bridgeHandler.HandleHTTPIngest)
bridgeWS.GET ("/bridge/outbox",       bridgeHandler.GetBridgeOutbox)
bridgeWS.POST("/bridge/outbox/ack",   bridgeHandler.AckBridgeOutbox)
```

- `account_id` 缺失 → 400 `account_id required`（不写 `default` 兜底）；
- `channel` 不在白名单 → 400 `unsupported`；
- body 上限 `HTTPIngestMaxBodySize = 4MB`，单批消息上限 `HTTPIngestMaxMessages = 200`；
- Token 走 `X-Bridge-Token` Header（`Authorization: Bearer` 不被桥接闸门读取，发了也是 401）。
  唯一例外是 SSE：EventSource 带不了自定义头，允许 `?bridge_token=<token>`，
  服务端校验通过后会把该参数从 `RawQuery` 剥离并在日志里脱敏。

## 3. 协议类型单源

`user-server/internal/channelgw` 包定义跨 HTTP / WS 共用的协议类型：

- `IngestMessage` ↔ 前端 `src/core/types.js::UnifiedMessage`
- `OutboxMessage` ↔ 前端 `src/core/types.js::UnifiedReply`
- `HistoryItem` ↔ 前端 history 帧中的单轮

**禁止在 bridge 业务代码中重定义协议结构**；调整时同步 channelgw + 前端 types.js + DEFAULTS.md + 单测。

## 4. 数据流时序

### 4.1 入站（客户发私信 → AI 处理）

```
客户发私信
  → content script MutationObserver 监听到新 DOM 节点
  → 解析为 UnifiedMessage
  → chrome.runtime port.send → background
  → Uplink 在 350ms 合并窗口内合并（按 account|conversation 分桶）
  → POST /api/bridge/ingest 上报 messages[]
  → BridgeIngestHandler → InboxIngressService.HandleIngressMessage
  → 落 message_hub + 通知 AgentRuntime
  → SmartCSOrchestrator 调 LLM → 生成回复
  → ReachAdapter.Send(req)
  → BridgeReachAdapter.Push 入 outbox（BridgeReachAdapter.Send 同时返回）
  → 随下次 /api/bridge/ingest 响应 outbound_replies 拉回
  → background 路由到对应 content script
  → adapter.sendOutbound：填输入框 + 点发送
  → 网页私信框出现 AI 回复
```

### 4.2 出站（AI 回复 → 网页发送）

```
sendOutbound(text, targetConvId)
  ├─ targetConvId == 当前会话 → 直接模拟输入发送
  ├─ targetConvId ≠ 当前会话 → openConversation(targetConvId)
  │    ├─ 左侧列表找目标用户 → 点击进入右侧聊天页
  │    └─ 找不到目标 → 放弃发送（防串台）
  └─ 模拟输入（fillContentEditable / setValue） + 点发送按钮

发送成功
  → POST /api/bridge/outbox/ack {msg_ids, status:"delivered"}
  → 服务端 UPDATE message_hub 标记 delivered
  → ack 失败入 _pendingAck（10 次重试，1s→60s 退避，24h TTL）
```

### 4.3 历史回填（页面加载 / 会话切换）

```
content script 加载 / onConversationChange
  → getConversationList 枚举左侧列表
  → 逐个 openConversation(cid) → _backfill() 读取右侧线程
  → history 帧（含 direction 标记，inbound/outbound）
  → 仅落 message_hub，不触发 AI（防回环）
```

## 5. 选择器版本兼容

平台网页 DOM 经常改版，渠道适配器的选择器采用「多候选 + 兜底」策略：

| 平台 | 主选择器（实测） | 兼容旧版 |
| --- | --- | --- |
| 抖音 | `conversationConversationItemwrapper` / `messageMsgInputpublishBtn` / `zone-container.editor-kit-container.messageEditorinputArea` | `#island_b69f5 li` / `div[data-e2e="msg-item-content"]` |
| 小红书 | `.xhs-im-conv-item` / `.chat-item` / `.xhs-im-msg-list` / `.xhs-im-input-bar-editor[contenteditable]` | `#jarvis-reply-textarea` / `.im-msg-item` / `.sx-contact-item` |
| TikTok | `[data-e2e="chat-list"]` / DraftEditor `[data-e2e="message-input"]` | `div[contenteditable]` / `[role="textbox"]` |
| 闲鱼 | 会话列表 data-* | URL path / query |
| 快手 | 会话列表 data-* | URL path / query |

> 平台改版后仅需在 `src/channels/<platform>.js` 的 `SEL` 追加候选，无需发版即可自愈。

## 6. 限速 / 风控（详见 [./../bridge.md](../bridge.md) §7）

- 扩展端三层：拟人节奏 + 令牌桶 + 会话冷却 + 相同文案去重
- 服务端兜底：入 outbox 前每账号令牌桶（60/min）
- `_pendingAck` 重试：10 次，1s→60s 退避，24h TTL

## 7. 协议与代码入口

| 概念 | 扩展端 | 服务端 |
| --- | --- | --- |
| 协议帧名 | `src/core/constants.js::PROTOCOL` | `internal/bridge/frames.go` |
| 协议结构 | `src/core/types.js` | `internal/channelgw`（HTTP/WS 共用单源） |
| 上行 / 下行 / ack 端点 | `src/core/{uplink,downlink,polling-loop}.js` + `src/core/http-ingest.js` | `internal/bridge/handler_http.go` |
| outbox 缓冲 | `src/core/downlink.js::SentCache` | `internal/bridge/http_reply_buffer.go` |
| 出站 adapter | — | `internal/bridge/reach_adapter.go` |
| 渠道常量 | `src/core/constants.js::PROTOCOL.CHANNELS` | `internal/bridge/channel.go` |
| 限速 / 风控 | `src/core/{rate-limiter,humanize}.js` | `internal/bridge/reach_adapter.go` |
