import { beforeEach, afterEach, describe, it, expect, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createPinia } from 'pinia'
import { settle, settleBody } from './settle-helper'
import i18n from '@/i18n'
const api = vi.hoisted(() => ({ getPlatforms: vi.fn(), getStats: vi.fn(), getMessages: vi.fn(), getMessageById: vi.fn(), markRead: vi.fn(), recallMessage: vi.fn(), pushMessage: vi.fn() }))
vi.mock('@/api/messageHub', () => ({ messageHubApi: api }))
vi.mock('@/api/customerService', () => ({ getMyAgent: async () => ({}) }))
vi.mock('@/utils/agentSocket', () => ({ default: class { connect() {} close() {} } }))
import List from '@/views/messageHub/List.vue'
let wrapper
const row = (id, content) => ({ id, content, msg_id: `m-${id}`, platform: 'douyin', msg_type: 'text', direction: 'inbound', is_read: false })
beforeEach(() => {
  for (const fn of Object.values(api)) fn.mockReset()
  api.getPlatforms.mockResolvedValue({ platforms: ['douyin'], msg_types: ['text'] })
  api.getStats.mockResolvedValue({ total: 1 })
  api.getMessages.mockResolvedValue({ list: [row(1, '初始消息')], total: 1 })
})
afterEach(() => { wrapper?.unmount(); document.body.replaceChildren() })
async function page() { wrapper = mount(List, { attachTo: document.body, global: { plugins: [createPinia(), ElementPlus, i18n] } }); await settle(wrapper) }
describe('消息中台请求和详情状态', () => {
  it('较旧的搜索响应不能覆盖新列表', async () => {
    await page()
    let resolveOld
    api.getMessages.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve })).mockResolvedValueOnce({ list: [row(2, '新结果')], total: 1 })
    const old = wrapper.vm.fetchMessageList()
    await wrapper.vm.fetchMessageList()
    resolveOld({ list: [row(3, '过时结果')], total: 1 })
    await old; await settle(wrapper)
    expect(wrapper.text()).toContain('新结果')
    expect(wrapper.text()).not.toContain('过时结果')
  })
  it('列表失败清旧行和批量选择，重试恢复', async () => {
    await page()
    wrapper.vm.handleSelectionChange([row(1, '初始消息')])
    api.getMessages.mockRejectedValueOnce(new Error('列表受控失败'))
    await wrapper.vm.fetchMessageList(); await settle(wrapper)
    expect(wrapper.findAll('.el-table__row')).toHaveLength(0)
    expect(wrapper.find('.batch-bar').exists()).toBe(false)
    expect(wrapper.text()).toContain('列表受控失败')
    await wrapper.findAll('button').find(button => button.text() === i18n.global.t('重试')).trigger('click')
    await settle(wrapper)
    expect(wrapper.text()).toContain('初始消息')
  })
  it('详情失败不冒充旧消息，较旧详情不覆盖新详情', async () => {
    await page()
    let resolveOld
    api.getMessageById.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve })).mockResolvedValueOnce(row(2, '新详情'))
    const old = wrapper.vm.handleViewDetail({ id: 1 })
    await wrapper.vm.handleViewDetail({ id: 2 })
    resolveOld(row(1, '旧详情'))
    await old; await settleBody()
    expect(document.body.textContent).toContain('新详情')
    expect(document.body.textContent).not.toContain('旧详情')
    api.getMessageById.mockRejectedValueOnce(new Error('详情受控失败'))
    await wrapper.vm.handleViewDetail({ id: 3 }); await settleBody()
    expect(document.body.textContent).toContain('详情受控失败')
    expect(document.body.textContent).not.toContain('新详情')
  })
  it('手动推送校验阶段即拒绝重复提交', async () => {
    await page()
    let resolveValidation
    wrapper.vm.openPushDialog()
    await settleBody()
    const validate = vi.spyOn(wrapper.vm.pushFormRef, 'validate').mockReturnValue(new Promise(resolve => { resolveValidation = resolve }))
    api.pushMessage.mockResolvedValue({ id: 99 })
    const first = wrapper.vm.submitPush()
    await wrapper.vm.submitPush()
    expect(validate).toHaveBeenCalledTimes(1)
    expect(api.pushMessage).not.toHaveBeenCalled()
    resolveValidation(true)
    await first
    expect(api.pushMessage).toHaveBeenCalledTimes(1)
    validate.mockRestore()
  })
  it('未读筛选中标记成功刷新列表和统计', async () => {
    await page()
    api.markRead.mockResolvedValue({ updated: 1 })
    api.getMessages.mockResolvedValue({ list: [], total: 0 })
    const calls = api.getMessages.mock.calls.length
    await wrapper.vm.handleMarkRead(row(1, '初始消息')); await settle(wrapper)
    expect(api.getMessages).toHaveBeenCalledTimes(calls + 1)
    expect(wrapper.findAll('.el-table__row')).toHaveLength(0)
  })
})
