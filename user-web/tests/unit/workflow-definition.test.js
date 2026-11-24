// FA-WF-09：普通保存与另存共用的确定性序列化函数单元测试。
import { describe, it, expect } from 'vitest'
import { buildPayloadDefinition } from '../../src/utils/workflowDefinition.js'

describe('buildPayloadDefinition (FA-WF-09)', () => {
  it('config_text → config，保留 action_type 等多字段', () => {
    const def = {
      nodes: [{
        id: 'n1', type: 'action', label: '打日志',
        config_text: '{"action_type":"log","message":"ok","retries":3}'
      }],
      edges: [{ source: 'n1', target: 'n2', label: '' }]
    }
    const out = buildPayloadDefinition(def)
    expect(out.nodes[0].config).toEqual({ action_type: 'log', message: 'ok', retries: 3 })
    expect(out.nodes[0].name).toBe('打日志')
    expect(out.nodes[0].type).toBe('action')
    expect(out.edges[0]).toEqual({ source: 'n1', target: 'n2', label: '' })
  })

  it('非法 JSON 抛错并带节点信息', () => {
    const def = { nodes: [{ id: 'bad-node', type: 'action', config_text: '{not json' }], edges: [] }
    let err
    try { buildPayloadDefinition(def) } catch (e) { err = e }
    expect(err).toBeTruthy()
    expect(err.message).toContain('bad-node')
    expect(err.message).toContain('JSON')
  })

  it('空 config_text → config={}', () => {
    const def = { nodes: [{ id: 'n1', type: 'trigger' }], edges: [] }
    const out = buildPayloadDefinition(def)
    expect(out.nodes[0].config).toEqual({})
  })

  it('edges 兼容 from/to 与默认值', () => {
    const def = { nodes: [], edges: [{ from: 'a', to: 'b' }, {}] }
    const out = buildPayloadDefinition(def)
    expect(out.edges[0]).toEqual({ source: 'a', target: 'b', label: '' })
    expect(out.edges[1]).toEqual({ source: '', target: '', label: '' })
  })

  it('name 回退：label → name → id，坐标默认 0', () => {
    const def = { nodes: [
      { id: 'a', name: '仅name' },
      { id: 'b', label: '有label', name: '被覆盖' },
      { id: 'c' }
    ], edges: [] }
    const out = buildPayloadDefinition(def)
    expect(out.nodes[0].name).toBe('仅name')
    expect(out.nodes[1].name).toBe('有label')
    expect(out.nodes[2].name).toBe('c')
    expect(out.nodes[0].x).toBe(0)
    expect(out.nodes[0].y).toBe(0)
  })

  it('definition 为空/非数组时安全回退', () => {
    expect(buildPayloadDefinition(undefined)).toEqual({ nodes: [], edges: [] })
    expect(buildPayloadDefinition({ nodes: 'bad', edges: null })).toEqual({ nodes: [], edges: [] })
  })
})
