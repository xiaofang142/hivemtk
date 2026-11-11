// M-6（2026-11-11）：follow_user 动作行的 outbox 处理（HTTP 轮询 + SSE 双传输）。
// 验收锚点：
//   · 动作行不进文本下发队列（sendOutbound 不被调用 = 不阻塞 AI 回复主链路）；
//   · 执行结果 ack 收口：delivered / failed 终态；rateLimited 留 pending 重推；
//   · 无执行器（旧扩展）= 旧行为（留 pending）；
//   · 文本行与动作行同批时文本仍正常下发。
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

let live = null;

vi.mock('../src/core/http-ingest.js', () => ({
  getOutbox: vi.fn(),
  ackOutbox: vi.fn(),
}));

vi.mock('../src/core/sanitize.js', () => ({
  sanitizeForDisplay: (t) => t,
  stripMarkdownForDM: (t) => t,
}));

vi.mock('../src/core/sse-fetch-client.js', () => ({
  connectSSE: (channel, accountId, opts) => new Promise(() => { live = { opts }; }),
  getLastEventID: () => '',
  setLastEventID: () => {},
  stopSSE: () => false,
}));

import { pollDownlink, startSSEDelivery, FOLLOW_ACTION_MSG_TYPE } from '../src/core/downlink.js';
import { getOutbox, ackOutbox } from '../src/core/http-ingest.js';

const cfg = () => async () => ({ serverUrl: 'http://localhost:8204', token: 't' });
const followRow = (msgId, convId) => ({
  msg_id: msgId,
  conversation_id: convId,
  msg_type: FOLLOW_ACTION_MSG_TYPE,
  content: '',
});

describe('downlink / follow_user 动作行（HTTP 轮询）', () => {
  let st;
  beforeEach(() => {
    st = makeChromeStorage();
    globalThis.chrome = st;
    vi.clearAllMocks();
    ackOutbox.mockResolvedValue({ status: 'ok', items: [{ msg_id: 'x', status: 'acked' }] });
    Object.defineProperty(document, 'visibilityState', { configurable: true, value: 'visible' });
  });
  afterEach(() => {
    delete globalThis.chrome;
  });

  it('成功：followAction 执行一次，sendOutbound 不被调用，ack delivered', async () => {
    getOutbox.mockResolvedValue({ status: 'ok', messages: [followRow('m-f1', 'c1')] });
    const sendOutbound = vi.fn(async () => ({ ok: true }));
    const followAction = vi.fn(async () => ({ ok: true, via: 'follow_user' }));

    await pollDownlink('fa_poll', 'acc1', cfg(), { sendOutbound, followAction });

    expect(followAction).toHaveBeenCalledTimes(1);
    expect(followAction).toHaveBeenCalledWith('c1');
    expect(sendOutbound).not.toHaveBeenCalled(); // 动作行不进文本下发队列
    expect(ackOutbox).toHaveBeenCalledTimes(1);
    const [, ids, opts] = ackOutbox.mock.calls[0];
    expect(ids).toEqual(['m-f1']);
    expect(opts.status).toBeUndefined(); // delivered 默认
    const keys = (st._data.bridge_sent_fa_poll || []).map((e) => (Array.isArray(e) ? e[0] : e));
    expect(keys).toContain('m-f1|c1'); // 写已发缓存防重推
  });

  it('失败：followAction ok:false → ack failed 终态（不再重推，留痕）', async () => {
    getOutbox.mockResolvedValue({ status: 'ok', messages: [followRow('m-f2', 'c2')] });
    const followAction = vi.fn(async () => ({ ok: false, error: 'follow_button_not_found' }));

    await pollDownlink('fa_fail', 'acc1', cfg(), { followAction });

    expect(ackOutbox).toHaveBeenCalledTimes(1);
    const [, ids, opts] = ackOutbox.mock.calls[0];
    expect(ids).toEqual(['m-f2']);
    expect(opts.status).toBe('failed');
    expect(opts.items).toEqual([{ msg_id: 'm-f2', conversation_id: 'c2' }]);
  });

  it('限速：rateLimited → 不 ack（留 pending 重推，与文本行同形）', async () => {
    getOutbox.mockResolvedValue({ status: 'ok', messages: [followRow('m-f3', 'c3')] });
    const followAction = vi.fn(async () => ({ ok: false, rateLimited: true }));

    await pollDownlink('fa_rl', 'acc1', cfg(), { followAction });

    expect(ackOutbox).not.toHaveBeenCalled();
    const keys = (st._data.bridge_sent_fa_rl || []).map((e) => (Array.isArray(e) ? e[0] : e));
    expect(keys).not.toContain('m-f3|c3');
  });

  it('明确跳过（disabled/busy/cooldown 等 adapter 决策）→ ack delivered 收口（防重推死循环）', async () => {
    getOutbox.mockResolvedValue({ status: 'ok', messages: [followRow('m-f4', 'c4')] });
    const followAction = vi.fn(async () => ({ ok: false, skipped: 'disabled' }));

    await pollDownlink('fa_skip', 'acc1', cfg(), { followAction });

    expect(ackOutbox).toHaveBeenCalledTimes(1);
    const [, ids, opts] = ackOutbox.mock.calls[0];
    expect(ids).toEqual(['m-f4']);
    expect(opts.status).toBeUndefined();
  });

  it('未提供 followAction（旧调用方）→ 不执行不 ack，留 pending（与未知行同形）', async () => {
    getOutbox.mockResolvedValue({ status: 'ok', messages: [followRow('m-f5', 'c5')] });

    await pollDownlink('fa_nohandler', 'acc1', cfg(), {});

    expect(ackOutbox).not.toHaveBeenCalled();
  });

  it('文本行与动作行同批：文本照常下发（动作不阻塞主链路），动作随后执行', async () => {
    getOutbox.mockResolvedValue({
      status: 'ok',
      messages: [
        followRow('m-f6', 'c6'),
        { msg_id: 'm-t1', conversation_id: 'c7', msg_type: 'text', content: 'AI 回复正文' },
      ],
    });
    const sendOutbound = vi.fn(async () => ({ ok: true }));
    const followAction = vi.fn(async () => ({ ok: true }));

    await pollDownlink('fa_mixed', 'acc1', cfg(), { sendOutbound, followAction });

    expect(sendOutbound).toHaveBeenCalledTimes(1);
    expect(sendOutbound).toHaveBeenCalledWith('AI 回复正文', 'c7', expect.anything());
    expect(followAction).toHaveBeenCalledTimes(1);
    expect(followAction).toHaveBeenCalledWith('c6');
  });
});

