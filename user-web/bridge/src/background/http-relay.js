import { HTTP_INGEST_DEFAULTS, buildAuthHeaders } from '../core/http-ingest.js';
import { pickContentScriptFile } from './injector.js';

const pending = new Map();
const MAX_PENDING = 128;
const MAX_DOCUMENT_PENDING = 16;
const METHODS = new Map([
  ['/api/channelbus/ingest', 'POST'], ['/api/channelbus/outbox', 'GET'],
  ['/api/channelbus/outbox/ack', 'POST'], ['/api/channelbus/capabilities', 'GET'],
]);
const QUERY_KEYS = new Set(['channel', 'account_id', 'conversation_id', 'tenant_code', 'limit']);

function sourceChannel(sender) {
  if (!chrome.runtime?.id || sender?.id !== chrome.runtime.id || sender.frameId !== 0
    || !Number.isInteger(sender.tab?.id) || sender.tab.id < 0 || !sender.url?.startsWith('https:')) return null;
  const file = pickContentScriptFile(sender.url);
  if (!file) return null;
  return file === 'content-xhs.js' ? 'xiaohongshu' : file.slice('content-'.length, -'.js'.length);
}

const CHANNEL_ALIASES = new Map([
  ['xhs', 'xiaohongshu'], ['xhs_web', 'xiaohongshu'], ['xiaohongshu_web', 'xiaohongshu'],
  ['douyin_web', 'douyin'], ['kuaishou_web', 'kuaishou'], ['ks', 'kuaishou'],
  ['xianyu_web', 'xianyu'], ['tiktok_web', 'tiktok'], ['jd_web', 'jd'],
  ['jingdong', 'jd'], ['jd_chat', 'jd'], ['bili', 'bilibili'], ['bilibili_web', 'bilibili'],
  ['b站', 'bilibili'], ['wb', 'weibo'], ['weibo_web', 'weibo'],
  ['pinduoduo', 'pdd'], ['pdd_web', 'pdd'],
  // 2026-11-18 电商店铺五渠道（CH-15/18/19/28/35，二次审核补齐）：与
  // bridge.NormalizeBridgeChannel / 前端 CHANNEL_LEGACY_WEB_TO_CANONICAL 同源。
  ['taobao_web', 'taobao'], ['tmall', 'taobao'], ['qianniu', 'taobao'],
  ['jinritemai', 'douyin_shop'], ['feige', 'douyin_shop'], ['douyin_shop_web', 'douyin_shop'],
  ['kwaixiaodian', 'kuaishou_shop'], ['kuaishou_shop_web', 'kuaishou_shop'],
  ['shopee_web', 'shopee'], ['shopee_seller', 'shopee'],
  ['walmart', 'walmart_us'], ['walmart_web', 'walmart_us'],
]);
const normalizeChannel = (channel) => CHANNEL_ALIASES.get(channel) || channel;
const documentKey = (sender) => JSON.stringify([sender.tab.id, sender.frameId, sender.documentId || sender.url]);

function requestKey(requestId, sender) {
  if (typeof requestId !== 'string' || !/^[A-Za-z0-9_-]{1,100}$/.test(requestId)) return null;
  // ID 属于同一 tab/文档的请求；其他 tab 不能按碰撞 ID 中止它。
  return JSON.stringify([sender?.tab?.id ?? null, sender?.frameId ?? null,
    sender?.documentId ?? sender?.url ?? null, requestId]);
}

export function cancelHttpRelay(requestId, sender) {
  if (!sourceChannel(sender)) return false;
  const key = requestKey(requestId, sender);
  const request = key && pending.get(key);
  if (!request) return false;
  request.controller.abort();
  return true;
}

function abortError() { return new DOMException('HTTP 中转已取消', 'AbortError'); }

function untilAborted(work, signal) {
  return new Promise((resolve, reject) => {
    if (signal.aborted) { reject(abortError()); return; }
    let settled = false;
    const finish = (callback, value) => {
      if (settled) return;
      settled = true;
      signal.removeEventListener('abort', aborted);
      callback(value);
    };
    const aborted = () => finish(reject, abortError());
    signal.addEventListener('abort', aborted, { once: true });
    Promise.resolve(work).then(value => finish(resolve, value), error => finish(reject, error));
  });
}

