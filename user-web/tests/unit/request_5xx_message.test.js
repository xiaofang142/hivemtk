import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import axios from 'axios'
import { ElMessage } from 'element-plus'

// 5xx 的提示必须优先用"服务端说了什么"。
//
// 真实场景（2026-10-10 线上）：/dashboard/drafts 调 /api/manage/order-drafts 拿到 503，
// 后端 body 里写着「草稿服务未装配 + 该翻哪个开关」，而 request.js 的 case 503 一律
// 替换成通用文案「服务器内部错误」⇒ 值班看到一句无从下手的话，页面看起来就是"坏了"。
//
// 判据打在"弹出去的文案"上，但**不用 DOM 断言**：ElMessage 是异步挂 DOM 的，全量套件
// 高负载下会读到空列表（实测假红一次）。改成 spy ElMessage.error —— 同一条同步调用链，
// 确定性读法。

const routerPush = vi.hoisted(() => vi.fn())
vi.mock('@/router', () => ({ default: { push: (...a) => routerPush(...a) } }))

let request
let errorSpy

async function freshRequest() {
  vi.resetModules() // showToast 的去重状态挂在模块上，逐条重来
  const m = await import('@/utils/request')
  request = m.default
}

// 造 5xx：axios v1 的 settle 只在内置适配器里跑，所以这里自己抛 AxiosError
// （config/response 都得带上，拦截器读的就是这两个字段）。
function forceStatus(status, body) {
  request.defaults.adapter = async (config) => {
    const response = {
      data: body,
      status,
      statusText: String(status),
      headers: { 'content-type': 'application/json' },
      config
    }
    throw new axios.AxiosError(`Request failed with status code ${status}`, 'ERR_BAD_REQUEST', config, null, response)
  }
}

async function hit() {
  try {
    await request.get('/api/manage/order-drafts')
  } catch { /* 预期 reject */ }
}

describe('5xx 错误提示：服务端能说清原因时不许替换成通用文案', () => {
  beforeEach(() => {
    errorSpy = vi.spyOn(ElMessage, 'error').mockImplementation(() => {})
    vi.spyOn(ElMessage, 'success').mockImplementation(() => {})
    vi.spyOn(ElMessage, 'warning').mockImplementation(() => {})
    vi.spyOn(ElMessage, 'info').mockImplementation(() => {})
    routerPush.mockReset()
  })

  afterEach(() => {
    vi.restoreAllMocks()
  })

  it('503 带 message → 提示用服务端那句话（含开关名），而不是「服务器内部错误」', async () => {
    await freshRequest()
    forceStatus(503, { code: 'SERVICE_UNAVAILABLE', message: '草稿服务未装配（LTC_ORDER_DRAFT_ENABLED=off）' })
    await hit()
    expect(errorSpy).toHaveBeenCalledTimes(1)
    expect(String(errorSpy.mock.calls[0][0])).toContain('LTC_ORDER_DRAFT_ENABLED=off')
    expect(String(errorSpy.mock.calls[0][0])).not.toBe('服务器内部错误')
  })

  it('500 完全没有 message → 仍回落到通用文案（不能弹空条）', async () => {
    await freshRequest()
    forceStatus(500, {})
    await hit()
    expect(errorSpy).toHaveBeenCalledTimes(1)
    expect(String(errorSpy.mock.calls[0][0]).trim().length).toBeGreaterThan(0)
  })

  it('反向：500 带 message 也必须用它（不是只对 503 开的口子）', async () => {
    await freshRequest()
    forceStatus(500, { code: 500, message: '数据库连接池已耗尽' })
    await hit()
    expect(String(errorSpy.mock.calls[0][0])).toContain('数据库连接池已耗尽')
  })
})
