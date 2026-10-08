import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'

// 消息中枢 Dashboard 的渲染腿：证明"页面上那几个数"真的来自 /api/message-hub/stats，
// 而不是模板里自己造的。
//
// 这一页以前同时犯了三类错，每一类都只在渲染这一层才看得见：
//   1) 请求打的是 window=1h，而 stats 端点只读 start_time/end_time ⇒ 文案说"最近 1 小时"、数是全量；
//   2) 图表数据来自 Math.random()、标题写着「实时吞吐（SSE）」，还连了一个服务端根本不存在的
//      /api/sse/message-hub（现测 404）⇒ 曲线每次刷新都在动，但动的都是噪声；
//   3) 死信列表拿 {list,total} 整个对象当 :data，列名写的是 channel/retryCount（后端给的是
//      platform/retries），批量重试按钮挂在 !stats.dlq 上（stats 里根本没有 dlq 这一格）
//      ⇒ 表格永远空、按钮永远点不动。
// 所以下面每一条都断言**渲染出来的字符串或 setOption 的实参**，不看源码里写了什么。

const http = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  delete: vi.fn()
}))

vi.mock('@/utils/request', () => ({ http: http }))

// echarts 在 jsdom 里 init 不出 canvas，这里换成一个只记实参的桩：
// 这张桩收到的 option 就是"用户在这块画布上看见的那些柱子"。
const chart = vi.hoisted(() => ({ setOption: vi.fn() }))
vi.mock('@/utils/echarts', () => ({
  safeInit: () => chart,
  safeDispose: vi.fn(),
  echarts: {}
}))

import Dashboard from '@/views/messageHub/Dashboard.vue'

const STATS = {
  total: 6,
  inbound: 4,
  outbound: 2,
  unread: 1,
  by_platform: { douyin: 4, wechat: 2 },
  by_direction: { inbound: 4, outbound: 2 },
  by_msg_type: { text: 5, image: 1 },
  recent_24h: 42
}

const DLQ = {
  list: [
    {
      id: 11,
      platform: 'douyin',
      msg_id: 'm_11',
      direction: 'outbound',
      content: '在的呢',
      error: '投递失败',
      retries: 3,
      failedAt: '2026-09-29 10:00:00'
    },
    {
      id: 12,
      platform: 'telegram',
      msg_id: 'm_12',
      direction: 'inbound',
      content: 'hello',
      error: '投递失败',
      retries: 1,
      failedAt: '2026-09-29 10:05:00'
    }
  ],
  total: 7
}

const getStatsParams = () => {
  const call = http.get.mock.calls.find((c) => c[0] === '/api/message-hub/stats')
  expect(call, '必须有一次 /api/message-hub/stats 请求').toBeTruthy()
  return call[1]
}

const findButton = (text) =>
  Array.from(document.querySelectorAll('button')).find((b) => b.textContent.trim() === text)

async function mountPage() {
  const wrapper = mount(Dashboard, { attachTo: document.body, global: { plugins: [ElementPlus] } })
  await flushPromises()
  return wrapper
}

beforeEach(() => {
  vi.clearAllMocks()
  http.get.mockImplementation((url) => {
    if (url === '/api/message-hub/stats') return Promise.resolve(STATS)
    if (url === '/api/message-hub/dlq') return Promise.resolve(DLQ)
    return Promise.reject(new Error(`不该再请求 ${url}`))
  })
  // 服务端没有任何 /api/sse/* 前缀（现测 404），所以这一页一旦再新建 EventSource，
  // 这里就会留下痕迹；断言打在这个桩上，而不是打在"代码里没写"这种读源码的结论上。
  global.EventSource = vi.fn(function EventSource() {
    this.close = vi.fn()
  })
})

afterEach(() => {
  vi.useRealTimers()
  document.body.innerHTML = ''
})

describe('读数口径：文案里的窗口必须就是请求里的窗口', () => {
  it('带 start_time/end_time 且正好差一个窗长，不再发那个会被端点静默忽略的 window=', async () => {
    await mountPage()
    const params = getStatsParams()
    expect(params).not.toHaveProperty('window')
    expect(params.start_time).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/)
    expect(params.end_time).toMatch(/^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}/)
    const delta = new Date(params.end_time).getTime() - new Date(params.start_time).getTime()
    expect(delta).toBe(60 * 60 * 1000)
    expect(document.body.textContent).toContain('最近 1 小时')
  })

  it('四张卡的数字就是 stats 给的那四个，且没有一处再请求不存在的 channel-health', async () => {
    const wrapper = await mountPage()
    const values = Array.from(wrapper.findAll('.stat-value')).map((n) => n.text())
    expect(values).toEqual(['6', '2', '4', '1'])
    const urls = http.get.mock.calls.map((c) => c[0])
    expect(urls).toEqual(['/api/message-hub/stats', '/api/message-hub/dlq'])
    expect(urls.some((u) => u.includes('channel-health'))).toBe(false)
    wrapper.unmount()
  })

  it('近 24 小时累计这一格读的是 recent_24h，不是窗口总数', async () => {
    const wrapper = await mountPage()
    expect(wrapper.text()).toContain('近 24 小时累计 42 条')
    wrapper.unmount()
  })
})

