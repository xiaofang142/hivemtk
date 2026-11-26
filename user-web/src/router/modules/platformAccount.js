// T04（2026-11-26）菜单去重：旧「平台账号」是「渠道账号」的半成品重复实现
// （platform_accounts 表 vs channel_accounts 统一目录）。菜单行已从后端菜单树移除，
// 这里保留旧深链的重定向，老书签/历史消息里的 /platformAccount/list 不会落进 404。
// 不要再挂 List.vue：统一事实源是 /channels/accounts（渠道 → 渠道账号）。
export default [
  {
    path: '/platformAccount/list',
    name: 'PlatformAccountList',
    redirect: '/channels/accounts',
    meta: { title: '渠道账号', group: 'channel', requiresAuth: true }
  }
]
