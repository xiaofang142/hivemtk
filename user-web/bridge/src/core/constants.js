
// =============================================================
// 1) 服务端默认地址（user-server 连接入口）
// =============================================================
// 文档源：DEVELOPMENT.md §端口对照表
//   | 8204 | user-server (Gin HTTP)            |
//   | 8202 | PostgreSQL（docker 宿主机映射）   |
//   | 8207 | llama.cpp (LLM)                   |
//   | 8208 | embedding (bge-m3)                |
//   | 8209 | rerank (bge-reranker-v2-m3)       |
//   | 8232 | PostgreSQL（dev 本地直连）        |
// 交叉验证：user-server/Dockerfile:57  ENV SERVER_PORT=8204
// 交叉验证：user-server/cmd/api/main.go listenAddr 默认 :8204
//
// 这里刻意没有 wsPath：扩展与 user-server 之间只有 HTTP 上行 /api/bridge/ingest、
// 下行轮询 /api/bridge/outbox、回执 /api/bridge/outbox/ack，加一条下行推送 SSE
// /api/bridge/outbox/sse（capabilities 报 sse_enabled=false 时自动退回轮询）。
// 那个常量的值 /api/ws/bridge 在服务端从未注册过（router 层全量 grep 零命中；同前缀下
// 真实存在的是 /api/ws/channel，那是 channelgw 的传输口，扩展不走它），而扩展 src/ 里
// 也从未出现 new WebSocket。
export const DEFAULT_USER_SERVER = {
  host: 'localhost',
  port: 8204,
  baseUrl: 'http://localhost:8204',
  healthPaths: ['/health', '/healthz', '/readyz', '/api/health'],
  profile: 'dev',
};

// 线上（SaaS 云端）user-server 地址 —— 扩展分发渠道是平台（平台运营上传安装包、
// 租户从用户端网页下载），对这份渠道装出来的扩展，「默认能连上」的地址是云端 API 口。
//
// 文档源：deploy/deploy.conf USER_API_URL（唯一配置源，随 DOMAIN 派生）；
// 交叉验证：deploy/README.md §5 端口与域名对照 —— workapi.xapptool.cn → 宝塔反代 127.0.0.1:8204，
// 且 README §8.7 PUBLIC_BASE_URL=https://workapi.xapptool.cn 是服务端自认的 API 正域名。
// 注意别用 work.xapptool.cn 当扩展默认：它虽也反代 /api/*（2026-11-04 实测），
// 但 /health 落 SPA 兜底回 HTML（假 200），扩展「测试连接」的健康口在 API 域名上才是真的。
// SaaS 云端 user-server 地址：**构建期注入**，不是源码常量。
//
// 隐私红线（2026-11-04 二次审核）：本项目卖点是「100% 私域零出域」，把
// workapi.xapptool.cn 写死成源码默认值，等于让任何自部署用户第一次打开
// 弹窗就预填厂商云端地址——点保存就把客户私信传到厂商域。故：
//   · 开源 / 自部署构建（不设 BRIDGE_ONLINE_SERVER）→ baseUrl 为空串，
//     弹窗默认仍是 localhost:8204（DEFAULT_USER_SERVER），零行为变化；
//   · 官方 SaaS 发行包构建时设 BRIDGE_ONLINE_SERVER=https://<我方 API>，
//     由 scripts/build.mjs 的 esbuild define 注入，源码里不出现域名。
//
// 注入必须走「专用标识符」，不能走 process.env（2026-11-04 二次审核实测）：
//   MV3 扩展里没有 process 全局。原先写成
//     (typeof process !== 'undefined' && process.env && process.env.BRIDGE_ONLINE_SERVER)
//   时，esbuild 只能替换掉 `process.env.BRIDGE_ONLINE_SERVER` 这一小段，产物是
//     typeof process<"u" && process.env && "https://<我方 API>" || ""
//   在扩展运行时 `typeof process` 恒为 "undefined" 而短路 ⇒ 注入被静默丢弃，
//   带 BRIDGE_ONLINE_SERVER 构建出来的包依然不预填云端地址（线上按钮直接隐藏）。
//   改成让构建工具替换整个标识符：注入时表达式折成 `typeof "https://…"`（string），
//   未注入（测试 / 开源构建）该标识符不存在，typeof 得 "undefined" ⇒ 空串。
//   回归用例：test/popup-online-injection.test.js（带 define 打包，断言预填值）。
/* global __BRIDGE_ONLINE_SERVER__ */
const INJECTED_ONLINE_SERVER =
  typeof __BRIDGE_ONLINE_SERVER__ !== 'undefined' ? __BRIDGE_ONLINE_SERVER__ : ''

