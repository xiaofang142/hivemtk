// cdp/input.js — chrome.debugger trusted 输入模块（实现级规范来源：
//   Puppeteer cdp/Input.ts + USKeyboardLayout.ts（参数表唯一事实来源）
//   autoclaw-cc/xiaohongshu-skills（同场景：mouseMoved 轨迹 + press hold）
//   A9T9/RPA cdp_input（事件序列工厂 + attach 生命周期 + infobar 坐标陷阱对策）
//   xiaohongshu-mcp humanize（时序参数：对数正态分布）
// B7：分布采样/时序参数表/轨迹与落点算法迁入 @hivemtk/browser-core
// ——与 bridge 内容脚本共读同一参数源（数值逐字保持，双份漂移即缺陷源）。

import {
  sleep,
  makeCdpTiming,
  bezierPoints,
  clickJitter,
  moveStartPoint,
  stepIntervalMs,
} from '../../../../../packages/browser-core/index.js';

// ---- WindowsVirtualKeyCode 码表（USKeyboardLayout 子集，仅本基座用到的）----

const KEY_DEFS = {
  // 数字
  '0': { code: 'Digit0', vk: 48 }, '1': { code: 'Digit1', vk: 49 },
  '2': { code: 'Digit2', vk: 50 }, '3': { code: 'Digit3', vk: 51 },
  '4': { code: 'Digit4', vk: 52 }, '5': { code: 'Digit5', vk: 53 },
  '6': { code: 'Digit6', vk: 54 }, '7': { code: 'Digit7', vk: 55 },
  '8': { code: 'Digit8', vk: 56 }, '9': { code: 'Digit9', vk: 57 },
  // 字母（key 与 code 是两条等价记录）
  ...Object.fromEntries('abcdefghijklmnopqrstuvwxyz'.split('').map((c, i) => [c, { code: `Key${c.toUpperCase()}`, vk: 65 + i }])),
  // 常用符号（无 shift 主位）
  ';': { code: 'Semicolon', vk: 186 }, '=': { code: 'Equal', vk: 187 },
  ',': { code: 'Comma', vk: 188 }, '-': { code: 'Minus', vk: 189 },
  '.': { code: 'Period', vk: 190 }, '/': { code: 'Slash', vk: 191 },
  '`': { code: 'Backquote', vk: 192 }, '[': { code: 'BracketLeft', vk: 219 },
  '\\': { code: 'Backslash', vk: 220 }, ']': { code: 'BracketRight', vk: 221 },
  "'": { code: 'Quote', vk: 222 }, ' ': { code: 'Space', vk: 32 },
  // 特殊键
  Enter: { code: 'Enter', vk: 13, text: '\r' },
  Tab: { code: 'Tab', vk: 9 }, Backspace: { code: 'Backspace', vk: 8 },
  Delete: { code: 'Delete', vk: 46 }, Escape: { code: 'Escape', vk: 27 },
};

const isASCIIKey = (ch) => Object.prototype.hasOwnProperty.call(KEY_DEFS, ch);

// ---- humanize 时序（对数正态采样；参数表单一来源见 @hivemtk/browser-core/timing.js）----

const TIMING = makeCdpTiming();

// ---- 写通道 deadline（真机实测：无闸门 = 整条 host 链黑盒）----
// 实测（Chrome 154 / 靶站 tab / SW 内直测）：attach 97ms、单条 sendCommand 49ms 都正常，
// 但**连续** Input 事件的 ack 会随机尖刺——3 条 insertText 串行 9403ms（≈3.1s/条），
// 同批次另一轮 4 条为 213/72/1367/68ms。渲染进程不出帧时 ack 就压着不回来。
// 旧实现 attach 与 send 全是裸 await、typeText 逐字串行 await：
// 最坏耗时 = 2N 条 × 单条尖刺，N=6 的评论就能撑爆服务端 30s/45s 闸，
// 而扩展一个帧都不回 → 服务端只见「host 命令超时」，连是哪一步卡住都无从归因。
// 处置：每条 CDP 调用各自加 deadline（单条卡住=快速失败，不让它吃掉整条预算），
// typeText 另加整体墙钟预算（连续卡顿时提前收手，回报部分字数=结局未知而非成功）。
const CDP_ATTACH_DEADLINE_MS = 8000;
const CDP_SEND_DEADLINE_MS = 5000;
const TYPE_TOTAL_BUDGET_MS = 15000;
const CDP_COMMAND_DEADLINE_MS = 25000;

