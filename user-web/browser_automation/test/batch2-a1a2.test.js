// 批2 A1/A2 扩展侧契约测试：
//  A2 快照携带 location.href（拦截判据 URL 层的数据源）；
//  A1 resolve_ref 原语 + comment_prep 的 @eN input_selector 解析（自愈回路重下发通道）。
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { dispatch } from '../src/core/primitives.js';
import { assembleSnapshot, getRefSelector } from '../src/core/accessibility.js';

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

beforeEach(() => {
  document.body.innerHTML = '';
  fakeChrome.scripting.executeScript.mockClear();
});

Object.defineProperty(global.HTMLElement.prototype, 'offsetParent', {
  get() { return this.parentElement ? document.body : null; },
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
    collectInPage: () => ({ nodes: [], paths: [], url: location.href }),
    assemble: (collected, tabKey) => {
      const { text, new_count, url } = assembleSnapshot(collected, tabKey);
      return { snapshot: text, new_count, url };
    },
    resetBaseline: () => {},
    getRefSelector: (ref) => (ref === '@e1' ? 'textarea.content-textarea' : null),
  },
});

describe('A2 快照携带页面 URL', () => {
  it('assembleSnapshot 透传 collected.url', () => {
    const snap = assembleSnapshot({ nodes: [{ role: 'button', name: '发送' }], paths: ['button.send'], url: 'https://www.xiaohongshu.com/website-login/error?error_code=300012' }, 't-a2');
    expect(snap.url).toBe('https://www.xiaohongshu.com/website-login/error?error_code=300012');
  });
  it('旧形态 collected 无 url 字段时降级空串不抛错', () => {
    const snap = assembleSnapshot({ nodes: [], paths: [] }, 't-a2b');
    expect(snap.url).toBe('');
  });
  it('dispatch snapshot 回包含 url（页面 location 经 collect 传入）', async () => {
    document.body.innerHTML = '<button>ok</button>';
    const data = await dispatch({ action: 'snapshot', tab_id: 42 }, makeDeps());
    expect(typeof data.url).toBe('string');
  });
});

describe('A1 resolve_ref 原语与 comment 选择器 ref 解析', () => {
  it('resolve_ref：@eN 换回 CSS；失效/非 ref 输入分别得空串/原样透传', async () => {
    const deps = makeDeps();
    expect(await dispatch({ action: 'resolve_ref', tab_id: 42, ref: '@e1' }, deps)).toEqual({ selector: 'textarea.content-textarea' });
    expect(await dispatch({ action: 'resolve_ref', tab_id: 42, ref: '@e99' }, deps)).toEqual({ selector: '' });
    expect(await dispatch({ action: 'resolve_ref', tab_id: 42, ref: '.plain' }, deps)).toEqual({ selector: '.plain' });
  });

  it('comment_prep 的 input_selector 允许 @eN（自愈重下发通道）：注入实参已是解析后 CSS', async () => {
    document.body.innerHTML = '<textarea class="content-textarea"></textarea>';
    await dispatch({ action: 'comment_prep', tab_id: 42, value: '真好吃', input_selector: '@e1' }, makeDeps());
    const injected = fakeChrome.scripting.executeScript.mock.calls
      .flatMap((c) => (c[0].args || []).filter((a) => typeof a === 'string'));
    expect(injected).toContain('textarea.content-textarea');
    expect(injected.every((a) => a !== '@e1')).toBe(true);
  });

  it('ref 已失效（resolve 空）时回退原串——querySelectorAll 语法错误由 Go 侧 fail-closed 归因，不在扩展静默吞', async () => {
    document.body.innerHTML = '<textarea class="content-textarea"></textarea>';
    await expect(
      dispatch({ action: 'comment_prep', tab_id: 42, value: 'x', input_selector: '@e404' }, makeDeps()),
    ).rejects.toThrow();
  });
});
