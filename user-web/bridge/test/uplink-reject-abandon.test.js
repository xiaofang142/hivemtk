// M-1（2026-11-11）上报 ack 收口：服务端明确拒绝的消息不得无限重传。
// 三类响应结果分账：accepted / duplicate → confirmed（终态）；
// rejected（accepted=false 且非 duplicate）→ abandoned 放弃集合（不回插、不再上行）；
// HTTP 4xx（fetchWithRetry 标 nonRetryable）→ 整批放弃不回插；
// 仅瞬态故障（网络失败/5xx/429/408/超时）保留 B4 回插重传。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { Uplink } from '../src/core/uplink.js';

const msg = (i) => ({
  channel: 'douyin',
  account_id: 'acc-1',
  conversation_id: 'conv-1',
  content: `m${i}`,
  timestamp: Date.now(),
});

describe('uplink 服务端明确拒绝 → 放弃集合（不无限重传）', () => {
  let realFetch;
  beforeEach(() => { realFetch = globalThis.fetch; });
  afterEach(() => { globalThis.fetch = realFetch; vi.restoreAllMocks(); });

  it('200 + 逐条结果：accepted/duplicate 入 confirmed，rejected 入 abandoned 并计数', async () => {
    let seen = null;
    globalThis.fetch = vi.fn(async (_u, init) => {
      seen = JSON.parse(init.body);
      return new Response(JSON.stringify({
        ok: true,
        ingested: seen.messages.map((m, i) => (
          i === 0
            ? { event_id: m.event_id, accepted: true }
            : i === 1
              ? { event_id: m.event_id, duplicate: true }
              : { event_id: m.event_id, accepted: false, reason: 'tenant unresolved' }
        )),
      }), { status: 200 });
    });
    const up = new Uplink({ channel: 'douyin', getConfig: async () => ({}) });
    up.enqueue(msg(0));
    up.enqueue(msg(1));
    up.enqueue(msg(2));
    await up.flushAll();
    const ids = seen.messages.map((m) => m.event_id);
    expect(ids).toHaveLength(3);
    expect(up._confirmed.has(ids[0])).toBe(true);
    expect(up._confirmed.has(ids[1])).toBe(true);
    expect(up._abandoned.has(ids[2])).toBe(true);
    expect(up.metrics.rejectedDropped).toBe(1);
    expect(up.metrics.requeued).toBe(0);
  });

  it('rejected event_id 再次 enqueue（重新捕获）不再上行', async () => {
    let fetchCount = 0;
    let seen = null;
    globalThis.fetch = vi.fn(async (_u, init) => {
      fetchCount += 1;
      seen = JSON.parse(init.body);
      return new Response(JSON.stringify({
        ok: true,
        ingested: seen.messages.map((m) => ({ event_id: m.event_id, accepted: false, reason: 'invalid' })),
      }), { status: 200 });
    });
    const up = new Uplink({ channel: 'douyin', getConfig: async () => ({}) });
    up.enqueue(msg(0));
    await up.flushAll();
    expect(fetchCount).toBe(1);
    expect(up._abandoned.has(seen.messages[0].event_id)).toBe(true);
    // 同内容 → 同 event_id（前端 hash 幂等），模拟重新捕获后再次入队
    up.enqueue(msg(0));
    await up.flushAll();
    expect(fetchCount).toBe(1); // 终态跳过，未发第二次请求
  });

  it('HTTP 400（参数违规，nonRetryable）→ 整批放弃不回插，后续 flush 不再请求', async () => {
    let fetchCount = 0;
    globalThis.fetch = vi.fn(async () => {
      fetchCount += 1;
      return new Response('{"ok":false,"reason":"messages required"}', { status: 400 });
    });
    const up = new Uplink({ channel: 'douyin', getConfig: async () => ({}) });
    up.enqueue(msg(0));
    up.enqueue(msg(1));
    await up.flushAll();
    expect(fetchCount).toBe(1);
    expect(up.buffers.has('acc-1|conv-1')).toBe(false); // 不回插
    expect(up.metrics.rejectedDropped).toBe(2);
    // 再触发一次 flush：buffer 已空，无第二次请求（不再打无效请求）
    await up.flushAll();
    expect(fetchCount).toBe(1);
  });

  it('瞬态故障（网络失败）仍走 B4 回插重传（有界、可恢复）', async () => {
    globalThis.fetch = vi.fn(async () => { throw new TypeError('Failed to fetch'); });
    const up = new Uplink({ channel: 'douyin', getConfig: async () => ({}), retryOpts: { maxRetries: 0, retryBaseMs: 1 } });
    up.enqueue(msg(0));
    await up.flushAll();
    expect(up.buffers.get('acc-1|conv-1')?.items.length).toBe(1); // 回插保留待重传
    expect(up.metrics.requeued).toBe(1);
    expect(up.metrics.rejectedDropped).toBe(0);
  });

  it('abandoned 持久化：跨实例（页面刷新）后同 event_id 仍不上行', async () => {
    const store = {};
    globalThis.chrome = {
      storage: {
        local: {
          get: async (k) => ({ [k]: store[k] || [] }),
          set: async (obj) => { Object.assign(store, obj); },
        },
      },
    };
    try {
      let seen = null;
      globalThis.fetch = vi.fn(async (_u, init) => {
        seen = JSON.parse(init.body);
        return new Response(JSON.stringify({
          ok: true,
          ingested: seen.messages.map((m) => ({ event_id: m.event_id, accepted: false, reason: 'invalid' })),
        }), { status: 200 });
      });
      const up1 = new Uplink({ channel: 'douyin', getConfig: async () => ({}) });
      up1.enqueue(msg(0));
      await up1.flushAll();
      const rejectedId = seen.messages[0].event_id;
      expect(store.mtk_uplink_abandoned).toContain(rejectedId);
      // 新实例（模拟页面刷新后重建）：同 event_id 直接跳过
      globalThis.fetch = vi.fn(async () => { throw new Error('should not be called'); });
      const up2 = new Uplink({ channel: 'douyin', getConfig: async () => ({}) });
      up2.enqueue(msg(0));
      await up2.flushAll();
      expect(up2.buffers.has('acc-1|conv-1')).toBe(false);
    } finally {
      delete globalThis.chrome;
    }
  });
});
