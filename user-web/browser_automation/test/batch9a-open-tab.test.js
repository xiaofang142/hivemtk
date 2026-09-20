// 批9a 假绿收口：open_tab 必须等到页面这一帧真能读才回包；读不到内容要判红而不是回空 markdown。
// 立项依据是真机 session=432 的实测：open_tab（chrome.tabs.create 立即返回）→ markdown
// 读到空气泡 DOM 回 "# \n"，三步全绿、整轮 completed —— 编排方拿到的是「成功但零内容」。
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { dispatch } from '../src/core/primitives.js';
import { createTabManager } from '../src/core/tab-manager.js';
import { strictExecuteScript } from './inject-sandbox.js';

const fakeChrome = {
  scripting: {
    executeScript: strictExecuteScript,
  },
  tabs: {
    create: vi.fn(async (opts) => ({ id: 42, ...opts })),
    get: vi.fn(async (id) => ({ id, status: 'complete', title: '大页夹具' })),
    remove: vi.fn(async () => {}),
    update: vi.fn(async () => {}),
    captureVisibleTab: vi.fn(async () => 'data:image/png;base64,AAAA'),
  },
  windows: { update: vi.fn(async () => {}) },
};
global.chrome = fakeChrome;

const makeDeps = (overrides = {}) => ({
  tabManager: {
    openTab: vi.fn(async (url) => ({ id: 42, url, title: '' })),
    waitForLoad: vi.fn(async () => ({ loaded: true, title: '大页夹具', wait_ms: 40 })),
    closeTab: vi.fn(async () => {}),
    activateTab: vi.fn(async () => {}),
    tabExists: vi.fn(async (id) => id !== 999),
    ...overrides,
  },
  accessibility: {
    collectInPage: () => ({ nodes: [], paths: [] }),
    assemble: () => ({ snapshot: 'button "x" @e1' }),
    getRefSelector: () => null,
  },
});

beforeEach(() => {
  document.body.innerHTML = '';
  document.title = '';
  fakeChrome.scripting.executeScript.mockClear();
  fakeChrome.tabs.get.mockClear();
});

// jsdom 无布局引擎，innerText 恒空 → 用 textContent 路径，需要 stub 掉可见性无关项
Object.defineProperty(global.HTMLElement.prototype, 'offsetParent', {
  get() { return this.parentElement ? document.body : null; },
  configurable: true,
});

