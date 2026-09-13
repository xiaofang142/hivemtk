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
  delete fakeChrome.debugger;
  fakeChrome.scripting.executeScript.mockClear();
});

// jsdom 无布局引擎：offsetParent 恒 null → 可见性过滤永远命不中。
// 标准对策：stub offsetParent（有父节点即可见）。
Object.defineProperty(global.HTMLElement.prototype, 'offsetParent', {
  get() { return this.parentElement ? document.body : null; },
  configurable: true,
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

  it('@eN refs 解析为真实 selector 再执行 click（probe→trusted→兜底链路里选择器已归一）', async () => {
    document.body.innerHTML = '<button id="real-btn">按钮</button>';
    const deps = makeDeps();
    // jsdom 无 chrome.debugger → trusted probe 因 zero_box/CDP 失败走 DOM 兜底；
    // 断言所有注入调用中 @e1 已被解析成 #real-btn（不再出现原始 ref 字符串）
    const data = await dispatch({ action: 'click', tab_id: 42, target: '@e1' }, deps);
    const allArgs = fakeChrome.scripting.executeScript.mock.calls.map((c) => c[0].args[0]);
    expect(allArgs).toContain('#real-btn');
    expect(allArgs.every((a) => a !== '@e1')).toBe(true);
    expect(data.channel).toBe('dom_fallback'); // 无调试器环境必须显式标兜底
  });

  it('未知 refs 透传原始字符串', async () => {
    document.body.innerHTML = '<button id="plain">按钮</button>';
    const deps = makeDeps();
    const data = await dispatch({ action: 'click', tab_id: 42, target: '#plain' }, deps);
    const lastCall = fakeChrome.scripting.executeScript.mock.calls.at(-1)[0];
    expect(lastCall.args[0]).toEqual('#plain');
    expect(data.channel).toBe('dom_fallback');
  });

  it('R25-Q1 回归：trusted 点击 A[_blank] 不得再接管 location.href（禁双跳）', async () => {
    document.body.innerHTML = '<a id="ext" href="https://other.example/x" target="_blank">去</a>';
    const el = document.getElementById('ext');
    el.getBoundingClientRect = () => ({ left: 50, top: 50, width: 40, height: 20, right: 90, bottom: 70, x: 50, y: 50 });
    fakeChrome.debugger = {
      attach: vi.fn(async () => {}), detach: vi.fn(async () => {}),
      sendCommand: vi.fn(async () => ({})), onDetach: { addListener: vi.fn() },
    };
    const deps = makeDeps();
    const data = await dispatch({ action: 'click', tab_id: 42, target: '#ext' }, deps);
    expect(data.channel).toBe('cdp');
    // 关键断言：trusted 路径当前 tab 的 location 不应被接管跳走（jsdom 里 location.href 保持原值）
    expect(window.location.href).not.toContain('other.example');
  });

  it('chrome.debugger 可用时 click 走 trusted 通道（mock CDP 事件序列断言）', async () => {
    document.body.innerHTML = '<button id="tbtn">按钮</button>';
    const el = document.getElementById('tbtn');
    // 伪造几何：让 probe 过 actionability（jsdom 无 elementFromPoint=自动跳过遮挡项）
    el.getBoundingClientRect = () => ({ left: 100, top: 100, width: 40, height: 20, right: 140, bottom: 120, x: 100, y: 100 });
    const cmds = [];
    fakeChrome.debugger = {
      attach: vi.fn(async () => {}),
      detach: vi.fn(async () => {}),
      sendCommand: vi.fn(async (target, method, params) => { cmds.push({ method, params }); return {}; }),
      onDetach: { addListener: vi.fn() },
    };
    const deps = makeDeps();
    const data = await dispatch({ action: 'click', tab_id: 42, target: '#tbtn' }, deps);
    expect(data.channel).toBe('cdp');
    const moves = cmds.filter((c) => c.method === 'Input.dispatchMouseEvent' && c.params.type === 'mouseMoved');
    expect(moves.length).toBeGreaterThanOrEqual(10); // F3：步数随距离夹 10–40（此处起点随机）
    const down = cmds.find((c) => c.params.type === 'mousePressed');
    const up = cmds.find((c) => c.params.type === 'mouseReleased');
    expect(down.params.buttons).toBe(1);
    expect(down.params.clickCount).toBe(1);
    expect(up.params.buttons).toBe(0);
    expect(up.params.clickCount).toBe(1);
    delete fakeChrome.debugger;
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

  // ---- F2② 三段式发评论：prep / send / verify 无状态拆分，提交与验证分离 ----
  it('comment_prep 命中 textarea：合成注入文字，needs_trusted=false，不触发 CDP', async () => {
    document.body.innerHTML = '<textarea class="content-textarea"></textarea>';
    const deps = makeDeps();
    const data = await dispatch({
      action: 'comment_prep', tab_id: 42, value: '真好吃', input_selector: 'textarea.content-textarea',
    }, deps);
    expect(data.ok).toBe(true);
    expect(data.input_found).toBe(true);
    expect(data.needs_trusted).toBe(false);
    expect(document.querySelector('textarea.content-textarea').value).toBe('真好吃');
    expect(fakeChrome.debugger).toBeUndefined();
  });

  it('comment_prep 无输入框：抛 comment_input_not_found', async () => {
    document.body.innerHTML = '<div>no input here</div>';
    const deps = makeDeps();
    await expect(dispatch({
      action: 'comment_prep', tab_id: 42, value: 'x', input_selector: 'textarea.missing',
    }, deps)).rejects.toThrow('comment_input_not_found');
  });

  it('comment_send 定位发送按钮 + trusted 坐标点击，回包仅 {sent}（不掺验证）', async () => {
    document.body.innerHTML = `
      <div class="box"><textarea class="content-textarea"></textarea><button class="send">发送</button></div>`;
    const btn = document.querySelector('button.send');
    Object.defineProperty(btn, 'innerText', { value: '发送', configurable: true }); // jsdom 无 innerText
    btn.getBoundingClientRect = () => ({ left: 200, top: 200, width: 60, height: 30, right: 260, bottom: 230, x: 200, y: 200 });
    fakeChrome.debugger = {
      attach: vi.fn(async () => {}), detach: vi.fn(async () => {}),
      sendCommand: vi.fn(async () => ({})), onDetach: { addListener: vi.fn() },
    };
    const deps = makeDeps();
    const data = await dispatch({
      action: 'comment_send', tab_id: 42, input_selector: 'textarea.content-textarea', send_button_text: '发送',
    }, deps);
    expect(data.ok).toBe(true);
    expect(data.sent).toBe(true);
    expect(data.verified).toBeUndefined(); // 提交点绝不掺验证（验证归 Go finalize）
    const pressed = fakeChrome.debugger.sendCommand.mock.calls.filter(
      (c) => c[1] === 'Input.dispatchMouseEvent' && c[2].type === 'mousePressed');
    expect(pressed.length).toBe(1); // 只点一次（单次提交红线）
    delete fakeChrome.debugger;
  });

  it('comment_send 找不到发送按钮：抛 send_button_not_found（未提交，可安全终止）', async () => {
    document.body.innerHTML = '<textarea class="content-textarea"></textarea>';
    fakeChrome.debugger = {
      attach: vi.fn(async () => {}), detach: vi.fn(async () => {}),
      sendCommand: vi.fn(async () => ({})), onDetach: { addListener: vi.fn() },
    };
    const deps = makeDeps();
    await expect(dispatch({
      action: 'comment_send', tab_id: 42, input_selector: 'textarea.content-textarea', send_button_text: '发送',
    }, deps)).rejects.toThrow('send_button_not_found');
    delete fakeChrome.debugger;
  });

  it('comment_verify 评论已渲染：verified=true + evidence（容器数/条目文本）', async () => {
    document.body.innerHTML = '<div class="comments-container"><div class="note-text">真好吃</div></div>';
    const deps = makeDeps();
    const data = await dispatch({
      action: 'comment_verify', tab_id: 42, value: '真好吃',
      comment_container: '.comments-container', comment_item_text: '.note-text', timeout_ms: 500,
    }, deps);
    expect(data.ok).toBe(true);
    expect(data.verified).toBe(true);
    expect(data.evidence.containers).toBe(1);
    expect(data.evidence.item_text).toContain('真好吃');
  });

  it('comment_verify 评论未渲染：超时 verified=false reason=comment_not_rendered（只读可安全轮询）', async () => {
    document.body.innerHTML = '<div class="comments-container"><div>还没出现</div></div>';
    const deps = makeDeps();
    const data = await dispatch({
      action: 'comment_verify', tab_id: 42, value: '真好吃',
      comment_container: '.comments-container', timeout_ms: 500,
    }, deps);
    expect(data.verified).toBe(false);
    expect(data.posted).toBe(false);
    expect(data.reason).toBe('comment_not_rendered');
  });

  it('一站式 post_comment 已从扩展协议移除（单一路径防分叉）', async () => {
    const deps = makeDeps();
    await expect(dispatch({ action: 'post_comment', tab_id: 42, value: 'x' }, deps))
      .rejects.toThrow('unknown_action');
  });

  it('R26-2 注入竞速：executeScript 挂起时 comment_prep 按 inject_timeout_ms 早返明确错误', async () => {
    // 模拟重页注入队列拥堵：executeScript 永不 resolve
    fakeChrome.scripting.executeScript.mockImplementationOnce(() => new Promise(() => {}));
    const deps = makeDeps();
    await expect(dispatch({
      action: 'comment_prep', tab_id: 42, value: 'x',
      input_selector: 'textarea.content-textarea', inject_timeout_ms: 200,
    }, deps)).rejects.toThrow('comment_prep_inject_timeout_200ms');
  });

  it('R26-2 注入竞速：comment_send 同样有 deadline（未执行=点击从未发生，可安全重下发）', async () => {
    fakeChrome.scripting.executeScript.mockImplementationOnce(() => new Promise(() => {}));
    const deps = makeDeps();
    await expect(dispatch({
      action: 'comment_send', tab_id: 42, inject_timeout_ms: 200,
    }, deps)).rejects.toThrow('comment_send_inject_timeout_200ms');
  });
});
