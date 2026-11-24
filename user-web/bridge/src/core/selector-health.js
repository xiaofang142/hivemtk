// selector-health.js — B1-a：桥接选择器「命中率」采集与上报（扩展侧）。
//
// 为什么需要它：桥接靠 DOM 选择器读写抖音/小红书/TikTok/闲鱼/快手 的页面。平台改版一次，
// 选择器就静默失效——扩展仍在跑、日志里没有异常，只是再也读不到消息。后端看到的
// 是「这个账号今天没客户」，与「客户今天没来」在指标上完全同形。
//
// 分工（与 user-server/internal/service/selector_health.go 一一对应）：
//   · 扩展侧只统计事实：某渠道某字段「试了几次、命中几次」，累计到一定量就报上去；
//   · 服务端做判定：阈值走参数中心（bridge.selector_health_degraded_below），改阈值
//     不需要发扩展版本。
// 本地那份 classifySelectorHealth 只用于自检页/日志，**不是**权威判定——两份口径
// 必须保持同一套规则，权威始终在服务端（否则各版本扩展会给出互相矛盾的结论）。
//
// 计数口径：
//   attempts = 调用定位逻辑的次数；hits = 其中拿到非空结果的次数。
//   只报命中数会让「从未被调用」与「一直失败」同形（都是 0 命中），
//   有分母才能区分「选择器坏了」与「这段路径没跑过」。

import { buildAuthHeaders } from './http-ingest.js';
import { TENANT_CODE } from './constants.js';

export const SELECTOR_HEALTH_PATH = '/api/bridge/selector-health';

// 与服务端 SelectorHealthMinAttempts / SelectorHealthDegradedBelow / 上报口上限同源。
export const SELECTOR_HEALTH_DEFAULTS = Object.freeze({
  // 低于该百分比判 degraded（服务端权威值来自参数中心，这里只是本地自检用的默认口径）
  degradedBelow: 60,
  // 样本不足时不判降级：刚打开页面时 0 命中是常态，不是平台改版
  minAttempts: 20,
  // 单次上报最多带几个字段（服务端 SelectorHealthMaxItemsPerReport = 32）
  maxItemsPerReport: 32,
  // 累计多少次尝试就发一次（发太勤＝每条消息一次请求；发太懒＝改版后很久才被发现）
  flushAfterAttempts: 50,
  // 单次请求超时
  requestTimeoutMs: 15000,
});

export const SELECTOR_HEALTH_STATUS = Object.freeze({
  ok: 'ok',
  degraded: 'degraded',
  broken: 'broken',
  warming: 'warming',
});

function normalizeThreshold(degradedBelow) {
  if (typeof degradedBelow !== 'number' || !Number.isFinite(degradedBelow)) {
    return SELECTOR_HEALTH_DEFAULTS.degradedBelow;
  }
  if (degradedBelow <= 0 || degradedBelow > 100) return SELECTOR_HEALTH_DEFAULTS.degradedBelow;
  return degradedBelow;
}

/**
 * 命中率（0–100，一位小数）。attempts 为 0 时返回 0（而不是 NaN/Infinity）。
 * 保留一位小数与服务端 selectorHitRate 同口径，避免两处显示不一致。
 */
export function selectorHitRate(attempts, hits) {
  const a = Number(attempts) || 0;
  const h = Number(hits) || 0;
  if (a <= 0) return 0;
  return Math.round((h * 100 * 10) / a) / 10;
}

/**
 * 本地判定（与服务端 ClassifySelectorHealth 同一套顺序：先样本、再全零、最后命中率）。
 * 反过来先判命中率时，样本 1 次 0 命中的行会直接判 broken——那正是新装扩展首次打开页面的常态。
 */
export function classifySelectorHealth(attempts, hits, degradedBelow = SELECTOR_HEALTH_DEFAULTS.degradedBelow) {
  const a = Number(attempts) || 0;
  const h = Number(hits) || 0;
  if (a < SELECTOR_HEALTH_DEFAULTS.minAttempts) return SELECTOR_HEALTH_STATUS.warming;
  if (h <= 0) return SELECTOR_HEALTH_STATUS.broken;
  if (selectorHitRate(a, h) < normalizeThreshold(degradedBelow)) return SELECTOR_HEALTH_STATUS.degraded;
  return SELECTOR_HEALTH_STATUS.ok;
}

