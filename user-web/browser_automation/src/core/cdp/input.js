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

// ---- 鼠标：mouseMoved 前置 → settle → press → hold → release（clickCount 一致）----

async function clickAt(tabId, x, y) {
  return withDebugger(tabId, async (target) => {
    // 前置移动：从邻近点 5 步线性轨迹（xiaohongshu-skills 模式），button:"none"
    const sx = Math.max(0, x - 20), sy = Math.max(0, y - 45);
    for (let i = 1; i <= 5; i++) {
      await send(target, 'Input.dispatchMouseEvent', {
        type: 'mouseMoved',
        x: Math.round(sx + ((x - sx) * i) / 5),
        y: Math.round(sy + ((y - sy) * i) / 5),
        button: 'none', buttons: 0, modifiers: 0,
      });
      await sleep(8);
    }
    await sleep(TIMING.pointerSettle());
    await send(target, 'Input.dispatchMouseEvent', {
      type: 'mousePressed', x, y, button: 'left', buttons: 1, clickCount: 1, modifiers: 0,
    });
    await sleep(TIMING.clickHold());
    await send(target, 'Input.dispatchMouseEvent', {
      type: 'mouseReleased', x, y, button: 'left', buttons: 0, clickCount: 1, modifiers: 0,
    });
    await sleep(TIMING.afterClick());
    return { ok: true };
  });
}

export { typeText, clickAt, isASCIIKey, KEY_DEFS, TIMING };
