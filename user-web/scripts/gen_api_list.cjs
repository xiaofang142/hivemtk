#!/usr/bin/env node
/**
 * 静态提取 user-web/src/api/*.js 的 API 清单 → user-web/api-list.md
 * 仅读取源码，不发任何请求。v3：先定位所有 http.<verb>( 调用点，再向前回溯最近方法名/签名。
 *
 * v4（2026-11-13 非破坏式收口）：重跑**保留**既有 api-list.md 的「实测」「问题/修复」两列
 * （键 = 文件|方法|动词|路径），新行才落 ⬜ 空白——与 scripts/gen_router_list.py
 * 「重跑保留既有勾选状态」同一设计纪律。此前 v3 直接整表覆写，一跑就把 281 条
 * ✅ 实测证据清成 ⬜（2026-11-13 实际踩雷，git checkout 恢复后修的本条）。
 * COVERAGE-GAP 统计口径同步修正：publicPost/publicGet 也算调用点。
 */
const fs = require('fs')
const path = require('path')

const apiDir = path.join(__dirname, '..', 'src', 'api')
const outFile = path.join(__dirname, '..', 'api-list.md')
const files = fs.readdirSync(apiDir).filter(f => f.endsWith('.js')).sort()

// ── 读入既有清单的实测/修复注记（非破坏式重跑的关键） ──
const preserved = new Map()
if (fs.existsSync(outFile)) {
  for (const line of fs.readFileSync(outFile, 'utf8').split('\n')) {
    if (!line.startsWith('| ')) continue
    const c = line.split('|').map(s => s.trim())
    // | # | 文件 | 导出对象 | 方法 | 动词 | 路径 | 前端签名 | 实测 | 问题/修复 |
    if (c.length < 10 || c[1] === '#' || /^-+$/.test(c[1])) continue
    const key = [c[2], c[4], c[5], c[6].replace(/^`|`$/g, '')].join('|')
    const tested = c[8] || ''
    const note = c[9] || ''
    // verbatim：人工勘注的 helper 伪行（路径格是「（...说明...）」而非真路径），
    // 弱匹配命中时整行原样沿用，机器行不顶掉。
    if (tested || note) preserved.set(key, { tested, note, path: c[6], sig: c[7], verbatim: c[6].startsWith('`（') })
  }
}