export const ONLINE_USER_SERVER = {
  baseUrl: INJECTED_ONLINE_SERVER,
  profile: 'saas-cloud',
};

// 租户短码（tenant_code）——SaaS 多租户模式下桥接入口的归属自证凭据。
//
// 服务端判据（user-server/internal/bridge/tenant_binding.go:63-92）：
//   ① bridge_accounts 里 (channel, account_id) 行只属于一个租户 ⇒ 绑它；
//   ② 账号尚未注册过，或同一 (channel, account_id) 在两个租户各有一行
//      ⇒ 只认显式 tenant_code（query 或 X-Tenant-Code 头），由短码解析；
//   ③ 两条线索都没有 ⇒ 拒绝（404，提示补 tenant_code）。
//
// 首次上报必须带短码：账号行只能由 ingest 内部的异步 Upsert 创建，而那段 Upsert
// 排在租户判定之后 ⇒ 不带短码的首次上报必然 404（先有鸡还是先有蛋的死锁）。
//
// 短码来源：用户端 GET /api/tenant/info 的 tenant_code 字段（登录后可见）。
// 离线单机模式（TENANT_MODE=off）服务端整段跳过租户判定 ⇒ 留空即可。
export const TENANT_CODE = Object.freeze({
  QUERY_PARAM: 'tenant_code',
  HEADER: 'X-Tenant-Code',
});

// =============================================================
// 2) 平台入口（仅用于 popup 一键打开的快捷入口）
// =============================================================
// 文档源：用户指定的真实私信/聊天页入口（需求①）
//   - 抖音聊天页：https://www.douyin.com/chat
//   - 小红书聊天页：https://www.xiaohongshu.com/chat
//   - TikTok 私信：https://www.tiktok.com/messages（官网消息入口）
//   - 闲鱼聊天页：https://www.goofish.com/im（闲鱼 IM 入口）
// 用途：popup 「打开抖音/小红书/TikTok/闲鱼 私信」按钮的跳转目标
// 2026-08-05 渠道编码统一：key 改为平台全名（与后端 model.Channel* 对齐）。
export const PLATFORM_ENTRY_URLS = {
  douyin: 'https://www.douyin.com/chat',
  xiaohongshu: 'https://www.xiaohongshu.com/chat',
  tiktok: 'https://www.tiktok.com/messages',
  xianyu: 'https://www.goofish.com/im',
  kuaishou: 'https://www.kuaishou.com/new-reco',
  jd: 'https://jdm.jd.com/',
  bilibili: 'https://message.bilibili.com/',
  weibo: 'https://weibo.com/chat',
};

// =============================================================
// 3) 限速/风控默认值（详见 bridge.md §17.3 三层风控）
// =============================================================
// 约束：所有数字都基于业务经验值，禁止"软启动"空值
// 调整建议：见 docs/bridge/DEFAULTS.md
export const RATE_LIMIT_DEFAULTS = Object.freeze({
  accountCapacity: 12,
  accountRefillPerMin: 12,
  minIntervalMs: 1500,
  jitterMinMs: 800,
  jitterMaxMs: 2600,
  conversationCooldownMs: 3000,
  conversationPerHour: 40,
  dedupWindowMs: 60000,
});

