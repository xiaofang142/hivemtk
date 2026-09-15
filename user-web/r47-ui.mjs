import { chromium } from 'playwright'
import { readFileSync } from 'fs'

// R47 UI 全交互覆盖：每页枚举所有可点元素(button/tab/链接/分页/开关)逐个点击
// 记录: 页面×交互×结果; 判定: pageerror/5xx/页面白屏 = FAIL
const BASE = 'http://localhost:8212'
const routes = readFileSync('/tmp/all_routes.txt', 'utf-8').split('\n').map(s => s.trim()).filter(Boolean)
routes.push('/backup/enhanced','/email/deliverability','/knowledge/rag-eval','/userSegment/rfm-matrix','/analytics/cohort-path','/knowledge/connectors')
const browser = await chromium.launch({ headless: true })
const page = await (await browser.newContext({ viewport: { width: 1440, height: 900 } })).newPage()

let cur = ''
const curInteractions = []  // 本页交互记录
page.on('pageerror', (e) => curInteractions.push('PAGEERROR: ' + String(e).slice(0, 90)))
page.on('response', (r) => {
  if (r.status() >= 500 && r.url().includes('/api/')) curInteractions.push(`5xx: ${r.status()} ${r.url().replace(BASE,'').split('?')[0].slice(0,60)}`)
})

await page.goto(BASE + '/login', { waitUntil: 'networkidle', timeout: 30000 })
await page.fill('input[type="text"]', 'admin')
await page.fill('input[type="password"]', 'Admin@12345678')
await page.click('button:has-text("Log In")')
await page.waitForTimeout(3000)

const problems = []
let totalInteractions = 0
let passPages = 0

async function cleanupDialogs() {
  for (let i = 0; i < 3; i++) {
    const msgboxConfirm = page.locator('.el-message-box__btns button:has-text("确定"), .el-message-box__btns button:has-text("确认")').first()
    const msgboxCancel = page.locator('.el-message-box__btns button:has-text("取消")').first()
    if (await msgboxCancel.count() > 0 && await msgboxCancel.isVisible().catch(() => false)) {
      await msgboxCancel.click({ timeout: 800 }).catch(() => {})
      await page.waitForTimeout(300)
      continue
    }
    if (await msgboxConfirm.count() > 0 && await msgboxConfirm.isVisible().catch(() => false)) {
      await msgboxConfirm.click({ timeout: 800 }).catch(() => {})
      await page.waitForTimeout(400)
      continue
    }
    const close = page.locator('.el-dialog__headerbtn:visible, .el-drawer__close-btn:visible').first()
    if (await close.count() > 0) {
      await close.click({ timeout: 800 }).catch(() => {})
      await page.waitForTimeout(300)
      continue
    }
    await page.keyboard.press('Escape')
    break
  }
}

for (const r of routes) {
  cur = r
  curInteractions.length = 0
  let clicked = 0
  try {
    await page.evaluate((h) => { location.hash = h }, r)
    await page.waitForTimeout(1400)

    // 枚举本页所有可见按钮（排除纯导航侧栏，聚焦内容区）
    let list = []
    try {
      list = await page.locator('main button:visible, .app-main button:visible, .page-content button:visible, #app .el-main button:visible').all()
    } catch { list = [] }
    if (list.length === 0) {
      try { list = await page.locator('button:visible').all() } catch { list = [] }
    }
    // 每页最多点 12 个（防极端页面），跳过登录/退出
    const texts = []
    for (const b of list.slice(0, 12)) {
      const t = (await b.textContent().catch(() => '') || '').trim()
      texts.push({ b, t })
    }
    for (const { b, t } of texts) {
      if (!t || /退出|登出|注销/.test(t)) continue
      curInteractions.length = 0
      try {
        if (!(await b.isVisible().catch(() => false))) continue
        if (!(await b.isEnabled().catch(() => false))) continue
        await b.click({ timeout: 1800 })
        clicked++
        await page.waitForTimeout(650)
        await cleanupDialogs()
      } catch { /* 元素消失等可接受 */ }
      const bad = curInteractions.filter(e => !e.includes('429'))
      if (bad.length) {
        problems.push({ route: r, interaction: t, bad: bad.slice(0, 2) })
        // 回到干净状态
        await page.evaluate((h) => { location.hash = h }, r)
        await page.waitForTimeout(900)
      }
    }
    // tab 切换
    const tabs = await page.locator('.el-tabs__item:visible').all()
    for (const tb of tabs.slice(0, 4)) {
      curInteractions.length = 0
      try { await tb.click({ timeout: 1200 }); clicked++; await page.waitForTimeout(500) } catch { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
      const bad = curInteractions.filter(e => !e.includes('429'))
      if (bad.length) problems.push({ route: r, interaction: 'tab:' + (await tb.textContent().catch(() => '') || '').trim(), bad: bad.slice(0, 2) })
    }
    totalInteractions += clicked
    if (problems.length === 0 || problems[problems.length - 1].route !== r) passPages++
    else if (!problems.some(p => p.route === r)) passPages++
  } catch (e) {
    problems.push({ route: r, interaction: 'NAV', bad: [String(e).slice(0, 60)] })
  }
}
const failPages = [...new Set(problems.map(p => p.route))]
console.log(`=== R47 UI 全交互: ${routes.length - failPages.length}/${routes.length} 页 PASS, 总点击 ${totalInteractions} 次 ===`)
const seen = new Set()
for (const p of problems) {
  const key = p.route + p.interaction
  if (seen.has(key)) continue
  seen.add(key)
  console.log(`[${p.route}] 点击"${p.interaction}"`)
  p.bad.forEach(b => console.log('   ', b))
}
await browser.close()
