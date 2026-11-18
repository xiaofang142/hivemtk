# 渠道账号注册与配置接入总指南（2026-11-18）

> **用途**：每个渠道的「①账号注册入口与所需资料 ②注册后拿到什么凭证 ③在 HiveMTK 里怎么配置接入 ④怎么验收」四段式总指南。任务三交付物。
> **字段口径**：配置字段全部对照 `user-server/internal/model/` 现行模型（表名列名以代码为准）；注册入口/资料清单以官方现行页面为准（历史调研=知识库口径，注册时复核）。
> **验收口径**：无账号/未部署阶段一律**模拟信号验收**（httptest 回放 + AcceptanceSim 全链 + fake 契约集，`specs/025-im-platforms/delivery.md` §四）；真实收发=真机四证据（DOM 发送动作/桥端 ack/平台受理回执/授权接收端可见），随账号/生产资源到位执行，**不作为链路完整性阻塞**（2026-11-18 用户令口径）。
> **配套**：渠道×载体支持矩阵 `docs/integration/CHANNEL-GAP-ANALYSIS-2026-11.md` §11；API 协议细则 `docs/research/API-CHANNEL-PUBLIC-PROTOCOLS-2026-11.md`；任务跟踪 `外层 docs/未完成任务清单.md`。

---

## 〇、接入形态总览（四类，决定你在哪张表建账号）

| 形态 | 机制 | 账号落点 | 出站路径 | 渠道 |
|---|---|---|---|---|
| **A 官方 API 回调** | 平台 webhook → 服务端验签 → 统一入站 | 各渠道专属账号表（webhook_accounts 族） | 渠道直发 sender（024 B1 注册表） | telegram/whatsapp/wecom/dingtalk/feishu/qq/wechat/wecomcs/onebot/slack/wecombot |
| **B 蜂桥网页桥** | Chrome 扩展采集页面私信 → POST /api/bridge/ingest | `bridge_accounts`（首报自动注册） | bridge_outbox（扩展认领→页面发送→ack） | douyin/xiaohongshu/tiktok/kuaishou/xianyu/jd/bilibili/weibo/pdd |
| **C 店铺桥骨架** | 同 B（入口域已注册，适配器 fail-closed） | 同 B | 同 B | taobao/douyin_shop/kuaishou_shop/shopee/walmart_us（真机校准后开闸） |
| **D 通用 custom** | 自有系统 POST /api/webhook/custom/{account_id}（HMAC） | `custom_accounts` | 出站回调（签名） | 自有 CRM/小程序/任何能发 webhook 的系统 |
| **E Web Widget** | embed-sdk 访客端 | `chat_channels`（app_key/app_secret） | 直回复 | web/web_embed |

注册表单源：`internal/channelgw/registry.go` Default()（2026-11-18 起 **15 桥接渠道** + API 渠道族），渠道白名单/能力位/实验标注全以它为准。

## 一、接入前统一准备（所有渠道通用，一次填全）

**账号补填模板**（CS-R1 同款，逐渠道复用；填入对应渠道行即解锁真机验收）：

> 平台｜店铺/账号标识｜账号持有人｜测试执行人｜审批操作人｜授权时段（写明时区）｜子账号/角色与可授客服权限｜可用消息类型（先文本）｜可用模型与版本｜发布知识来源与版本（禁「最新」含糊）｜知识 owner｜暂停止发联系人

**通用纪律**：只测授权会话；不给无关真实客户发信；生产窗不重置账号/数据；发送成功但 ack 丢失先核对再重试；结果不明不自动重发（见 §六）。

---

## 二、官方 API 回调渠道（A 包）

### 2.1 Telegram（`telegram_accounts`）

- **注册**：Telegram 内找 @BotFather → `/newbot` → 起名 → **拿到 bot token（`123456:ABC-...` 格式，只显示一次）**。免费，即时生效，无需企业资质。
- **拿到的凭证**：bot_token、bot_username。
- **HiveMTK 配置**：用户端「渠道管理」新建 Telegram 账号 → 填 `bot_token`/`account_name` → 开启 webhook（系统向 Telegram `setWebhook` 登记 `https://<域名>/api/webhook/telegram/{account_id}`，`webhook_secret` 服务端自动生成用于入站验签）。
- **验收**：模拟信号已过（025 前交付面）；真机=给 bot 发消息→AI 回复→平台可见。

