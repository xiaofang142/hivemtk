import { afterEach, describe, expect, it, vi } from 'vitest';
import { createOutboxWakeRuntime, readOutboxWakeStream } from '../src/background/outbox-wake.js';

const cleanups = [];
afterEach(() => { cleanups.splice(0).forEach((cleanup) => cleanup()); vi.restoreAllMocks(); });

function fixture() {
  let controller;
  const cfg = { serverUrl: 'https://service.example', token: 'wake-secret', tenantCode: 'tenant-a' };
  const api = {
    runtime: { id: 'extension-id' },
    storage: { local: { get: vi.fn(async () => ({ bridgeConfig: cfg })) } },
    tabs: { sendMessage: vi.fn(async () => {}) },
  };
  const fetcher = vi.fn(async () => ({
    ok: true,
    headers: new Headers({ 'content-type': 'text/event-stream' }),
    body: new ReadableStream({ start(c) { controller = c; } }),
  }));
  const runtime = createOutboxWakeRuntime(api, fetcher);
  cleanups.push(() => runtime.dispose());
  const sender = { id: 'extension-id', frameId: 0, url: 'https://www.douyin.com/chat', tab: { id: 7 } };
  const registration = { channel: 'douyin', accountId: 'account-a', registrationId: 'registration-a' };
  return { api, cfg, fetcher, runtime, sender, registration, stream: () => controller };
}

