import { describe, it, expect, beforeEach, vi } from 'vitest'
import { setActivePinia, createPinia } from 'pinia'

// dictionary store 的回落语义测试。
//
// 重点：DB 优先、DB 没有就回落本地常量，且**不反过来**。反过来的后果是
// 「翻译用本地常量、后端判定用库里值域」，两边对同一行给出不一致的结论。
const dictionaryGet = vi.fn()

vi.mock('@/api/runtimeConfig', () => ({
  dictionary: (...args) => dictionaryGet(...args),
  runtimeConfig: () => Promise.resolve({ data: {} }),
}))

const { useDictionaryStore } = await import('@/stores/dictionary')

describe('dictionary store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    dictionaryGet.mockReset()
  })

  const LOCAL = [
    { code: 'pending', name: '待审批' },
    { code: 'approved', name: '已批准' },
  ]

  it('后端有这一批时以 DB 为准', async () => {
    dictionaryGet.mockResolvedValue({
      parent: 'approval_status',
      items: [{ code: 'pending', name: '待审批(DB)', enabled: true }],
      transitions: [{ from_code: 'pending', to_code: 'approved' }],
    })
    const s = useDictionaryStore()
    const items = await s.items('approval_status', LOCAL)
    expect(items).toHaveLength(1)
    expect(items[0].name).toBe('待审批(DB)')
    const entry = await s.load('approval_status', LOCAL)
    expect(entry.transitions).toHaveLength(1)
    expect(s.failed).toEqual([])
  })

  it('后端 404（表里没有这一批）时回落本地常量', async () => {
    dictionaryGet.mockRejectedValue({ response: { status: 404 } })
    const s = useDictionaryStore()
    const items = await s.items('approval_status', LOCAL)
    expect(items).toHaveLength(2)
    expect(items[0].name).toBe('待审批')
  })

  it('后端 500/网络错也回落本地常量，但记进 failed 供排查', async () => {
    dictionaryGet.mockRejectedValue(new Error('boom'))
    const s = useDictionaryStore()
    const items = await s.items('approval_status', LOCAL)
    expect(items).toHaveLength(2)
    expect(s.failed).toContain('approval_status')
  })

  it('失败不写进 loaded：后端恢复后还能再拉一次', async () => {
    dictionaryGet.mockRejectedValueOnce(new Error('boom'))
    const s = useDictionaryStore()
    await s.items('approval_status', LOCAL)
    expect(s.failed).toContain('approval_status')

    dictionaryGet.mockResolvedValue({ items: [{ code: 'pending', name: 'DB 版' }], transitions: [] })
    const again = await s.items('approval_status', LOCAL)
    expect(again[0].name).toBe('DB 版')
    expect(s.failed).not.toContain('approval_status')
  })

  it('同一个 parent 只拉一次（Promise 去重）', async () => {
    dictionaryGet.mockResolvedValue({ items: [{ code: 'a', name: 'A' }], transitions: [] })
    const s = useDictionaryStore()
    await Promise.all([
      s.items('approval_status', LOCAL),
      s.items('approval_status', LOCAL),
      s.load('approval_status', LOCAL),
    ])
    expect(dictionaryGet).toHaveBeenCalledTimes(1)
  })

  it('label 译不出时回落原值（比显示空白好）', async () => {
    dictionaryGet.mockResolvedValue({ items: LOCAL, transitions: [] })
    const s = useDictionaryStore()
    expect(await s.label('approval_status', 'pending')).toBe('待审批')
    expect(await s.label('approval_status', 'weird_code')).toBe('weird_code')
  })

  it('后端返回非数组时不当成有数据', async () => {
    dictionaryGet.mockResolvedValue({ items: null, transitions: 'oops' })
    const s = useDictionaryStore()
    const entry = await s.load('approval_status', LOCAL)
    expect(entry.items).toEqual([])
    expect(entry.transitions).toEqual([])
  })

  it('reset 清空全部缓存', async () => {
    dictionaryGet.mockResolvedValue({ items: LOCAL, transitions: [] })
    const s = useDictionaryStore()
    await s.items('approval_status', LOCAL)
    s.reset()
    expect(s.loaded).toEqual({})
    expect(s.failed).toEqual([])
  })
})
