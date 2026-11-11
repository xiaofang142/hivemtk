import { createLogger } from './logger.js';
import { RateLimiter, globalSendWaitMs, stampGlobalSendAt } from './rate-limiter.js';
import { makeUnifiedMessage, SENDER, DIRECTION, HISTORY_CONTEXT_WINDOW, PATROL_DEFAULTS, contentHash, typedContentHash, mediaPlaceholder, isMediaMsgType } from './types.js';
import { mergeSelectors } from './selector-ai.js';
import { createSelectorHealthTracker } from './selector-health.js';
import { simulateRealClick } from './dom.js';
import { humanSendTimeoutMs, BRIDGE_THREE_CHANNEL, RESCAN_DEFAULTS, FOLLOW_ACTION_DEFAULTS } from './constants.js';
import { withTimeout } from './downlink.js';

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

// 批25：出站临界区释放后仍算"忙"的尾巴时长（口径见 constants.js:outboundLingerMs）。
const OUTBOUND_LINGER_MS = BRIDGE_THREE_CHANNEL.outboundLingerMs;

// M-6：followUser 让路下发的短等上限。关注动作是低优先旁路——主链路（AI 回复下发）
// 在途时不与之抢单页 DOM，短等后仍忙即跳过（动作行收口+留痕），绝不阻塞下行轮询。
const FOLLOW_ACTION_YIELD_WAIT_MS = 3000;


// 下行告警去抖：下行被全局最小间隔 / 会话未找到拦截时，若每轮轮询都打印会刷屏。
// 按 key 限频，默认 15s 内同 key 仅打印一次。
const _warnThrottle = new Map(); 
function throttledWarn(log, key, intervalMs, msg, extra) {
  const now = Date.now();
  const last = _warnThrottle.get(key) || 0;
  if (now - last >= intervalMs) {
    _warnThrottle.set(key, now);
    if (extra !== undefined) log.warn(msg, extra);
    else log.warn(msg);
  }
}
const WARN_THROTTLE_MS = 15000;

// M-6（2026-11-11）：parsed.extra（渠道打标：new_user_unfollowed 等）与调用方显式
// extra（如 send_verified 结论位）的合并。只加字段不改语义：两键冲突时调用方
// （既有调用点）优先；两侧皆空返回 undefined（makeUnifiedMessage 对空 extra 无键输出，
// 与既有帧形状逐字节一致）。
function mergeParsedExtra(parsed, callerExtra) {
  const base = parsed && parsed.extra && typeof parsed.extra === 'object' ? { ...parsed.extra } : null;
  const over = callerExtra && typeof callerExtra === 'object' ? callerExtra : null;
  if (!base && !over) return undefined;
  return { ...(base || {}), ...(over || {}) };
}

// collectBatchExtra 聚合一批 parsed 的 extra（巡检/会话回填批次里任意一条带
// new_user_unfollowed 标记即随批透传——信号不因批次合帧而丢失）。
function collectBatchExtra(items) {
  let out = null;
  for (const it of items || []) {
    const ex = it && it.parsed ? it.parsed.extra : (it ? it.extra : null);
    if (ex && typeof ex === 'object') {
      out = out || {};
      for (const k of Object.keys(ex)) {
        if (!(k in out)) out[k] = ex[k];
      }
    }
  }
  return out;
}
// 批24：出站回查的文本归一化。平台会在气泡里把 NBSP 换成普通空格、把连续空白折叠、
// 在两端留换行；不归一化就会把「发出去了、只是渲染差异」判成未见（假红方向，可接受），
// 但那是纯噪声，所以按空白归一后再比。这里不做包含匹配：长文案被平台分段时空白归一
// 也救不回来，宁可报未见，也不给一次没落地的发送发绿。
function normSendText(s) {
  // \s 在 JS 正则里含 NBSP（U+00A0），平台把不换行空格渲染成普通空白的差异由这一次折叠吃掉。
  return String(s == null ? '' : s).replace(/\s+/g, ' ').trim();
}
// 整页导航护栏：小红书无法用 URL 深链可靠打开屏外会话，若盲目整页导航且打不开会
// 每轮 downlink 重载页面形成抖动。故整页导航只尝试一次——重载后仍打不开即标记
// "navfail"，停止后续破坏性重载，留 pending 由下一轮 downlink 安全重试（用户打开该会话时自然投递）。
const NAV_STATE_KEY = 'hivebridge:navstate';
function _navLoad() {
  try { return JSON.parse(localStorage.getItem(NAV_STATE_KEY)) || {}; } catch (_) { return {}; }
}
function _navSave(s) {
  try { localStorage.setItem(NAV_STATE_KEY, JSON.stringify(s)); } catch (_) {  }
}
function _navMarkFailed(cid) {
  const s = _navLoad();
  s[cid] = { failed: true, failedAt: Date.now() };
  _navSave(s);
}
function _navIsFailed(cid) {
  const s = _navLoad();
  return !!(s[cid] && s[cid].failed);
}
function _navMarkPendingReload(cid) {
  const s = _navLoad();
  s[cid] = Object.assign({}, s[cid], { pendingReloadAt: Date.now() });
  _navSave(s);
}
// 随机整数 [min, max]（含端点）：用于会话间随机暂停，模拟真人操作节奏，规避平台风控。
const randInt = (min, max) => Math.floor(min + Math.random() * (max - min + 1));

export class BaseAdapter {
  constructor({ name, channel, SEL, hooks, rateLimiter }) {
    this.name = name;
    this.channel = channel;
    this.SEL = SEL || {};
    this.hooks = hooks || {};
    this.rateLimiter = rateLimiter || new RateLimiter();
    this.account = null;
    this.conversationId = null;
    this.seenNodes = new WeakSet();
    this._sentKeys = new Map(); 
    this._sentKeysMax = 5000;   
    this._sentKeysTtlMs = 120 * 60 * 1000; 
    this._sentKeysDirty = false;
    this._sentSaveTimer = null;
    this.observer = null;
    this.convPollTimer = null;
    this.fallbackTimer = null;
    this.activeRoot = null;
    this._graceTimer = null;
    this.callbacks = {};
    this.log = createLogger(channel, 'adapter');
    this._convWindow = new Map();
    this.domain = '';
    this._syncedConvIds = new Set();  
    this._syncingAll = false;         
    this._initialSyncDone = false;    
    this._rescanTimer = null;         
    this._patrolTimer = null;
    this._patrolling = false;
    this._patrolOpts = null;          
    this._patrolStarted = false;      
    this._lastPatrolAt = undefined;   
    this._accountEnabled = true;
    // 会话锚点（批：Bridge 重复消息根治——扩展侧）：会话激活/切换时快照
    // { cid, peerName, ts }。所有采集路径以锚点为准做两道校验：
    //   ① 新鲜度：当前 getConversationId() ≠ 锚点 cid ⇒ 抓取全部丢弃
    //     （URL/虚拟列表未切完的中间态，消息不知道属于谁）；
    //   ② sender 绑定：1v1 私信里 parsed.sender_name 与锚点 peerName 不一致 ⇒
    //     判跨会话残留拒收（线上实证 tz3rmdj：说缘 的消息被归到 做点吃的 的会话）。
    this._convAnchor = { cid: null, peerName: '', ts: 0 };      
    this._patrolStats = { rounds: 0, visited: 0, withNew: 0, captured: 0, failures: 0, lastRoundAt: 0, lastDurationMs: 0, lastScheduleMode: 'sequential', lastUnreadCount: 0 };
    // 批25：换会话互斥（下发优先，巡检让路）——见 beginOutbound 处的论证。
    this._outboundInFlight = 0;
    this._outboundStartedAt = 0;
    this._outboundLingerUntil = 0;
    this._patrolYield = { sendInFlight: 0 };
    // M-3：在途 sendMedia 的弃用令牌。sendMedia 被 withTimeout 放弃后，在途 hook 在
    // 点发送等不可逆动作前自检该令牌，命中即清场退出 —— 防降级文本已发出、
    // 被弃的 hook 迟到又点一次发送的双发（详见 media-inject.js 顶部注释）。
    this._activeMediaAbort = null;
    // M-5：未读优先巡检的渠道开关集合（来自参数中心，经 /api/bridge/capabilities 下发）。
    //   空集 = 开关关 = 现行顺序遍历。本地 localStorage 覆盖优先级更高（见 _patrolUnreadFirstEnabled）。
    this._unreadFirstChannels = new Set();
    // M-6：followUser 自动关注动作。默认关（caps 下发 bridge/follow_user_enabled）；
    //   _followUserAt 为会话级关注冷却表（内存，见 FOLLOW_ACTION_DEFAULTS）。
    this._followUserEnabled = false;
    this._followUserChannels = [];
    this._followUserAt = new Map();
  }

  setAccount(account) { this.account = account; }

  // P1-P6：账号级启停 —— 暂停即停巡检（kill switch 立即生效，不等下轮轮询）。
  isAccountEnabled() { return this._accountEnabled !== false; }

