// 线上验收（默认 https://api.xjznpt.com）：未登录面 —— 登录页、i18n、路由兜底、产物指纹
// 用法：cd user-web && CHROME_PATH=... node deploy-live-check.mjs（依赖本目录 node_modules 的 @playwright/test）
import { chromium } from '@playwright/test'

const BASE = process.env.BASE || 'https://api.xjznpt.com'
const browser = await chromium.launch(process.env.CHROME_PATH ? { executablePath: process.env.CHROME_PATH } : {})
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 }, ignoreHTTPSErrors: true })
const page = await ctx.newPage()
const consoleErrors = []
const pageErrors = []
page.on('console', (m) => { if (m.type() === 'error') consoleErrors.push(m.text().slice(0, 200)) })
page.on('pageerror', (e) => pageErrors.push(String(e.message).slice(0, 200)))

const out = { base: BASE, steps: [] }

// 1) 产物指纹
const ver = await page.request.get(`${BASE}/version.json`)
out.versionJson = await ver.json()
const idx = await page.request.get(`${BASE}/`)
const html = await idx.text()
out.entry = (html.match(/assets\/index-[A-Za-z0-9_-]+\.js/) || [])[0]
out.swRegistered = false

// 2) 登录页
await page.goto(`${BASE}/#/login`, { waitUntil: 'domcontentloaded', timeout: 30000 })
await page.waitForTimeout(3000)
out.login = await page.evaluate(() => ({
  title: document.title,
  hash: location.hash,
  userInput: !!document.querySelector('input[placeholder]'),
  placeholders: [...document.querySelectorAll('input')].map((i) => i.placeholder).filter(Boolean),
  registerLink: (document.body.innerText || '').includes('注册新账号'),
  errorToasts: [...document.querySelectorAll('.el-message--error')].map((n) => n.innerText.trim()),
  bodyLen: (document.body.innerText || '').length,
}))
out.loginConsoleErrors = [...consoleErrors]
out.loginPageErrors = [...pageErrors]
consoleErrors.length = 0
pageErrors.length = 0

// 3) 路由兜底：不存在的 hash 应显示「页面不存在」
await page.goto(`${BASE}/#/this-page-does-not-exist-xyz`, { waitUntil: 'domcontentloaded', timeout: 30000 })
await page.waitForTimeout(2500)
out.notFound = await page.evaluate(() => ({
  showsNotFound: (document.body.innerText || '').includes('页面不存在'),
  bodyLen: (document.body.innerText || '').length,
}))

// 4) 未登录访问受保护页 → 应跳登录
await page.goto(`${BASE}/#/clue/list`, { waitUntil: 'domcontentloaded', timeout: 30000 })
await page.waitForTimeout(2500)
out.guard = await page.evaluate(() => ({ hash: location.hash, hasLogin: (document.body.innerText || '').includes('欢迎回来') || !!document.querySelector('input[placeholder]') }))

// 5) 英文语言包（登录页 facet）
await page.goto(`${BASE}/#/login`, { waitUntil: 'domcontentloaded' })
await page.evaluate(() => localStorage.setItem('app_locale', 'en'))
await page.reload({ waitUntil: 'domcontentloaded' })
await page.waitForTimeout(2500)
out.enLogin = await page.evaluate(() => ({
  placeholders: [...document.querySelectorAll('input')].map((i) => i.placeholder).filter(Boolean),
  hasChinese: /[\u4e00-\u9fff]/.test((document.querySelector('.login-main')?.innerText) || ''),
}))

console.log(JSON.stringify(out, null, 2))
await browser.close()
