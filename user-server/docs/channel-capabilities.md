# 渠道消息级能力表

> 本文件由 `internal/channelcaps` 生成（channelcaps.Markdown），测试钉死与注册表一致，**请勿手改**。
> 口径：只登记代码已落地的收发路径；未登记 = 未实现/未验证，出站降级与 AI 能力提示词一律按「不支持」处理。
> 附件上限只做声明与校验提示，不截断；落点在渠道客户端（inbox_ingress / channelbot），不在出站渲染层。0 = 未设上限或无附件通道，依据见「附件取证」。
> 粗粒度能力（能否接入/能否发）见 `internal/capregistry`；`ima`/`webhttp` 是知识库连接器（capregistry 只登记 `kb.sync`），没有 IM 消息路径，故不在本表；`sms`/`email` 虽非 IM，但按触达载荷登记。

| 渠道 | 名称 | 接入 | 入站消息 | 出站消息 | 平台事件 | 出站格式 | 文本上限 | 附件数 | 附件上限 | 附件取证 | 交互特征 | 说明 |
|---|---|---|---|---|---|---|---|---|---|---|---|---|
| bilibili | B 站私信 | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | `plain` | — | 1 | — | 网页私信透传 URL，服务端不下载媒体，无字节上限；单条消息一个媒体（inbox_ingress_persist 落单值 media_url） | — | Experimental |
| custom | 自定义 webhook | webhook | `audio` `card` `file` `image` `link` `text` `video` | `file` `image` `text` | `message.received` | `plain` | — | 1 | — | 契约仅一个 media_url 字段，URL 由自有系统托管，服务端不校验字节数 | — | 自有系统对接：固定 JSON 契约 + X-Hive-Signature（t=unix,v1=HMAC-SHA256） |
| dingtalk | 钉钉 | webhook | `file` `image` `link` `text` | `text` | `message.received` | `plain` | 20000 | 1 | 20MB | 钉钉消息媒体 ≤20MB，一条消息一个 media；媒体异步回填 media_url | `group` `mention` | 出站 webhook 域名受 allowlist 约束（loadDingtalkWebhookHostAllowed） |
| douyin | 抖音私信 | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | `plain` | — | 1 | — | 网页私信透传 URL，服务端不下载媒体，无字节上限；单条消息一个媒体（inbox_ingress_persist 落单值 media_url） | — | 扩展巡检上报；自声/去重由 inbox_ingress 兜底 |
| douyin_shop | 抖店飞鸽客服 | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | `plain` | — | 1 | — | 网页私信透传 URL，服务端不下载媒体，无字节上限；单条消息一个媒体（inbox_ingress_persist 落单值 media_url） | — | Experimental，选择器待真机 |
| email | 邮件 | smtp | `file` `image` `text` | `text` | `message.received` | `html` | — | — | — | SMTP 单封大小由服务商决定，本仓不校验；入站附件在 inbox_ingress_persist 压成单值 media_url | — | SMTP 需后台配置授权码 |
| feishu | 飞书 | webhook | `audio` `card` `file` `image` `link` `location` `text` `video` | `card` `text` | `group.member_joined` `group.member_left` `message.deleted` `message.received` | `plain` | — | 1 | 10MB | 飞书消息图片 ≤10MB、文件 ≤30MB（取最严 10MB）；一条消息一个附件，媒体异步回填 media_url | `group` `mention` `rich_card` | 卡片转 interactive JSON；媒体文件异步回填 media_url |
| jd | 京东（京麦/咚咚） | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | `plain` | — | 1 | — | 网页私信透传 URL，服务端不下载媒体，无字节上限；单条消息一个媒体（inbox_ingress_persist 落单值 media_url） | — | Experimental，选择器待真机 |
| kuaishou | 快手私信 | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | `plain` | — | 1 | — | 网页私信透传 URL，服务端不下载媒体，无字节上限；单条消息一个媒体（inbox_ingress_persist 落单值 media_url） | — | Experimental |
| kuaishou_shop | 快手小店快语客服 | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | `plain` | — | 1 | — | 网页私信透传 URL，服务端不下载媒体，无字节上限；单条消息一个媒体（inbox_ingress_persist 落单值 media_url） | — | Experimental，选择器待真机 |
| onebot | OneBot v11 | webhook | `audio` `file` `image` `text` `video` | `text` | `group.member_joined` `group.member_left` `message.deleted` `message.received` | `plain` | 2000 | 1 | — | 媒体段只透传 URL，服务端不下载，无统一字节上限；单条消息落单值 media_url | `group` `mention` | HTTP 上报 + 反向 WS 双通道；出站优先 WS 回落 HTTP；request 事件只入总线不自动同意 |
| pdd | 拼多多商家 | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | `plain` | — | 1 | — | 网页私信透传 URL，服务端不下载媒体，无字节上限；单条消息一个媒体（inbox_ingress_persist 落单值 media_url） | — | Experimental，选择器待真机 |
| personal_wx | 个人微信（桌面伴生） | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | `plain` | — | 1 | — | 网页私信透传 URL，服务端不下载媒体，无字节上限；单条消息一个媒体（inbox_ingress_persist 落单值 media_url） | — | 伴生为外部工程，服务端协议已就绪 |
| qq | QQ 官方机器人 | webhook | `audio` `card` `file` `image` `link` `text` `video` | `text` | `bot.invited_to_group` `bot.removed_from_group` `message.received` | `plain` | 2000 | 1 | — | 官方机器人媒体以 URL 下发，无统一字节上限；服务端落单值 media_url | `group` `mention` | bot.q.qq.com；Ed25519 验签，被动回复带 channel_msg_id |
| shopee | Shopee Seller Centre Webchat | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | `plain` | — | 1 | — | 网页私信透传 URL，服务端不下载媒体，无字节上限；单条消息一个媒体（inbox_ingress_persist 落单值 media_url） | — | Experimental，选择器待真机 |
| slack | Slack | webhook | `file` `image` `link` `text` | `text` | `message.deleted` `message.edited` `message.received` | `plain` | 40000 | 1 | — | 入站文件只透传 Slack 私有 URL，不下载；出站只投文本（原生文件上传不在 sender 范围） | `group` `mention` | Events API 签名 + 5 分钟时间窗；忽略 bot_id/subtype=bot_message；回复沿用 thread_ts |
| sms | 短信 | aliyun-sms | — | `text` | `message.received` | `plain` | 500 | — | — | 短信无附件通道（Outbound 仅 text） | — | 仅触达，无入站回调 |
| taobao | 千牛网页接待中心 | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | `plain` | — | 1 | — | 网页私信透传 URL，服务端不下载媒体，无字节上限；单条消息一个媒体（inbox_ingress_persist 落单值 media_url） | — | Experimental，选择器待真机 |
| telegram | Telegram | webhook | `audio` `card` `file` `image` `link` `location` `text` `video` | `card` `text` | `group.member_joined` `group.member_left` `interaction.callback` `message.edited` `message.received` | `html` | 4096 | 10 | 20MB | Bot API sendMediaGroup 单组 1–10 个 media；getFile 下载 ≤20MB | `group` `interaction` `mention` `rich_card` | 群聊 @ 提及走 TelegramReplyMeta；卡片为真载体（outboundSupportsRichCards） |
| tiktok | TikTok 私信 | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | `plain` | — | 1 | — | 网页私信透传 URL，服务端不下载媒体，无字节上限；单条消息一个媒体（inbox_ingress_persist 落单值 media_url） | — | 海外链路 |
| walmart_us | Walmart Seller Center Inbox | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | `plain` | — | 1 | — | 网页私信透传 URL，服务端不下载媒体，无字节上限；单条消息一个媒体（inbox_ingress_persist 落单值 media_url） | — | Experimental，选择器待真机 |
| web | 站内网页控件 | widget | `file` `image` `text` | `text` | `message.received` | `markdown` | — | 1 | 20MB | user-web/src/views/chat/embed/components/ChatInput.vue:186 拦截 file.size > 20MB；单次选择 1 个文件 | — | embed-sdk 直连，无第三方验签 |
| wechat | 微信公众号 | webhook | `audio` `image` `link` `location` `text` `video` | `text` | `message.received` | `plain` | 2048 | 1 | 10MB | 公众号客服消息 media_id ≤10MB；一条消息一个媒体 | — | 客服消息通道；公众号无群聊能力 |
| wecom | 企业微信（自建应用） | webhook | `audio` `file` `image` `link` `location` `text` `video` | `text` | `message.received` | `plain` | 2048 | 1 | 20MB | 企微应用消息 media 素材 ≤20MB；一条消息一个 media_id | `group` `mention` | XML 信封 msg_signature 恒时比较 + 时间窗验签 |
| wecombot | 企业微信群机器人 | webhook | `card` `image` `link` `text` | `text` | `message.received` | `markdown` | 4096 | 1 | — | markdown 载荷不携带附件；入站图片只透传 URL，服务端不下载 | `group` `mention` | 回调 AES-CBC 解密 + 签名校验；出站走群机器人 webhook（markdown） |
| wecomcs | 企业微信客服 | webhook | `audio` `card` `file` `image` `link` `location` `text` `video` | `card` `image` `link` `text` | `message.received` | `plain` | 2048 | 1 | 10MB | 微信客服 send_msg image 走临时素材 media_id，图片 ≤10MB；一条消息一个 media_id | `mention` | 回调验签后按 cursor 增量拉取；send_msg 受 48 小时会话窗口约束（95021/95026/95019） |
| weibo | 微博私信 | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | `plain` | — | 1 | — | 网页私信透传 URL，服务端不下载媒体，无字节上限；单条消息一个媒体（inbox_ingress_persist 落单值 media_url） | — | Experimental |
| whatsapp | WhatsApp Cloud | webhook | `audio` `file` `image` `link` `location` `text` `video` | `text` | `message.received` | `plain` | 4096 | 1 | 5MB | Cloud API 每条消息 1 个 media；image 5MB / video 16MB / document 100MB（取最严 5MB） | `group` | 24 小时窗口外用模板消息；出站当前只提交文字载荷 |
| xianyu | 闲鱼 | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | `plain` | — | 1 | — | 网页私信透传 URL，服务端不下载媒体，无字节上限；单条消息一个媒体（inbox_ingress_persist 落单值 media_url） | — |  |
| xiaohongshu | 小红书私信 | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | `plain` | — | 1 | — | 网页私信透传 URL，服务端不下载媒体，无字节上限；单条消息一个媒体（inbox_ingress_persist 落单值 media_url） | — | 同抖音桥接链路 |
