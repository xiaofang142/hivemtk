// 线上（或任意环境）登录态全页面巡检：找出"显示异常"的页面。
// 逐页收集：控制台错误、页面异常、error toast、API 4xx/5xx、正文长度、
// 以及可疑渲染文本（undefined / NaN / [object Object] / 裸 i18n key / 空页）。
//
// 用法（密码从环境变量取，不回显）：
//   $env:BASE='https://api.xjznpt.com'; $env:ADMIN_USER='admin'; $env:ADMIN_PASS='***'
//   node scripts/e2e/ui_anomaly_sweep.mjs
import { chromium } from '@playwright/test'
import { readFileSync, readdirSync, writeFileSync, mkdirSync } from 'node:fs'
import { join, resolve, dirname } from 'node:path'
import { fileURLToPath } from 'node:url'

const HERE = dirname(fileURLToPath(import.meta.url))
const ROOT = resolve(HERE, '../..')
const OUT_DIR = join(HERE, '.out')
const BASE = process.env.BASE || 'http://localhost:8211'
const USER = process.env.ADMIN_USER || 'admin'
const PASS = process.env.ADMIN_PASS || ''
const LIMIT = Number(process.env.LIMIT || '0')

if (!PASS && !process.env.SEED_SESSION) {
  console.error('缺少 ADMIN_PASS 环境变量（或设 SEED_SESSION=1 走本地种会话模式）')
  process.exit(2)
}

// ---- 收集静态路由（与 menu_route_sweep 同源）----
const routes = []
const joinPath = (b, p) => (p.startsWith('/') ? p : `${b.replace(/\/$/, '')}/${p}`.replace(/\/{2,}/g, '/'))
const MOD_DIR = join(ROOT, 'src/router/modules')
for (const f of readdirSync(MOD_DIR).filter((n) => n.endsWith('.js'))) {
  let mod
  try {
    mod = new Function(readFileSync(join(MOD_DIR, f), 'utf8').replace(/export\s+default/, 'return'))()
  } catch { continue }
  const walk = (list, base) => {
    for (const r of Array.isArray(list) ? list : [list]) {
      if (!r || typeof r.path !== 'string') continue
      const full = joinPath(base, r.path)
      if (!full.includes(':') && !full.includes('*') && (r.component || r.meta?.menuSection)) {
        routes.push(full)
      }
      if (r.children) walk(r.children, full)
    }
  }
  walk(mod, '')
}
const uniq = [...new Set(routes)].sort()
const list = LIMIT > 0 ? uniq.slice(0, LIMIT) : uniq

const browser = await chromium.launch(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {})
const ctx = await browser.newContext({ viewport: { width: 1600, height: 1000 }, ignoreHTTPSErrors: true })
const page = await ctx.newPage()

let consoleErrors = []
let pageErrors = []
let apiFails = []
page.on('console', (m) => { if (m.type() === 'error') consoleErrors.push(m.text().slice(0, 240)) })
page.on('pageerror', (e) => pageErrors.push(String(e.message).slice(0, 240)))
page.on('response', (r) => {
  const u = r.url()
  if (u.includes('/api/') && r.status() >= 400) apiFails.push(`${r.status()} ${u.replace(BASE, '')}`)
})

// ---- 登录（SEED_SESSION=1 时不登录，直接种本地会话：用于无后端环境排查纯前端渲染异常）----
if (process.env.SEED_SESSION) {
  await page.addInitScript(() => {
    localStorage.setItem('token', 'e2e-fake-token')
    localStorage.setItem('user_info', JSON.stringify({ id: 1, username: 'e2e_admin', role: 'admin' }))
    localStorage.setItem('system_initialized', 'true')
    localStorage.setItem('app_locale', 'zh')
  })
  console.log('[login] SEED_SESSION 模式：跳过登录，直接种会话')
} else {
  console.log(`[login] ${BASE} as ${USER}`)
  await page.goto(`${BASE}/#/login`, { waitUntil: 'domcontentloaded', timeout: 45000 })
  await page.waitForTimeout(2500)
  const inputs = page.locator('input')
  await inputs.nth(0).fill(USER)
  await page.locator('input[type="password"]').first().fill(PASS)
  await page.getByRole('button', { name: /登\s*录|Login|Sign in/ }).first().click()
  await page.waitForTimeout(5000)
  const afterLogin = await page.evaluate(() => ({ hash: location.hash, text: (document.body.innerText || '').slice(0, 120) }))
  if (afterLogin.hash.startsWith('#/login')) {
    console.error('[login] 失败，仍在登录页：', JSON.stringify(afterLogin))
    console.error('  console:', consoleErrors.slice(-5))
    console.error('  页面异常:', pageErrors.slice(-5))
    await browser.close()
    process.exit(3)
  }
  console.log(`[login] OK -> ${afterLogin.hash}`)
}

