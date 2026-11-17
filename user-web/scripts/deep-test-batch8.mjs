import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';

const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD;
if (!ADMIN_PASSWORD) { console.error('E2E_ADMIN_PASSWORD 未设置，拒绝运行'); process.exit(1); }

const BASE_URL = 'http://localhost:8211';

// 第八批：geo-tools 子页 + system 子页 + 分析/工作台类
const batch8 = [
  { name: '资产包沙盒', route: '/asset-bundle/playground', type: 'page', texts: ['沙盒', 'Playground', '资产'] },
  { name: '资产包商家新建', route: '/asset-bundle/merchant-new', type: 'form', texts: ['商家', '新建', '创建', 'Merchant'] },
  { name: 'Bridge令牌', route: '/bridge', type: 'page', texts: ['Bridge', '令牌', 'Token'] },
  { name: '大屏列表', route: '/dashboardScreen/list', type: 'page', texts: ['看板', '列表', 'Screen'] },
  { name: '会话标签', route: '/customerService/sessionTag', type: 'page', texts: ['标签', 'Tag', '会话'] },
  { name: 'AI建议', route: '/customerService/aiSuggestion', type: 'page', texts: ['建议', 'Suggestion', 'AI'] },
  { name: '邮件账号信息', route: '/email/info', type: 'page', texts: ['邮件', '账号', 'Email'] },
  { name: '邮件送达率', route: '/email/deliverability', type: 'page', texts: ['送达', 'Deliver', '邮件'] },
  { name: 'GEO漏斗看板', route: '/geo-tools/funnel-dashboard', type: 'page', texts: ['漏斗', 'Funnel', '看板'] },
  { name: 'GEO SOV看板', route: '/geo-tools/sov-board', type: 'page', texts: ['SOV', '看板'] },
  { name: 'GEO爬虫统计', route: '/geo-tools/crawler-stats', type: 'page', texts: ['爬虫', 'Crawler', '统计'] },
  { name: 'GEO验证', route: '/geo-tools/verification', type: 'page', texts: ['验证', 'Verification'] },
  { name: '知识反馈', route: '/knowledge/feedbacks', type: 'page', texts: ['反馈', 'Feedback', '知识'] },
  { name: '知识Token', route: '/knowledge/tokens', type: 'page', texts: ['Token', '令牌'] },
  { name: '外部知识', route: '/knowledge/external', type: 'page', texts: ['外部', 'External', '知识'] },
  { name: '知识连接器列表', route: '/knowledge/connectors', type: 'page', texts: ['连接', 'Connector'] },
  { name: 'LLM成本', route: '/llmRouting/cost', type: 'page', texts: ['成本', 'Cost', 'LLM'] },
  { name: 'SOP模板列表', route: '/sop-template/list', type: 'page', texts: ['SOP', '模板', '列表'] },
  { name: 'SOP模板市场', route: '/sop-template/market', type: 'page', texts: ['市场', 'Market', 'SOP'] },
  { name: '反馈闭环面板', route: '/feedbackLoop/panel', type: 'page', texts: ['反馈', '闭环', 'Feedback'] },
  { name: '分群构建器', route: '/userSegment/builder', type: 'page', texts: ['分群', '构建', 'Builder'] },
  { name: 'WhatsApp群发', route: '/whatsapp/group-messaging', type: 'page', texts: ['群发', 'Group', 'WhatsApp'] },
  { name: 'WhatsApp批量矩阵', route: '/whatsapp/bulk-matrix', type: 'page', texts: ['批量', '矩阵', 'Bulk'] },
  { name: 'TikTok列表', route: '/tiktok/list', type: 'page', texts: ['TikTok', '列表'] },
  { name: 'OneID冲突', route: '/oneid/conflicts', type: 'page', texts: ['冲突', 'Conflict', 'OneID'] },
];

const is404 = (text) => text.includes('404 Page Does not Exist') || text.includes('页面不存在');

async function main() {
  console.log(`=== user-web 深度测试 第八批（${batch8.length} 条） ===\n`);

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
  console.log('--- 登录 ---');
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
  console.log(`  ${isLoggedIn ? '✓' : '✗'} 登录${isLoggedIn ? '成功' : '失败'}\n`);
  if (!isLoggedIn) {
    console.log('登录失败，终止测试');
    await browser.close();
    process.exit(1);
  }

  const results = [];

  for (let i = 0; i < batch8.length; i++) {
    const test = batch8[i];
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

      const realErrors = consoleErrors.filter(e => !e.includes('favicon') && !e.includes('Failed to load resource'));
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
    console.log(`${icon} [${i + 1}/${batch8.length}] ${test.name} (${test.route}) => ${result.status}`);
    for (const c of result.checks) {
      console.log(`   ${c.passed ? '✓' : '✗'} ${c.desc}: ${c.detail}`);
    }
    if (result.errors.length > 0) console.log(`   ! 错误: ${result.errors[0]}`);
  }

  await browser.close();

  const reportPath = path.join(process.cwd(), 'deep-test-batch8-report.json');
  fs.writeFileSync(reportPath, JSON.stringify(results, null, 2));

  const ok = results.filter(r => r.status === 'ok').length;
  const partial = results.filter(r => r.status === 'partial').length;
  const fail = results.filter(r => ['fail', 'error', 'timeout'].includes(r.status)).length;

  console.log(`\n=== 第八批深度测试完成 ===`);
  console.log(`通过: ${ok} | 部分通过: ${partial} | 失败: ${fail}`);
  console.log(`报告: ${reportPath}`);
}

main().catch(e => {
  console.error('测试脚本异常:', e);
  process.exit(1);
});
