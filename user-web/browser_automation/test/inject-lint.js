// test/inject-lint.js — 注入函数自包含性检查器（测试工具，不进产物）
//
// 背景（批14 真机实证）：chrome.scripting.executeScript 的 func 只以 func.toString() 的
// 形态送进页面，模块作用域里的自由变量在页面侧一律 ReferenceError，且 Chrome 回包
// result:null —— 上层把它误读成「注入没返回」，trusted 通道就这样静默死掉了几个批次。
// test/inject-sandbox.js 用真反序列化挡住了「被测试跑到的」注入函数；这里补上静态的一层：
// 把源文件里**每一个**送进 executeInTab 的函数都查一遍，没被用例跑到也拦。
//
// 实现取 AST（acorn），不取字符串正则：写第一版时用 `fn.toString()` + \bname\b 粗匹配，
// 结果注释里写一句「与 injClick 同一份检查」就被判成引用 injClick —— 一个会误报的门
// 最终等于没人看的门。acorn 是 vitest 传递依赖（未在本包 package.json 声明），
// 一旦它不在依赖树里这里会直接 import 失败：宁可门红着，不可门悄悄不检查。

import { readFileSync } from 'node:fs';
import { resolve } from 'node:path';
import * as acorn from 'acorn';

const INJECT_CALLEE = 'executeInTab';

function parse(src) {
  // locations 必须开：违规项要带被引用常量的声明行号，否则报出来的东西没法直接下手改。
  return acorn.parse(src, { ecmaVersion: 'latest', sourceType: 'module', locations: true });
}

// walk：递归遍历所有子节点。只认 type/start/end/loc 之外的键为子结构，
// 因此不需要为每种节点写一份字段表（新增语法形态默认也被覆盖）。
export function walk(node, visit) {
  if (!node || typeof node !== 'object' || typeof node.type !== 'string') return;
  visit(node);
  for (const key of Object.keys(node)) {
    if (key === 'type' || key === 'start' || key === 'end' || key === 'loc' || key === 'range') continue;
    const child = node[key];
    if (Array.isArray(child)) {
      for (const c of child) walk(c, visit);
    } else if (child && typeof child === 'object' && typeof child.type === 'string') {
      walk(child, visit);
    }
  }
}

function patternNames(pattern, out) {
  walk(pattern, (n) => {
    if (n.type === 'Identifier') out.add(n.name);
  });
}

// topLevelNames + 声明行号：只有模块作用域才有 = 页面侧必然 ReferenceError
function moduleScope(ast) {
  const declLine = new Map();
  const names = new Set();
  const add = (name, node) => {
    if (!name) return;
    names.add(name);
    if (!declLine.has(name) && node?.loc) declLine.set(name, node.loc.start.line);
  };
  for (const stmt of ast.body) {
    const s = stmt.type === 'ExportNamedDeclaration' || stmt.type === 'ExportDefaultDeclaration' ? stmt.declaration : stmt;
    if (!s) continue;
    if (s.type === 'ImportDeclaration') {
      for (const spec of s.specifiers) add(spec.local.name, spec);
    } else if (s.id) {
      add(s.id.name, s);
    } else if (s.type === 'VariableDeclaration') {
      for (const d of s.declarations) {
        const out = new Set();
        patternNames(d.id, out);
        for (const n of out) add(n, d);
      }
    }
  }
  return { names, declLine };
}

// functionDecls：本模块内所有可按月名字取用的函数（声明式、具名函数表达式、
// 以及 `const injX = () => {}` 这类常量绑定）。取第一个同名绑定：注入函数都是顶层定义。
function functionDecls(ast) {
  const byName = new Map();
  const put = (name, node) => {
    if (name && !byName.has(name)) byName.set(name, node);
  };
  walk(ast, (n) => {
    if ((n.type === 'FunctionDeclaration' || n.type === 'FunctionExpression') && n.id?.name) put(n.id.name, n);
    if (n.type === 'VariableDeclarator' && n.id?.type === 'Identifier'
        && (n.init?.type === 'FunctionExpression' || n.init?.type === 'ArrowFunctionExpression')) {
      put(n.id.name, n.init);
    }
  });
  return byName;
}

