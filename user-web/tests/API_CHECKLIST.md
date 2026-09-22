# user-web API 接口清单（710 个）

> 每个接口含 文件 / 函数 / method / url。

## abExperiment.js（13）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 1 | `getExperimentList` | GET | `/api/ab-experiments` | - |
| 2 | `getExperiment` | GET | `/api/ab-experiments/${id}` | - |
| 3 | `createExperiment` | POST | `/api/ab-experiments` | - |
| 4 | `updateExperiment` | PUT | `/api/ab-experiments/${id}` | - |
| 5 | `deleteExperiment` | DELETE | `/api/ab-experiments/${id}` | - |
| 6 | `startExperiment` | POST | `/api/ab-experiments/${id}/start` | - |
| 7 | `pauseExperiment` | POST | `/api/ab-experiments/${id}/pause` | - |
| 8 | `stopExperiment` | POST | `/api/ab-experiments/${id}/stop` | - |
| 9 | `getExperimentResults` | GET | `/api/ab-experiments/${id}/results` | - |
| 10 | `getConversionEvents` | GET | `/api/ab-experiments/${id}/conversion-events` | - |
| 11 | `getExperiments` | GET | `/api/ab-experiments` | - |
| 12 | `resumeExperiment` | GET | `/api/ab-experiments` | - |
| 13 | `getExperimentStats` | GET | `/api/ab-experiments` | - |

## aiAgent.js（9）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 14 | `listAgents` | GET | `/api/ai-agents` | - |
| 15 | `listEnabledAgents` | GET | `/api/ai-agents-enabled` | - |
| 16 | `getAgent` | GET | `/api/ai-agents/${id}` | - |
| 17 | `createAgent` | POST | `/api/ai-agents` | - |
| 18 | `updateAgent` | PUT | `/api/ai-agents/${id}` | - |
| 19 | `deleteAgent` | DELETE | `/api/ai-agents/${id}` | - |
| 20 | `toggleAgent` | POST | `/api/ai-agents/${id}/toggle` | - |
| 21 | `testAgent` | POST | `/api/ai-agents/${id}/test` | - |
| 22 | `getAgentContext` | GET | `/api/ai-agents/${id}/context` | - |

## aiContent.js（13）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 23 | `generateAIContent` | POST | `/api/ai/generate` | - |
| 24 | `getGenerationHistory` | GET | `/api/ai/history` | - |
| 25 | `getAIContentRecord` | GET | `/api/ai/history/${id}` | - |
| 26 | `saveAIContentRecord` | POST | `/api/ai/history/${id}/save` | - |
| 27 | `favoriteAIContentRecord` | POST | `/api/ai/history/${id}/favorite` | - |
| 28 | `rateAIContentRecord` | POST | `/api/ai/history/${id}/rate` | - |
| 29 | `deleteAIContentRecord` | DELETE | `/api/ai/history/${id}` | - |
| 30 | `getAITemplates` | GET | `/api/ai/templates` | - |
| 31 | `getAITemplate` | GET | `/api/ai/templates/${id}` | - |
| 32 | `createAITemplate` | POST | `/api/ai/templates` | - |
| 33 | `updateAITemplate` | PUT | `/api/ai/templates/${id}` | - |
| 34 | `deleteAITemplate` | DELETE | `/api/ai/templates/${id}` | - |
| 35 | `getAITemplateTypes` | GET | `/api/ai/template-types` | - |

## aiProductivity.js（6）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 36 | `AiProductivityApi.getOverview` | GET | `/api/analytics/ai-productivity` | - |
| 37 | `AiProductivityApi.getConversationStats` | GET | `/api/analytics/ai-productivity` | - |
| 38 | `AiProductivityApi.getConversionRate` | GET | `/api/analytics/ai-productivity` | - |
| 39 | `AiProductivityApi.getResponseTimeStats` | GET | `/api/analytics/ai-productivity` | - |
| 40 | `AiProductivityApi.getTopSalesPortrait` | GET | `/api/analytics/ai-productivity` | - |
| 41 | `AiProductivityApi.getAgentRanking` | GET | `/api/analytics/ai-productivity` | - |

## autoReply.js（9）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 42 | `autoReplyApi.listAccounts` | GET | `/api/auto-reply/accounts` | - |
| 43 | `autoReplyApi.upsertAccount` | POST | `/api/auto-reply/accounts` | - |
| 44 | `autoReplyApi.deleteAccount` | DELETE | `/api/auto-reply/accounts/${id}` | - |
| 45 | `autoReplyApi.loginStart` | POST | `/api/auto-reply/start-login` | - |
| 46 | `autoReplyApi.loginStatus` | GET | `/api/auto-reply/login-status` | - |
| 47 | `autoReplyApi.getRule` | GET | `/api/auto-reply/rule` | - |
| 48 | `autoReplyApi.saveRule` | POST | `/api/auto-reply/rule` | - |
| 49 | `autoReplyApi.start` | POST | `/api/auto-reply/start` | - |
| 50 | `autoReplyApi.stop` | POST | `/api/auto-reply/stop` | - |

## backup.js（7）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 51 | `getBackupList` | GET | `/api/backups` | - |
| 52 | `getBackupByID` | GET | `/api/backups/${id}` | - |
| 53 | `createBackup` | POST | `/api/backups` | - |
| 54 | `deleteBackup` | DELETE | `/api/backups/${id}` | - |
| 55 | `restoreBackup` | POST | `/api/restore` | - |
| 56 | `getRestoreList` | GET | `/api/restore/list` | - |
| 57 | `getLastRestore` | GET | `/api/restore/last` | - |

## batchOperation.js（11）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 58 | `batchImportFile` | POST | `/api/batch/import` | - |
| 59 | `downloadBatchTemplate` | GET | `/api/batch/template` | - |
| 60 | `batchExport` | POST | `/api/batch/export` | - |
| 61 | `batchDelete` | POST | `/api/batch/delete` | - |
| 62 | `batchUpdate` | POST | `/api/batch/update` | - |
| 63 | `getBatchTools` | GET | `/api/batch/tools` | - |
| 64 | `runBatch` | GET | `/api/batch/histories` | - |
| 65 | `getBatchHistories` | GET | `/api/batch/histories` | - |
| 66 | `cancelBatch` | POST | `/api/batch/histories/${id}/cancel` | - |
| 67 | `getBatchDetail` | GET | `/api/batch/histories/${id}` | - |
| 68 | `previewBatch` | POST | `/api/batch/preview` | - |

## bulkMessaging.js（7）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 69 | `getTemplates` | GET | `/api/whatsapp/templates` | - |
| 70 | `createTemplate` | POST | `/api/whatsapp/templates` | - |
| 71 | `updateTemplate` | PUT | `/api/whatsapp/templates/${id}` | - |
| 72 | `deleteTemplate` | DELETE | `/api/whatsapp/templates/${id}` | - |
| 73 | `sendBulkMessage` | POST | `/api/whatsapp/group-messaging/send` | - |
| 74 | `getMessageStatus` | GET | `/api/whatsapp/group-messaging/status/${queueId}` | - |
| 75 | `getSendRecords` | GET | `/api/whatsapp/group-messaging/records` | - |

## channelAgentBinding.js（5）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 76 | `listBindings` | GET | `/api/channel-agent-bindings` | - |
| 77 | `listBindingsByAgent` | GET | `/api/channel-agent-bindings/by-agent/${agentId}` | - |
| 78 | `createBinding` | POST | `/api/channel-agent-bindings` | - |
| 79 | `updateBinding` | PUT | `/api/channel-agent-bindings/${id}` | - |
| 80 | `deleteBinding` | DELETE | `/api/channel-agent-bindings/${id}` | - |

## chat.js（1）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 81 | `getUploadToken` | GET | `/api/chat/public/upload-token` | - |

## chatChannel.js（7）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 82 | `listChannels` | GET | `/api/chat-channels` | - |
| 83 | `getChannel` | GET | `/api/chat-channels/${channelId}` | - |
| 84 | `createChannel` | POST | `/api/chat-channels` | - |
| 85 | `updateChannel` | PUT | `/api/chat-channels/${channelId}` | - |
| 86 | `deleteChannel` | DELETE | `/api/chat-channels/${channelId}` | - |
| 87 | `rotateAppKey` | POST | `/api/chat-channels/${channelId}/rotate-key` | - |
| 88 | `resetAppSecret` | POST | `/api/chat-channels/${channelId}/reset-secret` | - |

