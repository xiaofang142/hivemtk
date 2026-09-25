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
  expect(consoleErrors, '邮件组功能测试控制台错误:\n' + consoleErrors.join('\n')).toEqual([])
  expect(api5xx, '邮件组功能测试 API 5xx:\n' + api5xx.join('\n')).toEqual([])
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

// 邮件列表：发送邮件对话框 + 空主题校验 + 关闭
test('邮件列表 发送邮件 对话框与校验', async ({ page }) => {
  await page.goto('/#/email')
  await page.waitForTimeout(800)
  const dlg = await openFirstDialog(page) // 发送邮件
  // 空主题提交 => 校验警告 + 对话框不关闭
  await dlg.locator('.el-button--primary').click()
  await expect(page.locator('.el-message--warning').first()).toBeVisible()
  await expect(dlg).toBeVisible()
  await closeDialog(page)
  await expect(page.locator('.el-dialog').first()).toBeHidden()
})

// 邮件列表：存在数据时删除按钮弹出确认框（取消，不真正删除）
test('邮件列表 删除 弹出确认框可取消', async ({ page }) => {
  await page.goto('/#/email')
  await page.waitForTimeout(800)
  const del = page.locator('.el-table__row .el-button--danger').first()
  if (await del.count()) {
    await del.click()
    const box = page.locator('.el-message-box').first()
    await expect(box).toBeVisible()
    await box.locator('.el-button--default').click() // 取消
    await expect(box).toBeHidden()
  }
})

test('邮件草稿 新建对话框', async ({ page }) => {
  await page.goto('/#/email/drafts')
  await page.waitForTimeout(800)
  await openFirstDialog(page)
  await closeDialog(page)
})

test('邮件任务 列表渲染(只读统计页)', async ({ page }) => {
  await page.goto('/#/email/jobs')
  await page.waitForTimeout(800)
  await expect(page.locator('.el-table').first()).toBeVisible()
})

test('邮件账号 添加账号对话框', async ({ page }) => {
  await page.goto('/#/email/smtp')
  await page.waitForTimeout(800)
  await openFirstDialog(page)
  await closeDialog(page)
})
