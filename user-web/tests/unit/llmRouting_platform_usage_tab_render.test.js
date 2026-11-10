/**
 * 平台垫资用量明细 Tab 渲染测试（PLT-01 刀2）
 *
 * List.vue 的「平台用量」Tab 是 GET /api/llm/platform-usage 的唯一消费方。
 * 钉三件事：
 *   1. Tab 存在且分页拉取带 page/page_size；
 *   2. 行渲染成本三列（整数分 → 元，两位小数）——读侧只做展示换算不重算；
 *   3. 空态可见（未使用平台模型时不白屏）。
 *
 * api 全 mock（端点归属在 api 模块层），只钉渲染与交互。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'

const api = vi.hoisted(() => ({
  getModelList: vi.fn(),
  getSceneRouting: vi.fn(),
  getFallbackStrategy: vi.fn(),
  getCostStats: vi.fn(),
  getDailyUsage: vi.fn(),
  getAuditHistory: vi.fn(),
  getModelTypeStats: vi.fn(),
  getHealth: vi.fn(),
  getEgressAlerts: vi.fn(),
  getStats: vi.fn(),
  getUsage: vi.fn(),
  getCostStatsLegacy: vi.fn(),
  getPlatformUsage: vi.fn()
}))

vi.mock('@/api/llmRouting.js', () => ({
  LlmRoutingApi: {
    getModelList: api.getModelList,
    getSceneRouting: api.getSceneRouting,
    getFallbackStrategy: api.getFallbackStrategy,
    getCostStats: api.getCostStats,
    getDailyUsage: api.getDailyUsage,
    getAuditHistory: api.getAuditHistory,
    getModelTypeStats: api.getModelTypeStats,
    getHealth: api.getHealth,
    getEgressAlerts: api.getEgressAlerts,
    getStats: api.getStats,
    getUsage: api.getUsage,
    getPlatformUsage: api.getPlatformUsage
  }
}))

import List from '@/views/llmRouting/List.vue'
import { settle } from './settle-helper'

const mounted = []

beforeEach(() => {
  api.getModelList.mockReset()
  api.getModelList.mockResolvedValue([])
  api.getSceneRouting.mockReset()
  api.getSceneRouting.mockResolvedValue([])
  api.getFallbackStrategy.mockReset()
  api.getFallbackStrategy.mockResolvedValue({})
  api.getCostStats.mockReset()
  api.getCostStats.mockResolvedValue({})
  api.getDailyUsage.mockReset()
  api.getDailyUsage.mockResolvedValue([])
  api.getAuditHistory.mockReset()
  api.getAuditHistory.mockResolvedValue([])
  api.getModelTypeStats.mockReset()
  api.getModelTypeStats.mockResolvedValue({ by_provider_type: [] })
  api.getHealth.mockReset()
  api.getHealth.mockResolvedValue({})
  api.getEgressAlerts.mockReset()
  api.getEgressAlerts.mockResolvedValue([])
  api.getStats.mockReset()
  api.getUsage.mockReset()
  api.getPlatformUsage.mockReset()
})

afterEach(() => {
  while (mounted.length) {
    try {
      mounted.pop().unmount()
    } catch {
      /* 重复卸载不抛即可 */
    }
  }
  document.body.innerHTML = ''
})

function mountPage() {
  const w = mount(List, { global: { plugins: [ElementPlus] } })
  mounted.push(w)
  return w
}

describe('LLM 路由页 · 平台用量 Tab', () => {
  it('挂载即分页拉取明细（page/page_size 透传），行成本按分→元两位小数渲染', async () => {
    api.getPlatformUsage.mockResolvedValue({
      list: [
        {
          id: 1, provider: 'plat-a', model: 'm1', plan_code: 'plan-x',
          scenario: 'sop_reply', prompt_tokens: 1500, completion_tokens: 200,
          input_cost_cents: 10, output_cost_cents: 14, total_cost_cents: 24,
          occurred_at: '2026-11-11T08:00:00Z'
        },
        {
          id: 2, provider: 'plat-b', model: 'm2', plan_code: 'plan-y',
          scenario: 'intent', prompt_tokens: 1000, completion_tokens: 100,
          input_cost_cents: 0, output_cost_cents: 0, total_cost_cents: 0,
          occurred_at: '2026-11-11T09:00:00Z'
        }
      ],
      total: 2
    })

    const w = mountPage()
    await settle(w)

    expect(api.getPlatformUsage).toHaveBeenCalled()
    const [params] = api.getPlatformUsage.mock.calls[0]
    expect(params).toEqual({ page: 1, page_size: 20 })

    const text = w.text()
    expect(text).toContain('平台用量')
    expect(text).toContain('plat-a')
    expect(text).toContain('plan-x')
    // 整数分 → 元：10 分 = ¥0.10、14 分 = ¥0.14、24 分 = ¥0.24
    expect(text).toContain('¥0.10')
    expect(text).toContain('¥0.14')
    expect(text).toContain('¥0.24')
    // 0 分如实显示 ¥0.00（无价降级格，不编数）
    expect(text).toContain('¥0.00')
  })

  it('空明细渲染空态文案，不白屏', async () => {
    api.getPlatformUsage.mockResolvedValue({ list: [], total: 0 })
    const w = mountPage()
    await settle(w)
    expect(w.text()).toContain('暂无平台用量明细')
  })
})