/**
 * 计数器：按 (channel, field) 累计 attempts/hits。
 *
 * 这层刻意做成无 IO 的纯对象：content script 里每 3 秒就会调一次定位逻辑，
 * 任何在里面做存储/网络的分支都会变成热路径上的抖动来源。
 */
export function createSelectorHealthTracker(opts = {}) {
  const flushAfterAttempts = Number(opts.flushAfterAttempts) > 0
    ? Number(opts.flushAfterAttempts)
    : SELECTOR_HEALTH_DEFAULTS.flushAfterAttempts;
  const maxItemsPerReport = Number(opts.maxItemsPerReport) > 0
    ? Math.min(Number(opts.maxItemsPerReport), SELECTOR_HEALTH_DEFAULTS.maxItemsPerReport)
    : SELECTOR_HEALTH_DEFAULTS.maxItemsPerReport;

  /** @type {Map<string, Map<string, {attempts:number, hits:number}>>} */
  const byChannel = new Map();

  function bucket(channel) {
    if (!byChannel.has(channel)) byChannel.set(channel, new Map());
    return byChannel.get(channel);
  }

  return {
    /** record 一次定位尝试。hit 表示这次是否拿到非空结果。 */
    record(channel, field, hit) {
      if (!channel || !field) return;
      const fields = bucket(channel);
      const cur = fields.get(field) || { attempts: 0, hits: 0 };
      cur.attempts += 1;
      if (hit) cur.hits += 1;
      fields.set(field, cur);
    },

    /** 该渠道累计的尝试次数（用于判断「够不够一次上报」）。 */
    attemptsOf(channel) {
      let n = 0;
      for (const v of (byChannel.get(channel) || new Map()).values()) n += v.attempts;
      return n;
    },

    /** 是否已经累计到该上报的量。 */
    shouldFlush(channel) {
      return this.attemptsOf(channel) >= flushAfterAttempts;
    },

    /**
     * 取出并清零该渠道的计数（上报用）。
     * 返回 { items:[{field,attempts,hits}], attempts }；没有数据时 items 为空数组。
     */
    drain(channel) {
      const fields = byChannel.get(channel);
      if (!fields || fields.size === 0) return { items: [], attempts: 0 };
      const items = [];
      let attempts = 0;
      for (const [field, v] of fields) {
        if (v.attempts <= 0) continue;
        attempts += v.attempts;
        items.push({ field, attempts: v.attempts, hits: v.hits });
      }
      byChannel.delete(channel);
      // 字段数上限：正常只有 6 个字段（SELECTOR_FIELDS），超出说明调用方传了脏 field，
      // 截断而不是整包丢掉（截断仍能上报主要字段，丢掉等于这次采样白采）。
      return { items: items.slice(0, maxItemsPerReport), attempts };
    },

    /** 只读快照（自检页展示用，不清零）。 */
    snapshot(channel) {
      const out = [];
      for (const [field, v] of (byChannel.get(channel) || new Map())) {
        out.push({
          field,
          attempts: v.attempts,
          hits: v.hits,
          hit_rate: selectorHitRate(v.attempts, v.hits),
          status: classifySelectorHealth(v.attempts, v.hits, opts.degradedBelow),
        });
      }
      return out;
    },

    /** 测试/重连用：清空全部计数。 */
    reset() { byChannel.clear(); },
  };
}

/**
 * 合并两批 items（background 侧把多个 content script 的上报合到一条渠道上）。
 * 同名 field 的 attempts/hits 相加，不改入参（返回新数组）。
 */
export function mergeSelectorHealthItems(base, added) {
  const map = new Map();
  for (const it of Array.isArray(base) ? base : []) {
    if (!it || !it.field) continue;
    map.set(it.field, { field: it.field, attempts: Number(it.attempts) || 0, hits: Number(it.hits) || 0 });
  }
  for (const it of Array.isArray(added) ? added : []) {
    if (!it || !it.field) continue;
    const cur = map.get(it.field) || { field: it.field, attempts: 0, hits: 0 };
    cur.attempts += Number(it.attempts) || 0;
    cur.hits += Number(it.hits) || 0;
    map.set(it.field, cur);
  }
  return Array.from(map.values());
}

