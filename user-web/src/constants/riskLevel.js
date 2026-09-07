export const RISK_LEVEL_OPTIONS = Object.freeze([
  { value: 1, label: '低',    tagType: 'success', description: '低风险' },
  { value: 2, label: '中',    tagType: 'warning', description: '中风险' },
  { value: 3, label: '高',    tagType: 'danger',  description: '高风险' },
  { value: 4, label: '严重',  tagType: 'danger',  description: '严重风险' }
]);

const ALIAS = {
  low: 1, medium: 2, high: 3, critical: 4,
  L: 1, M: 2, H: 3, S: 4
};

const ALIAS_KEY = Object.freeze(
  Object.entries(ALIAS).reduce((acc, [k, v]) => { acc[k.toLowerCase()] = v; return acc }, {})
)

export const RISK_LEVEL_LABEL_MAP = Object.freeze(
  RISK_LEVEL_OPTIONS.reduce((acc, o) => { acc[o.value] = o.label; return acc }, {})
)

export const RISK_LEVEL_TAG_TYPE_MAP = Object.freeze(
  RISK_LEVEL_OPTIONS.reduce((acc, o) => { acc[o.value] = o.tagType; return acc }, {})
)

export const normalizeRiskLevel = (v) => {
  if (typeof v === 'number') return v
  if (typeof v === 'string') {
    const key = v.toLowerCase()
    if (key in ALIAS_KEY) return ALIAS_KEY[key]
    const n = Number(v)
    if (!Number.isNaN(n)) return n
  }
  return v
}

export const getRiskLevelLabel = (v) => {
  if (v === undefined || v === null || v === '') return '-'
  const n = normalizeRiskLevel(v)
  return RISK_LEVEL_LABEL_MAP[n] || String(v)
}

export const getRiskLevelTagType = (v) => {
  const n = normalizeRiskLevel(v)
  return RISK_LEVEL_TAG_TYPE_MAP[n] || 'info'
}

export default {
  RISK_LEVEL_OPTIONS,
  RISK_LEVEL_LABEL_MAP,
  RISK_LEVEL_TAG_TYPE_MAP,
  getRiskLevelLabel,
  getRiskLevelTagType,
  normalizeRiskLevel
}
