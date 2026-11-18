# 渠道能力矩阵

> 本文件由 `internal/capregistry` 生成（capregistry.MatrixMarkdown），测试钉死与注册表一致，**请勿手改**。改能力先改注册表。

| 平台 | 能力 | 接入方式 | 说明 |
|---|---|---|---|
| bilibili | `dm.receive` | bridge | message.bilibili.com；Experimental |
| bilibili | `dm.send` | bridge | outbox 下发；Experimental |
| custom | `dm.send` | webhook | POST callback_url 同款签名；4xx 不重试、5xx/超时可重试 |
| custom | `webhook` | webhook | 固定 JSON 契约 + X-Hive-Signature |
| douyin | `dm.receive` | bridge | 蜂桥扩展巡检上报；自声/去重由服务端 inbox_ingress 兜底 |
| douyin | `dm.send` | bridge | outbox 下发，扩展回写输入框发送（会话漂移守卫拒误发） |
| douyin | `lead.mine` | unified-miner | 关键词打分 + LLM 复核（lead.llm_refine_enabled） |
| email | `email` | smtp | 需后台配置 SMTP 授权码 |
| feishu | `webhook` | webhook |  |
| ima | `kb.sync` | kbconnector | 腾讯 IMA OpenAPI；笔记类平台不提供导出，仅文件类 |
| jd | `dm.receive` | bridge | 京麦/咚咚网页私信；Experimental，选择器待真机 |
| jd | `dm.send` | bridge | outbox 下发；Experimental |
| kuaishou | `dm.receive` | bridge |  |
| kuaishou | `dm.send` | bridge |  |
| onebot | `dm.send` | ws | 出站优先反向 WS，未连接时回落 HTTP /send_private_msg\|/send_group_msg |
| onebot | `webhook` | webhook | HTTP 上报 + 反向 WS；Bearer token 校验 |
| pdd | `dm.receive` | bridge | 拼多多商家网页（mms.pinduoduo.com/chat-merchant）；Experimental，选择器待真机 |
| pdd | `dm.send` | bridge | outbox 下发；Experimental |
| personal_wx | `dm.receive` | bridge | 桌面伴生上报；伴生未交付（外部工程） |
| personal_wx | `dm.send` | bridge | outbox 下发；伴生未交付（外部工程） |
| slack | `dm.send` | api | chat.postMessage，回复沿用 thread_ts |
| slack | `webhook` | webhook | Events API v0 签名 + 5 分钟时间窗 |
| sms | `sms` | aliyun-sms | 需后台配置 AccessKey/签名/模板 |
| telegram | `dm.send` | api |  |
| telegram | `webhook` | webhook | PUBLIC_BASE_URL 配置时 webhook，否则 polling 单实例 |
| tiktok | `dm.receive` | bridge | 海外链路 |
| tiktok | `dm.send` | bridge | 海外链路 |
| webhttp | `kb.sync` | kbconnector | 通用 HTTP 清单端点 |
| wechat | `dm.send` | api | 客服消息；触达侧渠道码 weixin 同源 |
| wechat | `webhook` | webhook | 公众号回调验签（/api/webhook/wechat/:account_id） |
| wecom | `dm.send` | api |  |
| wecom | `webhook` | webhook | XML 信封验签（msg_signature 恒时比较 + 时间窗） |
| wecombot | `dm.send` | webhook | 群机器人 webhook，markdown 载荷 |
| wecombot | `webhook` | webhook | AES-CBC 解密 + 签名校验 |
| wecomcs | `dm.send` | api | send_msg；48 小时会话窗口外拒发（window_expired） |
| wecomcs | `webhook` | webhook | 回调验签 + sync_msg 游标增量拉取 |
| weibo | `dm.receive` | bridge | weibo.com/chat；Experimental |
| weibo | `dm.send` | bridge | outbox 下发；Experimental |
| whatsapp | `webhook` | webhook |  |
| xianyu | `dm.receive` | bridge |  |
| xianyu | `dm.send` | bridge |  |
| xiaohongshu | `dm.receive` | bridge | 同抖音桥接链路 |
| xiaohongshu | `dm.send` | bridge | 同抖音桥接链路 |
| xiaohongshu | `lead.mine` | unified-miner |  |
