# 025 新平台接入 — 交付记录（delivery）

> 交付日期：2026-11-18。施工源：本目录 spec.md / plan.md / tasks.md（T701–T1103，五平台批次）。
> 范围口径：五个平台（wecomcs / OneBot v11 / Slack / wecombot / custom）的客户端、入站、出站、
> 能力登记与账号表全部落地；T707/T806/T904 三项 🔵「真实账号验收」**本轮无真实账号**，
> 按既定口径用 **httptest 回放 + 固定签名向量**完成模拟信号验收，待有号环境按 tasks.md 补跑。

## 一、批次交付总览

| 批次 | 任务 | 交付提交 | 证据 |
|---|---|---|---|
| wecomcs 企业微信客服 | T701–T706 | `ead1fe913` | `channelbot/wecomcs`（token 缓存 + `sync_msg` 游标 + `send_msg`）、`webhook_wecomcs_puller.go`（回调触发 + 60s 兜底 + 单飞）、迁移 071 |
| OneBot v11 | T801–T805 | `ead1fe913` | `channelbot/onebot`（四类 post_type + CQ 转义/切块）、`onebot_ws.go`（反向 WS 管理器）、迁移 072 |
| Slack | T901–T903 | `ead1fe913` | `channelbot/slack`（v0 签名 + 5 分钟窗 + `chat.postMessage` 线程回复）、迁移 073 |
| wecombot 智能机器人 | T1001–T1002 | `ead1fe913` | `channelbot/wecombot`（43 位 AESKey → AES-256-CBC + 签名 + markdown 推送）、迁移 074 |
| custom 通用入口 | T1101–T1103 | `ead1fe913` | 固定 JSON 契约 + `X-Hive-Signature`（t/v1）+ 出站回调签名、迁移 075、拒绝提示摘除 |
| 跟进修复（编译完整性） | — | 本批跟进提交 | `model/message_event.go` 5 个 `Channel*` 常量 + `model/ai_agent.go` 4 个 `ChannelType*`（ead1fe913 遗漏，HEAD 曾不可编译） |

## 二、收口明细

### 入站

- 五渠道统一走通用路由 `POST /api/webhook/:channel/:account_id`；`webhookInboundCapable` /
  `webhookAccountTableByChannel` / `ParsePayload` / `Verify` / `dispatchToChannel` 五处全部接线。
- 需在入队前短路的：Slack `url_verification` 回 challenge（先验签）、wecomcs `GET` 回调验证。
- wecomcs 回调只当「有新消息」通知：worker 按 `sync_msg` 的 `cursor` 增量拉取，
  游标每页落 `wecom_cs_accounts.sync_cursor`，同账号单飞（`pulling` map），另有 60s ticker 兜底。
- OneBot 反向 WS：`GET /ws/onebot/:account_id`（挂在 webhook 分组之外，鉴权靠 `access_token`），
  先验 Bearer 再 Upgrade；同 `self_id` 重连顶掉旧连接（旧账号的 `byID` 表项一并摘除），读超时 90s 注销。
- custom 由「枚举占位」转为真适配器：`webhookInboundRejectHints` 的 custom 条目与对应断言已删除；
  旧自有系统报文（顶层 `event_id`/`content`/`sender`）仍走通用别名解析，升级不断流。

### 出站

- 五个 sender（`SlackOutboundSender`/`OneBotOutboundSender`/`WeComCSOutboundSender`/
  `WeCombBotOutboundSender`/`CustomOutboundSender`）在 `outbound_sender_im_platforms.go` 内
  `RegisterChannelSender` 登记；实现放 `outbound_impl_im_platforms.go`（脚本只扫 `outbound_sender_*.go`）。
- OneBot 出站优先反向 WS，未连接/失败回落 HTTP `/send_private_msg`、`/send_group_msg`。
- wecomcs 会话窗口错误（95019/95021/95026）映射 `window_expired`，不可重试。
- custom 出站 POST `callback_url` 同款签名；4xx 判 `bad_request` 不重试，5xx/网络判 `network` 可重试。

