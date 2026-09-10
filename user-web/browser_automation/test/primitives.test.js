import { describe, it, expect, vi, beforeEach } from 'vitest';
import { describe, it, expect, vi } from 'vitest';
import { dispatch } from '../src/core/primitives.js';

// mock chrome.scripting / chrome.tabs
const fakeChrome = {
  scripting: {
    executeScript: vi.fn(async ({ func, args }) => {
      // 直接以 document 为 this 执行注入函数（jsdom 环境）
      return [{ result: func(...(args || [])) }];
    }),
  },
  tabs: {
    create: vi.fn(async (opts) => ({ id: 42, ...opts })),
    get: vi.fn(async (id) => {
      if (id === 999) throw new Error('No tab with id: 999');
      return { id };
    }),
    remove: vi.fn(async () => {}),
    update: vi.fn(async () => {}),
    captureVisibleTab: vi.fn(async () => 'data:image/png;base64,AAAA'),
  },
  windows: { update: vi.fn(async () => {}) },
};
global.chrome = fakeChrome;

beforeEach(() => {
  document.body.innerHTML = '';
});

const makeDeps = () => ({
  tabManager: {
    openTab: vi.fn(async (url) => ({ id: 42, url })),
    closeTab: vi.fn(async () => {}),
    activateTab: vi.fn(async () => {}),
    tabExists: vi.fn(async (id) => id !== 999),
  },
  accessibility: {
    collectInPage: () => ({ nodes: [], paths: [] }),
    assemble: () => ({ snapshot: 'button "x" @e1' }),
    getRefSelector: (ref) => (ref === '@e1' ? '#real-btn' : null),
  },
});

describe('primitives dispatch', () => {
  it('open_tab 返回 chrome_tab_id', async () => {
    const deps = makeDeps();
    const data = await dispatch({ action: 'open_tab', url: 'https://example.com', active: false }, deps);
    expect(data.chrome_tab_id).toBe(42);
    expect(deps.tabManager.openTab).toHaveBeenCalledWith('https://example.com', false);
  });

  it('@eN refs 解析为真实 selector 再执行 click', async () => {
    document.body.innerHTML = '<button id="real-btn">按钮</button>';
    const deps = makeDeps();
    await dispatch({ action: 'click', tab_id: 42, target: '@e1' }, deps);
    const call = fakeChrome.scripting.executeScript.mock.calls.at(-1)[0];
    expect(call.args).toEqual(['#real-btn']);
  });

  it('未知 refs 透传原始字符串', async () => {
    document.body.innerHTML = '<button id="plain">按钮</button>';
    const deps = makeDeps();
    await dispatch({ action: 'click', tab_id: 42, target: '#plain' }, deps);
    const call = fakeChrome.scripting.executeScript.mock.calls.at(-1)[0];
    expect(call.args).toEqual(['#plain']);
  });

  it('tab 不存在抛错', async () => {
    const deps = makeDeps();
    await expect(dispatch({ action: 'click', tab_id: 999, target: '#x' }, deps))
      .rejects.toThrow('tab_not_found');
  });

  it('close_tab 调用 tabManager', async () => {
    const deps = makeDeps();
    await dispatch({ action: 'close_tab', tab_id: 42 }, deps);
    expect(deps.tabManager.closeTab).toHaveBeenCalledWith(42);
  });

  it('wait 受 60s 上限钳制（不等真实时长）', async () => {
    const deps = makeDeps();
    // ms 超上限会被钳到 60000 —— 此处只验证不抛错并正常返回
    await expect(dispatch({ action: 'wait', ms: 0 }, deps)).resolves.toEqual({});
  });

  it('未知 action 报错', async () => {
    const deps = makeDeps();
    await expect(dispatch({ action: 'fly', tab_id: 42 }, deps)).rejects.toThrow('unknown_action');
  });

  it('extract 多元素提取：返回数组且过滤空文本（jsdom 无 innerText 时回退 href）', async () => {
    document.body.innerHTML = `
      <a class="cover" href="/a">笔记一</a>
      <a class="cover" href="/b">笔记二</a>
      <a class="cover" href=""></a>
    `;
    const deps = makeDeps();
    const r = await dispatch({ action: 'extract', tab_id: 42, selectors: { notes: 'a.cover' } }, deps);
    expect(r.data.notes).toEqual(['/a', '/b']);
  });

  it('extract 无匹配返回空数组', async () => {
    const deps = makeDeps();
    const r = await dispatch({ action: 'extract', tab_id: 42, selectors: { none: '.missing' } }, deps);
    expect(r.data.none).toEqual([]);
  });
});
