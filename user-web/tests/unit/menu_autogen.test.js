import { describe, it, expect, vi } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'

// 菜单 / 路由自动生成的守护用例（本文件替代原先「查 moduleNames 白名单」的两条判据）。
//
// 为什么换判据：旧实现的根因就是"第二份手工清单"——
//   src/router/index.js 的 moduleNames/pathToModule + Layout.vue 的手写菜单树。
// 文件写了但清单漏登记 ⇒ 点菜单进「页面不存在」（实测 /cap/matrix 挂错模块、
// /tiktok-card-stats/:id 首段无映射）；菜单键漏翻译 ⇒ 界面上印裸 key。
// 现在路由全量 eager 注册、菜单由 router.getRoutes() 生成，判据就必须打在**真路由表**上：
// 这里跑真 router.resolve / 真 buildMenuTree，而不是 grep 源码字符串。

vi.mock('@/utils/initHelper', () => ({ isInitialized: () => true }))
vi.mock('@/stores/user', () => ({
  useUserStore: () => ({ isLoggedIn: true, role: 'admin', userInfo: { id: 1, username: 'probe' } })
}))

import router from '@/router'
import i18n from '@/i18n'
import { buildMenuTree, collectMenuLeaves, filterMenuByRole, findTitleChain } from '@/layout/menu'
import { MENU_SECTIONS, MENU_GROUPS } from '@/layout/menuRegistry'

// jsdom 里 navigator.language 是 en-US，getStoredLocale() 会落到 en —— 断言按 zh 写，
// 这里显式钉住语言，避免用例随运行环境（浏览器语言）漂移。
i18n.global.locale.value = 'zh'

const at = (p) => resolve(process.cwd(), p)
const LOCALES = ['zh', 'en', 'ja', 'de', 'es', 'fr', 'pt', 'ru', 'ar']

const t = i18n.global.t
const te = (key) => i18n.global.te(key) || i18n.global.te(key, 'zh')
const menuTree = buildMenuTree(router.getRoutes(), { t, te })

const flattenTree = (nodes) =>
  (nodes || []).flatMap((n) => [n, ...flattenTree(n.children)])

