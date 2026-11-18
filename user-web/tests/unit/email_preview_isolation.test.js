import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { mount } from '@vue/test-utils'
import ElementPlus from 'element-plus'
import DragEditor from '@/views/email/DragEditor.vue'
import { settle, settleBody } from './settle-helper'

const boundary = vi.hoisted(() => ({
  html: '<!doctype html><html><head><style>body{color:red}</style></head><body><p>完整邮件</p><script>window.previewExecuted=true</script></body></html>',
  compile: vi.fn(),
  post: vi.fn(),
}))
vi.mock('mjml-browser', () => ({ default: boundary.compile }))
vi.mock('@/utils/request', () => ({ http: { post: boundary.post } }))

let wrapper
beforeEach(() => {
  vi.clearAllMocks()
  boundary.compile.mockReturnValue({ html: boundary.html, errors: [] })
  boundary.post.mockResolvedValue({})
})
afterEach(() => {
  wrapper?.unmount()
  document.body.innerHTML = ''
})

async function openPreview(label) {
  wrapper = mount(DragEditor, { attachTo: document.body, global: { plugins: [ElementPlus] } })
  await settle(wrapper)
  const button = wrapper.findAll('button').find(item => item.text().includes(label))
  expect(button, '真实预览按钮应存在').toBeTruthy()
  await button.trigger('click')
  await settleBody()
  return document.querySelector('.preview-frame')
}

describe('邮件预览隔离而不修改邮件载荷', () => {
  it.each(['桌面预览', '移动预览'])('%s使用没有脚本/同源/导航权限的独立文档', async label => {
    const frame = await openPreview(label)
    expect(frame?.tagName).toBe('IFRAME')
    expect(frame.getAttribute('sandbox')).toBe('')
    expect(frame.getAttribute('referrerpolicy')).toBe('no-referrer')
    expect(frame.getAttribute('title')).toBe('预览')
    expect(frame.getAttribute('srcdoc')).toContain(boundary.html)
    expect(document.querySelector('.el-dialog script, .el-dialog style')).toBeNull()
    expect(boundary.post).not.toHaveBeenCalled()
    if (label === '移动预览') expect(frame.getAttribute('srcdoc')).toContain('max-width:375px')
  })

  it('预览隔离不改变保存的完整邮件HTML', async () => {
    await openPreview('桌面预览')
    const save = wrapper.findAll('button').find(item => item.text() === '保存')
    expect(save).toBeTruthy()
    await save.trigger('click')
    await settle(wrapper)
    expect(boundary.post).toHaveBeenCalledExactlyOnceWith('/api/email/drafts', {
      subject: '未命名拖拽模板', content: boundary.html,
    })
  })
})
