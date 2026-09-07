export const ORDER_STATUS = Object.freeze({
  PENDING: 'pending',
  SCHEDULED: 'scheduled',
  SENDING: 'sending',
  SENT: 'sent',
  RUNNING: 'running',
  PAUSED: 'paused',
  COMPLETED: 'completed',
  FAILED: 'failed',
  CANCELLED: 'cancelled'
});

export const ORDER_STATUS_OPTIONS = Object.freeze([
  { value: 'pending',    label: '待执行',   tagType: 'info',    sort: 1, group: 'job' },
  { value: 'scheduled',  label: '已计划',   tagType: 'info',    sort: 2, group: 'job' },
  { value: 'sending',    label: '发送中',   tagType: 'warning', sort: 3, group: 'sms' },
  { value: 'sent',       label: '已发送',   tagType: 'success', sort: 4, group: 'sms' },
  { value: 'running',    label: '执行中',   tagType: 'warning', sort: 5, group: 'job' },
  { value: 'paused',     label: '已暂停',   tagType: 'info',    sort: 6, group: 'job' },
  { value: 'completed',  label: '已完成',   tagType: 'success', sort: 7, group: 'job' },
  { value: 'failed',     label: '已失败',   tagType: 'danger',  sort: 8, group: 'job' },
  { value: 'cancelled',  label: '已取消',   tagType: 'info',    sort: 9, group: 'job' }
]);

export const ORDER_STATUS_LABEL_MAP = Object.freeze(
  ORDER_STATUS_OPTIONS.reduce((acc, o) => { acc[o.value] = o.label; return acc }, {})
);

export const ORDER_STATUS_TAG_TYPE_MAP = Object.freeze(
  ORDER_STATUS_OPTIONS.reduce((acc, o) => { acc[o.value] = o.tagType; return acc }, {})
);

export const getOrderStatusLabel = (value) => {
  if (value === undefined || value === null || value === '') return '-'
  return ORDER_STATUS_LABEL_MAP[value] || String(value)
};

export const getOrderStatusTagType = (value) => ORDER_STATUS_TAG_TYPE_MAP[value] || 'info';

export const filterOrderStatusByGroup = (groups) => {
  const list = Array.isArray(groups) ? groups : [groups]
  return ORDER_STATUS_OPTIONS.filter((o) => list.includes(o.group))
};

export default {
  ORDER_STATUS,
  ORDER_STATUS_OPTIONS,
  ORDER_STATUS_LABEL_MAP,
  ORDER_STATUS_TAG_TYPE_MAP,
  getOrderStatusLabel,
  getOrderStatusTagType,
  filterOrderStatusByGroup
}

