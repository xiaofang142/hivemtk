// follow-signal.js —— 新用户（非关注好友）识别词表与打标工具（M-6，2026-11-11）
//
// 背景（2026-11-10 审计 M-6）：各渠道系统消息文案（「互相关注」「你还不是对方好友」
// 「followed you」等）此前只用于「归类 system 不触发 AI」的规避，关注关系信号本身
// 从不上报。本模块把这些既有识别素材收敛成单一词表：parseMessageItem 命中时给
// parsed.extra 打上 `new_user_unfollowed` 标记（另附 `new_user_signal` 来源），
// 随既有消息 payload 上行（只加字段，不改 system 归类与 AI 触发现状）。
//
// 误伤面控制（验收硬性要求）：
//   1. 文案词表是各渠道既有 SYSTEM_TEXT_PATTERNS 的**子集口径**——调用方只在
//      parseMessageItem 已判 system 的分支里打标，绝不会把客户消息错标；
//   2. 每条 pattern 是逐字明确的关注关系文案（不含 typing/recalled 等无关项）；
//   3. tab 归属（bilibili 未关注人消息）只在「当前激活 tab 可判定为未关注人消息」
//      时打标，选择器/激活态判不出 → 不打标（fail-safe：宁缺勿错）。
//
// 上行键名与服务端约定：`new_user_unfollowed` / `new_user_signal` 已登记在
// user-server internal/channelgw/protocol.go 的 IngestExtraAllowKeys 显式白名单
// （ctx. 前缀之外的放行接缝），经 /api/bridge/ingest 与 WS 双传输透传进
// message_hub.extra。键名改动必须两端同步。

// extra 键名（与服务端 IngestExtraAllowKeys 逐字一致）
export const FOLLOW_SIGNAL_KEY = 'new_user_unfollowed';
export const FOLLOW_SIGNAL_SOURCE_KEY = 'new_user_signal';

// 信号来源词表（与服务端 service.FollowSignalActionable 同一口径，改动两端同步）
export const FOLLOW_SIGNAL_SOURCES = Object.freeze({
  // 互相关注已建立（douyin「互相关注」/ tiktok follow each other / 好友申请已通过）
  MUTUAL: 'mutual_follow',
  // 对方主动关注了我们（回关场景的典型素材）
  PEER_FOLLOW: 'peer_followed_you',
  // 明确提示当前非好友/非互关（闲鱼陌生人提示、tiktok you are not friends 等）
  NOT_MUTUAL: 'not_mutual',
  // 消息位于「未关注人消息」tab（bilibili tab 归属）
  TAB: 'unfollowed_tab',
  // 对方发来好友申请/关注请求类
  FRIEND_REQUEST: 'friend_request',
});

// 各渠道「关注关系」系统文案词表。
// 约束：每条 pattern 命中的文本，该渠道自己的 isSystemMessage/SYSTEM_TEXT_PATTERNS
// 必然也会归类为 system（douyin 走 互相关注/已添加好友 显式项与「关注/添加」前缀
// 泛化分支；tiktok/xianyu 走各自 SYSTEM_TEXT_PATTERNS）——保证打标只落在 system
// 消息上，system 归类现状不变。
export const FOLLOW_SIGNAL_SYSTEM_PATTERNS = Object.freeze({
  douyin: Object.freeze([
    // 「互相关注」但排除闲鱼式「互相关注后」（后者=还没有互关）
    { re: /互相关注(?!后)/, source: FOLLOW_SIGNAL_SOURCES.MUTUAL },
    { re: /关注了你|已关注你|关注了我/, source: FOLLOW_SIGNAL_SOURCES.PEER_FOLLOW },
    { re: /已添加.*为好友/, source: FOLLOW_SIGNAL_SOURCES.FRIEND_REQUEST },
  ]),
  tiktok: Object.freeze([
    { re: /followed you/i, source: FOLLOW_SIGNAL_SOURCES.PEER_FOLLOW },
    { re: /sent you a (?:message|friend request|follow)/i, source: FOLLOW_SIGNAL_SOURCES.FRIEND_REQUEST },
    { re: /follow each other to start chatting/i, source: FOLLOW_SIGNAL_SOURCES.NOT_MUTUAL },
    { re: /you are (?:no longer|not) (?:friends|following)/i, source: FOLLOW_SIGNAL_SOURCES.NOT_MUTUAL },
    { re: /accepted your follow request/i, source: FOLLOW_SIGNAL_SOURCES.MUTUAL },
  ]),
  xianyu: Object.freeze([
    { re: /你还不是对方好友/, source: FOLLOW_SIGNAL_SOURCES.NOT_MUTUAL },
    { re: /互相关注后/, source: FOLLOW_SIGNAL_SOURCES.NOT_MUTUAL },
    { re: /请勿向陌生人/, source: FOLLOW_SIGNAL_SOURCES.NOT_MUTUAL },
  ]),
  // bilibili 无关注类系统文案词表（tab 归属在渠道内单独判定）
  bilibili: Object.freeze([]),
});

// followSignalFromSystemText 返回该渠道系统文案命中的关注信号来源（未命中 null）。
// text 空 / 渠道无词表 / 全部不命中 → null（不打标）。
export function followSignalFromSystemText(channel, text) {
  if (!text) return null;
  const patterns = FOLLOW_SIGNAL_SYSTEM_PATTERNS[channel];
  if (!patterns || !patterns.length) return null;
  const t = String(text).trim();
  if (!t) return null;
  for (const { re, source } of patterns) {
    if (re.test(t)) return source;
  }
  return null;
}

// applyFollowSignal 把关注信号写进 parsed.extra（只加字段；parsed.extra 已有键
// 一律不覆盖——打标是旁路信息，不得改动渠道原有 extra 语义）。
// source 为空（未命中）时是 no-op。返回 parsed 便于链式书写。
export function applyFollowSignal(parsed, source) {
  if (!parsed || !source) return parsed;
  const extra = (parsed.extra && typeof parsed.extra === 'object') ? parsed.extra : {};
  if (!(FOLLOW_SIGNAL_KEY in extra)) extra[FOLLOW_SIGNAL_KEY] = true;
  if (!(FOLLOW_SIGNAL_SOURCE_KEY in extra)) extra[FOLLOW_SIGNAL_SOURCE_KEY] = source;
  parsed.extra = extra;
  return parsed;
}

// followSignalOf 读取消息对象上的关注信号（extra 键位），供服务端/测试对账。
export function followSignalOf(parsed) {
  const extra = parsed && parsed.extra;
  if (!extra || typeof extra !== 'object') return null;
  return extra[FOLLOW_SIGNAL_KEY] ? (extra[FOLLOW_SIGNAL_SOURCE_KEY] || FOLLOW_SIGNAL_SOURCES.TAB) : null;
}
