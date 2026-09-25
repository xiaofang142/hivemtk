// 批18：type + submit_on_enter 的提交键失败不许被吞。
// 现场缺陷（第三轮深查）：case 'type' 里 `await cdpInput.pressEnter(tabId).catch(() => {})`
// ——Enter 派发失败时整句仍 `return { ok: true }`。而 type+submit_on_enter 命中平台注册的
// comment_input 时被 classifyStepEffect 判成**写步**（service/write_ledger.go:279），
// 于是 Go 侧记 status=success / submit_state=sent：一次「评论根本没发出去」的步骤
// 既报绿（假绿）又被双发闸永久拦死（唯一正确处置=看清页面后再跑一次，被自己锁掉）。
// 这里锁两件事：① 提交键失败必须上抛且不上报 ok；② 上抛文案不得含 Go 判「从未派发」的
// 三个 token（*_not_found / *_not_interactable / *_inject_timeout_）——含了就被判成
// 「没发生」，台账留空，反而放行双发。
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
import { getRefSelector } from '../src/core/accessibility.js';
import { strictExecuteScript } from './inject-sandbox.js';
import primitivesSrc from '../src/core/primitives.js?raw';

global.chrome = {
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

// 无布局引擎下给 probe 一个「有几何、可见」的假象（同批14 那份测试的准备）。
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

let hitTests = null;

beforeEach(() => {
  document.body.innerHTML = '';
  hitTests = { keydowns: 0, inputs: 0 };
  global.chrome.scripting.executeScript.mockClear();
  cdp.clickAt.mockReset();
  cdp.clickAt.mockResolvedValue({ ok: true });
  cdp.typeText.mockReset();
  cdp.typeText.mockResolvedValue(undefined);
  cdp.pressEnter.mockReset();
  cdp.pressEnter.mockResolvedValue(undefined);
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
    assemble: (c) => ({ nodes: c.nodes }),
    resetBaseline: () => {},
    getRefSelector,
  },
});

const NEVER_EXECUTED_TOKENS = ['_inject_timeout_', '_not_found', '_not_interactable'];

