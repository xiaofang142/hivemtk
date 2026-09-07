export const PRIORITY_OPTIONS = Object.freeze([
  { value: 'urgent', label: '紧急',   tagType: 'danger',  sort: 1 },
  { value: 'high',   label: '高',     tagType: 'warning', sort: 2 },
  { value: 'medium', label: '中',     tagType: 'info',        sort: 3 },
  { value: 'normal', label: '普通',   tagType: 'info',    sort: 4 },
  { value: 'low',    label: '低',     tagType: 'info',    sort: 5 },
  {
    value: 1,
    label: '紧急',
    tagType: 'danger',
    sort: 1
  },
  { value: 2, label: '高',   tagType: 'warning', sort: 2 },
  { value: 3, label: '中',   tagType: 'info',        sort: 3 },
  { value: 4, label: '低',   tagType: 'info',    sort: 4 }
]);

export const PRIORITY_LABEL_MAP = Object.freeze(
  PRIORITY_OPTIONS.reduce((acc, o) => { acc[o.value] = o.label; return acc }, {})
)

export const PRIORITY_TAG_TYPE_MAP = Object.freeze(
  PRIORITY_OPTIONS.reduce((acc, o) => { acc[o.value] = o.tagType; return acc }, {})
)

export const getPriorityLabel = (v) => {
  if (v === undefined || v === null || v === '') return '-'
  return PRIORITY_LABEL_MAP[v] || String(v)
}

export const getPriorityTagType = (v) => PRIORITY_TAG_TYPE_MAP[v] || 'info'

export default {
  PRIORITY_OPTIONS,
  PRIORITY_LABEL_MAP,
  PRIORITY_TAG_TYPE_MAP,
  getPriorityLabel,
  getPriorityTagType
}
