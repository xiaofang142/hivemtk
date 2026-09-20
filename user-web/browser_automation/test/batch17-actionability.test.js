// 批17 §8.2-1：写步点击的两半 —— 探测期内 box 必须**结算**（stable），
// 以及 CDP 真点之后必须**再认一次身份**（仅写步）。
//
// 动机（读码 + §8.2-1 同行口径）：probe 与 clickAt 之间隔着拟人贝塞尔轨迹的飞行时间（可达数百 ms），
// 这期间轮播/懒加载/toast 挪动页面，就会点到一个从未被探测过的元素，而回包仍是 {ok:true, channel:'cdp'}。
// 批14 只挡住了「probe 失败还被兜底绕过」和「兜底双发」，没挡住「probe 当时是真的、飞行途中变了」。
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

// 注入边界本身（沙箱的严格版 executeScript）。注意不能用 strictExecuteScript 再套一层
// mockImplementation：它自己就是个 vi.fn，把自己设成自己的实现＝无限递归（首轮 16 条里
// 有 9 条就是这么红的，红因与闸门毫无关系）。
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

// 布局可控台：getBoundingClientRect 由本文件供给，于是「元素在飞」是可以直接编排的输入，
// 而不是靠真动画碰运气。每次读取都问一次 boxAt(第几次读)，读几次由生产代码自己决定
// ——stable 循环少跑一轮，这里就会如实反映成「没等到结算就点了」。
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

const staticBox = () => { boxFn = () => ({ left: 100, top: 200, width: 100, height: 40 }); };
// 前三次读各向左偏 15/10/5px、之后固定：模拟「元素正在落位」，stable 必须等它停下再取中心点。
const settlingBox = () => {
  boxFn = (n) => ({ left: 100 + Math.max(0, 3 - n) * 5, top: 200, width: 100, height: 40 });
};
// 永远在动：stable 判据必须放弃并报错，而不是"读满两轮就随便拿一个坐标点下去"。
const alwaysMovingBox = () => {
  boxFn = (n) => ({ left: 100 + n * 7, top: 200 + n * 3, width: 100, height: 40 });
};

beforeEach(() => {
  document.body.innerHTML = '';
  reads = 0;
  staticBox();
  document.elementFromPoint = () => null; // 默认无遮挡（= 无从判定，检查项按现状跳过）
  fakeChrome.scripting.executeScript.mockClear();
  fakeChrome.scripting.executeScript.mockImplementation(pageScript);
  cdp.clickAt.mockReset();
  cdp.clickAt.mockResolvedValue({ ok: true });
  resetSnapshotBaseline();
});

describe('批17(a) stable：探测到真点之间元素必须已经停下', () => {
  it('对照腿：box 静止时 stable 不许把可点步判死（channel=cdp、只点一次）', async () => {
    document.body.innerHTML = '<button id="send">发送</button>';
    const r = await dispatch({ action: 'click', tab_id: 42, target: '#send' }, makeDeps());
    expect(r.channel).toBe('cdp');
    expect(cdp.clickAt).toHaveBeenCalledTimes(1);
  });

  it('永远在动的元素：一帧坐标都不许下发，红因点名 unstable', async () => {
    document.body.innerHTML = '<button id="send">发送</button>';
    alwaysMovingBox();
    await expect(
      dispatch({ action: 'click', tab_id: 42, target: '#send' }, makeDeps()),
    ).rejects.toThrow(/unstable/);
    expect(cdp.clickAt).not.toHaveBeenCalled();
  });

  it('落位中的元素：点的是**结算后**那一帧的中心，不是第一帧的', async () => {
    document.body.innerHTML = '<button id="send">发送</button>';
    settlingBox();
    const r = await dispatch({ action: 'click', tab_id: 42, target: '#send' }, makeDeps());
    expect(r.channel).toBe('cdp');
    expect(cdp.clickAt).toHaveBeenCalledTimes(1);
    // 结算后 left=100 ⇒ 中心 x=150。少了这条断言，「读了两帧却拿第一帧坐标点下去」也算过。
    const [, x, y] = cdp.clickAt.mock.calls[0];
    expect({ x, y }).toEqual({ x: 150, y: 220 });
  });

  it('click_near 的 probe 同样有牙（第二份内联检查，不许只补 injClick）', async () => {
    document.body.innerHTML = '<div><textarea></textarea><button id="send">发送</button></div>';
    alwaysMovingBox();
    await expect(
      dispatch({ action: 'click_near', tab_id: 42, anchor: 'textarea', button_text: '发送' }, makeDeps()),
    ).rejects.toThrow(/unstable/);
    expect(cdp.clickAt).not.toHaveBeenCalled();
  });

  it('comment_send 的发送按钮 probe 同样有牙（第三份内联检查：提交点比一次点击更该严）', async () => {
    document.body.innerHTML = '<div><textarea></textarea><button>发送</button></div>';
    alwaysMovingBox();
    await expect(
      dispatch({
        action: 'comment_send', tab_id: 42, input_selector: 'textarea', send_button_text: '发送',
      }, makeDeps()),
    ).rejects.toThrow(/unstable/);
    expect(cdp.clickAt).not.toHaveBeenCalled();
  });

  it('stable 必须在远小于 15s 注入竞速窗的时间内给出结论', async () => {
    // 这条守的是预算：settle 窗写死过大时，comment_send 会被 raceTimeout 切成
    // comment_send_inject_timeout——那是「点击从未发生」的归因，被 stable 借用就是假归因。
    document.body.innerHTML = '<button id="send">发送</button>';
    alwaysMovingBox();
    const started = Date.now();
    await expect(
      dispatch({ action: 'click', tab_id: 42, target: '#send' }, makeDeps()),
    ).rejects.toThrow(/unstable/);
    expect(Date.now() - started).toBeLessThan(3000);
  });
});

