// 深度数据一致性测试引擎（user-web ↔ API ↔ DB）
//
// 与 audit-runner 的本质区别：
//   audit-runner 只验证"前端不报错"（语法/交互层）；
//   本引擎验证"数据正确性"（业务/持久化层），三层对账：
//
//   层A 读一致性：前端表格渲染行 ↔ 页面调用的 API list 响应（仅当双方都有数据时）
//   层B 写一致性：通过真实 API 执行 增→改→删，每步直接查数据库，
//               比对「API 返回字段」↔「DB 落库行字段」，确认端到端数据正确。
//
// 用法：
//   node tests/audit/deep-test.mjs --phase b          # 跑全部已配置资源的写一致性
//   node tests/audit/deep-test.mjs --phase a          # 跑全部页面的读一致性
//   node tests/audit/deep-test.mjs --phase ab         # 两者都跑
//   node tests/audit/deep-test.mjs --page <hashPath>  # 单页读一致性
//
// 资源（写一致性靶子）在下方 RESOURCES 中配置，逐页扩展。

import { chromium } from 'playwright'
import { execFileSync } from 'child_process'
import fs from 'fs'
import path from 'path'
import { fileURLToPath } from 'url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const ROOT = path.resolve(__dirname, '../..')
const BASE = process.env.E2E_BASE_URL || 'http://localhost:8213'
const API_BASE = process.env.API_BASE_URL || 'http://localhost:8204'
const OUT_DIR = path.resolve(__dirname, 'deep-reports')
fs.mkdirSync(OUT_DIR, { recursive: true })

const PW = process.env.PGPASSWORD || (() => {
  try {
    const env = fs.readFileSync(path.resolve(ROOT, '.env'), 'utf8')
    const m = env.match(/^POSTGRES_PASSWORD=(.*)$/m)
    return m ? m[1].trim() : ''
  } catch { return '' }
})()

// ---------------- DB 直查 ----------------
function psql(sql) {
  try {
    return execFileSync('psql', ['-h', 'localhost', '-p', '8232', '-U', 'admin', '-d', 'user_db', '-tAc', sql],
      { env: { ...process.env, PGPASSWORD: PW }, encoding: 'utf8', maxBuffer: 100 * 1024 * 1024, stdio: ['ignore', 'pipe', 'ignore'] })
  } catch (e) {
    return 'SQL_ERROR:' + (e.stdout || e.message)
  }
}
function psqlRows(sql) {
  const raw = psql(sql)
  if (raw.startsWith('SQL_ERROR')) return { error: raw }
  const lines = raw.split('\n').map((l) => l.trim()).filter(Boolean)
  try { return lines.map((l) => JSON.parse(l)) } catch (e) { return { raw, parseError: String(e) } }
}
// 标量查询：SELECT count(*) 等，返回单值字符串
function psqlScalar(sql) {
  const raw = psql(sql)
  if (raw.startsWith('SQL_ERROR')) return { error: raw }
  const v = raw.split('\n').map((l) => l.trim()).filter(Boolean)[0]
  return v === undefined ? null : v
}

