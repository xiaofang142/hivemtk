// §8.2-1 尾项：comment_send 是三条 trusted 写通道里唯一没补「点后身份复核」的一条。
//
// 形状与完全同构，只是后果更重：click 点错了一个元素通常还能撤销，comment_send 点下去
// 是一次不可撤回的公开提交。探测与真点之间隔着拟人贝塞尔轨迹的飞行时间（可达数百 ms），
// 这期间浮层压上来 / 轮播挪位，真点就落在一个从未被探测过的元素上，而回包仍写 {sent:true}。
// click 与 click_near 各有一条复核，这里补第三处消费点。
//
// 复核要有对象：click 的 target 是调用方给的 selector，click_near 靠 probe 回传 pathOf(hit)。
// comment_send 的按钮是「输入框往上 4 层找文本」现场算出来的，调用方手里没有任何定位——
// 所以 injPostCommentSend 必须自己回传一条可再解析的路径，否则这条复核形同
// 拿空串 querySelector，静默 ok（本文件第 5 条行为腿专门钉这一格）。
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
import { strictExecuteScript } from './inject-sandbox.js';
import primitivesSrc from '../src/core/primitives.js?raw';

const pageScript = strictExecuteScript.getMockImplementation();

const fakeChrome = {
  scripting: { executeScript: vi.fn(pageScript) },
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

Object.defineProperty(global.HTMLElement.prototype, 'offsetParent', {
  get() { return this.parentElement ? document.body : null; },
  configurable: true,
});
Object.defineProperty(global.HTMLElement.prototype, 'innerText', {
  get() { return this.textContent; },
  configurable: true,
});

// 布局可控台（与 batch17-actionability.test.js 那份同款）：中心点默认 (150,220)，recheck 拿到的就是这个坐标被挪走/被接管
let reads = 0;
let boxFn = () => ({ left: 100, top: 200, width: 100, height: 40 });
global.HTMLElement.prototype.getBoundingClientRect = function () {
  reads += 1;
  const b = boxFn(reads);
  return { ...b, right: b.left + b.width, bottom: b.top + b.height, x: b.left, y: b.top, toJSON() {} };
};

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

// 身份复核注入的特征：第二个实参是坐标（probe 的第二参是 mode 字符串）
let identityArgs;

const sendStep = (extra = {}) => ({
  action: 'comment_send', tab_id: 42,
  input_selector: 'textarea', send_button_text: '发送',
  verify_identity: true, ...extra,
});
const fixture = () => {
  // 前置一个同级、同文案的干扰按钮：injPostCommentSend 是从输入框往上找按钮（命中的是
  // 容器内那个），而 document.querySelector('button') 拿到的是页面上第一个。
  // 少了这颗诱饵，「pathOf 退化成 'button'」这种错到不了任何断言手里（M3 实测）。
  document.body.innerHTML = '<button>发送</button><div><textarea></textarea><button>发送</button></div>';
  return document.querySelector('div > button');
};

beforeEach(() => {
  document.body.innerHTML = '';
  reads = 0;
  identityArgs = [];
  boxFn = () => ({ left: 100, top: 200, width: 100, height: 40 });
  document.elementFromPoint = () => null; // 默认无遮挡
  fakeChrome.scripting.executeScript.mockClear();
  fakeChrome.scripting.executeScript.mockImplementation(async (opts) => {
    if (Array.isArray(opts.args) && typeof opts.args[1] === 'number') identityArgs.push(opts.args);
    return pageScript(opts);
  });
  cdp.clickAt.mockReset();
  cdp.clickAt.mockResolvedValue({ ok: true });
  resetSnapshotBaseline();
});

describe('comment_send 的点后身份复核（第三个消费点）', () => {
  it('提交后目标消失（selector 解析不到）→ element_moved，且**绝不再点第二次**', async () => {
    const btn = fixture();
    cdp.clickAt.mockImplementation(async () => {
      btn.remove(); // 真点之后页面把按钮换掉了
      return { ok: true };
    });
    await expect(dispatch(sendStep(), makeDeps())).rejects.toThrow(/element_moved/);
    expect(cdp.clickAt).toHaveBeenCalledTimes(1);
  });

  it('提交后中心点挪出容差 → element_moved（不许"大概没动"就放行）', async () => {
    fixture();
    cdp.clickAt.mockImplementation(async () => {
      boxFn = () => ({ left: 180, top: 200, width: 100, height: 40 }); // 中心 x 150 → 230
      return { ok: true };
    });
    await expect(dispatch(sendStep(), makeDeps())).rejects.toThrow(/element_moved/);
    expect(cdp.clickAt).toHaveBeenCalledTimes(1);
  });

  it('落点被浮层接管 → element_moved：这条才是本批的立项形状（点下去的是浮层，不是发送按钮）', async () => {
    document.body.innerHTML = '<div><textarea></textarea><button>发送</button><div id="overlay"></div></div>';
    const btn = document.querySelector('button');
    const overlay = document.querySelector('#overlay');
    document.elementFromPoint = () => btn; // 探测时按钮就是自己的 hit-target
    cdp.clickAt.mockImplementation(async () => {
      document.elementFromPoint = () => overlay; // 飞行途中浮层盖上来
      return { ok: true };
    });
    await expect(dispatch(sendStep(), makeDeps())).rejects.toThrow(/element_moved/);
    expect(cdp.clickAt).toHaveBeenCalledTimes(1);
  });

  it('身份未变 → sent=true，且回包明说这次复核过（identity_checked=true）', async () => {
    fixture();
    const r = await dispatch(sendStep(), makeDeps());
    expect(r.sent).toBe(true);
    expect(r.identity_checked).toBe(true);
  });

  it('复核的再解析对象 = probe 回传的那条路径，且它确实指回被点的按钮', async () => {
    // 这条锁的是「复核有没有对象」。injPostCommentSend 不回 selector 时，
    // dispatch 拿 undefined 去 querySelector → 页面侧抛 SyntaxError → 归因成
    // identity_recheck_failed（未知态）；回了错的（如 'button' 命中同级另一个节点）
    // → 判成 element_moved。两种都不是这里想要的「指回同一个节点」。
    const btn = fixture();
    await dispatch(sendStep(), makeDeps());
    expect(identityArgs).toHaveLength(1);
    const [sel, x, y, tol] = identityArgs[0];
    expect(typeof sel, '复核必须拿到一条 CSS 路径').toBe('string');
    expect(sel.length, '路径不许是空串').toBeGreaterThan(0);
    expect(document.querySelector(sel), `路径 ${sel} 没有解析回被点的按钮`).toBe(btn);
    expect({ x, y, tol }).toEqual({ x: 150, y: 220, tol: 5 });
  });

  it('旧服务端（不带 verify_identity）不付这次额外注入，也不冒充复核过', async () => {
    fixture();
    const r = await dispatch(sendStep({ verify_identity: undefined }), makeDeps());
    expect(r.sent).toBe(true);
    expect(r.identity_checked).toBeUndefined();
    expect(identityArgs).toHaveLength(0);
  });

  it('复核自己跑不动（注入没回包）不能变成静默 ok：未知态点名 identity', async () => {
    fixture();
    fakeChrome.scripting.executeScript.mockImplementation(async (opts) => {
      if (Array.isArray(opts.args) && typeof opts.args[1] === 'number') {
        identityArgs.push(opts.args);
        return [{ result: undefined }];
      }
      return pageScript(opts);
    });
    await expect(dispatch(sendStep(), makeDeps())).rejects.toThrow(/identity/);
    expect(identityArgs.length).toBeGreaterThan(0);
    expect(cdp.clickAt).toHaveBeenCalledTimes(1);
  });

  it('复核必须落在 try 之外：抛出去就是抛出去，不许被兜底通道再提交一次', async () => {
    // click 的教训写在这：comment_send 现在确实没有 DOM 兜底分支，
    // 一旦有人"顺手"加一条（CDP 失败 → el.click()），这条腿立刻红——
    // 复核失败会被 catch 成「CDP 不可用」，在已经发生的提交之上再点一次。
    const btn = fixture();
    let domClicks = 0;
    btn.addEventListener('click', () => { domClicks += 1; });
    cdp.clickAt.mockImplementation(async () => {
      boxFn = () => ({ left: 180, top: 200, width: 100, height: 40 });
      return { ok: true };
    });
    await expect(dispatch(sendStep(), makeDeps())).rejects.toThrow(/element_moved/);
    expect(domClicks).toBe(0);
  });
});

describe('静态锁：多份内联的定位路径与复核挂点必须一起长牙', () => {
  it('pathOf 恰有两份内联、两处回传 selector（注入函数自包含 ⇒ 不能抽公共函数）', () => {
    const defs = (primitivesSrc.match(/const pathOf = /g) || []).length;
    const uses = (primitivesSrc.match(/selector: pathOf\(/g) || []).length;
    expect(defs, `pathOf 定义 ${defs} 处 want 2（injClickNear / injPostCommentSend）`).toBe(2);
    expect(uses, `selector 回传 ${uses} 处 want 2`).toBe(2);
  });

  it('injClickIdentityCheck 的三处消费里，comment_send 那一句用的是 btn.selector（不是别的变量的）', () => {
    const i = primitivesSrc.indexOf('await cdpInput.clickAt(tabId, btn.x, btn.y');
    expect(i, '找不到 comment_send 的点击语句（改名要同时改这条锁）').toBeGreaterThan(-1);
    const tail = primitivesSrc.slice(i, i + 600);
    expect(tail, '点完之后没有复核语句 = 本批白做').toContain('injClickIdentityCheck');
    expect(tail).toContain('btn.selector');
  });
});
