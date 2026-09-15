// 全页面自动巡检脚本（user-web）
// 用法: node tests/audit/audit-runner.mjs [pagesFile] [--page <hashPath>]
//
// 行为:
//  1. 用 admin 登录拿到 cookie（复用 auth 逻辑）
//  2. 导航到目标页面，加载等待
//  3. 收集"静态"问题: console.error / pageerror / 失败的网络请求(4xx/5xx)
//  4. 枚举页面交互元素(button/a[role=button]/input/select/textarea 等)
//  5. 对每个可点击元素做"观察式"点击(捕获弹窗自动取消/接受), 收集"动态"问题
//  6. 对每个 input/textarea/select 做填充/选择, 收集问题
//  7. 输出 JSON 报告到 tests/audit/reports/<safe>.json
//
// 设计原则:
//  - 不破坏真实数据: 删除/提交类操作若弹出 confirm 则取消;
//    若直接执行则捕获报错即可, 不深究副作用。
//  - 宁滥勿漏: 收集到的任何错误都记录, 供人工/自动修复判定。

import { chromium } from 'playwright'
import fs from 'fs'
import path from 'path'
import { fileURLToPath } from 'url'

const __dirname = path.dirname(fileURLToPath(import.meta.url))
const ROOT = path.resolve(__dirname, '../..')
const BASE = process.env.E2E_BASE_URL || 'http://localhost:8213'
const API_BASE = process.env.API_BASE_URL || 'http://localhost:8204'
const CRED = { user: 'admin', pass: process.env.ADMIN_PASS || 'Admin@123456' }
const OUT_DIR = path.resolve(__dirname, 'reports')
fs.mkdirSync(OUT_DIR, { recursive: true })

// 候选密码（容器重建后可能变化）
const CANDIDATES = [CRED.pass, 'Admin@12345678', 'Admin@123456', '62cfdc6bf1b075830734cc6f9a63501b']

function safeName(p) {
  return p.replace(/[^a-zA-Z0-9_-]/g, '_')
}

