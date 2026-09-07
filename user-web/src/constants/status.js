export const TASK_STATUS = Object.freeze([
  { value: 'pending',   label: '等待中',     tagType: 'info',    sort: 1, group: 'task' },
  { value: 'running',   label: '执行中',     tagType: 'warning', sort: 2, group: 'task' },
  { value: 'paused',    label: '已暂停',     tagType: 'info',    sort: 3, group: 'task' },
  { value: 'completed', label: '已完成',     tagType: 'success', sort: 4, group: 'task' },
  { value: 'success',   label: '成功',       tagType: 'success', sort: 4, group: 'task' },
  { value: 'failed',    label: '失败',       tagType: 'danger',  sort: 5, group: 'task' },
  { value: 'cancelled', label: '已取消',     tagType: 'info',    sort: 6, group: 'task' }
]);

export const SYNC_STATUS = Object.freeze([
  { value: 'pending', label: '等待中',     tagType: 'info',    sort: 1, group: 'sync' },
  { value: 'running', label: '同步中',     tagType: 'warning', sort: 2, group: 'sync' },
  { value: 'success', label: '成功',       tagType: 'success', sort: 3, group: 'sync' },
  { value: 'failed',  label: '失败',       tagType: 'danger',  sort: 4, group: 'sync' },
  { value: 'partial', label: '部分成功',   tagType: 'warning', sort: 5, group: 'sync' }
])

export const BACKUP_STATUS = Object.freeze([
  { value: 'pending',   label: '等待中',     tagType: 'info',    sort: 1, group: 'backup' },
  { value: 'running',   label: '备份中',     tagType: 'warning', sort: 2, group: 'backup' },
  { value: 'success',   label: '成功',       tagType: 'success', sort: 3, group: 'backup' },
  { value: 'failed',    label: '失败',       tagType: 'danger',  sort: 4, group: 'backup' },
  { value: 'expired',   label: '已过期',     tagType: 'info',    sort: 5, group: 'backup' }
])

export const EXPERIMENT_STATUS = Object.freeze([
  { value: 'draft',     label: '草稿',       tagType: 'info',    sort: 1, group: 'experiment' },
  { value: 'running',   label: '进行中',     tagType: 'success', sort: 2, group: 'experiment' },
  { value: 'paused',    label: '已暂停',     tagType: 'warning', sort: 3, group: 'experiment' },
  { value: 'completed', label: '已完成',     tagType: 'info',        sort: 4, group: 'experiment' },
  { value: 'archived',  label: '已归档',     tagType: 'info',    sort: 5, group: 'experiment' }
])

export const GROUP_STATUS = Object.freeze([
  { value: 'active',   label: '正常',       tagType: 'success', sort: 1, group: 'group' },
  { value: 'disabled', label: '禁用',       tagType: 'danger',  sort: 2, group: 'group' },
  { value: 'archived', label: '已归档',     tagType: 'info',    sort: 3, group: 'group' }
])

export const CONTENT_STATUS = Object.freeze([
  { value: 'draft',      label: '草稿',       tagType: 'info',    sort: 1, group: 'content' },
  { value: 'pending',    label: '待发布',     tagType: 'warning', sort: 2, group: 'content' },
  { value: 'reviewing',  label: '审核中',     tagType: 'warning', sort: 3, group: 'content' },
  { value: 'published',  label: '已发布',     tagType: 'success', sort: 4, group: 'content' },
  { value: 'rejected',   label: '已驳回',     tagType: 'danger',  sort: 5, group: 'content' },
  { value: 'archived',   label: '已归档',     tagType: 'info',    sort: 6, group: 'content' }
])

export const AUDIT_STATUS = Object.freeze([
  { value: 'pending',   label: '待审核',     tagType: 'warning', sort: 1, group: 'audit' },
  { value: 'reviewing', label: '审核中',     tagType: 'warning', sort: 2, group: 'audit' },
  { value: 'approved',  label: '已通过',     tagType: 'success', sort: 3, group: 'audit' },
  { value: 'rejected',  label: '已驳回',     tagType: 'danger',  sort: 4, group: 'audit' }
])

export const PROMPT_STATUS = Object.freeze([
  { value: 'draft',     label: '草稿',       tagType: 'info',    sort: 1, group: 'prompt' },
  { value: 'reviewing', label: '审核中',     tagType: 'warning', sort: 2, group: 'prompt' },
  { value: 'approved',  label: '已批准',     tagType: 'success', sort: 3, group: 'prompt' },
  { value: 'rejected',  label: '已驳回',     tagType: 'danger',  sort: 4, group: 'prompt' },
  { value: 'archived',  label: '已归档',     tagType: 'info',    sort: 5, group: 'prompt' }
])