function withDeadline(promise, ms, label) {
  // 内层先挂一个空 catch：race 被 sleep 分支赢下后，CDP 命令仍可能在几十秒后才 reject，
  // 那时若没人接就是一个 unhandled rejection（跑在定时器/socket 回调里，前缀全量会 rc=1）。
  // 迟到的 reject 一律吞掉——race 的结果早已定了，late settle 不该改写已回的帧。
  const inner = Promise.resolve(promise);
  inner.catch(() => {});
  const deadline = sleep(ms).then(() => {
    throw new Error(`${label}_deadline: 超过 ${ms}ms 未回包`);
  });
  deadline.catch(() => {});
  return Promise.race([inner, deadline]);
}

// ---- attach 生命周期（A9T9 模式：单例 onDetach + idle 延迟 detach + 容忍 already-attached）----

const attached = new Map(); // tabId -> true
let detachTimer = null;
let listenersReady = false;

function ensureDebugListeners() {
  if (listenersReady) return; // SW 顶层注册一次；测试环境（无 chrome）跳过
  if (typeof chrome === 'undefined' || !chrome.debugger) return;
  listenersReady = true;
  chrome.debugger.onDetach.addListener((source) => {
    const tabId = source?.tabId;
    if (tabId != null) attached.delete(tabId);
  });
}

async function withDebugger(tabId, fn) {
  ensureDebugListeners();
  const target = { tabId };
  if (detachTimer) { clearTimeout(detachTimer); detachTimer = null; }
  if (!attached.has(tabId)) {
    try {
      await withDeadline(chrome.debugger.attach(target, '1.3'), CDP_ATTACH_DEADLINE_MS, 'cdp_attach');
      attached.set(tabId, true);
      // infobar 挤压视口 ~56px 且动画期间坐标错位（A9T9 实测）——固定等待稳定
      await sleep(500);
    } catch (e) {
      const msg = String(e?.message || e);
      if (!msg.includes('Another debugger is already attached')) throw e;
      attached.set(tabId, true); // 已被占用视为成功（midscene 模式）
    }
  }
  try {
    // 兜底总闸：attach/单条 send 各自有 deadline，typeText/clickAt 内部也各有预算，
    // 但这层是「无论如何都要回一帧」的最后保证——扩展不回帧 = 服务端只能等 30s/45s 黑盒，
    // 连卡在哪一步都无从归因。总闸取 25s < 服务端 defaultCmdTimeout(30s)，
    // 保证扩展先于服务端超时回一个带错误名的帧（比让服务端判超时可归因得多）。
    return await withDeadline(fn(target), CDP_COMMAND_DEADLINE_MS, 'cdp_command');
  } catch (e) {
    const msg = String(e?.message || e);
    // detach 类错误：只修状态（清掉 + 重 attach），**绝不重跑 fn**。
    // 真机语义：本模块的 fn 全是输入类命令（键入/点击），一条事件入队后
    // 页面可能已经消费掉它；重跑=把整个动作再来一遍（单测实测：一次 typeText('ab')
    // 在中途 detach 后发出两遍 keyDown('a')）。恢复动作交上层按「结局未知」裁决，
    // 不在这里赌「大概没生效」。
    if (msg.includes('Debugger is not attached') || msg.includes('Cannot access') || msg.includes('No target with given id')) {
      attached.delete(tabId);
      await withDeadline(chrome.debugger.attach(target, '1.3'), CDP_ATTACH_DEADLINE_MS, 'cdp_reattach').catch(() => {});
      attached.set(tabId, true);
    }
    throw e;
  } finally {
    // idle 复用：3s 内无新命令才 detach，减少横幅闪烁
    if (detachTimer) clearTimeout(detachTimer);
    detachTimer = setTimeout(() => {
      detachTimer = null;
      // 两个坑都由单测照出来：
      // ① attached 是 Map，`[...attached]` 解出来的是 [tabId, true] **对**——
      //   detach 于是收到 {tabId:[21,true]}（必然失败又被吞），delete 删的是不存在的键，
      //   结果是"3s 后收起调试横幅"这个功能从来没生效过。必须显式取 .keys()。
      // ② 这个回调可能在 chrome.debugger 已经不在的时候才跑（SW 被回收、测试拆除），
      //   而属性访问本身就抛 TypeError，`.catch()` 挡不住；异常跑在定时器里没人接，
      //   于是一次动作留下一个未捕获错误（全量跑「129 用例全绿 + 进程 rc=1」的真因），
      //   并且循环半途而废把后面的 tab 全憋住。收尾路径逐条吞。
      for (const tid of [...attached.keys()]) {
        attached.delete(tid);
        try {
          const p = chrome.debugger?.detach({ tabId: tid });
          if (p && typeof p.catch === 'function') p.catch(() => {});
        } catch { /* noop：一个 tab 的 detach 失败不影响其余 */ }
      }
    }, 3000);
  }
}

