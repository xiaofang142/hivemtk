import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const refreshAccessToken = vi.hoisted(() => vi.fn())
vi.mock('@/utils/request', () => ({ refreshAccessToken }))
vi.mock('@/utils/configManager', () => ({ getApiConfig: () => ({ baseUrl: window.location.origin }) }))
vi.mock('@/utils/runtimeConfig', () => ({
  rtNum: (_group, _key, fallback) => fallback,
  rtDur: (_group, _key, fallback) => fallback,
}))

// 令牌取自内存 store（JWT 不落 localStorage）；测试里没有 pinia，用可写桩模拟「已登录」。
const storeToken = vi.hoisted(() => ({ value: '' }))
vi.mock('@/stores/user', () => ({
  useUserStore: () => ({ get token() { return storeToken.value } }),
}))

import AgentSocket from '@/utils/agentSocket'
import { ChatSocket } from '@/utils/chatSocket'

let sockets, connections

class SocketBoundary {
  static OPEN = 1
  static CONNECTING = 0
  constructor() {
    this.readyState = SocketBoundary.CONNECTING
    this.send = vi.fn()
    this.close = vi.fn(() => { this.readyState = 3 })
    connections.push(this)
  }
}

function agent() {
  const socket = new AgentSocket('synthetic-agent', 'synthetic-agent')
  sockets.push(socket)
  return socket
}

function chat(options = {}) {
  const socket = new ChatSocket({ sessionId: 'synthetic-session', visitorId: 'synthetic-visitor', ...options })
  sockets.push(socket)
  return socket
}

beforeEach(() => {
  // jsdom 的存储清理可能安排 storage 事件；它不属于 Socket 的计时器。
  localStorage.clear()
  sessionStorage.clear()
  vi.useFakeTimers()
  sockets = []
  connections = []
  refreshAccessToken.mockReset()
  vi.stubGlobal('WebSocket', SocketBoundary)
  vi.spyOn(console, 'warn').mockImplementation(() => {})
})

afterEach(() => {
  sockets.forEach(socket => socket.close())
  vi.clearAllTimers()
  vi.useRealTimers()
  vi.unstubAllGlobals()
  vi.restoreAllMocks()
})

describe('主动关闭后不再建立 Socket 连接', () => {
  it('坐席重复安排只保留一个计时器和一次尝试，未关闭时仍正常重连', async () => {
    const socket = agent()
    socket.scheduleReconnect()
    socket.scheduleReconnect()
    expect(socket.getReconnectAttempts()).toBe(1)
    expect(vi.getTimerCount()).toBe(1)
    await vi.advanceTimersByTimeAsync(2000)
    expect(connections).toHaveLength(1)
    expect(vi.getTimerCount()).toBe(0)
  })

  it('坐席关闭会实际清除待重连计时器，之后不能再安排', async () => {
    const socket = agent()
    socket.scheduleReconnect()
    socket.close()
    expect(vi.getTimerCount()).toBe(0)
    socket.scheduleReconnect()
    expect(vi.getTimerCount()).toBe(0)
    await vi.advanceTimersByTimeAsync(30000)
    expect(connections).toHaveLength(0)
  })

  it.each(['坐席', '访客'])('%s实例关闭后显式 connect 也不会复活', (name) => {
    const socket = name === '坐席' ? agent() : chat()
    socket.close()
    socket.connect()
    expect(connections).toHaveLength(0)
  })

  it.each(['坐席', '访客'])('%s关闭后晚到的 open 事件不能重新启动心跳或回调', async (name) => {
    const onConnected = vi.fn()
    const socket = name === '坐席'
      ? new AgentSocket('synthetic-agent', 'synthetic-agent', { onConnected })
      : chat({ onConnected })
    if (name === '坐席') sockets.push(socket)
    await socket.connect()
    const boundary = connections.at(-1)
    // buildWsUrl 写入 sticky sessionStorage 会安排 jsdom 的存储事件；
    // 只验证晚到 open 不增加计时器，并直接核对心跳启动入口。
    const baselineTimers = vi.getTimerCount()
    const startPing = vi.spyOn(socket, 'startPing')
    socket.close()
    boundary.readyState = WebSocket.OPEN
    boundary.onopen()
    expect(socket.connected).toBe(false)
    expect(onConnected).not.toHaveBeenCalled()
    expect(startPing).not.toHaveBeenCalled()
    expect(vi.getTimerCount()).toBe(baselineTimers)
    expect(boundary.send).not.toHaveBeenCalled()
  })

  it.each(['成功', '失败'])('访客关闭期间晚到的刷新%s不建立连接或安排重试', async (outcome) => {
    storeToken.value = 'synthetic-agent-token'
    let finish
    refreshAccessToken.mockImplementation(() => new Promise((resolve, reject) => {
      finish = outcome === '成功' ? () => resolve('synthetic-new-token') : () => reject(new Error('synthetic network failure'))
    }))
    const onError = vi.fn()
    const socket = chat({ onError })
    const pending = socket._tryRefreshTokenAndReconnect(true)
    await vi.advanceTimersByTimeAsync(0)
    expect(refreshAccessToken).toHaveBeenCalledTimes(1)
    socket.close()
    finish()
    await pending
    expect(connections).toHaveLength(0)
    expect(vi.getTimerCount()).toBe(0)
    expect(onError).not.toHaveBeenCalled()
  })

  it('未关闭的访客刷新成功仍建立连接，主动关闭前提不会全局禁止续期', async () => {
    storeToken.value = 'synthetic-agent-token'
    refreshAccessToken.mockResolvedValue('synthetic-new-token')
    await chat()._tryRefreshTokenAndReconnect(true)
    expect(connections).toHaveLength(1)
  })
})
