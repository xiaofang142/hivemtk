// @vitest-environment node
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

describe('HTTP 中转只允许已配置的桥接边界', () => {
  let relay;
  let cancel;
  let fetcher;
  let cfg;
  let sender;
  let message;

  beforeEach(async () => {
    vi.resetModules();
    vi.useFakeTimers();
    cfg = { serverUrl: 'https://service.example/base', token: 'stored-secret', tenantCode: 'tenant-a' };
    sender = { id: 'bridge-extension', tab: { id: 7 }, frameId: 0, documentId: 'doc-a', url: 'https://www.douyin.com/chat' };
    message = {
      requestId: 'boundary-1',
      url: 'https://service.example/base/api/bridge/outbox?channel=douyin&account_id=account-a&tenant_code=tenant-a',
      options: { method: 'GET', headers: { 'X-Bridge-Token': 'stored-secret', 'X-Untrusted': 'do-not-forward' } },
    };
    fetcher = vi.fn(async () => new Response('{"status":"ok"}', { headers: { 'content-type': 'application/json' } }));
    vi.stubGlobal('fetch', fetcher);
    vi.stubGlobal('chrome', { runtime: { id: sender.id }, storage: { local: { get: vi.fn(async () => ({ bridgeConfig: cfg })) } } });
    ({ relayHttpRequest: relay, cancelHttpRelay: cancel } = await import('../src/background/http-relay.js'));
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('合法同源接口使用存储认证，不能透传页面凭证或任意头', async () => {
    expect((await relay(message, sender)).status).toBe(200);
    const options = fetcher.mock.calls[0][1];
    const headers = new Headers(options.headers);
    expect(headers.get('X-Bridge-Token')).toBe(cfg.token);
    expect(headers.get('Authorization')).toBe(`Bearer ${cfg.token}`);
    expect(headers.has('X-Untrusted')).toBe(false);
    expect(options.redirect).toBe('error');
    expect(options.credentials).toBe('omit');
    expect(vi.getTimerCount()).toBe(0);
  });

  it.each([
    ['其它扩展', (m, s) => [m, { ...s, id: 'other-extension' }]],
    ['过期凭证', (m, s) => [{ ...m, options: { ...m.options, headers: { 'X-Bridge-Token': 'stale-secret' } } }, s]],
    ['子帧', (m, s) => [m, { ...s, frameId: 1 }]],
    ['不支持的页面', (m, s) => [m, { ...s, url: 'https://untrusted.example/' }]],
    ['跨目标源', (m, s) => [{ ...m, url: m.url.replace('service.example', 'other.example') }, s]],
    ['非桥接路径', (m, s) => [{ ...m, url: m.url.replace('/api/bridge/outbox', '/admin/delete') }, s]],
    ['错误方法', (m, s) => [{ ...m, options: { method: 'DELETE' } }, s]],
    ['跨渠道', (m, s) => [{ ...m, url: m.url.replace('channel=douyin', 'channel=xiaohongshu') }, s]],
    ['跨租户', (m, s) => [{ ...m, url: m.url.replace('tenant_code=tenant-a', 'tenant_code=tenant-b') }, s]],
    ['地址含凭证', (m, s) => [{ ...m, url: m.url.replace('https://', 'https://user:password@') }, s]],
    ['重复参数', (m, s) => [{ ...m, url: `${m.url}&account_id=account-b` }, s]],
  ])('%s 拒绝且不发网络请求', async (_label, mutate) => {
    const [request, origin] = mutate(message, sender);
    expect(await relay(request, origin)).toHaveProperty('__err');
    expect(fetcher).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  it('配置读取期间取消，晚到配置不能复活请求', async () => {
    let release;
    chrome.storage.local.get.mockImplementation(() => new Promise(resolve => { release = resolve; }));
    const promise = relay(message, sender);
    expect(cancel(message.requestId, sender)).toBe(true);
    release({ bridgeConfig: cfg });
    expect(await promise).toMatchObject({ errorName: 'AbortError' });
    expect(fetcher).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  it('底层配置读取不结算时，取消仍立即释放监听器和容量', async () => {
    chrome.storage.local.get.mockImplementation(() => new Promise(() => {}));
    const added = vi.spyOn(AbortSignal.prototype, 'addEventListener');
    const removed = vi.spyOn(AbortSignal.prototype, 'removeEventListener');
    try {
      const promise = relay(message, sender);
      expect(cancel(message.requestId, sender)).toBe(true);
      expect(await promise).toMatchObject({ errorName: 'AbortError' });
      expect(fetcher).not.toHaveBeenCalled();
      expect(vi.getTimerCount()).toBe(0);
      for (const [index, [type, listener]] of added.mock.calls.entries()) {
        if (type !== 'abort') continue;
        expect(removed.mock.calls.some(([removedType, removedListener], removalIndex) =>
          removedType === 'abort' && removedListener === listener
          && removed.mock.contexts[removalIndex] === added.mock.contexts[index])).toBe(true);
      }
      chrome.storage.local.get.mockResolvedValue({ bridgeConfig: cfg });
      expect((await relay(message, sender)).status).toBe(200);
    } finally {
      added.mockRestore();
      removed.mockRestore();
    }
  });

  it.each([
    ['/api/bridge/ingest', 'POST', { channel: 'douyin', account_id: 'account-a', messages: [] }],
    ['/api/bridge/outbox', 'GET', undefined],
    ['/api/bridge/outbox/ack', 'POST', { v: 2, items: [{ msg_id: 'message-a', conversation_id: 'conversation-a' }], status: 'delivered' }],
    ['/api/bridge/capabilities', 'GET', undefined],
  ])('合法 %s 保留请求内容和固定方法', async (path, method, payload) => {
    const url = new URL(`https://service.example/base${path}`);
    url.searchParams.set('tenant_code', cfg.tenantCode);
    if (path !== '/api/bridge/capabilities') {
      url.searchParams.set('channel', 'douyin');
      url.searchParams.set('account_id', 'account-a');
    }
    const body = payload ? JSON.stringify(payload) : undefined;
    expect((await relay({ ...message, url: url.toString(), options: { ...message.options, method, body } }, sender)).status).toBe(200);
    expect(fetcher.mock.calls[0][0]).toBe(url.toString());
    expect(fetcher.mock.calls[0][1]).toMatchObject({ method, body, credentials: 'omit', redirect: 'error' });
    expect(vi.getTimerCount()).toBe(0);
  });

  it.each([
    ['wss://service.example/base', 'https://service.example/base'],
    ['ws://127.0.0.1:8204/base', 'http://127.0.0.1:8204/base'],
    ['http://localhost:8204', 'http://localhost:8204'],
  ])('配置地址 %s 保留合法开发或协议兼容', async (serverUrl, base) => {
    cfg.serverUrl = serverUrl;
    const url = `${base}/api/bridge/outbox?channel=douyin&account_id=account-a&tenant_code=tenant-a`;
    expect((await relay({ ...message, url }, sender)).status).toBe(200);
    expect(fetcher.mock.calls[0][0]).toBe(url);
  });

  it('小红书别名只匹配本渠道，不能借用抖音范围', async () => {
    const xhs = { ...sender, url: 'https://www.xiaohongshu.com/chat/peer-a' };
    expect((await relay({ ...message, url: message.url.replace('channel=douyin', 'channel=xhs') }, xhs)).status).toBe(200);
    expect(await relay(message, xhs)).toHaveProperty('__err');
    expect(fetcher).toHaveBeenCalledTimes(1);
  });

  it.each([
    ['空配置', undefined],
    ['缺凭证', { serverUrl: 'https://service.example/base', token: '', tenantCode: 'tenant-a' }],
    ['非本机明文地址', { ...{ serverUrl: 'http://service.example/base', token: 'stored-secret', tenantCode: 'tenant-a' } }],
    ['服务地址带凭证', { serverUrl: 'https://user:password@service.example/base', token: 'stored-secret', tenantCode: 'tenant-a' }],
  ])('%s 不发送请求且回收预算', async (_label, storedConfig) => {
    cfg = storedConfig;
    expect(await relay(message, sender)).toHaveProperty('__err');
    expect(fetcher).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  it.each([
    ['请求体跨渠道', { channel: 'xiaohongshu', messages: [] }],
    ['请求体跨租户', { tenant_code: 'tenant-b', messages: [] }],
    ['请求体跨账号', { account_id: 'account-b', messages: [] }],
    ['非对象请求体', []],
  ])('%s 不透传网络', async (_label, payload) => {
    const request = { ...message, url: message.url.replace('/api/bridge/outbox', '/api/bridge/ingest'),
      options: { ...message.options, method: 'POST', body: JSON.stringify(payload) } };
    expect(await relay(request, sender)).toHaveProperty('__err');
    expect(fetcher).not.toHaveBeenCalled();
  });

  it('无ID旧请求也受同文档容量限制，取消后额度恢复', async () => {
    let release;
    chrome.storage.local.get.mockImplementation(() => new Promise(resolve => { release = resolve; }));
    const requests = [];
    for (let i = 0; i < 15; i++) requests.push(relay({ ...message, requestId: `capacity-${i}` }, sender));
    const { requestId: _requestId, ...legacy } = message;
    requests.push(relay(legacy, sender));
    expect(await relay({ ...message, requestId: 'overflow' }, sender)).toMatchObject({ __err: 'HTTP 中转容量已满' });
    expect(fetcher).not.toHaveBeenCalled();
    expect(cancel('capacity-0', sender)).toBe(true);
    expect((await requests[0]).errorName).toBe('AbortError');
    const recovered = relay({ ...message, requestId: 'recovered' }, sender);
    expect(cancel('recovered', sender)).toBe(true);
    expect((await recovered).errorName).toBe('AbortError');
    await vi.advanceTimersByTimeAsync(30000);
    await Promise.all(requests);
    release({ bridgeConfig: cfg });
    expect(fetcher).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  it.each([
    ['内层渠道', { channel: 'xiaohongshu', account_id: 'account-a', conversation_id: 'conversation-a' }],
    ['内层账号', { channel: 'douyin', account_id: 'account-b', conversation_id: 'conversation-a' }],
    ['限定会话', { channel: 'douyin', account_id: 'account-a', conversation_id: 'conversation-b' }],
  ])('%s 冲突时整个批次拒绝，不先转发合法消息', async (_label, invalid) => {
    const payload = { channel: 'douyin', account_id: 'account-a', conversation_id: 'conversation-a',
      messages: [{ content: '合成合法消息', conversation_id: 'conversation-a' }, { ...invalid, content: '合成冲突消息' }] };
    const request = { ...message,
      url: message.url.replace('/api/bridge/outbox', '/api/bridge/ingest') + '&conversation_id=conversation-a',
      options: { ...message.options, method: 'POST', body: JSON.stringify(payload) } };
    expect(await relay(request, sender)).toHaveProperty('__err');
    expect(fetcher).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
  });

  it('没有限定顶层会话时，同渠道账号的多会话消息仍可转发', async () => {
    const payload = { channel: 'douyin', account_id: 'account-a', messages: [
      { conversation_id: 'conversation-a', content: '合成第一会话' },
      { channel: 'douyin', account_id: 'account-a', conversation_id: 'conversation-b', content: '合成第二会话' },
    ] };
    const body = JSON.stringify(payload);
    const request = { ...message, url: message.url.replace('/api/bridge/outbox', '/api/bridge/ingest'),
      options: { ...message.options, method: 'POST', body } };
    expect((await relay(request, sender)).status).toBe(200);
    expect(fetcher.mock.calls[0][1].body).toBe(body);
  });

  it('ACK v2多会话items不被上行会话规则误拒绝', async () => {
    const body = JSON.stringify({ v: 2, status: 'delivered', items: [
      { msg_id: 'ack-a', conversation_id: 'conversation-a' },
      { msg_id: 'ack-b', conversation_id: 'conversation-b' },
    ] });
    expect((await relay({ ...message, url: message.url.replace('/api/bridge/outbox', '/api/bridge/outbox/ack'),
      options: { ...message.options, method: 'POST', body } }, sender)).status).toBe(200);
    expect(fetcher.mock.calls[0][1].body).toBe(body);
  });
});