const rows = []
for (const file of files) {
  const text = fs.readFileSync(path.join(apiDir, file), 'utf8')
  const lines = text.split('\n')

  let exportName = ''
  const mObj = text.match(/export\s+const\s+([A-Za-z_$][\w$]*)\s*=\s*\{/)
  if (mObj) exportName = mObj[1]
  else { const m = text.match(/export\s+default\s+([A-Za-z_$][\w$]*)/); if (m) exportName = '(default) ' + m[1] }
  if (!exportName) exportName = '—（模块级函数）'

  // 路径变量解析（对齐既有清单口径）：只解析「裸变量实参」VAR:NAME → 模块级 const
  // 字符串值；模板串（`${base}/x`）保持**原文**——与已提交 api-list.md 的行路径逐字一致，
  // 保证重跑键匹配、注记不丢。
  const consts = {}
  for (const mm of text.matchAll(/(?:^|\n)\s*(?:export\s+)?const\s+([A-Za-z_$][\w$]*)\s*=\s*(?:'([^']*)'|"([^"]*)")/g)) {
    consts[mm[1]] = mm[2] ?? mm[3] ?? ''
  }
  const resolvePath = (s) => {
    for (let i = 0; i < 2; i++) {
      const vm = s.match(/^VAR:([A-Za-z_$][\w$]*)$/)
      if (vm && vm[1] in consts) s = consts[vm[1]]
      else break
    }
    return s
  }

  // 全部调用点：http.<verb>( / <instance>().<verb>( / publicPost|publicGet(；路径为字符串字面量或变量
  const callRe = /(?:http|getPublicRequestInstance\(\)|getRequestInstance\(\))\s*\.\s*(get|post|put|delete|patch)\s*\(\s*(`[^`]*`|'[^']*'|"[^"]*"|[A-Za-z_$][\w$]*(?:\.[\w$]+)*)|(?:publicPost|publicGet)\s*\(\s*(`[^`]*`|'[^']*'|"[^"]*")/g
  let cm
  while ((cm = callRe.exec(text)) !== null) {
    const verb = (cm[1] || (cm[0].includes('publicGet') ? 'get' : 'post')).toUpperCase()
    let pPath = cm[2] || cm[3] || ''
    if (/^[`'"]/.test(pPath)) pPath = pPath.slice(1, -1)
    else pPath = 'VAR:' + pPath
    pPath = resolvePath(pPath)
    const callIdx = cm.index

    // 向前回溯最近的方法定义（2 空格缩进的 key(: / key( ，或 export const/function）
    const before = text.slice(Math.max(0, callIdx - 1200), callIdx)
    const methodRe = /(?:^|\n)\s{2}([A-Za-z_$][\w$]*)\s*[:(=]|(?:^|\n)export\s+(?:const\s+([A-Za-z_$][\w$]*)\s*=|function\s+([A-Za-z_$][\w$]*)\s*\()/g
    let last = null, lastIdx = -1, sigLine = ''
    let mm
    while ((mm = methodRe.exec(before)) !== null) {
      const name = mm[1] || mm[2] || mm[3]
      if (['export','import','const','let','return','if','for','while','switch','catch','function','new'].includes(name)) continue
      last = name; lastIdx = mm.index; sigLine = before.slice(mm.index).split('\n')[0]
    }
    // 签名：取该行第一个 ( ... )
    let sig = ''
    const sigM = (sigLine || '').match(/\(([^)]*)\)/)
    if (sigM) sig = sigM[1].replace(/\s+/g, ' ').trim()

    // 上方注释（方法定义行上方 8 行内 // 行）
    let comment = ''
    if (lastIdx >= 0) {
      const defLineOffset = text.slice(0, Math.max(0, callIdx - 1200) + lastIdx).split('\n').length - 1
      const comments = []
      for (let k = defLineOffset - 1; k >= Math.max(0, defLineOffset - 8); k--) {
        const c = (lines[k] || '').match(/^\s*\/\/\s?(.*)$/)
        if (c) comments.unshift(c[1].trim())
        else if ((lines[k] || '').trim() === '') continue
        else break
      }
      comment = comments.join(' ')
    }

    rows.push({ file, exportName, method: last || '(top-level)', sig, verb, path: pPath, comment })
  }
}

let out = '# user-web API 清单（api-list.md）\n\n'
out += '> 来源：`user-web/src/api/*.js` 静态提取 v3，共 ' + rows.length + ' 个 HTTP 调用（' + files.length + ' 个文件）。\n'
out += '> 响应信封：`{code, data, message}`，code=0 成功；400 参数 / 401 认证 / 403 权限 / 404 不存在 / 409 冲突 / 500 服务端；业务码 4301=模块未开通、4305=订阅到期。\n'
out += '> 「实测」列：✅=curl 验证通过；❌=验证发现问题（注明）；⬜=未测。修复内容写入对应行「问题/修复」。\n\n'
out += '| # | 文件 | 导出对象 | 方法 | 动词 | 路径 | 前端签名 | 实测 | 问题/修复 |\n'
out += '|---|------|----------|------|------|------|----------|------|-----------|\n'
rows.forEach((r, idx) => {
  const esc = s => String(s).replace(/\|/g, '\\|').replace(/\n/g, ' ').slice(0, 120)
  // 非破坏式 1：同键（文件|方法|动词|路径）行复用既有「实测/问题-修复」
  let keep = preserved.get([r.file, r.method, r.verb, r.path].join('|'))
  // 非破坏式 2：路径解析不出字面量（VAR:xxx，如 postForm(url) helper 伪行）且
  // (文件|方法|动词) 唯一命中既有勘注行时——**整行沿用勘注版**（路径/实测/注记原样），
  // 不让机器行顶掉人工勘注。多命中视为歧义，不沿用。
  if (!keep && /^VAR:/.test(r.path)) {
    const candidates = [...preserved.entries()].filter(([k]) => {
      const p = k.split('|')
      return p[0] === r.file && p[1] === r.method && p[2] === r.verb
    })
    if (candidates.length === 1) keep = candidates[0][1]
  }
  let tested = keep ? keep.tested : '⬜'
  let note = keep ? keep.note : ''
  let pathCell = `\`${esc(r.path)}\``
  let sigCell = esc(r.sig) || '—'
  if (keep && keep.verbatim) { pathCell = keep.path; sigCell = keep.sig }
  out += `| ${idx + 1} | ${r.file} | ${r.exportName} | ${r.method} | ${r.verb} | ${pathCell} | ${sigCell} | ${tested} | ${note} |\n`
})

// preserved 行结构：{tested, note, path, sig, verbatim}
let reused = 0
rows.forEach(r => {
  if (preserved.get([r.file, r.method, r.verb, r.path].join('|'))) { reused++; return }
  if (/^VAR:/.test(r.path)) {
    const c = [...preserved.entries()].filter(([k]) => {
      const p = k.split('|')
      return p[0] === r.file && p[1] === r.method && p[2] === r.verb
    })
    if (c.length === 1) reused++
  }
})
fs.writeFileSync(outFile, out)
console.log('rows:', rows.length, 'of files:', files.length, '· preserved rows:', reused, '· lost notes:', preserved.size - reused)
// 覆盖率报告（口径：http.* 与 publicPost/publicGet 都算调用点）
const byFile = {}
rows.forEach(r => byFile[r.file] = (byFile[r.file] || 0) + 1)
let miss = 0
for (const f of files) {
  const t = fs.readFileSync(path.join(apiDir, f), 'utf8')
  const n = (t.match(/http\.(get|post|put|delete|patch)\s*\(/g) || []).length +
    (t.match(/(?:publicPost|publicGet)\s*\(/g) || []).length
  if (n !== (byFile[f] || 0)) { console.log('COVERAGE-GAP', f, 'http_calls=' + n, 'rows=' + (byFile[f] || 0)); miss++ }
}
console.log('files with gaps:', miss)
