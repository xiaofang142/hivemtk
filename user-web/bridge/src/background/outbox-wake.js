import { pickContentScriptFile } from './injector.js';
import { applyTenantCode, buildAuthHeaders } from '../core/http-ingest.js';

const CHANNEL_FILES = {
  douyin: 'content-douyin.js', xhs: 'content-xhs.js', xiaohongshu: 'content-xhs.js',
  tiktok: 'content-tiktok.js', xianyu: 'content-xianyu.js', kuaishou: 'content-kuaishou.js',
  jd: 'content-jd.js', bilibili: 'content-bilibili.js', weibo: 'content-weibo.js', pdd: 'content-pdd.js',
};

const canonicalChannel = (channel) => channel === 'xhs' ? 'xiaohongshu' : channel;

// 通知不是授权或消息载荷；严格只接受一个空对象的固定事件。
export async function readOutboxWakeStream(body, signal, ready) {
  const reader = body.getReader();
  const decoder = new TextDecoder('utf-8', { fatal: true });
  let buffer = '';
  let event = '';
  let data = [];
  let frameSize = 0;
  const abort = () => { reader.cancel().catch(() => {}); };
  signal.addEventListener('abort', abort, { once: true });
  try {
    while (!signal.aborted) {
      const chunk = await reader.read();
      if (chunk.done) break;
      buffer += decoder.decode(chunk.value, { stream: true });
      let newline;
      while ((newline = buffer.indexOf('\n')) >= 0) {
        const line = buffer.slice(0, newline).replace(/\r$/, '');
        buffer = buffer.slice(newline + 1);
        frameSize += line.length + 1;
        if (frameSize > 8192) throw new Error('wake frame exceeds limit');
        if (!line) {
          if (event === 'outbox_ready' && data.join('\n').trim() === '{}' && !signal.aborted) await ready();
          event = '';
          data = [];
          frameSize = 0;
        } else if (line.startsWith('event:')) {
          event = line.slice(6).trim();
        } else if (line.startsWith('data:')) {
          data.push(line.slice(5).trimStart());
        }
      }
      if (buffer.length + frameSize > 8192) throw new Error('wake frame exceeds limit');
    }
  } finally {
    signal.removeEventListener('abort', abort);
    await reader.cancel().catch(() => {});
    reader.releaseLock();
  }
}

