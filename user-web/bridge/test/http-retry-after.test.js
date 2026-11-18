import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { bridgeFetch, fetchWithRetry } from '../src/core/http-ingest.js';

describe('HTTP Retry-After 不早于服务端限制且可取消', () => {
  let send;
  beforeEach(() => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date('2026-11-16T12:00:00Z'));
    send = vi.fn();
    vi.stubGlobal('location', { protocol: 'https:' });
    vi.stubGlobal('chrome', { runtime: { id: 'test-extension', sendMessage: send } });
  });
  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });
  const url = 'https://service.example/api/bridge/outbox';
  const ok = { status: 200, headers: {}, body: '{}' };

  it('中转响应头与原生 Headers 一样不区分大小写', async () => {
    send.mockResolvedValue({ ...ok, headers: { 'retry-after': '4' } });
    expect((await bridgeFetch(url, {})).headers.get('Retry-After')).toBe('4');
  });

  it.each([
    ['小写秒数', '4'],
    ['HTTP 日期', 'Mon, 16 Nov 2026 12:00:04 GMT'],
  ])('%s 在限流窗口内不发第二次请求', async (_label, retryAfter) => {
    send.mockResolvedValueOnce({ status: 429, headers: { 'retry-after': retryAfter }, body: '{}' }).mockResolvedValue(ok);
    const promise = fetchWithRetry(url, {}, { maxRetries: 1, retryBaseMs: 1 });
    await vi.advanceTimersByTimeAsync(3999);
    expect(send).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    expect((await promise).status).toBe(200);
    expect(send).toHaveBeenCalledTimes(2);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('超过单个 timer 上限的秒数不发生溢出提前重试，取消仍立即完成', async () => {
    send.mockResolvedValue({ status: 429, headers: { 'retry-after': '3000000' }, body: '{}' });
    const controller = new AbortController();
    const promise = fetchWithRetry(url, { signal: controller.signal }, { maxRetries: 1, retryBaseMs: 1 }).catch(error => error);
    await vi.advanceTimersByTimeAsync(1000);
    expect(send).toHaveBeenCalledTimes(1);
    controller.abort();
    expect((await promise).name).toBe('AbortError');
    expect(vi.getTimerCount()).toBe(0);
  });

  it('旧 entries 格式响应头保持大小写不敏感', async () => {
    send.mockResolvedValue({ ...ok, headers: [['ReTrY-AfTeR', '4']] });
    expect((await bridgeFetch(url, {})).headers.get('retry-after')).toBe('4');
  });

  it.each(['4seconds', '-4', 'not-a-date', 'Mon, 16 Nov 2026 11:59:59 GMT'])('非法或过期值 %s 回到原指数退避', async retryAfter => {
    vi.spyOn(Math, 'random').mockReturnValue(0);
    send.mockResolvedValueOnce({ status: 429, headers: { 'retry-after': retryAfter }, body: '{}' }).mockResolvedValue(ok);
    const promise = fetchWithRetry(url, {}, { maxRetries: 1, retryBaseMs: 1000 });
    await vi.advanceTimersByTimeAsync(999);
    expect(send).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    expect((await promise).status).toBe(200);
    Math.random.mockRestore();
  });
});
