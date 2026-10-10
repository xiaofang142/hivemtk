import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'

// CSAT 看板的渲染腿：证明那四张卡、两张图和差评表读的真是 /api/csat/* 的字段，
// 而不是模板自己造的数。
//
// 这一页原先同时踩了三个坑，每个都只在"渲染出来是什么字符串"这一层才看得见：
//   1) 页面把接口当成"直接返回数组/驼峰字段"来取（stats.positiveRate、neg 直接进 :data），
//      而 /api/csat/* 给的是 {list,total} 信封 + snake_case ⇒ el-table 收到对象
//      （rows is not iterable）、趋势图 data.map 抛错、四张卡恒空；
//   2) 统计卡的副标题写着"本月"，请求却没带 window ⇒ 后端按全量算，文案与数不是一回事；
//   3) 差评列表只有调查单本身的列（坐席/客户两列永远是空的），深链又写成 /customerSession/list
//      （路由是 hash 模式，不带 # 打的是兜底页）。
// 所以下面每条断言都落在渲染文本或 setOption 的实参上，不看源码里写了什么注释。

const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }))

vi.mock('@/utils/request', () => ({ http: http }))

// echarts 在 jsdom 里 init 不出 canvas，换成只记实参的桩：
// 这张桩收到的 option 就是"用户在这两块画布上看见的那些点"。
const chart = vi.hoisted(() => ({ setOption: vi.fn() }))
vi.mock('@/utils/echarts', () => ({
  safeInit: () => chart,
  safeDispose: vi.fn(),
  echarts: {}
}))

import CsatDashboard from '@/views/customerService/CsatDashboard.vue'

// 现测于 2026-10-09 的 /api/csat/* 响应（已剥掉 request.js 拦截器返回的 data.data 外层）
const STATS = {
  avg_score: 4.5,
  positive_rate: 66.67,
  responded: 3,
  negative_count: 1,
  threshold: 3,
  total: 8,
  window: '本月',
  distribution: [
    { score: 5, count: 2 },
    { score: 4, count: 1 },
    { score: 1, count: 1 }
  ]
}
const TREND = {
  list: [
    { date: '2026-10-07', avg_score: 4.2, count: 5 },
    { date: '2026-10-08', avg_score: 3, count: 2 }
  ],
  total: 2
}
const NEGATIVE = {
  list: [
    {
      session_id: 'sess-77',
      agent_name: '坐席乙',
      user_name: '客户丙',
      score: 1,
      comment: '响应太慢',
      responded_at: '2026-10-08T10:00:00Z'
    }
  ],
  total: 1,
  threshold: 2
}

function stubHappy() {
  http.get.mockImplementation((url) => {
    if (url === '/api/csat/stats') return Promise.resolve(STATS)
    if (url === '/api/csat/trend') return Promise.resolve(TREND)
    if (url === '/api/csat/negative') return Promise.resolve(NEGATIVE)
    return Promise.reject(new Error(`不该再请求 ${url}`))
  })
}

async function mountPage() {
  const wrapper = mount(CsatDashboard, { attachTo: document.body, global: { plugins: [ElementPlus] } })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  stubHappy()
})

describe('统计卡：读接口字段，不读模板造的数', () => {
  it('均分/好评率/总评分数/差评数分别来自 avg_score、positive_rate、responded、negative_count', async () => {
    const wrapper = await mountPage()
    const values = wrapper.findAll('.stat-value').map((n) => n.text().trim())
    expect(values).toEqual(['4.5', '66.67%', '3', '1'])
    wrapper.unmount()
  })

  it('请求 stats 时带上 window=month，副标题的「本月」才有依据', async () => {
    const wrapper = await mountPage()
    const statsCall = http.get.mock.calls.find(([url]) => url === '/api/csat/stats')
    expect(statsCall[1]).toEqual({ window: 'month' })
    // 窗口文案取后端回写的 window，而不是页面自己写死的"本月"
    expect(wrapper.findAll('.stat-sub')[2].text()).toBe('本月')
    wrapper.unmount()
  })

  it('差评阈值只有一个事实源：列表接口回的 threshold（模板 low_threshold）', async () => {
    const wrapper = await mountPage()
    // STATS.threshold=3 而 NEGATIVE.threshold=2，页面必须取后者
    expect(wrapper.text()).toContain('≤2 星')
    wrapper.unmount()
  })
})

