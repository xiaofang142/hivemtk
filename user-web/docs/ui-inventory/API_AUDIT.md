# User-Web API 审计清单

## 项目概述
- 技术栈：Vue 3 + Element Plus + Vue Router + Pinia + Axios
- API 基础路径：`/api/`
- 审计时间：2026-07-30

## 页面与 API 清单

### 1. 邮件模块 (email)
**路由路径：**
- `/email` - 邮件列表
- `/email/drafts` - 草稿箱
- `/email/jobs` - 任务列表
- `/email/smtp` - 邮件账号
- `/email/info` - 邮件代理
- `/email/guide` - 使用引导

**API 列表：**
1. `GET /api/email/smtp` - 获取有效账号列表
2. `POST /api/email/smtp` - 添加账号
3. `PUT /api/email/smtp/:id` - 更新账号
4. `DELETE /api/email/smtp/:id` - 删除账号
5. `GET /api/email/drafts` - 获取草稿列表
6. `GET /api/email/drafts/:id` - 获取单个草稿详情
7. `POST /api/email/drafts` - 创建草稿
8. `PUT /api/email/drafts/:id` - 更新草稿
9. `DELETE /api/email/drafts/:id` - 删除草稿
10. `POST /api/upload` - 上传图片
11. `POST /api/email/list` - 发送邮件
12. `GET /api/email/list` - 获取邮件列表
13. `GET /api/email/jobs` - 获取任务列表
14. `DELETE /api/email/jobs/:id` - 删除任务
15. `GET /api/email/jobs/:id` - 获取任务详情

**审计状态：** 待测试

---

### 2. Telegram 模块 (telegram)
**路由路径：**
- `/telegram` - TG 机器人
- `/telegram/account` - 机器人账号

**API 列表：**
1. `GET /api/telegram/accounts` - 列出所有 Bot 账号
2. `GET /api/telegram/accounts/:id` - 获取账号详情
3. `POST /api/telegram/accounts` - 创建 Bot 账号
4. `PUT /api/telegram/accounts/:id` - 更新 Bot 账号
5. `DELETE /api/telegram/accounts/:id` - 删除 Bot 账号
6. `POST /api/telegram/accounts/:id/register-webhook` - 注册 Webhook
7. `POST /api/telegram/accounts/:id/test-send` - 测试发送消息

**审计状态：** 待测试

---

### 3. WhatsApp 模块 (whatsapp)
**路由路径：**
- `/whatsapp` - WhatsApp 社群
- `/whatsapp/account` - 账号管理
- `/whatsapp/drafts` - 草稿箱
- `/whatsapp/jobs` - 群发
- `/whatsapp/lead-group-selection` - 从线索库选择群体
- `/whatsapp/group-messaging` - 批量消息发送

**API 列表：**
1. `GET /api/whatsapp/accounts` - 账号管理列表
2. `POST /api/whatsapp/accounts` - 创建账号
3. `POST /api/whatsapp/accounts/:id/login/start` - 开始登录
4. `GET /api/whatsapp/accounts/:id/login/status` - 登录状态
5. `PUT /api/whatsapp/accounts/:id` - 更新账号
6. `DELETE /api/whatsapp/accounts/:id` - 删除账号
7. `GET /api/whatsapp/drafts` - 草稿列表
8. `POST /api/whatsapp/drafts` - 创建草稿
9. `PUT /api/whatsapp/drafts/:id` - 更新草稿
10. `DELETE /api/whatsapp/drafts/:id` - 删除草稿
11. `GET /api/whatsapp/jobs` - 群发任务列表
12. `POST /api/whatsapp/jobs` - 创建群发任务
13. `GET /api/whatsapp/jobs/:id` - 获取任务详情
14. `DELETE /api/whatsapp/jobs/:id` - 删除任务

**审计状态：** 待测试

---

### 4. 线索模块 (clue)
**路由路径：**
- `/clue/list` - 线索列表
- `/clue/statistics` - 线索统计

**API 列表：**
1. `DELETE /api/clues/delete/:id` - 删除线索
2. `GET /api/clues/list` - 线索列表
3. `GET /api/clues/statistics` - 线索统计
4. `POST /api/clues/import` - 导入线索

**审计状态：** 待测试

---

### 5. 系统模块 (system)
**路由路径：**
- `/system/config` - 站点设置
- `/system/obs-config` - 存储配置
- `/system/material-library` - 素材库
- `/system/monitor` - 监控
- `/system/guide` - 使用引导
- `/system/rag-overview` - RAG概览

**API 列表：**
1. `GET /api/system/config` - 获取站点配置
2. `POST /api/system/config` - 保存站点配置
3. `GET /api/obs/config` - 获取OBS配置列表
4. `GET /api/obs/config/:id` - 获取OBS配置详情
5. `POST /api/obs/config` - 创建OBS配置
6. `PUT /api/obs/config/:id` - 更新OBS配置
7. `DELETE /api/obs/config/:id` - 删除OBS配置
8. `POST /api/obs/config/:id/test` - 测试OBS连接
9. `POST /api/obs/config/:id/default` - 设置默认OBS配置
10. `GET /api/obs/config/default` - 获取默认OBS配置

**审计状态：** 待测试

---

### 6. 域名池模块 (domainPool)
**路由路径：**
- `/domainPool` - 域名池管理

**API 列表：**
1. `GET /api/domainpool/list` - 获取域名池列表
2. `GET /api/domainpool/:id` - 根据ID获取域名池详情
3. `POST /api/domainpool/create` - 创建域名池
4. `PUT /api/domainpool/update` - 更新域名池
5. `DELETE /api/domainpool/delete/:id` - 删除域名池
6. `POST /api/domainpool/check/:id` - 检查单个域名
7. `POST /api/domainpool/checkall` - 检查所有域名

**审计状态：** 待测试

---

### 7. 短链模块 (shortLink)
**路由路径：**
- `/shortLink` - 短链管理
- `/shortLink/stats` - 短链统计

**API 列表：**
1. `GET /api/shortlink/list` - 获取短链列表
2. `GET /api/shortlink/:id` - 获取短链详情
3. `POST /api/shortlink/create` - 创建短链
4. `PUT /api/shortlink/update` - 更新短链
5. `DELETE /api/shortlink/delete/:id` - 删除短链
6. `POST /api/shortlink/access` - 访问短链
7. `POST /api/shortlink/generate` - 生成短码
8. `GET /api/shortlink/:id/stats` - 获取短链统计
9. `GET /api/shortlink/stats/all` - 获取所有短链统计
10. `POST /api/shortlink/:id/share` - 分享短链

**审计状态：** 待测试

---

### 8. 抖音卡片模块 (douyinCard)
**路由路径：**
- `/douyinCard` - 抖音卡片
- `/douyin/auto-reply` - 抖音自动回复
- `/douyin/stats` - 抖音卡片统计
- `/douyin-card-stats/:id` - 抖音卡片详情统计

