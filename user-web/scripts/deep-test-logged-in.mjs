import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';

const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD;
if (!ADMIN_PASSWORD) { console.error('E2E_ADMIN_PASSWORD 未设置，拒绝运行'); process.exit(1); }

const BASE_URL = 'http://localhost:8211';
const HASH_PREFIX = '/#/';

const coreTests = [
  {
    name: '仪表盘',
    route: '/ops-overview',
    checks: [
      { type: 'selector', value: '.el-card, .dashboard-card, .stat-card, [class*="card"]', desc: '数据卡片' },
      { type: 'text', value: ['系统状态', '在线用户', '总用户', '今日消息', '告警'], desc: '关键指标文本' },
      { type: 'selector', value: 'table, .el-table, [class*="table"]', desc: '数据表格' },
    ]
  },
  {
    name: '客户列表',
    route: '/customer360/list',
    checks: [
      { type: 'selector', value: '.el-table, table', desc: '客户表格' },
      { type: 'selector', value: 'input[placeholder*="搜索"], input[placeholder*="search"], .el-input', desc: '搜索框' },
      { type: 'text', value: ['客户', '姓名', '手机号', '来源'], desc: '表头文本' },
    ]
  },
  {
    name: '知识库',
    route: '/knowledgeBase/list',
    checks: [
      { type: 'selector', value: '.el-table, table', desc: '知识库表格' },
      { type: 'selector', value: '.el-button, button', desc: '操作按钮' },
      { type: 'text', value: ['知识库', '名称', '文档', '状态'], desc: '关键文本' },
    ]
  },
  {
    name: 'AI Agent',
    route: '/aiAgent/list',
    checks: [
      { type: 'selector', value: '.el-table, table', desc: 'Agent表格' },
      { type: 'selector', value: '.el-button, button', desc: '操作按钮' },
      { type: 'text', value: ['Agent', '名称', '模型', '状态'], desc: '关键文本' },
    ]
  },
  {
    name: '个人资料',
    route: '/profile',
    checks: [
      { type: 'selector', value: 'form, .el-form, [class*="form"]', desc: '表单' },
      { type: 'selector', value: 'input, .el-input', desc: '输入框' },
      { type: 'selector', value: '.el-button, button', desc: '保存按钮' },
    ]
  },
  {
    name: '系统用户',
    route: '/system/users',
    checks: [
      { type: 'selector', value: '.el-table, table', desc: '用户表格' },
      { type: 'selector', value: '.el-button, button', desc: '操作按钮' },
      { type: 'text', value: ['Personnel', 'New Account', 'Manage accounts'], desc: '关键文本' },
    ]
  },
  {
    name: '角色权限',
    route: '/system/roles',
    checks: [
      { type: 'selector', value: '.el-card, [class*="card"]', desc: '角色卡片' },
      { type: 'selector', value: '.el-button, button', desc: '操作按钮' },
      { type: 'text', value: ['Role', 'Members', '权限'], desc: '关键文本' },
    ]
  },
  {
    name: 'LLM路由',
    route: '/llmRouting/list',
    checks: [
      { type: 'selector', value: '.el-table, table', desc: '路由表格' },
      { type: 'text', value: ['模型', '提供商', '状态', '优先级'], desc: '关键文本' },
    ]
  },
  {
    name: '消息中心',
    route: '/messageHub/list',
    checks: [
      { type: 'selector', value: '.el-table, table, [class*="list"]', desc: '消息列表' },
      { type: 'text', value: ['消息', '发送', '接收', '状态'], desc: '关键文本' },
    ]
  },
  {
    name: '操作日志',
    route: '/operationLog/list',
    checks: [
      { type: 'selector', value: '.el-table, table', desc: '日志表格' },
      { type: 'selector', value: '.el-pagination, [class*="pagination"]', desc: '分页' },
      { type: 'text', value: ['操作', '用户', '时间', '详情'], desc: '关键文本' },
    ]
  },
];

async function main() {
  console.log(`=== user-web 核心页面深度测试（已登录） ===\n`);

  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({
    viewport: { width: 1280, height: 800 }
  });

  const page = await context.newPage();

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

  // 验证登录状态
  const currentUrl = page.url();
  const bodyText = await page.evaluate(() => document.body?.innerText || '');
  const isLoggedIn = !bodyText.includes('Log In') && !bodyText.includes('登录');
  console.log(`  URL: ${currentUrl}`);
  console.log(`  ${isLoggedIn ? '✓' : '✗'} 登录${isLoggedIn ? '成功' : '失败'}\n`);

  if (!isLoggedIn) {
    console.log('登录失败，终止测试');
    await browser.close();
    return;
  }

  console.log(`--- 测试 ${coreTests.length} 个核心页面 ---\n`);

  const results = [];

  for (let i = 0; i < coreTests.length; i++) {
    const test = coreTests[i];
    const url = BASE_URL + HASH_PREFIX + test.route;
    const result = { name: test.name, route: test.route, status: 'unknown', checks: [], errors: [] };

    try {
      await page.goto(BASE_URL + '/#/' + test.route.replace(/^\//, ''), { waitUntil: 'networkidle', timeout: 15000 });
      await page.waitForTimeout(2000);

      for (const check of test.checks) {
        let passed = false;
        let detail = '';

        try {
          if (check.type === 'selector') {
            const el = await page.$(check.value);
            passed = !!el;
            if (passed) {
              const count = await page.$$eval(check.value, els => els.length).catch(() => 1);
              detail = `${count} 个元素`;
            }
          } else if (check.type === 'text') {
            const bodyText = await page.evaluate(() => document.body?.innerText || '');
            const found = check.value.filter(t => bodyText.includes(t));
            passed = found.length > 0;
            detail = `找到: ${found.join(', ')}`;
          }
        } catch (e) {
          detail = `检查异常: ${e.message.substring(0, 100)}`;
        }

        result.checks.push({ desc: check.desc, passed, detail });
      }

      const allPassed = result.checks.every(c => c.passed);
      const somePassed = result.checks.some(c => c.passed);
      result.status = allPassed ? 'ok' : somePassed ? 'partial' : 'fail';
    } catch (e) {
      result.status = 'error';
      result.errors.push(e.message.substring(0, 200));
    }

    results.push(result);
    const icon = result.status === 'ok' ? '✓' : result.status === 'partial' ? '⚠' : '✗';
    console.log(`${icon} [${i + 1}/${coreTests.length}] ${test.name} (${test.route}) => ${result.status}`);
    for (const c of result.checks) {
      console.log(`   ${c.passed ? '✓' : '✗'} ${c.desc}: ${c.detail}`);
    }
    if (result.errors.length > 0) {
      console.log(`   ! 错误: ${result.errors[0]}`);
    }
  }

  await browser.close();

  const reportPath = path.join(process.cwd(), 'deep-test-report.json');
  fs.writeFileSync(reportPath, JSON.stringify(results, null, 2));

  const ok = results.filter(r => r.status === 'ok').length;
  const partial = results.filter(r => r.status === 'partial').length;
  const fail = results.filter(r => r.status === 'fail' || r.status === 'error' || r.status === 'timeout').length;

  console.log(`\n=== 深度测试完成 ===`);
  console.log(`通过: ${ok} | 部分通过: ${partial} | 失败: ${fail}`);
  console.log(`报告: ${reportPath}`);
}

main().catch(e => {
  console.error('测试脚本异常:', e);
  process.exit(1);
});
