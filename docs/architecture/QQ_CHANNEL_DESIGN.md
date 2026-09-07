# QQ 机器人渠道接入设计文档

> 版本: v1.0 | 日期: 2026-09-07 | 状态: 已实施
> 调研结论附件: QQ 官方开放平台(q.qq.com)支持个人/企业认证,群聊 @ 消息收发全场景开放(2024→2026 扩权),Webhook + Ed25519 验签。

## 1. 目标与非目标

**目标**: 接入 QQ 官方机器人开放平台,实现:
- 群聊 @机器人 消息入站(GROUP_AT_MESSAGE_CREATE)与单聊入站(C2C_AT_MESSAGE_CREATE)
- AI 智能体自动承接回复(复用 SalesEngine / SmartOrchestrator 全链路)
- 消息落 message_hub 中台 + 统一收件箱 + 渠道绑定智能体
- 账号管理(CRUD + webhook 验签配置 + 测试发送)

**非目标**(本期不做):
- WebSocket 接入方式(仅 Webhook;官方双通道,webhook 与现有架构一致)
- 富媒体出站(仅文本;Markdown/富媒体后续迭代)
- 主动消息营销(官方频控严格:群 60条/分钟、单群日 1000 条,且需用户开关允许;仅做被动回复)
- 群管理能力(禁言/审批等 2026 新接口,与私域承接无关)

## 2. 官方协议要点(实现依据)

| 项 | 内容 |
|---|---|
| 鉴权 | AppID + AppSecret → POST `https://api.bot.qq.com/app/getAppAccessToken` 换 access_token(7200s,临期 60s 内返回新 token);API 请求头 `Authorization: QQBot {access_token}` |
| 回调验签 | Ed25519:对 BotSecret 派生 seed(repeat 补齐 32 字节)→ `ed25519.GenerateKey` 得公钥;请求头 `X-Signature-Ed25519`(hex,校验 len==128 且 sig[63]&224==0)+ `X-Signature-Timestamp`;验签消息 = timestamp + body |
| 回调地址验证 | Op 13:对 `event_ts + plain_token` 用派生私钥签名,hex 返回 `{"plain_token","signature"}` |
| 群事件 | `GROUP_AT_MESSAGE_CREATE`(op 0, Intent 1<<25):group_openid / author.member_openid / content(已去 @ 前缀) |
| 单聊事件 | `C2C_AT_MESSAGE_CREATE`:user_openid / content |
| 群发消息 | POST `/v2/groups/{group_openid}/messages`,msg_type=0(文本),必传 `msg_seq`(同一 msg_id 重发需递增,主动消息可随机) |
| 单聊发消息 | POST `/v2/users/{user_openid}/messages`,同上 |
| 应答 | webhook 接收后返回 HTTP 200 即 ACK |

## 3. 架构设计(遵循现有三层渠道接入模式)

```
QQ 服务器 ──HTTPS POST──▶ /api/webhook/qq/{account_id}
                            │ controller/webhook.go Receive(已有通用入口)
                            ▼
                     WebhookService.Receive
                       ├─ Verify: qq Ed25519 验签(channelbot/qq)
                       ├─ ParsePayload: 通用 JSON 提取
                       └─ 入队 → handleJob → dispatchToChannel
                            │ case ChannelQQ: dispatchQQ (webhook_channel_qq.go)
                            ▼
                     qq.ParseEvent → Event.ToInbound → Ingress
                       (EventID = "qq_{id}" 幂等键)
                            ▼
                     InboxIngressService.HandleIngressMessage
                       (标准化 → 人工锁/AI 串行锁 → message_hub → 触发 AI)
                            ▼
                     triggerSalesEngine ─▶ AI 回复 ─▶ sendOutbound
                            │ case ChannelQQ
                            ▼
                     QQIntegrationService.SendMessage
                       (access_token 管理 → POST /v2/groups|users/{openid}/messages)
```

与 Telegram 完全同构:协议层 `channelbot/qq`(纯协议零业务依赖)、dispatch 层 `service/webhook_channel_qq.go`、出站集成 `QQIntegrationService`。

## 4. 数据模型

`model.QQAccount`(表 `qq_accounts`,AutoMigrate 管理,无需 SQL migration):

| 字段 | 类型 | 说明 |
|---|---|---|
| ID | uint | 主键 |
| OwnerUserID | uint | 归属用户(渠道账号所有权守卫) |
| AccountName | string(100) | 账号名称 |
| AppID | string(100) | 开放平台 AppID(必填) |
| AppSecret | string(200) | AppSecret(必填,更新不回显) |
| WebhookSecret | string(200) | 官方 BotSecret(用于 Ed25519 派生验签) |
| WebhookURL | string(500) | 回调地址 |
| WebhookEnabled | bool | webhook 是否已注册(在 q.qq.com 管理端配置) |
| AIAgentEnabled | bool | 智能体自动回复开关 |
| AccessToken / TokenExpires | 缓存字段 | 服务端 access_token 缓存(7200s,提前 300s 刷新) |
| LastSyncAt / LastErrorAt / LastErrorMsg | 状态字段 | 与 telegram_accounts 对齐 |
| Status | int | 1=正常 0=停用 |

**消息中台**:`messageHubPlatforms` 白名单 + `model.ChannelQQ = "qq"` 渠道常量。**AI 渠道枚举**:`model.ChannelTypeQQ`,`NormalizeChannelType("qq")` 映射,支持渠道绑定智能体。

## 5. 关键设计决策

