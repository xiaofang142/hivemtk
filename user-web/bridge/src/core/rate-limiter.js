
import { RATE_LIMIT_DEFAULTS } from './constants.js';

class TokenBucket {
  constructor(capacity, refillPerMin) {
    this.capacity = capacity;
    this.refillPerMs = refillPerMin / 60000;
    this.tokens = capacity;
    this.last = Date.now();
  }
  tryAcquire(n = 1) {
    const now = Date.now();
    this.tokens = Math.min(this.capacity, this.tokens + (now - this.last) * this.refillPerMs);
    this.last = now;
    if (this.tokens >= n) {
      this.tokens -= n;
      return true;
    }
    return false;
  }
  retryAfterMs() {
    if (this.tokens >= 1) return 0;
    return Math.ceil((1 - this.tokens) / this.refillPerMs);
  }
}

function hashStr(s) {
  let h = 0;
  for (let i = 0; i < s.length; i++) h = (Math.imul(31, h) + s.charCodeAt(i)) | 0;
  return (h >>> 0).toString(36);
}

// 2026-08-15 P1-F：桶上限与 TTL（LRU 淘汰阈值）
const ACCOUNT_BUCKETS_MAX = 200;
const CONV_BUCKETS_MAX = 1000;
const CONV_BUCKETS_TTL_MS = 30 * 24 * 3600 * 1000;

export class RateLimiter {
  constructor(cfg = {}) {
    this.cfg = { ...RATE_LIMIT_DEFAULTS, ...cfg };
    this.accountBuckets = new Map();   
    this.convBuckets = new Map();      
    this.lastGlobalSendAt = 0;
  }

  _accountKey(channel, account) { return `${channel}:${account}`; }
  _convKey(channel, account, conv) { return `${channel}:${account}:${conv || '_'}`; }

  // Map 的 delete+re-set 把 key 移到队尾 = 标记为最近访问（LRU）。
  _touch(map, key) {
    if (map.has(key)) {
      const v = map.get(key);
      map.delete(key);
      map.set(key, v);
    }
  }

  _accountBucket(channel, account) {
    const k = this._accountKey(channel, account);
    if (!this.accountBuckets.has(k)) {
      this.accountBuckets.set(k, new TokenBucket(this.cfg.accountCapacity, this.cfg.accountRefillPerMin));
      this._evictIfNeeded();
    } else {
      this._touch(this.accountBuckets, k);
    }
    return this.accountBuckets.get(k);
  }

  _convState(channel, account, conv) {
    const k = this._convKey(channel, account, conv);
    if (!this.convBuckets.has(k)) {
      this.convBuckets.set(k, {
        bucket: new TokenBucket(this.cfg.conversationPerHour, this.cfg.conversationPerHour),
        hourly: [],
        lastSentAt: 0,
        lastHash: '',
        lastHashAt: 0,
        lastAccessAt: Date.now(),
      });
      this._evictIfNeeded();
    } else {
      const cs = this.convBuckets.get(k);
      cs.lastAccessAt = Date.now();
      this._touch(this.convBuckets, k);
    }
    return this.convBuckets.get(k);
  }

  // P1-F：accountBuckets 超 200 → LRU 淘汰最久未访问；
  // convBuckets 超 1000 → 先按 TTL（30 天未访问）淘汰，再 LRU。
  _evictIfNeeded() {
    while (this.accountBuckets.size > ACCOUNT_BUCKETS_MAX) {
      const oldest = this.accountBuckets.keys().next().value;
      if (oldest === undefined) break;
      this.accountBuckets.delete(oldest);
    }
    if (this.convBuckets.size > CONV_BUCKETS_MAX) {
      const now = Date.now();
      for (const [k, cs] of this.convBuckets) {
        if (now - (cs.lastAccessAt || 0) > CONV_BUCKETS_TTL_MS) this.convBuckets.delete(k);
      }
    }
    while (this.convBuckets.size > CONV_BUCKETS_MAX) {
      const oldest = this.convBuckets.keys().next().value;
      if (oldest === undefined) break;
      this.convBuckets.delete(oldest);
    }
  }

  // P1-F：暴露桶统计（账户桶/会话桶数量 + 上限 + TTL），供状态面板与巡检诊断。
  bucketStats() {
    return {
      accountBuckets: this.accountBuckets.size,
      accountBucketsMax: ACCOUNT_BUCKETS_MAX,
      convBuckets: this.convBuckets.size,
      convBucketsMax: CONV_BUCKETS_MAX,
      convBucketsTtlMs: CONV_BUCKETS_TTL_MS,
    };
  }

  // 测试/自检辅助：清空所有状态
  __reset() {
    this.accountBuckets.clear();
    this.convBuckets.clear();
    this.lastGlobalSendAt = 0;
  }

