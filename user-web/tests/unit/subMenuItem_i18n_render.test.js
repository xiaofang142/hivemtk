import { describe, it, expect, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import { createRouter, createMemoryHistory } from 'vue-router'
import { defineComponent, h } from 'vue'
import ElementPlus, { ElMenu } from 'element-plus'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'

// W-9：i18n 的「渲染腿」——补上现有门抓不到的那一半。
//
// scripts/check-i18n-hardcode.cjs 只数「源码里还有多少行硬编码中文」，
// 它证明不了「$t() / t() 真的把译文渲染到了 DOM」：组件把 t 丢掉、
// key 拼错、词条在 en 里缺一块、翻完还是中文（fallbackLocale:'zh' 静默兜底）……
// 以上任何一种，硬编码扫描都是全绿。
//
// 这里拿侧栏菜单组件 SubMenuItem.vue 当第一个挂载点，因为它是**纯展示件**：
//   - 文案只有一处来源：模板里的 {{ t('menu.' + menu.key) }}；
//   - 不碰接口、不碰 store（只 useRouter 做点击跳转，用例不触发点击）；
//   - 菜单 key 是命名空间式（menu.workspace），原始 key 与任何译文都不相等，
//     所以「原始 key 不许出现在 DOM 里」这条反向对照才有意义（中文 key 的词条
//     做不到：key 本身就是译文，正反两向分不开）。
//
// 判据三件套（缺一都不算「真的渲染」）：
//   1. DOM 文本 === **磁盘上** locales/*.json 里该语种的值（真 i18n 实例 +
//      项目自己的消息包，不是 stub；直读 JSON 是为了绕开运行时 transform，
//      避免「拿 t() 的结果去验 t() 的渲染」这种自证）；
//   2. DOM 文本 === 写死的显示口径字面量（防「译文被改坏了但恰好等于 key」
//      这类自证式通过 —— 改译文会在这里红，这是刻意的棘轮）；
//   3. 命中数 = 1 + 原始 key 不出现（i18n 没接上时，DOM 里就是 menu.workspace）。
//      反向对照刻意排在**第一条**跑：模板丢了 t() 时它先炸，报错原文就是
//      「原始 key 漏进 DOM」，比「文案对不上」更快指到病根。
//
// 组件里的 t 取自模块级单例 i18n.global.t（不是 useI18n()），所以用例
// 必须切**单例**的 locale —— 与既有渲染用例（chat_embed_avatar_*）同法。
//
// 挂载细节：ElMenuItem / ElSubMenu 的 setup 会 inject 根菜单，独立挂
// SubMenuItem 会抛 ElementPlusError: can not inject root menu，所以统一套一层
// ElMenu（用 render 函数，仓内单测不引运行时模板编译器）。

import i18n from '@/i18n'
import SubMenuItem from '@/components/SubMenuItem.vue'

const HERE = path.dirname(fileURLToPath(import.meta.url))
const LOCALES_DIR = path.resolve(HERE, '../../src/i18n/locales')

// 磁盘口径：不经过 vite 的 hivemtk-i18n-functions 预编译，拿到的是原始字符串。
const readBundle = (locale) =>
  JSON.parse(fs.readFileSync(path.join(LOCALES_DIR, `${locale}.json`), 'utf8'))

const ZH = readBundle('zh')
const EN = readBundle('en')
const BUNDLES = { zh: ZH, en: EN }

const savedLocale = i18n.global.locale.value

// 刻意写死：这两个值就是「显示口径」，改译文必须同时改这里。
const CASES = [
  { locale: 'zh', key: 'menu.workspace', literal: '工作台' },
  { locale: 'en', key: 'menu.workspace', literal: 'Workspace' },
]

const occurrences = (haystack, needle) => haystack.split(needle).length - 1

const Host = defineComponent({
  props: { menu: { type: Object, required: true } },
  render() {
    return h(ElMenu, null, { default: () => h(SubMenuItem, { menu: this.menu }) })
  },
})

function mountItem(locale, menu) {
  i18n.global.locale.value = locale
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/', component: { render: () => h('div') } },
      { path: '/customer', component: { render: () => h('div') } },
    ],
  })
  return mount(Host, {
    props: { menu },
    global: { plugins: [i18n, ElementPlus, router] },
  })
}

describe('SubMenuItem.vue · 菜单文案真的渲染进 DOM（W-9 渲染腿）', () => {
  afterEach(() => {
    i18n.global.locale.value = savedLocale
  })

  it('前置：menu.workspace 在 zh.json / en.json 里都存在', () => {
    expect(typeof ZH.menu?.workspace).toBe('string')
    expect(typeof EN.menu?.workspace).toBe('string')
  })

  CASES.forEach(({ locale, key, literal }) => {
    it(`${locale}：叶子菜单渲染「${literal}」且恰好 1 次，原始 key 不出现`, () => {
      // 语言包侧锚：写死的口径必须与磁盘上的译文一致，否则先红在这里。
      expect(BUNDLES[locale].menu.workspace, `${locale}.json 的显示口径被改了`).toBe(literal)

      const w = mountItem(locale, { key: 'workspace', path: '/' })
      const itemText = w.get('.el-menu-item').text()

      // 1) 反向对照先跑：原始 key 漏进 DOM = 模板没走 t()（i18n 没接上）
      expect(w.text(), '原始 key 漏进 DOM，说明模板没走 t()').not.toContain(key)
      expect(w.html()).not.toContain(key)
      // 2) DOM 文本 === 该语种语言包（真 i18n 实例，真 locales/*.json）
      expect(itemText).toBe(BUNDLES[locale].menu.workspace)
      // 3) 显示口径字面锚
      expect(itemText).toBe(literal)
      // 4) 命中数 = 1
      expect(occurrences(w.text(), literal)).toBe(1)

      w.unmount()
    })
  })

  it('en 下不许残留中文（locale 没真正生效的控制项）', () => {
    const w = mountItem('en', { key: 'workspace', path: '/' })
    expect(w.text()).not.toContain('工作台')
    expect(i18n.global.t('menu.workspace')).toBe('Workspace')
    w.unmount()
  })

  it('分组菜单：父标题与子项各命中 1 次，两个原始 key 都不出现', () => {
    const w = mountItem('zh', {
      key: 'customer',
      children: [
        { key: 'customer360', path: '/customer' },
        { key: 'crm', path: '/crm/list' },
      ],
    })
    const text = w.text()

    expect(text).toContain('客户与销售')
    expect(text).toContain('客户 360')
    expect(occurrences(text, '客户与销售')).toBe(1)
    expect(occurrences(text, '客户 360')).toBe(1)
    expect(text).not.toContain('menu.customer')
    expect(w.html()).not.toContain('menu.customer')

    w.unmount()
  })
})
