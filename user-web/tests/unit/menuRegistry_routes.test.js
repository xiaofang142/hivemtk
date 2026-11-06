import { describe, it, expect } from 'vitest'
import { readdirSync, readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { backendMenuItems, backendLeafMenuItems } from './menuRegistry-helper'
import { routeIconMap } from '@/utils/iconMap'

// 后端菜单注册表 ↔ 前端路由 ↔ 图标表 三者的一致性。
//
// 为什么要有这一整份用例：菜单在 RBAC 改造后由后端下发，权威注册表是
// user-server/internal/model/menu.go 的 var SystemMenuTree，前端 Layout.vue 里
// 已经没有静态菜单常量了。于是前端加一条路由**不会自动出现在左侧菜单里**——
// 页面能通过 URL 直接进，但没有任何入口点得到它；反过来菜单里写一个不存在的
// path，点进去就是 NotFound。两侧都是「功能在但没人找得到」。
//
// 2026-10-12 加这个文件时发现的真实缺口：
//   1. /system/dictionaries、/system/feature-flags 加了路由和后端接口，菜单里没有；
//   2. 12 个图标名有效但没登记进 routeIconMap，3 个（Cloud/QrCode/Shield）
//      在 @element-plus/icons-vue 里根本不存在 —— 一共 15 个菜单项当时都在
//      显示兜底的 Document 字形，语义完全丢失。
//
// 为什么直接 import 路由模块、递归展开 children，而不是读 router.getRoutes()：
// 本仓路由是懒加载的（router.beforeEach 里 ensureRouteLoaded → addRoute），
// 刚 import 时 getRoutes() 只有 13 条骨架，任何"菜单 path 是不是都有路由"的断言
// 都会把还没加载的路由误判成缺失。反过来用 router.push 去触发加载也不行：
// beforeEach 带登录守卫，未登录会被弹到 /login，于是每个 path 的 matched
// 都变成登录页，断言同样失真。模块导入没有这两个坑。
//
// 为什么不用正则扫 src/router/modules/*.js 的 path 字面量：
// 模块里大量用嵌套相对路由（browserAutomation 的 path: 'tasks' 挂在
// /browser-automation 之下），正则只看得到 '/browser-automation' 看不到
// '/browser-automation/tasks'，会把已存在的路由误判成缺失。

const MODULES_DIR = resolve(process.cwd(), 'src/router/modules')
const MENU_GO = resolve(process.cwd(), '../user-server/internal/model/menu.go')

/** 递归展开模块里的全部路由，返回 vue-router 解析后的完整 path。 */
const walkRoutes = (routes, prefix = '') =>
  routes.flatMap((r) => {
    const full = r.path.startsWith('/') ? r.path : `${prefix}/${r.path}`.replace(/\/{2,}/g, '/')
    const kids = Array.isArray(r.children) && r.children.length ? walkRoutes(r.children, full) : []
    return [full, ...kids]
  })

const allRoutePaths = async () => {
  const out = new Set()
  for (const file of readdirSync(MODULES_DIR)) {
    if (!file.endsWith('.js')) continue
    const mod = await import(/* @vite-ignore */ `${MODULES_DIR}/${file}`)
    const list = Array.isArray(mod.default) ? mod.default : [mod.default]
    for (const r of list.filter(Boolean)) walkRoutes([r]).forEach((p) => out.add(p))
  }
  return out
}

/** 后端注册表里的叶子菜单（分组节点自带 Path 指向默认子页，与子项同 path 是有意设计）。 */
const leafMenuItems = () => backendLeafMenuItems()

/** menu.go 里用到的全部图标名（含父节点，去重）。 */
const menuIconNames = () => {
  const src = readFileSync(MENU_GO, 'utf8')
  const block = src.match(/var\s+SystemMenuTree\s*=\s*\[\]SystemMenuNode\{([\s\S]*?)\n\}/)
  if (!block) throw new Error(`menu.go 里找不到 var SystemMenuTree（读的是 ${MENU_GO}）`)
  return [...new Set([...block[1].matchAll(/Icon:\s*"([^"]+)"/g)].map((m) => m[1]))]
}

describe('后端菜单注册表 ↔ 前端路由', () => {
  let routePaths = new Set()

  it('两边都非空（读不到内容时后面的断言会全部真空通过，这里先卡一道）', async () => {
    routePaths = await allRoutePaths()
    expect(leafMenuItems().length).toBeGreaterThan(20)
    expect(routePaths.size).toBeGreaterThan(50)
  })

  it('菜单里每一条 path 都能在前端路由里找到（找不到 = 菜单点进去进 NotFound）', () => {
    const orphans = leafMenuItems()
      .map((it) => it.path)
      .filter((p) => !routePaths.has(p))
      .sort()
    expect(
      orphans,
      `这些菜单 path 在前端路由表里不存在：\n${orphans.join('\n')}\n` +
        `处理办法二选一：给 src/router/modules/*.js 补路由，或把 menu.go 这条摘掉——` +
        `挂着指向空处的菜单项，比没有这一项更难排查。`
    ).toEqual([])
  })

  it('控制台里的配置页都登记进了菜单（只加前端路由的话，页面进得去但菜单点不到）', () => {
    // 这一条不能写成"所有前端路由都必须在菜单里"：详情页、编辑页本来就是
    // 从列表点进去的，不该出现在菜单。所以只钉住「控制台家族」这组入口，
    // 新增一个配置页就必须把它列进来——列进来的那一刻，这条断言就守着它。
    const MUST_BE_IN_MENU = [
      '/system/config-params',  // 动态阈值参数
      '/system/dictionaries',  // 业务字典
      '/system/feature-flags'  // 功能开关
    ]
    const menuPaths = new Set(leafMenuItems().map((it) => it.path))
    const missing = MUST_BE_IN_MENU.filter((p) => !routePaths.has(p))
    expect(missing, '这些控制台页面连前端路由都不存在').toEqual([])
    const notInMenu = MUST_BE_IN_MENU.filter((p) => !menuPaths.has(p))
    expect(
      notInMenu,
      `这些控制台页面没有登记进 user-server/internal/model/menu.go：${notInMenu.join(', ')}\n` +
        `菜单已改为后端驱动（GET /api/menus），只加前端路由不会出现在左侧菜单里。`
    ).toEqual([])
  })

  it('菜单 path 没有重复登记（同一个入口挂两次会让后注册的静默顶掉先注册的）', () => {
    const seen = new Map()
    const dups = []
    for (const it of leafMenuItems()) {
      if (seen.has(it.path)) dups.push(`${it.path}（${seen.get(it.path)} vs ${it.key}）`)
      else seen.set(it.path, it.key)
    }
    expect(dups).toEqual([])
  })

  it('菜单里没有参数化或通配 path（那种形态点进去必然进 NotFound）', () => {
    const bad = leafMenuItems()
      .map((it) => it.path)
      .filter((p) => p.includes(':') || p.includes('*'))
      .sort()
    expect(bad).toEqual([])
  })
})

describe('菜单标题 i18n 覆盖', () => {
  // Layout.vue / SubMenuItem.vue 渲染标题走 t('menu.' + key)。vue-i18n 对缺失的
  // key 不报错、不抛异常，直接把 key 原样当文案显示出来——2026-10-12 实测
  // systemDictionaries、systemFeatureFlags 两条在侧边栏显示成
  // 「menu.systemDictionaries」。上一份用例管住了 path 与图标，没有管标题，
  // 所以这两条一路全绿进到了页面上。
  //
  // 九语种（zh/en/ja/de/ru/pt/fr/es/ar）是产品承诺的语言矩阵，不是「恰好有九个
  // JSON 文件」：临时调试文件忘删、某语言包被误删、某语言缺新菜单的 key——
  // 这些都不会让页面崩，只会让某语言用户看到裸 key 或英文兜底。所以这里
  // 三道闸一起上：文件集合精确等于九个、每个后端菜单 key 都有非空标题、
  // 标题不许是占位符（值等于裸 key 说明根本没翻译）。
  const EXPECTED_LOCALES = ['ar', 'de', 'en', 'es', 'fr', 'ja', 'pt', 'ru', 'zh']
  const localesDir = resolve(process.cwd(), 'src/i18n/locales')

  // 为什么包一层而不是直接 JSON.parse：解析失败抛的是原生 SyntaxError，
  // 报错里看不出是哪个语言包坏了；包一层把文件名带进错误信息，坏文件一眼定位。
  // （JSON 坏了的文件在后面的用例里也会红——这里只是让红得可读。）
  const loadLocaleJson = (locale) => {
    const file = `${locale}.json`
    try {
      return JSON.parse(readFileSync(resolve(localesDir, file), 'utf8'))
    } catch (err) {
      throw new Error(`${file} 不是合法 JSON，语言包加载会失败：${err.message}`, { cause: err })
    }
  }

  // 注意这里返回的是语言代码（去掉 .json 后缀），与 EXPECTED_LOCALES 直接可比。
  const localeFiles = () =>
    readdirSync(localesDir)
      .filter((f) => f.endsWith('.json'))
      .map((f) => f.replace(/\.json$/, ''))
      .sort()

  it('语言包文件集合精确等于九语种 zh/en/ja/de/ru/pt/fr/es/ar（多一个少一个都是矩阵破损）', () => {
    const actual = localeFiles()
    expect(
      actual,
      `语言包必须是精确的九个：${EXPECTED_LOCALES.join('、')}。\n` +
        `多出来的通常是临时调试文件忘了删；少掉的那个语言会在页面上静默退回兜底文案。`
    ).toEqual(EXPECTED_LOCALES)
  })

  it('注册表里每个菜单 key 在全部语言包都有非空标题（缺一个就渲染成裸 key）', () => {
    const keys = backendMenuItems().map((it) => it.key).filter(Boolean)
    expect(keys.length, '读不到后端菜单 key，后面的断言会真空通过').toBeGreaterThan(20)

    // 按「语言包 → 缺失 key 列表」汇总成一次断言：以前逐文件断言时，第一个
    // 红的语言包会吞掉后面语言包的报错，一次只能看见一个文件的缺口。
    const gaps = []
    for (const file of localeFiles()) {
      const menuLabels = loadLocaleJson(file).menu || {}
      const missing = keys.filter((k) => typeof menuLabels[k] !== 'string' || !menuLabels[k].trim()).sort()
      if (missing.length) gaps.push(`${file}: ${missing.join(', ')}`)
    }
    expect(
      gaps,
      `以下语言包的 menu 段缺这些标题：\n${gaps.join('\n')}\n` +
        `缺 key 时页面不报错，直接把「menu.xxx」显示在菜单上。\n` +
        `补文案时注意：值不许是裸 key 本身，也不许留空白字符串——那是占位符不是翻译。`
    ).toEqual([])
  })

  it('标题值不许是裸 key（值等于 key 或 menu.key 说明是占位符，根本没翻译）', () => {
    // 缺 key 会渲染成裸 key，上一条闸住了；但另一种偷懒是补上 key 却把值写成
    // key 本身或「menu.xxx」——不缺 key 了，页面上显示的仍然是裸 key。
    const keys = backendMenuItems().map((it) => it.key).filter(Boolean)
    const placeholders = []
    for (const file of localeFiles()) {
      const menuLabels = loadLocaleJson(file).menu || {}
      for (const k of keys) {
        if (menuLabels[k] === k || menuLabels[k] === `menu.${k}`) placeholders.push(`${file}: ${k}`)
      }
    }
    expect(
      placeholders,
      `以下语言包的这些标题值是占位符（值等于裸 key），不是真翻译：\n${placeholders.join('\n')}`
    ).toEqual([])
  })
})

describe('菜单图标', () => {
  it('注册表里用到的每个图标名都登记在 routeIconMap 里（否则静默显示成兜底图标）', () => {
    // resolveRouteIcon 对未登记的名字兜底成 Document，所以「不报错、菜单照常显示」，
    // 只是那个图标跟语义完全无关——比如「安全审计」显示成一张文档。
    // 这类错没人会报，因为它不崩；只能靠这道门把它挡住。
    // 另外三个名字（Cloud / QrCode / Shield）在 @element-plus/icons-vue 里压根不存在，
    // 属于拼错的，已在 menu.go 里换成有效的等价图标。
    const names = menuIconNames()
    expect(names.length).toBeGreaterThan(20)
    const unregistered = names.filter((n) => !routeIconMap[n]).sort()
    expect(
      unregistered,
      `这些图标名不在 user-web/src/utils/iconMap.js 的 routeIconMap 里：${unregistered.join(', ')}\n` +
        `处理办法二选一：给 routeIconMap 补上（须是 @element-plus/icons-vue 里真实存在的名字），` +
        `或把 menu.go 改成已登记的名字。`
    ).toEqual([])
  })
})
