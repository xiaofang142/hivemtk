import axios from 'axios';
import { ElMessage } from 'element-plus'
import { getApiConfig } from './configManager'
import i18n from '@/i18n'
import { getStoredLocale } from '@/i18n/locale'

const t = (key) => i18n.global.t(key)

const createRequestInstance = () => {
  let apiBaseUrl = import.meta.env?.VITE_API_BASE_URL || '';
  // localStorage 覆盖仅允许在开发环境生效；生产构建一律走 env 配置，
  // 防止被篡改的 localStorage 把全部 API 流量指向任意地址
  if (import.meta.env?.DEV) {
    try {
      const configStr = localStorage.getItem('apiConfig')
      if (configStr) {
        const cfg = JSON.parse(configStr)
        apiBaseUrl = cfg.baseUrl || apiBaseUrl
      }
    } catch (e) {}
  }
  return axios.create({
    baseURL: apiBaseUrl,
    headers: {
      'Content-Type': 'application/json'
    }
  })
};

const INIT_REDIRECT_MAP = {
  INIT_REQUIRED: '/setup'
};

let lastToastMsg = '';
let lastToastTs = 0
function showToast(message, type = 'error') {
  const msg = message || t('http.requestFailed')
  const now = Date.now()
  if (msg === lastToastMsg && now - lastToastTs < 2500) return
  lastToastMsg = msg
  lastToastTs = now
  if (type === 'warning') ElMessage.warning(msg)
  else if (type === 'info') ElMessage.info(msg)
  else ElMessage.error(msg)
}

function isJsonResponse(resp) {
  const ct =
    (resp && resp.headers && (resp.headers['content-type'] || resp.headers['Content-Type'])) || ''
  return String(ct).toLowerCase().includes('application/json')
}

function extractServerMessage(data, fallback) {
  if (data && typeof data === 'object') {
    return data.message || data.msg || fallback
  }
  return fallback
}

function buildRequestError(message, response, bizCode) {
  const err = new Error(message || t('http.requestFailed'))
  err.response = response || null
  if (bizCode !== undefined) err.bizCode = bizCode
  return err
}

function redirectTo(path) {
  if (!path) return
  const target = path.startsWith('/') ? path : '/' + path
  const current = (window.location.hash || '').replace(/^#/, '') || '/'
  if (current === target) return
  import('@/router')
    .then((m) => {
      const r = m.default
      if (r && typeof r.push === 'function') r.push(target)
      else window.location.href = '#' + target
    })
    .catch(() => {
      window.location.href = '#' + target
    })
}

function clearAuthAndGoLogin() {
  localStorage.removeItem('token')
  localStorage.removeItem('refreshToken')
  redirectTo('/login')
}

const addInterceptors = () => {
  request.interceptors.request.use(
    (config) => {
      const token = localStorage.getItem('token');
      if (token) {
        config.headers.Authorization = `Bearer ${token}`
      }
      config.headers['Accept-Language'] = getStoredLocale();
      return config
    },
    (error) => {
      console.error('请求错误:', error)
      return Promise.reject(error)
    }
  );

  request.interceptors.response.use(
    (response) => {
      const config = response.config
      let data = response.data
      const silent = !!(config && config._silent)

      if (config && config.responseType === 'blob') {
        return response.data
      }

      let body = data;
      if (!isJsonResponse(response) && typeof data === 'string') {
        try {
          const parsed = JSON.parse(data)
          if (parsed && typeof parsed === 'object') {
            body = parsed
          }
        } catch {}
      }
      if (!isJsonResponse(response) && typeof body !== 'object') {
        if (import.meta.env?.DEV) {
          console.error('[request] 收到非 JSON 响应，疑似网关/ 反向代理层 兜底到前端页面：', data)
        }
        const msg = t('http.unexpectedResponse')
        if (!silent) showToast(msg)
        return Promise.reject(buildRequestError(msg, response))
      }
      data = body

      if (data == null) {
        const msg = t('http.emptyResponse')
        if (!silent) showToast(msg)
        return Promise.reject(buildRequestError(msg, response))
      }

      const code = data.code

      if (code === 'SUCCESS' || code === 200 || code === 0) {
        return data.data
      }

      if (INIT_REDIRECT_MAP[code]) {
        const target = data.redirect || INIT_REDIRECT_MAP[code]
        redirectTo(target)
        const msg = data.message || t('http.initRequired')
        if (!silent) showToast(msg, 'warning')
        return Promise.reject(buildRequestError(msg, response, code))
      }

      const msg = extractServerMessage(data, t('http.requestFailed'));
      if (!silent) showToast(msg)
      return Promise.reject(buildRequestError(msg, response, code))
    },
    (error) => {
      if (error.response) {
        const { status, data, config } = error.response
        const silent = !!(config && config._silent)

        if (!isJsonResponse(error.response) || typeof data !== 'object') {
          if (import.meta.env?.DEV) {
            console.error('[request] 网关返回非 JSON 响应：', data)
          }
          const msg = t('http.unexpectedResponse')
          if (!silent) showToast(msg)
          return Promise.reject(buildRequestError(msg, error.response))
        }

        const msg = extractServerMessage(data, t('http.requestFailed'))
        const bizCode = data && data.code

        switch (status) {
          case 401:
            // 401 一律清凭据跳登录：silent 请求只是不弹提示，
            // token 已过期时保留它只会造成后续请求反复失败
            if (!silent) {
              showToast(t('http.loginExpired'))
            }
            clearAuthAndGoLogin()
            break
          case 403:
            if (!silent) showToast(t('http.accessDenied'))
            break
          case 404:
            if (!silent) showToast(t('http.notFound'))
            break
          case 429:
            if (!silent) showToast(msg || t('http.tooManyRequests'))
            break
          case 500:
          case 502:
          case 503:
          case 504:
            if (!silent) showToast(t('http.serverError'))
            break
          default:
            if (!silent) showToast(msg)
        }
        return Promise.reject(buildRequestError(msg, error.response, bizCode))
      }

      if (error.request) {
        if (!error.config || !error.config._silent)
          showToast(t('http.networkTimeout'));
        return Promise.reject(error)
      }
      if (!error.config || !error.config._silent)
        showToast(t('http.configError'));
      return Promise.reject(error)
    }
  );
};

let request = createRequestInstance();
addInterceptors()

export function getRequestInstance() {
  return request
}

export const updateRequestConfig = async () => {
  try {
  const apiConfig = getApiConfig()
  const apiBaseUrl = apiConfig.baseUrl || import.meta.env?.VITE_API_BASE_URL || '';
    
    if (request && request.defaults && request.defaults.baseURL === apiBaseUrl) {
      return { baseURL: apiBaseUrl }
    }
    
    request = axios.create({
      baseURL: apiBaseUrl,
      headers: {
        'Content-Type': 'application/json'
      }
    })
    addInterceptors()
    return { baseURL: apiBaseUrl }
  } catch (error) {
    console.error('更新请求配置失败:', error)
    throw error
  }
};

export { http } from './http';

export default request
