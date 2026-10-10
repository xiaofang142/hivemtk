// 由 scripts/.gen/menu-structure.json 生成 src/layout/menuRegistry.js
const fs = require('fs')
const path = require('path')
const ROOT = path.resolve(__dirname, '..')
const s = JSON.parse(fs.readFileSync(path.join(ROOT, 'scripts/.gen/menu-structure.json'), 'utf8'))

const q = (v) => JSON.stringify(v)
const rolesLine = (r) => (r && r.length ? `, roles: ${JSON.stringify(r)}` : '')

const sections = s.sections
  .sort((a, b) => a.order - b.order)
  .map((x) => `  { key: ${q(x.key)}, icon: ${q(x.icon)}, order: ${x.order}, fallback: ${q(x.fallback)}${rolesLine(x.roles)} },`)
  .join('\n')

const groups = s.groups
  .sort((a, b) => a.section.localeCompare(b.section) || a.parents.join('/').localeCompare(b.parents.join('/')) || a.order - b.order)
  .map((g) => {
    const parents = g.parents.length ? `, parents: ${JSON.stringify(g.parents)}` : ''
    return `  ${g.key}: { icon: ${q(g.icon)}, section: ${q(g.section)}${parents}, order: ${g.order}, fallback: ${q(g.fallback)}${rolesLine(g.roles)} },`
  })
  .join('\n')

const header = `// 侧边栏「分区 / 分组」注册表
// ---------------------------------------------------------------------------
// 这里只登记**分组骨架**（顶层分区 section 与中间分组 group）的图标、顺序、可见角色。
// 菜单叶子（真正可点的页面）不在这里维护：它们全部来自路由 meta ——
//   meta.menuKey      i18n 键名（用于 t('menu.<key>')，缺失自动回退 meta.title）
//   meta.menuSection  所属顶层分区（对应 MENU_SECTIONS[].key）
//   meta.menuParents  可选的中间分组键数组，从外到内（最多三层）
//   meta.menuOrder    同级排序
//   meta.menuIcon     菜单图标（不写则用 meta.icon）
//   meta.menuRoles    菜单可见角色（不写则按 meta.requiresAdmin 推导）
//   meta.menuHidden   true 表示该页不进菜单（详情/编辑等）
// 新页面接入 = 在 src/router/modules/*.js 的 meta 里写上述字段，本文件不用改。
// 未登记的 menuParents 键会按 key 自动兜底渲染（icon 用 Menu、标题走 menu.<key>、
// 再回退到 key），所以「漏登记」只会影响外观，不会让页面从菜单里消失或显示裸 key。
// 本文件由 scripts/gen-menu-structure.cjs 从改造前的手写菜单树一次性生成（保持原有 IA 与顺序）。
// ---------------------------------------------------------------------------

export const MENU_SECTIONS = [
${sections}
]

export const MENU_GROUPS = {
${groups}
}

export const DEFAULT_GROUP_ICON = 'Menu'
export const DEFAULT_LEAF_ICON = 'Document'
`

fs.writeFileSync(path.join(ROOT, 'src/layout/menuRegistry.js'), header)
console.log('written src/layout/menuRegistry.js', header.length, 'bytes')
