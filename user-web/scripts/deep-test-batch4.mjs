import { print as _print } from './lib/stdout.cjs';
import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';

const ADMIN_PASSWORD = process.env.E2E_ADMIN_PASSWORD;
if (!ADMIN_PASSWORD) { console.error('E2E_ADMIN_PASSWORD 未设置，拒绝运行'); process.exit(1); }

const BASE_URL = 'http://localhost:8211';

// 第四批：geo-tools 子页 + system 子页 + 分析/工作台类
const batch4 = [
  { name: 'GEO品牌配置', route: '/geo-tools/brand-config', type: 'page', texts: ['品牌', 'Brand', '配置'] },
  { name: 'GEO SEO基建', route: '/geo-tools/seo-infra', type: 'page', texts: ['SEO', '基建', '站点'] },
  { name: 'GEO关键词挖掘', route: '/geo-tools/keyword-mining', type: 'page', texts: ['关键词', 'Keyword', '挖掘'] },
  { name: 'GEO内容创作', route: '/geo-tools/content-creation', type: 'page', texts: ['内容', '创作', 'Content'] },
  { name: 'GEO报告', route: '/geo-tools/reports', type: 'page', texts: ['报告', 'Report', 'GEO'] },
  { name: 'GEO实体图谱', route: '/geo-tools/entity-graph', type: 'page', texts: ['实体', 'Entity', '图谱'] },
  { name: '可观测配置', route: '/system/obs-config', type: 'page', texts: ['可观测', 'Observability', '监控', '配置'] },
  { name: '素材库', route: '/system/material-library', type: 'page', texts: ['素材', 'Material', '库'] },
  { name: '系统监控', route: '/system/monitor', type: 'page', texts: ['监控', 'Monitor', '系统'] },
  { name: '链路追踪', route: '/system/trace', type: 'page', texts: ['追踪', 'Trace', '链路'] },
  { name: '系统指南', route: '/system/guide', type: 'page', texts: ['指南', 'Guide', '系统'] },
  { name: 'RAG总览', route: '/system/rag-overview', type: 'page', texts: ['RAG', '总览', '知识'] },
  { name: '配置参数', route: '/system/config-params', type: 'page', texts: ['配置', '参数', 'Config'] },
  { name: '权限列表', route: '/system/permissions', type: 'page', texts: ['权限', 'Permission', '角色'] },
  { name: '销售驾驶舱', route: '/sales-cockpit', type: 'page', texts: ['销售', '驾驶舱', 'Sales'] },
  { name: '销售工作台', route: '/sales-workbench', type: 'page', texts: ['工作台', 'Workbench', '销售'] },
  { name: 'AB实验', route: '/abExperiment/list', type: 'page', texts: ['AB', '实验', 'Experiment'] },
  { name: '队列路径分析', route: '/analytics/cohort-path', type: 'page', texts: ['队列', 'Cohort', '分析'] },
  { name: '自定义报表', route: '/customReport/list', type: 'page', texts: ['报表', 'Report', '自定义'] },
  { name: '大屏构建', route: '/dashboardScreen/builder', type: 'page', texts: ['看板', '组件', '添加', '保存'] },
  { name: 'i18n看板', route: '/i18n/dashboard', type: 'page', texts: ['i18n', '国际化', '语言'] },
  { name: '线索挖掘', route: '/lead-mining/index', type: 'page', texts: ['线索', '挖掘', 'Lead'] },
  { name: '短链', route: '/shortLink', type: 'page', texts: ['短链', 'Short', '链接'] },
  { name: 'WhatsApp Cloud', route: '/whatsapp-cloud', type: 'page', texts: ['WhatsApp', 'Cloud', '云'] },
  { name: '标签分群', route: '/tagSegmentation/list', type: 'page', texts: ['标签', '分群', 'Tag'] },
];

const is404 = (text) => text.includes('404 Page Does not Exist') || text.includes('页面不存在');

async function main() {
  _print(`=== user-web 深度测试 第四批（${batch4.length} 条） ===\n`);

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

  for (let i = 0; i < batch4.length; i++) {
    const test = batch4[i];
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
    _print(`${icon} [${i + 1}/${batch4.length}] ${test.name} (${test.route}) => ${result.status}`);
    for (const c of result.checks) {
      _print(`   ${c.passed ? '✓' : '✗'} ${c.desc}: ${c.detail}`);
    }
    if (result.errors.length > 0) _print(`   ! 错误: ${result.errors[0]}`);
  }

  await browser.close();

  const reportPath = path.join(process.cwd(), 'deep-test-batch4-report.json');
  fs.writeFileSync(reportPath, JSON.stringify(results, null, 2));

  const ok = results.filter(r => r.status === 'ok').length;
  const partial = results.filter(r => r.status === 'partial').length;
  const fail = results.filter(r => ['fail', 'error', 'timeout'].includes(r.status)).length;

  _print(`\n=== 第四批深度测试完成 ===`);
  _print(`通过: ${ok} | 部分通过: ${partial} | 失败: ${fail}`);
  _print(`报告: ${reportPath}`);
}

main().catch(e => {
  console.error('测试脚本异常:', e);
  process.exit(1);
});
