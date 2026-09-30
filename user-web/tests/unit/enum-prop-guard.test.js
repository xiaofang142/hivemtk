import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'

// 本文件是「Element Plus 枚举 prop 非法值」这一类缺陷的防回归闸门。
//
// 事故背景：全项目审核期间在 20+ 个页面抓出 16 处 enum type prop 传非法值
// （el-tag 的 ''/'default'、el-button 的 'link'、颜色函数被当 type 传…），
// 单页最多 791 条 Vue 告警。修复后把闸门固化在此，避免回归。
//
// 三条设计纪律（都是踩坑换来的，勿删）：
// 1) 合法值集合一律从 node_modules/element-plus 源码读，不凭记忆 ——
//    el-tag 不含 'default' 而 el-button 含 'default'，两者不同。
// 2) 任何「全 0 结论」必须先过第 4 组 selftest ——
//    否则无法区分「真的干净」与「提取器/正则静默失效」。
// 3) 函数必须真的被执行（而不是读源码断言）—— 静态看不见兜底分支。
//    早期版本靠正则切调用括号，嵌套调用与尾部运算符都被切坏，造出假阳性/假阴性。

const SRC_ROOT = path.resolve(__dirname, '../../src')

// ── 合法值集合：从 element-plus 源码实测 ────────────────────────────────
// es/components/tag/src/tag.mjs:11-22
const EL_TAG_TYPES = ['primary', 'success', 'info', 'warning', 'danger']
// es/components/button/src/button.mjs:6-14（注意含 'default' 与 ''）
const EL_BUTTON_TYPES = ['default', 'primary', 'success', 'warning', 'info', 'danger', 'text', '']
// es/components/alert/src/alert.mjs:24-28 → values: keysOf(TypeComponentsMap)
const EL_ALERT_TYPES = ['primary', 'success', 'warning', 'error', 'info']

const LEGAL = { 'el-tag': EL_TAG_TYPES, 'el-button': EL_BUTTON_TYPES, 'el-alert': EL_ALERT_TYPES }

// @vue/runtime-core validateProp: `if (value == null && !required) return;`
// ⇒ null / undefined 走 prop default，不产生告警。安全。
function isIllegal(legal, v) {
  if (v === null || v === undefined) return false
  if (typeof v !== 'string') return true // 数字/对象/布尔都触发 type check failed
  return !legal.includes(v)
}

// ── 对抗性入参，分两档 ───────────────────────────────────────────────────
// CORE：任何 `|| 'info'` 兜底都能挡住，是本闸门断言的对象。
const CORE_CASES = [
  undefined, null, '', ' ', 'good', 'poor', 'needs-improvement', 'primary',
  'default', 'link', 'text', 'error', 'view', 'click', 'like', 'follow',
  'unknown_action', 'a'.repeat(200),
  0, 1, -1, 80, Number.NaN, true, false,
  {}, { code: 'x', name: 'y' }, { code: undefined, name: undefined },
  [], ['a'], [null], () => {},
]

// PROTO：能命中 Object.prototype 的键。`{...}[k] || 'info'` 挡不住它们 ——
// map[k] 会返回原型上的对象/函数（非 falsy）⇒ 返回非字符串 ⇒ Vue 告警。
// 审核已定性：这类只在后端真的吐出这些保留字时触发（本仓这些字段都是后端自写值），
// 属不可达路径，改 89 处是巨大 churn。故这里不做修复，只用「棘轮」冻结当前条数，
// 保证它不会继续增长。
const PROTO_CASES = ['__proto__', 'constructor', 'toString', 'hasOwnProperty', 'valueOf']

// ── 源码提取（括号/花括号配平 + 跳过字符串字面量）───────────────────────
const OPEN = { '(': ')', '[': ']', '{': '}' }
const CLOSE = new Set([')', ']', '}'])

function sliceBalanced(src, start) {
  let depth = 0
  let quote = null
  for (let i = start; i < src.length; i++) {
    const c = src[i]
    if (quote) {
      if (c === '\\') { i++; continue }
      if (c === quote) quote = null
      continue
    }
    if (c === '"' || c === "'" || c === '`') { quote = c; continue }
    if (OPEN[c]) { depth++; continue }
    if (CLOSE.has(c)) {
      depth--
      if (depth === 0) return src.slice(start, i + 1)
    }
  }
  return null
}

