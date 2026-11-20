import { describe, it, expect } from 'vitest'
import { createPinia } from 'pinia'
import { usePermissionStore } from '@/stores/permission'

// 2026-11-13 SaaS 注册实测实锤的回退缺口：零菜单新用户登录 → /messageHub/list
// 不可访问 → 旧回退硬编码 /inbox/list 也无授权 → 直接 403（守卫注释的意图是
// 「避免登录后直接 403」，被自己的回退打破）。修复后：空菜单回退 /profile
// （personalPaths 白名单成员，恒可达）。
describe('非管理员落地回退', () => {
  it('菜单树为空（新注册用户）→ /profile，不再落 403', () => {
    const permission = usePermissionStore(createPinia())
    expect(permission.getMenuReturnPath()).toBe('/profile')
  })

  it('有授权菜单 → 首个授权菜单', () => {
    const permission = usePermissionStore(createPinia())
    permission.menusLoaded = true
    permission.menus = [{ key: 'workspace', children: [{ path: '/oneid/list' }, { path: '/faq' }] }]
    expect(permission.getMenuReturnPath(undefined, '/messageHub/list')).toBe('/oneid/list')
  })

  it('分组路径不对应该角色的可见叶子 → 首个实际授权入口', () => {
    const permission = usePermissionStore(createPinia())
    permission.menusLoaded = true
    permission.menus = [{ key: 'workspace', path: '/messageHub/list', children: [{ path: '/faq' }] }]
    expect(permission.getMenuReturnPath(undefined, '/messageHub/list')).toBe('/faq')
  })
})
