import { print as _print } from '../../scripts/lib/stdout.cjs';
// 汇总 tests/e2e/audit 下各模块报告，分类列出异常：
//  - realBugs: 非平台代理、非中止/非 HMR 的真实后端 4xx/5xx 与 JS 错误
//  - environmental: /api/platform/* 代理到未运行的 platform-server（环境性，非代码 bug）
//  - noise: ERR_ABORTED / @intlify HMR / CSP meta 警告（审计方法学伪阳性）
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const ROOT = path.join(__dirname, '..', '..', 'tests', 'e2e', 'audit')

const isNoiseConsole = (t) =>
  /frame-ancestors|@intlify|__x00__|Failed to load resource.*ERR_ABORTED|net::ERR_ABORTED/i.test(t)
const isNoiseApi = (a) => /ERR_ABORTED|@intlify|__x00__/i.test(a.url || '')
const isPlatform = (a) => /\/api\/platform\//i.test(a.url || '')

const realBugs = []
const environmental = []
const noise = []

const mods = fs.readdirSync(ROOT).filter((d) => fs.statSync(path.join(ROOT, d)).isDirectory())
for (const mod of mods) {
  const dir = path.join(ROOT, mod)
  for (const f of fs.readdirSync(dir)) {
    if (!f.endsWith('.json')) continue
    const rep = JSON.parse(fs.readFileSync(path.join(dir, f), 'utf-8'))
    for (const it of rep.items || []) {
      const ce = (it.consoleErrors || []).filter((e) => !isNoiseConsole(e))
      const pe = it.pageErrors || []
      const aeRaw = it.apiErrors || []
      const ae = aeRaw.filter((a) => !isNoiseApi(a))
      for (const e of ce) {
        const item = { mod, route: rep.route, text: it.text, type: it.type, msg: e }
        if (/status of 5\d\d|网关返回非 JSON/.test(e)) noise.push(item)
        else realBugs.push(item)
      }
      for (const e of pe) realBugs.push({ mod, route: rep.route, text: it.text, type: it.type, pageerror: e })
      for (const a of ae) {
        const item = { mod, route: rep.route, text: it.text, type: it.type, api: a.status, url: a.url }
        if (isPlatform(a)) environmental.push(item)
        else realBugs.push(item)
      }
    }
    for (const e of rep.errors || []) realBugs.push({ mod, route: rep.route, text: e.item, error: e.error, kind: 'clickError' })
  }
}

const byMod = (arr) => arr.reduce((m, x) => ((m[x.mod] = (m[x.mod] || 0) + 1), m), {})
_print(`真实 bug 交互: ${realBugs.length}`, JSON.stringify(byMod(realBugs)))
_print(`环境性(platform代理, 平台端未运行): ${environmental.length}`, JSON.stringify(byMod(environmental)))
_print(`噪声(中止/HMR/CSP): ${noise.length}`)

const show = (title, arr) => {
  _print('\n################ ' + title + ' ################')
  for (const r of arr) {
    const loc = `[${r.mod}] ${r.route}`
    const act = r.text ? `交互: ${r.text} (${r.type})` : ''
    const detail = r.pageerror ? `🔴 pageerror: ${r.pageerror}` : r.api ? `🟠 api ${r.api}: ${r.url}` : r.msg ? `🔴 ${r.msg}` : r.error ? `⚠ ${r.error}` : ''
    _print(`${loc} | ${act} | ${detail}`)
  }
}
show('真实 BUG', realBugs)
show('环境性(平台代理, 非代码 bug)', environmental)
