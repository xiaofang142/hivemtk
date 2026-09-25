import { describe, it, expect } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

// Bad Case 队列的路由登记守护用例（镜像 tests/unit/router_approvalTask.test.js 的判据）。
//
// 为什么要测这个：本项目的路由是**懒登记**的 —— src/router/modules/*.js 里的文件写好了
// 不等于挂上了，还得在 index.js 的 moduleNames 里出现一次；漏掉那一句的表现不是白屏报错，
// 而是"点菜单进 NotFound"，前端 review 时几乎看不出来（文件明明在）。

const at = (p) => resolve(process.cwd(), p)
const INDEX_SRC = readFileSync(at('src/router/index.js'), 'utf8')

// 解析不到就抛：这是一条闸门用例，找不到自己该读的东西时必须红，
// 而不是"数组为空 ⇒ 后面每条断言都真空地通过"。
const listOf = (src, decl) => {
  const block = src.match(new RegExp(`const ${decl} = \\[([\\s\\S]*?)\\n\\];`))
  if (!block) throw new Error(`index.js 里找不到 const ${decl}`)
  return [...block[1].matchAll(/['"]([A-Za-z0-9_]+)['"]/g)].map((m) => m[1])
}

const moduleNames = listOf(INDEX_SRC, 'moduleNames')
const badCaseRoute = () => import('@/router/modules/badCase.js')

describe('Bad Case 队列的路由登记', () => {
  it('模块导出 /badCase/list，且组件是懒加载函数', async () => {
    const mod = await badCaseRoute()
    expect(Array.isArray(mod.default)).toBe(true)
    const [route] = mod.default
    expect(route.path).toBe('/badCase/list')
    expect(route.name).toBe('BadCaseList')
    expect(typeof route.component).toBe('function')
    expect(route.meta.group).toBe('workspace')
  })

  it('已在 moduleNames 里登记（漏了就是点菜单进 NotFound）', () => {
    expect(moduleNames).toContain('badCase')
  })

  it('菜单里有这一项，且 path 与路由字面量一致（不一致时高亮与面包屑都会失效）', async () => {
    const layout = readFileSync(at('src/layout/Layout.vue'), 'utf8')
    const mod = await badCaseRoute()
    expect(layout).toContain(`path: '${mod.default[0].path}'`)
  })

  it('菜单项的 key 在九份语言包里都有译文（缺一个就整站显示成字面量 menu.badCase）', () => {
    // 侧栏那句标签走的是 t('menu.' + key)，而 i18n 初始化时关了 missingWarn ——
    // 少一个键**不会有任何告警**，只会把键名当文案印在菜单上。
    // 名单里只有本批真正入库的那个键：判据不许依赖任何没随本批提交的键，
    // 否则干净克隆里跑这份用例必红（menu.approvalTask 属于另一条泳道的待办面）。
    const locales = ['zh', 'en', 'ja', 'ar', 'es', 'fr', 'de', 'ru', 'pt']
    for (const lang of locales) {
      const menu = JSON.parse(readFileSync(at(`src/i18n/locales/${lang}.json`), 'utf8')).menu
      expect(menu.badCase, `${lang}.json 缺 menu.badCase`).toBeTruthy()
    }
  })
})
