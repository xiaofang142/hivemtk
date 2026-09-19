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

const HUMAN_TASK_PREFIX = '/api/human-tasks'
const APPROVAL_PREFIX = '/api/approvals'
const INBOX_PREFIX = '/api/inbox'

const urlsOf = (calls) =>
  calls.map((c) => (typeof c === 'string' ? c : c && c.url)).filter((u) => typeof u === 'string')

async function loadHumanTask() {
  return (await import('@/api/humanTask.js')).humanTaskApi
}
async function loadApproval() {
  return (await import('@/api/approval.js')).approvalApi
}
async function loadInbox() {
  return (await import('@/api/inbox.js')).inboxApi
}

// 逐个调用导出方法，收集期间发出的全部 URL。
// 参数按"最多三个占位"给：api 层的函数签名从 (params) 到 (id, verdict, note) 都有，
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

describe('humanTaskApi / approvalApi：每个方法都打到本竖自己的端点', () => {
  it('待办 API 全部落在 /api/human-tasks 下', async () => {
    const urls = await collectUrls(await loadHumanTask())
    expect(urls.length).toBeGreaterThan(0)
    expect(urls.filter((u) => !u.startsWith(HUMAN_TASK_PREFIX))).toEqual([])
  })

  it('审批 API 全部落在 /api/approvals 下', async () => {
    const urls = await collectUrls(await loadApproval())
    expect(urls.length).toBeGreaterThan(0)
    expect(urls.filter((u) => !u.startsWith(APPROVAL_PREFIX))).toEqual([])
  })
})

describe('AC②：待办中心与坐席收件箱数据源隔离', () => {
  it('待办/审批 API 一次都不碰 /api/inbox', async () => {
    const urls = [
      ...(await collectUrls(await loadHumanTask())),
      ...(await collectUrls(await loadApproval()))
    ]
    expect(urls.filter((u) => u.startsWith(INBOX_PREFIX))).toEqual([])
  })

  it('反向：收件箱 API 也不碰待办与审批端点（隔离是双向的，不然只是换了个方向耦合）', async () => {
    const urls = await collectUrls(await loadInbox())
    expect(
      urls.filter((u) => u.startsWith(HUMAN_TASK_PREFIX) || u.startsWith(APPROVAL_PREFIX))
    ).toEqual([])
  })
})

describe('端点形状：与后端 controller 的契约逐条对齐', () => {
  it('列表把 kind/status 作为可重复参数原样透传，并带分页', async () => {
    const api = await loadHumanTask()
    clearMocks(mocks)
    await api.list({ kind: ['approval', 'collection_escalation'], page: 2, page_size: 20 })
    expect(mocks.http.get).toHaveBeenCalledTimes(1)
    const [url, params] = mocks.http.get.mock.calls[0]
    expect(url).toBe(HUMAN_TASK_PREFIX)
    expect(params.kind).toEqual(['approval', 'collection_escalation'])
    expect(params.page).toBe(2)
  })

  it('未读数走 /counts 且静默（轮询失败不该刷屏）', async () => {
    const api = await loadHumanTask()
    clearMocks(mocks)
    await api.counts()
    const [url, , config] = mocks.http.get.mock.calls[0]
    expect(url).toBe(`${HUMAN_TASK_PREFIX}/counts`)
    expect(config._silent).toBe(true)
  })

  it('assignee=me 由后端换成登录态身份，前端不拼自己的 id', async () => {
    const api = await loadHumanTask()
    clearMocks(mocks)
    await api.list({ assignee: 'me' })
    expect(mocks.http.get.mock.calls[0][1].assignee).toBe('me')
  })

  it('认领/释放/完成是无请求体的 POST，路径带待办 id', async () => {
    const api = await loadHumanTask()
    for (const action of ['claim', 'release', 'complete']) {
      clearMocks(mocks)
      await api[action]('ht_42')
      expect(mocks.http.post.mock.calls[0][0]).toBe(`${HUMAN_TASK_PREFIX}/ht_42/${action}`)
    }
  })

  it('撤销必须带理由（后端 Cancel 对空理由回 400）', async () => {
    const api = await loadHumanTask()
    clearMocks(mocks)
    await api.cancel('ht_42', '客户已自行解决')
    const [url, body] = mocks.http.post.mock.calls[0]
    expect(url).toBe(`${HUMAN_TASK_PREFIX}/ht_42/cancel`)
    expect(body).toEqual({ reason: '客户已自行解决' })
  })

  it('待办 id 进路径前必须编码（id 是 text 列，含 / 或 ? 会改写出别的路由）', async () => {
    const api = await loadHumanTask()
    clearMocks(mocks)
    await api.get('a/b?c')
    expect(mocks.http.get.mock.calls[0][0]).toBe(`${HUMAN_TASK_PREFIX}/a%2Fb%3Fc`)
  })

  it('裁决打到 /approvals/:id/decide，body 用 verdict/note', async () => {
    const api = await loadApproval()
    clearMocks(mocks)
    await api.decide('apr_9', 'rejected', '金额不对')
    const [url, body] = mocks.http.post.mock.calls[0]
    expect(url).toBe(`${APPROVAL_PREFIX}/apr_9/decide`)
    expect(body).toEqual({ verdict: 'rejected', note: '金额不对' })
  })

  it('裁决是静默调用：409"已被别人裁决"是常态，要由视图自己换文案', async () => {
    const api = await loadApproval()
    clearMocks(mocks)
    await api.decide('apr_9', 'approved', '')
    expect(mocks.http.post.mock.calls[0][2]).toMatchObject({ _silent: true })
  })

  it('详情读 /approvals/:id', async () => {
    const api = await loadApproval()
    clearMocks(mocks)
    await api.get('apr_9')
    expect(mocks.http.get.mock.calls[0][0]).toBe(`${APPROVAL_PREFIX}/apr_9`)
  })
})