**API 列表：**
1. `GET /api/douyin/list` - 获取抖音卡片列表
2. `GET /api/douyin/:id` - 获取单个抖音卡片
3. `POST /api/douyin/create` - 创建抖音卡片
4. `PUT /api/douyin/update` - 更新抖音卡片
5. `DELETE /api/douyin/delete/:id` - 删除抖音卡片
6. `GET /api/douyin/view/:id` - 访问抖音卡片
7. `GET /api/douyin/stats/card/:id` - 获取抖音卡片统计数据
8. `GET /api/douyin/stats/overall` - 获取抖音卡片总体统计数据
9. `POST /api/douyin/:id/generate-short-link` - 生成抖音卡片短链接
10. `GET /api/auto-reply/accounts` - 获取自动回复账号列表
11. `POST /api/auto-reply/accounts` - 创建自动回复账号
12. `DELETE /api/auto-reply/accounts/:id` - 删除自动回复账号
13. `POST /api/auto-reply/start-login` - 开始登录
14. `GET /api/auto-reply/login-status` - 登录状态
15. `GET /api/auto-reply/rule` - 获取自动回复规则
16. `POST /api/auto-reply/rule` - 保存自动回复规则
17. `GET /api/auto-reply/logs` - 获取自动回复日志
18. `POST /api/auto-reply/start` - 启动自动回复
19. `POST /api/auto-reply/stop` - 停止自动回复

**审计状态：** 待测试

---

### 9. 小红书卡片模块 (xiaohongshuCard)
**路由路径：**
- `/xiaohongshuCard` - 小红书卡片
- `/xiaohongshu/auto-reply` - 小红书自动回复
- `/xiaohongshu/stats` - 小红书卡片统计
- `/xiaohongshu-card-stats/:id` - 小红书卡片详情统计

**API 列表：**
1. `GET /api/xiaohongshu/list` - 获取小红书卡片列表
2. `GET /api/xiaohongshu/:id` - 获取小红书卡片详情
3. `POST /api/xiaohongshu/create` - 创建小红书卡片
4. `PUT /api/xiaohongshu/update` - 更新小红书卡片
5. `DELETE /api/xiaohongshu/delete/:id` - 删除小红书卡片
6. `GET /api/xiaohongshu/view/:id` - 访问小红书卡片
7. `GET /api/xiaohongshu/stats/card/:id` - 获取小红书卡片统计数据
8. `GET /api/xiaohongshu/stats/overall` - 获取小红书卡片总体统计数据
9. `POST /api/xiaohongshu/:id/generate-short-link` - 生成小红书卡片短链接

**审计状态：** 待测试

---

### 10. 快手卡片模块 (kuaishouCard)
**路由路径：**
- `/kuaishouCard` - 快手卡片
- `/kuaishou/auto-reply` - 快手自动回复
- `/kuaishou/stats` - 快手卡片统计
- `/kuaishou-card-stats/:id` - 快手卡片详情统计

**API 列表：**
1. `GET /api/kuaishou/list` - 获取快手卡片列表
2. `GET /api/kuaishou/:id` - 获取单个快手卡片
3. `POST /api/kuaishou/create` - 创建快手卡片
4. `PUT /api/kuaishou/update` - 更新快手卡片
5. `DELETE /api/kuaishou/delete/:id` - 删除快手卡片
6. `GET /api/kuaishou/view/:id` - 访问快手卡片
7. `POST /api/kuaishou/like/:id` - 点赞快手卡片
8. `POST /api/kuaishou/share/:id` - 分享快手卡片
9. `GET /api/kuaishou/stats/card/:id` - 获取快手卡片统计数据
10. `GET /api/kuaishou/stats/overall` - 获取快手卡片总体统计数据
11. `POST /api/kuaishou/:id/generate-short-link` - 生成快手卡片短链接

**审计状态：** 待测试

---

### 11. 闲鱼卡片模块 (xianyuCard)
**路由路径：**
- `/xianyuCard` - 闲鱼卡片
- `/xianyu/auto-reply` - 闲鱼自动回复
- `/xianyu/stats` - 闲鱼卡片统计
- `/xianyu-card-stats/:id` - 闲鱼卡片详情统计

**API 列表：**
1. `GET /api/xianyu/list` - 获取闲鱼卡片列表
2. `GET /api/xianyu/:id` - 获取单个闲鱼卡片
3. `POST /api/xianyu/create` - 创建闲鱼卡片
4. `PUT /api/xianyu/update` - 更新闲鱼卡片
5. `DELETE /api/xianyu/delete/:id` - 删除闲鱼卡片
6. `GET /api/xianyu/view/:id` - 访问闲鱼卡片
7. `GET /api/xianyu/stats/card/:id` - 获取闲鱼卡片统计数据
8. `GET /api/xianyu/stats/overall` - 获取闲鱼卡片总体统计数据
9. `POST /api/xianyu/:id/generate-short-link` - 生成闲鱼卡片短链接

**审计状态：** 待测试

---

### 12. 短信模块 (sms)
**路由路径：**
- `/sms/list` - 短信列表
- `/sms/drafts` - 短信草稿
- `/sms/jobs` - 短信任务
- `/sms/config` - 短信配置

**API 列表：**
1. `GET /api/sms/config` - 获取短信配置
2. `POST /api/sms/config` - 保存短信配置
3. `GET /api/sms/list` - 获取短信列表
4. `GET /api/sms/detail/:id` - 获取短信详情
5. `POST /api/sms/resend/:id` - 重新发送短信
6. `POST /api/sms/send` - 发送短信
7. `GET /api/sms/draft/list` - 获取草稿列表
8. `GET /api/sms/draft/:id` - 获取草稿详情
9. `POST /api/sms/draft` - 创建草稿
10. `PUT /api/sms/draft/:id` - 更新草稿
11. `DELETE /api/sms/draft/:id` - 删除草稿
12. `POST /api/sms/draft/:id/send` - 发送草稿
13. `GET /api/sms/job/list` - 获取任务列表
14. `GET /api/sms/job/:id` - 获取任务详情
15. `POST /api/sms/job/:id/pause` - 暂停任务
16. `POST /api/sms/job/:id/resume` - 恢复任务
17. `POST /api/sms/job/:id/stop` - 停止任务
18. `POST /api/sms/job` - 创建任务
19. `DELETE /api/sms/job/:id` - 删除任务
20. `GET /api/sms/job/:id/records` - 获取任务记录

**审计状态：** 待测试

---

### 13. 活码模块 (livecode)
**路由路径：**
- `/livecode` - 活码管理

**API 列表：**
1. `GET /api/live-codes/list` - 获取活码列表
2. `GET /api/live-codes/:id` - 获取活码详情
3. `POST /api/live-codes/create` - 创建活码
4. `PUT /api/live-codes/:id/update` - 更新活码
5. `DELETE /api/live-codes/:id/delete` - 删除活码
6. `GET /api/live-codes/:id/stats` - 获取活码统计
7. `GET /api/live-codes/:liveCodeId/qrcodes` - 获取活码二维码列表
8. `POST /api/live-codes/:liveCodeId/qrcodes/create` - 生成活码二维码
9. `GET /api/live-codes/qrcodes/:qrId/stats` - 获取活码二维码统计
10. `POST /api/live-codes/:id/share` - 分享活码
11. `DELETE /api/live-codes/qrcodes/:id/delete` - 删除活码二维码
12. `PUT /api/live-codes/qrcodes/:id/update` - 更新活码二维码

**审计状态：** 待测试

---

### 14. TikTok 模块 (tiktok)
**路由路径：**
- `/tiktok/list` - TikTok卡片
- `/tiktok/stats` - TikTok统计
- `/tiktok/card-stats/:id` - TikTok卡片统计
- `/tiktok/auto-reply` - TikTok自动回复

