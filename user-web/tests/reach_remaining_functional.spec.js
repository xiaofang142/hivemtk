import { test, expect } from '@playwright/test'
import path from 'path'

const BASE = process.env.E2E_BASE_URL || 'http://localhost:8211'
const STATE = path.resolve(process.cwd(), 'tests/.auth/user.json')

// 剩余触达运营页面（邮件/短信已单独覆盖）：逐页点击主 CTA，验证不崩溃、无 5xx
const PAGES = [
  '/douyinCard', '/douyin/stats',
  '/kuaishouCard', '/kuaishou/stats',
  '/xiaohongshuCard', '/xiaohongshu/stats',
  '/xianyuCard', '/xianyu/stats',
  '/tiktok/list', '/tiktok/stats',
  '/whatsapp/account', '/whatsapp/drafts', '/whatsapp/jobs',
  '/telegram/account', '/feishu/account',
  '/community/list', '/shortLink', '/shortLink/stats', '/livecode'
]

test.use({ baseURL: BASE, storageState: STATE })

const consoleErrors = []
const apiFail = [] // >=400

test.beforeEach(async ({ page }) => {
  page.on('console', (m) => {
    if (m.type() === 'error') {
      const t = m.text()
      if (!/favicon|ERR_|Failed to load resource|Devtools|Content Security Policy|frame-ancestors|429|Too Many Requests|请求过于频繁|rate limit|Network Error|net::ERR/i.test(t)) {
        consoleErrors.push(`[${page.url()}] ` + t)
      }
    }
  })
  page.on('response', (res) => {
    if (res.url().includes('/api/') && res.status() >= 400) apiFail.push(res.status() + ' ' + res.url())
  })
})

test.afterAll(() => {
  // 仅 5xx 视为硬性失败；4xx 打印供排查（部分为预期，如未配置的短信服务）
  const hard = apiFail.filter((s) => s.startsWith('5'))
  expect(hard, '剩余页面功能测试 API 5xx:\n' + hard.join('\n')).toEqual([])
  expect(consoleErrors, '剩余页面功能测试控制台错误:\n' + consoleErrors.join('\n')).toEqual([])
})

for (const path of PAGES) {
  test(`功能冒烟: ${path}`, async ({ page }) => {
    await page.goto('/#' + path)
    await page.waitForTimeout(1000)
    // 点击第一个主操作按钮（新建/保存/统计分析/搜索 等），验证不崩溃
    const primary = page.locator('.el-button--primary').first()
    if (await primary.count()) {
      await primary.click().catch(() => {})
      await page.waitForTimeout(700)
      // 打开对话框则关闭，避免影响后续
      const dlg = page.locator('.el-dialog').first()
      if (await dlg.isVisible().catch(() => false)) {
        const close = page.locator('.el-dialog__close').first()
        if (await close.count()) await close.click().catch(() => {})
        await page.waitForTimeout(200)
      }
    }
    // 无控制台错误 / 无 5xx 由 afterAll 统一断言
  })
}
