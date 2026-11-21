import { computed, onBeforeUnmount, onDeactivated, onMounted, ref, watch } from 'vue'
import { onBeforeRouteLeave, useRoute } from 'vue-router'
import { useUserStore } from '@/stores/user'
import { merchantConnectionApi as api } from '@/api/merchantConnection'

const STATES = ['unconfigured', 'connecting', 'ready', 'reauth', 'refreshing', 'revoked']
const REASONS = ['pending_live_validation', 'action_preflight_required', 'authorization_attempt_expired_or_changed', 'authorization_exchange_failed', 'refresh_in_progress', 'refresh_outcome_unknown', 'refresh_exchange_failed', 'credential_expired_or_changed', 'credential_storage_unavailable', 'credential_expired_refresh_available', 'provider_revocation_unconfirmed']
const bytes = (value) => new TextEncoder().encode(value).length
const identifier = (value, max) => typeof value === 'string' && value.length > 0 && bytes(value) <= max && /^[\p{L}\p{Nd}_.:-]+$/u.test(value)

// 仅页面内存拥有一次授权尝试。服务端负责生成/保管 PKCE verifier；前端不接受它。
export function useMerchantConnection() {
  const user = useUserStore()
  const route = useRoute()
  const provider = ref('')
  const shop = ref('')
  const code = ref('')
  const returnedState = ref('')
  const status = ref(null)
  const fresh = ref(false)
  const busy = ref('')
  const phase = ref('idle')
  const error = ref('')
  const confirmed = ref(false)
  const authorized = computed(() => user.isLoggedIn && user.role === 'admin' && Number(user.userInfo.tenant_id) > 0 && Number(user.userInfo.id) > 0)
  const targetValid = computed(() => authorized.value && identifier(provider.value, 64) && identifier(shop.value, 160))
  const identity = computed(() => JSON.stringify([user.userInfo.tenant_id, user.userInfo.id, user.role, user.isLoggedIn, provider.value, shop.value, route.fullPath]))
  let generation = 0
  let controller
  let statusTimer
  let attemptTimer
  let requestTimer
  let completionPayload
  let authorizationURL = ''
  let expectedState = ''
  let deadline = 0

  const target = () => ({ provider: provider.value, shop_id: shop.value })
  const clearAttempt = () => {
    clearTimeout(attemptTimer)
    authorizationURL = ''
    expectedState = ''
    deadline = 0
    code.value = ''
    returnedState.value = ''
    phase.value = 'idle'
  }
  const invalidate = () => {
    generation++
    controller?.abort()
    clearTimeout(requestTimer)
    clearTimeout(statusTimer)
    clearAttempt()
    if (completionPayload) {
      completionPayload.code = ''
      completionPayload.state = ''
      completionPayload = undefined
    }
    busy.value = ''
    status.value = null
    fresh.value = false
    confirmed.value = false
    error.value = ''
  }
  watch(identity, invalidate, { flush: 'sync' })
  onBeforeRouteLeave(() => { invalidate() })
  onDeactivated(invalidate)
  onMounted(() => window.addEventListener('pagehide', invalidate))
  onBeforeUnmount(() => { invalidate(); window.removeEventListener('pagehide', invalidate) })

  const current = (op) => op.generation === generation && op.identity === identity.value && authorized.value
  const begin = (kind) => {
    if (!targetValid.value || busy.value) return null
    clearTimeout(statusTimer)
    confirmed.value = false
    error.value = ''
    busy.value = kind
    controller = new AbortController()
    const op = { generation: ++generation, identity: identity.value, signal: controller.signal }
    requestTimer = setTimeout(() => {
      if (!current(op)) return
      invalidate()
      // 超时的凭证写入结论未知，只用新请求重读状态，绝不重发写操作。
      if (kind === 'refresh' || kind === 'revoke') void check()
      error.value = 'requestExpired'
    }, 10000)
    return op
  }
  const finish = (op) => {
    if (!current(op)) return false
    clearTimeout(requestTimer)
    busy.value = ''
    return true
  }
  const accept = (value) => {
    if (!value || value.provider !== provider.value || (value.shop_id && value.shop_id !== shop.value) || !STATES.includes(value.connection_state) ||
      (value.reason_code && !REASONS.includes(value.reason_code))) throw new Error('invalid safe status')
    const hasCredential = value.connection_state === 'ready' || value.refresh_available === true || value.credential_ref != null || value.credential_version != null
    if (hasCredential && (value.shop_id !== shop.value || !Number.isSafeInteger(value.credential_version) || value.credential_version <= 0 ||
      !Number.isSafeInteger(value.credential_ref) || value.credential_ref <= 0 || !Number.isFinite(Date.parse(value.expires_at)))) throw new Error('invalid credential status')
    // 不保留接口上意外出现的任何字段、错误或秘密。
    status.value = {
      state: value.connection_state,
      reason: value.reason_code || '',
      version: Number.isSafeInteger(value.credential_version) && value.credential_version > 0 ? value.credential_version : 0,
      reference: Number.isSafeInteger(value.credential_ref) && value.credential_ref > 0 ? value.credential_ref : 0,
      refresh: value.refresh_available === true,
      expires: typeof value.expires_at === 'string' ? Date.parse(value.expires_at) : 0
    }
    fresh.value = true
    confirmed.value = false
    clearTimeout(statusTimer)
    statusTimer = setTimeout(() => { fresh.value = false; confirmed.value = false }, 30000)
  }
  const check = async () => {
    if (phase.value !== 'idle') return
    const op = begin('status')
    if (!op) return
    fresh.value = false
    status.value = null
    try {
      const value = await api.get(target(), op.signal)
      if (current(op)) accept(value)
    } catch {
      if (current(op)) error.value = 'statusFailed'
    } finally { finish(op) }
  }
  const pending = computed(() => ['connecting', 'refreshing'].includes(status.value?.state))
  const blocked = computed(() => status.value?.reason === 'pending_live_validation')
  const canStart = computed(() => targetValid.value && fresh.value && !!status.value && !blocked.value && !busy.value && !pending.value && phase.value === 'idle')
  const credentialCurrent = computed(() => targetValid.value && fresh.value && !!status.value?.version && !!status.value?.reference && !blocked.value && !pending.value && !busy.value && phase.value === 'idle' && ['ready', 'reauth'].includes(status.value.state))
  const canRefresh = computed(() => credentialCurrent.value && status.value.refresh)
  const canRevoke = computed(() => credentialCurrent.value && confirmed.value)

  const start = async () => {
    if (!canStart.value) return
    const op = begin('start')
    if (!op) return
    clearAttempt()
    fresh.value = false
    try {
      const value = await api.start(target(), op.signal)
      if (!current(op)) return
      const url = new URL(value?.authorization_url)
      const states = url.searchParams.getAll('state')
      const forbidden = ['code', 'code_verifier', 'access_token', 'refresh_token', 'client_secret', 'id_token']
      const until = Date.parse(value?.expires_at)
      if (url.protocol !== 'https:' || url.username || url.password || url.hash || url.hostname === window.location.hostname ||
        states.length !== 1 || !/^[A-Za-z0-9_-]{43}$/.test(states[0]) || forbidden.some(key => url.searchParams.has(key)) ||
        !Number.isFinite(until) || until <= Date.now() || until - Date.now() > 10 * 60 * 1000) throw new Error('invalid authorization handoff')
      authorizationURL = url.href
      expectedState = states[0]
      deadline = until
      phase.value = 'prepared'
      attemptTimer = setTimeout(() => {
        if (!current(op)) return
        invalidate()
        error.value = 'attemptExpired'
      }, until - Date.now())
    } catch {
      if (current(op)) { clearAttempt(); status.value = null; error.value = 'startFailed' }
    } finally { finish(op) }
  }
  const openAuthorization = () => {
    if (!authorized.value || busy.value || phase.value !== 'prepared' || deadline <= Date.now()) return
    try {
      // 不创建 href，不将授权 URL 放入 DOM、路由或 referrer。窗口句柄不被保留。
      window.open(authorizationURL, '_blank', 'noopener,noreferrer')
      authorizationURL = ''
      phase.value = 'opened'
    } catch {
      invalidate()
      error.value = 'startFailed'
    }
  }
  const canComplete = computed(() => targetValid.value && !busy.value && phase.value === 'opened' && code.value.length > 0 && bytes(code.value) <= 4096 &&
    !/\p{Cc}/u.test(code.value) && !/:\/\/|^[?#]|[?&#](?:code|state|token)=/i.test(code.value) && returnedState.value === expectedState)
  const complete = async () => {
    if (!canComplete.value || deadline <= Date.now()) return
    const payload = { ...target(), code: code.value, state: returnedState.value }
    clearAttempt()
    const op = begin('complete')
    if (!op) return
    status.value = null
    fresh.value = false
    completionPayload = payload
    try {
      const value = await api.complete(payload, op.signal)
      if (current(op)) accept(value)
    } catch {
      if (current(op)) error.value = 'completeFailed'
    } finally {
      payload.code = ''
      payload.state = ''
      if (completionPayload === payload) completionPayload = undefined
      finish(op)
    }
  }
  const lifecycle = async (kind) => {
    if (kind === 'refresh' ? !canRefresh.value : !canRevoke.value) return
    const payload = { ...target() }
    if (kind === 'refresh') payload.credential_version = status.value.version
    const op = begin(kind)
    if (!op) return
    fresh.value = false
    try {
      const value = await api[kind](payload, op.signal)
      if (current(op)) accept(value)
    } catch {
      if (!current(op)) return
      // 失败/代次冲突只重读摘要，绝不自动重发写操作。
      status.value = null
      error.value = kind === 'refresh' ? 'refreshFailed' : 'revokeFailed'
      try {
        const value = await api.get(target(), op.signal)
        if (current(op)) accept(value)
      } catch { /* 固定本地文案；原始异常不得流向全局日志或 toast。 */ }
    } finally { finish(op) }
  }
  return { provider, shop, code, returnedState, status, fresh, busy, phase, error, confirmed, authorized, targetValid, pending, blocked,
    canStart, canComplete, canRefresh, canRevoke, check, start, openAuthorization, complete, invalidate,
    refresh: () => lifecycle('refresh'), revoke: () => lifecycle('revoke') }
}
