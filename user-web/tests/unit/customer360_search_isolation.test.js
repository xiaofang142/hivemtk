import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'
import { createPinia } from 'pinia'
import { settle } from './settle-helper'
const api = vi.hoisted(() => ({ list: vi.fn(), detail: vi.fn(), events: vi.fn(), messages: vi.fn(), orders: vi.fn(), query: {} }))
vi.mock('@/api/customer360.js', () => ({ getCustomerList: api.list, getCustomerDetail: api.detail, getCustomerEvents: api.events, getCustomerMessages: api.messages, addCustomerTag: vi.fn(), removeCustomerTag: vi.fn() }))
vi.mock('@/api/integration.js', () => ({ getExternalOrdersByCustomer: api.orders }))
vi.mock('@/api/customerSession.js', () => ({ createSession: vi.fn(), sendMessage: vi.fn() }))
vi.mock('vue-router', () => ({ useRoute: () => ({ query: api.query }) }))
import Customer from '@/views/customer360/List.vue'
import i18n from '@/i18n'
let wrapper
const customer = (id, name = `客户${id}`) => [String(id), { basic_info: { user_id: String(id), user_name: name, user_phone: '123', user_email: `${id}@example.test` } }]
const response = (entries, total) => ({ list: Object.fromEntries(entries), total })
const searchInput = () => wrapper.get('input[placeholder="搜索客户(姓名/手机/邮箱)"]')
beforeEach(() => {
  vi.clearAllMocks()
  api.query = {}
  api.list.mockImplementation(({ page, keyword }) => Promise.resolve(keyword
    ? response([customer('remote', '数据库命中客户')], 1)
    : response(page === 1 ? Array.from({ length: 20 }, (_, i) => customer(i + 1)) : [customer(21)], 21)))
  api.detail.mockImplementation(id => Promise.resolve({ basic_info: { user_id: id, user_name: `详情${id}`, user_phone: id }, tags: [] }))
  api.events.mockResolvedValue({ events: [] }); api.messages.mockResolvedValue({ message_history: [] }); api.orders.mockResolvedValue({ list: [] })
})
afterEach(() => { wrapper?.unmount(); ElMessage.closeAll() })
async function page() { wrapper = mount(Customer, { global: { plugins: [createPinia(), ElementPlus, i18n] } }); await settle(wrapper) }
describe('客户 360 服务端搜索分页和切换', () => {
  it('使用后端唯一客户总数，翻页请求服务端并直接展示当前页', async () => {
    await page()
    expect(api.list).toHaveBeenLastCalledWith({ page: 1, page_size: 20, keyword: '' })
    expect(wrapper.findAll('.el-table__row')).toHaveLength(20)
    expect(wrapper.get('.el-pagination__total').text()).toContain('21')
    await wrapper.get('.btn-next').trigger('click')
    await settle(wrapper)
    expect(api.list).toHaveBeenLastCalledWith({ page: 2, page_size: 20, keyword: '' })
    expect(wrapper.findAll('.el-table__row')).toHaveLength(1)
    expect(wrapper.text()).toContain('客户21')
    expect(wrapper.text()).not.toContain('客户20')
  })
  it('第二页搜索回首页，跨原始会话页命中仍展示且不进行二次本地过滤', async () => {
    await page()
    await wrapper.get('.btn-next').trigger('click')
    await settle(wrapper)
    await searchInput().setValue('  name@example.test  ')
    await settle(wrapper)
    expect(api.list).toHaveBeenLastCalledWith({ page: 1, page_size: 20, keyword: 'name@example.test' })
    expect(wrapper.findAll('.el-table__row')).toHaveLength(1)
    expect(wrapper.text()).toContain('数据库命中客户')
    expect(wrapper.find('.el-pagination').exists()).toBe(false)
    expect(api.list).toHaveBeenCalledTimes(3)
  })
  it('清空搜索重新获取第一页且恢复服务器总数', async () => {
    await page()
    await searchInput().setValue('13800138')
    await settle(wrapper)
    await searchInput().setValue('')
    await settle(wrapper)
    expect(api.list).toHaveBeenLastCalledWith({ page: 1, page_size: 20, keyword: '' })
    expect(wrapper.findAll('.el-table__row')).toHaveLength(20)
    expect(wrapper.get('.el-pagination__total').text()).toContain('21')
  })
  it('旧搜索迟到不能覆盖新搜索结果与总数', async () => {
    await page()
    let resolveOld
    api.list.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
      .mockResolvedValueOnce(response([customer('new', '新搜索客户')], 41))
    await searchInput().setValue('旧搜索')
    await searchInput().setValue('新搜索')
    await settle(wrapper)
    resolveOld(response([customer('old', '旧搜索客户')], 99))
    await settle(wrapper)
    expect(wrapper.text()).toContain('新搜索客户')
    expect(wrapper.text()).not.toContain('旧搜索客户')
    expect(wrapper.get('.el-pagination__total').text()).toContain('41')
  })
  it('旧搜索失败不能清空新结果，新搜索失败不能残留上一页', async () => {
    await page()
    let rejectOld
    api.list.mockReturnValueOnce(new Promise((_, reject) => { rejectOld = reject }))
      .mockResolvedValueOnce(response([customer('new', '新搜索客户')], 1))
    await searchInput().setValue('旧搜索')
    await searchInput().setValue('新搜索')
    await settle(wrapper)
    rejectOld(new Error('controlled old failure'))
    await settle(wrapper)
    expect(wrapper.text()).toContain('新搜索客户')
    api.list.mockRejectedValueOnce(new Error('controlled current failure'))
    await searchInput().setValue('失败搜索')
    await settle(wrapper)
    expect(wrapper.findAll('.el-table__row')).toHaveLength(0)
    expect(wrapper.find('.el-pagination').exists()).toBe(false)
  })
  it('零匹配显示空列表和零分页', async () => {
    await page()
    api.list.mockResolvedValueOnce(response([], 0))
    await searchInput().setValue('不存在')
    await settle(wrapper)
    expect(wrapper.findAll('.el-table__row')).toHaveLength(0)
    expect(wrapper.text()).toContain('暂无客户')
    expect(wrapper.find('.el-pagination').exists()).toBe(false)
  })
  it('深链可加载当前列表页外客户，列表失败也不阻断详情', async () => {
    api.query = { id: 'deep-link' }
    api.list.mockRejectedValueOnce(new Error('controlled list failure'))
    await page()
    expect(api.detail).toHaveBeenCalledWith('deep-link')
    expect(wrapper.get('.customer-header').text()).toContain('详情deep-link')
  })
  it('旧客户详情迟到不能覆盖当前客户', async () => {
    await page()
    let resolveOld
    api.detail.mockReturnValueOnce(new Promise(resolve => { resolveOld = resolve }))
    const oldSelection = wrapper.vm.selectCustomer({ id: '1' })
    await wrapper.vm.selectCustomer({ id: '2' })
    resolveOld({ basic_info: { user_id: '1', user_name: '旧详情' } })
    await oldSelection
    await settle(wrapper)
    expect(wrapper.get('.customer-header').text()).toContain('详情2')
    expect(wrapper.text()).not.toContain('旧详情')
  })
  it('旧客户订单迟到不能覆盖当前客户', async () => {
    await page()
    let resolveOld
    api.orders.mockReturnValueOnce(new Promise(r => { resolveOld = r })).mockResolvedValueOnce({ list: [{ order_id: 'current-order', total_amount: 100 }] })
    await wrapper.vm.selectCustomer({ id: '1' })
    await wrapper.vm.selectCustomer({ id: '2' })
    wrapper.vm.activeTab = 'orders'
    resolveOld({ list: [{ order_id: 'old-order' }] })
    await settle(wrapper)
    expect(wrapper.text()).toContain('current-order')
    expect(wrapper.text()).not.toContain('old-order')
  })
  it('主资料失败不能显示伪造客户详情', async () => {
    await page()
    api.detail.mockRejectedValue(new Error('controlled failure'))
    await wrapper.vm.selectCustomer({ id: '1' })
    await settle(wrapper)
    expect(wrapper.find('.customer-header').exists()).toBe(false)
    expect(wrapper.text()).toContain('请从左侧选择客户')
  })
})
