/**
 * 安全审计 E2E 测试
 */
import { test, expect, request } from '@playwright/test'

test.skip(!process.env.E2E_ADMIN_PASSWORD, 'E2E_ADMIN_PASSWORD not set')

const API_BASE = 'http://localhost:8204'

async function getAdminToken() {
  const ctx = await request.newContext({ baseURL: API_BASE })
  const resp = await ctx.post('/api/auth/login', {
    data: { username: 'admin', password: process.env.E2E_ADMIN_PASSWORD }
  })
  const body = await resp.json()
  return body.data?.token
}

test.describe('P2-5 安全审计 E2E', () => {
  test('API: 审计历史列表', async () => {
    const token = await getAdminToken()
    const ctx = await request.newContext({
      baseURL: API_BASE,
      extraHTTPHeaders: { Authorization: `Bearer ${token}` }
    })
    const resp = await ctx.get('/api/security/audit/list', { params: { page: 1, page_size: 5 } })
    expect(resp.ok()).toBeTruthy()
    const body = await resp.json()
    expect(body.code).toBe('SUCCESS')
    expect(Array.isArray(body.data.list)).toBe(true)
  })

  test('API: 启动审计任务', async () => {
    const token = await getAdminToken()
    const ctx = await request.newContext({
      baseURL: API_BASE,
      extraHTTPHeaders: { Authorization: `Bearer ${token}` }
    })
    const resp = await ctx.post('/api/security/audit', {
      data: { audit_name: `e2e_audit_${Date.now()}` }
    })
    expect(resp.ok()).toBeTruthy()
    const body = await resp.json()
    expect(body.code).toBe('SUCCESS')
    expect(body.data).toHaveProperty('id')
    expect(body.data).toHaveProperty('status')
    expect(body.data).toHaveProperty('score')
    expect(typeof body.data.score).toBe('number')
  })

  test('API: 审计详情含 results', async () => {
    const token = await getAdminToken()
    const ctx = await request.newContext({
      baseURL: API_BASE,
      extraHTTPHeaders: { Authorization: `Bearer ${token}` }
    })
    // 先启动一次
    const runResp = await ctx.post('/api/security/audit', {
      data: { audit_name: `detail_check_${Date.now()}` }
    })
    const auditId = (await runResp.json()).data.id

    // 取详情
    const detailResp = await ctx.get(`/api/security/audit/${auditId}`)
    expect(detailResp.ok()).toBeTruthy()
    const body = await detailResp.json()
    expect(body.data.id).toBe(auditId)
    // 后端返回 results 数组
    expect(Array.isArray(body.data.results)).toBe(true)
    expect(body.data.results.length).toBeGreaterThan(0)
  })
})
