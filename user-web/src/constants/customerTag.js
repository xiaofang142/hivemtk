export const CUSTOMER_TAG = Object.freeze({
  ACTIVE: 'active',
  INACTIVE: 'inactive',
  LOST: 'lost',
  CHURN: 'churn',
  VIP: 'vip',
  POTENTIAL: 'potential',
  HIGH_VALUE: 'high_value',
  IMPORTANT: 'important',
  KEEP: 'keep'
});

export const CUSTOMER_STATUS_OPTIONS = Object.freeze([
  { value: 'active',   label: '正常',     tagType: 'success', sort: 1, group: 'status' },
  { value: 'inactive', label: '不活跃',   tagType: 'info',    sort: 2, group: 'status' },
  { value: 'lost',     label: '已流失',   tagType: 'danger',  sort: 3, group: 'status' },
  { value: 'churn',    label: '已流失',   tagType: 'danger',  sort: 4, group: 'status' }
]);

export const CUSTOMER_TAG_OPTIONS = Object.freeze([
  { value: 'vip',         label: 'VIP 客户',     tagType: 'danger',  sort: 1, group: 'rfm' },
  { value: 'high_value',  label: '高价值客户',   tagType: 'success', sort: 2, group: 'rfm' },
  { value: 'important',   label: '重要客户',     tagType: 'success', sort: 3, group: 'rfm' },
  { value: 'potential',   label: '潜在客户',     tagType: 'primary', sort: 4, group: 'rfm' },
  { value: 'keep',        label: '保持客户',     tagType: 'warning', sort: 5, group: 'rfm' },
  { value: 'lost',        label: '流失客户',     tagType: 'danger',  sort: 6, group: 'rfm' }
]);

const _buildLabelMap = (arr) => arr.reduce((acc, o) => { acc[o.value] = o.label; return acc }, {});
const _buildTagTypeMap = (arr) => arr.reduce((acc, o) => { acc[o.value] = o.tagType; return acc }, {})

export const CUSTOMER_STATUS_LABEL_MAP = Object.freeze(_buildLabelMap(CUSTOMER_STATUS_OPTIONS))
export const CUSTOMER_STATUS_TAG_TYPE_MAP = Object.freeze(_buildTagTypeMap(CUSTOMER_STATUS_OPTIONS))
export const CUSTOMER_TAG_LABEL_MAP = Object.freeze(_buildLabelMap(CUSTOMER_TAG_OPTIONS))
export const CUSTOMER_TAG_TAG_TYPE_MAP = Object.freeze(_buildTagTypeMap(CUSTOMER_TAG_OPTIONS))

export const getCustomerStatusLabel = (value) => {
  if (value === undefined || value === null || value === '') return '-'
  return CUSTOMER_STATUS_LABEL_MAP[value] || String(value)
};

export const getCustomerStatusTagType = (value) => CUSTOMER_STATUS_TAG_TYPE_MAP[value] || 'info';

export const getCustomerTagLabel = (value) => {
  if (value === undefined || value === null || value === '') return '-'
  return CUSTOMER_TAG_LABEL_MAP[value] || String(value)
};

export const getCustomerTagTagType = (value) => CUSTOMER_TAG_TAG_TYPE_MAP[value] || 'info';

export const filterCustomerByGroup = (groups) => {
  const list = Array.isArray(groups) ? groups : [groups]
  return [...CUSTOMER_STATUS_OPTIONS, ...CUSTOMER_TAG_OPTIONS].filter((o) => list.includes(o.group))
};

export default {
  CUSTOMER_TAG,
  CUSTOMER_STATUS_OPTIONS,
  CUSTOMER_TAG_OPTIONS,
  CUSTOMER_STATUS_LABEL_MAP,
  CUSTOMER_STATUS_TAG_TYPE_MAP,
  CUSTOMER_TAG_LABEL_MAP,
  CUSTOMER_TAG_TAG_TYPE_MAP,
  getCustomerStatusLabel,
  getCustomerStatusTagType,
  getCustomerTagLabel,
  getCustomerTagTagType,
  filterCustomerByGroup
}
