import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus, { ElMessage, ElMessageBox } from 'element-plus'
import { createPinia } from 'pinia'
import i18n from '@/i18n'
import { settle } from './settle-helper'

const api = vi.hoisted(() => ({
  list: vi.fn(),
  get: vi.fn(),
  rotate: vi.fn(),
  rotateForAccount: vi.fn(),
  listChannels: vi.fn(),
  catalog: vi.fn(),
  managedAccounts: vi.fn(),
  setTenantEnabled: vi.fn()
}))

vi.mock('@/api/channelIdentifyCode.js', () => ({
  default: {
    list: api.list,
    get: api.get,
    rotate: api.rotate,
    rotateForAccount: api.rotateForAccount
  },
  channelIdentifyCodeApi: {
    list: api.list,
    get: api.get,
    rotate: api.rotate,
    rotateForAccount: api.rotateForAccount
  }
}))

vi.mock('@/api/channelCatalog.js', () => ({
  default: {
    catalog: api.catalog,
    managedAccounts: api.managedAccounts,
    setTenantEnabled: api.setTenantEnabled,
    updateManagedAccount: vi.fn(),
    setAccountStatus: vi.fn()
  }
}))

vi.mock('@/api/channelAccount.js', () => ({
  default: { listChannels: api.listChannels }
}))

vi.mock('element-plus', async (importOriginal) => {
  const actual = await importOriginal()
  return {
    ...actual,
    ElMessage: {
      success: vi.fn(),
      error: vi.fn(),
      warning: vi.fn(),
      info: vi.fn()
    },
    ElMessageBox: { confirm: vi.fn() }
  }
})

import IdentifyCodeManage from '@/views/channels/identify-code-manage.vue'

const ROWS = [
  { id: 1, channel_id: 10, code: 'cid_old', version: 1, reset_at: null }
]
const CHANNELS = [
  { id: 10, name: 'Telegram' },
  { id: 11, name: 'Slack' }
]

async function mountPage() {
  const wrapper = mount(IdentifyCodeManage, {
    global: { plugins: [createPinia(), ElementPlus, i18n] }
  })
  await settle(wrapper)
  return wrapper
}

function button(wrapper, label) {
  const found = wrapper.findAll('button').find((candidate) => candidate.text().trim() === label)
  if (!found) throw new Error(`button not found: ${label}`)
  return found
}

beforeEach(() => {
  vi.clearAllMocks()
  api.list.mockReset().mockResolvedValue({ list: [...ROWS] })
  api.get.mockReset().mockResolvedValue({ ...ROWS[0] })
  api.rotate.mockReset().mockResolvedValue({ code: 'cid_new', version: 2 })
  api.rotateForAccount.mockReset().mockResolvedValue({ code: 'cid_account_new' })
  api.listChannels.mockReset().mockResolvedValue({ list: [...CHANNELS] })
  api.catalog.mockReset().mockResolvedValue({
    channels: CHANNELS.map((c) => ({ id: c.id, code: String(c.id), name: c.name, label: c.name, enabled: true })),
    accounts: []
  })
  api.managedAccounts.mockReset().mockResolvedValue({ list: [] })
  ElMessageBox.confirm.mockReset().mockResolvedValue('confirm')
})

describe('identify-code management rotate/error/recovery', () => {
  it('keeps the legitimate short in-flight window and does not render retired PREV grace copy', async () => {
    const wrapper = await mountPage()
    expect(wrapper.text()).toContain('短暂在途窗口')
    expect(wrapper.text()).not.toContain('灰度窗口')
    expect(wrapper.text()).not.toContain('PREV')
    expect(wrapper.text()).not.toContain('prev_configured')
  })

  it('rotates an existing code, shows the new code, and reloads the list', async () => {
    const wrapper = await mountPage()
    await button(wrapper, '重置').trigger('click')
    await settle(wrapper)

    expect(ElMessageBox.confirm).toHaveBeenCalledTimes(1)
    expect(api.rotate).toHaveBeenCalledWith(10)
    expect(ElMessage.success).toHaveBeenCalledWith(expect.stringContaining('cid_new'))
    expect(api.list).toHaveBeenCalledTimes(2)
  })

  it('does not rotate when confirmation is cancelled', async () => {
    ElMessageBox.confirm.mockRejectedValueOnce('cancel')
    const wrapper = await mountPage()
    await button(wrapper, '重置').trigger('click')
    await settle(wrapper)

    expect(api.rotate).not.toHaveBeenCalled()
    expect(ElMessage.error).not.toHaveBeenCalled()
  })

  it('surfaces rotate API failure and never shows a success toast', async () => {
    api.rotate.mockRejectedValueOnce(new Error('rotate failed'))
    const wrapper = await mountPage()
    await button(wrapper, '重置').trigger('click')
    await settle(wrapper)

    expect(ElMessage.success).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith(expect.stringContaining('rotate failed'))
    expect(wrapper.find('.rotation-error').exists()).toBe(true)
    expect(wrapper.find('.rotation-error').text()).toContain('rotate failed')
    expect(api.list).toHaveBeenCalledTimes(1)
  })

  it('recovers after a failed rotate and clears the inline error on success', async () => {
    api.rotate
      .mockRejectedValueOnce(new Error('temporary failure'))
      .mockResolvedValueOnce({ code: 'cid_recovered', version: 2 })
    const wrapper = await mountPage()

    await button(wrapper, '重置').trigger('click')
    await settle(wrapper)
    expect(wrapper.find('.rotation-error').exists()).toBe(true)

    await button(wrapper, '重置').trigger('click')
    await settle(wrapper)
    expect(ElMessage.success).toHaveBeenCalledWith(expect.stringContaining('cid_recovered'))
    expect(wrapper.find('.rotation-error').exists()).toBe(false)
    expect(api.list).toHaveBeenCalledTimes(2)
  })

  it('generate failure is handled and selection recovers for another attempt', async () => {
    api.rotate
      .mockRejectedValueOnce(new Error('generate failed'))
      .mockResolvedValueOnce({ code: 'cid_generated', version: 1 })
    const wrapper = await mountPage()
    wrapper.vm.newChannelId = 11
    await wrapper.vm.$nextTick()

    await button(wrapper, '生成识别码').trigger('click')
    await settle(wrapper)
    expect(ElMessage.error).toHaveBeenCalledWith(expect.stringContaining('generate failed'))
    expect(wrapper.find('.rotation-error').text()).toContain('generate failed')
    expect(wrapper.vm.newChannelId).toBeNull()

    wrapper.vm.newChannelId = 11
    await wrapper.vm.$nextTick()
    await button(wrapper, '生成识别码').trigger('click')
    await settle(wrapper)
    expect(ElMessage.success).toHaveBeenCalledWith(expect.stringContaining('cid_generated'))
    expect(wrapper.find('.rotation-error').exists()).toBe(false)
    expect(wrapper.vm.newChannelId).toBeNull()
  })
})
