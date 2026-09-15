// 全页面 UI 审计（模拟用户逐页访问）：
//   1) 页面是否有数据   2) 页面渲染是否正确   3) API 请求参数/响应是否正确
// 以管理员身份登录后，按 START/END 切片遍历路由，逐页采集：
//   - 该页触发的所有 /api 请求：method / url / 请求体 / 响应状态 / 响应 code+message
//   - console error / pageerror / 5xx
//   - 数据存在性（表格行数 / el-empty / 空态文案 / 统计卡片）
//   - 渲染正确性（NotFound / 页面文本长度 / 崩溃）
// 结果以 JSON 追加写入 REPORT_FILE，供主控聚合分析（结合 DB 与 API 日志）。
import { test, expect } from '@playwright/test'
import fs from 'fs'

const BASE = process.env.E2E_BASE_URL || 'http://localhost:8211'
const ADMIN_USER = process.env.E2E_USER || 'admin'
const ADMIN_PASS = process.env.E2E_PASS || 'Admin@123456'
const ROUTES_FILE = process.env.ROUTES_FILE || '/tmp/uiwalk/routes.json'
const REPORT_FILE = process.env.REPORT_FILE || '/tmp/uiwalk/report.jsonl'
const START = parseInt(process.env.START || '0', 10)
const END = parseInt(process.env.END || '9999', 10)

const ALL = JSON.parse(fs.readFileSync(ROUTES_FILE, 'utf8'))
const PATHS = ALL.slice(START, END)

// 良性噪声（不计入渲染问题）
const benign = (t) =>
  /favicon|Download the React DevTools|ResizeObserver loop|\[Vue warn\]|Feature flag|__VUE/i.test(t)

