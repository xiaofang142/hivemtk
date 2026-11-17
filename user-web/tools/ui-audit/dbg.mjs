import { chromium } from 'playwright'

const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD
if (!ADMIN_PASSWORD) { console.error('E2E_ADMIN_PASSWORD 未设置，拒绝运行'); process.exit(1) }
const BASE = 'http://127.0.0.1:8211'
const b = await chromium.launch({ headless: true })
const p = await b.newPage()
await p.goto(BASE + '/', { waitUntil: 'domcontentloaded' })
await p.waitForTimeout(800)
if (!(await p.evaluate(() => !!localStorage.getItem('token')))) {
  const inp = p.locator('.login-box input.el-input__inner')
  await inp.nth(0).fill('admin')
  await inp.nth(1).fill(ADMIN_PASSWORD)
  await p.click('.login-box button.el-button--primary')
  await p.waitForFunction(() => !!localStorage.getItem('token'), { timeout: 20000 })
}
const route = process.argv[2] || '/aiAgent/list'
await p.goto(BASE + '/#' + route, { waitUntil: 'networkidle' })
await p.waitForTimeout(1500)
const info = await p.evaluate(() => {
  const sel = ['.app-main', '.main-container', 'main', '#app']
  const res = {}
  for (const s of sel) {
    const e = document.querySelector(s)
    res[s] = e ? e.querySelectorAll('*').length : 'MISSING'
  }
  const all = document.querySelectorAll(
    'button,a,input,select,textarea,.el-switch,.el-tabs__item,.el-select'
  )
  return { containers: res, totalInteractive: all.length, bodyLen: document.body.innerHTML.length, url: location.href }
})
console.log(JSON.stringify(info, null, 2))
await b.close()
