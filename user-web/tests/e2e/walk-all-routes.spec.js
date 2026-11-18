import { print as _print } from '../../scripts/lib/stdout.cjs';
// 商户端全路由深度遍历：以管理员身份登录后，逐一访问所有路由，
// 记录每个路由的控制台报错 / 页面异常 / 5xx，输出报告。
import { test, expect } from '@playwright/test'
import fs from 'fs'

const BASE = process.env.E2E_BASE_URL || 'http://localhost:8211'
const ADMIN_USER = process.env.E2E_USER || 'admin'
const ADMIN_PASS = process.env.E2E_ADMIN_PASSWORD || process.env.E2E_PASS
test.skip(!ADMIN_PASS, 'E2E_ADMIN_PASSWORD/E2E_PASS not set')
const PATHS = JSON.parse(fs.readFileSync('/tmp/route-paths.json', 'utf8'))

test('[WALKALL] 遍历全部路由并采集错误', async ({ page }) => {
  test.setTimeout(600000)
  const errorsByRoute = {}

  page.on('console', (msg) => {
    if (msg.type() === 'error') {
      const cur = page.url().split('#')[1] || '/'
      ;(errorsByRoute[cur] ||= []).push('CONSOLE: ' + msg.text())
    }
  })
  page.on('pageerror', (err) => {
    const cur = page.url().split('#')[1] || '/'
    ;(errorsByRoute[cur] ||= []).push('PAGEERROR: ' + err.message)
  })
  page.on('response', (res) => {
    if (res.status() >= 500) {
      const cur = page.url().split('#')[1] || '/'
      ;(errorsByRoute[cur] ||= []).push(`5xx ${res.status()} ${res.url()}`)
    }
  })

  // 登录
  await page.goto(BASE + '/#/login', { waitUntil: 'networkidle' })
  await page.fill('input[type="text"]', ADMIN_USER)
  await page.fill('input[type="password"]', ADMIN_PASS)
  await page.click('button[type="button"].el-button--primary')
  await page.waitForURL('**/#/**', { timeout: 15000 }).catch(() => {})
  await page.waitForTimeout(1500)

  for (const p of PATHS) {
    const url = BASE + '/#' + (p.startsWith('/') ? p : '/' + p)
    await page.goto(url, { waitUntil: 'domcontentloaded' }).catch(() => {})
    await page.waitForTimeout(450)
  }

  // 汇总报告
  const routesWithErrors = Object.entries(errorsByRoute)
  _print('\n===== 有错误的路由数: ' + routesWithErrors.length + ' / ' + PATHS.length + ' =====')
  for (const [route, errs] of routesWithErrors) {
    _print('\n### ' + route)
    _print([...new Set(errs)].join('\n'))
  }
  _print('\n===== 遍历完成 =====')

  // 断言：无页面级异常
  const pageErrors = Object.values(errorsByRoute).flat().filter((e) => e.startsWith('PAGEERROR'))
  expect(pageErrors).toEqual([])
})
