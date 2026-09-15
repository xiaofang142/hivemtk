import { chromium } from 'playwright';
import { writeFileSync } from "fs";
import { execSync } from "child_process";;

const BASE = 'http://localhost:8211';

// 提取所有路由路径
const paths = execSync(`grep -rh "path:" src/router/*.js src/router/modules/*.js 2>/dev/null | \
  grep -oE "path:\\s*['"]/[^'"]+['"]" | \
  sed "s/path:\\s*['"]//;s/['"]//" | \
  grep -v "redirect|component|meta|//" | \
  sort -u`, { encoding: 'utf8' });

const routes = paths.split('\n')
  .map(p => p.trim())
  .filter(p => p && !p.includes(':') && !p.includes('*') && p.length > 2);

console.log(`📋 提取到 ${routes.length} 个唯一路由`);

const browser = await chromium.launch({ headless: true });
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await ctx.newPage();

const consoleErrors = [];
page.on('console', msg => { if (msg.type() === 'error') consoleErrors.push(msg.text()); });
page.on('pageerror', err => consoleErrors.push('PAGE: ' + err.message));

// 登录
console.log('\n🔐 登录...');
await page.goto(BASE + '/#/login');
await page.waitForTimeout(1500);
await page.getByRole('textbox', { name: '用户名' }).fill('uit_admin');
await page.getByRole('textbox', { name: '密码' }).fill('UiTest@2026');
await page.getByRole('button', { name: '登录' }).click();
await page.waitForTimeout(2500);
console.log(`✅ 当前: ${page.url()}`);

const passed = [], failed = [];
let totalButtons = 0;

for (const route of routes) {
  const hash = '#/' + route.replace(/^\//, '');
  const url = BASE + hash;
  consoleErrors.length = 0;
  
  try {
    await page.goto(url, { timeout: 8000 });
    await page.waitForTimeout(600);
    
    const bodyLen = await page.evaluate(() => document.body?.textContent?.length || 0);
    const has404 = await page.evaluate(() => {
      const t = document.body?.innerText || '';
      return t.includes('404') && t.length < 500;
    });
    
    // 扫所有可见按钮
    const btns = await page.evaluate(() => {
      const arr = [];
      document.querySelectorAll('button, [role="button"], .el-button, .el-link').forEach(b => {
        const text = (b.innerText || b.textContent || '').trim();
        if (text && text.length < 30 && b.offsetParent !== null && b.offsetHeight > 0) {
          arr.push({ 
            text: text.replace(/\s+/g, ' ').slice(0, 25), 
            tag: b.tagName.toLowerCase(),
            disabled: b.disabled || b.getAttribute('disabled') !== null,
            cls: (b.className || '').includes('el-button') ? 'el-btn' : ''
          });
        }
      });
      return arr;
    });
    
    const uniqueBtns = [...new Map(btns.map(b => [b.text, b])).values()];
    totalButtons += uniqueBtns.length;
    
    if (has404) {
      failed.push({ route, reason: '404', consoleErr: consoleErrors.slice(0, 3) });
      console.log(`  ❌ ${route} → 404`);
    } else {
      passed.push({ route, bodyLen, btnCount: uniqueBtns.length, consoleErrCount: consoleErrors.length });
      console.log(`  ✅ ${route} (${uniqueBtns.length} 按钮, body=${bodyLen}${consoleErrors.length ? ', err=' + consoleErrors.length : ''})`);
    }
  } catch (e) {
    failed.push({ route, reason: e.message.slice(0, 60) });
    console.log(`  ❌ ${route} → ${e.message.slice(0, 60)}`);
  }
}

console.log(`\n========================================`);
console.log(`扫描完成: ${passed.length}/${routes.length} 通过, ${failed.length} 失败`);
console.log(`总按钮数: ${totalButtons}`);
console.log(`========================================`);

if (failed.length > 0) {
  console.log('\n❌ 失败路由:');
  failed.forEach(f => console.log(`  ${f.route} → ${f.reason}`));
}

// 有 console error 的页面
const withErr = passed.filter(p => p.consoleErrCount > 0);
if (withErr.length > 0) {
  console.log(`\n⚠️  有控制台错误的页面 (${withErr.length}):`);
  withErr.forEach(p => console.log(`  ${p.route} → ${p.consoleErrCount} errors`));
}

// 按钮数为 0 的页面
const noBtn = passed.filter(p => p.btnCount === 0 && p.bodyLen > 100);
if (noBtn.length > 0) {
  console.log(`\n📭 无按钮但有内容的页面 (${noBtn.length}):`);
  noBtn.forEach(p => console.log(`  ${p.route}`));
}

writeFileSync('/tmp/menu_scan.json', JSON.stringify({ passed, failed }, null, 2));
console.log('\n💾 详情 → /tmp/menu_scan.json');

await browser.close();
