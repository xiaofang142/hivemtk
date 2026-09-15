/**
 * UX 旅程走查脚本（真实用户视角）
 * 用法: node scripts/ux_audit_journey.mjs [journey1|journey2|journey3|all]
 * 输出: test-results/ux-audit/ 下 JSON 结果 + 截图
 */
import { chromium } from 'playwright'
import fs from 'node:fs'
import path from 'node:path'

const BASE = process.env.E2E_BASE_URL || 'http://localhost:8211'
const OUT_DIR = path.resolve('test-results/ux-audit')
fs.mkdirSync(OUT_DIR, { recursive: true })

const journeyArg = process.argv[2] || 'all'

// ---------- 采集器 ----------
function makeCollector() {
  return {
    consoleErrors: [],
    consoleWarnings: [],
    apiFailures: [],
    toasts: [],
    steps: []
  }
}

function attachPage(page, c) {
  page.on('console', (msg) => {
    const text = msg.text()
    if (msg.type() === 'error') {
      if (/ERR_CONNECTION_CLOSED|ERR_FAILED.*img\.example/.test(text)) return // 已知噪音
      c.consoleErrors.push(text.slice(0, 500))
    } else if (msg.type() === 'warning') {
      if (/CSP|Vue warn.*extraneous|Download the Vue Devtools|\[intlify\]/.test(text)) return
      c.consoleWarnings.push(text.slice(0, 300))
    }
  })
  page.on('response', (resp) => {
    if (resp.url().includes('/api/') && resp.status() >= 400) {
      c.apiFailures.push({ status: resp.status(), url: resp.url().replace(BASE, ''), method: resp.request().method() })
    }
  })
  page.on('pageerror', (err) => {
    c.consoleErrors.push('PAGEERROR: ' + String(err).slice(0, 500))
  })
}

async function snap(page, name) {
  await page.screenshot({ path: path.join(OUT_DIR, name + '.png'), fullPage: false })
}

