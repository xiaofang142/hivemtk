# User-Web API 完整清单

## 1. 邮件管理 (email)

### 页面: EmailList.vue (邮件列表)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getEmailList | GET | /api/email/list | page, limit | 获取邮件列表 |
| sendEmail | POST | /api/email/list | subject, content, attachments | 发送邮件 |
| deleteEmailList | DELETE | /api/email/list/:id | id | 删除邮件 |
| getEmailTrace | POST | /api/email/list/:id/trace | id | 获取邮件追踪 |

### 页面: Drafts.vue (草稿箱)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getDrafts | GET | /api/email/drafts | - | 获取草稿列表 |
| createDraft | POST | /api/email/drafts | data | 创建草稿 |
| updateDraft | PUT | /api/email/drafts/:id | id, data | 更新草稿 |
| deleteDraft | DELETE | /api/email/drafts/:id | id | 删除草稿 |

### 页面: Jobs.vue (任务)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getJobsList | GET | /api/email/jobs | page, limit | 获取任务列表 |
| getJobDetail | GET | /api/email/jobs/:id | id | 获取任务详情 |
| deleteJob | DELETE | /api/email/jobs/:id | id | 删除任务 |

### 页面: Smtp.vue (邮件账号)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getEmailSmtpList | GET | /api/email/smtp | - | 获取SMTP列表 |
| addEmailSmtp | POST | /api/email/smtp | data | 添加账号 |
| updateEmailSmtp | PUT | /api/email/smtp/:id | id, data | 更新账号 |
| deleteEmailSmtp | DELETE | /api/email/smtp/:id | id | 删除账号 |

---

## 2. Telegram 管理

### 页面: AccountList.vue (账号列表)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| listAccounts | GET | /api/telegram/accounts | params | 获取账号列表 |
| getAccount | GET | /api/telegram/accounts/:id | id | 获取账号详情 |
| createAccount | POST | /api/telegram/accounts | data | 创建Bot账号 |
| updateAccount | PUT | /api/telegram/accounts/:id | id, data | 更新账号 |
| deleteAccount | DELETE | /api/telegram/accounts/:id | id | 删除账号 |
| registerWebhook | POST | /api/telegram/accounts/:id/register-webhook | id, data | 注册Webhook |
| testSend | POST | /api/telegram/accounts/:id/test-send | id, data | 测试发送 |

---

## 3. WhatsApp 管理

### 页面: AccountList.vue (账号列表)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| listAccounts | GET | /api/whatsapp/accounts | params | 获取账号列表 |
| createAccount | POST | /api/whatsapp/accounts | data | 创建账号 |
| startLogin | POST | /api/whatsapp/accounts/:id/login/start | id | 开始登录 |
| loginStatus | GET | /api/whatsapp/accounts/:id/login/status | id | 获取登录状态 |
| updateAccount | PUT | /api/whatsapp/accounts/:id | id, data | 更新账号 |
| deleteAccount | DELETE | /api/whatsapp/accounts/:id | id | 删除账号 |

### 页面: Drafts.vue (草稿箱)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| listDrafts | GET | /api/whatsapp/drafts | params | 获取草稿列表 |
| createDraft | POST | /api/whatsapp/drafts | data | 创建草稿 |
| updateDraft | PUT | /api/whatsapp/drafts/:id | id, data | 更新草稿 |
| deleteDraft | DELETE | /api/whatsapp/drafts/:id | id | 删除草稿 |

### 页面: Jobs.vue (任务)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| listJobs | GET | /api/whatsapp/jobs | params | 获取任务列表 |
| createJob | POST | /api/whatsapp/jobs | data | 创建任务 |
| getJob | GET | /api/whatsapp/jobs/:id | id | 获取任务详情 |
| deleteJob | DELETE | /api/whatsapp/jobs/:id | id | 删除任务 |

---

## 4. 线索管理 (clue)

### 页面: List.vue (线索列表)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| list | GET | /api/clues/list | page, limit | 获取线索列表 |
| delete | DELETE | /api/clues/delete/:id | id | 删除线索 |
| statistics | GET | /api/clues/statistics | - | 获取统计 |
| import | POST | /api/clues/import | data | 导入线索 |

---

## 5. 系统管理 (system)

### 页面: SystemConfig.vue (系统配置)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getConfig | GET | /api/system/config | - | 获取配置 |
| saveConfig | POST | /api/system/config | data | 保存配置 |

---

## 6. 域名池管理 (domainPool)

