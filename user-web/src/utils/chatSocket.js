const STORAGE_KEY_PREFIX = 'chatSocket:lastSeq:';
const MAX_RECONNECT_DELAY_MS = 30000
const INITIAL_RECONNECT_DELAY_MS = 1000
const PING_INTERVAL_MS = 25000
const ACK_BATCH_INTERVAL_MS = 200;
const MAX_RECONNECT_ATTEMPTS_DEFAULT = 50;

export class ChatSocket {
  constructor(options) {
    this.sessionId = options.sessionId
    this.visitorId = options.visitorId
    this.visitorToken = options.visitorToken || options.token || '';
    this.channelId = options.channelId || 'default';
    this.baseURL = options.baseURL || (typeof window !== 'undefined' ? window.location.origin : '')
    this.onMessage = options.onMessage || (() => {})
    this.onAgentJoined = options.onAgentJoined || (() => {})
    this.onSessionClosed = options.onSessionClosed || (() => {})
    this.onWelcome = options.onWelcome || (() => {})
    this.onOfflineMessages = options.onOfflineMessages || (() => {})
    this.onAITyping = options.onAITyping || (() => {})
    this.onError = options.onError || (() => {})
    this.onConnected = options.onConnected || (() => {})
    this.onDisconnected = options.onDisconnected || (() => {})
    this.onMaxAttempts = options.onMaxAttempts || (() => {})

    this.ws = null
    this.pingTimer = null
    this.reconnectTimer = null
    this.reconnectDelay = INITIAL_RECONNECT_DELAY_MS
    this.connected = false
    this.shouldReconnect = true
    this.reconnectAttempts = 0
    this.maxReconnectAttempts = options.maxReconnectAttempts || MAX_RECONNECT_ATTEMPTS_DEFAULT

    this.lastSeq = this._loadLastSeq();
    this.seenSeqs = new Set();

    this.pendingAcks = new Set();
    this.ackFlushTimer = null

    this.frameBuffer = new Map();
    this.maxBufferSize = options.maxBufferSize || 500;
  }

  _storageKey() {
    return `${STORAGE_KEY_PREFIX}${this.sessionId}:${this.visitorId}`
  }

  _loadLastSeq() {
    try {
      if (typeof sessionStorage === 'undefined') return 0
      const v = sessionStorage.getItem(this._storageKey())
      return v ? Number(v) || 0 : 0
    } catch {
      return 0
    }
  }

  _saveLastSeq(seq) {
    if (seq <= this.lastSeq) return
    this.lastSeq = seq
    try {
      if (typeof sessionStorage !== 'undefined') {
        sessionStorage.setItem(this._storageKey(), String(seq))
      }
    } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
  }

  connect() {
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) {
      return
    }

    const protocol = this.baseURL.startsWith('https') ? 'wss' : 'ws'
    const host = this.baseURL.replace(/^https?:\/\//, '')
    const sinceSeqPart = this.lastSeq > 0 ? `&since_seq=${this.lastSeq}` : '';
    const tokenPart = this.visitorToken ? `&visitor_token=${encodeURIComponent(this.visitorToken)}` : ''
    const url = `${protocol}://${host}/api/ws/visitor?session_id=${encodeURIComponent(this.sessionId)}&visitor_id=${encodeURIComponent(this.visitorId)}&channel_id=${encodeURIComponent(this.channelId)}${sinceSeqPart}${tokenPart}`

    try {
      this.ws = new WebSocket(url)
    } catch (err) {
      console.error('[ChatSocket] 创建失败：', err)
      this.onError(err)
      this.scheduleReconnect()
      return
    }

    this.seenSeqs.clear();

    this.ws.onopen = () => {
      this.connected = true
      this.reconnectDelay = INITIAL_RECONNECT_DELAY_MS
      this.reconnectAttempts = 0;
      this.onConnected()
      this.startPing()
      this._flushAcks(true);
      this.postConnectRecovery();
    }

    this.ws.onmessage = (event) => {
      try {
        const msg = JSON.parse(event.data)
        this.handleMessage(msg)
      } catch (err) {
        console.warn('[ChatSocket] 消息解析失败：', err)
      }
    }

    this.ws.onerror = (err) => {
      console.warn('[ChatSocket] 连接错误：', err)
      this.onError(err)
    }

    this.ws.onclose = (event) => {
      this.connected = false
      this.stopPing()
      this._flushAcks(true);
      if (event.code === 1006 || event.code === 4001) {
        this._tryRefreshTokenAndReconnect()
      } else {
        this.onDisconnected(event)
        if (this.shouldReconnect) {
          this.scheduleReconnect()
        }
      }
    }
  }

  async _tryRefreshTokenAndReconnect() {
    // 1006（服务器重启/网络闪断）在访客侧最常见：浏览器无 close 帧，
    // 与"token 失效"无关。访客端没有 agent token，不能因缺少它而放弃重连
    // （否则聊天窗口永久冻结，访客以为客服不理人）；有 agent token 的坐席端
    // 才走 token 刷新路径。访客侧走普通指数退避重连，resume 依赖 since_seq 续传。
    const hasAgentToken = !!localStorage.getItem('token');
    if (!hasAgentToken) {
      if (this.shouldReconnect) {
        this.scheduleReconnect()
      } else {
        this.onError(new Error('auth_required'))
      }
      return
    }
    try {
      const { http } = await import('@/utils/request')
      const resp = await http.post('/api/auth/refresh-token', undefined, { _silent: true })
      if (resp && resp.token) {
        localStorage.setItem('token', resp.token)
        console.info('[ChatSocket] token 已刷新，重连')
        this.connect()
      }
    } catch (err) {
      console.warn('[ChatSocket] token 刷新失败，需重新登录', err)
      this.onError(new Error('auth_required'))
    }
  }

