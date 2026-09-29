import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  buildIngestUrl,
  buildAuthHeaders,
  describeIngestParams,
  previewBody,
  fetchWithRetry,
  postIngest,
  HTTP_INGEST_DEFAULTS,
  INGEST_PATH,
} from '../src/core/http-ingest.js';

describe('http-ingest / buildIngestUrl', () => {
  it('默认 baseUrl 拼接 /api/bridge/ingest（2026-08-14 P0-A：token 不在 URL）', () => {
    const url = buildIngestUrl('http://localhost:8204', {
      channel: 'xiaohongshu',
      accountId: 'acc-1',
      token: 'tkn-1234',
    });
    expect(url).toBe('http://localhost:8204/api/bridge/ingest?channel=xiaohongshu&account_id=acc-1');
    expect(url).not.toContain('token=');
  });

  it('ws:// 自动归一为 http://', () => {
    const url = buildIngestUrl('ws://localhost:8204', {
      channel: 'douyin',
      accountId: 'acc-2',
      token: 't',
    });
    expect(url.startsWith('http://localhost:8204/api/bridge/ingest?')).toBe(true);
    expect(url).not.toContain('token=');
  });

  it('wss:// 自动归一为 https://', () => {
    const url = buildIngestUrl('wss://api.example.com', {
      channel: 'tiktok',
      accountId: 'acc-3',
      token: 't',
    });
    expect(url.startsWith('https://api.example.com/api/bridge/ingest?')).toBe(true);
    expect(url).not.toContain('token=');
  });

  it('带 conversationId 时拼上', () => {
    const url = buildIngestUrl('http://x', {
      channel: 'xianyu',
      accountId: 'acc-4',
      conversationId: 'conv-9',
      token: 't',
    });
    expect(url).toContain('conversation_id=conv-9');
  });

  it('空 conversationId 不拼该参数', () => {
    const url = buildIngestUrl('http://x', {
      channel: 'xianyu',
      accountId: 'acc-5',
      conversationId: '',
      token: 't',
    });
    expect(url).not.toContain('conversation_id=');
  });

  it('2026-08-14 P0-A：无论 token 是否有值，都不进 URL', () => {
    const url1 = buildIngestUrl('http://x', { channel: 'c', accountId: 'a', token: '' });
    const url2 = buildIngestUrl('http://x', { channel: 'c', accountId: 'a', token: 'secret' });
    expect(url1).not.toContain('token=');
    expect(url2).not.toContain('token=');
  });

  it('channel 空字符串也允许（服务端会拒绝）', () => {
    const url = buildIngestUrl('http://x', {
      channel: '',
      accountId: 'acc-7',
      token: 't',
    });
    expect(url).toContain('channel=&');
  });

  it('INGEST_PATH 与 user-server 端点对齐', () => {
    expect(INGEST_PATH).toBe('/api/bridge/ingest');
  });

  it('HTTP_INGEST_DEFAULTS 关键字段冻结', () => {
    expect(HTTP_INGEST_DEFAULTS.longPollTimeoutMs).toBe(500000);
    expect(HTTP_INGEST_DEFAULTS.maxRetries).toBe(3);
    expect(HTTP_INGEST_DEFAULTS.retryBaseMs).toBe(1000);
    expect(HTTP_INGEST_DEFAULTS.maxContentBytes).toBe(4 * 1024);
  });
});

describe('http-ingest / buildAuthHeaders (2026-08-14 P0-A)', () => {
  it('有 token 时同时设 X-Bridge-Token 与 Authorization: Bearer <token>', () => {
    // X-Bridge-Token 是服务端 BridgeIngressGuard 唯一读取的头；缺它则 ingest/outbox/ack 全 401。
    const h = buildAuthHeaders('my-secret-token');
    expect(h['Content-Type']).toBe('application/json');
    expect(h['X-Bridge-Token']).toBe('my-secret-token');
    expect(h['Authorization']).toBe('Bearer my-secret-token');
  });

  it('空 token 不设凭证头（让服务端走匿名/默认分支）', () => {
    const h = buildAuthHeaders('');
    expect(h['Content-Type']).toBe('application/json');
    expect(h['Authorization']).toBeUndefined();
    expect(h['X-Bridge-Token']).toBeUndefined();
  });

  it('token 前后空白被 trim', () => {
    const h = buildAuthHeaders('  trimmed  ');
    expect(h['Authorization']).toBe('Bearer trimmed');
    expect(h['X-Bridge-Token']).toBe('trimmed');
  });
});

