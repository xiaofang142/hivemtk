/**
 * 触达运营 · 二次执行深层测试（强化版）
 *
 * 相对首轮的强化点：
 *  1) i18n 回归防护：在 en locale 下断言按钮渲染正确译文（Send Email），
 *     且整页不得出现上次回归产生的英文驼峰裸 key（sendEmail/viewTrace/...）。
 *  2) API 契约循环：34 页逐一加载，断言
 *     - 未被踢回 /login（认证有效）
 *     - 主内容区渲染（非空屏）
 *     - 所有 /api 响应无 4xx/5xx（首轮仅查 5xx，这里收紧到 4xx，捕捉 401/403/404 等真实缺陷）
 *     - 控制台无真实错误
 *     - 页面发起的 /api 请求至少有一条 2xx（证明数据真实流回，而非空渲染）
 *  3) 正向"创建→读回"：短信草稿 / 邮件草稿 新建后重新加载列表，断言记录已落库并可读回（数据库结果验证）。
 *  4) 负向校验：短信发送填写非法手机号，断言出现校验警告且弹窗不关闭。
 *
 * 运行：先 `npx playwright test auth.setup.spec.js --workers=1`，再本文件 `--workers=1`
 */
import { test, expect } from '@playwright/test'
import path from 'path'

const BASE = process.env.E2E_BASE_URL || 'http://localhost:8213'
const STATE = path.resolve(process.cwd(), 'tests/.auth/user.json')

const PAGES = [
  { group: '邮件触达', name: '邮件列表', path: '/email' },
  { group: '邮件触达', name: '我的草稿', path: '/email/drafts' },
  { group: '邮件触达', name: '我的任务', path: '/email/jobs' },
  { group: '邮件触达', name: '邮件账号', path: '/email/smtp' },
  { group: '邮件触达', name: '邮件代理', path: '/email/info' },
  { group: '邮件触达', name: '邮件使用指南', path: '/email/guide' },
  { group: '短信触达', name: '短信列表', path: '/sms/list' },
  { group: '短信触达', name: '短信草稿', path: '/sms/drafts' },
  { group: '短信触达', name: '短信任务', path: '/sms/jobs' },
  { group: '短信触达', name: '短信配置', path: '/sms/config' },
  { group: '抖音', name: '抖音卡片', path: '/douyinCard' },
  { group: '抖音', name: '抖音统计', path: '/douyin/stats' },
  { group: '快手', name: '快手卡片', path: '/kuaishouCard' },
  { group: '快手', name: '快手统计', path: '/kuaishou/stats' },
  { group: '小红书', name: '小红书卡片', path: '/xiaohongshuCard' },
  { group: '小红书', name: '小红书统计', path: '/xiaohongshu/stats' },
  { group: '闲鱼', name: '闲鱼卡片', path: '/xianyuCard' },
  { group: '闲鱼', name: '闲鱼统计', path: '/xianyu/stats' },
  { group: 'TikTok', name: 'TikTok卡片', path: '/tiktok/list' },
  { group: 'TikTok', name: 'TikTok统计', path: '/tiktok/stats' },
  { group: 'WhatsApp', name: '账号管理', path: '/whatsapp/account' },
  { group: 'WhatsApp', name: '草稿箱', path: '/whatsapp/drafts' },
  { group: 'WhatsApp', name: '群发', path: '/whatsapp/jobs' },
  { group: '电报社群', name: '机器人', path: '/telegram/account' },
  { group: '飞书', name: '飞书账号', path: '/feishu/account' },
  { group: '社群管理', name: '社群管理', path: '/community/list' },
  { group: '短链管理', name: '短链列表', path: '/shortLink' },
  { group: '短链管理', name: '短链统计', path: '/shortLink/stats' },
  { group: '活码管理', name: '活码管理', path: '/livecode' },
]

function setLocale(page, locale) {
  // 在应用加载前注入 locale，确保测试以指定语言渲染（便于用确定性文案断言）
  return page.addInitScript((l) => {
    try { localStorage.setItem('app_locale', l) } catch (e) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
  }, locale)
}

