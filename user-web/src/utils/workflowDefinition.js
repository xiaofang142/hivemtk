// FA-WF-09：工作流定义提交前的确定性序列化（普通保存与另存共用）。
// 画布节点把动作配置编辑在 config_text（字符串），后端定义只认 config（对象，含
// action_type）。两条保存路径必须走同一个函数，避免「另存」漏序列化被后端拒。
export function buildPayloadDefinition(definition) {
  const rawNodes = Array.isArray(definition?.nodes) ? definition.nodes : []
  const rawEdges = Array.isArray(definition?.edges) ? definition.edges : []

  const nodes = rawNodes.map((n) => {
    let config = {}
    if (n.config_text) {
      try {
        config = JSON.parse(n.config_text)
      } catch (e) {
        const err = new Error(`节点 ${n.id} 的配置不是合法 JSON: ${e.message}`)
        err.nodeId = n.id
        throw err
      }
    }
    return {
      id: n.id,
      type: n.type,
      name: n.label || n.name || n.id,
      label: n.label || '',
      x: n.x || 0,
      y: n.y || 0,
      config,
    }
  })

  const edges = rawEdges.map((e) => ({
    source: e.source || e.from || '',
    target: e.target || e.to || '',
    label: e.label || '',
  }))

  return { nodes, edges }
}

export default buildPayloadDefinition