async function recordToasts(page, c) {
  try {
    const els = await page.locator('.el-message').allInnerTexts({ timeout: 500 })
    for (const t of els) if (!c.toasts.includes(t)) c.toasts.push(t)
  } catch { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
}

async function step(c, name, fn) {
  const entry = { name, ok: true, notes: [] }
  c.steps.push(entry)
  try {
    await fn((n) => entry.notes.push(n))
  } catch (e) {
    entry.ok = false
    entry.notes.push('FAIL: ' + String(e).slice(0, 400))
  }
  console.log(`[${entry.ok ? 'OK' : 'FAIL'}] ${name}${entry.notes.length ? ' | ' + entry.notes.join(' ; ') : ''}`)
}

async function login(page) {
  await page.goto(BASE + '/login', { waitUntil: 'networkidle' })
  await page.fill('input[placeholder*="用户名"], input[type="text"]', 'admin')
  await page.fill('input[type="password"]', 'Test@123456')
  await page.click('button:has-text("登"), button.login-btn, .el-button--primary')
  await page.waitForURL(/(?!.*login)/, { timeout: 15000 })
  await page.waitForTimeout(1500)
}

async function hasEmptyState(page) {
  const empty = await page.locator('.el-empty').count()
  const tableEmptyText = await page.locator('.el-table__empty-text').count()
  return { elEmpty: empty > 0, tableEmpty: tableEmptyText > 0 }
}

async function gotoMenu(page, path, waitMs = 2500) {
  await page.goto(BASE + '/#' + path)
  await page.waitForTimeout(waitMs)
}

// ---------- Journey 1: 登录 + 空状态引导 ----------
async function journey1() {
  const c = makeCollector()
  const browser = await chromium.launch()
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
  attachPage(page, c)

  await step(c, 'J1-登录页可访问且表单完整', async () => {
    await page.goto(BASE + '/login', { waitUntil: 'networkidle' })
    const u = await page.locator('input[type="text"], input[placeholder*="用户"]').count()
    const p = await page.locator('input[type="password"]').count()
    if (!u || !p) throw new Error('登录表单字段缺失')
    await snap(page, 'j1_login')
  })

  await step(c, 'J1-登录成功进入系统', async () => {
    await login(page)
    await snap(page, 'j1_after_login')
  })

  // 空状态检查核心页面
  const pages = [
    ['/aiAgent/list', 'AI智能体列表'],
    ['/knowledgeBase', '知识库列表'],
    ['/unifiedInbox', '统一收件箱'],
    ['/sopTemplate/list', 'SOP模板列表'],
    ['/reachPipeline', '触达计划'],
    ['/dashboard', '工作台']
  ]
  for (const [p, label] of pages) {
    await step(c, `J1-空状态检查: ${label} (${p})`, async (note) => {
      await gotoMenu(page, p)
      const es = await hasEmptyState(page)
      note(`el-empty=${es.elEmpty} tableEmpty=${es.tableEmpty}`)
      await snap(page, `j1_empty_${p.replace(/\//g, '_')}`)
    })
  }

  await browser.close()
  return c
}

// ---------- Journey 2: AI智能体→知识库→上传→对话 ----------
async function journey2() {
  const c = makeCollector()
  const browser = await chromium.launch()
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
  attachPage(page, c)
  await login(page)

  await step(c, 'J2-打开智能体创建页', async () => {
    await gotoMenu(page, '/aiAgent/create')
    await snap(page, 'j2_agent_create')
    const form = await page.locator('form, .el-form').count()
    if (!form) throw new Error('创建页无表单')
  })

  await step(c, 'J2-填写并提交创建智能体', async (note) => {
    // 尝试填名称等必填字段
    const inputs = page.locator('.el-form input:not([type="hidden"])')
    const n = await inputs.count()
    note(`表单输入框 ${n} 个`)
    // 填第一个可见文本框（一般是名称）
    const firstInput = page.locator('.el-form input:visible').first()
    await firstInput.fill('UX审计测试智能体' + Date.now())
    await snap(page, 'j2_agent_form_filled')
    // 找提交按钮
    const btn = page.locator('button:has-text("保存"), button:has-text("创建"), button:has-text("提交")').first()
    await btn.click()
    await page.waitForTimeout(2500)
    await recordToasts(page, c)
    await snap(page, 'j2_agent_after_submit')
    note('当前URL: ' + page.url())
  })

  // 知识库
  await step(c, 'J2-知识库列表页', async () => {
    await gotoMenu(page, '/knowledgeBase')
    await snap(page, 'j2_kb_list')
  })

  await step(c, 'J2-创建知识库', async (note) => {
    const btn = page.locator('button:has-text("新建"), button:has-text("创建"), button:has-text("新增")').first()
    if ((await btn.count()) === 0) throw new Error('找不到创建知识库入口')
    await btn.click()
    await page.waitForTimeout(1200)
    await snap(page, 'j2_kb_create_dialog')
    // 弹窗内填写名称
    const dlgInput = page.locator('.el-dialog input:visible, .el-drawer input:visible').first()
    if (await dlgInput.count()) {
      await dlgInput.fill('UX审计知识库' + Date.now())
    }
    const confirmBtn = page.locator('.el-dialog button:has-text("确定"), .el-dialog button:has-text("保存"), .el-dialog button:has-text("创建"), .el-drawer button:has-text("确定"), .el-drawer button:has-text("保存")').last()
    await confirmBtn.click()
    await page.waitForTimeout(2000)
    await recordToasts(page, c)
    await snap(page, 'j2_kb_after_create')
    note('当前URL: ' + page.url())
  })

  // 知识上传：找知识库详情/文档入口
  await step(c, 'J2-知识库详情与上传入口', async (note) => {
    await gotoMenu(page, '/knowledgeBase')
    await page.waitForTimeout(1500)
    // 点击第一行的"文档"或行本身
    const docBtn = page.locator('button:has-text("文档"), button:has-text("管理"), button:has-text("查看")').first()
    if (await docBtn.count()) {
      await docBtn.click()
      await page.waitForTimeout(2000)
    }
    await snap(page, 'j2_kb_detail')
    note('当前URL: ' + page.url())
    const upBtn = await page.locator('button:has-text("上传"), button:has-text("导入")').count()
    note(`上传/导入按钮数: ${upBtn}`)
  })

  // 对话测试（回到智能体列表，用测试弹窗）
  await step(c, 'J2-智能体对话测试', async (note) => {
    await gotoMenu(page, '/aiAgent/list')
    await page.waitForTimeout(1500)
    const testBtn = page.locator('button:has-text("测试")').first()
    if (!(await testBtn.count())) throw new Error('找不到测试按钮（无智能体数据?）')
    await testBtn.click()
    await page.waitForTimeout(1000)
    await page.locator('.el-dialog textarea:visible').first().fill('你好，请介绍一下你们的产品')
    await snap(page, 'j2_chat_dialog')
    await page.locator('.el-dialog button:has-text("执行测试")').click()
    await page.waitForTimeout(8000)
    await recordToasts(page, c)
    await snap(page, 'j2_chat_result')
  })

  await browser.close()
  return c
}

// ---------- Journey 3: 收件箱/SOP模板/触达计划 ----------
async function journey3() {
  const c = makeCollector()
  const browser = await chromium.launch()
  const page = await browser.newPage({ viewport: { width: 1440, height: 900 } })
  attachPage(page, c)
  await login(page)

  await step(c, 'J3-统一收件箱查看会话', async (note) => {
    await gotoMenu(page, '/unifiedInbox', 3500)
    await snap(page, 'j3_inbox')
    const convItems = await page.locator('.conversation-item, .conv-item, [class*=session], .el-table__row').count()
    note(`会话条目类元素: ${convItems}`)
  })

  await step(c, 'J3-SOP模板创建页', async (note) => {
    await gotoMenu(page, '/sopTemplate/list')
    await snap(page, 'j3_sop_list')
    const createBtn = page.locator('button:has-text("新建"), button:has-text("创建"), button:has-text("新增")').first()
    if (await createBtn.count()) {
      await createBtn.click()
      await page.waitForTimeout(2000)
      await snap(page, 'j3_sop_editor')
      note('编辑页URL: ' + page.url())
    } else {
      note('列表页无创建按钮')
    }
  })

  await step(c, 'J3-SOP模板提交（若表单可填）', async (note) => {
    const saveBtn = page.locator('button:has-text("保存"), button:has-text("提交"), button:has-text("创建")').last()
    if (await saveBtn.count()) {
      await saveBtn.click()
      await page.waitForTimeout(2000)
      await recordToasts(page, c)
      await snap(page, 'j3_sop_submit_result')
    }
  })

  await step(c, 'J3-触达计划列表+创建', async (note) => {
    await gotoMenu(page, '/reachPipeline')
    await snap(page, 'j3_reach_list')
    const createBtn = page.locator('button:has-text("新建"), button:has-text("创建"), button:has-text("新增")').first()
    if (await createBtn.count()) {
      await createBtn.click()
      await page.waitForTimeout(2000)
      await snap(page, 'j3_reach_editor')
      note('编辑页URL: ' + page.url())
      const saveBtn = page.locator('button:has-text("保存"), button:has-text("提交")').last()
      if (await saveBtn.count()) {
        await saveBtn.click()
        await page.waitForTimeout(2000)
        await recordToasts(page, c)
        await snap(page, 'j3_reach_submit_result')
      }
    } else {
      note('触达计划页无创建按钮')
    }
  })

  await browser.close()
  return c
}

// ---------- main ----------
;(async () => {
  const results = {}
  if (journeyArg === 'journey1' || journeyArg === 'all') results.journey1 = await journey1()
  if (journeyArg === 'journey2' || journeyArg === 'all') results.journey2 = await journey2()
  if (journeyArg === 'journey3' || journeyArg === 'all') results.journey3 = await journey3()

  const outPath = path.join(OUT_DIR, `ux_audit_${Date.now()}.json`)
  fs.writeFileSync(outPath, JSON.stringify(results, null, 2))
  console.log('\n=== 汇总 ===')
  for (const [j, c] of Object.entries(results)) {
    const fails = c.steps.filter((s) => !s.ok)
    console.log(`${j}: ${c.steps.length - fails.length}/${c.steps.length} 步通过, console错误=${c.consoleErrors.length}, API失败=${c.apiFailures.length}, toasts=${JSON.stringify(c.toasts)}`)
    if (fails.length) fails.forEach((f) => console.log('  FAIL:', f.name))
    if (c.apiFailures.length) console.log('  API:', JSON.stringify(c.apiFailures))
    if (c.consoleErrors.length) console.log('  CErr:', c.consoleErrors.slice(0, 5))
  }
  console.log('结果已写入', outPath)
})()
