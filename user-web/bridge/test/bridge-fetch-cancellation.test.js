import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ackOutbox, bridgeFetch, fetchWithRetry, getOutbox, postIngest } from '../src/core/http-ingest.js';

const URL = 'https://example.invalid/api/bridge/outbox';
const CRED = { serverUrl: 'https://example.invalid', channel: 'douyin', accountId: 'test-account' };
const OK = { status: 200, headers: {}, body: '{"status":"ok","messages":[]}' };

describe('页面 HTTP 中转取消与请求预算', () => {
  let sendMessage;
  let directFetch;

  beforeEach(() => {
    vi.useFakeTimers();
    sendMessage = vi.fn(() => new Promise(() => {}));
    directFetch = vi.fn();
    vi.stubGlobal('location', { protocol: 'https:' });
    vi.stubGlobal('chrome', { runtime: { id: 'test-extension', sendMessage } });
    vi.stubGlobal('fetch', directFetch);
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('正常响应后清理中转时限计时器', async () => {
    sendMessage.mockResolvedValue(OK);
    expect((await bridgeFetch(URL, {})).status).toBe(200);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('中转失败后清理时限计时器且不直连', async () => {
    sendMessage.mockRejectedValue(new Error('SW unavailable'));
    await expect(bridgeFetch(URL, {})).rejects.toThrow('SW unavailable');
    expect(vi.getTimerCount()).toBe(0);
    expect(directFetch).not.toHaveBeenCalled();
  });

  it('SW 同步抛错后也清理时限，不回退页面源直连', async () => {
    sendMessage.mockImplementation(() => { throw new Error('Extension context invalidated'); });
    await expect(bridgeFetch(URL, {})).rejects.toThrow('Extension context invalidated');
    expect(vi.getTimerCount()).toBe(0);
    expect(directFetch).not.toHaveBeenCalled();
  });

  it('没有调用方信号时，中转安全时限也停止重试并通知后台取消', async () => {
    let error;
    const promise = fetchWithRetry(URL, {}).catch(e => { error = e; });
    await vi.advanceTimersByTimeAsync(30001);
    expect(error?.name).toBe('AbortError');
    expect(sendMessage.mock.calls.map(([msg]) => msg.type)).toEqual(['bridgeFetch', 'bridgeFetchCancel']);
    await vi.advanceTimersByTimeAsync(10000);
    expect(sendMessage).toHaveBeenCalledTimes(2);
    expect(vi.getTimerCount()).toBe(0);
    expect(directFetch).not.toHaveBeenCalled();
    await promise;
  });

  it('已经取消的信号不发送中转请求', async () => {
    const controller = new AbortController();
    controller.abort();
    let error;
    const promise = bridgeFetch(URL, { signal: controller.signal }).catch(e => { error = e; });
    await vi.advanceTimersByTimeAsync(30001);
    await promise;
    expect(error?.name).toBe('AbortError');
    expect(sendMessage).not.toHaveBeenCalled();
  });

  it('执行中取消立即结算并通知 SW 对应请求', async () => {
    const controller = new AbortController();
    let error;
    const promise = bridgeFetch(URL, { signal: controller.signal }).catch(e => { error = e; });
    controller.abort();
    await vi.advanceTimersByTimeAsync(1);
    expect(error?.name).toBe('AbortError');
    const request = sendMessage.mock.calls.find(([m]) => m.type === 'bridgeFetch')?.[0];
    expect(request.requestId).toEqual(expect.any(String));
    expect(sendMessage).toHaveBeenCalledWith({ type: 'bridgeFetchCancel', requestId: request.requestId });
    expect(vi.getTimerCount()).toBe(0);
    expect(directFetch).not.toHaveBeenCalled();
    await promise;
  });

  it.each([
    ['outbox', () => getOutbox(CRED, { timeoutMs: 50 })],
    ['ack', () => ackOutbox(CRED, ['m1'], { conversationId: 'c1', timeoutMs: 50 })],
    ['ingest', () => postIngest(CRED, { messages: [] }, { timeoutMs: 50 })],
  ])('%s 请求预算到期后结算，不再重试', async (_label, request) => {
    let settled = false;
    const promise = request().then(() => { settled = true; }, () => { settled = true; });
    await vi.advanceTimersByTimeAsync(51);
    expect(settled).toBe(true);
    await vi.advanceTimersByTimeAsync(10000);
    expect(sendMessage.mock.calls.filter(([m]) => m.type === 'bridgeFetch')).toHaveLength(1);
    expect(directFetch).not.toHaveBeenCalled();
    expect(vi.getTimerCount()).toBe(0);
    await promise;
  });

  it('退避期间取消立即停止，不发送下一次请求', async () => {
    sendMessage.mockRejectedValue(new Error('network failure'));
    const controller = new AbortController();
    let error;
    const promise = fetchWithRetry(URL, { signal: controller.signal }, { retryBaseMs: 1000 })
      .catch(e => { error = e; });
    await vi.advanceTimersByTimeAsync(1);
    controller.abort();
    await vi.advanceTimersByTimeAsync(1);
    expect(error?.name).toBe('AbortError');
    await vi.advanceTimersByTimeAsync(10000);
    expect(sendMessage.mock.calls.filter(([m]) => m.type === 'bridgeFetch')).toHaveLength(1);
    expect(vi.getTimerCount()).toBe(0);
    await promise;
  });

  it('未取消的可重试错误仍按既有策略恢复', async () => {
    sendMessage.mockRejectedValueOnce(new Error('network failure')).mockResolvedValue(OK);
    const promise = fetchWithRetry(URL, {}, { maxRetries: 1, retryBaseMs: 10 });
    await vi.advanceTimersByTimeAsync(210);
    expect((await promise).status).toBe(200);
    expect(sendMessage).toHaveBeenCalledTimes(2);
    expect(vi.getTimerCount()).toBe(0);
  });
});
