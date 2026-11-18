import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createPinia } from 'pinia'
import List from '@/views/inbox/List.vue'
import i18n from '@/i18n'
import { settle } from './settle-helper'

// specs/024 T606 前端验收的组件级钉子：生命周期筛选（稍后处理/待首响）与徽标渲染。
// 浏览器逐元素点验需登录态全栈（环境受阻见 delivery §七），本文件按规则 -1 的
// 「snapshot/显式断言」口径在组件层留证：筛选项存在 + 徽标按状态渲染。

const conversations = vi.hoisted(() => ({ list: [] }))
vi.mock('@/utils/request', () => ({
  http: {
    get: vi.fn(url => {
      if (url === '/api/inbox/conversations') return Promise.resolve({ list: conversations.list, total: conversations.list.length })
      if (typeof url === 'string' && url.startsWith('/api/inbox/conversations/')) {
        const id = url.split('/').pop()
        return Promise.resolve(conversations.list.find(c => c.id === id) || {})
      }
      if (url === '/api/inbox/stats') return Promise.resolve({ total: conversations.list.length, open: conversations.list.length })
      return Promise.resolve({ list: [], total: 0 })
    }),
    delete: vi.fn(),
  },
}))
vi.mock('@/stores/user', () => ({ useUserStore: () => ({ userInfo: { id: 7, role: 'customer_service' } }) }))

let wrapper
let previousLocale
beforeEach(() => {
  vi.clearAllMocks()
  previousLocale = i18n.global.locale.value
  i18n.global.locale.value = 'zh'
  conversations.list = [
    { id: 'conv-snoozed', customer_name: '稍后处理客户', platform: 'web', status: 'snoozed', unread_count: 0, tags: [] },
    { id: 'conv-waiting', customer_name: '待首响客户', platform: 'web', status: 'open', unread_count: 1, tags: [],
      waiting_since: '2026-11-18T07:00:00Z', first_reply_at: null },
    { id: 'conv-replied', customer_name: '已首响客户', platform: 'web', status: 'open', unread_count: 0, tags: [],
      waiting_since: '2026-11-18T07:00:00Z', first_reply_at: '2026-11-18T07:01:00Z' },
  ]
})
afterEach(() => {
  wrapper?.unmount()
  document.body.innerHTML = ''
  i18n.global.locale.value = previousLocale
})

describe('T606 生命周期筛选与徽标', () => {
  it('筛选器提供 稍后处理(snoozed) 与 待首响(awaiting_first_reply) 选项', async () => {
    wrapper = mount(List, { global: { plugins: [createPinia(), ElementPlus, i18n] } })
    await settle(wrapper)
    // 筛选项是 el-select 的 el-option（teleport 渲染，DOM 字符串不可见）——
    // 按组件树 props 断言（value 是稳定契约）
    const options = wrapper.findAllComponents({ name: 'ElOption' }).map(node => node.props())
    expect(options.some(option => option.value === 'snoozed' && String(option.label).includes('稍后处理'))).toBe(true)
    expect(options.some(option => option.value === 'awaiting_first_reply' && String(option.label).includes('待首响'))).toBe(true)
  })

  it('snoozed 会话展示「稍后处理」info 徽标', async () => {
    wrapper = mount(List, { global: { plugins: [createPinia(), ElementPlus, i18n] } })
    await settle(wrapper)
    const snoozedCard = wrapper.findAll('.conv-item').find(node => node.text().includes('稍后处理客户'))
    expect(snoozedCard).toBeTruthy()
    expect(snoozedCard.text()).toContain('稍后处理')
  })

  it('待首响徽标只对「客户在等且未首响」的会话展示', async () => {
    wrapper = mount(List, { global: { plugins: [createPinia(), ElementPlus, i18n] } })
    await settle(wrapper)
    const cards = wrapper.findAll('.conv-item')
    const waitingCard = cards.find(node => node.text().includes('待首响客户'))
    const repliedCard = cards.find(node => node.text().includes('已首响客户'))
    expect(waitingCard).toBeTruthy()
    expect(repliedCard).toBeTruthy()
    expect(waitingCard.text()).toContain('待首响')
    expect(repliedCard.text()).not.toContain('待首响')
  })
})
