import { describe, it, expect } from 'vitest'
import { humanTaskApi } from '@/api/humanTask.js'

// 这个文件不 mock http：它要看的正是 **axios 拼出来、即将发出去的那条 URL**。
//
// 为什么必须跑到这一层：后端读重复参数用的是 gin 的 QueryArray("kind")，只认
// ?kind=a&kind=b；而 axios 对数组的默认写法是 ?kind[]=a&kind[]=b（PHP 那一路的约定）。
// 两者错开时的表现是"勾了两个类型，列表回 400 或空"，且只在真发请求时暴露 ——
// 把 http 换成 mock 的用例看不见这一层（同目录那个 mock 版用例就是这么绿的）。
//
// 探针放在 XMLHttpRequest.open：axios 的 xhr 适配器把 buildURL（含 paramsSerializer）
// 的结果直接喂给 open，所以那是"序列化已完成、尚未上网"的唯一位置。假 XHR 在 open
// 之后就会被适配器判死（它没有 addEventListener），promise 以 error 收尾 ——
// 本用例不关心响应，只关心那句 URL，所以把它吃掉。
async function urlOf(call) {
  const rec = {}
  const Orig = globalThis.XMLHttpRequest
  globalThis.XMLHttpRequest = function FakeXHR() {
    this.open = (_method, url) => {
      rec.url = url
    }
  }
  try {
    await call()
  } catch {
    /* 有意不处理：见上 */
  } finally {
    globalThis.XMLHttpRequest = Orig
  }
  expect(rec.url, 'axios 没有走到 open：请求没被发出去，用例本身失效').toBeTruthy()
  return rec.url
}

const queryOf = (url) => new URL(url, 'http://under-test').searchParams

describe('列表查询串的真实形状（gin QueryArray 读得到的那种）', () => {
  it('多值 kind 序列化成重复键，不带 []', async () => {
    const url = await urlOf(() =>
      humanTaskApi.list({
        kind: ['approval', 'collection_escalation'],
        status: ['pending'],
        page: 2,
        page_size: 20
      })
    )
    expect(url).toContain('/api/human-tasks?')
    const q = queryOf(url)
    // 判"键名里没有方括号"而不是判"URL 里没有 ["：axios 会把方括号转义成 %5B%5D，
    // 所以字面量判据在错误形状下照样通过（本文件写第一版时就这么绿过一次）。
    expect([...q.keys()]).not.toContain('kind[]')
    expect(q.getAll('kind')).toEqual(['approval', 'collection_escalation'])
    expect(q.getAll('status')).toEqual(['pending'])
    expect(q.get('page')).toBe('2')
    expect(q.get('page_size')).toBe('20')
  })

  it('空筛选/空串参数不进查询串（?kind= 在网关日志里读起来像"传了个空类型"）', async () => {
    const url = await urlOf(() => humanTaskApi.list({ kind: [], status: '', assignee: 'me' }))
    const q = queryOf(url)
    expect(q.has('kind')).toBe(false)
    expect(q.has('status')).toBe(false)
    expect(q.get('assignee')).toBe('me')
  })

  it('不带任何筛选时 URL 干净（没有尾随的 ?）', async () => {
    expect(await urlOf(() => humanTaskApi.list())).toBe('/api/human-tasks')
  })

  it('counts 打的是 /counts 这个具体路径，不是把 "counts" 当成 id', async () => {
    // 两件事都能从这一句读出来：路径没写错，以及 /:id 那条路由不会被 /counts 抢走。
    expect(await urlOf(() => humanTaskApi.counts())).toBe('/api/human-tasks/counts')
  })
})
