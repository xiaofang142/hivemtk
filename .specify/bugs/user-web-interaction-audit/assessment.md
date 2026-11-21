# Bug Assessment: user-web 全页面交互与数据审计

- Slug: user-web-interaction-audit
- Created: 2026-11-20
- Source: 用户要求逐页检查每个交互、每项数据，整理问题并修复。
- Verdict: valid（静态根因确认，逐项复现和验证执行中）
- Severity: high

## Symptom / Root Cause

1. 批量操作 `runBatch` 忽略 dryRun；选择文件立即导入，执行再次导入；非法筛选结构退化为全部，查询分页可被筛选覆盖；预览字段映射不符后端。
2. 意图工具流、平台探针、运行时配置与字典重复拆包；测试 mock 多包 data 掩盖问题。
3. 权限菜单请求并发不等待，失败时 canAccessPath 放行，重置后旧响应可能回灌。
4. 客户会话画像未映射 session_stats 与 tags；消息、画像、建议、发送回执在会话切换时可能串数据。
5. 客户 360 搜索总数/分页不一致，主详情失败未判定，订单缺请求代次保护。
6. 素材手工上传绕过大小钩子，缺锁与部分成功处理；知识批量导入缺类型校验、CSV 多行处理、旧预览失效与移除文件清理。
7. 登录 Enter 冒泡重复提交，校验前无锁；收银台 interval 重叠且关闭/卸载/订单切换不能隔离旧响应。

## Reproduction

浏览器已打开 http://localhost:8211/#/login，确认用户名、密码、登录按钮可见。本机 API/PostgreSQL/Redis 无监听，无已登录会话；真实业务链路受阻。危险写操作使用本地合成数据夹具，不能当作真实后端验收。现有 npm test 基线正在执行。

## Proposed Remediation

按真实请求层拆包契约修复读取，危险操作前验证并在预览时只读，文件只在执行时写一次。提交加锁，异步按代次和身份隔离。页面失败与空态分开。每个发现补针对性行为回归，逐元素浏览器取证。

## Files likely to change

- user-web/src/api/batchOperation.js、src/views/batchOperation/List.vue
- user-web/src/api/customer360.js：getCustomerDetail 吞掉基础资料失败，需拒绝伪造空客户。
- user-web/src/views/customerSession/List.vue：画像/建议重复读取与标签初始化时序，纳入会话切换修复。
- user-web/src/views/userSegment/List.vue：查看客户详情跳到未注册的 /customer360，需改到真实列表路由。
- src/views/intentRecognition/ToolFlow.vue、src/views/system/ConfigParams.vue
- src/stores/{permission,runtimeConfig,dictionary}.js、src/router/index.js、src/main.js
- src/views/customerSession/composables/useSessionList.js、components/CustomerProfilePanel.vue
- src/views/customer360/List.vue、src/views/system/MaterialLibrary.vue
- src/views/KnowledgeWorkspace/BatchImport.vue、src/views/Login.vue、src/components/CashierDialog.vue
- user-web/tests/unit/ 对应专项测试、tests/fixtures/page-audit/ 受控入口
- user-web/docs/audit/ 本轮清单与证据、.specify/bugs/user-web-interaction-audit/{fix,test}.md

## Tests to add or update

批量 dryRun 不写、非法筛选拒绝、选择不提交及执行去重；真实响应拆包；菜单失败拒绝/并发/账号隔离；客户搜索分页与订单竞态；会话画像与切换隔离；文件大小/部分成功、导入类型/旧预览；Enter 去重；支付关闭/卸载/订单切换和串行轮询。

## Risks & Considerations

保留原有 docs/verification/api-list.md 修改。不提交或推送（用户尚未要求）。无证据不标通过，夹具结果与真实链路分开。追加新根因后再改相关文件；已存在报告不覆盖。hooks 配置为空。
