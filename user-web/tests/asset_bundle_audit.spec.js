import { test, expect, request } from '@playwright/test'
import fs from 'fs'

// 资产包全 UI 真实后端审计：逐个点击每个按钮，真实发起 API 调用（不 mock 写操作），
// 捕获 API 请求体、响应体、HTTP 5xx 以及应用层错误（code != 成功）和控制台错误、页面异常。
const PAGES = [
  { group: '资产包', name: '开发者 Playground', path: '/asset-bundle/playground', rounds: 1 },
  { group: '资产包', name: '商户话术包', path: '/asset-bundle/merchant-new', rounds: 1 },
  { group: '资产包', name: '资产包管理', path: '/asset-bundle/list', rounds: 2 },
  { group: '资产包', name: '资产市场', path: '/asset-market', rounds: 1, clickCards: true },
  { group: '资产包', name: '我的资产', path: '/asset-market/my-assets', rounds: 2 },
  { group: '资产包', name: '同步日志', path: '/asset-market/sync-log', rounds: 1 }
]

const isApiUrl = (s) => s.includes('/api/') && !s.includes('/src/')
const stripOrigin = (u) => { try { return new URL(u).pathname + new URL(u).search } catch { return u } }

// 导航类按钮（点击会离开当前页）在通用循环里跳过，避免 NAV_AWAY 提前 break；
// 这些目标页（Playground / 商户编辑 / 详情返回 / 市场互链）已在 PAGES 中单独覆盖。
const NAV_SKIP = ['Playground', '商户编辑', '返回', '去市场浏览', '我的资产', '同步日志', '去市场浏览', '查看']

// 已知良性业务冲突（非代码缺陷）：重复购买 / 未购买即同步 / 手动资产不支持同步上报 / 非法输入校验
const BENIGN_MSG = ['资产已存在', '请直接点击', '请先购买', '非平台购买资产', '拉取失败', '平台数据校验失败', 'already exists', '已存在', '无需上报']

const isBenignConsole = (t) =>
  t.includes('Failed to load resource') || t.includes('favicon') ||
  t.includes('Download the React DevTools') || t.includes('[vite]') ||
  t.includes('frame-ancestors') || t.includes('Content Security Policy directive')

// 后端成功码：字符串枚举("SUCCESS"/"OK")或数字 0/200
const isSuccessCode = (c) => c === 'SUCCESS' || c === 'OK' || c === 0 || c === '0' || c === 200 || c === '200'

const safeText = (p) => Promise.race([
  p.text().catch(() => ''),
  new Promise((r) => setTimeout(() => r(''), 1500))
])

let AUTH = { token: '', user: null }
test.beforeAll(async () => {
  const base = process.env.E2E_BASE_URL || 'http://localhost:8211'
  const ctx = await request.newContext({ baseURL: base })
  const candidates = ['Seed@123456', 'Admin@12345678', 'Admin@123456', '62cfdc6bf1b075830734cc6f9a63501b']
  for (const pw of candidates) {
    const resp = await ctx.post('/api/auth/login', { data: { username: 'admin', password: pw } })
    const body = await resp.json().catch(() => ({}))
    if (isSuccessCode(body?.code) && body?.data?.token) { AUTH = { token: body.data.token, user: body.data.user }; break }
  }
  await ctx.dispose()
  if (!AUTH.token) throw new Error('登录失败：所有候选密码均无效')
})

test.beforeEach(async ({ context }) => {
  await context.addInitScript((a) => {
    localStorage.setItem('system_initialized', 'true')
    localStorage.setItem('token', a.token)
    localStorage.setItem('user_info', JSON.stringify(a.user || {}))
  }, AUTH)
})