## churnPrediction.js（15）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 89 | `getChurnPrediction` | GET | `/api/churn/prediction` | - |
| 90 | `getChurnPredictions` | GET | `/api/churn/predictions` | - |
| 91 | `getHighRiskUsers` | GET | `/api/churn/high-risk-users` | - |
| 92 | `getChurnWarnings` | GET | `/api/churn/warnings` | - |
| 93 | `getUnhandledWarnings` | GET | `/api/churn/unhandled-warnings` | - |
| 94 | `markWarningHandled` | POST | `/api/churn/warnings/${id}/handle` | - |
| 95 | `getChurnModelConfig` | GET | `/api/churn/model-config` | - |
| 96 | `saveChurnModelConfig` | POST | `/api/churn/model-config` | - |
| 97 | `getChurnStatistics` | GET | `/api/churn/statistics` | - |
| 98 | `getRiskDistribution` | GET | `/api/churn/risk-distribution` | - |
| 99 | `runChurnPrediction` | POST | `/api/churn/warnings/intervene` | - |
| 100 | `interveneUser` | POST | `/api/churn/warnings/intervene` | - |
| 101 | `getChurnStats` | POST | `/api/user-segment/rfm/calculate` | - |
| 102 | `getChurnConfig` | POST | `/api/user-segment/rfm/calculate` | - |
| 103 | `updateChurnConfig` | POST | `/api/user-segment/rfm/calculate` | - |

## clue.js（5）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 104 | `clueApi.type` | GET | `/api/clues/type` | - |
| 105 | `clueApi.delete` | DELETE | `/api/clues/delete/${id}` | - |
| 106 | `clueApi.list` | GET | `/api/clues/list?page=${page}&limit=${limit}` | - |
| 107 | `clueApi.statistics` | GET | `/api/clues/statistics` | - |
| 108 | `clueApi.import` | POST | `/api/clues/import` | - |

## community.js（10）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 109 | `communityApi.getGroups` | GET | `/api/community/groups` | - |
| 110 | `communityApi.createGroup` | POST | `/api/community/groups` | - |
| 111 | `communityApi.getGroupById` | GET | `/api/community/groups/${id}` | - |
| 112 | `communityApi.updateGroup` | PUT | `/api/community/groups/${id}` | - |
| 113 | `communityApi.deleteGroup` | DELETE | `/api/community/groups/${id}` | - |
| 114 | `communityApi.getMembers` | GET | `/api/community/members` | - |
| 115 | `communityApi.getMessages` | GET | `/api/community/messages` | - |
| 116 | `communityApi.getStats` | GET | `/api/community/stats` | - |
| 117 | `communityApi.importData` | POST | `/api/community/import` | - |
| 118 | `communityApi.exportData` | POST | `/api/community/export` | - |

## conversionFunnel.js（4）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 119 | `ConversionFunnelApi.getFunnelStages` | GET | `/api/analytics/funnel` | - |
| 120 | `ConversionFunnelApi.getFunnelStats` | GET | `/api/analytics/funnel` | - |
| 121 | `ConversionFunnelApi.getFunnelLossAnalysis` | GET | `/api/analytics/funnel/stage` | - |
| 122 | `ConversionFunnelApi.getFunnelTrend` | GET | `/api/analytics/funnel/stage` | - |

## customReport.js（14）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 123 | `getReportList` | GET | `/api/custom-reports` | - |
| 124 | `getReport` | GET | `/api/custom-reports/${id}` | - |
| 125 | `createReport` | POST | `/api/custom-reports` | - |
| 126 | `updateReport` | PUT | `/api/custom-reports/${id}` | - |
| 127 | `deleteReport` | DELETE | `/api/custom-reports/${id}` | - |
| 128 | `getPublicTemplates` | GET | `/api/custom-reports/templates` | - |
| 129 | `useReportTemplate` | POST | `/api/custom-reports/templates/${id}/use` | - |
| 130 | `queryReportData` | GET | `/api/custom-reports/${id}/data` | - |
| 131 | `getCustomReports` | GET | `/api/custom-reports/${id}/data?format=export` | - |
| 132 | `createCustomReport` | GET | `/api/custom-reports/${id}/data?format=export` | - |
| 133 | `updateCustomReport` | GET | `/api/custom-reports/${id}/data?format=export` | - |
| 134 | `deleteCustomReport` | GET | `/api/custom-reports/${id}/data?format=export` | - |
| 135 | `exportCustomReport` | GET | `/api/custom-reports/${id}/data?format=export` | - |
| 136 | `runCustomReport` | GET | `/api/custom-reports/${id}/data?format=run` | - |

## customer360.js（9）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 137 | `getCustomerList` | GET | `/api/customer/list` | - |
| 138 | `getCustomer360Detail` | GET | `/api/customer/360/${id}` | - |
| 139 | `addCustomerTag` | POST | `/api/customer/${id}/tags` | - |
| 140 | `removeCustomerTag` | DELETE | `/api/customer/${id}/tags/${tag}` | - |
| 141 | `updateCustomer` | PUT | `/api/customer/${id}` | - |
| 142 | `getCustomerDetail` | GET | `/api/customer/${id}` | - |
| 143 | `getCustomerBehaviors` | GET | `/api/customer/${id}/behaviors` | - |
| 144 | `getCustomerOrders` | GET | `/api/customer/${id}/orders` | - |
| 145 | `getCustomerCommunications` | GET | `/api/customer/${id}/communications` | - |

## customerEvent.js（13）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 146 | `trackEvent` | POST | `/api/events/track` | - |
| 147 | `trackPageView` | POST | `/api/events/pageview` | - |
| 148 | `trackClick` | POST | `/api/events/click` | - |
| 149 | `trackPurchase` | POST | `/api/events/purchase` | - |
| 150 | `trackSignup` | POST | `/api/events/signup` | - |
| 151 | `trackLogin` | POST | `/api/events/login` | - |
| 152 | `trackAddToCart` | POST | `/api/events/add-to-cart` | - |
| 153 | `getCustomerEventHistory` | GET | `/api/events/customer/${customerId}` | - |
| 154 | `getEventStats` | GET | `/api/events/stats` | - |
| 155 | `getCustomerEvents` | DELETE | `/api/events/customer/${id}` | - |
| 156 | `createEvent` | DELETE | `/api/events/customer/${id}` | - |
| 157 | `getEventDetail` | DELETE | `/api/events/customer/${id}` | - |
| 158 | `deleteEvent` | DELETE | `/api/events/customer/${id}` | - |

## customerJourney.js（6）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 159 | `getJourneyOverview` | GET | `/api/customer-journey/overview` | - |
| 160 | `getJourneyState` | GET | `/api/customer-journey/overview?customer_id=${customerId}` | - |
| 161 | `listJourneyStages` | GET | `/api/customer-journey/stages` | - |
| 162 | `listByStage` | GET | `/api/customer-journey/by-stage?stage=${stage}` | - |
| 163 | `transitionJourney` | POST | `/api/customer-journey/transition` | - |
| 164 | `touchCustomer` | POST | `/api/customer-journey/touch` | - |

## customerService.js（21）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 165 | `createAgent` | POST | `/api/agents` | - |
| 166 | `getAgentStatus` | GET | `/api/agents/${id}` | - |
| 167 | `getOnlineAgents` | GET | `/api/agents/online` | - |
| 168 | `listAllAgents` | GET | `/api/agents/all` | - |
| 169 | `updateAgentStatus` | PUT | `/api/agents/${id}/status` | - |
| 170 | `goOnline` | POST | `/api/agents/${id}/online` | - |
| 171 | `goOffline` | POST | `/api/agents/${id}/offline` | - |
| 172 | `getAgentSessions` | GET | `/api/agents/${id}/sessions` | - |
| 173 | `getMyAgent` | GET | `/api/agents/me` | - |
| 174 | `getQuickReplies` | GET | `/api/quick-replies` | - |
| 175 | `getQuickReplyCategories` | GET | `/api/quick-replies/categories` | - |
| 176 | `createQuickReply` | POST | `/api/quick-replies` | - |
| 177 | `updateQuickReply` | PUT | `/api/quick-replies/${id}` | - |
| 178 | `deleteQuickReply` | DELETE | `/api/quick-replies/${id}` | - |
| 179 | `getSessionTags` | GET | `/api/session-tags` | - |
| 180 | `createSessionTag` | POST | `/api/session-tags` | - |
| 181 | `updateSessionTag` | PUT | `/api/session-tags/${id}` | - |
| 182 | `deleteSessionTag` | DELETE | `/api/session-tags/${id}` | - |
| 183 | `getAISuggestions` | GET | `/api/ai-suggestions/${sessionId}` | - |
| 184 | `useAISuggestion` | POST | `/api/ai-suggestions/${id}/use` | - |
| 185 | `tagSession` | POST | `/api/customer-sessions/${sessionId}/tags` | - |

