import { onMounted, ref } from 'vue'
import { inboxApi } from '@/api/inbox'
import { listSystemUsers } from '@/api/systemUser'
import { listEnabledAgents } from '@/api/aiAgent'
import { leadsApi } from '@/api/leads'

// 全站外键字段的统一选项源（T06）。
//
// 口径与 channelCatalog 对齐：
//   - 一实体一份共享选项数组 + 单飞 Promise + 60s 内存缓存，几十个页面共用，不各拉一遍；
//   - 拉不到（无权限/网络抖动/无数据）只留空数组，调用方用「#id」兜底回显原值，绝不写 0；
//   - value 保持后端字段原类型（用户/智能体=数字 id，客户=字符串 id），提交不转换。
//
// 用户有两套源：user=脱敏坐席名单 /api/inbox/agents（非超管可读），
// adminUser=仅超管账号清单 /api/system/users（含停用账号，供启停恢复）。
// 后者只在确认是超管页时才传 admin:true，避免把账号资料带到坐席可访问页面。

const TTL = 60_000
const state = {
  user: { options: ref([]), ts: 0, pending: null },
  adminUser: { options: ref([]), ts: 0, pending: null },
  agent: { options: ref([]), ts: 0, pending: null },
  customer: { options: ref([]), ts: 0, pending: null }
}

function pickList(res) {
  return Array.isArray(res) ? res : (Array.isArray(res?.list) ? res.list : (Array.isArray(res?.data?.list) ? res.data.list : []))
}

const loaders = {
  user: async () => pickList(await inboxApi.listAgents()).map((u) => ({
    value: u.id,
    label: `${u.real_name || u.username || '员工'}（#${u.id}）`,
    name: u.real_name || u.username || ''
  })),
  adminUser: async () => pickList(await listSystemUsers({ page: 1, size: 500 })).map((u) => ({
    value: u.id,
    label: `${u.real_name || u.username || '员工'}（#${u.id}）`,
    name: u.real_name || u.username || ''
  })),
  agent: async () => pickList(await listEnabledAgents()).map((a) => ({
    value: a.id,
    label: `${a.name || a.agent_code || '智能体'}（#${a.id}）`,
    name: a.name || a.agent_code || ''
  })),
  customer: async () => pickList(await leadsApi.listScopedCustomers({ page: 1, limit: 500 })).map((c) => ({
    value: c.id,
    label: `${c.name || c.unified_id || '客户'}（#${String(c.id).slice(0, 8)}）`,
    name: c.name || c.unified_id || ''
  }))
}

function ensure(entity, force = false) {
  const s = state[entity]
  if (!s) return Promise.resolve([])
  if (!force && s.ts && Date.now() - s.ts < TTL && s.options.value.length) return Promise.resolve(s.options.value)
  if (s.pending) return s.pending
  s.pending = loaders[entity]()
    .then((list) => { s.options.value = list; s.ts = Date.now(); return list })
    .catch(() => s.options.value)
    .finally(() => { s.pending = null })
  return s.pending
}

function keyOf(entity, admin) {
  return admin && entity === 'user' ? 'adminUser' : entity
}

/** 供组件使用：options=共享候选，labelOf=「名称（#id）」/ 历史裸值兜底。 */
export function useEntityOptions(entity, { immediate = true, admin = false } = {}) {
  const key = keyOf(entity, admin)
  const options = state[key]?.options ?? ref([])
  const loading = ref(false)
  async function refresh() {
    loading.value = true
    try { await ensure(key, true) } finally { loading.value = false }
  }
  if (immediate) onMounted(() => ensure(key))
  function labelOf(value) {
    if (value == null || value === '' || value === 0) return ''
    const hit = options.value.find((o) => String(o.value) === String(value))
    return hit ? hit.label : `#${value}`
  }
  return { options, loading, refresh, labelOf }
}

/** 纯函数读缓存（已加载时同步出关联值；未加载返回空，由组件响应式重算）。 */
export function optionLabel(entity, value, { admin = false } = {}) {
  const key = keyOf(entity, admin)
  const opts = state[key]?.options.value || []
  const hit = opts.find((o) => String(o.value) === String(value))
  return hit ? hit.label : ''
}
