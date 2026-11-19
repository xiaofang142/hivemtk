import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { reactive } from 'vue'
import ElementPlus, { ElMessageBox } from 'element-plus'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh.json'
import { settle } from './settle-helper'

const api = vi.hoisted(() => ({ createAgent: vi.fn(), updateAgent: vi.fn(), getAgent: vi.fn(), testAgent: vi.fn() }))
const bindings = vi.hoisted(() => ({ listByAgent: vi.fn(), replaceAgentKBs: vi.fn() }))
const routing = vi.hoisted(() => ({ route: null, push: vi.fn() }))
vi.mock('vue-router', () => ({ useRoute: () => routing.route, useRouter: () => ({ push: routing.push }) }))
vi.mock('@/api/aiAgent.js', () => api)
vi.mock('@/api/ragProductConfig', () => ({ ragProductConfigAPI: { getRagProducts: async () => ({ list: [] }) } }))
vi.mock('@/api/sopAgent.js', () => ({ sopApi: { list: async () => ({ list: [] }) } }))
vi.mock('@/api/scriptTemplate.js', () => ({ getScriptTemplateList: async () => ({ list: [] }) }))
vi.mock('@/api/faq', () => ({ faqApi: {} }))
vi.mock('@/api/sopTemplate', () => ({ sopTemplateApi: {} }))
vi.mock('@/api/knowledgeBase', () => ({ listByType: async (type) => ({ list: type === 'faq' ? [{ id: 11, name: '原知识库' }, { id: 22, name: '新知识库' }] : [] }) }))
vi.mock('@/api/agentKBBinding', () => bindings)
vi.mock('@/api/assetBundle', () => ({ listBundles: async () => ({ list: [] }) }))
vi.mock('@/api/assetMarket', () => ({ listLocalAssets: async () => ({ list: [] }) }))
vi.mock('@/i18n', () => ({ default: { global: { t: (key) => key } } }))
import Edit from '@/views/aiAgent/Edit.vue'

let wrapper
const form = () => wrapper.findComponent({ name: 'ElForm' }).props('model')
const tree = () => wrapper.findComponent({ name: 'ElTreeSelect' })
const deferred = () => { let resolve, reject; const promise = new Promise((yes, no) => { resolve = yes; reject = no }); return { promise, resolve, reject } }
async function open(create = false) {
  routing.route = reactive({ params: create ? {} : { id: '7' } })
  wrapper = mount(Edit, { attachTo: document.body, global: { plugins: [ElementPlus, createI18n({ legacy: false, locale: 'zh', messages: { zh }, missingWarn: false, fallbackWarn: false })] } })
  await settle(wrapper)
  if (create) Object.assign(form(), { agent_code: 'kb-test', name: '绑定验收', persona: '受控人设' })
}
async function save() {
  await wrapper.findAll('button').find((item) => ['保存更新', '创建智能体'].includes(item.text())).trigger('click')
  await settle(wrapper)
}
async function reload() {
  await wrapper.findAll('button').find((item) => item.text().includes('加载知识库')).trigger('click')
  await settle(wrapper)
}
beforeEach(() => {
  vi.resetAllMocks()
  api.getAgent.mockResolvedValue({ id: 7, agent_code: 'kb-test', name: '绑定验收', persona: '受控人设', agent_type: 'sales', agent_mode: 'passive', kb_ids: [999] })
  api.updateAgent.mockResolvedValue({ id: 7 })
  api.createAgent.mockResolvedValue({ id: 8 })
  bindings.listByAgent.mockResolvedValue([{ kb_id: 11 }])
  bindings.replaceAgentKBs.mockResolvedValue({})
  vi.spyOn(ElMessageBox, 'confirm').mockResolvedValue('confirm')
})
afterEach(() => { wrapper?.unmount(); wrapper = null; vi.restoreAllMocks(); document.body.replaceChildren() })