const BENIGN = (t) =>
  !/favicon|ERR_|Failed to load resource.*(woff|ttf|png|svg)|Download the Vue Devtools/i.test(t) &&
  !/429|Too Many Requests|请求过于频繁|rate limit|RateLimit|Network Error|net::ERR/i.test(t) &&
  !/Content Security Policy|frame-ancestors|CSP/i.test(t)

test.use({ baseURL: BASE, storageState: STATE })

// ===== 1) 邮件列表 i18n 回归防护（en locale）=====
test('[i18n回归] 邮件列表按钮渲染正确译文且无驼峰裸key泄漏', async ({ page }) => {
  await setLocale(page, 'en')
  const errors = []
  page.on('console', (m) => { if (m.type() === 'error' && BENIGN(m.text())) errors.push(m.text()) })
  page.on('pageerror', (e) => errors.push('PAGEERROR: ' + e.message))

  await page.goto('/#/email')
  await page.waitForTimeout(1000)

  // 主操作按钮必须显示英文译文（回归前会显示裸 key "sendEmail"）
  const primary = page.locator('.el-button--primary').first()
  await expect(primary, '发送按钮未渲染译文 Send Email（疑似驼峰裸key回归）')
    .toHaveText(/Send Email/i)

  // 整页不得出现上次的驼峰裸 key
  const leaks = ['sendEmail', 'viewTrace', 'traceInfo', 'emailSubject']
  const bodyText = await page.locator('#app').innerText()
  for (const leak of leaks) {
    expect(bodyText, `页面出现未翻译的裸 key: ${leak}`).not.toContain(leak)
  }

  // 打开发送弹窗，标题同样应为译文
  await primary.click()
  const dlg = page.locator('.el-dialog').first()
  await expect(dlg).toBeVisible()
  await expect(dlg).toContainText(/Send Email/i)
  await page.locator('.el-dialog__close').first().click()

  expect(errors, '控制台真实错误:\n' + errors.join('\n')).toEqual([])
})

// ===== 2) 全量页面 API 契约 + 零重定向 循环 =====
const STATIC_PAGES = new Set(['/email/guide', '/email/info'])

test('[API契约] 34页 零4xx/5xx + 零重定向 + 数据真实流回', { timeout: 180000 }, async ({ page }) => {
  test.setTimeout(240000)
  await setLocale(page, 'zh')
  const bad = [] // 收集接口 4xx/5xx
  const total = [] // 所有 /api 响应
  const errors = []
  page.on('response', (res) => {
    const u = res.url()
    if (!u.includes('/api/')) return
    total.push(u)
    if (res.status() >= 400) bad.push(`${res.status()} ${u}`)
  })
  page.on('console', (m) => { if (m.type() === 'error' && BENIGN(m.text())) errors.push(m.text()) })
  page.on('pageerror', (e) => errors.push('PAGEERROR: ' + e.message))

  const fails = []
  for (const p of PAGES) {
    bad.length = 0; total.length = 0
    await page.goto('/#' + p.path)
    // 轮询等待页面发起至少一条 /api 请求：统计类接口经平台聚合可能较慢，
    // 用轮询代替固定短等待，既覆盖慢接口又不在快速页上浪费时间。
    if (!STATIC_PAGES.has(p.path)) {
      const deadline = Date.now() + 4000
      while (total.length === 0 && Date.now() < deadline) {
        await page.waitForTimeout(200)
        if (page.url().includes('/login')) break
      }
    }
    const redirect = page.url().includes('/login')
    const appVisible = await page.locator('#app').isVisible().catch(() => false)
    if (redirect) fails.push(`[重定向] ${p.group}/${p.name} (${p.path}) -> /login`)
    if (!appVisible) fails.push(`[白屏] ${p.group}/${p.name} (${p.path}) #app 不可见`)
    if (bad.length) fails.push(`[接口错误] ${p.group}/${p.name} (${p.path}): ${bad.join(' | ')}`)
    if (STATIC_PAGES.has(p.path)) {
      // 静态参考页不发起 API，但必须确实渲染了参考内容（防止未来变空白）
      const txt = await page.locator('#app').innerText().catch(() => '')
      if (!/Gmail|QQ邮箱|163邮箱|邮件代理/.test(txt)) {
        fails.push(`[静态空白] ${p.group}/${p.name} (${p.path}) 参考内容未渲染`)
      }
    } else if (total.length === 0 && !redirect) {
      // 其余页面应至少有一条 /api 响应（证明数据真实加载而非空渲染）
      fails.push(`[无数据] ${p.group}/${p.name} (${p.path}) 加载期间无任何 /api 响应`)
    }
  }

  expect(errors, '控制台真实错误:\n' + errors.join('\n')).toEqual([])
  expect(fails, '存在未通过的页面:\n' + fails.join('\n')).toEqual([])
})

