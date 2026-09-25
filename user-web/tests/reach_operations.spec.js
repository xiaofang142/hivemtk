import { test, expect } from '@playwright/test'
import path from 'path'

const BASE = process.env.E2E_BASE_URL || 'http://localhost:8211'
const STATE = path.resolve(process.cwd(), 'tests/.auth/user.json')

// 触达运营（reach）全量页面清单（对应 docs/REACH_OPERATIONS_CHECKLIST.md）
const PAGES = [
  // 邮件触达
  { group: '邮件触达', name: '邮件列表', path: '/email' },
  { group: '邮件触达', name: '我的草稿', path: '/email/drafts' },
  { group: '邮件触达', name: '我的任务', path: '/email/jobs' },
  { group: '邮件触达', name: '邮件账号', path: '/email/smtp' },
  { group: '邮件触达', name: '邮件代理', path: '/email/info' },
  { group: '邮件触达', name: '邮件使用指南', path: '/email/guide' },
  // 短信触达
  { group: '短信触达', name: '短信列表', path: '/sms/list' },
  { group: '短信触达', name: '短信草稿', path: '/sms/drafts' },
  { group: '短信触达', name: '短信任务', path: '/sms/jobs' },
  { group: '短信触达', name: '短信配置', path: '/sms/config' },
  // 抖音
  { group: '抖音', name: '抖音卡片', path: '/douyinCard' },
  { group: '抖音', name: '抖音统计', path: '/douyin/stats' },
  // 快手
  { group: '快手', name: '快手卡片', path: '/kuaishouCard' },
  { group: '快手', name: '快手统计', path: '/kuaishou/stats' },
  // 小红书
  { group: '小红书', name: '小红书卡片', path: '/xiaohongshuCard' },
  { group: '小红书', name: '小红书统计', path: '/xiaohongshu/stats' },
  // 闲鱼
  { group: '闲鱼', name: '闲鱼卡片', path: '/xianyuCard' },
  { group: '闲鱼', name: '闲鱼统计', path: '/xianyu/stats' },
  // TikTok
  { group: 'TikTok', name: 'TikTok卡片', path: '/tiktok/list' },
  { group: 'TikTok', name: 'TikTok统计', path: '/tiktok/stats' },
  // WhatsApp
  { group: 'WhatsApp', name: '账号管理', path: '/whatsapp/account' },
  { group: 'WhatsApp', name: '草稿箱', path: '/whatsapp/drafts' },
  { group: 'WhatsApp', name: '群发', path: '/whatsapp/jobs' },
  // 电报社群
  { group: '电报社群', name: '机器人', path: '/telegram/account' },
  // 飞书
  { group: '飞书', name: '飞书账号', path: '/feishu/account' },
  // 社群管理
  { group: '社群管理', name: '社群管理', path: '/community/list' },
  // 短链管理
  { group: '短链管理', name: '短链列表', path: '/shortLink' },
  { group: '短链管理', name: '短链统计', path: '/shortLink/stats' },
  // 活码管理
  { group: '活码管理', name: '活码管理', path: '/livecode' },
]

test.use({ baseURL: BASE, storageState: STATE })

// ===== 全量页面冒烟覆盖（100% 页面渲染 + 控制台/接口错误捕获）=====
for (const p of PAGES) {
  test(`[冒烟] ${p.group} / ${p.name} (${p.path})`, async ({ page }) => {
    const errors = []
    const failed = []
    page.on('console', (m) => { if (m.type() === 'error') errors.push(m.text()) })
    page.on('pageerror', (e) => errors.push('PAGEERROR: ' + e.message))
    page.on('response', (res) => {
      const u = res.url()
      if (u.includes('/api/') && res.status() >= 500) failed.push(res.status() + ' ' + u)
    })

    await page.goto('/#' + p.path)
    await page.waitForTimeout(1200)

    // 1) 认证未失效（未被踢回登录页）
    expect(page.url(), '被重定向到登录页，认证失败').not.toContain('/login')

    // 2) 主内容区已渲染（非白屏）
    const main = page.locator('#app .el-main, #app main, #app .app-main').first()
    await expect(main, '主内容区不可见（可能白屏）').toBeVisible()

    // 3) 过滤掉无关的浏览器资源告警 + 限流(429)抖动 + CSP meta 提示，仅保留真实错误
    const real = errors.filter((e) =>
      !/favicon|ERR_|Failed to load resource.*(woff|ttf|png|svg)|Download the Vue Devtools/i.test(e) &&
      !/429|Too Many Requests|请求过于频繁|rate limit|RateLimit|Network Error|net::ERR/i.test(e) &&
      !/Content Security Policy|frame-ancestors|CSP/i.test(e)
    )
    expect(real, '控制台错误:\n' + real.join('\n')).toEqual([])
    expect(failed, 'API 5xx:\n' + failed.join('\n')).toEqual([])
  })
}