**API 列表：**
1. `GET /api/tiktok-card/list` - 获取TikTok卡片列表
2. `GET /api/tiktok-card/:id` - 获取TikTok卡片详情
3. `POST /api/tiktok-card` - 创建TikTok卡片
4. `PUT /api/tiktok-card/:id` - 更新TikTok卡片
5. `DELETE /api/tiktok-card/:id` - 删除TikTok卡片
6. `POST /api/tiktok-card/generate-short-link` - 生成短链接
7. `GET /api/tiktok-card/stats/overall` - 获取总体统计信息
8. `GET /api/tiktok-card/:cardId/stats` - 获取单个卡片统计信息
9. `GET /api/tiktok/auto-reply/accounts` - 获取TikTok自动回复账号列表
10. `GET /api/tiktok/auto-reply/rule` - 获取TikTok自动回复规则
11. `POST /api/tiktok/auto-reply/rule` - 保存TikTok自动回复规则
12. `POST /api/tiktok/auto-reply/accounts` - 更新或创建TikTok账号
13. `DELETE /api/tiktok/auto-reply/accounts/:accountId` - 删除TikTok账号
14. `GET /api/tiktok/auto-reply/logs` - 获取TikTok自动回复日志
15. `POST /api/tiktok/auto-reply/start` - 启动TikTok自动回复
16. `POST /api/tiktok/auto-reply/stop` - 停止TikTok自动回复

**审计状态：** 待测试

---

### 15. A/B 实验模块 (abExperiment)
**路由路径：**
- `/abExperiment/list` - A/B 实验

**API 列表：**
1. `GET /api/ab-experiments` - 获取实验列表
2. `GET /api/ab-experiments/:id` - 获取实验详情
3. `POST /api/ab-experiments` - 创建实验
4. `PUT /api/ab-experiments/:id` - 更新实验
5. `DELETE /api/ab-experiments/:id` - 删除实验
6. `POST /api/ab-experiments/:id/start` - 启动实验
7. `POST /api/ab-experiments/:id/pause` - 暂停实验
8. `POST /api/ab-experiments/:id/stop` - 停止实验
9. `GET /api/ab-experiments/:id/results` - 获取实验结果
10. `GET /api/ab-experiments/:id/conversion-events` - 获取转化事件

**审计状态：** 待测试

---

### 16. 批量操作模块 (batchOperation)
**路由路径：**
- `/batchOperation/list` - 批量操作

**API 列表：**
1. `POST /api/batch/import` - 批量导入文件
2. `GET /api/batch/template` - 下载批量模板
3. `POST /api/batch/export` - 批量导出
4. `POST /api/batch/delete` - 批量删除
5. `POST /api/batch/update` - 批量更新
6. `GET /api/batch/tools` - 获取批量工具
7. `GET /api/batch/histories` - 获取批量历史
8. `POST /api/batch/histories/:id/cancel` - 取消批量任务
9. `GET /api/batch/histories/:id` - 获取批量详情
10. `POST /api/batch/preview` - 预览批量

**审计状态：** 待测试

---

### 17. 流失预警模块 (churnPrediction)
**路由路径：**
- `/churnPrediction/list` - 流失预警

**API 列表：**
1. `GET /api/churn/prediction` - 获取流失预测
2. `GET /api/churn-prediction` - 获取流失预测列表
3. `GET /api/churn-prediction/users` - 获取高风险用户
4. `GET /api/churn-prediction/warnings` - 获取流失预警
5. `GET /api/churn-prediction/unhandled-warnings` - 获取未处理预警
6. `POST /api/churn/warnings/:id/handle` - 标记预警已处理
7. `GET /api/churn-prediction/model-config` - 获取流失模型配置
8. `POST /api/churn-prediction/model-config` - 保存流失模型配置
9. `GET /api/churn-prediction/statistics` - 获取流失统计
10. `GET /api/churn-prediction/risk-distribution` - 获取风险分布
11. `POST /api/user-segment/rfm/calculate` - 计算RFM

**审计状态：** 待测试

---

### 18. 自定义报表模块 (customReport)
**路由路径：**
- `/customReport/list` - 自定义报表

**API 列表：**
1. `GET /api/custom-reports` - 获取报表列表
2. `GET /api/custom-reports/:id` - 获取报表详情
3. `POST /api/custom-reports` - 创建报表
4. `PUT /api/custom-reports/:id` - 更新报表
5. `DELETE /api/custom-reports/:id` - 删除报表
6. `GET /api/custom-reports/templates` - 获取公开模板
7. `POST /api/custom-reports/templates/:id/use` - 使用报表模板
8. `GET /api/custom-reports/:id/data` - 查询报表数据

**审计状态：** 待测试

---

### 19. 客户360模块 (customer360)
**路由路径：**
- `/customer360/list` - 客户 360

**API 列表：**
1. `GET /api/customer/list` - 获取客户列表
2. `GET /api/customer/360/:id` - 获取客户360详情
3. `POST /api/customer/:id/tags` - 添加客户标签
4. `DELETE /api/customer/:id/tags/:tag` - 删除客户标签
5. `PUT /api/customer/:id` - 更新客户
6. `GET /api/customer/:id` - 获取客户详情
7. `GET /api/customer/:id/behaviors` - 获取客户行为
8. `GET /api/customer/:id/communications` - 获取客户沟通记录

**审计状态：** 待测试

---

### 20. 客户事件模块 (customerEvent)
**路由路径：**
- `/customerEvent/list` - 客户事件

**API 列表：**
1. `POST /api/events/track` - 追踪事件
2. `POST /api/events/pageview` - 追踪页面浏览
3. `POST /api/events/click` - 追踪点击
4. `POST /api/events/purchase` - 追踪购买
5. `POST /api/events/signup` - 追踪注册
6. `POST /api/events/login` - 追踪登录
7. `POST /api/events/add-to-cart` - 追踪加入购物车
8. `GET /api/events/customer/:customerId` - 获取客户事件历史
9. `GET /api/events/stats` - 获取事件统计
10. `DELETE /api/events/customer/:id` - 删除事件

**审计状态：** 待测试

---

### 21. 客服会话模块 (customerSession)
**路由路径：**
- `/customerSession/list` - 客服会话

**API 列表：**
1. `GET /api/customer-sessions` - 获取会话列表
2. `GET /api/customer-sessions/:id/messages` - 获取会话消息
3. `POST /api/customer-sessions/:id/messages` - 发送消息
4. `POST /api/customer-sessions` - 创建会话
5. `POST /api/customer-sessions/:id/close` - 关闭会话
6. `POST /api/customer-sessions/:id/transfer` - 转接会话
7. `POST /api/customer-sessions/:id/takeover` - 坐席接管会话
8. `POST /api/customer-sessions/:id/release` - 释放会话回AI
9. `POST /api/customer-sessions/:id/switch-handler` - 统一AI/人工切换
10. `POST /api/customer-sessions/:id/blacklist` - 拉黑访客
11. `POST /api/customer-sessions/blacklist/remove` - 解除拉黑
12. `GET /api/customer-sessions/blacklist/check` - 查询黑名单
13. `GET /api/customer-sessions/blacklist` - 黑名单分页列表
14. `GET /api/customer-360` - 获取客户360画像
15. `GET /api/customer-360/basic` - 获取客户基本信息
16. `GET /api/customer-360/stats` - 获取客户统计
17. `GET /api/customer-360/sessions` - 获取客户会话
18. `GET /api/customer-360/tags` - 获取客户标签