### 2.2 WhatsApp Cloud API（`whatsapp_accounts`）

- **注册**：developers.facebook.com → Meta 开发者账号（需实名+手机验证）→ 创建 App（类型=Business）→ WhatsApp → 添加电话号码（新号或转接）→ **拿到 phone_number_id + 永久 access_token（System User token，需商务验证）**。商务验证免费档即可收发（24h 客服窗口+250 会话/月免费额度，2026 口径）。
- **拿到的凭证**：phone_number_id、access_token、app_id、app_secret。
- **HiveMTK 配置**：新建 WhatsApp 账号 → 填 `phone_id`/`token`/`app_id`/`app_secret` → Meta App 后台 Webhooks 订阅 messages → 回调 URL 指向服务端 WhatsApp 路由（verify token=应用配置）。
- **验收**：真机=WhatsApp 真号收发（P-5 终态登记：生产 .env 注入后真发信验证）。

### 2.3 企业微信 wecom（`wecom_accounts`）

- **注册**：work.weixin.qq.com → 企业管理后台 → 我的企业（拿 **corp_id**）→ 应用管理 → 创建自建应用（拿 **agent_id + agent_secret**）。免费，企业主体或个人可建（个人可建无认证企业）。
- **拿到的凭证**：corp_id、corp_secret、agent_id、agent_secret。
- **HiveMTK 配置**：新建企业微信账号 → 填四件；接收消息模式需在应用后台配「接收消息」回调（Token/EncodingAESKey 与应用一致）。
- **验收**：模拟信号已过；真机=企业成员在企微内与应用会话。

### 2.4 企微客服 wecomcs（`wecomcs_accounts`，specs/025）

- **注册**：企微管理后台 → 应用管理 → **微信客服** → 创建客服账号 → **拿到 open_kfid**；「通过 API 管理捎带回调」处拿 **回调 Token + EncodingAESKey**；API 专用 secret（在微信客服 API 区创建）。
- **拿到的凭证**：corp_id、客服 secret、open_kfid、Token、AESKey。
- **HiveMTK 配置**：`wecomcs_accounts` 字段=`corp_id`/`secret`/`open_kfid`/`token`/`aes_key`；入站走「拉取消息」（sync_cursor 增量，`sync_msg` API），出站 `send_msg`（auth 大小写敏感）。**服务端为主动拉取型——无需公网回调 URL，内网可跑**。
- **验收**：模拟信号已过（httptest 平台两页回放+`TestWeComCSFullChain`+`TestWeComCSAcceptanceSim` 出站三断言）；真机增强=真号+客户侧收件确认（T707 执行包）。

### 2.5 企业微信智能机器人 wecombot（`wecombot_accounts`，specs/025）

- **注册**：企微管理后台 → 协作 → **智能机器人**（或群内添加机器人）→ 拿 **回调 Token + EncodingAESKey**（接收事件）+ **机器人 webhook 推送地址**（`https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=...`）。
- **拿到的凭证**：Token、AESKey、webhook_url。
- **HiveMTK 配置**：`wecombot_accounts` 三字段；入站=企微推送加密事件（AES 解密），出站=模板卡片/文本推送到 webhook_url。
- **验收**：模拟信号已过（自造 43 位 AESKey 往返+官方密文向量解密+AcceptanceSim 全链）；真机增强=T904 工作区。

### 2.6 钉钉 dingtalk（`dingtalk_app_accounts`）

- **注册**：open-dev.dingtalk.com → 应用开发 → 企业内部应用 → **AppKey + AppSecret + AgentId**；「消息推送」拿 **Token + AESKey**（加密回调）。免费，个人可建组织。
- **HiveMTK 配置**：四件套 `app_key`/`app_secret`/`agent_id`/`token`+`aes_key`；机器人回调 URL 指向钉钉路由（服务端验签解密）。
- **验收**：模拟信号已过（024 金样含 dingtalk×3）；真机=钉钉群/单聊@机器人。

### 2.7 飞书 feishu（feishu 模型族）

- **注册**：open.feishu.cn → 开发者后台 → 创建企业自建应用 → **App ID + App Secret**；事件订阅拿 **Verify Token + Encrypt Key**；机器人启用后订阅 im.message.receive_v1；Webhook 验签用 **BotSecret（Ed25519 派生）**。
- **HiveMTK 配置**：`app_id`/`app_secret`/`verify_token`/`webhook_secret`；回调 URL 指向飞书路由。
- **验收**：模拟信号已过（024 金样含 feishu×3 含 interactive 卡片）；真机=飞书会话机器人。

