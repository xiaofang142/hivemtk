// 批14 A1 取证落地：一次 click 步骤 = 一次 handler 调用。
// 真机证据（session 536/537，/tmp/b14_ev537.txt）：夹具页面只收到 6 个事件、全部
// isTrusted=false，其中 click 两个（t=3460 与 t=3461）→ 按钮计数 CLICKED-2。
// 成因在 injClick 的 DOM 兜底：el.click() 之外又补发了一个 MouseEvent('click')。
// 对「发送/发布」按钮这就是双发公开内容，且不可撤回。
import { describe, it, expect, vi, beforeEach } from 'vitest';

const { cdp } = vi.hoisted(() => ({
  cdp: {
    clickAt: vi.fn(async () => ({ ok: true })),
    typeText: vi.fn(async () => {}),
    pressEnter: vi.fn(async () => {}),
  },
}));
vi.mock('../src/core/cdp/input.js', () => cdp);

import { dispatch } from '../src/core/primitives.js';
import { assembleSnapshot, getRefSelector, resetSnapshotBaseline } from '../src/core/accessibility.js';
import { deserializedAsPage, strictExecuteScript } from './inject-sandbox.js';

const fakeChrome = {
  scripting: { executeScript: strictExecuteScript },
  tabs: {
    create: vi.fn(async (opts) => ({ id: 42, ...opts })),
    get: vi.fn(async (id) => ({ id })),
    remove: vi.fn(async () => {}),
    update: vi.fn(async () => {}),
    captureVisibleTab: vi.fn(async () => 'data:image/png;base64,AAAA'),
  },
  windows: { update: vi.fn(async () => {}) },
};
global.chrome = fakeChrome;

// 无布局引擎下给 probe 一个「有几何、可见」的假象（否则永远 zero_box，走不到被测分支）。
Object.defineProperty(global.HTMLElement.prototype, 'offsetParent', {
  get() { return this.parentElement ? document.body : null; },
  configurable: true,
});
global.HTMLElement.prototype.getBoundingClientRect = function () {
  return { x: 0, y: 0, left: 0, top: 0, right: 100, bottom: 40, width: 100, height: 40, toJSON() {} };
};
Object.defineProperty(global.HTMLElement.prototype, 'innerText', {
  get() { return this.textContent; },
  configurable: true,
});
// jsdom 不实现 isContentEditable（injType 的富文本分支全靠它）
Object.defineProperty(global.HTMLElement.prototype, 'isContentEditable', {
  get() { return this.getAttribute('contenteditable') === 'true'; },
  configurable: true,
});
// jsdom 也没有 PointerEvent，且它拒绝任何 view（实测 new MouseEvent('click',{view:window})
// 与 {view:document.defaultView} 都抛「member view is not of type Window」）。
// 兜底序列因此在单测里会在第一个构造处抛错、被生产代码那个 catch 整段吞掉 →
// 单测永远只看到 1 次 click，双发测不出来（本轮之前的真实盲区，session 536/537 才暴露）。
// 真 Chrome 两样都有/都接受，这里按浏览器事实补齐，而不是把 jsdom 的缺失当语义。
const NativeMouseEvent = global.MouseEvent;
const buildMouseEvent = (type, params) => {
  try {
    return new NativeMouseEvent(type, params);
  } catch {
    const withoutView = { ...params };
    delete withoutView.view;
    return new NativeMouseEvent(type, withoutView);
  }
};
global.MouseEvent = class MouseEventShim extends NativeMouseEvent {
  constructor(type, params = {}) {
    return buildMouseEvent(type, params);
  }
};
if (typeof global.PointerEvent === 'undefined') {
  global.PointerEvent = class PointerEventShim extends NativeMouseEvent {
    constructor(type, params = {}) {
      const e = buildMouseEvent(type, params);
      Object.defineProperty(e, 'pointerId', { value: params.pointerId ?? 0, configurable: true });
      Object.defineProperty(e, 'isPrimary', { value: params.isPrimary ?? false, configurable: true });
      Object.defineProperty(e, 'pointerType', { value: params.pointerType ?? 'mouse', configurable: true });
      return e;
    }
  };
}

