import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { settle } from './settle-helper'

// TB-11 刀5 渲染腿：观测页 route 段（北极星三指标 + 三图）读的真是
// /api/observability/dashboard 的 route 字段，而不是模板自己造的数。
// 口径与后端 service/observability_dashboard.go 的 RouteSection 对齐：
//   · north_star 三个比率是 0–100 一位小数（后端 percent 纯函数算好）；
//   · intent_distribution / chain_hit_rate / tool_success_rate 是后端聚合行。

const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn(), put: vi.fn() }))

vi.mock('@/utils/request', () => ({ http }))

// echarts 在 jsdom 里 init 不出 canvas，换成只记实参的桩：
// 桩收到的 option 就是「用户在这三块画布上看见的那些点」。
const chart = vi.hoisted(() => ({ setOption: vi.fn() }))
vi.mock('@/utils/echarts', () => ({
  safeInit: () => chart,
  safeDispose: vi.fn(),
  echarts: {}
}))

import ObservabilityDashboard from '@/views/observabilityDashboard/Index.vue'

const SNAPSHOT = {
  generated_at: '2026-11-10T12:00:00Z',
  window: '7d',
  ai_cost: { calls: 0, success_calls: 0, prompt_tokens: 0, completion_tokens: 0, total_tokens: 0, cost: 0, avg_latency_ms: 0, by_provider: [], by_scenario: [] },
  delivery: { delivered: 0, failed: 0, inflight: 0, rate: 0, by_channel: [] },
  funnel: { stages: [] },
  channel_health: { channels: [], domain_banned: 0, domain_total: 0 },
  route: {
    north_star: {
      intent_accuracy: 87.5,
      resolve_rate: 70,
      handoff_rate: 20,
      corrections: 1,
      intent_recognized: 8,
      sessions: 10,
      resolved: 7,
      handoff: 2
    },
    intent_distribution: [
      { intent_type: 'order_status', count: 7 },
      { intent_type: 'complaint', count: 3 }
    ],
    chain_hit_rate: [
      { chain_name: 'order_status', hits: 6, misses: 2, hit_rate: 75 }
    ],
    tool_success_rate: [
      { tool_name: 'order.lookup', calls: 5, success: 4, success_rate: 80 }
    ]
  }
}

function stubHappy() {
  http.get.mockImplementation((url) => {
    if (url === '/api/observability/dashboard') return Promise.resolve(SNAPSHOT)
    return Promise.reject(new Error(`不该再请求 ${url}`))
  })
}

async function mountPage() {
  const wrapper = mount(ObservabilityDashboard, { attachTo: document.body, global: { plugins: [ElementPlus] } })
  await settle(wrapper)
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  stubHappy()
})

describe('TB-11 刀5：北极星三指标卡读 route.north_star 字段', () => {
  it('意图准确率/解决率/转人工率渲染为百分比，分子分母读数同卡展示', async () => {
    const wrapper = await mountPage()
    const values = wrapper.findAll('.stat-value').map((n) => n.text().trim())
    expect(values).toContain('87.5%')
    expect(values).toContain('70%')
    expect(values).toContain('20%')
    const text = wrapper.text()
    expect(text).toContain('识别 8 · 纠错 1')
    expect(text).toContain('已了结 7/10 会话')
    expect(text).toContain('转人工 2/10 会话')
    wrapper.unmount()
  })

  it('请求带 window=7d（默认窗口口径与后端一致）', async () => {
    await mountPage()
    const call = http.get.mock.calls.find(([url]) => url === '/api/observability/dashboard')
    expect(call[1]).toEqual({ window: '7d' })
  })
})

describe('TB-11 刀5：三图喂给画布的是接口数值序列', () => {
  it('意图分布图 x=intent_type y=count', async () => {
    const wrapper = await mountPage()
    const opt = chart.setOption.mock.calls[0][0]
    expect(opt.xAxis.data).toEqual(['order_status', 'complaint'])
    expect(opt.series[0].data).toEqual([7, 3])
    wrapper.unmount()
  })

  it('链命中率图命中/跳过堆叠且按 chain_name 分组', async () => {
    const wrapper = await mountPage()
    const opt = chart.setOption.mock.calls[1][0]
    expect(opt.xAxis.data).toEqual(['order_status'])
    expect(opt.series[0].name).toBe('命中')
    expect(opt.series[0].stack).toBe('chain')
    expect(opt.series[0].data).toEqual([6])
    expect(opt.series[1].name).toBe('跳过')
    expect(opt.series[1].data).toEqual([2])
    wrapper.unmount()
  })

  it('工具成功率图 y 轴固定 0–100（百分比口径）', async () => {
    const wrapper = await mountPage()
    const opt = chart.setOption.mock.calls[2][0]
    expect(opt.xAxis.data).toEqual(['order.lookup'])
    expect(opt.series[0].data).toEqual([80])
    expect(opt.yAxis.min).toBe(0)
    expect(opt.yAxis.max).toBe(100)
    wrapper.unmount()
  })

  it('空态渲染「暂无」文案，三块画布都不画图', async () => {
    http.get.mockImplementation(() => Promise.resolve({
      ...SNAPSHOT,
      route: {
        north_star: SNAPSHOT.route.north_star,
        intent_distribution: [],
        chain_hit_rate: [],
        tool_success_rate: []
      }
    }))
    const wrapper = await mountPage()
    const text = wrapper.text()
    expect(text).toContain('窗口内无意图识别记录')
    expect(text).toContain('窗口内无绑定链调用')
    expect(text).toContain('窗口内无工具调用')
    expect(chart.setOption).not.toHaveBeenCalled()
    wrapper.unmount()
  })
})
