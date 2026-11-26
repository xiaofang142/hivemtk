import { describe, it, expect } from 'vitest'
import platformAccountRoutes from '@/router/modules/platformAccount.js'
import telegramRoutes from '@/router/modules/telegram.js'
import qqRoutes from '@/router/modules/qq.js'
import feishuRoutes from '@/router/modules/feishu.js'
import whatsappRoutes from '@/router/modules/whatsapp.js'
import channelRoutes from '@/router/modules/channels.js'

// T04（2026-11-26）菜单去重：旧「平台账号」与「渠道账号」是同一个事实的重复实现，
// 机器人族同一页面曾有两个路由路径。旧入口只保留重定向，不再挂载半成品页面。
describe('T04 菜单去重：旧路由重定向到统一入口', () => {
  it('平台账号旧路由不挂载页面，重定向到渠道账号', () => {
    const old = platformAccountRoutes.find(r => r.path === '/platformAccount/list')
    expect(old).toBeTruthy()
    expect(old.redirect).toBe('/channels/accounts')
    expect(old.component).toBeUndefined()
    expect(channelRoutes.find(r => r.path === '/channels/accounts')).toBeTruthy()
  })

  it.each([
    ['/telegram', '/telegram/account', telegramRoutes],
    ['/qq', '/qq/account', qqRoutes],
    ['/feishu', '/feishu/account', feishuRoutes],
    ['/whatsapp', '/whatsapp/account', whatsappRoutes]
  ])('%s 机器人别名重定向到 %s，不再重复挂载同一组件', (from, to, routes) => {
    const alias = routes.find(r => r.path === from)
    expect(alias, `${from} 路由缺失`).toBeTruthy()
    expect(alias.redirect).toBe(to)
    expect(alias.component, `${from} 不应再挂组件`).toBeUndefined()
    expect(routes.find(r => r.path === to)?.component, `${to} 必须仍是真实页面`).toBeTruthy()
  })
})
