// timing.js — 拟人节奏参数表（B7 单一来源：A 侧 CDP 与 B 侧 bridge 内容脚本共读此表，
// 双份漂移即缺陷源）。值一律照抄两侧既有实证参数，不做臆改——调参须配真机回归。

import { lognormal, gaussian } from './dist.js';

/**
 * CDP_TIMING A 侧 trusted 输入节拍（对数正态；出处 xiaohongshu-mcp humanize/mouse.go 实测）。
 * rng 注入点供测试与未来分布升级，默认 Math.random。
 */
export function makeCdpTiming(rng = Math.random) {
  return {
    keystroke: () => lognormal(120, 30, 400, rng),      // 逐字间隔
    clickHold: () => lognormal(84, 45, 250, rng),       // down→up
    pointerSettle: () => lognormal(300, 200, 1200, rng), // move后→down前
    afterClick: () => lognormal(400, 150, 2000, rng),   // 点击后
  };
}

/**
 * DELAY_PROFILES B 侧 bridge 场景延迟（截断正态）。
 * 真人操作典型延迟参考（bridge/humanize.js 原表注释：CSDN 人类化 2026 评测）；
 * 数值与旧表逐字一致——迁移只动归属，不动行为。
 */
export const DELAY_PROFILES = Object.freeze({
  click:   { mean: 180, std: 60, min: 50, max: 360 },
  type:    { mean: 80, std: 25, min: 30, max: 220 },
  scroll:  { mean: 300, std: 120, min: 150, max: 800 },
  think:   { mean: 1500, std: 600, min: 800, max: 3500 },
  longthink: { mean: 3500, std: 1200, min: 2000, max: 8000 },
});

/** sampleDelay 按场景档案抽一次延迟（ms，四舍五入整数）。 */
export function sampleDelay(profile = 'think', rng = Math.random) {
  const cfg = DELAY_PROFILES[profile] || DELAY_PROFILES.think;
  return Math.round(gaussian(cfg.mean, cfg.std, cfg.min, cfg.max, rng));
}
