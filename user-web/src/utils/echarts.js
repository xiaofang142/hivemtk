import * as echarts from 'echarts';

export function safeInit(el, theme, opts) {
  if (!el) return null
  const existing = echarts.getInstanceByDom(el)
  if (existing) return existing
  return echarts.init(el, theme, opts)
}

export function safeDispose(inst) {
  if (!inst || typeof inst.dispose !== 'function') return
  try {
    if (!inst.isDisposed()) inst.dispose()
  } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
}

export { echarts }
export default { safeInit, safeDispose, echarts }
