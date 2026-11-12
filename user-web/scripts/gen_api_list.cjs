#!/usr/bin/env node
/**
 * 静态提取 user-web/src/api/*.js 的 API 清单 → user-web/api-list.md
 * 仅读取源码，不发任何请求。v3：先定位所有 http.<verb>( 调用点，再向前回溯最近方法名/签名。
 */
const fs = require('fs')
const path = require('path')

const apiDir = path.join(__dirname, '..', 'src', 'api')
const files = fs.readdirSync(apiDir).filter(f => f.endsWith('.js')).sort()

const rows = []
for (const file of files) {
  const text = fs.readFileSync(path.join(apiDir, file), 'utf8')
  const lines = text.split('\n')

  let exportName = ''
  const mObj = text.match(/export\s+const\s+([A-Za-z_$][\w$]*)\s*=\s*\{/)
  if (mObj) exportName = mObj[1]
  else { m = text.match(/export\s+default\s+([A-Za-z_$][\w$]*)/); if (m) exportName = '(default) ' + m[1] }
  if (!exportName) exportName = '—（模块级函数）'

  // 全部调用点：http.<verb>( / <instance>().<verb>( / publicPost|publicGet(；路径为字符串字面量或变量
  const callRe = /(?:http|getPublicRequestInstance\(\)|getRequestInstance\(\))\s*\.\s*(get|post|put|delete|patch)\s*\(\s*(`[^`]*`|'[^']*'|"[^"]*"|[A-Za-z_$][\w$]*(?:\.[\w$]+)*)|(?:publicPost|publicGet)\s*\(\s*(`[^`]*`|'[^']*'|"[^"]*")/g
  let cm
  while ((cm = callRe.exec(text)) !== null) {
    const verb = (cm[1] || (cm[0].includes('publicGet') ? 'get' : 'post')).toUpperCase()
    let pPath = cm[2] || cm[3] || ''
    if (/^[`'"]/.test(pPath)) pPath = pPath.slice(1, -1)
    else pPath = 'VAR:' + pPath
    const callIdx = cm.index

    // 向前回溯最近的方法定义（2 空格缩进的 key(: / key( ，或 export const/function）
    const before = text.slice(Math.max(0, callIdx - 1200), callIdx)
    const methodRe = /(?:^|\n)\s{2}([A-Za-z_$][\w$]*)\s*[\:(=]|(?:^|\n)export\s+(?:const\s+([A-Za-z_$][\w$]*)\s*=|function\s+([A-Za-z_$][\w$]*)\s*\()/g
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
  out += `| ${idx + 1} | ${r.file} | ${r.exportName} | ${r.method} | ${r.verb} | \`${esc(r.path)}\` | ${esc(r.sig) || '—'} | ⬜ |  |\n`
})

fs.writeFileSync(path.join(__dirname, '..', 'api-list.md'), out)
console.log('rows:', rows.length, 'of files:', files.length)
// 覆盖率报告
const byFile = {}
rows.forEach(r => byFile[r.file] = (byFile[r.file] || 0) + 1)
let miss = 0
for (const f of files) {
  const n = fs.readFileSync(path.join(apiDir, f), 'utf8').match(/http\.(get|post|put|delete|patch)\s*\(/g)?.length || 0
  if (n !== (byFile[f] || 0)) { console.log('COVERAGE-GAP', f, 'http_calls=' + n, 'rows=' + (byFile[f] || 0)); miss++ }
}
console.log('files with gaps:', miss)