### 页面: DomainPoolList.vue (域名池列表)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getList | GET | /api/domainpool/list | params | 获取列表 |
| getById | GET | /api/domainpool/:id | id | 获取详情 |
| create | POST | /api/domainpool/create | data | 创建 |
| update | PUT | /api/domainpool/update | data | 更新 |
| delete | DELETE | /api/domainpool/delete/:id | id | 删除 |
| checkDomain | POST | /api/domainpool/check/:id | id | 检查域名 |
| checkAllDomains | POST | /api/domainpool/checkall | - | 检查所有 |

---

## 7. 短链管理 (shortLink)

### 页面: ShortLinkList.vue (短链列表)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getList | GET | /api/shortlink/list | params | 获取列表 |
| getById | GET | /api/shortlink/:id | id | 获取详情 |
| create | POST | /api/shortlink/create | data | 创建 |
| update | PUT | /api/shortlink/update | data | 更新 |
| delete | DELETE | /api/shortlink/delete/:id | id | 删除 |
| getStats | GET | /api/shortlink/:id/stats | id, params | 获取统计 |
| getAllStats | GET | /api/shortlink/stats/all | params | 获取所有统计 |

---

## 8. 抖音卡片 (douyinCard)

### 页面: DouyinCardList.vue (卡片列表)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getDouyinCardList | GET | /api/douyin/list | params | 获取列表 |
| getDouyinCard | GET | /api/douyin/:id | id | 获取详情 |
| createDouyinCard | POST | /api/douyin/create | data | 创建 |
| updateDouyinCard | PUT | /api/douyin/update | data | 更新 |
| deleteDouyinCard | DELETE | /api/douyin/delete/:id | id | 删除 |
| getDouyinCardStats | GET | /api/douyin/stats/card/:id | id, params | 获取统计 |
| getDouyinCardOverallStats | GET | /api/douyin/stats/overall | params | 获取总体统计 |

---

## 9. 小红书卡片 (xiaohongshuCard)

### 页面: XiaohongshuCardList.vue (卡片列表)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getXiaohongshuCardList | GET | /api/xiaohongshu/list | params | 获取列表 |
| getXiaohongshuCard | GET | /api/xiaohongshu/:id | id | 获取详情 |
| createXiaohongshuCard | POST | /api/xiaohongshu/create | data | 创建 |
| updateXiaohongshuCard | PUT | /api/xiaohongshu/update | data | 更新 |
| deleteXiaohongshuCard | DELETE | /api/xiaohongshu/delete/:id | id | 删除 |
| getXiaohongshuCardStats | GET | /api/xiaohongshu/stats/card/:id | id, params | 获取统计 |
| getXiaohongshuCardOverallStats | GET | /api/xiaohongshu/stats/overall | params | 获取总体统计 |

---

## 10. 快手卡片 (kuaishouCard)

### 页面: KuaishouCardList.vue (卡片列表)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getKuaishouCardList | GET | /api/kuaishou/list | params | 获取列表 |
| getKuaishouCard | GET | /api/kuaishou/:id | id | 获取详情 |
| createKuaishouCard | POST | /api/kuaishou/create | data | 创建 |
| updateKuaishouCard | PUT | /api/kuaishou/update | data | 更新 |
| deleteKuaishouCard | DELETE | /api/kuaishou/delete/:id | id | 删除 |
| getKuaishouCardStats | GET | /api/kuaishou/stats/card/:id | id, params | 获取统计 |
| getKuaishouCardOverallStats | GET | /api/kuaishou/stats/overall | params | 获取总体统计 |

---

## 11. 闲鱼卡片 (xianyuCard)

### 页面: XianyuCardList.vue (卡片列表)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getXianyuCardList | GET | /api/xianyu/list | params | 获取列表 |
| getXianyuCard | GET | /api/xianyu/:id | id | 获取详情 |
| createXianyuCard | POST | /api/xianyu/create | data | 创建 |
| updateXianyuCard | PUT | /api/xianyu/update | data | 更新 |
| deleteXianyuCard | DELETE | /api/xianyu/delete/:id | id | 删除 |
| getXianyuCardStats | GET | /api/xianyu/stats/card/:id | id, params | 获取统计 |
| getXianyuCardOverallStats | GET | /api/xianyu/stats/overall | params | 获取总体统计 |

---

## 12. 短信管理 (sms)

### 页面: SmsList.vue (短信列表)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getSmsList | GET | /api/sms/list | params | 获取列表 |
| getSmsDetail | GET | /api/sms/detail/:id | id | 获取详情 |
| sendSms | POST | /api/sms/send | data | 发送短信 |
| resendSms | POST | /api/sms/resend/:id | id | 重发短信 |

