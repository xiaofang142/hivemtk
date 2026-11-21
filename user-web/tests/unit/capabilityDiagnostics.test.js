import { afterEach, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createI18n } from 'vue-i18n'
import { createPinia } from 'pinia'
import { useUserStore } from '@/stores/user'
import { settle } from './settle-helper'

const api = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/api/capabilityDiagnostics', () => ({ capabilityDiagnosticsApi: api }))
import Diagnostics from '@/views/cap/Diagnostics.vue'
import zh from '@/i18n/locales/zh.json'
import en from '@/i18n/locales/en.json'

let wrapper
afterEach(() => {
  wrapper?.unmount()
  wrapper = undefined
})

it.each([
  ['zh', '已配置账号', '缺少可信的店铺授权映射'],
  ['en', 'Account configured', 'Trusted shop authorization mapping is missing']
])('在 %s 分开显示账号配置、动作阻塞原因和就绪计数', async (locale, configured, reason) => {
  api.get.mockResolvedValue({
    capabilities: [
      { platform: 'douyin_shop', capability: 'dm.send', via: 'bridge', implemented: true, configured: true, ready: false, reason_code: 'capability_mapping_missing' },
      { platform: 'douyin_shop', capability: 'dm.receive', via: 'bridge', implemented: true, configured: true, ready: true }
    ],
    total: 2,
    ready_count: 1
  })
  wrapper = mount(Diagnostics, { global: { plugins: [createPinia(), ElementPlus, createI18n({ legacy: false, locale, messages: { zh, en } })] } })
  await settle(wrapper)

  expect(wrapper.text()).toContain(configured)
  const send = wrapper.findAll('tbody tr').find(row => row.text().includes('dm.send'))
  expect(send.classes()).toContain('row-not-ready')
  expect(send.text()).toContain(reason)
  expect(send.text()).toContain('capability_mapping_missing')
  const receive = wrapper.findAll('tbody tr').find(row => row.text().includes('dm.receive'))
  expect(receive.classes()).not.toContain('row-not-ready')
  expect(wrapper.find('.summary-value').text()).toBe('1 / 2')
  expect(wrapper.text()).not.toContain('ready = implemented && configured')
})

it('mounts the shop summary for tenant admins and resets it when the tenant changes', async () => {
  api.get.mockResolvedValue({ capabilities: [], total: 0, ready_count: 0 })
  const pinia = createPinia()
  const user = useUserStore(pinia)
  const session = { id: 1, tenant_id: 71 }
  user.userInfo = { ...session, role: 'agent' }
  wrapper = mount(Diagnostics, {
    global: {
      plugins: [pinia, ElementPlus, createI18n({ legacy: false, locale: 'en', messages: { en } })],
      stubs: { CapabilityAdminPanel: { template: '<div data-test="shop-summary" />' } }
    }
  })
  expect(wrapper.find('[data-test="shop-summary"]').exists()).toBe(false)
  user.userInfo = { ...session, role: 'admin' }
  await settle(wrapper)
  const before = wrapper.get('[data-test="shop-summary"]').element
  user.userInfo = { ...session, tenant_id: 72, role: 'admin' }
  await settle(wrapper)
  expect(wrapper.get('[data-test="shop-summary"]').element).not.toBe(before)
})
