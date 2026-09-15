// 数据分析 7 页端到端回归测试（二次论证 / 二次执行测试）
// 用法: E2E_BASE_URL=http://localhost:8216 E2E_API_URL=http://localhost:8204 npx playwright test data-analysis.spec.js --workers=1
import { test, expect } from '@playwright/test'
import { readFileSync } from 'fs'

const BASE = process.env.E2E_BASE_URL || 'http://localhost:8216'
const API = process.env.E2E_API_URL || 'http://localhost:8204'
const ADMIN = { username: 'admin', password: 'Admin@12345678' }

// 数据分析菜单 7 页
const PAGES = [
  { name: 'AI产能', path: 'aiProductivity/list' },
  { name: '转化漏斗', path: 'conversionFunnel/list' },
  { name: '客户旅程大屏', path: 'customerJourney/dashboard' },
  { name: '营销数据大屏', path: 'dashboardScreen/list' },
  { name: '自定义报表', path: 'customReport/list' },
  { name: 'A/B测试', path: 'abExperiment/list' },
  { name: '流失预警', path: 'churnPrediction/list' }
]

const SKIP_TEXT = ['删除', '清空', '重置', '运行预测', '预测', 'logout', '退出', '登出', '日志', '登录', '刷新', 'reload']

let AUTH = null // { token, refreshToken }

async function apiLogin() {
  try {
    const resp = await fetch(API + '/api/auth/login', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(ADMIN)
    })
    const json = await resp.json()
    return json.data || {}
  } catch (e) {
    return null
  }
}

async function isSkip(btn) {
  const t = ((await btn.textContent().catch(() => '')) || '').trim().toLowerCase()
  return SKIP_TEXT.some((s) => t.includes(s.toLowerCase()))
}

async function fillAndSubmitAnyDialog(page) {
  const dialog = page.locator('.el-dialog:visible, .el-message-box:visible').first()
  if (!(await dialog.count())) return false
  const selects = dialog.locator('.el-select')
  const sc = await selects.count()
  for (let i = 0; i < sc; i++) {
    const sel = selects.nth(i)
    if (await sel.count()) {
      await sel.click({ timeout: 3000 }).catch(() => {})
      const opt = page.locator('.el-select-dropdown:visible .el-select-dropdown__item, .el-popper:visible .el-select-dropdown__item').first()
      await opt.click({ timeout: 3000 }).catch(() => {})
    }
  }
  const inputs = dialog.locator('input.el-input__inner, textarea.el-textarea__inner')
  const ic = await inputs.count()
  for (let i = 0; i < ic; i++) {
    const inp = inputs.nth(i)
    const val = await inp.inputValue().catch(() => '')
    if (!val) await inp.fill('e2e-auto-' + Date.now()).catch(() => {})
  }
  const checks = dialog.locator('.el-checkbox')
  const cc = await checks.count()
  for (let i = 0; i < cc; i++) {
    await checks.nth(i).click({ timeout: 2000 }).catch(() => {})
  }
  const submit = dialog.locator('button.el-button--primary').first()
  await submit.click({ timeout: 5000 }).catch(() => {})
  return true
}

