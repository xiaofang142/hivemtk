import { print as _print } from './lib/stdout.cjs';
import { chromium } from 'playwright';

const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD;
if (!ADMIN_PASSWORD) { console.error('E2E_ADMIN_PASSWORD 未设置，拒绝运行'); process.exit(1); }

const BASE_URL = 'http://localhost:8211';

async function main() {
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const page = await context.newPage();

  page.on('console', msg => {
    if (msg.type() === 'error' || msg.type() === 'warning') {
      _print(`[console.${msg.type()}] ${msg.text().substring(0, 200)}`);
    }
  });

  page.on('response', resp => {
    const status = resp.status();
    if (status >= 400) {
      _print(`[api.error] ${status} ${resp.url().substring(0, 150)}`);
    }
  });

  // 登录
  _print('--- 登录 ---');
  await page.goto(BASE_URL + '/#/login', { waitUntil: 'networkidle', timeout: 15000 });
  await page.waitForTimeout(2000);

  const usernameInput = await page.$('input[type="text"], input[placeholder*="用户名"], input[placeholder*="username"]');
  const passwordInput = await page.$('input[type="password"]');
  if (usernameInput && passwordInput) {
    await usernameInput.fill('admin');
    await passwordInput.fill(ADMIN_PASSWORD);
    const loginBtn = await page.$('button[type="submit"], .el-button--primary');
    if (loginBtn) await loginBtn.click();
  }
  await page.waitForTimeout(5000);

  _print('URL after login:', page.url());

  // 检查 localStorage
  const token = await page.evaluate(() => localStorage.getItem('token'));
  const userInfo = await page.evaluate(() => localStorage.getItem('user_info'));
  _print('token:', token ? 'exists' : 'missing');
  _print('user_info:', userInfo ? 'exists' : 'missing');

  // 检查当前页面内容
  const bodyText1 = await page.evaluate(() => document.body?.innerText?.substring(0, 500) || '');
  _print('Body text (first 500 chars):', bodyText1.substring(0, 200));

  // 导航到 /ops-overview
  _print('\n--- 导航到 /ops-overview ---');
  await page.goto(BASE_URL + '/#/ops-overview', { waitUntil: 'networkidle', timeout: 15000 });
  await page.waitForTimeout(3000);

  _print('URL after nav:', page.url());

  const bodyText2 = await page.evaluate(() => document.body?.innerText?.substring(0, 1000) || '');
  _print('Body text after nav:', bodyText2.substring(0, 500));

  // 检查是否有 404
  const is404 = bodyText2.includes('页面不存在') || bodyText2.includes('404') || bodyText2.includes('NotFound');
  _print('Is 404:', is404);

  // 检查路由
  const routeInfo = await page.evaluate(() => {
    const app = document.querySelector('#app');
    if (!app || !app.__vue_app__) return 'no vue app';
    const router = app.__vue_app__.config.globalProperties.$router;
    if (!router) return 'no router';
    const current = router.currentRoute.value;
    return { path: current.path, matched: current.matched.length, name: current.name };
  });
  _print('Current route:', JSON.stringify(routeInfo));

  // 检查页面 HTML 结构
  const html = await page.evaluate(() => {
    const app = document.querySelector('#app');
    if (!app) return 'no #app';
    return app.innerHTML.substring(0, 500);
  });
  _print('App HTML:', html.substring(0, 300));

  await browser.close();
}

main().catch(e => { console.error(e); process.exit(1); });
