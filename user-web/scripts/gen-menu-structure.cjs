// 从 Layout.vue 手写菜单树抽取「分组/叶子」结构，生成
//   1) src/router/menuRegistry.js 的分组注册表内容
//   2) 各路由模块的菜单 meta 补丁指令（交由 patch-menu-meta.mjs 写回）
// 用法：node scripts/gen-menu-structure.cjs
const fs = require('fs')
const path = require('path')

const ROOT = path.resolve(__dirname, '..')
const layout = fs.readFileSync(path.join(ROOT, 'src/layout/Layout.vue'), 'utf8')

const start = layout.indexOf('const topMenus = ref(')
if (start === -1) throw new Error('topMenus not found')
const arrStart = layout.indexOf('[', start)
// 从 arrStart 起做括号配对（字符串里的括号需跳过）
let depth = 0
let end = -1
let inStr = null
for (let i = arrStart; i < layout.length; i++) {
  const ch = layout[i]
  if (inStr) {
    if (ch === '\\') { i++; continue }
    if (ch === inStr) inStr = null
    continue
  }
  if (ch === "'" || ch === '"' || ch === '`') { inStr = ch; continue }
  if (ch === '[') depth++
  else if (ch === ']') { depth--; if (depth === 0) { end = i; break } }
}
if (end === -1) throw new Error('topMenus array not closed')
const tree = new Function('return ' + layout.slice(arrStart, end + 1))()

const sections = []
const groups = []
const leaves = []

function walk(nodes, sectionKey, parents) {
  nodes.forEach((node, idx) => {
    const order = idx + 1
    if (node.children && node.children.length) {
      if (parents.length === 0 && !sectionKey) {
        sections.push({ key: node.key, icon: node.icon, fallback: node.title, roles: node.roles || null, order })
        walk(node.children, node.key, [])
      } else {
        groups.push({ key: node.key, icon: node.icon, fallback: node.title, roles: node.roles || null, section: sectionKey, parents: [...parents], order })
        walk(node.children, sectionKey, [...parents, node.key])
      }
    } else {
      leaves.push({
        key: node.key,
        title: node.title,
        icon: node.icon,
        path: node.path,
        roles: node.roles || null,
        section: sectionKey,
        parents: [...parents],
        order,
      })
    }
  })
}
walk(tree, null, [])

const out = { sections, groups, leaves }
fs.mkdirSync(path.join(ROOT, 'scripts/.gen'), { recursive: true })
fs.writeFileSync(path.join(ROOT, 'scripts/.gen/menu-structure.json'), JSON.stringify(out, null, 2))
console.log(`sections=${sections.length} groups=${groups.length} leaves=${leaves.length}`)
const dupPaths = leaves.map((l) => l.path).filter((p, i, a) => a.indexOf(p) !== i)
console.log('重复 path:', dupPaths.length ? dupPaths : '无')
const dupKeys = leaves.map((l) => l.key).filter((k, i, a) => a.indexOf(k) !== i)
console.log('重复 key:', dupKeys.length ? dupKeys : '无')
