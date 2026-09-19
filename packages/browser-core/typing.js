// typing.js — contenteditable 逐段键入计划器（B7：bridge 发送链的拟人核心）。
//
// 分工纪律：本模块只产「计划」（段文本 + 段后停顿 ms），纯函数零 DOM；
// 实际注入（execCommand insertText / 事件派发 / 鼠标活动穿插）由 bridge dom.js 执行。
// 分布与参数全部来自 timing.js 单一来源（type 档案 + 标点停顿/偶发思考停顿形态，
// 停顿形态对齐 bridge humanize.humanType 既有实证注释：标点 200±80ms、随机微停顿）。

import { gaussian } from './dist.js';
import { DELAY_PROFILES } from './timing.js';

const PUNCT = /[,，.。!！?？;；:：、\n]/;

/** 单字符键入间隔（type 档案抽样） */
export function keystrokeDelayMs(rng = Math.random) {
  const cfg = DELAY_PROFILES.type;
  return gaussian(cfg.mean, cfg.std, cfg.min, cfg.max, rng);
}

/** 标点后的加重停顿 */
export function punctuationPauseMs(rng = Math.random) {
  return Math.round(gaussian(200, 80, 80, 500, rng));
}

/**
 * planTypingBursts 把文本切成 1–4 码点的段，段后附停顿毫秒：
 *  - 逐码点遍历（Array.from：emoji/CJK 代理对不被拆碎）；
 *  - 段长随机（1–4，均值≈2.4）——一气呵成整段=机器人特征；
 *  - 段延迟 = 段长 × keystroke 抽样；段尾标点 → 追加标点停顿；
 *  - 5% 概率注入 0.6–1.5s 思考停顿（模拟阅读/措辞迟疑）；
 *  - 末段延迟 0（调用方发完即走）。
 * @returns {Array<{text: string, delayMs: number}>}
 */
export function planTypingBursts(text, { rng = Math.random } = {}) {
  const chars = Array.from(String(text ?? ''));
  const bursts = [];
  let i = 0;
  while (i < chars.length) {
    const maxLen = Math.min(chars.length - i, 1 + Math.floor(rng() * 4)); // 1..4
    let seg = '';
    let count = 0;
    let j = i;
    while (j < chars.length && count < maxLen) {
      seg += chars[j];
      count++;
      j++;
      if (PUNCT.test(chars[j - 1])) break; // 标点收段（停顿挂在段尾）
    }
    let delay = 0;
    if (j < chars.length) {
      for (let k = 0; k < count; k++) delay += keystrokeDelayMs(rng);
      if (PUNCT.test(seg[seg.length - 1])) delay += punctuationPauseMs(rng);
      if (rng() < 0.05) delay += Math.round(600 + rng() * 900); // 偶发思考停顿
    }
    bursts.push({ text: seg, delayMs: delay });
    i = j;
  }
  return bursts;
}

/** 鼠标活动穿插判定：每段键入后以 p 概率（缺省 18%）产生一次视口微动 */
export function shouldInterjectMouse(rng = Math.random) {
  return rng() < 0.18;
}
