import { http } from '@/utils/request'

// 渠道统一目录（018/027 渠道统一化）：一次拿到「已开启渠道 + 其下已启用账号」。
// 全系统渠道/账号下拉与中文显示只认这一处，禁止各页面再自行维护码表或自由输入。
export const channelCatalogApi = {
  // GET /api/channels/catalog → { channels: ChannelCatalogItem[], accounts: ChannelAccountCatalogItem[] }
  catalog() {
    return http.get('/api/channels/catalog')
  },
  // 租户级渠道开关（预设码落 system_config_kv，自定义码落 channels.enabled）。
  setTenantEnabled(id, enabled) {
    return http.put(`/api/channels/${id}/tenant-enabled`, { enabled })
  },
  // 管理面：含停用账号（普通 /channel-accounts 列表只返回启用账号）。
  managedAccounts(params = {}) {
    return http.get('/api/channel-accounts/managed', params)
  },
  updateManagedAccount(id, data) {
    return http.put(`/api/channel-accounts/${id}/managed`, data)
  },
  setAccountStatus(id, status) {
    return http.put(`/api/channel-accounts/${id}/status`, { status })
  }
}

export default channelCatalogApi
