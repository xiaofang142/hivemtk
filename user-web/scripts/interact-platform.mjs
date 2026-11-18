import { print as _print } from './lib/stdout.cjs';
import { chromium } from 'playwright';
import { execSync } from 'child_process';

const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD;
if (!ADMIN_PASSWORD) { console.error('E2E_ADMIN_PASSWORD 未设置，拒绝运行'); process.exit(1); }

const BASE = 'http://localhost:8212';
const routes = [
  '/dashboard',
  '/profile',
  '/platform/merchant',
  '/platform/merchant/list',
  '/platform/heartbeat',
  '/platform/monitoring',
  '/platform/asset-market/pending',
  '/platform/asset-market/list',
  '/platform/asset-market/contributors',
  '/platform/asset-market/revenue',
  '/platform/asset-market/withdrawals',
  '/platform/asset-market/playground'
];

async function login(page) {
  await page.goto(BASE + '/#/login', { waitUntil: 'domcontentloaded', timeout: 15000 });
  await page.waitForTimeout(2000);
  const u = await page.$('input[placeholder*="用户名"], input[type="text"]');
  const p = await page.$('input[type="password"]');
  if (u && p) {
    await u.fill('admin');
    await p.fill(ADMIN_PASSWORD);
    const btn = await page.$('button[type="submit"], .el-button--primary');
    if (btn) await btn.click();
    await page.waitForTimeout(4000);
  }
}

