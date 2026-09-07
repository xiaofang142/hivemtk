export const AI_AGENT_TYPE_OPTIONS = Object.freeze([
  { value: 'sales',           label: '销售',         tagType: 'success', description: '销售型智能体（销冠话术）' },
  { value: 'customer_service', label: '客服',         tagType: 'warning', description: '客服型智能体（应答服务）' },
  { value: 'hybrid',          label: '混合',         tagType: 'primary', description: '混合型智能体（销售+客服）' },
  { value: 'marketing',       label: '营销',         tagType: 'info',        description: '营销型智能体（私域运营）' },
  { value: 'support',         label: '支持',         tagType: 'info',    description: '技术支持型智能体' }
]);

export const AI_AGENT_TYPE_LABEL_MAP = Object.freeze(
  AI_AGENT_TYPE_OPTIONS.reduce((acc, o) => { acc[o.value] = o.label; return acc }, {})
)

export const AI_AGENT_TYPE_TAG_TYPE_MAP = Object.freeze(
  AI_AGENT_TYPE_OPTIONS.reduce((acc, o) => { acc[o.value] = o.tagType; return acc }, {})
)

export const getAiAgentTypeLabel = (v) => {
  if (v === undefined || v === null || v === '') return '-'
  return AI_AGENT_TYPE_LABEL_MAP[v] || String(v)
}

export const getAiAgentTypeTagType = (v) => AI_AGENT_TYPE_TAG_TYPE_MAP[v] || 'info'

export default {
  AI_AGENT_TYPE_OPTIONS,
  AI_AGENT_TYPE_LABEL_MAP,
  AI_AGENT_TYPE_TAG_TYPE_MAP,
  getAiAgentTypeLabel,
  getAiAgentTypeTagType
}
