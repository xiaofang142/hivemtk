// R22 第二十三轮 §6-4：_pendingAck 是纯内存重试队列，ack 失败/补确认失败的条目入队后要有人排水。
// 原实现里 claimDuePendingAck 只在 pollDownlink 调用，而生产默认形态走 SSE（服务端 sse_enabled，
// 客户端探到即 return，轮询定时器根本不启动）⇒ 那条「下个周期重试」的承诺在默认形态下永不兑现。
// 本文件锁三件事：SSE 启动后队列真被排、排水与轮询共用同一实现（不许长第二份）、stop 之后不再排。
import { describe, it, expect, vi, beforeEach } from 'vitest';

globalThis.chrome = {
  storage: {
    local: {
      _data: { bridgeConfig: { serverUrl: 'http://127.0.0.1:9999', token: 't-r23' } },
      async get(keys) {
        const out = {};
        const list = Array.isArray(keys) ? keys : [keys];
        for (const k of list) if (k in this._data) out[k] = this._data[k];
        return out;
      },
      async set(obj) { Object.assign(this._data, obj); },
      async remove(keys) {
        const list = Array.isArray(keys) ? keys : [keys];
        for (const k of list) delete this._data[k];
      },
    },
  },
};

// 回显被 ack 的那批 id：processAckDetailedResult 按 msg_id 逐条配对，夹具若固定回另一批 id，
// 判出来的就是 retriable 而不是 acked（那是夹具的错，不是排水的错）。
const ackOutbox = vi.fn(async (_cred, ids) => ({
  status: 'ok',
  items: (ids || []).map((id) => ({ msg_id: id, status: 'acked' })),
}));

vi.mock('../src/core/http-ingest.js', () => ({
  ackOutbox: (...args) => ackOutbox(...args),
  getOutbox: async () => ({ messages: [] }),
}));

vi.mock('../src/core/sse-fetch-client.js', () => ({
  connectSSE: () => new Promise(() => {}), // 活动长连接：永不 resolve，模拟"一直在线"
  getLastEventID: () => '',
  setLastEventID: () => {},
  stopSSE: vi.fn(),
}));

const dl = await import('../src/core/downlink.js');
const { addPendingAck, getPendingAckStats, pollDownlink, startSSEDelivery, drainPendingAcks } = dl;

const tick = (ms) => new Promise((r) => setTimeout(r, ms));

function seedDue(channel, msgId, convId) {
  addPendingAck(channel, msgId, 'sse_ack_failed', convId);
}

beforeEach(() => {
  ackOutbox.mockClear();
  globalThis.chrome.storage.local._data = { bridgeConfig: { serverUrl: 'http://127.0.0.1:9999', token: 't-r23' } };
});

describe('§6-4 SSE 模式必须排 _pendingAck 队列', () => {
  it('SSE 启动后，到期条目在无人再推消息的情况下被重发 ack', async () => {
    const ch = 'r23_sse_drain';
    seedDue(ch, 'm-r23-1', 'conv-r23');
    const stop = await startSSEDelivery(ch, 'acc-r23', {
      sendOutbound: async () => ({ ok: true }),
      pendingAckDrainIntervalMs: 120,
    });
    // 新入队条目 lastTryAt=0 ⇒ 即刻到期，所以下一轮 tick 就该被排走；窗口只留给调度抖动
    let retried = 0;
    for (let i = 0; i < 40 && !retried; i++) {
      await tick(100);
      retried = ackOutbox.mock.calls.filter((c) => JSON.stringify(c[1]).includes('m-r23-1')).length;
    }
    await stop();
    if (!retried) {
      throw new Error('§6-4 未达成：SSE 模式下入队的 ack 重试条目没有任何排水（客户侧表现=这条下行永远停在欠投递集合）');
    }
    expect(getPendingAckStats(ch).size).toBe(0);
  }, 20000);

  it('stop() 之后排水器不再触发（不许留悬空 interval）', async () => {
    // 判据落在"定时器有没有被回收"，不是"之后有没有再打 ack"——生产路径上 stopped 守卫
    // 已经挡住了排水，只看行为的话 clearInterval 怎么写都杀不掉（无牙判据）。
    const ch = 'r23_sse_stop';
    vi.useFakeTimers();
    let stop;
    try {
      stop = await startSSEDelivery(ch, 'acc-r23', {
        sendOutbound: async () => ({ ok: true }),
        pendingAckDrainIntervalMs: 100,
      });
      const timersWhileLive = vi.getTimerCount();
      await vi.advanceTimersByTimeAsync(250);
      expect(vi.getTimerCount()).toBe(timersWhileLive); // 排水是周期 timer，跑完不自我注销
      await stop();
      expect(vi.getTimerCount()).toBeLessThan(timersWhileLive);
    } finally {
      vi.useRealTimers();
    }
    seedDue(ch, 'm-r23-2', 'conv-r23');
    await tick(400);
    const calls = ackOutbox.mock.calls.filter((c) => JSON.stringify(c[1]).includes('m-r23-2')).length;
    expect(calls).toBe(0);
  }, 20000);

  it('排水实现只有一份：轮询与 SSE 共用 drainPendingAcks，且 ack 请求带会话归属', async () => {
    expect(typeof drainPendingAcks).toBe('function');
    const ch = 'r23_shared_seam';
    seedDue(ch, 'm-r23-3', 'conv-r23-3');
    const before = ackOutbox.mock.calls.length;
    const res = await drainPendingAcks(ch, { serverUrl: 'http://127.0.0.1:9999', channel: ch, accountId: 'acc-x', token: 't' });
    expect(ackOutbox.mock.calls.length).toBeGreaterThan(before);
    const call = ackOutbox.mock.calls[ackOutbox.mock.calls.length - 1];
    expect(call[0]).toMatchObject({ channel: ch, accountId: 'acc-x' });
    expect(call[2]).toMatchObject({ conversationId: 'conv-r23-3' });
    expect(res.retried).toBe(1);
    expect(res.success).toBe(1);
  }, 20000);

  it('pollDownlink 仍走同一个排水函数（重构不许把轮询那条腿弄丢）', async () => {
    const ch = 'r23_poll_still_drains';
    seedDue(ch, 'm-r23-4', 'conv-r23-4');
    const before = ackOutbox.mock.calls.length;
    await pollDownlink(ch, 'acc-r23', async () => ({ serverUrl: 'http://127.0.0.1:9999', token: 't' }));
    const calls = ackOutbox.mock.calls.slice(before).filter((c) => JSON.stringify(c[1]).includes('m-r23-4'));
    expect(calls.length).toBe(1);
  }, 20000);
});
