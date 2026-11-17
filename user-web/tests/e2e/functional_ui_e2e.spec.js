/**
 * 功能写操作 UI E2E（深入一层：路由遍历只覆盖渲染、API 探针只覆盖接口层，
 * 这里覆盖「UI 表单 → 提交 → 落库 → 列表回显」完整闭环，并在用例内直查 DB 对比）。
 */
import { test, expect, request } from '@playwright/test'
import fs from 'fs'
import { execSync } from 'child_process'

const BASE = process.env.E2E_BASE_URL || 'http://localhost:8211'
const API_BASE = process.env.E2E_API_URL || 'http://localhost:8204'
const ADMIN_USER = process.env.E2E_USER || 'admin'
const ADMIN_PASS = process.env.E2E_ADMIN_PASSWORD || process.env.E2E_PASS
test.skip(!ADMIN_PASS, 'E2E_ADMIN_PASSWORD/E2E_PASS not set')
const PG_PW = fs.existsSync('/tmp/pg_pw.txt') ? fs.readFileSync('/tmp/pg_pw.txt', 'utf8').trim() : ''

function dbCount(sql) {
  if (!PG_PW) return -1
  const out = execSync(
    `PGPASSWORD='${PG_PW}' psql -h 127.0.0.1 -p 8232 -U admin -d user_db -t -A -c "${sql}"`,
    { encoding: 'utf8' }
  )
  return parseInt((out || '0').trim(), 10)
}

async function uiLogin(page) {
  await page.goto(BASE + '/#/login', { waitUntil: 'networkidle' })
  await page.fill('input[type="text"]', ADMIN_USER)
  await page.fill('input[type="password"]', ADMIN_PASS)
  await page.click('button[type="button"].el-button--primary')
  await page.waitForURL('**/#/**', { timeout: 15000 }).catch(() => {})
  await page.waitForTimeout(1200)
}

async function getAdminToken() {
  const ctx = await request.newContext({ baseURL: API_BASE })
  const resp = await ctx.post('/api/auth/login', {
    data: { username: ADMIN_USER, password: ADMIN_PASS }
  })
  const body = await resp.json()
  return { ctx, token: body.data?.token }
}

test('[FUNC-UI] 标签分段：真实表单创建 → 列表回显 → DB 落库', async ({ page }) => {
  const errors = []
  page.on('console', (m) => { if (m.type() === 'error') errors.push('CONSOLE: ' + m.text()) })
  page.on('pageerror', (e) => errors.push('PAGEERROR: ' + e.message))

  const marker = 'E2E_UI_TAG_' + Date.now()
  await uiLogin(page)
  await page.goto(BASE + '/#/tagSegmentation/list', { waitUntil: 'networkidle' })
  await page.waitForTimeout(600)

  // 打开新增对话框（注意：i18n 将按钮文案翻成英文 "Add a Label"，占位符仍为中文）
  await page.getByRole('button', { name: 'Add a Label' }).click()
  await page.waitForSelector('.el-dialog', { timeout: 5000 })
  await page.getByPlaceholder('请输入标签名称').fill(marker)
  await page.getByPlaceholder('如 价值/行为/偏好').fill('价值')
  await page.locator('.el-dialog__footer .el-button--primary').click()

  // 断言：列表中出现新标签（UI 回显）
  await expect(page.getByText(marker, { exact: false }).first()).toBeVisible({ timeout: 8000 })

  // 断言：DB 落库
  const n = dbCount(`SELECT count(*) FROM session_tags WHERE name='${marker}'`)
  expect(n).toBe(1)

  expect(errors).toEqual([])
})

test('[FUNC-UI] 标签分段：API 创建 → 列表视图回显（写→读闭环）', async ({ page }) => {
  const errors = []
  page.on('console', (m) => { if (m.type() === 'error') errors.push('CONSOLE: ' + m.text()) })
  page.on('pageerror', (e) => errors.push('PAGEERROR: ' + e.message))

  const marker = 'E2E_API_TAG_' + Date.now()
  const { ctx, token } = await getAdminToken()
  const create = await ctx.post('/api/session-tags', {
    headers: { Authorization: 'Bearer ' + token },
    data: { name: marker, code: 'tag_' + marker.toLowerCase(), group: '行为', description: 'e2e' }
  })
  const cbody = await create.json()
  expect(cbody.code).toBe('SUCCESS')
  await ctx.dispose()

  await uiLogin(page)
  await page.goto(BASE + '/#/tagSegmentation/list', { waitUntil: 'networkidle' })
  // 用搜索框过滤（.filter-bar 首个 input），确保新行可见
  const search = page.locator('.filter-bar input').first()
  if (await search.count()) {
    await search.fill(marker)
    await page.waitForTimeout(1000)
  }
  await expect(page.getByText(marker, { exact: false }).first()).toBeVisible({ timeout: 8000 })

  const n = dbCount(`SELECT count(*) FROM session_tags WHERE name='${marker}'`)
  expect(n).toBe(1)

  expect(errors).toEqual([])
})

