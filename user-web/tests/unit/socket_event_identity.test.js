import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

vi.mock('@/utils/configManager', () => ({ getApiConfig: () => ({ baseUrl: window.location.origin }) }))
vi.mock('@/utils/runtimeConfig', () => ({
  rtNum: (_group, _key, fallback) => fallback,
  rtDur: (_group, _key, fallback) => fallback,
}))
// 令牌取自内存 store（JWT 不落 localStorage）；测试里没有 pinia，给一个空令牌的桩。
vi.mock('@/stores/user', () => ({ useUserStore: () => ({ token: '' }) }))

import AgentSocket from '@/utils/agentSocket'
import { ChatSocket } from '@/utils/chatSocket'

let sockets, connections

class SocketBoundary {
  static CONNECTING = 0
  static OPEN = 1
  constructor() {
    this.readyState = SocketBoundary.CONNECTING
    this.send = vi.fn()
    this.close = vi.fn(() => { this.readyState = 3 })
    connections.push(this)
  }
}

function create(kind) {
  const received = vi.fn(), error = vi.fn(), disconnected = vi.fn()
  const socket = kind === '坐席'
    ? new AgentSocket('identity-agent', 'identity-agent', { onNewMessage: received, onError: error, onDisconnected: disconnected })
    : new ChatSocket({ sessionId: 'identity-session', visitorId: 'identity-visitor', onMessage: received, onError: error, onDisconnected: disconnected })
  sockets.push(socket)
  return { socket, received, error, disconnected }
}

function message(kind, seq) {
  return { data: JSON.stringify({ ...(kind === '坐席' ? { event: 'new_message' } : { type: 'message' }), seq, payload: { id: 'synthetic-message' } }) }
}

beforeEach(() => {
  localStorage.clear()
  sessionStorage.clear()
  vi.useFakeTimers()
  vi.stubGlobal('WebSocket', SocketBoundary)
  vi.spyOn(console, 'warn').mockImplementation(() => {})
  sockets = []
  connections = []
})

afterEach(() => {
  sockets.forEach(socket => socket.close())
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.restoreAllMocks()
  vi.unstubAllGlobals()
})

describe.each(['坐席', '访客'])('%s网络事件按当前连接身份隔离', kind => {
  it.each(['message', 'error', 'close'])('重连后旧 %s 不能改变新连接', async event => {
    const { socket, received, error, disconnected } = create(kind)
    await socket.connect()
    const previous = connections.at(-1)
    previous.readyState = 3
    await socket.connect()
    const current = connections.at(-1)
    expect(current).not.toBe(previous)
    current.readyState = WebSocket.OPEN
    current.onopen()
    current.send.mockClear()
    const stopPing = vi.spyOn(socket, 'stopPing')
    const reconnect = vi.spyOn(socket, 'scheduleReconnect')
    const refresh = kind === '访客' ? vi.spyOn(socket, '_tryRefreshTokenAndReconnect') : null
    if (event === 'message') previous.onmessage(message(kind, 11))
    else if (event === 'error') previous.onerror(new Error('synthetic stale event'))
    else previous.onclose({ code: kind === '访客' ? 1006 : 1000 })
    expect(received).not.toHaveBeenCalled()
    expect(error).not.toHaveBeenCalled()
    expect(disconnected).not.toHaveBeenCalled()
    expect(stopPing).not.toHaveBeenCalled()
    expect(reconnect).not.toHaveBeenCalled()
    if (refresh) expect(refresh).not.toHaveBeenCalled()
    expect(socket.connected).toBe(true)
    expect(socket.lastSeq).toBe(0)
    expect(socket.pendingAcks.size).toBe(0)
    expect(current.send).not.toHaveBeenCalled()
  })

  it('主动关闭后晚到消息不更新序号、回执或业务回调', async () => {
    const { socket, received, error, disconnected } = create(kind)
    await socket.connect()
    const previous = connections.at(-1)
    socket.close()
    previous.onmessage(message(kind, 23))
    previous.onerror(new Error('synthetic closed event'))
    previous.onclose({ code: 1000 })
    expect(received).not.toHaveBeenCalled()
    expect(error).not.toHaveBeenCalled()
    expect(disconnected).not.toHaveBeenCalled()
    expect(socket.lastSeq).toBe(0)
    expect(socket.pendingAcks.size).toBe(0)
    expect(socket.ackFlushTimer).toBeNull()
  })

  it('当前对象仍投递正常消息、错误和关闭通知', async () => {
    const { socket, received, error, disconnected } = create(kind)
    await socket.connect()
    const current = connections.at(-1)
    current.readyState = WebSocket.OPEN
    current.onopen()
    current.onmessage(message(kind, 31))
    expect(received).toHaveBeenCalledWith({ id: 'synthetic-message' })
    expect(socket.lastSeq).toBe(31)
    const failure = new Error('synthetic current event')
    current.onerror(failure)
    expect(error).toHaveBeenCalledWith(failure)
    current.readyState = 3
    const closeEvent = { code: 1000 }
    current.onclose(closeEvent)
    expect(disconnected).toHaveBeenCalledWith(closeEvent)
    expect(socket.connected).toBe(false)
    expect(socket.reconnectTimer).not.toBeNull()
  })
})
