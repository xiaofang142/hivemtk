// 批11（2026-09-19）B 链路下行「已发重复补确认 + 主动私信会话定位同源」回归测试。
//
// 覆盖两处真实缺陷：
//   H3 客户端 SentCache 命中后直接 continue：服务端仍认为该行欠投递 →
//      每 claimTimeout(30s) 重推一次、每次命中缓存跳过，循环永不收敛（且行永远不翻终态）。
//   H4 SSE 路径缺 extra.dm_target==='member' 的会话重映射（轮询有）：
//      SSE 是生产默认通道 → 主动私信文案被填进群会话输入框（发错对象，不可撤回）。
// 对应 spec §3.7-2。
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
vi.mock('../src/core/sanitize.js', () => ({ sanitizeForDisplay: (t) => t }));

// 与 batch3-sse-stop 同款：connectSSE 悬挂并把 opts 交出来，测试即可主动投喂 onMessage。
let live = null;
vi.mock('../src/core/sse-fetch-client.js', () => ({
  connectSSE: (channel, accountId, opts) => new Promise((resolve) => { live = { resolve, opts }; }),
  getLastEventID: () => '',
  setLastEventID: () => {},
  stopSSE: vi.fn(() => {
    if (live) { const { resolve } = live; live = null; resolve(async () => {}); return true; }
    return false;
  }),
}));

import {
  pollDownlink,
  initDownlink,
  claimDuePendingAck,
  dmAwareConvId,
  startSSEDelivery,
} from '../src/core/downlink.js';
import { getOutbox, ackOutbox } from '../src/core/http-ingest.js';
import { contentHash } from '../src/core/types.js';

const tick = (ms) => new Promise((r) => setTimeout(r, ms));
const cfg = () => async () => ({ serverUrl: 'http://localhost:8204', token: 't' });

describe('dmAwareConvId：主动私信会话定位键', () => {
  it('extra.dm_target=member（对象）→ 用 receiver_id', () => {
    expect(dmAwareConvId(
      { conversation_id: 'group9', receiver_id: 'member7', extra: { dm_target: 'member' } }, 'group9',
    )).toBe('member7');
  });

  it('extra 是 JSON 字符串（老 payload 直传 DB 列）→ 同样重映射', () => {
    expect(dmAwareConvId(
      { conversation_id: 'group9', receiver_id: 'member7', extra: '{"dm_target":"member"}' }, 'group9',
    )).toBe('member7');
  });

  it('dm_target=conv / extra 非法 JSON / 无 receiver_id → 保持原会话', () => {
    expect(dmAwareConvId({ conversation_id: 'c1', receiver_id: 'u1', extra: { dm_target: 'conv' } }, 'c1')).toBe('c1');
    expect(dmAwareConvId({ conversation_id: 'c1', receiver_id: 'u1', extra: '{bad json' }, 'c1')).toBe('c1');
    expect(dmAwareConvId({ conversation_id: 'c1', extra: { dm_target: 'member' } }, 'c1')).toBe('c1');
  });

  it('receiver_id 与 conversation_id 相同（抖音私信会话 id 即成员 id）→ 不重映射', () => {
    expect(dmAwareConvId(
      { conversation_id: 'm1', receiver_id: 'm1', extra: { dm_target: 'member' } }, 'm1',
    )).toBe('m1');
  });
});

