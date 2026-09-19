// 批9 扩展侧卫生：refs 按 tab 分桶 + click_unacked 不落 DOM 兜底
import { describe, it, expect, vi, beforeEach } from 'vitest';

// cdp/input.js 是命名空间导入（primitives.js 里 import * as cdpInput），
// 只能整模块替换；clickAt 的失败形态正是本批要分场景裁决的东西。
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

const fakeChrome = {
  scripting: {
    executeScript: vi.fn(async ({ func, args }) => [{ result: func(...(args || [])) }]),
  },
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

// jsdom 无布局引擎：可见性/几何两项必须手动给值，否则 probe 永远 zero_box，
// CDP 通道压根不会被调用（那样 click_unacked 的断言就是空跑）。
Object.defineProperty(global.HTMLElement.prototype, 'offsetParent', {
  get() { return this.parentElement ? document.body : null; },
  configurable: true,
});
global.HTMLElement.prototype.getBoundingClientRect = function () {
  return { x: 0, y: 0, left: 0, top: 0, right: 100, bottom: 40, width: 100, height: 40, toJSON() {} };
};
document.elementFromPoint = () => null;
// jsdom 不实现 innerText，而 injClickNear 按 innerText 找按钮——不补这一条，
// click_near 永远停在 button_not_found，unacked 分支根本没被走到（假绿）。
Object.defineProperty(global.HTMLElement.prototype, 'innerText', {
  get() { return this.textContent; },
  configurable: true,
});

beforeEach(() => {
  document.body.innerHTML = '';
  fakeChrome.scripting.executeScript.mockClear();
  cdp.clickAt.mockReset();
  cdp.clickAt.mockResolvedValue({ ok: true });
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

// 找出所有以 mode='fallback' 进入注入函数的调用（=DOM 兜底真的跑过）
const fallbackCalls = () => fakeChrome.scripting.executeScript.mock.calls
  .filter((c) => (c[0].args || []).includes('fallback'));

describe('refs 按 tab 分桶（批9）', () => {
  it('两个 tab 各自的 @eN 互不覆盖、互不清空', () => {
    const a = assembleSnapshot({
      nodes: [{ role: 'button', name: 'A页发送' }, { role: 'textbox', name: 'A页输入' }],
      paths: ['#a-send', '#a-input'],
      url: 'https://a.test',
    }, 11);
    const b = assembleSnapshot({
      nodes: [{ role: 'button', name: 'B页发送' }],
      paths: ['#b-send'],
      url: 'https://b.test',
    }, 22);
    expect(a.text).toContain('@e1');
    expect(b.text).toContain('@e1');

    // 旧实现（全局单桶 + 每次快照清空）在此处必然失败：
    // ① @e1 已被 B 覆盖 → A 的 ref 解析到 B 的元素；② A 的 @e2 已被清空 → 定位凭空失效。
    expect(getRefSelector('@e1', 11)).toBe('#a-send');
    expect(getRefSelector('@e2', 11)).toBe('#a-input');
    expect(getRefSelector('@e1', 22)).toBe('#b-send');
    expect(getRefSelector('@e2', 22)).toBeNull();
  });

  it('tabKey 数字/字符串同键（Go 侧 tab_id 是 JSON number，调用方混用不该裂成两桶）', () => {
    assembleSnapshot({ nodes: [{ role: 'button', name: 'x' }], paths: ['#k'], url: '' }, 7);
    expect(getRefSelector('@e1', '7')).toBe('#k');
    assembleSnapshot({ nodes: [{ role: 'button', name: 'y' }], paths: ['#s'], url: '' }, '8');
    expect(getRefSelector('@e1', 8)).toBe('#s');
  });

  it('导航清基线时同 tab 的 refs 一起作废，别的 tab 不受牵连', () => {
    assembleSnapshot({ nodes: [{ role: 'button', name: 'a' }], paths: ['#a'], url: '' }, 11);
    assembleSnapshot({ nodes: [{ role: 'button', name: 'b' }], paths: ['#b'], url: '' }, 22);
    resetSnapshotBaseline(11);
    expect(getRefSelector('@e1', 11)).toBeNull();
    expect(getRefSelector('@e1', 22)).toBe('#b');
  });

  it('同 tab 重新快照会重置该 tab 计数器（旧桶内容不外溢）', () => {
    assembleSnapshot({ nodes: [{ role: 'button', name: 'a' }], paths: ['#old'], url: '' }, 11);
    const again = assembleSnapshot({ nodes: [{ role: 'button', name: 'a' }], paths: ['#new'], url: '' }, 11);
    expect(again.text).toContain('@e1');
    expect(getRefSelector('@e1', 11)).toBe('#new');
  });

  it('dispatch resolve_ref 把 tab_id 带给解析器', async () => {
    assembleSnapshot({ nodes: [{ role: 'button', name: 'a' }], paths: ['#a-send'], url: '' }, 42);
    const deps = makeDeps();
    const spy = vi.spyOn(deps.accessibility, 'getRefSelector');
    const r = await dispatch({ action: 'resolve_ref', tab_id: 42, ref: '@e1' }, deps);
    expect(spy).toHaveBeenCalledWith('@e1', 42);
    expect(r.selector).toBe('#a-send');
    spy.mockRestore();
  });

  it('别的 tab 的 ref 不会串到当前 tab 的定位里', async () => {
    assembleSnapshot({ nodes: [{ role: 'button', name: 'a' }], paths: ['#tab42'], url: '' }, 42);
    assembleSnapshot({ nodes: [{ role: 'button', name: 'a' }], paths: ['#tab43'], url: '' }, 43);
    document.body.innerHTML = '<button id="tab42">A</button><button id="tab43">B</button>';
    const deps = makeDeps();
    const r = await dispatch({ action: 'resolve_ref', tab_id: 43, ref: '@e1' }, deps);
    expect(r.selector).toBe('#tab43');
  });
});

// 注入函数第一个实参（=真正进 document.querySelector 的定位串），按 mode 区分通道
const injectedTargets = (mode) => fakeChrome.scripting.executeScript.mock.calls
  .filter((c) => (c[0].args || []).includes(mode))
  .map((c) => c[0].args[0]);

describe('生产路径 @eN 定位按 tab 解析（批9：覆盖 click / type）', () => {
  // resolve_ref 走的是显式命令；click/type 走的是 dispatch 内部的 resolveTarget。
  // 两条路共用同一个分桶解析器，但只有后者是 LLM 每一步真正会走到的路径，
  // 所以必须单独锁——把 resolveTarget 的 tabId 参数摘掉（J8）只会红在这一条上。
  beforeEach(() => {
    assembleSnapshot({ nodes: [{ role: 'button', name: 'A页发送' }], paths: ['#tab42'], url: '' }, 42);
    assembleSnapshot({ nodes: [{ role: 'button', name: 'B页发送' }], paths: ['#tab43'], url: '' }, 43);
  });

  it('click：@e1 解析成当前 tab 的 selector 后才进注入', async () => {
    // 只把 B 页元素放进 DOM：解析错桶 → querySelector 落空 → element_not_found（不会假绿）
    document.body.innerHTML = '<button id="tab43">发送</button>';
    const deps = makeDeps();
    const r = await dispatch({ action: 'click', tab_id: 43, target: '@e1' }, deps);
    expect(r.channel).toBe('cdp');
    expect(injectedTargets('probe')).toEqual(['#tab43']);
    expect(fakeChrome.scripting.executeScript.mock.calls
      .map((c) => c[0].args[0]).every((a) => a !== '@e1')).toBe(true);
  });

  it('type：@e1 同样按当前 tab 解析', async () => {
    document.body.innerHTML = '<input id="tab43-input">';
    assembleSnapshot({ nodes: [{ role: 'textbox', name: 'B页输入' }], paths: ['#tab43-input'] }, 43);
    const deps = makeDeps();
    const r = await dispatch({ action: 'type', tab_id: 43, target: '@e1', value: '你好' }, deps);
    expect(r.channel).toBe('cdp');
    expect(injectedTargets('probe')).toEqual(['#tab43-input']);
  });

  it('反向锁：普通 selector 原样进注入（不经 ref 解析器）', async () => {
    document.body.innerHTML = '<input id="plain">';
    const deps = makeDeps();
    const r = await dispatch({ action: 'type', tab_id: 43, target: '#plain', value: 'x' }, deps);
    expect(r.channel).toBe('cdp');
    expect(injectedTargets('probe')).toEqual(['#plain']);
  });
});

describe('失效 @eN 报结构化错误名（批9：A1 自愈的触发词不能被打成语法错误）', () => {
  it('click 用失效 ref：报 element_not_found，且不做第二次注入', async () => {
    document.body.innerHTML = '<button id="tab43">发送</button>';
    assembleSnapshot({ nodes: [{ role: 'button', name: 'a' }], paths: ['#tab43'] }, 43);
    resetSnapshotBaseline(43); // 导航后 refs 全废，@e1 不再可解析
    const deps = makeDeps();
    await expect(dispatch({ action: 'click', tab_id: 43, target: '@e1' }, deps))
      .rejects.toThrow(/element_not_found/);
    // 旧行为：'@e1' 原样进 querySelector → DOMException（不是 element_not_found）
    // → 白跑一次 DOM 兜底注入 → 错误名对不上任何分类，自愈回路收不到。
    expect(fakeChrome.scripting.executeScript).not.toHaveBeenCalled();
  });

  it('comment_prep 用失效 ref：报 comment_input_not_found（Go 侧 healCommentInput 只认这个）', async () => {
    const deps = makeDeps();
    await expect(dispatch({ action: 'comment_prep', tab_id: 42, value: 'x', input_selector: '@e9' }, deps))
      .rejects.toThrow(/comment_input_not_found/);
    expect(fakeChrome.scripting.executeScript).not.toHaveBeenCalled();
  });

  it('comment_send 用失效 ref：同样报 comment_input_not_found（不是伪造 send_button_not_found）', async () => {
    const deps = makeDeps();
    await expect(dispatch({ action: 'comment_send', tab_id: 42, input_selector: '@e9' }, deps))
      .rejects.toThrow(/comment_input_not_found/);
    expect(cdp.clickAt).not.toHaveBeenCalled(); // 提交点绝不可因归因错误而被触碰
  });
});

describe('click_unacked 不落 DOM 兜底（批9）', () => {
  it('CDP 已下发但 ack 丢失：原样上抛，绝不二次点击', async () => {
    document.body.innerHTML = '<button id="send">发送</button>';
    let domClicks = 0;
    document.querySelector('#send').addEventListener('click', () => { domClicks += 1; });
    cdp.clickAt.mockRejectedValue(new Error('click_unacked'));
    const deps = makeDeps();
    await expect(dispatch({ action: 'click', tab_id: 42, target: '#send' }, deps))
      .rejects.toThrow(/click_unacked/);
    // 双发证据：兜底注入一次都不许跑（跑了就是又点了一次，公开评论不可撤回）
    expect(fallbackCalls()).toHaveLength(0);
    expect(domClicks).toBe(0);
  });

  it('click_near 同样不为 unacked 兜底', async () => {
    document.body.innerHTML = '<div><button>发送</button></div>';
    cdp.clickAt.mockRejectedValue(new Error('click_unacked'));
    const deps = makeDeps();
    await expect(dispatch({ action: 'click_near', tab_id: 42, anchor: 'div', button_text: '发送' }, deps))
      .rejects.toThrow(/click_unacked/);
    expect(fallbackCalls()).toHaveLength(0);
  });

  it('反向锁：CDP 从未下发（attach 被拒）时 DOM 兜底仍然保留', async () => {
    document.body.innerHTML = '<button id="send">发送</button>';
    cdp.clickAt.mockRejectedValue(new Error('debugger_not_available'));
    const deps = makeDeps();
    const r = await dispatch({ action: 'click', tab_id: 42, target: '#send' }, deps);
    expect(r.channel).toBe('dom_fallback');
    expect(fallbackCalls().length).toBeGreaterThan(0);
  });

  it('反向锁：元素不存在时不回退（原语义不变）', async () => {
    document.body.innerHTML = '<button id="send">发送</button>';
    const deps = makeDeps();
    await expect(dispatch({ action: 'click', tab_id: 42, target: '#ghost' }, deps))
      .rejects.toThrow(/element_not_found/);
    expect(cdp.clickAt).not.toHaveBeenCalled();
    expect(fallbackCalls()).toHaveLength(0);
  });
});