### 2.8 Slack（`slack_accounts`，specs/025）

- **注册**：api.slack.com → Your Apps → From scratch → **App-Level/Bot Token（xoxb-…）+ Signing Secret**；OAuth & Permissions 装 bot 进 workspace；Event Subscriptions 订阅 message events + 回调 URL。
- **拿到的凭证**：Bot Token、Signing Secret、team_id、app_id。
- **HiveMTK 配置**：四字段；`thread_ts` 线程回复自动走（能力位已声明）；签名校验=5 分钟时间窗（过期拒绝）。
- **验收**：模拟信号已过（固定 `v0:ts:body` HMAC 向量含过期负例+AcceptanceSim 经 `SetSlackAPIBaseForTest` 断言 channel/thread_ts/text/Bearer）；真机增强=T904 真实工作区 @bot 回帖。

### 2.9 QQ 官方机器人（`qq_accounts`）

- **注册**：q.qq.com（QQ 开放平台）→ 创建机器人 → **AppID + AppSecret**；沙箱环境可即时测试，正式发布需审核。个人可注册（个人主体机器人能力受限，2026 口径）。
- **HiveMTK 配置**：`app_id`/`app_secret`/`webhook_secret`/`webhook_url`；出站走被动回复 msg_seq（024 金样 qq×3）。
- **验收**：模拟信号已过；真机=QQ 沙箱群机器人收发。

### 2.10 微信公众号 wechat（`wechat_accounts`）

- **注册**：mp.weixin.qq.com → 订阅号/服务号（**客服消息接口需认证服务号**；认证=企业资质 300 元/年审核）→ **AppID + AppSecret**；服务器配置拿 **Token + EncodingAESKey**。
- **HiveMTK 配置**：`app_id`/`app_secret`/`token`/`encoding_aes_key`；回调 `POST /api/webhook/wechat/{account_id}`（自行验签，通用路由不处理）。
- **验收**：模拟信号已过（024 金样 wechat×3 含卡片折叠正文）；真机=公众号粉丝客服消息 48h 窗口。

### 2.11 OneBot v11（`onebot_accounts`，specs/025）——QQ 个人号，无需平台注册

- **注册**：**无平台审批**——准备一个 QQ 号 + 部署 NapCat（github.com/NapNeko/NapCatQQ，QQ 协议端实现）→ NapCat 配置 **HTTP 上报**（event POST 到服务端）或**反向 WS**（NapCat 主动连服务端）+ **access_token**。
- **拿到的凭证**：QQ 号（self_id）、NapCat HTTP base URL、access_token。
- **HiveMTK 配置**：`self_id`/`http_base_url`/`access_token`；`group_recall` 自动映射 `message.deleted`；断线重连顶替旧连接（不泄漏）。
- **验收**：模拟信号已过（四种 post_type 回放+重连顶替+回环发送四腿 AcceptanceSim）；真机增强=T806 NapCat 实机（QQ 风控自担——个人协议端有封号风险，建议小号）。

### 2.12 custom 通用渠道（`custom_accounts`，specs/025）——自有系统接入

- **注册**：**无外部平台**——你自己的系统（CRM/小程序/ERP）作为对端。
- **HiveMTK 配置**：新建 custom 账号生成 `secret` → 对端 `POST /api/webhook/custom/{account_id}`，固定 JSON 契约 + HMAC 头 `X-Signature: t=<unix>,v1=<HMAC-SHA256(secret, t + '.' + body)>`（错签/过期 401 不落库；同 msg_id 双投不重复）；出站回调 `callback_url` 带签名。
- **验收**：模拟信号已过（自签向量+错签/过期/篡改三负例+AcceptanceSim 全链）。

---

## 三、蜂桥网页桥渠道（B 包）——浏览器已登录即可接入

### 3.1 通用接入流（所有 B/C 渠道一致，5 分钟）

