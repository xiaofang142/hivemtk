import { print as _print } from './lib/stdout.cjs';
import { chromium } from 'playwright';
import { readFileSync, writeFileSync } from 'fs';

const BASE = 'http://localhost:8211';
const routes = readFileSync('/tmp/routes.txt', 'utf8').trim().split('\n').filter(Boolean);

_print(`📋 共 ${routes.length} 个路由待扫描\n`);

const browser = await chromium.launch({ headless: true });
const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
const page = await ctx.newPage();

const pageErrors = new Map(); // route -> [errors]
page.on('console', msg => {
  if (msg.type() === 'error') {
    const url = new URL(page.url());
    const key = url.hash.replace('#/', '').split('?')[0] || '/';
    if (!pageErrors.has(key)) pageErrors.set(key, []);
    pageErrors.get(key).push(msg.text().slice(0, 120));
  }
});
page.on('pageerror', err => {
  const url = new URL(page.url());
  const key = url.hash.replace('#/', '').split('?')[0] || '/';
  if (!pageErrors.has(key)) pageErrors.set(key, []);
  pageErrors.get(key).push('PAGE: ' + err.message.slice(0, 100));
});

// 登录
_print('🔐 登录...');
await page.goto(BASE + '/#/login');
await page.waitForTimeout(1500);
await page.getByRole('textbox', { name: '用户名' }).fill('uit_admin');
await page.getByRole('textbox', { name: '密码' }).fill('UiTest@2026');
await page.getByRole('button', { name: '登录' }).click();
await page.waitForTimeout(2500);
_print(`✅ 已登录, 当前: ${page.url()}\n`);

const results = { passed: [], failed: [], zeroBtn: [], consoleErr: [] };
let btnTotal = 0, clickTotal = 0, clickFail = 0;