export function createOutboxWakeRuntime(api, fetcher = globalThis.fetch) {
  const registrations = new Map();
  const pending = new Map();
  const connections = new Map();
  let disposed = false;
  let generation = 0;

  function valid(message, sender) {
    return !disposed && sender?.id === api?.runtime?.id && sender?.frameId === 0
      && Number.isInteger(sender?.tab?.id) && sender.tab.id >= 0
      && /^https:/.test(sender.url || '')
      && !!CHANNEL_FILES[message?.channel] && pickContentScriptFile(sender.url) === CHANNEL_FILES[message.channel]
      && typeof message.accountId === 'string' && message.accountId.length <= 100
      && !!message.accountId && message.accountId !== 'default' && !/[\s\p{Cc}]/u.test(message.accountId)
      && typeof message.registrationId === 'string' && /^[a-zA-Z0-9_-]{1,100}$/.test(message.registrationId);
  }

  const registrationKey = (message, sender) => JSON.stringify([sender.tab.id, message.channel]);
  function close(connection) {
    connection.closed = true;
    clearTimeout(connection.retry);
    connection.controller?.abort();
    connections.delete(connection.key);
  }
  function prune() {
    for (const connection of connections.values()) {
      if (![...registrations.values()].some((registration) => registration.connection === connection)) close(connection);
    }
  }

  async function notify(connection) {
    if (connection.closed || disposed) return;
    for (const registration of registrations.values()) {
      if (registration.connection !== connection) continue;
      try {
        await api.tabs.sendMessage(registration.tabId, {
          type: 'BRIDGE_OUTBOX_READY', channel: registration.channel,
          accountId: registration.accountId, registrationId: registration.registrationId,
        }, { frameId: 0 });
      } catch (_) { /* 页签离开/Worker重启：原轮询仍是可靠退路。 */ }
    }
  }

  async function connect(connection) {
    if (connection.closed || disposed) return;
    const controller = new AbortController();
    connection.controller = controller;
    const startedAt = Date.now();
    let retryFloor = 0;
    // 连接建立也有预算；读到HTTP头后改由心跳与服务器流时长约束。
    const connectTimeout = setTimeout(() => controller.abort(), 10000);
    let idle;
    const resetIdle = () => { clearTimeout(idle); idle = setTimeout(() => controller.abort(), 45000); };
    try {
      const response = await fetcher(connection.url, {
        method: 'GET', headers: buildAuthHeaders(connection.token), signal: controller.signal,
        credentials: 'omit', cache: 'no-store', redirect: 'error',
      });
      clearTimeout(connectTimeout);
      if (response.status === 429) {
        const raw = response.headers?.get('retry-after');
        const seconds = raw && /^\d+(?:\.\d+)?$/.test(raw) ? Number(raw) : NaN;
        const advised = Number.isFinite(seconds) ? seconds * 1000 : Date.parse(raw || '') - Date.now();
        retryFloor = Math.max(60000, Number.isFinite(advised) ? Math.min(advised, 86400000) : 0);
      } else if (response.status >= 400 && response.status < 500 && response.status !== 408) {
        retryFloor = 60000;
      }
      if (!response.ok || !response.body || !response.headers.get('content-type')?.includes('text/event-stream')) throw new Error('wake unavailable');
      resetIdle();
      // 每个网络块（含心跳）重置空闲预算，不输出响应内容。
      const reader = response.body.getReader();
      const observed = new ReadableStream({
        async pull(c) {
          try {
            const value = await reader.read();
            if (value.done) c.close();
            else { resetIdle(); c.enqueue(value.value); }
          } catch (error) { c.error(error); }
        },
        cancel() { return reader.cancel(); },
      });
      await readOutboxWakeStream(observed, controller.signal, () => notify(connection));
    } catch (_) { /* 只退避，不打印凭证、URL或响应正文。 */ }
    finally {
      clearTimeout(connectTimeout);
      clearTimeout(idle);
      controller.abort();
      if (!connection.closed && !disposed) {
        // 只有确实维持过健康连接才重置退避，不能以收到200头部代替稳定流。
        if (Date.now() - startedAt >= 30000 && retryFloor === 0) connection.failures = 0;
        const delay = Math.max(retryFloor, Math.min(30000, 1000 * 2 ** Math.min(connection.failures++, 5))) + Math.random() * 500;
        connection.retry = setTimeout(() => connect(connection), delay);
      }
    }
  }

  async function register(message, sender) {
    if (!valid(message, sender)) return false;
    const epoch = generation;
    const regKey = registrationKey(message, sender);
    if (!registrations.has(regKey) && !pending.has(regKey) && registrations.size + pending.size >= 256) return false;
    const reservation = { registrationId: message.registrationId, tabId: sender.tab.id };
    pending.set(regKey, reservation);
    let cfg;
    try { cfg = (await api.storage.local.get('bridgeConfig'))?.bridgeConfig; }
    catch (_) { if (pending.get(regKey) === reservation) pending.delete(regKey); return false; }
    if (pending.get(regKey) !== reservation) return false;
    pending.delete(regKey);
    if (disposed || epoch !== generation || !cfg?.serverUrl || !cfg.token?.trim()) return false;
    let url;
    try {
      const base = new URL(cfg.serverUrl);
      if (base.username || base.password || !['https:', 'http:'].includes(base.protocol)) return false;
      if (base.protocol === 'http:' && !['localhost', '127.0.0.1', '[::1]'].includes(base.hostname)) return false;
      url = new URL(base.pathname.replace(/\/$/, '') + '/api/channelbus/outbox/wake', base.origin);
      url.searchParams.set('channel', canonicalChannel(message.channel));
      url.searchParams.set('account_id', message.accountId);
      url = applyTenantCode(url.toString(), cfg.tenantCode);
    } catch (_) { return false; }
    const key = JSON.stringify([url, cfg.token.trim()]); // 内存内比较，不向页面或日志返回。
    if (!registrations.has(regKey) && registrations.size >= 256) return false;
    let connection = connections.get(key);
    if (!connection) {
      connection = { key, url, token: cfg.token.trim(), failures: 0, closed: false };
      connections.set(key, connection);
    }
    registrations.set(regKey, {
      channel: message.channel, accountId: message.accountId,
      registrationId: message.registrationId, tabId: sender.tab.id, connection,
    });
    prune();
    if (!connection.controller) void connect(connection);
    return true;
  }

  function unregister(message, sender) {
    if (!valid(message, sender)) return;
    const key = registrationKey(message, sender);
    if (pending.get(key)?.registrationId === message.registrationId) pending.delete(key);
    if (registrations.get(key)?.registrationId === message.registrationId) registrations.delete(key);
    prune();
  }
  function removeTab(tabId) {
    for (const [key, reservation] of pending) if (reservation.tabId === tabId) pending.delete(key);
    for (const [key, registration] of registrations) if (registration.tabId === tabId) registrations.delete(key);
    prune();
  }
  function invalidate() {
    generation++;
    pending.clear();
    registrations.clear();
    for (const connection of connections.values()) close(connection);
  }
  return { register, unregister, removeTab, invalidate, dispose() { disposed = true; invalidate(); } };
}

// 由既有 sse-client 导入装配，旧消息投递接口不变。
if (typeof chrome !== 'undefined' && chrome.runtime?.onMessage && chrome.storage?.local) {
  const runtime = createOutboxWakeRuntime(chrome);
  chrome.runtime.onMessage.addListener((message, sender, respond) => {
    if (message?.type === 'BRIDGE_WAKE_REGISTER') {
      runtime.register(message, sender).then((registered) => respond({ registered }), () => respond({ registered: false }));
      return true;
    }
    if (message?.type === 'BRIDGE_WAKE_UNREGISTER') runtime.unregister(message, sender);
    return false;
  });
  chrome.tabs?.onRemoved?.addListener((tabId) => runtime.removeTab(tabId));
  chrome.tabs?.onUpdated?.addListener((tabId, change) => { if (change.url) runtime.removeTab(tabId); });
  chrome.storage.onChanged?.addListener((changes, area) => {
    if (area === 'local' && changes.bridgeConfig) runtime.invalidate();
  });
}
