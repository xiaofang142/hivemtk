// B5（批3）SSE 停止路径行为测试：mock sse-fetch-client 后可控地"活动连接期间 stop()"——
// 原实现 stop 只 await 一个尚未 resolve 的 cleanupSSE，无法断开进行中的流，也无法
// 打断重连退避 sleep（最长可挂 5min+）。
import { describe, it, expect, vi } from 'vitest';

let live = null; // 当前挂起的 connectSSE { resolve, reject, opts }
let connectCalls = 0;

vi.mock('../src/core/sse-fetch-client.js', () => ({
  connectSSE: (channel, accountId, opts) => {
    connectCalls++;
    return new Promise((resolve, reject) => {
      live = { resolve, reject, opts };
    });
  },
  getLastEventID: () => '',
  setLastEventID: () => {},
  stopSSE: vi.fn(() => {
    if (live) {
      const { resolve } = live;
      live = null;
      resolve(async () => {}); // 模拟 AbortError 收尾：connectSSE 正常 resolve 停止函数
      return true;
    }
    return false;
  }),
}));

const { startSSEDelivery } = await import('../src/core/downlink.js');
const { stopSSE } = await import('../src/core/sse-fetch-client.js');

const tick = (ms) => new Promise((r) => setTimeout(r, ms));

describe('SSE startSSEDelivery 停止路径', () => {
  it('活动连接期间 stop()：走 key 级 stopSSE abort，循环即刻收束且不再重连', async () => {
    connectCalls = 0;
    const stop = await startSSEDelivery('douyin', 'acc-A', { sendOutbound: async () => ({ ok: true }) });
    for (let i = 0; i < 50 && !live; i++) await tick(5);
    expect(live).toBeTruthy();
    const callsAtConnect = connectCalls;

    await stop();
    expect(stopSSE).toHaveBeenCalledWith('douyin', 'acc-A');
    await tick(60);
    expect(connectCalls).toBe(callsAtConnect); // stopped 后不得再发起新连接
    expect(live).toBe(null);
  });

  it('退避等待期间 stop()：wakeWait 即刻唤醒，循环秒收（不等满 delay）', async () => {
    connectCalls = 0;
    const stop = await startSSEDelivery('douyin', 'acc-B', {});
    for (let i = 0; i < 50 && !live; i++) await tick(5);
    const rejected = live;
    live = null;
    rejected.reject(new Error('stream broke')); // 触发 catch → 指数退避 sleep(500~1000ms)
    await tick(20); // 确认已进入等待（下一轮 connect 尚未发生）
    expect(connectCalls).toBe(1);

    const t0 = Date.now();
    await stop();
    const dt = Date.now() - t0;
    expect(dt).toBeLessThan(200); // stop 本体不阻塞
    await tick(60);
    // 唤醒后 stopped break：最多再发生 0 次 connect（唤醒即时收束，而非等满退避后重连）
    expect(connectCalls).toBe(1);
  });
});
