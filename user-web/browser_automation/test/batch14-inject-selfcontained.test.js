// 批14 静态闸门：任何被送进页面的注入函数都必须自包含（不得引用模块顶层名字）。
// 与 inject-sandbox.js 的分工：沙箱跑的是「用例真的调用到」的注入函数，
// 这里覆盖的是「全部」注入函数——包括 wait_for_selector / markdown / comment_verify
// 这些单测不一定逐个跑到页面的。漏网一条就是一整个批次的静默降级（真机实证过）。
import { describe, it, expect } from 'vitest';
import { resolve, dirname } from 'node:path';
import { readFile } from 'node:fs/promises';
import { analyzeSource, injectedTargets, externalInjectionTargets } from './inject-lint.js';

const CORE = resolve(dirname(new URL(import.meta.url).pathname), '../src/core');

// accessibility 侧只有 collectInPage 会被注入（assemble/getRefSelector 跑在 SW 里，
// 引用模块态是正当的）。这个清单是手工的，所以下面两条守卫钉住它：
// 成员表达式注入点必须被枚举到、清单成员必须还在导出。
const INJECTED_FROM_ACCESSIBILITY = ['collectInteractiveNodes'];

const read = (f) => readFile(resolve(CORE, f), 'utf8');
const refs = (vs) => vs.map((v) => `${v.fn}->${v.ref}`).sort();

describe('注入函数自包含性（批14 静态闸门）', () => {
  it('primitives.js 枚举到的注入目标不少于 12 个（枚举集合本身不能缩水）', async () => {
    const targets = injectedTargets(await read('primitives.js'));
    expect(targets.length).toBeGreaterThanOrEqual(12);
    for (const must of ['injClick', 'injType', 'injClickNear', 'injPostCommentSend', 'injMarkdown']) {
      expect(targets).toContain(must);
    }
    // executeInTab 自己的形参名就叫 func：它是定义不是注入点，混进来会把「目标解析失败」
    // 伪装成「func 不合规」，所以显式钉住。
    expect(targets).not.toContain('func');
  });

  it('跨模块注入点（deps.x.y）必须被枚举出来——不能悄悄漏成零检查', async () => {
    const external = externalInjectionTargets(await read('primitives.js'));
    expect(external).toContain('deps.accessibility.collectInPage');
  });

  it('primitives.js 全部注入目标零自由变量', async () => {
    expect(analyzeSource(await read('primitives.js')).violations).toEqual([]);
  });

  it('accessibility.js 注入目标零自由变量', async () => {
    const src = await read('accessibility.js');
    for (const name of INJECTED_FROM_ACCESSIBILITY) {
      // 清单成员改名/删除 = 这条闸门悄悄失效，先把它钉住
      expect(src).toMatch(new RegExp(`function\\s+${name}\\b`));
    }
    expect(analyzeSource(src, INJECTED_FROM_ACCESSIBILITY).violations).toEqual([]);
  });

  // 反向测试（闸门自身会不会咬、会不会装作在咬）
  it('反向测试：注入函数引用模块顶层 helper 与常量时闸门必须报出来', async () => {
    const buggy = [
      "const INTERVAL = 200;",
      "function helperCheck(node) { return !!node; }",
      "function injBad(target) {",
      "  const el = document.querySelector(target);",
      "  if (!helperCheck(el)) return { ok: false };",
      "  return { ok: true, ms: INTERVAL };",
      "}",
      "async function dispatchBad(tabId) {",
      "  return await executeInTab(tabId, injBad, ['#x']);",
      "}",
      "async function executeInTab(tabId, func, args) { return func(...args); }",
    ].join('\n');
    const { targets, violations } = analyzeSource(buggy);
    expect(targets).toEqual(['injBad']);
    expect(refs(violations)).toEqual(['injBad->INTERVAL', 'injBad->helperCheck']);
  });

  it('反向测试：注释与字符串里提到顶层名字不算引用（闸门不误伤，也不被人当噪音关掉）', async () => {
    const noisy = [
      "const MAX_NODES = 400;",
      "function injClick(target) { return { ok: !!document.querySelector(target) }; }",
      "function injLoud(sel) {",
      "  // 与 injClick 同一份检查；MAX_NODES 的口径也照它抄（批14 前这里是注释）",
      "  /* 另一段注释提到 MAX_NODES */",
      "  const msg = 'MAX_NODES 引用在字符串里: injClick';",
      "  return { ok: !!document.querySelector(sel), msg };",
      "}",
      "async function dispatchLoud(tabId) { return await executeInTab(tabId, injLoud, ['#x']); }",
      "async function executeInTab(tabId, func, args) { return func(...args); }",
    ].join('\n');
    expect(analyzeSource(noisy).violations).toEqual([]);
  });

  it('反向测试：成员访问的属性名与对象 key 不算引用', async () => {
    const shape = [
      "const role = 'r';",
      "function injShape(el) {",
      "  const o = { role: 'button' };",
      "  return { kind: o.role, prop: el.role, computed: o['role'], role2: role };",
      "}",
      "async function dispatchShape(tabId) { return await executeInTab(tabId, injShape, [null]); }",
      "async function executeInTab(tabId, func, args) { return func(...args); }",
    ].join('\n');
    // o.role / el.role / o['role'] / 对象 key 都不是对模块顶层 role 的引用；
    // 但 role2: role 的 value 位置是真引用，必须报出来。
    expect(refs(analyzeSource(shape).violations)).toEqual(['injShape->role']);
  });

  it('反向测试：同名的函数内局部声明不算违规（闸门不误伤）', async () => {
    const ok = [
      "function helperCheck(node) { return !!node; }",
      "function injGood(target) {",
      "  const helperCheck = (n) => !!n;",
      "  return { ok: helperCheck(document.querySelector(target)) };",
      "}",
      "async function dispatchGood(tabId) { return await executeInTab(tabId, injGood, ['#x']); }",
      "async function executeInTab(tabId, func, args) { return func(...args); }",
    ].join('\n');
    expect(analyzeSource(ok).violations).toEqual([]);
  });

  it('反向测试：注入目标在本模块找不到函数声明时必须报，不能静默通过', async () => {
    const missing = [
      "async function dispatchX(tabId) { return await executeInTab(tabId, injGone, ['#x']); }",
      "async function executeInTab(tabId, func, args) { return func(...args); }",
    ].join('\n');
    expect(refs(analyzeSource(missing).violations)).toEqual(['injGone-><注入目标在本模块没有函数声明>']);
  });
});
