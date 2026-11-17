import { test, expect } from '@playwright/test'

test.skip(!process.env.E2E_ADMIN_PASSWORD, 'E2E_ADMIN_PASSWORD not set')
const BASE = 'http://localhost:8216'
test('debug login+nav', async ({ page }) => {
  const logs = []
  page.on('console', (m) => logs.push(m.type() + ': ' + m.text()))
  page.on('response', (r) => { if (r.url().includes('/api/')) logs.push('RESP ' + r.status() + ' ' + r.url()) })
  page.on('requestfailed', (r) => { if (r.url().includes('/api/')) logs.push('REQFAIL ' + r.url() + ' ' + (r.failure()?.errorText || '')) })

  await page.goto(BASE, { waitUntil: 'networkidle' })
  await page.waitForTimeout(2000)
  console.log('after BASE load: loginBox=', await page.locator('.login-box').count(), 'menu=', await page.locator('.el-menu').count(), 'url=', page.url())

  await page.goto(BASE + '/#/login', { waitUntil: 'networkidle' })
  await page.waitForSelector('.login-box input', { timeout: 15000 })
  const inputs = page.locator('.login-box input')
  await inputs.nth(0).fill('admin')
  await page.locator('.login-box input[type="password"]').fill(process.env.E2E_ADMIN_PASSWORD)
  await page.locator('.login-box .el-button--primary').click()
  await page.waitForSelector('.el-menu', { timeout: 20000 }).catch((e) => console.log('menu wait fail:', e.message))
  console.log('after login: menu=', await page.locator('.el-menu').count(), 'url=', page.url())
  console.log('token len=', await page.evaluate(() => (localStorage.getItem('token') || '').length))

  await page.goto(BASE + '/#/clue/list', { waitUntil: 'networkidle' })
  await page.waitForTimeout(3000)
  console.log('after nav: url=', page.url(), 'elMain=', await page.locator('.el-main').count(), 'cluelist=', await page.locator('.cluelist').count())
  console.log('body=', (await page.locator('body').innerText().catch(() => '') || '').slice(0, 300))
  console.log('LOGS:\n' + logs.slice(-40).join('\n'))
})
