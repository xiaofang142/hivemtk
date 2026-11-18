import { print as _print } from './lib/stdout.cjs';
import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';

const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD;
if (!ADMIN_PASSWORD) { console.error('E2E_ADMIN_PASSWORD 未设置，拒绝运行'); process.exit(1); }

const BASE_URL = 'http://localhost:8211';

// 第七批：geo-tools 子页 + system 子页 + 分析/工作台类
const batch7 = [
  { name: '新建自动化任务', route: '/browser-automation/tasks/create', type: 'form', texts: ['任务', '新建', '创建', 'Task'] },
  { name: '自动化定时任务', route: '/browser-automation/cron', type: 'page', texts: ['定时', 'Cron', '任务'] },
  { name: '自动化状态', route: '/browser-automation/status', type: 'page', texts: ['状态', 'Status', '自动化'] },
  { name: '邮件任务', route: '/email/jobs', type: 'page', texts: ['任务', 'Job', '邮件'] },
  { name: '邮件指南', route: '/email/guide', type: 'page', texts: ['指南', 'Guide', '邮件'] },
  { name: '短信任务', route: '/sms/jobs', type: 'page', texts: ['任务', 'Job', '短信'] },
  { name: 'WhatsApp任务', route: '/whatsapp/jobs', type: 'page', texts: ['任务', 'Job', 'WhatsApp'] },
  { name: 'WhatsApp草稿', route: '/whatsapp/drafts', type: 'page', texts: ['草稿', 'Draft', 'WhatsApp'] },
  { name: 'GEO推送配置', route: '/geo-tools/pusher-config', type: 'page', texts: ['推送', '配置', 'Push'] },
  { name: 'GEO站点配置', route: '/geo-tools/site-config', type: 'page', texts: ['站点', '配置', 'Site'] },
  { name: 'GEO竞品', route: '/geo-tools/competitors', type: 'page', texts: ['竞品', 'Competitor'] },
  { name: 'GEO内容优化', route: '/geo-tools/content-optimize', type: 'page', texts: ['内容', '优化', 'Optimize'] },
  { name: 'GEO站点发布', route: '/geo-tools/site-publish', type: 'page', texts: ['站点', '发布', 'Publish'] },
  { name: 'GEO索引追踪', route: '/geo-tools/index-tracking', type: 'page', texts: ['索引', 'Index', '追踪'] },
  { name: 'GEO决策报告', route: '/geo/decision-report', type: 'page', texts: ['决策', '报告', 'Report'] },
  { name: 'RAG产品配置', route: '/system/rag-product-config', type: 'page', texts: ['RAG', '配置', '产品'] },
  { name: '自动化中心', route: '/system/automation-hub', type: 'page', texts: ['自动化', '中心', 'Automation'] },
  { name: 'LTC配置', route: '/system/ltc-config', type: 'page', texts: ['LTC', '配置'] },
  { name: 'QQ账号', route: '/qq', type: 'page', texts: ['QQ', '账号'] },
  { name: '抖音卡片', route: '/douyinCard', type: 'page', texts: ['抖音', 'Douyin', '卡片'] },
  { name: '快手卡片', route: '/kuaishouCard', type: 'page', texts: ['快手', 'Kuaishou', '卡片'] },
  { name: '小红书卡片', route: '/xiaohongshuCard', type: 'page', texts: ['小红书', '卡片'] },
  { name: '闲鱼卡片', route: '/xianyuCard', type: 'page', texts: ['闲鱼', '卡片'] },
  { name: '置信度面板', route: '/confidence/panel', type: 'page', texts: ['置信', 'Confidence', '面板'] },
  { name: '去AI化面板', route: '/humanize/panel', type: 'page', texts: ['拟人度', 'Humanization', '基线', '评估'] },
];

const is404 = (text) => text.includes('404 Page Does not Exist') || text.includes('页面不存在');

async function main() {
  _print(`=== user-web 深度测试 第七批（${batch7.length} 条） ===\n`);

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

  for (let i = 0; i < batch7.length; i++) {
    const test = batch7[i];
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
    _print(`${icon} [${i + 1}/${batch7.length}] ${test.name} (${test.route}) => ${result.status}`);
    for (const c of result.checks) {
      _print(`   ${c.passed ? '✓' : '✗'} ${c.desc}: ${c.detail}`);
    }
    if (result.errors.length > 0) _print(`   ! 错误: ${result.errors[0]}`);
  }

  await browser.close();

  const reportPath = path.join(process.cwd(), 'deep-test-batch7-report.json');
  fs.writeFileSync(reportPath, JSON.stringify(results, null, 2));

  const ok = results.filter(r => r.status === 'ok').length;
  const partial = results.filter(r => r.status === 'partial').length;
  const fail = results.filter(r => ['fail', 'error', 'timeout'].includes(r.status)).length;

  _print(`\n=== 第七批深度测试完成 ===`);
  _print(`通过: ${ok} | 部分通过: ${partial} | 失败: ${fail}`);
  _print(`报告: ${reportPath}`);
}

main().catch(e => {
  console.error('测试脚本异常:', e);
  process.exit(1);
});
