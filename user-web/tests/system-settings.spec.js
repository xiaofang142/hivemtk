import { test, expect } from '@playwright/test'

// 系统设置模块全部页面（来自 docs/system-settings-e2e-checklist.md）
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

// 仅拦截真正的后端 API（host/api/...），务必排除 /src/api/* 源码模块，否则会把模块当 JSON 返回导致白屏
const isApiUrl = (url) => {
  const s = url.toString()
  return (s.includes('/api/') || s.includes('/upload/')) && !s.includes('/src/')
}

// 通用 API mock：让前端在无后端情况下也能渲染，重点暴露运行时崩溃/白屏/控制台错误
async function mockApi(route, request) {
  const url = request.url()
  const method = request.method()
  const json = (d) => route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 0, data: d }) })
  const emptyList = { list: [], total: 0, content: [], records: [], items: [] }

  if (url.includes('/api/auth/login')) return json({ token: 'mock-token', user: { id: 1, username: 'admin', role: 'admin' } })
  if (url.includes('/api/user/info') || url.includes('/api/auth/info') || url.includes('/api/auth/me') || url.includes('/api/auth/userInfo'))
    return json({ id: 1, username: 'admin', role: 'admin', email: 'admin@xapptool.cn' })
  if (url.includes('/api/system/menu') || url.includes('/api/menu')) return json([])
  if (url.includes('/api/system/init-status')) return json({ initialized: true, needSetup: false, adminExists: true, configComplete: true })

  if (method === 'GET') return json(emptyList)
  return json({ id: 1 })
}

async function setupMocks(page) {
  await page.route(isApiUrl, mockApi)
  await page.route('**/upload/**', (route) =>
    route.fulfill({ status: 200, contentType: 'application/json', body: JSON.stringify({ code: 0, data: { url: 'https://example.com/mock.png' } }) })
  )
}

