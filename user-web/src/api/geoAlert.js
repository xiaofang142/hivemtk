import { http } from '@/utils/request'

export const listGeoAlerts = (params) =>
  http.get('/api/geo/alerts', params);

export const getGeoAlertsUnreadCount = () =>
  http.get('/api/geo/alerts/unread-count')

export const ackGeoAlert = (id) =>
  http.post(`/api/geo/alerts/${id}/ack`, {})

export const deleteGeoAlert = (id) =>
  http.delete(`/api/geo/alerts/${id}`)