for (const route of routes) {
  const url = BASE + '#/' + route.replace(/^\//, '');
  await page.goto(url, { timeout: 8000 });
  await page.waitForTimeout(700);
  
  const currentErrors = pageErrors.get(route) || [];
  
  // 页面是否 404
  const info = await page.evaluate(() => {
    const text = document.body?.innerText || '';
    const has404 = text.includes('404') && text.length < 600;
    const bodyLen = text.length;
    const title = document.title;
    
    // 扫按钮
    const buttons = [];
    document.querySelectorAll('button, .el-button, [role="button"], .el-link--primary').forEach(b => {
      if (b.offsetParent === null || b.offsetHeight === 0) return;
      const text = (b.innerText || b.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 30);
      if (!text) return;
      const rect = b.getBoundingClientRect();
      buttons.push({ 
        text, 
        tag: b.tagName.toLowerCase(),
        disabled: b.disabled || b.getAttribute('disabled') !== null || b.getAttribute('aria-disabled') === 'true',
        cls: b.className?.includes('el-button') ? 'el' : '',
        w: Math.round(rect.width),
        h: Math.round(rect.height),
        x: Math.round(rect.x),
        y: Math.round(rect.y)
      });
    });
    
    // 去重（同文本同位置只算一个）
    const seen = new Set();
    const unique = [];
    for (const btn of buttons) {
      const k = `${btn.text}_${btn.x}_${btn.y}`;
      if (!seen.has(k)) { seen.add(k); unique.push(btn); }
    }
    
    return { has404, bodyLen, title, buttons: unique };
  });
  
  btnTotal += info.buttons.length;
  
  if (info.has404) {
    results.failed.push({ route, reason: '404 Page' });
    _print(`  ❌ ${route} → 404`);
  } else {
    results.passed.push({ 
      route, 
      bodyLen: info.bodyLen, 
      btnCount: info.buttons.length,
      errCount: currentErrors.length
    });
    
    const flags = [];
    if (info.buttons.length === 0 && info.bodyLen > 200) { flags.push('无按钮'); results.zeroBtn.push(route); }
    if (currentErrors.length > 0) { flags.push(`err=${currentErrors.length}`); results.consoleErr.push({ route, errors: currentErrors.slice(0, 3) }); }
    
    const flagStr = flags.length ? ` [${flags.join(',')}]` : '';
    _print(`  ✅ ${route} (${info.buttons.length} btn)${flagStr}`);
  }
}

// ======== 第二阶段：对有按钮的页面尝试点击 ========
_print(`\n========================================`);
_print(`第二阶段: 按钮交互测试`);
_print(`========================================\n`);

const clickablePages = results.passed.filter(p => p.btnCount > 0 && p.btnCount < 30);
_print(`🎯 选 ${clickablePages.length} 个页面做按钮点击测试\n`);

const clickResults = [];

for (const pageInfo of clickablePages.slice(0, 40)) {
  const route = pageInfo.route;
  const url = BASE + '#/' + route.replace(/^\//, '');
  
  await page.goto(url, { timeout: 8000 });
  await page.waitForTimeout(600);
  
  // 重新扫按钮（页面可能已变）
  const buttons = await page.evaluate(() => {
    const arr = [];
    document.querySelectorAll('button, .el-button, .el-link--primary').forEach((b, i) => {
      if (b.offsetParent === null || b.offsetHeight === 0) return;
      const text = (b.innerText || b.textContent || '').trim().replace(/\s+/g, ' ').slice(0, 25);
      if (!text) return;
      arr.push({ text, disabled: b.disabled, index: i });
    });
    return arr;
  });
  
  // 选前 3 个非禁用按钮点击
  const toClick = buttons.filter(b => !b.disabled).slice(0, 3);
  const pageClickResult = { route, clicks: [] };
  
  for (const btn of toClick) {
    clickTotal++;
    try {
      // 用 text 找按钮
      const btnEl = page.getByRole('button', { name: new RegExp(btn.text.replace(/[.*+?^${}()|[\]\\]/g, '\\$&').slice(0, 15)) }).first();
      const beforeUrl = page.url();
      const _beforeModal = await page.evaluate(() => !!document.querySelector('.el-dialog[aria-hidden="false"], .el-message-box'));
      
      await btnEl.click({ timeout: 2000, force: true }).catch(() => {
        // fallback: 用 index 点击
        return page.locator('button, .el-button').nth(btn.index).click({ timeout: 2000, force: true });
      });
      await page.waitForTimeout(400);
      
      const afterUrl = page.url();
      const afterModal = await page.evaluate(() => !!document.querySelector('.el-dialog[aria-hidden="false"], .el-message-box, .el-drawer'));
      
      // 如果弹出对话框，关掉
      if (afterModal) {
        await page.keyboard.press('Escape').catch(() => {});
        await page.waitForTimeout(200);
      }
      
      const navigated = beforeUrl !== afterUrl;
      pageClickResult.clicks.push({ btn: btn.text, ok: true, navigated, modal: afterModal });
      _print(`    👆 "${btn.text}" → ${navigated ? '导航' : afterModal ? '弹窗' : '无变化'}`);
      
    } catch (e) {
      clickFail++;
      pageClickResult.clicks.push({ btn: btn.text, ok: false, error: e.message.slice(0, 60) });
      _print(`    ❌ "${btn.text}" 点击失败: ${e.message.slice(0, 50)}`);
    }
  }
  
  clickResults.push(pageClickResult);
  _print(`  📄 ${route}: ${toClick.length}/${buttons.length} 按钮点击`);
}

// ======== 汇总 ========
_print(`\n========================================`);
_print(`最终汇总`);
_print(`========================================`);
_print(`✅ 页面通过: ${results.passed.length}/${routes.length}`);
_print(`❌ 页面失败: ${results.failed.length}`);
_print(`📊 总按钮数: ${btnTotal}`);
_print(`👆 按钮点击: ${clickTotal} (${clickFail} 失败)`);
_print(`⚠️  有 console error: ${results.consoleErr.length}`);
_print(`📭 无按钮页面: ${results.zeroBtn.length}`);

if (results.failed.length) {
  _print(`\n❌ 失败路由:`);
  results.failed.forEach(f => _print(`  ${f.route} → ${f.reason}`));
}
if (results.consoleErr.length) {
  _print(`\n⚠️  Console error (前 10):`);
  results.consoleErr.slice(0, 10).forEach(e => _print(`  ${e.route}: ${e.errors[0]}`));
}

writeFileSync('/tmp/full_scan_result.json', JSON.stringify({ results, clickResults }, null, 2));
_print(`\n💾 完整结果 → /tmp/full_scan_result.json`);

await browser.close();