describe('http-ingest / describeIngestParams', () => {
  it('正常 URL 解析出 origin / path / query', () => {
    const url = buildIngestUrl('http://localhost:8204', {
      channel: 'douyin',
      accountId: 'a1',
      token: 'abcdef',
    });
    const desc = describeIngestParams(url);
    expect(desc.origin).toBe('http://localhost:8204');
    expect(desc.path).toBe('/api/bridge/ingest');
    expect(desc.query.channel).toBe('douyin');
    expect(desc.query.account_id).toBe('a1');
  });

  it('2026-08-14 P0-A：URL 中无 token → describeIngestParams 也无 token 字段', () => {
    const url = buildIngestUrl('http://localhost:8204', {
      channel: 'douyin',
      accountId: 'a2',
      token: 'abcdefghij',
    });
    const desc = describeIngestParams(url);
    expect(desc.query.token).toBeUndefined();
  });

  it('无 token 入参时 describeIngestParams 不写入 token', () => {
    const url = buildIngestUrl('http://localhost:8204', {
      channel: 'douyin',
      accountId: 'a3',
    });
    const desc = describeIngestParams(url);
    expect(desc.query.token).toBeUndefined();
  });

  it('非法 URL 走兜底（不抛）', () => {
    const desc = describeIngestParams('::not-a-url::');
    expect(desc.parseError).toBe(true);
  });
});

describe('http-ingest / previewBody', () => {
  it('短字符串原样返回', () => {
    const r = previewBody('hello', 4096);
    expect(r.preview).toBe('hello');
    expect(r.size).toBe(5);
    expect(r.truncated).toBe(false);
  });

  it('超过 maxBytes 截断并标记 truncated', () => {
    const big = 'x'.repeat(5000);
    const r = previewBody(big, 1024);
    expect(r.truncated).toBe(true);
    expect(r.size).toBe(5000);
    expect(r.preview.startsWith('x'.repeat(1024))).toBe(true);
    expect(r.preview).toContain('[truncated, total=5000 bytes]');
  });

  it('对象自动 JSON.stringify 后再截断', () => {
    const obj = { a: 'x'.repeat(2000) };
    const r = previewBody(obj, 512);
    expect(r.truncated).toBe(true);
    expect(r.size).toBeGreaterThan(2000);
  });

  it('null body 返回空预览', () => {
    const r = previewBody(null);
    expect(r.preview).toBe('');
    expect(r.size).toBe(0);
    expect(r.truncated).toBe(false);
  });

  it('undefined body 返回空预览', () => {
    const r = previewBody(undefined);
    expect(r.preview).toBe('');
    expect(r.size).toBe(0);
  });
});

