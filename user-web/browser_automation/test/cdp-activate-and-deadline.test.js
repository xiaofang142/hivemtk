// 根因修复的回归闸（用户「必须彻底解决问题」轮）：
//
// ① 根因——openTab 恒 active:false 寄生式后台打开，后台 tab 渲染进程不产帧，
//    CDP Input 事件的 ack 压栈（真机实测 3.1s/条，激活后塌到 1~346ms）。
//    cdp/input.js 的 deadline 只是止损（挂死→快速报错），**光有 deadline 评论照样发不出去**。
//    本文件锁住「写通道前置激活」这条根因修复：没激活就没有 trusted 写。
// ② 同类隐患——executeInTab 原本无 deadline，读动作挂重页主线程会以同一形态黑盒。
// ③ 总闸——comment_prep 的定位(15s)+键入(15s)相加会越过服务端 30s 闸。
//
// 三者缺一都会重现「host 命令超时」黑盒，且都不是靶站能测出来的（靶站无持续渲染）。
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

import { createTabManager } from '../src/core/tab-manager.js';

// ---- primitives 需要 mock 掉真实 CDP 模块，才能断言调用序 ----
const { cdp } = vi.hoisted(() => ({
  cdp: {
    clickAt: vi.fn(async () => ({ ok: true })),
    typeText: vi.fn(async () => ({ ok: true, chars: 1, typed: 1 })),
    pressEnter: vi.fn(async () => ({ ok: true })),
  },
}));
vi.mock('../src/core/cdp/input.js', () => cdp);

const { dispatch } = await import('../src/core/primitives.js');
const { strictExecuteScript } = await import('./inject-sandbox.js');

const fakeChrome = {
  scripting: { executeScript: vi.fn() },
  tabs: {
    create: vi.fn(async (opts) => ({ id: 42, ...opts })),
    get: vi.fn(async (id) => {
      if (id === 999) throw new Error('No tab with id: 999');
      return { id, active: false, windowId: 7 };
    }),
    remove: vi.fn(async () => {}),
    update: vi.fn(async () => {}),
    captureVisibleTab: vi.fn(async () => 'data:image/png;base64,AAAA'),
  },
  windows: { update: vi.fn(async () => {}) },
};
global.chrome = fakeChrome;

// 默认注入通道 = 真实的 strictExecuteScript（去闭包执行注入函数）。
// 必须逐例重置：vi.clearAllMocks() 只清调用记录、**不清实现**，
// 否则某个用例的 mockImplementation 会漏给后续用例（本次就踩过：
// click 用例的假实现让 comment_prep 用例拿到了 x/y 而非 input_found）。
const defaultExecuteScript = async (arg) => strictExecuteScript(arg);

Object.defineProperty(global.HTMLElement.prototype, 'offsetParent', {
  get() { return this.parentElement ? document.body : null; },
  configurable: true,
});
global.HTMLElement.prototype.getBoundingClientRect = function () {
  return { x: 0, y: 0, left: 0, top: 0, right: 100, bottom: 40, width: 100, height: 40, toJSON() {} };
};
Object.defineProperty(global.HTMLElement.prototype, 'isContentEditable', {
  get() { return this.getAttribute('contenteditable') === 'true'; },
  configurable: true,
});
// jsdom 无 innerText：不补这个，按钮文案读出来是空串 ⇒ injPostCommentSend 一律
// send_button_not_found（本次就踩过，与产品代码无关）。
Object.defineProperty(global.HTMLElement.prototype, 'innerText', {
  get() { return this.textContent; },
  configurable: true,
});

const makeDeps = () => ({
  tabManager: {
    openTab: vi.fn(async (url) => ({ id: 42, url })),
    waitForLoad: vi.fn(async () => ({ loaded: true, title: 't', wait_ms: 30 })),
    closeTab: vi.fn(async () => {}),
    activateTab: vi.fn(async () => {}),
    ensureActiveForInput: vi.fn(async () => {}),
    tabExists: vi.fn(async (id) => id !== 999),
  },
  accessibility: {
    collectInPage: () => ({ nodes: [], paths: [] }),
    assemble: () => ({ snapshot: '' }),
    getRefSelector: (ref) => (ref === '@e1' ? '#real-btn' : null),
  },
});

beforeEach(() => {
  document.body.innerHTML = '';
  vi.clearAllMocks();
  fakeChrome.scripting.executeScript.mockReset();
  fakeChrome.scripting.executeScript.mockImplementation(defaultExecuteScript);
  fakeChrome.tabs.get.mockImplementation(async (id) => {
    if (id === 999) throw new Error('No tab with id: 999');
    return { id, active: false, windowId: 7 };
  });
  cdp.typeText.mockImplementation(async () => ({ ok: true, chars: 1, typed: 1 }));
});

// ═══════════════ ① 根因：写通道前置激活 ═══════════════

