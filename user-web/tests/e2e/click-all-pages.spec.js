// 全页面「模拟人工点击」交互扫描：以管理员身份登录后，遍历全部路由，
// 对每个页面模拟点击安全交互按钮（新增/查询/刷新/导入/同步/查看/导出/设置等），
// 实时采集 console / pageerror / 5xx，并在结尾输出问题明细。
// 破坏性操作（删除/退出/重置/清空）被显式排除，避免误删数据与退出登录。
import { test, expect } from '@playwright/test'
import fs from 'fs'

const BASE = process.env.E2E_BASE_URL || 'http://localhost:8211'
const ADMIN_USER = process.env.E2E_USER || 'admin'
const ADMIN_PASS = process.env.E2E_PASS || 'Admin@12345678'
const PATHS = JSON.parse(fs.readFileSync('/tmp/route-paths.json', 'utf8'))

// 仅点击「打开/查询/切换/导出」类安全交互；排除会写库或退出的操作
const SAFE = /新增|新建|添加|Add|Create|创建|导入|Import|同步|Sync|刷新|Refresh|查询|搜索|Search|查找|查看|详情|Detail|导出|Export|统计|Statistics|测试|Test|设置|Setting|配置|Config|草稿|Drafts|展开|收起|全部|重置筛选/i
const DANGER = /删除|Delete|移除|Remove|重置|清除|Logout|退出|注销|清空|解散/i

test('[CLICK-ALL] 全页面模拟人工点击扫描', async ({ page }) => {
  test.setTimeout(3000000)
  const log = []        // 所有采集到的问题（含上下文）
  const realBugs = []   // 真实崩溃：pageerror + 5xx
  let ctxRoute = ''
  let ctxAction = ''

  const benign = (t) =>
    /favicon|404 \(Not Found\)|Failed to load resource.*404|net::ERR|ERR_NAME_NOT_RESOLVED|ERR_CONNECTION|ERR_NETWORK|Mixed Content|Download the React DevTools/i.test(t)

  page.on('console', (m) => {
    if (m.type() === 'error') {
      const t = m.text()
      if (benign(t)) return
      const e = `[${ctxRoute} :: ${ctxAction}] CONSOLE: ${t}`
      log.push(e)
    }
  })
  page.on('pageerror', (err) => {
    const e = `[${ctxRoute} :: ${ctxAction}] PAGEERROR: ${err.message}`
    log.push(e)
    realBugs.push(e)
  })
  page.on('requestfailed', (r) => {
    const u = r.url()
    if (u.includes('/api/') && !benign(u)) {
      log.push(`[${ctxRoute} :: ${ctxAction}] REQFAIL ${r.failure()?.errorText || '?'} ${r.method()} ${u.slice(0, 130)}`)
    }
  })
  page.on('response', (res) => {
    if (res.status() >= 500) {
      const e = `[${ctxRoute} :: ${ctxAction}] 5xx ${res.status()} ${res.url()}`
      log.push(e)
      realBugs.push(e)
    }
  })

  // 登录
  ctxRoute = '(login)'; ctxAction = 'login'
  await page.goto(BASE + '/#/login', { waitUntil: 'networkidle' })
  await page.fill('input[type="text"]', ADMIN_USER)
  await page.fill('input[type="password"]', ADMIN_PASS)
  await page.click('button[type="button"].el-button--primary')
  await page.waitForURL('**/#/**', { timeout: 15000 }).catch(() => {})
  await page.waitForTimeout(1500)

  let visited = 0
  let interacted = 0
  for (const p of PATHS) {
    // /login 不是业务页：已登录态下 goto 会破坏会话（清 storage），排除
    if (p === '/login' || p === '/login/' || p === 'login') continue
    const route = p
    const url = BASE + '/#' + (p.startsWith('/') ? p : '/' + p)
    ctxRoute = route; ctxAction = '(render)'
    await page.goto(url, { waitUntil: 'domcontentloaded' }).catch(() => {})
    await page.waitForTimeout(450)
    visited++

    // 收集页面可见安全按钮文案
    let texts = []
    try {
      texts = await page.evaluate(() => {
        const out = []
        document.querySelectorAll('button.el-button').forEach((b) => {
          const t = (b.innerText || '').trim()
          if (t) out.push(t)
        })
        return out
      })
    } catch (e) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
    const safe = [...new Set(texts)].filter((t) => SAFE.test(t) && !DANGER.test(t)).slice(0, 6)

    for (const label of safe) {
      ctxAction = 'click:' + label
      try {
        await page.goto(url, { waitUntil: 'domcontentloaded' }).catch(() => {})
        await page.waitForTimeout(350)
        const btn = page.locator('button.el-button', { hasText: label }).first()
        await btn.click({ timeout: 4000 })
        await page.waitForTimeout(550)
        // 关闭可能弹出的对话框（点 X），避免影响后续交互
        const dlg = page.locator('.el-dialog:visible').first()
        if (await dlg.count()) {
          await dlg.locator('.el-dialog__headerbtn').first().click({ timeout: 2000 }).catch(() => {})
          await page.waitForTimeout(250)
        }
        interacted++
      } catch (e) {
        log.push(`[${route} :: ${ctxAction}] INTERACT_FAIL: ${e.message.split('\n')[0]}`)
      }
    }
    ctxAction = '(done)'
    if (visited % 20 === 0) {
      console.log(`[progress] visited=${visited}/${PATHS.length} interacted=${interacted} issuesSoFar=${log.length}`)
    }
  }

  const unique = [...new Set(log)]
  console.log('\n===== CLICK-ALL 扫描完成 =====')
  console.log(`总路由数: ${PATHS.length}  访问: ${visited}  模拟点击: ${interacted}`)
  console.log(`采集到的问题条目数(去重): ${unique.length}  其中真实崩溃(pageerror/5xx): ${realBugs.length}`)
  if (unique.length) {
    console.log('\n----- 问题明细(去重) -----')
    console.log(unique.join('\n'))
  }
  console.log('\n===== 结束 =====')

  expect(realBugs).toEqual([])
})
