import { describe, it, expect } from 'vitest'
import { badCaseApi } from '@/api/badCase.js'

// 这个文件不 mock http：它要看的正是 **axios 拼出来、即将发出去的那条 URL**。
//
// 为什么必须跑到这一层：后端读重复参数用的是 gin 的 QueryArray("label")，只认
// ?label=a&label=b；而 axios 对数组的默认写法是 ?label[]=a（转义后 %5B%5D）。
// 两者错开时的表现**不是报错，是筛选项被整个忽略** —— 勾了"只看知识库缺词"，
// 拿回来的是全队列，页面上看不出任何异常；把 http 换成 mock 的用例看不见这一层。
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
  it('多值 source/status/label 序列化成重复键，不带 []', async () => {
    const url = await urlOf(() =>
      badCaseApi.list({
        source: ['low_confidence', 'zero_hit'],
        status: ['pending', 'labeled'],
        label: ['kb_missing', 'intent_misjudge'],
        fix_layer: 'knowledge',
        page: 3,
        page_size: 50
      })
    )
    expect(url).toContain('/api/bad-cases?')
    const q = queryOf(url)
    // 判"键名里没有方括号"而不是判"URL 里没有 ["：axios 会把方括号转义成 %5B%5D，
    // 所以字面量判据在错误形状下照样通过。
    for (const key of ['source', 'status', 'label']) {
      expect([...q.keys()]).not.toContain(`${key}[]`)
    }
    expect(q.getAll('source')).toEqual(['low_confidence', 'zero_hit'])
    expect(q.getAll('status')).toEqual(['pending', 'labeled'])
    expect(q.getAll('label')).toEqual(['kb_missing', 'intent_misjudge'])
    expect(q.get('fix_layer')).toBe('knowledge')
    expect(q.get('page')).toBe('3')
  })

  it('空数组/空串不进查询串（?label= 会被读成"传了个空类目"）', async () => {
    const url = await urlOf(() =>
      badCaseApi.list({ source: [], status: '', label: [''], fix_layer: '', page: 1 })
    )
    const q = queryOf(url)
    for (const key of ['source', 'status', 'label', 'fix_layer']) {
      expect(q.has(key)).toBe(false)
    }
    expect(q.get('page')).toBe('1')
  })

  it('数组里的空串被丢掉，非空的留下（?label=&label=kb_missing 是半条筛选器）', async () => {
    const url = await urlOf(() => badCaseApi.list({ label: ['', 'kb_missing', '  '] }))
    expect(queryOf(url).getAll('label')).toEqual(['kb_missing', '  '])
  })

  it('不带任何筛选时 URL 干净（没有尾随的 ?）', async () => {
    expect(await urlOf(() => badCaseApi.list())).toBe('/api/bad-cases')
  })

  it('stats 与 taxonomy 打的是具体路径，不是把名字当成 id', async () => {
    expect(await urlOf(() => badCaseApi.stats())).toBe('/api/bad-cases/stats')
    expect(await urlOf(() => badCaseApi.taxonomy())).toBe('/api/bad-cases/taxonomy')
  })
})
