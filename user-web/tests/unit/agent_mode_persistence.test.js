import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh.json'
import { settle } from './settle-helper'

const api = vi.hoisted(() => ({ createAgent: vi.fn(), updateAgent: vi.fn(), getAgent: vi.fn(), findAgentByCode: vi.fn(), testAgent: vi.fn() }))
const route = vi.hoisted(() => ({ params: {} }))
const push = vi.hoisted(() => vi.fn())
vi.mock('vue-router', () => ({ useRoute: () => route, useRouter: () => ({ push }) }))
vi.mock('@/api/aiAgent.js', () => api)
vi.mock('@/api/ragProductConfig', () => ({ ragProductConfigAPI: { getRagProducts: async () => ({ list: [] }) } }))
vi.mock('@/api/sopAgent.js', () => ({ sopApi: { list: async () => ({ list: [] }) } }))
vi.mock('@/api/scriptTemplate.js', () => ({ getScriptTemplateList: async () => ({ list: [] }) }))
vi.mock('@/api/faq', () => ({ faqApi: {} }))
vi.mock('@/api/sopTemplate', () => ({ sopTemplateApi: {} }))
vi.mock('@/api/knowledgeBase', () => ({ listByType: async () => ({ list: [] }) }))
vi.mock('@/api/agentKBBinding', () => ({ listByAgent: async () => [], replaceAgentKBs: async () => ({}) }))
vi.mock('@/api/assetBundle', () => ({ listBundles: async () => ({ list: [] }) }))
vi.mock('@/api/assetMarket', () => ({ listLocalAssets: async () => ({ list: [] }) }))
vi.mock('@/i18n', () => ({ default: { global: { t: (key) => key } } }))
import Edit from '@/views/aiAgent/Edit.vue'

let wrapper
const record = (mode) => ({ id: 7, agent_code: 'mode-test', name: '模式验收', persona: '受控人设', agent_type: 'sales', agent_mode: mode })
const form = () => wrapper.findComponent({ name: 'ElForm' }).props('model')
async function open(mode) {
  route.params = mode ? { id: '7' } : {}
  api.getAgent.mockResolvedValue(record(mode))
  wrapper = mount(Edit, {
    attachTo: document.body,
    global: { plugins: [ElementPlus, createI18n({ legacy: false, locale: 'zh', messages: { zh }, missingWarn: false, fallbackWarn: false })] },
  })
  await settle(wrapper)
  if (!mode) Object.assign(form(), record('passive'), { id: undefined })
}
async function save() {
  const button = wrapper.findAll('button').find((item) => ['保存更新', '创建智能体'].includes(item.text()))
  expect(button).toBeTruthy()
  await button.trigger('click')
  await settle(wrapper)
}
beforeEach(() => {
  vi.clearAllMocks()
  api.findAgentByCode.mockResolvedValue(null)
  api.createAgent.mockImplementation(async (data) => { api.getAgent.mockResolvedValue({ ...data, id: 7 }); return { id: 7 } })
  api.updateAgent.mockImplementation(async (id, data) => { api.getAgent.mockResolvedValue({ ...data, id: Number(id) }); return { id: Number(id) } })
  vi.spyOn(ElMessage, 'success').mockImplementation(() => {})
  vi.spyOn(ElMessage, 'error').mockImplementation(() => {})
})
afterEach(() => { wrapper?.unmount(); wrapper = null; vi.restoreAllMocks(); document.body.replaceChildren() })

describe('智能体模式保存契约', () => {
  it.each(['active', 'passive'])('创建传递 %s，并可重载回显', async (mode) => {
    await open()
    form().agent_mode = mode
    await save()
    expect(api.createAgent).toHaveBeenCalledWith(expect.objectContaining({ agent_mode: mode }))
    expect(ElMessage.success).toHaveBeenCalled()
    wrapper.unmount()
    await open(mode)
    expect(form().agent_mode).toBe(mode)
  })
  it.each([['active', 'passive'], ['passive', 'active']])('修改 %s 为 %s', async (from, to) => {
    await open(from)
    expect(form().agent_mode).toBe(from)
    form().agent_mode = to
    await save()
    expect(api.updateAgent).toHaveBeenCalledWith('7', expect.objectContaining({ agent_mode: to }))
    wrapper.unmount()
    await open(to)
    expect(form().agent_mode).toBe(to)
  })
  it.each(['create', 'update'])('%s 保存拒绝时不提示成功、不跳转且保留模式', async (operation) => {
    await open(operation === 'update' ? 'passive' : undefined)
    form().agent_mode = 'active'
    api[operation === 'update' ? 'updateAgent' : 'createAgent'].mockRejectedValueOnce(new Error('受控写入拒绝'))
    await save()
    expect(ElMessage.success).not.toHaveBeenCalled()
    expect(push).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalledWith(expect.stringContaining('受控写入拒绝'))
    expect(form().agent_mode).toBe('active')
  })
})
