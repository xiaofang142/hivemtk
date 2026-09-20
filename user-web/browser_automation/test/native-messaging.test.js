import { describe, it, expect, vi, beforeEach } from 'vitest';
import { createNativePort } from '../src/core/native-messaging.js';

function fakeChromeRuntime() {
  const ports = [];
  const makePort = () => {
    const listeners = { message: [], disconnect: [] };
    const port = {
      onMessage: { addListener: (fn) => listeners.message.push(fn) },
      onDisconnect: { addListener: (fn) => listeners.disconnect.push(fn) },
      postMessage: vi.fn(),
      disconnect: vi.fn(),
    };
    return { port, listeners };
  };
  let latest = null;
  const makeFreshPort = () => {
    const entry = makePort();
    ports.push(entry);
    latest = entry;
    return entry.port;
  };
  return {
    // 真实 Chrome：每次 connectNative 返回**独立** port，监听器不跨 port 累积。
    // 早前的夹具复用同一个 port 对象，一次 disconnect 会把历史 N 个监听器全叫醒，
    // 于是「重连上限」在测试里靠这个假象才够得到——夹具本身的假绿（批14 修）。
    makeFreshPort,
    chromeAPI: {
      runtime: {
        connectNative: vi.fn(() => makeFreshPort()),
        lastError: null,
      },
    },
    // 只对最近一个 port 发消息/断连（等价于 Chrome 对当前 port 的事件）
    emitMessage: (m) => latest?.listeners.message.forEach((fn) => fn(m)),
    emitDisconnect: () => latest?.listeners.disconnect.forEach((fn) => fn()),
    ports,
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
    // online 现在只在「端口存活满证活窗口」后才报（批14：连上就报 online 是假绿，
    // Host 秒退的场景当时会反复闪 online，popup 显示一切正常而服务端一个命令都下发不了）
    await vi.advanceTimersByTimeAsync(2000);
    expect(statuses).toContain('online');
  });

  it('port 建立后立刻断开：不得报 online（秒退风暴里 online 是假的）', async () => {
    const ctx = fakeChromeRuntime();
    const statuses = [];
    ctx.chromeAPI.runtime.connectNative.mockImplementation(() => {
      const p = ctx.makeFreshPort();
      setTimeout(() => ctx.emitDisconnect(), 10);
      return p;
    });
    createNativePort(ctx.chromeAPI, () => {}, (s) => statuses.push(s));
    await vi.advanceTimersByTimeAsync(60000);
    expect(statuses).not.toContain('online');
  });

  // 批14：connect() 入口无条件 reconnectAttempts=0，把退避序列打回原形——
  // 上限永远够不到（giveup 是死代码），Host 缺失时变成 2s 一次的永久进程风暴。
  it('连续失败退避递增（2s→4s），说明计数没在 connect 时被清零', async () => {
    const ctx = fakeChromeRuntime();
    createNativePort(ctx.chromeAPI, () => {}, () => {});
    expect(ctx.chromeAPI.runtime.connectNative).toHaveBeenCalledTimes(1);
    ctx.emitDisconnect();
    await vi.advanceTimersByTimeAsync(1999);
    expect(ctx.chromeAPI.runtime.connectNative).toHaveBeenCalledTimes(1);
    await vi.advanceTimersByTimeAsync(1);
    expect(ctx.chromeAPI.runtime.connectNative).toHaveBeenCalledTimes(2);
    ctx.emitDisconnect(); // 第二次也没活过证活窗口
    await vi.advanceTimersByTimeAsync(3999);
    expect(ctx.chromeAPI.runtime.connectNative).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(1);
    expect(ctx.chromeAPI.runtime.connectNative).toHaveBeenCalledTimes(3);
  });

  it('到上限报 giveup，但之后仍按长间隔重试（SW 没有 alarms，停下就永远不回来）', async () => {
    const ctx = fakeChromeRuntime();
    const statuses = [];
    // 每次 connect 都在证活窗口之前秒退 → 退避真的能累计到上限
    ctx.chromeAPI.runtime.connectNative.mockImplementation(() => {
      const p = ctx.makeFreshPort();
      setTimeout(() => ctx.emitDisconnect(), 10);
      return p;
    });
    createNativePort(ctx.chromeAPI, () => {}, (s) => statuses.push(s));
    // 退避序列 2+4+8+16+30s（第 5 档被 30s 封顶）→ 第 6 次失败才够到上限
    await vi.advanceTimersByTimeAsync(70000);
    expect(statuses.filter((s) => s === 'giveup').length).toBe(1); // 只播报一次，不刷屏
    const callsAtGiveup = ctx.chromeAPI.runtime.connectNative.mock.calls.length;
    await vi.advanceTimersByTimeAsync(700000); // giveup 不是终点：长间隔继续重试
    expect(ctx.chromeAPI.runtime.connectNative.mock.calls.length).toBeGreaterThanOrEqual(callsAtGiveup + 2);
  });

  it('连接证活后计数归零：恢复满窗口再断，退避回到最短档', async () => {
    const ctx = fakeChromeRuntime();
    const statuses = [];
    createNativePort(ctx.chromeAPI, () => {}, (s) => statuses.push(s));
    ctx.emitDisconnect();
    await vi.advanceTimersByTimeAsync(2000); // 第 2 次 connect
    await vi.advanceTimersByTimeAsync(2000); // 活满证活窗口 → online
    expect(statuses).toContain('online');
    ctx.emitDisconnect();
    await vi.advanceTimersByTimeAsync(1999);
    expect(ctx.chromeAPI.runtime.connectNative).toHaveBeenCalledTimes(2);
    await vi.advanceTimersByTimeAsync(1);
    expect(ctx.chromeAPI.runtime.connectNative).toHaveBeenCalledTimes(3);
  });
});