1. **装扩展**：Chrome 加载蜂桥（保留加载目录路径不变=扩展 ID 不变=配置不丢）。
2. **填三件套**：popup → serverUrl（线上=`https://workapi.xapptool.cn`，本机 dev=`http://127.0.0.1:8204`）+ tenant_code（租户码）+ **bridge token**（登录用户端 → `/api/bridge/token` 生成/重置；生产重置会挤 `_prev` 槽，扩展心跳无缝）→「保存并连接」。
3. **登录渠道页面**：浏览器登录对应平台网页版私信/客服页。
4. **自动注册**：扩展首报 ingest 自动在 `bridge_accounts` 建行（tenant/channel/account_id/online 心跳）；capabilities 校验凭证（无 token=401 fail-closed）。
5. **验收**：上行=message_hub inbound 行；下行=AI 回复落 message_hub pending → SSE 下发 → 扩展认领发送 → ack delivered；四证据回读（§六）。

### 3.2 B 包渠道接入表（11 个，选择器已校准/部分校准）

| 渠道 | 网页入口 | 选择器状态 | 接入注记 |
|---|---|---|---|
| douyin 抖音 | `https://www.douyin.com/chat`（/chat 才是网页 IM） | 生产实证（会话 id 选择器待精校，conv: 昵称回退） | 登录即通；`douyin-identity` 双 content script |
| xiaohongshu 小红书 | www.xiaohongshu.com/chat | 生产实证 | 登录即通 |
| tiktok TikTok | www.tiktok.com/messages | 生产在用 | 登录即通 |
| kuaishou 快手 | www.kuaishou.com/new-reco | 实验标注（模式猜测） | 登录后观察 |
| xianyu 闲鱼 | www.goofish.com/im + xianyu.com 双域 | 生产实证（消息级 peer id 设计） | account_id 逐消息解析 |
| jd 京东 | jdm.jd.com / dongdong.jd.com / chat.jd.com / im.jd.com | 实验（咚咚网页待 17a 校准） | 与 CH-17 咚咚校准联动 |
| bilibili B站 | message.bilibili.com + www.bilibili.com/message/ | 生产实证（.brt-editor 等） | conv id 口径待精校 |
| weibo 微博 | weibo.com/chat | 已校准（SEL/isChatUrl 2026-11-12 批） | $CONFIG.uid 空时 self id 占位 |
| pdd 拼多多 | mms.pinduoduo.com/chat-merchant/index.html | 实验标注（A 源候选+fail-closed 多候选） | 16a 真机校准后移出实验 |
| personal_wx 个人微信 | —— 桌面伴生 | 外部伴生工程 | CH-11①：需负责人书面授权+Windows，与扩展分格 |

### 3.3 C 包店铺桥骨架（5 个，2026-11-18 交付，fail-closed 等校准）

| 渠道 | 入口域 | 注册→拿到什么 | 状态与开闸条件 |
|---|---|---|---|
| taobao 千牛(淘宝/天猫) | work.taobao.com（接待中心） | 淘宝/天猫店铺（C 店身份证/天猫企业+商标）→ 主账号+子账号客服权限 | 服务端全链已注册；适配器 SEL 零证据拒启；15a 登录接待中心取 DOM 后补选择器开闸 |
| douyin_shop 抖店飞鸽 | im.jinritemai.com/pc_seller_v2 | 抖店入驻（抖音号+执照）→ 客服子账号权限 | 同上；18a 登录直核 |
| kuaishou_shop 快语 | im.kwaixiaodian.com | 快手小店入驻 → 快语客服权限+地域确认 | 同上；19a 登录直核 |
| shopee Shopee | seller.shopee.{my,co.id,ph,sg,co.th,com.br,vn}.webchat | 跨境店入驻（企业）→ Seller Centre + Chat API 工单 | 同上；28a 登录直核（Chat API 批准可转 A 包） |
| walmart_us Walmart | seller.walmart.com（Inbox→Customer） | 美国公司主体申请 → Seller Center | 同上；35a 登录直核 Inbox |

### 3.4 店铺类渠道注册资料与门槛速查（金额=知识库口径，注册时以官方现行页复核）

