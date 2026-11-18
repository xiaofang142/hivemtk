import { print as _print } from './lib/stdout.cjs';
import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';

const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD;
if (!ADMIN_PASSWORD) { console.error('E2E_ADMIN_PASSWORD 未设置，拒绝运行'); process.exit(1); }

const BASE_URL = 'http://localhost:8211';

// 第二批：从 216 条清单中选取的 20 条代表性路由（均无 :param）
const batch2 = [
  { name: '线索列表', route: '/clue/list', texts: ['线索', '线索列表', 'Clue', 'Lead'] },
  { name: '会话列表', route: '/customerSession/list', texts: ['会话', 'Session', '客户'] },
  { name: '知识管理', route: '/knowledge/management', texts: ['知识', 'Knowledge', '文档'] },
  { name: '邮件', route: '/email', texts: ['邮件', 'Email', '收件', '发件'] },
  { name: '短信列表', route: '/sms/list', texts: ['短信', 'SMS', '发送'] },
  { name: 'WhatsApp', route: '/whatsapp', texts: ['WhatsApp', 'whatsapp', '账号', 'Account'] },
  { name: 'Telegram', route: '/telegram', texts: ['Telegram', 'telegram', '账号', 'Account'] },
  { name: '飞书', route: '/feishu', texts: ['飞书', 'Feishu', '账号', 'Account'] },
  { name: '钉钉应用', route: '/dingtalk-app', texts: ['钉钉', 'DingTalk', '应用', 'App'] },
  { name: '备份', route: '/backup/list', texts: ['备份', 'Backup', '恢复', 'Restore'] },
  { name: '安全审计', route: '/securityAudit/list', texts: ['安全', '审计', 'Security', 'Audit'] },
  { name: '用户分群', route: '/userSegment/list', texts: ['分群', 'Segment', '用户'] },
  { name: '转化漏斗', route: '/conversionFunnel/list', texts: ['漏斗', 'Funnel', '转化', 'Conversion'] },
  { name: '工作流编排', route: '/workflow-orchestrator/list', texts: ['工作流', 'Workflow', '编排'] },
  { name: '浏览器自动化', route: '/browser-automation', texts: ['浏览器', 'Browser', '自动化', '任务'] },
  { name: '资产市场', route: '/asset-market', texts: ['资产', 'Asset', '市场', 'Market'] },
  { name: 'GEO配置', route: '/geo-tools/config', texts: ['配置', 'Config', 'GEO'] },
  { name: '系统配置', route: '/system/config', texts: ['配置', 'Config', '系统', 'System'] },
  { name: '收件箱', route: '/inbox/list', texts: ['收件', 'Inbox', '消息'] },
  { name: 'FAQ', route: '/faq', texts: ['FAQ', '常见问题', '问题'] },
];

const is404 = (text) => text.includes('404 Page Does not Exist') || text.includes('页面不存在');

async function main() {
  _print(`=== user-web 深度测试 第二批（${batch2.length} 条） ===\n`);

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

  for (let i = 0; i < batch2.length; i++) {
    const test = batch2[i];
    consoleErrors.length = 0;
    apiErrors.length = 0;

    const result = { name: test.name, route: test.route, status: 'unknown', checks: [], errors: [...apiErrors] };

    try {
      await page.goto(BASE_URL + '/#/' + test.route.replace(/^\//, ''), { waitUntil: 'networkidle', timeout: 20000 });
      await page.waitForTimeout(2500);

      const bodyText = await page.evaluate(() => document.body?.innerText || '');

      // 检查1: 非404
      const not404 = !is404(bodyText);
      result.checks.push({ desc: '非404页面', passed: not404, detail: not404 ? '正常渲染' : '出现404' });

      // 检查2: 内容量
      const len = bodyText.trim().length;
      result.checks.push({ desc: '页面内容', passed: len > 100, detail: `${len} 字符` });

      // 检查3: 结构元素
      const structCount = await page.$$eval('.el-card, .el-table, .el-form, .el-button, button, input, table', els => els.length).catch(() => 0);
      result.checks.push({ desc: '交互/结构元素', passed: structCount > 0, detail: `${structCount} 个` });

      // 检查4: 路由关键文本（命中任一即过）
      const found = test.texts.filter(t => bodyText.includes(t));
      result.checks.push({ desc: '关键文本', passed: found.length > 0, detail: found.length ? `找到: ${found.join(', ')}` : `未命中: ${test.texts.join('/')}` });

      // 检查5: 无API错误
      result.checks.push({ desc: 'API请求', passed: apiErrors.length === 0, detail: apiErrors.length ? apiErrors.slice(0, 3).join(' | ') : '全部成功' });

      // 检查6: 无控制台错误
      result.checks.push({ desc: '控制台错误', passed: consoleErrors.length === 0, detail: consoleErrors.length ? consoleErrors[0] : '无' });

      const allPassed = result.checks.every(c => c.passed);
      const somePassed = result.checks.some(c => c.passed);
      result.status = allPassed ? 'ok' : somePassed ? 'partial' : 'fail';
      if (not404 && len > 100 && structCount > 0 && result.status !== 'ok') result.status = 'partial';
    } catch (e) {
      result.status = 'error';
      result.errors.push(e.message.substring(0, 200));
    }

    results.push(result);
    const icon = result.status === 'ok' ? '✓' : result.status === 'partial' ? '⚠' : '✗';
    _print(`${icon} [${i + 1}/${batch2.length}] ${test.name} (${test.route}) => ${result.status}`);
    for (const c of result.checks) {
      _print(`   ${c.passed ? '✓' : '✗'} ${c.desc}: ${c.detail}`);
    }
    if (result.errors.length > 0) _print(`   ! 错误: ${result.errors[0]}`);
  }

  await browser.close();

  const reportPath = path.join(process.cwd(), 'deep-test-batch2-report.json');
  fs.writeFileSync(reportPath, JSON.stringify(results, null, 2));

  const ok = results.filter(r => r.status === 'ok').length;
  const partial = results.filter(r => r.status === 'partial').length;
  const fail = results.filter(r => ['fail', 'error', 'timeout'].includes(r.status)).length;

  _print(`\n=== 第二批深度测试完成 ===`);
  _print(`通过: ${ok} | 部分通过: ${partial} | 失败: ${fail}`);
  _print(`报告: ${reportPath}`);
}

main().catch(e => {
  console.error('测试脚本异常:', e);
  process.exit(1);
});
