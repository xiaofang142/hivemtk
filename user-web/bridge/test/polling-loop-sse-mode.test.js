// 下行形态选择（SSE 默认 / 轮询兜底）
//
// getServerCapabilities 的读数对不对，另有凭证契约测试盯着；这里盯的是**下游决策**：
// 探到 sse_enabled=true 后 PollingLoop 必须真的走 SSE，且一个轮询定时器都不建。
// 运维手册（docs/operations/Bridge_Runbook.md §0.2）与本扩展 bridge.md §4.5 写着"默认是 SSE、
// 轮询只在三种情况下才启动"，那三种情况各占下面一格；缺了这层，文档那句话只剩构造函数里的
// 一个三元表达式撑着，而把 preferSSE 的默认值改掉是不会有任何测试变红的。
import { describe, it, expect, vi, afterEach } from 'vitest';

const mocks = vi.hoisted(() => ({
  initDownlink: vi.fn(),
  pollDownlink: vi.fn(async () => {}),
  startSSEDelivery: vi.fn(async () => () => {}),
}));

vi.mock('../src/core/downlink.js', () => ({
  initDownlink: mocks.initDownlink,
  pollDownlink: mocks.pollDownlink,
  startSSEDelivery: mocks.startSSEDelivery,
}));

const { PollingLoop } = await import('../src/core/polling-loop.js');

const SERVER = 'http://127.0.0.1:8204';
const TOKEN = 'bridge-secret-xyz';
const getMeta = () => ({ accountId: 'acc-1' });
const getConfig = async () => ({ serverUrl: SERVER, token: TOKEN });

function stubCapabilities(body, status = 200) {
  globalThis.fetch = vi.fn(async () =>
    new Response(typeof body === 'string' ? body : JSON.stringify(body), { status })
  );
}

function newLoop() {
  return new PollingLoop({
    channels: ['douyin'],
    getAdapter: () => null,
    getConfig,
    getMeta,
  });
}

afterEach(() => {
  delete globalThis.fetch;
  mocks.initDownlink.mockClear();
  mocks.pollDownlink.mockClear();
  mocks.startSSEDelivery.mockClear();
  mocks.startSSEDelivery.mockImplementation(async () => () => {});
});

describe('polling-loop / 下行形态选择', () => {
  it('服务端报 sse_enabled=true ⇒ 走 SSE 且不建轮询定时器', async () => {
    stubCapabilities({ poll_interval_ms: 1500, sse_enabled: true, sse_heartbeat_ms: 15000 });
    const loop = newLoop();
    await loop.start();
    try {
      expect(loop._sseActive).toBe(true);
      expect(loop._downlinkTimer).toBeNull();
      expect(mocks.startSSEDelivery).toHaveBeenCalledTimes(1);
      expect(mocks.startSSEDelivery.mock.calls[0][0]).toBe('douyin');
      expect(mocks.startSSEDelivery.mock.calls[0][1]).toBe('acc-1');
    } finally {
      await loop.stop();
    }
  });

  it('capabilities 回 401 ⇒ 保守降到轮询（凭证错不能把下行整体卡死）', async () => {
    stubCapabilities('{"code":"UNAUTHORIZED_2001"}', 401);
    const loop = newLoop();
    await loop.start();
    try {
      expect(loop._sseActive).toBe(false);
      expect(loop._downlinkTimer).not.toBeNull();
      expect(mocks.startSSEDelivery).not.toHaveBeenCalled();
    } finally {
      await loop.stop();
    }
  });

  it('探测请求本身抛错（网络不通）⇒ 同样降到轮询', async () => {
    globalThis.fetch = vi.fn(async () => {
      throw new Error('network down');
    });
    const loop = newLoop();
    await loop.start();
    try {
      expect(loop._sseActive).toBe(false);
      expect(loop._downlinkTimer).not.toBeNull();
    } finally {
      await loop.stop();
    }
  });

  it('SSE 报开但每个渠道都启动失败 ⇒ 兜底起轮询，而不是"两都不跑"', async () => {
    stubCapabilities({ poll_interval_ms: 1500, sse_enabled: true, sse_heartbeat_ms: 15000 });
    mocks.startSSEDelivery.mockImplementation(async () => {
      throw new Error('SSE 建连失败');
    });
    const loop = newLoop();
    await loop.start();
    try {
      // 先证明这格真的走过了 SSE 分支：只断"最后有轮询"的话，
      // 一条根本没尝试 SSE 直接落轮询的路径也会绿，兜底逻辑删掉都不会有人知道。
      expect(mocks.startSSEDelivery).toHaveBeenCalledTimes(1);
      expect(loop._sseCleanups.size).toBe(0);
      expect(loop._sseActive).toBe(false);
      expect(loop._downlinkTimer).not.toBeNull();
    } finally {
      await loop.stop();
    }
  });

  it('非 OK 响应即使 body 里写着 sse_enabled=true 也不采信', async () => {
    // 反代/网关会带着上游 body 回一个 5xx（上游刚恢复、网关仍报旧错），
    // 采信这种读数等于把下行挂到一条根本没建立的长连接上，且不会再有任何报错。
    stubCapabilities('{"sse_enabled":true}', 502);
    const loop = newLoop();
    await loop.start();
    try {
      expect(loop._sseActive).toBe(false);
      expect(mocks.startSSEDelivery).not.toHaveBeenCalled();
      expect(loop._downlinkTimer).not.toBeNull();
    } finally {
      await loop.stop();
    }
  });

  it('preferSSE:false ⇒ 不探测能力，直接轮询（老部署的显式退路）', async () => {
    stubCapabilities({ poll_interval_ms: 1500, sse_enabled: true, sse_heartbeat_ms: 15000 });
    const loop = new PollingLoop({
      channels: ['douyin'],
      getAdapter: () => null,
      getConfig,
      getMeta,
      preferSSE: false,
    });
    await loop.start();
    try {
      expect(globalThis.fetch).not.toHaveBeenCalled();
      expect(loop._downlinkTimer).not.toBeNull();
      expect(mocks.startSSEDelivery).not.toHaveBeenCalled();
    } finally {
      await loop.stop();
    }
  });
});
