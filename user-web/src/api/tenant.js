import { http } from '@/utils/request'

export const TenantApi = {
  // 当前登录租户信息：租户由 JWT 里的 tenant_id 解析，不是请求参数，
  // 所以任何登录用户读到的都是"自己这一家"，天然防越权。
  // tenant_code 就是登录页「租户代码」输入框要填的那串短码。
  getInfo() {
    return http.get('/api/tenant/info')
  },

  // 部署形态：SaaS 多租户(on) 才有「租户代码」这个概念；
  // 私域独立部署(off) 登录页根本不显示这个输入框，展示租户短码只会误导。
  // 与 Login.vue / ExtensionPackage.vue 同一口径：读公开配置 /api/public/config。
  isMultiTenant() {
    return http
      .get('/api/public/config', undefined, { _silent: true })
      .then((res) => {
        const cfg = res?.data ?? res
        return !!cfg?.tenant_mode
      })
      .catch(() => false)
  }
}
