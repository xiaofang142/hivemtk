
import { createLogger } from './logger.js';
// B7（批2）：分布采样 / sleep / 场景延迟参数表迁入 @hivemtk/browser-core
// ——与 A 链路 CDP 输入共读同一参数源（数值逐字保持，双份漂移即缺陷源）。
// 滑动窗口_density 自适应与 DOM 键入节奏是 bridge 特有能力，留在本地。
import { gaussian, sleep, DELAY_PROFILES } from '../../../../packages/browser-core/index.js';

const log = createLogger('humanize', 'bridge');

// 滑动窗口记录器（P3-B）：按 (channel, account, action) 跟踪"过去 windowMs 内的次数"
class SlidingWindowCounter {
  constructor({ windowMs = 60000, capacity = 50 } = {}) {
    this.windowMs = windowMs;
    this.capacity = capacity;
    this._buckets = new Map(); 
  }

  _key(...parts) { return parts.join('|'); }

  hit(...parts) {
    const k = this._key(...parts);
    const now = Date.now();
    const arr = this._buckets.get(k) || [];
    arr.push(now);
    while (arr.length && now - arr[0] > this.windowMs) arr.shift();
    this._buckets.set(k, arr);
    return arr.length;
  }

  count(...parts) {
    const k = this._key(...parts);
    const arr = this._buckets.get(k);
    if (!arr) return 0;
    const now = Date.now();
    while (arr.length && now - arr[0] > this.windowMs) arr.shift();
    return arr.length;
  }

  isFull(...parts) {
    return this.count(...parts) >= this.capacity;
  }

  prune() {
    const now = Date.now();
    for (const [k, arr] of this._buckets.entries()) {
      while (arr.length && now - arr[0] > this.windowMs) arr.shift();
      if (arr.length === 0) this._buckets.delete(k);
    }
  }
}

// 全局滑动窗口单例（按 channel 维度）
const sendWindow = new SlidingWindowCounter({ windowMs: 60000, capacity: 20 });
const clickWindow = new SlidingWindowCounter({ windowMs: 60000, capacity: 30 });
if (typeof setInterval !== 'undefined') {
  setInterval(() => {
    sendWindow.prune();
    clickWindow.prune();
  }, 60000);
}

// 业务场景 → 默认延迟参数：见 browser-core timing.js DELAY_PROFILES（单一来源）。
// 真人操作典型延迟（参考 [CSDN 人类化] 2026 评测）：click 180±60 / type 80±25 /
// scroll 300±120 / think 1500±600 / longthink 3500±1200（截断正态）。

async function humanDelay(profile = 'think', options = {}) {
  const cfg = DELAY_PROFILES[profile] || DELAY_PROFILES.think;
  let mult = 1.0;
  if (options.channel && options.account) {
    const w = (profile === 'type' || profile === 'think' || profile === 'longthink')
      ? sendWindow : clickWindow;
    const cnt = w.count(options.channel, options.account);
    const cap = w.capacity;
    const density = cnt / cap;
    if (density > 0.9) mult = 2.0;
    else if (density > 0.7) mult = 1.6;
    else if (density > 0.3) mult = 1.3;
  }
  const ms = gaussian(cfg.mean * mult, cfg.std * mult, cfg.min * mult, cfg.max * mult);
  if (options.channel && options.account) {
    const w = (profile === 'type' || profile === 'think' || profile === 'longthink')
      ? sendWindow : clickWindow;
    w.hit(options.channel, options.account);
  }
  await sleep(Math.round(ms));
  return Math.round(ms);
}

