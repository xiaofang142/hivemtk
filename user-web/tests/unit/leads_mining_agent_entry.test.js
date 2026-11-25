import { expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import ElementPlus from 'element-plus'
import Rules from '@/views/leads/Rules.vue'
import { settle } from './settle-helper'

// 「线索发现智能体」是 leadGovernance 菜单下的深链子页：后端菜单注册表和
// menuNavigation 只把它当面包屑归属，侧栏不会单独渲染入口。规则页页头的按钮
// 是用户唯一可达路径；缺了它就只能手输 URL，正是线上「看不到功能」的成因。

vi.mock('@/api/leads.js', () => ({
  listRules: vi.fn().mockResolvedValue({ list: [], total: 0 }),
  createRule: vi.fn(),
  updateRule: vi.fn(),
  deleteRule: vi.fn()
}))

it('线索规则页提供进入线索发现智能体的入口', async () => {
  const router = createRouter({
    history: createMemoryHistory(),
    routes: [
      { path: '/leads/rules', component: { render: () => null } },
      { path: '/leads/mining-agents', component: { render: () => null } }
    ]
  })
  await router.push('/leads/rules')
  const wrapper = mount(Rules, { global: { plugins: [router, ElementPlus] } })
  try {
    await settle(wrapper)
    const entry = wrapper.findAll('button').find(button => button.text() === '线索发现智能体')
    expect(Boolean(entry)).toBe(true)
    await entry.trigger('click')
    await settle(wrapper)
    expect(router.currentRoute.value.path).toBe('/leads/mining-agents')
  } finally {
    wrapper.unmount()
  }
})
