import { http } from '@/utils/request'

export const sendCollaborativeMessage = (sessionId, data) =>
  http.post(`/api/customer-sessions/${sessionId}/messages`, data);

export const addInternalNote = (sessionId, data) =>
  http.post(`/api/customer-sessions/${sessionId}/internal-notes`, data);

export const searchMentionUsers = (params) =>
  http.get('/api/users/search', params);

export const markMentionRead = (mentionId) =>
  http.post(`/api/mentions/${mentionId}/read`, {});

export const getMyMentions = (params) =>
  http.get('/api/mentions/mine', params);

// holder 由后端按登录人补成 user:<id>（与 release 同口径）。
// 修前这里写死 { holder: 'me' }：所有人共用一个持有人，锁就退化成"谁都能解"。
export const acquireEditLock = (sessionId) =>
  http.post(`/api/customer-sessions/${sessionId}/edit-lock`, {}, { _silent: true });

export const releaseEditLock = (sessionId) =>
  http.delete(`/api/customer-sessions/${sessionId}/edit-lock`, { _silent: true });

export const getEditLock = (sessionId) =>
  http.get(`/api/customer-sessions/${sessionId}/edit-lock`, { _silent: true });

export const listInternalNotes = (sessionId, params) =>
  http.get(`/api/customer-sessions/${sessionId}/internal-notes`, params);
