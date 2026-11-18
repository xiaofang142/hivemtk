// 未测必须与真实0区分；不从异步写库时间推导操作耗时。
export function durationText(value) {
  return typeof value === 'number' && Number.isFinite(value) && value >= 0 ? String(value) : '—'
}

export function spanDurationText(span) {
  if (!span || span.duration_measured !== true || !(span.timing_version > 0)) return '—'
  const duration = durationText(span.duration_ms)
  return duration === '—' ? duration : `${duration} ms`
}

export function operationTime(span) {
  return span?.started_at || null
}

export function buildTraceQuery(form) {
  const params = {}
  for (const key of ['trace_id', 'turn_id', 'conversation_id', 'msg_id', 'channel', 'account_id']) {
    const value = form?.[key]
    if (typeof value === 'string' && value.trim()) params[key] = value.trim()
  }
  return params
}

function attemptKey(span) {
  return JSON.stringify([
    span.tenant_id, span.channel, span.account_id, span.conversation_id,
    span.turn_id || `legacy:${span.trace_id || ''}`, span.answer_attempt || 0
  ])
}

// 多个同身份dispatch无法唯一定位父节点时，不猜挂载；子节点仍可见而不会重复显示。
export function flattenTraceSpans(spans) {
  if (!Array.isArray(spans)) return []
  const lifecycles = spans.filter(span => !span.span_kind || span.span_kind === 'lifecycle')
  const dispatches = lifecycles.filter(span => span.node === 'ai_dispatch')
  const counts = new Map()
  for (const span of dispatches) {
    const key = attemptKey(span)
    counts.set(key, (counts.get(key) || 0) + 1)
  }
  const emitted = new Set()
  const out = []
  for (const span of lifecycles) {
    out.push(span)
    if (span.node !== 'ai_dispatch' || counts.get(attemptKey(span)) !== 1) continue
    const children = spans.filter(child => child.parent_node === 'ai_dispatch' && attemptKey(child) === attemptKey(span))
    const turns = children.filter(child => child.span_kind === 'agent_turn')
      .sort((a, b) => (a.turn_index || 0) - (b.turn_index || 0) || (a.id || 0) - (b.id || 0))
    for (const turn of turns) {
      out.push({ _turn: true, ti: turn.turn_index, turn })
      emitted.add(turn)
      // 同工具轮若有多个agent父节点仍不猜，稍后作为未关联节点保留。
      if (turns.filter(candidate => candidate.turn_index === turn.turn_index).length !== 1) continue
      const tools = children.filter(child => child.span_kind === 'tool_call' && child.turn_index === turn.turn_index)
        .sort((a, b) => (a.id || 0) - (b.id || 0))
      for (const tool of tools) {
        out.push({ _tool: true, tool })
        emitted.add(tool)
      }
    }
  }
  for (const span of spans) {
    if (emitted.has(span)) continue
    if (span.span_kind === 'agent_turn') out.push({ _turn: true, ti: span.turn_index, turn: span })
    if (span.span_kind === 'tool_call') out.push({ _tool: true, tool: span })
  }
  return out
}
