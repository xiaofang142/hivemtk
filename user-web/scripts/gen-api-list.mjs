#!/usr/bin/env node
// 生成 api-list.md：AST（acorn）扫描 src/api/*.js 中所有 http.<method> 调用
// 用途：API 全量回归索引（每个端点一行，测试后回填状态列）
import fs from 'node:fs'
import path from 'node:path'
import * as acorn from 'acorn'

const apiDir = path.resolve(process.argv[2] || 'src/api')
const outFile = path.resolve(process.argv[3] || '../api-list.md')

const files = fs.readdirSync(apiDir).filter((f) => f.endsWith('.js')).sort()

function strValue(node, consts) {
  if (!node) return null
  if (node.type === 'Literal' && typeof node.value === 'string') return node.value
  // 文件级字符串常量（const base = '/api/bad-cases'）：不解引用就会整条漏收，
  // 而漏收的端点比多收的更危险 —— 回归清单上它压根不出现。
  if (node.type === 'Identifier' && consts && Object.prototype.hasOwnProperty.call(consts, node.name)) {
    return consts[node.name]
  }
  if (node.type === 'TemplateLiteral') {
    let s = ''
    node.quasis.forEach((q, i) => {
      s += q.value.cooked
      if (i < node.expressions.length) {
        const e = node.expressions[i]
        s += `{${e.name || e.property?.name || 'expr'}}`
      }
    })
    return s
  }
  return null
}

function paramsOf(fnNode) {
  const params = (fnNode.params || []).map((p) => {
    if (p.type === 'Identifier') return p.name
    if (p.type === 'ObjectPattern') return '{' + p.properties.map((x) => x.key?.name || '...').join(',') + '}'
    if (p.type === 'AssignmentPattern') return p.left?.name || '?'
    if (p.type === 'RestElement') return '...' + p.argument?.name
    return '?'
  })
  return params.join(', ')
}

const rows = []

for (const file of files) {
  const src = fs.readFileSync(path.join(apiDir, file), 'utf8')
  let ast
  try {
    ast = acorn.parse(src, { ecmaVersion: 'latest', sourceType: 'module', locations: true })
  } catch (e) {
    console.error(`[skip] ${file}: ${e.message}`)
    continue
  }

  // 文件级字符串常量表：先扫一遍顶层 const，供 strValue 解引用
  const consts = {}
  ;(ast.body || []).forEach((n) => {
    if (n.type === 'VariableDeclaration') {
      for (const d of n.declarations || []) {
        if (d.id?.name && d.init?.type === 'Literal' && typeof d.init.value === 'string') {
          consts[d.id.name] = d.init.value
        }
      }
    }
  })

  // 建立父节点索引
  const parents = new Map()
  const stack = [ast]
  while (stack.length) {
    const n = stack.pop()
    for (const k in n) {
      if (k === 'loc' || k === 'range') continue
      const v = n[k]
      if (Array.isArray(v)) {
        for (const c of v) if (c && typeof c.type === 'string') { parents.set(c, n); stack.push(c) }
      } else if (v && typeof v.type === 'string') { parents.set(v, n); stack.push(v) }
    }
  }

  const walk = (node, cb) => {
    cb(node)
    for (const k in node) {
      if (k === 'loc' || k === 'range') continue
      const v = node[k]
      if (Array.isArray(v)) {
        for (const c of v) {
          if (c && c.type) walk(c, cb)
        }
      } else if (v && v.type) {
        walk(v, cb)
      }
    }
  }

  walk(ast, (node) => {
    if (node.type !== 'CallExpression') return
    const callee = node.callee
    if (!callee || callee.type !== 'MemberExpression') return
    const name = callee.object?.name
    if (name !== 'http' && name !== 'request' && name !== 'service' && name !== 'instance') return
    const method = callee.property?.name?.toUpperCase()
    if (!['GET', 'POST', 'PUT', 'DELETE', 'PATCH'].includes(method)) return
    const url = strValue(node.arguments[0], consts)
    if (!url) return

    // 找最近的函数/方法名与形参
    let fn = '-'
    let params = ''
    let cur = parents.get(node)
    while (cur) {
      if (cur.type === 'Property' && cur.value && cur.value.params) {
        fn = cur.key?.name || cur.key?.value || fn
        params = paramsOf(cur.value)
        break
      }
      if (cur.type === 'VariableDeclarator' && cur.init && (cur.init.type === 'ArrowFunctionExpression' || cur.init.type === 'FunctionExpression')) {
        fn = cur.id?.name || fn
        params = paramsOf(cur.init)
        break
      }
      if (cur.type === 'MethodDefinition') {
        fn = cur.key?.name || fn
        params = paramsOf(cur.value)
        break
      }
      if (cur.type === 'PropertyDefinition') {
        fn = cur.key?.name || fn
        break
      }
      if (cur.type === 'ExportDefaultDeclaration' || cur.type === 'Program') break
      cur = parents.get(cur)
    }
    // 命名空间：上层 export const X = {...}
    let obj = '-'
    let p = cur
    while (p) {
      if (p.type === 'VariableDeclarator' && p.id?.name && p.init?.type === 'ObjectExpression') {
        obj = p.id.name
        break
      }
      p = parents.get(p)
    }

    const rest = node.arguments.slice(1)
    const bodyArg = rest[0]
    const optsArg = rest[1]
    const bodyDesc = bodyArg
      ? bodyArg.type === 'Identifier'
        ? bodyArg.name
        : bodyArg.type === 'ObjectExpression'
          ? '{' + bodyArg.properties.map((x) => x.key?.name).join(',') + '}'
          : bodyArg.type
      : ''
    let queryDesc = ''
    if (optsArg?.type === 'ObjectExpression') {
      const q = optsArg.properties.find((x) => x.key?.name === 'params')
      if (q) queryDesc = q.value?.name || 'inline'
    }
    if (!queryDesc && (bodyDesc === 'params' || bodyDesc === '{params}')) queryDesc = 'params'
    const realBody = bodyDesc === 'params' || bodyDesc === '{params}' ? '' : bodyDesc

    rows.push({
      file,
      obj,
      fn,
      method,
      url,
      params,
      body: realBody,
      query: queryDesc,
      line: node.loc.start.line,
    })
  })
}

