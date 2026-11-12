import { DEFAULT_USER_SERVER } from '../core/constants.js';
import { createLogger } from '../core/logger.js';
import { autoInjectOnCommit, injectContentScript, scriptingAvailable } from './injector.js';
import {
  buildSelectorHealthRequest,
  mergeSelectorHealthItems,
  postSelectorHealth,
  subtractSelectorHealthItems,
} from '../core/selector-health.js';
import './sse-client.js';

const log = createLogger('bg', 'bg');

// 存储 key：popup 与 background 共享，禁止字面量散布
const KEY = 'bridgeConfig';
// M-3：媒体下载代理上限（与 content 端 media-inject.js MAX_MEDIA_BYTES 一致）。
// base64 后 ~34MB，chrome.runtime 消息通道 64MB 上限内。
const MEDIA_PROXY_MAX_BYTES = 25 * 1024 * 1024;
const MEDIA_PROXY_TIMEOUT_MS = 10000;
// content 端写入的 active meta（按 channel 索引；多个 content 可同时存在，popup 取最新）
const KEY_ACTIVE = 'bridgeActiveByChannel';
// content 端上报的 health snapshot（按 channel 索引；2026-08-14 P0-C 死开关）
const KEY_HEALTH = 'bridgeHealthByChannel';
// B1-a：选择器命中率的待上报增量（按 channel 索引；content 端累计够量才推过来）
const KEY_SELECTOR_HEALTH = 'bridgeSelectorHealthPending';
// B1-a：定时把待上报增量发给服务端（service worker 会被浏览器挂起，必须用 alarms 而不是 setInterval）
const SELECTOR_HEALTH_ALARM = 'bridgeSelectorHealthFlush';

// ---- chrome API 兜底 ----
function lastError() {
  try {
    if (!chrome.runtime.lastError) return null;
    return chrome.runtime.lastError.message || '(无错误详情)';
  } catch (_) { return null; }
}

// ---- 配置存储 ----
function getConfig() {
  return new Promise((resolve) => {
    chrome.storage.local.get(KEY, (res) => {
      resolve(
        res[KEY] || {
          serverUrl: DEFAULT_USER_SERVER.baseUrl,
          token: '',
          tenantCode: '',
          autoConnect: true,
        }
      );
    });
  });
}

function setConfig(cfg) {
  return new Promise((resolve) => {
    chrome.storage.local.set({ [KEY]: cfg }, () => {
      const err = lastError();
      if (err) {
        resolve({ ok: false, error: err });
        return;
      }
      resolve({ ok: true });
    });
  });
}