/**
 * 从 pending 里减掉「已经成功上报的那一批」。
 *
 * 为什么需要减而不是「发完就清空」：上报是异步的，POST 期间 content script 还在继续
 * 计数；直接清空会把这段时间的观测一起丢掉（丢分母 ⇒ 命中率偏高 ⇒ 恰好掩盖问题）。
 * 减到 0 为止（钳位），负数是无意义状态。
 */
export function subtractSelectorHealthItems(base, sent) {
  const spent = new Map();
  for (const it of Array.isArray(sent) ? sent : []) {
    if (it && it.field) spent.set(it.field, it);
  }
  const out = [];
  for (const it of Array.isArray(base) ? base : []) {
    if (!it || !it.field) continue;
    const s = spent.get(it.field);
    const attempts = (Number(it.attempts) || 0) - (Number(s && s.attempts) || 0);
    const hits = (Number(it.hits) || 0) - (Number(s && s.hits) || 0);
    if (attempts <= 0) continue;
    out.push({ field: it.field, attempts, hits: Math.max(0, Math.min(hits, attempts)) });
  }
  return out;
}

function toHttpUrl(serverUrl) {
  return String(serverUrl || '').replace(/^ws/, 'http');
}

/**
 * 构造上报请求（纯函数，便于用例断言 URL/头/体，不碰网络）。
 *
 * 识别码走 Header（X-Channel-Identify-Code + Authorization），与 http-ingest.js 同一条铁律：
 * 服务端 `/api/bridge/*` 的守卫只认 X-Channel-Identify-Code（或 identify_code 查询参数），
 * 旧 X-Bridge-Token 已退役，只发 Bearer 会被 401，而下行/上报都把 401 吞成一行日志。
 */
export function buildSelectorHealthRequest({
  serverUrl,
  token,
  tenantCode,
  channel,
  accountId,
  items,
  timeoutMs = SELECTOR_HEALTH_DEFAULTS.requestTimeoutMs,
} = {}) {
  if (!serverUrl) throw new Error('selector health: serverUrl 为空');
  if (!channel) throw new Error('selector health: channel 为空');
  if (!Array.isArray(items) || items.length === 0) {
    throw new Error('selector health: items 为空');
  }
  const url = new URL(`${toHttpUrl(serverUrl)}${SELECTOR_HEALTH_PATH}`);
  const code = String(tenantCode || '').trim();
  if (code) url.searchParams.set(TENANT_CODE.QUERY_PARAM, code);
  const body = {
    channel,
    account_id: accountId || '',
    items: items.slice(0, SELECTOR_HEALTH_DEFAULTS.maxItemsPerReport),
  };
  return {
    url: url.toString(),
    body,
    timeoutMs,
    init: {
      method: 'POST',
      headers: buildAuthHeaders(token),
      body: JSON.stringify(body),
    },
  };
}

/**
 * 发一次上报。返回 { ok, status, error?, degraded?, items? }——**不抛**：
 * 健康上报失败不该影响消息收发（这是观测面，不是业务面）。
 * 失败时保留 pending（调用方据此决定是否重试），因此这里绝不吞掉「没发成功」这件事。
 */
export async function postSelectorHealth(req, fetchImpl) {
  const doFetch = fetchImpl || (typeof fetch === 'function' ? fetch : null);
  if (!doFetch) return { ok: false, status: 0, error: 'fetch unavailable' };
  const ctrl = typeof AbortController === 'function' ? new AbortController() : null;
  let timer = null;
  if (ctrl && req.timeoutMs > 0) {
    timer = setTimeout(() => { try { ctrl.abort(); } catch (_) {  } }, req.timeoutMs);
  }
  try {
    const res = await doFetch(req.url, ctrl ? { ...req.init, signal: ctrl.signal } : req.init);
    let payload = null;
    try { payload = await res.json(); } catch (_) { payload = null; }
    if (!res.ok) {
      return {
        ok: false,
        status: res.status,
        error: (payload && payload.error) || `HTTP ${res.status}`,
      };
    }
    return {
      ok: true,
      status: res.status,
      degraded: (payload && payload.degraded) || [],
      items: (payload && payload.items) || [],
      threshold: payload && payload.threshold,
    };
  } catch (e) {
    return { ok: false, status: 0, error: e && e.message ? e.message : String(e) };
  } finally {
    if (timer) clearTimeout(timer);
  }
}

export const _internal = { normalizeThreshold, toHttpUrl, SELECTOR_HEALTH_DEFAULTS };