describe('知识库绑定读取保护', () => {
  it('读取失败后修改名称只保存普通字段，禁止隐式清空绑定', async () => {
    bindings.listByAgent.mockRejectedValueOnce(new Error('读取拒绝'))
    await open()
    form().name = '新名称'
    await save()
    expect(api.updateAgent).toHaveBeenCalledWith('7', expect.objectContaining({ name: '新名称' }))
    expect(api.updateAgent.mock.calls[0][1]).not.toHaveProperty('kb_ids')
    expect(bindings.replaceAgentKBs).not.toHaveBeenCalled()
    expect(tree().props('disabled')).toBe(true)
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
  })
  it.each([{ list: [] }, { list: [{ kb_id: 11 }] }])('已知绑定 %j 未变时不发送全量替换', async ({ list }) => {
    bindings.listByAgent.mockResolvedValue(list)
    await open()
    await save()
    expect(bindings.replaceAgentKBs).not.toHaveBeenCalled()
    expect(ElMessageBox.confirm).not.toHaveBeenCalled()
    expect(tree().props('disabled')).toBe(false)
  })
  it('失败重试读回权威 ID 后允许更改，并发送规范化选择', async () => {
    bindings.listByAgent.mockRejectedValueOnce(new Error('暂时失败'))
    await open()
    await reload()
    expect(form().kb_ids).toEqual(['11'])
    expect(tree().props('disabled')).toBe(false)
    tree().vm.$emit('update:modelValue', ['22'])
    await save()
    expect(bindings.replaceAgentKBs).toHaveBeenCalledWith('7', ['22'])
  })
  it('显式清空先确认，取消时两个写接口都不调用', async () => {
    await open()
    tree().vm.$emit('update:modelValue', [])
    ElMessageBox.confirm.mockRejectedValueOnce('cancel')
    await save()
    expect(ElMessageBox.confirm).toHaveBeenCalledTimes(1)
    expect(api.updateAgent).not.toHaveBeenCalled()
    expect(bindings.replaceAgentKBs).not.toHaveBeenCalled()
    await save()
    expect(bindings.replaceAgentKBs).toHaveBeenCalledWith('7', [])
  })
  it('读取未完成时普通保存不写绑定，晚到读取不变成隐式写入', async () => {
    const pending = deferred()
    bindings.listByAgent.mockReturnValueOnce(pending.promise)
    await open()
    expect(tree().props('disabled')).toBe(true)
    await save()
    expect(bindings.replaceAgentKBs).not.toHaveBeenCalled()
    pending.resolve([{ kb_id: 11 }])
    await settle(wrapper)
    expect(bindings.replaceAgentKBs).not.toHaveBeenCalled()
  })
  it.each(['success', 'failure'])('较旧的 %s 不能覆盖新读取', async (outcome) => {
    const old = deferred()
    bindings.listByAgent.mockReturnValueOnce(old.promise)
    await open()
    bindings.listByAgent.mockResolvedValueOnce([{ kb_id: 22 }])
    // 调用同一真实按钮回调，模拟两个已经在途的重试请求。
    const button = wrapper.findAllComponents({ name: 'ElButton' }).find((item) => item.text().includes('加载知识库'))
    button.vm.$emit('click', new MouseEvent('click'))
    await settle(wrapper)
    if (outcome === 'success') old.resolve([{ kb_id: 11 }]); else old.reject(new Error('旧读取失败'))
    await settle(wrapper)
    expect(form().kb_ids).toEqual(['22'])
    expect(tree().props('disabled')).toBe(false)
  })
  it.each([{}, null, { list: 'invalid' }, { list: [{}] }, { list: [{ kb_id: 0 }] }])('未知响应 %j 不能当成空绑定', async (value) => {
    bindings.listByAgent.mockResolvedValue(value)
    await open()
    expect(tree().props('disabled')).toBe(true)
    await save()
    expect(bindings.replaceAgentKBs).not.toHaveBeenCalled()
  })
  it('已改选择时重载不覆盖用户操作', async () => {
    await open()
    tree().vm.$emit('update:modelValue', ['22'])
    await settle(wrapper)
    await reload()
    expect(form().kb_ids).toEqual(['22'])
    expect(bindings.listByAgent).toHaveBeenCalledTimes(1)
  })
  it('来源智能体变更后，旧请求不能写入新来源的绑定状态', async () => {
    const old = deferred()
    bindings.listByAgent.mockReturnValueOnce(old.promise)
    await open()
    routing.route.params.id = '8'
    await settle(wrapper)
    old.resolve([{ kb_id: 11 }])
    await settle(wrapper)
    expect(tree().props('disabled')).toBe(true)
    await save()
    expect(bindings.replaceAgentKBs).not.toHaveBeenCalled()
  })
  it('新建时允许从已知空集合选择，初始空集合不写绑定', async () => {
    await open(true)
    expect(tree().props('disabled')).toBe(false)
    await save()
    expect(bindings.replaceAgentKBs).not.toHaveBeenCalled()
    expect(api.createAgent.mock.calls[0][0]).not.toHaveProperty('kb_ids')
  })
  it('新建时仅把明确选择写入新建成功返回的智能体', async () => {
    await open(true)
    tree().vm.$emit('update:modelValue', ['22'])
    await save()
    expect(bindings.replaceAgentKBs).toHaveBeenCalledWith(8, ['22'])
  })
  it('卸载后晚到的读取不再改写表单快照', async () => {
    const old = deferred()
    bindings.listByAgent.mockReturnValueOnce(old.promise)
    await open()
    const originalForm = form()
    wrapper.unmount()
    wrapper = null
    old.resolve([{ kb_id: 22 }])
    await old.promise
    await Promise.resolve()
    expect(originalForm.kb_ids).toEqual([])
    expect(bindings.replaceAgentKBs).not.toHaveBeenCalled()
  })
})
