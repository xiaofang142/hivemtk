// inject-sandbox.js — 复刻 Chrome executeScript 的「序列化边界」，供单测使用。
//
// 为什么需要它（批14 真机实证）：chrome.scripting.executeScript 只把 func.toString()
// 送到页面里执行，闭包变量全部丢失。真 Chrome 上 probe 分支因此稳定抛
// ReferenceError（executeScript 回 result:null），primitives.js 的 click 兜底把这条
// 误判成「CDP 不可用」→ 每次点击都静默降级成 DOM 合成，且被降级路径的双发放大成
// 「一次步骤两次动作」。旧测试直接 func(...args) 调用原函数，闭包全在，
// 于是这条断链在单测里永远不可能露出来——测试环境本身就是盲区。
//
// new Function 编译出的函数作用域是**全局**，不是模块作用域：
// 用它重建注入函数，任何真正的自由闭包引用都会在调用时抛 ReferenceError，
// 与页面里的行为一致。这就是可跑起来的「注入函数必须自包含」闸门。
import { vi } from 'vitest';

/** 把函数重建到全局作用域（丢掉闭包），模拟页面侧拿到的那份代码。 */
export function deserializedAsPage(fn) {
  // eslint-disable-next-line no-new-func
  return new Function(`return (${fn.toString()})`)();
}

/**
 * 严格版 executeScript mock：注入前先去闭包，注入后原样返回 [{result}]。
 * 任何引用了模块作用域自由变量的注入函数，都会在测试里抛 ReferenceError，
 * 而不是像以前那样静默通过。
 * result 一律 await：真 Chrome 会等注入函数返回的 Promise 结算后再回包
 *（wait_for_selector 这类就靠它），旧 mock 把 Promise 对象本身当结果返回，
 * 等于把「页内异步原语」排除在测试面之外。
 */
export const strictExecuteScript = vi.fn(async ({ func, args = [] }) => {
  const pageFn = deserializedAsPage(func);
  return [{ result: await pageFn(...args) }];
});