// 单条 CDP 命令加 deadline：ack 尖刺时在这里截断，而不是把整条命令预算耗光。
// 报错名 cdp_send_deadline 独立于 click_unacked —— clickAt 的 press/release 段
// 另有 awaitAcks 预算把它收敛成 click_unacked（结局未知），本层只负责别把调用方挂死。
const send = (target, method, params) =>
  withDeadline(chrome.debugger.sendCommand(target, method, params), CDP_SEND_DEADLINE_MS, 'cdp_send');

// ---- 键入：ASCII 走 keyDown/keyUp（码表），CJK/emoji 逐字 insertText（=ImeCommitText）----

async function typeText(tabId, text) {
  const chars = [...String(text)];
  return withDebugger(tabId, async (target) => {
    // 整体墙钟预算：逐字串行下最坏耗时 = 2N × 单条尖刺（F11 实测后台 tab 5s/ack，
    // 这里真机实测尖刺 ~3.1s）。不给总闸的话 N 稍大就把 30s 服务端闸吃光 → 黑盒。
    // 超预算时**提前收手并如实回报已落字数**：已敲进去的字留在输入框里（不可假装没发生），
    // 上层 comment_prep 拿 partial 回包照常走 comment_verify 的 DOM 真相裁决。
    const startedAt = Date.now();
    let typed = 0;
    for (const ch of chars) {
      if (Date.now() - startedAt > TYPE_TOTAL_BUDGET_MS) {
        return { ok: true, chars: chars.length, typed, partial: true, elapsed_ms: Date.now() - startedAt };
      }
      if (isASCIIKey(ch)) {
        const def = KEY_DEFS[ch];
        const base = { key: ch, code: def.code, windowsVirtualKeyCode: def.vk };
        await send(target, 'Input.dispatchKeyEvent', { type: 'keyDown', ...base, text: def.text ?? ch, unmodifiedText: ch });
        await sleep(30);
        await send(target, 'Input.dispatchKeyEvent', { type: 'keyUp', ...base });
        typed += 1;
      } else {
        // CJK/emoji/中文标点：USKeyboardLayout 无条目，insertText=一次 ImeCommitText（trusted）
        await send(target, 'Input.insertText', { text: ch });
        typed += 1;
      }
      await sleep(TIMING.keystroke());
    }
    return { ok: true, chars: chars.length, typed, elapsed_ms: Date.now() - startedAt };
  });
}

// ---- 鼠标：贝塞尔轨迹 → settle → 落点抖动 → press → hold → release（clickCount 一致）----
// F3（G12）：起点=上一 mousemove 位置（无则目标点随机偏移），三次贝塞尔插值
// （步数随距离 10–40、每步 5–9ms、控制点垂直偏移 ±5–15%、easeInOut）+ 落点抖动
// （min(8, radius) 内均匀随机）——参数表照抄 xiaohongshu-mcp humanize/mouse.go 实测值。

const lastMouse = new Map(); // tabId -> {x,y}

// F11（夹具真机实测）：后台 tab 不出帧 → 每个 mouseMoved 的 CDP ack 都要等满
// 5s 超时（实测逐条 5003/5004/5005ms 串联），10–40 步轨迹 = 50–200s，写腿永远走不到
// 按下那一步（服务端只能看到 45s 命令超时，页面上一个鼠标事件都没落地）。
// 对策：事件全部按序入队（IPC 顺序即事件顺序），只在「一段共享预算」里等 ack——
// 慢 ack 不再逐个串联；按下/抬起同样入队后共享预算，超时抛 click_unacked：
// 事件已下发=结局未知，交服务端 finalize 用只读验证裁决（禁重试，防双发）。
const TRAJECTORY_ACK_BUDGET_MS = 6000;
const CLICK_ACK_BUDGET_MS = 15000;

// awaitAcks 在预算内等一组命令的 ack；返回 {done, error}——预算内没等完不算失败
// （事件已在渲染进程队列里），只有真正抛错才算。
// cdp_send_deadline 属于「没等完」而不是「抛错」：单条 deadline 截断的语义与本
// 函数的共享预算完全一致（ack 没回来，事件已入队），若把它当硬失败会直接废掉
// F11 慢 ack 路径——后台 tab 的 mouseMoved 本来就不 ack，抛错=永远走不到按下那一步。
async function awaitAcks(promises, budgetMs) {
  const settled = Promise.allSettled(promises);
  const raced = await Promise.race([
    settled.then(() => "done"),
    sleep(budgetMs).then(() => "timeout"),
  ]);
  if (raced === "timeout") return { done: false };
  const results = await settled;
  if (results.some((r) => r.status === 'rejected' && String(r.reason?.message || '').includes('cdp_send_deadline'))) {
    return { done: false };
  }
  const failed = results.find((r) => r.status === "rejected");
  if (failed) throw failed.reason;
  return { done: true };
}

