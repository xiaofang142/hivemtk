export const AUTH_TYPE_OPTIONS = Object.freeze([
  { value: 'bearer',  label: 'Bearer Token', tagType: 'info',    description: 'Bearer Token 鉴权' },
  { value: 'api_key', label: 'API Key',      tagType: 'warning', description: 'API Key 鉴权' },
  { value: 'hmac',    label: 'HMAC 签名',    tagType: 'primary', description: 'HMAC 签名鉴权' },
  { value: 'basic',   label: 'Basic Auth',   tagType: 'danger',  description: 'HTTP Basic 鉴权' },
  { value: 'oauth2',  label: 'OAuth2',       tagType: 'success', description: 'OAuth2 鉴权' },
  { value: 'none',    label: '无鉴权',       tagType: 'info',        description: '无需鉴权（内网调用）' }
]);

export const AUTH_TYPE_LABEL_MAP = Object.freeze(
  AUTH_TYPE_OPTIONS.reduce((acc, o) => { acc[o.value] = o.label; return acc }, {})
)

export const AUTH_TYPE_TAG_TYPE_MAP = Object.freeze(
  AUTH_TYPE_OPTIONS.reduce((acc, o) => { acc[o.value] = o.tagType; return acc }, {})
)

export const getAuthTypeLabel = (v) => {
  if (v === undefined || v === null || v === '') return '-'
  return AUTH_TYPE_LABEL_MAP[v] || String(v)
}

export const getAuthTypeTagType = (v) => AUTH_TYPE_TAG_TYPE_MAP[v] || 'info'

export default {
  AUTH_TYPE_OPTIONS,
  AUTH_TYPE_LABEL_MAP,
  AUTH_TYPE_TAG_TYPE_MAP,
  getAuthTypeLabel,
  getAuthTypeTagType
}
