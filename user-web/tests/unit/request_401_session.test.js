import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flushPromises } from '@vue/test-utils'
import axios from 'axios'

// 401 的处理只该动"这条请求带出去的那把令牌"。
//
// 原来 case 401 无条件 clearAuthAndGoLogin()，两种情况下会误伤：
//   1) 迟到的 401——请求发出时会话还有效，等它回来用户已经重新登录换了一把新令牌，
//      旧响应把新令牌抹掉 ⇒ "登录成功后过一会儿又被踢回登录页"；
//   2) 本来没登录却打到 401（登录页口令错、访客端点自己回 401），也照响一条"登录已过期"。
// 顺带：并发 401 原来会各自清一遍、各跳一次；现在第一条清完令牌，其余在 expireSession
// 里就判定"没有会话可终结"了。
//
// 断言落在用户能感觉到的两件事上：localStorage 的最终状态、跳了几次登录页——
// 不是内部调用计数。

const routerPush = vi.hoisted(() => vi.fn())

vi.mock('@/router', () => ({
  default: { push: (...args) => routerPush(...args) }
}))

import requestModule from '@/utils/request'

const ENVELOPE_401 = { code: 'UNAUTHORIZED', message: 'token expired' }

// showToast 按"同文案 2.5 秒内只响一次"去重，那份状态挂在模块上（lastToastMsg/lastToastTs）。
// 一个文件里跑多条 401，从第二条起红条就被它压掉——"没弹提示"这类断言会因此假绿。
// 所以每条用例重新 import 一次模块，让去重状态从零开始。
let request = requestModule

async function freshRequest() {
  vi.resetModules()
  const m = await import('@/utils/request')
  request = m.default
}

// 用自定义适配器把请求钉死成 401。axios v1 的 settle 只在内置适配器里跑，
// 所以这里自己造 AxiosError（config/response 都得带上，拦截器读的就是这两个字段）。
// onSend 在"请求已经带着 Authorization 出去、还没收到响应"这一刻执行——
// 想模拟飞行途中换令牌，只能挂在这里；挂在调用处会赶在请求拦截器之前，测到的是另一件事。
function always401(onSend) {
  request.defaults.adapter = async (config) => {
    if (onSend) onSend(config)
    const response = {
      data: ENVELOPE_401,
      status: 401,
      statusText: 'Unauthorized',
      headers: { 'content-type': 'application/json' },
      config
    }
    throw new axios.AxiosError('Request failed with status code 401', 'ERR_BAD_REQUEST', config, null, response)
  }
}

// redirectTo 走 import('@/router')，是条 microtask；请求失败之后还得等它落进 DOM/调用记录
async function hit(path = '/api/whatever') {
  let out = { rejected: false }
  try {
    await request.get(path)
  } catch (e) {
    out = { rejected: true, status: e.status, message: e.message }
  }
  await flushPromises()
  return out
}

describe('request 拦截器：401 只在"打的是当前这把令牌"时终结会话', () => {
  beforeEach(async () => {
    localStorage.clear()
    window.location.hash = '#/customerService/csat'
    document.body.innerHTML = ''
    routerPush.mockReset()
    await freshRequest()
    always401()
  })

  it('令牌过期：清掉 token/refreshToken、跳一次登录页、错误照旧抛给调用方', async () => {
    localStorage.setItem('token', 'jwt-old')
    localStorage.setItem('refreshToken', 'rt-old')

    const out = await hit()

    expect(out.rejected).toBe(true)
    expect(localStorage.getItem('token')).toBe(null)
    expect(localStorage.getItem('refreshToken')).toBe(null)
    expect(routerPush).toHaveBeenCalledTimes(1)
    expect(routerPush).toHaveBeenCalledWith('/login')
  })

  it('迟到的 401 判据要看发出去的那把令牌：换令牌发生在响应回来之前', async () => {
    localStorage.setItem('token', 'jwt-A')
    let sent = ''
    always401((config) => {
      const h = config.headers
      sent = typeof h.get === 'function' ? String(h.get('Authorization') || '') : String(h.Authorization || '')
      localStorage.setItem('token', 'jwt-B')
    })
    await hit()

    expect(sent).toBe('Bearer jwt-A')
    expect(localStorage.getItem('token')).toBe('jwt-B')
    expect(routerPush).not.toHaveBeenCalled()
  })

  it('本来就没登录时打到 401（登录页口令错、访客端点自己回 401）：不弹"登录已过期"、不跳转', async () => {
    localStorage.removeItem('token')

    const out = await hit('/api/auth/login')

    expect(out.rejected).toBe(true)
    expect(routerPush).not.toHaveBeenCalled()
    // 会话终结提示只在真的终结了会话时才响——否则用户看到的是两条毫不相干的报错
    expect(Array.from(document.querySelectorAll('.el-message')).map((n) => n.textContent.trim())).toEqual([])
  })

  it('并发 401：会话只终结一次', async () => {
    localStorage.setItem('token', 'jwt-shared')
    // 数的是"令牌被抹了几次"，不是"跳了几次登录页"：redirectTo 里那一次 import('@/router')
    // 是真异步，三条并发 401 各自排队，跳转计数会受 promise 落地的先后影响（实测坏代码下也只显示 1 次）——
    // 拿它当判据等于把这条腿交给时序，而不是交给判据。
    const removeSpy = vi.spyOn(Storage.prototype, 'removeItem')
    await Promise.all([hit('/api/a'), hit('/api/b'), hit('/api/c')])

    const tokenClears = removeSpy.mock.calls.filter((c) => c[0] === 'token').length
    removeSpy.mockRestore()
    expect(localStorage.getItem('token')).toBe(null)
    expect(tokenClears).toBe(1)
  })

  it('401 仍然把错误交给调用方（status/bizCode 供分流用，不能变成静默 resolve）', async () => {
    localStorage.setItem('token', 'jwt-old')
    const out = await hit()
    expect(out.rejected).toBe(true)
    expect(out.status).toBe(401)
    expect(out.message).toBe(ENVELOPE_401.message)
  })

  it('会话真的终结时，"登录已过期"响一条', async () => {
    localStorage.setItem('token', 'jwt-old')
    await hit()
    const toasts = Array.from(document.querySelectorAll('.el-message')).map((n) => n.textContent.trim())
    expect(toasts.length).toBe(1)
  })

  it('夹具自证：请求确实带着 localStorage 里那把令牌出去', async () => {
    localStorage.setItem('token', 'jwt-check-header')
    let seen = ''
    always401((config) => {
      const h = config.headers
      seen = typeof h.get === 'function' ? String(h.get('Authorization') || '') : String(h.Authorization || '')
    })
    await hit()
    expect(seen).toBe('Bearer jwt-check-header')
  })
})
