import { describe, it, expect, vi } from 'vitest'
import { readFileSync, readdirSync } from 'node:fs'
import { resolve } from 'node:path'

// 待办中心的路由登记守护用例。
//
// 判据换过一次：旧实现是「按 URL 首段 + moduleNames 白名单懒装配」，
// 文件写好了不等于挂上了，漏登记的表现是"点菜单进 NotFound"；
// 那套白名单本身才是故障源（实测 /cap/matrix 因白名单指向不存在的 capMatrix 模块而 404）。
// 现在 modules/*.js 由 import.meta.glob(..., { eager: true }) 全量注册，
// 判据改打在**真路由表**上：解析得到真页面 + meta 里声明了菜单归属 + 九语译文齐备。

vi.mock('@/utils/initHelper', () => ({ isInitialized: () => true }))
vi.mock('@/stores/user', () => ({
  useUserStore: () => ({ isLoggedIn: true, role: 'admin', userInfo: { id: 1 } })
}))

import router from '@/router'
import i18n from '@/i18n'
import { collectMenuLeaves } from '@/layout/menu'

i18n.global.locale.value = 'zh'

const at = (p) => resolve(process.cwd(), p)
const MODULE_DIR = at('src/router/modules')
const PATH = '/approvalTask/list'
const KEY = 'approvalTask'

const approvalRoute = () => import('@/router/modules/approvalTask.js')

describe('待办中心的路由登记', () => {
  it('模块导出 /approvalTask/list，且组件是懒加载函数', async () => {
    const mod = await approvalRoute()
    expect(Array.isArray(mod.default)).toBe(true)
    const [route] = mod.default
    expect(route.path).toBe(PATH)
    expect(route.name).toBe('ApprovalTaskList')
    expect(typeof route.component).toBe('function')
    expect(route.meta.group).toBe('workspace')
  })

  it('真路由表里能解析到它（文件存在即注册，不再有第二份清单可漏）', () => {
    const resolved = router.resolve(PATH)
    expect(resolved.name).toBe('ApprovalTaskList')
    expect(resolved.matched.length).toBeGreaterThan(0)
    expect(resolved.meta.menuSection).toBe('workspace')
    expect(resolved.meta.menuKey).toBe(KEY)
  })

  it('菜单构建器按 meta 把它放进了工作台分区（而不是靠 Layout.vue 手写一行）', () => {
    const leaf = collectMenuLeaves(router.getRoutes()).find((l) => l.path === PATH)
    expect(leaf, `${PATH} 未进入菜单候选`).toBeTruthy()
    expect(leaf.section).toBe('workspace')
    expect(leaf.key).toBe(KEY)
  })

  it('九份语言包里都有 menu.approvalTask 译文（缺一个就把裸 key 印到菜单上）', () => {
    const locales = ['zh', 'en', 'ja', 'ar', 'es', 'fr', 'de', 'ru', 'pt']
    for (const lang of locales) {
      const menu = JSON.parse(readFileSync(at(`src/i18n/locales/${lang}.json`), 'utf8')).menu
      expect(menu[KEY], `${lang}.json 缺 menu.${KEY}`).toBeTruthy()
    }
  })
})

describe('登记面整体不失配（后续每张前端卡的通用闸门）', () => {
  it('modules 目录被 eager glob 整体覆盖，没有"写了文件却没人登记"的模块', () => {
    const indexSrc = readFileSync(at('src/router/index.js'), 'utf8')
    expect(indexSrc).toMatch(/import\.meta\.glob\(\s*'\.\/modules\/\*\.js'\s*,\s*\{\s*eager:\s*true\s*\}\s*\)/)
    const files = readdirSync(MODULE_DIR).filter((f) => f.endsWith('.js'))
    expect(files.length).toBeGreaterThan(50)
  })

  it('路由 name 全局唯一（重名会让后登记的那条静默顶掉前一条）', () => {
    const routes = import.meta.glob('../../src/router/modules/*.js', { eager: true })
    const names = Object.values(routes).flatMap((mod) => {
      const list = Array.isArray(mod.default) ? mod.default : [mod.default]
      return list.map((r) => r && r.name).filter(Boolean)
    })
    expect(names.length).toBeGreaterThan(0)
    expect(names.filter((n, i) => names.indexOf(n) !== i)).toEqual([])
  })
})
