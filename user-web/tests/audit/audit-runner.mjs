// 全页面自动巡检脚本（user-web）
// 用法: node tests/audit/audit-runner.mjs [--auto] [pagesFile] [--page <hashPath>]
//                                        [--only <路径子串>] [--shard <i>/<n>]
//   --auto   现场从 src/router/modules/*.js 提取全部路由（默认读 pages.json）
//   --only   只跑路径含该子串的页面
//   --shard  取模分片，多进程并行时用（如 --shard 0/3）
// 账号: e2e_admin ↔ .env 的 HIVEMTK_ADMIN_PASS（专用自动化账号）；E2E_USER/E2E_PASS 可覆盖。
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
const BASE = process.env.E2E_BASE_URL || 'http://localhost:8211'
const API_BASE = process.env.API_BASE_URL || 'http://localhost:8204'
const OUT_DIR = path.resolve(__dirname, 'reports')
fs.mkdirSync(OUT_DIR, { recursive: true })

// 仓根 .env 逐键取值（同 scripts/rotate-admin-password.sh 的 env_get）：同名键可重复出现，取首条；
// 不 source 整个文件——带空格的值会打断解析。
function envGet(key) {
  try {
    for (const line of fs.readFileSync(path.resolve(ROOT, '../.env'), 'utf8').split('\n')) {
      if (line.startsWith(`${key}=`)) return line.slice(key.length + 1).replace(/\r$/, '').trim()
    }
  } catch { /* 干净克隆 / CI 无 .env 属正常档 */ }
  return ''
}

// 账号与口令必须成对尝试：HIVEMTK_ADMIN_PASS 配的是 e2e_admin（专用自动化账号，
// 口径同 scripts/api_verify_full.py），拿它去登 admin 只会白烧防爆破阈值（5 次/15 分钟）。
const ACCOUNTS = [
  { user: process.env.E2E_USER || 'e2e_admin', pass: process.env.E2E_PASS || envGet('HIVEMTK_ADMIN_PASS') },
  { user: 'admin', pass: envGet('SEED_PASSWORD') },
  { user: 'admin', pass: 'Admin@12345678' },
  { user: 'admin', pass: 'Admin@123456' },
  { user: 'admin', pass: '62cfdc6bf1b075830734cc6f9a63501b' }
].filter((a, i, arr) => a.pass && arr.findIndex((x) => x.user === a.user && x.pass === a.pass) === i)

function safeName(p) {
  return p.replace(/[^a-zA-Z0-9_-]/g, '_')
}

