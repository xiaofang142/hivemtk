import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createPinia } from 'pinia'
import i18n from '@/i18n'
import ChannelManage from '@/views/channels/channel-manage.vue'
import AccountManage from '@/views/channels/account-manage.vue'
import channelAccountApi from '@/api/channelAccount'
import channelCatalogApi from '@/api/channelCatalog'
import { CHANNEL_OPTIONS, getChannelLabel } from '@/constants/channel'
import { settle, settleBody } from './settle-helper'

vi.mock('@/api/channelAccount', () => ({ default: {
  listChannels: vi.fn(), listAccounts: vi.fn(), getAccount: vi.fn(),
  listLeadMiningAgents: vi.fn(), updateAccount: vi.fn(), updateChannel: vi.fn()
} }))
vi.mock('@/api/channelCatalog', () => ({ default: {
  catalog: vi.fn(), setTenantEnabled: vi.fn(), managedAccounts: vi.fn(),
  updateManagedAccount: vi.fn(), setAccountStatus: vi.fn()
} }))
vi.mock('@/api/aiAgent', () => ({ listAgents: vi.fn().mockResolvedValue([]) }))

beforeEach(() => {
  i18n.global.locale.value = 'zh'
  vi.clearAllMocks()
  channelAccountApi.listChannels.mockResolvedValue({ list: [{ id: 2, name: 'QQ', code: 'qq', type: 'dm', remark: '渠道备注', enabled: true }] })
  channelAccountApi.listAccounts.mockResolvedValue({ list: [{ id: 1, channel_id: 2, name: '示例公司客服账号', type: 'group', status: 1, self_identify_name: '示例公司微信' }] })
  channelAccountApi.getAccount.mockResolvedValue({ remark: '账号备注', owner_user_ids: [], data_scope: '{}' })
  channelAccountApi.listLeadMiningAgents.mockResolvedValue([])
  channelCatalogApi.catalog.mockResolvedValue({
    channels: [{ id: 2, code: 'qq', name: 'QQ', label: 'QQ', enabled: true }],
    accounts: []
  })
  channelCatalogApi.setTenantEnabled.mockResolvedValue({})
  channelCatalogApi.managedAccounts.mockResolvedValue({ list: [{ id: 1, channel_id: 2, name: '示例公司客服账号', type: 'group', status: 1, self_identify_name: '示例公司微信' }] })
})

const mountPage = (Component) => mount(Component, { global: { plugins: [createPinia(), ElementPlus, i18n] } })

const clickButton = async (wrapper, text) => {
  await wrapper.findAll('button').find(button => button.text() === text).trigger('click')
  await settle(wrapper)
}

describe('渠道与会话类型分离', () => {
  it('QQ 和个人微信均是平台选项，服务名称不作为平台', () => {
    expect(getChannelLabel('qq')).toBe('QQ')
    expect(CHANNEL_OPTIONS.some(option => option.value === 'qq')).toBe(true)
    expect(CHANNEL_OPTIONS.some(option => option.value === 'personal_wx')).toBe(true)
    expect(CHANNEL_OPTIONS.some(option => /sagex|magex/i.test(option.value))).toBe(false)
  })

  it('渠道列表只提供租户开关，不再有编辑弹窗且调用统一开关接口', async () => {
    const wrapper = mountPage(ChannelManage)
    try {
      await settle(wrapper)
      expect(wrapper.text()).toContain('QQ')
      expect(wrapper.findAll('th').map(cell => cell.text())).not.toContain('类型')
      expect(wrapper.findAll('button').some(button => button.text() === '编辑')).toBe(false)
      expect(wrapper.findAll('button').some(button => button.text() === '保存')).toBe(false)
      await wrapper.find('.el-switch').trigger('click')
      await settle(wrapper)
      expect(channelCatalogApi.setTenantEnabled).toHaveBeenCalledWith(2, false)
      expect(channelAccountApi.updateChannel).not.toHaveBeenCalled()
    } finally { wrapper.unmount() }
  })

  it('示例公司的历史 SageX 绑定可见，接入服务不混入平台列表', async () => {
    channelAccountApi.listChannels.mockResolvedValue({ list: [
      { id: 1, name: '示例公司', code: 'sagex-t18', enabled: true },
      { id: 2, name: 'QQ', code: 'qq', enabled: true }
    ] })
    const wrapper = mountPage(ChannelManage)
    try {
      await settle(wrapper)
      expect(wrapper.get('[data-testid="legacy-service-bindings"]').text()).toContain('示例公司')
      expect(wrapper.get('[data-testid="legacy-service-bindings"]').text()).toContain('SageX')
      expect(wrapper.get('.el-table').text()).toContain('QQ')
      expect(wrapper.get('.el-table').text()).not.toContain('sagex-t18')
    } finally { wrapper.unmount() }
  })

  it('账号编辑无需单聊群聊选择，保存详情备注', async () => {
    const wrapper = mountPage(AccountManage)
    try {
      await settle(wrapper)
      expect(wrapper.text()).toContain('示例公司客服账号')
      expect(wrapper.findAll('th').map(cell => cell.text())).not.toContain('类型')
      await clickButton(wrapper, '编辑')
      expect(wrapper.find('.el-dialog').text()).not.toContain('会话类型')
      await clickButton(wrapper, '保存')
      expect(channelAccountApi.updateAccount).toHaveBeenCalledWith(1, expect.objectContaining({ remark: '账号备注' }))
      expect(channelAccountApi.updateAccount.mock.calls[0][1]).not.toHaveProperty('type')
    } finally { wrapper.unmount() }
  })

  it('自他判断名称（发送者标记）列表可见，清空标记拒绝保存', async () => {
    const wrapper = mountPage(AccountManage)
    try {
      await settle(wrapper)
      expect(wrapper.findAll('th').map(cell => cell.text())).toContain('发送者标记')
      expect(wrapper.text()).toContain('示例公司微信')
      await clickButton(wrapper, '编辑')
      expect(wrapper.find('.el-dialog').text()).toContain('自他判断名称（上报发送者标记）')
      const markInput = wrapper.findAll('.el-dialog input').find(
        input => input.attributes('placeholder') === '请输入上报消息里的发送者名称，如「示例公司微信」'
      )
      expect(markInput).toBeTruthy()
      await markInput.setValue('')
      await clickButton(wrapper, '保存')
      expect(channelAccountApi.updateAccount).not.toHaveBeenCalled()
      await settleBody()
      expect(document.body.textContent).toContain('自他判断名称（上报发送者标记）必填')
    } finally { wrapper.unmount() }
  })
})