### 页面: Drafts.vue (草稿箱)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getDraftList | GET | /api/sms/draft/list | params | 获取草稿列表 |
| createDraft | POST | /api/sms/draft | data | 创建草稿 |
| updateDraft | PUT | /api/sms/draft/:id | id, data | 更新草稿 |
| deleteDraft | DELETE | /api/sms/draft/:id | id | 删除草稿 |
| sendDraft | POST | /api/sms/draft/:id/send | id, data | 发送草稿 |

### 页面: Jobs.vue (任务)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getJobList | GET | /api/sms/job/list | params | 获取任务列表 |
| createJob | POST | /api/sms/job | data | 创建任务 |
| getJobDetail | GET | /api/sms/job/:id | id | 获取任务详情 |
| deleteJob | DELETE | /api/sms/job/:id | id | 删除任务 |
| pauseJob | POST | /api/sms/job/:id/pause | id | 暂停任务 |
| resumeJob | POST | /api/sms/job/:id/resume | id | 恢复任务 |
| stopJob | POST | /api/sms/job/:id/stop | id | 停止任务 |
| getJobRecords | GET | /api/sms/job/:id/records | id, params | 获取任务记录 |

---

## 13. 活码管理 (livecode)

### 页面: LiveCodeList.vue (活码列表)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getLiveCodes | GET | /api/live-codes/list | params | 获取列表 |
| getLiveCode | GET | /api/live-codes/:id | id | 获取详情 |
| createLiveCode | POST | /api/live-codes/create | data | 创建 |
| updateLiveCode | PUT | /api/live-codes/:id/update | id, data | 更新 |
| deleteLiveCode | DELETE | /api/live-codes/:id/delete | id | 删除 |
| getLiveCodeStats | GET | /api/live-codes/:id/stats | id | 获取统计 |
| getLiveCodeQRs | GET | /api/live-codes/:id/qrcodes | id | 获取二维码列表 |
| generateLiveCodeQR | POST | /api/live-codes/:id/qrcodes/create | id, data | 生成二维码 |
| shareLiveCode | POST | /api/live-codes/:id/share | id, data | 分享活码 |

---

## 14. 集成管理 (integration)

### 页面: IntegrationList.vue (集成列表)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getIntegrationAccountList | GET | /api/integrations | params | 获取列表 |
| getIntegrationAccount | GET | /api/integrations/:id | id | 获取详情 |
| createIntegrationAccount | POST | /api/integrations | data | 创建 |
| updateIntegrationAccount | PUT | /api/integrations/:id | id, data | 更新 |
| deleteIntegrationAccount | DELETE | /api/integrations/:id | id | 删除 |
| testIntegration | POST | /api/integrations/:id/test | id | 测试集成 |
| syncCustomers | POST | /api/integrations/:id/sync-customers | id | 同步客户 |
| syncProducts | POST | /api/integrations/:id/sync-products | id | 同步产品 |
| getSyncLogs | GET | /api/integration/sync-logs | params | 获取同步日志 |
| getExternalCustomers | GET | /api/integration/external-customers | - | 获取外部客户 |
| getExternalOrders | GET | /api/integration/external-orders | - | 获取外部订单 |
| getExternalProducts | GET | /api/integration/external-products | - | 获取外部产品 |

---

## 15. 客户360 (customer360)

### 页面: Customer360List.vue (客户列表)
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getCustomerList | GET | /api/customer/list | params | 获取客户列表 |
| getCustomer360Detail | GET | /api/customer/360/:id | id | 获取客户360详情 |
| getCustomerDetail | GET | /api/customer/:id | id | 获取客户详情 |
| updateCustomer | PUT | /api/customer/:id | id, data | 更新客户 |
| addCustomerTag | POST | /api/customer/:id/tags | id, tag | 添加标签 |
| removeCustomerTag | DELETE | /api/customer/:id/tags/:tag | id, tag | 删除标签 |
| getCustomerBehaviors | GET | /api/customer/:id/behaviors | id | 获取行为记录 |
| getCustomerCommunications | GET | /api/customer/:id/communications | id | 获取沟通记录 |

---

## 16. 批量操作 (batchOperation)

