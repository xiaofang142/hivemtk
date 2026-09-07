export const TREND_OPTIONS = Object.freeze([
  { value: 'up',     label: '上升', tagType: 'success', description: '指标上升' },
  { value: 'down',   label: '下降', tagType: 'danger',  description: '指标下降' },
  { value: 'flat',   label: '持平', tagType: 'info',    description: '指标无明显变化' }
]);

export const TREND_LABEL_MAP = Object.freeze(
  TREND_OPTIONS.reduce((acc, o) => { acc[o.value] = o.label; return acc }, {})
)

export const TREND_TAG_TYPE_MAP = Object.freeze(
  TREND_OPTIONS.reduce((acc, o) => { acc[o.value] = o.tagType; return acc }, {})
)

export const getTrendLabel = (value) => {
  if (value === undefined || value === null || value === '') return '-'
  return TREND_LABEL_MAP[value] || String(value)
};

export const getTrendTagType = (value) => TREND_TAG_TYPE_MAP[value] || 'info';

export default {
  TREND_OPTIONS,
  TREND_LABEL_MAP,
  TREND_TAG_TYPE_MAP,
  getTrendLabel,
  getTrendTagType
}
