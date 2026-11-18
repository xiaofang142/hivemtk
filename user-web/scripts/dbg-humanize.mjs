import { print as _print } from './lib/stdout.cjs';
import { chromium } from 'playwright';

const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD;
if (!ADMIN_PASSWORD) { console.error('E2E_ADMIN_PASSWORD 未设置，拒绝运行'); process.exit(1); }
const b = await chromium.launch({ headless: true });
const ctx = await b.newContext({ viewport: { width: 1280, height: 800 } });
const page = await ctx.newPage();
await page.goto('http://localhost:8211/#/login', { waitUntil: 'networkidle', timeout: 15000 });
await page.waitForTimeout(2000);
const u = await page.$('input[type="text"], input[placeholder*="用户名"], input[placeholder*="邮箱"]');
const p = await page.$('input[type="password"]');
await u.fill('admin'); await p.fill(ADMIN_PASSWORD);
const btn = await page.$('button[type="submit"], .el-button--primary');
if (btn) await btn.click();
await page.waitForTimeout(5000);
await page.goto('http://localhost:8211/#/humanize/panel', { waitUntil: 'networkidle', timeout: 15000 });
await page.waitForTimeout(3000);
const t = await page.evaluate(() => document.body?.innerText || '');
_print('CONTENT:', t.substring(0, 600));
await b.close();
