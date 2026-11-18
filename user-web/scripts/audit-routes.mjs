import { print as _print } from './lib/stdout.cjs';
/**
 * user-web 全路由批量审计脚本
 * 检测：白屏、404、控制台错误、API失败
 */
import { chromium } from 'playwright';
import fs from 'fs';
import path from 'path';

const BASE_URL = 'http://localhost:8211';
const HASH_PREFIX = '/#/';

const routes = [
  // 静态路由
  '/setup', '/login', '/help-center', '/chat/embed/default',
  // 动态路由
  '/abExperiment/list',
  '/aiAgent', '/aiAgent/list', '/aiAgent/create', '/aiAgent/edit/1', '/aiAgent/tools',
  '/aiProductivity/list',
  '/analytics/cohort-path',
  '/approvalTask/list',
  '/asset-bundle/list', '/asset-bundle/playground', '/asset-bundle/playground/1',
  '/asset-bundle/merchant-new', '/asset-bundle/merchant/1',
  '/asset-market', '/asset-market/detail/1', '/asset-market/my-assets', '/asset-market/sync-log',
  '/backup/list', '/backup/enhanced',
  '/badCase/list',
  '/batchOperation/list',
  '/bridge', '/bridge/token', '/token',
  '/browser-automation', '/browser-automation/tasks', '/browser-automation/tasks/create',
  '/browser-automation/tasks/1/edit', '/browser-automation/tasks/1',
  '/browser-automation/sessions/1', '/browser-automation/cron', '/browser-automation/status',
  '/chatChannel/list', '/chatChannel/create', '/chatChannel/edit/1', '/chatChannel/install-guide/1',
  '/churnPrediction/list',
  '/clue/list', '/clue/statistics',
  '/community/list',
  '/conversionFunnel/list',
  '/customReport/list',
  '/customer360/list',
  '/customerEvent/list',
  '/customerJourney/dashboard',
  '/customerService/agentStatus', '/customerService/quickReply', '/customerService/sessionTag',
  '/customerService/aiSuggestion', '/customerService/csat',
  '/customerSession/list',
  '/dashboardScreen/list', '/dashboardScreen/builder',
  '/dialogueMemory/list',
  '/dingtalk-app',
  '/domainPool',
  '/douyinCard', '/douyin/stats', '/douyin-card-stats/1',
  '/email', '/email/drafts', '/email/jobs', '/email/smtp', '/email/info',
  '/email/guide', '/email/deliverability', '/email/drag-editor',
  '/faq', '/faq/list', '/faq/editor', '/faq/editor/1',
  '/feishu', '/feishu/account',
  '/followups/today', '/followups/1',
  '/geo-tools/brand-config', '/geo-tools/seo-infra', '/geo-tools/pusher-config',
  '/geo-tools/site-config', '/geo-tools/schema-templates', '/geo-tools/competitors',
  '/geo-tools/config', '/geo-tools/keyword-mining', '/geo-tools/content-creation',
  '/geo-tools/content-optimize', '/geo-tools/site-publish', '/geo-tools/push-center',
  '/geo-tools/platform-publish', '/geo-tools/workflow', '/geo-tools/knowledge-base',
  '/geo-tools/entity-graph', '/geo-tools/funnel-dashboard', '/geo-tools/index-tracking',
  '/geo-tools/visibility', '/geo-tools/sov-board', '/geo-tools/crawler-stats',
  '/geo-tools/verification', '/geo/decision-report', '/geo-tools/reports', '/geo-tools/alerts',
  '/glossary',
  '/i18n/dashboard',
  '/inbox/list',
  '/integration/list',
  '/intentRecognition/list',
  '/kb/connectors', '/cap/matrix',
  '/knowledge/management', '/knowledge/batch-import', '/knowledge/playground',
  '/knowledge/connectors', '/knowledge/feedbacks', '/knowledge/tokens',
  '/knowledge/external', '/knowledge/statistics', '/knowledge/openapi', '/knowledge/rag-eval',
  '/knowledgeBase', '/knowledgeBase/list', '/knowledgeBase/create', '/knowledgeBase/edit/1',
  '/kuaishouCard', '/kuaishou/stats', '/kuaishou-card-stats/1',
  '/lead-mining/index',
  '/livecode',
  '/llmRouting/list', '/llmRouting/cost',
  '/marketingFlow/list',
  '/mcp/credentials',
  '/messageHub/list', '/messageHub/dashboard',
  '/objection/list',
  '/oneid/list', '/oneid/conflicts', '/oneid/merge-rules',
  '/operationLog/list', '/operationLog/enhanced',
  '/ops-overview', '/sales-cockpit',
  '/dashboard/drafts', '/dashboard/drafts/1',
  '/system/permissions',
  '/persona/list',
  '/platformAccount/list',
  '/qq', '/qq/account',
  '/system/rag-product-config', '/system/rag-account', '/system/rag-product',
  '/reachPipeline/list', '/reachPipeline/editor/1',
  '/system/roles',
  '/sales-workbench',
  '/scriptTemplate/list', '/scriptTemplate/ab-stats',
  '/securityAudit/list',
  '/shortLink', '/shortLink/stats',
  '/sms/list', '/sms/drafts', '/sms/jobs', '/sms/config',
  '/sopAgent/list',
  '/sop-template', '/sop-template/list', '/sop-template/market',
  '/sop-template/editor', '/sop-template/editor/1',
  '/system/config', '/system/obs-config', '/system/material-library',
  '/system/monitor', '/system/trace', '/system/guide', '/system/rag-overview',
  '/system/automation-hub', '/system/config-params', '/system/ltc-config',
  '/system/users',
  '/tagSegmentation/list',
  '/telegram', '/telegram/account', '/telegram/gate',
  '/tiktok', '/tiktok/list', '/tiktok/stats', '/tiktok-card-stats/1',
  '/confidence/panel', '/humanize/panel', '/feedbackLoop/panel',
  '/unifiedMessage/list',
  '/userSegment/list', '/userSegment/rfm-matrix', '/userSegment/builder',
  '/wecomAccount/list', '/wecomAccount/data',
  '/whatsapp', '/whatsapp/account', '/whatsapp/drafts', '/whatsapp/jobs',
  '/whatsapp/lead-group-selection', '/whatsapp/group-messaging', '/whatsapp/bulk-matrix',
  '/whatsapp-cloud',
  '/workflow-orchestrator/list', '/workflow-orchestrator/editor/1',
  '/workflow-orchestrator/execution/1',
  '/xianyuCard', '/xianyu/stats', '/xianyu-card-stats/1',
  '/xiaohongshuCard', '/xiaohongshu/stats', '/xiaohongshu-card-stats/1',
  '/profile', '/notifications',
];