describe('H3 轮询：SentCache 命中的欠投递行补确认', () => {
  let chrome;
  beforeEach(() => { chrome = makeChromeStorage(); globalThis.chrome = chrome; vi.clearAllMocks(); });
  afterEach(() => { delete globalThis.chrome; });

  it('已发过的行不再下发，但按行原始 conversation_id 补一次 delivered 确认', async () => {
    chrome._data.bridge_sent_b11a = ['m1|c1'];
    await initDownlink(['b11a']);
    getOutbox.mockResolvedValue({
      status: 'ok',
      messages: [
        { msg_id: 'm1', content: '重复', conversation_id: 'c1' },
        { msg_id: 'm2', content: '新消息', conversation_id: 'c2' },
      ],
    });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    const sendOutbound = vi.fn(async () => ({ ok: true }));
    await pollDownlink('b11a', 'acc1', cfg(), { sendOutbound });

    expect(sendOutbound).toHaveBeenCalledTimes(1);
    expect(sendOutbound.mock.calls[0][1]).toBe('c2');
    expect(ackOutbox).toHaveBeenCalledTimes(2);
    const reAck = ackOutbox.mock.calls[0];
    expect(reAck[1]).toEqual(['m1']);
    expect(reAck[2].items).toEqual([{ msg_id: 'm1', conversation_id: 'c1' }]);
    expect(reAck[2].label).toContain('补确认');
  });

  it('DM 场景：缓存键用 receiver_id，补确认仍用行原始 conversation_id', async () => {
    chrome._data.bridge_sent_b11b = ['dm1|member7'];
    await initDownlink(['b11b']);
    getOutbox.mockResolvedValue({
      status: 'ok',
      messages: [{ msg_id: 'dm1', content: 'hi', conversation_id: 'group9', receiver_id: 'member7', extra: { dm_target: 'member' } }],
    });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    const sendOutbound = vi.fn(async () => ({ ok: true }));
    await pollDownlink('b11b', 'acc1', cfg(), { sendOutbound });

    expect(sendOutbound).not.toHaveBeenCalled();
    expect(ackOutbox).toHaveBeenCalledTimes(1);
    expect(ackOutbox.mock.calls[0][2].items).toEqual([{ msg_id: 'dm1', conversation_id: 'group9' }]);
  });

  it('补确认失败（非 ok）→ 入 _pendingAck 带会话归属，下轮按退避重试', async () => {
    chrome._data.bridge_sent_b11c = ['m3|c3'];
    await initDownlink(['b11c']);
    getOutbox.mockResolvedValue({ status: 'ok', messages: [{ msg_id: 'm3', content: 'x', conversation_id: 'c3' }] });
    ackOutbox.mockResolvedValue({ status: 'error' });
    await pollDownlink('b11c', 'acc1', cfg(), { sendOutbound: async () => ({ ok: true }) });

    const due = claimDuePendingAck('b11c');
    expect(due.map((d) => d.msgId)).toEqual(['m3']);
    expect(due[0].conversationId).toBe('c3');
  });

  it('服务端逐条回执 not_found → 视为已了结，不再入重试队列（防无限重发）', async () => {
    chrome._data.bridge_sent_b11d = ['m4|c4'];
    await initDownlink(['b11d']);
    getOutbox.mockResolvedValue({ status: 'ok', messages: [{ msg_id: 'm4', content: 'x', conversation_id: 'c4' }] });
    ackOutbox.mockResolvedValue({ status: 'ok', items: [{ msg_id: 'm4', status: 'not_found' }] });
    await pollDownlink('b11d', 'acc1', cfg(), { sendOutbound: async () => ({ ok: true }) });
    expect(claimDuePendingAck('b11d')).toEqual([]);
  });

  it('无会话归属的重复行不补确认（legacy 会跨会话误翻，v2 会整单 400）', async () => {
    chrome._data.bridge_sent_b11e = ['m5|_unknown_'];
    await initDownlink(['b11e']);
    getOutbox.mockResolvedValue({ status: 'ok', messages: [{ msg_id: 'm5', content: 'x' }] });
    ackOutbox.mockResolvedValue({ status: 'ok' });
    await pollDownlink('b11e', 'acc1', cfg(), { sendOutbound: async () => ({ ok: true }) });
    expect(ackOutbox).not.toHaveBeenCalled();
  });
});

