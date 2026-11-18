import { print as _print } from './lib/stdout.cjs';
import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';

const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD;
if (!ADMIN_PASSWORD) { console.error('E2E_ADMIN_PASSWORD 未设置，拒绝运行'); process.exit(1); }

const BASE_URL = 'http://localhost:8211';

// 第九批：geo-tools 子页 + system 子页 + 分析/工作台类
const batch9 = [
  { name: '资产包沙盒(id)', route: '/asset-bundle/playground/1', type: 'page', texts: ['资产', '沙盒', 'Playground', '加载', '不存在'] },
  { name: '资产包商家(id)', route: '/asset-bundle/merchant/1', type: 'page', texts: ['商家', 'Merchant', '加载', '不存在'] },
  { name: 'BridgeToken', route: '/bridge/token', type: 'page', texts: ['Token', '令牌', 'Bridge'] },
  { name: 'Token页', route: '/token', type: 'page', texts: ['Token', '令牌'] },
  { name: '编辑自动化任务(id)', route: '/browser-automation/tasks/1/edit', type: 'form', texts: ['任务', '编辑', '保存', '加载', '不存在'] },
  { name: '自动化会话(id)', route: '/browser-automation/sessions/1', type: 'page', texts: ['会话', 'Session', '加载', '不存在'] },
  { name: '抖音统计', route: '/douyin/stats', type: 'page', texts: ['抖音', '统计', 'Douyin'] },
  { name: '抖音卡片统计(id)', route: '/douyin-card-stats/1', type: 'page', texts: ['抖音', '统计', '加载', '不存在'] },
  { name: '跟进详情(id)', route: '/followups/1', type: 'page', texts: ['跟进', '详情', '加载', '不存在'] },
  { name: 'GEO模板', route: '/geo-tools/schema-templates', type: 'page', texts: ['模板', 'Schema', 'GEO'] },
  { name: '快手统计', route: '/kuaishou/stats', type: 'page', texts: ['快手', '统计', 'Kuaishou'] },
  { name: 'OneID合并规则', route: '/oneid/merge-rules', type: 'page', texts: ['合并', '规则', 'OneID'] },
  { name: 'QQ账号页', route: '/qq/account', type: 'page', texts: ['QQ', '账号'] },
  { name: 'RAG账号', route: '/system/rag-account', type: 'page', texts: ['RAG', '账号'] },
  { name: 'RAG产品', route: '/system/rag-product', type: 'page', texts: ['RAG', '产品'] },
  { name: '话术AB统计', route: '/scriptTemplate/ab-stats', type: 'page', texts: ['AB', '统计', '话术'] },
  { name: '短链统计', route: '/shortLink/stats', type: 'page', texts: ['短链', '统计', 'Short'] },
  { name: 'SOP模板首页', route: '/sop-template', type: 'page', texts: ['SOP', '模板'] },
  { name: 'Telegram网关', route: '/telegram/gate', type: 'page', texts: ['Telegram', '网关', 'Gate'] },
  { name: 'TikTok统计', route: '/tiktok/stats', type: 'page', texts: ['TikTok', '统计'] },
  { name: 'TikTok卡片统计(id)', route: '/tiktok-card-stats/1', type: 'page', texts: ['TikTok', '统计', '加载', '不存在'] },
  { name: 'WhatsApp群选', route: '/whatsapp/lead-group-selection', type: 'page', texts: ['群', '选择', 'WhatsApp'] },
  { name: '工作流执行(id)', route: '/workflow-orchestrator/execution/1', type: 'page', texts: ['执行', '工作流', '加载', '不存在'] },
  { name: '闲鱼统计', route: '/xianyu/stats', type: 'page', texts: ['闲鱼', '统计'] },
  { name: '小红书统计', route: '/xiaohongshu/stats', type: 'page', texts: ['小红书', '统计'] },
];

const is404 = (text) => text.includes('404 Page Does not Exist') || text.includes('页面不存在');

