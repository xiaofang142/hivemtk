import { afterEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import BulkMessaging from '@/views/whatsappBot/BulkMessaging.vue'
import i18n from '@/i18n'
import { settle, settleBody } from './settle-helper'

const boundary = vi.hoisted(() => ({ post: vi.fn(), templates: [
  { id: 'preview-a', name: '格式预览A', content: '<p><strong>{{name}}</strong>，你好</p><img src="x" onerror="window.previewExecuted=true"><script>window.previewExecuted=true</script>', isActive: true },
  { id: 'preview-b', name: '格式预览B', content: '<p>第二份模板 {{company}}</p>', isActive: true },
] }))
vi.mock('@/utils/request', () => ({ http: {
  get: vi.fn(url => Promise.resolve(url === '/api/whatsapp/templates' ? boundary.templates : [])),
  post: boundary.post,
} }))

let wrapper
afterEach(() => {
  wrapper?.unmount()
  document.body.innerHTML = ''
  vi.clearAllMocks()
})

async function selectTemplate(name) {
  await wrapper.findAll('.el-select')[0].trigger('click')
  await settleBody()
  const option = [...document.querySelectorAll('.el-select-dropdown__item')].find(item => item.textContent.trim() === name)
  expect(option, '合成模板选项必须存在').toBeTruthy()
  option.dispatchEvent(new MouseEvent('click', { bubbles: true }))
  await settle(wrapper)
}

async function open() {
  wrapper = mount(BulkMessaging, { attachTo: document.body, global: { plugins: [ElementPlus, i18n] } })
  await settle(wrapper)
  await selectTemplate('格式预览A')
}

describe('批量消息只预览，不发送', () => {
  it('完整净化HTML放独立沙箱，不插入管理端文档', async () => {
    await open()
    const frame = wrapper.get('.preview-card').find('iframe')
    expect(frame.exists(), '格式预览应隔离到iframe').toBe(true)
    expect(frame.attributes('sandbox')).toBe('')
    expect(frame.attributes('referrerpolicy')).toBe('no-referrer')
    expect(frame.attributes('title')).toBe('消息预览')
    expect(frame.attributes('srcdoc')).toContain('<strong>张三</strong>')
    expect(frame.attributes('srcdoc')).not.toContain('<script')
    expect(frame.attributes('srcdoc')).not.toContain('onerror')
    expect(wrapper.find('.preview-card strong').exists()).toBe(false)
    expect(boundary.post).not.toHaveBeenCalled()
  })

  it('既有变量替换与切换模板保留，预览选择不会发送', async () => {
    await open()
    const previewHTML = () => {
      const frame = wrapper.find('.preview-card iframe')
      return frame.exists() ? frame.attributes('srcdoc') : wrapper.get('.preview-card').html()
    }
    expect(previewHTML()).toContain('<strong>张三</strong>')
    expect(previewHTML()).not.toContain('onerror')
    expect(previewHTML()).not.toContain('<script')
    await selectTemplate('格式预览B')
    expect(previewHTML()).toContain('第二份模板 示例公司')
    expect(previewHTML()).not.toContain('<strong>张三</strong>')
    expect(boundary.post).not.toHaveBeenCalled()
  })
})
