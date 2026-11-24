import { http } from '@/utils/request'

// 渠道账号统一配置接口（FR-024/025）：渠道账号下拉、两类智能体绑定、共享上下文交叉引用。
// 列表/下拉统一走后端 /api/channel-accounts（ChannelAccountService.ListOptions，FR-029）。
export const channelAccountApi = {
  listChannels(params = {}) {
    return http.get('/api/channels', params)
  },
  // 预设渠道 CRUD（FR-022/023，一次配置全系统生效）。
  createChannel(data) {
    return http.post('/api/channels', data)
  },
  updateChannel(id, data) {
    return http.put(`/api/channels/${id}`, data)
  },
  deleteChannel(id) {
    return http.delete(`/api/channels/${id}`)
  },
  listAccounts(params = {}) {
    return http.get('/api/channel-accounts', params)
  },
  accountOptions(params = {}) {
    return http.get('/api/channel-accounts/options', params)
  },
  getAccount(id) {
    return http.get(`/api/channel-accounts/${id}`)
  },
  createAccount(data) {
    return http.post('/api/channel-accounts', data)
  },
  updateAccount(id, data) {
    return http.put(`/api/channel-accounts/${id}`, data)
  },
  deleteAccount(id) {
    return http.delete(`/api/channel-accounts/${id}`)
  },
  // 「线索发现智能体」独立注册（FR-026）：列表即可作为账号配置的可选智能体来源。
  listLeadMiningAgents(params = {}) {
    return http.get('/api/manage/lead-mining-agents', params)
  },
  // 共享对话上下文 / 交叉引用展示（T054）。
  conversationContext(params = {}) {
    return http.get('/api/conversation-context', params)
  }
}

export default channelAccountApi
