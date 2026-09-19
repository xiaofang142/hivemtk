// dist.js — 分布采样原语（A/B 双链路单一来源）。
// 全部纯函数：rng 可注入（默认 Math.random），统计特性由包内测试锁定，禁止在调用方各自造轮子。

/** sleep（两链共用节拍原语） */
export const sleep = (ms) => new Promise((r) => setTimeout(r, ms));

/**
 * lognormal 截断对数正态采样（Box-Muller + 拒绝采样到 [min,max]）。
 * 出处：xiaohongshu-mcp humanize 时序参数（A 侧 CDP 键入/点击节奏实证值）。
 * median=exp(mu)；sigma 缺省 0.35。
 */
export function lognormal(median, min, max, rng = Math.random, sigma = 0.35) {
  const mu = Math.log(median);
  let v;
  do {
    const u1 = rng() || 1e-9;
    const u2 = rng();
    v = Math.exp(mu + sigma * Math.sqrt(-2 * Math.log(u1)) * Math.cos(2 * Math.PI * u2));
  } while (v < min || v > max);
  return Math.round(v);
}

/** gaussian 截断正态采样（Box-Muller，钳位到 [min,max]）。出处：bridge humanize 参数表。 */
export function gaussian(mean, std, min, max, rng = Math.random) {
  let u = 0;
  let v = 0;
  while (u === 0) u = rng();
  while (v === 0) v = rng();
  const z = Math.sqrt(-2.0 * Math.log(u)) * Math.cos(2.0 * Math.PI * v);
  const val = mean + z * std;
  if (min != null && val < min) return min;
  if (max != null && val > max) return max;
  return val;
}
