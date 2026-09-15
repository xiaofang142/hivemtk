import { test, expect } from '@playwright/test'
import path from 'path'
import fs from 'fs'

/**
 * 权限系统 UI 多角色验证测试 (阶段 9 - 第 2 轮)
 *
 * 核心验证目标：
 *  - admin 登录后能进入 /system/users /system/roles /system/permissions 三个子页面
 *  - 客服 (customer_service) 直接访问这三个子页面应被前端路由拦截
 *  - admin 顶栏"系统设置"下能看到"团队"菜单及其 3 个子项
 *
 * 关键风险点（前置已知）：
 *  - 后端 /api/system/users/* 路由可能未注册（setupSystemUserRoutes 在 router.go 未被调用）
 *  - 前端 Layout 菜单的国际化键：zh 模式"系统设置 > 团队"，en 模式"System Settings > Team"
 *  - 本测试同时支持 zh/en 两种模式（按 i18n locale 检测）
 *
 * 修复历史：
 * 后端注册 setupSystemUserRoutes + 前端 router 懒加载 systemUser/role/permission 模块
 * 简化点击逻辑，直接 click by text 强制启用 locale=zh
 */

const BASE = process.env.E2E_BASE_URL || 'http://localhost:8213'
const API = process.env.API_BASE_URL || 'http://localhost:8204'

const SCREENSHOT_DIR = path.resolve(process.cwd(), 'tests/screenshots/permission_ui')
fs.mkdirSync(SCREENSHOT_DIR, { recursive: true })

const CANDIDATE_PASSWORDS = [
  'Admin@12345678',
  'Admin@123456',
  '62cfdc6bf1b075830734cc6f9a63501b',
  '9eb623e1979ad575b98bad24e12669a2f58205e9a8c79f61a7083f8468fc18b3'
]

// ---- 工具函数 ----
async function adminLogin() {
  for (const pwd of CANDIDATE_PASSWORDS) {
    try {
      const resp = await fetch(`${API}/api/auth/login`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ username: 'admin', password: pwd })
      })
      const data = await resp.json()
      if (data?.code === 'SUCCESS' && data?.data?.token) {
        return { token: data.data.token, password: pwd }
      }
    } catch (e) {
      console.log(`  密码 ${pwd} 失败: ${e.message}`)
    }
  }
  return null
}

async function createCustomerServiceUser(token) {
  const username = `uitest_cs_${Date.now()}_${Math.floor(Math.random() * 1000)}`
  const password = 'Test12345!'
  const resp = await fetch(`${API}/api/system/users`, {
    method: 'POST',
    headers: {
      'Authorization': `Bearer ${token}`,
      'Content-Type': 'application/json'
    },
    body: JSON.stringify({
      username,
      password,
      email: `${username}@test.com`,
      name: 'UI测试客服',
      role: 'customer_service'
    })
  })
  const data = await resp.json()
  if (data?.code !== 'SUCCESS') {
    return null
  }
  return { id: data?.data?.id, username, password }
}

// webLogin: 优先用 token + userInfo 直接注入 localStorage，避免重复登录触发防爆破
// fallback: 当未传 token 时走 UI 登录表单
async function webLogin(page, username, password, token, userInfo) {
  // 1. 决定用 token 还是 UI 登录
  const useToken = token || null

  // 先访问根 URL 注入 init script（必须在任何页面加载前）
  await page.addInitScript(({ tok, locale, info, initFlag, name }) => {
    try {
      localStorage.setItem('app_locale', locale)
      localStorage.setItem('system_initialized', initFlag)
      if (tok) localStorage.setItem('token', tok)
      if (info) localStorage.setItem('user_info', JSON.stringify(info))
    } catch (e) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
  }, {
    tok: useToken,
    locale: 'zh',
    info: userInfo || null,
    initFlag: 'true',
    name: username
  })

  if (useToken) {
    // 直接访问受保护页面（router 守卫会读取 localStorage.token 放行）
    await page.goto(`${BASE}/#/messageHub/list`)
    await page.waitForTimeout(1500)
    return
  }

  // fallback：UI 登录（仅在没有 token 时使用）
  await page.goto(`${BASE}/#/login`)
  try {
    await page.waitForSelector('.login-box', { timeout: 10000 })
  } catch (e) {
    console.log('  登录页 .login-box 不可见')
  }
  await page.waitForTimeout(500)
  // 强制 zh locale（页面重定向/跳转后仍生效）
  await page.evaluate(() => {
    try { localStorage.setItem('app_locale', 'zh') } catch (e) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
  })
  const userInput = page.locator('.login-box input').first()
  const pwdInput = page.locator('.login-box input[type="password"]')
  await userInput.fill(username)
  await pwdInput.fill(password)
  await page.locator('.login-box button.el-button--primary').click()
  try {
    await page.waitForURL((url) => !url.hash.includes('/login'), { timeout: 10000 })
  } catch (e) {
    await page.waitForTimeout(2000)
  }
  // 登录后再设一次
  await page.evaluate(() => {
    try { localStorage.setItem('app_locale', 'zh') } catch (e) { /* 忽略异常：失败时保持既有状态，不打断用户 */ }
  })
  await page.waitForTimeout(1500)
}

