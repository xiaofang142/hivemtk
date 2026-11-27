import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { settle } from './settle-helper'
import { createPinia } from 'pinia'

// 组件渲染用例：证明"登录后能在页面上看到租户代码"这条需求**穿过了模板**，而不只是活在 api 层。
//
// 为什么 api 层绿了还要这一份：api/tenant.js 返回 tenant_code 只说明数据取得到，
// 模板完全可以无视它——有人在 v-if 上写错条件、或者干脆没绑这个字段，api 层一个字都不用改。
// 所以这里数的是**渲染出来的租户代码**：SaaS 多租户形态下要出现，短码要一字不差。
//
// 三条判据：
//   1) SaaS(on)：显示租户代码 + 租户名称，且代码与接口返回一致；
//   2) 私域(off)：整块隐藏——登录页没有「租户代码」输入框，显示短码只会让人去找不存在的概念；
//   3) 接口失败：页面照常渲染，不弹错误、不白屏。

const tenantApi = vi.hoisted(() => ({
  getInfo: vi.fn(),
  isMultiTenant: vi.fn()
}))

vi.mock('@/api/tenant', () => ({
  TenantApi: {
    getInfo: tenantApi.getInfo,
    isMultiTenant: tenantApi.isMultiTenant
  }
}))

const httpMock = vi.hoisted(() => ({ get: vi.fn() }))
vi.mock('@/utils/request', () => ({
  http: { get: httpMock.get, put: vi.fn(), post: vi.fn() }
}))

vi.mock('@/stores/permission', () => ({
  usePermissionStore: () => ({ menus: [], ensureMenus: vi.fn(), menuPaths: [] })
}))

import Profile from '@/views/Profile.vue'
import i18n from '@/i18n'

const mountProfile = async () => {
  const wrapper = mount(Profile, {
    global: { plugins: [ElementPlus, createPinia(), i18n] }
  })
  await settle()
  return wrapper
}

describe('Profile.vue 租户代码展示', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    httpMock.get.mockResolvedValue({
      id: 26,
      username: 'uit_admin',
      email: 'uit_admin@uitest.local',
      role: 'admin',
      status: 1
    })
  })

  it('SaaS 多租户形态：显示租户代码与租户名称，代码一字不差', async () => {
    tenantApi.isMultiTenant.mockResolvedValue(true)
    tenantApi.getInfo.mockResolvedValue({
      tenant_id: 18,
      tenant_code: 'tz3rmdj',
      name: '示例科技有限公司',
      status: 1
    })

    const wrapper = await mountProfile()
    const text = wrapper.text()

    expect(text).toContain('租户代码')
    expect(text).toContain('tz3rmdj')
    expect(text).toContain('租户名称')
    expect(text).toContain('示例科技有限公司')
  })

  it('私域独立部署形态：整块隐藏，不展示租户代码', async () => {
    tenantApi.isMultiTenant.mockResolvedValue(false)
    tenantApi.getInfo.mockResolvedValue({
      tenant_id: 1,
      tenant_code: 'tei6385',
      name: 'E2E计费10191548326',
      status: 1
    })

    const wrapper = await mountProfile()
    const text = wrapper.text()

    expect(text).not.toContain('租户代码')
    expect(text).not.toContain('tei6385')
    expect(text).not.toContain('租户名称')
    // 私域形态连接口都不该打——没有租户概念，打了也是白打
    expect(tenantApi.getInfo).not.toHaveBeenCalled()
  })

  it('租户接口失败：页面照常渲染账号信息，不白屏', async () => {
    tenantApi.isMultiTenant.mockResolvedValue(true)
    tenantApi.getInfo.mockRejectedValue(new Error('租户不存在'))

    const wrapper = await mountProfile()
    const text = wrapper.text()

    expect(text).toContain('uit_admin')
    expect(text).not.toContain('租户代码')
  })
})
