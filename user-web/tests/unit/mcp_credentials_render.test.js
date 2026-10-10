import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createPinia } from 'pinia'

// MCP 凭证签发页的回归用例（真实报障：签发后复制出来是 "Client ID: undefined / API Key: undefined"）。
//
// 根因不是"后端没返回"，而是**重复拆包**：@/utils/request 的响应拦截器已经把
// {code,message,data} 里的 data 拆出来了，页面又写 `issued.value = res?.data ?? {}`，
// 于是 res.data 为 undefined 时兜成了空对象 {} ⇒ issued.client_id/api_key 全 undefined。
// 复制文本模板正是 `Client ID: ${issued.client_id}\nAPI Key: ${issued.api_key}`，
// 用户拿到的就是那串 undefined。
//
// 判据打在**用户真正拿到的东西**上（剪贴板文本），而不是"页面里有没有 res.data"：
// 前者无论中间怎么改都只有真修好才会绿。

const api = vi.hoisted(() => ({
  list: vi.fn(),
  create: vi.fn(),
  update: vi.fn(),
  remove: vi.fn()
}))
vi.mock('@/api/mcpCredential', () => ({ mcpCredentialApi: api }))

import Credentials from '@/views/mcp/Credentials.vue'

const ISSUED = { id: 7, client_id: 'mcp-test123', api_key: 'wgk_testkey456', api_hint: '只显示一次' }

const findByText = (wrapper, text) =>
  wrapper.findAll('button').find((b) => b.text().replace(/\s+/g, '') === text.replace(/\s+/g, ''))

describe('MCP 凭证签发：明文必须真的显示/复制出来（不是 undefined）', () => {
  let clipboard

  beforeEach(() => {
    document.body.innerHTML = ''
    clipboard = { writeText: vi.fn().mockResolvedValue(undefined) }
    Object.defineProperty(navigator, 'clipboard', { value: clipboard, configurable: true })
    api.list.mockReset().mockResolvedValue([])
    api.create.mockReset().mockResolvedValue(ISSUED)
  })

  it('签发后「复制并关闭」给出的文本含真实 Client ID/API Key，且不含 undefined', async () => {
    const wrapper = mount(Credentials, {
      global: { plugins: [ElementPlus, createPinia()] },
      attachTo: document.body
    })
    await flushPromises()

    await findByText(wrapper, '签发新凭证').trigger('click')
    await flushPromises()
    await findByText(wrapper, '签发').trigger('click')
    await flushPromises()

    expect(api.create).toHaveBeenCalledTimes(1)

    // 弹窗里就能看见明文（不是空白/undefined）
    const body = document.body.textContent || ''
    expect(body).toContain('mcp-test123')
    expect(body).toContain('wgk_testkey456')
    expect(body).not.toContain('undefined')

    await findByText(wrapper, '复制并关闭').trigger('click')
    await flushPromises()

    expect(clipboard.writeText).toHaveBeenCalledTimes(1)
    const copied = clipboard.writeText.mock.calls[0][0]
    expect(copied).toBe(`Client ID: ${ISSUED.client_id}\nAPI Key: ${ISSUED.api_key}`)
    expect(copied).not.toContain('undefined')
  })

  it('反向：接口返回被拦截器拆包后的形状时，页面不得再取一层 .data', async () => {
    // 若实现退回 `res?.data ?? {}`，本用例会以 undefined 失败 —— 这就是这次的钉子。
    api.create.mockResolvedValue({ ...ISSUED })
    const wrapper = mount(Credentials, {
      global: { plugins: [ElementPlus, createPinia()] },
      attachTo: document.body
    })
    await flushPromises()
    await findByText(wrapper, '签发新凭证').trigger('click')
    await flushPromises()
    await findByText(wrapper, '签发').trigger('click')
    await flushPromises()
    await findByText(wrapper, '复制并关闭').trigger('click')
    await flushPromises()
    expect(clipboard.writeText.mock.calls[0][0]).not.toMatch(/undefined/)
  })
})
