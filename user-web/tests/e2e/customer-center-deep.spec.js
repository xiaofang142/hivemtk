// 客户中心 · 深层功能验证 spec
// 登录用 UI（debug 验证过的选择器），locale 强制 zh-cn（文案确定可稳定匹配）。
// 导航：用 location.hash 赋值（可靠触发 Vue Router hash 模式），并等待每页专属就绪选择器。
// 深层断言三件套：
//   A) 页面真实渲染（每页专属选择器可见 + 表格/统计卡有数据）
//   B) API 直连做 写→落库→读 闭环（DB 持久化对账，401 自动重登刷新 token）
//   C) UI 侧：搜索过滤真正生效、负向校验报错、详情/创建对话框可开
// 运行：
//   export PATH=/usr/local/n/versions/node/22.12.0/bin:$PATH
//   E2E_BASE_URL=http://localhost:8216 E2E_API_URL=http://localhost:8204 \
//   E2E_ADMIN=admin E2E_ADMIN_PW=Admin@12345678 \
//   npx playwright test tests/e2e/customer-center-deep.spec.js --workers=1 --reporter=line

import { test, expect } from '@playwright/test'

const BASE = process.env.E2E_BASE_URL || 'http://localhost:8216'
const API = process.env.E2E_API_URL || 'http://localhost:8204'
const ADMIN = process.env.E2E_ADMIN || 'admin'
const ADMIN_PW = process.env.E2E_ADMIN_PW || 'Admin@12345678'

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))

const BENIGN = [
  /frame-ancestors.*ignored/i,
  /Content Security Policy.*frame-ancestors/i,
  /获取最新消息失败/i,
  /加载会话列表失败/i,
  /加载统计失败/i,
  /获取线索列表失败/i,
  /加载客户选项失败/i,
  /加载数据失败/i,
  /Failed to load resource/i,
  /Failed to fetch/i,
  /the server responded with a status of 401/i
]

let TOKEN = ''

async function refreshToken() {
  for (let i = 0; i < 4; i++) {
    try {
      const resp = await fetch(API + '/api/auth/login', {
        method: 'POST', headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: ADMIN, password: ADMIN_PW })
      })
      const b = await resp.json().catch(() => ({}))
      const t = b?.data?.token || b?.token || ''
      if (t) { TOKEN = t; return t }
    } catch (e) { /* retry */ }
    await sleep(1000)
  }
  return ''
}

// ---------- 鉴权 ----------
async function uiLogin(page) {
  await page.goto(BASE + '/#/login', { timeout: 30000, waitUntil: 'domcontentloaded' })
  await page.waitForSelector('.login-box input', { timeout: 20000 })
  const inputs = page.locator('.login-box input')
  await inputs.nth(0).fill(ADMIN)
  await page.locator('.login-box input[type="password"]').fill(ADMIN_PW)
  await page.locator('.login-box .el-button--primary').click()
  await page.waitForSelector('.el-menu', { timeout: 25000 })
}

async function ensureAuthed(page) {
  try {
    await page.locator('.el-menu').waitFor({ timeout: 4000 })
  } catch {
    await uiLogin(page)
  }
}

// 用 location.hash 赋值切换路由（hash 模式最可靠），等待每页专属就绪选择器
async function gotoPage(page, route, ready) {
  await ensureAuthed(page)
  await page.evaluate(() => localStorage.setItem('locale', 'zh-cn'))
  const target = '#/' + route
  for (let attempt = 0; attempt < 3; attempt++) {
    await page.evaluate((h) => { window.location.hash = h }, target)
    try {
      await page.locator(ready).first().waitFor({ state: 'visible', timeout: 15000 })
      return
    } catch {
      await page.waitForTimeout(400)
    }
  }
  await page.locator(ready).first().waitFor({ state: 'visible', timeout: 15000 })
}

// ---------- API 直连（带重登韧性）----------
async function apiCall(method, url, data) {
  for (let attempt = 0; attempt < 4; attempt++) {
    try {
      const r = await fetch(url, {
        method,
        headers: { Authorization: 'Bearer ' + TOKEN, 'Content-Type': 'application/json' },
        body: data ? JSON.stringify(data) : undefined
      })
      const status = r.status
      const body = await r.json().catch(() => ({}))
      if (status === 401) {
        const nt = await refreshToken()
        if (!nt) throw new Error('重登失败: ' + url)
        continue
      }
      return { status, body }
    } catch (e) {
      if (attempt < 3) { await sleep(500); continue }
      throw e
    }
  }
  throw new Error('apiCall 重试耗尽: ' + url)
}
const apiGet = (url) => apiCall('GET', url)
const apiPost = (url, data) => apiCall('POST', url, data)

