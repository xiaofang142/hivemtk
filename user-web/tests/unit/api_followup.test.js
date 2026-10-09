/**
 * 跟进提醒 API 单元测试（A11 读写口的契约锁）。
 *
 * 钉五件事：五条路径/方法、owner_id 恒带（后端空=全员，前端不能漏）、
 * date/limit 可选不硬塞、complete 的 body 形状、id 段编码。
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

import { followupApi } from '@/api/followup.js'

describe('跟进提醒 API', () => {
  beforeEach(() => {
    request.mockReset()
    http.get.mockReset()
    http.post.mockReset()
    http.get.mockResolvedValue({ code: 0, data: {} })
    http.post.mockResolvedValue({ code: 0, data: {} })
  })

  it('today 带 owner_id，date 缺省不硬塞', async () => {
    await followupApi.today('u1')
    expect(http.get).toHaveBeenCalledWith('/api/followups/today', { owner_id: 'u1' })
  })

  it('today 带 date 时原样透传', async () => {
    await followupApi.today('u1', '2026-10-09')
    expect(http.get).toHaveBeenCalledWith('/api/followups/today', { owner_id: 'u1', date: '2026-10-09' })
  })

  it('pending 带 owner_id，limit 缺省不硬塞', async () => {
    await followupApi.pending('u1')
    expect(http.get).toHaveBeenCalledWith('/api/followups/pending', { owner_id: 'u1' })
  })

  it('overdue 带 owner_id', async () => {
    await followupApi.overdue('u2')
    expect(http.get).toHaveBeenCalledWith('/api/followups/overdue', { owner_id: 'u2' })
  })

  it('complete 打 /complete 并带 result/note body，id 段编码', async () => {
    await followupApi.complete('rem/a1', 'interested', '聊过价格')
    expect(http.post).toHaveBeenCalledWith('/api/followups/rem%2Fa1/complete', {
      result: 'interested',
      note: '聊过价格'
    })
  })

  it('complete 缺省 result/note 发空串（后端缺省 contacted）', async () => {
    await followupApi.complete('rem_1')
    expect(http.post).toHaveBeenCalledWith('/api/followups/rem_1/complete', { result: '', note: '' })
  })

  it('cancel 打 /cancel 且无 body', async () => {
    await followupApi.cancel('rem_1')
    expect(http.post).toHaveBeenCalledWith('/api/followups/rem_1/cancel')
  })

  it('409 已处理原样 reject（err.message 透给页面）', async () => {
    const err = new Error('提醒 rem_1 已处理')
    err.status = 409
    http.post.mockRejectedValue(err)
    await expect(followupApi.complete('rem_1')).rejects.toMatchObject({ status: 409 })
  })
})
