import axios from 'axios';
import { ElMessage } from 'element-plus'
import { getApiConfig } from './configManager'
import i18n from '@/i18n'
import { getStoredLocale } from '@/i18n/locale'
import { markUserReported } from './errorReporting'
import { createTokenRefresher } from './tokenRefresh'
// OPT-FE-05（2026-10-24）：信封判定/业务码/错误形状抽到 @hivemtk/http-core
// （与 bridge/platform-web 迁移后共用；拦截器装配仍留本文件——应用侧差异）。
import {
  BIZ_CODE_MODULE_NOT_ENABLED,
  BIZ_CODE_SUBSCRIPTION_EXPIRED,
  isBizSuccess,
  isJsonContentType,
  extractServerMessage as sharedExtractServerMessage,
  createBizError
} from '../../../packages/http-core/index.js'

// 生产构建里 vue-i18n 的预编译消息编译器对全部现存 key 抛 UNEXPECTED_RETURN_TYPE
// （spec §6：dev/prod 一致，1834/1834）。这里必须兜住：t() 的调用点全在拦截器给错误对象
// 挂 status/bizCode 之前，一抛出去整条错误分支就变成 toast「SyntaxError」，
// 调用方拿到的连 Error 都不是，分流字段自然全丢。取不到译文就退回 key 本身
// （和 vue-i18n 自己遇到缺 key 的行为一致），全站 i18n 的正解仍按 §6 单独排期。
const t = (key) => {
  try {
    return i18n.global.t(key)
  } catch {
    return key
  }
}

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
    } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
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

// ============================================================================
// 功能模块闸（.specify/002-modular-subscription · 业务码 4301）
//
// 为什么必须在这里拦，而不是让每个页面各判一次：
// 未开通模块的接口**返回 HTTP 200 + 体内 code 4301**（本仓错误通道约定），
// 于是它会落进上面 `code === 0` 之外的通用分支 —— 只弹一句 toast。
// 用户看到的是"操作失败"，而事实是"你没买这个功能"。他既不知道为什么，
// 也不知道该去哪买 —— 于是多半会反复重试，或者以为系统坏了。
//
// 拦在这里的好处是**全站一致**：任何深链、任何页面、任何写法都得到同一句
// 引导 + 同一个去处。加它只需在业务码表登记一行，不会有页面漏掉。
// ============================================================================
// ============================================================================
// 订阅到期（003 T023 · 业务码 4305）
//
// 与 4301 的两条关键差别：
//   ① **不受 silent 短路影响**：订阅到期是全局状态，不是"某个页面点错了"——
//      后台轮询撞到 4305 同样要把用户带去续费墙（silent 只免 toast）。
//   ② 跳转走 sessionStorage 防抖 10s：expired 态下全站接口都会 4305，
//      不防抖就是同一秒内几十次 location 赋值 + toast。
// 4305/4301 跳转互斥节流（修订 #3）：两者共用同一个节流键，10s 内
// 只允许一次跳转发生 —— 墙页与市场页依赖请求集不得互相触发对方业务码。
// 业务码数值已迁 @hivemtk/http-core，import 进来直接用。
// ============================================================================
const SUBSCRIPTION_WALL_REDIRECT = '/subscription-wall'
const SUB_WALL_THROTTLE_KEY = 'biz_redirect_throttle'
const SUB_WALL_THROTTLE_MS = 10000

// redirectThrottled 10s 内已有任何业务码跳转发生（4301/4305 互斥节流）
function redirectThrottled () {
  try {
    const raw = sessionStorage.getItem(SUB_WALL_THROTTLE_KEY)
    if (!raw) return false
    const at = Number(raw)
    if (Date.now() - at < SUB_WALL_THROTTLE_MS) return true
    return false
  } catch { return false }
}

function markRedirected () {
  try { sessionStorage.setItem(SUB_WALL_THROTTLE_KEY, String(Date.now())) } catch { /* 隐私模式下静默 */ }
}

function handleSubscriptionExpired (data, code) {
  if (Number(code) !== BIZ_CODE_SUBSCRIPTION_EXPIRED) return false
  // toast 照弹（看得见原因），但跳转受 10s 节流 —— 不看 silent：
  // 全局状态一律跳墙，silent 只影响"要不要打断当前页面的静默轮询提示"。
  showToast(data?.message || '订阅已到期，业务功能已暂停，请续费后继续使用', 'warning')
  if (!redirectThrottled()) {
    markRedirected()
    setTimeout(() => redirectTo(SUBSCRIPTION_WALL_REDIRECT), 800)
  }
  return true
}

