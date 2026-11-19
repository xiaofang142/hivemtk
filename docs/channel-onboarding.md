# 渠道接入手册（IM 平台接入触点清单）

> specs/024-im-seam-alignment T1203 定稿。配套机器门：`python3 scripts/check_im_touchpoints.py`
> （漏一处触点即红，CI 作业 `IM Touchpoints` 全量跑）。能力矩阵（机器生成，勿手改）见
> [channel-capabilities.md](../user-server/docs/channel-capabilities.md)；本文是**手写**的「接一个新平台要改哪几处」。

## 一、六触点清单（漏一处即被 T1201 拦下）

接一个 IM 平台，按顺序落这些触点（全部有既有先例可抄）：

| # | 触点 | 位置 | 说明 |
|---|---|---|---|
| 1 | **渠道常量** | `user-server/internal/service/webhook_channel_telegram.go`（或 model 包） | `Channel<Name> WebhookChannel = "<id>"`；桥接族渠道在 `model` + `channelgw` 登记名字串，不立 service 常量 |
| 2 | **能力 manifest** | `user-server/internal/channelcaps/manifests.go` | `Register(Manifest{...})`：Inbound/Outbound/Events/Features/OutboundFormat/MaxTextLen/**FormatEvidence**（无证据的格式声明等于猜的） |
| 3 | **账号表行** | `user-server/internal/service/webhook.go` `webhookAccountTableByChannel` | 直发渠道必须登记本地账号表（租户反查权威源）；桥接族豁免（走 integration_accounts） |
| 4 | **入站分支** | `webhookInboundCapable` switch 或专用控制器 | 通用 webhook 路由的渠道加进 switch；钉钉/公众号走专用控制器的登记豁免集 |
| 5 | **出站 sender** | 新建 `outbound_impl_<id>.go` + `outbound_sender_<id>.go` | 实现 `channelSender`；`init()` 里 `RegisterChannelSender(...)`；失败只准经 `req.Fail/FailUntracked` 上报（R5 不变量会兜底，但别依赖兜底） |
| 6 | **client 包** | `user-server/internal/channelbot/<id>/` | 平台协议 client（验签/解析/发送统一注入 `core.WithHTTPClient(httpclient.Client)`，出站 HTTP 才能被测试劫持） |

配套义务（触点之外，同样有门）：

- **事件类型**：解析器产出的事件要填 `MessageEvent.EventType`（取值必须来自 `model.EventType*`），
  并同步 `channelcaps.Manifest.Events`——`channelcaps_events_test` 钉住一致性。
- **行为冻结**：出站样例进 `outbound_golden_test.go` 的样例表（8+ 渠道 × 3 样例），
  `testdata/outbound_golden.json` 人工确认后重写。
- **路由规则**：`channel_route_rules` 免配置即放行（默认行为不变），无需为新渠道预置规则。

## 二、入站接入（curl 冒烟示例）

通用 webhook 回调入口（以 telegram 形状为例，`account_id` 为本地账号表主键）：

```bash
curl -sS -X POST "http://127.0.0.1:8080/api/webhook/telegram/<account_id>" \
  -H "Content-Type: application/json" \
  -d '{"update_id":1,"message":{"message_id":9,"date":1700000000,
       "chat":{"id":555001,"type":"private"},"from":{"id":555001,"first_name":"G"},
       "text":"hello"}}'
```

验收判据：验签失败回 4xx **不落库**；同 msg_id 重放不产生第二行
（`message_hub` 幂等）；`channel_event_audits` 能按 `event_type` 检索到这条事件。

桥接族入站不走本入口：扩展经 `POST /api/bridge/ingest` 上报（三通道契约见
`docs/bridge/BRIDGE-ARCHITECTURE-*.md`），服务端按 `channelgw` registry 能力位放行。

## 三、出站验证

```bash
# AI 回复出站走 sendOutbound → 渠道 sender；人工旁路（如需）走各渠道管理 API，
# 不要手写第二个外发权威（单一 sender 门）。
# 观测：outbound_send_failed_total{channel,category} / outbound_text_overflow_total{channel}
#       route_rule_hit_total{tenant,target}（specs/024 T1205 三指标）
```

## 四、门禁自检（接完先自己跑一遍）

```bash
python3 scripts/check_im_touchpoints.py          # 六触点矩阵
cd user-server && go test ./internal/service -run 'TestOutboundRegistry|Parity|TestOutboundBehaviorFreezeGolden' -count=1
```

## 五、025 平台批接入要点（已在仓库落地）

### OneBot v11（NapCat / Lagrange）

1. 协议端启用 **HTTP 服务端** 与 **反向 WebSocket**：
   - HTTP 上报地址：`http://<user-server>/api/webhook/onebot/<account_id>`
   - 反向 WS：`ws://<user-server>/ws/onebot/<account_id>`，`Authorization: Bearer <access_token>`
2. 账号表 `onebot_accounts` 填 `self_id`（QQ 号）、`http_base_url`、`access_token`。
3. 出站优先走反向 WS；连接断开时自动回落 `http_base_url` 的 `/send_private_msg`、`/send_group_msg`。
4. CQ 码控制字符在发送前转义（`&` `[` `]`），长文按 2000 字优先换行切块。
5. 验收：反向 WS 带坏 token 必须 401 且不登记连接；同 `self_id` 重连顶掉旧连接；
   撤回 notice 映射 `message.deleted`，成员增减映射 `group.member_joined/left`，request 只入总线不自动同意。

### Slack（Events API）

1. Slack App 配置 Request URL：`https://<domain>/api/webhook/slack/<account_id>`。
2. 订阅 `app_mention`、`message.im`、`message.channels`（按需）。
3. 账号表 `slack_accounts` 填 `signing_secret`、`bot_token`（`xoxb-`）。
4. 验签：`X-Slack-Signature` 的 `v0=HMAC_SHA256(secret, "v0:"+ts+":"+body)`，5 分钟时间窗。
5. `url_verification` 在入队前短路回 challenge；`bot_id`/`subtype=bot_message` 一律忽略。
6. 出站 `chat.postMessage` 回复沿用原消息 `thread_ts`。

### 企业微信客服（wecomcs）

1. 回调 URL：`https://<domain>/api/webhook/wecomcs/<account_id>`（POST 通知 + GET 验证）。
2. 账号表 `wecom_cs_accounts` 填 `corp_id`、`secret`、`open_kfid`、回调 `token`/`aes_key`。
3. 回调仅作「有新消息」通知；消息由 worker 按 `sync_msg` 的 `cursor` 增量拉取，
   游标每页落 `sync_cursor`，进程重启不丢消息。
4. 出站 `send_msg` 只支持 48 小时会话窗口内；窗口过期（95021/95026/95019）返回
   `window_expired` 不可重试错误，需改用模板/主动触达。

### 企业微信智能机器人（wecombot）

1. 回调 URL：`https://<domain>/api/webhook/wecombot/<account_id>`。
2. 账号表 `wecombot_accounts` 填回调 `token`、`encoding_aes_key`（43 位）与群机器人 `webhook_url`。
3. 回调体 AES-CBC 解密（16 随机 + 4 字节 msg_len + msg + receiveid），签名
   `sha1(sort(token,timestamp,nonce,encrypt))`；`enter_chat`/`template_card_event`/`feedback`
   只留事件审计，不进消息链路。
4. 出站走群机器人 webhook 的 `markdown` 载荷（单条 ≤4096 字节）。

### 自定义 webhook（custom）

固定入站 JSON 契约：

```json
{"event_id":"e1","channel_msg_id":"cm1","conversation_id":"c1",
 "sender":{"id":"u1","name":"张三"},"msg_type":"text","content":"hi",
 "media_url":"","timestamp":1700000000,"is_group":false,"group_id":""}
```

- 签名头：`X-Hive-Signature: t=<unix>,v1=<hex hmac_sha256(secret, t+"."+body)>`，5 分钟时间窗。
- 出站 POST `callback_url`，同款签名；4xx 判 `bad_request` 不重试，5xx/网络错误判 `network` 可重试。
- 旧的自有系统报文（顶层 `event_id`/`content`/`sender`）仍按通用别名解析，升级不断流。