// USR-BR-01: 健康度上报增强
// 后端聚合：channel 错误率告警 / popup 实时卡片 / 异常自动暂停
chrome.runtime.onMessage.addListener((msg, _sender, sendResponse) => {
  if (msg && msg.type === 'bridgeFetch') {
    // content script 在页面源下直连服务端受 CORS/CSP 限制（生产实证
    // TypeError: Failed to fetch），统一经 SW 中转：SW 是扩展页，
    // host_permissions 豁免 CORS。响应只回状态/头/正文，调用方只读这些。
    fetch(msg.url, msg.options || {})
      .then(async (r) => {
        const body = await r.text();
        sendResponse({
          status: r.status,
          statusText: r.statusText,
          headers: Object.fromEntries(r.headers.entries()),
          body,
        });
      })
      .catch((e) => sendResponse({ __err: String((e && e.message) || e) }));
    return true;
  }
  if (msg && msg.type === 'getStatus') {
    chrome.storage.local.get([KEY_ACTIVE, KEY_HEALTH], (res) => {
      const statuses = res[KEY_ACTIVE] || {};
      const healthByChannel = res[KEY_HEALTH] || {};
      // 统计异常 channel（健康度超时 / 错误率 > 50%）
      const unhealthy = [];
      const now = Date.now();
      for (const ch of Object.keys(healthByChannel)) {
        const h = healthByChannel[ch];
        if (h.errorRate && h.errorRate > 0.5) {
          unhealthy.push({ channel: ch, errorRate: h.errorRate, lastError: h.lastError });
        }
        if (now - (h.reportedAt || 0) > 60000) {
          // 健康度 60s 未更新，视为异常
        }
      }
      sendResponse({ statuses, health: healthByChannel, unhealthy, routes: 0, connStats: null });
    });
    return true;
  }
  if (msg && msg.type === 'setConfig') {
    setConfig(msg.config).then((r) => sendResponse(r));
    return true;
  }
  if (msg && msg.type === 'injectContentScript') {
    const { tabId, url, allFrames } = msg;
    if (typeof tabId !== 'number') {
      sendResponse({ ok: false, reason: 'invalid_tabId' });
      return false;
    }
    injectContentScript(tabId, url, { allFrames: !!allFrames }, (r) => {
      sendResponse(r);
    });
    return true;
  }
  if (msg && msg.type === 'reportHealth') {
    const { channel, snapshot } = msg;
    if (!channel || !snapshot) {
      sendResponse({ ok: false, reason: 'invalid_payload' });
      return false;
    }
    chrome.storage.local.get(KEY_HEALTH, (res) => {
      const cur = res[KEY_HEALTH] || {};
      cur[channel] = { ...snapshot, reportedAt: Date.now() };
      chrome.storage.local.set({ [KEY_HEALTH]: cur }, () => {
        const err = lastError();
        if (err) sendResponse({ ok: false, error: err });
        else sendResponse({ ok: true });
        // USR-BR-01: 错误率过高自动暂停
        if (snapshot.errorRate > 0.7) {
          console.warn(`[bg] channel ${channel} 错误率 ${(snapshot.errorRate * 100).toFixed(0)}%，自动暂停`);
          broadcastToAllTabs({ type: 'emergencyStop', reason: 'high_error_rate', channel });
        }
      });
    });
    return true;
  }
  // B1-a：选择器命中率增量（content → 这里 → 服务端）。
  //
  // 只落 storage、不在这里直接 POST：content script 可能同时有多个标签页在报，
  // 而 token/serverUrl 的读点在 background 的配置里。攒到定时器（或调用方显式
  // flush）再走一趟网络，避免每个 content 每次上报都发一次请求。
  if (msg && msg.type === 'reportSelectorHealth') {
    const { channel, items } = msg;
    if (!channel || !Array.isArray(items) || items.length === 0) {
      sendResponse({ ok: false, reason: 'invalid_payload' });
      return false;
    }
    chrome.storage.local.get(KEY_SELECTOR_HEALTH, (res) => {
      const cur = res[KEY_SELECTOR_HEALTH] || {};
      cur[channel] = mergeSelectorHealthItems(cur[channel], items);
      chrome.storage.local.set({ [KEY_SELECTOR_HEALTH]: cur }, () => {
        const err = lastError();
        if (err) { sendResponse({ ok: false, error: err }); return; }
        sendResponse({ ok: true, pending: cur[channel].length });
      });
    });
    return true;
  }
  // 自检页/排障用：立刻把待上报增量发出去，并回传服务端的判定结果。
  if (msg && msg.type === 'flushSelectorHealth') {
    flushSelectorHealthPending().then((r) => sendResponse(r));
    return true;
  }
  // M-3 下行媒体原生发送：content script 直取被平台 CORS/防盗链拦截时，
  // 由 background（host_permissions 免 CORS）代取一次，base64 回传给 content
  // 组 File 注入编辑器。字节仅在扩展内存转发，不落盘、不外发第三方；
  // 只读 GET，失败如实回 error，由 content 端走文本降级（不丢行）。
  if (msg && msg.type === 'bridge:fetchMedia') {
    const url = typeof msg.url === 'string' ? msg.url : '';
    if (!/^https?:\/\//i.test(url)) {
      sendResponse({ ok: false, error: 'invalid_url' });
      return false;
    }
    proxyFetchMedia(url, Math.min(Number(msg.maxBytes) || MEDIA_PROXY_MAX_BYTES, MEDIA_PROXY_MAX_BYTES))
      .then((r) => sendResponse(r))
      .catch((e) => sendResponse({ ok: false, error: String((e && e.message) || e) }));
    return true;
  }
  if (msg && msg.type === 'emergencyStop') {
    broadcastToAllTabs({ type: 'emergencyStop', reason: msg.reason || 'manual' });
    sendResponse({ ok: true, broadcast: true });
    return false;
  }
  if (msg && msg.type === 'resumeBridge') {
    broadcastToAllTabs({ type: 'resumeBridge' });
    sendResponse({ ok: true, broadcast: true });
    return false;
  }
  if (msg && msg.type === 'getAccounts') {
    chrome.storage.local.get([KEY_ACTIVE, KEY_HEALTH], (res) => {
      sendResponse({
        accounts: res[KEY_ACTIVE] || {},
        health: res[KEY_HEALTH] || {},
      });
    });
    return true;
  }
  // P1-P6：账号暂停/启用 —— 持久化到 config.accounts 并广播 accountsUpdated（content 立即生效）
  if (msg && msg.type === 'bridge:setAccountEnabled') {
    const { channel, accountId, enabled } = msg;
    if (!channel || !accountId) {
      sendResponse({ ok: false, error: 'bridge:setAccountEnabled 缺少 channel/accountId' });
      return false;
    }
    getConfig().then((cfg) => {
      const accounts = cfg.accounts && typeof cfg.accounts === 'object' && !Array.isArray(cfg.accounts)
        ? cfg.accounts : {};
      const ch = accounts[channel] && typeof accounts[channel] === 'object' && !Array.isArray(accounts[channel])
        ? accounts[channel] : {};
      ch[accountId] = { enabled: !!enabled, pausedAt: enabled ? null : Date.now() };
      accounts[channel] = ch;
      return setConfig({ ...cfg, accounts });
    }).then((r) => {
      if (!r.ok) { sendResponse({ ok: false, error: r.error || '持久化失败' }); return; }
      broadcastToAllTabs({ type: 'bridge:accountsUpdated', channel, accountId, enabled: !!enabled });
      sendResponse({ ok: true });
    });
    return true;
  }
  // P1-P6：账号切换 —— 更新 channel/accountId/conversationId 并广播 accountsUpdated
  if (msg && msg.type === 'bridge:switchAccount') {
    const { channel, accountId, conversationId } = msg;
    if (!channel || !accountId) {
      sendResponse({ ok: false, error: 'bridge:switchAccount 缺少 channel/accountId' });
      return false;
    }
    getConfig().then((cfg) =>
      setConfig({ ...cfg, channel, accountId, conversationId: conversationId || '' })
    ).then((r) => {
      if (!r.ok) { sendResponse({ ok: false, error: r.error || '持久化失败' }); return; }
      broadcastToAllTabs({ type: 'bridge:accountsUpdated', channel, accountId });
      sendResponse({ ok: true });
    });
    return true;
  }
  return false;
});

