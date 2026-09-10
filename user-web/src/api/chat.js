import { http } from '@/utils/request'

export function getUploadToken(params) {
  return http.get('/api/chat/public/upload-token', { params })
}
