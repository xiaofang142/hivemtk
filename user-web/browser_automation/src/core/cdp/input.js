// cdp/input.js — chrome.debugger trusted 输入模块（实现级规范来源：
//   Puppeteer cdp/Input.ts + USKeyboardLayout.ts（参数表唯一事实来源）
//   autoclaw-cc/xiaohongshu-skills（同场景：mouseMoved 轨迹 + press hold）
//   A9T9/RPA cdp_input（事件序列工厂 + attach 生命周期 + infobar 坐标陷阱对策）
//   xiaohongshu-mcp humanize（时序参数：对数正态分布）

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

// ---- humanize 时序（对数正态采样，参数表来自 xiaohongshu-mcp humanize/provider.go）----

function lognormal(median, min, max) {
  // Mu/Sigma 反解：对数正态的 median = exp(Mu)
  const mu = Math.log(median);
  const sigma = 0.35;
  let v;
  do {
    const u1 = Math.random() || 1e-9;
    const u2 = Math.random();
    v = Math.exp(mu + sigma * Math.sqrt(-2 * Math.log(u1)) * Math.cos(2 * Math.PI * u2));
  } while (v < min || v > max);
  return Math.round(v);
}

const TIMING = {
  keystroke: () => lognormal(120, 30, 400),   // 逐字间隔
  clickHold: () => lognormal(84, 45, 250),    // down→up
  pointerSettle: () => lognormal(300, 200, 1200), // move后→down前
  afterClick: () => lognormal(400, 150, 2000),  // 点击后
};

const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

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
      await chrome.debugger.attach(target, '1.3');
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
    return await fn(target);
  } catch (e) {
    const msg = String(e?.message || e);
    // detach 类错误：清状态后重 attach 重试一次（midscene 模式）
    if (msg.includes('Debugger is not attached') || msg.includes('Cannot access') || msg.includes('No target with given id')) {
      attached.delete(tabId);
      await chrome.debugger.attach(target, '1.3').catch(() => {});
      attached.set(tabId, true);
      return await fn(target);
    }
    throw e;
  } finally {
    // idle 复用：3s 内无新命令才 detach，减少横幅闪烁
    if (detachTimer) clearTimeout(detachTimer);
    detachTimer = setTimeout(() => {
      for (const tid of [...attached]) {
        chrome.debugger.detach({ tabId: tid }).catch(() => {});
        attached.delete(tid);
      }
    }, 3000);
  }
}

const send = (target, method, params) => chrome.debugger.sendCommand(target, method, params);

// ---- 键入：ASCII 走 keyDown/keyUp（码表），CJK/emoji 逐字 insertText（=ImeCommitText）----

async function typeText(tabId, text) {
  return withDebugger(tabId, async (target) => {
    for (const ch of String(text)) {
      if (isASCIIKey(ch)) {
        const def = KEY_DEFS[ch];
        const base = { key: ch, code: def.code, windowsVirtualKeyCode: def.vk };
        await send(target, 'Input.dispatchKeyEvent', { type: 'keyDown', ...base, text: def.text ?? ch, unmodifiedText: ch });
        await sleep(30);
        await send(target, 'Input.dispatchKeyEvent', { type: 'keyUp', ...base });
      } else {
        // CJK/emoji/中文标点：USKeyboardLayout 无条目，insertText=一次 ImeCommitText（trusted）
        await send(target, 'Input.insertText', { text: ch });
      }
      await sleep(TIMING.keystroke());
    }
    return { ok: true, chars: String(text).length };
  });
}

// ---- 鼠标：贝塞尔轨迹 → settle → 落点抖动 → press → hold → release（clickCount 一致）----
// F3（G12）：起点=上一 mousemove 位置（无则目标点随机偏移），三次贝塞尔插值
// （步数随距离 10–40、每步 5–9ms、控制点垂直偏移 ±5–15%、easeInOut）+ 落点抖动
// （min(8, radius) 内均匀随机）——参数表照抄 xiaohongshu-mcp humanize/mouse.go 实测值。

