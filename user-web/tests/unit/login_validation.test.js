import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { createPinia } from 'pinia'
import { settleBody } from './settle-helper'

// 登录页的"表单没填"和"登录失败"必须是两件事。
//
// 真机 /#/login 点空表单的"登录"时，控制台打出 `登录失败: {username:[…],password:[…]}`、
// 右上角弹出红条"登录失败"，而两个输入框下面已经写着"请输入用户名/请输入密码"。
// Element Plus 的 validate() 在校规不过时 reject 的是**字段错误集合**（不是 Error），
// 它和"口令错/服务端挂了"一起落进同一个 catch ⇒ 把用户的漏填报成了系统故障。
// 真机用户看到"登录失败"会去翻口令，而不是去补用户名。
//
// 这一份只管 handleLogin 的分支走向，校验本身交给浏览器那条腿（tests/audit 的 /login 全元素点击腿
// 实测过：空表单点登录 → 零 POST、零 console.error、零红条，行内提示两条都在）。
// 为什么不在这儿用真 el-form 断言行内提示：jsdom 里 el-form 的 validate() 对空 required 直接 resolve
// true（最小复现：`<el-form :rules="{a:[{required:true}]}">` 加一个空 input，validate() 仍是 true），
// 所以这里把 validate 的结果显式喂进来——用例断言的是"校验说不通过之后，前端做了什么"，
// 而"校验说什么通过"由 Element Plus 和浏览器负责。

const apiLogin = vi.hoisted(() => vi.fn())
const routerPush = vi.hoisted(() => vi.fn())
// 登录页在 <script setup> 顶层就发 GET /api/public/config（自助注册开关，未登录可取）。
// 不桩掉 @/utils/request 的话，jsdom 里那条请求拿不到响应，走 request.js 的
// error.request 分支弹一条「网络超时」红条 —— 于是"校验不通过 ⇒ 零红条"这条判据
// 量的其实是这条后台配置请求，而不是登录分支。桩成"开关关"，让页面停在登录态。
const httpGet = vi.hoisted(() => vi.fn().mockResolvedValue({ data: { enable_register: false } }))
const httpPost = vi.hoisted(() => vi.fn())

vi.mock('@/api/users', () => ({ usersApi: { login: apiLogin } }))
vi.mock('@/utils/request', () => ({
  default: vi.fn(),
  http: { get: httpGet, post: httpPost },
  getRequestInstance: () => ({ interceptors: { response: { use: vi.fn() } } })
}))
vi.mock('vue-router', () => ({
  useRouter: () => ({ push: routerPush }),
  useRoute: () => ({ path: '/login', query: {} })
}))

import i18n from '@/i18n'
import Login from '@/views/Login.vue'

const t = i18n.global.t

// 替掉 el-form 只为了把 validate() 的结果变成用例的参数：暴露同名方法，其余（ref 句柄、
// @keyup.enter）保持原样，模板里的 el-form-item / el-input / el-button 仍走真实组件。
function formStub(validateImpl) {
  return {
    name: 'ElForm',
    template: '<form class="stub-form"><slot /></form>',
    setup(_, { expose }) {
      expose({ validate: validateImpl, validateField: validateImpl, resetFields() {}, clearValidate() {} })
    }
  }
}

function mountLogin(validateImpl) {
  return mount(Login, {
    global: {
      plugins: [ElementPlus, createPinia()],
      stubs: validateImpl ? { ElForm: formStub(validateImpl) } : {},
      components: {},
      mocks: {}
    },
    attachTo: document.body
  })
}

// 点击后 validate() 是 promise，EP 的字段校验链还要再排几轮 microtask 才落到 ElMessage 的 DOM。
// 表单 attachTo body、提示条也挂在 body 上，所以用整页 DOM 做稳定判据（settleBody）。
async function clickLogin(wrapper) {
  const buttons = wrapper.findAll('.el-button').filter((b) => b.text() === t('core.login.submit'))
  expect(buttons.length).toBe(1)
  await buttons[0].trigger('click')
  await settleBody()
}

const errorToasts = () => Array.from(document.querySelectorAll('.el-message--error')).map((n) => n.textContent.trim())
const successToasts = () => Array.from(document.querySelectorAll('.el-message--success')).map((n) => n.textContent.trim())

const VALID = { username: 'e2e_admin', password: 'p-123456' }
const fill = async (wrapper) => {
  await wrapper.find(`input[placeholder="${t('core.login.username')}"]`).setValue(VALID.username)
  await wrapper.find(`input[placeholder="${t('core.login.password')}"]`).setValue(VALID.password)
}

