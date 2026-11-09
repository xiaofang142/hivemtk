// intentToolChain.js TB-02：意图→工具调用流绑定 HTTP 出口。
// 后端契约：/api/intent/tool-chains（管理端，AdminAuth 闸，TB-01 交付）。
import { http } from '@/utils/request';

export function listIntentToolChains(params) {
  return http.get('/api/intent/tool-chains', params);
}

export function createIntentToolChain(data) {
  return http.post('/api/intent/tool-chains', data);
}

export function updateIntentToolChain(id, data) {
  return http.put(`/api/intent/tool-chains/${id}`, data);
}

export function deleteIntentToolChain(id) {
  return http.delete(`/api/intent/tool-chains/${id}`);
}

// 词表（单一事实源）：意图双家族+模板变量白名单+注册中心工具名
export function getIntentToolChainVocab() {
  return http.get('/api/intent/tool-chains/vocab');
}

// ---- 自定义意图词条（T-09） ----
export function listCustomIntents(params) {
  return http.get('/api/intent/custom', params);
}

export function createCustomIntent(data) {
  return http.post('/api/intent/custom', data);
}

export function updateCustomIntent(id, data) {
  return http.put(`/api/intent/custom/${id}`, data);
}

export function deleteCustomIntent(id) {
  return http.delete(`/api/intent/custom/${id}`);
}