**审计状态：** 待测试

---

### 22. OneID 模块 (oneid)
**路由路径：**
- `/oneid/list` - OneID 列表
- `/oneid/conflicts` - 身份冲突解决

**API 列表：**
1. `GET /api/customer/oneid/list` - 获取OneID客户列表
2. `GET /api/customer/oneid/conflicts` - 获取身份冲突列表
3. `POST /api/customer/oneid/merge` - 合并两个客户
4. `POST /api/customer/oneid/conflicts/:id/resolve` - 解决冲突
5. `GET /api/customer-oneid/:customerId/identities` - 获取客户身份映射详情
6. `POST /api/customer-oneid/:customerId/identities` - 链接新身份
7. `POST /api/customer/oneid/resolve` - 识别或创建（OneID解析）
8. `GET /api/customer/oneid/stats` - 获取OneID统计

**审计状态：** 待测试

---

### 23. 数据大屏模块 (dashboardScreen)
**路由路径：**
- `/dashboardScreen/list` - 数据大屏

**API 列表：**
1. `GET /api/dashboards` - 获取大屏列表
2. `GET /api/dashboards/:id` - 获取大屏详情
3. `POST /api/dashboards` - 创建大屏
4. `PUT /api/dashboards/:id` - 更新大屏
5. `DELETE /api/dashboards/:id` - 删除大屏
6. `GET /api/dashboards/public/:code` - 公开查看大屏
7. `GET /api/dashboards/data` - 获取大屏数据
8. `GET /api/dashboards/activities` - 获取实时活动

**审计状态：** 待测试

---

### 24. 第三方对接模块 (integration)
**路由路径：**
- `/integration/list` - 第三方对接

**API 列表：**
1. `GET /api/integrations` - 获取集成账号列表
2. `GET /api/integrations/:id` - 获取集成账号详情
3. `POST /api/integrations` - 创建集成账号
4. `PUT /api/integrations/:id` - 更新集成账号
5. `DELETE /api/integrations/:id` - 删除集成账号
6. `POST /api/integrations/:id/sync-customers` - 同步客户
7. `POST /api/integrations/:id/sync-orders` - 同步订单
8. `POST /api/integrations/:id/sync-products` - 同步产品
9. `GET /api/integration/sync-logs` - 获取同步日志
10. `GET /api/integration/external-customers` - 获取外部客户
11. `GET /api/integration/external-orders` - 获取外部订单
12. `GET /api/integration/external-orders-by-customer` - 按客户获取外部订单
13. `GET /api/integration/external-products` - 获取外部产品
14. `POST /api/integrations/:id/test` - 测试集成

**审计状态：** 待测试

---

### 25. 营销自动化模块 (marketingFlow)
**路由路径：**
- `/marketingFlow/list` - 营销自动化

**API 列表：**
1. `GET /api/marketing-flows` - 获取营销流程列表
2. `GET /api/marketing-flows/:id` - 获取营销流程详情
3. `POST /api/marketing-flows` - 创建营销流程
4. `PUT /api/marketing-flows/:id` - 更新营销流程
5. `DELETE /api/marketing-flows/:id` - 删除营销流程
6. `POST /api/marketing-flows/:id/activate` - 激活流程
7. `POST /api/marketing-flows/:id/pause` - 暂停流程
8. `POST /api/marketing-flows/:id/stop` - 停止流程
9. `GET /api/marketing-flows/:id/executions` - 获取流程执行记录
10. `GET /api/marketing-flows/:id/stats` - 获取流程统计

**审计状态：** 待测试

---

### 26. 操作日志模块 (operationLog)
**路由路径：**
- `/operationLog/list` - 操作日志

**API 列表：**
1. `GET /api/team/logs` - 获取操作日志列表
2. `GET /api/team/logs/:id` - 获取操作日志详情
3. `GET /api/team/logs/export` - 导出操作日志
4. `DELETE /api/team/logs` - 删除操作日志
5. `POST /api/team/logs/clean` - 清理操作日志

**审计状态：** 待测试

---

### 27. RAG 产品配置模块 (ragProductConfig)
**路由路径：**
- `/system/rag-product-config` - RAG 主配置
- `/system/rag-account` - RAG 账号配置
- `/system/rag-product` - RAG 产品管理

**API 列表：**
1. `POST /api/rag-config/products` - 创建RAG产品
2. `GET /api/rag-config/products` - 获取RAG产品列表
3. `PUT /api/rag-config/products/:id` - 更新RAG产品
4. `DELETE /api/rag-config/products/:id` - 删除RAG产品
5. `GET /api/rag-config/accounts/config` - 获取账号配置
6. `PUT /api/rag-config/accounts/config` - 更新账号配置
7. `POST /api/rag-config/process-message` - 处理消息

**审计状态：** 待测试

---

### 28. 话术库模块 (scriptTemplate)
**路由路径：**
- `/scriptTemplate/list` - 话术库

**API 列表：**
1. `GET /api/scripts` - 获取话术模板列表
2. `GET /api/scripts/:id` - 获取话术模板详情
3. `POST /api/scripts` - 创建话术模板
4. `PUT /api/scripts/:id` - 更新话术模板
5. `DELETE /api/scripts/:id` - 删除话术模板
6. `GET /api/scripts/categories` - 获取话术分类
7. `GET /api/scripts/search` - 搜索话术模板
8. `GET /api/scripts/public` - 获取公开话术模板
9. `POST /api/scripts/recommend` - 推荐话术

**审计状态：** 待测试

---

### 29. 用户分层 RFM 模块 (userSegment)
**路由路径：**
- `/userSegment/list` - 用户分层 RFM

**API 列表：**
1. `GET /api/user-segment/rfm/list` - 获取RFM列表
2. `GET /api/user-segment/rfm/rule` - 获取RFM规则
3. `POST /api/user-segment/rfm/rule` - 保存RFM规则
4. `PUT /api/user-segment/rfm/rule/:id` - 更新RFM规则
5. `GET /api/user-segment/rfm/user` - 获取用户RFM
6. `GET /api/user-segment/rfm/stats` - 获取RFM统计
7. `POST /api/user-segment/rfm/calculate` - 计算RFM
8. `GET /api/user-segment/layers` - 获取分层描述
9. `DELETE /api/user-segment/rfm/rule/:id` - 删除RFM规则

**审计状态：** 待测试

---

### 30. 社群管理模块 (community)
**路由路径：**
- `/community/list` - 社群管理

**API 列表：**
1. `GET /api/community/groups` - 获取社群分组列表
2. `POST /api/community/groups` - 创建社群分组
3. `GET /api/community/groups/:id` - 获取社群分组详情
4. `PUT /api/community/groups/:id` - 更新社群分组
5. `DELETE /api/community/groups/:id` - 删除社群分组
6. `GET /api/community/members` - 获取社群成员列表
7. `GET /api/community/messages` - 获取社群消息列表
8. `GET /api/community/stats` - 获取社群统计
9. `POST /api/community/import` - 导入社群数据
10. `POST /api/community/export` - 导出社群数据

**审计状态：** 待测试

---

### 31. 统一消息模块 (unifiedMessage)
**路由路径：**
- `/unifiedMessage/list` - 统一消息

**API 列表：**
1. `GET /api/unified-messages` - 获取消息列表
2. `GET /api/unified-messages/:id` - 获取消息详情
3. `GET /api/unified-messages/:id/replies` - 获取消息回复列表

