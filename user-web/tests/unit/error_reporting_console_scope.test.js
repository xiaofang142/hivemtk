import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import axios from 'axios'

// 全局兜底该打谁、该放过谁。
//
// 走查里量到的两类噪声：点确认框的"取消"（reject('cancel')）在控制台留一条
// `[Vue errorHandler] component event handler cancel`；服务端 500 时用户已经看到
// "服务器内部错误"的红条，同一条失败又在控制台打第二遍。前者不是错误，后者已经报过了。
//
// 但放过必须是"有凭据才放过"，不能顺手关掉整个控制台：
//   - 凭据是 request.js 在 reject 前盖的 Symbol 标记，手写 Error 伪造不出来；
//   - 只认 ElMessageBox 那两个确切的取消值，其余字符串照打。
// 组件自己抛异常、渲染期报错、缺 key 的 TypeError 都还得原样出声，
// 否则这条兜底就从"防白屏无报错"变成了"防报错"。
//
// 处理器本身的接线（main.js 里那两处 return）由浏览器那条腿量：
// tests 里 import main.js 会真的挂载整个应用，代价与收益不成比例。

import { markUserReported, isUserReportedError } from '@/utils/errorReporting'

// request 模块在文件加载期静态导入一次，不再每条用例 vi.resetModules() 后重新实例化。
// 原因（AV-07 压力复现实录）：beforeEach 里的 `await import('@/utils/request')` 会把
// element-plus + i18n + http-core 整张模块图重新求值一遍，CPU 被抢占时单次 import
// 就能越过 vitest 默认的 hookTimeout 10000ms（12 个 CPU burner 下 3/5 轮翻车，
// 翻的都是"Hook timed out in 10000ms"）；而且 hook 一挂 afterEach 里的
// errSpy.mockRestore() 会因为 errSpy 还没赋值再补一刀 TypeError，把原失败盖掉。
// 静态导入让 hook 全部变成同步代码：无 hookTimeout 暴露面，清理必然有 spy 可还原。
// 每用例共用同一实例后要复核的共享状态只有两处，都成立：
//   - showToast 的 2500ms 去重：四条腿的提示文案（服务器内部错误 / 无权访问 /
//     网络超时 / _silent 不提示）互不相同，不会互相抑制；
//   - defaults.adapter：每条用例 adapterReply 都先覆写再发请求，不依赖全新实例。
import request from '@/utils/request'

function adapterReply(status, body, extra = {}) {
  request.defaults.adapter = async (config) => {
    // AxiosError 的位序是 (message, code, config, request, response)。
    // noResponse 必须排在 status>=400 之前判：它是"根本没拿到响应"那一档，
    // status 传 0 时若先按状态码分流，这条腿会静默走成功返回、再掉进软失败分支，
    // 用例名字里写的 error.request 分支就一次也没被跑到（先前就是这么假绿的）。
    if (extra.noResponse) {
      throw new axios.AxiosError('Network Error', 'ERR_NETWORK', config, { url: config.url }, null)
    }
    if (status >= 400) {
      // response 必须带 content-type 和 config：少前者掉进"网关非 JSON"分支，
      // 少后者 _silent 读不到，用例就变成给一条它声称没测的路径盖章。
      const response = {
        data: body,
        status,
        statusText: 'Error',
        headers: { 'content-type': 'application/json' },
        config
      }
      throw new axios.AxiosError(`Request failed with status code ${status}`, 'ERR_BAD_REQUEST', config, response, response)
    }
    return { data: body, status, statusText: 'OK', headers: { 'content-type': 'application/json' }, config }
  }
}

async function rejectionOf(promise) {
  try {
    await promise
    return { caught: false }
  } catch (err) {
    return { caught: true, err }
  }
}

