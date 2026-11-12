import { describe, it, expect } from 'vitest'
import { fallbackLandingPath } from '@/router/index'

// 2026-11-13 SaaS 注册实测实锤的回退缺口：零菜单新用户登录 → /messageHub/list
// 不可访问 → 旧回退硬编码 /inbox/list 也无授权 → 直接 403（守卫注释的意图是
// 「避免登录后直接 403」，被自己的回退打破）。修复后：空菜单回退 /profile
// （personalPaths 白名单成员，恒可达）。
describe('fallbackLandingPath 非管理员落地回退', () => {
  it('菜单树为空（新注册用户）→ /profile，不再落 403', () => {
    expect(fallbackLandingPath([])).toBe('/profile')
    expect(fallbackLandingPath(undefined)).toBe('/profile')
    expect(fallbackLandingPath(null)).toBe('/profile')
  })

  it('有授权菜单 → 首个授权菜单', () => {
    expect(fallbackLandingPath(['/messageHub/list', '/oneid/list'])).toBe('/messageHub/list')
    expect(fallbackLandingPath(['/faq'])).toBe('/faq')
  })

  it('入参非数组（store 未初始化形态）→ 安全回退 /profile', () => {
    expect(fallbackLandingPath({})).toBe('/profile')
    expect(fallbackLandingPath('x')).toBe('/profile')
  })
})