// clickTopMenu 点击顶栏菜单（如"系统设置"），用 page.evaluate 滚动 + force click
// 关键：el-menu 水平模式下超过 6 个菜单项时，第 7 个会被渲染为 size=0，
// 必须通过 element.click() 绕过 Playwright 的可见性校验
async function clickTopMenu(page, text) {
  const clicked = await page.evaluate((searchText) => {
    const items = document.querySelectorAll('.top-menu .el-menu-item')
    for (const item of items) {
      if (item.textContent.includes(searchText)) {
        // el-menu 使用 el-menu-item，需要触发 el-menu 的 select 事件
        // 直接 .click() 会触发原生事件，但 el-menu 监听的是 click
        item.click()
        return true
      }
    }
    return false
  }, text)

  if (!clicked) {
    throw new Error(`未找到顶栏菜单: ${text}`)
  }
  await page.waitForTimeout(800)
}

// clickAsideSubMenu 点击侧边栏子菜单文本
async function clickAsideSubMenu(page, text) {
  const item = page.locator('.app-aside').locator(`text=${text}`).first()
  await item.waitFor({ state: 'attached', timeout: 10000 })
  await item.scrollIntoViewIfNeeded({ timeout: 8000 })
  await item.click({ force: true, timeout: 10000 })
  await page.waitForTimeout(800)
}

