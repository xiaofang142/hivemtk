import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import { defineComponent } from 'vue'
import { createPinia } from 'pinia'
import { createMemoryHistory, createRouter, RouterView } from 'vue-router'
import ElementPlus, { ElTable, ElTransfer } from 'element-plus'
import LeadGroupSelection from '@/views/whatsappBot/LeadGroupSelection.vue'
import BulkMessaging from '@/views/whatsappBot/BulkMessaging.vue'
import { useUserStore } from '@/stores/user'
import i18n from '@/i18n'
import { settle, settleBody } from './settle-helper'

const api = vi.hoisted(() => ({ post: vi.fn(), leads: [] }))
vi.mock('@/utils/request', () => ({ http: {
  get: vi.fn(url => Promise.resolve(url.startsWith('/api/clues/list')
    ? { list: api.leads, total: api.leads.length }
    : url === '/api/whatsapp/templates'
      ? [{ id: 'template', name: '合成模板', content: '你好 {{name}}' }]
      : url.startsWith('/api/whatsapp/accounts') ? [{ id: 'account', name: '合成账号' }] : [])),
  post: api.post,
} }))

let wrapper
let router
let userStore
async function openSelection() {
  api.leads = [{ id: 'lead-a', name: '测试甲', account: '+15550001001' },
    { id: 'lead-b', name: '测试乙', account: '+15550001002' }]
  const pinia = createPinia()
  userStore = useUserStore(pinia)
  userStore.setUserInfo({ id: 'user-a', tenant_id: 'tenant-a' })
  router = createRouter({ history: createMemoryHistory(), routes: [
    { path: '/whatsapp/lead-group-selection', component: LeadGroupSelection },
    { path: '/whatsapp/group-messaging', component: BulkMessaging },
  ] })
  await router.push('/whatsapp/lead-group-selection')
  await router.isReady()
  const view = defineComponent({ components: { RouterView }, template: '<RouterView v-slot="{ Component }"><component :is="Component" :key="$route.fullPath" /></RouterView>' })
  wrapper = mount(view, { attachTo: document.body, global: { plugins: [pinia, router, ElementPlus, i18n] } })
  await settle(wrapper)
}

async function handoff() {
  const table = wrapper.findComponent(ElTable)
  const chosen = table.props('data')[0]
  expect(chosen.id, '必须保留当前后端小写 id').toBe('lead-a')
  table.vm.$emit('selection-change', [chosen])
  await settle(wrapper)
  await wrapper.findAll('button').find(button => button.text().includes('下一步')).trigger('click')
  await settle(wrapper)
}

async function reviewRecipients() {
  const selects = wrapper.findAllComponents({ name: 'ElSelect' })
  selects[0].vm.$emit('update:modelValue', 'template')
  selects[1].vm.$emit('update:modelValue', 'account')
  await settle(wrapper)
  await wrapper.findAll('button').find(button => button.text() === '立即发送').trigger('click')
  await settleBody()
}

afterEach(() => {
  wrapper?.unmount()
  document.body.innerHTML = ''
  localStorage.clear()
  vi.clearAllMocks()
})

describe('NAV04 线索选择到群发页面', () => {
  it('实际路由预选当前联系人并一次性消费，直接重访不复用旧选择', async () => {
    await openSelection()
    await handoff()
    expect(router.currentRoute.value.path).toBe('/whatsapp/group-messaging')
    expect(wrapper.findComponent(ElTransfer).props('modelValue')).toEqual(['lead-a'])
    expect(wrapper.get('[data-testid="bulk-recipients"]').text()).toContain('测试甲')
    expect(wrapper.get('[data-testid="bulk-recipients"]').text()).toContain('+15550001001')
    expect(router.currentRoute.value.query.leadSelection).toEqual(expect.any(String))
    expect(localStorage.getItem('selectedLeads')).toBeNull()
    expect(api.post).not.toHaveBeenCalled()
    await router.push('/whatsapp/lead-group-selection')
    await router.push('/whatsapp/group-messaging')
    await settle(wrapper)
    expect(wrapper.findComponent(ElTransfer).props('modelValue')).toEqual([])
    expect(api.post).not.toHaveBeenCalled()
  })

  it('重置清空收件人，不触发发送', async () => {
    await openSelection()
    await handoff()
    await wrapper.findAll('button').find(button => button.text() === '重置').trigger('click')
    await settle(wrapper)
    expect(wrapper.findComponent(ElTransfer).props('modelValue')).toEqual([])
    expect(wrapper.find('[data-testid="bulk-recipients"]').exists()).toBe(false)
    expect(api.post).not.toHaveBeenCalled()
  })

  it('发送前展示真实名单，取消确认保持零 POST', async () => {
    await openSelection()
    await handoff()
    await reviewRecipients()
    expect(document.body.querySelector('[data-testid="bulk-send-confirmation"]').textContent).toContain('+15550001001')
    expect(api.post).not.toHaveBeenCalled()
    const cancel = document.body.querySelector('[data-testid="bulk-cancel-send"]')
    cancel.click()
    await settleBody()
    expect(api.post).not.toHaveBeenCalled()
  })

  it('确认过程中号码变化要求重新核对，第二次明确确认仅创建一次任务', async () => {
    await openSelection()
    await handoff()
    await reviewRecipients()
    api.leads[0].account = '+15550001999'
    document.body.querySelector('[data-testid="bulk-confirm-send"]').click()
    await settleBody()
    expect(api.post).not.toHaveBeenCalled()
    expect(document.body.querySelector('[data-testid="bulk-send-confirmation"]').textContent).toContain('+15550001999')
    api.post.mockResolvedValue({ message: '合成任务已创建' })
    document.body.querySelector('[data-testid="bulk-confirm-send"]').click()
    document.body.querySelector('[data-testid="bulk-confirm-send"]').click()
    await settleBody()
    expect(api.post).toHaveBeenCalledTimes(1)
    expect(api.post).toHaveBeenCalledWith('/api/whatsapp/group-messaging/send', {
      templateId: 'template', accountId: 'account', leadIds: ['lead-a'], scheduleAt: null,
    })
    expect(wrapper.findComponent(ElTransfer).props('modelValue')).toEqual([])
  })

  it('确认前联系人已删除，拒绝创建任务', async () => {
    await openSelection()
    await handoff()
    await reviewRecipients()
    api.leads = []
    document.body.querySelector('[data-testid="bulk-confirm-send"]').click()
    await settleBody()
    expect(api.post).not.toHaveBeenCalled()
  })

  it('切换租户清空选择并关闭旧身份的确认框', async () => {
    await openSelection()
    await handoff()
    await reviewRecipients()
    userStore.setUserInfo({ id: 'user-b', tenant_id: 'tenant-b' })
    await settle(wrapper)
    expect(wrapper.findComponent(ElTransfer).props('modelValue')).toEqual([])
    expect(api.post).not.toHaveBeenCalled()
  })

  it('消费过的路由标识和历史 localStorage 都不恢复收件人', async () => {
    await openSelection()
    await handoff()
    const consumedRoute = router.currentRoute.value.fullPath
    localStorage.setItem('selectedLeads', JSON.stringify(['lead-b']))
    await router.push('/whatsapp/lead-group-selection')
    await router.push(consumedRoute)
    await settle(wrapper)
    expect(wrapper.findComponent(ElTransfer).props('modelValue')).toEqual([])
    expect(api.post).not.toHaveBeenCalled()
  })
})
