// test/cdp-input.test.js — cdp/input.js 事件序列单测（G8 优先级④：F1/F3 护栏）
// mock chrome.debugger，断言：ASCII 走 dispatchKeyEvent、CJK 走 insertText、
// 鼠标序列 mouseMoved(≥10 步)→press{buttons:1,clickCount:1}→release{buttons:0,clickCount:1}、
// 轨迹起点记忆、pressEnter text='\r'。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

let sent = [];

function makeDebugger(attachImpl) {
  return {
    attach: attachImpl || vi.fn(async () => {}),
    detach: vi.fn(async () => {}),
    sendCommand: vi.fn(async (target, method, params) => {
      sent.push({ method, params });
      return {};
    }),
    onDetach: { addListener: vi.fn() },
  };
}

// 自定义 sendCommand 行为（其余保持桩实现）；tally 记录每次实际下发的方法+事件类型
function makeDebuggerWith(sendImpl, attachImpl) {
  const d = makeDebugger(attachImpl);
  d.sendCommand = vi.fn(async (target, method, params) => {
    sent.push({ method, params });
    return sendImpl(method, params);
  });
  return d;
}

async function loadWithDebugger(dbg) {
  global.chrome = { debugger: dbg };
  vi.resetModules();
  return await import('../src/core/cdp/input.js');
}

