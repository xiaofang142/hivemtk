import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest'
import { defineComponent, h, ref } from 'vue'
import { mount } from '@vue/test-utils'
import { settle } from './settle-helper'
const api = vi.hoisted(() => ({ get: vi.fn(), post: vi.fn() }))
vi.mock('@/utils/request', () => ({ http: api }))
vi.mock('@/utils/agentSocket', () => ({ default: class { connect() {} close() {} } }))
import { useSessionList } from '@/views/customerSession/composables/useSessionList'
let wrapper, list
function mountPanel() {
  wrapper = mount(defineComponent({ setup() {
    list = useSessionList({ sessions: ref([]), inputMsg: ref(''), findSession: () => null, upsertSession: () => {} })
    return () => h('div', `${list.profileStats.value.messageCount}|${list.profileStats.value.sessionCount}|${list.profileStats.value.aiReplyCount}|${list.profileTags.value.join(',')}|${list.messages.value.map(m => m.content).join(',')}|${list.aiSuggestions.value.map(s => s.suggestion).join(',')}`)
  } }))
}
beforeEach(() => {
  api.get.mockReset()
  api.get.mockResolvedValue([])
})
afterEach(() => wrapper?.unmount())
const session = (id) => ({ id, customerId: `u${id}`, sessionId: `s${id}`, handlerType: 'human' })
describe('客服画像与异步隔离', () => {
  it('标签保存失败保留旧标签，成功后才更新会话', async () => {
    mountPanel()
    await list.selectSession({ ...session(1), tags: ['1'] })
    list.allTags.value = [{ id: 1, name: '旧标签' }, { id: 2, name: '新标签' }]
    list.loadSessionTags()
    api.post.mockRejectedValueOnce(new Error('controlled failure')).mockResolvedValueOnce(null)
    await list.addSessionTag(2)
    expect(list.sessionTags.value.map(tag => tag.id)).toEqual([1])
    await list.addSessionTag(2)
    expect(list.sessionTags.value.map(tag => tag.id)).toEqual([1, 2])
    expect(api.post).toHaveBeenLastCalledWith('/api/customer-sessions/1/tags', { tags: ['1', '2'] })
  })
  it('真实统计和标签契约显示为非零数据', async () => {
    api.get.mockImplementation(url => Promise.resolve(url.includes('/stats') ? { session_stats: { total_messages: 42, total_sessions: 3, ai_replies: 19 } } : url.includes('/tags?') ? { user_id: 'u1', tags: ['老客户'] } : []))
    mountPanel()
    await list.selectSession(session(1))
    await list.loadCustomerProfile()
    await settle(wrapper)
    expect(wrapper.text()).toContain('42|3|19|老客户')
  })
  it('选另一个会话后旧消息立即清空，加载失败不显示上一位客户消息', async () => {
    api.get.mockImplementation(url => {
      if (url === '/api/customer-sessions/1/messages') return Promise.resolve({ list: [{ id: 1, sender_type: 'user', content: '客户一消息' }] })
      if (url === '/api/customer-sessions/2/messages') return Promise.reject(new Error('controlled failure'))
      return Promise.resolve([])
    })
    mountPanel()
    await list.selectSession(session(1))
    await settle(wrapper)
    expect(wrapper.text()).toContain('客户一消息')
    await list.selectSession(session(2))
    await settle(wrapper)
    expect(wrapper.text()).not.toContain('客户一消息')
  })
  it('旧画像和建议的慢响应不能覆盖当前会话', async () => {
    let resolveStats, resolveTags, resolveSuggestions
    api.get.mockImplementation(url => {
      if (url.includes('/stats?user_id=u1')) return new Promise(r => { resolveStats = r })
      if (url.includes('/tags?user_id=u1')) return new Promise(r => { resolveTags = r })
      if (url.endsWith('/ai-suggestions/s1')) return new Promise(r => { resolveSuggestions = r })
      return Promise.resolve(url.includes('/stats') ? { session_stats: { total_messages: 7 } } : url.includes('/tags?') ? { tags: ['当前客户'] } : [])
    })
    mountPanel()
    list.currentSession.value = session(1)
    const oldProfile = list.loadCustomerProfile(), oldSuggestions = list.loadAiSuggestions()
    await list.selectSession(session(2))
    await list.loadCustomerProfile()
    resolveStats({ session_stats: { total_messages: 999 } })
    resolveTags({ tags: ['旧客户'] })
    resolveSuggestions([{ suggestion: '旧建议' }])
    await Promise.all([oldProfile, oldSuggestions])
    await settle(wrapper)
    expect(wrapper.text()).toContain('7|0|0|当前客户')
    expect(wrapper.text()).not.toMatch(/999|旧客户|旧建议/)
  })
})
