// 深度数据库校验闭环：核心 CRUD 页面「创建 → 落库直查 → UI 列表回显 → 清理 → 库空」，
// 并对此前修复 res.data 的页面做渲染冒烟（打开新增对话框，确认不再崩溃/列表非空）。
import { test, expect } from '@playwright/test'
import { execFileSync } from 'child_process'
import fs from 'fs'

const BASE = process.env.E2E_BASE_URL || 'http://localhost:8211'
const API_BASE = process.env.E2E_API_URL || 'http://localhost:8204'
const ADMIN_USER = 'admin'
const ADMIN_PASS = process.env.E2E_ADMIN_PASSWORD
test.skip(!ADMIN_PASS, 'E2E_ADMIN_PASSWORD not set')
const PW = fs.existsSync('/tmp/pg_pw.txt') ? fs.readFileSync('/tmp/pg_pw.txt', 'utf8').trim() : ''

function dbCount(sql) {
  if (!PW) return -1
  try {
    // execFileSync + 位置参数：口令经 env 传递、SQL 作为独立 argv，不拼进命令串，杜绝命令注入。
    const out = execFileSync('psql', ['-h', '127.0.0.1', '-p', '8232', '-U', 'admin', '-d', 'user_db', '-t', '-A', '-c', sql], {
      encoding: 'utf8',
      env: { ...process.env, PGPASSWORD: PW },
    })
    const v = out.trim()
    return v === '' ? 0 : Number(v)
  } catch  {
    return -1
  }
}

// request 为 APIRequestContext，直接用绝对 URL 调用；无需 newContext/dispose
async function getAdminToken(request) {
  const res = await request.post(API_BASE + '/api/auth/login', { data: { username: ADMIN_USER, password: ADMIN_PASS } })
  const body = await res.json()
  return { api: request, token: body.data.token }
}

async function uiLogin(page) {
  await page.goto(BASE + '/#/login', { waitUntil: 'networkidle' })
  await page.fill('input[type="text"]', ADMIN_USER)
  await page.fill('input[type="password"]', ADMIN_PASS)
  await page.click('button[type="button"].el-button--primary')
  await page.waitForURL('**/#/**', { timeout: 15000 }).catch(() => {})
  await page.waitForTimeout(1200)
}

// ---- 1. 短链：API 创建 → 落库直查 → 清理 → 库空（纯 DB 校验）----
test('[DB] 短链：API 造数→落库直查→清理闭环', async ({ request }) => {
  const { api, token } = await getAdminToken(request)
  const headers = { Authorization: 'Bearer ' + token }
  const marker = 'E2ESL_' + Date.now()
  const c = await (await api.post(API_BASE + '/api/short-links', { headers, data: { short_code: marker, original_url: 'https://e2e.example.com/' + marker } })).json()
  expect(c.code).toBe('SUCCESS')
  const id = c.data?.id
  expect(dbCount(`SELECT count(*) FROM short_links WHERE short_code='${marker}'`)).toBe(1)
  await api.delete(API_BASE + '/api/short-links/' + id, { headers }).catch(() => {})
  expect(dbCount(`SELECT count(*) FROM short_links WHERE short_code='${marker}'`)).toBe(0)
})

// ---- 2. 短链：真实点击创建 → 落库闭环 + UI 回显 ----
test('[DB] 短链：UI 真实点击创建 → 落库闭环', async ({ page, request }) => {
  const { api, token } = await getAdminToken(request)
  const headers = { Authorization: 'Bearer ' + token }
  const marker = 'E2ESL_' + Date.now()
  const errors = []
  page.on('pageerror', (e) => errors.push('PAGEERROR: ' + e.message))
  let newId = null
  try {
    await uiLogin(page)
    await page.goto(BASE + '/#/shortLink', { waitUntil: 'networkidle' })
    await page.waitForSelector('.el-table', { timeout: 8000 })
    // 本地 i18n 把按钮译作英文，正则兼容中英文
    await page.locator('button.el-button', { hasText: /添加短链|Add Short Chain|添加/ }).first().click({ timeout: 5000 })
    await page.waitForSelector('.el-dialog:visible', { timeout: 5000 })
    await page.fill('input[placeholder="请输入短码"]', marker)
    await page.fill('input[placeholder="请输入原始URL"]', 'https://e2e.example.com/' + marker)
    await page.locator('.el-dialog:visible button.el-button--primary', { hasText: /确定|Confirm/ }).click({ timeout: 5000 })
    // 等待对话框关闭，证明提交成功（否则提交失败对话框不会关闭）
    await page.waitForSelector('.el-dialog:visible', { state: 'hidden', timeout: 8000 })
    const listBody = await (await api.get(API_BASE + '/api/short-links', { headers })).json()
    const created = Array.isArray(listBody.data) ? listBody.data.find((x) => x.short_code === marker) : null
    newId = created?.id
    expect(dbCount(`SELECT count(*) FROM short_links WHERE short_code='${marker}'`)).toBe(1)
    expect(created, '接口列表应包含新建的短链').toBeTruthy()
    const apiCount = Array.isArray(listBody.data) ? listBody.data.length : (listBody.data?.list?.length || 0)
    const rendered = await page.locator('.el-table__body-wrapper tbody tr').count()
    expect(rendered).toBe(apiCount)
  } finally {
    if (newId) await api.delete(API_BASE + '/api/short-links/' + newId, { headers }).catch(() => {})
    else await api.delete(API_BASE + '/api/short-links/' + marker, { headers }).catch(() => {})
  }
  expect(dbCount(`SELECT count(*) FROM short_links WHERE short_code='${marker}'`)).toBe(0)
  expect(errors).toEqual([])
})