// =============================================================
// 4) 巡检制度（patrol）默认值（详见 bridge.md 上行巡检）
// =============================================================
// 巡检语义：一轮巡检完成 → 自动进入下一轮。遍历左侧聊天列表，对有新消息
// （未读红点）的会话点击进入右侧聊天页，捕获新消息上行（触发 AI 自动对话）。
// 已 seen 的消息靠去重跳过，故只有真正新增的消息会上行。
//
// 2026-08-14 风控重设：原 intervalMs=3s/switch 1-2s/maxPerRound=0 节奏过快，
//   在抖音/小红书 IM 网页端实测会被风控识别为脚本（连续点击触发滑块/封号预警）。
//   新节奏模拟真人：
//     - 轮间隔 30-60s（带 30s 抖动窗口）
//     - 单轮最多访问 6 个会话（避免一秒钟连续点 10+ 个会话）
//     - 会话间切换 3-5s（每次都看起来像"看了看又点开另一个"）
//     - 同一会话 120s 冷却期内不重复点开（避免来回点同一会话触发风控）
//   上述参数全部为单一源，禁止在 channel-adapter.js 硬编码同样数字。
export const PATROL_DEFAULTS = Object.freeze({
  intervalMs: 30000,
  jitterMs: 30000,
  waitActiveMs: 5000,
  throttleMs: 1500,
  switchMinMs: 3000,
  switchMaxMs: 5000,
  maxPerRound: 6,
  maxConversationsPerRound: 6,
  conversationCooldownMs: 120000,
  scrollLoadMs: 1500,
  maxPasses: 8,
  maxBatchPerPatrol: 80,
  firstRunMaxBatch: 20,
  firstRunWindowMs: 60000,
  // ── 批25：换会话互斥（巡检让路）─────────────────────────────────────────
  // 巡检与下发都会 openConversation 换右侧会话。下发的键入是逐段拟人 insertText
  // （30 字≈2.7s、100 字≈9s），巡检切换节拍 3–5s ⇒ 键入期被切走是结构性必现，
  // 后果是把 A 的文案点进切换后的 B 会话（发给下一个用户，不可撤回）。
  // 契约：下发持锁期间（含 linger）巡检不换会话（单向优先级，无相互等待 ⇒ 无死锁）。
  switchBlockWaitMs: 20000,   // 等一个可切会话窗口的上限（下发之间有间隙，不必整轮作废）
  switchBlockPollMs: 500,     // 等窗口的轮询节拍
});

// =============================================================
// 5) 历史回填宽限期（per-channel，2026-08-05 审计 P0/A6）
// =============================================================
// 语义：会话初次挂载/切换后的一段时间内，新出现的客户消息仅回填历史（落库），
//   不触发 AI 自动回复。避免打开含存量私信的会话时被当成新消息逐一自动回复。
//
// per-channel 依据（实测不同平台 SPA 渲染延迟差异极大）：
//   - douyin：5s（聊天页结构稳定，DOM 渲染快）
//   - xiaohongshu：2s（XHS IM 是 React 受控组件，渲染最快）
//   - tiktok：6s（海外链路 + 重 SPA，渲染最慢）
//   - xianyu：4s（闲鱼 IM 入口 goofish.com，DOM 中等复杂度）
//   - kuaishou：5s（实验渠道，选择器未经真机校准，取抖音同档保守值）
//
// 默认值（BaseAdapter 未注入 hooks.historyGraceMs 时使用）：8s（保守上限）
export const HISTORY_GRACE_MS = Object.freeze({
  douyin: 5000,
  xiaohongshu: 2000,
  tiktok: 6000,
  xianyu: 4000,
  kuaishou: 5000,
  default: 8000,
});

// getHistoryGraceMs 按 channel 取 per-channel 宽限期；未知 channel 用 default。
export function getHistoryGraceMs(channel) {
  if (!channel) return HISTORY_GRACE_MS.default;
  return HISTORY_GRACE_MS[channel] || HISTORY_GRACE_MS.default;
}

// =============================================================
// 6) UI / 测试用超时
// =============================================================
export const UI_DEFAULTS = Object.freeze({
  healthCheckTimeoutMs: 3500,
  metaReportIntervalMs: 5000,
  popupHealthPanelPollMs: 5000,
  popupAlertPollMs: 10000,
});

// 渠道展示名 → 统一只显示「抖音 / 小红书 / TikTok / 闲鱼」，不出现「抖音私信(网页)」这类冗长写法
// （需求④：只有一个渠道名称、来源平台编码只有 抖音/小红书/闲鱼/TikTok，列表渲染/搜索同理）
//
// 2026-08-05 渠道编码统一：key 与 value 解耦——key 是后端约定的渠道编码（与 user-server
// model.ChannelXHS/Douyin/Kuaishou/Xianyu/TikTok 完全一致，全名无 _web 后缀），
// value 是 UI 展示文案（中文/品牌名）。
export const CHANNEL_DISPLAY = Object.freeze({
  douyin: '抖音',
  xiaohongshu: '小红书',
  tiktok: 'TikTok',
  xianyu: '闲鱼',
  kuaishou: '快手',
  jd: '京东',
  bilibili: 'B站',
  weibo: '微博',
  // CH-16 首切片（2026-11-09）：拼多多商家网页聊天，与后端 registry.Label 一致。
  pdd: '拼多多',
});