function skipArrow(src, i) {
  while (i < src.length && /\s/.test(src[i])) i++
  // 箭头函数的 '=>' 必须先跳过，否则会把 '=> (s) => ...' 整段当表达式体，
  // 编译出 `function(k){return (=> ...)}` 这种 SyntaxError。
  if (src[i] === '=' && src[i + 1] === '>') {
    i += 2
    while (i < src.length && /\s/.test(src[i])) i++
  }
  return i
}

// 抽 `const NAME = ...` / `function NAME(...)` 的源码，返回可直接编译的函数文本
function extractBinding(src, name) {
  const fnDecl = new RegExp(`(?:async\\s+)?function\\s+${name}\\s*\\(`)
  let m = fnDecl.exec(src)
  if (m) {
    const args = sliceBalanced(src, m.index + m[0].length - 1)
    if (!args) return null
    const braceAt = src.indexOf('{', m.index + m[0].length - 1 + args.length)
    if (!braceAt) return null
    const body = sliceBalanced(src, braceAt)
    if (!body) return null
    // 块体箭头必须写 `=> `：`(a) { return x }` 是非法的（那是方法简写的残缺形式），
    // 拼进 `new Function` 会报 `SyntaxError: Unexpected token '{'`。
    return `${args} => ${body}`
  }

  const constDecl = new RegExp(`(?:const|let|var)\\s+${name}\\s*=\\s*(?:async\\s*)?(\\(|function\\b)`)
  m = constDecl.exec(src)
  if (!m) return null
  // m[0] 本身就以 '(' 结尾，定位左括号要 -1
  const afterEq = m.index + m[0].length - 1
  if (src[afterEq] === '(') {
    const args = sliceBalanced(src, afterEq)
    if (!args) return null
    let i = skipArrow(src, afterEq + args.length)
    if (src[i] === '{') {
      const body = sliceBalanced(src, i)
      if (!body) return null
      return `${args} => ${body}`
    }
    // 表达式体：到顶层 ';' 或「下一行不是续行」为止
    let j = i
    let quote = null
    let nest = 0
    for (; j < src.length; j++) {
      const c = src[j]
      if (quote) { if (c === '\\') { j++; continue } if (c === quote) quote = null; continue }
      if (c === '"' || c === "'" || c === '`') { quote = c; continue }
      if (c === '(' || c === '[' || c === '{') { nest++; continue }
      if (c === ')' || c === ']' || c === '}') { if (nest === 0) break; nest--; continue }
      if (c === ';' && nest === 0) break
      if (c === '\n' && nest === 0) {
        const cont = src.slice(j + 1).match(/^\s*([.?[:&|,+)]|=)/)
        if (!cont) break
      }
    }
    return `${args} => (${src.slice(i, j)})`
  }

  // function 表达式
  const braceAt = src.indexOf('{', afterEq)
  if (!braceAt) return null
  const body = sliceBalanced(src, braceAt)
  if (!body) return null
  return `() ${body}`
}

// 抽 `const NAME = { ... }` 的字面量源码
function extractObjectLiteral(src, name) {
  const m = new RegExp(`(?:const|let|var)\\s+${name}\\s*=\\s*\\{`).exec(src)
  if (!m) return null
  return sliceBalanced(src, src.indexOf('{', m.index + m[0].length - 1))
}

const JS_BUILTINS = new Set([
  'true', 'false', 'null', 'undefined', 'NaN', 'Infinity', 'this', 'arguments',
  'Object', 'Array', 'String', 'Number', 'Boolean', 'JSON', 'Math', 'Date',
  'RegExp', 'Map', 'Set', 'WeakMap', 'Symbol', 'Promise', 'Error', 'TypeError',
  'console', 'window', 'document', 'parseInt', 'parseFloat', 'isNaN',
])

const JS_KEYWORDS = new Set([
  'const', 'let', 'var', 'function', 'class', 'return', 'if', 'else', 'for', 'while',
  'of', 'in', 'new', 'delete', 'typeof', 'instanceof', 'void', 'do', 'switch', 'case',
  'default', 'break', 'continue', 'try', 'catch', 'finally', 'throw', 'yield', 'await',
  'async', 'static', 'get', 'set', 'extends', 'super', 'this', 'import', 'export', 'from',
])