/**
 * clickAt CDP 可信点击（铁律 2：写操作必经通道）。
 * opts: { jitterRadius } —— 目标元素半尺寸（内容器提供）用于落点抖动。
 * 轨迹起点记忆（lastMouse）：连续操作从上一位置自然移动，而非每次同一偏移出发。
 */
async function clickAt(tabId, x, y, opts = {}) {
  return withDebugger(tabId, async (target) => {
    const last = lastMouse.get(tabId);
    let sx, sy;
    if (last && (last.x !== x || last.y !== y)) {
      sx = last.x; sy = last.y;
    } else {
      // 无历史位置：从目标点随机方向 20–60px 处出发（起点恒定=可检测特征）
      ({ sx, sy } = moveStartPoint(x, y));
    }
    const moves = [];
    for (const pt of bezierPoints(sx, sy, x, y)) {
      moves.push(send(target, 'Input.dispatchMouseEvent', {
        type: 'mouseMoved', x: pt.x, y: pt.y, button: 'none', buttons: 0, modifiers: 0,
      }));
      // 入队间隔=站点看到的移动节奏（humanize 语义保留）；等 ack 才是要避开串联的东西
      await sleep(stepIntervalMs());
    }
    // 与 press/release 同理：整段轨迹跑完才 awaitAcks，途中的拒收先挂空 catch
    for (const p of moves) p.catch(() => {});
    await awaitAcks(moves, TRAJECTORY_ACK_BUDGET_MS);
    lastMouse.set(tabId, { x, y });
    const { dx, dy } = clickJitter(opts.jitterRadius);
    const cx = Math.max(0, x + dx), cy = Math.max(0, y + dy);
    await sleep(TIMING.pointerSettle());
    // press/release 一落地就各挂一个空 catch：它们要等下面的 awaitAcks 才被真正处理，
    // 而这中间的 await sleep() 是宏任务边界——若 CDP 在此期间拒收（实测 Internal error），
    // Node 会在宏任务边界判定为 unhandledRejection（跑在 socket 回调里会让全量 rc=1），
    // 真实的裁决仍在 awaitAcks 的 catch 里，这里只是不让它变成未处理异常。
    const press = send(target, 'Input.dispatchMouseEvent', {
      type: 'mousePressed', x: cx, y: cy, button: 'left', buttons: 1, clickCount: 1, modifiers: 0,
    });
    press.catch(() => {});
    await sleep(TIMING.clickHold());
    const release = send(target, 'Input.dispatchMouseEvent', {
      type: 'mouseReleased', x: cx, y: cy, button: 'left', buttons: 0, clickCount: 1, modifiers: 0,
    });
    release.catch(() => {});
    // press/release 已入队 = 这一页面上很可能已经发生了这次点击。此后**任何**失败
    // （ack 超时、CDP 直接拒、debugger 掉线）都必须收敛成 click_unacked：
    // 原样上抛会被上层当成「CDP 不可用 → 事件从未下发」而降级 DOM 兜底，
    // 在同一个目标上再点一次 = 双发（写按钮的 handler 跑了两遍、评论不可撤回）。
    // 轨迹段的失败不走这条：那时 press 还没入队，兜底是第一次下发，安全。
    let acked;
    try {
      acked = await awaitAcks([press, release], CLICK_ACK_BUDGET_MS);
    } catch (e) {
      throw new Error(`click_unacked: press/release 失败原因 ${String(e?.message || e)}`, { cause: e });
    }
    if (!acked.done) throw new Error('click_unacked');
    await sleep(TIMING.afterClick());
    return { ok: true };
  });
}

// pressEnter 可信 Enter 提交（type submit_on_enter 用）：text='\r'，对齐 Puppeteer 规范
async function pressEnter(tabId) {
  return withDebugger(tabId, async (target) => {
    const base = { key: 'Enter', code: 'Enter', windowsVirtualKeyCode: 13, nativeVirtualKeyCode: 13 };
    await send(target, 'Input.dispatchKeyEvent', { type: 'keyDown', ...base, text: '\r', unmodifiedText: '\r' });
    await sleep(30);
    await send(target, 'Input.dispatchKeyEvent', { type: 'keyUp', ...base });
    return { ok: true };
  });
}

export {
  typeText, clickAt, pressEnter, isASCIIKey, KEY_DEFS, TIMING, bezierPoints,
  CDP_ATTACH_DEADLINE_MS, CDP_SEND_DEADLINE_MS, TYPE_TOTAL_BUDGET_MS, CDP_COMMAND_DEADLINE_MS,
};