const lastMouse = new Map(); // tabId -> {x,y}

// easeInOut 缓动参数（0~1）
function easeInOut(t) {
  return t < 0.5 ? 2 * t * t : 1 - Math.pow(-2 * t + 2, 2) / 2;
}

// bezierPoints 三次贝塞尔轨迹采样：从 (x0,y0) 到 (x1,y1)，控制点沿垂直方向随机偏移
function bezierPoints(x0, y0, x1, y1) {
  const dist = Math.hypot(x1 - x0, y1 - y0);
  const steps = Math.max(10, Math.min(40, Math.round(dist / 10)));
  // 单位垂直向量（轨迹弧的侧向控制基）
  const dx = x1 - x0, dy = y1 - y0;
  const len = Math.max(dist, 1);
  const px = -dy / len, py = dx / len;
  // 两个控制点：沿线 1/3、2/3 处，各带 ±5–15% 距离的随机侧移（同向为主、少量反向，逼近真人弧线）
  const sgn = Math.random() < 0.75 ? 1 : -1;
  const amp1 = dist * (0.05 + Math.random() * 0.10) * sgn;
  const amp2 = dist * (0.05 + Math.random() * 0.10) * (Math.random() < 0.5 ? sgn : -sgn);
  const c1x = x0 + dx / 3 + px * amp1, c1y = y0 + dy / 3 + py * amp1;
  const c2x = x0 + (2 * dx) / 3 + px * amp2, c2y = y0 + (2 * dy) / 3 + py * amp2;
  const pts = [];
  for (let i = 1; i <= steps; i++) {
    const t = easeInOut(i / steps);
    const mt = 1 - t;
    const x = mt * mt * mt * x0 + 3 * mt * mt * t * c1x + 3 * mt * t * t * c2x + t * t * t * x1;
    const y = mt * mt * mt * y0 + 3 * mt * mt * t * c1y + 3 * mt * t * t * c2y + t * t * t * y1;
    pts.push({ x: Math.round(x), y: Math.round(y) });
  }
  return pts;
}

// clickJitter 落点抖动半径：目标元素半宽的 15% 与 8px 取小（无尺寸信息时 3px）
function clickJitter(radius) {
  const r = Math.max(0, Math.min(8, radius || 3));
  return { dx: Math.round((Math.random() * 2 - 1) * r), dy: Math.round((Math.random() * 2 - 1) * r) };
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
      const ang = Math.random() * Math.PI * 2;
      const d = 20 + Math.random() * 40;
      sx = Math.max(0, Math.round(x + Math.cos(ang) * d));
      sy = Math.max(0, Math.round(y + Math.sin(ang) * d));
    }
    for (const pt of bezierPoints(sx, sy, x, y)) {
      await send(target, 'Input.dispatchMouseEvent', {
        type: 'mouseMoved', x: pt.x, y: pt.y, button: 'none', buttons: 0, modifiers: 0,
      });
      await sleep(5 + Math.round(Math.random() * 4)); // 5–9ms/步
    }
    lastMouse.set(tabId, { x, y });
    const { dx, dy } = clickJitter(opts.jitterRadius);
    const cx = Math.max(0, x + dx), cy = Math.max(0, y + dy);
    await sleep(TIMING.pointerSettle());
    await send(target, 'Input.dispatchMouseEvent', {
      type: 'mousePressed', x: cx, y: cy, button: 'left', buttons: 1, clickCount: 1, modifiers: 0,
    });
    await sleep(TIMING.clickHold());
    await send(target, 'Input.dispatchMouseEvent', {
      type: 'mouseReleased', x: cx, y: cy, button: 'left', buttons: 0, clickCount: 1, modifiers: 0,
    });
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

export { typeText, clickAt, pressEnter, isASCIIKey, KEY_DEFS, TIMING, bezierPoints };
