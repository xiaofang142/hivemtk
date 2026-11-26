import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { createPinia } from 'pinia'
import { createI18n } from 'vue-i18n'
import zh from '@/i18n/locales/zh.json'
import { settle } from './settle-helper'

const api = vi.hoisted(() => ({ listTools: vi.fn(), updateToolStatus: vi.fn(), bindToolAccount: vi.fn() }))
vi.mock('@/api/aiTool.js', () => api)
vi.mock('@/api/channelCatalog.js', () => ({ default: {
  catalog: vi.fn().mockResolvedValue({ channels: [], accounts: [] }),
  managedAccounts: vi.fn().mockResolvedValue({ list: [] }),
  setTenantEnabled: vi.fn(),
  updateManagedAccount: vi.fn(),
  setAccountStatus: vi.fn()
} }))
vi.mock('@/api/channelAccount.js', () => ({ default: {
  listChannels: vi.fn().mockResolvedValue([]),
  listAccounts: vi.fn().mockResolvedValue([]),
  accountOptions: vi.fn().mockResolvedValue([])
} }))
import ToolList from '@/views/aiAgent/ToolManagement/ToolList.vue'

const rows = (count) => Array.from({ length: count }, (_, i) => ({
  tool_name: `tool-${i + 1}`, category: i % 2 ? 'customer' : 'business',
  is_enabled: true, config: {}, bound_accounts: [], display_order: i,
}))
const deferred = () => {
  let resolve, reject
  const promise = new Promise((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
let wrapper, database
const serve = (params = {}) => {
  const filtered = database.filter((r) => (!params.category || r.category === params.category)
    && (typeof params.enabled !== 'boolean' || r.is_enabled === params.enabled))
  const page = params.page || 1, size = params.page_size || 20
  return { list: structuredClone(filtered.slice((page - 1) * size, page * size)), total: filtered.length }
}
const query = () => api.listTools.mock.calls.at(-1)[0]
const visibleNames = () => wrapper.findAll('.tool-name').map((r) => r.text())
const pagination = () => wrapper.findComponent({ name: 'ElPagination' })
async function open(count = 21) {
  database = rows(count)
  wrapper = mount(ToolList, {
    attachTo: document.body,
    global: { plugins: [createPinia(), ElementPlus, createI18n({ legacy: false, locale: 'zh', messages: { zh }, missingWarn: false, fallbackWarn: false })] },
  })
  await settle(wrapper)
}
async function button(label) {
  const target = wrapper.findAll('button').find((b) => b.text() === label)
  expect(target, label).toBeTruthy()
  await target.trigger('click')
  await settle(wrapper)
}
async function next() {
  expect(wrapper.find('.btn-next').exists(), '分页入口必须存在').toBe(true)
  await wrapper.find('.btn-next').trigger('click')
  await settle(wrapper)
}
async function status(value) {
  wrapper.findAllComponents({ name: 'ElSelect' })[1].vm.$emit('update:modelValue', value)
  wrapper.findAllComponents({ name: 'ElSelect' })[1].vm.$emit('change', value)
  await settle(wrapper)
}
beforeEach(() => {
  api.listTools.mockReset().mockImplementation(async (p) => serve(p))
  api.updateToolStatus.mockReset().mockImplementation(async (name, enabled) => { database.find((r) => r.tool_name === name).is_enabled = enabled })
  api.bindToolAccount.mockReset().mockResolvedValue({})
  vi.spyOn(ElMessage, 'error').mockImplementation(() => {})
  vi.spyOn(ElMessage, 'success').mockImplementation(() => {})
})
afterEach(() => { wrapper?.unmount(); wrapper = null; vi.restoreAllMocks(); document.body.replaceChildren() })

describe('工具列表服务端分页', () => {
  it('21 条：使用服务端总数并到达第二页管理工具', async () => {
    await open()
    expect(query()).toEqual({ page: 1, page_size: 20 })
    expect(wrapper.find('.stat-card .stat-value').text()).toBe('21')
    expect(wrapper.text()).toContain('本页已启用')
    await next()
    expect(query()).toEqual({ page: 2, page_size: 20 })
    expect(visibleNames()).toEqual(['tool-21'])
    await wrapper.find('.el-table__body .el-switch').trigger('click')
    await settle(wrapper)
    expect(api.updateToolStatus).toHaveBeenCalledWith('tool-21', false)
    expect(query().page).toBe(2)
    expect(wrapper.find('.stat-warning .stat-value').text()).toBe('1')
  })
  it('101 条：六页覆盖每个工具，不以首页长度冒充总数', async () => {
    await open(101)
    const names = [...visibleNames()]
    for (let page = 2; page <= 6; page++) { await next(); names.push(...visibleNames()) }
    expect(new Set(names).size).toBe(101)
    expect(names).toEqual(database.map((r) => r.tool_name))
    expect(pagination().props('total')).toBe(101)
  })
  it('筛选回第一页、false 保留、刷新保持页码、重置清除筛选', async () => {
    await open(101)
    await next()
    await button('刷新')
    expect(query().page).toBe(2)
    await status(false)
    expect(query()).toEqual({ page: 1, page_size: 20, enabled: false })
    expect(visibleNames()).toEqual([])
    await button('重置')
    expect(query()).toEqual({ page: 1, page_size: 20 })
  })
  it('改变每页条数回到第一页并传递新大小', async () => {
    await open(101)
    await next()
    expect(pagination().exists()).toBe(true)
    pagination().vm.$emit('size-change', 100)
    await settle(wrapper)
    expect(query()).toEqual({ page: 1, page_size: 100 })
    expect(visibleNames()).toHaveLength(100)
    await next()
    expect(visibleNames()).toEqual(['tool-101'])
  })
  it('并发删除末页后刷新退回最后有效页', async () => {
    await open()
    await next()
    database.pop()
    await button('刷新')
    expect(query().page).toBe(1)
    expect(visibleNames()).toHaveLength(20)
    expect(pagination().props('total')).toBe(20)
  })
  it('筛选下禁用末页最后一项后重新定位且计数真实', async () => {
    await open()
    await status(true)
    await next()
    await wrapper.find('.el-table__body .el-switch').trigger('click')
    await settle(wrapper)
    expect(query()).toEqual({ page: 1, page_size: 20, enabled: true })
    expect(visibleNames()).toHaveLength(20)
    expect(pagination().props('total')).toBe(20)
  })
  it('读取失败明确显示并清除可操作旧行，刷新可恢复', async () => {
    await open()
    api.listTools.mockRejectedValueOnce(new Error('模拟读取失败'))
    await button('刷新')
    expect(visibleNames()).toEqual([])
    expect(wrapper.find('[role="alert"]').text()).toContain('模拟读取失败')
    await button('刷新')
    expect(visibleNames()).toHaveLength(20)
    expect(wrapper.find('[role="alert"]').exists()).toBe(false)
  })
  it.each(['success', 'failure'])('过时 %s 不覆盖新筛选和 loading', async (outcome) => {
    await open()
    const old = deferred()
    api.listTools.mockImplementationOnce(() => old.promise)
    const refresh = wrapper.findAll('button').find((b) => b.text() === '刷新')
    await refresh.trigger('click')
    await status(false)
    expect(visibleNames()).toEqual([])
    if (outcome === 'success') old.resolve({ list: rows(20), total: 999 })
    else old.reject(new Error('旧错误'))
    await settle(wrapper)
    expect(visibleNames()).toEqual([])
    expect(pagination().props('total')).toBe(0)
    expect(ElMessage.error).not.toHaveBeenCalled()
  })
  it('保存失败恢复开关且不伪报成功', async () => {
    await open()
    api.updateToolStatus.mockRejectedValueOnce(new Error('模拟保存拒绝'))
    await wrapper.find('.el-table__body .el-switch').trigger('click')
    await settle(wrapper)
    expect(wrapper.find('.el-table__body [role="switch"]').attributes('aria-checked')).toBe('true')
    expect(ElMessage.success).not.toHaveBeenCalled()
    expect(ElMessage.error).toHaveBeenCalled()
  })
  it('旧请求先结束不能提前解除新请求的 loading', async () => {
    await open()
    const old = deferred(), latest = deferred()
    api.listTools.mockImplementationOnce(() => old.promise).mockImplementationOnce(() => latest.promise)
    await wrapper.findAll('button').find((b) => b.text() === '刷新').trigger('click')
    await status(false)
    old.resolve({ list: rows(20), total: 21 })
    await settle(wrapper)
    expect(pagination().props('disabled')).toBe(true)
    expect(visibleNames()).toEqual([])
    latest.resolve({ list: [], total: 0 })
    await settle(wrapper)
    expect(pagination().props('disabled')).toBe(false)
  })
  it('分类变化回第一页；清空状态不发送空字符串筛选', async () => {
    await open(101)
    await next()
    const category = wrapper.findAllComponents({ name: 'ElSelect' })[0]
    category.vm.$emit('update:modelValue', 'customer')
    category.vm.$emit('change', 'customer')
    await settle(wrapper)
    expect(query()).toEqual({ page: 1, page_size: 20, category: 'customer' })
    expect(pagination().props('total')).toBe(50)
    await status('')
    expect(query()).toEqual({ page: 1, page_size: 20, category: 'customer' })
  })
  it.each([undefined, -1, '21'])('无效 total=%s 明确报读取错误', async (total) => {
    api.listTools.mockResolvedValue({ list: rows(20), total })
    await open()
    expect(visibleNames()).toEqual([])
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
  })
  it('服务端持续返回空页但总数非零时有界失败，不伪装无数据', async () => {
    api.listTools.mockResolvedValue({ list: [], total: 21 })
    await open()
    expect(api.listTools.mock.calls.length).toBeLessThanOrEqual(3)
    expect(wrapper.find('[role="alert"]').exists()).toBe(true)
  })
})

describe('工具列表统计卡（UWA-24）', () => {
  const statValues = () => [...wrapper.findAll('.stat-value').map((n) => n.text().trim())]

  it('空列表：分类数按真实数据计算为 0，不再显示写死的「工具分类 5」', async () => {
    await open(0)
    const values = statValues()
    expect(values).toEqual(['0', '0', '0', '0'])
    expect(wrapper.text()).not.toContain('工具分类 5')
  })

  it('有数据：分类数等于本页工具的去重分类数', async () => {
    await open(21)
    // 21 条按 i%2 分为 business/customer 两类；首页 20 条同样两类
    expect(statValues()).toEqual(['21', '20', '0', '2'])
  })
})