let hitTests = null;

beforeEach(() => {
  document.body.innerHTML = '';
  hitTests = { clicks: 0, pointerdowns: 0, keydowns: 0 };
  document.elementFromPoint = () => null; // 默认无遮挡
  fakeChrome.scripting.executeScript.mockClear();
  cdp.clickAt.mockReset();
  cdp.clickAt.mockResolvedValue({ ok: true });
  cdp.typeText.mockReset();
  cdp.typeText.mockResolvedValue(undefined);
  resetSnapshotBaseline();
});

const makeDeps = () => ({
  tabManager: {
    openTab: vi.fn(async (url) => ({ id: 42, url })),
    waitForLoad: vi.fn(async () => ({ loaded: true, title: '', wait_ms: 20 })),
    closeTab: vi.fn(async () => {}),
    activateTab: vi.fn(async () => {}),
    tabExists: vi.fn(async (id) => id !== 999),
  },
  accessibility: {
    collectInPage: () => ({ nodes: [], paths: [] }),
    assemble: (collected, tabKey) => assembleSnapshot(collected, tabKey),
    resetBaseline: resetSnapshotBaseline,
    getRefSelector,
  },
});

// CDP 不可用（attach 被拒 / 后台 tab 不出帧）→ 必然进 DOM 兜底。
const cdpUnavailable = () =>
  cdp.clickAt.mockRejectedValue(new Error('Debugger is not attached to the target'));

const attachCounter = (el) => {
  el.addEventListener('click', () => { hitTests.clicks += 1; });
  el.addEventListener('pointerdown', () => { hitTests.pointerdowns += 1; });
  el.addEventListener('keydown', () => { hitTests.keydowns += 1; });
};

describe('注入沙箱自身必须咬得住（批14：闸门的反向测试）', () => {
  const LEAKED = '模块作用域的常量';
  function leakyInjected(el) { return LEAKED; }

  it('引用模块自由变量的注入函数，在沙箱里必抛 ReferenceError', () => {
    // 这条不成立就说明沙箱是摆设：injClick 当初引用 actionabilityCheck 的死法
    // 全绿跑了 20 多个批次（session 536/537 才由真机事件序列暴露）。
    const pageFn = deserializedAsPage(leakyInjected);
    expect(() => pageFn('#x')).toThrow(ReferenceError);
    // 原始闭包版本反而正常返回——这正是旧 mock 永远测不出来的原因。
    expect(leakyInjected('#x')).toBe(LEAKED);
  });
});

describe('click 主通道（批14：trusted 必须真的跑得起来）', () => {
  it('probe 在去闭包后仍能算出坐标：CDP 可用时 channel=cdp 且 clickAt 收到正数坐标', async () => {
    document.body.innerHTML = '<button id="send">发送</button>';
    attachCounter(document.querySelector('#send'));
    const r = await dispatch({ action: 'click', tab_id: 42, target: '#send' }, makeDeps());
    // 注入函数一旦引用模块作用域的自由变量，页面里就是 ReferenceError→
    // result:null→executeInTab 抛 inject_no_result→被误判成「CDP 不可用」而降级。
    // 这条断言把「trusted 通道其实从没跑过」钉死为红。
    expect(r.channel).toBe('cdp');
    expect(cdp.clickAt).toHaveBeenCalledTimes(1);
    const [tabId, x, y] = cdp.clickAt.mock.calls[0];
    expect(tabId).toBe(42);
    expect(x).toBeGreaterThan(0);
    expect(y).toBeGreaterThan(0);
  });

  it('trusted 路径不往页面下发任何合成事件（页面侧 handler 零次调用）', async () => {
    document.body.innerHTML = '<button id="send">发送</button>';
    attachCounter(document.querySelector('#send'));
    await dispatch({ action: 'click', tab_id: 42, target: '#send' }, makeDeps());
    expect(hitTests.clicks).toBe(0);
    expect(hitTests.pointerdowns).toBe(0);
  });

  it('click_near 主通道同样经 probe 拿坐标后走 CDP', async () => {
    document.body.innerHTML = '<div><textarea></textarea><button id="send">发送</button></div>';
    attachCounter(document.querySelector('#send'));
    const r = await dispatch(
      { action: 'click_near', tab_id: 42, anchor: 'textarea', button_text: '发送' }, makeDeps());
    expect(r.channel).toBe('cdp');
    expect(cdp.clickAt).toHaveBeenCalledTimes(1);
    expect(hitTests.clicks).toBe(0);
  });
});

