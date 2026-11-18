import { print as _print } from './lib/stdout.cjs';
import { chromium } from 'playwright';

const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD;
if (!ADMIN_PASSWORD) { console.error('E2E_ADMIN_PASSWORD 未设置，拒绝运行'); process.exit(1); }
const BASE_URL = 'http://localhost:8211';
const browser = await chromium.launch({ headless: true });
const ctx = await browser.newContext({ viewport: { width: 1280, height: 800 } });
const page = await ctx.newPage();
const errs = [];
page.on('console', m => { if (m.type()==='error') errs.push(m.text().substring(0,500)); });
await page.goto(BASE_URL + '/#/login', { waitUntil: 'networkidle', timeout: 15000 });
await page.waitForTimeout(2000);
const u = await page.$('input[type="text"], input[placeholder*="用户名"], input[placeholder*="username"], input[placeholder*="邮箱"]');
const p = await page.$('input[type="password"]');
await u.fill('admin'); await p.fill(ADMIN_PASSWORD);
const btn = await page.$('button[type="submit"], .el-button--primary');
if (btn) await btn.click();
await page.waitForTimeout(5000);

errs.length = 0;
await page.goto(BASE_URL + '/#/' + 'setup', { waitUntil: 'networkidle', timeout: 15000 });
await page.waitForTimeout(2500);
const t1 = await page.evaluate(() => document.body?.innerText || '');
_print('=== /setup ===');
_print(t1.substring(0, 800));
_print('URL:', page.url());

errs.length = 0;
await page.goto(BASE_URL + '/#/' + 'reachPipeline/editor', { waitUntil: 'networkidle', timeout: 15000 });
await page.waitForTimeout(3000);
const t2 = await page.evaluate(() => document.body?.innerText || '');
_print('\n=== /reachPipeline/editor ===');
_print(t2.substring(0, 800));
_print('\n=== console errors ===');
_print(errs.join('\n---\n').substring(0, 3000));
await browser.close();