test.describe('数据分析 7 页回归', () => {
  test.setTimeout(120000)

  test.beforeAll(async () => {
    // 优先从文件读取（最稳定），失败则尝试 API 登录
    try {
      const tok = readFileSync('/tmp/da_token.txt', 'utf8').trim()
      if (tok) { AUTH = { token: tok }; return }
    } catch (e) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
    AUTH = await apiLogin()
    if (!AUTH || !AUTH.token) throw new Error('登录失败，无法获取 token')
  })

  let pageErrors, consoleErrors, failedRequests

  test.beforeEach(async ({ browser }, testInfo) => {
    const ctx = await browser.newContext()
    // 注入登录态，避免重复 UI 登录触发认证降级
    await ctx.addInitScript((auth) => {
      try {
        if (auth && auth.token) {
          localStorage.setItem('token', auth.token)
          if (auth.refreshToken) localStorage.setItem('refreshToken', auth.refreshToken)
          localStorage.setItem('system_initialized', 'true')
        }
      } catch (e) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
    }, AUTH)
    pageErrors = []
    consoleErrors = []
    failedRequests = []
    const page = await ctx.newPage()
    page.on('pageerror', (e) => pageErrors.push(String(e)))
    page.on('console', (msg) => {
      if (msg.type() === 'error') {
        const txt = msg.text() || ''
        if (txt.includes('frame-ancestors') || txt.includes('favicon')) return
        consoleErrors.push(txt)
      }
    })
    page.on('response', (res) => {
      if (res.status() >= 400) failedRequests.push(res.status() + ' ' + res.url())
    })
    testInfo.page = page
    testInfo.ctx = ctx
  })

  test.afterEach(async ({}, testInfo) => {
    if (testInfo.ctx) await testInfo.ctx.close().catch(() => {})
  })

  for (const p of PAGES) {
    test(`页面渲染+交互: ${p.name} (${p.path})`, async ({}, testInfo) => {
      const page = testInfo.page
      const cb = '?cb=' + Date.now()
      await page.goto(BASE + '/#/' + p.path + cb, { waitUntil: 'domcontentloaded' })
      if (page.url().includes('/#/login')) {
        await page.goto(BASE + '/#/' + p.path + cb, { waitUntil: 'domcontentloaded' })
      }
      await page.waitForSelector('.el-main', { timeout: 15000 })
      await page.waitForURL('**/#/' + p.path + '**', { timeout: 10000 })
      await page.waitForSelector('.el-loading-mask:not(.is-loading)', { timeout: 8000 }).catch(() => {})
      await page.waitForTimeout(1200)

      const mainHtml = await page.locator('.el-main').innerHTML()
      expect(mainHtml.length, '页面主区域为空').toBeGreaterThan(50)

      try {
        const tabs = page.locator('.el-tabs__item:visible')
        const tc = await tabs.count()
        for (let i = 0; i < tc; i++) {
          const t = page.locator('.el-tabs__item:visible').nth(i)
          await t.click({ timeout: 3000 }).catch(() => {})
          await page.waitForTimeout(300)
        }
      } catch (e) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }

      try {
        const buttons = page.locator('.el-main button.el-button:visible')
        const bc = await buttons.count()
        for (let i = 0; i < bc; i++) {
          const btn = page.locator('.el-main button.el-button:visible').nth(i)
          if ((await btn.count()) === 0) continue
          if (await btn.isDisabled().catch(() => true)) continue
          if (await isSkip(btn)) continue
          const text = (await btn.textContent().catch(() => '')) || ''
          if (!text.trim()) continue
          await btn.click({ timeout: 4000 }).catch(() => {})
          await page.waitForTimeout(400)
          await fillAndSubmitAnyDialog(page)
          await page.waitForTimeout(300)
          const close = page.locator('.el-dialog__headerbtn:visible, .el-message-box__headerbtn:visible').first()
          if (await close.count()) await close.click({ timeout: 2000 }).catch(() => {})
          await page.waitForTimeout(200)
        }
      } catch (e) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }

      await page.goto(BASE + '/#/' + p.path + '?cb=' + Date.now(), { waitUntil: 'domcontentloaded' })
      if (page.url().includes('/#/login')) {
        await page.goto(BASE + '/#/' + p.path + '?cb=' + Date.now(), { waitUntil: 'domcontentloaded' })
      }
      await page.waitForSelector('.el-main', { timeout: 15000 })
      await page.waitForSelector('.el-loading-mask:not(.is-loading)', { timeout: 8000 }).catch(() => {})
      await page.waitForTimeout(800)
      const reHtml = await page.locator('.el-main').innerHTML()
      expect(reHtml.length, '刷新后主区域为空（keep-alive 陈旧）').toBeGreaterThan(50)

      expect(pageErrors, 'pageerror: ' + pageErrors.join(' | ')).toEqual([])
      expect(consoleErrors, 'console.error: ' + consoleErrors.join(' | ')).toEqual([])
      const real4xx = failedRequests.filter((r) => !r.startsWith('401 '))
      expect(real4xx, 'HTTP>=400(非401): ' + real4xx.join(' | ')).toEqual([])
    })
  }
})