describe('后台非认领唤醒', () => {
  it('同账号共享连接，只发固定通知；认证头不进入URL或页面消息', async () => {
    const f = fixture();
    expect(await f.runtime.register(f.registration, f.sender)).toBe(true);
    expect(await f.runtime.register({ ...f.registration, registrationId: 'registration-b' }, { ...f.sender, tab: { id: 8 } })).toBe(true);
    expect(f.fetcher).toHaveBeenCalledTimes(1);
    const [url, options] = f.fetcher.mock.calls[0];
    expect(url).toContain('/api/channelbus/outbox/wake');
    expect(url).toContain('tenant_code=tenant-a');
    expect(url).not.toContain('wake-secret');
    expect(options.headers['X-Channel-Identify-Code']).toBe('wake-secret');
    f.stream().enqueue(new TextEncoder().encode('event: outbox_ready\ndata: {}\n\n'));
    await vi.waitFor(() => expect(f.api.tabs.sendMessage).toHaveBeenCalledTimes(2));
    expect(f.api.tabs.sendMessage.mock.calls[0][1]).toEqual({ type: 'BRIDGE_OUTBOX_READY', channel: 'douyin', accountId: 'account-a', registrationId: 'registration-a' });
    f.runtime.unregister(f.registration, f.sender);
    expect(options.signal.aborted).toBe(false);
    f.runtime.unregister({ ...f.registration, registrationId: 'registration-b' }, { ...f.sender, tab: { id: 8 } });
    expect(options.signal.aborted).toBe(true);
  });

  it('仅同扩展、顶层且渠道匹配的内容页可以登记', async () => {
    const f = fixture();
    expect(await f.runtime.register(f.registration, { ...f.sender, id: 'foreign-extension' })).toBe(false);
    expect(await f.runtime.register(f.registration, { ...f.sender, frameId: 1 })).toBe(false);
    expect(await f.runtime.register(f.registration, { ...f.sender, url: 'https://www.xiaohongshu.com/chat/a' })).toBe(false);
    expect(await f.runtime.register({ ...f.registration, accountId: 'default' }, f.sender)).toBe(false);
    expect(f.fetcher).not.toHaveBeenCalled();
  });

  it('分块UTF-8只接受固定空载荷；大帧拒绝', async () => {
    const valid = new TextEncoder().encode(': 中文心跳\nevent: outbox_ready\ndata: {}\n\nevent: new_outbound\ndata: {"content":"不应传递"}\n\n');
    const signal = new AbortController();
    const ready = vi.fn();
    const stream = new ReadableStream({ start(c) { c.enqueue(valid.slice(0, 4)); c.enqueue(valid.slice(4)); c.close(); } });
    await readOutboxWakeStream(stream, signal.signal, ready);
    expect(ready).toHaveBeenCalledTimes(1);
    const large = new ReadableStream({ start(c) { c.enqueue(new TextEncoder().encode('data: ' + 'x'.repeat(9000))); c.close(); } });
    await expect(readOutboxWakeStream(large, signal.signal, ready)).rejects.toThrow();
  });

  it('取消尚在读配置的登记不能晚到复活连接', async () => {
    const f = fixture();
    let release;
    f.api.storage.local.get.mockImplementationOnce(() => new Promise((resolve) => { release = resolve; }));
    const pending = f.runtime.register(f.registration, f.sender);
    f.runtime.unregister(f.registration, f.sender);
    release({ bridgeConfig: f.cfg });
    expect(await pending).toBe(false);
    expect(f.fetcher).not.toHaveBeenCalled();
  });

  it('配置变化中断旧流，重登记只使用新配置；关闭后不重连', async () => {
    vi.useFakeTimers();
    const f = fixture();
    try {
      expect(await f.runtime.register(f.registration, f.sender)).toBe(true);
      const oldSignal = f.fetcher.mock.calls[0][1].signal;
      f.runtime.invalidate();
      expect(oldSignal.aborted).toBe(true);
      f.cfg.token = 'new-wake-secret';
      expect(await f.runtime.register({ ...f.registration, registrationId: 'new-registration' }, f.sender)).toBe(true);
      expect(f.fetcher.mock.calls[1][1].headers['X-Channel-Identify-Code']).toBe('new-wake-secret');
      f.runtime.dispose();
      await vi.advanceTimersByTimeAsync(60000);
      expect(f.fetcher).toHaveBeenCalledTimes(2);
    } finally { vi.clearAllTimers(); vi.useRealTimers(); }
  });

  it('晚到的旧登记不能覆盖新账号；页签离开撤销在途登记', async () => {
    const f = fixture();
    let release;
    f.api.storage.local.get.mockImplementationOnce(() => new Promise((resolve) => { release = resolve; }));
    const old = f.runtime.register(f.registration, f.sender);
    expect(await f.runtime.register({ ...f.registration, accountId: 'account-b', registrationId: 'new-registration' }, f.sender)).toBe(true);
    release({ bridgeConfig: f.cfg });
    expect(await old).toBe(false);
    expect(f.fetcher).toHaveBeenCalledTimes(1);
    expect(f.fetcher.mock.calls[0][0]).toContain('account_id=account-b');
    f.api.storage.local.get.mockImplementationOnce(() => new Promise((resolve) => { release = resolve; }));
    const leaving = f.runtime.register(f.registration, f.sender);
    f.runtime.removeTab(f.sender.tab.id);
    release({ bridgeConfig: f.cfg });
    expect(await leaving).toBe(false);
    expect(f.fetcher.mock.calls[0][1].signal.aborted).toBe(true);
  });

  it.each([{ status: 401, seconds: 60 }, { status: 429, seconds: 120 }])('HTTP $status 不快速重连并尊重限流等待', async ({ status, seconds }) => {
    vi.useFakeTimers();
    const f = fixture();
    try {
      f.fetcher.mockResolvedValue({ ok: false, status, headers: new Headers({ 'retry-after': '120' }) });
      await f.runtime.register(f.registration, f.sender);
      await vi.advanceTimersByTimeAsync((seconds - 1) * 1000);
      expect(f.fetcher).toHaveBeenCalledTimes(1);
      await vi.advanceTimersByTimeAsync(2000);
      expect(f.fetcher).toHaveBeenCalledTimes(2);
    } finally { f.runtime.dispose(); vi.clearAllTimers(); vi.useRealTimers(); }
  });

  it('立即断开的200流不能每次重置退避', async () => {
    vi.useFakeTimers();
    vi.spyOn(Math, 'random').mockReturnValue(0);
    const f = fixture();
    try {
      f.fetcher.mockImplementation(async () => ({
        ok: true, headers: new Headers({ 'content-type': 'text/event-stream' }),
        body: new ReadableStream({ start(c) { c.close(); } }),
      }));
      await f.runtime.register(f.registration, f.sender);
      await vi.advanceTimersByTimeAsync(3000);
      expect(f.fetcher).toHaveBeenCalledTimes(3);
      await vi.advanceTimersByTimeAsync(1000);
      expect(f.fetcher).toHaveBeenCalledTimes(3);
    } finally { f.runtime.dispose(); vi.clearAllTimers(); vi.useRealTimers(); }
  });
});
