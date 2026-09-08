import { http } from '@/utils/request'

export const SystemApi = {
  getConfig() {
    return http.get('/api/system/config')
  },
  saveConfig(data) {
    return http.post('/api/system/config', data)
  },
};
