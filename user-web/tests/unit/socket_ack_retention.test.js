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
  static OPEN = 1
  static CONNECTING = 0
  static CLOSED = 3
  constructor() {
    this.readyState = SocketBoundary.CONNECTING
    this.send = vi.fn()
    this.close = vi.fn(() => { this.readyState = SocketBoundary.CLOSED })
    connections.push(this)
  }
}

beforeEach(() => {
  vi.useFakeTimers()
  localStorage.clear()
  sessionStorage.clear()
  sockets = []
  connections = []
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

const ackFrames = boundary => boundary.send.mock.calls
  .map(([raw]) => JSON.parse(raw)).filter(frame => frame.type === 'ack')

for (const [name, create] of [
  ['坐席', () => new AgentSocket('synthetic-agent', 'synthetic-agent')],
  ['访客', () => new ChatSocket({ sessionId: 'synthetic-session', visitorId: 'synthetic-visitor' })],
]) {
  describe(`${name} ACK 批次保留`, () => {
    async function open() {
      const socket = create()
      sockets.push(socket)
      // 坐席端 buildWsUrl 是 async（换票），connect 必须 await 后才建出 boundary。
      await socket.connect()
      const boundary = connections.at(-1)
      boundary.readyState = WebSocket.OPEN
      boundary.onopen()
      boundary.send.mockClear()
      return { socket, boundary }
    }

    it('发送抛错保留批次，重新连接后补交且成功批次不再重复', async () => {
      const { socket, boundary } = await open()
      boundary.send.mockImplementation(() => { throw new Error('synthetic transport failure') })
      socket._enqueueAck(4)
      socket._enqueueAck(2)
      await vi.advanceTimersByTimeAsync(200)
      expect(ackFrames(boundary)).toEqual([{ type: 'ack', seq: [2, 4] }])
      expect([...socket.pendingAcks]).toEqual([4, 2])
      expect(socket.ackFlushTimer).toBeNull()

      boundary.readyState = WebSocket.CLOSED
      await socket.connect()
      const replacement = connections.at(-1)
      replacement.readyState = WebSocket.OPEN
      replacement.onopen()
      expect(ackFrames(replacement)).toEqual([{ type: 'ack', seq: [2, 4] }])
      expect(socket.pendingAcks.size).toBe(0)
      socket._flushAcks(true)
      expect(ackFrames(replacement)).toHaveLength(1)
    })

    it('发送期间新加入的回执不能被旧批次清除', async () => {
      const { socket, boundary } = await open()
      boundary.send.mockImplementationOnce(() => socket._enqueueAck(9))
      socket._enqueueAck(2)
      socket._flushAcks(true)
      expect(ackFrames(boundary)).toEqual([{ type: 'ack', seq: [2] }])
      expect([...socket.pendingAcks]).toEqual([9])
      await vi.advanceTimersByTimeAsync(200)
      expect(ackFrames(boundary)).toEqual([{ type: 'ack', seq: [2] }, { type: 'ack', seq: [9] }])
      expect(socket.pendingAcks.size).toBe(0)
    })

    it('断开时保留回执且不忙重试，关闭取消待发送计时器', async () => {
      const { socket, boundary } = await open()
      boundary.readyState = WebSocket.CLOSED
      socket._enqueueAck(3)
      await vi.advanceTimersByTimeAsync(200)
      expect(ackFrames(boundary)).toEqual([])
      expect([...socket.pendingAcks]).toEqual([3])
      expect(socket.ackFlushTimer).toBeNull()
      socket._enqueueAck(5)
      socket.close()
      expect(socket.ackFlushTimer).toBeNull()
      await vi.advanceTimersByTimeAsync(1000)
      expect(ackFrames(boundary)).toEqual([])
    })
  })
}