// 清理遮挡层：移除全屏 loading 遮罩；对确认框点“确定”以真正触发删除/操作类 API；关闭残留弹窗
async function clearOverlays(page) {
  await page.evaluate(() => {
    document.querySelectorAll('.el-loading-mask').forEach((e) => e.remove())
  }).catch(() => {})
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
  t.includes('Failed to load resource') ||
  t.includes('favicon') ||
  t.includes('Download the React DevTools') ||
  t.includes('[vite]') ||
  t.includes('frame-ancestors') ||
  t.includes('Content Security Policy directive')

// 与后端契约核对：从捕获的请求里抽取 method + path
function stripOrigin(u) {
  try { return new URL(u).pathname + new URL(u).search } catch { return u }
}

test.describe('系统设置模块 E2E 覆盖', () => {
  test.beforeEach(async ({ context }) => {
    await context.addInitScript(() => {
      localStorage.setItem('system_initialized', 'true')
      localStorage.setItem('token', 'mock-token')
      localStorage.setItem('user_info', JSON.stringify({ id: 1, username: 'admin', role: 'admin', email: 'admin@xapptool.cn' }))
    })
  })

  for (const p of PAGES) {
    test(`[${p.group}] ${p.name} (${p.path})`, async ({ page }, testInfo) => {
      test.setTimeout(120000)
      const realErrors = []   // 真实缺陷：控制台 error / 页面异常 / 白屏
      const notes = []        // 交互备注
      const apiCalls = []     // 发出的 API 请求（证据）

      page.on('console', (m) => {
        if (m.type() === 'error' && !isBenign(m.text())) realErrors.push('CONSOLE: ' + m.text())
      })
      page.on('pageerror', (e) => realErrors.push('PAGEERROR: ' + (e.stack || e.message)))
      page.on('requestfailed', (r) => {
        if (!r.url().includes('/api/')) realErrors.push('REQFAIL: ' + r.url() + ' ' + (r.failure()?.errorText || ''))
      })
      page.on('request', (req) => {
        if (isApiUrl(req.url())) {
          const m = req.method()
          let body = ''
          try { body = (req.postData() || '').slice(0, 240) } catch (_) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
          apiCalls.push(`${m} ${stripOrigin(req.url())}${body ? ' :: ' + body : ''}`)
        }
      })

      await setupMocks(page)
      page.setDefaultTimeout(4000)
      await page.goto('#' + p.path, { waitUntil: 'domcontentloaded' })
      try {
        await page.waitForSelector('.app-main', { state: 'visible', timeout: 15000 })
      } catch (_) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
      await page.waitForTimeout(600)

      const shotDir = 'test-results/system-settings'
      await page.screenshot({ path: `${shotDir}/${p.group}-${p.name}.png` }).catch(() => {})

      const main = page.locator('.app-main').first()
      if (!(await main.isVisible().catch(() => false))) realErrors.push('WHITE_SCREEN: 主内容区(.app-main)不可见')

      // 模拟人工点击主内容区所有可见按钮，并真正操作弹出的对话框（新增/编辑/删除）
      const buttons = await page.locator('.app-main button, main button').all()
      let clicked = 0
      for (const b of buttons) {
        if (!page.url().includes(p.path)) { notes.push(`NAV_AWAY: 点击后离开 ${p.path}`); break }
        let label = ''
        try {
          await clearOverlays(page)
          if (!(await b.isVisible().catch(() => false))) continue
          if (await b.isDisabled().catch(() => false)) { notes.push(`SKIP_DISABLED: ${label || '(无文本)'}`); continue }
          label = ((await b.innerText().catch(() => '')).trim() || '').slice(0, 16)
          await b.click({ timeout: 2000 })
          clicked++
          await page.waitForTimeout(200)
          // 对话框：填表 → 提交 → 关闭
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
            await ok.click({ timeout: 2000 }).catch(() => {})
            await page.waitForTimeout(300)
            if (await dlg.isVisible().catch(() => false)) {
              await dlg.locator('.el-dialog__headerbtn').click().catch(() => {})
            }
          }
          await clearOverlays(page)
          await page.waitForTimeout(150)
        } catch (e) {
          notes.push(`CLICK_FAIL(${clicked}): ${label} :: ${e.message.split('\n')[0]}`)
        }
      }

      // 列表页主区表单输入填充（验证校验/提交路径不崩溃）
      if (page.url().includes(p.path)) {
        const inputs = await page.locator('.app-main input.el-input__inner, main input').all()
        for (const inp of inputs.slice(0, 20)) {
          try {
            await clearOverlays(page)
            if (!(await inp.isVisible().catch(() => false))) continue
            if (await inp.isDisabled().catch(() => false)) continue
            const ty = await inp.getAttribute('type').catch(() => '')
            if (ty === 'file') continue
            await inp.fill('测试输入_auto').catch(() => {})
          } catch (_) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
        }
      }

      await page.screenshot({ path: `${shotDir}/${p.group}-${p.name}-after.png` }).catch(() => {})

      if (realErrors.length) {
        console.log(`\n❌ [${p.group}] ${p.name} (${p.path}) 真实问题 ${realErrors.length} 条:`)
        realErrors.forEach((e) => console.log('   - ' + e))
      } else {
        console.log(`\n✅ [${p.group}] ${p.name} (${p.path}) 通过 (点击 ${clicked} 按钮, API ${apiCalls.length} 次, 备注 ${notes.length})`)
      }
      notes.forEach((n) => console.log('   · ' + n))
      testInfo.attach('realErrors', { body: realErrors.join('\n'), contentType: 'text/plain' })
      testInfo.attach('notes', { body: notes.join('\n'), contentType: 'text/plain' })
      testInfo.attach('apiCalls', { body: apiCalls.join('\n'), contentType: 'text/plain' })
      expect(realErrors, `页面 ${p.path} 发现 ${realErrors.length} 个真实问题`).toEqual([])
    })
  }
})
