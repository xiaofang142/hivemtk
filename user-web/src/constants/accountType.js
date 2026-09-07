export const ACCOUNT_STATUS = Object.freeze({
  NORMAL: 1,
  ABNORMAL: 2,
  NOT_LOGGED_IN: 3,

  ACTIVE: 'active',
  ONLINE: 'online',
  OFFLINE: 'offline',
  BANNED: 'banned',
  WARNING: 'warning',
  CRITICAL: 'critical'
});

export const ACCOUNT_STATUS_NUMERIC_OPTIONS = Object.freeze([
  { value: 1, label: '正常',   tagType: 'success', sort: 1, group: 'numeric' },
  { value: 2, label: '异常',   tagType: 'danger',  sort: 2, group: 'numeric' },
  { value: 3, label: '未登录', tagType: 'warning', sort: 3, group: 'numeric' }
]);

export const ACCOUNT_STATUS_STRING_OPTIONS = Object.freeze([
  { value: 'active',   label: '正常', tagType: 'success', sort: 1, group: 'string' },
  { value: 'online',   label: '在线', tagType: 'success', sort: 2, group: 'string' },
  { value: 'offline',  label: '离线', tagType: 'info',    sort: 3, group: 'string' },
  { value: 'banned',   label: '封禁', tagType: 'danger',  sort: 4, group: 'string' }
]);

export const ACCOUNT_RISK_LEVEL_OPTIONS = Object.freeze([
  { value: 'normal',   label: '正常', tagType: 'success', sort: 1, group: 'risk' },
  { value: 'warning',  label: '警告', tagType: 'warning', sort: 2, group: 'risk' },
  { value: 'critical', label: '危险', tagType: 'danger',  sort: 3, group: 'risk' },
  { value: 'banned',   label: '封禁', tagType: 'danger',  sort: 4, group: 'risk' }
]);

const _buildLabelMap = (arr) => arr.reduce((acc, o) => {
  acc[o.value] = o.label
  acc[String(o.value)] = o.label
  return acc
}, {});
const _buildTagTypeMap = (arr) => arr.reduce((acc, o) => {
  acc[o.value] = o.tagType
  acc[String(o.value)] = o.tagType
  return acc
}, {})

export const ACCOUNT_STATUS_LABEL_MAP = Object.freeze({
  ..._buildLabelMap(ACCOUNT_STATUS_NUMERIC_OPTIONS),
  ..._buildLabelMap(ACCOUNT_STATUS_STRING_OPTIONS),
  ..._buildLabelMap(ACCOUNT_RISK_LEVEL_OPTIONS)
})

export const ACCOUNT_STATUS_TAG_TYPE_MAP = Object.freeze({
  ..._buildTagTypeMap(ACCOUNT_STATUS_NUMERIC_OPTIONS),
  ..._buildTagTypeMap(ACCOUNT_STATUS_STRING_OPTIONS),
  ..._buildTagTypeMap(ACCOUNT_RISK_LEVEL_OPTIONS)
})

export const getAccountStatusLabel = (value) => {
  if (value === undefined || value === null || value === '') return '-'
  return ACCOUNT_STATUS_LABEL_MAP[value] || ACCOUNT_STATUS_LABEL_MAP[String(value)] || String(value)
};

export const getAccountStatusTagType = (value) => {
  if (value === undefined || value === null || value === '') return ''
  return ACCOUNT_STATUS_TAG_TYPE_MAP[value] || ACCOUNT_STATUS_TAG_TYPE_MAP[String(value)] || 'info'
};

export const filterAccountStatusByGroup = (groups) => {
  const list = Array.isArray(groups) ? groups : [groups]
  return [
    ...ACCOUNT_STATUS_NUMERIC_OPTIONS,
    ...ACCOUNT_STATUS_STRING_OPTIONS,
    ...ACCOUNT_RISK_LEVEL_OPTIONS
  ].filter((o) => list.includes(o.group))
};

export default {
  ACCOUNT_STATUS,
  ACCOUNT_STATUS_NUMERIC_OPTIONS,
  ACCOUNT_STATUS_STRING_OPTIONS,
  ACCOUNT_RISK_LEVEL_OPTIONS,
  ACCOUNT_STATUS_LABEL_MAP,
  ACCOUNT_STATUS_TAG_TYPE_MAP,
  getAccountStatusLabel,
  getAccountStatusTagType,
  filterAccountStatusByGroup
}
