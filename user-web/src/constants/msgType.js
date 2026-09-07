export const MSG_TYPE_OPTIONS = Object.freeze([
  { value: 'text',     label: '文本',   tagType: 'info',    icon: 'ChatDotRound', description: '纯文本消息' },
  { value: 'image',    label: '图片',   tagType: 'success', icon: 'Picture',      description: '图片消息' },
  { value: 'link',     label: '链接',   tagType: 'warning', icon: 'Link',         description: '图文链接' },
  { value: 'markdown', label: 'Markdown', tagType: 'primary', icon: 'Document',  description: 'Markdown 富文本' },
  { value: 'file',     label: '文件',   tagType: 'info',    icon: 'Folder',       description: '文件附件' },
  { value: 'video',    label: '视频',   tagType: 'success', icon: 'VideoCamera',  description: '视频消息' },
  { value: 'voice',    label: '语音',   tagType: 'primary', icon: 'Microphone',   description: '语音消息' },
  { value: 'card',     label: '卡片',   tagType: 'warning', icon: 'Postcard',     description: '卡片消息' },
  { value: 'event',    label: '事件',   tagType: 'info',    icon: 'Bell',         description: '事件通知' },
  { value: 'system',   label: '系统',   tagType: 'info',    icon: 'Setting',      description: '系统消息' }
]);

export const MSG_TYPE_LABEL_MAP = Object.freeze(
  MSG_TYPE_OPTIONS.reduce((acc, o) => { acc[o.value] = o.label; return acc }, {})
)

export const MSG_TYPE_TAG_TYPE_MAP = Object.freeze(
  MSG_TYPE_OPTIONS.reduce((acc, o) => { acc[o.value] = o.tagType; return acc }, {})
)

export const getMsgTypeLabel = (v) => {
  if (v === undefined || v === null || v === '') return '-'
  return MSG_TYPE_LABEL_MAP[v] || String(v)
}

// 未知类型兜底 'info'，避免 ElTag 收到空 type 触发 prop 校验告警（控制台噪音）
export const getMsgTypeTagType = (v) => MSG_TYPE_TAG_TYPE_MAP[v] || 'info'

export default {
  MSG_TYPE_OPTIONS,
  MSG_TYPE_LABEL_MAP,
  MSG_TYPE_TAG_TYPE_MAP,
  getMsgTypeLabel,
  getMsgTypeTagType
}
