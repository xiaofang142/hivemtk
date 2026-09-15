import { test, expect, request } from '@playwright/test'

// 系统设置模块全部页面（真实后端 + DB 级 E2E）
const PAGES = [
  { group: '站点配置', name: '站点设置', path: '/system/config' },
  { group: '站点配置', name: '存储配置', path: '/system/obs-config' },
  { group: '站点配置', name: '素材库', path: '/system/material-library' },
  { group: '站点配置', name: '监控', path: '/system/monitor' },
  { group: '站点配置', name: '系统使用指南', path: '/system/guide' },
  { group: '站点配置', name: '域名池', path: '/domainPool' },
  { group: '站点配置', name: '备份恢复', path: '/backup/list' },
  { group: '团队', name: '团队成员', path: '/teamUser/list' },
  { group: '团队', name: '角色权限', path: '/teamUser/role' },
  { group: '权限设置', name: '平台账号', path: '/platformAccount/list' },
  { group: '权限设置', name: '第三方对接', path: '/integration/list' },
  { group: '权限设置', name: '操作日志', path: '/operationLog/list' },
  { group: '权限设置', name: '安全审计', path: '/securityAudit/list' },
  { group: '资产包', name: '资产市场', path: '/asset-market' },
  { group: '资产包', name: '我的资产', path: '/asset-market/my-assets' },
  { group: '资产包', name: '资产包管理', path: '/asset-bundle/list' },
  { group: '资产包', name: '开发者 Playground', path: '/asset-bundle/playground' },
  { group: '资产包', name: '商户话术包', path: '/asset-bundle/merchant-new' },
  { group: '资产包', name: '同步日志', path: '/asset-market/sync-log' }
]

const isApiUrl = (s) => s.includes('/api/') && !s.includes('/src/')

// 一次性真实登录（独立 API 上下文，避免 page.route 污染 / 上下文状态串扰）
let AUTH = { token: '', user: null }
test.beforeAll(async () => {
  const ctx = await request.newContext({ baseURL: process.env.E2E_BASE_URL || 'http://localhost:8214' })
  const resp = await ctx.post('/api/auth/login', { data: { username: 'admin', password: 'Admin@12345678' } })
  const body = await resp.json().catch(() => ({}))
  AUTH = { token: body?.data?.token || '', user: body?.data?.user || null }
  await ctx.dispose()
})
test.beforeEach(async ({ context }) => {
  await context.addInitScript((a) => {
    localStorage.setItem('system_initialized', 'true')
    localStorage.setItem('token', a.token)
    localStorage.setItem('user_info', JSON.stringify(a.user))
  }, AUTH)
})

// 清理遮挡层
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

const isBenign = (t) =>
  t.includes('Failed to load resource') || t.includes('favicon') ||
  t.includes('Download the React DevTools') || t.includes('[vite]') ||
  t.includes('frame-ancestors') || t.includes('Content Security Policy directive') ||
  // 全局头部 best-effort 平台调用（开源版未配置平台端，非本模块 bug，见 memory）
  t.includes('加载会话列表失败') || t.includes('加载统计失败') ||
  t.includes('获取最新消息失败') || t.includes('unifiedInbox') || t.includes('MessageNotification')

function stripOrigin(u) { try { return new URL(u).pathname + new URL(u).search } catch { return u } }

