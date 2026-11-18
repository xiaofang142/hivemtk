// 商户电脑设备信标（任务三）。
//
// 背景：SaaS 形态下心跳原本只带云端服务器（user-server 所在机）的信息，
// 平台侧无法回答"商户此刻在用哪台电脑"。本工具在登录态下、每个浏览器
// 会话发一帧轻量信标 POST /api/client-device/report，user-server 侧从
// 请求中采集出口 IP 与 User-Agent，连同浏览器自报的屏幕/时区/语言合并成
// 设备快照，随后由心跳协程作为 client_machine 字段随帧上报平台。
//
// 口径：
//   - 每个浏览器会话（sessionStorage）最多一帧——设备信息低频变化，
//     页面加载即刷新，足够让平台看到"最近在用的那台电脑"。
//   - 离线单机部署（TENANT_MODE=off）下服务端静默拒收（recorded=false），
//     那台机器的口径就是部署机本身，无需浏览器快照。
import { useUserStore } from '@/stores/user'

const REPORT_URL = '/api/client-device/report'
const SENT_FLAG = 'hivemtk-client-device-reported'

function collectDevicePayload() {
  let platform = ''
  try {
    platform = navigator.userAgentData?.platform || navigator.platform || ''
  } catch {
    platform = ''
  }
  let screen = ''
  try {
    screen = `${window.screen.width}x${window.screen.height}`
  } catch {
    screen = ''
  }
  let timezone = ''
  try {
    timezone = Intl.DateTimeFormat().resolvedOptions().timeZone || ''
  } catch {
    timezone = ''
  }
  let language = ''
  try {
    language = navigator.language || ''
  } catch {
    language = ''
  }
  return { platform, screen, timezone, language }
}

/**
 * 上报一帧设备信标（幂等：同一浏览器会话只发一次）。
 *
 * 登录态判断与 webVitalsMonitor 同口径：内存令牌存在才发；刷新后 Cookie
 * 兜底会话（restoreSession 成功、cookieSession=true）也发——同源 fetch
 * 默认带 HttpOnly Cookie，服务端回落认证放行。
 * 信标属 best-effort 观测面，任何失败都静默，不影响主流程。
 */
export const reportClientDeviceOnce = () => {
  try {
    if (sessionStorage.getItem(SENT_FLAG)) return
    const store = useUserStore()
    if (!store.token && !store.cookieSession) return
    const payload = collectDevicePayload()
    const headers = { 'Content-Type': 'application/json' }
    if (store.token) headers.Authorization = `Bearer ${store.token}`
    fetch(REPORT_URL, {
      method: 'POST',
      headers,
      body: JSON.stringify(payload),
      keepalive: true,
    })
      .then((res) => {
        // 只有服务端真正收下（recorded=true，即 SaaS 形态）才标记本会话已报，
        // 离线形态（recorded=false）不标记——切到 SaaS 形态后下一会话仍可上报。
        if (res.ok) sessionStorage.setItem(SENT_FLAG, '1')
      })
      .catch(() => {})
  } catch {
    // 静默：信标属 best-effort 观测面
  }
}