describe('http-ingest / fetchWithRetry', () => {
  let realFetch;
  beforeEach(async () => {
    realFetch = globalThis.fetch;
    // 2026-08-15 P3-A：重置 CB 幂等键
    const cbMod = await import('../src/core/circuit-breaker.js');
    if (cbMod && cbMod.circuitBreaker) {
      cbMod.circuitBreaker._idempotencyKeys?.clear?.();
      cbMod.circuitBreaker.__reset?.();
    }
  });
  afterEach(() => {
    globalThis.fetch = realFetch;
    vi.restoreAllMocks();
  });

  it('首次成功（200 OK）不重试', async () => {
    const calls = [];
    globalThis.fetch = vi.fn(async (url) => {
      calls.push(url);
      return new Response('{}', { status: 200 });
    });
    const res = await fetchWithRetry('http://x/y', {}, { maxRetries: 3, retryBaseMs: 1 });
    expect(res.status).toBe(200);
    expect(calls.length).toBe(1);
  });

  it('5xx 失败时按指数退避重试', async () => {
    let n = 0;
    globalThis.fetch = vi.fn(async () => {
      n += 1;
      if (n < 3) return new Response('fail', { status: 503 });
      return new Response('{}', { status: 200 });
    });
    const res = await fetchWithRetry('http://x/y', {}, { maxRetries: 3, retryBaseMs: 1 });
    expect(res.status).toBe(200);
    expect(n).toBe(3);
  });

  it('4xx（除 408）不重试，直接抛错', async () => {
    let n = 0;
    globalThis.fetch = vi.fn(async () => {
      n += 1;
      return new Response('bad', { status: 400 });
    });
    await expect(fetchWithRetry('http://x/y', {}, { maxRetries: 3, retryBaseMs: 1 })).rejects.toThrow(/HTTP 400/);
    expect(n).toBe(1);
  });

  it('408（timeout）允许重试', async () => {
    let n = 0;
    globalThis.fetch = vi.fn(async () => {
      n += 1;
      if (n === 1) return new Response('timeout', { status: 408 });
      return new Response('{}', { status: 200 });
    });
    const res = await fetchWithRetry('http://x/y', {}, { maxRetries: 3, retryBaseMs: 1 });
    expect(res.status).toBe(200);
    expect(n).toBe(2);
  });

  it('429（Too Many Requests）允许重试（不标 nonRetryable）', async () => {
    let n = 0;
    let lastErr = null;
    globalThis.fetch = vi.fn(async () => {
      n += 1;
      if (n === 1) {
        return new Response('rate', { status: 429, headers: { 'Retry-After': '1' } });
      }
      return new Response('{}', { status: 200 });
    });
    const res = await fetchWithRetry('http://x/y', {}, { maxRetries: 3, retryBaseMs: 1 }).catch((e) => { lastErr = e; return null; });
    expect(res && res.status).toBe(200);
    expect(n).toBe(2);
    expect(lastErr).toBeNull();
  });

  it('429 超过 maxRetries 最终抛出且带 retryAfterMs', async () => {
    globalThis.fetch = vi.fn(async () => new Response('rate', { status: 429, headers: { 'Retry-After': '1' } }));
    let caught = null;
    await fetchWithRetry('http://x/y', {}, { maxRetries: 2, retryBaseMs: 1 }).catch((e) => { caught = e; });
    expect(caught).not.toBeNull();
    expect(caught.message).toMatch(/HTTP 429/);
    expect(caught.nonRetryable).toBeFalsy();
    expect(caught.retryAfterMs).toBe(1000);
  });

  it('400（客户端错误）仍不可重试', async () => {
    let n = 0;
    globalThis.fetch = vi.fn(async () => {
      n += 1;
      return new Response('bad', { status: 400 });
    });
    await expect(fetchWithRetry('http://x/y', {}, { maxRetries: 3, retryBaseMs: 1 })).rejects.toThrow(/HTTP 400/);
    expect(n).toBe(1);
  });

  it('超过 maxRetries 抛出最后一次错误', async () => {
    globalThis.fetch = vi.fn(async () => {
      return new Response('boom', { status: 500 });
    });
    await expect(
      fetchWithRetry('http://x/y', {}, { maxRetries: 2, retryBaseMs: 1 })
    ).rejects.toThrow(/HTTP 500/);
  });

  it('fetch reject（网络错误）触发重试退避', async () => {
    let n = 0;
    globalThis.fetch = vi.fn(async () => {
      n += 1;
      if (n < 2) throw new TypeError('Failed to fetch');
      return new Response('{}', { status: 200 });
    });
    const res = await fetchWithRetry('http://x/y', {}, { maxRetries: 3, retryBaseMs: 1 });
    expect(res.status).toBe(200);
    expect(n).toBe(2);
  });
});