### 页面: BatchOperation.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| importFile | POST | /api/batch/import | file | 导入文件 |
| downloadTemplate | GET | /api/batch/template | - | 下载模板 |
| exportData | POST | /api/batch/export | data | 导出数据 |
| batchDelete | POST | /api/batch/delete | data | 批量删除 |
| batchUpdate | POST | /api/batch/update | data | 批量更新 |
| getTools | GET | /api/batch/tools | - | 获取工具列表 |
| getHistories | GET | /api/batch/histories | - | 获取历史记录 |
| getHistoryByID | GET | /api/batch/histories/:id | id | 获取历史详情 |
| cancelHistory | POST | /api/batch/histories/:id/cancel | id | 取消历史 |
| preview | POST | /api/batch/preview | data | 预览 |

---

## 17. 营销流程 (marketingFlow)

### 页面: MarketingFlowList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getFlowList | GET | /api/marketing-flows | - | 获取流程列表 |
| getFlowByID | GET | /api/marketing-flows/:id | id | 获取流程详情 |
| createFlow | POST | /api/marketing-flows | data | 创建流程 |
| updateFlow | PUT | /api/marketing-flows/:id | id, data | 更新流程 |
| deleteFlow | DELETE | /api/marketing-flows/:id | id | 删除流程 |
| activateFlow | POST | /api/marketing-flows/:id/activate | id | 激活流程 |
| pauseFlow | POST | /api/marketing-flows/:id/pause | id | 暂停流程 |
| stopFlow | POST | /api/marketing-flows/:id/stop | id | 停止流程 |
| getExecutionList | GET | /api/marketing-flows/:id/executions | id | 获取执行列表 |
| getExecutionStats | GET | /api/marketing-flows/:id/stats | id | 获取执行统计 |

---

## 18. 自定义报表 (customReport)

### 页面: CustomReportList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getReportList | GET | /api/custom-reports | - | 获取报表列表 |
| getReport | GET | /api/custom-reports/:id | id | 获取报表详情 |
| createReport | POST | /api/custom-reports | data | 创建报表 |
| updateReport | PUT | /api/custom-reports/:id | id, data | 更新报表 |
| deleteReport | DELETE | /api/custom-reports/:id | id | 删除报表 |
| getPublicTemplates | GET | /api/custom-reports/templates | - | 获取公共模板 |
| useTemplate | POST | /api/custom-reports/templates/:id/use | id | 使用模板 |
| queryReportData | GET | /api/custom-reports/:id/data | id, params | 查询报表数据 |

---

## 19. 数据大屏 (dashboardScreen)

### 页面: DashboardScreenList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getScreenList | GET | /api/dashboards | - | 获取大屏列表 |
| getDashboardData | GET | /api/dashboards/data | - | 获取仪表板数据 |
| getRealtimeActivities | GET | /api/dashboards/activities | - | 获取实时活动 |
| getScreenByID | GET | /api/dashboards/:id | id | 获取大屏详情 |
| createScreen | POST | /api/dashboards | data | 创建大屏 |
| updateScreen | PUT | /api/dashboards/:id | id, data | 更新大屏 |
| deleteScreen | DELETE | /api/dashboards/:id | id | 删除大屏 |

---

## 20. A/B测试 (abExperiment)

### 页面: ABExperimentList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getExperimentList | GET | /api/ab-experiments | - | 获取实验列表 |
| getExperiment | GET | /api/ab-experiments/:id | id | 获取实验详情 |
| createExperiment | POST | /api/ab-experiments | data | 创建实验 |
| updateExperiment | PUT | /api/ab-experiments/:id | id, data | 更新实验 |
| deleteExperiment | DELETE | /api/ab-experiments/:id | id | 删除实验 |
| startExperiment | POST | /api/ab-experiments/:id/start | id | 启动实验 |
| pauseExperiment | POST | /api/ab-experiments/:id/pause | id | 暂停实验 |
| stopExperiment | POST | /api/ab-experiments/:id/stop | id | 停止实验 |
| getExperimentResults | GET | /api/ab-experiments/:id/results | id | 获取实验结果 |
| getConversionEvents | GET | /api/ab-experiments/:id/conversion-events | id | 获取转化事件 |

---

## 21. 流失预警 (churnPrediction)

### 页面: ChurnPredictionList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getChurnPrediction | GET | /api/churn/prediction | - | 获取流失预测 |
| getChurnPredictions | GET | /api/churn/predictions | - | 获取流失预测列表 |
| getHighRiskUsers | GET | /api/churn/high-risk-users | - | 获取高风险用户 |
| getChurnWarnings | GET | /api/churn/warnings | - | 获取流失预警 |
| getUnhandledWarnings | GET | /api/churn/unhandled-warnings | - | 获取未处理预警 |
| markWarningHandled | POST | /api/churn/warnings/:id/handle | id | 标记预警已处理 |
| interveneUser | POST | /api/churn/warnings/intervene | data | 干预用户 |
| getModelConfig | GET | /api/churn/model-config | - | 获取模型配置 |
| saveModelConfig | POST | /api/churn/model-config | data | 保存模型配置 |
| getChurnStatistics | GET | /api/churn/statistics | - | 获取流失统计 |
| getRiskDistribution | GET | /api/churn/risk-distribution | - | 获取风险分布 |

