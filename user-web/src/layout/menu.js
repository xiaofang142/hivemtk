// 侧边栏菜单构建器（纯函数，便于单测）
// ---------------------------------------------------------------------------
// 事实源只有一个：路由表（router.getRoutes()）。本文件不含任何页面清单，
// 页面是否进菜单、进哪个分区、排第几，全部读路由 meta：
//   meta.menuKey      i18n 键名（t('menu.<key>')，缺失回退 meta.title）
//   meta.menuSection  顶层分区 key
//   meta.menuParents  中间分组 key 数组（从外到内）
//   meta.menuOrder    同级排序
//   meta.menuIcon     菜单图标，缺省 meta.icon
//   meta.menuRoles    可见角色，缺省按 meta.requiresAdmin 推导
//   meta.menuHidden   true = 不进菜单（详情/编辑页）
// 分区/分组的图标与顺序来自 menuRegistry.js；未登记的分组按 key 兜底渲染，
// 因此「新页面漏登记分组」只影响外观，不会让页面从菜单消失或把裸 key 印到界面上。
import { MENU_SECTIONS, MENU_GROUPS, DEFAULT_GROUP_ICON, DEFAULT_LEAF_ICON } from './menuRegistry'

const asArray = (v) => (Array.isArray(v) ? v : [])

/** 从路由表抽出所有「应进菜单」的叶子入口 */
export function collectMenuLeaves(routeRecords) {
  const seen = new Set()
  const leaves = []
  for (const r of asArray(routeRecords)) {
    const m = r.meta || {}
    if (m.menuHidden || m.hideLayout || m.public) continue
    if (!m.menuSection) continue
    if (seen.has(r.path)) continue
    seen.add(r.path)
    const menuKey = m.menuKey || null
    leaves.push({
      key: menuKey || String(r.name || r.path),
      titleKey: menuKey ? `menu.${menuKey}` : null,
      // 有些旧路由把 meta.title 直接写成 i18n 键（如 'role.title'），这里一并兼容
      fallbackTitle: typeof m.title === 'string' ? m.title : r.path,
      path: r.path,
      section: m.menuSection,
      parents: asArray(m.menuParents),
      order: typeof m.menuOrder === 'number' ? m.menuOrder : 9999,
      icon: m.menuIcon || m.icon || DEFAULT_LEAF_ICON,
      roles: asArray(m.menuRoles).length ? m.menuRoles : (m.requiresAdmin ? ['admin'] : null),
    })
  }
  return leaves
}

/**
 * 生成侧边栏树。
 * @param {Array} routeRecords router.getRoutes()
 * @param {{ t: Function, te: Function }} i18n vue-i18n 的 t / te（te 判定译文是否存在）
 */
export function buildMenuTree(routeRecords, { t, te }) {
  const translate = (key, fallback) => {
    if (key && typeof te === 'function' && te(key)) return t(key)
    if (fallback && fallback.includes('.') && typeof te === 'function' && te(fallback)) return t(fallback)
    return fallback || key || ''
  }

  const sections = new Map()
  const sectionOrder = new Map()
  MENU_SECTIONS.forEach((s, i) => sectionOrder.set(s.key, typeof s.order === 'number' ? s.order : i + 1))

  const ensureSection = (key) => {
    if (!sections.has(key)) {
      const def = MENU_SECTIONS.find((s) => s.key === key) || {}
      sections.set(key, {
        key,
        type: 'section',
        title: translate(`menu.${key}`, def.fallback),
        icon: def.icon || DEFAULT_GROUP_ICON,
        roles: def.roles || null,
        order: sectionOrder.get(key) || 9999,
        children: [],
        groupIndex: new Map(),
      })
    }
    return sections.get(key)
  }

  const ensureGroup = (sectionNode, key, parentGroup) => {
    if (sectionNode.groupIndex.has(key)) return sectionNode.groupIndex.get(key)
    const def = MENU_GROUPS[key] || {}
    const node = {
      key,
      type: 'group',
      title: translate(`menu.${key}`, def.fallback),
      icon: def.icon || DEFAULT_GROUP_ICON,
      roles: def.roles || null,
      order: typeof def.order === 'number' ? def.order : 9999,
      children: [],
      groupIndex: new Map(),
    }
    sectionNode.groupIndex.set(key, node)
    ;(parentGroup ? parentGroup.children : sectionNode.children).push(node)
    return node
  }

  for (const leaf of collectMenuLeaves(routeRecords)) {
    const sectionNode = ensureSection(leaf.section)
    let cursor = null
    for (const parentKey of leaf.parents) {
      cursor = ensureGroup(sectionNode, parentKey, cursor)
    }
    const node = {
      key: leaf.key,
      type: 'leaf',
      title: translate(leaf.titleKey, leaf.fallbackTitle),
      icon: leaf.icon,
      path: leaf.path,
      roles: leaf.roles,
      order: leaf.order,
      children: null,
    }
    ;(cursor ? cursor.children : sectionNode.children).push(node)
  }

  // 未登记顺序的分组按「子项最小 order」兜底，保证顺序稳定
  const finalize = (node) => {
    if (!node.children) return { ...node }
    const children = node.children.map(finalize)
    let order = node.order
    if (node.type === 'group' && order === 9999 && children.length) {
      order = Math.min(...children.map((c) => (typeof c.order === 'number' ? c.order : 9999)))
    }
    children.sort((a, b) => a.order - b.order || String(a.title || a.key).localeCompare(String(b.title || b.key)))
    return { key: node.key, type: node.type, icon: node.icon, roles: node.roles, title: node.title, order, children }
  }

  return [...sections.values()]
    .sort((a, b) => a.order - b.order || a.key.localeCompare(b.key))
    .map(finalize)
    .filter((s) => s.children.length > 0)
}

/** 角色过滤：admin 全通；node.roles 为空表示不限制；子项全被过滤掉的分组/分区自动隐藏 */
export function filterMenuByRole(nodes, role) {
  const allowed = (node) => {
    if (!node.roles || node.roles.length === 0) return true
    if (role === 'admin') return true
    return node.roles.includes(role)
  }
  const walk = (list) =>
    asArray(list)
      .filter(allowed)
      .map((node) => {
        if (!node.children) return node
        const children = walk(node.children)
        return children.length ? { ...node, children } : null
      })
      .filter(Boolean)
  return walk(nodes)
}

/** 在树里找包含 path 的分区，并给出从分区到叶子的标题链（面包屑） */
export function findSectionByPath(tree, path) {
  for (const section of asArray(tree)) {
    if (sectionContainsPath(section, path)) return section
  }
  return null
}

export function sectionContainsPath(node, path) {
  if (node.path && node.path === path) return true
  return asArray(node.children).some((c) => sectionContainsPath(c, path))
}

export function findTitleChain(tree, path, trail = []) {
  for (const node of asArray(tree)) {
    const next = trail.concat(node.title || node.key)
    if (node.path && node.path === path) return next
    const found = findTitleChain(node.children, path, next)
    if (found) return found
  }
  return null
}
