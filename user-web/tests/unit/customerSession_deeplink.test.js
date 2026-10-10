import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import ElementPlus, { ElMessage } from 'element-plus'

// 客户会话页的深链入口腿：CSAT 看板「查看会话」以 /#/customerSession/list?session_id=… 打开本页，
// 本页必须在列表就位后把那条会话选出来。
//
// 这一条为什么值得单独测：实测本实例 GET /api/customer-sessions 默认只回一页 20 条（total=227），
// 排序是 priority DESC / last_message_at DESC，而问卷回收发生在会话结束**之后**——
// 差评对应的老会话正好排在后面。原实现只在首页那 20 条里找，找不到只打一行 console.warn，
// 于是从看板点「查看会话」会落到一个"什么都没选中、也不说为什么"的空页面（用户视角＝按钮坏了）。
// 所以这里的断言全部落在渲染结果上：哪一项带 .active、聊天栏标题是谁、有没有那条看得见的提示。
//
// 深链的口径对账（不是随手写的假数）：csat_surveys.session_id 联的是
// customer_sessions.session_id（见 user-server/internal/repository/csat.go 的 ListNegative），
// 而前端 loadSessions 把 session_id 映射成 sessionId、id 映射成 id ⇒ 两种写法都要能命中。

const http = vi.hoisted(() => ({
  get: vi.fn(),
  post: vi.fn(),
  put: vi.fn(),
  delete: vi.fn()
}))

vi.mock('@/utils/request', () => ({ http: http, getRequestInstance: () => http, default: http }))

// 坐席 socket 会真连 WebSocket，jsdom 里没有；换成只记实例的桩，
// 断言"深链选中"这件事与 socket 装配互不影响。
const sockets = vi.hoisted(() => [])
vi.mock('@/utils/agentSocket', () => ({
  default: class AgentSocketStub {
    constructor(agentId, agentName, options) {
      this.agentId = agentId
      this.options = options
      sockets.push(this)
    }
    connect() { this.connected = true }
    close() { this.closed = true }
  }
}))

// 深链参数是用例的参数：每条腿在挂载前改 ctx.query。
const ctx = vi.hoisted(() => ({ query: {} }))
vi.mock('vue-router', () => ({
  useRoute: () => ({ query: ctx.query, path: '/customerSession/list' }),
  useRouter: () => ({ push: vi.fn(), replace: vi.fn() })
}))

import i18n from '@/i18n'
import List from '@/views/customerSession/List.vue'

const NOW = new Date('2026-10-08T09:00:00+08:00').toISOString()

// 后端一行的形状（snake_case，取自 model.CustomerSession 的 json tag）
const row = (id, sessionId, userName, extra = {}) => ({
  id,
  session_id: sessionId,
  platform: 'web',
  user_id: `u_${id}`,
  user_name: userName,
  status: 'human_handling',
  handler_type: 'human',
  last_message: '已经处理好了',
  last_message_at: NOW,
  created_at: NOW,
  message_count: 4,
  unread_count: 0,
  tags: [],
  ...extra
})

// 日常那一页：20 条，全是近期会话。总数 227 是现测值，不是编的。
const DAILY_TOTAL = 227
const DAILY_PAGE = Array.from({ length: 20 }, (_, i) => row(40 + i, `sess_recent_${i}`, `近期客户${i}`))
// 深链指定的老会话只出现在宽列表里（对应"回收过问卷、已排到首页之后"的真实形态）
const OLD_HIT = row(310, 'sess_old_9', '差评客户癸')
const WIDE_PAGE = [...DAILY_PAGE, row(300, 'sess_recent_x', '凑数客户'), OLD_HIT]

const MESSAGES = [
  { id: 1, sender_type: 'user', sender_name: '差评客户癸', content: '响应太慢', created_at: NOW },
  { id: 2, sender_type: 'agent', sender_name: '坐席乙', content: '已加急处理', created_at: NOW }
]

