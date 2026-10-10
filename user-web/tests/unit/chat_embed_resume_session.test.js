import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { mount, flushPromises } from '@vue/test-utils'
import { nextTick } from 'vue'

// 访客侧"历史会话"恢复的凭证腿。
//
// 为什么单独立一份：visitor_token 是签在 (channel, visitor, session) 三元组上的无状态 HMAC，
// 而 openSession 只给"最近活跃的那一条"签发。所以从 recent-closed 列表点开更早的会话时，
// 手上带的仍是别的会话的证 —— 那条会话上的 messages / offline-messages / close / rate 会全部 403。
// 真机走查实测到的现象：点列表项 → 窗口是空的；点右上角 × → /close 回 403（而且 close 是静默 catch，
// 访客只会以为界面坏了）。
//
// 断言全部落在"访客能看见/后端能收到"的两端：
//   1) 换证必须先于拉历史（顺序用 seq 标签对账，不是只看各自被调过一次）；
//   2) 拉历史时带出去的那串证必须是这条会话自己的（不是 openSession 那串）；
//   3) 换证失败/换到空证时**不切会话** —— 用"点 × 时 close 打在哪条会话上"来证 sessionId 没动，
//      并要窗口里出现那句看得见的提示；
//   4) 列表项是 role=button + tabindex=0，键盘 Enter 也得走同一条腿。

const VISITOR_ID = 'v_test_r67'
const TOKEN_NEW = 'tk_for_newest_session'
const TOKEN_OLD = 'tk_for_older_session'
const NOW = new Date('2026-10-08T09:00:00+08:00').toISOString()

// 调用序列：每个 mock 顺手记一枚标签，用于断言"先换证、再拉历史"
const seq = vi.hoisted(() => [])

const api = vi.hoisted(() => ({
  getAvailableAgents: vi.fn(),
  openSession: vi.fn(),
  getRecentClosedSessions: vi.fn(),
  getMessages: vi.fn(),
  getOfflineMessages: vi.fn(),
  exchangeSessionToken: vi.fn(),
  sendMessage: vi.fn(),
  requestHumanTransfer: vi.fn(),
  closeSession: vi.fn(),
  rateSession: vi.fn()
}))

vi.mock('@/api/chatPublic', () => ({ default: api }))

// ChatSocket 真连 WebSocket，jsdom 里没有；换成只记实例与构造参数的桩。
// 桩保留 options，用例靠它手动推 onOfflineMessages —— 那条回调是把"历史会话"入口横幅点亮的那一步。
// 桩类必须生在 vi.hoisted 里：vi.mock 的调用会被提到文件最前，
// 放在下方 import 之后声明会撞 "Cannot access 'ChatSocketStub' before initialization"。
const sockets = vi.hoisted(() => [])
const ChatSocketStub = vi.hoisted(() => {
  class Stub {
    constructor(options) {
      this.options = options
      sockets.push(this)
    }

    connect() { this.connected = true }

    close() { this.closed = true }

    ackDelivered() {}

    send() {}
  }

  return Stub
})
vi.mock('@/utils/chatSocket', () => ({ ChatSocket: ChatSocketStub, default: ChatSocketStub }))

import i18n from '@/i18n'
import ChatWindow from '@/views/chat/embed/ChatWindow.vue'

const olderRow = {
  id: 11,
  session_id: 'sess_older',
  created_at: NOW,
  last_message: '上次问的是发票的事'
}
const newestRow = {
  id: 12,
  session_id: 'sess_newest',
  created_at: NOW,
  last_message: '刚才问的是退货的事'
}

// 挂载 = 访客打开挂件：initVisitorId + loadOfflineSessions + openSession(true)
const mountWindow = async () => {
  const wrapper = mount(ChatWindow, {
    props: { channelId: 'default', channelTitle: '在线客服' },
    global: { plugins: [i18n] }
  })
  await flushPromises()
  await nextTick()
  return wrapper
}

// 访客点"查看"打开历史会话弹窗：横幅只在有离线消息时出现（走的是 socket 回调那条真实链路）
const openOfflineModal = async (wrapper) => {
  sockets[0].options.onOfflineMessages({
    messages: [{ id: 5, sender_type: 'agent', content: '坐席留言', created_at: NOW }]
  })
  await flushPromises()
  await nextTick()
  const btn = wrapper.find('.offline-banner button')
  expect(btn.exists()).toBe(true)
  await btn.trigger('click')
  await flushPromises()
  await nextTick()
  expect(wrapper.findAll('.session-item').length).toBe(2)
}