describe('open_tab 等加载（假绿收口）', () => {
  it('回包带 loaded/load_wait_ms，且等待发生在回包之前', async () => {
    const deps = makeDeps();
    const data = await dispatch({ action: 'open_tab', url: 'https://example.com' }, deps);
    expect(deps.tabManager.waitForLoad).toHaveBeenCalledWith(42, undefined);
    expect(data).toMatchObject({ chrome_tab_id: 42, loaded: true, title: '大页夹具' });
    expect(typeof data.load_wait_ms).toBe('number');
  });

  it('等不到 complete 时如实回 loaded:false（不把没加载伪装成可读）', async () => {
    const deps = makeDeps({ waitForLoad: vi.fn(async () => ({ loaded: false, title: '', wait_ms: 10000 })) });
    const data = await dispatch({ action: 'open_tab', url: 'https://example.com' }, deps);
    expect(data.loaded).toBe(false);
  });

  it('load_timeout_ms 透传给等待方（编排可放宽重页预算）', async () => {
    const deps = makeDeps();
    await dispatch({ action: 'open_tab', url: 'https://example.com', load_timeout_ms: 1234 }, deps);
    expect(deps.tabManager.waitForLoad).toHaveBeenCalledWith(42, 1234);
  });

  it('真实 waitForLoad：loading→complete 轮询到完成并带回最终标题', async () => {
    const get = vi.fn()
      .mockResolvedValueOnce({ id: 7, status: 'loading', title: '' })
      .mockResolvedValueOnce({ id: 7, status: 'loading', title: '' })
      .mockResolvedValue({ id: 7, status: 'complete', title: '好了' });
    const tm = createTabManager({ tabs: { get } });
    const r = await tm.waitForLoad(7, 500);
    expect(r).toMatchObject({ loaded: true, title: '好了' });
    // 必须真的轮过：首帧 status 还是 loading 就回 loaded:true 等于把本批的修复原地取消
    expect(get.mock.calls.length).toBeGreaterThanOrEqual(3);
  });

  it('真实 waitForLoad：预算内等不到 complete → 返回 loaded:false 且不抛（永不加载完的重页是真实现实）', async () => {
    const tm = createTabManager({ tabs: { get: vi.fn(async () => ({ id: 7, status: 'loading', title: '半页' })) } });
    const t0 = Date.now();
    const r = await tm.waitForLoad(7, 600);
    expect(r.loaded).toBe(false);
    expect(r.title).toBe('半页');
    expect(Date.now() - t0).toBeLessThan(3000);
  });

  it('真实 waitForLoad：tab 中途被关闭 → loaded:false，报错交给后续步骤的 tab_not_found', async () => {
    const tm = createTabManager({ tabs: { get: vi.fn(async () => { throw new Error('No tab with id: 7'); }) } });
    const r = await tm.waitForLoad(7, 500);
    expect(r.loaded).toBe(false);
  });

  it('预算被夹在 [500,30000]：编排给 999999 也不能吃掉命令超时', async () => {
    let now = Date.now();
    const realNow = Date.now;
    Date.now = () => now;
    try {
      const tm = createTabManager({ tabs: { get: vi.fn(async () => ({ id: 7, status: 'loading' })) } });
      const p = tm.waitForLoad(7, 999999);
      now = realNow() + 30001; // 时间一次跳满：只允许按 30s 上限判定
      const r = await p;
      expect(r.loaded).toBe(false);
      expect(r.wait_ms).toBeLessThanOrEqual(30000);
    } finally {
      Date.now = realNow;
    }
  });
});

describe('markdown 读数如实（假绿收口）', () => {
  it('零可读内容 → empty_document 判红（真机 432 的那个形状：回 "# \\n" 却整轮绿）', async () => {
    const deps = makeDeps();
    await expect(dispatch({ action: 'markdown', tab_id: 42 }, deps))
      .rejects.toThrow(/empty_document/);
  });

  it('反向对照一：正文全在 div 里但有标题 → 不判红，只标 content_empty（既有抽取边界，不误伤）', async () => {
    document.title = '某页';
    document.body.innerHTML = '<div>真实正文，只是没有 p/h1/li</div>';
    const data = await dispatch({ action: 'markdown', tab_id: 42 }, makeDeps());
    expect(data.markdown).toContain('某页');
    expect(data.content_empty).toBe(true);
    expect(data.truncated).toBe(false);
  });

  it('反向对照二：无标题但有正文行 → 不判红', async () => {
    document.body.innerHTML = '<p>一句正文</p>';
    const data = await dispatch({ action: 'markdown', tab_id: 42 }, makeDeps());
    expect(data.markdown).toContain('一句正文');
    expect(data.content_empty).toBe(false);
  });

  it('超 64KiB 时如实标截断（只回截断长度=把 64KiB 当成整页）', async () => {
    const para = '这一段足够长以便把正文堆过六十四千字节上限'.repeat(60);
    document.body.innerHTML = `<h1>标题</h1>${('<p>' + para + '</p>').repeat(60)}`;
    const deps = makeDeps();
    const data = await dispatch({ action: 'markdown', tab_id: 42 }, deps);
    expect(data.markdown_chars).toBe(data.markdown.length);
    expect(data.full_chars).toBeGreaterThan(data.markdown_chars);
    expect(data.truncated).toBe(true);
  });

  it('未截断时 full_chars === markdown_chars 且 truncated:false', async () => {
    document.body.innerHTML = '<h1>短页</h1><p>一句话</p>';
    const deps = makeDeps();
    const data = await dispatch({ action: 'markdown', tab_id: 42 }, deps);
    expect(data.truncated).toBe(false);
    expect(data.full_chars).toBe(data.markdown_chars);
  });
});
