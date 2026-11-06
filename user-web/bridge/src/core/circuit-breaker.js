
import { createLogger } from './logger.js';

const log = createLogger('circuit', 'bridge');

class CircuitBreaker {
  constructor(opts = {}) {
    this.failureThreshold = opts.failureThreshold || 5;
    this.openDurationMs = opts.openDurationMs || 30000;
    this.halfOpenSuccessThreshold = opts.halfOpenSuccessThreshold || 1;
    // 时钟注入点（AV-08）：默认仍走全局 Date.now，生产行为不变；测试可注入受控时钟，
    // 使 100ms 窗口边界（t0+99/t0+100、重开窗口 t1+99/t1+100）可精确断言，不依赖真实 CPU/日志耗时。
    this._now = typeof opts.now === 'function' ? opts.now : () => Date.now();
    this.state = 'CLOSED';      
    this.failureCount = 0;
    this.halfOpenSuccessCount = 0;
    this.openedAt = 0;
    this.lastTransitionAt = 0;  
    this.lastSuccessAt = 0;
    this.lastFailureAt = 0;
    this._recentReasons = [];
    this._maxReasons = 5;
    this.deadManSeconds = opts.deadManSeconds || 300; 

    this._idempotencyKeys = new Map();
    this._idempotencyTtlMs = opts.idempotencyTtlMs ?? 5 * 60 * 1000; 
    this._idempotencyMax = opts.idempotencyMax ?? 5000; 
    this._idempotencyDedupeHits = 0; 

    this._latencyBuckets = []; 
    this._maxLatencySamples = opts.maxLatencySamples ?? 100;
    this._errorCodeDistribution = { '4xx': 0, '5xx': 0, 'net': 0, 'abort': 0, 'other': 0 };
    this._recentCalls = []; 
    this._maxRecentCalls = 50;
    this._totalCalls = 0;
    this._totalOk = 0;
    this._totalFail = 0;
  }

  isOpen() {
    if (this.state === 'OPEN') {
      if (this._now() - this.openedAt >= this.openDurationMs) {
        this._transitionTo('HALF_OPEN');
        return false;
      }
      return true;
    }
    return false;
  }

  registerIdempotency(key) {
    if (!key) return { accepted: true, deduped: false };
    const now = this._now();
    const existed = this._idempotencyKeys.get(key);
    if (existed && now - existed.at < this._idempotencyTtlMs) {
      this._idempotencyDedupeHits++;
      return { accepted: true, deduped: true, prev: existed };
    }
    this._idempotencyKeys.set(key, { at: now, status: 'pending' });
    if (this._idempotencyKeys.size > this._idempotencyMax) {
      const oldest = this._idempotencyKeys.keys().next().value;
      if (oldest) this._idempotencyKeys.delete(oldest);
    }
    this._pruneIdempotency(now);
    return { accepted: true, deduped: false };
  }

  markIdempotencyOk(key) {
    if (!key) return;
    const existed = this._idempotencyKeys.get(key);
    if (existed) this._idempotencyKeys.set(key, { ...existed, at: this._now(), status: 'ok' });
  }

  clearIdempotency(key) {
    if (!key) return;
    this._idempotencyKeys.delete(key);
  }

  _pruneIdempotency(now) {
    if (this._idempotencyKeys.size === 0) return;
    for (const [k, v] of this._idempotencyKeys.entries()) {
      if (now - v.at > this._idempotencyTtlMs) this._idempotencyKeys.delete(k);
    }
  }

  _recordCall(latencyMs, ok, err) {
    this._totalCalls++;
    if (ok) this._totalOk++; else this._totalFail++;
    this._latencyBuckets.push(latencyMs);
    if (this._latencyBuckets.length > this._maxLatencySamples) {
      this._latencyBuckets.shift();
    }
    if (!ok) {
      const code = this._classifyError(err);
      this._errorCodeDistribution[code] = (this._errorCodeDistribution[code] || 0) + 1;
    }
    this._recentCalls.push({
      at: this._now(),
      latencyMs,
      ok,
      errCode: ok ? null : this._classifyError(err),
      errMsg: ok ? null : (err && err.message ? String(err.message).slice(0, 200) : 'unknown'),
    });
    if (this._recentCalls.length > this._maxRecentCalls) this._recentCalls.shift();
  }

  _classifyError(err) {
    if (!err) return 'other';
    if (err.name === 'AbortError') return 'abort';
    if (err.circuitOpen) return 'other'; 
    const msg = String(err.message || '');
    if (/HTTP 4\d\d/.test(msg)) return '4xx';
    if (/HTTP 5\d\d/.test(msg)) return '5xx';
    if (/TypeError|Failed to fetch|NetworkError|net::/.test(msg)) return 'net';
    return 'other';
  }