describe('批17(b) 点后身份复核：写步不许静默 ok', () => {
  const writeClick = () => ({ action: 'click', tab_id: 42, target: '#send', verify_identity: true });

  it('点完之后中心点被别人接管 → 报 element_moved，且**不再点第二次**', async () => {
    document.body.innerHTML = '<button id="send">发送</button><div id="overlay"></div>';
    const btn = document.querySelector('#send');
    const overlay = document.querySelector('#overlay');
    document.elementFromPoint = () => btn; // 探测时按钮就是自己的 hit-target
    cdp.clickAt.mockImplementation(async () => {
      document.elementFromPoint = () => overlay; // 飞行途中浮层盖上来：落点其实是浮层
      return { ok: true };
    });
    await expect(dispatch(writeClick(), makeDeps())).rejects.toThrow(/element_moved/);
    expect(cdp.clickAt).toHaveBeenCalledTimes(1);
  });

  it('点完之后目标消失（selector 解析不到）→ element_moved', async () => {
    document.body.innerHTML = '<button id="send">发送</button>';
    cdp.clickAt.mockImplementation(async () => {
      document.querySelector('#send').remove();
      return { ok: true };
    });
    await expect(dispatch(writeClick(), makeDeps())).rejects.toThrow(/element_moved/);
    expect(cdp.clickAt).toHaveBeenCalledTimes(1);
  });

  it('点完之后中心点挪出容差 → element_moved（不许"大概没动"就放行）', async () => {
    document.body.innerHTML = '<button id="send">发送</button>';
    cdp.clickAt.mockImplementation(async () => {
      boxFn = () => ({ left: 180, top: 200, width: 100, height: 40 }); // 中心 x 从 150 挪到 230
      return { ok: true };
    });
    await expect(dispatch(writeClick(), makeDeps())).rejects.toThrow(/element_moved/);
    expect(cdp.clickAt).toHaveBeenCalledTimes(1);
  });

  it('身份未变 → ok，且回包明说这次复核过（identity_checked=true）', async () => {
    document.body.innerHTML = '<button id="send">发送</button>';
    const r = await dispatch(writeClick(), makeDeps());
    expect(r.channel).toBe('cdp');
    expect(r.identity_checked).toBe(true);
  });

  it('复核自己跑不动（注入没回包）不能变成静默 ok：未知态点名 identity', async () => {
    document.body.innerHTML = '<button id="send">发送</button>';
    let identityCalls = 0;
    fakeChrome.scripting.executeScript.mockImplementation((opts) => {
      // 身份复核注入的特征：第二个实参是坐标（probe 的第二参是 'probe'/'fallback' 字符串）
      if (Array.isArray(opts.args) && typeof opts.args[1] === 'number') {
        identityCalls += 1;
        return Promise.resolve([{ result: undefined }]);
      }
      return pageScript(opts);
    });
    await expect(dispatch(writeClick(), makeDeps())).rejects.toThrow(/identity/);
    expect(identityCalls).toBeGreaterThan(0);
    expect(cdp.clickAt).toHaveBeenCalledTimes(1);
  });

  it('读步（不带 verify_identity）不付这次额外注入', async () => {
    document.body.innerHTML = '<button id="send">发送</button>';
    const r = await dispatch({ action: 'click', tab_id: 42, target: '#send' }, makeDeps());
    expect(r.channel).toBe('cdp');
    expect(r.identity_checked).not.toBe(true);
    const identityCalls = fakeChrome.scripting.executeScript.mock.calls
      .filter((c) => Array.isArray(c[0].args) && typeof c[0].args[1] === 'number').length;
    expect(identityCalls).toBe(0);
  });

  it('页面已经跳走（navigated）时不做中心点判据：那是跳转，不是移动', async () => {
    document.body.innerHTML = '<button id="send">发送</button>';
    Object.defineProperty(globalThis, 'location', {
      value: { href: 'https://fixture.test/a' }, configurable: true, writable: true,
    });
    cdp.clickAt.mockImplementation(async () => {
      globalThis.location.href = 'https://fixture.test/b'; // 真点之后页面换了
      // 跳转后的新页面里同一个 selector 已经不在原位——这正是"复核会把跳转误判成挪位"的形状。
      // 用例必须能区分「生产代码跳过了复核」和「复核跑了但恰好没红」，所以这里就把复核
      // 会看到的证据摆出来：守卫一旦被删掉（M8），这条腿必须红，而不是继续绿。
      document.body.innerHTML = '<div>新页面：#send 已经没了</div>';
      return { ok: true };
    });
    const r = await dispatch(writeClick(), makeDeps());
    expect(r.navigated).toBe(true);
    expect(cdp.clickAt).toHaveBeenCalledTimes(1);
    expect(r.identity_checked).toBeUndefined(); // 跳过的复核不冒充"我复核过了"
  });

  it('click_near 的写步同样复核（第二个消费点：锚点+文本现算的定位也要能再解析回去）', async () => {
    // 静态锁要求两处消费，但「两处」里只有一处有行为腿就是半只眼：
    // click_near 的 target 不是调用方给的 selector，probe 必须自己回传可再解析的路径，
    // 否则这条复核在这一步形同「拿空串 querySelector」，静默 ok。
    document.body.innerHTML = '<div><textarea></textarea><button id="send">发送</button><div id="overlay"></div></div>';
    const btn = document.querySelector('#send');
    const overlay = document.querySelector('#overlay');
    document.elementFromPoint = () => btn; // 探测时按钮就是自己的 hit-target
    cdp.clickAt.mockImplementation(async () => {
      document.elementFromPoint = () => overlay; // 落点其实被浮层接管
      return { ok: true };
    });
    await expect(
      dispatch({ action: 'click_near', tab_id: 42, anchor: 'textarea', button_text: '发送', verify_identity: true }, makeDeps()),
    ).rejects.toThrow(/element_moved/);
    expect(cdp.clickAt).toHaveBeenCalledTimes(1);
  });

  it('DOM 兜底路径不做点后复核（兜底点的就是元素本身，复核只会把真动作误判成移动）', async () => {
    document.body.innerHTML = '<button id="send">发送</button>';
    let hits = 0;
    document.querySelector('#send').addEventListener('click', () => { hits += 1; });
    cdp.clickAt.mockRejectedValue(new Error('Debugger is not attached to the target'));
    const r = await dispatch(writeClick(), makeDeps());
    expect(r.channel).toBe('dom_fallback');
    expect(hits).toBe(1);
    expect(r.identity_checked).not.toBe(true);
  });
});