const results = [];
let browser;

async function main() {
  _print(`=== user-web 路由审计开始: ${routes.length} 条路由 ===\n`);

  browser = await chromium.launch({ headless: true });
  const context = await browser.newContext({
    viewport: { width: 1280, height: 800 }
  });

  for (let i = 0; i < routes.length; i++) {
    const route = routes[i];
    const url = BASE_URL + HASH_PREFIX + route;
    const result = { route, url, status: 'unknown', errors: [], apiErrors: [], renderOk: false };

    try {
      const page = await context.newPage();
      const consoleErrors = [];
      const apiErrors = [];

      page.on('console', msg => {
        if (msg.type() === 'error') {
          consoleErrors.push(msg.text().substring(0, 300));
        }
      });

      page.on('response', resp => {
        const status = resp.status();
        if (status >= 400) {
          const url = resp.url();
          if (url.includes('/api/') || url.includes('/files/')) {
            apiErrors.push(`${status} ${url.substring(0, 150)}`);
          }
        }
      });

      const response = await page.goto(url, { waitUntil: 'networkidle', timeout: 15000 }).catch(() => null);

      if (!response) {
        result.status = 'timeout';
      } else {
        const bodyText = await page.evaluate(() => document.body?.innerText?.substring(0, 500) || '');
        const is404 = bodyText.includes('页面不存在') || bodyText.includes('404') || bodyText.includes('NotFound');
        const isBlank = bodyText.trim().length < 20;
        const hasContent = await page.evaluate(() => {
          const el = document.querySelector('.el-container, .app-container, main, #app > div');
          return el && el.children.length > 0;
        }).catch(() => false);

        if (is404) {
          result.status = '404';
        } else if (isBlank) {
          result.status = 'blank';
        } else {
          result.status = 'ok';
          result.renderOk = hasContent;
        }
      }

      result.errors = consoleErrors.slice(0, 5);
      result.apiErrors = apiErrors.slice(0, 5);

      await page.close();
    } catch (e) {
      result.status = 'error';
      result.errors.push(e.message.substring(0, 300));
    }

    results.push(result);
    const icon = result.status === 'ok' ? '✓' : result.status === 'blank' ? '⚠' : '✗';
    _print(`${icon} [${i + 1}/${routes.length}] ${route} => ${result.status}${result.errors.length ? ' (' + result.errors.length + ' errors)' : ''}`);
  }

  await browser.close();

  // 生成报告
  const reportPath = path.join(process.cwd(), 'route-audit-report.json');
  fs.writeFileSync(reportPath, JSON.stringify(results, null, 2));

  const ok = results.filter(r => r.status === 'ok').length;
  const fail = results.filter(r => r.status !== 'ok').length;
  const withErrors = results.filter(r => r.errors.length > 0).length;
  const withApiErrors = results.filter(r => r.apiErrors.length > 0).length;

  _print(`\n=== 审计完成 ===`);
  _print(`正常: ${ok} | 异常: ${fail} | 控制台错误: ${withErrors} | API错误: ${withApiErrors}`);
  _print(`报告: ${reportPath}`);

  // 输出问题路由
  const issues = results.filter(r => r.status !== 'ok' || r.errors.length > 0 || r.apiErrors.length > 0);
  if (issues.length > 0) {
    _print(`\n=== 问题路由 (${issues.length}) ===`);
    for (const r of issues) {
      _print(`  ${r.route}: ${r.status}, ${r.errors.length} console errors, ${r.apiErrors.length} API errors`);
      if (r.errors.length > 0) _print(`    console: ${r.errors[0]}`);
      if (r.apiErrors.length > 0) _print(`    api: ${r.apiErrors[0]}`);
    }
  }
}

main().catch(e => {
  console.error('审计脚本异常:', e);
  process.exit(1);
});
