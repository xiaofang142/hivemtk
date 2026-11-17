# 渠道消息级能力表

> 本文件由 `internal/channelcaps` 生成（channelcaps.Markdown），测试钉死与注册表一致，**请勿手改**。
> 口径：只登记代码已落地的收发路径；未登记 = 未实现/未验证，出站降级与 AI 能力提示词一律按「不支持」处理。

| 渠道 | 名称 | 接入 | 入站消息 | 出站消息 | 平台事件 | 交互特征 | 说明 |
|---|---|---|---|---|---|---|---|
| bilibili | B 站私信 | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | — | Experimental |
| custom | 自定义 webhook | webhook | `file` `image` `text` | `text` | `message.received` | — | 自有系统对接，载荷按 WebhookService 约定 |
| dingtalk | 钉钉 | webhook | `file` `image` `link` `text` | `text` | `message.received` | `group` `mention` | 出站 webhook 域名受 allowlist 约束（loadDingtalkWebhookHostAllowed） |
| douyin | 抖音私信 | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | — | 扩展巡检上报；自声/去重由 inbox_ingress 兜底 |
| email | 邮件 | smtp | `file` `image` `text` | `text` | `message.received` | — | SMTP 需后台配置授权码 |
| feishu | 飞书 | webhook | `audio` `card` `file` `image` `link` `location` `text` `video` | `card` `text` | `group.member_joined` `group.member_left` `message.deleted` `message.received` | `group` `mention` `rich_card` | 卡片转 interactive JSON；媒体文件异步回填 media_url |
| jd | 京东（京麦/咚咚） | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | — | Experimental，选择器待真机 |
| kuaishou | 快手私信 | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | — | Experimental |
| pdd | 拼多多商家 | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | — | Experimental，选择器待真机 |
| personal_wx | 个人微信（桌面伴生） | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | — | 伴生为外部工程，服务端协议已就绪 |
| qq | QQ 官方机器人 | webhook | `audio` `card` `file` `image` `link` `text` `video` | `text` | `bot.invited_to_group` `bot.removed_from_group` `message.received` | `group` `mention` | bot.q.qq.com；Ed25519 验签，被动回复带 channel_msg_id |
| sms | 短信 | aliyun-sms | — | `text` | `message.received` | — | 仅触达，无入站回调 |
| telegram | Telegram | webhook | `audio` `card` `file` `image` `link` `location` `text` `video` | `card` `text` | `group.member_joined` `group.member_left` `interaction.callback` `message.edited` `message.received` | `group` `interaction` `mention` `rich_card` | 群聊 @ 提及走 TelegramReplyMeta；卡片为真载体（outboundSupportsRichCards） |
| tiktok | TikTok 私信 | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | — | 海外链路 |
| web | 站内网页控件 | widget | `file` `image` `text` | `text` | `message.received` | — | embed-sdk 直连，无第三方验签 |
| wechat | 微信公众号 | webhook | `audio` `image` `link` `location` `text` `video` | `text` | `message.received` | — | 客服消息通道；公众号无群聊能力 |
| wecom | 企业微信（自建应用） | webhook | `audio` `file` `image` `link` `location` `text` `video` | `text` | `message.received` | `group` `mention` | XML 信封 msg_signature 恒时比较 + 时间窗验签 |
| weibo | 微博私信 | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | — | Experimental |
| whatsapp | WhatsApp Cloud | webhook | `audio` `file` `image` `link` `location` `text` `video` | `text` | `message.received` | `group` | 24 小时窗口外用模板消息；出站当前只提交文字载荷 |
| xianyu | 闲鱼 | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | — |  |
| xiaohongshu | 小红书私信 | bridge | `image` `link` `text` `video` | `image` `text` | `message.received` | — | 同抖音桥接链路 |
