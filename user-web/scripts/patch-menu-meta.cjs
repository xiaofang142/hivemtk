// 把菜单结构（scripts/.gen/menu-structure.json）写回各路由模块的 meta：
//   menuKey / menuSection / menuParents / menuOrder  -> 出现在侧边栏的入口页
//   menuHidden: true                                 -> 详情/编辑等不进菜单的页
// 用 acorn 解析 AST 定位 meta 对象，MagicString 精确插入，最后重解析校验。
// 用法：node scripts/patch-menu-meta.cjs [--dry]
const fs = require('fs')
const path = require('path')
const acorn = require('acorn')

const ROOT = path.resolve(__dirname, '..')
const DRY = process.argv.includes('--dry')
const MOD_DIR = path.join(ROOT, 'src/router/modules')
const structure = JSON.parse(fs.readFileSync(path.join(ROOT, 'scripts/.gen/menu-structure.json'), 'utf8'))

const byPath = new Map()
for (const leaf of structure.leaves) byPath.set(leaf.path, leaf)

// 先记录现有 meta.icon，用于对比
const iconDiff = []
const applied = []
const unmatched = []

function joinPath(base, p) {
  if (p.startsWith('/')) return p
  return (base.replace(/\/$/, '') + '/' + p).replace(/\/{2,}/g, '/')
}

function propName(node) {
  if (node.key.type === 'Identifier') return node.key.name
  if (node.key.type === 'Literal') return String(node.key.value)
  return null
}

function walkObject(node, base, file, out) {
  if (!node || node.type !== 'ObjectExpression') return
  const props = {}
  for (const p of node.properties) {
    if (p.type !== 'Property') continue
    props[propName(p)] = p
  }
  const pathProp = props.path
  let fullPath = null
  if (pathProp && pathProp.value.type === 'Literal') {
    fullPath = joinPath(base, String(pathProp.value.value))
  }
  if (fullPath) out.push({ fullPath, metaProp: props.meta, obj: node, file })
  // 递归 children
  const children = props.children
  if (children && children.value.type === 'ArrayExpression') {
    for (const el of children.value.elements) {
      if (el && el.type === 'ObjectExpression') walkObject(el, fullPath || base, file, out)
    }
  }
}

for (const f of fs.readdirSync(MOD_DIR).filter((n) => n.endsWith('.js'))) {
  const file = path.join(MOD_DIR, f)
  const src = fs.readFileSync(file, 'utf8')
  const ast = acorn.parse(src, { ecmaVersion: 'latest', sourceType: 'module' })
  const decl = ast.body.find((n) => n.type === 'ExportDefaultDeclaration')
  if (!decl) { console.log('SKIP (no export default):', f); continue }
  const records = []
  const value = decl.declaration
  if (value.type === 'ArrayExpression') {
    for (const el of value.elements) if (el && el.type === 'ObjectExpression') walkObject(el, '', f, records)
  } else if (value.type === 'ObjectExpression') {
    walkObject(value, '', f, records)
  }

  const inserts = [] // { pos, text }
  for (const r of records) {
    const leaf = byPath.get(r.fullPath)
    const fields = []
    if (leaf) {
      fields.push(`menuKey: '${leaf.key}'`)
      fields.push(`menuSection: '${leaf.section}'`)
      if (leaf.parents.length) fields.push(`menuParents: ${JSON.stringify(leaf.parents)}`)
      fields.push(`menuOrder: ${leaf.order}`)
      if (leaf.roles && leaf.roles.length) fields.push(`menuRoles: ${JSON.stringify(leaf.roles)}`)
      if (leaf.icon) fields.push(`menuIcon: '${leaf.icon}'`)
      applied.push(`${r.fullPath} <- ${leaf.key}`)
    } else {
      fields.push('menuHidden: true')
    }
    if (!r.metaProp) { unmatched.push(`${f}:${r.fullPath} 无 meta 字段`); continue }
    const metaVal = r.metaProp.value
    if (metaVal.type !== 'ObjectExpression') { unmatched.push(`${f}:${r.fullPath} meta 非对象字面量`); continue }
    const iconProp = metaVal.properties.find((p) => p.type === 'Property' && propName(p) === 'icon')
    if (leaf && iconProp && iconProp.value.type === 'Literal' && iconProp.value.value !== leaf.icon) {
      iconDiff.push(`${r.fullPath}: meta.icon=${iconProp.value.value} tree.icon=${leaf.icon}`)
    }
    // 已存在则跳过（幂等）
    const existing = new Set(metaVal.properties.filter((p) => p.type === 'Property').map(propName))
    const toAdd = fields.filter((t) => !existing.has(t.split(':')[0]))
    if (!toAdd.length) continue
    inserts.push({ pos: metaVal.start + 1, text: ' ' + toAdd.join(', ') + ',' })
  }

  if (!inserts.length) continue
  // 从后往前插入
  inserts.sort((a, b) => b.pos - a.pos)
  let out = src
  for (const ins of inserts) out = out.slice(0, ins.pos) + ins.text + out.slice(ins.pos)
  // 校验仍是合法 ESM
  acorn.parse(out, { ecmaVersion: 'latest', sourceType: 'module' })
  if (DRY) {
    console.log(`--- ${f} (+${inserts.length} 处) ---`)
  } else {
    fs.writeFileSync(file, out)
  }
}

console.log(`\n菜单叶子匹配 = ${applied.length}/${byPath.size}`)
const missLeaf = structure.leaves.filter((l) => !applied.some((a) => a.startsWith(l.path + ' ')))
console.log('未匹配到路由的菜单叶子:', missLeaf.map((l) => l.path))
console.log('icon 差异数 =', iconDiff.length)
iconDiff.slice(0, 40).forEach((d) => console.log('   ' + d))
console.log('异常记录:', unmatched.length)
unmatched.forEach((u) => console.log('   ' + u))
