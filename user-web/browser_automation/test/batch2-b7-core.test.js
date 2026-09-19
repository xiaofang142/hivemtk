// B7（批2）共享基座 @hivemtk/browser-core 契约测试：
//  1) 分布/轨迹/键入计划纯函数的统计与形态特性；
//  2) A 侧 input.js 的 bezierPoints/TIMING 导出必须就是共享包本体（对象同一性），
//     防止"改了一份忘了另一份"的双源漂移。
import { describe, it, expect } from 'vitest';
import {
  lognormal, gaussian, sleep,
  makeCdpTiming, DELAY_PROFILES, sampleDelay,
  bezierPoints, clickJitter, moveStartPoint, stepIntervalMs,
  planTypingBursts, shouldInterjectMouse,
} from '../../../packages/browser-core/index.js';
import { bezierPoints as inputBezier, TIMING as inputTiming } from '../src/core/cdp/input.js';

// 确定性 rng：LCG，可复现
const lcg = (seed = 42) => {
  let s = seed >>> 0;
  return () => {
    s = (s * 1664525 + 1013904223) >>> 0;
    return s / 4294967296;
  };
};

describe('browser-core / 分布采样', () => {
  it('lognormal 落在 [min,max] 且样本非恒定', () => {
    const samples = new Set();
    for (let i = 0; i < 100; i++) {
      const v = lognormal(120, 30, 400);
      expect(v).toBeGreaterThanOrEqual(30);
      expect(v).toBeLessThanOrEqual(400);
      samples.add(v);
    }
    expect(samples.size).toBeGreaterThan(10);
  });
  it('gaussian 钳位截断', () => {
    for (let i = 0; i < 100; i++) {
      const v = gaussian(100, 60, 50, 150);
      expect(v).toBeGreaterThanOrEqual(50);
      expect(v).toBeLessThanOrEqual(150);
    }
  });
  it('rng 注入 → 完全确定性（同 seed 同序列）', () => {
    const a = [lognormal(120, 30, 400, lcg(7)), gaussian(80, 25, 30, 220, lcg(8))];
    const b = [lognormal(120, 30, 400, lcg(7)), gaussian(80, 25, 30, 220, lcg(8))];
    expect(a).toEqual(b);
  });
});

describe('browser-core / 时序参数表', () => {
  it('makeCdpTiming 四档全在参数表区间内', () => {
    const t = makeCdpTiming();
    for (let i = 0; i < 50; i++) {
      expect(t.keystroke()).toBeGreaterThanOrEqual(30);
      expect(t.keystroke()).toBeLessThanOrEqual(400);
      expect(t.clickHold()).toBeGreaterThanOrEqual(45);
      expect(t.clickHold()).toBeLessThanOrEqual(250);
      expect(t.pointerSettle()).toBeGreaterThanOrEqual(200);
      expect(t.pointerSettle()).toBeLessThanOrEqual(1200);
      expect(t.afterClick()).toBeGreaterThanOrEqual(150);
      expect(t.afterClick()).toBeLessThanOrEqual(2000);
    }
  });
  it('DELAY_PROFILES 五档逐字锁定（迁移只动归属不动数值）', () => {
    expect(DELAY_PROFILES.click).toEqual({ mean: 180, std: 60, min: 50, max: 360 });
    expect(DELAY_PROFILES.type).toEqual({ mean: 80, std: 25, min: 30, max: 220 });
    expect(DELAY_PROFILES.scroll).toEqual({ mean: 300, std: 120, min: 150, max: 800 });
    expect(DELAY_PROFILES.think).toEqual({ mean: 1500, std: 600, min: 800, max: 3500 });
    expect(DELAY_PROFILES.longthink).toEqual({ mean: 3500, std: 1200, min: 2000, max: 8000 });
  });
  it('sampleDelay 未知档案回落 think 且不抛', () => {
    const v = sampleDelay('no-such-profile');
    expect(v).toBeGreaterThanOrEqual(800);
    expect(v).toBeLessThanOrEqual(3500);
  });
});