  tryAcquire(channel, account, conversation, text) {
    const now = Date.now();
    const reasons = [];

    // —— 层1：拟人节奏（最小全局间隔）——
    const sinceLast = now - this.lastGlobalSendAt;
    if (sinceLast < this.cfg.minIntervalMs) {
      reasons.push(`minInterval ${this.cfg.minIntervalMs}ms`);
    }

    // —— 层2a：单账号 Token 桶 ——
    const ab = this._accountBucket(channel, account);
    const accountOk = ab.tryAcquire(1);
    if (!accountOk) reasons.push(`account bucket(${this.cfg.accountCapacity}/min)`);

    // —— 层2b：单会话每小时桶 ——
    const cs = this._convState(channel, account, conversation);
    cs.hourly = cs.hourly.filter((t) => now - t < 3600000);
    const convOk = cs.bucket.tryAcquire(1);
    if (!convOk || cs.hourly.length >= this.cfg.conversationPerHour) {
      reasons.push(`conversation ${this.cfg.conversationPerHour}/hour`);
    }

    // —— 层3a：会话冷却（防回环/刷屏）——
    const cooldownOk = now - cs.lastSentAt >= this.cfg.conversationCooldownMs;
    if (!cooldownOk) reasons.push(`cooldown ${this.cfg.conversationCooldownMs}ms`);

    // —— 层3b：相同文案去重 ——
    let dedupOk = true;
    if (text) {
      const h = hashStr(text);
      if (h === cs.lastHash && now - cs.lastHashAt < this.cfg.dedupWindowMs) {
        dedupOk = false;
        reasons.push('dedup same text');
      }
    }

    if (reasons.length || !accountOk || !convOk || !cooldownOk || !dedupOk) {
      if (accountOk) ab.tokens = Math.min(ab.capacity, ab.tokens + 1);
      if (convOk) cs.bucket.tokens = Math.min(cs.bucket.capacity, cs.bucket.tokens + 1);
      return {
        allowed: false,
        reason: reasons.join('; ') || 'blocked',
        retryAfterMs: ab.retryAfterMs(),
        waitHintMs: 0,
      };
    }

    // 计算拟人延迟（jitter + 补足最小间隔）
    const waitHintMs = Math.max(
      this.cfg.minIntervalMs - sinceLast,
      this.cfg.jitterMinMs + Math.random() * (this.cfg.jitterMaxMs - this.cfg.jitterMinMs)
    );
    return { allowed: true, reason: 'ok', retryAfterMs: 0, waitHintMs: Math.round(waitHintMs) };
  }

  markSent(channel, account, conversation, text) {
    const now = Date.now();
    this.lastGlobalSendAt = now;
    const cs = this._convState(channel, account, conversation);
    cs.lastSentAt = now;
    cs.hourly.push(now);
    if (text) {
      cs.lastHash = hashStr(text);
      cs.lastHashAt = now;
    }
  }
}

// ── B6（批3）跨上下文全局发送闸 ──────────────────────────────────────
// 每渠道 content script 是独立 JS 上下文：内存 lastGlobalSendAt 跨 tab/跨渠道
// 互不可见，"层1·拟人最小间隔"实际只约束单 tab。以 chrome.storage.local 为
// 共享时钟：发送前读取全局最近发送时刻补足 minIntervalMs，发送成功后写回。
// best-effort 无原子锁（限流目标是反机器人节奏而非精确调度，毫秒级竞争可接受）；
// storage 不可用/读写失败 → 静默降级为单 tab 语义（返回 0，不阻塞发送）。
export const GLOBAL_SEND_AT_KEY = 'mtk_global_last_send_at';

function storageLocal() {
  try {
    if (typeof chrome !== 'undefined' && chrome.storage && chrome.storage.local) return chrome.storage.local;
  } catch (_) { }
  return null;
}

export async function readGlobalLastSendAt() {
  const s = storageLocal();
  if (!s) return 0;
  try {
    const r = await s.get(GLOBAL_SEND_AT_KEY);
    const v = r && r[GLOBAL_SEND_AT_KEY];
    // 未来时间戳（其他设备时钟漂移）视为无效，防永久自锁
    return typeof v === 'number' && v > 0 && v <= Date.now() + 5000 ? v : 0;
  } catch (_) {
    return 0;
  }
}

export async function stampGlobalSendAt(at = Date.now()) {
  const s = storageLocal();
  if (!s) return;
  try {
    await s.set({ [GLOBAL_SEND_AT_KEY]: at });
  } catch (_) { }
}

export async function globalSendWaitMs(minIntervalMs) {
  if (!minIntervalMs || minIntervalMs <= 0) return 0;
  const last = await readGlobalLastSendAt();
  if (!last) return 0;
  const wait = minIntervalMs - (Date.now() - last);
  return wait > 0 ? Math.round(wait) : 0;
}

