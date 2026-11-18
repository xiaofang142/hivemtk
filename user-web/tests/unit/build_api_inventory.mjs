import { print as _print } from '../../scripts/lib/stdout.cjs';
// 解析 src/api 下所有文件，抽取每个导出函数 / 对象方法的 HTTP method + url。
// 支持：export function / export const = () => / export const X = { 方法 } / export default { 方法 }
// 输出 tests/API_INVENTORY.json 与人类可读的 tests/API_CHECKLIST.md。
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const apiDir = path.resolve(__dirname, '../../src/api')
const files = fs.readdirSync(apiDir).filter((f) => f.endsWith('.js')).sort()

// 从一段函数/方法源码里抽取 method + url（url 可能是含 ${id} 的模板字符串）
function extractCall(src) {
  const urlM = src.match(/url\s*:\s*(['"`])([\s\S]*?)\1/) || src.match(/url\s*=\s*(['"`])([\s\S]*?)\1/)
  const methodM = src.match(/method\s*:\s*(['"])(get|post|put|delete|patch)\1/i)
  if (urlM && urlM[2] != null) {
    const method = methodM && methodM[2] ? methodM[2].toUpperCase() : 'GET'
    return { method, url: urlM[2] }
  }
  const mCall = src.match(/(?:http|request|axios)\.(get|post|put|delete|patch)\(\s*(['"`])([\s\S]*?)\2/s)
  if (mCall) return { method: mCall[1].toUpperCase(), url: mCall[3] }
  return null
}

// 扫描整个文件，找出所有「方法头」并切出方法体（括号配对）
function parseMethods(code) {
  const out = []
  const re = /([A-Za-z0-9_]+)\s*\(([^)]*)\)\s*\{|([A-Za-z0-9_]+)\s*:\s*(?:\([^)]*\)|[A-Za-z0-9_]+)\s*=>\s*\{/g
  let m
  while ((m = re.exec(code))) {
    const name = m[1] || m[3]
    const braceIdx = code.indexOf('{', m.index)
    if (braceIdx < 0) continue
    let depth = 0
    let j = braceIdx
    for (; j < code.length; j++) {
      if (code[j] === '{') depth++
      else if (code[j] === '}') { depth--; if (depth === 0) break }
    }
    out.push({ name, src: code.slice(braceIdx, j + 1) })
    re.lastIndex = j + 1
  }
  return out
}

const inventory = []
for (const file of files) {
  const full = path.join(apiDir, file)
  const code = fs.readFileSync(full, 'utf8')
  const units = []
  // 1) export function NAME
  let m
  const reFn = /export\s+(?:async\s+)?function\s+([A-Za-z0-9_]+)/g
  while ((m = reFn.exec(code))) units.push({ name: m[1], src: code.slice(m.index) })
  // 2) export const NAME = ... （可能是箭头函数，也可能是对象字面量）
  const reConst = /export\s+(?:const|let|var)\s+([A-Za-z0-9_]+)\s*=/g
  while ((m = reConst.exec(code))) {
    const eq = code.indexOf('=', m.index)
    const after = code.slice(eq + 1)
    if (/^\s*\{/.test(after)) {
      // 对象字面量：解析其每个方法
      for (const mm of parseMethods(after)) units.push({ name: `${m[1]}.${mm.name}`, src: mm.src })
    } else {
      units.push({ name: m[1], src: after })
    }
  }
  // 3) export default {
  const reDef = /export\s+default\s*\{/g
  while ((m = reDef.exec(code))) {
    const after = code.slice(m.index + m[0].length - 1)
    for (const mm of parseMethods(after)) units.push({ name: mm.name, src: mm.src, isDefault: true })
  }
  for (const u of units) {
    const call = extractCall(u.src)
    if (call) {
      inventory.push({
        file, function: u.name, method: call.method, url: call.url,
        hasPathParam: /\$\{[^}]+\}/.test(call.url),
      })
    }
  }
}

const out = path.resolve(__dirname, '../API_INVENTORY.json')
fs.writeFileSync(out, JSON.stringify(inventory, null, 2))
_print(`解析完成：${files.length} 个文件，${inventory.length} 个接口`)

const byFile = {}
for (const it of inventory) (byFile[it.file] ||= []).push(it)
let md = `# user-web API 接口清单（${inventory.length} 个）\n\n> 每个接口含 文件 / 函数 / method / url。\n\n`
let n = 0
for (const f of Object.keys(byFile).sort()) {
  md += `## ${f}（${byFile[f].length}）\n\n| # | 函数 | method | url | 行 |\n|---|------|--------|-----|----|\n`
  for (const it of byFile[f]) { n++; md += `| ${n} | \`${it.function}\` | ${it.method} | \`${it.url}\` | - |\n` }
  md += '\n'
}
fs.writeFileSync(path.resolve(__dirname, '../API_CHECKLIST.md'), md)
_print(`清单已写入 ${path.resolve(__dirname, '../API_CHECKLIST.md')}`)