describe('图表：喂给画布的是接口里的数值序列', () => {
  it('趋势图 x 轴用 date、曲线用 avg_score（不是 undefined）', async () => {
    const wrapper = await mountPage()
    const trend = chart.setOption.mock.calls[0][0]
    expect(trend.xAxis.data).toEqual(['2026-10-07', '2026-10-08'])
    expect(trend.series[0].data).toEqual([4.2, 3])
    wrapper.unmount()
  })

  it('分布饼图按 score/count 拼「N星」', async () => {
    const wrapper = await mountPage()
    const dist = chart.setOption.mock.calls[1][0]
    expect(dist.series[0].data).toEqual([
      { name: '5星', value: 2 },
      { name: '4星', value: 1 },
      { name: '1星', value: 1 }
    ])
    wrapper.unmount()
  })
})

describe('差评表：坐席与客户列来自联表，深链带 #', () => {
  it('渲染出 agent_name、user_name 与格式化后的提交时间', async () => {
    const wrapper = await mountPage()
    const text = wrapper.text()
    expect(text).toContain('坐席乙')
    expect(text).toContain('客户丙')
    expect(text).toContain('响应太慢')
    // 时间列渲染的是本地化串，不是原始 ISO 串，也不是 Invalid Date
    expect(text).not.toContain('2026-10-08T10:00:00Z')
    expect(text).not.toContain('Invalid Date')
    wrapper.unmount()
  })

  it('查看会话开的是 hash 路由深链，并把会话 ID 编码进去', async () => {
    const openSpy = vi.fn()
    const original = window.open
    window.open = openSpy
    try {
      const wrapper = await mountPage()
      await wrapper.find('.negative-card .el-button').trigger('click')
      expect(openSpy).toHaveBeenCalledTimes(1)
      expect(openSpy.mock.calls[0][0]).toBe('/#/customerSession/list?session_id=sess-77')
      wrapper.unmount()
    } finally {
      window.open = original
    }
  })
})

describe('降级：空值与失败不许变成白屏或假数', () => {
  it('接口给 null 列表时（今天还没有差评）页面照常渲染，卡片显示「—」', async () => {
    http.get.mockImplementation((url) => {
      if (url === '/api/csat/stats') {
        return Promise.resolve({ avg_score: 0, distribution: null, responded: 0, total: 0 })
      }
      if (url === '/api/csat/trend') return Promise.resolve({ list: null, total: 0 })
      if (url === '/api/csat/negative') return Promise.resolve({ list: null, threshold: 3, total: 0 })
      return Promise.reject(new Error(`不该再请求 ${url}`))
    })
    const wrapper = await mountPage()
    const values = wrapper.findAll('.stat-value').map((n) => n.text().trim())
    // 没有回收样本时，均分与好评率不能假装是 0/0%
    expect(values[0]).toBe('—')
    expect(values[1]).toBe('—')
    expect(wrapper.text()).not.toContain('CSAT 数据加载失败')
    // 空数组照样喂给画布，曲线是空的而不是 undefined
    expect(chart.setOption.mock.calls[0][0].series[0].data).toEqual([])
    wrapper.unmount()
  })

  it('接口失败时自报错误，而不是留一张看着像"零差评"的空看板', async () => {
    http.get.mockRejectedValue(new Error('502 Bad Gateway'))
    const wrapper = await mountPage()
    expect(wrapper.find('.load-error').exists()).toBe(true)
    expect(wrapper.text()).toContain('CSAT 数据加载失败：502 Bad Gateway')
    wrapper.unmount()
  })
})

describe('夹具自证', () => {
  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('桩没接上时这条腿等于没测：三个端点必须各被请求一次', async () => {
    const wrapper = await mountPage()
    const urls = http.get.mock.calls.map(([url]) => url)
    expect(urls.sort()).toEqual(['/api/csat/negative', '/api/csat/stats', '/api/csat/trend'])
    wrapper.unmount()
  })
})