**审计状态：** 待测试

---

### 32. 平台账号模块 (platformAccount)
**路由路径：**
- `/platformAccount/list` - 平台账号

**API 列表：**
1. `GET /api/platform-accounts` - 获取平台账号列表
2. `GET /api/platform-accounts/platforms` - 获取支持的平台列表
3. `GET /api/platform-accounts/:id` - 获取平台账号详情
4. `POST /api/platform-accounts` - 创建平台账号
5. `PUT /api/platform-accounts/:id` - 更新平台账号
6. `DELETE /api/platform-accounts/:id` - 删除平台账号
7. `POST /api/platform-accounts/:id/login` - 登录平台账号
8. `GET /api/platform-accounts/:id/status` - 检查平台账号状态

**审计状态：** 待测试

---

### 33. 消息中台模块 (messageHub)
**路由路径：**
- `/messageHub/list` - 消息中台

**API 列表：**
1. `POST /api/message-hub/push` - 推送消息
2. `POST /api/message-hub/push-batch` - 批量推送
3. `POST /api/message-hub/push-from-channel` - 从渠道原始消息推送
4. `GET /api/message-hub/list` - 消息列表
5. `GET /api/message-hub/:id` - 消息详情
6. `POST /api/message-hub/:id/read` - 标记已读
7. `GET /api/message-hub/stats` - 统计
8. `GET /api/message-hub/platforms` - 支持的平台与消息类型

**审计状态：** 待测试

---

### 34. 意图识别模块 (intentRecognition)
**路由路径：**
- `/intentRecognition/list` - 意图识别

**API 列表：**
1. `POST /api/intent/recognize` - 单条识别
2. `POST /api/intent/recognize/batch` - 批量识别
3. `GET /api/intent/stats` - 意图统计
4. `GET /api/intent/recent` - 最近意图
5. `GET /api/intent/dict` - 意图词典
6. `POST /api/intent/recognize/fine` - 精细识别
7. `GET /api/intent/logs` - 精细识别日志
8. `GET /api/intent/stats/fine` - 精细识别统计
9. `GET /api/intent/config` - 获取意图识别配置
10. `PUT /api/intent/config` - 更新意图识别配置

**审计状态：** 待测试

---

### 35. 对话记忆模块 (dialogueMemory)
**路由路径：**
- `/dialogueMemory/list` - 对话记忆

**API 列表：**
1. `POST /api/memory/messages` - 追加消息
2. `GET /api/memory/short` - 获取短期记忆
3. `GET /api/memory/long` - 获取长期记忆
4. `POST /api/memory/facts` - 更新关键事实
5. `POST /api/memory/objections` - 记录异议
6. `POST /api/memory/purchase-intent` - 更新购买意向
7. `POST /api/memory/intent-trail` - 记录意图轨迹
8. `POST /api/memory/sop-history` - 记录 SOP 历史
9. `GET /api/memory/context` - 构建上下文
10. `GET /api/memory/list` - 客户记忆列表

**审计状态：** 待测试

---

### 36. 销冠 SOP 智能体模块 (sopAgent)
**路由路径：**
- `/sopAgent/list` - 销冠 SOP 智能体

**API 列表：**
1. `GET /api/sop` - 获取SOP列表
2. `POST /api/sop` - 创建SOP
3. `GET /api/sop/stats` - 获取统计
4. `GET /api/sop/match` - 按意图匹配
5. `GET /api/sop/executions` - 获取执行列表
6. `GET /api/sop/executions/:id` - 获取执行详情
7. `POST /api/sop/executions/:id/pause` - 暂停执行
8. `POST /api/sop/executions/:id/resume` - 恢复执行
9. `POST /api/sop/executions/:id/cancel` - 取消执行
10. `GET /api/sop/:id` - 获取SOP详情
11. `PUT /api/sop/:id` - 更新SOP
12. `DELETE /api/sop/:id` - 删除SOP
13. `POST /api/sop/:id/activate` - 激活SOP
14. `POST /api/sop/:id/deactivate` - 停用SOP
15. `POST /api/sop/execute` - 执行SOP
16. `POST /api/sop/step` - 单步推进

**审计状态：** 待测试

---

### 37. 触达 Pipeline 模块 (reachPipeline)
**路由路径：**
- `/reachPipeline/list` - 触达Pipeline

**API 列表：**
1. `GET /api/reach/pipelines` - 获取Pipeline列表
2. `POST /api/reach/pipelines` - 创建Pipeline
3. `GET /api/reach/pipelines/:id` - 获取Pipeline详情
4. `PUT /api/reach/pipelines/:id` - 更新Pipeline
5. `DELETE /api/reach/pipelines/:id` - 删除Pipeline
6. `POST /api/reach/pipelines/:id/pause` - 暂停Pipeline
7. `POST /api/reach/pipelines/:id/resume` - 恢复Pipeline
8. `POST /api/reach/pipelines/:id/archive` - 归档Pipeline
9. `GET /api/reach/jobs` - 获取任务列表
10. `POST /api/reach/jobs` - 入队任务
11. `GET /api/reach/jobs/:id` - 获取任务详情
12. `POST /api/reach/jobs/:id/cancel` - 取消任务
13. `POST /api/reach/jobs/:id/retry` - 重试任务
14. `POST /api/reach/jobs/:id/execute` - 执行任务
15. `GET /api/reach/stats` - 获取统计
16. `POST /api/reach/rate-limit/reset` - 重置限流

**审计状态：** 待测试

---

### 38. 统一收件箱模块 (unifiedInbox)
**路由路径：**
- `/unifiedInbox/list` - 统一收件箱

**API 列表：**
1. `GET /api/inbox` - 获取会话列表
2. `GET /api/inbox/stats` - 获取收件箱统计
3. `GET /api/inbox/assignments` - 获取分配历史列表
4. `POST /api/inbox/assign` - 手动分配
5. `POST /api/inbox/auto-assign` - 自动分配
6. `GET /api/inbox/staff/:staff/load` - 获取客服当前负载
7. `GET /api/inbox/:id` - 获取会话详情
8. `POST /api/inbox/:id/read` - 标记已读
9. `POST /api/inbox/:id/pin` - 置顶/取消置顶
10. `POST /api/inbox/:id/star` - 星标/取消星标
11. `POST /api/inbox/:id/mute` - 免打扰/取消免打扰
12. `POST /api/inbox/:id/tags` - 添加标签
13. `DELETE /api/inbox/:id/tags/:tag` - 移除标签
14. `GET /api/inbox/:id/messages` - 获取消息流

**审计状态：** 待测试

---

### 39. 企微账号管理模块 (wecomAccount)
**路由路径：**
- `/wecomAccount/list` - 企微账号管理
- `/wecomAccount/data` - 企微数据看板