  setAccountEnabled(enabled) {
    this._accountEnabled = !!enabled;
    if (this._accountEnabled) {
      // 恢复启用：若此前已启动过巡检，自动重开（保持频率配置）
      if (this._patrolStarted) {
        const intervalMs = this._patrolIntervalFromConfig() || PATROL_DEFAULTS.intervalMs;
        this.startPatrol({ intervalMs });
      }
    } else {
      this._stopPatrol();
    }
    return this._accountEnabled;
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // 批25：换会话互斥 —— 不可逆性优先级：下发 > 巡检
  // ═══════════════════════════════════════════════════════════════════════════
  // 问题（结构性必现，不是偶发竞态）：
  //   巡检（patrol/_patrolVisit）与下发（sendOutbound）都会 openConversation —— 点左栏项换右侧
  //   会话，而两条链路互相不知道对方在干什么。下发的键入是**扩展自己**干的逐段拟人
  //   insertText（dom.js fillContentEditableHumanized：30 字≈2.7s、100 字≈9s，长文案上探 120s）；
  //   巡检一轮最多 6 个会话、每次切换后 3–5s 一个 ⇒ 切换节拍 < 键入窗，键入期几乎必然被切走。
  //   后果：文字填进了 A 的输入框，点发送却落在切换后的 B 会话上 ——「正在给 A 打字，跳到了
  //   下一个用户」。这条路径不可撤销，且 send_verified 回查在错误会话里数气泡只会判"未见"，
  //   记账侧照样 ack 成功（"已送达"），线上不会报错。
  // 为什么由巡检让路、下发不让路：
  //   - 巡检是可延迟的只读扫描：晚一轮的代价只是时延（seenNodes + 稳定键去重保证延迟不漏不重）；
  //   - 下发是不可撤销的写入：错发人＝隐私事故。
  // 为什么用"让路"而不是"抢锁"：抢锁要排队，就引入等待与死锁面；单向让路没有反向等待。
  // 临界区边界（"完成输入、上报才继续巡检"）：
  //   openConversation → 稳定等待 → 逐段键入 → 点击 → send_verified 回查 → 自报上行入队，
  //   全程持锁；释放后只剩纯 HTTP（uplink 合并窗 + outbox ack），不碰 DOM。
  //   再留 lingerMs 的尾巴：downlink 是逐条 sendOutbound 的，两条消息之间有 await 空档，
  //   不留尾巴巡检就会插进来切一次会话、下一条再切回去（白跑两次切换）。
  beginOutbound() {
    this._outboundInFlight = (this._outboundInFlight || 0) + 1;
    this._outboundStartedAt = Date.now();
    this._outboundLingerUntil = 0;
  }

  endOutbound() {
    this._outboundInFlight = Math.max(0, (this._outboundInFlight || 0) - 1);
    if (this._outboundInFlight === 0) {
      this._outboundStartedAt = 0;
      this._outboundLingerUntil = Date.now() + OUTBOUND_LINGER_MS;
    }
  }

  // 「下发占着页面 DOM」= 在途计数 > 0 或 linger 尾巴未过。
  isSendBusy() {
    if ((this._outboundInFlight || 0) > 0) return true;
    return Date.now() < (this._outboundLingerUntil || 0);
  }

  // 换会话的互斥判据：返回 null 表示此刻可以切会话。
  //   （"人工也在打字"这一层已按用户口径移除：本条链路的键入全由扩展自己完成，
  //     混进人的草稿判据会把巡检在未知页面上按停，换不来对下发链路的任何保护。）
  convSwitchBlocker() {
    if (this.isSendBusy()) return 'send-in-flight';
    return null;
  }

  _noteYield(blocker) {
    if (!this._patrolYield) this._patrolYield = { sendInFlight: 0 };
    if (blocker === 'send-in-flight') this._patrolYield.sendInFlight++;
  }

  // 等一个可切会话的窗口：下发是"发完就有间隙"的，所以让路的粒度是「每次切换」而不是「整轮」，
  // 巡检照样能在发送间隙里推进，不会因为下行密集而整轮饿死。
  async _waitForSwitchWindow(maxWaitMs = PATROL_DEFAULTS.switchBlockWaitMs, pollMs = PATROL_DEFAULTS.switchBlockPollMs) {
    const deadline = Date.now() + Math.max(0, maxWaitMs);
    for (;;) {
      const blocker = this.convSwitchBlocker();
      if (!blocker) return null;
      if (Date.now() >= deadline) return blocker;
      await sleep(Math.max(10, pollMs));
    }
  }

  match() { return this.hooks.match ? this.hooks.match() : false; }
  matchMode() {
    if (!this.match()) return null;
    if (this.hooks.matchMode) return this.hooks.matchMode();
    return 'strict';
  }
  snapshotMeta() {
    if (this.hooks.snapshotMeta) return this.hooks.snapshotMeta();
    return {
      accountId: this.getAccountId() || '',
      conversationId: this.getConversationId() || null,
    };
  }
  getAccountId() { return this.hooks.getAccountId ? this.hooks.getAccountId() : (this.account || ''); }

  _hash(s) {
    let h1 = 0xdeadbeef ^ s.length, h2 = 0x41c6ce57 ^ s.length;
    for (let i = 0; i < s.length; i++) {
      const ch = s.charCodeAt(i);
      h1 = Math.imul(h1 ^ ch, 2654435761);
      h2 = Math.imul(h2 ^ ch, 1597334677);
    }
    h1 = Math.imul(h1 ^ (h1 >>> 16), 2246822507);
    h1 ^= Math.imul(h2 ^ (h2 >>> 13), 3266489909);
    h2 = Math.imul(h2 ^ (h2 >>> 16), 2246822507);
    h2 ^= Math.imul(h1 ^ (h1 >>> 13), 3266489909);
    return (4294967296 * (2097151 & h2) + (h1 >>> 0)).toString(36);
  }

  // per-pass 计数器：返回该内容键在本轮已出现的次数（0-based），并把计数推进一格。
  // 批量路径（巡检 / 回填 / getMessages）与增量路径的 _occurrenceInList 必须给出同一个数，
  // 所以两边都只按「本轮可见消息里排在前面」定义，不掺到达顺序。
  // —— 会话锚点（扩展侧错归防线）——
  // _refreshConvAnchor：会话激活/切换/每轮采集前刷新锚点。peerName 取自 hooks.getPeerName
  // （各渠道 content 脚本已导出的 header 昵称抓取），取不到时留空——空 peerName 只跳过②校验，
  // 不影响①。
  _refreshConvAnchor() {
    const cid = this.getConversationId() || null;
    let peerName = '';
    try {
      if (this.hooks.getPeerName) peerName = String(this.hooks.getPeerName() || '').trim();
    } catch (_) { peerName = ''; }
    if (cid !== this._convAnchor.cid || (peerName && peerName !== this._convAnchor.peerName)) {
      this._convAnchor = { cid, peerName, ts: Date.now() };
    }
    return this._convAnchor;
  }

  // _isConvAnchorStale：锚点新鲜度校验（①）。锚点惰性跟随 cid（每次采集入口先
  // _refreshConvAnchor）——锚点校验的价值不在「cid 何时变」（那由 convPolling 负责），
  // 而在「一批采集里 cid 与 sender 绑定必须自洽」：入口刷新后，循环内 cid 漂移
  // （SPA 路由切换进行中）⇒ 拒收这批里的后续节点。
  _isConvAnchorStale(cid) {
    if (!this._convAnchor.cid) this._refreshConvAnchor();
    return !cid || !this._convAnchor.cid || cid !== this._convAnchor.cid;
  }

  // _senderMatchesAnchor：sender 绑定校验（②）。仅对 1v1 私信 + 双方名字都非空时生效：
  //   消息上抓到的 sender_name 与锚点 peerName 不一致 ⇒ 这条气泡是另一会话的 DOM 残留。
  //   放行情形：群聊（sender_name 是成员昵称）、self/agent/系统（sender_name 本就为空）、
  //   锚点或消息任一侧名字缺失（抓取能力不足时宁可不拦，交给服务端错归闸兜底）。
  _senderMatchesAnchor(parsed) {
    if (!parsed || parsed.is_group) return true;
    const st = parsed.sender_type || '';
    if (st && st !== 'customer') return true;
    const anchor = this._convAnchor.peerName;
    const sender = String(parsed.sender_name || '').trim();
    if (!anchor || !sender) return true;
    return sender === anchor;
  }

  _bumpOccurrence(counter, cid, parsed) {
    const base = this._dedupKey(cid, parsed);
    const n = counter.get(base) || 0;
    counter.set(base, n + 1);
    return n;
  }

  // 稳定键（内容维度）+ 发生次数。occurrence 是「该内容键在本轮可见消息里排第几」（0-based）。
  // 为什么必须有 occurrence：同一会话里客户把同一句话说两遍是两条真实消息，只按
  // (会话|发送者|文本) 拦就是把第二条永远吞掉（§8.3-17，客户侧表现＝「说了没回」）。
  // 为什么 0 必须保持旧字节：_sentKeys 落 localStorage（_sentKey() 键），键形一变就等于全部重报一遍。
  //
  // 002-multi-type-messages：非文本消息的 text 常是「[图片]」占位符，两张不同图若只按
  // (会话|发送者|文本) 拦就会互相吞并 ⇒ 媒体消息的键并入 msgType 与 media_url
  // （与服务端 ContentHashMsgIDTyped 同构）。文本消息的键保持旧字节，_sentKeys 不重算。
  _dedupKey(cid, parsed, occurrence = 0) {
    const sender = parsed.sender_id || parsed.sender_name || '';
    const type = (parsed.msg_type || '').toLowerCase();
    const isMedia = isMediaMsgType(type);
    const text = parsed.text || '';
    const base = isMedia
      ? this._hash(`${cid}|${sender}|${type}|${parsed.media_url || ''}|${text}`)
      : this._hash(`${cid}|${sender}|${text}`);
    return occurrence > 0 ? `${base}#${occurrence}` : base;
  }

  // 内容键相同的气泡在本轮 items 顺序里排在该节点之前有几条 —— 增量路径一次只拿到一个
  // 节点，编号只能从 DOM 顺序数出来（批量路径用同一份语义的 per-pass 计数器）。
  // （历史备注：这里曾有 _uptextContent——媒体气泡占位符补齐的统一出口；实际补齐
  // 发生在 _emitMessage/_historyItem/_emitPatrolMessage 三处内联，本方法从未被调用，
  // 2026-11-10 审计 F8 按死代码移除。）

  // 媒体消息是否可上行：msg_type 非文本即可。
  // 2026-11-10 审计 F3：旧口径要求「有媒体 URL 或有文字」，但纯语音/emoji 气泡在
  // 渠道解析器里本就不产 URL（douyin.js 的 voice 分支不取 src）——纯语音/纯表情被
  // 四条采集路径整条蒸发，服务端的 [语音] 占位词表形同虚设。放宽为非文本即可上行：
  // content 缺失由上行构造时补占位符（_emitMessage/_historyItem/_emitPatrolMessage），
  // media_url 允许为空（服务端 AppendMediaContextToContent 对空 URL 原样返回）。
  // system/recall 不在此列（走各自的消息 id 前缀路径，由调用方单独处理）。
  _isUplodableMedia(parsed) {
    const type = (parsed.msg_type || '').toLowerCase();
    return isMediaMsgType(type);
  }

  _occurrenceInList(item, cid, parsed) {
    let items;
    try { items = this.getMessageItems() || []; } catch (_) { return 0; }
    const root = this.getMessageRoot();
    const base = this._dedupKey(cid, parsed);
    let n = 0;
    for (const node of items) {
      if (node === item) break;
      // 批24b：编号只由「本会话窗口里的顺序」决定。上一会话的残留气泡内容可以与本条相同，
      // 把它算进来会把真实那条从 mh:<hash> 挤成 mh:<hash>#1 —— 与 §8.3-17 的编号口径互相打脸。
      if (root && node && !root.contains(node)) continue;
      let p;
      try { p = this.parseMessageItem(node); } catch (_) { continue; }
      if (!p) continue;
      if (!p.text && !this._isUplodableMedia(p)) continue;
      if (this._dedupKey(cid, p) === base) n++;
    }
    return n;
  }

  // event_id 的生成规则与服务端钩子2.5 是一对契约：裸 `mh:<hash>` 表示「内容就是这条的全部
  // 身份」，服务端可以继续按内容嗅探；`mh:<hash>#<n>` 表示上报方按可见顺序声明的第 n+1 条，
  // 服务端只按 msg_id 精确判等、不再按内容吞（见 service/inbox_ingress_ingest.go 的
  // eventAssertsDistinctMessage）。改这里必须同时看那一边。
  _canonicalMsgId(item, cid, parsed, occurrence = 0) {
    const type = parsed.msg_type || 'text';
    // 002-multi-type-messages：媒体消息的哈希并入类型与 media_url（typedContentHash
    // 与服务端 ContentHashMsgIDTyped 同构），纯占位符不再互相碰撞。
    const hash = isMediaMsgType(type)
      ? typedContentHash(this.channel, cid || '', type, parsed.media_url || '', parsed.text || '')
      : contentHash(this.channel, cid || '', parsed.text || '');
    const id = occurrence > 0 ? `${hash}#${occurrence}` : hash;
    if (type === 'system' || type === 'recall') return `${type}:${id}`;
    return id;
  }

  _hasSent(key) {
    const ts = this._sentKeys.get(key);
    if (ts && Date.now() - ts > this._sentKeysTtlMs) {
      this._sentKeys.delete(key);
      return false;
    }
    return !!ts;
  }

  _markSent(key) {
    this._sentKeys.set(key, Date.now());
    this._sentKeysDirty = true;
    if (this._sentKeys.size > this._sentKeysMax) {
      // 简单 LRU：删除最旧的 20%
      const entries = [...this._sentKeys.entries()].sort((a, b) => a[1] - b[1]);
      const drop = Math.ceil(this._sentKeysMax * 0.2);
      for (let i = 0; i < drop; i++) this._sentKeys.delete(entries[i][0]);
    }
  }
  getConversationId() { return this.hooks.getConversationId ? this.hooks.getConversationId() : (this.snapshotMeta().conversationId || null); }
  getMessageRoot() {
    if (this.hooks.getMessageRoot) {
      const r = this.hooks.getMessageRoot();
      if (r) return r;
    }
    return this.hooks.getMessageListRoot ? this.hooks.getMessageListRoot() : null;
  }
  // B1-a：定位面的两处入口（消息项 / 会话列表）在这里统计命中率。
  //
  // 为什么把探针放在这两个胶水方法上，而不是每个渠道的 hooks 里：
  //   ① 5 个渠道的 getMessageItems 实现各不相同（虚拟列表、iframe、React fiber…），
  //      逐渠道埋点＝5 份口径，且新增渠道必然漏一处；而所有渠道都经过这两个方法。
  //   ② 这两个方法本身就是「选择器最终有没有产出」的裁决点：hooks 返回非空数组
  //      才说明这一轮的选择器是活的。
  // 计数只进内存、够量才上报（见 selector-health.js），热路径上不碰存储、不发网络。
  getMessageItems() {
    const items = this.hooks.getMessageItems ? this.hooks.getMessageItems() : [];
    this._recordSelectorHealth('messageItem', Array.isArray(items) && items.length > 0);
    return items;
  }
  parseMessageItem(item) {
    return this.hooks.parseMessageItem ? this.hooks.parseMessageItem(item) : null;
  }
  getConversationList() {
    const list = this.hooks.getConversationList ? this.hooks.getConversationList() : [];
    this._recordSelectorHealth('conversationList', Array.isArray(list) && list.length > 0);
    return list;
  }
  // _recordSelectorHealth 记一次定位尝试，累计够了就把增量推给 background。
  //
  // 上报走 background（service worker）而不是直接在 content script 里 fetch：
  //   · 页面切走/刷新会带走 content script 的内存计数，service worker 活得更久；
  //   · token/serverUrl 的读点在 background 的配置里，content 侧不必再持有一份凭证。
  // 没有 chrome.runtime（用例/jsdom）时静默跳过——统计面永远不该让业务代码抛错。
  _recordSelectorHealth(field, hit) {
    try {
      this._selectorHealth = this._selectorHealth || createSelectorHealthTracker();
      this._selectorHealth.record(this.channel, field, !!hit);
      if (!this._selectorHealth.shouldFlush(this.channel)) return;
      const { items } = this._selectorHealth.drain(this.channel);
      if (!items.length) return;
      if (typeof chrome === 'undefined' || !chrome.runtime || typeof chrome.runtime.sendMessage !== 'function') return;
      chrome.runtime.sendMessage({
        type: 'reportSelectorHealth',
        channel: this.channel,
        accountId: this.account || '',
        items,
      }, () => { try { void chrome.runtime.lastError; } catch (_) {  } });
    } catch (_) {  }
  }
  // 批25：opts 透传渠道 sendText（目前只有 { expectConv, assertConv }）：渠道在点发送前
  //   调 assertConvOrAbort 复核会话归属，漂移就清框中止。不传 = 旧行为（无复核）。
  rawSendText(text, opts) { return this.hooks.sendText ? this.hooks.sendText(text, opts) : Promise.resolve(false); }
  selfTest() { return this.hooks.selfTest ? this.hooks.selfTest() : []; }

  // ═══════════════════════════════════════════════════════════════════════════
  // M-6（2026-11-11）：followUser 自动关注/回关动作
  // ═══════════════════════════════════════════════════════════════════════════
  // 触发面：服务端策略（参数中心 bridge/follow_user_enabled，默认 false）经 outbox
  // msg_type=follow_user 动作行下发（HTTP 轮询与 SSE 双传输同形），downlink 分派到此。
  // 硬性边界：
  //   - 默认关：caps 未拉到/字段缺失/服务端开关关/渠道不在清单 → 一律拒绝执行；
  //   - 不阻塞 AI 回复主链路：本方法由 downlink 独立分派，且入口处让路
  //     （isSendBusy 时短等后跳过，绝不与 sendOutbound 临界区抢单页 DOM）；
  //   - 复用既有风控：rateLimiter（拟人最小间隔/账号桶/会话桶/会话冷却）+
  //     globalSendWaitMs 跨页全局间隔 + FOLLOW_ACTION_DEFAULTS.sessionCooldownMs
  //     会话级关注冷却，不另建节奏通道；
  //   - 失败留痕：结果经 downlink ack（delivered/failed）回写 hub 行终态，
  //     服务端 bridge_follow_events 审计表登记动作行。
  setFollowUserEnabled(enabled, channels) {
    this._followUserEnabled = enabled === true;
    this._followUserChannels = Array.isArray(channels) ? channels.filter((c) => typeof c === 'string' && c) : [];
    this.log.info('followUser 开关更新', { enabled: this._followUserEnabled, channels: this._followUserChannels.length });
  }

  async followUser({ conversationId } = {}) {
    // 闸1：默认关——caps 缺字段/拉取失败/服务端开关未开，永不执行。
    if (this._followUserEnabled !== true) {
      return { ok: false, skipped: 'disabled', via: 'follow_user' };
    }
    // 闸2：渠道未实现 followUser hook（如 xianyu 好友制无关注模型）。
    if (!this.hooks || typeof this.hooks.followUser !== 'function') {
      return { ok: false, skipped: 'no-hook', via: 'follow_user' };
    }
    // 闸3：渠道清单防御（caps 下发的 follow_user_channels）。fail-safe：清单缺省/为空
    // = 未授权（不可逆写动作，宁可不执行），与服务端 fail-open 默认清单互补——
    // 服务端保证了清单字段总在，扩展侧拿不到清单只可能是拉取异常。
    if (!Array.isArray(this._followUserChannels) || !this._followUserChannels.includes(this.channel)) {
      return { ok: false, skipped: 'channel-not-allowed', via: 'follow_user' };
    }
    const conv = String(conversationId || '').trim();
    if (!conv) return { ok: false, notFound: true, via: 'follow_user' };
    // 闸4：让路下发——AI 回复主链路优先。sendOutbound 临界区在途/linger 内短等，
    // 仍忙则跳过（动作行按 delivered 收口，warn 留痕；不重试不阻塞下行轮询）。
    const yieldBlocker = await this._waitForSwitchWindow(FOLLOW_ACTION_YIELD_WAIT_MS, PATROL_DEFAULTS.switchBlockPollMs);
    if (yieldBlocker) {
      this.log.warn('followUser 让路：下发在途，跳过本次关注动作', { conv_id: conv, blocker: yieldBlocker });
      return { ok: false, skipped: 'busy', via: 'follow_user' };
    }
    // 闸5：会话级关注冷却——重复动作行/ack 丢包重推不重复点击（跳过按 delivered 收口）。
    const lastAt = (this._followUserAt && this._followUserAt.get(conv)) || 0;
    if (Date.now() - lastAt < FOLLOW_ACTION_DEFAULTS.sessionCooldownMs) {
      this.log.info('followUser 会话冷却中，跳过', { conv_id: conv });
      return { ok: true, skipped: 'cooldown', via: 'follow_user' };
    }
    // 定位会话：与 sendOutbound 同一原语；目标非当前会话时切换（preemptible:false，
    // 已过闸4 = 此刻无下发在途，切换不可再让路给巡检，否则点错会话的关注按钮）。
    const account = this.getAccountId();
    const current = this.getConversationId();
    if (current !== conv) {
      const opened = await this.openConversation(conv, { backfill: false, exact: true, preemptible: false });
      if (!opened) {
        return { ok: false, notFound: true, via: 'follow_user' };
      }
    }
    // 闸6：复用既有拟人限速（与文本发送同一条 rateLimiter）。
    // 'follow_user' 占位作为同文案去重键；拒绝时不重试——rateLimited 交由动作行
    // 留 pending 重推（与文本 rateLimited 行为一致），其余失败 ack failed 终态。
    const decision = this.rateLimiter.tryAcquire(this.channel, account, conv, 'follow_user');
    if (!decision.allowed) {
      this.log.warn('followUser 被风控拦截', { conv_id: conv, reason: decision.reason });
      return { ok: false, rateLimited: true, reason: decision.reason, via: 'follow_user' };
    }
    const gateWaitMs = await globalSendWaitMs(this.rateLimiter.cfg.minIntervalMs);
    const waitMs = Math.max(gateWaitMs, decision.waitHintMs || 0);
    if (waitMs > 0) await sleep(Math.min(waitMs, 5000));
    let result;
    try {
      result = await this.hooks.followUser({ conversationId: conv });
    } catch (e) {
      result = { ok: false, error: 'hook_error:' + String((e && e.message) || e) };
    }
    if (result && result.ok) {
      this._followUserAt.set(conv, Date.now());
      this.rateLimiter.markSent(this.channel, account, conv, 'follow_user');
      stampGlobalSendAt();
    } else {
      this.log.warn('followUser 执行失败（动作行 ack failed 留痕 + 服务端审计登记）', {
        conv_id: conv, skipped: result && result.skipped, error: result && result.error,
      });
    }
    return { ...(result || { ok: false, error: 'no-result' }), via: 'follow_user' };
  }

  getMessages({ limit = 100 } = {}) {
    this._refreshConvAnchor();
    const out = [];
    const occ = new Map();
    let items;
    try { items = this.getMessageItems() || []; } catch (_) { items = []; }
    const cid = this.getConversationId() || this.conversationId || '';
    for (const item of items) {
      if (out.length >= limit) break;
      if (!item) continue;
      let parsed;
      try { parsed = this.parseMessageItem(item); } catch (_) { continue; }
      if (!parsed) continue;
      // 002-multi-type-messages：媒体消息不再整行丢弃——text 缺失时由上行侧补占位符。
      if (!parsed.text && !this._isUplodableMedia(parsed)) continue;
      // 会话归属校验：getMessageItems(root=document) 可能返回虚拟列表残留的
      // 上一会话 DOM 节点。检查 item 是否仍在活动消息容器内（getMessageRoot()）。
      // 不在当前会话容器的节点 → 跨会话残留 → 丢弃。
      const root = this.getMessageRoot();
      if (root && item && !root.contains(item)) continue;
      // 会话锚点校验：cid 与锚点不一致（中间态）或 sender 与锚点昵称错配 ⇒ 丢弃。
      if (this._isConvAnchorStale(cid)) continue;
      if (!this._senderMatchesAnchor(parsed)) continue;
      // 稳定键去重（跨 DOM 重渲染）：与 _handleIncremental/_backfill/_collectUnseenText 共用
      // _sentKeys。PollingLoop 每秒巡检、每轮重抓全部可见消息，若不做去重，同一逻辑消息会被反复上行；
      // 规范 event_id（mh:<hash>，同内容第二条起为 mh:<hash>#<n>）让后端按稳定键幂等去重
      // （即便 _sentKeys 过期也能兜底）。
      const occurrence = this._bumpOccurrence(occ, cid, parsed);
      const key = this._dedupKey(cid, parsed, occurrence);
      if (this._hasSent(key)) continue;
      parsed.message_id = this._canonicalMsgId(item, cid, parsed, occurrence);
      this._markSent(key);
      out.push({
        message_id: parsed.message_id || '',
        sender_id: parsed.sender_id || '',
        sender_name: parsed.sender_name || '',
        sender_type: parsed.sender_type || SENDER.CUSTOMER,
        text: parsed.text || '',
        msg_type: parsed.msg_type || 'text',
        timestamp: parsed.timestamp || Date.now(),
        is_group: !!parsed.is_group,
        group_id: parsed.group_id || '',
        group_name: parsed.group_name || '',
        media_url: parsed.media_url || '',
      });
    }
    return out;
  }

  // 批24（§8.3-8）：可见气泡里与 text 归一化后相等的条数。**只读**——不碰 _sentKeys、
  // 不推 occurrence、不进 _convWindow：这三件事任何一件被回查做掉，都会把本该上行的气泡
  // 吃掉（与 §8.3-17 的发生次数身份直接冲突）。跨会话残留节点的排除与 getMessages 同一条
  // （root.contains），两边口径不许漂。
  _countVisibleText(text) {
    const want = normSendText(text);
    if (!want) return 0;
    let items;
    try { items = this.getMessageItems() || []; } catch (_) { items = []; }
    const root = this.getMessageRoot();
    let n = 0;
    for (const item of items) {
      if (!item) continue;
      if (root && !root.contains(item)) continue;
      let parsed;
      try { parsed = this.parseMessageItem(item); } catch (_) { continue; }
      if (!parsed) continue;
      // 媒体气泡不参与文本回查计数（002-multi-type-messages）：回查判据是
      // 「同文本可见计数增加」，媒体消息 text 是占位符，混入会把发送回查误判。
      if (parsed.msg_type && parsed.msg_type !== 'text') continue;
      if (normSendText(parsed.text) === want) n++;
    }
    return n;
  }

  // 复核判据是「计数增加」而不是「文本命中」：客户先说过同一句话时，后者会把一次
  // 失败的发送判成已送达 —— 那比不判更糟。返回 false 只意味「没见着」，绝不意味「没发出去」。
  async _verifySendLanded(text, baseline, waitMs) {
    const deadline = Date.now() + Math.max(0, waitMs);
    for (;;) {
      if (this._countVisibleText(text) > baseline) return true;
      const left = deadline - Date.now();
      if (left <= 0) return false;
      await sleep(Math.min(BRIDGE_THREE_CHANNEL.sendVerifyPollMs, left));
    }
  }

  start(callbacks = {}) {
    try {
      this.stop();
      this.callbacks = callbacks;
      if (!this.match()) {
        this.log.warn('页面不匹配，适配器未启动');
        return false;
      }
      try { this.domain = location && location.host ? location.host : ''; } catch (_) { this.domain = ''; }
      const meta = this.snapshotMeta();
      this.account = meta.accountId || this.account;
      this.conversationId = this.getConversationId();
      this._attachConversation();
      this._startConvPolling();
      this._loadSyncedConvIds();
      this._loadSentKeys();
      this._scheduleInitialSync();
      this._startRescan();
      this._startPatrolAuto();
      if (!this._sentSaveTimer) {
        this._sentSaveTimer = setInterval(() => this._saveSentKeys(), 30000);
      }
      return true;
    } catch (e) {
      this.log.error('适配器启动失败，已隔离异常', e);
      try { this.stop(); } catch (_) {  }
      return false;
    }
  }

  stop() {
    // B5（批3）泄漏族收口：全部 timer 清除并置 null（幂等——重复 stop 不误清新实例），
    // 补漏原实现未清的 _mutationTimer（stop 后 100ms 窗口内仍会触发一次 _flushMutations）。
    if (this.observer) this.observer.disconnect();
    if (this.convPollTimer) { clearInterval(this.convPollTimer); this.convPollTimer = null; }
    if (this.fallbackTimer) { clearInterval(this.fallbackTimer); this.fallbackTimer = null; }
    if (this._graceTimer) { clearTimeout(this._graceTimer); this._graceTimer = null; }
    if (this._rescanTimer) { clearInterval(this._rescanTimer); this._rescanTimer = null; }
    if (this._mutationTimer) { clearTimeout(this._mutationTimer); this._mutationTimer = null; }
    this._pendingMutations = [];
    this._stopPatrol();
    if (this._sentSaveTimer) { clearInterval(this._sentSaveTimer); this._sentSaveTimer = null; }
    this._saveSentKeys();
    this._convWindow.clear();
  }

  _startConvPolling() {
    this._pendingCid = null;
    this.convPollTimer = setInterval(() => {
      const cid = this.getConversationId();
      if (!cid) { this._pendingCid = null; return; }
      if (cid === this.conversationId) { this._pendingCid = null; return; }
      if (this._pendingCid !== cid) {
        this._pendingCid = cid;
        return;
      }
      this._pendingCid = null;
      this.log.info(`会话切换: ${this.conversationId} -> ${cid}`);
      this.conversationId = cid;
      this._attachConversation();
      this._refreshConvAnchor();
    }, 2000);
  }

  _attachConversation() {
    this._refreshConvAnchor();
    const root = this.getMessageRoot();
    this.activeRoot = root;
    if (root) {
      this._observe(root);
      this._backfill(root); 
    }
    if (this._graceTimer) clearTimeout(this._graceTimer);
    this._graceTimer = setTimeout(() => {
      const r = this.getMessageRoot();
      if (r) this._backfill(r);
    }, 1500);
    if (this.fallbackTimer) clearInterval(this.fallbackTimer);
    this.fallbackTimer = setInterval(() => this._scanIncremental(), 3000);
  }

  _observe(root) {
    if (this.observer) this.observer.disconnect();
    this.observer = new MutationObserver((muts) => this._onMutations(muts));
    this.observer.observe(root, { childList: true, subtree: true });
  }

  _addedElements(muts) {
    const out = [];
    for (const m of muts) {
      if (m.addedNodes) {
        for (const n of m.addedNodes) {
          if (n.nodeType === 1) out.push(n);
        }
      }
    }
    return out;
  }

  _onMutations(muts) {
    this._pendingMutations = (this._pendingMutations || []).concat(muts);
    if (this._mutationTimer) return;
    this._mutationTimer = setTimeout(() => {
      this._mutationTimer = null;
      const all = this._pendingMutations || [];
      this._pendingMutations = [];
      this._flushMutations(all);
    }, 100);
  }

  _flushMutations(muts) {
    // 用用户配置选择器优先 → SEL 默认兜底
    let itemSelectors = [];
    try {
      // 尝试从渠道 hooks 获取合并后的选择器（含用户配置）
      const selectors = this.hooks.mergedSelectors ? this.hooks.mergedSelectors() : null;
      if (selectors && selectors.itemSelectors && selectors.itemSelectors.length) {
        itemSelectors = selectors.itemSelectors;
      }
    } catch (_) {  }
    if (!itemSelectors.length) {
      itemSelectors = (this.SEL.MSG_ITEM || '').split(',').map((s) => s.trim()).filter(Boolean);
    }
    const nodes = this._addedElements(muts);
    // 上限：单次回调最多处理 50 个新增 DOM 节点（防抖屏 OOM）
    const MAX_PER_FLUSH = 50;
    let processed = 0;
    for (const node of nodes) {
      if (processed >= MAX_PER_FLUSH) break;
      let items = [];
      for (const sel of itemSelectors) {
        try {
          if (node.matches && node.matches(sel)) items.push(node);
          if (node.querySelectorAll) {
            const sub = node.querySelectorAll(sel);
            for (const s of sub) items.push(s);
          }
        } catch (_) {  }
      }
      for (const item of items) {
        if (processed >= MAX_PER_FLUSH) break;
        this._handleIncremental(item);
        processed++;
      }
    }
  }

  _scanIncremental() {
    this._refreshConvAnchor();
    const items = this.getMessageItems();
    for (const item of items) this._handleIncremental(item);
  }


  _syncedKey() {
    return `hivebridge:synced:${this.channel}:${this.domain || (typeof location !== 'undefined' ? location.host : '')}`;
  }

  _loadSyncedConvIds() {
    try {
      if (typeof localStorage === 'undefined') return;
      const raw = localStorage.getItem(this._syncedKey());
      if (!raw) return;
      const arr = JSON.parse(raw);
      if (Array.isArray(arr)) for (const id of arr) this._syncedConvIds.add(id);
    } catch (_) {  }
  }

  _saveSyncedConvIds() {
    try {
      if (typeof localStorage === 'undefined') return;
      localStorage.setItem(this._syncedKey(), JSON.stringify(Array.from(this._syncedConvIds)));
    } catch (_) {  }
  }

  _sentKey() {
    return `hivebridge:sent:${this.channel}:${this.domain || (typeof location !== 'undefined' ? location.host : '')}`;
  }

  _loadSentKeys() {
    try {
      if (typeof localStorage === 'undefined') return;
      const raw = localStorage.getItem(this._sentKey());
      if (!raw) return;
      const arr = JSON.parse(raw);
      if (Array.isArray(arr)) {
        const now = Date.now();
        for (const [key, ts] of arr) {
          if (now - ts < this._sentKeysTtlMs) this._sentKeys.set(key, ts);
        }
      }
    } catch (_) {  }
  }

  _saveSentKeys() {
    try {
      if (typeof localStorage === 'undefined') return;
      if (!this._sentKeysDirty) return;
      this._sentKeysDirty = false;
      // 仅保留最近 _sentKeysMax 条（按时间戳升序，旧者先裁）
      const arr = [...this._sentKeys.entries()]
        .sort((a, b) => a[1] - b[1])
        .slice(-this._sentKeysMax);
      localStorage.setItem(this._sentKey(), JSON.stringify(arr));
    } catch (_) {  }
  }

  _scheduleInitialSync() {
    if (this._initialSyncDone) return;
    this._initialSyncDone = true;
    setTimeout(() => {
      this.syncAllConversations({ throttleMs: 1500 }).catch(() => {});
    }, RESCAN_DEFAULTS.initialSyncDelayMs);
  }

  // M-5：rescan 间隔三档取值——localStorage 覆盖 > 未读优先档 > 现行档。
  //   开关关 = intervalMs（10min，现行值逐字节不变）；开关开 = intervalMsUnreadFirst
  //   （30min 降频：常规覆盖交给未读优先巡检，rescan 只当漏检兜底）。
  _rescanIntervalMs() {
    try {
      if (typeof localStorage !== 'undefined') {
        const v = localStorage.getItem(`hivebridge:rescan:${this.channel}`);
        if (v) {
          const n = parseInt(v, 10);
          if (Number.isFinite(n) && n > 0) return n;
        }
      }
    } catch (_) {  }
    return this._patrolUnreadFirstEnabled()
      ? RESCAN_DEFAULTS.intervalMsUnreadFirst
      : RESCAN_DEFAULTS.intervalMs;
  }

  _startRescan() {
    if (this._rescanTimer) clearInterval(this._rescanTimer);
    this._rescanTimer = setInterval(() => {
      this.syncAllConversations({ throttleMs: 1500 }).catch(() => {});
    }, this._rescanIntervalMs());
  }

  // ═══════════════════════════════════════════════════════════════════════════
  // M-5（2026-11-11）：巡检未读优先 —— 渠道级开关与调度
  // ═══════════════════════════════════════════════════════════════════════════
  // 现行问题：patrol 每轮全量遍历会话列表、只访问前 maxPerRound=6 个（从未读徽章
  //   视角看是"盲访"——unreadCount 恒为 0，各渠道 detectUnread 的结果被丢弃）。
  // 新调度：开关开启的渠道，巡检只把带未读徽章的会话放进本轮访问配额；未读为 0
  //   的会话不再进全量遍历（活动会话靠 MutationObserver + 3s fallbackTimer 实时
  //   覆盖，非活动会话靠降频后的 rescan 兜底）。
  // 回退安全（不能因新逻辑漏消息）：
  //   - 开关关 = 完全现行行为（顺序遍历）；
  //   - 列表项缺 unread 布尔字段（渠道未接线/返回异常）→ 该轮 fail-open 回顺序遍历；
  //   - 调度分区自身抛错 → 该轮 fail-open 回顺序遍历。
  // 开关来源与优先级：
  //   1. 本地 localStorage `hivebridge:patrolUnreadFirst:<channel>`（"1"/"0"，应急覆盖）；
  //   2. 参数中心经 /api/bridge/capabilities 下发的渠道清单（setPatrolUnreadFirstChannels）；
  //   3. 默认关。
  setPatrolUnreadFirstChannels(list) {
    const prev = this._patrolUnreadFirstEnabled();
    const set = new Set();
    if (Array.isArray(list)) {
      for (const ch of list) {
        if (typeof ch === 'string' && ch) set.add(ch);
      }
    }
    this._unreadFirstChannels = set;
    const next = this._patrolUnreadFirstEnabled();
    // 开关状态翻转且 rescan 已在跑 → 按新模式重排间隔（关=10min / 开=30min；覆盖值优先）。
    if (prev !== next && this._rescanTimer) this._startRescan();
    return next;
  }

  _patrolUnreadFirstEnabled() {
    try {
      if (typeof localStorage !== 'undefined') {
        const v = localStorage.getItem(`hivebridge:patrolUnreadFirst:${this.channel}`);
        if (v === '1') return true;
        if (v === '0') return false;
      }
    } catch (_) {  }
    return this._unreadFirstChannels instanceof Set && this._unreadFirstChannels.has(this.channel);
  }

  async _waitForActiveConversation(targetId, prevCid, timeout = 5000, exact = false) {
    const start = Date.now();
    while (Date.now() - start < timeout) {
      const cur = this.getConversationId();
      if (cur === targetId) return cur; 
      if (!exact && cur && cur !== prevCid) return cur; 
      await sleep(200);
    }
    return null;
  }

  async openConversation(cid, { waitActiveMs = 5000, backfill = false, exact = false, preemptible = true } = {}) {
    if (!cid) return null;
    // 批25：可抢占的换会话（巡检 / 巡检收尾回切 / 全量同步）在"下发在途（含 linger）"时
    //   一律不切，返回 null 由调用方下一轮/下个窗口再来。下发走 preemptible:false
    //   —— 下发永不因巡检让路（不可逆性不对称，见 beginOutbound 处的论证）。
    if (preemptible) {
      const blocker = this.convSwitchBlocker();
      if (blocker) {
        this._noteYield(blocker);
        this.log.debug(`换会话让路：${blocker}（目标 ${cid}）`);
        return null;
      }
    }
    const cur = this.getConversationId();
    if (cur === cid) {
      if (backfill) {
        this._backfill();
      }
      return cur;
    }
    if (_navIsFailed(cid) === false && _navLoad()[cid] && _navLoad()[cid].pendingReloadAt) {
      _navMarkFailed(cid);
      throttledWarn(this.log, `openConvNavFail:${cid}`, WARN_THROTTLE_MS,
        `整页导航后仍无法打开会话 ${cid}（小红书深链无法打开屏外会话），停止破坏性重载，留 pending 待用户打开`);
    }
    let list;
    try {
      list = this.getConversationList() || [];
    } catch (_) { list = []; }
    if (!Array.isArray(list)) list = [];
    // 命中目标：优先精确 id；兼容 id 表述不一致（如 /user/ 链接 vs data-* / conv: 前缀）。
    //   双向 conv: 前缀匹配：缺任一方向都会导致巡检用 data-conv-id 找会话时找不到 → 巡检失效。
    let target = list.find((c) => c && c.id === cid);
    if (!target) {
      target = list.find((c) => c && (
        c.id === cid.replace(/^conv:/, '') ||
        ('conv:' + c.id) === cid ||
        c.id === 'conv:' + cid ||
        c.id.replace(/^conv:/, '') === cid
      ));
    }
    if (!target || !target.el) {
      if (cid.startsWith('conv:')) {
        const name = cid.slice('conv:'.length);
        // 昵称派生 id 的匹配顺序：精确等值 → 双向包含。包含匹配命中多个不同列表项时
        // 视为歧义，宁可不发也不能点到错误的会话（find 顺序取首个曾导致 A 的文案
        // 被发进 B 的会话——昵称互为子串或改昵后残留旧名都会命中多项）。
        const exacts = list.filter((c) => c && c.el && c.name && c.name === name);
        if (exacts.length > 0) {
          target = exacts[0];
        } else {
          const partials = list.filter((c) => c && c.el && c.name && name
            && (c.name.includes(name) || name.includes(c.name)));
          const uniqEls = new Set(partials.map((c) => c.el));
          if (partials.length === 1 || (partials.length > 1 && uniqEls.size === 1)) {
            target = partials[0];
          } else if (partials.length > 1) {
            throttledWarn(this.log, `openConvAmbig:${name}`, WARN_THROTTLE_MS,
              `会话昵称「${name}」模糊匹配到 ${uniqEls.size} 个不同会话项，拒绝切换防误发`, null);
          }
        }
      }
    }
    if (!target || !target.el) {
      // 真实 id 屏外：URL 导航兜底（SPA 路由或整页导航）。conv:<名> 与占位账号 URL 导航无意义，返回 null。
      const navOpened = await this._openConversationByNavigation(cid);
      if (navOpened) return navOpened;
      throttledWarn(this.log, `openConv:${cid}`, WARN_THROTTLE_MS, `左侧列表未找到目标会话 ${cid}`);
      return null;
    }
    // conv:<名> 派生 id：活动会话真实 id 与 conv: 名永不相等，只能用「切到非 prevCid」判定，
    // 不能用精确匹配（否则永远返回 null）。name 匹配已保证点击的是正确会话项。
    const useExact = !cid.startsWith('conv:') && exact;
    const prevCid = this.getConversationId();
    if (typeof target.el.scrollIntoView === 'function') {
      try { target.el.scrollIntoView({ block: 'nearest' }); } catch (_) {  }
    }
    try {
      if (target.el.tagName === 'A' || target.el.tagName === 'BUTTON') target.el.click();
      else simulateRealClick(target.el);
    } catch (_) {  }
    const opened = await this._waitForActiveConversation(cid, prevCid, waitActiveMs, useExact);
    if (!opened) {
      this.log.warn(`会话 ${cid} 点击后未打开线程`);
      return null;
    }
    this.log.info(`已切换到会话 ${opened}`);
    if (backfill) {
      this._backfill();
    }
    return opened;
  }

  async _openConversationByNavigation(cid) {
    if (this.channel !== 'xiaohongshu') return null;
    if (!cid || cid.startsWith('conv:') || cid.endsWith('-unknown')) return null;
    if (_navIsFailed(cid)) return null;
    const prev = this.getConversationId();
    try {
      if (typeof history !== 'undefined' && typeof history.pushState === 'function') {
        history.pushState({}, '', `/chat/${cid}`);
        if (typeof window !== 'undefined' && typeof window.dispatchEvent === 'function') {
          try { window.dispatchEvent(new PopStateEvent('popstate')); } catch (_) {  }
        }
      }
    } catch (_) {  }
    const opened = await this._waitForActiveConversation(cid, prev, 5000, true);
    if (opened) return opened;
    try {
      if (typeof location !== 'undefined' && location.href) {
        _navMarkPendingReload(cid);
        location.href = `https://www.xiaohongshu.com/chat/${cid}`;
      }
    } catch (_) {  }
    return null;
  }

  _findListScroller() {
    try {
      const list = this.getConversationList();
      const probe = list && list[0] && list[0].el;
      if (!probe) return null;
      let cur = probe.parentElement;
      while (cur && cur !== document.body) {
        const style = (typeof getComputedStyle === 'function') ? getComputedStyle(cur) : null;
        const scrollable =
          style && (style.overflowY === 'auto' || style.overflowY === 'scroll' || cur.scrollHeight > cur.clientHeight + 50);
        if (scrollable) return cur;
        cur = cur.parentElement;
      }
    } catch (_) {  }
    return null;
  }

  async syncAllConversations({ max = 0, throttleMs = 1500, waitActiveMs = 5000, scrollLoadMs = 1500, onProgress } = {}) {
    if (this._syncingAll) {
      this.log.warn('会话全量同步已在进行，忽略重复触发');
      return { skipped: true, reason: 'in-progress' };
    }
    const hook = this.hooks.getConversationList;
    if (typeof hook !== 'function') {
      return { skipped: true, reason: 'no-hook' };
    }
    this._syncingAll = true;
    this.log.info('开始遍历同步私信会话（多轮扫描至列表底部）…');
    let done = 0;
    let failures = 0;
    let scannedTotal = 0;
    let blockedBy = null;
    try {
      let pass = 0;
      const MAX_PASSES = 16;
      outer: while (pass < MAX_PASSES) {
        pass++;
        let list = [];
        try {
          list = hook() || [];
        } catch (e) {
          this.log.error('读取会话列表失败', e);
          break;
        }
        if (!Array.isArray(list)) list = [];
        // 去重 + 跳过已同步（持久化续传）
        const seen = new Set();
        list = list.filter((c) => {
          if (!c || !c.id) return false;
          if (seen.has(c.id)) return false;
          seen.add(c.id);
          return true;
        });
        list = list.filter((c) => !this._syncedConvIds.has(c.id));
        if (max > 0 && list.length > max) list = list.slice(0, max);
        if (!list.length) break; 
        for (const conv of list) {
          try {
            // 批25：全量同步同样是"换会话"，遇下发在途（含 linger）就让路。本次同步提前收尾，
            //   已同步进度落在 _syncedConvIds（持久化），下次 rescan/巡检续跑，不会重复扫。
            const blocker = await this._waitForSwitchWindow();
            if (blocker) {
              this._noteYield(blocker);
              this.log.info(`全量同步让路：${blocker}，本轮提前收尾（已同步进度已落盘）`);
              blockedBy = blocker;
              break outer;
            }
            // 复用 openConversation：左侧列表找会话项 → 点击打开线程（兼容虚拟列表 & React 事件）。
            // 若活动会话已是目标（列表首项=当前打开会话）无需点击，直接回填。
            const opened = await this.openConversation(conv.id, { waitActiveMs, backfill: true });
            if (!opened) {
              this.log.warn(`会话 ${conv.id} 点击后未打开线程，跳过回填`, { name: conv.name });
              failures++;
              continue;
            }
            this._syncedConvIds.add(opened);
            if (conv.id && conv.id !== opened) this._syncedConvIds.add(conv.id);
            this._saveSyncedConvIds();
            done++;
            scannedTotal++;
            if (onProgress) {
              try { onProgress({ done, total: scannedTotal, id: conv.id, name: conv.name }); } catch (_) {  }
            }
            await sleep(throttleMs);
          } catch (e) {
            this.log.error('同步单个私信会话异常', conv.id, e);
            failures++;
            continue;
          }
        }
        // 本轮结束：滚动列表到底部，等待加载更多（虚拟/懒加载列表），继续下一轮。
        //   间隔必须够长（scrollLoadMs，默认 1500ms）：太快时虚拟列表还没加载完就开下一轮 → 漏抓 + 平台风控
        const scroller = this._findListScroller();
        if (scroller) {
          try { scroller.scrollTop = scroller.scrollHeight; } catch (_) {  }
          await sleep(scrollLoadMs);
        } else {
          break; 
        }
      }
    } finally {
      this._syncingAll = false;
    }
    this.log.info(`私信会话遍历同步完成：成功 ${done}，失败 ${failures}`);
    return { synced: done, total: scannedTotal, failures, yielded: blockedBy || null };
  }


  _startPatrolAuto() {
    const intervalMs = this._patrolIntervalFromConfig() || PATROL_DEFAULTS.intervalMs;
    this.startPatrol({ intervalMs });
  }

  _patrolIntervalFromConfig() {
    try {
      if (typeof localStorage === 'undefined') return 0;
      const v = localStorage.getItem(`hivebridge:patrol:${this.channel}`);
      if (!v) return 0;
      const n = parseInt(v, 10);
      return Number.isFinite(n) && n > 0 ? n : 0;
    } catch (_) { return 0; }
  }
  _savePatrolInterval(ms) {
    try {
      if (typeof localStorage === 'undefined') return;
      if (!ms || ms <= 0) localStorage.removeItem(`hivebridge:patrol:${this.channel}`);
      else localStorage.setItem(`hivebridge:patrol:${this.channel}`, String(Math.max(1000, Math.floor(ms))));
    } catch (_) {  }
  }

  startPatrol(opts = {}) {
    const intervalMs = opts.intervalMs && opts.intervalMs > 1000
      ? Math.floor(opts.intervalMs)
      : (this._patrolIntervalFromConfig() || PATROL_DEFAULTS.intervalMs);
    this._patrolStarted = true;
    // P1-P6：账号暂停时不启动巡检（_startPatrolAuto / 恢复启用都会走这里）
    if (!this.isAccountEnabled()) {
      this.log.debug('账号已暂停，巡检不启动');
      return { ok: false, skipped: true, reason: 'account-paused' };
    }
    this._patrolOpts = {
      intervalMs,
      throttleMs: opts.throttleMs || PATROL_DEFAULTS.throttleMs,
      waitActiveMs: opts.waitActiveMs || PATROL_DEFAULTS.waitActiveMs,
      maxPerRound: opts.maxPerRound != null ? opts.maxPerRound : PATROL_DEFAULTS.maxPerRound,
      scrollLoadMs: opts.scrollLoadMs || PATROL_DEFAULTS.scrollLoadMs,
      maxPasses: opts.maxPasses || PATROL_DEFAULTS.maxPasses,
      visitAllWhenNoUnread: !!opts.visitAllWhenNoUnread,
      switchMinMs: opts.switchMinMs || PATROL_DEFAULTS.switchMinMs,
      switchMaxMs: opts.switchMaxMs || PATROL_DEFAULTS.switchMaxMs,
      // 批25：等"可切会话窗口"的上限与轮询节拍（下发在途 / linger 优先）
      switchBlockWaitMs: opts.switchBlockWaitMs || PATROL_DEFAULTS.switchBlockWaitMs,
      switchBlockPollMs: opts.switchBlockPollMs || PATROL_DEFAULTS.switchBlockPollMs,
    };
    this._savePatrolInterval(intervalMs);
    if (this._patrolTimer) clearTimeout(this._patrolTimer);
    this.log.debug(`巡检已启动：每 ${intervalMs}ms 一轮`);
    this._scheduleNextPatrol(0);
    return { ok: true, intervalMs };
  }

  stopPatrol() {
    this._stopPatrol();
    this.log.debug('巡检已停止');
    return { ok: true };
  }

  _stopPatrol() {
    if (this._patrolTimer) { clearTimeout(this._patrolTimer); this._patrolTimer = null; }
    this._patrolOpts = null;
  }

  isPatrolling() { return !!this._patrolOpts; }

  patrolStatus() {
    return {
      running: !!this._patrolOpts,
      intervalMs: this._patrolOpts ? this._patrolOpts.intervalMs : 0,
      inRound: this._patrolling,
      ...this._patrolStats,
      // 批25：让路计数（下发在途）。不暴露这个，"巡检怎么不走了"线上无从诊断。
      yielded: { sendInFlight: 0, ...(this._patrolYield || {}) },
      sendBusy: this.isSendBusy(),
    };
  }

  _scheduleNextPatrol(delay) {
    if (!this._patrolOpts) return;
    this._patrolTimer = setTimeout(() => {
      if (this._syncingAll) { this._scheduleNextPatrol(this._patrolOpts.intervalMs); return; }
      this.patrol(this._patrolOpts)
        .catch((e) => this.log.warn('巡检轮异常', e && e.message))
        .finally(() => {
          if (this._patrolOpts) this._scheduleNextPatrol(this._patrolOpts.intervalMs);
        });
    }, Math.max(0, delay));
  }

  async patrol({
    throttleMs = PATROL_DEFAULTS.throttleMs,
    waitActiveMs = PATROL_DEFAULTS.waitActiveMs,
    maxPerRound = PATROL_DEFAULTS.maxPerRound,
    scrollLoadMs = PATROL_DEFAULTS.scrollLoadMs,
    maxPasses = PATROL_DEFAULTS.maxPasses,
    visitAllWhenNoUnread = false,
    scanOnly = false,
    switchMinMs = PATROL_DEFAULTS.switchMinMs,
    switchMaxMs = PATROL_DEFAULTS.switchMaxMs,
    switchBlockWaitMs = PATROL_DEFAULTS.switchBlockWaitMs,
    switchBlockPollMs = PATROL_DEFAULTS.switchBlockPollMs,
  } = {}) {
    if (this._patrolling) { this.log.warn('巡检已在进行，忽略重复触发'); return { skipped: true, reason: 'in-progress' }; }
    // P1-P6：账号暂停 → 立即返回，不巡检
    if (!this.isAccountEnabled()) return { skipped: true, reason: 'account-paused' };
    const hook = this.hooks.getConversationList;
    if (typeof hook !== 'function') { return { skipped: true, reason: 'no-hook' }; }
    this._patrolling = true;
    const startedAt = Date.now();
    let visited = 0, withNew = 0, captured = 0, failures = 0, scannedTotal, unreadCount;
    // M-5：本轮调度模式——sequential（现行顺序遍历）/ unread-first（未读优先）/
    //   unread-first-fallback（未读优先开启但信息不可靠，fail-open 回顺序遍历）。
    let scheduleMode = 'sequential';
    let yieldReason = null;   // 批25：本轮因"下发在途（含 linger）"让路的判据（null=没让路）
    let yieldSkipped = 0;

    // 记住巡检前活动会话，巡检结束后尽量回到它（减少打扰）
    const beforeConv = this.getConversationId();

    try {
      // ============ 阶段 1：扫描左侧列表（轻量级）============
      //   读取会话列表并统计未读数。未读徽章只在「未读优先」开关开启的渠道上参与
      //   调度（开关关 = 完全现行行为：顺序遍历，仅顺手统计未读数供可观测）。
      //   采集面不变：靠 _collectUnseenText 的 seenNodes/seen 去重，只有真正新增的
      //   消息才上行 inbound 触发 AI（"合理评论" = 不重复触发已处理消息）。
      const allList = [];
      try {
        const list = hook() || [];
        if (Array.isArray(list)) allList.push(...list);
      } catch (e) { this.log.warn('巡检阶段1读取列表失败', e && e.message); }
      // 去重
      const seenIds = new Set();
      const uniqList = allList.filter((c) => c && c.id && !seenIds.has(c.id) && seenIds.add(c.id));
      scannedTotal = uniqList.length;
      unreadCount = this._countUnread(uniqList);

      if (scanOnly) {
        this.log.debug(`巡检扫描-only 完成：总 ${scannedTotal}，未读 ${unreadCount}`);
        return { scannedTotal, unreadCount, scheduleMode, visited: 0, withNew: 0, captured: 0, failures: 0, durationMs: Date.now() - startedAt };
      }

      // ============ 阶段 1.5：本轮访问对象选择（M-5 未读优先调度）============
      //   顺序模式（开关关）：targets = 全量列表 —— 现行行为逐字节保留。
      //   未读优先模式（开关开）：只访问带未读徽章的会话；未读为 0 的会话靠活动会话
      //     MutationObserver/fallbackTimer + rescan 兜底，不再进全量遍历。
      //   fail-open：渠道列表没带 unread 布尔字段（未接线/异常）或分区抛错 ⇒ 本轮
      //     回退顺序遍历（宁可多访，不能漏访）。
      let targets = uniqList;
      if (this._patrolUnreadFirstEnabled()) {
        try {
          if (uniqList.length === 0) {
            scheduleMode = 'unread-first';
            targets = [];
          } else {
            const unreadWired = uniqList.every((c) => c && typeof c.unread === 'boolean');
            if (unreadWired) {
              scheduleMode = 'unread-first';
              if (unreadCount > 0) {
                targets = uniqList.filter((c) => c.unread);
              } else if (visitAllWhenNoUnread) {
                targets = uniqList;   // 兼容选项：无未读时仍全量访问（靠 seen 去重只报新消息）
              } else {
                targets = [];
              }
            } else {
              throttledWarn(this.log, `patrolUnreadFallback:${this.channel}`, WARN_THROTTLE_MS,
                '未读优先开启但会话列表未带 unread 布尔字段（渠道未接线或 detectUnread 异常），本轮回退顺序遍历');
              scheduleMode = 'unread-first-fallback';
            }
          }
        } catch (e) {
          targets = uniqList;
          scheduleMode = 'unread-first-fallback';
          this.log.warn('未读优先调度异常，本轮回退顺序遍历', e && e.message);
        }
      }
      if (maxPerRound > 0 && targets.length > maxPerRound) targets = targets.slice(0, maxPerRound);

      // ============ 阶段 2：分批点开未读会话（限并发）============
      // MAX_VISIT_PARALLEL=2：同一时刻最多 2 个 _patrolVisit 跑（控制 DOM mutation 并发）
      const MAX_VISIT_PARALLEL = 2;
      const semaphore = new Array(MAX_VISIT_PARALLEL).fill(Promise.resolve());
      const nextSlot = () => {
        const p = semaphore.shift();
        semaphore.push(p);
        return p;
      };

      for (const conv of targets) {
        if (this.getConversationId() === conv.id) {
          this.log.debug(`巡检跳过：用户已停留在会话 ${conv.id}（依赖 observer 实时捕获）`);
          continue;
        }
        // 批25：切会话之前先等一个「没有下发在途（含 linger 尾巴）」的窗口。
        //   让路的粒度是"每次切换"而不是"整轮"：下发完一条到发下一条之间除了 linger 还可能有
        //   限速重试/网络等待，巡检照样能推进，不会因为下行密集而整轮饿死；真等不到窗口
        //   （下发一直占着）就让本轮作废，下个轮询周期再来。
        const blocker = await this._waitForSwitchWindow(switchBlockWaitMs, switchBlockPollMs);
        if (blocker) {
          yieldReason = blocker;
          yieldSkipped = targets.length - targets.indexOf(conv);
          this._noteYield(blocker);
          this.log.info(`巡检让路：${blocker} ⇒ 本轮剩余 ${yieldSkipped} 个会话推迟到下一轮`);
          break;
        }
        try {
          // 限并发：每个 _patrolVisit 占一个 semaphore 槽位
          const prev = nextSlot();
          const visitPromise = prev.then(() => this._patrolVisit(conv, { throttleMs, waitActiveMs }));
          semaphore[semaphore.length - 1] = visitPromise.catch((e) => {
            this.log.warn('巡检单个会话异常', conv && conv.id, e && e.message);
            failures++;
          });
          const r = await visitPromise;
          visited++;
          if (r && r.ok && r.newCount > 0) { withNew++; captured += r.newCount; }
          await sleep(randInt(switchMinMs, switchMaxMs));
        } catch (e) {
          this.log.warn('巡检单个会话异常', conv && conv.id, e && e.message);
          failures++;
        }
        if (this._patrolOpts && maxPerRound > 0 && visited >= maxPerRound) break;
      }
    } finally {
      this._patrolling = false;
    }
    if (beforeConv && beforeConv !== this.getConversationId()) {
      // 批25：收尾"切回原会话"也是一次换会话，同样走 preemptible（下发在途/linger 内放弃回切）。
      try { await this.openConversation(beforeConv, { backfill: false, waitActiveMs: 2000 }); } catch (_) {  }
    }
    const dur = Date.now() - startedAt;
    this._lastPatrolAt = startedAt;
    this._patrolStats.rounds++;
    this._patrolStats.visited += visited;
    this._patrolStats.withNew += withNew;
    this._patrolStats.captured += captured;
    this._patrolStats.failures += failures;
    this._patrolStats.lastRoundAt = startedAt;
    this._patrolStats.lastDurationMs = dur;
    // M-5：调度模式与未读数进统计面（patrolStatus 展开 _patrolStats 即可见）。
    //   运维据此回答「未读检测是不是全瞎了」：unread-first 模式下 lastUnreadCount
    //   长期为 0 而实际有新消息 ⇒ 该渠道徽章选择器失效，应把渠道移出未读优先清单。
    this._patrolStats.lastScheduleMode = scheduleMode;
    this._patrolStats.lastUnreadCount = unreadCount;
    this.log.debug(`巡检一轮完成：扫描 ${scannedTotal} 个会话（未读 ${unreadCount}，调度 ${scheduleMode}），访问 ${visited}，有新消息 ${withNew}，捕获新消息 ${captured}，失败 ${failures}，用时 ${dur}ms`);
    if (yieldReason) {
      this.log.info(`巡检本轮让路 ${yieldSkipped} 个会话（${yieldReason}）：下发优先，下一轮再访`);
    }
    return {
      scannedTotal, unreadCount, scheduleMode, visited, withNew, captured, failures, durationMs: dur,
      yielded: yieldReason, skippedByYield: yieldSkipped,
    };
  }

  // M-5：未读数统计（只读，不影响调度）。渠道列表带 unread 布尔字段才计数，
  //   否则返回 0（与现行口径一致：不产生未读信息时不再编造）。
  _countUnread(list) {
    try {
      if (!Array.isArray(list) || list.length === 0) return 0;
      if (!list.every((c) => c && typeof c.unread === 'boolean')) return 0;
      return list.reduce((n, c) => n + (c.unread ? 1 : 0), 0);
    } catch (_) { return 0; }
  }

  async _patrolVisit(conv, { throttleMs, waitActiveMs }) {
    const opened = await this.openConversation(conv.id, { backfill: false, waitActiveMs });
    if (!opened) return { ok: false, newCount: 0 };
    const renderWaitMs = Math.min(throttleMs, 600);
    await sleep(renderWaitMs);
    // 批25（对称方向）：这个 sleep 窗口里下发可能已经切走了会话。_collectUnseenText 是同步的
    //   （内部无 await），一旦跑起来就不会被打断；但若在切走之后才采集，batch 就是**另一个
    //   会话**的消息，却会被 _emitPatrolMessage 挂在 opened 名下上行 —— 串台：AI 拿 B 的话
    //   当 A 的上下文来回复。所以采集之前必须复核"还停在 opened"。
    const cur = this.getConversationId();
    if (cur && cur !== opened) {
      this.log.debug(`巡检采集跳过：会话已漂移 ${opened} -> ${cur}`);
      return { ok: false, newCount: 0, drifted: true };
    }
    const batch = this._collectUnseenText();
    if (batch.length) this._emitPatrolMessage(opened, conv, batch);
    return { ok: true, newCount: batch.length };
  }

  // P1-3：首次巡检判定 —— 从未巡检过，或距上次巡检超过 firstRunWindowMs（popup 关闭重开场景）。
  _isFirstPatrolRun() {
    const now = Date.now();
    return typeof this._lastPatrolAt !== 'number' || (now - this._lastPatrolAt) > PATROL_DEFAULTS.firstRunWindowMs;
  }

  _collectUnseenText() {
    this._refreshConvAnchor();
    const batch = [];
    // P1-3：首次巡检单次抓取上限 firstRunMaxBatch（20，防首次涌入），
    // 常规巡检 maxBatchPerPatrol（80）。剩余一律靠下轮扫描补齐。
    const firstRun = this._isFirstPatrolRun();
    const MAX_BATCH = firstRun ? PATROL_DEFAULTS.firstRunMaxBatch : PATROL_DEFAULTS.maxBatchPerPatrol;
    const cid = this.getConversationId() || this.conversationId || '';
    const occ = new Map();
    let items;
    try { items = this.getMessageItems() || []; } catch (_) { items = []; }
    for (const item of items) {
      if (batch.length >= MAX_BATCH) {
        if (firstRun) this.log.warn(`[首次] 巡检抓取消息数达上限 ${MAX_BATCH}，剩余靠下轮扫描补齐`);
        else this.log.warn(`[常规] 巡检抓取消息数达上限 ${MAX_BATCH}，剩余靠下轮扫描补齐`);
        break;
      }
      if (!item) continue;
      let parsed;
      try { parsed = this.parseMessageItem(item); } catch (_) { continue; }
      if (!parsed) continue;
      // 会话归属校验：虚拟列表切换后 getMessageItems(root=document) 可能返回
      // 上一会话的残留 DOM 节点。检查 item 是否仍在活动消息容器内。
      const root = this.getMessageRoot();
      if (root && item && !root.contains(item)) { this.seenNodes.add(item); continue; }
      // 会话锚点校验：与 getMessages 同口径（锚点过期/sender 错配 ⇒ 残留，标记 seen 不再上报）。
      if (this._isConvAnchorStale(cid) || !this._senderMatchesAnchor(parsed)) { this.seenNodes.add(item); continue; }
      // 002-multi-type-messages：媒体消息不再丢弃（原先这里直接 continue 是客户图片
      // 上行蒸发的根因）。text 为空且无媒体 URL 的仍跳过。
      if (!parsed.text && !this._isUplodableMedia(parsed)) { this.seenNodes.add(item); continue; }
      // §8.3-17：同内容气泡的发生次数只由「本轮可见消息的顺序」决定，所以 seenNodes 的跳过
      // 必须排在计数之后 —— 否则前面那条被跳过时，后面这条会被编成 0、进而被稳定键吞掉。
      const occurrence = this._bumpOccurrence(occ, cid, parsed);
      if (this.seenNodes.has(item)) continue;
      this.seenNodes.add(item);
      // 稳定键去重：巡检重访同一会话时，已上报过的消息按 (会话|发送者|文本|发生次数) 拦截
      const key = this._dedupKey(cid, parsed, occurrence);
      if (this._hasSent(key)) {
        this.log.debug('巡检稳定键去重跳过:', key.slice(0, 56));
        continue;
      }
      parsed.message_id = this._canonicalMsgId(item, cid, parsed, occurrence);
      this._markSent(key);
      batch.push(parsed);
    }
    return batch;
  }

  _emitPatrolMessage(cid, conv, batch) {
    const history = batch.map((p) =>
      this._historyItem(p, p.sender_type === SENDER.CUSTOMER ? DIRECTION.INBOUND : DIRECTION.OUTBOUND)
    );
    const last = batch[batch.length - 1];
    const groupItem = [...batch].reverse().find((p) => p.is_group);
    const isGroup = !!groupItem;
    const groupId = (groupItem && groupItem.group_id) || '';
    const groupName = (groupItem && groupItem.group_name) || '';
    const senderName = (conv && conv.name) || batch.find((p) => p.sender_name)?.sender_name || '';
    // M-2 客户头像链路：巡检帧也带上客户头像（最后一条有头像的消息优先，1v1 各轮同源）。
    const senderAvatar = last.sender_avatar || batch.find((p) => p.sender_avatar)?.sender_avatar || '';
    // sender_type 取最后一条消息的 sender_type（可能为 system）；event_id 取其 message_id（稳定）。
    const lastSenderType = last.sender_type || SENDER.CUSTOMER;
    const msg = makeUnifiedMessage({
      channel: this.channel,
      account_id: this.getAccountId(),
      conversation_id: cid,
      sender_type: lastSenderType,
      sender_id: isGroup && groupId ? groupId : (last.sender_id || cid),
      sender_name: senderName,
      sender_avatar: senderAvatar,
      // 002-multi-type-messages：顶层 content/msg_type 按最后一条真实类型输出，
      // 媒体消息 text 为空时补占位符（原先写死 'text'，媒体帧到服务端类型失真）。
      content: last.text || mediaPlaceholder(last.msg_type),
      msg_type: last.msg_type || 'text',
      media_url: last.media_url || '',
      timestamp: last.timestamp || Date.now(),
      event_id: last.message_id,
      is_group: isGroup,
      group_id: groupId,
      group_name: groupName,
      // M-6：批次内任一条带 new_user_unfollowed 标记即随巡检帧透传。
      extra: collectBatchExtra(batch),
      history,
    });
    this.log.info(`巡检上行消息: 会话 ${cid}（${history.length} 轮新消息）`);
    if (this.callbacks.onMessage) this.callbacks.onMessage(msg);
  }

  _ingest(parsed) {
    if (!parsed) return;
    // 002-multi-type-messages：媒体消息放行（占位符补齐在上行构造时做）。
    if (!parsed.text && !this._isUplodableMedia(parsed)) return;
    this._pushWindow(parsed);
    this._emitMessage(parsed);
  }

  _pushWindow(parsed) {
    const cid = this.getConversationId() || this.conversationId || '_';
    if (!parsed) return;
    let w = this._convWindow.get(cid);
    if (!w) { w = []; this._convWindow.set(cid, w); }
    w.push(parsed);
    if (w.length > HISTORY_CONTEXT_WINDOW) w.splice(0, w.length - HISTORY_CONTEXT_WINDOW);
    if (this._convWindow.size > 200) {
      const keys = Array.from(this._convWindow.keys()).slice(0, 100);
      for (const k of keys) this._convWindow.delete(k);
    }
  }

  _windowFor(cid) {
    const w = this._convWindow.get(cid || this.getConversationId() || this.conversationId || '_');
    return w ? w.slice(-HISTORY_CONTEXT_WINDOW) : [];
  }

  _handleIncremental(item) {
    if (item && this.seenNodes.has(item)) return;
    if (!this.getConversationId()) return;
    // 批24b：会话归属校验与 getMessages/巡检/回查共用同一条判据 —— 不在活动消息容器里的节点
    // 是上一会话的残留。增量这条（_scanIncremental 每 3s 全量重扫）此前没判，会把别的会话
    // 私信按当前会话 id 上行。不 seenNodes.add：这个节点将来若真的回到活动容器仍可上报。
    const root = this.getMessageRoot();
    if (root && item && !root.contains(item)) return;
    // 会话锚点校验：锚点过期（中间态）或 sender 与锚点昵称错配 ⇒ 判跨会话残留拒收。
    // 不 seenNodes.add：锚点刷新后若确认归属仍可上报。
    if (this._isConvAnchorStale(this.getConversationId()) || !this._senderMatchesAnchor(this.parseMessageItem(item))) return;
    const parsed = this.parseMessageItem(item);
    if (!parsed) return;
    if (item) this.seenNodes.add(item);
    // 稳定键去重（跨 DOM 重渲染）：平台虚拟列表重渲染会换节点 → seenNodes 失效，
    // 但同一逻辑消息的文本+发送者+会话稳定 → 用稳定键拦截重复上行（避免 429 死循环/无效 POST）。
    const cid = this.getConversationId();
    const occurrence = this._occurrenceInList(item, cid, parsed);
    const key = this._dedupKey(cid, parsed, occurrence);
    if (this._hasSent(key)) {
      this.log.debug('稳定键去重跳过增量:', key.slice(0, 56));
      return;
    }
    parsed.message_id = this._canonicalMsgId(item, cid, parsed, occurrence);
    this._markSent(key);
    this._ingest(parsed);
  }

  _backfill(root) {
    this._refreshConvAnchor();
    if (!this.getConversationId()) {
      this.log.info('回填跳过：当前无活动会话（私信列表视图）');
      return;
    }
    const batch = []; 
    const occ = new Map();
    const items = this.getMessageItems();
    const activeRoot = root || this.getMessageRoot();
    const cid = this.getConversationId();
    for (const item of items) {
      const parsed = this.parseMessageItem(item);
      if (!parsed) continue;
      // 002-multi-type-messages：媒体消息参与回填历史（text 为空时占位符在上行构造时补）。
      if (!parsed.text && !this._isUplodableMedia(parsed)) continue;
      // 批24b：回填与巡检/扫描/回查共用这一条会话归属判据。虚拟列表切换后残留节点仍在
      // 文档里，不判归属就会把上一会话的私信并进**当前**会话的历史帧上行（串台）。
      if (activeRoot && item && !activeRoot.contains(item)) { this.seenNodes.add(item); continue; }
      // 会话锚点校验：与巡检/增量同口径（锚点过期/sender 错配 ⇒ 残留）。
      if (this._isConvAnchorStale(cid) || !this._senderMatchesAnchor(parsed)) { this.seenNodes.add(item); continue; }
      // §8.3-17：先计数再判 seenNodes，理由同 _collectUnseenText。
      const occurrence = this._bumpOccurrence(occ, cid, parsed);
      if (this.seenNodes.has(item)) continue;
      this.seenNodes.add(item);
      // 稳定键去重（跨 DOM 重渲染）：端口断开重连 / 虚拟列表重渲染换节点 seenNodes 失效后，
      // 平台重发的同一条历史消息按 (会话|发送者|文本|发生次数) 拦截，避免重复上行。
      const key = this._dedupKey(cid, parsed, occurrence);
      if (this._hasSent(key)) {
        this.log.debug('稳定键去重跳过历史:', key.slice(0, 56));
        continue;
      }
      parsed.message_id = this._canonicalMsgId(item, cid, parsed, occurrence);
      this._markSent(key);
      batch.push({ parsed, direction: parsed.sender_type === SENDER.CUSTOMER ? DIRECTION.INBOUND : DIRECTION.OUTBOUND });
    }
    if (batch.length) {
      const now = Date.now();
      const shouldLog = !this._lastBackfillLogAt || (now - this._lastBackfillLogAt > 5000);
      if (shouldLog) {
        this.log.info(`回填会话 ${this.conversationId}: ${items.length} 条消息（新增 ${batch.length}）`);
        this._lastBackfillLogAt = now;
      } else {
        this.log.debug(`回填会话 ${this.conversationId}: ${items.length} 条消息（新增 ${batch.length}）`);
      }
      this._emitConversationHistory(batch);
    } else {
      this.log.debug(`回填会话 ${this.conversationId}: ${items.length} 条消息（新增 0，全部已去重）`);
    }
  }

  _historyItem(parsed, direction) {
    const cid = this.getConversationId() || this.conversationId || '';
    const st = parsed.sender_type || SENDER.CUSTOMER;
    // 与 makeUnifiedMessage 一致的 sender_id 解析：群聊→群 id；客户→会话 id；自己/AI→账号 id。
    // 缺失会落成 channel:unknown，破坏统一收信中心按「对方」聚合。
    const senderId =
      parsed.sender_id ||
      (parsed.is_group && parsed.group_id ? parsed.group_id : (st === SENDER.CUSTOMER ? cid : this.getAccountId()));
    return {
      event_id: parsed.message_id || '',
      sender_type: st,
      sender_id: senderId,
      sender_name: parsed.sender_name || '',
      // M-2 客户头像链路：历史轮也带发送者头像（服务端 HistoryItem.SenderAvatar 落库）。
      sender_avatar: parsed.sender_avatar || '',
      // 002-multi-type-messages：媒体消息无文字时补占位符，content 永不为空。
      content: parsed.text || mediaPlaceholder(parsed.msg_type),
      media_url: parsed.media_url || '',
      msg_type: parsed.msg_type || 'text',
      timestamp: parsed.timestamp || Date.now(),
      direction,
      is_group: !!parsed.is_group,
      group_id: parsed.group_id || '',
      group_name: parsed.group_name || '',
      receiver_id: direction === DIRECTION.OUTBOUND ? cid : (parsed.receiver_id || ''),
    };
  }

  _emitConversationHistory(batch) {
    if (!batch || !batch.length) return;
    const last = batch[batch.length - 1];
    const cid = this.getConversationId() || this.conversationId || '_';
    // 会话级群聊元数据：取最近一条带群信息的历史项
    const groupItem = [...batch].reverse().find((b) => b.parsed && b.parsed.is_group);
    const isGroup = !!groupItem;
    const groupId = (groupItem && groupItem.parsed.group_id) || '';
    const groupName = (groupItem && groupItem.parsed.group_name) || '';
    const history = batch.map(({ parsed, direction }) => this._historyItem(parsed, direction));
    // 摘要方向：有客户消息 → inbound（主导），否则 outbound
    const summaryDirection = batch.some((b) => b.direction === DIRECTION.INBOUND) ? DIRECTION.INBOUND : DIRECTION.OUTBOUND;
    // 摘要方向：sender_type 取最后一条历史项的 sender_type（可能为 system）；event_id 用其 message_id（稳定）。
    const lastSenderType = last.parsed.sender_type || SENDER.CUSTOMER;
    const msg = makeUnifiedMessage({
      channel: this.channel,
      account_id: this.getAccountId(),
      conversation_id: cid,
      sender_type: lastSenderType,
      sender_id: isGroup && groupId ? groupId : (last.parsed.sender_id || cid),
      content: last.parsed.text || '',
      media_url: last.parsed.media_url || '',
      msg_type: last.parsed.msg_type || 'text',
      timestamp: last.parsed.timestamp,
      event_id: last.parsed.message_id,
      direction: summaryDirection,
      is_group: isGroup,
      group_id: groupId,
      group_name: groupName,
      // M-6：批次内任一条带 new_user_unfollowed 标记即随会话级帧透传。
      extra: collectBatchExtra(batch),
      history,
    });
    this.log.debug(`回填会话 ${cid}: 1 条会话级消息（${history.length} 轮历史, ${summaryDirection}）`);
    if (this.callbacks.onMessage) this.callbacks.onMessage(msg);
  }

  _emitMessage(parsed, extra) {
    const cid = this.getConversationId();
    const windowMsgs = this._windowFor(cid);
    const msg = makeUnifiedMessage({
      channel: this.channel,
      account_id: this.getAccountId(),
      conversation_id: cid,
      sender_type: parsed.sender_type,
      sender_id: parsed.is_group && parsed.group_id ? parsed.group_id : (parsed.sender_id || ''),
      // 002-multi-type-messages：媒体消息无文字时补占位符（与服务端直连渠道同词表）。
      content: parsed.text || mediaPlaceholder(parsed.msg_type),
      media_url: parsed.media_url || '',
      msg_type: parsed.msg_type || 'text',
      timestamp: parsed.timestamp,
      event_id: parsed.message_id,
      is_group: !!parsed.is_group,
      group_id: parsed.group_id || '',
      group_name: parsed.group_name || '',
      sender_name: parsed.sender_name || '',
      // M-2 客户头像链路：实时帧带客户头像（parsed.sender_avatar，1v1 为「对方」头像）。
      sender_avatar: parsed.sender_avatar || '',
      raw: parsed.raw || null,
      // M-6：parsed.extra（渠道 parseMessageItem 打的 new_user_unfollowed 标记等）
      // 随帧透传；调用方显式 extra（如批24 send_verified 结论位）优先，同键不覆盖。
      extra: mergeParsedExtra(parsed, extra),
      history: windowMsgs.map((m) => this._historyItem(m, m.sender_type === SENDER.CUSTOMER ? DIRECTION.INBOUND : DIRECTION.OUTBOUND)),
    });
    this.log.info('上行消息:', (msg.content || `[${msg.msg_type}]`).slice(0, 40));
    if (this.callbacks.onMessage) this.callbacks.onMessage(msg);
  }

  // 批25：整次下发 = 一个临界区（openConversation → 稳定等待 → 键入 → 点击 → 回查）。
  //   持锁期间巡检不得换会话；真正危险的不是 openConversation 那一刻，而是后面几秒的键入期
  //   —— 所以锁必须覆盖整段，而不是包住"切会话"这一瞬。
  async sendOutbound(text, targetConvId, opts = {}) {
    // 002-multi-type-messages：非文本出站（opts.msgType + opts.mediaUrl）。
    // 媒体消息的「正文」是占位符或空，`!text` 不能再作为唯一闸门——
    // 非 text 且有 mediaUrl 时走媒体路径（降级或渠道原生）。
    const outType = (opts.msgType || '').toLowerCase();
    if (!text && !(isMediaMsgType(outType) && opts.mediaUrl)) {
      this.log.warn('回复内容为空，跳过');
      return { ok: false, rateLimited: false, notFound: false };
    }
    this.beginOutbound();
    try {
      return await this._sendOutboundLocked(text, targetConvId, opts);
    } finally {
      this.endOutbound();
    }
  }

  async _sendOutboundLocked(text, targetConvId, opts = {}) {
    const account = this.getAccountId();
    let conv = this.getConversationId();
    if (targetConvId && conv !== targetConvId) {
      // exact=true：只接受精确切到目标会话，杜绝"切到别处也当成功"→ 误发到其他会话
      // preemptible:false：这次切换是下发的一部分，绝不因巡检让路（让路＝这条永远发不出去）
      const opened = await this.openConversation(targetConvId, { backfill: false, exact: true, preemptible: false });
      if (!opened) {
        throttledWarn(this.log, `sendNotOpen:${targetConvId}`, WARN_THROTTLE_MS, `下行目标会话 ${targetConvId} 未打开，放弃发送`, { current: conv });
        return { ok: false, rateLimited: false, notFound: true };
      }
      conv = opened;
      // SPA DOM 渲染稳定等待：openConversation 确认 URL/活动会话已切换，
      // 但 React 异步渲染输入框可能滞后——等待 getConversationId 在 3 轮(约600ms)内不变，
      // 确保输入框 DOM 属于目标会话而非上一个会话的残留节点。
      let stableCount = 0;
      let lastCid = null;
      const stableDeadline = Date.now() + 2000;
      while (Date.now() < stableDeadline) {
        const c = this.getConversationId();
        if (c === lastCid) {
          if (++stableCount >= 3) break;
        } else {
          stableCount = 0;
          lastCid = c;
        }
        await sleep(200);
      }
      // 二次校验（防 DOM 竞争导致会话漂移）：确认仍在 openConversation 打开的会话，否则拒绝发送
      const after = this.getConversationId();
      if (after && after !== conv) {
        throttledWarn(this.log, `sendConvDrift:${targetConvId}`, WARN_THROTTLE_MS,
          `下行会话漂移: 期望 ${conv} 实际 ${after}，放弃发送防误发`, { targetConvId, conv, after });
        return { ok: false, rateLimited: false, notFound: true };
      }
    }
    const decision = this.rateLimiter.tryAcquire(this.channel, account, conv, text);
    if (!decision.allowed) {
      throttledWarn(this.log, `ratelimit:${decision.reason}`, WARN_THROTTLE_MS, `下行被风控拦截: ${decision.reason}`);
      if (this.callbacks.onRateLimited) this.callbacks.onRateLimited(decision);
      return { ok: false, rateLimited: true, notFound: false };
    }
    if (decision.waitHintMs > 0) await sleep(decision.waitHintMs);
    // B6（批3）：跨 tab/跨渠道全局最小间隔闸（storage 共享时钟；不可用时 0ms 降级）
    const gateWaitMs = await globalSendWaitMs(this.rateLimiter.cfg.minIntervalMs);
    if (gateWaitMs > 0) await sleep(gateWaitMs);
    // 批24（§8.3-8）：回查基线必须在**点击前一刻**取。放在这里而不是方法开头，是因为上面
    // 两段等待（风控提示 + 全局最小间隔）能睡到秒级，其间客户自己也可能说过同一句话——
    // 用睡前的基线会把别人的那条气泡算成我们发出去的。
    const verifyMs = opts.sendVerifyMs != null ? opts.sendVerifyMs : BRIDGE_THREE_CHANNEL.sendVerifyMs;
    // 批25：最后一道闸 —— 点发送前复核会话归属。
    //   锁（_outboundInFlight）能挡住巡检，但挡不住 SPA 自己改路由和用户手点别的会话；
    //   一旦漂移，"点之前"就是唯一止损点（点下去＝发给了下一个用户，不可撤回）。
    //   判据挂在渠道 sendText / sendMedia 的点击前一刻（渠道 hook 调 assertConvOrAbort），
    //   漂移 ⇒ 清空输入框 + 抛 conv-drift ⇒ 本条不 ack、留 pending 由下一轮重投。
    //   （M-3：定义提前到媒体分派之前 —— sendMedia 原生发送同样要过这道闸。）
    const assertConv = () => {
      const now = this.getConversationId();
      return !now || now === conv;
    };
    // 002-multi-type-messages：媒体出站分派。
    //   - opts.msgType 非 text 且有 mediaUrl：优先渠道原生 hooks.sendMedia（若实现）；
    //     未实现则统一降级为「[图片] URL」文本走 rawSendText —— 降级也是一次真实投递，
    //     成功即 ack，行不再无限重推（原先非文本出站在扩展端静默失败、行反复重推）。
    //   - 降级文本参与同一套限速/回查/自报管线，行为与纯文本完全一致。
    //   - M-3（2026-11-11）：降级可观测 —— via 标记区分 sendMedia 原生成功 /
    //     text-fallback:no-hook（渠道未实现）/ text-fallback:sendmedia-failed（hook 返回
    //     未成功）/ text-fallback:sendmedia-error（hook 抛错含超时），随结果上行日志可见。
    //   - M-3：assertConv / abortToken 随 opts 透传给 hook（原生发送同样过会话复核闸；
    //     超时弃用的 hook 在点发送前自检 abortToken 退出，防双发）。
    const outType = (opts.msgType || '').toLowerCase();
    const isMediaOut = isMediaMsgType(outType) && !!opts.mediaUrl;
    let mediaVia = '';
    let sendText = text;
    let sendMetaType = 'text';
    let sendMetaUrl = '';
    if (isMediaOut) {
      if (typeof this.hooks.sendMedia === 'function') {
        const mediaAbort = { aborted: false, reason: '' };
        const prevMediaAbort = this._activeMediaAbort;
        this._activeMediaAbort = mediaAbort;
        let mres = null;
        let mediaErr = null;
        try {
          mres = await withTimeout(
            this.hooks.sendMedia(outType, opts.mediaUrl, conv, { text, assertConv, abortToken: mediaAbort }),
            BRIDGE_THREE_CHANNEL.sendOutboundTimeoutMs,
            `sendMedia(${this.channel})`
          );
        } catch (e) {
          mediaErr = e;
        } finally {
          this._activeMediaAbort = prevMediaAbort;
        }
        if (mres && mres.ok) {
          this.rateLimiter.markSent(this.channel, account, conv, opts.mediaUrl);
          stampGlobalSendAt();
          this._emitMessage(
            { sender_type: SENDER.AGENT, text: text || mediaPlaceholder(outType), media_url: opts.mediaUrl, msg_type: outType, timestamp: Date.now(), message_id: typedContentHash(this.channel, conv, outType, opts.mediaUrl, text || '') },
            { send_verified: true }
          );
          return { ok: true, sendVerified: true, rateLimited: false, notFound: false, via: 'sendMedia', mediaMethod: (mres && mres.method) || '' };
        }
        // 降级：先弃用在途 hook（若已超时仍在跑，点发送前自检会退出），再走文本路径
        mediaAbort.aborted = true;
        mediaAbort.reason = (mres && mres.reason) || 'failed';
        if (mediaErr) {
          mediaVia = 'text-fallback:sendmedia-error';
          this.log.warn('sendMedia 异常，降级为 URL 文本投递', { channel: this.channel, type: outType, via: mediaVia, err: String(mediaErr?.message || mediaErr) });
        } else {
          mediaVia = 'text-fallback:sendmedia-failed';
          this.log.warn('sendMedia 未成功，降级为 URL 文本投递', { channel: this.channel, type: outType, via: mediaVia, reason: (mres && mres.reason) || '' });
        }
      } else {
        // M-3：渠道未实现 sendMedia —— 保留文本降级（不丢行），via 标记可观测；
        // 节流告警：同渠道 15s 一次，避免媒体下行密集时刷屏。
        mediaVia = 'text-fallback:no-hook';
        throttledWarn(this.log, `sendMediaNoHook:${this.channel}`, WARN_THROTTLE_MS,
          '渠道未实现 sendMedia，媒体下行降级为 URL 文本投递', { channel: this.channel, type: outType, via: mediaVia });
      }
      sendText = `${text ? text + ' ' : ''}${mediaPlaceholder(outType)} ${opts.mediaUrl}`;
      sendMetaType = outType;
      sendMetaUrl = opts.mediaUrl;
    }
    const textBaseline = this._countVisibleText(sendText);
    let ok;
    try {
      // B4（批3）：rawSendText 裸 await——sendText 卡死（DOM 阻塞/框架吞事件）会永久挂起
      // 整条按会话串行的下行队列。统一 withTimeout，预算随文案长度伸缩（B7 拟人键入时长）。
      await withTimeout(this.rawSendText(sendText, { expectConv: conv, assertConv }), humanSendTimeoutMs(sendText), `rawSendText(${this.channel})`);
      ok = true;
    } catch (e) {
      const drifted = !!(e && e.code === 'conv-drift');
      if (drifted) {
        this.log.warn('下发中止：点发送前检测到会话漂移，已清空输入框、不发送（不 ack，留 pending 重投）', {
          channel: this.channel, expect_conv: conv, actual_conv: this.getConversationId(),
        });
      } else {
        this.log.error('回写失败', String(e?.message || e));
      }
      return { ok: false, rateLimited: false, notFound: false, drifted };
    }
    let sendVerified = false;
    if (ok) {
      // 回查只产结论位：未见**不碰** ok、不碰 markSent、不碰 ack。让"没见着"变成重投，
      // 就是把一次不可撤销的双发交回给一段渲染滞后（与批20b 在 A 链路的取舍同一条）。
      sendVerified = await this._verifySendLanded(sendText, textBaseline, verifyMs);
      this.rateLimiter.markSent(this.channel, account, conv, sendText);
      stampGlobalSendAt(); // B6：不 await——storage 写失败已在内部吞掉，仅 best-effort 更新共享时钟
      // 自报出站：媒体降级路径按真实类型与 URL 自报，服务端回声去重按 typed 口径对齐。
      this._emitMessage(
        {
          sender_type: SENDER.AGENT,
          text: sendText,
          media_url: sendMetaUrl,
          msg_type: sendMetaType,
          timestamp: Date.now(),
          message_id: sendMetaUrl
            ? typedContentHash(this.channel, conv, sendMetaType, sendMetaUrl, text || '')
            : contentHash(this.channel, conv, sendText),
        },
        { send_verified: sendVerified }
      );
      if (!sendVerified) {
        this.log.warn('出站已提交但窗口内未见自己的气泡（仅记结论位，不重发）', {
          channel: this.channel, conv_id: conv, verify_ms: verifyMs,
        });
      }
    }
    // M-3：媒体出站带 via 标记（sendMedia / text-fallback:*）；纯文本不带（结果形状不变）
    return isMediaOut
      ? { ok, sendVerified, rateLimited: false, notFound: false, via: mediaVia }
      : { ok, sendVerified, rateLimited: false, notFound: false };
  }
}

