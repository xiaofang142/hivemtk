import { describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import WorkflowNavigation from '@/components/WorkflowNavigation.vue'
import { useUserStore } from '@/stores/user'
import { usePermissionStore } from '@/stores/permission'
import i18n from '@/i18n'
import { settle } from './settle-helper'

describe('管理工作流入口', () => {
  it('管理员有所属模块时可进入子页，未加载、模块关闭或受限角色均隐藏入口', async () => {
    const pinia = createPinia()
    const user = useUserStore(pinia)
    user.login({ id: 'admin', role: 'admin' }, 'fixture')
    const permission = usePermissionStore(pinia)
    permission.menusLoaded = true
    permission.menus = [{ key: 'customer', children: [{ key: 'crm', path: '/crm/list' }] }]
    const router = createRouter({
      history: createMemoryHistory(),
      routes: ['/crm/list', '/crm/dedupe'].map(path => ({ path, component: { render: () => null } }))
    })
    await router.push('/crm/list')
    const wrapper = mount(WorkflowNavigation, {
      props: { owner: '/crm/list', links: [{ key: 'dedupe', path: '/crm/dedupe' }] },
      global: { plugins: [pinia, router, i18n] }
    })
    try {
      await settle(wrapper)
      expect(wrapper.get('nav').attributes('aria-label')).toBe(i18n.global.t('workflowNavigation.label'))
      await wrapper.get('a[href="/crm/dedupe"]').trigger('click')
      await settle(wrapper)
      expect(router.currentRoute.value.path).toBe('/crm/dedupe')

      permission.menusLoaded = false
      await settle(wrapper)
      expect(wrapper.find('nav').exists()).toBe(false)
      permission.menusLoaded = true
      permission.menus = []
      await settle(wrapper)
      expect(wrapper.find('nav').exists()).toBe(false)
      permission.menus = [{ key: 'crm', path: '/crm/list' }]
      user.setUserInfo({ role: 'staff' })
      await settle(wrapper)
      expect(wrapper.find('nav').exists()).toBe(false)
    } finally {
      wrapper.unmount()
    }
  })
})
