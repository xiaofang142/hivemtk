import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';

const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD;
if (!ADMIN_PASSWORD) { console.error('E2E_ADMIN_PASSWORD 未设置，拒绝运行'); process.exit(1); }

const BASE_URL = 'http://localhost:8211';

// 第五批：geo-tools 子页 + system 子页 + 分析/工作台类
const batch5 = [
  { name: '知识批量导入', route: '/knowledge/batch-import', type: 'page', texts: ['导入', 'Import', '批量'] },
  { name: '知识沙盒', route: '/knowledge/playground', type: 'page', texts: ['沙盒', 'Playground', '测试'] },
  { name: '知识统计', route: '/knowledge/statistics', type: 'page', texts: ['统计', 'Statistics', '知识'] },
  { name: '知识OpenAPI', route: '/knowledge/openapi', type: 'page', texts: ['OpenAPI', 'API', '文档'] },
  { name: 'GEO推送中心', route: '/geo-tools/push-center', type: 'page', texts: ['推送', 'Push', '中心'] },
  { name: 'GEO平台发布', route: '/geo-tools/platform-publish', type: 'page', texts: ['平台', '发布', 'Publish'] },
  { name: 'GEO工作流', route: '/geo-tools/workflow', type: 'page', texts: ['工作流', 'Workflow'] },
  { name: 'GEO知识库', route: '/geo-tools/knowledge-base', type: 'page', texts: ['知识', '库', 'GEO'] },
  { name: 'GEO可见性', route: '/geo-tools/visibility', type: 'page', texts: ['可见', 'Visibility'] },
  { name: 'GEO告警', route: '/geo-tools/alerts', type: 'page', texts: ['告警', 'Alert'] },
  { name: '客服坐席状态', route: '/customerService/agentStatus', type: 'page', texts: ['坐席', '状态', 'Agent'] },
  { name: '快捷回复', route: '/customerService/quickReply', type: 'page', texts: ['快捷', '回复', 'Reply'] },
  { name: '满意度', route: '/customerService/csat', type: 'page', texts: ['满意', 'CSAT', '评分'] },
  { name: 'OneID列表', route: '/oneid/list', type: 'page', texts: ['OneID', 'ID', '列表'] },
  { name: '画像列表', route: '/persona/list', type: 'page', texts: ['画像', 'Persona'] },
  { name: '术语表', route: '/glossary', type: 'page', texts: ['术语', 'Glossary'] },
  { name: '集成列表', route: '/integration/list', type: 'page', texts: ['集成', 'Integration'] },
  { name: '统一消息', route: '/unifiedMessage/list', type: 'page', texts: ['消息', 'Message', '统一'] },
  { name: '企微账号', route: '/wecomAccount/list', type: 'page', texts: ['企微', 'WeCom', '账号'] },
  { name: '话术模板', route: '/scriptTemplate/list', type: 'page', texts: ['话术', '模板', 'Script'] },
  { name: 'SOP Agent', route: '/sopAgent/list', type: 'page', texts: ['SOP', 'Agent'] },
  { name: '营销流程', route: '/marketingFlow/list', type: 'page', texts: ['营销', '流程', 'Flow'] },
  { name: 'MCP凭证', route: '/mcp/credentials', type: 'page', texts: ['MCP', '凭证', 'Credential'] },
  { name: 'TikTok', route: '/tiktok', type: 'page', texts: ['TikTok', '抖音', '账号'] },
  { name: '通知中心', route: '/notifications', type: 'page', texts: ['通知', 'Notification'] },
];

const is404 = (text) => text.includes('404 Page Does not Exist') || text.includes('页面不存在');

async function main() {
  console.log(`=== user-web 深度测试 第五批（${batch5.length} 条） ===\n`);

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

  for (let i = 0; i < batch5.length; i++) {
    const test = batch5[i];
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
    console.log(`${icon} [${i + 1}/${batch5.length}] ${test.name} (${test.route}) => ${result.status}`);
    for (const c of result.checks) {
      console.log(`   ${c.passed ? '✓' : '✗'} ${c.desc}: ${c.detail}`);
    }
    if (result.errors.length > 0) console.log(`   ! 错误: ${result.errors[0]}`);
  }

  await browser.close();

  const reportPath = path.join(process.cwd(), 'deep-test-batch5-report.json');
  fs.writeFileSync(reportPath, JSON.stringify(results, null, 2));

  const ok = results.filter(r => r.status === 'ok').length;
  const partial = results.filter(r => r.status === 'partial').length;
  const fail = results.filter(r => ['fail', 'error', 'timeout'].includes(r.status)).length;

  console.log(`\n=== 第五批深度测试完成 ===`);
  console.log(`通过: ${ok} | 部分通过: ${partial} | 失败: ${fail}`);
  console.log(`报告: ${reportPath}`);
}

main().catch(e => {
  console.error('测试脚本异常:', e);
  process.exit(1);
});
