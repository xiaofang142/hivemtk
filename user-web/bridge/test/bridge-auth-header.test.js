// 桥接凭证头契约测试（2026-09-28）
//
// 起因：服务端闸门 middleware.BridgeIngressGuard 只认 `X-Bridge-Token` 头或 `bridge_token`
// 查询参数（router.go 把它挂在 /api/bridge/ingest|outbox|outbox/ack|outbox/sse|capabilities 上），
// 而扩展历史上只发 `Authorization: Bearer`。凭证在这五条路由上没有任何读取点 ⇒ 部署了通道凭证后
// 每一次上行/下行/ack/SSE 都是 401，用户看到的是"扩展已连接却一条也不同步"。
// 本文件把"每个 bridge 请求都带上服务端真正读取的凭证"钉成断言，防止再次回归。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

import { postIngest, getOutbox, ackOutbox, buildAuthHeaders } from '../src/core/http-ingest.js';
import { connectSSE } from '../src/core/sse-fetch-client.js';
import { getServerCapabilities } from '../src/core/polling-loop.js';

const TOKEN = 'bridge-secret-xyz';
const SERVER = 'http://localhost:8204';

let captured = [];

function stubFetch(responseFactory) {
  captured = [];
  globalThis.fetch = vi.fn(async (url, init = {}) => {
    captured.push({ url: String(url), init });
    return responseFactory ? responseFactory(url, init) : new Response('{}', { status: 200 });
  });
}

// 一条 new_outbound 事件后即关闭流：connectSSE 会 resolve 停止函数，测试可即时收束。
function sseStream() {
  return new Response(
    new ReadableStream({
      start(controller) {
        controller.enqueue(new TextEncoder().encode('id: 7\nevent: new_outbound\ndata: {"msg_id":"mh:1"}\n\n'));
        controller.close();
      },
    }),
    { status: 200, headers: { 'Content-Type': 'text/event-stream' } }
  );
}

describe('凭证头单源构造 buildAuthHeaders', () => {
  it('X-Bridge-Token 与 Authorization 同值', () => {
    const h = buildAuthHeaders(TOKEN);
    expect(h['X-Bridge-Token']).toBe(TOKEN);
    expect(h['Authorization']).toBe(`Bearer ${TOKEN}`);
  });
});

describe('通道A/B/C 的 HTTP 请求带服务端可读凭证', () => {
  beforeEach(() => stubFetch());
  afterEach(() => delete globalThis.fetch);

  const ingestBody = {
    v: 2,
    channel: 'xiaohongshu',
    account_id: 'acc-1',
    messages: [
      {
        event_id: 'e1',
        conversation_id: 'conv-1',
        sender_id: 'cust-1',
        sender_name: '访客',
        msg_type: 'text',
        content: '你好',
        timestamp: 1700000000000,
      },
    ],
  };

  it('POST /api/bridge/ingest', async () => {
    stubFetch(() =>
      new Response(JSON.stringify({ ok: true, ingested: [], server_time: 1 }), { status: 200 })
    );
    await postIngest(
      { serverUrl: SERVER, channel: 'xiaohongshu', accountId: 'acc-1', conversationId: 'conv-1', token: TOKEN },
      ingestBody,
      { timeoutMs: 2000 }
    );
    expect(captured).toHaveLength(1);
    expect(captured[0].url).toContain('/api/bridge/ingest');
    expect(captured[0].init.headers['X-Bridge-Token']).toBe(TOKEN);
    // 凭证绝不进 URL（会落进访问日志与浏览器历史）
    expect(captured[0].url).not.toContain(TOKEN);
  });

  it('GET /api/bridge/outbox', async () => {
    stubFetch(() => new Response(JSON.stringify({ status: 'ok', messages: [] }), { status: 200 }));
    await getOutbox({ serverUrl: SERVER, channel: 'douyin', accountId: 'acc-1', token: TOKEN });
    expect(captured[0].url).toContain('/api/bridge/outbox');
    expect(captured[0].init.headers['X-Bridge-Token']).toBe(TOKEN);
    expect(captured[0].url).not.toContain(TOKEN);
  });

  it('POST /api/bridge/outbox/ack', async () => {
    stubFetch(() => new Response(JSON.stringify({ status: 'ok', acked_items_count: 1 }), { status: 200 }));
    await ackOutbox(
      { serverUrl: SERVER, channel: 'douyin', accountId: 'acc-1', token: TOKEN },
      ['mh:00550fed'],
      { conversationId: 'conv-1' }
    );
    expect(captured[0].url).toContain('/api/bridge/outbox/ack');
    expect(captured[0].init.headers['X-Bridge-Token']).toBe(TOKEN);
    expect(captured[0].url).not.toContain(TOKEN);
    expect(JSON.parse(captured[0].init.body).items[0].conversation_id).toBe('conv-1');
  });

  it('未配置 token 时不伪造空头（服务端 fail-closed 的报错才有意义）', async () => {
    await postIngest(
      { serverUrl: SERVER, channel: 'xiaohongshu', accountId: 'acc-1', conversationId: 'conv-1', token: '' },
      ingestBody,
      { timeoutMs: 2000 }
    );
    expect(captured[0].init.headers['X-Bridge-Token']).toBeUndefined();
  });
});

