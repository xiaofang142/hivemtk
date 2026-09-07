export const INTENT_TYPE_OPTIONS = Object.freeze([
  { value: 'purchase',      label: '购买意向',  tagType: 'success', description: '客户有购买意向' },
  { value: 'price_inquiry', label: '询价',      tagType: 'primary', description: '客户在询价' },
  { value: 'churn',         label: '流失意向',  tagType: 'danger',  description: '客户有流失倾向' },
  { value: 'complaint',     label: '投诉',      tagType: 'danger',  description: '客户投诉' },
  { value: 'greeting',      label: '问候',      tagType: 'info',    description: '寒暄/问候' },
  { value: 'support',       label: '售后',      tagType: 'info',        description: '售后服务' },
  { value: 'other',         label: '其他',      tagType: 'info',    description: '其他意图' }
]);

export const INTENT_TYPE_LABEL_MAP = Object.freeze(
  INTENT_TYPE_OPTIONS.reduce((acc, o) => { acc[o.value] = o.label; return acc }, {})
)

export const INTENT_TYPE_TAG_TYPE_MAP = Object.freeze(
  INTENT_TYPE_OPTIONS.reduce((acc, o) => { acc[o.value] = o.tagType; return acc }, {})
)

export const getIntentTypeLabel = (v) => {
  if (v === undefined || v === null || v === '') return '-'
  return INTENT_TYPE_LABEL_MAP[v] || String(v)
}

export const getIntentTypeTagType = (v) => INTENT_TYPE_TAG_TYPE_MAP[v] || 'info'

export default {
  INTENT_TYPE_OPTIONS,
  INTENT_TYPE_LABEL_MAP,
  INTENT_TYPE_TAG_TYPE_MAP,
  getIntentTypeLabel,
  getIntentTypeTagType
}
