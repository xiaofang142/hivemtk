import { createLogger } from './logger.js';
import { RateLimiter, globalSendWaitMs, stampGlobalSendAt } from './rate-limiter.js';
import { makeUnifiedMessage, SENDER, DIRECTION, HISTORY_CONTEXT_WINDOW, PATROL_DEFAULTS, contentHash } from './types.js';
import { mergeSelectors } from './selector-ai.js';
import { simulateRealClick } from './dom.js';
import { humanSendTimeoutMs } from './constants.js';
import { withTimeout } from './downlink.js';

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

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
    this._patrolStats = { rounds: 0, visited: 0, withNew: 0, captured: 0, failures: 0, lastRoundAt: 0, lastDurationMs: 0 };
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

  _dedupKey(cid, parsed) {
    const sender = parsed.sender_id || parsed.sender_name || '';
    const text = parsed.text || '';
    return this._hash(`${cid}|${sender}|${text}`);
  }

  _canonicalMsgId(item, cid, parsed) {
    const type = parsed.msg_type || 'text';
    const hash = contentHash(this.channel, cid || '', parsed.text || '');
    if (type === 'system' || type === 'recall') return `${type}:${hash}`;
    return hash;
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
  getMessageItems() { return this.hooks.getMessageItems ? this.hooks.getMessageItems() : []; }
  parseMessageItem(item) {
    return this.hooks.parseMessageItem ? this.hooks.parseMessageItem(item) : null;
  }
  getConversationList() {
    return this.hooks.getConversationList ? this.hooks.getConversationList() : [];
  }
  rawSendText(text) { return this.hooks.sendText ? this.hooks.sendText(text) : Promise.resolve(false); }
  selfTest() { return this.hooks.selfTest ? this.hooks.selfTest() : []; }

  getMessages({ limit = 100 } = {}) {
    const out = [];
    let items;
    try { items = this.getMessageItems() || []; } catch (_) { items = []; }
    const cid = this.getConversationId() || this.conversationId || '';
    for (const item of items) {
      if (out.length >= limit) break;
      if (!item) continue;
      let parsed;
      try { parsed = this.parseMessageItem(item); } catch (_) { continue; }
      if (!parsed) continue;
      if (parsed.msg_type && parsed.msg_type !== 'text') continue;
      if (!parsed.text) continue;
      // 会话归属校验：getMessageItems(root=document) 可能返回虚拟列表残留的
      // 上一会话 DOM 节点。检查 item 是否仍在活动消息容器内（getMessageRoot()）。
      // 不在当前会话容器的节点 → 跨会话残留 → 丢弃。
      const root = this.getMessageRoot();
      if (root && item && !root.contains(item)) continue;
      // 稳定键去重（跨 DOM 重渲染）：与 _handleIncremental/_backfill/_collectUnseenText 共用
      // _sentKeys。PollingLoop 每秒巡检、每轮重抓全部可见消息，若不做去重，同一逻辑消息会被反复上行；
      // 规范 event_id（h:<hash>）让后端按稳定键幂等去重（即便 _sentKeys 过期也能兜底）。
      const key = this._dedupKey(cid, parsed);
      if (this._hasSent(key)) continue;
      parsed.message_id = this._canonicalMsgId(item, cid, parsed);
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
    }, 2000);
  }

  _attachConversation() {
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
    }, 8000);
  }

  _startRescan() {
    if (this._rescanTimer) clearInterval(this._rescanTimer);
    this._rescanTimer = setInterval(() => {
      this.syncAllConversations({ throttleMs: 1500 }).catch(() => {});
    }, 10 * 60 * 1000);
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

  async openConversation(cid, { waitActiveMs = 5000, backfill = false, exact = false } = {}) {
    if (!cid) return null;
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
    // 命中目标：优先精确 id；兼容 id 表述不一致（如 /user/ 链接 vs data-* / conv: 前缀）
    // 2026-08-05 修复：补全双向 conv: 前缀匹配。
    //   原版仅覆盖 cid 带 conv: 前缀（剥掉比对）和 c.id 不带但 cid 带（补 conv: 比对）两个方向，
    //   缺漏「c.id 带 conv: 前缀但 cid 不带」→ 巡检用 data-conv-id 找会话时找不到 → 巡检失效。
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
        target = list.find((c) => c && c.name && c.name === name)
              || list.find((c) => c && c.name && name && c.name.includes(name))
              || target;
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
    try {
      let pass = 0;
      const MAX_PASSES = 16;
      while (pass < MAX_PASSES) {
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
        // 本轮结束：滚动列表到底部，等待加载更多（虚拟/懒加载列表），继续下一轮
        // 2026-08-05 修复：两轮列表之间间隔从 500ms 提到 scrollLoadMs（默认 1500ms），
        //   旧值 500ms 太快，虚拟列表还没加载完就开下一轮 → 漏抓 + 平台风控
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
    return { synced: done, total: scannedTotal, failures };
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
  } = {}) {
    if (this._patrolling) { this.log.warn('巡检已在进行，忽略重复触发'); return { skipped: true, reason: 'in-progress' }; }
    // P1-P6：账号暂停 → 立即返回，不巡检
    if (!this.isAccountEnabled()) return { skipped: true, reason: 'account-paused' };
    const hook = this.hooks.getConversationList;
    if (typeof hook !== 'function') { return { skipped: true, reason: 'no-hook' }; }
    this._patrolling = true;
    const startedAt = Date.now();
    let visited = 0, withNew = 0, captured = 0, failures = 0, scannedTotal, unreadCount = 0;

    // 记住巡检前活动会话，巡检结束后尽量回到它（减少打扰）
    const beforeConv = this.getConversationId();

    try {
      // ============ 阶段 1：扫描左侧列表（轻量级）============
      // 2026-08-05 修复：巡检改为直接遍历所有会话，不再判断未读。
      //   原版仅访问有未读标记的会话（c.unread），但小红书未读徽章 DOM 不稳定
      //   （.xhs-im-conv-item__bottom-right 常为空 <!---->），detectUnread 全返回 false →
      //   "未读 0，跳过本轮" → 新消息永不上报。
      //   新逻辑：直接遍历所有会话，靠 _collectUnseenText 的 seenNodes/seen 去重，
      //   只有真正新增的消息才上行 inbound 触发 AI（"合理评论" = 不重复触发已处理消息）。
      const allList = [];
      try {
        const list = hook() || [];
        if (Array.isArray(list)) allList.push(...list);
      } catch (e) { this.log.warn('巡检阶段1读取列表失败', e && e.message); }
      // 去重
      const seenIds = new Set();
      const uniqList = allList.filter((c) => c && c.id && !seenIds.has(c.id) && seenIds.add(c.id));
      scannedTotal = uniqList.length;

      if (scanOnly) {
        this.log.debug(`巡检扫描-only 完成：总 ${scannedTotal}`);
        return { scannedTotal, unreadCount: 0, visited: 0, withNew: 0, captured: 0, failures: 0, durationMs: Date.now() - startedAt };
      }

      // 直接遍历所有会话，靠 seenNodes/seen 去重确保只捕获新消息
      let targets = uniqList;
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
    this.log.debug(`巡检一轮完成：扫描 ${scannedTotal} 个会话，访问 ${visited}，有新消息 ${withNew}，捕获新消息 ${captured}，失败 ${failures}，用时 ${dur}ms`);
    return { scannedTotal, unreadCount, visited, withNew, captured, failures, durationMs: dur };
  }

  async _patrolVisit(conv, { throttleMs, waitActiveMs }) {
    const opened = await this.openConversation(conv.id, { backfill: false, waitActiveMs });
    if (!opened) return { ok: false, newCount: 0 };
    const renderWaitMs = Math.min(throttleMs, 600);
    await sleep(renderWaitMs);
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
    const batch = [];
    // P1-3：首次巡检单次抓取上限 firstRunMaxBatch（20，防首次涌入），
    // 常规巡检 maxBatchPerPatrol（80）。剩余一律靠下轮扫描补齐。
    const firstRun = this._isFirstPatrolRun();
    const MAX_BATCH = firstRun ? PATROL_DEFAULTS.firstRunMaxBatch : PATROL_DEFAULTS.maxBatchPerPatrol;
    const cid = this.getConversationId() || this.conversationId || '';
    let items;
    try { items = this.getMessageItems() || []; } catch (_) { items = []; }
    for (const item of items) {
      if (batch.length >= MAX_BATCH) {
        if (firstRun) this.log.warn(`[首次] 巡检抓取消息数达上限 ${MAX_BATCH}，剩余靠下轮扫描补齐`);
        else this.log.warn(`[常规] 巡检抓取消息数达上限 ${MAX_BATCH}，剩余靠下轮扫描补齐`);
        break;
      }
      if (!item || this.seenNodes.has(item)) continue;
      let parsed;
      try { parsed = this.parseMessageItem(item); } catch (_) { continue; }
      if (!parsed) continue;
      // 会话归属校验：虚拟列表切换后 getMessageItems(root=document) 可能返回
      // 上一会话的残留 DOM 节点。检查 item 是否仍在活动消息容器内。
      const root = this.getMessageRoot();
      if (root && item && !root.contains(item)) { this.seenNodes.add(item); continue; }
      if (parsed.msg_type && parsed.msg_type !== 'text') { this.seenNodes.add(item); continue; }
      if (!parsed.text) { this.seenNodes.add(item); continue; }
      this.seenNodes.add(item);
      // 稳定键去重：巡检重访同一会话时，已上报过的消息按 (会话|发送者|文本) 拦截
      const key = this._dedupKey(cid, parsed);
      if (this._hasSent(key)) {
        this.log.debug('巡检稳定键去重跳过:', key.slice(0, 56));
        continue;
      }
      parsed.message_id = this._canonicalMsgId(item, cid, parsed);
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
    // sender_type 取最后一条消息的 sender_type（可能为 system）；event_id 取其 message_id（稳定）。
    const lastSenderType = last.sender_type || SENDER.CUSTOMER;
    const msg = makeUnifiedMessage({
      channel: this.channel,
      account_id: this.getAccountId(),
      conversation_id: cid,
      sender_type: lastSenderType,
      sender_id: isGroup && groupId ? groupId : (last.sender_id || cid),
      sender_name: senderName,
      content: last.text || '',
      msg_type: 'text',
      timestamp: last.timestamp || Date.now(),
      event_id: last.message_id,
      is_group: isGroup,
      group_id: groupId,
      group_name: groupName,
      history,
    });
    this.log.info(`巡检上行消息: 会话 ${cid}（${history.length} 轮新消息）`);
    if (this.callbacks.onMessage) this.callbacks.onMessage(msg);
  }

  _ingest(parsed) {
    if (!parsed) return;
    if (parsed.msg_type && parsed.msg_type !== 'text') return;
    if (!parsed.text) return;
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
    const parsed = this.parseMessageItem(item);
    if (!parsed) return;
    if (item) this.seenNodes.add(item);
    // 稳定键去重（跨 DOM 重渲染）：平台虚拟列表重渲染会换节点 → seenNodes 失效，
    // 但同一逻辑消息的文本+发送者+会话稳定 → 用稳定键拦截重复上行（避免 429 死循环/无效 POST）。
    const cid = this.getConversationId();
    const key = this._dedupKey(cid, parsed);
    if (this._hasSent(key)) {
      this.log.debug('稳定键去重跳过增量:', key.slice(0, 56));
      return;
    }
    parsed.message_id = this._canonicalMsgId(item, cid, parsed);
    this._markSent(key);
    this._ingest(parsed);
  }

  _backfill(root) {
    if (!this.getConversationId()) {
      this.log.info('回填跳过：当前无活动会话（私信列表视图）');
      return;
    }
    const batch = []; 
    const items = this.getMessageItems();
    const cid = this.getConversationId();
    for (const item of items) {
      if (this.seenNodes.has(item)) continue;
      const parsed = this.parseMessageItem(item);
      if (!parsed) continue;
      if (parsed.msg_type && parsed.msg_type !== 'text') continue;
      if (!parsed.text) continue;
      this.seenNodes.add(item);
      // 稳定键去重（跨 DOM 重渲染）：端口断开重连 / 虚拟列表重渲染换节点 seenNodes 失效后，
      // 平台重发的同一条历史消息按 (会话|发送者|文本) 拦截，避免重复上行。
      const key = this._dedupKey(cid, parsed);
      if (this._hasSent(key)) {
        this.log.debug('稳定键去重跳过历史:', key.slice(0, 56));
        continue;
      }
      parsed.message_id = this._canonicalMsgId(item, cid, parsed);
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
      content: parsed.text || '',
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
      history,
    });
    this.log.debug(`回填会话 ${cid}: 1 条会话级消息（${history.length} 轮历史, ${summaryDirection}）`);
    if (this.callbacks.onMessage) this.callbacks.onMessage(msg);
  }

  _emitMessage(parsed) {
    const cid = this.getConversationId();
    const windowMsgs = this._windowFor(cid);
    const msg = makeUnifiedMessage({
      channel: this.channel,
      account_id: this.getAccountId(),
      conversation_id: cid,
      sender_type: parsed.sender_type,
      sender_id: parsed.is_group && parsed.group_id ? parsed.group_id : (parsed.sender_id || ''),
      content: parsed.text,
      media_url: parsed.media_url || '',
      msg_type: parsed.msg_type || 'text',
      timestamp: parsed.timestamp,
      event_id: parsed.message_id,
      is_group: !!parsed.is_group,
      group_id: parsed.group_id || '',
      group_name: parsed.group_name || '',
      sender_name: parsed.sender_name || '',
      raw: parsed.raw || null,
      history: windowMsgs.map((m) => this._historyItem(m, m.sender_type === SENDER.CUSTOMER ? DIRECTION.INBOUND : DIRECTION.OUTBOUND)),
    });
    this.log.info('上行消息:', (msg.content || `[${msg.msg_type}]`).slice(0, 40));
    if (this.callbacks.onMessage) this.callbacks.onMessage(msg);
  }

  async sendOutbound(text, targetConvId) {
    if (!text) { this.log.warn('回复内容为空，跳过'); return { ok: false, rateLimited: false, notFound: false }; }
    const account = this.getAccountId();
    let conv = this.getConversationId();
    if (targetConvId && conv !== targetConvId) {
      // exact=true：只接受精确切到目标会话，杜绝"切到别处也当成功"→ 误发到其他会话
      const opened = await this.openConversation(targetConvId, { backfill: false, exact: true });
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
    let ok;
    try {
      // B4（批3）：rawSendText 裸 await——sendText 卡死（DOM 阻塞/框架吞事件）会永久挂起
      // 整条按会话串行的下行队列。统一 withTimeout，预算随文案长度伸缩（B7 拟人键入时长）。
      await withTimeout(this.rawSendText(text), humanSendTimeoutMs(text), `rawSendText(${this.channel})`);
      ok = true;
    } catch (e) {
      this.log.error('回写失败', String(e?.message || e));
      return { ok: false, rateLimited: false, notFound: false };
    }
    if (ok) {
      this.rateLimiter.markSent(this.channel, account, conv, text);
      stampGlobalSendAt(); // B6：不 await——storage 写失败已在内部吞掉，仅 best-effort 更新共享时钟
      this._emitMessage(
        { sender_type: SENDER.AGENT, text, media_url: '', timestamp: Date.now(), message_id: contentHash(this.channel, conv, text) }
      );
    }
    return { ok, rateLimited: false, notFound: false };
  }
}