// ---------------- API 调用 ----------------
async function apiLogin() {
  const res = await fetch(`${API_BASE}/api/auth/login`, {
    method: 'POST', headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username: 'admin', password: process.env.ADMIN_PASS || 'Admin@123456' })
  })
  const j = await res.json()
  if (!j.data?.token) throw new Error('登录失败: ' + JSON.stringify(j).slice(0, 200))
  return j.data.token
}
async function apiReq(token, method, url, body) {
  const res = await fetch(`${API_BASE}${url}`, {
    method,
    headers: { 'Content-Type': 'application/json', Authorization: 'Bearer ' + token },
    body: body ? JSON.stringify(body) : undefined
  })
  const text = await res.text()
  let json = null
  try { json = JSON.parse(text) } catch { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
  return { status: res.status, json, text }
}

// ---------------- 资源定义（写一致性靶子） ----------------
// 字段无关深度比对：create 后用 API 返回的 data 对象与 DB row_to_json 做全字段递归比对，
// 排除自动生成字段（id/时间戳/软删标记），任何差异都记为 mismatch。
// updateFields: 指定更新时改哪些字段（验证更新落库）。
const AGENT_ID = 73
const RESOURCES = {
  knowledgeBase: {
    label: '知识库',
    createUrl: '/api/knowledge-bases',
    createBody: () => ({
      kb_code: 'DEEPTEST_' + Date.now(), type: 'rag',
      name: '深度测试知识库_' + Date.now(), description: 'deep-test-desc',
      owner_type: 'shared', enabled: true
    }),
    getUrl: (id) => `/api/knowledge-bases/${id}`,
    deleteUrl: (id) => `/api/knowledge-bases/${id}`,
    dbTable: 'knowledge_bases', dbIdCol: 'id', idType: 'int',
    updateFields: () => ({ name: 'DEEPTEST_UPD', description: 'deep-test-desc-UPD', enabled: false })
  },
  faq: {
    label: 'FAQ',
    createUrl: '/api/faqs',
    createBody: () => ({
      question: '深度测试问题_' + Date.now(), answer: '深度测试答案',
      keywords: ['test'], category: 'general', intent: 'test_intent',
      confidence: 0.9, enabled: true, agent_id: AGENT_ID
    }),
    getUrl: (id) => `/api/faqs/${id}`, deleteUrl: (id) => `/api/faqs/${id}`,
    dbTable: 'faq_entries', dbIdCol: 'id', idType: 'int', softDelete: false,
    updateFields: (b) => ({ question: b.question + '_UPD', answer: b.answer + '_UPD', enabled: false, keywords: ['test'], category: 'general', intent: 'test_intent', confidence: 0.9, agent_id: AGENT_ID })
  },
  sopTemplate: {
    label: 'SOP模板',
    createUrl: '/api/sop-templates',
    createBody: () => ({
      name: '深度测试SOP_' + Date.now(), intent: 'test_intent', stage: 'lead',
      template: '模板内容', vars: '{}', priority: 1, confidence: 0.8,
      enabled: true, agent_id: AGENT_ID
    }),
    getUrl: (id) => `/api/sop-templates/${id}`, deleteUrl: (id) => `/api/sop-templates/${id}`,
    dbTable: 'sop_templates', dbIdCol: 'id', idType: 'int',
    updateFields: (b) => ({ name: b.name + '_UPD', intent: b.intent, stage: b.stage, template: b.template + '_UPD', vars: b.vars, priority: b.priority, confidence: b.confidence, enabled: false, agent_id: AGENT_ID })
  },
  glossary: {
    label: '术语库',
    createUrl: '/api/glossaries',
    createBody: () => ({
      term_id: 'DEEPTEST_' + Date.now(), category: 'brand',
      preserve: false, translations: { zh: '深度测试术语' }, pattern: '', status: 'active'
    }),
    getUrl: (id) => `/api/glossaries/${id}`, deleteUrl: (id) => `/api/glossaries/${id}`,
    dbTable: 'glossaries', dbIdCol: 'term_id', idType: 'str', softDelete: true,
    idFromApi: (resp) => resp?.data?.term_id,
    updateFields: (b) => ({ category: b.category, preserve: b.preserve, translations: b.translations, pattern: b.pattern, status: 'inactive' })
  },
  scriptTemplate: {
    label: '话术模板',
    createUrl: '/api/script-templates',
    createBody: () => ({
      category: 'test', name: '深度测试话术_' + Date.now(), title: '标题',
      content: '话术内容', variables: [], tags: 'test', journey_stage: 'lead', created_by: AGENT_ID
    }),
    getUrl: (id) => `/api/script-templates/${id}`, deleteUrl: (id) => `/api/script-templates/${id}`,
    dbTable: 'script_templates', dbIdCol: 'id', idType: 'int', softDelete: false,
    updateFields: (b) => ({ category: b.category, name: b.name + '_UPD', title: b.title, content: b.content + '_UPD', variables: b.variables, tags: b.tags, journey_stage: b.journey_stage, created_by: AGENT_ID })
  },
  materialCategory: {
    label: '素材分类',
    createUrl: '/api/material/categories',
    createBody: () => ({
      name: '深度测试分类_' + Date.now(),
      type: 'image',
      icon: 'test-icon',
      color: '#FF0000',
      sort: 10,
      description: 'deep-test-category'
    }),
    getUrl: (id) => `/api/material/categories/${id}`,
    deleteUrl: (id) => `/api/material/categories/${id}`,
    dbTable: 'material_categories', dbIdCol: 'id', idType: 'str', softDelete: false,
    updateFields: (b) => ({
      name: b.name + '_UPD', type: b.type, icon: b.icon + '_U', color: '#00FF00',
      sort: b.sort + 1, description: b.description + '_UPD', status: 'active'
    })
  },
  domainPool: {
    label: '域名池',
    createUrl: '/api/domainpool',
    createBody: () => ({
      domain: 'deeptest-' + Date.now() + '.example.com',
      port: 8080,
      purpose: 'deep-test'
    }),
    getUrl: (id) => `/api/domainpool/${id}`,
    deleteUrl: (id) => `/api/domainpool/${id}`,
    dbTable: 'domain_pool', dbIdCol: 'id', idType: 'int', softDelete: false,
    // 以下为健康探测异步写入的监控列，非 CRUD 操作字段，跨格式(零时间/布尔)跳过
    skipKeys: ['last_check', 'blacklist_at', 'switched_at'],
    updateFields: (b, id) => ({
      id: Number(id), domain: b.domain, port: b.port + 1, purpose: b.purpose + '_UPD', status: 2
    })
  }
}

// 深度比对：API 对象 vs DB 行，排除自动字段，递归比较
const SKIP_KEYS = new Set(['id', 'created_at', 'updated_at', 'deleted_at', 'created_by', 'createdat', 'updatedat'])
function deepCmp(apiObj, dbObj, prefix = '', extraSkip = []) {
  const diffs = []
  if (!apiObj || typeof apiObj !== 'object') return diffs
  for (const k of Object.keys(apiObj)) {
    if (SKIP_KEYS.has(k.toLowerCase()) || (extraSkip && extraSkip.includes(k))) continue
    const av = apiObj[k]
    const dv = dbObj?.[k]
    // 跳过对象/数组嵌套（如 translations JSONB 已平铺）
    if (av !== null && typeof av === 'object') {
      // DB 可能以 JSON 字符串形式存储（text 列），解析后比对
      let dvObj = dv
      if (typeof dv === 'string') { try { dvObj = JSON.parse(dv) } catch { /* 忽略异常：失败时保持既有状态，不打断用户 */ } }
      if (JSON.stringify(av) !== JSON.stringify(dvObj || null)) diffs.push({ field: prefix + k, api: av, db: dv })
      continue
    }
    // API 数组/对象 vs DB JSON 字符串：尝试解析等价
    if (Array.isArray(av) || (typeof av === 'object' && av !== null)) {
      let dvObj = dv
      if (typeof dv === 'string') { try { dvObj = JSON.parse(dv) } catch { /* 忽略异常：失败时保持既有状态，不打断用户 */ } }
      if (JSON.stringify(av) !== JSON.stringify(dvObj || null)) diffs.push({ field: prefix + k, api: av, db: dv })
      continue
    }
    if (norm(av) !== norm(dv)) diffs.push({ field: prefix + k, api: norm(av), db: norm(dv) })
  }
  return diffs
}

// 通用写一致性验证
async function verifyWrite(token, resKey) {
  const R = RESOURCES[resKey]
  const result = { resource: resKey, steps: [], mismatches: [], errors: [] }
  const body = R.createBody()
  // 1. create
  const c = await apiReq(token, 'POST', R.createUrl, body)
  if (c.status >= 400 || !c.json?.data) {
    result.errors.push({ step: 'create', status: c.status, body: c.text.slice(0, 400) })
    return result
  }
  const created = c.json.data?.data || c.json.data
  const id = R.idFromApi ? R.idFromApi(c.json) : (R.idType === 'str' ? c.json.data?.id : c.json.data?.id)
  result.steps.push({ step: 'create', status: c.status, id })
  if (id === undefined || id === null) { result.errors.push({ step: 'create', note: '无法解析 id' }); return result }

  // 2. DB 比对 create 落库
  const where = R.softDelete ? `${R.dbIdCol}='${id}' AND deleted_at IS NULL` : (R.idType === 'str' ? `${R.dbIdCol}='${id}'` : `${R.dbIdCol}=${id}`)
  const row = psqlRows(`SELECT row_to_json(t) AS r FROM ${R.dbTable} t WHERE ${where}`)
  const dbObj = Array.isArray(row) && row[0] && !row[0].error ? row[0] : null
  if (!dbObj) { result.errors.push({ step: 'create-db', note: `DB 未找到 id=${id}` }) }
  else {
    const d = deepCmp(created, dbObj, '', R.skipKeys)
    if (d.length) result.mismatches.push({ step: 'create', diffs: d })
    result.steps.push({ step: 'create-db', checked: Object.keys(created).length, diffs: d.length })
  }

  // 3. update
  const updBody = R.updateFields(body, id)
  const upd = await apiReq(token, 'PUT', R.getUrl(id), updBody)
  if (upd.status >= 400) {
    result.errors.push({ step: 'update', status: upd.status, body: upd.text.slice(0, 400) })
  } else {
    const urow = psqlRows(`SELECT row_to_json(t) AS r FROM ${R.dbTable} t WHERE ${where}`)
    const udb = Array.isArray(urow) && urow[0] && !urow[0].error ? urow[0] : null
    if (udb) {
      const d = deepCmp(updBody, udb, '', R.skipKeys)
      if (d.length) result.mismatches.push({ step: 'update', diffs: d })
      result.steps.push({ step: 'update-db', checked: Object.keys(updBody).length, diffs: d.length, dbSnapshot: pick(udb, Object.keys(updBody)) })
    }
  }

  // 4. delete
  const del = await apiReq(token, 'DELETE', R.deleteUrl(id))
  result.steps.push({ step: 'delete', status: del.status })
  let remaining
  if (R.softDelete) {
    const cnt = psqlScalar(`SELECT count(*) FROM ${R.dbTable} WHERE ${R.dbIdCol}='${id}' AND deleted_at IS NULL`)
    remaining = cnt && !cnt.error ? Number(cnt) : -1
    result.steps.push({ step: 'delete-db-soft', activeRemaining: remaining })
  } else {
    const cnt = psqlScalar(`SELECT count(*) FROM ${R.dbTable} WHERE ${R.dbIdCol}=${R.idType === 'str' ? `'${id}'` : id}`)
    remaining = cnt && !cnt.error ? Number(cnt) : -1
    result.steps.push({ step: 'delete-db', remaining })
  }
  if (remaining !== 0) result.mismatches.push({ step: 'delete', note: `删除后 DB 残留 ${remaining} 行 (softDelete=${!!R.softDelete})` })

  return result
}

function pick(obj, keys) { const o = {}; for (const k of keys) o[k] = obj?.[k]; return o }
function norm(v) { if (v === null || v === undefined) return ''; if (typeof v === 'boolean') return v ? 'true' : 'false'; return String(v).trim() }

// ---------------- 层A：读一致性（保留，单页） ----------------
async function openPage(browser, pagePath) {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  const xhr = []
  page.on('response', async (r) => {
    const u = r.url()
    if (!u.includes('/api/')) return
    const req = r.request()
    let reqBody = null
    try { reqBody = req.method() !== 'GET' ? JSON.parse(req.postData() || 'null') : Object.fromEntries(new URL(u).searchParams) } catch { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
    let resJson = null
    try { resJson = await r.json() } catch { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
    xhr.push({ url: u.replace(BASE, ''), method: req.method(), status: r.status(), reqBody, resJson })
  })
  const CANDIDATES = [process.env.ADMIN_PASS || 'Admin@123456', 'Admin@12345678', 'Admin@123456', '62cfdc6bf1b075830734cc6f9a63501b']
  await page.goto(`${BASE}/#/login`, { waitUntil: 'domcontentloaded', timeout: 20000 }).catch(() => {})
  await page.waitForSelector('.login-box', { timeout: 15000 }).catch(() => {})
  for (const pw of CANDIDATES) {
    await page.locator('.login-box input[type="text"]').first().fill('admin').catch(() => {})
    await page.locator('.login-box input[type="password"]').fill(pw).catch(() => {})
    await page.locator('.login-box button.el-button--primary').click().catch(() => {})
    try { await page.waitForURL((u) => !u.hash.includes('/login'), { timeout: 6000 }); break } catch { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
    await page.waitForTimeout(300)
  }
  await page.goto(`${BASE}/#/${pagePath.replace(/^\//, '')}`, { waitUntil: 'networkidle', timeout: 25000 }).catch(() => {})
  await page.waitForTimeout(2500)
  return { ctx, page, xhr }
}

async function runReadPage(browser, pagePath) {
  const report = { page: pagePath, readDiffs: [], notes: [] }
  const { ctx, page, xhr } = await openPage(browser, pagePath)
  try {
    const tableRows = await page.evaluate(() => {
      const grids = Array.from(document.querySelectorAll('.el-table:not([style*="display: none"]), table:not([style*="display: none"]):not(.el-date-table):not(.el-calendar-table)'))
      for (const g of grids) {
        const rows = Array.from(g.querySelectorAll('.el-table__body-wrapper tbody tr, tbody tr'))
        if (rows.length) {
          const headers = Array.from(g.querySelectorAll('thead th, tr.el-table__header th')).map((th) => (th.innerText || '').replace(/\s+/g, ' ').trim())
          return rows.map((tr) => { const obj = {}; Array.from(tr.querySelectorAll('td')).forEach((td, i) => { obj[headers[i] || `col${i}`] = (td.innerText || '').replace(/\s+/g, ' ').trim() }); return obj })
        }
      }
      return []
    })
    const lists = xhr.filter((x) => x.method === 'GET' && x.status === 200 && x.resJson &&
      (Array.isArray(x.resJson.data) || (x.resJson.data && Array.isArray(x.resJson.data.list)) || (x.resJson.data && Array.isArray(x.resJson.data.items))))
    report.frontendRows = tableRows.length
    report.listApis = lists.map((l) => l.url)
    report.notes.push(tableRows.length ? 'has-rows' : 'no-rows')
    if (tableRows.length && lists.length) {
      for (const api of lists) {
        const apiRows = Array.isArray(api.resJson.data) ? api.resJson.data : (api.resJson.data.list || api.resJson.data.items || [])
        // 弱比对：前端每行是否都能在 API 行中找到（按任意单元格值包含）
        let notFound = 0
        for (const fr of tableRows) {
          const vals = Object.values(fr).map((v) => String(v).replace(/\s+/g, '').toLowerCase()).filter((v) => v.length >= 2)
          const hit = apiRows.some((ar) => {
            const av = Object.values(ar).map((v) => String(v ?? '').replace(/\s+/g, '').toLowerCase())
            return vals.some((v) => av.some((a) => a.includes(v) || v.includes(a)))
          })
          if (!hit) notFound++
        }
        if (notFound > 0) report.readDiffs.push({ api: api.url, frontendRows: tableRows.length, apiRows: apiRows.length, frontendRowsNotFoundInApi: notFound })
      }
    }
  } finally { await ctx.close() }
  return report
}

// ---------------- 主流程 ----------------
async function main() {
  const argv = process.argv.slice(2)
  let single = null, phase = 'ab'
  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === '--page') single = argv[++i]
    else if (argv[i] === '--phase') phase = argv[++i]
  }
  const token = await apiLogin().catch((e) => { console.error('login fail', e.message); process.exit(1) })

  if (phase === 'b' || phase === 'ab') {
    const writeResults = []
    for (const key of Object.keys(RESOURCES)) {
      const r = await verifyWrite(token, key)
      const bad = r.errors.length || r.mismatches.length
      process.stdout.write(`WRITE ${key} errs=${r.errors.length} mismatches=${r.mismatches.length} ${bad ? 'FLAGGED' : 'OK'}\n`)
      if (bad) fs.writeFileSync(path.resolve(OUT_DIR, 'write_' + key + '.json'), JSON.stringify(r, null, 2))
      writeResults.push({ resource: key, errors: r.errors.length, mismatches: r.mismatches.length, detail: r })
    }
    fs.writeFileSync(path.resolve(OUT_DIR, 'write-summary.json'), JSON.stringify(writeResults, null, 2))
  }

  if (phase === 'a' || phase === 'ab') {
    let pages
    if (single) pages = [single]
    else {
      const repDir = path.resolve(__dirname, 'reports')
      pages = fs.readdirSync(repDir).filter((f) => f.endsWith('.json') && f !== 'summary.json' && f !== 'progress.log')
        .map((f) => { try { return JSON.parse(fs.readFileSync(path.resolve(repDir, f), 'utf8')).page } catch { return null } }).filter(Boolean)
    }
    const browser = await chromium.launch({ headless: true })
    const readResults = []
    for (const p of pages) {
      const r = await runReadPage(browser, p)
      if (r.readDiffs.length) {
        fs.writeFileSync(path.resolve(OUT_DIR, 'read_' + p.replace(/[^a-zA-Z0-9_-]/g, '_') + '.json'), JSON.stringify(r, null, 2))
        process.stdout.write(`READ ${p} DIFFS=${r.readDiffs.length}\n`)
      }
      readResults.push({ page: p, diffs: r.readDiffs.length })
    }
    await browser.close()
    fs.writeFileSync(path.resolve(OUT_DIR, 'read-summary.json'), JSON.stringify(readResults, null, 2))
  }
  process.stdout.write('\nDONE\n')
}

main().catch((e) => { console.error('FATAL', e); process.exit(1) })
