import { DEFAULT_USER_SERVER, BRIDGE_PROTOCOL_V2 } from './constants.js';
import { createLogger } from './logger.js';

const log = createLogger('http', 'bridge');

// HTTP ingest 端点路径（与 user-server/internal/bridge/handler_http.go POST /api/bridge/ingest 严格对齐）
const INGEST_PATH = '/api/bridge/ingest';

// 长轮询/连接默认参数
const HTTP_INGEST_DEFAULTS = Object.freeze({
  longPollTimeoutMs: 500000,
  requestTimeoutMs: 30000,
  maxRetries: 3,
  retryBaseMs: 1000,
  maxContentBytes: 4 * 1024,
});

// 把 serverUrl 的 scheme 切换为 http(s)（ingest 是 HTTP，非 WS）
function toHttpUrl(serverUrl) {
  return serverUrl.replace(/^ws/, 'http');
}

// 构造带认证的请求头（2026-08-14 P0-A：token 一律走 Header，绝不放 URL query）。
// 有 token → Authorization: Bearer <token>；空 token → 不设 Authorization（服务端走匿名/默认分支）。
export function buildAuthHeaders(token) {
  const headers = { 'Content-Type': 'application/json' };
  if (token && token.trim()) {
    headers['Authorization'] = `Bearer ${token.trim()}`;
  }
  return headers;
}

// 构造完整的上行 URL（HTTP ingest 端点 + channel/account_id/conversation_id 全部 query 参数）。
//
// 文档源：DEFAULT_USER_SERVER.baseUrl（用户配置或默认）与 user-server internal/router/router.go:337
// （POST /api/bridge/ingest）严格对齐。
//
// 入参：
//   - serverUrl: 用户配置的 baseUrl（http/https/ws/wss 都接受，自动归一为 http(s)）
//   - params: { channel, accountId, conversationId }
//   - token 一律走 Authorization Header（2026-08-14 P0-A），绝不进 URL query。
// 返回：完整 URL 字符串，可直接传给 fetch。
function buildIngestUrl(serverUrl, params) {
  const u = new URL(`${toHttpUrl(serverUrl)}${INGEST_PATH}`);
  u.searchParams.set('channel', params.channel || '');
  u.searchParams.set('account_id', params.accountId || '');
  if (params.conversationId) u.searchParams.set('conversation_id', params.conversationId);
  return u.toString();
}

// 把 URL 解析为可读参数表（仅用于日志展示，不影响真实请求）。
// 真实请求仍由 URL + searchParams 决定，此处仅做 mirror 打印。
function describeIngestParams(url) {
  try {
    const u = new URL(url);
    const out = {};
    for (const [k, v] of u.searchParams.entries()) {
      if (k === 'token') {
        out[k] = v ? `${v.slice(0, 4)}***(${v.length} chars)` : '';
      } else {
        out[k] = v;
      }
    }
    return {
      origin: u.origin,
      path: u.pathname,
      query: out,
    };
  } catch (_) {
    return { url, parseError: true };
  }
}

// 截断 body 用于日志预览（与 user-server 端 readBodyPreview 4KB 上限对齐）
function previewBody(body, maxBytes = 4096) {
  if (body == null) return { preview: '', size: 0, truncated: false };
  const s = typeof body === 'string' ? body : JSON.stringify(body);
  if (s.length <= maxBytes) {
    return { preview: s, size: s.length, truncated: false };
  }
  return { preview: s.slice(0, maxBytes) + `... [truncated, total=${s.length} bytes]`, size: s.length, truncated: true };
}

// _logRequest 统一打印 HTTP ingest 上行（完整 URL + 解析后的 origin/path/query 字段 + body 预览）。
// 所有渠道（douyin/xiaohongshu/tiktok/xianyu/kuaishou）共用，格式与
// user-server 侧 collectHTTPRequestInfo 输出严格对齐，便于日志对照定位。
//
// 失败（URL 构造抛错）返回 null，调用方需自行处理。
// 成功返回 { url, payload }：url 给 fetch 使用；payload 给测试 / 调试使用。
function _logRequest(label, serverUrl, params, body, extra) {
  let url;
  let parsed;
  try {
    url = buildIngestUrl(serverUrl, params);
    parsed = describeIngestParams(url);
  } catch (e) {
    log.error(`${label} URL 构造失败`, e);
    return null;
  }
  const bodyInfo = previewBody(body);
  // 提取消息核心内容：用户最关心"上行了什么消息"，但 body_preview 被截断后看不到
  const msgs = Array.isArray(body && body.messages) ? body.messages : [];
  const messagesSummary = msgs.map((m) => ({
    sender: m.sender_type || '?',
    content: (m.content || '').slice(0, 80),
    event_id: (m.event_id || '').slice(-12),
  }));
  const payload = {
    url,
    ...(parsed || {}),
    body_preview: bodyInfo.preview,
    body_size: bodyInfo.size,
    body_truncated: bodyInfo.truncated,
    messages_count: msgs.length,
    messages_summary: messagesSummary,
    ...(extra || {}),
  };
  log.info(label, payload);
  return { url, payload };
}

