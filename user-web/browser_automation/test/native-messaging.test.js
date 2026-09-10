import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createNativePort } from '../src/core/native-messaging.js';

function fakeChromeRuntime() {
  const listeners = { message: [], disconnect: [] };
  const port = {
    onMessage: { addListener: (fn) => listeners.message.push(fn) },
    onDisconnect: { addListener: (fn) => listeners.disconnect.push(fn) },
    postMessage: vi.fn(),
    disconnect: vi.fn(),
  };
  return {
    port,
    chromeAPI: {
      runtime: {
        connectNative: vi.fn(() => port),
        lastError: null,
      },
    },
    emitMessage: (m) => listeners.message.forEach((fn) => fn(m)),
    emitDisconnect: () => listeners.disconnect.forEach((fn) => fn()),
  };
}

describe('native-messaging port', () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  it('connect 后命令帧回调 onCommand', () => {
    const ctx = fakeChromeRuntime();
    const onCommand = vi.fn();
    createNativePort(ctx.chromeAPI, onCommand, () => {});
    ctx.emitMessage({ req_id: 'r1', action: 'click' });
    expect(onCommand).toHaveBeenCalledWith({ req_id: 'r1', action: 'click' });
  });

  it('非命令帧（回包）不触发 onCommand', () => {
    const ctx = fakeChromeRuntime();
    const onCommand = vi.fn();
    createNativePort(ctx.chromeAPI, onCommand, () => {});
    ctx.emitMessage({ req_id: 'r1', ok: true, data: {} });
    expect(onCommand).not.toHaveBeenCalled();
  });

  it('断开后自动重连并恢复 online', async () => {
    const ctx = fakeChromeRuntime();
    const statuses = [];
    createNativePort(ctx.chromeAPI, () => {}, (s) => statuses.push(s));
    ctx.emitDisconnect();
    expect(statuses).toContain('offline');
    await vi.advanceTimersByTimeAsync(2000);
    // 第二次 connectNative 调用
    expect(ctx.chromeAPI.runtime.connectNative).toHaveBeenCalledTimes(2);
    expect(statuses).toContain('online');
  });

  it('重连上限 5 次后 giveup', async () => {
    const ctx = fakeChromeRuntime();
    const statuses = [];
    // 每次 connect 后立即断开
    ctx.chromeAPI.runtime.connectNative.mockImplementation(() => {
      setTimeout(() => ctx.emitDisconnect(), 0);
      return ctx.port;
    });
    createNativePort(ctx.chromeAPI, () => {}, (s) => statuses.push(s));
    // 退避序列 2s+4s+8s+16s+30s(封顶) —— 推进足够时间让 5 次重连全部用尽
    await vi.advanceTimersByTimeAsync(65000);
    await vi.advanceTimersByTimeAsync(65000);
    expect(statuses).toContain('giveup');
  });
});