  _transitionTo(next) {
    if (this.state === next) return;
    this.state = next;
    this.lastTransitionAt = this._now();
  }

  recordSuccess() {
    this.lastSuccessAt = this._now();
    if (this.state === 'HALF_OPEN') {
      this.halfOpenSuccessCount++;
      if (this.halfOpenSuccessCount >= this.halfOpenSuccessThreshold) {
        this._transitionTo('CLOSED');
        this.failureCount = 0;
        this.halfOpenSuccessCount = 0;
        this._recentReasons = [];
        log.info('熔断器关闭：探测成功，恢复正常请求');
      }
    } else if (this.state === 'CLOSED') {
      this.failureCount = 0; 
    }
  }

  recordFailure(err) {
    this.lastFailureAt = this._now();
    this.failureCount++;
    // 记录失败原因（去重后保留前 5）
    const reason = err && err.message ? String(err.message).slice(0, 200) : 'unknown';
    if (!this._recentReasons.includes(reason)) {
      this._recentReasons.unshift(reason);
      if (this._recentReasons.length > this._maxReasons) {
        this._recentReasons = this._recentReasons.slice(0, this._maxReasons);
      }
    }
    if (this.state === 'HALF_OPEN') {
      this._transitionTo('OPEN');
      this.openedAt = this._now();
      log.warn('熔断器探测失败，重新 OPEN', { reason });
    } else if (this.state === 'CLOSED' && this.failureCount >= this.failureThreshold) {
      this._transitionTo('OPEN');
      this.openedAt = this._now();
      log.warn(`熔断器开启：连续失败 ${this.failureCount} 次，${this.openDurationMs}ms 后探测`, {
        reasons: this._recentReasons,
      });
    }
  }

  _percentile(arr, p) {
    if (!arr || arr.length === 0) return 0;
    const sorted = [...arr].sort((a, b) => a - b);
    const idx = Math.min(sorted.length - 1, Math.floor(sorted.length * p));
    return sorted[idx];
  }

  snapshot() {
    const latency = this._latencyBuckets;
    const p50 = this._percentile(latency, 0.5);
    const p95 = this._percentile(latency, 0.95);
    const max = latency.length ? Math.max(...latency) : 0;
    const min = latency.length ? Math.min(...latency) : 0;
    const sum = latency.reduce((a, b) => a + b, 0);
    return {
      state: this.state,
      failureCount: this.failureCount,
      lastSuccessAt: this.lastSuccessAt,
      lastFailureAt: this.lastFailureAt,
      lastTransitionAt: this.lastTransitionAt,
      openedAt: this.openedAt,
      recentReasons: [...this._recentReasons],
      latencyMs: {
        p50, p95, max, min, sum,
        count: latency.length,
        avg: latency.length ? Math.round(sum / latency.length) : 0,
      },
      errorCodeDistribution: { ...this._errorCodeDistribution },
      totals: {
        calls: this._totalCalls,
        ok: this._totalOk,
        fail: this._totalFail,
        okRate: this._totalCalls ? Math.round((this._totalOk / this._totalCalls) * 10000) / 10000 : 0,
      },
      recentCalls: [...this._recentCalls],
      idempotency: {
        keysTracked: this._idempotencyKeys.size,
        dedupeHits: this._idempotencyDedupeHits,
        ttlMs: this._idempotencyTtlMs,
        max: this._idempotencyMax,
      },
      healthy: this.lastSuccessAt === 0
        ? (this.failureCount === 0)
        : (this._now() - this.lastSuccessAt < this.deadManSeconds * 1000),
      deadManSeconds: this.deadManSeconds,
    };
  }

  reset() {
    this.state = 'CLOSED';
    this.failureCount = 0;
    this.halfOpenSuccessCount = 0;
    this.openedAt = 0;
    this._recentReasons = [];
    this._idempotencyKeys.clear();
    this._idempotencyDedupeHits = 0;
    this._latencyBuckets = [];
    this._errorCodeDistribution = { '4xx': 0, '5xx': 0, 'net': 0, 'abort': 0, 'other': 0 };
    this._recentCalls = [];
    this._totalCalls = 0;
    this._totalOk = 0;
    this._totalFail = 0;
  }
}

// 全局单例（仅一份熔断器状态，整个 content script 共享）
export const circuitBreaker = new CircuitBreaker({
  failureThreshold: 5,
  openDurationMs: 30000,
  deadManSeconds: 300,
  idempotencyTtlMs: 5 * 60 * 1000,
  idempotencyMax: 5000,
  maxLatencySamples: 100,
});

export { CircuitBreaker };