## customerServiceAgent.js（7）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 186 | `listMounts` | GET | `/api/customer-service-agents` | - |
| 187 | `listMountsByAIAgent` | GET | `/api/customer-service-agents/by-ai-agent/${aiAgentId}` | - |
| 188 | `createMount` | POST | `/api/customer-service-agents` | - |
| 189 | `updateMount` | PUT | `/api/customer-service-agents/${id}` | - |
| 190 | `deleteMount` | DELETE | `/api/customer-service-agents/${id}` | - |
| 191 | `listMountsByUser` | GET | `/api/customer-service-agents/by-user/${userId}` | - |
| 192 | `createMountByUser` | POST | `/api/customer-service-agents/by-user/${userId}` | - |

## customerSession.js（6）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 193 | `getSessions` | GET | `/api/customer-sessions` | - |
| 194 | `getSessionMessages` | GET | `/api/customer-sessions/${id}/messages` | - |
| 195 | `sendMessage` | POST | `/api/customer-sessions/${data.sessionId}/messages` | - |
| 196 | `createSession` | POST | `/api/customer-sessions` | - |
| 197 | `closeSession` | POST | `/api/customer-sessions/${id}/close` | - |
| 198 | `transferSession` | POST | `/api/customer-sessions/${id}/transfer` | - |

## dashboardScreen.js（12）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 199 | `getScreenList` | GET | `/api/dashboards` | - |
| 200 | `getScreenByID` | GET | `/api/dashboards/${id}` | - |
| 201 | `createScreen` | POST | `/api/dashboards` | - |
| 202 | `updateScreen` | PUT | `/api/dashboards/${id}` | - |
| 203 | `deleteScreen` | DELETE | `/api/dashboards/${id}` | - |
| 204 | `publicViewScreen` | GET | `/api/dashboards/public/${code}` | - |
| 205 | `getDashboardData` | GET | `/api/dashboards/data` | - |
| 206 | `getRealtimeActivities` | GET | `/api/dashboards/activities` | - |
| 207 | `getKPIs` | GET | `/api/dashboards/data` | - |
| 208 | `getTrends` | GET | `/api/dashboards/data` | - |
| 209 | `getChannels` | GET | `/api/dashboards/data` | - |
| 210 | `getRegions` | GET | `/api/dashboards/data` | - |

## dialogueMemory.js（10）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 211 | `memoryApi.appendMessage` | POST | `/api/memory/messages` | - |
| 212 | `memoryApi.getShortTerm` | GET | `/api/memory/short` | - |
| 213 | `memoryApi.getLongTerm` | GET | `/api/memory/long` | - |
| 214 | `memoryApi.updateKeyFacts` | POST | `/api/memory/facts` | - |
| 215 | `memoryApi.recordObjection` | POST | `/api/memory/objections` | - |
| 216 | `memoryApi.updatePurchaseIntent` | POST | `/api/memory/purchase-intent` | - |
| 217 | `memoryApi.recordIntent` | POST | `/api/memory/intent-trail` | - |
| 218 | `memoryApi.recordSOP` | POST | `/api/memory/sop-history` | - |
| 219 | `memoryApi.buildContext` | GET | `/api/memory/context` | - |
| 220 | `memoryApi.list` | GET | `/api/memory/list` | - |

## domainPool.js（7）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 221 | `domainPoolApi.getList` | GET | `/api/domainpool/list` | - |
| 222 | `domainPoolApi.getById` | GET | `/api/domainpool/${id}` | - |
| 223 | `domainPoolApi.create` | POST | `/api/domainpool/create` | - |
| 224 | `domainPoolApi.update` | PUT | `/api/domainpool/update` | - |
| 225 | `domainPoolApi.delete` | DELETE | `/api/domainpool/delete/${id}` | - |
| 226 | `domainPoolApi.checkDomain` | POST | `/api/domainpool/check/${id}` | - |
| 227 | `domainPoolApi.checkAllDomains` | POST | `/api/domainpool/checkall` | - |

## douyinCard.js（9）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 228 | `getDouyinCardList` | GET | `/api/douyin/list` | - |
| 229 | `getDouyinCard` | GET | `/api/douyin/${id}` | - |
| 230 | `createDouyinCard` | POST | `/api/douyin/create` | - |
| 231 | `updateDouyinCard` | PUT | `/api/douyin/update` | - |
| 232 | `deleteDouyinCard` | DELETE | `/api/douyin/delete/${id}` | - |
| 233 | `viewDouyinCard` | GET | `/api/douyin/view/${id}` | - |
| 234 | `getDouyinCardStats` | GET | `/api/douyin/stats/card/${id}` | - |
| 235 | `getDouyinCardOverallStats` | GET | `/api/douyin/stats/overall` | - |
| 236 | `generateShortLink` | POST | `/api/douyin/${id}/generate-short-link` | - |

## email.js（16）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 237 | `emailApi.getEmailSmtpList` | GET | `/api/email/smtp` | - |
| 238 | `emailApi.addEmailSmtp` | POST | `/api/email/smtp` | - |
| 239 | `emailApi.updateEmailSmtp` | PUT | `/api/email/smtp/${id}` | - |
| 240 | `emailApi.deleteEmailSmtp` | DELETE | `/api/email/smtp/${id}` | - |
| 241 | `emailApi.getDrafts` | GET | `/api/email/drafts` | - |
| 242 | `emailApi.getDraftDetail` | GET | `/api/email/drafts/${id}` | - |
| 243 | `emailApi.createDraft` | POST | `/api/email/drafts` | - |
| 244 | `emailApi.updateDraft` | PUT | `/api/email/drafts/${id}` | - |
| 245 | `emailApi.deleteDraft` | DELETE | `/api/email/drafts/${id}` | - |
| 246 | `emailApi.uploadImage` | POST | `/api/upload` | - |
| 247 | `emailApi.sendEmail` | POST | `/api/email/list` | - |
| 248 | `emailApi.getEmailList` | GET | `/api/email/list?page=${page}&limit=${limit}` | - |
| 249 | `emailApi.getJobsList` | GET | `/api/email/jobs?page=${page}&limit=${limit}` | - |
| 250 | `emailApi.deleteJob` | DELETE | `/api/email/jobs/${id}` | - |
| 251 | `emailApi.getJobDetail` | GET | `/api/email/jobs/${id}` | - |
| 252 | `emailApi.deleteJobDetail` | DELETE | `/api/email/jobs/detail?id=${id}` | - |

## feishu.js（7）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 253 | `listAccounts` | GET | `/api/feishu/accounts` | - |
| 254 | `getAccount` | GET | `/api/feishu/accounts/${id}` | - |
| 255 | `createAccount` | POST | `/api/feishu/accounts` | - |
| 256 | `updateAccount` | PUT | `/api/feishu/accounts/${id}` | - |
| 257 | `deleteAccount` | DELETE | `/api/feishu/accounts/${id}` | - |
| 258 | `testSend` | POST | `/api/feishu/accounts/${id}/test-send` | - |
| 259 | `refreshToken` | POST | `/api/feishu/accounts/${id}/refresh-token` | - |

## integration.js（19）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 260 | `getIntegrationAccountList` | GET | `/api/integrations` | - |
| 261 | `getIntegrationAccount` | GET | `/api/integrations/${id}` | - |
| 262 | `createIntegrationAccount` | POST | `/api/integrations` | - |
| 263 | `updateIntegrationAccount` | PUT | `/api/integrations/${id}` | - |
| 264 | `deleteIntegrationAccount` | DELETE | `/api/integrations/${id}` | - |
| 265 | `syncCustomers` | POST | `/api/integrations/${id}/sync-customers` | - |
| 266 | `syncOrders` | POST | `/api/integrations/${id}/sync-orders` | - |
| 267 | `syncProducts` | POST | `/api/integrations/${id}/sync-products` | - |
| 268 | `getSyncLogs` | GET | `/api/integration/sync-logs` | - |
| 269 | `getExternalCustomers` | GET | `/api/integration/external-customers` | - |
| 270 | `getExternalOrders` | GET | `/api/integration/external-orders` | - |
| 271 | `getExternalProducts` | GET | `/api/integration/external-products` | - |
| 272 | `getIntegrations` | POST | `/api/integrations/${id}/test` | - |
| 273 | `createIntegration` | POST | `/api/integrations/${id}/test` | - |
| 274 | `updateIntegration` | POST | `/api/integrations/${id}/test` | - |
| 275 | `deleteIntegration` | POST | `/api/integrations/${id}/test` | - |
| 276 | `toggleIntegrationStatus` | POST | `/api/integrations/${id}/test` | - |
| 277 | `testIntegration` | POST | `/api/integrations/${id}/test` | - |
| 278 | `getIntegrationStats` | GET | `/api/integrations` | - |