---

## 22. 社群管理 (community)

### 页面: CommunityList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getGroups | GET | /api/community/groups | - | 获取群组列表 |
| getGroupByID | GET | /api/community/groups/:id | id | 获取群组详情 |
| createGroup | POST | /api/community/groups | data | 创建群组 |
| updateGroup | PUT | /api/community/groups/:id | id, data | 更新群组 |
| deleteGroup | DELETE | /api/community/groups/:id | id | 删除群组 |
| getMembers | GET | /api/community/members | - | 获取成员列表 |
| addMember | POST | /api/community/members | data | 添加成员 |
| getMemberByID | GET | /api/community/members/:id | id | 获取成员详情 |
| updateMember | PUT | /api/community/members/:id | id, data | 更新成员 |
| removeMember | DELETE | /api/community/members/:id | id | 删除成员 |
| getMessages | GET | /api/community/messages | - | 获取消息列表 |
| getStatistics | GET | /api/community/stats | - | 获取统计 |
| importData | POST | /api/community/import | data | 导入数据 |
| exportData | POST | /api/community/export | data | 导出数据 |

---

## 23. 用户分层 (userSegment)

### 页面: UserSegmentList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getRFMRule | GET | /api/user-segment/rfm/rule | - | 获取RFM规则 |
| listRFMRules | GET | /api/user-segment/rfm/rules | - | 获取RFM规则列表 |
| saveRFMRule | POST | /api/user-segment/rfm/rule | data | 保存RFM规则 |
| updateRFMRule | PUT | /api/user-segment/rfm/rule/:id | id, data | 更新RFM规则 |
| deleteRFMRule | DELETE | /api/user-segment/rfm/rule/:id | id | 删除RFM规则 |
| getRFMList | GET | /api/user-segment/rfm/list | - | 获取RFM列表 |
| getUserRFM | GET | /api/user-segment/rfm/user | - | 获取用户RFM |
| getRFMStats | GET | /api/user-segment/rfm/stats | - | 获取RFM统计 |
| calculateRFM | POST | /api/user-segment/rfm/calculate | - | 计算RFM |
| getLayerDescription | GET | /api/user-segment/layers | - | 获取层级描述 |

---

## 24. 意图识别 (intentRecognition)

### 页面: IntentRecognitionList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getIntentList | GET | /api/intent/list | - | 获取意图列表 |
| createIntent | POST | /api/intent | data | 创建意图 |
| updateIntent | PUT | /api/intent/:id | id, data | 更新意图 |
| deleteIntent | DELETE | /api/intent/:id | id | 删除意图 |

---

## 25. 对话记忆 (dialogueMemory)

### 页面: DialogueMemoryList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getMemoryList | GET | /api/dialogue-memory/list | - | 获取记忆列表 |
| getMemoryDetail | GET | /api/dialogue-memory/:id | id | 获取记忆详情 |
| deleteMemory | DELETE | /api/dialogue-memory/:id | id | 删除记忆 |

---

## 26. SOP智能体 (sopAgent)

### 页面: SOPAgentList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getSOPList | GET | /api/sop/list | - | 获取SOP列表 |
| createSOP | POST | /api/sop | data | 创建SOP |
| updateSOP | PUT | /api/sop/:id | id, data | 更新SOP |
| deleteSOP | DELETE | /api/sop/:id | id | 删除SOP |

---

## 27. 触达管道 (reachPipeline)

### 页面: ReachPipelineList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getPipelineList | GET | /api/reach-pipeline/list | - | 获取管道列表 |
| createPipeline | POST | /api/reach-pipeline | data | 创建管道 |
| updatePipeline | PUT | /api/reach-pipeline/:id | id, data | 更新管道 |
| deletePipeline | DELETE | /api/reach-pipeline/:id | id | 删除管道 |

---

## 28. 统一收件箱 (unifiedInbox)

### 页面: UnifiedInboxList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getMessageList | GET | /api/unified-inbox | - | 获取消息列表 |
| getMessageDetail | GET | /api/unified-inbox/:id | id | 获取消息详情 |
| markAsRead | POST | /api/unified-inbox/:id/read | id | 标记已读 |
| deleteMessage | DELETE | /api/unified-inbox/:id | id | 删除消息 |