**API 列表：**
1. `GET /api/wecom/health/accounts` - 获取账号列表（含最新健康度）
2. `GET /api/wecom/health/accounts/risks` - 获取风险账号列表
3. `GET /api/wecom/health/accounts/select` - 选择最优健康账号
4. `GET /api/wecom/health/accounts/summary` - 获取健康度概览摘要
5. `GET /api/wecom/health/accounts/:id` - 获取账号最新健康度
6. `GET /api/wecom/health/accounts/:id/history` - 获取健康度历史
7. `POST /api/wecom/health/accounts/:id` - 上报健康度
8. `POST /api/wecom/health/accounts/:id/status` - 更新账号状态
9. `POST /api/wecom/health/accounts/:id/quota/consume` - 消耗配额
10. `POST /api/wecom/health/accounts/quota/reset` - 重置日配额
11. `POST /api/wecom/messages/ingest` - 接入企微消息
12. `POST /api/wecom/messages/send` - 发送企微消息
13. `POST /api/wecom/accounts` - 创建企微账号
14. `PUT /api/wecom/accounts/:id` - 编辑企微账号
15. `DELETE /api/wecom/accounts/:id` - 删除企微账号
16. `GET /api/wecom/customers` - 获取企微客户列表
17. `GET /api/wecom/groups` - 获取企微客户群列表
18. `GET /api/wecom/tags` - 获取企微标签列表
19. `GET /api/wecom/messages` - 获取企微消息列表
20. `POST /api/wecom/accounts/:id/sync-customers` - 同步客户
21. `POST /api/wecom/accounts/:id/sync-groups` - 同步客户群
22. `POST /api/wecom/accounts/:id/sync-tags` - 同步标签
23. `POST /api/wecom/accounts/:id/refresh` - 刷新登录状态
24. `POST /api/wecom/accounts/:id/send-message` - 测试发送消息

**审计状态：** 待测试

---

### 40. WhatsApp Cloud 模块 (whatsappCloud)
**路由路径：**
- `/whatsapp-cloud` - WhatsApp Cloud

**API 列表：**
1. `GET /api/whatsapp-cloud/accounts` - 获取账号列表
2. `GET /api/whatsapp-cloud/accounts/:id` - 获取账号详情
3. `POST /api/whatsapp-cloud/accounts` - 创建账号
4. `PUT /api/whatsapp-cloud/accounts/:id` - 更新账号
5. `DELETE /api/whatsapp-cloud/accounts/:id` - 删除账号
6. `POST /api/whatsapp-cloud/accounts/:id/test-send` - 测试发送

**审计状态：** 待测试

---

### 41. 钉钉应用模块 (dingtalkApp)
**路由路径：**
- `/dingtalk-app` - 钉钉应用

**API 列表：**
1. `GET /api/dingtalk-app/accounts` - 获取账号列表
2. `GET /api/dingtalk-app/accounts/:id` - 获取账号详情
3. `POST /api/dingtalk-app/accounts` - 创建账号
4. `PUT /api/dingtalk-app/accounts/:id` - 更新账号
5. `DELETE /api/dingtalk-app/accounts/:id` - 删除账号
6. `POST /api/dingtalk-app/accounts/:id/test` - 测试配置

**审计状态：** 待测试

---

### 42. LLM路由模块 (llmRouting)
**路由路径：**
- `/llmRouting/list` - LLM路由

**API 列表：**
1. `GET /api/llm/models` - 获取模型列表
2. `GET /api/llm/models/:name` - 获取模型详情
3. `POST /api/llm/models` - 新增模型
4. `PUT /api/llm/models/:name` - 更新模型
5. `DELETE /api/llm/models/:name` - 删除模型
6. `POST /api/llm/models/:name/test` - 测试模型连通性
7. `GET /api/llm/scene-routing` - 获取场景路由配置
8. `GET /api/llm/scenarios` - 获取场景列表
9. `PUT /api/llm/scene-routing` - 保存场景路由配置
10. `GET /api/llm/fallback` - 获取 Fallback 兜底配置
11. `GET /api/llm/audit` - 获取路由变更审计历史
12. `GET /api/llm/stats` - 获取进程内实时统计
13. `GET /api/llm/usage` - 获取跨进程历史统计
14. `GET /api/llm/cost-stats` - 获取成本统计
15. `GET /api/llm/health` - 获取整体健康度
16. `GET /api/llm/model-type-stats` - 获取本地/云端分类统计
17. `GET /api/llm/egress-audit` - 获取出域审计
18. `GET /api/llm/egress-alerts` - 获取出域告警

**审计状态：** 待测试

---

### 43. 标签分层模块 (tagSegmentation)
**路由路径：**
- `/tagSegmentation/list` - 标签分层

**API 列表：**
1. `GET /api/session-tags` - 获取标签列表
2. `PUT /api/session-tags` - 更新标签（批量）
3. `POST /api/session-tags` - 新增单个标签
4. `DELETE /api/session-tags/:id` - 删除标签
5. `GET /api/customer-360/tag-rules` - 获取自动标签规则
6. `POST /api/customer-360/tag-rules` - 保存自动标签规则
7. `PUT /api/customer-360/tag-rules/:id` - 更新自动标签规则
8. `DELETE /api/customer-360/tag-rules/:id` - 删除自动标签规则
9. `GET /api/user-segment/layers` - 获取分层策略
10. `PUT /api/user-segment/layers` - 保存分层策略
11. `GET /api/customer-360/tag-stats` - 获取标签统计

**审计状态：** 待测试

---

### 44. 转化漏斗模块 (conversionFunnel)
**路由路径：**
- `/conversionFunnel/list` - 转化漏斗

**API 列表：**
1. `GET /api/conversion-funnels` - 获取漏斗报告
2. `GET /api/conversion-funnels/stage` - 获取阶段详情

**审计状态：** 待测试

---

### 45. AI产能分析模块 (aiProductivity)
**路由路径：**
- `/aiProductivity/list` - AI产能分析

**API 列表：**
1. `GET /api/ai-productivity/overview` - 获取产能报告
2. `GET /api/ai-productivity/trend` - 获取日趋势

**审计状态：** 待测试

---

### 46. 知识库模块 (knowledge)
**路由路径：**
- `/knowledge/management` - 知识库管理
- `/knowledge/batch-import` - 批量导入
- `/knowledge/playground` - 检索 Playground
- `/knowledge/feedbacks` - 反馈管理
- `/knowledge/tokens` - API Token
- `/knowledge/external` - 外部系统接入
- `/knowledge/statistics` - 知识库统计
- `/knowledge/openapi` - OpenAPI 集成

**API 列表：**
1. `POST /api/knowledge/import/upload` - 上传文件导入
2. `POST /api/knowledge/import/text` - 文本导入
3. `POST /api/knowledge/import/url` - URL 导入
4. `GET /api/knowledge/documents` - 获取文档列表
5. `GET /api/knowledge/documents/:id` - 获取文档详情
6. `GET /api/knowledge/documents/:id/progress` - 获取文档进度
7. `GET /api/knowledge/documents/:id/chunks` - 获取文档分块
8. `PUT /api/knowledge/documents/:id` - 更新文档
9. `DELETE /api/knowledge/documents/:id` - 删除文档
10. `POST /api/knowledge/documents/:id/reindex` - 重建索引
11. `POST /api/knowledge/products/:productId/rebuild-index` - 重建产品索引
12. `GET /api/knowledge/products/:productId/overview` - 获取产品概览
13. `POST /api/knowledge/search` - 搜索
14. `GET /api/knowledge/import-logs` - 获取导入日志
15. `GET /api/knowledge/openapi/sources` - 获取OpenAPI数据源列表
16. `POST /api/knowledge/openapi/sources` - 创建OpenAPI数据源
17. `GET /api/knowledge/openapi/sources/:id` - 获取OpenAPI数据源详情
18. `PUT /api/knowledge/openapi/sources/:id` - 更新OpenAPI数据源
19. `DELETE /api/knowledge/openapi/sources/:id` - 删除OpenAPI数据源
20. `POST /api/knowledge/openapi/sources/:id/sync` - 同步OpenAPI数据源
21. `POST /api/knowledge/openapi/sources/:id/test` - 测试OpenAPI数据源
22. `POST /api/knowledge/openapi/sources/:id/toggle` - 切换OpenAPI数据源状态
23. `GET /api/knowledge/stats/overview` - 获取概览统计
24. `GET /api/knowledge/stats/documents` - 获取文档统计
25. `GET /api/knowledge/stats/searches` - 获取搜索统计
26. `GET /api/knowledge/stats/imports` - 获取导入统计
27. `GET /api/knowledge/stats/openapi` - 获取OpenAPI统计

