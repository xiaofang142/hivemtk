import { describe, expect, it } from 'vitest'
import { readFileSync } from 'node:fs'
import { durationText, spanDurationText, operationTime, flattenTraceSpans, buildTraceQuery } from '../../src/utils/traceTiming'

const component = readFileSync('src/views/system/TraceMonitor.vue', 'utf8')

describe('追踪耗时与生成尝试展示', () => {
  it('未测、非法与真实零毫秒区别展示', () => {
    expect(spanDurationText({ duration_ms: 0 })).toBe('—')
    expect(spanDurationText({ duration_ms: 0, duration_measured: true, timing_version: 1 })).toBe('0 ms')
    expect(spanDurationText({ duration_ms: -1, duration_measured: true, timing_version: 1 })).toBe('—')
    expect(durationText(null)).toBe('—')
    expect(durationText('0')).toBe('—')
    expect(durationText(NaN)).toBe('—')
    expect(operationTime({ created_at: 'write-time' })).toBeNull()
  })

  it('同一智能体轮号在两次生成中不能混挂，孤立工具保留可见', () => {
    const make = (id, kind, attempt, extra = {}) => ({ id, tenant_id: 18, channel: 'douyin', account_id: 'a', conversation_id: 'c', turn_id: 'turn', answer_attempt: attempt, span_kind: kind, parent_node: kind === 'lifecycle' ? '' : 'ai_dispatch', turn_index: 1, ...extra })
    const spans = [make(1, 'lifecycle', 1, { node: 'ai_dispatch' }), make(2, 'agent_turn', 1), make(3, 'tool_call', 1), make(4, 'lifecycle', 2, { node: 'ai_dispatch' }), make(5, 'agent_turn', 2), make(6, 'tool_call', 2), make(7, 'tool_call', 3)]
    const out = flattenTraceSpans(spans)
    expect(out.map(item => item.id || item.turn?.id || item.tool?.id)).toEqual([1, 2, 3, 4, 5, 6, 7])
    expect(spans).toHaveLength(7)
    expect(flattenTraceSpans(null)).toEqual([])
  })

  it('重复父节点不猜关联，不丢掉或重复显示子节点', () => {
    const spans = [{ id: 1, node: 'ai_dispatch', span_kind: 'lifecycle' }, { id: 2, node: 'ai_dispatch', span_kind: 'lifecycle' }, { id: 3, span_kind: 'tool_call', parent_node: 'ai_dispatch' }]
    expect(flattenTraceSpans(spans).map(item => item.id || item.tool?.id)).toEqual([1, 2, 3])
  })
  it('节点耗时使用显式实测状态，不能把缺测零显示成0ms', () => {
    expect(component).not.toContain('{{ item.duration_ms }} ms')
    expect(component).not.toContain('{{ item.turn.duration_ms }} ms')
    expect(component).not.toContain('{{ item.tool.duration_ms }} ms')
    expect(component).toContain('spanDurationText(')
  })

  it('工具与智能体按轮次和生成尝试归并，不跨重生成重复挂载', () => {
    expect(component).toContain('flattenTraceSpans(')
    expect(component).not.toContain("t.spans.find(s => s.span_kind === 'agent_turn'")
  })
  it('从分轮总览下钻必须携带轮次和账号，不能再次落到最近另一轮', () => {
    expect(component.includes('form.turn_id = row.turn_id')).toBe(true)
    expect(component.includes('form.account_id = row.account_id')).toBe(true)
    expect(component.includes('buildTraceQuery(form)')).toBe(true)
    expect(buildTraceQuery({ trace_id: ' session ', turn_id: 'turn-one', account_id: 'account', channel: 'douyin', msg_id: '', arbitrary: 'ignored' })).toEqual({ trace_id: 'session', turn_id: 'turn-one', account_id: 'account', channel: 'douyin' })
    expect(buildTraceQuery(null)).toEqual({})
  })
})
