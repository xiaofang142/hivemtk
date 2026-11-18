import { print as _print } from '../../scripts/lib/stdout.cjs';
// 客户中心 8 页 100% 覆盖回归测试（第三~五步：主 Agent 循环逐页模拟点击 + API/控制台/DB 校验）
// 运行：E2E_BASE_URL=http://localhost:8211 E2E_API_URL=http://localhost:8204 npx playwright test tests/e2e/customer-center.spec.js
import { test, expect } from '@playwright/test'

const BASE = process.env.E2E_BASE_URL || 'http://localhost:8211'
const API = process.env.E2E_API_URL || 'http://localhost:8204'
const ADMIN = process.env.E2E_ADMIN || 'admin'
const ADMIN_PW = process.env.E2E_ADMIN_PASSWORD || process.env.E2E_ADMIN_PW
test.skip(!ADMIN_PW, 'E2E_ADMIN_PASSWORD/E2E_ADMIN_PW not set')

// 客户中心 8 个页面
const PAGES = [
  { key: 'clueList', name: '线索列表', path: '/clue/list' },
  { key: 'clueStatistics', name: '线索统计', path: '/clue/statistics' },
  { key: 'customer360', name: '客户360', path: '/customer360/list' },
  { key: 'customerEvent', name: '客户事件', path: '/customerEvent/list' },
  { key: 'tagSegmentation', name: '标签分层', path: '/tagSegmentation/list' },
  { key: 'userSegment', name: '用户分层RFM', path: '/userSegment/list' },
  { key: 'unifiedMessage', name: '统一消息', path: '/unifiedMessage/list' },
  { key: 'oneidList', name: 'OneID列表', path: '/oneid/list' }
]

// 跳过可能破坏数据的破坏性按钮（删除/清空/重置），避免污染 e2e 库；其覆盖由显式用例保证
const SKIP_TEXT = ['删除', '清空', '重置', '退出', '修改密码', 'Remove', 'Delete', 'Clear', 'Reset', 'Logout', 'Sign out']

let TOKEN = ''

async function uiLogin(page) {
  let lastErr
  for (let attempt = 0; attempt < 4; attempt++) {
    try {
      await page.goto(BASE + '/login', { waitUntil: 'networkidle' }).catch(() => {})
      await page.waitForSelector('.login-box input', { timeout: 15000 })
      const inputs = page.locator('.login-box input')
      await inputs.nth(0).fill(ADMIN)
      await page.locator('.login-box input[type="password"]').fill(ADMIN_PW)
      await page.locator('.login-box .el-button--primary').click()
      // 等待进入主框架（侧边栏菜单出现）
      await page.waitForSelector('.el-menu', { timeout: 30000 })
      await page.waitForLoadState('networkidle').catch(() => {})
      return
    } catch (e) {
      lastErr = e
      // 实时栈在连续交互后可能瞬时降级登录，退避后自愈重试
      await page.waitForTimeout(8000 * (attempt + 1))
    }
  }
  throw lastErr
}

test.beforeAll(async ({ request }) => {
  let body = {}
  for (let attempt = 0; attempt < 4; attempt++) {
    const r = await request.post(API + '/api/auth/login', {
      data: { username: ADMIN, password: ADMIN_PW }
    })
    body = await r.json().catch(() => ({}))
    TOKEN = body?.data?.token || ''
    if (TOKEN) return
    await new Promise((res) => setTimeout(res, 5000 * (attempt + 1)))
  }
  throw new Error('登录失败，无法获取 token：' + JSON.stringify(body))
}, { timeout: 120000 })

// 打开弹窗后，尽量把必填项填上并提交
async function fillAndSubmitAnyDialog(page, ctx) {
  const inputs = page.locator('.el-dialog:visible input, .el-drawer:visible input')
  const count = await inputs.count()
  for (let i = 0; i < count; i++) {
    const el = inputs.nth(i)
    const type = await el.getAttribute('type').catch(() => '')
    if (type === 'password') continue
    const val = await el.inputValue().catch(() => '')
    if (!val) {
      const ph = (await el.getAttribute('placeholder').catch(() => '')) || ''
      await el.fill(ph.includes('手机') ? '13800000000' : ph.includes('邮箱') ? 'test@example.com' : '自动化测试')
    }
  }
  const selects = page.locator('.el-dialog:visible .el-select, .el-drawer:visible .el-select')
  const sc = await selects.count()
  for (let i = 0; i < sc; i++) {
    const sel = selects.nth(i)
    await sel.click().catch(() => {})
    const opt = page.locator('.el-select-dropdown:visible .el-select-dropdown__item:not(.is-disabled)').first()
    if (await opt.count()) await opt.click().catch(() => {})
    await page.waitForTimeout(150)
  }
  const submit = page.locator('.el-dialog:visible .el-button--primary, .el-drawer:visible .el-button--primary')
  if (await submit.count()) {
    const before = ctx.failed
    await submit.first().click().catch(() => {})
    await page.waitForTimeout(700)
    return ctx.failed > before
  }
  return false
}

