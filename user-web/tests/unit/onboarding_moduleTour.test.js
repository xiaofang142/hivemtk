import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, getActivePinia, setActivePinia } from 'pinia'
import { mount } from '@vue/test-utils'
import { createMemoryHistory, createRouter } from 'vue-router'
import ElementPlus from 'element-plus'
import Layout from '@/layout/Layout.vue'
import { settle } from './settle-helper'
import { useUserStore } from '@/stores/user'
import { usePermissionStore } from '@/stores/permission'
import { useOnboarding } from '@/composables/useOnboarding'
import i18n from '@/i18n'

const driverState = vi.hoisted(() => ({ instances: [] }))
vi.mock('@/api/menu', () => ({ getUserMenus: vi.fn() }))
vi.mock('@/utils/request', () => ({ updateRequestConfig: vi.fn(), http: {}, default: {} }))
vi.mock('vue-i18n', async importOriginal => ({
  ...await importOriginal(),
  useI18n: () => i18n.global
}))
vi.mock('driver.js', () => ({
  driver: vi.fn(config => {
    const instance = {
      config,
      drive: vi.fn(),
      destroy: vi.fn(() => config.onDestroyed(undefined, undefined, { reason: 'api' }))
    }
    driverState.instances.push(instance)
    return instance
  })
}))

const moduleKeys = ['workspace', 'customer', 'aiAgent', 'reach', 'knowledge', 'geoTools', 'analytics', 'system']
let onboarding
let user
let permission

function renderShell(active = 'geoTools') {
  document.body.innerHTML = `<div class="app-shell">
    <nav class="top-menu">${moduleKeys.map(key => `<button data-module="${key}" class="${key === active ? 'is-active' : ''}">${key}</button>`).join('')}</nav>
    <aside class="app-aside"></aside><main class="app-main"></main>
  </div>`
}

beforeEach(() => {
  setActivePinia(createPinia())
  localStorage.clear()
  driverState.instances.length = 0
  i18n.global.locale.value = 'zh'
  user = useUserStore()
  user.login({ id: 'tour-user', role: 'admin' }, 'fixture')
  permission = usePermissionStore()
  permission.menusLoaded = true
  permission.menus = moduleKeys.map(key => ({ key, children: [{ key: `${key}Page`, path: `/${key}/page` }] }))
  renderShell()
  onboarding = useOnboarding()
})

afterEach(() => {
  onboarding.destroy()
  document.body.innerHTML = ''
})

describe('模块导览', () => {
  it('只列出已下发且呈现的八组 tab，菜单未就绪和未登录均不返回模块', () => {
    expect(onboarding.getAvailableModules()).toEqual(moduleKeys)
    permission.menus = permission.menus.filter(menu => menu.key !== 'system')
    document.querySelector('[data-module="reach"]').hidden = true
    document.querySelector('[data-module="knowledge"]').style.display = 'none'
    document.querySelector('[data-module="customer"]').remove()
    expect(onboarding.getAvailableModules()).toEqual(['workspace', 'aiAgent', 'geoTools', 'analytics'])
    permission.menusLoaded = false
    expect(onboarding.getAvailableModules()).toEqual([])
    permission.menusLoaded = true
    permission.menusLoadError = true
    expect(onboarding.getAvailableModules()).toEqual([])
    permission.menusLoadError = false
    user.logout()
    expect(onboarding.getAvailableModules()).toEqual([])
  })

  it.each(moduleKeys)('%s 在当前模块启动真实三步，完成后退出且可强制重看', key => {
    renderShell(key)
    expect(onboarding.startModuleTour(key)).toBe(true)
    const instance = driverState.instances.at(-1)
    expect(instance.drive).toHaveBeenCalledOnce()
    expect(instance.config.steps).toHaveLength(3)
    expect(instance.config.steps[0].element).toContain(`data-module="${key}"`)
    expect(instance.config.steps.map(step => document.querySelector(step.element))).not.toContain(null)
    for (const step of instance.config.steps) {
      expect(step.popover.title).not.toMatch(/^navigationGuide\./)
      expect(step.popover.description).not.toMatch(/^navigationGuide\./)
      expect(step.popover.description.length).toBeGreaterThan(0)
    }
    expect(onboarding.getActiveTourType()).toBe('module')
    instance.config.steps[2].popover.onNextClick()
    expect(instance.destroy).toHaveBeenCalledOnce()
    expect(onboarding.isTourActive()).toBe(false)
    expect(onboarding.isCompleted('module')).toBe(true)
    expect(onboarding.startModuleTour(key, false)).toBe(false)
    expect(onboarding.startModuleTour(key)).toBe(true)
  })

  it('重看使用切换后的语言，并支持从 restart 入口重看', () => {
    onboarding.startModuleTour('geoTools')
    const title = driverState.instances.at(-1).config.steps[0].popover.title
    i18n.global.locale.value = 'en'
    onboarding.restart('module', 'geoTools')
    expect(onboarding.isTourActive()).toBe(true)
    const translated = driverState.instances.at(-1).config.steps[0].popover.title
    expect(translated).not.toBe(title)
    expect(translated).not.toMatch(/^navigationGuide\./)
  })

  it('替换正在运行的模块导览时保留两个模块的已看记录', () => {
    onboarding.startModuleTour('geoTools')
    renderShell('customer')
    onboarding.startModuleTour('customer')
    onboarding.destroy()
    expect(onboarding.startModuleTour('customer', false)).toBe(false)
    renderShell('geoTools')
    expect(onboarding.startModuleTour('geoTools', false)).toBe(false)
  })

  it.each(['close', 'escape', 'overlay'])('%s 退出后不再自动打扰，同一模块仍能手动重看且不影响其它模块', reason => {
    expect(onboarding.startModuleTour('geoTools')).toBe(true)
    driverState.instances.at(-1).config.onDestroyed(undefined, undefined, { reason })
    expect(onboarding.isTourActive()).toBe(false)
    expect(onboarding.isCompleted('module')).toBe(false)
    expect(onboarding.startModuleTour('geoTools', false)).toBe(false)
    expect(onboarding.startModuleTour('geoTools', true)).toBe(true)
    onboarding.destroy()
    renderShell('customer')
    expect(onboarding.startModuleTour('customer')).toBe(true)
  })

  it('未知键、未下发模块、未登录、非当前 tab 和缺目标均拒绝启动', () => {
    expect(onboarding.startModuleTour('unknown')).toBe(false)
    expect(onboarding.startModuleTour('__proto__')).toBe(false)
    expect(onboarding.startModuleTour('customer')).toBe(false)
    permission.menus = permission.menus.filter(menu => menu.key !== 'geoTools')
    expect(onboarding.startModuleTour('geoTools', true)).toBe(false)
    permission.menus = [{ key: 'geoTools', children: [{ key: 'geoPage', path: '/geo-tools/config' }] }]
    document.querySelector('.app-aside').remove()
    expect(onboarding.startModuleTour('geoTools', true)).toBe(false)
    renderShell()
    document.querySelector('.app-main').remove()
    expect(onboarding.startModuleTour('geoTools', true)).toBe(false)
    renderShell()
    user.logout()
    expect(onboarding.startModuleTour('geoTools', true)).toBe(false)
    expect(driverState.instances).toHaveLength(0)
  })
})

