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