// 2026-08-15 M2-P1-产品3：向所有抖音/小红书/TikTok/闲鱼 标签页广播消息
//   用于 kill switch 立即生效（不等 storage.onChanged 轮询）
function broadcastToAllTabs(payload) {
  try {
    if (!chrome.tabs || !chrome.tabs.query) return;
    chrome.tabs.query({}, (tabs) => {
      const list = Array.isArray(tabs) ? tabs : [];
      for (const t of list) {
        if (!t || !t.id || !t.url) continue;
        // CH-16 首切片（2026-11-09）：pinduoduo.com 一并广播（kill switch 对 pdd 同样即时生效）。
        if (!/douyin\.com|xiaohongshu\.com|tiktok\.com|goofish\.com|xianyu\.com|kuaishou\.com|jdm\.jd\.com|dongdong\.jd\.com|chat\.jd\.com|im\.jd\.com|message\.bilibili\.com|www\.bilibili\.com|weibo\.com|pinduoduo\.com/i.test(t.url)) continue;
        try {
          chrome.tabs.sendMessage(t.id, payload, () => { try { void chrome.runtime.lastError; } catch (_) {} });
        } catch (_) {  }
      }
    });
  } catch (e) {
    log.warn('broadcastToAllTabs 失败：' + (e && e.message ? e.message : String(e)));
  }
}

setInterval(() => {
  chrome.storage.local.get(KEY_HEALTH, (res) => {
    const cur = res[KEY_HEALTH] || {};
    const now = Date.now();
    let changed = false;
    for (const ch of Object.keys(cur)) {
      if (now - (cur[ch].reportedAt || 0) > 600000) { 
        delete cur[ch];
        changed = true;
      }
    }
    if (changed) chrome.storage.local.set({ [KEY_HEALTH]: cur });
  });
}, 60000); 

// ── M-3：媒体下载代理（content → background）────────────────────────────
// content script 直取平台媒体常被 CORS/防盗链拦（opaque/no-cors 读不到体）；
// background 持 host_permissions，跨源 GET 免 CORS。只搬字节不落盘。
// 大小/content-length 双重上限；10s 超时；错误如实回传，content 端据此降级文本。
async function proxyFetchMedia(url, maxBytes) {
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), MEDIA_PROXY_TIMEOUT_MS);
  try {
    const resp = await fetch(url, { signal: ctrl.signal });
    if (!resp.ok) return { ok: false, error: `HTTP ${resp.status}` };
    const lenHeader = Number(resp.headers.get('content-length') || 0);
    if (lenHeader > maxBytes) return { ok: false, error: `too_large(content-length ${lenHeader})` };
    const buf = await resp.arrayBuffer();
    if (buf.byteLength > maxBytes) return { ok: false, error: `too_large(${buf.byteLength})` };
    const bytes = new Uint8Array(buf);
    // 分块转二进制串（避开 String.fromCharCode(...spread) 的参数上限与栈溢出）
    let bin = '';
    const CHUNK = 0x8000;
    for (let i = 0; i < bytes.length; i += CHUNK) {
      bin += String.fromCharCode.apply(null, bytes.subarray(i, i + CHUNK));
    }
    return {
      ok: true,
      base64: btoa(bin),
      mime: resp.headers.get('content-type') || '',
      bytes: bytes.byteLength,
    };
  } finally {
    clearTimeout(timer);
  }
}