| 渠道 | 入驻主体 | 自助注册 | 客服/IM 权限 | 门槛·费用 | 接通额外依赖 |
|---|---|---|---|---|---|
| 千牛 淘宝 | C 店身份证/天猫企业+商标 | ✅C 店自助；天猫招商审核 | 千牛网页「接待中心」+子账号客服 | C 店免费+消保保证金 ~1000 起（30 元/年保险可替代）；天猫保证金 5 万起+年费 | 主账号+子客服+授权时段 |
| 拼多多 | 个人/企业均可 | ✅自助（身份证+银行卡/对公） | mms 网页消息管理 | 保证金类目 1000 起+0.6% 支付通道费 | 商家号+第二买家号 |
| 抖店飞鸽 | 个体/企业执照 | ✅自助（抖音号+执照） | 飞鸽网页+客服子账号 | 保证金 2000-20000 按类目+佣金 0.6-5% | 抖店授权+客服权限 |
| 快手快语 | 企业/个体 | ✅自助 | 快语网页 | 保证金类目 1000-20000+佣金 1-5% | 小店+快语权限+地域 |
| Shopee | 企业（跨境） | ✅招商官网申请 | Seller Centre Webchat/Chat API 工单 | 无月租+佣金 ~5-6%+交易费 2% | 区域账号+Chat API 工单 |
| Walmart | 美国公司主体（需核实） | ✅申请表单（审批较严） | Seller Center Inbox | 无月租+referral fee ~6-20% | Seller Centre 账号 |

> 五行共同免费测试路径=A 档零硬门槛（零成交年成本≈0，保证金可退类目）；自测六步与证据口径见外层清单 §3.9「免费客服-only 逐项验证」块。

---

## 四、E 形态：Web Widget（自有访客端）

- **配置**：用户端「渠道管理」新建 Web 渠道 → `chat_channels` 生成 `app_key`（公开）+`app_secret`（哈希存）→ embed-sdk 引 IIFE + `window.MarketingChatWidgetConfig = { appKey, ... }`（**config 必须先于 SDK IIFE 设置**，迟到配置静默失效——README §4）。
- **隐私通知**（specs/023）：`privacyNotice` 配置开启同意闸（同意前不建 iframe；version 变更重问）。
- **验收**：unit 116/116+boundary 62/62+真浏览器 S1-S5 回归（2026-11-17）。

---

## 五、Parking 渠道注册入口一句话索引（16 个，详见外层清单 §七）

CH-17 京东（im.jd.com/seller/download→咚咚网页）｜CH-20 小红书千帆（ironman.xiaohongshu.com/pc/app/eva 网页工作台）｜CH-21 微信小店（developers.weixin.qq.com/doc/store/shop/guide/connect/kf.html，commkf，测试店+超管）｜CH-22 得物（seller.poizon.com）｜CH-23 有赞（后台「全渠道消息」+七鱼市场订购）｜CH-24 美团外卖（developer.waimai.meituan.com openim 官方 API）｜CH-25 饿了么/淘宝闪购（open.shop.ele.me，企业应用）｜CH-26 1688（work.1688.com「客户&询盘」）｜CH-27 TikTok Shop（partner.tiktokshop.com Customer Service API，开发者审批）｜CH-29 Lazada（open.lazada.com 应用）｜CH-30 Amazon（SP-API OAuth+Messaging Permissions+专属邮箱 relay）｜CH-31 eBay（developer.ebay.com Message API）｜CH-32 Shopify（自有 widget 主路线：店铺主题扩展部署权即可）｜CH-33 AliExpress（seller.aliexpress.com 消息中心）｜CH-34 Etsy（Shop Manager→Messages）｜CH-36 唯品会/苏宁（云台 sop.suning.com 客服管理）。
**重开动作**：迁回外层清单 §3.9 按 §3.10 二审规格开工；申请类前置「现在递申请、将来实现」不等重开。

---

## 六、验收口径与红线（所有渠道统一）

1. **模拟信号验收**（无账号阶段的功能链路完整性判据）：httptest 平台回放 / fake 契约集 / AcceptanceSim 全链（真实测试库+生产路由+`sendOutbound` 真实入口，非纯函数打桩）——025 delivery §四 与 `channel-shop-channels-failclosed.test.js` 为范本。
2. **真机四证据**（有账号后）：DOM 发送动作 / 桥端 ack=delivered / 平台受理回执 / 授权接收端可见——缺一不得删行；`delivered` 字段语义不擅自改；结果不明先对账不盲重发。
3. **红线**：只测授权会话；不给无关真实客户发信；不绕风控（阿里系只走官方可授客服身份）；平台密钥/用户消息不进 fixture/log；不宣称 exactly-once；跨租户 fail-closed。
4. **实验渠道如实降级**：EXPERIMENTAL 渠道（当前 9 个）只影响日志/诊断展示与文档声明，不拦截功能；选择器真机校准后按 pdd 模式补 fixture 再移出实验集。