---

## 29. 企微账号 (wecomAccount)

### 页面: WecomAccountList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getAccountList | GET | /api/wecom/accounts | - | 获取账号列表 |
| createAccount | POST | /api/wecom/accounts | data | 创建账号 |
| updateAccount | PUT | /api/wecom/accounts/:id | id, data | 更新账号 |
| deleteAccount | DELETE | /api/wecom/accounts/:id | id | 删除账号 |

---

## 30. WhatsApp Cloud (whatsappCloud)

### 页面: WhatsAppCloudList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getAccountList | GET | /api/whatsapp-cloud/accounts | - | 获取账号列表 |
| createAccount | POST | /api/whatsapp-cloud/accounts | data | 创建账号 |
| updateAccount | PUT | /api/whatsapp-cloud/accounts/:id | id, data | 更新账号 |
| deleteAccount | DELETE | /api/whatsapp-cloud/accounts/:id | id | 删除账号 |

---

## 31. 钉钉应用 (dingtalkApp)

### 页面: DingTalkAppList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getAppList | GET | /api/dingtalk/apps | - | 获取应用列表 |
| createApp | POST | /api/dingtalk/apps | data | 创建应用 |
| updateApp | PUT | /api/dingtalk/apps/:id | id, data | 更新应用 |
| deleteApp | DELETE | /api/dingtalk/apps/:id | id | 删除应用 |

---

## 32. LLM路由 (llmRouting)

### 页面: LLMRoutingList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getRouteList | GET | /api/llm-routing/list | - | 获取路由列表 |
| createRoute | POST | /api/llm-routing | data | 创建路由 |
| updateRoute | PUT | /api/llm-routing/:id | id, data | 更新路由 |
| deleteRoute | DELETE | /api/llm-routing/:id | id | 删除路由 |

---

## 33. 标签分层 (tagSegmentation)

### 页面: TagSegmentationList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getTagList | GET | /api/tag-segmentation/list | - | 获取标签列表 |
| createTag | POST | /api/tag-segmentation | data | 创建标签 |
| updateTag | PUT | /api/tag-segmentation/:id | id, data | 更新标签 |
| deleteTag | DELETE | /api/tag-segmentation/:id | id | 删除标签 |

---

## 34. 转化漏斗 (conversionFunnel)

### 页面: ConversionFunnelList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getFunnelList | GET | /api/conversion-funnel/list | - | 获取漏斗列表 |
| createFunnel | POST | /api/conversion-funnel | data | 创建漏斗 |
| updateFunnel | PUT | /api/conversion-funnel/:id | id, data | 更新漏斗 |
| deleteFunnel | DELETE | /api/conversion-funnel/:id | id | 删除漏斗 |

---

## 35. AI产能 (aiProductivity)

### 页面: AIProductivityList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getProductivityList | GET | /api/ai-productivity/list | - | 获取产能列表 |
| getProductivityDetail | GET | /api/ai-productivity/:id | id | 获取产能详情 |

---

## 36. 知识库 (knowledge)

### 页面: KnowledgeList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getKnowledgeList | GET | /api/knowledge/list | - | 获取知识库列表 |
| createKnowledge | POST | /api/knowledge | data | 创建知识库 |
| updateKnowledge | PUT | /api/knowledge/:id | id, data | 更新知识库 |
| deleteKnowledge | DELETE | /api/knowledge/:id | id | 删除知识库 |

---

## 37. AI智能体 (aiAgent)

### 页面: AIAgentList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getAgentList | GET | /api/ai-agents | - | 获取智能体列表 |
| getAgent | GET | /api/ai-agents/:id | id | 获取智能体详情 |
| createAgent | POST | /api/ai-agents | data | 创建智能体 |
| updateAgent | PUT | /api/ai-agents/:id | id, data | 更新智能体 |
| deleteAgent | DELETE | /api/ai-agents/:id | id | 删除智能体 |

---

## 38. 资产市场 (assetMarket)

### 页面: AssetMarketList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getAssetList | GET | /api/v1/asset-market/list | - | 获取资产列表 |
| getAsset | GET | /api/v1/asset-market/detail/:id | id | 获取资产详情 |
| purchaseAsset | POST | /api/v1/asset-market/purchase | - | 购买资产 |

---

## 39. 资产包 (assetBundle)

