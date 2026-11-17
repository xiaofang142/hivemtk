# LangBot ↔ hivemtk 平台接入架构对照（谁有谁没有 / 怎么接一个新平台）

> **性质**：第三方开源项目 Anthropic 之外的横向对照调研（2026-11-17）。①LangBot 结论全部来自本地
> 浅克隆 `langbot-app/LangBot`（master，`refs/langbot`，5731 文件）；②hivemtk 结论来自本仓
> `master` 源码与在库测试；③差集清单按**渠道常量/注册表**逐项核对，不按文档宣传页核对。
> **证据标记**：`实证`=源码可直接指出（附路径）；`测试`=在库测试名；`本轮复跑`=2026-11-17 执行记录。

---

# A. LangBot 有的平台（全量，实证）

LangBot 现在有两代接入层并存：

1. **新一代 Omni / 事件总线适配器**：`src/langbot/pkg/platform/adapters/<platform>/`，
   每个目录固定 `manifest.yaml + adapter.py + event_converter.py + message_converter.py +
   api_impl.py + platform_api.py + types.py`。共 13 个目录（实证）：
   `aiocqhttp, dingtalk, discord, kook, lark, line, officialaccount, qqofficial, slack,
   telegram, wecom, wecombot, wecomcs`。
2. **旧一代 sources**：`src/langbot/pkg/platform/sources/*.py`，多出
   `matrix, mattermost, http_bot, wechatpad, satori, web_page_bot_adapter, websocket_adapter,
   openclaw_weixin`（实证）。旧源仍在用，`adapter_names.py` 的 `canonical_adapter_name`
   负责把 `aiocqhttp-eba` 归一为 `onebot-v11`、`*-eba` 归一为 `*-omni`。

官方文档（`docs/zh/usage/platforms/readme.mdx`）列 16 个平台；`docs/zh/insight/platform-features.mdx`
另有一节 Satori（标注全能力），Slack / LINE / QQ 官方三节内容为空（未测）。

## A1. 每个平台的可接入面（manifest 实证）

| 平台 | 适配器 | 事件数 | 关键事件（除 message.received/platform.specific） | 能力备注 |
|---|---|---|---|---|
| OneBot v11 | `aiocqhttp` | 12 | message.deleted、group.member_joined/left/banned、friend.request_received、friend.added、bot.invited_to_group/removed_from_group/muted/unmuted | 可选 API 最全：撤回/转发/取消息/群资料/成员/禁言/踢人/退群 |
| QQ 官方 | `qqofficial` | 8 | message.reaction、group.member_joined/left、bot.invited/removed、interaction.callback | 走 QQ 开放平台 |
| 企业微信（自建） | `wecom` | 2 | — | 只声明收消息 |
| 企业微信客服 | `wecomcs` | 8 | message.deleted、wecomcs.enter_session、msg_send_fail、servicer_status_change、session_status_change、reject_customer_msg_switch_change | 客服会话状态机 |
| 企业微信智能机器人 | `wecombot` | 6 | feedback.received、wecombot.enter_chat、wecombot.template_card_event、interaction.callback | 模板卡片交互 |
| 微信公众号 | `officialaccount` | 2 | — | required 只有 `reply_message` |
| 飞书 | `lark` | 10 | message.deleted/reaction、bot.invited/removed、group.member_joined/left、group.info_updated、interaction.callback | 卡片 + 交互回调 |
| 钉钉 | `dingtalk` | 8 | feedback.received、group.member_joined/left、group.info_updated、dingtalk.card_action、interaction.callback | 卡片动作 |
| Discord | `discord` | 10 | message.edited/deleted/reaction、group.member_joined/left、bot.invited/removed、interaction.callback | 服务器/频道模型 |
| Telegram | `telegram` | 12 | message.edited/reaction、group.member_joined/left/banned、bot.invited/removed/muted/unmuted、interaction.callback | 交互 API 最全 |
| Slack | `slack` | 10 | message.edited/deleted/reaction、group.member_joined/left、group.info_updated、bot.invited/removed | 文档能力矩阵为空 |
| LINE | `line` | 7 | — | 文档能力矩阵为空 |
| KOOK | `kook` | 2 | — | 仅收消息 |
| Mattermost | `sources/mattermost.py` | 旧源 | — | 未迁 Omni |
| Matrix | `sources/matrix.py` | 旧源 | — | 未迁 Omni |
| Satori | `sources/satori.py` | 旧源 | — | 文档标注全能力 |
| 个人微信助手 | `sources/wechatpad.py` | 旧源 | — | iPad 协议 |
| 页面机器人 | `sources/web_page_bot_adapter.py` | 旧源 | — | 可嵌网页组件 |
| HTTP Bot / WebSocket 通用入口 | `sources/http_bot.py`(685 行) / `websocket_adapter.py`(827 行) | 旧源 | — | **自带协议就能接**，是"没有的平台"的兜底 |