## intentRecognition.js（5）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 279 | `intentApi.recognize` | POST | `/api/intent/recognize` | - |
| 280 | `intentApi.batchRecognize` | POST | `/api/intent/recognize/batch` | - |
| 281 | `intentApi.getStats` | GET | `/api/intent/stats` | - |
| 282 | `intentApi.getRecent` | GET | `/api/intent/recent` | - |
| 283 | `intentApi.getDict` | GET | `/api/intent/dict` | - |

## knowledge.js（27）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 284 | `knowledgeAPI.importUpload` | POST | `/api/knowledge/import/upload` | - |
| 285 | `knowledgeAPI.importText` | POST | `/api/knowledge/import/text` | - |
| 286 | `knowledgeAPI.importURL` | POST | `/api/knowledge/import/url` | - |
| 287 | `knowledgeAPI.listDocuments` | GET | `/api/knowledge/documents` | - |
| 288 | `knowledgeAPI.getDocument` | GET | `/api/knowledge/documents/${id}` | - |
| 289 | `knowledgeAPI.getDocumentProgress` | GET | `/api/knowledge/documents/${id}/progress` | - |
| 290 | `knowledgeAPI.getDocumentChunks` | GET | `/api/knowledge/documents/${id}/chunks` | - |
| 291 | `knowledgeAPI.updateDocument` | PUT | `/api/knowledge/documents/${id}` | - |
| 292 | `knowledgeAPI.deleteDocument` | DELETE | `/api/knowledge/documents/${id}` | - |
| 293 | `knowledgeAPI.reindexDocument` | POST | `/api/knowledge/documents/${id}/reindex` | - |
| 294 | `knowledgeAPI.rebuildProductIndex` | POST | `/api/knowledge/products/${productId}/rebuild-index` | - |
| 295 | `knowledgeAPI.getProductOverview` | GET | `/api/knowledge/products/${productId}/overview` | - |
| 296 | `knowledgeAPI.search` | POST | `/api/knowledge/search` | - |
| 297 | `knowledgeAPI.listImportLogs` | GET | `/api/knowledge/import-logs` | - |
| 298 | `knowledgeAPI.listOpenAPISources` | GET | `/api/knowledge/openapi/sources` | - |
| 299 | `knowledgeAPI.createOpenAPISource` | POST | `/api/knowledge/openapi/sources` | - |
| 300 | `knowledgeAPI.getOpenAPISource` | GET | `/api/knowledge/openapi/sources/${id}` | - |
| 301 | `knowledgeAPI.updateOpenAPISource` | PUT | `/api/knowledge/openapi/sources/${id}` | - |
| 302 | `knowledgeAPI.deleteOpenAPISource` | DELETE | `/api/knowledge/openapi/sources/${id}` | - |
| 303 | `knowledgeAPI.syncOpenAPISource` | POST | `/api/knowledge/openapi/sources/${id}/sync` | - |
| 304 | `knowledgeAPI.testOpenAPISource` | POST | `/api/knowledge/openapi/sources/test` | - |
| 305 | `knowledgeAPI.toggleOpenAPISource` | POST | `/api/knowledge/openapi/sources/${id}/toggle` | - |
| 306 | `knowledgeAPI.getOverviewStats` | GET | `/api/knowledge/stats/overview` | - |
| 307 | `knowledgeAPI.getDocumentStats` | GET | `/api/knowledge/stats/documents` | - |
| 308 | `knowledgeAPI.getSearchStats` | GET | `/api/knowledge/stats/searches` | - |
| 309 | `knowledgeAPI.getImportStats` | GET | `/api/knowledge/stats/imports` | - |
| 310 | `knowledgeAPI.getOpenAPIStats` | GET | `/api/knowledge/stats/openapi` | - |

## knowledgeBase.js（4）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 311 | `knowledgeBaseAPI.getDocuments` | GET | `/api/rag/documents` | - |
| 312 | `knowledgeBaseAPI.getDocument` | GET | `/api/rag/documents/${id}` | - |
| 313 | `knowledgeBaseAPI.deleteDocument` | DELETE | `/api/rag/documents/${id}` | - |
| 314 | `knowledgeBaseAPI.importKnowledgeBase` | POST | `/api/rag/import` | - |

## knowledgeMerchant.js（14）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 315 | `knowledgeMerchantAPI.batchImport` | POST | `/api/knowledge-merchant/batch/import` | - |
| 316 | `knowledgeMerchantAPI.batchUpload` | POST | `/api/knowledge-merchant/batch/upload` | - |
| 317 | `knowledgeMerchantAPI.playground` | POST | `/api/knowledge-merchant/playground` | - |
| 318 | `knowledgeMerchantAPI.listDocumentChunks` | GET | `/api/knowledge-merchant/documents/${documentId}/chunks` | - |
| 319 | `knowledgeMerchantAPI.updateChunk` | PUT | `/api/knowledge-merchant/chunks/${chunkId}` | - |
| 320 | `knowledgeMerchantAPI.deleteChunk` | DELETE | `/api/knowledge-merchant/chunks/${chunkId}` | - |
| 321 | `knowledgeMerchantAPI.splitChunk` | POST | `/api/knowledge-merchant/chunks/${chunkId}/split` | - |
| 322 | `knowledgeMerchantAPI.submitFeedback` | POST | `/api/knowledge-merchant/feedback` | - |
| 323 | `knowledgeMerchantAPI.listFeedbacks` | GET | `/api/knowledge-merchant/feedbacks` | - |
| 324 | `knowledgeMerchantAPI.createToken` | POST | `/api/knowledge-merchant/tokens` | - |
| 325 | `knowledgeMerchantAPI.listTokens` | GET | `/api/knowledge-merchant/tokens` | - |
| 326 | `knowledgeMerchantAPI.revokeToken` | POST | `/api/knowledge-merchant/tokens/${tokenId}/revoke` | - |
| 327 | `knowledgeMerchantAPI.externalImport` | POST | `/api/knowledge-merchant/external/import` | - |
| 328 | `knowledgeMerchantAPI.listExternalJobs` | GET | `/api/knowledge-merchant/external/jobs` | - |

## kuaishouCard.js（11）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 329 | `getKuaishouCardList` | GET | `/api/kuaishou/list` | - |
| 330 | `getKuaishouCard` | GET | `/api/kuaishou/${id}` | - |
| 331 | `createKuaishouCard` | POST | `/api/kuaishou/create` | - |
| 332 | `updateKuaishouCard` | PUT | `/api/kuaishou/update` | - |
| 333 | `deleteKuaishouCard` | DELETE | `/api/kuaishou/delete/${id}` | - |
| 334 | `viewKuaishouCard` | GET | `/api/kuaishou/view/${id}` | - |
| 335 | `likeKuaishouCard` | POST | `/api/kuaishou/like/${id}` | - |
| 336 | `shareKuaishouCard` | POST | `/api/kuaishou/share/${id}` | - |
| 337 | `getKuaishouCardStats` | GET | `/api/kuaishou/stats/card/${id}` | - |
| 338 | `getKuaishouCardOverallStats` | GET | `/api/kuaishou/stats/overall` | - |
| 339 | `generateShortLink` | POST | `/api/kuaishou/${id}/generate-short-link` | - |

## livecode.js（12）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 341 | `getLiveCodes` | GET | `/api/live-codes/list` | - |
| 342 | `getLiveCode` | GET | `/api/live-codes/${id}` | - |
| 343 | `createLiveCode` | POST | `/api/live-codes/create` | - |
| 344 | `updateLiveCode` | PUT | `/api/live-codes/${id}/update` | - |
| 345 | `deleteLiveCode` | DELETE | `/api/live-codes/${id}/delete` | - |
| 346 | `getLiveCodeStats` | GET | `/api/live-codes/${id}/stats` | - |
| 347 | `getLiveCodeQRs` | GET | `/api/live-codes/${liveCodeId}/qrcodes` | - |
| 348 | `generateLiveCodeQR` | POST | `/api/live-codes/${liveCodeId}/qrcodes/create` | - |
| 349 | `getLiveCodeQRStats` | GET | `/api/live-codes/qrcodes/${qrId}/stats` | - |
| 350 | `shareLiveCode` | POST | `/api/live-codes/${id}/share` | - |
| 351 | `deleteLiveCodeQR` | DELETE | `/api/live-codes/qrcodes/${id}/delete` | - |
| 352 | `updateLiveCodeQR` | PUT | `/api/live-codes/qrcodes/${id}/update` | - |

