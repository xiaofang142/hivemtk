/**
 * 页面导览注册表（任务六 · 新手引导文案优化）
 *
 * 为什么单独一个文件：引导是「按页配置」的，一个页面的说明必须跟着那个页面的
 * 真实功能走。把选择器与文案键集中在这里，是为了让「读页面 → 写说明 → 挂选择器」
 * 这件事一处收敛，不必在 useOnboarding.ts 里堆几百个模块。
 *
 * 四条硬约束（踩过才知道）：
 *  1. `element` 必须是**该页面 DOM 里真实存在**的选择器。写错不会报错，只会
 *     让 driver.js 退化成 0×0 的 dummy element —— 弹窗跑到屏幕正中，用户看到
 *     一个没有高亮的空气泡。所以每条配置都要在浏览器里点过。
 *  2. `pageKey` 只能由字母数字下划线组成。它要拼进 i18n 键
 *     （setup.onboarding.page.<pageKey>.…），而 i18n 门禁只认
 *     `^[A-Za-z0-9_]+(\.[A-Za-z0-9_]+)+$` 的点分键，出现连字符会被判成
 *     「点分键解析不到词条」而把 CI 门禁打红。
 *  3. `route` 是**路由模式**（带 :param 的那一份），不是实例路径。
 *     运行时用 route.matched 还原模式后查表，动态段才能命中同一条导览。
 *  4. 文案用**静态的点分键调用**取，不用变量拼接的动态取键：
 *     i18n 门禁的「真 i18n 键占比」只统计静态实参里的点分键，动态取键会被
 *     当成没接 i18n，把占比棘轮打红；静态取键同时也是词条「有消费方」的凭据，
 *     避免这些词条被判成孤儿。
 */

/** 导览的一步：高亮一个元素 + 一段说明（文案已按当前语言解析） */
export interface OnboardingStep {
  /** 目标元素的 CSS 选择器（必须是该页真实存在的语义化 class） */
  element: string
  /** 弹窗标题 */
  title: string
  /** 弹窗说明 */
  description: string
  /** 气泡相对高亮元素的位置 */
  side?: 'top' | 'bottom' | 'left' | 'right'
  /** 气泡与高亮元素的对齐方式 */
  align?: 'start' | 'center' | 'end'
}

/** 一个页面的完整导览配置 */
export interface PageTour {
  /** 路由模式，例如 /aiAgent/edit/:id */
  route: string
  /** i18n 命名空间片段，例如 aiAgentEdit */
  pageKey: string
  /** 步骤列表 */
  steps: OnboardingStep[]
}

/**
 * 构建页面导览表。
 *
 * 每次调用都用**当前语言**的 t 重新解析文案，所以切换语言后下一次打开导览
 * 就是新语言（规则 5.5「多语言切换后引导文案即时生效」）。
 * 调用方按 locale 缓存这份表，不必每次路由变化都重算。
 */
