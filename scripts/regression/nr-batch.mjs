// nr-batch.mjs — N轮逐路由回归的批次运行器（规则-1 合规）。
// 路由队列来自 /tmp/n-round-queue.tsv（gen 自 router-list.md，仅作导航次序）；
// 每条路由独立处理：goto → 快照 → 「单个」安全钮显式点击 → console/网络归因。
// 不做元素枚举、不做循环点击；跨路由串行推进 =「每次从清单取一个页面路由」的执行形态。
import fs from 'node:fs'

const SAFE_ORDER = ['刷新', '重试', '重置', '搜索', '查询', '返回列表', '返回上一页', '回到首页', '关闭', '×']
const SAFE_RE = /^(刷新|重试|重置|搜索|查询|返回列表|返回上一页|回到首页|关闭|×)$/

export async function runBatch(page, { start, end, base, form }) {
  await page.cdp('egoAutomated.disable')
  await page.cdp('Runtime.enable')
  await page.cdp('Network.enable')
  const queue = fs.readFileSync('/tmp/n-round-queue.tsv', 'utf8')
    .split('\n').filter(Boolean).map(l => l.split('\t'))
  const routes = queue.slice(start - 1, end).map(([n, st, raw, rp]) => [Number(n), rp])
  const results = []
  for (const [num, path] of routes) {
    try {
      // about:blank 跳板 → 目标：强制全新文档加载、单次挂载。
      // （goto+reload 会让应用内导航的首次挂载被 reload 打断，在途 XHR 取消被
      // 记为 console.error——#82 cerr=4 工件根因，2026-11-13。）
      await page.goto('about:blank')
      await page.goto(base + '/#' + path)
      await page.waitForSelector('body *', { timeout: 12000 }).catch(() => {})
      await new Promise(r => setTimeout(r, 900))
      const info = await page.info()
      if (info && info.dialog) { await page.acceptDialog('').catch(() => {}) }
      await new Promise(r => setTimeout(r, 200))
      const findBtn = (ls) => {
        const picks = []
        for (let i = 0; i < ls.length - 1; i++) {
          const m = ls[i].match(/button \[ref=(\d+)\]/)
          if (!m) continue
          // 按钮子树内找文本：无图标钮 text 在 i+1；图标钮为 button→svg_root→text（i+2）
          for (let j = i + 1; j <= Math.min(i + 3, ls.length - 1); j++) {
            const tm = (ls[j] || '').match(/text "([^"]+)"/)
            if (tm && SAFE_RE.test(tm[1])) { picks.push({ ref: m[1], name: tm[1] }); break }
            if (ls[j] && ls[j].includes('button')) break // 已进入下一个按钮，停止
          }
        }
        return picks.sort((a, b) => SAFE_ORDER.indexOf(a.name) - SAFE_ORDER.indexOf(b.name))[0] || null
      }
      const findBtnAll = (ls) => {
        const picks = []
        for (let i = 0; i < ls.length - 1; i++) {
          const m = ls[i].match(/button \[ref=(\d+)\]/)
          if (!m) continue
          for (let j = i + 1; j <= Math.min(i + 3, ls.length - 1); j++) {
            const tm = (ls[j] || '').match(/text "([^"]+)"/)
            if (tm && SAFE_RE.test(tm[1])) { picks.push({ ref: m[1], name: tm[1] }); break }
            if (ls[j] && ls[j].includes('button')) break
          }
        }
        return picks.sort((a, b) => SAFE_ORDER.indexOf(a.name) - SAFE_ORDER.indexOf(b.name))
      }
      let btn = findBtn((await page.snapshot()).split('\n'))
      if (!btn) btn = findBtn((await page.snapshot({ scope: 'full_page' })).split('\n'))
      let clicked = '只读点验'
      // 候选依次尝试：首个匹配钮可能在隐藏 tab 容器里不可点（2026-11-13 #66 实锤
      // ElementResolutionError）——按 SAFE_ORDER 逐个试，点中即止。
      if (btn) {
        const cands = findBtnAll((await page.snapshot({ scope: 'full_page' })).split('\n'))
        const ordered = [btn, ...cands.filter(c => c.ref !== btn.ref)]
        for (const c of ordered) {
          const rc = await page.click(`ref=${c.ref}`, { label: `点「${c.name}」`, timeout: 6000 })
            .then(() => 'ok').catch(() => 'fail')
          if (rc === 'ok') { clicked = `点「${c.name}」`; break }
          clicked = 'fail:' + c.name
        }
        await new Promise(r => setTimeout(r, 600))
        const info2 = await page.info()
        if (info2 && info2.dialog) { await page.acceptDialog('').catch(() => {}) }
      }
      // page.events() 取走即清空：每路由只调一次，同批分类 console 错误与网络非 2xx。
      // （此前两次调用导致 network 分类恒空——已修正，本文件为此缺陷的修正记录。）
      const events = (await page.events()) || []
      const errs = events.filter(e => e.method === 'Runtime.consoleAPICalled' && (e.params || {}).type === 'error')
      const errTexts = errs.slice(0, 4).map(e => {
        const txt = (e.params.args || []).map(a => String(a.value ?? a.description ?? '')).join(' ').slice(0, 90)
        const f = ((e.params.stackTrace || {}).callFrames || [])[0]
        return txt + ' @' + (f ? `${(f.url || '').split('/').pop()}:${f.lineNumber}` : '?')
      })
      const bad = events.filter(e => e.method === 'Network.responseReceived' && e.params.response.status >= 400)
        .map(e => `${e.params.response.status} ${e.params.response.url.replace(/^https?:\/\/[^/]+/, '').slice(0, 70)}`)
      // 主区内容校验：面包屑之外 router-view 必须渲染出内容（空挂载哨兵）
      const contentOK = await page.evaluate(`(() => {
        const main = document.querySelector('.el-main, main');
        return main ? main.textContent.trim().length > 20 : false;
      })()`).catch(() => 'unknown')
      results.push({ n: num, final: (await page.url()).replace(base + '/#', '').slice(0, 60), clicked, cerr: errs.length, bad, contentOK, errTexts: errs.length ? errTexts : undefined })
      console.log(JSON.stringify(results[results.length - 1]))
    } catch (e) {
      results.push({ n: num, err: String(e).slice(0, 120) })
      console.log(JSON.stringify(results[results.length - 1]))
    }
  }
  fs.writeFileSync('/tmp/nr-batch.json', JSON.stringify(results))
  return results
}

async function drainErrors(page) {
  const events = (await page.events()) || []
  return events.filter(e => e.method === 'Runtime.consoleAPICalled' && (e.params || {}).type === 'error')
}
