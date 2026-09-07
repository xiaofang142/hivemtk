import i18n from '@/i18n';

export const ENABLED_OPTIONS = Object.freeze([
  { value: 1,        label: '启用',   tagType: 'success', aliases: ['active', 'enabled', 'online', 'on', 1, '1', true] },
  { value: 0,        label: '禁用',   tagType: 'danger',  aliases: ['inactive', 'disabled', 'offline', 'off', 0, '0', false] }
])

const ALIAS_TO_VALUE = Object.freeze(
  ENABLED_OPTIONS.reduce((acc, o) => {
    o.aliases.forEach((a) => { acc[String(a).toLowerCase()] = o.value })
    return acc
  }, {})
);

export const ENABLED_LABEL_MAP = Object.freeze(
  ENABLED_OPTIONS.reduce((acc, o) => {
    acc[String(o.value)] = o.label
    o.aliases.forEach((a) => { acc[String(a).toLowerCase()] = o.label })
    return acc
  }, {})
);

export const ENABLED_TAG_TYPE_MAP = Object.freeze(
  ENABLED_OPTIONS.reduce((acc, o) => {
    acc[String(o.value)] = o.tagType
    o.aliases.forEach((a) => { acc[String(a).toLowerCase()] = o.tagType })
    return acc
  }, {})
);

export const normalizeEnabled = (v) => {
  if (typeof v === 'number') return v === 1 ? 1 : 0
  if (typeof v === 'boolean') return v ? 1 : 0
  const key = String(v ?? '').toLowerCase()
  if (key in ALIAS_TO_VALUE) return ALIAS_TO_VALUE[key]
  return NaN
};

export const isEnabled = (v) => normalizeEnabled(v) === 1;

export const getEnabledLabel = (v) => {
  if (v === undefined || v === null || v === '') return '-'
  const n = normalizeEnabled(v)
  if (n === 1 || n === 0) {
    const key = n === 1 ? 'enabledLabel.enabled' : 'enabledLabel.disabled'
    const tr = i18n.global.t(key)
    if (tr && tr !== key) return tr
  }
  const key = String(v).toLowerCase()
  return ENABLED_LABEL_MAP[key] || String(v)
};

export const getEnabledTagType = (v) => {
  const key = String(v ?? '').toLowerCase()
  return ENABLED_TAG_TYPE_MAP[key] || 'info'
};

export default {
  ENABLED_OPTIONS,
  ENABLED_LABEL_MAP,
  ENABLED_TAG_TYPE_MAP,
  getEnabledLabel,
  getEnabledTagType,
  isEnabled,
  normalizeEnabled
}
