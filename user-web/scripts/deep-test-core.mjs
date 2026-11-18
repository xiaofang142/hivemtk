import { print as _print } from './lib/stdout.cjs';
import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';

const BASE_URL = 'http://localhost:8211';
const HASH_PREFIX = '/#/';

const coreTests = [
  {
    name: '仪表盘',
    route: '/ops-overview',
    checks: [
      { type: 'selector', value: '.el-card, .dashboard-card, .stat-card, [class*="card"]', desc: '数据卡片' },
      { type: 'text', value: ['总客户', '活跃', '收入', '订单', '转化'], desc: '关键指标文本' },
      { type: 'selector', value: 'table, .el-table, [class*="table"]', desc: '数据表格' },
    ]
  },
  {
    name: '客户列表',
    route: '/customer360/list',
    checks: [
      { type: 'selector', value: '.el-table, table', desc: '客户表格' },
      { type: 'selector', value: '.el-pagination, [class*="pagination"]', desc: '分页组件' },
      { type: 'selector', value: 'input[placeholder*="搜索"], input[placeholder*="search"], .el-input', desc: '搜索框' },
      { type: 'text', value: ['客户', '姓名', '邮箱', '电话', '状态'], desc: '表头文本' },
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
      { type: 'text', value: ['用户名', '邮箱', '角色', '状态'], desc: '表头文本' },
    ]
  },
  {
    name: '角色权限',
    route: '/system/roles',
    checks: [
      { type: 'selector', value: '.el-table, table', desc: '角色表格' },
      { type: 'selector', value: '.el-button, button', desc: '操作按钮' },
      { type: 'text', value: ['角色', '权限', '描述'], desc: '关键文本' },
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
  _print(`=== user-web 核心页面深度测试: ${coreTests.length} 个页面 ===\n`);

  const browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({
    viewport: { width: 1280, height: 800 }
  });

  const results = [];

  for (let i = 0; i < coreTests.length; i++) {
    const test = coreTests[i];
    const url = BASE_URL + HASH_PREFIX + test.route;
    const result = { name: test.name, route: test.route, status: 'unknown', checks: [], errors: [] };

    try {
      const page = await context.newPage();
      const consoleErrors = [];

      page.on('console', msg => {
        if (msg.type() === 'error') {
          consoleErrors.push(msg.text().substring(0, 200));
        }
      });

      const response = await page.goto(url, { waitUntil: 'networkidle', timeout: 20000 }).catch(() => null);

      if (!response) {
        result.status = 'timeout';
      } else {
        await page.waitForTimeout(1000);

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
      }

      result.errors = consoleErrors.slice(0, 3);
      await page.close();
    } catch (e) {
      result.status = 'error';
      result.errors.push(e.message.substring(0, 200));
    }

    results.push(result);
    const icon = result.status === 'ok' ? '✓' : result.status === 'partial' ? '⚠' : '✗';
    _print(`${icon} [${i + 1}/${coreTests.length}] ${test.name} (${test.route}) => ${result.status}`);
    for (const c of result.checks) {
      _print(`   ${c.passed ? '✓' : '✗'} ${c.desc}: ${c.detail}`);
    }
    if (result.errors.length > 0) {
      _print(`   ! 控制台错误: ${result.errors[0]}`);
    }
  }

  await browser.close();

  const reportPath = path.join(process.cwd(), 'deep-test-report.json');
  fs.writeFileSync(reportPath, JSON.stringify(results, null, 2));

  const ok = results.filter(r => r.status === 'ok').length;
  const partial = results.filter(r => r.status === 'partial').length;
  const fail = results.filter(r => r.status === 'fail' || r.status === 'error' || r.status === 'timeout').length;

  _print(`\n=== 深度测试完成 ===`);
  _print(`通过: ${ok} | 部分通过: ${partial} | 失败: ${fail}`);
  _print(`报告: ${reportPath}`);
}

main().catch(e => {
  console.error('测试脚本异常:', e);
  process.exit(1);
});
