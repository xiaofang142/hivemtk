// 一次性取证：以"已经有两条已结束会话的访客"身份打开 embed 挂件，看这个页面上到底有哪些可点元素。
// 用完即删（不是常驻件）。用法：node tests/audit/.tmp-r67-entry.mjs <visitor_id>
import { chromium } from 'playwright'

const visitor = process.argv[2]
if (!visitor) {
  process.stderr.write('缺 visitor_id\n')
  process.exit(2)
}

const base = process.env.E2E_BASE_URL || 'http://localhost:8211'
const browser = await chromium.launch({ headless: true })
const ctx = await browser.newContext()
const page = await ctx.newPage()
await page.addInitScript(v => localStorage.setItem('chat_visitor_id', v), visitor)

const net = []
page.on('response', r => {
  const c = r.status()
  if (c >= 400) net.push(`${c} ${r.request().method()} ${r.url()}`)
})
const errs = []
page.on('pageerror', e => errs.push(String(e).slice(0, 160)))

await page.goto(`${base}/#/chat/embed/default`, { waitUntil: 'load' })
await page.waitForTimeout(3000)

const facts = await page.evaluate(() => ({
  banners: [...document.querySelectorAll('.offline-banner')].map(n => n.textContent.trim().replace(/\s+/g, ' ')),
  buttons: [...document.querySelectorAll('button')].map(n => n.textContent.trim() || n.getAttribute('aria-label')),
  sessionItems: document.querySelectorAll('.session-item').length,
  modal: !!document.querySelector('.modal-mask'),
  bubbles: [...document.querySelectorAll('.bubble')].map(n => n.textContent.trim().slice(0, 40)),
  recentClosedRow: (performance.getEntriesByType('resource') || [])
    .map(r => r.name)
    .filter(u => u.includes('recent-closed'))
    .length
}))
process.stdout.write(JSON.stringify({ visitor, ...facts, net, errs }, null, 2) + '\n')
await browser.close()
