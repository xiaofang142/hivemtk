import { http } from '@/utils/request'

export const SystemApi = {
  // 公开端点，无需鉴权即可读到平台集成开关等运行期形态
  getInfo(config) {
    return http.get('/api/system/info', config)
  },
  getConfig() {
    return http.get('/api/system/config')
  },
  saveConfig(data) {
    return http.post('/api/system/config', data)
  },
};