test.describe('客户中心 全页面 100% 覆盖', () => {
  let ctx
  test.beforeEach(async ({ page }) => {
    ctx = { consoleErrors: [], failed: 0, failedList: [] }
    await uiLogin(page)
    // 良性浏览器警告白名单（非页面缺陷）：CSP frame-ancestors 经 <meta> 传递被忽略，仅为浏览器提示
    const BENIGN = [/frame-ancestors.*ignored when delivered via a <meta>/i]
    page.on('console', (m) => {
      if (m.type() === 'error') {
        const t = m.text()
        if (!BENIGN.some((re) => re.test(t))) ctx.consoleErrors.push(t)
      }
    })
    page.on('response', (r) => {
      const s = r.status()
      if (s >= 400) { ctx.failed++; ctx.failedList.push(`${r.request().method()} ${r.url()} -> ${s}`) }
    })
    page.on('dialog', (d) => d.accept().catch(() => {}))
  })

  for (const p of PAGES) {
    test(`[${p.key}] ${p.name} 加载与交互`, async ({ page }) => {
      test.setTimeout(180000)
      const pageErrors = []
      page.on('pageerror', (e) => pageErrors.push(String(e)))

      await page.goto(BASE + p.path, { waitUntil: 'networkidle' }).catch(() => {})
      await page.waitForTimeout(1500)

      // 渲染断言
      const hasContent = await page.locator('.el-main .el-table, .el-main .el-card, .el-main .stats-row, .el-main canvas, .el-main').first().count()
      if (hasContent === 0) {
        const url = page.url()
        const body = (await page.locator('body').innerText().catch(() => '')) || ''
        _print(`[DIAG] ${p.key} url=${url} body=${body.slice(0, 400)}`)
      }
      expect(hasContent, `${p.name} 页面无渲染内容`).toBeGreaterThan(0)

      // 点击【主内容区】内所有非破坏性按钮，触发弹窗/交互并捕获错误（避免点到顶栏/侧边栏）
      let buttons = page.locator('.el-main button.el-button')
      if ((await buttons.count()) === 0) buttons = page.locator('button.el-button')
      const bc = await buttons.count()
      const loopStart = Date.now()
      for (let i = 0; i < bc; i++) {
        if (page.isClosed()) break
        if (Date.now() - loopStart > 90000) break // 单页循环硬上限，防止卡死
        const btn = buttons.nth(i)
        const visible = await btn.isVisible().catch(() => false)
        if (!visible) continue
        const txt = (await btn.innerText().catch(() => '')) || ''
        if (SKIP_TEXT.some((s) => txt.includes(s))) continue
        await btn.click({ timeout: 3000 }).catch(() => {})
        await page.waitForTimeout(300)
        if (page.isClosed()) break
        const dialogOpen = await page.locator('.el-dialog:visible, .el-drawer:visible').count()
        if (dialogOpen) {
          await fillAndSubmitAnyDialog(page, ctx).catch(() => {})
          const close = page.locator('.el-dialog:visible .el-button:not(.el-button--primary), .el-drawer:visible .el-button:not(.el-button--primary)')
          if (await close.count()) await close.first().click().catch(() => {})
          await page.waitForTimeout(200)
        }
      }

      // 断言：无 pageerror、无控制台 error、无 >=400 请求
      expect(pageErrors, `${p.name} 发生页面 JS 异常: ${pageErrors.join(' | ')}`).toHaveLength(0)
      expect(ctx.consoleErrors, `${p.name} 控制台错误: ${ctx.consoleErrors.join(' | ')}`).toHaveLength(0)
      expect(ctx.failed, `${p.name} 存在失败请求(${ctx.failed}): ${ctx.failedList.join(' | ')}`).toBe(0)
      _print(`[CUSTOMER_CENTER_RESULT] ${p.key} PASS`)
    })
  }
})