describe('downlink / follow_user 动作行（SSE）', () => {
  let st;
  beforeEach(() => {
    st = makeChromeStorage();
    globalThis.chrome = st;
    vi.clearAllMocks();
    ackOutbox.mockResolvedValue({ status: 'ok', items: [{ msg_id: 'x', status: 'acked' }] });
    live = null;
  });
  afterEach(() => {
    delete globalThis.chrome;
  });

  it('SSE 帧 msg_type=follow_user → followAction 执行 + ack delivered', async () => {
    const stop = await startSSEDelivery('fa_sse', 'acc1', {
      followAction: async (convId) => ({ ok: true, convId }),
    });
    for (let i = 0; i < 20 && !live; i++) await new Promise((r) => setTimeout(r, 5));
    expect(live).toBeTruthy();

    await live.opts.onMessage({ msg_id: 'm-s1', conversation_id: 'cs1', msg_type: FOLLOW_ACTION_MSG_TYPE, content: '' });
    await stop();

    expect(ackOutbox).toHaveBeenCalledTimes(1);
    const [, ids] = ackOutbox.mock.calls[0];
    expect(ids).toEqual(['m-s1']);
  });

  it('SSE 帧 follow_action 失败 → ack failed 终态', async () => {
    const stop = await startSSEDelivery('fa_sse2', 'acc1', {
      followAction: async () => ({ ok: false, notFound: true }),
    });
    for (let i = 0; i < 20 && !live; i++) await new Promise((r) => setTimeout(r, 5));

    await live.opts.onMessage({ msg_id: 'm-s2', conversation_id: 'cs2', msg_type: FOLLOW_ACTION_MSG_TYPE, content: '' });
    await stop();

    expect(ackOutbox).toHaveBeenCalledTimes(1);
    const [, ids, opts] = ackOutbox.mock.calls[0];
    expect(ids).toEqual(['m-s2']);
    expect(opts.status).toBe('failed');
  });

  it('SSE 文本帧不受影响（动作分型只拦 follow_user）', async () => {
    const sendOutbound = vi.fn(async () => ({ ok: true }));
    const stop = await startSSEDelivery('fa_sse3', 'acc1', {
      sendOutbound,
      followAction: async () => ({ ok: true }),
    });
    for (let i = 0; i < 20 && !live; i++) await new Promise((r) => setTimeout(r, 5));

    await live.opts.onMessage({ msg_id: 'm-s3', conversation_id: 'cs3', msg_type: 'text', content: '普通回复' });
    await stop();

    expect(sendOutbound).toHaveBeenCalledTimes(1);
  });
});