### 能力与枚举

- channelcaps 新增 4 条 manifest、custom 更新；`docs/channel-capabilities.md` 经 `UPDATE_GOLDEN=1` 重生成。
- capregistry 5 平台 Webhook/DMSend 登记；`docs/capabilities.md` 重生成。
- 迁移 071–075 账号表 + 076 枚举增量，`047_pg_enums.sql` 注释同步；`migrate.go` 已登记。
- `messageHubPlatforms` 补 `wecomcs`/`onebot`/`slack`/`wecombot`（`TestBF6_ChannelVocabularyCoversEveryOutboundChannel` 锁词表镜像）。

### 文档

- `docs/channel-onboarding.md` 新增「五、025 平台批接入要点」：五个平台各自的回调地址、账号表字段、
  验签/解密算法、出站约束与验收要点。
- `docs/INDEX.md` §九 与 `specs/README.md` 状态行改为已交付；`.specify/*` 归档说明补登。

## 三、全量门禁记录

| 门禁 | 结果 |
|---|---|
| `go build ./...` / `go vet ./...` | ✅ 绿 |
| `go test ./internal/...` | ✅ 绿（`USER_POSTGRES_TEST_HOST_PORT=8202` + `POSTGRES_PASSWORD`，见下注） |
| `go test ./internal/service/ -race`（OneBot WS 用例） | ✅ 绿 |
| `go test ./internal/controller/`（含五平台全链路） | ✅ 绿 |
| `check_im_touchpoints.py` | ✅ 常量 21 / manifest 25 / sender 12 / 入站 12 / 账号表 12 / client 包 8 |
| `check_model_migration.py` | ✅ 全部模型已注册 AutoMigrate |
| `check-enum-consistency.sh` | ✅ 通过（2 条既有 warning） |
| `check-layer-boundary.py` | ✅ 0 违规（扫描 1589 文件） |
| `check-naked-goroutine.py` | ✅ 0 hit |
| bridge `npx vitest run` | ✅ 106 文件 / 1310 用例通过（7 skipped） |
| user-web `npx vitest run` | ✅ 参见提交前全量记录 |

> 注：本机测试库为 Docker `mtk-postgres-test`，容器内端口 8202；`testdb_config.go` 默认只放行
> 8217，需显式 `USER_POSTGRES_TEST_HOST_PORT=8202`，属环境配置而非代码缺陷。

## 四、模拟信号验收（替代真实账号）

| 验收项 | 模拟手段 | 用例 |
|---|---|---|
| wecomcs 增量拉取 | httptest 平台（`gettoken` + `sync_msg` 两页 + origin=5 跳过） | `TestWeComCSPullerMapsMessages`、`TestWeComCSFullChain` |
| OneBot 反向 WS | gorilla 测试服务端；重连顶替、旧连接不误删、未连接回落 | `TestOneBotWSManagerReconnectReplaces`、`TestOneBotWSManagerSendUnconnected` |
| Slack 验签 | 固定 `v0:ts:body` HMAC 向量（含 +6min 过期） | `TestVerifySignatureWindow`、`TestSlackFullChain` |
| wecombot 解密 | 自造 43 位 AESKey 往返加密 + 签名向量 | `TestEncryptDecryptRoundTrip`、`TestWeCombBotFullChain` |
| custom 契约 | 自签 `t=...,v1=...`（错签/过期/篡改三负例） | `TestVerifyCustomSignature`、`TestCustomFullChain` |

全链路用例均在真实测试库上跑生产路由（`NewWebhookController(svc).RegisterRoutes`），
断言落到 `message_hub` / `webhook_events` / `channel_event_audits`，非纯函数打桩。

## 五、遗留

- T707 / T806 / T904：🔵 真实账号验收（企微客服真号、NapCat+QQ 号、真实 Slack 工作区），
  需要凭据与外部环境，本轮不勾；模拟信号已覆盖同一批判据。
