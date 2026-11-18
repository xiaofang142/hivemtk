import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import axios from 'axios'

const getApiConfig = vi.hoisted(() => vi.fn())
vi.mock('@/utils/configManager', () => ({ getApiConfig }))
vi.mock('@/router', () => ({ default: { push: vi.fn() } }))

const privateText = 'synthetic-private-response-body'
const token = 'synthetic-request-token'
let request, requestModule, errorLog

function expectPrivateLogAbsent() {
  expect(errorLog).toHaveBeenCalled()
  for (const args of errorLog.mock.calls) {
    expect(args).toHaveLength(1)
    expect(typeof args[0]).toBe('string')
    expect(args[0]).not.toContain(privateText)
    expect(args[0]).not.toContain(token)
  }
}

beforeEach(async () => {
  vi.resetModules()
  localStorage.clear()
  sessionStorage.clear()
  getApiConfig.mockReset()
  getApiConfig.mockReturnValue({ baseUrl: window.location.origin })
  requestModule = await import('@/utils/request')
  request = requestModule.getRequestInstance()
  errorLog = vi.spyOn(console, 'error').mockImplementation(() => {})
})

afterEach(() => { vi.restoreAllMocks() })

describe('请求层日志保留错误分类，不复制凭证与响应正文', () => {
  it('请求拦截器异常仍原样拒绝，不打印携带凭证的异常对象', async () => {
    const failure = Object.assign(new Error(`${privateText} ${token}`), {
      config: { _silent: true, headers: { Authorization: `Bearer ${token}` } },
    })
    request.interceptors.request.use(() => { throw failure })
    request.defaults.adapter = vi.fn()
    await expect(request.get('/api/test', { _silent: true, _skipRefresh: true })).rejects.toBe(failure)
    expect(request.defaults.adapter).not.toHaveBeenCalled()
    expectPrivateLogAbsent()
  })

  it.each([200, 502])('HTTP %s非JSON仍拒绝，不打印网关返回的业务正文', async (status) => {
    request.defaults.adapter = async (config) => {
      const response = { status, statusText: 'synthetic', config,
        headers: { 'content-type': 'text/html' }, data: `<html>${privateText} ${token}</html>` }
      if (status === 200) return response
      throw new axios.AxiosError('synthetic gateway failure', 'ERR_BAD_RESPONSE', config, null, response)
    }
    const promise = request.get('/api/test', { _silent: true, _skipRefresh: true })
    await expect(promise).rejects.toMatchObject({ status })
    expectPrivateLogAbsent()
  })

  it('配置读取失败仍抛原异常，不打印可能含凭证的配置', async () => {
    const failure = new Error(`${privateText} Authorization=${token}`)
    getApiConfig.mockImplementation(() => { throw failure })
    await expect(requestModule.updateRequestConfig()).rejects.toBe(failure)
    expectPrivateLogAbsent()
  })
})