async function login(page) {
  await page.goto(`${BASE}/#/login`)
  await page.waitForSelector('.login-box', { timeout: 15000 })
  // 换下一对候选的**唯一**许可来自服务端响应码。
  // 早先的写法是"等 6 秒没跳走就换下一对"：后端一慢，一次翻页就连撞 5 对口令，
  // 而防爆破阈值是 5 次失败/15 分钟 → 锁 30 分钟，后面每一页都登不进去（假红成片）。
  let lastStatus = 0
  const probe = (res) => {
    if (res.url().includes('/api/auth/login')) lastStatus = res.status()
  }
  page.on('response', probe)
  try {
    for (const acc of ACCOUNTS) {
      lastStatus = 0
      await page.locator('.login-box input[type="text"]').first().fill(acc.user)
      await page.locator('.login-box input[type="password"]').fill(acc.pass)
      await page.locator('.login-box button.el-button--primary').click()
      try {
        await page.waitForURL((u) => !u.hash.includes('/login'), { timeout: 20000 })
        return true
      } catch {
        const rejected = lastStatus === 400 || lastStatus === 401 || lastStatus === 403
        if (!rejected) {
          throw new Error(
            `登录未取得跳转（最后响应码=${lastStatus || '无应答/20s 超时'}）：` +
            '口令未被判定，停止尝试其余候选以免烧掉防爆破阈值——这是环境问题，不是页面缺陷'
          )
        }
      }
      await page.waitForTimeout(300)
    }
  } finally {
    page.off('response', probe)
  }
  throw new Error(`登录失败：${ACCOUNTS.length} 组候选账号/口令均被拒（最后一次 ${lastStatus}）`)
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

// 每页各自登录，不共享一份登录态快照：/api/auth/refresh-token 是一次性轮换
// （AuthService.RefreshToken 签发新令牌同时把旧令牌拉黑，防新旧并行可用），
// 只要有一页触发过刷新，快照里那把旧令牌对之后所有页都变成"已失效"，
// 后面每一页都会被 401 踢回登录页——是测试自己的动作，不是被测页面的缺陷。
async function newAuthedContext(browser) {
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await login(page)
  return { ctx, page }
}

// 页面上"人眼看得见"的坏状态：落到 404 / 被踢回登录页 / 首屏空白 / 加载即弹错误条。
// 这四种都不一定留 console 痕迹，只看 console 会把它们全判成绿。
async function renderSignals(page) {
  return await page.evaluate(() => {
    const main = document.querySelector('.app-main')
    const text = (main && main.innerText ? main.innerText : document.body.innerText || '')
      .replace(/\s+/g, ' ').trim()
    const notFound = !!document.querySelector('.error-code')
    const toast = Array.from(document.querySelectorAll('.el-message--error, .el-notification--error'))
      .map((el) => el.innerText.replace(/\s+/g, ' ').trim().slice(0, 160))
      .filter(Boolean)
    return {
      notFound,
      blank: !notFound && text.length < 24,
      textSample: text.slice(0, 200),
      nodeCount: main ? main.querySelectorAll('*').length : -1,
      toast
    }
  }).catch(() => ({ notFound: false, blank: false, textSample: 'eval-failed', nodeCount: -1, toast: [] }))
}

// 把收集器里的原始增量落成报告字段（去重 + 去噪）。
// 抽出来是因为"被踢回登录页"这条早退路径同样要留证据——哪一次 401 把用户踢出去的，
// 是判定的核心，不能因为早退就只剩一句 kickedTo。
function finalizeErrors(col, report) {
  report.errors.console = [...new Set(col.consoleErrors.filter((t) => !isNoise(t)))]
  report.errors.page = [...new Set(col.pageErrors)]
  report.errors.net = [...new Set(col.netErrors.map((n) => JSON.stringify(n)))].map((s) => JSON.parse(s))
}

// 点了就把当前令牌作废的按钮（侧栏用户菜单里的"退出登录"等）。
const SESSION_KILL = /^(退出登录|登出|注销|安全退出|退出账号|log\s?out|sign\s?out)$/i

// 对单个页面做巡检：独立登录 → 导航 → 取证 → 观察式点击。
// 登录成功后仍被踢回登录页时换一把新令牌重试；两次都被踢才判为页面问题。
async function auditPage(browser, pagePath) {
  const report = {
    page: pagePath,
    url: `${BASE}/#/${pagePath.replace(/^\//, '')}`,
    errors: { console: [], page: [], net: [] },
    interactions: [],
    loaded: false,
    loadError: null,
    sessionKillSkipped: 0
  }
  // 导航到目标页
  const target = `${BASE}/#/${pagePath.replace(/^\//, '')}`
  // 目标本身就是登录页/安装向导时，最终停在 #/login 不是"被踢"：/login 是它自己，
  // /setup 在系统已初始化时按设计 router.push('/login')（views/setup/InitSetup.vue
  // onMounted 的 HAS_ADMIN/INITIALIZED 分支）。早先这两种和真被踢走同一条早退路径，
  // 结果是这两个页面一个元素都没被点过，报告里 elements=undefined 看着却像"测过且干净"。
  const isAuthRoute = /^\/(login|setup)(\/|$|\?)/.test('/' + pagePath.replace(/^\//, ''))
  let ctx, page, col
  for (let attempt = 0; ; attempt++) {
    try {
      ;({ ctx, page } = await newAuthedContext(browser))
    } catch (e) {
      report.loadError = 'login: ' + e.message
      return report
    }
    col = makeCollector()
    col.attach(page)
    // networkidle 等的是"这页没有未完成的请求"。两类合法页面天生过不了它：
    // 会自刷新的看板，以及示例数据里指向外网的图片（卡片表 image_url=https://example.com，
    // DNS 挂起 20s+）。页面本身是好的，所以这里退一档按 domcontentloaded 判定，
    // 并把走过的路记进报告——不然是把"取证工具等不动"写成"这页坏了"。
    try {
      await page.goto(target, { waitUntil: 'networkidle', timeout: 20000 })
      report.loaded = true
    } catch (e) {
      const idleErr = e.message
      try {
        await page.goto(target, { waitUntil: 'domcontentloaded', timeout: 20000 })
        report.loaded = true
        report.loadNote = `networkidle 超时已降级按 domcontentloaded 判定：${idleErr.slice(0, 80)}`
      } catch (e2) {
        report.loaded = false
        report.loadError = 'goto: ' + idleErr
      }
    }
    await page.waitForTimeout(1500)
    report.render = await renderSignals(page)
    if (!/#\/(login|setup)/.test(page.url())) break
    if (isAuthRoute) {
      // 落在登录页属预期：记下 landing 供读数核对，继续走全元素点击腿
      report.render.landedOn = new URL(page.url()).hash
      break
    }
    report.render.kickedTo = new URL(page.url()).hash
    finalizeErrors(col, report)
    await ctx.close()
    if (attempt >= 1) return report
    report.reauthed = true
  }

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
    // 会话销毁类按钮一律不点：它会把本趟巡检用的令牌送进黑名单，之后每一页都被
    // 401 踢回登录页——那是测试自己造成的动作，不是被测页面的缺陷。
    if (SESSION_KILL.test(el.label)) { report.sessionKillSkipped++; continue }
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
  finalizeErrors(col, report)

  await ctx.close()
  return report
}

// 页面清单不写死：在浏览器里 import 每个路由模块（Vite dev 直接按 /src/ 路径回源），
// 拿到的是注册用的同一份对象——静态正则提取会漏掉 children 和非常规写法。
// 这一步只是读模块导出的路由表，不需要登录态。
async function discoverPages(browser) {
  const dir = path.resolve(ROOT, 'src/router/modules')
  const files = fs.readdirSync(dir).filter((f) => f.endsWith('.js')).sort()
  const ctx = await browser.newContext()
  const page = await ctx.newPage()
  await page.goto(BASE, { waitUntil: 'load', timeout: 30000 })
  const found = await page.evaluate(async (files) => {
    const out = []
    for (const f of files) {
      try {
        const mod = await import(`/src/router/modules/${f}`)
        const list = Array.isArray(mod.default) ? mod.default : [mod.default]
        const walk = (rs, parent) => rs.forEach((r) => {
          const full = !r.path ? parent : (r.path.startsWith('/') ? r.path : `${parent}/${r.path}`.replace(/\/+/g, '/'))
          if (r.path) out.push({ module: f, path: full, name: r.name || '', title: (r.meta && r.meta.title) || '' })
          if (r.children) walk(r.children, full)
        })
        walk(list, '')
      } catch (e) {
        out.push({ module: f, path: '', error: String(e && e.message || e).slice(0, 200) })
      }
    }
    return out
  }, files)
  await ctx.close()
  const statics = ['/login', '/setup', '/profile', '/notifications', '/help-center', '/chat/embed/default']
    .map((p) => ({ module: 'index.js', path: p, name: '', title: '' }))
  return { routes: found.concat(statics) }
}

const USAGE = '用法: node tests/audit/audit-runner.mjs [--auto] [pagesFile] [--page <hashPath>] [--only <路径子串>] [--shard <i>/<n>]'
const KNOWN_FLAGS = ['--page', '--auto', '--only', '--shard']
const NEEDS_VALUE = new Set(['--page', '--only', '--shard'])

async function main() {
  // 解析参数
  const argv = process.argv.slice(2)
  let pagesFile = null
  let single = null
  let auto = false
  let only = null
  let shard = null
  for (let i = 0; i < argv.length; i++) {
    const a = argv[i]
    if (!a.startsWith('--')) {
      pagesFile = a
      continue
    }
    // 认不出的旗标、或带值旗标漏了值，都会让下面的分支一个都不进 ⇒ 静默退化成"跑整份清单"。
    // 巡检一次要开上百个页面，这种退化必须当场报错，不能靠人看出跑多了。
    if (!KNOWN_FLAGS.includes(a)) {
      process.stderr.write(`用法错误：不认识的参数 ${a}\n${USAGE}\n`)
      process.exit(2)
    }
    if (NEEDS_VALUE.has(a) && (argv[i + 1] === undefined || argv[i + 1].startsWith('--'))) {
      process.stderr.write(`用法错误：${a} 后面必须有取值\n${USAGE}\n`)
      process.exit(2)
    }
    if (a === '--page') single = argv[++i]
    else if (a === '--auto') auto = true
    else if (a === '--only') only = argv[++i]
    else if (a === '--shard') shard = argv[++i]
  }

  const RUN_TAG = process.env.AUDIT_RUN_TAG || new Date().toISOString().slice(0, 19).replace(/[-:T]/g, '')
  const RUN_DIR = path.resolve(OUT_DIR, `run-${RUN_TAG}`)
  fs.mkdirSync(RUN_DIR, { recursive: true })

  const browser = await chromium.launch({ headless: true })

  let pages
  let discovered = null
  if (single) {
    pages = [single]
  } else if (auto) {
    discovered = await discoverPages(browser)
    const importErrors = discovered.routes.filter((r) => r.error)
    fs.writeFileSync(path.resolve(RUN_DIR, 'routes.json'), JSON.stringify(discovered.routes, null, 2))
    pages = discovered.routes
      .filter((r) => r.path && !r.path.includes(':'))
      .map((r) => r.path)
      .filter((p, i, arr) => arr.indexOf(p) === i)
      .sort()
    process.stdout.write(`[auto] 路由记录 ${discovered.routes.length} 条，可直访 ${pages.length} 条，模块 import 失败 ${importErrors.length} 条\n`)
    if (importErrors.length) process.stdout.write(JSON.stringify(importErrors, null, 2) + '\n')
  } else {
    const raw = JSON.parse(fs.readFileSync(path.resolve(ROOT, pagesFile || path.resolve(__dirname, 'pages.json')), 'utf8'))
    // 清单既可能是路径字符串数组，也可能是 extract_routes.py 的对象数组
    pages = raw.map((p) => (typeof p === 'string' ? p : p.path))
  }
  if (only) pages = pages.filter((p) => p.includes(only))
  if (shard) {
    const [idx, n] = shard.split('/').map(Number)
    pages = pages.filter((_, i) => i % n === idx)
  }
  process.stdout.write(`[run] ${pages.length} 页 → ${RUN_DIR}\n`)

  const results = []
  const progressLog = path.resolve(RUN_DIR, 'progress.log')
  fs.writeFileSync(progressLog, '')
  for (const p of pages) {
    const line = `\n=== AUDIT ${p} ===\n`
    process.stdout.write(line)
    fs.appendFileSync(progressLog, line)
    const r = await auditPage(browser, p)
    const interactionErr = r.interactions.filter((i) => i.error)
    const net5xx = r.errors.net.filter((n) => n.status >= 500)
    const net4xx = r.errors.net.filter((n) => n.status >= 400 && n.status < 500)
    // 阻断性判定：代码层错误（console/pageerror/interactionErr）、5xx 服务端错误，
    // 以及"人眼可见"的坏状态（404 兜底页 / 空白首屏 / 被踢回登录页 / 加载即弹错误条）。
    // 4xx 多为业务/客户端预期错误（前端已捕获处理），降级为 warning 记录，不阻断。
    const vis = r.render || {}
    const visualBad = Boolean(vis.notFound || vis.blank || vis.kickedTo || (vis.toast || []).length)
    // 没载入成功的页面，四个错误计数天然是 0 —— 那不是"干净"，是"这一页根本没测到"。
    // 早先这里把它算成 hasErr=false，逐行读数看着全绿，只有跑 triage 翻 JSON 才看得见 LOAD。
    const notLoaded = !r.loaded
    const hasErr = notLoaded || r.errors.console.length || r.errors.page.length || interactionErr.length || net5xx.length > 0 || visualBad
    const summaryLine = `  loaded=${notLoaded ? 'NO ' + JSON.stringify((r.loadError || '').slice(0, 120)) : 'yes'}${r.loadNote ? ' (loadNote: ' + r.loadNote + ')' : ''} elements=${r.elementCount}${vis.landedOn ? ' landedOn=' + vis.landedOn : ''} errors(c/p/n5/n4)=${r.errors.console.length}/${r.errors.page.length}/${net5xx.length}/${net4xx.length} interactionErr=${interactionErr.length} skipLogout=${r.sessionKillSkipped || 0} reauth=${r.reauthed ? 'yes' : 'no'} visual=${visualBad ? JSON.stringify({ nf: vis.notFound, blank: vis.blank, kicked: vis.kickedTo, toast: vis.toast }) : 'ok'}\n`
    process.stdout.write(summaryLine)
    fs.appendFileSync(progressLog, summaryLine)
    if (hasErr) {
      const detail = '  ' + JSON.stringify({ render: r.render, console: r.errors.console.slice(0, 5), page: r.errors.page.slice(0, 3), net: r.errors.net.slice(0, 5), interactionErrs: interactionErr.slice(0,5) }, null, 2) + '\n'
      process.stdout.write(detail)
      fs.appendFileSync(progressLog, detail)
    }
    const file = path.resolve(RUN_DIR, safeName(p) + '.json')
    fs.writeFileSync(file, JSON.stringify(r, null, 2))
    results.push({ page: p, file, hasErr })
  }
  await browser.close()

  const summary = { run: RUN_TAG, total: results.length, withErrors: results.filter((r) => r.hasErr).map((r) => r.page) }
  fs.writeFileSync(path.resolve(RUN_DIR, 'summary.json'), JSON.stringify(summary, null, 2))
  process.stdout.write(`\nDONE total=${summary.total} withErrors=${summary.withErrors.length}\n`)
  process.stdout.write(JSON.stringify(summary.withErrors, null, 2) + '\n')
}

main().catch((e) => { console.error('FATAL', e); process.exit(1) })