// B8（2026-09-19）实验渠道清单——与 user-server internal/channelgw/registry.go 的
// ChannelSpec.Experimental 同步维护。快手网页 IM 选择器是模式猜测、零真机校准：
// 链路可跑但不保证生产级正确率。语义是「如实降级声明」：只影响日志/诊断展示，
// 不拦截任何功能（未证伪不删除）。
// 2026-11 新增 jd/bilibili/weibo：选择器有公开页面结构证据但未真机校准，同样标实验。
// 2026-11-09 新增 pdd（CH-16 首切片）：选择器为 X 级历史源码实证（ChatGPT-On-CS
// v1.4.5 静态源，2026-11-06 评估报告 §5.1），本方真机未校准，同样标实验。
// 与 user-server registry_parity_test.go TestExperimentalFlagsParityWithBridgeExtension 双向钉住。
export const EXPERIMENTAL_CHANNELS = Object.freeze({ kuaishou: true, jd: true, bilibili: true, weibo: true, pdd: true });

export function isExperimentalChannel(channel) {
  return !!EXPERIMENTAL_CHANNELS[channel];
}

// =============================================================
// 7) 协议常量（与服务端 user-server/internal/bridge/frames.go 一一对应）
// =============================================================
// 文档源：bridge.md §3 / frames.go Frame* 常量
// 警告：frame 名称是协议契约，禁改字面量
//
// 2026-08-05 渠道编码统一：CHANNELS 值改为平台全名（去掉 _web 后缀），与后端
// model.ChannelXHS/Douyin/Kuaishou/Xianyu/TikTok 一一对应。
export const PROTOCOL = Object.freeze({
  CHANNELS: { DOUYIN: 'douyin', XHS: 'xiaohongshu', TIKTOK: 'tiktok', XIANYU: 'xianyu', KUAISHOU: 'kuaishou', JD: 'jd', BILIBILI: 'bilibili', WEIBO: 'weibo', PDD: 'pdd' },
  FRAME: {
    REGISTER: 'register',
    INBOUND: 'inbound_message',
    HISTORY: 'history',
    OUTBOUND: 'outbound_reply',
    PONG: 'pong',
    PING: 'ping',
    ACK: 'ack',
    ERROR: 'error',
  },
  SENDER: { CUSTOMER: 'customer', AGENT: 'agent', SELF: 'self', SYSTEM: 'system' },
  DIRECTION: { INBOUND: 'inbound', OUTBOUND: 'outbound' },
});

// =============================================================
// 7.1) 桥接 ack v2 协议常量（P0-7 单源化，与服务端 model/bridge_protocol.go 严格对齐）
// =============================================================
// 服务端权威源：user-server/internal/model/bridge_protocol.go 的 BridgeAckStatus* 常量。
// 禁止在业务代码（http-ingest.js / downlink.js）中硬编码下列协议字段字符串。
export const BRIDGE_PROTOCOL_V2 = Object.freeze({
  VERSION: 2,
  // ack 请求字段
  FIELD: Object.freeze({
    V: 'v',
    ITEMS: 'items',
    MSG_IDS: 'msg_ids',
    STATUS: 'status',
    CONVERSATION_ID: 'conversation_id',
    ERROR: 'error',
    MSG_ID: 'msg_id',
  }),
  // ack 请求终态（P0-3）
  TERMINAL: Object.freeze({
    DELIVERED: 'delivered',
    FAILED: 'failed',
  }),
  // ack 响应 items[].status（P3-D）
  RESPONSE_STATUS: Object.freeze({
    ACKED: 'acked',
    FAILED: 'failed',
    DUPLICATE: 'duplicate',
    NOT_FOUND: 'not_found',
    NOT_IN_SCOPE: 'not_in_scope',
  }),
});

// =============================================================
// 8) 安全护栏（防 XSS / 防滥用 / 防误用）
// =============================================================
// 服务端 maxReplyContentBytes=4*1024（handler.go:43）
// 客户端 4KB 与之对齐，避免 AI 生成超大回复被服务端截断后用户看到残缺
export const SECURITY = Object.freeze({
  maxReplyContentBytes: 4 * 1024,
  logMaskMaxChars: 24,
});