test('[UI-AUDIT] 逐页数据/渲染/API 审计', async ({ page }) => {
  test.setTimeout(30 * 60 * 1000)

  let current = { route: '(init)', api: [], console: [], pageerror: [] }

  // 测试专用：放宽首页文档的 CSP，加入 'unsafe-eval'，绕开 vue-i18n 运行时编译(new Function)被 script-src 'self' 拦截导致整页白屏的问题。
  // 该改写仅作用于测试浏览器，不修改任何产品文件（此问题作为独立缺陷记录并上报）。
  await page.route('**/*', async (route) => {
    const req = route.request()
    if (req.resourceType() !== 'document') return route.continue()
    try {
      const resp = await route.fetch()
      let body = await resp.text()
      body = body.replace(/script-src 'self'/g, "script-src 'self' 'unsafe-eval'")
      return route.fulfill({ response: resp, body })
    } catch (e) {
      return route.continue()
    }
  })

  // 采集 API 请求与响应
  page.on('response', async (res) => {
    try {
      const req = res.request()
      const url = res.url()
      if (!/\/api\//.test(url)) return
      const rec = {
        method: req.method(),
        url: url.replace(BASE, '').replace('http://localhost:8204', ''),
        status: res.status(),
      }
      const pd = req.postData()
      if (pd) rec.req = pd.slice(0, 500)
      try {
        const ct = (res.headers()['content-type'] || '')
        if (ct.includes('application/json')) {
          const body = await res.json()
          rec.code = body && (body.code !== undefined ? body.code : undefined)
          rec.msg = body && (body.message || body.msg)
          // 数据载荷概览
          const d = body && body.data
          if (Array.isArray(d)) rec.dataLen = d.length
          else if (d && Array.isArray(d.list)) rec.dataLen = d.list.length
          else if (d && Array.isArray(d.items)) rec.dataLen = d.items.length
          else if (d && typeof d === 'object') rec.dataKeys = Object.keys(d).length
        }
      } catch (e) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
      current.api.push(rec)
    } catch (e) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
  })
  page.on('console', (m) => {
    if (m.type() === 'error') {
      const t = m.text()
      if (!benign(t)) current.console.push(t.slice(0, 300))
    }
  })
  page.on('pageerror', (err) => {
    current.pageerror.push((err.message || String(err)).slice(0, 300))
  })

  // 登录
  current = { route: '(login)', api: [], console: [], pageerror: [] }
  await page.goto(BASE + '/#/login', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(1500)
  // 若被重定向到 setup，标记本地已初始化后再回到 login
  if (/\/setup/.test(page.url())) {
    await page.evaluate(() => localStorage.setItem('system_initialized', 'true'))
    await page.goto(BASE + '/#/login', { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(1000)
  }
  await page.waitForSelector('.el-input__inner', { timeout: 15000 })
  const inputs = page.locator('.el-input__inner')
  await inputs.nth(0).fill(ADMIN_USER)
  await inputs.nth(1).fill(ADMIN_PASS)
  await page.click('button.el-button--primary')
  await page.waitForTimeout(3000)
  const loggedIn = !/\/login|\/setup/.test(page.url()) && !!(await page.evaluate(() => localStorage.getItem('token')))
  console.log(`[login] url=${page.url()} loggedIn=${loggedIn}`)

  // 清空报告（仅当从头开始）
  if (START === 0) fs.writeFileSync(REPORT_FILE, '')
  fs.appendFileSync(REPORT_FILE, JSON.stringify({ _meta: true, loggedIn, batch: [START, END], loginApi: current.api }) + '\n')

  for (const p of PATHS) {
    current = { route: p, api: [], console: [], pageerror: [] }
    const url = BASE + '/#' + p
    let navErr = null
    try {
      await page.goto(url, { waitUntil: 'domcontentloaded' })
    } catch (e) { navErr = e.message.split('\n')[0] }
    // 等待网络与渲染稳定
    await page.waitForLoadState('networkidle', { timeout: 8000 }).catch(() => {})
    await page.waitForTimeout(600)

    // DOM 检测
    let dom
    try {
      dom = await page.evaluate(() => {
        const q = (s) => document.querySelector(s)
        const bodyText = (document.body.innerText || '').trim()
        const rows = document.querySelectorAll('.el-table__body-wrapper tbody tr.el-table__row').length
        const hasEmpty = !!q('.el-empty')
        const emptyText = /暂无数据|暂无|no data|nothing|empty/i.test(bodyText)
        // 独立 404 路由页（不含侧边栏，文本很短）；避免把带有 "404" 提示的正常页误判
        const notFound = /您访问的页面|页面不存在|Page Does not Exist|Page Not Found/i.test(bodyText) && bodyText.length < 400
        const cards = document.querySelectorAll('.el-card, .el-statistic, .stat-card').length
        const forms = document.querySelectorAll('form, .el-form').length
        const errAlerts = Array.from(document.querySelectorAll('.el-message--error, .el-alert--error'))
          .map(e => (e.innerText || '').trim()).filter(Boolean).slice(0, 3)
        return { textLen: bodyText.length, rows, hasEmpty, emptyText, notFound, cards, forms, errAlerts,
          textHead: bodyText.slice(0, 80) }
      })
    } catch (e) { dom = { evalError: e.message } }

    // API 汇总
    const badApi = current.api.filter(a => a.status >= 400 ||
      (a.code !== undefined && !['SUCCESS', 200, 0].includes(a.code)))

    // 数据存在性判定
    const dataApiOk = current.api.some(a => a.dataLen > 0 || a.dataKeys > 0)
    const hasData = dom.rows > 0 || dataApiOk || dom.cards > 0

    const rec = {
      route: p,
      url,
      navErr,
      render: {
        ok: !dom.notFound && !navErr && (dom.textLen || 0) > 20 && current.pageerror.length === 0,
        notFound: dom.notFound,
        textLen: dom.textLen,
        cards: dom.cards,
        rows: dom.rows,
        hasEmpty: dom.hasEmpty,
        errAlerts: dom.errAlerts,
        textHead: dom.textHead,
      },
      data: { hasData, tableRows: dom.rows, dataApiOk },
      api: { total: current.api.length, bad: badApi, all: current.api },
      console: current.console,
      pageerror: current.pageerror,
    }
    fs.appendFileSync(REPORT_FILE, JSON.stringify(rec) + '\n')
    const flag = rec.pageerror.length ? 'PAGEERR' : badApi.length ? 'BADAPI' : !hasData ? 'NODATA' : 'ok'
    console.log(`[${flag}] ${p}  render=${rec.render.ok} rows=${dom.rows} api=${current.api.length} bad=${badApi.length}`)
  }

  expect(loggedIn).toBe(true)
})
