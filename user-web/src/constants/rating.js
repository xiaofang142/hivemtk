export const RATING_OPTIONS = Object.freeze([
  { value: 1,  label: '相关',     tagType: 'success', description: '用户认为相关' },
  { value: 0,  label: '一般',     tagType: 'info',    description: '用户认为一般' },
  { value: -1, label: '不相关',   tagType: 'danger',  description: '用户认为不相关' },
  {
    value: 'good',
    label: '相关',
    tagType: 'success',
    description: '相关'
  },
  { value: 'neutral',  label: '一般',   tagType: 'info',    description: '一般' },
  { value: 'bad',      label: '不相关', tagType: 'danger',  description: '不相关' }
]);

export const RATING_LABEL_MAP = Object.freeze(
  RATING_OPTIONS.reduce((acc, o) => { acc[o.value] = o.label; return acc }, {})
)

export const RATING_TAG_TYPE_MAP = Object.freeze(
  RATING_OPTIONS.reduce((acc, o) => { acc[o.value] = o.tagType; return acc }, {})
)

export const getRatingLabel = (v) => {
  if (v === undefined || v === null || v === '') return '-'
  return RATING_LABEL_MAP[v] || String(v)
}

export const getRatingTagType = (v) => RATING_TAG_TYPE_MAP[v] || 'info'

export default {
  RATING_OPTIONS,
  RATING_LABEL_MAP,
  RATING_TAG_TYPE_MAP,
  getRatingLabel,
  getRatingTagType
}
