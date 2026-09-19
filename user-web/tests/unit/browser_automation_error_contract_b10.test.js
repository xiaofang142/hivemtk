/**
 * 批10 契约锁：浏览器自动化的 409 必须在客户端可分流。
 *
 * 两条都是真踩过的形态，各配一条用例：
 *  ① 服务端把整族 409 折成同一个 code（DUPLICATE_ENTRY_3003）时，「已有任务执行中」
 *     也会弹出「去装 Host」的引导 → 断的是 bizCode，不是文案；
 *  ② 生产构建里 t() 对每个现存 key 抛 UNEXPECTED_RETURN_TYPE（spec F-N2 实测），
 *     而它的调用点全在给错误对象挂 status/bizCode **之前** → 抛出后调用方拿到的
 *     连 Error 都不是，分流字段全丢 → 断的是「i18n 坏掉时契约仍然在」。
 */
import { describe, it, expect, vi } from 'vitest'

// 用 vi.hoisted 造一个可在用例里开关的 i18n：mock 工厂会被提升，普通外层变量引用不到
const i18nCtl = vi.hoisted(() => ({ throwing: false }))
vi.mock('@/i18n', () => ({
  default: {
    global: {
      t: (key) => {
        if (i18nCtl.throwing) throw new SyntaxError('UNEXPECTED_RETURN_TYPE')
        return key
      },
    },
  },
}))
// element-plus 在无 DOM 环境里装载会碰到 document，弹提示本身不是本用例要测的东西
vi.mock('element-plus', () => ({
  ElMessage: Object.assign(() => {}, {
    error: vi.fn(), warning: vi.fn(), info: vi.fn(), success: vi.fn(),
  }),
}))

const store = {}
globalThis.localStorage = {
  setItem: vi.fn(), getItem: vi.fn(() => null), removeItem: vi.fn(), clear: vi.fn(),
}
globalThis.window = globalThis

import { getRequestInstance } from '@/utils/request.js'

// 复刻 axios 对 HTTP 错误的成形方式：reject 一个 Error 并把 response 挂在上面，
// 拦截器读的就是 error.response.{status,data,config,headers}
function httpError(status, body) {
  const e = new Error(`Request failed with status code ${status}`)
  e.config = { url: '/api/browser-automation/tasks/1/run', method: 'post' }
  e.response = {
    status,
    data: body,
    config: e.config,
    headers: { 'content-type': 'application/json' },
  }
  return e
}

async function runThroughInterceptor(err) {
  const inst = getRequestInstance()
  const prevAdapter = inst.defaults.adapter
  inst.defaults.adapter = async () => { throw err }
  try {
    await inst.post('/api/browser-automation/tasks/1/run', {})
  } catch (caught) {
    return caught
  } finally {
    inst.defaults.adapter = prevAdapter
  }
  throw new Error('拦截器没有按预期 reject')
}

describe('浏览器自动化 409 的客户端契约（批10）', () => {
  it('离线与忙各自带自己的 bizCode，且 HTTP 都是 409', async () => {
    const offline = await runThroughInterceptor(
      httpError(409, { code: 'BROWSER_HOST_OFFLINE_8001', message: '浏览器 Host 未连接' }),
    )
    const busy = await runThroughInterceptor(
      httpError(409, { code: 'BROWSER_TASK_BUSY_8002', message: '已有浏览器任务执行中' }),
    )
    expect(offline.status).toBe(409)
    expect(busy.status).toBe(409)
    expect(offline.bizCode).toBe('BROWSER_HOST_OFFLINE_8001')
    expect(busy.bizCode).toBe('BROWSER_TASK_BUSY_8002')
    // 撞在一起就等于前端分不了流（真机踩过的原状）
    expect(offline.bizCode).not.toBe(busy.bizCode)
  })

  it('i18n 抛错（生产构建现状）时契约字段不能一起丢', async () => {
    i18nCtl.throwing = true
    try {
      const busy = await runThroughInterceptor(
        httpError(409, { code: 'BROWSER_TASK_BUSY_8002', message: '已有浏览器任务执行中' }),
      )
      expect(busy).toBeInstanceOf(Error)
      expect(busy.bizCode).toBe('BROWSER_TASK_BUSY_8002')
      expect(busy.status).toBe(409)
      // 服务端带了文案就用服务端的，不该因为取不到译文而退化成异常对象
      expect(busy.message).toBe('已有浏览器任务执行中')
    } finally {
      i18nCtl.throwing = false
    }
  })

  it('服务端没带 code 时仍给调用方 status 与文案兜底（老网关不能把 UI 晾在静默里）', async () => {
    const e = await runThroughInterceptor(httpError(409, { message: '任务正在执行中，请勿重复触发' }))
    expect(e.status).toBe(409)
    expect(e.bizCode).toBeUndefined()
    expect(e.message).toBe('任务正在执行中，请勿重复触发')
  })
})
