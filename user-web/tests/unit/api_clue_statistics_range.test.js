/**
 * 线索区间统计 API 契约：服务端六维聚合，替代前端 10000 行截断。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'

const { http } = vi.hoisted(() => ({
  http: { get: vi.fn() }
}))
vi.mock('@/utils/request', () => ({ default: { get: http.get }, http }))

import { clueApi } from '@/api/clue.js'

const sentUrl = () => http.get.mock.calls.at(-1)[0]

describe('clueApi.statisticsRange 区间统计', () => {
  beforeEach(() => {
    http.get.mockReset()
    http.get.mockResolvedValue({ data: { total: 0, truncated: false } })
  })

  it('带上 start/end 打到服务端区间统计端点', async () => {
    await clueApi.statisticsRange(1000, 2000)
    const u = new URL(sentUrl(), 'http://x')
    expect(u.pathname).toBe('/api/clues/statistics/range')
    expect(u.searchParams.get('start')).toBe('1000')
    expect(u.searchParams.get('end')).toBe('2000')
  })

  it('0 边界不写入查询串（表示不设该侧边界）', async () => {
    await clueApi.statisticsRange(0, 2000)
    const u = new URL(sentUrl(), 'http://x')
    expect([...u.searchParams.keys()]).toEqual(['end'])
  })
})
