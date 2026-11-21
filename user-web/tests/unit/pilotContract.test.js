import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createI18n } from 'vue-i18n'
import { settle } from './settle-helper'

const api = vi.hoisted(() => ({ get: vi.fn(), validate: vi.fn(), save: vi.fn() }))
vi.mock('@/api/pilotContract', () => ({ pilotContractApi: api }))
import PilotContractPanel from '@/views/system/PilotContractPanel.vue'
import zh from '@/i18n/locales/zh.json'

let wrapper
beforeEach(() => {
  api.get.mockReset()
  api.get.mockResolvedValue({
    configured: false, tenant_id: 42, installation: { install_id: 'ins-fake' },
    contract: { version: 'pilot-contract.v1', provider_mode: '', tenant_id: 42 },
    blockers: ['shop_authorization_missing'], production_gate: 'blocked',
    validation_signal: 'pending_live_validation', simulated: true
  })
})
afterEach(() => { wrapper?.unmount(); wrapper = undefined })

describe('PilotContract 配置骨架', () => {
  it('展示已有身份、未配置状态和真实平台尚未验证', async () => {
    wrapper = mount(PilotContractPanel, { global: { plugins: [ElementPlus, createI18n({ legacy: false, locale: 'zh', messages: { zh } })] } })
    await settle(wrapper)
    expect(wrapper.text()).toContain('ins-fake')
    expect(wrapper.text()).toContain('未配置')
    expect(wrapper.text()).toContain('pending_live_validation')
    expect(wrapper.text()).toContain('shop_authorization_missing')
    expect(wrapper.find('textarea').element.value).toContain('"provider_mode": ""')
  })

  it('读取失败显示故障，不显示未配置假结论', async () => {
    api.get.mockRejectedValue(new Error('fake unavailable'))
    wrapper = mount(PilotContractPanel, { global: { plugins: [ElementPlus, createI18n({ legacy: false, locale: 'zh', messages: { zh } })] } })
    await settle(wrapper)
    expect(wrapper.text()).toContain('读取失败')
    expect(wrapper.find('textarea').exists()).toBe(false)
  })
})
