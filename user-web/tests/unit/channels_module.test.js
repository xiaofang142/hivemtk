import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter } from 'vue-router'
import ElementPlus from 'element-plus'
import i18n from '@/i18n'
import Channels from '@/views/channels/index.vue'
import { usePermissionStore } from '@/stores/permission'
import { settle } from './settle-helper'

vi.mock('@/views/channels/channel-manage.vue', () => ({ default: { template: '<div data-testid="catalog-content">渠道列表</div>' } }))
vi.mock('@/views/channels/account-manage.vue', () => ({ default: { template: '<div data-testid="accounts-content">示例公司客服账号</div>' } }))
vi.mock('@/views/channels/identify-code-manage.vue', () => ({ default: { template: '<div data-testid="codes-content">识别码列表</div>' } }))

let wrapper
afterEach(() => wrapper?.unmount())

async function openModule(paths, path) {
  i18n.global.locale.value = 'zh'
  const pinia = createPinia()
  const permission = usePermissionStore(pinia)
  permission.menusLoaded = true
  permission.menus = [{ key: 'channels', children: paths.map(path => ({ key: path, path })) }]
  const router = createRouter({ history: createMemoryHistory(), routes: paths.map(path => ({ path, component: Channels })) })
  await router.push(path)
  wrapper = mount(Channels, { global: { plugins: [pinia, router, ElementPlus, i18n] } })
  await settle(wrapper)
  return router
}

describe('独立渠道模块', () => {
  it('深链显示对应内容，页内切换保持原路由', async () => {
    const router = await openModule(['/channels/manage', '/channels/accounts', '/channels/identify-codes'], '/channels/accounts')
    expect(wrapper.get('h1').text()).toBe('渠道')
    expect(wrapper.findAll('[role="tab"]')).toHaveLength(3)
    expect(wrapper.get('[data-testid="accounts-content"]').text()).toContain('示例公司')
    expect(wrapper.text()).toContain('SageX')
    await wrapper.get('#tab-\\/channels\\/manage').trigger('click')
    await settle(wrapper)
    expect(router.currentRoute.value.path).toBe('/channels/manage')
    expect(wrapper.find('[data-testid="catalog-content"]').exists()).toBe(true)
  })

  it('只展示已授权的页内入口', async () => {
    await openModule(['/channels/accounts'], '/channels/accounts')
    expect(wrapper.findAll('[role="tab"]').map(tab => tab.text())).toEqual(['账号'])
    expect(wrapper.find('[data-testid="accounts-content"]').exists()).toBe(true)
    expect(wrapper.find('[data-testid="codes-content"]').exists()).toBe(false)
  })

  it('T08：渠道模块为左侧导航 + 右侧内容的左右分栏', async () => {
    await openModule(['/channels/manage', '/channels/accounts', '/channels/identify-codes'], '/channels/accounts')
    expect(wrapper.get('[data-testid="channels-tabs"]').classes()).toContain('el-tabs--left')
    // 内容组件必须落在 el-tabs 的默认插槽里，左栏导航才与右栏表格并列。
    expect(wrapper.get('[data-testid="channels-tabs"]').find('[data-testid="accounts-content"]').exists()).toBe(true)
  })
})