1. **Ed25519 验签放协议层**(`channelbot/qq.VerifySignature` + `BuildCallbackVerifyResponse`),Go 标准库 `crypto/ed25519` 实现,零新依赖;secret 为空直接拒绝(与 telegram 同等安全水位,无"跳过验签"后门,开发环境走全局 `ALLOW_INSECURE_WEBHOOK`)。
2. **Op13 回调地址验证**:QQ 管理端配置回调时发 `op=13` 请求,需用私钥签名 `event_ts+plain_token` 回传。在 `Verify()` 阶段前置检测(`payload.op==13`),由 `WebhookService.VerifyQQCallbackChallenge` 直接在 Verify 层短路返回(不走入队,因为需要同步 HTTP 响应返回签名)——实现上放到 controller 的 Receive 里、在调 `svc.Receive` **之前**拦截。
3. **幂等**:官方群消息事件 `id` 字段为事件唯一 ID,EventID 取 `qq_{event_id}`;同事件重投依赖 message_hub 唯一约束 + webhook eventRepo 去重。
4. **会话归属**:群聊 ConversationID = `group_openid`(群维度会话,与 TG 群一致),单聊 = `user_openid`;`IsGroup=true` 时 GroupID = group_openid。
5. **msg_seq 递增**:同一 msg_id 的被动回复 5 分钟限 5 条,每条发送 msg_seq 自增;实现为 IntegrationService 内 `map[msgID]seq` 内存计数(重启丢失可接受,最坏退化为平台拒绝第 6 条起)。
6. **消息长度**:官方单条消息内容上限,超长按 TG 模式分段发送(`splitMessage` 复用逻辑在 qq 包内独立实现,避免跨包耦合)。
7. **静默时段**:复用全局 quiet hours(23:00-7:00)延迟出站队列,无渠道特判。

## 6. 文件清单

| 文件 | 职责 |
|---|---|
| `user-server/docs/dev/QQ_CHANNEL_DESIGN.md` | 本文档 |
| `internal/channelbot/qq/qq.go` | 协议层:Client(access token 管理/发送/分段)、Ed25519 验签、Op13 应答、事件解析、ToInbound/Ingress |
| `internal/channelbot/qq/qq_test.go` | 协议层单测(验签向量、解析、token 刷新、httptest 发送) |
| `internal/model/feishu.go` | 追加 `QQAccount` 模型(与 TG/WA 账号模型同文件族) |
| `internal/model/message_event.go` | `ChannelQQ` 常量 |
| `internal/model/ai_agent.go` | `ChannelTypeQQ` 常量 |
| `internal/repository/feishu.go` | 追加 `QQAccountRepository`(同文件族) |
| `internal/service/qq_account.go` | QQService(CRUD)+ QQIntegrationService(入站 Ingest + 出站 SendMessage) |
| `internal/service/webhook_channel_qq.go` | dispatchQQ 入站分发 + Verify 接线辅助 |
| `internal/service/webhook.go` | `Verify()` 加 ChannelQQ 分支 |
| `internal/service/webhook_outbound.go` | `sendOutbound` 加 ChannelQQ 分支 |
| `internal/service/message_hub.go` | 白名单加 "qq" |
| `internal/service/webhook_ai.go` | shouldTriggerAI 加 ChannelQQ 分支 |
| `internal/service/channel_overview.go` | 渠道总览加 qq 条目 |
| `internal/service/ai_agent.go` | NormalizeChannelType 加 "qq" |
| `internal/controller/qq_account.go` | QQAccountController(REST CRUD + test-send) |
| `internal/router/platform_routes.go` | 路由注册 `/api/qq/accounts` |
| `internal/pkg/db/migrate.go` | AutoMigrate 加 `&model.QQAccount{}` |
| `internal/service/webhook_channel_qq_test.go` | dispatch/验签/触发 AI 集成测试 |
| `user-web/src/api/qqBot.js` | 前端 API 出口 |
| `user-web/src/views/qq/account.vue` | 前端账号管理页 |
| `user-web/src/router/modules/qq.js` | 前端路由 |

## 7. API(用户端,遵循 CLAUDE.md 规范)

```
GET    /api/qq/accounts            列表
GET    /api/qq/accounts/:id        详情
POST   /api/qq/accounts            创建 {account_name, app_id, app_secret, webhook_secret, ai_agent_enabled, status}
PUT    /api/qq/accounts/:id        更新(secret 空串=保留原值)
DELETE /api/qq/accounts/:id        删除
POST   /api/qq/accounts/:id/test-send  {target_type: "group"|"c2c", target_openid, msg_seq?, text}
```

Webhook(公开,已有通用路由): `POST /api/webhook/qq/{account_id}`

响应统一 `{"code":0,"data":...,"message":"ok"}`;AppSecret 永不回显(仅掩码)。

## 8. 测试策略

- **协议层单测**(无外部依赖):Ed25519 验签(用固定 seed 生成向量,覆盖篡改/过期/缺 header)、Op13 应答格式、事件解析(GROUP/C2C/非消息事件)、ToInbound 映射、token 缓存刷新(httptest mock)、SendMessage(httptest mock,覆盖分段/msg_seq/auth 头)。
- **集成测试**(testutil PG):账号 CRUD 全链路、dispatchQQ 入站 → message_hub 落库 → upsert inbox、AI 开关触发、出站失败标记。
- **回归**:`go build ./...` + `go test ./internal/service/... ./internal/channelbot/... -count=1` 全绿。
