# 新平台接入（wecomcs / OneBot v11 / Slack / wecombot / custom）

来源：

- `specs/024-im-seam-alignment/`（接缝：出站注册表、事件类型、路由、渲染、身份双层键、交互回调）——**本 spec 的前置**
- `docs/research/LANGBOT-VS-HIVEMTK-PLATFORM-ARCH-2026-11.md`（平台差集 + 接入配方）
- `docs/research/LANGBOT-ARCH-DEEPDIVE-2026-11.md`（manifest 契约、10 步接入清单）
- `docs/research/CHATWOOT-UNIFIED-INBOX-DEEPDIVE-2026-11.md`（8 件产物清单与验收口径）

本 spec 从原 024 拆出（二次审核 R1/R13：62 任务、30–34 人日不可交付）。**024 交付接缝，025 交付平台**，
两者以「024 的 B1 出站注册表 + B2 事件类型」为唯一交接面：这两批未交付，本 spec 不开工。

## 一、为什么要接这五个（差集结论）

对照 LangBot 13 个 Omni 适配器与我们已有的渠道（webhook 7 + bridge 10 + web widget + sms/email），
真正的平台缺口是 8 个：OneBot v11、企业微信客服、企业微信智能机器人、Slack、Discord、LINE、KOOK、Matrix；
另有 2 个形态缺口（Satori、通用 HTTP/WS 入口）与 1 个部分缺口（页面机器人）。

本 spec 只做**能明确验证接缝**的五个，其余按需求排期：

| 平台 | 为什么是它 | 验证什么接缝 |
|---|---|---|
| 企业微信客服 wecomcs | 与我们已有 `wecom` 共用 corp_id，账号最可能已存在 | 账号表分离 + 游标续拉 + 事件类型（`enter_session`/`msg_send_fail`） |
| OneBot v11 | 覆盖 QQ 私域最大缺口（我们只有 QQ 官方机器人） | 通用 HTTP + 反向 WS 两种入口、`echo` 请求响应关联 |
| Slack | 国外平台 + 线程回复 | Events API 验签 + `thread_ts` 线程 + `event_id` 重投幂等 |
| 企业微信智能机器人 wecombot | 模板卡片事件最多 | 卡片回调 → `channel_interactions`（024 D8） |
| 通用 custom 入口 | 不写 SDK 的第三方自助接入 | 平台无关契约（HMAC 签名 + msg_id 幂等）能不能撑住「无 SDK」 |

不做：Discord / LINE / KOOK / Matrix（接缝就绪后按需求排期）、`wechatpad`（iPad 协议）与 `openclaw_weixin`
（合规与稳定性风险）、Satori（等 OneBot 落地后评估是否只是换协议名）。

## 二、平台接入的八个触点（缺一即红或静默降级）

每个平台必须交付同一套产物，顺序即依赖顺序：

1. `channelbot/<x>` 纯协议客户端（验签 + 解析 + 发送；`httptest` 可回放，不依赖真实账号）
2. `service/webhook_channel_<x>.go`（解析原生报文 → `MessageEvent`，显式填 `EventType`）
3. `webhook.go` 三处登记：`webhookInboundCapable`、`dispatchToChannel`、`Verify`
4. `service/outbound_sender_<x>.go`（实现 024 的 `channelSender`，注册进注册表）
5. `channelcaps` manifest（`Inbound`/`Outbound`/`Events`/`Features`/`OutboundFormat`/`MaxTextLen`）
6. 账号表 + `webhookAccountTableByChannel`（租户反查的唯一权威）
7. 路由与 AI 触发（`shouldTriggerAI` 的账号级开关、路由规则可用性）
8. 测试：验签失败 4xx 且不落库、同 msg_id 双投不重复、私聊/群聊各一条 fixture 回放、`*ChannelError` 分类

## 三、隐性必做项（漏一项的后果已实测）

| 必做项 | 位置 | 漏掉的后果 |
|---|---|---|
| 账号表 + `webhookAccountTableByChannel` 条目 | `webhook.go:393` | 租户反查退到 `integration_accounts` 按平台名找，多租户共用同名平台即歧义 → fail-closed 404，回调全丢 |
| `platform_type_enum` 增枚举值 | `migrations/047_pg_enums.sql`（+ 063 式增量） | `check-enum-consistency.sh` 红；离线 SQL 部署库写 `message_hub.platform` 报 invalid input |
| 模型登记 `allModels()` | `internal/pkg/db/migrate.go` | 新库不建表；`check_model_migration.py` 红 |
| 渠道常量 + `model.Channel*` | `model/message_event.go` | 收件箱前端/统计的渠道词表缺值 |
| `channelcaps` manifest | `internal/channelcaps/manifests.go` | `TestEveryChannelConstantHasManifest` 红 |
| 验签失败 4xx 不落库的非对称测试 | 渠道测试文件 | 安全面回退无门禁 |
| 拒绝提示表同步 | `webhookInboundRejectHints` + 一致性测试 | 已接入却仍提示「不支持」 |
| 口令存放与出口脱敏 | 新账号表 secret 列 + 读接口 | 明文泄露/接口回显（先例：`controller/wecom_secret_mask.go`、`secrets/aesgcm.go`） |
| 长连接或游标的生命周期 | WS 启停 / `sync_cursor` 列 | 重启丢消息，或重复拉取产生重复消息 |