async function clearOverlays(page) {
  await page.evaluate(() => { document.querySelectorAll('.el-loading-mask').forEach((e) => e.remove()) }).catch(() => {})
  const mb = page.locator('.el-message-box').first()
  if (await mb.isVisible().catch(() => false)) {
    await mb.getByRole('button', { name: /确定|确 定|确认|是|OK|Yes/i }).first().click().catch(() => {})
  }
  const dlg = page.locator('.el-dialog').first()
  if (await dlg.isVisible().catch(() => false)) {
    await dlg.locator('.el-dialog__headerbtn').click().catch(() => {})
  }
}

async function fillForm(page) {
  const inputs = await page.locator('.el-dialog input.el-input__inner, .el-dialog textarea.el-textarea__inner, .el-dialog input').all()
  for (const inp of inputs.slice(0, 30)) {
    try {
      if (!(await inp.isVisible().catch(() => false))) continue
      if (await inp.isDisabled().catch(() => false)) continue
      const ty = await inp.getAttribute('type').catch(() => '')
      if (ty === 'file') continue
      const tag = (await inp.evaluate((e) => e.tagName).catch(() => '')).toUpperCase()
      if (tag === 'TEXTAREA') {
        const val = ((await inp.inputValue().catch(() => '')) || '').trim()
        if (val.startsWith('{') || val.startsWith('[')) continue // 保留合法 JSON 模板
        await inp.fill('自动测试_' + Date.now()).catch(() => {})
      } else {
        await inp.fill('自动测试_' + Date.now()).catch(() => {})
      }
    } catch (_) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
  }
}

// 每轮重新查询按钮，逐个点击，避免对话框开关导致 locator 失效（detached）。
async function clickAllButtons(page, realErrors, apiCalls, notes, skipLabels = []) {
  const clicked = new Set()
  const failed = new Set()
  for (let safety = 0; safety < 500; safety++) {
    await clearOverlays(page)
    if (!page.url().includes(page._auditPath)) break
    const btns = await page.locator('button, .el-button').all()
    const labelCounts = {}
    let acted = false
    for (const b of btns) {
      // key 在 try 内赋值、在 catch 内使用；必须提升到 try 之外，
      // 否则 catch 分支访问会抛 ReferenceError 并掩盖原始异常。
      let key
      try {
        if (!(await b.isVisible().catch(() => false))) continue
        if (await b.isDisabled().catch(() => false)) continue
        const label = ((await b.innerText().catch(() => '')).trim() || '').slice(0, 20)
        labelCounts[label] = (labelCounts[label] || 0) + 1
        key = label + '#' + labelCounts[label]
        if (skipLabels.some((s) => label.includes(s))) continue
        if (clicked.has(key) || failed.has(key)) continue
        clicked.add(key)
        if (!page.url().includes(page._auditPath)) break
        await b.click({ timeout: 3000 })
        await page.waitForTimeout(250)
        const dlg = page.locator('.el-dialog').first()
        if (await dlg.isVisible({ timeout: 700 }).catch(() => false)) {
          await fillForm(page)
          const ok = dlg.getByRole('button', { name: /确定|确 定|保存|提交|新增|创建|确认|是|关闭/i }).first()
          await ok.click({ timeout: 3000 }).catch(() => {})
          await page.waitForTimeout(300)
          if (await dlg.isVisible().catch(() => false)) await dlg.locator('.el-dialog__headerbtn').click().catch(() => {})
        }
        await clearOverlays(page)
        await page.waitForTimeout(120)
        acted = true
        break
      } catch (e) {
        failed.add(key)
        notes.push(`CLICK_FAIL(${key}): ${e.message.split('\n')[0]}`)
      }
    }
    if (!acted) break
  }
}

