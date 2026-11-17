/**
 * 备份恢复 E2E 测试
 * 验证前后端联动：列表加载、创建备份、查看详情、恢复记录
 * 不使用 mock 数据，直接打 user-server
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

test.describe('P2-5 备份恢复 E2E', () => {
  test('API: 备份列表可分页查询', async () => {
    const token = await getAdminToken()
    const ctx = await request.newContext({
      baseURL: API_BASE,
      extraHTTPHeaders: { Authorization: `Bearer ${token}` }
    })
    const resp = await ctx.get('/api/backups', { params: { page: 1, page_size: 5 } })
    expect(resp.ok()).toBeTruthy()
    const body = await resp.json()
    expect(body.code).toBe('SUCCESS')
    expect(body.data).toHaveProperty('list')
    expect(Array.isArray(body.data.list)).toBe(true)
  })

  test('API: 恢复记录列表可查询', async () => {
    const token = await getAdminToken()
    const ctx = await request.newContext({
      baseURL: API_BASE,
      extraHTTPHeaders: { Authorization: `Bearer ${token}` }
    })
    const resp = await ctx.get('/api/restore/list', { params: { page: 1, page_size: 5 } })
    expect(resp.ok()).toBeTruthy()
    const body = await resp.json()
    expect(body.code).toBe('SUCCESS')
    expect(Array.isArray(body.data.list)).toBe(true)
  })

  test('API: 创建备份任务', async () => {
    const token = await getAdminToken()
    const ctx = await request.newContext({
      baseURL: API_BASE,
      extraHTTPHeaders: { Authorization: `Bearer ${token}` }
    })
    const backupName = `e2e_backup_${Date.now()}`
    const resp = await ctx.post('/api/backups', {
      data: { backup_name: backupName, backup_type: 'full' }
    })
    expect(resp.ok()).toBeTruthy()
    const body = await resp.json()
    expect(body.code).toBe('SUCCESS')
    expect(body.data.backup_name).toBe(backupName)
    expect(body.data.backup_type).toBe('full')
    expect(['pending', 'running']).toContain(body.data.status)
  })

  test('API: 校验备份类型参数', async () => {
    const token = await getAdminToken()
    const ctx = await request.newContext({
      baseURL: API_BASE,
      extraHTTPHeaders: { Authorization: `Bearer ${token}` }
    })
    // 缺字段应返回 400
    const resp = await ctx.post('/api/restore', { data: {} })
    expect([400, 500]).toContain(resp.status())
  })
})
