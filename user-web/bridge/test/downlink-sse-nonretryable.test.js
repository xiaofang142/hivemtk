// 不可重试的 SSE 错误不进重连梯子（2026-09-28）
//
// 起因：重连循环对所有失败一视同仁——先按 1s→30s 指数重打 10 次，再降 5min 档无限重试。
// 服务端 401「bridge token 无效」走的就是这条路：同一个坏请求被敲了十几次鉴权，日志里
// 十几条没有原因的 HTTP 401，而它永远不会自己变好（凭证不会在一次重连之间被换掉）。
// 反过来，一张断流的网卡绝不能用同一个慢档——那会把瞬时断流的恢复从 1s 拖到 60s。
// 两类的节奏必须分开，所以这里各自钉一条：
//   · 不可重试 ⇒ 降到 60s 慢档、原因（含服务端 message）只上交一次
//   · 可重试   ⇒ 仍走快速恢复期（秒级），不许被顺手一起降档
// 60s 而不是永久停手：SSE 是下行主通道，运维在选项页改对 token 后要能自己接上。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let live = null; // 当前挂起的 connectSSE { resolve, reject }
let connectCalls = 0;

// 可变的配置存储：自愈那条用例要在"被拒之后"把 token 改掉，再看下一次试探拿没拿到新值。
const store = { bridgeConfig: { serverUrl: 'http://127.0.0.1:8204', token: 't-old' } };
globalThis.chrome = {
  storage: {
    local: {
      async get(key) {
        return key in store ? { [key]: store[key] } : {};
      },
    },
  },
};

const ackOutbox = vi.fn(async (_cred, ids) => ({
  status: 'ok',
  items: (ids || []).map((id) => ({ msg_id: id, status: 'acked' })),
}));

vi.mock('../src/core/http-ingest.js', () => ({
  ackOutbox: (...args) => ackOutbox(...args),
  getOutbox: async () => ({ messages: [] }),
}));

vi.mock('../src/core/sse-fetch-client.js', () => ({
  connectSSE: (channel, accountId, opts) => {
    connectCalls++;
    return new Promise((resolve, reject) => {
      live = { resolve, reject, opts };
    });
  },
  getLastEventID: () => '',
  setLastEventID: () => {},
  stopSSE: () => false,
}));

const { startSSEDelivery, addPendingAck } = await import('../src/core/downlink.js');

const NON_RETRYABLE_RETRY_MS = 60_000;

function rejected401() {
  const err = new Error('SSE 连接失败: HTTP 401 — bridge token 无效');
  err.status = 401;
  err.code = 'UNAUTHORIZED_2001';
  err.serverMessage = 'bridge token 无效';
  err.nonRetryable = true;
  return err;
}

// 等到重连循环真正发起了一次 connect（假计时器下也能把微任务队列排空）
async function waitConnected() {
  for (let i = 0; i < 20 && !live; i++) {
    await vi.advanceTimersByTimeAsync(5);
  }
  expect(live).toBeTruthy();
}