async function interactRoute(page, route, apiErrs, conErrs) {
  apiErrs.length = 0; conErrs.length = 0;
  // 先跳 about:blank 清理上一路由可能残留的 SSE/定时器
  try { await page.goto('about:blank', { timeout: 5000 }); } catch { /* 上一页面可能已关，清不掉就继续 */ }
  await page.waitForTimeout(300);
  await page.goto(BASE + '#/' + route.replace(/^\//, ''), { waitUntil: 'domcontentloaded', timeout: 20000 });
  await page.waitForTimeout(2500);

  // 点击所有可见按钮（最多10个）
  const btns = await page.$$('button:visible, .el-button:visible');
  let clicks = 0;
  for (const b of btns.slice(0, 10)) {
    try {
      const txt = (await b.textContent())?.trim() || '';
      if (!txt) continue; // 跳过空文本（头像/图标按钮，易误触下拉）
      if (/删除|移除|清空|注销|退出|登出|logout/i.test(txt)) continue; // 跳过危险/登出操作
      // 跳过 el-dropdown 内部元素（用户菜单，含退出登录）
      const inDropdown = await b.evaluate(el => !!el.closest('.el-dropdown'));
      if (inDropdown) continue;
      await b.click({ timeout: 2000 });
      await page.waitForTimeout(400);
      clicks++;
      // 关闭可能弹出的dialog/messagebox
      const closeBtn = await page.$('.el-dialog__headerbtn .el-icon, .el-message-box__headerbtn .el-icon, .el-dialog__headerbtn, .el-message-box__headerbtn');
      if (closeBtn) { await closeBtn.click({ timeout: 1000 }).catch(()=>{}); await page.waitForTimeout(200); }
      // Esc 关闭弹层
      if (page.url().includes('/#/login')) {
        _print('    [!] 误登出，重新登录');
        await login(page);
        await page.goto(BASE + '#/' + route.replace(/^\//, ''), { waitUntil: 'domcontentloaded', timeout: 20000 });
        await page.waitForTimeout(1500);
        break;
      }
    } catch { /* 单个按钮点不动不影响其余，继续下一个 */ }
  }

  // 填写输入框（跳过密码/搜索）
  const inputs = await page.$$('input:visible:not([type=password]):not([type=hidden]), textarea:visible');
  for (const inp of inputs.slice(0, 5)) {
    try {
      const ph = (await inp.getAttribute('placeholder')) || '';
      if (/密码|password|搜索|search/i.test(ph)) continue;
      await inp.fill('测试');
      await page.waitForTimeout(100);
    } catch { /* 只读/受控输入框填不进，跳过 */ }
  }

  // 点击el-tabs
  const tabs = await page.$$('.el-tabs__item:visible');
  for (const t of tabs.slice(0, 4)) {
    try { await t.click({ timeout: 1500 }); await page.waitForTimeout(500); } catch { /* 切不动的 tab 不算页面缺陷 */ }
  }

  // 点击el-switch
  const switches = await page.$$('.el-switch:visible');
  for (const sw of switches.slice(0, 3)) {
    try { await sw.click({ timeout: 1500 }); await page.waitForTimeout(400); } catch { /* 禁用的开关点不动 */ }
  }

  // 点击分页
  const pagerNums = await page.$$('.el-pagination .el-pager li:visible');
  if (pagerNums.length > 1) {
    try { await pagerNums[1].click({ timeout: 1000 }); await page.waitForTimeout(500); } catch { /* 只有一页时分页项会消失 */ }
  }

  const pageErrs = conErrs.filter(e => !/favicon|Download the Vue Devtools|Failed to load resource|登录失败/i.test(e));
  const status = (apiErrs.length === 0 && pageErrs.length === 0) ? 'ok' : (apiErrs.length <= 1 ? 'partial' : 'fail');
  _print(`  ${route.padEnd(45)} clicks=${clicks} switches=${switches.length} tabs=${tabs.length} api:${apiErrs.length}${apiErrs.length?' ['+apiErrs.slice(0,2).join('; ')+']':''} console:${pageErrs.length}${pageErrs.length?' ['+pageErrs[0].substring(0,80)+']':''} => ${status}`);
  return status;
}

async function launchAndLogin() {
  const browser = await chromium.launch({ headless: true });
  const ctx = await browser.newContext({ viewport: { width: 1440, height: 900 } });
  const page = await ctx.newPage();
  const apiErrs = [], conErrs = [];
  page.on('response', r => { if (r.url().includes('/api/') && r.status() >= 400) apiErrs.push(r.status() + ' ' + r.url().replace(BASE, '').substring(0, 80)); });
  page.on('console', m => { if (m.type() === 'error') conErrs.push(m.text().substring(0, 200)); });
  await login(page);
  return { browser, ctx, page, apiErrs, conErrs };
}

let { browser, ctx: _ctx, page, apiErrs, conErrs } = await launchAndLogin();
_print('=== platform-web 真人模拟交互测试 ===');

const results = { ok: 0, partial: 0, fail: 0 };
const skip = process.argv[2] ? process.argv[2].split(',').map(Number) : [];
for (let i = 0; i < routes.length; i++) {
  if (skip.includes(i)) continue;
  // 每6路由强制重启browser，清理SSE/内存累积（heartbeat/monitoring的EventSource会拖垮后续page）
  if (i > 0 && i % 6 === 0) {
    try { await Promise.race([browser.close(), new Promise((_, rj) => setTimeout(() => rj(new Error('close timeout')), 3000))]); } catch { /* 关不掉就走下面的重新登录 */ }
    await new Promise(r => setTimeout(r, 500));
    ({ browser, ctx: _ctx, page, apiErrs, conErrs } = await launchAndLogin());
    _print('  [browser restarted for memory/SSE cleanup]');
  }
  const route = routes[i];
  let done = false;
  for (let attempt = 0; attempt < 2 && !done; attempt++) {
    try {
      if (page.isClosed() || !browser.isConnected()) throw new Error('browser dead');
      const s = await interactRoute(page, route, apiErrs, conErrs);
      results[s] = (results[s] || 0) + 1;
      done = true;
    } catch (e) {
      _print(`  ${route.padEnd(45)} ${attempt === 0 ? 'CRASH' : 'FAIL'}: ${e.message.substring(0, 100)}`);
      // 重启整个 browser（close 带超时防挂起）
      try { await Promise.race([browser.close(), new Promise((_, rj) => setTimeout(() => rj(new Error('close timeout')), 3000))]); } catch { /* 关不掉的残骸由下一行 pkill 收走 */ }
      try { execSync('pkill -9 -f "playwright_chromium" 2>/dev/null || true', { timeout: 1500, shell: '/bin/bash' }); } catch { /* 命令自带 || true，这里只兜 execSync 超时 */ }
      await new Promise(r => setTimeout(r, 500));
      try {
        ({ browser, ctx: _ctx, page, apiErrs, conErrs } = await launchAndLogin());
      } catch (e2) {
        _print(`  ${route.padEnd(45)} relaunch failed: ${e2.message.substring(0, 80)}`);
      }
    }
  }
  if (!done) results.fail++;
}

_print(`\n=== 完成 ===`);
_print(`路由: ${results.ok} ok | ${results.partial} partial | ${results.fail} fail`);
await browser.close();