describe('路由：全量注册，不存在"写了文件却没挂上"的页面', () => {
  const moduleFiles = readdirSync(at('src/router/modules')).filter((f) => f.endsWith('.js'))

  it('每个模块文件导出的静态路由都能在真路由表里解析出来（无白名单可漏）', () => {
    const unresolved = []
    for (const f of moduleFiles) {
      const src = readFileSync(at(`src/router/modules/${f}`), 'utf8')
      const mod = new Function(src.replace(/export\s+default/, 'return'))()
      const walk = (list, base = '') => {
        for (const r of Array.isArray(list) ? list : [list]) {
          if (!r || typeof r.path !== 'string') continue
          const full = r.path.startsWith('/') ? r.path : `${base.replace(/\/$/, '')}/${r.path}`
          // 纯重定向记录（无 component / 无 name，如 /token）不是页面，解析不到名字是正常的
          const isPureRedirect = !r.component && !r.name
          if (!full.includes(':') && !isPureRedirect) {
            const name = router.resolve(full).name
            if (!name || name === 'NotFound') unresolved.push(`${f}: ${full}`)
          }
          if (r.children) walk(r.children, full)
        }
      }
      walk(mod)
    }
    expect(unresolved).toEqual([])
  })

  it('回归：/cap/matrix（能力矩阵，挂在 kbConnector.js）能解析到真页面', () => {
    expect(router.resolve('/cap/matrix').name).toBe('CapabilityMatrix')
    expect(router.resolve('/cap/matrix').matched.length).toBeGreaterThan(0)
  })

  it('回归：/tiktok-card-stats/:id 首段不再落 NotFound', () => {
    expect(router.resolve('/tiktok-card-stats/9').name).toBe('TikTokCardStats')
  })

  it('index.js 里不再有 moduleNames / pathToModule / ensureRouteLoaded 这类手工白名单', () => {
    const src = readFileSync(at('src/router/index.js'), 'utf8')
      .replace(/\/\*[\s\S]*?\*\//g, ' ')
      .split('\n').map((l) => l.replace(/^\s*\/\/.*$/, '')).join('\n')
    expect(src).toMatch(/import\.meta\.glob\(\s*'\.\/modules\/\*\.js'\s*,\s*\{\s*eager:\s*true\s*\}\s*\)/)
    expect(src).not.toMatch(/const\s+moduleNames\s*=/)
    expect(src).not.toMatch(/const\s+pathToModule\s*=/)
    expect(src).not.toMatch(/function\s+ensureRouteLoaded\s*\(/)
  })
})

describe('侧边栏：由路由 meta 自动生成', () => {
  it('Layout.vue 不再有手写菜单树，标题也不再现场拼 t(\'menu.\' + key)', () => {
    // 只判代码不判注释：改造说明里正解释着被废掉的写法，拿注释当证据会自伤。
    const strip = (s) => s.replace(/\/\*[\s\S]*?\*\//g, ' ').split('\n')
      .map((l) => l.replace(/^\s*\/\/.*$/, '')).join('\n')
    const layout = strip(readFileSync(at('src/layout/Layout.vue'), 'utf8'))
    expect(layout).not.toMatch(/const\s+topMenus\s*=\s*ref\(\[/)
    expect(layout).toMatch(/buildMenuTree\(routeRecords\.value/)
    const sub = strip(readFileSync(at('src/components/SubMenuItem.vue'), 'utf8'))
    expect(sub).not.toMatch(/t\(\s*'menu\.'\s*\+/)
    expect(sub).toMatch(/menu\.title/)
    // 面包屑曾经自带第二份硬编码菜单表（topMenusMap / subMenusMap）
    const bc = strip(readFileSync(at('src/components/Breadcrumb.vue'), 'utf8'))
    expect(bc).not.toMatch(/topMenusMap|subMenusMap/)
    expect(bc).toMatch(/findTitleChain/)
  })

  it('菜单入口全部来自带 menuSection 的路由，且每条都能解析到真路由', () => {
    const leaves = collectMenuLeaves(router.getRoutes())
    expect(leaves.length).toBeGreaterThan(150)
    const bad = leaves.filter((l) => (router.resolve(l.path).name || 'NotFound') === 'NotFound')
    expect(bad.map((b) => b.path)).toEqual([])
  })

  it('菜单标题不会是裸 i18n key（menu.capabilityMatrix 这类界面字面量已根除）', () => {
    const titles = flattenTree(menuTree).map((n) => n.title)
    expect(titles.length).toBeGreaterThan(150)
    expect(titles.filter((x) => /^menu\./.test(String(x)))).toEqual([])
    expect(titles.filter((x) => !String(x || '').trim())).toEqual([])
  })

  it('能力矩阵（capabilityMatrix）在菜单里出现且标题正常', () => {
    const node = flattenTree(menuTree).find((n) => n.path === '/cap/matrix')
    expect(node).toBeTruthy()
    expect(node.title).toBe('能力矩阵')
    expect(node.title).not.toContain('menu.')
  })

  it('面包屑能给出从分区到当前页的完整链路', () => {
    const chain = findTitleChain(menuTree, '/geo-tools/alerts')
    expect(chain[0]).toBe('数据分析')
    expect(chain[chain.length - 1]).toBe('预警中心')
  })

  it('角色过滤：非 admin 看不到仅管理员的分区/条目', () => {
    const adminTree = filterMenuByRole(menuTree, 'admin')
    const salesTree = filterMenuByRole(menuTree, 'sales')
    const pathOf = (tree) => new Set(collectPaths(tree))
    expect(pathOf(adminTree).has('/cap/matrix')).toBe(true)
    expect(pathOf(salesTree).has('/cap/matrix')).toBe(false)
    expect(pathOf(salesTree).has('/clue/list')).toBe(true)
  })

  it('分区/分组注册表与路由 meta 对得上（无未登记分组、无孤儿分区）', () => {
    const sectionKeys = new Set(MENU_SECTIONS.map((s) => s.key))
    const groupKeys = new Set(Object.keys(MENU_GROUPS))
    const leaves = collectMenuLeaves(router.getRoutes())
    const problems = []
    for (const l of leaves) {
      if (!sectionKeys.has(l.section)) problems.push(`${l.path} 的 section=${l.section} 未登记`)
      for (const p of l.parents) if (!groupKeys.has(p)) problems.push(`${l.path} 的 parents=${p} 未登记`)
    }
    expect(problems).toEqual([])
    for (const [k, v] of Object.entries(MENU_GROUPS)) {
      if (!sectionKeys.has(v.section)) problems.push(`分组 ${k} 指向未登记分区 ${v.section}`)
    }
    expect(problems).toEqual([])
  })
})

function collectPaths(nodes) {
  return (nodes || []).flatMap((n) => [...(n.path ? [n.path] : []), ...collectPaths(n.children)])
}

describe('i18n：菜单键九语齐备，且非中文语言不再出现中文', () => {
  const locales = Object.fromEntries(
    LOCALES.map((l) => [l, JSON.parse(readFileSync(at(`src/i18n/locales/${l}.json`), 'utf8'))])
  )
  const requiredKeys = [
    ...new Set([
      ...collectMenuLeaves(router.getRoutes()).map((l) => l.key),
      ...MENU_SECTIONS.map((s) => s.key),
      ...Object.keys(MENU_GROUPS),
    ])
  ]

  it('每个菜单键在 9 份语言包里都有非空译文（缺一个就是把键名印到菜单上）', () => {
    const missing = []
    for (const loc of LOCALES) {
      const menu = locales[loc].menu || {}
      for (const k of requiredKeys) {
        if (typeof menu[k] !== 'string' || !menu[k].trim()) missing.push(`${loc}:menu.${k}`)
      }
    }
    expect(missing).toEqual([])
  })

  it('menu.capabilityMatrix / menu.mcpCredentials 九语齐备（本次报障的两个键）', () => {
    for (const loc of LOCALES) {
      expect(locales[loc].menu.capabilityMatrix, `${loc} 缺 menu.capabilityMatrix`).toBeTruthy()
      expect(locales[loc].menu.mcpCredentials, `${loc} 缺 menu.mcpCredentials`).toBeTruthy()
    }
    expect(locales.zh.menu.capabilityMatrix).toBe('能力矩阵')
    expect(locales.en.menu.capabilityMatrix).toBe('Capability Matrix')
  })

  it('非 zh/ja 语言包的 menu 段不得含汉字（曾有脚本把 209 个中文键追加进 9 份文件）', () => {
    const offenders = []
    for (const loc of LOCALES) {
      if (loc === 'zh' || loc === 'ja') continue
      for (const [k, v] of Object.entries(locales[loc].menu || {})) {
        if (/[\u4e00-\u9fff]/.test(String(v))) offenders.push(`${loc}:menu.${k}=${v}`)
      }
    }
    expect(offenders).toEqual([])
  })

  it('语言包里不得再有扁平 "menu.*" 键（带点的键会绕过 menu 段，历史事故来源）', () => {
    const offenders = []
    for (const loc of LOCALES) {
      for (const k of Object.keys(locales[loc])) {
        if (/^menu\./.test(k)) offenders.push(`${loc}:${k}`)
      }
    }
    expect(offenders).toEqual([])
  })

  it('语言包里不残留 [XX] 未翻译占位', () => {
    const offenders = []
    for (const loc of LOCALES) {
      const raw = readFileSync(at(`src/i18n/locales/${loc}.json`), 'utf8')
      const hits = raw.match(/\[(JA|AR|KO|DE|FR|ES|PT|RU|EN)\]/g)
      if (hits) offenders.push(`${loc}:${hits.length}`)
    }
    expect(offenders).toEqual([])
  })
})
