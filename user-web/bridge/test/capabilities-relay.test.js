import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { getServerCapabilities } from '../src/core/polling-loop.js';

const cfg = { serverUrl: 'http://localhost:8204', token: ' bridge-test ', tenantCode: 'tenant-test' };
const payload = {
  sse_enabled: true,
  poll_interval_ms: 1200,
  patrol_unread_first_channels: ['douyin', 'xianyu'],
  follow_user_enabled: false,
  follow_user_channels: ['douyin'],
};

function relayResponse(status = 200) {
  return { status, statusText: '', headers: {}, body: JSON.stringify(payload) };
}

describe('页面能力查询 / 后台中转与资源回收', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    vi.stubGlobal('chrome', { runtime: { id: 'bridge-test-id', sendMessage: vi.fn(async () => relayResponse()) } });
    // jsdom 的普通 http 页面模拟 content script：直连遭页面源限制。
    vi.stubGlobal('fetch', vi.fn(async () => { throw new TypeError('Failed to fetch'); }));
  });

  afterEach(() => {
    vi.clearAllTimers();
    vi.useRealTimers();
    vi.unstubAllGlobals();
  });

  it('页面直连不可用仍通过后台获得服务端策略，租户和凭证不丢失', async () => {
    const caps = await getServerCapabilities(cfg);
    expect(caps.patrol_unread_first_channels).toEqual(['douyin', 'xianyu']);
    expect(caps.sse_enabled).toBe(true);
    expect(caps.follow_user_enabled).toBe(false);
    expect(fetch).not.toHaveBeenCalled();
    const request = chrome.runtime.sendMessage.mock.calls[0][0];
    expect(request.type).toBe('bridgeFetch');
    expect(new URL(request.url).searchParams.get('tenant_code')).toBe('tenant-test');
    expect(request.options.headers['X-Channel-Identify-Code']).toBe('bridge-test');
    expect(request.options.headers.Authorization).toBe('Bearer bridge-test');
    expect(request.url).not.toContain('bridge-test');
    expect(vi.getTimerCount()).toBe(0);
  });

  it('网络失败后保守降级且立即回收 timer，不留下五秒悬挂', async () => {
    chrome.runtime.sendMessage.mockRejectedValue(new Error('relay unavailable'));
    const caps = await getServerCapabilities(cfg);
    expect(caps.sse_enabled).toBe(false);
    expect(caps.follow_user_enabled).toBe(false);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('401 后保守降级且不重试、不泄漏 timer', async () => {
    chrome.runtime.sendMessage.mockResolvedValue(relayResponse(401));
    const caps = await getServerCapabilities(cfg);
    expect(caps.patrol_unread_first_channels).toEqual([]);
    expect(chrome.runtime.sendMessage).toHaveBeenCalledTimes(1);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('五秒预算到期取消后台请求，释放查询而非继续等待中转三十秒', async () => {
    chrome.runtime.sendMessage.mockImplementation((request) => request.type === 'bridgeFetchCancel'
      ? Promise.resolve({ ok: true }) : new Promise(() => {}));
    const query = getServerCapabilities(cfg);
    await vi.advanceTimersByTimeAsync(5000);
    const caps = await query;
    expect(caps.sse_enabled).toBe(false);
    expect(chrome.runtime.sendMessage.mock.calls.some(([request]) => request.type === 'bridgeFetchCancel')).toBe(true);
    expect(vi.getTimerCount()).toBe(0);
  });

  it('无扩展运行时仍保留直连语义与能力解析', async () => {
    vi.stubGlobal('chrome', undefined);
    fetch.mockResolvedValue(new Response(JSON.stringify(payload), { status: 200 }));
    const caps = await getServerCapabilities(cfg);
    expect(caps.poll_interval_ms).toBe(1200);
    expect(caps.patrol_unread_first_channels).toEqual(['douyin', 'xianyu']);
    expect(vi.getTimerCount()).toBe(0);
  });
});
