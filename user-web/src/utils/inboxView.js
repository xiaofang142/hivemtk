// T10 统一收件箱展示纯函数（无 Vue / DOM 依赖，便于单测）。
//
// 纪律：自他判据以服务端下发的 is_self 为权威，前端只在缺字段时做保守回退，
// 绝不按昵称猜 self；群聊归并在读时完成，不回写任何绑定或消息数据。

export const PLATFORM_SENDER_TYPES = new Set([
  'staff', 'agent', 'ai', 'system', 'self', 'human', 'bot'
])

/** 平台发言（右侧）：is_self 权威，direction/is_ai_reply/sender_type 依次回退。 */
export function isPlatformMessage(msg) {
  if (!msg) return false
  if (typeof msg.is_self === 'boolean') return msg.is_self
  if (msg.direction === 'outbound') return true
  if (msg.is_ai_reply) return true
  const st = String(msg.sender_type || '').toLowerCase().trim()
  return PLATFORM_SENDER_TYPES.has(st)
}

/** 会话类型：服务端 conversation_type 显式口径优先，缺字段时用旧数据信号兜底。 */
export function isGroupConversation(conv) {
  if (!conv) return false
  const explicit = conv.conversation_type
  if (explicit === 'group') return true
  if (explicit === 'single') return false
  return !!(
    conv.is_group ||
    (conv.extra && (conv.extra.is_group || conv.extra.group_name)) ||
    conv.group_name || conv.group_id || conv.group_key
  )
}

/** 群名称：后端投影 group_name → extra.group_name → group_id → conversation_id，空值 fail-closed。 */
export function groupNameFor(conv) {
  if (!conv) return ''
  const candidates = [
    conv.group_name,
    conv.extra && conv.extra.group_name,
    conv.group_id,
    conv.conversation_id
  ]
  for (const raw of candidates) {
    const value = String(raw == null ? '' : raw).trim()
    if (value) return value
  }
  return ''
}

/** 群归并键：渠道 + 账号 + 群名称；群名为空返回空串（调用方落单聊，绝不跨群合并）。 */
export function groupKeyFor(conv) {
  const name = groupNameFor(conv)
  if (!name) return ''
  return `${conv.platform || ''}|${conv.account_id || ''}|${name}`
}

const convTime = (conv) => {
  const t = Date.parse(conv && (conv.last_message_at || conv.created_at))
  return Number.isFinite(t) ? t : 0
}

/**
 * 按 group_key 折叠群会话：一张群卡带 member_count / conversation_ids / 成员会话，
 * 代表卡取最近活跃的一条；不能命名的群退化为自己的单卡，不与其他群误并。
 * 客户/线索绑定（customer_id）保持原样，不回填、不改写。
 */
export function partitionInboxConversations(list) {
  const singles = []
  const groupMap = new Map()
  for (const conv of Array.isArray(list) ? list : []) {
    if (!conv) continue
    if (!isGroupConversation(conv)) {
      singles.push(conv)
      continue
    }
    const key = groupKeyFor(conv) || `conv:${conv.id}`
    let card = groupMap.get(key)
    if (!card) {
      card = {
        key,
        name: groupNameFor(conv) || conv.customer_name || conv.customer_id || '-',
        ids: Array.isArray(conv.conversation_ids) && conv.conversation_ids.length
          ? [...conv.conversation_ids]
          : (conv.conversation_id ? [String(conv.conversation_id)] : []),
        members: Number(conv.member_count || 0) || 1,
        conversations: [],
        representative: conv
      }
      groupMap.set(key, card)
    }
    card.conversations.push(conv)
    for (const id of (Array.isArray(conv.conversation_ids) ? conv.conversation_ids : []).concat(conv.conversation_id ? [String(conv.conversation_id)] : [])) {
      if (id && !card.ids.includes(id)) card.ids.push(id)
    }
    card.members = Math.max(card.members, card.conversations.length, Number(conv.member_count || 0) || 0)
    if (convTime(conv) > convTime(card.representative)) card.representative = conv
  }
  const groups = [...groupMap.values()].map((card) => {
    const rep = card.representative
    return {
      ...rep,
      is_group: true,
      conversation_type: 'group',
      group_key: card.key,
      group_name: card.name,
      member_count: card.members,
      conversation_ids: card.ids,
      conversations: card.conversations,
      customer_name: card.name
    }
  })
  groups.sort((a, b) => convTime(b) - convTime(a))
  return { singles, groups }
}

/** 高亮当前成员：用 sender_id 优先，sender_name 兜底；平台消息永不高亮。 */
export function isHighlightedMember(msg, highlight) {
  if (!msg || isPlatformMessage(msg)) return false
  if (!highlight) return false
  const id = String(highlight.sender_id || highlight.id || '').trim()
  const name = String(highlight.sender_name || highlight.name || '').trim()
  const msgID = String(msg.sender_id || '').trim()
  const msgName = String(msg.sender_name || '').trim()
  if (id && msgID === id) return true
  if (name && msgName === name) return true
  return false
}

/** 群线程加载后，默认高亮当前卡绑定的 CRM 人；找不到成员时用绑定值本身构造高亮。 */
export function defaultMemberHighlight(conv, members) {
  const id = String((conv && (conv.customer_id || conv.sender_id)) || '').trim()
  const name = String((conv && conv.customer_name) || '').trim()
  if (!id && !name) return null
  const list = Array.isArray(members) ? members : []
  const match = list.find((m) =>
    (id && String(m.sender_id || '').trim() === id) ||
    (name && String(m.sender_name || '').trim() === name)
  )
  if (match) return { sender_id: match.sender_id, sender_name: match.sender_name }
  return { sender_id: id, sender_name: name }
}

/** 筛选表单 → 服务端查询参数（分页/租户隔离始终留在服务端）。 */
export function buildInboxListParams(form, page, pageSize) {
  const params = { page, page_size: pageSize }
  const f = form || {}
  if (f.platform) params.platform = f.platform
  if (f.account_id) params.account_id = f.account_id
  if (f.conversation_type) params.conversation_type = f.conversation_type
  if (f.status) params.status = f.status
  if (f.filter) params.filter = f.filter
  if (f.pinned !== '' && f.pinned !== null && f.pinned !== undefined) params.pinned = f.pinned
  if (f.nickname) params.nickname = f.nickname
  if (f.keyword) params.keyword = f.keyword
  return params
}
