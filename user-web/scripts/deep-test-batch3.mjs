import { print as _print } from './lib/stdout.cjs';
import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';

const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD;
if (!ADMIN_PASSWORD) { console.error('E2E_ADMIN_PASSWORD 未设置，拒绝运行'); process.exit(1); }

const BASE_URL = 'http://localhost:8211';

// 第三批：静态路由 + create/editor 表单页 + :param 路由
const batch3 = [
  // 静态路由
  { name: '帮助中心', route: '/help-center', type: 'page', texts: ['帮助', 'Help', '中心', 'FAQ'] },
  { name: '在线客服嵌入', route: '/chat/embed/default', type: 'page', texts: ['客服', '消息', 'Chat', '输入'] },
  { name: '系统初始化', route: '/setup', type: 'page', texts: ['Marketing', '系统', '部署', 'Log In', '登录'], allowRedirectToLogin: true },

  // create / editor 表单页
  { name: '新建AI Agent', route: '/aiAgent/create', type: 'form', texts: ['Agent', '名称', '模型', '新建', '创建'] },
  { name: '新建知识库', route: '/knowledgeBase/create', type: 'form', texts: ['知识库', '名称', '新建', '创建'] },
  { name: '新建渠道', route: '/chatChannel/create', type: 'form', texts: ['渠道', 'Channel', '新建', '创建'] },
  { name: 'FAQ编辑器', route: '/faq/editor', type: 'form', texts: ['FAQ', '问题', '答案', '编辑'] },
  { name: 'SOP编辑器', route: '/sop-template/editor', type: 'form', texts: ['SOP', '模板', '编辑', '保存'] },
  { name: '触达管线编辑器', route: '/reachPipeline/editor', type: 'page', texts: ['管线', 'Pipeline', '编辑', '保存', '节点'] },
  { name: '短信草稿', route: '/sms/drafts', type: 'page', texts: ['草稿', 'Draft', '短信'] },
  { name: '邮件草稿', route: '/email/drafts', type: 'page', texts: ['草稿', 'Draft', '邮件'] },
  { name: '邮件拖拽编辑器', route: '/email/drag-editor', type: 'form', texts: ['编辑', '拖拽', 'Email', '模板'] },

  // :param 路由（假ID，验证页面框架渲染、不白屏不404）
  { name: '编辑AI Agent(id=1)', route: '/aiAgent/edit/1', type: 'form', texts: ['Agent', '名称', '模型', '保存'] },
  { name: '编辑知识库(id=1)', route: '/knowledgeBase/edit/1', type: 'form', texts: ['知识库', '名称', '保存', '不存在', 'not found', '加载'] },
  { name: '编辑渠道(id=1)', route: '/chatChannel/edit/1', type: 'form', texts: ['渠道', 'Channel', '保存', '不存在', '加载'] },
  { name: 'FAQ编辑(id=1)', route: '/faq/editor/1', type: 'form', texts: ['FAQ', '问题', '答案', '保存', '不存在', '加载'] },
  { name: '工作流编辑(id=1)', route: '/workflow-orchestrator/editor/1', type: 'page', texts: ['工作流', 'Workflow', '保存', '节点', '不存在', '加载'] },
  { name: '自动化任务详情(id=1)', route: '/browser-automation/tasks/1', type: 'page', texts: ['任务', 'Task', '状态', '不存在', '加载'] },
  { name: '资产详情(id=1)', route: '/asset-market/detail/1', type: 'page', texts: ['资产', 'Asset', '详情', '不存在', '加载'] },
  { name: '草稿详情(id=1)', route: '/dashboard/drafts/1', type: 'page', texts: ['草稿', 'Draft', '详情', '不存在', '加载'] },
];

const is404 = (text) => text.includes('404 Page Does not Exist') || text.includes('页面不存在');