const itemByPreview = (wrapper, text) => {
  const hit = wrapper.findAll('.session-item').filter(n => n.text().includes(text))
  expect(hit.length).toBe(1)
  return hit[0]
}

// 访客在窗口里能看见的那句话：system 气泡（sender_type=system 才会带 .bubble.system）
const lastSystemBubble = (wrapper) => {
  const rows = wrapper.findAll('.bubble.system')
  expect(rows.length).toBeGreaterThan(0)
  return rows[rows.length - 1].text()
}

describe('embed 访客恢复历史会话时的 visitor_token 归属', () => {
  beforeEach(() => {
    localStorage.clear()
    localStorage.setItem('chat_visitor_id', VISITOR_ID)
    // 钉住语言：jsdom 的 navigator.language 是 en，不钉的话提示会渲染成英文，
    // 断言就跟着宿主机器变（文案本体由 scripts/check-i18n-coverage.cjs 门覆盖九语）。
    i18n.global.locale.value = 'zh'
    seq.length = 0
    sockets.length = 0

    api.getAvailableAgents.mockImplementation(() => {
      seq.push('agents')
      return Promise.resolve({ code: 0, data: { available: 2 } })
    })
    api.openSession.mockImplementation(() => {
      seq.push('openSession')
      // is_new_session=true ⇒ 挂载时不会去拉历史，调用序列才干净
      return Promise.resolve({
        code: 0,
        data: {
          session: { session_id: 'sess_newest', is_new_session: true },
          visitor_token: TOKEN_NEW,
          welcome_message: ''
        }
      })
    })
    api.getRecentClosedSessions.mockImplementation(() => {
      seq.push('recentClosed')
      return Promise.resolve({ code: 0, data: { list: [olderRow, newestRow] } })
    })
    api.exchangeSessionToken.mockImplementation(() => {
      seq.push('exchange')
      return Promise.resolve({ code: 0, data: { visitor_token: TOKEN_OLD } })
    })
    api.getMessages.mockImplementation(() => {
      seq.push('messages')
      return Promise.resolve({
        code: 0,
        data: { list: [{ id: 91, sender_type: 'agent', content: '发票已经开了', created_at: NOW }] }
      })
    })
    api.getOfflineMessages.mockImplementation(() => {
      seq.push('offlineMessages')
      return Promise.resolve({ code: 0, data: { list: [] } })
    })
    api.closeSession.mockImplementation(() => {
      seq.push('close')
      return Promise.resolve({ code: 0, data: {} })
    })
  })

  afterEach(() => {
    vi.clearAllMocks()
    document.body.replaceChildren()
  })

  it('点开更早的会话：先为这条会话换证，再带这条会话的证去拉历史', async () => {
    const wrapper = await mountWindow()
    await openOfflineModal(wrapper)

    await itemByPreview(wrapper, '发票').trigger('click')
    await flushPromises()
    await nextTick()

    // 换证只发生一次，且打的是"这条会话 + 本访客 + 本渠道"
    expect(api.exchangeSessionToken).toHaveBeenCalledTimes(1)
    expect(api.exchangeSessionToken.mock.calls[0]).toEqual(['sess_older', 'default', VISITOR_ID])

    // 顺序：换证在拉历史之前。只看"两个都被调过"是量不到顺序的 —— 而缺陷正是带着旧证去拉。
    expect(seq.indexOf('exchange')).toBeGreaterThanOrEqual(0)
    expect(seq.indexOf('messages')).toBeGreaterThan(seq.indexOf('exchange'))

    // 落点：拉历史带出去的证必须是刚换来的这条会话自己的
    expect(api.getMessages).toHaveBeenCalledTimes(1)
    const args = api.getMessages.mock.calls[0]
    expect(args[0]).toBe('sess_older')
    expect(args[3]).toBe('default')
    expect(args[4]).toBe(VISITOR_ID)
    expect(args[5]).toBe(TOKEN_OLD)
    expect(args[5]).not.toBe(TOKEN_NEW)

    // 访客可见的结果：那条会话的历史真的出现在窗口里
    expect(wrapper.text()).toContain('发票已经开了')

    // 续聊用的 socket 也得换到新会话/新证上，否则回复推不进这条会话
    const latest = sockets[sockets.length - 1]
    expect(latest.options.sessionId).toBe('sess_older')
    expect(latest.options.visitorToken).toBe(TOKEN_OLD)

    wrapper.unmount()
  })

  it('换证失败：不切会话（点× 时 close 仍打在原来那条），并给出看得见的提示', async () => {
    const wrapper = await mountWindow()
    await openOfflineModal(wrapper)

    api.exchangeSessionToken.mockImplementationOnce(() => {
      seq.push('exchange')
      return Promise.reject({ response: { status: 403 } })
    })

    await itemByPreview(wrapper, '发票').trigger('click')
    await flushPromises()
    await nextTick()

    // 没换到证就不该去拉历史
    expect(api.getMessages).not.toHaveBeenCalled()
    expect(lastSystemBubble(wrapper)).toBe('历史会话加载失败，请稍后重试')

    // sessionId 没动的证据：右上角 × 关的是原来那条
    await wrapper.find('.close-btn').trigger('click')
    await flushPromises()
    expect(api.closeSession).toHaveBeenCalledTimes(1)
    expect(api.closeSession.mock.calls[0][0]).toBe('sess_newest')

    wrapper.unmount()
  })

  it('换证返回里没有 token：同样不切会话，不能拿空证去敲门', async () => {
    const wrapper = await mountWindow()
    await openOfflineModal(wrapper)

    api.exchangeSessionToken.mockImplementationOnce(() => {
      seq.push('exchange')
      return Promise.resolve({ code: 0, data: {} })
    })

    await itemByPreview(wrapper, '发票').trigger('click')
    await flushPromises()
    await nextTick()

    expect(api.getMessages).not.toHaveBeenCalled()
    expect(lastSystemBubble(wrapper)).toBe('历史会话加载失败，请稍后重试')

    await wrapper.find('.close-btn').trigger('click')
    await flushPromises()
    expect(api.closeSession.mock.calls[0][0]).toBe('sess_newest')

    wrapper.unmount()
  })

  it('键盘 Enter 打开列表项走的也是同一条换证腿', async () => {
    const wrapper = await mountWindow()
    await openOfflineModal(wrapper)

    await itemByPreview(wrapper, '发票').trigger('keydown.enter')
    await flushPromises()
    await nextTick()

    expect(api.exchangeSessionToken).toHaveBeenCalledTimes(1)
    expect(api.exchangeSessionToken.mock.calls[0]).toEqual(['sess_older', 'default', VISITOR_ID])
    expect(api.getMessages.mock.calls[0][5]).toBe(TOKEN_OLD)

    wrapper.unmount()
  })

  // —— 以下是"入口本身可达吗"这一腿 ——
  // 实测（真浏览器 + 已攒够两条已结束会话的访客）：挂件只把"查看"挂在未读横幅里
  // （v-if="offlineBannerCount > 0"），而横幅只在 socket 推了离线消息时才亮。
  // 于是 recent-closed 已经取回 2 条数据、页面上却一个入口都没有，
  // 老访客永远走不到"历史会话"。上面那整套换证修复也就跟着变成够不着的代码。

  it('有历史会话但没有未读消息：页面上就有可见入口，点开能列出这两条', async () => {
    const wrapper = await mountWindow()

    // 不推任何离线消息 ⇒ offlineBannerCount 停在 0，入口仍必须出现
    const banner = wrapper.find('.offline-banner')
    expect(banner.exists()).toBe(true)
    expect(banner.text()).toContain('历史会话')
    expect(banner.text()).toContain('2')

    await banner.find('button').trigger('click')
    await flushPromises()
    await nextTick()
    expect(wrapper.findAll('.session-item').length).toBe(2)

    wrapper.unmount()
  })

  it('没有历史会话时不出现这个入口', async () => {
    api.getRecentClosedSessions.mockImplementationOnce(() => {
      seq.push('recentClosed')
      return Promise.resolve({ code: 0, data: { list: [] } })
    })
    const wrapper = await mountWindow()

    expect(wrapper.find('.offline-banner').exists()).toBe(false)
    expect(wrapper.findAll('.session-item').length).toBe(0)

    wrapper.unmount()
  })

  it('未读横幅亮着时不并排出第二个入口，且横幅文案走翻译', async () => {
    const wrapper = await mountWindow()
    expect(wrapper.findAll('.offline-banner').length).toBe(1)

    sockets[0].options.onOfflineMessages({
      messages: [{ id: 5, sender_type: 'agent', content: '坐席留言', created_at: NOW }]
    })
    await flushPromises()
    await nextTick()

    const banners = wrapper.findAll('.offline-banner')
    expect(banners.length).toBe(1)
    expect(banners[0].text()).toContain('您有 1 条未读消息')

    wrapper.unmount()
  })
})
