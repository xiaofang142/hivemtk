import { http } from '@/utils/request'

export const listGeoAlerts = (params) =>
  http.get('/api/geo/alerts', params);

// 顶栏未读角标是后台轮询：GEO 模块未开通时 4301 必须静默吞掉，
// 否则全局拦截器会 toast+3 秒后强跳功能模块市场，把用户从当前页面拽走
//（handleModuleNotEnabled 的 _silent 分支专为此而设）。
export const getGeoAlertsUnreadCount = () =>
  http.get('/api/geo/alerts/unread-count', undefined, { _silent: true })

export const ackGeoAlert = (id) =>
  http.post(`/api/geo/alerts/${id}/ack`, {})

export const deleteGeoAlert = (id) =>
  http.delete(`/api/geo/alerts/${id}`)