describe('SSE 重连按可重试性分档', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    connectCalls = 0;
    live = null;
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it('4xx 不可重试：不进秒级指数梯，降到 60s 慢档，且到点自己再试', async () => {
    const stop = await startSSEDelivery('douyin', 'acc-nr', {
      sendOutbound: async () => ({ ok: true }),
    });
    await waitConnected();
    const first = live;
    connectCalls = 0;

    first.reject(rejected401());
    // 快速恢复期第一档是 500~1000ms：推进 10s 足以让旧节奏重打好几次
    await vi.advanceTimersByTimeAsync(10_000);
    expect(connectCalls).toBe(0);

    await vi.advanceTimersByTimeAsync(NON_RETRYABLE_RETRY_MS + 2_000);
    expect(connectCalls).toBe(1); // 慢档到点必须真重连（凭证改对后不用刷新页面）

    await stop();
  });

  it('可重试错误仍走快速恢复期（没被顺手一起降档）', async () => {
    const stop = await startSSEDelivery('douyin', 'acc-rt', {
      sendOutbound: async () => ({ ok: true }),
    });
    await waitConnected();
    const first = live;
    connectCalls = 0;

    first.reject(new Error('stream broke')); // 没有 nonRetryable 标记 = 断流
    await vi.advanceTimersByTimeAsync(5_000);
    expect(connectCalls).toBeGreaterThanOrEqual(1);

    await stop();
  });

  it('拒绝原因原样上交 handlers.onError（服务端 message 看得见、且不逐次刷屏）', async () => {
    const errors = [];
    const stop = await startSSEDelivery('douyin', 'acc-why', {
      sendOutbound: async () => ({ ok: true }),
      onError: (err) => errors.push(err),
    });
    await waitConnected();

    // 每次 reject 前把 live 摘干净：已 settle 的 promise 再 reject 是空操作，
    // 那样"第二次尝试"根本没发生，下面的"只播一次"就成了没牙的断言。
    const first = live;
    live = null;
    first.reject(rejected401());
    await vi.advanceTimersByTimeAsync(100);

    const why = errors.find((e) => e && e.nonRetryable);
    expect(why).toBeTruthy();
    expect(why.serverMessage).toBe('bridge token 无效');
    expect(why.code).toBe('UNAUTHORIZED_2001');
    expect(errors.filter((e) => e && e.nonRetryable)).toHaveLength(1);

    // 慢档到点、循环真的又试了一次 ⇒ 这才谈得上"第二次会不会重复播报"
    await vi.advanceTimersByTimeAsync(NON_RETRYABLE_RETRY_MS + 2_000);
    await waitConnected();
    const second = live;
    live = null;
    second.reject(rejected401());
    await vi.advanceTimersByTimeAsync(100);
    expect(errors.filter((e) => e && e.nonRetryable)).toHaveLength(1);

    await stop();
  });

  it('被拒后在选项页改对 token：下一次试探与 ack 排水都拿新凭证（不用刷新页面）', async () => {
    // 循环里的 serverUrl/token 是闭包值。快照成 const 的话，运维改对凭证这件事
    // 对这条连接永远不可见——"慢试探不永久放弃"就只是换个姿势卡死。
    // 排水同理：下行接上了而 ack 仍用旧凭证，行会一直留在欠投递集合里（服务端看就是"没投达"）。
    const ch = 'acc-heal-cfg';
    store.bridgeConfig = { serverUrl: 'http://127.0.0.1:8204', token: 't-old' };
    const stop = await startSSEDelivery(ch, 'acc-heal-cfg', {
      sendOutbound: async () => ({ ok: true }),
      pendingAckDrainIntervalMs: 120,
    });
    await waitConnected();
    expect(live.opts.token).toBe('t-old');

    const first = live;
    live = null;
    first.reject(rejected401());
    await vi.advanceTimersByTimeAsync(100);

    // 被拒之后、下一次试探之前，运维把凭证改对了
    store.bridgeConfig = { serverUrl: 'http://127.0.0.1:8204', token: 't-fixed' };

    await vi.advanceTimersByTimeAsync(NON_RETRYABLE_RETRY_MS + 2_000);
    await waitConnected();
    expect(live.opts.token).toBe('t-fixed');

    // 入队放在现在：早排一次就把它 ack 成功了，之后无项可排 ⇒ 判据会空转
    addPendingAck(ch, 'm-heal-1', 'sse_ack_failed', 'conv-heal');
    ackOutbox.mockClear();
    await vi.advanceTimersByTimeAsync(400);
    expect(ackOutbox.mock.calls.length).toBeGreaterThan(0);
    expect(ackOutbox.mock.calls[ackOutbox.mock.calls.length - 1][0].token).toBe('t-fixed');

    await stop();
  });
});
