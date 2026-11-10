/**
 * 平台垫资用量明细 API 单元测试（PLT-01 刀2）
 * 钉 GET /api/llm/platform-usage 的调用契约：路径、分页参数透传。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'

const { http } = vi.hoisted(() => ({
  http: {
    get: vi.fn(),
    post: vi.fn(),
    put: vi.fn(),
    delete: vi.fn()
  }
}))
vi.mock('@/utils/request', () => ({ default: vi.fn(), http }))

import { LlmRoutingApi } from '@/api/llmRouting.js'

describe('平台垫资用量明细 API', () => {
  beforeEach(() => {
    http.get.mockReset()
    http.get.mockResolvedValue({ code: 'SUCCESS', data: { list: [], total: 0 } })
  })

  it('getPlatformUsage 请求 /api/llm/platform-usage 并透传分页参数', async () => {
    await LlmRoutingApi.getPlatformUsage({ page: 2, page_size: 50 })
    expect(http.get).toHaveBeenCalledTimes(1)
    const [path, params] = http.get.mock.calls[0]
    expect(path).toBe('/api/llm/platform-usage')
    expect(params).toEqual({ page: 2, page_size: 50 })
  })

  it('getPlatformUsage 无参调用不炸（首页默认形态）', async () => {
    await LlmRoutingApi.getPlatformUsage()
    expect(http.get).toHaveBeenCalledWith('/api/llm/platform-usage', undefined)
  })
})
