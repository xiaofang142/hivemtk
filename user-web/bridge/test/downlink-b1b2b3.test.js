// 批1（2026-09-19）数据正确性回归测试：B1 SSE 键解析 / B2 ack v2 会话归属 / B3 SSE 降级重连。
// 对应 spec：docs/superpowers/specs/2026-09-19-browser-automation-optimization-design.md
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

function makeChromeStorage() {
  const data = {};
  return {
    storage: {
      local: {
        get: vi.fn((keys) => {
          const out = {};
          const arr = Array.isArray(keys) ? keys : [keys];
          for (const k of arr) if (k in data) out[k] = data[k];
          return Promise.resolve(out);
        }),
        set: vi.fn((obj) => { Object.assign(data, obj); return Promise.resolve(); }),
      },
    },
    runtime: { sendMessage: vi.fn(async () => {}) },
    _data: data,
  };
}

vi.mock('../src/core/http-ingest.js', async (importOriginal) => {
  const actual = await importOriginal();
  return { ...actual, getOutbox: vi.fn(), ackOutbox: vi.fn() };
});
vi.mock('../src/core/sanitize.js', () => ({
  sanitizeForDisplay: (t) => t,
}));
vi.mock('../src/core/sse-fetch-client.js', () => ({
  connectSSE: vi.fn(async () => { throw new Error('net down'); }),
  getLastEventID: vi.fn(() => ''),
  setLastEventID: vi.fn(),
  closeAll: vi.fn(),
  // B5（批3）：teardown 现在会调 stopSSE 唤醒等待体，缺此导出会抛 mock 未定义
  stopSSE: vi.fn(),
}));

import {
  pollDownlink,
  initDownlink,
  addPendingAck,
  claimDuePendingAck,
  resolveSSEOutboundKeys,
  startSSEDelivery,
} from '../src/core/downlink.js';
import { getOutbox, ackOutbox } from '../src/core/http-ingest.js';
import { connectSSE } from '../src/core/sse-fetch-client.js';
import { contentHash } from '../src/core/types.js';

function cfg() { return async () => ({ serverUrl: 'http://localhost:8204', token: 't' }); }

describe('B1 resolveSSEOutboundKeys：SSE Data → 去重/ack 键', () => {
  it('Data 含 msg_id → 原样返回（服务端 BuildOutboundSSEEvent 契约）', () => {
    const { msgId, convId } = resolveSSEOutboundKeys(
      { msg_id: 'mh:abcd1234', conversation_id: 'c1', content: '你好', hub_id: 99 }, 'xianyu');
    expect(msgId).toBe('mh:abcd1234');
    expect(convId).toBe('c1');
  });

  it('msg_id 缺失 → contentHash(channel,conv,content) 同源回退，且绝不吃 data.id/hub_id', () => {
    const data = { hub_id: 99, id: '99', conversation_id: 'c1', content: ' 你好 ' };
    const { msgId } = resolveSSEOutboundKeys(data, 'xianyu');
    expect(msgId).toBe(contentHash('xianyu', 'c1', ' 你好 '));
    expect(msgId).not.toBe('99');
    expect(msgId).toMatch(/^mh:[0-9a-f]{8}$/);
  });

  it('conversation_id 缺失 → _unknown_（与 SentCache 复合键语义一致）', () => {
    const { convId } = resolveSSEOutboundKeys({ msg_id: 'mh:x1', content: 'a' }, 'douyin');
    expect(convId).toBe('_unknown_');
  });
});

describe('B2 pollDownlink：ack 携带会话归属（v2 items）', () => {
  let chrome;
  beforeEach(() => { chrome = makeChromeStorage(); globalThis.chrome = chrome; vi.clearAllMocks(); });
  afterEach(() => { delete globalThis.chrome; });

  it('普通消息：ackOutbox opts.items = [{msg_id, conversation_id}]', async () => {
    await initDownlink(['b2a']);
    getOutbox.mockResolvedValue({ status: 'ok', messages: [{ msg_id: 'm1', content: 'hi', conversation_id: 'c1' }] });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    await pollDownlink('b2a', 'acc1', cfg(), { sendOutbound: async () => ({ ok: true }) });
    expect(ackOutbox).toHaveBeenCalledTimes(1);
    const opts = ackOutbox.mock.calls[0][2];
    expect(opts.items).toEqual([{ msg_id: 'm1', conversation_id: 'c1' }]);
  });

  it('DM 重映射（extra.dm_target=member）：发送用 receiver_id，ack 用行原始 conversation_id', async () => {
    await initDownlink(['b2b']);
    getOutbox.mockResolvedValue({
      status: 'ok',
      messages: [{ msg_id: 'dm1', content: 'hi', conversation_id: 'group9', receiver_id: 'member7', extra: { dm_target: 'member' } }],
    });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    const sendOutbound = vi.fn(async () => ({ ok: true }));
    await pollDownlink('b2b', 'acc1', cfg(), { sendOutbound });
    expect(sendOutbound.mock.calls[0][1]).toBe('member7');
    const opts = ackOutbox.mock.calls[0][2];
    expect(opts.items).toEqual([{ msg_id: 'dm1', conversation_id: 'group9' }]);
  });

  it('payload 缺 conversation_id（老服务端）→ 回退 legacy msg_ids 不发 items', async () => {
    await initDownlink(['b2c']);
    getOutbox.mockResolvedValue({ status: 'ok', messages: [{ msg_id: 'm9', content: 'hi' }] });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    await pollDownlink('b2c', 'acc1', cfg(), { sendOutbound: async () => ({ ok: true }) });
    const opts = ackOutbox.mock.calls[0][2];
    expect(opts.items).toBeUndefined();
  });

  it('ack 失败入队携带 conversationId，下轮重试透传给 ackOutbox', async () => {
    await initDownlink(['b2d']);
    getOutbox.mockResolvedValue({ status: 'ok', messages: [{ msg_id: 'mr1', content: 'hi', conversation_id: 'c-r' }] });
    ackOutbox.mockResolvedValue({ status: 'error' });
    const c = cfg();
    await pollDownlink('b2d', 'acc1', c, { sendOutbound: async () => ({ ok: true }) });
    const due = claimDuePendingAck('b2d');
    expect(due[0].conversationId).toBe('c-r');
    ackOutbox.mockResolvedValue({ status: 'ok' });
    await pollDownlink('b2d', 'acc1', c, { sendOutbound: async () => ({ ok: true }) });
    const retryCall = ackOutbox.mock.calls[1];
    expect(retryCall[1]).toEqual(['mr1']);
    expect(retryCall[2].conversationId).toBe('c-r');
  });

  it('addPendingAck 旧调用点（不传 conv）向后兼容：conversationId 为空串', () => {
    addPendingAck('b2e', 'm-legacy', 'err');
    const due = claimDuePendingAck('b2e');
    expect(due[0].conversationId).toBe('');
  });
});