async function fetchWithRetry(url, options, retryOpts = {}) {
  const maxRetries = retryOpts.maxRetries ?? HTTP_INGEST_DEFAULTS.maxRetries;
  const retryBaseMs = retryOpts.retryBaseMs ?? HTTP_INGEST_DEFAULTS.retryBaseMs;
  let lastErr = null;
  for (let attempt = 0; attempt <= maxRetries; attempt++) {
    try {
      const res = await fetch(url, options);
      if (!res.ok) {
        const text = await res.text().catch(() => '');
        // 429 限流 / 408 超时：可重试（退避后大概率恢复）；
        // 其余 4xx 视为客户端错误，直接放弃（重试无意义）。
        // 429 限流 / 408 超时 / 5xx 服务端错误：可重试（退避后大概率恢复）；
        // 其余 4xx（400/401/403/404...）：客户端错误，不可重试。
        const retryable =
          res.status === 429 || res.status === 408 || res.status >= 500;
        const err = new Error(
          `HTTP ${res.status} ${res.statusText}: ${text.slice(0, 200)}`
        );
        if (res.status === 429) {
          const ra = res.headers.get('Retry-After');
          if (ra) {
            const secs = parseInt(ra, 10);
            if (!Number.isNaN(secs)) err.retryAfterMs = secs * 1000;
          }
        }
        if (!retryable) err.nonRetryable = true;
        throw err;
      }
      return res;
    } catch (e) {
      lastErr = e;
      if (e && e.nonRetryable) break;
      if (attempt >= maxRetries) break;
      // 退避：优先用服务端 Retry-After（429 场景），否则指数退避 + 抖动
      let delay = Math.min(8000, retryBaseMs * Math.pow(2, attempt)) + Math.floor(Math.random() * 200);
      if (e && e.retryAfterMs && e.retryAfterMs > 0) {
        delay = Math.max(delay, e.retryAfterMs);
      }
      log.warn(`fetch 失败，${delay}ms 后重试 #${attempt + 1}/${maxRetries}`, { error: String(e && e.message || e) });
      await new Promise((r) => setTimeout(r, delay));
    }
  }
  throw lastErr || new Error('fetch failed');
}

async function postIngest({ serverUrl, channel, accountId, conversationId, token }, body, opts = {}) {
  const params = { channel, accountId, conversationId, token };
  const label = opts.label || '[HTTP ingest]';
  const logResult = _logRequest(label, serverUrl, params, body);
  if (!logResult) {
    throw new Error('ingest URL 构造失败');
  }
  const { url } = logResult;
  const timeoutMs = opts.timeoutMs ?? HTTP_INGEST_DEFAULTS.requestTimeoutMs;
  // 透传 retry 参数：测试场景可显式覆盖 maxRetries/retryBaseMs；
  // 生产默认走 HTTP_INGEST_DEFAULTS（maxRetries=3, retryBaseMs=1000）。
  const retryOpts = {
    maxRetries: opts.maxRetries ?? HTTP_INGEST_DEFAULTS.maxRetries,
    retryBaseMs: opts.retryBaseMs ?? HTTP_INGEST_DEFAULTS.retryBaseMs,
  };
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  const startedAt = Date.now();
  let responsePayload = null;
  try {
    // 认证头（P0-A：token 走 Header 不进 URL）+ traceId（仅非空时添加，避免无谓 CORS preflight）
    const headers = buildAuthHeaders(token);
    if (opts.traceId) headers['X-Trace-Id'] = opts.traceId;
    const res = await fetchWithRetry(url, {
      method: 'POST',
      headers,
      body: JSON.stringify(body),
      signal: controller.signal,
    }, retryOpts);
    const text = await res.text();
    let json = null;
    try {
      json = text ? JSON.parse(text) : null;
    } catch (e) {
      log.error('ingest 响应 JSON 解析失败', e, { status: res.status, text: text.slice(0, 200) });
      throw new Error('ingest 响应非 JSON: ' + text.slice(0, 100), { cause: e });
    }
    responsePayload = json;
    return json;
  } catch (e) {
    log.error('ingest POST 失败', e, { url, elapsed_ms: Date.now() - startedAt });
    throw e;
  } finally {
    clearTimeout(timer);
    if (responsePayload) {
      const out = responsePayload.outbound_replies || [];
      log.info('ingest 响应', {
        url,
        ok: responsePayload.ok,
        elapsed_ms: Date.now() - startedAt,
        ingested_count: (responsePayload.ingested || []).length,
        outbound_replies_count: out.length,
        outbound_replies_preview: out.map((r) => ({
          channel: r && r.channel,
          conversation_id: r && r.conversation_id,
          content_preview: r && r.content ? r.content.slice(0, 100) : '',
          content_length: r && r.content ? r.content.length : 0,
          reply_to_event_id: r && r.reply_to_event_id,
        })),
        reason: responsePayload.reason,
      });
    }
  }
}


