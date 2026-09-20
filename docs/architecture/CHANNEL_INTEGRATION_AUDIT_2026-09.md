# 渠道接入审计：13 个渠道 × 官方契约 × 可测性 —— 文档（2026-09-19）

> 所属系统：user-server 消息网关
> 代码位置：`user-server/internal/service/webhook*.go`、`internal/channelbot/*`、`internal/{wecom,feishu,qq_account,wechat,dingtalk}*.go`
> 文档状态：批A/B/C 修复进行中；契约矩阵与偏差清单为实测结论
> 取证方式：每条「偏差」都带 file:line，标注「本会话读码坐实」或「待复核」；官方口径带来源 URL

## 1. 为什么需要这份文档

此前只按 TG 单渠道做过全链路真机测试。本次把 13 个渠道常量全部摊开，逐个对官方契约，
结论分三类：**接入正确**、**与官方契约不符（可测）**、**官方根本没有公开 API（不可测，属设计约束）**。
第三类必须写进文档，否则后续会话会继续给它写"应该能跑"的测试。

## 2. 渠道清单与三类接入形态

| 形态 | 路由 | 说明 |
|---|---|---|
| A 通用推送 | `POST /api/webhook/:channel/:account_id`、`/api/webhook/:channel` | `controller/webhook.go:68-69`，挂于 `router.go:629`；13 渠道共用一个 `Verify`+`dispatchToChannel` |
| B 静态路由 | `GET\|POST /api/webhook/wechat/:account_id` | `controller/wechat.go:48-50`（`router.go:532`）。gin 静态优先于 `:channel`，**wechat 实际走 B，A 对 wechat 是死路** |
| C 浏览器桥 | `POST /api/bridge/ingest`、`GET /api/bridge/ws/channel` | `router.go:465,477`；抖/快/红/闲/TikTok 的真实入站通道，靠登录态 Chrome 夹具 |

出站只有一个入口：`service/webhook_outbound.go:348 sendOutbound`，`switch channel` 分发；
失败按 `(sent, sendErr)` 约定决定是否入 `reach_delayed_outbound` 持久化重试通道（60s/2m/4m，上限 3 次）。

## 3. 官方契约矩阵

只列对本仓库有裁决意义的字段。「仓库现状」列为本会话读码结果。

### 3.1 Telegram（Bot API 10.3） — [api](https://core.telegram.org/bots/api) / [faq](https://core.telegram.org/bots/faq)

| 契约 | 仓库现状 |
|---|---|
| `secret_token` 可选，1-256 字符，逐请求置于 `X-Telegram-Bot-Api-Secret-Token`，服务端须比对 | ✔ `webhook.go:501-518`，密钥自动装配 `telegram_webhook_bootstrap.go:21` |
| 投递为 at-least-once：非 2xx 会重推 | ✔ 需幂等，见下行 |
| 去重：`update_id` 单调递增可忽略重复；`message_id` 仅**会话内**唯一，二级键须 `(chat.id, message_id)` | ⚠ 出站有 `reply_guard`，入站无 update_id 高水位（批B） |
| `sendMessage.text` 1-4096 字符（**entities 解析后**）；群 20 msg/min、单聊 1 msg/s、广播 ~30/s | ✔ 4096 分片 `channelbot/telegram/telegram.go:24,80` |
| 429 体 `parameters.retry_after`；`description` 官方声明"内容可变"，无终态/可重试码表 | ✔ 唯一在客户端内做 3× 退避 + 读 `retry_after` 的渠道（`telegram.go:134-183`） |
| 群判别以 `chat.type`（private/group/supergroup/channel）为准 | ✔ `telegram.go:752` |
| 媒体：`sendPhoto/sendDocument/…`，入站 photo/video/document/voice + `caption`；`getFile` 上限 20 MB | ✔ **批F-4b 已补齐**（原状：`TGMessage` 只有 `Text`/`Caption`，全仓无 `getFile` 调用）：九个容器 `animation/audio/document/live_photo/photo[]/sticker/video/video_note` + `story` 原文留存（`channelbot/telegram/telegram.go:831-840`），判序按官方互斥条款（animation 先于 document、live_photo 先于 photo，`mediaKind` :878），photo 取最大档（`largestPhotoSize` :915），caption 并入正文（`Inbound` :934）；下载走 `GetFile`/`DownloadFile`（:666、:705，20 MB 上限 :653）；落库与长期 URL 回填在 `service/telegram_media.go`。官方原文与 36 条变异电池见 §14 |
| 回复用 `reply_parameters`（`reply_to_message_id` 已从文档移除） | ⚠ 仍发 `reply_to_message_id`（`telegram.go:912-955`），兼容字段，未验证官方是否长期保留 |

### 3.2 WhatsApp Cloud API — [webhooks](https://developers.facebook.com/documentation/business/messaging/whatsapp/webhooks) / [messages](https://developers.facebook.com/docs/whatsapp/cloud-api/reference/messages)

| 契约 | 仓库现状 |
|---|---|
| GET `hub.mode/challenge/verify_token` 握手；POST `X-Hub-Signature-256 = sha256=HMAC(raw body, app_secret)` | ✔ `webhook.go:552-561` → `channelbot/whatsapp.VerifyWebhook` |
| **不保证顺序、不保证 exactly-once**；重投最长 7 天指数退避；去重键 `messages[].id`(wamid) / `statuses[].id` | ⚠ 入站按 wamid 落 hub 唯一键；但 `webhook_channel_whatsapp.go:46-66` 只读**首条** message，一次推送多消息会漏 |
| 发：`POST /{Version}/{Phone-Number-ID}/messages`，`Bearer <system-user token>`，必填 `messaging_product/recipient_type/to/type` | ✔ `channelbot/whatsapp/whatsapp.go:21-22,46-49`，版本 v21.0（官方文档示例已到 v25.0，最新 v26.0 — 版本偏旧，非阻断） |
| 24h 客服窗口：窗口外只能发已审批模板（131047） | ✔ 模板回退 `webhook_outbound.go:526-557` + `WHATSAPP_FALLBACK_TEMPLATE` |
| 文本上限 4096；限流 80 msg/s 默认，超阈 130429/131056/131057；终态码 131026/131049/131050/130403/100 | ⚠ 无 4096 分片、无 429 退避（只有持久化重试通道兜底） |
| 媒体须先 `POST /{id}/media` 换 `id`，或直传 `link` | ⚠ 出站仅文本/模板，媒体未接 |

### 3.3 飞书 / Lark — [事件订阅](https://open.feishu.cn/document/ukTMukTMukTM/uYDNxYjL2QTM24iN0EjN/event-subscription-configure-/choose-a-subscription-mode/send-notifications-to-developers-server) / [消息收发](https://open.feishu.cn/document/server-docs/im-v1/message-content-description/create_json)

| 契约 | 仓库现状 |
|---|---|
| 两套独立机制：**Encrypt Key**（AES-256-CBC，key=`sha256(encrypt_key)`，**IV=密文前 16 字节且 IV 前置**）与 **Verification Token**（明文模式校验事件体 `token`） | ⚠ 解密 ✔；但 EncryptKey 缺失时 **`return true` fail-open**（`webhook.go:540-546`），且 POST 路径从不比对 VerificationToken |
| 签名 `sha256(timestamp+nonce+encrypt_key+body)` vs `X-Lark-Signature` | ✔ `webhook.go:574-598` |
| 3s 内必须 200，否则最多重推 4 次；去重键 `header.event_id`（v2）/ `message_id` | ⚠ 未见 event_id 显式去重（批B） |
| **`content` 是 JSON 字符串**；`receive_id_type ∈ open_id\|union_id\|user_id\|email\|chat_id` | ✘ **两处违背**：`feishu.go:219-223` 把原文直接当 `content`；群聊传 `open_chat_id`（`webhook_outbound.go:430`）非官方取值 → 群发必被拒。正解 helper `feishuTextContentJSON`（`feishu.go:1010`）存在但 `//nolint:unused` 未接入 |
| text ≤ 20 000 字符；单发送者/群 5 QPS；频控 999400/9991400 可重试，230002/230013/230025/230034 终态 | ✘ 无长度校验、无 QPS 护栏、错误只看 `status != 200`（`feishu.go:236`） |
| 入站 `im.message.receive_v1`：`event.message.content` 同为 JSON 字符串、mentions 为 `@_user_N` | ✔ `webhook_channel_feishu.go:171` 按字符串处理 |

### 3.4 企业微信 / WeCom — [回调](https://developer.work.weixin.qq.com/document/path/90930) / [加解密](https://developer.work.weixin.qq.com/document/path/90968)

| 契约 | 仓库现状 |
|---|---|
| `msg_signature = sha1(字典序 sort(token,timestamp,nonce,encrypt))`，POST 时三者**在 query**，body 为 XML `<Encrypt/>`（智能机器人为 JSON `{"encrypt"}`） | ✔ 算法 `webhook_channel_wecom.go:90-98`；✘ 无时窗（官方称 nonce 两小时内唯一） |
| `AESKey=Base64_Decode(EncodingAESKey+"=")`，**IV = AESKey 前 16 字节**，明文 = `random(16)+msg_len(4,大端)+msg+receiveid`，PKCS#7 补到 32 倍数 | ✘ **`DecryptWeComMessage` 错位**：`webhook_channel_wecom.go:122-125` 取**密文首块**当 IV 并跳过它，随后 :136-140 仍在 `plain[16:20]` 读长度、`plain[20:]` 取消息 —— 丢弃 random 块后长度域应在偏移 0。真机加密回调必然解坏（`VerifyURL` 同源，URL 验证亦坏） |
| 发：`/cgi-bin/message/send?access_token=`，`agentid` 必填，token 7200s **按应用缓存**；text `content` ≤ **2048 字节**，超限**静默截断**、超配额**静默丢弃且不报错** | ⚠ `wecom.go:454-502` 有 text/markdown/card 但出站硬编 `"text"`（`webhook_outbound.go:408`），无字节级截断 |
| 客户联系**没有** `externalcontact/message/send`，只有 `add_msg_template`（成员确认后下发、每客户每月限额） | ⚠ 出站走 `externalcontact/message/send`（`wecom.go:460`）—— 待复核该端点在客户联系场景是否真实可用（`本会话未验证`） |
| 5s 超时 / 3 次重投，任何非 200 都算失败并重投；按 `MsgId` 去重 | ⚠ 无 MsgId 去重 |
| 错误码：`-1` 系统繁忙(≤3 次重试)、`40014/42001` token 失效(重取)、`45009` 频控、`45033` 并发；`60011/93000` 系列终态 | ✘ 这些码在 `channel_error.go:102-106` 落入 default→**retryable**；配额/封禁类前置条件也被判可重试（`wecom_integration.go:136-143`） |
| `WECOM_DISABLE_OUTBOUND` / `IS_TEST_MODE` 时写完 hub 就 `return nil`（`wecom_integration.go:172-185`） | ⚠ 环境开关会**伪造成功**，重试通道永不触发 |

### 3.5 钉钉 — [机器人接收消息](https://open.dingtalk.com/document/orgapp/receive-message) / [HTTP回调](https://open.dingtalk.com/document/development/http-callback-overview)

官方是**三套不同机制**，仓库把它们混成一套：

| 机制 | 官方验签 | 仓库现状 |
|---|---|---|
| 机器人 HTTP 接收消息 | body 为**明文 JSON**，头 `timestamp`(ms) + `sign = Base64(HmacSHA256(timestamp+"\n"+appSecret))`；时差 > **1 小时**判非法 | ✘ **`dingtalk_app.go:101-103`：`AESKey=="" → 直接报错拒绝明文`**。真机机器人消息进不来；而带 `{"encrypt"}` 的体只要能 AES 解密就放行 → **零验签、可伪造** |
| 事件订阅 HTTP 回调 | query `msg_signature/timestamp/nonce` + body `{"encrypt"}`，签名为 `SHA1(sort(token,timestamp,nonce,encrypt))` | ✘ `dingtalk_app.go:77-82` 用 `HMAC-SHA256(timestamp+"\n"+nonce)` 配 `acc.Token`，两套官方规则都不匹配 |
| 自定义机器人（出站加签） | URL 参数 **`timestamp=`** + `sign=` | ✘ `dingtalk.go:58` 拼的是 **`ts=`** → 官方返回 310000 |
| 回复 | `sessionWebhook` 是**临时**地址，有效期只看 `sessionWebhookExpiredTime` | ✔ 过期与域名白名单检查在位（`webhook_outbound.go:586-601`）；✘ 但传输失败(:614-617)与 `errcode!=0`(:624-629)都 `return` 而**不调 `markSendFailed`** → 契约上 `(false,nil)`，永不进重试通道、无失败轨迹 |
| 入站字段 | `msgtype/text.content/senderStaffId/senderId/conversationId/conversationType(1单2群)/atUsers/isAdmin/createAt/richText` | ⚠ 一半已补、一半仍在：**已承载**（批F-4c）`msgtype` 原样进归一层（`dingTalkInboundBody` `dingtalk_app.go:289` 起，richText 逐项取 downloadCode）、`createAt` 按毫秒转时间戳且数字两种写法都收（`dingTalkFlexInt` :210、`dingTalkMessageTime` :332）、`senderStaffId‖senderId‖conversationId` 三级兜底（:144-150）。**仍完全没读**：`conversationType`、`atUsers`、`isAdmin` —— 全仓 grep 零命中 ⇒ 钉钉群/单聊不分、@机器人判定缺失、管理员身份丢失（N-22，见 §5） |
| 自定义机器人限流 | 每机器人 20 条/分钟，超则**限流 10 分钟** | ✘ 无护栏 |

### 3.6 QQ 官方机器人 — [签名](https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/interface-framework/sign.html) / [消息概述](https://bot.q.qq.com/wiki/develop/api-v2/server-inter/message/overview.html)

| 契约 | 仓库现状 |
|---|---|
| 头 `X-Signature-Ed25519`(hex) + `X-Signature-Timestamp`，被签字节是 **`timestamp + body` 拼接** | ⚠ `webhook.go:519-532` → `qq.VerifySignature`；须复核签名输入是否含 timestamp（`本会话未逐行验证`） |
| 事件名 **`C2C_MESSAGE_CREATE`**（单聊）、`GROUP_AT_MESSAGE_CREATE`、全推模式 `GROUP_MESSAGE_CREATE` | ✘ `channelbot/qq/qq.go:386` 写的是 **`C2C_AT_MESSAGE_CREATE`**（官方无此事件）→ 单聊事件全丢；`GROUP_MESSAGE_CREATE` 未处理 |
| 去重：**同一 `msg_id` 会重复推送**，须配合 `msg_seq`；`msg_id+msg_seq` 重发被 40054005 拒 | ✘ `qq.go:489-491` 用外层事件 `id`（`qq_evt_*`）作幂等键，非官方去重键 |
| 被动回复窗口：**群 5 分钟 / 5 条**，**单聊 60 分钟 / 4 条**；`msg_id`+`msg_seq` | ⚠ 只有被动态的 msg_id 复用（`qq_account.go:195`），无窗口/次数守卫；`qq_account.go:250-291` 用 openid 前缀**猜**群/单聊（`isGroup` 形参没传，`:512`） |
| token：`POST https://api.bot.qq.com/app/getAppAccessToken` `{appId,clientSecret}`，≤7200s，有效期内重复申请返回同 token；`Authorization: QQBot <token>` | ✔ `qq.go:95-143,49`，401 刷新重试 1 次 |
| 长度：官方仅体现为 `40054007 消息长度超限`，**无数字上限** | ⚠ `qq.go:34` 的 2000 与 op13 的 ±5 分钟时窗都**不是官方值**，应标注为本地策略 |
| 主动消息限流：单聊 10 qps / 群 60 qpm 等；终态码 40034101/40054002/40034006/40034128/304103 | ✘ 无限流、无码表分类（全落 default→retryable） |

### 3.7 微信公众号 — [加解密](https://developers.weixin.qq.com/doc/oplatform/Third-party_Platforms/2.0/api/Before_Develop/Message_encryption_and_decryption.html) / [客服消息](https://developers.weixin.qq.com/doc/subscription/api/customer/message/api_sendcustommessage.html)

| 契约 | 仓库现状 |
|---|---|
| 明文 `SHA1(sort(token,timestamp,nonce))`；安全模式 `msg_signature=SHA1(sort(token,timestamp,nonce,encrypt))` + AES-256-CBC(EncodingAESKey) | ⚠ `wechat.go:103-108` 算法 ✔ 但 `expected == signature` **非常数时间比较**；A 形态路由要求 `X-Wechat-*` **头**（`webhook_channel_wecom.go:81-99`），微信从不发这些头 → 只有 B 静态路由可用 |
| 客服消息 48h 窗口，`/cgi-bin/message/custom/send`；无群聊 API | ⚠ 无 48h 窗口守卫；出站 `wechat.go:229-294` |
| 45015 客服频率 / 45009 频控 / 40001 凭据 / 43004·43005 需订阅 | ✘ 出站**吞错**：`webhook_outbound.go:640-645` 只记日志，不调 `markSendFailed` → 永远 `(false,nil)`，不重试 |

### 3.8 抖音 / TikTok / 快手 / 小红书 / 闲鱼 / custom —— 官方能力决定它们是设计约束

| 渠道 | 官方消息 API | 门槛 | 官方回调验签 | 可测性 |
|---|---|---|---|---|
| 抖音 | 部分：小程序 IM（自有小程序内）、企业号私信在**巨量引擎商业开放平台**、飞鸽电商客服 | 企业认证 / 广告主 / 商家+ISV | 小程序镜像微信（SHA1/AES+msg_signature）；dop webhook 用 `X-Douyin-Signature` | ✘ 无公开沙箱 |
| TikTok | 部分：Business Messaging / DM API | 应用审核 + DM 白名单 | `TikTok-Signature = HMAC-SHA256(timestamp + "." + payload)` | ✘ 需获批应用 |
| 快手 | **无**客服私信 API；自助仅「订阅消息」；客服在电商开放平台 | 商家/ISV | `kwaisign = MD5(body + secret)` | ✘ |
| 小红书 | **无**公开私信/客服 API；私信通走聚光+服务商协议 | ISV | 合作方登录墙内，**未能确认** | ✘ |
| 闲鱼 | **无任何公开 API**（仅淘宝开放平台闲鱼类目 ISV 合同）；社区"闲鱼API"库均为逆向 | ISV | — | ✘ |
| custom | — | — | — | ✘ `chat_channels.app_secret_hash` 长度为 0，无凭据 |

仓库现状与上述结论的冲突（均本会话读码坐实；✅ 表示批C 已收口，落点见 §9）：

1. ~~**`tiktok` 没有自己的解析器**~~ ✅ **D-01 已修**：原 `webhook.go` 直接复用 `dispatchDouyin`、hub `Platform` 硬编 `"douyin"` → TikTok 数据以抖音身份入库，出站也按抖音桥走。现按渠道分平台落库（`webhook_channel_douyin.go` 的 `douyinPlatformKeyPrefix`：tiktok → `("tiktok","tt")`），商机行、出站适配器、幂等键前缀全部跟渠道走。
2. **验签算法**：原 `verifyHMAC` 只对 **body** 做 HMAC-SHA256，与三家官方都不符。✅ **D-04 已按官方口径分开处理**：TikTok 独立走 `verifyTiktokWebhook`（`timestamp + "." + body` 的 hex HMAC，头名大小写无关）；抖音/TikTok 不再共用一个分支。**快手/小红书** 未改（见 §6：取不到可引用的服务端回调签名原文，不抄来路不明的算法），且它们的入站在 D-03 能力闸之后 HTTP 层已不可达。
3. ~~**`kuaishou` / `xiaohongshu` / `xianyu` / `custom` 的 A 形态路由是死路**~~ ✅ **D-03 已收口**：`Receive` 新增 `webhookInboundCapable` 能力闸，无适配器渠道直接 400 + 指路 `/api/bridge/ingest`，不再"收下并回 200"（那会让渠道方停止重投而消息凭空消失）。
4. ~~**桥接五渠道的 `sent=true` 是记账值不是投递值**~~ ✅ **D-02 已修**：`hubMsg==nil`、目标不可达、出站未落库三种情形不再返回 `sent=true`，只有真正交接进桥接出库队列才算。

## 4. 凭据现状（真实测试可行性判定）

实测线上库 `user_db`（`pg_stat_user_tables` 统计严重过期，以下均为精确 `COUNT(*)`）：

| 渠道 | 凭据表 | 行数 | 密钥字段 | 判定 |
|---|---|---|---|---|
| telegram | `telegram_accounts` | 2（id 5 `hivemtk_bot`、id 9 `lovesanrenxing_bot`） | bot_token 46 字符、webhook_secret 64 字符，**`getMe` 实测各返回 `ok:true`** | **可真机端到端** |
| wechat / wecom / feishu / dingtalk / whatsapp / qq | 各自 `*_accounts` | 0 / 1 / 16 / 4 / 26 / 0 | 全为占位：`corp_id='hack_corp_001'`、`app_id` 是 UUID（真实为 `cli_*`）、`app_secret='auto_<ts>'`、`token`/`aes_key` 长度 0 | **无凭据** |
| douyin / kuaishou / xiaohongshu / xianyu / tiktok | `bridge_accounts`（无密钥列）+ `integration_accounts` | 36 / 19 | api_key/secret/access_token 长度 0 | **无凭据** |
| custom | `chat_channels` | 1 | `app_secret_hash` 长度 0 | **无凭据** |

`.env` 里唯一真实渠道凭据是 `TG_BOT_TOKEN`；`EMBEDDING_API_KEY`/`RERANK_API_KEY` 为空（本地自建 :8208/:8209 提供）。
外联未被阻断：`api.telegram.org` 302、`graph.facebook.com` 400、`open.feishu.cn` 404、`qyapi.weixin.qq.com` 403、`api.dingtalk.com` 200、`open.douyin.com` 200、`bot.qq.com` 302，仅 `api.kuaishou.com` 不通。

**因此本审计的测试口径**（不是退而求其次，是唯一诚实口径）：

- Telegram —— 真机端到端（真 token、真投递、真 429/长度行为）。
- 其余 12 渠道 —— **官方契约 fixture 测试**：入站按官方算法造签名/密文喂给真实验签与解析函数，出站用 `httptest` 假上游断言**请求形状逐字段等于官方文档**。这类测试能抓出 §3 里所有 ✘，且不依赖凭据、不会因沙箱缺失假绿。
- 桥接五渠道 —— DOM/选择器契约测试 + 桥协议测试，**不得**写成"上游 API 已验证"。

## 5. 偏差清单（按可测性与影响排序）

