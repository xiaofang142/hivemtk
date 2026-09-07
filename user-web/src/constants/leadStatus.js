export const LEAD_STATUS = Object.freeze({
  NEW: 'new',
  CONTACTED: 'contacted',
  QUALIFIED: 'qualified',
  NEGOTIATING: 'negotiating',
  CONVERTED: 'converted',
  LOST: 'lost',
  INVALID: 'invalid'
});

export const LEAD_STATUS_OPTIONS = Object.freeze([
  { value: 'new',         label: '新线索',   tagType: 'info',    sort: 1, group: 'funnel' },
  { value: 'contacted',   label: '已联系',   tagType: 'warning', sort: 2, group: 'funnel' },
  { value: 'qualified',   label: '已认证',   tagType: 'primary', sort: 3, group: 'funnel' },
  { value: 'negotiating', label: '洽谈中',   tagType: 'warning', sort: 4, group: 'funnel' },
  { value: 'converted',   label: '已转化',   tagType: 'success', sort: 5, group: 'funnel' },
  { value: 'lost',        label: '已流失',   tagType: 'danger',  sort: 6, group: 'funnel' },
  { value: 'invalid',     label: '无效',     tagType: 'info',    sort: 7, group: 'funnel' }
]);

export const LEAD_STATUS_OPTIONS_SIMPLE = Object.freeze([
  { value: 'new',         label: '新线索',   tagType: 'info',    sort: 1, group: 'simple' },
  { value: 'contacted',   label: '已联系',   tagType: 'warning', sort: 2, group: 'simple' },
  { value: 'converted',   label: '已转化',   tagType: 'success', sort: 3, group: 'simple' },
  { value: 'lost',        label: '已流失',   tagType: 'danger',  sort: 4, group: 'simple' }
]);

export const LEAD_STATUS_LABEL_MAP = Object.freeze(
  LEAD_STATUS_OPTIONS.reduce((acc, o) => { acc[o.value] = o.label; return acc }, {})
);

export const LEAD_STATUS_TAG_TYPE_MAP = Object.freeze(
  LEAD_STATUS_OPTIONS.reduce((acc, o) => { acc[o.value] = o.tagType; return acc }, {})
);

export const getLeadStatusLabel = (value) => {
  if (value === undefined || value === null || value === '') return '-'
  return LEAD_STATUS_LABEL_MAP[value] || String(value)
};

export const getLeadStatusTagType = (value) => LEAD_STATUS_TAG_TYPE_MAP[value] || 'info';

export const filterLeadStatusByGroup = (groups) => {
  const list = Array.isArray(groups) ? groups : [groups]
  return LEAD_STATUS_OPTIONS.filter((o) => list.includes(o.group))
};

export default {
  LEAD_STATUS,
  LEAD_STATUS_OPTIONS,
  LEAD_STATUS_OPTIONS_SIMPLE,
  LEAD_STATUS_LABEL_MAP,
  LEAD_STATUS_TAG_TYPE_MAP,
  getLeadStatusLabel,
  getLeadStatusTagType,
  filterLeadStatusByGroup
}