function stubEndpoints(wideRows) {
  http.get.mockImplementation((url, params) => {
    if (url === '/api/customer-sessions') {
      const list = params && params.page_size ? wideRows : DAILY_PAGE
      return Promise.resolve({ list, total: DAILY_TOTAL, page: 1, page_size: list.length })
    }
    if (url.startsWith('/api/customer-sessions/') && url.endsWith('/messages')) {
      return Promise.resolve({ list: MESSAGES, total: MESSAGES.length })
    }
    if (url === '/api/session-tags') return Promise.resolve([])
    if (url === '/api/quick-replies' || url === '/api/quick-replies/categories') return Promise.resolve([])
    if (url.startsWith('/api/ai-suggestions/')) return Promise.resolve([])
    if (url.startsWith('/api/customer-360/stats')) return Promise.resolve({ messageCount: 4, sessionCount: 2, aiReplyCount: 1 })
    if (url.startsWith('/api/customer-360/tags')) return Promise.resolve([])
    if (url === '/api/agents/me') return Promise.resolve({ agent_id: 'ag-1', status: 'online' })
    if (url === '/api/agents/online') return Promise.resolve([])
    if (url === '/api/customer-sessions/blacklist') return Promise.resolve([])
    return Promise.reject(new Error(`不该再请求 ${url}`))
  })
}

async function mountPage() {
  const wrapper = mount(List, {
    attachTo: document.body,
    global: {
      plugins: [ElementPlus, i18n],
      // 右栏客户 360° 与黑名单弹窗有自己的数据流与端点，这里只测左栏选中这件事。
      stubs: { CustomerProfilePanel: true, BlacklistDialog: true }
    }
  })
  // onMounted 里是 loadSessions → 深链再取宽列表 → selectSession → 标签/AI 建议/画像，
  // 一串真 promise，多排几轮才落到位。
  for (let i = 0; i < 8; i++) {
    await flushPromises()
    await new Promise((r) => setTimeout(r, 5))
  }
  return wrapper
}

const activeItems = (wrapper) => wrapper.findAll('.session-item.active')
const sessionRows = (wrapper) => wrapper.findAll('.session-item')
const warningToasts = () =>
  Array.from(document.querySelectorAll('.el-message--warning')).map((n) => n.textContent.trim())
const sessionRequests = () => http.get.mock.calls.filter(([url]) => url === '/api/customer-sessions')

beforeEach(() => {
  vi.clearAllMocks()
  ctx.query = {}
  sockets.length = 0
})

afterEach(() => {
  ElMessage.closeAll()
  // 消息条在 body 上而不是 wrapper 上：不清的话下一条腿的"有没有提示"会读到上一条腿的。
  document.body.replaceChildren()
})

describe('深链命中日常那一页：直接选中，不额外捞宽列表', () => {
  it('选中的是那一条会话，且聊天栏打开的也是它', async () => {
    ctx.query = { session_id: 'sess_recent_3' }
    stubEndpoints(WIDE_PAGE)
    const wrapper = await mountPage()

    const active = activeItems(wrapper)
    expect(active.length).toBe(1)
    // "近期客户3" 是 id=43 / session_id=sess_recent_3 那一行的 user_name
    expect(active[0].find('.name').text()).toBe('近期客户3')
    expect(wrapper.find('.chat-header h3').text()).toBe('近期客户3')
    // 选中即按那条会话的数字主键拉消息（证明走的是 selectSession 而不是只高亮）
    expect(http.get).toHaveBeenCalledWith('/api/customer-sessions/43/messages', undefined)
    wrapper.unmount()
  })

  it('命中首页时不该多发一次宽列表请求（日常路径不许被这个入口带跑）', async () => {
    ctx.query = { session_id: 'sess_recent_3' }
    stubEndpoints(WIDE_PAGE)
    const wrapper = await mountPage()
    expect(sessionRequests().length).toBe(1)
    expect(sessionRequests()[0][1]).toBeUndefined()
    expect(warningToasts()).toEqual([])
    wrapper.unmount()
  })

  it('深链写成数字主键也要命中（两种口径同源）', async () => {
    ctx.query = { session_id: '42' }
    stubEndpoints(WIDE_PAGE)
    const wrapper = await mountPage()
    const active = activeItems(wrapper)
    expect(active.length).toBe(1)
    expect(active[0].find('.name').text()).toBe('近期客户2')
    wrapper.unmount()
  })
})

