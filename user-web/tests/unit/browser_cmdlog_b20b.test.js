/**
 * 契约锁（审计流三态的前端侧）：三态必须原样落到眼睛看到的符号上。
 *
 * 为什么这一半不是"顺手改个显示"：服务端把 command 帧的 ok 从常量 true 改成 null 之后，
 * 旧模板 `row.ok ? '✓' : '✗'` 会立刻把**每一条下发**都画成 ✗ 失败。
 * 也就是说这一列的红绿是前后端同一份契约的两端——只改一边，监控页就从"虚绿"翻成"实红"，
 * 比原来更误导。所以本批的 Go 侧与这里必须同批落地、同批锁住。
 */
import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const api = vi.hoisted(() => ({
  getBrowserSession: vi.fn(),
  getBrowserSessionSteps: vi.fn(),
  getBrowserSessionLogs: vi.fn(),
  getBrowserConfirmGate: vi.fn(),
  confirmBrowserSession: vi.fn(),
  stopBrowserSession: vi.fn(),
  exportBrowserSessionAudit: vi.fn(),
  interpretConfirmResult: vi.fn()
}))
vi.mock('@/api/browserAutomation', () => api)
vi.mock('vue-router', () => ({ useRoute: () => ({ params: { id: '88' } }) }))

import Monitor from '@/views/browserAutomation/Monitor.vue'

const SESSION = { id: '88', status: 'completed', confirm_pending: false, duration_ms: 900, total_steps: 1, success_steps: 1 }

// 三帧一台戏：下发帧无结论（ok=null）、回包帧结论成立（true）、回包帧结论不成立（false）
const LOGS = [
  { seq: 1, direction: 'command', action: 'comment_send', ok: null, payload: { action: 'comment_send' } },
  { seq: 2, direction: 'event', action: 'comment_send', ok: true, payload: { result: { sent: true } } },
  { seq: 3, direction: 'event', action: 'comment_verify', ok: false, payload: { error: '未命中评论容器' } },
  { seq: 4, direction: 'judge', action: 'write_confirm', ok: false, payload: { payload_hash: 'ab12cd34', state: 'unattributed' } }
]

async function mountMonitor() {
  const wrapper = mount(Monitor, { global: { plugins: [ElementPlus] } })
  await flushPromises()
  await flushPromises()
  return wrapper
}

describe('审计流一列三态', () => {
  beforeEach(() => {
    for (const fn of Object.values(api)) fn.mockReset()
    api.getBrowserSession.mockResolvedValue(SESSION)
    api.getBrowserSessionSteps.mockResolvedValue([])
    api.getBrowserSessionLogs.mockResolvedValue(LOGS)
  })
  afterEach(() => vi.restoreAllMocks())

  it('null 画「—」而不是「✗」：下发帧不是失败帧', async () => {
    const wrapper = await mountMonitor()
    const cells = wrapper.findAll('td .cell').map((c) => c.text())
    expect(cells.filter((t) => t === '—').length).toBeGreaterThanOrEqual(1)
    expect(cells.filter((t) => t === '✓').length).toBe(1)
    expect(cells.filter((t) => t === '✗').length).toBe(2)
    wrapper.unmount()
  })

  it('表头讲清 ✓/✗/— 各自的意思（读包的人不该靠猜）', async () => {
    const wrapper = await mountMonitor()
    expect(wrapper.text()).toContain('不携带结论')
    wrapper.unmount()
  })

  it('写操作的确认结论按 judge 帧原样进表，不在前端二次加工', async () => {
    const wrapper = await mountMonitor()
    expect(wrapper.text()).toContain('write_confirm')
    expect(wrapper.text()).toContain('unattributed')
    wrapper.unmount()
  })
})

describe('静态锁：Monitor 模板的结果列必须走三态函数', () => {
  const monitor = readFileSync(resolve(process.cwd(), 'src/views/browserAutomation/Monitor.vue'), 'utf8')
  // 锁只能锁模板：注释里那句"旧的二态写法"是解释为什么改的，拿整份文件做 not.toMatch
  // 会锁成一桩自我审查——注释里不许出现这个词，那不是判据是忌讳。
  // 边界取 <script 之前而不是第一个 </template>：模板里有好几个 #default 插槽，
  // 第一个闭合标签在结果列之前，按它截会让锁悄悄只看半张表。
  const scriptStart = monitor.indexOf('<script')
  const tpl = monitor.slice(0, scriptStart)
  // 先证明截取本身成立（模板结构一动就静默变成"整份文件"或"半张表"，锁随之失焦）
  expect(scriptStart).toBeGreaterThan(0)
  expect(tpl).toContain('<el-table')

  it('模板里的结果列必须走三态函数，不得留二态三元表达式', () => {
    expect(tpl).not.toMatch(/row\.ok \?/)
    expect(tpl).toMatch(/outcomeMark\(row\.ok\)/)
  })

  it('判定函数得对「无结论」与「结论为否」分开（把 null 折进 falsy 分支就是本批要修的 bug 的镜像）', () => {
    const script = monitor.slice(scriptStart)
    expect(script).toMatch(/ok\s*===\s*true/)
    expect(script).toMatch(/ok\s*===\s*false/)
    // 第三态必须是兜底分支而不是又一次相等判断：字段缺失/字符串这类未知形状也要落成 '—'，
    // 宁可说"这帧没结论"，也不替读包的人编一个红或绿
    expect(script).toMatch(/:\s*'—'/)
  })
})
