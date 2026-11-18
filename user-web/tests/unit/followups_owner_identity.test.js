import { describe, it, expect, vi, beforeEach, afterEach as _afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createPinia } from 'pinia'
import { settle } from './settle-helper'

// 今日跟进的身份腿。
//
// 真机走查实测到的现象：/followups/today 每次进来都顶一条红条"未识别当前登录
// 用户，无法按 owner 隔离跟进数据"，三区恒为 0，且**三条读请求一条都没发出去**。
// 根因不在后端也不在权限：页面自己从 localStorage 读 'user' / 'userInfo' 两个键，
// 而全仓登录写入的键是 'user_info'（stores/user.js）—— 读侧键名对不上，
// 于是 owner 恒为空，页面把"没拿到 id"当成未登录。
//
// 判据因此钉在两端：
//   1) 有身份时，三条读口都带上那个 owner_id（不是"发过请求"这种弱断言）；
//   2) 页面里不出现那句"未识别当前登录用户"；
//   反向一格：身份缺失时红条必须在、且一条请求都不发（那是页明的守卫，不是巧合）。

const api = vi.hoisted(() => ({
  today: vi.fn(),
  pending: vi.fn(),
  overdue: vi.fn(),
  complete: vi.fn(),
  cancel: vi.fn()
}))

const ctx = vi.hoisted(() => ({ me: { id: 7, username: 'admin', email: '', role: 'admin' } }))

vi.mock('@/api/followup.js', () => ({ followupApi: api, default: api }))
vi.mock('@/stores/user', () => ({ useUserStore: () => ({ userInfo: ctx.me }) }))
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: {}, query: {}, path: '/followups/today' }),
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() })
}))

import Today from '@/views/followups/Today.vue'

const EMPTY = { code: 0, data: { list: [] } }

const mountPage = async () => {
  const w = mount(Today, {
    props: {},
    global: { plugins: [createPinia(), ElementPlus] }
  })
  await settle(w)
  return w
}

beforeEach(() => {
  vi.clearAllMocks()
  ctx.me = { id: 7, username: 'admin', email: '', role: 'admin' }
  api.today.mockResolvedValue(EMPTY)
  api.pending.mockResolvedValue(EMPTY)
  api.overdue.mockResolvedValue(EMPTY)
})

describe('今日跟进按当前登录人取数', () => {
  it('三条读口都带上 owner_id，且不报"未识别当前登录用户"', async () => {
    const w = await mountPage()
    expect(api.today.mock.calls[0][0]).toBe('7')
    expect(api.pending.mock.calls[0][0]).toBe('7')
    expect(api.overdue.mock.calls[0][0]).toBe('7')
    expect(w.text()).not.toContain('未识别当前登录用户')
  })

  it('身份从 user store 取，而不是页面上原先那两个无人写入的 localStorage 键', async () => {
    // 反向证据：即便把那两个键塞上别的 id，取数也只认 store。
    localStorage.setItem('user', JSON.stringify({ id: 999 }))
    localStorage.setItem('userInfo', JSON.stringify({ id: 888 }))
    try {
      const w = await mountPage()
      expect(api.today.mock.calls[0][0]).toBe('7')
      expect(w.text()).not.toContain('未识别当前登录用户')
    } finally {
      localStorage.removeItem('user')
      localStorage.removeItem('userInfo')
    }
  })

  it('反向：store 里没有 id 时给出守卫文案且一条读请求都不发', async () => {
    ctx.me = { id: '', username: '', email: '', role: '' }
    const w = await mountPage()
    expect(w.text()).toContain('未识别当前登录用户')
    expect(api.today).not.toHaveBeenCalled()
    expect(api.pending).not.toHaveBeenCalled()
    expect(api.overdue).not.toHaveBeenCalled()
  })
})
