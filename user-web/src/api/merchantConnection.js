import { http } from '@/utils/request'

// 单次敏感操作由页面显式处理失败；禁止通用 toast 展示提供方响应或隐式续期。
export const merchantConnectionApi = {
  get: ({ provider, shop_id }, signal) => http.get('/api/v1/integrations/merchant-connections', { provider, shop_id }, { _silent: true, _skipRefresh: true, timeout: 10000, signal }),
  start: ({ provider, shop_id }, signal) => http.post('/api/v1/integrations/merchant-connections/start', { provider, shop_id }, { _silent: true, _skipRefresh: true, timeout: 10000, signal }),
  complete: ({ provider, shop_id, code, state }, signal) => http.post('/api/v1/integrations/merchant-connections/complete', { provider, shop_id, code, state }, { _silent: true, _skipRefresh: true, timeout: 10000, signal }),
  refresh: ({ provider, shop_id, credential_version }, signal) => http.post('/api/v1/integrations/merchant-connections/refresh', { provider, shop_id, credential_version }, { _silent: true, _skipRefresh: true, timeout: 10000, signal }),
  revoke: ({ provider, shop_id }, signal) => http.post('/api/v1/integrations/merchant-connections/revoke', { provider, shop_id }, { _silent: true, _skipRefresh: true, timeout: 10000, signal })
}
