import { describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import ElementPlus from 'element-plus'
import i18n from '@/i18n'
import Layout from '@/layout/Layout.vue'
import agentRoutes from '@/router/modules/aiAgent'
import { usePermissionStore } from '@/stores/permission'
import { useUserStore } from '@/stores/user'
import { settle } from './settle-helper'

vi.mock('@/utils/request', () => ({
  updateRequestConfig: vi.fn(),
  http: {},
  default: {}
}))
vi.mock('@/composables/useOnboarding', () => ({
  useOnboarding: () => ({ destroy: vi.fn(), destroyModuleTour: vi.fn() }),
  resolveRoutePattern: () => ''
}))

describe('Layout 菜单展开', () => {
  it('切换系统与智能体分组时，打开新分组且不访问尚未注册的子菜单', async () => {
    const pinia = createPinia()
    useUserStore(pinia).setToken('fixture')
    const permission = usePermissionStore(pinia)
    permission.menusLoaded = true
    permission.menus = [
      { key: 'aiAgent', children: [{ key: 'agentManage', children: [{ key: 'aiAgentList', path: '/aiAgent/list' }] }] },
      { key: 'system', children: [{ key: 'assetBundle', children: [
        { key: 'assetBundleList', path: '/asset-bundle/list' },
        { key: 'assetBundlePlayground', path: '/asset-bundle/playground' }
      ] }] }
    ]
    const paths = ['/aiAgent/list', '/asset-bundle/list', '/asset-bundle/merchant-new', '/aiAgent/edit/:id']
    const emptyPage = { render: () => null }
    const router = createRouter({
      history: createMemoryHistory(),
      routes: paths.map(path => {
        const record = agentRoutes.find(item => item.path === path)
        return record?.components
          ? { ...record, components: { default: emptyPage, list: emptyPage } }
          : { ...record, path, component: emptyPage }
      })
    })
    await router.push(paths[0])
    const errorHandler = vi.fn()
    const wrapper = mount(Layout, {
      attachTo: document.body,
      global: {
        plugins: [pinia, router, ElementPlus, i18n],
        stubs: { MessageNotification: true, SubscriptionBanner: true, LanguageSwitcher: true },
        config: { errorHandler }
      }
    })

    try {
      await settle(wrapper)
      expect(wrapper.find('.app-aside .el-sub-menu').exists()).toBe(false)
      expect(errorHandler).not.toHaveBeenCalled()
      await router.push(paths[1])
      await settle(wrapper)
      expect(errorHandler).not.toHaveBeenCalled()
      expect(wrapper.get('.app-aside .el-sub-menu').classes()).toContain('is-opened')
      const active = wrapper.get('.app-aside .el-menu-item.is-active')
      await router.push(paths[2])
      await settle(wrapper)
      expect(wrapper.get('.app-aside .el-menu-item.is-active').element).toBe(active.element)
      expect(errorHandler).not.toHaveBeenCalled()
      await router.push('/aiAgent/edit/42')
      await settle(wrapper)
      expect(wrapper.find('.app-aside .el-sub-menu').exists()).toBe(false)
      expect(errorHandler).not.toHaveBeenCalled()
      expect(wrapper.get('.breadcrumb-wrap [aria-current="page"]').text()).toBe(i18n.global.t('navigation.editAgent'))
      const backLink = wrapper.get('.breadcrumb-wrap a[href="/aiAgent/list"]')
      await backLink.trigger('click')
      await settle(wrapper)
      expect(router.currentRoute.value.path).toBe('/aiAgent/list')
      expect(wrapper.find('.geo-alert-bell').exists()).toBe(false)
    } finally {
      wrapper.unmount()
    }
  })
})