// MODULE_NOT_ENABLED_REDIRECT 未开通模块时的去处：功能模块市场。
// 带 next 参数让市场页高亮/预选该模块，省掉用户自己找的一步。
const MODULE_NOT_ENABLED_REDIRECT = '/module-market'

// silent=true 时（调用方带了 `_silent`，多为后台轮询/页面预取）仍按 4301 拒绝——
// 调用方自己的 catch 必须照常拿到 bizCode——但**不 toast、不安排跳转**：
// 这类请求的失败本就该由页面自己吞掉，把它当「用户点错了入口」处理会把人
// 从正在看的页面上强行拽走（MyModules 拉 llm/wallet 被踢去市场页就是这么来的）。
// 非 silent 路径一个字都不改：仍是那句引导 toast + 3 秒后跳市场页。
function handleModuleNotEnabled (data, code, silent) {
  if (Number(code) !== BIZ_CODE_MODULE_NOT_ENABLED) return false
  if (silent) return true
  const moduleKey = data && data.data && data.data.module_key
  const target = moduleKey
    ? `${MODULE_NOT_ENABLED_REDIRECT}?highlight=${encodeURIComponent(moduleKey)}`
    : MODULE_NOT_ENABLED_REDIRECT
  // 不直接跳：用户可能是误点深链（别人发的链接），跳走会丢上下文。
  // 给 5 秒缓冲 + 可关闭提示，足够他看清原因，也给误点留了退路。
  showToast(`功能模块「${moduleKey || ''}」未开通，请在功能模块市场订阅后使用`, 'warning')
  // 与 4305 跳转互斥节流（003 修订 #3）：10s 内已发生过业务码跳转就不再跳
  if (!redirectThrottled()) {
    markRedirected()
    setTimeout(() => redirectTo(target), 3000)
  }
  return true
}

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

// isJsonResponse / extractServerMessage / buildRequestError 的形状逻辑迁
// @hivemtk/http-core；这里只保留应用侧差异：t() 兜底文案与
// markUserReported 上报挂载。
function isJsonResponse(resp) {
  return isJsonContentType(resp && resp.headers)
}

function extractServerMessage(data, fallback) {
  return sharedExtractServerMessage(data, fallback)
}

