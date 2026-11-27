import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { settle } from './settle-helper'
import { createPinia } from 'pinia'

// 组件渲染用例：站点设置页上的租户代码展示。
//
// 与 profile_tenant_code_render.test.js 同源判据（模板层证明，不只是 api 层）：
//   1) SaaS(on)：显示租户代码 + 租户名称，短码一字不差；
//   2) 私域(off)：整块隐藏；
//   3) 接口失败：站点配置照常渲染，不白屏。

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

const systemApi = vi.hoisted(() => ({ getConfig: vi.fn(), saveConfig: vi.fn() }))
vi.mock('@/api/system', () => ({
  SystemApi: { getConfig: systemApi.getConfig, saveConfig: systemApi.saveConfig }
}))

import Config from '@/views/system/Config.vue'
import i18n from '@/i18n'

const mountConfig = async () => {
  const wrapper = mount(Config, {
    global: { plugins: [ElementPlus, createPinia(), i18n] }
  })
  await settle()
  return wrapper
}

describe('system/Config.vue 租户代码展示', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    systemApi.getConfig.mockResolvedValue({ site_name: '示例站点' })
  })

  it('SaaS 多租户形态：显示租户代码与租户名称', async () => {
    tenantApi.isMultiTenant.mockResolvedValue(true)
    tenantApi.getInfo.mockResolvedValue({
      tenant_id: 18,
      tenant_code: 'tz3rmdj',
      name: '示例科技有限公司',
      status: 1
    })

    const wrapper = await mountConfig()
    const text = wrapper.text()

    expect(text).toContain('租户代码')
    expect(text).toContain('tz3rmdj')
    expect(text).toContain('示例科技有限公司')
  })

  it('私域独立部署形态：整块隐藏', async () => {
    tenantApi.isMultiTenant.mockResolvedValue(false)
    tenantApi.getInfo.mockResolvedValue({
      tenant_id: 1,
      tenant_code: 'tei6385',
      name: 'E2E计费10191548326',
      status: 1
    })

    const wrapper = await mountConfig()
    const text = wrapper.text()

    expect(text).not.toContain('租户代码')
    expect(text).not.toContain('tei6385')
    expect(text).not.toContain('租户名称')
    expect(tenantApi.getInfo).not.toHaveBeenCalled()
  })

  it('租户接口失败：站点配置照常渲染', async () => {
    tenantApi.isMultiTenant.mockResolvedValue(true)
    tenantApi.getInfo.mockRejectedValue(new Error('租户不存在'))

    const wrapper = await mountConfig()

    expect(wrapper.text()).toContain('站点名称')
    expect(wrapper.text()).not.toContain('租户代码')
  })
})
