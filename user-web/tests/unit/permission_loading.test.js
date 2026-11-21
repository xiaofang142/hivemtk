import { describe, it, expect, vi, beforeEach } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
const menus = vi.hoisted(() => vi.fn())
vi.mock('@/api/menu', () => ({ getUserMenus: menus }))
import { useUserStore } from '@/stores/user'
import { usePermissionStore } from '@/stores/permission'
beforeEach(() => {
  localStorage.clear()
  setActivePinia(createPinia())
  menus.mockReset()
  useUserStore().login({ id: 'viewer', role: 'viewer' }, 'fixture-token')
})
describe('菜单权限失败与并发', () => {
  it('菜单未加载或加载失败都不放行业务路径', async () => {
    const store = usePermissionStore()
    expect(store.canAccessPath('/clue/list')).toBe(false)
    menus.mockRejectedValue(new Error('controlled failure'))
    await expect(store.ensureMenus()).rejects.toThrow('controlled failure')
    expect(store.canAccessPath('/clue/list')).toBe(false)
  })
  it('并发调用必须一起等待同一次菜单读取', async () => {
    let resolve
    menus.mockReturnValue(new Promise(r => { resolve = r }))
    const store = usePermissionStore()
    const first = store.ensureMenus()
    let secondDone = false
    const second = store.ensureMenus().then(() => { secondDone = true })
    await Promise.resolve()
    expect(secondDone).toBe(false)
    resolve({ tree: [{ path: '/clue/list' }], all: false })
    await Promise.all([first, second])
    expect(menus).toHaveBeenCalledTimes(1)
    expect(store.canAccessPath('/clue/list')).toBe(true)
    expect(store.canAccessPath('/system/users')).toBe(false)
  })
  it('重置后旧账号响应不得恢复旧菜单和全量权限', async () => {
    let resolve
    menus.mockReturnValueOnce(new Promise(r => { resolve = r }))
    const store = usePermissionStore()
    const first = store.ensureMenus()
    store.resetMenus()
    resolve({ tree: [{ path: '/system/users' }], all: true })
    await first
    expect(store.canAccessPath('/system/users')).toBe(false)
    expect(store.menus).toEqual([])
  })
})
