import { describe, expect, it, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import i18n from '@/i18n'
import ChannelManage from '@/views/channels/channel-manage.vue'
import AccountManage from '@/views/channels/account-manage.vue'
import channelAccountApi from '@/api/channelAccount'
import { CHANNEL_OPTIONS, getChannelLabel } from '@/constants/channel'
import { settle } from './settle-helper'

vi.mock('@/api/channelAccount', () => ({ default: {
  listChannels: vi.fn(), listAccounts: vi.fn(), getAccount: vi.fn(),
  listLeadMiningAgents: vi.fn(), updateAccount: vi.fn(), updateChannel: vi.fn()
} }))
vi.mock('@/api/aiAgent', () => ({ listAgents: vi.fn().mockResolvedValue([]) }))

beforeEach(() => {
  i18n.global.locale.value = 'zh'
  vi.clearAllMocks()
  channelAccountApi.listChannels.mockResolvedValue({ list: [{ id: 2, name: 'QQ', code: 'qq', type: 'dm', remark: '渠道备注', enabled: true }] })
  channelAccountApi.listAccounts.mockResolvedValue({ list: [{ id: 1, channel_id: 2, name: '四川同咨客服账号', type: 'group', status: 1 }] })
  channelAccountApi.getAccount.mockResolvedValue({ remark: '账号备注', owner_user_ids: [], data_scope: '{}' })
  channelAccountApi.listLeadMiningAgents.mockResolvedValue([])
})

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

  it('渠道列表展示实际返回数据，编辑无需固定会话类型且保留备注', async () => {
    const wrapper = mount(ChannelManage, { global: { plugins: [ElementPlus, i18n] } })
    try {
      await settle(wrapper)
      expect(wrapper.text()).toContain('QQ')
      expect(wrapper.findAll('th').map(cell => cell.text())).not.toContain('类型')
      await clickButton(wrapper, '编辑')
      expect(wrapper.find('.el-dialog').text()).not.toContain('单聊')
      await clickButton(wrapper, '保存')
      expect(channelAccountApi.updateChannel).toHaveBeenCalledWith(2, { name: 'QQ', code: 'qq', enabled: true, remark: '渠道备注' })
    } finally { wrapper.unmount() }
  })

  it('四川同咨的历史 SageX 绑定可见，接入服务不混入平台列表', async () => {
    channelAccountApi.listChannels.mockResolvedValue({ list: [
      { id: 1, name: '四川同咨', code: 'sagex-t18', enabled: true },
      { id: 2, name: 'QQ', code: 'qq', enabled: true }
    ] })
    const wrapper = mount(ChannelManage, { global: { plugins: [ElementPlus, i18n] } })
    try {
      await settle(wrapper)
      expect(wrapper.get('[data-testid="legacy-service-bindings"]').text()).toContain('四川同咨')
      expect(wrapper.get('[data-testid="legacy-service-bindings"]').text()).toContain('SageX')
      expect(wrapper.get('.el-table').text()).toContain('QQ')
      expect(wrapper.get('.el-table').text()).not.toContain('sagex-t18')
    } finally { wrapper.unmount() }
  })

  it('账号编辑无需单聊群聊选择，保存详情备注', async () => {
    const wrapper = mount(AccountManage, { global: { plugins: [ElementPlus, i18n] } })
    try {
      await settle(wrapper)
      expect(wrapper.text()).toContain('四川同咨客服账号')
      expect(wrapper.findAll('th').map(cell => cell.text())).not.toContain('类型')
      await clickButton(wrapper, '编辑')
      expect(wrapper.find('.el-dialog').text()).not.toContain('会话类型')
      await clickButton(wrapper, '保存')
      expect(channelAccountApi.updateAccount).toHaveBeenCalledWith(1, expect.objectContaining({ remark: '账号备注' }))
      expect(channelAccountApi.updateAccount.mock.calls[0][1]).not.toHaveProperty('type')
    } finally { wrapper.unmount() }
  })
})