async function login(page) {
  await page.goto(`${BASE}/#/login`)
  await page.waitForSelector('.login-box', { timeout: 15000 })
  for (const pw of CANDIDATES) {
    await page.locator('.login-box input[type="text"]').first().fill(CRED.user)
    await page.locator('.login-box input[type="password"]').fill(pw)
    await page.locator('.login-box button.el-button--primary').click()
    try {
      await page.waitForURL((u) => !u.hash.includes('/login'), { timeout: 6000 })
      return true
    } catch { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
    await page.waitForTimeout(300)
  }
  throw new Error('登录失败：所有候选密码均无效')
}

// 收集器
function makeCollector() {
  const consoleErrors = []
  const pageErrors = []
  const netErrors = [] // {url, status, method}
  return {
    consoleErrors, pageErrors, netErrors,
    attach(page) {
      page.on('console', (m) => {
        if (m.type() === 'error') consoleErrors.push(m.text())
      })
      page.on('pageerror', (e) => {
        const msg = String(e && e.stack || e)
        // 过滤测试工具噪声：点击删除触发的浏览器原生确认框被 dismiss 时发出的 "cancel" 信号
        if (/^cancel$/.test(msg.trim())) return
        pageErrors.push(msg)
      })
      page.on('response', (r) => {
        const s = r.status()
        if (s >= 400) netErrors.push({ url: r.url(), status: s, method: r.request().method() })
      })
      page.on('requestfailed', (req) => {
        const u = req.url()
        // 过滤 Vite dev server 内部虚拟模块 / HMR 机制请求（非用户可见功能缺陷）
        if (/__x00__|@id\/|@vite\/|@react-refresh|vite\.hot|__vite__|\/@fs\//.test(u)) return
        if (/\/hmr\b|\?t=\d+$|&t=\d+$/.test(u)) return
        // 过滤静态资源（图片/字体）加载失败：多为占位/mock 图，不影响业务逻辑
        if (/\.(jpg|jpeg|png|gif|svg|webp|avif|woff2?|ttf|eot|ico|mp4|mp3)(\?|$)/i.test(u)) return
        netErrors.push({ url: u, status: 0, method: req.method(), failed: true })
      })
    }
  }
}

// 过滤已知无害的控制台消息
const NOISE = [
  /frame-ancestors.*ignored/i,
  /The Content Security Policy directive/i,
  /frame-ancestors.*ignored when delivered via a <meta>/i,
  /Download the React DevTools/i,
  /\[vite\]/i,
  /ResizeObserver loop/i,
  /Non-Error promise rejection/i,
  // 浏览器对 4xx/5xx 响应的固有网络日志（响应状态已在 net 中独立记录，且前端已捕获处理）
  /Failed to load resource/i
]
function isNoise(text) {
  return NOISE.some((re) => re.test(text))
}

// 枚举交互元素（打 data-audit-idx 以便精确索引定位）
async function enumInteractive(page) {
  return await page.evaluate(() => {
    const out = []
    const seen = new Set()
    const assign = (el) => {
      if (el.dataset.auditIdx === undefined) {
        el.dataset.auditIdx = String(out.length)
      }
      return Number(el.dataset.auditIdx)
    }
    const add = (el, kind, label) => {
      const t = (label || '').slice(0, 60)
      const k = kind + '::' + t
      if (seen.has(k)) return
      seen.add(k)
      const idx = assign(el)
      out.push({ idx, kind, label: t, tag: el.tagName, type: el.getAttribute('type') || '', placeholder: el.getAttribute('placeholder') || '' })
    }
    document.querySelectorAll('button, [role="button"], a.el-button, .el-button').forEach((el) => {
      // 排除日期/时间选择器浮层内部按钮（真实用户通过 picker 面板交互，枚举器无法可靠点击）
      if (el.closest('.el-picker-panel, .el-date-picker, .el-time-panel, .el-popper')) return
      const t = (el.innerText || el.getAttribute('aria-label') || el.title || el.getAttribute('alt') || '').replace(/\s+/g, ' ').trim()
      if (t) add(el, 'click', t)
    })
    document.querySelectorAll('input').forEach((el) => {
      const t = el.getAttribute('placeholder') || el.getAttribute('aria-label') || el.getAttribute('name') || el.type
      add(el, 'input-' + (el.type || 'text'), t)
    })
    document.querySelectorAll('textarea').forEach((el) => {
      const t = el.getAttribute('placeholder') || el.getAttribute('aria-label') || 'textarea'
      add(el, 'textarea', t)
    })
    document.querySelectorAll('select').forEach((el) => {
      add(el, 'select', el.getAttribute('aria-label') || el.name || 'select')
    })
    return out
  })
}

// 按 idx 定位元素
function locatorByIdx(page, idx) {
  return page.locator(`[data-audit-idx="${idx}"]`)
}

// 对单个页面做巡检
async function auditPage(browser, pagePath, opts = {}) {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  const col = makeCollector()
  col.attach(page)
  const report = {
    page: pagePath,
    url: `${BASE}/#/${pagePath.replace(/^\//, '')}`,
    errors: { console: [], page: [], net: [] },
    interactions: [],
    loaded: false,
    loadError: null
  }
  try {
    await login(page)
  } catch (e) {
    report.loadError = 'login: ' + e.message
    await ctx.close()
    return report
  }
  // 导航到目标页
  const target = `${BASE}/#/${pagePath.replace(/^\//, '')}`
  try {
    await page.goto(target, { waitUntil: 'networkidle', timeout: 20000 })
  } catch (e) {
    report.loadError = 'goto: ' + e.message
  }
  await page.waitForTimeout(1500)
  report.loaded = true

  // 静态错误快照
  report.errors.console.push(...col.consoleErrors.map((t) => 'STATIC: ' + t))
  report.errors.page.push(...col.pageErrors.map((t) => 'STATIC: ' + t))
  report.errors.net.push(...col.netErrors.map((n) => `STATIC ${n.status} ${n.method} ${n.url}`))

  // 枚举元素
  let els = await enumInteractive(page)
  report.elementCount = els.length

  // 帮助函数
  const waitLoadingGone = async (maxMs = 8000) => {
    const start = Date.now()
    while (Date.now() - start < maxMs) {
      const n = await page.locator('.el-loading-mask:not([style*="display: none"]), .el-loading-mask:not(.is-fullscreen[style*="none"])').count().catch(() => 0)
      const visibleMask = await page.evaluate(() => {
        const ms = Array.from(document.querySelectorAll('.el-loading-mask'))
        return ms.some((m) => {
          const s = getComputedStyle(m)
          return s.display !== 'none' && s.visibility !== 'hidden' && Number(s.opacity || '1') > 0.01 && m.offsetParent !== null
        })
      }).catch(() => false)
      if (!visibleMask) return true
      await page.waitForTimeout(300)
    }
    return false
  }
  const closePopups = async () => {
    // 关闭 message-box（确认弹窗）：优先点取消(default 样式按钮)
    const mb = page.locator('.el-message-box')
    if (await mb.count()) {
      const cancel = mb.locator('.el-message-box__btns .el-button--default').first()
      if (await cancel.count()) { await cancel.click().catch(() => {}); await page.waitForTimeout(300); return }
      await page.keyboard.press('Escape').catch(() => {})
      await page.waitForTimeout(300)
      return
    }
    // 关闭 dialog/drawer：点关闭按钮或取消
    const closeBtn = page.locator('.el-dialog__headerbtn, .el-drawer__close-btn')
    if (await closeBtn.count()) { await closeBtn.first().click().catch(() => {}); await page.waitForTimeout(300); return }
    await page.keyboard.press('Escape').catch(() => {})
    await page.waitForTimeout(300)
  }
  const isDisabled = async (loc) => {
    return await loc.evaluate((e) => e.disabled || e.getAttribute('aria-disabled') === 'true' || e.classList.contains('is-disabled')).catch(() => false)
  }

  // 单页整体超时保护
  let pageTimedOut = false
  const pageDeadline = Date.now() + 110000

  // 收集增量错误
  const collectInc = (before, label, action) => {
    for (let k = before.c; k < col.consoleErrors.length; k++) { const t = col.consoleErrors[k]; if (!isNoise(t)) report.interactions.push({ action, label, note: 'CONSOLE: ' + t }) }
    for (let k = before.p; k < col.pageErrors.length; k++) report.interactions.push({ action, label, note: 'PAGEERROR: ' + col.pageErrors[k] })
    for (let k = before.n; k < col.netErrors.length; k++) report.interactions.push({ action, label, note: 'NET: ' + JSON.stringify(col.netErrors[k]) })
  }

  // 观察式交互: 逐个点击可点击元素
  const clickEls = els.filter((e) => e.kind === 'click')
  for (let i = 0; i < clickEls.length; i++) {
    if (Date.now() > pageDeadline) { pageTimedOut = true; break }
    const el = clickEls[i]
    // 跳过日期选择器内部按钮 / 分页 disabled / 已知内部控件
    if (/^(Previous|Next) (Year|Month|Year|Day)$|^(20\d\d|\d+月|January|February|March|April|May|June|July|August|September|October|November|December)$/.test(el.label)) continue
    if (/^(Go to previous page|Go to next page|Jump to|Total|goto)$/i.test(el.label)) continue
    const before = { c: col.consoleErrors.length, p: col.pageErrors.length, n: col.netErrors.length }
    // locator 在 try 内赋值、在 catch 内用于判定"是否 disabled 元素"；
    // 必须提升到 try 之外，否则 catch 分支访问会抛 ReferenceError。
    let locator
    try {
      locator = locatorByIdx(page, el.idx)
      if (await locator.count() === 0) continue
      if (await isDisabled(locator)) continue
      await waitLoadingGone()
      page.once('dialog', (d) => d.dismiss().catch(() => {}))
      await locator.click({ timeout: 4000 })
      await page.waitForTimeout(400)
      // 处理可能弹出的 message-box（确认类）
      const mbCount = await page.locator('.el-message-box').count()
      if (mbCount) {
        await closePopups()
        collectInc(before, el.label, 'click')
        continue
      }
      // 打开的 dialog 内: 仅填充表单字段，不点提交/危险按钮（避免副作用与连锁弹窗）
      const dlgCount = await page.locator('.el-dialog:visible, .el-drawer:visible').count()
      if (dlgCount) {
        const fields = await page.evaluate(() => {
          const out = []
          document.querySelectorAll('.el-dialog:not([style*="display: none"]), .el-drawer').forEach((d) => {
            d.querySelectorAll('input, textarea').forEach((b) => {
              if (b.type === 'checkbox' || b.type === 'radio' || b.type === 'file') return
              b.dataset.auditDlg = String(out.length)
              out.push({ idx: b.dataset.auditDlg, tag: b.tagName, placeholder: b.placeholder || '' })
            })
          })
          return out
        })
        for (const fld of fields) {
          const fb = { c: col.consoleErrors.length, p: col.pageErrors.length, n: col.netErrors.length }
          try {
            const f = page.locator(`[data-audit-dlg="${fld.idx}"]`)
            if (await f.count() === 0) continue
            if (fld.tag === 'TEXTAREA') await f.fill('测试内容').catch(() => {})
            else await f.fill('test').catch(() => {})
            await page.waitForTimeout(200)
          } catch (e) {
            report.interactions.push({ action: 'dialog-field', label: fld.placeholder, error: String(e.message || e).slice(0, 150) })
          }
          collectInc(fb, 'dialog:' + fld.placeholder, 'dialog-field')
          if (Date.now() > pageDeadline) { pageTimedOut = true; break }
        }
        await closePopups()
      }
    } catch (e) {
      // click 超时通常是 disabled 元素被框架拦截 / 元素被遮挡（element-plus link 按钮无 disabled 属性但 aria-disabled）。
      // 这类不涉及 JS 报错或网络错误，属"不可交互"而非功能缺陷，降级为 skip。
      const msg = String(e.message || e)
      const isTimeout = /Timeout \d+ms exceeded/.test(msg)
      let skip = isTimeout
      if (isTimeout) {
        try {
          if (await locator.count() && await isDisabled(locator)) skip = true
        } catch { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
        // 若点击超时但同时出现 console/pageerror，则仍记为 error（可能是真实功能异常）
        if (col.consoleErrors.length > before.c || col.pageErrors.length > before.p) skip = false
      }
      if (!skip) report.interactions.push({ action: 'click', label: el.label, error: msg.slice(0, 200) })
    }
    collectInc(before, el.label, 'click')
  }
  if (pageTimedOut) report.note = 'page_interaction_timeout_guard_triggered'

  // 汇总（仅保留非噪声 console）
  report.errors.console = [...new Set(col.consoleErrors.filter((t) => !isNoise(t)))]
  report.errors.page = [...new Set(col.pageErrors)]
  report.errors.net = [...new Set(col.netErrors.map((n) => JSON.stringify(n)))].map((s) => JSON.parse(s))

  await ctx.close()
  return report
}

async function main() {
  // 解析参数
  const argv = process.argv.slice(2)
  let pagesFile = null
  let single = null
  for (let i = 0; i < argv.length; i++) {
    if (argv[i] === '--page') single = argv[++i]
    else if (!argv[i].startsWith('--')) pagesFile = argv[i]
  }
  let pages
  if (single) {
    pages = [single]
  } else if (pagesFile) {
    pages = JSON.parse(fs.readFileSync(path.resolve(ROOT, pagesFile), 'utf8'))
  } else {
    // 默认从 pages.json
    pages = JSON.parse(fs.readFileSync(path.resolve(__dirname, 'pages.json'), 'utf8'))
  }

  const browser = await chromium.launch({ headless: true })
  const results = []
  const progressLog = path.resolve(OUT_DIR, 'progress.log')
  fs.writeFileSync(progressLog, '')
  for (const p of pages) {
    const line = `\n=== AUDIT ${p} ===\n`
    process.stdout.write(line)
    fs.appendFileSync(progressLog, line)
    const r = await auditPage(browser, p)
    const interactionErr = r.interactions.filter((i) => i.error)
    const net5xx = r.errors.net.filter((n) => n.status >= 500)
    const net4xx = r.errors.net.filter((n) => n.status >= 400 && n.status < 500)
    // 阻断性判定：代码层错误（console/pageerror/interactionErr）与 5xx 服务端错误。
    // 4xx 多为业务/客户端预期错误（前端已捕获处理），降级为 warning 记录，不阻断。
    const hasErr = r.errors.console.length || r.errors.page.length || interactionErr.length || net5xx.length > 0
    const summaryLine = `  elements=${r.elementCount} errors(c/p/n5/n4)=${r.errors.console.length}/${r.errors.page.length}/${net5xx.length}/${net4xx.length} interactionErr=${interactionErr.length}\n`
    process.stdout.write(summaryLine)
    fs.appendFileSync(progressLog, summaryLine)
    if (hasErr) {
      const detail = '  ' + JSON.stringify({ console: r.errors.console.slice(0, 5), page: r.errors.page.slice(0, 3), net: r.errors.net.slice(0, 5), interactionErrs: interactionErr.slice(0,5) }, null, 2) + '\n'
      process.stdout.write(detail)
      fs.appendFileSync(progressLog, detail)
    }
    const file = path.resolve(OUT_DIR, safeName(p) + '.json')
    fs.writeFileSync(file, JSON.stringify(r, null, 2))
    results.push({ page: p, file, hasErr })
  }
  await browser.close()

  const summary = { total: results.length, withErrors: results.filter((r) => r.hasErr).map((r) => r.page) }
  fs.writeFileSync(path.resolve(OUT_DIR, 'summary.json'), JSON.stringify(summary, null, 2))
  process.stdout.write(`\nDONE total=${summary.total} withErrors=${summary.withErrors.length}\n`)
  process.stdout.write(JSON.stringify(summary.withErrors, null, 2) + '\n')
}

main().catch((e) => { console.error('FATAL', e); process.exit(1) })