describe('errorReporting：兜底只放过"已经对用户报过"的那两类', () => {
  it('标记过的错误对象算已报，未标记的原生异常不算', () => {
    const marked = markUserReported(new Error('boom'))
    expect(isUserReportedError(marked)).toBe(true)
    expect(isUserReportedError(new Error('boom'))).toBe(false)
  })

  it('确认框的两个取消值算已报，其余字符串不算', () => {
    expect(isUserReportedError('cancel')).toBe(true)
    expect(isUserReportedError('close')).toBe(true)
    expect(isUserReportedError('cancelAll')).toBe(false)
    expect(isUserReportedError('用户名或密码错误')).toBe(false)
  })

  it('标记是非枚举字符串键：enumerable 自检与序列化都带不出去（防伪造凭据混进日志）', () => {
    const marked = markUserReported(new Error('boom'))
    expect(Object.keys(marked)).toEqual([])
    expect(JSON.stringify({ e: marked })).toBe('{"e":{}}')
    expect(marked.__userReported__).toBe(true)
    // 拦截器造的错误对象也一样：多挂的 status/bizCode 是可枚举的机器读数，标记不在其中
    const biz = markUserReported(Object.assign(new Error('boom'), { status: 500, bizCode: 'X' }))
    expect(Object.keys(biz).sort()).toEqual(['bizCode', 'status'])
  })

  it('模块被重新实例化后旧标记仍认得出（Symbol 键会在 resetModules/HMR 下换枚新的）', async () => {
    const before = markUserReported(new Error('boom'))
    vi.resetModules()
    const fresh = await import('@/utils/errorReporting')
    expect(fresh.isUserReportedError(before)).toBe(true)
    // 反向：另一侧新造的标记，本侧也认得——两侧必须共用同一把字符串键
    expect(isUserReportedError(fresh.markUserReported(new Error('x')))).toBe(true)
  })

  it('空值与非对象理由不得抛异常，且一律判为未报', () => {
    for (const v of [null, undefined, 0, '', 'cancel ', { message: 'x' }]) {
      expect(() => isUserReportedError(v)).not.toThrow()
    }
    expect(isUserReportedError({ message: 'x' })).toBe(false)
    expect(isUserReportedError(404)).toBe(false)
    expect(markUserReported('字符串')).toBe('字符串')
    expect(markUserReported(null)).toBe(null)
  })
})

describe('拦截器给每条"用户已经被告知"的拒绝盖凭据', () => {
  let errSpy

  // 顺序即安全：先装 spy、清 body，再做任何可能失败的事（这里已经没有 await，
  // hook 不可能半途而废）。即便将来有人往前几行加回 await，spy 也已就位，
  // afterEach 的还原不会因为"spy 还没装上"而抛 TypeError 掩盖原始失败。
  beforeEach(() => {
    document.body.innerHTML = ''
    errSpy = vi.spyOn(console, 'error').mockImplementation(() => {})
  })

  afterEach(() => {
    // ?.：beforeEach 若在装 spy 之前失败，errSpy 仍是 undefined —— 清理跳过，
    // 让用例停在它自己的原始错误上，而不是被清理代码的 TypeError 盖掉。
    errSpy?.mockRestore()
    errSpy = undefined
    document.body.innerHTML = ''
  })

  // 分支自证：夹具掉进"网关非 JSON"分支时也会盖标记，那等于给一条它声称没测的路径盖章。
  const onIntendedBranch = () =>
    expect(errSpy.mock.calls.flat().join(' ')).not.toContain('网关返回非 JSON')

  it('500：红条文案之外，拒绝对象带已报标记（兜底因此不再打第二遍）', async () => {
    adapterReply(500, { code: 'ERR', message: 'boom' })
    const { caught, err } = await rejectionOf(request.get('/api/x'))
    expect(caught).toBe(true)
    expect(isUserReportedError(err)).toBe(true)
    onIntendedBranch()
    expect(document.querySelectorAll('.el-message--error').length).toBe(1)
  })

  it('_silent：页面自己有告警位，同样算已报', async () => {
    adapterReply(500, { code: 'ERR', message: 'boom' })
    // axios 实例的签名是 get(url, config)；三参数是仓库 http 包装层的形状。
    // 传成第三个参数时 _silent 根本进不了 config，红条照弹，用例量的就不是那条分支。
    const { err } = await rejectionOf(request.get('/api/x', { _silent: true }))
    expect(isUserReportedError(err)).toBe(true)
    onIntendedBranch()
    // 没读到 config 的话这里会弹红条——弹了就说明 _silent 那档根本没进对分支
    expect(document.querySelectorAll('.el-message').length).toBe(0)
  })

  it('业务码非 0（HTTP 200 的软失败）也带凭据', async () => {
    adapterReply(200, { code: 'FORBIDDEN_2002', message: '无权访问' })
    const { err } = await rejectionOf(request.get('/api/x'))
    expect(isUserReportedError(err)).toBe(true)
    expect(err.message).toBe('无权访问')
  })

  it('网络层失败（有 error.request、没有 response）同样已提示超时文案 ⇒ 带凭据', async () => {
    adapterReply(0, {}, { noResponse: true })
    const { caught, err } = await rejectionOf(request.get('/api/x'))
    expect(caught).toBe(true)
    expect(isUserReportedError(err)).toBe(true)
    // 网络层这条拒的是 axios 原错（没有 buildRequestError 的 status 字段），
    // 断 ERR_NETWORK 才是「打的确实是 error.request 分支」的证据。
    expect(err.code).toBe('ERR_NETWORK')
  })
})