describe('H4 SSE：与轮询同源的会话定位 + 重复补确认', () => {
  let chrome;
  beforeEach(() => {
    chrome = makeChromeStorage();
    chrome._data.bridgeConfig = { serverUrl: 'http://localhost:8204', token: 't' };
    globalThis.chrome = chrome;
    vi.clearAllMocks();
    live = null;
  });
  afterEach(async () => { delete globalThis.chrome; live = null; });

  async function openSSE(channel, handlers) {
    const stop = await startSSEDelivery(channel, 'acc1', handlers);
    for (let i = 0; i < 50 && !live; i++) await tick(5);
    expect(live).toBeTruthy();
    return stop;
  }

  it('主动私信事件：sendOutbound 收 receiver_id，ack 收行原始 conversation_id', async () => {
    ackOutbox.mockResolvedValue({ status: 'ok' });
    const sendOutbound = vi.fn(async () => ({ ok: true }));
    const stop = await openSSE('b11s1', { sendOutbound });
    live.opts.onMessage({
      msg_id: 'dm1', content: 'hi', conversation_id: 'group9', receiver_id: 'member7',
      extra: { dm_target: 'member' },
    });
    // onMessage 未被 connectSSE await（单条处理不能阻塞读流），改为轮询等待落定
    for (let i = 0; i < 50 && sendOutbound.mock.calls.length === 0; i++) await tick(5);
    await tick(20);
    expect(sendOutbound).toHaveBeenCalledTimes(1);
    expect(sendOutbound.mock.calls[0][1]).toBe('member7');
    expect(ackOutbox).toHaveBeenCalledTimes(1);
    expect(ackOutbox.mock.calls[0][1]).toEqual(['dm1']);
    expect(ackOutbox.mock.calls[0][2].conversationId).toBe('group9');
    await stop();
  });

  it('SentCache 命中的事件：跳过下发但补一次 delivered 确认（打破 30s 重推循环）', async () => {
    chrome._data.bridge_sent_b11s2 = ['ms1|cs1'];
    ackOutbox.mockResolvedValue({ status: 'ok' });
    const sendOutbound = vi.fn(async () => ({ ok: true }));
    const onDuplicate = vi.fn();
    const stop = await openSSE('b11s2', { sendOutbound, onDuplicate });
    live.opts.onMessage({ msg_id: 'ms1', content: '再来一次', conversation_id: 'cs1' });
    for (let i = 0; i < 50 && onDuplicate.mock.calls.length === 0; i++) await tick(5);
    expect(sendOutbound).not.toHaveBeenCalled();
    expect(onDuplicate).toHaveBeenCalledWith('ms1');
    expect(ackOutbox).toHaveBeenCalledTimes(1);
    expect(ackOutbox.mock.calls[0][2].items).toEqual([{ msg_id: 'ms1', conversation_id: 'cs1' }]);
    await stop();
  });

  it('msg_id 缺失（本地 contentHash 回退键）→ 不拿合成 id 去 ack，只回调 onDuplicate', async () => {
    // 服务端 Data 契约异常时 msgId = contentHash(channel, conv, content)：它不是 DB 行 id，
    // 拿去 ack 只会命中 not_found 噪声，所以补确认必须以 data.msg_id 真实存在为前提。
    const synth = contentHash('b11s3', 'c9', 'dup');
    chrome._data.bridge_sent_b11s3 = [`${synth}|c9`];
    ackOutbox.mockResolvedValue({ status: 'ok' });
    const sendOutbound = vi.fn(async () => ({ ok: true }));
    const onDuplicate = vi.fn();
    const stop = await openSSE('b11s3', { sendOutbound, onDuplicate });
    live.opts.onMessage({ content: 'dup', conversation_id: 'c9' });
    for (let i = 0; i < 50 && onDuplicate.mock.calls.length === 0; i++) await tick(5);
    expect(onDuplicate).toHaveBeenCalledWith(synth);
    expect(sendOutbound).not.toHaveBeenCalled();
    expect(ackOutbox).not.toHaveBeenCalled();
    await stop();
  });
});
