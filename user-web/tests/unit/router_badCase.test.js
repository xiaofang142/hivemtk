import { describe, it, expect, vi } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

// Bad Case 队列的路由登记守护用例（镜像 tests/unit/router_approvalTask.test.js 的判据）。
//
// 历史上判据是"index.js 的 moduleNames 里要出现 badCase" —— 那套手工白名单已废除
// （它既是"漏登记⇒进 NotFound"的检查项，也是"白名单指向不存在的模块⇒必然 404"的故障源，
// 见 /cap/matrix）。现在路由全量 eager 注册、菜单由 meta 生成，判据打在真路由表上。

vi.mock('@/utils/initHelper', () => ({ isInitialized: () => true }))
vi.mock('@/stores/user', () => ({
  useUserStore: () => ({ isLoggedIn: true, role: 'admin', userInfo: { id: 1 } })
}))

import router from '@/router'
import i18n from '@/i18n'
import { collectMenuLeaves } from '@/layout/menu'

i18n.global.locale.value = 'zh'

const at = (p) => resolve(process.cwd(), p)
const PATH = '/badCase/list'
const KEY = 'badCase'

const badCaseRoute = () => import('@/router/modules/badCase.js')

describe('Bad Case 队列的路由登记', () => {
  it('模块导出 /badCase/list，且组件是懒加载函数', async () => {
    const mod = await badCaseRoute()
    expect(Array.isArray(mod.default)).toBe(true)
    const [route] = mod.default
    expect(route.path).toBe(PATH)
    expect(route.name).toBe('BadCaseList')
    expect(typeof route.component).toBe('function')
    expect(route.meta.group).toBe('workspace')
  })

  it('真路由表里能解析到它（文件存在即注册）', () => {
    const resolved = router.resolve(PATH)
    expect(resolved.name).toBe('BadCaseList')
    expect(resolved.matched.length).toBeGreaterThan(0)
    expect(resolved.meta.menuSection).toBe('workspace')
    expect(resolved.meta.menuKey).toBe(KEY)
  })

  it('菜单构建器按 meta 把它放进了工作台分区', () => {
    const leaf = collectMenuLeaves(router.getRoutes()).find((l) => l.path === PATH)
    expect(leaf, `${PATH} 未进入菜单候选`).toBeTruthy()
    expect(leaf.section).toBe('workspace')
    expect(leaf.key).toBe(KEY)
  })

  it('九份语言包里都有 menu.badCase 译文（缺一个就整站显示成字面量 menu.badCase）', () => {
    // 侧栏标题现在由 layout/menu.js 解析：先 t('menu.<key>')，键缺失才回退 meta.title。
    // 少了译文不会再印裸 key（有兜底），但菜单会退回中文，多语言等于白做 —— 仍然必须齐备。
    const locales = ['zh', 'en', 'ja', 'ar', 'es', 'fr', 'de', 'ru', 'pt']
    for (const lang of locales) {
      const menu = JSON.parse(readFileSync(at(`src/i18n/locales/${lang}.json`), 'utf8')).menu
      expect(menu[KEY], `${lang}.json 缺 menu.${KEY}`).toBeTruthy()
      expect(menu[KEY]).not.toMatch(/^\[[A-Z]{2}\]/)
    }
  })
})
