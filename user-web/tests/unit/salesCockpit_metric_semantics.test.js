import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import i18n from '@/i18n'
import zh from '@/i18n/locales/navigationContent.zh.json'
import en from '@/i18n/locales/navigationContent.en.json'
import { settle } from './settle-helper'

const api = vi.hoisted(() => ({ cockpit: vi.fn(), rates: vi.fn() }))
vi.mock('@/api/cockpit', () => ({ getSalesCockpit: api.cockpit }))
vi.mock('@/api/ltcRates', () => ({ default: { getRates: api.rates } }))
vi.mock('@/utils/echarts', () => ({
  safeInit: () => ({ setOption: vi.fn(), dispose: vi.fn() })
}))
import SalesCockpit from '@/views/SalesCockpit/Index.vue'

let wrapper
beforeEach(() => {
  vi.clearAllMocks()
  i18n.global.mergeLocaleMessage('zh', zh)
  i18n.global.mergeLocaleMessage('en', en)
  i18n.global.locale.value = 'zh'
  api.cockpit.mockResolvedValue({
    llmRoutes: [{ scenario: 'reply', provider: 'test-provider', calls: 120 }],
    channelHealth: [
      { platform: 'test-channel', status: 'offline', cnt: 3 },
      { platform: 'test-channel', status: 'online', cnt: 120 },
      { platform: 'test-channel', status: 2, cnt: 4 },
      { platform: 'other', status: 'new-status', cnt: 1 }
    ]
  })
  api.rates.mockResolvedValue({
    closure: { rate: 0.5, numerator: 1, denominator: 2 },
    collection: { rate: 0.75, numerator: 30, denominator: 40 },
    overdue: { rate: 0.25, numerator: 1, denominator: 4 }
  })
})
afterEach(() => {
  wrapper?.unmount()
  vi.restoreAllMocks()
})

async function page() {
  wrapper = mount(SalesCockpit, { global: { plugins: [ElementPlus, i18n] } })
  await settle(wrapper)
}

describe('销售驾驶舱指标口径', () => {
  it('120 次调用展示今日次数，不生成 QPS、限额和零错误率列', async () => {
    await page()
    const headers = wrapper.findAll('.el-table__header-wrapper th').map(cell => cell.text())
    expect(headers).toContain('今日调用次数')
    expect(headers).toContain('账号数量')
    expect(headers.join(' ')).not.toMatch(/QPS|错误率|限额|已用/)
    const routes = wrapper.findAll('.el-table')[0].findAll('.el-table__body-wrapper tbody tr')
    expect(routes[0].text()).toContain('120')
    expect(wrapper.findAll('.el-table')[0].text()).not.toContain('0.0%')
    expect(wrapper.findAll('.el-table')[1].text()).not.toContain('0.0%')
  })

  it('账号数量与真实状态独立，离线/异常不显示正常，未知值可见', async () => {
    await page()
    const rows = wrapper.findAll('.el-table')[1].findAll('.el-table__body-wrapper tbody tr')
    expect(rows[0].text()).toContain('离线')
    expect(rows[0].text()).toContain('3')
    expect(rows[0].find('.el-tag--success').exists()).toBe(false)
    expect(rows[1].text()).toContain('在线')
    expect(rows[1].text()).toContain('120')
    expect(rows[2].text()).toContain('异常')
    expect(rows[3].text()).toContain('new-status')
    expect(rows[3].find('.el-tag--success').exists()).toBe(false)
  })

  it('保留真实 LTC 三率及分子分母', async () => {
    await page()
    const text = wrapper.text()
    expect(text).toContain('50.0%')
    expect(text).toContain('75.0%')
    expect(text).toContain('25.0%')
    expect(text).toContain('已闭环 1 / 2')
    expect(text).toContain('已回款 30 / 40 元')
  })

  it('汇总请求失败明确提示，并能重试，不把未加载的计数当成零', async () => {
    vi.spyOn(console, 'error').mockImplementation(() => {})
    api.cockpit.mockRejectedValueOnce(new Error('offline'))
    await page()
    expect(wrapper.find('.el-alert--error').exists()).toBe(true)
    expect(wrapper.findAll('.big-num')[0].text()).toBe('—')
    await wrapper.findAll('button').find(button => button.text() === '重试').trigger('click')
    await settle(wrapper)
    expect(wrapper.find('.el-alert--error').exists()).toBe(false)
    expect(wrapper.findAll('.el-table')[0].text()).toContain('120')
  })
})