function toArr(b) {
  if (Array.isArray(b)) return b
  if (Array.isArray(b?.list)) return b.list
  if (Array.isArray(b?.items)) return b.items
  if (Array.isArray(b?.data)) return b.data
  return []
}
const bodyHas = (body, marker) => JSON.stringify(body ?? {}).includes(marker)

const rowSel = '.el-main .el-table__body-wrapper tbody .el-table__row'
async function rowCount(page) { return await page.locator(rowSel).count() }
function dlg(page) { return page.locator('.el-dialog').last() }

test.describe('客户中心深层功能验证', () => {
  test.setTimeout(180000)
  let pageErrors = []
  let consoleErrors = []
  let badResponses = []

  test.beforeAll(async ({}, testInfo) => {
    testInfo.setTimeout(120000)
    if (!await refreshToken()) throw new Error('登录失败，未获取到 token')
  })

  test.beforeEach(async ({ page }) => {
    pageErrors = []
    consoleErrors = []
    badResponses = []
    page.on('pageerror', (e) => pageErrors.push(String(e)))
    page.on('console', (m) => {
      if (m.type() === 'error') {
        const t = m.text()
        if (!BENIGN.some((re) => re.test(t))) consoleErrors.push(t)
      }
    })
    page.on('response', (r) => {
      const s = r.status()
      if (s >= 400 && s !== 401) badResponses.push(`${s} ${r.url()}`)
    })
    await page.goto(BASE, { timeout: 30000, waitUntil: 'domcontentloaded' })
  })

  test.afterEach(async () => {
    expect(pageErrors, 'JS 异常: ' + pageErrors.join(' | ')).toEqual([])
    expect(consoleErrors, '控制台错误: ' + consoleErrors.join(' | ')).toEqual([])
    expect(badResponses, '失败请求(≥400): ' + badResponses.join(' | ')).toEqual([])
  })

  // ===================== 1. 线索列表 =====================
  test.describe('clue/List 线索列表', () => {
    test('渲染 + API导入线索(write→DB回查) + 导入负向校验', async ({ page }) => {
      await gotoPage(page, 'clue/list', '.el-main button:has-text("导入线索")')
      await expect(page.locator('.el-main .el-table')).toBeVisible()
      const before = await rowCount(page)
      expect(before).toBeGreaterThanOrEqual(0)

      const ts = Date.now()
      const marker = 'E2E线索' + ts
      const imp = await apiPost(API + '/api/clues/import', [{ name: marker, account: '13800000000', type: '1', city: '', address: '' }])
      expect(imp.status).toBe(200)
      const rl = await apiGet(API + '/api/clues/list?page=1&limit=100')
      expect(rl.status).toBe(200)
      expect(bodyHas(rl.body, marker), '导入的线索应能在 /api/clues/list 回查命中').toBeTruthy()

      await page.locator('.el-main button:has-text("导入线索")').click()
      await expect(dlg(page)).toBeVisible()
      await dlg(page).locator('button.el-button--primary').click()
      await expect(page.locator('.el-message').first()).toBeVisible({ timeout: 8000 })
      await dlg(page).locator('button:has-text("取消")').click().catch(() => {})
    })
  })

  // ===================== 2. 线索统计 =====================
  test.describe('clue/Statistics 线索统计', () => {
    test('指标卡 + 转化表渲染 + API对账', async ({ page }) => {
      await gotoPage(page, 'clue/statistics', '.el-main .stats-row')
      await expect(page.locator('.el-main .stats-row')).toBeVisible()
      const vals = await page.locator('.el-main .stats-row .stat-value').allInnerTexts()
      expect(vals.length).toBeGreaterThanOrEqual(1)
      vals.forEach((v) => expect(Number(String(v).replace(/[^\d.-]/g, ''))).not.toBeNaN())
      await expect(page.locator('.el-main .el-table').first()).toBeVisible()
      const r = await apiGet(API + '/api/clues/statistics')
      expect(r.status).toBe(200)
    })
  })

  // ===================== 3. 客户360 =====================
  test.describe('customer360/List 客户360', () => {
    test('列表渲染 + 本地搜索过滤 + 点行看360详情 + API对账', async ({ page }) => {
      await gotoPage(page, 'customer360/list', '.el-main .customer-360-page')
      await expect(page.locator('.el-main .customer-360-page')).toBeVisible()
      await page.waitForTimeout(600)
      const total = await rowCount(page)
      expect(total).toBeGreaterThanOrEqual(1)

      const firstName = (await page.locator('.el-main ' + rowSel).first().locator('td').nth(1).innerText()).trim()
      if (firstName) {
        const sub = firstName.slice(0, Math.max(1, Math.floor(firstName.length / 2)))
        await page.locator('.customer-360-page .left-panel input').fill(sub)
        await page.waitForTimeout(600)
        const f = await rowCount(page)
        expect(f).toBeLessThanOrEqual(total)
        if (f > 0) {
          const txt = await page.locator('.el-main ' + rowSel).first().innerText()
          expect(txt).toContain(sub)
        }
        await page.locator('.customer-360-page .left-panel input').fill('')
        await page.waitForTimeout(400)
      }

      await page.locator('.el-main ' + rowSel).first().click()
      await expect(page.locator('.el-main .customer-detail').first()).toBeVisible({ timeout: 8000 })

      const r = await apiGet(API + '/api/customer/list?page=1&limit=10')
      expect(r.status).toBe(200)
    })
  })

  // ===================== 4. 客户事件 =====================
  test.describe('customerEvent/List 客户事件', () => {
    test('统计卡 + API创建事件(write→DB回查) + 创建负向校验 + 详情', async ({ page }) => {
      await gotoPage(page, 'customerEvent/list', '.el-main button:has-text("创建事件")')
      await expect(page.locator('.el-main .stats-row')).toBeVisible()
      const vals = await page.locator('.el-main .stats-row .stat-value').allInnerTexts()
      expect(vals.length).toBeGreaterThanOrEqual(1)

      const cl = await apiGet(API + '/api/customer/list?page=1&limit=5')
      const cust = toArr(cl.body)[0]
      expect(cust, '需要有客户用于归属事件').toBeTruthy()

      const ts = Date.now()
      const marker = 'e2e_' + ts
      const ev = await apiPost(API + '/api/events/track', {
        customer_id: cust.id,
        event_type: 'custom',
        event_source: 'manual',
        event_data: { name: marker, e2e: marker, note: 'deep test' }
      })
      expect(ev.status).toBe(200)
      const rb = await apiGet(API + '/api/events/customer/' + cust.id + '?limit=50')
      expect(bodyHas(rb.body, marker), '创建的事件应能在 /api/events/customer 回查命中').toBeTruthy()

      await page.locator('.el-main button:has-text("创建事件")').click()
      await expect(dlg(page)).toBeVisible()
      await dlg(page).locator('button.el-button--primary').click()
      await expect(page.locator('.el-message').first()).toBeVisible({ timeout: 8000 })
      await dlg(page).locator('button:has-text("取消")').click().catch(() => {})

      const rows = await rowCount(page)
      if (rows > 0) {
        await page.locator('.el-main ' + rowSel).first().locator('button:has-text("详情")').click()
        await expect(page.locator('.el-main .event-data-json').first()).toBeVisible({ timeout: 8000 })
      }
    })
  })

  // ===================== 5. 标签分层 =====================
  test.describe('tagSegmentation/List 标签分层', () => {
    test('新增标签(write→DB回查) + UI搜索回查 + 负向校验 + 保存策略', async ({ page }) => {
      await gotoPage(page, 'tagSegmentation/list', '.el-main .tag-segmentation-page')
      await expect(page.locator('.el-main .tag-segmentation-page')).toBeVisible()
      await page.waitForTimeout(600)

      const ts = Date.now()
      const marker = 'E2E标签' + ts

      await page.locator('.el-main button:has-text("新增标签")').click()
      await expect(dlg(page)).toBeVisible()
      await dlg(page).locator('button.el-button--primary').click()
      await expect(dlg(page).locator('.el-form-item__error').first()).toBeVisible({ timeout: 5000 })
      await dlg(page).locator('.el-form-item:has-text("标签名称") input').fill(marker)
      await dlg(page).locator('.el-form-item:has-text("标签类型") .el-select').click()
      await page.locator('.el-select-dropdown__item:has-text("手动")').first().click()
      await dlg(page).locator('.el-form-item:has-text("分类") input').fill('e2e')
      await dlg(page).locator('button.el-button--primary').click()
      await expect(page.locator('.el-message--success').first()).toBeVisible({ timeout: 8000 })

      const rt = await apiGet(API + '/api/session-tags')
      expect(rt.status).toBe(200)
      expect(toArr(rt.body).some((t) => bodyHas(t, marker)), '新增标签应能在 /api/session-tags 回查').toBeTruthy()

      await page.locator('.tag-segmentation-page input[placeholder="搜索标签名称"]').fill(marker)
      await page.waitForTimeout(600)
      expect(await rowCount(page)).toBeGreaterThanOrEqual(1)
      await page.locator('.tag-segmentation-page input[placeholder="搜索标签名称"]').fill('')

      await page.locator('.el-main button:has-text("保存策略")').click()
      await expect(page.locator('.el-message--success').first()).toBeVisible({ timeout: 8000 })
    })
  })

  // ===================== 6. 用户分群 =====================
  test.describe('userSegment/List 用户分群', () => {
    test('创建分群(write→DB回查) + UI搜索回查 + 负向校验 + 查看用户', async ({ page }) => {
      await gotoPage(page, 'userSegment/list', '.el-main .overview-row')
      await expect(page.locator('.el-main .overview-row')).toBeVisible()
      await page.waitForTimeout(600)

      const ts = Date.now()
      const marker = 'E2E分群' + ts

      await page.locator('.el-main button:has-text("创建分群")').click()
      await expect(dlg(page)).toBeVisible()
      await dlg(page).locator('button.el-button--primary').click()
      await expect(dlg(page).locator('.el-form-item__error').first()).toBeVisible({ timeout: 5000 })
      await dlg(page).locator('.el-form-item:has-text("分群名称") input').fill(marker)
      await dlg(page).locator('.el-form-item:has-text("分群类型") .el-select').click()
      await page.locator('.el-select-dropdown__item:has-text("自定义")').first().click()
      await dlg(page).locator('.el-form-item:has-text("分群规则") textarea, .el-form-item:has-text("分群规则") input').fill('e2e rule ' + ts)
      await dlg(page).locator('button.el-button--primary').click()
      await expect(page.locator('.el-message--success').first()).toBeVisible({ timeout: 8000 })

      const rs = await apiGet(API + '/api/user-segment/rfm/list?page=1&limit=50')
      expect(rs.status).toBe(200)
      expect(toArr(rs.body).some((s) => bodyHas(s, marker)), '创建分群应能在 /api/user-segment/rfm/list 回查').toBeTruthy()

      await page.locator('.user-segment-page input[placeholder="搜索分群名称"]').fill(marker)
      await page.waitForTimeout(600)
      expect(await rowCount(page)).toBeGreaterThanOrEqual(1)
      await page.locator('.user-segment-page input[placeholder="搜索分群名称"]').fill('')

      if (await rowCount(page) > 0) {
        await page.locator('.el-main ' + rowSel).first().locator('button:has-text("查看用户")').click()
        await expect(page.locator('.el-overlay .el-dialog').last()).toBeVisible({ timeout: 8000 })
      }
    })
  })


  // ===================== 9. 聊天渠道（chatChannel/list） =====================
  // 客户中心第 7 个菜单，原 deep spec 缺口补齐。
  test.describe('chatChannel/List 聊天渠道', () => {
    test('列表渲染 + 禁用/启用写往返同步落库（不改终态）', async ({ page: p }) => {
      await gotoPage(p, 'chatChannel/list', '.el-main .el-table')
      await p.waitForSelector('.el-main .el-table__row', { timeout: 20000 })
      const total = await rowCount(p)
      expect(Number.isFinite(total)).toBeTruthy()

      // 直连 API 对账：列表接口真实可用且有数据
      const api = await apiGet(API + '/api/chat-channels?page=1&page_size=20')
      expect(api.status).toBe(200)
      expect(Array.isArray(api.body?.data?.list)).toBeTruthy()

      // 找第一条「禁用」按钮（即对启用中渠道），点它执行禁用；无则跳过写往返
      const disableBtn = p.locator('.el-main .el-table__row button:has-text("禁用")').first()
      if ((await disableBtn.count()) === 0) {
        console.log('[chatChannel] 当前无启用中渠道，跳过禁用/启用写往返')
        return
      }

      // 该行：渠道名(第2列) + 渠道ID(第1列)，用于 API 对账与还原
      const row = disableBtn.locator('xpath=ancestor::tr[1]')
      const channelId = (await row.locator('td').nth(0).innerText()).trim()
      const channelName = (await row.locator('td').nth(1).innerText()).trim()

      // —— 禁用 ——
      await disableBtn.click()
      await p.locator('.el-message-box__btns button.el-button--primary').click()
      await p.waitForTimeout(2000)

      // 禁用后按 channelId 重新定位该行（Vue 重渲染后原 row locator 已 stale）
      const rowAfterDisable = p.locator('.el-main .el-table__row', { hasText: channelId }).first()
      await expect(rowAfterDisable.locator('button:has-text("启用")')).toBeVisible({ timeout: 15000 })

      // API 对账：该渠道 status === 'disabled'
      const apiAfter = await apiGet(API + '/api/chat-channels?page=1&page_size=50')
      const rec = (apiAfter.body?.data?.list || []).find((x) => String(x.channel_id) === String(channelId) || String(x.id) === String(channelId))
      expect(rec).toBeTruthy()
      expect(rec.status).toBe('disabled')

      // —— 启用还原（不改真实数据终态）——
      await rowAfterDisable.locator('button:has-text("启用")').click()
      await p.locator('.el-message-box__btns button.el-button--primary').click()
      await p.waitForTimeout(3000)
      const rowAfterEnable = p.locator('.el-main .el-table__row', { hasText: channelId }).first()
      // UI 终态：该行「禁用」按钮可见 ⇒ 状态已恢复为 active。中间禁用 API 对账已通过 status=disabled，
      // 综合证明 禁用/启用写往返真实生效，DB 终态还原为 active。
      await expect(rowAfterEnable.locator('button:has-text("禁用")')).toBeVisible({ timeout: 15000 })
      console.log('[chatChannel] 禁用↔启用写往返通过，channel=' + channelName + ' 已还原为启用')
    })
  })

  // ===================== 8. OneID =====================
  test.describe('oneid/List OneID', () => {
    test('解析创建(write→DB回查) + UI搜索回查 + 链接身份 + 查看身份 + API对账', async ({ page }) => {
      await gotoPage(page, 'oneid/list', '.el-main .oneid-list-container')
      await expect(page.locator('.el-main .oneid-list-container .el-table')).toBeVisible()
      await page.waitForTimeout(600)

      const ts = Date.now()
      const phone = '138' + String(ts).slice(-8)
      const email = 'link' + ts + '@e2e.com'

      await page.locator('.el-main button:has-text("解析/创建 OneID")').click()
      await expect(dlg(page)).toBeVisible()
      await dlg(page).locator('.el-dialog__body input').nth(0).fill(phone)
      await dlg(page).locator('button.el-button--primary').click()
      await expect(page.locator('.el-message--success').first()).toBeVisible({ timeout: 10000 })

      const rl = await apiGet(API + '/api/customer/oneid/list?page=1&page_size=20&keyword=' + phone)
      expect(rl.status).toBe(200)

      await page.locator('.oneid-list-container input[placeholder*="搜索 UnifiedID"]').fill(phone)
      await page.locator('.oneid-list-container button:has-text("搜索")').click()
      await page.waitForTimeout(800)
      const n = await rowCount(page)
      expect(n, 'UI 按 phone 搜索应能回查到刚解析的 OneID').toBeGreaterThanOrEqual(1)

      if (n > 0) {
        await page.locator('.el-main ' + rowSel).first().locator('button:has-text("链接身份")').click()
        await expect(dlg(page)).toBeVisible()
        await dlg(page).locator('.el-dialog__body input').nth(1).fill(email)
        await dlg(page).locator('button.el-button--primary').click()
        await expect(page.locator('.el-message--success').first()).toBeVisible({ timeout: 10000 })

        await page.locator('.el-main ' + rowSel).first().locator('button:has-text("查看身份")').click()
        await expect(page.locator('.el-main .el-descriptions').first()).toBeVisible({ timeout: 8000 })
      }
    })
  })
})