function buildRequestError(message, response, bizCode) {
  // status / bizCode 都是给调用方分流用的机器读数字段：只靠 message 文案判分支，
  // 服务端换一次措辞（或切了语言）就把逻辑判错——浏览器任务的「忙」与「Host 离线」都栽过这条。
  return createBizError(message || t('http.requestFailed'), response, bizCode, markUserReported)
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

// 取这条请求实际带出去的令牌（axios v1 的 config.headers 是 AxiosHeaders 实例）
function bearerSentWith(config) {
  const headers = config && config.headers
  if (!headers) return ''
  let raw = ''
  if (typeof headers.get === 'function') raw = headers.get('Authorization') || ''
  if (!raw) raw = headers.Authorization || headers.authorization || ''
  raw = String(raw)
  return raw.startsWith('Bearer ') ? raw.slice(7).trim() : ''
}

// 401 说的是"这把令牌不被认"，不是"此刻 localStorage 里这把不被认"。
// 迟到的响应（用户在这条请求返回之前已经重新登录）会把刚换到的新令牌连带会话一起抹掉，
// 表现为"登录成功后过一会儿又被踢回登录页"；没带令牌的请求（访客端点自己回 401）
// 更不该动站点的登录态。第一个 401 清完令牌后 localStorage 就空了，
// 并发的其余 401 因此既不再重复清、也不再弹第二条"登录已过期"。
function expireSession(sentWith) {
  const current = localStorage.getItem('token')
  if (!current) return false
  if (!sentWith || sentWith !== current) return false
  clearAuthAndGoLogin()
  return true
}

// ============================================================================
// 令牌主动续期（防 24h 到期硬登出）
//
// JWT 有效期 24h，旧实现没有任何刷新路径：用户干着活令牌到期，
// 下一条请求 401 → 清凭据踢回登录页，进度全丢。现在请求侧主动看一眼
// exp：剩余寿命不足 15 分钟就先用旧令牌换新（后端 refresh-token 会把旧令牌
// 拉黑），再发业务请求。刷新必须在令牌仍然有效时发生——过期令牌会被
// 中间件直接拒绝，15 分钟窗口就是给这条路径留的余量。
// ============================================================================

// 续期的单飞锁、401 分档与“不覆盖并发新令牌”全在 tokenRefresh.js 里，
// request 拦截器与 chatSocket 共用同一份状态（这是修“偶发被踢回登录”的关键：
// 两个入口各自 refresh 会让后端轮换两次，后一次必 401，旧失败分支还会清掉新令牌）。
const tokenRefresher = createTokenRefresher({
  getToken: () => localStorage.getItem('token'),
  setToken: (t) => localStorage.setItem('token', t),
  onSessionInvalid: () => clearAuthAndGoLogin(),
  refreshCall: async (token) => {
    // 裸 axios 实例：不挂业务拦截器——刷新请求自身绝不能再触发续期 / 401 清会话
    const raw = axios.create({
      baseURL: request.defaults.baseURL,
      headers: { 'Content-Type': 'application/json' }
    })
    const res = await raw.post('/api/auth/refresh-token', null, {
      headers: { Authorization: `Bearer ${token}` },
      _skipRefresh: true
    })
    // 响应是信封 {code, data:{token}}，裸实例不拆信封
    return res?.data?.data?.token
  }
})

// ensureFreshToken 确保 localStorage 里的令牌剩余寿命充足，不足则静默换发。
// 失败语义：只有「明确 401 且仍是同一把令牌」才清凭据跳登录（且不 toast）；
// 网络错误 / 5xx / 并发轮换期间令牌已被别处换新，一律保留会话。
function ensureFreshToken() {
  return tokenRefresher.refresh()
}

// chatSocket 等非 axios 入口复用同一把单飞锁；force=true 用于 WS 4001（鉴权失败）。
export function refreshAccessToken(options) {
  return tokenRefresher.refresh(options)
}

const addInterceptors = () => {
  request.interceptors.request.use(
    async (config) => {
      // 主动续期：先确保令牌新鲜再带上。失败（清凭据跳登录已发生）也照常放行，
      // 让这条请求自然走 401 兜底——刷新失败拦在这里只会掩盖真实错误。
      if (!config._skipRefresh) {
        try {
          await ensureFreshToken()
        } catch { /* 已静默清凭据并跳登录，请求继续即可 */ }
      }
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
        } catch { /* 忽略：清理/存储/恢复类 best-effort 操作 */ }
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

      if (isBizSuccess(code)) {
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
      // 4305：订阅已到期。处理函数不受 silent 短路（全局状态一律跳墙），
      // 且与 4301 跳转互斥节流 —— 先于 4301 判定。
      if (handleSubscriptionExpired(data, code)) {
        return Promise.reject(buildRequestError(msg, response, code))
      }
      // 4301：未开通模块。处理函数已自行提示并安排跳转，这里**不再**弹通用 toast
      // （两句提示叠在一起只会让人更困惑）。
      if (handleModuleNotEnabled(data, code, silent)) {
        return Promise.reject(buildRequestError(msg, response, code))
      }
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
          case 401: {
            // 只有"存储里这把令牌正是被打回的那把"才清凭据跳登录。
            // silent 请求同样要清——token 已过期时留着它只会让后续请求反复失败。
            if (expireSession(bearerSentWith(config)) && !silent) {
              showToast(t('http.loginExpired'))
            }
            break
          }
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
            if (!silent) showToast(t('http.serverError'))
            break
          // 502/503/504 是「上游/依赖不可用」而不是本服务内部炸了：
          // 后端通常已给出可照做的原因（如渠道返回的 code/msg），
          // 一律盖成「服务器内部错误」会让用户错判并盲目重试。
          case 502:
          case 503:
          case 504:
            if (!silent) showToast(msg || t('http.serverError'))
            break
          default:
            if (!silent) showToast(msg)
        }
        return Promise.reject(buildRequestError(msg, error.response, bizCode))
      }

      if (error.request) {
        if (!error.config || !error.config._silent)
          showToast(t('http.networkTimeout'));
        return Promise.reject(markUserReported(error))
      }
      if (!error.config || !error.config._silent)
        showToast(t('http.configError'));
      return Promise.reject(markUserReported(error))
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