## 四、平台要求

### FR-7x 企业微信客服 wecomcs

- FR060 新表 `wecom_cs_accounts`：`corp_id / kf_secret / kf_token / kf_aes_key / open_kfid`（`WeComAccount` 无这些列，
  且不能复用——否则 `ResolveTenantByChannelAccount` 会在两张表间猜）。迁移 069。
- FR061 回调验签复用 `verifyWeCom(token, aesKey, body, query)`；`sync_msg` 游标落库（`sync_cursor` + 拉取 worker），
  重启续拉；`kf/send_msg` 出一站式发送 sender。
- FR062 事件映射：`enter_session` / `msg_send_fail` / `session_status_change` 等走事件类型 + 审计，
  不伪装成文本消息。

### FR-8x OneBot v11

- FR063 `POST /api/webhook/onebot/{account_id}`：支持 `message` / `notice` / `request` / `meta_event` 四类上报，
  映射到 024 的事件类型（`group.member_joined`/`left`、`bot.invited_to_group` 等）。
- FR064 反向 WS 入口：独立 upgrade 路由、Bearer 鉴权（`access_token`）、同 `self_id` 顶掉旧连接、心跳与启停幂等；
  须过 `check-naked-goroutine.sh`。
- FR065 出站：`send_private_msg` / `send_group_msg`，`echo` 关联请求响应；`qq_accounts` 不能承载 `self_id/access_token`，
  新建 `onebot_accounts`。迁移 070。

### FR-9x Slack

- FR066 Events API：`v0` 签名校验（`X-Slack-Signature` + 5 分钟时间窗）、`url_verification` challenge 短路。
- FR067 消息映射 `app_mention` / `message.im`；`event_id` 作幂等键（Slack 会重投同一 event_id）。
- FR068 出站 `chat.postMessage` + `thread_ts` 线程回复；sender 走 024 注册表。迁移 071。

### FR-10x 企业微信智能机器人 wecombot

- FR069 回调解密（AESKey）+ `template_card_event` → `interaction.callback`（024 D8：写 `channel_interactions` + 真答复）。
- FR070 主动推送 webhook；账号表 `wecombot_accounts`。迁移 072。

### FR-11x 通用 custom 入口

- FR071 `POST /api/webhook/custom/{account_id}`：JSON 契约（`event_id/channel_msg_id/conversation_id/sender{id,name}/
  msg_type/content/media_url/timestamp/is_group/group_id`）+ `X-Hive-Signature`（HMAC-SHA256 + 5 分钟时间窗）+ msg_id 幂等。
  替代今天的 `ChannelCustom` 拒绝提示（`webhook.go:358-372`）。
- FR072 同时要改三处：`webhookInboundRejectHints` 删 custom 条目、`webhookAccountTableByChannel` 加
  `custom_channel_accounts`（否则租户反查走到歧义拒绝）、`controller/webhook_batchc_d03_http_test.go:75` 的断言
  （今天要求 custom 回「无入站适配器」）。迁移 073。
- FR073 出站：账号配置 `callback_url` + 签名回调，重试沿用 024 的 `enqueueSendRetry`。

## 五、验收

1. 每平台验收必须包含：验签失败 4xx 且不落库、同 msg_id 二次投递不重复、私聊与群聊各一条 fixture 回放、
   渠道错误返回 `*ChannelError`（分类正确）、能力表声明与实现一致。
2. 全量门禁：`go build ./... && go vet ./... && go test ./internal/service/ ./internal/channelbot/... ./internal/channelcaps/ ./internal/channelgw/...`；
   `python3 scripts/check-layer-boundary.py`、`python3 scripts/check_model_migration.py`、`bash scripts/check-enum-consistency.sh`。
3. 每平台独立可发布、可回滚（账号表与枚举增量可并存，不回滚已发出的消息）。
4. 🔵 外部腿：wecomcs 真实企业微信客服账号、OneBot 需 NapCat QQ 号、Slack 需工作区——只卡「真实回放」，
   代码与 fixture 全部本地完成。

## 六、风险

| 风险 | 缓解 |
|---|---|
| 平台协议变更（Slack/OneBot 最常见） | 协议细节全在 `channelbot/<x>`，升级只改一个包；fixture 回放固定报文形状 |
| 账号表口令明文 | 新表 secret 列按 `secrets/aesgcm.go` 加密，读接口走 mask（`wecom_secret_mask.go` 同款） |
| WS 长连接泄漏 | 启停幂等 + `check-naked-goroutine.sh` + 连接数指标 |
| 游标丢失导致重复拉取 | 游标落库 + 幂等键，重复拉取的后果仅是多一次去重，不是重复消息 |
| 平台审核/资质 | 与 024 接缝无关；每平台单独评估（Slack 需 app 审核，OneBot 无需） |