# B. hivemtk 已接入的平台（实证）

渠道词表：`internal/model/message_event.go` 的 `Channel*` 常量（web, telegram, whatsapp,
xiaohongshu, wecom, xianyu, douyin, kuaishou, tiktok, jd, bilibili, weibo, personal_wx, pdd,
email, feishu, dingtalk, qq）。接入形态两种：

| 形态 | 组件 | 平台 |
|---|---|---|
| 官方 API / webhook | `service/webhook*.go` + `channelbot/{telegram,whatsapp,qq}` | telegram、whatsapp、wecom、wechat（公众号）、feishu、dingtalk、qq、custom |
| 浏览器扩展桥（蜂桥） | `channelgw` + `user-web/bridge/src/channels/*` + outbox | douyin、xiaohongshu、kuaishou、tiktok、xianyu、jd、bilibili、weibo、personal_wx、pdd |
| 站内控件 | `embed-sdk` + `ChatChannel` | web |
| 触达 | `sms`、`email` | 短信、邮件 |

# C. 差集清单：LangBot 有、hivemtk 没有（完整）

按「缺口真实性 + 复用价值」排序。`落地成本`按 hivemtk 现有五层架构估算（不改架构的前提下）。

| # | 平台/形态 | LangBot 证据 | hivemtk 缺口 | 成本 | 建议 |
|---|---|---|---|---|---|
| 1 | **OneBot v11**（NapCat/Lagrange/LLOneBot） | `adapters/aiocqhttp/manifest.yaml` 12 事件、可选 API 最全 | hivemtk 的 `qq` 是**官方机器人**（bot.q.qq.com），不是 QQ 个人号/群机器人。国内私域最常用的一类接入完全缺失 | 中 | P1：`channelbot/onebot` 纯客户端 + `webhook_channel_onebot.go`（HTTP/WS 双形态） |
| 2 | **企业微信客服（wecomcs）** | `adapters/wecomcs/` 8 事件，含会话状态/接待人员状态 | 只有自建应用 `wecom` | 中 | P1：同属企微生态，客户/会话语义与现有 inbox 最贴合 |
| 3 | **企业微信智能机器人（wecombot）** | `adapters/wecombot/` 6 事件、模板卡片 | 无 | 中 | P2：等客户确有机器人场景再做 |
| 4 | **Slack** | `adapters/slack/` 10 事件 | 无 | 中 | P1（海外/团队协作场景） |
| 5 | **Discord** | `adapters/discord/` 10 事件 | 无 | 中 | P2 |
| 6 | **LINE** | `adapters/line/` 7 事件 | 无 | 中 | P2 |
| 7 | **KOOK** | `adapters/kook/` 2 事件 | 无 | 低 | P2 |
| 8 | **Mattermost** | `sources/mattermost.py` | 无 | 低 | P3 |
| 9 | **Satori** | `sources/satori.py` | 无 | 低 | P3（协议桥，一份适配多 IM） |
| 10 | **Matrix** | `sources/matrix.py` | 无 | 中 | P3 |
| 11 | **个人微信（wechatpad / iPad 协议）** | `sources/wechatpad.py` | `personal_wx` 是桌面伴生（桥接），不是协议级 | 高（合规） | P3，合规风险先评估 |
| 12 | **页面机器人（可嵌网页组件）** | `sources/web_page_bot_adapter.py` | 已有 `web` + embed-sdk（**部分覆盖**） | 低 | 不立项：先按 `channelcaps` 对齐能力口径 |
| 13 | **通用 HTTP Bot / WebSocket 入口** | `sources/http_bot.py`、`websocket_adapter.py` | 有 `custom` webhook 与 `channelgw`，但 channelgw 只服务浏览器扩展、`custom` 未成体系 | 低 | P1：把 `custom` 升级成"自带协议即可接"的正式入口（见 §F） |
| 14 | **openclaw_weixin** | `sources/openclaw_weixin.py` | 无 | — | 不立项（实验源） |

