// 巡检结果汇总：把 reports/run-*/<page>.json 摊平成"按根因聚类"的清单。
// 用法: node tests/audit/triage-run.mjs <runDirGlob> [--full]
//   默认按"错误指纹"聚类（同一条信息跨页重复只列一次 + 命中页数），
//   因为一处后端/前端根因通常会在几十个页面上重复刷屏。
import fs from 'fs'
import path from 'path'
import { fileURLToPath } from 'url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const glob = process.argv[2] || 'run-*'
const full = process.argv.includes('--full')

const dirs = fs.readdirSync(path.resolve(__dirname, 'reports'))
  .filter((d) => d.startsWith(glob.replace(/\/$/, '')))
  .map((d) => path.resolve(__dirname, 'reports', d))
if (!dirs.length) {
  console.error(`没有匹配的运行目录: ${glob}`)
  process.exit(1)
}

const rows = []
for (const dir of dirs) {
  for (const f of fs.readdirSync(dir).filter((f) => f.endsWith('.json') && f !== 'summary.json' && f !== 'routes.json')) {
    rows.push(JSON.parse(fs.readFileSync(path.join(dir, f), 'utf8')))
  }
}

const norm = (s) => String(s).replace(/\s+/g, ' ').trim()
const fp = (s) => norm(s).slice(0, 180)

const clusters = new Map()
const add = (kind, text, page, extra) => {
  const key = `${kind}::${fp(text)}`
  if (!clusters.has(key)) clusters.set(key, { kind, text: norm(text).slice(0, 400), pages: [], extra })
  const c = clusters.get(key)
  if (!c.pages.includes(page)) c.pages.push(page)
}

let ok = 0
const badPages = []
for (const r of rows) {
  const vis = r.render || {}
  const interactionErr = (r.interactions || []).filter((i) => i.error)
  const net5 = (r.errors.net || []).filter((n) => n.status >= 500)
  const net4 = (r.errors.net || []).filter((n) => n.status >= 400 && n.status < 500)
  const netFail = (r.errors.net || []).filter((n) => n.failed)
  const visualIssues = []
  if (vis.notFound) visualIssues.push('404兜底页')
  if (vis.blank) visualIssues.push('首屏空白')
  if (vis.kickedTo) visualIssues.push(`被踢回${vis.kickedTo}`)
  for (const t of vis.toast || []) add('TOAST', t, r.page)
  if (r.loadError) add('LOAD', r.loadError, r.page)
  for (const e of r.errors.console) add('CONSOLE', e, r.page)
  for (const e of r.errors.page) add('PAGEERROR', e, r.page)
  for (const n of net5) add('NET5XX', `${n.status} ${n.method} ${n.url}`, r.page)
  for (const n of netFail) add('NETFAIL', `${n.method} ${n.url} (${n.failed ? 'requestfailed' : n.status})`, r.page)
  // 4xx 不进"阻断"判定，但要成簇：前端打错路径/参数（404/400/422）只有在这里才看得见
  for (const n of net4) add('NET4XX', `${n.status} ${n.method} ${n.url}`, r.page)
  for (const i of interactionErr) add('CLICKERR', `${i.label} → ${i.error}`, r.page)
  for (const v of visualIssues) add('VISUAL', v, r.page)
  const n4 = net4.length
  const hit = r.errors.console.length || r.errors.page.length || net5.length || netFail.length || interactionErr.length || visualIssues.length || r.loadError
  if (!hit) ok++
  else badPages.push({ page: r.page, c: r.errors.console.length, p: r.errors.page.length, n5: net5.length, nf: netFail.length, n4, ce: interactionErr.length, vis: visualIssues.join('+'), els: r.elementCount })
}

console.log(`运行目录 ${dirs.length} 个 · 页面报告 ${rows.length} 份 · 干净 ${ok} · 有发现 ${badPages.length}`)
console.log('\n## 按页面')
for (const b of badPages.sort((a, z) => (z.c + z.p + z.n5 + z.nf + z.ce) - (a.c + a.p + a.n5 + a.nf + a.ce))) {
  console.log(`  ${b.page.padEnd(38)} c=${b.c} p=${b.p} 5xx=${b.n5} fail=${b.nf} 4xx=${b.n4} clickErr=${b.ce} visual=${b.vis || '-'} els=${b.els}`)
}
console.log('\n## 按指纹聚类（kind · 命中页数 · 信息）')
const list = [...clusters.values()].sort((a, z) => z.pages.length - a.pages.length || a.kind.localeCompare(z.kind))
for (const c of list) {
  console.log(`\n[${c.kind}] ×${c.pages.length} 页`)
  console.log(`  ${c.text}`)
  console.log(`  pages: ${c.pages.slice(0, full ? 999 : 12).join(', ')}${c.pages.length > 12 && !full ? ' …' : ''}`)
}