describe('深链不在首页：按深链专用的一页宽列表再找一次', () => {
  it('宽列表命中后该项被并进左栏并选中', async () => {
    ctx.query = { session_id: 'sess_old_9' }
    stubEndpoints(WIDE_PAGE)
    const wrapper = await mountPage()

    const wide = sessionRequests()[1]
    expect(sessionRequests().length).toBe(2)
    expect(wide[1]).toEqual({ page: 1, page_size: 500 })

    const active = activeItems(wrapper)
    expect(active.length).toBe(1)
    expect(active[0].find('.name').text()).toBe('差评客户癸')
    expect(wrapper.find('.chat-header h3').text()).toBe('差评客户癸')
    expect(http.get).toHaveBeenCalledWith('/api/customer-sessions/310/messages', undefined)
    // 选中的那条必须看得见：它进了左栏列表，而不是只活在 currentSession 里
    expect(sessionRows(wrapper).map((n) => n.find('.name').text())).toContain('差评客户癸')
    expect(warningToasts()).toEqual([])
    wrapper.unmount()
  })

  it('宽列表里也没有时：不许假装选中，要给一条看得见的提示', async () => {
    ctx.query = { session_id: 'sess_nowhere' }
    stubEndpoints(WIDE_PAGE)
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => {})
    const wrapper = await mountPage()

    expect(sessionRequests().length).toBe(2)
    expect(activeItems(wrapper).length).toBe(0)
    // 没有 currentSession ⇒ 聊天栏是空的占位，而不是某条会话的壳子
    expect(wrapper.find('.chat-header').exists()).toBe(false)
    const toasts = warningToasts()
    expect(toasts.length).toBe(1)
    expect(toasts[0]).toContain('sess_nowhere')
    // 开发态那行 warn 也要在（两个口径都得留：用户看提示，排查看控制台）。
    // 按参数逐个匹配，不做 String(args)——数组转串会在参数间插逗号，'…: sess_x' 这种整句永远对不上。
    const warnCalls = warn.mock.calls.filter((c) => String(c[0]).includes('深链指定的会话不在当前列表内'))
    expect(warnCalls.length).toBe(1)
    expect(warnCalls[0][1]).toBe('sess_nowhere')
    warn.mockRestore()
    wrapper.unmount()
  })
})

describe('没有深链参数时保持原样', () => {
  it('只取日常一页、无选中项、无提示', async () => {
    stubEndpoints(WIDE_PAGE)
    const wrapper = await mountPage()
    expect(sessionRequests().length).toBe(1)
    expect(sessionRequests()[0][1]).toBeUndefined()
    expect(activeItems(wrapper).length).toBe(0)
    expect(warningToasts()).toEqual([])
    expect(wrapper.text()).toContain('请从左侧选择会话')
    wrapper.unmount()
  })
})

describe('夹具自证', () => {
  it('左栏真的渲染了 20 条日常会话（桩接上了才有"不在首页"这一档）', async () => {
    stubEndpoints(WIDE_PAGE)
    const wrapper = await mountPage()
    expect(sessionRows(wrapper).length).toBe(20)
    expect(DAILY_TOTAL).toBeGreaterThan(DAILY_PAGE.length)
    // 深链目标确实不在首页里，只在宽列表里
    expect(DAILY_PAGE.map((r) => r.session_id)).not.toContain('sess_old_9')
    expect(WIDE_PAGE.map((r) => r.session_id)).toContain('sess_old_9')
    wrapper.unmount()
  })

  it('坐席身份到位后 socket 真的装上（深链那条腿不能顺手把 socket 腿证伪）', async () => {
    stubEndpoints(WIDE_PAGE)
    const wrapper = await mountPage()
    expect(sockets.length).toBe(1)
    expect(sockets[0].agentId).toBe('ag-1')
    expect(sockets[0].connected).toBe(true)
    wrapper.unmount()
  })
})