> 结论：**真正的平台级缺口是 8 个**（OneBot v11、wecomcs、wecombot、Slack、Discord、LINE、
> KOOK、Matrix），另外 2 个是形态缺口（Satori / 通用 HTTP-WS 入口），1 个是部分覆盖（页面组件）。

# D. LangBot 架构清单（可借鉴的六件事）

## D1. manifest 驱动的能力声明（最值得抄的一条）

`adapters/<platform>/manifest.yaml` 是**唯一事实源**，框架从它读取并对外暴露：

```yaml
metadata: { name: telegram-omni, label: { zh_Hans: 电报 }, spec: { categories: [...] } }
spec:
  config: { token: {required: true}, ... }      # 凭据表单
supported_events: [message.received, message.edited, ...]
supported_apis:
  required: [send_message, reply_message]
  optional: [edit_message, delete_message, forward_message, get_group_info, ...]
platform_specific_apis: [pin_message, unpin_message, ...]
execution: { python: { path: ./adapter.py, attr: TelegramAdapter } }
```

框架侧 `AbstractPlatformAdapter.get_supported_events()/get_supported_apis()`
（`pkg/platform/definition/abstract/platform/adapter.py`）默认读 manifest；未实现的可选
方法统一抛 `NotSupportedError`。**能力声明与实现同目录、同版本**，不存在"文档说支持、
代码没写"的漂移空间。

## D2. 适配器契约（薄接口）

```python
class AbstractPlatformAdapter(BaseModel, ABC):
    bot_account_id; config; logger
    async def send_message(target_type, target_id, message) -> MessageResult   # 抽象
    async def reply_message(event, message, quote_origin)                      # 抽象
    async def run_async(); async def kill()                                    # 生命周期
    def register_listener(event_type, callback) / unregister_listener(...)
    # 可选：edit_message / delete_message / forward_message / get_group_info /
    #       get_group_member_list / call_platform_api -> NotSupportedError
    # 流式：reply_message_chunk / is_stream_output_supported
```

## D3. 事件转换器（原生事件 → 统一事件）

`AbstractEventConverter.target2yiri(raw_event) -> Optional[Event]`，按事件类型分派
（message / edited_message / chat_member / callback_query / reaction…）。平台差异全部
收在 `event_converter.py`，上层只认统一事件。

## D4. 事件路由与绑定（botmgr.py，2515 行）

`RuntimeBot` 负责：事件 → pipeline 目标解析（`get_pipeline_target_for_event_type`）、
agent/plugin 事件绑定与过滤（`_evaluate_eba_event_bindings` / `_resolve_eba_event_binding`
/ `diagnose_eba_event_binding`）、路由追踪（`_record_event_route_trace`）、
actor/subject 推断、回复目标推断。**同一平台的不同事件可以绑到不同处理器**，
而不是"一条消息流打天下"。

## D5. Pipeline 阶段化

`pkg/pipeline`：preproc → cntfilter（敏感词/内容审核）→ ratelimit → process（chat/command、
流式结果）→ respback → resprule → longtext（长文转发/图片策略）→ bansess → aggregator
（消息聚合）→ pool → controller。每段可替换。

## D6. 插件 SDK 与发现

适配器/插件都靠 `ComponentDiscoveryEngine` 扫目录 + manifest 注册，新增平台不需要改核心
调度代码；`adapter_names.py` 维护 canonical 名字映射。

# E. hivemtk 现状与差异