async function humanType(element, text, options = {}) {
  if (!element || !text) return;
  const charMean = options.charMean ?? 80;
  const charStd = options.charStd ?? 25;
  const punctPause = options.punctuationPause ?? { mean: 200, std: 80 };
  for (let i = 0; i < text.length; i++) {
    const ch = text[i];
    // 真实用户不会一气呵成——每 3-5 字符会有一次微小停顿
    const isPunct = /[,，.。!！?？;；:：\n]/.test(ch);
    const ms = isPunct
      ? gaussian(punctPause.mean, punctPause.std, 80, 500)
      : gaussian(charMean, charStd, 30, 220);
    await sleep(Math.round(ms));
    try {
      const ev = new KeyboardEvent('keydown', { key: ch, bubbles: true });
      element.dispatchEvent(ev);
    } catch (_) {  }
    try {
      const proto = Object.getPrototypeOf(element);
      const setter = Object.getOwnPropertyDescriptor(proto, 'value')?.set;
      if (setter) setter.call(element, (element.value || '') + ch);
      else element.value = (element.value || '') + ch;
    } catch (_) {
      element.value = (element.value || '') + ch;
    }
    try {
      const ev = new InputEvent('input', { inputType: 'insertText', data: ch, bubbles: true });
      element.dispatchEvent(ev);
    } catch (_) {  }
  }
  try {
    element.dispatchEvent(new Event('change', { bubbles: true }));
  } catch (_) {  }
}

async function humanMousePath(from, to, moveFn, options = {}) {
  if (!from || !to || typeof moveFn !== 'function') {
    return { steps: 0, durationMs: 0 };
  }
  const steps = options.steps || (20 + Math.floor(Math.random() * 15)); 
  const bias = options.controlPointBias ?? 0.3;
  const jitter = options.jitter ?? 30;
  // 控制点：中段偏置
  const cp1 = {
    x: from.x + (to.x - from.x) * bias + (Math.random() - 0.5) * jitter,
    y: from.y + (to.y - from.y) * bias + (Math.random() - 0.5) * jitter,
  };
  const cp2 = {
    x: from.x + (to.x - from.x) * (1 - bias) + (Math.random() - 0.5) * jitter,
    y: from.y + (to.y - from.y) * (1 - bias) + (Math.random() - 0.5) * jitter,
  };
  const startMs = Date.now();
  for (let i = 1; i <= steps; i++) {
    const t = i / steps;
    const u = 1 - t;
    const x = u*u*u*from.x + 3*u*u*t*cp1.x + 3*u*t*t*cp2.x + t*t*t*to.x;
    const y = u*u*u*from.y + 3*u*u*t*cp1.y + 3*u*t*t*cp2.y + t*t*t*to.y;
    // 加速度：起步慢、中段快、收尾慢（sin 曲线）
    const accel = 1 - Math.cos((t * Math.PI) * 2) * 0.3; 
    await moveFn(x, y, t);
    await sleep(Math.round(8 + 8 * accel + Math.random() * 6));
  }
  return { steps, durationMs: Date.now() - startMs };
}

async function humanScroll(scrollFn, options = {}) {
  if (typeof scrollFn !== 'function') return { chunks: 0 };
  const total = options.totalDelta || 600;
  const chunks = options.chunks || (4 + Math.floor(Math.random() * 4)); 
  const pauseEvery = options.pauseEvery || 2;
  const backtrackProb = options.backtrackProb || 0.15; 
  let remaining = total;
  let n = 0;
  while (remaining > 0 && n < chunks) {
    n++;
    const seg = Math.min(remaining, Math.round(remaining / (chunks - n + 1) * (0.7 + Math.random() * 0.5)));
    await scrollFn(seg);
    remaining -= seg;
    if (n % pauseEvery === 0) {
      await humanDelay('scroll');
    }
    if (Math.random() < backtrackProb && remaining > 0) {
      const back = Math.min(remaining, 30 + Math.floor(Math.random() * 40));
      await scrollFn(-back);
      remaining += back;
      await sleep(120 + Math.random() * 200);
    }
  }
  return { chunks: n };
}

// 暴露给测试的辅助
export const _internal = {
  gaussian,
  SlidingWindowCounter,
  DELAY_PROFILES,
};

export { humanDelay, humanType, humanMousePath, humanScroll, sendWindow, clickWindow };


