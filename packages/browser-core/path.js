// path.js — 鼠标轨迹与落点抖动纯函数（A 侧 CDP clickAt 单一实现来源，
// bridge 侧「穿插鼠标活动」复用同一采样器）。出处：xiaohongshu-mcp humanize/mouse.go 实测参数。

/** easeInOut 缓动（0~1）：起步慢、中段快、收尾慢 */
export function easeInOut(t) {
  return t < 0.5 ? 2 * t * t : 1 - Math.pow(-2 * t + 2, 2) / 2;
}

/**
 * bezierPoints 三次贝塞尔轨迹采样：控制点沿垂直方向随机偏移（75% 同向弧线）。
 * 步数随距离 10–40、easeInOut 节拍——与 A 侧原实现逐行等价（rng 注入）。
 */
export function bezierPoints(x0, y0, x1, y1, rng = Math.random) {
  const dist = Math.hypot(x1 - x0, y1 - y0);
  const steps = Math.max(10, Math.min(40, Math.round(dist / 10)));
  const dx = x1 - x0;
  const dy = y1 - y0;
  const len = Math.max(dist, 1);
  const px = -dy / len;
  const py = dx / len;
  const sgn = rng() < 0.75 ? 1 : -1;
  const amp1 = dist * (0.05 + rng() * 0.10) * sgn;
  const amp2 = dist * (0.05 + rng() * 0.10) * (rng() < 0.5 ? sgn : -sgn);
  const c1x = x0 + dx / 3 + px * amp1;
  const c1y = y0 + dy / 3 + py * amp1;
  const c2x = x0 + (2 * dx) / 3 + px * amp2;
  const c2y = y0 + (2 * dy) / 3 + py * amp2;
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

/** clickJitter 落点抖动：半径=min(8, 目标半宽)内均匀随机；无尺寸信息退化 3px */
export function clickJitter(radius, rng = Math.random) {
  const r = Math.max(0, Math.min(8, radius || 3));
  return {
    dx: Math.round((rng() * 2 - 1) * r),
    dy: Math.round((rng() * 2 - 1) * r),
  };
}

/**
 * moveStartPoint 轨迹起点：无上一鼠标位置时从目标随机方向 20–60px 处出发
 * （固定起点=可检测特征）。返回 {sx, sy}，非负钳位。
 */
export function moveStartPoint(x, y, rng = Math.random) {
  const ang = rng() * Math.PI * 2;
  const d = 20 + rng() * 40;
  return { sx: Math.max(0, Math.round(x + Math.cos(ang) * d)), sy: Math.max(0, Math.round(y + Math.sin(ang) * d)) };
}

/** 轨迹每步间隔（ms）：5–9ms/步 */
export function stepIntervalMs(rng = Math.random) {
  return 5 + Math.round(rng() * 4);
}
