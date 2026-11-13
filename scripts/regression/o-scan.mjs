// o-scan.mjs — O轮对抗性复扫：全量路由只读导航 + 采集 #app 全文 + 扫「误报失败」关键词。
// 规则-1 合规：只导航+采集，不点击任何元素。
// 目标盲区：短链 bug 类（API 200，但前端 if(res&&res.x) 判负 → 页面显示「获取失败/加载失败」）——
// 该类缺陷 0 console.error、0 非2xx，只能靠正文关键词捕获。
// 用法：sed 's/__S__/<起>/; s/__E__/<止>/' o-scan.mjs > /tmp/run.mjs && ego-browser nodejs < /tmp/run.mjs
// 环境变量：BASE_URL（默认 https://work.xapptool.cn）/ EGO_SPACE / ROUTE_QUEUE / OUT（结果 JSON）
import fs from 'node:fs'

const START = __S__
const END = __E__
const BASE = process.env.BASE_URL || 'https://work.xapptool.cn'
// 只在「加载即失败」语义上命中；排除合法内容词
const KW = [
  /获取[^\n]{0,8}失败/, /加载[^\n]{0,4}失败/, /请求错误/, /加载异常/,
  /出错了/, /系统错误/, /服务器错误/, /服务异常/, /网络异常/, /网络错误/,
  /查询失败/, /初始化失败/, /连接失败/,
]
const queue = fs.readFileSync((process.env.ROUTE_QUEUE || '/tmp/o-round-queue.tsv'), 'utf8')
  .split('\n').filter(Boolean).map((l) => l.split('\t'))
const routes = queue.slice(START - 1, END).map(([n, st, raw, rp, name, login]) => [Number(n), rp, name, login || ''])

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
    const bad = events.filter((e) => e.method === 'Network.responseReceived' && e.params.response.status >= 400)
      .map((e) => `${e.params.response.status} ${e.params.response.url.replace(/^https?:\/\/[^/]+/, '').slice(0, 70)}`)
    const dom = await page.evaluate(`(() => {
      const app = document.querySelector('#app')
      const txt = app ? app.innerText.replace(/\\s+/g, ' ').trim() : ''
      let rn = ''
      try { const r = app.__vue_app__.config.globalProperties.$route; rn = (r.name || '') + '|' + r.path } catch (e) {}
      const main = document.querySelector('.el-main, main')
      return JSON.stringify({ txt: txt.slice(0, 6000), rn, mainLen: main ? main.textContent.trim().length : 0 })
    })()`).catch(() => '{}')
    const d = JSON.parse(dom || '{}')
    const txt = d.txt || ''
    const hits = []
    for (const re of KW) { const m = txt.match(re); if (m) hits.push(m[0]) }
    results.push({
      n, path, name, final: (await page.url()).replace(BASE + '/#', '').slice(0, 60), dlg,
      cerr: errs.length, bad, rn: d.rn || '', mainLen: d.mainLen || 0,
      txtLen: txt.length, hits: [...new Set(hits)],
      // 命中时给上下文，便于人工判读
      ctx: hits.length ? hits.map((h) => { const i = txt.indexOf(h); return txt.slice(Math.max(0, i - 60), i + 80) }).slice(0, 3) : undefined,
    })
  } catch (e) {
    results.push({ n, path, name, err: String(e).slice(0, 140) })
  }
  console.log(JSON.stringify(results[results.length - 1]))
}
fs.writeFileSync((process.env.OUT || '/tmp/o-scan.json'), JSON.stringify(results))
console.log('DONE', results.length)
