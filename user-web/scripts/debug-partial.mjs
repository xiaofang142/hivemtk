import { print as _print } from './lib/stdout.cjs';
import { chromium } from 'playwright';

const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD;
if (!ADMIN_PASSWORD) { console.error('E2E_ADMIN_PASSWORD 未设置，拒绝运行'); process.exit(1); }

const BASE_URL = 'http://localhost:8211';

const pages = [
  { name: '客户列表', route: '/customer360/list' },
  { name: '系统用户', route: '/system/users' },
  { name: '角色权限', route: '/system/roles' },
];

async function main() {
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const page = await context.newPage();

  // 登录
  await page.goto(BASE_URL + '/#/login', { waitUntil: 'networkidle', timeout: 15000 });
  await page.waitForTimeout(2000);
  const usernameInput = await page.$('input[type="text"], input[placeholder*="用户名"]');
  const passwordInput = await page.$('input[type="password"]');
  if (usernameInput && passwordInput) {
    await usernameInput.fill('admin');
    await passwordInput.fill(ADMIN_PASSWORD);
    const loginBtn = await page.$('button[type="submit"], .el-button--primary');
    if (loginBtn) await loginBtn.click();
  }
  await page.waitForTimeout(5000);

  for (const p of pages) {
    _print(`\n=== ${p.name} (${p.route}) ===`);
    await page.goto(BASE_URL + '/#/' + p.route.replace(/^\//, ''), { waitUntil: 'networkidle', timeout: 15000 });
    await page.waitForTimeout(3000);

    const bodyText = await page.evaluate(() => document.body?.innerText?.substring(0, 800) || '');
    _print('Body text:', bodyText.substring(0, 400));

    const tables = await page.evaluate(() => {
      const els = document.querySelectorAll('.el-table, table');
      return els.length;
    });
    _print('Tables found:', tables);

    const pagination = await page.evaluate(() => {
      const els = document.querySelectorAll('.el-pagination, [class*="pagination"]');
      return els.length;
    });
    _print('Pagination found:', pagination);

    const forms = await page.evaluate(() => {
      const els = document.querySelectorAll('.el-form, form');
      return els.length;
    });
    _print('Forms found:', forms);

    const buttons = await page.evaluate(() => {
      const els = document.querySelectorAll('.el-button, button');
      return els.length;
    });
    _print('Buttons found:', buttons);

    const inputs = await page.evaluate(() => {
      const els = document.querySelectorAll('input, .el-input');
      return els.length;
    });
    _print('Inputs found:', inputs);

    const cards = await page.evaluate(() => {
      const els = document.querySelectorAll('.el-card, .dashboard-card, .stat-card, [class*="card"]');
      return els.length;
    });
    _print('Cards found:', cards);

    const emptyText = await page.evaluate(() => {
      const els = document.querySelectorAll('.el-table__empty-text, .empty-text, [class*="empty"]');
      return Array.from(els).map(e => e.textContent).join(', ');
    });
    _print('Empty text:', emptyText || 'none');
  }

  await browser.close();
}

main().catch(e => { console.error(e); process.exit(1); });
