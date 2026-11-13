// o-probe.mjs — O轮逐路由只读探针（规则-1 合规：只导航+采集，不点击任何元素）。
// 队列：ROUTE_QUEUE 环境变量（默认 /tmp/o-round-queue.tsv，由 gen_route_queue.py 生成，258 行）。
// 每路由：about:blank 跳板 → goto hash 路由 → 采集 console.error / 非2xx / 主区文本 / 可见按钮名 / Vue 路由名。
// 交互点击由调用方（agent）读快照后逐个显式指定，不走本脚本。
// 用法：sed 's/__S__/<起>/; s/__E__/<止>/' o-probe.mjs > /tmp/run.mjs && ego-browser nodejs < /tmp/run.mjs
// 环境变量：BASE_URL（默认 https://work.xapptool.cn）/ EGO_SPACE（默认 hivemtk-route-test）/ OUT（结果 JSON）
import fs from 'node:fs'

const START = __S__
const END = __E__
const BASE = process.env.BASE_URL || 'https://work.xapptool.cn'
const queue = fs.readFileSync((process.env.ROUTE_QUEUE || '/tmp/o-round-queue.tsv'), 'utf8')
  .split('\n').filter(Boolean).map((l) => l.split('\t'))
const routes = queue.slice(START - 1, END).map(([n, st, raw, rp, name, login]) => [Number(n), rp, name, login])

const task = await taskSpace(process.env.EGO_SPACE || 'hivemtk-route-test')
const page = task.page('p1')
await page.cdp('egoAutomated.disable')
await page.cdp('Runtime.enable')
await page.cdp('Network.enable')

const results = []
for (const [n, path, name, login] of routes) {
  try {
    await page.goto('about:blank')
    await page.goto(BASE + '/#' + path, { timeout: 25000 })
    await page.waitForSelector('body *', { timeout: 12000 }).catch(() => {})
    await page.waitForTimeout(1300)
    let dlg = 0
    for (let i = 0; i < 3; i++) {
      const info = await page.info()
      if (info && info.dialog) { await page.acceptDialog('').catch(() => {}); dlg++; await page.waitForTimeout(250) } else break
    }
    const events = (await page.events()) || []
    const errs = events.filter((e) => e.method === 'Runtime.consoleAPICalled' && (e.params || {}).type === 'error')
    const errTexts = errs.slice(0, 3).map((e) => {
      const txt = (e.params.args || []).map((a) => String(a.value ?? a.description ?? '')).join(' ').slice(0, 90)
      const f = ((e.params.stackTrace || {}).callFrames || [])[0]
      return txt + ' @' + (f ? `${(f.url || '').split('/').pop()}:${f.lineNumber}` : '?')
    })
    const bad = events.filter((e) => e.method === 'Network.responseReceived' && e.params.response.status >= 400)
      .map((e) => `${e.params.response.status} ${e.params.response.url.replace(/^https?:\/\/[^/]+/, '').slice(0, 70)}`)
    const dom = await page.evaluate(`(() => {
      const main = document.querySelector('.el-main, main')
      const app = document.querySelector('#app')
      let rn = ''
      try { const r = app.__vue_app__.config.globalProperties.$route; rn = (r.name || '') + '|' + r.path } catch (e) {}
      const btns = [...document.querySelectorAll('button, .el-button')]
        .filter((b) => b.offsetParent !== null)
        .map((b) => (b.innerText || '').trim()).filter(Boolean).slice(0, 14)
      return JSON.stringify({ main: main ? main.textContent.replace(/\\s+/g, ' ').trim().slice(0, 160) : '', mainLen: main ? main.textContent.trim().length : 0, rn, btns })
    })()`).catch(() => '{}')
    const d = JSON.parse(dom || '{}')
    const finalUrl = (await page.url()).replace(BASE + '/#', '').slice(0, 60)
    results.push({
      n, path, name, login: login || '', final: finalUrl, dlg,
      cerr: errs.length, bad, mainLen: d.mainLen || 0, rn: d.rn || '', btns: d.btns || [],
      head: (d.main || '').slice(0, 120), errTexts: errs.length ? errTexts : undefined,
    })
  } catch (e) {
    results.push({ n, path, name, err: String(e).slice(0, 140) })
  }
  console.log(JSON.stringify(results[results.length - 1]))
}
fs.writeFileSync((process.env.OUT || '/tmp/o-probe.json'), JSON.stringify(results))
console.log('DONE', results.length)
