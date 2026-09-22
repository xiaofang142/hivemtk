import { http } from '@/utils/request';

export const platformAPI = {
  getLatestMessage() {
    return http.get('/api/platform/message/latest', { _silent: true })
  },

  markMessageRead(messageId) {
    return http.post(`/api/platform/message/${messageId}/read`)
  }
};