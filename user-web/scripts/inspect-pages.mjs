import { print as _print } from './lib/stdout.cjs';
import { chromium } from 'playwright';

const BASE_URL = 'http://localhost:8211';

async function main() {
  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const page = await context.newPage();

  const routes = ['/llmRouting/list', '/operationLog/list', '/system/users'];

  for (const route of routes) {
    const url = BASE_URL + '/#/' + route;
    _print(`\n=== ${route} ===`);
    await page.goto(url, { waitUntil: 'networkidle', timeout: 15000 }).catch(() => null);
    await page.waitForTimeout(2000);

    const bodyText = await page.evaluate(() => document.body?.innerText?.substring(0, 800) || '');
    _print('Body text:', bodyText);

    const tables = await page.evaluate(() => {
      const els = document.querySelectorAll('table, [class*="table"], [class*="list"], .el-table');
      return Array.from(els).slice(0, 10).map(e => ({
        tag: e.tagName,
        class: e.className?.substring(0, 100),
        children: e.children.length,
        text: e.innerText?.substring(0, 100)
      }));
    });
    _print('Tables/Lists:', JSON.stringify(tables, null, 2));

    const allClasses = await page.evaluate(() => {
      const els = document.querySelectorAll('[class]');
      const classes = new Set();
      els.forEach(e => {
        e.className?.split(' ').forEach(c => {
          if (c && (c.includes('table') || c.includes('list') || c.includes('card') || c.includes('grid'))) {
            classes.add(c);
          }
        });
      });
      return Array.from(classes).slice(0, 20);
    });
    _print('Relevant classes:', allClasses);
  }

  await browser.close();
}

main().catch(e => { console.error(e); process.exit(1); });