describe('CDP 通道：提交键失败=整步失败（批18）', () => {
  it('pressEnter 抛错时上抛，且不回读成 ok:true', async () => {
    document.body.innerHTML = '<input id="q" value="">';
    cdp.pressEnter.mockRejectedValue(new Error('Debugger is not attached to the target'));
    await expect(dispatch(
      { action: 'type', tab_id: 42, target: '#q', value: '真好吃', submit_on_enter: true }, makeDeps()))
      .rejects.toThrow();
  });

  it('失败文案不含 Go 判「从未派发」的任何 token', async () => {
    document.body.innerHTML = '<input id="q" value="">';
    cdp.pressEnter.mockRejectedValue(new Error('Debugger is not attached to the target'));
    const err = await dispatch(
      { action: 'type', tab_id: 42, target: '#q', value: '真好吃', submit_on_enter: true }, makeDeps())
      .catch((e) => e);
    expect(err).toBeInstanceOf(Error);
    for (const token of NEVER_EXECUTED_TOKENS) {
      expect(String(err.message)).not.toContain(token);
    }
  });

  it('上抛的错误必须带原始失败原因（cause）：排"为什么没按下去"只有这一条线索', async () => {
    // 超时 / CDP 直接拒 / debugger 掉线三种成因的处置完全不同（前者要查预算、后两者要查装机），
    // 把原因只剩在 message 字符串里=人读得到、程序读不到；ESLint 的 preserve-caught-error
    // 在 user-web 是 error 级门禁（CI eslint-user-web job 阻断），不写 cause 就是给门里塞红。
    document.body.innerHTML = '<input id="q" value="">';
    const root = new Error('Debugger is not attached to the target');
    cdp.pressEnter.mockRejectedValue(root);
    const err = await dispatch(
      { action: 'type', tab_id: 42, target: '#q', value: '真好吃', submit_on_enter: true }, makeDeps())
      .catch((e) => e);
    expect(err).toBeInstanceOf(Error);
    expect(err.cause).toBe(root);
    // message 形状不许因带 cause 而变：Go 侧按前缀归因（write_ledger 的 never-executed 判据读的是它）
    expect(String(err.message).startsWith('submit_key_not_dispatched: ')).toBe(true);
  });

  it('提交键失败不得触发 DOM 兜底重打一遍正文（一次步骤一次输入）', async () => {
    const el = document.createElement('input');
    el.id = 'q';
    el.value = '旧稿';
    el.addEventListener('input', () => { hitTests.inputs += 1; });
    document.body.appendChild(el);
    cdp.pressEnter.mockRejectedValue(new Error('Debugger is not attached to the target'));
    await dispatch(
      { action: 'type', tab_id: 42, target: '#q', value: '真好吃', submit_on_enter: true }, makeDeps())
      .catch(() => {});
    // 读数含义：probe 只做「清空+聚焦」＝1 次注入 1 次 input；兜底注入会再清一次并写正文。
    // 这条现在是绿的，它是「修成上抛后别顺手把整步重跑一遍输入」的反向锁。
    expect(cdp.typeText).toHaveBeenCalledTimes(1);
    expect(global.chrome.scripting.executeScript).toHaveBeenCalledTimes(1);
    expect(hitTests.inputs).toBe(1);
    expect(el.value).toBe('');
  });

  it('要求提交时恰好按一次 Enter；未要求时一次都不按', async () => {
    document.body.innerHTML = '<input id="q" value="">';
    await dispatch(
      { action: 'type', tab_id: 42, target: '#q', value: 'x', submit_on_enter: true }, makeDeps());
    expect(cdp.pressEnter).toHaveBeenCalledTimes(1);

    cdp.pressEnter.mockClear();
    document.body.innerHTML = '<input id="q2" value="">';
    await dispatch(
      { action: 'type', tab_id: 42, target: '#q2', value: 'x' }, makeDeps());
    expect(cdp.pressEnter).toHaveBeenCalledTimes(0);
  });
});

describe('DOM 兜底通道：Enter 只发一次（批18 防过修正成双发）', () => {
  it('兜底输入后不再补按 CDP Enter，页面侧 keydown 恰好一次', async () => {
    const el = document.createElement('input');
    el.id = 'q';
    el.addEventListener('keydown', () => { hitTests.keydowns += 1; });
    document.body.appendChild(el);
    cdp.typeText.mockRejectedValue(new Error('Debugger is not attached to the target'));
    const r = await dispatch(
      { action: 'type', tab_id: 42, target: '#q', value: '真好吃', submit_on_enter: true }, makeDeps());
    expect(r.channel).toBe('dom_fallback');
    expect(hitTests.keydowns).toBe(1);
    expect(cdp.pressEnter).toHaveBeenCalledTimes(0);
  });
});

describe('dispatch 的 type 分支不得再出现空 catch（批18 静态锁）', () => {
  it('吞错的 .catch(() => {}) 是本批要消灭的形状', () => {
    // 锚点必须带 `{`：`case 'type':` 在「需要活 tab 的清单」那个 switch 里也出现一次
    // （:784），不带花括号就锁到清单那一段、空 catch 落在窗外——实测这样会白过。
    expect((primitivesSrc.match(/case 'type': \{/g) || []).length).toBe(1);
    const i = primitivesSrc.indexOf("case 'type': {");
    const j = primitivesSrc.indexOf("case 'wait_for_selector': {", i);
    expect(j).toBeGreaterThan(i);
    const body = primitivesSrc.slice(i, j);
    expect(body).toContain('pressEnter');
    // 空 catch 吞掉的正是「输入成了、提交键没发出去」这一事实（`.catch(() => null)` 不同：
    // 那个 null 会走 `if (!r?.ok) throw` 上抛，不是一片绿）。
    expect(body).not.toContain('.catch(() => {})');
  });
});
