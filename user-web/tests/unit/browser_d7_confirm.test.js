/**
 * 写操作人工确认（D7 闸门）的前端契约测试
 * 三件事必须成立：API 打到正确端点、监控页有放行入口（且只随 confirm_pending 出现）、
 * 编排页开关默认关闭（铁律 4 全自动不变）。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const { http } = vi.hoisted(() => ({
  http: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() }
}))
vi.mock('@/utils/http', () => ({ http }))

import { confirmBrowserSession, stopBrowserSession } from '@/api/browserAutomation.js'

const readSrc = (p) => readFileSync(resolve(process.cwd(), 'src', p), 'utf8')

describe('D7 确认放行 API', () => {
  beforeEach(() => {
    http.post.mockReset()
    http.post.mockResolvedValue({ code: 'SUCCESS', data: { confirmed: true } })
  })

  // 放行绑载荷：body 必须带 payload_hash（见 browser_d7_gate_b20.test.js）。
  // 这里原来断言「body 是对象就行」，那条宽松判据正好放过了「空对象也算放行」。
  it('POST /sessions/:id/confirm，body 只带放行载荷', async () => {
    await confirmBrowserSession(12, 'cafe1234')
    expect(http.post).toHaveBeenCalledTimes(1)
    const [url, body] = http.post.mock.calls[0]
    expect(url).toBe('/api/browser-automation/sessions/12/confirm')
    expect(body).toEqual({ payload_hash: 'cafe1234' }) // 会话 id 之外的语义只允许是「批的是哪份内容」
  })

  it('确认端点与中断端点不同路径（复用 stop = 语义混淆）', async () => {
    await stopBrowserSession(12, 'x')
    await confirmBrowserSession(12, 'cafe1234')
    const urls = http.post.mock.calls.map(([u]) => u)
    expect(urls).toContain('/api/browser-automation/sessions/12/stop')
    expect(urls).toContain('/api/browser-automation/sessions/12/confirm')
    expect(new Set(urls).size).toBe(2)
  })
})

describe('D7 界面接线', () => {
  it('监控页：确认按钮只随 confirm_pending 出现，且走 confirmBrowserSession', () => {
    const monitor = readSrc('views/browserAutomation/Monitor.vue')
    expect(monitor).toContain('session?.confirm_pending')
    expect(monitor).toContain('onConfirm')
    expect(monitor).toContain('confirmBrowserSession')
  })

  it('编排页：开关绑 require_confirm 且默认 false（默认全自动不破）', () => {
    const editor = readSrc('views/browserAutomation/Editor.vue')
    expect(editor).toContain('v-model="form.require_confirm"')
    expect(editor).toMatch(/require_confirm:\s*false/)
    // 开关只对准不可逆写原语：可见性由 post_comment 步（或 Brain 模式）驱动
    expect(editor).toContain("s.action === 'post_comment'")
  })

  it('详情页：写操作确认状态可读（配置与展示一致）', () => {
    expect(readSrc('views/browserAutomation/Detail.vue')).toContain('task.require_confirm')
  })
})
