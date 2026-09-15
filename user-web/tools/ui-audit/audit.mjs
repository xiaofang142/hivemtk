// user-web UI 审计工具
// 用法: node audit.mjs <inventory|audit> <all|模块key|模块中文名> [baseURL]
import { chromium } from 'playwright'
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { MODULES, ALL_ROUTES } from './routes.mjs'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const BASE = process.argv[4] || 'http://127.0.0.1:8211'
const MODE = process.argv[2] || 'audit'
const SCOPE = process.argv[3] || 'all'

// 主内容区（排除侧边栏/顶栏，聚焦页面本体）
async function contentRoot(page) {
  for (const sel of ['.app-main', '.main-container', 'main', '#app']) {
    const r = page.locator(sel).first()
    if (await r.count()) return r
  }
  return page.locator('body')
}

// 可交互元素选择器（点击流）
const CLICK_SELECTOR =
  'button, a, [role="button"], .el-switch, .el-radio, .el-checkbox, .el-tabs__item, .el-select, .el-dropdown'
// 全部交互元素（含表单输入，用于清单）
const ALL_SELECTOR =
  'button, a, [role="button"], input, select, textarea, .el-switch, .el-radio, .el-checkbox, .el-tabs__item, .el-select, .el-dropdown, .el-upload'

const SKIP_TEXT = ['退出', '注销', '登出', 'logout', 'sign out']

function classify(el) {
  const tag = (el.tag || '').toLowerCase()
  const cls = el.cls || ''
  if (tag === 'button') return 'button'
  if (tag === 'a') return 'link'
  if (tag === 'input') {
    const t = (el.type || '').toLowerCase()
    if (t === 'checkbox') return 'checkbox'
    if (t === 'radio') return 'radio'
    if (t === 'submit') return 'submit'
    if (t === 'file') return 'file'
    return 'input'
  }
  if (tag === 'select') return 'select'
  if (tag === 'textarea') return 'textarea'
  if (cls.includes('el-switch')) return 'switch'
  if (cls.includes('el-radio')) return 'radio'
  if (cls.includes('el-checkbox')) return 'checkbox'
  if (cls.includes('el-tabs__item')) return 'tab'
  if (cls.includes('el-select')) return 'select-ui'
  if (cls.includes('el-dropdown')) return 'dropdown'
  if (cls.includes('el-upload')) return 'upload'
  if (el.role === 'button') return 'button'
  return 'other'
}

// 提取 root 下所有匹配 selector 的交互元素（含后代）
async function extract(page, selector) {
  const root = await contentRoot(page)
  return await root.evaluateAll(
    (els, sel) => {
      const out = []
      for (const el of els) {
        for (const child of el.querySelectorAll(sel)) {
          const cls = (child.className && child.className.toString()) || ''
          const text =
            (child.innerText || child.textContent || child.getAttribute('aria-label') || child.value || child.placeholder || '')
              .replace(/\s+/g, ' ')
              .trim()
              .slice(0, 80)
          const rect = child.getBoundingClientRect()
          const visible = rect.width > 0 && rect.height > 0
          if (!visible) continue
          out.push({
            tag: child.tagName,
            type: child.type || '',
            role: child.getAttribute('aria-label') || child.getAttribute('role') || '',
            cls,
            text,
            disabled:
              child.disabled ||
              /is-disabled/.test(cls) ||
              child.getAttribute('aria-disabled') === 'true',
          })
        }
      }
      return out
    },
    selector
  )
}

async function login(page) {
  await page.goto(BASE + '/', { waitUntil: 'domcontentloaded' })
  await page.waitForTimeout(800)
  const hasToken = await page.evaluate(() => !!localStorage.getItem('token'))
  if (hasToken) return
  if (!/login/.test(page.url())) {
    await page.goto(BASE + '/login', { waitUntil: 'domcontentloaded' })
    await page.waitForTimeout(500)
  }
  const inputs = page.locator('.login-box input.el-input__inner')
  if (await inputs.count()) {
    await inputs.nth(0).fill('admin')
    await inputs.nth(1).fill('Seed@123456')
    await page.click('.login-box button.el-button--primary')
    await page.waitForFunction(() => !!localStorage.getItem('token'), { timeout: 20000 })
  }
}

