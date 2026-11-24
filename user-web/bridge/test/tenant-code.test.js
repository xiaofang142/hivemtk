import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  applyTenantCode,
  buildIngestUrl,
  postIngest,
  getOutbox,
  ackOutbox,
} from '../src/core/http-ingest.js';
import { connectSSE } from '../src/core/sse-fetch-client.js';
import { buildDefaultConfig, configStore } from '../src/core/config-store.js';
import { serializeConfig, importConfig } from '../src/popup/config-io.js';

// tenant_code（SaaS 租户短码）贯通测试。
//
// 契约服务端：user-server/internal/bridge/tenant_binding.go
//   - 短码从 query 读，其次 X-Tenant-Code 头（bridgeTenantHintFromRequest）
//   - 账号行不存在且无短码 ⇒ 404 ErrBridgeTenantUnresolved（首次上报死锁）
//   - 账号行不存在且有短码 ⇒ 绑定该租户，正常 200
//
// 覆盖四条链路：ingest（上行）、outbox（下行轮询）、ack（回执）、sse（下行推送）。

describe('tenant_code / applyTenantCode', () => {
  it('带短码 ⇒ URL 上多出 tenant_code 参数', () => {
    const url = applyTenantCode('http://localhost:8204/api/bridge/ingest?channel=douyin', 't42xk');
    expect(url).toContain('tenant_code=t42xk');
    expect(url).toContain('channel=douyin');
  });

  it('短码为空 ⇒ URL 不变（离线单机模式不带多余参数）', () => {
    const base = 'http://localhost:8204/api/bridge/ingest?channel=douyin';
    expect(applyTenantCode(base, '')).toBe(base);
    expect(applyTenantCode(base, undefined)).toBe(base);
    expect(applyTenantCode(base, '   ')).toBe(base);
  });

  it('短码做 URL 编码（短码本就是随机串，但格式不该假设）', () => {
    const url = applyTenantCode('http://localhost:8204/x', 'a b&c');
    expect(url).toContain('tenant_code=a+b%26c');
  });
});

describe('tenant_code / config-store', () => {
  it('默认配置带 tenantCode 空串（不是 undefined）', () => {
    expect(buildDefaultConfig().tenantCode).toBe('');
  });

  it('合并持久化配置时把非字符串 tenantCode 归一成空串', () => {
    const merged = configStore._mergeWithDefault({ tenantCode: null });
    expect(merged.tenantCode).toBe('');
    const merged2 = configStore._mergeWithDefault({ tenantCode: 123 });
    expect(merged2.tenantCode).toBe('');
    const merged3 = configStore._mergeWithDefault({ tenantCode: '  t42xk  ' });
    expect(merged3.tenantCode).toBe('t42xk');
  });
});

describe('tenant_code / 配置导入导出', () => {
  it('导出带 tenantCode', () => {
    const out = serializeConfig({ serverUrl: 'http://x', token: 'tk', tenantCode: 't42xk' });
    expect(out.tenantCode).toBe('t42xk');
  });

  it('导入把 tenantCode 写回配置', async () => {
    const store = { cfg: {}, async set(patch) { this.cfg = { ...this.cfg, ...patch }; return this.cfg; } };
    const res = await importConfig(
      JSON.stringify({ schema: 1, encrypted: false, data: { serverUrl: 'http://x', tenantCode: 't42xk' } }),
      '',
      { store }
    );
    expect(res.ok).toBe(true);
    expect(store.cfg.tenantCode).toBe('t42xk');
  });
});

describe('tenant_code / 四条链路都带上短码', () => {
  let fetchMock;
  const calls = [];

  beforeEach(() => {
    calls.length = 0;
    fetchMock = vi.fn(async (url, opts) => {
      calls.push({ url: String(url), opts });
      return {
        ok: true,
        status: 200,
        headers: { get: () => null },
        text: async () => JSON.stringify({ ok: true, status: 'ok', ingested: [], messages: [] }),
        body: null,
      };
    });
    global.fetch = fetchMock;
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('ingest 带上 tenant_code', async () => {
    await postIngest(
      {
        serverUrl: 'http://localhost:8204',
        channel: 'douyin',
        accountId: 'acc-1',
        conversationId: 'conv-1',
        token: 'tk',
        tenantCode: 't42xk',
      },
      { v: 2, messages: [] }
    );
    expect(calls[0].url).toContain('tenant_code=t42xk');
    expect(calls[0].url).toContain('/api/bridge/ingest');
  });

  it('outbox 带上 tenant_code', async () => {
    await getOutbox(
      { serverUrl: 'http://localhost:8204', channel: 'douyin', accountId: 'acc-1', token: 'tk', tenantCode: 't42xk' },
      { batchSize: 10 }
    );
    expect(calls[0].url).toContain('/api/bridge/outbox');
    expect(calls[0].url).toContain('tenant_code=t42xk');
  });

  it('ack 带上 tenant_code', async () => {
    await ackOutbox(
      { serverUrl: 'http://localhost:8204', channel: 'douyin', accountId: 'acc-1', token: 'tk', tenantCode: 't42xk' },
      ['mh:abc'],
      { conversationId: 'conv-1' }
    );
    expect(calls[0].url).toContain('/api/bridge/outbox/ack');
    expect(calls[0].url).toContain('tenant_code=t42xk');
  });

  it('sse 带上 tenant_code', async () => {
    // SSE 客户端建连后即读流；给一个空 body 让它立刻结束。
    global.fetch = vi.fn(async (url) => {
      calls.push({ url: String(url) });
      return {
        ok: true,
        status: 200,
        headers: { get: () => null },
        body: { getReader: () => ({ read: async () => ({ done: true }) }) },
      };
    });
    await connectSSE('douyin', 'acc-1', {
      serverUrl: 'http://localhost:8204',
      token: 'tk',
      tenantCode: 't42xk',
    });
    expect(calls[0].url).toContain('/api/bridge/outbox/sse');
    expect(calls[0].url).toContain('tenant_code=t42xk');
  });

  it('短码为空时四条链路都不带 tenant_code 参数', async () => {
    await postIngest(
      { serverUrl: 'http://localhost:8204', channel: 'douyin', accountId: 'a', conversationId: 'c', token: 'tk' },
      { v: 2, messages: [] }
    );
    await getOutbox({ serverUrl: 'http://localhost:8204', channel: 'douyin', accountId: 'a', token: 'tk' });
    await ackOutbox({ serverUrl: 'http://localhost:8204', channel: 'douyin', accountId: 'a', token: 'tk' }, ['m']);
    for (const c of calls) {
      expect(c.url).not.toContain('tenant_code');
    }
  });

  it('token 始终走 Header，不因加了短码而漏进 URL', async () => {
    await postIngest(
      {
        serverUrl: 'http://localhost:8204',
        channel: 'douyin',
        accountId: 'a',
        conversationId: 'c',
        token: 'secret-token',
        tenantCode: 't42xk',
      },
      { v: 2, messages: [] }
    );
    expect(calls[0].url).not.toContain('secret-token');
    expect(calls[0].opts.headers['X-Channel-Identify-Code']).toBe('secret-token');
  });
});