test.describe('资产包 UI 真实后端审计', () => {
  test('逐个页面点击所有按钮并校验 API 响应', async ({ page }, testInfo) => {
    test.setTimeout(600000)
    const realErrors = []
    const apiCalls = []
    const notes = []
    page._auditPath = ''
    page.setDefaultTimeout(8000)

    page.on('console', (m) => { if (m.type() === 'error' && !isBenignConsole(m.text())) realErrors.push('CONSOLE: ' + m.text().slice(0, 300)) })
    page.on('pageerror', (e) => {
      const m = (e.stack || e.message || '')
      if (BENIGN_MSG.some((b) => m.includes(b))) return
      realErrors.push('PAGEERROR: ' + m.slice(0, 300))
    })

    page.on('request', (req) => {
      const m = req.method()
      const u = req.url()
      if (isApiUrl(u) && ['POST', 'PUT', 'DELETE', 'PATCH'].includes(m)) {
        let body = ''
        try { body = (req.postData() || '').slice(0, 300) } catch (_) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
        apiCalls.push(`SEND ${m} ${stripOrigin(u)}${body ? ' :: ' + body : ''}`)
      }
    })

    page.on('response', async (res) => {
      const u = res.url()
      if (!isApiUrl(u)) return
      const m = res.request().method()
      const st = res.status()
      const body = await safeText(res)
      const short = (body || '').slice(0, 400)
      apiCalls.push(`RESP ${st} ${m} ${stripOrigin(u)} :: ${short}`)
      if (st >= 500) { realErrors.push(`API_5XX(${st}): ${stripOrigin(u)} :: ${short}`); return }
      try {
        const j = JSON.parse(body)
        if (j && typeof j === 'object' && j.code !== undefined && !isSuccessCode(j.code)) {
          const m = (j.msg || j.message || '')
          if (!BENIGN_MSG.some((b) => m.includes(b))) {
            realErrors.push(`API_ERR(code=${j.code}): ${stripOrigin(u)} :: ${m || short}`)
          }
        }
      } catch (_) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
    })

    page.on('requestfailed', (r) => { if (isApiUrl(r.url())) realErrors.push('REQFAIL: ' + stripOrigin(r.url()) + ' ' + (r.failure()?.errorText || '')) })

    for (const p of PAGES) {
      for (let r = 0; r < p.rounds; r++) {
        page._auditPath = p.path
        await page.goto('#' + p.path, { waitUntil: 'domcontentloaded' })
        try { await page.waitForSelector('.app-main', { state: 'visible', timeout: 15000 }) } catch (_) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
        await page.waitForTimeout(900)
        if (!(await page.locator('.app-main').first().isVisible().catch(() => false))) {
          realErrors.push(`WHITE_SCREEN: ${p.group}/${p.name} (${p.path})`)
          continue
        }
        await clickAllButtons(page, realErrors, apiCalls, notes, NAV_SKIP)
        if (p.clickCards) {
          const card = page.locator('.asset-card').first()
          if (await card.isVisible().catch(() => false)) {
            await card.click({ timeout: 4000 }).catch(() => {})
            await page.waitForTimeout(900)
            page._auditPath = page.url().split('#')[1] || p.path
            await clickAllButtons(page, realErrors, apiCalls, notes, ['返回']) // 详情页只点“免费试用”
            await page.goto('#' + p.path, { waitUntil: 'domcontentloaded' }).catch(() => {})
            await page.waitForTimeout(500)
          }
        }
      }
    }

    const report = [
      '===== API CALLS (' + apiCalls.length + ') =====', ...apiCalls,
      '', '===== NOTES (' + notes.length + ') =====', ...notes,
      '', '===== REAL ERRORS (' + realErrors.length + ') =====', ...realErrors
    ].join('\n')
    const outFile = 'test-results/asset_audit_' + Date.now() + '.txt'
    fs.writeFileSync(outFile, report)
    console.log(report)
    console.log('\n报告已写入: ' + outFile)

    await testInfo.attach('realErrors', { body: realErrors.join('\n'), contentType: 'text/plain' })
    if (realErrors.length) console.log('\n❌ 资产包审计发现 ' + realErrors.length + ' 个真实问题')
    else console.log('\n✅ 资产包审计通过，无真实错误')
    expect(realErrors, `资产包审计发现 ${realErrors.length} 个真实问题`).toEqual([])
  })
})
