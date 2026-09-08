const STORAGE_KEY_USER = 'journey:userId';
const STORAGE_KEY_SESSION = 'journey:sessionId'
const STORAGE_KEY_HISTORY = 'journey:history'
const QUEUE_KEY = 'journey:queue'
const MAX_HISTORY = 50
const MAX_QUEUE = 100
const FLUSH_INTERVAL_MS = 30 * 1000
const JOURNEY_STAGES = [
  "visit",
  "browse",
  "engaged",
  "cart_viewed",
  "checkout",
  "purchased",
  "retained"
]

function inferStage(event) {
  if (['purchased', 'order_completed'].includes(event)) return 'purchased'
  if (['checkout', 'order_created'].includes(event)) return 'checkout'
  if (['cart_viewed', 'add_to_cart'].includes(event)) return 'cart_viewed'
  if (['comment', 'share', 'like'].includes(event)) return 'engaged'
  if (['view', 'page_view', 'search'].includes(event)) return 'browse'
  return null
}

function uuid() {
  return 'xxxxxxxx-xxxx-4xxx-yxxx-xxxxxxxxxxxx'.replace(/[xy]/g, (c) => {
    const r = (Math.random() * 16) | 0
    const v = c === 'x' ? r : (r & 0x3) | 0x8
    return v.toString(16)
  })
}

function getOrCreateUserId() {
  try {
    let id = localStorage.getItem(STORAGE_KEY_USER)
    if (!id) {
      id = `v_${uuid()}`
      localStorage.setItem(STORAGE_KEY_USER, id)
    }
    return id
  } catch (_) {
    return `v_${Date.now()}`
  }
}

function getOrCreateSessionId() {
  try {
    let id = sessionStorage.getItem(STORAGE_KEY_SESSION)
    if (!id) {
      id = `s_${uuid()}`
      sessionStorage.setItem(STORAGE_KEY_SESSION, id)
    }
    return id
  } catch (_) {
    return `s_${Date.now()}`
  }
}

function loadHistory() {
  try {
    const raw = localStorage.getItem(STORAGE_KEY_HISTORY)
    return raw ? JSON.parse(raw) : []
  } catch (_) {
    return []
  }
}

function saveHistory(events) {
  try {
    localStorage.setItem(STORAGE_KEY_HISTORY, JSON.stringify(events.slice(-MAX_HISTORY)))
  } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}

function loadQueue() {
  try {
    const raw = localStorage.getItem(QUEUE_KEY)
    return raw ? JSON.parse(raw) : []
  } catch (_) {
    return []
  }
}

function saveQueue(queue) {
  try {
    localStorage.setItem(QUEUE_KEY, JSON.stringify(queue.slice(-MAX_QUEUE)))
  } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}

async function flush() {
  const queue = loadQueue()
  if (queue.length === 0) return
  try {
    const { http } = await import('@/utils/request')
    await http.post('/api/customer-events/batch', { events: queue }, { _silent: true })
    saveQueue([])
  } catch (e) {
    console.debug('[journey] 事件上报失败，保留队列');
  }
}

let flushTimer = null

function startFlush() {
  if (flushTimer) return
  flushTimer = setInterval(flush, FLUSH_INTERVAL_MS)
  if (typeof window !== 'undefined') {
    window.addEventListener('beforeunload', () => {
      navigator.sendBeacon?.('/api/customer-events/batch', JSON.stringify({ events: loadQueue() }))
    })
  }
}

const journey = {
  userId: null,
  sessionId: null,
  currentStage: null,

  init() {
    this.userId = getOrCreateUserId()
    this.sessionId = getOrCreateSessionId()
    startFlush()
  },

  identify(userId, attributes = {}) {
    try {
      localStorage.setItem(STORAGE_KEY_USER, userId)
      this.userId = userId
      this.track('identify', { ...attributes, user_id: userId })
    } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
  },

  track(eventName, properties = {}) {
    const event = {
      event: eventName,
      user_id: this.userId,
      session_id: this.sessionId,
      properties,
      occurred_at: new Date().toISOString(),
      url: typeof location !== 'undefined' ? location.href : ''
    }
    const stage = inferStage(eventName)
    if (stage) {
      this.currentStage = stage
      event.stage = stage
    }

    const history = loadHistory();
    history.push({ event: eventName, ts: event.occurred_at, stage })
    saveHistory(history)

    const queue = loadQueue();
    queue.push(event)
    saveQueue(queue)
  },

  page(name, properties = {}) {
    this.track('page_view', { page: name, ...properties })
  },

  setStage(stage) {
    if (JOURNEY_STAGES.includes(stage)) this.currentStage = stage
  },

  getStage() {
    return this.currentStage
  },

  getHistory() {
    return loadHistory()
  }
}

journey.init()
export default journey