### 页面: AssetBundleList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getBundleList | GET | /api/asset-bundle/list | - | 获取资产包列表 |
| getBundle | GET | /api/asset-bundle/:id | id | 获取资产包详情 |
| createBundle | POST | /api/asset-bundle | data | 创建资产包 |
| updateBundle | PUT | /api/asset-bundle/:id | id, data | 更新资产包 |
| deleteBundle | DELETE | /api/asset-bundle/:id | id | 删除资产包 |

---

## 40. 客服管理 (customerService)

### 页面: CustomerServiceList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getAgentList | GET | /api/customer-service/agents | - | 获取坐席列表 |
| updateAgentStatus | PUT | /api/customer-service/agents/:id/status | id, data | 更新坐席状态 |
| getQuickReplies | GET | /api/customer-service/quick-replies | - | 获取快捷回复 |
| createQuickReply | POST | /api/customer-service/quick-replies | data | 创建快捷回复 |
| deleteQuickReply | DELETE | /api/customer-service/quick-replies/:id | id | 删除快捷回复 |

---

## 41. 渠道管理 (chatChannel)

### 页面: ChatChannelList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getChannelList | GET | /api/chat-channels | - | 获取渠道列表 |
| getChannel | GET | /api/chat-channels/:id | id | 获取渠道详情 |
| createChannel | POST | /api/chat-channels | data | 创建渠道 |
| updateChannel | PUT | /api/chat-channels/:id | id, data | 更新渠道 |
| deleteChannel | DELETE | /api/chat-channels/:id | id | 删除渠道 |

---

## 42. 异议处理 (objection)

### 页面: ObjectionList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getObjectionList | GET | /api/objection/list | - | 获取异议列表 |
| createObjection | POST | /api/objection | data | 创建异议 |
| updateObjection | PUT | /api/objection/:id | id, data | 更新异议 |
| deleteObjection | DELETE | /api/objection/:id | id | 删除异议 |

---

## 43. 客户画像 (persona)

### 页面: PersonaList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getPersonaList | GET | /api/persona/list | - | 获取画像列表 |
| createPersona | POST | /api/persona | data | 创建画像 |
| updatePersona | PUT | /api/persona/:id | id, data | 更新画像 |
| deletePersona | DELETE | /api/persona/:id | id | 删除画像 |

---

## 44. 客户旅程 (customerJourney)

### 页面: CustomerJourneyList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getJourneyList | GET | /api/customer-journey/list | - | 获取旅程列表 |
| getJourneyDetail | GET | /api/customer-journey/:id | id | 获取旅程详情 |
| createJourney | POST | /api/customer-journey | data | 创建旅程 |
| updateJourney | PUT | /api/customer-journey/:id | id, data | 更新旅程 |
| deleteJourney | DELETE | /api/customer-journey/:id | id | 删除旅程 |

---

## 45. 备份恢复 (backup)

### 页面: BackupList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getBackupList | GET | /api/backup/list | - | 获取备份列表 |
| createBackup | POST | /api/backup | data | 创建备份 |
| restoreBackup | POST | /api/backup/:id/restore | id | 恢复备份 |
| deleteBackup | DELETE | /api/backup/:id | id | 删除备份 |

---

## 46. 安全审计 (securityAudit)

### 页面: SecurityAuditList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getAuditList | GET | /api/security-audit/list | - | 获取审计列表 |
| getAuditDetail | GET | /api/security-audit/:id | id | 获取审计详情 |

---

## 47. 飞书管理 (feishu)

### 页面: FeishuList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getAccountList | GET | /api/feishu/accounts | - | 获取账号列表 |
| createAccount | POST | /api/feishu/accounts | data | 创建账号 |
| updateAccount | PUT | /api/feishu/accounts/:id | id, data | 更新账号 |
| deleteAccount | DELETE | /api/feishu/accounts/:id | id | 删除账号 |

---

## 48. 置信度/拟人度/反馈学习 (tuning)

### 页面: Confidence.vue / Humanize.vue / FeedbackLoop.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getTuningConfig | GET | /api/tuning/config | - | 获取配置 |
| saveTuningConfig | POST | /api/tuning/config | data | 保存配置 |
| getTuningStats | GET | /api/tuning/stats | - | 获取统计 |

---

## 49. 人员管理 (systemUser)

### 页面: SystemUserList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getUserList | GET | /api/system/users | - | 获取用户列表 |
| getUser | GET | /api/system/users/:id | id | 获取用户详情 |
| createUser | POST | /api/system/users | data | 创建用户 |
| updateUser | PUT | /api/system/users/:id | id, data | 更新用户 |
| deleteUser | DELETE | /api/system/users/:id | id | 删除用户 |