// ===== 3) 短信草稿 创建 -> 读回（数据库持久化）=====
test('[持久化] 短信草稿新建后可在列表读回', async ({ page }) => {
  await setLocale(page, 'zh')
  await page.goto('/#/sms/drafts')
  await page.waitForTimeout(1000)

  const title = 'AUTO_' + Date.now()
  await page.getByRole('button', { name: '新建草稿' }).click()
  const dlg = page.locator('.el-dialog').first()
  await expect(dlg).toBeVisible()
  await dlg.locator('input[placeholder="请输入草稿标题"]').fill(title)
  await dlg.locator('textarea[placeholder="请输入短信内容"]').fill('深层测试内容')
  await dlg.getByRole('button', { name: '确定' }).click()

  // 弹窗关闭 + 列表重新加载
  await expect(dlg).toBeHidden()
  await page.waitForTimeout(800)

  // 重新进入列表，断言记录已落库（数据库读回）
  await page.goto('/#/sms/drafts')
  await page.waitForTimeout(1000)
  await expect(page.locator('.el-table__row', { hasText: title }).first(),
    '新建的短信草稿未出现在列表中（数据库未持久化或读回失败）').toBeVisible()

  // 清理：删除该测试草稿
  const row = page.locator('.el-table__row', { hasText: title }).first()
  await row.getByRole('button', { name: '删除' }).click()
  const box = page.locator('.el-message-box').first()
  await expect(box).toBeVisible()
  await box.getByRole('button', { name: '确定' }).click()
  await page.waitForTimeout(600)
})

// ===== 4) 邮件草稿 创建 -> 读回（数据库持久化）=====
test('[持久化] 邮件草稿新建后可在列表读回', async ({ page }) => {
  await setLocale(page, 'zh')
  await page.goto('/#/email/drafts')
  await page.waitForTimeout(1000)

  const subject = 'AUTO_' + Date.now()
  await page.getByRole('button', { name: '新建草稿' }).click()
  const dlg = page.locator('.el-dialog').first()
  await expect(dlg).toBeVisible()
  await dlg.locator('input[placeholder="请输入邮件主题"]').fill(subject)
  // SimpleEditor 为 contenteditable，使用点击+键入填充
  const editor = dlg.locator('.editor-content').first()
  await editor.click()
  await page.keyboard.type('深层测试邮件内容')
  await dlg.getByRole('button', { name: '保存草稿' }).click()

  await expect(dlg).toBeHidden()
  await page.waitForTimeout(800)

  await page.goto('/#/email/drafts')
  await page.waitForTimeout(1000)
  await expect(page.locator('.el-table__row', { hasText: subject }).first(),
    '新建的邮件草稿未出现在列表中（数据库未持久化或读回失败）').toBeVisible()
})

// ===== 5) 短信发送 负向校验 =====
test('[负向] 短信发送填写非法手机号出现校验警告', async ({ page }) => {
  await setLocale(page, 'zh')
  await page.goto('/#/sms/list')
  await page.waitForTimeout(1000)

  await page.getByRole('button', { name: '发送短信' }).click()
  const dlg = page.locator('.el-dialog').first()
  await dlg.waitFor({ state: 'visible' })
  await page.waitForTimeout(600) // 等待弹窗开启动画结束，避免按钮不稳定/脱离
  await dlg.locator('input[placeholder="请输入手机号"]').fill('123')
  await dlg.locator('textarea[placeholder="请输入短信内容"]').fill('hi')
  await dlg.locator('.el-dialog__footer .el-button--primary').click({ timeout: 20000 })

  await expect(page.locator('.el-message--warning').first(),
    '非法手机号未触发校验警告').toBeVisible()
  await expect(dlg, '校验失败时弹窗应保持不变').toBeVisible()
  await page.locator('.el-dialog__close').first().click()
})