| # | 渠道 | 偏差 | 证据 | 严重 | 批次 |
|---|---|---|---|---|---|
| C-01 | 飞书 | 群聊 `receive_id_type=open_chat_id` 非官方取值，群发必被拒 | `webhook_outbound.go:430`、`feishu.go:215,226` 坐实 | P0 | A |
| C-02 | 飞书 | `content` 未按契约 JSON 字符串化；正解 helper 未接入 | `feishu.go:219-223` vs `:1010` 坐实 | P0 | A |
| C-03 | 企微 | 解密 IV 取密文首块并跳过之，长度域偏移错 16 字节 → 加密回调与 URL 验证均解坏 | `webhook_channel_wecom.go:122-140` 坐实 | P0 | A |
| C-04 | 钉钉 | 机器人明文回调被硬拒；带 encrypt 的体零验签放行（可伪造） | `dingtalk_app.go:101-120` 坐实 | P0 | A |
| C-05 | 钉钉 | 出站吞掉传输错误与 `errcode!=0`，不进重试通道 | `webhook_outbound.go:613-629` 坐实 | P1 | A |
| C-06 | 微信 | 出站只记日志不调 `markSendFailed` | `webhook_outbound.go:640-645` 坐实 | P1 | A |
| C-07 | QQ | 单聊事件名 `C2C_AT_MESSAGE_CREATE` 官方不存在 → 单聊全丢 | `channelbot/qq/qq.go:386` 坐实 | P0 | A |
| C-08 | 钉钉 | 自定义机器人加签参数拼 `ts=`，官方为 `timestamp=` → 310000 | `dingtalk.go:58` 坐实 | P1 | A |
| S-01 | 飞书 | EncryptKey 缺失即 `return true` fail-open | `webhook.go:540-546` 坐实 | P1 | B ✅ §8 |
| S-02 | 全部 | `ALLOW_INSECURE_WEBHOOK=true` 一刀切绕过**所有**渠道验签 | `webhook.go:475-477` 坐实（:463 仅启动期环境护栏） | P2 | B ✅ §8 |
| S-03 | 微信 | 签名 `==` 比对，非常数时间 | `wechat.go:108` 坐实 | P2 | B ✅ §8 |
| S-04 | TG/WA/飞书/企微/QQ | 官方均明示重投/重复推送，仓库缺按官方去重键的幂等 | §3 各行 坐实 | P1 | B ✅ §8 |
| S-05 | 钉钉/企微 | 官方 1 小时 / nonce 两小时时窗，仓库全无 freshness 检查 | `dingtalk_app.go:77-82`、`webhook_channel_wecom.go:90-98` 坐实 | P2 | B ✅ §8 |
| D-01 | tiktok | 复用抖音解析且 `Platform` 写死 `douyin` | `webhook.go:750-751`、`webhook_channel_douyin.go:83,133` 坐实 | P1 | C ✅ §9 |
| D-02 | 抖/快/红/闲/TikTok | 出站返回 `sent=true` 但只是 DB 交接，`failed` 行与 `hubMsg==nil` 也算成功 | `webhook_outbound.go:690-704,779-813` 坐实 | P1 | C ✅ §9 |
| D-03 | kuaishou/xiaohongshu/xianyu/custom | A 形态路由死路（不落库、不触发 AI、不可出站） | `webhook.go:754-756`、`webhook_outbound.go:814-817` 坐实 | P2 | C ✅ §9 |
| D-04 | 三家验签 | `verifyHMAC` 只签 body，与 TikTok(`timestamp.payload`)/快手(`MD5(body+secret)`)官方均不符 | `webhook.go:600-618` 坐实 | P2 | C ✅ §9（TikTok 已按官方实现；快手/小红书无官方出处可抄，且入站已被 D-03 闸挡住） |
| N-07 | 抖音/TikTok | 结构化分支缺「取不到发送人即非消息事件」守卫：URL 校验/审核/授权类通知照样落一行 sender 为空、content 兜底成 `"[tiktok message]"` 的 `message_hub`，进收件箱还顺带驱动一次 AI 回复 | 读码坐实（`dispatchDouyin` 结构化分支 vs `dispatchDouyinGeneric` 早已有同规则）；处置见 §9.2，代码内注释标为「D-04 连带发现」 | P1 | C ✅ §9.2 |
| N-08 | 企微 | 入站链路**四处**独立写死 JSON（验签取 encrypt、`ParsePayload`、`officialEventID`、解密明文），带 `<xml>` 外壳的回调在第一步就 400，整个渠道的该形态一条进不来；连带：hub 层按官方 `MsgId` 收敛重投时返回的是 error，`dispatchWeCom` 原样上抛会让 `handleJob` 用取不到正文的外壳落一条空内容 `unified_message` | 红测实跑坐实：`signature mismatch`（第四段是空串）/ `invalid character '<' looking for beginning of value` / `officialEventID` 返回 "" —— 三条均在修复前实跑复现、修复后转绿 | P0（若官方按 XML 推送则整渠道失联） | C ✅ §9.2 |
| N-09 | 企微 | N-08 的第一版修复自带三处：验签**之前**的 XML→map 递归没有深度上限（2 MiB 全塞 `<a>` ＝几十万层，未验签请求即可打爆栈）；嵌套子元素的文本被折成空 map（`<Image><MediaId>` 解成 `{}`）；②修好后**调用点仍只查顶层**，嵌套形态的 `media_id`/link 的 `Title`+`Url` 解得出来却没人取，link 正文还会算成"一个空格"落库 | 红测实跑坐实：`--- FAIL: TestN08_XMLDepthIsBounded … got 1 个字段`、探针 `--- FAIL … got map[string]interface {}{}`、`--- FAIL: TestN08_DispatchWeCom_NestedXMLFieldsReachHubContent … got " "`、`--- FAIL: TestN08_DispatchWeCom_NestedMediaIDReachesHubRow … got ""`；修复后同组全绿 | P0（预认证 DoS）/ P1（媒体与链接字段） | C ✅ §9.2 |
| M-01 | TG/QQ/钉钉/公众号（企微的嵌套取值已随 N-09 处置） | 入站媒体被丢弃：解析层根本没有媒体字段，归一层也没有承载位，四类渠道各自把 MsgType 写死成 text | 逐渠道复核完毕（原引用 `telegram.go:735-737` 路径不完整，现按当前行号给出）：<br>**TG** `internal/channelbot/telegram/telegram.go:653-665` 的 `TGMessage` 只有 `Text`/`Caption`，photo/video/voice/audio/document/sticker **一个字段都没有**；`ToInbound` 取 `Text‖Caption` 且 `MsgType:"text"` 写死（:735-737、:764）；归一层 `core.InboundMessage`（`core/core.go:161-174`）无媒体字段；全仓无 `getFile` 调用 ⇒ 即便解析出来也没有下载能力<br>**QQ** `channelbot/qq/qq.go:448-474` 两个分支都 `Content: d.Content` + `MsgType:"text"`；`webhook_channel_qq.go:108` 再写死一次 `model.MsgTypeText`，且 `:114-116` 把空正文兜成 `"[qq]"` ⇒ 纯图片消息落成一行 `[qq]`<br>**钉钉** `dingtalk_app.go:135-152` 的结构体只声明 `content.content` 与 `text.content`，图片/语音/视频/富文本携带的 `downloadCode` 无处落，`:156-160` 空正文兜成 `"[" + msgtype + "]"`，`:176` MsgType 写死 text<br>**公众号** `service/wechat.go:124-126` **解析得出** `MediaID`/`PicURL`/`Format`，`controller/wechat.go:261-276` 构造 `MessageEvent` 时只带 `Content`，三个字段全丢（`model.MessageEvent` 本有 `MediaURL`，是调用方没填）。批F-4 排查中扩到**六渠道**：飞书（`[media]`/`[post]` 越词表 + 富文本正文蒸发）、企微（`voice`/`shortvideo`/`mixed` 越词表 → **整条消息被拒**）与"半截文件当完整转存"（六处入站媒体读取缺超限拒收）一并归入本条，归一层口径见 N-17 | P2 | **已修（批F-4a/b/c/d/e/f）**：TG 见 §14（九个容器 + `getFile`/下载 + 账号级幂等键 + 36 条变异电池），钉钉/QQ/飞书/企微/公众号见 §15（逐渠道落点 × 用例 + M1–M4 反证）。残留：入站 `sent_at` 只有钉钉跟了官方时间戳（N-21）、钉钉 `conversationType`/`atUsers`/`isAdmin` 仍未读（N-22） |
| M-02 | WhatsApp | 一次推送含多条 message 时，**只有首条驱动 AI 回复**（其余各条照常落 `message_hub`/收件箱/线索，但返回给 `handleJob` 的是 `firstHub`，而 `handleJob` 用 `payload.Content` 当 `SalesRequest.UserMessage`） | `webhook_channel_whatsapp.go` 的 `if firstHub == nil` 段 坐实（原证据 :46-66 是已删除的乱序缓冲入站块，见 N-04）；红测复现：`--- FAIL: TestM02_WhatsAppMultiMessageBatchFeedsEveryMessageToAI … 驱动 AI 的 payload.Content = "多少钱-m02-…"，want "多少钱-m02-…\n有现货吗-m02-…\n能开发票吗-m02-…"（同推送内的后几条没进推理输入）` | P1 | **已修（批F-3）**：dispatch 循环把每条正文累进 `contents`，末尾 `p.Content = strings.Join(contents, "\n")`——口径与中台批量入口一致（N 条合成一份输入、一次回复），避免"一次推送三条问题只答最后一条"或"抢答首条"；`firstHub` 仍作为 hub 返回值供媒体回填与幂等键使用。证据与变异电池见 §13 |
| M-03 | 全部非 TG | 仅 Telegram 在客户端内做 429/退避，其余靠上层重试通道 | §3 各行 | P2 | 待排（与 N-11 一并处置） |
| N-10 | WhatsApp | 媒体转存的**回填键用错**：`persistWhatsAppMediaAsync` 把 `media_id` 当成消息 ID 传给 `EnrichHubMediaURLByMsgID`，而 hub 行的 `msg_id` 是 **wamid**（`webhook_channel_whatsapp.go:102` `MsgID: msg.ID`），函数内的兜底 `UpdateMediaURLByMsgID` 同样按 `msg_id` 查 ⇒ 主路径与兜底两条都命中 0 行，长期 URL 算好了却永远落不回那行，`media_url` 停在占位（企微/飞书两处传的都是真 msg_id，只有 WA 传错）。连带：入口只调 `MediaRef()`（只返回**第一条**媒体消息），一条推送里多条媒体时其余根本不转存 | 读码坐实（三处文件:行号 + 签名第 5 参形名为 `msgID`，`channel_media.go:289`）；红测复现待批F 首跑（先证明"传 media_id 时查不到行、传 wamid 时回填成功"） | P1 | **已修（批F-2）**：`WAMessageRef` 带上本条 `MsgID`（= wamid = hub 行的 `msg_id`），由 `MediaByMsgID()` 一次性建索引，转存任务在 dispatch 循环内**逐条媒体**各起一次、回填键用该行的 wamid；旧的 `MediaRef()`（只回第一条媒体）删除。连带 N-10b：`media_id`/`mime_type`/`filename` 随入站事件落进 `message_hub.Extra`（media_id 只有 7 天有效，转存成败都要留痕，AI 侧理解「[图片]」背后的原件也靠这几个字段）。证据与变异电池见 §12 |
| N-11 | 全部出站渠道 | **渠道给出的限流信号在归一层被逐段丢掉**（四处独立环节，全部由读码 + 逐行核对本仓真实产出的错误串坐实）：<br>① **退避秒数取不到**：`reRetryAfter = retry_after[ ":]+(\d+)` 的字符类没有 `=`，而本仓**所有**产出点用的都是 `retry_after=` —— `channelbot/telegram/telegram.go:174`（`tg send 429 (rate limited, retry_after=%ds)`）、`:453`（`tg %s 429 (retry_after=%ds)`）、`service/whatsapp_tier.go:223`（`retry_after=%s` 是 Go Duration，形如 `1m0s`，即使补了 `=` 也不是"数字秒"）⇒ 解析恒不命中。<br>&nbsp;&nbsp;&nbsp;&nbsp;**本轮红测校正 ① 的口径（不要照抄上面那句当结论）**：Telegram 那两条串的等待值改前**取得到**——因为 Raw 原样回显了响应体，体内的 `"retry_after":31`（JSON 冒号形态）恰好落在旧字符类里。真正恒不命中的只有 `service/whatsapp_tier.go:223` 的 Duration 形态，以及任何"只有 `=` 形态、没有响应体回显"的串。⇒ 旧实现在 TG 上是**靠回显侥幸取到、不是按设计取到**；这条侥幸有边界（响应体为空/被截断/换成非 JSON 错误体即失效）。夹具本身也差点因此假绿：`TestN11_TGRetryAfterEqualsFormKeepsDelay` 初版直接抄 TG 全串，改前也绿；拆成「`=` 单形态」+「TG 全串」两条后才露出差异（见 §9.3）。<br>② **状态码取不到**：`reHTTPStatus` 要求字面 `status` + 恰好 3 位数字，于是 `:453` 的裸 `429`、`feishu.go:270` 的 `feishu api code 99991400`（8 位业务码）、`wechat.go:302` 的 `wechat send error: 45009 …`、`webhook_outbound.go:632` 的 `dingtalk sessionWebhook errcode=%d`、`qq.go:242` 的 `code=%d` 全都不在判据里 ⇒ 是否落 `rate_limited` 只取决于响应体里是否恰好带 `"Too Many Requests"` 文案，是字符串巧合而非结构判据。<br>③ **渠道码被丢弃**：`wecom.go:525-527` `if result.ErrCode != 0 { return "", errors.New(result.ErrMsg) }` —— 45009 这类码值根本没进错误串；`feishu.go:206-208` 把拉 token 的真实错误只写日志、对外返回常量 `"get feishu access token failed"` ⇒ 授权类失败落进 `default:` 变成 `CategoryUnknown + Retryable:true`（该 fail-fast 的被无限重试）。<br>④ **拿到了也没人用**：`retryDelaysFor` 全仓唯一非测试调用点是 `webhook_ai.go:31`（补触发路径），出站重试另有自己那张常量表 `sendRetryBackoffs = {60s, 2m, 4m}`（`webhook_outbound.go:134-138`），`enqueueSendRetry` 只看 `ce.Retryable` 布尔、从不读 `ce.RetryAfter`（`:162-180`）⇒ **`ChannelError.RetryAfter` 对出站时序是死字段**，光改正则不接入等于没改 | 逐条核对本仓真实产出形态（上列 file:line 全部实读原文）；既有 `channel_error_test.go:24` 的夹具是 `status 429: {"…retry_after":31}`（JSON 冒号形态，恰好匹配），从未覆盖任何一条真实产出串 ⇒ 又一个"夹具与实现互相漏项"。红测待批F | P1（限流期按固定梯度反复早退避 → 二次触发封禁；授权失败被无限重试） | **已修（批F-1）**：新增 `internal/channelbot/core.APIError`（只装事实：Channel/StatusCode/Code/RetryAfter/Raw，由各客户端在解析响应的当场构造；放这层是因为 `channelbot` 不能反向依赖 `service`，而那三个事实只在解析现场存在）；`service.ChannelError` 增 `Channel`/`Code` 两个事实位与 `quota_exhausted`/`window_expired` 两个类别；判据顺序＝**渠道业务码表（按渠道分表，45009 同号不同义）> HTTP 状态码 > 文案**，文本分类降为兜底；`nextSendRetryAt` 接上 `ce.RetryAfter`，口径"只顺延不提前"；飞书读 `x-ogw-ratelimit-reset`；TG（含 Markdown 兜底分支）/QQ/WA 客户端与企微/公众号/飞书/pacing 构造点共 7 处一律带出码值。证据与变异电池见 §11、§10「批F-1 执行时对本表的两处偏离」 |
| N-12 | 微信公众号 | **安全模式（消息加解密）整条链路不存在**：账号侧把 `encoding_aes_key` 收下来并写库（`controller/wechat.go:68,84,115` → `repository/wechat_account_repo.go:79`、`model/wechat_account.go:15`），运行侧却**全仓零读取**（`grep -rn EncodingAESKey internal/ cmd/` 只命中"存入"这三处，没有任何解密点），入站一律按明文 XML 解（`service/wechat.go:142` `xml.Unmarshal`）。商家一旦在公众号后台启用安全模式：外层 `<xml><ToUserName>gh_x</ToUserName><Encrypt>…` 解出来 `MsgType`/`Content` 全空，`controller/wechat.go:261-276` 照样把这条**空消息**送进 Ingress ⇒ 表现为「配置成功、验签通过、200 OK，但一条客户消息都收不到」，与 N-08 在企微侧的形态同源 | 读码坐实（配置面与运行面的差集由上述 grep 给出）；红测待批F：造官方加密外壳（AESKey=Base64(EncodingAESKey+"=")、appid 作 receiveid，与 §3.4 企微同一算法）证明当前解出空 `MsgType` | P0（启用即整渠道静默失联） | 待排 |
| N-13 | 钉钉 | 出站**三条"根本没发"的分支返回的是 `sendErr == nil`**：`webhook_outbound.go:579-584`（缺 sessionWebhook）、`:589-594`（已过期）、`:596-601`（域名非法）在 500 行的大函数 `sendOutbound(...)(sent bool, sendErr error)` 里都是**裸 `return`** ⇒ `sent=false` 且 `sendErr=nil`，`:372-375` 的 `markSendFailed` 没走、`:381-385` 的 defer 条件 `sendErr != nil && !sent` 不成立 ⇒ 既不进持久化重试队列、也不写失败轨迹，客户侧最后一条永远停在入站行，只有服务端一条日志。这正是 C-05（批A 已修的"吞掉传输错误与 errcode"）剩下的那半边：**不是吞掉错误，是从未构造错误** | 读码坐实（三处 `return` 与 defer 条件同文件对照，函数签名 :348 具名返回）；红测待批F：断言这三条分支后 hub 行必须带失败轨迹、且不得静默 `sent=false,nil` | P1（客户收不到回复且系统内无痕） | 待排 |
| N-14 | WhatsApp/钉钉（TG/QQ 同源） | 入站**内容窗口去重**（`interceptInbound` 的 `duplicate(channel+sender+content) within window`，TTL 5 分钟）只看"同渠道+同发送人+同正文"，**完全不看平台给的消息 ID** ⇒ 同一客户五分钟内连发两条相同文本、或 Meta 一次推两条图片（占位正文同为 `[图片]`）时，第二条在写 `message_hub` 之前就被拦掉：既不入库、也不转存媒体、也不进收件箱。平台 at-least-once 重投本来由 `message_hub` 的 `(platform, msg_id, conversation_id)` 唯一索引 + `isDuplicateKey` 幂等兜底，内容窗口去重只对"没有稳定消息 ID"的事件才有独立价值。钉钉侧另有一半原因：入站事件根本没带 `channel_msg_id` | 红测实跑坐实（两处现场，均在批F-2 写媒体用例时跑出）：`--- FAIL: TestN14_RepeatedContentWithDistinctWAMIDsIsNotDropped … msg_id=wamid-n14-2 没入库（record not found）`、`--- FAIL: TestN14_DingTalkRepeatedTextIsNotDropped … 实际 1 行`，配套服务端日志 `dup=true event_id=wamid-n14-2 reason="duplicate(channel+sender+content) within window"`；钉钉那条 `event_id=dt-1-m-n14-2` 说明它的 EventID 本来就带 msgId，只是没往 Extra 里放 | P1（真实客户消息静默丢失，且现象随内容重合而随机复现） | **已修（批F-2）**：`interceptInbound` 的内容窗口去重加守卫 `chanMsgID == ""`（带稳定渠道消息 ID 的事件不进入内容窗口），`channelMsgIDOf` 的取值上移为函数内单一变量供两处复用；钉钉入站事件补 `"channel_msg_id": msg.MsgID`。反向边界同样钉住：无 ID 事件（含 `wa-out-`/`tg-out-` 占位 ID）仍按内容去重，防止重投双份入库。证据与变异电池见 §12 |
| N-15 | TG | 下载 URL 本身带 bot token（官方 `https://api.telegram.org/file/bot<token>/<file_path>`），而 `*http.Client.Do` 的传输层错误是 `*url.Error`，`Error()` **原样回显 URL** ⇒ 一旦下载腿抖动，bot token 就进错误日志、进而进工单/告警。同类风险在所有"凭据置于 URL 路径"的渠道都成立，TG 是当前唯一真会拼出这种 URL 的 | 读码坐实（官方原文见 §14.1：`file/bot<token>/<file_path>`）；红测 `TestM01_TelegramTokenNeverInError`（关端口，`GetFile`/`DownloadFile`/`SendMessage` 三条腿逐个断言不含 token 且 `errors.As(syscall.Errno)` 仍成立）改前实跑红 | P1（凭据泄漏，且泄漏面随日志采集外扩） | **已修（批F-4b）**：`Client.scrubToken`（`channelbot/telegram/telegram.go:70-84`）只在确实含 token 时套 `redactedError` 壳、`Unwrap` 保留底层错误（否则出站归类断链，把限流/抖动误判成不可重试）；不含 token 与 nil 原样返回（`TestM01_TelegramScrubKeepsUntokenedErrorIntact`）。见 §14.3 |
| N-16 | TG | 入站幂等键 `tg_upd_<update_id>` **不带 account_id**：`update_id` 只保证**单个 bot 内**单调，两个 bot 各自数到同一 `update_id` 是常态（库里现有 2 个 telegram 账号）。落 `message_hub` 时撞 `(platform, msg_id, conversation_id)` 唯一键 ⇒ 第二个 bot 的这条客户消息被当成重复**永久丢弃**，且表现为"什么都没发生" | 读码坐实；红测 `TestM01_TelegramSameUpdateIDAcrossAccounts`（同 `update_id` 两账号，改前第二行 `record not found`）+ 变异 T8「幂等键不带 accountID」caught=15 | P1（多 bot 部署下真实消息静默丢失） | **已修（批F-4b）**：`Update.HubMsgID(accountID)` 把账号并入键；`TestM01_TelegramHubMsgIDPrecedence` 8 子例钉住取值优先级，`TestM01_TelegramBackfillScopedToAccount` 钉住回填同样按账号收窄。见 §14.3 |
| N-17 | 企微/飞书/WhatsApp/公众号/TG/钉钉 | **渠道官方类型名 ≠ 中台 `msg_type` 词表**，三种失效形态各不相同：① 企微把官方名直接喂 `model.MessageHub` 校验 ⇒ `voice`/`shortvideo`/`mixed` **整条消息被拒**（客户发了消息，系统里一行都没有）；② WhatsApp/公众号的 `[图片]` 类占位落到词表外；③ 飞书 `post` 富文本连正文一起蒸发 | 逐渠道读码 + 红测（`webhook_batchf4_msgtype_test.go` 的 N17 组：映射表 :53、未知类型不拒收 :108、别名表自洽 :124、超限拒收 :152、源码闸 :215/:258、飞书 post :349/:386）。第一现场红字：`以下 hub 类型字面量越出中台词表…[webhook_channel_telegram.go:200 → "message"]`。闸的反证见 §15.5 M1–M4（钉钉 D1–D12、企微 W1–W9 两条电池**尚未跑过**，状态见 §15.8） | P0（企微：官方类型一到就整条丢）/ P1（其余） | **已修（批F-4d/e/f）**：单一归一层 `InboundHubMsgType` + `inboundHubMsgTypeAliases`（`service/message_hub.go:84-127`），兜底**刻意不对称**（官方名越表→退回 text 保正文；占位符越表→保留官方名，可观测）；每渠道源码闸 + 性质测试。见 §15.1、§15.2 |
| N-19 | 中台（全渠道共用） | 补触发判据 `HasUnrepliedCustomerMessage` 的 WHERE **只有 `conversation_id`**，不带 `platform`/`account_id`，而 `ORDER BY sent_at DESC` ⇒ 同号会话（固定夹具、或真实的跨渠道 id 碰撞）会把**别的账号、别的渠道、别的历次运行**的行算进同一段会话 | 全量门禁实跑坐实（不是推断）：`钩子3：最后一条 inbound 超过 5 分钟…conv_id=cid-77 event_id=dt-1-m-77` —— `cid-77` 那次运行里根本没有新消息，被判"未回"的是历史行 | P1（跨账号判据串味：漏触发或误触发 AI） | **待排**：改键要同时评估既有依赖（同一用户在多渠道会话 id 恰好同号时的查询语义），不在批F-4 范围内动 |
| N-20 | 测试侧（钉钉承载用例） | `f4DtSetup` 没掐媒体两条腿 ⇒ 5 条带 `downloadCode` 的用例各真发一次 `POST https://api.dingtalk.com/v1.0/oauth2/accessToken`（假凭据），日志回显 5 个真 `requestid` 与 `invalidClientIdOrSecret`。危害不是错，是**慢 + 依赖外网 + 观测噪音**，且与 QQ 承载用例（早已 stub）不一致 | 实跑日志坐实（5 个真 requestid） | P3（测试卫生） | **已修（批F-4c）**：`f4DtSetup` stub `dtMediaFetchFn`/`dtMediaStoreFn` 并 `t.Cleanup` 还原；转存腿仍由 `…_download_test.go` 逐字段断言。见 §15.7 |
| N-21 | WA/TG/飞书/QQ/抖音 | 入站 `sent_at` 仍写 `time.Now()`：客服侧看到的是"处理时刻"而不是"客户发送时刻"，时序、响应时长统计、跨渠道对齐全被污染；重投事件的时序也因此失真。钉钉半场已在批F-4c 修掉（正是那次改动暴露了全量门禁的 3 条时间炸弹红） | 读码坐实：`webhook_channel_whatsapp.go:97`、`webhook_channel_telegram.go:159,202,322`、`webhook_channel_feishu.go:219`、`webhook_channel_qq.go:112`、`webhook_channel_douyin.go:108,166` | P2 | **待排**：逐渠道官方字段与**单位**不同（WA `timestamp` 秒、TG `date` 秒、飞书 `create_time` 纳秒字符串、QQ `timestamp` 对象），必须先逐渠道取 A 档原文再改，不能一把梭。见 §15.7。<br>**取证现状（2026-09-20 复核 `/tmp/chandocs` 全 88 份）**：TG 已有原文（`tg_api.html`（860,075 B ⇒ `https://core.telegram.org/bots/api`，页内 `<title>Telegram Bot API</title>` + 自链 `/bots/api` 佐证）内 `Message.date` 明写 "Date the message was sent in Unix time"=**秒**；佐证 `tg_wh.html`（59,319 B ⇒ `/bots/webhooks`「Marvin's Marvellous Guide to All Things Webhook」）13 处示例 `"date":1441645532` 为 10 位）；抖音 `create_time` 13 位毫秒（`jina_dy_mini_private-msg-webhook.txt:64` 明写"13位毫秒时间戳"）；钉钉 `timestamp` 毫秒（`jina_dt_recv.txt:157`，已随批F-4c 落）。**缺原文**：WA（`jina_wa_wh.txt`/`jina_wa2.txt` 内 `timestamp` 零命中）、QQ（`qq_msg.html` 同零命中）、飞书入站事件（缓存里只有出站 `message_create`）⇒ 这三家的时间戳字段与单位**未取证**，批N-21 开工前必须补取，不许按"惯例是秒/纳秒"推断。<br>**本轮补取失败实录（不得当作已取证）**：对 Meta 开发者站的一次抓取返回了 `timestamp`=Unix 秒 + 样例 `"1749416383"`，但**实际取回内容的地址是被改写成带签名的 OSS 代理链接**（`routify-file-proxy-sg.oss-ap-southeast-1.aliyuncs.com/…?Expires=…&Signature=…`），与请求的官方域不是同一来源，且回文里没有逐字引文与页面自证 ⇒ 出处不可核验，按 §6 规矩**拒用**；对官方域重试则回 `403 / code 10605`（服务排队）。结论：WA 入站时间戳仍是未证项。<br>**同一改写第二次复现（2026-09-20，QQ 侧）**：为取 `getAppAccessToken` 的响应字段类型抓 `bot.q.qq.com/wiki/…/interface-framework/api-use.html`，工具回显的实际取回地址同样是被改写的带签名 OSS 代理链接（`routify-file-proxy-sg.oss-ap-southeast-1.aliyuncs.com/…?Expires=…&Signature=…`）；且**对同一 URL 连抓两次内容互不相容**（一次给出 `{"access_token":…,"expires_in":"7200"}` 成功体，一次称端点为 `https://api.bot.qq.com/app/getAppAccessToken` 而仓库生产用的是 `https://bots.qq.com/…`）⇒ 两次全部拒用，QQ 的 `code` 字段类型与 token 端点域名都记为未证项。由此暴露的仓内自相矛盾按批J 登记、不按推断改：`qq.go:74` 声明 `Code int`，而既有测试夹具写 `"code":"100014"`（字符串）——已实测该夹具走的是 **parse 分支**而不是它名字声称的 token 失败分支，详见 §17.7 |
| N-22 | 钉钉 | 入站 `conversationType`（官方 1=单聊 2=群聊）、`atUsers`、`isAdmin` **三个字段全仓零读取** ⇒ 群/单聊不分（ outbound 侧只能靠 `sessionWebhook` 蒙）、@机器人判定缺失（群里误回/漏回）、管理员身份丢失 | 读码坐实：`grep -rn "conversationType\|atUsers\|isAdmin" internal/` 在钉钉入站链路零命中（仅有的命中在 `repository/scope/tenant.go` 与 `content/controller/marketing_flow.go`，与钉钉无关）；§3.5 入站字段行同步更新 | P2（群聊场景语义错，但需真机群聊才暴露） | **待排（本轮新记，批F-4 收尾时发现）** |
| N-23 | 门禁侧（`config_param_guard_test.go` D12 规则） | 架构闸把源码**整文件原文**（含注释）喂给"禁止出现 `system_config_kv`"的判定 ⇒ 别的改动线在注释里**提到**这个表名就把门禁判红。第一现场：D12 红字指向 `internal/service/ltc_config.go`，而该文件的两处命中都在注释里（`git status` 显示文件干净、是已提交内容），真正的新增点根本不存在。危害不是误报本身，是**误报会把人推向"往白名单里加豁免"** —— 那等于给这条闸开个永久的洞 | 实跑坐实 + 反证：用 `go/parser` 剥注释后重跑判定即绿；再在**代码**（非注释）里加一处 `system_config_kv` 字面量，门禁仍判红（反证证明修复没把闸放松） | P2（门禁可信度；误修方向会削弱闸） | **已修（批G 收尾）**：`goCodeOnly()` 用 `go/parser` 把注释替换为等长空格后再匹配，解析失败时**退回原文**（判得更严而不是更松）；原注释式豁免（写 `禁止新增`/`D12` 绕闸）随之失效，无需白名单 |
| N-24 | 测试侧（钉钉媒体转存用例） | `…_download_test.go` 的 4 条用例**先装自己的替身、再调 `f4DtSetup`**，而该 helper（N-20 的修复产物）会把 `dtMediaFetchFn`/`dtMediaStoreFn` 换成"永不发起真实下载"的替身并登记还原 ⇒ 调用方的替身被静默覆盖：3 条下载断言红，第 4 条"不该下载"的计数用例反而**假绿**（它的 0 是覆盖出来的，不是行为）。N-20 只修了"helper 要掐腿"，没修"掐腿的顺序" | 实跑坐实：`-run 'TestM01_DingTalk'` 7 条里 3 红 1 假绿；把夹具补上 `robotCode` 反向测出「缺 robotCode 却起了 1 次下载，want 0」，证明那条计数断言此前是空的 | P2（假绿比红更贵：它把"没测"伪装成"测过"） | **已修（批G 收尾）**：4 条用例统一改成"先 `f4DtSetup` 再 `g2bSeams` 式装替身"，还原交给 helper 的 `t.Cleanup`；文件头把顺序写成显式约束。`TestG2B_*` 自始按此形状写（`g2bSetup` 明确**不碰**两个替身，见其注释） |
| N-25 | `service/channel_media.go:31-92` | **一整套"渠道媒体按需下载"注册表是死的**：`channelMediaFollower` 类型、`channelMediaFollowers` map、`RegisterChannelMediaFollower`、`fetchChannelMediaFollower`、导出入口 `PersistChannelMedia` 五者自成一圈，全仓（含测试）**没有任何装配期注册、没有任何调用** ⇒ `PersistChannelMedia` 唯一可能的返回值是 `no media follower for channel X`。真正的活路径是各渠道自己的 `*MediaFetchFn` 替身 + `channelMediaPersist`（飞书/企微/WA/TG/QQ/钉钉/公众号/抖音 8 处）。危害不是"多写了 60 行"，是**导出的死 API 会骗后来者**：看见 `PersistChannelMedia(ctx, channel, mediaID)` 以为"延迟取媒体"的能力已就位，接上去拿到一个恒定 error，再往下就有人去补注册表而不是看官方 ID 有效期约束（批G-2b 已证明：抖音/飞书这类 ID 必须**入站当场**换，拖到点开时必失败） | 三段取证（均为只读，未动代码）：<br>① 全仓 `grep`（type=go 与全文件类型各一遍）：`PersistChannelMedia` 仅定义处 1 次、`RegisterChannelMediaFollower` 仅定义处 1 次、`fetchChannelMediaFollower` 仅定义处 + `PersistChannelMedia` 内 1 次、map 只在 `RegisterChannelMediaFollower` 内被写<br>② `git log --all -S`（两个名字各一次）：**只有** `a200aa65`（2026-09-08，单文件 +294 行），提交信息自陈"期间合入同事未提交的渠道媒体转存**半成品**" ⇒ 从未有过调用方，不是后来被删的<br>③ 动态调用面排除：包在 `internal/` 下，`hivemtk-user` 之外的模块**不可能** import；仓内 `MethodByName` 仅 `model/kuaishou_card_test.go` 查 `TableName` 一处，与本案无关<br>另：`scripts/audit-loop/STATE.md:1153` 显示上一轮审计曾把这套 map 当活代码处理（"补 `sync.RWMutex`"），锁加在了死路径上 | P3（无运行时危害；误导性 API + 死锁代码） | **待删（本轮新记，批G-2b 复核 `channelMediaPersist` 调用面时发现）**：删除须与门禁复跑同一窗口（`go build ./...` + 无过滤 `./internal/service/`）。本轮**不删**：该文件上正压着批F-4f 的未提交改动（`git diff HEAD` 18+/5−，即抽出的 `readInboundMedia`），在同一文件上叠一刀删除会让"哪个改动导致哪条红"分不清；且并行线随时可能在补注册（按 mtime 13:24 核对：该文件的最近一次写是本轮 F-4f 自己）。不并入批G-2b 的落点表：它不是抖音链路的组成部分 |

## 6. 官方文档缺口（不得当作已验证）

- Telegram：4096 是 UTF-16 码元还是 Unicode 标量；超限 `description` 文案；终态/可重试码表（官方称码值可变）；`getUpdates.timeout` 上限；畸形 Markdown/HTML 行为。
- WhatsApp：无"乱序"明文表述（只说并发+重复）；未强制要求常数时间比对；去重键是按行为推断；当前实际版本号（示例 v25.0，声明最新 v26.0）。
- 飞书：无数字时窗；未找到 `X-Lark-Retry-Count`；20 000 字符与 150 KB 两处上限未能互相印证；45009 之外的重试建议。
- 企微：`add_msg_template` 与 `externalcontact/message/send` 的可用边界；POST 重投次数；**回调外壳的官方形态（XML vs JSON）** —— `developer.work.weixin.qq.com` 是 SPA，回调页与加解密页均只回 CSS，原文至今取不到。能取到的最近的**同族官方原文**是微信开放平台《消息加解密》（§3.7 那条链接）：明文给出外层 `<xml><ToUserName></ToUserName><Encrypt></Encrypt></xml>`，并说明"消息 XML 体…使用 EncodingAESKey 加密"、查询参数带 `encrypt_type=aes` 与 `msg_signature`。企微与公众号共用同一套加解密方案（C-03 的 IV/布局即据此实现），所以"回调是 XML 外壳"有依据；但**这不等于企微页的原文**，故 N-08 的修复口径仍是 JSON/XML 双收、不声明哪一种是官方形态。同样地，"媒体字段挂在 `<Image>/<Voice>/<Video>/<File>` 子对象下"取到的是**公众号**同族页的形状，企微侧未取到原文 → N-09 的两层查找按"顶层摊平与子对象嵌套都读"实现，不写成官方要求。企微入站 **msgtype 全集**也未取到：`shortvideo` 是否会出现无法确认，故 `isWeComMediaMsgType` 没有把它加进媒体类型（加进去＝把猜测当契约），小视频若真出现则其 media_id 不做长期转存 —— 这一条留作真机回归观测点。
- 钉钉：sessionWebhook 固定时长、机器人文本长度、130101/300001 码、事件订阅重投计划、机器人消息去重建议 —— 均**未找到官方出处**。
- QQ：验签时间戳窗口、内容数字长度上限、回调重投次数、是否存在不签名模式、`X-Bot-Appid` 是必需还是信息性 —— 均**未确认**。
- ~~抖音 dop `X-Douyin-Signature` 的被签字符串：官方页为 SPA，未取到原文 → 按「本地策略」`hex(HMAC-SHA256(secret, body))` 实现并在代码里注明~~ **本条已于 2026-09-20 作废**：`.../dop/develop/webhooks/summarize` 的原文与 Go 示例已通过文本代理取到并落盘（§16.1 第 1 条），官方口径是 `hex(sha1(client_secret ‖ 原始 body))`。本地策略分支已删除，"本地策略冒充契约"的那份实现正是批G 修的死路本体。小红书/快手仍无原文 → 不受理入站（D-03 能力闸）。
- **TikTok 已不再是缺口**：签名格式取到官方原文（https://developers.tiktok.com/doc/webhooks-verification ，2026-09-20 取到）—— 头 `TikTok-Signature: t=<ts>,s=<hex>`，被签串 `ts + "." + 原始 body`，`hex(HMAC-SHA256(client_secret, signed_payload))`。官方**未给**时间戳新鲜度窗口（原文是把"是否容忍时间差"交给实现方），故本轮刻意不加时窗（加窗=本地策略冒充契约，与 S-05 的处置口径不同：企微官方明文写了 nonce 两小时，TikTok 没有对应句子）。
- TikTok 的 URL 校验握手（`challenge` 回显）：官方文档站取该页时 404，**未找到可引用原文** → 仓库不实现臆造的握手分支，`url_verification` 事件按"非消息事件"落库不触发 AI。
- 快手 `kwaisign = MD5(body + secret)`：只在**支付/非回调**类文档里出现过，服务端事件回调的签名规则未取到原文 → **不得**据此改代码（改了就是把猜测当契约）。

以上条目在代码与测试中一律标注为「本地策略」，不得写成「官方要求」。

## 7. 批A 修复与验证记录（2026-09-19）

结论均来自实际运行：每条修复先写红测，再改实现，最后用「逐条破坏实现的变异电池」证明新断言真的会红（变异后还原并比对 md5）。

| # | 修复落点 | 红→绿的用例 | 变异反证 |
|---|---|---|---|
| C-01/C-02 | `feishu.go sendMessageTyped`：`open_chat_id`→官方 `chat_id`；text 的 `content` 序列化为 JSON 字符串；按官方「HTTP 200 + 非零 code」通道判失败 | `feishu_outbound_wire_contract_test.go` 3 条（抓真实出站请求体逐字段比对） | 去掉任一归一即红 |
| C-03 | `webhook_channel_wecom.go`：IV 改为官方 `AESKey[:16]` 且整段解密（原实现把密文首块当 IV 跳过，长度域读偏 16 字节）；`VerifyURL` 删除第二次错位剥离 | `wecom_crypto_official_contract_test.go` 6 条（含独立官方加密实现造夹具） | 还原 IV 取法即红（`msg_len overflow: 1315007845`） |
| C-04 | `dingtalk_app.go`：入站按官方拆两条通道 —— 明文机器人回调校验 header `sign`+`timestamp`（`base64(HMAC-SHA256("<ts>\n<appSecret>"))`、1 小时窗），事件订阅回调校验 query `signature`（`sha1(sort(token,ts,nonce,encrypt))`）并按官方布局 `random16+len4+msg+receiveId` 剥离；两种模式均 fail-closed | `dingtalk_official_contract_test.go` 8 条 + `webhook_dingtalk_fullchain_test.go` 4 条（走真实 gin 路由） | 8 项变异全部命中：sign 比对失效→错密钥用例红；时间窗放开→过期用例红；事件签名失效→篡改用例红；不剥头→解密用例红 |
| C-05 | `webhook_outbound.go` 钉钉分支：传输错误、`errcode!=0`、响应不可解析均 `markSendFailed` → 进持久化重试通道（缺/过期 sessionWebhook、域名非法仍按 `(false,nil)` 口径不入队） | `webhook_outbound_dingtalk_wechat_retry_test.go` 3 条 | 去掉任一 `markSendFailed` 即对应红 |
| C-06 | `webhook_outbound.go` 公众号分支：`SendCustomMessage` 失败改为 `markSendFailed` | 同上 `TestSendOutbound_Wechat_FailureEntersRetryLane` | 去掉即红 |
| C-07 | `channelbot/qq/qq.go`：事件名改回官方 `C2C_MESSAGE_CREATE`（原 `C2C_AT_MESSAGE_CREATE` 官方不存在，单聊消息全被 default 分支静默丢弃），并补 `GROUP_MESSAGE_CREATE` 全量推送模式 | `qq_test.go` `TestEventNames_AreOfficialDocNames`、`TestToInbound_GroupFullPushMode` | 事件名改回错误值即红 |
| C-08 | `dingtalk.go`：自定义机器人加签查询参数 `ts=` → 官方 `timestamp=`（钉钉忽略未知参数，线上表现为 errcode=310000 签名不匹配） | `TestDingTalkSendRobot_SignQueryParamIsOfficialName` | 退回 `ts=` 即红 |