const authFree = /^\/(api\/(auth\/(login|register|mfa|refresh|logout)|public|tenant\/register|setup|health|visitor|webhook)|files\/)/
// 优先队列：核心骨架（鉴权/租户/权限/系统）先测，其余按文件名排序
const PRIORITY = ['users.js', 'tenant.js', 'permission.js', 'role.js', 'systemUser.js', 'system.js', 'menu.js', 'configParams.js', 'moduleMarket.js', 'platform.js', 'platformAccount.js']
const order = (a, b) => {
  const ia = PRIORITY.indexOf(a), ib = PRIORITY.indexOf(b)
  if (ia >= 0 || ib >= 0) return (ia < 0 ? 999 : ia) - (ib < 0 ? 999 : ib)
  return a.localeCompare(b)
}
const byFile = new Map()
for (const f of [...new Set(rows.map((r) => r.file))].sort(order)) byFile.set(f, [])
for (const r of rows) byFile.get(r.file).push(r)

const L = []
L.push('# API 全量清单（user-web/src/api → user-server）')
L.push('')
L.push(`> 生成：\`node user-web/scripts/gen-api-list.mjs\`　扫描文件：${files.length} 个　端点数：${rows.length} 条`)
L.push('> 状态图例：`☐ 待测` · `✅ 通过` · `🔧 已修复` · `⛔ 阻塞` · `➖ 不适用`（off=独立部署 / on=SaaS 双形态各记一次）')
L.push('> 复核判据：HTTP 状态 + 响应体 `code` + 数据落库（写操作）+ 租户隔离（on 模式）')
L.push('')
L.push('## 汇总')
L.push('')
L.push('| 模块 | 端点数 | 待测 | 通过 | 修复 | 阻塞 |')
L.push('|---|---|---|---|---|---|')
for (const [f, list] of byFile) L.push(`| ${f} | ${list.length} | ${list.length} | 0 | 0 | 0 |`)
L.push(`| **合计** | **${rows.length}** | **${rows.length}** | 0 | 0 | 0 |`)
L.push('')
L.push('## 端点明细')
L.push('')
for (const [f, list] of byFile) {
  L.push(`### ${f}`)
  L.push('')
  L.push('| # | 导出 | 函数 | 方法 | 路径 | 入参 | Body | Query | 鉴权 | 状态 | 备注 |')
  L.push('|---|---|---|---|---|---|---|---|---|---|---|')
  list.forEach((r, i) => {
    const needAuth = authFree.test(r.url) ? '公开' : 'JWT'
    L.push(
      `| ${i + 1} | ${r.obj} | ${r.fn} | ${r.method} | \`${r.url}\` | ${r.params ? '`' + r.params + '`' : '-'} | ${
        r.body ? r.body : '-'
      } | ${r.query ? r.query : '-'} | ${needAuth} | ☐ 待测 | L${r.line} |`
    )
  })
  L.push('')
}

fs.writeFileSync(outFile, L.join('\n'))
console.log(`files=${files.length} endpoints=${rows.length} -> ${outFile}`)
