/**
 * D7 放行闭环的前端契约：审批绑载荷 + 结论四态分流
 * 服务端口径：不带 payload_hash 的放行一律 400，闸门挂起态与结论走 data.status。
 * 前端必须跟着变，且变完仍是 fail-closed：
 * ① 哈希只能来自 GET confirm-gate（预览的就是要批的那一份）；
 * ② 没拿到哈希就不发请求（一条空白支票送到服务端也是支票）；
 * ③ 只有 granted 能宣布「已放行」，四个态各说各的话，拿不到 status 一律 unknown。
 */
import { describe, it, expect, vi, beforeEach } from 'vitest'
import { readFileSync } from 'node:fs'
import { resolve } from 'node:path'

const { http } = vi.hoisted(() => ({
  http: { get: vi.fn(), post: vi.fn(), put: vi.fn(), delete: vi.fn() }
}))
vi.mock('@/utils/http', () => ({ http }))

import {
  getBrowserConfirmGate,
  confirmBrowserSession,
  interpretConfirmResult
} from '@/api/browserAutomation.js'

const readSrc = (p) => readFileSync(resolve(process.cwd(), 'src', p), 'utf8')

// 拦截器成功时回 data.data、409 时 reject 一个带 err.response.data 的 Error，
// 两种形状都要过同一个判读函数，否则「不符」和「网络断了」在前端会长得一样。
const httpError = (status, data) => {
  const err = new Error(data?.message || 'request failed')
  err.response = { status, data }
  return err
}

describe('读侧：闸门详情端点', () => {
  beforeEach(() => http.get.mockReset())

  it('GET /sessions/:id/confirm-gate（payload_hash 的唯一来源）', async () => {
    http.get.mockResolvedValue({
      pending: true,
      gate: { step_index: 3, payload_hash: 'cafe1234', preview: '测试评论', expires_at: '2026-09-21T10:00:00Z' }
    })
    const res = await getBrowserConfirmGate(77)
    expect(http.get.mock.calls[0][0]).toBe('/api/browser-automation/sessions/77/confirm-gate')
    expect(res.gate.payload_hash).toBe('cafe1234')
  })
})

describe('写侧：放行必须指明批的是哪份载荷', () => {
  beforeEach(() => {
    http.post.mockReset()
    http.post.mockResolvedValue({ confirmed: true, status: 'granted' })
  })

  it('POST body 带 payload_hash（不再是空对象）', async () => {
    await confirmBrowserSession(77, 'deadbeef')
    const [url, body] = http.post.mock.calls[0]
    expect(url).toBe('/api/browser-automation/sessions/77/confirm')
    expect(body).toEqual({ payload_hash: 'deadbeef' })
  })

  it('缺哈希 / 全空白：本地拒绝且一个请求都不发', async () => {
    for (const bad of [undefined, null, '', '   ']) {
      await expect(confirmBrowserSession(77, bad)).rejects.toThrow(/payload_hash/)
    }
    expect(http.post).not.toHaveBeenCalled()
  })

  it('_silent：四个态的文案由页面给，拦截器不再重复弹一条', async () => {
    await confirmBrowserSession(77, 'deadbeef')
    expect(http.post.mock.calls[0][2]).toMatchObject({ _silent: true })
  })
})

describe('四态分流：结论只认 status，不认文案', () => {
  it('granted 是唯一能宣布放行的态', () => {
    expect(interpretConfirmResult({ confirmed: true, status: 'granted' }))
      .toMatchObject({ released: true, status: 'granted' })
  })

  it.each(['no_gate', 'payload_mismatch', 'gate_on_another_instance'])(
    '%s → released=false（200 与 409 都不许被读成成功）',
    (status) => {
      expect(interpretConfirmResult({ confirmed: false, status }).released).toBe(false)
      expect(interpretConfirmResult({ confirmed: false, status }).status).toBe(status)
    }
  )

  it('409 载荷不符：从错误对象里取到 status 与重高亮用的 gate', () => {
    const err = httpError(409, {
      code: 'DUPLICATE_ENTRY_3003',
      message: '放行的载荷与挂起中的提交内容不一致',
      data: { confirmed: false, status: 'payload_mismatch', gate: { payload_hash: 'newhash' } }
    })
    const v = interpretConfirmResult(err)
    expect(v.released).toBe(false)
    expect(v.status).toBe('payload_mismatch')
    expect(v.gate?.payload_hash).toBe('newhash')
  })

  it('无 status 的旧响应：confirmed 布尔仍能读出 granted / no_gate', () => {
    expect(interpretConfirmResult({ confirmed: true }).status).toBe('granted')
    expect(interpretConfirmResult({ confirmed: false }).status).toBe('no_gate')
  })

  it('网络失败 / 空响应 → unknown 且不 released（没证据就不许替用户编一个结论）', () => {
    expect(interpretConfirmResult(new Error('Network Error'))).toMatchObject({ released: false, status: 'unknown' })
    expect(interpretConfirmResult(undefined).status).toBe('unknown')
    expect(interpretConfirmResult({ status: 'weird_new_state' }).released).toBe(false)
  })
})

describe('监控页接线', () => {
  const monitor = readSrc('views/browserAutomation/Monitor.vue')

  it('挂起时先取闸门详情，展示第几步 / 将要提交什么 / 到什么时候', () => {
    expect(monitor).toContain('getBrowserConfirmGate')
    expect(monitor).toContain('payload_hash')
    expect(monitor).toContain('gate?.preview')
    expect(monitor).toContain('gate?.step_index')
    expect(monitor).toContain('gate?.expires_at')
  })

  it('放行用取到的哈希，没取到不放行', () => {
    expect(monitor).toContain('gate.value?.payload_hash')
    expect(monitor).toMatch(/confirmBrowserSession\(sessionId\.value,\s*hash\)/)
    expect(monitor).toContain('interpretConfirmResult')
  })

  it('四个态各有独立文案（把「闸门在别处」说成「没有闸门」会把用户送去改编排）', () => {
    for (const status of ['granted', 'no_gate', 'payload_mismatch', 'gate_on_another_instance']) {
      expect(monitor).toContain(`'${status}'`)
    }
  })

  it('会话未就位的首帧不得裸访问 session.xxx', () => {
    expect(monitor).not.toMatch(/v-if="session\.confirm_pending"/)
    expect(monitor).toContain('session?.confirm_pending')
  })
})