describe('tab-manager.ensureActiveForInput —— 后台 tab 不出帧是 CDP ack 压栈的根因', () => {
  it('已激活的 tab 不做任何事（零开销，也完全不打断用户）', async () => {
    fakeChrome.tabs.get.mockResolvedValue({ id: 5, active: true, windowId: 7 });
    const tm = createTabManager(fakeChrome);
    await tm.ensureActiveForInput(5);
    expect(fakeChrome.tabs.update).not.toHaveBeenCalled();
    expect(fakeChrome.windows.update).not.toHaveBeenCalled();
  });

  it('后台 tab 被切到激活态，且不抢窗口焦点（抢焦点会打断用户手头的工作）', async () => {
    const tm = createTabManager(fakeChrome);
    await tm.ensureActiveForInput(5);
    expect(fakeChrome.tabs.update).toHaveBeenCalledWith(5, { active: true });
    // 关键断言：绝不能调 windows.update({focused})——那是 activateTab（截图用）的行为。
    // 截图需要真实可见，写输入只需要出帧；抢焦点是纯打扰。
    expect(fakeChrome.windows.update).not.toHaveBeenCalled();
  });

  it('tab 已消失时静默返回，交上层 deadline 兜底（不抛 tab 错误掩盖真实卡点）', async () => {
    const tm = createTabManager(fakeChrome);
    await expect(tm.ensureActiveForInput(999)).resolves.toBeUndefined();
    expect(fakeChrome.tabs.update).not.toHaveBeenCalled();
  });
});

describe('dispatch：CDP 写之前必须激活 tab', () => {
  it('click：先定位注入（纯读，后台跑得动）→ 再激活 → 再 trusted 点', async () => {
    document.body.innerHTML = '<button id="real-btn">按钮</button>';
    const deps = makeDeps();
    const order = [];
    deps.tabManager.ensureActiveForInput.mockImplementation(async () => { order.push('activate'); });
    cdp.clickAt.mockImplementation(async () => { order.push('click'); return { ok: true }; });
    fakeChrome.scripting.executeScript.mockImplementation(async () => { order.push('inject'); return [{ result: { ok: true, x: 5, y: 5 } }]; });

    await dispatch({ action: 'click', tab_id: 42, target: '#real-btn' }, deps);

    // 定位在前、激活在中、写在后：顺序反了会白激活（定位不需要帧），
    // 或漏激活（写在后台 tab 上 = ack 压栈 = 30s 黑盒）。
    // 尾部还多一次 inject 是点后导航复核（injNavigatedCheck），那是既有行为，不参与本断言。
    expect(order.slice(0, 3)).toEqual(['inject', 'activate', 'click']);
  });

  it('comment_prep：contenteditable 走 trusted 键入前先激活', async () => {
    document.body.innerHTML = '<div id="editor" contenteditable="true"></div>';
    const deps = makeDeps();
    const order = [];
    deps.tabManager.ensureActiveForInput.mockImplementation(async () => { order.push('activate'); });
    cdp.typeText.mockImplementation(async () => { order.push('type'); return { ok: true, chars: 1, typed: 1 }; });

    await dispatch({ action: 'comment_prep', tab_id: 42, value: '真好吃' }, deps);

    expect(order).toEqual(['activate', 'type']);
  });

  it('comment_send：提交点击前先激活（这是不可逆动作，不能丢）', async () => {
    document.body.innerHTML = '<div class="comments-container"><div><textarea></textarea><button>发送</button></div></div>';
    const deps = makeDeps();
    const order = [];
    deps.tabManager.ensureActiveForInput.mockImplementation(async () => { order.push('activate'); });
    cdp.clickAt.mockImplementation(async () => { order.push('click'); return { ok: true }; });

    await dispatch({ action: 'comment_send', tab_id: 42, value: '真好吃' }, deps);

    expect(order).toEqual(['activate', 'click']);
  });

  it('纯读动作不激活（读不需要帧，激活只会白白打断用户）', async () => {
    document.body.innerHTML = '<div id="box">内容</div>';
    const deps = makeDeps();
    fakeChrome.scripting.executeScript.mockImplementation(async () => [{ result: { ok: true, data: { box: '内容' } } }]);

    await dispatch({ action: 'extract', tab_id: 42, selectors: { box: '#box' } }, deps);

    expect(deps.tabManager.ensureActiveForInput).not.toHaveBeenCalled();
  });

  it('旧版 deps（无 ensureActiveForInput）不得崩：退化为空操作', async () => {
    document.body.innerHTML = '<button id="real-btn">按钮</button>';
    const deps = makeDeps();
    delete deps.tabManager.ensureActiveForInput;
    fakeChrome.scripting.executeScript.mockImplementation(async () => [{ result: { ok: true, x: 5, y: 5 } }]);
    // 只要求不抛 TypeError：真实 tabManager 已带该方法，这里锁的是向后兼容。
    await expect(dispatch({ action: 'click', tab_id: 42, target: '#real-btn' }, deps)).resolves.toBeTruthy();
  });
});

