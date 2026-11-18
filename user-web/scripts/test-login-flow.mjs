import { print as _print } from './lib/stdout.cjs';
import { chromium } from 'playwright';

const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD;
if (!ADMIN_PASSWORD) { console.error('E2E_ADMIN_PASSWORD 未设置，拒绝运行'); process.exit(1); }

const BASE_URL = 'http://localhost:8211';

async function main() {
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const page = await context.newPage();

  const apiResponses = [];
  page.on('response', resp => {
    const url = resp.url();
    if (url.includes('/api/')) {
      apiResponses.push(`${resp.status()} ${url.substring(0, 120)}`);
    }
  });

  // 登录
  _print('--- 登录 ---');
  await page.goto(BASE_URL + '/#/login', { waitUntil: 'networkidle', timeout: 15000 });
  await page.waitForTimeout(1000);

  const usernameInput = await page.$('input[type="text"], input[placeholder*="用户名"], input[placeholder*="username"], input[placeholder*="邮箱"], input[placeholder*="email"]');
  const passwordInput = await page.$('input[type="password"]');

  if (usernameInput && passwordInput) {
    await usernameInput.fill('admin');
    await passwordInput.fill(ADMIN_PASSWORD);
    _print('  已填写表单');

    const loginBtn = await page.$('button[type="submit"], .el-button--primary');
    if (loginBtn) {
      await loginBtn.click();
      _print('  已点击登录');
    }
  }

  await page.waitForTimeout(5000);

  // 检查当前URL和页面内容
  const currentUrl = page.url();
  _print(`\n登录后URL: ${currentUrl}`);

  const bodyText = await page.evaluate(() => document.body?.innerText?.substring(0, 1000) || '');
  _print(`页面内容: ${bodyText}`);

  // 检查localStorage
  const token = await page.evaluate(() => {
    return localStorage.getItem('token') || localStorage.getItem('access_token') || localStorage.getItem('user') || '';
  });
  _print(`\nlocalStorage token: ${token?.substring(0, 50)}...`);

  // 尝试访问一个需要登录的页面
  _print('\n--- 访问 /ops-overview ---');
  apiResponses.length = 0;
  await page.goto(BASE_URL + '/#/ops-overview', { waitUntil: 'networkidle', timeout: 15000 });
  await page.waitForTimeout(2000);

  const opsBodyText = await page.evaluate(() => document.body?.innerText?.substring(0, 1000) || '');
  _print(`页面内容: ${opsBodyText}`);
  _print(`\nAPI响应 (${apiResponses.length}):`);
  apiResponses.forEach(r => _print(`  ${r}`));

  await browser.close();
}

main().catch(e => { console.error(e); process.exit(1); });