// 找出函数体里引用但未在本地声明的标识符（= 需要一起塞进沙箱的依赖）
function freeVars(code) {
  const declared = new Set()
  for (const m of code.matchAll(/\b(?:const|let|var|function|class)\s+([A-Za-z_$][\w$]*)/g)) declared.add(m[1])
  const paren = /^\(([^)]*)\)/
  const pm = paren.exec(code)
  if (pm) for (const p of pm[1].split(',')) declared.add(p.trim())
  const out = new Set()
  for (const m of code.matchAll(/\b[A-Za-z_$][\w$]*\b/g)) {
    const n = m[0]
    // 关键字必须排除，否则 freeVars 会把 const/return/view 这类词当成依赖，
    // 后面 extractObjectLiteral/extractBinding 又找不到它们，依赖收集静默漏掉真依赖。
    if (!JS_BUILTINS.has(n) && !JS_KEYWORDS.has(n) && !declared.has(n)) out.add(n)
  }
  return out
}

// 把 `<script setup>` 里的目标函数连同它依赖的顶层声明一起编译成可调用函数。
// 只取需要的声明（而不是整段 script）—— 整段含 defineProps/useStore 等
// 只能在组件实例里跑的宏，直接 new Function 会 ReferenceError。
function compileFromScript(scriptSrc, name) {
  const code = extractBinding(scriptSrc, name)
  if (!code) return null
  const decls = []
  const seen = new Set([name])
  const queue = [...freeVars(code)]
  while (queue.length) {
    const dep = queue.shift()
    if (seen.has(dep)) continue
    seen.add(dep)
    const lit = extractObjectLiteral(scriptSrc, dep)
    if (lit) {
      decls.push(`const ${dep} = ${lit};`)
      continue
    }
    const depCode = extractBinding(scriptSrc, dep)
    if (depCode) {
      decls.push(`const ${dep} = ${depCode};`)
      for (const d of freeVars(depCode)) if (!seen.has(d)) queue.push(d)
    }
  }
  // 先把目标函数赋给一个顶层 var，再由具名包装器调用。
  // 末尾的 `()` 不能省：`new Function(body)` 返回的是「执行 body 的那个外层函数」，
  // 直接交给调用方的话，对方拿到的是 body 里 return 的 wrapper 本身（一个函数），
  // 于是每个入参的返回值都成了 `[Function]`，看起来像"提取器坏了"。
   
  return new Function(`${decls.join('\n')}\nvar __target = (${code});\nreturn function () { return __target.apply(this, arguments) }`)()
}

function scriptOf(vueSrc) {
  const blocks = []
  const re = /<script\b[^>]*>([\s\S]*?)<\/script>/g
  let m
  while ((m = re.exec(vueSrc))) blocks.push(m[1])
  // 只删 import 行，保留其余全部代码。
  // 教训：早前版本写的是「块内含 import 就整块跳过」，结果把几乎所有 <script setup>
  // 全跳掉了，提取器静默返回 null，看起来像「没找到函数」。
  return blocks.join('\n').replace(/^\s*import[^\n]*$/gm, '')
}

const readVue = (rel) => fs.readFileSync(path.join(SRC_ROOT, rel), 'utf8')
const scriptOfVue = (rel) => scriptOf(readVue(rel))

function walkVue(dir, out = []) {
  for (const e of fs.readdirSync(dir, { withFileTypes: true })) {
    const p = path.join(dir, e.name)
    // browser_automation 下有构建产物与第三方拷贝，不属于业务 .vue
    if (p.includes(`${path.sep}browser_automation${path.sep}`)) continue
    if (e.isDirectory()) walkVue(p, out)
    else if (e.name.endsWith('.vue')) out.push(p)
  }
  return out
}

