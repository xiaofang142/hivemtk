/**
 * 订单草稿 API 单元测试（A1 前端半的契约锁）。
 *
 * 钉三件事：五条业务端点的路径/方法、id 段必须编码、观察端点在 /api/agent 前缀
 * （与业务端点的 /api/manage 前缀不同是刻意的 —— 一个读运行时档位，一个读业务数据）。
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

import { orderDraftApi } from '@/api/orderDraft.js'

describe('订单草稿 API', () => {
  beforeEach(() => {
    request.mockReset()
    http.get.mockReset()
    http.post.mockReset()
    http.patch.mockReset()
    http.get.mockResolvedValue({ code: 0, data: {} })
    http.post.mockResolvedValue({ code: 0, data: {} })
    http.patch.mockResolvedValue({ code: 0, data: {} })
  })

  it('list 缺省走待确认池视图（只带 limit，不硬塞 owner）', async () => {
    await orderDraftApi.list({ limit: 50 })
    expect(http.get).toHaveBeenCalledWith('/api/manage/order-drafts', { limit: 50 })
  })

  it('list 带归属与筛选时原样透传', async () => {
    await orderDraftApi.list({ owner_id: '7', limit: 100, customer_id: 'c1' })
    expect(http.get).toHaveBeenCalledWith('/api/manage/order-drafts', {
      owner_id: '7',
      limit: 100,
      customer_id: 'c1'
    })
  })

  it('get 对 id 段做 URL 编码（含 / ? 的 id 不得改写请求路径）', async () => {
    await orderDraftApi.get('draft/a?b')
    expect(http.get).toHaveBeenCalledWith('/api/manage/order-drafts/draft%2Fa%3Fb')
  })

  it('edit 走 PATCH 且请求体原样是 updates 对象', async () => {
    await orderDraftApi.edit('d1', { unit_price: 9.9, quantity: 3 })
    expect(http.patch).toHaveBeenCalledWith('/api/manage/order-drafts/d1', {
      unit_price: 9.9,
      quantity: 3
    })
  })

  it('confirm 走 POST 且不带请求体（没有要传的字段）', async () => {
    await orderDraftApi.confirm('d1')
    expect(http.post).toHaveBeenCalledWith('/api/manage/order-drafts/d1/confirm')
  })

  it('cancel 必须带 reason（后端对空理由回 400）', async () => {
    await orderDraftApi.cancel('d1', '客户不要了')
    expect(http.post).toHaveBeenCalledWith('/api/manage/order-drafts/d1/cancel', {
      reason: '客户不要了'
    })
  })

  it('stats 在 /api/agent 前缀（观察端点，与业务端点分家）', async () => {
    await orderDraftApi.stats()
    expect(http.get).toHaveBeenCalledWith('/api/agent/order-drafts/stats')
  })
})
