import { print as _print } from './lib/stdout.cjs';
import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';

const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD;
if (!ADMIN_PASSWORD) { console.error('E2E_ADMIN_PASSWORD 未设置，拒绝运行'); process.exit(1); }

const BASE_URL = 'http://localhost:8211';

// 第六批：geo-tools 子页 + system 子页 + 分析/工作台类
const batch6 = [
  { name: 'AI生产力', route: '/aiProductivity/list', type: 'page', texts: ['AI', '生产力', 'Productivity'] },
  { name: '审批任务', route: '/approvalTask/list', type: 'page', texts: ['审批', '任务', 'Approval'] },
  { name: '资产包列表', route: '/asset-bundle/list', type: 'page', texts: ['资产', '包', 'Bundle'] },
  { name: 'BadCase队列', route: '/badCase/list', type: 'page', texts: ['Bad', 'Case', '队列'] },
  { name: '批量操作', route: '/batchOperation/list', type: 'page', texts: ['批量', '操作', 'Batch'] },
  { name: '渠道列表', route: '/chatChannel/list', type: 'page', texts: ['渠道', 'Channel'] },
  { name: '流失预测', route: '/churnPrediction/list', type: 'page', texts: ['流失', '预测', 'Churn'] },
  { name: '线索统计', route: '/clue/statistics', type: 'page', texts: ['线索', '统计', 'Clue'] },
  { name: '社群列表', route: '/community/list', type: 'page', texts: ['社群', 'Community'] },
  { name: '客户旅程', route: '/customerJourney/dashboard', type: 'page', texts: ['旅程', 'Journey', '客户'] },
  { name: '对话记忆', route: '/dialogueMemory/list', type: 'page', texts: ['记忆', 'Memory', '对话'] },
  { name: '域名池', route: '/domainPool', type: 'page', texts: ['域名', 'Domain', '池'] },
  { name: '意图识别', route: '/intentRecognition/list', type: 'page', texts: ['意图', 'Intent'] },
  { name: '知识连接器', route: '/kb/connectors', type: 'page', texts: ['连接', 'Connector', '知识'] },
  { name: '平台账号', route: '/platformAccount/list', type: 'page', texts: ['平台', '账号', 'Account'] },
  { name: '触达管线列表', route: '/reachPipeline/list', type: 'page', texts: ['管线', 'Pipeline'] },
  { name: '增强操作日志', route: '/operationLog/enhanced', type: 'page', texts: ['日志', 'Log', '操作'] },
  { name: '消息看板', route: '/messageHub/dashboard', type: 'page', texts: ['消息', '看板', 'Dashboard'] },
  { name: '异议处理', route: '/objection/list', type: 'page', texts: ['异议', 'Objection', '处理'] },
  { name: '今日跟进', route: '/followups/today', type: 'page', texts: ['跟进', '今日', 'Follow'] },
  { name: 'RFM矩阵', route: '/userSegment/rfm-matrix', type: 'page', texts: ['RFM', '矩阵'] },
  { name: '短信配置', route: '/sms/config', type: 'page', texts: ['短信', '配置', 'SMS'] },
  { name: '邮件SMTP', route: '/email/smtp', type: 'page', texts: ['SMTP', '邮件'] },
  { name: 'Telegram账号', route: '/telegram/account', type: 'page', texts: ['Telegram', '账号'] },
  { name: 'WhatsApp账号', route: '/whatsapp/account', type: 'page', texts: ['WhatsApp', '账号'] },
];

const is404 = (text) => text.includes('404 Page Does not Exist') || text.includes('页面不存在');

async function main() {
  _print(`=== user-web 深度测试 第六批（${batch6.length} 条） ===\n`);

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

  for (let i = 0; i < batch6.length; i++) {
    const test = batch6[i];
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
    _print(`${icon} [${i + 1}/${batch6.length}] ${test.name} (${test.route}) => ${result.status}`);
    for (const c of result.checks) {
      _print(`   ${c.passed ? '✓' : '✗'} ${c.desc}: ${c.detail}`);
    }
    if (result.errors.length > 0) _print(`   ! 错误: ${result.errors[0]}`);
  }

  await browser.close();

  const reportPath = path.join(process.cwd(), 'deep-test-batch6-report.json');
  fs.writeFileSync(reportPath, JSON.stringify(results, null, 2));

  const ok = results.filter(r => r.status === 'ok').length;
  const partial = results.filter(r => r.status === 'partial').length;
  const fail = results.filter(r => ['fail', 'error', 'timeout'].includes(r.status)).length;

  _print(`\n=== 第六批深度测试完成 ===`);
  _print(`通过: ${ok} | 部分通过: ${partial} | 失败: ${fail}`);
  _print(`报告: ${reportPath}`);
}

main().catch(e => {
  console.error('测试脚本异常:', e);
  process.exit(1);
});
