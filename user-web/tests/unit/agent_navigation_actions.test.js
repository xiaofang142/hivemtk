import { expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import ElementPlus from 'element-plus'
import i18n from '@/i18n'
import List from '@/views/aiAgent/List.vue'
import { usePermissionStore } from '@/stores/permission'
import { useUserStore } from '@/stores/user'
import { settle } from './settle-helper'

vi.mock('@/api/aiAgent.js', () => ({
  listAgents: vi.fn().mockResolvedValue({ list: [{ id: 42, name: '测试智能体' }] }),
  deleteAgent: vi.fn(),
  toggleAgent: vi.fn(),
  testAgent: vi.fn()
}))

it.each([
  ['staff', false, false],
  ['admin', true, true],
  ['admin', false, false]
])('只显示实际可达的智能体表单入口（%s，授权=%s）', async (role, granted, expected) => {
  const pinia = createPinia()
  useUserStore(pinia).setUserInfo({ role })
  const permission = usePermissionStore(pinia)
  permission.menusLoaded = true
  permission.menus = role === 'staff' || granted ? [{ key: 'aiAgentList', path: '/aiAgent/list' }] : []
  if (granted) permission.menus.push({ key: 'aiAgentCreate', path: '/aiAgent/create' })
  const router = createRouter({
    history: createMemoryHistory(),
    routes: ['/aiAgent/list', '/aiAgent/edit/:id'].map(path => ({ path, component: { render: () => null } }))
  })
  await router.push('/aiAgent/list')
  const wrapper = mount(List, { global: { plugins: [pinia, router, ElementPlus, i18n] } })
  try {
    await settle(wrapper)
    const buttons = wrapper.findAll('button')
    expect(buttons.some(button => button.text() === i18n.global.t('新建智能体'))).toBe(expected)
    const edit = buttons.find(button => button.text() === '编辑')
    expect(Boolean(edit)).toBe(expected)
    if (edit) {
      await edit.trigger('click')
      await settle(wrapper)
      expect(router.currentRoute.value.path).toBe('/aiAgent/edit/42')
    }
  } finally {
    wrapper.unmount()
  }
})
