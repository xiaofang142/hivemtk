import { http } from '@/utils/request'

// EC-00 只保存和校验本地试点契约，不调用真实平台。
export const pilotContractApi = {
  get: () => http.get('/api/pilot/contract'),
  validate: (contract) => http.post('/api/pilot/contract/validate', contract),
  save: (contract) => http.put('/api/pilot/contract', contract)
}