export const BLACKLIST_STATUS = Object.freeze([
  { value: 'active',  label: '生效中',     tagType: 'danger',  sort: 1, group: 'blacklist' },
  { value: 'expired', label: '已过期',     tagType: 'info',    sort: 2, group: 'blacklist' },
  { value: 'removed', label: '已解除',     tagType: 'success', sort: 3, group: 'blacklist' }
])

export const SESSION_STATUS = Object.freeze([
  { value: 'open',       label: '进行中',     tagType: 'success', sort: 1, group: 'session' },
  { value: 'pending',    label: '待处理',     tagType: 'warning', sort: 2, group: 'session' },
  { value: 'closed',     label: '已关闭',     tagType: 'info',    sort: 3, group: 'session' },
  { value: 'transferred',label: '已转接',     tagType: 'primary', sort: 4, group: 'session' }
])

export const STAGE_STATUS = Object.freeze([
  { value: 'new',         label: '新阶段',     tagType: 'info',    sort: 1, group: 'stage' },
  { value: 'active',      label: '激活',       tagType: 'success', sort: 2, group: 'stage' },
  { value: 'archived',    label: '已归档',     tagType: 'info',    sort: 3, group: 'stage' }
])

export const CONVERSATION_STATUS = Object.freeze([
  { value: 'open',         label: '未结束',   tagType: 'success', sort: 1, group: 'conversation' },
  { value: 'closed',       label: '已结束',   tagType: 'info',    sort: 2, group: 'conversation' },
  { value: 'pending',      label: '待响应',   tagType: 'warning', sort: 3, group: 'conversation' },
  { value: 'processing',   label: '处理中',   tagType: 'warning', sort: 4, group: 'conversation' }
])

export const EMBED_STATUS = Object.freeze([
  { value: 'indexed',    label: '已索引',   tagType: 'success', sort: 1, group: 'embed' },
  { value: 'processing', label: '处理中',   tagType: 'warning', sort: 2, group: 'embed' },
  { value: 'failed',     label: '失败',     tagType: 'danger',  sort: 3, group: 'embed' },
  { value: 'pending',    label: '待处理',   tagType: 'info',    sort: 4, group: 'embed' }
]);

export const AGENT_STATUS = Object.freeze([
  { value: 'online',  label: '在线', tagType: 'success', sort: 1, group: 'agent' },
  { value: 'busy',    label: '忙碌', tagType: 'warning', sort: 2, group: 'agent' },
  { value: 'away',    label: '离开', tagType: 'info',    sort: 3, group: 'agent' },
  { value: 'offline', label: '离线', tagType: 'info',    sort: 4, group: 'agent' }
]);

export const PASS_FAIL_STATUS = Object.freeze([
  { value: 'success', label: '成功', tagType: 'success', sort: 1, group: 'passfail' },
  { value: 'failed',  label: '失败', tagType: 'danger',  sort: 2, group: 'passfail' }
]);

export const SEGMENT_TYPE = Object.freeze([
  { value: 'auto',    label: '自动分群',   tagType: 'primary', sort: 1, group: 'segment' },
  { value: 'manual',  label: '手动分群',   tagType: 'info',    sort: 2, group: 'segment' },
  { value: 'dynamic', label: '动态分群',   tagType: 'success', sort: 3, group: 'segment' },
  { value: 'static',  label: '静态分群',   tagType: 'info',        sort: 4, group: 'segment' }
]);

export const buildStatusMaps = (arr) => {
  const labelMap = {}
  const tagTypeMap = {}
  arr.forEach((o) => {
    labelMap[o.value] = o.label
    tagTypeMap[o.value] = o.tagType
  })
  return { labelMap, tagTypeMap }
};

export const getStatusLabel = (value, arr) => {
  if (value === undefined || value === null || value === '') return '-'
  const o = arr.find((s) => s.value === value)
  return o ? o.label : String(value)
};

export const getStatusTagType = (value, arr) => {
  if (value === undefined || value === null || value === '') return 'info'
  const o = arr.find((s) => s.value === value)
  return o ? o.tagType : 'info'
};

export default {
  TASK_STATUS,
  SYNC_STATUS,
  BACKUP_STATUS,
  EXPERIMENT_STATUS,
  GROUP_STATUS,
  CONTENT_STATUS,
  AUDIT_STATUS,
  PROMPT_STATUS,
  BLACKLIST_STATUS,
  SESSION_STATUS,
  STAGE_STATUS,
  CONVERSATION_STATUS,
  EMBED_STATUS,
  AGENT_STATUS,
  PASS_FAIL_STATUS,
  SEGMENT_TYPE,
  buildStatusMaps,
  getStatusLabel,
  getStatusTagType
}
