import { onCLS, onFCP, onFID, onLCP, onTTFB } from 'web-vitals';

const REPORT_URL = '/api/monitor/web-vitals'
const PROD_ONLY = true
const SAMPLING_RATE = 0.1;

function report(payload) {
  try {
    const body = JSON.stringify(payload)
    if (navigator.sendBeacon) {
      const blob = new Blob([body], { type: 'application/json' })
      const ok = navigator.sendBeacon(REPORT_URL, blob)
      if (ok) return
    }
    fetch(REPORT_URL, {
      method: 'POST',
      body,
      headers: { 'Content-Type': 'application/json' },
      keepalive: true,
    }).catch(() => {});
  } catch (_) { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}

function getRating(name, value) {
  switch (name) {
  case 'CLS':
    return value <= 0.1 ? 'good' : value <= 0.25 ? 'needs-improvement' : 'poor'
  case 'FID':
    return value <= 100 ? 'good' : value <= 300 ? 'needs-improvement' : 'poor'
  case 'LCP':
    return value <= 2500 ? 'good' : value <= 4000 ? 'needs-improvement' : 'poor'
  case 'FCP':
    return value <= 1800 ? 'good' : value <= 3000 ? 'needs-improvement' : 'poor'
  case 'TTFB':
    return value <= 800 ? 'good' : value <= 1800 ? 'needs-improvement' : 'poor'
  default:
    return 'unknown'
  }
}

function buildPayload(metric) {
  return {
    name: metric.name,
    value: metric.value,
    id: metric.id,
    rating: metric.rating || getRating(metric.name, metric.value),
    navigationType: metric.navigationType || 'navigate',
    page: location.pathname + location.search,
    ts: Date.now(),
    ua: navigator.userAgent,
    dev: import.meta.env.DEV,
  };
}

let initialized = false

export function initWebVitals() {
  if (initialized) return
  initialized = true

  if (PROD_ONLY && !import.meta.env.PROD) return
  if (typeof window === 'undefined') return

  if (Math.random() >= SAMPLING_RATE)
    return;

  const onUpdate = (metric) => {
    report(buildPayload(metric))
  }

  try {
    onCLS(onUpdate, { reportAllChanges: true })
    onFCP(onUpdate)
    onFID(onUpdate)
    onLCP(onUpdate)
    onTTFB(onUpdate)
  } catch (e) {
    console.warn('[web-vitals] init failed:', e)
  }
}

export default initWebVitals