describe('图表：柱子高度是 stats 里的计数，不是随机数', () => {
  it('x 轴用渠道名、series 用同窗口的条数，并按数量降序', async () => {
    const wrapper = await mountPage()
    expect(chart.setOption).toHaveBeenCalledTimes(1)
    const option = chart.setOption.mock.calls[0][0]
    // douyin 有中文词表、wechat 没有（hub 写 wechat，前端词表是 weixin）：
    // 认不出的一律原样露出，不许替它编一个相近的渠道名。
    expect(option.xAxis.data).toEqual(['抖音', 'wechat'])
    expect(option.series[0].data).toEqual([4, 2])
    // notMerge=true：渠道少一档时上一轮的柱子必须跟着消失
    expect(chart.setOption.mock.calls[0][1]).toBe(true)
    wrapper.unmount()
  })

  it('标题里不再有 SSE / 实时吞吐那种它没做到的承诺', async () => {
    const wrapper = await mountPage()
    expect(wrapper.text()).toContain('渠道吞吐分布')
    expect(wrapper.text()).not.toContain('SSE')
    expect(global.EventSource).not.toHaveBeenCalled()
    wrapper.unmount()
  })

  it('窗口内一条都没有时说的是"画不出"，不是留一张空白画布', async () => {
    http.get.mockImplementation((url) => {
      if (url === '/api/message-hub/stats') {
        return Promise.resolve({ ...STATS, total: 0, inbound: 0, outbound: 0, by_platform: {}, by_direction: {}, by_msg_type: {} })
      }
      return Promise.resolve(DLQ)
    })
    const wrapper = await mountPage()
    expect(chart.setOption).not.toHaveBeenCalled()
    expect(wrapper.text()).toContain('该窗口内没有消息')
    wrapper.unmount()
  })
})

describe('方向与类型分布：后端给 map，页面按降序把它译成人话', () => {
  const distRows = () => Array.from(document.querySelectorAll('.dist-table .el-table__row'))
    .map((r) => r.textContent)

  it('方向两格、类型两格都成行，各自按数量降序', async () => {
    await mountPage()
    expect(distRows()).toEqual(['方向接收4', '方向发送2', '消息类型文本5', '消息类型图片1'])
  })

  it('值域外的消息类型不许被译成任何一个已知类型', async () => {
    http.get.mockImplementation((url) => {
      if (url === '/api/message-hub/stats') {
        return Promise.resolve({ ...STATS, by_msg_type: { text: 5, sticker: 2 } })
      }
      return Promise.resolve(DLQ)
    })
    await mountPage()
    expect(distRows()).toContain('消息类型sticker2')
    expect(distRows().some((t) => t.includes('表情'))).toBe(false)
  })
})

describe('死信队列：表格吃的是 list，按钮吃的是 total', () => {
  it('两行都在，渠道译成中文、重试次数读得到', async () => {
    const wrapper = await mountPage()
    expect(wrapper.text()).toContain('待重试 7 条')
    const dlqRows = Array.from(document.querySelectorAll('.dlq-card .el-table__row'))
    expect(dlqRows.length).toBe(2)
    expect(dlqRows[0].textContent).toContain('抖音')
    expect(dlqRows[0].textContent).toContain('发送')
    expect(dlqRows[0].textContent).toContain('3')
    expect(dlqRows[1].textContent).toContain('Telegram')
    expect(dlqRows[1].textContent).toContain('接收')
  })

  it('待重试 0 条时按钮禁着；有 7 条时必须点得动（以前挂在 stats.dlq 上，恒禁）', async () => {
    const filled = await mountPage()
    expect(findButton('批量重试').disabled).toBe(false)
    filled.unmount()

    document.body.innerHTML = ''
    http.get.mockImplementation((url) => {
      if (url === '/api/message-hub/stats') return Promise.resolve(STATS)
      return Promise.resolve({ list: [], total: 0 })
    })
    const empty = await mountPage()
    expect(findButton('批量重试').disabled).toBe(true)
    empty.unmount()
  })

  it('批量重试成功后按返回的 requeued 出声，并重新拉一次读数', async () => {
    const wrapper = await mountPage()
    http.post.mockResolvedValue({ requeued: 7 })
    findButton('批量重试').click()
    await flushPromises()
    expect(http.post).toHaveBeenCalledWith('/api/message-hub/dlq/batch-retry', {})
    expect(document.body.textContent).toContain('已重新入队 7 条')
    expect(http.get.mock.calls.filter((c) => c[0] === '/api/message-hub/stats').length).toBe(2)
    wrapper.unmount()
  })
})

describe('读不到的那一轮：旧数还在，但必须说它是旧数', () => {
  it('stats 挂了时页面挂着"上一次成功读到的"，不静默展示陈旧数', async () => {
    http.get.mockRejectedValueOnce(Object.assign(new Error('网关超时'), { status: 504 }))
    http.get.mockResolvedValue(DLQ)
    const wrapper = await mountPage()
    expect(wrapper.text()).toContain('统计本次没读到')
    expect(wrapper.text()).toContain('上一次成功读到的')
    wrapper.unmount()
  })

  it('30 秒后再拉一次：窗口是滚动的，不是一次性读数', async () => {
    const statsStarts = () =>
      http.get.mock.calls.filter((c) => c[0] === '/api/message-hub/stats').map((c) => c[1].start_time)
    vi.useFakeTimers()
    vi.setSystemTime(new Date('2026-09-29T10:00:00Z'))
    const wrapper = await mountPage()
    expect(statsStarts()).toHaveLength(1)
    const first = statsStarts()[0]
    expect(first).toBe('2026-09-29T09:00:00.000Z')
    vi.advanceTimersByTime(30000)
    await flushPromises()
    expect(statsStarts()).toHaveLength(2)
    expect(new Date(statsStarts()[1]).getTime() - new Date(first).getTime()).toBe(30000)
    vi.useRealTimers()
    wrapper.unmount()
  })
})
