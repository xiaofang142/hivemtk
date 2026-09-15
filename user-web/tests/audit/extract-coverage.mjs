// 全覆盖清单自动提取引擎
// 输出三份资产 + 一份对账矩阵：
//   1. routes.json            —— 全部前端页面路径（从 router/modules/*.js 自动提取，替代手工 route-paths.json）
//   2. frontend-apis.json     —— 全部前端 API 调用（方法+URL+文件+函数）
//   3. backend-apis.json      —— 全部后端路由端点（方法+路径+所在文件+group 前缀）
//   4. coverage-matrix.json   —— 前后端对账矩阵（前端调用 ↔ 后端注册），找出：
//        - frontendOnly: 前端调用但后端未注册（真 bug / 已废弃）
//        - backendOnly: 后端注册但前端未调用（死端点 / 平台端用）
//        - match: 已对账
import fs from 'node:fs'
import path from 'node:path'
import { execFileSync } from 'node:child_process'

const __dirname = path.dirname(new URL(import.meta.url).pathname)
const WEB = path.resolve(__dirname, '../../src')
const SERVER = path.resolve(__dirname, '../../../user-server')
const OUT = path.resolve(__dirname, 'coverage')

fs.mkdirSync(OUT, { recursive: true })

// ==================== 1. 前端页面路由 ====================
// router/index.js 中 moduleNames 列表决定加载哪些模块；每个模块导出 path 数组（Layout children）
function extractRoutes() {
  const modDir = path.join(WEB, 'router/modules')
  const mods = fs.readdirSync(modDir).filter((f) => f.endsWith('.js'))
  const routes = []
  for (const f of mods) {
    const src = fs.readFileSync(path.join(modDir, f), 'utf8')
    // 提取 path: 'xxx' 或 path: "/xxx"
    const re = /path:\s*['"]([^'"]+)['"]/g
    let m
    while ((m = re.exec(src)) !== null) {
      let p = m[1]
      if (p.startsWith('/')) {
        routes.push({ path: p, module: f, absolute: true })
      } else {
        routes.push({ path: '/' + p, module: f, absolute: false })
      }
    }
  }
  // 去重 + 排序
  const seen = new Set()
  const uniq = []
  for (const r of routes.sort((a, b) => a.path.localeCompare(b.path))) {
    if (!seen.has(r.path)) { seen.add(r.path); uniq.push(r) }
  }
  return uniq
}

