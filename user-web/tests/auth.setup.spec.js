import { test, expect } from '@playwright/test'
import fs from 'fs'
import path from 'path'

const BASE = process.env.E2E_BASE_URL || 'http://localhost:8213'
const STATE = path.resolve(process.cwd(), 'tests/.auth/user.json')
const ENV_FILE = process.env.HIVEMTK_ENV_FILE || path.resolve(process.cwd(), '../.env')

// 没显式给环境变量时，从仓根 .env 现取第一条 SEED_PASSWORD —— 口径同 scripts/rotate-admin-password.sh 的
// env_get：同一个键名可以出现多次（实测 136 个键行只有 71 个不同键名），故取**首条**；
// 只取这一个键，不 source 整个文件（带空格的值会打断 shell 解析），也不把这个值印出来。
// HIVEMTK_ENV_FILE 是给反向测留的注入口，平时不用设。
function seedFromEnvFile() {
  let first = ''
  try {
    for (const line of fs.readFileSync(ENV_FILE, 'utf8').split('\n')) {
      if (line.startsWith('SEED_PASSWORD=')) {
        first = line.slice('SEED_PASSWORD='.length).replace(/\r$/, '').trim()
        break
      }
    }
  } catch {
    // 干净克隆 / CI 没有 .env 属正常档，不是错误
  }
  return first
}

// 口令链与 docs/DEPLOYMENT_GUIDE.md §6.2 同口径：显式入参 > 环境 > .env 现值 > 历史重置值，
// 逐一尝试直到登录成功。上一版注释承诺的 `PLATFORM_ADMIN_PASSWORD` 根本没进数组、也没有 SEED_PASSWORD
// 入口 ⇒ 凡是跑过 scripts/rotate-admin-password.sh 的实例，这一格必然"所有候选密码均无法登录"。
const CANDIDATES = [
  process.env.SEED_PASSWORD,
  seedFromEnvFile(),
  process.env.PLATFORM_ADMIN_PASSWORD,
  'Admin@12345678',
  'Admin@123456',
  '62cfdc6bf1b075830734cc6f9a63501b'
].filter((pw, i, arr) => typeof pw === 'string' && pw.trim() !== '' && arr.indexOf(pw) === i)

test.use({ baseURL: BASE })

test('authenticate admin and persist storage state', async ({ page }) => {
  await page.goto('/#/login')
  await page.waitForSelector('.login-box', { timeout: 15000 })

  let ok = false
  for (const pw of CANDIDATES) {
    await page.locator('.login-box input[type="text"]').first().fill('admin')
    await page.locator('.login-box input[type="password"]').fill(pw)
    await page.locator('.login-box button.el-button--primary').click()
    try {
      // 登录成功后 SPA 会离开 /login（hash 不再含 /login）
      await page.waitForURL((url) => !url.hash.includes('/login'), { timeout: 6000 })
      ok = true
      break
    } catch (e) {
      await page.waitForTimeout(400)
    }
  }
  expect(ok, '所有候选密码均无法登录，请检查管理员凭据').toBeTruthy()
  await page.context().storageState({ path: STATE })
})
