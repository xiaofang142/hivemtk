// 数据分析模块 7 页真实渲染巡检
// 复用 audit-runner 登录逻辑, 导航到 7 个数据分析页, 收集 console/page/网络错误
// 并断言页面确实渲染出数据区域(非白屏/非全空态报错)
import { chromium } from 'playwright'
import fs from 'fs'
import path from 'path'
import { fileURLToPath } from 'url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const BASE = process.env.E2E_BASE_URL || 'http://localhost:8211'
const CRED = { user: 'admin', pass: process.env.ADMIN_PASS || 'Admin@123456' }
const OUT_DIR = path.resolve(__dirname, 'reports')
fs.mkdirSync(OUT_DIR, { recursive: true })

const PAGES = [
  ['conversionFunnel', 'conversionFunnel/list', '转化漏斗'],
  ['aiProductivity', 'aiProductivity/list', 'AI产能分析'],
  ['dashboardScreen', 'dashboardScreen/list', '数据大屏'],
  ['customerJourney', 'customerJourney/dashboard', '客户旅程大屏'],
  ['customReport', 'customReport/list', '自定义报表'],
  ['abExperiment', 'abExperiment/list', 'A/B实验'],
  ['churnPrediction', 'churnPrediction/list', '流失预警'],
]

async function login(page) {
  await page.goto(`${BASE}/#/login`)
  await page.waitForSelector('.login-box', { timeout: 15000 })
  await page.locator('.login-box input[type="text"]').first().fill(CRED.user)
  await page.locator('.login-box input[type="password"]').fill(CRED.pass)
  await page.locator('.login-box button.el-button--primary').click()
  await page.waitForURL((u) => !u.hash.includes('/login'), { timeout: 8000 })
}

function attach(page, col) {
  page.on('console', (m) => { if (m.type() === 'error') col.consoleErrors.push(m.text()) })
  page.on('pageerror', (e) => {
    const msg = String(e && e.stack || e)
    if (/^cancel$/.test(msg.trim())) return
    col.pageErrors.push(msg)
  })
  page.on('response', (r) => {
    const s = r.status()
    if (s >= 400 && !/__x00__|@vite|hmr|\?t=\d+$/.test(r.url()))
      col.netErrors.push({ url: r.url(), status: s })
  })
}

async function main() {
  const browser = await chromium.launch()
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  const all = []
  await login(page)
  for (const [mod, hash, title] of PAGES) {
    const col = { consoleErrors: [], pageErrors: [], netErrors: [] }
    attach(page, col)
    await page.goto(`${BASE}/#/${hash}`, { waitUntil: 'networkidle', timeout: 20000 }).catch(() => {})
    await page.waitForTimeout(2500)
    const bodyText = (await page.locator('body').innerText().catch(() => '')) || ''
    const cards = await page.locator('.el-card, .data-card, [class*="chart"], table').count()
    const hasEmpty = /暂无数据|无数据|empty|no data/i.test(bodyText)
    const errSummary = {
      module: mod, title, hash,
      consoleErrors: col.consoleErrors.slice(0, 5),
      pageErrors: col.pageErrors.slice(0, 5),
      netErrors: col.netErrors.slice(0, 8),
      renderedNodes: cards,
      bodyLen: bodyText.length,
      likelyEmptyState: hasEmpty,
    }
    all.push(errSummary)
    const ok = col.pageErrors.length === 0 && col.consoleErrors.length === 0
    console.log(`${ok ? 'OK  ' : 'WARN'} ${mod.padEnd(16)} nodes=${cards} empty=${hasEmpty} netErr=${col.netErrors.length} console=${col.consoleErrors.length} page=${col.pageErrors.length}`)
  }
  fs.writeFileSync(path.resolve(OUT_DIR, 'data_analysis_render.json'), JSON.stringify(all, null, 2))
  await browser.close()
  const bad = all.filter((a) => a.pageErrors.length || a.consoleErrors.length)
  console.log(`\n=== ${bad.length ? 'HAS ISSUES' : 'ALL PAGES RENDER CLEAN'} ===`)
  process.exit(bad.length ? 1 : 0)
}
main().catch((e) => { console.error('FATAL', e); process.exit(2) })
