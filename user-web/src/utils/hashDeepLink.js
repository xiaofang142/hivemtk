// T08：兼容无 `#` 的渠道深链。
//
// 本应用使用 hash 路由（createWebHashHistory）。用户与外部文档常把两条渠道页写成
// `/channels/accounts`、`/channels/identify-codes`（不带 `#`）。这类裸路径到达 SPA 后
// `location.hash` 为空，router 会按默认落地页导航（实测落到 `#/messageHub/list`），
// 目标页根本没有打开 —— 这正是「访问报错 / 页面打不开」的真实根因。
//
// 归一化只认 `/channels/` 前缀，且保留 query。不处理其他路径，避免无界重定向把
// 服务端路由（/api、/files 等）或首页误伤。判定为纯函数，便于单测；实际跳转由
// `normalizeChannelHashDeepLink` 调用 `location.replace` 完成（整页重载一次，
// 让 hash history 在正确 hash 下初始化，不会形成循环）。

export function channelHashRedirect(location) {
  const pathname = location?.pathname || ''
  const search = location?.search || ''
  const hash = location?.hash || ''

  if (!pathname.startsWith('/channels/')) return null
  if (hash.startsWith('#/channels/')) return null

  return `/#${pathname}${search}`
}

export function normalizeChannelHashDeepLink(location) {
  const target = channelHashRedirect(location)
  if (!target) return false
  location.replace(target)
  return true
}

export default normalizeChannelHashDeepLink
