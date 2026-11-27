import { describe, it, expect, vi, beforeEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { settle } from './settle-helper'
import { createPinia } from 'pinia'

// 注册页租户代码用例：SaaS 多租户下「加入已有商户」与「新商户开户」两条路径分流。
//
// 为什么要有这一份：注册页原先只有开户一条路（POST /api/tenant/register），
// 后端其实早就支持「带租户代码加入已有商户」（POST /api/auth/register，
// on 模式必填 tenant_code），但前端 0 处调用 —— 商户给员工开通账号只能走后台。
//
// 断言的是**打到了哪个接口、带了什么字段**：
//   1) 填了租户代码 → 打 /api/auth/register 且带 tenant_code，不打 /api/tenant/register；
//   2) 留空         → 仍走 /api/tenant/register（原有开户路径不能被带偏）；
//   3) off 形态     → 租户代码框不渲染（私域部署没有租户概念）。

const httpMock = vi.hoisted(() => ({ post: vi.fn(), get: vi.fn() }))
vi.mock('@/utils/request', () => ({
  http: { post: httpMock.post, get: httpMock.get, put: vi.fn() }
}))

const tenantApi = vi.hoisted(() => ({ isMultiTenant: vi.fn(), getInfo: vi.fn() }))
vi.mock('@/api/tenant', () => ({
  TenantApi: { isMultiTenant: tenantApi.isMultiTenant, getInfo: tenantApi.getInfo }
}))

vi.mock('@/api/moduleMarket', () => ({
  regulatoryNotice: vi.fn().mockResolvedValue({
    notice: '告知内容',
    agreement_version: '2026-10-17.v1',
    required_fields: []
  })
}))

const userStoreMock = vi.hoisted(() => ({ login: vi.fn() }))
vi.mock('@/stores/user', () => ({
  useUserStore: () => ({ login: userStoreMock.login })
}))

vi.mock('@/stores/permission', () => ({
  usePermissionStore: () => ({ resetMenus: vi.fn() })
}))

import Register from '@/views/register/Register.vue'
import i18n from '@/i18n'

const routerMock = vi.hoisted(() => ({ push: vi.fn() }))
vi.mock('vue-router', () => ({
  useRouter: () => routerMock
}))

const mountRegister = async () => {
  const wrapper = mount(Register, {
    global: { plugins: [ElementPlus, createPinia(), i18n] }
  })
  await settle()
  return wrapper
}

// 填第 1 步（账户信息）+ 第 2 步（开户登记）两组表单字段。
// 第 2 步的 el-form 没有 ref，validateGroup 实际校验的是第 1 步那张表
// ⇒ 开户登记四项在客户端并不真的拦人，所以这里逐个填上，保证打出去的
// 载荷是「用户认真填完」的形状。
const fillAccount = async (wrapper, { username, email, password }) => {
  await wrapper.find('input[placeholder*="3-32"]').setValue(username)
  await wrapper.find('input[placeholder*="至少 8 位"]').setValue(password)
  await wrapper.findAll('input[type="password"]')[1].setValue(password)
  await wrapper.find('input[placeholder*="用于登录"]').setValue(email)
}

const fillRegistration = async (wrapper) => {
  await wrapper.find('input[placeholder*="营业执照"]').setValue('示例科技有限公司')
  await wrapper.find('input[placeholder*="负责人/法人姓名"]').setValue('张负责人')
  await wrapper.find('input[placeholder*="手机号或固定电话"]').setValue('13800138000')
  await wrapper.find('input[placeholder*="监管问询联系"]').setValue('四川省成都市高新区天府大道 1 号')
  await settle()
}

const goToLastStep = async (wrapper) => {
  for (let i = 0; i < 2; i++) {
    const nextBtn = wrapper.findAll('button').find((b) => b.text().includes('下一步'))
    await nextBtn.trigger('click')
    await settle()
  }
  await wrapper.find('input[type="checkbox"]').setValue(true)
}

const clickSubmit = async (wrapper) => {
  const submitBtn = wrapper.findAll('button').find((b) => b.text().includes('完成注册'))
  await submitBtn.trigger('click')
  await settle()
}

describe('register/Register.vue 租户代码', () => {
  beforeEach(() => {
    vi.clearAllMocks()
    httpMock.get.mockResolvedValue({})
    userStoreMock.login.mockImplementation(() => {})
  })

  it('SaaS 形态：渲染租户代码输入框', async () => {
    tenantApi.isMultiTenant.mockResolvedValue(true)
    const wrapper = await mountRegister()
    const text = wrapper.text()
    expect(text).toContain('租户代码')
    expect(wrapper.find('input[placeholder*="加入已有商户"]').exists()).toBe(true)
  })

  it('私域形态：不渲染租户代码输入框', async () => {
    tenantApi.isMultiTenant.mockResolvedValue(false)
    const wrapper = await mountRegister()
    expect(wrapper.text()).not.toContain('租户代码')
    expect(wrapper.find('input[placeholder*="加入已有商户"]').exists()).toBe(false)
  })

  it('填了租户代码 → 走 /api/auth/register 且带上 tenant_code', async () => {
    tenantApi.isMultiTenant.mockResolvedValue(true)
    httpMock.post.mockResolvedValue({
      token: 'jwt-join',
      user: { id: 99, username: 'staff', email: 'staff@x.com', role: 'user' },
      tenant_id: 18,
      tenant_code: 'tz3rmdj'
    })

    const wrapper = await mountRegister()
    await fillAccount(wrapper, { username: 'staff', email: 'staff@x.com', password: 'Hive@Test123' })
    await wrapper.find('input[placeholder*="加入已有商户"]').setValue('tz3rmdj')
    await settle()
    await goToLastStep(wrapper)
    await fillRegistration(wrapper)
    await clickSubmit(wrapper)

    const calls = httpMock.post.mock.calls
    const joinCall = calls.find((c) => c[0] === '/api/auth/register')
    const openCall = calls.find((c) => c[0] === '/api/tenant/register')
    expect(joinCall, '应调用 /api/auth/register').toBeTruthy()
    expect(openCall, '不应调用 /api/tenant/register').toBeFalsy()
    expect(joinCall[1].tenant_code).toBe('tz3rmdj')
    expect(joinCall[1].username).toBe('staff')
    expect(joinCall[1].real_name).toBe('张负责人')
    // 登录落库时把租户代码记进去
    expect(userStoreMock.login).toHaveBeenCalled()
    expect(userStoreMock.login.mock.calls[0][0].tenant_code).toBe('tz3rmdj')
  })

  it('留空租户代码 → 仍走 /api/tenant/register 开户路径', async () => {
    tenantApi.isMultiTenant.mockResolvedValue(true)
    httpMock.post.mockResolvedValue({
      tenant_id: 30, tenant_code: 'tnew123', token: 'jwt-open'
    })

    const wrapper = await mountRegister()
    await fillAccount(wrapper, { username: 'boss', email: 'boss@x.com', password: 'Hive@Test123' })
    await settle()
    await goToLastStep(wrapper)
    await fillRegistration(wrapper)
    await clickSubmit(wrapper)

    const calls = httpMock.post.mock.calls
    const openCall = calls.find((c) => c[0] === '/api/tenant/register')
    const joinCall = calls.find((c) => c[0] === '/api/auth/register')
    expect(openCall, '应调用 /api/tenant/register').toBeTruthy()
    expect(joinCall, '不应调用 /api/auth/register').toBeFalsy()
    expect(openCall[1].tenant_code).toBeUndefined()
    expect(openCall[1].legal_person).toBe('张负责人')
  })
})
