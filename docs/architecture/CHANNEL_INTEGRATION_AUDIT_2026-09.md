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
| 入站字段 | `msgtype/text.content/senderStaffId/senderId/conversationId/conversationType(1单2群)/atUsers/isAdmin/createAt/richText` | ⚠ 一半已补、一半仍在：**已承载**（批F-4c）`msgtype` 原样进归一层（`dingTalkInboundBody` `dingtalk_app.go:289` 起，richText 逐项取 downloadCode）、`createAt` 按毫秒转时间戳且数字两种写法都收（`dingTalkFlexInt` :210、`dingTalkMessageTime` :332）、`senderStaffId‖senderId‖conversationId` 三级兜底（:144-150）。**仍完全没读**：~~`conversationType`、`atUsers`、`isAdmin`~~ → **批H 已补读** `conversationType`/`conversationTitle`/`senderNick`/`isAdmin`/`isInAtList`（`atUsers` 经官方复核确认无消费者，刻意不读，§21.1）⇒ 群/单聊自此分得清、管理员身份可存档；钉钉的「@机器人判定」在官方协议里由平台代做，中台无需重复判定（N-22，见 §5、§21） |
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

> 编号说明：`N-18` 是全仓零命中的空号（`git log --all -S 'N-18'` 对该文件零命中，即首版提交 7329590d 里 N-17 就直接跳到 N-19）⇒ 不是本轮删除，是**从未落档**。后续新增一律取当前最大号 +1（现到 N-34），不回填此号，以免和某个丢失的原始条目撞号。

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
| M-01 | TG/QQ/钉钉/公众号（企微的嵌套取值已随 N-09 处置） | 入站媒体被丢弃：解析层根本没有媒体字段，归一层也没有承载位，四类渠道各自把 MsgType 写死成 text | 逐渠道复核完毕（原引用 `telegram.go:735-737` 路径不完整，现按当前行号给出）：<br>**TG** `internal/channelbot/telegram/telegram.go:653-665` 的 `TGMessage` 只有 `Text`/`Caption`，photo/video/voice/audio/document/sticker **一个字段都没有**；`ToInbound` 取 `Text‖Caption` 且 `MsgType:"text"` 写死（:735-737、:764）；归一层 `core.InboundMessage`（`core/core.go:161-174`）无媒体字段；全仓无 `getFile` 调用 ⇒ 即便解析出来也没有下载能力<br>**QQ** `channelbot/qq/qq.go:448-474` 两个分支都 `Content: d.Content` + `MsgType:"text"`；`webhook_channel_qq.go:108` 再写死一次 `model.MsgTypeText`，且 `:114-116` 把空正文兜成 `"[qq]"` ⇒ 纯图片消息落成一行 `[qq]`<br>**钉钉** `dingtalk_app.go:135-152` 的结构体只声明 `content.content` 与 `text.content`，图片/语音/视频/富文本携带的 `downloadCode` 无处落，`:156-160` 空正文兜成 `"[" + msgtype + "]"`，`:176` MsgType 写死 text<br>**公众号** `service/wechat.go:124-126` **解析得出** `MediaID`/`PicURL`/`Format`，`controller/wechat.go:261-276` 构造 `MessageEvent` 时只带 `Content`，三个字段全丢（`model.MessageEvent` 本有 `MediaURL`，是调用方没填）。批F-4 排查中扩到**六渠道**：飞书（`[media]`/`[post]` 越词表 + 富文本正文蒸发）、企微（`voice`/`shortvideo`/`mixed` 越词表 → **整条消息被拒**）与"半截文件当完整转存"（六处入站媒体读取缺超限拒收）一并归入本条，归一层口径见 N-17 | P2 | **已修（批F-4a/b/c/d/e/f）**：TG 见 §14（九个容器 + `getFile`/下载 + 账号级幂等键 + 36 条变异电池），钉钉/QQ/飞书/企微/公众号见 §15（逐渠道落点 × 用例 + M1–M4 反证）。残留：入站 `sent_at` 只有钉钉跟了官方时间戳（N-21）。~~钉钉 `conversationType`/`atUsers`/`isAdmin` 仍未读（N-22）~~ → N-22 已由批H 收口（§21） |
| M-02 | WhatsApp | 一次推送含多条 message 时，**只有首条驱动 AI 回复**（其余各条照常落 `message_hub`/收件箱/线索，但返回给 `handleJob` 的是 `firstHub`，而 `handleJob` 用 `payload.Content` 当 `SalesRequest.UserMessage`） | `webhook_channel_whatsapp.go` 的 `if firstHub == nil` 段 坐实（原证据 :46-66 是已删除的乱序缓冲入站块，见 N-04）；红测复现：`--- FAIL: TestM02_WhatsAppMultiMessageBatchFeedsEveryMessageToAI … 驱动 AI 的 payload.Content = "多少钱-m02-…"，want "多少钱-m02-…\n有现货吗-m02-…\n能开发票吗-m02-…"（同推送内的后几条没进推理输入）` | P1 | **已修（批F-3）**：dispatch 循环把每条正文累进 `contents`，末尾 `p.Content = strings.Join(contents, "\n")`——口径与中台批量入口一致（N 条合成一份输入、一次回复），避免"一次推送三条问题只答最后一条"或"抢答首条"；`firstHub` 仍作为 hub 返回值供媒体回填与幂等键使用。证据与变异电池见 §13 |
| M-03 | 全部非 TG | 仅 Telegram 在客户端内做 429/退避，其余靠上层重试通道 | §3 各行 | P2 | 待排（与 N-11 一并处置） |
| N-10 | WhatsApp | 媒体转存的**回填键用错**：`persistWhatsAppMediaAsync` 把 `media_id` 当成消息 ID 传给 `EnrichHubMediaURLByMsgID`，而 hub 行的 `msg_id` 是 **wamid**（`webhook_channel_whatsapp.go:102` `MsgID: msg.ID`），函数内的兜底 `UpdateMediaURLByMsgID` 同样按 `msg_id` 查 ⇒ 主路径与兜底两条都命中 0 行，长期 URL 算好了却永远落不回那行，`media_url` 停在占位（企微/飞书两处传的都是真 msg_id，只有 WA 传错）。连带：入口只调 `MediaRef()`（只返回**第一条**媒体消息），一条推送里多条媒体时其余根本不转存 | 读码坐实（三处文件:行号 + 签名第 5 参形名为 `msgID`，`channel_media.go:289`）；红测复现待批F 首跑（先证明"传 media_id 时查不到行、传 wamid 时回填成功"） | P1 | **已修（批F-2）**：`WAMessageRef` 带上本条 `MsgID`（= wamid = hub 行的 `msg_id`），由 `MediaByMsgID()` 一次性建索引，转存任务在 dispatch 循环内**逐条媒体**各起一次、回填键用该行的 wamid；旧的 `MediaRef()`（只回第一条媒体）删除。连带 N-10b：`media_id`/`mime_type`/`filename` 随入站事件落进 `message_hub.Extra`（media_id 只有 7 天有效，转存成败都要留痕，AI 侧理解「[图片]」背后的原件也靠这几个字段）。证据与变异电池见 §12 |
| N-11 | 全部出站渠道 | **渠道给出的限流信号在归一层被逐段丢掉**（四处独立环节，全部由读码 + 逐行核对本仓真实产出的错误串坐实）：<br>① **退避秒数取不到**：`reRetryAfter = retry_after[ ":]+(\d+)` 的字符类没有 `=`，而本仓**所有**产出点用的都是 `retry_after=` —— `channelbot/telegram/telegram.go:174`（`tg send 429 (rate limited, retry_after=%ds)`）、`:453`（`tg %s 429 (retry_after=%ds)`）、`service/whatsapp_tier.go:223`（`retry_after=%s` 是 Go Duration，形如 `1m0s`，即使补了 `=` 也不是"数字秒"）⇒ 解析恒不命中。<br>&nbsp;&nbsp;&nbsp;&nbsp;**本轮红测校正 ① 的口径（不要照抄上面那句当结论）**：Telegram 那两条串的等待值改前**取得到**——因为 Raw 原样回显了响应体，体内的 `"retry_after":31`（JSON 冒号形态）恰好落在旧字符类里。真正恒不命中的只有 `service/whatsapp_tier.go:223` 的 Duration 形态，以及任何"只有 `=` 形态、没有响应体回显"的串。⇒ 旧实现在 TG 上是**靠回显侥幸取到、不是按设计取到**；这条侥幸有边界（响应体为空/被截断/换成非 JSON 错误体即失效）。夹具本身也差点因此假绿：`TestN11_TGRetryAfterEqualsFormKeepsDelay` 初版直接抄 TG 全串，改前也绿；拆成「`=` 单形态」+「TG 全串」两条后才露出差异（见 §9.3）。<br>② **状态码取不到**：`reHTTPStatus` 要求字面 `status` + 恰好 3 位数字，于是 `:453` 的裸 `429`、`feishu.go:270` 的 `feishu api code 99991400`（8 位业务码）、`wechat.go:302` 的 `wechat send error: 45009 …`、`webhook_outbound.go:632` 的 `dingtalk sessionWebhook errcode=%d`、`qq.go:242` 的 `code=%d` 全都不在判据里 ⇒ 是否落 `rate_limited` 只取决于响应体里是否恰好带 `"Too Many Requests"` 文案，是字符串巧合而非结构判据。<br>③ **渠道码被丢弃**：`wecom.go:525-527` `if result.ErrCode != 0 { return "", errors.New(result.ErrMsg) }` —— 45009 这类码值根本没进错误串；`feishu.go:206-208` 把拉 token 的真实错误只写日志、对外返回常量 `"get feishu access token failed"` ⇒ 授权类失败落进 `default:` 变成 `CategoryUnknown + Retryable:true`（该 fail-fast 的被无限重试）。<br>④ **拿到了也没人用**：`retryDelaysFor` 全仓唯一非测试调用点是 `webhook_ai.go:31`（补触发路径），出站重试另有自己那张常量表 `sendRetryBackoffs = {60s, 2m, 4m}`（`webhook_outbound.go:134-138`），`enqueueSendRetry` 只看 `ce.Retryable` 布尔、从不读 `ce.RetryAfter`（`:162-180`）⇒ **`ChannelError.RetryAfter` 对出站时序是死字段**，光改正则不接入等于没改 | 逐条核对本仓真实产出形态（上列 file:line 全部实读原文）；既有 `channel_error_test.go:24` 的夹具是 `status 429: {"…retry_after":31}`（JSON 冒号形态，恰好匹配），从未覆盖任何一条真实产出串 ⇒ 又一个"夹具与实现互相漏项"。红测待批F | P1（限流期按固定梯度反复早退避 → 二次触发封禁；授权失败被无限重试） | **已修（批F-1）**：新增 `internal/channelbot/core.APIError`（只装事实：Channel/StatusCode/Code/RetryAfter/Raw，由各客户端在解析响应的当场构造；放这层是因为 `channelbot` 不能反向依赖 `service`，而那三个事实只在解析现场存在）；`service.ChannelError` 增 `Channel`/`Code` 两个事实位与 `quota_exhausted`/`window_expired` 两个类别；判据顺序＝**渠道业务码表（按渠道分表，45009 同号不同义）> HTTP 状态码 > 文案**，文本分类降为兜底；`nextSendRetryAt` 接上 `ce.RetryAfter`，口径"只顺延不提前"；飞书读 `x-ogw-ratelimit-reset`；TG（含 Markdown 兜底分支）/QQ/WA 客户端与企微/公众号/飞书/pacing 构造点共 7 处一律带出码值。证据与变异电池见 §11、§10「批F-1 执行时对本表的两处偏离」 |
| N-12 | 微信公众号 | **安全模式（消息加解密）整条链路不存在**：账号侧把 `encoding_aes_key` 收下来并写库（`controller/wechat.go:68,84,115` → `repository/wechat_account_repo.go:79`、`model/wechat_account.go:15`），运行侧却**全仓零读取**（`grep -rn EncodingAESKey internal/ cmd/` 只命中"存入"这三处，没有任何解密点），入站一律按明文 XML 解（`service/wechat.go:142` `xml.Unmarshal`）。商家一旦在公众号后台启用安全模式：外层 `<xml><ToUserName>gh_x</ToUserName><Encrypt>…` 解出来 `MsgType`/`Content` 全空，`controller/wechat.go:261-276` 照样把这条**空消息**送进 Ingress ⇒ 表现为「配置成功、验签通过、200 OK，但一条客户消息都收不到」，与 N-08 在企微侧的形态同源 | 读码坐实（配置面与运行面的差集由上述 grep 给出）；红测待批F：造官方加密外壳（AESKey=Base64(EncodingAESKey+"=")、appid 作 receiveid，与 §3.4 企微同一算法）证明当前解出空 `MsgType` | P0（启用即整渠道静默失联） | 待排 |
| N-13 | 钉钉 | 出站**三条"根本没发"的分支返回的是 `sendErr == nil`**：`webhook_outbound.go:579-584`（缺 sessionWebhook）、`:589-594`（已过期）、`:596-601`（域名非法）在 500 行的大函数 `sendOutbound(...)(sent bool, sendErr error)` 里都是**裸 `return`** ⇒ `sent=false` 且 `sendErr=nil`，`:372-375` 的 `markSendFailed` 没走、`:381-385` 的 defer 条件 `sendErr != nil && !sent` 不成立 ⇒ 既不进持久化重试队列、也不写失败轨迹，客户侧最后一条永远停在入站行，只有服务端一条日志。这正是 C-05（批A 已修的"吞掉传输错误与 errcode"）剩下的那半边：**不是吞掉错误，是从未构造错误** | 读码坐实（三处 `return` 与 defer 条件同文件对照，函数签名 :348 具名返回）；红测待批F：断言这三条分支后 hub 行必须带失败轨迹、且不得静默 `sent=false,nil` | P1（客户收不到回复且系统内无痕） | **已修（批F-6，2026-09-21）**：口径从"钉钉三条"扩到**全表 15 条前置不满足分支**，一律经 `preSendFailure`/`markSendFailed` 回传**不可重试的 `*ChannelError`**，并由 `outboundSendFailed`→`pushUndeliveredReplyTrace`→`MessageHubService.PushSendFailureTrace` 补一行 `status=send_failed` 出站轨迹（只写库不推送，见 §22.1）。13 条腿 + 23 格电池（21 杀 / 2 等价，M04 首轮存活已补 L13 闭合）见 §22.4、§22.5。<br>⚠ 三条**连带登记**：① 就地改状态那一支（`channel_error.go:374`）**今日从生产不可达**，三个调用点传的全是入站行或合成行（§22.2）；② 桥接 `persisted.ID == 0` 的 `Retryable:true` 实为判档器 `default` **碰巧一致**，未钉桩（§22.3）；③ 真正的残留缺口是**非桥接七渠道成功投递也不写出站 hub 行** ⇒ 库里只有反证没有正证（§22.2 末，属出站链路改造，未夹带进本批） |
| N-14 | WhatsApp/钉钉（TG/QQ 同源） | 入站**内容窗口去重**（`interceptInbound` 的 `duplicate(channel+sender+content) within window`，TTL 5 分钟）只看"同渠道+同发送人+同正文"，**完全不看平台给的消息 ID** ⇒ 同一客户五分钟内连发两条相同文本、或 Meta 一次推两条图片（占位正文同为 `[图片]`）时，第二条在写 `message_hub` 之前就被拦掉：既不入库、也不转存媒体、也不进收件箱。平台 at-least-once 重投本来由 `message_hub` 的 `(platform, msg_id, conversation_id)` 唯一索引 + `isDuplicateKey` 幂等兜底，内容窗口去重只对"没有稳定消息 ID"的事件才有独立价值。钉钉侧另有一半原因：入站事件根本没带 `channel_msg_id` | 红测实跑坐实（两处现场，均在批F-2 写媒体用例时跑出）：`--- FAIL: TestN14_RepeatedContentWithDistinctWAMIDsIsNotDropped … msg_id=wamid-n14-2 没入库（record not found）`、`--- FAIL: TestN14_DingTalkRepeatedTextIsNotDropped … 实际 1 行`，配套服务端日志 `dup=true event_id=wamid-n14-2 reason="duplicate(channel+sender+content) within window"`；钉钉那条 `event_id=dt-1-m-n14-2` 说明它的 EventID 本来就带 msgId，只是没往 Extra 里放 | P1（真实客户消息静默丢失，且现象随内容重合而随机复现） | **已修（批F-2）**：`interceptInbound` 的内容窗口去重加守卫 `chanMsgID == ""`（带稳定渠道消息 ID 的事件不进入内容窗口），`channelMsgIDOf` 的取值上移为函数内单一变量供两处复用；钉钉入站事件补 `"channel_msg_id": msg.MsgID`。反向边界同样钉住：无 ID 事件（含 `wa-out-`/`tg-out-` 占位 ID）仍按内容去重，防止重投双份入库。证据与变异电池见 §12 |
| N-15 | TG | 下载 URL 本身带 bot token（官方 `https://api.telegram.org/file/bot<token>/<file_path>`），而 `*http.Client.Do` 的传输层错误是 `*url.Error`，`Error()` **原样回显 URL** ⇒ 一旦下载腿抖动，bot token 就进错误日志、进而进工单/告警。同类风险在所有"凭据置于 URL 路径"的渠道都成立，TG 是当前唯一真会拼出这种 URL 的 | 读码坐实（官方原文见 §14.1：`file/bot<token>/<file_path>`）；红测 `TestM01_TelegramTokenNeverInError`（关端口，`GetFile`/`DownloadFile`/`SendMessage` 三条腿逐个断言不含 token 且 `errors.As(syscall.Errno)` 仍成立）改前实跑红 | P1（凭据泄漏，且泄漏面随日志采集外扩） | **已修（批F-4b）**：`Client.scrubToken`（`channelbot/telegram/telegram.go:70-84`）只在确实含 token 时套 `redactedError` 壳、`Unwrap` 保留底层错误（否则出站归类断链，把限流/抖动误判成不可重试）；不含 token 与 nil 原样返回（`TestM01_TelegramScrubKeepsUntokenedErrorIntact`）。见 §14.3 |
| N-16 | TG | 入站幂等键 `tg_upd_<update_id>` **不带 account_id**：`update_id` 只保证**单个 bot 内**单调，两个 bot 各自数到同一 `update_id` 是常态（库里现有 2 个 telegram 账号）。落 `message_hub` 时撞 `(platform, msg_id, conversation_id)` 唯一键 ⇒ 第二个 bot 的这条客户消息被当成重复**永久丢弃**，且表现为"什么都没发生" | 读码坐实；红测 `TestM01_TelegramSameUpdateIDAcrossAccounts`（同 `update_id` 两账号，改前第二行 `record not found`）+ 变异 T8「幂等键不带 accountID」caught=15 | P1（多 bot 部署下真实消息静默丢失） | **已修（批F-4b）**：`Update.HubMsgID(accountID)` 把账号并入键；`TestM01_TelegramHubMsgIDPrecedence` 8 子例钉住取值优先级，`TestM01_TelegramBackfillScopedToAccount` 钉住回填同样按账号收窄。见 §14.3 |
| N-17 | 企微/飞书/WhatsApp/公众号/TG/钉钉 | **渠道官方类型名 ≠ 中台 `msg_type` 词表**，三种失效形态各不相同：① 企微把官方名直接喂 `model.MessageHub` 校验 ⇒ `voice`/`shortvideo`/`mixed` **整条消息被拒**（客户发了消息，系统里一行都没有）；② WhatsApp/公众号的 `[图片]` 类占位落到词表外；③ 飞书 `post` 富文本连正文一起蒸发 | 逐渠道读码 + 红测（`webhook_batchf4_msgtype_test.go` 的 N17 组：映射表 :53、未知类型不拒收 :108、别名表自洽 :124、超限拒收 :152、源码闸 :215/:258、飞书 post :349/:386）。第一现场红字：`以下 hub 类型字面量越出中台词表…[webhook_channel_telegram.go:200 → "message"]`。闸的反证见 §15.5 M1–M4（钉钉 D1–D12、企微 W1–W9 两条电池**尚未跑过**，状态见 §15.8） | P0（企微：官方类型一到就整条丢）/ P1（其余） | **已修（批F-4d/e/f）**：单一归一层 `InboundHubMsgType` + `inboundHubMsgTypeAliases`（`service/message_hub.go:84-127`），兜底**刻意不对称**（官方名越表→退回 text 保正文；占位符越表→保留官方名，可观测）；每渠道源码闸 + 性质测试。见 §15.1、§15.2 |
| N-19 | 中台（全渠道共用） | 补触发判据 `HasUnrepliedCustomerMessage` 的 WHERE **只有 `conversation_id`**，不带 `platform`/`account_id`，而 `ORDER BY sent_at DESC` ⇒ 同号会话（固定夹具、或真实的跨渠道 id 碰撞）会把**别的账号、别的渠道、别的历次运行**的行算进同一段会话 | 全量门禁实跑坐实（不是推断）：`钩子3：最后一条 inbound 超过 5 分钟…conv_id=cid-77 event_id=dt-1-m-77` —— `cid-77` 那次运行里根本没有新消息，被判"未回"的是历史行 | P1（跨账号判据串味：漏触发或误触发 AI） | **待排**：改键要同时评估既有依赖（同一用户在多渠道会话 id 恰好同号时的查询语义），不在批F-4 范围内动 |
| N-20 | 测试侧（钉钉承载用例） | `f4DtSetup` 没掐媒体两条腿 ⇒ 5 条带 `downloadCode` 的用例各真发一次 `POST https://api.dingtalk.com/v1.0/oauth2/accessToken`（假凭据），日志回显 5 个真 `requestid` 与 `invalidClientIdOrSecret`。危害不是错，是**慢 + 依赖外网 + 观测噪音**，且与 QQ 承载用例（早已 stub）不一致 | 实跑日志坐实（5 个真 requestid） | P3（测试卫生） | **已修（批F-4c）**：`f4DtSetup` stub `dtMediaFetchFn`/`dtMediaStoreFn` 并 `t.Cleanup` 还原；转存腿仍由 `…_download_test.go` 逐字段断言。见 §15.7 |
| N-21 | WA/TG/飞书/QQ/抖音 | 入站 `sent_at` 仍写 `time.Now()`：客服侧看到的是"处理时刻"而不是"客户发送时刻"，时序、响应时长统计、跨渠道对齐全被污染；重投事件的时序也因此失真。钉钉半场已在批F-4c 修掉（正是那次改动暴露了全量门禁的 3 条时间炸弹红） | 读码坐实：`webhook_channel_whatsapp.go:97`、`webhook_channel_telegram.go:159,202,322`、`webhook_channel_feishu.go:219`、`webhook_channel_qq.go:112`、`webhook_channel_douyin.go:108,166` | P2 | **待排**：逐渠道官方字段与**单位**不同（WA `timestamp` 秒、TG `date` 秒、飞书 `create_time` 纳秒字符串、QQ `timestamp` 对象），必须先逐渠道取 A 档原文再改，不能一把梭。见 §15.7。<br>**取证现状（2026-09-20 复核 `/tmp/chandocs` 全 88 份）**：TG 已有原文（`tg_api.html`（860,075 B ⇒ `https://core.telegram.org/bots/api`，页内 `<title>Telegram Bot API</title>` + 自链 `/bots/api` 佐证）内 `Message.date` 明写 "Date the message was sent in Unix time"=**秒**；佐证 `tg_wh.html`（59,319 B ⇒ `/bots/webhooks`「Marvin's Marvellous Guide to All Things Webhook」）13 处示例 `"date":1441645532` 为 10 位）；抖音 `create_time` 13 位毫秒（`jina_dy_mini_private-msg-webhook.txt:64` 明写"13位毫秒时间戳"）；钉钉 `timestamp` 毫秒（`jina_dt_recv.txt:157`，已随批F-4c 落）。**缺原文**：WA（`jina_wa_wh.txt`/`jina_wa2.txt` 内 `timestamp` 零命中）、QQ（`qq_msg.html` 同零命中）、飞书入站事件（缓存里只有出站 `message_create`）⇒ 这三家的时间戳字段与单位**未取证**，批N-21 开工前必须补取，不许按"惯例是秒/纳秒"推断。<br>**本轮补取失败实录（不得当作已取证）**：对 Meta 开发者站的一次抓取返回了 `timestamp`=Unix 秒 + 样例 `"1749416383"`，但**实际取回内容的地址是被改写成带签名的 OSS 代理链接**（`routify-file-proxy-sg.oss-ap-southeast-1.aliyuncs.com/…?Expires=…&Signature=…`），与请求的官方域不是同一来源，且回文里没有逐字引文与页面自证 ⇒ 出处不可核验，按 §6 规矩**拒用**；对官方域重试则回 `403 / code 10605`（服务排队）。结论：WA 入站时间戳仍是未证项。<br>**同一改写第二次复现（2026-09-20，QQ 侧）**：为取 `getAppAccessToken` 的响应字段类型抓 `bot.q.qq.com/wiki/…/interface-framework/api-use.html`，工具回显的实际取回地址同样是被改写的带签名 OSS 代理链接（`routify-file-proxy-sg.oss-ap-southeast-1.aliyuncs.com/…?Expires=…&Signature=…`）；且**对同一 URL 连抓两次内容互不相容**（一次给出 `{"access_token":…,"expires_in":"7200"}` 成功体，一次称端点为 `https://api.bot.qq.com/app/getAppAccessToken` 而仓库生产用的是 `https://bots.qq.com/…`）⇒ 两次全部拒用，QQ 的 `code` 字段类型与 token 端点域名都记为未证项。由此暴露的仓内自相矛盾按批J 登记、不按推断改：`qq.go:74` 声明 `Code int`，而既有测试夹具写 `"code":"100014"`（字符串）——已实测该夹具走的是 **parse 分支**而不是它名字声称的 token 失败分支，详见 §17.7<br>**【批J 闭合（2026-09-20）】** 该页后来经 curl 直连取到且 `url_effective` 未被改写（24,598 B，md5 `f1acdd7dc218e41635259d0fa7fafea0`）：`code` = **number**、端点 = `https://api.bot.qq.com/app/getAppAccessToken`、业务失败仍回 200、且官方明写「不要依据 `message` 判定错误类型」⇒ 本行两项 QQ 未证结掉，随批J 落进码表与用例（§18.1–18.3）。**本行剩下的未证项只有 WA 与飞书入站时间戳。** |
| N-22 | 钉钉 | 入站 `conversationType`（官方 1=单聊 2=群聊）、`atUsers`、`isAdmin` **三个字段全仓零读取** ⇒ 群/单聊不分（ outbound 侧只能靠 `sessionWebhook` 蒙）、@机器人判定缺失（群里误回/漏回）、管理员身份丢失 | 读码坐实：`grep -rn "conversationType\|atUsers\|isAdmin" internal/` 在钉钉入站链路零命中（仅有的命中在 `repository/scope/tenant.go` 与 `content/controller/marketing_flow.go`，与钉钉无关）；§3.5 入站字段行同步更新 | P2（群聊场景语义错，但需真机群聊才暴露） | **已修（批H，2026-09-21）**：`conversationType`→`IsGroup`/`GroupID`、`conversationTitle`→`Extra.group_name`、`senderNick`→`SenderName`、`isAdmin`/`isInAtList` 三态存档，见 §21.2。<br>⚠ **本条的半个前提被官方推翻**：「@机器人判定缺失 ⇒ 群里误回/漏回」**不成立** —— 钉钉原文「当用户@群机器人或与机器人发送单聊消息时，钉钉会把机器人接收到的消息发送到开发者设置的机器人回调服务」⇒ 能进回调的群消息必然已 @ 本机器人，中台再判一次是空转（A 档取证与反证刀见 §21.1、§21.5）。`atUsers` 因此**刻意不读**（它是"被@的人"，机器人自身在 `chatbotUserId`，读了也没有消费者），旧夹具把这两者写反了，一并纠正（§21.3） |
| N-23 | 门禁侧（`config_param_guard_test.go` D12 规则） | 架构闸把源码**整文件原文**（含注释）喂给"禁止出现 `system_config_kv`"的判定 ⇒ 别的改动线在注释里**提到**这个表名就把门禁判红。第一现场：D12 红字指向 `internal/service/ltc_config.go`，而该文件的两处命中都在注释里（`git status` 显示文件干净、是已提交内容），真正的新增点根本不存在。危害不是误报本身，是**误报会把人推向"往白名单里加豁免"** —— 那等于给这条闸开个永久的洞 | 实跑坐实 + 反证：用 `go/parser` 剥注释后重跑判定即绿；再在**代码**（非注释）里加一处 `system_config_kv` 字面量，门禁仍判红（反证证明修复没把闸放松） | P2（门禁可信度；误修方向会削弱闸） | **已修（批G 收尾）**：`goCodeOnly()` 用 `go/parser` 把注释替换为等长空格后再匹配，解析失败时**退回原文**（判得更严而不是更松）；原注释式豁免（写 `禁止新增`/`D12` 绕闸）随之失效，无需白名单 **【§23.17 第 12 段订正：该修复在全部提交历史里查无字节（`goCodeOnly` 只以文本形式存在于三笔文档提交），HEAD 版守卫仍按整文件原文匹配、注释式豁免仍在 ⇒ 状态回到"未入库"；同一形状已第二次报红（`quote.go`），修复正被并行泳道重写】** |
| N-24 | 测试侧（钉钉媒体转存用例） | `…_download_test.go` 的 4 条用例**先装自己的替身、再调 `f4DtSetup`**，而该 helper（N-20 的修复产物）会把 `dtMediaFetchFn`/`dtMediaStoreFn` 换成"永不发起真实下载"的替身并登记还原 ⇒ 调用方的替身被静默覆盖：3 条下载断言红，第 4 条"不该下载"的计数用例反而**假绿**（它的 0 是覆盖出来的，不是行为）。N-20 只修了"helper 要掐腿"，没修"掐腿的顺序" | 实跑坐实：`-run 'TestM01_DingTalk'` 7 条里 3 红 1 假绿；把夹具补上 `robotCode` 反向测出「缺 robotCode 却起了 1 次下载，want 0」，证明那条计数断言此前是空的 | P2（假绿比红更贵：它把"没测"伪装成"测过"） | **已修（批G 收尾）**：4 条用例统一改成"先 `f4DtSetup` 再 `g2bSeams` 式装替身"，还原交给 helper 的 `t.Cleanup`；文件头把顺序写成显式约束。`TestG2B_*` 自始按此形状写（`g2bSetup` 明确**不碰**两个替身，见其注释） |
| N-25 | `service/channel_media.go:31-92` | **一整套"渠道媒体按需下载"注册表是死的**：`channelMediaFollower` 类型、`channelMediaFollowers` map、`RegisterChannelMediaFollower`、`fetchChannelMediaFollower`、导出入口 `PersistChannelMedia` 五者自成一圈，全仓（含测试）**没有任何装配期注册、没有任何调用** ⇒ `PersistChannelMedia` 唯一可能的返回值是 `no media follower for channel X`。真正的活路径是各渠道自己的 `*MediaFetchFn` 替身 + `channelMediaPersist`（飞书/企微/WA/TG/QQ/钉钉/公众号/抖音 8 处）。危害不是"多写了 60 行"，是**导出的死 API 会骗后来者**：看见 `PersistChannelMedia(ctx, channel, mediaID)` 以为"延迟取媒体"的能力已就位，接上去拿到一个恒定 error，再往下就有人去补注册表而不是看官方 ID 有效期约束（批G-2b 已证明：抖音/飞书这类 ID 必须**入站当场**换，拖到点开时必失败） | 三段取证（均为只读，未动代码）：<br>① 全仓 `grep`（type=go 与全文件类型各一遍）：`PersistChannelMedia` 仅定义处 1 次、`RegisterChannelMediaFollower` 仅定义处 1 次、`fetchChannelMediaFollower` 仅定义处 + `PersistChannelMedia` 内 1 次、map 只在 `RegisterChannelMediaFollower` 内被写<br>② `git log --all -S`（两个名字各一次）：**只有** `a200aa65`（2026-09-08，单文件 +294 行），提交信息自陈"期间合入同事未提交的渠道媒体转存**半成品**" ⇒ 从未有过调用方，不是后来被删的<br>③ 动态调用面排除：包在 `internal/` 下，`hivemtk-user` 之外的模块**不可能** import；仓内 `MethodByName` 仅 `model/kuaishou_card_test.go` 查 `TableName` 一处，与本案无关<br>另：`scripts/audit-loop/STATE.md:1153` 显示上一轮审计曾把这套 map 当活代码处理（"补 `sync.RWMutex`"），锁加在了死路径上 | P3（无运行时危害；误导性 API + 死锁代码） | **待删（本轮新记，批G-2b 复核 `channelMediaPersist` 调用面时发现）**：删除须与门禁复跑同一窗口（`go build ./...` + 无过滤 `./internal/service/`）。本轮**不删**：该文件上正压着批F-4f 的未提交改动（`git diff HEAD` 18+/5−，即抽出的 `readInboundMedia`），在同一文件上叠一刀删除会让"哪个改动导致哪条红"分不清；且并行线随时可能在补注册（按 mtime 13:24 核对：该文件的最近一次写是本轮 F-4f 自己）。不并入批G-2b 的落点表：它不是抖音链路的组成部分。**【批I 闭合（2026-09-21）**：五件套 + 级联变死的 `detectContentType` 已删（§20.1、§20.2）；<br>删完补的反向探针反而暴露了更大的洞 —— 活路径 `channelMediaPersist → storeInboundMedia` 此前**零用例**（八家渠道的媒体用例全都把 `xxMediaStoreFn` 换成替身），已补五条腿 + 八刀变异（§20.3），并顺手逮到日期目录被拼两次的真缺陷（§20.4）**】**|
| N-26 | `repository/obs_config.go`（`GetDefault` / `SetDefault`）+ `service/obs_config.go`（`SetDefaultConfig` / `UploadFile` 写回） | **"默认存储"这一全站唯一入口的选取面三处不成判据**（本条由 §20.7 的一句话登记补成正表条目）：① `GetDefault` 只看 `is_default`、不看 `status`，选中的那台是什么状态无人问；② 切换默认是"先清全表、再 UPDATE 目标行"**两句、无事务** ⇒ 目标行在此期间被删掉时第二句影响 0 行仍返回 nil，库里留下**零条默认**，此后两条上传口都报"未找到默认存储配置"且**没有任何自愈路径**（默认行没了，管理页也就没有"取消默认"可点；入站媒体那一支不报错，它静默退回本地盘 —— 见 §5 N-32）；③ 上传成功后写用量用的是**整行 Save** ⇒ 写回的是"上传开始时"那份快照，期间的默认切换被陈旧的 `is_default=true` 复活成**两条默认行**，之后选取由 uuid 主键序决定（媒体落到哪台存储变抛硬币，而两边公开 URL 都成立，只有取回原文件时才露馅） | 读码 + 真库取证。**①的原描述要打折**：`UpdateStatus` 全仓零生产调用方、无 DTO/controller 路由，三个 `CreateConfig` 入口一律硬编码 active，真实库 `SELECT status, is_default, count(*) FROM obs_config GROUP BY 1,2` = 仅 1 组 `(active, true)` ⇒ "默认行被停用"今天**做不出来**，①是"缺判据"不是现行故障（但 ① 与"设默认不许停用"必须同批改，否则 ① 立刻变成一键可作的致残态，见批M 的 S1）。②③ 不需要额外前提即可复现。改产码前先以红测取得预测红因（R1/R2 两条，红因与上述描述逐字对得上），再落地修复 | P1（②是"两条上传口全断且无自愈、入站媒体静默改落本地盘"；③是"同一客户文件分散两台存储"；①单看 P3） | **已修（批M，2026-09-21，未提交）**：判据统一成 `is_default AND status = active` 并以 `created_at ASC` 定死取序（多条存量时取最早那台 = 文件实际在的那台）；`SetDefault` 收进单事务、按"先摘别人→后置自己"的顺序（这个顺序被新加的库级索引强制，反序必撞唯一冲突），目标行缺失 ⇒ `gorm.ErrRecordNotFound` 且整体回滚；新增 `IncrementUsage`（`UpdateColumns` 只写 `file_count/total_size` 两列，不整行写回、不刷 `updated_at`），`UploadFile` 改走它；`ClearDefault` 从接口与实现一并删除（单独暴露"只清默认"没有合法用法，它存在的唯一后果就是让人在事务外先清后设）；服务层 `SetDefaultConfig` 补 active 校验；库侧新增 `postMigrateObsDefaultUniqueIndex`（`migrate.go:474-500`，挂在 `:423`），偏唯一索引 `idx_obs_config_single_default` + 存量双默认先清重再建索引，详见 §23 |
| N-27 | `pkg/messageid/identity.go:12-43` | 客户身份归一函数 `NormalizeCustomerIDFromMessageHub` / `NormalizeCustomerNameFromMessageHub` **零生产调用方**：函数注释自称「取代原 unified_inbox/inbox.go 内部 inboxCustomerID 包级函数……供 `inbox_ingress` / `message_hub` / `reconciliation` 等多包复用」，而列名的三个复用方**一个都没接**（三处各自决定客户标识）。危害不是多写 44 行，是**注释里的"多包复用"是既成事实的口吻**——批H 做群面影响面核查时，第一反应就是「改 `IsGroup` 会打到归一层」，实际那条路根本不通；测试越全（8 条用例覆盖 nil/群/前缀/方向/名称回落），越像已经接线 | 三段取证（只读，批H §21.4）：<br>① 全仓 grep：仅定义处 + 同包 `messageid_test.go`，零调用点（含非 Go 文件一起扫）<br>② 动态面排除：`MethodByName` 全仓唯一命中在 `model/kuaishou_card_test.go` 查 `TableName`，与本案无关；仓内只有一个 Go 模块（`user-server/go.mod`），`internal/` 下的包外部模块不可能 import<br>③ `git log --all -S`：裸名两次命中 —— `735d43a3` 加 `identity.go`（+54 行，**同一次提交没有加任何调用方**）、`dcda7ff4`（"审计R46-test-coverage"）只加 `messageid_test.go`（+66 行）；带包名的调用形态 `messageid.NormalizeCustomerIDFromMessageHub` **在全部历史里零命中**；`--diff-filter=D` 该目录从未删过文件 ⇒ 同包内的裸调用也没有过宿主。**结论：从未接线，不是后来被拆走的**（区别于 N-25 的"半成品"叙事） | P3（无运行时危害；误导性注释 + 只有测试在跑的死判据；与 N-25 同类） | **待排（本轮新记，批H 影响面核查时发现）**：只记不删。它与 N-25 的差别在于**判据本身可能是对的**（群聊以客户=会话、单聊以客户=发送人），删掉还是把三家渠道接上去，是"收件箱/线索按群还是按人收敛"的产品决策，不该夹在渠道契约批里顺手做。批H 因此**没有**依赖这两个函数来实现钉钉群面，改用 `IsGroup`/`GroupID`/`Extra.group_name` 直落（§21.2） |
| N-28 | `internal/service/group_silence.go`（整模块） | 群沉默判定与「群复活」信号面**整模块零生产调用方**：`DetectGroupSilence` / `BuildReviveVerdict` / `EmitGroupRevive` 三函数 + `GroupReviveSignal` / `GroupReviveCandidate` / `GroupDeadVerdict` 三常量，全仓 `*.go` `*.md` `*.sh` `*.yaml` `*.json` 里除自身与自己的 `group_silence_test.go` 外**零命中**（含字符串字面量 `group_revive`，因此也排除了"按名从配置/事件订阅里消费"） ⇒ 一条带测试、已进 git 的主动触达能力没有消费者 | 取证即本条偏差段所记的那一遍全文件类型 grep（`*.go` `*.md` `*.sh` `*.yaml` `*.json` 五类一起扫，并把字面量 `group_revive` 一并扫进去，故"按名从配置/事件订阅消费"这一类动态面同时被排除）；来源提交 `8fecb6ad` 见末列，影响面核查过程在 §21.4 第 8 条 | P3（无运行时危害；本批需要它的是**反向**结论：批H 打开钉钉 `IsGroup` **不会**新激活"群沉默 → 复活触达"，正因为无人调它） | **待排（本轮新记，批H 影响面核查时发现，§21.4-8）**：只记不删——来源是 `8fecb6ad feat(v2-step5)…群聊复活`，属第五步能力清单而非渠道契约；接不接、由谁接（cron 还是 SOP 触发）是那条线的产品决策，渠道审计不代做 |
| N-29 | `internal/service/inbox_ingress.go:369`（`NormalizeEvent`，`:381` 只拒空串）+ `inbox_ingress_persist.go:53` | **统一入站口不校验渠道词表**：`POST /api/chat/ingress` 的 `event.Channel` 直接成为 `message_hub.platform`，`NormalizeEvent` 只在 `Channel==""` 时报 `invalid channel (empty)`，`persistMessage` 走 `hubRepo.Create` 绕过 `Normalize` ⇒ 持 `IngressSecretAuth()` 秘钥的一方（浏览器扩展/未来任何接入方）把平台名写错一个字母（`wechat_official`、`weChat`），这行消息**照落库**，但工作台按 `platform = ?` 筛不到、`by_platform` 统计不进 ⇒ 静默的数据质量洞，且与 §5 全篇"词表为准"的口径矛盾 | 读码坐实（批F-6 §22.7 排查 `custom` 是否该进词表时发现）：`grep -rn "ValidPlatform" internal/` 除本包测试外**零生产消费者**，即没有任何一处给 `Channel` 判过档；`MessageEvent.Channel` 的两条写入口（`controller/inbox.go:383`、各渠道 `HandleIngressMessage`）均无白名单 | P3（不丢消息、不致错，但会让行"在库里隐身"；修复方向两难，见处置栏） | **待排（本批只记不修）**：两种修法都有代价 —— ① 在 `NormalizeEvent` 里硬校验：会把未知平台的**真实客户消息整条拒掉**，与批F-4d/e 在 `MsgType` 侧定下的"只归一不校验：校验会把整条消息丢掉"直接冲突；② 只加结构化告警（照 `webhook.go:1024` 的"漂移要出声"先例）：不丢消息，但告警消费方（ops 面板/审计日志）今日不存在，等于先留一条没人读的日志。选②且要选得有意义，得先定"未知平台"是接入方 bug 还是新渠道未登记 —— 那是接入流程决策，不属渠道契约批。本批把 `custom` 留在词表的理由已写清（§22.7），与本条不冲突：词表是"合法值集合"，本条说的是"没人按它校验" |
| N-30 | `internal/content/service/material.go:93-111`、`internal/service/channel_media.go:71-72` | **存储用量计数器只有管理页上传这一条路径会写**：`obs_config.file_count` / `total_size` 的唯一自增入口 `IncrementUsage` 只有一个生产调用方（`internal/service/obs_config.go:304`，管理页上传）。而另外两条**真实往存储写文件**的路径都只 `GetDefault` 取那一台、不回报用量 —— 素材上传（`material.go:96` 取默认 → 直接 `driver.UploadMultipart`，函数体内无计数写回）与渠道入站媒体转存（`channel_media.go:72`，**本选取面真正的大宗消费者**）⇒ 管理页那台存储显示的"已用 N 个文件 / M 字节"只统计管理页自己传的东西，**用得越多偏差越大** | 读码 + 全仓 grep（批M 收 `UploadFile` 写回路径时逐个调用方核出）：`grep -rn "IncrementUsage" --include=*.go` 非测试命中 24 处，其中**签名匹配 obs 那台的**只有 `repository/obs_config.go:29,187`（声明+实现）与 `service/obs_config.go:304`（唯一生产调用）；其余同名词全部是**别的仓储的别的对象**（`script_library`/`material`/`script_template`/`knowledge_merchant`/`objection_handler`），不构成 obs 用量写入。`material.go` 与 `channel_media.go` 两处 `GetDefault` 之后的函数体内 `IncrementUsage` 零命中 | P3（纯统计口径偏差：不影响选取正确性，不影响文件实际落位，只让容量决策读到偏小的数） | **待排（批M 只记不修，详见 §23.5）**：`channel_media.go` 正压着并行泳道的未提交改动（`git status` dirty），同文件叠刀会让"哪条红归谁"分不清 —— 与 N-25"待删"用的是同一条纪律。补法现成：两处各一行 `cfgRepo.IncrementUsage(ctx, cfg.ID, header.Size)`（素材侧 `licenseID` 已被 `_ =` 丢弃，无额外入参缺口），配一条"转存之后计数确实涨了"的腿，S2 的记账替身形状可直接复用。建议与批F-4 的入站媒体面同批做，省一次 `./internal/service/` 门禁 |
| N-31 | `internal/service/obs_config.go:113-154`（`UpdateConfig` 合并段）、`internal/model/obs_config.go:47-48` + `:205-276`（`TestConnection`） | **存储配置编辑口的两个残留（批M 做 ③ 另一半时顺带核出）**：① 合并用 `if req.X != ""` 逐字段判空 ⇒ 12 个可编辑列**全都无法清空**：`Domain`/`Endpoint`/`Config` 一旦填过就只能改成别的值，改不回空串，而"把自定义域名去掉"是真实运维动作；② `last_error` / `last_test_at` 两列**没有任何生产写口** ⇒ `TestConnection` 只把 error 返回给调用方、一次都不落库（model/DTO/两处 struct copy 之外零写点，路由 `POST /obs/config/:id/test` 的整条链无持久化），管理页那两栏恒空、"上一次连接测试为什么失败"不留痕 | 读码 + 全仓 grep：`grep -rn "LastError\|LastTestAt" --include=*.go` 非测试命中里与 obs 相关的只有 `model/obs_config.go:47-48`（列定义）、`dto/obs_config.go:57-58`（出参）、`service/obs_config.go:376-377`（model→DTO）、`:426-427`（DTO→model，仅供 `storage.Factory` 用，`obsConfigResponseToModel` 的四个调用点全在 `TestConnection` 内部且从不写回）⇒ 无写口。①由 `UpdateConfig:119-154` 的合并段直接读出，与本次 `Update` 白名单化无关（白名单只列可写列，合并策略在上层未动） | P3（①是"改得动、清不掉"的运维死角；②是"能力有、留痕无"的展示空栏。都不影响选取与投递正确性） | **待排（批M 只记不修）**：①的修法要动 `UpdateObsConfigRequest`（指针字段或显式 `null` 语义），那是**对外 JSON 契约变更**，前端得同步；②的修法要先定口径——"测试失败要不要写进配置行"与"什么时候清"（成功时抹掉？保留最近一次？）都是产品决定，不该由审计批替答。两条都不宜夹在渠道契约批里顺手做 |
| N-32 | `internal/service/channel_media.go:72-87`（`storeInboundMedia` 取默认失败的分支） | **零默认状态下入站媒体静默改落本地盘，且不留一行日志**：`GetDefault` 返回**任何**错误（`ErrRecordNotFound`，也包括批M 之后新增的「默认行不是 active」这一类）时，这一支不报错，而是就地构造 `&model.ObsConfig{Provider: local, Endpoint: $STORAGE_LOCAL_BASE_DIR, Domain: "/files", IsDefault: true}` 交给 `storage.Factory` ⇒ 客户发来的图片落到**当前进程的本地磁盘**，公开 URL 照样成立（`/files` 有静态挂载），而管理页显示的默认是另一台云存储。容器重建/多副本滚动后取不到文件，运维侧**零线索**（该分支无 `logger` 调用、转存照常返回成功）。批M 把「造出零默认」最常见的成因（事务外先清后设）收进了一个事务；批M-2 又把**删掉默认行**这条路堵上（`DeleteNonDefault` 的 WHERE 里有 `is_default = false`，见 §5 N-33）。⇒ 本条要触发只剩一条路：默认行的 `status` 被改成非 active（`UpdateStatus`）—— 而它今天零生产调用方（§23.1），所以"零默认 + 入站媒体改道"目前仍是**做不出来**的状态，本条因此是"缺可观测性"而不是现行故障 | 读码 + 按对象口径的写面 grep（见 §23.1 第二遍）：`channel_media.go` 里 `GetDefault` 唯一命中 `:72`，其 `if err != nil \|\| cfg == nil` 分支 `:73-87` 全文无 `logger.`；对照另两条消费方 `service/obs_config.go:281`、`content/service/material.go:96-99` 都是直接 `return err` | P2（不是「选错桶」，而是「文件去了一个没人知道的地方」，暴露时机是用户点开历史图片 404 那天） | **待排，本批不动**：① 该文件压着并行泳道的未提交改动（`git status` = ` M`，批F-4f/批I 的历史改动也在同一文件），叠刀会让「哪条红归谁」分不清 —— 与 N-30 同一条纪律；② 修法分两半、第二半是产品口径：**先补一行 Warn**（可观测性，不改行为，拿到该文件的人当场就能做）；**要不要取消静默兜底**需判断 —— 私有化部署靠 `init_storage.go` 启动 seed 保证表非空，兜底只在 seed 失败或被删光时生效，取消它会把这些场景从「能用但落错地方」变成「直接失败」，属可用性取舍 |
| N-33 | `internal/repository/obs_config.go:132-141`（`DeleteNonDefault`）+ `internal/service/obs_config.go:164-181`（`DeleteConfig`） | **"删除存储配置"是先读后删两句，中间一次并发就能把全站唯一默认删走**（§23 把"设默认"那条零默认窗口收进一个事务之后，同一面里剩下的最后一口）：服务层 `GetByID` 看 `IsDefault`、不是默认才 `Delete`。管理员在另一处点"设为默认"（批M 之后那是一句事务，提交即生效）落在**预读与删除之间** ⇒ 被删那一行已经是唯一默认，两次点击界面都回"成功"，库里留下零条默认 ⇒ `GetDefault` 从此无行可选：两条上传口报"未找到默认存储配置"，入站媒体静默改道本地盘（N-32）。而 `InitDefaultStorageIfEmpty` 只在**表为空**时兜底 seed，这次只删掉一行、表里还有别的行 ⇒ **没有任何自愈路径**（与 N-26② 同一后果，只是窗口从"两句 SQL 之间"缩到"一次读与一次写之间"） | 读码（`DeleteConfig` 预读 + `Delete` 落库，两句之间无事务无锁）+ 选取面写口逐个核过：`Create`（`IsDefault = true` 仅当 `Count == 0`，第二台由 §23.2 的偏唯一索引拦，且该判定只在表空时为真，造不出双默认）、`Update` 白名单不含 `is_default`（M23 钉）、`SetDefault` 单事务（M04–M07 钉）、`Delete`（修前**无任何判据**）、`UpdateStatus` 零生产调用方（§23.1）⇒ 界面上还能改动选取结果的口子，删这一条是最后一个。修前取红因：以 R7 三条断言先在真库上跑出"默认行被删走 + 返回 nil"，再落地 `DeleteNonDefault`。修后 M28/M29 红因集合互不相同（谓词 vs `RowsAffected`），见 §23.4 | P1（触发前提是两次管理页写操作交叠，不是单击可达，故不升到 P0；但一旦落到就是"全站上传全断且无自愈"，与 N-26② 同级） | **已修（批M-2，2026-09-21，未提交）**：判据下移到写语句本身 —— 仓储口 `Delete` 改名并实现为 `DeleteNonDefault`（`DELETE … WHERE id = ? AND is_default = false`，`RowsAffected == 0 ⇒ gorm.ErrRecordNotFound`，与 `SetDefault` 同口径："是不是默认"由那条语句负责，不由调用方的记忆负责）；服务层预读**保留但职责降级**为"把错误话说清楚"，注释写明它不是安全判据（并发越过它时返回的是通用 `record not found`）。腿：R7（真库：默认行删不动且库里仍 1 条默认 / 非默认行删得掉且是真删 / 不存在的 id 必须报错）+ S5（替身：只许走带守卫入口 / 文案点名"默认" / `beforeDelete` 铺出并发抬升后删除必须失败且行仍在）。格 M28–M30 全杀，见 §23.3、§23.4；勿删条款见 §23.6 第 11 条 |
| N-34 | `internal/storage/factory.go:15-29`（`Factory`，四家云合并成一条 `case` 在 :22-24）+ `internal/service/obs_config.go:205-242`（`TestConnection` 四支云厂商臂）+ `internal/controller/obs_config.go:143-148` 与 `user-web/src/views/system/ObsConfig.vue:314-315` | **"测试连接"对四家云厂商报的"成功"不代表任何一次网络往返**：`Factory` 对 aliyun/qiniu/tencent/aws 一律 `return newCloudStub(cfg), nil`（`cloudStub` 的 `UploadMultipart/UploadReader/Download/Delete/SignUploadURL/Exists` 六个方法各自返回 "SDK not wired yet"），于是 `TestConnection` 那四支里唯一可能产生错误的第二句（`if _, err := storage.Factory(...)` ⇒ `驱动构造失败`）**不可达** —— provider 已被外层 switch 收窄到这四家，`Factory` 对它们永不返错。四支的真实判据因此只剩"AK/SK/Bucket 三个字符串非空"，而 controller 拿到 nil 就回 `"连接测试成功"`、管理页照原文弹绿色提示 ⇒ 填错的 AK、不存在的 bucket 一律"连接成功"；同一台一旦点"设为默认"，下一次上传当场死在 `cloudStub` 那句 SDK not wired。"测过了"与"能用"之间没有任何一次证明 | 读码 + 新增腿 S7（`obs_config_batchm3_test.go`：4 家 × {三字段齐→nil、缺 AK、缺 SK、缺 Bucket} + 缺 region 不拦 + 未知 provider 报错 = 18 子档，全绿）。**这条腿是含金量声明、不是成绩单**：它把"三字段齐即 nil"钉成事实，同时在腿注里写明这一句只证明格式、不证明可连通。配套变异格 M33–M38 六格各杀一角（三家各摘一个字段的判空、AWS 整段摘掉、七牛文案改通用、default 臂改 `return nil`），见 §23.10。附带核过的可达性：`Driver.PublicURL` 在接口层零生产调用方（`grep` 全仓非测试命中只有 `local.go:121` 调自己的那台），故 `obsConfigAccessURL` 里四条按 region 拼 URL 的分支今天不承重 —— 空 region 的下游后果只在 SDK 接上之后才成立，本条不把它算作现行缺陷 | P2（不静默丢数据，但**主动给出错误的健康信号**：运维据此认为云存储已就绪并把默认切过去，切完下一秒全站上传失败；而管理页此刻显示的是绿色成功态，不会有人去复核） | **本批只登记不修**：真正的修法是"把四家 SDK 接上"（每家一个 client 依赖 + 凭据校验 + 超时与探测对象口径），属产品功能开发而非契约审计。两个次一步的收敛方向都改对外语义、需产品拍板：① 云厂商臂的文案改成明确"仅校验配置格式，未做连通性探测"（controller 与前端提示要同步）；② 或 `Factory` 在未接线期间对四家直接返回 `ErrUnsupportedProvider`，让"建配置/设默认/测试连接"三条口一致地拒绝云厂商，而不是前两条接受、第三条报喜。**同批核出、同样未修的两处小分歧**：`dto/obs_config.go:6-8` 注释写"云厂商必须填 AccessKey / SecretKey / Bucket / Region"，实现只认前三个（S7 专设一档断"缺 region 不拦"，钉的是今天的真值而非应有值）；`TestConnection` 四支里的 `驱动构造失败` 分支不可达，本批不为不可达分支编造断言，谁接上 SDK 它才活过来 |
| N-35 | `internal/model/ai_sales_champion.go:39`（`MessageHub.DeletedAt gorm.DeletedAt`）+ `internal/pkg/db/migrate.go:513`（`CREATE UNIQUE INDEX … ON message_hub (platform, msg_id, conversation_id)`，**不带 WHERE**） | **三元组唯一索引看不见软删，于是"重投一条被丢弃的消息"这条路在软删世界里永久封死**：`message_hub` 带 `deleted_at`（且 `internal/migration/migrations/v3_22_1_soft_delete_migration.go:35` 把它列进 `coreTables`），而那道去重键是全列唯一、不排除已软删行 ⇒ 任何一次 GORM 默认口径的 `Delete()`（少写一个 `Unscoped()` 就是那种改动）都会留下一行"逻辑上没了、键还占着"的记录：同一条消息再投一次（人工补发、渠道重推、迁移回灌）当场撞 23505，而入库口拿到唯一键冲突只记一行日志、不重试 ⇒ **消息静默丢失，且库面查询怎么看都像"这条早就在了"**。同一枚反面：读侧已经有五处**裸 SQL**（`Table("message_hub")` 全仓非测试、非迁移命中 9 处，其中读 5 处 —— `repository/csplus_ops_repo.go:45`（死信计数）、`:55`（死信列表）、`repository/message_hub_inbox_conversation_query.go:108`（会话列表取"最后一条"，`DISTINCT ON (conversation_id) … ORDER BY sent_at DESC`）、`service/trace_learning/aggregator.go:61,70`（学习语料兜底取正文），另 4 处是写口）。裸 SQL 不自动加 `deleted_at IS NULL`，而 GORM 口径的读会（模型带 `DeletedAt` ⇒ 组内 `Model(&MessageHub{})` 的查询自带过滤）⇒ 软删一旦落地就当场多出"收件箱看不到、死信页/会话列表/学习语料里还在"的读路径裂脑（`grep -c deleted_at` 在上述四个文件里全为 0；全仓唯一命中的 `r44_gap.go:315` 是另一张表的语句），而这正是不会让人怀疑到唯一键上的那种症状 | 一次性取证探针跑出来的四档（`/tmp/bm5_first.log`，用例文件 `zz_batchm5_probe_test.go` 取证后即从两棵树删场，`find` 复核无残留）：`PROBE-A 软删之后可见行=0 含未删除标记的总行=1`（键确实还占着）→ `PROBE-B 同三元组重投：失败 err=duplicate key value violates unique constraint "uni_message_hub_platform_msg_conv" (SQLSTATE 23505)` → `PROBE-C 物理清理 rows=1` → `PROBE-D 之后同三元组重投 err=<nil>`。**可达性按写口逐个枚举过，结论是今天不可达**：全仓对 `message_hub` 的生产删除口只有两处、都是物理删 —— `repository/message_hub_inbox.go:133`（`Unscoped().Where("id = ?").Delete(&model.MessageHub{})`）与 `repository/csplus_ops_repo.go:78`（`Table("message_hub").Where("id = ? AND status = 'failed'").Delete(nil)`，本批实测走 DELETE 而非软删标记）。⇒ 本条不是现行故障，是"离现行故障只差一个 `Unscoped()`"的结构缺陷 | P3（今天没有生产路径踩得到，故不升；一旦有软删写口落地就是 P2 级静默丢消息，且没有任何现场症状指向唯一键） | **登记不修**，两条理由：① 现在改索引形状要 `DROP INDEX` + 重建，而真实库里那条索引今天还不存在（§23.8 第 7 条同口径），改不到东西、只能把断言搬到将来；② 选哪种修法是产品口径 —— 要么把索引改成 `WHERE deleted_at IS NULL`（"丢弃过的消息允许重投"），要么在软删契约里把 `message_hub` 显式豁免（"去重键含已删行，丢弃不许重投"）。两种都自洽，混着才是最坏的。**批M-5 已经把两半都钉在腿上，只等哪一半被选中**：S16 第三段（`message_hub_unique_batchm5_test.go:246-262`）钉"物理删之后同三元组必须能重投"，这一句在两种世界里都必须成立；S17 的形状断言钉今天读到的 `indisunique = t / indpred = NULL`，⇒ 谁加 `WHERE deleted_at IS NULL` 都会先让 S17 红，那是**一次被迫的对话**而不是回归（这条腿故意写成"绊线"而不是"锁"）。见 §23.12 第 4 段 |
| N-36 | `internal/pkg/db/migrate.go` 三条 post-migrate 守卫的**结论面**（`:493`／`:542`／`:561` 各自那句 `logger.Info("post-migrate: … 已就绪")`，判据只来自 `:489`／`:538`／`:556` 那句 DDL 的 `err`） | **名字在场 ≠ 形状在场，而那三句『已就绪』只看名字**：`CREATE UNIQUE INDEX IF NOT EXISTS` 的对账口径是**索引名**，库里若已有一枚同名**非唯一**索引，整句是静默 no-op（`err=<nil>`），于是钩子往下走到 `else` 支、对运维报出与库里事实相反的那一句。这三条钩子存在的唯一理由（钩子注释原文）就是「让『第二层没铺上』这件事在启动日志里看得见」，而这一格它恰好反过来。补不回来的是第二层：GORM 的标签对账同样只看名字（`driver/postgres@v1.6.0/migrator.go:109` 的 `HasIndex` 只查 `pg_indexes.indexname`）⇒ 只要同名非唯一索引在场，**标签那一层也不会**把它换成唯一的，两侧一起沉默 | 探针 J（`message_hub` 上手工铺一枚同名非唯一 `uni_…` 之后跑钩子）实测：`err=<nil>`、`pg_index.indisunique=false`、同三元组两行**都落得进去**；探针 I 实测 GORM 侧 `HasIndex(名字)=true` 而形状仍非唯一 ⇒ 两层的「按名字对账」在同一格上互相掩护。腿的判据面按修复前后各跑一次核对：修复前那三条腿红在「仍报已就绪」，修复后同一处红因变成「没有点名是哪一枚索引不是唯一的」⇒ 文案是要件，不是装饰 | P3（今天没有生产路径会铺出那三枚同名非唯一索引：`uni_message_hub_platform_msg_conv` 的唯一生产者是模型标签、另两枚的唯一生产者是本钩子；但一次手工救火（「索引建不出来就先建个普通的让服务起来」）就把这一格点亮，而点亮之后的症状是**日志说兜底在、兜底其实不在**） | **已修（批M-7，2026-09-21，未提交）**：新增 `verifyUniqueIndex(db, indexName)`（`migrate.go:434-461`，函数体 `:448-461`，按名字读 `pg_index.indisunique`；非唯一、或读不到形状，都返回一句带索引名的归因），三条钩子各在报『已就绪』之前过这一道（`:489`／`:538`／`:558`）⇒ 形状不对时落 `Warn`、点名是哪一枚、并说清下一步该 `DROP` 谁；钩子**只报不改**（不去动别人名下那枚索引，那是启动路径上的静默结构改写）。腿：S27／S28／S29（`index_shape_batchm7_test.go`，同一符号被三处消费 ⇒ 逐处拆刀），格 M77–M82。见 §23.14 |

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
| 钉钉 | 自定义机器人："每个机器人每分钟最多发送20条消息到群里，如果超过20条，**会限流10分钟**"；回调侧 `sessionWebhook` / `sessionWebhookExpiredTime`（毫秒 epoch）在**回调体里** | 官方只给"限流 10 分钟"这一量级，且整合消息的建议 ⇒ 与 2/10/30s 梯度不在同一量级；`sessionWebhookExpiredTime` 过期后重投必然无意义 | ~~过期/缺失分支根本不构造错误（N-13）~~ → **批F-6 已修**：缺失/过期/域名非法三支现在回传不可重试的 `*ChannelError` 并落 `send_failed` 轨迹（过期值按毫秒归一后写进原因，§22.1、L1–L3） | B→降级（自定义机器人页 `curl` 直连只拿到 SPA 空壳，「20 条／限流 10 分钟」本轮**未取到原文**，不得作为改码依据） |
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
  修法：新增 `douyinTransientError`（带外瞬时态的载体）+ `douyinStatusRetryable`（5xx 与 429 收，其余 4xx 判终态：403 官方口径是「否则无法访问相关资源」）+
  `douyinErrRetryable`，并把重投从"资源接口这一条腿"上移到 **`FetchDouyinMessageResource` 整动作**（`douyinFetchMessageResourceOnce` 为一次走完三条腿的内层）。
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
| G14 | G1 | `service/webhook_channel_douyin.go:263` `if env.Event == "" \|\| !haveContent {` | 信封解不动时不再走通用兜底 ⇒ 桥上报的怪报文彻底无留痕 | catch | 18/18（17 绿 1 红）（26 行 PASS） | 1 | …BatchG_DispatchDouyin_UnparseableBodyStillGoesGeneric | 0 | ok | ✅ 被抓 |
| G15 | G1 | `service/webhook_channel_douyin.go:169` `case "text", "other":` | message_type=other 不取正文 ⇒ 官方「请打开抖音app查看」那类提示变 [other] | catch | 18/18（17 绿 1 红）（25 行 PASS） | 2 | …BatchG_DispatchDouyin_MsgTypesInsideHubVocabulary | 0 | ok | ✅ 被抓 |
| G16 | G1 | `service/webhook_channel_douyin.go:164` `return "[" + messageType + "]"` | 未知类型统一压成 [消息] ⇒ 平台新增类型时看不出是哪种 | catch | 18/18（17 绿 1 红）（25 行 PASS） | 2 | …BatchG_DispatchDouyin_MsgTypesInsideHubVocabulary | 0 | ok | ✅ 被抓 |
| G17 | TT | `service/webhook_channel_douyin.go:397` `return string(ChannelTiktok), "tt"` | TikTok 平台标记写回 douyin ⇒ 两家消息在 hub/线索表里混成一家、幂等键互相吞 | catch | 1/1（0 绿 1 红）（0 行 PASS） | 1 | …DispatchDouyin_TikTokMustNotBeLabelledDouyin | 0 | ok | ✅ 被抓 |
| G18 | CT | `service/webhook_channel_douyin.go:240` `return json.RawMessage(`{"challenge":` + str…` | challenge 被加引号回显（数字→字符串）⇒ 抖音控制台判定校验失败，注册卡死 | catch | 4/4（2 绿 2 红）（2 行 PASS） | 2 | …WebhookRoute_Douyin_ChallengeTypePreserved, …WebhookRoute_Douyin_VerifyWebhookEchoesNumericChallenge | 0 | ok | ✅ 被抓 |
| G19 | CT | `controller/webhook.go:132` `if channel == service.ChannelDouyin {` | challenge 分支挂错渠道（挪到 tiktok）⇒ 抖音保存回调地址那一步永远过不去 | catch | 4/4（2 绿 2 红）（2 行 PASS） | 2 | …WebhookRoute_Douyin_ChallengeTypePreserved, …WebhookRoute_Douyin_VerifyWebhookEchoesNumericChallenge | 0 | ok | ✅ 被抓 |
| G20 | CT | `service/webhook_channel_douyin.go:225` `if !ok {` | 验签失败仍回显 challenge ⇒ 握手变成无门槛回声弹（任何人都能替我们「完成校验」） | catch | 4/4（3 绿 1 红）（3 行 PASS） | 1 | …WebhookRoute_Douyin_VerifyWebhookRejectsUnsignedChallenge | 0 | ok | ✅ 被抓 |
| M01 | MB | `service/douyin_media.go:198` `if e, ok := douyinTokenCache[clientKey]; ok …` | client_token 不再走缓存 ⇒ 每条媒体各敲一次，撞官方频控 10020 并互相顶号 | catch | 25/25（18 绿 7 红）（21 行 PASS） | 10 | …G2B_ClientTokenIsCachedAcrossBurst, …G2B_ExpiredTokenCodeAlsoRefreshes, …G2B_Gateway5xxRetriesEveryLeg, …G2B_PermissionCodesAreTerminal, …G2B_RetryBudgetIsBoundedAndTerminal | 0 | ok | ✅ 被抓 |
| M02 | MB | `service/douyin_media.go:298` `token, err = douyinClientTokenForce(ctx, cli…` | 28001003 后不重取 token ⇒ 带着坏值一路失败到 2 小时到点 | catch | 25/25（22 绿 3 红）（28 行 PASS） | 3 | …G2B_ExpiredTokenCodeAlsoRefreshes, …G2B_RefreshThatDoesNotHelpIsTerminalWithErrNo, …G2B_StaleTokenRefreshesOnceAndRetries | 0 | ok | ✅ 被抓 |
| M03 | MB | `service/douyin_media.go:327` `return e.ErrNo == dyErrTokenInvalid \|\| e.Err…` | 只认 28001003 不认 28001008（官方两条处置相同）⇒ 过期不刷新 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_ExpiredTokenCodeAlsoRefreshes | 0 | ok | ✅ 被抓 |
| M04 | MB | `service/douyin_media.go:340` `douyinAPIBase()+douyinMsgResourcesPath+"?"+q…` | query 里的 + / = 不做百分号编码 ⇒ 官方 ID 被解成空格，只看到「这条消息没有媒体」 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_OfficialIDsAreQueryEscaped | 0 | ok | ✅ 被抓 |
| M05 | MB | `service/douyin_media.go:389` `return strings.TrimSpace(strings.ReplaceAll(…` | 字面量 \u0026 不还原成 & ⇒ 直链带着反斜杠去请求，拿到 404 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_ResourceURLUnescuesAmpersand | 0 | ok | ✅ 被抓 |
| M06 | MB | `service/douyin_media.go:367` `if payload.ErrNo != 0 {` | 只看 HTTP 状态码不看 err_no ⇒ 业务错误被当成功、再去下载空 URL | catch | 25/25（18 绿 7 红）（24 行 PASS） | 7 | …G2B_ExpiredTokenCodeAlsoRefreshes, …G2B_NonZeroErrNoIsNotASuccess, …G2B_PermissionCodesAreTerminal, …G2B_RefreshThatDoesNotHelpIsTerminalWithErrNo, …G2B_RetryBudgetIsBoundedAndTerminal | 0 | ok | ✅ 被抓 |
| M07 | MB | `service/douyin_media.go:344` `req.Header.Set("access-token", token)` | resources 不带 access-token 头（官方必填） | catch | 25/25（22 绿 3 红）（28 行 PASS） | 3 | …G2B_DownloadCarriesAccessTokenAndOpenID, …G2B_FetchWalksOfficialTwoLegContract, …G2B_RefreshThatDoesNotHelpIsTerminalWithErrNo | 0 | ok | ✅ 被抓 |
| M08 | MB | `service/douyin_media.go:204` `"grant_type":    douyinGrantTypeClientCred,` | grant_type 写成 client_credentials（官方固定值是 client_credential）⇒ 10002 参数错误 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_FetchWalksOfficialTwoLegContract | 0 | ok | ✅ 被抓 |
| M09 | MB | `service/douyin_media.go:402` `req.Header.Set("Access-Token", token)` | 下载直链不带 Access-Token/OpenID 头（官方明写否则无法访问） | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_DownloadCarriesAccessTokenAndOpenID | 0 | ok | ✅ 被抓 |
| M10 | MB | `service/douyin_media.go:395` `if err != nil \|\| (u.Scheme != "https" && u.S…` | 直链形状不判 ⇒ 渠道回个 file:// 就带着凭证去开本地文件/内网端口 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_ResourceURLMustBeAbsoluteHTTP | 0 | ok | ✅ 被抓 |
| M11 | MB | `service/douyin_media.go:155` `if k := douyinMsgKey(r.MessageID); k != "" {` | 存储键直接用官方 base64 ID ⇒ 斜杠变子目录、点点点变穿越，只有转存成功时才走这条路 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_MediaKeyHasNoPathSeparators | 0 | ok | ✅ 被抓 |
| M12 | MB | `service/douyin_media.go:164` `return messageType == "user_local_image" \|\| …` | 所有消息类型都去敲 resources ⇒ 官方只支持 user_local_*，其余白烧频控 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_OnlyUserLocalTypesFetch | 0 | ok | ✅ 被抓 |
| M13 | MB | `service/douyin_media.go:435` `if hubMsgID == "" \|\| !ref.complete() {` | 官方三个 ID 缺一也照发 ⇒ 注定 28001007 参数不合法 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_IncompleteOfficialIDsSkip | 0 | ok | ✅ 被抓 |
| M14 | MB | `service/douyin_media.go:429` `if channel != ChannelDouyin {` | TikTok 也走抖音媒体端点（无 A 档依据）⇒ 静默失败还掩盖真实缺因 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_TikTokNeverCallsDouyinEndpoint | 0 | ok | ✅ 被抓 |
| M15 | MB | `service/douyin_media.go:439` `if err != nil \|\| clientKey == "" \|\| clientSe…` | 没配 client_key 也起换取 ⇒ 拿空凭证敲门；且掩盖「只有 webhook 密钥换不到 token」这一真实缺因 | catch | 25/25（24 绿 1 红）（30 行 PASS） | 1 | …G2B_MissingClientKeySkips | 0 | ok | ✅ 被抓 |
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
> **【批J 结论（2026-09-20，见 §18.1）】** 已取到 A 档：`code` 是 **number**、端点确为
> `https://api.bot.qq.com/app/getAppAccessToken`，且业务失败**仍回 HTTP 200** ⇒ 上面那段推断的方向对、
> 但结论相反——实现没错，**错的是夹具**（把字符串塞进 int 才让用例打偏到 parse 分支）。
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

> **【批K 闭合（2026-09-21，见 §19）】** ①② 两步都已做完，且**电池没有作废**：`webhook.go` 收口 md5 仍是
> §17.8 认的 BASE 值 `3cb01f02…`（三刀变异全部 `cp` 还原并比过字节）。守卫用例
> `TestBatchK_QQHandleJobDoesNotDoubleTriggerAI` 以 `handleJob` 为入口钉住**三臂**（中台 1 次 / QQ 侧 0 次 /
> 非 QQ 侧 1 次），K-m1、K-m2、K-m3 三刀分别打死三臂、红因落在三个不同断言行（§19.2 表）。
> 本节那条"诚实口径"括号里的话（"摘掉半句跑全量仍绿"要等整树电池）已被就地兑现：**不需要等** —— 摘掉半句
> 现在就有专用用例红。一处偏离：处置① 要求"注释一起清掉"，实测那句注释指代的是**活函数** `triggerSalesEngine`
> 而非被删的死函数，删它等于删掉守卫的存在理由 ⇒ 保留，理由记在 §19.3 末段。
> 处置②原文里"其余渠道恰好一次"这一臂在我第一版里也没做（只钉了 QQ 的 0 次），是复查 §17.7 时发现自己
> 漏了一臂才补上（补上之后才拦得住 K-m3 那种"整段不触发"的失误）。



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

> **【批K 闭合（2026-09-21，见 §19.3 处 3/4）】** 已删，连同它唯一的消费者（`inbox_ingress_dedup_test.go` 的
> `场景8_内容hash计算验证` 那一格，同文件其余七格未动）与随之失去使用者的两个导入。

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


## 18. 批J：四家 token 腿的「HTTP 200 + 空凭证」格（2026-09-20，含 QQ A 档两项闭合）

§17.7 把这一格从"没有用例"读到"用例是错的"，并登记了三处（结果 A 假覆盖、结果 B 钉钉不可测、结果 C 飞书连判定都没有）。
本批按那条清单落地，一处顺带扩到第四家。**先说没做成的**：钉钉业务错误码仍无 A 档（§18.5），它不是"待办忘做"，
是取证手段够不到，因此那条缺口被钉成了一条**显式断言"未证"**的用例，防止后来人当成已覆盖。
§17.7 结果 D（QQ 零断言守卫用例 + `triggerQQSalesEngine` 死函数）不在本批 —— 它按 §17.7 的判定归批I/批K，
且必须与 `webhook.go` 那半句守卫同批动（`webhook.go` 是 §17.5 电池的四份 BASE 之一）。

### 18.1 QQ A 档：把 §6 N-21 与 §17.7 的两项"未取证"结掉

取证页 `https://bot.q.qq.com/wiki/develop/api-v2/dev-prepare/access-token.html`：curl 直连 http=200、
**24,598 B**、`url_effective` 未被改写；副本 `.tmp_files/mut-evidence-2026-09-20/batchJ/access-token.html`
（md5 `f1acdd7dc218e41635259d0fa7fafea0`）。**反面物证**同目录 `apiv2-root.html`（24,658 B、md5
`05ec9711bbc4c7c93099cbf8f1b337c4`）—— 那是 wiki 深链被 302 送回根页的壳，与 §17.9 记的 batchM 那次同一种陷阱。

原文里对本批有决定性意义的四句：

| 原文 | 后果 |
|---|---|
| 基本 HTTP URL `https://api.bot.qq.com/app/getAppAccessToken`，Method POST | 仓内 `defaultAPIBase = "https://api.bot.qq.com"` 一致 ⇒ §6 N-21 末"端点未证"结掉 |
| 失败响应 `code` **number** / `message` string | 实现里 `tokenResp.Code int` 是对的 ⇒ §17.7"类型未证"结掉，且证明那条假覆盖用例的夹具（字符串 `"100014"`）本身就是错的 |
| 「该接口的业务错误通过响应体的 code 返回，即使调用失败，HTTP 返回码仍为 200」 | 判档不能走状态码那一路；本批新用例全部按 `status==200` 铺 |
| 「不要依据 `message` 判定错误类型」「message 仅用于人工排查，内容可能随时调整」 | 码表必须按码不按文案 ⇒ 电池里专门铺了一格"官方换了文案" |

成功示例写的是 `"expires_in": "7200"`（字符串）而参数表写 `number`——官方自相矛盾，实现两侧都容忍（`tokenResp.expiresInSeconds`），
记下来是为了下次别把这条当缺陷去"修"。

### 18.2 结果 A（QQ 假覆盖）：夹具按官方形状重写

`internal/channelbot/qq/qq_test.go:390 TestClient_TokenErrorSurfaces` 原来铺 `{"code":"100014"}`（带引号），
打在 `json.Unmarshal` 的类型错误分支上，只判 `err != nil` 就绿 ⇒ 名字声称测 token 失败，实际测的是"JSON 类型不匹配会报错"。
现在铺 `{"code":100007,"message":"appid invalid"}`，并断言错误文本里同时出现 `100007` 与官方原话。

同文件新增 `:420 TestClient_TokenEmptyAtHTTP200`，三条断言各钉一件事（缺一就是假绿形态）：
① 报错且带根因码与文案；② 失败后 `c.accessToken==""` 且 `c.tokenExpAt` 是零值、第二次调用仍打网络
（`tokenCalls==2`，证明"空值没被当成有效凭证缓存住"）；③ 取不到凭证时发送腿零调用（`sendCalls==0`）。

### 18.3 结果 A 的另一半：QQ 取凭证失败码在归类表里根本没有行（产品缺陷，已修）

`internal/service/channel_error.go` 的 `channelCodeRules["qq"]` 原本只有**发送腿**的码，取凭证腿那四个码一条没有 ⇒
落 `default:` = `CategoryUnknown + Retryable=true`：AppID/Secret 错这类"重投不会自己变好"的失败白撞三轮退避，
且永远不触发 `CategoryAuth` 那条运维事件（凭证坏了没人知道）。补四行（`:192-195`，注释连 A 档出处一起写）：

- `100001 Too many requests` → RateLimited / 可重试 / **无** RetryAfter（官方只说降频、没给解除时长，不许自己编）
- `100007 appid invalid`、`100016 invalid appid or secret`、`10004 机器人不存在` → Auth / 不可重试

`10004` 是 5 位、不跟 `1000xx` 一族同形，这是官页面原文，不是笔误（差点按"族"补成 `100004`，被原文否掉）。

新用例 `internal/service/webhook_batchj_qq_token_test.go:27 TestBatchJ_QQTokenFailureRetryTier`：
五个子用例真起 `qq.NewClient(..., core.WithBaseURL(srv.URL))`，从**客户端错误串**过
`AsChannelError` → 断言 `Channel/StatusCode==200/Code/Category/Retryable`，再过 `retryDelaysFor` 断言退避后果
（Auth 一档必须拿到 0 次延迟）。其中一格把文案换成 `"please slow down"` 而码仍是 `100001` —— 用来证明判据是码不是文案。

### 18.4 结果 B（钉钉不可测）：加缝 + 四格形状 + 把缺口钉成断言

`internal/service/dingtalk_media.go:39` 新增 `var dingtalkOpenAPIBase = "https://api.dingtalk.com"`，
取凭证（`:116`）与机器人接收文件下载（`:55`）两条腿都改拼它。这是仓内**第三家**（先例 `telegram_media.go:39 tgAPIBaseOverride`、
`douyin_media.go:130 dyAPIBaseOverride`）；写这一句的理由就写在注释里：域名硬编码 + 上层替身把整条腿换掉，
是"200+空凭证"那一格至今零用例的直接原因。

parse 分支（`:131`）改成回带响应体片段：`dingtalk token parse status %d: %w body=%s`。
200 但应答不是 JSON（网关/登录页把请求换掉了）时，只有 json 报错的话现场分不出"钉钉答了"还是"根本没打到钉钉"——与抖音 token 腿同口径。

新用例 `internal/service/webhook_batchj_dingtalk_token_test.go`：

- `:21 TestBatchJ_DingTalkTokenLegShapes` 四格 —— 200+空 accessToken / 200+非 JSON 应答 / 403+空 accessToken /
  **正例腿**（拿到凭证，断言下载请求头真的带上 `TK-DT-1` 且 `downloadCalls==1`；只数失败格不铺成功格，
  实现改成"永远报错"也能全绿）。
- `:116 TestBatchJ_DingTalkTokenErrorIsNotRetryable` 把本批**没取证到的那件事**写成断言：
  `Channel=="dingtalk"`、`StatusCode==200`、**`Code==""`**（钉钉错误串里没有可被 `reChannelCode["dingtalk"]` 提取的
  `errcode=` 字段）、`Retryable==true`。这条用例的含义是"缺口还在"，将来补上 A 档码表时它必须一起被改红 ——
  缺口因此是跑出来的，不是读码读出来的。

### 18.5 钉钉 A 档为何仍然没有（诚实口径）

`open.dingtalk.com` 的文档站是 JS 渲染的 SPA：探针 `dt-internal-app-token.html` 抓到 **362,633 B**，
`accessToken`/`errcode`/`expireIn` 三个关键词在正文里**零命中**（全是壳）。字节数大不等于取到原文 ——
按 §6 的 A/B 档规矩（A ＝ curl 直连取到正文并逐字核对），这种一律不得当 A 档援引。
钉钉站是 SPA 这件事 §6 早有记录（N-01、N-05 两行），本轮的增量不是"发现它是 SPA"，
而是**量了一遍空壳有多大**：362,633 B、md5 `ff2837746b3d861148a58c2416517594` ——
以后谁再拿"这一页抓下来了"当依据，比对这两个数即可判伪。所以钉钉码表这一格留在"未证"，代价写进 §18.4 那条断言里。

### 18.6 结果 C（飞书，第六处同型站点）：连判定都没有

`internal/service/feishu.go:374`（补守卫前该函数最后一道判定是 `out.Code != 0`）；`code==0` 而 `tenant_access_token` 为空时，
会把 `""` 连同**未来过期时间**一起写回账号行并返回 `("", nil)`，调用方（`sendMessageTyped`）拿着空 token 真发一次请求，
现场只剩平台侧鉴权错误码。补：

```go
if out.TenantAccessToken == "" {
    return "", fmt.Errorf("feishu token empty code=%d msg=%s expire=%d", out.Code, out.Msg, out.Expire)
}
```

新用例 `internal/service/webhook_batchj_feishu_token_test.go:63 TestBatchJ_FeishuTokenLegShapes`，两层判据
（缺一条会假绿）：① 报错且错误里留着 `code=0`/`msg=success`（根因在上一跳）；② **内存与落库两侧的
`TokenExpires` 都必须是 nil** —— 只看"下次仍打网络"抓不住这一半，因为 `AccessToken==""` 本身就会让缓存判定失效。
正例腿钉过期时间落 6600~7200s（实现留 300s 提前量）。

注入方式与钉钉不同：**没加 base 缝**，改换 `httpclient.Client`（成对还原）。理由是 `open.feishu.cn` 在非测试代码里有四处
（`feishu.go` 两条腿、`channel_media.go:273`、`kb_connectors.go:189`），只为一条腿加缝会留下"同域两个写法"；
仓内也早有换客户端的飞书用例（`feishu_test.go:87`）。

**两条变异各杀一半判据**（实录 `batchJ/feishu-Jm4-red.log`、`batchJ/feishu-Jm4b-red.log`，还原后绿证
`batchJ/feishu-restored-green.log`）：

| 刀 | 变异 | 红因（实测落在哪一行） |
|---|---|---|
| J-m4 | 整块摘掉守卫 | `:80: 第 1 次取用：空凭证必须报错，got token=""` |
| J-m4b | 守卫保留、但挪到写缓存**之后** | `:93: 内存里的账号不得被写入未来过期时间 … got 2026-09-21 01:11:07 …` |

J-m4b 是这一处的重点：`AccessToken==""` 本身就会让下次的缓存判定失效，所以"两次取用打两次网络"那条断言
**抓不到**缓存污染 —— 只有 `TokenExpires` 那一行抓得住。两条红因落在不同断言行上，证明两层判据各自承重。

写法也和钉钉那两处不同：**没有走快照式电池脚本**，而是用 Edit 就地摘/装那 6 行（`feishu.go` 压着另一条线
75 增 13 删的未提交改动，整文件读-改-写有覆盖别人编辑的风险），两刀之间用 `git diff -U2` 逐次核对改动面只在 `getAccessToken` 内，
跑完后全仓 `grep -rn MUTANT user-server/internal/` **零命中**（无变异残迹）；
还原后与注码前的唯一差异是多了一句注释「守卫必须在写缓存之前」——那正是 J-m4b 教出来的约束。

### 18.7 变异电池与门禁

三处变异证据，脚本与逐刀实录都在 `.tmp_files/mut-evidence-2026-09-20/batchJ/`
（`reverse.log` = qq 包电池，`reverse-service.log` = service 侧判档邻域电池，`feishu-*.log` = §18.6 那两刀）。

| 刀 | 变异 | 预期红集合 | 实测 |
|---|---|---|---|
| J-q1/q2/q3 | `channelbot/qq`：删空凭证判定 / 删根因回带 / 失败后仍写缓存 | 各一条 | 3/3 killed，`qq.go` 还原 md5 `c1ce6914054bbdaedc2396cb07dc9ab9` 一致 |
| J-m1 | 删掉 `channelCodeRules["qq"]` 那四行 | `{TestBatchJ_QQTokenFailureRetryTier}` | killed（ran=43，红集合恰好等于预期） |
| J-m2 | 摘掉钉钉空凭证守卫 | `{TestBatchJ_DingTalkTokenLegShapes, TestBatchJ_DingTalkTokenErrorIsNotRetryable}` | **第一次 BROKEN(编译不过)** —— 锚点只截到 `if` 的两行、没含收尾 `}`，变异体留下孤花括号；补全锚点后 killed |
| J-m3 | 钉钉 parse 分支不回带 body 片段 | `{TestBatchJ_DingTalkTokenLegShapes}` | killed |
| J-m4 / J-m4b | 飞书守卫：整块摘掉 / 挪到写缓存之后 | `{TestBatchJ_FeishuTokenLegShapes}` | 两刀皆 killed，且红因落在**不同断言行**（详见 §18.6 表） |

控制组：`ran=43 skip=0 红=0`（用例宇宙是判档邻域 = 批J 三腿 + N-11 全部 + 整表驱动 + `retryDelaysFor` +
抖音同形三腿，**不是整包**；整包门另跑，见下表）。J-m1 与 J-m3 的红集不同（后者只红形状用例、不红归类用例），
说明"body 片段"与"码表归类"两半各自有主，不是一句 `err != nil` 通吃。

**电池口径新增一条（J-m2 那次 BROKEN 教的）**：锚点必须覆盖**完整语法块**。只截 `if` 头两行会留下孤立花括号，
变异体编译不过 —— 此时既不是"killed"也不是"没杀掉"，而是**什么都没测到**；`run()` 里 `[build failed] → BROKEN`
那条分支就是为这一格准备的，第一次跑就真被它拦住，没有当成"绿"混过去。

**门禁（三趟 `internal/service` 整包 + 两趟 `internal/channelbot/qq/` 整包，日志在 `.tmp_files/mut-evidence-2026-09-20/gates/`）**：

| 趟 | 覆盖 | 结果 |
|---|---|---|
| round1（`batchJ-service-round1.log`） | service 整包，含钉钉/QQ 两腿与码表改动，**不含**飞书守卫 | `rc=0`，505s |
| round2（`batchJ-service-round2.log` + `…-red-excerpt.txt`） | 同上 + 飞书守卫 | `rc=1`，534s，**唯一红** `TestOfflineReplayService_DetectPartitionsByStatus` |
| round3（`batchJ-service-round3.log` / `.meta`） | 同上，重跑到另一线提交落定后的树 | **`rc=0`，408s**，HEAD 记为 `8f07f792` |
| qq round1 / round2（`batchJ-qq-full*.log`） | `channelbot/qq` 整包（含 §18.2 两条用例） | 两趟均 `rc=0`、`-v` 计数 `PASS=23 FAIL=0 SKIP=0`、2s |

round2 那条红的归因是**跑出来的不是猜的**：红因原文 `离线侧应含两个已置离线的渠道` 这一串断言文本，
在二进制编译之后的树里已被另一条线改掉（`grep` 全仓 0 命中；两文件 mtime 23:12:49 / 23:17:13，
均晚于 round2 的编译点；对方随后提交 `2bf9e339 fix(bridge): 离线回扫的渠道检测改读 bridge_accounts 实有列`）。
本批动的四个文件（`channel_error.go`、`dingtalk_media.go`、`feishu.go` 与三个新测试）与 bridge 离线回扫无调用关系，
round3 在同一棵树上转绿 ⇒ 判"另一线在飞窗口的中间态"，不记在本批账上，也不去动别人的用例。

`go test -list '.*' ./internal/service/` 现报 **3712** 条声明用例（跑于 round3 前）—— 这个数含别的会话未提交的用例，
只能说明"整包门不是子集"，不得当成本批的覆盖面数字（本批新增：service 侧 3 个顶层用例 / 11 个子用例，qq 侧 1 改 1 增）。

### 18.8 本批未覆盖面（不得当作已验证，也不得由 §18.1–18.6 的存在而推断它们已解决）

1. **钉钉两条腿仍无码表**：`reChannelCode["dingtalk"]` 提的是 `errcode=(-?\d+)`，而 v1.0 网关失败回的是
   `{"code":"Forbidden.…","message":…}` 一族的**字符串码** —— 该形状来自实现的 parse/拼接分支与既有错误串，
   **官方页未取到**（§18.5），因此不得进表。要闭合缺两件事，都不轻：
   ① 一份可核验原文（SPA 换路子：官方 SDK 源码里的错误码常量，或控制台的对照页）；
   ② `ChannelError.Code` 现在只提数字，收字符串码要扩正则 ⇒ 动到**所有渠道**的判档路径，
   等于 §17.5 那块整树电池重跑。本批只把缺口钉成 §18.4 那条 `Code==""` 断言。
2. **QQ 只做了取凭证腿**：发送腿那三码（`40034100` / `40034128` / `304103`）各有 A 档（见 §6 表 QQ 行），
   但官方另有一整页错误码清单未逐条对过 —— §18.1 取的是 access-token 页，只够定取凭证腿那四码。
3. **飞书那一格没有原文**：`code==0` 且 `tenant_access_token` 为空，官方页只写"code 为 0 表示成功"，
   没有任何一句描述"成功码 + 空凭证"该怎么处理 ⇒ 本批按**保守判失败**处置（与 §18.4 钉钉 parse 格同口径），
   这是工程选择、不是契约结论。
4. **飞书发送腿没补第二道 `token == ""`**：上游守卫已使 `getAccessToken` 不可能回 `("", nil)`，
   再加一道就是死分支；顺序约束改记在守卫注释里（"守卫必须在写缓存之前"），
   并由 J-m4b 那刀证明它一旦被挪走用例立刻红（§18.6 表第二行）。


---

## 19. 批K：QQ 双触发守卫从"零生产路径用例"到三臂互钉（2026-09-21，含同族死代码一刀）

### 19.1 起点：§17.7 结果 D 的两步处置，与本批实际动的字节

处置①＝删死代码，处置②＝删之前先补一条打在**真路径**（`handleJob`）上的守卫用例。两步都在本批做完，
但有一处**刻意不照原文执行**（§19.3 第三项）。先记一件事后文档里到处要用：

**本批没有改动 `webhook.go` 的任何一个字节。** 三刀变异都是"临时改—跑—`cp` 还原—比 md5"，
收口时该文件 md5 仍是 `3cb01f02642e71505fcef7275c09ad0f`，
与 §17.8 那次字节对齐里 §17.5 电池认的 BASE 值**逐字相同**
（那一节列的四份 BASE：`controller/webhook.go 71fbd82f…`、`service/douyin_media.go 5618f208…`、
`service/webhook.go 3cb01f02…`、`service/webhook_channel_douyin.go 8b9cc43f…`）。
⇒ §18 开头那句"必须与 `webhook.go` 那半句守卫同批动、`webhook.go` 是电池 BASE 之一"所担心的**整块电池作废**，
本批没有触发：守卫的**行为**没有被改，被改的是它两侧的**可观察性**。

### 19.2 守卫用例的三条臂，与两条"少一条就是假绿"的前置

新用例：`internal/service/webhook_batchk_qq_dual_trigger_test.go` → `TestBatchK_QQHandleJobDoesNotDoubleTriggerAI`。
入口是 `handleJob` 本体（生产路径 Receive → 队列 → `handleJob`，用例直调后一步，判据就在那函数体内），
计分打在两处：**中台臂**用包内既有替身 `fakeAITrigger`（`inbox_ingress_ai_test.go:14`），
**handleJob 臂**用全局事件总线订阅 `TopicCustomerMessageReceived`（判据依据：
`triggerSalesEngine` 的第一句是 `agent_runtime.PublishCustomerMessage`，位置在 `smartOrchestrator` / `salesEngine`
两处判空**之前** ⇒「总线收到一条」等价于「真进过那个触发分支」，不必往用例里塞真的推理链；
中台那条路不调这个函数，两臂计数因此互不串味）。

| 臂 | 断言（行号） | 钉住什么失误 |
|---|---|---|
| 中台 | `tr.called == 1` 且 `channel=="qq"` / `event_id=="qq_evt_…"`（:133、:136） | 有人把守卫扩成"QQ 连 Ingress 也不触发" ⇒ 消息没人回 |
| handleJob / QQ | 该臂增量 `== 0`（:143） | 摘掉 `channel != ChannelQQ` 半句 ⇒ 同一条消息两份回复（本审计批A / #6 / #13 同款事故） |
| handleJob / 非 QQ | 抖音一条增量 `== 1`（:175） | 整段触发被删掉或条件写反 ⇒ QQ 照样有回复、其余渠道**一条都没有** |

**前置 1（K-m1 第一版实测逼出来的）**：`shouldTriggerAI` 第一句是 `if s.salesEngine == nil { return false }`。
不注入销售引擎，`triggerAI` 恒 false，守卫那半句删掉也全绿 —— 我的**第一版用例就是这么活下来的**
（`.tmp_files/mut-evidence-2026-09-20/batchK/batchK-Km1-red.log`：变异体 rc=0）。修法不是"再等等看"，而是把前置本身钉成断言：
用例先 `ws.SetSalesEngine(...)`，再 `if !ws.shouldTriggerAI(ctx, ChannelQQ, "1") { t.Fatal(前置不成立…) }`（:91-92）。
（为什么这一格此前一直空着：全仓 `SetSalesEngine` 只有两处使用，另一处 `channel_fullchain_e2e_test.go:508`
注入完只**直调** `shouldTriggerAI`、从不进 `handleJob` ⇒ 引擎注入了也到不了守卫那行，两件事各自缺一半。）
**前置 2**：注入引擎后变异态会真进 `triggerSalesEngine`，用例因此把事件正文留空，
引擎在它自己第一道校验（`sales_engine.go:185` `user_message is empty`）就地返回错误 —— 变异跑不会去敲真实 LLM
（三刀红日志里那句 `sales engine error: user_message is empty` 就是这条在自证）。
另两条防哑的腿：正例先跑（直接调 `triggerSalesEngine`，总线必须记到 1 次，否则 `t.Fatal("计数器是哑的")`，:105），
到达见证 `unified_messages > 0`（:129）—— 教训记法：**见证必须落在判据的前置之后**，
第一版只靠它当"走到了守卫"的证据，而 `dispatchToUnified` 在守卫**之前**，中间还隔着 `shouldTriggerAI`，不足以证明那半句被评估过。

**三刀反向测试（各打死一条臂，红因互不相同 ⇒ 三臂各自有主）**：

| 刀 | 变异 | 预期 | 实测 |
|---|---|---|---|
| K-m1 | `webhook.go:973` 摘掉 `&& channel != ChannelQQ` | 红在 handleJob/QQ 臂 | killed，`…_test.go:143`（"多打 1 次"）|
| K-m2 | `webhook_channel_qq.go:89` 给 QQ 的 `e.Ingress(…)` 加上 `WithChannelOwnedAITrigger(ctx)`（＝把中台那一臂也关掉） | 红在中台臂 | killed，`…_test.go:133`（"期望恰好 1 次，实际 0 次"）|
| K-m3 | `webhook.go:973` 整块短路（`if false && triggerAI && …`） | 红在非 QQ 臂 | killed，`…_test.go:175`（"实际 0 次"）|

三刀跑完各自 `cp` 还原：`webhook.go` 回到 `3cb01f02…`、`webhook_channel_qq.go` 回到 `efb4149c1019525190e786325fdf72be`，
还原后同一条用例复跑 `rc=0`。K-m1 与 K-m3 打在**同一行源码**却红在**不同断言**（143 vs 175），
这正是"一处符号多处消费要逐格拆刀"的口径：只补 QQ 那一臂的话，K-m3 那种"整段不触发"的失误会静默通过。

### 19.3 同族死代码一刀：四处删除、一处刻意不清

| 处 | 删掉的东西 | 依据（"未使用"是证出来的，不是 grep 出来的） |
|---|---|---|
| 1 | `webhook_channel_qq.go:146 triggerQQSalesEngine`（自带 `//nolint:unused //// 仅被 *_test.go 引用`） | 全仓 `grep -rn triggerQQSalesEngine` 删后**零命中**（删前唯一非测试命中就是 `webhook.go` 里那句注释与函数自身），且 K-m1/K-m3 两刀已把真路径的判据搬到 `handleJob` |
| 2 | `webhook_channel_qq_test.go:259 TestQQ_TriggerSalesEngineGuard`（§17.7 结果 D 那条零断言用例） | 它调的正是处 1；删后 `webhook_channel_qq_test.go` 的 `"bytes"` 导入仅剩它一处使用（`_ = bytes.MinRead`），连带清导入 |
| 3 | `inbox_ingress_ingest.go:153 contentHashOf`（§17.8 末节登记的同族，自带同款 `//nolint:unused`） | 职责已由 `ContentHashWithSender`（`webhook_dedup.go:131`）承担，生产 3 处调用点逐处看过：`inbox_ingress_persist.go:76`、`inbox_ingress_ingest.go:130`、`webhook_outbound.go:684`；其替代者自己的用例在 `webhook_hash_test.go:47` 保留未动 |
| 4 | `inbox_ingress_dedup_test.go` 的 `t.Run("场景8_内容hash计算验证", …)` | 它是处 3 的唯一消费者；**只删这一格**，同文件其余七场景原样保留（备份 `/tmp/km-dedup.bak`） |

导入残留逐处验证：删 `"crypto/sha256"` / `"encoding/hex"` 前先 `grep -n "sha256\.\|hex\."` 证零命中（rc=1），
`gofmt -l` 空、`go vet ./internal/service/` rc=0。
（行号漂移备忘，防后来人按 §17.8 原文找不到：删掉那两行导入使 `inbox_ingress_ingest.go` 整体前移 2 行，
§17.8 写的"`:132` 的 `ContentHashWithSender`"现在是 `:130`。）

**一处刻意不清（对 §17.7 处置① 的偏离，理由如下）**：那一句写的是"函数、它的零断言用例、
`webhook.go:968` 那句指代它的注释**一起清掉**"。读原文才看清那条注释是

```go
// QQ 渠道 AI 触发已由 dispatchQQ → Ingress（aiTrigger=webhookSvc.TriggerInboundAI）
// 完成，这里不再走 triggerSalesEngine，避免同一事件双触发 AI（双重回复）。
```

它指代的是**活函数 `triggerSalesEngine`**（`webhook_ai.go:133`），不是被删的 `triggerQQSalesEngine`；
它解释的正是 K-m1 那一臂"为什么这里不触发"。删掉它等于把守卫的**为什么**一起删了，
而这条守卫恰恰是最容易被后来人"顺手统一一下"的地方 ⇒ **保留**。
处置① 里"注释一起清"这一项按实测不成立，登记于此而非静默偏离。
（顺带：K-m3 那刀的变异体 `if false && triggerAI …` 会让这段注释整块失去对应实现 —— 用例红，见 §19.2 表第三行。）

### 19.4 门禁（两趟 `internal/service` 整包，无 `-run`）

| 趟 | 命令 | 结果 |
|---|---|---|
| round1（`gates/batchK-service-round1.log` + `.rc` + `.meta`） | `go test -p 1 -count=1 -timeout 2500s ./internal/service/` | **`rc=0`，435.997s**；`--- FAIL` / `FAIL` / `panic` 标记 **0** 行 |
| round2（`gates/batchK-service-round2.log` + `.rc`） | 同命令复跑 | **`rc=0`，430.235s**；三类标记 **0** 行 |

**为什么要跑第二趟**：本批那条用例带一个 300 ms 排空窗与两个 2 s 轮询窗（§19.2），
"绿一次"在共享机上可能只是运气 ⇒ 复跑一趟把窗口口径的绿钉成两趟一致。
两趟之前各自等另一条泳道的 service 门退出（等待器 `.tmp_files/wait-then-gate-batchk*.sh`，
round1 `waited=10s`、开跑时 `concurrent_service_gate=0`、load `{6.16 6.44 7.50}`）——
同一个包并行跑两趟会在共享 Redis 与自增 id 上互相串味，那条红既不是我的、也证不了不是我的。

静态门：`gofmt -l` 对四处改动文件全空、`go vet ./internal/service/` rc=0（变异还原后各测一次）。
`go test -list '.*' ./internal/service/` 现报 **3,721** 条声明用例（§18.7 当时 3,712；增量里含本批 1 条，
其余属并行泳道，不做逐条归因）⇒ 上面两趟是**整包**，不是子集。
中间检查用的 `-run 'TestQQ_|TestBatchK_|TestInbox|…HandleJob'` 子集（`PASS=56 FAIL=0 SKIP=0`）
**只作过程证据、不算门**（见 `batchK/batchK-family-targeted.log` 与其 MANIFEST 读法）。

### 19.5 本批未覆盖面（不得当作已验证）

1. **"非 QQ 恰好一次"那一臂只铺了抖音一家。** 选它的理由是它走 `shouldTriggerAI` 的 `default` 分支
   （无账号级 AI 开关，`webhook_ai.go:114-118`），不碰任何家账号表；代价是企微 / 飞书 / WhatsApp
   各自的 `handleJob → triggerSalesEngine` **端到端臂没有独立用例**。
   它们依赖的"账号 AI 开关是否放行"那一层由 `channel_fullchain_e2e_test.go:522-536` 那张表钉着，
   但那是**函数级**判据、不是 handleJob 级 ⇒ 若有人在这几家渠道的 dispatch 里补了中台触发、
   又忘了给 `webhook.go:973` 加例外，本批用例拦不住（QQ 正是踩过这一格）。
   闭合口径：按 K-m3 同形给每家各下一刀（"整段不触发"必须让那一家红），归到下一次整树电池批次。
2. **`tgExtra.GateHandled` 那一臂仍零用例**（`webhook.go:970-971`：/start 网关已消费 ⇒ 不再触发销售）。
   全仓 `grep -rn GateHandled` 在 `*_test.go` 里零命中；`telegram_gate_test.go` 测的是网关本身
   （令牌归属、过期拒绝、放行与回滚），不测"消费后不再触发"。它与本批三臂同属一个 `triggerAI` 决策块，
   漏这一格的后果是"客户刚验证完就收到一条推销"。**归批D（TG 真机端到端）就地覆盖**：
   那条链路本来就要跑 /start → 放行 → 再发消息，加一句"放行后第一条普通消息触发一次、/start 那条不触发"即可，
   不必在离线用例里伪造网关令牌。
3. **中台臂断言的是替身被调，不是真回复。** `fakeAITrigger` 只记参数，`runAIGeneration → sendOutbound`
   那一段（含出站失败改投、AI 排他锁 `ai_processing`）不在本用例覆盖面内；QQ 的 `ai_processing`
   串行锁与 debounce 组合也仍按 §17.3 的口径记为未覆盖面。
4. **§19.3 的四处删除只证"编译与既有门不变"，不证"运行时没人反射/DI 拿到它们"。** 判据是
   `grep` 全仓零命中 + `go vet` rc=0 + service 整包门（§19.4）；本仓无字符串反射注册、无 `fx`/`wire`
   之类按名注入（已按 §16 之前的同一口径核过两次），因此沿用该结论，未再单独探针。

---

## 20. 批I：N-25 死媒体注册表删除，以及"删完才暴露的活路径零用例"（2026-09-21）

### 20.1 这一刀凭什么能下（取证复跑，含机器核对面）

1. **全仓 grep（含非 Go 文件）**：五件套只在定义文件 `channel_media.go` 命中；其余命中全在
   `CHANGELOG.md`、`docs/architecture/*`、`scripts/audit-loop/STATE.md` 这类**历史叙述**里。
2. **`git log --all -S`（三个名字各一次）**：只有 `a200aa65`（2026-09-08，引入整套半成品）与
   `59aa223d`（2026-09-08，只给死路径的 map 加锁）两个提交碰过它 ⇒ **从未有过调用方**，
   不是"曾经有、后来被删"。这条区分只能由 `-S` 给，`grep` 给不了。
3. **动态面排除**：包在 `internal/` 下，`hivemtk-user` 之外的模块不可能 import；仓内无 `fx`/`wire`
   按名注入；`MethodByName` 只有 `model/kuaishou_card_test.go` 查 `TableName` 一处。
4. **本批多加的一问：门禁机器有没有引用它。** `grep -rln` 在 `scripts/` `.github/` 下只指到
   `scripts/audit-loop/{STATE.md,state.json}` —— 那是上一轮永动循环写的人类读物，
   CI 里零引用（`grep -rn audit-loop .github/` 零命中）⇒ 永动循环 R5 那条
   "`channelMediaFollowersMu` 在位"的核对口径随路径一起退休。**不改历史报告**（STATE.md 是流水账），
   只在此登记：那把 `sync.RWMutex` 当初就加在了死路径上。

### 20.2 删除清单：五件套 + 级联出的第六处

| 符号 | 原位置 | 判据 |
|---|---|---|
| `channelMediaFollower`（type） | :31-32 | 只被下面四件用 |
| `channelMediaFollowersMu` + `channelMediaFollowers`（var） | :34-39 | 写口只有 `RegisterChannelMediaFollower`，读口只有 `fetchChannelMediaFollower` |
| `RegisterChannelMediaFollower` | :41-48 | 全仓零调用（含装配层 `cmd/`、`router/`） |
| `fetchChannelMediaFollower` | :57-63 | 唯一调用点在被删的 `PersistChannelMedia` 内 |
| `PersistChannelMedia`（导出） | :65-92 | 全仓零调用；两个 `-S` 提交都只碰定义 |
| **`detectContentType`（级联）** | :164-176 | 它**唯一**的调用点在 `PersistChannelMedia` 里 ⇒ 前五件一删它即死 |

**级联这一条是本批最容易漏的形状**：删死代码会**产出新的死代码**，所以每删一件都要把全仓 grep
重跑一遍，而不是按清单删完就走。
保留两件容易被误判成"一起删"的：`mediaPersistRecord`（活路径 `storeInboundMedia` 的返回类型）、
`readInboundMedia`（六家入站媒体在用的批F-4f 腿）。
import fallout：`sync` 随锁摘；`time`、`strings` 随 §20.5 两处改动摘；`mime`、`net/http`、`io`、
`os`、`path/filepath` 仍有消费方 —— 这一格不由人推断，由 `go build` 的 "imported and not used" 强制核对。
**顺带收小的安全面**：`PersistChannelMedia` 是这套里唯一的导出入口，且把调用方给的任意 `channel`
原样拼进落盘目录 `channels/<channel>/…`。删掉之后 `storeInboundMedia` 的 channel 实参只剩仓内八个
渠道常量 ⇒ 落盘目录不再有外部可控输入面。

### 20.3 删除动作的反向探针，逮到比被删物更大的洞

按规矩给"删除没有伤到活路径"补一刀探针：把 `channelMediaPersist` 整段短路成
`return "", nil`（等价于"转存静默不干活，上层保留占位符"）→
`-run 'Media|Backfill'` **28 条用例全绿**（`batchI/batchI-I1-survived.log`）。

根因很具体：八家渠道的媒体用例**全部**把 `xxMediaStoreFn` 这条缝换成替身（只记参数、不落盘，
例 `webhook_batchf4_m01_telegram_test.go:42`、`webhook_batchf2_n10_media_test.go:54`），
于是装配语句 `xxMediaStoreFn = channelMediaPersist` 的右半边那条真链路 —— obs 配置查询、
驱动选择、目录与文件名组装、失败上报 —— **一条断言都没挨过**。

⇒ "删的是死的所以不影响"这句话在本批**不成立**：死代码和一条从没被测过的活路径住在同一个文件里，
删除动作把后者的零覆盖照了出来。真正交付的是 §20.4 那五条腿，删除只是引出它的探针。

### 20.4 补的腿与八刀电池

新文件 `internal/service/webhook_batchi_media_store_test.go`（5 顶层用例 + 6 子用例）。
⚠️ 用例会写包级全局 DB 句柄（`db.SetTestDB`，因为 `storeInboundMedia` 经 `dbForStorage` 读全局），
每条都在 cleanup 里成对还原，见 [[platform-test-global-db-handle-leak]]。

| 腿 | 钉住的那一格 |
|---|---|
| A `…WritesUnderChannelFolder` | 兜底存储：URL 前缀、目录层级、落盘字节相等、扩展名由 contentType 推出，外加 `mediaPersistRecord.MimeType/Size` |
| B `…UsesConfiguredDefaultStorage` | `is_default` 命中时按记录的 `Endpoint`/`Domain` 走，且**兜底目录必须零写入**（防止"两边都写"）|
| C `…ReportsUploadFailure` | 两条失败臂各一子例：云驱动未接通 → `upload media`，provider 不认识 → `storage factory`；都要求 `url==""` 且 `err!=nil` |
| D `…ExtFromContentType` | 四行**单值**映射（`.png`/`.pdf`/`.bin`/`.bin`），不写容差（口径见批L 第 15 条）|
| E `…FilenameHintOutranksMediaType` | `filenameHint` 带扩展名时压过 contentType：语音 `.amr` 不能被写成 `.ogg` |

八刀（驱动脚本 `.tmp_files/mut-evidence-2026-09-20/batchI/battery-batchI.py`，日志
`battery-r3-m{1..8}.log` + `battery-r3-ctrl.log`；r1/r2 保留失败现场不覆盖）：
控制组 **`ran=5/5(-list) skip=0 rc=0`**，**8/8 KILLED，且每刀红集合恰等于期望集合**（上下界都核）。

| 刀 | 注码（完整语法块） | 红集合 |
|---|---|---|
| m1 摘掉整段转存 | `if true { return "", nil }` 置于 `storeInboundMedia` 之前 | A B C E |
| m2 日期目录回到调用方 | `folder :=` 重新拼上 `"2026","01"` | A |
| m3 忽略查到的默认配置 | `if err != nil \|\| cfg == nil {` → `if true {` | B C |
| m4 上传失败被吞 | `return nil, fmt.Errorf("upload media: %w", err)` → `return &mediaPersistRecord{}, nil` | C |
| m5 不再反推扩展名 | `extFromContentType` 体替成 `_ = mime.ExtensionsByType; _ = ct; return ".bin"` | A D |
| m6 记录里丢类型与大小 | 只填 `PublicURL` | A |
| m7 忽略 `filenameHint` | `if filename == "" {` → `if true {` | E |
| m8 驱动选择错误被吞 | `return nil, fmt.Errorf("storage factory: %w", err)` → 空记录 + nil | C |

**一次自我纠偏（首轮 BROKEN）**：m5 的替换文本以 `".bin` 结尾，与 Python 三引号相邻 ⇒
注码后的 Go 少一个收尾引号，`channel_media.go:117:14: newline in string`、`ran=0`，被脚本正确记成
BROKEN（`batchI/battery-m5.log` —— 首轮日志还没有轮次前缀，"按轮分文件"正是这一轮的产出，
所以现场文件名是 `battery-*` 而非 `battery-r1-*`，`battery-r2/r3-*` 才是新口径）。
按口径第 19 条把该 `new` 改成**转义单行串**、日志按轮分文件
（`BATTERY_ROUND` 环境变量），r3 才是收口证据。**变异失效就修变异，没动任何 expect。**

**等价类（不补刀，写清理由）**：`strings.NewReader(string(data))` 改 `bytes.NewReader(data)`
对驱动可观测差异为零（同字节、同 `int64(len)`）⇒ 无变异可杀；A 腿的"落盘字节与入参相等"断言
正是这一格的观察面，写法变了它照绿才说明改对了。

### 20.5 腿跑出来的两个真缺陷

1. **日期目录被拼两次（A 腿第一次跑就红，行 75）**：`channels/qq` 之后应是 3 段
   （yyyy/mm/文件名），实得 5 段 `["2026","09","2026","09","<uuid>.png"]`。
   根因：`storeInboundMedia` 把 `time.Now().Format("2006/01")` 拼进 `folder`，而本地驱动按
   自家契约 `{baseDir}/{folder}/{yyyy}/{mm}/{uuid}.{ext}`（`internal/storage/local.go` 文件头）又补一次。
   危害面在运维：按 `channels/<渠道>/<年>/<月>` 归档或清理的脚本找不到文件。
   **修法取调用方** —— 契约有文档的一侧是驱动，folder 只给 `channels/<渠道>`。
   前滚式变更：已落库的旧行 URL 原样可用（文件仍在旧层级，`/files` 守卫按整条相对路径解析），新行不再叠层。
   全仓 `grep '"channels'` 证实**没有任何读侧解析这个前缀**，改形不伤反序列化。
2. **`extFromContentType` 的 `switch` 两臂同值**（`case image/ → ".bin"` 与 `default → ".bin"`）：
   两臂可观测差异为零 ⇒ 要么臂写错、要么整臂多余。按"死臂收掉"处理，真实映射交给 D 腿钉住，
   防的是后来人"顺手给 image 特殊处理"时无人拦。

### 20.6 门禁

静态：`gofmt -l internal/service/` 空、`go vet ./internal/service/` rc=0、`go build ./...` rc=0、
**`go vet ./...` rc=0 且零输出**。最后一条是本批的关键证据 —— `go vet` 会连每个包的**测试文件**
一起类型检查 ⇒ "全仓没有任何测试引用被删符号"是**跑出来的**，不是 grep 出来的。

整包门（无 `-run`，`gates/batchI-service-round1.log|.rc|.meta`，md5 `8160a8f858384fec9a40a567549bec1e`；
基线 HEAD `11755c55`，门 01:22:14 开跑、01:29:35 收口。另一泳道 01:38:33 才提交 `292c92e3`（opportunity 八条端点，
`+2228` 行、含 `internal/service/opportunity_test.go`）⇒ **在门结束之后、且不碰本批三个路径**，
`PASS=3722` 按 `11755c55` 读，复跑者会数到更大的数，那不是本批掉了腿）：

```
go test -p 1 -count=1 -timeout 2500s -test.v ./internal/service/
rc=0  ok  hivemtk-user/internal/service  436.293s
顶层 PASS=3722 FAIL=0 SKIP=3 ；子用例 PASS=858 FAIL=0 SKIP=0
```

三条 SKIP 与本批无关且是结构性的：`TestPlatformAccountService_Login` 在源码里是**无条件 `t.Skip`**
（需 chromedp 真浏览器），`TestAIAgent_AssetBundleBinding` / `TestAIAgent_FullChain` 走 `login()`，
需要本机起着 user-server 且口令可用（本机回"无效的请求参数"即跳）。
**这属于门的环境前提缺口，不是本批引入**（记入 [[gate-scope-blind-spots]] 的"环境前提"一格）。

本批五条腿在整包门内同样全 PASS（log 行 17246–17267）⇒ 写全局句柄没串到同包后续用例。
电池与门不并跑同一 package（等待器 `.tmp_files/wait-then-gate-batchi.sh`，开跑时
`concurrent_service_gate=0`，load `{12.79 12.69 10.38}`）。

**门里"panic"字样的逐行归因（免得下一轮当崩溃追）**：`^panic:` 起始行 **0 行**（真崩溃为 0，
rc=0 与 `--- FAIL` 0 行同向）；含 "panic" 子串的行共 **8** 行，每一行都属于一条**故意喂 nil/boom
并且自己 PASS** 的用例：`[D-6] 告警任务 panic key=panic_probe err=boom`（log:259，属
`TestAlertDispatcher_ConcurrentExec`，上一行 `--- PASS`）、`backup_test.go:797/820` 的
"Expected panic when repository is nil"（测试自己 `t.Log` 出的 recover 结果）、
`[JourneySleepCron] 沉睡检测 panic: runtime error: invalid memory address…`（log:1837，由
`TestJourneySleepCron_RunOnce_PanicRecovered`（`customer_journey_test.go:141`，喂
`NewJourneySleepCron(nil)`）打印，批J round2 的门里同样有这一行、同样 1 次）、
`[order-draft] 意向提取 panic 已 recover`（log:11448–11449）、
`xhs_2268_fix_test.go:56/121` 的"未 panic / 不 panic"正例断言。
⚠️ 这里第一版写的是"门里唯一一行 panic"，重数后**不成立**（8 行）⇒ 改成逐行归因；
"唯一"只对 JourneySleepCron 那一行成立（全门 1 次）。

### 20.7 本批未覆盖面（不得当作已验证）

1. **兜底 `baseDir` 缺省臂（`STORAGE_LOCAL_BASE_DIR` 未设 → `./uploads`）零用例**：测它要在
   `user-server/uploads/` 真建目录、污染工作树，代价大于收益。同一字面量在
   `storage.newLocalFromConfig`、`storage.NewLocalDriver`、`service.storeInboundMedia` 各写一次
   （三处兜底），将来若收口成一处，这一臂自然有腿。
2. **八家"替身 → 真实现"那一层仍靠静态锁**：A–E 钉的是 `channelMediaPersist` 之后；
   装配语句 `xxMediaStoreFn = channelMediaPersist` 那八行"指针指对没有"由 §17/§19 那类读源码的锁管。
3. **`GetDefault` 只看 `is_default`、不看 `status`**（`repository/obs_config.go:84` 为**修复前**坐标；批M 已改成 `is_default AND status = active`，现位 `:142-148`）⇒ 一台被置为
   `inactive`/`error` 的存储若仍带 `is_default`，媒体转存照样往它上面投。本批**未改**：
   `GetDefault` 是全站上传的共用入口（controller 上传、素材库、obs 配置页都在读），
   改它的判据属跨调用方行为变更，须与告警面/配置面一起看。已登记为 N-26 与独立任务。
4. **云驱动 SDK 本身未接通**（`storage.cloudStub` 恒返回 "SDK not wired yet"）：全仓既有事实，
   本批只是把"它失败时必须上报"这条钉住了。

---

## 21. 批H：N-22 钉钉入站会话/身份面（2026-09-21，A 档取证把这一条的**半个前提**推翻）

> 编号说明：H 排在 I（§20）之后，因为本批的**结论**依赖先看清"入站链路上谁在消费会话面"，
> 而那幅图是批I 删完死注册表之后才干净的。标签按字母、章节按完工顺序，两处不一致登记于此。

### 21.1 先记被推翻的那半：@判定门**不成立**（A 档，真实渲染页）

| 取证项 | 值 |
|---|---|
| 访问 URL | `https://open.dingtalk.com/document/orgapp/receive-message` |
| 重定向到 | `https://open.dingtalk.com/document/development/robot-message-type` |
| 页面标题 | 消息发送与接收类型 - 钉钉开放平台 |
| 正文规模 | innerText **17,156 字符** / **1,253 非空行** / 关键词命中 **19 行** |
| 取证时间 | 2026-09-21 02:03:55 +0800（脚本内 UTC `2026-09-20T18:03:55.474Z`） |
| 方式 | 本地 Chrome 真实渲染页，`evaluate_script` 取 `document.body.innerText`（需 `pageId`） |
| 落盘 | `.tmp_files/mut-evidence-2026-09-20/batchH/dt-robot-receive-message-official.txt`，5,305 字节，md5 `456096886109b4956e2cab78f7970900` |

触发条件逐字（正文行 1656）：

> 当用户@群机器人或与机器人发送单聊消息时，钉钉会把机器人接收到的消息发送到开发者设置的机器人回调服务。

⇒ **进入 `DingTalkAppService.ReceiveMessage` 的群消息必然已经 @ 本机器人**（单聊则直接回调）。
N-22 原文里"`@机器人`判定缺失 ⇒ 群里误回/漏回 ⇒ 需要补一道判定门"这半句**前提不成立**，
中台再实现一道 @门是**空转**（更糟：它有了"看起来在拦"的外观，而真实拦阻条件是平台给的）。
已在 §5 的 N-22 行加 ⚠ 注记，**不划原文**（同一行另有半边前提成立：`conversationType`/`isAdmin` 确实零读取）。

顺带把两家的差别说清，防止后来人"照 TG 抄一道门"：TG 的入站是 `getUpdates` 全量转发，
仓库里以 `mentioned` / `tgExtra.GateHandled` / `newOpp` 三门**自筛**（`webhook.go:970-998`）；
钉钉是**平台侧已经筛过**。形态不同 ⇒ 门只属于前者。

另一条**不是我们的缺陷**的口径（正文行 217 / 2225 / 2311 / 2397，逐字在证据文件 §2）：

> 群聊会话中，群成员 @机器人时，机器人不支持接收语音/视频/文件消息。

⇒ 群里只有 `picture` 需要媒体分支，`audio`/`video`/`file` 官方就不下发。批F-4c 的钉钉媒体半场此前按
"四类都要落群分支"设计，本批据官方把这一格**判否**（不新增代码，只在 §21.7-3 登记未钉成用例）。

### 21.2 落地清单：读什么、写去哪儿、为什么不读另一批

官方参数表整表读过后（证据文件 §4，正文行 1706-1975），本批**只落有下游判据的面**：

| 官方字段 | 官方口径（表内逐字摘） | 落在哪（`dingtalk_app.go` 行号） | 理由 |
|---|---|---|---|
| `conversationType` | String，1=单聊 2=群聊 | `:156` `isGroup := msg.ConversationType == "2"`；原值另存 `Extra["conversation_type"]`（`:178`） | 群/单聊分岔的**唯一**来源；原值留一份是因为 `IsGroup` 把"没下发"和"1"压成同一个 `false`，排障要能分辨是平台没升应用还是我们读错 |
| `conversationId` | String，会话ID | 群时 `event.GroupID = msg.ConversationID`（`:184`） | 官方**没有**独立群 id 字段；`GroupID` 非空才能让 AI 侧把"一个群"收敛成"一个会话"（`smart_cs_orchestrator.go` 的 `group:<GroupID‖SenderID>`），与飞书 `GroupID=ChatID` 同口径 |
| `conversationTitle` | String，群聊时才有的会话标题 | 非空才写 `Extra["group_name"]`（`:185-187`） | 群名唯一的来源；空串不写，免得覆盖下游已有的展示名 |
| `senderNick` | String，发送者昵称 | `event.SenderName`（`:162`） | 此前恒空 ⇒ 中台自回环识别里 `sender_name` 判据永远命中 `''`/NULL 那一支 |
| `isAdmin` / `isInAtList` | Boolean，「机器人发布上线后生效，否则不返回」 | 三态，`Set` 才写 `Extra["is_admin"]` / `["is_in_at_list"]`（`:189-194`） | 官方明写**可能不返回** ⇒ 缺席不能当 false，否则 ops 看到 `is_admin=false` 会误判"非管理员"而不是"应用未升级" |

**刻意不读的六个字段**（不是漏了，是不落判据）：`atUsers`（官方语义是**被@的人**，唯一可能的消费者是
"被@的是不是本机器人"，而 §21.1 已证该判据恒真）、`chatbotUserId`（只为解释 `atUsers` 而存在）、
`chatbotCorpId` / `senderCorpId` / `senderUnionId` / `senderPlatform`（全仓无任何下游判据）。
`senderUnionId` 那一格另有来历：身份归一函数本身没接线，已单独登记为 **N-27**，本批不借道它、
也不在渠道契约批里顺手做那个产品决策。

**类型面**新增两个宽松标量（`:275-307`，与批前已有的 `dingTalkFlexInt` 并列）：

```go
type dingTalkFlexString string                    // 数字/字符串两种形态都收
type dingTalkFlexBool struct{ Set, Val bool }     // 未下发 / false / true 三者可分辨
```

理由是同一课教训的第三次付账：官方表里 `conversationType` 类型栏写 String，而**同一份文档**的
`createAt` 也写 String、真实回调却给数字 ⇒ 整包 `Unmarshal` 是原子的，一个标量形态不符就让
整条客户消息 400 丢掉且平台**不重投**。
但两枚旗标的**失败方向刻意不同**：`dingTalkFlexInt` 认不出形态会报错（它供 `Timestamp`，判错会让
补触发窗口整个错位），`dingTalkFlexBool` 认不出时**不报错、只置 `Set=false`** ——
它今日无任何判定消费，为一枚旁证字段把客户消息沉掉是反向的取舍。这一格由腿⑥ + H-m11 双向钉住。

### 21.3 夹具里 `atUsers` 的语义是反的（一处会邀请后来人建假门的注释）

`dtRobotPlainMsgJSON` 原写 `"atUsers":[{"dingtalkId":"bot"}]` —— 把**机器人自己**放进"被@的人"名单，
而官方示例是 `{"dingtalkId":"xxx","staffId":"xxx","unionId":"edxxx34"}`（机器人自身的 id 在 `chatbotUserId`）。
危害不是"测了个假形状"，而是**它恰好看起来在证明"@列表里有 bot ⇒ 该回"**，
下一轮读到夹具的人有较大概率据此建一道恒真门。
本批按官方形状改写（三键齐、`staffId` 与 `senderStaffId` 同值），并补 `conversationTitle`；
改写时保留 `dtLiveRobotMsg` 的两处 `Replacer` 锚点（`"conversationId":"cid-77"`、`"createAt":1700000000000`），
否则同文件另五条既有腿会静默失去夹具。

### 21.4 影响面逐条走完（九条，每条给判据）

| # | 面 | 结论 | 判据 |
|---|---|---|---|
| 1 | 身份归一函数 | 不参与，本批直落 `IsGroup`/`GroupID` | `NormalizeCustomer*FromMessageHub`（`pkg/messageid/identity.go:12-43`）零生产调用方 ⇒ 已记 N-27 |
| 2 | 正文窗口去重 | 行为不漂 | `Extra["channel_msg_id"]` 恒有值（`:173`）⇒ `interceptInbound` 的内容窗口那支本就不启用（`inbox_ingress_ingest.go:129`） |
| 3 | 自回环识别 | 第二支由"恒可能命中"变"按昵称命中"，另两支不变 | `interceptInbound` 三支里只有第 2 支看 `sender_name`（`inbox_ingress_ingest.go:106`），且该谓词是**空串时整个不加**（`message_hub_inbox_outbound.go:170-172`）⇒ 钉钉此前传 `""`＝不判昵称、任何同正文出站都算回环；现在传客户昵称 ⇒ 出站行昵称是 `"AI 助手"`/`"系统"`（`smart_cs_orchestrator.go:785,838`）故不再命中 ⇒ **这一支变松**。真回环仍由第 1 支（平台 msg_id 精确匹配，钉钉 `channel_msg_id` 恒有值）与第 3 支（2h/20 条近期出站做去空白全文比对，不看昵称）拦住，两支各有既有用例（`inbox_ingress_echo_exact_test.go` / `inbox_ingress_recent_echo_test.go`）。⚠ 顺带看清一件既有事实（本批未改）：第 3 支的判据是"逐字复述近期任一条出站 ⇒ 拦"，且 `normalizeEchoText` 只去空白、无最短长度门槛 ⇒ 客户复读机器人上一句会被当回环丢弃；这是该设计的**刻意取舍**（有测试钉着），不是本批引入，若要复议应在回环设计那条线上提，不属渠道契约 |
| 4 | 会话收敛 | 群内不同发言人由"各开一会话"变"共用 `group:<cid>`" | `findOrCreateSession` 的 `in.IsGroup` 支；与飞书/QQ/TG 同形 ⇒ 这是**纠正**，但见 §21.7-5 的存量面 |
| 5 | 线索侧 | hub 行带 `is_group`/`group_id`，与其他渠道一致 | `leadMiningSvc.Enqueue` 读 hub 行，不读新引入的键 |
| 6 | AI 触发语义 | **完全未动** | 全仓钉钉链路无任何群门；H-m12（假想门）那一刀实测让腿④⑦红 ⇒ "加门"才是行为变更，本批没加 |
| 7 | 出站 | 未动 | 单聊与群聊都走 `sessionWebhook`，出站侧不读 `GroupID` ⇒ 本批不影响发送路径与 §3.5 的出站行 |
| 8 | 群沉默 / 复活 | **不会**被新激活 | `group_silence.go` 整模块六个符号（`DetectGroupSilence`/`BuildReviveVerdict`/`EmitGroupRevive` + 三常量，含字面量 `group_revive`）全仓 `*.go`/`*.md`/`*.sh`/`*.yaml`/`*.json` 除自身与其测试外**零命中** ⇒ 已记 N-28 |
| 9 | 媒体分支 | 不扩面 | 群内 `audio`/`video`/`file` 官方不下发（§21.1）⇒ 批F-4c 的媒体半场无需为群另开分支 |

### 21.5 七条腿 × 十二刀，其中一刀的**期望值**被实测订正

新用例：`internal/service/webhook_batchh_dingtalk_group_test.go`（295 行，7 顶层腿 + 4 helper）。
入口是 `ReceiveMessage` 本体（验签走包内既有的 `dtRobotHeaders`），计分打在**落库那一行**
（`batchHReadHub` 用独立零值 struct 读 `message_hub`，不复用填充过的 struct）与 AI 替身计数，
夹具每次以 nonce `cid-h-<nanos>` / `m-h-<nanos>` 起会话，避免与并行泳道在同一份进程内 Redis 上串味。

| 腿 | 钉住什么 |
|---|---|
| ① GroupCallbackLandsGroupFaces | 群面全落：`is_group` / `group_id` / `Extra.group_name` / `SenderName` / 两枚旗标 |
| ② SingleChatHasNoGroupFaces | `conversationType="1"` 时**不得**有 `group_id`、`group_name`（防"顺手把 cid 当群名"） |
| ③ AbsentOptionalFlagsStayAbsent | 字段不下发 ⇒ `Extra` 里**没有**这两枚键（不是 `false`） |
| ④ ExplicitFalseLandsAsFalse | 下发 `false` ⇒ 必须写 `false`（防三态退化成"只记 true"） |
| ⑤ NumericConversationTypeStillGroup | 官方文档写 String、真实回调给数字时仍判成群（`json.Number("2")`） |
| ⑥ UnknownBoolFormDoesNotSinkMessage | 认不出的布尔形态**不丢**整条消息（腿⑥ + H-m11 双向钉住失败方向） |
| ⑦ GroupWithoutAtSignalStillTriggersAI | 群消息不带任何 @信号 也必须触发 AI —— 本批结论的行为锁 |

电池：`.tmp_files/mut-evidence-2026-09-20/batchH/battery-batchH.py`（md5 `9d79999c8bfec77791985d59e69a237c`，
FILE=`internal/service/dingtalk_app.go`，`-run TestBatchH_DingTalk`，控制组上界由
`go test -list` 现算，注码全为完整语法块，每刀 `finally` 还原并比 md5）。

**r1 十十一刀按期望红，H-m12 报 EXTRA**：首轮推演期望 {③,⑦}，实测 {④,⑦}。
读红因（`battery-r1-m12.log:14` `会话 … 应有一行 message_hub: record not found`、
`:25` `…无 @ 信号也必须触发 AI，called=0`）才看清是**期望欠规格**，不是变异失手：

- ③ 的夹具把 `conversationType` 一并删了 ⇒ `isGroup=false` ⇒ 那道假想门**根本不进**，③ 不可能红；
- ④ 是"群聊 + `isInAtList=false`"，即**群里普通成员 @机器人** ⇒ 早退 ⇒ 消息被丢。

后者恰恰是这道假想门真正的危害面 ⇒ **改期望、不改变异**（理由以注释留在脚本 `:71-74`），
整电池复跑 r2 ⇒ **12/12 KILLED**，且 r1 与 r2 的两轮日志（各 13 份）逐刀 red 集合**完全相同**
（唯一差异是脚本里的 expect），收口 `final_md5=ab353527a259e6de7e0cd1037b394f01` = 开工 BASE，
树里 `MUTANT-` 零残留。

| 刀 | 变异 | 红集合（r1=r2 实测） |
|---|---|---|
| H-m1 | `isGroup` 写死 false | ①⑤ |
| H-m2 | 判据写成 `!= "1"`（没下发也算群） | ③ |
| H-m3 | `senderNick` 不落地 | ①② |
| H-m4 | `event.GroupID = ""` | ① |
| H-m5 | `group_name` 拿会话 id 顶替标题 | ① |
| H-m6 | 三态退化成"没下发也写 false" | ③⑥ |
| H-m7 | `isAdmin` 整枚不存 | ①④ |
| H-m8 | `isInAtList` 整枚不存 | ①④ |
| H-m9 | 宽松字符串恒返回空串 | ①⑤ |
| H-m10 | 显式 `false` 当缺席 | ④ |
| H-m11 | 认不出的布尔形态上报错误（丢整条消息） | ⑥ |
| H-m12 | 补一道"群里没 @信号就不回"的门 | ④⑦（首轮推演为 ③⑦，见上） |

H-m1 与 H-m9 打在**不同符号**（判定行 / 类型 `UnmarshalJSON`）却红在**同一双腿**，这是"宽松类型 +
其唯一消费点"的正常重叠，不算一格两刀；反之 H-m7 与 H-m8 同形不同字段，各自只红在含该字段的腿 ⇒
逐刀可分辨。

### 21.6 门禁

静态：`go build ./...` rc=0、`go vet ./internal/service/` rc=0、`gofmt -l` 对本批触及文件空。

整包门（无 `-run`；脚本 `.tmp_files/run-gate-batchh.sh`，产物 `gates/batchH-service-round1.log|.rc|.meta`）：

```
gate start 02:27:19 HEAD=292c92e3 TZ=CST+0800 → stop 02:35:06
go test -p 1 -count=1 -timeout 2500s -test.v ./internal/service/
rc=0  ok  hivemtk-user/internal/service  462.791s
顶层 PASS=3729  FAIL=0  SKIP=3   batchH legs: 7
```

七条腿在整包门内逐条 `--- PASS`（0.08–0.10s），`--- FAIL` 计数 **0**。三条 SKIP 是 §20.6 已逐条归过因的
结构性环境跳（`TestPlatformAccountService_Login` 源码内无条件 `t.Skip` 需 chromedp；
`TestAIAgent_AssetBundleBinding` / `TestAIAgent_FullChain` 走 `login()` 需本机起着服务且口令可用），
非本批引入。电池与门**不并跑同一个包**：r2 开跑前另一泳道的 service 门已退出。

⚠ **给复算者的口径**：`git diff --stat user-server/internal/service/dingtalk_app.go` 报
`358 insertions(+), 43 deletions(-)`，那是该文件**跨批累积的未提交改动**（批A / 批B / 批F-4c / 批J 的同文件
前批都在这里，按纪律均未提交）。批H 自己的字节面是 struct 四字段（`:230`、`:241-244`）、
两个宽松类型（`:275-307`）、event build 的 `:156`、`:162`、`:166`、`:178`、`:181-194`
与新测试文件 295 行。

### 21.7 本批未覆盖面（不得当作已验证）

1. **A 档是"官方文档"，不是一条真实回调报文。** 数字/字符串形态漂移在真实下发里的分布**未知**：
   腿⑤⑥钉的是"任一形态都不塌"，不是"真实给的是哪种"。闭合要到真钉钉应用 + 真群里 @机器人
   抓一次回调（归入"待真实账号"清单，与 §21.1 的门结论同级证据要求）。
2. **`senderStaffId` 缺失那一格无新腿。** 官方明写它「非本企业成员时为空」「机器人发布上线后才生效」，
   既有三级兜底（`staffId → senderId → conversationId`，`:142-148`）本批未动 ⇒ 外部群外部成员的
   hub 行 `SenderID` 会落到加密 `senderId`；群面收敛后这只影响 hub 行归属与线索侧，不影响会话键，
   但没有用例钉这个形状。
3. **群媒体限制只做成了文档结论，没有用例。** 若要钉，判据应是"群里收到 `audio` 回调时不崩、
   按未知类型处理而不是静默丢正文"，属批F-4c 钉钉媒体半场的续。
4. **`atUsers` / `chatbotUserId` / `chatbotCorpId` / `senderCorpId` / `senderUnionId` / `senderPlatform`
   六字段仍不读。** 本批"恒真"结论的前提是**同一群里只有一个机器人**；若将来多机器人同群，
   需回头读 `chatbotUserId` 与 `atUsers` 配对来辨"被@的是不是本机器人"，届时 §21.1 的判否要重做。
5. **存量数据不回填。** 改前落库的钉钉群消息 hub 行仍是 `is_group=false`、旧会话 OneID 是个人 id ⇒
   同一群在改前/改后各有一套会话行，DM→群收敛是否要做合并迁移属产品决策，本批未评估、未动数据。
6. **两枚旗标今日只存档、无判定消费** ⇒ 没有腿断言"运营界面把 `Set=false` 渲染成'未下发'而不是 false"。
   这一格在接口/前端层，本批的三态只在 `Extra` 一侧成立。
7. **§21.4-3 那处"变松"没有用例盯。** 腿① 只断言 hub 行落了 `sender_name`，
   没有断言"因此自回环第 2 支不再命中钉钉客户消息"。要钉它得往批H 的用例里塞一条同正文的出站行，
   而那实际是在测 `interceptInbound` 的第 2 支、不是测钉钉面 ⇒ 判为**不属于本批覆盖面**，
   归入回环设计那条线（与第 3 支的取舍一并复议）。风险评级低：第 1、3 支都不依赖 `sender_name`，
   真回环的拦阻主路径未动。

---

## 22. 批F-6：N-13「根本没发」的分支 —— 从静默 `(false, nil)` 到结构化错误 + 失败轨迹（2026-09-21，全渠道面收口）

### 22.1 这一条真正缺的是两样东西，不是一样

§5 原文只说了钉钉三条分支裸 `return` ⇒ `sendErr==nil`。动手时把判据放大到整张出站表：
`sendOutbound`（`webhook_outbound.go:358`，签名具名返回 `(sent bool, sendErr error)`）的 9 个 `case` 里，
**"请求根本没发出去"这一类前置不满足分支共 15 条**（账号 id 非法 ×5、会话键解析不出 ×2、
缺 sessionWebhook、窗口过期 ×2、域名非法、请求构造失败、渠道不在表内、桥接缺入站上下文、
桥接目标不可达、企微客户端未接线），改前**全部是裸 `return`**。

补错误只是第一半，第二半才是这条 P1 的实际危害：**失败在库里没有反证**。
只补 `sendErr` 的话，坐席视图仍显示"客户问了、没人答"，`HasUnrepliedCustomerMessage` 恒 `true`
⇒ 补触发 worker 每轮重挑同一条、每轮进同一分支，形成**永动的无效补触发**。
所以本批同时落"错误"与"轨迹"两件事。

| 落点 | 位置（改后） | 职责 |
|---|---|---|
| `preSendFailure(channel, category, reason)` | `webhook_outbound.go:916` | 构造"没发出去"这一类的 `*ChannelError`：`Retryable:false` 恒定（重投同一份内容必然同样失败），`Category` **必须显式给** |
| `markSendFailed` / `markSendFailedLogged` | `:382` / `:389` | 前者"记录错误 + 走统一失败收口"；后者用于**无可归属轨迹行**的那两支（渠道不在出站分支表 / 桥接族已有自己那一行），只记错误与日志、不试写库 |
| `outboundSendFailed` | `channel_error.go:330-379` | 结构化日志 → `CategoryAuth` 告警事件 → 不可重试时落轨迹；`hubMsg==nil \|\| messageHubRepo==nil` 或 `ce.Retryable` 时**只留日志** |
| `pushUndeliveredReplyTrace` | `:386` | 另造一行出站轨迹，走 `MessageHubService.PushSendFailureTrace` 而非自拼 `repo.Create` |
| `PushSendFailureTrace` | `message_hub.go:387-404` | `Status="send_failed"` + `Extra[send_failed_at]` + `Extra[send_failed_reason]`；**只写库，不进 stream、不通知订阅者** |

两处判据上的硬约束，记下来防回头踩：

1. **`Category` 不能留 `CategoryUnknown`**。`completeChannelError`（`channel_error.go:77`）对已判定类别原样放行，
   而对 unknown 会 `classifyChannelError(ce.Raw)` **按文案重判并无条件覆盖 `Retryable`**（`:95` 是无赋值保护的
   `ce.Category, ce.Retryable = base.Category, base.Retryable`）⇒ 手工设的"不可重试"会被一句文案左右，
   换个说法重试行为就变。电池 M15 就是打这一格的。
2. **轨迹行不推给订阅者**（`message_hub.go:384-386`）：这条消息客户从未收到，
   推给坐席实时视图/会话镜像等于宣告一次没发生的投递；它的用途是留痕 + 给"是否已回复"提供反证。

### 22.2 就地改状态那一支，今天在生产里到不了（子发现 a，登记不修）

`outboundSendFailed` 分岔条件是 `if hubMsg.ID != 0 && hubMsg.Direction == "outbound"`（`:374`）。
把 `sendOutbound` 的**全部**调用点枚举完（`grep "sendOutbound("`，非测试命中 3 处）：

| 调用点 | 传入的 `hubMsg` | `ID` | `Direction` |
|---|---|---|---|
| `webhook_outbound.go:309`（延后重放） | 结构字面量 `&model.MessageHub{ConversationID, SenderID, SentAt}` | 0 | `""` |
| `webhook_ai.go:204`（实时销售引擎） | 入站行 | ≠0 | `inbound` |
| `webhook_ai.go:541`（智能客服编排） | 入站行 | ≠0 | `inbound` |

⇒ `MarkOutboundSendFailed`（WHERE 带 `direction='outbound'`）**从生产路径恒不可达**，
本批的分岔是**前瞻**分支，实际生效的一直是"另造一行"那一支。为什么不删：
它在"出站先落库再投递"的链路一旦落地（见下一条）就是正确路径，删了等于把那条链路的反证再丢一次。
钉法：M03/M04/M05 三刀分别打整个条件恒真、只删 `ID != 0`、只删 `Direction` 判据，
配套腿 L8/L9/L13 覆盖三种形状。

**这一格暴露的真实缺口比"分支到不了"更大**：AI 投递**成功**的回复，除桥接五渠道
（`case ChannelDouyin, ChannelXiaohongshu, ChannelTiktok, ChannelXianyu, ChannelKuaishou` 在 `:756` 自己落一行）之外，
TG/QQ/钉钉/公众号/飞书/企微/WhatsApp **不写任何出站 hub 行** —— 两个 AI 调用点都是 `_, _ = s.sendOutbound(...)`，
函数体里也确无落库。后果是"发出去了"在库里同样没有正面证据：批F-6 补的是**反证**（失败留痕），
**正面轨迹仍缺**。这属出站链路改造（涉及幂等键、`UpdateDeliveryStatus` 真达收口、坐席视图口径），
不在"补错误/补轨迹"这一批的射程内 ⇒ 登记为独立后续，不夹带。

### 22.3 桥接族的两格：一行不许多，也不许少

桥接的失败轨迹**本来就是它自己落的那一行**（`:739-757`：`Status="failed"` +
`Extra["scenario"]="undeliverable"` + `undeliverable_reason`）。这一支若也走 `markSendFailed` 的轨迹落库，
同一会话会出现**两行出站**，坐席侧与补触发侧都会看到一条根本不存在的回复
⇒ 改走 `markSendFailedLogged`（M13 一刀即"把 logged 换成 trace"，被 L11 + 既有 `..._P1_4` 腿双杀）。

**子发现 b（本批不修，登记）**：`case persisted.ID == 0`（`:867-872`）显式写了 `Retryable: true`，
但类别是 `CategoryUnknown` ⇒ 按 §22.1-1，`completeChannelError` 会把这枚 `true` **覆盖掉**，
最终值由文案"桥接出站未写入 message_hub(channel=… conv=…)"落到 `classifyChannelError` 的 `default`
分支（`:304-306` 恰好也给 `Retryable=true`）**碰巧一致**。
今日行为正确，但正确性来自判档器兜底而非构造点意图 —— 一旦该文案里出现 `timeout`/`no such host`
之类的字样（`conv=` 是外部输入，可控性在我们之外但非无穷），类别与重试性都会跟着文案漂移。
修法是给这一支一个明确类别（落库失败是**我们侧**的故障，不是渠道拒绝，也不是窗口问题），
而那会改变重投行为 ⇒ 属重试口径决策，与 §22.2 的正面轨迹一并排，不在本批顺手做。
当前由 L7 钉住"可重试那一支不写轨迹"的口径，`ID==0` 那一格**未钉**，列入 §22.7。

### 22.4 用例：13 条腿（`webhook_outbound_batchf6_trace_test.go`，699 行）

按"三条渠道分支逐个 + 形状 + 静态锁"排：

| 腿 | 断言的判据 |
|---|---|
| L1 `DingTalkMissingWebhookLeavesTrace` | 不可重试错误 + 轨迹行携带正文/平台/`IsAIReply`/`ReceiverID`/`send_failed_category`/`send_failed_reason`/`send_failed_at`；不占重投队列；`HasUnrepliedCustomerMessage==(true,true)`（反证生效） |
| L2 `…ExpiredWebhookReasonCarriesDeadline` | 毫秒形态先归一（`:622-624`），`expired_at` **秒值**同时出现在 `ce.Raw` 与库里；夹具域名写成 `http://127.0.0.1:1/…`，防"过期守卫被变异掉时真打到钉钉" |
| L3 `…IllegalHostNeverTouchesNetwork` | 原子计数器：非法域名那一支对 `httptest` 的命中数必须为 0 |
| L4 `TelegramNonNumericChatIDLeavesTrace` | `chatID==0` 守卫（`:491`）→ 轨迹 + 不可重试 |
| L5 `WhatsAppWindowClosedWithoutTemplateLeavesTrace` | 24h 窗口（`:560`）+ 无兜底模板（`:564`，`t.Setenv("WHATSAPP_FALLBACK_TEMPLATE","")`）；原因内嵌 `2006-01-02 15:04:05` 时刻 |
| L6 `UnsupportedChannelErrorsWithoutTraceRow` | 词表外渠道 → `markSendFailedLogged`，回传错误但**不落**轨迹（`Normalize` 必然拒） |
| L7 `RetryableFailureWritesNoTraceButKeepsRetryLane` | 真实投递被平台拒（`errcode=310000`）→ 0 条 `send_failed`、恰好 1 条 `send_retry`（两通道互斥） |
| L8 `PersistedOutboundMarksInPlace` | 出站行形状 → 就地改状态，**只此一行** |
| L9 `InboundCopyCreatesExactlyOneRow` | 入站行形状 → 入站行不动（`status` 保持非 `send_failed`；默认值是 `pending` 不是空串）+ 新增 `dingtalk-fail-` 前缀出站行 |
| L13 `OutboundShapeNeverPersistedStillCreatesRow` | `Direction=="outbound"` 但 `ID==0` ⇒ 必须另造一行（**M04 第一轮存活后补的腿**） |
| L10 `NoTraceTargetOnlyLogs` | `hubMsg==nil` 与"有行但会话键为空"两种形状都只记日志，总行数不变（防"造孤儿轨迹"） |
| L11 `BridgeUndeliverableKeepsSingleOutboundRow` | 桥接不可达 ⇒ 恰好一行出站，且就是它自己那行 `status=failed` + `scenario=undeliverable` |
| L12 `ChannelVocabularyCoversEveryOutboundChannel` | `go/ast` 静态锁：`sendOutbound` 的 `switch channel` 每个 case 常量都必须在 `messageHubPlatforms` 里（否则该渠道失败轨迹永远落不了库）。自带下界：常量 <10、case <8、解析文件数 0 均判红 |

### 22.5 变异电池：23 格（`.tmp_files/mut-evidence-2026-09-21/batchF6/battery.py`）

跑在 `/tmp/bf6-hivemtk` 的 rsync 副本上（231M），**未碰**压着 104 处未提交改动的工作树；
电池结束后四个文件与工作树逐字节 md5 相等（`channel_error.go 3f229162`、`webhook_outbound.go aa1a3fb6`、
`message_hub.go 1c6248fb`、测试文件 `344366e1`）。

控制组 `ran=13 skip=0`；`--check` 先验 23 个锚点全部 `count=1`。结果：**21 格按预期被杀，2 格判等价**，
无"该杀没杀"。

| 判等价的 2 格 | 为什么不是漏 |
|---|---|
| M08 删 `content==""` 守卫 | 空正文在 `Normalize` 的正文校验处即被拒，轨迹本来就落不了 ⇒ 变异与不变异**外部行为一致** |
| M22 把 unsupported-channel 从 `logged` 换成 `trace` | `messageHubPlatforms` 拒了该渠道 ⇒ `PushSendFailureTrace` 必返错、行数仍为 0 |

**M04 第一轮存活 = 真缺口，不是噪声**：只删 `ID != 0` 时，L1–L5/L9 全部照绿，
因为它们的夹具都是"入站行"形状（`Direction=="inbound"`，短路在 `&&` 第二段），
没有任何一条喂"出站形状但从未落库"的 `hubMsg` ⇒ 补 L13 后重跑，M04 KILLED。
另加 **T01 反向对照**（改夹具而非改生产码：把 L10 孤儿行的 `ConversationID` 填上）→ L10 转红，
证明 L10 的"行数不变"不是空断言。

### 22.6 静默前提：三条桥接腿在免打扰时段会整段跳过投递

`webhook_outbound_p1_4_test.go` 的三条腿不叫 `quietHoursOffForTest` ——
`sendOutbound` 在 23:00–07:00 会先进"延后首发"分支直接 `return`，**渠道真实投递根本不执行**。
本批没有改它们：它们今日绿，是因为夹具里没写 `reach_delayed_outbound`，
"延后首发"落不了库于是仍走到后面的口径 —— 即**通过路径与命名意图不一致**，
挂钟一到位就可能变味（`DISABLE_AI_QUIET_HOURS` 在 `.env` 里不存在，`TestMain` 也没关它）。
登记为观察项：要么给这三条腿补 `quietHoursOffForTest`，要么把它们显式改成"测延后首发"。
本批的新腿一律显式 `quietHoursOffForTest(t)`（有先例：批F-6 之前的钉钉/公众号重投腿已这么做）。

### 22.7 词表补齐是本批唯一一处"改动既有测试的期望"（必须逐条交代）

L12 那条静态锁要求"每个 `WebhookChannel` 常量都在 `messageHubPlatforms` 里"。跑完整门时它把三处
既有缺口照了出来，而其中一处是**既有测试在替旧世界观说话**：

| 现象 | 判据 | 处置 |
|---|---|---|
| 补词后 `TestValidPlatform_Unsupported` 红：`expected wechat invalid`（`message_hub_test.go:62`） | 该腿把 `wechat` 列在"非法平台"里，而真实入站路径 `controller/wechat.go:292 → inbox_ingress_persist.go:53` 一直用 `hubRepo.Create` 写 `Platform="wechat"`（绕过 `Normalize`）⇒ 库里本来就有这种行，**表在漏报，不是数据在越界**（证据：`message_hub_test.go:53-72`，两条 `ValidPlatform` 腿） | 把 `wechat`/`dingtalk`/`custom` 从非法侧移到合法侧，并写明本表语义从"hub.Push 允许哪些"变为"hub 里会出现哪些平台"。**这是本批唯一一处改测试期望**，理由与取证在此，不留给 diff 自证 |
| `custom` 为何也算合法 | 它**没有**入站路径（`webhookInboundCapable` 判 false，`webhook.go:334-342` 在落库前就拒），但 `POST /api/chat/ingress` 的 `channel` 是自由文本：`NormalizeEvent` 只在 `event.Channel == ""` 时报 `invalid channel (empty)` ⇒ 任意字符串都能成为 `message_hub.platform` | 保留在表内（表是"渠道常量全集的镜像"，由 L12 锁），并把上面那格**单独登记为观察项**（见 §22.8-1）：统一入站口不校验渠道词表，`by_platform` 统计与工作台筛选可被任意值污染 |
| 新红风险排查 | `ValidPlatform` 除本测试外**零生产消费者**；`messageHubPlatforms` 只被 `Normalize`（`:271` 的 `messageHubPlatforms[req.Platform]`）、`ValidPlatform`（`:742`）、`ListPlatforms`（`:757`，供 `controller/message_hub.go:202` 的下拉选项）读 | 拓宽只影响"这些平台的轨迹落得了库"与前端多三个筛选项（这三个值本就出现在数据里，属**补报**），不改任何鉴权/受理判定 |

### 22.8 本批未覆盖面（不得当作已验证）

1. **统一入站口不校验 `channel`**（§22.7 第二行）：`/api/chat/ingress` 有 `IngressSecretAuth()` 挡着，
   但持秘者（浏览器扩展、未来任何接入方）写错一个平台名不会被拒，只会在统计里隐身。
   本批只在 `persistMessage` 的 `MsgType` 侧做了归一（`InboundHubMsgType`），`Channel` 侧未动。
   ⇒ 已升格为独立偏差条目 **N-29**（含两种修法的代价分析），不在本批顺手做。
2. **`persisted.ID == 0` 那一格没有腿**（§22.3 子发现 b）：它的重试性目前由文案落到判档器 `default` 决定，
   未钉桩。修它需要先定"落库失败该不该重投"这个口径。
3. **成功投递的正面轨迹缺失**（§22.2 末）：非桥接七渠道的 AI 回复在库里只有反证没有正证。
4. **`_test.go` 之外仍写死 `(false, nil)` 语义的调用方**：本批只改了 `sendOutbound` 内部收口；
   `enqueueSendRetry` 的 defer 条件依赖 `sendErr != nil && !sent`，两侧在新口径下自洽（L7 钉住），
   但"哪些渠道的真实投递接口会继续返回裸 `error` 而非 `*ChannelError`"没有逐渠道清单。
5. **静默时段那三条既有腿未改**（§22.6），观察项待处置。
6. **无真实平台账号**：本批全部在 `httptest` 与守卫路径上，"窗口过期/域名非法在真实回调里
   出现频率"未知 ⇒ 轨迹行数在生产的量级没有测量。

### 22.9 门禁与证据

静态：`go build ./...` rc=0、`go vet ./internal/service/` rc=0（两条都单独取 `$?`，不经管道）、
`gofmt -l` 对本批触及的 5 个文件输出空。

| 轮 | 命令 | 结果 |
|---|---|---|
| 1 | `go test -p 1 -count=1 -timeout 2500s -test.v ./internal/service/`（HEAD `4f6d0e81`） | **rc=1** — 3786 PASS / **1 FAIL** / 3 SKIP；唯一红 `TestValidPlatform_Unsupported`，是本批词表拓宽推翻了既有期望（§22.7），非环境 |
| 2 | 同上 | **rc=0** — 3787 PASS / 0 FAIL / 3 SKIP，489.290s；13 条 BF6 腿在整包门内逐条 `--- PASS` |

产物镜像在 `.tmp_files/mut-evidence-2026-09-21/batchF6/gates/`（两轮日志整份都留，红轮不删），
清单与逐条 md5 见 `.tmp_files/mut-evidence-2026-09-21/MANIFEST.md`（**31 行数据行**，口径 = `grep -c '^| `' MANIFEST.md`；
python 逐行复算 size+md5，0 处不符、0 处缺文件）。
三条 SKIP 属 §20.6 已逐条归因的结构性环境跳，非本批引入。

**HEAD 漂移后的可复算性（2026-09-21 本批收尾时补）**：两轮门跑在 `4f6d0e81`，收口时并行泳道把
T-P4-05 落成 `478ef1c4`。核对方式 = 对该提交涉及的 16 个文件逐个 `git status --porcelain`：**全部干净**
（工作树内容与其提交内容逐字节相同，本泳道未碰其中任何一个），因此两轮门覆盖的代码面与
"现 HEAD + 本批未提交改动"完全等价，**无需复跑**，下轮复算也不必因为 HEAD 号不同而重开本批结论。

⚠ **给复算者的口径**：`git diff --stat user-server/internal/service/message_hub.go` 报的是**跨批累积**
的未提交改动（批F-4d/e 的 `InboundHubMsgType` 与别名表同在这个文件）。批F-6 自己的字节面是
`messageHubPlatforms` 的三个词 + 其注释、`PushSendFailureTrace`（`:387-404`）、
`channel_error.go` 的 `outboundSendFailed`/`pushUndeliveredReplyTrace`、
`webhook_outbound.go` 的 `preSendFailure` 与 15 处调用点改写、新测试文件 699 行、
`message_hub_test.go` 两条 `ValidPlatform` 腿的期望更正。整批按纪律**未提交**。

## 23. 批M：N-26「默认存储」选取面 —— 先把登记的那句话验实，再按到得了的缺陷下刀（2026-09-21）

对象不是某条渠道，而是**所有渠道出站媒体与素材上传共用的那一个入口**：`obs_config` 里 `is_default = true` 的那一行。批I 在 §20.7 以一句话登记了它（"`GetDefault` 不看 status"），本批先把它补成 §5 的正表条目（表里此前从 N-25 直接跳到 N-27），再收口。

### 23.1 登记的那句话只说对了一半：先取证，再动判据

原登记的症状是"停用/错误的默认存储仍接收媒体转存"。四条只读取证（2026-09-21 复算）：

| 面 | 取证方式 | 结果 |
|---|---|---|
| 谁能改一行的 `status` | 全仓 grep `UpdateStatus`（含 `*.md` `*.yaml`） | 仓储实现自己 + 本批新写的测试；`obsConfigRepo.UpdateStatus` 在 service/controller 层**零生产调用方**，`UpdateObsConfigRequest` 里根本没有 status 字段 ⇒ 界面上没有"停用"这个按钮 |
| 建配置时 status 从哪来 | 读三个 `CreateConfig` 入口 | 一律硬编码 `model.ObsStatusActive` |
| 存量数据面 | `PGPASSWORD=… psql -h 127.0.0.1 -p 8232 -U admin -d user_db -Atc "SELECT status, is_default, count(*) FROM obs_config GROUP BY 1,2"`（**必须带 `-U admin`**，不带会以本机用户连库、直接 auth 失败） | `active\|t\|1` —— 全表 1 行，active 且默认（rc=0，stderr 空；2026-09-21 收口时复跑一次，仍是同一组） |
| 旁路写入（第一遍，窄口径） | grep 裸表名 `obs_config` 的写语句 | 除本仓储与 `migrate.go` 钩子外无命中 —— **这一遍口径不够**，见下一行 |
| 旁路写入（第二遍，按对象口径） | grep `model\.ObsConfig\{\|IsDefault:\|is_default` 全 `*.go`（构造体也算写面，GORM 的 `Create(&model.ObsConfig{…})` 不含裸表名） | 多出两处，都不构成第二写口：① `service/init_storage.go:43-58` 启动 seed —— 只在 `Count == 0` 时 `gdb.Create` 一条 active＋默认（`count > 0` 直接 return），表空才插 ⇒ 造不出第二条默认，与新索引不冲突；② `service/channel_media.go:79-87` 是**内存里的**兜底构造体（`GetDefault` 失败时现造一台 local 交给 `storage.Factory`），全函数体无任何 `Create`/`Save`/`Update` ⇒ 不落库。两处均已在 §5 N-32 单独记账（②的静默退回本地属行为缺陷，但那条文件压着并行泳道改动，本批不动） |

⇒ "默认行是非 active"这个状态**今天做不出来**：① 是**缺判据**，不是现行故障。本批因此**没有**按原描述去"修 ①"，而是按同一面里**真到得了**的 ②③ 下刀（零默认窗口、陈旧快照复活双默认），并把 ① 顺手补成判据。

⚠ 但 ① 与"设默认必须校验 active"是同一枚硬币的两面，**必须同批**：一旦读侧开始按 `status = active` 筛，写侧若不校验，管理员在管理页点一下"设为默认"（目标那台恰好停用）就把全站切成"没有任何可用默认"，而界面回"成功" —— 那是本批**自己造出来**的致残路径。服务层腿 S1 存在的唯一理由就是这一格。

### 23.2 形状改动，每一处都是"另一种写法不行"才定下来的

| 位置 | 旧 | 新 | 为什么不能是别的写法 |
|---|---|---|---|
| `repository/obs_config.go:142-148` `GetDefault` | `WHERE is_default` + `First` | `WHERE is_default AND status = active` + `ORDER BY created_at ASC` | 不能"没默认就降级到任意 active 行"：默认是运维显式选的那台，悄悄换一台会把文件写进另一个桶，而两边公开 URL 都成立 ⇒ 只有取回原文件时才露馅（R1 第二档钉"不许降级"）。`Order` 不能省：它是钩子清重语句的同一把尺子（R5） |
| `:160-176` `SetDefault` | `ClearDefault()` 先提交，再 `UPDATE` 目标行（两句、无事务、影响 0 行也返回 nil） | 单事务内"先摘别人 → 后置自己"，`RowsAffected == 0 ⇒ gorm.ErrRecordNotFound` | 顺序**反不得**：索引在场时先置自己当场 23505（M05 即死于此）。两句**分不得**：先清后设中间失败就留零默认（M07）。不查 `RowsAffected` 等于把失败报成成功（M06） |
| `:187-200` `IncrementUsage`（新增入口） | 无此入口，旧路径是 `config.FileCount++` + `repo.Update`（整行 `Save`） | `UpdateColumns(map…)` 只写 `file_count`/`total_size` | 不用 `Save`：写回的是上传开始时的快照，会连陈旧 `is_default` 一起落库（③ 的成因）。不用 `Updates`：它顺手刷 `updated_at`，"有人传了个文件"会顶掉管理页的"最后修改时间"，把"谁改了配置"的时间线淹掉（M08）。列上用 SQL 表达式而非绝对值：并发上传下只有 `file_count + 1` 是对的（M09/M10） |
| `:92-118` `Update`（编辑口，**③ 的另一半**） | `r.db.Save(config)` —— 整行写回，且主键为零值时退化成 INSERT | `Updates(map…)` 只写 `UpdateObsConfigRequest` 实际携带的 12 列；`config.ID == ""` 直接 `ErrRecordNotFound`；`RowsAffected == 0 ⇒ ErrRecordNotFound` | ③ 的机制是"整行写回陈旧快照"，本批一开始只换了**上传**那一处，**编辑**这一处的 `Save` 同样会把快照里的 `is_default`（→ 复活成第二条默认，R6）、`status`、`file_count`/`total_size`（→ 计数器被旧值覆盖，R6）一起写回。为什么用**白名单**不用 `Omit` 黑名单：黑名单只挡当下认识的列，模型以后加一列就静默失守（M27 实测"只 Omit 选取列"仍漏用量列与无主键插入两格）。为什么有索引还不够：索引只把静默污染变成 23505，管理员改个名字会收到一句 duplicate key 且改动不生效（R6b 钉这一格）。为什么留 `updated_at` 刷新：旧实现的 `Save` 连 `updated_at` 都不动（R6 跑出来的实测事实），管理员改过配置的时间线等于没有 |
| 接口 `ObsConfigRepository`（`:18-33`） | 暴露 `ClearDefault` | 删除，并在接口注释写明"刻意不提供只清默认的方法"；`Update` 上补一行"只写可编辑 12 列" | 单独"只清默认"没有任何合法用法；这个入口只要存在，就一定有人在事务外先清后设（M15 把它放回接口 + 服务层调用 ⇒ S1b 红） |
| `service/obs_config.go:193-203` `SetDefaultConfig` | 先 `ClearDefault` 再 `SetDefault` | 只读校验"存在且 active"，写全交给仓储那一个事务 | 服务层不该知道"清"这一步 —— 它一知道就必然在事务外做 |
| `:278-309` `UploadFile` | 整行 `Save` 写用量 | `IncrementUsage`，失败仍只 `Warn` 不阻断 | 计数写失败不该把已经落到存储的文件判成上传失败；但必须**可判定**（R3b 钉"0 行 ⇒ error"，否则计数与真实文件数长期对不上而无人知） |
| `repository/obs_config.go:132-140` `DeleteNonDefault`（**批M-2**，原名 `Delete`） | `WHERE id = ?` 的无条件删除；"不许删默认"这件事只存在于服务层那次预读里 | `WHERE id = ? AND is_default = false` + `RowsAffected == 0 ⇒ ErrRecordNotFound`，并把入口改名字成判据的一部分 | 不能留成"预读 + 删"：预读到落库之间另一次"设为默认"提交就把唯一默认删走了（N-33）。也不能只加 `RowsAffected` 判定而不加谓词 —— 那样默认行照删，只是"删成功"这件事变得可观察；也不能反过来只加谓词不查 0 行 —— 删不存在的 id 会对界面回"已删除"。两半各有一格变异（M28 拆谓词、M29 拆 0 行判定），红因集合互不相同。改名而不是保留 `Delete`：以后要"连默认一起删"（清库/重建）必须显式另开入口，不许顺手放宽这条 |
| `db/migrate.go:423` 调用 + `:474-500` 钩子 | 无库级守卫 | `CREATE UNIQUE INDEX IF NOT EXISTS idx_obs_config_single_default ON obs_config (is_default) WHERE is_default`，建之前先清重 | 必须 **partial**：GORM 标签表达不了 `WHERE`，去掉 `WHERE` 会把"第二台非默认存储"拦在创建门外（M19 实测插第 2 条非默认就 23505）—— 比双默认更坏。清重门槛只能是 `>1`（抬到 `>2` 后最常见的 2 条脏状态没人管，M16）。清重保留的是 `created_at ASC, id ASC` 那一条 = GetDefault 本来会选中的那台 = 存量文件实际落在的那台（M18/M22）。为什么这里要"先清重"而 `clue_id` 那条不用：那条的重复要人来判断留哪一条，这里的重复**语义上必然有一行是错的**且答案唯一（见钩子注释） |

### 23.3 用例：22 条腿跨三包（新增 3 个文件、1135 行）

| 文件 | 腿 | 钉什么 |
|---|---|---|
| `internal/repository/obs_config_default_batchm_test.go`（553 行，真库） | R1 `:108` | 默认行停用 ⇒ `ErrRecordNotFound`，且**不许降级**到别的 active 行；恢复 active ⇒ 必须选中（防"过滤写反"把 active 当 inactive 筛掉） |
| | R2 `…:148` | 目标行不存在 ⇒ 报错且**一条都不改**（旧实现留下零默认） |
| | R2b `…:170` | 切换后全表恰好 1 条默认。计数走 `batchMCountDefault` 直读列，不走 `GetDefault` —— 否则 `First` 把两条藏成一条，用例变成自证 |
| | R3 `…:198` | 取快照 → 切默认 → 计数写回 ⇒ 旧行没被复活、计数 2 个/5120 字节、`status` 未动、默认仍是新那台 |
| | R3c `…:250` | 计数写回不刷 `updated_at` |
| | R4 `…:283` | 守卫在场时切换仍走得通 —— "顺序反不得"的唯一杀手。测试库不跑 post-migrate 钩子，故腿内显式建同一道索引（DDL 与 `migrate.go:493` 同源，两处若漂移 db 包那五条腿先红） |
| | R5 `…:311` | 本文件唯一**故意**铺双默认的腿：断的不是"读侧兜住脏状态"，而是"读侧取序 = 清重取序"。夹具把 id 字典序与创建序拧反（`aaa-batchm-r5-newer` 先建、`zzz-batchm-r5-older` 后建），所以删掉 `Order` 时 GORM 的默认主键升序必选错的那台，不靠物理存储顺序侥幸 |
| | R3b `…:348` | `Update`/`UpdateColumns` 对 0 行都返回 nil ⇒ 必须自己判成 error |
| | R6 `…:375` | **N-26③ 的另一半**：读快照 → 切默认 → 涨计数 → 用 `UpdateStatus` 把这台判成 `error` → 再用快照改个名写回 ⇒ 默认仍只有一条（新那台）、改名生效、用量仍是 1/4096、`status` 仍是 `error`（不许被快照抹回 `active`）、`updated_at` **必须**刷新。三组列各有各的属主，编辑口一张都不碰。本腿刻意**不建**索引，让"复活"以数据形状暴露而不是先撞唯一冲突 |
| | R6b `…:440` | 同一份陈旧快照在守卫**在场**时仍要编辑得动：只把"复活"改成"撞 23505 报 500"是不合格的修法（管理员改个名字会收到 duplicate key 且改动不生效） |
| | R6c `…:488` | 编辑口对"行已不在"必须报错；且主键为空的 struct 不许经这个口落库（旧实现用 `Save`，无主键时它会**退化成 INSERT**，编辑口因此有第二身份） |
| | R7 `…:526` | **删除口删不动唯一默认行**（N-33）：判据写在 `DELETE … WHERE id = ? AND is_default = false` 里，而不是留给调用方预读 —— 服务层"先 `GetByID` 看 `is_default` 再删"的两句之间，另一次"设为默认"提交就会把全站唯一默认删走，且 `InitDefaultStorageIfEmpty` 只在**表为空**时兜底 ⇒ 零默认无自愈。三条断言各钉一格：默认行删不动、非默认行删得掉且是真删、不存在的 id 必须报错（`Delete` 对 0 行返回 nil，界面会把"什么都没删"显示成"已删除"） |
| `internal/service/obs_config_batchm_test.go`（365 行，记账替身） | S1 `…SetDefaultConfigRejectsInactive:160` | 停用行不许设默认，错误里要带状态；对照腿：同一台恢复 active 后必须能设、且只调一次仓储 |
| | S1b `…SetDefaultConfigDoesNotPreClear:192` | 服务层**一次都不许**自己清默认（零默认窗口的成因就是它） |
| | S2 `…UploadFileWritesUsageViaIncrementOnly:214` | 上传成功后写回只走 `IncrementUsage{size=12}`、`Update` 调用次数为 0、文件真落在该配置的 `Endpoint` 下（防"驱动换了别的路径还报成功"） |
| | S3 `…UploadFileWithoutDefaultIsExplicit:294` | 无可用默认时错误必须归因到"选取失败"而不是"驱动坏了"，且不写用量。夹具故意传 `file=nil, header=nil`：能安全走过这一句本身就证明它在选取失败时就返回了 |
| | S5 `…DeleteConfigCannotRemoveDefault:322` | 服务层只许走**带守卫的那个删除入口**：① 非默认行删得掉且记到 `deleted`；② 预读就看得见默认 ⇒ 错误里点名"默认"、且不必再走删除口（这一条断的是**文案**，M30 杀的就是它，不是安全判据）；③ 用替身的 `beforeDelete` 铺出"预读之后、落库之前被并发抬成默认"这个交错 ⇒ 删除必须失败、行必须在、默认仍指向并发那次切换的目标 |
| `internal/pkg/db/obs_config_default_index_batchm_test.go`（217 行，真库 + 直接调钩子） | I1 `…CreatedAndIdempotent:72` | 钩子可重跑（启动路径每次都过这段） |
| | I2 `…RejectsSecondDefault:90` | INSERT 与 UPDATE **两侧**都拦得住（陈旧快照复活走的正是 UPDATE 侧） |
| | I3 `…AllowsManyNonDefaults:126` | `is_default = false` 不受约束；清掉默认后再来一条默认要成功（"唯一"≠"必须存在"） |
| | I4 `…RepairKeepsOldestDefault:156` | 存量 **2 条与 3 条两档**：都只留最早那条、索引建成、总行数不丢（降级是改列不是删行） |
| | I5 `…HookIsWiredIntoMigrate:199` | 静态锁（行为不变，只钉装配）：`migrate.go` 里那句调用恰好 1 次，且排在 `missingTables` 终校验之后 —— 其余四条腿都是直接调函数，测不到"有没有人调它" |

**三层分工口径**：repository 那份断 **SQL 落地形状**（真库），service 那份断**调用形状**（替身记账），db 那份断**库级守卫与装配**。不互相代打 —— 替身测不出 `UpdateColumns` 与 `Save` 的差别，真库测不出"服务层是不是自己又清了一遍"。

**既有测试文件的一处删除（本批唯一的测试面减法）**：`internal/repository/obs_config_test.go` 的 `TestObsConfigRepository_ClearDefault` 随入口一起删（原地留一行注释说明"删的是入口不是判据"，并指向接管它的 `TestBatchM_SetDefaultLeavesExactlyOneDefault`）。判据没有丢：旧用例断的是"清完之后那行不再是默认"，新腿断的是更强的"任意时刻全表恰好一条"，前者是后者的子集。全仓对 `ClearDefault` 的引用面靠 `go vet ./...` rc=0 兜住（编译期即暴露漏清的消费方，不靠 grep）。

**控制组（无变异）**：`repository 12 / service 5 / db 5` 条腿全 PASS，**0 FAIL、0 SKIP**，三包 rc=0。"0 SKIP"是必需读法：`testutil.NewTestDB` 在本地连不上 PG 时走 `t.Skipf` 而非 Fatal（只有 `CI=true` 才 Fatal），所以"绿"必须先排除"根本没跑"。

### 23.4 变异电池：29 格全杀（M21 不进电池，登记为等价 —— 该登记已由批M-4 撤销，见本节末与 §23.11）

驱动 = `/tmp/batchm_battery.py`（`CELLS` 按包分列，实测 **29 条**：repository 18 条 = M01–M11 + M23–M29、service 5 条 = M12–M15 + M30、db 6 条 = M16–M20 + M22；M21 刻意不在里面，理由见下。⚠ 本节格号只在本节内自洽 —— §21 那份抖音电池另有一套 M01–M28，同号不同事，跨节引用请连节号一起写），逐格日志 `/tmp/batchm_battery/<格号>.log`，三包的汇总行分别归档在 `/tmp/batchm_battery/ALL-{repository,service,db}.log`。备份写在 `/tmp/batchm_battery/bak/`，工作树不留 `.bak`（跑完 `find . -name "*batchm-bak*"` 为空；三处产码文件跑完逐个 `md5` 与备份逐字一致）。
**KILLED 的三条同时成立**：`go test` rc ≠ 0 **且**指定腿出现 `--- FAIL`（构建失败不算杀，判 BROKEN）**且** 还原后 md5 与备份逐格一致。每格跑该包 `-run TestBatchM_` 过滤集 —— 过滤集只用于迭代，§23.7 的无过滤全量才是门。表格按**属面包**分组（repository 那 **18** 条里，编辑口 M23–M27 紧跟在计数口 M11 之后，编号不连续是为了同族相邻。原句写的是"16 条" ⇒ 批M 收口那版的残留计数，彼时删除口 M28/M29 尚不存在；批M-2 加了这两格却漏改这句，§23.10 复跑时按格清单重数出来才发现）。

| 格 | 变异 | 杀手腿 | 实测红因（摘自各格日志） |
|---|---|---|---|
| M01 | `GetDefault` 丢 `status` 谓词 | R1 | 默认行已停用却返回了它 |
| M02 | `GetDefault` 丢 `is_default` 谓词 | R1 | 挑中了一台 active 的非默认行（= 降级） |
| M03 | 删 `Order("created_at ASC")` | R5 | `双默认时选中了 batchm-r5-newer(id=aaa-…)，期望 batchm-r5-older(id=zzz-…)` |
| M04 | 删"先摘别人"那一句 | R2b | 默认行 = 2 条 |
| M05 | 两句换顺序（先置自己） | R4 | `duplicate key … idx_obs_config_single_default (SQLSTATE 23505)` |
| M06 | 不查 `RowsAffected` | R2 | 不存在的 id 返回 nil，且默认行被清空 |
| M07 | "先摘别人"挪出事务（`r.db` 直写） | R2 | 切换失败之后默认行没了 |
| M08 | `UpdateColumns` → `Updates` | R3c | `updated_at` 被计数写回顶掉 |
| M09 | `file_count` 改绝对赋值 | R3 | 两次上传计数 = 1 |
| M10 | `total_size` 改绝对赋值 | R3 | 5120 变 1024 |
| M11 | `IncrementUsage` 不查 `RowsAffected` | R3b | 不存在的 id 返回 nil |
| M23 | 编辑口白名单里加回 `is_default`（最"无害"的一次扩列：管理员想从编辑框改默认） | R6 | 无索引时 `编辑之后默认行 = 2 条 [batchm-r6-other batchm-r6-renamed]`；同一次变异让 R6b 撞 `23505`（管理员改个名字收到 duplicate key） |
| M24 | 编辑口退回本批修之前的原样：整行 `Save(config)` | R6 | 一次变异四条红：默认行 2 条、`file_count/total_size = 0/0，期望 1/4096`、`status 被快照复活成 active，期望 error`、`编辑没刷 updated_at`；R6c 另抓到 `Save` 对已删除的 id 返回 nil |
| M25 | 编辑口不查 `RowsAffected` | R6c | `对已删除的 id 执行 Update 返回了 nil（界面会显示保存成功）` |
| M26 | 白名单漏掉 `name`（少写一列，而不是多写） | R6 | `编辑没落地：name = "batchm-r6-old"，期望 batchm-r6-renamed` —— 白名单**多一列少一列**都被抓，危险不是单向的 |
| M27 | 改用 `Omit("is_default","status").Save` 黑名单（本批第二"顺手"的修法） | R6 | 选取列这次守住了（R6b 因此**绿**，正是黑名单的假安全处），但 `用量 = 0/0`、`updated_at` 没刷，且 R6c 两条身份红：`无主键的 struct 经编辑口返回了 nil` + `编辑口把无主键的 struct 插进了库（count=1，期望 0）` |
| M28 | 删除口丢掉 `is_default = false` 谓词（批M-2 新增那条 DELETE） | R7 | 两条一起红：`删除唯一默认行返回了 nil`、`试着删除默认行之后，库里默认行 = 0 条 []，期望 1 条 [batchm-r7-default]` —— 后者证明变异**真的把默认行删走了**，红因不是"错误没说清" |
| M29 | 删除口不查 `RowsAffected`（谓词在场、0 行也算成功） | R7 | 与 M28 红因集合**不同**：只有 `:534 返回了 nil` 与 `:551 删除不存在的 id 返回了 nil`，默认行那条计数断言仍绿（行确实没被删）⇒ 这一格钉的是"0 行不许报成功"这半条，M28 钉的是谓词那半条 |
| M12 | 服务层 active 校验掏空成 `return nil` | S1 | `把停用中的配置设成默认返回了 nil` |
| M13 | 上传写回退回整行 `Save` | S2 | `Update` 调用 1 次、`IncrementUsage` 0 次 |
| M14 | 选取失败的文案改成"构造存储驱动失败" | S3 | 归因错位 |
| M15 | `ClearDefault` 回到接口 + 服务层先清后设（4 处补丁，第 4 处给替身补方法以免编译红） | S1b | `服务层仍自己清了默认（1 次）` |
| M30 | 服务层删除预检掏空成 `return nil` | S5 | `删除默认配置的返回 = <nil>，期望错误里点名默认`。**代价说清楚**：这一格杀的是**文案**（预检之外的安全判据在仓储那条 WHERE 里，所以掏空预检不会造成零默认），把它放进电池是因为管理页"不能删除默认配置"这句话是唯一的可操作提示 |
| M16 | 清重门槛 `>1` → `>2` | I4/存量2条默认 | 只有"2 条"那档红、"3 条"档仍绿 —— 分档跑的收益就在这 |
| M17 | 清重分支短路（`if false`） | I4 | 索引建不起来（唯一冲突），双默认复现 |
| M18 | 清重保留"最新"一条 | I4 | 留下 `batchm-rep-newest` |
| M19 | 索引去掉 `WHERE is_default` | I3 | 插第 2 台**非默认**存储就 23505 |
| M20 | 删掉 `migrate.go` 里的钩子调用 | I5 | 调用出现 0 次 |
| M22 | 清重语句 `id <> (…)` → `id IN (…)` | I4 | 把要保留的那条也降级了，默认行 = `[batchm-rep-newest]` |

编号跳过 M21 —— 它是本批唯一**登记为等价、且刻意不进电池**的一格（`CELLS` 里没有它，因此也没有 `M21.log`；它的"杀不动"是读钩子分支推出来的，不是跑出来的，读者不要把它当实测结果）：

**M21「去掉 `CREATE UNIQUE INDEX` 的 `IF NOT EXISTS`」＝等价，未设防**。钩子第二次跑会报错，但该错误只落进 `logger.Warn` 后 `return`：索引仍在、启动不炸，所有可观察判据不变 ⇒ 没有任何腿杀得动。要杀只能断"重跑不产生 Warn"，那需要把 logger 输出拉进断言面（`internal/pkg/db` 没有 logger 捕获夹具）。**代价如实记**：本批接受"`IF NOT EXISTS` 被删只剩启动日志多一条 Warn"这一差异不设防；它不是功能塌，但也不再假装是"已被测过的性质"。

> **这条登记已由批M-4 撤销（2026-09-21）**：那面"logger 捕获夹具"被做了出来（`os.Pipe` 换 `os.Stdout` + `logger.InitLogger` 指向它），M21 因此进了电池、由 S12 杀掉，红因是消息级的：`第二次启动报了建索引失败（启动日志长期带这条噪音，运维再也看不见别的 Warn）`。上面那段推理当时是对的（在**没有夹具**的前提下这条差异确实不可断言），留下的可复用结论是它的**方法论**那一半：一条"等价、无腿"的登记必须同时写清"要杀它需要先得有什么"—— 有了那件东西，登记就该被重跑而不是长期挂着。批M-4 的全部新腿就是按这句话挑出来的。见 §23.11。

**两格的返工记录（判据：BROKEN 修变异，不改 expect）**：M12 第一版把整个 `if` 块删掉 ⇒ `config` 成为未使用变量 ⇒ 整包编译失败（红因 `internal/service/obs_config.go:194:2: declared and not used: config`。**这行现在是跑出来的**：收口时把那一版变异在最终文件上重放一次，取到上面的编译器原文后立刻 `cp` 还原，还原后 md5 与备份逐字一致、`find . -name "*batchm-bak*"` 为空；原文留档 `/tmp/repro_m12_build.log`。本段此前抄的是 `:188:2` —— 那是第一轮（批M-2 之前的 428 行版本）的行号，同一句编译器原文在 434 行的最终版本上是 `:194:2`。抄编译器输出必须连"在哪一版内容上跑的"一起写，否则行号会随文件生长悄悄失真），改成"留着形状、只把返回值换成 nil"才测到判据本身；M15 第一版只打三处补丁 ⇒ 服务测试的替身缺 `ClearDefault` 方法 ⇒ 整包 type-check 失败（同样**已重放取证**：`go vet ./internal/service/` 报 `internal/service/obs_config_batchm_test.go:164:33: cannot use repo (variable of type *batchMObsRepo) as repository.ObsConfigRepository value in struct literal: missing method ClearDefault`，rc=1；跑前逐文件 `cp` 备份、跑后 `cp` 还原并比对 md5，三份均与 §23.7 表中值一致；原文留证 `/tmp/repro_m15_vet.log`（含 `rc=1` 首行），两次重放的备份留在 `/tmp/repro_m15/`（M12 那份是 `m12-repro-orig.go`），未进电池自己的备份目录 `/tmp/batchm_battery/bak/`，免得污染它的四文件基线。**"4 处 struct literal"这个数是第一轮（S5 之前，替身被 4 条腿用）的，最终内容上是 5 处**（`:164 :196 :220 :297 :328`）；而 `go vet` 只打印第一个出错点，所以"几处"必须自己数，不能拿工具输出当计数），补第 4 处后 S1b 才以"clearCalls = 1"红。两格第一次都被脚本判成 `SURVIVED-or-BROKEN` 而非 KILLED —— 这正是"红因必须是**指定腿**的腿红"这条判据在起作用：`rc != 0` 单独看的话，29 格会被当成 31 格全"杀"。

**电池自己也会假绿：一轮"26 格全 KILLED"是环境红冒充的（已加判据）**。批M-2 复跑时先从一个**没导出 `POSTGRES_TEST_*`** 的 shell 起电池，脚本照样报了 `KILLED M28 / KILLED M29 / 未杀格 = 无` —— 打开日志一看，仓库包**每一条腿**都红，红因是 `testdb.go:288: 初始化进程级测试库失败: … failed SASL auth: FATAL: password authentication failed for user "admin"`：判据只要求"指定腿出现 `--- FAIL`"，而环境红让所有腿都 FAIL，指定腿自然也在其中。这类假绿的危险在于它**长得和真结果一模一样**（rc=1、mtime 新、汇总行漂亮）。修法不是"记得导 env"（会再忘），而是把判据补严：驱动现在在 verdict 之前先扫日志里的 `初始化进程级测试库失败 / password authentication failed / connection refused / no such host`，命中即判 `ENV-BROKEN` 并计入未杀格 ⇒ 环境不成立的那一格**不可能**被算成杀。复算（带 env 重跑）后的红因才是上表 M28/M29 那两条，且两格红因集合互不相同。

**证据归档状态（本轮已闭合）**：上一版这里登记过一条尾巴 —— `ALL.log` 停着第一轮汇总（`未杀格 = ['M12', 'M15']`），第二轮那两格只按 `sys.argv[2]="M12,M15"` 单跑、汇总行打在终端未落文件，只能靠 mtime 推断。收口复跑改成**每包一份 `ALL-<包>.log`**：repository / service / db 三份各自含逐格 `KILLED …` 行与末尾的 `BATTERY <包> done, 未杀格 = 无` + `battery_<包>_rc=0`，M12/M15 如今在归档的汇总行里直接读得到，不需要再靠时间戳猜。

### 23.5 顺带发现，本批只登记不修（N-30 与 N-31）

`file_count` / `total_size` 只有**管理页上传**这一条路径会写。另外两条真实写存储的路径只读默认、不写用量：

- `internal/content/service/material.go:93-111`（素材上传）
- `internal/service/channel_media.go:71-72`（渠道入站媒体转存 —— 也就是本批选取面真正的大宗消费者）

⇒ 管理页上那台存储的"已用 N 个文件 / M 字节"只统计管理页自己传的东西，**用得越多偏差越大**。这是统计口径缺陷，不影响选取正确性。

为什么本批不顺手补：`channel_media.go` 正压着并行泳道的未提交改动（`git status` dirty；批F-4f/批I 的历史改动也在同一文件），同文件叠刀会让"哪条红归谁"分不清 —— 与 N-25"待删"那一条用的是同一条纪律。补法现成：两处各一行 `cfgRepo.IncrementUsage(ctx, cfg.ID, header.Size)`，配一条"转存之后计数确实涨了"的腿（S2 的替身形状可直接复用）。已登记 §5 N-30，建议与批F-4 的入站媒体面同批做，省一次门禁。

**N-31（做 ③ 的另一半时核出的编辑口两个残留，同样只记不修）**：① `service.UpdateConfig` 的合并段用 `if req.X != ""` 逐字段判空 ⇒ 12 个可编辑列**全都清不空**（`domain`/`endpoint`/`config` 填过就改不回空值，而"去掉自定义域名"是真实运维动作）；② `last_error` / `last_test_at` 两列**没有任何生产写口** —— `TestConnection` 只把错误返回给调用方、一次都不落库，管理页那两栏恒空。①要改 `UpdateObsConfigRequest` 的字段类型（指针或显式 `null`），那是**对外 JSON 契约变更**、前端得同步；②要先定"测试失败要不要写进配置行、成功时抹不抹"的产品口径。两条都不该由审计批替答，登记在 §5 N-31。

### 23.6 勿删 / 勿放松（下一位读者请先读这一段）

1. **不要把切换默认的两句话拆回服务层** —— `SetDefaultConfig` 里只许出现一次仓储调用（S1b 就是为这一格写的，拆回去必红）。
2. **不要给 `ObsConfigRepository` 加回 `ClearDefault`** 或任何"只清不置"的入口。
3. **不要删 `GetDefault` 的 `Order("created_at ASC")`** —— 在"最多一条默认"由索引保证之后它看着像废代码，实际是钩子清重语句的镜像；两边取序一分开，清重就会把默认从"文件在的那台"切到"文件不在的那台"（R5 + I4 双钉）。
4. **不要把 `IncrementUsage` 的 `UpdateColumns` 换成 `Updates`/`Save`**（R3c / S2）。
5. **编辑口 `Update` 的白名单是它的唯一防线**：不许加列（尤其 `is_default` / `status` / `file_count` / `total_size` —— 前三组各有各的属主，M23/M24/M27 各钉一张）、不许换成 `Omit` 黑名单（黑名单只挡住这次点名的两列，用量列与"无主键时 `Save` 退化成 INSERT"全漏，M27 实测就是这一格）、也不许因为"白名单要维护两遍"而合回 `Save`。**新增模型列时这里默认不动** —— 想让它可经编辑口写，就同时给 R6 加一条断言。
6. **不要去掉索引的 `WHERE is_default`**（I3），也不要为了"让 GORM 标签能表达"把它降级成普通列标签 —— 那等于放弃 partial 语义，第二台非默认存储直接建不出来。
7. **清重门槛保持 `dups > 1`**、保留条件保持 `ORDER BY created_at ASC, id ASC`（I4 两档都跑）。
8. 钩子失败一律 `Warn` + `return`，**不要升级成 panic**：建不成索引的后果是"少一层兜底"，panic 的后果是整个服务起不来（与 `postMigrateOpportunityClueUniqueIndex` 同一口径）。
9. **新增测试库用例时别假设 post-migrate 钩子跑过**：`testutil.NewTestDB` 只做 AutoMigrate。要测索引影响，像 R4 那样在腿内显式建，或直接调钩子（`internal/pkg/db` 包内）。
10. 真实库里那道索引**尚未存在**（`SELECT count(*) FROM pg_indexes WHERE indexname='idx_obs_config_single_default'` = 0，因为跑着的进程是本批之前的二进制）。下一次启动才会由钩子建起来；如果那时启动日志里出现"创建失败"，根因按钩子注释的口径查（存量重复已在 `:483-492` 处理，失败通常是权限或表不在）。
11. **删除口的判据在那条 SQL 里，不许搬回服务层** —— `DeleteNonDefault` 的两半各被一格钉住：`AND is_default = false`（M28）与 `RowsAffected == 0 ⇒ 报错`（M29）。`DeleteConfig` 里那次 `GetByID` 只负责"不能删除默认配置"这句人话（M30 钉的是文案），把它当安全判据搬回去 = §5 N-33 的窗口重新打开。入口**改名**也是判据的一部分：以后要"连默认行一起删"（清库、重建）必须显式另开一个入口并在 §5 记一条，不许顺手放宽这条 WHERE。

### 23.7 门禁与证据（收口复跑，2026-09-21，全部在**含批M-2 的最终内容**上跑）

先钉"跑在哪份内容上"，否则下面的数字没有指涉。这一份是本批**第二次**全量复跑：第一次（§23 前六节的数字）跑在批M-2 之前，md5 与腿数都已作废，旧汇总行挪到了 `/tmp/batchm_battery/round1-26cells/` 单独存 —— 一份"看着就对"的过期汇总比没有汇总更危险，所以不留它在原位。

| 文件 | 行数 | md5 |
|---|---|---|
| `internal/repository/obs_config_default_batchm_test.go`（新） | 553 | `7387ce1968ef6f6572888405a3343949` |
| `internal/service/obs_config_batchm_test.go`（新） | 365 | `57d01234bcfa684965120c1274afcddf` |
| `internal/pkg/db/obs_config_default_index_batchm_test.go`（新） | 217 | `a13782bd88cba9cab01409b963292ac0` |
| `internal/repository/obs_config.go`（改） | 216 | `75a9504911bb1650070828024a8fdaec` |
| `internal/service/obs_config.go`（改） | 434 | `9cfc4c1fa4e9bfa7c09fc66e41a074c0` |
| `internal/pkg/db/migrate.go`（改） | 593 | `2e30cba80eb6c24c178d2a36c15a3ca6` |
| `internal/repository/obs_config_test.go`（改：删 `ClearDefault` 腿、`Delete` 改 `DeleteNonDefault`） | 452 | `ab43b51d286e34203b07cd0556c7781e` |

三份新腿合计 **1135 行**，22 条顶层腿。命令统一在 `cd user-server && set -a && . ../.env && set +a; export POSTGRES_TEST_PORT=8232 POSTGRES_TEST_PASSWORD="$POSTGRES_PASSWORD"` 之下；本轮日志在 `/tmp/batchm2_gates/`（静态门、控制组、`ALL-<包>.log`），逐格日志在 `/tmp/batchm_battery/<格号>.log`。

| # | 命令 | 实测结果 |
|---|---|---|
| 1 | `gofmt -l <上述 7 个文件>` | `rc=0`，`gofmt.log` **0 字节**（0 个待格式化文件） |
| 2 | `go build ./...` | `rc=0`，`build.log` 0 字节 |
| 3 | `go vet ./internal/repository/ ./internal/service/ ./internal/pkg/db/` | `rc=0`，`vet3.log` 0 字节 |
| 4 | `go vet ./...`（整模块） | `rc=0`，`vet_all.log` **0 字节** |
| 5 | 电池控制组（未打补丁，`-run TestBatchM_` 过滤集） | repository `rc=0 pass=12 fail=0 skip=0`；service `4→5 pass=5 fail=0 skip=0`；db `rc=0 pass=5 fail=0 skip=0` |
| 6 | 变异电池 29 格 | `ALL-repository.log` 18 行 `KILLED`（16→18，加 M28/M29）+ `ALL-service.log` 5 行（4→5，加 M30）+ `ALL-db.log` 6 行 = **29 行 `KILLED`**；三份末尾各一行 `BATTERY <包> done, 未杀格 = 无`，`battery_<包>_rc=0` |
| 7 | `go test -p 1 -count=1 -timeout 2400s -test.v ./internal/repository/` | `rc=0 pass=798 fail=0 skip=0` `ok hivemtk-user/internal/repository 94.648s` |
| 8 | 同上 `./internal/pkg/db/` | `rc=0 pass=29 fail=0 skip=0` `ok hivemtk-user/internal/pkg/db 15.055s` |
| 9 | 同上 `./internal/service/` | `rc=0 pass=3792 fail=0 skip=3` `ok hivemtk-user/internal/service 466.165s` |
| 10 | 工作树残留检查 `find . -name "*batchm-bak*"` | **0 个**（备份统一写在 `/tmp/batchm_battery/bak/`，四个文件：三处产码 + 服务测试替身那份 —— M15 要给它补 `ClearDefault` 方法） |

**#5 与 #7~#9 都要跑，不是重复劳动**：控制组走 `-run` 过滤集，只证明本批腿自身成立且不吃包级状态；#7~#9 是**全量、不带 `-run`**，同包既有实现一起跑，才会暴露"本批腿改写了包级状态"或"既有腿依赖了本批改掉的入口"——#7 的 798 与 #9 的 3792 里就含着既有 `TestObsConfigRepository_Delete`（它调的就是被改名的 `DeleteNonDefault`，`full-repository.log:1444` 逐字 PASS）。过滤集只用于迭代，无过滤全量才是门。

**#4 全绿要说清范围**：整模块 `go vet ./...` rc=0 只保证"本批没给模块引入 vet 告警"，**不等于**全模块测试通过。全量 `go test ./...` 本批**没跑** —— `internal/service` 单包已 466s，全量必撞默认 timeout（项目既有口径），所以下刀面按 #7/#8/#9 逐包跑足，未下刀的包不宣称已验。

**SKIP=3 逐条点名**（不点名则 `fail=0 skip=3` 是空断言：`testutil.NewTestDB` 在本地连不上 PG 时走 `t.Skipf` 而非 Fatal，只看 rc 会把"没跑"读成"跑绿"）：`TestAIAgent_AssetBundleBinding`（`full-service.log:148`）、`TestAIAgent_FullChain`（:151）、`TestPlatformAccountService_Login`（:9858）—— 三条与上一轮**同名同行号**，都不是本批文件、也与 obs 无关。本批 5 条 service 腿在这同一份全量日志里逐条 `--- PASS` 读得到（含新增的 `TestBatchM_DeleteConfigCannotRemoveDefault`），repository 12 条、db 5 条同理，两包 skip=0。

**活库取证复算（两条 `psql -U admin` 均 rc=0、stderr 空）**：

```
PGPASSWORD="$POSTGRES_PASSWORD" psql -h 127.0.0.1 -p 8232 -U admin -d user_db -Atc \
  "SELECT count(*) FROM pg_indexes WHERE indexname='idx_obs_config_single_default'"   → 0
… -Atc "SELECT status, is_default, count(*) FROM obs_config GROUP BY 1,2"             → active|t|1
```

⇒ 索引在真实库里还不存在。这条 `0` **不是**"钩子没生效"的证据，而是"钩子还没被跑过"：当前监听的进程是本批之前的二进制，索引会在下一次携带本批改动的启动里由 §23.2 的钩子建起来（M20 杀的是包内静态锁 I5，**不等于**跑通一次真实启动 —— 见 §23.8 第 3 条）。

**一条与本批同源的取证纪律（本轮现场学到的）**：这批 M28/M29 第一次跑出来是"两格 `KILLED`、`未杀格 = 无`"，而它是**假的** —— 那一次从没导出 `POSTGRES_TEST_*` 的 shell 起电池，测试库连不上，仓库包每一条腿都红，指定腿自然也在其中。判据补严之后（`ENV-BROKEN` 直接计入未杀格，见 §23.4）带 env 复跑，才拿到上表 #6 与 §23.4 里 M28/M29 那两组**互不相同**的红因。危险不在"忘了导 env"，而在"忘之后结果长得和对的一模一样"。

### 23.8 本批未覆盖面（不得当作已验证）

1. ~~**`service.UpdateConfig` 的合并语义零腿**~~ **已由批M-3 补腿**（S6 + 格 M31/M32，见 §23.10）：合并段换成"整个请求覆盖"、或整段删除（="整个请求丢弃"），现在各有一条红腿点名到列。R6/R6b/R6c 守仓储白名单、S6 守服务层合并段，两半现在都在。**残留未覆盖的是 N-31① 本身**："12 个可编辑列全都清不空"是**现行行为**而不是回归，所以 S6 刻意不钉它（钉了就把待排的产品口径变更写成了不可改的契约，见 §23.5 N-31 与 §5 N-31）。
2. ~~**`TestConnection` 整条零腿**~~ **已由批M-3 补腿**（S7/S8/S9 + 格 M33–M43，共 11 格）：四家云厂商的三字段判空、local 臂的四档探测（不存在／不是目录／不可写／通过且不留探测文件）＋ env 回退、以及"测试连接一次都不写库"，全都有腿。**残留未覆盖的两件**：① N-31② 的 `last_error`/`last_test_at` 落库依然零腿，因为**生产里根本没有写口**（S9 反过来把"不写库"钉成了判据）；② "驱动真的连通"没有腿也不该有腿 —— `storage.Factory` 对四家云返回 stub，`TestConnection` 能证到的天花板就是"三字段非空"，这一条记成 §5 **N-34**，S7 的腿注里同步写明了含金量边界。
3. ~~**钩子没有被"走一次真实启动"验过**。I1–I5 是包内直调函数 + 一句静态锁（`migrate.go` 里那句调用出现 1 次、位置在 `missingTables` 终校验之后）。`testutil.NewTestDB` 只做 AutoMigrate、不跑 post-migrate 钩子（§23.6 第 9 条），所以"下一次启动真的会建出索引"这句话的证据强度是**读装配点**，不是跑通一次启动~~ **已由批M-4 补腿**（S11/S12/S13 跑 `AutoMigrate()` 本体 + 格 M52–M55 与本批新进电池的 M21，见 §23.11；`testutil` 那句"不跑钩子"至今成立，批M-4 靠的是腿内自建全新库 + 自己调本体，不是把夹具换掉）。这一条**还顺手往上挪了一层**：`cmd/api/main.go` 里 `db.InitDB()` / `db.AutoMigrate()` 那两句此前同样零判据 —— 它一旦少一行／换序／调两遍，本包所有 db 腿照绿（它们自己调本体），而生产库里三条 post-migrate 守卫（obs 单默认、message_hub 三元组、opportunities clue_id）一起不存在。批M-4 把它钉成 S15 + 格 M56–M59，**残余边界写在 §23.11 第 4 段**（"整行 = 一个制表符 + 语句"的行计数挡得住删句、调两遍、换序、挪进多行块；挡不住 `if false { db.AutoMigrate() }` 那种**单行包装**）。这一格交卷时先活了两格（M57、M59），两处都是判据/注码自己的缺陷，写在 §23.11 第 3 段。
4. **批M-2 的并发交错只在替身层模拟**：S5 用 `beforeDelete` 铺"预读之后、落库之前被抬成默认"，替身里那条 `if c.IsDefault { return ErrRecordNotFound }` 是真库那条 WHERE 的镜像。真库两个事务交错的腿**没有**。⇒ "零默认在真库上做不出来"这句话靠的是 ① 每条写语句自身的判据（R7/R2b/R6）＋ ② 对 `obs_config` 全部写口的逐个枚举（§5 N-33 证据格），不是并发压力测试。这不是等价强度，读者按这个强度取用。
5. **S5 第二段断的是文案**（错误里要点名"默认"）：改文案会红，而它不表征安全性质 —— M30 就是照这一格设计的，别把它读成"安全判据被测过"。
6. ~~**电池格子之外不设防**（写本节前是 29 格，批M-3 之后是 48 格，见 §23.10；下面两条到今天仍然无格）：M21（去掉 `IF NOT EXISTS`）已登记为等价且无腿；`model/obs_config.go` 的列默认值（`Status default:'active'`、`IsDefault default:false`）无腿 —— 建配置走 `Create` 前服务层已显式赋值，所以库侧默认值今天没有承重，但"没有承重"这件事本身没被测过~~ **两条都已有格**（批M-4，电池 48→59，见 §23.11）：M21 进了电池并被 S12 杀掉 —— 批M 当年说"要杀只能断'重跑不产生 Warn'，而 `internal/pkg/db` 没有 logger 捕获夹具"，批M-4 就是把那个夹具做了；列默认值由 S14 三段 + 格 M50/M51 钉住，而**当年那句"库侧默认值没有承重"实测只对一半**，正确的分层结论写在 §23.11 第 5 段（那是一句结论级更正，别只看见划掉的两条线）。
7. **真实库里那道索引还不存在**（§23.7 活库取证 = 0）。本批能证明到"钩子会建它 + 存量双默认会被清重"，不能证明"生产上已经拦得住第二条默认"。批M-4 之后**再测一次仍是 `0`**（`idx_obs_config_single_default` 计数 0、行数 1、默认行 1），且仍**不关**：补这条要真启动一次携带改动的 API 进程，会把别的泳道未提交的迁移一起推进共享开发库，代价不对（见 §23.11 第 9 段）。
8. ~~**另外两条 post-migrate 守卫仍零腿**~~ **message_hub 那一条已由批M-5 补腿**（S16/S17 + 格 M60–M66，见 §23.12）：现在"启动会清掉两道窄唯一性""三元组索引的列清单就是那三列""重跑静默"三句话都有断言读，钩子那句"已就绪"日志也被 S17 逐字读了。~~**剩下的另一半仍然开着**~~ **opportunities 那一条已由批M-6 补腿**（S18/S19 + 格 M70–M76，见 §23.13）：`postMigrateOpportunityClueUniqueIndex` 今天有两条跑 `AutoMigrate()` 本体的腿，"下一次启动真的会建出那道 partial 索引"与"存量真有重复时它只报不改"两句话都有判据；那句"旧那四处包内直调全都测不到装配点"也已从读码升级为量出来的（M70 无过滤复跑：七条 `TestOpportunity*` 全绿、只有新两条红，§23.13 第 4 段）。**这一条与登记时的方子有一处偏离**：§23.12 第 9 段开的是"静态锁 + 跑本体"两条，本批只做跑本体（跑本体一次同时钉 presence 与 position，计数式锁对"语句还在、被包进恒假分支"是瞎的），偏离的理由与代价写在 §23.13 第 3 段。另：批M-5 顺带量出**这条钩子自己三句 DDL 的承重面并不对称**（其中一句与 GORM 的列级对账等价、一句被模型标签空转），这件事写在 §23.12 第 3 段，读那条守卫时按那个强度取用；而 §23.13 第 6 段量出**商机这一条的不对称方向正相反**（`clue_id` 没有索引标签 ⇒ 钩子是形状的唯一事实源 ⇒ 同形状变异在那边是等价格、在这边是普通格），两条一起读。

### 23.9 批M-2 的来路：为什么最后一口不是又一条登记

写 §23.8 时按"选取面还剩哪些没腿"逐个口过，`DeleteConfig` 本来排在"未覆盖"清单的第二条：它的守卫**存在**（服务层 `if config.IsDefault { return errors.New("不能删除默认配置") }`），看上去"已经防住了"，于是要不要登记成缺陷就变成了判据问题。三条取证把它从"未覆盖"改成了"缺陷"：

| 取证 | 结果 |
|---|---|
| 读守卫的**位置** | 守卫在**读到的快照**上，不在那条 `DELETE` 上：`Delete` 本身是 `WHERE id = ?`，无 `is_default` 谓词 |
| 问"预读与落库之间的窗口谁挡" | 没人挡：批M 之后 `SetDefault` 是一个事务、提交即生效，正好能插进那两句之间（批M 之前不需要并发，事务外先清后设自己就留了个更大的窗口） |
| 问"落下去能不能自愈" | 不能：`InitDefaultStorageIfEmpty` 只在 `Count == 0` 时 seed（`init_storage.go:29-31`），而这条路径删完表里还有别的行 ⇒ 与 N-26② 同一条"全断且无自愈" |

⇒ 它与本批**已修**的 N-26② 同形（同一枚硬币：写侧的原子性归一条语句），差别只在触发要两次管理页写操作交叠而不是单击。"登记但不修"在本批只用在两类地方：文件压着并行泳道改动（N-30/N-32），或修法要改对外契约/产品口径（N-31）。这一口两条都不占，且修法与本批既有口径同构（谓词 + 0 行判定，两条腿三条断言），所以当场修掉、记成 §5 N-33。**若当时按 §23.8 的原稿登记成"未覆盖"，本批交付的"选取面收口"就还剩一扇开着的门，而文档会把它读成已知边界。**

同时留下的一条纪律：把删除口从"未覆盖"里划掉之后，本节重数了一遍这条分界线 —— **未覆盖**是"实现正确、测试没到"（本节第 2、3、5、6、7 条，其中第 4 条是强度声明不是缺陷），**缺陷**是"实现本身不成判据"（删除口原本就属于这一类）。把后者写进前者，是一份审计文档最体面的漏判方式：读者会以为边界是"还不知道"，而它其实是"知道了没写"。

### 23.10 批M-3：把 §23.8 的第 1、2 条从"未覆盖"划成"有腿"（产码零改动，电池 29→48 格）

**先纠一处编号，因为它正是这条分界线自己该抓的那类错**。§23.9 收尾把"未覆盖 vs 缺陷"立成判据，并列出"本节未覆盖是第 2、3、5、6、7 条"—— **漏了第 1 条**（`UpdateConfig` 合并语义零腿）。第 1 条恰恰是"实现正确、测试没到"最纯的一例，把它从枚举里漏掉的后果不是少一个数字，是这份文档读起来像"合并语义已经归到缺陷那侧处置过了"。正确枚举：§23.8 的未覆盖是 **1、2、3、5、6、7** 六条（第 4 条是强度声明、不是缺陷）。批M-3 的边界就划在这里：**只做"测试没到"那两条里能纯靠加腿解决的部分**，凡是要动对外契约（N-31① 的字段类型）或要定产品口径（N-31② 的写回时机）的一律留在 §5，不夹带。

**产码 0 改动，且这一句是可核的**：三处产码文件 md5 与 §23.7 表逐字节相同 ⇒ 本批任何一条红的责任都只在测试面，也意味着"补腿"没有顺手改实现来让腿变绿。

| 文件 | 行数 | md5 | 相对 §23.7 |
|---|---|---|---|
| `internal/service/obs_config_batchm3_test.go`（**新**） | 519 | `3a193553fc7d7ad2bb3ced395486a6ec` | 本批唯一新增文件 |
| `internal/service/obs_config.go` | 434 | `9cfc4c1fa4e9bfa7c09fc66e41a074c0` | **相同** |
| `internal/repository/obs_config.go` | 216 | `75a9504911bb1650070828024a8fdaec` | **相同** |
| `internal/pkg/db/migrate.go` | 593 | `2e30cba80eb6c24c178d2a36c15a3ca6` | **相同** |

**五条腿接续 §23.4 的 S 序列**（"子档"是跑出来的 `--- PASS: 腿/子档` 计数，不是 `t.Run` 的字面个数 —— 四家厂商 × 四档那种是循环铺出来的）：

| 腿 | 钉住哪段此前无人看守的实现 | 顶层 / 子档 | 变异格 |
|---|---|---|---|
| S6 `TestBatchM_UpdateConfigIsPatchNotReplace` | 合并段（`service/obs_config.go:119-154`，12 个 `if req.X` 分支）的**两个失效方向**：整段条件变恒真 = 整个请求覆盖（没给的列被抹成零值）；整段删除 = 整个请求丢弃（点了保存什么都没变） | 1 / 0（平铺：9 列 kept + 3 列 changed + `status`/`is_default`/用量三组不动 + `len(repo.updated)==1` + DTO 两半各断） | M31 M32 |
| S7 `TestBatchM_TestConnectionCloudArmsRequireThreeFields` | 四家云厂商臂的真实判据只有"AK/SK/Bucket 三个串非空"，且**每家必须报自己那一句**（`七牛云存储配置不完整` 这类厂商名是运维唯一的定位信息）；另钉"缺 `region` 不拦"与"越词表的 provider 必须报错" | 1 / 18 | M33–M38 |
| S8 `TestBatchM_TestConnectionLocalProbesDirectory` | local 臂是真的在探文件系统（`os.Stat` + 写一个 `.obs_test_write` 再删）：目录不存在／路径是文件／不可写／通过且**不留探测文件**，外加 `STORAGE_LOCAL_BASE_DIR` 回退链 | 1 / 5 | M39–M42 |
| S9 `TestBatchM_TestConnectionDoesNotWrite` | 纯校验口**一次都不写库**：4 次调用（含 2 次失败路径）后替身四个写计数器之和 = 0，并对被传入的 config 做值比较（防止"就地改入参"这种不写库但写快照的形态） | 1 / 0 | M43 |
| S10 `TestBatchM_CreateConfigValidatesAndSeedsDefault` | 建配置口三条判据：云厂商三字段必填（校验失败**不得建行**）、local 兜底值 `./uploads` + `/files`（与 `init_storage.go` 的 seed 同口径）、**只有表里第一行**自动成默认且新建行必 `active`（"装完系统有没有一个能用的默认存储"就悬在这两句上） | 1 / 8 | M44–M49 |

**腿号的一处提醒（写本批时才发现的两套重号）**：本节 S6–S10 接续 §23.3 的 S 序列，而那条序列本身**跳过 S4**（§23.3 表里是 S1、S1b、S2、S3、S5 五条，没有 S4，本轮不回填、也不为对齐去改号）；另 §11 那张错误码电池表里的 S1–S20 是**变异格**号、与腿号无关。两套 S 不通用 ⇒ 跨节引用请连节号一起写。

**三条"刻意不断"，请连同腿注一起读，别把它们当遗漏**：

1. **S6 不断"能不能把一列清回空值"**。那是 §5 N-31① 记着的缺陷（判空即"未提供" ⇒ 12 个可编辑列全都清不空）。钉住现行行为 = 把一个待拍的对外契约变更写成测试保护的既成事实。腿注里写死了改法与顺序：谁实现 N-31①，必须**连带把 S6 的期望翻成"空串 = 清列"**。
2. **S7 是含金量声明，不是成绩单**。`storage.Factory` 对四家云返回 `cloudStub`（SDK 未接线，六个方法各自返回 `SDK not wired yet`），构造永不失败 ⇒ 那一支里 `驱动构造失败` 分支在 provider 已被外层 `switch` 收窄之后**不可达**，本腿不为不可达分支编断言；而"三字段齐 ⇒ `nil`"这句的真实含义是"三个字符串非空"，不是"连上了"。controller 拿到 nil 就回 `"连接测试成功"`、管理页照原文弹绿色提示 ⇒ 新登记 **§5 N-34**（本批只登记不修：修法是接真 SDK 或把文案改成"配置格式校验通过"，前者是产品排期、后者是对外文案口径）。
3. **S9 不断"测试失败该不该写进 `last_error`/`last_test_at`"**。那两列今天零生产写口（N-31②），补写回是**修法**不是缺陷；S9 反过来把"不写库"钉成判据。腿注同时写明：将来实现它必须新开一个只写那两列的入口（像 `UpdateStatus` 那样），届时把 `len(repo.updated) != 0` 改成"只许走那个新入口"，**不许**顺手放宽成"用 `Update` 写也行"—— 那等于把 §23.2 收掉的"陈旧快照整行写回复活 `is_default`"（R6 那一族）重新放回来。

**门禁 48 格全杀（18 repository + 24 service + 6 db），但这一句的取得过程包含三次真实的失败，先把失败记完再记结论**：

- **第一次：M39 报 `SURVIVED-or-BROKEN`**。日志里 `rc=1` 却没有 `--- FAIL`，只有 `[build failed]` + `declared and not used: info` —— 变异体把 `if !info.IsDir()` 整块摘掉，而那一句是 `info` 的唯一使用点。这不是"变异存活"，是**变异根本没跑**（§23.4 的 BROKEN 同类，只是这次是"删使用点"而不是"删来源"）。改法保住引用、只让判据恒不成立：`if !info.IsDir() && false {`。复跑后红因正好落在期望的那一子档上，且它给的是**文案级**证据：`错误 = local 存储目录不可写: open …/a-file/.obs_test_write: not a directory，期望「不是目录」`—— 与 §23.4 的口径一致，杀得对不只要看名字，要看它红在哪句话。
- **第二次：中途 `pkill` 电池，把变异残迹留在了树里**。为躲并行泳道当时弄脏的 `internal/service`（对方 `bridge_offline_replay.go` 改了签名、测试还在按旧签名调，活树 `go vet` 报 build failed），本批的门与电池整体搬进 `--shared` 影子克隆 `/tmp/shadow-m3/hivemtk`。第一趟电池被 `pkill` 打断后，影子树里留下了两处未还原的变异（repository 的 `total_size` 绝对赋值、`migrate.go` 少一行钩子调用），于是下一趟开局就报 5 个 `ANCHOR-BAD`。两条纪律已固化进工具：① 电池驱动脚本每次开跑前**逐文件比 md5**（8 个文件任一漂移直接 `exit 8`，不跑）；② `pkill` 只杀得到 python，杀不到包它的 bash ⇒ 后半段照样起跑，判断"这趟到底跑没跑"要看驱动日志最后一个 echo 与文件 mtime，不能只看进程表。

- **第三次：一盘"编译失败"其实是磁盘写满**。`go build ./...` 报 `rc=1`、日志 635 字节，红因是 `link: running strip failed … can't write output file … (No space left on device)`，而**同一棵树同一时刻 `go vet ./...` 是 `rc=0`**（vet 只做类型检查、不做最终链接）⇒ "vet 绿 + build 红"这一组合本身就是磁盘/工具链层的信号，不是代码的。处置：清掉本审计自己前几轮留下的影子克隆与 `GOCACHE` 目录（先 `lsof` 确认零打开句柄，`OPEN_HITS=0` 才删；十项 `du -sh` 相加 ≈17.7G；日志目录与本批影子树一个不动），磁盘从 `581Mi` 可用回到 `18Gi`，四项静态门在**同一份内容**上重跑全部 `rc=0`。上一段那批 `ANCHOR-BAD` 与这一段这次 `build_rc=1` 是同一类错误的两个方向：**门的红与门的绿都要先归因到环境，再决定归给谁。**

**门跑在影子树上，因此绝对计数不与 §23.7 可比 —— 差异逐条闭合，不留"差不多"**：影子树 = HEAD `cc065b02` + 本批 8 个文件，不含别人**未追踪**的在飞文件。`internal/repository` 本批读到顶层 797，§23.7 记 798；差额核到 `internal/repository/help_center_public_list_test.go`（`git status` = `??`，`grep -c '^func Test'` = 1）⇒ 797 + 1 = 798，闭合。**计数口径本批统一写明**：`top` = `^--- PASS`（顶层腿），`sub` = `^    --- PASS`（子档），两者不相加时不引用来比较。

| # | 命令（影子树 `cd user-server && set -a && . …/.env && set +a; export POSTGRES_TEST_PORT=8232 POSTGRES_TEST_PASSWORD="$POSTGRES_PASSWORD"`） | 实测 |
|---|---|---|
| 1 | `gofmt -l <8 个文件>` / `go build ./...` / `go vet <三包>` / `go vet ./...` | 四项 `rc=0`、日志 0 字节。跑了两趟：`07:41–07:42` 与最终内容上的 `08:06:18`（后者即表 6 那一趟） |
| 2 | 控制组（`-run TestBatchM_` 三包） | repository `rc=0 top=12 sub=0 fail=0 skip=0`；service `top=10 sub=31 fail=0 skip=0`；db `top=5 sub=2 fail=0 skip=0`。service 那一趟在最终内容上复跑过（`07:55`，同口径 `top=10 sub=31 skip=0`） |
| 3 | 变异电池 48 格（**三趟，全部落在最后一次编辑之后的同一份内容上**） | ① service 24 格 `07:55–08:00`、② repository 18 格 + ③ db 6 格 `08:16–08:17`（影子树逐文件 md5 预检 `PREFLIGHT-CLEAN 8/8` 通过后才起跑）。三趟均 `KILLED` 满格、末行各 `未杀格 = 无, 收尾残留 = 无`，`rc=0`；逐刀日志按轮次分目录存（`/tmp/battery-m3-shadow-r2/`、`-r3/`，不覆写首趟）。`--check` 锚点全 `ANCHOR-OK`、`--subs` 报"现存子档 31 个，指定子档 全部命中"（子档名必须真跑核对：Go 会把空格换成下划线，拼接出来的名字不算证据）。**为什么复跑而不是沿用首趟**：本批最后一次编辑（S8 腿注 `三档`→`五档`）发生在 `07:45`，而首趟电池在 `07:2x–07:3x` ⇒ 按"改过用例后逐刀复跑"的口径重跑；repository/db 那两包其实编译不到 service 包里的那行注释，复跑是为了让"48 格跑在最终树上"这句话字面成立，而不是为了换个数字 |
| 4 | 无过滤全量 `go test -p 1 -count=1 -timeout 2400s -test.v ./internal/repository/` | `rc=0 top=797 sub=385 fail=0 skip=0`，`ok … 137.753s`；本批 12 条 repository 腿在同一份日志里逐条 `--- PASS` |
| 5 | 同上 `./internal/pkg/db/` | `rc=0 top=29 sub=2 fail=0 skip=0`，`ok … 16.633s` |
| 6 | 同上 `./internal/service/`（**最终内容那一趟，`08:06:30`–`08:13:39`**） | `rc=0 top=3564 sub=782 fail=0 skip=3`，`ok … 426.378s`；本批 5 条腿在这份日志里逐行读得到：S6 `:9119`、S7 `:9144`（子档 `:9145-9162`，18 档）、S8 `:9171`（`:9172-9176`，5 档）、S9 `:9180`（无子档）、S10 `:9190`（`:9191-9198`，8 档）—— 行号取自这一趟，前一趟的 `9123/9148/…` 已作废 |

`skip=3` 逐条点名并**纠正一处"行号稳定"的错觉**：`TestAIAgent_AssetBundleBinding`（`:148`）、`TestAIAgent_FullChain`（`:151`）与 §23.7 同名同行号，`TestPlatformAccountService_Login` 同一条但行号从 §23.7 的 `:9858` 漂到 `:9898` —— 本批 5 条腿在它之前多产出 ~40 行输出。⇒ 跨轮比 SKIP 只认同名，**行号会随前置腿漂移，拿行号当身份迟早把"漂移"读成"新红"**。三条都不是 obs 文件。**但 S8 的"不可写目录"那一档自带一条 `os.Geteuid() == 0 ⇒ t.Skip`**（root 无视 `0500`），本机 `skip=0` 说明这一档是真跑过的 —— 换到以 root 跑的 CI 上它会静默变成少一档，而控制组只看 `rc` 的人不会发现。

**划掉两条之后，§23.8 剩下的四条（3、5、6、7）本批一条都没动**：钩子仍未走过一次真实启动、S5 第二段仍只断文案、`model/obs_config.go` 的列默认值仍无腿、真实库里那道唯一索引仍不存在（本批没启动过携带改动的进程，所以 §23.7 那条 `0` 到今天还是 `0`，没变成本批的证据）。加上新登记的 N-34，obs 这条线上"知道但没修"的清单比批M 收口时更长而不是更短 —— 这是本批应有的形状：把能纯靠加腿关掉的洞关掉，其余的换成更精确的一句话。

### 23.11 批M-4：真实启动路径 + 库侧列默认值 + 装配点（§23.8 第 3、6 条划掉，产码零改动，电池 48→59 格）

**1 · 来路与边界。** 这一批的腿不是新想出来的，是把 §23.4 里那条"等价、无腿"的登记（M21）当成抓手：当年说"要杀它得先有 logger 捕获夹具"，于是先做夹具、再让 M21 真进电池 —— 它当场就红了（`第二次启动报了建索引失败`），说明那句登记在**当时**没错、在**今天**就不该继续挂着（见 §23.4 末的撤销块）。按同一条判据把 §23.8 第 3、6 条各自要的东西列出来：第 3 条缺"跑通一次真实启动"（`AutoMigrate()` 本体 + 钩子 + 装配），第 6 条缺"列默认值到底承哪一层重"的测量。边界照批M-3：第 4 条是强度声明、第 5 条要改的是断言口径不是覆盖面、第 7 条要启动携带改动的进程 —— 三条都不在本批，理由写在第 9 段。**产码 0 改动，且这句可核**：下表 5 个既有文件与 §23.10 表逐字节相同（`cmd/api/main.go` 本批只读不改），所以本批任何一条红的责任都只在测试面。

| 文件 | 行数 | md5 | 本批身份 |
|---|---|---|---|
| `internal/pkg/db/obs_config_startup_batchm4_test.go` | 461 | `99b81ce46da49e72fc204b112b394a71` | **新增**（S11–S15 + logger 捕获夹具） |
| `internal/pkg/db/obs_config_default_index_batchm_test.go` | 217 | `a13782bd88cba9cab01409b963292ac0` | 与 §23.10 相同（I1–I5 直调面） |
| `internal/pkg/db/migrate.go` | 593 | `2e30cba80eb6c24c178d2a36c15a3ca6` | 相同 |
| `internal/pkg/db/db.go` | 85 | `45750ea50db6df8410f4e6e65f754420` | 相同 |
| `internal/model/obs_config.go` | 68 | `e8a7b17d881d2aa912731b1baa62a83e` | 相同 |
| `cmd/api/main.go` | 509 | `e4da631347059435ce1c81b6ed4c273f` | 相同（S15 只读它） |

**2 · 五条腿，编号接续 §23.10 的 S 序列。** "变异格"一列是这批准入的最低标准：一条腿没有对应的格，就等于没被证明会红。

| 腿 | 钉住什么 | 顶层 / 子档 | 变异格 |
|---|---|---|---|
| S11 `TestBatchM_StartupFreshDatabaseBuildsObsGuard` | 腿内自建全新库 + 清掉 `obs_config` ⇒ 跑 `AutoMigrate()` **本体**：表要建出来、`idx_obs_config_single_default` 要在、且形状是**唯一 + 带 `WHERE is_default` 谓词**（`pg_indexes`/`pg_index` 读回来，不是数日志），最后真的插两条默认验证它挡得住（`:174/:178/:181/:184/:187/:200/:203`） | 1 / 0 | M54（钩子没挂进迁移路径，同 M20，换"新库首启"这条腿）、M55（索引丢掉 `WHERE` 谓词，同 M19，换索引形状断言这条腿） |
| S12 `…_StartupSecondRunIsSilentAndGuardSurvives` | 第二次启动（索引已在）：`CREATE … IF NOT EXISTS` 不许报错、要报"已就绪"、**不许**报清重 Warn（夹具在此：`os.Pipe` 换 `os.Stdout` + `logger.InitLogger` 指过去），且守卫要活过重跑（`:219/:226/:229/:232/:236/:241/:250`） | 1 / 0 | M21（去 `IF NOT EXISTS`，本批唯一进电池的老登记；M22 虽然也红在这族语句上，但它的杀手腿是 §23.4 的 I4，不记在本批账上） |
| S13 `…_StartupDedupesExistingDefaultsOnRealPath` | 存量双默认的库（先手工铺两条、且断言"索引必须不在"作前置）走**真启动路径**清重：Warn 里要点名"2 条清成 1 条"、留下的是 `GetDefault` 会选中的最早一条、降级是改列不是删行、清完索引就位（`:264/:272/:277/:280/:284/:291/:294`） | 1 / 0 | M52、M53（与 M16/M18 同一刀变异，换"真启动清重"这条腿） |
| S14 `…_ModelColumnDefaultsApplyAtDatabaseLevel` | `model/obs_config.go` 的 `default:` 标签承哪一层重，分三段（裸 SQL 省略列 / GORM `Create` 显式给零值 / 把库侧默认 `ALTER` 成 `true` 之后再 `Create`）（`:340/:343/:347/:351/:362/:392`） | 1 / 0 | M50（`status` 默认改 `inactive`）、M51（`is_default` 标签改 `true`） |
| S15 `…_MigrateIsWiredIntoAPILifecycle` | 装配点静态锁：`cmd/api/main.go` 顶层独占一行的 `db.InitDB()` / `db.AutoMigrate()` 各恰好一次、`AutoMigrate` 在 `InitDB` 之后、在 `InitDefaultStorageIfEmpty` 之前（`:445/:448/:451/:455/:458`） | 1 / 0 | M56 M57 M58 M59 |

**3 · S15 第一次交卷是两格存活，而且两格都是我自己写的判据的缺陷** —— 这一段是本批含金量最高的部分，因为它的红不在产码里。r5（17 格第一趟）`KILLED` 15、`未杀格 = ['M57','M59']`，且两格都是 `rc=0`（变异后整包全绿）：

- **M57「`db.AutoMigrate()` 被调两遍」存活**：判据当时写成 `strings.Count(s, "\n\tdb.AutoMigrate()\n") != 1`。`strings.Count`（与 Python `str.count` 同）是**不重叠**计数，紧邻两句 `db.AutoMigrate()` **中间那个换行会被前一次命中吃掉** ⇒ 两遍数出来还是 1。机理不是推演，是当场复算：`'\n\tdb.AutoMigrate()\n\tdb.AutoMigrate()\n'.count('\n\tdb.AutoMigrate()\n') == 1`，而同一段按"整行相等"数是 2。修法是把判据换成逐行比 `l == "\tdb.AutoMigrate()"` 的行计数（不是把期望改成 2 —— 那等于承认锁没牙）。
- **M59「`AutoMigrate` 挪到 seed 之后」存活**：这一格是**注码本身没打中判据** —— 第二刀的插入点写在 seed 那一行**之前**，于是迁移照旧跑在 seed 之前，S15 的顺序断言永远进不去。与 §23.10 第 21 条同源（腿的前置由夹具哪一处供给），只是这次"夹具"是变异文本。修法是把变异写在 seed **之后**，判据一行不动。
- 两处改完之后**逐刀复跑这四格**（`/tmp/battery-m4-shadow-r5b`，四格全 `KILLED`），并读红因确认各自落在自己那一句上、而不是都挤在"0 次"那一行：M56 `:445 出现 0 次`、M57 `:445 出现 2 次`、M58 `:451 db.AutoMigrate()（:124）排在 db.InitDB()（:125）之前`、M59 `:458 （:144）排在 InitDefaultStorageIfEmpty（:143）之后`。**推广两条口径**：① 用带分隔符的字面量数"某句出现几次"，对紧邻重复天然失明 —— 要么数行，要么显式做重叠扫描；② 静态锁的"顺序"判据必须有一条**能真的把顺序打反**的格，否则它只是把两句字面量抄了一遍。

**4 · S15 的残余边界（改写判据之后重算过一遍，不是沿用旧文案）。** 现在比的是"整行 = 一个制表符 + 语句"，即 `func main` 顶层、独占一行。它**挡得住**：整句删掉（0 行）、调两遍（2 行）、`InitDB`/`AutoMigrate` 换序、把它挪进多行块（顶层那行没了 ⇒ 判 0 行，也就是一条合法的条件包装也会以"0 次"红 —— 那是在要求**同步改这条腿**，不许静默放宽成"缩进几格都算"）。它**挡不住**的只剩一种：`if false { db.AutoMigrate() }` 这种**单行包装**（整行内容不是那句裸语句，于是既不算次数也读不到顺序），要拦它得 `go/ast` 级校验，本批不为此引依赖。**同一句话的另半边**：这条腿是纯静态锁，`go test ./internal/pkg/db/` 根本不编译 `cmd/api/main.go` ⇒ M56–M59 四格只能经由它显形，它一旦被人"顺手放宽"，这四格会集体变哑，而电池照样报 17/17。

**5 · 库侧列默认值到底承哪一层重（§23.8 第 6 条那句"没有承重"只对一半）。** S14 三段 + 两格把这件事量成了四条可复跑的观测：

| 写法 | `is_default` 落值 | 结论 |
|---|---|---|
| 裸 SQL `INSERT` 省略该列（以及省略 `status`/`max_size`/…） | 由**列上的 DEFAULT** 决定（`false`／`active`／`104857600`…） | `default:` 标签对**旁路写入承重** —— 标签就是那张 DDL 的唯一来源 |
| GORM `Create`，调用方显式给 `false` | `false` | 调用方说了算 |
| 同上，但先 `ALTER TABLE … ALTER COLUMN is_default SET DEFAULT true` | **仍 `false`** | GORM 把这一列**显式写进** `INSERT`，库侧默认够不到这条路 ⇒ "改库侧默认能救回零值"这个假设是错的 |
| 格 M51：把标签本身改成 `default:true` | `true`（调用方给的 `false` 被吞） | 标签值 ≠ 零值时，GORM 拿**标签值替下调用方的零值** |

⇒ 分层结论：**标签在"旁路面"承重（DDL 默认值）、在"GORM 面"不承重，但一旦它的值不等于 Go 零值，它会反过来覆盖调用方**。这条区分决定两件事：能不能删标签（不能，旁路写入会立刻失去 `status=active` 这类兜底）、以及能不能"给 `is_default` 写个 `default:true` 省事"（不能，它会把所有没显式赋值的 `Create` 变成默认行）。顺带一条读日志的规矩：S14 第三段的红有**两个**因（GORM 没写这列 vs 标签覆盖零值），那一格的日志要配着 `:362` 一起读才分得开 —— 这也是本批把消息写成两因并列、且**按消息文本而不是行号**交叉引用的原因（行号会随文件增长整体漂移）。

**6 · 两类"红得没有信息量"：编译红与计时红（电池判据补第三、第四类）。** §23.8 之外还留了一笔工具债，因为它连着废掉两趟：**r1 的 M22** 判 `SURVIVED-or-BROKEN`，日志里只有 `[build failed]` + `could not import go/token (open …/go-build/12/1287…-d: no such file or directory)` —— 那条日志里 `No space left` **0 命中**（真满盘的原文长 §23.10 表 3 那次那样），缺的是构建缓存的一个条目 ⇒ 并行泳道把 Go 构建缓存裁掉了一次；既不是 ENOSPC、也不是变异存活（同一趟 `--- FAIL` 也是 0 行；日志目录 `/tmp/battery-m4-shadow-r1-BUILDBROKEN-M22/` 就地保留，名字里的 `BUILDBROKEN` 是复核之后改的 —— 它原来写着 `ENVBROKEN`，而那一格既没有 SASL 失败也没有连不上库，两类红的处置完全不同）。**r4 的 M21** 同样判不出来，红因是 `panic: test timed out after 10m0s`：当时另一条泳道在跑 `./internal/service/` 全量，1 分钟负载 264，S11 单腿从 4.34s 涨到 **499.29s**，整包 602.5s 被掐死。两条处置：① 判据加 `BUILD-BROKEN`、`TIME-BROKEN` 两类，均**计入未杀格**（§23.8 之外的同类扩展）；② 电池的 `-timeout` 从 600s 抬到 2400s（与真门禁同口径），每格输出加上耗时与该格结束时刻的 `load1`（顺带逮到取证脚本自己的一个 bug：`vm.loadavg` 的原文是 `{ 15.31 … }`，花括号是**第一个 token**，`split()[0].lstrip("{")` 稳定返回空串 ⇒ 取 `split()[1]`）。**不对称的那一半也要写清**：超时告警**不会**替在跑的腿印 `--- FAIL`（M21 日志里在跑的 S12 只出现在 `running tests:` 清单里），所以"指定腿已经红了 + 整趟超时"是**已杀但被截断**，判据保留杀、另打一行"其余腿状态未知"，别把已经拿到的杀又吞回去。反向自测（`BATTERY_TIMEOUT=15s` 真跑两格）：M20 → `KILLED`+截断标记、M56 → `TIME-BROKEN`，汇总行两栏都列出来。**这一类判据的通用形状**：任何"红即证明"的判据，每加一类整体失效（环境／编译／计时），就要同时给它一个"计入未杀"的出口，否则失效本身会伪装成满分。

**7 · 本批给 `internal/pkg/db` 的代价，写在账上。** 五条腿里三条跑 `AutoMigrate()` **本体**（S11 一遍、S12 两遍、S13 一遍），即 4 次全量迁移（一趟就把测试库里建到 **299 张表**：收口时在 testutil 的 slot 库上直接数 `information_schema.tables` 的 `BASE TABLE` = 299）。活树无过滤全量门实测：本批之前 §23.10 表 5 记 `top=29 / ok 16.633s`，本批之后 `top=34 / ok 50.604s`，其中**这五条腿自己占 23.31s**（逐条 `7.35 / 11.76 / 4.14 / 0.06 / 0.00`），其余差额是同机负载差不是本批的账 —— 两个数一起记，是为了让"要不要在 CI 上保留这四次全量迁移"这个问题有据可问，而不是让人去猜包变慢是谁干的。

**8 · 门禁（最终内容 `99b81ce4…` 那一版，全部在最后一次编辑之后跑）**：

| # | 命令 | 实测 |
|---|---|---|
| 1 | 影子树逐文件 md5 预检（6 个文件，含本批新打的 `cmd/api/main.go`） | `PREFLIGHT-CLEAN 6 个文件 live/shadow 逐字节相同` |
| 2 | `gofmt -l <6 文件>` / `go build ./...` / `go vet <3 包>` / `go vet ./...` | 四项 `rc=0`、日志均 0 字节（`14:26–14:27`） |
| 3 | `--check db`（锚点 + 指定杀手腿存在性） | `rc=0`，`ANCHOR-OK` 18 行（M59 两条锚点各一次）、`ANCHOR-BAD/KILLER-BAD` 0 |
| 4 | 控制组（`-run TestBatchM_` 三包） | repository `rc=0 pass=12 fail=0 skip=0`；service `pass=10`；db `pass=10`（= 批M 的 5 条直调腿 + 本批 5 条） |
| 5 | **db 电池 17 格**（r6，`14:29:19`–`14:39:00`） | 17 格全 `KILLED`；末行 `未杀格 = 无, 被计时截断的格 = 无, 收尾残留 = 无`，`rc=0`；逐刀耗时 18–104s、`load1` 10.6–101.2 一并记在每行 |
| 6 | `--subs db`（真跑核对子档名） | `rc=0 现存子档 2 个，指定子档 全部命中` |
| 7 | 电池后 md5 复核 | `POSTCLEAN` 6/6（一格残迹都不留；r4 那次中途 `kill` 的残迹当场从备份摆回并核过） |
| 8 | 活树无过滤全量 `go test -p 1 -count=1 -timeout 2400s -test.v ./internal/pkg/db/` | `rc=0 top=34 sub=2 fail=0 skip=0`，`ok 50.604s`；五条新腿在日志 `:74/:80/:82/:84/:86` 逐条 `--- PASS` |

**9 · §23.8 的第 7 条本批故意不关，并新登记第 8 条。** 第 7 条（真实库里那道索引还不存在）今天再测一次仍然成立：`user_db` 里 `idx_obs_config_single_default` 计数 `0`、`obs_config` 行数 `1`、默认行数 `1`。不关它的理由不是"来不及"，是**代价不对**：补这条证据要真启动一次携带本批改动的 API 进程，而那会把别的泳道**未提交**的迁移一起推进共享开发库（§23.7 的活库取证因此只能读、不能造）。新登记的第 8 条是批M-4 自己照出来的邻居：`AutoMigrate()` 末尾那三条 post-migrate 守卫里，`postMigrateMessageHubUniqueIndex` 今天**零测试腿** —— 全仓 `postMigrateMessageHubUniqueIndex` 只有两处命中、都在 `migrate.go`（定义 + 调用），另一条 `postMigrateOpportunityClueUniqueIndex` 有 `opportunity_migration_test.go` 的四处直调。S11 跑真启动时确实把那句打了出来（`post-migrate: message_hub (platform, msg_id, conversation_id) 三元组唯一索引已就绪`，逐字可在 r4/r6 的格日志里 grep 到），但**没有任何断言读它**。本批不顺手补：那是 message_hub 那条线的判据（三元组形状、存量重复怎么清），归它自己的批次，混进来只会让 obs 这条线的收口又变回"一揽子"。⇒ 一句更精确的话：**批M-4 证明了"启动路径会建 obs 那道守卫"，没有证明"启动路径会建另外两道守卫"。**


---

### 23.12 批M-5：message_hub 那道守卫补腿，顺带量出"这条钩子三句 DDL 里只有一句承重"（§23.8 第 8 条的 message_hub 半场划掉，产码零改动，电池 59→66 格）

**1 · 来路与边界。** 腿是 §23.11 第 9 段自己登记的那条邻居：`AutoMigrate()` 末尾三条 post-migrate 守卫里，message_hub 那一条当时"全仓两处命中、都在 `migrate.go`（定义 + 调用）⇒ 零测试面"。本批把它接到真实启动路径上并配上变异格，**产码零改动**（`internal/pkg/db/migrate.go` 自批M-2 起没再动，最终 md5 `2e30cba8…`；`internal/model/ai_sales_champion.go` 完全未改，`git status` 该路径干净），只新增一个用例文件 `internal/pkg/db/message_hub_unique_batchm5_test.go`（326 行，md5 `023dcb6b…`）。三条守卫的另外两条：obs 那条已由批M-4 收口（S11–S14），opportunities 那条**仍然只有包内直调**，本批不动，登记为批M-6（见第 9 段）。

**2 · 两条腿，编号接续 §23.11 的 S 序列。**

| 腿 | 顶层用例名 | 判什么 | 变异格 |
| --- | --- | --- | --- |
| S16 | `TestBatchM_StartupClearsLegacyMessageHubNarrowUniqueness` | 带旧形状的库存量部署跑一次**真启动**（`db.AutoMigrate()` 本体）之后：① 两道窄唯一性都不在场；② 用入库口的 INSERT 量一遍"放行的是什么"——同 `msg_id` 跨渠道放行、跨会话放行、三元组全同必须挡且必须是被那道三元组索引挡；③ 物理删一行之后同三元组可重投 | M60、M62（等价格 M61） |
| S17 | `TestBatchM_StartupMessageHubIndexShapeAndSecondRunSilent` | 新库首启：① 索引形状（`pg_index` 的列清单按 `indkey` 下标折、`indisunique = t`、`indpred IS NULL`）；② 那句"已就绪"日志随启动打出来、两个失败分支一次都不出现；③ 第二次启动同样静默（幂等）且旧约束不复活 | M64、M65、M66（等价格 M63） |

腿注里写明"本批产码零改动"的一句别读成"这批改不动产码"：`postMigrateMessageHubUniqueIndex` 的三句 DDL 有一句是**空转**（第 3 段），那是可以删的死码，但删它会让格 M63 失去存在意义、且不解决任何缺陷，故本批只登记、不动。

**3 · 本批最大的一处自我更正：钩子里三句 DDL 的承重面不对称，而批M-5 最初的说法是错的。** 用例注释的第一版写着"两道 DROP 都是 GORM 一辈子不会做的事（它只加不删）⇒ 各配一刀，M61/M62 各杀一角"。r1 趟当场否掉了它的一半：**M61 存活**（`rc=0`、166s、变异后全绿）。红因读不出来就读库源码，量到的事实是：

- **句①（`CREATE UNIQUE INDEX IF NOT EXISTS …`）不承重。** 三元组索引由模型标签（`ai_sales_champion.go:14/:15/:26` 的 `priority:1/2/3`）在逐模型 `AutoMigrate` 阶段就建好了，钩子在同一趟迁移的**末尾**才跑 ⇒ 撞同名 + `IF NOT EXISTS` = 整句空转。改它的列清单没有任何可观察差异（格 M63 实测：把三列换成同样合法的 `(platform, msg_id, account_id)`，S17 全绿 ⇒ 等价格），形状的事实源在标签那一侧（格 M64 摘掉 `priority:3` 立刻红，S17 读到 `列 = "platform,msg_id"`）。
- **句②（`ALTER TABLE … DROP CONSTRAINT IF EXISTS uni_message_hub_msg_id`）与 GORM 自己的动作重复。** 这句话正是被 M61 逼出来的：`gorm@v1.30.0/migrator/migrator.go:580-598`（`MigrateColumnUnique`）在读到"库侧该列 unique、模型侧 `field.Unique` 为假"时，按 `NamingStrategy.UniqueName(table, column)` 拼名并 `DropConstraint`；`driver/postgres@v1.6.0/migrator.go:541-579` 的 `ColumnType.Unique()` 只在 `information_schema.table_constraints` 里查到**单列 UNIQUE 约束**时为真。GORM 的默认 `NamingStrategy` 对单列 `unique` 标签生成的正是 `uni_message_hub_msg_id` —— 与钩子里那个字面量**逐字同形**。⇒ 摘掉钩子那句，约束仍然会在逐模型对账时被删掉，M61 的"存活"不是判据没牙，是这一格打的本来就是重复品。
- **句③（`DROP INDEX IF EXISTS uni_message_hub_msg_id_conv`）是唯一真正非它不可的一句。** 它删的是**裸索引**：既不在上面那条约束查询的结果里、名字也不是 GORM 会给的名字 ⇒ GORM 一辈子不碰。格 M62 摘掉它，S16 红在两句（`:223` 旧二元索引还在 + `:232` 跨渠道第二条撞 `uni_message_hub_msg_id_conv`）；格 M60 摘掉**整个钩子**，红的还是这同一句 —— 差别只多了 S17 读不到"已就绪"日志。

出处可查，不是拼接的叙事：`uni_message_hub_msg_id` 这个名字来自 `e2829727`（2026-07-21 仓库初始拆分）里 `MsgID` 的 `gorm:"…;unique"` 标签；`uni_message_hub_msg_id_conv` 来自 `3c2cf104`（2026-08-07，提交标题就是"修复不稳定 conversation_id 导致 287 条 pending 永久堆积"）给 `MsgID`/`ConversationID` 打的二元 `uniqueIndex` 标签；`bcbf16f5`（2026-08-25）把标签换成三元、同时在钩子里补上句③ 并**当场留下了句① 的空转**（它把 CREATE 的名字跟着标签改了，于是从此与标签建的那道索引同名）。⇒ 不对称不是设计，是三次改键叠出来的层积；这也是本批为什么把结论写成"第 3 段"而不是一句"钩子已覆盖"。

**4 · N-35（顺带照出来，登记不修）：这条守卫在软删世界里会反过来锁死重投。** `message_hub` 模型带 `DeletedAt`（`ai_sales_champion.go:39`）、`internal/migration/migrations/v3_22_1_soft_delete_migration.go:35` 又把这张表列进 `coreTables`，而钩子建的是**全列唯一、不带 `WHERE deleted_at IS NULL`** 的索引 ⇒ 谁把某个删除口从 `Unscoped()` 改回普通 `Delete()`（一行改动），那条行的 `msg_id` 就永久占着三元组键，同一条消息再也进不来。四档取证（一次性探针 `zz_batchm5_probe_test.go`，日志 `/tmp/bm5_first.log`，取证后从两棵树删场、`find` 复核无残留）：软删后可见行 `0`/总行 `1` → 同三元组重投失败于 `uni_message_hub_platform_msg_conv (SQLSTATE 23505)` → 物理清理 `rows=1` → 重投成功。**今天不可达**：全仓对 `message_hub` 的两个生产删除口都是物理删（`repository/message_hub_inbox.go:133` 显式 `Unscoped()`；`repository/csplus_ops_repo.go:78` 是 `Table("message_hub").Delete(nil)`，本批实测走 DELETE 而非软删标记）。⇒ 记 §5 **N-35**、P3、只登记不修。本批做的是把接缝留在腿上：S16 第三段钉"物理删之后同三元组必须能重投"（这一句在两种世界里都成立，是契约不是遮羞布），S17 的 `indpred IS NULL` 断言**故意写成绊线** —— 哪天按 N-35 改成 partial 索引，它会先红，注码里已经写明红了该怎么改（改成"必须是 partial 且谓词是 `deleted_at IS NULL`"并连带重读本段）。

**5 · 于是电池多了一类判据：等价格（EQUIV），和一类新的"红得没有信息量"（TOOL-BROKEN）。** 登记等价有两种做法：把那格从电池里删掉（批M 当年对 M21 就是这么干的，结果那句"等价"成了不可证伪的口头结论，直到批M-4 才翻案 —— 见 §23.4 与 §23.11 第 5 段），或者**给绿配红法**。本批取后者：`EQUIV = {M61, M63}`，每条带一句"凭什么等价"的理由；判据链在普通格之前分叉 —— 期望 `rc == 0` 记 `EQUIV-OK`，指定腿红了记 `EQUIV-DIED`（＝那句等价断言作废，那一句其实承重，必须要么升回普通格、要么去查是哪处变化让钩子重新承重：多半是 gorm/驱动升版或标签被摘），环境红／编译红／计时红仍各自归类并记"这一格判不了"。`EQUIV-DIED` 不进"未杀格"名单（它不是漏杀），但**进退出码**、进汇总行的第 2 列（`等价格误红 = …`），这样"登记过期"和"锁没牙"在报告里是两件事，而在 CI 里都是红。r3 实测：`EQUIV-OK M61 rc=0 31s`、`EQUIV-OK M63 rc=0 30s`，汇总 `未杀格 = 无, 等价格误红 = 无`。
另一类是 r2 当场教的：**取证命令自己没起来**。驱动脚本把 `BATTERY_TIMEOUT` 写成 `2400`（少了 `s`），`go test` 在解析旗标阶段就退 2 ⇒ 24 格 3 秒"跑完"、汇总列了 22 个"未杀格"、`--subs` 报"现存子档 0 个"。那一趟既不是环境连不上（库是通的，`pg_isready` 当场过了）、也不是编译红（没有任何 Go 源码错），旧的四类判据把它兜成了"变异存活"—— 如果我只看汇总行，接下来就会去"修"22 把根本没坏锁。补两件事：① 电池新增 `TOOL-BROKEN`（认 `usage: go test` / `invalid value "` / `flag needs an argument`），命中即单独打印并计入未杀格，**不参与** KILLED/EQUIV 判定；② 驱动把 `control` 前置成门：`CONTROL db … pass=0` 就 `exit 4` 中止整趟，因为那时后面每一格都没有意义。⇒ 与 [[feedback-mutation-battery-hygiene]] 记的那几条同源，但这一类以前不在集合里：**判据的红集合要有"我这句话根本没跑成"这一档**。
**6 · 装配点那一行在跑电池期间被并行泳道改了（S15 / 格 M56–M59 的账怎么记）。** M56–M59 打在 `cmd/api/main.go`，是批M-4 留下的四条装配点格；r1 趟跑它们时影子树与活树那一版**逐字节相同**（md5 `e4da6313…`）。r1 收尾复核发现活树已被改：并行泳道 15:34:11 给 main.go 加了 `PLATFORM_ENABLED` 门控（`:199-215`；`db.InitDB()` / `db.AutoMigrate()` 在更早的 `:124-125`，本就不在这个块里），md5 变 `7491f803…` ⇒ "M56–M59 杀过"这句话的证据落在了一个不再是今天的文件形状上。核了两件事才继续：① 那次改动没有碰 `db.InitDB()` / `db.AutoMigrate()` 那两行的**行形状**（S15 的判据是整行行计数，见 §23.11 第 4 段的残余边界），也没碰 `service.InitDefaultStorageIfEmpty`；② 活树全量门禁当场复跑通过（`top=36 fail=0`）⇒ S15 在两版上都成立。收口做法：影子树整体换成与活树同步后的内容（`bm5_rd2.sh` 的第 0 步 PREFLIGHT 对 9 个电池目标文件逐个比 md5，任一 DRIFT 就 `exit 3` 中止整趟），r3 在最终内容上把 24 格**全部重跑**，M56–M59 四格的"杀"因此是 `e4f7466c…` 这一版给的（r3 全红，见第 8 段表第 6 行）。⇒ 记一条口径：**电池目标文件只要被别的泳道动过，那一族的证据就作废到重跑为止**，"看起来不相关"不是理由。

**7 · 本批给 `internal/pkg/db` 的代价，写在账上。** 两条腿都是"跑真启动"量出来的：S16 三段（含两次 `AutoMigrate()` 本体 + 一次铺旧形状 + 三次入库口试插），S17 两次启动并捕获日志。包内无过滤全量从批M-4 收口的 `ok 50.604s` 涨到 `77.720s`（+27.1s，`top=34 → 36`）。这一包现在的门禁预算不能低于 120s，`-timeout` 短于此的跑法只会量产计时红（§23.11 第 6 段同一件事）。**这条"现在"只到批M-5 为止**：批M-6 又加了两条真启动腿（`top 36 → 38`），预算抬到 **≥150s**，现行口径以 §23.13 第 10 段为准。影子树侧：单格耗时 30–99s，r3 整趟 `17:09:22 → 17:33:15` ≈ 24 分钟，负载 `6.5–30.4`。

**8 · 门禁与证据（全部在**含批M-5 的最终内容**上跑，且都在最后一次编辑之后）。**

| # | 步骤 | 结果 |
| --- | --- | --- |
| 1 | 预检：影子树与活树 9 个电池目标文件逐个 md5 | `PREFLIGHT same ×9`（`migrate.go 2e30cba8…`、新用例 `023dcb6b…`、`ai_sales_champion.go 094fc52d…`、`cmd/api/main.go e4f7466c…`），DRIFT = 0 ⇒ 不中止 |
| 2 | 影子树独占性（`lsof -d cwd`） | 无并行 shell 停在影子树内 |
| 3 | 落盘与数据层 | `Avail 112Gi`；`127.0.0.1:8232 - 接受连接` |
| 4 | `--check db`（跑之前验锚点） | `ANCHOR-OK` 25 行 / 24 格（M59 两处锚），`ANCHOR-BAD` 0 |
| 5 | `control`（变异前的三包全量绿，真门禁口径） | `repository rc=0 pass=12 fail=0 skip=0`、`service rc=0 pass=10 …`、`db rc=0 pass=12 …`；驱动把这一行当前置门（`pass=0` 即 `exit 4`） |
| 6 | `battery db` 全 24 格 | `未杀格 = 无, 等价格误红 = 无, 被计时截断的格 = 无, 收尾残留 = 无` ⇒ 22 格 `KILLED` + 2 格 `EQUIV-OK`（M61 31s、M63 30s），新格逐格带耗时与负载：M60 32s/7.7、M62 45s/12.0、M64 39s/12.1、M65 43s/9.1、M66 99s/30.4 |
| 7 | `--subs db` | `rc=0 现存子档 2 个，指定子档 全部命中` |
| 8 | POSTCLEAN（打过/还原过的文件回到基线） | `POSTCLEAN DRIFT` 0 行，残留变异 grep 0 命中 |
| 9 | `gofmt -l`（两个改动文件）／`go vet ./internal/pkg/db/`／`go build ./...` | 空 ／ `rc=0` ／ `rc=0` |
| 10 | 活树无过滤全量 `go test -p 1 -count=1 -timeout 2400s -test.v ./internal/pkg/db/` | `rc=0 top=36 sub=2 fail=0 skip=0`，`ok 77.720s`；S16 在 `:30 (14.43s)`、S17 在 `:32 (16.46s)` 逐条读到 `--- PASS` |
| 11 | 日志分轮存 | `/tmp/battery-m5-shadow-r1`（r1，M61 存活那一趟）、`…-r2-flagfail`（第 5 段说的旗标错那一趟，整包 3 秒）、`…-r3`（收口）；驱动日志同名三份 |

**9 · 本批未覆盖面（不得当作已验证）。**
① ~~**`postMigrateOpportunityClueUniqueIndex` 仍然没有启动路径腿**~~ **已由批M-6 补腿**（S18/S19 + 格 M70–M76，见 §23.13）。登记时写的方子是"照 S16/S17 的形状做两条（装配点静态锁 + 本体跑真启动）"，实做**只做跑本体那一侧**（两条都跑 `AutoMigrate()`），理由是跑本体一次把 presence 与 position 一起钉住、而计数式静态锁挡不住"语句还在但被包进恒假分支"（§23.13 第 3 段，偏离已记账）。登记时点出的那句判据也一起补上了，并且当场发现它比写的时候更弱：`opportunity_migration_test.go:352-371` 那句"表没被清空"读的是**不带 WHERE 的全表计数**、而同一张表那时已有 9 行 ⇒ 钩子把重复行删掉它照绿，所以"不该被自动改写"这一半在旧腿里是零判据而不是弱判据（§23.13 第 5 段）。
② **真实库里那道三元组索引今天存不存在，仍未测**（与 §23.8 第 7 条同一代价理由：补这条要真启动一次携带未提交改动的 API 进程，会把别的泳道的迁移一起推进共享开发库）。本批能证明的是"启动路径会建它/会清掉旧的"，不是"生产上已经建好了"。
③ **"存量里有三元组重复行时钩子会怎样"没有腿，而且刻意不补**：历史上在位的每一道键都比三元组更窄（`unique(msg_id)` ⊂ `unique(msg_id, conversation_id)` ⊂ 三元组），所以任何一条升级路径上，三元组全同的第二条都在更早那道键上撞死过 ⇒ 造这个夹具只能凭空插重复行，那是在测一个做不出来的部署（§23.6 那条纪律的同一型）。而"建索引失败只落一行 Warn"这一支的可观察性反过来是有腿的：S17 必须读到那句"已就绪"，格 M60/M66 的红因正好打在这两句上。
④ **两条等价格的有效期绑定库版本**（`gorm.io/gorm v1.30.0` + `gorm.io/driver/postgres v1.6.0`，读的是 `go.mod:41-42`）。升 gorm 之后必须先复跑 M61/M63 再谈别的：`EQUIV-DIED` 就是为那一趟准备的，红了不要顺手改回"期望杀"，先核 `MigrateColumnUnique` 与 `ColumnType.Unique()` 那两段还在不在做同一件事。
⑤ S16 第二段断的是**同一张表上的 INSERT**，不是仓储的入库函数（那要拉进 `internal/repository` 的夹具，跨包且会碰到并行泳道的文件）。理由与 §23.6 一致：唯一键是库级的，谁插都撞在同一道索引上 —— 但这句话的强度就到"键"为止，不含入库口拿到 23505 之后的处理分支。

### 23.13 批M-6：opportunities 那道 clue_id 守卫接上真实启动路径，顺带量出"旧那四条直调真的看不见装配点"（§23.8 第 8 条剩余半场划掉，产码零改动，电池 66→73 格）

**1 · 来路与边界。** 腿是 §23.12 第 9 段第 ① 条自己登记的那一半：三条 post-migrate 守卫只剩 `postMigrateOpportunityClueUniqueIndex` 没有启动路径腿。本批把它接上，**产码零改动**（`internal/pkg/db/migrate.go` 自批M-2 起没再动，最终 md5 仍 `2e30cba8…`；`internal/model/opportunity.go` 完全未改），只新增一个用例文件 `internal/pkg/db/opportunity_clue_startup_batchm6_test.go`（302 行，最终 md5 `823cb04c…`）。电池 db 24→31 格、总数 66→73（repository 18 / service 24 / db 31）。夹具复用批M-4 的 `batchM4StartupDB` / `batchM4RunStartup` / `batchM4CaptureStartup`，腿名前缀仍是 `TestBatchM_`（电池的 `-run TestBatchM_` 是子串匹配，换前缀等于新腿不进电池）。

**2 · 两条腿，编号接续 §23.12 的 S 序列。**

| 腿 | 顶层用例名 | 判什么 | 变异格 |
| --- | --- | --- | --- |
| S18 | `TestBatchM_StartupBuildsOpportunityClueGuard` | 全新部署（先把 `opportunities` 抹到"表还不存在"）跑两遍**真启动**：① 第一次的启动日志里必须读得到那句『已就绪』、读不到任何建索引失败；② 索引形状（`indisunique`、`indpred IS NOT NULL`、谓词里点名 `clue_id`）；③ 行为面 —— 两条 `clue_id` 为空串的商机都落得进去（并有一句 Fatal 验"两行真的都落了空串"，防 GORM 省列把这段变成测 NULL）、同一 `clue_id` 的第二条必须**撞在 `idx_opportunities_clue_id` 上**、换线索不受牵连；④ 第二次启动静默且守卫活过重跑 | M70–M74 |
| S19 | `TestBatchM_StartupDoesNotAutoRewriteLegacyDuplicateOpportunities` | 摆成"钩子那次上线之前的库"（表在、索引被临时删掉、同一 `clue_id` 两条存量行）再跑一次真启动：① 不 panic；② 日志里既有点名 `CREATE … 失败` 也有『需人工清重』、且**不许**出现『已就绪』；③ 索引仍然不在场、两行按 `code` 逐字读回都还在、第三条同线索反而插得进去（＝库级兜底确实没铺上） | M75、M76 |

**3 · 一处对登记方子的偏离：本批不补装配点静态锁。** §23.12 第 9 段开的方子是"照 S16/S17 的形状做两条（装配点静态锁 + 本体跑真启动）"。静态锁那一半**没做**，理由不是省工，是强度：这两格的红的都是"位置不对"（M70 = 那句调用没了、M71 = 那句挪到模型迁移**之前** ⇒ 全新库里 `relation "opportunities" does not exist`），而计数式锁（obs 那条在用的 I5）只读文本 —— 同一句调用被包进 `if false { … }` 这种恒假分支，它照样数到 1（§23.11 第 4 段写过的残余边界）。跑本体一次把 presence 与 position 一起钉住，两格实测都红（M70 60s、M71 96s），所以这里取跑本体那一侧。⇒ 这是**偏离**，不是补全：三条守卫现在挂进 `AutoMigrate()` 的证据强度齐了，但形态不一致（obs 有 I5 + S11/S12，另两条只有"跑本体"），下一位读者若要统一口径，应该往下拆静态锁而不是往上补，而不是反过来。

**4 · 本批最大的一处取证升级：把一句读出来的话变成量出来的。** §23.8 第 8 条与用例注释头部都写着"四处直调（`opportunity_migration_test.go:204/:289/:343/:363`）全都自己拿句柄跑函数 ⇒ 删掉 `migrate.go:422` 那句调用它们全都照绿"。电池那趟是 `-run TestBatchM_` 过滤跑的（旧腿压根没执行）⇒ 到本批之前，这句话的证据强度仍然停在**读装配点**。补一趟全包无过滤复跑（影子树，打 M70 那一刀，`go test -p 1 -count=1 -timeout 1800s -test.v ./internal/pkg/db/`）：`PASS=36 FAIL=2 SKIP=0`，红的**只有本批两条新腿**（`TestBatchM_StartupBuildsOpportunityClueGuard 3.97s`、`TestBatchM_StartupDoesNotAutoRewriteLegacyDuplicateOpportunities 8.60s`），七条旧 `TestOpportunity*`（含 `TestOpportunityClueIDPartialUniqueIndex`）**一条不红**；跑完 `migrate.go` 还原回 `2e30cba8…`（`/tmp/bm6_m70_fullpkg.log`，日志 `/tmp/bm6_m70_fullpkg_testv.log`）。⇒ 旧那四档判据（形状、撞键、空串/NULL 不受约束、可重跑）与"下一次启动会不会建它"是两件事，这句话现在有执行记录背书了。

**5 · 顺带量出：旧腿那句"表没被清空"比 §23.12 第 9 段登记时说的还要轻。** 登记时写的是"只断没 panic 和表没被清空（`:366`、`:369`）"。实际读下来是：`:364-370` 断的是 `SELECT COUNT(*) FROM opportunities`（**没有 WHERE**）之后 `remaining != 0` 才报红 —— 而同一张表在那个时刻已经被该用例前面的判据 1–4 留下了 **9 行**（`dup_a`、`ok_a`、`ok_b`、三条 `blank_*`、两条 `null_*`、`after_rerun`；`:280-285` 那两条对照行被显式 DELETE、`:314`/`:348` 两条期望失败的没落库，逐条按行号数过）。⇒ 钩子就算把那两条重复的 legacy 行 DELETE 掉，`remaining` 照样是 9 上下、旧腿**照绿**。⇒ "不自动改写"这一半在旧腿里不是弱判据，是零判据。格 M76 就是打在这一处：把失败分支改成"删掉重复行再 CREATE 一次"，S19 红在四句（`:280` 还报了『已就绪』、`:285` 索引被建出来了、`:293` 存量只剩 `OPP-BM6-L1`、`:296` 第三条同线索反而插不进），其中 `:293` 那一句是整批唯一直接钉"数据没被改"的断言。这一条不新增 N 号（生产里没有正在被改写的证据，`remaining` 那句只是判据弱），但它是 §23.6 那条纪律的又一例：**"看起来在断"的计数式断言，要先数清楚它读的是哪几行。**

**6 · 与 message_hub 那条的不对称：这里的钩子是形状的唯一事实源。** 批M-5 第 3 段量出 message_hub 钩子那句 CREATE 是空转（列清单由模型标签先建，`IF NOT EXISTS` 撞上同名 ⇒ 格 M63 只能登记成等价格）。这条反过来：`internal/model/opportunity.go:72` 的 `ClueID` 只有 `gorm:"type:varchar(36)"`，**没有任何索引标签**（`:70` 的注释本身就是这么写的："所以它不在标签上，而在 `postMigrateOpportunityClueUniqueIndex()`"）⇒ 索引名、唯一性、谓词三件事全由钩子那一句 DDL 决定，没有任何别处在建它。于是同一形状的变异在两个包里分属两类：`去掉 IF NOT EXISTS` 在 message_hub 是等价格（M66 那侧是"每次启动都报失败"才杀的），在这里是**普通格 M72**，红在 S18 第二段的两句日志断言（`:217` 二次启动报了失败、`:220` 不再报『已就绪』）。⇒ 别把批M-5"钩子多半与标签重复"的结论平移过来读；判断先看标签在不在。

**7 · M73 / M74：谓词的两种坏法红在不同句上，形状断言挡不住语义等价。** 两格打同一句 `WHERE clue_id <> ''`：M73 整句丢到全列唯一 ⇒ `pg_index` 的形状断言当场红（`:162` `indpred` 为空、`:165` 谓词里读不到 `clue_id`），行为段随后也红；M74 换成 `WHERE clue_id IS NOT NULL` ⇒ **形状三句全绿**（谓词在、键也对），只有"两条空串商机都落得进去"那一段抓得住（`:174` 第二条被 `idx_opportunities_clue_id` 挡 + `:188` 夹具 Fatal 点名"只落了 1 行"）。⇒ 通用口径：**读系统目录只能证"形状长这样"，证不了"形状管的是这件事"**；配一段行为面（空串放行、非空挡重、换值不误伤）才是承重的那一侧。这一条与 §23.12 第 3 段"标签才是事实源"是同一族的两面。

**8 · 两处自我更正（都在用例侧，产码无关）。** ① `batchM6InsertOpp` 的注释第一版写着"手工建的商机在生产里落的是空串"——读写口否掉了：今天唯一的商机写口是线索转化，而它把空 `clueID` 当场拒（`internal/service/opportunity_convert.go:161-164`），路由里也没有手工建单的 POST 口（`internal/controller/opportunity.go:65-75` 只有读口与转化/推进类写口）⇒ 空串这一档今天只由**旁路写入**落出来，而那恰恰是谓词存在的理由（库级契约不按"当前有没有人这么写"来设）。注释按实测改口，没有动判据。② r1 的 M73/M74 日志里那句夹具 Fatal 写的是"另一行是 NULL 或被省列"，而真实成因是"第二条被索引挡了、压根没落库"——**把判据命中报成了夹具不成立**，读者据此会去查 GORM 而不是查谓词。改成按上一条 INSERT 的成败分流（`blankErr2`，`:172-193`），r2 复跑读到的就是"少的那一行是被唯一索引挡掉的（谓词坏了），不是夹具省列"。⇒ 与 [[feedback-verify-secondhand-review-claims]]、[[feedback-mutation-battery-hygiene]] 同源：**红因的文案也是判据的一部分**，报错原因是会带人走错方向的。

**9 · N-35 不迁移到这一条（照完三处才敢写这句）。** §23.12 第 4 段那道"软删世界里全列唯一会锁死重投"的缺陷，在这里三个前提逐个核过：`internal/model/opportunity.go` 无 `DeletedAt`（全文件 grep 零命中）⇒ 不是软删表；`internal/migration/migrations/v3_22_1_soft_delete_migration.go` 里 `opportunit` 零命中 ⇒ 也没被列进那套软删改造；而钩子建的本来就是 **partial**（`WHERE clue_id <> ''`）⇒ 与"键被已删行永久占住"不是一件事。⇒ N-35 只留在 `message_hub`，不复制、不登记新号。

**10 · 代价，写在账上（含一份丢掉的日志，按丢的记）。** 包内无过滤全量的墙钟读到四档，**三档在最终内容 `823cb04c…` 上、一档在它的纯注释前版本 `31271222…` 上（两版只差 7 行注释，用例逻辑同一）**：`ok 121.376s`（`31271222…`，活树，load 9.85/13.45/18.20，当时另有并行泳道在跑 `./internal/service/` 全包）、`ok 76.865s`（`823cb04c…`，r2 的影子树基线，负载 8.9–36 区间里跑的）、`ok 55.157s`（`823cb04c…`，活树补测趟，起跑 load 9.27、同期另一泳道在跑 `./internal/app ./internal/router`）、`ok 45.904s`（`823cb04c…`，活树收口趟，load 5.81，泳道已经跑完）。**但 `121.376s` 那一趟的原始日志已不在盘上**：产出它的 `/tmp/bm6_live_gate.sh` 第一句就是 `: > "$OUT"`，同一驱动为最终内容再跑一趟时把上一趟的日志就地截掉了（本节第 11 段行 12 写的"S18 在 `:49 (27.10s)`、S19 在 `:57 (18.09s)`"是当时从那份日志抄下来的，抄录留着、原件没了）。⇒ 补了一趟带轮次后缀的 `/tmp/bm6_live_gate_r3.sh`（`OUT=/tmp/bm6_live_gate_r3.log`，不再截自己）留证，**结论不依赖那份丢掉的原件**：剩下三档都在 46–77s，摆动 1.7 倍且**负载主导**。⇒ "负载主导、不是新腿变慢"这句也改成跑出来的：把两趟活树日志的逐用例耗时对齐比过（`python3` 读 `--- PASS:` 行，38 条对 38 条），总和 `45.2s → 54.6s`（+9.4s，与包级 `45.904s → 55.157s` 对得上），其中 **`TestAutoMigrate` 一条就 +7.72s（6.22 → 13.94）**，两条新腿只动 +1.51s / +1.04s（S18 5.31 → 6.82、S19 5.66 → 6.70）⇒ 增量落在全库建表那条老腿上，不在本批新腿上。顶层用例 `36 → 38`。本包的 `-timeout` 预算按负载侧取，不低于 **150s**（§23.11 第 6 段、§23.12 第 7 段同一件事的第三次记账），CI core 片每包 `-timeout 900s`（`.github/workflows/user-server-ci.yml:277`）⇒ 仓库侧不动。影子树侧：新 7 格单格耗时 49–154s，r1 整趟（31 格）`18:18:50 → 19:22:52` ≈ 64 分钟，r2 复跑 7 格 `19:41:23 → 19:48:33` ≈ 7 分钟。**规律（进本项目记忆）**：被文档引用的测量，产出它的驱动不许覆写自己的日志——`log 分轮存` 这条老规矩在批M-6 收口时又踩了一次。

**11 · 门禁与证据（最终内容 = 用例 `823cb04c…`，且都在最后一次编辑之后）。**

| # | 步骤 | 结果 |
| --- | --- | --- |
| 1 | 预检：影子树与活树 8 个电池目标文件逐个 md5（r1 用 7 文件版、r2 用 8 文件版） | `PREFLIGHT same ×8`（`migrate.go 2e30cba8…`、新用例最终版 `823cb04c…`、`ai_sales_champion.go 094fc52d…`、`obs_config.go(模型) e8a7b17d…`、`cmd/api/main.go e4f7466c…`），DRIFT = 0 ⇒ 不中止 |
| 2 | 影子树独占性 | `pgrep -f /tmp/shadow-m3` 无命中 ⇒ 那条 `lsof -d cwd` 检查**没生效**（驱动日志里明写了这一点，不记成"查过且干净"）；真独占由 `/tmp/battery_flock.py` 的 `LOCK_EX` 保证 |
| 3 | 落盘与数据层 | `Avail 101Gi`；`127.0.0.1:8232 - 接受连接`；`load 9.85 13.45 18.20` |
| 4 | `--check db`（跑之前验锚点） | `ANCHOR-OK` 33 行 / 31 格（M71 两处锚、M75/M76 同锚不同坏法），`ANCHOR-BAD` 0 |
| 5 | `control`（真门禁口径） | r1 整包：`CONTROL db rc=0 pass=14 fail=0 skip=0`；r2 复跑前：`rc=0 pass=38 fail=0 skip=0`（无过滤全包，`ok 76.865s`）—— 两趟驱动都把 `pass=0` 当 `exit 4`、把任何 fail/skip 当 `exit 6` |
| 6 | `battery db` 全 31 格（r1） | `未杀格 = 无, 等价格误红 = 无, 被计时截断的格 = 无, 收尾残留 = 无` ⇒ 29 格 `KILLED` + 2 格 `EQUIV-OK`；新 7 格带耗时与负载：M70 60s/9.75、M71 96s/20.05、M72 99s/15.60、M73 154s/40.04、M74 72s/27.64、M75 71s/21.52、M76 71s/11.41 |
| 7 | `--subs db` | `rc=0 现存子档 2 个，指定子档 全部命中` |
| 8 | r1 POSTCLEAN | `DRIFT` 1 行：`opportunity_clue_startup_batchm6_test.go 31271222…(live) vs 0714e32e…(影子)` ⇒ 跑电池途中打过那次纯注释改口（第 8 段①）。`diff` 机械证到：变化 10 行、每一行都以 `//` 开头；电池 7 格的变异锚点全在 `migrate.go` ⇒ 那一趟的"杀"不因它作废。处置：改口后的内容同步进影子树（`same` 复核）+ 第 9 行 r2 逐刀复跑 |
| 9 | r2（改过用例之后按「改过用例后逐刀复跑」）| 受影响 7 格 `KILLED M70 61s / M71 81s / M72 70s / M73 57s / M74 57s / M75 55s / M76 49s`，汇总 `未杀格 = 无, 等价格误红 = 无, 被计时截断的格 = 无, 收尾残留 = 无`；M73 红四句 `:162/:165/:174/:188`、M74 红两句 `:174/:188`（第 7 段那处分野就是在这儿读的）；POSTCLEAN DRIFT = 0 |
| 10 | 旧腿是否连带红（第 4 段那趟无过滤复跑） | M70 那一刀 `PASS=36 FAIL=2`：七条 `TestOpportunity*` 全 `--- PASS`，两条新腿全红；`migrate.go` 还原后 md5 == 基线 |
| 11 | `gofmt -l internal/pkg/db/`／`go vet ./internal/pkg/db/`／`go build ./...` | 空 ／ `rc=0` ／ `rc=0` |
| 12 | 活树无过滤全量 `go test -p 1 -count=1 -timeout 1800s -test.v ./internal/pkg/db/`（**三趟**：改口版 `31271222…`、最终版 `823cb04c…` 收口趟、最终版补测趟 r3） | 三趟都 `rc=0` `PASS=38 FAIL=0 SKIP=0`（顶层用例源码 grep 同为 38 ⇒ 没有执行不到的腿）。改口版那趟：`ok 121.376s`（load 9.85/13.45），S18 在 `:49 (27.10s)`、S19 在 `:57 (18.09s)` 逐条 `--- PASS` —— **⚠️ 这一趟的原件已被同一驱动的第二趟截掉（`: > $OUT`），本行数字是当时抄录，见第 10 段**；最终版收口趟：`ok 45.904s`（load 5.81），S18 5.31s、S19 5.66s；最终版 r3：`ok 55.157s`（load 9.27，同期另一泳道在跑 `./internal/app ./internal/router`），S18 6.82s、S19 6.70s ⇒ 两趟留证的差在负载不在用例（逐用例分解见第 10 段） |
| 13 | 日志分轮存 | `/tmp/battery-m6-shadow-r1`（31 格）、`/tmp/battery-m6-shadow-r2`（复跑 7 格）；驱动 `/tmp/bm6_rd1.log`、`/tmp/bm6_rd2.log`、`/tmp/bm6_m70_fullpkg.log`、`/tmp/bm6_live_gate.log`（最终版收口趟；**改口版那一趟的日志已被本文件截断覆盖**）、`/tmp/bm6_live_gate_r3.log` + `_r3_testv.log`（补测趟，轮次后缀、不覆写） |

**12 · 本批未覆盖面（不得当作已验证）。**
① **真库里那道 `idx_opportunities_clue_id` 今天还不存在**（§23.8 第 7 条同一代价、同一处置：补这条要真启动一次携带未提交改动的 API 进程，会把别的泳道的迁移一起推进共享开发库）。本批能证明"启动路径会建它 / 建不成只报不改"，不能证明"生产上已经拦得住第二条"。
② **电池的杀信号只含 `TestBatchM_` 那几条腿**：其余 6 格没有做第 10 行那种无过滤复跑 ⇒ "旧腿不会被这一刀打到"这句话只对 M70 是量出来的，对 M71–M76 是按旧腿判据读出来的（读的是它们各自断什么，不是没跑过）。
③ **S19 的"存量重复"是最小形状**（同一条 `clue_id` 两行、`code` 可读回那两条），真实旁路写入留下的形态（同线索多行 + 各自不同 stage/金额、或带 `NULL` 与空串混排）没铺；而钩子失败分支只有一条 Warn，不区分数量的性质 ⇒ 结论只到"不改写、不建索引、报出来"，不到"清重方案好不好做"。
④ **M71 只红 S18，S19 全绿**（`--- PASS … 9.36s`）：S19 不读铺形阶段的日志，而它的第二次启动发生在表已经建出来之后 ⇒ "钩子位置错"对它不可见。两格不冗余，但也别按"每格都双红"读这张表。
⑤ ~~**`AutoMigrate` 的容忍/兜底面仍然零腿**~~ **已由批M-7 结**（S20–S26 + S30 共 8 条腿、格 M83–M96，见 §23.14；登记时写的那道前置——『要做一个真让单个模型的 AutoMigrate 失败的夹具』——最后是用三个合成模型挂 `RegisterExtraModels` 同一条口子做到的，产码一行没为它改）：`isTolerableMigrateError`、`createTableFallback`、`ensureExtensions` 三个符号在全仓 `--include='*_test.go'` 里 grep 命中均为 **0**（本批实测，三条各查一次），而基线启动日志读得到「AutoMigrate 完成，无迁移漂移」⇒ 容忍分支 ≡ 没执行过。登记为批M-7（任务 #54），前置是要做一个"真让单个模型的 AutoMigrate 失败"的夹具，那是独立设计活，不在本批代价里。
⑥ 等价格 M61/M63 的版本绑定（`gorm v1.30.0` + `driver/postgres v1.6.0`）沿 §23.12 第 9 段第 ④ 条；本批没有新增等价格，但这两格在 r1 仍然 `EQUIV-OK`（50s、192s）⇒ 那句"钩子第一句与 GORM 列级对账重复"到今天还没失效。

### 23.14 批M-7：把 `AutoMigrate()` 自己那三条分支接上腿，顺带修掉一条"把名字在场当成形状在场"的守卫（N-36 已修，电池 73→93 格）

**1 · 来路与边界。** 这一批的腿是 §23.13 第 12 段第 ⑤ 条自己登记的半场：批M-4/5/6 把三条 post-migrate 守卫逐个接到了真启动路径上，但那条路径自己"遇到迁移漂移怎么办"这一步始终只有读码 —— `isTolerableMigrateError`（容忍）、`createTableFallback`（兜底）、`AutoMigrate()` 末尾的 `panic(err)`（终止）三个符号在全仓 `--include='*_test.go'` 里 grep 命中为 **0**，而基线启动日志永远读得到那句「AutoMigrate 完成，无迁移漂移」⇒ 容忍分支 ≡ 没执行过。**本批与前三批有一处不同：动了产码**（N-36，见第 3 段），所以"任何一条红的责任都只在测试面"这句本批不成立，产码改动面按文件列在下面。

| 文件 | 行数 | 最终 md5 | 本批动了什么 |
| --- | --- | --- | --- |
| `internal/pkg/db/migrate.go`（活树） | — | `81b51871146dce5bc8c6ab96dd6111e5` | 新增 `verifyUniqueIndex` 一个函数（29 行，含注码）+ 三条钩子各一处消费（4 / 4 / 2 行）⇒ **本批净增 39 行、删 0 行**。注意别拿 `git diff` 的总数读：该文件对 HEAD 的未提交增量是 85 行，另 46 行是批M-2/批M-6 的钩子与调用（其中 obs 那处消费就压在批M-2 那整块 `+49` 里，git 分不出批次） |
| `internal/pkg/db/migrate_tolerance_batchm7_test.go` | 702 | `760d12099ec49349bad10ed8c2587939` | 新增（S20–S26、S30 共 8 条腿 + 两个合成模型族 + `batchM7LinesWith` 取证 helper） |
| `internal/pkg/db/index_shape_batchm7_test.go` | 315 | `b4f72bb5ee3ad19bd483580b61337156` | 新增（S27/S28/S29 三条腿 + `batchM7InsertObs` 错误返回版夹具） |

电池：db 31→**51** 格（新增 M77–M96 二十格，编号无空洞，M67–M69 那三个号从未存在）、总数 73→**93**（repository 18 / service 24 / db 51，按 `CELLS` 逐包数出来的）。合成模型一律挂 `RegisterExtraModels` 那条真口子（与 `allModels()` 并列迁移），表名前缀 `zz_batchm7_`、腿名前缀仍是 `TestBatchM_`。

**2 · 十一条腿，编号接续 §23.13 的 S 序列。** 分工不是按分支条数排的，是按"这条分支坏起来长什么样"：三条启动腿（S20/S21/S22）各钉一种坏法、代价完全不同；三条直调腿（S23/S25/S26）判的是纯函数与分类器的**判定半径**；S24 判装配；S26/S30 见第 4、6 段。

| 腿 | 顶层用例名 | 判什么 | 变异格 |
| --- | --- | --- | --- |
| S20 | `TestBatchM_StartupHealsOrphanCompositeType` | 现场"表被删、同名复合类型还留在 `pg_class`" ⇒ 真启动一次自愈：读得到『命中幂等重跑提示』与『兜底 CreateTable 已重建缺失表』、表真在、**两条汇总行恰好出现一条**；第二次启动干净且表名不再出现在任何一行 | M84、M89 |
| S21 | `TestBatchM_StartupToleratesLegacyConstraintNameDriftWithoutRewriting` | 现场"表在、列上挂着一条手写名字的单列 UNIQUE 约束"（配方从 `gorm@v1.30.0 migrator/migrator.go:580` 的 `MigrateColumnUnique` 反推实测）⇒ 容忍但**一步兜底都不许走**：不许出现『已重建缺失表』、数据一行不动 | M83、M87 |
| S22 | `TestBatchM_StartupPanicsOnNonTolerableNotNullDrift` | 现场"表在、有存量行、模型新增一列 `not null`" ⇒ 必须当场 panic，且消息里同时读得到 `null values` 与那张表的名字 | M88 |
| S23 | `TestBatchM_MigrateErrorClassifierTruthTable` | 分类器真值表（**直调**）：nil / 空错误 / 只含 `does not exist` / 只含 `constraint` / 两支齐全 / `already exists` / 无关错误，各一档返回值 | M85、M86 |
| S24 | `TestBatchM_StartupReinstallsMissingPGExtension` | 扩展是被 `AutoMigrate()` **第一句**装的 ⇒ 卸掉 `uuid-ossp` 再跑一次真启动，它自己回来 | M91 |
| S25 | `TestBatchM_EnsureExtensionsWarnsButDoesNotPanicOnDeadHandle` | 装扩展失败那一支只 Warn 不 panic（直调，句柄是**已关掉**的独立连接），并**逐家点名**：提示行数与带名字的行数各数一遍 | M92、M93 |
| S26 | `TestBatchM_MissingTablesAndTableNameHelpers` | 终校验的两个纯函数的判定半径（直调）：`missingTables` 在 nil 句柄下"全报缺失"、在场/缺失各归各位；`tableNameOf` 解析失败返回空串 | M94、M95、M96 |
| S30 | `TestBatchM_FallbackPanicsInsteadOfClaimingRebuildWhenNameIsTakenByView` | 兜底自己建不出表时不许谎报"已重建"：见第 4 段 | M90 |
| S27 | `TestBatchM_OpportunityGuardDoesNotClaimReadyWhenNameIsNotUnique` | N-36 的三处消费，第一处：`idx_opportunities_clue_id` 名字被一枚非唯一索引占住 ⇒ 不许报『已就绪』、必须点名是哪一枚 | M77 |
| S28 | `TestBatchM_ObsGuardDoesNotClaimReadyWhenNameIsNotUnique` | 第二处，同一符号被换一处消费 ⇒ **逐处拆刀**（一刀只会红一处） | M78 |
| S29 | `TestBatchM_MessageHubGuardDoesNotClaimReadyWhenNameIsNotUnique` | 第三处（三元组索引），也是三处里最便宜撞上的一枚 | M79 |
| — | （复用批M-6 的 S18） | 核验退化成"只看有没有报错"、以及核验查询带上恒假谓词两刀，判据都落在 S18 那句『已就绪』上 | M80、M81、M82 |

**3 · N-36：这三条守卫报的"已就绪"此前只看索引名。** `CREATE UNIQUE INDEX IF NOT EXISTS` 的对账口径是**名字**：库里已有一枚同名**非唯一**索引时整句是静默 no-op（`err=<nil>`），钩子于是走到 `else` 支、对运维报出与库里事实相反的那一句。修法是那句 DDL 之后、报『已就绪』之前过一道 `verifyUniqueIndex(db, indexName)`：按名字读 `pg_index.indisunique`，**读不到形状与读到非唯一都算不对**，返回一句带索引名的归因走 `Warn`。三条理由写进注释并各自有腿：① 形状不对时**只报不修**（非唯一索引可能是人为读路径特意建的，启动期替运维决定 `DROP` 它是拿可用性换一致性）；② 索引名只按名字查、不带表条件（PG 里索引名在 schema 内唯一）；③ helper 那两支的文案其实**不同**（一支带 `读不到它的形状: %v`、一支带 `名字被一枚非唯一的历史索引占住`），但三条腿的断言只读到共同前缀 ⇒ 对断言不可分，把"读不到形状"那一支单独钉住的是批M-6 的 S18 + 格 M81（恒假谓词那一刀）。⇒ 通用口径：**同一个 helper 被多处消费时，格要按消费点拆，不能按 helper 拆**；三处消费拆出 M77/M78/M79 三格，另两格（M80 打在 helper 侧、M81 打在查询侧）补的是 helper 自己那两支。

**4 · 一格存活怎么变成一个夹具：M90 与视图占位。** r1 的 M90 判 `SURVIVED-or-BROKEN`（`rc=0`、63s、load 4.80，`/tmp/batchm7-cells.log`）—— 不是判据松，是**那个变异根本没有现场**：单格变异（把兜底那句"建出来了没有"的核验退化成无条件报成功）只在"兜底自己也建不出表"时可观察，而基线代码里模型循环的四条出口没有一条会把"缺表"留到兜底之后（S20 建回来了、S21 表在场、S22 当场 panic）。处置是**造现场而不是改产码**：让模型要的那个名字被一枚**视图**占住。四档探针实测（`postgres` 库上 `zz_probe2_*`，跑完即删、收尾残留 0）：

- `P1b CREATE TABLE`（名字被视图占住）→ `ERROR: relation "…" already exists` ⇒ 落进容忍表（分类器那句 `"already exists"`），所以 `cerr` 那一支不响、真正响的是「后表仍缺失」那一句 —— 这也是否证第 5 段那条复审意见的根据；
- `P2 CREATE TABLE (id 未知类型)` 同时名字又被视图占住 → 报的是 `type "…" does not exist`，**早于**执行期的名字冲突检查 ⇒ 解析/分析期就先炸，拿不到想要的"名字冲突型非容忍错误"；
- `P3 CREATE TABLE IF NOT EXISTS` 撞视图 → `NOTICE: … already exists, skipping` 之后正常返回 ⇒ 这条夹具能成立**全靠 GORM 建表不带 `IF NOT EXISTS`**（`migrator.go:225` 那条路径）；
- `P4 DROP TABLE <视图>` → `ERROR: "…" is not a table` ⇒ 兜底那句 `DROP TYPE … CASCADE` 也带不走视图。

腿的判据面因此是三块：panic 了没有（`recover` 到的是 **string** 而不是 error —— 产码是 `panic(fmt.Sprintf(…))`，所以腿里 `switch` 两支都接、红因写"是哪一枚对象占的"）、日志里**不许**出现那句『已重建缺失表』、状态面 `relkind` 仍是 `v` 且视图行数仍是 2（兜底没把别人的对象删掉）。⇒ 与 §23.11 第 2 段那条方法论同源：一条"构造不出来"的登记，先问"要造它得先有什么"，问得出配方就不该挂着。

**5 · 二次检查（独立只读复审）指认八处：七处成立并已改，一处经探针否证；收口时自己又抓到第九处。** 七处全在判据面、零产码：① 汇总行断言用的是全局 `strings.Contains`，而「AutoMigrate 完成，无迁移漂移」是「…但存在可容忍的迁移漂移」的**子串**之外的两句、彼此却会被同一趟日志同时读到 ⇒ 新增 `batchM7LinesWith(log, 子串…) `按行 AND 匹配；② S20 那句"不许出现无漂移汇总"的负断言是同义反复（红不了），改成"两条汇总行恰好出现一条"；③ S20 第二段改判"表名不再出现在任何一行"而不是判那句全局汇总行；④ S21 的 already-exists 面从"两条都在场"折叠成本腿该判的那一面，另一面指回 S20（写明在谁的腿上）；⑤ S22 必须**同时**读到 `null values` 与表名（只读一个，别的表上的同类错误也能让它绿）；⑥ S23 头部机理写错（把 23502 当成"`&&` 退化成 `||`"那一刀的杀手，实测杀手是 23514 / 42P01 那一族）⇒ 只改注码、判据一行不动；⑦ S25 从"日志里出现过扩展提示"改成**逐家点名**（提示行数 + 带该名字的行数两个计数）。第八处在 `index_shape`：S28 那条 `COUNT(*) … stillDefault != 2` 是**死判据**（它读的共享夹具 `batchMInsertObs` 自己会 `t.Fatalf`，判据根本走不到），换成"第二条默认插得进去"的错误面，并为避开 `Fatalf` 另写错误返回版 `batchM7InsertObs`。**否证的那一条**：复审说"兜底那句 `cerr` panic 是可构造的"⇒ 第 4 段的 P1b/P2 量到视图占名下 `CreateTable` 报的是可容忍的 42P07、非容忍的那一支又被解析期类型错误挡在前面，且同样的类型错误会先让**外层**那次 AutoMigrate 红在 `panic(err)`（`if err := DB.Migrator().AutoMigrate(m); err != nil {` 那一支）⇒ 进不到兜底，不补腿，登记在第 6 段。**第九处（自抓）**：本文件头部立的规矩是"指认产码位置一律用符号 + 代码字面、不写行号"，同一只手上却还留着 `（:417）`、`（:614）` 两个行号 —— 而并行泳道往 `allModels()` 里加了 Quote 登记（6 行），活树与影子树从 :346 起整体错位，那两个数字在影子里指向别的行。改成产码字面后，机械证过与影子那份的 diff **0 行非注释**（`diff | grep -c '^[<>] [^/]'` = 0），仍按「改过用例后逐刀复跑」把 18 格全复跑一遍（第 7 段 r3b）。

**6 · 两条分支仍然没有启动腿，理由从"想不出来"换成"量过界"。** ① `AutoMigrate()` 末尾的 `missingTables` 终校验 panic：模型循环四条出口已全部有腿，没有一条把"缺表"留到循环之后 ⇒ 基线下构造不出触发现场，本批只判纯函数的**判定半径**（S26 直调；倒装条件 `if db.Migrator().HasTable(m)` 在启动路径上没有任何一条腿会红，只有 S26 会 —— 这条差异就是这条腿存在的理由，格 M94/M95/M96）。② 兜底那句 `cerr` panic：见第 5 段末的否证链条。⇒ 这两句在账上是**已知无腿**，不是"已覆盖"。

**7 · 代价，写在账上。** `-run TestBatchM_` 子档（25 条腿，影子树）在四趟上读到 **47.930s / 48.060s / 62.679s / 62.740s**（`-count=2` 那趟 50 条腿 93.174s ≈ 每条翻倍）⇒ 摆动 1.3 倍、**负载主导**（起跑 1 分钟负载 3.59 与 7.96），不是腿变慢了；本批不给这一档设 CI 预算（CI 跑的是全包无过滤）。活树无过滤全量门两趟：`ok 70.170s`（改口前，load 6.03）与 `ok 59.028s`（最终字节），顶层都是 `PASS=55 / FAIL=0 / SKIP=0`，其中 **6 条属并行泳道的 `quote_migration_test.go`**、不属本批（影子树同一趟是 49 条，见第 9 段第 ⑤ 条）。电池：r1（新 20 格）19 KILLED + 1 存活（M90，第 4 段）；r2 覆盖整族 db 51 格（`21:41:49 → 22:23:32` ≈ 42 分钟，KILLED 单格 27–60s、负载 3.28–9.75）；r3（判据改口后的 18 格，`22:35:57 → 23:01:26` ≈ 25 分钟，31–233s、负载 5.01–27.22）；r3b（注释改口后的同 18 格，55–152s、负载 5.76–**54.39**）。⇒ 三趟同内容的单格耗时从 27–60s 涨到 55–152s，而 **`KILLED` 一条没变、`被计时截断的格 = 无`** ⇒ 杀信号不看计时，`-timeout 600s` 在最重的那一趟负载（54.39）下仍有四倍余量。

**8 · 门禁与证据（最终字节 = `760d1209…` + `b4f72bb5…`，都在最后一次编辑之后）。**

| # | 步骤 | 结果 |
| --- | --- | --- |
| 1 | 预检：影子树与活树逐个 md5 | `same` ×10（`internal/pkg/db` 全部 9 个用例文件 + `migrate.go` 除下列两处外）；**DRIFT 两处且都属并行泳道，本批不同步也不回滚**：`migrate.go 81b518…(活，含 Quote 登记 6 行) vs 8675623…(影子)`、`quote_migration_test.go` 影子侧 `MISSING` ⇒ 电池跑的那份产码不含那 6 行，两棵树各自全绿是**两件事**（见第 9 段第 ⑤ 条） |
| 2 | 落盘与数据层 | `avail=81914Gi`；`127.0.0.1:8232 接受连接`；起跑 load 7.96 |
| 3 | `--check db`（跑之前验锚点） | `ANCHOR-OK`（末三行 M94/M95/M96 逐条列出锚点原文），`ANCHOR-BAD` 0 |
| 4 | `control`（最终字节，无过滤子档） | r3 趟 `rc=0 top=25 FAIL=0 SKIP=0 ok 47.930s`、`-count=2` `rc=0`（50 条腿，`ok 93.174s`）、`-shuffle=on` `rc=0`（`ok 62.740s`）；r3b 趟 `rc=0 top=25 FAIL=0 SKIP=0 ok 62.679s` |
| 5 | r1 电池（新 20 格） | `未杀格 = ['M90']`，其余 19 格 KILLED，`等价格误红 = 无`、`被计时截断的格 = 无`、`收尾残留 = 无` |
| 6 | r2 电池（整族 db 51 格复跑） | `未杀格 = 无` ⇒ 49 KILLED + 2 EQUIV-OK（M61/M63，等价格沿 §23.12 第 9 段第 ④ 条的版本绑定），`md5_restored=True` 全部成立 |
| 7 | r3 电池（判据改口后的 18 格） | `KILLED M77 M78 M79 M80 M83 M84 M85 M86 M87 M88 M89 M90 M91 M92 M93 M94 M95 M96`（18/18），汇总 `未杀格 = 无`；跑完 `migrate.go` 回 `8675623…`（= 影子基线） |
| 8 | r3b 电池（**最终字节**，同 18 格） | `KILLED M77 M78 M79 M80 M83 M84 M85 M86 M87 M88 M89 M90 M91 M92 M93 M94 M95 M96`（18/18），汇总 `未杀格 = 无, 等价格误红 = 无, 被计时截断的格 = 无, 收尾残留 = 无`，`BATTERY-rc=0` |
| 9 | 活树无过滤全量 `go test -p 1 -count=1 -timeout 1800s -test.v ./internal/pkg/db/`（两趟：注释改口前、最终字节） | r3 趟 `rc=0 PASS=55 FAIL=0 SKIP=0 ok 70.170s`（load 6.03）；r3b 趟 `rc=0 PASS=55 FAIL=0 SKIP=0 ok 59.028s` |
| 10 | **无过滤复跑 + 变异叠加**（把 N-36 的修复整体摘掉：`verifyUniqueIndex` 三处消费一次叠满、helper 定义留着） | 影子树全包 49 条腿：`rc=1 topPASS=46 topFAIL=3 合计=49(=控制组) panic=0`，红的三条**恰好是 S27/S28/S29** ⇒ 其余 46 条腿（含批M/2/4/5/6 全部腿与 24 条非 `TestBatchM_` 旧腿）对这一格缺陷**零判据**，本批三条新腿是全仓第一批。生效断言：三个锚点各命中 1 次、`diff` 10 行且**非注释 10 行**、摘完 `verifyUniqueIndex(` 只剩 1 行（定义）；跑完还原 `md5 == 86756237…`。另有一趟对照：只摘 message_hub 那一处 ⇒ `topPASS=48 topFAIL=1`，只有 S29 红（同一符号三处消费、逐处独立可观察）。驱动 `/tmp/bm7_fullpkg_revert3.py`、`/tmp/bm7_fullpkg_revert.py`，日志 `/tmp/bm7_fullpkg_revert3_testv.log`、`/tmp/bm7_fullpkg_revert_testv.log` |
| 11 | 槽库残留扫描（`zz_batchm%` 对象，slot0…8） | 三趟各 9 个库全部 `objects=0`；探针用的 `zz_probe2_*` 亦已删场（收尾 `残留=0`） |
| 12 | `gofmt -l internal/pkg/db/` ／ `go vet ./internal/pkg/db/`（活树与影子树各一次） | 空 ／ `rc=0` |
| 13 | 日志分轮存 | `/tmp/batchm7-cells.log`（r1）、`-round2.log`、`-round3.log`、`-round3b.log`；驱动与每格原文 `/tmp/batchm_battery/{,m7r2,m7r3,m7r3b}/`；控制趟 `/tmp/bm7_ctrl_r3_c1.log`、`_c2`、`_shuf`、`/tmp/bm7_ctrl_r3b.log`；活树门 `/tmp/bm7_live_gate_r3.log`、`_r3b.log`。**一处按丢的记**：本批早前还有一趟 `TestBatchM_` 读到 77.565s，它的日志已被同驱动的下一趟就地截掉 ⇒ 第 7 段那一串耗时里不含它，结论不依赖它 |

> **第 5–8 行的 `KILLED` 少了一条断言，§23.15 已补并改口。** 那时电池只判"指定腿出现在 `--- FAIL` 里"（下界），不判"这一趟有没有跑满该包声明的全部顶层腿"（上界）。拿存下来的逐格日志复盘：r1 那趟 db 每格 24 条腿（当时确实只有 24 条，r2 起 25 条 ⇒ 不是截断），四趟里**唯一**没跑满的是 **M94 = 12/24（r1）与 12/25（r2/r3/r3b）**，且四趟都带同一枚 `panic: runtime error: invalid memory address`。⇒ "18/18 KILLED" 里 M94 那一格从来只有下界。其余 47 格（r2 全 51 格中除 M94）腿数=当轮分母，结论不动。电池驱动已加"跑满"门（`BROKEN-NOTFULL`，计进未杀格），并全量复audit一遍，见 §23.15 第 4、5 段。

**9 · 本批未覆盖面（不得当作已验证）。**
① **真实库里那三枚索引今天还不存在**（与 §23.8 第 7 条同一代价理由：补这条要真启动一次携带未提交改动的 API 进程，会把别的泳道的迁移一起推进共享开发库）。本批能证明"形状不对时启动日志会点名"，不能证明"生产上那三枚已经是唯一的"。
② ~~**旧腿的连带只在 N-36 那一族叠刀上无过滤复跑过**（第 8 段第 10 行：49 条腿里只红本批三条新腿）。**容忍／兜底／终止那 14 格（M83–M96）没有做同规格的无过滤复跑**：它们的现场全在本批新腿的夹具里（复合类型残留、手写约束名、`not null` 加列、视图占名），旧腿根本没有那种现场，所以~~"旧腿不会被这些刀打到"这一句对它们是**读出来的**、不是跑出来的。~~ **整族 51 格已做同规格无过滤复跑（§23.15），这一句被量否了两次**：M73/M74/M82 三格在 49 条腿里各打红旧腿 `TestOpportunityClueIDPartialUniqueIndex`（同一道 partial 索引、同一个理由：那条旧腿自己就会建/判这枚索引的形状），M95 打红并按序先崩在旧腿 `TestAutoMigrate`。全 51 格没有一格红出控制组 49 条以外的腿，48/51 跑满；剩下 3 格（M75/M94/M95）是 panic 带走二进制 ⇒ **那三格的红集合上界在这一口径下判不了**，而电池的杀信号本身仍只含 `-run TestBatchM_` 过滤集（M94 在四趟过滤电池里都是 12/25，见 §23.15 第 4 段）。
③ **视图占位是合成模型的现场**（`zz_batchm7_viewblock`），真模型的名字被视图占住这种事在生产里从没观察到发生过；本批判的是"发生了会怎样"，不是"它会发生"。同理 S20/S21/S22 的三个现场都是手工摆出来的历史残留形态，不代表任何一台真库今天长这样。
④ 第 6 段那两条分支（终校验 panic、兜底 `cerr` panic）**无腿**，只挂了判定半径。
⑤ **影子树与活树的产码不同一份**：影子缺并行泳道那 6 行 Quote 登记，也缺它的 6 条腿 ⇒ 电池（影子）与全量门（活树）验的不是同一字节；两棵树各自绿是本批实际做到的强度，交叉那一份（活树产码 + 电池变异）没跑过。
⑥ 等价格 M61/M63 的版本绑定（`gorm.io/gorm v1.30.0` + `gorm.io/driver/postgres v1.6.0`）沿 §23.12 第 9 段第 ④ 条；本批没有新增等价格，r2 里这两格仍 `EQUIV-OK`。

### 23.15 批M-7 后续：把"上界"从读码换成跑码，顺带照出电池自己少一条断言（产码零改动，电池判据链加一类）

**1 · 来路。** §23.14 第 9 段第 ② 条留的是一条**读出来的**结论（"容忍／兜底／终止那 14 格打不到旧腿，因为旧腿没有那种现场"），第 8 段第 5–8 行的 `KILLED` 也只建立在"指定腿出现在 `--- FAIL` 里"这一条下界上。这一段把两者都换成跑出来的：整族 db 51 格在**无过滤整包**口径下重跑一遍，并给电池补上"跑满"断言。**产码零改动**；动了一处用例（第 5 段，S26 的 nil 那一支）和取证侧两个脚本（`/tmp/batchm_battery.py` 的判据链、`/tmp/bm7_fullpkg_upper.py` 新写）。

**2 · 方法（与 §23.14 第 8 段第 10 行那趟叠刀复跑同一套机制，差别只在跑法）。** 驱动 `import` 电池模块拿同一份 `CELLS`/`EQUIV`/`apply`/`restore`/`ensure_clean`，不复制；跑法换成 `go test -p 1 -count=1 -timeout 1800s -test.v ./internal/pkg/db/`（**不带** `-run TestBatchM_`），影子树。开跑前先跑一次**同口径控制组**，拿"本来就该绿的顶层集合"当分母与红集合上界：实测 `rc=0 topPASS=49 topFAIL=0 63.4s`（49 = 25 条 `TestBatchM_` + 24 条旧腿）。分类口径按顺序：`合计 != 49` ⇒ `BROKEN(未跑满)`（把 `^panic:` 首行一起印出来供归因，不当结论）→ `cid ∈ EQUIV` ⇒ `EQUIV-OK/DIED` → `rc == 0` ⇒ `SURVIVED` → 指定腿没红 ⇒ `WRONG-KILLER` → 红集合 ⊋ 指定腿 ⇒ `KILLED+连带` → 恰好 ⇒ `KILLED+EXACT`；另有一列 `extra_vs_control = 红集合里出现控制组没有的腿`（那是"注码把包弄坏到不编译"那一类，和"杀掉别家"不同罪）。落盘前测一次 `md5(migrate.go)`、每格还原后再测一次，收尾要求回到基线。`BATTERY_LOGDIR` 用全新目录（旧 `bak/` 会在每格开跑前把产码退回上一轮，§23.14 第 9 段那条陷阱的同族）。

**3 · 结果：48/51 跑满，0 格红出控制组之外的腿，但"打不到旧腿"被否了两次。**

| 分类 | 格数 | 说明 |
| --- | --- | --- |
| `KILLED+EXACT`（红集合恰好 = 指定腿） | 21 | 上界与下界同时成立 |
| `KILLED+连带`（红集合 ⊋ 指定腿） | 25 | 连带全在批M/2/4/5/6/7 的新腿之间，除下表那 3 格 |
| `EQUIV-OK` | 2 | M61/M63 在**无过滤**口径下仍全绿 ⇒ 等价登记不因口径换面而失效 |
| `BROKEN(未跑满)` | 3 | M75 48/49、M94 29/49、M95 6/49，三格都带 panic |
| `SURVIVED` / `WRONG-KILLER` / `extra_vs_control` | 0 / 0 / 0 | 没有任何一刀把包弄坏到编译不过或多出腿 |

被否证的那半句：连带里**确实有旧腿**——`TestOpportunityClueIDPartialUniqueIndex` 在 **M73**（丢 `WHERE` 谓词）、**M74**（谓词换成 `IS NOT NULL`）、**M82**（`UNIQUE` 退成普通索引）三格各红一次（M75 也红它，但那格未跑满）。理由不是意外，是读那位旧作者的用例读出来的：它的"判据 4 · 钩子可重跑"自己就 `postMigrateOpportunityClueUniqueIndex(testDB)` 直调两次，然后插三条空串来源线索（"谓词没生效"就被判红）、再插一条重复的 `clue_three` 并断言 `err != nil`（"重跑两次钩子后唯一性没了"）⇒ **同一道索引、同一个形状事实源，旧腿判的正是这三刀改的东西**。M75 更直接，而且它同时是第 4 段那个机制的**现场样本**：那一刀把钩子的 Warn+return 换成 `panic(err)`，旧腿正是那次的调用方 ⇒ 日志里指定腿先有一行真红因（`存量重复把启动弄成了 panic ⇒ 一次历史旁路写入让整个服务起不来`）再印 `--- FAIL`，而旧腿名下那一行 `--- FAIL: TestOpportunityClueIDPartialUniqueIndex (0.04s)` **上面一行自己的失败输出都没有**、紧跟着就是 `panic: … could not create unique index …(SQLSTATE 23505)` ⇒ 那行 FAIL 是崩溃时替它补印的，不是它判红的。第二处：M95（终校验条件倒装）在无过滤顺序里先撞旧腿 `TestAutoMigrate` 并把它崩掉（跑到第 6 条），指定腿根本没轮到 ⇒ 它在过滤集里是 25/25 全跑、11 条红的正常击杀，在无过滤口径下**上界判不了**。⇒ ② 已就地改口，两句"读出来的"划掉。

**4 · 电池自己的缺陷（这一趟顺手照出来的）。** `/tmp/batchm_battery.py` 的判据链只有下界：`rc != 0 and died and md5ok → KILLED`，`died` 只是"指定腿的名字出现在 `--- FAIL` 里"。而**一条用例 panic 会带走整个测试二进制**，`testing` 在panic 路径上照样替当时在跑的那条腿印 `--- FAIL` ⇒ 看起来是一次干净击杀，实际其余腿一条都没跑。拿存下来的逐格日志复盘四趟历史（r1/r2/r3/r3b，db 族）：**唯一**中招的是 M94（`12/25`，r1 时是 `12/24`——那一轮分母本来就少一条，不是截断），四趟全带同一枚 `panic: runtime error: invalid memory address` ⇒ §23.14 第 8 段第 5–8 行那句"18/18 KILLED"里，这一格从来只有下界。其余格腿数=当轮分母，结论不动（旧轮 repository/service 的逐格 `go test` 原文没留 ⇒ 无法事后判断，只能复跑，见第 6 段）。修法是加一类，不是改期望：

- `toplevel_results(out)`：只认顶格的 `--- PASS/FAIL/SKIP:`，得到"这一趟真正交了卷的顶层腿"集合；
- `expected_legs(pkg)`：分母用 `go test -list '^TestBatchM_' <pkg>` 现取（只编译不跑，约 5s，取到就缓存）；取不到就**抛错拒绝出结论**，不退回"数源码里的 func"那种会漏 build-tag 的写法；
- 新分类 `BROKEN-NOTFULL`：放在 `ENV/BUILD` 之后、`KILLED` 之前，计进未杀格 ⇒ 驱动 `rc=1`，并把缺席腿与 panic 行打出来；EQUIV 链同步加 `NOTFULL-BROKEN`；
- **`-timeout` 截断仍按老口径**（不改判、继续进 `truncated`）：超时不会替在跑的腿印 `--- FAIL`，所以 `died=True` 那一行仍是真红，截断只说明其余腿状态未知 ⇒ 与"panic 带走"不是一回事，两类分开记；
- 起跑时打印一行分母，让每一趟自己带着口径出门。

反向测试（门的门，`/tmp/bm7_notfull_gate.sh`）：同趟混跑 5 格 = 3 格历史上有 panic 风险 + 2 格对照 ⇒ `KILLED M75 M93 M95 M96`（全部 25/25）与 `BROKEN-NOTFULL M94 12/25`、汇总 `未杀格 = ['M94']`、驱动 `rc=1`、跑完 `migrate.go` 回 `8675623…`。门既没漏抓（M94 改判）也没误伤（对照四格仍算杀）。

**5 · 由 M94 引出的一处用例侧修（不是改判据，是把崩溃关进本腿）。** M94 摘的是 `missingTables` 里 `if db == nil { 全报缺失; continue }` 那三行守卫，而 S26 的 ② 那一支**直调** `missingTables(nil, …)` ⇒ 变异下先崩后红，崩的是整包。改成走新的 `batchM7MissingTablesSafely`（`defer recover()`，命名返回值把 panic 落成 `t.Errorf` 的红因）：判据一个字没动（仍要求"两条全报"），基线下行为不变，变异下崩溃变成**这一条腿自己的红** ⇒ 其余腿照跑，上界重新可判。活树单腿复跑（`-run TestBatchM_MissingTablesAndTableNameHelpers`）`--- PASS (30.27s) ok 30.869s`，`gofmt -l` 空、`go vet ./internal/pkg/db/` 绿。⇒ **本批改了 §23.14 第 8 段那句"最终字节"的效力**：`migrate_tolerance_batchm7_test.go` 由 `760d1209…`（702 行）变到 `b568b1a1…`（717 行），`index_shape_batchm7_test.go` 仍是 `b4f72bb5…`，产码 `migrate.go` 未动；两棵树同步后按第 6 段那趟整族复跑取用（影子树与活树这两个文件已逐一 `md5` 核过相等，不是"应该是同一份"）。

**6 · 加了"跑满"门之后的 93 格复audit（repository／service 两族全过，db 族那一趟中途掐掉）。** 门一改，历史结论里"每格都跑满"这句就成了未被验证的假设 ⇒ 按 §23.14 第 9 段与记忆里的老规矩，动过判据链就要拿全量重跑一遍，不能只挑改过的那一族。驱动 `/tmp/bm7_full_g1.sh`（起跑前断言 `LOGDIR/bak` 不存在且为空，逐阶段单独 echo `rc`），LOGDIR `/tmp/batchm_battery/full_g1/`，影子树，`-run TestBatchM_` 过滤口径不变：

| 族 | 分母（现取） | 结果 | 逐格耗时 min/中/max | 阶段墙钟 |
| --- | --- | --- | --- | --- |
| repository（18 格） | 12 条顶层腿 | `KILLED` 18、未杀格 = 无、未跑满 = 无、等价格误红 = 无、计时截断 = 无、收尾残留 = 无，阶段 `rc=0` | 4 / 13 / 30s（合计 266s） | 01:17:18 → 01:21:47 = 269s，起跑 load `{3.18 3.78 4.79}`，逐格 load 3.07–24.33 |
| service（24 格） | 10 条顶层腿 | 同上，全 `KILLED`，阶段 `rc=0` | 10 / 11 / 15s（合计 271s） | 01:21:47 → 01:26:30 = 283s |
| db（51 格） | 25 条顶层腿 | **未交卷**：01:27:41 `rc=143` | — | 起跑 01:26:30 后 71s 掐掉 |

db 那一趟掐掉的理由是它跑的是**旧用例字节**：S26 的 recover 化落在 repository／service 两族跑完之后，按"改过用例后逐刀复跑"的规矩，旧字节那趟对 db 族不作数 ⇒ 与其让它出两份混着口径的结论，不如掐了同步字节再跑（第 7 段）。**掐的代价如实记一笔**：中断正好落在一格注着码的时候，影子树 `migrate.go` 停在 `7574a51f…`，日志末尾那行 `POST md5(migrate.go)=7574a51f…` 记的就是这个脏状态（不是基线）；没有对未提交文件用 `git restore`，用 `cp full_g1/bak/internal_pkg_db_migrate.go` 就地写回并复验回 `86756237…`。两族收尾 md5 均回基线：`internal/repository/obs_config.go = 75a95049…`、`internal/service/obs_config.go = 9cfc4c1f…`（与影子树现值一致）。这两族的结论至此在新门下发过言：**18/18 与 24/24 都在"下界 + 上界"同时成立**，且它们的产码字节本轮未动。

**7 · 新字节上的 db 族整族复跑（`db_g2`）：51 格 = 49 `KILLED` + 2 `EQUIV-OK`，未杀 = 无、未跑满 = 无。** 驱动 `/tmp/bm7_db_g2.sh`，LOGDIR `/tmp/batchm_battery/db_g2`（全新）。起跑前三件事：逐文件 `md5` 证明影子树与活树的 S26 用例是同一份（两边都 `b568b1a1…`）、`--check db` 锚点预检、过滤集控制组（`repository pass=12 / service pass=10 / db pass=25`，全 `rc=0 skip=0`）。逐格 **47 / 51 / 67s**（合计 2565s），墙钟 01:30:09 → 02:14:41 = **2672s**，load 2.89–6.55，收尾 `migrate.go` 回 `86756237…`。分母仍是现取的 25 条腿 ⇒ `未跑满的格 = 无`，也就是**M94 从四趟历史的 `12/25` 变成 `25/25`**：第 5 段那句"把崩溃关进本腿"至此由复跑坐实，而不是由"我改代码时看了一眼"坐实。

无过滤那一侧同规格复判（M94／M95／M75，驱动换成族参数化的 `/tmp/bm_upper_pkg.py`，LOGDIR `upper_m94_r2`，控制组 `rc=0 顶层=49（其中旧腿 24 条）FAIL=0 panic=0 60s`）：**M94 = `KILLED+EXACT 合计=49/49`**（此前是 `29/49` 未跑满）⇒ recover 化在无过滤口径下同样把上界判回来了。M95 仍 `6/49`、M75 仍 `48/49`，且**这两格永久判不了**——崩溃产自**产码**（M95 是终校验那句 `panic` 在旧腿 `TestAutoMigrate` 里当场响；M75 是钩子里 `panic(err)` 被旧腿自己的建索引调用带走），用例侧的 recover 救不到、也不该救（救它＝改掉产码的 panic 语义）。⇒ 这两格的账现在是两句：过滤口径 25/25 全跑、指定腿真红（有磁盘原文）；无过滤口径只到 6/49、48/49。**没有把它当成"击杀 + 一点小瑕疵"记**，与 §23.14 第 9 段那条"未跑满不许当已验证"同一口径。

**7b · 这一轮在取证驱动自己身上抓到的三处（都不影响被测面，但都会造出自洽的假账）。**
① **显示层管道吞掉了本段唯一的结论行**：`bm7_db_g2.sh` 第 24 行把无过滤复判那段接进 `| tail -12`，而那段先打 `ROOT=`/`CONTROL`、再按 `M94 M95 M75` 顺序逐格打 ⇒ `tail -12` 恰好留下 M95／M75 与汇总，**吃掉了 M94 那一行**（也就是唯一"从未判变成可判"的那格）和**控制组那一行**（跑满判据的分母）。汇总里的 `KILLED+EXACT 1` 仍对得上，所以只看汇总一切正常。规律同 `| tail -3` 吞红因那一族，但这次被吞的是**排在最前的、唯一变了的那一行**：`tail` 保留尾部，而驱动的固定版式恰好把控制组与首格放在头部 ⇒ 结果**专吃对照与新信息**，最该留的两行最先没。处置是**同口径重跑一次**（`/tmp/bm7_db_g3.sh`，不接 `tail`、整段落盘），而不是拿汇总去补那句话。
② 族参数化驱动的**还原核验时机错**：`restored_ok()` 被放在 `restore()` 之前 ⇒ 注码还挂在树上时比"当前 vs 备份"必然不等，三格全报假的"读取还原失败"（真信号被假信号淹没，而收尾 md5 显示确实回了基线）。挪到 `restore()` 之后并打 `RESTORED … 全部回基线=是`，由 repository 的 M01 单格反向测试复验（第 8 段）。
③ 同一驱动的**收尾残留扫描按整族 51 格取文件**，而本趟只跑 3 格 ⇒ 其余 48 格的备份从未落进 `$BAKDIR`，`md5()` 抛 `FileNotFoundError` 把汇总之后的一切刷掉。改成只扫本趟跑过的格。
另有一处**预检也被截**：第 18 行 `--check db | tail -3` 把 48 条 `ANCHOR-OK` 吃成 3 条 ⇒ 整族锚点预检随后在不注码、不起测试的口径下补齐（`/tmp/bm7_anchorcheck_g2_{repository,service,db}.log`：18 / 27+6 / 53 行，全 `ANCHOR-OK`、`rc=0`）。顺手核清那 6 行 `SUB-DYN` 不是缺陷：它们是**子档名为拼接名**、要靠 `--subs` 真跑核对的格 ⇒ 本轮不必重跑 `--subs`，因为电池对带子档的格要求 `--- FAIL: 父/子` **全串命中**才算杀，拼接名一旦漂就杀不掉，而两族 42 格全 `KILLED` 已经反证这六个名字今日合法。

**8 · 代价账（这一轮量出来的三个数，决定"补哪一侧"的取舍）。** 无过滤整包口径 = **过滤口径的 1.2–40 倍**，且分母差得越远的族越贵：

| 口径 | 分母（顶层腿） | 单趟耗时 | 全族格数 ⇒ 估算 |
| --- | --- | --- | --- |
| db 过滤 `-run TestBatchM_` | 25 | 47 / 51 / 67s（本轮 db_g2） | 51 格 ⇒ 实测 2672s ≈ 45 分钟 |
| db 无过滤 | 49（旧腿 24） | 控制组 60s；格 9 / 63 / 141s（`upper_s1` 51 格，合计 3420s，load 3.42–40.39） | 51 格 ⇒ 实测 3483s ≈ 58 分钟 |
| repository 无过滤 | **797**（旧腿 785） | 控制组 **86s** | 18 格 ⇒ ≈ 26–45 分钟 |
| service 无过滤 | **3564**（旧腿 3554） | 控制组 **399s** | 24 格 ⇒ ≈ 2.7–6 小时 |

`upper_s1` 那趟（00:01:18 → 00:59:21，控制组 63.4s）与本轮 db_g2 的差值说明：**db 这一包加过滤集是划算的**（49 → 25 腿，单趟省一半），而 repository／service 两族的旧腿是 785 / 3554 条量级 ⇒ 无过滤口径在那里不是"多跑 24 条腿"，是"多跑一整包"。

**三个包的"声明腿 vs 交了卷的腿"逐一对过（门自己也要有第二口径）。** db：`(t *testing.T)` 声明 49 = 无过滤观察 49，该包没有 `TestMain`、没有 `//go:build ignore` 的测试文件 ⇒ 本段所有 `49/49` 有独立口径可对。repository：声明 797 = 观察 797、0 skip。**service：三个数不一样，且差得有名字**——控制组 `PASS+FAIL = 3564`（驱动计数口径），另有 3 条 `SKIP` ⇒ 交卷的顶层腿 3567；而按函数签名数声明是 3579 ⇒ **多出来的 12 条从不参与编译**，全在 `internal/service/auth_domain_test.go`（该文件首行 `//go:build ignore`），是 12 条 `TestAuthService_*`。⇒ "数源码当分母"在这里当场高估 12 条；先前从别处引来的"3583"也不可用（那个数把 `TestMain` 也算进了 `^func Test`）。两条口径顺带钉住：驱动的 `TOTAL` 只数 `--- PASS/FAIL`，所以"某腿从跑变成跳过"会让 `合计` 掉下来、被判成 `BROKEN(未跑满)`（保守方向，不会悄悄放行）；而控制组自己跳过的那 3 条永远进不了任何一格的分子——它们是 §20.6／§21 归过因的结构性环境跳（chromedp 登录腿 + 两条 `login()`），不是本批漏判。

**9 · repository／service 两族的无过滤上界：42 格全部"上界 = 下界"，一条旧腿都没被打到。** 第 8 段那三个数的用途就是决定这件事划不划算，结论是划算（repository 半小时、service 两小时出头，全在夜里跑），于是 task #57 从"先量代价再决定跑不跑"直接变成跑完。驱动 `bm_upper_pkg.py` 按 `UPPER_PKG` 参数化（同一份 `CELLS`/`EQUIV`/`apply`/`restore`，只是 `TARGET` 与分母换族），外壳 `/tmp/bm7_upper_rs.sh` 分三段、各自全新 LOGDIR（`upper_repository_probe` / `upper_repository_full` / `upper_service_full`），影子树，跑法 `go test -p 1 -count=1 -timeout 1800s -test.v ./internal/<族>/`（不带 `-run`）。日志 `/tmp/bm7_upper_rs.log`，127 行，**全程没有过 `tail`/`head`**（第 7b 段那条口径的兑现）。

| 段 | 墙钟 | 控制组 | 单格用时 min/中位/max | 判定 |
| --- | --- | --- | --- | --- |
| 反向测试（repository M01 一格） | 03:09:17 → 03:11:45 | `rc=0 顶层=797（旧腿 785）FAIL=0 panic=0 75s` | 72s | `KILLED+EXACT 合计=797/797` ⇒ 打印 **`PROBE-OK`** |
| repository 全族 18 格 | 03:11:45 → 03:43:53（**1948s**） | `rc=0 顶层=797 FAIL=0 97s` | 94 / 100 / 113s（合计 1830s） | **`KILLED+EXACT` 18**，连带／非击杀／还原失败／残留全为「无」 |
| service 全族 24 格 | 03:43:53 → 06:37:45（**8032s**） | `rc=0 顶层=3564（旧腿 3554）FAIL=0 panic=0 423s` | 341 / 430 / 489s（合计 10012s） | **`KILLED+EXACT` 24**，四行「无」同上 |

`PROBE-OK` 那一行是**先决条件而不是装饰**：驱动 `bm_upper_pkg.py` 在本轮之前刚被改掉两处自身缺陷（`restored_ok()` 放在 `restore()` 之前 ⇒ 每格假报"读取还原失败"；残留扫描按整族格号取备份 md5 而本趟只跑几格 ⇒ `FileNotFoundError` 死在汇总之后）。这两处**都由 M01 这一格说了算**——它输出 `RESTORED M01 files=internal/repository/obs_config.go 全部回基线=是` 且收尾 `POST 残留=…：无`，才允许放整族。这一格顺带也是一次有效测量。

**与 db 族正好对称，而且这个对称是跑出来的。** db 那 51 格里有 **25 格红到指定腿之外**（连带全在新腿之间，另有三格连带到旧腿 `TestOpportunityClueIDPartialUniqueIndex`）；这两族 42 格的红集合**每一格都恰好等于指定腿**，`extra_vs_control` 42 趟全为「无」。机理不在"这两族写得更好"，在**事实源是否共享**：这 42 刀的落点全在 `obs_config.go`（repository／service 两侧）的选取与写入谓词上，其后果只有批M 系列的新腿读；而 db 那三格改的那道 partial 索引，旧腿自己就在钩子里直调两次并判它的形状。⇒ 一句只读码就能猜的话，这里同样是 42 趟逐格与当趟控制集合比对出来的，且 42 趟 `合计` 全部跑满（797/797 与 3564/3564，无一次 `BROKEN(未跑满)`）。

**电池不是"只动产码"：M15 一格同时动三份文件。** 它的 `RESTORED` 行列出 `internal/repository/obs_config.go,internal/service/obs_config.go,internal/service/obs_config_batchm_test.go` 三份、且三份都回基线。⇒ 有一格的注码落在**用例侧**（跨树那格），读电池时别以为变异只进产码。

**收尾（三族产码 md5 逐字回到起跑值）**：`75a9504911bb1650070828024a8fdaec`（repository）／`9cfc4c1fa4e9bfa7c09fc66e41a074c0`（service）／`86756237de11d98e06ac9ea287777654`（db）。全程 1min 负载 2.85–6.33，起跑磁盘可用 60,518,508 KB，无 ENOSPC、无被计时截断的格。

**93 格的双界总账（第 3、7、9 段合起来）**：`KILLED+EXACT`（上界=下界）**64 格** = repository 18 + service 24 + db 21 + M94 1；`KILLED+连带` **25 格**（全在 db）；`EQUIV-OK` **2 格**（M61/M63，无过滤口径下仍绿）；**只有下界、上界永久不可判 2 格**（M75 把钩子的 Warn+return 换成 `panic(err)`、M95 终校验条件倒装，二者在无过滤顺序里都先崩在**旧腿**身上，而旧腿是那次的调用方 ⇒ panic 由产码产生，recover 化解决不了）。⇒ 64 + 25 + 2 + 2 = 93。

**这一段的字节边界（不许当成最终字节）。** 42 格跑在影子树的 B1 用例上。03:31（repository 阶段中途）我在**活树**改了四份用例文件里的四处「未判空即解引用」（第 10 段），影子树未动 ⇒ 两棵树当时差的就是那几个 hunk（`diff` 逐文件核过，除我的 hunk 外零差异，也就是影子那份就是 B1）。这对本段 42 格的**判定**没有影响（没有一刀把这四个函数变成 `(nil, nil)`，见第 10 段末的可达性核对），但"最终字节"仍欠一次复跑，记在第 10 段末与 §23.15 收尾的未覆盖清单里。**⇒ 这一句已结（同日第 10 段末）：42 格按最终字节复跑完，全 `KILLED`、五个「无」，且加固后的判空在取证趟里被证明是"有牙的"（同一刀从崩整包变成本腿红）。**

**10 · 用例侧的「一条注码崩掉整包」窗口：13 处判空、一枚编译不过的刀，与一次"上界=下界"的机制证明（产码零改动）。** 第 4／5 段把 M75／M94／M95 三格的上界判不了归因到"产码 panic 带走测试二进制、`testing` 再替在跑的那条腿补印一行 `--- FAIL`"。这一段的对象是**同一件事的用例侧版本**：用例自己不解引用前先判空，任何一枚把指针返回变成 `nil` 的刀都会替整包交一份残缺答卷——不是本批漏判，是**下一批**会踩。所以这一段做三件事：找出这类点、堵掉、并且**证明堵的方式真的有效**（不然是白改）。

*两类扫描，只有窄口径能用。* 宽口径（"任何 err 检查之后的解引用"）在三个电池包里报 **877 处**，按文件铺开是 `short_link_test.go` 45／`quote_test.go` 30／各渠道 card 测试 20 上下／**本批自己的** `obs_config_default_batchm_test.go` 14——绝大多数是 gorm 值结构体返回（`First(&x)` 不返回指针），那是**全仓约定**不是危险，所以这轮**撤回宽口径**、不进清单。窄口径按形状取：`if err != nil || <var>.<Field>`（`||` 只短路条件、护不住函数体），全包 **13 处候选**，返回类型逐个核后，三个电池包内 4 个文件 8 处为真：

| 文件 | 被调 → 返回类型 | 原形状 |
| --- | --- | --- |
| `internal/repository/obs_config_default_batchm_test.go` | `GetDefault` → `*model.ObsConfig` | 条件里 `got.ID`（当时的第 1 处） |
| `internal/service/approval_request_test.go` | `Submit` → `*model.ApprovalRequest` | 条件里 `row2.ExpiresAt`；同腿另有三处裸 `row.` |
| `internal/service/web_chat_e2e_test.go` | `SendMessage` → `*VisitorSendMessageResult` | 条件里 `s1.AIReplied`、体内 `s1.AIResponse.Content`（嵌套指针）——同文件 191 行早已是 `open == nil \|\| open.Session == nil` 的正确形状 ⇒ 作者知道，只是没写全 |
| `internal/aiagent/agent/lifecycle/lifecycle_test.go` | `Run` → `*LifecycleResult` | 47／51 两处；只插 `res == nil \|\|`，判据与文案一字未动。这个形状不是本轮新造的口径，HEAD 里就有现成先例：`approval_request_test.go` 的 `if err != nil \|\| back == nil`（HEAD:373）、`web_chat_e2e_test.go` 的 `open == nil \|\| open.Session == nil`（HEAD:191，`git show HEAD:` 对过） |

**5 处假阳否证（连同"为什么不是窗口"一起记，下一轮别再重扫）：** `tooluse/decorator_approval_gate_test.go:97,209`、`tooluse/p2_test.go:481`、`tooluse/risk_gate_test.go:178` ⇒ `runGate`／`decorated`／`handler` 返回 `(ToolResult, error)`，`ToolResult` 是值结构体（`tooluse/tool.go:59`）；`browser_automation/service/d7_gate_b20_test.go:325` ⇒ `SessionService.Confirm` 返回 `(ConfirmResult, error)` 值（`session.go:162`、类型在 `:119`）；`service/human_task_test.go:815` ⇒ 正则伪捕，条件里 `stale == nil` 已在 `stale.Status` 之前，被当成变量的 `model` 是包名；`service/quote_test.go:395,427` ⇒ `ActiveQuoteScript` 返回 `(QuoteScript, error)` 值（`quote.go:879`）。**1 处半否**：`service/asset_bundle_test.go:1035` 的 `enabledList[0]` 有 `len(enabledList) != 1 ||` 短路护着，但候选变量 `b` 的赋值落在 12 行窗口外、未定 ⇒ 与 `config_param_guard_test.go:78`（`info` 是 `filepath.Walk` 回调入参，stdlib 契约 `err==nil ⇒ 非 nil`，且下一行自己写了 `info != nil &&`）一起进下面的交回清单。

**窄口径也漏了最大的一块，是取证趟自己照出来的。** 正则要的是"判空与解引用在同一行"；而 `if err != nil { t.Fatalf(...) }` **紧跟着**裸解引用这个相邻两行形状它抓不到。于是第一版"改完了"的 repository 文件其实只补了 6 处的 1 处。跑机制证明（下面 A 相）时，同一枚刀在这条腿上先崩，才把同文件另外 3 处（`got2.ID`／`after.ID`／`snapshot.ID`）与双默认那条腿的 `got.ID` 全照出来，外加**旧文件** `obs_config_test.go:319` 的 `result.Name`（旧腿，`err` 用 `Errorf` 不中止，后面照单解引用）。⇒ 口径：**这类点必须按"被调函数的返回类型"逐个枚举措名，不能按正则行形状收口**；枚举行数的正确办法是"同一个刀 + 加固前后各跑一趟，看红腿名单长度"（见下表 B 相的 6 条）。

**取证驱动 `/tmp/bm7_deref_proof.py`（影子树，三相，结果行不过 `tail`/`head`）。** 刀打在产码 `GetDefault` 的 return 上，锚点取 `First(&config).Error` + 下一行两行联合（`return &config, err` 全文件出现 2 次，`GetByID` 那次与 `First` 同行 ⇒ 命中数断言 =1 才动手）。**第一版刀写的是 `return nil, nil`，它连编译都过不了**：`obs_config.go:144:2: declared and not used: err` ⇒ A／B 两相各 1s／0s、日志 5 行、`交卷=0`、`panic 行=无`——看着像"没崩"，其实是**什么都没测**（`run_pkg` 从此内置 `build failed` ⇒ `BUILD-BROKEN ⇒ 停`，不再往下跑）。换成 **`return &config, err` → `return nil, err`**（只摘 `&`，`err` 仍在用 ⇒ 可编译；成功路径 `err==nil` ⇒ 交给调用方的正是 `(nil, nil)`；报错路径行为不变）之后才有测量：

| 相 | 用例字节 | 读数 | 判读 |
| --- | --- | --- | --- |
| A 加固**前** | `obs_config_default_batchm_test.go` `960d0aa9`（6 处里只补了 1 处）＋ `obs_config_test.go` `ab43b51d`（未补） | `rc=1 交卷=496(PASS 495/FAIL 1) 69s`，`panic: runtime error: invalid memory address or nil pointer dereference [recovered, repanicked]`，唯一 FAIL = `TestBatchM_GetDefaultRequiresActiveStatus` | **崩整包**：一枚刀吃掉 **301 条腿**不交卷，而那"1 条 FAIL"是 panic 替在跑的腿补印的假信号 |
| B 加固**后**（同一刀） | 终态 `7d8fca16` ＋ `91fe8a1d` | `rc=1 交卷=797/797(PASS 791/FAIL 6) 78s`，`panic 行=无`，红的腿 6 条：`TestBatchM_GetDefaultRequiresActiveStatus`／`SetDefaultRejectsMissingIDAndKeepsPrevious`／`SetDefaultLeavesExactlyOneDefault`／`IncrementUsageDoesNotTouchSelectionColumns`／`GetDefaultPicksOldestWhenDuplicated`／`TestObsConfigRepository_GetDefault` | 机制成立：**红腿名单与判空处数 1∶1**（repository 侧 5＋1＝6），整包照常跑满 ⇒ 上界可判 |
| C 惰性（无注码） | 同上终态 | repository `rc=0 交卷=797/797 89s`；lifecycle `rc=0 交卷=1 2s`；service 取首趟同脚本 `rc=0 交卷=3567(=3564 PASS＋3 SKIP) 423s` | 判据没动、一条腿的状态都没变 ⇒ 加固是惰性的。service 那一趟的字节与本轮**逐字相同**（`d72e4538`／`11c404af` 两趟 md5 一致，本轮只动 repository 侧两份 ⇒ 复跑趟用 `SKIP_SERVICE_BASELINE=1` 明确跳过并在日志里写明理由） |

跑后产码三族 md5 逐字回基线（`75a95049…`／`9cfc4c1f…`／`86756237…`）。两处读数要留在账上：① 复跑趟末行仍打 `完成 rc=1`，逐项行显示唯一为假的是**跳过位的占数**（占位写 3564、真实上界口径是 3567），三处实质判据 `A 崩=True／B 不崩=True B 跑满=797 B 红腿数=6／产码回基线=True` 全真——脚本已把占数改成 3567；② **A 相是一次性的**：B 相的 `shutil.copy2` 把影子覆盖成终态字节，"加固前"此后不可复现（除非反向摘掉这 6 处判空），所以两相的字节都按 md5 记进 `/tmp/bm7_deref_proof2.log`（首趟留在 `/tmp/bm7_deref_proof_run1.log` 与同名目录）。

**改判据强度的一处，必须自己说出来。** `approval_request_test.go` 拆 `row2.ExpiresAt` 那个复合条件时，`err != nil` 分支从 `Errorf`（记下继续）变成 `Fatalf`（当场止）——**期望值一字未改**，但这条腿在变异下的行为变了（更早停）。同类：`web_chat_e2e_test.go` 的 `s1`／`s2` 拆完后，原先由后续断言顺带覆盖的"nil 结果"会先在本腿止。⇒ 这两处属"判据强度上调"，不是判据内容变更；若日后有人比对该腿的红因，先想到这里。

**登记不修（2 处，同一函数）〔本条已被下面第 12 段推翻：两处都修了，且这里给的"修法形状"经核不可采用，理由写在第 12 段第 2 条。原文照留不删，是为了让"上一轮以为不用修"这个判断本身留在账上〕** `obs_config_default_batchm_test.go:116`、`:126` 的 `if err == nil { t.Errorf("… %s …", got.Name, got.Status) }`——解引用在 **`err == nil` 分支内**，本轮这枚刀（`return nil, err`）在这些腿上给的是真 `ErrRecordNotFound` ⇒ 走不到；要走到得再来一枚"把 err 也抹成 nil"的刀。若要修，形状是**把两个字段收进单参数 `%+v`**（`fmt` 对 nil 指针印 `<nil>`、不解引用），而不是再叠一层判空。另：**指针返回的 repository `GetByID` 家族**（`alert_rule.go:55`、`telegram_gate.go:45`、`knowledge_base.go:43` …）在三电池包之外，877 的宽口径池子里那一层本轮**没扫**，记为未覆盖面。

**交回清单（3 个文件，本轮一律不动）：** `internal/service/quote_test.go`（未跟踪，他泳道新文件；其中 **:1066 是真窗口**——`ViewAt` 返回 `*QuoteView`（`quote.go:416`），条件里 `back.ID` 先解引用）、`internal/service/asset_bundle_test.go`（未暂存 ` M`，压着他会话改动）、`internal/service/config_param_guard_test.go`（同样未暂存 ` M`，同上）。三条状态本轮按 `git status --porcelain` 逐字复取过（先前笔记把 `asset_bundle_test.go` 记成"已暂存"，不成立）。

**最终字节的过滤口径复跑（把第 9 段末欠的那一次结掉）。** 逐文件 md5 预证两棵树同步（5 份用例 ＋ 2 份产码全部"同"）后跑 `/tmp/bm7_final_rs.sh`：两族各先 `--check`（预检 `rc=0`、`ANCHOR-BAD`/`KILLER-BAD` 行数 0），再整族复跑。`repository` 18 格 07:04:43→07:06:47（124s，分母 12 条腿，单格 5–10s）、`service` 24 格 07:06:48→07:10:59（251s，分母 10 条腿，单格 8–19s），**42 格全 `KILLED`、`killer_died=True`、`md5_restored=True`**，两行收尾都是"未杀格 = 无，未跑满的格 = 无，等价格误红 = 无，被计时截断的格 = 无，收尾残留 = 无"。load 12.04–33.08（并行泳道在同机编译），磁盘可用 52,404,052 KB。**顺带一条新的树漂移**：跑完后 `internal/pkg/db/migrate.go` 活树已是 `3161838c…`（并行泳道在改第 5 个文件），影子仍是批M-7 的 `86756237…` ⇒ db 族那 51 格结论的**适用对象是影子基线，不是活树当前字节**，今天没有复跑（该文件禁改，归他们）。

**11 · 本后续（§23.15）未覆盖面（不得当作已验证）。**
① §23.14 第 9 段第 ① 条原样带过来：真实库里那三枚索引今天仍不存在，本后续没有推进（代价也一样——要真启动一次携带未提交改动的 API 进程）。
② ~~repository／service 两族的红集合上界是读出来的~~ ⇒ **本后续第 9 段已跑完（42 格上界=下界）、第 10 段末又按最终字节复跑一次** ⇒ 从清单划掉。db 族的 2 格（M75／M95）仍是"只有下界"，产码 panic 那一条不在此列可修范围。
③ §23.14 第 9 段第 ③④⑥ 条（合成现场、两条无腿 panic 分支、等价格 M61/M63 的 `gorm v1.30.0` + `driver/postgres v1.6.0` 绑定）本后续一律未动、未新增。
④ **两棵树的产码不是同一份，今天多一条实证**：`migrate.go` 活树 `3161838c…` vs 影子 `86756237…` ⇒ db 族 51 格的对象是影子。对照面：本后续的 repository／service 42 格与第 10 段的取证趟，两棵树在 8 份文件上 md5 **逐字相同**（跑前后各取一次）⇒ 这两族没有这个缺口。
⑤ **判空清单只收到一行式形状**。窄口径正则取的是 `if err != nil || x.Field`；"`if err != nil { t.Fatalf }` 紧跟着裸解引用"的两行式它抓不到（第 10 段里 repository 侧 6 处有 5 处是这种，靠机制证明趟在自己包里照出来）。**包外那一层没有等价的趟** ⇒ 指针返回的 `GetByID` 家族（`alert_rule.go:55`、`telegram_gate.go:45`、`knowledge_base.go:43` 等）在三电池包之外的用例里何时解引用、判没判空，本轮**没扫**，不得当作已清。
⑥ **A 相是一次性的**：`shutil.copy2` 覆盖了影子，"加固前"字节（`960d0aa9`／`ab43b51d`）此后不可复现，除非按第 10 段名单反向摘掉那 6 处判空。⇒ 这组对照不能靠"再跑一遍"复核，只能靠留在 `/tmp/bm7_deref_proof2.log` 里的读数。
⑦ **两处判据强度上调没有电池覆盖**（approval 的 `err` 分支 `Errorf`→`Fatalf`；web_chat 的 `s1`／`s2` 拆完更早止）：这两条腿不是 93 格里任何一格的指定腿，所以两族复跑的读数证明不了"强度变了但判据没变"这一句——它现在只是**我写的说法**，加腿或改腿时才需要被证。
⑧ **service 惰性基线是跨趟引用的**：复跑趟用 `SKIP_SERVICE_BASELINE=1` 跳过了 service 整包，用的是首趟同脚本的 `rc=0 交卷=3567 423s`。支撑是"两份 service 用例字节两趟 md5 一致"（`d72e4538`／`11c404af`），不是同一趟里的直接读数。
⑨ **S26 那条单腿用时已重新取档，并顺手给"负载主导"再添一档**：先前那个 `--- PASS (30.27s) ok 30.869s` 的原件日志被后续趟就地覆盖（产出它的驱动第一句就是截断自己），**它从未进过本文档任何预算口径** ⇒ 本轮既不追认它、也不用它替换任何已登记数。改按活树今日字节重跑一趟留档 `/tmp/bm7_s26_live.log` ⇒ `--- PASS (0.31s)`、`ok 3.695s`，`migrate.go` md5 起跑与收尾同为 `3161838c…`（跑动途中没被并行泳道换掉）。⇒ **同一判据的两档相差 100 倍 ⇒ 这条腿的用时是负载主导的**（与 §23.14 第 8 段第 10 条"四档 46–121s、负载主导"同一件事的第四个样本，只是那一份丢掉的原件这次补上了新读数）。这一趟是 `-run` 单腿子集，**只是取一个用时读数，不构成门禁**（门禁口径见 §23.13）。

---

### §23.15 第 12 段（批M-8：把 `GetByID`／`GetDefault` 那扇窗按"被调函数"整族收口，产码零改动）

**1 · 来路与范围。** 第 10 段末登记的两件事一起结：① 「登记不修」的 `:116`／`:126`；② 第 11 段第 ⑤ 条里"本泳道自己两份文件"的那一半。**枚举方式是按被调函数**（`obsConfigRepo.GetByID`、`obsConfigRepo.GetDefault` 各自 grep 出全部调用点），不是按正则行形状——这条口径正是第 10 段用一趟崩包换来的。两文件里这两个被调函数共 **25 个调用点**（`obs_config_test.go` 9、batchm 16，逐条打行号核过），其中 **6 处上一轮已带判空**（batchm 的 `got2`／`after`／`got`／`snapshot`／双默认那条腿的 `got`，加旧文件 `:321` 的 `result`），**3 处读码判定不是窗口**（`obs_config_test.go:295`、`:466` 与 batchm `:598`：返回值丢进 `_` 或根本不用，只看 `err`），本轮补 **16 处 / 15 个守卫块**（旧文件 5 块覆盖 6 个点，其中 `config1Updated2`＋`config2Updated` 同起一次；batchm 8 块 + 原"登记不修"的 2 处 `err == nil` 分支改造）。算术：3 + 6 + 16 = 25 ✓。改动面：`obs_config_test.go` 455→470 行（+15／−0）、`obs_config_default_batchm_test.go` 573→607 行（+37／−3），终态 md5 `b9806015894c072e2eec87c17dfa9a0b`／`c1f11142975994d613f083a4bd684e5c`。**产码一字未动**（`internal/repository/obs_config.go` 起收同为 `75a9504911bb1650070828024a8fdaec`），`gofmt -l` 空、`go vet ./internal/repository/` rc=0 零输出。

**2 · 上一轮给的"修法形状"经核不可采用，改判据文案前先说清。** 第 10 段写的是"把两个字段收进单参数 `%+v`"。`fmt` 对 nil 指针确实印 `<nil>` 且不解引用，**但 `model.ObsConfig` 里有 `AccessKey`／`SecretKey` 两列**——`%+v` 在**非 nil** 的那一支会把它们原样打进测试输出，等于把凭证面写进日志（这条形状在 §23.7 的密钥面口径里是明令不进输出的）。所以本轮按同一文件里已有的形状收口：`err == nil` 且返回 nil 时给一条**独立分支**的红（`…却给了 (nil, nil) —— 期望 record not found`），真行为（返回了一台停用/非 active 的存储）走**原文案、原判据**，`t.Errorf` 不升级为 `Fatalf`（这一支不是前置断言，判"该报错却没报错"本就该继续把后面的 UpdateStatus 腿跑完）。⇒ 口径：**照抄"更省一层判空"的修法前，先核那个 struct 有哪些列**。

**3 · 机制证明（两把刀 × 两相，影子树，驱动 `/tmp/bm8_ab.py`，结果行不过 `tail`/`head`）。** 刀形都取"可编译的最小语义破坏"（第 10 段那条 `return nil, nil` 编译不过的教训沿用）：

| 刀 | 注码 | A 相（加固前字节 `91fe8a1d`＋`7d8fca16`） | B 相（加固后字节 `b9806015`＋`c1f11142`） |
| --- | --- | --- | --- |
| **K1** | `GetByID` 的 `return &config, err` → `return nil, err`（成功路径交 `(nil, nil)`，报错路径行为不变） | `rc=1 交卷=4/22 PASS=3 FAIL=1`，`panic: … nil pointer dereference [recovered, repanicked]`，**18 条腿从未运行** | `rc=1 交卷=22/22 PASS=14 FAIL=9`（原始 FAIL 行 10：`TestObsConfigRepository_GetByID` 一名同时落在 PASS 与 FAIL 两侧——它的 `get_non-existing_config` 子档绿、`get_existing_config` 子档红，去重后父名重复计入），`panic 行=无`，红腿恰好 9 条 = **含 `GetByID` 判空的腿数**（batchm 5：`IncrementUsageDoesNotTouchSelectionColumns`／`…UpdatedAt`／`UpdateDoesNotResurrectSelectionColumns`／`UpdateSucceedsWithSingleDefaultGuard`／`UpdateMissingIDIsError`；旧文件 4：`GetByID`／`SetDefault`／`Update`／`UpdateStatus`） |
| **K2** | `GetDefault` 的 not-found 转成 `(nil, nil)`（"吞掉哨兵错当成功"的真实重构形状） | `rc=1 交卷=1/22 FAIL=1`，同样 panic | `rc=1 交卷=22/22 PASS=21 FAIL=1`，红腿 = `TestBatchM_GetDefaultRequiresActiveStatus` 一条（两处 `err == nil` 分支都在这一条腿里），无 EXTRA、无漏 |

**A 相那一条 `--- FAIL` 是崩溃替在跑的腿印的**（[[feedback-mutation-battery-hygiene]] 第 34 条），且 K1/A 里那次红**正是崩的那条腿自己** ⇒ "红腿归因正确"也救不了：缺了 `-list` 分母，18 条没跑的腿在两种判据下都不留痕。**两把刀各自的上界都是精确的**（9/9、1/1），这才有"每一处判空都承重、且没有多余判空"的读数。K1/B 的红腿集合还顺带证明：`GetByID_NotFound`、`Delete` 两条**只读 `err`** 的腿没被误伤 ⇒ 判空没把判据放宽。

**4 · 最终字节的电池复跑与惰性门。** repository 族 **18 格全 `KILLED`**（07:37→07:39，单格 3–8s，分母 `go test -list '^TestBatchM_'` = 12 条腿），收尾行「未杀格 = 无，未跑满的格 = 无，等价格误红 = 无，被计时截断的格 = 无，收尾残留 = 无」，`md5_restored=True` 逐格真。惰性门（无注码、整包无过滤）：**活树** `go test -count=1 -v ./internal/repository/` ⇒ `rc=0 顶层=817 PASS=817 FAIL=0 SKIP=0 83.755s`（原始 PASS 行含子 1202）。

**5 · 一条必须留在账上的跨树读数差。** 第 10 段登记的 repository 无过滤上界是 **797**，本轮活树读到 **817**。逐文件核过、差额精确闭合：影子缺 4 份**并行泳道**的文件（`email_drain_test.go` 4 条 + `help_center_public_list_test.go` 1 条 + `message_hub_outbound_push_cap_b20d_test.go` 5 条 + `quote_test.go` 12 条 = +22），而 `email_send_test.go` 活树已删掉影子还在的 2 条（`TestEmailSendRepository_GetPendingEmails`／`_EmptyResult`）⇒ 797 + 22 − 2 = 817，无未解释残差。⇒ 口径照旧（第 25 条）：**两树不同数不判红，但必须逐项闭合后再报**；本轮 K1/K2 与 18 格电池的**对象是影子字节**（用例两树逐文件 md5 相同：`b9806015`／`c1f11142`，产码同为 `75a95049` ⇒ 这两族无适用性缺口），惰性门那一趟的**对象是活树**。

**6 · 本段仍未覆盖面（不得当作已清）。** ① 第 11 段第 ⑤ 条的**另一半**没动：其它指针返回的 repository（`alert_rule.go:55`、`telegram_gate.go:45`、`knowledge_base.go:43` …）在其用例里的解引用点，本轮只按"本泳道两份文件"收口，包外那一层仍未扫，宽口径 877 池子依旧是噪声不是清单。② 三处"只读 `err`"的调用点是**读码判定**（`_` 丢弃），不是跑出来的判空；它们由 K1/B 的红腿集合反向证了"没被误伤"，但"永远不需要判空"这句只在产码保持 `return &config, err` 的前提下成立。③ 取证驱动自己交过一次学费：为省屏幕把每相的 `用例字节 md5` 行 `grep -v` 掉了，于是第一趟 K1/A 其实是**新字节**在跑、被记成了 A 相（读数与 B 相逐字相同才暴露）。正解不是"更小心"，而是**身份行（哪一相／哪份字节）属结果行，一律不许过滤**——与第 38 条"结果行不许过 tail"同一族，只是这次吞掉的不是结论、是结论的**归属**。


---

### 23.16 批M-9：十九批用例随产码入库（`1af6d28c`），顺带量出「密钥门的绿是推送窗口的绿、不是历史的绿」

**来路。** 批A→批M-8 的用例面（含 §23.10～§23.15 那六批 obs_config / message_hub / opportunities 的补腿）自写出起**只活在共享工作树里**：产码那半边早在前几轮已随各自批次提交，用例这半边一直未 `git add`。本段记的是"入库"这一刀本身的取证，不新增被测行为。

**1 · 必须按 hunk 切的一处，以及为什么整文件暂存必定编译不过。** 246 个待提交项里 108 项属本泳道，唯一一份**同时压着两条泳道改动**的产码文件是 `user-server/internal/pkg/db/migrate.go`：本泳道要提交 `verifyUniqueIndex`（N-36：`CREATE UNIQUE INDEX IF NOT EXISTS` 在**同名非唯一索引已存在**时是静默 no-op，故形状必须读 `pg_index.indisunique` 而非"名字在不在"）、`postMigrateObsDefaultUniqueIndex`、以及 `postMigrateMessageHubUniqueIndex` 里那条形状核验分支；并行泳道在同一文件的**相邻 hunk** 往 `allModels()` 里加了 `BrowserAuditDigest`／`BrowserAuditPruneRun`／`BrowserWriteClaim` 三个模型，而这三个类型定义在**未跟踪文件**里 ⇒ 整文件暂存＝把"引用未定义类型"提交进仓，`go build ./...` 必红。做法：`git diff -U3` 取补丁 → `awk '/^@@/{h++} h>=2'` 丢掉对方那一 hunk → `git apply --cached --check` 通过后 `--cached`（工作树不动，对方那几行留在未暂存区）。索引面在提交前用 `git diff --cached --stat` 复核为"恰好本泳道 109 项"（108 + 切过的 `migrate.go`）。

**2 · 提交前/后的核验链（每一步都真跑，非"应该没问题"）。** `check-secrets-workspace.sh` rc=0 → `go build ./...`＋`go vet ./...` rc=0 → 提交前用 `git write-tree`/`git commit-tree` 造出**未落分支的候选树**、克隆该树跑 `pkg/db`＋`repository`＋`channelbot`＋`model` 四门（rc=0，118.280s / 293.915s / 另两门）→ `TREE=$(git write-tree)` 与提交后 `--shared` 克隆各自复验自洽。提交：`1af6d28c`，109 文件、+23754 / −1228。

**3 · 一条不算"结论"但会吞掉结论的形状：证据文件 0 字节。** 首轮把 controller＋service 两门串在同一个后台任务里、各自重定向到独立 log。回执报"完成、exit 0"，而 `…_svc.log` 与任务自身的 output 文件**都是 0 字节**，同一时刻另一条命令的截图里却出现了 `FAIL hivemtk-user/internal/service 392.877s` + `svc rc=1` —— 那行属于**并行泳道就地改写的同名热文件**（[[feedback-mutation-battery-hygiene]] 的"热文件就地摘装"）。⇒ 两条口径进账：① 后台任务的 exit 0 不是产物存在的证明，判"跑完了"要认**文件非空 + 自己写的末行标记**；② 一次门禁读数的身份由**文件名独占性**保证，复用他人前缀（`b4g3_*`）等于没有读数。重跑改用唯一名 `…_svc_run2.log` 并在末尾追加 `RUN2-MARKER-END`。

**4 · 密钥门：本轮 push 窗口实测 2 处命中，都在本批新文件里。** CI 的 `security-scans` job 用 `gitleaks/gitleaks-action@v2` + `fetch-depth: 0`，**阻断**；工作流注释里预先写死了处置口径——「若未来误报测试夹具，用仓库根 `.gitleaks.toml` allowlist 精确豁免，而不是回退本门禁」。本轮就是那个"未来"：在 `1af6d28c` 的克隆里按推送窗口跑 `gitleaks detect --log-opts="HEAD~3..HEAD"`（3 commits scanned）⇒ `rc=1 leaks found: 2`，两条都是 `generic-api-key`，都是本批新写的夹具：`webhook_batchf4_msgtype_test.go:459` 的假飞书 `file_key`、`webhook_batchg2b_douyin_media_test.go:1190` 的假抖音 app_key 常量。二者均不参与任何鉴权（前者只断言 `sticker`→`[表情]` 的映射，后者只当缓存 map 的键，与 `tt_g2b_margin_long` 成对区分"够长→缓存／短于提前量→不缓存"）。处置=按**精确串值**加两条 allowlist（不按路径、不按规则；同文件出现任何其它 key-shaped 串照旧命中）。

**5 · 豁免的四格反向测试（缺任一格都不能写"门有牙"）。**

| 格 | 命令 | 读数 |
| --- | --- | --- |
| 新配置 × 真实窗口 | `detect --log-opts="HEAD~3..HEAD"` | `rc=0 no leaks found` |
| 旧配置（=新配置减去那两条）× 同窗口 | 同上 | `rc=1 leaks found: 2`，逐条同名 ⇒ 豁免**承重** |
| 隔离仓 canary × 新配置 | `detect --log-opts=-1`（一份 4 常量的 Go 文件） | `rc=1 leaks found: 1`：只报 `github-pat` 的 `ghp_…`，两条豁免串被吃掉 ⇒ **半径未扩大**；`AKIAIOSFODNN7EXAMPLE` 不报，是 gitleaks 内置豁免，故它不能当探针（本文件头注释原话） |
| 同一 canary × 摘掉配置 | 同上 | `leaks found: 3`：两条夹具各计一次 ⇒ 证明扫描**真读到了**这些字节，不是"没扫所以绿" |

版本口径照实记：本机 `gitleaks` 为源码构建，`--version` 只印 `version is set by build process`，与 `.gitleaks.toml` 注释里那次实测的 v8.24.3 **不保证同版** ⇒ 上表读数只对本机这一版成立，CI 那一版仍需以推送后的 run 日志为准。

**6 · 历史面：登记，不在本批处置。** 同一克隆不带 `--log-opts`（整史）跑 ⇒ `1885` 处命中 / `38` 个文件，其中 `1825` 处集中在一份合成数据 `user-server/scripts/simulate/interactions.jsonl`；余下与本泳道无关的已知点含 `internal/bridge/bridge_helpers_test.go` 的 2 处 `jwt`（jwt.io 文档示例 token）＋1 处 `generic-api-key`、`internal/pkg/mail/unsubscribe_test.go` 的 1 处。⇒ 两条**互不抵消**的结论要一起说：推送窗口的绿**不**等于历史扫描的绿（本批 2 处就是被窗口口径放过、被克隆复跑抓出来的）；反过来历史扫描的红也**不**等于存在泄露——1825/1885 是合成会话 JSON 撞 `generic-api-key` 的形状匹配。真凭据面（F1：8232 口令曾进历史）由用户拍板**暂不处置**，本批不动 `.env`、不轮换、不改写历史。

**7 · 真机库的形状核验：三条守卫的索引，库里实测只有一条在。** 读 `127.0.0.1:8232/user_db` 的 `pg_indexes`：`uni_message_hub_platform_msg_conv` **在**（unique、非 partial），`idx_obs_config_single_default`、`idx_opportunities_clue_id` **不在**。根因不是钩子写错，是**启动路径没在新字节上跑过**——本泳道不重启在跑的 user-server，`AutoMigrate` 之后的那几个 post-migrate 钩子自然没执行。⇒ §23.11/§23.13 那两条"真实启动路径"的腿证的是**代码形状**，不是**这台库的形状**。本轮**不擅自建**这两个索引：① 8232 是并行泳道共用的开发库，partial unique 一旦落下会把"无索引"这一形状从此测不到（`pkg/db` 的 R6 腿正要求**无索引**的前置）；② 建索引属部署动作，该由重启服务的部署腿自然带出。口径进账：**凡以"库里有这个索引"为前提的判断，必须先数 `pg_indexes`**（[[feedback-verify-by-running]]）。

**8 · §23.15 未覆盖面 ① 的分母终于量出来了（同包解析，不是名字相接）。** ⚠ 本条那组数（250 处／61 文件）**已由 §23.17 第 2 段作废**——"同包"仍不够，包内**跨类型同名**一样误伤；保留原文只为记下口径是怎么一步步收错的。三组数一起记：产码里存在裸 `return nil, nil` 的函数 **229** 个（站点 **320**）；测试里"未判空即解引用"的候选站点 **710**；按**同包**解析后（调用与被调在同一 package 才可能命中）落到 **250** 处活站点、分布在 **61** 个测试文件（最密：`internal/service/inbox_test.go` 62、`repository/integration_test.go` 14、`customer_session_test.go` 11、`opportunity_test.go` 10、`unified_message_test.go` 9、`wecom_test.go` 8、`webhook_channel_whatsapp_status_test.go` 7）。跨包那一层未并进来是**故意**的：按裸方法名相接得到 310，属假阳膨胀（同名跨包不相干），宁缺不混。⇒ 这 250 处是**候选清单**、不是**缺陷清单**——多数用例的夹具本就走非 nil 路径；真要清，形状应是"门 + 基线"（照 `check-env-coverage.py`/`env-coverage.baseline` 的先例，只锁"新增不许抬高"），而不是批量改测试。本批未做，见下条。

**9 · 顺带复测的两件小事。** ① markdownlint（`markdownlint-cli2 v0.23.3 / markdownlint v0.41.1`，读仓库根 `.markdownlint.json` 与 `.markdownlint-cli2.jsonc`）在 `1af6d28c` 上跑：**Linting 170 files，0 issues in 0 files**——§23.14 登记的那条 `CHANNEL_INTEGRATION_AUDIT_2026-09.md:797:3 MD004` 已不在当前字节里，故本条以"复跑为绿"结，不改判据。② N-27／N-28／N-29 三处登记项回读：符号仍存在、消费方仍为零（`grep` 逐条命中数＝仅定义处），维持 §5 的"待产品口径"，本批不修。

**10 · 本段未覆盖面（不得当作已清）。** ① 第 8 条那 250 处只到"分母"，门与基线未落地；跨包解引用面仍未解析。⇒ **本条由 §23.17 结掉一半**：门与基线已落地，但那 250 这个数**作废**（同包按方法名相接仍会跨类型误伤，见 §23.17 第 2 段），改口径后的数在 §23.17。② `gitleaks` 那四格反向测的是**本机版本 + 克隆里的候选树**，CI 上 v8.24.3 的读数要等推送后的 run 日志回填。③ ~~service 整包门在 `1af6d28c` 上本轮只有一次"0 字节读数"~~ ⇒ **四趟读数已齐，且结论不是"绿"**：见 §23.17 第 4 段的归因（红在 `TestD12_NoNewLegacyKVDirectQuery`，与解引用门无关）。④ `-race` 下 `TestCreateSession_AnonymousUser` 的全局 DB 句柄项（[[project-platform-test-global-db-handle-leak]]）只定位到文件行号，未复跑判据。⑤ 第 7 条那两个索引在**任何真实部署库**里在不在，本批无证据。

---

### 23.17 批M-10：把"未判空即解引用"换成了跑码的门，顺带照出两道"门的绿不在我这边"（2026-09-22）

**来路。** §23.16 第 10 条第 ① 项欠的是"门 + 基线"这一件工具，第 ③ 项欠的是 service 整包门的读数。本段两件一起结，另加一条本段没打算碰、却被照出来的既有红。

**1 · 门的形状（`scripts/check-test-nil-deref.py` + `scripts/test-nil-deref.baseline`）。** 判据三条：某测试文件的**确证站点**（被调函数按 `类型.方法名` 解析成功那一档）超过基线数额、或文件不在基线里却出现站点 ⇒ `NEW`/`OVER` 红；基线登记的文件现算变小 ⇒ `STALE` 红（修掉就得划掉，不许留旧数额养僵尸条目）；基线行格式坏/同文件重复登记 ⇒ 红。另加一条**不属于**判据但决定这道门死活的前置：**基线文件不存在 ⇒ rc=2 ENV-BROKEN**，而不是"没基线＝没可比＝全绿"。这条是照 `audit-artifacts`（缺产物退 0＝SKIP 不是 PASS）那条老账加的——少了它，误删或漏提交基线会把门变成空转。基线当前是**零条目**（只有表头注释），因为第 6 段那 8 处本轮全部就地收口了。

**2 · 分母是四轮收紧收出来的，每一轮都靠一条被证伪的假阳。** 产码函数键 ／ 确证站点。**判据标签不靠回忆**：收尾时把三档判据各做成一个变体脚本（`/tmp/bm10_variant/scripts/v_none|v_any|v_cur.py`，只改 `first_result_is_pointer` 那一句），在当前活树上重跑，读数与当年日志逐档对上（下表的"复现"列）。

| 轮 | 判据 | 产码函数键 | 确证站点 | 当年日志 ⇒ 收尾复现 | 被证伪的样本 |
| --- | --- | --- | --- | --- | --- |
| 一 | 测试里的裸方法名 ∩ 产码里的裸函数名（同包） | 241 | **119**（13 文件） | `bm9_deref_gate_run1.log` | `short_link_test.go` 一口气中 26 处：产码里带裸 `return nil, nil` 的 `Create` 只有 `HTTPAfterSaleClient` 一个，而那 26 处的接收者是 `*ShortLinkService` ⇒ 名字相接在大包里等于噪声生成器 |
| 二 | 键改成 `类型.方法名` 相接，结果表**不看形态** | 268 | **43**（21 文件） | `run2.log` ⇒ `v_none` 今天读 **268／35（17 文件）**，差额正好是本泳道收口的 8 处／4 个文件 | nil 切片／nil 映射全被算进来：`rerank_advanced_test.go` 8 处、`dialogue_memory_test.go` 5 处、`memory_system_test.go` 4 处——`range`／`len()` 安全，用例踩的是 `x[0]` 越界，判据该写 `len(x)==0` |
| 三 | 要求结果表**任意位置**含 `*` | 153 | **9**（5 文件） | `run3.log`（153／9）、`run4.log`（153／5，QQ 那 4 处已收口后）⇒ `v_any` 今天读 **153／1** | `knowledge_document_test.go:149`：`MatchByAgent` 交回 `[]*KnowledgeDocument`，用例只用 `len(got)`／`got[0]` ⇒ `[]*T` 仍被"含 `*`"放过 |
| 四 | 只认**首项是裸指针**（`[]`／`map[`／`chan` 前缀一律排除） | **114**（活树）／112（隔离克隆） | **8**（HEAD 字节）→ 0 | `bm10_teeth_run5.log`（114／8／4 文件）⇒ `v_cur` 今天读 **114／0** | 四档之间**没有再出现新假阳**，第 6 段那 8 处逐条读源码确认为真 |

⇒ §23.16 第 8 条那组"250 处／61 文件"随最终判据一起**作废**（在那儿只否证了跨包，没否证包内跨类型同名）。第四轮的 8 处即第 6 段的名单，收口后现存 **0**（活树与改名克隆 `/tmp/bm9_mine` 各跑一次：`rc=0`、站点 0、两树产码函数键 114／112 的差额来自并行泳道在飞文件 ⇒ 两树不同数不判红，但必须逐项闭合后再报）。顺带把脚本判据注释里的两处错标签改掉：先前它写"第一轮…得到 43 处，收紧到首项是裸指针后剩 9 处"，实为**不看形态＝43／任意含 `*`＝9／首项裸指针＝8**（本文档表格同一版也曾把"任意位置含 `*`"错挂在 268 那一行——复现变体把它按回 153）。**存疑站点本轮读到 113**：`service.Create(...)` 这类**接收者由 helper 构造**（`types` 表里只有 `x := &Foo{}`／`NewFoo()`／`var x *Foo` 三种字面构造点）的调用点全部落在"认不出类型"那一档，只印数、不进基线、不判红——这是门的**已知盲区**，不是"这 113 处干净"。

**3 · 门有没有牙：不靠推断，把真文件的旧字节塞回真树跑。** 四份被测文件（QQ、cond_tree、两个 repository）先 `cp` 备份，再用 `git show HEAD:<path>` 覆写回工作树（**不对未提交文件跑 `git checkout`/`restore**——[[feedback-reverse-test-file-restore]]），跑门 ⇒ `产码…＝114 · 测试确证站点＝8（4 个文件）`，四条 `NEW` 逐文件列出、`rc=1`；按备份写回后四文件 md5 与覆写前**逐文件全同**、门回到 `站点＝0 rc=0`。⇒ "收口 8 处"与"门能抓这 8 处"是同一次实验的两相，不是两句分开的说法。**收尾复核补记**：这趟实验先前只有终端读数、没落盘产物（第 4 段刚为同类事收回一句话，这里不能双标），故在最终字节上原地重跑一遍并落两份日志——`/tmp/bm10_teeth_run5.log`＝`＝114 · 站点＝8（4 个文件）` + 四条 `NEW` + `rc=1`，`/tmp/bm10_restored_run6.log`＝`＝114 · 站点＝0` + `rc=0`，四文件 md5 逐文件 `SAME 4/4`。表头那三行的 241／119、268／43 两相有盘上产物（`bm9_deref_gate_run1/2.log`），114／8 这一相自此也有。

**4 · service 整包门的四趟读数，以及一处必须收回的说法。** 盘上四份带 `MARKER-END` 的日志（本机 load 11–14，用时随负载摆动）：

| 趟 | 对象树 | 用时 | 读数 |
| --- | --- | --- | --- |
| `bm9_v2_svc.log` | 影子克隆 `1af6d28c`（只含已提交内容） | 477.142s | `FAIL`，唯一一条 `--- FAIL: TestD12_NoNewLegacyKVDirectQuery` |
| `bm9_v2_svc_run2.log` | 同一影子克隆 | 547.418s | 同上 |
| `bm9_svc_run3.log` | 活树 | 581.805s | 同上 |
| `bm9_svc_run4.log` | 活树 | 604.919s | 同上 |

⇒ 四趟**全红于同一条**，且**影子那一侧也红**——红在已提交内容里，与本轮用例无关。根因：`internal/service/quote.go` 的两处**注释**（活树 13:14 读为 `:45`、`:214`；先前记的 `:211` 随并行泳道改文件已漂移 ⇒ 行号一律带"测于何时"）写了表名 `system_config_kv`，而 D12 守卫读的是**整文件原文**（`strings.Contains(content, "system_config_kv")`），白名单按文件名放行、`quote.go` 不在册。引入提交 `91482dca`（报价卡 T-P6-02），且 `91482dca` 已是两远端 `master` 的祖先 ⇒ **`go test ./internal/service/` 这条红从公网树上早就在**，`git show upstream/master:…config_param_guard_test.go | grep -c goCodeOnly` ＝ 0（远端守卫还没有剥注释那段）。修法已在并行泳道的**未提交**字节里（`goCodeOnly`：`go/parser` 剥注释、解析失败**退回原文**即判得更严），故本泳道不重复那一刀，只登记归属（同一件事的口径面见 [[project-gate-scope-blind-spots]] 的"守卫读原文还是剥注释"）。⇒ 本段另须收回一句：先前账上写过"run2/run3 两趟绿（569.836s／594.707s）"，**回读盘上产物不成立**（`/tmp` 里没有这两个数，四份日志末行一律 `FAIL` + marker），属"没核产物就转述"的复发（[[feedback-verify-secondhand-review-claims]]，样本包括我自己上一轮的摘要）。另两趟 `bm9_final_repo.log`/`bm9_final_svc.log` 是 **ENV-BROKEN**：`nohup zsh -c` 起的 detachment 不继承交互 shell 里导出的 `POSTGRES_TEST_PASSWORD` ⇒ 整包 90 秒"红完"、每一条红因同为 `failed SASL auth … 28P01`。口径：**红得快 + 红因同一条＝门没开**，先补 env 再谈判据。补 env 后的单格读数（活树 13:14:55，产物 `/tmp/bm10_d12_cell.log`）：`go test ./internal/service/ -count=1 -run 'TestD12_NoNewLegacyKVDirectQuery$' -v` ⇒ `--- PASS (0.76s)` + `ok 1.679s` + `rc=0`——它只证"守卫在剥注释版下放行"，**不是门禁**（`-run` 子集口径见 §23.13）。同一事的三处锚点也一并落盘核过：活树 `config_param_guard_test.go` 里 `goCodeOnly` 命中 **3** 处、`git show HEAD:` 同一文件命中 **0** 处（⇒ 修法确实只在未提交字节里）。⇒ 盘上产物：`/tmp/bm10_d12_cell.log`。（本句先前写的是 `ok 1.379s`，该读数没落盘、无法复现，收尾按新趟重跑后改为上面这组带产物的数——与第 4 段末收回的那句同类，见 [[feedback-verify-by-running]]。）

**5 · 反向电池 16 格，电池自己先交了一次学费。** 驱动 `/tmp/bm9_nil_battery.py` 在 `/tmp/bm9_nil_probe` 里搭一份假仓（一个 `Widget.Find` 真生产者 + 四个"该被排除"的样板：同名不同类的 `Gadget.Find`、切片 `List`、首项是 `error` 的 `Second`、首项是 `int` 的 `Count`），每格换测试字节与基线字节，跑门比对 `rc` 与判据关键字：**控制组**先断言"假仓生产者数＝1 且 C3 判红"，随后 `TALLY ran=16 pass=16 fail=0 skip=0`。格覆盖：`NEW`/`OVER`/`STALE`/`BASELINE-BAD`/`BASELINE-DUP`/缺基线 rc=2、判空在前算放行、**只判 `err != nil` 仍判红**（本门的存在理由）、注释里的 `got == nil` 不算判空、`assert.Nil` 算、判空排在解引用之后仍判红（顺序敏感）、`_` 丢弃与"赋值后不解引用"不算站点、三类非裸指针返回各排一格、接收者解析不出只进存疑不判红。第一版驱动把 `run_gate()` 指向了**原脚本**而不是副本 ⇒ 每一格读的都是真树的 114/113，"绿"和"红"都与我写的字节无关；是控制组那句"假仓生产者该等于 1"当场把它拦下（[[feedback-mutation-battery-hygiene]]：电池自身也会假绿，控制组不是仪式）。**收尾补记**：本段这 16 格在最终字节的脚本上又整跑一遍，产物 `/tmp/bm10_battery_final.log`（`TALLY ran=16 pass=16 fail=0 skip=0` + `BATTERY-END`；先前那趟读数同样只在终端、没落盘）。

**6 · 电池顺带照出门的一处死码，以及 8 处站点里两处"不是 panic、是假绿"。** ① 死码：`存疑` 那一档写作 `if resolved or callee not in names: continue`，而 `names` 的键是 `Type.Method` ⇒ 裸方法名**永远**不在里面，`callee not in names` 恒真 ⇒ 存疑分支根本走不到，门只会"少报"却**看起来**在报。改判据为按"本包生产者**方法名后缀集**"（`method_names`）判，之后真树读到 113 存疑——**判据要能被打到，才算在生效**（同"静态锁得配行为不变的注码"）。② 站点处置（4 文件 8 处，算术 4+2+1+1）：QQ 的 4 处是 `dispatchQQ` 在 `lazyDB()==nil` 那一支交回 `(nil, nil)`；两个 repository 站点是 `GetByAgentKB`／`GetByID` 把 `ErrRecordNotFound` 吞成 `(nil, nil)`，用例形状是 `got, _ := …` 紧跟 `got.Priority`／`got.Name`；cond_tree 那两处读源码后**改口**——`ParseCondTree` 对空串交回 `(nil, nil)`，但 `(*CondNode).Evaluate` **自带 `if n == nil { return false }`** ⇒ 缺判空**不会 panic**，于是这两处的账要分开记：`:9` 那条最坏是"前置没满足被摊成三条看不懂的断言失败"（补判空是**诊断成本**，不是崩溃成本），`:50` 那条期望值本身就是 `false` ⇒ **nil 树也照样"通过"，是假绿**，这一判是承重墙。⇒ 口径：**"未判空"不等于"会 panic"，也不等于"危害更小"**；按被调函数的 nil 守卫形状分别记账，才有"哪一格是真的在防事故"的读数。

**7 · 门进了哪条链。** `make audit` 的静态审计链里排在 `check-env-coverage.py` 之后（同一形状：本地跑、带基线、红因带文件与数额）。**CI 那一头本轮不接**，理由是口径而不是省事：这道门的输入只有 `user-server/**.go`，而它要防的是"新写的用例抬高站点"，接进 `user-server-ci.yml` 就必须同时把 `scripts/check-test-nil-deref.py` 与 `scripts/test-nil-deref.baseline` 写进 `paths:`（否则改判据不触发本门，`docs-link-check.yml` 那次的教训），这一步留给接 CI 的那一批做，届时须按 §23.16 第 5 条的规矩做"摘掉配置照样红"的反向格。**（后续兑现见 §23.20：作业已接、反向格已照做，且顺带把同文件里另外三处同族缺口一起补了。）****收尾补记**：`make audit` 整链在隔离克隆 `/tmp/bm9_mine`（只含本泳道 8 份文件，逐文件 md5 与活树相同）真跑过一遍 ⇒ **rc=0、8 步全绿、末步即本门**（该树读 `＝112 · 站点＝0 · 存疑＝113`），产物 `/tmp/bm10_make_audit.log`；先前只做过 `make -n audit`（干跑，只证命令能解析）。

**8 · 最终字节上的整包复跑（隔离树，排除并行在飞文件）。** 对象：`/tmp/bm9_mine` ＝ `git clone --shared` 到 HEAD `e293e233` 后**只**覆写本泳道 6 份文件（4 份用例 + 门 + 基线，逐文件 md5 与活树相同；`git status --porcelain` 在该克隆里只有这 6 项）。`internal/repository` 整包无过滤：`ok 110.528s rc=0`，`--- FAIL` 计数 0。`internal/service` 整包跑了**两趟**，第一趟不算读数：默认 `-timeout 10m` 下 `panic: test timed out after 10m0s`（600.875s 截断，产物 `/tmp/bm9_mine_svc.log`），截断前唯一那条 `--- FAIL` 已经是 D12 ⇒ 超时不是"包里的红"，是**门没跑完**（[[feedback-mutation-battery-hygiene]] 的"TIME-BROKEN 另记"）。把超时抬到 40m 重跑同一棵树跑完：`FAIL hivemtk-user/internal/service 746.475s`、`svc rc=1`、`--- FAIL` 计数 **1**（`TestD12_NoNewLegacyKVDirectQuery (0.41s)`），无 panic（产物 `/tmp/bm10_mine_svc2.log`）。⇒ 两点入账：① 本泳道字节上 service 整包**除 D12 之外零红**（与第 4 段四趟同形；未带 `-test.v` ⇒ PASS 计数不可得，判据只认 `--- FAIL` 计数与末行汇总）；② 默认 600s 不够：本趟实测 **746s > 600s**（本机 load 8–12；[[project-go-test-suite-timing]]）⇒ 从本批起，凡跑 `internal/service` 整包一律显式带 `-timeout 40m`，否则读数按"未跑完"处理而不是按"绿/红"处理。

**9 · 一次必须登记的"对不上"。** 并行泳道在本段收尾前来信，称已把本泳道工作树里的用例「折进他们的 `cba1849d`」并推送（自述 `ahead=5`、工作树只剩他们的离线化产物）。本树实测：`git fetch upstream`＋`git fetch gitee-upstream` 均 rc=0，两远端 `master` 同为 `d5489aa4`，本地 `master`＝`e293e233`、`ahead=3 / behind=0`（三个未推提交＝`1af6d28c`、`029a6d32`、`e293e233`）；`git cat-file -t cba1849d` ⇒ **Not a valid object**（fetch 之后仍不存在，即它既不在本树也不在两远端）；本泳道那 4 份用例仍 `M`、门与基线仍 `??`。⇒ 结论按"未证实"记：转录请求（把其第 44 轮 v3.43.0 取证写进 §23.15）**同样不落**，因为要引的两处锚点在本树不可复现（`git show HEAD:user-server/internal/pkg/db/migrate.go` 的第 147–149 行现是 `allModels()` 里的模型清单行，`18ca8f8` 在本仓不是有效对象——它属于 assetdpo 那侧的架构门提交，见 [[project-platform-arch-gate-quirks]]）。口径：**别把别人给的行号与 commit 抄进本文档**，能 `git show` 出来的才写；写不出来的按"请求已收到、证据待补"登记（[[feedback-verify-secondhand-review-claims]]）。同一泳道 13:08 的第二次来信另给四项，本树逐项复测：① "离线化四笔提交已落"——**成立**，本地 `git log` 读到 `9058143f`／`065ff053`／`66368fdd`／`6414d663`（同族另有 `d5489aa4`／`e293e233`）；② "`9149593a` 里 7 个文件逐字节一致"——**不成立**，`git cat-file -t 9149593a` 在 hivemtk／hivemtk-platform／assetdpo 三仓均 `Not a valid object`；③ "日志在 `/tmp/pc08_make_audit.log`"——**不在盘上**（`/tmp` 只有 09-18～09-21 的 `pc*.log` 旧文件，`pc08` 零命中）；④ "未推 9 笔"——本树实测 `ahead=3`（`hivemtk-platform` 对 `origin` 为 `0 0`），若其读数取自 12:04 那次推送之前则当时可能为真，**现值不可复现**。⇒ 对 ②③ 不给"已核验"的回执，只回"不重复提交、请改给本树可 `git show`／可 `ls` 的形式"。

**10 · 本段未覆盖面（不得当作已清）。** ① 113 处存疑（helper 构造接收者那一档）没有等价的类型解析，门只承诺"确证面不新增"。② 产码侧仍只认**字面** `return nil, nil`——`return` 换行、经变量中转的 nil、以及 `nil, fmt.Errorf(…nil…)` 之类都不进门视野；跨包调用面同样未解析（`internal/service` 调 `internal/repository` 的生产者抓不到）。③ 门**没进 CI**（第 7 段），所以它拦不住"只在 CI 里跑出来"的新站点，只拦本地 `make audit` 与人工那一趟。④ 第 4 段那条 D12 红仍在并行泳道的未提交字节里，本树**没有**它绿着的 HEAD；在它落地之前，任何"`internal/service` 整包绿"的说法都必须附"除 D12 之外"。⑤ 22 包 `-p 1 -race` 全量趟仍未跑（欠账自 §23.16，本轮 load 11–14 未清）。**归因补记（13:20 读盘）**：巡检泳道已在 13:06:51 起自行跑 `-p 1 -count=1 ./internal/...`（**非** `-race`），对象是其克隆 `.tmp_files/audit-watch/hivemtk-e293e233-r46`（＝HEAD `e293e233`，**不含**本泳道未提交的 8 份文件），日志 `r46-gotest.sh` 起的 `r46-gotest.log` 13:20 读到 48 行、推进到 `internal/geo/repository` ⇒ 本泳道不重复那一趟非 race 全量；`-race` 变体、以及"含本泳道字节的整树全量"两件事仍开放。

**11 · 提交边界上照出的第二道"绿不在我这边"：`audit-secrets` 压在本泳道自己的未推提交里常红。** 门（`6d135bc4`）落库后按推送前自查跑 `bash scripts/check-secrets.sh` ⇒ **rc=1、3 条命中**：`webhook_batchc_d04_tiktok_http_test.go:25`（`tt_client_secret_http_d04`）、`webhook_batchc_d04_tiktok_test.go:27`（`tt_client_secret_d04`）、`webhook_batchg2b_douyin_media_test.go:208`（httptest 自有 URL 上的 `…?secret=skv2…%3D`）。**归属先核再认**：三条文件 `git log -1 -- <path>` 全部指回 `1af6d28c`（本泳道"十九批用例随产码入库"那一笔），`git status --short` 对这三条为空 ⇒ 不是并行泳道的在飞字节，而是**本泳道未推的三笔里压着一道红门**（同"未推提交对 CI 隐形"那一族：`audit-secrets` 无 CI／hook 入口，只在 `Makefile:474`）。

处置沿用 `Makefile:464` 起写死的规矩——"恒红的门与坏掉的门长得一样"，所以**逐值豁免、不开 `*_test.go` 整类口子**：三条以「文件 + 变量名/函数名 + 完整值」三段锚点登记进 `scripts/.secret-allowlist`（`b1407b3b`）。值确为假夹具的三条独立读数：A 段（本机 `.env` 真值逐个搜进待纳管文件）对这三条一律未命中；`grep -c` 三个值串在 `.env` 里 0 命中；前两条是"名字即值"的自描述串（值就是变量名去掉大小写后拼出来的 `tt_client_secret_*_d04`，带用例编号 `_d04`，不是任何服务下发的随机串），第三条的对端是自己起的 httptest 进程。**真实凭证的位数形状本轮未取证**，故这三条的判据只落在"与本机 `.env` 无交集 + 形状自描述 + 对端是本进程假服务器"三条可复现读数上。**豁免窄不窄要反向测**：把三处值各换成另一枚 24 位子串像样的串 ⇒ 门仍 **rc=1** 且逐条点名（说明锚点打在"值"上而不是"文件"上），`cp` 备份写回后 `git status` 对三条路径为空、全树门回到 rc=0（`--verbose` 读到 `(allow)` 4 条＝3 新 + 1 条 064c6a21 的旧豁免）。

**自己交的一次学费（记进正文，不只是提交信息）**：第一次改表用了**整文件重写**，把表头那 15 行规矩（"为什么只能逐值豁免"＋"本表自身也在扫描范围内，所以豁免式不能写成 `Xxx = "值"` 的形状，2026-09-19 实测过一次"）连带删了——而那段正是我刚引为依据的出处。发现方式＝提交后 `--stat` 读出 `15 insertions(+), 16 deletions(-)` 与"我只该加 3 行"对不上，而不是等别人指出。修法：`git show HEAD~1:` 取回原文后**程序化重组**（表头 + 旧条目 + 新块，逐段断言仍在），复跑门 rc=0 再另起一笔 `546895e5` 补回。⇒ 口径：**已在 git 里的文件用局部替换改，别用全量覆写**；写完必须核 `--stat` 的增删比是否等于意图，`deletions` 非零而本轮没打算删东西＝红灯。

**12 · 推送后 CI 抓出的第一道"红已经上了两远端"：D12 守卫在 HEAD 上常红，红因是注释里的一个表名。** 推送后的 run 读数（`gh run list --workflow=user-server-ci.yml --json headSha,status,conclusion,databaseId` → `bd4dadaf` 那趟 id `35691489563`；`gh run view <id> --json jobs` 逐格读）：`Static gates (arch/vet/gofmt/lint/build)`、`Security scans (gitleaks / govulncheck)`、`npm audit (user-web)`、`Benchmarks (P0-5)`、`Bridge Extension Tests`、`Lint (embed-sdk)`、`API Inventory Report`、`Build (user-web)` ＝ success；`ESLint (user-web)` 与 **`Unit tests -race (user-server service)` ＝ failure**；`Unit tests -race (user-server core)` 与 `Coverage` 读时仍 `in_progress` ⇒ 那两格本段不声称任何结论。`Markdown Lint`／`Docs Link Check`／`Docs Consistency`／`SBOM`／`API Contract`／`ENUM Consistency` 六道在 `gh run list --json workflowName,headSha,status,conclusion` 里对 `bd4dadaf` 逐条 success（各自一条 workflow，非同一文件）⇒ 第 11 段那条常红随推送结掉。

两条红各自归属，都不落在本泳道的判据上。**ESLint** 在两条 workflow 上各红一次（`user-server-ci` 的 `ESLint (user-web)`、`Lint` 的 `ESLint (user-web 主应用)`，后者 run `35691489569`、`headSha` 同为 `bd4dadaf`），日志读自后者：`--log-failed` 里按时间戳把文件名与其下的 error 行配上，两条 error 是 `##[error] 216:7 error There is no `cause` attached…preserve-caught-error` → `user-web/browser_automation/src/core/cdp/input.js`、`821:15 error The value assigned to 'navigated' is not used…no-useless-assignment` → `user-web/browser_automation/src/core/primitives.js`，汇总行 `✖ 18738 problems (2 errors, 18736 warnings)` ⇒ 与 §23.14 名册里那两条老红逐字同号同位；本泳道入库的十九批用例那一笔 `git show --stat 1af6d28c | grep -c user-web` ＝ 0，非本批引入，维持原归属不动对方热区。

**D12** 这条本段**不新立事实**——"红在已提交内容里、`91482dca` 已是两远端祖先"第 4 段就已钉死（连同"远端守卫还没有剥注释那段"）。本段补的是三件第 4 段拿不到的东西：**CI 侧第一次点名**、**纯已提交字节上的复现**，以及**修复从未入库**这条订正。CI 日志当时取不到（`gh run view --log-failed` 回 `run … is still in progress; logs will be available when it is complete`）⇒ 改在**同一份字节的干净克隆**取证：`/tmp/bm10_push`（`clone --shared` 到 `bd4dadaf`，`git status --porcelain` 空）单跑那条用例 ⇒ `--- FAIL: TestD12_NoNewLegacyKVDirectQuery (0.30s)`，红因 `config_param_guard_test.go:77: 发现新增遗留 KV 直查（新配置请走 ConfigParamService）: [../service/quote.go]`、包级 `FAIL hivemtk-user/internal/service 1.378s`。红因**不是直查**：`git show bd4dadaf:user-server/internal/service/quote.go` 里 `system_config_kv` 出现 2 次，`:45` 与 `:211` 两行都以 `//` 开头（逐 occurrence 看前缀：注释位 2 / 非注释位 0，两行都不含反引号或引号，即没有一处进过 SQL 字符串），该文件由 `91482dca 2026-09-22 09:37:28 +0800 feat(quote): T-P6-02 报价生成…` 引入。守卫判据是 `strings.Contains(整文件原文, "system_config_kv")`，于是"在注释里解释自己为什么用 KV 存策略"必然判红 ⇒ 同一条闸在本文档里第三次报同一形状（N-23 第一现场 `ltc_config.go` → §23.17 第 4 段 `quote.go` → 本段的 CI 读数），[[project-gate-scope-blind-spots]] 的"守卫读原文还是剥注释"那一档到此已不是假想风险。

**订正一条我自己写下的旧事实**：§1 名册 N-23 那格的处置写的是"**已修（批G 收尾）**：`goCodeOnly()` 用 `go/parser` 把注释替换为等长空格后再匹配"。**该修复在全部提交历史里查无字节**：`git log --all -S"goCodeOnly"` 只命中三笔**文档**提交（`7329590d`＝本档首次入库那一笔、`6d135bc4`、本档这一笔），逐笔 `git show <c> -- user-server/internal/service/config_param_guard_test.go` 对动过该用例的四笔（`5db5e17f`/`bc73fe4d`/`3513c879`/`bcbb8010`，均为 HEAD 祖先）grep `goCodeOnly` 命中数全为 0，`git show HEAD:...config_param_guard_test.go | grep -cE "go/parser|goCodeOnly|ParseComments"` ＝ **0**，而 HEAD 版里那段"写 `禁止新增`/`D12` 即放行"的注释式豁免**仍在**（N-23 同一格却写着"随之失效"）⇒ 状态从"已修"改回"**未入库**"，按 [[project-batch-work-clobbered-by-parallel-session]]（未提交改动曾被静默还原）与"没核产物就转述"同一族记；这也解释了为什么同一枚修复要被写第二次。

第 10 段第 ④ 条据此**改判（且是判错方向的那类，须说明）**：那句"D12 红仍在并行泳道的**未提交**字节里，本树没有它绿着的 HEAD"——后半句对，前半句**不成立**：上面那趟复现跑在 `git status --porcelain` 为空的干净克隆里，触发文件 `quote.go` 与守卫用例都已在 `bd4dadaf` 之内 ⇒ **整条红不依赖任何未提交字节**，它在两远端当前顶点（`603d2dd2`、`a94bf639`）上原样存在。写错的代价也在这里：按④的口径人会判"等对方提交即解"，而真值是"对方那笔补丁此前从未入库、现在正在被第二次写"。**修复当前在对方工作树里、未提交**：`config_param_guard_test.go` 本树 `git status` 为 ` M`、`git diff --stat HEAD` ＝ `+32/−1`，新增 `goCodeOnly()` 用 `go/parser` 把注释区间逐字符涂空后再匹配（解析失败退回原文＝判得更严而不是放行）。本泳道**不碰这份文件、不代为提交**（它在并行泳道的 do-not-touch 名单上；把别人半截的在飞改动扫进我的提交，代价比这道红本身大）。只在相同字节的影子克隆里做两件验证：① 打上他们的版本 → `--- PASS: TestD12_NoNewLegacyKVDirectQuery (0.61s)`、`ok hivemtk-user/internal/service 1.750s`；② **修完还有没有牙**——在影子树 `internal/service/` 临时新写一个只含代码级字符串的文件（`const bm11ProbeSQL = "SELECT v FROM system_kv_config WHERE k = $1"`）→ 用他们的版本仍 `--- FAIL (0.43s)` 并点名 `[../service/bm11_probe.go]`（红因行号 77 → 108，只因补丁加了函数，不是判据变了）。影子树收尾：删探针、把被覆盖的用例 `cp` 回 `git show bd4dadaf:` 那份，md5 两侧同＝`ff2d214069800c2c7f159e6d5d4071fa`、`git status --porcelain` 行数 0（活树与远端全程未动）。远端顶点逐笔复核其 diff **不含** `config_param_guard_test.go` 与 `quote.go`，且 `git show a94bf639:user-server/internal/service/config_param_guard_test.go | grep -c goCodeOnly` ＝ 0 ⇒ **等对方那笔补丁落地即解，本泳道不抢**。

**本段未覆盖面（不得当作已清）**：① 只单跑了 D12 那一条用例，**没在 `bd4dadaf` 的纯已提交字节上跑过 service 整包**，故第 8 段那句"除 D12 之外零红"读的是含本泳道未提交字节的树，不能就地升级成"HEAD 上除 D12 外零红"；② CI 的 core `-race` 与 Coverage 两格读数仍 in_progress，回填前不得引；③ `-race` 这一趟在影子树未复跑（本段是单用例复现，判据只到"红因可复现"，不到"race 干净"）；④ 白名单里那 18 个文件名与"注释里提表名"这条豁免口子之间的边界（剥注释后是否有任何一个白名单项其实只为绕注释而存在）未逐个核，交对方那笔补丁一起看。⇒ ①③ 由第 13 段结掉（整包 `-race` 已在含修复字节的干净树上跑完），② 同段回填，④ 仍开放。

**13 · CI 的 `-race` 那格抓到四条红，其中一条确实是本泳道自己写的：修复、同族扫描，与一次"绿是运气"的形状订正（用例侧，产码零改动）。**

**归因前先记推送态的漂移**（本段所有 `gh` 读数都依赖它）：`git reflog show upstream/master` 读到 `bd4dadaf` 已于 13:38:18 双推（两远端同一分钟），其后并行泳道又推了 `603d2dd2`（13:42）与 `a94bf639`（13:45）⇒ 第 12 段那句"未推的两笔文档提交"已过时，本树实测 `git rev-list --left-right --count upstream/master...HEAD` ＝ `0 5`（未推 5 笔 = 本泳道 `4732002c`/`7747f403` + 并行 `ccbd7b6f`/`6543ffcf`/`53034b42`），且 `HEAD` 之上仍在长（本段收尾时 `5f76e471` 已上了两远端）⇒ **本泳道下一次推送会连带别人的提交**，故推送仍单独请口径、不自行决定。

**1 · CI 读数（对象 `a94bf639`，即两远端顶点上"含第 12 段那条 D12 红"的那份字节）。** `gh run list --json headSha,workflowName,status,conclusion` ＋ `gh run view <run> --json jobs` 逐格读：`user-server-ci`（run `35691962298`）里 `Static gates`／`Security scans`／`npm audit`／`Benchmarks`／`Bridge Extension Tests`／`Lint (embed-sdk)`／`API Inventory Report`／`Build (user-web)` ＝ success，**`ESLint (user-web)`、`Unit tests -race (user-server service)`、`Coverage (user-server)` 三格 failure**，而第 12 段读时 still-in_progress 的 **`Unit tests -race (user-server core)` 现为 success** ⇒ 那格的 `TestAutoMigrate_ConcurrentAccess` 疑虑随之撤销（它没在 core 报红；本段不引任何"core 红"的说法）。另两道 workflow 在同一 SHA 上的读数：`Lint`（run `35691962293`）failure（其 `ESLint (user-web 主应用)` 那格与 `user-server-ci` 里的 ESLint 同因，第 12 段已归到 browser_automation 那两条老红）、`SBOM`（run `35691962310`）亦 failure，而 `Markdown Lint`（`35691962296`）／`API Contract`（`35691962374`）success。`SBOM` 红因读自其 `--log-failed`：`curl: (35) Recv failure: Connection reset by peer` → `##[error]Process completed with exit code 127.`，且下一趟（`5f76e471`）SBOM 转 success ⇒ 判为 CI 侧取工具的瞬时网络态，**不记为产码/用例事实**（`gh api .../jobs/<id>/steps` 回 404，本段没拿到步骤级 conclusion，故这条只有日志一个来源）。

**2 · `Unit tests -race (user-server service)` 的四条红逐条归因**（日志整份存 `/tmp/bm15_ci_svc_race.log`，10598 行；包级 `FAIL hivemtk-user/internal/service 366.893s`——CI 366s vs 本机 1052s，同一件事两处摆动 3 倍，[[project-go-test-suite-timing]]）：① `TestM01_QQFetchUsesRealAttachmentURLAndBytes` —— `testing.go:1712: race detected during execution of test`，前面 4 条 `WARNING: DATA RACE` ⇒ **本泳道自有用例，真缺陷**，第 3 段处理；② `TestD12_NoNewLegacyKVDirectQuery` ⇒ 第 12 段那条注释假阳，红因不变、修复仍在对方工作树；③ `TestFallbackVersionResolvesDBHandleSynchronously (0.50s)` 红因 `async_db_handle_probe_test.go:47: 连接 PostgreSQL 测试库失败（dsn=host=127.0.0.1 port=5432 ... user_db_test_slot0）: FATAL: sorry, too many clients already (SQLSTATE 53300)` ⇒ **环境红**（CI 机上 PG 连接数打满），不是判据；④ `TestAbExperiment_LogExposureFireAndForget (0.00s)` 红因 `ab_experiment_test.go:50: DroppedCount = 1, want 3（buffer=2，满则丢弃）` ⇒ 调度依赖断言（fire-and-forget 溢出计数在 CI 负载下不必然填满 buffer）。③④ 的归属**不用推断交付**：在 `a94bf639` 的干净 `--shared` 克隆（`git status --porcelain` 行数 0）里 `-race -count=4 -p 1` 单跑这两条 ⇒ `rc=0`、`TestAbExperiment_LogExposureFireAndForget` 4/4 PASS（每条 0.00s）、`TestFallbackVersionResolvesDBHandleSynchronously` 4/4 PASS（0.09／0.27／0.67／5.78s 摆动即"连库/拿到句柄"的时序敏感证据），`DATA RACE` 0 条 ⇒ 判"CI 环境/负载态"，本泳道**不改别人的用例**。

**3 · 本泳道那条 race 的修法与 A/B。** 形状：转存回写发生在 `persistQQMediaAsync` 的 `utils.SafeGo` 协程里（`qq_media.go:116` 调 `qqMediaStoreFn`），闭包直接写测试捕获的 `gotData`/`gotCT`/`gotHint`，测试协程裸读同一组变量轮询。修法是快照过锁（`sync.Mutex`：写侧锁内赋值、轮询读 hint 锁内取副本、断言前一次性锁内摘三个值），与 telegram 同族用例既有的形状一致（比它更严——telegram 循环后那次 `gotHint` 读没进锁）。取证：修复前 `-race` ⇒ `--- FAIL`＋4 条 `WARNING: DATA RACE`（写 `:340`／读 `:347`、`:351`、`:354`、`:357`，四个读点各配一条 race 报告 ⇒ 4 条；这些行号属**修复前那份字节**，现树同位置已是别的语句（`:340` 现为 `var gotData []byte`），别再拿它们定位）；修复后同用例 `-race -count=6` ⇒ rc=0、race 0、PASS=6/FAIL=0；整文件（该文件共 10 条用例）`-race -count=2` ⇒ rc=0、PASS=20（10 名 × 2 轮）、FAIL=0、RACE=0、`ok 29.263s`——**这一趟是在第 4 段的原子计数改造也落完之后重跑的当前字节**，不是修复当时的旧读数（当时那趟 `ok 43.680s`，两次摆动 1.5 倍，同 [[project-go-test-suite-timing]]）；顺带记一次取证工具的坑：同一趟先不带 `-test.v` 跑，`--- PASS` 计数恒为 0（非 `v` 模式包级只印 `ok`），这类计数必须带 `-v`（[[feedback-cli-toolchain-gotchas]] 已录，本轮再踩一次）。**牙仍在（本轮在改造后的当前字节上重打了一次，不是转述修复当时那一趟）**：影子克隆 `/tmp/bm14_ci`（`a94bf639`，`git status --porcelain` 打点前后均为 0 行；把本泳道现字节 `cp` 进去后 md5 两侧同＝`3de475e1…` 见下），将夹具响应改成截半（`:333` → `w.Write([]byte(payload[:2048]))`）⇒ `rc=1`、`--- FAIL: TestM01_QQFetchUsesRealAttachmentURLAndBytes (5.96s)`，红因 `webhook_batchf4_m01_qq_test.go:370: 转存字节数 = 2048, want 4096（下载腿把文件读残了）`。**行号从初记的 `:369` 挪到了 `:370`**——第 4 段的改造在断言块之前多插了一行（`mu.Lock()` 取快照那组），⇒ "文档里的 file:line 要在最后一次源码编辑（含纯形状改动）之后复算"这条又踩实一次（[[feedback-verify-secondhand-review-claims]]）；`cp` 写回克隆原版后 `git status --porcelain` 行数 0。**md5 要按版本分开记，否则后人读成"还原没做干净"**：那一次比对的是"仅互斥锁快照"那一版的字节 `801e318fc60d46f2b6994cf46d0d35c6`；第 4 段随后把同文件 4 条计数腿换成 `atomic.Int32`，**现活树字节 md5 ＝ `3de475e194b2b0cad0bcd68972a561de`**（`md5 -q internal/service/webhook_batchf4_m01_qq_test.go`），两者之差就是那 4 条腿的形状，不是残留差异。

**4 · 同族扫描：12 处"睡后断 0"的计数（外加第 3 段那条正例腿），一枚实测出 race、一枚实测不出——原因须写清。** 静态口径＝生产侧在 `SafeGo`/`SafeGoDetached` 协程里调用的包级 seam（`*MediaFetchFn`/`*MediaStoreFn`/`TokenFn`/`sendFn`/`handleFn`/`aiReplyQuietHoursFn`）∩ 测试侧把这些 seam 换成"写外部变量"的闭包。命中 4 份文件 **12 条**"期望 0 次"计数腿（`webhook_batchf4_m01_qq_test.go` 4、`..._telegram_test.go` 5、`..._dingtalk_download_test.go` 1、`wechat_batchf4_m01_media_test.go` 2；逐条名 `grep -n atomic.Int32` 对 `^func Test` 配得上，计数与文件名行号同树可查），形状全是 `X++` 写在 seam 体内、`time.Sleep` 后 `if X != 0 { Errorf }`。除这 12 条之外本段还动了**第 3 段那条 QQ 正例腿**（`TestM01_QQFetchUsesRealAttachmentURLAndBytes`：断的是"转存确实发生了 1 次且字节/类型对"，不属"断 0"家族，同步手段是快照过 `sync.Mutex` 而不是原子计数，见第 3 段）⇒ **动 13 条腿、其中 12 条是同一形状**，初稿把 13 条都写成"睡后断 0"是不准的。两枚 A/B（对象 `/tmp/bm12_probe`，干净克隆覆写本泳道文件）：把公众号假接口的 `err-` 分支挪走让它回真字节 ⇒ 修复前 `WARNING: DATA RACE` **1 条**（`Read at ... wechat_batchf4_m01_media_test.go:197 by goroutine 18` / `Previous write at ... :188 by goroutine 26` ← `wechat_inbound_media.go:177` ← `utils.SafeGoDetached`）；**同一棵树换成 `atomic.Int32` 后同样的翻转 ⇒ 照样红两条（`:199`、`:206`）而 `DATA RACE` 计数 0** ⇒ 修的是竞争、没削判据。反过来，把 `vid-` 分支挪走的那一枚**不出 race**——不是它同步得好，而是 `:171` 那次 `f4WxMediaURL` 的 DB 读把 happens-before 边补上了（写→DB 更新过连接池互斥，测试读 storeCalled 之前先做过同池查询）⇒ 口径：**"睡醒再断 0"的计数在 `-race` 下绿不绿取决于中间有没有别的共享锁，属运气形状**，与 [[feedback-async-and-global-state-tests]] 的"回填断言要轮询/排空再断总数"同族，差别只是这批断的是"零次"。（本小段行号分两版，读的时候别混：**修复前那两条 `:188`（写）／`:197`（读）与 `:170` 属改造前的字节**；改造后同一条腿是 `:189` 写、`:198` 读，而"照样红两条"的 `:199`、`:206` 两句 Errorf **就是当前树的位置**，已 `sed -n` 逐行核过。）修法统一为 `atomic.Int32`（`.Add(1)`／`if got := X.Load(); got != 0 {`，红因文案改引 `got`）。验证：13 条腿 `-race -count=2` 逐条点名 `pass=2`（含第 3 段那条 QQ 正例腿；日志 `/tmp/bm13_atomic_subset.log`）。**这趟读数的分母要说清**：`--- PASS` 行 114 ＝ **57 个顶层用例 × 2 轮**（前缀正则 `TestM01_*` 一并带进 42 条 M01 族、另有 `TestN17_*` 14 条与 `TestE2E_QQ_*` 1 条），不是"13 条腿 × 2"；`=== RUN` 行数 192 含子测试，别拿它当分母。同趟 FAIL=0、RACE=0、SKIP=0、`ok hivemtk-user/internal/service 346.852s`。`-run '^(TestM01_QQ|TestM01_Telegram|TestM01_DingTalk|TestM01_Wechat|...)$'` 过滤出来的**只是子集取证，不算门禁**，门禁是第 5 段那趟整包 `-race`。`gofmt -l internal/service/ internal/repository/` 空（**口径要说清**：提交边界复跑时整棵 `internal/` 的 `gofmt -l` 报 1 处＝`internal/browser_automation/repository/command_log.go`，该文件 `git status` 为 ` M`、属并行泳道在飞字节，本泳道不碰也不代为格式化；只把范围收成本批动过的两个包才是"空"这个读数的真身）；`go vet ./internal/service/ ./internal/repository/` rc=0。被扫到但**判为非命中**的要留名，免得下批重新怀疑一遍：`handleFn`（`webhook_recovery_test.go:141/166` —— 该文件同步调 `sc.replay`/`sc.scanOnce`，协程只在 `Start()` 里起而用例不起）、`aiReplyQuietHoursFn`（三处闭包只回常量，不写外部变量）、`wecom*Fn`/`wa*Fn`/`dt*Fn` 的 `msgtype`/`n10`/`dingtalk_download` 腿（回传走 channel，或 `sync.Map`）、`lead_mining_integration_test.go:59`（`LeadMiningConfig` 无凭证列）、`internal/bridge/reach_adapter_test.go:244`、`:256`（打印的是 `tooluse.AccountHealthInfo`／`[]tooluse.AccountInfo`，定义在 `internal/aiagent/agent/tooluse/reach_tools.go:35`、`:46`；两结构全列只有账号标识、渠道、状态、配额、风险/时间字段（`AccountInfo` 另有 `Nickname`/`IsHealthy`），**无 secret/token/key 列** ⇒ 不属凭证面）——注意这两个文件在 `internal/bridge/`，不在 `internal/service/`，只写文件名会让人在错的包里找。

**5 · 第 12 段未覆盖面①③ 的账：整包 `-race` 在含修复字节的干净树上跑完。** 对象 `/tmp/bm10_push`（`clone --shared` 到 `bd4dadaf`，仅覆写本泳道那份 QQ 用例，`git status --porcelain` 只这一项）：`go test ./internal/service/ -race -count=1 -timeout 40m -v` ⇒ `PASS=3894`、`--- FAIL` 计数 **1**（`TestD12_NoNewLegacyKVDirectQuery (0.29s)`）、`--- SKIP` 3、**`WARNING: DATA RACE` 0**、`FAIL hivemtk-user/internal/service 1052.467s`、rc=1。两点入账：① "除 D12 之外零红、且零数据竞争"第一次在**整包无过滤 `-race`** 上成立（此前只有单用例复现与非 race 趟）；② 1052s ≫ 默认 600s（本轮 load 峰值 76）⇒ 第 12 段立的"`internal/service` 整包一律 `-timeout 40m`"不是保险，是**不写就读不到**的那一类。

**6 · 顺手收掉的一处凭证面：测试红因里 `%+v` 打印带凭证列的结构体。** 触发点是第 4 段那枚"截半"反向测的日志读数顺出来的（要读它有没有把夹具值打出去），随后按"类型带不带凭证列"逐个核（`grep -A "type X struct" internal/model/`）而非按 `grep -c %+v` 的计数交差。四处确认命中、其中三处属本泳道字节、一处在并行泳道的提交（文件干净，一并收，改动只是文案）：`webhook_batchf4_msgtype_test.go:524`（`*model.FeishuAccount`：`AppSecret`/`VerificationToken`/`EncryptKey`/`AccessToken` 全在表上）→ 只印 `AppID`；`webhook_channel_qq_test.go:57`（`*model.QQAccount`）→ 拆成两条字段级断言、只印 `len()`；`obs_config_default_batchm_test.go:210`（`*model.ObsConfig`：`AccessKey`/`SecretKey`——正是"禁止 `%+v` 打印 `model.ObsConfig`"那条口径的字面对象）→ 只印 `Name`+`ID`；`telegram_ai_sales_test.go:485`（`*model.TelegramBotAccount.BotToken`，owner `c96b8964`）→ 同名形状改造。**反向测（三枚，全在影子树）**：把夹具/期望各改一处 ⇒ 三条各红一次（`FAIL=3 PASS=0`），红因行分别是 `替身收到的账号不对：AppID="a"`、`AppSecret 未原样读回（got 长度 16, want 10；凭证值不落测试日志）`、`接口返回错误 JSON 却上传了 1 次…`——被改坏的那个 16 字节串**没出现在日志里**，即掩码是真生效而不是文案好看；收尾把三处翻转 `cp` 回原字节，逐文件 md5 与活树 SAME、探针克隆 `grep -rc "BROKEN-BY-PROBE\|MUTATED-BY-PROBE"` 命中文件数 0。门禁随提交边界复跑：`python3 scripts/check-test-nil-deref.py` ⇒ 确证站点 0／存疑 113、rc=0（本段只动用例侧文案与计数形状，站点集合不新增），`bash scripts/check-secrets.sh` ⇒ rc=0（A 段零命中——门是"任一段命中即 FAIL"，整体 rc=0 就把"A 段对这两条命中过"一并排除了）。**订正本段初稿写下的一句假事实**：初稿写"`webhook_channel_qq_test.go` 里重复出现的 `"secret-abc"` 是既有夹具字面量，仍在 `b1407b3b` 的逐值豁免之内"——查无依据：`grep -c 'secret-abc' scripts/.secret-allowlist` ＝ **0**，该表非注释行只有 4 条（nm-host 一条 + `b1407b3b` 三条），`--verbose` 打印的 `(allow)` 恰好也是这 4 条，没有一条指向本文件 ⇒ 这两个值**从未进过豁免表**。它们不被报出的真因是**长度**：B 段 `PATTERN` 的右值要求 `[A-Za-z0-9@#_-]{16,}`，`secret-abc`＝10 字符、`bot-secret-xyz`＝14 字符，都短于此 ⇒ 门根本没看见这两行。反向测（对象 `/tmp/bm14_ci`，`a94bf639` 干净克隆，`ENV_FILE` 指向真实 `.env`）：在同一目录临时放一份探针，两行形状完全相同（struct 字段赋值 + 明文值）、只有右值长度不同 ⇒ 10 字符那行**不报**、23 字符那行报 `❌ ...bm16_probe_test.go:9: WebhookSecret: "secret-abc-0123456789"` 且 rc=1；删探针复跑 ⇒ rc=0、`git status --porcelain` 行数 0（活树全程未动）。也就是说命中与否**与"键名/struct 字段形状"无关，只与长度有关**（同一形状加长即命中，且 `internal/service/*_test.go` 确在扫描范围内——上面那三条 `(allow)` 就是它报出来后被逐值豁免的），代价是**任何 <16 字符的凭证字面量对这道门完全隐形**。这一维属 [[project-gate-scope-blind-spots]] 的 ⑩ 命中阈值轴，本段**只登记不修**（压低阈值会把大批短夹具与占位符变红，属另一次口径决策）。

**本段未覆盖面（不得当作已清）**：① 第 4 段的同族扫描只覆盖"生产侧 seam 在 `SafeGo*` 协程里被调"这一族，**没有**扫"测试自己 `go func()` 起的协程里写外部变量"，也没解析跨包 seam（`internal/channelbot/*` 自己起的 goroutine 回写测试变量那一族未看）。② `atomic.Int32` 只保证单次读写的竞争性消失；`if got := X.Load(); got != 0` 与后续 `Sleep` 窗口的组合仍是"到点为止没发生＝没发生"，**没**改成排空式断言（改法要把"零次"变成"观察窗口内零次"的显式预算，属另一批）。③ CI ③④ 两条环境/负载红只做了一次"本机 4/4 绿"的反证，**没**在 CI 上复跑确认它们随下一趟转绿（`5f76e471` 的 `user-server-ci` 读数在本段收尾时仍 `in_progress`）⇒ 不得当作"CI 只剩 D12 + QQ race"。④ `Coverage` 那格的 `--- FAIL` 只到 D12 一条（日志里 `[JourneySleepCron] 沉睡检测 panic: ... nil pointer dereference` 与 `[order-draft] 意向提取 panic 已 recover` 都被 recover 接住并只打 ERR/WRN 行，**不是**失败证据；这一归类不是本段新判，§20.6 的"门里 panic 字样逐行归因"早已把同一行钉成 `TestJourneySleepCron_RunOnce_PanicRecovered`（喂 `NewJourneySleepCron(nil)`）自己打的日志，本段只是把 CI 日志里那两行按同一口径对上号），覆盖率数字本身未读。⑤ QQ 修复与 12 处 `atomic.Int32` 改造（＋第 3 段那条过锁的正例腿，共 13 条腿）目前只在**影子树＋子集**上验过；提交后要在只含已提交字节的干净克隆上再跑整包 `-race`（本段第 5 条那棵树含未提交覆写，严格说仍是"字节混合"证据）。⑥ 第 6 条那个 `<16 字符` 的长度盲区只登记了"它存在"（一枚截半探针），**没有**清点全仓还有多少处短于阈值的凭证形状字面量，也没测过把阈值下调会放出多少存量红 ⇒ 不得读成"长度盲区已量化"；同条探针是新建文件、不是本泳道热区文件，所以"若在真实文件上打这一刀会不会命中别的豁免"也未测。

**归因补记（15:35 读盘，本笔提交 `4176e599` 之后）：上面③结掉。** 上一趟已完成的 `user-server-ci`（run `35696270591`，headSha `5f76e471`，并行泳道的 SHA）里，`Unit tests -race (user-server service)` 的 `--log-failed` 整份存 `/tmp/bm16_ci_race_5f76.log`（9472 行），`--- FAIL` 去重后**只有两条**：`TestD12_NoNewLegacyKVDirectQuery (0.07s)`（对方那枚注释假阳）与 `TestM01_QQFetchUsesRealAttachmentURLAndBytes (0.14s)`（本泳道已修的这条），`WARNING: DATA RACE` 4 条（写点 `:340`、读点 `:347`/`:351`/`:354`/`:357`），包级 `FAIL[Tab]hivemtk-user/internal/service[Tab]389.105s`（与 `a94bf639` 那趟的 366.893s、本机的 1052s 并排 ⇒ [[project-go-test-suite-timing]] 的"三处读数差 1～3 倍"第三次成立；顺带一次取证坑：`grep "FAIL hivemtk-user"` 用空格匹配恒零命中，Go 打的是 **Tab**，BRE 里 `[ \t]` 也不解释 `\t`，要用 `[[:space:]]`）。第 2 段归的 ③（`TestFallbackVersionResolvesDBHandleSynchronously` 的 `SQLSTATE 53300`）与 ④（`TestAbExperiment_LogExposureFireAndForget` 的 `DroppedCount`）在这一趟里**连一行都没出现**（`grep -c` ＝ 0，非"出现但绿"）⇒ 与"本机 4/4 绿"的反证方向一致，判 CI 环境/负载态成立，不必改任何用例。**两点收窄**：其一，这仍是"另一份字节、另一趟 run"的对照，不是把我这笔修复推上去后同一 run 的复跑，故不能读成"CI 从此只剩 D12"；其二，同趟 `Coverage (user-server)` 与 `ESLint (user-web)` 仍 failure（前者自 D12，后者属 §23.14 名册那两条老红），本泳道不碰对方热区——`git show --stat 4176e599 | grep -c "user-web\|browser_automation"` ＝ **0**，对 `quote*`/`config_param_guard_test.go` 的命中数同为 0。

**归因补记二（16:55 读盘）：上面那句"③④ 判 CI 环境/负载态成立，不必改任何用例"里 ④ 判错，须整条收回；③ 维持。** 触发收回的是本泳道随后那趟整包 `-race`（活树，产物 `/tmp/bm17_svc_race.log`，末行 `FAIL hivemtk-user/internal/service 1077.791s`）：`--- FAIL` 顶层计数 **1**，正是 ④ —— 红因 `ab_experiment_test.go:50: DroppedCount = 1, want 3（buffer=2，满则丢弃）`（该行号属**改前**字节；修后断言在 `:56`）。⇒ "CI 才红"这个前提当场不成立：它在本机、在非 CI 机器上就红。**当初那枚反证的样本量根本不够**，这是本段真正的账：第 2 段用的是"干净克隆 `-race -count=4` ⇒ 4/4 绿"，而下面第 3 段量出单轮红概率 p≈0.215 ⇒ 4 轮全绿的概率 `0.785^4 ≈ 0.38`，即**十次里有六次会给出"看不出问题"的读数**。口径：偶发红不能用小样本反证判成"环境态"，要么把概率量出来再比，要么把断言改成与时序无关（本批走的是后者）。**③ 维持原判**（`TestFallbackVersionResolvesDBHandleSynchronously` 的 `SQLSTATE 53300 = sorry, too many clients already` 是 CI 机上 PG 连接数打满）：本泳道随后三轮整包跑（活树 1077s、HEAD 干净克隆 984s、修复后克隆）里它**一次都没红**，而那三趟都是"探针真跑、真连库"的形状（顶层 PASS 3919～3943，见 §23.18 第 1 段的对照表），不是没跑。另记一次自己踩到的同类环境红，免得后人把它读成判据：第一次在克隆里跑整包时漏 export `POSTGRES_TEST_PASSWORD` ⇒ `顶层 FAIL=2252`、红因逐条同为 `failed SASL auth … 28P01`、`ok` 只走 30.760s（`/tmp/r41_svc_race.log`），这正是第 4 段"红得快＋红因同一条＝门没开"的第四次复发；补 env 后同一棵树重跑即正常（`/tmp/r41_svc_race2.log`）。补一句机制：`.env` 里的键叫 `POSTGRES_PASSWORD`、测试读的是 `POSTGRES_TEST_PASSWORD`，且 `testdb.go` 的提示行写明"密码不回落到 `.env`（避免把密钥写进代码）"⇒ 克隆/新 shell 里必须显式导出，`cp .env` 过去不解决问题。

### 23.18 批M-11 后续：整包 `-race` 换到"只含已提交字节"的树上仍报两条竞争，读方是两个不同的 spawn 点，于是锁加在了 accessor 上（2026-09-22）

**来路。** §23.17 未覆盖面⑤欠的是"提交后在只含已提交字节的干净克隆上再跑整包 `-race`"。那趟跑完没把"零 race"这个假设结掉，反而把它推翻：两朵 race 的读方分属两个异步 spawn 点，其中一个不是任何探针盯过的形状，另一个**根本挪不动**。本段记两层的修法、四格 A/B、锁自己的回归腿，与三处必须订正的计数口径。

**1 · HEAD 干净克隆整包 `-race` 的读数（未覆盖面⑤ 由此结掉，结出的不是"干净"）。** 计数一律按**顶层用例**（`grep -c '^--- PASS'`）；同一份日志不锚行首的计数会高 20%+（子测试也打 `--- PASS`），两个数别混。

| 趟 | 对象树 | 顶层 PASS / FAIL / SKIP | `WARNING: DATA RACE` | 包级读数 |
| --- | --- | --- | --- | --- |
| `/tmp/bm16_head_race.log` | `/tmp/bm16_head`＝`clone --shared` 到 `4176e599`，`git status --porcelain` 0 行（**纯已提交字节**） | 3919 / **3** / 3 | **2** | `FAIL hivemtk-user/internal/service 984.693s`、rc=1、起跑时 load `{10.67 16.56 17.24}` |
| `/tmp/bm17_svc_race.log` | 活树（含本泳道未提交四份 + 并行在飞字节） | 3943 / 1 / 3 | **0** | `FAIL hivemtk-user/internal/service 1077.791s`、rc=1 |

`4176e599` 那趟的三条红：D12（对方那枚注释假阳，§23.17 第 12 段）＋ `TestCreateSession_AllowDifferentPlatform` ＋ `TestCreateSession_AnonymousUser`，后两条红因逐字同为 `testing.go:1712: race detected during execution of test` ⇒ **红的是 victim，不是过错方**（与 §23.17 第 4 段那条"panic 替在跑的腿补印"同理，过错方在别的协程里）。两趟顶层 PASS 差 24（3919→3943）＝本批新增探针 1 条 + 并行泳道在飞/后入库的用例 ⇒ 跨树全量计数不可直接比（[[feedback-mutation-battery-hygiene]]），可比的只有 `--- FAIL` 名单。

**2 · 两条 race 读方不同、写方相同（同一地址 `0x000105dc8a38`）。** 行号属 `4176e599` 那份字节（race 报告打在克隆里，前缀 `/tmp/bm16_head/`）。
- **A**：写方 goroutine 4464 = `db.SetTestDB()` ← `setupBlacklistServiceTestDB(customer_session_blacklist_test.go:24)` ← 用例 `:347`；读方 goroutine 4463 = `repository.NewAutomationRuleRepository(session_chain_repo.go:101)` ← `NewRuleEngineService(session_chain.go:195)` ← `NewRuleEngineServiceFromGlobal(:202)` ← **`DispatchSessionEventAsync.func1(customer_session.go:463)`**，协程由 `:455` 起。
- **B**：写方同一 helper、由用例 `:380` 调；读方 goroutine 4475 = `repository.NewSystemConfigKVRepository(system_config_kv.go:35)` ← `(*OfficeHoursService).GetConfig(office_hours.go:45)` ← `IsWithinOfficeHours(:81)` ← `AwayMessageFor(:97)` ← `SendAwayReplyIfClosed(:107)` ← **`MaybeSendAwayReply.func1(customer_service_plus.go:487)`**，协程由 `:479` 起，上游是 `CreateSession(customer_session.go:132)`。

⇒ 两帧集合的交集只有一件事：**在 fire-and-forget 的异步体里 new 仓储**。这条读数是"只修 A 必剩 B"的直接证据，也是本段决定上锁的第一条理由。

**3 · 站点级修法（`customer_session.go:454-472`）：把引擎构造挪到 spawning 之前。** 与仓库先例逐字同形（`24546eab` 立的口径：`session_chain.go:36-55` 的 `TriggerCSATOnClose`、`objection_handler.go:393-423` 的 `fallbackVersionOf`）。A 站点在异步体里把句柄读走**十余次**（`NewAutomationRuleRepository` ＋ `NewCustomerServicePlusService` 名下 8 个子仓储 ＋ `NewEmailServiceAuto` 的账号仓储 ＋ `NewMessageHubService`）——"十余次"按调用图数到第二层，不是逐帧穷举，故写"余"不写确数。

**4 · 类级修法（`db.go:16-25`、`86-88`、`91-101`）：`DB` 外面罩一层 `sync.RWMutex`，收口在 accessor。** 三条理由：① B 挪不动（第 8 条）；② cron 的 tick 体本身就是"协程已经起来了再取句柄"（`internal/pkg/cron/cron.go:154-160` 的 `service.NewPasswordResetService(db.GetDB())` 那一类），没有"spawning 之前"可挪；③ 广度：生产码里 `X.GetDB(` 有 **375 处 / 200 个文件**（口径见第 7 条），逐点挪既不可行也不可持续——新写一个构造点就会再造一次同形 race。锁的形状：读走 `RLock`（多读并发不互斥），写只有 `InitDB` 尾与 `SetTestDB` 两处、都在 `Lock` 下。

**5 · 四格 A/B：两层修法各自独立充分，探针各自有牙。**（对象都是 `/tmp/bm17_*` 那批日志，活树）

| 格 | 字节 | `-race` 读数 | 判 |
| --- | --- | --- | --- |
| 只加探针（产码未动） | 探针 300 轮回写 × 无挪无锁 | `WARNING: DATA RACE` **3**、`--- FAIL: TestSessionEventDispatchResolvesDBHandleSynchronously (2.47s)`（`bm17_probe_red.log`） | 偶发窗口被撑成必现 ⇒ 探针能当门 |
| 探针 + 站点修法 | engine 挪到 spawn 前，无锁 | 5 条用例 ×4 轮：顶层 `PASS=20 FAIL=0 RACE=0`、`ok 21.138s`（`bm17_hoist_green.log`） | A 消除 |
| 站点修法的牙 | 把 engine 再挪回异步体（上一格的逆刀），仍无锁 | `--- FAIL` 1、`DATA RACE` 2、包级 `FAIL 4.505s`（`bm17_teeth_inline.log`） | 第二格不是"跑过了而已" |
| 只加锁（同一把反向刀仍在） | 反向刀 + `dbMu`，不挪 | `rc=0`、`--- PASS (3.53s)`、`RACE=0`、`ok 6.250s`（`bm17_mutex_only.log`） | 锁**独立**足以杀 ⇒ 两层是冗余不是互相依赖 |

⇒ 站点修完仍留一把反向刀，因为"挪一句"这种改动最容易被后来人以"这句放哪儿不一样"为名挪回去；而第四格说明即使那天两层只剩一层，A 也拦得住。

**6 · 锁自己的回归腿，和它为什么必须存在（新文件 `internal/pkg/db/db_race_test.go`）。** 收口从站点上移到 accessor 之后，三枚站点探针的判据变成"异步体读句柄会不会撞"——异步体已经不读全局了 ⇒ **把 `dbMu` 删掉，那三枚全绿**：收口点自己没了守卫。补的那条直接钉 accessor：4 条写协程满速 `SetTestDB(probe)` ＋ 主协程 20_000 次 `GetDB()`，判据交给 `-race`；不连 PG、不碰业务表。**牙按最终字节重打**（不是转述修复当时那趟）：删掉 `GetDB`/`SetTestDB` 里的加锁两句（`dbMu.` 引用 6 行→2 行，写盘前 `assert` 两处替换各命中 1 次）⇒ `rc=1`、顶层 `--- FAIL` 1、`DATA RACE` 1（`/tmp/r41_lock_teeth_final.log`）；`cp` 备份写回后 md5 与下刀前同＝`3de630490123ebb30c251622388e3e38`，恢复字节上 `-race -count=5` ⇒ `PASS=5 FAIL=0 RACE=0`、`ok 1.973s`（`/tmp/r41_lock_green5b.log`）。**自己交的一次学费**：第一版把断言写成"每次读到的都等于刚写进去的那个句柄"，而"写进去"由四条协程去兑现 ⇒ 第 0 次读就赶在它们之前（`-count=5` 第一轮 `got=0x0`、后四轮读到上一轮留下的指针，`/tmp/r41_lock_green5.log` 五条全红、`RACE=0`）。那不是竞争，是**断言自己造了一条不成立的时序**；修法是 spawn 之前先由本协程同步落一次，此后写方只写同一个值。属 [[feedback-async-and-global-state-tests]]"别断时序"同族，新轴是：**把用例判红的那趟也先归因到断言本身**——`RACE=0` 而 `FAIL=5` 的形状已经指出了答案。

**7 · 三处"数出来的数"要订正。** ① 本批注释与上一段草稿里引过的"**372 处 / 195 个文件**"（名单 `/tmp/bm17_getdb_sites.txt`）出自一个临时扫描器，过滤条件已不可复现 ⇒ **作废**，换成可复现口径：`grep -rn --include='*.go' -E '[a-zA-Z0-9_]+\.GetDB\(' user-server` 去掉 `*_test.go` 再去掉整行注释 ⇒ **375 处 / 200 个文件**；同一条不排测试、不剥注释 ⇒ 709 处 / 253 文件。三道门各数各的：`scripts/check-architecture.sh` 的 L4 那条自己印的是"存量 **39 处 / 22 个文件**"（口径＝`internal/service/` 且不含 `*_test.go`），与 375/200 不矛盾，**别拿一个数去对另一道门**。② "包外对 `DB` 的直读 0 处"必须按**符号**判而不是字符串：`grep -rnE '\bdb\.DB\b' user-server` 命中 3 行，逐行读后**没有一处是直读**——1 行是 `internal/pkg/testutil/testdb.go:133` 的注释，2 行是 `internal/migration/migrations/*_test.go` 里的 `db.DB()`（gorm 的 `(*gorm.DB).DB()` 方法，取 `*sql.DB`，与包级句柄同名不同物）；`pkg/db` 在别处还有**四个导入别名**（import 声明处数：`dbutil` 23／`dbUtil` 14／`_db` 146／`pdb` 1），按四个前缀各再扫 `\b<别名>\.DB\b` ⇒ **各 0 命中**（那一版草稿只核了 `dbutil`／`pdb` 两枚，`dbUtil`／`_db` 是本轮补核的）。⇒ 口径：[[feedback-prove-nonuse-before-deleting]] 那条"grep 未命中≠无用"反过来同样成立——**grep 命中≠是那个符号**，同名方法与导入别名两条都要算。③ 写方面（这一段在写入后又复算过一次，按"命中行是否注释"分流后才是终值）：全仓 `grep -rn --include='*.go' '\.SetTestDB('`＝**301 行 / 184 个文件**，剥掉 5 行整行注释 ⇒ **296 代码位 / 180 个文件**，且代码位**全部落在 `*_test.go`**（非测试文件里 0 处代码位）⇒ **生产路径没有任何一处写它**；命中的 4 个非测试文件全是注释行（`chat_visitor.go:53` 旧、`session_chain.go:41` 与 `customer_session.go:457` 是先例与本批写的 race 说明、`db.go:17` 是本批自己那段注释）。前缀分布（代码位）：`db.` 212／`dbutil.` 70／`dbUtil.` 10／`pdb.` 2／`_db.` 2＝296 ⇒ **四个导入别名（`dbutil` 23 处声明／`dbUtil` 14／`_db` 146／`pdb` 1，均已在源码里核到 import 行）连本名一起算**，只按 `db\.SetTestDB(` 扫会漏 84 处。草稿里那版"300 处／剥 4 处注释／183 个文件"是本批自己那行注释加进来之前的读数（183＝当时的**命中文件数**、184＝今天同一个数，代码位数 296 不变）——一枚会因为写注释而漂移的数字不该写进代码注释里，故 `db.go` 文件头最终取的是"296 处／180 个文件（全在 `*_test.go`）"这一版稳定表述。`internal/service` 单包 106 处 `db.SetTestDB(`、40 处 `go func(`（`grep -rho … internal/service/*_test.go | wc -l`，含注释提及），探针文件头改成了"100+／40"的下界表述，同理。

**8 · 第四站点（B）不补站点探针的决断与理由。** `MaybeSendAwayReply` 的"挪"看着更简单——把 `GetOfficeHoursService()` 提到 `go func()` 之前——但它治不了 B：读全局的是 `GetConfig` 里**每次调用都 new 一次**的 `repository.NewSystemConfigKVRepository()`（`office_hours.go:45`），不是单例构造那一次。真要挪就得把仓储收进 `OfficeHoursService` 字段，而那是 `sync.Once` 的进程级单例 ⇒ **第一个跑进这条腿的用例把自己的测试库句柄冻进单例**，之后所有用例读到的是别人的库——比 race 更糟（race 偶发、冻句柄必现且静默）。所以 B 的守卫就是锁本身，腿就是第 6 条那条 accessor 腿（它不依赖任何业务形状）。⇒ 口径：**站点探针不是越多越好；收口点从"站点"上移到"accessor"之后，守卫也得跟着上移，否则得到的是 N 条互等对方失效的重复腿。** 这一条也写进了 `async_db_handle_probe_test.go` 第三枚探针的注释尾，免得下一个人在那儿再补一条同形腿。

**9 · 代价是量出来的（一次性目录 `/tmp/bm17_lockbench`，Apple M1、8 核、`-benchtime=200000x -benchmem`，产物 `/tmp/r41_lockbench.log`）。** 裸指针读 `0.3396 ns/op`；`RLock＋读＋RUnlock` 无争用 `16.41 ns/op`、`0 B/op`；再加 4 条满速写协程 `426.5 ns/op`、`4 B/op`。⇒ 取一次句柄多花 ~16ns，而每个取句柄的点后面跟的是一次 PG 往返（µs～ms 级），比值 10²～10⁵；"每请求几十次构造"合计在亚微秒级。426ns 那一档是**人造最坏**（生产里写方只有启动时那一次 `InitDB`，`SetTestDB` 只存在于测试），真实写频下的形状未测，见第 11 条。

**10 · 最终字节上的复跑：两朵 race 归零，红只剩对方那一条。** 计数一律按顶层用例（`grep -c '^--- PASS'`）。

| 趟 | 对象树 / 字节 | 顶层 PASS / FAIL / SKIP | `WARNING: DATA RACE` | 包级读数 |
| --- | --- | --- | --- | --- |
| `/tmp/r41_svc_race2.log` | `/tmp/r41_clone`＝`clone --shared` 到 `669169b9` ＋ **只**覆写本泳道四份（该克隆里 `git status --porcelain` 就这 4 行 `M`，`git diff --stat HEAD` ＝ 66 增 8 删）；`internal/service` 整包 `-race -test.v -count=1 -timeout 40m` | 3924 / **1** / 3 | **0** | `FAIL hivemtk-user/internal/service 1128.621s`、`go_test_rc=1`（这趟起跑时没取 load 样本） |
| `/tmp/r41_pkgdb_race_final.log` | 活树，锁与 accessor 腿已在（`db.go` 当时 md5 `3de63049…`） | 58 / 0 / 0 | **0** | `ok hivemtk-user/internal/pkg/db 231.200s`、rc=0 |
| `/tmp/r41_pkgdb_race_final4.log` | 活树**最终字节**：`db.go` md5 `7148e830…`（第 7 条那两处口径订正之后的版本），起跑前取此值、跑完复测同值 ⇒ 这趟测的就是入库那份字节 | 58 / 0 / 0 | **0** | `ok … 339.386s`、rc=0 |

中间两趟（`final2.log` 无 `-test.v`、`final3.log`）各是"注释又改了一行"之后的复跑，`ok 318.662s`／`ok 313.675s` 均 rc=0，被第 4 趟取代，这里只算过程记录、不当读数。四条归因与边界：

- **"race 2→0"这个对照成立，靠的是逐笔核过区间。** 第 1 趟与第 1 段那趟（`4176e599` 纯已提交字节、`RACE=2`）之间隔着并行泳道六笔（`08578d1f`…`669169b9`），`git log --name-only --format='%h %s' 4176e599..669169b9 -- user-server/internal/service user-server/internal/pkg/db user-server/internal/repository` 逐笔读下来只碰到 `migrate.go`、`password_reset.go` 与三份新测试文件，**没有一笔动过两条 race 的任何一帧**（读方 `customer_session.go:463`／`customer_service_plus.go:487`，写方 `customer_session_blacklist_test.go`）⇒ 差值能归到本泳道这两层修法上。同理，`669169b9..HEAD`（`a751c541`／`0b23b3f8`／`e3af05d0`）对本泳道五份 Go 文件的命中数＝0。
- **唯一那条红仍属对方**：`--- FAIL: TestD12_NoNewLegacyKVDirectQuery (0.33s)`，红因逐字 `config_param_guard_test.go:77: 发现新增遗留 KV 直查（新配置请走 ConfigParamService）: [../service/quote.go]` ⇒ 与 §23.17 第 12 段同一枚假阳；命中文件 `quote.go` 不属本泳道，而顶点上刚落的 `e3af05d0`（折扣二次审批）改的正是那片热区 ⇒ 那条红的名单还会继续漂，本泳道不代改（[[project-audit-backlog-2026-09]] 的"D12 属公开既有红勿代改"）。
- **clone 那趟与最终字节只差注释，是核出来的不是推出来的**：`db.go`（clone `15568193…`）与探针文件（clone `54c73c87…`）各自 `grep -v '^[[:space:]]*//'` 之后与活树**逐位相同**，另两份（`customer_session.go` `2cb8dbaf…`、`ab_experiment_test.go` `b366019b…`）整文件相同 ⇒ 那趟的"零 race"对最终字节的**代码**成立。但严格说"在最终字节上再跑一遍 1128s 的 service 整包"没做，记进第 11 条。
- **用时又添两个读数**：service 整包 1128.621s（同树第 1 段是 984.693s／1077.791s），pkg/db 同一份代码 231→319→314→339s（load 24～32）⇒ [[project-go-test-suite-timing]] 的"三处读数差 1～3 倍"这一批又复现四次，任何写进脚本的默认 `-timeout` 都按"跑不完就是没测"处理（批M-10 起的 `-timeout 40m` 口径继续有效）。

**11 · 本段未覆盖面（下一位从这里接手）。** ① **锁的保护边界**：`internal/pkg/db/migrate.go` 里 **19 处**裸 `DB` 读写（`grep -nE '(^|[^.[:alnum:]_])DB([^_a-zA-Z]|$)' migrate.go` 剥整行注释后现算）不经这把锁。今天不出事只因为唯一的生产调用点是 `cmd/api/main.go:125`、跑在起协程之前（`cmd/geo-run/main.go:59` 那个 `db.AutoMigrate(` 是本地 gorm 变量、不是包级句柄，其余命中全在 `*_test.go` 的同协程路径）。**没有一条腿钉住"AutoMigrate 不许被挪进协程"**——`TestBatchM_MigrateIsWiredIntoAPILifecycle`（S15）钉的是"`main.go` 顶层那两行的存在、次数与先后"，两回事。② 本机 `-race` 只跑了两格；全仓 **125 个含 `*_test.go` 的包**，CI 的 core 片（`go list ./...` 去掉 `internal/service`）带 `-p 1 -short -race` 全覆盖，本批没在本地整片复跑 ⇒ 别的包里的同类形状仍只有 CI 会报。③ 三条新腿都不看 `testing.Short()`，CI 的 `-short` 不会把它们跳掉（这点是好的），但 CI 的 `-p 1` 前提本机没复刻（本机整包天然串行）。④ **站点 B 无专属腿**、cron tick 体也无站点腿，守卫只有 accessor 锁：若有人拆掉 `GetOfficeHoursService` 的 `sync.Once` 或把仓储收进结构体字段，B 的形状变了而锁还在 ⇒ **没有用例会同红**。⑤ 反过来，那三枚站点探针在锁被删时全绿（第 6 条实测），防删锁的只有 accessor 一条腿；而"产码里绕过 `GetDB` 直读包级 `DB`"这一面只有第 7 条那枚 grep 口径（当前 0 处），**没有门**。⑥ 锁的代价只测了单点无争用与 4 写协程人造最坏（第 9 条），未测"一次请求取句柄 N 次"的累积，也没有 P99。⑦ **CI 侧零证据**：本批六份文件全未推，未推字节对 CI 隐形（只有影子克隆门看得见，[[project-audit-backlog-2026-09]]），#63（解引用门接进 CI、`paths:` 含脚本自身与基线）仍未做。⑧ D12 常红照旧挂在顶点。⑨ 375/200 那批 `GetDB` 读点里"哪些落在协程内"仍**未逐点枚举**——本批处理的是 `-race` 真报出来的四处，其余按"锁兜住"结案，那是推理不是清单。

### 23.19 批M-11 后续 2：把一轮性取证脚本转成常驻电池，第一次全族跑逼出三处驱动自己的错，顺带照出「短信受理成功」那条分支整片没腿（2026-09-23）

**来路。** §23.18 第 11 段欠的是证据形态，不是新缺陷：本泳道那四处改动（三家接口域注入／测试库句柄池上界／TG 建号两条外部腿／飞书媒体三条腿替身）当时只有 `/tmp/r44_measure/` 里一轮性的普查读数，任务 #75 的代号叫"70 族"（旧注释里的 `70D`／`70E` 即那批一次性格名）。本段把它落成随批入库的常驻件 `scripts/mut_egress_pool_r30.py`，按"每处承诺至少一格、且格要能被反向证伪"重排成 **S／P／T／F 四族 29 个行为格 + 1 个 race 格**，旧别名在注释里已改指 `S2 格`／`S3 格`（`grep -rn "变异格 70" user-server/internal/` 现算 0 命中）。跑出来最值钱的不是"全杀"，是它先照出一整片没腿的面（第 3 段）。

**1 · 五趟读数（产物落在 `docs/superpowers/specs/ledger/logs/R30/<tag>/`，tag 不覆盖）。**

| 趟 | 范围 | 控制组（现测 settled／skip／CONNECT） | 判定 |
| --- | --- | --- | --- |
| `full-1` | 全族 21 格（当时只推到 21 格，race 族含在内） | SMS 8／POOL 1／TG 1／F17 1，四组全 0 红 0 SKIP 零 CONNECT | **16 杀／5 未杀**：`Q-on` 分母断言错、S4/S5 `CONN-MISMATCH`、`F2 BROKEN=行为变了`、`Q-off` 被前者连带 SKIPPED |
| `succ-1` | 新格 10 格（`--only`，非全族） | SMS **12**／POOL 1／TG 1／F17 1，同样零 CONNECT | **5 杀／5 未杀**：S13/S14/S15 `BROKEN=红因而非预期`、S17/S19 `BUILD-BROKEN` |
| `succ-2` | 修后那 5 格 | 同上 | **5 格全杀**，S17 红三条首次成功腿、S19 只红重发腿（两站点分开了） |
| `full-2` | 全族 **30** 格（含 race A/B 两相） | 同上，四组零 CONNECT | **逐格被杀，无存活，无 CONN/RAN/PATCH/BUILD 异常**，rc=0 |
| `full-3` | S 族 21 格，绑定到最终字节重跑（`sms_success_test.go` md5 `605334c6…`、`sms.go` `500262a9…`） | 同上 | **21 格全杀**，逐格读数与 `full-2` 逐字一致 |

`full-2` 的 race 两相：A 相（带锁、`-count=10`）`rc=0 settled=10 红名=— DATA RACE 块=0`；B 相（摘掉 `quote_test.go` 里那三行 `mu.Lock()/Unlock()`）`rc=1`、**2 朵**竞争块，两朵都归到 `--- FAIL: TestQuoteService_ReviseConcurrentSecondLoser` 且报告里含本家文件名。**两相的读数都不靠驱动 stdout**：`race_A_locked.log` 里 `grep -c -- "--- PASS: TestQuoteService_ReviseConcurrentSecondLoser"` = **10**、`grep -c "DATA RACE"` = **0**；`race_B_Q-off.log` 里 `DATA RACE` = **2**、该腿的 `--- FAIL` = **1**、`quote_test.go` 命中 16 行——这两份 log 现在随批在库内（第 1-补 段），所以"锁有效／摘锁就炸"是可复算的而不是转述。

判据为什么仍**不许**锚在标识符上，这里要订正一条说得太死的老口径（原先只登在长期记忆 [[feedback-mutation-battery-hygiene]] 里，§23.18 正文并没有它，本文上一版把它误引成"§23.18 教训"）：`-race` 的 `Write at 0x00c000190968 by goroutine 144:` 那一行确实只有地址、不印被竞争的字段名，**但它会印栈帧的函数符号**，本腿的符号恰好是 `hivemtk-user/internal/service.(*qsScripts).ActiveQuoteScript()` ⇒ 在这份产物上 `grep qsScripts` 命中 **4** 行、"认变量名"偶然可满足。不可依赖的理由有两条：符号身份来自**帧**而非被竞争的那个字段，竞争点挪进别的函数就没这个标识符；且摘锁后的单行 getter 会被内联、帧本身可以不出现（§23.18 那批取证的实测）。常驻件的"判据形状"段已按这条改成"口径，不是不可能命中"的写法。

**1-补 · 上一段那句"产物在 …"原本在仓库里不成立，这一档是本段写完自查时才修上的。** 拿引用回核磁盘：`git check-ignore -v docs/.../R30/full-2/S1.log` 退 **rc=0 + `.gitignore:23:logs/`** ⇒ 六趟共 **117** 份逐格产物全被库外的忽略规则挡着，`git ls-files docs/superpowers/specs/ledger/` 命中 **0**：文档里的读数在这台开发机上找得回，在克隆/CI 里找不回——正是本段要消灭的那种"只有作者看得见"的证据形态（`logs/` 与 `*.log` 是运行时日志规则，误伤了审计取证目录）。处置＝给 `.gitignore` 加例外，而这条例外**改过两版，第一版是反向测出来的过宽**：先写的是两行（`!docs/.../ledger/logs/` + `!docs/.../logs/**/*.log`），拿"别人的目录会不会被一起放进来"去问它——临时造 `logs/R99sim/{a.txt,b.log,deep/c.js.orig}`，三份**全部**变成可跟踪；而共享树同一棵 `docs/superpowers/specs/ledger/logs/` 下如今躺着 **17 个轮次目录、283 份产物、12 MB**（其中 26 份非 `.log`，含别的泳道留下的 `bak/*.js.orig` 残本与 .md/.txt 记要）⇒ 那版例外会把**别人批次的字节**一并送进下一个 `git add`，而这批的提交推送归另一条并行线（[[project-audit-backlog-2026-09]] 批M-7 口径）。终版因此按"轮次目录"收窄成六行：放行 `logs/` 以便下降（父目录被排除时对其内任何 `!` 都不生效）→ 用 `logs/*` 把直接子项整体挡回 → 只放开 `R30/`、`R28/` 两棵及其内 `**/*.log`。**四向都现测**：① 本批 R30 下 **117/117** 份进待跟踪集；② 上面那枚 R99sim 探针（.txt/.log/深一层 .orig）**0** 份可跟踪；③ R28 探针 `.log` 1/1 可跟踪（两枚探针连同目录都清掉了，`ls logs/` 现只剩 `R30`）；④ 仓库根 `logs/x.log` 与 `user-server/y.log` 仍分别由 `.gitignore:23` 和 `user-server/.gitignore:20` 挡住 ⇒ 运行时日志面一字未动。泄露面在放行前扫过两棵树：本批 117 份与共享树 283 份里，`password=`、`postgres(ql)://`、`AKIA…|eyJ…|ghp_…` 三类命中均 **0**（`testdb.go` 的失败路径本就过 `maskedDSN`），`user=` 出现 3442 处但取值全是 `user=admin`(3376)／`user=b19d` 这类**用户名或格标签**而不是凭证；克隆树里没有 `.env`（只有 `.env-example`）⇒ 无口令值可泄。白名单口径留一句：放开的是"这两棵轮次目录"，目录内非 .log 也会入库（现数 R30 里非 .log = **0**，两艘电池都不往 logdir 写 `bak/`），别的轮次要跟进应由那一轮自己改这份配置，不由本批代签。另有第六趟 `20260923-022537`（驱动定稿前的单格验形，只跑 `--only P1`，5 份）：`P1.log` 红因逐字为 `测试库句柄的 MaxOpenConns = 0，期望 32（0 表示根本没设上界，database/sql 里 0＝不限）`，四枚控制组同趟留档——它与 `full-1`/`full-2` 的 P1 格同判，留作"这一格第一次被杀掉"的现场。

**1-再补 · 本段自己犯了一次驱动文档里写着的那条错，而且犯了两次。** 上面几处编辑先给常驻件补了"产物可找回性"的前提，随后第 1-补 段那条"订正判据口径"的编辑又动过它一回（race 那段的措辞就是被这次改掉的）⇒ 两次之间我一度把 md5 记成 `c650fcb8a03e9c968f24e0a49434d1ac`，而它已是中间态。**入库字节以最后一次编辑后现算为准：`md5 -q scripts/mut_egress_pool_r30.py` = `2426aac7a81648a03ba6f53d569653ba`**，`--check` 在这一版上重跑仍 `preflight：29 个行为格 + 1 个 race 格，0 处静态问题`、rc=0。这正是 [[feedback-verify-secondhand-review-claims]] 说的"文档里的 file:line 在最后一次源码编辑后复算"，只不过这次编辑者是我自己；本段表格 `full-3` 行引用的另两枚绑定不受影响，磁盘现值逐字为 `sms_success_test.go` `605334c6fa7075412cf6df63288a803d`、`sms.go` `500262a90c58e580e9493f18988ba2b5`（另 `testdb.go` `dfc201cb3594c71882105e4a97398dec`）⇒ 成功腿的读数仍绑定有效。**口径值得单独记**：驱动自己"每刀还原后比 md5"的判据，反过来同样适用于"文档引用了哪个 md5"——引用 md5 的段落必须晚于对那个文件的最后一次编辑；而常驻件不参与任何格的注码面，所以它变动只失配"身份"这一项，不需要整族复跑（这一点成立的前提也写下来：注码面按锚点定位、锚点没动，改的是注释文字）。

**2 · 出站面：控制组零真出站这条，从"人肉普查"换成了机器判。** 电池自带只记不走的 CONNECT 代理（回 502、不建隧道），四枚控制组过滤器必须 **0 条** CONNECT，`full-1`／`full-2`／`succ-*` 每趟都满足 ⇒ "测试不再打穿外部世界"从此有了一条会红的腿。反向格的 CONNECT 主机名是**判据的一等公民**（缺期望主机 ⇒ `CONN-BROKEN`，出现意外主机 ⇒ `CONN-MISMATCH`）：`full-2` 总表 28 条全部落在反向格内——`dysmsapi.aliyuncs.com` 20（S1 18 + S4 2）、`sms.tencentcloudapi.com` 4（S2 2 + S5 2）、`smsapi.cn-north-4.boe-business.huaweicloud.com` 2（S3）、`api.telegram.org` 2（T1/T2 各 1）。逐格计数会随用例名单漂（`full-1`→`full-2`：S1 12→18、S2/S3 各 1→2，差值恰是新加的成功腿把"改回官方域"这条路多走一遍），所以**判据只比主机名集合、不比次数**，次数只作旁证（同"共享树下控制组会漂"那条口径，[[project-audit-backlog-2026-09]] 第三十八轮）。

**3 · 本轮真正的产出是九格新格（S13–S21）与四条新腿：短信「受理成功」分支此前整片没腿。** 来路是 `full-1` 放刀前的一次自检——"若有人把成功判据摘掉会红吗"，答不出来：`sms_test.go` 那六条桩腿**全回业务错**（走 `err != nil` 那一路），于是 `sms.go:296-297`／`:587-588` 的 `record.SendTime = &sentTime` 与 `record.Status = "sent"` 两句话在整套用例里**从未被执行过** ⇒ 台账上"成功"这两列、以及"成功行不许挂失败原因"这一条，删掉都没人红。补 `internal/service/sms_success_test.go`（新文件，未跟踪→已随批入 `git status`）四条腿：三家各一条首次成功（回包形状按各家判据写：`Code=="OK"` / `Response.Error.Code!=""` / `code=="000000"`）+ 一条重发成功（夹具带上一轮的 `ErrorCode/ErrorMsg`，用来额外钉 `:563-564` 那两行清空）。

三条读数是这九格换来的，不是先验写出来的：

- **同一句话两个站点 ⇒ 一刀两站必假全杀。** `record.Status = "sent"` 与 `record.SendTime = &sentTime` 在 `sms.go` 里**各出现两次**、逐字相同（首次发送 `:296-297`、重发 `:587-588`）。单行锚点命中 2 次 ⇒ `PATCH-BROKEN`；只注一处（另一处仍在）⇒ 红名少一半却报"全杀"。四格按站点拆开，锚点各带一侧独有的错误包装行（`send sms failed: %w` / `resend sms failed: %w`）定位 ⇒ `full-2` 的 S17 红三条首次成功腿、S19 只红重发腿，互不遮蔽（口径：一处符号多处消费要逐格拆刀，[[feedback-mutation-battery-hygiene]]）。
- **`record.SendTime = nil` 不是"删掉这句"的合法写法。** succ-1 里 S17/S19 报 `BUILD-BROKEN`：`sentTime` 别处不再被引用 ⇒ `declared and not used`，拿到的是编译红而不是"这条性质没了"。改成把整句包进 `if false { … }`（词法上仍是使用点、行为上等同于删句），两格立刻可判。
- **红名对、红因不是这一格的分支。** S13/S14/S15 把成功判据写成 `if false`，三格红名与推演完全一致，但输出里**没有**"失败原因没落账"——腿先 `t.Fatalf` 在"官方回业务错时应上抛"那一条上，根本走不到台账断言。期望字面量因此拆成两条常量（`R_NO_LAND` 给 S1/S2/S3/S4/S5、`R_NOT_RAISED` 给 S13–S15）。这是"红必须读红因"的又一次复发，且这次是被**自己写的期望**骗过去的。

归属口径记一句：S20/S21（各摘一行清空）的红因是 `assertSentLedger` 第 ③ 条"成功行不该带失败原因"，这条断言三枚首次成功腿也共用——分辨靠**测试名**而非红因文本：那三条腿的夹具行本来干净，③开火只能是成功路径自己写了码；只有重发腿带着上一轮的 `code/msg` 进格子。腿里因此刻意**没有**再写一条只属于重发的重复断言（写了两条同义的 `Errorf`，杀力不变、多一条要维护的红因）。

**4 · 三处驱动/期望自己的错，全部由读数逼出来（`full-1`/`succ-1`），不是推演出来的。**
- **race A 相的分母不是 1。** 那一相跑 `-count=10` ⇒ `settled=10` 才是健康读数；断言原先写死 1，于是把"锁有效、10 趟零竞争"判成"过滤器没匹配到用例"，还连带把 B 相打成 `SKIPPED=控制组不成立`——**判据自己制造一条假红、并顺手拆掉唯一能做对比的那一相**。改成 `settled != args.race_count` 后 A/B 都在 `full-2` 拿到读数。
- **S4/S5 的 CONNECT 期望写成"无出站"是推演错。** 把 tencent 那条腿改成读 `aliyunAPIURL`，没被测试覆写的那个字段就是官方域 ⇒ 请求**改道到另一家的官方网关**（实测 `dysmsapi` 2 次 / `tencentcloudapi` 2 次）。"改道"正是这一格要钉的失效形状 ⇒ 修的是期望，不是变异。这两个数（连同第 2 段那张 28 条的总表）不靠转述：每格 log 尾部自带 `===== CONNECT =====` 段，`awk '/===== CONNECT =====/{f=1;next} f&&/CONNECT/{print $3}' docs/superpowers/specs/ledger/logs/R30/full-2/S4.log | sort | uniq -c` 现读 = `2 dysmsapi.aliyuncs.com:443`，S5 同形 = `2 sms.tencentcloudapi.com:443`；五格逐字复算（S1 18/S2 2/S3 2/S4 2/S5 2）与总表 20/4/2/2 闭合，本段最后一次复核时做过，不是从上一版的表里抄的。
- **F2 原先不是行为不变的变异。** 只摘 `tenantTokenFn` 一条腿的引用、留 `mediaFetchFn/mediaStoreFn` 指向已不存在的局部变量 ⇒ `undefined:` 编译红，被 tally 成"静态格却把用例跑红了（红名=[]）"。三处引用一起改道、快照行才删得干净，改完 `full-2` 拿到 `gate rc=1 点名 internal/service/webhook_channel_feishu.go，用例 1 条全绿`。

**5 · 顺手加的一档 `--check`，以及它挡不住的那一类。** 五件磁盘上能核的事：锚点命中数＝1、红因字面量在某个用例文件里查得见、期望红名在该格过滤器的名单内、名单里的腿真有 `func` 定义、gate 格点名的文件存在。当前读数 `preflight：29 个行为格 + 1 个 race 格，0 处静态问题`。**边界要写明**：第二条只挡"字面量根本不存在"（抄错/漏字），挡不住 P1 那种"字面量在仓库里查得到、但不是本格该开火的那句"（`MaxOpenConnections` 确实在 `testdb_pool_test.go` 里，用例红因印的却是"测试库句柄的 MaxOpenConns"）⇒ 这一类只有真跑能照出来，**`--check` 绿不是"电池有牙"的证据**。

**6 · 门禁与本批字节的自洽（跑在最终字节上）。**
- `python3 scripts/check-async-global-read.py` ⇒ `OK … 项目根 /private/tmp/r45，扫 153 个 package，站点 0（= 基线合计 0）`，rc=0。
- `python3 scripts/check-seam-guard.py` ⇒ `OK … 登记 17 个全局，扫 2755 个 .go 文件 ⇒ accessor 之外 0 处访问`，rc=0。这里订正一处：**本文上一版把 2740 当现值抄了下来，那个数是在合并前的旧基座 `1bce38c3` 上量的**；本段第 8 段把影子树 `--ff-only` 推到 `2091cb07` 后两扇门都现重跑，`153 个 package／2755 个 .go` 是最终字节上的读数（上一版那句 `扫 153 个 package，站点 0` 恰好没变，因为它数的是 package 而非文件）。口径并入"读数必附测于哪一版"：门计数随顶点漂，写死数字的段落要在每次 `merge` 后重跑一次才允许留在文里。
- `gofmt -l` 对本批四份文件报一处（新文件里一段注释的续行折行），`gofmt -w` 后只剩该折行差异；因它动的是**没被任何格注码的** `sms_success_test.go`，为免"读数与入库字节差一行注释"这种账，S 族 21 格在最终字节上整族重跑（`full-3`）而非结转 `full-2`。`go vet ./internal/service/ ./internal/pkg/testutil/` rc=0。
- **漂移核验（本轮订正，原先两个 SHA 都是旧顶）**：本泳道基线 `2091cb07` 是共享树现测顶点 `b7c5258e`（测于 06:02，`git log --oneline -1`）的祖先，中间 4 笔（`3d441385`、`444e0234`、`79378eb9`、`b7c5258e`，全属并行泳道的 install.lock/并发迁移那两档），`git diff --name-only 2091cb07..b7c5258e` 与本批脏文件**交集 0**（06:26 在同一棵共享树上复测：并行面新增提交仍是这 4 笔、其改动 10 份文件，与本批 `git diff --name-only` 的 18 份 `comm -12` 交集仍为 0。这里顺手把数目订正过来：上一版写的是 17，漏的是 `scripts/mut_startup_hook_p702.py`——它在 06:05 才因本段末那条 ENV-BROKEN 修复变脏 ⇒ **"本批有几份脏文件"要在最后一次源码/脚本编辑之后数，不能在编辑之前数完再抄**）。**07:4x 在同一棵共享树上第三次复测**（写本段收口时）：顶点已漂到 `6b3380f7`（测于 07:40 的 `git log --oneline -1`；`aab2e733`/`b2d62f22`/`6b3380f7` 三笔是 #63 收口期间新落的，全在 `docs/superpowers/plans/` 与 install.lock 那一族），`git diff --name-only 2091cb07..6b3380f7` 现数 **11 份**，与本批 `git diff --name-only`(21) ∪ 未跟踪新增(7) 共 29 条路径做 `comm -12` ⇒ **交集仍为 0**，`git merge-base --is-ancestor 2091cb07 HEAD` 退 0 ⇒ 基线仍是祖先、提交窗口仍只需 fast-forward。这一行的价值不在"这次也是 0"，在**它每复测一次就证明一次"漂移核验"不是一次性动作**：同一批字节在四小时内被三次不同的顶撞过，若只在第一次数完就不再核，最后一次 `git add` 面对的是一条已经换了主人的树 ⇒ 提交窗口只需 fast-forward，无需语义对账；**"当前顶点"这种写法本身是引信**：同一天里它从 `442de55c` 漂到 `96e9fa81` 又漂到 `b7c5258e`，所以这一行今后只写"测于 HH:MM 的 `SHA`"，不写"当前"。
- 一格口径值得单独记：T3/F2 的 `run_gate` 是 `cwd=clone` 跑**已提交那一版**的门（常驻件自己在 `scripts/` 下、不在 `LANE_PATHS` 里，故不进覆盖表）。这是有意的——门的牙齿要用入库版证；但反过来说，若哪天在活树里未提交地改了门，这两格证的仍是旧门。

**6-补 · 把"取证落点在库外"这一族整个迁进仓库树：10 份常驻电池的复跑、一处新泄露面与三处自洞。** 第 7 段⑩ 那句"剩下的活＝把其余 7 份的落点迁进仓库树，并各自复跑一次"本轮做完，且做完后发现该段本身有三处读数已经过期（见下）。

- **落点普查的现值（`for f in scripts/mut_*.py` 逐份核 `LOGDIR`/`DEFAULT_LOGS` 行）**：常驻电池 **10 份**（不是⑩写的 9 份——`mut_startup_hook_p702.py` 是⑩写完之后加的，而⑩的"其余 7 份用 `tempfile.mkdtemp`/`Path("/tmp")`"同样过期：本轮迁完后，**10 份全部**把逐格产物写进 `docs/superpowers/specs/ledger/logs/<轮次>/<趟次戳>/`；`tempfile.mkdtemp` 如今只用于**私有作业/影子克隆目录**，那是工作副本不是证据，留在树外是对的）。`git status --porcelain -uno` 本段初稿于 06:2x 现数 18 改 + 5 增（18 = 上一版的 17 + 06:05 变脏的 `scripts/mut_startup_hook_p702.py`）（含 `scripts/redact.py`、`scripts/battlog.py` 两份新共用件），产物 **564 份（10 个轮次、33 个趟次目录）** 份、`find … | git check-ignore --stdin` 退 rc=1（**0 份仍在库外**，`.gitignore` 第 35–56 行的轮次白名单正好 10 轮成对 20 行，与磁盘上的 10 个轮次目录 1:1）。
- **同一句在 #63 做完后已过期，07:40 现数脏文件面、07:47 现数产物面（同一棵 lane 树）**：**21 改 + 7 增**（改方多出的三份全属 #63：`.github/workflows/user-server-ci.yml`、`.github/workflows/lint.yml`、`Makefile`；增方多出的两份是新增的常驻门 `scripts/check-ci-gate-paths.py` 与其反向验形件 `scripts/check-ci-gate-paths.test.sh`，两份都不在 `mut_*.py` 名单里，所以上一条那句"常驻电池 10 份"没被 #63 改动），产物 **572 份（11 个轮次、37 个趟次目录）**（`CIwiring/` 下现有四档：`073421` 反向格 + `074135`/`074449`/`074759` 三趟收口复跑，判读逐项相同，为何留三趟见 §23.20 6-补），`find … | git check-ignore --stdin` 仍退 rc=1、命中 **0** 行 ⇒ **0 份仍在库外**（`.gitignore` 第 35–58 行的轮次白名单现为 11 轮成对 22 行，与磁盘上的 11 个轮次目录 1:1），572 份**全部**是 `.log`（非 `.log` 现数 0）。带判定行的目录仍是 **22** 个 ⇒ 无判定行的目录从 11 变 **15**，多出的四档不是"漏挂 tee"，而是这一族根本没有电池驱动（`00-run.log` 由 `battlog.tee_to()` 写，门与 `make audit` 的判读行就地写在 `00-make-audit*.log` 与 `20-gitleaks-final.log` 里）。**这一条是上一条口径的第三次兑现，而且这一次是连着涨四轮**：566/34（07:40）→ 568/35（07:43）→ 570/36（07:45）→ 572/37（07:47），每一次都不是笔误，而是**上一趟"为了确认最终字节而跑门"自己又落了产物** ⇒ 结论要改写成一条排序约束而不是一句提醒：**"复跑取证"和"回灌计数"不能交替做，必须先把所有会产物的动作做完、只留最后一趟作终趟，然后一次性抄数**（本轮第一版没按这个顺序做，所以多烧了三趟、多写两次订正）。另：本段此后若再有人在此加产物，**照本节写法新开一行、写清新读数的时间戳，不要改本行**——本行的时间戳就是它的有效期。
- **迁移动力不是"方便"，是前几轮的读数在库里查无对证**：§23.18 登的"R28 那趟 27 格全杀 + SKIP 1"写在 `docs/superpowers/plans/2026-09-20-coverage-heavy-low-packages.md:3952`（**出处是这份计划文档，不是 §23.18 正文**——⑩里"它登在 §23.18 里"这句话指错了地方），而它当时的产物目录 `/tmp/r28_seam_battery_<ts>` 已经随重启蒸发。本轮 R28 在三棵不同条件下复跑三趟，其中 `20260923-040136` 读 **`结论：判 30 格（另有 1 格合并 SKIP），全杀 ⇒ True` + `树残留：无（全部还原且 md5 与开刀前一致）`** ⇒ **⑩那句"该电池的 27 格没有复跑"与"只有文档转述、没有产物"两条都已不成立**。30 vs 27 的差额是 `a9c2aea6` 那笔把 14 扇 test-only 写侧搬进 `_test.go` 之后新增的三格注册表面腿（`setter-prefix-unknown-file` / `-no-such-func` / `-bad-shape`，`mut_seam_guard_r28.py:467-476`），不是同名的格子被重复计数 ⇒ **数"某一族有几格"必须连"数于哪一版代码"一起记**（同一棵树同日两趟，一趟 `--only-gate` 读 28、一趟全族读 30，差的是范围不是牙）。
- **复跑逼出来的真缺陷（唯一一个判据变更）**：hub/R22 电池的 R4 格第一轮复跑 BROKEN——期望红名只有 `TestSetInboundMediaURLsMissingRowIsNotFoundNotError`，实测另外红到 `TestSetInboundMediaURLsScopesByAccount`。查红因不是连带打偏：该格摘的是 `errors.Is(err, gorm.ErrRecordNotFound) → (false, nil)` 这一支，而**同一支有两条腿在消费**（真·没有这一行；账号 2002 查 2001 的行，同一次 `Find` 同样回 `ErrRecordNotFound`，`message_hub_inbox_media_test.go:98`）⇒ 按 [[feedback-mutation-battery-hygiene]] 的"一处符号多处消费要逐格拆刀/改期望不改变异"口径**把期望集 widen 成两条**，R1（只红 L_SCOPE）的红集仍与它互异 ⇒ 上界没降级回下界。改后独立复跑两趟皆读 `判定：1/18 格（--cells R4），逐格被杀，无存活`。
- **新泄露面：夹具现算的令牌，不是真口令。** 产物落进仓库树后 `gitleaks detect --no-git --source docs/superpowers/specs/ledger/logs` 命中 4 处（全 `generic-api-key`），值是 `key=uid-rs-<n>-<nonce>`、`"_approval_token":"rt_<64hex>"` 这类**测试夹具**（"0 份真口令"是**规则集的读数**、不是人眼结论：终扫见下）。这类东西不能靠豁免表：夹具带 nonce ⇒ 本轮豁免下轮即失效；而项目规则禁 `paths` 型豁免 ⇒ 唯一出路是**落盘前**改命中面，即 `scripts/redact.py::scrub()`（标签含 key/token/secret… + `=`/`:` + ≥18 位 `[A-Za-z0-9._~-]` 值 ⇒ 收敛成 `前6字[len N]`）。同族坑：JSON 的 `"token":"值"` 里标签与 `:` 之间隔着**一枚闭合引号**，第一版只按 `key=裸值` 写 ⇒ 脱敏后仍剩 1 处。接法：每份驱动**唯一的日志写点**包一层 `scrub()`；子进程直写句柄的那族（R28）在函数收尾就地 `scrub_file()`。无附带损伤是断言出来的不是看出来的：改动面只落在含夹具令牌的那一轮（P503 的 22 份、每档 1～2 行），`TestQuoteService_ReviseConcurrentSecondLoser` 这类测试名、包路径、时间戳、md5 前缀一律没被改；终扫 ``no leaks found`（命中 0，`scanned ~3.24 MB`，测于 06:19 的 `logs/` 全树）`。
- **新缺失面：判定行本身也是取证产物。** 后台复跑 hub 那一趟时，逐格产物 19 份齐、结论行却只存在于 CLI 任务文件里（任务文件一回收就只剩"判读不可引用"）⇒ 补 `scripts/battlog.py::tee_to(LOGDIR/00-run.log)`，10 份驱动各自在日志目录确定之后挂上，判定行随产物同地落盘。两处自洞当场修：① 子进程 stdout 重定向到文件是**块缓冲**，"日志文件还是空的"不等于"判读丢了"（本轮据此误判过一次，把已经跑完的那趟整趟重跑，白烧一趟；判"跑完"要认末步标记行 + 进程不在两件事）；② `atexit` 关文件早于解释器最后一次 flush ⇒ `ValueError: I/O operation on closed file`、驱动**明明判"逐格被杀"却退 rc=120**，那一轮目录已移出树外（`/tmp/r45_prescrub_artifacts/R22-rc120-…`）不再作为证据。旧轮次里磁盘上还能找到 stdout 的（P701/R28/R22 共 5 份）按"归档补录"写明来源文件名后搬进各自 `00-run.log`；找不到来源的（B16 三族、B17、R30 的 succ 两趟、R28 的 `--only-gate` 趟）**不伪造映射**，改由复跑补齐 ⇒ 现值：`B16ledger/`20260923-053305`、B16ledgerB/`053800`、B16ledgerC/`054414` 三族各读 `电池终态: OK`（b16c 的 M32 顺手把 b16 那格 M7 的等价类理由证伪了一半：`sent 与终态两次台账写之间出现了 return`）；B17action/`054956` `JS 11 格 + Go 7 格 逐格被杀，无存活`；P503/`051832` `KILLED=24 SURVIVED=0 ENV-BROKEN=0`；P701/`042206` `KILLED=71 … 格子数=71`（归档补录）；P702hook/`053219` `total=2 killed=2 alive=0 broken=0` + `md5_orig=md5_after=74b8efc1ea66b937887bcb682b64611f`；R22/`060705` 为改期望后的**全族**复跑，读 `判定：18/18 格，逐格被杀，无存活`（此前 `045158`、`050358` 两趟皆读 `1 格未杀/BROKEN：R4`，是同一判据缺口的两次独立复现，不是抖动）；R28/`040136` 判 30 格 + SKIP 1 全杀；R30/`055445` `判定：30 格（全族）逐格被杀，无存活，无 CONN/RAN/PATCH/BUILD 异常` + 四个控制组 `CONNECT=无``。
- **一条与本段无关但同批踩到的口径：`git check-ignore` 对"不存在"或"符号链接"的路径不匹配带斜杠的规则。** 白名单收窄到"轮次目录"后要确认别的项目规则不会把产物挡回去，顺手核 `user-web/.gitignore:7` 的 `node_modules/`：在共享树里它是真实目录 ⇒ `user-web/.gitignore:7:node_modules/  user-web/node_modules`、rc=0 命中；在同一份内容的**影子克隆**里那个路径根本不存在 ⇒ 同一条命令退 rc=1，看起来像"这条规则失效了"。带斜杠的模式只匹配目录，而 check-ignore 判不存在的路径时没有目录身份可依据 ⇒ **这类"某路径会不会被挡住"的核账必须在它真实存在为目录的那棵树上测**，跨树测出来的 rc=1 是假证据（与本轮"计数要写明数于哪棵树"是同一族）。
- **反向趟照出来的一处真缺陷（本轮修完并双向验过）**：`mut_startup_hook_p702.py:15` 的 docstring 早就写着 "影子克隆里没有 `ROOT/.env` ⇒ 由调用方导出 `POSTGRES_TEST_PASSWORD`（否则控制组红是 ENV-BROKEN）"，而代码里的控制组分支只有 `CONTROL-RED：基线就是红的，先修树再放刀`——**这一类从来没有实现过**。不导口令跑一趟，它把 `failed SASL auth` 说成"树的红"⇒ 读的人会去改没坏的代码（与"连不上库也能报全杀"同一族，只是方向相反：这里不是假绿，是**假归因**）。补 `ENV_SIGNS` 六枚连接层字面量 ⇒ `CONTROL-ENV-BROKEN` 并退 rc=8（与"树红"的 7 分开），末了明写"本趟未放刀，判据未验证"。两向都跑：不导口令 ⇒ 读 ENV-BROKEN、产物里 `KILLED` 0 次、rc=8（`P702hook/20260923-060530`）；导口令 ⇒ `CONTROL-GREEN` + `total=2 killed=2` + md5 一致、rc=0（`P702hook/20260923-053219`）。**教训**：驱动自己承诺的判据类别要用一趟反向跑兑现，注释不是判据。


**7 · 本段未覆盖面（下一位从这里接手）。** ① **四条成功腿只断"这一行落库的形状"，没断"发出去的那一次请求的参数"**——签名、模板、手机号编码、区域端点这些字段级契约仍零腿（与 §23.x 各家"字段没核"同一族）。② `dispatchToProvider` 有 4 个 return，S13–S15 只翻了其中三处判据的 `if` 侧；重试链（`RetryFailedMessages` 读 `sms_retryable_error_prefixes`）不在本电池的过滤器名单里，"成功判据误开 ⇒ 把不可重试的码写进重试队列"这一面没有格。③ **四条成功腿是在"退订检查报错放行"的前提下绿的**：`setupSmsServiceTestDB` 不建 `sms_unsubscribes` 表，整包日志里逐字有 `relation "sms_unsubscribes" does not exist` 与 `sql: database is closed` 两条 ⇒ `s.unsub().IsUnsubscribed()` 恒返回"未退订"。退订拦截本身另有 `sms_unsubscribed_send_test.go` 四条腿（含两格"没退订"停在"凭据缺失"、不进成功分支）⇒ 两套腿互不重叠，但**没有一条腿断"报错即放行"这件事本身**（改成"报错即拒发"时，本批四条腿会同红，那是巧合级保护，不是腿）。④ 池上界三格（P1–P3）判的是**句柄属性**，不等于"CI 不再 53300"：#73（把 53300 变成本地可复现的 100 连接容器 A/B）仍未做；且并行泳道那条"把 CI 的 postgres 抬到 400"的待办（R32⑨ 已订正为写在 `command:` 而非 `options:`）与本批的 32 是**两件事**——抬服务器名额≠给客户端加上界，反之亦然，两边都不许拿对方的绿当自己的依据。⑤ race 族只有 Q-off 一格；站点 B（`MaybeSendAwayReply`）与 cron tick 体仍无专属腿（§23.18 第 11 条④原样挂着），①②③⑤⑥⑦⑧⑨ 那八条也全部结转未动。⑥ 控制组"零 CONNECT"只覆盖四枚过滤器圈到的用例；`internal/service` 整包与其余 125 个含测试的包没走过这台代理，CI 的 core 片也不带它 ⇒ #63（解引用门接进 CI）仍未做，本常驻件同样**没有任何 CI 执行点**。⑦ 电池自身没有"跑全族"的门禁：`--only` 子集与 `--skip-race` 都能给出"逐格被杀"的终态字样（终态行会写明本趟范围，但要有人读）。⑧ D12 常红照旧挂在顶点（对方泳道的假阳，勿代改）。⑨ **未提交**（本轮订正计数：原文写"11 份改/增 + 1 份 `.gitignore` + 117 份产物"，第二类与第三类都已过期）：`git status --porcelain -uno` 本段写于 06:26 时现数 **18 份已跟踪改动**（lane 内 7 份 `.go` + 9 份 `scripts/mut_*.py` + `.gitignore` + 本文档 = 18，06:26 用 `git diff --name-only` 逐条数过；上一版这里的"17 = 6 份 `.go`"两处都少算了一份，少的是本段末那条 p702 修复与一份 `.go` 的归属，见 6-补 段的同一句订正）**→ 07:40 因 #63 再涨到 21 份**（`user-server-ci.yml`／`lint.yml`／`Makefile` 三份，分解见 6-补 段末条）与 **5 份新增文件**（常驻件 `scripts/mut_egress_pool_r30.py`、本轮共用的 `scripts/redact.py` 与 `scripts/battlog.py`、`user-server/internal/pkg/testutil/testdb_pool_test.go`、`user-server/internal/service/sms_success_test.go）**→ 07:40 为 7 份**（+#63 的 `scripts/check-ci-gate-paths.py` 与 `.test.sh`），外加 **564 份逐格取证产物**（10 个轮次、33 个趟次目录；其中 22 份目录自带判定行，余下 11 份"有产物无判读"的归属在 6-补 段逐条写明，其同名电池的新趟均已带判定行）**→ 07:47 终数为 572 份／11 轮次／37 趟次目录，22 份带判定行、15 份不带的归属见 6-补 段末条**，`gitleaks detect --no-git` 对整棵 `logs/` 读 `no leaks found`）。三类全部未推，未推字节对 CI 隐形，只有影子克隆门看得见；且第 1-补 段的可找回性是靠 `.gitignore` 例外给的，**这份例外不入库则产物依旧在库外**（[[project-audit-backlog-2026-09]]）。⑩ **上一版这里写着"其余 7 份电池的落点还没修完"，本轮整个结掉**（读数与教训见上一段"6-补"）：10 份常驻电池的逐格产物现在全部落 `docs/superpowers/specs/ledger/logs/<轮次>/<趟次戳>/`，判定行随产物同地（`00-run.log`），且**每份都在改完写路径之后复跑过**（迁移而不复跑＝交付一份没验过的常驻件）。`tempfile.mkdtemp`/`Path("/tmp")` 现在只承载**私有作业目录与影子克隆**（那是工作副本不是证据，留在树外是对的）。两处仍然找不回的东西照实记：R28 的 `/tmp/r28_seam_battery_*` 那一趟产物已随重启蒸发（同日三趟复跑在树里，取的是"判 30 格全杀"这一版，见 6-补 段），`/tmp/r44_measure/`（第四十四轮普查 84 项）仍在库外——它属"普查快照"而非电池产物，结转给下一位按同一写法迁进 `logs/` 的对应轮次。

**订正（09-23 17:2x 收口轮，按"不划原文、另起一行"的口径写；对应正文见 §23.21）。** 本段有四处结转项已经结掉、一处产物前提已经不成立：
- 第 7 段④ 那句"#73（把 53300 变成本地可复现的 100 连接容器 A/B）仍未做" ⇒ **已做**：`scripts/cap-ab-53300-probe.py` 常驻件在案，100 名额容器上 A 相（HEAD 那份无 `SetMaxOpenConns` 的 `testdb.go`）三轮各报 1 行上限报错、B 相（32/8 那份）三轮 0 行，控制格（把 A 相挪到 500 名额的开发容器）亦 0 行 ⇒ 读数与归因见 §23.21 第 2 段。同段那句"抬服务器名额≠给客户端加上界，两边都不许拿对方的绿当自己的依据"**原样有效**，本轮没有把它当成"CI 侧不用再动"的依据。
- 第 7 段⑥ 那句"#63（解引用门接进 CI）仍未做" ⇒ #63 已在 §23.20 做完、且其字节已随 `1ccfe5be` 进入已推送顶点（现测 `grep -n 'test-nil-deref' .github/workflows/user-server-ci.yml` 在 `push.paths`／`pull_request.paths` 与独立作业名四处命中）。但**"本常驻件同样没有任何 CI 执行点"这一句仍成立**：十份变异电池与这台 CONNECT 代理都只在本地跑，CI 里没有它们。
- 第 6 段"漂移核验"那一行的末态（`21 改 + 7 增／572 份／11 轮次／37 趟次目录`）随 09-23 上午的开发机重启整体失效，本轮按转录时序重放成 `1ccfe5be`（28 份文件、2420 增／85 删），产物面重跑成 12 轮次／**488** 份（终数见 §23.21 第 7 段）。**重跑的是同一套格、同一份判据，但旧趟次的原始输出已经不在这台机器上，也不再在任何一棵 HEAD 里**：`git log --all --oneline -- 'docs/superpowers/specs/ledger/logs/*20260923-073421*'`（另测 `042206`／`053219`／`054956`／`full-2` 四枚）各退 **0 笔** ⇒ 本段正文引用的旧档名（`R30/full-2/S1.log`、`race_A_locked.log` 等）如今**只余本段的转述**，逐枚的新旧对照与各自判定行在 §23.21 第 6 段。
- 6-补 首条那句"10 份常驻电池全部把逐格产物写进 `docs/.../logs/<轮次>/<趟次戳>/`"仍然成立，但它掩盖了一个当时没做的事：**"写进树内"不等于"进了版本控制"**。本轮收口前现测 `git ls-files docs/superpowers/specs/ledger/logs | wc -l` = **0**，而 `git ls-files -o` 同径 = **488** ⇒ 白名单给了"可跟踪"，一批都没 add 过，克隆里照旧找不回。这笔在 §23.21 第 7 段结（连带泄露面复扫与 `.gitignore` 12/12 闭合），也是 [[feedback-verify-secondhand-review-claims]] 说的"自己上一轮的结论同样是二手证据"——上一轮写"0 份仍在库外"时用的是 `check-ignore` 的口径（**没被忽略**），不是 `ls-files` 的口径（**已入库**），两个词在这句话里被我当成了一件事。

### 23.20 批M-11 #63：把解引用门接进 CI，代价是先得让"接 CI 这件事"本身有一道门（2026-09-23）

**1 · 作业本体。** `scripts/check-test-nil-deref.py` 从"只在 `make audit`（CI 从不执行它）"变成
`user-server-ci.yml` 的独立作业 `test-nil-deref`（该文件作业数 12 → 13，YAML 解析现测），`run: python3 scripts/check-test-nil-deref.py`，
`timeout-minutes: 5`。**为什么不是 `static-gates` 的第 9 个步骤**：本文件 A3 注释记的就是
"同 job 前序一步红 ⇒ 后续步骤全成 skipped"，被藏起来的门与坏掉的门长得一样；作业之间无 `needs:`
（现测：`jobs with needs: []`），所以独立作业既不被遮挡也不遮挡别人。门耗时本机实测 **1.2s**（纯正则，无 DB、无网络、不编译）。

**2 · 抬 pin 的时候被自家门拦下，这一拦正是门有用的证据。** 新作业先照抄本文件其余作业的
`actions/checkout@v4` ⇒ `check-action-runtime.py` 当场红：
`user-server-ci.yml: actions/checkout@v4 实有 13 > 上界 12`。上界 12 是 R47 抬干净侧后留下的豁免账本
（理由"并行泳道持有该文件未提交改动"），账本只许降不许升 ⇒ 正解不是放宽账本，而是新作业用
`actions/checkout@v7`（仓内已有 21 处 v7）与 `actions/setup-python@v7`（与 `seam-guard.yml` 同 pin）。
改后两向读数：门 `node20/node16 命中 30 处：未豁免 0，豁免内 30` ⇒ rc=0，本文件 v4 站点回到 12。

**3 · 反向格照做（§23.16 第 5 条欠的那笔），但它证的是形状不是 GitHub 的求值器。**
`cp` 备份后从 `user-server-ci.yml` 的两个 events 里各摘掉 deref 门的脚本行与基线行（共 4 行），
现算的门立刻红且点名到位：

```
user-server-ci.yml 的 push.paths 缺 `scripts/check-test-nil-deref.py`（该路径被作业 test-nil-deref 的步骤实际执行 ⇒ 改它不会重跑这道门）
user-server-ci.yml 的 pull_request.paths 缺 `scripts/test-nil-deref.baseline`（…）      # 共 4 条
```

`cp` 还原后 md5 与放刀前相同，复跑 rc=0。产物：
`docs/superpowers/specs/ledger/logs/CIwiring/20260923-073421/{00-make-audit.log,10-drop-deref-paths.log}`。
**边界要写死**：GitHub 对 `on.push.paths` 的求值发生在服务端，本地任何门都只能证"这些行还在、
且与作业实际执行的脚本对得上"。"改基线那一行确实会拉起这个作业"这一句，要等本批推上去之后
**第一个只碰这两个文件的提交**跑出 run 才算兑现（交接给 #78 之后的复验，别提前记账）。

**4 · 为了让下一次不重犯，新做了一道门：`scripts/check-ci-gate-paths.py`。** 判据：逐份工作流扫出
"步骤里真跑了 `scripts/**.py|sh`"的站点 ⇒ 该门脚本路径 **加上从门源码里现抓的判据文件**
（字面量 `*.baseline` / `*.registry`，且磁盘上真存在）必须逐条出现在 `on.push.paths` 与
`on.pull_request.paths` 里；该事件整体没有 `paths:` 过滤则判为满足，但必须打印"无 paths 过滤⇒每次触发"，
不许静默通过。派生而不是手抄（[[project-gate-scope-blind-spots]] ㉗：判据依赖手抄表时最先失效的是表本身）。
现值：`工作流 15 份 · 门站点 27 处 · 派生判据文件 6 份 · 输入面 md 引用 19 处（不要求进 paths）· paths-ignore 站点 0`。

**5 · 这道门上线第一跑就抓到四处同族存量缺口，一并补掉。** 补 deref 之前它先红在
`static-gates` 另外三个门上：`scripts/check-architecture.sh`、`scripts/check-date-bucket-tz.sh`、
`scripts/check-unwired-assets.sh` 与 `scripts/date-bucket-tz.baseline` 全都不在
`user-server-ci.yml` 的触发面里（＝改了架构门的判据或日期门的基线，CI 根本不重跑那道门）。
两行改四行（push/pull_request 各一份），改后门绿。**为什么 `docs/**` 不加**：`paths` 是作业级的
总闸门，加一行 `docs/**` 会让每次文档提交拉起这条 13 作业链（含两个 `-race` 作业，单跑 450–880s），
所以门把这类"输入在业务树/文档里"的引用打印成"不要求"而非静默放过——
残留洞因此是**明写的**：`check-unwired-assets.sh` 读 `docs/architecture/AI_CORE_FEATURE_INVENTORY.md`，
改那份台账不重跑这道门。正解是把它挪到一个只被 `docs/**` 触发的廉价作业，属下一位的活，不在本批（只修被报告的入口）。

**6 · 注册与自洽（全部真跑，跑在最终字节上）。**
- `Makefile` 的 `audit` 静态链里排在 `check-action-runtime.py` 之后（工作流形状门挨在一起），
  `make audit` 整链 ⇒ **rc=0、`✅ 静态审计通过`、链内 12 个 `──` 门块**（`10-drop` 同一目录的 `00-make-audit.log`）。**上一版这里写着"14 个"，是数错了口径**：那是不锚行首的 `grep -c '──'`，多出来的两行是 `check-md-links-offline.py` 自己打的 `──── 扫描 162 个 md（CI 口径：只认 git 索引）────` 与其断链行——锚行首 `grep -c '^── '` 与直接数 `Makefile` 的 `audit:` 目标里的 `@echo "──` 两个独立口径都读 **12**（[[feedback-forensic-harness-hygiene]] 的"计数要锚行首＋形状扫描"在这一段自己头上又兑现一次）；
- `lint.yml` 的 `workflow-refs` 作业加两步：门本体 `python3 scripts/check-ci-gate-paths.py --repo .`
  与用例 `bash scripts/check-ci-gate-paths.test.sh`（该作业无 `paths:` 过滤 ⇒ 每次 push 都跑，
  所以它守的是别的作业的触发面）；
- 用例 `scripts/check-ci-gate-paths.test.sh` 五格全过：G1 缺 paths 必红（红因同时点名门脚本与**派生**基线）、
  G2 补齐必绿、G2b 无过滤必须明说、G3 真仓库绿且自证 `scanned=15` 与独立复算 `ls|grep -c` 相同＋派生集非空、
  G4 工作流目录不存在 ⇒ rc=2（没跑过 ≠ 绿）。G3 的抽数一开始恒空——BSD sed 不认 `\+`，
  改用 `grep -oE`；这是"计数 helper 自己先把数读丢"的又一例（[[feedback-forensic-harness-hygiene]]）；
- 同批受影响的门全部复跑：`check_workflow_refs.py` rc=0（15 份）、`check-action-runtime.py` rc=0、
  `check-action-runtime.test.sh` `PASS=43 FAIL=0`、`check-ci-step-coverage.test.sh` `PASS=33 FAIL=0`；
- 产物落树后 `gitleaks detect --no-git --source docs/superpowers/specs/ledger/logs` ⇒ `no leaks found`
  （scanned ~3.63 MB），`.gitignore` 白名单从 10 轮成对扩到 11 轮（新增 `CIwiring/` 两行），
  新目录实测 `git check-ignore` 退非 0 ⇒ 产物可追踪。

**6-补 · 收口复跑：把上面每一条在"最后一次编辑之后"的字节上再读一遍，并当场抓到本段自己的一处计数假数。**
第 6 段那份 `00-make-audit.log` 记的是 **07:34** 的字节，而它写完之后本批还动了文档与取证目录 ⇒
按 [[feedback-delivery-accounting-layers]] 第 7 条（"数要在最后一次编辑之后数"）与
[[feedback-mutation-battery-hygiene]] 的"取证基座一动就重跑便宜格"，整链在**终趟 07:47** 的字节上重跑，
产物 `docs/superpowers/specs/ledger/logs/CIwiring/20260923-074759/{00-make-audit-final.log,20-gitleaks-final.log}`
（两档 `scrub()` 前后**逐行比对：0 行改动**，即门输出里没有任何"标签+长令牌"形状）。
**同一判据在本段里连留了三趟（`074135`/`074449`/`074759`），这是本段自己的排序失误，不是设计**：
每抄一次计数就为确认字节再跑一趟门，而那一趟又落两档产物 ⇒ 计数再漂一次，如此三轮（566/34 → 568/35 →
570/36 → **572/37**，逐项见 §23.19 6-补 段末条）。三趟都没删、也没被覆写
（[[feedback-mutation-battery-hygiene]]"复跑不许覆盖上一轮"），**逐项读数完全相同**，下表取终趟；
教训写成可执行的一条：**收口顺序＝①做完所有会产物的动作（跑门/跑电池/落档）②只留最后一趟作终趟
③然后一次抄数抄到底**——"跑一趟、抄一次"交替做，等于用无限趟去追一个自己每轮都推远的数。

| 复跑项 | 07:34（第 6 段原记） | 终趟 07:47 的最终字节 | 判定 |
| --- | --- | --- | --- |
| `make audit` 整链 | rc=0 | **rc=0、`✅ 静态审计通过`、`grep -c '^── '` = 12 块** | 一致 |
| `check-ci-gate-paths.py` | 绿 | `工作流 15 份 · 门站点 27 处 · 派生判据文件 6 份 · 输入面 md 引用 19 处 · paths-ignore 0`，rc=0 | 一致 |
| `check-ci-gate-paths.test.sh`（新门自己的五格） | 全部通过 | **rc=0、`全部通过`、`✓` 计 12 处**（G1/G2/G2b/G3/G4 五格含 12 条断言） | 一致 |
| `check_workflow_refs.py` | rc=0（15 份） | rc=0 | 一致 |
| `check-action-runtime.py` | rc=0 | `命中 30 处：未豁免 0，豁免内 30`，rc=0 | 一致 |
| `check-action-runtime.test.sh` | PASS=43 | **PASS=43 FAIL=0 rc=0** | 一致 |
| `check-ci-step-coverage.test.sh` | PASS=33 | **PASS=33 FAIL=0 rc=0** | 一致 |
| `gitleaks`（整棵 `logs/`） | `no leaks found` ~3.63 MB | `no leaks found` **scanned ~3.70 MB**、rc=0 | 一致（增 0.07 MB＝#63 落的六档产物） |

`rc=` 的两处口径事故顺带记一笔（都是本段取证过程自己踩的）：① `${PIPESTATUS[0]}` 写在 `;` 之后的
`echo` 里，在 **zsh** 下取不到（zsh 的数组叫 `pipestatus`、且下标从 1 起），于是第一版 `20-gitleaks-final.log`
末行是 `gitleaks_rc=`（空串）——**空串和 0 在扫读者眼里是一回事**，故改为"先 `cmd > file` 再 `rc=$?`"重落一档；
② 同一条判据在 `bash -c` 与交互 zsh 下行为不同 ⇒ 落档的取证命令一律**不用管道取码**。

**抓到的一处假数在本段第 1–2 行里**：原写"链内 **14** 个 `──` 门块"，真值 **12**。差的两行不是门，
是 `check-md-links-offline.py` 自己打印的 `──── 扫描 162 个 md（CI 口径：只认 git 索引）────` 与它的断链行——
`grep -c '──'` 不锚行首就会把这两行一并计入（且它用的是四个 `─`，与门块的两个不同形）。
两个独立口径现算都读 12：锚行首 `grep -c '^── '`，以及直接数 `Makefile` 的 `audit:` 目标里
`@echo "──` 的出现次数（`awk` 限定在 `audit:` 与下一个目标之间）。这正是
[[feedback-forensic-harness-hygiene]] 那条"形状扫描须锚行首"在本轮**第二次**兑现（第一次是 G3 抽数恒空）；
差别在于第一次是**新写的 helper** 读丢，这一次是**已经写进文档的读数**读丢 ⇒ 结论：
**抄进文里的计数，与抄进文里的 file:line 同罪**，收口复跑要连它们一起重算，不能只重跑门禁本身。

**7 · 本段未覆盖面（下一位从这里接手）。** ① 第 3 段那句"服务端求值"的兑现要在推送后读 run；
② 门只认 `*.baseline` / `*.registry` 两种后缀的字面量——别的后缀（`*.allowlist`、`*.txt`）或变量拼名会漏派生，
现值 6 份是从码上现抓的，扩后缀要先数真仓有几处；③ `paths-ignore` 不参与判定（现值 0 处，若哪天有人用它，
本门会打印数额但不会判"被 ignore 的目录"）；④ 门站点判定依赖步骤 `run` 里出现 repo 相对路径字面量，
`working-directory` + 相对文件名（`python3 check-x.py`）这种写法会漏——现扫 27 处站点里没有这种形状，
下一条加门时要留意。

**订正（09-23 17:3x 收口轮，见 §23.21）。** 三条：
- 第 7 段① 那句"第 3 段那句'服务端求值'的兑现要在推送后读 run" ⇒ **已读**：本泳道推送顶点 `f4da0192` 的那跑 `user-server-ci`（run 35825221569，作业级现读 `gh api …/runs/$RID/jobs`）里 **`Test nil-deref gate (零条目基线棘轮)` = success**、`Static gates` = success、`Security scans (gitleaks / govulncheck)` = success ⇒ "改基线那一行会拉起这个作业"从此有了一次真实执行史，不再是本地形状证据。该跑整体仍 `failure`，红在四处别处（归因见 §23.21 第 3 段，无一属本泳道）。
- 第 6-补 段末那句"原写链内 **14** 个 `──` 门块，真值 **12**" ⇒ 这条订正本身现在也过期了，且**它过期不是因为读错，是因为树动了**：本轮在 `2ab6a4b9` 现数 `awk '/^audit:/{f=1;next} f&&/^[a-zA-Z0-9_.-]+:/{exit} f' Makefile | grep -c '@echo "──'` = **14**（并行泳道 R48/R52 把 `check-action-runtime.py`、`check-ci-gate-paths.py` 等注册了进来）。⇒ 口径不是"12 才是对的"，而是**这个数不属于文档，属于 Makefile**：任何一次引用都必须带"测于哪一版"。同趟 `make audit` 在干净克隆里第 9 块红、档内只印出 9 块（`make_audit_rc=2`），"跑了几块"与"有几块"是两个数，别拿前者订正后者。
- 本段引用的四档 `CIwiring/20260923-073421`、`-074135`、`-074449`、`-074759` 已不在磁盘上（09-23 上午的开发机重启；`git log --all --oneline -- 'docs/superpowers/specs/ledger/logs/*20260923-073421*'` 退 **0 笔** ⇒ 从未入库，无法从历史找回）。同判据的替代品是重跑出的 `CIwiring/20260923-153119`（四档：`00-make-audit.log`／`10-gate-selftest.log`(`### check-ci-gate-paths.test.sh rc=0`)／`20-gitleaks-tipcommit.log`／`30-gitleaks-worktree.log`(`### gitleaks(尖端提交) rc=0`)）与 §23.21 第 7 段那三档；本段第 3 段那张反向格表（`10-drop-deref-paths.log`）随旧档消失，其结论未变——反向格本轮没再复跑，结转项记在 §23.21 第 8 段②。

### 23.21 批M-11 后续 2 收口（#73／#78／#79）：重启后按转录重放整条泳道、把 53300 变成一台本地按得下的开关、一次整族复跑逼出七处判据不干净（2026-09-23）

**1 · 重启带走了什么、按什么重放（#79）。** 09-23 上午开发机重启 ⇒ 本泳道那批未提交字节（§23.19 6-补 的终数"21 改 + 7 增"）与当时已在树内的 11 轮次取证产物一起消失。恢复只认主证据：按会话转录时序把 28 份文件逐一重放进持久树 `r45-lane`，落 `1ccfe5be`（现测 `git show --stat`：**28 files changed, 2420 insertions(+), 85 deletions(-)**），直接接在共享树顶点 `c985cdf3` 之上，链为 `c985cdf3` → `1ccfe5be` → `f4da0192` → `f8d83b59` → `2ab6a4b9`（测于 17:05 的 `git log --format='%h %ad %s'`）。重放不是"抄回来就行"，它同时作废了两类结论，两类都在本段重做：① **产物**（第 6 段那张新旧趟次对照表就是为这笔而存在）；② **一切以计数为判据的常驻件**——基座从 `2091cb07` 换到 `c985cdf3` 之后，并行泳道往 `internal/{model,repository,pkg/db}` 里加了用例，P701 电池三枚控制组的"该跑几条"整片漂（model 9→13、repository 11→16、db 6→7），细节见第 4 段。

**2 · #73：把"只有 CI 才红"的 53300 变成一台本地按得下的开关。** 之所以本地从来跑不出这条红，是两处配置之差（本轮现读，不是回忆）：开发容器 `mtk-postgres` 的名额是 **500**（`docker-compose.yml:69`），CI 的 `services.postgres` 不写任何连接数参数 ⇒ 用镜像默认 **100**。"本地跑不出来的红"＝没有回归门，于是起一枚同样 100 名额的容器（`r45-pg100`，`127.0.0.1:8234->5432`；探针每趟自报的端口自检行读 `max_connections=100`），常驻件 `scripts/cap-ab-53300-probe.py` 只换 `user-server/internal/pkg/testutil/testdb.go` **一份字节**做 A/B：A 相＝HEAD 泳道基线那份（无 `SetMaxOpenConns`，md5 前缀 `b7d993b72758`），B 相＝本批那份（`SetMaxOpenConns(32)`／`SetMaxIdleConns(8)`，`dfc201cb3594`）。其余字节两相逐字相同 ⇒ 差值只能归因到那两行。产物在 `logs/CapAB/<趟次戳>/`，六趟读数：

| 戳 | 范围／容器 | A 相上限报错行数 | B 相 | A 相峰值连接 | B 相峰值 | 附带读数 |
| --- | --- | --- | --- | --- | --- | --- |
| `133538` | narrow 1 轮 @8234（**100 名额**） | 1 | 0 | 1 | 1 | 两相 `go rc=1`／`rc=0`，采样器此刻仍是坏的（第 2-补 条①） |
| `134029` | narrow 3 轮 @8234 | 1／0／1（合计 2） | 0／0／0 | 50／40／47 | 7／21／7 | A 相 `FAIL=1 PASS=3`×2、`FAIL=0 PASS=4`×1 |
| `134115` | **`control-a`：A 相挪到 500 名额的 `mtk-postgres`@8232** | 0 | — | 58 | — | `FAIL=0 PASS=4` rc=0 ⇒ 同一份无上限字节、只是名额不同 ⇒ **分歧来源是容量** |
| `134129` | narrow @**8201**（打错端口：那是容器内部端口） | 0 | 0 | `-1★SAMPLER-BROKEN` | 同左 | 两相 `FAIL=4 PASS=0`；端口自检行逐字 `FATAL: password authentication failed for user "admin"` ⇒ 本趟证的是"驱动遇到死库会怎么报"，不是"53300 不出现"（第 2-补 条②） |
| `134337` | **full：整包 `./internal/service/`，CI 参数** | 4（旧裸计数口径，真值 2） | 0 | 68 | 32 | A 相 `FAIL=3 PASS=4008`、B 相 `FAIL=1 PASS=4010` ⇒ B 相那 1 条红是 §23.20 归因过的 D12 家族，不是容量 |
| `152743` | narrow 3 轮 @8234（**CAP_RE 定稿口径**） | 1／1／1 | 0／0／0 | 53／65／42 | 1／19／13 | 判定行：`A 相上限报错 3 行／B 相 0 行 ⇒ 分歧成立，53300 归因到连接池上界` |

每格跑完都回读磁盘上的 `testdb.go` md5 并与注码时记下的值比（六趟 `00-summary.log` 逐格行都带 `跑后磁盘=…一致`），末了再复测一次还原态（六趟同为 `dfc201cb3594`＝B 相那份）⇒ 这条是防"先编后改造出的陈旧二进制假绿"（§23.19 那族的同一口井）。三枚判据彼此独立：上限报错行数、峰值连接数、`go test` 的 rc／FAIL／PASS——53300 出现与否与"这一相吃掉多少名额"互不推出。

**2-补 · 采样器自己的两个缺陷，都是读数暴露的，不是审代码审出来的。** ① `133538` 那趟两相峰值都是 **1**：采样 SQL 当时按 `state = 'active'` 过滤，而连接池里绝大多数句柄处于 `idle`（`idle in transaction` 之外）⇒ "只数活跃的"把 50～68 个名额的真实占用读成了 1。改成 `datname like '<dbname>%'` 计数**含 idle**、并排除采样器自身（`pid <> pg_backend_pid()`）之后，`134029` 当场读出 A 相 40～50、B 相 7～21，与同趟的 53300 是否出现互相独立地对上了。② `134129` 那趟峰值印 **`-1★SAMPLER-BROKEN`**：采样器连不上库时原先会退化成"0 条连接"这一串看起来像"这相真省"的读数，本轮把它改成显式的 `-1`＋★ 标记，并在判定行追加 `★2 格采样器无读数 ⇒ 名额占用那一维本趟没测到`；探针自身退出码同时分成两档——判据不成立退 0／1，**取证基座死了退 5**（这一档在 `134129` 兑现）。这两条都是 [[feedback-mutation-battery-hygiene]] 的"读值 helper 不许把空串与失败并成一类"在同族上的第三次兑现。

**2-再补 · 计数口径本身错过一次：裸 `53300` 把读数从 2 抬到 4。** `134337` 的整包趟当时报 `53300/上限报错行数=4`，事后拿产物复算 `grep -ic '53300'` 还给 **5**，而真命中只有 **2**：多出的三行分别是①产物首行的身份行（含字面量 `53300/上限报错行数=`——**驱动自己写的行，事后复算会再多吃 1 行，跑的时候它还不存在**，所以 4 与 5 都"对"，取决于谁在数），②夹具自造的 event_id `tg_upd_1_426533000_0`（整包趟里 2 行，narrow 趟里 0 行 ⇒ 这就是"同一判据在不同范围下口径不同"）。修法不是"再减 3"，是把计数锚到错误形状本身：`CAP_RE = too many clients|SQLSTATE 53300`。锚定后的四向复核：整包 A 相 2 行、整包 B 相 0 行、narrow 每轮 A 相 1 行 B 相 0 行，且 `152743` 三轮的裸计数与 CAP_RE 计数已同为 1（narrow 里夹具行不存在）⇒ 换锚不改判。

**3 · 推送后的 CI 回读：新作业绿，四枚红全部归到别人，"53300 这一跑没出现"不构成依据。** 对已推送顶点 `f4da0192` 现读 run `35825221569` 的作业表：**`Test nil-deref gate (零条目基线棘轮)` = success**（§23.20 第 7 段① 欠的那笔兑现）、`Static gates`／`Security scans`／`Build (user-web)`／`Coverage`… 逐项见产物；红在 4 个作业：`Unit tests -race (service)`、`Unit tests -race (core)`、`Coverage (user-server)`、`ESLint (user-web)`。红名逐条读红因（不是拿作业名推）：
- `--- FAIL: TestD12_NoNewLegacyKVDirectQuery` ×2 ⇒ 与 §23.17／§23.18 同一枚假阳（命中文件 `../service/quote.go` 属折扣那一片），结转项照旧，本泳道不代改。
- `--- FAIL: TestExternalOrderRepository_GetByOrderID/get_non-existing_order`，红因逐字 `integration_test.go:824: GetByOrderID() error = <nil>, wantErr true` ⇒ **本轮新登记的第三枚公开既有红**（前两枚是 D12 与 env-coverage）。三条证据链把它钉死在别人那边：① 三跑做差：`79378eb9`（09-22 21:39，早于本泳道重建）、`a63700a4`、`c985cdf3` 三跑的 `--log-failed` 里同一测试名各出现 2 次 ⇒ 与本批字节无关；② 本地在干净克隆里同判据复现（同一条红因、同一次 `go test ./internal/repository/ -run TestExternalOrderRepository_GetByOrderID`）⇒ 不是 CI 环境抖动；③ 归属：该三态契约（`integration.go:259-264`：命中 `(行,nil)`／未命中 `(nil,nil)`／故障 `(nil,err)`，注释点名 T-P7-02／G15 第④条）由 `5b92525c` 有意改成不报错，而 `integration_test.go` 仍按老契约要 err；且**共享活树里这个文件正被并行会话改**（`git status --porcelain` 现读 ` M user-server/internal/repository/integration_test.go`，其 diff 里正在删 `wantErr: true` 那行）⇒ 修法已在别人的未提交面里，本泳道一刀都不补（[[project-shared-worktree-parallel-agent]]）。
- `ESLint (user-web)` ⇒ 与 §23.20 那笔同源（浏览器自动化泳道持有 `user-web` 未提交面）。
- **53300 的回读要按"下界"写**：本跑 `--log-failed` 里 `too many clients|SQLSTATE 53300` 命中 **0**，而它前一跑 `c985cdf3` 命中 **12**、`a63700a4` 命中 1。⇒ 只允许说"B 相字节上去之后的这一跑没出现"，不允许说"池上界修好了 53300"——`--log-failed` 只含失败作业、名单本身在两跑间会漂，且 #73 的本地 A/B 证的只是"32/8 那份字节在 100 名额下不吃第 33 个名额"。另记一条对第 2 段有用的旁证：`79378eb9` 那一跑的红名是 `TestAudience_SelectBySegment`，而**本地 A 相三轮里开火的是 `audience_selector_test.go:93` 与 `async_db_handle_probe_test.go` 两枚之一**（各轮随机）⇒ 名额打满时红落在"最后一个开连接的用例"上，这与 §23.19 第 7 段④ 早就写过的那句"CI 的红名会漂"是同一件事的两个读数。

**4 · 一次整族复跑逼出七处判据不干净（P701 电池，`2ab6a4b9`）。** 先记账号：`f8d83b59` 那笔结的是**半提交**账（T-P7-03 由 `23dae260` 只把 model 的复合索引标签与 repository 的方法集改名提交了一半，service 侧 `in.DueAt` 连同判据用例从未进过任何一棵 HEAD），它留下的口径是**常驻电池跑的是克隆 HEAD 的字节 ⇒ 锚点必须跟着 tip 而不是工作树**。这一笔结的是**判据侧**的账，全部由 `160821` 那趟读数逼出（末行 `===== 电池判定：有洞 =====`，`计数：KILLED=65 SURVIVED=1 RED-UNNAMED=5 … 格子数=71`）：
- **四格 expect 点名点到了不存在的用例**：`K06`／`K38`／`K92`／`K93` 各自红的是别的派生用例（问题行逐字如 `K38 红了但没点出 TestBillRepository_ReadsDistinguishMissingFromFailure：['TestBillRepository_ReadFailureIsNotMissingRead']`）⇒ 名字是**用例改名**时留下的化石。修法按"改期望不改变异"：四格 expect 分别改点 `TestBillSchemaShape`（其断言逐字含 `numeric(14,2)`，正对本格量程漂移）、`TestBillRepository_ReadFailureIsNotMissingRead`、`TestBillRoutes_UnassembledStillMountsRoute`、`TestBillRoutes_RouterFileHasNoInlineHandler`，并在注释里写清"这一格靠哪条断言开火"。
- **`K30` 是一类新的形态：崩溃替在跑的腿补印。** 本格注码后整包 `ran=4`（控制组 16），红名里没有 expect 那条腿、却也没有 12 条别的腿 ⇒ 一条 `panic` 带走了整个二进制。守恒式当场把它抓住（`K30 计数不平：顶层 PASS=3 + FAIL=1 ≠ 控制组的 16`），但抓它的不是新判据。**新增 `panic-split` 类**：`RED-UNNAMED` 且输出含 `panic:` ⇒ 把 expect 那条腿单拎复跑（`-run '^TestBillRepository_NilHandleFailsLoudly$'`），要求 `rc≠0 且 ran=1 且 FAIL 名单恰为它` 才判 KILLED，并把截断那一趟与单拎那一趟两份产物都留下（`K30.log` 与 `K30-split.log`）。这一条不是豁免：**崩溃正是这把锁要防的事**（nil 句柄该在装配期炸而不是在别的用例里炸），豁免的只是"别拿连带红当杀"。
- **`G3` 少一个消费方 ⇒ "注了码而门不报"看着像全杀。** 台账 23a 那格的注码面当时只列 `bill_wiring.go` 与 `payment_wiring.go` 两处 `repository.NewBillRepositoryWithDB(db),`，而 `collection_wiring.go:96` 是第三处（并行泳道 `c985cdf3` 之后新增的装配点）⇒ 摘两留一，门自然不必报漂移。补成三站点 3-tuple（每刀自带文件名），且**若不读门自己打印的接线数，这一格看起来就是"没牙"**，所以格的判定同时看 `gate rc` 与门点名。
- **三枚控制组的计数不许写死**：`--onto` 之后 model 9→13、repository 11→16、db 6→7。放刀前现测名单、只比集合不比常量，这条 §23.19 早写过，本轮是它第一次**因为基座移动而整体失配**，代价是一趟全族（`160821`，80 份产物）。
- 终态：`logs/P701/20260923-165140/00-run.log` 读 **`计数： KILLED=71 SURVIVED=0 RED-UNNAMED=0 BUILD-BROKEN=0 ENV-BROKEN=0 NO-RUN=0 格子数=71`** + `全部格子已还原（逐文件 md5 与基线一致）` + `===== 电池判定：71 格逐格核验，无未登记存活 =====`（81 份产物，含 8 枚控制组档与 `K30-split.log`）。**同族同树两趟都读 71 格**（`151025` 测于基座 `2091cb07`、`165140` 测于 tip `f8d83b59`），差的是判据名，不是牙。

**5 · 一处环境假红，长得像"代码坏了"：`xcrun` 退 69 ⇒ `SDKROOT` 空 ⇒ `ld: library 'resolv' not found`。** 放完刀的整族第一趟（`164728`，只有 3 份产物）在读到控制组之前停：`控制组[ctrl] build failed` ＋红因逐字 `clang: error: linker command failed with exit code 1`、上一行 `ld: library 'resolv' not found`。这不是本批字节的红：`go test` 要链接 cgo，链接器要的 SDK 路径来自 `xcrun --show-sdk-path`，而它当时退 **69**（Xcode 27.0 许可未接受，`SDKROOT` 因此是空串）。绕法是把 SDK 显式指到 CLT 那份：`SDKROOT=/Library/Developer/CommandLineTools/SDKs/MacOSX.sdk` ＋ `PATH=/Library/Developer/CommandLineTools/usr/bin:$PATH`，之后 `165140` 整族跑通。**接受许可是人的动作，本泳道不静默装/改系统**（`sudo xcodebuild -license accept` 留给使用者）；这一趟**不覆盖、不删**，作为"门的红先归因环境"的现场留在 `164728/`（[[feedback-verify-by-running]] 里"vet 绿 + build 红＝磁盘写满"的同类：build 红先问链接器要什么，别先改代码）。顺带记两条同趟踩到的 shell 坑：zsh 的 `for g in "python3 scripts/x.py"` 不拆词 ⇒ 把整串当一条命令找不到、`rc=0` 又是 `tail` 的码；BSD `cat` 没有 `-A`（用 `-t`）。

**6 · 旧趟次 → 新趟次对照表（重启之后所有引用的唯一有效落点）。** 左列是本节四段（§23.18—§23.20）正文引用过的旧档名，磁盘已无、且从未在任何一棵 HEAD 里（现测 5 枚旧戳 `git log --all --oneline -- <glob>` 各 **0 笔**）；右列是本轮重跑的同判据产物，判定行逐字照抄。**今后引用左列一律按"转述"处理，要复算请打开右列。**

| 轮次 | 旧档（正文引用，已消失） | 新档（本轮，`logs/<轮次>/…`） | 新档判定行（逐字） |
| --- | --- | --- | --- |
| R30 | `full-1`／`full-2`／`full-3`／`succ-1`／`succ-2`／`022537` | `132543`（单格验形）＋`141640`（全族 37 份） | `===== 判定：30 格（全族） =====`／`逐格被杀，无存活，无 CONN/RAN/PATCH/BUILD 异常` |
| R28 | `040136`（另 `--only-gate` 趟） | `143021`（33 份） | `结论：判 30 格（另有 1 格合并 SKIP），全杀 ⇒ True` ＋ `树残留：无（全部还原且 md5 与开刀前一致）` |
| R22 | `060705`（及 `045158`／`050358` 两趟 BROKEN） | `143232`（21 份） | `===== 判定：18/18 格，逐格被杀，无存活 =====` |
| P503 | `051832` | `150317`（27 份） | `===== 电池判定：24 格逐格核验，无未登记存活 =====` |
| P701 | `042206` | `151025`（80 份，基座 `2091cb07`）→ `160821`（80 份，tip `f8d83b59`，**有洞**）→ `165140`（81 份，修后） | `KILLED=71 SURVIVED=0 … 格子数=71`；中间那趟是 `KILLED=65 SURVIVED=1 RED-UNNAMED=5` ⇒ 两趟合起来才是"七处不干净 + 全修完"这条结论 |
| P702hook | `053219`（正向）／`060530`（ENV-BROKEN 反向） | `144412`／`153535` | `total=2 killed=2 alive=0 broken=0` ＋ `md5_orig=74b8efc1… md5_after=74b8efc1…` ＋ `ALL-CASES-KILLED-AND-SOURCE-RESTORED`；反向档读 `CONTROL-ENV-BROKEN：不是树的红，是连不上测试库 …（本趟未放刀，判据未验证）` |
| B16ledger／B／C | `053305`／`053800`／`054414` | `144419`（11 份）／`144845`（14 份）／`145614`（16 份） | 三档同读 `电池终态: OK`，收尾对照腿分别 `ran=8/8`／`11/11`／`11/11`、`skip=0` |
| B17action | `054956` | `152256`（21 份）＋ `161617`（2 份，反向） | 正向 `===== 电池判定：JS 11 格 + Go 7 格 逐格被杀，无存活 =====`；反向档首行即写明"JS 腿前置（node_modules）缺失时，常驻件必须当场停机而非静默跳过"，读数 `…node_modules 不存在——先在 user-web/browser_automation 里 npm install` ＋ `rc=1（非零即判停机成立；红因点名缺失目录的绝对路径）`——**lane 树天生没有 `node_modules`，这一枚夹具是免费的** |
| CIwiring | `073421`／`074135`／`074449`／`074759` | `153119`（4 份）＋ `172115`（本轮 3→5 份，含终档） | `### check-ci-gate-paths.test.sh rc=0`、`### gitleaks(尖端提交) rc=0`；`172115` 的三档见第 7 段 |

**7 · 收口：门禁、"可跟踪 ≠ 已跟踪"那笔、以及 491 份产物第一次真正入库。** 全部**会产物的动作**先做完（[[feedback-delivery-accounting-layers]] 那条排序约束），再一次性抄数：
- `make audit` 于改名外的干净克隆 `/tmp/r45-final-gates`（HEAD `2ab6a4b9`，零脏文件）⇒ **rc=2**，红在第 9 块 `check-env-coverage.py`：`生产代码读取键 183 · 已文档化 76 · 工具进程自动豁免 16 · 基线登记 88 · 红 3`，三枚 UNDOCUMENTED 全来自 `user-server/internal/service/collection_job.go`（`FF_LTC_COLLECTION_JOB`／`LTC_COLLECTION_JOB_BATCH`／`LTC_COLLECTION_JOB_INTERVAL`）⇒ 属并行 collection 泳道**未提交**的文档面（键本身来自已提交的 `23dae260`），本泳道**不替别人补写文档行**（[[project-audit-backlog-2026-09]] 第五十二轮同判）。产物：`CIwiring/172115/00-make-audit.log`（尾部两行注码写明"本档内 9 块／`audit:` 目标现数 14 块"与 `rc=2`）。
- 第 9 块之后的五块不靠 make 的连锁、逐个直跑 ⇒ **五道全 rc=0**：`check-test-nil-deref.py`（`产码交回 (nil, nil) 的函数键＝116 · 测试确证站点＝0 · 存疑站点＝114`）、`check-async-global-read.py`（`扫 153 个 package，站点 0`）、`check-seam-guard.py`（`登记 17 个全局，扫 2763 个 .go 文件 ⇒ accessor 之外 0 处访问`）、`check-shellcheck.sh`（`scanned=138 checked=138 error=0`）、`check-shell-cjk-expansion.sh`（`命中 1 处（基线 2 处）`）。产物：`10-post9-gates.log`。
- 那枚 CJK 门的"基线条目已归零，请删掉 `scripts/check-architecture.sh`"提示**不成立**，两棵树现测并档在 `20-cjk-two-trees.log`：克隆（只含已提交字节）`scanned=138 命中 1 处`，hub 活树（HEAD `c985cdf3` ＋并行面）`scanned=139 命中 2 处` ⇒ 差的那一份 shell 文件在克隆里根本不存在，提示是"扫不到"而不是"改好了"。基线一字不动，连两向读数交回。
- **其余五份带 `--check` 的常驻电池在 `2ab6a4b9` 上逐份预检**（`--check` 只证锚点与名字，不证有牙，§23.19 第 5 段原口径）：`mut_egress_pool_r30.py` rc=0 `preflight：29 个行为格 + 1 个 race 格，0 处静态问题`、`mut_ledger_b16.py`／`b16b`／`b16c` rc=0 `预检: 锚点全部唯一、用例名全部存在`、`mut_collection_p703.py` rc=0 `基线字节：克隆 HEAD 2ab6a4b9；本卡文件已在 HEAD 8/8；装配锚点 7 处各命中 1 次`＋`锚点校验：116 格，0 格锚点有问题`。**没有 `--check` 的那五份（r22／r28／p503／p702／b17）本轮只能靠"整趟跑绿"回读，是残余盲区**（第 8 段③）。
- `.gitignore` 轮次白名单与磁盘**两向闭合**：`grep -c '^!docs.*logs/[^*]*/$' .gitignore` = **12**，`ls logs/` = **12**，`comm -3` 两列各取一方 ⇒ **零差集**；反向探针照旧（另造一枚未列入的 `logs/R99sim/` 形状目录，其内文件仍 `git check-ignore` 命中 rc=0、`git status` 0 行 ⇒ 白名单不是"放开 logs/"）。
- **本轮真正补上的一刀：488 份产物此前全部只在未跟踪集里。** 现测 `git ls-files docs/superpowers/specs/ledger/logs | wc -l` = **0**、`git ls-files -o` 同径 = **488** ⇒ §23.19 1-补 那句"0 份仍在库外"用的是"没被忽略"，而"入库"要 `git add`。本段连同新档（终数 **493 份／12 轮次／31 趟次目录**，`find -type f` 与 `ls -d */*/` 现算；含本节随后落的 `40-gitleaks-final.log` 与 `50-md-links.log` 两档，这两档之后再无产物动作）一次性 `git add` 显式路径入库；非 `.log` 文件 `find … ! -name '*.log'` 现数 **0** 份；`CapAB/20260923-133217`（空目录，驱动在写第一档前停过）已 `rmdir`——**有档无判读＝没有证据**，与 §23.19 6-补 那条同判。
- 泄露面按规矩分三处数（落盘前后各扫一遍，gitleaks 放最后）：产物里 `password=` 一律是 `password=****…` 的 maskedDSN 形状，本轮 3 档新产物在写盘处过 `scripts/redact.py::scrub()` 并现断言 `口令形状命中=0（须 0）`；`gitleaks detect --no-git --source docs/superpowers/specs/ledger/logs` 与对提交后的 `git grep HEAD` 两向读数记在终档 `CIwiring/172115/40-gitleaks-final.log`（半径要写明：`--no-git --source logs` 扫的是树内工作副本，不含 tip 提交面与已推历史，那两层各自另算，见 §23.16 第 4 段的三层口径；又因这份档自己就写在 `logs/` 里，扫描必须排在会产物的动作之后 ⇒ 终档读两向数：写档前一次、写档后一次，两向 scanned 若不等，差值只能是这份档本身）。

**8 · 本段未覆盖面（下一位从这里接手）。** ① **B 相的 32/8 只钉在 `testdb.go` 一份文件上**：`user-server/internal/pkg/db` 与 platform 侧的**生产**句柄池上界是另一件事（§23.19 第 7 段④ 的"两边都不许拿对方的绿当自己的依据"原样有效），CI 侧那 100 个名额仍要靠并行泳道的 `command: -c max_connections=400` 待办（R32⑨）。② §23.20 第 3 段那张 `paths` 摘行反向格的**原始输出已随旧档消失**，本轮没复跑（`check-ci-gate-paths.test.sh rc=0` 是其自证件，不等同那 4 条点名行）⇒ 下一位若要引用"摘一行就红"，请重跑并新开一趟档，不要引用本节。③ 五份无 `--check` 的电池（r22／r28／p503／p702／b17）没有锚点预检口，基座再漂一次要靠整趟跑才知道。④ `TestExternalOrderRepository_GetByOrderID` 与 D12 两枚既有红本段只做到"归因＋指认在途修法"，未验并行泳道那一刀是否真把断言改对（改了断言就要有新的变异格，那边目前没有）。⑤ 53300 的"CI 侧消失"只有一次单跑读数（第 3 段末），且 `--log-failed` 天然只含失败作业 ⇒ 它既不是门禁也不是回归证据；要把它变入门禁，得让 CI 现测 `pg_settings.max_connections` 并在 B 相池上界被改时点名——#63 那类"接 CI"的活儿在这一维仍未做。⑥ `make audit` 的 14 块里本轮直跑了第 9 块之后的 5 块、之前 8 块靠 make 那一趟（含一块红 ⇒ 第 9 块之后没轮到），**"链整体绿"这件事本段没有读数**；它红在别人的文档面，结掉那笔之前也不会有。⑦ 十份电池与这台 CONNECT 代理依旧没有任何 CI 执行点（§23.19 第 7 段⑥ 结转未动）。⑧ 真机/外部账号类腿（#19、#25、#26、#45、#46）仍挂。

