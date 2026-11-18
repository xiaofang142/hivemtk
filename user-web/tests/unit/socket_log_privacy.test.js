import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const refreshAccessToken = vi.hoisted(() => vi.fn())
vi.mock('@/utils/request', () => ({ refreshAccessToken }))
vi.mock('@/utils/configManager', () => ({ getApiConfig: () => ({ baseUrl: window.location.origin }) }))
vi.mock('@/utils/runtimeConfig', () => ({
  rtStr: (_group, _key, fallback) => fallback,
  rtNum: (_group, _key, fallback) => fallback,
  rtDur: (_group, _key, fallback) => fallback,
  rtBool: (_group, _key, fallback) => fallback,
}))
// 令牌取自内存 store（JWT 不落 localStorage）；测试里没有 pinia，用可写桩模拟「已登录」。
const storeToken = vi.hoisted(() => ({ value: '' }))
vi.mock('@/stores/user', () => ({
  useUserStore: () => ({ get token() { return storeToken.value } }),
}))

import AgentSocket from '@/utils/agentSocket'
import { ChatSocket } from '@/utils/chatSocket'

const privateText = 'synthetic-private-customer-message'
const token = 'synthetic-private-visitor-token'
const failure = () => new Error(`${privateText} Authorization=Bearer ${token}`)
let instances, sockets, warn, error, creationFailure

class SocketBoundary {
  static OPEN = 1
  static CONNECTING = 0
  constructor(url) {
    if (creationFailure) throw creationFailure
    this.url = url
    this.readyState = SocketBoundary.CONNECTING
    this.send = vi.fn()
    this.close = vi.fn(() => { this.readyState = 3 })
    instances.push(this)
  }
}

function chat(options = {}) {
  const socket = new ChatSocket({ sessionId: 'test-session', visitorId: 'test-visitor', visitorToken: token, ...options })
  sockets.push(socket)
  return socket
}

function expectClassifiedLogs() {
  const calls = [...warn.mock.calls, ...error.mock.calls]
  expect(calls.length).toBeGreaterThan(0)
  for (const call of calls) {
    expect(call).toHaveLength(1)
    expect(typeof call[0]).toBe('string')
    expect(call[0]).not.toContain(privateText)
    expect(call[0]).not.toContain(token)
  }
}

beforeEach(() => {
  vi.useFakeTimers()
  instances = []
  sockets = []
  creationFailure = null
  refreshAccessToken.mockReset()
  localStorage.clear()
  sessionStorage.clear()
  vi.stubGlobal('WebSocket', SocketBoundary)
  // 访客建连必须先换 ws-ticket（visitor_token 不进 URL query）。
  vi.stubGlobal('fetch', vi.fn(async () => ({ ok: true, json: async () => ({ data: { ticket: 'synthetic-ticket' } }) })))
  warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
  error = vi.spyOn(console, 'error').mockImplementation(() => {})
})

afterEach(() => {
  sockets.forEach(socket => socket.close())
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('Socket控制台只记录操作分类，不记录帧、凭证或错误对象', () => {
  it('坐席解析失败不输出原帧，后续正常帧仍交付', async () => {
    const onNewMessage = vi.fn()
    const socket = new AgentSocket('test-agent', 'test-agent', { onNewMessage })
    sockets.push(socket)
    await socket.connect()
    const boundary = instances[0]
    boundary.onmessage({ data: `${privateText} visitor_token=${token}` })
    expectClassifiedLogs()
    const payload = { content: privateText }
    boundary.onmessage({ data: JSON.stringify({ event: 'new_message', payload }) })
    expect(onNewMessage).toHaveBeenCalledWith(payload)
  })

  it('访客创建失败保持onError与退避，但不把可能含URL的异常写控制台', async () => {
    creationFailure = failure()
    const onError = vi.fn()
    const socket = chat({ onError })
    await socket.connect()
    expect(onError).toHaveBeenCalledWith(creationFailure)
    expect(socket.reconnectTimer).not.toBeNull()
    expectClassifiedLogs()
  })

  it('访客连接事件不输出带凭证的target，仍原样通知界面', async () => {
    const onError = vi.fn()
    await chat({ onError }).connect()
    const event = { target: { url: `wss://service.example/?visitor_token=${token}` }, message: privateText }
    instances[0].onerror(event)
    expect(onError).toHaveBeenCalledWith(event)
    expectClassifiedLogs()
  })

  it('解析失败和未知类型不写外部输入，后续正文回调不变', async () => {
    const onMessage = vi.fn()
    await chat({ onMessage }).connect()
    instances[0].onmessage({ data: `${privateText} ${token}` })
    instances[0].onmessage({ data: JSON.stringify({ type: privateText }) })
    expectClassifiedLogs()
    instances[0].onmessage({ data: JSON.stringify({ type: 'message', payload: { content: privateText } }) })
    expect(onMessage).toHaveBeenCalledWith({ content: privateText })
  })

  it.each([401, 503])('续期%s保留认证或重连分流，不写网络异常', async (status) => {
    storeToken.value = token
    const networkError = Object.assign(failure(), { response: { status }, config: { headers: { Authorization: token } } })
    refreshAccessToken.mockRejectedValue(networkError)
    const onError = vi.fn()
    const socket = chat({ onError })
    await socket._tryRefreshTokenAndReconnect(true)
    if (status === 401) {
      expect(onError).toHaveBeenCalledWith(expect.objectContaining({ message: 'auth_required' }))
      expect(socket.reconnectTimer).toBeNull()
    } else {
      expect(socket.reconnectTimer).not.toBeNull()
      expect(onError).not.toHaveBeenCalled()
    }
    expectClassifiedLogs()
  })

  it('ACK、离线回执和恢复失败只记分类，不改变发送协议', async () => {
    const socket = chat()
    await socket.connect()
    const boundary = instances[0]
    boundary.readyState = WebSocket.OPEN
    boundary.send.mockImplementation(() => { throw failure() })
    socket.pendingAcks.add(2)
    socket._flushAcks(true)
    socket.ackDelivered([4])
    socket.resume()
    socket.frameBuffer.set(2, {})
    socket.flushBuffer()
    expect(boundary.send.mock.calls.map(([raw]) => JSON.parse(raw).type)).toEqual(['ack', 'delivered', 'resume', 'recovery'])
    expectClassifiedLogs()
  })
})