describe('SSE 下行（fetch 版）带服务端可读凭证', () => {
  afterEach(() => delete globalThis.fetch);

  it('GET /api/bridge/outbox/sse 走 X-Bridge-Token 头', async () => {
    stubFetch(sseStream);
    await connectSSE('tiktok', 'acc-1', { serverUrl: SERVER, token: TOKEN, onMessage: () => {} });
    expect(captured).toHaveLength(1);
    expect(captured[0].url).toContain('/api/bridge/outbox/sse');
    expect(captured[0].init.headers['X-Bridge-Token']).toBe(TOKEN);
    expect(captured[0].url).not.toContain(TOKEN);
  });
});

describe('能力探测（决定走 SSE 还是轮询）带服务端可读凭证', () => {
  afterEach(() => delete globalThis.fetch);

  it('GET /api/bridge/capabilities 带 X-Bridge-Token，且 sse_enabled 透传', async () => {
    stubFetch(() =>
      new Response(JSON.stringify({ poll_interval_ms: 1200, sse_enabled: true, sse_heartbeat_ms: 15000 }), { status: 200 })
    );
    const caps = await getServerCapabilities({ serverUrl: SERVER, token: TOKEN });
    expect(captured[0].init.headers['X-Bridge-Token']).toBe(TOKEN);
    expect(caps.sse_enabled).toBe(true);
    expect(caps.poll_interval_ms).toBe(1200);
  });

  it('401 时按 sse_enabled=false 保守降级（不抛错打断巡检）', async () => {
    stubFetch(() => new Response('{"code":"UNAUTHORIZED_2001"}', { status: 401 }));
    const caps = await getServerCapabilities({ serverUrl: SERVER, token: 'wrong' });
    expect(caps.sse_enabled).toBe(false);
  });
});

describe('background EventSource 版 SSE（不支持自定义头）走 bridge_token 查询参数', () => {
  const listeners = [];

  beforeEach(() => {
    listeners.length = 0;
    globalThis.chrome = {
      runtime: { onMessage: { addListener: (fn) => listeners.push(fn) } },
      storage: { local: { get: (key, cb) => cb({ bridgeConfig: { serverUrl: SERVER, token: TOKEN } }) } },
    };
    captured = [];
    globalThis.fetch = vi.fn(async (url, init = {}) => {
      captured.push({ url: String(url), init });
      return new Response(JSON.stringify({ status: 'ok', acked_items_count: 1 }), { status: 200 });
    });
    globalThis.EventSource = class {
      constructor(url) {
        this.url = url;
        globalThis.__lastEventSource = this;
      }
      addEventListener() {}
      close() {}
    };
  });

  afterEach(() => {
    delete globalThis.chrome;
    delete globalThis.fetch;
    delete globalThis.EventSource;
    delete globalThis.__lastEventSource;
  });

  async function loadBackgroundSSE() {
    vi.resetModules();
    await import('../src/background/sse-client.js');
  }

  it('注册 tab 建立的 EventSource URL 用 bridge_token=（服务端读的键名），不是 token=', async () => {
    await loadBackgroundSSE();
    for (const fn of listeners) {
      fn({ type: 'BRIDGE_REGISTER_TAB', channel: 'xianyu', accountId: 'acc-1' }, { tab: { id: 1 } }, () => {});
    }
    await new Promise((r) => setTimeout(r, 30));
    const es = globalThis.__lastEventSource;
    expect(es).toBeTruthy();
    expect(es.url).toContain('/api/bridge/outbox/sse');
    expect(es.url).toContain(`bridge_token=${encodeURIComponent(TOKEN)}`);
    expect(es.url).not.toMatch(/[?&]token=/);
  });

  it('SSE ack 回发带 X-Bridge-Token 头', async () => {
    await loadBackgroundSSE();
    for (const fn of listeners) {
      fn(
        { type: 'BRIDGE_SSE_ACK', channel: 'xianyu', accountId: 'acc-1', msgIds: ['mh:1'] },
        { tab: { id: 1 } },
        () => {}
      );
    }
    await new Promise((r) => setTimeout(r, 30));
    expect(captured).toHaveLength(1);
    expect(captured[0].url).toContain('/api/bridge/outbox/ack');
    expect(captured[0].init.headers['X-Bridge-Token']).toBe(TOKEN);
    expect(captured[0].url).not.toContain(TOKEN);
  });
});
