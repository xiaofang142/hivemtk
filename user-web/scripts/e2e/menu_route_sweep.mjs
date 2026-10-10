// 全量路由浏览器巡检：把 modules/*.js 里所有静态路径（去掉 :param / 重定向）在真实浏览器里走一遍，
// 断言不出现「页面不存在」、不出现裸 menu.* key。无后端也能跑（只种本地会话）。
// 前置：起 dev server（npm run dev）；chromium 版本不匹配时用 CHROME_PATH 指到已装的
// chrome-headless-shell.exe。
// 用法：CHROME_PATH=... node scripts/e2e/menu_route_sweep.mjs
import { chromium } from '@playwright/test'
import { readFileSync, readdirSync, writeFileSync, mkdirSync } from 'node:fs'
import { join, resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const HERE = dirname(fileURLToPath(import.meta.url))
const ROOT = resolve(HERE, '../..')
const OUT_DIR = join(HERE, '.out')
const BASE = process.env.BASE || 'http://localhost:8211'
const MOD_DIR = join(ROOT, 'src/router/modules')

const routes = []
const joinPath = (base, p) => (p.startsWith('/') ? p : `${base.replace(/\/$/, '')}/${p}`.replace(/\/{2,}/g, '/'))
for (const f of readdirSync(MOD_DIR).filter((n) => n.endsWith('.js'))) {
  const src = readFileSync(join(MOD_DIR, f), 'utf8')
  let mod
  try {
    mod = new Function(src.replace(/export\s+default/, 'return'))()
  } catch { continue }
  const walk = (list, base) => {
    for (const r of Array.isArray(list) ? list : [list]) {
      if (!r || typeof r.path !== 'string') continue
      const full = joinPath(base, r.path)
      if (!full.includes(':') && !full.includes('*') && (r.component || r.meta?.menuSection)) {
        routes.push({ path: full, file: f, hidden: !!r.meta?.menuHidden })
      }
      if (r.children) walk(r.children, full)
    }
  }
  walk(mod, '')
}
routes.sort((a, b) => a.path.localeCompare(b.path))

const browser = await chromium.launch(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {})
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } })
const page = await ctx.newPage()
const consoleErrors = []
page.on('console', (m) => { if (m.type() === 'error') consoleErrors.push(m.text()) })
page.on('pageerror', (e) => consoleErrors.push('PAGEERR ' + e.message))
await page.addInitScript(() => {
  localStorage.setItem('token', 'e2e-fake-token')
  localStorage.setItem('user_info', JSON.stringify({ id: 1, username: 'e2e_admin', role: 'admin' }))
  localStorage.setItem('system_initialized', 'true')
  localStorage.setItem('app_locale', 'zh')
})

const failed = []
const rawKey = []
let ok = 0
for (const r of routes) {
  consoleErrors.length = 0
  try {
    await page.goto(BASE + '/#' + r.path, { waitUntil: 'domcontentloaded', timeout: 15000 })
    await page.waitForTimeout(500)
    const info = await page.evaluate(() => {
      const text = document.body?.innerText || ''
      return { notFound: text.includes('页面不存在'), keys: (text.match(/menu\.[a-zA-Z]+/g) || []).slice(0, 3) }
    })
    if (info.notFound) failed.push({ path: r.path, file: r.file, consoleErrors: consoleErrors.slice(0, 2) })
    if (info.keys.length) rawKey.push({ path: r.path, keys: info.keys })
    if (!info.notFound && !info.keys.length) ok++
  } catch (e) {
    failed.push({ path: r.path, file: r.file, error: String(e.message).slice(0, 80) })
  }
  if ((ok + failed.length) % 25 === 0) console.log(`... 已扫 ${ok + failed.length}/${routes.length}`)
}

console.log('\n===== 全量路由巡检 =====')
console.log(`静态路由 ${routes.length} 条：通过 ${ok}，页面不存在 ${failed.length}，裸 menu.* key ${rawKey.length}`)
if (failed.length) { console.log('❌ 失败：'); failed.forEach((f) => console.log(`  ${f.path} (${f.file}) ${f.error || ''}`)) }
if (rawKey.length) { console.log('裸 key：'); rawKey.forEach((f) => console.log(`  ${f.path} ${f.keys}`)) }
mkdirSync(OUT_DIR, { recursive: true })
writeFileSync(join(OUT_DIR, 'menu-sweep-report.json'), JSON.stringify({ total: routes.length, ok, failed, rawKey }, null, 2))
await browser.close()
process.exit(failed.length || rawKey.length ? 1 : 0)