// ═══════════════ ② 同类隐患：executeInTab 的 deadline ═══════════════

describe('executeInTab：注入挂死必须早于服务端闸门自己回帧', () => {
  afterEach(() => { vi.useRealTimers(); });

  it('executeScript 永不回包 → query 在 20s 本地闸报错，而不是让服务端等满 30s 黑盒', async () => {
    vi.useFakeTimers();
    const deps = makeDeps();
    fakeChrome.scripting.executeScript.mockImplementation(() => new Promise(() => {})); // 永不 settle
    const p = dispatch({ action: 'query', tab_id: 42, query: 'count', selector: '.x' }, deps);
    const settled = p.then(() => 'resolved', (e) => e.message);
    await vi.advanceTimersByTimeAsync(20001);
    // 错误名含 _inject_timeout_ ⇒ 服务端 isNeverExecuted 归类「从未发生」
    expect(await settled).toMatch(/_inject_timeout_/);
  });

  it('markdown 用 45s 预算（大 DOM 页），不是默认 20s —— 真站整页序列化实测很慢', async () => {
    vi.useFakeTimers();
    const deps = makeDeps();
    fakeChrome.scripting.executeScript.mockImplementation(() => new Promise(() => {}));
    const p = dispatch({ action: 'markdown', tab_id: 42 }, deps);
    const settled = p.then(() => 'resolved', (e) => e.message);
    // 20s 默认闸放行后仍在等 ⇒ 说明确实走了 45s 预算
    await vi.advanceTimersByTimeAsync(20001);
    expect(await Promise.race([settled, Promise.resolve('still-waiting')])).toBe('still-waiting');
    await vi.advanceTimersByTimeAsync(25001);
    expect(await settled).toMatch(/inject_inject_timeout_45000ms/);
  });

  it('wait_for_selector 的本地预算要覆盖页内自计时，否则会掐断一次合法等待', async () => {
    vi.useFakeTimers();
    const deps = makeDeps();
    fakeChrome.scripting.executeScript.mockImplementation(() => new Promise(() => {}));
    const p = dispatch({ action: 'wait_for_selector', tab_id: 42, selector: '.x', timeout_ms: 10000 }, deps);
    const settled = p.then(() => 'resolved', (e) => e.message);
    await vi.advanceTimersByTimeAsync(20001);
    // 页内自计时 10s + 5s 回程余量 = 15s，早于默认 20s 生效 → 早于 20s 就该报
    expect(await settled).toMatch(/inject_inject_timeout_15000ms/);
  });
});

// ═══════════════ ③ comment_prep 总闸 ═══════════════

describe('comment_prep 总闸：各层 deadline 相加仍会越服务端 30s 闸', () => {
  afterEach(() => { vi.useRealTimers(); });

  it('定位注入 15s 用满 + 键入一直挂着 → 整段在 25s 总闸收手', async () => {
    vi.useFakeTimers();
    const deps = makeDeps();
    // 定位注入慢（15s）但最终成功；键入永不回包
    fakeChrome.scripting.executeScript.mockImplementation(async () => {
      await new Promise((r) => setTimeout(r, 14000));
      return [{ result: { ok: true, input_found: true, needs_trusted: true, input_text: '' } }];
    });
    cdp.typeText.mockImplementation(() => new Promise(() => {}));

    const p = dispatch({ action: 'comment_prep', tab_id: 42, value: '真好吃' }, deps);
    const settled = p.then(() => 'resolved', (e) => e.message);

    await vi.advanceTimersByTimeAsync(25001);
    // 若无总闸：定位 15s + 键入挂死 ⇒ 越过服务端 30s ⇒ 「host 命令超时」黑盒。
    // 有总闸：25s < 30s ⇒ 扩展先回一个可归因的帧。
    expect(await settled).toMatch(/comment_prep_total_inject_timeout_25000ms/);
  });

  it('总闸错误名必须含 _inject_timeout_：服务端据此判「从未发生」，台账才可不落行', async () => {
    // prep 阶段副作用上限只是「输入框多了几个字」，提交是独立的 comment_send，
    // 故把它归成「从未提交过」是安全的；带错名字会被判成 unattributed → 双发闸锁死文本。
    const err = await (async () => {
      try {
        vi.useFakeTimers();
        const deps = makeDeps();
        fakeChrome.scripting.executeScript.mockImplementation(() => new Promise(() => {}));
        const p = dispatch({ action: 'comment_prep', tab_id: 42, value: 'x' }, deps);
        p.catch(() => {});
        await vi.advanceTimersByTimeAsync(25001);
        return await p.then(() => 'resolved', (e) => e.message);
      } finally {
        vi.useRealTimers();
      }
    })();
    expect(err).toContain('_inject_timeout_');
  });
});
