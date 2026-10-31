import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createPinia } from 'pinia'
import { settle } from './settle-helper'

// 模板/技能市场（F2）渲染用例：证明「分类/类型/可用性」这条元数据契约**穿过了模板**，
// 而不只是活在 api/templateMarket.js 的常量里。断言全部打在渲染出来的 DOM 上。

const api = vi.hoisted(() => ({
  list: vi.fn(),
  categories: vi.fn()
}))

vi.mock('@/api/templateMarket', () => ({
  listMarketEntries: api.list,
  marketCategories: api.categories,
  marketEntryDetail: vi.fn(),
  // 页面卡片价格列消费这个助手（S-22 收费上架后新增）；mock 不补的话
  // 渲染期 undefined 调用直接把整棵组件树炸空。
  marketPriceText: (cents) => (cents > 0 ? `¥${(cents / 100).toFixed(2)}` : '免费'),
  ENTRY_KIND_LABEL: { template: '模板', skill: '技能' },
  AVAILABILITY_LABEL: { builtin: '内置可用', needs_module: '需开通模块', coming_soon: '即将上线' },
  AVAILABILITY_TAG_TYPE: { builtin: 'success', needs_module: 'warning', coming_soon: 'info' }
}))

// 页面 onMounted 读 route.query（?publish=1 直开发布表单），无 router 装配时
// useRoute() 返回 undefined → TypeError 把 fetch 掐死 → 橱窗永远空。
vi.mock('vue-router', () => ({
  useRoute: () => ({ query: {} }),
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() })
}))

import Market from '@/views/templateMarket/Market.vue'
import i18n from '@/i18n'

// jsdom 的 navigator.language 是 en-US，页面经全局 i18n 直译后断言里的中文
// 文案全部落空——组件用例统一钉在 zh（与 chat_embed 系列同一做法）。
i18n.global.locale.value = 'zh'

const ENTRIES = [
  { id: 'card-douyin', kind: 'template', name: '抖音卡片页', category: 'channel_card', summary: '抖音渠道卡片', source: 'internal/reach/card/template/douyin_card.html', availability: 'builtin', tags: ['douyin', '卡片'] },
  { id: 'live-code', kind: 'template', name: '活码落地页', category: 'live_code', summary: '活码展示页', source: 'internal/reach/card/template/live_code.html', availability: 'builtin', tags: ['活码', '二维码'] },
  { id: 'skill-mcp-tool', kind: 'skill', name: 'MCP 工具接入', category: 'mcp', summary: 'MCP 协议接入', source: 'internal/middleware/mcp_guard.go', availability: 'builtin', tags: ['mcp', '工具'] },
  { id: 'skill-agentic-ltc', kind: 'skill', name: 'Agentic LTC', category: 'agentic', summary: 'lead-to-cash', source: 'internal/middleware/ltc_gate.go', availability: 'coming_soon', tags: ['ltc'] }
]

async function mountPage() {
  api.list.mockResolvedValue({ list: ENTRIES, total: ENTRIES.length })
  api.categories.mockResolvedValue(['agentic', 'channel_card', 'live_code', 'mcp'])
  const wrapper = mount(Market, {
    attachTo: document.body,
    global: { plugins: [createPinia(), ElementPlus] }
  })
  await settle(wrapper)
  return wrapper
}

function keywordInput() {
  return document.querySelector('input[placeholder="名称 / 简介 / 标签"]')
}

beforeEach(() => {
  vi.clearAllMocks()
})

afterEach(() => {
  document.body.innerHTML = ''
})

describe('模板/技能市场：卡片网格 + 分类过滤', () => {
  it('全部条目都渲染成卡片，类型/分类/可用性译成人话', async () => {
    const wrapper = await mountPage()
    const text = wrapper.text()
    for (const e of ENTRIES) expect(text).toContain(e.name)
    expect(text).toContain('模板')
    expect(text).toContain('技能')
    expect(text).toContain('渠道卡片')
    expect(text).toContain('MCP 工具')
    expect(text).toContain('内置可用')
    // coming_soon 条目必须如实标「即将上线」，而不是被当成内置可用。
    expect(text).toContain('即将上线')
    wrapper.unmount()
  })

  it('关键词过滤：只留命中名称/简介/标签的卡片', async () => {
    const wrapper = await mountPage()
    const input = keywordInput()
    expect(input, '必须有关键词输入框').toBeTruthy()
    input.value = 'mcp'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await settle(wrapper)
    expect(wrapper.text()).toContain('MCP 工具接入')
    expect(wrapper.text()).not.toContain('抖音卡片页')
    wrapper.unmount()
  })

  it('无命中时显示空态而不是白屏', async () => {
    const wrapper = await mountPage()
    const input = keywordInput()
    input.value = 'zzz-no-hit'
    input.dispatchEvent(new Event('input', { bubbles: true }))
    await settle(wrapper)
    expect(wrapper.text()).toContain('没有匹配的模板或技能')
    wrapper.unmount()
  })
})
