import { afterEach, describe, expect, it, vi } from 'vitest'
import { fetchBulkLeads, stageLeadSelection, takeLeadSelection } from '@/utils/bulkLeadSelection'

const user = { id: 'operator-a', tenant_id: 'tenant-a' }
afterEach(() => vi.restoreAllMocks())

describe('一次性收件人交接', () => {
  it('去重并仅消费一次，不持久化收件人', () => {
    const token = stageLeadSelection(['lead-a', 'lead-a'], user)
    expect(takeLeadSelection(token, user)).toEqual(['lead-a'])
    expect(takeLeadSelection(token, user)).toBeNull()
  })
  it.each([{ ...user, id: 'operator-b' }, { ...user, tenant_id: 'tenant-b' }])('换身份拒绝恢复且不可重放', other => {
    const token = stageLeadSelection(['lead-a'], user)
    expect(takeLeadSelection(token, other)).toBeNull()
    expect(takeLeadSelection(token, user)).toBeNull()
  })
  it('缺身份或缺 ID 拒绝创建', () => {
    expect(stageLeadSelection(['lead-a'], { id: 'operator-a' })).toBeNull()
    expect(stageLeadSelection([undefined], user)).toBeNull()
    expect(stageLeadSelection([], user)).toBeNull()
  })
  it('五分钟后失效，新选择替代旧选择', () => {
    vi.spyOn(Date, 'now').mockReturnValue(1000)
    const token = stageLeadSelection(['lead-a'], user)
    vi.spyOn(Date, 'now').mockReturnValue(301000)
    expect(takeLeadSelection(token, user)).toBeNull()
    const old = stageLeadSelection(['lead-a'], user)
    const current = stageLeadSelection(['lead-b'], user)
    expect(takeLeadSelection(old, user)).toBeNull()
    expect(takeLeadSelection(current, user)).toEqual(['lead-b'])
  })
})

describe('当前租户联系人解析', () => {
  it('跨列表分页查找，使用当前姓名与号码', async () => {
    const list = vi.fn().mockResolvedValueOnce({ list: [{ id: 'other', name: '其他', account: '+100' }], total: 2 })
      .mockResolvedValueOnce({ list: [{ id: 'target', name: '当前姓名', account: '+200' }], total: 2 })
    expect(await fetchBulkLeads(list, ['target'])).toEqual([
      expect.objectContaining({ id: 'other' }), expect.objectContaining({ id: 'target', name: '当前姓名', phone: '+200' }),
    ])
    expect(list.mock.calls).toEqual([[1, 1000], [2, 1000]])
  })
  it.each([
    { list: [{ id: 'other', account: '+100' }], total: 1 },
    { list: [{ id: 'target', account: '' }], total: 1 },
    { list: 'invalid' },
  ])('缺失或不可用联系人整批拒绝', async response => {
    await expect(fetchBulkLeads(vi.fn().mockResolvedValue(response), ['target'])).rejects.toThrow()
  })
  it('接口失败原样拒绝，不使用上次的联系人', async () => {
    await expect(fetchBulkLeads(vi.fn().mockRejectedValue(new Error('offline')), ['target'])).rejects.toThrow('offline')
  })
})