**审计状态：** 待测试

---

### 47. AI智能体模块 (aiAgent)
**路由路径：**
- `/aiAgent` - 智能体
- `/aiAgent/list` - 智能体列表
- `/aiAgent/create` - 创建智能体
- `/aiAgent/edit/:id` - 编辑智能体
- `/aiAgent/tools` - AI 工具管理

**API 列表：**
1. `GET /api/ai-agents` - 获取智能体列表
2. `GET /api/ai-agents-enabled` - 获取启用的智能体列表
3. `GET /api/ai-agents/:id` - 获取智能体详情
4. `POST /api/ai-agents` - 创建智能体
5. `PUT /api/ai-agents/:id` - 更新智能体
6. `DELETE /api/ai-agents/:id` - 删除智能体
7. `POST /api/ai-agents/:id/toggle` - 启用/禁用智能体
8. `POST /api/ai-agents/:id/test` - 测试智能体执行
9. `GET /api/ai-agents/:id/context` - 获取智能体执行上下文
10. `GET /api/ai-tools` - 获取工具列表
11. `GET /api/ai-tools/:name` - 获取工具详情
12. `PUT /api/ai-tools/:name/status` - 更新工具启用状态
13. `POST /api/ai-tools/batch-status` - 批量更新工具状态
14. `GET /api/ai-tools/:toolName/accounts` - 获取工具绑定的账号
15. `POST /api/ai-tools/:toolName/accounts` - 绑定账号到工具
16. `DELETE /api/ai-tools/:toolName/accounts/:accountType/:accountId` - 解绑账号
17. `GET /api/channel-accounts` - 获取渠道账号列表
18. `POST /api/channel-accounts/:type/:id/test` - 测试账号连接

**审计状态：** 待测试

---

### 48. 资产市场模块 (assetMarket)
**路由路径：**
- `/asset-market` - 资产市场
- `/asset-market/detail/:id` - 资产详情
- `/asset-market/my-assets` - 我的资产
- `/asset-market/sync-log` - 同步日志

**API 列表：**
1. `GET /api/v1/asset-market/list` - 获取市场资产列表
2. `GET /api/v1/asset-market/detail/:id` - 获取资产详情
3. `POST /api/v1/asset-market/purchase` - 购买资产
4. `POST /api/v1/asset-market/sync` - 同步资产
5. `POST /api/v1/asset-market/report-usage` - 报告使用情况
6. `GET /api/v1/local-assets` - 获取本地资产列表
7. `GET /api/v1/local-assets/:id` - 获取本地资产详情
8. `POST /api/v1/local-assets` - 创建本地资产
9. `PUT /api/v1/local-assets/:id` - 更新本地资产
10. `DELETE /api/v1/local-assets/:id` - 删除本地资产
11. `PUT /api/v1/local-assets/:id/toggle-active` - 切换本地资产状态
12. `GET /api/v1/local-assets/sync-log` - 获取同步日志

**审计状态：** 待测试

---

### 49. 资产包模块 (assetBundle)
**路由路径：**
- `/asset-bundle/list` - 资产包管理
- `/asset-bundle/playground` - 开发者 Playground
- `/asset-bundle/playground/:aid` - 开发者 Playground（编辑）
- `/asset-bundle/merchant-new` - 商户新建话术包
- `/asset-bundle/merchant/:aid` - 商户配置话术包

**API 列表：**
1. `POST /api/asset-bundle` - 创建资产包
2. `PUT /api/asset-bundle/:id` - 更新资产包
3. `GET /api/asset-bundle/:id` - 按 ID 查询
4. `GET /api/asset-bundle/by-aid/:aid` - 按 AssetID 查询
5. `POST /api/asset-bundle/list` - 分页查询
6. `POST /api/asset-bundle/:id/publish` - 启用资产包
7. `POST /api/asset-bundle/:id/submit-platform` - 提交平台审核上架
8. `POST /api/asset-bundle/:id/archive` - 归档资产包
9. `DELETE /api/asset-bundle/:id` - 软删除
10. `POST /api/asset-bundle/weave` - 织布算法
11. `POST /api/asset-bundle/merchant-save` - 商户表单保存
12. `POST /api/asset-bundle/merchant-parse/:aid` - 商户表单解析
13. `POST /api/asset-bundle/:id/enable` - 热启用资产包
14. `POST /api/asset-bundle/:id/disable` - 热禁用资产包
15. `POST /api/asset-bundle/enabled/list` - 查询已热启用的资产包列表

**审计状态：** 待测试

---

### 50. 客服子功能模块 (customerService)
**路由路径：**
- `/customerService/agentStatus` - 坐席状态
- `/customerService/quickReply` - 快捷回复
- `/customerService/sessionTag` - 会话标签
- `/customerService/aiSuggestion` - AI 建议

**API 列表：**
1. `POST /api/agents` - 创建坐席
2. `GET /api/agents/:id` - 获取坐席状态
3. `GET /api/agents/online` - 获取在线坐席
4. `GET /api/agents/all` - 获取所有坐席
5. `PUT /api/agents/:id/status` - 更新坐席状态
6. `POST /api/agents/:id/online` - 上线
7. `POST /api/agents/:id/offline` - 下线
8. `GET /api/agents/:id/sessions` - 获取坐席会话
9. `GET /api/agents/me` - 获取当前登录用户坐席身份
10. `GET /api/quick-replies` - 获取快捷回复列表
11. `GET /api/quick-replies/categories` - 获取快捷回复分类
12. `POST /api/quick-replies` - 创建快捷回复
13. `PUT /api/quick-replies/:id` - 更新快捷回复
14. `DELETE /api/quick-replies/:id` - 删除快捷回复
15. `GET /api/session-tags` - 获取会话标签
16. `POST /api/session-tags` - 创建会话标签
17. `PUT /api/session-tags/:id` - 更新会话标签
18. `DELETE /api/session-tags/:id` - 删除会话标签
19. `GET /api/ai-suggestions/:sessionId` - 获取AI建议
20. `POST /api/ai-suggestions/:id/use` - 使用AI建议
21. `POST /api/customer-sessions/:sessionId/tags` - 会话打标

**审计状态：** 待测试

---

### 51. 客服渠道模块 (chatChannel)
**路由路径：**
- `/chatChannel/list` - 客服渠道
- `/chatChannel/create` - 新建客服渠道
- `/chatChannel/edit/:id` - 编辑客服渠道
- `/chatChannel/install-guide/:id?` - Widget 安装引导

