import { test, expect } from '@playwright/test'
import path from 'path'

const BASE = process.env.E2E_BASE_URL || 'http://localhost:8211'
const STATE = path.resolve(process.cwd(), 'tests/.auth/user.json')

test.use({ baseURL: BASE, storageState: STATE })

const consoleErrors = []
const api5xx = []

test.beforeEach(async ({ page }) => {
  page.on('console', (m) => {
    if (m.type() === 'error') {
      const t = m.text()
      if (!/favicon|ERR_|Failed to load resource|Devtools|Content Security Policy|frame-ancestors|429|Too Many Requests|请求过于频繁|rate limit|Network Error|net::ERR/i.test(t)) {
        consoleErrors.push(t)
      }
    }
  })
  page.on('response', (res) => {
    if (res.url().includes('/api/') && res.status() >= 500) api5xx.push(res.status() + ' ' + res.url())
  })
})

test.afterAll(() => {
  expect(consoleErrors, '短信组功能测试控制台错误:\n' + consoleErrors.join('\n')).toEqual([])
  expect(api5xx, '短信组功能测试 API 5xx:\n' + api5xx.join('\n')).toEqual([])
})

async function openFirstDialog(page) {
  await page.locator('.el-button--primary').first().click()
  const dlg = page.locator('.el-dialog').first()
  await expect(dlg).toBeVisible()
  return dlg
}
async function closeDialog(page) {
  const close = page.locator('.el-dialog__close').first()
  if (await close.count()) await close.click()
  await page.waitForTimeout(200)
}

// 短信列表：发送短信对话框 + 手机号校验
test('短信列表 发送短信 对话框与校验', async ({ page }) => {
  await page.goto('/#/sms/list')
  await page.waitForTimeout(800)
  const dlg = await openFirstDialog(page) // 发送短信
  // 空手机号提交 => 警告
  await dlg.locator('.el-button--primary').click()
  await expect(page.locator('.el-message--warning').first()).toBeVisible()
  await expect(dlg).toBeVisible()
  // 填合法手机号+内容后提交 => 出现反馈(成功或失败 toast)
  await dlg.locator('input').first().fill('13800138000')
  await dlg.locator('textarea').first().fill('测试短信内容')
  await dlg.locator('.el-button--primary').click()
  await expect(page.locator('.el-message').first()).toBeVisible()
})

test('短信草稿 新建草稿对话框', async ({ page }) => {
  await page.goto('/#/sms/drafts')
  await page.waitForTimeout(800)
  await openFirstDialog(page)
  await closeDialog(page)
})

test('短信任务 创建任务对话框', async ({ page }) => {
  await page.goto('/#/sms/jobs')
  await page.waitForTimeout(800)
  await openFirstDialog(page)
  await closeDialog(page)
})

test('短信配置 保存按钮可点击(不崩溃)', async ({ page }) => {
  await page.goto('/#/sms/config')
  await page.waitForTimeout(800)
  await page.locator('.el-button--primary').first().click()
  await page.waitForTimeout(500)
  await expect(page.locator('.el-message').first()).toBeVisible()
})