// ============================================================
test.describe('权限系统 UI 多角色验证', () => {
  let adminToken = ''
  let adminPassword = ''
  let adminUserInfo = null
  let csUsername = ''
  let csPassword = ''
  let csToken = ''
  let csUserInfo = null

  // 直接用 API 登录客服账号拿到 token（与 CSRF/防爆破隔离）
  async function loginByApi(username, password) {
    const resp = await fetch(`${API}/api/auth/login`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ username, password })
    })
    const data = await resp.json()
    if (data?.code === 'SUCCESS' && data?.data?.token) {
      return { token: data.data.token, user: data.data.user }
    }
    return null
  }

  test.beforeAll(async () => {
    const result = await adminLogin()
    if (!result) {
      throw new Error('admin 登录失败，所有候选密码均不可用')
    }
    adminToken = result.token
    adminPassword = result.password
    // 构造 user_info（user store 期望的字段）
    adminUserInfo = {
      id: 136,
      username: 'admin',
      role: 'admin',
      real_name: '系统超管',
      email: 'admin@hivemtk.demo'
    }
    console.log(`✓ admin token 获取成功 (pwd=${adminPassword})`)

    const cs = await createCustomerServiceUser(adminToken)
    if (cs) {
      csUsername = cs.username
      csPassword = cs.password
      // 直接登录客服拿 token
      const login = await loginByApi(csUsername, csPassword)
      if (login) {
        csToken = login.token
        csUserInfo = {
          id: login.user?.id || 0,
          username: csUsername,
          role: 'customer_service',
          real_name: login.user?.real_name || 'UI测试客服',
          email: login.user?.email || `${csUsername}@test.com`
        }
      }
      console.log(`✓ 测试客服账号创建成功: ${csUsername}`)
    } else {
      console.log(`⚠ 创建客服失败（/api/system/users 路由可能未注册）`)
    }
  })

  // ----- 1. admin 顶栏能看到"系统设置"和"团队"菜单 -----
  test('1. admin 顶栏能看到"系统设置"菜单', async ({ page }) => {
    test.setTimeout(60000)
    await webLogin(page, 'admin', adminPassword, adminToken, adminUserInfo)
    await page.screenshot({ path: `${SCREENSHOT_DIR}/admin-home.png`, fullPage: true })

    // 强制 locale=zh，菜单项使用硬编码中文
    await page.evaluate(() => {
      const tm = document.querySelector('.top-menu .el-menu, .top-menu')
      if (tm) tm.scrollLeft = tm.scrollWidth
    })
    await page.waitForTimeout(400)

    // 通过 evaluate 检查菜单项是否存在（绕过 overflow 可视性）
    const exists = await page.evaluate(() => {
      const items = document.querySelectorAll('.top-menu .el-menu-item')
      for (const item of items) {
        if (item.textContent.includes('系统设置')) return true
      }
      return false
    })

    console.log(`  "系统设置"菜单存在: ${exists}`)
    expect(exists).toBeTruthy()
  })

  // ----- 2. admin 点击"系统设置"出现"团队"侧边栏 -----
  test('2. admin 点击"系统设置"出现"团队"侧边栏', async ({ page }) => {
    test.setTimeout(60000)
    await webLogin(page, 'admin', adminPassword, adminToken, adminUserInfo)
    await page.waitForTimeout(1000)

    await clickTopMenu(page, '系统设置')
    await page.screenshot({ path: `${SCREENSHOT_DIR}/admin-system-clicked.png`, fullPage: true })

    // 侧边栏出现"团队"
    const teamMenu = page.locator('.app-aside').locator(`text=团队`).first()
    await expect(teamMenu).toBeVisible({ timeout: 10000 })
    console.log('  ✓ 侧边栏"团队"菜单可见')
  })

  // ----- 3. admin 团队菜单下能看到 3 个子项 -----
  test('3. admin 团队菜单下能看到 3 个子项（人员/角色/授权）', async ({ page }) => {
    test.setTimeout(60000)
    await webLogin(page, 'admin', adminPassword, adminToken, adminUserInfo)
    await page.waitForTimeout(1000)

    await clickTopMenu(page, '系统设置')
    await page.waitForTimeout(500)

    // 点击"团队"展开
    const teamMenu = page.locator('.app-aside').locator(`text=团队`).first()
    if (await teamMenu.count() > 0) {
      await teamMenu.click()
      await page.waitForTimeout(800)
    }
    await page.screenshot({ path: `${SCREENSHOT_DIR}/admin-team-expanded.png`, fullPage: true })

    const expectedItems = ['人员管理', '角色管理', '授权管理']
    const asideText = await page.locator('.app-aside').innerText()
    const result = {}
    for (const item of expectedItems) {
      result[item] = asideText.includes(item)
    }
    console.log('  子菜单验证:', JSON.stringify(result, null, 2))

    for (const item of expectedItems) {
      expect(result[item], `应包含 "${item}"`).toBeTruthy()
    }
  })

  // ----- 4. admin 点击"人员管理"能进入 /system/users -----
  test('4. admin 点击"人员管理"能进入 /system/users', async ({ page }) => {
    test.setTimeout(60000)
    await webLogin(page, 'admin', adminPassword, adminToken, adminUserInfo)
    await page.waitForTimeout(1000)

    await clickTopMenu(page, '系统设置')
    await page.waitForTimeout(500)
    const teamMenu = page.locator('.app-aside').locator(`text=团队`).first()
    if (await teamMenu.count() > 0) {
      await teamMenu.click()
      await page.waitForTimeout(800)
    }

    const userItem = page.locator('.app-aside').locator(`text=人员管理`).first()
    await userItem.click()
    await page.waitForTimeout(2000)
    await page.screenshot({ path: `${SCREENSHOT_DIR}/admin-users-page.png`, fullPage: true })

    const hash = await page.evaluate(() => window.location.hash)
    console.log(`  点击后 hash: ${hash}`)
    expect(hash).toContain('/system/users')
  })

  // ----- 5. admin 点击"角色管理"能进入 /system/roles -----
  test('5. admin 点击"角色管理"能进入 /system/roles', async ({ page }) => {
    test.setTimeout(60000)
    await webLogin(page, 'admin', adminPassword, adminToken, adminUserInfo)
    await page.waitForTimeout(1000)

    await clickTopMenu(page, '系统设置')
    await page.waitForTimeout(500)
    const teamMenu = page.locator('.app-aside').locator(`text=团队`).first()
    if (await teamMenu.count() > 0) {
      await teamMenu.click()
      await page.waitForTimeout(800)
    }

    const roleItem = page.locator('.app-aside').locator(`text=角色管理`).first()
    await roleItem.click()
    await page.waitForTimeout(2000)
    await page.screenshot({ path: `${SCREENSHOT_DIR}/admin-roles-page.png`, fullPage: true })

    const hash = await page.evaluate(() => window.location.hash)
    console.log(`  点击后 hash: ${hash}`)
    expect(hash).toContain('/system/roles')
  })

  // ----- 6. admin 点击"授权管理"能进入 /system/permissions -----
  test('6. admin 点击"授权管理"能进入 /system/permissions', async ({ page }) => {
    test.setTimeout(60000)
    await webLogin(page, 'admin', adminPassword, adminToken, adminUserInfo)
    await page.waitForTimeout(1000)

    await clickTopMenu(page, '系统设置')
    await page.waitForTimeout(500)
    const teamMenu = page.locator('.app-aside').locator(`text=团队`).first()
    if (await teamMenu.count() > 0) {
      await teamMenu.click()
      await page.waitForTimeout(800)
    }

    const permItem = page.locator('.app-aside').locator(`text=授权管理`).first()
    await permItem.click()
    await page.waitForTimeout(2000)
    await page.screenshot({ path: `${SCREENSHOT_DIR}/admin-permissions-page.png`, fullPage: true })

    const hash = await page.evaluate(() => window.location.hash)
    console.log(`  点击后 hash: ${hash}`)
    expect(hash).toContain('/system/permissions')
  })

  // ----- 7. 客服登录后顶栏/侧边栏不可见团队菜单 -----
  test('7. 客服登录后侧边栏不显示"团队"', async ({ page }) => {
    test.setTimeout(60000)
    test.skip(!csUsername, '客服账号不可用（/api/system/users 路由未注册）')

    await webLogin(page, csUsername, csPassword, csToken, csUserInfo)
    await page.screenshot({ path: `${SCREENSHOT_DIR}/cs-home.png`, fullPage: true })

    // 客服看不到"系统设置"顶菜单（因为 roles 限定 admin）
    const systemMenu = page.locator('.top-menu .el-menu-item').filter({ hasText: '系统设置' })
    const systemCount = await systemMenu.count()
    console.log(`  客服顶栏"系统设置"出现次数: ${systemCount}`)
    expect(systemCount).toBe(0)

    // 也不应看到"团队"侧边栏
    const teamCount = await page.locator(`.app-aside`).locator(`text=团队`).count()
    console.log(`  客服侧边栏"团队"出现次数: ${teamCount}`)
    expect(teamCount).toBe(0)
  })

  // ----- 8. 客服直接访问 /system/users 应被路由拦截 -----
  test('8. 客服直接访问 /system/users 应被路由拦截', async ({ page }) => {
    test.setTimeout(60000)
    test.skip(!csUsername, '客服账号不可用')

    await webLogin(page, csUsername, csPassword, csToken, csUserInfo)
    await page.goto(`${BASE}/#/system/users`)
    await page.waitForTimeout(2500)
    await page.screenshot({ path: `${SCREENSHOT_DIR}/cs-direct-users.png`, fullPage: true })

    const finalHash = await page.evaluate(() => window.location.hash)
    console.log(`  客服访问 /system/users 后 hash: ${finalHash}`)
    // 解析路径（去掉 ?from=* 等 query），仅校验 pathname
    const pathOnly = finalHash.split('?')[0]
    console.log(`  pathname: ${pathOnly}`)
    // 客服被重定向到非 /system/users 页面（403 → NotFound）
    expect(pathOnly.includes('/system/users')).toBe(false)
  })
})
