import { print as _print } from '../../scripts/lib/stdout.cjs';
// 二次诊断：对「启用但点击超时」的按钮用 force 点击，判断功能是否真的可用
// （能开对话框/无报错 => 仅 Playwright 可操作性被遮罩拦截，人工可点，非 bug；
//   force 后仍报错/对话框异常 => 真 bug）。
import { test, expect as _expect } from '@playwright/test'

test.skip(!process.env.E2E_ADMIN_PASSWORD, 'E2E_ADMIN_PASSWORD not set')

const BASE = process.env.E2E_BASE_URL || 'http://localhost:8211'
const ADMIN_USER = 'admin'
const ADMIN_PASS = process.env.E2E_ADMIN_PASSWORD

const CASES = [
  ['llmRouting/list', '新增映射'],
  ['sopAgent/list', '查询'],
  ['system/rag-product-config', '添加规则'],
  ['system/rag-product-config', '保存配置'],
  ['tagSegmentation/list', '新增规则'],
  ['whatsapp/group-messaging', '新建模板']
]

test('[DIAG2] force 点击判定功能可用性', async ({ page }) => {
  const errs = []
  page.on('console', (m) => { if (m.type() === 'error') errs.push('CONSOLE: ' + m.text()) })
  page.on('pageerror', (e) => errs.push('PAGEERROR: ' + e.message))

  await page.goto(BASE + '/#/login', { waitUntil: 'networkidle' })
  await page.fill('input[type="text"]', ADMIN_USER)
  await page.fill('input[type="password"]', ADMIN_PASS)
  await page.click('button[type="button"].el-button--primary')
  await page.waitForURL('**/#/**', { timeout: 15000 }).catch(() => {})
  await page.waitForTimeout(1200)

  for (const [route, label] of CASES) {
    const url = BASE + '/#' + (route.startsWith('/') ? route : '/' + route)
    await page.goto(url, { waitUntil: 'domcontentloaded' }).catch(() => {})
    await page.waitForTimeout(700)
    const beforeErr = errs.length
    let opened = false
    let forceErr = ''
    try {
      await page.locator('button.el-button', { hasText: label }).first().click({ force: true, timeout: 3000 })
      await page.waitForTimeout(800)
      opened = await page.locator('.el-dialog:visible, .el-drawer:visible').count() > 0
      // 关闭可能弹出的对话框
      const dlg = page.locator('.el-dialog:visible, .el-drawer:visible').first()
      if (await dlg.count()) {
        await dlg.locator('.el-dialog__headerbtn, .el-drawer__headerbtn').first().click({ timeout: 1500 }).catch(() => {})
        await page.waitForTimeout(300)
      }
    } catch (e) {
      forceErr = e.message.split('\n')[0]
    }
    const newErrs = errs.slice(beforeErr)
    _print(`[DIAG2] ${route} :: "${label}" => openedDialog=${opened} forceErr=${forceErr || 'none'} newErrors=${newErrs.length ? newErrs.join(' | ') : 'none'}`)
  }
})
