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
| douyin_shop | `dm.receive` | bridge | 抖店飞鸽客服（im.jinritemai.com）；Experimental，选择器待真机 |
| douyin_shop | `dm.send` | bridge | outbox 下发；Experimental |
| email | `email` | smtp | 需后台配置 SMTP 授权码 |
| feishu | `webhook` | webhook |  |
| ima | `kb.sync` | kbconnector | 腾讯 IMA OpenAPI；笔记类平台不提供导出，仅文件类 |
| jd | `dm.receive` | bridge | 京麦/咚咚网页私信；Experimental，选择器待真机 |
| jd | `dm.send` | bridge | outbox 下发；Experimental |
| kuaishou | `dm.receive` | bridge |  |
| kuaishou | `dm.send` | bridge |  |
| kuaishou_shop | `dm.receive` | bridge | 快手小店快语客服（im.kwaixiaodian.com）；Experimental，选择器待真机 |
| kuaishou_shop | `dm.send` | bridge | outbox 下发；Experimental |
| onebot | `dm.send` | ws | 出站优先反向 WS，未连接时回落 HTTP /send_private_msg\|/send_group_msg |
| onebot | `webhook` | webhook | HTTP 上报 + 反向 WS；Bearer token 校验 |
| pdd | `dm.receive` | bridge | 拼多多商家网页（mms.pinduoduo.com/chat-merchant）；Experimental，选择器待真机；EC-16 commerce cells 未登记 |
| pdd | `dm.send` | bridge | outbox 下发；Experimental；EC-16 commerce cells 未登记 |
| personal_wx | `dm.receive` | bridge | 桌面伴生上报；伴生未交付（外部工程） |
| personal_wx | `dm.send` | bridge | outbox 下发；伴生未交付（外部工程） |
| shopee | `dm.receive` | bridge | Shopee Seller Centre Webchat；Experimental，选择器待真机 |
| shopee | `dm.send` | bridge | outbox 下发；Experimental |
| slack | `dm.send` | api | chat.postMessage，回复沿用 thread_ts |
| slack | `webhook` | webhook | Events API v0 签名 + 5 分钟时间窗 |
| sms | `sms` | aliyun-sms | 需后台配置 AccessKey/签名/模板 |
| taobao | `dm.receive` | bridge | 千牛网页接待中心（work.taobao.com）；Experimental，选择器待真机；EC-16 commerce cells 未登记 |
| taobao | `dm.send` | bridge | outbox 下发；Experimental；EC-16 commerce cells 未登记 |
| telegram | `dm.send` | api |  |
| telegram | `webhook` | webhook | PUBLIC_BASE_URL 配置时 webhook，否则 polling 单实例 |
| tiktok | `dm.receive` | bridge | 海外链路 |
| tiktok | `dm.send` | bridge | 海外链路 |
| walmart_us | `dm.receive` | bridge | Walmart Seller Center Inbox；Experimental，选择器待真机 |
| walmart_us | `dm.send` | bridge | outbox 下发；Experimental |
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

## 动作级能力格默认声明

> Experimental 及其他静态声明只描述候选范围，不授予店铺权限；生产放行需匹配租户/店铺/动作证据。

