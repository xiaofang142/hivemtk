import { describe, it, expect, vi, beforeEach } from 'vitest'
import { flattenApiExports, clearMocks, getCalls } from './apitest-helper.js'

// mock 的是 @/utils/request 里那个 http（api 层唯一的出口），不是后端。
const mocks = vi.hoisted(() => {
  const ok = () => Promise.resolve({ list: [], total: 0 })
  const http = {
    get: vi.fn(ok),
    post: vi.fn(ok),
    put: vi.fn(ok),
    delete: vi.fn(ok),
    upload: vi.fn(ok)
  }
  const request = vi.fn(ok)
  request.get = vi.fn(ok)
  request.post = vi.fn(ok)
  request.put = vi.fn(ok)
  request.delete = vi.fn(ok)
  request.upload = vi.fn(ok)
  // apitest-helper 的 clearMocks/getCalls 会读 m.axios.*（三个 api 约定共用一套收集），
  // 本卡的模块只走 http，这里给齐形状只为复用那个收集器。
  const axios = {
    get: vi.fn(async () => ({ data: {} })),
    post: vi.fn(async () => ({ data: {} })),
    put: vi.fn(async () => ({ data: {} })),
    delete: vi.fn(async () => ({ data: {} }))
  }
  return { request, http, axios }
})

vi.mock('@/utils/request', () => ({ default: mocks.request, http: mocks.http }))

const BAD_CASE_PREFIX = '/api/bad-cases'
const HUMAN_TASK_PREFIX = '/api/human-tasks'
const APPROVAL_PREFIX = '/api/approvals'
const INBOX_PREFIX = '/api/inbox'

const urlsOf = (calls) =>
  calls.map((c) => (typeof c === 'string' ? c : c && c.url)).filter((u) => typeof u === 'string')

async function loadBadCase() {
  return (await import('@/api/badCase.js')).badCaseApi
}
async function loadOthers() {
  const [human, approval, inbox] = await Promise.all([
    import('@/api/humanTask.js'),
    import('@/api/approval.js'),
    import('@/api/inbox.js')
  ])
  return [human.humanTaskApi, approval.approvalApi, inbox.inboxApi]
}

// 逐个调用导出方法，收集期间发出的全部 URL。
// 参数按"最多三个占位"给：本卡的方法签名从 (params) 到 (id, label, note) 都有，
// 多给的 undefined 不影响 URL，少给会让函数在拼 URL 前就抛错（那才是要红的地方）。
async function collectUrls(mod) {
  const urls = []
  for (const [name, fn] of flattenApiExports({ api: mod })) {
    clearMocks(mocks)
    await fn('x1', 'x2', 'x3')
    urls.push(...urlsOf(getCalls(mocks)))
    if (!urls.length) throw new Error(`${name} 未发起任何 HTTP 调用`)
  }
  return urls
}

beforeEach(() => clearMocks(mocks))

describe('badCaseApi：每个方法都打到本竖自己的端点', () => {
  it('八条出口全部落在 /api/bad-cases 下（一条都没有 = 模块没导出东西）', async () => {
    const api = await loadBadCase()
    expect(Object.keys(api).length).toBe(8)
    const urls = await collectUrls(api)
    expect(urls.length).toBeGreaterThanOrEqual(8)
    expect(urls.filter((u) => !u.startsWith(BAD_CASE_PREFIX))).toEqual([])
  })

  it('本竖不碰待办/审批/收件箱端点（G-2 与 N-9 是两本账）', async () => {
    const urls = await collectUrls(await loadBadCase())
    const foreign = [HUMAN_TASK_PREFIX, APPROVAL_PREFIX, INBOX_PREFIX]
    expect(urls.filter((u) => foreign.some((p) => u.startsWith(p)))).toEqual([])
  })

  it('反向：待办/审批/收件箱也不碰 /api/bad-cases（隔离是双向的，不然只是换了个方向耦合）', async () => {
    const others = await loadOthers()
    const urls = []
    for (const mod of others) urls.push(...(await collectUrls(mod)))
    expect(urls.filter((u) => u.startsWith(BAD_CASE_PREFIX))).toEqual([])
  })
})

