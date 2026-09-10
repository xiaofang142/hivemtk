import { chromium } from 'playwright';
import { mkdirSync } from 'fs';

const OUT = 'C:/documents/www/hivemtk/docs/screenshots';
mkdirSync(OUT, { recursive: true });

const browser = await chromium.launch();
const ctx = await browser.newContext({ viewport: { width: 1600, height: 900 }, deviceScaleFactor: 2 });
const page = await ctx.newPage();

// 登录
await page.goto('http://localhost:8211/#/login', { waitUntil: 'domcontentloaded' });
await page.waitForTimeout(3000);
await page.getByRole('textbox', { name: '用户名' }).fill('admin');
await page.getByRole('textbox', { name: '密码' }).fill('Seed@123456');
await page.locator('button', { hasText: '登录' }).first().click();
await page.waitForTimeout(6000);
console.log('after login url:', page.url());

const shots = [
  ['http://localhost:8211/#/messageHub/list',    'screenshot-unified-inbox.png',   5000],
  ['http://localhost:8211/#/aiAgent/list',       'screenshot-ai-agent.png',        5000],
  ['http://localhost:8211/#/marketingFlow/list', 'screenshot-marketing-canvas.png',5000],
  ['http://localhost:8211/#/customer360/list',   'screenshot-customer-360.png',    5000],
  ['http://localhost:8211/#/knowledgeBase/list', 'screenshot-knowledge-rag.png',   5000],
];

for (const [url, file, ms] of shots) {
  await page.goto(url, { waitUntil: 'domcontentloaded' });
  await page.waitForTimeout(ms);
  await page.screenshot({ path: `${OUT}/${file}` });
  console.log('saved', file, page.url());
}

await browser.close();