// ---- 逐页巡检 ----
const SUSPECT = /\b(undefined|NaN|\[object Object\])\b/
const RAW_KEY = /\b(menu|core|common|login|system|ab|permission|role)\.[a-zA-Z][a-zA-Z0-9_]{2,}\b/
const results = []
for (const path of list) {
  consoleErrors = []
  pageErrors = []
  apiFails = []
  const row = { path }
  try {
    await page.goto(`${BASE}/#${path}`, { waitUntil: 'domcontentloaded', timeout: 30000 })
    await page.waitForTimeout(900)
    const info = await page.evaluate(() => {
      const text = document.body?.innerText || ''
      return {
        bodyLen: text.trim().length,
        notFound: text.includes('页面不存在'),
        sample: text.replace(/\s+/g, ' ').slice(0, 160),
        toasts: [...document.querySelectorAll('.el-message--error')].map((n) => n.innerText.trim()).slice(0, 3),
        hasTable: !!document.querySelector('.el-table'),
        hasCard: !!document.querySelector('.el-card'),
        appChildren: document.querySelector('#app')?.children.length || 0,
      }
    })
    row.bodyLen = info.bodyLen
    row.notFound = info.notFound
    row.toasts = info.toasts
    row.hasTable = info.hasTable
    row.hasCard = info.hasCard
    row.appChildren = info.appChildren
    row.sample = info.sample
    row.consoleErrors = consoleErrors.slice(0, 4)
    row.pageErrors = pageErrors.slice(0, 3)
    row.apiFails = [...new Set(apiFails)].slice(0, 4)
    const suspects = (info.sample.match(SUSPECT) || []).concat(/undefined|NaN|\[object Object\]/.test(info.sample) ? ['(sample)'] : [])
    const rawKeys = (info.sample.match(RAW_KEY) || [])
    row.suspectText = [...new Set(suspects)]
    row.rawKeys = [...new Set(rawKeys)]
    row.blank = info.bodyLen < 120
    row.abnormal = info.notFound || row.blank || info.toasts.length > 0 || row.consoleErrors.length > 0 || row.pageErrors.length > 0 || row.rawKeys.length > 0 || row.apiFails.length > 0
  } catch (e) {
    row.error = String(e.message).slice(0, 120)
    row.abnormal = true
  }
  results.push(row)
  if (row.abnormal) {
    console.log(`⚠️  ${path.padEnd(42)} len=${String(row.bodyLen).padStart(5)} toast=${(row.toasts || []).length} cerr=${(row.consoleErrors || []).length} perr=${(row.pageErrors || []).length} api=${(row.apiFails || []).length} rawKey=${JSON.stringify(row.rawKeys || [])} suspect=${JSON.stringify(row.suspectText || [])}`)
  }
  if (results.length % 40 === 0) console.log(`... ${results.length}/${list.length}`)
}

const abnormal = results.filter((r) => r.abnormal)
const blank = abnormal.filter((r) => r.blank)
const notFound = abnormal.filter((r) => r.notFound)
const withRawKey = abnormal.filter((r) => (r.rawKeys || []).length)
const withToast = abnormal.filter((r) => (r.toasts || []).length)
const withCfErr = abnormal.filter((r) => (r.consoleErrors || []).length || (r.pageErrors || []).length)
const withApiFail = abnormal.filter((r) => (r.apiFails || []).length)

console.log('\n===== 汇总 =====')
console.log(`巡检 ${results.length} 页：异常 ${abnormal.length}`)
console.log(`  页面不存在 ${notFound.length} / 空页 ${blank.length} / error toast ${withToast.length} / 控制台或JS异常 ${withCfErr.length} / API 失败 ${withApiFail.length} / 裸 i18n key ${withRawKey.length}`)
if (withRawKey.length) { console.log('  裸 key 页面:'); withRawKey.forEach((r) => console.log(`    ${r.path} ${JSON.stringify(r.rawKeys)}`)) }
if (blank.length) { console.log('  空页:'); blank.forEach((r) => console.log(`    ${r.path} len=${r.bodyLen} sample=${r.sample}`)) }
if (notFound.length) { console.log('  404:'); notFound.forEach((r) => console.log(`    ${r.path}`)) }

mkdirSync(OUT_DIR, { recursive: true })
writeFileSync(join(OUT_DIR, 'ui-anomaly-report.json'), JSON.stringify({ base: BASE, total: results.length, abnormal }, null, 2))
console.log(`\n报告: scripts/e2e/.out/ui-anomaly-report.json`)
await browser.close()