function installListeners(page, sink) {
  page.on('console', (m) => {
    if (m.type() === 'error') sink.console.push(m.text().slice(0, 400))
  })
  page.on('pageerror', (e) => sink.pageerror.push((e.message || String(e)).slice(0, 400)))
  page.on('response', (r) => {
    if (r.status() >= 400) sink.api.push({ url: r.url().slice(0, 200), status: r.status() })
  })
  page.on('requestfailed', (r) =>
    sink.api.push({ url: r.url().slice(0, 200), status: 'FAILED', err: r.failure()?.errorText || '' })
  )
  page.on('dialog', async (d) => {
    try {
      await d.dismiss()
    } catch { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
  })
}

async function settleDialogs(page) {
  await page.waitForTimeout(450)
  // ElMessageBox 确认框 → 点取消中止，避免破坏性提交
  const mb = page.locator('.el-message-box__wrapper:not(.el-message-box-fade-leave)').first()
  if (await mb.count()) {
    const cancel = mb.locator('.el-message-box__btns .el-button--default').first()
    if (await cancel.count()) await cancel.click().catch(() => {})
    await page.waitForTimeout(250)
  }
  // el-dialog 弹窗 → 关闭
  const dlg = page.locator('.el-dialog__wrapper:not(.el-dialog-fade-leave), .el-overlay-dialog').first()
  if (await dlg.count()) {
    const close = dlg.locator('.el-dialog__headerbtn').first()
    if (await close.count()) await close.click().catch(() => {})
    await page.waitForTimeout(250)
  }
}

async function gotoRoute(page, route) {
  await page.goto(BASE + '/#' + route, { waitUntil: 'domcontentloaded' }).catch(() => {})
  // 等待主内容区渲染出元素（避免长轮询页面 networkidle 挂起）
  await page
    .waitForFunction(
      () => {
        const m = document.querySelector('.app-main')
        return m && m.querySelectorAll('*').length > 5
      },
      { timeout: 8000 }
    )
    .catch(() => {})
  await page.waitForTimeout(900)
}

function isSkipped(text, cls) {
  if (SKIP_TEXT.some((s) => (text || '').includes(s))) return true
  if (/el-upload/.test(cls || '')) return true
  return false
}

async function runInventory(scopeRoutes) {
  const browser = await chromium.launch({ headless: true })
  const page = await browser.newPage()
  const sink = { console: [], pageerror: [], api: [] }
  installListeners(page, sink)
  await login(page)
  const out = {}
  for (const { module, key, route } of scopeRoutes) {
    out[key] = out[key] || { module, routes: {} }
    await gotoRoute(page, route)
    const all = await extract(page, ALL_SELECTOR)
    const items = all.map((e) => ({
      type: classify(e),
      tag: e.tag.toLowerCase(),
      text: e.text,
      disabled: e.disabled,
    }))
    out[key].routes[route] = { count: items.length, items }
    console.log(`[inventory] ${module} ${route} -> ${items.length} 交互元素`)
  }
  await browser.close()
  const jsonDir = path.join(__dirname, '..', '..', 'docs', 'ui-inventory')
  fs.mkdirSync(jsonDir, { recursive: true })
  fs.writeFileSync(path.join(jsonDir, 'inventory.json'), JSON.stringify(out, null, 2, 'utf-8'))
  for (const [key, mod] of Object.entries(out)) {
    const lines = [`# ${mod.module} (${key}) — UI 交互清单`, '']
    for (const [route, data] of Object.entries(mod.routes)) {
      lines.push(`## 路由 ${route}  （${data.count} 个交互元素）`, '')
      const grouped = {}
      for (const it of data.items) {
        grouped[it.type] = grouped[it.type] || []
        grouped[it.type].push(it)
      }
      for (const [g, arr] of Object.entries(grouped)) {
        lines.push(`### ${g} (${arr.length})`)
        for (const it of arr) lines.push(`- [ ] ${it.text || '(无文本)'}${it.disabled ? '  `disabled`' : ''}`)
        lines.push('')
      }
    }
    fs.writeFileSync(path.join(jsonDir, `${key}.md`), lines.join('\n'), 'utf-8')
  }
  console.log('[inventory] 完成，已写出 docs/ui-inventory/')
}

async function runAudit(scopeRoutes) {
  const browser = await chromium.launch({ headless: true })
  const page = await browser.newPage()
  const sink = { console: [], pageerror: [], api: [] }
  installListeners(page, sink)
  await login(page)

  const reportDir = path.join(__dirname, '..', '..', 'tests', 'e2e', 'audit')
  fs.mkdirSync(reportDir, { recursive: true })
  const summary = []

  for (const { module, key, route } of scopeRoutes) {
    const modDir = path.join(reportDir, key)
    fs.mkdirSync(modDir, { recursive: true })
    const pageReport = { route, module, errors: [], items: [] }

    // ---- 点击流：逐元素点击（每次重置） ----
    await gotoRoute(page, route)
    const root0 = await contentRoot(page)
    const n0 = await root0.locator(CLICK_SELECTOR).count()
    // 预计算去重后的点击目标（避免对每元素都重载）
    const seen = new Set()
    const targets = []
    for (let i = 0; i < n0; i++) {
      const item = root0.locator(CLICK_SELECTOR).nth(i)
      const disabled = await item
        .evaluate((el) => el.disabled || el.classList.contains('is-disabled') || el.getAttribute('aria-disabled') === 'true')
        .catch(() => false)
      if (disabled) continue
      const text = (await item.innerText().catch(() => '')).replace(/\s+/g, ' ').trim()
      const cls = await item.getAttribute('class').catch(() => '')
      if (isSkipped(text, cls)) continue
      const tag = (await item.evaluate((el) => el.tagName).catch(() => 'div'))
      const dtype = classify({ tag, cls })
      const key = text + '|' + dtype
      if (seen.has(key)) continue // 同页重复行按钮只点首个代表
      seen.add(key)
      targets.push({ i, text, dtype })
    }
    // 逐个点击（仅去重目标，每次重置干净状态）
    for (const t of targets) {
      await gotoRoute(page, route)
      const root = await contentRoot(page)
      const item = root.locator(CLICK_SELECTOR).nth(t.i)
      const before = { c: sink.console.length, p: sink.pageerror.length, a: sink.api.length }
      let clicked = true
      try {
        await item.click({ timeout: 4000 }).catch(() => {})
        await settleDialogs(page)
      } catch (err) {
        clicked = false
        pageReport.errors.push({ item: t.text || '(无文本)', error: String(err).slice(0, 200) })
      }
      pageReport.items.push({
        text: t.text || '(无文本)',
        type: t.dtype,
        action: 'click',
        clicked,
        consoleErrors: sink.console.slice(before.c),
        pageErrors: sink.pageerror.slice(before.p),
        apiErrors: sink.api.slice(before.a),
      })
    }

    // ---- 表单流：填充文本输入 + 点击搜索/提交 ----
    await gotoRoute(page, route)
    try {
      const root = await contentRoot(page)
      const inputs = root.locator('input.el-input__inner:not([type=password]), textarea')
      const ic = await inputs.count()
      for (let k = 0; k < ic; k++) await inputs.nth(k).fill('测试' + k).catch(() => {})
      const submit = root
        .locator('button:has-text("搜索"), button:has-text("查询"), button:has-text("筛选"), button:has-text("提交")')
        .first()
      if (await submit.count()) {
        const before = { c: sink.console.length, p: sink.pageerror.length, a: sink.api.length }
        await submit.click().catch(() => {})
        await settleDialogs(page)
        pageReport.items.push({
          text: '(表单) 填充输入+点击搜索/筛选',
          type: 'form',
          action: 'fill+submit',
          consoleErrors: sink.console.slice(before.c),
          pageErrors: sink.pageerror.slice(before.p),
          apiErrors: sink.api.slice(before.a),
        })
      }
    } catch (err) {
      pageReport.errors.push({ item: '(form)', error: String(err).slice(0, 200) })
    }

    const errCount =
      pageReport.items.reduce(
        (s, it) => s + it.consoleErrors.length + it.pageErrors.length + it.apiErrors.length,
        0
      ) + pageReport.errors.length
    pageReport.errorCount = errCount
    fs.writeFileSync(path.join(modDir, sanitize(route) + '.json'), JSON.stringify(pageReport, null, 2, 'utf-8'))
    summary.push({ module, key, route, interactions: pageReport.items.length, errorCount: errCount })
    console.log(`[audit] ${module} ${route} -> 交互 ${pageReport.items.length}, 异常 ${errCount}`)
  }
  await browser.close()
  fs.writeFileSync(path.join(reportDir, 'summary.json'), JSON.stringify(summary, null, 2, 'utf-8'))
  console.log('[audit] 完成，汇总 tests/e2e/audit/summary.json')
}

function sanitize(route) {
  return route.replace(/[^a-zA-Z0-9]/g, '_')
}

function resolveRoutes() {
  if (SCOPE === 'all') return ALL_ROUTES
  const matched = ALL_ROUTES.filter(
    (r) => r.key === SCOPE || r.module === SCOPE || r.key.includes(SCOPE) || r.module.includes(SCOPE)
  )
  return matched.length ? matched : ALL_ROUTES
}

const routes = resolveRoutes()
console.log(`模式=${MODE} scope=${SCOPE} 路由数=${routes.length}`)
if (MODE === 'inventory') await runInventory(routes)
else await runAudit(routes)