| 产品 | 地区 | 动作 | 载体 | 执行模式 | 必需 scope | 状态限制 | 接口版本 | 文档版本 | 默认实验性 |
|---|---|---|---|---|---|---|---|---|---|
| bilibili | CN | `message.text.send` | bridge | bridge | message.text.send |  | bridge-v1 | design-cell-v1 | 是 |
| douyin | CN | `message.image.send` | bridge | bridge | message.image.send |  | bridge-v1 | design-cell-v1 | 是 |
| douyin | CN | `message.text.send` | bridge | bridge | message.text.send |  | bridge-v1 | design-cell-v1 | 是 |
| douyin_shop | CN | `aftersale.write` | api_adapter | api_adapter | aftersale.write | platform_preflight_required | api-v1 | design-cell-v1 | 是 |
| douyin_shop | CN | `message.image.send` | bridge | bridge | message.image.send |  | bridge-v1 | design-cell-v1 | 是 |
| douyin_shop | CN | `message.text.send` | bridge | bridge | message.text.send |  | bridge-v1 | design-cell-v1 | 是 |
| douyin_shop | CN | `order.address.change` | api_adapter | api_adapter | order.address.change | platform_preflight_required | api-v1 | design-cell-v1 | 是 |
| douyin_shop | CN | `refund.write` | api_adapter | api_adapter | refund.write | platform_preflight_required | api-v1 | design-cell-v1 | 是 |
| jd | CN | `message.text.send` | bridge | bridge | message.text.send |  | bridge-v1 | design-cell-v1 | 是 |
| jd_pop | CN | `aftersale.write` | official_ui | official_ui |  | approved_scope_required, native_action_available | m01-workflow-v1 | m01-7.3.5 | 否 |
| kuaishou | CN | `message.text.send` | bridge | bridge | message.text.send |  | bridge-v1 | design-cell-v1 | 是 |
| personal_wx | CN | `message.text.send` | bridge | bridge | message.text.send |  | bridge-v1 | design-cell-v1 | 是 |
| tiktok | CN | `message.text.send` | bridge | bridge | message.text.send |  | bridge-v1 | design-cell-v1 | 是 |
| vipshop | CN | `refund.observe` | platform_managed | platform_managed |  | native_case_required | m01-workflow-v1 | m01-7.3.5 | 否 |
| weibo | CN | `message.text.send` | bridge | bridge | message.text.send |  | bridge-v1 | design-cell-v1 | 是 |
| xianyu | CN | `message.text.send` | bridge | bridge | message.text.send |  | bridge-v1 | design-cell-v1 | 是 |
| xiaohongshu | CN | `message.text.send` | bridge | bridge | message.text.send |  | bridge-v1 | design-cell-v1 | 是 |

## EC-01 提交边界与生产阻塞

- connector bridge 与 Executor 的写端点验证精确能力格及可信上下文证据；消息生产者 `service.DeliverBridgeOutboundMedia`、AI 回复 `WebhookService.sendOutbound`（延迟入队前）与独立 bridge sender 的 `outboundBridge`（message_hub 写入前）共用 `capregistry.AuthorizeBridgeSubmission` 精确裁决。回复拒绝带不可重试的机器可读原因，零 outbox、quiet-hours 或 send_retry 入队。
- 原始 `InboxIngressService.DeliverOutbound` 的 outbound/default 分支写前复核，inbound 保留原逻辑。历史已入队记录在轮询 `ClaimPendingOutbound`、SSE 欠交付查询及 `OutboundDeliveryService.ClaimOutboundForPush`、显式回收 `OutboundReclaimTask.RunOnce` 中重新裁决库中租户、平台、账号与消息类型。文本与图片分别使用 message.text.send / message.image.send，事件平台/账号/类型与库行不一致返回 delivery_binding_mismatch。候选行按同租户 ID 有序分页，每页最多锁定 64 行。写入仅作用于该页已锁定且获准的行。
- 消费缺可信映射/证据、撤权、到期、scope、版本、D/I/R 或状态预检失败时保留历史状态、租约、重试预算与成功事实。轮询、SSE 欠交付查询及显式回收可继续处理其他获准行，轮询后续页失败时仍返回已提交的认领。轮询全拒绝返回 HTTP 403、status=blocked 与 reason_code，SSE 记录机器可读原因。SSE 欠交付查询可在较高 ID 已发送后恢复旧行，仅游标兼容路径在拒绝处停止并关闭即时出站推送。系统范围/无租户返回 tenant_scope_invalid。connector 即使有匹配证据也不得用系统范围发起写请求，请求构造与 HTTP 出站均为零，只读路径不变。
- `AfterSaleService.Create` 在已配置外部客户端时先于本地售后落库裁决；`HTTPAfterSaleClient.Create` 在 HTTP Do 前独立复核同一策略。退款与退货/换货分别使用 `refund.write`、`aftersale.write`，订单、平台、租户与可信店铺绑定必须一致；未配置外部客户端的本地记录模式保留。
- 所有 `bridgeOutboxCapable` 渠道（包括未来新增渠道）均受此门禁约束：精确产品/地区/动作/载体无登记格返回 `unknown_capability`，缺可信映射返回 `capability_mapping_missing`，不沿用旧放行。当前仅 douyin/douyin_shop 的 CN 文本和图片有候选桥接格，彼此不继承；其他桥接产品保持阻塞/待补精确登记与实证。
- 当前没有持久化的权威店铺映射/授权证据加载源；`bridge_accounts` 只能证明租户账号归属，`account_id` 不能当 shop_id，售后全局 URL/key/secret 也不证明订单所属店铺。生产代码没有装载 SubmissionBinding 或 CellEvidence，因此这些提交边界当前不开放生产发送；合成证据仅用于本地测试，带 Simulated 的 D/I/R 一律不能生产放行，无用户 HTTP body 注入入口。每次提交重算撤权、到期、scope、版本、D/I/R 与状态预检，不缓存旧许可。
- 非桥接直发 sender（wecom/telegram/whatsapp/qq/feishu/dingtalk/weixin，以及 slack/onebot/wecomcs/wecombot/custom）属于不同产品/载体，不会路由到上述 douyin/douyin_shop cells。本切片保留其现有账号/集成配置检查、钉钉 sessionWebhook 域名与到期检查、WhatsApp 会话窗口等渠道防护，不改造这些渠道，也不声称它们已接入 EC-01 动作台账。
- 范围限制：本次包含 01-C 历史待发记录的消费时阻塞，不执行历史迁移、不构造生产授权。可信连接/授权加载源、连接续期及真实店铺接通仍是生产依赖。服务启动时停用自动出站回收，记录一次 outbound_reclaim_evidence_source_missing。改单 `direct_api`/`via_erp` 仍由 `OrderChangeService.Execute` 明确拒绝。本矩阵不代表“所有 Submit 已受保护”或 M01 生产验收已完成。

