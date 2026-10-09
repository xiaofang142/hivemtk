import { getRequestInstance } from './request';

export const http = {
  get(url, params, config = {}) {
    let realParams = params
    if (realParams &&
    typeof realParams === 'object' &&
    !Array.isArray(realParams) &&
    Object.keys(realParams).length === 1 &&
    'params' in realParams) {
      realParams = realParams.params
    }
    return getRequestInstance().get(url, { params: realParams, ...config })
  },

  post(url, data, config = {}) {
    return getRequestInstance().post(url, data, config)
  },

  put(url, data, config = {}) {
    return getRequestInstance().put(url, data, config)
  },

  // patch 与 put 同形。此前缺这个方法，而 KnowledgeManagement.vue 的
  // 公开性切换已经在调 http.patch —— 运行时会抛 "http.patch is not a function"，
  // 订单草稿的改价/改量（PATCH /api/manage/order-drafts/:id）也走这一口。
  patch(url, data, config = {}) {
    return getRequestInstance().patch(url, data, config)
  },

  delete(url, params, config = {}) {
    let realParams = params
    if (
      realParams &&
      typeof realParams === 'object' &&
      !Array.isArray(realParams) &&
      Object.keys(realParams).length === 1 &&
      'params' in realParams
    ) {
      realParams = realParams.params
    }
    return getRequestInstance().delete(url, { params: realParams, ...config })
  },

  upload(url, formData, config = {}) {
    return getRequestInstance().post(url, formData, {
      headers: {
        'Content-Type': 'multipart/form-data'
      },
      ...config
    })
  }
}