async function main() {
  _print(`=== user-web 深度测试 第九批（${batch9.length} 条） ===\n`);

  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({ viewport: { width: 1280, height: 800 } });
  const page = await context.newPage();

  const consoleErrors = [];
  const apiErrors = [];
  page.on('console', (msg) => {
    if (msg.type() === 'error') consoleErrors.push(msg.text().substring(0, 300));
  });
  page.on('response', (res) => {
    if (res.url().includes('/api/') && res.status() >= 400) {
      apiErrors.push(`${res.status()} ${res.url().replace(BASE_URL, '')}`);
    }
  });

  // 登录
  _print('--- 登录 ---');
  await page.goto(BASE_URL + '/#/login', { waitUntil: 'networkidle', timeout: 15000 });
  await page.waitForTimeout(2000);
  const usernameInput = await page.$('input[type="text"], input[placeholder*="用户名"], input[placeholder*="username"], input[placeholder*="邮箱"], input[placeholder*="email"]');
  const passwordInput = await page.$('input[type="password"]');
  if (usernameInput && passwordInput) {
    await usernameInput.fill('admin');
    await passwordInput.fill(ADMIN_PASSWORD);
    const loginBtn = await page.$('button[type="submit"], .el-button--primary');
    if (loginBtn) await loginBtn.click();
  }
  await page.waitForTimeout(5000);
  const bodyAfterLogin = await page.evaluate(() => document.body?.innerText || '');
  const isLoggedIn = !is404(bodyAfterLogin) && (bodyAfterLogin.includes('退出') || bodyAfterLogin.toLowerCase().includes('logout') || !bodyAfterLogin.includes('Log In'));
  _print(`  ${isLoggedIn ? '✓' : '✗'} 登录${isLoggedIn ? '成功' : '失败'}\n`);
  if (!isLoggedIn) {
    _print('登录失败，终止测试');
    await browser.close();
    process.exit(1);
  }

  const results = [];

  for (let i = 0; i < batch9.length; i++) {
    const test = batch9[i];
    consoleErrors.length = 0;
    apiErrors.length = 0;

    const result = { name: test.name, route: test.route, status: 'unknown', checks: [], errors: [] };

    try {
      await page.goto(BASE_URL + '/#/' + test.route.replace(/^\//, ''), { waitUntil: 'networkidle', timeout: 20000 });
      await page.waitForTimeout(2500);

      const bodyText = await page.evaluate(() => document.body?.innerText || '');

      const not404 = !is404(bodyText);
      result.checks.push({ desc: '非404页面', passed: not404, detail: not404 ? '正常渲染' : '出现404' });

      const len = bodyText.trim().length;
      result.checks.push({ desc: '页面内容', passed: len > 50, detail: `${len} 字符` });

      if (test.type === 'form') {
        const formCount = await page.$$eval('form, .el-form, input, textarea, .el-select', els => els.length).catch(() => 0);
        result.checks.push({ desc: '表单元素', passed: formCount > 0, detail: `${formCount} 个` });
      } else {
        const structCount = await page.$$eval('.el-card, .el-table, .el-form, .el-button, button, input, table', els => els.length).catch(() => 0);
        result.checks.push({ desc: '交互/结构元素', passed: structCount > 0, detail: `${structCount} 个` });
      }

      const found = test.texts.filter(t => bodyText.includes(t));
      result.checks.push({ desc: '关键文本', passed: found.length > 0, detail: found.length ? `找到: ${found.join(', ')}` : `未命中: ${test.texts.join('/')}` });

      result.checks.push({ desc: 'API请求', passed: apiErrors.length === 0, detail: apiErrors.length ? apiErrors.slice(0, 3).join(' | ') : '全部成功', info: true });

      const isFakeId = /\/\d+$/.test(test.route);
      const realErrors = consoleErrors.filter(e =>
        !e.includes('favicon') &&
        !e.includes('Failed to load resource') &&
        // 假ID路由：API 404 触发的“资源不存在”提示属预期 UX，不算页面bug
        !(isFakeId && (e.includes('资源不存在') || e.includes('buildRequestError')))
      );
      result.checks.push({ desc: '控制台错误', passed: realErrors.length === 0, detail: realErrors.length ? realErrors[0] : '无' });

      const coreOk = not404 && len > 50;
      const allHard = result.checks.filter(c => c.desc !== 'API请求').every(c => c.passed);
      result.status = coreOk && allHard ? 'ok' : coreOk ? 'partial' : 'fail';
    } catch (e) {
      result.status = 'error';
      result.errors.push(e.message.substring(0, 200));
    }

    results.push(result);
    const icon = result.status === 'ok' ? '✓' : result.status === 'partial' ? '⚠' : '✗';
    _print(`${icon} [${i + 1}/${batch9.length}] ${test.name} (${test.route}) => ${result.status}`);
    for (const c of result.checks) {
      _print(`   ${c.passed ? '✓' : '✗'} ${c.desc}: ${c.detail}`);
    }
    if (result.errors.length > 0) _print(`   ! 错误: ${result.errors[0]}`);
  }

  await browser.close();

  const reportPath = path.join(process.cwd(), 'deep-test-batch9-report.json');
  fs.writeFileSync(reportPath, JSON.stringify(results, null, 2));

  const ok = results.filter(r => r.status === 'ok').length;
  const partial = results.filter(r => r.status === 'partial').length;
  const fail = results.filter(r => ['fail', 'error', 'timeout'].includes(r.status)).length;

  _print(`\n=== 第九批深度测试完成 ===`);
  _print(`通过: ${ok} | 部分通过: ${partial} | 失败: ${fail}`);
  _print(`报告: ${reportPath}`);
}

main().catch(e => {
  console.error('测试脚本异常:', e);
  process.exit(1);
});