describe('browser-core / 轨迹与落点', () => {
  it('bezierPoints 首点近起点、尾点=终点、步数随距离 10–40', () => {
    const near = bezierPoints(0, 0, 10, 10, lcg(1));
    expect(near.length).toBe(10);
    const last = near[near.length - 1];
    expect(last.x).toBe(10);
    expect(last.y).toBe(10);
    const far = bezierPoints(0, 0, 3000, 4000, lcg(2));
    expect(far.length).toBe(40);
    expect(far[far.length - 1]).toEqual({ x: 3000, y: 4000 });
  });
  it('clickJitter 半径钳位 [-8,8]', () => {
    for (let i = 0; i < 50; i++) {
      const { dx, dy } = clickJitter(100, lcg(i));
      expect(Math.abs(dx)).toBeLessThanOrEqual(8);
      expect(Math.abs(dy)).toBeLessThanOrEqual(8);
    }
    // radius=0/缺省 → 无尺寸信息退化 3px（原语义逐字保持）
    const zero = clickJitter(0);
    expect(Math.abs(zero.dx)).toBeLessThanOrEqual(3);
    expect(Math.abs(zero.dy)).toBeLessThanOrEqual(3);
  });
  it('moveStartPoint 距目标 20–60px（近似）且非负', () => {
    for (let i = 0; i < 50; i++) {
      const p = moveStartPoint(500, 500, lcg(i + 100));
      expect(p.sx).toBeGreaterThanOrEqual(0);
      expect(p.sy).toBeGreaterThanOrEqual(0);
      const d = Math.hypot(p.sx - 500, p.sy - 500);
      expect(d).toBeGreaterThanOrEqual(18); // 取整余量
      expect(d).toBeLessThanOrEqual(62);
    }
  });
  it('stepIntervalMs ∈ [5,9]', () => {
    for (let i = 0; i < 100; i++) {
      const v = stepIntervalMs(lcg(i));
      expect(v).toBeGreaterThanOrEqual(5);
      expect(v).toBeLessThanOrEqual(9);
    }
  });
});

describe('browser-core / 键入计划器', () => {
  it('段拼接逐字还原（含 emoji 代理对不被拆碎）', () => {
    const text = '你好👋，世界！abc.,;';
    for (let seed = 1; seed < 20; seed++) {
      const bursts = planTypingBursts(text, { rng: lcg(seed) });
      expect(bursts.map((b) => b.text).join('')).toBe(text);
      for (const ch of bursts.map((b) => b.text).join('')) {
        expect(ch.codePointAt(0) >= 0xD800 && ch.codePointAt(0) <= 0xDFFF).toBe(false);
      }
    }
  });
  it('末段延迟=0；标点收段（段尾标点段的下一段独立）', () => {
    const bursts = planTypingBursts('好，走。', { rng: lcg(5) });
    expect(bursts[bursts.length - 1].delayMs).toBe(0);
    expect(bursts.some((b) => /[，。]$/.test(b.text))).toBe(true);
  });
  it('空/缺失文本 → 空计划', () => {
    expect(planTypingBursts('')).toEqual([]);
    expect(planTypingBursts(null)).toEqual([]);
  });
  it('shouldInterjectMouse 概率形态：全 0 rng 恒真、全 0.99 恒假', () => {
    expect(shouldInterjectMouse(() => 0)).toBe(true);
    expect(shouldInterjectMouse(() => 0.99)).toBe(false);
  });
});

describe('B7 单一来源锁定（对象同一性，防双份漂移）', () => {
  it('input.js 导出的 bezierPoints 就是共享包本体', () => {
    expect(inputBezier).toBe(bezierPoints);
  });
  it('input.js TIMING 由 makeCdpTiming 装配（含四档函数）', () => {
    for (const k of ['keystroke', 'clickHold', 'pointerSettle', 'afterClick']) {
      expect(typeof inputTiming[k]).toBe('function');
    }
  });
});