// injectedCallTargets：真正**调用** executeInTab 的地方第二个实参。
// 只扫 CallExpression，所以 `function executeInTab(tabId, func, args)` 这条定义不会被误当成注入点。
// 实参是成员表达式（跨模块取来的注入函数，如 deps.accessibility.collectInPage）时本模块查不到，
// 单独列出来交给调用方的清单守卫，不能悄悄丢掉。
function injectedCallTargets(ast) {
  const targets = [];
  const external = [];
  walk(ast, (n) => {
    if (n.type !== 'CallExpression' || n.callee?.type !== 'Identifier' || n.callee.name !== INJECT_CALLEE) return;
    const fn = n.arguments[1];
    if (!fn) return;
    if (fn.type === 'Identifier') targets.push(fn.name);
    else if (fn.type === 'MemberExpression' && !fn.computed) external.push(dotted(fn));
  });
  return { targets: [...new Set(targets)], external: [...new Set(external)] };
}

function dotted(node) {
  if (node.type === 'Identifier') return node.name;
  if (node.type === 'ThisExpression') return 'this';
  if (node.type === 'MemberExpression' && !node.computed) return `${dotted(node.object)}.${node.property.name}`;
  return null;
}

// freeVariableRefs：注入函数体内「引用位置」的标识符，减去函数体内自己绑定过的名字。
// 排除的引用位：非计算成员访问的 property、非简写对象字面量的 key、方法/类字段 key、label。
// 绑定位收集整个子树，所以内层作用域的 const 会遮住同名的模块引用——本仓注入函数
// 没有这种跨层用法，真出现时报人工判（比误报成违规更可接受的是：这条偏差是漏报，
// 而漏报由反向测试钉住：见 batch14-inject-selfcontained.test.js 的「反向测试」两组）。
const NON_REF_ROLES = (parent, key) => {
  if (key === 'property' && !parent.computed) return true;
  if (parent.type === 'Property' && key === 'key' && !parent.computed && !parent.shorthand) return true;
  if ((parent.type === 'MethodDefinition' || parent.type === 'PropertyDefinition') && key === 'key') return true;
  if (parent.type === 'LabeledStatement' && key === 'label') return true;
  if ((parent.type === 'BreakStatement' || parent.type === 'ContinueStatement') && key === 'label') return true;
  return false;
};

export function referencedNames(fnNode) {
  const bound = new Set();
  const refs = new Set();
  const stack = [[fnNode, null, null]];
  while (stack.length) {
    const [node, parent, key] = stack.pop();
    if (!node || typeof node !== 'object' || typeof node.type !== 'string') continue;
    if (node !== fnNode) {
      if (node.type === 'FunctionDeclaration' || node.type === 'FunctionExpression' || node.type === 'ArrowFunctionExpression' || node.type === 'ClassDeclaration') {
        if (node.id) bound.add(node.id.name);
        for (const p of node.params ?? []) patternNames(p, bound);
      }
      if (node.type === 'VariableDeclarator') patternNames(node.id, bound);
      if (node.type === 'CatchClause' && node.param) patternNames(node.param, bound);
    }
    if (node.type === 'Identifier' && parent && !NON_REF_ROLES(parent, key)) refs.add(node.name);
    for (const k of Object.keys(node)) {
      if (k === 'type' || k === 'start' || k === 'end' || k === 'loc' || k === 'range') continue;
      const child = node[k];
      if (Array.isArray(child)) {
        for (const c of child) stack.push([c, node, k]);
      } else if (child && typeof child === 'object' && typeof child.type === 'string') {
        stack.push([child, node, k]);
      }
    }
  }
  if (fnNode.id) bound.add(fnNode.id.name);
  for (const p of fnNode.params ?? []) patternNames(p, bound);
  return [...refs].filter((n) => !bound.has(n));
}

// analyzeSource：检查一个模块源码里所有注入目标。targetsOverride 用于跨模块注入函数
// （实参写成 deps.accessibility.collectInPage 那种）：由归属文件的用例传叶子函数名。
export function analyzeSource(src, targetsOverride) {
  const ast = parse(src);
  const { names: modNames, declLine } = moduleScope(ast);
  const decls = functionDecls(ast);
  const calls = injectedCallTargets(ast);
  const targets = targetsOverride || calls.targets;
  const violations = [];
  for (const name of targets) {
    const fnNode = decls.get(name);
    if (!fnNode) {
      violations.push({ fn: name, ref: '<注入目标在本模块没有函数声明>', line: 0 });
      continue;
    }
    for (const ref of referencedNames(fnNode)) {
      if (!modNames.has(ref)) continue;
      violations.push({ fn: name, ref, line: declLine.get(ref) ?? 0 });
    }
  }
  return { targets, externalTargets: calls.external, violations };
}

export function injectedTargets(src) {
  return injectedCallTargets(parse(src)).targets;
}

export function externalInjectionTargets(src) {
  return injectedCallTargets(parse(src)).external;
}

export function analyzeInjectionFile(filePath) {
  return analyzeSource(readFileSync(resolve(filePath), 'utf8'));
}
