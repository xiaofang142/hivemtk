import { afterEach, describe, expect, it } from 'vitest'
import { createMemoryHistory, createRouter } from 'vue-router'
import { IframePanel } from '../../../embed-sdk/src/iframe-panel.js'

const panels = []
afterEach(() => {
  for (const panel of panels) panel.destroy()
})

describe('SDK iframe 路由参数', () => {
  it.each([
    ['欢迎 & hello / ? # 100% + 世界', 'en-US'],
    ['', 'zh-CN'],
    ['字面编码 %26 %25 与 +', 'zh-TW'],
  ])('真实 router 保留 welcome=%s 和语言=%s', async (welcome, lang) => {
    const panel = new IframePanel({ apiBaseURL: 'https://chat.example.test', channelId: 'shop / &中文', welcome, lang })
    panels.push(panel)
    panel.create()
    const url = new URL(panel.iframe.src)
    const router = createRouter({ history: createMemoryHistory(), routes: [{ path: '/chat/embed/:channelRef', component: { template: '<div />' } }] })
    await router.push(url.hash.slice(1))
    expect(router.currentRoute.value.params.channelRef).toBe('shop / &中文')
    expect(router.currentRoute.value.query.welcome || '').toBe(welcome)
    expect(router.currentRoute.value.query.lang).toBe(lang)
    expect(router.currentRoute.value.query.mcwParent).toBe(window.location.origin)
  })
})