**API 列表：**
1. `GET /api/chat-channels` - 获取渠道列表
2. `GET /api/chat-channels/:channelId` - 获取渠道详情
3. `POST /api/chat-channels` - 创建渠道
4. `PUT /api/chat-channels/:channelId` - 更新渠道
5. `DELETE /api/chat-channels/:channelId` - 禁用渠道
6. `POST /api/chat-channels/:channelId/rotate-key` - 轮换 AppKey
7. `POST /api/chat-channels/:channelId/reset-secret` - 重置 AppSecret

**审计状态：** 待测试

---

### 52. 异议处理模块 (objection)
**路由路径：**
- `/objection/list` - 异议处理

**API 列表：**
1. `POST /api/objection/handle` - 智能处理异议
2. `POST /api/objection/classify` - 仅分类
3. `GET /api/objection/categories` - 列出所有异议类别
4. `POST /api/objection/usage` - 记录异议模板使用结果

**审计状态：** 待测试

---

### 53. 销冠画像模块 (persona)
**路由路径：**
- `/persona/list` - 销冠画像

**API 列表：**
1. `GET /api/analytics/persona/staffs` - 列出所有员工
2. `GET /api/analytics/persona/staffs/:staffId` - 获取指定员工的能力画像报告

**审计状态：** 待测试

---

### 54. 客户旅程大屏模块 (customerJourney)
**路由路径：**
- `/customerJourney/dashboard` - 客户旅程大屏

**API 列表：**
1. `GET /api/customer-journey/overview` - 获取总览
2. `GET /api/customer-journey/overview?customer_id=:customerId` - 获取单个客户旅程状态
3. `GET /api/customer-journey/stages` - 列出所有阶段配置
4. `GET /api/customer-journey/by-stage?stage=:stage` - 按阶段列出客户
5. `POST /api/customer-journey/transition` - 迁移客户阶段
6. `POST /api/customer-journey/touch` - 记录客户互动

**审计状态：** 待测试

---

### 55. 备份恢复模块 (backup)
**路由路径：**
- `/backup/list` - 备份恢复

**API 列表：**
1. `GET /api/backups` - 获取备份列表
2. `GET /api/backups/:id` - 获取备份详情
3. `POST /api/backups` - 创建备份
4. `DELETE /api/backups/:id` - 删除备份
5. `POST /api/restore` - 触发恢复
6. `GET /api/restore/list` - 获取恢复记录列表
7. `GET /api/restore/last` - 获取最近一次恢复

**审计状态：** 待测试

---

### 56. 安全审计模块 (securityAudit)
**路由路径：**
- `/securityAudit/list` - 安全审计

**API 列表：**
1. `POST /api/security/audit` - 启动安全审计
2. `GET /api/security/audit/list` - 获取审计历史列表
3. `GET /api/security/audit/:id` - 获取审计详情

**审计状态：** 待测试

---

### 57. 飞书模块 (feishu)
**路由路径：**
- `/feishu` - 飞书
- `/feishu/account` - 飞书账号

**API 列表：**
1. `GET /api/feishu/accounts` - 列出所有飞书账号
2. `GET /api/feishu/accounts/:id` - 获取账号详情
3. `POST /api/feishu/accounts` - 创建飞书账号
4. `PUT /api/feishu/accounts/:id` - 更新飞书账号
5. `DELETE /api/feishu/accounts/:id` - 删除飞书账号
6. `POST /api/feishu/accounts/:id/test-send` - 测试发送消息
7. `POST /api/feishu/accounts/:id/refresh-token` - 刷新 Access Token

**审计状态：** 待测试

---

### 58. 置信度/拟人度/反馈学习模块 (tuning)
**路由路径：**
- `/confidence/panel` - 置信度运营
- `/humanize/panel` - 拟人度评估
- `/feedbackLoop/panel` - 反馈学习闭环

**API 列表：**
1. `GET /api/admin/tuning/confidence/signals` - 获取置信度信号列表
2. `GET /api/admin/tuning/confidence/signals/:id` - 获取置信度信号详情
3. `GET /api/admin/tuning/confidence/signals/stats` - 获取置信度信号统计
4. `GET /api/admin/tuning/confidence/calibrations` - 获取置信度校准列表
5. `GET /api/admin/tuning/confidence/policies` - 获取阈值策略列表
6. `PUT /api/admin/tuning/confidence/policies` - 更新阈值策略
7. `GET /api/admin/tuning/humanize/scores` - 获取拟人度评分列表
8. `GET /api/admin/tuning/humanize/scores/stats` - 获取拟人度评分统计
9. `GET /api/admin/tuning/humanize/baselines` - 获取销冠基线列表
10. `GET /api/admin/tuning/humanize/low-quality` - 获取低质样本列表
11. `GET /api/admin/tuning/feedback/events` - 获取反馈事件列表
12. `GET /api/admin/tuning/feedback/events/stats` - 获取反馈事件统计
13. `GET /api/admin/tuning/feedback/dialogues` - 获取销冠对话列表
14. `GET /api/admin/tuning/prompt/candidates` - 获取 Prompt 候选列表
15. `PUT /api/admin/tuning/prompt/candidates/:id/status` - 更新 Prompt 候选状态
16. `GET /api/admin/tuning/bandit/arms` - 获取 Bandit 臂列表

**审计状态：** 待测试

---

### 59. 系统用户模块 (systemUser)
**路由路径：**
- `/system/users` - 人员管理

**API 列表：**
1. `GET /api/system/users` - 获取系统用户列表
2. `GET /api/system/users/:id` - 获取系统用户详情
3. `POST /api/system/users` - 创建系统用户
4. `PUT /api/system/users/:id` - 更新系统用户
5. `DELETE /api/system/users/:id` - 删除系统用户

**审计状态：** 待测试

---

### 60. 角色管理模块 (role)
**路由路径：**
- `/system/roles` - 角色管理

**API 列表：**
1. `GET /api/system/roles` - 列出所有角色
2. `GET /api/system/roles/:code` - 获取角色详情
3. `GET /api/system/roles/:code/members` - 获取角色下成员列表

**审计状态：** 待测试

---

### 61. 授权管理模块 (permission)
**路由路径：**
- `/system/permissions` - 授权管理

**API 列表：**
1. `PUT /api/system/permissions/:id/enabled` - 启用/禁用账号
2. `PUT /api/system/permissions/:id/password` - 重置密码
3. `GET /api/system/permissions/audit-logs` - 获取操作审计日志

**审计状态：** 待测试

---

### 62. 术语表模块 (glossary)
**路由路径：**
- `/glossary` - 术语表管理

**API 列表：**
1. `POST /api/glossaries` - 创建术语
2. `GET /api/glossaries` - 获取术语列表
3. `GET /api/glossaries/:termId` - 获取术语详情
4. `PUT /api/glossaries/:termId` - 更新术语
5. `DELETE /api/glossaries/:termId` - 删除术语
6. `POST /api/glossaries/validate` - 校验预览

**审计状态：** 待测试

---

### 63. 多语言监控模块 (i18nStats)
**路由路径：**
- `/i18n/dashboard` - 多语言监控

**API 列表：**
1. `GET /api/i18n/stats` - 获取总览统计
2. `GET /api/i18n/stats/lang-dist` - 获取语言分布
3. `GET /api/i18n/stats/cache` - 获取缓存命中率
4. `GET /api/i18n/stats/glossary` - 获取术语覆盖率
5. `GET /api/i18n/stats/quality` - 获取质量评分趋势
6. `GET /api/i18n/stats/latency` - 获取延迟统计

**审计状态：** 待测试

---

*文档将随着审计过程持续更新*
