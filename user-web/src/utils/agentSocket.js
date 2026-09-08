import { getApiConfig as getConfig } from './configManager'

const STORAGE_KEY_PREFIX = 'agentSocket:lastSeq:';
const MAX_RECONNECT_ATTEMPTS_DEFAULT = 10
const INITIAL_RECONNECT_DELAY_MS = 2000
const MAX_RECONNECT_DELAY_MS = 30000
const PING_INTERVAL_MS = 25000
const ACK_BATCH_INTERVAL_MS = 200

export default class AgentSocket {
  constructor(agentId, agentName, options = {}) {
    this.agentId = agentId
    this.agentName = agentName
    this.options = options
    this.ws = null
    this.connected = false
    this.shouldReconnect = true
    this.reconnectAttempts = 0
    this.maxReconnectAttempts = options.maxReconnectAttempts || MAX_RECONNECT_ATTEMPTS_DEFAULT
    this.reconnectDelay = options.reconnectDelay || INITIAL_RECONNECT_DELAY_MS
    this.pingInterval = null

    this.onNewSession = options.onNewSession || (() => {})
    this.onNewMessage = options.onNewMessage || (() => {})
    this.onSessionUpdate = options.onSessionUpdate || (() => {})
    this.onAISuggestion = options.onAISuggestion || (() => {})
    this.onConnected = options.onConnected || (() => {})
    this.onDisconnected = options.onDisconnected || (() => {})
    this.onError = options.onError || (() => {})

    this.lastSeq = this._loadLastSeq();
    this.seenSeqs = new Set()
    this.pendingAcks = new Set()
    this.ackFlushTimer = null
  }

  _storageKey() {
    return `${STORAGE_KEY_PREFIX}${this.agentId}`
  }

  _loadLastSeq() {
    try {
      if (typeof localStorage === 'undefined') return 0
      const v = localStorage.getItem(this._storageKey())
      return v ? Number(v) || 0 : 0
    } catch {
      return 0
    }
  }

  _saveLastSeq(seq) {
    if (seq <= this.lastSeq) return
    this.lastSeq = seq
    try {
      if (typeof localStorage !== 'undefined') {
        localStorage.setItem(this._storageKey(), String(seq))
      }
    } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
  }

  buildWsUrl() {
    const cfg = getConfig()
    const base = cfg.baseUrl || ''
    const proto = location.protocol === 'https:' ? 'wss:' : 'ws:'
    const url = new URL('/api/ws/agent', base || location.origin)
    url.protocol = proto
    if (this.agentId != null) url.searchParams.set('agent_id', String(this.agentId))
    if (this.agentName) url.searchParams.set('agent_name', this.agentName)
    const token = localStorage.getItem('token')
    if (token) url.searchParams.set('token', token)
    const sticky = this._deriveSticky();
    if (sticky) url.searchParams.set('node_id', sticky)
    return url.toString()
  }

  _deriveSticky() {
    if (!this.agentId) return null
    let h = 0
    const s = String(this.agentId)
    for (let i = 0; i < s.length; i++) h = (h * 31 + s.charCodeAt(i)) | 0
    const nodeId = `n${(h % 100).toString().padStart(2, '0')}`;
    try { sessionStorage.setItem('agentSocket:stickyNodeId', nodeId) } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
    return nodeId
  }

  connect() {
    if (this.ws && (this.ws.readyState === WebSocket.OPEN || this.ws.readyState === WebSocket.CONNECTING)) return
    let ws
    try {
      ws = new WebSocket(this.buildWsUrl())
    } catch (e) {
      this.onError(e)
      this.scheduleReconnect()
      return
    }
    this.ws = ws

    this.seenSeqs.clear();

    ws.onopen = () => {
      this.connected = true
      this.reconnectAttempts = 0
      this.reconnectDelay = INITIAL_RECONNECT_DELAY_MS
      this.startPing()
      this._flushAcks(true);
      this.onConnected()
    }

    ws.onmessage = (event) => {
      let msg
      try {
        msg = JSON.parse(event.data)
      } catch (e) {
        console.warn('[AgentSocket] 无法解析消息:', event.data)
        return
      }
      this.handleMessage(msg)
    }

    ws.onerror = (event) => {
      this.onError(event)
    }

    ws.onclose = (event) => {
      this.connected = false
      this.stopPing()
      this._flushAcks(true);
      this.onDisconnected(event)
      if (this.shouldReconnect) {
        this.scheduleReconnect()
      }
    }
  }

  handleMessage(msg) {
    const event = msg.event || msg.type;
    const payload = msg.payload || msg.data || msg
    const seq = msg.seq

    if (typeof seq === 'number' && seq > 0) {
      if (this.seenSeqs.has(seq)) {
        return
      }
      this.seenSeqs.add(seq)
      if (seq > this.lastSeq) {
        this._saveLastSeq(seq)
      }
      if (event !== 'pong' && event !== 'error' && event !== 'heartbeat') {
        this._enqueueAck(seq)
      }
    }

    switch (event) {
      case 'new_session':
        this.onNewSession(payload)
        break
      case 'new_message':
        this.onNewMessage(payload)
        break
      case 'session_update':
        this.onSessionUpdate(payload)
        break
      case 'ai_suggestion':
        this.onAISuggestion(payload)
        break
      case 'pong':
      case 'heartbeat':
        break;
      default:
        console.debug('[AgentSocket] 未处理事件:', event)
    }
  }

  _enqueueAck(seq) {
    if (typeof seq !== 'number' || seq <= 0) return
    this.pendingAcks.add(seq)
    if (this.ackFlushTimer) return
    this.ackFlushTimer = setTimeout(() => this._flushAcks(false), ACK_BATCH_INTERVAL_MS)
  }

  _flushAcks(_immediate) {
    if (this.ackFlushTimer) {
      clearTimeout(this.ackFlushTimer)
      this.ackFlushTimer = null
    }
    if (this.pendingAcks.size === 0) return
    if (!this.ws || this.ws.readyState !== WebSocket.OPEN) {
      return
    }
    const seqs = Array.from(this.pendingAcks).sort((a, b) => a - b)
    this.pendingAcks.clear()
    try {
      this.ws.send(JSON.stringify({ type: 'ack', seq: seqs }))
    } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
  }

  startPing() {
    this.stopPing()
    this.pingInterval = setInterval(() => {
      if (this.ws && this.ws.readyState === WebSocket.OPEN) {
        try { this.ws.send(JSON.stringify({ event: 'ping' })) } catch (e) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
      }
    }, PING_INTERVAL_MS)
  }

  stopPing() {
    if (this.pingInterval) {
      clearInterval(this.pingInterval)
      this.pingInterval = null
    }
  }

  scheduleReconnect() {
    if (this.reconnectAttempts >= this.maxReconnectAttempts) return
    this.reconnectAttempts++
    const baseDelay = Math.min(this.reconnectDelay * Math.min(this.reconnectAttempts, 5), MAX_RECONNECT_DELAY_MS);
    const delay = Math.floor(baseDelay * (0.5 + Math.random() * 0.5))
    setTimeout(() => {
      if (this.shouldReconnect) this.connect()
    }, delay)
  }

  getReconnectAttempts() {
    return this.reconnectAttempts
  }

  close() {
    this.shouldReconnect = false
    this.stopPing()
    if (this.ackFlushTimer) {
      clearTimeout(this.ackFlushTimer)
      this.ackFlushTimer = null
    }
    if (this.ws) {
      try { this.ws.close() } catch (e) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
      this.ws = null
    }
  }
}