describe('http-ingest / postIngest', () => {
  let realFetch;
  beforeEach(async () => {
    realFetch = globalThis.fetch;
    // 2026-08-15 P3-A：重置 CB 幂等键，避免跨测试污染导致同 idempotencyKey 被短路
    const cbMod = await import('../src/core/circuit-breaker.js');
    if (cbMod && cbMod.circuitBreaker) {
      cbMod.circuitBreaker._idempotencyKeys?.clear?.();
      cbMod.circuitBreaker.__reset?.();
    }
  });
  afterEach(() => {
    globalThis.fetch = realFetch;
    vi.restoreAllMocks();
  });

  it('成功：构造 body 投递 + 解析响应', async () => {
    let captured;
    globalThis.fetch = vi.fn(async (url, init) => {
      captured = { url, init };
      return new Response(
        JSON.stringify({
          ok: true,
          ingested: [{ event_id: 'e1', accepted: true }],
          outbound_replies: [],
          server_time: 1700000000,
        }),
        { status: 200, headers: { 'Content-Type': 'application/json' } }
      );
    });
    const body = {
      v: 2,
      channel: 'xiaohongshu',
      account_id: 'acc-x',
      conversation_id: 'conv-x',
      messages: [
        {
          event_id: 'e1',
          conversation_id: 'conv-x',
          sender_id: 'peer-1',
          msg_type: 'text',
          content: '你好',
          timestamp: 1700000000,
        },
      ],
      expect_reply: true,
      timeout_ms: 30000,
    };
    const resp = await postIngest(
      {
        serverUrl: 'http://localhost:8204',
        channel: 'xiaohongshu',
        accountId: 'acc-x',
        conversationId: 'conv-x',
        token: 'tkn-xyz',
      },
      body,
      { timeoutMs: 5000 }
    );
    expect(resp.ok).toBe(true);
    expect(resp.ingested[0].event_id).toBe('e1');
    expect(captured.url).toContain('/api/bridge/ingest');
    expect(captured.url).toContain('channel=xiaohongshu');
    expect(captured.url).toContain('account_id=acc-x');
    expect(captured.url).toContain('conversation_id=conv-x');
    expect(captured.url).not.toContain('token=');
    expect(captured.init.headers['X-Bridge-Token']).toBe('tkn-xyz');
    expect(captured.init.headers['Authorization']).toBe('Bearer tkn-xyz');
    expect(captured.init.method).toBe('POST');
    expect(captured.init.headers['Content-Type']).toBe('application/json');
    const parsedBody = JSON.parse(captured.init.body);
    expect(parsedBody.messages[0].content).toBe('你好');
  });

  it('非 JSON 响应抛错（包含部分文本）', async () => {
    globalThis.fetch = vi.fn(async () => new Response('not json', { status: 200 }));
    await expect(
      postIngest(
        { serverUrl: 'http://x', channel: 'c', accountId: 'a', token: 't' },
        { messages: [] },
        { timeoutMs: 5000 }
      )
    ).rejects.toThrow(/非 JSON/);
  });

  it('服务端 5xx 抛错（走 fetchWithRetry 退避后失败）', async () => {
    globalThis.fetch = vi.fn(async () => new Response('err', { status: 500 }));
    await expect(
      postIngest(
        { serverUrl: 'http://x', channel: 'c', accountId: 'a', token: 't' },
        { messages: [] },
        { timeoutMs: 5000, label: 'test', maxRetries: 0, retryBaseMs: 1 }
      )
    ).rejects.toThrow(/HTTP 500/);
  });

  it('请求体含 expect_reply=true 时 headers 透传 traceId', async () => {
    let captured;
    globalThis.fetch = vi.fn(async (url, init) => {
      captured = { url, init };
      return new Response('{"ok":true,"ingested":[]}', { status: 200 });
    });
    await postIngest(
      { serverUrl: 'http://x', channel: 'c', accountId: 'a', token: 't' },
      { expect_reply: true, messages: [{ event_id: 'e1' }] },
      { timeoutMs: 5000, traceId: 'trace-xyz' }
    );
    expect(captured.init.headers['X-Trace-Id']).toBe('trace-xyz');
  });

  it('响应包含 outbound_replies 时能被读取', async () => {
    globalThis.fetch = vi.fn(async () =>
      new Response(
        JSON.stringify({
          ok: true,
          ingested: [{ event_id: 'e1', accepted: true, ai_handled: true }],
          outbound_replies: [
            {
              channel: 'xiaohongshu',
              conversation_id: 'conv-x',
              content: 'AI 回复内容',
              msg_type: 'text',
              reply_to_event_id: 'e1',
            },
          ],
        }),
        { status: 200 }
      )
    );
    const resp = await postIngest(
      { serverUrl: 'http://x', channel: 'xiaohongshu', accountId: 'a', conversationId: 'conv-x', token: 't' },
      { messages: [{ event_id: 'e1' }] },
      { timeoutMs: 5000 }
    );
    expect(resp.outbound_replies).toHaveLength(1);
    expect(resp.outbound_replies[0].content).toBe('AI 回复内容');
    expect(resp.outbound_replies[0].reply_to_event_id).toBe('e1');
  });
});

