import { defineStore } from 'pinia'
import { ref, computed } from 'vue'

export const useUserStore = defineStore('user', () => {
  const userInfo = ref({
    id: '',
    username: '',
    email: '',
    role: '',
    tenant_id: '',
    tenant_code: ''
  })
  
  const token = ref('')
  
  // P2-7 会话恢复（2026-11-18 二审修复）：刷新后内存令牌丢失，但 HttpOnly Cookie
  // 仍有效 —— isLoggedIn 不能只看内存令牌，否则刷新即被守卫弹回登录页。
  // cookieSession 由 restoreSession() 探测 /api/auth/current-user 后置位；
  // 登出/401 探针失败时清零。
  const cookieSession = ref(false)
  const isLoggedIn = computed(() => !!token.value || cookieSession.value)
  
  const username = computed(() => userInfo.value.username)
  const role = computed(() => userInfo.value.role || '')
  const isAdmin = computed(() => role.value === 'admin');
  
  const setUserInfo = (info) => {
    userInfo.value = {
      id: info.id || '',
      username: info.username || '',
      email: info.email || '',
      role: info.role || '',
      // 租户字段按「缺失就沿用旧值」合并，不是「缺失就清空」。
      // setUserInfo 有两类调用方：登录落库（带完整租户字段）和个人资料页刷新
      // （只带 id/username/email/role）。后者若按 || '' 覆盖，登录时落下的
      // tenant_code 会被抹成空串 —— 于是「登录后记录租户代码」在打开一次个人
      // 中心后就消失了（生产 localStorage 里 tenant_code 恒为空即此因），
      // 连带 HelpCenter 拼 tenant_code 查询参数也变空。
      // 换账号残留由 logout() 显式清空兜住，不靠这里兜。
      tenant_id: info.tenant_id ?? userInfo.value.tenant_id ?? '',
      tenant_code: info.tenant_code ?? userInfo.value.tenant_code ?? ''
    }
    localStorage.setItem('user_info', JSON.stringify(userInfo.value));
  }
  
  const setToken = (newToken) => {
    // JWT 只进内存（HttpOnly Cookie 双通道）：不再持久化进 localStorage，
    // 页面刷新后内存丢失时由 HttpOnly Cookie 在 API 调用时自动认证兜底
    // （axios withCredentials + 后端 JWT 中间件回落读 Cookie）。
    token.value = newToken
  }
  
  const login = (info, userToken) => {
    setUserInfo(info)
    setToken(userToken)
  }
  
  let restorePromise = null
  // restoreSession 刷新后的会话恢复：内存令牌在 ⇒ 直接过；否则若本机有上次登录
  // 的用户档案（user_info 持久化，非 JWT），用 Cookie 探一次 current-user 复核会话。
  // 探测 200 ⇒ cookieSession=true（后续 API 全走 Cookie）；401/异常 ⇒ 清档案弹登录。
  // 结果缓存（单飞），守卫每路由调用零重复网络开销。
  const restoreSession = () => {
    if (token.value) return Promise.resolve(true)
    if (!userInfo.value.id) return Promise.resolve(false)
    if (!restorePromise) {
      restorePromise = (async () => {
        try {
          const raw = (await import('axios')).default
          const res = await raw.get('/api/auth/current-user', { withCredentials: true })
          const user = res?.data?.data
          if (res?.status === 200 && user && user.id) {
            cookieSession.value = true
            setUserInfo(user)
            return true
          }
          logout()
          return false
        } catch {
          logout()
          return false
        }
      })()
    }
    return restorePromise
  }

  const logout = () => {
    userInfo.value = {
      id: '',
      username: '',
      email: '',
      role: '',
      tenant_id: '',
      tenant_code: ''
    }
    token.value = ''
    cookieSession.value = false
    restorePromise = null
    localStorage.removeItem('token')
    localStorage.removeItem('user_info')
  }
  
  const initAuth = () => {
    // 一次性迁移（HttpOnly Cookie 双通道改造）：旧版本持久化在 localStorage 的
    // JWT 迁入内存后立即删除。存量已登录用户本次会话不断线（带旧令牌发请求）；
    // 之后页面刷新内存丢失，由 HttpOnly Cookie 认证兜底，localStorage 不再有 token。
    const legacyToken = localStorage.getItem('token')
    if (legacyToken) {
      token.value = legacyToken
      localStorage.removeItem('token')
    }
    const savedUserInfo = localStorage.getItem('user_info')
    if (savedUserInfo) {
      try {
        const parsed = JSON.parse(savedUserInfo)
        userInfo.value = {
          id: parsed.id || '',
          username: parsed.username || '',
          email: parsed.email || '',
          role: parsed.role || '',
          tenant_id: parsed.tenant_id || '',
          tenant_code: parsed.tenant_code || ''
        }
      } catch {
        localStorage.removeItem('user_info')
      }
    }
  };
  
  initAuth();
  
  return {
    userInfo,
    token,
    isLoggedIn,
    restoreSession,
    username,
    role,
    isAdmin,
    setUserInfo,
    setToken,
    login,
    logout
  }
})