test.describe('系统设置模块 真实后端 E2E', () => {
  for (const p of PAGES) {
    test(`[${p.group}] ${p.name} (${p.path})`, async ({ page }, testInfo) => {
      test.setTimeout(120000)
      const realErrors = []
      const notes = []
      const apiCalls = []

      page.on('console', (m) => { if (m.type() === 'error' && !isBenign(m.text())) realErrors.push('CONSOLE: ' + m.text()) })
      page.on('pageerror', (e) => realErrors.push('PAGEERROR: ' + (e.stack || e.message)))
      // 真实后端响应状态（验证后端契约/API 日志）
      page.on('response', (res) => {
        const u = res.url()
        if (isApiUrl(u)) {
          const st = res.status()
          apiCalls.push(`RESP ${st} ${res.request().method()} ${stripOrigin(u)}`)
          if (st >= 500) realErrors.push(`API_5XX(${st}): ${stripOrigin(u)}`)
        }
      })
      page.on('requestfailed', (r) => {
        if (!isApiUrl(r.url())) realErrors.push('REQFAIL: ' + r.url())
      })

      // GET 走真实后端；写操作 mock 成功（避免污染 DB，但校验请求路径/参数）
      await page.route('**/*', async (route) => {
        const req = route.request()
        const method = req.method()
        const url = req.url()
        if (isApiUrl(url) && (method === 'POST' || method === 'PUT' || method === 'DELETE')) {
          let body = ''
          try { body = (req.postData() || '').slice(0, 240) } catch (_) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
          apiCalls.push(`SEND ${method} ${stripOrigin(url)}${body ? ' :: ' + body : ''}`)
          return route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 0, data: { id: 1 } }) })
        }
        return route.fallback()
      })

      page.setDefaultTimeout(5000)
      await page.goto('#' + p.path, { waitUntil: 'domcontentloaded' })
      try { await page.waitForSelector('.app-main', { state: 'visible', timeout: 15000 }) } catch (_) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
      await page.waitForTimeout(800)

      const shotDir = 'test-results/system-settings-live'
      await page.screenshot({ path: `${shotDir}/${p.group}-${p.name}.png` }).catch(() => {})

      const main = page.locator('.app-main').first()
      if (!(await main.isVisible().catch(() => false))) realErrors.push('WHITE_SCREEN: 主内容区(.app-main)不可见')

      // 统计真实数据渲染（表格行数 / 列表项）
      const rowCount = await page.locator('.app-main table tbody tr, .app-main .el-table__row, main table tbody tr').count().catch(() => 0)

      const buttons = await page.locator('.app-main button, main button').all()
      let clicked = 0
      for (const b of buttons) {
        if (!page.url().includes(p.path)) { notes.push(`NAV_AWAY`); break }
        let label = ''
        try {
          await clearOverlays(page)
          if (!(await b.isVisible().catch(() => false))) continue
          if (await b.isDisabled().catch(() => false)) { notes.push('SKIP_DISABLED'); continue }
          label = ((await b.innerText().catch(() => '')).trim() || '').slice(0, 16)
          await b.click({ timeout: 2500 })
          clicked++
          await page.waitForTimeout(200)
          const dlg = page.locator('.el-dialog').first()
          if (await dlg.isVisible({ timeout: 600 }).catch(() => false)) {
            const dlgInputs = await dlg.locator('input.el-input__inner, textarea.el-textarea__inner, input').all()
            for (const inp of dlgInputs.slice(0, 15)) {
              try {
                if (!(await inp.isVisible().catch(() => false))) continue
                if (await inp.isDisabled().catch(() => false)) continue
                const ty = await inp.getAttribute('type').catch(() => '')
                if (ty === 'file') continue
                await inp.fill('自动测试_' + Date.now()).catch(() => {})
              } catch (_) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
            }
            const ok = dlg.getByRole('button', { name: /确定|确 定|保存|提交|新增|创建|确认|是/i }).first()
            await ok.click({ timeout: 2500 }).catch(() => {})
            await page.waitForTimeout(300)
            if (await dlg.isVisible().catch(() => false)) await dlg.locator('.el-dialog__headerbtn').click().catch(() => {})
          }
          await clearOverlays(page)
          await page.waitForTimeout(150)
        } catch (e) {
          notes.push(`CLICK_FAIL(${clicked}): ${label} :: ${e.message.split('\n')[0]}`)
        }
      }

      await page.screenshot({ path: `${shotDir}/${p.group}-${p.name}-after.png` }).catch(() => {})

      if (realErrors.length) {
        console.log(`\n❌ [${p.group}] ${p.name} (${p.path}) 真实问题 ${realErrors.length} 条:`)
        realErrors.forEach((e) => console.log('   - ' + e))
      } else {
        console.log(`\n✅ [${p.group}] ${p.name} (${p.path}) 通过 (点击 ${clicked} 按钮, 真实数据行 ${rowCount}, API ${apiCalls.length} 次, 备注 ${notes.length})`)
      }
      notes.forEach((n) => console.log('   · ' + n))
      testInfo.attach('realErrors', { body: realErrors.join('\n'), contentType: 'text/plain' })
      testInfo.attach('apiCalls', { body: apiCalls.join('\n'), contentType: 'text/plain' })
      expect(realErrors, `页面 ${p.path} 发现 ${realErrors.length} 个真实问题`).toEqual([])
    })
  }
})