describe('批17 静态锁：三份内联 probe 与两处复核挂点必须一起长牙', () => {
  // 注入函数自包含 ⇒ 检查逻辑只能内联多份（见 primitives.js 头部铁律）。
  // 行为腿各测一处，「这些份有没有同步」只能靠读源码钉住：漏改一份 = 那一格的闸门不存在。
  // ?raw 而不是 readFileSync(相对路径)：后者把用例绑死在「必须从包目录跑」上（实测 import.meta.url
  // 在 vitest 里不是 file: 方案，new URL 直接抛 ERR_INVALID_URL_SCHEME）。
  it('unstable 判据在三处 probe 里各出现一次', () => {
    const n = (primitivesSrc.match(/'unstable'/g) || []).length;
    expect(n, `stable 判据出现 ${n} 次 want 3（injClick / injClickNear / injPostCommentSend）`).toBe(3);
  });

  // 这条锁的不是样式，是**服务端台账语义的前提**：Go 侧 isNeverExecuted 把 *_not_interactable
  // 判成「从未在页面上发生」（不记提交尝试、允许重下发）。这个判断成立的唯一理由是
  // 这类文案只在页面内 probe 里产出、产出的那一刻坐标还没离开扩展。
  // 一旦有人在 dispatch（SW 侧，知道"坐标已经发出去了"的那一段）里合成一个 *_not_interactable，
  // 「派发前拒绝」这个前提就假了，而 Go 那边依旧留空台账 —— 那就是一台会双发的机器。
  it('SW 侧 dispatch 一段里不许出现 *_not_interactable（Go 判「从未派发」的前提）', () => {
    const i = primitivesSrc.indexOf('export async function dispatch');
    expect(i, '找不到 dispatch 起点（改名要同时改这条锁）').toBeGreaterThan(-1);
    const tail = primitivesSrc.slice(i);
    const hits = (tail.match(/not_interactable/g) || []).length;
    expect(hits, `dispatch 之后出现 ${hits} 处 not_interactable want 0`).toBe(0);
    // 反向自证：整文件里它确实存在，否则上面那条 0 是"没人产出这个文案"的假绿
    expect((primitivesSrc.match(/not_interactable/g) || []).length).toBeGreaterThan(0);
  });

  it('injClickIdentityCheck 恰有一处定义、两处消费（click 与 click_near 的 CDP 分支）', () => {
    const def = (primitivesSrc.match(/function injClickIdentityCheck/g) || []).length;
    const use = (primitivesSrc.match(/executeInTab\(tabId, injClickIdentityCheck/g) || []).length;
    expect(def, `定义 ${def} 处 want 1`).toBe(1);
    expect(use, `消费 ${use} 处 want 2（两处必须同步，兜底分支不算）`).toBe(2);
  });
});