describe('端点形状：与后端 controller/bad_case.go 的契约逐条对齐', () => {
  it('列表把 source/status/label 作为可重复参数原样透传，并带分页', async () => {
    const api = await loadBadCase()
    clearMocks(mocks)
    await api.list({
      source: ['low_confidence', 'manual'],
      status: ['pending', 'labeled'],
      label: ['kb_missing'],
      page: 2,
      page_size: 20
    })
    expect(mocks.http.get).toHaveBeenCalledTimes(1)
    const [url, params] = mocks.http.get.mock.calls[0]
    expect(url).toBe(BAD_CASE_PREFIX)
    expect(params.source).toEqual(['low_confidence', 'manual'])
    expect(params.status).toEqual(['pending', 'labeled'])
    expect(params.page).toBe(2)
  })

  it('stats 打的是 /stats 这个具体路径，不是把 "stats" 当成 id', async () => {
    // 两件事从这一句一起读出来：路径没写错，以及 GET /:id 那条路由不会被 /stats 抢走
    //（gin 里 /stats 与 /:id 在同一层，写错成 `${base}/stats/` 就变成读 id="stats"）。
    const api = await loadBadCase()
    clearMocks(mocks)
    await api.stats()
    const [url, , config] = mocks.http.get.mock.calls[0]
    expect(url).toBe(`${BAD_CASE_PREFIX}/stats`)
    expect(config._silent).toBe(true)
  })

  it('taxonomy 打的是 /taxonomy 且静默（取不到就退回显示英文枚举值，不弹错）', async () => {
    const api = await loadBadCase()
    clearMocks(mocks)
    await api.taxonomy()
    const [url, , config] = mocks.http.get.mock.calls[0]
    expect(url).toBe(`${BAD_CASE_PREFIX}/taxonomy`)
    expect(config._silent).toBe(true)
  })

  it('详情读 /:id', async () => {
    const api = await loadBadCase()
    clearMocks(mocks)
    await api.get('bc_9')
    expect(mocks.http.get.mock.calls[0][0]).toBe(`${BAD_CASE_PREFIX}/bc_9`)
  })

  it('补录是带请求体的 POST，路径就是集合本身', async () => {
    const api = await loadBadCase()
    clearMocks(mocks)
    const input = { session_id: 's1', message_id: 'm1', query_text: '问', answer_text: '答' }
    await api.create(input)
    const [url, body] = mocks.http.post.mock.calls[0]
    expect(url).toBe(BAD_CASE_PREFIX)
    expect(body).toEqual(input)
  })

  it('打标带着类目与判定依据两个字段（后端两个都是 400 判据）', async () => {
    const api = await loadBadCase()
    clearMocks(mocks)
    await api.label('bc_9', 'kb_stale', '库里那条写的是 2024 年的政策')
    const [url, body] = mocks.http.post.mock.calls[0]
    expect(url).toBe(`${BAD_CASE_PREFIX}/bc_9/label`)
    expect(body).toEqual({ label: 'kb_stale', note: '库里那条写的是 2024 年的政策' })
  })

  it('note 即使为空串也显式给出去（省掉字段不会绕过后端那道闸，只会换一句报错）', async () => {
    const api = await loadBadCase()
    clearMocks(mocks)
    await api.label('bc_9', 'kb_stale', '')
    expect(mocks.http.post.mock.calls[0][1]).toEqual({ label: 'kb_stale', note: '' })
    expect('note' in mocks.http.post.mock.calls[0][1]).toBe(true)
  })

  it('撤销必须带理由', async () => {
    const api = await loadBadCase()
    clearMocks(mocks)
    await api.dismiss('bc_9', '这次答案是对的，置信度被否决规则压低了')
    const [url, body] = mocks.http.post.mock.calls[0]
    expect(url).toBe(`${BAD_CASE_PREFIX}/bc_9/dismiss`)
    expect(body).toEqual({ reason: '这次答案是对的，置信度被否决规则压低了' })
  })

  it('导出是 POST /export 且静默（409"没有可导出的样本"是业务结论，不是失败）', async () => {
    const api = await loadBadCase()
    clearMocks(mocks)
    await api.exportSet(['kb_missing'], 50)
    const [url, body, config] = mocks.http.post.mock.calls[0]
    expect(url).toBe(`${BAD_CASE_PREFIX}/export`)
    expect(body).toEqual({ labels: ['kb_missing'], limit: 50 })
    expect(config._silent).toBe(true)
  })

  it('limit 与 labels 都不给时请求体是空对象（后端对"没给"兜默认档，对负数回 400）', async () => {
    // 这里不能写成 limit: null —— Go 侧 int 字段收到 null 会走绑定错误（400），
    // 而"没给这个字段"才是默认档那条路。JSON.stringify 把 undefined 整键丢掉，
    // 所以断言打在**即将上线的那串字节**上，不是打在 JS 对象的关键字表上。
    const api = await loadBadCase()
    clearMocks(mocks)
    await api.exportSet(undefined, undefined)
    const body = mocks.http.post.mock.calls[0][1]
    expect(JSON.stringify(body)).toBe('{}')
  })

  it('坏例 id 进路径前必须编码（id 是 text 列，含 / 或 ? 会改写出别的路由）', async () => {
    const api = await loadBadCase()
    for (const call of [
      () => api.get('a/b?c'),
      () => api.label('a/b', 'kb_missing', '依据'),
      () => api.dismiss('a/b', '理由')
    ]) {
      clearMocks(mocks)
      await call()
      const url = (mocks.http.get.mock.calls[0] || mocks.http.post.mock.calls[0])[0]
      expect(url).not.toContain('/a/b')
      expect(url).toContain('a%2Fb')
    }
  })
})
