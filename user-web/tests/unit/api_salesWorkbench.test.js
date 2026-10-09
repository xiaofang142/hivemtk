/**
 * 销售工作台 API 单元测试（三读口零消费闭环的契约锁）。
 *
 * 钉三件事：三条路径与 query 参数形状、overview 必带 sales_id（缺失后端 400，
 * 不做可选默认）、days 原样透传（7/30/90 由页面给，API 层不改写）。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'

const { request, http } = vi.hoisted(() => {
  const request = vi.fn()
  const http = {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    patch: vi.fn(),
    delete: vi.fn()
  }
  return { request, http }
})
vi.mock('@/utils/request', () => ({ default: request, http }))

import { salesWorkbenchApi } from '@/api/salesWorkbench.js'

describe('销售工作台 API', () => {
  beforeEach(() => {
    request.mockReset()
    http.get.mockReset()
    http.get.mockResolvedValue({ code: 0, data: {} })
  })

  it('overview 走 /api/sales-workbench/overview 并带 sales_id', async () => {
    await salesWorkbenchApi.overview('u_42')
    expect(http.get).toHaveBeenCalledWith('/api/sales-workbench/overview', { sales_id: 'u_42' })
  })

  it('teamDashboard 走 /api/sales-workbench/team-dashboard 并透传 days', async () => {
    await salesWorkbenchApi.teamDashboard(7)
    expect(http.get).toHaveBeenCalledWith('/api/sales-workbench/team-dashboard', { days: 7 })
  })

  it('champion 走 /api/sales-workbench/champion 并透传 days', async () => {
    await salesWorkbenchApi.champion(30)
    expect(http.get).toHaveBeenCalledWith('/api/sales-workbench/champion', { days: 30 })
  })

  it('三条端点的返回值原样 resolve（拦截器已解包 data.data，不二次包裹）', async () => {
    const payload = { todos: [], today: null, month: null }
    http.get.mockResolvedValue(payload)
    await expect(salesWorkbenchApi.overview('u_1')).resolves.toEqual(payload)
  })

  it('errors 原样 reject（err.status 给页面区分 503 未装配与 400 参数）', async () => {
    const err = new Error('服务未装配')
    err.status = 503
    http.get.mockRejectedValue(err)
    await expect(salesWorkbenchApi.teamDashboard(30)).rejects.toMatchObject({ status: 503 })
  })
})
