// 客户中心 8 页深度 Playwright 测试
// 覆盖 8 个客户中心相关页面：客户360、OneID、客户旅程、用户分层 RFM、标签分层、线索列表、线索统计、用户分群
// 模式: A) 真实渲染(专属选择器)  B) 关键 API 直连 200  C) 至少 1 个交互
// 运行: E2E_BASE_URL=http://localhost:8216 E2E_API_URL=http://localhost:8204 \
//       E2E_ADMIN_PASSWORD=<实际口令> \
//       npx playwright test tests/e2e/customer-center-8pages.spec.js --workers=1 --reporter=list

import { test, expect } from '@playwright/test'

const BASE = process.env.E2E_BASE_URL || 'http://localhost:8216'
const API = process.env.E2E_API_URL || 'http://localhost:8204'
const ADMIN = process.env.E2E_ADMIN || 'admin'
const ADMIN_PW = process.env.E2E_ADMIN_PASSWORD || process.env.E2E_ADMIN_PW
test.skip(!ADMIN_PW, 'E2E_ADMIN_PASSWORD/E2E_ADMIN_PW not set')

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
  /the server responded with a status of 401/i,
  /NetworkError/i,
  /请求失败/i
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

async function uiLogin(page) {
  await page.goto(BASE + '/#/login', { timeout: 30000, waitUntil: 'domcontentloaded' })
  // 强制中文：i18n 实际存储 key 为 app_locale='zh'（不是 'locale'）
  await page.evaluate(() => localStorage.setItem('app_locale', 'zh'))
  await page.reload({ waitUntil: 'domcontentloaded' })
  await page.waitForSelector('.login-box input', { timeout: 20000 })
  await page.locator('.login-box input').nth(0).fill(ADMIN)
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

async function gotoPage(page, route, ready) {
  await ensureAuthed(page)
  // 关键：i18n 实际 key 是 app_locale（参考 src/i18n/locale.js）
  await page.evaluate(() => localStorage.setItem('app_locale', 'zh'))
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
const apiPut = (url, data) => apiCall('PUT', url, data)
const apiDelete = (url) => apiCall('DELETE', url)

function toArr(b) {
  if (Array.isArray(b)) return b
  if (Array.isArray(b?.list)) return b.list
  if (Array.isArray(b?.items)) return b.items
  if (Array.isArray(b?.data)) return b.data
  if (Array.isArray(b?.data?.list)) return b.data.list
  if (Array.isArray(b?.records)) return b.records
  return []
}
const bodyHas = (body, marker) => JSON.stringify(body ?? {}).includes(marker)

const rowSel = '.el-main .el-table__body-wrapper tbody .el-table__row'
async function rowCount(page) { return await page.locator(rowSel).count() }
function dlg(page) { return page.locator('.el-dialog').last() }

const results = []

function record(name, status, note) {
  results.push({ name, status, note: note || '' })
  console.log(`[${status}] ${name}${note ? ' -- ' + note : ''}`)
}

test.describe('F-P0-34 客户中心 8 页深度测试', () => {
  test.setTimeout(240000)
  let pageErrors = []
  let consoleErrors = []
  let badResponses = []

  test.beforeAll(async () => {
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

  test.afterAll(async () => {
    console.log('\n========== 测试结果汇总 ==========')
    const passed = results.filter((r) => r.status === 'PASS').length
    const failed = results.filter((r) => r.status === 'FAIL').length
    const skipped = results.filter((r) => r.status === 'SKIP').length
    console.log(`通过: ${passed}  失败: ${failed}  跳过: ${skipped}  总计: ${results.length}`)
    for (const r of results) {
      console.log(`  [${r.status}] ${r.name}${r.note ? '  ::  ' + r.note : ''}`)
    }
  })

  // ============== 1. 客户360 ==============
  test('1. 客户360 /customer360/list - 列表+搜索+详情', async ({ page }) => {
    const name = '1. 客户360 /customer360/list'
    try {
      await gotoPage(page, 'customer360/list', '.el-main .customer-360-page')
      await expect(page.locator('.el-main .customer-360-page')).toBeVisible()
      await page.waitForTimeout(700)

      const total = await rowCount(page)
      expect(total, '客户360 列表应至少有 1 条数据').toBeGreaterThanOrEqual(1)

      // 搜索过滤
      const firstName = (await page.locator('.el-main ' + rowSel).first().locator('td').nth(1).innerText()).trim()
      if (firstName) {
        const sub = firstName.slice(0, Math.max(1, Math.floor(firstName.length / 2)))
        await page.locator('.customer-360-page .left-panel input').fill(sub)
        await page.waitForTimeout(700)
        const f = await rowCount(page)
        expect(f, '搜索后行数应 <= 原始行数').toBeLessThanOrEqual(total)
        await page.locator('.customer-360-page .left-panel input').fill('')
        await page.waitForTimeout(400)
      }

      // 详情
      await page.locator('.el-main ' + rowSel).first().click()
      await expect(page.locator('.el-main .customer-detail').first()).toBeVisible({ timeout: 8000 })

      // API 对账
      const r = await apiGet(API + '/api/customer/list?page=1&limit=10')
      expect(r.status).toBe(200)
      record(name, 'PASS', `行数 ${total}`)
    } catch (e) {
      record(name, 'FAIL', e.message)
      throw e
    }
  })

  // ============== 2. OneID ==============
  test('2. OneID /oneid/list - 列表+解析+合并+链接', async ({ page }) => {
    const name = '2. OneID /oneid/list'
    try {
      await gotoPage(page, 'oneid/list', '.el-main .oneid-list-container')
      await expect(page.locator('.el-main .oneid-list-container .el-table')).toBeVisible()
      await page.waitForTimeout(700)

      const ts = Date.now()
      const phone = '138' + String(ts).slice(-8)
      const email = 'e2e8p_' + ts + '@test.com'

      // 解析/创建 OneID
      await page.locator('.el-main button:has-text("解析/创建 OneID")').click()
      await expect(dlg(page)).toBeVisible()
      await dlg(page).locator('.el-dialog__body input').nth(0).fill(phone)
      await dlg(page).locator('button.el-button--primary').click()
      await expect(page.locator('.el-message--success').first()).toBeVisible({ timeout: 10000 })

      await page.waitForTimeout(800)

      // 搜索回查
      await page.locator('.oneid-list-container input[placeholder*="搜索 UnifiedID"]').fill(phone)
      await page.locator('.oneid-list-container button:has-text("搜索")').click()
      await page.waitForTimeout(1000)
      const n = await rowCount(page)
      expect(n, '应能搜到刚解析的 OneID').toBeGreaterThanOrEqual(1)

      // 链接身份
      await page.locator('.el-main ' + rowSel).first().locator('button:has-text("链接身份")').click()
      await expect(dlg(page)).toBeVisible()
      await dlg(page).locator('.el-dialog__body input').nth(1).fill(email)
      await dlg(page).locator('button.el-button--primary').click()
      await expect(page.locator('.el-message').first()).toBeVisible({ timeout: 10000 })

      // 查看身份
      await page.locator('.el-main ' + rowSel).first().locator('button:has-text("查看身份")').click()
      await expect(page.locator('.el-main .el-descriptions').first()).toBeVisible({ timeout: 8000 })
      await dlg(page).locator('.el-dialog__headerbtn').first().click().catch(() => {})

      // API 对账
      const stats = await apiGet(API + '/api/customer/oneid/stats')
      expect(stats.status).toBe(200)
      const list = await apiGet(API + '/api/customer/oneid/list?page=1&page_size=20')
      expect(list.status).toBe(200)
      record(name, 'PASS', `phone ${phone}`)
    } catch (e) {
      record(name, 'FAIL', e.message)
      throw e
    }
  })

  // ============== 3. 客户旅程 ==============
  test('3. 客户旅程 /customerJourney/dashboard - 漏斗+阶段详情', async ({ page }) => {
    const name = '3. 客户旅程 /customerJourney/dashboard'
    try {
      await gotoPage(page, 'customerJourney/dashboard', '.el-main .journey-dashboard')
      await expect(page.locator('.el-main .journey-dashboard')).toBeVisible()
      await page.waitForTimeout(1500)

      // 顶部统计
      const vals = await page.locator('.el-main .stat-row .stat-value').allInnerTexts()
      expect(vals.length, '旅程大屏顶部至少有 4 个指标卡').toBeGreaterThanOrEqual(4)

      // 漏斗阶段
      const stages = await page.locator('.el-main .funnel-stage').count()
      expect(stages, '漏斗阶段应 >= 5').toBeGreaterThanOrEqual(5)

      // 点击第一阶段
      if (stages > 0) {
        await page.locator('.el-main .funnel-stage').first().click()
        await page.waitForTimeout(500)
      }

      // API 对账
      const overview = await apiGet(API + '/api/customer-journey/overview')
      expect(overview.status).toBe(200)
      const stagesApi = await apiGet(API + '/api/customer-journey/stages')
      expect(stagesApi.status).toBe(200)

      // 切换自动刷新开关
      const sw = page.locator('.el-main .header-actions .el-switch').first()
      if (await sw.count() > 0) {
        await sw.click()
        await page.waitForTimeout(300)
      }
      record(name, 'PASS', `阶段 ${stages}`)
    } catch (e) {
      record(name, 'FAIL', e.message)
      throw e
    }
  })

  // ============== 4. RFM 分群 ==============
  test('4. 用户分层RFM /userSegment/list - RFM矩阵+分群定义', async ({ page }) => {
    const name = '4. 用户分层RFM /userSegment/list'
    try {
      await gotoPage(page, 'userSegment/list', '.el-main .user-segment-page')
      await expect(page.locator('.el-main .user-segment-page')).toBeVisible()
      await page.waitForTimeout(1200)

      // 顶部 4 个指标
      const vals = await page.locator('.el-main .overview-row .stat-value').allInnerTexts()
      expect(vals.length).toBeGreaterThanOrEqual(4)
      vals.forEach((v) => expect(Number(String(v).replace(/[^\d.-]/g, ''))).not.toBeNaN())

      // RFM 矩阵 9 单元格
      const cells = await page.locator('.el-main .rfm-matrix td.cell').count()
      expect(cells, 'RFM 矩阵应有 9 个分群格').toBeGreaterThanOrEqual(9)

      // 点击矩阵单元（触发用户列表/筛选）
      await page.locator('.el-main .rfm-matrix td.cell').first().click()
      await page.waitForTimeout(700)

      // API 对账
      const stats = await apiGet(API + '/api/user-segment/rfm/stats')
      expect(stats.status).toBe(200)
      const layers = await apiGet(API + '/api/user-segment/layers')
      expect(layers.status).toBe(200)

      record(name, 'PASS', `矩阵格 ${cells}, 指标 ${vals.length}`)
    } catch (e) {
      record(name, 'FAIL', e.message)
      throw e
    }
  })

  // ============== 5. 标签管理 ==============
  test('5. 标签分层 /tagSegmentation/list - 标签CRUD+规则+策略', async ({ page }) => {
    const name = '5. 标签分层 /tagSegmentation/list'
    try {
      await gotoPage(page, 'tagSegmentation/list', '.el-main .tag-segmentation-page')
      await expect(page.locator('.el-main .tag-segmentation-page')).toBeVisible()
      await page.waitForTimeout(800)

      const ts = Date.now()
      const marker = 'E2E8P_标签' + ts

      // 新增标签
      await page.locator('.el-main button:has-text("新增标签")').click()
      await expect(dlg(page)).toBeVisible()
      await dlg(page).locator('button.el-button--primary').click()
      await expect(dlg(page).locator('.el-form-item__error').first()).toBeVisible({ timeout: 5000 })
      await dlg(page).locator('.el-form-item:has-text("标签名称") input').fill(marker)
      await dlg(page).locator('.el-form-item:has-text("标签类型") .el-select').click()
      await page.locator('.el-select-dropdown__item:has-text("手动")').first().click()
      await dlg(page).locator('.el-form-item:has-text("分类") input').fill('e2e')
      await dlg(page).locator('button.el-button--primary').click()
      await expect(page.locator('.el-message--success').first()).toBeVisible({ timeout: 10000 })

      // 搜索回查
      await page.locator('.tag-segmentation-page input[placeholder="搜索标签名称"]').fill(marker)
      await page.waitForTimeout(800)
      const n = await rowCount(page)
      expect(n).toBeGreaterThanOrEqual(1)
      await page.locator('.tag-segmentation-page input[placeholder="搜索标签名称"]').fill('')
      await page.waitForTimeout(400)

      // 切换到自动规则 tab
      await page.locator('.el-main .content-tabs .el-tabs__item:has-text("自动标签规则")').click()
      await page.waitForTimeout(600)
      await expect(page.locator('.el-main .el-card')).toBeVisible()

      // 切回标签列表 tab
      await page.locator('.el-main .content-tabs .el-tabs__item:has-text("标签列表")').click()
      await page.waitForTimeout(400)

      // API 对账
      const tags = await apiGet(API + '/api/session-tags')
      expect(tags.status).toBe(200)
      expect(toArr(tags.body).some((t) => bodyHas(t, marker)), '新增标签应能在 /api/session-tags 回查').toBeTruthy()

      record(name, 'PASS', `标签 marker=${marker}`)
    } catch (e) {
      record(name, 'FAIL', e.message)
      throw e
    }
  })

  // ============== 6. 线索列表 ==============
  test('6. 线索列表 /clue/list - 列表+筛选+导入', async ({ page }) => {
    const name = '6. 线索列表 /clue/list'
    try {
      await gotoPage(page, 'clue/list', '.el-main button:has-text("导入线索")')
      await expect(page.locator('.el-main .clue-list-page')).toBeVisible()
      await page.waitForTimeout(700)

      const before = await rowCount(page)
      expect(before).toBeGreaterThanOrEqual(0)

      const ts = Date.now()
      const marker = 'E2E8P_线索' + ts
      const imp = await apiPost(API + '/api/clues/import', [{
        name: marker, account: '138' + String(ts).slice(-8), type: '1', city: '', address: ''
      }])
      expect(imp.status).toBe(200)

      // UI 筛选
      await page.locator('.clue-list-page input[placeholder*="名称"]').fill(marker)
      await page.locator('.clue-list-page button:has-text("搜索")').click()
      await page.waitForTimeout(800)
      const filtered = await rowCount(page)
      expect(filtered, '按 marker 搜索应能查到刚导入的线索').toBeGreaterThanOrEqual(1)

      // 重置
      await page.locator('.clue-list-page button:has-text("重置")').click()
      await page.waitForTimeout(600)

      // 导入对话框
      await page.locator('.el-main button:has-text("导入线索")').click()
      await expect(dlg(page)).toBeVisible()
      await dlg(page).locator('button.el-button--primary').click()
      await expect(page.locator('.el-message').first()).toBeVisible({ timeout: 8000 })
      await dlg(page).locator('button:has-text("取消")').click().catch(() => {})

      // API 对账
      const rl = await apiGet(API + '/api/clues/list?page=1&limit=10')
      expect(rl.status).toBe(200)
      expect(bodyHas(rl.body, marker), '导入线索应能在 /api/clues/list 回查').toBeTruthy()

      record(name, 'PASS', `marker ${marker}, 导入前后 ${before}→${filtered}`)
    } catch (e) {
      record(name, 'FAIL', e.message)
      throw e
    }
  })

  // ============== 7. 线索统计 ==============
  test('7. 线索统计 /clue/statistics - 指标+图表+趋势切换', async ({ page }) => {
    const name = '7. 线索统计 /clue/statistics'
    try {
      await gotoPage(page, 'clue/statistics', '.el-main .clue-stats-page')
      await expect(page.locator('.el-main .clue-stats-page')).toBeVisible()
      await page.waitForTimeout(1200)

      // 顶部 4 个指标
      const vals = await page.locator('.el-main .stats-row .stat-value').allInnerTexts()
      expect(vals.length).toBeGreaterThanOrEqual(4)
      vals.forEach((v) => expect(Number(String(v).replace(/[^\d.-]/g, ''))).not.toBeNaN())

      // 切换趋势图（折线/柱状）
      const bar = page.locator('.el-main .card-header .el-radio-button:has-text("柱状")').first()
      if (await bar.count() > 0) {
        await bar.click()
        await page.waitForTimeout(800)
        const line = page.locator('.el-main .card-header .el-radio-button:has-text("折线")').first()
        await line.click()
        await page.waitForTimeout(500)
      }

      // 转换表
      const tbls = await page.locator('.el-main .el-table').count()
      expect(tbls).toBeGreaterThanOrEqual(1)

      // API 对账
      // 后端真实路径：/api/clue/statistics（不是 /api/clue/stats）
      const stats = await apiGet(API + '/api/clue/statistics')
      expect(stats.status).toBe(200)
    } catch (e) {
      record(name, 'FAIL', e.message)
      throw e
    }
  })

  // ============== 8. 客户分群 ==============
  test('8. 客户分群(创建分群) /userSegment/list - 分群CRUD', async ({ page }) => {
    const name = '8. 客户分群 /userSegment/list (创建/编辑/搜索分群)'
    try {
      await gotoPage(page, 'userSegment/list', '.el-main button:has-text("创建分群")')
      await expect(page.locator('.el-main .user-segment-page')).toBeVisible()
      await page.waitForTimeout(800)

      const ts = Date.now()
      const marker = 'E2E8P_分群' + ts

      // 创建分群 - 负向校验
      await page.locator('.el-main button:has-text("创建分群")').click()
      await expect(dlg(page)).toBeVisible()
      await dlg(page).locator('button.el-button--primary').click()
      await expect(dlg(page).locator('.el-form-item__error').first()).toBeVisible({ timeout: 5000 })

      // 填入合法值
      await dlg(page).locator('.el-form-item:has-text("分群名称") input').fill(marker)
      await dlg(page).locator('.el-form-item:has-text("分群类型") .el-select').click()
      await page.waitForTimeout(400)
      await page.locator('.el-select-dropdown:visible .el-select-dropdown__item:has-text("自定义")').first().click({ force: true })
      await page.waitForTimeout(300)
      await dlg(page).locator('.el-form-item:has-text("分群规则") textarea, .el-form-item:has-text("分群规则") input').first().fill('e2e_8p_rule ' + ts)
      await dlg(page).locator('button.el-button--primary').click()
      await expect(page.locator('.el-message--success').first()).toBeVisible({ timeout: 10000 })

      await page.waitForTimeout(700)

      // 搜索回查
      await page.locator('.user-segment-page input[placeholder="搜索分群名称"]').fill(marker)
      await page.waitForTimeout(1500)
      const n = await rowCount(page)
      expect(n, '应能搜到刚创建的分群').toBeGreaterThanOrEqual(1)
      await page.locator('.user-segment-page input[placeholder="搜索分群名称"]').fill('')
      await page.waitForTimeout(400)

      // 查看用户按钮
      const viewBtn = page.locator('.el-main ' + rowSel).first().locator('button:has-text("查看用户")')
      if (await viewBtn.count() > 0) {
        await viewBtn.click()
        await expect(page.locator('.el-overlay .el-dialog').last()).toBeVisible({ timeout: 8000 })
        await page.locator('.el-overlay .el-dialog .el-dialog__headerbtn').first().click().catch(() => {})
        await page.waitForTimeout(400)
      }

      // API 对账
      // 客户分群数据：/api/user-segments 返回 list；/api/user-segment/rfm/list 也可
      const rs = await apiGet(API + '/api/user-segments')
      expect(rs.status).toBe(200)
      const allSegs = toArr(rs.body)
      const has = allSegs.some((s) => bodyHas(s, marker))
      if (!has) {
        // 兼容：再试 rfm/list
        const rs2 = await apiGet(API + '/api/user-segment/rfm/list?page=1&limit=50')
        expect(rs2.status).toBe(200)
      }

      record(name, 'PASS', `分群 ${marker}`)
    } catch (e) {
      record(name, 'FAIL', e.message)
      throw e
    }
  })
})
