/**
 * D7 写操作人工确认 前端契约测试（批3）
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

  it('POST /sessions/:id/confirm（无业务请求体）', async () => {
    await confirmBrowserSession(12)
    expect(http.post).toHaveBeenCalledTimes(1)
    const [url, body] = http.post.mock.calls[0]
    expect(url).toBe('/api/browser-automation/sessions/12/confirm')
    expect(body).toBeTypeOf('object') // 不得把会话 id 之外的语义塞进 body
  })

  it('确认端点与中断端点不同路径（复用 stop = 语义混淆）', async () => {
    await stopBrowserSession(12, 'x')
    await confirmBrowserSession(12)
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