export function createPageTours(t: (key: string) => string): Record<string, PageTour> {
  return {
    '/commerce/operations': {
      route: '/commerce/operations',
      pageKey: 'commerceOperations',
      steps: [
        { element: '.commerce-target', title: t('setup.onboarding.page.commerceOperations.title'), description: t('setup.onboarding.page.commerceOperations.target'), side: 'top' },
        { element: '.commerce-create', title: t('setup.onboarding.page.commerceOperations.title'), description: t('setup.onboarding.page.commerceOperations.draft'), side: 'top' },
        { element: '.commerce-list', title: t('setup.onboarding.page.commerceOperations.title'), description: t('setup.onboarding.page.commerceOperations.ledger'), side: 'top' },
        { element: '.commerce-detail', title: t('setup.onboarding.page.commerceOperations.title'), description: t('setup.onboarding.page.commerceOperations.evidence'), side: 'top' }
      ]
    },
    '/integration/list': {
      route: '/integration/list',
      pageKey: 'merchantConnection',
      steps: [
        { element: '.merchant-connection-panel', title: t('setup.onboarding.page.merchantConnection.title'), description: t('setup.onboarding.page.merchantConnection.summary'), side: 'top' },
        { element: '.merchant-connection-target', title: t('setup.onboarding.page.merchantConnection.target'), description: t('setup.onboarding.page.merchantConnection.targetSummary'), side: 'top' },
        { element: '.merchant-connection-status', title: t('setup.onboarding.page.merchantConnection.status'), description: t('setup.onboarding.page.merchantConnection.statusSummary'), side: 'top' },
        { element: '.merchant-connection-actions', title: t('setup.onboarding.page.merchantConnection.actions'), description: t('setup.onboarding.page.merchantConnection.actionsSummary'), side: 'top' }
      ]
    },
    '/system/config': {
      route: '/system/config',
      pageKey: 'pilotContract',
      steps: [
        { element: '.pilot-contract-panel', title: t('setup.onboarding.page.pilotContract.title'), description: t('setup.onboarding.page.pilotContract.summary'), side: 'top' },
        { element: '.pilot-contract-identity', title: t('setup.onboarding.page.pilotContract.identity'), description: t('setup.onboarding.page.pilotContract.identitySummary'), side: 'top' },
        { element: '.pilot-contract-blockers', title: t('setup.onboarding.page.pilotContract.blockers'), description: t('setup.onboarding.page.pilotContract.blockersSummary'), side: 'top' },
        { element: '.pilot-contract-editor', title: t('setup.onboarding.page.pilotContract.draft'), description: t('setup.onboarding.page.pilotContract.draftSummary'), side: 'top' }
      ]
    },
    '/messageHub/list': {
      route: '/messageHub/list',
      pageKey: 'messageHubList',
      steps: [
        {
          element: '.message-hub-container .stats-row',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.messageHubList.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.message-hub-container .search-form',
          title: t('setup.onboarding.page.messageHubList.step2.title'),
          description: t('setup.onboarding.page.messageHubList.step2.description'),
          side: 'bottom',
          align: 'center'
        }
      ]
    },
    '/abExperiment/list': {
      route: '/abExperiment/list',
      pageKey: 'abExperimentList',
      steps: [
        {
          element: '.ab-experiment .filter-container',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.abExperimentList.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.ab-experiment .el-table',
          title: t('setup.onboarding.page.abExperimentList.step2.title'),
          description: t('setup.onboarding.page.abExperimentList.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/aiAgent/list': {
      route: '/aiAgent/list',
      pageKey: 'aiAgentList',
      steps: [
        {
          element: '.ai-agent-list-page .header-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.aiAgentList.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.ai-agent-list-page .filter-card',
          title: t('setup.onboarding.page.aiAgentList.step2.title'),
          description: t('setup.onboarding.page.aiAgentList.step2.description'),
          side: 'bottom',
          align: 'center'
        }
      ]
    },
    '/aiProductivity/list': {
      route: '/aiProductivity/list',
      pageKey: 'aiProductivityList',
      steps: [
        {
          element: '.ai-productivity .kpi-row',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.aiProductivityList.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.ai-productivity .trend-card',
          title: t('setup.onboarding.page.aiProductivityList.step2.title'),
          description: t('setup.onboarding.page.aiProductivityList.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/analytics/cohort-path': {
      route: '/analytics/cohort-path',
      pageKey: 'cohortPath',
      steps: [
        {
          element: '.cohort-path .el-tabs__header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.cohortPath.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.cohort-path .el-tabs__content',
          title: t('setup.onboarding.page.cohortPath.step2.title'),
          description: t('setup.onboarding.page.cohortPath.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/approvalTask/list': {
      route: '/approvalTask/list',
      pageKey: 'approvalTaskList',
      steps: [
        {
          element: '.approval-task .counts',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.approvalTaskList.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.approval-task .el-form',
          title: t('setup.onboarding.page.approvalTaskList.step2.title'),
          description: t('setup.onboarding.page.approvalTaskList.step2.description'),
          side: 'bottom',
          align: 'center'
        }
      ]
    },
    '/aiAgent/create': {
      route: '/aiAgent/create',
      pageKey: 'aiAgentEdit',
      steps: [
        {
          element: '.ai-agent-edit-page .header-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.aiAgentEdit.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.ai-agent-edit-page .section-card',
          title: t('setup.onboarding.page.aiAgentEdit.step2.title'),
          description: t('setup.onboarding.page.aiAgentEdit.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/aiAgent/edit/:id': {
      route: '/aiAgent/edit/:id',
      pageKey: 'aiAgentEdit',
      steps: [
        {
          element: '.ai-agent-edit-page .header-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.aiAgentEdit.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.ai-agent-edit-page .section-card',
          title: t('setup.onboarding.page.aiAgentEdit.step2.title'),
          description: t('setup.onboarding.page.aiAgentEdit.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/asset-bundle/list': {
      route: '/asset-bundle/list',
      pageKey: 'assetBundleList',
      steps: [
        {
          element: '.asset-bundle-list .filter-bar',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.assetBundleList.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.asset-bundle-list .hot-bar',
          title: t('setup.onboarding.page.assetBundleList.step2.title'),
          description: t('setup.onboarding.page.assetBundleList.step2.description'),
          side: 'bottom',
          align: 'center'
        }
      ]
    },
    '/asset-bundle/playground': {
      route: '/asset-bundle/playground',
      pageKey: 'assetBundlePlayground',
      steps: [
        {
          element: '.playground .header-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.assetBundlePlayground.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.playground .left-panel',
          title: t('setup.onboarding.page.assetBundlePlayground.step2.title'),
          description: t('setup.onboarding.page.assetBundlePlayground.step2.description'),
          side: 'right',
          align: 'center'
        }
      ]
    },
    '/asset-bundle/merchant-new': {
      route: '/asset-bundle/merchant-new',
      pageKey: 'assetBundleMerchantNew',
      steps: [
        {
          element: '.merchant-editor .status-bar',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.assetBundleMerchantNew.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.merchant-editor .el-form',
          title: t('setup.onboarding.page.assetBundleMerchantNew.step2.title'),
          description: t('setup.onboarding.page.assetBundleMerchantNew.step2.description'),
          side: 'bottom',
          align: 'center'
        }
      ]
    },
    '/asset-market': {
      route: '/asset-market',
      pageKey: 'assetMarketMarket',
      steps: [
        {
          element: '.asset-market .filter-bar',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.assetMarketMarket.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.asset-market .asset-grid',
          title: t('setup.onboarding.page.assetMarketMarket.step2.title'),
          description: t('setup.onboarding.page.assetMarketMarket.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/asset-market/detail/:id': {
      route: '/asset-market/detail/:id',
      pageKey: 'assetMarketDetail',
      steps: [
        {
          element: '.asset-detail .el-page-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.assetMarketDetail.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.asset-detail',
          title: t('setup.onboarding.page.assetMarketDetail.step2.title'),
          description: t('setup.onboarding.page.assetMarketDetail.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/asset-market/my-assets': {
      route: '/asset-market/my-assets',
      pageKey: 'assetMarketMyAssets',
      steps: [
        {
          element: '.my-assets .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.assetMarketMyAssets.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.my-assets .el-table',
          title: t('setup.onboarding.page.assetMarketMyAssets.step2.title'),
          description: t('setup.onboarding.page.assetMarketMyAssets.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/asset-market/sync-log': {
      route: '/asset-market/sync-log',
      pageKey: 'assetMarketSyncLog',
      steps: [
        {
          element: '.sync-log .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.assetMarketSyncLog.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.sync-log .el-form',
          title: t('setup.onboarding.page.assetMarketSyncLog.step2.title'),
          description: t('setup.onboarding.page.assetMarketSyncLog.step2.description'),
          side: 'bottom',
          align: 'center'
        }
      ]
    },
    '/backup/list': {
      route: '/backup/list',
      pageKey: 'backupList',
      steps: [
        {
          element: '.backup-page .stat-row',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.backupList.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.backup-page .header-card',
          title: t('setup.onboarding.page.backupList.step2.title'),
          description: t('setup.onboarding.page.backupList.step2.description'),
          side: 'bottom',
          align: 'center'
        }
      ]
    },
    '/badCase/list': {
      route: '/badCase/list',
      pageKey: 'badCaseList',
      steps: [
        {
          element: '.bad-case .stats',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.badCaseList.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.bad-case .el-form',
          title: t('setup.onboarding.page.badCaseList.step2.title'),
          description: t('setup.onboarding.page.badCaseList.step2.description'),
          side: 'bottom',
          align: 'center'
        }
      ]
    },
    '/batchOperation/list': {
      route: '/batchOperation/list',
      pageKey: 'batchOperationList',
      steps: [
        {
          element: '.batch-operation-page .tool-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.batchOperationList.summary'),
          side: 'right',
          align: 'center'
        },
        {
          element: '.batch-operation-page .history-card',
          title: t('setup.onboarding.page.batchOperationList.step2.title'),
          description: t('setup.onboarding.page.batchOperationList.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/bridge/token': {
      route: '/bridge/token',
      pageKey: 'bridgeToken',
      steps: [
        {
          element: '.p-4 .el-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.bridgeToken.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.p-4 .el-form',
          title: t('setup.onboarding.page.bridgeToken.step2.title'),
          description: t('setup.onboarding.page.bridgeToken.step2.description'),
          side: 'bottom',
          align: 'center'
        }
      ]
    },
    '/browser-automation/tasks': {
      route: '/browser-automation/tasks',
      pageKey: 'browserAutomationList',
      steps: [
        {
          element: '.page .toolbar',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.browserAutomationList.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.page .el-table',
          title: t('setup.onboarding.page.browserAutomationList.step2.title'),
          description: t('setup.onboarding.page.browserAutomationList.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/browser-automation/tasks/create': {
      route: '/browser-automation/tasks/create',
      pageKey: 'browserAutomationEditor',
      steps: [
        {
          element: '.page .el-form',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.browserAutomationEditor.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.page .steps-editor',
          title: t('setup.onboarding.page.browserAutomationEditor.step2.title'),
          description: t('setup.onboarding.page.browserAutomationEditor.step2.description'),
          side: 'left',
          align: 'center'
        }
      ]
    },
    '/browser-automation/tasks/:id/edit': {
      route: '/browser-automation/tasks/:id/edit',
      pageKey: 'browserAutomationEditor',
      steps: [
        {
          element: '.page .el-form',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.browserAutomationEditor.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.page .steps-editor',
          title: t('setup.onboarding.page.browserAutomationEditor.step2.title'),
          description: t('setup.onboarding.page.browserAutomationEditor.step2.description'),
          side: 'left',
          align: 'center'
        }
      ]
    },
    '/browser-automation/tasks/:id': {
      route: '/browser-automation/tasks/:id',
      pageKey: 'browserAutomationDetail',
      steps: [
        {
          element: '.page .header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.browserAutomationDetail.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.page .el-descriptions',
          title: t('setup.onboarding.page.browserAutomationDetail.step2.title'),
          description: t('setup.onboarding.page.browserAutomationDetail.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/browser-automation/sessions/:id': {
      route: '/browser-automation/sessions/:id',
      pageKey: 'browserAutomationMonitor',
      steps: [
        {
          element: '.page .header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.browserAutomationMonitor.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.page .el-card',
          title: t('setup.onboarding.page.browserAutomationMonitor.step2.title'),
          description: t('setup.onboarding.page.browserAutomationMonitor.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/browser-automation/cron': {
      route: '/browser-automation/cron',
      pageKey: 'browserAutomationCron',
      steps: [
        {
          element: '.page .toolbar',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.browserAutomationCron.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.page .el-table',
          title: t('setup.onboarding.page.browserAutomationCron.step2.title'),
          description: t('setup.onboarding.page.browserAutomationCron.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/churnPrediction/list': {
      route: '/churnPrediction/list',
      pageKey: 'churnPredictionList',
      steps: [
        {
          element: '.churn-prediction-page .stats-row',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.churnPredictionList.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.churn-prediction-page .el-tabs__header',
          title: t('setup.onboarding.page.churnPredictionList.step2.title'),
          description: t('setup.onboarding.page.churnPredictionList.step2.description'),
          side: 'bottom',
          align: 'center'
        }
      ]
    },
    '/clue/list': {
      route: '/clue/list',
      pageKey: 'clueList',
      steps: [
        {
          element: '.clue-list-page .stats-row',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.clueList.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.clue-list-page .filter-form',
          title: t('setup.onboarding.page.clueList.step2.title'),
          description: t('setup.onboarding.page.clueList.step2.description'),
          side: 'bottom',
          align: 'center'
        }
      ]
    },
    '/community/list': {
      route: '/community/list',
      pageKey: 'communityList',
      steps: [
        {
          element: '.community-container .page-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.communityList.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.community-container .search-form',
          title: t('setup.onboarding.page.communityList.step2.title'),
          description: t('setup.onboarding.page.communityList.step2.description'),
          side: 'bottom',
          align: 'center'
        }
      ]
    },
    '/conversionFunnel/list': {
      route: '/conversionFunnel/list',
      pageKey: 'conversionFunnelList',
      steps: [
        {
          element: '.conversion-funnel .summary-row',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.conversionFunnelList.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.conversion-funnel .chart-row',
          title: t('setup.onboarding.page.conversionFunnelList.step2.title'),
          description: t('setup.onboarding.page.conversionFunnelList.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/customer360/list': {
      route: '/customer360/list',
      pageKey: 'customer360List',
      steps: [
        {
          element: '.customer-360-page .left-panel',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.customer360List.summary'),
          side: 'right',
          align: 'center'
        },
        {
          element: '.customer-360-page .left-panel .el-card__body .el-input',
          title: t('setup.onboarding.page.customer360List.step2.title'),
          description: t('setup.onboarding.page.customer360List.step2.description'),
          side: 'bottom',
          align: 'center'
        }
      ]
    },
    '/customerService/agentStatus': {
      route: '/customerService/agentStatus',
      pageKey: 'customerServiceAgentStatus',
      steps: [
        {
          element: '.agent-status-page .stat-row',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.customerServiceAgentStatus.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.agent-status-page .el-table',
          title: t('setup.onboarding.page.customerServiceAgentStatus.step2.title'),
          description: t('setup.onboarding.page.customerServiceAgentStatus.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/customerService/quickReply': {
      route: '/customerService/quickReply',
      pageKey: 'customerServiceQuickReply',
      steps: [
        {
          element: '.quick-reply-page .header-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.customerServiceQuickReply.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.quick-reply-page .el-table',
          title: t('setup.onboarding.page.customerServiceQuickReply.step2.title'),
          description: t('setup.onboarding.page.customerServiceQuickReply.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/customerService/sessionTag': {
      route: '/customerService/sessionTag',
      pageKey: 'customerServiceSessionTag',
      steps: [
        {
          element: '.session-tag-page .header-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.customerServiceSessionTag.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.session-tag-page .el-table',
          title: t('setup.onboarding.page.customerServiceSessionTag.step2.title'),
          description: t('setup.onboarding.page.customerServiceSessionTag.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/customerService/aiSuggestion': {
      route: '/customerService/aiSuggestion',
      pageKey: 'customerServiceAiSuggestion',
      steps: [
        {
          element: '.ai-suggestion-page .header-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.customerServiceAiSuggestion.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.ai-suggestion-page .el-table',
          title: t('setup.onboarding.page.customerServiceAiSuggestion.step2.title'),
          description: t('setup.onboarding.page.customerServiceAiSuggestion.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/customerService/csat': {
      route: '/customerService/csat',
      pageKey: 'customerServiceCsat',
      steps: [
        {
          element: '.csat-page .stats-row',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.customerServiceCsat.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.csat-page .el-table',
          title: t('setup.onboarding.page.customerServiceCsat.step2.title'),
          description: t('setup.onboarding.page.customerServiceCsat.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/email': {
      route: '/email',
      pageKey: 'emailList',
      steps: [
        {
          element: '.email-list .page-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.emailList.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.email-list .table-container',
          title: t('setup.onboarding.page.emailList.step2.title'),
          description: t('setup.onboarding.page.emailList.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/email/drafts': {
      route: '/email/drafts',
      pageKey: 'emailDrafts',
      steps: [
        {
          element: '.email-drafts .drafts-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.emailDrafts.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.email-drafts .el-table',
          title: t('setup.onboarding.page.emailDrafts.step2.title'),
          description: t('setup.onboarding.page.emailDrafts.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/email/jobs': {
      route: '/email/jobs',
      pageKey: 'emailJobs',
      steps: [
        {
          element: '.dashboard .recent-tasks',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.emailJobs.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.dashboard .recent-tasks .el-table',
          title: t('setup.onboarding.page.emailJobs.step2.title'),
          description: t('setup.onboarding.page.emailJobs.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/email/smtp': {
      route: '/email/smtp',
      pageKey: 'emailSmtp',
      steps: [
        {
          element: '.email-settings .toolbar',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.emailSmtp.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.email-settings .el-table',
          title: t('setup.onboarding.page.emailSmtp.step2.title'),
          description: t('setup.onboarding.page.emailSmtp.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/email/info': {
      route: '/email/info',
      pageKey: 'emailInfo',
      steps: [
        {
          element: '.email-providers-container .port-badges',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.emailInfo.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.email-providers-container .auth-notes',
          title: t('setup.onboarding.page.emailInfo.step2.title'),
          description: t('setup.onboarding.page.emailInfo.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/email/deliverability': {
      route: '/email/deliverability',
      pageKey: 'emailDeliverability',
      steps: [
        {
          element: '.deliverability .stats-row',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.emailDeliverability.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.deliverability .el-table',
          title: t('setup.onboarding.page.emailDeliverability.step2.title'),
          description: t('setup.onboarding.page.emailDeliverability.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/email/drag-editor': {
      route: '/email/drag-editor',
      pageKey: 'emailDragEditor',
      steps: [
        {
          element: '.email-editor .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.emailDragEditor.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.email-editor .block-palette',
          title: t('setup.onboarding.page.emailDragEditor.step2.title'),
          description: t('setup.onboarding.page.emailDragEditor.step2.description'),
          side: 'left',
          align: 'center'
        }
      ]
    },
    '/sms/list': {
      route: '/sms/list',
      pageKey: 'smsList',
      steps: [
        {
          element: '.sms-list .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.smsList.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.sms-list .search-form',
          title: t('setup.onboarding.page.smsList.step2.title'),
          description: t('setup.onboarding.page.smsList.step2.description'),
          side: 'bottom',
          align: 'center'
        }
      ]
    },
    '/sms/drafts': {
      route: '/sms/drafts',
      pageKey: 'smsDrafts',
      steps: [
        {
          element: '.sms-drafts .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.smsDrafts.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.sms-drafts .el-table',
          title: t('setup.onboarding.page.smsDrafts.step2.title'),
          description: t('setup.onboarding.page.smsDrafts.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/sms/jobs': {
      route: '/sms/jobs',
      pageKey: 'smsJobs',
      steps: [
        {
          element: '.sms-jobs .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.smsJobs.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.sms-jobs .el-table',
          title: t('setup.onboarding.page.smsJobs.step2.title'),
          description: t('setup.onboarding.page.smsJobs.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/sms/config': {
      route: '/sms/config',
      pageKey: 'smsConfig',
      steps: [
        {
          element: '.sms-config .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.smsConfig.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.sms-config .el-form',
          title: t('setup.onboarding.page.smsConfig.step2.title'),
          description: t('setup.onboarding.page.smsConfig.step2.description'),
          side: 'bottom',
          align: 'center'
        }
      ]
    },
    '/whatsapp/account': {
      route: '/whatsapp/account',
      pageKey: 'whatsappAccount',
      steps: [
        {
          element: '.page .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.whatsappAccount.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.page .el-form',
          title: t('setup.onboarding.page.whatsappAccount.step2.title'),
          description: t('setup.onboarding.page.whatsappAccount.step2.description'),
          side: 'bottom',
          align: 'center'
        }
      ]
    },
    '/whatsapp/drafts': {
      route: '/whatsapp/drafts',
      pageKey: 'whatsappDrafts',
      steps: [
        {
          element: '.page .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.whatsappDrafts.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.page .search-form',
          title: t('setup.onboarding.page.whatsappDrafts.step2.title'),
          description: t('setup.onboarding.page.whatsappDrafts.step2.description'),
          side: 'bottom',
          align: 'center'
        }
      ]
    },
    '/whatsapp/jobs': {
      route: '/whatsapp/jobs',
      pageKey: 'whatsappJobs',
      steps: [
        {
          element: '.page .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.whatsappJobs.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.page .search-form',
          title: t('setup.onboarding.page.whatsappJobs.step2.title'),
          description: t('setup.onboarding.page.whatsappJobs.step2.description'),
          side: 'bottom',
          align: 'center'
        }
      ]
    },
    '/whatsapp/lead-group-selection': {
      route: '/whatsapp/lead-group-selection',
      pageKey: 'whatsappLeadGroupSelection',
      steps: [
        {
          element: '.lead-group-selection .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.whatsappLeadGroupSelection.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.lead-group-selection .el-form',
          title: t('setup.onboarding.page.whatsappLeadGroupSelection.step2.title'),
          description: t('setup.onboarding.page.whatsappLeadGroupSelection.step2.description'),
          side: 'bottom',
          align: 'center'
        }
      ]
    },
    '/whatsapp/group-messaging': {
      route: '/whatsapp/group-messaging',
      pageKey: 'whatsappGroupMessaging',
      steps: [
        {
          element: '.bulk-messaging .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.whatsappGroupMessaging.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.bulk-messaging .preview-card',
          title: t('setup.onboarding.page.whatsappGroupMessaging.step2.title'),
          description: t('setup.onboarding.page.whatsappGroupMessaging.step2.description'),
          side: 'left',
          align: 'center'
        }
      ]
    },
    '/whatsapp/bulk-matrix': {
      route: '/whatsapp/bulk-matrix',
      pageKey: 'whatsappBulkMatrix',
      steps: [
        {
          element: '.whatsapp-matrix .stats-row',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.whatsappBulkMatrix.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.whatsapp-matrix .el-table',
          title: t('setup.onboarding.page.whatsappBulkMatrix.step2.title'),
          description: t('setup.onboarding.page.whatsappBulkMatrix.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/telegram/account': {
      route: '/telegram/account',
      pageKey: 'telegramAccount',
      steps: [
        {
          element: '.tg-bot-management .setup-guide-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.telegramAccount.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.tg-bot-management .el-card',
          title: t('setup.onboarding.page.telegramAccount.step2.title'),
          description: t('setup.onboarding.page.telegramAccount.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/telegram/gate': {
      route: '/telegram/gate',
      pageKey: 'telegramGate',
      steps: [
        {
          element: '.tg-gate-management .toolbar',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.telegramGate.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.tg-gate-management .el-alert',
          title: t('setup.onboarding.page.telegramGate.step2.title'),
          description: t('setup.onboarding.page.telegramGate.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/qq/account': {
      route: '/qq/account',
      pageKey: 'qqAccount',
      steps: [
        {
          element: '.qq-bot-management .qq-webhook-hint',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.qqAccount.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.qq-bot-management .account-list',
          title: t('setup.onboarding.page.qqAccount.step2.title'),
          description: t('setup.onboarding.page.qqAccount.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/knowledge/management': {
      route: '/knowledge/management',
      pageKey: 'knowledgeManagement',
      steps: [
        {
          element: '.knowledge-v2-management .overview-row',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.knowledgeManagement.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.knowledge-v2-management .el-table',
          title: t('setup.onboarding.page.knowledgeManagement.step2.title'),
          description: t('setup.onboarding.page.knowledgeManagement.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/knowledge/batch-import': {
      route: '/knowledge/batch-import',
      pageKey: 'knowledgeBatchImport',
      steps: [
        {
          element: '.batch-import-page',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.knowledgeBatchImport.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.batch-import-page .el-upload',
          title: t('setup.onboarding.page.knowledgeBatchImport.step2.title'),
          description: t('setup.onboarding.page.knowledgeBatchImport.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/knowledge/playground': {
      route: '/knowledge/playground',
      pageKey: 'knowledgePlayground',
      steps: [
        {
          element: '.playground-page',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.knowledgePlayground.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.playground-page .threshold-hint',
          title: t('setup.onboarding.page.knowledgePlayground.step2.title'),
          description: t('setup.onboarding.page.knowledgePlayground.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/knowledge/connectors': {
      route: '/knowledge/connectors',
      pageKey: 'knowledgeConnectors',
      steps: [
        {
          element: '.connectors-page .connector-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.knowledgeConnectors.summary'),
          side: 'right',
          align: 'center'
        },
        {
          element: '.connectors-page .cred-form',
          title: t('setup.onboarding.page.knowledgeConnectors.step2.title'),
          description: t('setup.onboarding.page.knowledgeConnectors.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/knowledge/feedbacks': {
      route: '/knowledge/feedbacks',
      pageKey: 'knowledgeFeedbacks',
      steps: [
        {
          element: '.feedback-list-page .filter-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.knowledgeFeedbacks.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.feedback-list-page .el-table',
          title: t('setup.onboarding.page.knowledgeFeedbacks.step2.title'),
          description: t('setup.onboarding.page.knowledgeFeedbacks.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/knowledge/tokens': {
      route: '/knowledge/tokens',
      pageKey: 'knowledgeApiToken',
      steps: [
        {
          element: '.api-token-page .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.knowledgeApiToken.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.api-token-page .el-table',
          title: t('setup.onboarding.page.knowledgeApiToken.step2.title'),
          description: t('setup.onboarding.page.knowledgeApiToken.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/knowledge/external': {
      route: '/knowledge/external',
      pageKey: 'knowledgeExternalImport',
      steps: [
        {
          element: '.external-import-page .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.knowledgeExternalImport.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.external-import-page .el-form',
          title: t('setup.onboarding.page.knowledgeExternalImport.step2.title'),
          description: t('setup.onboarding.page.knowledgeExternalImport.step2.description'),
          side: 'bottom',
          align: 'center'
        }
      ]
    },
    '/knowledge/statistics': {
      route: '/knowledge/statistics',
      pageKey: 'knowledgeStatistics',
      steps: [
        {
          element: '.knowledge-statistics .filter-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.knowledgeStatistics.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.knowledge-statistics .el-table',
          title: t('setup.onboarding.page.knowledgeStatistics.step2.title'),
          description: t('setup.onboarding.page.knowledgeStatistics.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/knowledge/openapi': {
      route: '/knowledge/openapi',
      pageKey: 'knowledgeOpenapi',
      steps: [
        {
          element: '.openapi-integration .filter-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.knowledgeOpenapi.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.openapi-integration .el-table',
          title: t('setup.onboarding.page.knowledgeOpenapi.step2.title'),
          description: t('setup.onboarding.page.knowledgeOpenapi.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/brand-config': {
      route: '/geo-tools/brand-config',
      pageKey: 'geoBrandConfig',
      steps: [
        {
          element: '.geo-brand-config .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoBrandConfig.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.geo-brand-config .code-block',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoBrandConfig.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/seo-infra': {
      route: '/geo-tools/seo-infra',
      pageKey: 'geoSeoInfra',
      steps: [
        {
          element: '.seo-infra .el-tabs__header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoSeoInfra.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.seo-infra .code',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoSeoInfra.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/pusher-config': {
      route: '/geo-tools/pusher-config',
      pageKey: 'geoPusherConfig',
      steps: [
        {
          element: '.geo-pusher-config .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoPusherConfig.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.geo-pusher-config .el-table',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoPusherConfig.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/site-config': {
      route: '/geo-tools/site-config',
      pageKey: 'geoSiteConfig',
      steps: [
        {
          element: '.geo-site-config .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoSiteConfig.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.geo-site-config .el-table',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoSiteConfig.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/schema-templates': {
      route: '/geo-tools/schema-templates',
      pageKey: 'geoSchemaTemplates',
      steps: [
        {
          element: '.geo-schema-templates .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoSchemaTemplates.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.geo-schema-templates .el-table',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoSchemaTemplates.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/competitors': {
      route: '/geo-tools/competitors',
      pageKey: 'geoCompetitorManage',
      steps: [
        {
          element: '.geo-page .el-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoCompetitorManage.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.geo-page .el-table',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoCompetitorManage.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/config': {
      route: '/geo-tools/config',
      pageKey: 'geoConfigOptimizer',
      steps: [
        {
          element: '.geo-page .form-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoConfigOptimizer.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.geo-page .el-form',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoConfigOptimizer.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/keyword-mining': {
      route: '/geo-tools/keyword-mining',
      pageKey: 'geoKeywordMining',
      steps: [
        {
          element: '.geo-page .search-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoKeywordMining.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.geo-page .el-table',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoKeywordMining.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/content-creation': {
      route: '/geo-tools/content-creation',
      pageKey: 'geoContentCreation',
      steps: [
        {
          element: '.geo-page .form-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoContentCreation.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.geo-page .el-form',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoContentCreation.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/content-optimize': {
      route: '/geo-tools/content-optimize',
      pageKey: 'geoContentOptimize',
      steps: [
        {
          element: '.geo-page .input-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoContentOptimize.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.geo-page .el-form',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoContentOptimize.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/site-publish': {
      route: '/geo-tools/site-publish',
      pageKey: 'geoSitePublish',
      steps: [
        {
          element: '.site-publish .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoSitePublish.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.site-publish .el-steps',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoSitePublish.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/push-center': {
      route: '/geo-tools/push-center',
      pageKey: 'geoPushCenter',
      steps: [
        {
          element: '.push-center .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoPushCenter.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.push-center .el-textarea',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoPushCenter.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/platform-publish': {
      route: '/geo-tools/platform-publish',
      pageKey: 'geoPlatformPublish',
      steps: [
        {
          element: '.geo-page .el-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoPlatformPublish.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.geo-page .el-table',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoPlatformPublish.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/workflow': {
      route: '/geo-tools/workflow',
      pageKey: 'geoWorkflowEditor',
      steps: [
        {
          element: '.geo-page .el-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoWorkflowEditor.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.geo-page .steps-container',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoWorkflowEditor.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/knowledge-base': {
      route: '/geo-tools/knowledge-base',
      pageKey: 'geoKnowledgeBase',
      steps: [
        {
          element: '.geo-page .el-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoKnowledgeBase.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.geo-page .el-table',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoKnowledgeBase.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/entity-graph': {
      route: '/geo-tools/entity-graph',
      pageKey: 'geoEntityGraph',
      steps: [
        {
          element: '.geo-page .el-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoEntityGraph.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.geo-page .el-table',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoEntityGraph.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/funnel-dashboard': {
      route: '/geo-tools/funnel-dashboard',
      pageKey: 'geoFunnelDashboard',
      steps: [
        {
          element: '.funnel-dashboard .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoFunnelDashboard.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.funnel-dashboard .card-header .el-button--primary',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoFunnelDashboard.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/index-tracking': {
      route: '/geo-tools/index-tracking',
      pageKey: 'geoIndexTracking',
      steps: [
        {
          element: '.index-tracking .card-header',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoIndexTracking.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.index-tracking .card-header .el-button--primary',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoIndexTracking.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/visibility': {
      route: '/geo-tools/visibility',
      pageKey: 'geoVisibilityBoard',
      steps: [
        {
          element: '.geo-page .stat-block',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoVisibilityBoard.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.geo-page .el-table',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoVisibilityBoard.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/sov-board': {
      route: '/geo-tools/sov-board',
      pageKey: 'geoSovBoard',
      steps: [
        {
          element: '.geo-page .el-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoSovBoard.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.geo-page .el-card__header .font-bold',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoSovBoard.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/crawler-stats': {
      route: '/geo-tools/crawler-stats',
      pageKey: 'geoCrawlerStats',
      steps: [
        {
          element: '.geo-page .coverage-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoCrawlerStats.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.geo-page .el-table',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoCrawlerStats.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/verification': {
      route: '/geo-tools/verification',
      pageKey: 'geoVerification',
      steps: [
        {
          element: '.geo-page .form-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoVerification.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.geo-page .el-form',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoVerification.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo/decision-report': {
      route: '/geo/decision-report',
      pageKey: 'geoDecisionReport',
      steps: [
        {
          element: '.geo-page .el-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoDecisionReport.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.geo-page .el-table',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoDecisionReport.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/reports': {
      route: '/geo-tools/reports',
      pageKey: 'geoReports',
      steps: [
        {
          element: '.geo-page .filter-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoReports.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.geo-page .el-table',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoReports.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/geo-tools/alerts': {
      route: '/geo-tools/alerts',
      pageKey: 'geoAlertCenter',
      steps: [
        {
          element: '.geo-page .el-card',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.geoAlertCenter.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.geo-page .el-table',
          title: t('setup.onboarding.page.common.action'),
          description: t('setup.onboarding.page.geoAlertCenter.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    },
    '/knowledge/rag-eval': {
      route: '/knowledge/rag-eval',
      pageKey: 'knowledgeRagEval',
      steps: [
        {
          element: '.rag-eval .stats-row',
          title: t('setup.onboarding.page.common.overview'),
          description: t('setup.onboarding.page.knowledgeRagEval.summary'),
          side: 'bottom',
          align: 'center'
        },
        {
          element: '.rag-eval .el-table',
          title: t('setup.onboarding.page.knowledgeRagEval.step2.title'),
          description: t('setup.onboarding.page.knowledgeRagEval.step2.description'),
          side: 'top',
          align: 'center'
        }
      ]
    }
  }
}

/** 按路由模式取导览配置；没有配置返回 undefined */
export function getPageTour(tours: Record<string, PageTour>, pattern: string): PageTour | undefined {
  return tours[pattern]
}
