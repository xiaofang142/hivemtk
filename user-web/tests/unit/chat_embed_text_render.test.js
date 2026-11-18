import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import ChatMessages from '@/views/chat/embed/components/ChatMessages.vue'

let wrapper
afterEach(() => wrapper?.unmount())

function bubble(content) {
  wrapper = mount(ChatMessages, {
    props: { messages: [{ id: 1, sender_type: 'user', content }], loading: false, typing: false },
    global: { stubs: { RichCard: true, CitationList: true } },
  })
  return wrapper.get('.bubble-content').element
}

describe('聊天文本内容不经过HTML解释器', () => {
  it('保留换行、空格和字符实体，而不是生成br元素', () => {
    const content = '第一行  保留空格\n第二行 &amp; < > "'
    const element = bubble(content)
    expect(element.textContent).toBe(content)
    expect(element.children).toHaveLength(0)
  })

  it('标签、事件处理器与脚本只作为原样文字显示', () => {
    const content = '<img src=x onerror="alert(1)"><script>alert(1)</script>\n<a href="javascript:alert(1)">链接</a>'
    const element = bubble(content)
    expect(element.textContent).toBe(content)
    expect(element.querySelector('img, script, a')).toBeNull()
    expect(element.children).toHaveLength(0)
  })

  it.each([null, undefined, ''])('空值保持空气泡：%s', content => {
    expect(bubble(content).textContent).toBe('')
  })
})