describe('click DOM 兜底：一次步骤一次动作（批14）', () => {
  it('click 兜底路径：按钮 click handler 恰好执行一次', async () => {
    document.body.innerHTML = '<button id="send">发送</button>';
    attachCounter(document.querySelector('#send'));
    cdpUnavailable();
    const r = await dispatch({ action: 'click', tab_id: 42, target: '#send' }, makeDeps());
    expect(r.channel).toBe('dom_fallback'); // 前提：确实走的是兜底
    expect(hitTests.clicks).toBe(1);
  });

  it('click_near 兜底路径：按钮 click handler 恰好执行一次', async () => {
    document.body.innerHTML = '<div><textarea></textarea><button id="send">发送</button></div>';
    attachCounter(document.querySelector('#send'));
    cdpUnavailable();
    const r = await dispatch(
      { action: 'click_near', tab_id: 42, anchor: 'textarea', button_text: '发送' }, makeDeps());
    expect(r.channel).toBe('dom_fallback');
    expect(hitTests.clicks).toBe(1);
  });

  it('兜底仍下发完整指针序列（防过修正成只剩 el.click()）', async () => {
    document.body.innerHTML = '<button id="send">发送</button>';
    attachCounter(document.querySelector('#send'));
    cdpUnavailable();
    await dispatch({ action: 'click', tab_id: 42, target: '#send' }, makeDeps());
    expect(hitTests.pointerdowns).toBe(1);
  });
});

describe('probe 判不可交互不得被兜底绕过（批14）', () => {
  it('元素被浮层遮挡：上抛 element_not_interactable，且 handler 零次调用', async () => {
    document.body.innerHTML = '<button id="send">发送</button><div id="mask"></div>';
    const btn = document.querySelector('#send');
    attachCounter(btn);
    document.elementFromPoint = () => document.querySelector('#mask');
    await expect(dispatch({ action: 'click', tab_id: 42, target: '#send' }, makeDeps()))
      .rejects.toThrow(/element_not_interactable/);
    expect(hitTests.clicks).toBe(0);
  });

  // 同一条闸门对 type 也必须成立：probe 判只读/不可见时兜底不得把字写进去。
  it('输入框只读：上抛 element_not_interactable，且值里没被写进东西', async () => {
    document.body.innerHTML = '<input id="q" aria-readonly="true" value="">';
    await expect(dispatch(
      { action: 'type', tab_id: 42, target: '#q', value: '真好吃' }, makeDeps()))
      .rejects.toThrow(/element_not_interactable/);
    expect(document.querySelector('#q').value).toBe('');
  });
});

describe('type 富文本兜底不得无谓发 Enter（批14）', () => {
  it('contenteditable 兜底：未要求 submit_on_enter 时不派发 keydown Enter', async () => {
    document.body.innerHTML = '<div id="ce" contenteditable="true"></div>';
    const ce = document.querySelector('#ce');
    attachCounter(ce);
    cdp.typeText.mockRejectedValue(new Error('Debugger is not attached to the target'));
    const r = await dispatch(
      { action: 'type', tab_id: 42, target: '#ce', value: '真好吃' }, makeDeps());
    expect(r.channel).toBe('dom_fallback');
    expect(hitTests.keydowns).toBe(0);
  });

  it('明确要求 submit_on_enter 时才发 Enter', async () => {
    document.body.innerHTML = '<div id="ce" contenteditable="true"></div>';
    const ce = document.querySelector('#ce');
    attachCounter(ce);
    cdp.typeText.mockRejectedValue(new Error('Debugger is not attached to the target'));
    await dispatch({ action: 'type', tab_id: 42, target: '#ce', value: 'x', submit_on_enter: true }, makeDeps());
    expect(hitTests.keydowns).toBe(1);
  });
});