/** 对一个函数跑全部入参，返回 { bad: [...], proto: [...] } */
function probeValues(f, legal) {
  const bad = []
  const proto = []
  for (const arg of CORE_CASES) {
    let got
    try { got = f(arg) } catch { continue } // 抛错不归本闸门管（模板有 v-if 守卫）
    if (isIllegal(legal, got)) bad.push(`${fmt(arg)} → ${fmt(got)}`)
  }
  for (const key of PROTO_CASES) {
    let got
    try { got = f(key) } catch { continue }
    if (isIllegal(legal, got)) proto.push(`${key} → ${fmt(got)}`)
  }
  return { bad, proto }
}

const fmt = (v) => {
  if (typeof v === 'string') return JSON.stringify(v.length > 30 ? v.slice(0, 20) + '…' : v)
  if (typeof v === 'symbol' || typeof v === 'function') return `[${typeof v}]`
  if (v === undefined) return 'undefined'
  try { return JSON.stringify(v) ?? String(v) } catch { return String(v) }
}

// ════════════════════════════════════════════════════════════════════════
describe('枚举 prop 防回归闸门', () => {
  describe('1. 映射函数：任何入参都不得返回非法 type', () => {
    const CASES = [
      ['views/douyinCard/Stats.vue', 'getActionType'],
      ['views/douyinCard/CardStats.vue', 'getActionType'],
      ['views/xiaohongshuCard/Stats.vue', 'getActionType'],
      ['views/kuaishouCard/Stats.vue', 'getActionType'],
      ['views/kuaishouCard/CardStats.vue', 'getActionType'],
      ['views/tiktokCard/Stats.vue', 'getActionType'],
      ['views/tiktokCard/CardStats.vue', 'getActionType'],
      ['views/abExperiment/List.vue', 'statusTagType'],
      ['views/intentRecognition/List.vue', 'getMethodTagType'],
      ['views/intentRecognition/List.vue', 'getFineMethodType'],
      ['views/intentRecognition/List.vue', 'getIntentTagType'],
      ['views/intentRecognition/List.vue', 'getConfidenceTagType'],
      ['views/persona/List.vue', 'scoreTagType'],
      ['views/confidence/Panel.vue', 'confTagType'],
      ['views/confidence/Panel.vue', 'decisionTagType'],
      ['views/workflowOrchestrator/List.vue', 'statusType'],
      ['views/workflowOrchestrator/List.vue', 'execStatusType'],
      ['views/unifiedMessage/List.vue', 'getTypeTagType'],
      ['views/unifiedMessage/List.vue', 'getStatusTagType'],
    ]

    it.each(CASES)('%s 的 %s 恒返回合法 el-tag type', (file, fn) => {
      const f = compileFromScript(scriptOfVue(file), fn)
      expect(f, `提取不到 ${file} 的 ${fn}（提取器坏了，不是代码干净）`).toBeTruthy()
      const { bad } = probeValues(f, EL_TAG_TYPES)
      expect(bad, `${file} 的 ${fn} 产生了非法 type`).toEqual([])
    })
  })

  describe('2. 映射表：字面量值必须全部合法', () => {
    const MAPS = [
      ['views/xiaohongshuCard/Stats.vue', 'typeMap'],
      ['views/kuaishouCard/Stats.vue', 'actionTypeMap'],
      ['views/kuaishouCard/CardStats.vue', 'typeMap'],
      ['views/intentRecognition/List.vue', 'RECOGNIZE_METHOD_TAG'],
      ['views/intentRecognition/List.vue', 'FINE_METHOD_TAG'],
      ['views/unifiedMessage/List.vue', 'TYPE_TAG'],
      ['views/unifiedMessage/List.vue', 'STATUS_TAG'],
    ]

    it.each(MAPS)('%s 的 %s 每个值都合法', (file, name) => {
      const lit = extractObjectLiteral(scriptOfVue(file), name)
      expect(lit, `提取不到 ${file} 的 ${name}`).toBeTruthy()
       
      const obj = new Function(`return (${lit})`)()
      const bad = Object.entries(obj)
        .filter(([, v]) => isIllegal(EL_TAG_TYPES, v))
        .map(([k, v]) => `${k}: ${fmt(v)}`)
      expect(bad, `${file} 的 ${name} 含非法 type`).toEqual([])
    })
  })

  describe('3. 模板静态绑定：不得出现非法字面量', () => {
    it('全仓 .vue 的 el-tag/el-button/el-alert type prop 绑定均合法', () => {
      // 必须扫整个 src：只扫 src/views 会漏掉 src/components 下的 23 个 .vue，
      // 早前版本正因为漏扫导致命中数偏低，误以为是正则坏了。
      const files = walkVue(SRC_ROOT)
      expect(files.length, '没扫到任何 .vue，扫描器一定坏了').toBeGreaterThan(200)
      const bad = []
      let checked = 0

      for (const abs of files) {
        const rel = path.relative(SRC_ROOT, abs)
        const tpl = fs.readFileSync(abs, 'utf8').replace(/<script\b[\s\S]*?<\/script>/g, '')
        for (const comp of Object.keys(LEGAL)) {
          // 只扫**静态**绑定 `type="x"`，它的值就是那个裸词，判定无歧义。
          //
          // 早前版本还扫动态绑定 `:type="expr"` 并抽取其中所有引号字面量，
          // 结果 33 条全是误报 —— 表达式里的引号字面量绝大多数不是 type 值：
          //   stepMeta(step.type, 'color')  的 'color' 是**参数**
          //   (x.source) === 'auto' ? … : … 的 'auto' 是**比较操作数**
          //   label 相关的 '高' / '中' 是**文案**
          //   {…}['direction'] || 'info' 的 '' 有 `||` 兜底
          // 动态绑定由第 1 组的执行式检查覆盖（它真调用函数看返回值，判定精确）。
          // 保留一个必然误报的检查比没有检查更糟：它会训练人忽略红色。
          // `type` 前必须是空白：这样 `:type="expr"`（前导冒号）天然不匹配，
          // 不需要易错的负向字符类。早前用 `[^:\s]type` 时静态绑定一律 0 命中，
          // 因为真实写法是 `<el-tag type="primary">` —— `type` 前面是空格。
          for (const m of tpl.matchAll(new RegExp(`<${comp}\\b[^>]*?\\stype\\s*=\\s*"([^"]*)"`, 'g'))) {
            checked++
            if (isIllegal(LEGAL[comp], m[1])) bad.push(`${rel} <${comp} type="${m[1]}"> 非法`)
          }
        }
      }

      // 阈值只做「正则没坏」的兜底，不做精确断言：命中的绑定数会随 .vue 增删漂移，
      // 卡死具体数字只会让闸门因为无关改动而红。真实值约 350+。
      expect(checked, '静态扫描命中数异常偏少，正则可能坏了').toBeGreaterThan(200)
      expect(bad).toEqual([])
    })
  })

  describe('4. PROTO 类棘轮：不可达路径，只冻结条数不修', () => {
    // 见文件头 PROTO_CASES 的说明：`map[k] || 'info'` 挡不住原型链查找。
    // 审核结论是「不修」（89 处、只在后端吐出保留字时触发）。
    // 这里冻结基线，保证它不会继续增长 —— 新增一处就会红。
    const BASELINE = 7

    const CASES = [
      ['views/douyinCard/Stats.vue', 'getActionType'],
      ['views/xiaohongshuCard/Stats.vue', 'getActionType'],
      ['views/kuaishouCard/Stats.vue', 'getActionType'],
      ['views/kuaishouCard/CardStats.vue', 'getActionType'],
      ['views/tiktokCard/Stats.vue', 'getActionType'],
      ['views/unifiedMessage/List.vue', 'getTypeTagType'],
      ['views/workflowOrchestrator/List.vue', 'statusType'],
    ]

    it('PROTO 暴露条数不超过基线', () => {
      // 基线单位是「有多少个映射函数会暴露 PROTO」，不是 findings 条数 ——
      // 每个函数会被 5 个探针键各打一次，按条数算的话一个函数就吃掉 5 的额度。
      const findings = []
      const exposed = []
      for (const [file, fn] of CASES) {
        const f = compileFromScript(scriptOfVue(file), fn)
        expect(f, `提取不到 ${file} 的 ${fn}`).toBeTruthy()
        const { proto } = probeValues(f, EL_TAG_TYPES)
        for (const p of proto) findings.push(`${file} ${fn}: ${p}`)
        if (proto.length) exposed.push(`${file} ${fn}`)
      }
      expect(
        exposed.length,
        `PROTO 暴露的映射函数数超过基线 ${BASELINE}：\n${findings.join('\n')}`
      ).toBeLessThanOrEqual(BASELINE)
    })
  })

  describe('5. selftest：闸门自身必须能抓到已知非法写法', () => {
    // 没有这一组，上面任何「全绿」都无法区分「真干净」与「提取器静默失效」。
    // fixture 必须显式给出被测函数名。早前用正则扫「最后一个 const X =」来猜名字，
    // 遇到函数体里还有别的 const 就取错：mapOwn 的 `const m = {…}` 会被当成被测函数，
    // extractBinding 找不到 `const m = (` 形式而返回 null，闸门误报"提取器坏了"。
    const BAD = [
      { name: 'el-tag 兜底返回空串', fn: "const badEmpty = (s) => ({ a: 'info' }[s] || '')", of: 'badEmpty' },
      { name: "el-tag 返回 'default'", fn: "const badDefault = (s) => ({ a: 'info' }[s] || 'default')", of: 'badDefault' },
      { name: "el-button 传 'link'", fn: "const badLink = () => 'link'", of: 'badLink', legal: EL_BUTTON_TYPES },
      { name: '返回数字（类型错）', fn: 'const badNum = (s) => (s ? 1 : 2)', of: 'badNum' },
      { name: '返回颜色 hex', fn: "const badHex = () => '#10B981'", of: 'badHex' },
      { name: '返回对象', fn: 'const badObj = () => ({ a: 1 })', of: 'badObj' },
    ]

    it.each(BAD)('能判非法：$name', ({ fn, of, legal }) => {
      const f = compileFromScript(fn, of)
      expect(f, `提取不到 ${of}`).toBeTruthy()
      const { bad } = probeValues(f, legal || EL_TAG_TYPES)
      expect(bad.length, `${of} 应被判非法但没判`).toBeGreaterThan(0)
    })

    const SAFE = [
      { name: '三元链字面量', fn: "const okTernary = (s) => (s === 'a' ? 'success' : 'info')", of: 'okTernary' },
      { name: '比较操作数不是 type', fn: "const cmpOnly = (s) => (s === 'default' ? 'warning' : 'info')", of: 'cmpOnly' },
      { name: '未命中返回 undefined（走 prop default）', fn: 'const nullSafe = () => undefined', of: 'nullSafe' },
      { name: '有 hasOwnProperty 守卫', fn: "const mapOwn = (k) => { const m = { a: 'info' }; return Object.prototype.hasOwnProperty.call(m, k) ? m[k] : 'info' }", of: 'mapOwn' },
      { name: '尾部 || 兜底（空串被兜住）', fn: "const trailing = (m) => ({ a: '' }[m] || 'info')", of: 'trailing' },
      { name: '依赖另一个映射函数', fn: "const inner = (x) => ({ a: 'success' }[x] || 'info')\nconst outer = (x) => (inner(x) === 'success' ? 'danger' : 'info')", of: 'outer' },
    ]

    it.each(SAFE)('不误报：$name', ({ fn, of }) => {
      const f = compileFromScript(fn, of)
      expect(f, `提取不到 ${of}`).toBeTruthy()
      const { bad } = probeValues(f, EL_TAG_TYPES)
      expect(bad, `${of} 误报了：${bad.join(', ')}`).toEqual([])
    })

    it('提取器能处理嵌套调用与尾部运算符（早期正则会切坏）', () => {
      const src = [
        "const inner = (x) => ({ a: 'success', b: 'info' }[x] || 'info')",
        "const withTail = (s) => inner(s) || 'info'",
        "const chained = (s) => (inner(s) === 'success' ? 'danger' : 'info')",
      ].join('\n')
      expect(compileFromScript(src, 'withTail')('zzz')).toBe('info')
      expect(compileFromScript(src, 'chained')('a')).toBe('danger')
    })

    it('scriptOf 不会因为块内含 import 就丢掉 <script setup>', () => {
      const s = scriptOf('<script setup>\nimport { ref } from "vue"\nconst f = () => 1\n</script>')
      expect(s).toContain('const f = () => 1')
      expect(s).not.toContain('import')
    })
  })
})