const OUTBOX_PATH = '/api/bridge/outbox';

async function getOutbox({ serverUrl, channel, accountId, token }, opts = {}) {
  const acct = accountId || 'default';
  const u = new URL(`${toHttpUrl(serverUrl)}${OUTBOX_PATH}`);
  u.searchParams.set('channel', channel || '');
  u.searchParams.set('account_id', acct);
  // 拉取条数：默认 50，可由配置覆盖（与后端 GetBridgeOutbox limit query 对齐）
  const batchSize = opts.batchSize && opts.batchSize > 0 ? opts.batchSize : 50;
  u.searchParams.set('limit', String(batchSize));
  const label = opts.label || '[HTTP outbox]';
  const timeoutMs = opts.timeoutMs ?? HTTP_INGEST_DEFAULTS.requestTimeoutMs;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  const startedAt = Date.now();
  try {
    const res = await fetchWithRetry(u.toString(), {
      method: 'GET',
      headers: buildAuthHeaders(token),
      signal: controller.signal,
    }, {
      maxRetries: opts.maxRetries ?? HTTP_INGEST_DEFAULTS.maxRetries,
      retryBaseMs: opts.retryBaseMs ?? HTTP_INGEST_DEFAULTS.retryBaseMs,
    });
    const text = await res.text();
    const json = text ? JSON.parse(text) : null;
    return json || { status: 'ok', messages: [] };
  } catch (e) {
    log.warn(`${label} 拉取下发队列失败`, { error: String(e && e.message || e), elapsed_ms: Date.now() - startedAt });
    return { status: 'error', messages: [] };
  } finally {
    clearTimeout(timer);
  }
}

async function ackOutbox({ serverUrl, channel, accountId, token }, msgIds, opts = {}) {
  const acct = accountId || 'default';
  const u = new URL(`${toHttpUrl(serverUrl)}${OUTBOX_PATH}/ack`);
  u.searchParams.set('channel', channel || '');
  u.searchParams.set('account_id', acct);
  const label = opts.label || '[HTTP outbox-ack]';
  const body = { [BRIDGE_PROTOCOL_V2.FIELD.MSG_IDS]: msgIds, [BRIDGE_PROTOCOL_V2.FIELD.STATUS]: BRIDGE_PROTOCOL_V2.TERMINAL.DELIVERED };
  const timeoutMs = opts.timeoutMs ?? HTTP_INGEST_DEFAULTS.requestTimeoutMs;
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMs);
  try {
    const res = await fetchWithRetry(u.toString(), {
      method: 'POST',
      headers: buildAuthHeaders(token),
      body: JSON.stringify(body),
      signal: controller.signal,
    }, {
      maxRetries: opts.maxRetries ?? HTTP_INGEST_DEFAULTS.maxRetries,
      retryBaseMs: opts.retryBaseMs ?? HTTP_INGEST_DEFAULTS.retryBaseMs,
    });
    const text = await res.text();
    const json = text ? JSON.parse(text) : null;
    return json || { status: 'ok' };
  } catch (e) {
    log.warn(`${label} 确认下发状态失败`, { error: String(e && e.message || e), msg_ids: msgIds });
    return { status: 'error' };
  } finally {
    clearTimeout(timer);
  }
}

export {
  INGEST_PATH,
  OUTBOX_PATH,
  HTTP_INGEST_DEFAULTS,
  buildIngestUrl,
  describeIngestParams,
  previewBody,
  _logRequest,
  fetchWithRetry,
  postIngest,
  getOutbox,
  ackOutbox,
};

