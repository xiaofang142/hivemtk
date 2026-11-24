// @vitest-environment node
// SW 不使用 DOM；避免 jsdom 初始化定时器混入中转生命周期断言。
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';

describe('background HTTP 中转生命周期', () => {
  let listeners;
  let fetchMock;
  let bootstrapTimers;
  const cfg = { serverUrl: 'https://example.invalid', token: 'stored-secret', tenantCode: 'tenant-a' };
  const sender = { id: 'test-extension', url: 'https://www.douyin.com/chat', tab: { id: 7 }, frameId: 0, documentId: 'document-1' };
  const message = { type: 'bridgeFetch', requestId: 'request-1', url: 'https://example.invalid/api/bridge/outbox?channel=douyin&account_id=test-account&tenant_code=tenant-a', options: { method: 'GET', headers: { 'X-Channel-Identify-Code': cfg.token } } };

  function dispatch(msg, origin = sender) {
    return new Promise(resolve => {
      const handled = listeners.some(fn => fn(msg, origin, resolve) === true);
      if (!handled) resolve(undefined);
    });
  }

  beforeEach(async () => {
    vi.resetModules();
    vi.useFakeTimers();
    listeners = [];
    fetchMock = vi.fn();
    vi.stubGlobal('fetch', fetchMock);
    vi.stubGlobal('chrome', {
      runtime: { id: sender.id, onMessage: { addListener: fn => listeners.push(fn) } },
      storage: { local: { get: async (_key, cb) => { const stored = { bridgeConfig: cfg }; cb?.(stored); return stored; }, set: (_obj, cb) => cb?.() } },
      tabs: { query: (_opts, cb) => cb([]) },
      alarms: { create: () => {}, onAlarm: { addListener: () => {} } },
      webNavigation: { onCommitted: { addListener: () => {} } },
    });
    await import('../src/background/index.js');
    // 动态加载本身已有 harness 计时器；只断言本请求没有新增残留，不能清掉基线装绿。
    bootstrapTimers = vi.getTimerCount();
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  function pendingFetch() {
    fetchMock.mockImplementation((_url, options) => new Promise((_resolve, reject) => {
      options?.signal?.addEventListener('abort', () => reject(new DOMException('aborted', 'AbortError')));
    }));
  }

  it('正常完成保留响应契约并清理计时器和取消状态', async () => {
    fetchMock.mockResolvedValue(new Response('{"status":"ok"}', { status: 200, headers: { 'content-type': 'application/json' } }));
    const result = await dispatch(message);
    expect(result).toMatchObject({ status: 200, headers: { 'content-type': 'application/json' }, body: '{"status":"ok"}' });
    expect(fetchMock.mock.calls[0][1].signal).toBeInstanceOf(AbortSignal);
    expect(vi.getTimerCount()).toBe(bootstrapTimers);
    expect(await dispatch({ type: 'bridgeFetchCancel', requestId: message.requestId })).toEqual({ ok: true, cancelled: false });
  });

  it('对应取消消息实际中止 SW fetch', async () => {
    pendingFetch();
    let result;
    const promise = dispatch(message).then(r => { result = r; });
    await vi.advanceTimersByTimeAsync(0);
    const cancellation = await dispatch({ type: 'bridgeFetchCancel', requestId: message.requestId });
    await vi.advanceTimersByTimeAsync(1);
    expect(cancellation).toEqual({ ok: true, cancelled: true });
    expect(fetchMock.mock.calls[0][1].signal.aborted).toBe(true);
    expect(result).toMatchObject({ errorName: 'AbortError' });
    expect(vi.getTimerCount()).toBe(bootstrapTimers);
    await promise;
  });

  it('其他 tab 的同 ID 取消不能中止本请求', async () => {
    pendingFetch();
    const promise = dispatch(message);
    await vi.advanceTimersByTimeAsync(0);
    expect(await dispatch({ type: 'bridgeFetchCancel', requestId: message.requestId }, { ...sender, tab: { id: 8 } }))
      .toEqual({ ok: true, cancelled: false });
    expect(fetchMock.mock.calls[0][1].signal.aborted).toBe(false);
    await dispatch({ type: 'bridgeFetchCancel', requestId: message.requestId });
    await promise;
  });

  it('同 tab 导航后的其他文档不能取消旧文档请求', async () => {
    pendingFetch();
    const promise = dispatch(message);
    await vi.advanceTimersByTimeAsync(0);
    expect(await dispatch({ type: 'bridgeFetchCancel', requestId: message.requestId }, { ...sender, documentId: 'document-2' }))
      .toEqual({ ok: true, cancelled: false });
    expect(fetchMock.mock.calls[0][1].signal.aborted).toBe(false);
    await dispatch({ type: 'bridgeFetchCancel', requestId: message.requestId });
    await promise;
  });

  it('重复请求 ID 不覆盖仍在执行的请求和取消控制器', async () => {
    pendingFetch();
    const promise = dispatch(message);
    expect(await dispatch(message)).toMatchObject({ __err: '重复的 HTTP 中转请求 ID' });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(await dispatch({ type: 'bridgeFetchCancel', requestId: message.requestId })).toEqual({ ok: true, cancelled: true });
    await promise;
    expect(vi.getTimerCount()).toBe(bootstrapTimers);
  });

  it('SW 30 秒安全时限实际取消 fetch', async () => {
    pendingFetch();
    let result;
    const promise = dispatch(message).then(r => { result = r; });
    await vi.advanceTimersByTimeAsync(30001);
    expect(result).toMatchObject({ errorName: 'AbortError' });
    expect(fetchMock.mock.calls[0][1].signal.aborted).toBe(true);
    expect(vi.getTimerCount()).toBe(bootstrapTimers);
    await promise;
  });

  it('旧无 requestId 请求仍可返回，错误也清计时器', async () => {
    fetchMock.mockRejectedValue(new Error('network failure'));
    const { requestId: _requestId, ...legacy } = message;
    expect(await dispatch(legacy)).toMatchObject({ __err: 'HTTP 中转失败或范围未确认' });
    expect(vi.getTimerCount()).toBe(bootstrapTimers);
  });

  it('页面 outbox 预算贯穿真实分发器，取消后台 fetch 且不再重试', async () => {
    pendingFetch();
    vi.stubGlobal('location', { protocol: 'https:' });
    chrome.runtime.id = 'test-extension';
    chrome.runtime.sendMessage = vi.fn(msg => dispatch(msg));
    const { getOutbox } = await import('../src/core/http-ingest.js');
    let result;
    const promise = getOutbox({ serverUrl: cfg.serverUrl, token: cfg.token, tenantCode: cfg.tenantCode, channel: 'douyin', accountId: 'test-account' }, { timeoutMs: 50 })
      .then(r => { result = r; });
    await vi.advanceTimersByTimeAsync(51);
    expect(result).toMatchObject({ status: 'error', messages: [] });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(fetchMock.mock.calls[0][1].signal.aborted).toBe(true);
    expect(chrome.runtime.sendMessage.mock.calls.map(([msg]) => msg.type))
      .toEqual(['bridgeFetch', 'bridgeFetchCancel']);
    await vi.advanceTimersByTimeAsync(10000);
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(vi.getTimerCount()).toBe(bootstrapTimers);
    await promise;
  });

  it('页面成功响应贯穿真实分发器，保留正文和 headers 且无取消残留', async () => {
    fetchMock.mockResolvedValue(new Response('{"status":"ok","messages":[]}', { status: 200, headers: { 'content-type': 'application/json' } }));
    vi.stubGlobal('location', { protocol: 'https:' });
    chrome.runtime.id = 'test-extension';
    chrome.runtime.sendMessage = vi.fn(msg => dispatch(msg));
    const { bridgeFetch } = await import('../src/core/http-ingest.js');
    const result = await bridgeFetch(message.url, message.options);
    expect(result.headers.get('content-type')).toBe('application/json');
    expect(await result.json()).toEqual({ status: 'ok', messages: [] });
    expect(fetchMock).toHaveBeenCalledTimes(1);
    expect(chrome.runtime.sendMessage).toHaveBeenCalledTimes(1);
    expect(vi.getTimerCount()).toBe(bootstrapTimers);
  });
});