## llmRouting.js（11）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 353 | `LlmRoutingApi.getModelList` | GET | `/api/llm/models` | - |
| 354 | `LlmRoutingApi.getModelDetail` | GET | `/api/llm/models/${id}` | - |
| 355 | `LlmRoutingApi.saveModel` | POST | `/api/llm/models` | - |
| 356 | `LlmRoutingApi.deleteModel` | DELETE | `/api/llm/models/${id}` | - |
| 357 | `LlmRoutingApi.updateModelStatus` | PUT | `/api/llm/models/${id}/status` | - |
| 358 | `LlmRoutingApi.getSceneRouting` | GET | `/api/llm/scene-routing` | - |
| 359 | `LlmRoutingApi.saveSceneRouting` | PUT | `/api/llm/scene-routing` | - |
| 360 | `LlmRoutingApi.getFallbackStrategy` | GET | `/api/llm/fallback` | - |
| 361 | `LlmRoutingApi.saveFallbackStrategy` | PUT | `/api/llm/fallback` | - |
| 362 | `LlmRoutingApi.getCostStats` | GET | `/api/llm/cost-stats` | - |
| 363 | `LlmRoutingApi.testModel` | POST | `/api/llm/models/${id}/test` | - |

## marketingFlow.js（10）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 364 | `getMarketingFlowList` | GET | `/api/marketing-flows` | - |
| 365 | `getMarketingFlow` | GET | `/api/marketing-flows/${id}` | - |
| 366 | `createMarketingFlow` | POST | `/api/marketing-flows` | - |
| 367 | `updateMarketingFlow` | PUT | `/api/marketing-flows/${id}` | - |
| 368 | `deleteMarketingFlow` | DELETE | `/api/marketing-flows/${id}` | - |
| 369 | `activateFlow` | POST | `/api/marketing-flows/${id}/activate` | - |
| 370 | `pauseMarketingFlow` | POST | `/api/marketing-flows/${id}/pause` | - |
| 371 | `stopMarketingFlow` | POST | `/api/marketing-flows/${id}/stop` | - |
| 372 | `getFlowExecutions` | GET | `/api/marketing-flows/${id}/executions` | - |
| 373 | `getFlowStats` | GET | `/api/marketing-flows/${id}/stats` | - |

## material.js（10）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 374 | `getMaterialList` | GET | `/api/material/list` | - |
| 375 | `uploadMaterial` | POST | `/api/material/upload` | - |
| 376 | `deleteMaterial` | DELETE | `/api/material/${id}` | - |
| 377 | `getMaterialCategories` | GET | `/api/material/categories` | - |
| 378 | `createMaterialCategory` | POST | `/api/material/categories` | - |
| 379 | `updateMaterialCategory` | PUT | `/api/material/categories/${id}` | - |
| 380 | `deleteMaterialCategory` | DELETE | `/api/material/categories/${id}` | - |
| 381 | `getMaterialSelector` | GET | `/api/material/selector` | - |
| 382 | `updateMaterialUsage` | POST | `/api/material/${id}/usage` | - |
| 383 | `getMaterialStats` | GET | `/api/material/stats` | - |

## messageHub.js（8）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 384 | `messageHubApi.pushMessage` | POST | `/api/message-hub/push` | - |
| 385 | `messageHubApi.pushBatch` | POST | `/api/message-hub/push-batch` | - |
| 386 | `messageHubApi.pushFromChannel` | POST | `/api/message-hub/push-from-channel` | - |
| 387 | `messageHubApi.getMessages` | GET | `/api/message-hub/list` | - |
| 388 | `messageHubApi.getMessageById` | GET | `/api/message-hub/${id}` | - |
| 389 | `messageHubApi.markRead` | POST | `/api/message-hub/${ids[0]}/read` | - |
| 390 | `messageHubApi.getStats` | GET | `/api/message-hub/stats` | - |
| 391 | `messageHubApi.getPlatforms` | GET | `/api/message-hub/platforms` | - |

## objection.js（4）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 392 | `handleObjection` | POST | `/api/objection/handle` | - |
| 393 | `classifyObjection` | POST | `/api/objection/classify` | - |
| 394 | `listObjectionCategories` | GET | `/api/objection/categories` | - |
| 395 | `recordObjectionUsage` | POST | `/api/objection/usage` | - |

## obs.js（8）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 396 | `getObsConfigList` | GET | `/api/obs/config` | - |
| 397 | `getObsConfig` | GET | `/api/obs/config/${id}` | - |
| 398 | `createObsConfig` | POST | `/api/obs/config` | - |
| 399 | `updateObsConfig` | PUT | `/api/obs/config/${id}` | - |
| 400 | `deleteObsConfig` | DELETE | `/api/obs/config/${id}` | - |
| 401 | `testObsConnection` | POST | `/api/obs/config/${id}/test` | - |
| 402 | `setDefaultObsConfig` | POST | `/api/obs/config/${id}/default` | - |
| 403 | `getDefaultObsConfig` | GET | `/api/obs/config/default` | - |

## oneid.js（7）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 404 | `listOneID` | GET | `/api/customer/oneid/list` | - |
| 405 | `listConflicts` | GET | `/api/customer/oneid/conflicts` | - |
| 406 | `mergeOneID` | POST | `/api/customer/oneid/merge` | - |
| 407 | `resolveConflict` | POST | `/api/customer/oneid/conflicts/${id}/resolve` | - |
| 408 | `getIdentityMappings` | GET | `/api/customer-oneid/${customerId}/identities` | - |
| 409 | `linkIdentity` | POST | `/api/customer-oneid/${customerId}/identities` | - |
| 410 | `resolveIdentity` | POST | `/api/customer/oneid/resolve` | - |

## operationLog.js（5）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 411 | `getOperationLogs` | GET | `/api/team/logs` | - |
| 412 | `getOperationLogDetail` | GET | `/api/team/logs/${id}` | - |
| 413 | `exportOperationLogs` | GET | `/api/team/logs/export` | - |
| 414 | `deleteOperationLogs` | DELETE | `/api/team/logs` | - |
| 415 | `cleanOperationLogs` | POST | `/api/team/logs/clean` | - |

## order.js（10）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 416 | `getOrderList` | GET | `/api/orders/list` | - |
| 417 | `getOrderByID` | GET | `/api/orders/${id}` | - |
| 418 | `createOrder` | POST | `/api/orders` | - |
| 419 | `cancelOrder` | POST | `/api/orders/${id}/cancel` | - |
| 423 | `getRecentOrderList` | GET | `/api/orders/recent` | - |
| 424 | `updateOrder` | PUT | `/api/order/${id}` | - |
| 425 | `deleteOrder` | DELETE | `/api/orders/${id}` | - |

## persona.js（2）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 442 | `listStaffs` | GET | `/api/analytics/persona/staffs` | - |
| 443 | `getPersonaReport` | GET | `/api/analytics/persona/staffs/${staffId}` | - |

## platform.js（5）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 444 | `platformAPI.getLatestMessage` | GET | `/api/platform/message/latest` | - |
| 445 | `platformAPI.markMessageRead` | POST | `/api/platform/message/${messageId}/read` | - |
| 447 | `platformAPI.reportAPILog` | POST | `/api/platform/report-api-log` | - |

## platformAccount.js（8）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 449 | `platformAccountApi.getAccounts` | GET | `/api/platform-accounts` | - |
| 450 | `platformAccountApi.getPlatforms` | GET | `/api/platform-accounts/platforms` | - |
| 451 | `platformAccountApi.getAccountById` | GET | `/api/platform-accounts/${id}` | - |
| 452 | `platformAccountApi.createAccount` | POST | `/api/platform-accounts` | - |
| 453 | `platformAccountApi.updateAccount` | PUT | `/api/platform-accounts/${id}` | - |
| 454 | `platformAccountApi.deleteAccount` | DELETE | `/api/platform-accounts/${id}` | - |
| 455 | `platformAccountApi.loginAccount` | POST | `/api/platform-accounts/${id}/login` | - |
| 456 | `platformAccountApi.checkStatus` | GET | `/api/platform-accounts/${id}/status` | - |

