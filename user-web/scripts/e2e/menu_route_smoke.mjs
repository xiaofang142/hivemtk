// 真实浏览器验收：手写菜单树 → 路由自动生成 + 页面可达 + 菜单标题多语言
// 无后端环境：只种本地会话（token / user_info / system_initialized），API 失败不影响路由与菜单渲染。
// 注意：addInitScript 每次导航都会重放，所以每种语言必须各自开一个 context 种各自的 app_locale，
// 不能在同一个 page 上 evaluate 改 locale（会被下一次导航的 init script 覆盖回 zh）。
//
// 前置：起 dev server（npm run dev）；本机 chromium 版本与 playwright 不匹配时用 CHROME_PATH
// 指到已装的 chrome-headless-shell.exe。
// 用法：CHROME_PATH=... node scripts/e2e/menu_route_smoke.mjs
import { chromium } from '@playwright/test'
import { writeFileSync, mkdirSync } from 'node:fs'
import { dirname, join } from 'node:path'
import { fileURLToPath } from 'node:url'

const HERE = dirname(fileURLToPath(import.meta.url))
const OUT_DIR = join(HERE, '.out')
const BASE = process.env.BASE || 'http://localhost:8211'

const SAMPLE = [
  '/cap/matrix',            // 本次报障：能力矩阵（白名单指向不存在的 capMatrix 模块）
  '/mcp/credentials',       // 本次报障：MCP 凭证（缺 menu.mcpCredentials 译文）
  '/tiktok-card-stats/9',   // 本次报障：首段无映射 → 404
  '/clue/list',
  '/customerService/csat',
  '/geo-tools/alerts',
  '/browser-automation/tasks',
  '/browser-automation/cron',
  '/knowledge/management',
  '/analytics/cohort-path',
  '/system/users',
  '/asset-market/sync-log',
  '/sales-workbench',
  '/followups/today',
  '/whatsapp-cloud',
  '/i18n/dashboard',
]

const browser = await chromium.launch(
  process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {}
)

async function withLocale(locale, paths, fn) {
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } })
  const page = await ctx.newPage()
  const consoleErrors = []
  page.on('console', (m) => { if (m.type() === 'error') consoleErrors.push(m.text()) })
  page.on('pageerror', (e) => consoleErrors.push('PAGEERR ' + e.message))
  await page.addInitScript((loc) => {
    localStorage.setItem('token', 'e2e-fake-token')
    localStorage.setItem('user_info', JSON.stringify({ id: 1, username: 'e2e_admin', role: 'admin' }))
    localStorage.setItem('system_initialized', 'true')
    localStorage.setItem('app_locale', loc)
  }, locale)

  const read = () => page.evaluate(() => {
    const text = document.body?.innerText || ''
    return {
      notFound: text.includes('页面不存在') || text.includes('Page Does not Exist'),
      topMenus: [...document.querySelectorAll('.top-menu .el-menu-item')].map((n) => n.innerText.trim()),
      aside: [...document.querySelectorAll('.app-aside .el-menu-item, .app-aside .el-sub-menu__title')].map((n) => n.innerText.trim()),
      activeLeaf: document.querySelector('.app-aside .el-menu-item.is-active')?.innerText.trim() || '',
      breadcrumb: [...document.querySelectorAll('.breadcrumb-wrap .el-breadcrumb__inner')].map((n) => n.innerText.trim()),
      rawKeys: (text.match(/menu\.[a-zA-Z]+/g) || []).slice(0, 5),
      hash: location.hash,
    }
  })

  const out = []
  for (const path of paths) {
    consoleErrors.length = 0
    await page.goto(BASE + '/#' + path, { waitUntil: 'domcontentloaded', timeout: 20000 })
    await page.waitForTimeout(1200)
    out.push({ path, ...(await read()), consoleErrors: consoleErrors.slice(0, 3) })
  }
  const result = await fn(page, out)
  await ctx.close()
  return result
}

const zh = await withLocale('zh', SAMPLE, async (page, rows) => {
  for (const r of rows) {
    console.log(`${r.notFound ? '❌页面不存在' : '✅'} ${r.path.padEnd(32)} active="${r.activeLeaf}" 裸key=${JSON.stringify(r.rawKeys)} 面包屑=[${r.breadcrumb.join(' / ')}]`)
  }
  return { rows }
})
console.log('\n[zh] 顶栏分区:', JSON.stringify(zh.rows[0].topMenus))
const capZh = zh.rows.find((r) => r.path === '/cap/matrix')
console.log('[zh] 系统设置侧栏条目数:', capZh.aside.length, '含「能力矩阵」:', capZh.aside.includes('能力矩阵'))

const en = await withLocale('en', ['/cap/matrix', '/clue/list', '/geo-tools/alerts'], async (page, rows) => {
  return { row: rows[0], all: rows }
})
console.log('\n[en] 顶栏:', JSON.stringify(en.row.topMenus))
console.log('[en] 面包屑:', JSON.stringify(en.row.breadcrumb))
console.log('[en] 侧栏含 Capability Matrix:', en.row.aside.includes('Capability Matrix'))
console.log('[en] 侧栏是否含汉字:', /[\u4e00-\u9fff]/.test(en.row.aside.join(' ')))
console.log('[en] 线索页面包屑:', JSON.stringify(en.all[1].breadcrumb))

const ja = await withLocale('ja', ['/cap/matrix', '/clue/list'], async (page, rows) => ({ row: rows[0], clue: rows[1] }))
console.log('\n[ja] 顶栏:', JSON.stringify(ja.row.topMenus))
console.log('[ja] 面包屑:', JSON.stringify(ja.row.breadcrumb))
console.log('[ja] 侧栏含 機能マトリクス:', ja.row.aside.includes('機能マトリクス'))

const de = await withLocale('de', ['/clue/list'], async (page, rows) => ({ row: rows[0] }))
console.log('\n[de] 面包屑:', JSON.stringify(de.row.breadcrumb))
console.log('[de] 侧栏是否含汉字:', /[\u4e00-\u9fff]/.test(de.row.aside.join(' ')))

const ru = await withLocale('ru', ['/clue/list'], async (page, rows) => ({ row: rows[0] }))
console.log('[ru] 面包屑:', JSON.stringify(ru.row.breadcrumb))

const failed = zh.rows.filter((r) => r.notFound)
const rawKey = zh.rows.filter((r) => r.rawKeys.length)
console.log('\n===== 汇总 =====')
console.log(`zh 页面 ${zh.rows.length} 个：页面不存在 ${failed.length}，裸 menu.* key ${rawKey.length}`)
if (failed.length) console.log('失败:', failed.map((f) => f.path))
if (rawKey.length) console.log('裸 key:', rawKey.map((f) => `${f.path}:${f.rawKeys}`))

mkdirSync(OUT_DIR, { recursive: true })
writeFileSync(join(OUT_DIR, 'menu-e2e-report.json'), JSON.stringify({ zh: zh.rows, en: en.all, ja: ja.row }, null, 2))
await browser.close()
process.exit(failed.length || rawKey.length ? 1 : 0)