describe('Login.vue：漏填≠登录失败，真失败仍要出声', () => {
  let consoleError

  beforeEach(() => {
    document.body.innerHTML = ''
    apiLogin.mockReset()
    routerPush.mockReset()
    consoleError = vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    consoleError.mockRestore()
  })

  it('校验不通过：不发登录请求、不打 console.error、不弹红条', async () => {
    // reject 的值就是 Element Plus 真机给的形状：字段 → 错误数组，没有 message
    const reject = () => Promise.reject({
      username: [{ message: t('core.login.pleaseUsername'), field: 'username' }],
      password: [{ message: t('core.login.pleasePassword'), field: 'password' }]
    })
    const wrapper = mountLogin(reject)
    await clickLogin(wrapper)

    expect(apiLogin).not.toHaveBeenCalled()
    expect(consoleError).not.toHaveBeenCalled()
    expect(errorToasts()).toEqual([])
  })

  it('校验通过：发一次登录请求，成功后存令牌并跳首页', async () => {
    apiLogin.mockResolvedValue({ token: 'jwt-1', user: { id: 1, role: 'admin' } })
    const wrapper = mountLogin(() => Promise.resolve(true))
    await fill(wrapper)
    await clickLogin(wrapper)

    expect(apiLogin).toHaveBeenCalledTimes(1)
    // handleLogin 把整个 loginForm 交出去。登录表单只有账号 + 密码两个字段：
    // 此前内联的注册表单会在登录态带出一个空 email，那是被内联注册拖累出来的
    // 残留 —— 注册独立成页（/register）后这个字段没有任何用途。
    expect(apiLogin.mock.calls[0][0]).toEqual({ username: VALID.username, password: VALID.password })
    expect(routerPush).toHaveBeenCalledWith('/')
    expect(errorToasts()).toEqual([])
    expect(successToasts()).toEqual([t('core.login.success')])
  })

  it('提交请求未完成时再次点击不会重复登录', async () => {
    let resolveLogin
    apiLogin.mockReturnValueOnce(new Promise((resolve) => { resolveLogin = resolve }))
    const wrapper = mountLogin(() => Promise.resolve(true))
    await fill(wrapper)
    const button = wrapper.findAll('.el-button').find((b) => b.text() === t('core.login.submit'))
    await button.trigger('click')
    await button.trigger('click')
    expect(apiLogin).toHaveBeenCalledTimes(1)
    resolveLogin({ token: 'jwt-locked', user: { id: 1, role: 'admin' } })
    await settleBody()
  })

  it('密码框回车只触发一次提交', async () => {
    apiLogin.mockResolvedValue({ token: 'jwt-enter', user: { id: 1, role: 'admin' } })
    const wrapper = mountLogin(() => Promise.resolve(true))
    await fill(wrapper)
    await wrapper.find(`input[placeholder="${t('core.login.password')}"]`).trigger('keyup.enter')
    await settleBody()
    expect(apiLogin).toHaveBeenCalledTimes(1)
  })

  it('服务端报错：红条还在，文案用后端给的 message', async () => {
    apiLogin.mockRejectedValue(new Error('用户名或密码错误'))
    const wrapper = mountLogin(() => Promise.resolve(true))
    await fill(wrapper)
    await clickLogin(wrapper)

    expect(errorToasts()).toEqual(['用户名或密码错误'])
    expect(routerPush).not.toHaveBeenCalled()
  })

  it('响应里没有 token：仍报"服务器返回数据异常"，不能静默停在登录页', async () => {
    apiLogin.mockResolvedValue({ code: 0, data: null })
    const wrapper = mountLogin(() => Promise.resolve(true))
    await fill(wrapper)
    await clickLogin(wrapper)

    expect(errorToasts()).toEqual([t('core.login.failServer')])
    expect(routerPush).not.toHaveBeenCalled()
  })

  it('夹具自证：两个输入框按 placeholder 各命中 1 个、登录按钮只 1 个', () => {
    const wrapper = mountLogin(null)
    expect(wrapper.findAll(`input[placeholder="${t('core.login.username')}"]`).length).toBe(1)
    expect(wrapper.findAll(`input[placeholder="${t('core.login.password')}"]`).length).toBe(1)
    expect(wrapper.findAll('.el-button').filter((b) => b.text() === t('core.login.submit')).length).toBe(1)
  })
})