function configuredRequest(message, cfg, channel) {
  if (typeof cfg?.serverUrl !== 'string' || typeof cfg.token !== 'string' || !cfg.token.trim()) throw new Error('配置未确认');
  const base = new URL(cfg.serverUrl.replace(/^ws/, 'http'));
  if (base.username || base.password || base.search || base.hash
    || !['https:', 'http:'].includes(base.protocol)
    || (base.protocol === 'http:' && !['localhost', '127.0.0.1', '[::1]'].includes(base.hostname))) throw new Error('配置边界不合法');
  const url = new URL(message.url);
  const prefix = base.pathname.replace(/\/$/, '');
  const path = url.pathname.slice(prefix.length);
  const method = message.options?.method || 'GET';
  if (url.origin !== base.origin || url.username || url.password || url.hash
    || !url.pathname.startsWith(prefix) || METHODS.get(path) !== method) throw new Error('目标超出桥接边界');
  for (const key of url.searchParams.keys()) {
    if (!QUERY_KEYS.has(key) || url.searchParams.getAll(key).length !== 1) throw new Error('查询参数未确认');
  }
  const requestedChannel = url.searchParams.get('channel');
  if ((path !== '/api/channelbus/capabilities' && !requestedChannel)
    || (requestedChannel && normalizeChannel(requestedChannel) !== channel)) throw new Error('渠道不匹配');
  if ((url.searchParams.get('tenant_code') || '') !== (cfg.tenantCode || '')) throw new Error('租户配置不匹配');
  const headers = new Headers(message.options?.headers || {});
  const token = cfg.token.trim();
  const identifyCode = headers.get('X-Channel-Identify-Code');
  const authorization = headers.get('Authorization');
  if ((!identifyCode && !authorization) || (identifyCode && identifyCode !== token)
    || (authorization && authorization !== `Bearer ${token}`)) throw new Error('请求凭证已过期');
  let body;
  if (method === 'POST') {
    body = message.options?.body;
    if (typeof body !== 'string' || body.length > 1024 * 1024) throw new Error('请求体不合法');
    const payload = JSON.parse(body);
    if (!payload || typeof payload !== 'object' || Array.isArray(payload)
      || (payload.channel && normalizeChannel(payload.channel) !== channel)
      || (payload.tenant_code && payload.tenant_code !== cfg.tenantCode)) throw new Error('请求体范围不匹配');
    for (const key of ['account_id', 'conversation_id']) {
      const scope = url.searchParams.get(key);
      if (scope && payload[key] && scope !== payload[key]) throw new Error('请求体身份不匹配');
    }
    if (path === '/api/channelbus/ingest') {
      const account = url.searchParams.get('account_id') || payload.account_id;
      const conversation = url.searchParams.get('conversation_id') || payload.conversation_id;
      if (typeof account !== 'string' || !account || !Array.isArray(payload.messages)) throw new Error('消息范围缺失');
      for (const item of payload.messages) {
        if (item == null) continue;
        if (typeof item !== 'object' || Array.isArray(item)
          || (item.channel && normalizeChannel(item.channel) !== channel)
          || (item.account_id && item.account_id !== account)
          || (conversation && item.conversation_id && item.conversation_id !== conversation)) {
          throw new Error('消息范围不匹配');
        }
      }
    }
  } else if (message.options?.body != null) throw new Error('读取请求不能携带请求体');
  return { url: url.toString(), options: { method, headers: buildAuthHeaders(token), body,
    credentials: 'omit', redirect: 'error', cache: 'no-store' } };
}

export async function relayHttpRequest(message, sender) {
  const channel = sourceChannel(sender);
  if (!channel || !message || (message.requestId != null && !requestKey(message.requestId, sender))) return { __err: 'HTTP 中转来源未确认' };
  const key = requestKey(message.requestId, sender);
  const scope = documentKey(sender);
  if ((key && pending.has(key))) return { __err: '重复的 HTTP 中转请求 ID' };
  if (pending.size >= MAX_PENDING || [...pending.values()].filter(request => request.scope === scope).length >= MAX_DOCUMENT_PENDING) {
    return { __err: 'HTTP 中转容量已满' };
  }
  const controller = new AbortController();
  // 无ID旧请求同样占预算；有ID的请求在读取配置前即可被取消。
  const reservation = key || Symbol('legacy-relay');
  pending.set(reservation, { controller, scope });
  const timer = setTimeout(() => controller.abort(), HTTP_INGEST_DEFAULTS.requestTimeoutMs);
  try {
    const stored = await untilAborted(chrome.storage.local.get('bridgeConfig'), controller.signal);
    if (controller.signal.aborted) throw abortError();
    const request = configuredRequest(message, stored?.bridgeConfig, channel);
    const response = await untilAborted(fetch(request.url, { ...request.options, signal: controller.signal }), controller.signal);
    const body = await untilAborted(response.text(), controller.signal);
    return {
      status: response.status,
      statusText: response.statusText,
      headers: Object.fromEntries(response.headers.entries()),
      body,
    };
  } catch (e) {
    return { __err: e?.name === 'AbortError' ? 'HTTP 中转已取消或超时' : 'HTTP 中转失败或范围未确认',
      ...(e?.name === 'AbortError' ? { errorName: 'AbortError' } : {}) };
  } finally {
    clearTimeout(timer);
    pending.delete(reservation);
  }
}
