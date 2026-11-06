import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { CircuitBreaker, circuitBreaker } from '../src/core/circuit-breaker.js';

// 固定非零基准 t0（AV-08 可控时钟）：TTL/窗口判定随显式推进走，不用真实 sleep。
const T0 = 1_700_000_000_000;

// 每个用例结束后恢复真实时钟（等价 finally，保证不泄漏到其他测试文件）
afterEach(() => {
  vi.useRealTimers();
});

describe('CircuitBreaker 幂等键（P3-A 2026-08-15）', () => {
  let cb;
  beforeEach(() => {
    cb = new CircuitBreaker({ failureThreshold: 5, openDurationMs: 100, idempotencyTtlMs: 1000, idempotencyMax: 100 });
  });

  it('registerIdempotency 新 key → deduped=false', () => {
    const r = cb.registerIdempotency('key-1');
    expect(r.accepted).toBe(true);
    expect(r.deduped).toBe(false);
    expect(cb._idempotencyKeys.size).toBe(1);
  });

  it('同 key 重复 register：pending → ok → 命中', () => {
    const r1 = cb.registerIdempotency('key-1');
    expect(r1.deduped).toBe(false);
    cb.markIdempotencyOk('key-1');
    const r2 = cb.registerIdempotency('key-1');
    expect(r2.deduped).toBe(true);
    expect(r2.prev.status).toBe('ok');
    expect(cb._idempotencyDedupeHits).toBe(1);
  });

  it('TTL 外同 key 视为新（受控时钟，无真实 sleep）', () => {
    vi.useFakeTimers({ now: T0, toFake: ['Date'] });
    cb.registerIdempotency('key-1');
    cb.markIdempotencyOk('key-1');
    vi.advanceTimersByTime(50); // TTL(1000) 内：仍命中
    expect(cb.registerIdempotency('key-1').deduped).toBe(true);
    const orig = cb._idempotencyTtlMs;
    cb._idempotencyTtlMs = 10;
    try {
      vi.advanceTimersByTime(30); // 距注册/ok 时刻累计 80ms > 10ms TTL
      const r = cb.registerIdempotency('key-1');
      expect(r.deduped).toBe(false);
    } finally {
      cb._idempotencyTtlMs = orig;
    }
  });

  it('clearIdempotency 后可重发', () => {
    cb.registerIdempotency('key-1');
    cb.markIdempotencyOk('key-1');
    cb.clearIdempotency('key-1');
    const r = cb.registerIdempotency('key-1');
    expect(r.deduped).toBe(false);
  });

  it('容量上限：触顶时按插入顺序淘汰', () => {
    for (let i = 0; i < 105; i++) {
      cb.registerIdempotency(`k-${i}`);
    }
    expect(cb._idempotencyKeys.size).toBeLessThanOrEqual(100);
  });

  it('空 key 静默通过', () => {
    const r = cb.registerIdempotency('');
    expect(r.accepted).toBe(true);
    expect(r.deduped).toBe(false);
    expect(cb._idempotencyKeys.size).toBe(0);
  });
});

