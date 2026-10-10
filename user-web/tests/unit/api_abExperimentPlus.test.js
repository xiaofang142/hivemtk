/**
 * AB 高级分析 API 单元测试（I17 前端接线的契约锁）。
 *
 * 钉两件事：五条端点的路径/方法/参数原样透传；
 * 导出集恰为这五个——getFeatureEvalLog 曾是零消费死导出（I17 删除），
 * 不得回魂（后端 feature-flags eval-log 端点仍在，无页面承接时前端不预写）。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'

const { http } = vi.hoisted(() => ({
  http: {
    get: vi.fn(),
    post: vi.fn(),
  },
}))
vi.mock('@/utils/request', () => ({ http }))

import * as api from '@/api/abExperimentPlus.js'

describe('AB 高级分析 API', () => {
  beforeEach(() => {
    http.get.mockReset()
    http.post.mockReset()
    http.get.mockResolvedValue({ code: 0, data: {} })
    http.post.mockResolvedValue({ code: 0, data: {} })
  })

  it('导出集恰为五个高级分析函数（死导出不回魂）', () => {
    expect(Object.keys(api).sort()).toEqual([
      'bayesianTest',
      'getExperimentDiagnostics',
      'getExperimentWithCUPED',
      'getExperimentWithStats',
      'sequentialTest',
    ])
  })

  it('统计检验走 GET stats，method 缺省 frequentist', async () => {
    await api.getExperimentWithStats(7)
    expect(http.get).toHaveBeenCalledWith('/api/ab-experiments/7/stats', { method: 'frequentist' })
  })

  it('统计检验可切 bayesian 方法', async () => {
    await api.getExperimentWithStats(7, 'bayesian')
    expect(http.get).toHaveBeenCalledWith('/api/ab-experiments/7/stats', { method: 'bayesian' })
  })

  it('诊断与 CUPED 走 GET 各自段', async () => {
    await api.getExperimentDiagnostics(9)
    expect(http.get).toHaveBeenCalledWith('/api/ab-experiments/9/diagnostics')
    await api.getExperimentWithCUPED(9)
    expect(http.get).toHaveBeenCalledWith('/api/ab-experiments/9/cuped')
  })

  it('序贯检验走 POST 且 alpha 缺省 0.05 透传', async () => {
    await api.sequentialTest(3)
    expect(http.post).toHaveBeenCalledWith('/api/ab-experiments/3/sequential-test', { alpha: 0.05 })
    await api.sequentialTest(3, 0.01)
    expect(http.post).toHaveBeenCalledWith('/api/ab-experiments/3/sequential-test', { alpha: 0.01 })
  })

  it('贝叶斯检验走 POST 空体', async () => {
    await api.bayesianTest(5)
    expect(http.post).toHaveBeenCalledWith('/api/ab-experiments/5/bayesian-test', {})
  })
})