## rag-product-config.js（8）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 457 | `ragProductConfigAPI.createRagProduct` | POST | `/api/rag-config/products` | - |
| 458 | `ragProductConfigAPI.getRagProducts` | GET | `/api/rag-config/products` | - |
| 459 | `ragProductConfigAPI.listProducts` | GET | `/api/rag-config/products` | - |
| 460 | `ragProductConfigAPI.updateRagProduct` | PUT | `/api/rag-config/products/${id}` | - |
| 461 | `ragProductConfigAPI.deleteRagProduct` | DELETE | `/api/rag-config/products/${id}` | - |
| 462 | `ragProductConfigAPI.getAccountConfig` | GET | `/api/rag-config/accounts/config` | - |
| 463 | `ragProductConfigAPI.updateAccountConfig` | PUT | `/api/rag-config/accounts/config` | - |
| 464 | `ragProductConfigAPI.processMessage` | POST | `/api/rag-config/process-message` | - |

## reachPipeline.js（16）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 465 | `reachPipelineApi.getPipelines` | GET | `/api/reach/pipelines` | - |
| 466 | `reachPipelineApi.createPipeline` | POST | `/api/reach/pipelines` | - |
| 467 | `reachPipelineApi.getPipeline` | GET | `/api/reach/pipelines/${id}` | - |
| 468 | `reachPipelineApi.updatePipeline` | PUT | `/api/reach/pipelines/${id}` | - |
| 469 | `reachPipelineApi.deletePipeline` | DELETE | `/api/reach/pipelines/${id}` | - |
| 470 | `reachPipelineApi.pausePipeline` | POST | `/api/reach/pipelines/${id}/pause` | - |
| 471 | `reachPipelineApi.resumePipeline` | POST | `/api/reach/pipelines/${id}/resume` | - |
| 472 | `reachPipelineApi.archivePipeline` | POST | `/api/reach/pipelines/${id}/archive` | - |
| 473 | `reachPipelineApi.getJobs` | GET | `/api/reach/jobs` | - |
| 474 | `reachPipelineApi.enqueueJob` | POST | `/api/reach/jobs` | - |
| 475 | `reachPipelineApi.getJob` | GET | `/api/reach/jobs/${id}` | - |
| 476 | `reachPipelineApi.cancelJob` | POST | `/api/reach/jobs/${id}/cancel` | - |
| 477 | `reachPipelineApi.retryJob` | POST | `/api/reach/jobs/${id}/retry` | - |
| 478 | `reachPipelineApi.executeJob` | POST | `/api/reach/jobs/${id}/execute` | - |
| 479 | `reachPipelineApi.getStats` | GET | `/api/reach/stats` | - |
| 480 | `reachPipelineApi.resetRateLimit` | POST | `/api/reach/rate-limit/reset` | - |

## scriptTemplate.js（9）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 481 | `getScriptTemplateList` | GET | `/api/scripts` | - |
| 482 | `getScriptTemplate` | GET | `/api/scripts/${id}` | - |
| 483 | `createScriptTemplate` | POST | `/api/scripts` | - |
| 484 | `updateScriptTemplate` | PUT | `/api/scripts/${id}` | - |
| 485 | `deleteScriptTemplate` | DELETE | `/api/scripts/${id}` | - |
| 486 | `getScriptCategories` | GET | `/api/scripts/categories` | - |
| 487 | `searchScriptTemplates` | GET | `/api/scripts/search` | - |
| 488 | `getPublicScriptTemplates` | GET | `/api/scripts/public` | - |
| 489 | `recommendScript` | POST | `/api/scripts/recommend` | - |

## securityAudit.js（3）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 490 | `runSecurityAudit` | POST | `/api/security/audit` | - |
| 491 | `getSecurityAuditList` | GET | `/api/security/audit/list` | - |
| 492 | `getSecurityAuditDetail` | GET | `/api/security/audit/${id}` | - |

## shortLink.js（10）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 493 | `shortLinkApi.getList` | GET | `/api/shortlink/list` | - |
| 494 | `shortLinkApi.getById` | GET | `/api/shortlink/${id}` | - |
| 495 | `shortLinkApi.create` | POST | `/api/shortlink/create` | - |
| 496 | `shortLinkApi.update` | PUT | `/api/shortlink/update` | - |
| 497 | `shortLinkApi.delete` | DELETE | `/api/shortlink/delete/${id}` | - |
| 498 | `shortLinkApi.access` | POST | `/api/shortlink/access` | - |
| 499 | `shortLinkApi.generateShortCode` | POST | `/api/shortlink/generate` | - |
| 500 | `shortLinkApi.getStats` | GET | `/api/shortlink/${id}/stats` | - |
| 501 | `shortLinkApi.getAllStats` | GET | `/api/shortlink/stats/all` | - |
| 502 | `shortLinkApi.share` | POST | `/api/shortlink/${id}/share` | - |

## sms.js（20）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 503 | `getConfig` | GET | `/api/sms/config` | - |
| 504 | `saveConfig` | POST | `/api/sms/config` | - |
| 505 | `getSmsList` | GET | `/api/sms/list` | - |
| 506 | `getSmsDetail` | GET | `/api/sms/detail/${id}` | - |
| 507 | `resendSms` | POST | `/api/sms/resend/${id}` | - |
| 508 | `sendSms` | POST | `/api/sms/send` | - |
| 509 | `getDraftList` | GET | `/api/sms/draft/list` | - |
| 510 | `getDraftDetail` | GET | `/api/sms/draft/${id}` | - |
| 511 | `createDraft` | POST | `/api/sms/draft` | - |
| 512 | `updateDraft` | PUT | `/api/sms/draft/${id}` | - |
| 513 | `deleteDraft` | DELETE | `/api/sms/draft/${id}` | - |
| 514 | `sendDraft` | POST | `/api/sms/draft/${id}/send` | - |
| 515 | `getJobList` | GET | `/api/sms/job/list` | - |
| 516 | `getJobDetail` | GET | `/api/sms/job/${id}` | - |
| 517 | `pauseJob` | POST | `/api/sms/job/${id}/pause` | - |
| 518 | `resumeJob` | POST | `/api/sms/job/${id}/resume` | - |
| 519 | `stopJob` | POST | `/api/sms/job/${id}/stop` | - |
| 520 | `createJob` | POST | `/api/sms/job` | - |
| 521 | `deleteJob` | DELETE | `/api/sms/job/${id}` | - |
| 522 | `getJobRecords` | GET | `/api/sms/job/${id}/records` | - |

## sopAgent.js（16）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 523 | `sopApi.list` | GET | `/api/sop` | - |
| 524 | `sopApi.create` | POST | `/api/sop` | - |
| 525 | `sopApi.getStats` | GET | `/api/sop/stats` | - |
| 526 | `sopApi.matchByIntent` | GET | `/api/sop/match` | - |
| 527 | `sopApi.listExecutions` | GET | `/api/sop/executions` | - |
| 528 | `sopApi.getExecution` | GET | `/api/sop/executions/${id}` | - |
| 529 | `sopApi.pauseExecution` | POST | `/api/sop/executions/${id}/pause` | - |
| 530 | `sopApi.resumeExecution` | POST | `/api/sop/executions/${id}/resume` | - |
| 531 | `sopApi.cancelExecution` | POST | `/api/sop/executions/${id}/cancel` | - |
| 532 | `sopApi.get` | GET | `/api/sop/${id}` | - |
| 533 | `sopApi.update` | PUT | `/api/sop/${id}` | - |
| 534 | `sopApi.remove` | DELETE | `/api/sop/${id}` | - |
| 535 | `sopApi.activate` | POST | `/api/sop/${id}/activate` | - |
| 536 | `sopApi.deactivate` | POST | `/api/sop/${id}/deactivate` | - |
| 537 | `sopApi.execute` | POST | `/api/sop/execute` | - |
| 538 | `sopApi.step` | POST | `/api/sop/step` | - |

## system.js（3）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 540 | `SystemApi.getConfig` | GET | `/api/system/config` | - |
| 541 | `SystemApi.saveConfig` | POST | `/api/system/config` | - |