describe('CircuitBreaker 结构化健康度（P3-C 2026-08-15）', () => {
  let cb;
  beforeEach(() => {
    cb = new CircuitBreaker({ failureThreshold: 5, openDurationMs: 100, maxLatencySamples: 10 });
  });

  it('_recordCall 累计 calls/ok/fail', () => {
    cb._recordCall(100, true, null);
    cb._recordCall(200, true, null);
    cb._recordCall(50, false, new Error('HTTP 500'));
    const s = cb.snapshot();
    expect(s.totals.calls).toBe(3);
    expect(s.totals.ok).toBe(2);
    expect(s.totals.fail).toBe(1);
    expect(s.totals.okRate).toBeCloseTo(0.6667, 3);
  });

  it('_classifyError 正确分类 4xx/5xx/net/abort/other', () => {
    expect(cb._classifyError(new Error('HTTP 404 Not Found'))).toBe('4xx');
    expect(cb._classifyError(new Error('HTTP 500 Internal Server Error'))).toBe('5xx');
    expect(cb._classifyError(new Error('Failed to fetch'))).toBe('net');
    expect(cb._classifyError(new Error('NetworkError when attempting to fetch'))).toBe('net');
    const abort = new Error('aborted');
    abort.name = 'AbortError';
    expect(cb._classifyError(abort)).toBe('abort');
    expect(cb._classifyError(new Error('circuit breaker open'))).toBe('other');
    expect(cb._classifyError(null)).toBe('other');
  });

  it('P50/P95 计算正确', () => {
    const samples = [10, 20, 30, 40, 50, 60, 70, 80, 90, 100];
    for (const s of samples) cb._recordCall(s, true, null);
    const snap = cb.snapshot();
    expect(snap.latencyMs.p50).toBe(60);
    expect(snap.latencyMs.p95).toBe(100);
    expect(snap.latencyMs.min).toBe(10);
    expect(snap.latencyMs.max).toBe(100);
    expect(snap.latencyMs.avg).toBe(55);
  });

  it('errorCodeDistribution 正确累加', () => {
    cb._recordCall(0, false, new Error('HTTP 500'));
    cb._recordCall(0, false, new Error('HTTP 502'));
    cb._recordCall(0, false, new Error('HTTP 404'));
    cb._recordCall(0, false, new Error('Failed to fetch'));
    cb._recordCall(0, false, new Error('HTTP 500'));
    const s = cb.snapshot();
    expect(s.errorCodeDistribution['5xx']).toBe(3);
    expect(s.errorCodeDistribution['4xx']).toBe(1);
    expect(s.errorCodeDistribution['net']).toBe(1);
  });

  it('recentCalls 滚动窗口：最多保留 maxRecentCalls', () => {
    for (let i = 0; i < 60; i++) {
      cb._recordCall(i, true, null);
    }
    expect(cb._recentCalls.length).toBe(50);
  });

  it('snapshot 包含 idempotency 统计', () => {
    cb.registerIdempotency('k1');
    cb.registerIdempotency('k2');
    cb.markIdempotencyOk('k1');
    cb.registerIdempotency('k1');
    cb.registerIdempotency('k1');
    const s = cb.snapshot();
    expect(s.idempotency.keysTracked).toBe(2);
    expect(s.idempotency.dedupeHits).toBe(2);
    expect(s.idempotency.ttlMs).toBeGreaterThan(0);
    expect(s.idempotency.max).toBeGreaterThan(0);
  });

  it('_transitionTo 记录 lastTransitionAt', () => {
    expect(cb.lastTransitionAt).toBe(0);
    cb._transitionTo('OPEN');
    expect(cb.lastTransitionAt).toBeGreaterThan(0);
    const t1 = cb.lastTransitionAt;
    cb._transitionTo('HALF_OPEN');
    expect(cb.lastTransitionAt).toBeGreaterThanOrEqual(t1);
  });
});

describe('CircuitBreaker 三态机（P3-C 回归，AV-08 可控时钟）', () => {
  let cb;
  beforeEach(() => {
    vi.useFakeTimers({ now: T0, toFake: ['Date'] });
    cb = new CircuitBreaker({ failureThreshold: 3, openDurationMs: 100, deadManSeconds: 1 });
  });
  it('CLOSED 3 次失败 → OPEN（t0 开启瞬间）', () => {
    cb.recordFailure(new Error('e1'));
    cb.recordFailure(new Error('e2'));
    cb.recordFailure(new Error('e3'));
    expect(cb.state).toBe('OPEN');
    expect(cb.openedAt).toBe(T0);
    expect(cb.isOpen()).toBe(true);
  });
  it('OPEN openDurationMs 后 → HALF_OPEN（t0+100 调 isOpen() 才转态，不自动转）', () => {
    cb.recordFailure(new Error('e1'));
    cb.recordFailure(new Error('e2'));
    cb.recordFailure(new Error('e3'));
    vi.setSystemTime(T0 + 100);
    expect(cb.state).toBe('OPEN'); // 未调 isOpen() 前不自动转态
    expect(cb.isOpen()).toBe(false); // 调用瞬间才判定并转换
    expect(cb.state).toBe('HALF_OPEN');
  });
  it('reset() 完整重置（含 idempotency + P3-C 字段）', () => {
    cb._recordCall(100, true, null);
    cb._recordCall(0, false, new Error('e'));
    cb.registerIdempotency('k1');
    cb.failureCount = 3;
    cb.state = 'OPEN';
    cb.reset();
    expect(cb.state).toBe('CLOSED');
    expect(cb._latencyBuckets.length).toBe(0);
    expect(cb._idempotencyKeys.size).toBe(0);
    expect(cb._totalCalls).toBe(0);
  });
});
