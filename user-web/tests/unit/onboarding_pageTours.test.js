import { describe, it, expect } from 'vitest'
import fs from 'node:fs'
import path from 'node:path'
import { createPageTours } from '@/config/onboarding/pageTours'
import { createI18n } from 'vue-i18n'
import { messages } from '@/i18n/catalog'

// 页面导览注册表的不变量（任务六）。
//
// 这套引导以前是「写了但没人用」：usePageOnboarding 全仓无人调用，
// 页面导览的配置永远不执行；即便执行，38 个选择器一个都不在 DOM 里，
// driver.js 会退化成 0×0 的 dummy element，弹窗落在屏幕正中的空气上。
// 用户看到的就是「毫无意义的引导」+「关闭按钮按了没反应」。
//
// 这里把当时踩到的每个坑都钉成断言：
//   1. 每条导览至少两步、每步都有 element/title/description；
//   2. i18n 键必须是**点分键**且每段只含字母数字下划线
//      —— i18n 门禁只认这个形状，出现连字符或中文会判「解析不到词条」把 CI 打红；
//   3. 9 个语言包里这些键必须**都存在**（coverage 门要求 100%）；
//   4. 选择器不能是「条件渲染」的容器 —— 带 v-if 的元素在首屏不存在，
//      锚在它上面就等于把气泡放到了空气里；
//   5. 路由模式必须能在 router 里找到，且注册表的 route 与键一致。

const REPO = path.resolve(__dirname, '../../..')
const LOCALES_DIR = path.join(REPO, 'user-web/src/i18n/locales')
const LANGS = ['zh', 'en', 'ja', 'ar', 'es', 'fr', 'de', 'ru', 'pt']
const DOTTED_KEY = /^[A-Za-z0-9_]+(\.[A-Za-z0-9_]+)+$/

const locales = Object.fromEntries(
  LANGS.map((lang) => [lang, JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, `${lang}.json`), 'utf8'))])
)

/** 把点分键拆成路径数组 */
function keyPath(key) {
  return key.split('.')
}

/** 按点分键取值 */
function pick(obj, key) {
  return keyPath(key).reduce((acc, part) => (acc == null ? undefined : acc[part]), obj)
}

// 从组件源码里找出「只在某个条件下才出现」的 class —— 这些元素不能当锚点。
//
// 判据：一个 class 如果**只在 v-if 分支里出现**（全文件没有第二处同样的 class），
// 那它首屏可能压根不存在，锚在它上面就等于把气泡放到了空气里
//（driver.js 会退化成 0×0 的 dummy element，弹窗落在屏幕正中、没有高亮）。
//
// 反过来，如果同一个 class 在 v-else-if / v-else 分支里也有一份
//（如 browserAutomation/Detail.vue 的 .page：加载成功显示详情、失败显示报错），
// 那它总会占位，不算条件渲染。
function conditionalClasses(relPath) {
  const file = path.join(REPO, 'user-web/src', relPath)
  if (!fs.existsSync(file)) return new Set()
  const src = fs.readFileSync(file, 'utf8')
  // 先统计每个 class 在全文件出现几次
  const all = new Map()
  const re = /<([a-zA-Z][\w-]*)\b([^>]*?)\/?>/g
  let m
  const occurrences = []
  while ((m = re.exec(src)) !== null) {
    const attrs = m[2] || ''
    const cm = attrs.match(/class="([^"]*)"/)
    if (!cm) continue
    const classes = cm[1].split(/\s+/).filter(Boolean)
    classes.forEach((c) => all.set(c, (all.get(c) || 0) + 1))
    occurrences.push({ attrs, classes })
  }
  const out = new Set()
  occurrences.forEach(({ attrs, classes }) => {
    if (!/\bv-if\b/.test(attrs)) return
    classes.forEach((c) => {
      // 全文件只出现这一次的 = 条件渲染（没有 else 分支兜底）
      if ((all.get(c) || 0) <= 1) out.add(c)
    })
  })
  return out
}