---

## 50. 角色管理 (role)

### 页面: RoleList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getRoleList | GET | /api/system/roles | - | 获取角色列表 |
| getRole | GET | /api/system/roles/:id | id | 获取角色详情 |
| createRole | POST | /api/system/roles | data | 创建角色 |
| updateRole | PUT | /api/system/roles/:id | id, data | 更新角色 |
| deleteRole | DELETE | /api/system/roles/:id | id | 删除角色 |

---

## 51. 授权管理 (permission)

### 页面: PermissionList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getPermissionList | GET | /api/system/permissions | - | 获取权限列表 |
| getPermission | GET | /api/system/permissions/:id | id | 获取权限详情 |
| createPermission | POST | /api/system/permissions | data | 创建权限 |
| updatePermission | PUT | /api/system/permissions/:id | id, data | 更新权限 |
| deletePermission | DELETE | /api/system/permissions/:id | id | 删除权限 |

---

## 52. 术语表 (glossary)

### 页面: GlossaryList.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getGlossaryList | GET | /api/i18n/glossary | - | 获取术语列表 |
| createGlossary | POST | /api/i18n/glossary | data | 创建术语 |
| updateGlossary | PUT | /api/i18n/glossary/:id | id, data | 更新术语 |
| deleteGlossary | DELETE | /api/i18n/glossary/:id | id | 删除术语 |

---

## 53. 多语言监控 (i18nStats)

### 页面: I18nStats.vue
| API 方法 | HTTP 方法 | API 路径 | 参数 | 说明 |
|---------|----------|---------|------|------|
| getI18nStats | GET | /api/i18n/stats | - | 获取多语言统计 |
| getI18nProgress | GET | /api/i18n/progress | - | 获取翻译进度 |

---

## 已发现的问题

### 1. email.js - 缺失方法 ✅ 已修复
- `getEmailTrace` - 缺失
- `deleteEmailList` - 缺失

### 2. email.js - sendEmail 路径错误 ✅ 已修复
- 原路径: `/api/email/list` (POST)
- 正确路径: `/api/email/send` (POST)

### 3. 钉钉应用 API - 数据库表缺失
- 错误: `relation "dingtalk_app_accounts" does not exist`
- 需要运行数据库迁移

### 4. WhatsApp Cloud API - 数据库表缺失
- 错误: `relation "whatsapp_cloud_accounts" does not exist`
- 需要运行数据库迁移

---

## 测试结果汇总

| 模块 | 状态 | 备注 |
|------|------|------|
| 邮件管理 | ✅ 正常 | GET/POST/DELETE 均正常 |
| Telegram | ✅ 正常 | 账号 CRUD 正常 |
| WhatsApp | ✅ 正常 | 账号/草稿/任务正常 |
| 线索管理 | ✅ 正常 | 列表/统计/导入正常 |
| 系统管理 | ✅ 正常 | 配置读写正常 |
| 域名池 | ✅ 正常 | CRUD 正常 |
| 短链 | ✅ 正常 | CRUD 正常 |
| 抖音卡片 | ✅ 正常 | CRUD 正常 |
| 小红书卡片 | ✅ 正常 | CRUD 正常 |
| 短信 | ✅ 正常 | 列表正常 |
| 活码 | ✅ 正常 | 列表正常 |
| 集成 | ✅ 正常 | 列表正常 |
| 客户360 | ✅ 正常 | 列表正常 |
| 批量操作 | ✅ 正常 | 工具列表正常 |
| 营销流程 | ✅ 正常 | 列表正常 |
| 自定义报表 | ✅ 正常 | 列表正常 |
| 数据大屏 | ✅ 正常 | 列表正常 |
| A/B测试 | ✅ 正常 | 列表正常 |
| 流失预警 | ✅ 正常 | 列表正常 |
| 社群 | ✅ 正常 | 列表正常 |
| 用户分层 | ✅ 正常 | RFM规则正常 |
| 意图识别 | ✅ 正常 | 统计/字典正常 |
| 对话记忆 | ✅ 正常 | 列表正常 |
| SOP智能体 | ✅ 正常 | 列表正常 |
| 触达管道 | ✅ 正常 | 列表正常 |
| 统一收件箱 | ✅ 正常 | 列表/统计正常 |
| 企微账号 | ✅ 正常 | 列表正常 |
| LLM路由 | ✅ 正常 | 健康检查正常 |
| 钉钉应用 | ❌ 数据库缺失 | 需要迁移 |
| WhatsApp Cloud | ❌ 数据库缺失 | 需要迁移 |