## EC-01 受控采证与执行绑定

- CapabilityCollectionService 只接受构造注入的服务端采集器，采证前后检查当前提供方契约，精确匹配来源、动作、scope、连接及凭证修订；连接快照隔离且不含密钥。未配置采集器拒绝，无用户 HTTP 证据上传入口。
- CapabilityObservationRepository 事务锁定当前连接并重查撤权、版本、到期和授权/刷新状态，原子保存来源映射与动作证据；来源换店、已撤销映射和写入失败拒绝并回滚；并发同来源只允许一个归属。
- LocalCapabilityProofVerifier 按产品与 provenance 精确读取独立认证的本地证明，检查当前提供方契约、身份、模拟标记及有效期；数据库引用本身不构成运行证明。队列锁内不访问外部平台，默认生产不注册证明源。
- NewExecutorWithCapabilityBinding 为服务端适配器提供凭证绑定写请求接缝；先裁决、构造请求、校验目的同源与 SSRF、重核实际连接修订后出站，禁止跟随重定向。默认执行器未装配适配器时维持拒绝；本地 fake 测试不代表真实平台授权。

## EC-01 租户管理员只读摘要

- `/cap/diagnostics` 提供店铺能力摘要。租户管理员通过 `GET /api/v1/capabilities/cells` 获取登记组合，通过 `GET /api/v1/capabilities/resolve` 按产品、地区、动作、载体及独立填写的店铺 ID 查询。服务端使用登录身份中的租户。
- JSON 下载与当前界面使用同一份服务端字段白名单。摘要包含声明与观察到的 scope/版本、连接状态、动作就绪度、状态限制、阻塞原因、负责人和下一步。摘要有效期最长五分钟，并受尚有效的证据到期时间限制。提交时仍重新裁决。
- §7.3.5 的京东 POP 售后由商户操作员在京麦官方工作台处理。唯品会由平台处理原生售后和退款，系统观察原案件的处理结果。两个声明格始终拒绝自动提交。