describe('页内治理工具导览', () => {
  it.each([
    ['/crm/list', '<section class="crm-page"><header class="page-head"></header><div class="el-card"><form class="el-form"></form><div class="el-table"></div></div></section>'],
    ['/leads/rules', '<section class="leads-page"><header class="page-head"></header><div class="el-table"></div></section>'],
  ])('%s 没有受权限控制的工作流入口时仍可引导稳定内容，缺表格则拒绝', (route, html) => {
    document.querySelector('.app-main').innerHTML = html
    expect(onboarding.startPageTour(route, true)).toBe(true)
    expect(driverState.instances.at(-1).drive).toHaveBeenCalledOnce()
    onboarding.destroy()
    document.querySelector('.el-table').remove()
    const warning = vi.spyOn(console, 'warn').mockImplementation(() => {})
    try {
      expect(onboarding.startPageTour(route, true)).toBe(false)
      expect(onboarding.isTourActive()).toBe(false)
    } finally {
      warning.mockRestore()
    }
  })
})

describe('模块导览随布局导航清理', () => {
  it('同模块切页保留导览，浏览器后退跨模块时关闭旧导览', async () => {
    document.body.innerHTML = ''
    permission.menus = [
      { key: 'customer', children: [{ key: 'crmList', path: '/crm/list' }] },
      { key: 'geoTools', children: [
        { key: 'geoConfig', path: '/geo-tools/config' },
        { key: 'geoProbes', path: '/geo-tools/probes' }
      ] }
    ]
    onboarding.markPageSeen('/crm/list')
    onboarding.markPageSeen('/geo-tools/config')
    const router = createRouter({
      history: createMemoryHistory(),
      routes: ['/crm/list', '/geo-tools/config', '/geo-tools/probes'].map(path => ({
        path, component: { render: () => null }
      }))
    })
    await router.push('/crm/list')
    const wrapper = mount(Layout, {
      attachTo: document.body,
      global: {
        plugins: [getActivePinia(), router, ElementPlus, i18n],
        stubs: { MessageNotification: true, SubscriptionBanner: true, LanguageSwitcher: true }
      }
    })
    try {
      await settle(wrapper)
      await router.push('/geo-tools/config')
      await settle(wrapper)
      expect(onboarding.startModuleTour('geoTools')).toBe(true)
      const instance = driverState.instances.at(-1)
      await router.push('/geo-tools/probes')
      await settle(wrapper)
      expect(onboarding.isTourActive()).toBe(true)
      router.back()
      await settle(wrapper)
      expect(router.currentRoute.value.path).toBe('/geo-tools/config')
      expect(onboarding.isTourActive()).toBe(true)
      router.back()
      await settle(wrapper)
      expect(router.currentRoute.value.path).toBe('/crm/list')
      expect(onboarding.isTourActive()).toBe(false)
      expect(instance.destroy).toHaveBeenCalledOnce()
      onboarding.startGlobalTour()
      expect(onboarding.getActiveTourType()).toBe('global')
      const globalInstance = driverState.instances.at(-1)
      await router.push('/geo-tools/config')
      await settle(wrapper)
      expect(onboarding.isTourActive()).toBe(true)
      expect(globalInstance.destroy).not.toHaveBeenCalled()
    } finally {
      wrapper.unmount()
    }
  })
})