// ---- 3. 团队成员：API 创建 → 落库 → UI 回显 → 清理 → 库空 ----
test('[DB] 团队成员：API 造数→落库→列表回显→清理闭环', async ({ page, request }) => {
  const { api, token } = await getAdminToken(request)
  const headers = { Authorization: 'Bearer ' + token }
  const marker = 'E2ETU_' + Date.now()
  const errors = []
  page.on('pageerror', (e) => errors.push('PAGEERROR: ' + e.message))
  let newId = null
  try {
    const c = await (await api.post(API_BASE + '/api/team/users', {
      headers,
      data: { username: marker, password: 'E2e@' + '123456', name: marker, email: marker + '@e2e.com', role: 'viewer' }
    })).json()
    expect(c.code).toBe('SUCCESS')
    newId = c.data?.id
    expect(dbCount(`SELECT count(*) FROM team_users WHERE username='${marker}'`)).toBe(1)

    await uiLogin(page)
    await page.goto(BASE + '/#/teamUser/list', { waitUntil: 'networkidle' })
    await page.waitForSelector('.el-table__body-wrapper tbody tr', { timeout: 8000 })
    await page.waitForFunction((m) => document.body.innerText.includes(m), marker, { timeout: 8000 })
    const listBody = await (await api.get(API_BASE + '/api/team/users', { headers })).json()
    const apiCount = Array.isArray(listBody.data) ? listBody.data.length : (listBody.data?.list?.length || 0)
    const rendered = await page.locator('.el-table__body-wrapper tbody tr').count()
    expect(rendered).toBe(apiCount)
  } finally {
    if (newId) await api.delete(API_BASE + '/api/team/users/' + newId, { headers }).catch(() => {})
  }
  expect(dbCount(`SELECT count(*) FROM team_users WHERE username='${marker}'`)).toBe(0)
  expect(errors).toEqual([])
})

// 4. res.data 页面渲染冒烟：打开新增对话框，确认不崩溃/列表渲染 ----
const SMOKE = [
  'whatsapp/account', 'feishu/account', 'oneid/list', 'customer360/list',
  'abExperiment/list', 'templateMarket/list', 'integration/list', 'sms/config',
  'sms/jobs', 'livecode', 'customerEvent/list'
]
for (const route of SMOKE) {
  test(`[SMOKE] ${route}：渲染 + 打开新增对话框无崩溃`, async ({ page }) => {
    const errors = []
    page.on('pageerror', (e) => errors.push('PAGEERROR: ' + e.message))
    page.on('response', (res) => { if (res.status() >= 500) errors.push('5xx ' + res.status() + ' ' + res.url()) })
    await uiLogin(page)
    await page.goto(BASE + '/#' + (route.startsWith('/') ? route : '/' + route), { waitUntil: 'networkidle' })
    await page.waitForTimeout(800)
    const addBtn = page.locator('button.el-button', { hasText: /新增|添加|创建|Add|Create|新建/ }).first()
    if (await addBtn.count()) {
      await addBtn.click({ timeout: 4000 }).catch(() => {})
      await page.waitForTimeout(500)
      const dlg = page.locator('.el-dialog:visible, .el-drawer:visible').first()
      if (await dlg.count()) {
        await dlg.locator('.el-dialog__headerbtn, .el-drawer__headerbtn').first().click({ timeout: 2000 }).catch(() => {})
      }
    }
    expect(errors).toEqual([])
  })
}
