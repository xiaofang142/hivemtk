/**
 * 时长读数的单点判据：同一列 duration_ms 在会话头、执行历史、步表、命令流四处必须同量纲，
 * 且「真的 0 毫秒」「还没收口」「没采集到」三种事实不许共用一个读数。
 *
 * 立项依据（走查实测）：详情页执行历史写「9.6s」、监控页步表写「9573ms」，同一列两种单位，
 * 用户没法把两页对齐着看；而 `row.duration_ms ? ... : '—'` 这个形状把 0 毫秒与没采集到
 * 并成破折号，服务端刚补齐时长写入（收口那条 UPDATE 里算）之后，这个短路会把刚拿到的
 * 「0ms」这条真实读数又抹掉。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'

import { durationText, sessionDurationText } from '@/views/browserAutomation/durationText'

const readSrc = (p) => readFileSync(resolve(process.cwd(), 'src', p), 'utf8')

describe('durationText：三档量纲', () => {
  it('<1s 用毫秒，且 0 毫秒不等于「没数据」', () => {
    expect(durationText(0)).toBe('0ms')
    expect(durationText(46)).toBe('46ms')
    expect(durationText(999)).toBe('999ms')
  })

  it('1s..1min 用秒（一位小数，与后端整毫秒精度对齐）', () => {
    expect(durationText(1000)).toBe('1.0s')
    expect(durationText(9573)).toBe('9.6s')
    expect(durationText(10682)).toBe('10.7s')
  })

  it('≥1min 用「分+秒」，长执行不再是一串读不出位数的数', () => {
    expect(durationText(60000)).toBe('1分00秒')
    expect(durationText(302000)).toBe('5分02秒')
    expect(durationText(3600500)).toBe('60分01秒')
  })

  it('边界固定：59.999s 落在「秒」档、60s 起进「分」档；负数与脏值不许外溢', () => {
    // 59999 按一位小数进位显示成 60.0s——刻意接受：它确实是 59.999s，
    // 而把进位规则改到「分」档会让 59.95s 变 1分00秒，读数比现在更假。
    expect(durationText(59999)).toBe('60.0s')
    expect(durationText(-5)).toBe('0ms')
    expect(durationText(Number.NaN)).toBe('—')
    expect(durationText('abc')).toBe('—')
  })

  it('null/undefined/空串=没采集到，一律破折号', () => {
    expect(durationText(null)).toBe('—')
    expect(durationText(undefined)).toBe('—')
    expect(durationText('')).toBe('—')
  })
})

describe('sessionDurationText：未收口不显示成 0.0s', () => {
  it('在途两态（created/active）时长恒为 0，读数必须是「未收口」', () => {
    // 后端口径：duration_ms 只在终态那条 UPDATE 里写。给正在跑的会话显示 0.0s
    // 等于显示「瞬时完成」，而它还没结束。
    expect(sessionDurationText({ status: 'created', duration_ms: 0 })).toBe('未收口')
    expect(sessionDurationText({ status: 'active', duration_ms: 0 })).toBe('未收口')
  })

  it('终态三态按真实时长显示', () => {
    expect(sessionDurationText({ status: 'completed', duration_ms: 9573 })).toBe('9.6s')
    expect(sessionDurationText({ status: 'failed', duration_ms: 0 })).toBe('0ms')
    expect(sessionDurationText({ status: 'stopped', duration_ms: 302000 })).toBe('5分02秒')
  })

  it('会话对象缺失时不炸（读侧可能先于数据到达）', () => {
    expect(sessionDurationText(null)).toBe('—')
    expect(sessionDurationText(undefined)).toBe('—')
  })
})

describe('四处显示点必须共用同一份判据（静态锁）', () => {
  const monitor = readSrc('views/browserAutomation/Monitor.vue')
  const detail = readSrc('views/browserAutomation/Detail.vue')

  it('两页都从 ./durationText 取读数，不再各自换算', () => {
    expect(monitor).toContain("from './durationText'")
    expect(detail).toContain("from './durationText'")
  })

  it('旧的两种自换算写法已从模板里消失（残留其一就等于又分叉出一个单位）', () => {
    expect(monitor).not.toContain('${row.duration_ms}ms')
    expect(detail).not.toContain('row.duration_ms / 1000')
    expect(monitor).not.toContain('session.duration_ms / 1000')
  })

  it('会话级读数走 sessionDurationText，步/帧级走 durationText', () => {
    expect(monitor).toContain('sessionDurationText(session)')
    expect(detail).toContain('sessionDurationText(row)')
    expect(monitor.match(/durationText\(row\.duration_ms\)/g)).toHaveLength(2)
  })
})

// 渲染腿：静态锁只证明"源码里写了这个函数"，证明不了眼睛看到的字。
// 这里挂真相页组件、喂本腿实测过的三档读数，断言落到 DOM 上的字符串——
// 修的是显示，判据就得长在显示上。
const api = vi.hoisted(() => ({
  getBrowserSession: vi.fn(),
  getBrowserSessionSteps: vi.fn(),
  getBrowserSessionLogs: vi.fn(),
  getBrowserConfirmGate: vi.fn(),
  confirmBrowserSession: vi.fn(),
  stopBrowserSession: vi.fn(),
  exportBrowserSessionAudit: vi.fn(),
  interpretConfirmResult: vi.fn(),
  getBrowserTask: vi.fn(),
  listBrowserTaskSessions: vi.fn(),
  listBrowserCron: vi.fn(),
  createBrowserCron: vi.fn(),
  enableBrowserCron: vi.fn(),
  disableBrowserCron: vi.fn(),
  deleteBrowserCron: vi.fn(),
  publishBrowserTask: vi.fn(),
  runBrowserTask: vi.fn(),
}))
vi.mock('@/api/browserAutomation', () => api)
vi.mock('vue-router', () => ({
  useRoute: () => ({ params: { id: '542' } }),
  useRouter: () => ({ push: () => {} }),
}))

const Monitor = (await import('@/views/browserAutomation/Monitor.vue')).default
const Detail = (await import('@/views/browserAutomation/Detail.vue')).default

const sess = (over) => ({
  id: 605, status: 'completed', url: 'http://127.0.0.1/ui-duration-leg',
  total_steps: 3, success_steps: 3, failed_steps: 0, error_msg: '',
  created_at: '2026-09-28T10:00:00+08:00', started_at: '2026-09-28T10:00:00+08:00',
  confirm_pending: false, ...over,
})

// 按「这张表必须同时含这几列」选中表，再按列名取读数：
// 写死表序号或列号等于把判据绑在一次改版上——详情页除了执行历史还有一张步骤定义表，
// 拿 tables[0] 会读到步骤表（本轮实测就这么红的第一次），插一列也会静默读错列。
function durationColumn(root, requiredHeads, column = '耗时') {
  const tables = root.findAll('.el-table')
  const matched = []
  for (const t of tables) {
    const heads = t.findAll('.el-table__header-wrapper th .cell').map((c) => c.text().trim())
    if (requiredHeads.every((h) => heads.includes(h))) {
      matched.push({ t, heads })
    }
  }
  expect(matched.length, '按表头选表应当唯一命中，命中 ' + matched.length + ' 张（共 ' + tables.length + ' 张表）').toBe(1)
  const idx = matched[0].heads.indexOf(column)
  expect(idx, '表头里找不到「' + column + '」列：' + JSON.stringify(matched[0].heads)).toBeGreaterThan(-1)
  return matched[0].t.findAll('tbody tr').map((tr) => tr.findAll('td .cell')[idx].text().trim())
}

async function mountView(component, stubs) {
  const wrapper = mount(component, { global: { plugins: [ElementPlus], stubs: stubs || {} } })
  for (let i = 0; i < 4; i++) await flushPromises()
  return wrapper
}

describe('渲染腿：两页把同一列读成同一个量纲', () => {
  beforeEach(() => {
    for (const fn of Object.values(api)) fn.mockReset()
    api.getBrowserConfirmGate.mockResolvedValue({ pending: false })
    api.getBrowserTask.mockResolvedValue({ id: 542, name: '时长腿', status: 'done', task_type: 'one_shot', brain_goal: '' })
    api.listBrowserCron.mockResolvedValue([])
    api.listBrowserTaskSessions.mockResolvedValue({
      list: [
        sess({ id: 605, duration_ms: 46 }),
        sess({ id: 606, duration_ms: 9573 }),
        sess({ id: 607, duration_ms: 302000 }),
        sess({ id: 608, status: 'created', duration_ms: 0, completed_at: null }),
      ],
    })
  })
  afterEach(() => vi.restoreAllMocks())

  it('详情页执行历史：三档各归其位，在途那条不显示成 0.0s', async () => {
    const wrapper = await mountView(Detail, { HostInstallGuide: true })
    expect(durationColumn(wrapper, ['Session', '状态'])).toEqual(['46ms', '9.6s', '5分02秒', '未收口'])
    wrapper.unmount()
  })

  it('监控页会话头：终态按真实时长显示', async () => {
    api.getBrowserSession.mockResolvedValue(sess({ duration_ms: 9573 }))
    api.getBrowserSessionSteps.mockResolvedValue([])
    api.getBrowserSessionLogs.mockResolvedValue([])
    const wrapper = await mountView(Monitor)
    expect(wrapper.text()).toContain('9.6s')
    expect(wrapper.text()).not.toContain('9573ms')
    wrapper.unmount()
  })

  it('监控页会话头：在途会话显示「未收口」而不是「0.0s」', async () => {
    api.getBrowserSession.mockResolvedValue(sess({ status: 'active', duration_ms: 0 }))
    api.getBrowserSessionSteps.mockResolvedValue([{ id: 1, seq: 1, action: 'open_tab', status: 'success', duration_ms: 46 }])
    api.getBrowserSessionLogs.mockResolvedValue([])
    const wrapper = await mountView(Monitor)
    expect(wrapper.text()).toContain('未收口')
    expect(wrapper.text()).not.toContain('0.0s')
    wrapper.unmount()
  })

  it('监控页步表与命令流：与详情页同量纲，毫秒原值不外溢', async () => {
    api.getBrowserSession.mockResolvedValue(sess({ duration_ms: 302000 }))
    api.getBrowserSessionSteps.mockResolvedValue([
      { id: 1, seq: 1, action: 'open_tab', status: 'success', duration_ms: 46 },
      { id: 2, seq: 2, action: 'click', status: 'success', duration_ms: 999 },
      { id: 3, seq: 3, action: 'extract', status: 'success', duration_ms: 9573 },
      { id: 4, seq: 4, action: 'snapshot', status: 'success', duration_ms: 302000 },
    ])
    api.getBrowserSessionLogs.mockResolvedValue([
      { seq: 1, direction: 'command', action: 'open_tab', ok: null, duration_ms: 46 },
      { seq: 2, direction: 'event', action: 'extract', ok: true, duration_ms: 999 },
    ])
    const wrapper = await mountView(Monitor)
    // 999 这行刻意摆在毫秒/秒的分界下：档位阈值被人挪一格，DOM 上就得看得见（变异电池实测出来的缺口）
    expect(durationColumn(wrapper, ['目标', '值'])).toEqual(['46ms', '999ms', '9.6s', '5分02秒'])
    expect(durationColumn(wrapper, ['seq', '帧结论'])).toEqual(['46ms', '999ms'])
    expect(wrapper.text()).not.toContain('9573ms')
    wrapper.unmount()
  })
})
