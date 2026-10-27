import { describe, it, expect, vi, beforeEach } from 'vitest'
import axios from 'axios'
import { settleBody } from './settle-helper'

// 502/503/504 是「上游/依赖不可用」，不是本服务内部故障。
// 后端已经把渠道原因写在 message 里（如飞书 code=10003 invalid param），
// 拦截器若一律盖成「服务器内部错误」，用户只会看到一句没法照做的废话并盲目重试。
//
// 这条用例钉住：502 要展示服务端 message；500 仍保留通用文案（内部故障不泄露细节）。

const routerPush = vi.hoisted(() => vi.fn())
vi.mock('@/router', () => ({ default: { push: (...a) => routerPush(...a) } }))

const ENVELOPE_502 = { code: 5001, message: '刷新飞书 Token 失败：code=10003 invalid param' }

let request
async function freshRequest() {
  vi.resetModules()
  const m = await import('@/utils/request')
  request = m.default
}

function always(status, data) {
  request.defaults.adapter = async (config) => {
    const response = {
      data,
      status,
      statusText: status === 502 ? 'Bad Gateway' : 'Internal Server Error',
      headers: { 'content-type': 'application/json' },
      config,
    }
    // 自定义 adapter 不会走 axios 内置 settle：>=400 必须自己 reject，
    // 否则会被当成 fulfilled 响应走业务码分支，测到的就不是错误分支了。
    throw new axios.AxiosError(`Request failed with status code ${status}`, String(status), config, {}, response)
  }
}

async function hit() {
  try {
    await request.get('/api/feishu/accounts/16/refresh-token', { _silent: false })
  } catch (e) {
    await settleBody()
    return e
  }
  await settleBody()
  return null
}

describe('request 拦截器：502 展示可照做的上游原因，500 仍是通用内部错误', () => {
  beforeEach(async () => {
    localStorage.clear()
    document.body.innerHTML = ''
    routerPush.mockReset()
    await freshRequest()
  })

  it('502 使用服务端 message，而不是「服务器内部错误」', async () => {
    always(502, ENVELOPE_502)
    const err = await hit()
    expect(err).toBeTruthy()
    const toasts = Array.from(document.querySelectorAll('.el-message')).map((n) => n.textContent.trim())
    expect(toasts).toContain(ENVELOPE_502.message)
    expect(toasts).not.toContain('服务器内部错误')
  })

  it('500 仍使用通用内部错误文案（不把内部细节抛给用户）', async () => {
    always(500, { code: 'INTERNAL_ERROR_6002', message: 'sql: connection reset by peer' })
    const err = await hit()
    expect(err).toBeTruthy()
    const toasts = Array.from(document.querySelectorAll('.el-message')).map((n) => n.textContent.trim())
    expect(toasts.join(' ')).toMatch(/服务器内部错误|Internal server error/i)
    expect(toasts.join(' ')).not.toContain('sql:')
  })
})
