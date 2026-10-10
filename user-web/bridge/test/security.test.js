import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

// 内存版 chrome.storage.local（与 downlink.test.js 保持一致）
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
    _data: data,
  };
}

vi.mock('../src/core/http-ingest.js', () => ({
  getOutbox: vi.fn(),
  ackOutbox: vi.fn(),
}));
vi.mock('../src/core/sanitize.js', () => ({
    sanitizeForDisplay: (t) => t,
    stripMarkdownForDM: (t) => t,
  }));

import {
  pollDownlink,
  initDownlink,
  addPendingAck,
  markPendingAckTried,
  claimDuePendingAck,
  getPendingAckStats,
} from '../src/core/downlink.js';
import { getOutbox, ackOutbox } from '../src/core/http-ingest.js';
import { BRIDGE_PROTOCOL_V2 } from '../src/core/constants.js';

// 每个用例用独立 channel，避免模块级 SentCache / _pendingAckByChannel 单例跨用例污染
let _uniq = 0;
const newChannel = () => `sec_${++_uniq}_${Date.now()}_${Math.random().toString(36).slice(2, 8)}`;

// 批20d-A4：已发缓存落盘形状变成 ['key', 写入时刻]（TTL 要知道年龄）。这里只断言"记住了哪条"。
const sentKeys = (raw) => (raw || []).map((e) => (Array.isArray(e) ? e[0] : e));

function cfg() {
  return async () => ({ serverUrl: 'http://localhost:8204', token: 't' });
}