// ==================== 2. 前端 API 调用 ====================
// 从 src/api/*.js 提取 http.get/post/put/delete/upload('url') 与模板字符串
function extractFrontendApis() {
  const apiDir = path.join(WEB, 'api')
  const files = fs.readdirSync(apiDir).filter((f) => f.endsWith('.js'))
  const apis = []
  for (const f of files) {
    const src = fs.readFileSync(path.join(apiDir, f), 'utf8')
    // http.method('literal') 或 http.method(`template`)
    const re = /http\.(get|post|put|delete|upload)\s*\(\s*(['"`])([^'"`]+)\2/g
    let m
    while ((m = re.exec(src)) !== null) {
      apis.push({ file: f, method: m[1].toUpperCase(), url: m[3], hasPathParam: /\$\{|:\w+/.test(m[3]) })
    }
  }
  // 去重
  const seen = new Set()
  const uniq = []
  for (const a of apis) {
    const k = `${a.method} ${a.url}`
    if (!seen.has(k)) { seen.add(k); uniq.push(a) }
  }
  return uniq.sort((a, b) => a.url.localeCompare(b.url))
}

// ==================== 3. 后端路由端点 ====================
// 从 internal/router/*.go 提取 .METHOD("path")，并识别 group 前缀
function extractBackendApis() {
  const routerDir = path.join(SERVER, 'internal/router')
  const files = fs.readdirSync(routerDir).filter((f) => f.endsWith('.go'))
  const apis = []
  for (const f of files) {
    const src = fs.readFileSync(path.join(routerDir, f), 'utf8')
    // 识别当前文件的 group 前缀：auth.Group("")/r.Group("/api")/platform := r.Group("/api/platform")
    const groups = []
    for (const gm of src.matchAll(/(\w+)\s*[:=]\s*[a-z]+\.Group\(\s*"([^"]*)"\s*\)/g)) {
      groups.push({ var: gm[1], prefix: gm[2] })
    }
    // 捕获 .GET(".", handler) 等注册，并尝试推断所属 group
    const re = /\.(GET|POST|PUT|DELETE|PATCH)\(\s*"([^"]+)"\s*,/g
    let m
    while ((m = re.exec(src)) !== null) {
      // 向前找最近的 group 变量引用（粗略：看该行是否在某个 group 作用域，这里用前文最近的 group := 语句）
      const before = src.slice(0, m.index)
      let prefix
      // 找最后一个 "xxx := r.Group" 或 "xxx := auth.Group" 的位置，其 var 是否出现在本行调用对象前
      const lines = before.split('\n')
      let lastGroupLine = -1
      let lastGroupVar = ''
      let lastGroupPrefix = ''
      for (let i = lines.length - 1; i >= 0; i--) {
        const g = groups.find((g) => new RegExp(`\\b${g.var}\\b\\s*[:=]\\s*[a-z]+\\.Group\\(`).test(lines[i]))
        if (g) { lastGroupLine = i; lastGroupVar = g.var; lastGroupPrefix = g.prefix; break }
      }
      // 检查调用对象：形如 auth.GET( 或 platform.GET( 等
      const line = src.slice(0, m.index)
      const callObj = line.split('\n').pop().match(/(\w+)\.(GET|POST|PUT|DELETE|PATCH)\s*\(\s*"([^"]+)"/)
      const objVar = callObj ? callObj[1] : (lastGroupVar || '')
      const gp = groups.find((g) => g.var === objVar)
      prefix = gp ? gp.prefix : (objVar === 'r' ? '' : lastGroupPrefix)
      apis.push({ file: f, method: m[1], path: prefix + m[2], objVar })
    }
  }
  // 去重
  const seen = new Set()
  const uniq = []
  for (const a of apis) {
    const k = `${a.method} ${a.path}`
    if (!seen.has(k)) { seen.add(k); uniq.push(a) }
  }
  return uniq.sort((a, b) => a.path.localeCompare(b.path))
}

// ==================== 4. 对账 ====================
// 前端 URL 模板（如 /api/faqs/${id}）→ 匹配后端路径（/api/faqs/:id）
function frontendToBackend(fUrl) {
  // 模板字符串里的 ${var} → :var；:xxx → :xxx
  let p = fUrl.replace(/\$\{(\w+)\}/g, ':$1')
  // 去掉查询串
  p = p.split('?')[0]
  return p
}
function reconcile(front, back) {
  const backBy = new Map()
  for (const b of back) {
    const k = `${b.method} ${b.path}`
    if (!backBy.has(k)) backBy.set(k, b)
  }
  const match = []
  const frontendOnly = []
  for (const f of front) {
    const bp = frontendToBackend(f.url)
    // 后端路径可能带 :id，前端模板转成 :id 后可直接比较；也可能后端是具体路径（如 /api/clues/type）
    const found = backBy.get(`${f.method} ${bp}`) || [...backBy.keys()].find((k) => {
      const [m, p] = k.split(' ')
      if (m !== f.method) return false
      // 支持后端 :id 与前端 ${id} 对齐：把后端路径参数化成正则
      const re = new RegExp('^' + p.replace(/:[^/]+/g, '[^/]+') + '$')
      return re.test(bp)
    })
    if (found) {
      const [m, p] = found.split(' ')
      match.push({ ...f, backendPath: p })
    } else {
      frontendOnly.push(f)
    }
  }
  const backSet = new Set(back.map((b) => `${b.method} ${b.path}`))
  const matchedSet = new Set(match.map((m) => `${m.method} ${m.backendPath}`))
  const backendOnly = back.filter((b) => !matchedSet.has(`${b.method} ${b.path}`))
  return { match, frontendOnly, backendOnly, backSet }
}

// ==================== 主流程 ====================
const routes = extractRoutes()
const front = extractFrontendApis()
const back = extractBackendApis()
const { match, frontendOnly, backendOnly } = reconcile(front, back)

fs.writeFileSync(path.join(OUT, 'routes.json'), JSON.stringify(routes, null, 2))
fs.writeFileSync(path.join(OUT, 'frontend-apis.json'), JSON.stringify(front, null, 2))
fs.writeFileSync(path.join(OUT, 'backend-apis.json'), JSON.stringify(back, null, 2))

const matrix = {
  generatedAt: new Date().toISOString(),
  summary: {
    pages: routes.length,
    frontendApis: front.length,
    backendApis: back.length,
    matched: match.length,
    frontendOnly: frontendOnly.length,
    backendOnly: backendOnly.length,
  },
  frontendOnly,
  backendOnly,
}
fs.writeFileSync(path.join(OUT, 'coverage-matrix.json'), JSON.stringify(matrix, null, 2))

console.log('===== 全覆盖清单提取完成 =====')
console.log(`页面路由: ${routes.length}`)
console.log(`前端 API 调用: ${front.length}  (方法+URL 去重后)`)
console.log(`后端路由端点: ${back.length}`)
console.log(`前后端对账: 匹配 ${match.length} / 前端独有 ${frontendOnly.length} / 后端独有 ${backendOnly.length}`)
console.log(`输出目录: ${OUT}`)

// 打印前端独有的（可能是真 bug）
if (frontendOnly.length) {
  console.log('\n----- 前端调用但后端未注册 (可能真 bug) -----')
  for (const f of frontendOnly.slice(0, 60)) console.log(`  ${f.method} ${f.url}  (${f.file})`)
  if (frontendOnly.length > 60) console.log(`  ... 其余 ${frontendOnly.length - 60} 条`)
}
