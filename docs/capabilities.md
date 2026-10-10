# 渠道能力矩阵

> 本文件由 `internal/capregistry` 生成（capregistry.MatrixMarkdown），测试钉死与注册表一致，**请勿手改**。改能力先改注册表。

| 平台 | 能力 | 接入方式 | 说明 |
|---|---|---|---|
| douyin | `dm.receive` | bridge | 蜂桥扩展巡检上报；自声/去重由服务端 inbox_ingress 兜底 |
| douyin | `dm.send` | bridge | outbox 下发，扩展回写输入框发送（会话漂移守卫拒误发） |
| douyin | `lead.mine` | unified-miner | 关键词打分 + LLM 复核（lead.llm_refine_enabled） |
| email | `email` | smtp | 需后台配置 SMTP 授权码 |
| feishu | `webhook` | webhook |  |
| ima | `kb.sync` | kbconnector | 腾讯 IMA OpenAPI；笔记类平台不提供导出，仅文件类 |
| kuaishou | `dm.receive` | bridge |  |
| kuaishou | `dm.send` | bridge |  |
| sms | `sms` | aliyun-sms | 需后台配置 AccessKey/签名/模板 |
| telegram | `dm.send` | api |  |
| telegram | `webhook` | webhook | PUBLIC_BASE_URL 配置时 webhook，否则 polling 单实例 |
| tiktok | `dm.receive` | bridge | 海外链路 |
| tiktok | `dm.send` | bridge | 海外链路 |
| webhttp | `kb.sync` | kbconnector | 通用 HTTP 清单端点 |
| wecom | `dm.send` | api |  |
| wecom | `webhook` | webhook | XML 信封验签（msg_signature 恒时比较 + 时间窗） |
| whatsapp | `webhook` | webhook |  |
| xianyu | `dm.receive` | bridge |  |
| xianyu | `dm.send` | bridge |  |
| xiaohongshu | `dm.receive` | bridge | 同抖音桥接链路 |
| xiaohongshu | `dm.send` | bridge | 同抖音桥接链路 |
| xiaohongshu | `lead.mine` | unified-miner |  |