// ── B1-a：选择器命中率上报 ────────────────────────────────────────────────
//
// 失败时**保留**增量（不清空）：观测面丢一次采样，等于把「命中率掉了」这件事推迟一个
// 周期，而平台改版本来就是越早看到越好。
// 成功后按「已发出去的那一批」做减法而不是整份清空：POST 期间 content 仍在计数，
// 直接清空会连那段时间的观测一起丢掉（丢分母 ⇒ 命中率偏高 ⇒ 恰好掩盖问题）。
function subtractSelectorHealthPending(channel, sent) {
  return new Promise((resolve) => {
    chrome.storage.local.get(KEY_SELECTOR_HEALTH, (res) => {
      const cur = res[KEY_SELECTOR_HEALTH] || {};
      const left = subtractSelectorHealthItems(cur[channel], sent);
      if (left.length) cur[channel] = left;
      else delete cur[channel];
      chrome.storage.local.set({ [KEY_SELECTOR_HEALTH]: cur }, () => resolve());
    });
  });
}

async function flushSelectorHealthPending() {
  let snapshot = {};
  try {
    snapshot = await new Promise((resolve) => {
      chrome.storage.local.get(KEY_SELECTOR_HEALTH, (res) => resolve(res[KEY_SELECTOR_HEALTH] || {}));
    });
  } catch (e) {
    return { ok: false, reason: 'storage_unavailable' };
  }
  const channels = Object.keys(snapshot);
  if (!channels.length) return { ok: true, sent: 0, failed: 0 };
  const cfg = await getConfig();
  const results = [];
  for (const channel of channels) {
    const items = snapshot[channel] || [];
    if (!items.length) continue;
    let req;
    try {
      req = buildSelectorHealthRequest({
        serverUrl: cfg.serverUrl || DEFAULT_USER_SERVER.baseUrl,
        token: cfg.token,
        tenantCode: cfg.tenantCode,
        channel,
        // account_id 只用于服务端排障（哪个浏览器实例在报），不参与聚合；配置里没有
        // 账号时留空而不是编一个——空字符串在服务端是"未声明"，编出来的值会误导排障。
        accountId: '',
        items,
      });
    } catch (e) {
      results.push({ channel, ok: false, error: e && e.message ? e.message : String(e) });
      continue;
    }
    const r = await postSelectorHealth(req);
    results.push({ channel, ok: r.ok, status: r.status, error: r.error, degraded: r.degraded });
    if (r.ok) await subtractSelectorHealthPending(channel, items);
  }
  const failed = results.filter((x) => !x.ok);
  if (failed.length) log.warn('选择器健康上报失败：' + JSON.stringify(failed));
  else if (results.length) log.info(`选择器健康已上报 ${results.length} 个渠道`);
  return { ok: failed.length === 0, sent: results.length, failed: failed.length, results };
}

// service worker 会被浏览器挂起：setInterval 在 MV3 里不可靠，周期任务必须走 alarms。
// 1 分钟一次的代价可以忽略（一次 storage 读 + 增量非空时才发网络），
// 而周期太长会让「平台改版」到「运营看到」之间的窗口以小时计。
if (chrome.alarms && typeof chrome.alarms.create === 'function') {
  try {
    chrome.alarms.create(SELECTOR_HEALTH_ALARM, { periodInMinutes: 1 });
    chrome.alarms.onAlarm.addListener((a) => {
      if (a && a.name === SELECTOR_HEALTH_ALARM) void flushSelectorHealthPending();
    });
  } catch (e) {
    log.warn('选择器健康定时上报未挂上：' + (e && e.message ? e.message : String(e)));
  }
}

if (chrome.webNavigation && chrome.webNavigation.onCommitted) {
  chrome.webNavigation.onCommitted.addListener(autoInjectOnCommit);
  log.info('webNavigation.onCommitted 已接入：自动注入兜底');
} else {
  log.warn('webNavigation API 不可用，依赖 manifest content_scripts 静态注入');
}

if (scriptingAvailable()) {
  log.info('chrome.scripting 可用：popup 侧可触发自愈注入');
}

log.info('background 已就绪（HTTP-only 模式，0 WS）');