## tagSegmentation.js（11）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 542 | `TagSegmentationApi.getTags` | GET | `/api/customer-360/tags` | - |
| 543 | `TagSegmentationApi.updateTags` | PUT | `/api/customer-360/tags` | - |
| 544 | `TagSegmentationApi.createTag` | POST | `/api/customer-360/tags` | - |
| 545 | `TagSegmentationApi.deleteTag` | DELETE | `/api/customer-360/tags/${id}` | - |
| 546 | `TagSegmentationApi.getTagRules` | GET | `/api/customer-360/tag-rules` | - |
| 547 | `TagSegmentationApi.saveTagRule` | POST | `/api/customer-360/tag-rules` | - |
| 548 | `TagSegmentationApi.updateTagRule` | PUT | `/api/customer-360/tag-rules/${id}` | - |
| 549 | `TagSegmentationApi.deleteTagRule` | DELETE | `/api/customer-360/tag-rules/${id}` | - |
| 550 | `TagSegmentationApi.getLayerStrategy` | GET | `/api/user-segment/layers` | - |
| 551 | `TagSegmentationApi.saveLayerStrategy` | PUT | `/api/user-segment/layers` | - |
| 552 | `TagSegmentationApi.getTagStats` | GET | `/api/customer-360/tag-stats` | - |

## teamUser.js（19）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 553 | `getTeamUserList` | GET | `/api/team/users` | - |
| 554 | `getTeamUser` | GET | `/api/team/users/${id}` | - |
| 555 | `createTeamUser` | POST | `/api/team/users` | - |
| 556 | `updateTeamUser` | PUT | `/api/team/users/${id}` | - |
| 557 | `deleteTeamUser` | DELETE | `/api/team/users/${id}` | - |
| 558 | `resetTeamUserPassword` | POST | `/api/team/users/${id}/reset-password` | - |
| 559 | `getCurrentTeamUser` | GET | `/api/team/user/current` | - |
| 560 | `changeTeamUserPassword` | POST | `/api/team/user/change-password` | - |
| 561 | `getTeamRoleList` | GET | `/api/team/roles` | - |
| 562 | `createTeamRole` | POST | `/api/team/roles` | - |
| 563 | `updateTeamRole` | PUT | `/api/team/roles/${id}` | - |
| 564 | `deleteTeamRole` | DELETE | `/api/team/roles/${id}` | - |
| 565 | `getPermissions` | GET | `/api/team/permissions` | - |
| 566 | `getTeamMembers` | GET | `/api/team/logs/statistics` | - |
| 567 | `createTeamMember` | GET | `/api/team/logs/statistics` | - |
| 568 | `updateTeamMember` | GET | `/api/team/logs/statistics` | - |
| 569 | `deleteTeamMember` | GET | `/api/team/logs/statistics` | - |
| 570 | `resetTeamPassword` | GET | `/api/team/logs/statistics` | - |
| 571 | `getTeamStats` | GET | `/api/team/logs/statistics` | - |

## telegram.js（7）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 572 | `listAccounts` | GET | `/api/telegram/accounts` | - |
| 573 | `getAccount` | GET | `/api/telegram/accounts/${id}` | - |
| 574 | `createAccount` | POST | `/api/telegram/accounts` | - |
| 575 | `updateAccount` | PUT | `/api/telegram/accounts/${id}` | - |
| 576 | `deleteAccount` | DELETE | `/api/telegram/accounts/${id}` | - |
| 577 | `registerWebhook` | POST | `/api/telegram/accounts/${id}/register-webhook` | - |
| 578 | `testSend` | POST | `/api/telegram/accounts/${id}/test-send` | - |

## templateMarket.js（10）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 579 | `getTemplateMarketList` | GET | `/api/templates` | - |
| 580 | `getTemplateMarketDetail` | GET | `/api/templates/${id}` | - |
| 581 | `downloadTemplate` | POST | `/api/templates/${id}/download` | - |
| 582 | `getOfficialTemplates` | GET | `/api/templates/official` | - |
| 583 | `searchTemplates` | GET | `/api/templates/search` | - |
| 584 | `getMyDownloads` | GET | `/api/templates/my-downloads` | - |
| 585 | `getTemplates` | POST | `/api/templates` | - |
| 586 | `submitTemplate` | POST | `/api/templates` | - |
| 587 | `useTemplate` | POST | `/api/templates/${id}/rate` | - |
| 588 | `rateTemplate` | POST | `/api/templates/${id}/rate` | - |

## tiktokAutoReply.js（8）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 589 | `getTikTokAccounts` | GET | `/api/tiktok/auto-reply/accounts` | - |
| 590 | `getTikTokRule` | GET | `/api/tiktok/auto-reply/rule` | - |
| 591 | `saveTikTokRule` | POST | `/api/tiktok/auto-reply/rule` | - |
| 592 | `upsertTikTokAccount` | POST | `/api/tiktok/auto-reply/accounts` | - |
| 593 | `deleteTikTokAccount` | DELETE | `/api/tiktok/auto-reply/accounts/${accountId}` | - |
| 594 | `getTikTokLogs` | GET | `/api/tiktok/auto-reply/logs` | - |
| 595 | `startTikTokAutoReply` | POST | `/api/tiktok/auto-reply/start` | - |
| 596 | `stopTikTokAutoReply` | POST | `/api/tiktok/auto-reply/stop` | - |

## tiktokCard.js（8）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 597 | `getTikTokCardList` | GET | `/api/tiktok-card/list` | - |
| 598 | `getTikTokCard` | GET | `/api/tiktok-card/${id}` | - |
| 599 | `createTikTokCard` | POST | `/api/tiktok-card` | - |
| 600 | `updateTikTokCard` | PUT | `/api/tiktok-card/${data.id}` | - |
| 601 | `deleteTikTokCard` | DELETE | `/api/tiktok-card/${id}` | - |
| 602 | `generateShortLink` | POST | `/api/tiktok-card/generate-short-link` | - |
| 603 | `getTikTokCardOverallStats` | GET | `/api/tiktok-card/stats/overall` | - |
| 604 | `getTikTokCardStats` | GET | `/api/tiktok-card/${cardId}/stats` | - |

## tuning.js（16）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 605 | `getConfidenceSignals` | GET | `/api/admin/tuning/confidence/signals` | - |
| 606 | `getConfidenceSignal` | GET | `/api/admin/tuning/confidence/signals/${id}` | - |
| 607 | `getConfidenceSignalStats` | GET | `/api/admin/tuning/confidence/signals/stats` | - |
| 608 | `getConfidenceCalibrations` | GET | `/api/admin/tuning/confidence/calibrations` | - |
| 609 | `getThresholdPolicies` | GET | `/api/admin/tuning/confidence/policies` | - |
| 610 | `upsertThresholdPolicy` | PUT | `/api/admin/tuning/confidence/policies` | - |
| 611 | `getHumanizeScores` | GET | `/api/admin/tuning/humanize/scores` | - |
| 612 | `getHumanizeScoreStats` | GET | `/api/admin/tuning/humanize/scores/stats` | - |
| 613 | `getChampionBaselines` | GET | `/api/admin/tuning/humanize/baselines` | - |
| 614 | `getLowQualitySamples` | GET | `/api/admin/tuning/humanize/low-quality` | - |
| 615 | `getFeedbackEvents` | GET | `/api/admin/tuning/feedback/events` | - |
| 616 | `getFeedbackEventStats` | GET | `/api/admin/tuning/feedback/events/stats` | - |
| 617 | `getChampionDialogues` | GET | `/api/admin/tuning/feedback/dialogues` | - |
| 618 | `getPromptCandidates` | GET | `/api/admin/tuning/prompt/candidates` | - |
| 619 | `updatePromptCandidateStatus` | PUT | `/api/admin/tuning/prompt/candidates/${id}/status` | - |
| 620 | `getBanditArms` | GET | `/api/admin/tuning/bandit/arms` | - |

## unifiedInbox.js（14）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 621 | `unifiedInboxApi.listConversations` | GET | `/api/inbox` | - |
| 622 | `unifiedInboxApi.getStats` | GET | `/api/inbox/stats` | - |
| 623 | `unifiedInboxApi.listAssignments` | GET | `/api/inbox/assignments` | - |
| 624 | `unifiedInboxApi.assign` | POST | `/api/inbox/assign` | - |
| 625 | `unifiedInboxApi.autoAssign` | POST | `/api/inbox/auto-assign` | - |
| 626 | `unifiedInboxApi.getStaffLoad` | GET | `/api/inbox/staff/${encodeURIComponent(staff)}/load` | - |
| 627 | `unifiedInboxApi.getConversation` | GET | `/api/inbox/${id}` | - |
| 628 | `unifiedInboxApi.markRead` | POST | `/api/inbox/${id}/read` | - |
| 629 | `unifiedInboxApi.pin` | POST | `/api/inbox/${id}/pin` | - |
| 630 | `unifiedInboxApi.star` | POST | `/api/inbox/${id}/star` | - |
| 631 | `unifiedInboxApi.mute` | POST | `/api/inbox/${id}/mute` | - |
| 632 | `unifiedInboxApi.addTag` | POST | `/api/inbox/${id}/tags` | - |
| 633 | `unifiedInboxApi.removeTag` | DELETE | `/api/inbox/${id}/tags/${encodeURIComponent(tag)}` | - |
| 634 | `unifiedInboxApi.listMessages` | GET | `/api/inbox/${id}/messages` | - |

