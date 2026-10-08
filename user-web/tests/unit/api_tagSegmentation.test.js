import { describe, it, expect, vi, beforeEach } from 'vitest'
import { clearMocks, getCalls } from './apitest-helper.js'

// 标签分群 api 层的端点归属：这一层唯一的价值就是"方法 + 路径"打对，
// 所以断言全部打在 http.* 收到的实参上，不看函数体写了什么。
const mocks = vi.hoisted(() => {
  const ok = () => Promise.resolve({})
  const http = {
    get: vi.fn(ok),
    post: vi.fn(ok),
    put: vi.fn(ok),
    delete: vi.fn(ok),
    upload: vi.fn(ok)
  }
  const request = vi.fn(ok)
  request.get = vi.fn(ok)
  request.post = vi.fn(ok)
  request.put = vi.fn(ok)
  request.delete = vi.fn(ok)
  request.upload = vi.fn(ok)
  const axios = {
    get: vi.fn(ok),
    post: vi.fn(ok),
    put: vi.fn(ok),
    delete: vi.fn(ok)
  }
  return { request, http, axios }
})

vi.mock('@/utils/request', () => ({ default: mocks.request, http: mocks.http }))

import { TagSegmentationApi } from '@/api/tagSegmentation.js'

beforeEach(() => clearMocks(mocks))

describe('标签的增改删各走服务端真实注册的那条口', () => {
  it('编辑标签 PUT 到 /api/session-tags/{id}，不是集合路径', async () => {
    await TagSegmentationApi.updateTags({ id: 7, name: '高价值', code: 'hv', group: 'default' })
    expect(mocks.http.put).toHaveBeenCalledWith(
      '/api/session-tags/7',
      { name: '高价值', code: 'hv', group: 'default' }
    )
    // 服务端只注册了 PUT /api/session-tags/:id（service_routes.go:67）：
    // 打集合路径会回 405 METHOD_NOT_ALLOWED（现测），编辑功能整个失效。
    const coll = mocks.http.put.mock.calls.filter((c) => c[0] === '/api/session-tags')
    expect(coll).toEqual([])
  })

  it('id 只出现在路径里，body 不再重复带一份', async () => {
    await TagSegmentationApi.updateTags({ id: '42', name: 'x' })
    const [, body] = mocks.http.put.mock.calls[0]
    expect(body).not.toHaveProperty('id')
    expect(mocks.http.put.mock.calls[0][0]).toBe('/api/session-tags/42')
  })

  it('新建走集合 POST、删除走 /{id} DELETE', async () => {
    await TagSegmentationApi.createTag({ name: '新标签' })
    await TagSegmentationApi.deleteTag(9)
    expect(mocks.http.post).toHaveBeenCalledWith('/api/session-tags', { name: '新标签' })
    expect(mocks.http.delete).toHaveBeenCalledWith('/api/session-tags/9')
  })

  it('规则侧四条口都带得上 :id 的那一段', async () => {
    await TagSegmentationApi.getTagRules({ page: 1 })
    await TagSegmentationApi.saveTagRule({ name: 'r' })
    await TagSegmentationApi.updateTagRule(3, { name: 'r2' })
    await TagSegmentationApi.deleteTagRule(3)
    expect(mocks.http.get).toHaveBeenCalledWith('/api/customer-360/tag-rules', { page: 1 })
    expect(mocks.http.post).toHaveBeenCalledWith('/api/customer-360/tag-rules', { name: 'r' })
    expect(mocks.http.put).toHaveBeenCalledWith('/api/customer-360/tag-rules/3', { name: 'r2' })
    expect(mocks.http.delete).toHaveBeenCalledWith('/api/customer-360/tag-rules/3')
  })

  it('每个导出方法都真的发出一条请求（没有静默的空实现）', async () => {
    const names = Object.keys(TagSegmentationApi)
    for (const name of names) {
      await TagSegmentationApi[name]({ id: 1 })
    }
    expect(names).toHaveLength(10)
    expect(getCalls(mocks)).toHaveLength(names.length)
  })
})
