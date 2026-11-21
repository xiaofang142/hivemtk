import { http } from '@/utils/request'

export const listCapabilityCells = () => http.get('/api/v1/capabilities/cells', undefined, { _silent: true })
export const getCapabilityCell = (params) => http.get('/api/v1/capabilities/resolve', params, { _silent: true })