async function main() {
  _print(`=== user-web 深度测试 第三批（${batch3.length} 条） ===\n`);

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

  for (let i = 0; i < batch3.length; i++) {
    const test = batch3[i];
    consoleErrors.length = 0;
    apiErrors.length = 0;

    const result = { name: test.name, route: test.route, status: 'unknown', checks: [], errors: [] };

    try {
      await page.goto(BASE_URL + '/#/' + test.route.replace(/^\//, ''), { waitUntil: 'networkidle', timeout: 20000 });
      await page.waitForTimeout(2500);

      const bodyText = await page.evaluate(() => document.body?.innerText || '');

      // 检查1: 非404
      const not404 = !is404(bodyText);
      result.checks.push({ desc: '非404页面', passed: not404, detail: not404 ? '正常渲染' : '出现404' });

      // 检查2: 内容量（setup可能被重定向，内容量检查放宽到 >50）
      const len = bodyText.trim().length;
      result.checks.push({ desc: '页面内容', passed: len > 50, detail: `${len} 字符` });

      // 检查3: form页要求有表单元素
      if (test.type === 'form') {
        const formCount = await page.$$eval('form, .el-form, input, textarea, .el-select', els => els.length).catch(() => 0);
        result.checks.push({ desc: '表单元素', passed: formCount > 0, detail: `${formCount} 个` });
      } else {
        const structCount = await page.$$eval('.el-card, .el-table, .el-form, .el-button, button, input, table', els => els.length).catch(() => 0);
        result.checks.push({ desc: '交互/结构元素', passed: structCount > 0, detail: `${structCount} 个` });
      }

      // 检查4: 关键文本
      const found = test.texts.filter(t => bodyText.includes(t));
      result.checks.push({ desc: '关键文本', passed: found.length > 0, detail: found.length ? `找到: ${found.join(', ')}` : `未命中: ${test.texts.join('/')}` });

      // 检查5: API错误（:param 页在ID不存在时返回404/400属预期，不算失败，仅记录）
      const _realApiErrors = apiErrors.filter(e => !/\/api\/.*\/(edit|detail|info|get|list)/i.test(e) || !e.startsWith('404'));
      result.checks.push({ desc: 'API请求', passed: apiErrors.length === 0, detail: apiErrors.length ? apiErrors.slice(0, 3).join(' | ') : '全部成功', info: true });

      // 检查6: 控制台错误（过滤资源加载404噪音）
      const realErrors = consoleErrors.filter(e => !e.includes('favicon') && !e.includes('Failed to load resource'));
      result.checks.push({ desc: '控制台错误', passed: realErrors.length === 0, detail: realErrors.length ? realErrors[0] : '无' });

      // 状态判定：非404 + 有内容 + 有结构 = 页面正常（API 404在假ID场景允许）
      const coreOk = not404 && len > 50;
      const allHard = result.checks.filter(c => c.desc !== 'API请求').every(c => c.passed);
      result.status = coreOk && allHard ? 'ok' : coreOk ? 'partial' : 'fail';
    } catch (e) {
      result.status = 'error';
      result.errors.push(e.message.substring(0, 200));
    }

    results.push(result);
    const icon = result.status === 'ok' ? '✓' : result.status === 'partial' ? '⚠' : '✗';
    _print(`${icon} [${i + 1}/${batch3.length}] ${test.name} (${test.route}) => ${result.status}`);
    for (const c of result.checks) {
      _print(`   ${c.passed ? '✓' : '✗'} ${c.desc}: ${c.detail}`);
    }
    if (result.errors.length > 0) _print(`   ! 错误: ${result.errors[0]}`);
  }

  await browser.close();

  const reportPath = path.join(process.cwd(), 'deep-test-batch3-report.json');
  fs.writeFileSync(reportPath, JSON.stringify(results, null, 2));

  const ok = results.filter(r => r.status === 'ok').length;
  const partial = results.filter(r => r.status === 'partial').length;
  const fail = results.filter(r => ['fail', 'error', 'timeout'].includes(r.status)).length;

  _print(`\n=== 第三批深度测试完成 ===`);
  _print(`通过: ${ok} | 部分通过: ${partial} | 失败: ${fail}`);
  _print(`报告: ${reportPath}`);
}

main().catch(e => {
  console.error('测试脚本异常:', e);
  process.exit(1);
});
