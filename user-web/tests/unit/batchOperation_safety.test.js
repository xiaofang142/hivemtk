import { describe, it, expect, vi, beforeEach } from 'vitest'
const http = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('@/utils/request', () => ({ http }))
import { runBatch, previewBatch } from '@/api/batchOperation'
beforeEach(() => {
  vi.clearAllMocks()
  http.get.mockResolvedValue({ list: [{ id: '1' }], total: 1 })
  http.post.mockResolvedValue({ selected_count: 1, preview: [] })
})
describe('批量操作写入边界', () => {
  it('预览模式执行删除只调用预览端点', async () => {
    await runBatch({ tool: 'delete', source: 'all', dryRun: true })
    expect(http.post).toHaveBeenCalledWith('/api/batch/preview', expect.objectContaining({ operation_type: 'delete' }))
    expect(http.post).toHaveBeenCalledTimes(1)
  })
  it.each(['[]', 'null', '123', '', '{}', '{"page":2}', '{"status":"active"}', '{"keyword":{}}'])('拒绝不受支持的筛选 %s，不查询也不写入', async filter => {
    await expect(runBatch({ tool: 'delete', source: 'filter', filter, dryRun: false })).rejects.toThrow()
    expect(http.get).not.toHaveBeenCalled()
    expect(http.post).not.toHaveBeenCalled()
  })
  it('筛选 false/0 不被吞掉，分页保持受控', async () => {
    await previewBatch({ tool: 'delete', source: 'filter', filter: '{"is_verify":0,"type":1}' })
    expect(http.get).toHaveBeenCalledWith('/api/clues/list', { page: 1, limit: 500, is_verify: 0, type: 1 })
  })
  it('未知工具和无文件导入在发请求前拒绝', async () => {
    await expect(runBatch({ tool: 'unknown' })).rejects.toThrow()
    await expect(runBatch({ tool: 'import', source: 'import', dryRun: false })).rejects.toThrow()
    expect(http.get).not.toHaveBeenCalled()
    expect(http.post).not.toHaveBeenCalled()
  })
  it('预览后匹配集合变化必须重新预览，不能删除新增匹配项', async () => {
    const preview = await previewBatch({ tool: 'delete', source: 'all' })
    http.post.mockClear()
    http.get.mockResolvedValue({ list: [{ id: '2' }], total: 1 })
    await expect(runBatch({ tool: 'delete', source: 'all', dryRun: false, expectedIds: preview.selected_ids })).rejects.toThrow('匹配记录已变化')
    expect(http.post).not.toHaveBeenCalled()
  })
  it('不完整的匹配集合不能执行部分删除', async () => {
    http.get.mockResolvedValue({ list: [{ id: '1' }], total: 2 })
    await expect(runBatch({ tool: 'delete', source: 'all', dryRun: false })).rejects.toThrow()
    expect(http.post).not.toHaveBeenCalled()
  })
})