describe('cdp/input', () => {
  beforeEach(() => {
    sent = [];
    vi.useFakeTimers({ shouldAdvanceTime: true });
  });
  afterEach(() => {
    vi.useRealTimers();
    delete global.chrome;
  });

  async function loadFresh() {
    global.chrome = { debugger: makeDebugger() };
    vi.resetModules();
    return await import('../src/core/cdp/input.js');
  }

  it('ASCII 键走 keyDown/keyUp（带 code/windowsVirtualKeyCode），Enter 走 insertText', async () => {
    const m = await loadFresh();
    await m.typeText(1, 'a你');
    const kd = sent.find((c) => c.params.type === 'keyDown');
    expect(kd.params.key).toBe('a');
    expect(kd.params.code).toBe('KeyA');
    expect(kd.params.windowsVirtualKeyCode).toBe(65);
    expect(kd.params.text).toBe('a');
    const ins = sent.find((c) => c.method === 'Input.insertText');
    expect(ins.params.text).toBe('你'); // CJK 唯一 trusted 通道
  });

  it('clickAt 完整序列：mouseMoved≥10 步 → press(buttons=1,clickCount=1) → release(buttons=0,clickCount=1)', async () => {
    const m = await loadFresh();
    await m.clickAt(2, 300, 400);
    const moves = sent.filter((c) => c.params.type === 'mouseMoved');
    const down = sent.find((c) => c.params.type === 'mousePressed');
    const up = sent.find((c) => c.params.type === 'mouseReleased');
    expect(moves.length).toBeGreaterThanOrEqual(10);
    expect(moves.every((mv) => mv.params.buttons === 0)).toBe(true);
    expect(down.params.buttons).toBe(1);
    expect(down.params.clickCount).toBe(1);
    expect(up.params.buttons).toBe(0);
    expect(up.params.clickCount).toBe(1);
    // 落点抖动允许 ±8px 内偏差
    expect(Math.abs(down.params.x - 300)).toBeLessThanOrEqual(8);
    expect(Math.abs(up.params.x - down.params.x)).toBe(0); // press/release 同点
  });

  it('轨迹起点记忆：第二次点击起点=上一次终点（F3：起点不恒定）', async () => {
    const m = await loadFresh();
    await m.clickAt(3, 500, 500);
    const moves1First = sent.filter((c) => c.params.type === 'mouseMoved')[0].params;
    sent = [];
    await m.clickAt(3, 700, 700);
    const moves2First = sent.filter((c) => c.params.type === 'mouseMoved')[0].params;
    // 第二次轨迹首点应接近 (500±jitter, 500±jitter)（上次落点），而非 (700-20, 700-45) 恒定偏移
    expect(Math.abs(moves2First.x - 500)).toBeLessThan(80);
    expect(Math.abs(moves2First.y - 500)).toBeLessThan(80);
    expect(moves1First).toBeDefined();
  });

  // F11（批5g 夹具真机实测）：后台 tab 不出帧，每个 mouseMoved 的 ack 要等满 5s 超时。
  // 旧实现逐条 await → 10–40 步轨迹 = 50–200s，press/release 永远发不出去，
  // 服务端只能看到 comment_send 45s 命令超时（页面上一个鼠标事件都没落地）。
  it('慢 ack：mouseMoved 永不回包也必须把 press/release 发下去', async () => {
    global.chrome = {
      debugger: {
        attach: vi.fn(async () => {}),
        detach: vi.fn(async () => {}),
        sendCommand: vi.fn(async (target, method, params) => {
          sent.push({ method, params });
          if (params.type === 'mouseMoved') return new Promise(() => {}); // 永不 ack
          return {};
        }),
        onDetach: { addListener: vi.fn() },
      },
    };
    vi.resetModules();
    const m = await import('../src/core/cdp/input.js');
    await m.clickAt(7, 200, 300); // 旧实现在这里永久挂住（本用例超时即红）
    const down = sent.find((c) => c.params.type === 'mousePressed');
    const up = sent.find((c) => c.params.type === 'mouseReleased');
    expect(down).toBeTruthy();
    expect(up).toBeTruthy();
    expect(sent.filter((c) => c.params.type === 'mouseMoved').length).toBeGreaterThanOrEqual(10);
  }, 20000);

  it('pressEnter 事件 text 为 \\r（Puppeteer 规范）', async () => {
    const m = await loadFresh();
    await m.pressEnter(4);
    const kd = sent.find((c) => c.params.type === 'keyDown');
    const ku = sent.find((c) => c.params.type === 'keyUp');
    expect(kd.params.text).toBe('\r');
    expect(kd.params.code).toBe('Enter');
    expect(ku).toBeDefined();
  });

  it('attach 抛 already-attached 被容忍，键入继续', async () => {
    global.chrome = { debugger: makeDebugger(vi.fn(async () => { throw new Error('Another debugger is already attached'); })) };
    vi.resetModules();
    const m = await import('../src/core/cdp/input.js');
    await m.typeText(9, 'x'); // 不应 reject
    expect(sent.some((c) => c.method === 'Input.dispatchKeyEvent' && c.params.type === 'keyDown')).toBe(true);
  });

  // ---- 批14：按下之后的任何失败都只能是「结局未知」，绝不能重放或原样上抛 ----
  // 理由：press 一旦入队，页面上这个动作就可能已经发生。此时
  //  ① 原始错误上抛 → 上层判「CDP 不可用」→ DOM 兜底再点一次 = 双发；
  //  ② withDebugger 的 detach 恢复重跑整个函数体 → 第二遍 press = 双发。
  // 两条路都必须堵死：统一收敛为 click_unacked，且输入类命令不重放。
  it('mousePressed 被 CDP 拒绝：上抛 click_unacked 而非原始错误', async () => {
    const m = await loadWithDebugger(makeDebuggerWith(async (_method, params) => {
      if (params?.type === 'mousePressed') throw new Error('Internal error');
      return {};
    }));
    await expect(m.clickAt(11, 200, 300)).rejects.toThrow(/click_unacked/);
  }, 20000);

  it('mouseReleased 报 detach 类错误：上抛 click_unacked 且不重放第二次 press', async () => {
    const m = await loadWithDebugger(makeDebuggerWith(async (_method, params) => {
      if (params?.type === 'mouseReleased') throw new Error('Debugger is not attached');
      return {};
    }));
    await expect(m.clickAt(12, 200, 300)).rejects.toThrow(/click_unacked/);
    expect(sent.filter((c) => c.params?.type === 'mousePressed').length).toBe(1);
  }, 20000);

  it('键入中途 detach 报错：已发的按键不重放（一次字符 = 一次 keyDown）', async () => {
    const m = await loadWithDebugger(makeDebuggerWith(async (_method, params) => {
      if (params?.type === 'keyUp') throw new Error('Cannot access a chrome:// URL');
      return {};
    }));
    await expect(m.typeText(13, 'ab')).rejects.toThrow(/Cannot access/);
    expect(sent.filter((c) => c.params?.type === 'keyDown').length).toBe(1);
  }, 20000);

  // ---- idle detach 定时器（批15 真机全量跑 rc=1 的真因）----
  // withDebugger 收尾挂了个 3s 定时器，回调里读 chrome.debugger 是**属性访问**：
  // SW 被回收/测试拆除后 chrome.debugger 不在，`.catch(()=>{})` 挡不住这个 TypeError，
  // 而它跑在定时器里没人接 ⇒ 未捕获异常。全量跑时用例之间真实时间会流过 3s，
  // 于是「12 文件 129 用例全绿 + 进程 rc=1」这种自相矛盾的结论就是这么来的。
  it('idle detach 定时器在 chrome.debugger 已消失时触发：不得抛，且每个 tab 的状态都要清掉', async () => {
    const m = await loadFresh();
    await m.clickAt(21, 100, 100);
    await m.clickAt(22, 120, 120);
    global.chrome = {}; // SW 上下文没了：chrome.debugger 是 undefined
    await vi.advanceTimersByTimeAsync(3100); // 修复前：属性访问即抛，循环半途而废且无人接
    const dbg = makeDebugger();
    global.chrome = { debugger: dbg };
    await m.clickAt(21, 300, 300);
    await m.clickAt(22, 320, 320);
    // 两个 tab 都必须重新 attach：状态没清就等于"3s 收横幅"从未生效
    expect(dbg.attach.mock.calls.map((c) => c[0].tabId).sort()).toEqual([21, 22]);
  }, 20000);

  // 同一个回调里的循环更不许半途而废：一个 tab 的 detach 抛，就把后面的 tab 全憋住
  // （调试横幅永不收起 = 用户看得见的残留）。
  it('idle detach 逐个吞错：一个 tab detach 抛不得憋住其余 tab', async () => {
    const dbg = makeDebugger();
    dbg.detach = vi.fn(async (t) => {
      if (t.tabId === 21) throw new Error('detach boom');
    });
    global.chrome = { debugger: dbg };
    vi.resetModules();
    const m = await import('../src/core/cdp/input.js');
    await m.clickAt(21, 100, 100);
    await m.clickAt(22, 120, 120);
    await vi.advanceTimersByTimeAsync(3100);
    expect(dbg.detach.mock.calls.map((c) => c[0].tabId).sort()).toEqual([21, 22]);
    // 两个 tab 的状态都要清掉：下一次命令必须重新 attach
    dbg.attach = vi.fn(async () => {});
    sent = [];
    await m.clickAt(21, 300, 300);
    expect(dbg.attach).toHaveBeenCalled();
  }, 20000);

  it('bezierPoints 纯函数：步数 10–40、端点收敛到目标', async () => {
    const m = await loadFresh();
    const pts = m.bezierPoints(0, 0, 1000, 0);
    expect(pts.length).toBeGreaterThanOrEqual(10);
    expect(pts.length).toBeLessThanOrEqual(40);
    const last = pts[pts.length - 1];
    expect(Math.abs(last.x - 1000)).toBeLessThanOrEqual(1);
    expect(Math.abs(last.y)).toBeLessThanOrEqual(1);
  });
});
