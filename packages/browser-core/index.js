// @hivemtk/browser-core — A/B 双链路共享拟人输入基座（B7）。
// 纯函数、零依赖、零 DOM/chrome；rng 可注入。消费方：
//   A 链路 user-web/browser_automation/src/core/cdp/input.js（CDP trusted 输入）
//   B 链路 user-web/bridge/src/core/{humanize,dom}.js（内容脚本 contenteditable）
// 两链路的 manifest/权限/注入通道不合并（设计使然），合并的只有这里的参数与分布。

export { sleep, lognormal, gaussian } from './dist.js';
export { makeCdpTiming, DELAY_PROFILES, sampleDelay } from './timing.js';
export { easeInOut, bezierPoints, clickJitter, moveStartPoint, stepIntervalMs } from './path.js';
export { planTypingBursts, keystrokeDelayMs, punctuationPauseMs, shouldInterjectMouse } from './typing.js';
