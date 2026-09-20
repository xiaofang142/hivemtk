// 批14 comment_send 可点性闸门对齐：写操作的「点击从未发生」必须可判。
// 缺口：injPostCommentSend 选发送按钮只查 display/visibility，不查 zero_box / disabled /
// 浮层遮挡——而一次普通 click 的 probe 三项都查。comment_send 是全链路唯一不可逆点，
// 闸门反而更松，等于把「往浮层上落了一次提交」记成提交点已跨越（台账 sent → 回查 →
// unattributed），既污染证据又禁掉了本该安全的重下发。
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
import { strictExecuteScript } from './inject-sandbox.js';

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

// jsdom 无布局引擎：不造假几何就永远是 zero_box，测不到被测分支（同批14 click 夹具口径）
Object.defineProperty(global.HTMLElement.prototype, 'offsetParent', {
  get() { return this.parentElement ? document.body : null; },
  configurable: true,
});
const BOX = { x: 0, y: 0, left: 0, top: 0, right: 100, bottom: 40, width: 100, height: 40, toJSON() {} };
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
    tabExists: vi.fn(async (id) => id !== 999),
  },
  accessibility: {
    collectInPage: () => ({ nodes: [], paths: [] }),
    assemble: () => ({ text: '', count: 0 }),
    resetBaseline: () => {},
    getRefSelector: () => null,
  },
});

const PAGE = '<div class="wrap"><textarea id="ci" placeholder="写评论"></textarea><button id="send">发送</button></div>';

beforeEach(() => {
  document.body.innerHTML = '';
  document.elementFromPoint = () => null; // 默认无遮挡（jsdom 自身返回 null 也同义）
  global.HTMLElement.prototype.getBoundingClientRect = () => BOX;
  cdp.clickAt.mockReset();
  cdp.clickAt.mockResolvedValue({ ok: true });
});

describe('comment_send 发送按钮闸门（批14）', () => {
  it('正向对照：可点按钮仍恰好一次坐标点击（闸门不得过修正成永不提交）', async () => {
    document.body.innerHTML = PAGE;
    const r = await dispatch({ action: 'comment_send', tab_id: 42, input_selector: '#ci' }, makeDeps());
    expect(r).toEqual({ ok: true, sent: true });
    expect(cdp.clickAt).toHaveBeenCalledTimes(1);
  });

  it('disabled 发送按钮：判「点击从未发生」，零坐标下发', async () => {
    document.body.innerHTML = PAGE;
    document.querySelector('#send').disabled = true;
    await expect(dispatch({ action: 'comment_send', tab_id: 42, input_selector: '#ci' }, makeDeps()))
      .rejects.toThrow(/send_button_not_interactable: disabled/);
    expect(cdp.clickAt).toHaveBeenCalledTimes(0);
  });

  it('aria-disabled 同 disabled（组件库常用 aria 而非原生 disabled）', async () => {
    document.body.innerHTML = PAGE;
    document.querySelector('#send').setAttribute('aria-disabled', 'true');
    await expect(dispatch({ action: 'comment_send', tab_id: 42, input_selector: '#ci' }, makeDeps()))
      .rejects.toThrow(/send_button_not_interactable: disabled/);
    expect(cdp.clickAt).toHaveBeenCalledTimes(0);
  });

  it('被浮层遮挡：上抛 covered，不得把这一次提交落在浮层上', async () => {
    document.body.innerHTML = PAGE + '<div id="mask"></div>';
    document.elementFromPoint = () => document.querySelector('#mask');
    await expect(dispatch({ action: 'comment_send', tab_id: 42, input_selector: '#ci' }, makeDeps()))
      .rejects.toThrow(/send_button_not_interactable: covered/);
    expect(cdp.clickAt).toHaveBeenCalledTimes(0);
  });

  it('零尺寸按钮（折叠态/未渲染完）：zero_box 而不是往 (0,0) 提交', async () => {
    document.body.innerHTML = PAGE;
    global.HTMLElement.prototype.getBoundingClientRect = function () {
      return this.id === 'send'
        ? { x: 0, y: 0, left: 0, top: 0, right: 0, bottom: 0, width: 0, height: 0, toJSON() {} }
        : BOX;
    };
    await expect(dispatch({ action: 'comment_send', tab_id: 42, input_selector: '#ci' }, makeDeps()))
      .rejects.toThrow(/send_button_not_interactable: zero_box/);
    expect(cdp.clickAt).toHaveBeenCalledTimes(0);
  });

  it('遮挡只在滚入视口后才解开：复检通过即正常提交（闸门不许误杀）', async () => {
    document.body.innerHTML = PAGE + '<div id="mask"></div>';
    const btn = document.querySelector('#send');
    const mask = document.querySelector('#mask');
    let scrolled = false;
    btn.scrollIntoView = () => { scrolled = true; };
    document.elementFromPoint = () => (scrolled ? btn : mask);
    const r = await dispatch({ action: 'comment_send', tab_id: 42, input_selector: '#ci' }, makeDeps());
    expect(r.sent).toBe(true);
    expect(scrolled).toBe(true);
    expect(cdp.clickAt).toHaveBeenCalledTimes(1);
  });
});