describe('P0-6 前端安全：防越权探测 / 跨账号隔离 / 重试兜底', () => {
  let chrome;
  beforeEach(() => { chrome = makeChromeStorage(); globalThis.chrome = chrome; vi.clearAllMocks(); });
  afterEach(() => { delete globalThis.chrome; });

  it('场景1：ack items status=not_in_scope（存在但归属其他账号/方向）→ 不入 _pendingAck，停止重发（防越权探测）', async () => {
    const ch = newChannel();
    await initDownlink([ch]);
    const adapter = { sendOutbound: vi.fn(async () => ({ ok: true })) };
    getOutbox.mockResolvedValue({
      status: 'ok',
      messages: [{ msg_id: 'm-nis', content: 'hi', conversation_id: 'c1' }],
    });
    ackOutbox.mockResolvedValue({
      status: 'ok',
      items: [{ msg_id: 'm-nis', status: BRIDGE_PROTOCOL_V2.RESPONSE_STATUS.NOT_IN_SCOPE }],
    });
    await pollDownlink(ch, 'acc1', cfg(), { sendOutbound: adapter.sendOutbound });

    expect(getPendingAckStats(ch).size).toBe(0);
    // 消息已实际下发成功 → 写 cache 防内容重发（停止重发）
    const stored = await chrome.storage.local.get([`bridge_sent_${ch}`]);
    expect(sentKeys(stored[`bridge_sent_${ch}`])).toContain('m-nis|c1');
  });

  it('场景1b：已在 _pendingAck 的 msg 重发 ack 返回 not_in_scope → 停止重试并清理（不再探测）', async () => {
    const ch = newChannel();
    await initDownlink([ch]);
    addPendingAck(ch, 'm-nis2', 'retriable');
    getOutbox.mockResolvedValue({ status: 'ok', messages: [] });
    ackOutbox.mockResolvedValue({
      status: 'ok',
      items: [{ msg_id: 'm-nis2', status: BRIDGE_PROTOCOL_V2.RESPONSE_STATUS.NOT_IN_SCOPE }],
    });
    await pollDownlink(ch, 'acc1', cfg(), { sendOutbound: vi.fn(async () => ({ ok: true })) });

    expect(getPendingAckStats(ch).size).toBe(0);
    expect(ackOutbox).toHaveBeenCalledTimes(1);
  });

  it('场景2：ack items status=not_found → 停止重发（不入 _pendingAck）', async () => {
    const ch = newChannel();
    await initDownlink([ch]);
    const adapter = { sendOutbound: vi.fn(async () => ({ ok: true })) };
    getOutbox.mockResolvedValue({
      status: 'ok',
      messages: [{ msg_id: 'm-nf', content: 'hi', conversation_id: 'c1' }],
    });
    ackOutbox.mockResolvedValue({
      status: 'ok',
      items: [{ msg_id: 'm-nf', status: BRIDGE_PROTOCOL_V2.RESPONSE_STATUS.NOT_FOUND }],
    });
    await pollDownlink(ch, 'acc1', cfg(), { sendOutbound: adapter.sendOutbound });

    expect(getPendingAckStats(ch).size).toBe(0);
    const stored = await chrome.storage.local.get([`bridge_sent_${ch}`]);
    expect(sentKeys(stored[`bridge_sent_${ch}`])).toContain('m-nf|c1');
  });

  it('场景3a：ack items status=acked → 已处理完成，不入 _pendingAck', async () => {
    const ch = newChannel();
    await initDownlink([ch]);
    const adapter = { sendOutbound: vi.fn(async () => ({ ok: true })) };
    getOutbox.mockResolvedValue({
      status: 'ok',
      messages: [{ msg_id: 'm-acked', content: 'hi', conversation_id: 'c1' }],
    });
    ackOutbox.mockResolvedValue({
      status: 'ok',
      items: [{ msg_id: 'm-acked', status: BRIDGE_PROTOCOL_V2.RESPONSE_STATUS.ACKED }],
    });
    await pollDownlink(ch, 'acc1', cfg(), { sendOutbound: adapter.sendOutbound });

    expect(getPendingAckStats(ch).size).toBe(0);
  });

  it('场景3b：ack items status=duplicate → 幂等跳过（不入 _pendingAck）', async () => {
    const ch = newChannel();
    await initDownlink([ch]);
    const adapter = { sendOutbound: vi.fn(async () => ({ ok: true })) };
    getOutbox.mockResolvedValue({
      status: 'ok',
      messages: [{ msg_id: 'm-dup', content: 'hi', conversation_id: 'c1' }],
    });
    ackOutbox.mockResolvedValue({
      status: 'ok',
      items: [{ msg_id: 'm-dup', status: BRIDGE_PROTOCOL_V2.RESPONSE_STATUS.DUPLICATE }],
    });
    await pollDownlink(ch, 'acc1', cfg(), { sendOutbound: adapter.sendOutbound });

    expect(getPendingAckStats(ch).size).toBe(0);
  });

  it('场景3c：重发 ack 命中 acked → 清理 _pendingAck（成功移除，不死循环）', async () => {
    const ch = newChannel();
    await initDownlink([ch]);
    addPendingAck(ch, 'm-clean', 'retriable');
    getOutbox.mockResolvedValue({ status: 'ok', messages: [] });
    ackOutbox.mockResolvedValue({
      status: 'ok',
      items: [{ msg_id: 'm-clean', status: BRIDGE_PROTOCOL_V2.RESPONSE_STATUS.ACKED }],
    });
    await pollDownlink(ch, 'acc1', cfg(), { sendOutbound: vi.fn(async () => ({ ok: true })) });

    expect(getPendingAckStats(ch).size).toBe(0);
    expect(ackOutbox).toHaveBeenCalledTimes(1); 
  });

  it('场景4：downlink 按 channel 隔离 sentCache（bridge_sent_{channel} 键隔离），不同 channel 互不影响', async () => {
    const chA = newChannel();
    const chB = newChannel();
    await initDownlink([chA, chB]);
    const adapterA = { sendOutbound: vi.fn(async () => ({ ok: true })) };
    const adapterB = { sendOutbound: vi.fn(async () => ({ ok: true })) };
    getOutbox.mockResolvedValue({
      status: 'ok',
      messages: [{ msg_id: 'm-same', content: 'hi', conversation_id: 'c1' }],
    });
    ackOutbox.mockResolvedValue({
      status: 'ok',
      items: [{ msg_id: 'm-same', status: BRIDGE_PROTOCOL_V2.RESPONSE_STATUS.ACKED }],
    });
    await pollDownlink(chA, 'acc1', cfg(), { sendOutbound: adapterA.sendOutbound });
    await pollDownlink(chB, 'acc2', cfg(), { sendOutbound: adapterB.sendOutbound });

    expect(adapterA.sendOutbound).toHaveBeenCalledTimes(1);
    expect(adapterB.sendOutbound).toHaveBeenCalledTimes(1);

    // 键隔离：两个 channel 各自的 storage key 独立存在
    const sa = await chrome.storage.local.get([`bridge_sent_${chA}`]);
    const sb = await chrome.storage.local.get([`bridge_sent_${chB}`]);
    expect(sentKeys(sa[`bridge_sent_${chA}`])).toContain('m-same|c1');
    expect(sentKeys(sb[`bridge_sent_${chB}`])).toContain('m-same|c1');

    await pollDownlink(chA, 'acc1', cfg(), { sendOutbound: adapterA.sendOutbound });
    expect(adapterA.sendOutbound).toHaveBeenCalledTimes(1);
  });

  it('场景5：ack 响应缺失 item（ack_response_missing_item）→ 入 _pendingAck 下轮重试', async () => {
    const ch = newChannel();
    await initDownlink([ch]);
    const adapter = { sendOutbound: vi.fn(async () => ({ ok: true })) };
    getOutbox.mockResolvedValue({
      status: 'ok',
      messages: [{ msg_id: 'm-miss', content: 'hi', conversation_id: 'c1' }],
    });
    ackOutbox.mockResolvedValue({ status: 'ok', items: [] }); 
    await pollDownlink(ch, 'acc1', cfg(), { sendOutbound: adapter.sendOutbound });

    expect(getPendingAckStats(ch).size).toBe(1);
    // 验证确实以 ack_response_missing_item 原因入队（下轮会重试 ack）
    const due = claimDuePendingAck(ch);
    expect(due.length).toBe(1);
    expect(due[0].msgId).toBe('m-miss');
    expect(due[0].entry.lastError).toBe('ack_response_missing_item');
  });

  it('场景6：MAX_ACK_RETRY_ATTEMPTS 达到后停止重试并保留缓存（不死循环）', async () => {
    const ch = newChannel();
    await initDownlink([ch]);
    // 预热：模拟"用户已收到"（cache 已写），且该 msg 的 ack 重试已耗尽 10 次
    const storage1 = await chrome.storage.local.get([`bridge_sent_${ch}`]);
    const arr1 = storage1[`bridge_sent_${ch}`] || [];
    arr1.push('m-exhaust|c1');
    await chrome.storage.local.set({ [`bridge_sent_${ch}`]: arr1 });

    addPendingAck(ch, 'm-exhaust', 'retriable');
    for (let i = 0; i < 10; i++) markPendingAckTried(ch, 'm-exhaust', false, 'retriable');

    getOutbox.mockResolvedValue({ status: 'ok', messages: [] });
    await pollDownlink(ch, 'acc1', cfg(), { sendOutbound: vi.fn(async () => ({ ok: true })) });

    expect(ackOutbox).not.toHaveBeenCalled();
    expect(getPendingAckStats(ch).size).toBe(0);
    // 缓存保留（用户已收到，防内容重发）
    const stored = await chrome.storage.local.get([`bridge_sent_${ch}`]);
    expect(stored[`bridge_sent_${ch}`] || []).toContain('m-exhaust|c1');
  });
});

