import i18n from '@/i18n';

export const ROLE_OPTIONS = Object.freeze([
  {
    value: 'owner',
    label: '所有者',
    tagType: 'danger',
    group: 'team',
    description: '团队/社区创建者'
  },
  { value: 'admin',      label: '管理员',  tagType: 'warning', group: 'team',     description: '团队/社区管理员' },
  { value: 'staff',      label: '员工',    tagType: 'info',    group: 'team',     description: '普通员工（可登录）' },
  { value: 'supervisor', label: '主管',    tagType: 'info',        group: 'team',     description: '客服/销售主管' },
  { value: 'agent',      label: '坐席',    tagType: 'primary', group: 'team',     description: '客服坐席' },
  { value: 'member',     label: '成员',    tagType: 'info',    group: 'team',     description: '普通成员' },
  { value: 'guest',      label: '访客',    tagType: 'info',    group: 'team',     description: '只读访客' },
  {
    value: 'user',
    label: '用户',
    tagType: 'info',
    group: 'message',
    description: '终端用户/客户'
  },
  { value: 'ai',         label: 'AI',      tagType: 'primary', group: 'message',  description: 'AI 智能体' },
  { value: 'system',     label: '系统',    tagType: 'info',        group: 'message',  description: '系统消息' },
  {
    value: 'customer_service',
    label: '客服',
    tagType: 'primary',
    group: 'message',
    description: '客服坐席（旧值）'
  }
])

export const ROLE_LABEL_MAP = Object.freeze(
  ROLE_OPTIONS.reduce((acc, o) => { acc[o.value] = o.label; return acc }, {})
)

export const ROLE_TAG_TYPE_MAP = Object.freeze(
  ROLE_OPTIONS.reduce((acc, o) => { acc[o.value] = o.tagType; return acc }, {})
)

export const getRoleLabel = (v) => {
  if (v === undefined || v === null || v === '') return '-'
  const key = 'roleName.' + v
  const tr = i18n.global.t(key)
  if (tr && tr !== key) return tr
  return ROLE_LABEL_MAP[v] || String(v)
}

export const getRoleTagType = (v) => ROLE_TAG_TYPE_MAP[v] || 'info'

export const filterRolesByGroup = (groups) => {
  const list = Array.isArray(groups) ? groups : [groups]
  return ROLE_OPTIONS.filter((o) => list.includes(o.group))
}

export default {
  ROLE_OPTIONS,
  ROLE_LABEL_MAP,
  ROLE_TAG_TYPE_MAP,
  getRoleLabel,
  getRoleTagType,
  filterRolesByGroup
}
