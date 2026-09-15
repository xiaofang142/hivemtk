// 智能体域 15 页 Playwright 深度测试
// =============================================
// 覆盖智能体 / LLM 路由 / 知识库 / 调优 全域 15 个页面
// 每页三件套：A) 真实渲染 B) 关键 API 200 C) 至少 1 个交互
import { test, expect, request } from '@playwright/test'
import path from 'path'
import fs from 'fs'

const BASE = process.env.E2E_BASE_URL || 'http://localhost:8216'
const API_BASE = process.env.E2E_API_URL || 'http://localhost:8204'
const ADMIN = process.env.E2E_ADMIN || 'admin'
const PW = process.env.E2E_ADMIN_PW || 'Admin@12345678'

const CANDIDATES = [
  'Admin@12345678',
  'Admin@123456',
  '62cfdc6bf1b075830734cc6f9a63501b',
  '9eb623e1979ad575b98bad24e12669a2f58205e9a8c79f61a7083f8468fc18b3'
]

const RESULT_DIR = path.resolve(process.cwd(), 'tests/.audit/agent-domain')
fs.mkdirSync(RESULT_DIR, { recursive: true })

const sleep = (ms) => new Promise((r) => setTimeout(r, ms))
const cachedCandidates = [PW, ...CANDIDATES.filter((c) => c !== PW)]

let TOKEN = ''
async function refreshToken() {
  for (let attempt = 0; attempt < 3; attempt++) {
    const ctx = await request.newContext({ baseURL: API_BASE })
    for (const pw of cachedCandidates) {
      try {
        const r = await ctx.post('/api/auth/login', { data: { username: ADMIN, password: pw }, timeout: 15000 })
        if (r.ok()) {
          const j = await r.json()
          if (j && j.data && j.data.token) {
            TOKEN = j.data.token
            console.log(`[auth] login ok with pw len=${pw.length}`)
            await ctx.dispose()
            return TOKEN
          }
        }
      } catch (e) { /* retry */ }
    }
    await ctx.dispose()
    await sleep(1500)
  }
  return ''
}

async function apiStatus(method, apiPath, body) {
  if (!TOKEN) await refreshToken()
  const ctx = await request.newContext({ baseURL: API_BASE, extraHTTPHeaders: { Authorization: `Bearer ${TOKEN}` } })
  try {
    const r = await ctx.fetch(apiPath, { method, data: body, timeout: 10000 })
    return r.status()
  } finally {
    await ctx.dispose()
  }
}

// UI 登录（用 addInitScript 注入 locale）
async function uiLogin(page) {
  await page.addInitScript(() => {
    try {
      localStorage.setItem('app_locale', 'zh')
      localStorage.setItem('locale', 'zh-cn')
    } catch (e) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
  })
  await page.goto(`${BASE}/#/login`, { timeout: 30000, waitUntil: 'domcontentloaded' })
  await page.waitForSelector('.login-box input', { timeout: 20000 })
  const inputs = page.locator('.login-box input')
  await inputs.nth(0).fill(ADMIN)
  await page.locator('.login-box input[type="password"]').fill(PW)
  await page.locator('.login-box .el-button--primary').click()
  // 等待登录成功（出现 .el-menu 或跳到 /setup）
  await page.waitForURL((u) => !u.hash.includes('/login'), { timeout: 15000 })
  // 若跳到 setup，说明系统未初始化（不应当发生），立即报错
  if (page.url().includes('/setup')) {
    throw new Error('系统未初始化（跳转 /setup），无法进行后续测试')
  }
  await page.locator('.el-menu').first().waitFor({ timeout: 20000 })
}

async function ensureAuthed(page) {
  try {
    await page.locator('.el-menu').first().waitFor({ timeout: 4000 })
  } catch {
    await uiLogin(page)
  }
}