| 维度 | LangBot | hivemtk | 差异结论 |
|---|---|---|---|
| 能力声明 | 每平台 manifest.yaml（事件/API/配置/执行入口） | `capregistry` 只有 7 种粗能力（dm.receive/dm.send/webhook/lead.mine/kb.sync/sms/email） | **缺消息级能力表** → 本轮补 `internal/channelcaps` |
| 适配器契约 | 抽象基类 + 可选方法 NotSupportedError | `IngressHandler` 单方法 + 各渠道自己的 Parse/ToInbound | 我方更薄，但**入站适配器没有统一"能力自述"** |
| 事件模型 | 12+ 种平台事件 | `MessageEvent` 只有消息（编辑/撤回/入群/回调另走分支） | 事件维度少，短期够用 |
| 路由 | 事件类型 → pipeline/agent 绑定 | webhook 渠道 switch + AI 触发器 | 缺"按事件类型绑定处理器"，属 P2 |
| 新平台接入 | 复制目录 + 写 manifest + 实现 5 个方法 | 改 6~8 处（见 §F） | **我方接入成本高，是本节要解决的问题** |
| 能力防漂移 | manifest 即实现 | `capregistry` 有文档新鲜度测试 | 我方已有同类机制，可复用套路 |

# F. 接入配方：给 hivemtk 加一个"现在没有"的平台

以 **OneBot v11** 为例（同理适用 Slack/Discord/LINE/KOOK）。目标：**只加文件，不改架构**。

```
① 词表      internal/model/message_event.go          加 ChannelOneBot = "onebot"
② 能力      internal/capregistry/channels.go         登记 webhook/dm.receive/dm.send
            internal/channelcaps/manifests.go        登记消息级能力（本轮新增）
③ 协议      internal/channelbot/onebot/onebot.go     BaseClient + 验签 + ParseEvent + ToInbound
④ 入站      internal/service/webhook_channel_onebot.go VerifyXxx / dispatchXxx
            internal/service/webhook.go               dispatchToChannel 加一个 case（+ 能力门禁）
⑤ 出站      internal/service/webhook_outbound.go      sendOutbound 加一个 case（或接入 §G 的 sender 注册表）
⑥ 路由      internal/router/*.go                      /api/webhook/onebot/:account_id
⑦ 配置      integration_accounts 的 channel 枚举 + 管理台渠道选项
⑧ 测试      channelbot 单测（Parse/ToInbound/验签失败）
            service 全链路（webhook 收 → hub 落库 → 回复出站）
            capregistry 文档新鲜度（UPDATE_GOLDEN=1）
```

**验收口径**（与现有渠道一致，缺一不可）：

- 验签失败/时间窗过期 → 4xx 且不落库（`webhook_channel_qq.go` 是同款模板）；
- 同一平台消息重投 → `msg_id` 幂等，不重复入库、不重复触发 AI；
- 群聊 @ 机器人 / 私聊两种会话都能落 `inbox_conversations`；
- 出站失败必须返回 `*ChannelError`（可重试/不可重试分类），不能 `(false, nil)`；
- `capregistry` + `channelcaps` 两本文档由测试生成，手工改文档即红。

**若平台协议不在预期内**（LangBot 的 `http_bot.py` 思路）：优先把 `custom` 渠道补成
正式入口——固定信封（event_id/channel/conversation_id/sender/content/msg_type/timestamp）
+ HMAC 验签 + `IngressHandler`，第三方按信封对接即可，不必为每个长尾平台写 Go 适配器。

# G. 本轮交付与待立项

| 项 | 状态 | 落点 |
|---|---|---|
| 渠道消息级能力表（manifest 化，文档由测试钉死） | **本轮已交付** | `internal/channelcaps/{channelcaps.go,manifests.go,channelcaps_test.go}`、`docs/channel-capabilities.md` |
| 富卡判定改读能力表 | 待立项（`channel_card_delivery.go` 目前尚未入库，避免把无关未提交文件混进本次提交；接线为 `channelcaps.Has(channel, FeatureRichCard)` 一行） |
| 平台差集完整清单 | **本轮已交付** | 本文 §C |
| 接入配方 | **本轮已交付** | 本文 §F |
| 出站 sender 注册表（新平台=注册一个 sender） | 待立项 | 见 `CHATWOOT-MESSAGE-AGGREGATION-VS-HIVEMTK-2026-11.md` §D P0-3 |
| OneBot v11 / Slack / wecomcs 实接 | 待立项 | 按 §F 配方，每个平台独立 PR |