## unifiedMessage.js（3）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 635 | `unifiedMessageApi.getMessages` | GET | `/api/messages` | - |
| 636 | `unifiedMessageApi.getMessageById` | GET | `/api/messages/${id}` | - |
| 637 | `unifiedMessageApi.getReplies` | GET | `/api/messages/${id}/replies` | - |

## userSegment.js（14）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 638 | `getRFMList` | GET | `/api/user-segment/rfm/list` | - |
| 639 | `getRFMRule` | GET | `/api/user-segment/rfm/rule` | - |
| 640 | `saveRFMRule` | POST | `/api/user-segment/rfm/rule` | - |
| 641 | `updateRFMRule` | PUT | `/api/user-segment/rfm/rule/${id}` | - |
| 642 | `getUserRFM` | GET | `/api/user-segment/rfm/user?user_id=${userId}` | - |
| 643 | `getRFMStats` | GET | `/api/user-segment/rfm/stats` | - |
| 644 | `calculateRFM` | POST | `/api/user-segment/rfm/calculate` | - |
| 645 | `getLayerDescription` | GET | `/api/user-segment/layers` | - |
| 646 | `getUserSegments` | DELETE | `/api/user-segment/rfm/rule/${id}` | - |
| 647 | `getSegmentStats` | DELETE | `/api/user-segment/rfm/rule/${id}` | - |
| 648 | `createUserSegment` | DELETE | `/api/user-segment/rfm/rule/${id}` | - |
| 649 | `updateUserSegment` | DELETE | `/api/user-segment/rfm/rule/${id}` | - |
| 650 | `deleteUserSegment` | DELETE | `/api/user-segment/rfm/rule/${id}` | - |
| 651 | `getSegmentUsers` | GET | `/api/user-segment/rfm/list?segment_id=${id}` | - |

## users.js（7）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 652 | `usersApi.list` | GET | `/api/users` | - |
| 653 | `usersApi.get` | GET | `/api/users/${id}` | - |
| 654 | `usersApi.create` | POST | `/api/users` | - |
| 655 | `usersApi.update` | PUT | `/api/users/${id}` | - |
| 656 | `usersApi.delete` | DELETE | `/api/users/${id}` | - |
| 657 | `usersApi.updatePassword` | PUT | `/api/users/${id}/password` | - |
| 658 | `usersApi.login` | POST | `/api/auth/login` | - |

## wecomAccount.js（12）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 659 | `wecomAccountApi.listAccounts` | GET | `/api/wecom/health/accounts` | - |
| 660 | `wecomAccountApi.listRiskAccounts` | GET | `/api/wecom/health/accounts/risks` | - |
| 661 | `wecomAccountApi.selectHealthyAccount` | GET | `/api/wecom/health/accounts/select` | - |
| 662 | `wecomAccountApi.getHealthSummary` | GET | `/api/wecom/health/accounts/summary` | - |
| 663 | `wecomAccountApi.getLatestHealth` | GET | `/api/wecom/health/accounts/${id}` | - |
| 664 | `wecomAccountApi.listHealthHistory` | GET | `/api/wecom/health/accounts/${id}/history` | - |
| 665 | `wecomAccountApi.reportHealth` | POST | `/api/wecom/health/accounts/${id}` | - |
| 666 | `wecomAccountApi.updateAccountStatus` | POST | `/api/wecom/health/accounts/${id}/status` | - |
| 667 | `wecomAccountApi.consumeQuota` | POST | `/api/wecom/health/accounts/${id}/quota/consume` | - |
| 668 | `wecomAccountApi.resetDailyQuota` | POST | `/api/wecom/health/accounts/quota/reset` | - |
| 669 | `wecomAccountApi.ingestMessage` | POST | `/api/wecom/messages/ingest` | - |
| 670 | `wecomAccountApi.sendMessage` | POST | `/api/wecom/messages/send` | - |

## whatsapp.js（15）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 671 | `listAccounts` | GET | `/api/whatsapp/accounts` | - |
| 672 | `createAccount` | POST | `/api/whatsapp/accounts` | - |
| 673 | `startLogin` | POST | `/api/whatsapp/accounts/${id}/login/start` | - |
| 674 | `loginStatus` | GET | `/api/whatsapp/accounts/${id}/login/status` | - |
| 675 | `updateAccount` | PUT | `/api/whatsapp/accounts/${id}` | - |
| 676 | `deleteAccount` | DELETE | `/api/whatsapp/accounts/${id}` | - |
| 677 | `listDrafts` | GET | `/api/whatsapp/drafts` | - |
| 678 | `createDraft` | POST | `/api/whatsapp/drafts` | - |
| 679 | `updateDraft` | PUT | `/api/whatsapp/drafts/${id}` | - |
| 680 | `deleteDraft` | DELETE | `/api/whatsapp/drafts/${id}` | - |
| 681 | `listJobs` | GET | `/api/whatsapp/jobs` | - |
| 682 | `createJob` | POST | `/api/whatsapp/jobs` | - |
| 683 | `getJob` | GET | `/api/whatsapp/jobs/${id}` | - |
| 684 | `deleteJob` | DELETE | `/api/whatsapp/jobs/${id}` | - |
| 685 | `getAccounts` | POST | `/api/whatsapp/accounts` | - |

## xianyuAutoReply.js（7）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 686 | `xianyuAutoReplyApi.listAccounts` | GET | `/api/xianyu/auto-reply/accounts` | - |
| 687 | `xianyuAutoReplyApi.upsertAccount` | POST | `/api/xianyu/auto-reply/accounts` | - |
| 688 | `xianyuAutoReplyApi.deleteAccount` | DELETE | `/api/xianyu/auto-reply/accounts/${id}` | - |
| 689 | `xianyuAutoReplyApi.getRule` | GET | `/api/xianyu/auto-reply/rules` | - |
| 690 | `xianyuAutoReplyApi.saveRule` | POST | `/api/xianyu/auto-reply/rules` | - |
| 691 | `xianyuAutoReplyApi.start` | POST | `/api/xianyu/auto-reply/start` | - |
| 692 | `xianyuAutoReplyApi.stop` | POST | `/api/xianyu/auto-reply/stop` | - |

## xianyuCard.js（9）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 693 | `getXianyuCardList` | GET | `/api/xianyu/list` | - |
| 694 | `getXianyuCard` | GET | `/api/xianyu/${id}` | - |
| 695 | `createXianyuCard` | POST | `/api/xianyu/create` | - |
| 696 | `updateXianyuCard` | PUT | `/api/xianyu/update` | - |
| 697 | `deleteXianyuCard` | DELETE | `/api/xianyu/delete/${id}` | - |
| 698 | `viewXianyuCard` | GET | `/api/xianyu/view/${id}` | - |
| 699 | `getXianyuCardStats` | GET | `/api/xianyu/stats/card/${id}` | - |
| 700 | `getXianyuCardOverallStats` | GET | `/api/xianyu/stats/overall` | - |
| 701 | `generateXianyuShortLink` | POST | `/api/xianyu/${id}/generate-short-link` | - |

## xiaohongshuCard.js（9）

| # | 函数 | method | url | 行 |
|---|------|--------|-----|----|
| 702 | `getXiaohongshuCardList` | GET | `/api/xiaohongshu/list` | - |
| 703 | `getXiaohongshuCard` | GET | `/api/xiaohongshu/${id}` | - |
| 704 | `createXiaohongshuCard` | POST | `/api/xiaohongshu/create` | - |
| 705 | `updateXiaohongshuCard` | PUT | `/api/xiaohongshu/update` | - |
| 706 | `deleteXiaohongshuCard` | DELETE | `/api/xiaohongshu/delete/${id}` | - |
| 707 | `viewXiaohongshuCard` | GET | `/api/xiaohongshu/view/${id}` | - |
| 708 | `getXiaohongshuCardStats` | GET | `/api/xiaohongshu/stats/card/${id}` | - |
| 709 | `getXiaohongshuCardOverallStats` | GET | `/api/xiaohongshu/stats/overall` | - |
| 710 | `generateShortLink` | POST | `/api/xiaohongshu/${id}/generate-short-link` | - |

