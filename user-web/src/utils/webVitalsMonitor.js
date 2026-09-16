// 说明：web-vitals v4 已把 onFID 标为 deprecated（用 onINP 取代），
// 这里采集 LCP / FCP / CLS / TTFB / INP —— 不再单独采 FID，避免重复与弃用告警。
import { onLCP, onCLS, onFCP, onTTFB, onINP } from 'web-vitals';

const REPORT_URL = '/api/monitor/web-vitals'

// 匿名会话 ID：同一标签页内多条指标归为一组，便于按会话聚合。
// 用 sessionStorage 而非 localStorage —— 关闭标签页即失效，符合"会话"语义，
// 也不会跨设备长期追踪用户。
const SESSION_KEY = 'hivemtk-metrics-session'
function getSessionId() {
  try {
    let sid = sessionStorage.getItem(SESSION_KEY)
    if (!sid) {
      sid = (typeof crypto !== 'undefined' && crypto.randomUUID)
        ? crypto.randomUUID()
        : `s-${Date.now()}-${Math.random().toString(36).slice(2, 10)}`
      sessionStorage.setItem(SESSION_KEY, sid)
    }
    return sid
  } catch {
    return ''
  }
}

function getAuthHeader() {
  try {
    const token = localStorage.getItem('token')
    return token ? { Authorization: `Bearer ${token}` } : {}
  } catch {
    return {}
  }
}

/**
 * 上报一条指标。
 *
 * 传输方式刻意**不用** navigator.sendBeacon：
 *   端点 /api/monitor/web-vitals 注册在 JWT 之后（router/business_routes.go 的 auth 分组），
 *   而 sendBeacon 无法自定义请求头，必然 401。
 *   改用 fetch + keepalive —— 同样能在页面卸载后完成发送，但可以携带 Authorization。
 *   拿不到凭据（未登录 / 无 fetch）时直接放弃，避免制造一批必然 401 的噪声。
 */
const post = (endpoint, payload) => {
  if (typeof fetch === 'undefined') return
  const auth = getAuthHeader()
  if (!auth.Authorization) return
  fetch(endpoint, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json', ...auth },
    body: JSON.stringify(payload),
    keepalive: true,
  }).catch(() => {})
}

export const initWebVitals = (options = {}) => {
  const reportTo = options.endpoint || REPORT_URL
  const sampleRate = options.sampleRate ?? 1.0
  const debug = options.debug || false

  if (Math.random() > sampleRate) return

  const send = (metric) => {
    // 字段名必须与后端 WebVitalsController.Report 的 DTO 一致
    // （name / value / rating / page / id / session_id / ts / ua）。
    // 旧实现发的是 url / userAgent / timestamp，后端一个都收不到 ——
    // 落库的 page 与 session_id 恒为空，属"有上报但无数据"。
    const data = {
      name: metric.name,
      value: metric.value,
      rating: metric.rating,
      id: metric.id,
      page: (location.pathname || '').slice(0, 300),
      session_id: getSessionId(),
      ts: Date.now(),
      ua: navigator.userAgent,
      delta: metric.delta,
      navigationType: metric.navigationType,
    }
    if (debug) console.warn('[web-vitals][debug]', data)
    post(reportTo, data)
  }

  onLCP(send)
  onCLS(send)
  onFCP(send)
  onTTFB(send)
  onINP(send)
}

export const reportCustomMetric = (name, value, metadata = {}) => {
  post(REPORT_URL, {
    name,
    value,
    custom: true,
    ts: Date.now(),
    page: (location.pathname || '').slice(0, 300),
    session_id: getSessionId(),
    ...metadata,
  })
}

export default { initWebVitals, reportCustomMetric }
