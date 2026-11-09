import { http } from '@/utils/request'

export const getCustomerBasic = (id, signal) =>
  http.get(`/api/customer-360/basic?user_id=${id}`, undefined, { signal });

export const getCustomerEvents = (id, params, signal) =>
  http.get(`/api/customer-360/events`, { user_id: id, ...params }, { signal })

export const getCustomerSessions = (id, params, signal) =>
  http.get(`/api/customer-360/sessions`, { user_id: id, ...params }, { signal })

export const getCustomerTags = (id, signal) =>
  http.get(`/api/customer-360/tags?user_id=${id}`, undefined, { signal })

/**
 * 客户消息记录（沟通记录 tab 用）。
 *
 * 不能复用 getCustomerDetail：那里把 message_history 写死成空数组（只拉了
 * basic + tags），于是「行为轨迹」「沟通记录」两个 tab 永远是空白——UI 审计
 * 实测点击两个 tab 都只有一张空卡片。这里单独走 /messages 接口取真实数据。
 */
export const getCustomerMessages = (id, params, signal) =>
  http.get(`/api/customer-360/messages`, { user_id: id, ...params }, { signal })


export const getCustomerOrders = (id, params, signal) =>
  http.get(`/api/customer-360/orders`, { user_id: id, ...params }, { signal })

export const loadCustomer360 = async (id, { signal } = {}) => {
  const results = await Promise.allSettled([
    getCustomerBasic(id, signal),
    getCustomerEvents(id, { limit: 50 }, signal),
    getCustomerSessions(id, { limit: 20 }, signal),
    getCustomerTags(id, signal),
    getCustomerOrders(id, { limit: 20 }, signal)
  ])
  const [basic, events, sessions, tags, orders] = results
  return {
    basic: basic.status === 'fulfilled' ? basic.value : null,
    events: events.status === 'fulfilled' ? events.value : [],
    sessions: sessions.status === 'fulfilled' ? sessions.value : [],
    tags: tags.status === 'fulfilled' ? tags.value : [],
    orders: orders.status === 'fulfilled' ? orders.value : [],
    errors: results.filter((r) => r.status === 'rejected').map((r) => r.reason?.message)
  }
};

export const CACHE_KEY_PREFIX = 'c360:';

export const buildCacheKey = (id) => `${CACHE_KEY_PREFIX}${id}`

export const getCustomerList = (params) =>
  http.get('/api/customer-360/list', params);

export const getCustomerBasicById = (id) =>
  http.get(`/api/customer-360/basic?user_id=${encodeURIComponent(id)}`)

export const getCustomerDetail = async (id) => {
  const settled = await Promise.allSettled([
    getCustomerBasicById(id),
    getCustomerTags(id)
  ]);
  const basic = settled[0].status === 'fulfilled' ? settled[0].value : null
  const tags = settled[1].status === 'fulfilled' ? settled[1].value : []
  return {
    ...(basic && typeof basic === 'object' ? basic : {}),
    basic_info: basic?.basic_info || basic?.basic_info === null ? (basic?.basic_info ?? basic) : basic,
    tags: Array.isArray(tags) ? tags : tags?.tags || [],
    message_history: []
  }
}

const fetchTagsRaw = async (id) => {
  const r = await http.get(`/api/customer-360/tags?user_id=${encodeURIComponent(id)}`)
  if (Array.isArray(r)) return r
  if (Array.isArray(r?.tags)) return r.tags
  return []
}

export const addCustomerTag = async (id, tag) => {
  const tags = await fetchTagsRaw(id)
  if (!tags.includes(tag)) tags.push(tag)
  return http.put(`/api/customer-360/tags?user_id=${encodeURIComponent(id)}`, { tags })
}

export const removeCustomerTag = async (id, tag) => {
  const tags = (await fetchTagsRaw(id)).filter((t) => t !== tag)
  return http.put(`/api/customer-360/tags?user_id=${encodeURIComponent(id)}`, { tags })
}
