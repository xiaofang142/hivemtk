
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
export const DEFAULT_USER_SERVER = {
  host: 'localhost',
  port: 8204,
  baseUrl: 'http://localhost:8204',
  healthPaths: ['/health', '/healthz', '/readyz', '/api/health'],
  wsPath: '/api/ws/bridge',
  profile: 'dev',
};

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
// 4) WS 客户端默认值（bridge-client.js）
// =============================================================
// 心跳超时 25s（与 server handler.go pongWait=60s 错开 35s 以上）
// 文档源：user-server/internal/bridge/handler.go
//   const pongWait   = 60 * time.Second
//   const pingPeriod = 50 * time.Second
// 客户端 25s 超时 < 服务端 60s，避免出现客户端先断导致连接泄漏
export const WS_CLIENT_DEFAULTS = Object.freeze({
  serverIdleTimeoutMs: 25 * 1000,
  reconnectBaseMs: 1000,
  reconnectMaxMs: 30 * 1000,
  reconnectJitterMs: 500,
});

// =============================================================
// 5) 巡检制度（patrol）默认值（详见 bridge.md 上行巡检）
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
});

// =============================================================
// 5b) 历史回填宽限期（per-channel，2026-08-05 审计 P0/A6）
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
});

// B8（2026-09-19）实验渠道清单——与 user-server internal/channelgw/registry.go 的
// ChannelSpec.Experimental 同步维护。快手网页 IM 选择器是模式猜测、零真机校准：
// 链路可跑但不保证生产级正确率。语义是「如实降级声明」：只影响日志/诊断展示，
// 不拦截任何功能（未证伪不删除）。
export const EXPERIMENTAL_CHANNELS = Object.freeze({ kuaishou: true });

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
  CHANNELS: { DOUYIN: 'douyin', XHS: 'xiaohongshu', TIKTOK: 'tiktok', XIANYU: 'xianyu', KUAISHOU: 'kuaishou' },
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
  sendOutboundTimeoutMs: 20000,
});

// B4（批3）：发送全链统一超时。B7 拟人键入后单次 sendText 时长随文本长度线性增长
// （逐段停顿均值≈90ms/码点，标点/思考停顿上探 250ms/码点）——固定 20s 会对长文案
// 产生"假超时→pending 重发→重复消息"。超时预算 = 基础 + 250ms×码点数，封顶 120s。
export function humanSendTimeoutMs(text, baseMs = BRIDGE_THREE_CHANNEL.sendOutboundTimeoutMs) {
  const chars = Array.from(String(text || '')).length;
  return Math.min(baseMs + chars * 250, 120_000);
}


