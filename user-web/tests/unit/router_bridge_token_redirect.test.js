import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import { h } from 'vue'
import ElementPlus from 'element-plus'
import { settle } from './settle-helper'

const legacyApi = vi.hoisted(() => ({
  getBridgeTokenStatus: vi.fn(),
  resetBridgeToken: vi.fn()
}))

vi.mock('@/api/bridgeToken.js', () => legacyApi)

import bridgeRoutes from '@/router/modules/bridgeToken.js'
import channelRoutes from '@/router/modules/channels.js'

const IdentifyCodeStub = {
  render() {
    return h('div', { class: 'identify-code-stub' }, '渠道识别码管理')
  }
}

const ExtensionStub = {
  render() {
    return h('div', { class: 'extension-stub' }, '扩展安装包')
  }
}

function testRouter() {
  const identifyRoute = channelRoutes.find((route) => route.path === '/channels/identify-codes')
  const routes = [
    ...bridgeRoutes.map((route) => route.path === '/bridge/extension'
      ? { ...route, component: ExtensionStub }
      : route),
    { ...identifyRoute, component: IdentifyCodeStub }
  ]
  return createRouter({ history: createMemoryHistory(), routes })
}

async function navigate(path) {
  const router = testRouter()
  const wrapper = mount({ render: () => h(RouterView) }, {
    global: { plugins: [router, ElementPlus] }
  })
  await router.push(path)
  await flushPromises()
  await settle(wrapper)
  return { router, wrapper }
}

beforeEach(() => {
  vi.clearAllMocks()
  legacyApi.getBridgeTokenStatus.mockResolvedValue({
    retired: true,
    main_configured: true,
    prev_configured: true,
    source: 'db',
    token: 'legacy-token'
  })
  legacyApi.resetBridgeToken.mockResolvedValue({ token: 'legacy-token' })
})

describe('retired Bridge token routes converge on identify-code management', () => {
  it.each(['/bridge', '/bridge/token', '/token'])('%s redirects without mounting the retired token page', async (path) => {
    const { router, wrapper } = await navigate(path)

    expect(router.currentRoute.value.path).toBe('/channels/identify-codes')
    expect(wrapper.find('.identify-code-stub').exists()).toBe(true)
    expect(wrapper.text()).not.toContain('生成新凭证')
    expect(wrapper.text()).not.toContain('灰度窗口')
    expect(wrapper.text()).not.toContain('PREV')
    expect(legacyApi.getBridgeTokenStatus).not.toHaveBeenCalled()
    expect(legacyApi.resetBridgeToken).not.toHaveBeenCalled()
  })

  it('old route definitions are redirects, not lazy token components', () => {
    for (const path of ['/bridge', '/bridge/token', '/token']) {
      const route = bridgeRoutes.find((candidate) => candidate.path === path)
      expect(route, `${path} route missing`).toBeTruthy()
      expect(route.redirect, `${path} redirect`).toBe('/channels/identify-codes')
      expect(route.component, `${path} must not mount TokenManagement.vue`).toBeUndefined()
    }
  })

  it('preserves the admin boundary on the resolved identify-code route', () => {
    const identifyRoute = channelRoutes.find((route) => route.path === '/channels/identify-codes')
    expect(identifyRoute.meta.requiresAdmin).toBe(true)
    for (const path of ['/bridge', '/bridge/token']) {
      const route = bridgeRoutes.find((candidate) => candidate.path === path)
      expect(route.meta?.requiresAdmin).toBe(true)
    }
  })

  it('keeps the extension package route intact', () => {
    const route = bridgeRoutes.find((candidate) => candidate.path === '/bridge/extension')
    expect(route.name).toBe('BridgeExtensionPackage')
    expect(route.redirect).toBeUndefined()
    expect(typeof route.component).toBe('function')
  })
})
