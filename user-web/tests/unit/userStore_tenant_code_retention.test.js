import { describe, it, expect, beforeEach, vi as _vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'

// 回归用例：「登录后记录下来的租户代码」不能在打开个人中心后被抹掉。
//
// 缺陷史：stores/user.js 的 setUserInfo 原先对每个字段都是 `info.x || ''`，
// 而个人资料页刷新用户信息时调用 setUserInfo 只传 {id, username, email, role}
// —— tenant_id/tenant_code 两个 key 压根不存在，`|| ''` 于是把登录时落库的
// tenant_code 覆盖成空串。结果：
//   1) localStorage.user_info.tenant_code 恒为空 ⇒「记录租户代码」名存实亡；
//   2) 连带 HelpCenter 拼 tenant_code 查询参数也是空（SaaS 下会缺租户声明）。
//
// 修法：租户字段按「缺失就沿用旧值」合并（info.tenant_id ?? 旧值），
// 换账号残留由 logout() 显式清空兜住。

describe('stores/user 租户代码留存', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    localStorage.clear()
  })

  it('登录落库时写入租户代码', async () => {
    const { useUserStore } = await import('@/stores/user')
    const store = useUserStore()

    store.login({
      id: 18,
      username: 'admin',
      email: 'a@b.com',
      role: 'admin',
      tenant_id: 18,
      tenant_code: 'tz3rmdj'
    }, 'jwt-token')

    expect(store.userInfo.tenant_code).toBe('tz3rmdj')
    expect(store.userInfo.tenant_id).toBe(18)
    expect(JSON.parse(localStorage.getItem('user_info')).tenant_code).toBe('tz3rmdj')
  })

  it('个人资料页只传部分字段时，租户代码不被抹掉', async () => {
    const { useUserStore } = await import('@/stores/user')
    const store = useUserStore()

    store.login({
      id: 18,
      username: 'admin',
      email: 'a@b.com',
      role: 'admin',
      tenant_id: 18,
      tenant_code: 'tz3rmdj'
    }, 'jwt-token')

    // 个人资料页刷新：只有 id/username/email/role，没有租户字段
    store.setUserInfo({
      id: 18,
      username: 'admin',
      email: 'new-mail@b.com',
      role: 'admin'
    })

    expect(store.userInfo.tenant_code).toBe('tz3rmdj')
    expect(store.userInfo.tenant_id).toBe(18)
    expect(JSON.parse(localStorage.getItem('user_info')).tenant_code).toBe('tz3rmdj')
    // 非租户字段照常更新
    expect(store.userInfo.email).toBe('new-mail@b.com')
  })

  it('登出后租户代码被清空，不残留到下一个账号', async () => {
    const { useUserStore } = await import('@/stores/user')
    const store = useUserStore()

    store.login({
      id: 18, username: 'admin', email: 'a@b.com', role: 'admin',
      tenant_id: 18, tenant_code: 'tz3rmdj'
    }, 'jwt-token')

    store.logout()

    expect(store.userInfo.tenant_code).toBe('')
    expect(store.userInfo.tenant_id).toBe('')
    expect(localStorage.getItem('user_info')).toBeNull()
  })

  it('显式传入的新租户代码覆盖旧值（换租户场景不粘旧值）', async () => {
    const { useUserStore } = await import('@/stores/user')
    const store = useUserStore()

    store.login({
      id: 18, username: 'admin', email: 'a@b.com', role: 'admin',
      tenant_id: 18, tenant_code: 'tz3rmdj'
    }, 'jwt-token')

    store.setUserInfo({
      id: 18, username: 'admin', email: 'a@b.com', role: 'admin',
      tenant_id: 19, tenant_code: 'tmk45zs'
    })

    expect(store.userInfo.tenant_code).toBe('tmk45zs')
    expect(store.userInfo.tenant_id).toBe(19)
  })
})
