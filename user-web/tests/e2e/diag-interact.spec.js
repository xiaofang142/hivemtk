import { print as _print } from '../../scripts/lib/stdout.cjs';
// 诊断脚本：针对全页面点击扫描中超时的交互按钮，精确判定是「设计性禁用/链接」还是「真 bug」。
import { test, expect as _expect } from '@playwright/test'

const BASE = process.env.E2E_BASE_URL || 'http://localhost:8211'
const ADMIN_USER = 'admin'
const ADMIN_PASS = process.env.E2E_ADMIN_PASSWORD
test.skip(!ADMIN_PASS, 'E2E_ADMIN_PASSWORD not set')

const CASES = [
  ['knowledge/batch-import', '确认导入 (0)'],
  ['knowledge/chunks', 'Refresh Segments'],
  ['knowledge/external', '提交导入'],
  ['llmRouting/list', '新增映射'],
  ['reachPipeline/list', 'Refresh'],
  ['shortLink/stats', 'Add Short Chain'],
  ['sopAgent/list', '查询'],
  ['system/rag-product-config', '添加规则'],
  ['system/rag-product-config', '保存配置'],
  ['tagSegmentation/list', '新增规则'],
  ['telegram', 'Create a Role'],
  ['whatsapp/group-messaging', '添加'],
  ['whatsapp/group-messaging', '新建模板']
]

test('[DIAG] 诊断交互超时按钮真实状态', async ({ page }) => {
  await page.goto(BASE + '/#/login', { waitUntil: 'networkidle' })
  await page.fill('input[type="text"]', ADMIN_USER)
  await page.fill('input[type="password"]', ADMIN_PASS)
  await page.click('button[type="button"].el-button--primary')
  await page.waitForURL('**/#/**', { timeout: 15000 }).catch(() => {})
  await page.waitForTimeout(1200)

  for (const [route, label] of CASES) {
    const url = BASE + '/#' + (route.startsWith('/') ? route : '/' + route)
    await page.goto(url, { waitUntil: 'domcontentloaded' }).catch(() => {})
    await page.waitForTimeout(700)
    const info = await page.evaluate((label) => {
      const btns = [...document.querySelectorAll('button.el-button, a.el-button')]
      const found = btns.filter((b) => (b.innerText || '').trim().includes(label))
      if (!found.length) return { found: 0 }
      const b = found[0]
      const cs = getComputedStyle(b)
      return {
        found: found.length,
        tag: b.tagName,
        disabled: b.disabled,
        visible: cs.display !== 'none' && cs.visibility !== 'hidden' && parseFloat(cs.opacity) > 0,
        href: b.tagName === 'A' ? b.getAttribute('href') : null,
        classes: b.className
      }
    }, label)
    // 真实点击尝试（仅当存在且看起来可点）
    let clickErr = ''
    if (info.found && !info.disabled && info.visible && info.tag !== 'A') {
      try {
        await page.locator('button.el-button', { hasText: label }).first().click({ timeout: 3000 })
        await page.waitForTimeout(500)
      } catch (e) {
        clickErr = e.message.split('\n')[0]
      }
    }
    _print(`[DIAG] ${route} :: "${label}" => ${JSON.stringify(info)}${clickErr ? ' CLICK_ERR=' + clickErr : ''}`)
  }
})
