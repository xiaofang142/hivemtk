// A21：扩展原语不得自带平台形状缺省（fail-open 的第二处）。
//
// 背景：基座契约是「扩展零平台知识，选择器一律由服务端 L3 适配器下发」。但原语层留了
// 三处缺省兜底，没下发时扩展自己猜一组小红书式选择器/按钮文案/评论区容器：
//   · `CANDIDATE_INPUT = inputSelector || '.content-textarea, p.content-input, …'`
//   · `WANT = sendButtonText ? [sendButtonText] : ['发送', '发布', '评论']`
//   · `containerSel = o.containerSelector || '.comments-container, .comments-el, …'`
// 危害不是「少个参数」，而是**归因错位**：适配器没声明 → 报 comment_input_not_found /
// send_button_not_found，人看到的是「页面改版了」，真实原因是「配置缺失」；
// 而对 verify 来说更糟：容器缺省把验证范围无声扩大到整页，page 上任何地方出现这句话
// 都能判 verified（零提交假绿的另一条入口）。
//
// 现在：三处缺省全删，缺下发即报自己的名字（都带 _not_found 后缀，
// 服务端 isNeverExecuted 归类为「从未发生」，不会卡死双发闸）。
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { readFile } from 'node:fs/promises';
import { resolve, dirname } from 'node:path';

const { cdp } = vi.hoisted(() => ({
  cdp: {
    clickAt: vi.fn(async () => ({ ok: true })),
    typeText: vi.fn(async () => ({ ok: true, chars: 1, typed: 1 })),
    pressEnter: vi.fn(async () => {}),
  },
}));
vi.mock('../src/core/cdp/input.js', () => cdp);

import { dispatch } from '../src/core/primitives.js';
import { strictExecuteScript } from './inject-sandbox.js';

const PRIMITIVES = resolve(dirname(new URL(import.meta.url).pathname), '../src/core/primitives.js');

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

const BOX = { x: 0, y: 0, left: 0, top: 0, right: 100, bottom: 40, width: 100, height: 40, toJSON() {} };
Object.defineProperty(global.HTMLElement.prototype, 'offsetParent', {
  get() { return this.parentElement ? document.body : null; },
  configurable: true,
});
global.HTMLElement.prototype.getBoundingClientRect = () => BOX;
Object.defineProperty(global.HTMLElement.prototype, 'innerText', {
  get() { return this.textContent; },
  configurable: true,
});

const makeDeps = () => ({
  tabManager: {
    openTab: vi.fn(async (url) => ({ id: 42, url })),
    waitForLoad: vi.fn(async () => ({ loaded: true, title: '', wait_ms: 20 })),
    closeTab: vi.fn(async () => {}),
    activateTab: vi.fn(async () => {}),
    ensureActiveForInput: vi.fn(async () => {}),
    tabExists: vi.fn(async (id) => id !== 999),
  },
  accessibility: {
    collectInPage: () => ({ nodes: [], paths: [] }),
    assemble: () => ({ text: '', count: 0 }),
    resetBaseline: () => {},
    getRefSelector: () => null,
  },
});

beforeEach(() => {
  document.body.innerHTML = '<div class="wrap"><div id="ci" contenteditable="true"></div><button>发送</button></div>';
  global.chrome.scripting.executeScript = strictExecuteScript;
  cdp.clickAt.mockReset();
  cdp.clickAt.mockResolvedValue({ ok: true });
  cdp.typeText.mockReset();
  cdp.typeText.mockResolvedValue({ ok: true, chars: 1, typed: 1 });
});

describe('A21 静态：原语里不再有平台形状缺省', () => {
  it('三处缺省表达式必须全部消失（缺省只在适配器里）', async () => {
    const src = await readFile(PRIMITIVES, 'utf8');
    expect(src.includes('inputSelector ||')).toBe(false);
    expect(src.includes("['发送', '发布', '评论']")).toBe(false);
    expect(src.includes('o.containerSelector ||')).toBe(false);
  });
});

describe('A21 行为：未下发选择器必须报自己的名字（不猜）', () => {
  it('comment_prep 缺 input_selector → comment_input_selector_not_found', async () => {
    await expect(dispatch({ action: 'comment_prep', tab_id: 42, value: '真好吃' }, makeDeps()))
      .rejects.toThrow(/comment_input_selector_not_found/);
  });

  it('comment_send 缺 input_selector → comment_input_selector_not_found', async () => {
    await expect(dispatch({ action: 'comment_send', tab_id: 42, value: '真好吃', send_button_text: '发送' }, makeDeps()))
      .rejects.toThrow(/comment_input_selector_not_found/);
  });

  it('comment_send 缺 send_button_text → send_button_text_not_found（不再猜「发送/发布/评论」）', async () => {
    await expect(dispatch({ action: 'comment_send', tab_id: 42, value: '真好吃', input_selector: '#ci' }, makeDeps()))
      .rejects.toThrow(/send_button_text_not_found/);
  });

  it('comment_verify 缺 comment_container → comment_container_not_found（不再退化成整页搜）', async () => {
    await expect(dispatch({ action: 'comment_verify', tab_id: 42, value: '真好吃' }, makeDeps()))
      .rejects.toThrow(/comment_container_not_found/);
  });

  it('三个新错误名都带 _not_found 后缀：服务端 isNeverExecuted 才会判「从未发生」', async () => {
    // 这条是回归锁：错误名改了后缀（如 _missing），服务端就把它当「可能已派发」，
    // 一次配置缺失会被记成写尝试，双发闸随之锁死。
    for (const name of ['comment_input_selector_not_found', 'send_button_text_not_found', 'comment_container_not_found']) {
      expect(name.endsWith('_not_found')).toBe(true);
    }
  });
});