describe('页面导览注册表 · 不变量', () => {
  const tours = createPageTours((k) => k)

  /**
   * 收集注册表里用到的全部 i18n 键。
   *
   * 从**源码**里抓 t('点分键') 的实参，而不是从运行时的对象上取：
   * 注册表存的是解析后的文案（title/description），键只在源码里以字面量出现。
   * 从源码抓还有一个好处：能同时证明「键确实被 t() 消费着」——
   * i18n 门禁的「词条库无人引用」判定也是按这个口径算的。
   */
  function collectKeys() {
    const src = fs.readFileSync(path.join(REPO, 'user-web/src/config/onboarding/pageTours.ts'), 'utf8')
    const keys = new Set()
    for (const m of src.matchAll(/t\(\s*'([A-Za-z0-9_.]+)'\s*\)/g)) keys.add(m[1])
    return keys
  }

  it('至少注册了一页导览（空表 = 整套引导是死的）', () => {
    expect(Object.keys(tours).length).toBeGreaterThan(0)
  })

  it('每条导览的键与 route 字段一致', () => {
    Object.entries(tours).forEach(([key, tour]) => {
      expect(tour.route).toBe(key)
    })
  })

  it('每条导览至少两步，且每步字段齐全', () => {
    Object.entries(tours).forEach(([route, tour]) => {
      expect(Array.isArray(tour.steps), `${route} 缺 steps`).toBe(true)
      expect(tour.steps.length, `${route} 步骤太少`).toBeGreaterThanOrEqual(2)
      tour.steps.forEach((step, i) => {
        expect(typeof step.element, `${route} step${i + 1} 缺 element`).toBe('string')
        expect(step.element.length, `${route} step${i + 1} element 为空`).toBeGreaterThan(0)
        expect(typeof step.title, `${route} step${i + 1} 缺 title`).toBe('string')
        expect(typeof step.description, `${route} step${i + 1} 缺 description`).toBe('string')
        expect(step.description.length, `${route} step${i + 1} description 为空`).toBeGreaterThan(0)
      })
    })
  })

  it('i18n 键都是合法点分键（i18n 门禁只认这个形状）', () => {
    const keys = collectKeys()
    keys.forEach((key) => {
      expect(DOTTED_KEY.test(key), `非法 i18n 键：${key}`).toBe(true)
      expect(
        key.startsWith('setup.onboarding.page.') || key.startsWith('navigationGuide.pages.'),
        `键必须在已注册的页面导览命名空间下：${key}`
      ).toBe(true)
    })
  })

  it('9 个语言包都收录了全部页面导览词条（coverage 门要求 100%）', () => {
    const keys = [...collectKeys()].filter(key => key.startsWith('setup.onboarding.page.'))
    LANGS.forEach((lang) => {
      keys.forEach((key) => {
        const value = pick(locales[lang], key)
        expect(value, `${lang}.json 缺 ${key}`).toBeDefined()
        expect(typeof value === 'string' && value.length > 0, `${lang}.json 的 ${key} 是空值`).toBe(true)
      })
    })
  })

  it('新增页面词条从实际 catalog 解析，双语齐全且九语言按正式配置回落', () => {
    const keys = [...collectKeys()].filter(key => key.startsWith('navigationGuide.pages.'))
    expect(keys.length).toBeGreaterThan(0)
    for (const lang of ['zh', 'en']) {
      for (const key of keys) {
        const value = pick(messages[lang], key)
        expect(value, `${lang} catalog 缺 ${key}`).toBeDefined()
        expect(typeof value === 'function' || (typeof value === 'string' && value.length > 0)).toBe(true)
      }
    }
    for (const lang of LANGS) {
      const i18n = createI18n({ legacy: false, locale: lang, fallbackLocale: 'zh', messages, missingWarn: false, fallbackWarn: false })
      for (const key of keys) {
        const value = i18n.global.t(key)
        expect(value, `${lang} 无法解析 ${key}`).not.toBe(key)
        expect(value.length).toBeGreaterThan(0)
        expect(value).not.toMatch(/\{\{|\}\}|@[^\s]/)
      }
    }
  })

  it('选择器不锚在条件渲染的元素上（否则气泡落到空气里）', () => {
    // 注册表里的选择器 -> 页面组件文件
    const ROUTE_TO_COMPONENT = {
      '/crm/list': 'views/crm/List.vue',
      '/leads/rules': 'views/leads/Rules.vue',
      '/geo-tools/brand-config': 'views/geo/BrandConfig.vue',
      '/messageHub/list': 'views/messageHub/List.vue',
      '/abExperiment/list': 'views/abExperiment/List.vue',
      '/aiAgent/list': 'views/aiAgent/List.vue',
      '/aiAgent/create': 'views/aiAgent/Edit.vue',
      '/aiAgent/edit/:id': 'views/aiAgent/Edit.vue',
      '/aiProductivity/list': 'views/aiProductivity/List.vue',
      '/analytics/cohort-path': 'views/analytics/CohortPath.vue',
      '/approvalTask/list': 'views/approvalTask/List.vue',
      '/asset-bundle/list': 'views/assetBundle/List.vue',
      '/asset-bundle/playground': 'views/assetBundle/Playground.vue',
      '/asset-bundle/merchant-new': 'views/assetBundle/MerchantEditor.vue',
      '/asset-market': 'views/assetMarket/Market.vue',
      '/asset-market/detail/:id': 'views/assetMarket/Detail.vue',
      '/asset-market/my-assets': 'views/assetMarket/MyAssets.vue',
      '/asset-market/sync-log': 'views/assetMarket/SyncLog.vue',
      '/backup/list': 'views/backup/List.vue',
      '/badCase/list': 'views/badCase/List.vue',
      '/batchOperation/list': 'views/batchOperation/List.vue',
      '/browser-automation/tasks': 'views/browserAutomation/List.vue',
      '/browser-automation/tasks/create': 'views/browserAutomation/Editor.vue',
      '/browser-automation/tasks/:id/edit': 'views/browserAutomation/Editor.vue',
      '/browser-automation/tasks/:id': 'views/browserAutomation/Detail.vue',
      '/browser-automation/sessions/:id': 'views/browserAutomation/Monitor.vue',
      '/browser-automation/cron': 'views/browserAutomation/Cron.vue',
      '/churnPrediction/list': 'views/churnPrediction/List.vue',
      '/clue/list': 'views/clue/List.vue',
      '/community/list': 'views/community/List.vue',
      '/conversionFunnel/list': 'views/conversionFunnel/List.vue',
      '/customer360/list': 'views/customer360/List.vue'
    }
    Object.entries(tours).forEach(([route, tour]) => {
      const rel = ROUTE_TO_COMPONENT[route]
      if (!rel) return
      const conditional = conditionalClasses(rel)
      tour.steps.forEach((step, i) => {
        // 取选择器里用到的 class 名（只查最后一段 class，够用了）
        const classes = (step.element.match(/\.[A-Za-z][\w-]*/g) || []).map((c) => c.slice(1))
        classes.forEach((cls) => {
          expect(
            conditional.has(cls),
            `${route} step${i + 1} 的选择器 "${step.element}" 命中了条件渲染的 class "${cls}"（在 ${rel} 里带 v-if/v-show）——首屏不存在，锚点会落到空气里`
          ).toBe(false)
        })
      })
    })
  })

  it('文案里不能出现 Vue 模板占位符（会直接把整页编译崩掉）', () => {
    // vue-i18n 的消息编译器把 {{ }} 当成自己的占位语法，
    // 「Not allowed nest placeholder」这个编译错误会让**整页白屏**——
    // 实测「变量写成 {{name}} 这样的形式」这一句就把 user-web 顶掉了。
    // 引导文案里要写占位符，只能用文字描述，不能写字面的双花括号。
    LANGS.forEach((lang) => {
      const page = pick(locales[lang], 'setup.onboarding.page') || {}
      Object.entries(page).forEach(([pageKey, entry]) => {
        if (pageKey === 'common' || !entry || typeof entry !== 'object') return
        const texts = []
        const collect = (node) => {
          if (typeof node === 'string') texts.push(node)
          else if (node && typeof node === 'object') Object.values(node).forEach(collect)
        }
        collect(entry)
        texts.forEach((text) => {
          expect(text, `${lang} 的 ${pageKey} 文案里出现了 {{ }}，会触发 vue-i18n 编译错误`).not.toMatch(/\{\{|\}\}/)
          // 同理，vue-i18n 把 @ 开头的词当「链接消息」引用（@:key / @.modifier:key），
          // 「用户 @机器人 就能触发」这一句就报过 "Invalid linked format" 把页面顶掉。
          expect(text, `${lang} 的 ${pageKey} 文案里出现了 @ 引用语法，会触发 vue-i18n 编译错误`).not.toMatch(/@[^\s]/)
        })
      })
    })
  })

  it('路由模式在 router 里真实存在（写错就是永远走不到的一页）', () => {    const routerSrc = [
      fs.readFileSync(path.join(REPO, 'user-web/src/router/index.js'), 'utf8'),
      ...fs
        .readdirSync(path.join(REPO, 'user-web/src/router/modules'))
        .filter((f) => f.endsWith('.js'))
        .map((f) => fs.readFileSync(path.join(REPO, 'user-web/src/router/modules', f), 'utf8'))
    ].join('\n')
    // 收集 router 里出现过的 path 字面量（含相对子路径，父路径单独拼）
    const declared = new Set()
    for (const m of routerSrc.matchAll(/path:\s*'([^']+)'/g)) declared.add(m[1])

    Object.keys(tours).forEach((route) => {
      // 完整路径要么直接声明，要么是「父路径 + 相对子路径」拼出来的。
      // 子路径本身可能带斜杠和动态段（'tasks/:id/edit'、'sessions/:id'），
      // 所以要把 route 切成「父 + 子」的所有可能切法逐个试。
      const parts = route.split('/').filter(Boolean)
      let ok = declared.has(route)
      for (let i = 1; i < parts.length && !ok; i++) {
        const parent = '/' + parts.slice(0, i).join('/')
        const child = parts.slice(i).join('/')
        if (declared.has(parent) && declared.has(child)) ok = true
      }
      expect(ok, `路由 ${route} 在 router 里找不到对应声明`).toBe(true)
    })
  })
})