  handleMessage(msg) {
    const { type, payload, seq } = msg
    if (typeof seq === 'number' && seq > 0) {
      if (this.seenSeqs.has(seq)) {
        return
      }
      this.seenSeqs.add(seq)
      if (seq > this.lastSeq) {
        this._saveLastSeq(seq)
      }
      this._bufferFrame(seq, msg);
      if (this._isAckable(type)) {
        this._enqueueAck(seq)
      }
    }

    switch (type) {
      case 'welcome':
        this.onWelcome(payload)
        break
      case 'offline_messages':
        this.onOfflineMessages(payload);
        break
      case 'message':
        this.onMessage(payload)
        break
      case 'agent_joined':
        this.onAgentJoined(payload)
        break
      case 'session_closed':
        this.onSessionClosed(payload)
        break
      case 'ai_typing':
        this.onAITyping(payload);
        break
      case 'missed_ack':
        if (payload && Array.isArray(payload.pending)) {
          payload.pending.forEach((s) => this._enqueueAck(s))
        }
        break
      case 'pong':
        break;
      case 'error':
        this.onError(payload)
        break
      default:
        console.warn('[ChatSocket] 未知消息类型：', type)
    }
  }

  _isAckable(type) {
    if (!type) return false
    return type !== 'pong' && type !== 'error' && type !== 'welcome' && type !== 'missed_ack';
  }

  _bufferFrame(seq, frame) {
    if (this.frameBuffer.size >= this.maxBufferSize) {
      const oldestSeq = Math.min(...this.frameBuffer.keys());
      this.frameBuffer.delete(oldestSeq)
    }
    this.frameBuffer.set(seq, frame)
  }

  flushBuffer() {
    if (this.frameBuffer.size === 0) return
    const sorted = Array.from(this.frameBuffer.keys()).sort((a, b) => a - b)
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return
    try {
      this.ws.send(JSON.stringify({
        type: 'recovery',
        last_seq: this.lastSeq,
        frames: sorted
      }))
      console.debug(`[ChatSocket] flushBuffer sent ${sorted.length} frames`)
    } catch (err) {
      console.warn('[ChatSocket] flushBuffer 失败：', err)
    }
  }

  postConnectRecovery() {
    this.resume()
    this.flushBuffer()
  }

  _enqueueAck(seq) {
    if (typeof seq !== 'number' || seq <= 0) return
    this.pendingAcks.add(seq)
    if (this.ackFlushTimer) return
    this.ackFlushTimer = setTimeout(() => this._flushAcks(false), ACK_BATCH_INTERVAL_MS)
  }

  _flushAcks(immediate) {
    if (this.ackFlushTimer) {
      clearTimeout(this.ackFlushTimer)
      this.ackFlushTimer = null
    }
    if (this.pendingAcks.size === 0) return
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      if (immediate)
        { /* no-op */ }
      return
    }
    const seqs = Array.from(this.pendingAcks).sort((a, b) => a - b)
    this.pendingAcks.clear()
    try {
      this.ws.send(JSON.stringify({ type: 'ack', seq: seqs }))
    } catch (err) { console.warn("[request] 后台调用失败(已忽略):", err) }
  }

  ackDelivered(messageIds) {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return
    if (!messageIds || messageIds.length === 0) return
    try {
      this.ws.send(JSON.stringify({ type: 'delivered', payload: { ids: messageIds } }))
    } catch (err) { console.warn("[request] 后台调用失败(已忽略):", err) }
  }

  resume() {
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) return
    try {
      this.ws.send(JSON.stringify({ type: 'resume', since_seq: this.lastSeq }))
    } catch (err) { console.warn("[request] 后台调用失败(已忽略):", err) }
  }

  startPing() {
    this.stopPing()
    this.pingTimer = setInterval(() => {
      if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        try { this.ws.send(JSON.stringify({ type: 'ping' })) } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
      }
    }, PING_INTERVAL_MS)
  }

  stopPing() {
    if (this.pingTimer) {
      clearInterval(this.pingTimer)
      this.pingTimer = null
    }
  }

  scheduleReconnect() {
    if (this.reconnectAttempts >= this.maxReconnectAttempts) {
      this._emitMaxAttempts()
      return
    }
    if (this.reconnectTimer) return
    if (!this.shouldReconnect) return
    this.reconnectAttempts++
    const baseDelay = Math.min(this.reconnectDelay * Math.min(this.reconnectAttempts, 5), MAX_RECONNECT_DELAY_MS);
    const delay = Math.floor(baseDelay * (0.5 + Math.random() * 0.5))
    this.reconnectTimer = setTimeout(() => {
      this.reconnectTimer = null
      if (this.shouldReconnect) {
        this.connect()
      }
    }, delay)
  }

  _emitMaxAttempts() {
    console.warn(`[ChatSocket] 已达最大重连次数 ${this.maxReconnectAttempts}，停止重连`)
    this.onMaxAttempts(this.reconnectAttempts)
    this.onError(new Error(`重连失败：已达最大尝试次数 ${this.maxReconnectAttempts}`))
  }

  close() {
    this.shouldReconnect = false
    this.stopPing()
    if (this.reconnectTimer) {
      clearTimeout(this.reconnectTimer)
      this.reconnectTimer = null
    }
    if (this.ackFlushTimer) {
      clearTimeout(this.ackFlushTimer)
      this.ackFlushTimer = null
    }
    if (this.ws) {
      try {
        if (this.ws.readyState === WebSocket.OPEN) {
          this.ws.send(JSON.stringify({ type: 'close' }))
        }
      } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
      try { this.ws.close() } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
      this.ws = null
    }
  }
}

export default ChatSocket