// =============================================================
// 9) 桥接下发三通道配置（2026-08-06 架构重构，要求1：参数可配置）
// =============================================================
// 三通道相互独立：
//   通道A·上报:  uplink  → POST /api/bridge/ingest（父层统一上报，消息 hash 前端完成）
//   通道B·状态:  status → POST /api/bridge/outbox/ack（确认 delivered）
//   通道C·下发:  downlink → GET /api/bridge/outbox（独立轮询拉取待发消息）
//
// 全部可在 docs/bridge/DEFAULTS.md 覆盖（运行时读取使用者覆盖值）。
export const BRIDGE_THREE_CHANNEL = Object.freeze({
  uplinkMergeWindowMs: 350,
  uplinkMaxBatch: 20,
  outboxPollIntervalMs: 1500,
  outboxBatchSize: 50,
  ackFlushIntervalMs: 500,
  sentCacheMax: 2000,
  // 批20d-A4：本地已发缓存的时间界。取值必须 **≥ 上游仍能重投的时长**，否则"过期即重发"
  // 反而把重复消息放出去（不可撤回）。同行常用 5min（SQS 去重窗），此处不可照抄：
  // 服务端的重推窗取决于浏览器离线多久，24h 是对"人睡一觉/周末没开浏览器"的上界估计。
  // 有界是为了回收：界之后的记录既不防任何在途重推，还占着 sentCacheMax 的名额。
  sentCacheTtlMs: 24 * 3600 * 1000,
  sendOutboundTimeoutMs: 20000,
  // R22 §6-4：SSE 形态下 _pendingAck 的排水节拍。轮询每 outboxPollIntervalMs(1.5s) 顺带排一次，
  // SSE 没有那个循环，只能自己定。取 5s：退避首档 1s、服务端可见性超时 30s，再密只是多打 ack 接口。
  pendingAckDrainIntervalMs: 5000,
  // 批24 §8.3-8：出站发送后回 DOM 复核自己气泡的预算与节拍。取 2.5s 的理由：各渠道
  // historyGraceMs 是 2–6s（SPA 渲染滞后），而这条回查**只产结论位、不改 ack 与重发**，
  // 所以宁可取一个"大概率够、不够就报未见"的小值，也不要为了好看把下行队列按会话堵住 10s。
  sendVerifyMs: 2500,
  sendVerifyPollMs: 250,
  // 批25：出站临界区的 linger。downlink 是**逐条** sendOutbound 的，两条消息之间有
  //   `await cache.flush()` 的 await 空档；不留 linger，巡检就会在这个空档插进来切一次会话，
  //   下一条再把会话切回去（正确性有 assertConv 兜底，但白跑两次切换）。
  //   取 1.5s：盖住"存储读+写+下一条进锁"的常态空档；不盖限速重试那种秒级等待
  //   ——那一段本来就该让巡检有机会干活（否则下行密集时巡检会被无限期饿死）。
  outboundLingerMs: 1500,
});

// B4（批3）：发送全链统一超时。B7 拟人键入后单次 sendText 时长随文本长度线性增长
// （逐段停顿均值≈90ms/码点，标点/思考停顿上探 250ms/码点）——固定 20s 会对长文案
// 产生"假超时→pending 重发→重复消息"。超时预算 = 基础 + 250ms×码点数，封顶 120s。
export function humanSendTimeoutMs(text, baseMs = BRIDGE_THREE_CHANNEL.sendOutboundTimeoutMs) {
  const chars = Array.from(String(text || '')).length;
  return Math.min(baseMs + chars * 250, 120_000);
}

// 出站一次下发的**外层**预算 = 上面这条 sendText 预算 + 批24 的回查切片。
// 外层调用方（downlink 的三处 withTimeout）必须用这条而不是上一条：外层超时判定的是
// 「这条没发出去」⇒ 不 ack ⇒ 服务端重推，而回查是在**点击之后**才跑的，
// 只给 sendText 的预算会把"发得慢、已经点出去了"的文案判成没发出去 —— 重推即双发，
// 这是本链路唯一不可撤销的那一侧。回查自身有界（sendVerifyMs），所以这里的加法不会无界放大。
export function outboundStepTimeoutMs(text, baseMs = BRIDGE_THREE_CHANNEL.sendOutboundTimeoutMs) {
  return humanSendTimeoutMs(text, baseMs) + BRIDGE_THREE_CHANNEL.sendVerifyMs;
}