### 7.1 本轮连带处理的两类假绿

- **夹具与实现互相印证**：`webhook_service_e2e_test.go` 的 `encryptWeComPlain` 按「IV 前置到密文」的飞书式布局造夹具，`channels_delivery_fix_test.go` 的 `dtEncryptForTest` 直接加密裸 JSON —— 两者都与当时的错误实现自洽，所以旧测试全绿却测不到真实回调。已删除，改用独立实现的官方加密夹具（`wecomOfficialEncrypt` / `dtOfficialEncrypt`）。同理 `TestDingTalkSendRobot_WithSign` 原本断言 `ts` 参数存在，已改为断言官方 `timestamp`。
- **controller 取参白名单缺口**：`extractHeaders` 不含 `timestamp`/`sign`、`extractQuery` 不含 `signature`，钉钉两条验签通道在 HTTP 层就拿不到签名 —— service 层单测无法暴露。已在白名单补齐，并新增走真实路由的 `webhook_dingtalk_fullchain_test.go` 作为回归守卫。

### 7.2 本轮新发现（尚未处置）

| # |  finding | 证据 | 严重 |
|---|---|---|---|
| N-01 | `GET /api/webhook/dingtalk/{account_id}`（`DingTalkVerify`）按「signature/timestamp/nonce/echostr 四元组 + HMAC-SHA256(ts+\"\\n\"+nonce) 用 token 作密钥」验签，这套规则既不匹配机器人回调（header sign）也不匹配事件订阅（query sha1 四元组）；且官方事件订阅的 URL 校验是 **POST 加密 `check_url`**、需回加密的 `success`，本仓库没有该分支 | `dingtalk_app.go:65-91`、`controller/webhook.go:327-352` 坐实；官方 check_url 交互细节未取到可引用原文（open.dingtalk.com 为 SPA） | P1（配置期握手，不影响已运行账号收消息）|
| N-02 | 「投递失败→重试队列」类用例此前依赖 `DISABLE_AI_QUIET_HOURS` 环境变量才不被免打扰分支提前 return；换机器/换时段即假红或假绿 | `webhook_outbound.go:350-354` + `isAIReplyQuietHours` 读环境变量 | P2（测试卫生，已在新用例内显式关开关）|
| N-03 | 官方 `receive-message` 文档未写机器人消息去重键与重投次数（§6 的「未找到官方出处」经再次核对仍成立）；`conversationType` 官方定义为 1=单聊 / 2=群聊，仓库入站未使用该判别 | 官方页再次核对 | P2 |

## 8. 批B 修复与验证记录（2026-09-19 ~ 09-20）

口径同 §7：先写红测，再改实现，最后用变异电池逐条把新校验改坏，确认对应用例真的变红，再按 md5 还原。
电池脚本：`/tmp/mutation-battery-batchb.sh`（S-01/02/03/05，M1–M5）、`/tmp/mutation-battery-batchb-s04.sh`（S-04 及本轮新发现，M6–M11）。

| # | 修复落点 | 用例 | 变异反证 |
|---|---|---|---|
| S-01 | `webhook.go` 飞书分支：`EncryptKey=="" && 无 X-Lark-Signature` 时不再 `return true`，改为按官方明文模式的**唯一**来源凭据 —— 事件体 `header.token`(v2)/`token`(v1) 与账号 `verification_token` 常数时间比对；两者都没配则 fail-closed 报错。新增 `getFeishuVerificationToken`、`feishuPlaintextTokenMatches` | `webhook_batchb_security_test.go`：`TestVerify_Feishu_PlaintextModeRequiresVerificationToken`(5 子例) / `_MissingBothKeysFailsClosed` / `_EncryptedModeStillNeedsSignature` | M1 把比对改成恒真 → 飞书组红 |
| S-02 | `ALLOW_INSECURE_WEBHOOK` 语义收窄：由「置真即绕过**所有**渠道验签」改为「只豁免**该账号确实没配密钥**的分支」，配了 secret 的账号一律照验；抖音/快手/whatsapp/default 补齐「无密钥即 fail-closed」。启动护栏 `insecureWebhookStartupError` 认 `GIN_MODE=debug`，环境三项全未声明时按**生产姿态拒绝**（正是 `docs/TROUBLESHOOTING.md:230` 记的那个洞） | `TestVerify_InsecureBypassDoesNotSkipConfiguredSecret`、`webhook_test.go:TestWebhookInsecureWebhookGuard`（新增 `ginMode` 列，原「未设置环境=dev 放行」用例改为三条） | M2 `insecureWebhookAllowed` 恒真 → 红；M5 护栏把「环境未声明」当 dev → 红 |
| S-03 | `wechat.go:VerifySignature` 的 `expected == signature` 改 `subtle.ConstantTimeCompare` | `TestWechatVerifySignature_OfficialContract`（固定 ts + 确定性翻位，断言 `good[:39]`/`good+"00"`/`"zz"+good[2:]` 全部拒绝）+ `TestWechatVerifySignature_ConstantTimeCompare`（**源码守卫**：切出函数体，要求出现 `subtle.ConstantTimeCompare`、不得出现 `== signature`） | M4 退回 `==` 比对 → 源码守卫红、行为用例**仍绿**。这条差异就是必须有源码守卫的实证：行为测试无法区分两者 |
| S-04 | 新增 `webhook_event_key.go:officialEventID`，`Receive` 在整包哈希兜底之前按渠道取官方重复投递键：TG `update_id`、QQ 信封 `id`、飞书 `header.event_id`/`uuid`、企微 `MsgId`、WA 本批 wamid 排序集合指纹；键一律带 `<channel>:<accountID>:` 前缀（`webhook_events.event_id` 是**全局**唯一索引且不带账号列，裸官方键会让两个 bot 的 #1 消息互吞） | `webhook_batchb_dedup_test.go`：`TestOfficialEventID_PerChannel`(12 例)、`_WhatsappWamidSet`、`TestReceive_Telegram_OfficialUpdateIDDedupsReencodedReplay`（同 update_id、键序重排 → 必须判重复）、`TestReceive_OfficialEventIDScopedPerAccount` | M6 不取官方键 → 重编码重投用例红；M7 去账号作用域 → 跨账号用例红；M8 wamid 不再排序 → 换序同键用例红 |
| S-04（钉钉） | `dingtalk_app.go`：`EventID: "dt-<account>-<msgId>"` 在事件订阅载荷（无 `msgId`）下退化成常量键，第二条起被 `message_hub` 幂等钩子静默丢弃。新增 `dingTalkDedupKey`，缺官方键时退回载荷内容哈希 | `TestDingTalkInbound_MissingMsgIDMustNotCollapseToConstantKey`：直接断言 `message_hub` 落地行数（两条不同内容 → 2 行、字节相同的重投 → 仍 2 行）。此前按 `fakeAITrigger.called` 断言是错的：同会话第二条会被 `markAIProcessing` 的会话级排他锁挡掉，那是另一层机制 | M9 去掉内容哈希兜底 → 红（真实日志：`event_id=dt-1-` + `钩子2：msg_id 已存在，幂等跳过`） |
| S-05 | `webhook_channel_wecom.go`：按官方 `msg_signature=sha1(sort(token,ts,nonce,encrypt))` 的 `timestamp` 加 2 小时时窗（**本地策略**：官方只写明 nonce 两小时内唯一，未给 timestamp 判据），非数字 ts 直接拒绝；`VerifyURL`（GET 握手）刻意不加窗 | `TestVerifyWeCom_TimestampFreshnessWindow`（fresh / -3h / +3h / 非数字）；连带把 `webhook_service_e2e_test.go:TestWeCom_Verify_OK` 里写死的 `1700000123` 换成当前时间 —— 那个夹具编码的正是修复前的无窗契约 | M3 时窗失效 → 红 |

### 8.1 本轮连带查出的三处（不在原清单里）

| # | finding | 证据 | 处置 |
|---|---|---|---|
| N-05 | **飞书入站整包解析失败**：官方 `im.message.receive_v1` 的 `create_time` 是**字符串**（文档示例 `"create_time":"1609073151345"`；`header.create_time` 为纳秒字符串），而 `dispatchFeishu` 的结构体写成 `int64` → `json.Unmarshal` 直接报错，真实客户消息**一条都落不进 `message_hub`**，且 `handleJob` 只把 dispatch 错误记日志，看起来像"非消息事件被跳过" | 造官方形状夹具即复现：`feishu parse: json: cannot unmarshal string into Go struct field .event.message.create_time of type int64`；官方原文 [消息接收事件](https://open.feishu.cn/document/server-docs/im-v1/message/events/receive) 已确认字符串形态 | 已修：新增 `feishuTimestamp`（对字符串/数字两种形态都容错）。`feishu_inbound_wire_contract_test.go` 3 子例锁住两种形态；M10 退回 `int64` 即红。**旧用例之所以全绿**：`channel_fullchain_e2e_test.go:448` 的夹具根本没写 `create_time` 字段 —— 又一个「夹具与实现互相漏项」型假绿 |
| N-06 | **微信入站幂等失效**：`controller/wechat.go` 直接把 `msg.MsgID` 当 `EventID`，而官方事件推送（subscribe/扫码等）不带 `MsgId` → 空 EventID 被 `inbox_ingress.go:373` 兜底成随机 uuid，等于完全放弃幂等；微信 5 秒无回复会重推最多 3 次，每次重推多一条消息 + 多一次 AI 回复 | `wechat.go:121` `MsgId xml:"MsgId,omitempty"`、`controller/wechat.go:265` | 已修：`WechatIncomingMessage.DedupKey(accountID)` —— 有官方 MsgId 用它，否则退回本条报文字段哈希，并带账号前缀。`TestWechatDedupKey_OfficialMsgIDAndEventPushFallback`；M11 判定反向即红 |
| N-04 | **WhatsApp 乱序缓冲是死路**：`reorder_buffer.go:Offer` 在缓冲区只有 1 条时立即 flush 并删除会话，因此 `len(buf.messages)>=2` 分支、`window` 定时器与 `webhook.go:NewWebhookService` 里注册的 `FlushHandler`（含 `webhook_channel_whatsapp.go` 的入缓冲逻辑）**永不可达**；实测每条消息都 `delayed=false` 即刻放行、后到的早时间戳消息不会被重排、`FlushHandler` 收到 0 条 | 临时探针用例实跑输出：`单条到达: delayed=false ordered=1`／`s2 首条: delayed=false n=1；s2 次条: delayed=false n=1`／`FlushHandler 实际收到=0 条`（探针跑完即删，未入库） | **已处置（批C，§9）**。先证明"引用点只有两处、无其它调用方"：`globalReorderBuffer` 仅 `webhook.go` 的 FlushHandler 赋值与 `webhook_channel_whatsapp.go` 的一次 `Offer`；`Stats()` 全仓无调用方；`internal/**/*_test.go` 零引用。再用 `TestDispatchWhatsApp_OutOfOrderArrivalsAreNotHeld`（三条时间戳**倒序**到达）做删除前后差分：删除前 `--- PASS (8.03s)`（证明缓冲确实一条都不拦）、删除后同一组用例同名同结果 `--- PASS (5.20s)`（证明删除行为等价），并把该用例反向变异（在 `dispatchWhatsApp` 里插入 `return nil, nil` 模拟"被滞留"）→ `--- FAIL` 且三条都报「被滞留」+「实际 0 条」，确认它不是恒真守卫。官方从未要求重排（§6：WA 只说并发+重复），真正的防护是 wamid 幂等去重，故整块死机器（`reorder_buffer.go` 全文 + FlushHandler + 入缓冲预解析）删除，而不是补一个会拖慢每条消息的窗口 |

### 8.2 覆盖边界（不得写成"全渠道已收口"）

- `officialEventID` 只作用在 `WebhookService.Receive` 这一条漏斗上。走这条漏斗的渠道：`POST /api/webhook/:channel/:account_id` 与 `/api/webhook/:channel` —— telegram、qq、飞书、企微、whatsapp、douyin/tiktok、快手/小红书/闲鱼/custom。
- **不走 `Receive` 的入站通道**：钉钉（`POST /api/webhook/dingtalk/:account_id` → `DingTalkAppService.ReceiveMessage`）、微信公众号（`setupWechatWebhookRoutes` → 控制器自行验签后直接 `HandleIngressMessage`）。这两条的验签与幂等各自独立，本轮分别按 S-04（钉钉）与 N-06（微信）补修；`Verify` 里的 insecure 豁免对它们本就不生效。
- 加解密模式下的飞书/企微：官方键在密文里，`Receive` 层取不到，第二层由 dispatch 解出的官方 `message_id`/`MsgId` 落到 `message_hub.msg_id` 的唯一索引兜底。飞书那条已用 `TestFeishuDispatch_OfficialMessageIDDedupsAtHubLayer`（同一 `om_dup_1`、其余字节不同的两次投递 → 仍 1 行）实跑坐实；企微那条批B 记的是「仅代码核对、需要两套依赖所以留作待办」——**这个理由本身是错的**：`s.integration` 与 `s.wecomRepo` 都在 `NewWebhookService(db)` 里自动接线，用例并不贵。现已由 `TestN08_DispatchWeCom_XMLFullPathLandsInHub` 实跑补上（同一 `wecdn08_dup_1`、两次密文不同的投递 → 仍 1 行，且断言 `enc1 != enc2` 以防夹具假绿），见 §9.2。
- 钉钉 `createAt` / `sessionWebhookExpiredTime` 目前按**数字**解析。`open.dingtalk.com` 是 SPA、官方示例取不到可引用原文（§6 已记），无法断定官方形态；一旦官方改发字符串，N-05 同型的整包解析失败会在这里重演。真机回归时优先验证这一点。
- 桥接五渠道（浏览器抓取 + `POST /api/bridge/ingest`）不存在渠道侧验签/重投，§5 的 S 类条目对它们不适用；其出站语义问题在 D-02。

### 8.3 门禁

本轮没有单独的"批B 专属"门禁数：批B 收口后工作树继续被批C 改动，所以取一次覆盖两批的实跑。
2026-09-20 02:08 在隔离副本（§9 开头解释为什么要隔离副本）全量跑 `internal/service`：

```
ok  	hivemtk-user/internal/service	625.031s
```

`-count=1 -p 1`、`POSTGRES_TEST_PORT=8232`、`-timeout 1800s`，无 FAIL 行。两个必须写清的前提：
① 该副本里 `internal/service` 的**测试构建当时在工作树里编译不过**（并行会话的 `sop_approval_resume_test.go:240 itoa redeclared`），故副本内移开了那个文件 —— 这次实跑不覆盖它；
② 625.031s 是**同机有并行 agent 挤占**时的墙钟（`internal/service` 独跑的量级在 450~700s 之间摆动，撞默认 600s 超时会成"假红"），不要拿它当基线，见 §9.3。
批C 的最终门禁（含 N-04 删除）在 §9.3。

## 9. 批C 修复与验证记录（2026-09-20）

**验证环境**：本轮全部实跑在 `rsync` 出来的无 `.git` 副本 `/tmp/iso-hivemtk/hivemtk/user-server` 里做。原因不是省事：共享工作树这一时段被并行会话改动，`internal/service` 的**测试**构建在某个时刻编译不过（`itoa redeclared`），而变异电池要求"起点必须是已知干净的字节"。副本与真实树之间每轮都按 md5 逐一比对参与文件，两边一致才把结论搬回文档；副本内移开的并行会话文件（`sop_approval_resume_test.go`）不覆盖本轮任何结论。

**同步闸**：`rsync` 会撞上并行会话的"半成品编辑"——有一次同步过去的是 `card_stats_adapters.go` 用了 `timeutil.` 却没带 import 的那半个瞬间，影子树整包编译不过，五组电池不到 5 分钟就"跑完"并全部报 `invalid`（电池的编译闸把假证据挡住了，但那几分钟是白烧的）。此后编排脚本改成：**同步完先在影子树 `go vet ./internal/service/ ./internal/controller/`，vet 不干净就等 45s 重同步（最多 20 次），干净了才开电池**。

**顶点纪律**：电池开工前比 md5、每条变异后比 md5、全量还原后再比一次；任何一次不符就拒绝开工（`exit 4`）而不是继续跑。`webhook.go` 的顶点本轮前移两次：`f6d66d9a…`（D-04 落点）→ `4866c0ab…`（N-04 删除缓冲）→ `1e8a9a2a…`（N-08 的 `ParsePayload` 分支）。`webhook_channel_wecom.go` 前移三次：`44654e34…`（N-08 前）→ `529fe883…`（双形态兜底 + hub 幂等收敛）→ `6063bd38…`（XML 解析加深度上限、修掉嵌套子文本丢失）→ `63061007…`（嵌套子对象字段的两层查找，即 N-09③）。顶点每次前移后**五组电池全部在新顶点复跑**，不复跑就不能声称"D-01~D-04 的结论仍成立"；跑法是把五组电池串进一个 runner（8232 上的 `go test` 只能串行），runner 在每节前后各比一次全量顶点 md5（10 个文件，含 N-08 测试文件自身）。

### 9.1 修复落点 × 用例 × 变异反证

| # | 修复落点 | 钉住它的用例 | 变异反证 |
|---|---|---|---|
| D-01 | `webhook_channel_douyin.go`：平台短名与幂等键前缀按渠道派生（tiktok 不再借用 `douyin`/`dy`）；`lead_miner_adapters.go`：线索适配器按 `hub.Platform` 选；`douyin_lead_miner.go`：私信归因按行更新，不再插第二行；`webhook.go`：`dispatchToChannel` 的 douyin/tiktok 分派 | `webhook_batchc_tiktok_test.go` 7 条：`TestDispatchDouyin_TikTokMustNotBeLabelledDouyin`、`TestDispatchDouyinGeneric_TikTokKeyScopedByPlatform`、`TestLeadAdapterForTikTokIsNotDouyin`、`TestDouyinLeadAdapterForHub_SelectsByHubPlatform`、`TestDispatchDouyin_LeadRowKeepsPlatform`、`TestTriggerBridgeDMOutreach_EnqueuesUnderOwnChannel`、`TestDispatchToChannel_DouyinFamilyKeepsChannel` | 电池 `d01`：M12–M19 **caught=8 miss=0 invalid=0**（M12 把平台名整体退回 douyin、M13 只退回键前缀、M19 把归因改成再插一行） |
| D-02 | `webhook_outbound.go` 桥接分支的 `sent` 口径：`hubMsg == nil`、`persisted.ID == 0`（落库失败）、`persisted.Status == "failed"`（目标不可达）三种都**不算已发送**；不可达归类 `CategoryBadRequest` + `Retryable:false` | `webhook_batchc_bridge_sent_test.go` 4 条：`TestSendOutbound_Bridge_HubMsgNilIsNotSent`、`_UndeliverableIsNotSent`、`_PersistFailureIsNotSent`、`_QueuedIsSent` | 电池 `d02`：M20a–M20e **caught=5 miss=0**。其中 M20e 是"正常出库也不判成功"的反向守卫，专门防住把修复做成「一律 false」这种假修 |
| D-03 | `webhook.go`：`webhookInboundCapable` 能力表 + `webhookInboundRejectReason` + `Receive` 入口就地拒（回 400 而不是"收下再丢"）、`dispatchToChannel` 的 default 从静默丢弃改成显式 `ErrWebhookInboundNotWired`、`handleJob` 见该错误就地 `markProcessed`（不再落空内容 `unified_message`）、`insecureWebhookStartupError` + `runningUnderGoTest` 豁免 | `webhook_batchc_d03_route_contract_test.go` 5 条：`TestWebhookInbound_CapabilityMatchesDispatchSwitch`、`TestWebhookInbound_RejectHintsStayConsistentWithCapability`、`TestReceive_CapableChannelsPassTheGate`、`TestHandleJob_UnwiredChannelLeavesNoUnifiedMessage`、`TestHandleJob_DouyinNonMessageEventWritesNoUnifiedMessage`；HTTP 边 `TestWebhookRoute_NoAdapterChannelsGet400WithGuidance`（controller 包） | 电池 `d03-v2`：M21a…M28 **caught=9 miss=0 invalid=0**（含方向相反的两条：M25 能力表漏掉 douyin、M26 误纳 kuaishou） |
| D-04 | `webhook.go`：`parseTiktokSignature` / `verifyTiktokWebhook` 按官方 `TikTok-Signature: t=<ts>,s=<hex>`（常量 `tiktokSignatureHeader`，`webhook.go:759`）与被签串 `ts + "." + 原始 body`，`hex(HMAC-SHA256(secret, signed))`，比对用 `subtle.ConstantTimeCompare`；缺 `t=`/`s=` 任一段或 `t=` 不是数字时间戳都**显式报错**而不是静默判"签名不符"；头名经 `headerFold` 大小写不敏感取。`Verify` 里 tiktok 独立分支，不再与抖音共用 body-only HMAC；抖音的头列表去掉 `X-Lark-Signature` | `webhook_batchc_d04_tiktok_test.go` 7 条：`TestParseTiktokSignature_OfficialShapeAndStructuralErrors`、`TestVerifyTiktokWebhook_Contract`、`TestVerifyTiktokWebhook_HeaderNameIsCaseInsensitive`、`TestWebhookService_Verify_TiktokNotSharedWithDouyinBranch`、`TestWebhookService_Verify_DouyinIgnoresLarkHeader`、`TestWebhookService_Receive_Tiktok_LegacySignatureRejected`、`TestDispatchDouyin_SenderlessEventWritesNoHubRow`；HTTP 边 `TestWebhookRoute_TikTok_OfficialSignatureHeaderReachesService` | 电池 `d04`：M29–M35 **caught=7 miss=0 invalid=0**（M30 时间戳不再进被签串＝退回契约违背；M31 结构残缺静默当签名不符；M32 查头退回大小写敏感） |
| N-04 | 删除 `internal/service/reorder_buffer.go` 全文 + `webhook.go` 的 `FlushHandler` 注册块 + `webhook_channel_whatsapp.go` 的入缓冲预解析（三处引用点全仓核对、零调用方；细节与差分见 §8.1，不重复） | `webhook_batchc_n04_reorder_test.go`：`TestDispatchWhatsApp_OutOfOrderArrivalsAreNotHeld` | 删除前后同组用例差分（`--- PASS 8.03s` → `--- PASS 5.20s`）+ 反向变异（在 `dispatchWhatsApp` 插 `return nil, nil` 模拟"被滞留"）→ `--- FAIL` |

### 9.2 本轮新发现（读码/实跑查得，均已在批C 处置）

| # | 发现与证据 | 处置 |
|---|---|---|
| N-07 | 抖系**结构化**分支缺"取不到发送人即非消息事件"的守卫：URL 校验、审核/授权通知这类与会话无关的事件照样建出一行 `sender` 为空、`content` 兜底成 `"[tiktok message]"` 的 `message_hub` —— 客服收件箱多出一条永远无法回复的假会话，还会顺带驱动一次 AI 回复。`dispatchDouyinGeneric` 对同一条规则早已如此处理，结构化分支是漏的那半边。（代码内注释写作「D-04 连带发现」，文档编号 N-07） | 补 `if p.Sender == "" { return nil, nil, nil }`；用例 `TestDispatchDouyin_SenderlessEventWritesNoHubRow`；变异 M34 摘掉守卫即红 |
| N-08 | 企微入站**四处独立环节各自要求 JSON**：① `verifyWeCom` 用 `json.Unmarshal(body)` 取 `encrypt`，取不到就拿 `query["echostr"]` 当签名第四段 —— POST 消息回调没有 echostr，第四段是空串，sha1 恒不匹配 → 第一步 400；② `WebhookService.ParsePayload` 对 body `json.Unmarshal`，失败即 `Receive` 返回 `parse error`；③ `officialEventID` 的企微分支只在 JSON 文档上找 `MsgId`（明文 XML 回调退化成整包哈希兜底）；④ `decryptWeComPayload` 把解密出的明文再 `json.Unmarshal`。<br>**修复前实跑复现**（影子树、只放测试文件不放修复）：`XML 外壳验签报错：signature mismatch`、`invalid character '<' looking for beginning of value`×2、`明文 XML 回调的官方键…got ""` —— 4 红 1 绿（绿的正是"JSON 形态仍要能跑"那条反向守卫）。<br>连带查出第 5 个问题：hub 层按官方 `MsgId` 收敛重投时返回的是 `ErrMessageHubIdempotent` **错误**而不是已存在的那一行，`dispatchWeCom` 原样上抛会让 `handleJob` 继续往下走、用取不到正文的外壳落一条空内容 `unified_message` —— 正是 D-03 定性的"看得到、永远没人回复" | 修复口径**不声明哪一种是官方形态**（企微文档站是 SPA、原文未取到；只有同族的微信开放平台《消息加解密》给出 `<xml>…<Encrypt/>…</xml>` 外壳，见 §6，不足以宣称企微页原文）：envelope 与解密明文各自 JSON/XML 双解（`wecomEnvelopeMap`/`wecomFlatXMLMap`），企微在 `ParsePayload`/`officialEventID` 两处都走双解；幂等就地转成 `(nil, nil)`，与飞书/抖音/Telegram 的 dispatch 同一口径。用例 5 条（`webhook_batchc_n08_wecom_xml_test.go`），修复后同组全绿 `--- PASS`×5、`ok hivemtk-user/internal/service 8.140s`；变异反证见 §9.3 电池 `n08`；第 6 条用例补的是这个修复自身带进来的两个缺陷，见 N-09 |
| N-09 | **N-08 的第一版修复自带三个缺陷，是在写"第 6、7 条用例"时逐个查出来的**（不是读代码读出来的）：<br>① **预认证解析没有深度上限**。外壳解析必须发生在验签之前（不先取出 `Encrypt` 就算不出签名第四段），所以输入 100% 由请求方控制。`MaxWebhookBody` 只限总量 2 MiB，`encoding/xml` 自身不设深度限制 —— 2 MiB 全塞 `<a>` 就是几十万层递归，一个**未验签**的请求即可把 goroutine 栈打爆（预认证 DoS）。这条在 §5 的 S 类里没有对应条目，因为它是"修 A 问题时新引入的"，不在原清单上。<br>② **嵌套子元素的文本被丢掉**。旧实现的取值函数在"遇到子元素"和"取文本"之间是二选一：`<Image><MediaId>media_1</MediaId></Image>` 里 `MediaId` 的解法是"把子元素解成 map 再 `mapOrEmpty`"，而叶子节点解出的是**字符串**，`mapOrEmpty(非map)` 一律折成空 map → `MediaId` 变成 `{}`。<br>③ **解出来了也没人去取**（②改完才暴露的下一层）：`dispatchWeCom` 的取值全写在顶层 —— `getString(plain, "MediaId", "media_id")`、`getString(plain,"Title")+" "+getString(plain,"Url")`。嵌套形态的字段解析进了子 map，调用点却只看顶层，结果与②同样丢；且 link 消息的正文会算成**一个空格**（`"" + " " + ""`）落进 hub，绕过所有"正文非空"的判断。这条是 M-01（入站媒体被丢弃）在企微侧的实例。<br>**实跑取证**（①②跑在 `529fe883…` 上、③跑在 `6063bd38…` 上，都是"只把新用例放进旧字节"）：`--- FAIL: TestN08_XMLDepthIsBounded … 30 层嵌套的 XML 必须因超出深度上限而解析失败，got 1 个字段`；单独探针 `--- FAIL: TestN08Probe_NestedElementKeepsText … got map[string]interface {}{}`（探针只存在于影子树，取证即删）；`--- FAIL: TestN08_DispatchWeCom_NestedXMLFieldsReachHubContent … 嵌套在 <Link> 里的 Title/Url 必须被取到，got " "`；`--- FAIL: TestN08_DispatchWeCom_NestedMediaIDReachesHubRow … 必须被调用点取到并落进 hub.media_url，got ""`。<br>取证过程中还撞出一次**假红**：给媒体用例加的"别起外网协程"闸门（`svc.wecomRepo = nil`）同时掐断了 `parseWeComPlain` 取 AESKey 的路，失败原因是 `account_id, external_user_id are required` 而不是断言本身 —— 换发明文回调形态（该分支在碰 repo 之前就返回）后才拿到真红。 | ①②：递归改成显式带深度的下降 —— `wecomXMLMaxDepth = 16`，越界即置 `tooDeep` 结论位、`wecomFlatXMLMap` 一律返回 nil（调用方按"不是 XML"处理）；`element(depth)` 同时返回"子 map"和"叶子文本"。上限不贴着已知最浅形状取（外壳 1 层、已知明文形状 2 层），16 层是给更深业务结构留余量，防护强度只取决于"远小于 2 MiB 能塞下的层数"。<br>③：新增 `wecomSubString(plain, containers, keys…)` —— 先查顶层，再查已知子对象（`Image/Voice/Video/File/ShortVideo`、`Link`），两种形态都认（与 N-08 同一口径，不声明哪种是官方的）；`media_id` 与 link 正文改走它，link 两侧都空时正文回落到 `[link]` 而不是一个空格。<br>用例 3 条（深度 1 条 + 嵌套取值 2 条：一条走 hub 正文可观测、一条走 `hub.media_url`（企微收进来时 `MediaURL: req.MediaID`，长期 URL 才由异步转存回填）），另加一条纯 helper 用例钉住"摊平形态不能被两层查找改坏"与"未知子对象不得被当成媒体容器"。修复后同组全绿：`--- PASS`×9、`ok hivemtk-user/internal/service`（时间见 §9.3）；反证：M36i（上限抬到 4096）、M36j（越界不记录）、M36g（子元素取值 stub 成空串）、M36k（媒体取值调用点退回只查顶层）、M36l（链接正文取值退回只查顶层）、M36m（两层查找的子对象循环摘空），见 §9.3 |

### 9.3 门禁与电池复跑

**五组电池在新顶点的串行复跑**（日志 `/tmp/batchc-batteries-final3.log`，`batteries_rc=0`）。这一轮之所以要「复跑全部五组」而不是只跑新增那组：`webhook.go` 与 `webhook_channel_wecom.go` 的字节在本轮前移过，§9.1 里 D-01~D-04 的结论只在各自跑过的顶点上成立过，顶点一动就必须重证。

| 电池 | 变异 | 结果 | 该节结束时的顶点比对 |
|---|---|---|---|
| `n08` | M36a–M36m（13 条，覆盖 §9.2 N-09 的深度上限、双层取值、以及 N-08 的四处 JSON/XML 环节） | **caught=13 miss=0 invalid=0 skipped=0** | 顶点 10/10 一致（after-n08），当前 md5 `63061007…` |
| `d01` | M12–M19 | **caught=8**（按 `[OK ] M` 行计数） | 顶点 10/10 一致（after-d01） |
| `d02` | M20a–M20e | **caught=5** | 顶点 10/10 一致（after-d02） |
| `d03-v2` | M21a–M28 | **caught=9 miss=0 invalid=0 skipped=0** | 顶点 10/10 一致（after-d03-v2），当前 md5 `1e8a9a2a…` |
| `d04` | M29–M35 | **caught=7 miss=0 invalid=0 skipped=0** | 顶点 10/10 一致（after-d04） |

开工前的比对同样是 `顶点 10/10 一致（before）` ⇒ 每条电池的起点都是已知干净字节。两点必须写清、避免把这份记录读成"比实际更强"的门禁：

- 每条变异都带**预期失败特征串**（命中的用例名或预期的 `[SECURITY]` fatal 文案），`[OK ]` 只在"变红且红在该特征上"时才打印，编译错会判 `[INVALID]` 并停机 —— 本轮 `invalid=0`，13 条 n08 变异与 9+7 条 d03/d04 变异全部是**断言红**，不是构建红。
- `n08`/`d01`/`d02` 三组在还原后**复跑过基线并打印 green**；`d03-v2`/`d04` 只在开跑前做过双包基线预检（`基线=绿 (SVC)` / `基线=绿 (CTL)`），还原后没有再复跑测试。这两组的"还原后仍是绿的"依据是**逐文件 md5 与预检通过时的字节完全相同**（顶点比对），属于等价推理而非第二次实跑，如实标注。

**静态门（真实工作树）**：`go build ./...` → `build_rc=0`；`go vet ./internal/service/ ./internal/controller/` → `vet_rc=0`；`gofmt -l ./internal ./cmd` 无输出。这里记一个门禁脚本自身的洞：原命令写成 `gofmt -l ./internal ./cmd ./pkg`，本模块根本没有 `pkg` 目录，于是日志里混进一行 `lstat ./pkg: no such file or directory`（其余两个路径仍照常检查，"无文件列表"是真的干净，已单独用 `gofmt -l ./internal ./cmd` 复跑确认 rc=0 且无输出），路径列表已就地改正，避免下一轮把这行错误当成噪声读过去。

**全量测试（隔离副本 `POSTGRES_TEST_PORT=8232`、`-count=1 -p 1 -timeout 2400s`）**：两次跑的 `ok|FAIL` 行原文如下（第一次的门禁脚本把两个包的输出写到同一个 `/tmp/gate-batchc-internal.txt`，后跑的覆盖先跑的 ⇒ 第一次的失败正文丢失，脚本已改为按包名派生文件名）。

| 包 | 第一次（03:44–03:58） | 第二次（04:01–04:14，修掉互相覆盖后） |
|---|---|---|
| `internal/service` | `FAIL	hivemtk-user/internal/service	831.701s`<br>`--- FAIL: TestOrderDraftSweepWorker_EndToEndFlipsAndPurgesRealRows (0.33s)` | `ok  	hivemtk-user/internal/service	583.659s` |
| `internal/controller` | （正文被覆盖，只剩 `ok … 255.636s` 一行计数） | `ok  	hivemtk-user/internal/controller	144.040s` |

静态闸（真实工作树）：`gofmt -l ./internal ./cmd` 无输出、`go build ./...` `rc=0`、`go vet ./internal/service/ ./internal/controller/` `rc=0`。

**那条红的归因（不靠推断，三条独立证据）**：`TestOrderDraftSweepWorker_EndToEndFlipsAndPurgesRealRows` 出自 `internal/service/order_draft_sweep_test.go`（mtime 09-19 20:11），**不在批C 改动的 10 个文件里**；① 同参数第二次全量跑该包绿（`ok 583.659s`）；② 隔离复跑 `-count=5 -run 'TestOrderDraftSweepWorker_EndToEndFlipsAndPurgesRealRows'` 五次全过（`ok 1.725s`，单次 0.34s 与红那次的 0.33s 同量级 ⇒ 不是"根本没跑到"）；③ 红的市场窗口内 `ps` 观测到两个并行会话的测试二进制与本轮共用同一个 `POSTGRES_TEST_PORT=8232`（`/tmp/r26-hv/user-server` 的 `-timeout 1500s ./internal/service/ ./internal/controller/`，以及 `-run ShortLink|Xianyu|CardStats|Kuaishou|Douyin` 那组）。该用例断言"定时节拍把库里到期草稿翻成 expired / 删掉过保留期的终态行"，正是同库其它二进制并发读写同一张表就会打断的形态。⇒ 判为**并发共享测试库造成的偶发红，不是批C 引入的缺陷**；若后续再复现，按该用例自身的 durable 底座断言重新归因。墙钟也不可比：同一包三次分别 831s / 583s /（历史单跑）880s 量级，取决于同机并发数。


## 10. 各渠道出站限流契约（批F 取证，服务 N-11 / M-03）

取证分两档，**只有 A 档可直接作为改代码的依据**：A ＝ 本轮我用 `curl` 直连官方 URL 取到正文并逐字核对；B ＝ 官方站需 JS 渲染，由子代理经文本渲染代理读取，字段名与结论方向一致但**未逐字复核**，动手改之前必须自己再核一次或降级为"未验证 + 原因"。
> 本节以下所有 A 档引证指向的原文落在 `/tmp/chandocs/`（易失）⇒ 已整目录镜像到仓库外的 `.tmp_files/chandocs-2026-09-20/` 并附 `MANIFEST.md`（文件名 + 字节数 + md5），核验原文一律以镜像为准，来历见 §17.6。

| 渠道 | 限流信号在哪里 | 官方要求客户端做什么 | 本仓现状 | 档位 |
|---|---|---|---|---|
| Telegram | **in-band**：`result.parameters.retry_after`。原文（`core.telegram.org/bots/api`，`ResponseParameters` 表）："retry_after / Integer / Optional. In case of exceeding flood control, the number of seconds left to wait before the request can be repeated"。同页另有 "An Integer 'error_code' field is also returned, **but its contents are subject to change in the future**" ⇒ 码值不可当稳定判据 | FAQ（`bots/faq#my-bot-is-hitting-limits-how-do-i-avoid-this`）："In a single chat, avoid sending more than one message per second … eventually you'll begin receiving 429 errors."／"In a group, bots are not be able to send more than 20 messages per minute."／"For bulk notifications, bots are not able to broadcast more than about 30 messages per second"（原文语法如此）—— **只给速率，不给退避算法**，等待值只能来自 `retry_after` | 客户端内已读 `parameters.retry_after` 并据此设 `wait`（`telegram.go:169-176`），但跨层后这个值在归一层丢掉（N-11①④） | A |
| WhatsApp | **in-band**：`error.code` + `error.message`，形如 "`(#130429) Rate limit hit`"；码表 130429（吞吐到顶）/ 131056（同一收方过快）/ 131049（生态健康，"wait at least 24 hours before resending the template message"）。全页 **无 `Retry-After`**（`grep -c` 命中 0） | "if a send request fails, retry after **4^X seconds** (starting with X=0 and increasing X by 1 after each failure) until successful."；"Business phone numbers can send 1 message every 6 seconds to the same WhatsApp user (0.17 messages/second) … Exceeding this limit triggers error code 131056" | 走 `wa send status %d: %s`（含响应体）⇒ "rate limit" 文案能命中分类，但 131049 的 24 小时与 4^X 梯度都没实现；pacing 层自己的 `retry_after=1m0s` 反因格式解析不出 | A |
| 飞书 | **混合**：新接口 HTTP 429 **且响应头给等待秒数**（`x-ogw-ratelimit-limit` 窗口上限、`x-ogw-ratelimit-reset` 恢复周期，单位秒），体 `{"code":99991400,"msg":"request trigger frequency limit"}`；旧接口同 code 可能返回 **HTTP 400** | "失败的响应头中包含 `x-ogw-ratelimit-reset`，使用该响应头延迟请求是解除限频的最好方法：1. 等待…指定的秒数。2. 重试请求。" ⇒ **延迟值在 HTTP 头里**，纯文本分类器结构上看不见 | 出站只把 body 拼进错误串（`feishu.go:270`），头从未读；99991400 也不在触发词表 ⇒ 落 `CategoryUnknown` | B（`curl` 直连只得 7.5 KB JS 空壳） |
| 企业微信 | **in-band**：errcode 45009（接口调用超过限制）/ 45033（并发超过限制），无等待值字段 | "频率拦截时长一般与调用的限制时长相同…1分钟后自动解除"；并明确 "**接口实现时，仅系统失败需要重试。其余错误码，应该排查下调用失败原因**"；配额页另有一句"超过部分会被丢弃不下发" ⇒ 盲目重试不保证送达 | `wecom.go:526` 把 errcode 直接丢掉只留 errmsg（N-11③）⇒ 批F-1 已修：`wecomAPIError` 把 errcode 作为 `Code` 带出，45009/45033 进码表 | A（《全局错误码》<https://developer.work.weixin.qq.com/document/90000/90139/90313> 正文内联在 HTML 里，`curl` 直连 1.87 MB 取到：`45009 接口调用超过限制`、`45033 接口并发调用超过限制`、`40001 不合法的secret参数`、`40014 不合法的access_token`、`42001 access_token已过期 / access_token有时效性，需要重新获取一次`，另有「仅系统失败需要重试。其余错误码，应该排查下调用失败原因」与「1分钟后自动解除」两句原文） |
| 钉钉 | 自定义机器人："每个机器人每分钟最多发送20条消息到群里，如果超过20条，**会限流10分钟**"；回调侧 `sessionWebhook` / `sessionWebhookExpiredTime`（毫秒 epoch）在**回调体里** | 官方只给"限流 10 分钟"这一量级，且整合消息的建议 ⇒ 与 2/10/30s 梯度不在同一量级；`sessionWebhookExpiredTime` 过期后重投必然无意义 | 过期/缺失分支根本不构造错误（N-13） | B→降级（自定义机器人页 `curl` 直连只拿到 SPA 空壳，「20 条／限流 10 分钟」本轮**未取到原文**，不得作为改码依据） |
| QQ | in-band 错误码：40034100（主动消息超频控）vs 40034128 / 304103（**被动回复窗口/次数超限**）—— 语义不同档 | 被动窗口："群聊 5 分钟 5 次"；"msg_id 已过期，不能回复" ⇒ 后一类重试必然再失败，且重发必须换 `msg_seq`（"相同的 msg_id + msg_seq 重复发送会失败"） | `qq send status %d code=%d` 里 code 不在触发词表 ⇒ 两类都落 Unknown、同档重试 | A（40034100/40034128/304103 与「被动消息有效时间 5 分钟，每个消息最多回复 5 次」「相同的 msg_id + msg_seq 重复发送会失败」原文已 `curl` 直连取到） |
| 微信公众号 | in-band：`{"errcode":45009,...}`（与企微同号不同义）；日配额表（客服消息 50 万等）⇒ **配额型，退避无收益**；入站侧 "五秒内收不到响应…总共重试三次"，排重建议"有 msgid 的用 msgid，事件用 FromUserName + CreateTime" | 官方要求 5 秒内必须回 `success` 或空串 | `wechat send error: %d %s`（`wechat.go:302`）码进串了但不是 3 位状态码 ⇒ 不命中；入站排重已按 N-06 修 ⇒ 批F-1 已修：`wechatAPIError` 带出 `Code`，45009 判 `quota_exhausted`（不可重试）、45011 判限流顺延 1 分钟 | A（《公共错误码》<https://developers.weixin.qq.com/doc/oplatform/Return_codes/Return_code_descriptions_new.html>，`curl` 直连 200／156 965 B，正文内联：`45009 reach max api daily quota limit 接口调用超过限制`、`45011 api minute-quota reach limit, must slower, retry next minute API 调用太频繁，请稍候再试`、`-1 system error 系统繁忙，此时请开发者稍候再试`、`40001 invalid credential, access_token is invalid or not latest`、`40014 invalid access_token`、`41001 access_token missing`、`42001 access_token expired`；入站侧两句同页族亦为 A） |
| 抖音 | **in-band on HTTP 200**：错误表首列全是 200，28003070（超频控）、2190001（quota 已用完，"免费额度每日 8 点刷新"）、28029002（额度上限 5000） | 均为"次日/8 点刷新"或"降频联系平台"类 ⇒ 分钟级退避无效 | 抖系出站只写 `message_hub`（D-02 已收口），无 HTTP 客户端 ⇒ 本渠道这条不适用 | 半 A（错误码表 28003070/2190001/28029002 原文已 `curl` 直连取到；但出站无 HTTP 客户端，本档对本渠道无落点） |
| TikTok | 官方 rate-limit 页："a response will be returned with **HTTP status 429 and error code `rate_limit_exceeded`**"，一分钟滑动窗口；**未记载任何延迟头** | 入站侧官方要求幂等："TikTok retries the delivery of event notification for up to 72 hours using exponential backoff" | 入站已按 D-04/S-04 收口；出站不存在（D-02/D-03 定性） | B |
| 快手 / 小红书 / 闲鱼 | **未找到公开的官方 IM/私信服务端发送文档**（候选 URL 返回 404/空壳；检索命中全是融云/网易云信/腾讯云 IM 及第三方服务商博客） | 不适用 ⇒ 这三条渠道**不得配任何"官方限流契约"**，只能按自建保守值并在配置里标注"无官方依据" | 入站已被 D-03 的能力闸拒绝，出站只有 DB 交接 | —（缺口，见 §6） |

**由这张表直接决定的修复形状（批F-1 的验收口径）**：

1. 光修 `reRetryAfter` 的字符类**不够**：飞书把延迟放在 **HTTP 响应头**，企微/QQ/微信/钉钉放在 **in-band 业务码**，而这两类信息在现有客户端里要么从没被读、要么在拼错误串时被丢掉 ⇒ 必须由**各渠道客户端**构造带 `RetryAfter` 的结构化结果（`internal/service` 依赖 `internal/channelbot`，反向不成立，故错误类型要放在两边都能引的下层包），文本分类器只保留为兜底。
2. 出站时序要接上：`nextSendRetryAt` 现在无条件用 `sendRetryBackoffs = {60s,2m,4m}`（N-11④）。口径应为"渠道给了明确等待值就按它（并只顺延不提前），没给才用表"，并对**配额型/窗口过期型**（微信 45009、QQ 304103/40034128、抖音 2190001）显式判不可重试 —— 这类"重试必然再失败"的码混在可重试里，等于把队列灌满无效请求。
3. 判据要按码不按文案：WA 的"是否算限流"目前依赖响应体恰好含 `"Rate limit hit"` 这句话；官方码值是稳定字段（`error.code`），而 Telegram 官方自己声明 `error_code` 内容会变 ⇒ TG 用 `parameters.retry_after` 作判据、其余渠道用各自的 `error.code`/`errcode`/`code`，不新增"裸 3 位数字"这种宽松匹配（会把业务码误当 HTTP 码）。
4. 每条真实错误形态都要有用例（当前 `channel_error_test.go:24` 只覆盖了一条"恰好能匹配"的假想形态），且修复自身要过变异电池：把字符类改回不含 `=`、把结构化短路退回文本分类、把"只顺延不提前"改成取小 —— 三个方向各一条反证。

**批F-1 执行时对本表的两处偏离（逐条交代）**：

1. **飞书这一行的改动建立在 B 档证据上**，与本节开头"只有 A 档可驱动改码"的口径不符，故把安全性论证写清：`99991400` 进码表 + 读 `x-ogw-ratelimit-reset` 这两处改动的**后果集合不跨重试通道边界** —— 改前该串落 `CategoryUnknown + Retryable:true`（兜底档），改后落 `CategoryRateLimited + Retryable:true`，两条都在可重试侧，差别只有类别标签与"等多久"；读响应头是纯增量输入（头不存在时 `RetryAfter=0`，行为与改前一致）。也就是说：即使 `x-ogw-ratelimit-reset` 这个头名被记错，最坏结果是"退避值取不到、回到固定梯度"，不会出现"该 fail-fast 的被重试"或反之。**若后续核到该头名与官方口径不符，只需删掉这一处读头，码表与其余改动不受影响。**
2. **表里未列、由本轮新用例坐实的两处**（不是推测，是跑出来的红）：
   - 企微/公众号的**凭证类码**（企微 40001/40014/42001、公众号 40001/40014/41001/42001）在改前一律落 `unknown + Retryable:true` ⇒ 一个失效凭证会被重试通道反复喂同一条 token。首轮红测原文：`webhook_batchf1_n11_client_chain_test.go:250: got unknown/true，want auth/false（raw=wecom send errcode=40001 errmsg=invalid credential）`。现已进码表判 `auth`，并在文案兜底档补了 errmsg 缺码时的四种原文写法（`invalid credential` / `invalid access_token` / `access_token expired` / `access_token missing`，全部为上面两个 A 档页的原文列）。
   - **Telegram 的 Markdown 兜底分支**（`telegram.go` 去掉 `parse_mode` 重发的那一条）此前用裸 `fmt.Errorf` 记错误，撞上 429 时状态码与 `retry_after` 双双丢失。频控按账号计、连着两条请求本就容易撞，不是理论分支。
   
   另记一处**判据顺序修正**（由新用例反推出来的口径）：`retry_after>0` 原来与 429 同档并列在 `switch` 第一条，于是"HTTP 403 且体内带 retry_after"这种自相矛盾的响应会被等待值带进重试通道；现把它单列一档排在 auth 之后 —— 凭证类一律 fail-fast。等待值来自构造点字段（而非文案）时也参与判定，否则飞书这种"延迟值在响应头里"的渠道永远判不出限流。

## 11. 批F-1 修复与验证记录（2026-09-20，服务 N-11）

### 11.1 落点 × 反证用例

§10 那四条验收口径逐条对上代码与用例（用例全部在 `internal/service/webhook_batchf1_n11_backoff_test.go` 与 `…_client_chain_test.go`，前者判归一层与出站时序，后者从 `httptest` 起真实客户端走完整条链）：

| 口径 | 落点 | 反证用例 |
|---|---|---|
| ① 退避秒数取不到 | `reRetryAfter` 字符类补 `=`、token 允许单位后缀（`parseRetryAfterToken` 纯数字按秒、其余走 `time.ParseDuration`） | `TestN11_TGRetryAfterEqualsFormKeepsDelay`、`TestN11_DurationFormKeepsFullDelay`、`TestN11_RetryAfterProseAloneIsRateLimited` |
| ② 状态码取不到 | `reHTTPStatus` 认 `status ` / `status=` / `status:` 三种写法；`ChannelError.StatusCode` 由构造点直接带出 | `TestN11_HTTPStatusSpellingVariants`、`TestN11_ClientChain_Telegram429` / `_Telegram400NotRetryable` |
| ③ 渠道码被丢弃 | 新增 `core.APIError`（事实位）＋ 7 处构造点（TG 主分支与 Markdown 兜底分支、QQ、WA、企微、公众号、飞书、WA pacing）带出 `Channel/StatusCode/Code/RetryAfter/Raw`；`channelCodeRules` 按渠道分表；`AsChannelError` 先取结构化事实、文案只兜底 | `TestN11_ClientAPIErrorSurvivesWrapping`、`TestN11_ServiceErrorHelpers_WeComAndWeChat`、`TestN11_ServiceErrorHelper_Feishu`、`TestN11_ClientChain_QQCodeDecidesCategory`、`TestN11_ClientChain_WhatsAppErrorCodeDecidesCategory`、`TestN11_SameCodeDifferentChannel` |
| ④ 拿到了也没人用 | `nextSendRetryAt` 读 `ce.RetryAfter`，口径"只顺延不提前"；配额型/窗口过期型判不可重试 | `TestN11_EnqueueHonoursChannelDelay`、`TestN11_EnqueueHonoursClientStructuredDelay`、`TestN11_ChannelDelayNeverShortensTableBackoff`、`TestN11_WeChatDailyQuotaIsNotRetryable`、`TestN11_QQPassiveWindowExpiredIsNotRetryable`、`TestN11_QuotaCodeDoesNotEnterRetryLane` |
| 延迟只在响应头（飞书） | 读 `x-ogw-ratelimit-reset`；`RetryAfter>0` 且类别仍 unknown 时判限流 | `TestN11_HeaderOnlyDelayCountsAsRateLimit`、`TestN11_AuthStatusBeatsStrayRetryAfter` |

### 11.2 用例先跑红的生产缺陷（不是测试写错）

除 §10 偏离第 2 条列的两处外，本批还有一处是**用例夹具自身差点造假绿**：`TestN11_TGRetryAfterEqualsFormKeepsDelay` 初版照抄 Telegram 全串（含响应体回显），改前也绿；拆成「只有 `=` 形态」与「TG 全串」两条后才露出差异 —— 记录的正是 N-11① 那行"靠回显侥幸取到"的结论。

### 11.3 变异电池（`/tmp/f1-mut/battery.py`，隔离副本 `/tmp/rf1-hivemtk`）

20 条变异，每条=一处真实回退（把该修复方向改回原状或改成相邻的错误实现），驱动方式：改源码 → 跑 F-1 用例集 → 统计红的用例 → 逐文件 md5 与基线比对还原。控制组与收尾复跑都必须是 0 红。

首轮结果：`CONTROL caught=0 broken=False`；S1–S20 全部 `restored=ok`；`total=20 missed=['S18']`；`FINAL(变异已全部还原) caught=0`（`ok hivemtk-user/internal/service 34.878s`）。

| 变异 | 回退方向 | caught | 主要红测 |
|---|---|---|---|
| S1 | `retry_after` 字符类退回不含 `=` | 3 | DurationFormKeepsFullDelay / RetryAfterProseAloneIsRateLimited / TGRetryAfterEqualsFormKeepsDelay |
| S2 | token 退回不含单位后缀 | 1 | DurationFormKeepsFullDelay |
| S3 | 状态码字符类退回只认空格 | 1 | HTTPStatusSpellingVariants |
| S4 | 结构化短路整体退回纯文本分类 | 8 | ClientAPIErrorSurvivesWrapping / HeaderOnlyDelay… / ServiceErrorHelper_Feishu / WeComAndWeChat / SendOutbound_Bridge_UndeliverableIsNotSent |
| S5 | 分类函数里的业务码短路退回文案 | 5 | QQPassiveWindowExpired / QuotaCodeDoesNotEnterRetryLane / SameCodeDifferentChannel / WeChat 两条 |
| S6 | 渠道识别短路（同号不同义的前提） | 7 | FeishuBusinessCode / QQ… / WeChat… / UnknownChannelCodeKeepsTextFallback |
| S7 | 「只有 retry_after」的限流档退回不可达 | 2 | DurationForm… / RetryAfterProseAlone… |
| S8 | 构造点带来的等待值不再参与判定 | 1 | HeaderOnlyDelayCountsAsRateLimit |
| S9 | 凭证类原文兜底档退回单一写法 | 1 | AuthProseWithoutCodeStillFailsFast |
| S10 | 企微凭证码进表被抹掉 | 1 | ServiceErrorHelpers_WeComAndWeChat |
| S11 | 「只顺延不提前」改成取小 | 5 | Enqueue 三条 / ChannelDelayNeverShortens / SendOutbound_Wechat |
| S12/S13 | 企微 / 公众号构造点退回丢弃 errcode | 5 / 4 | ServiceErrorHelpers_WeComAndWeChat |
| S14/S15 | 飞书不读响应头 / 退回裸 error | 1 / 1 | ServiceErrorHelper_Feishu |
| S16 | QQ 退回只带 HTTP 状态码 | 1 | ClientChain_QQCodeDecidesCategory |
| S17 | WA 丢结构化 `Code`（Raw 仍回显 body） | 1 | ClientChain_WhatsAppErrorCodeDecidesCategory |
| **S18** | **WA 错误串不再回显响应体** | **0（首轮漏网）** | — |
| S19/S20 | TG 主分支 / Markdown 兜底分支退回裸 `fmt.Errorf` | 3 / 1 | ClientChain_Telegram 三条 |

**S18 这条 missed 是真缺口，不是等价变异**（首轮我把它标成"待查"，判据如下）：`waAPIError` 只在 `error.code` 存在时才填 `Code`；Graph 也存在只带 `message` 的失败体（`{"error":{"message":"(#130429) Rate limit hit","type":"OAuthException"}}`），此时唯一判据就是 Raw 里的响应体回显。把 body 摘掉后这条串只剩 `wa send status 400` ⇒ `bad_request`（不可重试，消息直接丢），限流被误判成客户端错误。⇒ 新增 `TestN11_ClientChain_WhatsAppProseOnlyErrorKeepsBodyEcho`（夹具先断言 `api.Code == ""`，防止"其实带码"的假夹具），并把 S17 的期望从 `equivalent` 改成 `catch`（它红在**事实字段**的断言上，不是文案兜底）。

复跑（`ONLY=S17,S18`，同一副本、同一用例集）：`CONTROL caught=0 broken=False` → `MUT S17 caught=1 restored=ok` → `MUT S18 caught=1 restored=ok :: TestN11_ClientChain_WhatsAppProseOnlyErrorKeepsBodyEcho` → `total=2 missed=[]` → `FINAL caught=0`，`ok hivemtk-user/internal/service 26.463s`。⇒ 电池口径现为 **20/20 expect=catch、20 条全部咬住、逐条 md5 还原一致**。

### 11.4 门禁

真实工作树：`go build ./...` rc=0；`go vet ./internal/service/ ./internal/channelbot/whatsapp/` rc=0（批F-2 改动后复跑）；`go test -count=1 -p 1 -run 'TestN11_' ./internal/service/` → `ok hivemtk-user/internal/service 11.069s`；`go test -count=1 -p 1 ./internal/channelbot/...` → `core 0.471s / qq 0.536s / telegram 3.722s / whatsapp 0.478s` 四行 `ok`。


## 12. 批F-2 修复与验证记录（2026-09-20，服务 N-10 / N-10b / N-14）

### 12.1 落点

| 环节 | 改动 | 反证用例 |
| --- | --- | --- |
| 媒体引用索引（N-10） | `whatsapp.WAMessageRef` 新增 `MsgID`（= wamid = hub 行 `msg_id`），`MediaRefs()` 逐条填，新增 `MediaByMsgID()` 建 wamid→引用映射；删除只回第一条媒体的 `MediaRef()`（全仓唯一调用点即被修的 dispatch，`git log --all -S'.MediaRef()'` 只剩当初的文件搬迁提交 `a200aa65`） | `TestN10_WhatsAppInboundMediaBackfillsEveryHubRow`（一条推送四行：图片 + 文本 + 文档 + 畸形图片） |
| 逐条转存 + 回填键（N-10） | 转存任务从"循环外只起一次"改为**在消息循环内、跟着本行落库动作**各起一次，`EnrichHubMediaURLByMsgID` 的键改成本条消息的 wamid（原来传 `media_id` ⇒ 主路径与兜底两条都 0 行） | 同上（两行各自断言 `media_url == /files/whatsapp/<自己的 media_id>`） |
| 媒体原件留痕（N-10b，新坐实） | `Ingress()` 里把 `media_id` / `mime_type` / document 的 `filename` 写进事件 Extra ⇒ 随 `message_hub.Extra` 落库。此前 media_id 在整条链路上**只存在于内存**，转存失败即无痕可查（它只有 7 天有效期） | 同一用例逐行断言 `Extra.media_id` / `Extra.mime_type` / `Extra.filename` |
| 内容窗口去重守卫（N-14，批F-2 期间新发现） | `interceptInbound` 的 `duplicate(channel+sender+content)` 分支加 `chanMsgID == ""` 守卫；`channelMsgIDOf(event)` 上移为函数内单一变量（回环判定与本守卫共用同一口径） | 正向 `TestN14_RepeatedContentWithDistinctWAMIDsIsNotDropped` + 反向 `TestN14_IDLessEventStillDedupsByContent` |
| 钉钉入站补齐稳定 ID（N-14 第二处现场） | `dingtalk_app.go` 的入站事件 Extra 补 `"channel_msg_id": msg.MsgID`（官方 msgId），否则钉钉永远落在"无 ID ⇒ 走内容窗口"这一侧 | `TestN14_DingTalkRepeatedTextIsNotDropped` |

三处 N-14 现场都是**写媒体用例时跑出来的**，不是先想到再补的：`webhook_batchf2_n10_media_test.go:153: load hub wamid-f2-4: record not found` + 服务端日志 `dup=true event_id=wamid-f2-4 reason="duplicate(channel+sender+content) within window"` —— 畸形图片行（`type:"image"` 但没带 `image` 对象）的占位正文与前一条图片相同，于是第二条被判重复、整行不入库。文本形态（五分钟内连发两条"好的"）与钉钉形态（`event_id=dt-1-m-n14-2`）随后各补一条用例独立复现。

### 12.2 用例侧的两处自证

- **同步点在写库之前**：转存协程是「下载 → 转存 → 回填」三步，替身 `waMediaStoreFn` 返回的那一刻还没写库。首版直读把竞态误读成缺陷（文档行报 `media_url` 为空），改为 `f2WaitHubMediaURL` 轮询（10s 上限、50ms 步进）并在注释里写明为什么不能直读。
- **多余任务也要断言**：只等到"前两条媒体到齐"就收工会漏掉"给纯文本也起了一次转存"这一类反向缺陷 ⇒ 补 `settle:` 排空段 + `len(seen) != 2` 断言 + 畸形图片行（`wamid-f2-4`）不得有 `media_url`/`Extra.media_id`。
- **去重用例一律用进程内 `MemoryCache`**：测试库与 Redis（8232 / 全局 cache）同时被多个会话读写，去重键是跨用例可见的全局状态；用 nonce 只是碰巧不撞，撞了就是查不出来的假红/假绿。反向用例（无 ID 仍要拦）因此不再需要"Redis 不可用就 skip"的探针。

### 12.3 变异电池（12 条，脚本 `/tmp/f2-mut/battery.py`）

副本树 `/tmp/rf1-hivemtk/hivemtk/user-server`（rsync 后 `go vet` 干净才开跑），用例集 `TestN10_|TestN14_|TestDispatchWhatsApp|TestWaMessageContent|TestDingTalkInbound|TestInboxIngress_|TestChannelMsgIDOf`。控制组 `caught=0 broken=False`。

| 变异 | 抹掉的点 | 红数 | 首个红用例 |
| --- | --- | --- | --- |
| T1 | 回填键 wamid → media_id（原始 N-10 形态） | 1 | TestN10_…EveryHubRow |
| T2 | wamid→媒体映射只留首条 | 1 | 同上 |
| T3 | `MediaRefs` 不带 wamid（映射建不起来） | 1 | 同上 |
| T4 | Ingress 不写 `Extra.media_id`（N-10b） | 1 | 同上 |
| T5 | Ingress 不写 `Extra.mime_type` | 1 | 同上 |
| **T6** | Ingress 不写 document 的 `Extra.filename` | **0（首轮漏网）** | — |
| T7 | 转存任务的 `mediaID != ""` 守卫消失 | 1 | 同上（`settle` + `len(seen)!=2` 咬住） |
| T8 | 内容窗口去重回到对所有事件生效（N-14） | 3 | N14 两条 + N10 |
| T9 | 守卫条件写反 | 4 | N14 三条（含反向用例）+ N10 |
| T10 | 钉钉事件不带官方 msgId | 1 | TestN14_DingTalkRepeatedTextIsNotDropped |
| T11 | `channelMsgIDOf` 恒返回空串 | 5 | 另含 TestInboxIngress_SelfEcho_ExactPlatformMsgID_Blocked、TestChannelMsgIDOf_FiltersPlaceholder |
| T12 | 不再过滤 `wa-out-`/`tg-out-` 占位 ID | 1 | TestChannelMsgIDOf_FiltersPlaceholder |

**T6 这条 missed 是真缺口**：filename 只在"转存调用参数"上被断言（`f2StoreCall.filename`，取自 dispatch 侧的 `mediaFilename`），而 `message_hub.Extra.filename`（Ingress 侧）无人断言——两处是两条独立取值路径，改一条不影响另一条。⇒ 在逐行断言里补 `Extra.filename`（文档行必须为 `报价单.pdf`、图片行必须无此键），复跑 `ONLY=T6`：`CONTROL caught=0` → `MUT T6 caught=1 restored=ok :: TestN10_WhatsAppInboundMediaBackfillsEveryHubRow` → `total=1 missed=[]` → `FINAL caught=0`，`ok hivemtk-user/internal/service 30.495s`。

电池口径现为 **12/12 expect=catch、12 条全部咬住、逐条 md5 还原一致**（首轮汇总 `missed=['T6']`，补断言后 `missed=[]`）。

### 12.4 门禁（真实工作树）

`go build ./...` rc=0；`gofmt -l internal/ cmd/` 无输出；`go vet ./internal/service/ ./internal/channelbot/...` rc=0；
`go test -count=1 -p 1 -timeout 560s -run 'TestN10_|TestN14_|TestDispatchWhatsApp|TestWaMessageContent|TestDingTalk|TestInboxIngress_|TestChannelMsgIDOf' ./internal/service/` → `ok hivemtk-user/internal/service 28.216s`；
`go test -count=1 -p 1 ./internal/channelbot/...` → `core 0.547s / qq 0.488s / telegram 3.687s / whatsapp 0.488s` 四行 `ok`。

## 13. 批F-3 修复与验证记录（2026-09-20，服务 M-02）

### 13.1 落点

| 文件 | 改动 | 反证用例 |
| --- | --- | --- |
| `internal/service/webhook_channel_whatsapp.go` | 循环外新增 `var contents []string`，循环内无条件 `contents = append(contents, content)`（原来只在 `if firstHub == nil` 分支里给 `p.Content` 赋值 ⇒ 只有首条进 AI 输入）；循环后 `if len(contents) > 0 { p.Content = strings.Join(contents, "\n") }` | `TestM02_WhatsAppMultiMessageBatchFeedsEveryMessageToAI`（改前红：`payload.Content = "多少钱-m02-…"，want "…\n有现货吗-m02-…\n能开发票吗-m02-…"`） |
| 同上 | `firstHub`/`p.Sender`/`p.ChatID` 的赋值位置不变（返回的 hub 仍是首条，媒体回填与幂等键都按各行自己的 wamid 走，见 §12） | `TestM02_SingleMessagePayloadUnchanged` |
| `internal/service/webhook_channel_whatsapp_test.go` | 既有 `TestDispatchWhatsApp_MultiMessageBatch` 的第 43 行断言原本写的是 `p.Content != "第一条"` —— **它在把缺陷当规格钉住**；改为合成口径 `"第一条\n第二条\n[图片]"` | 同上 |
| `internal/service/webhook_batchf3_m02_multimessage_test.go` | 新增两条：三条正文（各带 nonce 作用域，账号 90114）逐条落库 + `p.Content` 全量合成；单条推送正文不加分隔符、不掺空串 | — |

口径选择（为什么不"每条各触发一次 AI"）：一次推送内的多条消息在 Meta 侧是同一批乱序补齐的连续输入，逐条触发会让客户收到 N 条互相抢答的回复，且 `triggerSalesEngine` 的去重键是 job EventID（整批一个），逐条触发的预算与幂等都对不上。与中台批量入口（`PublishCustomerMessage` 一次投一份合成文本）保持同一口径：**N 条合成一份输入、一次回复**。

### 13.2 变异电池（5 条，脚本 `/tmp/f3-mut/battery.py`）

RUN_FILTER=`TestM02_|TestDispatchWhatsApp|TestN10_|TestWaMessageContent`；控制组 `caught=0 broken=False`；逐条 `restored=ok`（md5 与 BASE 一致）。

| 变异 | 抹掉的点 | 红数 | 首个红用例 |
| --- | --- | --- | --- |
| U1 | 累加挪回只在首条分支里（M-02 原状） | 2 | TestM02_…FeedsEveryMessageToAI、TestDispatchWhatsApp_MultiMessageBatch |
| U2 | 三条正文无分隔拼接（"多少钱有现货吗"糊成一句，问题边界丢失） | 2 | 同上 |
| U3 | AI 输入只取最后一条（首条问题反而没人答） | 2 | 同上 |
| U4 | 守卫写成 `len(contents) > 1` ⇒ 单条推送的 `payload.Content` 变空 | 1 | TestM02_SingleMessagePayloadUnchanged |
| U5 | 整块合成删除（等价于改前形态） | 3 | 另含 TestM02_SingleMessagePayloadUnchanged |

**U3 首轮是 BROKEN 而非 caught**：变异体把 `strings.Join` 整个换掉后 `"strings"` 成了 unused import，`go test` 只给编译失败，拿到的不是行为信号。修法是让变异体保留一处无害引用（`strings.Repeat("", 0)`）再跑 ⇒ `ONLY=U3` → `caught=2`。⇒ 电池脚本的 `tally` 里 `broken` 只能当"取证失败"，不能当"变异被覆盖"（同 §12.3 的 T6 是两类假绿：一个是断言漏项、一个是变异不自洽）。

汇总：`total=5 missed=[] unexpected_red_on_equivalent=[]`，`FINAL caught=0 broken=False` → `ok hivemtk-user/internal/service 14.007s`。

### 13.3 对批F-2 电池的回归复核（M-02 改动过同一函数）

M-02 与 N-10/N-10b 改在同一个 `dispatchWhatsApp` 里，因此把 12 条的 F-2 电池在**含 M-02 修复的树**上整体重跑一遍（`/tmp/f2-mut/f2_rerun.log`）：BASE 四个文件 md5 与当前树一致，`CONTROL caught=0`，**T1–T12 全部 `caught≥1 restored=ok`**，`missed=[]`，`FINAL caught=0 broken=False` → `ok hivemtk-user/internal/service 25.008s`。

### 13.4 门禁（真实工作树）

`go test -count=1 -p 1 -timeout 500s -run 'TestE2E_WebhookService_DispatchWhatsApp|TestE2E_WebhookService_ShouldTriggerAI_FourChannels|TestDispatchWhatsApp|TestM02_|TestN10_|TestN14_|TestWaMessageContent|TestE2E_WhatsApp|TestE2E_HandleJob_FourChannels_AIDisabled' -v ./internal/service/`
→ 20 条 `--- PASS`（含改前只断言"返回首条 hub"的两处 `dispatchWhatsApp` 调用点：`TestE2E_WebhookService_DispatchWhatsApp`、`TestDispatchWhatsApp_IngressDoesNotTriggerAI`），`ok hivemtk-user/internal/service 12.778s`。

## 14. 批F-4b 修复与验证记录（2026-09-20，服务 M-01 的 TG 半场；连带 N-15 / N-16）

M-01 在 TG 侧最严重：§3.1 那行「✘ 入站只取 `text‖caption`，媒体结构未声明」的完整成因是**三层同时缺**——解析层 `TGMessage` 没有媒体字段、协议层没有 `getFile` 调用能力（全仓零命中）、落库层 `MsgType` 写死 `text`。三层逐一补齐，缺任一层都表现为"客户发的图片在系统里不存在"。

### 14.1 官方取证（A 档）

`https://core.telegram.org/bots/api` 落盘 `/tmp/chandocs/tg_api.html`，**860 075 字节**（curl 直取整个页面即得正文，非 SPA 壳）。四条有裁决力的原文，逐字抄录：

1. getFile —— 「Use this method to get basic information about a file and prepare it for downloading. **For the moment, bots can download files of up to 20MB in size.** On success, a File object is returned. The file can then be downloaded via the link `https://api.telegram.org/file/bot<token>/<file_path>`, where `<file_path>` is taken from the response. **It is guaranteed that the link will be valid for at least 1 hour.** When the link expires, a new one can be requested by calling `getFile`.」
   ⇒ 三条直接决定实现的结论：① 下载前缀是 `/file/bot<token>/`，**与接口调用的 `/bot<token>/<method>` 不是同一路径段**（写成后者＝每次下载 404）；② 20 MB 是官方给的数，不是我们挑的数（`MaxDownloadFileBytes = 20 << 20`，`telegram.go:654`）；③ 链接只保 1 小时 ⇒ **入站当场换链接并转存进长期存储是唯一不留债的做法**，只存 `file_id` 等于把原件的有效期写成一小时。
2. File 对象 —— 「`file_path` String **Optional**. File path. Use `https://api.telegram.org/file/bot<token>/<file_path>` to get the file.」
   ⇒ `file_path` 可能缺席（本地 Bot API Server 场景官方明说会直接给绝对路径、免去下载）。缺失时**必须报错**：拼出 `…/file/bot<token>/` 是个不存在的接口，线上表现为"每次下载都 404"，而错误串里看不出是路径缺失。
3. Message 字段互斥回写 —— 「animation … **For backward compatibility, when this field is set, the document field will also be set.**」「live_photo … when this field is set, the photo field will also be set.」
   ⇒ 判序必须 animation 先于 document、live_photo 先于 photo，否则 GIF 存成压缩包（T2）、实况照片丢掉动态那一半（T3）。这条顺序在代码里靠 `mediaKind` 的 switch 次序承载（`telegram.go:878-911`），注释里点名是官方口径而非本地偏好。
4. photo —— 「photo **Array of PhotoSize** Optional. Message is a photo, **available sizes of the photo**」，且 `PhotoSize.file_size` 是 Optional、`width`/`height` 必填。
   ⇒ 数组是**同一张图的多个尺寸**，不是多张图：取首档＝把 10 字节缩略图当原件（T4）；"取最大档"因此主口径用 `file_size`、缺失时退回像素面积（`largestPhotoSize`，`telegram.go:915`）。

`caption` 的适用范围官方句（「animation, audio, document, paid media, photo, video or voice」）也在夹具里逐字核过 ⇒ 媒体那条的正文本来就在 `caption`，归一时必须带出（T6 抓丢弃 caption）。

### 14.2 落点 × 用例

| 落点 | 修的是什么 | 用例（`internal/service/webhook_batchf4_m01_telegram_test.go` + `internal/channelbot/telegram/telegram_media_test.go`） |
|---|---|---|
| `TGMessage` 补 photo/animation/audio/document/live_photo/voice/video/video_note/sticker/story 容器 + `TGMediaRef{FileID,FileSize,FileName,Width,Height}` | 解析层根本没有承载位（M-01 根因①） | `TestM01_TelegramMediaKindTable`（判序表逐条）、`TestM01_TelegramGIFNotMistakenForDocument`、`TestM01_TelegramVoiceIsAudioKind` |
| `mediaKind`/`Inbound()`：类型 + 占位符 + 待转存引用；`document` 带官方 `file_name` 进占位符 | 落库层写死 `text`（M-01 根因③） | `TestM01_TelegramInboundCaptionAndPlaceholder`、`TestM01_TelegramDocumentKeepsFileName`、`TestM01_TelegramEditedMediaKeepsType` |
| `Client.GetFile`：`ok:false`／非 200／非 JSON／`file_path` 缺失四类一律报错 | 协议层无下载能力（M-01 根因②） | `TestM01_TelegramGetFileHitsBotMethodPath`、`TestM01_TelegramGetFileErrors`（4 子例）、`TestM01_TelegramGetFileRejectsEmptyFileID` |
| `Client.DownloadFile`：`/file/bot<token>/<file_path>` + `io.LimitReader(max+1)` 判超限 + `checkTGFilePath` 只收相对路径 | 官方前缀与 20 MB；半截文件不得当原件 | `TestM01_TelegramDownloadFileUsesFilePathPrefix`、`TestM01_TelegramDownloadTruncationDetected`、`TestM01_TelegramDownloadPathGuard`、`TestM01_TelegramDownloadNon200` |
| `telegram_media.go`：`FetchTelegramMedia` 双预检（`getFile` 报明的 `file_size` 与事件自带的 `file_size`）→ `persistTelegramMediaAsync` 起下载腿 → `backfillTGMedia` 回填 `media_url`/`Extra.media_urls`/`Extra.media_file_ids` | 转存→回填整条腿，且**不整份进内存再丢** | `TestM01_TelegramImageIsStoredAndBackfilled`、`TestM01_TelegramFetchRejectsOversizedDeclaredSize`、`TestM01_TelegramOversizedMediaSkippedBeforeDownload`、`TestM01_TelegramRealGetFileThenDownload`、`TestM01_TelegramRealTruncationRejected`、`TestM01_TelegramRealDownloadNon200NotStored` |
| 失败口径：下载失败保留占位符行、缺 token 直接跳过转存 | "转存失败"不能变成"这条消息不存在"，也不能变成一条坏媒体 | `TestM01_TelegramDownloadFailureKeepsPlaceholderRow`、`TestM01_TelegramMissingTokenSkipsPersist` |

三类"取不到长期 URL"的兜底口径与 WhatsApp 侧（N-10）一致：**行一定在、类型一定对、`media_url` 一定可判空**，宁可工作台显示 `[图片]` 而不显示破图。

### 14.3 连带发现的两处（都已在本次修掉）

- **N-15｜bot token 进日志**：官方把长期凭证放在 URL 路径里（`/bot<token>/<method>`），而标准库 `*url.Error` 会把**完整 URL** 写进 `Error()` ⇒ 一次网络抖动就把 token 连同 `Post …` 一起落进日志与观测链路，出站错误串还会进 `ChannelError.Raw` 一路带到工单文案。修法：`Client.scrubToken`（`telegram.go:76`）+ `redactedError`（保留 `Unwrap` 错误链，否则上游 `errors.Is/As` 的限流分类会因脱敏失效），收口在本包**仅有的两个** HTTP 出口 `DoJSON`（覆写内嵌，覆盖 SendMessage/setWebhook/getFile）与 `DownloadFile`。早退条件（`err==nil || token=="" || !Contains`）刻意保留：不含 token 的错误原样返回，不让上游判据无谓变深一层壳。用例 `TestM01_TelegramTokenNeverInError`、`TestM01_TelegramScrubKeepsUntokenedErrorIntact`。
- **N-16｜入站幂等键不带账号 ⇒ 第二个 bot 的客户消息整条蒸发**：官方 `update_id` 与 `message_id` 都是**单个 bot 自己**的计数，而 `message_hub` 唯一键是 `(platform, msg_id, conversation_id)` 不含账号。两个 bot 各自数到同一个号、又落在同一个会话（同一用户在两个 bot 的私聊里 `chat.id` 就是他的 `user id`）时，第二条被中台当重复事件跳过。实测现场：日志 `钩子2：msg_id 已存在，幂等跳过` + 媒体回填 `record not found`。修法：`Update.HubMsgID(accountID)`（`telegram.go:1005`）三条退路一律带账号段（`tg_upd_<acct>_<id>` / `tg_<acct>_<message_id>` / `tg_cb_<acct>_<id>`），与出站 `telegramOutboundHubMsgID` 同口径；且 Ingress 的 `EventID`、dispatch 内存 hub 的 `MsgID`、回填找行的键**三处同源**（否则就是 N-10 在 TG 侧重演）。用例 `TestM01_TelegramHubMsgIDPrecedence`（8 子例，含「同一 `update_id` 不同账号各得各的键」）、`TestM01_TelegramSameUpdateIDAcrossAccounts`（中台真落两行 + 各回填各的）、`TestM01_TelegramBackfillScopedToAccount`、`TestM01_TelegramServiceFetchSeamTypes`。

### 14.4 变异电池（36 条，脚本 `/tmp/f4-mut-tg/battery.py`，隔离副本 `/tmp/rf1-hivemtk`）

三个被测文件：`internal/channelbot/telegram/telegram.go`、`internal/service/telegram_media.go`、`internal/service/webhook_channel_telegram.go`。首轮 `CONTROL passed=60 fail=0 skip=0`，逐条结果（`caught` = 该变异让多少条用例转红）：

| # | 被掐掉的口径 | caught | 首个红测 |
|---|---|---|---|
| T1 | 归一层整体无视媒体容器（M-01 原状） | 26 | `TestM01_TelegramDocumentKeepsFileName` |
| T2 | animation 判序退回 document（GIF 存成压缩包） | 4 | `TestM01_TelegramGIFNotMistakenForDocument` |
| T3 | live_photo 判序退回 photo（丢掉动态那一半） | 3 | `TestM01_TelegramInboundMsgTypeInHubSet` |
| T4 | photo 取首档而非最大档 | 3 | `TestM01_TelegramImageIsStoredAndBackfilled` |
| T5 | 最大档只按像素面积（丢 `file_size` 主口径） | 4 | `TestM01_TelegramLargestPhotoFallsBackToPixels` |
| T6 | 媒体件丢掉 caption | 5 | `TestM01_TelegramInboundCaptionAndPlaceholder` |
| T7 | 幂等键忽略 `update_id` ⇒ 重投换两行 | 15 | `TestM01_TelegramHubMsgIDPrecedence` |
| T8 | 幂等键不带 accountID（N-16 回归） | 15 | `TestM01_TelegramSameUpdateIDAcrossAccounts` |
| T9 | 回调键退化 ⇒ dispatch 视图与落库行分叉 | 2 | `TestM01_TelegramHubMsgIDPrecedence` |
| T10 | Ingress 的 `EventID` 与 `HubMsgID` 分家（N-10 同款） | 12 | `TestM01_TelegramDocumentKeepsFileName` |
| T11 | `file_path` 缺失不报错（官方 Optional） | 2 | `TestM01_TelegramGetFileErrors` |
| T12 | `getFile` 非 200 也继续解析 | 2 | `TestM01_TelegramGetFileErrors` |
| T13 | `ok:false` 不报错 ⇒ 官方描述被吞 | 2 | `TestM01_TelegramGetFileErrors` |
| T14 | 下载前缀写成接口段 `/bot<token>/` | 2 | `TestM01_TelegramDownloadFileUsesFilePathPrefix` |
| T15 | 读满上限后不判超限（半截当完整） | 2 | `TestM01_TelegramDownloadTruncationDetected` |
| T16 | `LimitReader` 少读那 1 字节 ⇒ 超限不可判 | 2 | `TestM01_TelegramRealTruncationRejected` |
| T17 | `file_path` 守卫失效（绝对路径／`..`） | 1 | `TestM01_TelegramDownloadPathGuard` |
| T18 | `DoJSON` 出口不脱敏（N-15） | 1 | `TestM01_TelegramTokenNeverInError` |
| T19 | 下载腿不脱敏（同类泄漏只补一半） | 1 | `TestM01_TelegramTokenNeverInError` |
| T20 | 脱敏去掉早退 ⇒ 无 token 错误也换壳 | 1 | `TestM01_TelegramScrubKeepsUntokenedErrorIntact` |
| T21 | `redactedError` 丢掉错误链 | 1 | `TestM01_TelegramTokenNeverInError` |
| T22 | 20 MB 单位写错成 20 KB | 2 | `TestM01_TelegramMaxDownloadConstant` |
| T23 | dispatch 不触发转存 | 4 | `TestM01_TelegramRealGetFileThenDownload` |
| T24 | 落库类型写死 `text` | 7 | `TestM01_TelegramInboundMsgTypeInHubSet` |
| T25 | `p.Content` 不回填 ⇒ AI 与工作台两个答案 | 1 | `TestM01_TelegramImageIsStoredAndBackfilled` |
| T26 | dispatch 内存 hub 的 MsgID 与 Ingress 键分家 | 2 | `TestM01_TelegramEditedMediaKeepsType` |
| T27 | `getFile` 报明的体积不预检 | 1 | `TestM01_TelegramFetchRejectsOversizedDeclaredSize` |
| T28 | 事件自带 `file_size` 不预检 | 1 | `TestM01_TelegramOversizedMediaSkippedBeforeDownload` |
| T29 | 转存键不带媒体下标 | 3 | `TestM01_TelegramDocumentKeepsFileName` |
| T30 | 文件名 hint 丢掉官方原名 | 首轮 **BROKEN** → 修变异后 **1** | `TestM01_TelegramDocumentKeepsFileName` |
| T31 | hint 下标写成常量 | **声明等价**（见 §14.5） | — |
| T32 | 不写 `Extra.media_urls` | 1 | `TestM01_TelegramImageIsStoredAndBackfilled` |
| T33 | 不留 `file_id`（官方可复用，与 WA 7 天 media_id 不同） | 2 | `TestM01_TelegramImageIsStoredAndBackfilled` |
| T34 | 回填找行漏 `account_id`（N-10 跨账号变体） | 首轮 **0（missed）** → 补测后 **1** | `TestM01_TelegramBackfillScopedToAccount` |
| T35 | 凭证缺失门失效 | 1 | `TestM01_TelegramMissingTokenSkipsPersist` |
| T36 | 下载失败仍继续转存 | 3 | `TestM01_TelegramDownloadFailureKeepsPlaceholderRow` |

首轮汇总：`total=36 missed=['T30(BROKEN)', 'T34'] unexpected_red_on_equivalent=[]`，`FINAL(变异已全部还原) passed=60 caught=0 broken=False`。
针对两条缺口补测后单跑（`/tmp/f4-mut-tg/run2.log`）：`CONTROL passed=61`，`MUT T30 … caught= 1 ran=61/61`、`MUT T34 … caught= 1 ran=61/61`，`total=2 missed=[]`，`FINAL passed=61 caught=0 broken=False` → `ok hivemtk-user/internal/service 33.169s`。
36 条全量复认证（`MUST_PASS=61`）见 §14.6。

### 14.5 这轮电池教的四条口径（写下来是为了下次不再踩）

1. **BROKEN ≠ 漏测，但必须当漏测处理**。T30 初版把 `if name := strings.TrimSpace(ref.FileName); name != "" {` 整段删掉，而函数体里还在用 `name` ⇒ `internal/service/telegram_media.go:105:13: undefined: name`，整个 service 包 `build failed`，该轮**没有任何一条用例信号**（首轮日志里那 44/60 就是这么来的）。改法是**掐来源、不掐声明**：`strings.TrimSpace(ref.FileName)` → `strings.TrimSpace("")`，同一处语义变异、可编译、`caught=1`。
2. **missed 要先证明"可达"再判等价**。T34（回填 WHERE 去掉 `account_id = ?`）首轮 `caught=0`。根因不是等价，是**当时的用例只有单账号**——两个账号各转存一次的场景在测试里不存在，所以"漏 `account_id`"在这份夹具下与正确实现不可区分。补 `TestM01_TelegramBackfillScopedToAccount`（A 号转存不得写进 B 号那行）后 `caught=1`。⇒ 判等价之前必须先造出能观察差异的那一行数据，否则"等价"只是给漏测起的名字。
3. **真等价要写明理由并留在电池里**。T31（`hint := strconv.Itoa(i)` → `Itoa(0)`）声明为等价：TG 一条消息按官方互斥口径只带一种媒体，归一后 `refs` 恒为 1 条 ⇒ 下标在本仓观察不到差异。保留该条并把理由写进 `why`，是为了让下次读到它的人不必重新推一遍。
4. **`MUST_PASS` 必须等于真实用例数，否则门自己会假绿**。补测把用例数从 60 抬到 61 后，控制组打出 `CONTROL passed=61`、子集日志打出 `ran=61/60` —— 断言里那个 60 已经过期。这类"计数漂移"在 §9.3 的 F-1 电池出现过一次，这次是第二次：**每次往电池覆盖的用例组里加用例，必须同时改 `MUST_PASS` 并重跑控制组**，否则"跑满 N 条"这句话不再有任何含义。

### 14.6 门禁

- 全量 TG 电池复认证：`MUST_PASS=61 BATTERY_ROOT=/tmp/rf1-hivemtk/… python3 /tmp/f4-mut-tg/battery.py`（隔离副本，跑期间**不**做 live→shadow 同步，否则中途换文件会让某条变异打在混合树上、结论不可解释）。
- 定向：`(set -a; . ./.env; set +a); export POSTGRES_TEST_PORT=8232 POSTGRES_TEST_PASSWORD=…; go test -count=1 -p 1 -timeout 900s -run 'TestM01_Telegram' -v ./internal/service/` 与 `-run 'TestM01_Telegram' ./internal/channelbot/telegram/`。
- 真机腿（`TestM01_TelegramReal*` 三条）走 `tgAPIBaseOverride` 指向 `httptest`，用**真 `file_path` 拼接与真 LimitReader**，不打 `api.telegram.org`——真 token 那部分已在批D 的 `getMe`/投递里覆盖，媒体下载真机回归留在批D 复跑清单。

## 15. 批F-4c/d/e/f 修复与验证记录（2026-09-20，服务 M-01 其余半场 + N-17；连带 N-19 / N-20 / N-21）

### 15.1 N-17：官方类型名 ≠ 中台词表，三种失效形态各不相同

中台 `message_hub.msg_type` 只认九个字（`messageHubMsgTypes`，`service/message_hub.go:57-70`：text/image/file/audio/video/link/card/location + 本轮补进表的 `event`），而各渠道官方各有各的叫法。修复前**三条独立的失效路径**（按后果从重到轻）：

| 失效 | 机制 | 后果 | 收口 |
|---|---|---|---|
| ① 整条消息蒸发 | 企微走 `hub.Push → Normalize` **硬校验**词表：官方 `voice` 不在表里（中台叫 `audio`）⇒ `ErrMessageHubInvalidMsgType` ⇒ dispatch 上抛 | 客户发的语音**连一行 hub 都没有**，不是类型标错 | `webhook_channel_wecom.go:372` 传 `InboundHubMsgType(msgType)`，媒体判断仍用官方原值 |
| ② 行落得下、永久筛不到 | WA / 飞书 / 公众号直接 `repo.Create`（**绕过** Normalize）⇒ 落的是词表外的 `document`/`media`/`post`/`shortvideo` | 工作台按类型筛选（`repository` 的 `msg_type = ?`）与 `by_msg_type` 统计**永远**数不到这些行 | 同一张 `inboundHubMsgTypeAliases`（`message_hub.go:84-111`）在落库点收口 |
| ③ 正文蒸发 | 飞书富文本（官方 `post`）顶层没有 `text` 键，正文只在 `content[][]{tag,text}` | 客户写了一屏图文，AI 收到的是字面量 `"[post]"` | `feishuInboundText`/`feishuPostText`（`webhook_channel_feishu.go:292+`）把 title 与逐段文字拼出来 |

**兜底方向刻意不对称**（`InboundHubMsgType`，`message_hub.go:115-127`）：官方名既不在别名表、也不在词表 ⇒ 落到 `text`（宁粗不被拒，官方还会新增类型）；但**占位符仍回显官方原值**（`feishuInboundPlaceholder` 未知类型返回 `"[" + rawType + "]"`，`:279-284`），让客服看得见"这里有条我们没承载的消息"，而不是静默变成一个中文词。这条不对称是取证缺口带来的必然结果：企微/飞书的类型全集不可能穷举，被拒的代价（客户消息没了）远大于标粗的代价（类型显示成 text）。

A 档枚举（飞书入站 `msg_type` 可能值，`/tmp/chandocs/fs_server-docs_im-v1_message_create.md`，**33 256 字节**，`https://open.feishu.cn/document/server-docs/im-v1/message/create` 的事件回显段）：`text / post / image / file / audio / media / sticker / interactive / share_chat / share_user / system`。`feishuInboundPlaceholders`（`:253-275`）除覆盖这 11 个之外还多列了 folder/location/hongbao/calendar/todo/vote/merge_forward 等 —— 多列只会让"看得懂"更宽，不会漏；少列才会。`TestN17_AliasTableSelfConsistency` 把"别名表的每个值都必须在词表内、每个占位符键映射后进词表"钉成属性测试，所以表本身扩了也不会跑偏。

### 15.2 各渠道落点 × 用例

| 渠道 | 落点 | 用例文件 |
|---|---|---|
| QQ | `attachments[]` 承载（官方是数组，一条消息可多件）、逐件转存、`[图片]`/`[文件]` 中文占位符、只收 http(s) 且拒回环地址、下载前先按声明体积预检、`LimitReader(qqMaxMediaBytes+1)` 判超限 | `webhook_batchf4_m01_qq_test.go`（10 条，含 `TestM01_QQMultiAttachmentStoresEveryOne`、`TestM01_QQTruncatedDownloadIsRejected`、`TestM01_QQNon200DownloadIsNotStored`） |
| 钉钉 | `dingTalkInboundBody`：官方 `content` 容器（`downloadCode`/`recognition`/`duration`/`fileName`/`richText[]`）逐项解出，中台类型 + 中文占位符 + 下载码列表三元组；`dingTalkFlexInt` 兼容官方两种数字写法（String 与 Long 都出现过，整包 `Unmarshal` 是原子的，形态不符就 400 ＝ 把这条客户消息永久丢掉）；`createAt` 落 `hub.sent_at`；媒体走 `POST /v1.0/robot/messageFiles/download`（`downloadCode`+`robotCode` 两个都必填）换**预签名链接**再 GET 一次 | `webhook_batchf4_m01_dingtalk_test.go`（承载）+ `webhook_batchf4_m01_dingtalk_download_test.go`（转存腿，走 `dtMediaFetchFn`/`dtMediaStoreFn` 替身） |
| 飞书 | 类型经 `InboundHubMsgType`；`post` 正文拼装；`Extra` 带 `file_key`/`message_id`/`file_name`；`feishuMediaFetchFn`/`StoreFn`/`tokenFn` 三条外部 IO 腿可注入 | `webhook_batchf4_msgtype_test.go` 的 `TestN17_FeishuPostBodyIsExtracted`、`TestN17_FeishuResourceKeysAndResTypes`、`TestN17_FeishuVoiceAndMediaLandInHubWithRightType`、`TestN17_FeishuMediaFullChainBackfillsHubRow` |
| 企微 | ①`MsgType` 收口（下表①）；②媒体容器 `wecomMediaContainers` 大小写双收（`Image`/`image`…`ShortVideo`/`shortvideo`）—— N-09 已定性"顶层摊平与子对象嵌套都读"；③`isWeComMediaMsgType` 现含 `shortvideo`（理由见 §15.5）；④`ErrMessageHubIdempotent` 就地吞掉返回 `(nil,nil)`，否则重投会经 `handleJob` 落一条空内容 `unified_message` | `webhook_batchf4_msgtype_test.go` 的 `TestN17_WeComVoiceReachesHubInsteadOfBeingRejected`、`TestN17_WeChatShortvideoIsVideoTypeWithOwnBody`（公众号侧同型） |
| WhatsApp | 占位符表**单源化**：`whatsapp.InboundPlaceholder` 导出，`service.waMessageContent` 改为复用 —— 这张表原本两处各写一份，而 `Ingress` 才是真正落库的那一份，结果 sticker 在服务侧改成了「[表情]」、库里仍是 `"[sticker]"`，两边各自都不算错 | `TestN17_WhatsAppTypesMapIntoVocabulary` + `webhook_channel_whatsapp_test.go:97 TestWaMessageContent` |

`TestWaMessageContent` 值得单记：它**钉的就是缺陷本身**（期望值原本写死 `"[sticker]"`）。处置是改期望而不是改实现，因为实现方向与其余 8 个类型一致（全中文占位符），且库里那一行本就由同一张表写。这类"用例把缺陷当契约"的形状，与 §7.1 那批"夹具与实现互相印证"是同一族，只是更隐蔽——期望值看起来像个事实陈述。

### 15.3 钉钉 `richText`：N-17 的第三处现场，正是源码闸看不见的那一类

两道源码闸（`TestN17_NoOfficialTypeNameHardcodedAsHubType` 等）的判据是"**构造 hub 行的文件里的 `MsgType:` 字面量**"，而钉钉的 `richText` 是在适配器函数 `dingTalkInboundBody` 的 **`return` 语句**上产生的——既不在 `model.MessageHub{` 字面量块里，也不是 `MsgType:` 前缀。⇒ 闸全绿、库里的行仍然越表。这就是「门禁口径盲区」那条记的交界处：**门的四个覆盖面（枚举集合 / 触发 paths / 是否真跑 / 检查了几个对象）都不包括"返回值的类型"**。

处置在**源头**而不是下游守卫：`dingtalk_app.go` 的 `richText` 分支改判 `model.MsgTypeText`（正文已逐项解出：文字 + 每张图片一个占位符 ⇒ 类型按文本走，与飞书 `post` 同口径），并把不变量「第一个返回值恒在中台词表里」写成属性测试 `TestN17_DingTalkInboundBodyTypesInHubVocabulary`（10 行：`""`/`text`/`picture`/`audio`/`video`/`file`/`richText`/`actionCard`/`stream`/`WHATEVER_NEW`，逐行断言 类型==期望 && 在词表内 && 正文非空），同时把该不变量写进函数 doc-comment。

### 15.4 批F-4f：六处入站媒体读取收口成 `readInboundMedia`

`io.ReadAll(io.LimitReader(r, limit))` 读到正好 `limit` 就 EOF ⇒ **`size == limit` 与 `size > limit` 从读取结果上无法区分** ⇒ 半截文件当完整原件转存、回填、并在日志里写"媒体已转存"（图片只渲染一半、zip 损坏，且永久留在存储里，因为占位符已被长期 URL 替换）。`readInboundMedia`（`channel_media.go:100-109`）读 `limit+1`、据「正好读到 limit+1」拒收。

现存 **6 处**调用点（`channel_media.go:81`、`dingtalk_media.go:98`、`webhook_channel_feishu.go:438`、`webhook_channel_wecom.go:456`、`webhook_channel_whatsapp.go:202`、`wechat_inbound_media.go:155`）；`git diff` 可见的"修复前就存在的未防护读取"是 **4 处**（前一条的 64<<20 硬编码 + 飞书/企微/WA 三处的 `maxInboundMediaBytes`），另 2 处随本批新文件引入、在本批内一并收口 —— 数字必须写清是两个口径，写死一个下次读就对不上现场。QQ（`qq_media.go` 的 `qqMaxMediaBytes+1`）与 TG（`telegram.DownloadFile` 的 `maxBytes+1`）各自成形态，理由见下面的闸覆盖面。

### 15.5 反向测试（新写的校验必须先证明它会红）

四道临时变异，全部 `cp` 备份 → 改 → 跑 → 写回 → `md5` 逐次比对（禁对未提交文件跑 `git checkout/restore`）。四条红信息都是**实跑输出**，不是推断：

| # | 变异 | 期望红在 | 实跑 |
|---|---|---|---|
| M1 | `readInboundMedia` 的 `LimitReader(r, limit+1)` → `limit` | 行为用例 | `--- FAIL: TestN17_ReadInboundMediaRejectsOverLimit`（子例「恰好等于上限」「超一字节」双双转红） |
| M2 | `dingtalk_media.go` 的调用改写成 `(readInboundMedia)(…)`（形态不变、计数掉一） | 闸的反空转下界 | `webhook_batchf4_msgtype_test.go:242: readInboundMedia 只有 5 处调用点（各渠道入站媒体至少 6 处）：闸变成空转` → `--- FAIL` |
| M3 | `webhook_channel_telegram.go:200` 的 `MsgType: "event"` → `"message"` | 类型字面量闸 | `webhook_batchf4_msgtype_test.go:309: 以下 hub 类型字面量越出中台词表…：[webhook_channel_telegram.go:200 → "message"]` → `--- FAIL` |
| M4 | `wechat_inbound_media.go` 追加一行 `io.ReadAll(io.LimitReader(rc, maxInboundMediaBytes))`（六个调用点仍全在） | 闸的 offender 分支（与 M2 分离） | `webhook_batchf4_msgtype_test.go:245: 以下入站媒体读取没有超限拒收（LimitReader 未读 limit+1）：[wechat_inbound_media.go:155]` → `--- FAIL` |

还原后 `md5` 与备份逐个一致：`channel_media.go 0db787c9…`、`dingtalk_media.go 3ba3f770…`、`webhook_channel_telegram.go 0d17d854…`、`wechat_inbound_media.go 49f636d2…`、`webhook_batchf4_msgtype_test.go 839c3fcc…`；三条受影响用例复跑转绿。

M2 与 M4 是分开的两条，不是为了凑数：闸有两个判据（调用点数下界、逐行 `+1` 检查），一次变异只能证明一次。首轮我做的 M2 版是"删掉一个调用点"，它同时压低计数与制造 offender，红了也说不清是哪一半在起作用 —— 拆成两条之后每条各打一个判据。

**闸的两个已知覆盖面**（已写进用例注释，免得后来人把它当"全仓媒体读取都已收口"）：① 只走 `service` 包，TG 的下载腿在 `channelbot/telegram`（由 §14.4 的 T15/T16 钉）；② 认的是**常量名** `maxInboundMediaBytes`，QQ 用自己的 `qqMaxMediaBytes+1`，形态正确但既不会判红、也不计入 `guardedCalls`。

### 15.6 全量门禁跑出的 4 条红：两颗我自己埋的炸弹

`go test -count=1 -p 1 -timeout 1200s ./internal/...` → `FAIL hivemtk-user/internal/service 1160.664s`，**恰好 4 条 `--- FAIL`**（其余包全 ok），日志 `/tmp/f4-msgtype-full.log`：

```
dingtalk_official_contract_test.go:127: 应触发 AI 1 次，实际 0
  … INF [Inbox] 钩子3：最后一条 inbound 超过 5 分钟，历史消息不触发 AI channel=dingtalk conv_id=cid-77 event_id=dt-1-m-77
dingtalk_official_contract_test.go:210: 应触发 AI 1 次，实际 0
webhook_batchf4_m01_dingtalk_test.go:159: M-01(richText)：hub.msg_type = "text"，want "richText"（官方 msgtype 被写死成 text）
TestDingTalkReceiveMessage_CapturesSessionWebhookAndTriggersAI（同款"实际 0"）
```

- **时间炸弹（3 条）**：批F-4c 把 `hub.sent_at` 从 `time.Now()` 改成官方 `createAt`（`Timestamp: dingTalkMessageTime(msg.CreateAt)`）之后，常量夹具里那个 `createAt:1700000000000`（2023-11）**真的**走进了中台钩子3 的 5 分钟窗口判断 ⇒ 服务端按设计把这条当历史堆积、只落库不触发 AI。用例期望的是"应当触发"，实现期望的是"这是三条两年前的消息"。**测试写错的一方是我**，不是实现。修法：给"应当触发 AI"的用例换成一次性活时间戳 + nonce 会话 id（`dtLiveRobotMsg`，`dingtalk_official_contract_test.go`），而**拒绝类**夹具刻意保留冻结值（它们测的就是"过期不触发"）。
- **断言自相矛盾（1 条）**：批F-4c 的用例钉 `msg_type == "richText"`，批F-4d 的持久化守卫把它归一成 `text` —— 两批用例互相打脸，谁都没错在细节、错在**没有先定"谁是事实源"**。按 §15.1 的词表规则在源头裁决（§15.3），并留下属性测试。
- **顺带坐实 N-19**：`conv_id=cid-77` 之所以能被"两年前的历史"污染，是因为 `HasUnrepliedCustomerMessage`（`repository/message_hub_inbox_ctx.go:35-68`）的 WHERE **只有 `conversation_id`**，不带 `platform`/`account_id`，`ORDER BY sent_at DESC` ⇒ 固定会话 id 会把历次运行、别的账号、别的渠道的行算进同一段会话。这是中台侧的真实缺陷（跨账号判据串味），不是测试专有现象；已编号 N-19 列入 §5，处置待排（改键要同时评估"同一用户在多渠道会话 id 恰好同号"的既有依赖）。

### 15.7 本轮新增的两条发现

- **N-20｜承载用例打真外网（已修）**：`TestM01_DingTalkInboundMediaKeepsTypeAndDownloadCode` 走 `f4DtSetup`，而该 helper 没掐 `dtMediaFetchFn` ⇒ 5 条带 `downloadCode` 的用例各真发一次 `POST https://api.dingtalk.com/v1.0/oauth2/accessToken`（假凭据），日志回显 5 个真 `requestid` 与 `invalidClientIdOrSecret`。危害不是错，是**慢 + 依赖外网 + 观测噪音**，且与 QQ 承载用例（早已 stub）不一致。修法：`f4DtSetup` 里 stub 两条腿并 `t.Cleanup` 还原（`errors.New("f4dt: 承载用例不发起真实下载")`），转存腿仍由 `…_download_test.go` 逐字段断言。`go vet ./internal/service/` rc=0。
- **N-21｜入站 `sent_at` 口径只有钉钉跟官方时间戳**（待排）：`grep` 全 dispatch 得 WA（`webhook_channel_whatsapp.go:97`）、TG（`webhook_channel_telegram.go:159,202,322`）、飞书（`webhook_channel_feishu.go:219`）、QQ（`webhook_channel_qq.go:112`）、抖音（`webhook_channel_douyin.go:108,166`）仍写 `time.Now()`。危害与批F-4c 在钉钉侧修掉的完全同源（客服看到"处理时刻"而非"发送时刻"、重投事件的时序、响应时长统计），而钉钉那次的 3 条红也正是这个字段被改对之后才暴露的。逐渠道官方字段与单位不同（WA `timestamp` 秒、TG `date` 秒、飞书 `create_time` **纳秒**字符串、QQ `timestamp` 对象），必须逐渠道取 A 档原文再改，不能一把梭。

### 15.8 电池与门禁状态（诚实口径）

- **TG 36 条已跑完、结论干净（本轮把"进行中"改掉）**：`/tmp/f4-mut-tg/run3-full.log`（14:53 收尾）末尾逐字为
  `total=36 missed=[] unexpected_red_on_equivalent=[]` + `FINAL(变异已全部还原) passed=61 caught=0 skip=0 broken=False`，36 行 MUT 全部 `ran=61/61 skip=0 restored=ok`，等价类只有 T31 一条且带理由。
  该电池的三个锚点文件（`internal/channelbot/telegram/telegram.go` 09:15、`internal/service/telegram_media.go` 09:05、`internal/service/webhook_channel_telegram.go` 13:30）**mtime 都早于 14:53**，
  且与活树逐字节 md5 相同 ⇒ 这份结论对当前字节成立，可引用。
- **钉钉 D1–D12、企微 W1–W9：脚本已写、一次都没跑过**（两个目录里只有 `battery.py`，无日志）。⇒ 仍记遗留。
  **本轮新查出的前提问题**：两块电池的 `TREE`/`BATTERY_ROOT` 指向 `/tmp/rf1-hivemtk/hivemtk/user-server`，那是今天 **01:59 前后**的快照 —— 它的 `webhook_channel_douyin.go` 还是批G 之前的 6,072 B（活树 16,897 B，差 378 行），
  `internal/service/webhook.go live=3cb01f02 rf1=1e8a9a2a`、`internal/controller/webhook.go live=71fbd82f rf1=7ac767ad`、`webhook_batchf4_m01_dingtalk_test.go live=6a8be86c rf1=9bfa8737` 全不一致。
  钉钉/企微自己的锚点（`dingtalk_app.go`/`dingtalk_media.go`/`controller/wechat.go`）反倒与活树相同 ⇒ **在原样 rf1 上跑，测的是"新渠道码 + 旧承载与旧用例"的混合体，结论不可迁移到活树**。
  处置口径：跑之前用 `rsync` 做一次**整树快照**（不是逐文件补，逐文件补就制造上面这种混合），快照目录另起（如 `/tmp/dtwx-<日期>`），不复用 rf1；跑完的结论只对该快照的字节负责。
- 定向复跑（还原后重跑，非引用旧结果）：`-run 'TestN17_ReadInboundMediaRejectsOverLimit|TestN17_NoUnguardedMediaReadAtAnyChannel|TestN17_NoOfficialTypeNameHardcodedAsHubType'` → 三条 `--- PASS`、`ok hivemtk-user/internal/service 3.383s`。

## 16. 批G：抖音 dop 入站死路（2026-09-20，A 档取证 + 修复）

### 16.1 官方取证（A 档，六份原文已落盘）

`developer.open-douyin.com` 是 SPA（curl 直取只得壳），本轮用文本代理渲染后存盘：

| 落盘文件 | 字节 | 官方 URL |
|---|---|---|
| `/tmp/chandocs/jina_dy_summarize.txt` | 2,917 | `.../dop/develop/webhooks/summarize` |
| `/tmp/chandocs/jina_dy_event-list.txt` | 8,849 | `.../dop/develop/webhooks/event-list` |
| `/tmp/chandocs/jina_dy_mini_private-msg-webhook.txt` | 12,395 | `.../mini-app/develop/server/reach-marketing/instant-message/private-message/private-msg-webhook` |
| `/tmp/chandocs/jina_dy_dop_get-message-resources.txt` | 6,446 | `.../dop/develop/openapi/search-management/business-tool/get-message-resources` |
| `/tmp/chandocs/jina_dy_client_token.txt` | 2,774 | `.../dop/develop/openapi/account-permission/client-token` |
| `/tmp/chandocs/jina_dy_status.txt` | 6,047 | `.../dop/develop/openapi/status-code` |

逐字引用（引号内为原文）：

1. **验签**：「用户可通过请求 header 中的 X-Douyin-Signature 字段判断该消息是否来自抖音开放平台。 抖音服务端会将应用的(client secret + 消息体)使用 sha1 哈希作为 X-Douyin-Signature header 的 value」，同页 Go 例子 `h.Write([]byte(clientSecret))` → `h.Write(body)` → `fmt.Sprintf("%x", bs)` ⇒ **被签字节 = client_secret ‖ 原始 body，摘要 SHA-1，编码 hex**。且该例的 body 字面量正是 `{"event":"verify_webhook","client_key":"abc","content":{"challenge":12345}}` ⇒ **URL 校验请求本身也带签名**。
2. **challenge 回显**：请求 `{"event": "verify_webhook", "client_key": "", "content": {"challenge": 12345}}`；「当你收到开放平台 POST 验证请求时，你需要解析出 challenge 值，并立即返回该 challenge 值作为响应」「需要注意：返回内容需要放入 ResponseBody 里,不能直接返回；并且返回内容为 text 格式的 json 数据」⇒ 响应体 `{"challenge":12345}`。两个必须照做的细节：**示例里 challenge 是数字**（不像飞书是字符串），**示例里 client_key 是空串**（⇒ 不得要求它非空、更不得要求它与账号相等）。
3. **重试与去重**：「连接超过 5s 会自动断开，共重试 3 次」「用户可通过请求头中的 Msg-Id 进行去重」。
4. **统一信封**：`{event, from_user_id, to_user_id, client_key, content}` +「from_user_id 即为 open_id」+「不同的 event 对应不同的 content」；较新示例（`new_video_digg`/`contract_authorize`/`union_auth_info_for_c`）多带 `log_id`，且 **`content` 是 JSON 字符串而非对象**（`"content": "{\"action_type\":1,…}"`）⇒ 同一渠道内 content 有两种形态，只按对象解就是把另一半报文丢掉。
5. **私信事件**：`im_receive_msg`「接收私信，用户收到私信触发」、`im_send_msg`「发送私信，用户发送私信触发」；群聊另有 `im_group_receive_msg`/`im_group_send_msg`。content 字段：`conversation_short_id`(会话 ID)、`server_message_id`(消息 ID)、`conversation_type`(int，表格只写「1：私聊」)、`message_type`、`text`、emoji 的 `resource_url`/`resource_type`/`resource_width`/`resource_height`、video 的 `item_id`（「加密后的视频ID」）、`retain_consult_card` 的 `card_id`/`card_status`(1 空白/2 完成)/`card_data`[{label,value}]、`source`、`create_time`（int，「13位毫秒时间戳」）、`index`、`user_infos[]`。
   `message_type` 全集（原文逐项）：`text` / `image` / `user_local_image` / `emoji` / `video` / `user_local_video` / `retain_consult_card` / `other`。
6. **媒体**：「针对用户在会话中发送的本地图片、视频（msg_type 对应 user_local_image, user_local_video），支持通过获取消息中的多媒体资源接口，获取具体的 URL」；`GET https://open.douyin.com/api/im/message/resources/`，scope `im.multimedia_message`（企业开发者），header `access-token` 必填 + `content-type` 固定 `application/json`，query `open_id` 必填、`conversation_id`/`message_id` 分别取 `conversation_short_id`/`server_message_id`，且「由于 conversation_id 包含 + = 等特殊字符，传参时需要进行编码」；响应 `data.media_type`(image/video) + `data.url`（「有效期 30 天」）；「访问 URL 时，需额外在请求 Header 中携带 Access-Token, OpenID 字段，字段的值与调用本接口的AccessToken, OpenID 相同，否则无法访问相关资源」「注意：URL 中可能包含转义字符 \u0026，需要将其替换为 & 才能正常访问资源」「只能获取发送时间为半年内消息多媒体资源」；错误码 28001003/28001008（token 无效/过期）、28029020（未获取到资源链接）、28029016（不支持的消息类型）。
7. **client_token**：`POST https://open.douyin.com/oauth/client_token/`，body `{grant_type:"client_credential", client_key, client_secret}`，响应 `data.{access_token, expires_in:7200, error_code}`；「client_token 的有效时间为 2 个小时，重复获取 client_token 后会使上次的 client_token 失效（但有 5 分钟的缓冲时间…）」+「禁止频繁调用 access-token 接口（频控规则：5 分钟内超过 500 次接口调用，接口报错，错误码 10020）」⇒ **必须缓存**，逐次现取会自我作废并撞频控。

### 16.2 与现实现的差集（= 批G 范围，四处独立环节）

| # | 环节 | 官方 | 修复前实现 | 后果 |
|---|---|---|---|---|
| G-1 | 验签 | hex(sha1(secret ‖ body))，只发 `X-Douyin-Signature` | `verifyHMAC` = hex(HMAC-SHA256(secret, body))，且额外接受非官方的 `Signature` 头（`webhook.go:574-579`） | 真实推送 100% 验签失败 ⇒ 401，一条都进不来 |
| G-2 | URL 校验 | 同步回显 `content.challenge`（数字形态） | 无抖音分支（`controller/webhook.go:109-126` 只有 QQ op13 与飞书） | 控制台保存回调地址必然失败 ⇒ 渠道开不通 |
| G-3 | 信封 | `{event, from_user_id, to_user_id, client_key, log_id, content}` | `{event_type, data{message,from,to,conversation}}`（`webhook_channel_douyin.go:13-38`）—— 字段名与官方**无一对应**，示例里的 `im.message.receive_v1` 反而是**飞书**事件名 | 官方报文解出全空 → `p.Sender==""` → `:71-73` `return nil,nil,nil`：不落库、不进收件箱、不触发 AI，而 HTTP 仍回 200 ⇒ 抖音认为投递成功、按「5s/3 次」的口径不再重投，消息**静默蒸发** |
| G-4 | 类型/时间/媒体 | `message_type` 八值、`create_time` 毫秒、本地图片视频二次接口取 URL | `MsgType:"text"` 写死（:106）、`SentAt: time.Now()`（:108，即 N-21 的抖音半场）、无媒体链路 | 类型全错、时序全假、图片/视频只剩空气 |

### 16.3 缺口（不得当作已验证）

- **TikTok 报文形态**：官方页仍 404（§6）。批G 不把旧结构当作"TikTok 契约"保留 —— 它是飞书命名的臆造物，留着只会把猜测固化成兼容分支。TikTok 落 `dispatchDouyinGeneric`（"解不出的报文"路径），本仓不为未取证形态声明任何字段。
- **`im_send_msg` 方向**：事件页原文「发送私信，用户发送私信触发」，同页脚注又定义「`当前抖音用户`是指: 授权给开发者应用的抖音用户」。两处合读最自然的解释是"授权用户（商家侧）发出私信"⇒ 出站回显。但这是**推读不是原话** ⇒ 保守处置：落 `direction="outbound"` 且**不驱动 AI**。若真机证明它其实承载客户消息，症状是"消息在库但没自动回复"，而不是"消息丢失"，按本行改回即可。
- **`conversation_type` 的 2**：参数表只写「1：私聊」，同页文本消息示例却给 `"conversation_type": 2`。⇒ 本仓**不用它判群**（判群改用 event-list 明确列出的 `im_group_*` 事件名前缀），2 的含义留作真机观测点。
- **`index`**：表格 int、示例字符串 `"1672502407220000"`。⇒ 不声明该字段（本仓不需要；声明错形态会让整包 Unmarshal 失败，代价是丢整条消息 —— 与钉钉 `createAt` 同一课）。
- **官方沙箱（推翻旧结论）**：dop 有「模拟webhook事件」`POST https://open.douyin.com/sandbox/webhook/event/send/`（需 `aweme.webhook` 权限 + client_token，可 mock `verify_webhook` 等）—— 这**推翻** §3.8 里"抖音 ✘ 无公开沙箱"的旧判定。但它同样要求已获批应用与凭据，本仓 36 行 bridge 账号密钥长度全 0（§4）⇒ 结论从"永远无法验证"改到"有凭据即可自证"，本轮仍不可真机。另记：该页事件名写作 `receive_msg`/`enter_im`，与事件列表页的 `im_receive_msg`/`im_enter_direct_msg` **不一致**（官方两页互斥命名）⇒ 本仓取"事件列表页 + 私信事件页"两处一致的写法。
- **官方 `Msg-Id` 去重未接线**（G-2b 收尾时判定）：summarize 页原文「用户可通过请求头中的 Msg-Id 进行去重」是**能力**而非要求。本仓入站去重走 `generateEventID`（channel + account + 原始 body 哈希），而抖音的 3 次重投是**逐字节相同**的投递 ⇒ 命中同一 `event_id`，`webhook_events` 的唯一键同样把重投放下。不改接 `Msg-Id` 的实际原因：要动 `officialEventID` 的签名，而该签名同时被另两条在途改动线引用（改一处三处红）。**留作后续**，不是漏项。
- **TikTok 的 challenge 回显与媒体端点**：两家私信报文同源，但握手与 `im/message/resources` 的 TikTok 侧对应页仍 404/登录墙 ⇒ 不拿抖音契约往 TikTok 身上套：challenge 分支只挂 `channel == douyin`，`persistDouyinMediaAsync` 对 tiktok **一次都不发**（`TestG2B_TikTokNeverCallsDouyinEndpoint` 钉住"不发"，电池 M14 证明这条断言不是空话）。
- **留资卡片的完成态数据未消费**：`retain_consult_card` 的 `card_data[]`（label/value 形态的姓名·手机号·城市）是线索最直接的来源，本仓只落 `[留资卡片]` 占位符 + `Extra.card_id`。未消费的原因：该字段在私信 webhook 页以展开表格出现，未取到可逐字引用的键名清单 ⇒ 不照抄猜测。**留作 §4 拿到凭据后的真机观测点**。
- **媒体直链 30 天 + 半年窗口 ⇒ 入站当场只有一条腿**：官方 `data.url` 只 30 天有效，且「只能获取发送时间为半年内消息多媒体资源」。本仓因此在**入站当场**换链并转存（占位符之上回填自有存储的长期 URL）。
  二次检查后的口径：官方标「请重试」的三个码、以及带外的 5xx/429/连接层断链，会在**同一次入站动作内**按整动作退避重投至多 2 次（§17.1），但重投仍失败（最常见是应用未开通 `im.multimedia_message` 企业权限，回 28001018/28029020）时**没有**跨请求的延迟补取队列 —— 与 D-04 的非阻断降级同一口径：宁可留占位符，也不在 webhook 的 5s 窗口里同步等下载。
  残余风险按官方窗口记账：半年内手工补取**在理论上仍然可行**（消息 ID 已留在 `Extra.server_message_id`），本仓未做该队列，登记为未覆盖面而不是已验证。
- **入站不查浏览器桥的去重钩子**：webhook 与桥上报是两个独立入口，同一句话理论上可能各进一次。本轮不合并：`unified_message` 的 platform+content 钩子按**内容**去重，合并会把"客户连发两条相同内容"错误吞掉，而官方重投语义只覆盖"同一渠道同一事件"。留作观测点。

### 16.4 修复落点 × 用例（每条先红后绿，红测记录在括号里）

夹具一律**抄官方示例原文**（`@` 开头的 base64 会话/消息 ID、13 位毫秒、`conversation_type` 与事件名不同步、昵称只在 `user_infos[]` 里），造签名的算法独立按 §16.1 第 1 条实现（`gDyOfficialSign`），不复用被测实现的任何输出 —— 否则等于拿实现的反推当契约。

| 落点 | 实现位置 | 用例（`internal/service/` 除非另注） | 首轮证据 |
|---|---|---|---|
| G-1 验签算法 | `webhook.go:829 verifyDouyinWebhook` + `:838 douyinSignature` | `webhook_batchg_douyin_test.go`：`TestBatchG_DouyinVerify_OfficialSha1Contract`（含**反向锚点** `gDyLegacySign`＝修复前的 HMAC-SHA256 必须验不过）、`_HeaderSpelling`（4 种大小写）、`_NotSharedWithTiktokBranch`（两家分支互不吃对方签名） | 改前：官方签名 `ok=false`、legacy 口径同样 `false`（永不相等的一侧）⇒ 用例红；改后 3/3 绿 |
| G-1 头名规范化 | `webhook.go:846 headerFold` | 同上 `_HeaderSpelling` | 与 tiktok 同一坑同源修 |
| G-2a challenge 回显 | `webhook_channel_douyin.go:209 HandleDouyinURLVerification`；`internal/controller/webhook.go:132` 路由分支 | `internal/controller/webhook_batchg_douyin_http_test.go`：`TestWebhookRoute_Douyin_VerifyWebhookEchoesNumericChallenge`、`_ChallengeTypePreserved`（19 位大数不得变科学计数法）、`_VerifyWebhookRejectsUnsignedChallenge`（**验签在回显之前**）、`_MessageEventNotSwallowedByChallengeBranch` | 改前：控制器无抖音分支 ⇒ 回显 `{"challenge":…}` 根本不存在，4 条全红；改后 4/4 绿 |
| G-3 信封与方向 | `webhook_channel_douyin.go:248 dispatchDouyin`（+ `douyinEnvelope`/`douyinMessageEvent`/`douyinContentObject`） | `TestBatchG_DispatchDouyin_OfficialTextEventLandsHubRow`（"静默蒸发"的现场取证）、`_GroupFromEventName`、`_SendEventIsOutbound`、`_ContentAsJSONObjectOrString`、`_NonMessageEventsWriteNoRow`（四类非会话事件零落库）、`_UnknownEventWithSenderStillVisible`、`_SenderlessEventStillWritesNoRow`、`_UnparseableBodyStillGoesGeneric`、`_EnvelopeFieldsInExtra`、`_ExtraIsJSONSerializable` | 改前：`hub == nil`（客户消息在库里不存在）⇒ 首条即红 |
| G-4 类型/时间/幂等键 | `douyinBody`/`douyinPlaceholder`/`douyinEventTime`/`douyinMsgKey`/`douyinPlatformKeyPrefix` | `_MsgTypesInsideHubVocabulary`（官方 8 值 + 未知类型，9 子例）、`_TolerantNumberShapes`（字符串 create_time、缺 create_time 退 now）、`_MsgIDDeterministicAndWithinColumnLimit`（≤ varchar(100)、不含 base64 特殊字符、含账号段）、`_SameServerMsgIDAcrossAccountsBothLand`（N-16 抖音版）、`_MissingServerMessageIDFallsBackToContentKey`；TikTok 平台标记由批C 的 `TestDispatchDouyin_TikTokMustNotBeLabelledDouyin` 继续把守 | 改前：`MsgType` 写死 text、`SentAt=now`、`Platform` 写死 douyin ⇒ 三条各红 |
| G-2b 媒体两条腿 | `internal/service/douyin_media.go`（新文件）+ `dispatchDouyin` 内的 `persistDouyinMediaAsync` 挂载点 | `webhook_batchg2b_douyin_media_test.go` **25 条**（二次检查把 17 → 22 → **25**，见 §17.1/§17.2/§17.6）：`FetchWalksOfficialTwoLegContract`（含把 `access-token` **钉成 clt. 形态**而非"非空"）、`ClientTokenIsCachedAcrossBurst`（3 次取用只 1 次 token，盯官方频控 10020）、`StaleTokenRefreshesOnceAndRetries`、`ExpiredTokenCodeAlsoRefreshes`（28001008 同样重取）、`RetryableCodesRetryWithSameToken`（28001005/28001006/28029014 官方写明"请重试" ⇒ 重投且**不重取 token**）、`RetryBudgetIsBoundedAndTerminal`（上界=退避表长度，到点必须带 err_no 报出）、`PermissionCodesAreTerminal`（28001012/28001015 一次都不重试，原样带出 err_no）、`Gateway5xxRetriesEveryLeg`（4 子例：token 腿 502 / resources 腿 503 / resources 腿**连掐两次连接** / 下载腿 504；下载腿子例同时钉住"整动作重投会把 resources 再走一遍"⇒ res=2）、`Client4xxIsTerminalOnEveryLeg`（2 子例：下载腿 403 一次都不重投、resources 腿 429 反过来**必须**重投）、`TokenCacheKeepsExpiryMargin`（留 ~5 分钟提前量；有效期短于提前量则**不缓存**）、`OfficialIDsAreQueryEscaped`（服务端解出的值必须与原文逐字节相等，且 RawQuery 里 `%2B`/`%2F` 在场）、`ResourceURLUnescuesAmpersand`、`NonZeroErrNoIsNotASuccess`（错误应答带真直链 + 好直链正例腿）、`ResourceURLMustBeAbsoluteHTTP`（`file://`、空 host、相对地址一律拒 + 计数器正例腿）、`DownloadCarriesAccessTokenAndOpenID`（两腿 token **同一个值**）、`InboundUserLocalImageBackfillsMediaURL`（端到端 + `Extra` 不被抹 + 真接口零调用）、`MediaKeyHasNoPathSeparators`、`OnlyUserLocalTypesFetch`（正例腿前置 + 800ms 排空窗口 + 顺序断言）、`OutboundEchoNeverFetches`、`IncompleteOfficialIDsSkip`、`TikTokNeverCallsDouyinEndpoint`（抖音诱饵账号 + 正例腿，M14 打出来的形状）、`MissingClientKeySkips`、`RefreshThatDoesNotHelpIsTerminalWithErrNo`（强刷后仍 28001003 ⇒ 带 err_no 终态、末次必须带**新**那把 `clt.test-2`）、`EmptyTokenResponseIsTerminalAndUncached`（200 + 空 `access_token` 连铺两把 ⇒ 判终态且**不进缓存**）、`TokenLegNonJSONKeepsBodySnippet`（token 腿回 HTML 错误页 ⇒ 把响应体片段原样带出） | 先写测后实现：`-run 'TestG2B_' -test.v` 17/17 绿、0 跳（21.4s / 29.7s 两轮）；二次检查增补后 `-run 'TestG2B_\|TestBatchG_DispatchDouyin_SendEventIsOutbound'` = `ok 20.798s`（20 条）；再增补带外瞬时态 2 条用例后 = 22 条顶层 PASS、0 FAIL、0 SKIP、`ok 46.595s`；§17.6 那批再补 3 条（token 腿两格 + 刷不动的终态）后 = **25 条**顶层 PASS / 含子例 31 行 PASS / 0 FAIL / 0 SKIP、`ok 17.922s`（`/tmp/g-mut/mb25.log`） |

批次门禁：`go test -count=1 -p 1 -timeout 1800s ./internal/service/`（**无 `-run` 过滤**）= `ok 1065.736s`，0 FAIL。**但这道门跑在 16:20，早于 §17 的全部改动 ⇒ 对最终字节不作数**，复跑结果见 §17.5 末段（最终字节上的电池已回填，无过滤门禁仍在跑）。
另注：本条按惯例不带 `-test.v`，因此这份日志**只能证明"没有失败"，不能证明跳过数为 0**；跳数证据由上面各组的 `-test.v` 单跑给出（顶层用例数：G-1/3/4 共 18 条含 1 个 9 子例、G-2a 4 条、G-2b 25 条、TikTok 标记 1 条，全部 `skip=0`）。

## 17. 批G 二次检查（2026-09-20，三路只读审查 → 逐条实跑复核 → 修 / 判否）

批G/G-2b 收口后，用三条互不通气的只读审查线重查一遍：**① 专猎"假绿断言"、② 拿 A 档原文逐条对实现找漏项、③ 盘"从没跑过的覆盖面"**。
审查线的产出**一律先当二手材料**：下面每条都标了复核结论（真漏判 / 真缺陷 / 不成立），复核方式是把断言与被断的源码逐行读回、或把官方原文读回。

### 17.1 审查线②：A 档漏项（2 条成立，全部已处置）

- **可重试错误码被当终态（成立，P2，已修）**：`jina_dy_dop_get-message-resources.txt:147/151/164` 逐字为
  「28001005 系统内部错误，请重试 → 请求重试，若依然无解请向平台提交反馈」「28001006 网络调用错误，请重试 → **重试即可**」「28029014 资源签发失败，请重试」（28029014 的**处置列是空的**，"请重试"只在错误文案里 —— 收录它是因为文案就是官方口径，不是因为处置列）。
  同一批码在总错误码页 `jina_dy_status.txt:22-23` 的**文案不同**：28001005 写作「系统繁忙，此时请开发者稍候再试」、28001006 写作「网络调用错误，请重试」，处置列同样指向重试 ⇒ **同号不同页不同义**（与飞书 45033/45009 同一课），判"可重试"必须两页都读到才能下结论，只引一页就是把措辞当契约。
  修复前实现只认 28001003/28001008 两个凭证码，其余一律终态，注释还写着「其余 err_no 都是终态」—— 这句是**我当时未经原文核对的断言**，正是它把三个官方明写重试的码关在门外。
  代价：媒体腿是入站一次性动作，平台抖一下 ⇒ 这条消息**永久**没有媒体，现场只有一行 Warn。
  修法：`douyinMediaRetryable`（`douyin_media.go`）按 `dyMediaRetryBackoff`（200ms/600ms，长度即上界）**重投同一个 token** ——
  重投不重取 token，否则把"非凭证问题"变成官方「重复获取使上次 token 失效」的顶号事故。
  新增 3 条用例 + 3 条变异（M19 永不重试 / M20 去掉上界 / M21 把权限码也纳入重试）。
- **`access-token` 到底填哪一类 token（成立，**未证项**，不靠猜改）**：resources 页 `:28-30` 的示例值是
  `act.943da17996fb5cebfbc70c044c3fc25a57…`（抖音**用户级** access_token 的形态），而 `open_id` 那行写「通过/oauth/access_token/获取，用户唯一标志」（`:42`）；
  反面证据同样硬：client_token 页 `:87` 的示例值是 `clt.75c380db41e815978a733994d96f5d23…`，resources 页自己的 Java 示例只配了
  `setClientKey("tt******").setClientSecret("cbs***")`（= SDK 内部拿 client_token），且**私信发送方从未授权过本应用，本仓根本拿不到它的 act. token**。
  ⇒ 两处都属"示例值"而非约束语句，**文档不足以判定，也不足以否证**。本仓维持 clt. 口径，做三件事代替猜：
  ① 断言从"头非空"改成**钉形态**（`strings.HasPrefix(got, "clt.test-")`）—— 原断言换任何一种 token 都照样绿，等于没钉；
  ② 28001012（未授权该 OpenAPI）/28001015（`access_token` 与 `openId` 不匹配，`jina_dy_status.txt:26,28`）单独判为**终态且必须原样带出 err_no**，
  不能被重试噪声冲销（`TestG2B_PermissionCodesAreTerminal` + 变异 M21）；
  ③ **判据的读法**（本轮二次检查改口：原先写"真机若回 28001015 就是这条口径错了的**唯一**露头信号"——那是把"我们希望的观测"当成了"唯一的观测"，过强）。
  token 类填错，平台把它落在哪一格不由本仓决定：`28001015`（不匹配）、`28001012`（该 OpenAPI 未授权）、以及 `28001003/28001008`
  （被判成 token 无效——这一格会先白跑一次 force-refresh、再原样终态）**都是同一条口径错了的后果** ⇒ 真机回归时四个码都要看，不许只盯 28001015。
  读法不需要额外埋点：`douyinAPIError.Error()` 恒带 `err_no=%d err_msg=%s log_id=%s`，而媒体腿所有失败都汇到那一行 Warn
  `[Douyin] 入站媒体下载失败（占位符保留）`，所以直接在日志里 grep 该 Msg 再看 `err_no` 即可（命中任一 ⇒ 回本节改口径）。
  ④ 记在这里，等 §4 拿到已获批应用后一次真机自证。
  顺带登记官方另一条硬约束（本轮**不处置**，属部署形态问题）：client_token 页「正式上线后，测试环境不能使用正式的 client_key,client_secret 获取 token，否则会导致线上正式环境的 token 失效」
  ⇒ 任何共用同一 `client_key` 的环境（含本地真机联调）都会互相顶号；多环境部署前必须按环境分应用，或把 token 取用集中到单点。
- **上一条修完后自己复检出的延伸（成立，已修）—— 带外瞬时态与 in-band err_no 同权**：给 28001005 加上重投之后回读实现，发现同一代价被按通道形状区别对待了：
  官方错误码走 HTTP 200 + `err_no`，而网关抖动走的是 **502/503/504、429、以及连接层直接断链（EOF）** —— 这些在旧实现里落 `fmt.Errorf("… status %d")` 与裸 `err`，一律终态。
  更要紧的是**下载腿的非 200 分支此前一次都没被覆盖过**：假平台的 `dlStatus` 全文件无人改写（`grep -n dlStatus` 只有声明、默认值和读取三处），
  即 `douyin media download status %d` 这条错误路径是**纯未验证代码**，与 §17.2 那三条"0 断言"同一类，只是这次未验证的是实现分支而不是断言。
  修法：新增 `douyinTransientError`（带外瞬时态的载体）+ `douyinStatusRetryable`（5xx 与 429 收，其余 4xx 判终态：403 官方口径是「否则无法访问相关资源」）
  + `douyinErrRetryable`，并把重投从"资源接口这一条腿"上移到 **`FetchDouyinMessageResource` 整动作**（`douyinFetchMessageResourceOnce` 为一次走完三条腿的内层）。
  上移的理由是代价按整动作算：三条腿任何一条被打断，结果都是"这条消息永久没有媒体"。副作用要说清 —— **重投会把前面已成功的腿再走一遍**：
  `client_token` 命中缓存不发 HTTP（用例钉住 token=1），`resources` 会**再签一次直链**（下载腿失败后 res 从 1 变 2），多花一次 JSON 调用换来的是重投带新签 URL。
  新增 2 条顶层用例（`TestG2B_Gateway5xxRetriesEveryLeg` 4 子例：token 腿 502 / resources 腿 503 / resources 腿连掐两次 / 下载腿 504；
  `TestG2B_Client4xxIsTerminalOnEveryLeg` 2 子例：下载腿 403 不重投、resources 腿 429 **必须**重投）+ 4 条变异（M23 不认带外类型 / M24 4xx 也纳入重投 /
  M25 5xx 判终态 / M26 连接层错误不包成瞬时态）。"连掐两次"是刻意设计：只掐一次分不清是我们的退避还是 `net/http` 对**复用连接**的内部重试（首次拨号不重试），三次连接才能唯一确定整动作预算。
  **诚实标注**：官方从没写过 5xx 该怎么办，这一条是**推读**（依据是"后果逐字相同"，不是原话），已同样写在 `webhook_batchg2b_douyin_media_test.go` 头部。
  复跑：`-run 'TestG2B_' -test.v` = **22 条**顶层 PASS、0 FAIL、0 SKIP、`ok 46.595s`。

### 17.2 审查线①：假绿断言（5 条候选 → 3 条成立已修、2 条不成立）

| 候选（用例位置） | 复核结论 | 处置 |
|---|---|---|
| `TestG2B_NonZeroErrNoIsNotASuccess` 的 `dlCalls != 0` | **成立**：错误应答里 `data:{}` 是空 URL，本来就够不着下载腿 —— 把 err_no 检查整块删掉这一腿也不会红（电池里 M06 是被**另外两条**用例打红的） | 错误应答改带**真实可下的直链**，并在同一用例里加正例腿（好直链必须让 `dl` 从 0 变 1），使"0 次"成为证据而不是运气 |
| `TestG2B_ResourceURLMustBeAbsoluteHTTP` 的 `dlCalls != 0` | **成立**，且比上一条更结构性：计数来自 httptest 服务端，而 `file://`、`http:///no-host`、相对地址**根本发不出**能到达它的请求 ⇒ 这一腿永不为真 | 保留形状报错断言（M10 由它把守），末尾加正例腿证明计数器会被合法直链推动，并把这个"够不着"的事实写进注释 |
| `TestG2B_OnlyUserLocalTypesFetch` 的 0 断言无排空窗口 | **成立（脆弱，非当前漏判）**：转存腿是 `SafeGoDetached` 起的协程，5 次 dispatch 后立刻读长度，读到的是"还没被调度"；本轮 M12 能打红是因为每条 dispatch 内含 DB 写、协程已抢先把事件投进 channel —— 依赖调度，负载变了就不成立 | 正例腿**前置**（先证明这套替身真能推动计数），负例段前给 800ms 排空窗口（与同文件其余 0 断言同口径），断言改按事件名逐字节比对、并校验"先 fetch 后 store"的顺序 |
| `TestBatchG_DispatchDouyin_SendEventIsOutbound` 的 `if extra != nil && extra.NewOpportunity` | **成立**：自禁用前缀。`dispatchDouyin` 恒返回非 nil extra，条件一旦哪天不成立，这条静默失效 | 改为 `extra == nil ⇒ t.Fatal`，商机断言不再挂在守卫上 |
| `TestBatchG_DispatchDouyin_NonMessageEventsWriteNoRow` 的 `verify_webhook` 子例"由 sender 守卫满足而非事件白名单" | **不成立**：`webhook_channel_douyin.go` 里事件白名单判在 `:268-273`、sender 判在 `:280-287`，白名单在前者先返回；且该用例其余三类事件夹具都**带 from_user_id**（注释即为此而写），电池 M13 正是打在这条上 | 不改。审查线读的是行号区间而不是执行顺序，属**审查线自己的假报告** |

### 17.3 审查线③：覆盖面口径（4 条成立，登记为"不得当作已验证"）

- **出站侧对抖音不成立**：本仓**没有** `/im/send/msg/` 的任何调用点 —— 全仓检索 `im/send/msg`、`business_token`、`bus_act` 三词，除本文档自身外**零命中**（本仓只有一个 Go 服务 `user-server`，无第二个可承载实现的进程）。
  `webhook_outbound.go:661` 的抖音分支只写 `status=pending` 的行，实际投递经 `reach_pipeline_dispatch.go:65` → `BridgeReachAdapter`（桥/WS 侧）。
  ⇒ 本轮 A 档契约工作**只覆盖入站**；官方 send 侧的 `bus_act.` 业务凭证、msg_id 24h、1000 字与外链限制，在本仓**无对应实现可审**。这条不是"审过了没问题"，是"本仓不在此路径上"。
- **抖音媒体腿的既有门禁覆盖不到最终字节**：`ok 1065.736s`（16:20 那次）早于 `douyin_media.go` 的 17:19 改动 ⇒ 结论作废，必须按 §17.5 复跑。
- **批A 的 8 条"反证"只有叙述、无脚本与日志**；批B 有脚本（`/tmp/mutation-battery-batchb*.sh`）无日志；钉钉 D1–D12、企微 W1–W9 两块电池**一次都没跑过**（且其目标树 `/tmp/rf1-hivemtk` 的承载文件已过时，跑之前必须整树重新快照，见 §15.8）；
  f1/f2/f3 的电池日志不带 `-test.v` ⇒ PASS 计数恒 0，属"取证据失败"而不是"没红"。全部仍挂在 §15.8 / 批I–批E 待办里，**不得引用为已验证**。
- **限流取证按字节复核后只剩零档（比审查线报的还差）**：`fs_server-docs_guides_server-api-rate-limits.md` 与 `fs_server-docs_im-v1_message-rate-limit_rate_limit_description.md` 各 **26 B，正文就是 "This document is not found"** ⇒ 飞书限流**无 A 档**；
  钉钉两份（`jina_dt_limits.txt` 1,392 B / `jina_dt_quota.txt` 1,434 B）**正文是站点导航壳**（图标 + 菜单，无限流数值）⇒ 同样无 A 档；
  小红书 `jina_xhs_ov.txt` 416 B，正文是代理站的"输入密码访问"广告页 ⇒ 该渠道官方文档**一次都没取到**。
  ⇒ §10 那张限流表里这几家**只能按"未取证"读**（WA/QQ 侧有 `wa_rate-limits.html` 142,300 B、`jina_wa_msglimits.txt` 76,291 B、`wx_limit.html` 75,353 B 这类真原文，属可引用档）。

### 17.4 电池自身的三处口径修正（含一次自我作废）

- `tally()` 的 `ran` 改为**按顶层用例名去重**统计。旧口径 PASS 侧父+子双计、FAIL 侧父去重，
  子例一红必然 ran 少 1 ⇒ G15/G16 报的 `RANSHORT 26<27` 是计数器裂缝，不是"有用例没跑"（`battery2.log` 同一条显示 `passlines=25 rawfail=2 uniqfail=1`）。
  新口径同时输出 `ranpass=X/Y`、`rawfail`、`uniqfail`、`sigtops` 四个自证量，异常时能指出是哪一侧的裂缝。
- 各组 `must` 下限随之改成顶层数：G1 18（另有 9 子例）、CT 4、**MB 22**（二次检查从 17→20→22，每改一次都要回到 `-test.v` 重数，不能沿用上一轮）、TT 1。控制组跑不满 `must` 直接终止电池。
- **给电池打补丁不能用静默 `str.replace()`**：一次改 `battery.py` 用 python 脚本做文本替换，old 里少打一个空格 ⇒ 替换没落、零报错，
  而那条 RANSHORT 判定从此失去牙齿（`ran=27/18` 这种明显异常都拦不住）。这趟日志（`battery3.log`）跑到 G07 时发现，代价是整趟作废。
  改法：一律用 Edit 工具（未命中直接报错），或替换后 `assert old not in s`；并且**改完必须 kill 当前趟**（Python 启动时读入源码，detach 的那趟永远用不到新语义），
  把沙箱树里被中途留下的变异文件从活仓重新同步回去（那次 `webhook_channel_douyin.go` 就是脏的：`live=8b9cc43f sand=5ade1945`），再对合成日志单测新 `tally()`。
  ⇒ `battery3.log` 记为**已作废的过程证据**（只用于说明口径裂缝被发现和修掉），§17.5 只认最终字节上的那一趟。

### 17.5 最终代码状态上的电池与门禁

**表口径（跑之前写死，跑之后不改）**：逐条列 id / 组 / 落点 file:line + 锚点首行 / 破坏什么 / expect /
ran(通过/红) / caught / 打红的用例名 / skip / 还原 / 判定；填表前不得对沙盒树做任何 live→shadow 的局部补文件
（要么整树快照、要么零改动）。**行号取自活树**，渲染时逐文件比活树/沙盒 md5，不一致就当场报警而不是静默出错号。

**这一趟是哪一趟**：`/tmp/g-mut/battery5.log`（**48** 条变异、四组、一次跑完，不是 `ONLY=` 补丁腿）。
它取代 `battery4.log` 作为本节唯一认的表 —— 理由见 §17.4 末段：battery4 的 `expand()` 每条 op 各从磁盘重读原文，
同一文件的多处改动只有最后一条生效，M20 因此报出一条假的 `caught=0`。修法是**按文件叠加后再落盘**，
并对"叠加后没变化"的 op 直接 `PREFLIGHT FAIL`。开工前的机械预检：`PREFLIGHT ok：48 条变异的锚点全部唯一命中`。

**BASE（四份文件，活树 = 沙盒，逐字节）**：
`service/webhook.go md5=3cb01f02642e…`、`service/webhook_channel_douyin.go md5=8b9cc43fe2fc…`、
`service/douyin_media.go md5=5618f208261a…`、`controller/webhook.go md5=71fbd82f7177…`。
M20 的叠加已单独验过（展开后同一文件里 `attempt >= 1 << 20` 与 `time.After(10 * time.Millisecond)` **两处同时在**），
本趟它 `caught=1`（battery4 那条无效行的正解）。新增的 M27/M28（token 腿空值判定 / 响应体片段）各 `caught=1`。

**收尾机械校验（渲染前先跑过一遍判据，不靠眼看）**：48 行全在 ⇒ `ran==组 must`、`skip==0`、
`restored=ok`、`expect=catch` 且 `caught>=1` 四条同时成立，违规清单为空；
`find /tmp/g-mut/hivemtk -name '*.bak*'` 只剩 1 个 `user-server/bin/user-server.r17.bak`
（70 MB 编译产物，活树里同名同尺寸同 mtime 的既有文件，是整树 rsync 带进来的，不是电池的残留）。

== 控制组 / 还原后收尾（FINAL 必须与 CONTROL 同数、caught=0） ==
| 组 | CONTROL ran/must | CONTROL passed行 | FINAL ran/must | FINAL passed行 | FINAL caught | FINAL skip | FINAL broken |
|---|---|---|---|---|---|---|---|
| CT | 4/4 | 4 | 4/4 | 4 | 0 | 0 | False |
| G1 | 18/18 | 27 | 18/18 | 27 | 0 | 0 | False |
| MB | 25/25 | 31 | 25/25 | 31 | 0 | 0 | False |
| TT | 1/1 | 1 | 1/1 | 1 | 0 | 0 | False |

汇总行：`total=48 missed=[] unexpected_red_on_equivalent=[]`

| 变异 | 组 | 落点 | 破坏什么 | 期望 | ran(通过/红) | caught | 打红的用例 | skip | 还原 | 判定 |
|---|---|---|---|---|---|---|---|---|---|---|
| G01 | G1 | `service/webhook.go:839` `h := sha1.New()` | 退回修复前的 HMAC-SHA256(secret, body)（官方是 sha1 拼接）⇒ 真实回调 100% 验不过 | catch | 18/18（16 绿 2 红）（25 行 PASS） | 2 | …BatchG_DouyinVerify_HeaderSpelling, …BatchG_DouyinVerify_OfficialSha1Contract | 0 | ok | ✅ 被抓 |
| G02 | G1 | `service/webhook.go:839` `h := sha1.New()` | 签名不再覆盖 body ⇒ 任何一条合法签名可被复用到任意报文 | catch | 18/18（16 绿 2 红）（25 行 PASS） | 2 | …BatchG_DouyinVerify_HeaderSpelling, …BatchG_DouyinVerify_OfficialSha1Contract | 0 | ok | ✅ 被抓 |
| G03 | G1 | `service/webhook.go:839` `h := sha1.New()` | client_secret 不进被签串 ⇒ Anyone 知道算法即可伪造签名（官方：应用密钥 + 消息体） | catch | 18/18（16 绿 2 红）（25 行 PASS） | 2 | …BatchG_DouyinVerify_HeaderSpelling, …BatchG_DouyinVerify_OfficialSha1Contract | 0 | ok | ✅ 被抓 |
| G04 | G1 | `service/webhook.go:831` `if sig == "" {` | 缺 X-Douyin-Signature 头改成静默放行（fail open）⇒ 未签名报文直接进漏斗 | catch | 18/18（16 绿 2 红）（25 行 PASS） | 2 | …BatchG_DouyinVerify_NotSharedWithTiktokBranch, …BatchG_DouyinVerify_OfficialSha1Contract | 0 | ok | ✅ 被抓 |
| G05 | G1 | `service/webhook.go:852` `if strings.EqualFold(k, name) {` | headerFold 退成字面量取键 ⇒ HTTP 层规范化成 X-douyin-signature 后永远取不到 | catch | 18/18（17 绿 1 红）（26 行 PASS） | 1 | …BatchG_DouyinVerify_HeaderSpelling | 0 | ok | ✅ 被抓 |
| G06 | G1 | `service/webhook_channel_douyin.go:87` `case dyEventSendMsg, dyEventGroupSendMsg:` | im_send_msg 回声被当成入站 ⇒ 工作台看到自己说话等着回复、AI 回复自己 | catch | 18/18（17 绿 1 红）（26 行 PASS） | 1 | …BatchG_DispatchDouyin_SendEventIsOutbound | 0 | ok | ✅ 被抓 |
| G07 | G1 | `service/webhook_channel_douyin.go:94` `return strings.HasPrefix(event, "im_group_")` | 群事件不再按事件名判定 ⇒ 群线索/会话全部塌成单聊 | catch | 18/18（17 绿 1 红）（26 行 PASS） | 1 | …BatchG_DispatchDouyin_GroupFromEventName | 0 | ok | ✅ 被抓 |
| G08 | G1 | `service/webhook_channel_douyin.go:107` `if strings.HasPrefix(trimmed, `"`) {` | 只认对象形态的 content ⇒ 官方另一外壳（字符串包裹的 JSON）整条解不出 | catch | 18/18（17 绿 1 红）（26 行 PASS） | 1 | …BatchG_DispatchDouyin_ContentAsJSONObjectOrString | 0 | ok | ✅ 被抓 |
| G09 | G1 | `service/webhook_channel_douyin.go:133` `return time.UnixMilli(ms)` | 官方 13 位毫秒不换算 ⇒ 客服看到处理时刻而不是客户发送时刻（排序失真） | catch | 18/18（16 绿 2 红）（25 行 PASS） | 2 | …BatchG_DispatchDouyin_OfficialTextEventLandsHubRow, …BatchG_DispatchDouyin_TolerantNumberShapes | 0 | ok | ✅ 被抓 |
| G10 | G1 | `service/webhook_channel_douyin.go:188` `return hex.EncodeToString(sum[:])[:16]` | 幂等键退回官方裸 ID（88 字符 base64）⇒ 顶爆 msg_id varchar(100)，插入错误被吞掉 | catch | 18/18（17 绿 1 红）（26 行 PASS） | 1 | …BatchG_DispatchDouyin_MsgIDDeterministicAndWithinColumnLimit | 0 | ok | ✅ 被抓 |
| G11 | G1 | `service/webhook_channel_douyin.go:142` `if u.OpenID == openID {` | 昵称不再按 open_id 匹配 ⇒ 收件箱/线索上全是 UUID，客服看不出在跟谁说话 | catch | 18/18（17 绿 1 红）（26 行 PASS） | 1 | …BatchG_DispatchDouyin_OfficialTextEventLandsHubRow | 0 | ok | ✅ 被抓 |
| G12 | G1 | `service/webhook_channel_douyin.go:284` `if sender == "" {` | 无 from_user_id 也落库 ⇒ 长出一条永远无法回复的假会话（D-04 已确立禁止） | catch | 18/18（17 绿 1 红）（26 行 PASS） | 1 | …BatchG_DispatchDouyin_SenderlessEventStillWritesNoRow | 0 | ok | ✅ 被抓 |
| G13 | G1 | `service/webhook_channel_douyin.go:272` `return nil, nil, nil` | 非会话事件（进入会话/加群审核/授权）也被当消息落库 ⇒ 假会话 + 无意义 AI 触发 | catch | 18/18（16 绿 2 红）（25 行 PASS） | 2 | …BatchG_DispatchDouyin_NonMessageEventsWriteNoRow, …BatchG_DispatchDouyin_UnknownEventWithSenderStillVisible | 0 | ok | ✅ 被抓 |
| G14 | G1 | `service/webhook_channel_douyin.go:263` `if env.Event == "" || !haveContent {` | 信封解不动时不再走通用兜底 ⇒ 桥上报的怪报文彻底无留痕 | catch | 18/18（17 绿 1 红）（26 行 PASS） | 1 | …BatchG_DispatchDouyin_UnparseableBodyStillGoesGeneric | 0 | ok | ✅ 被抓 |
| G15 | G1 | `service/webhook_channel_douyin.go:169` `case "text", "other":` | message_type=other 不取正文 ⇒ 官方「请打开抖音app查看」那类提示变 [other] | catch | 18/18（17 绿 1 红）（25 行 PASS） | 2 | …BatchG_DispatchDouyin_MsgTypesInsideHubVocabulary | 0 | ok | ✅ 被抓 |
| G16 | G1 | `service/webhook_channel_douyin.go:164` `return "[" + messageType + "]"` | 未知类型统一压成 [消息] ⇒ 平台新增类型时看不出是哪种 | catch | 18/18（17 绿 1 红）（25 行 PASS） | 2 | …BatchG_DispatchDouyin_MsgTypesInsideHubVocabulary | 0 | ok | ✅ 被抓 |
| G17 | TT | `service/webhook_channel_douyin.go:397` `return string(ChannelTiktok), "tt"` | TikTok 平台标记写回 douyin ⇒ 两家消息在 hub/线索表里混成一家、幂等键互相吞 | catch | 1/1（0 绿 1 红）（0 行 PASS） | 1 | …DispatchDouyin_TikTokMustNotBeLabelledDouyin | 0 | ok | ✅ 被抓 |
| G18 | CT | `service/webhook_channel_douyin.go:240` `return json.RawMessage(`{"challenge":` + str…` | challenge 被加引号回显（数字→字符串）⇒ 抖音控制台判定校验失败，注册卡死 | catch | 4/4（2 绿 2 红）（2 行 PASS） | 2 | …WebhookRoute_Douyin_ChallengeTypePreserved, …WebhookRoute_Douyin_VerifyWebhookEchoesNumericChallenge | 0 | ok | ✅ 被抓 |
| G19 | CT | `controller/webhook.go:132` `if channel == service.ChannelDouyin {` | challenge 分支挂错渠道（挪到 tiktok）⇒ 抖音保存回调地址那一步永远过不去 | catch | 4/4（2 绿 2 红）（2 行 PASS） | 2 | …WebhookRoute_Douyin_ChallengeTypePreserved, …WebhookRoute_Douyin_VerifyWebhookEchoesNumericChallenge | 0 | ok | ✅ 被抓 |
| G20 | CT | `service/webhook_channel_douyin.go:225` `if !ok {` | 验签失败仍回显 challenge ⇒ 握手变成无门槛回声弹（任何人都能替我们「完成校验」） | catch | 4/4（3 绿 1 红）（3 行 PASS） | 1 | …WebhookRoute_Douyin_VerifyWebhookRejectsUnsignedChallenge | 0 | ok | ✅ 被抓 |
| M01 | MB | `service/douyin_media.go:198` `if e, ok := douyinTokenCache[clientKey]; ok …` | client_token 不再走缓存 ⇒ 每条媒体各敲一次，撞官方频控 10020 并互相顶号 | catch | 25/25（18 绿 7 红）（21 行 PASS） | 10 | …G2B_ClientTokenIsCachedAcrossBurst, …G2B_ExpiredTokenCodeAlsoRefreshes, …G2B_Gateway5xxRetriesEveryLeg, …G2B_PermissionCodesAreTerminal, …G2B_RetryBudgetIsBoundedAndTerminal | 0 | ok | ✅ 被抓 |
| M02 | MB | `service/douyin_media.go:298` `token, err = douyinClientTokenForce(ctx, cli…` | 28001003 后不重取 token ⇒ 带着坏值一路失败到 2 小时到点 | catch | 25/25（22 绿 3 红）（28 行 PASS） | 3 | …G2B_ExpiredTokenCodeAlsoRefreshes, …G2B_RefreshThatDoesNotHelpIsTerminalWithErrNo, …G2B_StaleTokenRefreshesOnceAndRetries | 0 | ok | ✅ 被抓 |
| M03 | MB | `service/douyin_media.go:327` `return e.ErrNo == dyErrTokenInvalid || e.Err…` | 只认 28001003 不认 28001008（官方两条处置相同）⇒ 过期不刷新 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_ExpiredTokenCodeAlsoRefreshes | 0 | ok | ✅ 被抓 |
| M04 | MB | `service/douyin_media.go:340` `douyinAPIBase()+douyinMsgResourcesPath+"?"+q…` | query 里的 + / = 不做百分号编码 ⇒ 官方 ID 被解成空格，只看到「这条消息没有媒体」 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_OfficialIDsAreQueryEscaped | 0 | ok | ✅ 被抓 |
| M05 | MB | `service/douyin_media.go:389` `return strings.TrimSpace(strings.ReplaceAll(…` | 字面量 \u0026 不还原成 & ⇒ 直链带着反斜杠去请求，拿到 404 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_ResourceURLUnescuesAmpersand | 0 | ok | ✅ 被抓 |
| M06 | MB | `service/douyin_media.go:367` `if payload.ErrNo != 0 {` | 只看 HTTP 状态码不看 err_no ⇒ 业务错误被当成功、再去下载空 URL | catch | 25/25（18 绿 7 红）（24 行 PASS） | 7 | …G2B_ExpiredTokenCodeAlsoRefreshes, …G2B_NonZeroErrNoIsNotASuccess, …G2B_PermissionCodesAreTerminal, …G2B_RefreshThatDoesNotHelpIsTerminalWithErrNo, …G2B_RetryBudgetIsBoundedAndTerminal | 0 | ok | ✅ 被抓 |
| M07 | MB | `service/douyin_media.go:344` `req.Header.Set("access-token", token)` | resources 不带 access-token 头（官方必填） | catch | 25/25（22 绿 3 红）（28 行 PASS） | 3 | …G2B_DownloadCarriesAccessTokenAndOpenID, …G2B_FetchWalksOfficialTwoLegContract, …G2B_RefreshThatDoesNotHelpIsTerminalWithErrNo | 0 | ok | ✅ 被抓 |
| M08 | MB | `service/douyin_media.go:204` `"grant_type":    douyinGrantTypeClientCred,` | grant_type 写成 client_credentials（官方固定值是 client_credential）⇒ 10002 参数错误 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_FetchWalksOfficialTwoLegContract | 0 | ok | ✅ 被抓 |
| M09 | MB | `service/douyin_media.go:402` `req.Header.Set("Access-Token", token)` | 下载直链不带 Access-Token/OpenID 头（官方明写否则无法访问） | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_DownloadCarriesAccessTokenAndOpenID | 0 | ok | ✅ 被抓 |
| M10 | MB | `service/douyin_media.go:395` `if err != nil || (u.Scheme != "https" && u.S…` | 直链形状不判 ⇒ 渠道回个 file:// 就带着凭证去开本地文件/内网端口 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_ResourceURLMustBeAbsoluteHTTP | 0 | ok | ✅ 被抓 |
| M11 | MB | `service/douyin_media.go:155` `if k := douyinMsgKey(r.MessageID); k != "" {` | 存储键直接用官方 base64 ID ⇒ 斜杠变子目录、点点点变穿越，只有转存成功时才走这条路 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_MediaKeyHasNoPathSeparators | 0 | ok | ✅ 被抓 |
| M12 | MB | `service/douyin_media.go:164` `return messageType == "user_local_image" || …` | 所有消息类型都去敲 resources ⇒ 官方只支持 user_local_*，其余白烧频控 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_OnlyUserLocalTypesFetch | 0 | ok | ✅ 被抓 |
| M13 | MB | `service/douyin_media.go:435` `if hubMsgID == "" || !ref.complete() {` | 官方三个 ID 缺一也照发 ⇒ 注定 28001007 参数不合法 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_IncompleteOfficialIDsSkip | 0 | ok | ✅ 被抓 |
| M14 | MB | `service/douyin_media.go:429` `if channel != ChannelDouyin {` | TikTok 也走抖音媒体端点（无 A 档依据）⇒ 静默失败还掩盖真实缺因 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_TikTokNeverCallsDouyinEndpoint | 0 | ok | ✅ 被抓 |
| M15 | MB | `service/douyin_media.go:439` `if err != nil || clientKey == "" || clientSe…` | 没配 client_key 也起换取 ⇒ 拿空凭证敲门；且掩盖「只有 webhook 密钥换不到 token」这一真实缺因 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_MissingClientKeySkips | 0 | ok | ✅ 被抓 |
| M16 | MB | `service/webhook_channel_douyin.go:375` `if inbound && douyinNeedsMedia(content.Messa…` | 出站回声也换媒体 ⇒ 官方「仅支持接收消息 webhook」，回声一条都不该发 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_OutboundEchoNeverFetches | 0 | ok | ✅ 被抓 |
| M17 | MB | `service/douyin_media.go:480` `extra := hub.Extra` | 回填时整包覆盖 Extra ⇒ 抹掉官方 server_message_id（报障对号的唯一抓手） | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_InboundUserLocalImageBackfillsMediaURL | 0 | ok | ✅ 被抓 |
| M18 | MB | `service/douyin_media.go:250` `ttl -= douyinTokenSafety` | token 按 expires_in 用满缓存、不留提前量 ⇒ 卡在「我们以为有效、平台已作废」的静默窗口 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_TokenCacheKeepsExpiryMargin | 0 | ok | ✅ 被抓 |
| M19 | MB | `service/douyin_media.go:71` `switch e.ErrNo {` | 官方写明「请重试」的 28001005/28001006/28029014 当终态 ⇒ 平台抖一下这条消息就永久没媒体 | catch | 25/25（23 绿 2 红）（29 行 PASS） | 2 | …G2B_RetryBudgetIsBoundedAndTerminal, …G2B_RetryableCodesRetryWithSameToken | 0 | ok | ✅ 被抓 |
| M20 | MB | `service/douyin_media.go:114` `if attempt >= len(dyMediaRetryBackoff) {`<br>`service/douyin_media.go:118` `case <-time.After(dyMediaRetryBackoff[attemp…` | 重试无上界（去掉退避表长度判定）⇒ 抖动时把 5 分钟 500 次的配额烧穿 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_RetryBudgetIsBoundedAndTerminal | 0 | ok | ✅ 被抓 |
| M21 | MB | `service/douyin_media.go:71` `switch e.ErrNo {` | 把 28001012/28001015（权限/token 与 openId 不匹配）也当可重试 ⇒ 重投无用，且把「口径错了」这一露头信号冲销在重试噪声里（该信号是 12/15/03/08 四个码，见审计 §16.1 第 8 条③） | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_PermissionCodesAreTerminal | 0 | ok | ✅ 被抓 |
| M22 | MB | `service/douyin_media.go:344` `req.Header.Set("access-token", token)` | access-token 头换成用户级 act. 口径（未经 A 档证实的另一种猜法）⇒ 钉口径的断言必须响 | catch | 25/25（22 绿 3 红）（28 行 PASS） | 3 | …G2B_DownloadCarriesAccessTokenAndOpenID, …G2B_FetchWalksOfficialTwoLegContract, …G2B_RefreshThatDoesNotHelpIsTerminalWithErrNo | 0 | ok | ✅ 被抓 |
| M23 | MB | `service/douyin_media.go:108` `var tr *douyinTransientError` | 整动作只认 in-band err_no、不认 douyinTransientError ⇒ 502 放过的后果与放过 28001005 完全相同 | catch | 25/25（23 绿 2 红）（24 行 PASS） | 7 | …G2B_Client4xxIsTerminalOnEveryLeg, …G2B_Gateway5xxRetriesEveryLeg | 0 | ok | ✅ 被抓 |
| M24 | MB | `service/douyin_media.go:86` `return code >= http.StatusInternalServerErro…` | 4xx 一律纳入重投 ⇒ 403「无法访问相关资源」是凭证/权限判定，重投只会把 3 次配额花在同一个拒绝上 | catch | 25/25（24 绿 1 红）（29 行 PASS） | 2 | …G2B_Client4xxIsTerminalOnEveryLeg | 0 | ok | ✅ 被抓 |
| M25 | MB | `service/douyin_media.go:86` `return code >= http.StatusInternalServerErro…` | 5xx/429 一律当终态 ⇒ 网关抖一下这条消息永久没媒体（与 M19 同一代价，只是走的是带外通道） | catch | 25/25（23 绿 2 红）（25 行 PASS） | 6 | …G2B_Client4xxIsTerminalOnEveryLeg, …G2B_Gateway5xxRetriesEveryLeg | 0 | ok | ✅ 被抓 |
| M26 | MB | `service/douyin_media.go:90` `return &douyinTransientError{fmt.Sprintf("do…` | 连接层错误不包成瞬时态 ⇒ 掐断的连接被当终态，整动作预算形同虚设 | catch | 25/25（24 绿 1 红）（29 行 PASS） | 2 | …G2B_Gateway5xxRetriesEveryLeg | 0 | ok | ✅ 被抓 |
| M27 | MB | `service/douyin_media.go:238` `if parsed.Data.AccessToken == "" {` | 去掉「access_token 为空」判定 ⇒ 把 "" 缓存两小时再拿它敲 resources，现场只剩一个看不出根因的 28001003（真根因是凭证不对/被频控，就在上一跳的响应体里） | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_EmptyTokenResponseIsTerminalAndUncached | 0 | ok | ✅ 被抓 |
| M28 | MB | `service/douyin_media.go:236` `return "", fmt.Errorf("douyin client_token p…` | token 腿解析失败不再带响应体片段 ⇒ 分不清是官方在应答还是中间层（登录页/错误页）截了请求 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_TokenLegNonJSONKeepsBodySnippet | 0 | ok | ✅ 被抓 |

汇总：total=48 日志行=48 被抓=48 missed/未跑=无 未编译=无 控制组=CT:4/4 G1:18/18 MB:25/25 TT:1/1

**门禁（无过滤全量，本线实跑，2026-09-20 20:20–21:10）**：§17.5 认的表到此为止；不带 `-run` 的整包结果如下，
跑法一律 `cd user-server && set -a && . ../.env && set +a && export POSTGRES_TEST_PORT=8232 POSTGRES_TEST_PASSWORD="$POSTGRES_PASSWORD"; go test -p 1 -count=1 -timeout 2500s <pkg>`，
跑批器 `/tmp/gate-batchG.sh`（日志 `gates/gate-batchG.log` 及 `gates/*.log`，均已镜像到 `.tmp_files/mut-evidence-2026-09-20/gates/`）。

| 门 | 结果 | 逐字 |
|---|---|---|
| `./internal/router/` | ✅ 绿 | `rc=0 elapsed=12s declared_tests=125 last_line=ok hivemtk-user/internal/router 3.952s` |
| `./internal/controller/`（第一次） | ❌ 红 3 条 | `rc=1 elapsed=119s declared_tests=793`；红因原文见 §17.9 表头三条（upload_test.go:510 / webhook_qq_fullchain_test.go:414 / wechat_batchf4_m01_inbound_test.go:136） |
| `./internal/controller/`（三条处置后复跑） | ✅ 绿 | `rc=0 elapsed=219s`，包内 `ok hivemtk-user/internal/controller 207.467s`，`--- FAIL` 计数 0 |
| `./internal/service/` | ✅ 绿（在改名快照树里跑，见下） | 活树直跑两次都在 2s 内 `FAIL hivemtk-user/internal/service [build failed]`：20:25 那趟是 `opportunity_test.go:36:50: undefined: OpportunityService`（连带 `NewOpportunityService`/`OpportunityMove`）+ `opportunity.go:389:31: undefined: clientVersion`；21:05 那趟是 `local_asset.go:85:12: undefined: platform`、`:86:5: undefined: errors`。**两组符号都不属本线批次**，是并行泳道正在改的文件被读到半截 ⇒ 那两次记"未跑成"，既不记绿也不记红。改到快照树跑通：`rc=0 elapsed=723s`，包内 `ok hivemtk-user/internal/service 691.281s`，`--- FAIL` 计数 0 |

**service 快照门的字节对齐（这条决定了那个绿能不能算在活树头上）**：快照 = `rsync` 出的改名树
`/tmp/svcgate-210405/hivemtk/`（排除 `.git`/`bin`/`logs`/`tmp`），起跑时刻 `21:07:55`、`concurrent_go_test=0`
（没有别的 `go test` 在同刻抢共享 Redis DB15 与 8232 测试库 —— service 包对这两样都是敏感的，
见 `reach_gcra_limiter_test.go` 会 flush DB15）。跑完后把快照与活树逐文件比 md5：
**`user-server/internal` 下 2,564 个 `.go` 文件，差异 0 个** ⇒ 这趟绿所认的字节就是活树现在的字节，
快照只是躲开"读到半截文件"这一类，不是躲开"结果不适用"。原始行镜像在
`.tmp_files/mut-evidence-2026-09-20/gates/service-snap.meta`（含对比计数），跑完快照即删，不留 8.5G 副本。

**与本线数字不同的另一次测量（不合并、不改口径）**：另一泳道报过 router `207.961s`。同一份代码上
本线实测 router 包内 `3.952s`、controller 包内 `207.467s` —— 那个数量级属于 controller 而非 router，
两次测量的包很可能被对调了。跨泳道引用耗时前先核对是哪一道门。


### 17.6 二次检查的第三批产出（电池在跑的同时自查出来的六件事）

- **token 腿还有两格从没被任何用例踩过（成立，已补测）**：假平台的 token 分支只会回
  `{"data":{"access_token":"clt.test-N",…},"message":"success"}` 或一个非 200 状态，而实现里判在两者之间的两条
  —— `parsed.Data.AccessToken == ""`（官方给 `error_code`/`description` 两个字段就是为这一格：凭证不匹配、以及明写的频控 10020）
  与 `json.Unmarshal` 失败（中间层把请求换成 HTML 错误页）—— **补测前 `grep` 全仓零命中**（`client_token parse` 只在实现里出现过）。
  与 §17.1 那条下载腿非 200 同一类：未验证的是实现分支，不是断言；而这一格的代价是**根因被洗掉** ——
  删掉空值判定就会把 `""` 缓存两小时、再拿它去敲 resources，现场只剩一个看不出所以然的 28001003。
  补 3 条用例（MB 组 22 → **25**）：`RefreshThatDoesNotHelpIsTerminalWithErrNo`（强刷后仍 28001003 ⇒ 带 err_no 终态、
  且末次必须带**新**那一把 `clt.test-2`，只数次数等于没证明"刷"发生过）、
  `EmptyTokenResponseIsTerminalAndUncached`（连铺两把空应答 ⇒ 顺带钉"空值不进缓存"；并判终态，因为 10020 现场按 200ms/600ms 连打只会更糟）、
  `TokenLegNonJSONKeepsBodySnippet`（只要求把响应体片段带出来，**不**推断它是瞬时还是配置错）。
  电池侧随之补两条锚点（去掉空值判定 / 去掉 body 片段），按 §17.4 的规矩用 `ONLY=` 单腿重跑，不重跑整块。
- **同一格在别的渠道也空着（成立，登记待补，不混进本批）**：顺着上一条把全仓扫了一遍，"200 + 空 access_token"的判定还有两处同形实现 ——
  `internal/service/dingtalk_media.go:128`、`internal/channelbot/qq/qq.go:115`，而测试文件里带空 `access_token` 的应答体**只有本批新写的那一条**。
  ⇒ 这两处同属"分支从未被任何用例执行"，与 §17.1 的下载腿、上一条的 token 腿同一类。已登记为待补（每处 1 条同形用例即可，不必另起电池）；
  本批不动它们，是因为改动面要守住 §17.5 的口径 —— 电池 BASE 是那四个文件的最终字节，往里再加别的渠道文件就得整块重跑。
  **（此条"每处 1 条即可"已被 §17.7 的探针推翻：QQ 那一格不是缺一条用例，是既有用例走错了分支且字段类型未取证；钉钉那一格还差一个注入缝。以 §17.7 为准。）**
- **A 档语料本身没有 durable 底座（成立，已修）**：整份审计的"字节数 + 行号"引证全部指向 `/tmp/chandocs/`，
  而 88 份原文（合计 **14,913,472 B**）在仓库内、git 历史里、以及 `/tmp` 之外**都没有第二份**
  （`git log --all -S"jina_dy_dop_get-message-resources"` 零命中；`find` 全盘无同名目录）。
  macOS 重启即清 `/tmp`、且三日不访问也会被回收 ⇒ 一旦清掉，§10/§14/§16 里所有"逐字原文"立刻退化成不可核验的转述，
  而那正是本节 A/B 分档想防的事。处置：整目录复制到仓库外的 `.tmp_files/chandocs-2026-09-20/`（不入 git，避免把第三方 HTML 正文塞进版本库），
  生成 `MANIFEST.md`（文件名 + 字节数 + md5 三列 × 88 行），`diff -r` 与 `/tmp` 原件**逐字节一致**。
  今后引证以该清单为准；两份对不上时先复核清单再下结论。
- **同一格还漏了电池证据本身（成立，已修）**：上一条只镜像了 A 档语料，但 §11~§14、§16.4、§17 的每个电池数字
  （48/48、25/25、`ok 17.922s` 之类）同样只落在 `/tmp/g-mut/`、`/tmp/f[1-4]-mut*/`、`/tmp/qqprobe/` 下，
  跟着 `/tmp` 一起没的话，"跑出来的结论"就退回成转述。处置：30 份日志与脚本复制到
  `.tmp_files/mut-evidence-2026-09-20/`（同样不入 git），`MANIFEST.md` 列出**镜像路径 + 字节数 + md5 + 引用节 + 原件路径**，
  复制后逐字节回读并与原件比对；**`/tmp` 下的 `hivemtk/` 工作树副本不镜像**（那是变异的临时检出，无证据价值）。
  清单末尾另列"已知失效项"：`battery.log`/`battery2`/`battery3*` 是修正前跑批，`battery4.log` 的 M20 行作废 ——
  引用电池数字前先核对它出自 `battery5.log`（或 `mb25.log`），否则按未取证据处理。
- **"空断言"排查从抽样变成全量（结果：0 例外）**：脚本扫 25 个 `*batch*_test.go`／`*f4*_test.go` 的每一个顶层 `Test*`，
  判据是函数体内 `t.Errorf|t.Error|t.Fatal|t.Fatalf|t.Fail` 计数为 0 **且**不含 `t.Run` ⇒ `candidates 0`。
  §17.2 那三条是逐份读出来的，这一条是机械扫出来的，覆盖面口径不同：它只能证明"没有整条不判的用例"，
  不能证明"每条判得够细"（后者仍靠变异电池）。
- **"已修"行引用的文件确实还在树上（机械核过，只到存在性这一层）**：文档里含"已修/已落"的表行 16 条，逐行抽出被点名的 `.go` 文件，
  对着 `user-server` 全树 1,929 个 `.go` 文件名做集合差 ⇒ **缺失 0**（两条假缺失是一行里写成 `…_download_test.go` 的省略路径，
  实名 `webhook_batchf4_m01_dingtalk_download_test.go` 在树内）。这条防的是"并行会话把上批改动静默还原"那类事故；
  但它**只证明文件在**，不证明行为对 —— 行为那一层仍归各批的用例与门禁，批A 的重新取证（§待办）不因此关闭。

### 17.7 QQ / 钉钉 token 腿的探针（把 §17.6 第二条从"没有用例"读到"用例是错的"）

**方法**：共享工作树里 `internal/channelbot/qq/qq.go` 与 `qq_test.go` 都是别的会话的未提交 `M` 文件，
不能往里写 ⇒ 用 `go test -overlay` 把探针测试**虚拟**挂进包目录（`/tmp/qqprobe/zz_probe_test.go` + `overlay.json`），
工作树零改动，跑完即弃。token 腿本身在两版里都没被改动（`git diff` 该函数无 hunk），所以探针结论对 HEAD 同样成立。

**结果 A（成立，且比 §17.6 说得更重）：QQ 唯一的"token 失败"用例其实没测 token 失败。**
`qq_test.go:380 TestClient_TokenErrorSurfaces` 的夹具是 `{"code":"100014","message":"invalid appid"}`（`code` 带引号），
而实现里 `tokenResp.Code` 声明为 `int`（`qq.go:74`）⇒ 实测该夹具打中的是**解析分支**，原样回文：

```
PROBE-A err=qq token parse (status 200): json: cannot unmarshal string into Go struct field tokenResp.code of type int
PROBE-B err=qq token empty (status 200 code=0 msg=)
```

用例只判 `err != nil`，所以它绿着，但它证明的是"JSON 类型不匹配会报错"，不是它名字声称的那件事；
`qq.go:115` 的 `token empty` 判定至今**没有任何用例到达**（探针 B 是第一次让它出声）。
同族还有一处**测试内部的自相矛盾**：同一包的 `TestClient_SendMessage_PlatformError` 夹具写的是数字 `"code": 11253`（无引号）——
两处夹具对同一平台的同一字段一个写字符串一个写数字，说明当初没人按 A 档定过类型。

**取证状态**：为定这个类型抓 `bot.q.qq.com/wiki/develop/api-v2/dev-prepare/interface-framework/api-use.html`，
返回内容指向带签名的 OSS 代理域、且同一 URL 两次内容互不相容（一次成功体、一次称端点是 `api.bot.qq.com`）⇒ 全部拒用，
**QQ `code` 到底是字符串还是数字未取证**（详见 §6 表 N-21 行末）。批J 开工前必须先补这一档：
若官方为字符串，那"把字符串塞进 `int`"会让**任何**非零错误码退化成一条 parse 错误，
错误码/`message` 全部丢失，`channel_error` 也拿不到可归类的 `core.APIError`（对比：同批别的改动已把发送失败改成回 `*core.APIError`），
退避与归类一起失灵 —— 这一格是"改了能救回现场"，不是洁癖。

**结果 B（成立，处置方式与抖音不同）：钉钉 token 腿连"能不能测"都还没解决。**
`dingTalkNewAccessToken`（`dingtalk_media.go:107`）把 URL 硬编码成 `https://api.dingtalk.com/…` 并共用 `httpclient.Client`，
全仓 `grep` 无任何测试引用该函数；`dtMediaFetchFn` 那层替身把整条腿（含取 token）一起换掉了 ⇒
`dingtalk_media.go:128` 的空值判定从未被执行。要补用例得先选注入方式：仿抖音的 `dyAPIBaseOverride` 加一个只给用例用的 base 变量，
或临时换 `httpclient.Client.Transport` 改写到 `httptest` 服务（动的是包级全局，须成对还原，见口径）。
**缝的写法仓内已有两个先例**（`telegram_media.go:39 tgAPIBaseOverride`、`douyin_media.go:130 dyAPIBaseOverride`，
两处都注释了"仅测试用、生产留空"），所以这不是新决策、只是把第三个渠道补齐 —— 批J 里它排在补测前面。
本批不在此处展开，登记进批J。

**结果 C（新发现，第三处同形站点，且是"连判定都没有"）：飞书 token 腿缺空值判定。**
`feishu.go:347 getAccessToken` 只判 `out.Code != 0`；`code==0` 而 `tenant_access_token` 为空时，
会把 `""` 连同 `TokenExpires` 一起写进账号并返回 `("", nil)`（缓存里那次写无害：`AccessToken==""` 使下次仍走取 token，
不构成"缓存两小时空值"那种抖音式后果），但**调用方拿到空 token 且 err 为 nil**。
按仓内既有口径这本来不该发生：TG（`telegram_media.go:81`）、企微（`webhook_channel_wecom.go:446`）、
公众号（`wechat_inbound_media.go:144`）、WhatsApp（`webhook_channel_whatsapp.go:187`）以及飞书自己的媒体腿
（`webhook_channel_feishu.go:428`）全都是 `if err != nil || token == ""` 的双保险写法；
唯一漏掉后半句的是飞书**出站**发送腿 `feishu.go:234` —— 空 token 会真的一发 HTTPS 请求打出去，
现场留下的是平台侧错误码而不是"token 是空的"。严重度低于抖音/钉钉那两格，但同一类，一并登记批J。

**结果 D（新发现，一条"看起来在防双触发"的用例其实钉的是一个生产没人调的函数）**：
把 §17.6 的"零判定用例"机械扫描从 25 个 `*batch*_test.go` 扩到审计范围内的五个包
（`internal/{service,controller,channelbot,bridge,channelgw}`，共 **5,266** 个顶层用例），
命中 **122** 条零判定候选，其中绝大多数是"不 panic / 并发无数据竞争"型（判据落在 race detector 与 panic 上，属合法口径）。
渠道侧只有一条值得单独看：`webhook_channel_qq_test.go:259 TestQQ_TriggerSalesEngineGuard`
（函数体两次调用外加一句 `_ = bytes.MinRead` 填充，**零断言**）。往下读实现才看清它的问题不止"没断言"：

- `webhook_channel_qq.go:146 triggerQQSalesEngine` 自带 `//nolint:unused //// 仅被 *_test.go 引用，生产路径未用`，
  全仓 `grep` 确认：除该测试外**零调用点**。
- QQ 真正的防双触发写在别处：`webhook.go:972` 的 `if triggerAI && channel != ChannelQQ`
  （注释说明 QQ 走 `dispatchQQ → e.Ingress(…)` 这条中台路，见 `webhook_channel_qq.go:91`）。
- ⇒ 摘掉 `channel != ChannelQQ` 这半句，**现有用例看不到**（下面一条是判据，不是推断），而现场后果是每条 QQ 消息两条 AI 回复
  —— 与本审计批A / #6 / #13 在 TG、WhatsApp 上处理的是同一类事故。同族的守卫在别家都有用例：
  WhatsApp 三条（`webhook_channel_whatsapp_ai_owned_test.go:18 TestDispatchWhatsApp_IngressDoesNotTriggerAI`、
  `inbox_ingress_channel_owned_ai_test.go:16/62` 正反一对），TG 的群消息分支也有（`tgLeadOutreachAllowed` 在测试里 3 处命中）；
  **唯独 QQ 这一处没有**，唯一"看起来钉着它"的用例调的正是那条死路。
- **判据（为什么断言"看不到"）**：全仓**只有一处**用例真正调进 `handleJob` —— `channel_fullchain_e2e_test.go:922` 那一张表，
  而它每个 case 的账号都写 `AIAgentEnabled: false` ⇒ `shouldTriggerAI`（`webhook_ai.go:61`）在进 `if triggerAI && channel != ChannelQQ`
  **之前**就返回 false，整个分支在 e2e 里从未被执行；它的 verify 也只数 hub 行，不数 AI 触发。
  另一条可能到达的路径是恢复扫描器（`webhook_recovery.go:60` 把 `handleJob` 装进 `handleFn`），
  但它的用例（`webhook_recovery_test.go:141,166`）把 `handleFn` 整个换成替身 ⇒ 同样碰不到这一句。
  （诚实口径：这是"按调用图与前置条件证明该行不可观察"，不是"摘掉半句跑全量仍绿"的实测 —— 后者要等下一次整树电池，见处置②。）

判定：**成立（死代码 + 零断言 + 未覆盖面三合一）**。处置分两步，都不在本批：
① 批I 同族删除 —— `triggerQQSalesEngine` 函数、它的零断言用例、`webhook.go:968` 那句指代它的注释一起清掉；
② 删之前先补一条打在**真路径**上的守卫用例：以 `handleJob` 为入口、`salesEngine` 注入替身，
断言 `channel == ChannelQQ` 时替身零调用、其余渠道恰好一次；反向测试＝临时摘掉 `channel != ChannelQQ` 半句，必须看到该用例红。
`webhook.go` 是 §17.5 电池四份 BASE 之一，**动它等于整块电池作废重跑**，所以这条用例排进下一次整树重跑的那一批一起做，
本批只登记不处置。



### 17.8 另一线报来的两条"本线漏判"：对着树逐条核完，一新一旧各半（2026-09-20 收口）

§15.8 那条线（TG/门禁线）在 20:2x 通过会话间消息递来两条"你们二次检查没抓住的缺口"，并说结果"已原样写进 §15.8"。
本线的纪律是**别人递来的结论先进树核，再进文档**（这正是 §17 整节在做的事），所以逐条对活树取证：

| 递来的说法 | 树上实测 | 判定 |
| --- | --- | --- |
| `buildContentKey()` 造出来之后没有任何调用方 ⇒ 同内容重投照样进管线 | 全仓 `grep -rn buildContentKey` **只命中本文件**（1053/1055 行，即我自己刚写进去的那两行）；仓库里**没有这个函数**。真实实现叫 `ContentHashWithSender`（`webhook_dedup.go:131`），生产调用点 **3 处**：`inbox_ingress_persist.go:76`、`inbox_ingress_ingest.go:132`、`webhook_outbound.go:684`；内容窗口去重确实接在认领路径上（`inbox_ingress_ingest.go:131-137` 的 `SetNX`+`IsDup`），且带 `chanMsgID == ""` 守卫 —— 那守卫就是批F-2 §12 为 N-14 落的修复 | **不成立**（函数名不存在、调用方存在、守卫也在） |
| "同 `msg_id` 重复投必须被拦"这一条断言缺失 | 拦它的机制有**三层**且各有用例：① 事件级 `isDuplicate`（`webhook.go:405`，用例 `webhook_test.go:627-640` 正反对打，含 `eventID==""` 放行那一格）；② 落库级唯一索引 `(platform, msg_id, conversation_id)` + `isDuplicateKey`（`inbox_ingress_ingest.go:130,142`，注释逐字写明这是带 ID 渠道的兜底）；③ 渠道级重投用例**早就在门里**：`webhook_batchc_n08_wecom_xml_test.go:145-199` 把同一官方 `MsgId` 投两次（`:152-155` 故意让两次密文不同 ⇒ 整包哈希兜不住），`:183` 判不得产出第二行、`:192` 判 hub 收敛成 1 行，且 `:156-158` 自带"夹具失效即假绿"的反查 | **不成立**（该断言存在且是既有门的一部分；若对方要的是"每个渠道各铺一条同形用例",那是口径宽度、不是本线漏判） |
| "T108 幂等锁未接发送入口" | `grep -rn "T108"` 在 `user-server/` 全部 `*.go` 与 `docs/` 里**零命中**（唯一命中是我上一版 §17.8 写下的那行）。既没有这个编号的卡，也没有对应实现 | **无法复核**（指向的标识符不存在；若它出自另一仓或某个旧快照，请按 §15.8 的规矩给出树 + 字节） |

**同时核出的一条真的（但比递来的说法小，且早被就地标注）**：`contentHashOf`（`inbox_ingress_ingest.go:153`）
确实是"生产路径未用"的死函数 —— 它自带 `//nolint:unused //// 仅被 *_test.go 引用，生产路径未用`，
职责已被同文件 `:132` 的 `ContentHashWithSender` 取代。这与批K 的 `triggerQQSalesEngine` 同族（见 §17.7 结果 D），
已并入那条待办一起清，**不另开卡**。

**§15.8 那份门禁结果此刻不在文件里**：`grep "878 个 Test\|207.961\|586.858" CHANNEL_INTEGRATION_AUDIT_2026-09.md` 零命中，
§15.8 的正文仍是本轮开始前的版本。近 40 分钟内也没有任何 dedup/发送相关 `*_test.go` 被写入
（`find -mmin -40` 只有 `repository/bridge_offline_replay_repo_test.go`、`service/bridge_offline_replay_test.go`、
`service/order_draft_store_test.go`、`service/opportunity_test.go`、`browser_automation/service/ledger_b16c_test.go` 五份，均属其它泳道）。
⇒ 那三道门的 rc/elapsed 由**本线自己的跑批器**重新取证（见下），不引用未落地的转述。

**字节对齐的交叉证据（这条成立，且是两份独立测量）**：§17.5 电池认的四份 BASE，本轮收口时在活树里重算 md5，
逐一**等于**电池当时的值（`controller/webhook.go 71fbd82f…`、`service/douyin_media.go 5618f208…`、
`service/webhook.go 3cb01f02…`、`service/webhook_channel_douyin.go 8b9cc43f…`）。
⇒ "跑门/引用认的字节＝活树现在的字节"当场可证。
快照（全部 107 份未提交源文件，逐文件字节数 + md5 + 口令粗筛结果）落在 git 之外的
`.tmp_files/dirty-snapshot-2026-09-20/`，四份 BASE 的对齐结论由该清单的末节机械复算，防的是已发生过一次的
"未提交批次被并行会话静默还原"（本会话的 `douyin_media.go` 至今是 `??` 未跟踪态，正是风险最高的一类）。

**方法论收获（进 §17 的口径，不进代码）**：二次检查最容易漏的不是"实现里的分支没被踩"，而是
**"递来的说法被当成已核事实抄进文档"**。上一版本节就是这么写的，抄完才发现三句里两句的标识符在树里不存在。
判据固化成一条：**凡另一线/另一次会话递来的"缺陷"，落文档前必须给出可复制的取证命令（`grep -rn <名>` + 命中数）
与它声称所在的树；命不中就记"无法复核"，不记"成立"。**

### 17.9 门禁剩下的三条红：一条产品缺陷 + 两条测试侧陈旧（2026-09-20，批L / 批M）

§17.5 回填的无过滤门禁跑完，`./internal/controller/` 只剩三条红。逐条追到底，结论不是三条都改代码 ——
**判"红"必须连"该改测试还是该改实现"一起判**，否则会为了凑绿把实现改回契约错误的那一侧。

| 红 | 判定 | 取证 | 处置 |
|---|---|---|---|
| `upload_test.go:510 docx（内容是 ZIP magic）应被放行，实际 code=400「文件类型与内容不匹配，可能为伪造文件」` | **产品缺陷：真实用户上传同一个 .docx 会随机被拒** | `fileMagicNumbers` 把同一段 `50 4B 03 04` 挂在 `.zip/.docx/.xlsx/.pptx` 四个键上（`.jpg/.jpeg` 亦重复），而 `detectFileTypeByMagicNumber` 是 `for range map` 取首个命中 ⇒ 检成哪个键随遍历顺序变。闸门两侧都救不了：`extensionsMatch` 只认 jpg↔jpeg，`isZipBasedFormat` 只认「检成 `.zip` 且声称 Office」⇒ 检成兄弟成员时直接 400。`-count=50` 实测同一条 docx 忽绿忽红 | 已修（`controller/upload.go`）：magic 表收敛成「一段前缀只有一个规范键」——删 `.docx/.xlsx/.pptx/.jpeg` 四条，zip 家族统一检成 `.zip` 交给既有 `isZipBasedFormat` 放行；同时删掉 `:223-225` 的 `if ext == ".docx" \|\| … { return ext }` 死分支（它与下一行 `return ext` 等价，是当初为绕歧义打的补丁，也正是歧义存在过的物证） |
| `webhook_qq_fullchain_test.go:413 passive reply should carry msg_id=qq_m-full-1, got "m-full-1"` | **测试侧陈旧，实现是对的** | 官方《发送消息》参数表原文：「msg_id \| string \| 否 \| 被动回复的消息 ID。**从 GROUP_AT_MESSAGE_CREATE 等事件的 d.id 获取，5 分钟内有效**」。出处 `https://bot.q.qq.com/wiki/develop/api-v2/autogen/api/v2_groups_group_openid_messages.post.html`（curl 直连 http=200、**37,900 B**、`url_effective` 未被改写；副本 `.tmp_files/mut-evidence-2026-09-20/batchM/qq_v2_groups_group_openid_messages.post_37900B.html`，md5 `b4880e41cd3dbb98955f8a4bed31d985`）。`qq_` 前缀是 `Event.HubMsgID` 为中台幂等键加的（`qq.go:604`），把它发到线上等于自造一个「msg_id 无效或越权」 | 改断言为官方 d.id 原值 `m-full-1`，注释写清"带前缀即非法"的因由；同文件 `:385` 继续钉 hub 侧带前缀的键，两侧各归各，不许互相顶替 |
| `wechat_batchf4_m01_inbound_test.go:136 M-01(shortvideo)：hub.msg_type = "video"，want "shortvideo"` | **测试与仓内词表设计冲突，实现是对的** | `service/message_hub.go:91` `inboundHubMsgTypeAliases` 明写 `"shortvideo": model.MsgTypeVideo // 公众号 / 企微小视频`；`wechat_inbound_media.go:87-90` 进一步写明「中台没有"小视频"这个类型：类型仍归 video（否则 hub 落一个词表外的名字，工作台筛选与统计都筛不到），区分靠正文占位符」；同批 service 层用例 `webhook_batchf4_msgtype_test.go:63,784` 早已按 folding 断言 ⇒ 全仓只有这条 controller 用例是反的 | `wantType` 改 `model.MsgTypeVideo`，`[小视频]` 正文断言原样保留（它才是区分承担者） |

**反向验证（新钉的校验必须自己会红）**：把删掉的四条重复键与死分支按原样写回 `upload.go`，
`-count=30` 立刻 67 行 FAIL —— 其中 `docx … 应被放行，实际 code=400` 复现 4 次，
`zip: 第 1 次检出 ".pptx"，与首次 ".docx" 不一致（magic 表有多键命中）` 复现。
⇒ 新增的 `TestDetectFileTypeByMagicNumber_Deterministic`（同一段字节复检 200 次）确实抓得住这一类，
不是跟着改绿的装饰。还原用 `cp` 备份写回并 md5 比对（`1eaffefe8cfd7990ff8d9bf4db35c9e2`），
未对未提交文件动过任何 git 操作；还原后 `-count=50` ⇒ `ok hivemtk-user/internal/controller 1.174s`、0 FAIL。

**顺手清掉的一处假绿**：`TestDetectFileTypeByMagicNumber` 原来把 `.jpg`、`.zip` 两行写成 `"a|b|c"` 外加
「命中任一即通过」的容差分支，等于把「表里多键命中同一前缀」这个缺陷本身固化成永远绿 ——
**容差型断言（want one of / 多值接受）就是假绿形态**，规范名唯一就钉唯一。容差分支连同那两行一起删。

**范围修正（不得按我上一轮的口径复述）**：批L 立项标题写的是「.docx/.xlsx/.pptx 随机 400」。
实测只有 `.docx` 可达 —— `.xlsx/.pptx` 不在 `allowedExtensions`（只有 image 与 pdf/doc/docx）里，
`isValidExtension` 先一步回「不支持的文件类型：.xlsx」，根本走不到 magic 闸门（我为它们补的放行用例因此
断错了产品契约，已删）。magic 表里那三条键位**从一开始就是不可达死项**，这也解释了当初为什么会顺手加重复键：
加键的人没打算让 xlsx/pptx 上传，只是给"看起来该认的扩展名"补了个名目。

