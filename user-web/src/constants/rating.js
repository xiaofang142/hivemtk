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
  { value: 'bad',      label: '不相关', tagType: 'danger',  description: '不相关' },
  // 历史 e2e 数据用的是 2~5 星评分（Playground 三态之外的旧口径），
  // 这里补上映射，避免反馈列表把 5 直接渲染成数字「5」。
  { value: 5, label: '5 星', tagType: 'success', description: '历史星级评价' },
  { value: 4, label: '4 星', tagType: 'success', description: '历史星级评价' },
  { value: 3, label: '3 星', tagType: 'info',    description: '历史星级评价' },
  { value: 2, label: '2 星', tagType: 'danger',  description: '历史星级评价' }
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