describe('B2 ackOutbox 真实实现：请求体形状', () => {
  let fetchMock;
  beforeEach(() => {
    fetchMock = vi.fn(async () => ({
      ok: true, status: 200, statusText: 'OK',
      headers: { get: () => null },
      text: async () => JSON.stringify({ status: 'ok' }),
    }));
    globalThis.fetch = fetchMock;
  });
  afterEach(() => { delete globalThis.fetch; });

  async function bodyOf(i = 0) {
    return JSON.parse(fetchMock.mock.calls[i][1].body);
  }

  it('opts.conversationId → v2 items[]，含 v:2 与 status=delivered', async () => {
    const real = await vi.importActual('../src/core/http-ingest.js');
    await real.ackOutbox({ serverUrl: 'http://s', channel: 'xhs', accountId: 'a', token: 't' },
      ['m1', 'm2'], { conversationId: 'c1' });
    const body = await bodyOf(0);
    expect(body).toEqual({
      v: 2,
      items: [
        { msg_id: 'm1', conversation_id: 'c1' },
        { msg_id: 'm2', conversation_id: 'c1' },
      ],
      status: 'delivered',
    });
  });

  it('opts.items（逐条会话）→ 原样透传', async () => {
    const real = await vi.importActual('../src/core/http-ingest.js');
    await real.ackOutbox({ serverUrl: 'http://s', channel: 'xhs', accountId: 'a', token: 't' },
      ['m1', 'm2'], { items: [
        { msg_id: 'm1', conversation_id: 'c1' },
        { msg_id: 'm2', conversation_id: 'c2' },
      ] });
    const body = await bodyOf(0);
    expect(body.items).toEqual([
      { msg_id: 'm1', conversation_id: 'c1' },
      { msg_id: 'm2', conversation_id: 'c2' },
    ]);
  });

  it('无会话归属 / _unknown_ / 残缺 items → legacy msg_ids（不发必 400 的 v2）', async () => {
    const real = await vi.importActual('../src/core/http-ingest.js');
    await real.ackOutbox({ serverUrl: 'http://s', channel: 'xhs', accountId: 'a', token: 't' },
      ['m1'], {});
    await real.ackOutbox({ serverUrl: 'http://s', channel: 'xhs', accountId: 'a', token: 't' },
      ['m2'], { conversationId: '_unknown_' });
    await real.ackOutbox({ serverUrl: 'http://s', channel: 'xhs', accountId: 'a', token: 't' },
      ['m3'], { items: [{ msg_id: 'm3', conversation_id: '' }] });
    for (const [i, id] of [[0, 'm1'], [1, 'm2'], [2, 'm3']].entries()) {
      const body = await bodyOf(i);
      expect(body.msg_ids).toEqual([id[1]]);
      expect(body.items).toBeUndefined();
    }
  });
});

describe('B3 startSSEDelivery：重连超限后降级慢重连而非永久放弃', () => {
  let chrome;
  beforeEach(() => {
    chrome = makeChromeStorage(); globalThis.chrome = chrome;
    vi.clearAllMocks();
    vi.useFakeTimers();
  });
  afterEach(() => { vi.useRealTimers(); delete globalThis.chrome; });

  it('connectSSE 持续失败：第 11 次后仍继续尝试（旧实现此处永久停止）', async () => {
    connectSSE.mockImplementation(async () => { throw new Error('net down'); });
    const stop = await startSSEDelivery('b3a', 'acc1', { sendOutbound: async () => ({ ok: true }) });
    // 每轮推进 10 分钟：足够跨过最大指数退避(30s)与降级慢重连(5min+20%抖动)
    for (let i = 0; i < 13; i++) {
      await vi.advanceTimersByTimeAsync(600_000);
    }
    const attempts = connectSSE.mock.calls.length;
    expect(attempts).toBeGreaterThanOrEqual(13); // 13 轮推进 → 首连 + ≥12 次重连
    await stop();
    await vi.advanceTimersByTimeAsync(600_000);
    expect(connectSSE.mock.calls.length).toBe(attempts); // stop 后不再重连
  }, 15_000);
});