test('[FUNC-UI] 话术模板：真实表单创建 → DB 落库（回归 category 缺失缺陷）', async ({ page }) => {
  const errors = []
  page.on('console', (m) => { if (m.type() === 'error') errors.push('CONSOLE: ' + m.text()) })
  page.on('pageerror', (e) => errors.push('PAGEERROR: ' + e.message))

  const marker = 'E2E_SCRIPT_' + Date.now()
  await uiLogin(page)
  await page.goto(BASE + '/#/scriptTemplate/list', { waitUntil: 'networkidle' })
  await page.waitForTimeout(800)

  // 打开新增对话框（页面首个 primary 按钮 = 新增话术）
  await page.locator('button.el-button--primary').first().click()
  await page.waitForSelector('.el-dialog', { timeout: 5000 })

  // 标题
  await page.locator('.el-dialog .el-form-item').filter({ hasText: '话术标题' }).locator('input').fill(marker)
  // 分类（allow-create：输入并回车新建）
  const catInput = page.locator('.el-dialog .el-form-item').filter({ hasText: '话术分类' }).locator('input')
  await catInput.click()
  await catInput.fill('通用')
  await catInput.press('Enter')
  // 内容
  await page.locator('.el-dialog textarea').first().fill('E2E content ' + marker)

  await page.locator('.el-dialog__footer .el-button--primary').click()

  // 断言：列表回显 + DB 落库（若无 category，后端会拒绝，这里应成功）
  await expect(page.getByText(marker).first()).toBeVisible({ timeout: 8000 })
  const n = dbCount(`SELECT count(*) FROM script_templates WHERE title='${marker}'`)
  expect(n).toBe(1)

  expect(errors).toEqual([])
})

test('[FUNC-UI] 集成列表：API 造数 → 列表渲染条数闭环（回归 res.data 误用缺陷）', async ({ page }) => {
  const errors = []
  page.on('console', (m) => { if (m.type() === 'error') errors.push('CONSOLE: ' + m.text()) })
  page.on('pageerror', (e) => errors.push('PAGEERROR: ' + e.message))

  const marker = 'E2E_INT_' + Date.now()
  const { ctx, token } = await getAdminToken()
  const headers = { Authorization: 'Bearer ' + token }

  // 造数前列表条数
  const beforeBody = await (await ctx.get('/api/integrations', { headers })).json()
  const beforePayload = beforeBody.data
  const before = Array.isArray(beforePayload) ? beforePayload.length : (beforePayload?.list || beforePayload?.data || []).length

  // 造一条集成（后端仅需 platform，account_name 用于展示名）
  const cbody = await (await ctx.post('/api/integrations', {
    headers,
    data: { platform: 'e2e', account_name: marker, api_key: 'x', api_secret: 'y' }
  })).json()
  expect(cbody.code).toBe('SUCCESS')
  const newId = cbody.data?.id

  // 造数后列表条数
  const afterBody = await (await ctx.get('/api/integrations', { headers })).json()
  const afterPayload = afterBody.data
  const after = Array.isArray(afterPayload) ? afterPayload.length : (afterPayload?.list || afterPayload?.data || []).length
  expect(after).toBe(before + 1)

  // 进列表页，断言表格渲染条数 === 接口返回条数（证明 toList(res) 已解包，列表不再恒空）
  await uiLogin(page)
  await page.goto(BASE + '/#/integration/list', { waitUntil: 'networkidle' })
  await page.waitForSelector('.el-table__body-wrapper tbody tr', { timeout: 8000 })
  const rendered = await page.locator('.el-table__body-wrapper tbody tr').count()
  expect(rendered).toBe(after)

  // 清理
  if (newId) {
    await ctx.delete('/api/integrations/' + newId, { headers }).catch(() => {})
  }
  await ctx.dispose()

  expect(errors).toEqual([])
})