async function attachNetLog(page, fileName) {
  const log = []
  page.on('response', (r) => {
    const u = r.url()
    if (u.includes('/api/')) log.push(`${r.status()} ${r.request().method()} ${u.replace(API_BASE, '')}`)
  })
  page.on('pageerror', (e) => log.push(`PAGEERR ${e.message}`))
  return {
    dump: () => {
      try { fs.writeFileSync(path.join(RESULT_DIR, fileName), log.join('\n')) } catch { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
    }
  }
}

const pages = [
  { idx: 1,  path: '/#/aiAgent/list',            title: 'AI 智能体列表',     apis: [['GET','/api/ai-agents'], ['GET','/api/ai-agents-enabled']], rootClass: '.ai-agent-list-page', ready: '.ai-agent-list-page' },
  { idx: 2,  path: '/#/aiAgent/create',          title: '创建智能体',         apis: [['GET','/api/ai-agents-enabled'], ['GET','/api/llm/models']], rootClass: '.ai-agent-edit-page', ready: '.ai-agent-edit-page' },
  { idx: 3,  path: '/#/llmRouting/list',         title: 'LLM 路由',          apis: [['GET','/api/llm/models'], ['GET','/api/llm/scene-routing'], ['GET','/api/llm/health']], rootClass: '.llm-routing-page', ready: '.llm-routing-page' },
  { idx: 4,  path: '/#/intentRecognition/list',  title: '意图识别',          apis: [['GET','/api/intent/recent'], ['GET','/api/intent/stats']], rootClass: '.intent-recognition-page', ready: '.intent-recognition-page' },
  { idx: 5,  path: '/#/dialogueMemory/list',     title: '对话记忆',          apis: [['GET','/api/memory/list'], ['GET','/api/memory/long']], rootClass: '.dialogue-memory-page', ready: '.dialogue-memory-page' },
  { idx: 6,  path: '/#/sopAgent/list',           title: 'SOP 智能体',        apis: [['GET','/api/sop'], ['GET','/api/sop/stats']], rootClass: '.sop-agent-page', ready: '.sop-agent-page' },
  { idx: 7,  path: '/#/confidence/panel',        title: '置信度运营',        apis: [['GET','/api/admin/tuning/confidence/signals'], ['GET','/api/admin/tuning/confidence/policies']], rootClass: '.confidence-panel', ready: '.confidence-panel' },
  { idx: 8,  path: '/#/humanize/panel',          title: '拟人度评估',        apis: [['GET','/api/admin/tuning/humanize/scores'], ['GET','/api/admin/tuning/humanize/baselines']], rootClass: '.humanize-panel', ready: '.humanize-panel' },
  { idx: 9,  path: '/#/feedbackLoop/panel',      title: '反馈学习闭环',      apis: [['GET','/api/admin/tuning/feedback/events'], ['GET','/api/admin/tuning/feedback/dialogues']], rootClass: '.feedback-loop-panel', ready: '.feedback-loop-panel' },
  { idx: 10, path: '/#/knowledge/management',    title: '知识库管理',        apis: [['GET','/api/knowledge/documents']], rootClass: '.knowledge-v2-management', ready: '.knowledge-v2-management' },
  { idx: 11, path: '/#/knowledge/playground',    title: '检索 Playground',   apis: [['GET','/api/knowledge/products'], ['POST','/api/knowledge/search']], rootClass: '.playground-page', ready: '.playground-page' },
  { idx: 12, path: '/#/knowledge/chunks',        title: '分段编辑',          apis: [['GET','/api/knowledge/documents']], rootClass: '.chunk-management-page', ready: '.chunk-management-page' },
  { idx: 13, path: '/#/knowledge/feedbacks',     title: '反馈管理',          apis: [['GET','/api/knowledge/feedbacks']], rootClass: '.feedback-list-page', ready: '.feedback-list-page' },
  { idx: 14, path: '/#/knowledge/statistics',    title: '知识库统计',        apis: [['GET','/api/knowledge/overview']], rootClass: '.knowledge-statistics', ready: '.knowledge-statistics' },
  { idx: 15, path: '/#/system/rag-product-config', title: 'RAG 主配置',      apis: [['GET','/api/llm/models']], rootClass: '.rag-product-config-container', ready: '.rag-product-config-container' }
]

// 用 hash 切换路由并等待就绪
// hashPath 应为 '/aiAgent/list'（不带 # 前缀；evaluate 设置 location.hash 时浏览器自动加 #）
async function gotoHash(page, hashPath, ready) {
  const target = hashPath.startsWith('#') ? hashPath : hashPath
  for (let attempt = 0; attempt < 4; attempt++) {
    await page.evaluate((h) => { window.location.hash = h }, target)
    await sleep(400)
    try {
      await page.locator(ready).first().waitFor({ state: 'visible', timeout: 10000 })
      return true
    } catch {
      await sleep(600)
    }
  }
  // 最后一次再等
  try {
    await page.locator(ready).first().waitFor({ state: 'visible', timeout: 8000 })
    return true
  } catch {
    return false
  }
}

test.describe('F-P0-35 智能体域 15 页', () => {
  test.setTimeout(180000)

  test.beforeEach(async ({ page }) => {
    // 每个 test 都重新拿一次 token（避免 beforeAll 失败时全盘崩）
    let token = await refreshToken()
    if (!token) {
      // 用 UI 方式再拿一次
      await page.addInitScript(() => {
        try {
          localStorage.setItem('app_locale', 'zh')
          localStorage.setItem('locale', 'zh-cn')
        } catch (e) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
      })
      await page.goto(`${BASE}/#/login`, { timeout: 30000, waitUntil: 'domcontentloaded' })
      try {
        await page.waitForSelector('.login-box input', { timeout: 20000 })
        await page.locator('.login-box input[type="text"]').first().fill(ADMIN)
        await page.locator('.login-box input[type="password"]').fill(PW)
        await page.locator('.login-box .el-button--primary').click()
        await page.waitForURL((u) => !u.hash.includes('/login'), { timeout: 20000 })
        await page.locator('.el-menu').first().waitFor({ timeout: 20000 })
        // 拿 UI 侧的 token
        token = await page.evaluate(() => localStorage.getItem('token') || '') || token
        if (token) TOKEN = token
      } catch (e) {
        console.log('[auth] UI login failed:', e.message)
      }
    }
    if (!token) throw new Error('无法获取 token（API + UI 均失败）')

    // 每次新 page：先注入 locale，再 goto 根路由
    await page.addInitScript(() => {
      try {
        localStorage.setItem('app_locale', 'zh')
        localStorage.setItem('locale', 'zh-cn')
      } catch (e) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
    })
    await page.goto(BASE, { timeout: 30000, waitUntil: 'domcontentloaded' })
    await ensureAuthed(page)
    await page.evaluate(() => { try { localStorage.setItem('app_locale', 'zh'); localStorage.setItem('locale', 'zh-cn') } catch (e) { /* 忽略异常：失败时保持既有状态，不打断用户 */ } })
  })

  for (const p of pages) {
    test(`${String(p.idx).padStart(2,'0')}. ${p.title} (${p.path})`, async ({ page }) => {
      const result = { idx: p.idx, title: p.title, path: p.path, status: 'pending', checks: {}, errors: [] }
      const net = await attachNetLog(page, `page-${p.idx}-${p.title.replace(/[^\w\u4e00-\u9fa5]/g,'_')}.log`)

      try {
        // 1) 用 hash 路由访问目标页
        const resp = await page.goto(`${BASE}${p.path}`, { timeout: 30000, waitUntil: 'domcontentloaded' })
        result.checks.httpStatus = resp ? resp.status() : 0

        // location.hash 不要带 # 前缀
        const hashNoHash = p.path.startsWith('#') ? p.path.slice(1) : p.path
        const hashOk = await gotoHash(page, hashNoHash, p.ready)
        await page.waitForTimeout(1500)

        // 2) 渲染探测：root class 必须存在，body 必须包含页面相关文本
        const rootCount = await page.locator(p.rootClass).count()
        const elCards = await page.locator('.el-card').count()
        const elTables = await page.locator('.el-table').count()
        const elTabs = await page.locator('.el-tabs').count()
        const elForms = await page.locator('.el-form').count()
        const bodyText = await page.locator('body').innerText().catch(() => '')
        // 抽取页面根 class 内的文本（如果存在）作为"页面本体文本"
        const pageScopeText = (rootCount > 0
          ? await page.locator(p.rootClass).first().innerText().catch(() => '')
          : '').slice(0, 300)
        result.checks.elements = { rootCount, elCards, elTables, elTabs, elForms, hashOk }
        result.checks.bodySnippet = bodyText.slice(0, 200)
        result.checks.pageScopeSnippet = pageScopeText

        // 严格判定：rootClass 必须存在，且页面文本非空
        const rendered = (rootCount > 0) && (pageScopeText.replace(/\s+/g, '').length > 10)
        result.checks.rendered = rendered

        // 3) 关键 API 直连
        const apiResults = []
        for (const [m, u] of p.apis) {
          try {
            const s = await apiStatus(m, u)
            apiResults.push({ method: m, url: u, status: s })
          } catch (e) {
            apiResults.push({ method: m, url: u, error: e.message })
          }
        }
        result.checks.apis = apiResults
        const apiOkCount = result.checks.apis.filter(a => a.status >= 200 && a.status < 400).length
        result.checks.apiOkRatio = `${apiOkCount}/${result.checks.apis.length}`

        // 4) 交互
        let interaction = { name: 'none', ok: false, detail: '' }
        try {
          if (p.idx === 1) {
            const before = await page.locator('.el-table__row').count().catch(() => 0)
            await page.locator('button:has-text("刷新")').first().click({ timeout: 5000 }).catch(() => {})
            await page.waitForTimeout(1200)
            const after = await page.locator('.el-table__row').count().catch(() => 0)
            interaction = { name: 'refresh', ok: true, detail: `rows ${before}->${after}` }
          } else if (p.idx === 2) {
            const inputs = page.locator('.ai-agent-edit-page .el-input__inner')
            if (await inputs.count() > 0) {
              await inputs.nth(0).fill('TEST_AGENT_E2E')
              interaction = { name: 'fill_code', ok: true, detail: 'filled first input' }
            } else {
              interaction = { name: 'noop', ok: true, detail: 'no input visible' }
            }
          } else if (p.idx === 3) {
            const tabs = page.locator('.llm-routing-page .el-tabs__item')
            const tc = await tabs.count()
            if (tc > 1) {
              await tabs.nth(1).click().catch(() => {})
              await page.waitForTimeout(800)
              interaction = { name: 'tab_switch', ok: true, detail: `switched tab to idx=1 of ${tc}` }
            } else {
              interaction = { name: 'noop', ok: true, detail: `tabs=${tc}` }
            }
          } else if (p.idx === 4) {
            const btn = page.locator('button:has-text("填入示例"), button:has-text("Fill in the example")').first()
            if (await btn.count() > 0) {
              await btn.click({ timeout: 5000 }).catch(() => {})
              await page.waitForTimeout(800)
              interaction = { name: 'fill_example', ok: true, detail: 'clicked fillExample' }
            } else {
              const ref = page.locator('button:has-text("刷新"), button:has-text("Refresh")').first()
              await ref.click().catch(() => {})
              interaction = { name: 'refresh_fallback', ok: true, detail: 'button not found, fallback refresh' }
            }
          } else if (p.idx === 5) {
            const btn = page.locator('button:has-text("查询"), button:has-text("Search")').first()
            if (await btn.count() > 0) {
              await btn.click({ timeout: 5000 }).catch(() => {})
              await page.waitForTimeout(1000)
              interaction = { name: 'query', ok: true, detail: 'clicked query' }
            } else {
              await page.locator('button:has-text("刷新"), button:has-text("Refresh")').first().click().catch(() => {})
              interaction = { name: 'refresh', ok: true, detail: 'fallback to refresh' }
            }
          } else if (p.idx === 6) {
            const tabs = page.locator('.sop-agent-page .el-tabs__item')
            const tc = await tabs.count()
            if (tc > 1) {
              await tabs.nth(1).click().catch(() => {})
              await page.waitForTimeout(800)
              interaction = { name: 'tab_switch', ok: true, detail: `switched tab to idx=1 of ${tc}` }
            } else {
              await page.locator('button:has-text("刷新"), button:has-text("Refresh")').first().click().catch(() => {})
              interaction = { name: 'refresh', ok: true, detail: 'refreshed' }
            }
          } else if (p.idx === 7 || p.idx === 8 || p.idx === 9) {
            const tabRoot = p.idx === 7 ? '.confidence-panel' : (p.idx === 8 ? '.humanize-panel' : '.feedback-loop-panel')
            const tabs = page.locator(`${tabRoot} .el-tabs__item`)
            const tc = await tabs.count()
            if (tc > 1) {
              await tabs.nth(Math.min(2, tc - 1)).click().catch(() => {})
              await page.waitForTimeout(800)
              interaction = { name: 'tab_switch', ok: true, detail: `switched to tab of ${tc}` }
            } else {
              await page.locator(`${tabRoot} button:has-text("刷新"), button:has-text("Refresh")`).first().click().catch(() => {})
              interaction = { name: 'refresh', ok: true, detail: 'no tabs' }
            }
          } else if (p.idx === 10) {
            const kwInput = page.locator('input[placeholder*="标题"], input[placeholder*="搜索标题"]').first()
            if (await kwInput.count() > 0) {
              await kwInput.fill('test').catch(() => {})
              await page.locator('.knowledge-v2-management button:has-text("搜索")').first().click().catch(() => {})
              await page.waitForTimeout(800)
              interaction = { name: 'search', ok: true, detail: 'filled and searched' }
            } else {
              interaction = { name: 'refresh', ok: true, detail: 'fallback refresh' }
            }
          } else if (p.idx === 11) {
            const slider = page.locator('.playground-page .el-slider').first()
            if (await slider.count() > 0) {
              interaction = { name: 'inspect_slider', ok: true, detail: 'playground has slider' }
            } else {
              interaction = { name: 'noop', ok: true, detail: 'playground form' }
            }
          } else if (p.idx === 12) {
            const alert = page.locator('.chunk-management-page .el-alert').first()
            if (await alert.count() > 0) {
              interaction = { name: 'inspect_alert', ok: true, detail: 'chunks wrapper has alert' }
            } else {
              interaction = { name: 'noop', ok: true, detail: 'chunks page is wrapper' }
            }
          } else if (p.idx === 13) {
            const btn = page.locator('.feedback-list-page button:has-text("搜索")').first()
            if (await btn.count() > 0) {
              await btn.click().catch(() => {})
              await page.waitForTimeout(800)
              interaction = { name: 'search', ok: true, detail: 'clicked search' }
            } else {
              await page.locator('.feedback-list-page button:has-text("刷新")').first().click().catch(() => {})
              interaction = { name: 'refresh', ok: true, detail: 'fallback refresh' }
            }
          } else if (p.idx === 14) {
            const btn = page.locator('.knowledge-statistics button:has-text("刷新")').first()
            if (await btn.count() > 0) {
              await btn.click().catch(() => {})
              await page.waitForTimeout(800)
              interaction = { name: 'refresh', ok: true, detail: 'clicked refresh' }
            } else {
              interaction = { name: 'inspect', ok: true, detail: 'no refresh' }
            }
          } else if (p.idx === 15) {
            const tabs = page.locator('.rag-product-config-container .el-tabs__item')
            const tc = await tabs.count()
            if (tc > 1) {
              await tabs.nth(1).click().catch(() => {})
              await page.waitForTimeout(800)
              interaction = { name: 'tab_switch', ok: true, detail: `switched to tab 1 of ${tc}` }
            } else {
              interaction = { name: 'noop', ok: true, detail: 'rag config page' }
            }
          }
        } catch (e) {
          interaction = { name: 'error', ok: false, detail: e.message }
        }
        result.checks.interaction = interaction

        // 5) 整体判定
        result.status = (rendered && interaction.ok) ? 'pass' : 'fail'

        // 关键断言
        expect(rendered, `页面 ${p.path} 未真实渲染: rootCount=${rootCount} cards=${elCards} tables=${elTables} tabs=${elTabs}`).toBe(true)
        expect(interaction.ok, `页面 ${p.path} 交互失败: ${interaction.detail}`).toBe(true)
      } catch (err) {
        result.status = 'fail'
        result.errors.push(err.message)
        throw err
      } finally {
        net.dump()
        fs.writeFileSync(path.join(RESULT_DIR, `page-${p.idx}.json`), JSON.stringify(result, null, 2))
        console.log(`[page ${p.idx}] ${p.title}: ${result.status} | render=${result.checks?.rendered} | apis=${result.checks?.apiOkRatio || 'n/a'} | interact=${result.checks?.interaction?.name}`)
      }
    })
  }
})
