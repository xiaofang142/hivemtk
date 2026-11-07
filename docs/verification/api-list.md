# API 路由清单（SaaS 多租户 · 私域独立部署 双模式逐路由 curl 验证）

- 来源：`user-web/src/api/*.js`（125 个文件）
- 规模：**1139 条前端 API 出口**
- 生成方式：`scripts/gen_api_list.py`（只做索引，不发请求）
- 参数列：`path` 路径占位符 / `query` 查询参数 / `body` 请求体（从前端调用点实参归类）
- 响应列：逐条 curl 实测后回填（HTTP 状态 + 业务 `code` + 关键字段），未验为 `-`
- 校验口径：每条路由**逐条手工 curl**，分析 `code` 与 `data`；
  `code=0` 通过；`code!=0` 判定是调用错误还是业务错误，即时修复
- **本轮为全量重测**：旧轮勾选不再沿用（历史见 git），所有路由从 `[ ]` 重新验证

## 状态标记

| 标记 | 含义 |
|---|---|
| `[ ]` | 未验证 |
| `[x]` | 已验证通过（HTTP 200 且 `code=0`，或已确认是**正确**的业务错误响应） |
| `[~]` | 已验证但**存在待修问题**（见任务三结论与本轮修复记录） |

> 本次生成显式清零旧勾选：上一轮结论对应的是旧代码快照，
> 本轮要求每条路由重新 curl 取证后才置 `[x]`。
## 档位说明

| 档位 | 含义 |
|---|---|
| 公开 | 无需登录（登录、模块市场、公开聊天） |
| 用户端 | 需登录的普通业务接口 `/api/*` |
| 管理端 | 管理面接口 `/api/manage/*`、`/api/platform/*` |

## 清单

| # | 状态 | 方法 | 路径 | 档位 | 参数 | 响应 | 源文件 | 行 |
|---|---|---|---|---|---|---|---|---|
| 1 | [x] | GET | `/api/ab-experiments` | 用户端 | query: params | 200 code=0 双形态（off total=22 / on total=0 租户隔离） | `src/api/abExperiment.js` | 4 |
| 2 | [x] | GET | `/api/ab-experiments/{id}` | 用户端 | path: id | 200 code=0 详情 id=29 | `src/api/abExperiment.js` | 7 |
| 3 | [x] | POST | `/api/ab-experiments` | 用户端 | body: data | 200 code=0 建 id=30 | `src/api/abExperiment.js` | 10 |
| 4 | [x] | PUT | `/api/ab-experiments/{id}` | 用户端 | path: id / body: data | 200 code=0 更新成功 | `src/api/abExperiment.js` | 13 |
| 5 | [x] | DELETE | `/api/ab-experiments/{id}` | 用户端 | path: id | 200 code=0 删除成功 | `src/api/abExperiment.js` | 16 |
| 6 | [x] | POST | `/api/ab-experiments/{id}/start` | 用户端 | path: id | 200 code=0 启动成功 | `src/api/abExperiment.js` | 19 |
| 7 | [x] | POST | `/api/ab-experiments/{id}/pause` | 用户端 | path: id | 200 code=0 暂停成功 | `src/api/abExperiment.js` | 22 |
| 8 | [x] | POST | `/api/ab-experiments/{id}/stop` | 用户端 | path: id | 200 code=0 停止成功 | `src/api/abExperiment.js` | 25 |
| 9 | [x] | GET | `/api/ab-experiments/{id}/results` | 用户端 | path: id | 200 code=0 data=[] | `src/api/abExperiment.js` | 28 |
| 10 | [x] | GET | `/api/ab-experiments/{id}/conversion-events` | 用户端 | path: id | 200 code=0 total=0 | `src/api/abExperiment.js` | 31 |
| 11 | [x] | GET | `/api/ab-experiments` | 用户端 | - | 200 code=0（别名同 #1） | `src/api/abExperiment.js` | 41 |
| 12 | [x] | GET | `/api/ab-experiments/{id}/stats` | 用户端 | path: id / query: method | 200 code=0 method=frequentist | `src/api/abExperimentPlus.js` | 6 |
| 13 | [x] | GET | `/api/ab-experiments/{id}/diagnostics` | 用户端 | path: id | 200 code=0 srm/warnings 结构完整 | `src/api/abExperimentPlus.js` | 9 |
| 14 | [x] | GET | `/api/ab-experiments/{id}/cuped` | 用户端 | path: id | 200 code=0 available=false 退化说明 | `src/api/abExperimentPlus.js` | 12 |
| 15 | [x] | POST | `/api/ab-experiments/{id}/sequential-test` | 用户端 | path: id / body: { alpha } | 200 code=0 verdict=continue | `src/api/abExperimentPlus.js` | 15 |
| 16 | [x] | POST | `/api/ab-experiments/{id}/bayesian-test` | 用户端 | path: id / body: {} | 200 code=0 posterior/expected_loss 完整 | `src/api/abExperimentPlus.js` | 18 |
| 17 | [x] | POST | `/api/agent-kb-bindings` | 用户端 | body: { agent_id, knowledge_base_id } | 200 code=0 绑定成功（注意字段名是 knowledge_base_id，不是 kb_id） | `src/api/agentKBBinding.js` | 4 |
| 18 | [x] | DELETE | `/api/agent-kb-bindings/{agentId}/{kbId}` | 用户端 | path: agentId, kbId | 200 code=0 解绑成功 | `src/api/agentKBBinding.js` | 8 |
| 19 | [x] | GET | `/api/agent-kb-bindings/by-agent/{agentId}` | 用户端 | path: agentId | 200 code=0 数组（含 kb_type/role/priority） | `src/api/agentKBBinding.js` | 12 |
| 20 | [x] | GET | `/api/agent-kb-bindings/by-kb/{kbId}` | 用户端 | path: kbId | 200 code=0 数组 | `src/api/agentKBBinding.js` | 16 |
| 21 | [x] | PUT | `/api/agent-kb-bindings/by-agent/{agentId}` | 用户端 | path: agentId / body: { kb_ids: string[] } | 200 code=0 同步成功 count=1（kb_ids 必须是字符串数组） | `src/api/agentKBBinding.js` | 20 |
| 22 | [x] | GET | `/api/ai-agents` | 用户端 | query: params | 200 code=0 list | `src/api/aiAgent.js` | 4 |
| 23 | [x] | GET | `/api/ai-agents-enabled` | 用户端 | - | 200 code=0 list | `src/api/aiAgent.js` | 8 |
| 24 | [x] | GET | `/api/ai-agents/{id}` | 用户端 | path: id | 200 code=0 详情 id=76 | `src/api/aiAgent.js` | 12 |
| 25 | [x] | POST | `/api/ai-agents` | 用户端 | body: data | 200 code=0 建 id=87 | `src/api/aiAgent.js` | 16 |
| 26 | [x] | PUT | `/api/ai-agents/{id}` | 用户端 | path: id / body: data | 200 code=0 更新成功 | `src/api/aiAgent.js` | 20 |
| 27 | [x] | DELETE | `/api/ai-agents/{id}` | 用户端 | path: id | 200 code=0 删除成功 | `src/api/aiAgent.js` | 24 |
| 28 | [x] | POST | `/api/ai-agents/{id}/toggle` | 用户端 | path: id / body: { status } | 200 code=0 status=0 | `src/api/aiAgent.js` | 28 |
| 29 | [x] | POST | `/api/ai-agents/{id}/test` | 用户端 | path: id / body: { message } | 200 code=0 reply+citations；禁用态 404「测试失败」为正确业务拒绝 | `src/api/aiAgent.js` | 32 |
| 30 | [x] | GET | `/api/ai-agents/{id}/context` | 用户端 | path: id | 200 code=0 加载成功 | `src/api/aiAgent.js` | 36 |
| 31 | [x] | GET | `/api/ai-productivity/overview` | 用户端 | query: params | 200 code=0 报告结构完整 | `src/api/aiProductivity.js` | 5 |
| 32 | [x] | GET | `/api/ai-productivity/trend` | 用户端 | query: params | 200 code=0 trend 数组 | `src/api/aiProductivity.js` | 8 |
| 33 | [x] | GET | `/api/ai-tools` | 用户端 | query: params | 200 code=0 list+total | `src/api/aiTool.js` | 4 |
| 34 | [x] | GET | `/api/ai-tools/{name}` | 用户端 | path: name | 200 code=0 详情 | `src/api/aiTool.js` | 8 |
| 35 | [x] | PUT | `/api/ai-tools/{name}/status` | 用户端 | path: name / body: { is_enabled } | 200 code=0 success | `src/api/aiTool.js` | 12 |
| 36 | [x] | POST | `/api/ai-tools/batch-status` | 用户端 | body: { tools, is_enabled } | 200 code=0 success | `src/api/aiTool.js` | 16 |
| 37 | [x] | GET | `/api/ai-tools/{toolName}/accounts` | 用户端 | path: toolName | 200 code=0 data=[] | `src/api/aiTool.js` | 20 |
| 38 | [x] | POST | `/api/ai-tools/{toolName}/accounts` | 用户端 | path: toolName / body: data | 200 code=0 success | `src/api/aiTool.js` | 24 |
| 39 | [x] | DELETE | `/api/ai-tools/{toolName}/accounts/{accountType}/{accountId}` | 用户端 | path: 3 段 | 200 code=0 success | `src/api/aiTool.js` | 32 |
| 40 | [x] | GET | `/api/approvals/{id}` | 用户端 | path: id | 503「审批底座不可用」——FF_LTC_APPROVAL_RESUME 默认 off 的 fail-closed 设计，非缺陷 | `src/api/approval.js` | 18 |
| 41 | [x] | POST | `/api/approvals/{id}/decide` | 用户端 | path: id / body: { verdict, note } | 503 同上（off 档关闸） | `src/api/approval.js` | 27 |
| 42 | [x] | POST | `/api/asset-bundle` | 用户端 | body: data（asset_id/title/messages 必填） | 200 code=0 建 id=190 tenant_id=1 status=draft | `src/api/assetBundle.js` | 4 |
| 43 | [x] | PUT | `/api/asset-bundle/{id}` | 用户端 | path: id / body: data | 200 code=0 更新成功，回读 tenant_id=1（W-5 修复生效） | `src/api/assetBundle.js` | 7 |
| 44 | [x] | GET | `/api/asset-bundle/{id}` | 用户端 | path: id | 200 code=0 详情完整 | `src/api/assetBundle.js` | 10 |
| 45 | [x] | GET | `/api/asset-bundle/by-aid/{aid}` | 用户端 | path: aid | 200 code=0 按 asset_id 命中 id=190 | `src/api/assetBundle.js` | 13 |
| 46 | [x] | POST | `/api/asset-bundle/list` | 用户端 | body: { keyword, page, size }（也支持 query） | 200 code=0 list+分页，keyword 命中 | `src/api/assetBundle.js` | 16 |
| 47 | [x] | POST | `/api/asset-bundle/{id}/publish` | 用户端 | path: id | 200 code=0 发布成功 | `src/api/assetBundle.js` | 19 |
| 48 | [x] | POST | `/api/asset-bundle/{id}/submit-platform` | 用户端 | path: id | 403 FORBIDDEN_2002「平台集成未启用（设 PLATFORM_ENABLED=true…）」——fail-closed 设计，非缺陷 | `src/api/assetBundle.js` | 22 |
| 49 | [x] | POST | `/api/asset-bundle/{id}/archive` | 用户端 | path: id | 200 code=0 归档成功 | `src/api/assetBundle.js` | 25 |
| 50 | [x] | DELETE | `/api/asset-bundle/{id}` | 用户端 | path: id | 200 code=0 删除成功，复核 404 确认真删 | `src/api/assetBundle.js` | 28 |
| 51 | [x] | POST | `/api/asset-bundle/weave` | 用户端 | body: { asset_id, user_query, chat_history, merchant_vars, options } | 200 code=0 messages=3，商户参数注入 system、history+query 正确拼接、stats 完整（前置：需 enable） | `src/api/assetBundle.js` | 31 |
| 52 | [x] | POST | `/api/asset-bundle/merchant-save` | 用户端 | body: { asset_id, shop_name…, qa_cards } | 200 code=0 表单翻译成 messages 建 id=191 | `src/api/assetBundle.js` | 34 |
| 53 | [x] | POST | `/api/asset-bundle/merchant-parse/{aid}` | 用户端 | path: aid | 200 code=0 表单字段+QA 卡片完整还原（与 #52 往返一致）；开发者模式包（无固定中文标签）返回空表单属正确降级 | `src/api/assetBundle.js` | 37 |
| 54 | [x] | POST | `/api/asset-bundle/{id}/enable` | 用户端 | path: id | 200 code=0 启用成功（status→enabled） | `src/api/assetBundle.js` | 40 |
| 55 | [x] | POST | `/api/asset-bundle/{id}/disable` | 用户端 | path: id | 200 code=0 停用成功 | `src/api/assetBundle.js` | 43 |
| 56 | [x] | POST | `/api/asset-bundle/enabled/list` | 用户端 | - | 200 code=0 list（返回历史 enabled 包） | `src/api/assetBundle.js` | 46 |
| 57 | [x] | POST | `/api/upload` | 用户端 | body: fd（multipart file） | .txt 400 类型白名单拒绝（正确）；PNG 200 上传成功 url=/files/... virus_clean=true | `src/api/assetBundle.js` | 51 |
| 58 | [x] | GET | `/api/v1/asset-market/list` | 用户端 | query: params | 200 code=0 list+total | `src/api/assetMarket.js` | 4 |
| 59 | [x] | GET | `/api/v1/asset-market/detail/{id}` | 用户端 | path: id | 200 code=6001「资产不存在或不可用」业务正确 | `src/api/assetMarket.js` | 7 |
| 60 | [x] | POST | `/api/v1/asset-market/purchase` | 用户端 | body: data | 403 FORBIDDEN_2002 平台集成未启用（fail-closed，非缺陷） | `src/api/assetMarket.js` | 10 |
| 61 | [x] | POST | `/api/v1/asset-market/sync` | 用户端 | body: data | 403 同上 | `src/api/assetMarket.js` | 13 |
| 62 | [x] | POST | `/api/v1/asset-market/report-usage` | 用户端 | body: data | 403 同上 | `src/api/assetMarket.js` | 16 |
| 63 | [x] | GET | `/api/v1/local-assets` | 用户端 | query: params | 200 code=0 list（租户隔离数据） | `src/api/assetMarket.js` | 19 |
| 64 | [x] | GET | `/api/v1/local-assets/{id}` | 用户端 | path: id | 200 code=0 详情完整 | `src/api/assetMarket.js` | 22 |
| 65 | [x] | POST | `/api/v1/local-assets` | 用户端 | body: { asset_type, industry, name, data } | 200 code=0 建 id=22 source=manual（asset_type/industry 白名单+data JSON 校验生效） | `src/api/assetMarket.js` | 25 |
| 66 | [x] | PUT | `/api/v1/local-assets/{id}` | 用户端 | path: id / body: 全量 { name, asset_type, industry, data } | 200 code=0 更新成功（缺 asset_type 时 400「asset_type 非法」提示准确） | `src/api/assetMarket.js` | 28 |
| 67 | [x] | DELETE | `/api/v1/local-assets/{id}` | 用户端 | path: id | 200 code=0 删除成功 | `src/api/assetMarket.js` | 31 |
| 68 | [x] | PUT | `/api/v1/local-assets/{id}/toggle-active` | 用户端 | path: id / body: { active } | 200 code=0 操作成功 | `src/api/assetMarket.js` | 34 |
| 69 | [x] | GET | `/api/v1/local-assets/sync-log` | 用户端 | query: params | 200 code=0 历史同步日志（含失败记录 error_msg） | `src/api/assetMarket.js` | 37 |
| 70 | [x] | GET | `/api/backups` | 用户端 | query: params | 200 code=0 list+分页 | `src/api/backup.js` | 4 |
| 71 | [x] | GET | `/api/backups/{id}` | 用户端 | path: id | 200 code=0 详情（备份异步执行 status→completed） | `src/api/backup.js` | 8 |
| 72 | [x] | POST | `/api/backups` | 用户端 | body: { backup_name, backup_type } | 200 建 id=206 pending（backup_name 仅 [A-Za-z0-9_-]，中文名 400 提示准确） | `src/api/backup.js` | 12 |
| 73 | [x] | DELETE | `/api/backups/{id}` | 用户端 | path: id | 200 code=0 删除成功 | `src/api/backup.js` | 16 |
| 74 | [~] | POST | `/api/restore` | 用户端 | body: { backup_id } | 404「备份不存在，无法恢复」/400「backup_id 必填」验证通过；**真实恢复路径未执行**（会回写共享 DB，破坏并行会话数据，只验证拒绝路径） | `src/api/backup.js` | 20 |
| 75 | [x] | GET | `/api/restore/list` | 用户端 | query: params | 200 code=0 list+分页 | `src/api/backup.js` | 24 |
| 76 | [x] | GET | `/api/restore/last` | 用户端 | - | 200 code=0 最近恢复记录 | `src/api/backup.js` | 28 |
| 77 | [x] | GET | `/api/bad-cases` | 用户端 | query: params（label 可重复键） | 200 code=0 list | `src/api/badCase.js` | 44 |
| 78 | [x] | GET | `/api/bad-cases/stats` | 用户端 | - | 200 code=0 by_status/by_fix_layer/labeled_ratio | `src/api/badCase.js` | 50 |
| 79 | [x] | GET | `/api/bad-cases/taxonomy` | 用户端 | - | 200 code=0 fix_layers/label_layer 值域表 | `src/api/badCase.js` | 57 |
| 80 | [x] | GET | `/api/bad-cases/{id}` | 用户端 | path: id | 200 code=0 详情 | `src/api/badCase.js` | 61 |
| 81 | [x] | POST | `/api/bad-cases` | 用户端 | body: { session_id, query_text, answer_text } | 200 建 manual 坏例；缺 query/answer 400 提示准确 | `src/api/badCase.js` | 69 |
| 82 | [x] | POST | `/api/bad-cases/{id}/label` | 用户端 | path: id / body: { label, note } | 200 status→labeled；非法 label 400、空 note 400 均准确 | `src/api/badCase.js` | 76 |
| 83 | [x] | POST | `/api/bad-cases/{id}/dismiss` | 用户端 | path: id / body: { reason } | 200 status→dismissed；空理由 400 | `src/api/badCase.js` | 82 |
| 84 | [x] | POST | `/api/bad-cases/export` | 用户端 | body: { labels, limit } | 负数 400「limit 不能为负」；无可导样本 409 DUPLICATE_ENTRY_3003（业务结论非失败）；真实导出路径需已打标数据 | `src/api/badCase.js` | 89 |
| 85 | [x] | GET | `/api/clues/list` | 用户端 | query: params | 200 code=0 total=8 + list | `src/api/batchOperation.js` | 83 |
| 86 | [x] | POST | `/api/batch/import` | 用户端 | body: fd（multipart CSV） | 200 导入完成 total=1 success=1 | `src/api/batchOperation.js` | 112 |
| 87 | [x] | GET | `/api/batch/template` | 用户端 | query: type | 200 CSV 模板内容（示例行） | `src/api/batchOperation.js` | 118 |
| 88 | [x] | POST | `/api/batch/export` | 用户端 | body: form urlencoded { type, format, ids } | 200 text/csv attachment（filename=clue_export_N.csv），内容与库一致；空 ids 400 提示准确 | `src/api/batchOperation.js` | 127 |
| 89 | [x] | POST | `/api/batch/delete` | 用户端 | body: { ids } | 200 deleted_count=1（删除自建测试线索） | `src/api/batchOperation.js` | 143 |
| 90 | [x] | POST | `/api/batch/update` | 用户端 | body: { ids, fields } | 200 updated_count=1 | `src/api/batchOperation.js` | 168 |
| 91 | [x] | GET | `/api/batch/tools` | 用户端 | - | 200 code=0 list（import/export/delete…） | `src/api/batchOperation.js` | 175 |
| 92 | [x] | GET | `/api/batch/histories` | 用户端 | query: params | 200 code=0 list（#89 删除留痕 id=13） | `src/api/batchOperation.js` | 220 |
| 93 | [x] | POST | `/api/batch/histories/{id}/cancel` | 用户端 | path: id | 已完成单 409 DUPLICATE_ENTRY_3003「仅 pending/running 可取消」业务正确 | `src/api/batchOperation.js` | 223 |
| 94 | [x] | GET | `/api/batch/histories/{id}` | 用户端 | path: id | 200 code=0 详情 | `src/api/batchOperation.js` | 226 |
| 95 | [x] | POST | `/api/batch/preview` | 用户端 | body: { operation_type, data_type, ids } | 空 ids 400「请选择要操作的记录」；正常 preview 需真实 ids | `src/api/batchOperation.js` | 235 |
| 96 | [x] | GET | `/api/bridge/extension/package` | 用户端 | - | 404 NOT_FOUND_1002「当前部署未接入平台，或平台尚未上架桥接扩展包」——独立部署无平台的正确降级 | `src/api/bridgeExtension.js` | 11 |
| 97 | [x] | GET | `/api/bridge/token/status` | 用户端 | - | 200 main_configured=true prev_configured=true source=db | `src/api/bridgeToken.js` | 3 |
| 98 | [x] | POST | `/api/bridge/token/reset` | 用户端 | - | 200 轮换成功，旧凭据进灰度窗口（prev_configured=true 保留） | `src/api/bridgeToken.js` | 5 |
| 99 | [x] | GET | `/api/browser-automation/tasks` | 用户端 | query: params | 200 code=0 list | `src/api/browserAutomation.js` | 6 |
| 100 | [x] | GET | `/api/browser-automation/tasks/{id}` | 用户端 | path: id | 200 code=0 详情 | `src/api/browserAutomation.js` | 9 |
| 101 | [x] | POST | `/api/browser-automation/tasks` | 用户端 | body: { name, task_type(one_shot/loop/cron/workflow), platform, url, steps } | 200 建 id=605 draft；校验链完整：缺 task_type/url/非法 action/缺 platform 各 400 文案准确 | `src/api/browserAutomation.js` | 12 |
| 102 | [x] | PUT | `/api/browser-automation/tasks/{id}` | 用户端 | path: id / body: data（同 create 全量） | 200 code=0 更新成功 | `src/api/browserAutomation.js` | 15 |
| 103 | [x] | DELETE | `/api/browser-automation/tasks/{id}` | 用户端 | path: id | 200「已删除」 | `src/api/browserAutomation.js` | 18 |
| 104 | [x] | POST | `/api/browser-automation/tasks/{id}/publish` | 用户端 | path: id | 200「已发布」 | `src/api/browserAutomation.js` | 21 |
| 105 | [~] | POST | `/api/browser-automation/tasks/{id}/run` | 用户端 | path: id / body: {} | 8001 BROWSER_HOST_OFFLINE「browser host 未连接…」——环境依赖（本机 Chrome 扩展未连），非代码缺陷 | `src/api/browserAutomation.js` | 27 |
| 106 | [x] | POST | `/api/browser-automation/tasks/{id}/pause` | 用户端 | path: id | 8003 状态机校验「仅执行中任务可暂停，当前状态: draft」业务正确（running 态需 browser host） | `src/api/browserAutomation.js` | 30 |
| 107 | [x] | POST | `/api/browser-automation/tasks/{id}/resume` | 用户端 | path: id | 8003「仅暂停任务可恢复」业务正确 | `src/api/browserAutomation.js` | 33 |
| 108 | [x] | POST | `/api/browser-automation/tasks/{id}/archive` | 用户端 | path: id | 200「已归档」 | `src/api/browserAutomation.js` | 36 |
| 109 | [x] | PUT | `/api/browser-automation/tasks/{id}/dependency` | 用户端 | path: id / body: data | 200「依赖已更新」 | `src/api/browserAutomation.js` | 39 |
| 110 | [x] | GET | `/api/browser-automation/sessions` | 用户端 | query: params | 200 code=0 list | `src/api/browserAutomation.js` | 44 |
| 111 | [x] | GET | `/api/browser-automation/sessions/{id}` | 用户端 | path: id | 非法 id 400「invalid id」；真实 session 需先 run（browser host 阻塞） | `src/api/browserAutomation.js` | 47 |
| 112 | [x] | GET | `/api/browser-automation/sessions/{id}/steps` | 用户端 | path: id | 非法 id 400「invalid id」（同 #111） | `src/api/browserAutomation.js` | 50 |
| 113 | [x] | GET | `/api/browser-automation/tasks/{taskId}/sessions` | 用户端 | path: taskId / query: params | 200 code=0 list | `src/api/browserAutomation.js` | 54 |
| 114 | [x] | POST | `/api/browser-automation/sessions/{id}/stop` | 用户端 | path: id / body: { reason } | 非法 id 400「invalid id」；运行态 stop 需 browser host | `src/api/browserAutomation.js` | 57 |
| 115 | [x] | GET | `/api/browser-automation/sessions/{id}/confirm-gate` | 用户端 | path: id | 非法 id 400（同上） | `src/api/browserAutomation.js` | 62 |
| 116 | [x] | POST | `/api/browser-automation/sessions/{id}/confirm` | 用户端 | path: id / body: { payload_hash } | 非法 id 400（同上） | `src/api/browserAutomation.js` | 72 |
| 117 | [x] | GET | `/api/browser-automation/sessions/{id}/logs` | 用户端 | path: id / query: direction | 非法 id 400（同 #111） | `src/api/browserAutomation.js` | 93 |
| 118 | [x] | GET | `/api/browser-automation/sessions/{id}/export` | 用户端 | path: id | 非法 id 400（同 #111） | `src/api/browserAutomation.js` | 97 |
| 119 | [x] | GET | `/api/browser-automation/tasks/{taskId}/receipts` | 用户端 | path: taskId | 200 code=0 list | `src/api/browserAutomation.js` | 102 |
| 120 | [x] | GET | `/api/browser-automation/cron` | 用户端 | - | 200 code=0 list | `src/api/browserAutomation.js` | 107 |
| 121 | [x] | POST | `/api/browser-automation/cron` | 用户端 | body: { name, task_id, cron_expr } | 200 建 id=62 next_run_at 正确；非 cron 任务 400「仅 cron 类型任务可配置定时触发器」 | `src/api/browserAutomation.js` | 110 |
| 122 | [x] | PUT | `/api/browser-automation/cron/{id}` | 用户端 | path: id / body: data | 200 更新成功 next_run_at 重算 | `src/api/browserAutomation.js` | 113 |
| 123 | [x] | DELETE | `/api/browser-automation/cron/{id}` | 用户端 | path: id | 200「已删除」 | `src/api/browserAutomation.js` | 116 |
| 124 | [x] | POST | `/api/browser-automation/cron/{id}/enable` | 用户端 | path: id | 200「已启用」 | `src/api/browserAutomation.js` | 119 |
| 125 | [x] | POST | `/api/browser-automation/cron/{id}/disable` | 用户端 | path: id | 200「已停用」 | `src/api/browserAutomation.js` | 122 |
| 126 | [x] | GET | `/api/browser-automation/host/status` | 用户端 | - | 200 online=false hosts=[]（浏览器 host 未连接的真实状态） | `src/api/browserAutomation.js` | 127 |
| 127 | [x] | POST | `/api/browser-automation/host/token/reset` | 用户端 | - | 200 轮换 bh_21_… 旧 token 灰度可用 | `src/api/browserAutomation.js` | 130 |
| 128 | [x] | GET | `/api/browser-automation/platforms` | 用户端 | - | 200 平台能力表（xiaohongshu/douyin/xianyu） | `src/api/browserAutomation.js` | 135 |
| 129 | [x] | GET | `/api/browser-automation/platforms/{id}/locators` | 用户端 | path: id | 200 定位表 JSON | `src/api/browserAutomation.js` | 138 |
| 130 | [x] | GET | `/api/whatsapp/templates` | 用户端 | query: params | 200 获取模板成功 | `src/api/bulkMessaging.js` | 4 |
| 131 | [x] | POST | `/api/whatsapp/templates` | 用户端 | body: { name, content } | 200 建 tmpl_… | `src/api/bulkMessaging.js` | 8 |
| 132 | [x] | PUT | `/api/whatsapp/templates/{id}` | 用户端 | path: id / body: data | 200 更新成功 | `src/api/bulkMessaging.js` | 12 |
| 133 | [x] | DELETE | `/api/whatsapp/templates/{id}` | 用户端 | path: id | 200「删除模板成功」 | `src/api/bulkMessaging.js` | 16 |
| 134 | [x] | POST | `/api/whatsapp/group-messaging/send` | 用户端 | body: { lead_ids, template_id } | 200 入队 queue_id（sent=0/failed=1 因无 WhatsApp 连接，队列状态机正确）；缺参 400 提示准确 | `src/api/bulkMessaging.js` | 20 |
| 135 | [x] | GET | `/api/whatsapp/group-messaging/status/{queueId}` | 用户端 | path: queueId | 200 状态查询成功（不存在队列返回空态） | `src/api/bulkMessaging.js` | 24 |
| 136 | [x] | GET | `/api/whatsapp/group-messaging/records` | 用户端 | query: params | 200 发送记录列表 | `src/api/bulkMessaging.js` | 28 |
| 137 | [x] | GET | `/api/capabilities` | 用户端 | - | 200 能力矩阵（platform/capability/via/ready） | `src/api/capabilityDiagnostics.js` | 12 |
| 138 | [x] | GET | `/api/channel-agent-bindings` | 用户端 | query: channel_type + account_id 必填 | 200 list；缺参 400「channel_type 和 account_id 必填」 | `src/api/channelAgentBinding.js` | 4 |
| 139 | [x] | GET | `/api/channel-agent-bindings/by-agent/{agentId}` | 用户端 | path: agentId | 200 list | `src/api/channelAgentBinding.js` | 8 |
| 140 | [x] | POST | `/api/channel-agent-bindings` | 用户端 | body: { channel_type, account_id, agent_id(number) } | 200 建 id=44；重复绑定 409 唯一键拦截；agent_id 传字符串会被 bind 拒绝 | `src/api/channelAgentBinding.js` | 12 |
| 141 | [x] | PUT | `/api/channel-agent-bindings/{id}` | 用户端 | path: id / body: 全量必填+PATCH 指针字段 | 200 更新成功 priority=5 | `src/api/channelAgentBinding.js` | 16 |
| 142 | [x] | DELETE | `/api/channel-agent-bindings/{id}` | 用户端 | path: id | 200「删除成功」 | `src/api/channelAgentBinding.js` | 20 |
| 143 | [x] | GET | `/api/chat-channels` | 用户端 | query: params | 200 list | `src/api/chatChannel.js` | 4 |
| 144 | [x] | GET | `/api/chat-channels/{channelId}` | 用户端 | path: channelId | 200 详情 | `src/api/chatChannel.js` | 8 |
| 145 | [x] | POST | `/api/chat-channels` | 用户端 | body: { channel_name, allowed_origins, welcome_message } | 200 建 id=43 自动生成 channel_id/app_key | `src/api/chatChannel.js` | 12 |
| 146 | [x] | PUT | `/api/chat-channels/{channelId}` | 用户端 | path: channelId / body: data | 200「已更新」 | `src/api/chatChannel.js` | 16 |
| 147 | [x] | POST | `/api/chat-channels/{channelId}/rotate-key` | 用户端 | path: channelId | 200 AppKey 已轮换 | `src/api/chatChannel.js` | 20 |
| 148 | [x] | POST | `/api/chat-channels/{channelId}/reset-secret` | 用户端 | path: channelId | 200 AppSecret 已重置（仅返回一次） | `src/api/chatChannel.js` | 28 |
| 149 | [x] | POST | `/api/chat-channels/{channelId}/reset-secret` | 用户端 | path: channelId | 200 同 #148（重复行，同一实现） | `src/api/chatChannel.js` | 28 |
| 150 | [x] | GET | `/api/churn/prediction` | 用户端 | query: params（user_id 必填，缺失 400） | 200 code=0 无记录时返回零值结构 | `src/api/churnPrediction.js` | 4 |
| 151 | [x] | GET | `/api/churn-prediction` | 用户端 | query: params | 200 code=0 分页 list（忽略 user_id，全量列表） | `src/api/churnPrediction.js` | 7 |
| 152 | [x] | GET | `/api/churn-prediction/users` | 用户端 | query: params | 200 code=0 数组（tenant_id=1 数据，churn_score/risk_factors） | `src/api/churnPrediction.js` | 10 |
| 153 | [x] | GET | `/api/churn-prediction/warnings` | 用户端 | query: params | 200 code=0 分页 list（warning_level/type/description/suggestion） | `src/api/churnPrediction.js` | 13 |
| 154 | [x] | GET | `/api/churn/unhandled-warnings` | 用户端 | query: params | 200 code=0 分页 list（含已处理前的 id=256） | `src/api/churnPrediction.js` | 16 |
| 155 | [x] | POST | `/api/churn/warnings/{id}/handle` | 用户端 | path: id / body: data | 200 标记成功（id=256 已打标） | `src/api/churnPrediction.js` | 19 |
| 156 | [x] | GET | `/api/churn-prediction/model-config` | 用户端 | - | 200 code=0 权重/阈值配置完整 | `src/api/churnPrediction.js` | 22 |
| 157 | [x] | POST | `/api/churn-prediction/model-config` | 用户端 | body: data | 200 保存成功，回读值不变 | `src/api/churnPrediction.js` | 25 |
| 158 | [x] | GET | `/api/churn-prediction/statistics` | 用户端 | query: start_date/end_date 必填（缺失 400） | 200 code=0 按日统计（total/churn/high/critical） | `src/api/churnPrediction.js` | 28 |
| 159 | [x] | GET | `/api/churn/risk-distribution` | 用户端 | - | 200 code=0 {critical:103,high:18,low:0,medium:0} | `src/api/churnPrediction.js` | 31 |
| 160 | [x] | POST | `/api/churn/warnings/intervene` | 用户端 | body: data（intervention_type 必填，缺失 400） | 200 干预成功，返回 warning_id | `src/api/churnPrediction.js` | 38 |
| 161 | [x] | POST | `/api/user-segment/rfm/calculate` | 用户端 | body: data | 200 计算完成 churn_updated_count=120 updated_count=20（同步阻塞约数秒） | `src/api/churnPrediction.js` | 50 |
| 162 | [x] | DELETE | `/api/clues/delete/{id}` | 用户端 | path: id | 200 删除成功（total 8→7）；重复删除 404 线索不存在 | `src/api/clue.js` | 5 |
| 163 | [x] | GET | `/api/clues/list?{q}` | 用户端 | query: page/limit/filters | 200 code=0 total=8 分页 list（account/type/is_verify/create_time） | `src/api/clue.js` | 12 |
| 164 | [x] | GET | `/api/clues/statistics` | 用户端 | - | 200 code=0 按 type 聚合（total/verify_total） | `src/api/clue.js` | 15 |
| 165 | [x] | POST | `/api/clues/import` | 用户端 | body: 顶层数组 [{name,account,type:"9",is_verify:1}]（is_verify 必须数字） | 200 success_count=1/skip_count=0；空数组/缺字段/字符串布尔均 400 提示准确；type=999 400 指向 /api/clues/type；测试数据已清理 | `src/api/clue.js` | 18 |
| 166 | [x] | GET | `/api/system/stats` | 用户端 | - | 200 cpu/disk/memory/uptime/total_users=57/total_cards=81/total_short_links=167 | `src/api/cockpit.js` | 4 |
| 167 | [x] | GET | `/api/monitor/health` | 用户端 | - | 200 pending_count=74/oldest_pending_min=96402/sync_gap_count=1/stuck_reachable=74 | `src/api/cockpit.js` | 8 |
| 168 | [x] | GET | `/api/monitor/node-health` | 用户端 | - | 200 nodes[]（agent_turn/ingest 等，abnormal_rate=0） | `src/api/cockpit.js` | 12 |
| 169 | [x] | GET | `/api/monitor/anomalies` | 用户端 | query: limit | 200 sync_gap/stuck_reachable 分组明细（GET 带 limit 即可，body 参数文档与实现不符以实测为准） | `src/api/cockpit.js` | 16 |
| 170 | [x] | GET | `/api/ai/sales-cockpit` | 用户端 | - | 200 channelHealth/intentDistribution/llmRoutes（deepseek sop_reply） | `src/api/cockpit.js` | 24 |
| 171 | [x] | GET | `/api/community/groups` | 用户端 | query: params（page/limit/search） | 200 total=22 分页 list | `src/api/community.js` | 5 |
| 172 | [x] | POST | `/api/community/groups` | 用户端 | body: data | 200 建群返回 id；空名 400 name 不能为空 | `src/api/community.js` | 9 |
| 173 | [x] | GET | `/api/community/groups/{id}` | 用户端 | path: id | 200 详情回读一致 | `src/api/community.js` | 13 |
| 174 | [x] | PUT | `/api/community/groups/{id}` | 用户端 | path: id / body: data | 200 更新成功（改名 v2 回读生效） | `src/api/community.js` | 17 |
| 175 | [x] | DELETE | `/api/community/groups/{id}` | 用户端 | path: id | 200 删除成功（测试群已清理） | `src/api/community.js` | 21 |
| 176 | [x] | GET | `/api/community/members` | 用户端 | query: params（group_id/page） | 200 分页 list（测试群 0 成员） | `src/api/community.js` | 25 |
| 177 | [x] | GET | `/api/community/messages` | 用户端 | query: params（group_id/page） | 200 分页 list | `src/api/community.js` | 29 |
| 178 | [x] | GET | `/api/community/stats` | 用户端 | query: params | 200 total_groups/total_members/active_groups 汇总 | `src/api/community.js` | 33 |
| 179 | [x] | POST | `/api/community/import` | 用户端 | body: {"groups":[{name,description}]}（members 字段不支持） | 200 success_count=1；空 groups 400；同名重复导入会产生两个同名群（CreateGroup 无唯一约束，与手动创建行为一致，非缺陷，观察项） | `src/api/community.js` | 37 |
| 180 | [x] | POST | `/api/community/export` | 用户端 | body: data | 200 返回 groups 全量数组（按 group_id 过滤未生效时导全量，前端自取） | `src/api/community.js` | 41 |
| 181 | [x] | GET | `/api/manage/config-params` | 管理端 | query: group, include_deprecated | 200 参数数组（group/key/value/default/min/max/read_only） | `src/api/configParams.js` | 8 |
| 182 | [x] | GET | `/api/manage/config-params/groups` | 管理端 | - | 200 39 个分组元数据（中文 label/sort_order） | `src/api/configParams.js` | 17 |
| 183 | [x] | GET | `/api/manage/config-params/audit-logs` | 管理端 | query: limit | 200 审计记录（old_value/new_value） | `src/api/configParams.js` | 21 |
| 184 | [x] | PUT | `/api/manage/config-params/{group}/{key}` | 管理端 | path: group, key / body: { value } | 200 更新→回读生效；越界值 400 "value 50 > max 10"（HTTP 码也是 200 包装，见观察项）；非法值 400 | `src/api/configParams.js` | 25 |
| 185 | [x] | POST | `/api/manage/config-params/{group}/{key}/reset` | 管理端 | path: group, key | 200 重置回 default（2.6→2.5 回读确认） | `src/api/configParams.js` | 29 |
| 186 | [x] | POST | `/api/manage/config-params/{group}/reset` | 管理端 | path: group | 200 整组重置（pagination 组验证） | `src/api/configParams.js` | 33 |
| 187 | [x] | PUT | `/api/manage/platform-host` | 管理端 | body: { address } | 200 保存成功"约60秒内生效"；空地址 400 引导用参数中心重置（已用 reset 恢复）；GET 该路径 405（只有 PUT，查询走 connectivity） | `src/api/configParams.js` | 47 |
| 188 | [x] | POST | `/api/manage/platform-host/connectivity` | 管理端 | body: { address \|\| '' } | 200 探测结果（source=request/config_param/empty；PLATFORM_ENABLED 未开启时 reachable=false 并说明） | `src/api/configParams.js` | 56 |
| 189 | [x] | GET | `/api/contacts/{customerId}/consents` | 用户端 | path: customerId / query: params（limit） | 200 分页 list（decision/purpose/actor/occurred_at）；不存在的客户返回空列表（不 404） | `src/api/contactConsent.js` | 12 |
| 190 | [x] | POST | `/api/contacts/{customerId}/consents` | 用户端 | path: customerId / body: { decision: grant/revoke, purpose: marketing/support/transactional, channel, scene, remark } | 200 已记录 actor=user:54；grant→revoke 时间线追加 2 条；非法 decision/purpose 400 文案准确 | `src/api/contactConsent.js` | 16 |
| 191 | [x] | GET | `/api/contacts/{customerId}/preferences` | 用户端 | path: customerId | 200 分页 list（purpose/paused/timezone） | `src/api/contactConsent.js` | 20 |
| 192 | [x] | PUT | `/api/contacts/{customerId}/preferences` | 用户端 | path: customerId / body: { purpose 必填, paused, paused_until, timezone } | 200 已更新回读生效；缺 purpose 400；测试后已还原 paused=false | `src/api/contactConsent.js` | 24 |
| 193 | [x] | GET | `/api/conversion-funnels` | 用户端 | query: params（start_time/end_time 可选，默认近30天） | 200 stages 全漏斗（visit/clue/intent/session/opportunity…count/rate/drop_rate） | `src/api/conversionFunnel.js` | 5 |
| 194 | [x] | GET | `/api/conversion-funnels/stage` | 用户端 | query: stage | 200 单阶段明细（avg_duration/degraded/top_sources）；未知 stage 返回空结构不报错 | `src/api/conversionFunnel.js` | 8 |
| 195 | [x] | POST | `/api/customer-sessions/{sessionId}/csat` | 用户端 | path: sessionId / body: { score 1-5, comment } | 200 评分已提交 status=responded；越界分 400 "score 必须在 1-5"；不存在会话 404；已响应后重复提交会覆盖评分（幂等更新，非缺陷） | `src/api/csat.js` | 4 |
| 196 | [x] | GET | `/api/csat/stats` | 用户端 | query: params | 200 avg_score/distribution/negative_count/positive_rate/responded/threshold | `src/api/csat.js` | 7 |
| 197 | [x] | GET | `/api/csat/trend` | 用户端 | query: params | 200 按日 list（avg_score/count/date） | `src/api/csat.js` | 10 |
| 198 | [x] | GET | `/api/csat/negative` | 用户端 | query: params | 200 threshold=3 负评 list（当前为空） | `src/api/csat.js` | 13 |
| 199 | [x] | POST | `/api/customer-sessions/{sessionId}/csat/trigger` | 用户端 | path: sessionId / body: {} | 200 调查已触发 status=sent；不存在会话 404 文案准确 | `src/api/csat.js` | 16 |
| 200 | [x] | GET | `/api/csat/template` | 用户端 | - | 200 {content,low_threshold:3} | `src/api/csat.js` | 18 |
| 201 | [x] | PUT | `/api/csat/template` | 用户端 | body: data | 200 模板已保存回读一致；low_threshold=9 400 "必须是 1-5 之间的数字" | `src/api/csat.js` | 19 |
| 202 | [x] | GET | `/api/custom-tools` | 用户端 | - | 200 数组（初始空，导入后 tool_count=2） | `src/api/customIntegration.js` | 6 |
| 203 | [x] | POST | `/api/custom-tools/import` | 用户端 | body: { name, base_url, schema_json(OpenAPI 3 JSON 字符串) } | 200 导入成功 tool_count=2；缺字段 400；环回地址 SSRF 防护 400 "指向环回/链路本地地址，拒绝导入"；JSON 解析失败 400 | `src/api/customIntegration.js` | 10 |
| 204 | [x] | DELETE | `/api/custom-tools/{id}` | 用户端 | path: id | 200 deleted；重复删除 404 工具集不存在 | `src/api/customIntegration.js` | 14 |
| 205 | [x] | GET | `/api/mcp-servers` | 用户端 | - | 200 数组 | `src/api/customIntegration.js` | 18 |
| 206 | [x] | POST | `/api/mcp-servers` | 用户端 | body: { name, endpoint, description } | 200 已保存（默认 enabled=true/timeout 30s/fail_limit 3） | `src/api/customIntegration.js` | 22 |
| 207 | [x] | DELETE | `/api/mcp-servers/{id}` | 用户端 | path: id | 200 已删除；重复删除 404 MCP 配置不存在 | `src/api/customIntegration.js` | 26 |
| 208 | [x] | PUT | `/api/mcp-servers/{id}/enabled` | 用户端 | path: id / body: { enabled } | 200 已更新 enabled=false 回读生效 | `src/api/customIntegration.js` | 30 |
| 209 | [x] | GET | `/api/mcp-servers/{id}/test` | 用户端 | path: id | 502 UPSTREAM_FAILURE_5005 连通性测试失败（不可达端点，错误分类正确；外网可达端点会成功） | `src/api/customIntegration.js` | 34 |
| 210 | [x] | GET | `/api/custom-reports` | 用户端 | query: params（page/page_size） | 200 分页 list | `src/api/customReport.js` | 4 |
| 211 | [x] | GET | `/api/custom-reports/{id}` | 用户端 | path: id | 200 详情（dimensions/metrics 为 JSON 字符串） | `src/api/customReport.js` | 7 |
| 212 | [x] | POST | `/api/custom-reports` | 用户端 | body: { name, data_source(sessions/messages/orders/clues/users/rfm/agents), chart_type(bar/line/pie/table/card), dimensions[], metrics[] } | 200 创建成功；非法 data_source 400 "不支持的数据源类型"；指标字段限 session_count/message_count | `src/api/customReport.js` | 10 |
| 213 | [x] | PUT | `/api/custom-reports/{id}` | 用户端 | path: id / body: data（需全量字段，同 create） | 200 更新成功回读生效（name/chart_type 变更确认） | `src/api/customReport.js` | 13 |
| 214 | [x] | DELETE | `/api/custom-reports/{id}` | 用户端 | path: id | 200 删除成功 | `src/api/customReport.js` | 16 |
| 215 | [x] | GET | `/api/custom-reports/templates` | 用户端 | - | 200 数组（当前空） | `src/api/customReport.js` | 19 |
| 216 | [x] | POST | `/api/custom-reports/templates/{id}/use` | 用户端 | path: id | 404 "报表不存在（模板 id=999）"（无公共模板数据，404 路径验证通过） | `src/api/customReport.js` | 22 |
| 217 | [x] | GET | `/api/custom-reports/{id}/data` | 用户端 | path: id / query: params | 200 dimensions/metrics/data 聚合（发现缺陷 D-17 已修，见下） | `src/api/customReport.js` | 25 |
| 218 | [x] | GET | `/api/custom-reports/{id}/data?format=export` | 用户端 | path: id | 200 同 #217（format 参数不改变查询行为，仅前端语义） | `src/api/customReport.js` | 41 |
| 219 | [x] | GET | `/api/custom-reports/{id}/data?format=run` | 用户端 | path: id | 200 同 #217 | `src/api/customReport.js` | 44 |

**D-17 缺陷（本轮新发现并已修复）**：`internal/ops/service/custom_report.go` querySessionData 的维度分支缺 `platform` case，platform 维度静默落到 default 的 status 列，把「平台分布」画成「会话状态分布」（ai_handling/closed/escalated/pending）。修复：补 `case "platform": groupExpr = COALESCE(NULLIF(platform,''),'unknown')`，与 queryMessageData/report_filter.go 白名单对齐。ops/service 测试全绿（POSTGRES_TEST_PORT=8202 POSTGRES_TEST_PASSWORD=容器密码），重建二进制后复测：platform 维度返回 douyin=121/web_embed=163/xiaohongshu=19 等，与 customer_sessions 直查完全一致。
| 220 | [x] | GET | `/api/customer-360/basic?user_id={id}` | 用户端 | path: id / body: undefined; { signal } | 200 code=0 basic_info+user_profile（douyin/low/silent） | `src/api/customer360.js` | 4 |
| 221 | [x] | GET | `/api/customer-360/events` | 用户端 | query: params / body: { signal } | 200 code=0 events=[] total=0 | `src/api/customer360.js` | 7 |
| 222 | [x] | GET | `/api/customer-360/sessions` | 用户端 | query: params / body: { signal } | 200 code=0 session_history total=1 | `src/api/customer360.js` | 10 |
| 223 | [x] | GET | `/api/customer-360/tags?user_id={id}` | 用户端 | path: id / body: undefined; { signal } | 200 code=0 tags 数组（17 个 auto_*） | `src/api/customer360.js` | 13 |
| 224 | [x] | GET | `/api/customer-360/orders` | 用户端 | query: params / body: { signal } | 400 缺 user_id（1001）；带 user_id=1 → orders=[] | `src/api/customer360.js` | 17 |
| 225 | [x] | GET | `/api/customer-360/list` | 用户端 | query: params | 200 code=0 map 结构（r53user3 等，含 session_stats） | `src/api/customer360.js` | 43 |
| 226 | [x] | GET | `/api/customer-360/basic?user_id={id}` | 用户端 | path: id | 同 #220（别名，前端第二处封装）200 code=0 | `src/api/customer360.js` | 46 |
| 227 | [x] | GET | `/api/customer-360/tags?user_id={id}` | 用户端 | path: id | 同 #223（别名）200 code=0 | `src/api/customer360.js` | 64 |
| 228 | [x] | PUT | `/api/customer-360/tags?user_id={id}` | 用户端 | path: id / body: { tags } | 200 更新成功（全量替换语义；实测后已还原原 17 个标签） | `src/api/customer360.js` | 73 |
| 229 | [x] | PUT | `/api/customer-360/tags?user_id={id}` | 用户端 | path: id / body: { tags } | 200 清空语义 tags=[]（不存在用户 upsert 创建）；tags 非数组 1001 提示格式 | `src/api/customer360.js` | 78 |
| 230 | [x] | POST | `/api/events/track` | 用户端 | body: data | 200 追踪成功（customer_id+event_type 必填；缺参 1001 提示文案明确） | `src/api/customerEvent.js` | 4 |
| 231 | [x] | POST | `/api/events/pageview` | 用户端 | body: data | 200 页面浏览事件追踪成功（customer_id+url 必填） | `src/api/customerEvent.js` | 7 |
| 232 | [x] | POST | `/api/events/click` | 用户端 | body: data | 200 点击事件追踪成功（customer_id+element 必填） | `src/api/customerEvent.js` | 10 |
| 233 | [x] | POST | `/api/events/purchase` | 用户端 | body: data | 200 购买事件追踪成功（amount 必填） | `src/api/customerEvent.js` | 13 |
| 234 | [x] | POST | `/api/events/signup` | 用户端 | body: data | 200 注册事件追踪成功 | `src/api/customerEvent.js` | 16 |
| 235 | [x] | POST | `/api/events/login` | 用户端 | body: data | 200 登录事件追踪成功 | `src/api/customerEvent.js` | 19 |
| 236 | [x] | POST | `/api/events/add-to-cart` | 用户端 | body: data | 200 加购事件追踪成功（product_id+price 非零必填，quantity 可选；缺 price 400 文案准确） | `src/api/customerEvent.js` | 22 |
| 237 | [x] | GET | `/api/events/customer/{customerId}` | 用户端 | path: customerId / query: params | 200 code=0 limit 生效（实测 7 条事件，类型齐全） | `src/api/customerEvent.js` | 25 |
| 238 | [x] | GET | `/api/events/stats` | 用户端 | query: params | 200 code=0 total_events/by_event_type 聚合正确（与写入一致） | `src/api/customerEvent.js` | 28 |
| 239 | [x] | DELETE | `/api/events/customer/{id}` | 用户端 | path: id | 200 deleted_count=7；重复删 deleted_count=0（幂等） | `src/api/customerEvent.js` | 40 |
| 240 | [x] | GET | `/api/customer-events/list` | 用户端 | query: params | 200 list/total/page/page_size；event_type 过滤生效；page_size 夹取 ≤200；on 模式 tenant_id=5 隔离正确 | `src/api/customerEvent.js` | 44 |
| 241 | [x] | GET | `/api/customer-journey/overview` | 用户端 | - | 200 total_customers=4 + 10 阶段分布（stranger/lead/contact/interested/quoted/won/after_sale/repurchase/sleeping/lost） | `src/api/customerJourney.js` | 4 |
| 242 | [x] | GET | `/api/customer-journey/overview?customer_id={customerId}` | 用户端 | path: customerId | 200 单客户视图 current_stage/stage_history/total_touches（transition 后历史正确追加） | `src/api/customerJourney.js` | 8 |
| 243 | [x] | GET | `/api/customer-journey/stages` | 用户端 | - | 200 10 阶段配置（owner_role/allow_ai_handle/recommended_sop） | `src/api/customerJourney.js` | 12 |
| 244 | [x] | GET | `/api/customer-journey/by-stage?stage={stage}` | 用户端 | path: stage | 200 count/customer_ids；未知 stage 400 列出 10 个可用阶段 | `src/api/customerJourney.js` | 16 |
| 245 | [x] | POST | `/api/customer-journey/transition` | 用户端 | body: data | 200 迁移成功（customer_id+to_stage 必填；stranger→lead，operator_id=user:54；缺参 400） | `src/api/customerJourney.js` | 20 |
| 246 | [x] | POST | `/api/customer-journey/touch` | 用户端 | body: data | 200 互动已记录 touched=true（customer_id 必填；缺参 400） | `src/api/customerJourney.js` | 24 |
| 247 | [x] | POST | `/api/agents` | 用户端 | body: data | 200 创建坐席（agent_id 坐席号正整数+agent_name 必填，max_sessions 1~50；重复坐席号 409） | `src/api/customerService.js` | 4 |
| 248 | [x] | GET | `/api/agents/{id}` | 用户端 | path: id | 200 详情；**:id 是坐席号 agent_id 非主键 id**（传主键 22 → 404，传 9901 → 成功） | `src/api/customerService.js` | 7 |
| 249 | [x] | GET | `/api/agents/online` | 用户端 | - | 200 在线坐席数组（上线后出现在列表，下线后消失） | `src/api/customerService.js` | 10 |
| 250 | [x] | GET | `/api/agents/all` | 用户端 | - | 200 全量坐席（含 admin online/active_sessions 等统计） | `src/api/customerService.js` | 13 |
| 251 | [x] | PUT | `/api/agents/{id}/status` | 用户端 | path: id / body: data | 200 更新成功（status 限 online/busy/offline，max_sessions 1~50；非法状态 400；:id=坐席号） | `src/api/customerService.js` | 16 |
| 252 | [x] | POST | `/api/agents/{id}/online` | 用户端 | path: id | 200 上线成功（:id=坐席号） | `src/api/customerService.js` | 19 |
| 253 | [x] | POST | `/api/agents/{id}/offline` | 用户端 | path: id | 200 下线成功（有活跃会话时拒绝下线，业务校验存在） | `src/api/customerService.js` | 22 |
| 254 | [x] | GET | `/api/agents/{id}/sessions` | 用户端 | path: id | 200 活跃会话数组（:id=坐席号；无会话返回空数组） | `src/api/customerService.js` | 25 |
| 255 | [x] | GET | `/api/agents/me` | 用户端 | - | 200 当前登录用户坐席身份（agent_id=54=登录用户 ID，自动建档接入登录态） | `src/api/customerService.js` | 28 |
| 256 | [x] | GET | `/api/quick-replies` | 用户端 | query: params | 200 code=0 list（25 条历史 auto_* 数据）；query page/page_size 生效 | `src/api/customerService.js` | 32 |
| 257 | [x] | GET | `/api/quick-replies/categories` | 用户端 | - | 200 code=0 分类数组（去重 category 列表） | `src/api/customerService.js` | 35 |
| 258 | [x] | POST | `/api/quick-replies` | 用户端 | body: data | 200 创建 id=87；注意：POST 传 is_public:false 被忽略，创建一律公开（service 注释明确"创建口径一律公开"，更新才支持 PATCH 语义，非缺陷） | `src/api/customerService.js` | 38 |
| 259 | [x] | PUT | `/api/quick-replies/{id}` | 用户端 | path: id / body: data | 200 更新成功（is_public 可改 false；category/title/content 全量必填） | `src/api/customerService.js` | 41 |
| 260 | [x] | DELETE | `/api/quick-replies/{id}` | 用户端 | path: id | 200 删除成功；重复删 404 NOT_FOUND_1002 | `src/api/customerService.js` | 44 |
| 261 | [x] | GET | `/api/session-tags` | 用户端 | - | 200 code=0 数组（含 name/code/group/color/rule_condition） | `src/api/customerService.js` | 48 |
| 262 | [x] | POST | `/api/session-tags` | 用户端 | body: data | 400 name+code 均必填（缺 code 明确报错）→ 200 创建 id=75 | `src/api/customerService.js` | 51 |
| 263 | [x] | PUT | `/api/session-tags/{id}` | 用户端 | path: id / body: data | 200 更新成功（name/color 可改，code 必填保持） | `src/api/customerService.js` | 54 |
| 264 | [x] | DELETE | `/api/session-tags/{id}` | 用户端 | path: id | 200 删除成功；重复删 404 "会话标签不存在: 75" | `src/api/customerService.js` | 57 |
| 265 | [x] | GET | `/api/ai-suggestions/{sessionId}` | 用户端 | path: sessionId | 200 code=0 data=[]（不存在会话返回空数组不报错，双模式一致） | `src/api/customerService.js` | 61 |
| 266 | [x] | POST | `/api/ai-suggestions/{id}/use` | 用户端 | path: id | 200 同路径与 #265 路由一致（前端 use 标记；无建议数据，空数组路径验证） | `src/api/customerService.js` | 64 |
| 267 | [x] | POST | `/api/customer-sessions/{sessionId}/tags` | 用户端 | path: sessionId / body: data | 400 tags 必须非空字符串数组 {"tags":["vip"]}（tag_ids 字段名不对）→ 不存在会话 404 → 真实会话 779 标记成功（tags=["apitest","vip"]） | `src/api/customerService.js` | 68 |
| 268 | [x] | GET | `/api/customer-sessions` | 用户端 | query: params | 200 code=0 data={list,page,page_size,total}（total=1037+）；status 参数白名单 pending/ai_handling/human_handling/waiting/resolved/closed（传 active 400 列出可用值） | `src/api/customerSession.js` | 4 |
| 269 | [x] | GET | `/api/customer-sessions/{id}/messages` | 用户端 | path: id / query: params | 200 code=0 data={list,total}；空会话 total=0 | `src/api/customerSession.js` | 7 |
| 270 | [x] | POST | `/api/customer-sessions/{data_sessionId}/messages` | 用户端 | path: data_sessionId / body: { content: data.content, sender_type: data.sender_type \|\| 'agent', sender_name: data.sender_name \|\| '客服', sender_id: String(data.sender_id \|\| '') } | 200 发送成功（msg id=2170，sender_name 覆盖为登录用户名）；#269 复查 total=1 | `src/api/customerSession.js` | 10 |
| 271 | [x] | POST | `/api/customer-sessions` | 用户端 | body: data | 200 创建 id=779 sess_..._6bfbc21e（platform/account_id/user_id/user_name，初始 status=pending） | `src/api/customerSession.js` | 18 |
| 272 | [x] | POST | `/api/customer-sessions/{id}/close` | 用户端 | path: id | 200 关闭成功；重复关闭幂等成功（closed 状态再关不报错） | `src/api/customerSession.js` | 21 |
| 273 | [x] | POST | `/api/customer-sessions/{id}/transfer` | 用户端 | path: id / body: data | 400 new_agent_id 必填（target_agent_id 字段名不对）；目标客服离线 400；坐席在线后 200 转接成功（new_agent_id=坐席的用户主键） | `src/api/customerSession.js` | 24 |
| 274 | [x] | POST | `/api/customer-sessions/{id}/takeover` | 用户端 | path: id / body: { reason } | 200 接管成功 data={handler_type:human,session_id} | `src/api/customerSession.js` | 28 |
| 275 | [x] | POST | `/api/customer-sessions/{id}/release` | 用户端 | path: id | 200 释放成功 data={handler_type:ai,session_id} | `src/api/customerSession.js` | 32 |
| 276 | [x] | POST | `/api/customer-sessions/{id}/switch-handler` | 用户端 | path: id / body: { handler_type: handlerType, reason } | 200 切换成功（human→ai 状态流转） | `src/api/customerSession.js` | 36 |
| 277 | [x] | POST | `/api/customer-sessions/{id}/blacklist` | 用户端 | path: id / body: { reason, ttl_hours: ttlHours } | 200 拉黑成功 data={blacklisted:true}（ttl_hours=24 → expires_at 次日） | `src/api/customerSession.js` | 40 |
| 278 | [x] | POST | `/api/customer-sessions/blacklist/remove` | 用户端 | body: { user_id: userId, platform } | 200 解除拉黑成功；复查 blacklisted=false | `src/api/customerSession.js` | 44 |
| 279 | [x] | GET | `/api/customer-sessions/blacklist/check` | 用户端 | query: user_id, platform | 200 data={blacklisted:true}（拉黑期间）；解除后 false | `src/api/customerSession.js` | 48 |
| 280 | [x] | GET | `/api/customer-sessions/blacklist` | 用户端 | query: params | 200 code=0 list（含 user_id/platform/reason/source/operator/active/expires_at） | `src/api/customerSession.js` | 52 |
| 281 | [x] | GET | `/api/customer-360?user_id={id}` | 用户端 | path: id | 200 customer-360 聚合视图（basic_info/interaction_stats/session_history） | `src/api/customerSession.js` | 56 |
| 282 | [x] | GET | `/api/customer-360/basic?user_id={id}` | 用户端 | path: id | 200 basic_info（user_id/user_name/first_seen_at/last_seen_at/source_platform） | `src/api/customerSession.js` | 59 |
| 283 | [x] | GET | `/api/customer-360/stats?user_id={id}` | 用户端 | path: id | 200 interaction_stats（total_interactions=1 平台分布 last_7d/last_30d） | `src/api/customerSession.js` | 62 |
| 284 | [x] | GET | `/api/customer-360/sessions?user_id={id}` | 用户端 | path: id | 200 session_history（本测试会话 closed message_count=1） | `src/api/customerSession.js` | 65 |
| 285 | [x] | GET | `/api/customer-360/tags?user_id={id}` | 用户端 | path: id | 200 data={tags:[],user_id} | `src/api/customerSession.js` | 68 |
| 286 | [x] | GET | `/api/dashboards` | 用户端 | query: params | 200 data={list,...}（租户内大屏列表，含 code/layout/theme/view_count） | `src/api/dashboardScreen.js` | 4 |
| 287 | [x] | GET | `/api/dashboards/{id}` | 用户端 | path: id | 200 详情 id=43 | `src/api/dashboardScreen.js` | 7 |
| 288 | [x] | POST | `/api/dashboards` | 用户端 | body: data | 400 name 必填+layout/widgets 需合法 JSON 结构（layout 传字符串被拒）→ 200 创建 id=43 code=screen_...（layout/widgets 必须是 JSON 对象不是字符串；widgets 传 {} 序列化为空串） | `src/api/dashboardScreen.js` | 10 |
| 289 | [x] | PUT | `/api/dashboards/{id}` | 用户端 | path: id / body: data | 400 类型不符 400（widgets 传 {} 不是数组）→ 200 更新成功（widgets 需 WidgetConfig 数组 [{widget_type,title,config,x,y,width,height}]；theme 可改 dark） | `src/api/dashboardScreen.js` | 13 |
| 290 | [x] | DELETE | `/api/dashboards/{id}` | 用户端 | path: id | 200 删除成功；重复删 404 "大屏不存在（id=43）" | `src/api/dashboardScreen.js` | 16 |
| 291 | [x] | GET | `/api/dashboards/public/{code}` | 用户端 | path: code | is_public=false → 404 "大屏不存在或未公开"；置 public 后 200 返回 screen+widgets | `src/api/dashboardScreen.js` | 19 |
| 292 | [x] | GET | `/api/dashboards/data` | 用户端 | - | 200 获取仪表板数据成功（kpis 数组：总线索/今日线索等） | `src/api/dashboardScreen.js` | 23 |
| 293 | [x] | GET | `/api/dashboards/activities` | 用户端 | - | 200 实时活动数组（type=clue/title/user_name/created_at） | `src/api/dashboardScreen.js` | 26 |
| 294 | [x] | GET | `/api/dashboards/data` | 用户端 | - | 200 同 #292（前端多处调用同端点，query 变体） | `src/api/dashboardScreen.js` | 29 |
| 295 | [x] | GET | `/api/dashboards/data` | 用户端 | query: params | 200 同 #292（range=today 参数被接受） | `src/api/dashboardScreen.js` | 32 |
| 296 | [x] | GET | `/api/dashboards/data` | 用户端 | - | 200 同 #292（screen_id=37 参数被接受） | `src/api/dashboardScreen.js` | 35 |
| 297 | [x] | GET | `/api/dashboards/data` | 用户端 | - | 200 on 模式同结构（tenant 5 数据隔离，总线索=0） | `src/api/dashboardScreen.js` | 38 |
| 298 | [x] | POST | `/api/memory/messages` | 用户端 | body: data | 400 session_id+role+content 均必填（messages 数组形式不对，单条 {session_id,role,content}）→ 200 追加成功 | `src/api/dialogueMemory.js` | 5 |
| 299 | [x] | GET | `/api/memory/short` | 用户端 | query: params | 200 短期记忆数组（role/content/timestamp；AI 打分消息） | `src/api/dialogueMemory.js` | 9 |
| 300 | [x] | GET | `/api/memory/long` | 用户端 | query: params | 200 长期记忆结构（id=531 key_facts/objections/purchase_intent/intent_trail/sop_history） | `src/api/dialogueMemory.js` | 13 |
| 301 | [x] | POST | `/api/memory/facts` | 用户端 | body: data | 400 facts（字符串字典）必填 → 200 更新成功（facts={budget,demand}） | `src/api/dialogueMemory.js` | 17 |
| 302 | [x] | POST | `/api/memory/objections` | 用户端 | body: data | 400 objection_type+content 必填 → 200 记录成功（price/价格太贵） | `src/api/dialogueMemory.js` | 21 |
| 303 | [x] | POST | `/api/memory/purchase-intent` | 用户端 | body: data | 400 level 必填 → 200 更新成功（level=high） | `src/api/dialogueMemory.js` | 25 |
| 304 | [x] | POST | `/api/memory/intent-trail` | 用户端 | body: data | 400 intent_type 必填 → 200 记录成功（product_inquiry+detail） | `src/api/dialogueMemory.js` | 29 |
| 305 | [x] | POST | `/api/memory/sop-history` | 用户端 | body: data | 400 sop_name 必填 → 200 记录成功（sop_name+step+result） | `src/api/dialogueMemory.js` | 33 |
| 306 | [x] | GET | `/api/memory/context` | 用户端 | query: params | 200 组装上下文文本（长期记忆 key_facts/异议/意向 + 最近对话拼接） | `src/api/dialogueMemory.js` | 37 |
| 307 | [x] | GET | `/api/memory/list` | 用户端 | query: params | 200 data={list}（记忆记录列表，与 #300 同结构） | `src/api/dialogueMemory.js` | 41 |
| 308 | [x] | GET | `/api/dingtalk-app/accounts` | 用户端 | query: params | 200 code=0 list（历史数据 id=10，密钥均已掩码 aes_key_masked/app_secret_masked） | `src/api/dingtalkApp.js` | 6 |
| 309 | [x] | GET | `/api/dingtalk-app/accounts/{id}` | 用户端 | path: id | 200 详情 id=12（掩码返回）；不存在 404 | `src/api/dingtalkApp.js` | 10 |
| 310 | [x] | POST | `/api/dingtalk-app/accounts` | 用户端 | body: data | 400 account_name/app_key/app_secret 均必填 → 200 创建 id=12（app_secret 掩码 test****-abc） | `src/api/dingtalkApp.js` | 14 |
| 311 | [x] | PUT | `/api/dingtalk-app/accounts/{id}` | 用户端 | path: id / body: data | 200 更新成功（account_name/token/inbound_enabled 可改；agent_id 未传被清空） | `src/api/dingtalkApp.js` | 18 |
| 312 | [x] | DELETE | `/api/dingtalk-app/accounts/{id}` | 用户端 | path: id | 200 删除成功；重复删 404 NOT_FOUND_1002 | `src/api/dingtalkApp.js` | 22 |
| 313 | [x] | POST | `/api/dingtalk-app/accounts/{id}/test` | 用户端 | path: id | 200 配置校验通过 ok=true（本地校验，无外网调用） | `src/api/dingtalkApp.js` | 26 |
| 314 | [x] | GET | `/api/domain-pool/health` | 用户端 | - | 200 探测完成（逐域名 HEAD 探测，外网不可达 http_error=EOF，action_taken=mark_unhealthy；耗时约 20s+） | `src/api/domainPool.js` | 3 |
| 315 | [x] | GET | `/api/domain-pool/{domain}/blacklist` | 用户端 | path: domain | 200 data={blacklisted:false}（不存在域名也返回 false 不 404） | `src/api/domainPool.js` | 4 |
| 316 | [x] | POST | `/api/domain-pool/{id}/suspend` | 用户端 | path: id / body: {} | 200 域名已停用 status=4 已停用 | `src/api/domainPool.js` | 5 |
| 317 | [x] | POST | `/api/domain-pool/{id}/rotate` | 用户端 | path: id / body: {} | 停用状态 400 "域名已停用，无法轮换激活"；健康检查后 200 已轮换激活 status=1 正常 | `src/api/domainPool.js` | 6 |
| 318 | [x] | GET | `/api/domain-pool/alerts` | 用户端 | query: params | 200 data={list:[...]}（列表即域名健康状态，含 last_check/health_score） | `src/api/domainPool.js` | 7 |
| 319 | [x] | POST | `/api/domain-pool/alerts/{id}/resolve` | 用户端 | path: id / body: {} | 400 "域名仍不健康，告警保持"（业务规则：不健康域名不能解除告警） | `src/api/domainPool.js` | 8 |
| 320 | [x] | GET | `/api/domain-pool` | 用户端 | query: params | 200 data={list}（id=132 s10.example.com status=2 不可访问 consecutive_failures=10） | `src/api/domainPool.js` | 11 |
| 321 | [x] | POST | `/api/domain-pool` | 用户端 | body: data | 200 创建 id=134（health_score 初始 100，dns_resolved=false） | `src/api/domainPool.js` | 12 |
| 322 | [x] | PUT | `/api/domain-pool/{data_id}` | 用户端 | path: data_id / body: data | 400 id 与 domain 必填（body 需含 id=134）→ 200 更新成功（port/purpose 可改） | `src/api/domainPool.js` | 13 |
| 323 | [x] | DELETE | `/api/domain-pool/{id}` | 用户端 | path: id | 200 删除成功；重复删 404 "域名不存在" | `src/api/domainPool.js` | 14 |
| 324 | [x] | POST | `/api/domain-pool/{id}/check` | 用户端 | path: id / body: {} | 200 检查完成 healthy=false（无效域名） | `src/api/domainPool.js` | 15 |
| 325 | [x] | POST | `/api/domain-pool/check-all` | 用户端 | body: {} | 200 检查完成 data 数组逐域名返回 status/msg（外网 EOF 全部不可达，环境阻塞非缺陷） | `src/api/domainPool.js` | 16 |
| 326 | [x] | GET | `/api/douyin/list` | 用户端 | query: params | 200 data={list}（历史卡片 id=61，含 short_link_url/short_code） | `src/api/douyinCard.js` | 4 |
| 327 | [x] | GET | `/api/douyin/{id}` | 用户端 | path: id | 200 详情 id=66 | `src/api/douyinCard.js` | 8 |
| 328 | [x] | POST | `/api/douyin/create` | 用户端 | body: data | 400 title 必填（binding 校验，错误文案已掩码 Go 结构体名）→ 200 创建 id=66（自动生成 short_code=T863xm） | `src/api/douyinCard.js` | 12 |
| 329 | [x] | PUT | `/api/douyin/update` | 用户端 | body: data | 200 更新成功（id 在 body 传；路由 /douyin/update 无 :id 形状，controller 已兼容两种） | `src/api/douyinCard.js` | 16 |
| 330 | [x] | DELETE | `/api/douyin/delete/{id}` | 用户端 | path: id | 200 删除成功；重复删 404 NOT_FOUND_1002 | `src/api/douyinCard.js` | 20 |
| 331 | [x] | GET | `/api/douyin/view/{id}` | 用户端 | path: id | 200 view 递增 view_count | `src/api/douyinCard.js` | 24 |
| 332 | [x] | GET | `/api/douyin/stats/card/{id}` | 用户端 | path: id / query: params | 200 data={cardId,title,viewCount,dailyStats,recentActivity} | `src/api/douyinCard.js` | 28 |
| 333 | [x] | GET | `/api/douyin/stats/overall` | 用户端 | query: params | 200 data={totalCards:26,activeCards,totalViews,popularCards} | `src/api/douyinCard.js` | 32 |
| 334 | [x] | POST | `/api/douyin/{id}/generate-short-link` | 用户端 | path: id | 200 重新生成短链 short_code=Pf9oc1（旧码被替换） | `src/api/douyinCard.js` | 36 |
| 335 | [x] | GET | `/api/egolite/flows` | 用户端 | query: params | 200 data={list:[],total:0}（租户内无流程） | `src/api/egolite.js` | 11 |
| 336 | [x] | GET | `/api/egolite/flows/{id}` | 用户端 | path: id | 200? code=1 "流程不存在"（业务错误码 1 非 HTTP 错误，egolite 风格） | `src/api/egolite.js` | 13 |
| 337 | [x] | POST | `/api/egolite/flows/{id}/rescan` | 用户端 | path: id | code=1 "执行不存在"（流程 id=1 为 draft 状态，rescan 仅对可执行流程） | `src/api/egolite.js` | 19 |
| 338 | [x] | POST | `/api/egolite/flows/{id}/publish` | 用户端 | path: id | code=1 状态机校验（draft 不可 publish） | `src/api/egolite.js` | 21 |
| 339 | [x] | POST | `/api/egolite/flows/{id}/submit` | 用户端 | path: id | code=1 状态机校验（draft 不可 submit） | `src/api/egolite.js` | 23 |
| 340 | [x] | POST | `/api/egolite/flows/{id}/dry-run` | 用户端 | path: id / body: data | code=1 dry-run 需可执行流程 | `src/api/egolite.js` | 26 |
| 341 | [x] | DELETE | `/api/egolite/flows/{id}` | 用户端 | path: id | code=1 "流程不存在" | `src/api/egolite.js` | 28 |
| 342 | [x] | GET | `/api/egolite/flows/{id}/cron` | 用户端 | path: id | code=1 "流程未挂载定时触发器" | `src/api/egolite.js` | 31 |
| 343 | [x] | POST | `/api/egolite/flows/{id}/cron` | 用户端 | path: id / body: data | code=1 "流程不存在或不属于你" | `src/api/egolite.js` | 32 |
| 344 | [x] | DELETE | `/api/egolite/flows/{id}/cron` | 用户端 | path: id | code=1 "流程未挂载定时触发器" | `src/api/egolite.js` | 33 |
| 345 | [x] | GET | `/api/egolite/executions` | 用户端 | query: params | 200 data={list:[],total:0} | `src/api/egolite.js` | 37 |
| 346 | [x] | GET | `/api/egolite/executions/{id}` | 用户端 | path: id | code=1 "执行不存在" | `src/api/egolite.js` | 39 |
| 347 | [x] | GET | `/api/egolite/executions/{id}/events` | 用户端 | path: id / query: params | code=1 "执行不存在" | `src/api/egolite.js` | 42 |
| 348 | [x] | POST | `/api/egolite/executions` | 用户端 | body: data | code=1 "draft 执行必须提供 flow_id"（参数校验正常） | `src/api/egolite.js` | 109 |
| 349 | [x] | POST | `/api/egolite/executions/{id}/abort` | 用户端 | path: id | code=1 "执行不存在" | `src/api/egolite.js` | 111 |
| 350 | [x] | POST | `/api/egolite/executions/{id}/redispatch` | 用户端 | path: id / body: {} | code=1 "执行不存在" | `src/api/egolite.js` | 115 |
| 351 | [x] | POST | `/api/egolite/executions/{id}/confirm` | 用户端 | path: id / body: { allow } | code=1 "执行不存在" | `src/api/egolite.js` | 118 |
| 352 | [x] | GET | `/api/egolite/runner/status` | 用户端 | - | 200 data={count:0,runners:[]}（ego-runner 未连接） | `src/api/egolite.js` | 122 |
| 353 | [x] | GET | `/api/egolite/runner/tokens` | 用户端 | - | 200 data={list:[]} | `src/api/egolite.js` | 125 |
| 354 | [x] | POST | `/api/egolite/runner/tokens` | 用户端 | body: { label } | 200 创建 token id=4 eg_54_2be226…（"令牌只显示这一次"） | `src/api/egolite.js` | 128 |
| 355 | [x] | POST | `/api/egolite/runner/tokens/{id}/revoke` | 用户端 | path: id | 200 已撤销立即失效；tokens 列表复查为空 | `src/api/egolite.js` | 131 |
| 356 | [x] | GET | `/api/egolite/assets` | 用户端 | - | 200 data={list:[]}（租户内无资产） | `src/api/egolite.js` | 136 |
| 357 | [x] | GET | `/api/egolite/admin/assets` | 用户端 | query: params | 200 data={list:[],total:0} | `src/api/egolite.js` | 139 |
| 358 | [x] | GET | `/api/egolite/admin/flows` | 用户端 | query: params | 200 data={list}（admin 视角含 tenant_1 流程 e2e-hello-confirm id=1 draft） | `src/api/egolite.js` | 141 |
| 359 | [x] | POST | `/api/egolite/admin/assets/publish` | 用户端 | body: { flow_id: flowId } | code=1 状态机校验正确（"只有已提交审核（submitted）的流程可发布为资产，当前状态 draft"） | `src/api/egolite.js` | 144 |
| 360 | [x] | POST | `/api/egolite/admin/assets/pack` | 用户端 | body: data | code=1 "资产名称必填" | `src/api/egolite.js` | 146 |
| 361 | [x] | POST | `/api/egolite/admin/assets/{id}/offline` | 用户端 | path: id | code=1 "资产不存在" | `src/api/egolite.js` | 148 |
| 362 | [x] | GET | `/api/egolite/sessions` | 用户端 | query: params | 200 data={list:[],total:0} | `src/api/egolite.js` | 152 |
| 363 | [x] | GET | `/api/egolite/sessions/{id}` | 用户端 | path: id | code=1 "浏览器会话不存在" | `src/api/egolite.js` | 154 |
| 364 | [x] | POST | `/api/egolite/sessions` | 用户端 | body: data | code=1 参数校验（domains 必填，空表示拒绝一切导航）→ 补 domains 后 code=1 "ego-runner 未连接"（环境阻塞：本机无 ego-runner） | `src/api/egolite.js` | 156 |
| 365 | [x] | POST | `/api/egolite/sessions/{id}/round` | 用户端 | path: id / body: data | code=1 "浏览器会话不存在" | `src/api/egolite.js` | 159 |
| 366 | [x] | POST | `/api/egolite/sessions/{id}/observe` | 用户端 | path: id / body: { round } | code=1 "浏览器会话不存在" | `src/api/egolite.js` | 162 |
| 367 | [x] | GET | `/api/egolite/sessions/{id}/rounds` | 用户端 | path: id / query: params | code=1 "浏览器会话不存在" | `src/api/egolite.js` | 165 |
| 368 | [x] | POST | `/api/egolite/sessions/{id}/confirm` | 用户端 | path: id / body: { allow } | code=1 "浏览器会话不存在" | `src/api/egolite.js` | 168 |
| 369 | [x] | POST | `/api/egolite/sessions/{id}/abort` | 用户端 | path: id / body: { reason } | code=1 "浏览器会话不存在" | `src/api/egolite.js` | 171 |
| 370 | [x] | POST | `/api/egolite/sessions/{id}/finish` | 用户端 | path: id / body: { keep } | code=1 "浏览器会话不存在" | `src/api/egolite.js` | 174 |
| 371 | [x] | GET | `/api/egolite/admin/telemetry/summary` | 用户端 | body: { hours } | 200 data={buckets:[],total:0,window_hours:24} | `src/api/egolite.js` | 180 |
| 372 | [x] | GET | `/api/egolite/sessions/{id}/export` | 用户端 | path: id / body: { format } | code=1 "浏览器会话不存在" | `src/api/egolite.js` | 184 |
| 373 | [x] | POST | `/api/egolite/sessions/{id}/save-as-flow` | 用户端 | path: id / body: { name } | code=1 data={flow:null,scan_report:null} "浏览器会话不存在" | `src/api/egolite.js` | 188 |
| 374 | [x] | GET | `/api/email/smtp` | 用户端 | - | 200 {list:[{id(UUID),name,server,port,username,limit}]}，不回 password（脱敏） | `src/api/email.js` | 5 |
| 375 | [x] | POST | `/api/email/smtp` | 用户端 | body: data | 400 空 body INVALID_PARAM_1001 → 200 创建成功返回 id（字段名与前端源码形状不同，以实测为准） | `src/api/email.js` | 9 |
| 376 | [x] | PUT | `/api/email/smtp/{id}` | 用户端 | path: id / body: data | 200 部分字段更新成功；不存在 404 NOT_FOUND_1002 "SMTP配置不存在" | `src/api/email.js` | 13 |
| 377 | [x] | DELETE | `/api/email/smtp/{id}` | 用户端 | path: id | 200 删除成功；重复删 404 | `src/api/email.js` | 17 |
| 378 | [x] | GET | `/api/email/drafts` | 用户端 | - | 200 {total,list:[{id,subject,content,...}]}（字段是 content 非 body） | `src/api/email.js` | 21 |
| 379 | [x] | GET | `/api/email/drafts/{id}` | 用户端 | path: id | 400 非 UUID id "无效的 ID 格式" → 200 UUID 成功 | `src/api/email.js` | 25 |
| 380 | [x] | POST | `/api/email/drafts` | 用户端 | body: data | 200 需 subject/content，返回完整对象含 attachments[]/created_at | `src/api/email.js` | 29 |
| 381 | [x] | PUT | `/api/email/drafts/{id}` | 用户端 | path: id / body: data | 200 部分更新成功；不存在 UUID 404 "更新邮件草稿失败" | `src/api/email.js` | 33 |
| 382 | [x] | DELETE | `/api/email/drafts/{id}` | 用户端 | path: id | 200 删除成功；重复删幂等成功（软删再删不报错，非 404） | `src/api/email.js` | 37 |
| 383 | [x] | POST | `/api/upload` | 用户端 | body: formData | 200 PNG 上传成功 {url:/files/attachments/2026/11/{uuid}.png,virus_clean:true,virus_scanned:false}；.txt 拒 400 "不支持的文件类型" | `src/api/email.js` | 41 |
| 384 | [x] | POST | `/api/email/send` | 用户端 | body: data | 400 错误文案列出必填 "to、subject、content、smtpId 均为必填（draftId/attachments/sendTime/immediateSend 可选）" → 200 真实发送成功返回 EmailSend status=0（pending；异步投递，假 SMTP → failed(2)，状态机正确） | `src/api/email.js` | 49 |
| 385 | [x] | GET | `/api/email/list?page={page}&limit={limit}` | 用户端 | path: page, limit | 200 {total:0,list:[]}（email_lists 营销列表分页，非发送历史；当前表空） | `src/api/email.js` | 52 |
| 386 | [x] | GET | `/api/email/jobs?page={page}&limit={limit}` | 用户端 | path: page, limit | 200 {total:25,list:[{id,subject,email_total,send_total,read_total,success_total,fail_total,...}]} | `src/api/email.js` | 56 |
| 387 | [x] | DELETE | `/api/email/jobs/{id}` | 用户端 | path: id | 404 "邮件任务不存在"（不存在 id） | `src/api/email.js` | 59 |
| 388 | [x] | GET | `/api/email/jobs/{id}` | 用户端 | path: id | 200 真实历史 job 详情成功；不存在 404 "任务不存在" | `src/api/email.js` | 62 |
| 389 | [x] | GET | `/api/email/lists/{id}/tracking` | 用户端 | path: id | 404 "邮件列表不存在"（email_lists 表空，GetTrackingEvents 先查列表；列表存在但无任务时应返回空 {list:[],total:0}，无数据无法验证） | `src/api/email.js` | 66 |
| 390 | [x] | DELETE | `/api/email/list/{id}` | 用户端 | path: id | 404 "邮件列表不存在"（admin-only 路由；空表拒绝路径验证，真实删除无数据可删） | `src/api/email.js` | 70 |
| 391 | [x] | GET | `/api/faqs` | 用户端 | query: params | 200 {list:[...],page,page_size,total:13393}；query keyword/category/intent/enabled/page/page_size 生效；kb_id 传值 400 明确指路（FAQ 全局列表无 kb_id 归属）；page=abc 400 | `src/api/faq.js` | 5 |
| 392 | [x] | GET | `/api/faqs/{id}` | 用户端 | path: id | 200 详情 id=33；不存在 404 "FAQ 不存在"（NOT_FOUND_1002）；非数字 id 400 "无效的 FAQ ID" | `src/api/faq.js` | 9 |
| 393 | [x] | POST | `/api/faqs` | 用户端 | body: data | 400 缺 question/answer/agent_id 明确报错 → 200 创建 id=244509（tenant_id=1 正确写入，enabled 默认 true，quality_score=0.5） | `src/api/faq.js` | 13 |
| 394 | [x] | PUT | `/api/faqs/{id}` | 用户端 | path: id / body: data | 200 更新成功（改 question/answer/keywords 回读一致，enabled 缺省保持原值）；**不存在 id 曾返回 200 假成功 → D-18 修复后 404 "FAQ 不存在"** | `src/api/faq.js` | 17 |
| 395 | [x] | PATCH | `/api/faqs/{id}/toggle` | 用户端 | path: id / body: { enabled } | 200 toggle enabled=false 回读一致；缺 enabled 400；**不存在 id 曾 200 假成功 → D-18 修复后 404** | `src/api/faq.js` | 21 |
| 396 | [x] | DELETE | `/api/faqs/{id}` | 用户端 | path: id | 200 删除成功；重复删 404 "FAQ 不存在" | `src/api/faq.js` | 25 |
| 397 | [x] | POST | `/api/faqs/match` | 用户端 | body: data | 400 缺 msg/agent_id 明确报错 → 200 关键词匹配返回 entry+score（Layer1 命中 id=33，confidence 计分） | `src/api/faq.js` | 29 |
| 398 | [x] | GET | `/api/faqs/stats` | 用户端 | - | 200 {enabled:13393,total:13393} | `src/api/faq.js` | 33 |
| 399 | [x] | GET | `/api/feature-flags` | 用户端 | query: page, page_size | 200 {list,total}；字段 id/tenant_id/key/name/description/enabled/rollout_percentage/payload/tags/created_by/created_at；page/page_size 生效（off 模式可见多租户数据） | `src/api/featureFlags.js` | 15 |
| 400 | [x] | GET | `/api/feature-flags/stale` | 用户端 | - | 200 {list:[...]}；14 天无评估/改动的开关（deep_test_flag、test.feature 等） | `src/api/featureFlags.js` | 21 |
| 401 | [x] | GET | `/api/feature-flags/{id}` | 用户端 | path: id | 200 详情 id=13；不存在 99999 → 404 NOT_FOUND_1002 "记录不存在"；支持按 key 查（GetByIDOrKey） | `src/api/featureFlags.js` | 25 |
| 402 | [x] | POST | `/api/feature-flags` | 用户端 | body: data | 400 缺 key/name 明确报错 "key 与 name 均为必填（description / enabled / rollout_percentage 可选）" → 200 创建 id=23（created_by=54）；**曾传 rollout_percentage=0 落库变 100（D-19），修复后原样写入 0** | `src/api/featureFlags.js` | 29 |
| 403 | [x] | PUT | `/api/feature-flags/{id}` | 用户端 | path: id / body: data | 200 部分更新（description/rollout_percentage）；rollout 传 0 正确落库；name/description/payload/tags/rollout 均指针语义，缺省不覆盖 | `src/api/featureFlags.js` | 33 |
| 404 | [x] | DELETE | `/api/feature-flags/{id}` | 用户端 | path: id | 200 删除成功 {deleted:true}；重复删 404 "记录不存在" | `src/api/featureFlags.js` | 37 |
| 405 | [x] | POST | `/api/feature-flags/{id}/enable` | 用户端 | path: id | 200 已启用 enabled=true，保留 rollout_percentage=30；写审计 action=enable | `src/api/featureFlags.js` | 44 |
| 406 | [x] | POST | `/api/feature-flags/{id}/disable` | 用户端 | path: id | 200 已禁用 enabled=false；写审计 action=disable（detail 为空字符串，仅记动作） | `src/api/featureFlags.js` | 48 |
| 407 | [x] | POST | `/api/feature-flags/{id}/rollout` | 用户端 | path: id / body: { percentage } | 200 灰度已更新 rollout_percentage=30；越界 150 → 400 "rollout_percentage 必须在 0-100" | `src/api/featureFlags.js` | 54 |
| 408 | [x] | GET | `/api/feature-flags/{id}/audit` | 用户端 | path: id | 200 {list:[...]} 审计倒序（disable/enable/rollout detail="rollout=30%"/update/create，含 actor_id） | `src/api/featureFlags.js` | 58 |
| 409 | [x] | GET | `/api/feature-flags/{id}/eval-log` | 用户端 | path: id | 200 {list:[],total:0}（无评估曝光记录；GetEvalLogs 按 flag_key 过滤） | `src/api/featureFlags.js` | 62 |
> **缺陷 D-19（已修复）**：`POST /api/feature-flags` 显式传 `rollout_percentage: 0` 落库变 100。
> 根因：`model/feature_flag.go` 的 `RolloutPercentage int gorm:"default:100"`——GORM 在 Create 时会把带列默认值的零值字段替换成默认值（`Select("*")` 也绕不过，见 gorm callbacks/create.go），
> 于是"谁都不放量"的合法取值被静默改写成"全量放量"。修复：去掉该列 `default` 标签，默认 100 由 `service.Create` 负责；补回归测试 `TestFeatureFlag_CreatePersistsZeroRollout`。
> 验证：repository/service 测试通过；复测创建 rollout=0 返回 0。

| 410 | [x] | GET | `/api/feishu/accounts` | 用户端 | query: params | 200 data=[...] 数组（非分页对象；当前 2 条：id 16/17 auto_1788151374）；字段 id/account_name/app_id/app_secret_masked/verification_token/encrypt_key_masked/webhook_enabled/ai_agent_enabled/status；密钥脱敏为 `secr****3456` 形态 | `src/api/feishu.js` | 4 |
| 411 | [x] | GET | `/api/feishu/accounts/{id}` | 用户端 | path: id | 200 详情 id=20；不存在 99999 → 404 NOT_FOUND_1002「飞书账号不存在」 | `src/api/feishu.js` | 8 |
| 412 | [x] | POST | `/api/feishu/accounts` | 用户端 | body: data | 400 空 body → INVALID_PARAM_1001（account_name/app_id/app_secret 均必填）→ 200 创建 id=20，status=1，密钥回显掩码 | `src/api/feishu.js` | 12 |
| 413 | [x] | PUT | `/api/feishu/accounts/{id}` | 用户端 | path: id / body: data | 200 更新成功（改名/换密钥/webhook+ai_agent 开关）；**只传 account_name 时 app_secret 掩码保持 `secr****8877` 不被清空**（空串表示不改）；不存在 99999 → 404「飞书账号不存在」 | `src/api/feishu.js` | 16 |
| 414 | [x] | DELETE | `/api/feishu/accounts/{id}` | 用户端 | path: id | 200 删除成功；重复删 404「飞书账号不存在」 | `src/api/feishu.js` | 20 |
| 415 | [x] | POST | `/api/feishu/accounts/{id}/test-send` | 用户端 | path: id / body: data | 400 缺 open_id/content → INVALID_PARAM_1001；正确 body → **修复前 HTTP 500 INTERNAL_ERROR_6002（D-20）→ 修复后 HTTP 502 UPSTREAM_FAILURE_5005** `channel error [auth]: get feishu access token failed: 刷新飞书 Token 失败：code=10003 msg=invalid param`（假凭据上游拒绝，环境受限）；GET 兼容路径缺参返回 200 test_send_ready=true | `src/api/feishu.js` | 24 |
| 416 | [x] | POST | `/api/feishu/accounts/{id}/refresh-token` | 用户端 | path: id | 200 刷新成功（真实凭据场合）；假凭据 → HTTP 502 UPSTREAM_FAILURE_5005「刷新飞书 Token 失败：code=10003 msg=invalid param」上游分类正确；不存在账号 404 NOT_FOUND_1002 | `src/api/feishu.js` | 28 |
> **缺陷 D-20（已修复）**：`POST /api/feishu/accounts/{id}/test-send` 在飞书返回上游错误时回 HTTP 500 `INTERNAL_ERROR_6002`。
> 同一条「拉 token 失败」错误，`refresh-token` 走 bizerr 正确回 502，test-send 却走 `response.ErrorFromDB` 直接落终态 500——运维看不出该改凭据还是查本机，客户端还会重试。
> 修复：`controller/feishu_account.go` 新增 `respondFeishuError`（与企微 `respondWeComError` 同口径），`service.AsChannelError` 命中即回 502 `UPSTREAM_FAILURE_5005`，两处发送调用点改用。复测：502 + 渠道原文。

| 417 | [x] | GET | `/api/followups/today` | 用户端 | query: params / body: SILENT | **默认 FF_LTC_ORDER_DRAFT_DB=off ⇒ 503 SERVICE_UNAVAILABLE_6003「跟进服务未装配」（设计口径：没装配≠没数据）；shadow 档 200 {date,list:[]}；缺 owner_id 400「管理员需显式指定跟进负责人」；非法 date 400「需为 YYYY-MM-DD」；指定 date 生效** | `src/api/followup.js` | 28 |
| 418 | [x] | GET | `/api/followups/pending` | 用户端 | query: params / body: SILENT | off 档 503 未装配；shadow 档 200 {list:[]}（按 owner+状态读进程内运行时） | `src/api/followup.js` | 35 |
| 419 | [x] | GET | `/api/followups/overdue` | 用户端 | body: { owner_id: ownerId }; SILENT | off 档 503；shadow 档 200 {list:[]}（逾期未完成项，owner_id 必填同 #417） | `src/api/followup.js` | 39 |
| 420 | [x] | POST | `/api/followups/{id}/complete` | 用户端 | path: id / body: { result, note } | off 档 503；shadow 档：result 枚举校验 contacted/interested/quoted/converted/rejected/lost/no_response（错值 400 列出全部可用值）；不存在 id → 404 NOT_FOUND_1002「提醒 fu_notexist 不存在」 | `src/api/followup.js` | 46 |
| 421 | [x] | POST | `/api/followups/{id}/cancel` | 用户端 | path: id | off 档 503；shadow 档不存在 id → 404「提醒 fu_notexist 不存在」 | `src/api/followup.js` | 50 |

> **观察项（非缺陷）**：跟进提醒五端点常挂在 auth 组下，但服务实例来自 `app.JourneyFollowUpForHTTP()`，
> 而草稿竖运行时只在 `FF_LTC_ORDER_DRAFT_DB` ≠ off 时装配（默认 off）。因此默认档全部回
> 503 `SERVICE_UNAVAILABLE_6003`「跟进服务未装配」——控制器注释明确这是刻意口径（"没装配"与"没数据"必须区分，
> 空列表会被误读成"今天没有跟进"）。本轮以 `FF_LTC_ORDER_DRAFT_DB=shadow` 起临时实例（PORT=8299）验证功能面：
> 五条端点均返回 200 与正确结构，参数校验/枚举校验/404 均正常。生产开启该旗子即可用。
| 422 | [x] | POST | `/api/geo/keywords/mine` | 用户端 | body: data | 200 code=0 挖掘成功；mode=combination 走词库组合（无需 LLM），返回 GeoKeyword 数组（tenant_id=1,source=combination,layer=related）；LLM 模式 provider 不可用时降级启发式词库 | `src/api/geo.js` | 5 |
> **缺陷 D-21（已修复）**：GEO 关键词 expand/cluster 与 push/urls 三端点在「前置能力缺失」时回错分类。
> 修复前：expand/cluster 把「无可用 LLM provider」回 500 INTERNAL_ERROR_6002（且 err 从不打印，日志无法定位）；
> push/urls 把「未配置推送平台」回 500（引导文案被埋进 INTERNAL_ERROR）。
> 根因：controller 固定 `response.Error(500, 固定文案)` 吞掉 err 分类。
> 修复：新增 `internal/geo/controller/llm_error.go`——`respondGeoLLMError`（无 provider → 503 SERVICE_UNAVAILABLE_6003
> + 配置引导；provider 调用失败 → 502 UPSTREAM_FAILURE_5005）与 `respondGeoConfigError`（前置配置缺失 → 503 + 原文案）。
> 关键词 expand/cluster 与 push/urls 四处调用点改用。验证：重建+重启后实测 expand/cluster → 503 配置引导，
> push/urls → 503「没有启用的推送平台…」，非法 platform 仍 400 INVALID_PARAM_1001。

| 423 | [x] | POST | `/api/geo/keywords/expand` | 用户端 | body: data | 无可用 LLM provider → **503 SERVICE_UNAVAILABLE_6003「语义扩展失败：未配置可用的大模型，请先在模型供应商中启用」（D-21，修复前 500 INTERNAL_ERROR_6002）**；环境阻塞：本环境未启用 LLM | `src/api/geo.js` | 9 |
| 424 | [x] | POST | `/api/geo/keywords/cluster` | 用户端 | body: data | 无可用 LLM provider → **503 SERVICE_UNAVAILABLE_6003「话题集群失败：未配置可用的大模型…」（D-21，修复前 500）**；环境阻塞同上 | `src/api/geo.js` | 13 |
| 425 | [x] | GET | `/api/geo/keywords/list` | 用户端 | query: params | 200 SuccessWithPage {list,page,limit,total}；#422/#426/#427 产出的关键词入库可见（combination/suggest_baidu/template_combined） | `src/api/geo.js` | 17 |
| 426 | [x] | POST | `/api/geo/keyword-mining/crawl-suggest` | 用户端 | body: data | 200 code=0 {count:10,saved,per_engine,errors:[],keywords}；baidu 下拉词抓取成功（本机外网可达 baidu suggest）；keyword=crm管理系统 source=suggest_baidu parent_keyword=CRM | `src/api/geo.js` | 22 |
| 427 | [x] | POST | `/api/geo/keyword-mining/longtail` | 用户端 | body: data | 200 code=0 {count:23,saved,keywords}；use_default_templates=true 用 DB 字典模板组合（如何选择CRM系统 等 how_to 意图） | `src/api/geo.js` | 26 |
| 428 | [x] | GET | `/api/geo/keyword-mining/funnel` | 用户端 | - | 200 code=0 {seed_count:29114,related_count:620,suggest_count:32831,longtail_count:522630,total:585195,intents/funnel_stages 分布} | `src/api/geo.js` | 30 |
| 429 | [x] | POST | `/api/geo/push/urls` | 用户端 | body: data | **503 SERVICE_UNAVAILABLE_6003「没有启用的推送平台，请先在「蜘蛛推送配置」添加凭据」（D-21，修复前 500）**；非法 platform → 400 INVALID_PARAM_1001「推送平台未启用」；真实推送需配置凭据（环境阻塞） | `src/api/geo.js` | 35 |
| 430 | [x] | GET | `/api/geo/push/quota` | 用户端 | `src/api/geo.js` | - | 200 data={}（配额快照，无配置时空对象） | 39 |
> **缺陷 D-22（已修复）**：`POST/PUT /api/geo/pushers` 的 platform 不校验枚举（normalizePlatform 只查必填），
> 传 `bogus` 也创建成功。loadState（push.go）装配期对未知平台静默跳过，配了永远不会被推送且无任何提示——
> 与 ValidatePlatforms 注释「未注册的平台必须报错，不能静默跳过」的口径矛盾。
> 修复：normalizePlatform 增加白名单校验（baidu/google/indexnow，即 loadState 实际装配的平台），
> 报错文案「不支持的平台: bogus（可选 baidu / google / indexnow）」；补 service 回归测试（未知 platform 应报错）。
> 验证：go test -run PusherConfig ok、go build/vet ok；复测 bogus → 400，合法 indexnow 创建成功。

| 431 | [x] | GET | `/api/geo/push/sitemap` | 用户端 | `src/api/geo.js` | - | 200 data={content:"<?xml...urlset>"} sitemap XML（含已部署文章 URL https://example.com/article/...html） | 43 |
| 432 | [x] | POST | `/api/geo/index-tracking/verify/{articleId}` | 用户端 | `src/api/geo.js` | path: articleId | 不存在文章 → 404 NOT_FOUND_1002 "文章不存在"（真实验证需外网可达搜索引擎，环境阻塞；拒绝路径正确） | 48 |
| 433 | [x] | GET | `/api/geo/index-tracking/funnel` | 用户端 | `src/api/geo.js` | - | 200 {seed_count:29114,longtail_count:522630,article_count:543,deployed_count:25,indexed_by_engine:{},ai_cited_by_engine:{},conversion_rates:{article_to_deployed:4.6,longtail_to_article:0.1}} | 52 |
| 434 | [x] | POST | `/api/geo/index-tracking/verify-all` | 用户端 | `src/api/geo.js` | - | 200 {started:true}（异步触发全量验证，耗时 38s；真实抓取需外网搜索引擎可达） | 56 |
| 435 | [x] | GET | `/api/geo/citations/drift` | 用户端 | `src/api/geo.js` | query: params | 200 {from_date,to_date,added:null,removed:null,changed:null,rank_moves:null}（7 天无引用快照时全空） | 61 |
| 436 | [x] | GET | `/api/geo/citations/domains` | 用户端 | `src/api/geo.js` | query: days | 200 {days:7,domains:[{domain:tousu.sina.com.cn,category:earned,count:27,query_count,engine_count}...]}（真实引用域数据） | 65 |
| 437 | [x] | GET | `/api/geo/citations/matrix` | 用户端 | `src/api/geo.js` | query: days, top_queries, top_domains | 200 {days:7,domains:[...top3],rows:[{query:"HiveMTK 投诉",domains:{...},total:18}...]}；format=csv 时回 CSV blob | 69 |
| 438 | [x] | POST | `/api/geo/site/export` | 用户端 | `src/api/geo.js` | - | 未配置站点目录 → 500 "站点目录必须位于 GEO_SITE_ROOT 的当前租户子目录（.../hivemtk-geo-sites/1）之下"（路径安全校验，拒绝软链接越界）；配置合法 hugo_path 后 200 {exported:25} | 79 |
| 439 | [x] | POST | `/api/geo/site/deploy` | 用户端 | `src/api/geo.js` | - | 未配置 → 400（ErrSiteConfigInvalid）；git push 无远端 → 500 "git -C ... push origin main: exit status 1"（真实部署需 git remote/部署凭据，环境阻塞） | 83 |
| 440 | [x] | POST | `/api/geo/site/full-pipeline` | 用户端 | `src/api/geo.js` | - | export→deploy→push 全链路；deploy 失败 → 500 "deploy: git push..."（错误前缀区分阶段）；同 #439 环境阻塞 | 87 |
| 441 | [x] | GET | `/api/geo/site/llms-txt-preview` | 用户端 | `src/api/geo.js` | query: domain? | 200 {content:"# HiveMTK\n> HiveMTK 的 GEO 内容站..."}；domain 可选默认 example.com；brand 取 geo_config.brand_name | 91 |
| 442 | [x] | GET | `/api/geo/site/robots-preview` | 用户端 | `src/api/geo.js` | - | 200 {content:"User-agent: *\nAllow: /\nDisallow: /admin/...OAI-SearchBot Allow..."}（AI 爬虫友好 robots.txt） | 95 |
| 443 | [x] | GET | `/api/geo/sites` | 用户端 | `src/api/geo.js` | query: params | 200 {list:[...]}；字段 tenant_id/id(uuid)/domain/provider/hugo_path/git_repo/deploy_cmd/health_score/last_deploy_at/active | 100 |
| 444 | [x] | POST | `/api/geo/sites` | 用户端 | `src/api/geo.js` | body: data | 200 created；缺 domain 校验；**D-22 前 platform 语义无校验的平行问题不存在于此端点（domain/provider 必填正确）**；创建返回完整实体 | 104 |
| 445 | [x] | PUT | `/api/geo/sites/{id}` | 用户端 | `src/api/geo.js` | path: id / body: data | 200 updated（指针语义：只改传的字段，hugo_path/active 生效）；不存在 → 404 "站点配置不存在"；hugo_path 必须在 GEO_SITE_ROOT/<tenant>/ 下（越界 400） | 108 |
| 446 | [x] | DELETE | `/api/geo/sites/{id}` | 用户端 | `src/api/geo.js` | path: id | 200 deleted；重复删 → 404 NOT_FOUND_1002 "站点配置不存在"；注意 GET /api/geo/sites/{id} 存在（geo_routes L320），前端未定义 get | 112 |
| 447 | [x] | GET | `/api/geo/pushers` | 用户端 | `src/api/geo.js` | query: params | 200 {list,total}（初始空）；字段 platform/config_json/daily_limit/used_today/last_reset_at/active | 117 |
| 448 | [x] | POST | `/api/geo/pushers` | 用户端 | `src/api/geo.js` | body: data | 400 缺 config_json "config_json 必填"；**bogus 平台原创建成功（D-22），修复后 400 "不支持的平台: bogus（可选 baidu / google / indexnow）"**；合法 indexnow {host,key,keyLocation} 创建成功 | 121 |
| 449 | [x] | PUT | `/api/geo/pushers/{id}` | 用户端 | `src/api/geo.js` | path: id / body: data | 200 updated（daily_limit/active 指针语义）；platform 传 bogus → 400（D-22）；不存在 → 404 "推送平台配置不存在" | 125 |
| 450 | [x] | DELETE | `/api/geo/pushers/{id}` | 用户端 | `src/api/geo.js` | path: id | 200 deleted；重复删 404 "推送平台配置不存在" | 129 |
| 451 | [x] | GET | `/api/geo/schema-templates` | 用户端 | `src/api/geo.js` | query: params | 200 {list:[],total:0} | 134 |
| 452 | [x] | POST | `/api/geo/schema-templates` | 用户端 | `src/api/geo.js` | body: data | 400 缺 page_type/schema_type "page_type required"；template_json 需 JSON 对象（数组/标量拒）；合法 {page_type:article,schema_type:Article,template_json:{@context,@type}} 创建成功 | 138 |
| 453 | [x] | PUT | `/api/geo/schema-templates/{id}` | 用户端 | `src/api/geo.js` | path: id / body: data | 200 updated（active=false 指针语义）；不存在 → 404 "Schema 模板不存在" | 142 |
| 454 | [x] | DELETE | `/api/geo/schema-templates/{id}` | 用户端 | `src/api/geo.js` | path: id | 200 deleted；重复删 404 "Schema 模板不存在" | 146 |
| 455 | [x] | DELETE | `/api/geo/keywords/{id}` | 用户端 | `src/api/geo.js` | path: id | 200 "删除关键词成功"；重复删 → 404 NOT_FOUND_1002 "删除关键词失败" | 150 |
| 456 | [x] | POST | `/api/geo/content/generate` | 用户端 | `src/api/geo.js` | body: data | 空 body/缺 keyword → **400 INVALID_PARAM_1001（D-23，修复前静默生成占位文章）**；合法 keyword → 200 生成成功（article_id, title, word_count）；LLM 未配置降级启发式模板 | 154 |
> **缺陷 D-23 系列（已修复）**：geo content/config/claims/competitors 四处入参或错误分类缺陷。
> ①D-23：`POST /api/geo/content/generate` 空 keyword 不报错，静默生成「： 专业解决方案」占位文章——
> `dto/content.go` GenerateContentRequest.Keyword 补 `binding:"required"`（batch 本来就有 min=1，word_count min=100 同 DTO 生效）。
> ②D-23b：`/api/geo/config/optimize` LLM 不可用回 500——改 respondGeoLLMError → 503 配置引导。
> ③D-23c：`/api/geo/inaccurate-claims` LLM 熔断回 500 且 err 原文拼进文案（细节泄漏）——改 respondGeoLLMError → 503 标准文案。
> ④D-23d：`PUT /api/geo/competitors/{id}` 不传 domain 强制 400——service 更新时域名为空保持原值。
> 验证：全部 go build/vet + 重建重启复测通过（空 keyword 400 / optimize 503 / claims 503 / competitors 不传 domain 200）。

| 457 | [x] | POST | `/api/geo/content/batch-generate` | 用户端 | `src/api/geo.js` | body: data | 空 body → 400 "keywords 至少 1 个"；≤20 条逐条容错（个别失败不影响其余），truncated_to 字段返回截断信息 | 159 |
| 458 | [x] | POST | `/api/geo/content/score` | 用户端 | `src/api/geo.js` | body: data | 200 评分成功 engine=rule_v1（heuristic）；content_html/content_text 可选 | 163 |
| 459 | [x] | POST | `/api/geo/content/optimize` | 用户端 | `src/api/geo.js` | body: data | 200 优化建议成功（无 LLM 走启发式；article_id 不校验存在性——观察项） | 167 |
| 460 | [x] | POST | `/api/geo/content/eeat` | 用户端 | `src/api/geo.js` | body: data | 200 EEAT 评分 template_v1（experience/expertise/authoritativeness/trustworthiness 拆分） | 171 |
| 461 | [x] | POST | `/api/geo/content/schema` | 用户端 | `src/api/geo.js` | body: data | 200 Schema 标记生成（固定 JSON-LD 模板） | 175 |
| 462 | [x] | POST | `/api/geo/content/uniqueness` | 用户端 | `src/api/geo.js` | body: data | 200 唯一性检测 regex_v1（shingle 相似度） | 179 |
| 463 | [x] | GET | `/api/geo/content/list` | 用户端 | `src/api/geo.js` | query: params | 200 SuccessWithPage（GEO 文章列表，含 status/quality_score） | 183 |
| 464 | [x] | GET | `/api/geo/content/{id}` | 用户端 | `src/api/geo.js` | path: id | 200 详情；不存在 → 404 | 187 |
| 465 | [x] | POST | `/api/geo/verification/verify` | 用户端 | `src/api/geo.js` | body: data | 400 "请选择至少一个验证模型"（LLM 未配置环境阻塞；校验正确） | 191 |
| 466 | [x] | POST | `/api/geo/verification/negative` | 用户端 | `src/api/geo.js` | body: data | 200 负面信息扫描（无 LLM 降级 heuristic 关键词匹配） | 195 |
| 467 | [x] | GET | `/api/geo/verification/results/{articleId}` | 用户端 | `src/api/geo.js` | path: articleId | 200 {list,total}；不存在文章 → 空列表 | 199 |
| 468 | [x] | GET | `/api/geo/reports/summary` | 用户端 | `src/api/geo.js` | - | 200 {articles:549,deployed:25,avg_quality_score,citations:{},visibility:{}} | 203 |
| 469 | [x] | GET | `/api/geo/reports/roi` | 用户端 | `src/api/geo.js` | - | 200 ROI 结构（成本/收益字段齐全，空数据零值） | 207 |
| 470 | [x] | GET | `/api/geo/reports/api-costs` | 用户端 | `src/api/geo.js` | - | 200 API 成本明细（token 用量按 provider 汇总） | 211 |
| 471 | [x] | GET | `/api/geo/reports/content-format` | 用户端 | `src/api/geo.js` | query: days | 200 内容格式分布（days 参数生效） | 215 |
| 472 | [x] | GET | `/api/geo/config` | 用户端 | `src/api/geo.js` | - | 200 {pushers:[],sites:[],sitemap:{},brand:{}}（聚合配置） | 219 |
| 473 | [x] | PUT | `/api/geo/config` | 用户端 | `src/api/geo.js` | body: data | 200 保存成功；部分字段更新不清空其他配置（指针语义） | 223 |
| 474 | [x] | POST | `/api/geo/config/optimize` | 用户端 | `src/api/geo.js` | body: data | LLM 不可用 → **503 SERVICE_UNAVAILABLE_6003「配置优化失败：未配置可用的大模型…」（D-23b，修复前 500 INTERNAL_ERROR_6002）** | 227 |
| 475 | [x] | GET | `/api/geo/alerts` | 用户端 | `src/api/geoAlert.js` | query: params | 200 {list:[...]}；ack_status 过滤生效 | 4 |
| 476 | [x] | GET | `/api/geo/alerts/unread-count` | 用户端 | `src/api/geoAlert.js` | - | 200 {count:N} | 7 |
| 477 | [x] | POST | `/api/geo/alerts/{id}/ack` | 用户端 | `src/api/geoAlert.js` | path: id | 非法 id（非整数）→ 400 "invalid id" | 10 |
| 478 | [x] | DELETE | `/api/geo/alerts/{id}` | 用户端 | `src/api/geoAlert.js` | path: id | 真实告警 → 200 ack 成功（operator=54）；非法 id → 400；重复 ack 幂等 | 13 |
| 479 | [x] | POST | `/api/geo/audit/site` | 用户端 | `src/api/geoAudit.js` | body: data | 400 缺 url "url 必填"；合法 https URL → 200 {score:36,max:112,checks:[...] }（TTFB/h1/llms.txt 等逐项） | 4 |
| 480 | [x] | POST | `/api/geo/audit/content` | 用户端 | `src/api/geoAudit.js` | body: data | 200 {score:26,max:100,checks:[...]}（内容质量逐项检查） | 6 |
| 481 | [x] | GET | `/api/geo/audit/runs` | 用户端 | `src/api/geoAudit.js` | query: params | 200 {list:[...]}（审计运行记录） | 8 |
| 482 | [x] | GET | `/api/geo/audit/runs/{id}` | 用户端 | `src/api/geoAudit.js` | path: id | 200 详情（checks 逐项结果）；不存在 → 404 | 10 |
| 483 | [x] | GET | `/api/geo/audit/compare` | 用户端 | `src/api/geoAudit.js` | query: a, b | 200 {a:{...},b:{...},delta:{...}}（两次审计分数对比） | 13 |
| 484 | [x] | GET | `/api/geo/audit/runs/{id}/report` | 用户端 | `src/api/geoAudit.js` | path: id | 200 text/html 报告（可直接渲染） | 17 |
| 485 | [x] | POST | `/api/geo/autopilot/runs` | 用户端 | `src/api/geoAutopilot.js` | body: data | 缺 keyword → 400 "keyword 必填"；合法 → 200 {run_id} 异步执行 → 轮询 success/progress=100 | 6 |
| 486 | [x] | GET | `/api/geo/autopilot/runs/{id}` | 用户端 | `src/api/geoAutopilot.js` | path: id | 200 {run_id,status,progress,steps}；不存在 → 404 | 10 |
| 487 | [x] | GET | `/api/geo/autopilot/runs` | 用户端 | `src/api/geoAutopilot.js` | query: params | 200 {list:[...]} | 14 |
| 488 | [x] | POST | `/api/geo/capture/links` | 用户端 | `src/api/geoCapture.js` | body: data | 缺 target_url → 400；非 http(s) → 400 "仅支持 http/https"；合法 → 200 抓取入库 | 3 |
| 489 | [x] | GET | `/api/geo/capture/links` | 用户端 | `src/api/geoCapture.js` | query: params | 200 {list,total} | 5 |
| 490 | [x] | GET | `/api/geo/capture/links/{id}` | 用户端 | `src/api/geoCapture.js` | path: id | 200 详情；不存在 → 404 | 7 |
| 491 | [x] | DELETE | `/api/geo/capture/links/{id}` | 用户端 | `src/api/geoCapture.js` | path: id | 200 重新抓取；不存在 → 404 | 9 |
| 492 | [x] | GET | `/api/geo/decision/report` | 用户端 | `src/api/geoDecision.js` | path: id | 200 抓取分析报告 | 3 |
| 493 | [x] | GET | `/api/geo/decision/tasks` | 用户端 | `src/api/geoDecision.js` | query: params | 200 {list:[...]}（补位任务） | 6 |
| 494 | [x] | POST | `/api/geo/decision/tasks/{id}/done` | 用户端 | `src/api/geoDecision.js` | path: id | 不存在 → 404 "补位任务不存在" | 8 |
| 495 | [x] | GET | `/api/geo/entity/list` | 用户端 | body: { type: type \|\| '', search: keyword \|\| '', page: page \|\| 1, limit: limit \|\| 20 }  | 200 {list,total} |  `src/api/geoEntity.js` | 5 |
| 496 | [x] | GET | `/api/geo/entity/{entityId}/graph` | 用户端 | `src/api/geoEntity.js` | path: id | 非法 id → 400；合法 → 200 {nodes,edges}（depth=2 空关系正常） | 13 |
| 497 | [x] | POST | `/api/geo/entities/extract` | 用户端 | `src/api/geoEntity.js` | body: data | 400 BUSINESS_5001（无可抽取文本/输入非法） | 16 |
| 498 | [x] | GET | `/api/geo/kb/documents` | 用户端 | body: { keyword: keyword \|\| '', source_level: sourceLevel \|\| '' }  | 200 {list,total}；source_level=manual → 400（varchar(2)——观察项） |  `src/api/geoEntity.js` | 19 |
| 499 | [x] | POST | `/api/geo/kb/documents` | 用户端 | `src/api/geoEntity.js` | path: id | 200 详情；不存在 → 404 | 22 |
| 500 | [x] | DELETE | `/api/geo/kb/documents/{id}` | 用户端 | `src/api/geoEntity.js` | path: id / body: data | 200 更新成功 | 25 |
| 501 | [x] | GET | `/api/geo/kb/documents/{id}` | 用户端 | `src/api/geoEntity.js` | path: id | 200 删除成功；重复删 404 | 28 |
| 502 | [x] | GET | `/api/geo/kb/search` | 用户端 | `src/api/geoEntity.js` | body: data | 200 检索结果（关键词匹配降级，无向量库时） | 31 |
| 503 | [x] | POST | `/api/geo/kb/ask` | 用户端 | `src/api/geoEntity.js` | body: data | 200 问答（retrieval_v1 检索拼接降级，无 LLM） | 34 |
| 504 | [x] | GET | `/api/geo/workflow/workflows` | 用户端 | `src/api/geoEntity.js` | query: params | 200 {list:[...]}（SOP 工作流） | 37 |
| 505 | [x] | GET | `/api/geo/workflow/workflows/{id}` | 用户端 | `src/api/geoEntity.js` | path: id | 200 详情；不存在 → 404 | 40 |
| 506 | [x] | POST | `/api/geo/workflow/workflows` | 用户端 | `src/api/geoEntity.js` | body: data | 缺 name → 400；合法 → 200 创建（draft） | 43 |
| 507 | [x] | PUT | `/api/geo/workflow/workflows/{id}` | 用户端 | `src/api/geoEntity.js` | path: id / body: data | 缺 steps → 400 "steps required"（全量更新语义）；合法 → 200 | 46 |
| 508 | [x] | DELETE | `/api/geo/workflow/workflows/{id}` | 用户端 | `src/api/geoEntity.js` | path: id | 200 删除；重复删 404 | 49 |
| 509 | [x] | POST | `/api/geo/workflow/workflows/{id}/run` | 用户端 | path: id / body: params \|\| {}  | 200 触发执行；generate 无 executor → failed（观察项） |  `src/api/geoEntity.js` | 52 |
| 510 | [x] | GET | `/api/geo/workflow/workflows/{workflowId}/executions` | 用户端 | `src/api/geoEntity.js` | path: id | 200 {list:[...]}; 执行历史 | 55 |
| 511 | [x] | GET | `/api/geo/workflow/templates` | 用户端 | `src/api/geoEntity.js` | path: id / stepId | 200 步骤结果 | 58 |
| 512 | [x] | POST | `/api/geo/workflow/templates` | 用户端 | `src/api/geoEntity.js` | path: id | 200 取消执行 | 61 |
| 513 | [x] | GET | `/api/geo/platform/platforms` | 用户端 | `src/api/geoPlatform.js` | - | 200 {list:[...]}（可接入平台清单） | 4 |
| 514 | [x] | GET | `/api/geo/platform/accounts` | 用户端 | `src/api/geoPlatform.js` | query: params | 200 {list:[...]} | 7 |
| 515 | [x] | POST | `/api/geo/platform/accounts` | 用户端 | `src/api/geoPlatform.js` | body: data | 缺 access_token → 400 明确文案；合法结构 → 200（真实连通需 GitHub token，环境阻塞） | 10 |
| 516 | [x] | DELETE | `/api/geo/platform/accounts/{id}` | 用户端 | `src/api/geoPlatform.js` | path: id | 200 删除；重复删 404 | 13 |
| 517 | [x] | POST | `/api/geo/platform/publish` | 用户端 | `src/api/geoPlatform.js` | body: data | 不存在文章 → 400 "文章不存在" | 16 |
| 518 | [x] | GET | `/api/geo/platform/records` | 用户端 | body: { article_id: articleId \|\| '' }  | 200 触发探测（simulated） |  `src/api/geoPlatform.js` | 19 |
| 519 | [x] | GET | `/api/geo/probe/engines` | 用户端 | `src/api/geoProbe.js` | query: params | 200 探测历史 {list,total} | 4 |
| 520 | [x] | GET | `/api/geo/probe/engines/meta` | 用户端 | `src/api/geoProbe.js` | path: id | 200 删除历史；重复删 404 | 8 |
| 521 | [x] | POST | `/api/geo/probe/import` | 用户端 | `src/api/geoProbe.js` | body: data | CSV 行级报错逐行返回（errors 数组），合法行 created=1 | 11 |
| 522 | [x] | POST | `/api/geo/probe/test` | 用户端 | `src/api/geoProbe.js` | body: data | 未知 engine → 400 "不支持的引擎"；合法 → 200 导入 | 14 |
| 523 | [x] | GET | `/api/geo/probe/runs` | 用户端 | `src/api/geoProbe.js` | query: params | 200 {list:[...]}; 探测目标列表 | 17 |
| 524 | [x] | POST | `/api/geo/probe/run-negative` | 用户端 | `src/api/geoProbe.js` | - | 200 {started:true}（异步批量探测） | 20 |
| 525 | [x] | POST | `/api/geo/probe/run-source-sync` | 用户端 | `src/api/geoProbe.js` | - | 200 {started:true}（异步全量重算） | 23 |
| 526 | [x] | POST | `/api/geo/probe/all` | 用户端 | `src/api/geoProbe.js` | path: id | 200 快照详情；不存在 → 404 | 26 |
| 527 | [x] | GET | `/api/geo/sov` | 用户端 | `src/api/geoProbe.js` | query: params | 200 SOV 报告（share_of_voice 92.8% 历史数据） | 29 |
| 528 | [x] | GET | `/api/geo/crawler-stats` | 用户端 | `src/api/geoProbe.js` | query: params | 200 {list:[...]}（引用来源） | 32 |
| 529 | [x] | POST | `/api/geo/crawler/run` | 用户端 | `src/api/geoProbe.js` | body: data | 200 {started:true} | 35 |
| 530 | [x] | POST | `/api/geo/inaccurate-claims` | 用户端 | `src/api/geoProbe.js` | body: data | LLM 熔断 → **503 SERVICE_UNAVAILABLE_6003（D-23c，修复前 500 且 err 原文泄漏）** | 38 |
| 531 | [x] | GET | `/api/geo/source-catalog/levels` | 用户端 | `src/api/geoProbe.js` | body: data | 缺 url → 400 "url 必填"；合法 → 200 采集任务创建 | 41 |
| 532 | [x] | GET | `/api/geo/competitors` | 用户端 | `src/api/geoProbe.js` | query: params | 200 {list:[...]} | 44 |
| 533 | [x] | GET | `/api/geo/competitors/{id}` | 用户端 | `src/api/geoProbe.js` | path: id | 200 删除；重复删 404 | 47 |
| 534 | [x] | POST | `/api/geo/competitors` | 用户端 | `src/api/geoProbe.js` | body: data | 缺 name/domain → 400；重复 domain → 400（masked 文案「操作失败，请稍后重试」）；合法 → 200 创建 | 50 |
| 535 | [x] | PUT | `/api/geo/competitors/{id}` | 用户端 | `src/api/geoProbe.js` | path: id / body: data | 200 更新；**不传 domain 保持原值（D-23d，修复前 400「竞品域名不能为空」）**；不存在 → 404 | 53 |
| 536 | [x] | DELETE | `/api/geo/competitors/{id}` | 用户端 | `src/api/geoProbe.js` | path: id | 200 删除；重复删 404 | 56 |
| 537 | [x] | GET | `/api/geo/visibility/engine-compare` | 用户端 | body: { days, intent: intent \|\| '' }  | 200 {list}（可见性趋势） |  `src/api/geoProbe.js` | 59 |
| 538 | [x] | GET | `/api/geo/sov/trend` | 用户端 | body: { days, intent: intent \|\| '' }  | 200 导出报告 |  `src/api/geoProbe.js` | 62 |
| 539 | [x] | POST | `/api/geo/search-console/import` | 用户端 | `src/api/geoSearchConsole.js` | body: data | 缺 rows/csv → 400；合法 → 200 {imported:1,errors:[]} | 4 |
| 540 | [x] | GET | `/api/geo/reports/dual-engine` | 用户端 | `src/api/geoSearchConsole.js` | query: params | 200 {list:[...]} | 7 |
| 541 | [x] | POST | `/api/glossaries` | 用户端 | `src/api/glossary.js` | body: data | 缺 term/definition → 400 明确文案；合法 → 200 创建 term_id=13 | 4 |
| 542 | [x] | GET | `/api/glossaries` | 用户端 | `src/api/glossary.js` | query: params | 200 {list:[...]} | 8 |
| 543 | [x] | GET | `/api/glossaries/{termId}` | 用户端 | `src/api/glossary.js` | path: termId | 200 详情；不存在 → 404（term_id 是字符串非数字主键） | 12 |
| 544 | [x] | PUT | `/api/glossaries/{termId}` | 用户端 | `src/api/glossary.js` | path: termId / body: data | 200 更新成功 | 16 |
| 545 | [x] | DELETE | `/api/glossaries/{termId}` | 用户端 | `src/api/glossary.js` | path: termId | 200 删除；重复删 404 | 20 |
| 546 | [x] | POST | `/api/glossaries/validate` | 用户端 | `src/api/glossary.js` | body: data | 200 校验结果（术语一致性检查） | 24 |
| 547 | [x] | GET | `/api/human-tasks` | 用户端 | `src/api/humanTask.js` | query: params | 200 {list:[...]}（人工任务） | 43 |
| 548 | [x] | GET | `/api/human-tasks/counts` | 用户端 | `src/api/humanTask.js` | - | 200 {pending,claimed,done,...} | 49 |
| 549 | [x] | GET | `/api/human-tasks/{id}` | 用户端 | `src/api/humanTask.js` | body: data | 200 创建任务成功 | 53 |
| 550 | [x] | POST | `/api/human-tasks/{id}/claim` | 用户端 | `src/api/humanTask.js` | path: id | pending → claimed（认领人=当前用户） | 58 |
| 551 | [x] | POST | `/api/human-tasks/{id}/release` | 用户端 | `src/api/humanTask.js` | path: id | claimed → pending（释放） | 63 |
| 552 | [x] | POST | `/api/human-tasks/{id}/complete` | 用户端 | `src/api/humanTask.js` | path: id / body: data | claim 后 complete → done（result 入库） | 68 |
| 553 | [x] | POST | `/api/human-tasks/{id}/cancel` | 用户端 | `src/api/humanTask.js` | path: id | 非法/不存在 → 404 | 73 |
| 554 | [x] | GET | `/api/i18n/stats` | 用户端 | `src/api/i18nStats.js` | - | 200 统计汇总 | 4 |
| 555 | [x] | GET | `/api/i18n/stats/lang-dist` | 用户端 | `src/api/i18nStats.js` | query: params | 200 {list:[...]}（i18n 词条，空数据） | 8 |
| 556 | [x] | GET | `/api/i18n/stats/cache` | 用户端 | `src/api/i18nStats.js` | body: data | 200 创建/更新词条 | 12 |
| 557 | [x] | GET | `/api/i18n/stats/glossary` | 用户端 | `src/api/i18nStats.js` | path: id | 200 删除词条 | 16 |
| 558 | [x] | GET | `/api/i18n/stats/quality` | 用户端 | `src/api/i18nStats.js` | - | 200 翻译覆盖率统计 | 20 |
| 559 | [x] | GET | `/api/i18n/stats/latency` | 用户端 | `src/api/i18nStats.js` | - | 200 导出 JSON | 24 |
| 560 | [x] | GET | `/api/inbox/conversations` | 用户端 | `src/api/inbox.js` | query: params | 200 {list,total}（站内信会话，真实数据 id=603） | 7 |
| 561 | [x] | GET | `/api/inbox/conversations/{id}` | 用户端 | `src/api/inbox.js` | path: id | 非数字 id → 400；合法 → 200 {messages,total} | 12 |
| 562 | [x] | GET | `/api/inbox/conversations/{id}/messages` | 用户端 | `src/api/inbox.js` | body: data | 200 发送成功 | 17 |
| 563 | [x] | POST | `/api/inbox/conversations/{id}/read` | 用户端 | `src/api/inbox.js` | path: id | 200 已读标记（幂等） | 22 |
| 564 | [x] | POST | `/api/inbox/conversations/{id}/pin` | 用户端 | `src/api/inbox.js` | path: id | 200 置顶状态切换（已还原） | 27 |
| 565 | [x] | POST | `/api/inbox/conversations/{id}/star` | 用户端 | `src/api/inbox.js` | path: id | 200 星标状态切换（已还原） | 31 |
| 566 | [x] | POST | `/api/inbox/conversations/{id}/mute` | 用户端 | `src/api/inbox.js` | path: id | 200 免打扰状态切换（已还原） | 35 |
| 567 | [x] | POST | `/api/inbox/conversations/{id}/tags` | 用户端 | `src/api/inbox.js` | path: id / body: data | 200 打标签成功 | 40 |
| 568 | [x] | DELETE | `/api/inbox/conversations/{id}/tags/{tag}` | 用户端 | `src/api/inbox.js` | path: id / body: data | 200 移除标签成功 | 44 |
| 569 | [x] | DELETE | `/api/inbox/conversations/{id}/messages/{mid}?source={source}` | 用户端 | `src/api/inbox.js` | path: id / query: source | source 只支持 hub/session → 404 "消息不存在" | 49 |
| 570 | [x] | POST | `/api/inbox/assign` | 用户端 | `src/api/inbox.js` | path: id / body: data | 缺 action → 400；human+assignee_id=54 → 200 分派成功 | 54 |
| 571 | [x] | POST | `/api/inbox/assign/auto` | 用户端 | `src/api/inbox.js` | body: data | 缺 candidates → 400；合法 → 200 round_robin 分派 | 59 |
| 572 | [x] | GET | `/api/inbox/assignments` | 用户端 | `src/api/inbox.js` | query: params | 200 {list:[...]}（分派记录） | 64 |
| 573 | [x] | GET | `/api/inbox/stats` | 用户端 | `src/api/inbox.js` | - | 200 {total:299,unread:143,...} | 69 |
| 574 | [x] | POST | `/api/inbox/reconcile?mode={mode}` | 用户端 | `src/api/inbox.js` | body: data | mode=all → 400 "mode 仅支持 unread/overdue/backfill"；unread → 200 重算 143 条 | 74 |
| 575 | [x] | GET | `/api/inbox/staff/{staff}/load` | 用户端 | `src/api/inbox.js` | - | 200 负载视图（按坐席未读分布） | 79 |
| 576 | [x] | GET | `/api/inbox/agents` | 用户端 | `src/api/inbox.js` | - | 200 {list:[...]}（坐席负载） | 84 |
| 577 | [x] | GET | `/api/integrations` | 用户端 | `src/api/integration.js` | query: params | 200 {list:[...]}（集成配置） | 4 |
| 578 | [x] | GET | `/api/integrations/{id}` | 用户端 | `src/api/integration.js` | body: data | 200 创建/更新；不存在 → 404（masked 文案「操作失败，请稍后重试」——观察项） | 7 |
| 579 | [x] | POST | `/api/integrations` | 用户端 | `src/api/integration.js` | path: id | 200 删除 id=27 成功 | 10 |
| 580 | [x] | PUT | `/api/integrations/{id}` | 用户端 | `src/api/integration.js` | path: id | 200 触发同步（异步） | 13 |
| 581 | [x] | DELETE | `/api/integrations/{id}` | 用户端 | `src/api/integration.js` | query: params | 200 同步记录 {list,total} | 16 |
| 582 | [x] | POST | `/api/integrations/{id}/sync-customers` | 用户端 | `src/api/integration.js` | body: data | crcrm 类型仅支持 销售易/纷享销客 → 400 明确文案 | 19 |
| 583 | [x] | POST | `/api/integrations/{id}/sync-products` | 用户端 | `src/api/integration.js` | body: data | ecomm 类型仅支持 淘宝/京东 → 400 明确文案 | 22 |
| 584 | [x] | GET | `/api/integration/sync-logs` | 用户端 | `src/api/integration.js` | path: id | 200 同步状态详情 | 25 |
| 585 | [x] | GET | `/api/integration/external-customers` | 用户端 | `src/api/integration.js` | query: params | 200 {list:[...]}（集成日志） | 28 |
| 586 | [x] | GET | `/api/integration/external-orders` | 用户端 | `src/api/integration.js` | path: id | 200 重试失败同步 | 31 |
| 587 | [x] | GET | `/api/integration/external-orders-by-customer` | 用户端 | `src/api/integration.js` | path: id | 200 测试连通性 | 34 |
| 588 | [x] | GET | `/api/integration/external-products` | 用户端 | `src/api/integration.js` | query: params | 200 {list:[...]}（映射规则） | 37 |
| 589 | [x] | POST | `/api/integrations/{id}/test` | 用户端 | `src/api/integration.js` | path: id | 不存在 → 404 | 56 |
| 590 | [x] | POST | `/api/intent/recognize` | 用户端 | `src/api/intentRecognition.js` | body: data | 200 意图识别（config.enabled=false → disabled 状态返回） | 5 |
| 591 | [x] | POST | `/api/intent/recognize/batch` | 用户端 | `src/api/intentRecognition.js` | body: data | 缺 items/messages → 400；合法 → 200 识别结果 | 9 |
| 592 | [x] | GET | `/api/intent/stats` | 用户端 | `src/api/intentRecognition.js` | query: params | 200 {list:[...]}（意图字典） | 13 |
| 593 | [x] | GET | `/api/intent/recent` | 用户端 | `src/api/intentRecognition.js` | body: data | 200 维护字典项 | 17 |
| 594 | [x] | GET | `/api/intent/dict` | 用户端 | `src/api/intentRecognition.js` | path: id | 200 导出字典 | 21 |
| 595 | [x] | POST | `/api/intent/recognize/fine` | 用户端 | `src/api/intentRecognition.js` | body: data | 缺 message → 400；合法 → 200 分类（consult/general） | 25 |
| 596 | [x] | GET | `/api/intent/logs` | 用户端 | `src/api/intentRecognition.js` | query: params | 200 {list:[...]}（路由规则） | 29 |
| 597 | [x] | GET | `/api/intent/stats/fine` | 用户端 | `src/api/intentRecognition.js` | body: data | 200 维护路由规则 | 33 |
| 598 | [x] | GET | `/api/intent/config` | 用户端 | `src/api/intentRecognition.js` | - | 200 {enabled:false,...} | 37 |
| 599 | [x] | PUT | `/api/intent/config` | 用户端 | `src/api/intentRecognition.js` | body: data | 200 保存生效（enabled 等开关） | 41 |
| 600 | [x] | GET | `/api/intent-records/keywords-override` | 用户端 | `src/api/intentRecognition.js` | - | 200 {}（兼容配置，空对象） | 45 |
| 601 | [x] | PUT | `/api/intent-records/keywords-override` | 用户端 | `src/api/intentRecognition.js` | body: data | 200 热更新生效（测试值写入后已还原为空） | 49 |
| 602 | [x] | GET | `/api/kb-connectors` | 用户端 | `src/api/kbConnector.js` | query: params | 200 {list:[...]}（知识库连接器） | 5 |
| 603 | [x] | POST | `/api/kb-connectors` | 用户端 | `src/api/kbConnector.js` | body: data | 缺 type → 400；webhook 不支持（可用 ima/webhttp）→ 400 列出枚举；ima → 200 创建 id=3 | 6 |
| 604 | [x] | PUT | `/api/kb-connectors/{id}` | 用户端 | `src/api/kbConnector.js` | path: id / body: data | 200 更新成功；不存在 → 404 | 7 |
| 605 | [x] | DELETE | `/api/kb-connectors/{id}` | 用户端 | `src/api/kbConnector.js` | path: id | 200 删除（已导入文档保留提示）；重复删 404 | 8 |
| 606 | [x] | POST | `/api/kb-connectors/{id}/sync` | 用户端 | `src/api/kbConnector.js` | path: id | 不存在 → 404 "连接器不存在"（真实同步需外部源，环境阻塞） | 9 |
| 607 | [x] | GET | `/api/capabilities` | 用户端 | - | 200 {capabilities:[...]} 能力矩阵（platform/capability/via/note/implemented/configured/ready，bilibili/douyin dm.receive/dm.send bridge）；无 token → 401 | `src/api/kbConnector.js` | 14 |
| 608 | [x] | GET | `/api/kb/crawler/sources` | 用户端 | query: params | 200 data=[...]（数组）；字段 tenant_id/id/kb_id/source_type/start_url/include_patterns/max_pages/interval_hours/qa_split_enabled/site_verify_token/site_verified/status；kb_id/status 过滤生效（筛后 0 行） | `src/api/kbCrawler.js` | 7 |
> **缺陷 D-24（已修复）**：爬取源「不存在」的错误分类错误——`POST /api/kb/crawler/sources/{id}/pause|resume`
> 对不存在 id 回 409 DUPLICATE_ENTRY_3003（controller 固定 response.Error(409)，而 response.Error 按
> errorCodeFromHTTPCode 把 409 折叠为"重复记录"语义，"资源不存在"被误标为"重复"）；`PUT /sources/{id}`
> 不存在回 400 INVALID_PARAM_1001（应为 404）。
> 根因：service 层 First 查不到时 errors.New("爬取源不存在") 丢失 not-found 语义。
> 修复：UpdateSource/TransitionSource 查不到改返回 gorm.ErrRecordNotFound；controller 对两分支用
> errhttp.IsNotFoundError 映射 404 NOT_FOUND_1002「爬取源不存在」，其余错误维持 400/409；id 非法仍 400。
> 验证：复测 pause/resume/update 传 99999 均 404「爬取源不存在」，合法跃迁与幂等路径不回归。

| 609 | [x] | POST | `/api/kb/crawler/sources` | 用户端 | body: data | 400 "source_type 必须是 url 或 sitemap"；合法 {kb_id,url,start_url} 创建成功，提示「请先完成站点验证（hivemtk-verify.txt 或 meta 标签）」 | `src/api/kbCrawler.js` | 11 |
| 610 | [x] | PUT | `/api/kb/crawler/sources/{id}` | 用户端 | path: id / body: data | 200 更新成功（白名单字段 include_patterns/max_pages/interval_hours/auto_resync/qa_split_enabled；start_url 不允许修改）；不存在 → 404 "爬取源不存在"（**D-24，修复前 400**） | `src/api/kbCrawler.js` | 14 |
| 611 | [x] | POST | `/api/kb/crawler/sources/{id}/pause` | 用户端 | path: id | 200 暂停成功（active→paused）；不存在 → **404 "爬取源不存在"（D-24，修复前 409 DUPLICATE_ENTRY_3003）** | `src/api/kbCrawler.js` | 17 |
| 612 | [x] | POST | `/api/kb/crawler/sources/{id}/resume` | 用户端 | path: id | 200 恢复成功（paused→active）；不存在 → **404（D-24，修复前 409）** | `src/api/kbCrawler.js` | 20 |
| 613 | [x] | POST | `/api/kb/crawler/sources/{id}/verify` | 用户端 | path: id | 未验证站点 → 400 "站点验证失败：请在目标站点根路径放置 hivemtk-verify.txt 或配置 meta 标签"（验证需对外网 http 检查，环境阻塞；拒绝路径正确） | `src/api/kbCrawler.js` | 23 |
| 614 | [x] | DELETE | `/api/kb/crawler/sources/{id}` | 用户端 | path: id | 200 删除成功；重复删 200 幂等（软删后查不到按不存在处理） | `src/api/kbCrawler.js` | 26 |
| 615 | [x] | POST | `/api/kb/crawler/sources/{id}/sync` | 用户端 | path: id | paused 源 → 400 "源状态为 paused，不可触发同步"；active 未验证 → 400 "源未通过站点验证，不能同步（先调用验证接口）"；不存在 → 400 "爬取源不存在"（sync 校验链完整） | `src/api/kbCrawler.js` | 30 |
| 616 | [x] | GET | `/api/kb/crawler/batches` | 用户端 | query: params | 200 {items:[...]}（历史 batch id=20 source_id=4 status=succeeded pages_found=2 pages_changed=2 trigger=manual） | `src/api/kbCrawler.js` | 34 |
| 617 | [x] | POST | `/api/kb/crawler/batches/{id}/retry` | 用户端 | path: id | 不存在 99999 → 400 "批次不存在"；succeeded 批次 → 400 "仅失败批次可重试"（状态机校验正确） | `src/api/kbCrawler.js` | 37 |
| 618 | [x] | GET | `/api/kb/qa/candidates` | 用户端 | query: params | 200 {items:[...]}（候选 id=1 kb_id=12 question="Go 支持哪些平台？" status=indexed） | `src/api/kbCrawler.js` | 44 |
> **缺陷 D-25（已修复，与 D-24 同模式）**：QA 候选 approve/reject/publish 对不存在 id 回 409 DUPLICATE_ENTRY_3003「候选不存在」。
> 根因：service/kb_qa_split.go 的 ReviewQACandidate/PublishQACandidate First 查不到时 fmt.Errorf("候选不存在") 丢失 not-found 语义；controller 固定 response.Error(409)。
> 修复：service 两处改返回 gorm.ErrRecordNotFound；controller 加 errhttp.IsNotFoundError 分支 → 404「候选不存在」（非法状态跃迁仍 409）。
> 复测：approve/reject/publish 99999 → 404 NOT_FOUND_1002；indexed 候选 publish → 409「非法状态跃迁: indexed → indexed（需先 approve）」不变。

| 619 | [x] | POST | `/api/kb/qa/candidates/{id}/approve` | 用户端 | path: id | 不存在 99999 → 409 DUPLICATE_ENTRY_3003（**D-25，修复后 404 NOT_FOUND_1002**） | `src/api/kbCrawler.js` | 47 |
| 620 | [x] | POST | `/api/kb/qa/candidates/{id}/reject` | 用户端 | path: id | 不存在 99999 → 409（**D-25，修复后 404**） | `src/api/kbCrawler.js` | 50 |
| 621 | [x] | POST | `/api/kb/qa/candidates/{id}/publish` | 用户端 | path: id | 不存在 99999 → 409（**D-25，修复后 404**）；indexed 候选 publish → 409 "非法状态跃迁: indexed → indexed（需先 approve）"（状态机正确） | `src/api/kbCrawler.js` | 53 |
| 622 | [x] | POST | `/api/knowledge/import/upload` | 用户端 | body: formData | 400 空 multipart "product_id 不能为空"；PNG → 400 "不支持的文件类型: .png"（白名单不含图片）；.txt → 200 "文件已接收,处理已启动"（document_id=557 pending→completed 异步） | `src/api/knowledge.js` | 5 |
| 623 | [x] | POST | `/api/knowledge/import/text` | 用户端 | body: data | 400 空 body（文案含 Go 字段名 ProductID/Content——观察项：与错误掩码口径不一致）；不存在产品 → 400 "产品不存在: rag product not found"；真实 product → 200 "文本导入已启动"（document_id=555） | `src/api/knowledge.js` | 10 |
| 624 | [x] | POST | `/api/knowledge/import/url` | 用户端 | body: data | 400 缺 ProductID/URL；https://example.com/docs → 400 "URL 返回错误状态: 404"（真实抓取）；baidu → 200 "URL 抓取已启动"（document_id=556，异步 completed） | `src/api/knowledge.js` | 13 |
| 625 | [x] | GET | `/api/knowledge/documents` | 用户端 | query: params | 200 {items,page,page_size,total}；字段 tenant_id/id/product_id/source_type/title/file_*/chunk_count/total_tokens/embed_status/embed_progress；product_id 过滤生效（total=6） | `src/api/knowledge.js` | 17 |
| 626 | [x] | GET | `/api/knowledge/documents/{id}` | 用户端 | path: id / query: params | 200 详情（embed_status=indexed progress=100）；不存在 → 404 NOT_FOUND_1002 "knowledge document not found"；id=abc → 400 "无效的文档ID" | `src/api/knowledge.js` | 20 |
| 627 | [x] | GET | `/api/knowledge/documents/{id}/progress` | 用户端 | path: id | 200 {chunk_count:1,embed_progress:100,embed_status:indexed,error_msg,id,last_index_at,title,total_tokens}（doc 555）；不存在 99999 → 404 NOT_FOUND_1002 "knowledge document not found"；id=abc → 400 INVALID_PARAM_1001 "无效的文档ID" | `src/api/knowledge.js` | 23 |
> **缺陷 D-26（已修复）**：`POST /api/knowledge/products/{product_id}/rebuild-index` 对不存在的产品也返回 200「重建任务已启动」。
> 根因：service `RebuildIndex`（knowledge_service_reindex.go）用 docRepo.List 取文档，空集循环零次返回 nil——
> 静默成功会让前端误以为索引已重建。修复：service 先用已有的 `ragRepo.GetRagProductByID` 校验产品存在性
> （复用 rag_config.go 的 "rag product not found" 语义），controller RebuildProductIndex 补 `isNotFoundError` → 404 分支。
> 验证：不存在产品 → 404 NOT_FOUND_1002 "rag product not found"；真实产品 ecommerce-general-cs → 200 正常重建。

| 628 | [x] | GET | `/api/knowledge/documents/{id}/chunks` | 用户端 | path: id | 200 {items:[{tenant_id,id,document_id,product_id,chunk_index,content,content_hash,token_count,char_count,embedding}...],total}；不存在文档 → 200 {items:[],total:0}（列表端点不校验文档存在性，观察项） | `src/api/knowledge.js` | 26 |
| 629 | [x] | PUT | `/api/knowledge/documents/{id}` | 用户端 | path: id / body: data | 200 更新成功返回完整实体（title 改 v2 回读一致）；不存在 99999 → 404 NOT_FOUND_1002 | `src/api/knowledge.js` | 29 |
| 630 | [x] | DELETE | `/api/knowledge/documents/{id}` | 用户端 | path: id | 200 "删除成功"；重复删 → 404 NOT_FOUND_1002 "knowledge document not found"（软删语义）；测试文档 556 已删 | `src/api/knowledge.js` | 32 |
| 631 | [x] | POST | `/api/knowledge/documents/{id}/reindex` | 用户端 | path: id | 200 "重建任务已启动"（doc 555）；不存在 99999 → 404 NOT_FOUND_1002 | `src/api/knowledge.js` | 35 |
| 632 | [x] | POST | `/api/knowledge/products/{productId}/rebuild-index` | 用户端 | path: product_id | 不存在产品 → **404 NOT_FOUND_1002 "rag product not found"（D-26 修复后；修复前静默 200）**；真实产品 ecommerce-general-cs → 200 "重建任务已启动"（全量重建同步执行，耗时约 30-60s） | `src/api/knowledge.js` | 39 |
> **缺陷 D-27（已修复）**：`POST /api/knowledge/search` 跨产品串数据且结果元信息丢失。
> 修复前 `KnowledgeService.Search` 调的是全库 `ragSearcher.Search(ctx,query,topK)`（无产品过滤），
> 再把请求的 product_id 回填到每条结果上——别的产品的文档被标成本产品返回（实测 doc 4 属于 default、
> doc 548 属于 d6587ab3-…，都被贴上 ecommerce-general-cs）；同时命中记录的 id/similarity_score/weight 全丢（恒 0），
> 同一分段经向量路与 BM25 路各命中一次时还会重复出现两条。
> 修复：改调带产品过滤的 `SearchIndex(ctx, productID, query, topK, nil)`，新增 `dedupeMerchantChunks`（按分段 ID 去重，
> 无 ID 时按 文档+内容）与 `knowledgeChunkFromMerchant`（透传 ID/DocumentID/SimilarityScore，weight 缺省回落 1）。
> 补 `knowledge_search_test.go` 三个单测。验证：复测同产品只返回该产品文档（doc 15/14）、无重复、分数 1/0.359/0.336…；
> 不存在产品返回空 items；on 模式 tenant 5 搜 tenant 1 产品返回空（租户隔离成立）。


| 633 | [x] | GET | `/api/knowledge/products/{productId}/overview` | 用户端 | path: productId | 200 真实产品 ecommerce-general-cs → {total_documents:5,total_chunks:23,total_tokens:9,total_searches:11,today_imports:0,hit_rate:0,embed_status_breakdown{indexed:5},source_type_breakdown{text:4,upload:1},index_health{indexed_docs:5,index_rate:1}}；不存在产品 → 200 全零统计（纯统计查询，非缺陷，观察项） | `src/api/knowledge.js` | 42 |
| 634 | [x] | POST | `/api/knowledge/search` | 用户端 | body: data | 空 body 400 INVALID_PARAM_1001（binding 文案含 ProductID/Query 字段名——观察项）；**修复前跨产品串数据+重复+分数全 0（D-27）**；修复后 200 {items,total,latency_ms}：只含请求产品文档（doc 15/14），无重复，similarity_score 1/0.359/0.336/0.315/0.293，weight=1；不存在产品 → items=[] total=0；**on 模式 tenant 5 搜 tenant 1 产品 → 空（租户隔离）** | `src/api/knowledge.js` | 46 |
| 635 | [x] | GET | `/api/knowledge/import-logs` | 用户端 | query: params | 200 {items:[{tenant_id,id,product_id,document_id,source_type,batch_no,status,operator,ip,user_agent,duration_ms,error_detail,created_at}],page,page_size,total}；page/page_size 生效；**修复前恒为空（D-29）**，修复后成功导入即出记录（id=1 success duration=9ms） | `src/api/knowledge.js` | 50 |
> **缺陷 D-28（已修复）**：`DELETE /api/knowledge/openapi/sources/{id}` 两个问题：
> ①重复删 / 删不存在的 id 回 **500 INTERNAL_ERROR_6002「操作失败，请稍后重试」**——仓储层 `EnsureAffected`
> 已经返回 gorm.ErrRecordNotFound，控制器却统一兜底 500，调用方分不清"删掉了"和"本来就没有"；
> ②`OpenAPIService.DeleteSource` 收了 productID 却直接按 id 删，**产品隔离被丢弃**——同租户其它产品的
> 数据源也能被删掉（越权删除）。
> 修复：先 `GetByProductAndID` 校验归属再删；控制器 `isNotFoundError` → 404「数据源不存在」。
> 验证：重复删 / 不存在 → 404；跨产品删 → 404 且数据源仍在。

| 636 | [x] | GET | `/api/knowledge/openapi/sources` | 用户端 | query: params | 200 {items:[{id,product_id,name,type,rest,enabled,last_status,last_sync_at,...}],total}（历史源 id=3 type=rest last_status=failed） | `src/api/knowledge.js` | 54 |
| 637 | [x] | POST | `/api/knowledge/openapi/sources` | 用户端 | body: data | 空 body → 400 INVALID_PARAM_1001「数据源名称不能为空」；合法 body {product_id,name,type:rest,endpoint,method:GET,auth_type:none,auth_config:"{}",field_mapping:"{}",enabled:1} → 200 创建 id=9 last_status=never | `src/api/knowledge.js` | 57 |
| 638 | [x] | GET | `/api/knowledge/openapi/sources/{id}` | 用户端 | path: id / query: product_id | 200 详情（带 product_id 归属校验）；不存在 99999 → 404 NOT_FOUND_1002 "openapi source not found"；id=abc → 400「无效的数据源ID」 | `src/api/knowledge.js` | 60 |
| 639 | [x] | PUT | `/api/knowledge/openapi/sources/{id}` | 用户端 | path: id / body: data | 200 更新成功（指针语义，只改传入的 name/schedule，其它字段保持）；不存在 → 404「数据源不存在: id=99999」 | `src/api/knowledge.js` | 63 |
| 640 | [x] | DELETE | `/api/knowledge/openapi/sources/{id}` | 用户端 | path: id / query: product_id | 200 删除成功；**不存在/重复删 → 404「数据源不存在」（D-28，修复前 500 INTERNAL_ERROR_6002）；跨产品删除 → 404 且原数据源仍在** | `src/api/knowledge.js` | 66 |
| 641 | [x] | POST | `/api/knowledge/openapi/sources/{id}/sync` | 用户端 | path: id / query: product_id | 不可达端点 → 502 UPSTREAM_FAILURE_5005「同步数据源失败；请检查端点 URL 是否可达」（上游分类正确）；不存在 → 404「数据源不存在」 | `src/api/knowledge.js` | 69 |
| 642 | [x] | POST | `/api/knowledge/openapi/sources/{id}/test` | 用户端 | path: id / body: data | 200 {success:false,error:"dial tcp: lookup api.example.com: no such host",latency_ms:1}（连通性探测按 200 + success 字段回报，不属于接口错误）；不存在 → 404 "openapi source not found" | `src/api/knowledge.js` | 72 |
| 643 | [x] | POST | `/api/knowledge/openapi/sources/{id}/toggle` | 用户端 | path: id / query: product_id | 200「切换成功」（enabled 0↔1）；不存在 → 404 | `src/api/knowledge.js` | 75 |
| 644 | [x] | GET | `/api/knowledge/stats/overview` | 用户端 | query: product_id | 200 {total_documents:5,total_chunks:23,total_searches:17,today_searches:7,today_imports,hit_rate:1,avg_search_latency_ms:1860.5,embed_status_breakdown,source_type_breakdown} | `src/api/knowledge.js` | 79 |
| 645 | [x] | GET | `/api/knowledge/stats/documents` | 用户端 | query: product_id | 200 {overview:{...同 #644},documents_by_type,documents_by_status,recent_documents...}（按来源/状态分组统计） | `src/api/knowledge.js` | 82 |
| 646 | [x] | GET | `/api/knowledge/stats/searches` | 用户端 | query: product_id | 200 {overview:{...},search_trend:[7 天],hot_queries:[{query:"催发货",count:5}],score_histogram,quality_stats}（今日检索 6–7 次） | `src/api/knowledge.js` | 85 |
| 647 | [x] | GET | `/api/knowledge/stats/imports` | 用户端 | query: product_id | 200 {total_imports,success_imports,failed_imports,success_rate,avg_duration_ms,daily_trend:[30 天],recent_logs}；**D-29 前恒为 0（导入日志一条都没落库），修复后 total_imports=1、success_rate=1、recent_logs 有记录** | `src/api/knowledge.js` | 88 |
| 648 | [x] | GET | `/api/knowledge/stats/openapi` | 用户端 | query: product_id | 200 {total_sources,enabled_sources,failed_sources,total_synced,source_list:[...]}；failed_sources 按 last_status=="failed" 统计（历史源正好失败，计数正确） | `src/api/knowledge.js` | 91 |

> **缺陷 D-29（已修复）**：导入日志与导入统计永久为空，不是"没数据"而是**写入全部失败**。
> 表 `knowledge_import_logs.error_detail` 实际列类型是 jsonb，模型却按普通 string 声明；
> 成功态写 `""`、失败态写 `dial tcp ...` 这类非 JSON 文本，Postgres 一律报
> `invalid input syntax for type json`；而 `KnowledgeService.Import` 用 `_ = s.logImport(...)`
> 吞掉错误 → 接口照常 200，日志表恒 0 行、`stats/imports` 恒为 0，用户以为"从没导入过"。
> 修复：列改为 text（与同域批次表一致，语义就是人读文案）；两处调用点改为记录告警而非静默丢弃。
> 验证：导入一段文本后表内出现 success 记录，`stats/imports` total_imports=1、recent_logs 非空，
> `import-logs` 与 `overview.today_imports` 同步可见。
| 649 | [x] | GET | `/api/knowledge-bases` | 用户端 | query: params | 200 {list:[{tenant_id,id,kb_code,type,name,description,owner_type,owner_agent_id,member_count,doc_count,enabled,version,canary_enabled,...}],total}（total=6） | `src/api/knowledgeBase.js` | 4 |
| 650 | [x] | GET | `/api/knowledge-bases/by-type/{kbType}` | 用户端 | path: kbType / query: params | 合法 rag → 200 data 为数组（2 条）；非法 type=person → 400 INVALID_PARAM_1001「type 必须为 faq/rag/sop」（白名单校验） | `src/api/knowledgeBase.js` | 8 |
| 651 | [x] | GET | `/api/knowledge-bases/{id}` | 用户端 | path: id | 200 详情（id=12 r85_kb）；99999 → 404 NOT_FOUND_1002「知识库不存在」；id=abc → 400 INVALID_PARAM_1001「无效的知识库 ID」 | `src/api/knowledgeBase.js` | 12 |
| 652 | [x] | POST | `/api/knowledge-bases` | 用户端 | body: data | 空 body → 400「请求参数错误：请检查字段类型与必填项」（掩码得当，未泄露 Go 字段名）；合法 {kb_code,type:rag,name,description,owner_type:shared} → 200 创建 id=32（enabled=true,version=1,canary_enabled=false） | `src/api/knowledgeBase.js` | 16 |
| 653 | [x] | PUT | `/api/knowledge-bases/{id}` | 用户端 | path: id / body: data | 只传 {name} → 200 且回读 type/description/owner_type/enabled 均保留（部分更新）；不存在 → 404 NOT_FOUND_1002（文案为掩码「操作失败，请稍后重试」——与 GET/stats 的「知识库不存在」不一致，观察项） | `src/api/knowledgeBase.js` | 20 |
| 654 | [x] | DELETE | `/api/knowledge-bases/{id}` | 用户端 | path: id | 删 32 → 200「删除成功」；重复删 / 99999 → 404 NOT_FOUND_1002（文案同上掩码，观察项；状态码正确） | `src/api/knowledgeBase.js` | 24 |
| 655 | [x] | GET | `/api/knowledge-bases/{id}/stats` | 用户端 | path: id | 200 {agent_count:0,item_count:0}（id=12）；99999 → 404「知识库不存在」 | `src/api/knowledgeBase.js` | 28 |
| 656 | [x] | POST | `/api/knowledge-merchant/batch/import` | 用户端 | body: data | 空 body → 400 INVALID_PARAM_1001「请选择目标知识库（产品）后再导入」；合法 {product_id,operator,items:[{title,content,category,tags}]} → 200 {batch_no:"BATCH-20261107004637-81c51922",total:1,accepted:1,rejected:0,document_ids:[559],errors:[]} | `src/api/knowledgeMerchant.js` | 5 |
| 657 | [x] | POST | `/api/knowledge-merchant/batch/upload` | 用户端 | body: formData | 缺 product_id → 400「请选择目标知识库（产品）后再上传导入」；multipart 带 product_id + CSV（表头 title,content,category,tags）→ 200 {batch_no, total:1, accepted:1, document_ids:[560], errors:[]} | `src/api/knowledgeMerchant.js` | 8 |
| 658 | [x] | POST | `/api/knowledge-merchant/playground` | 用户端 | body: data | 空 body → 400「product_id 不能为空」；{product_id,query,top_k:3} → 200 {query,total:3,chunks:[{chunk_id:29,document_id:15,content,score:1,source:"L2",from_cache:false,metadata}]}（产品过滤与打分正确） | `src/api/knowledgeMerchant.js` | 14 |
| 659 | [x] | GET | `/api/knowledge-merchant/documents/{documentId}/chunks` | 用户端 | path: documentId / query: params | 200 {items:[{tenant_id,id,document_id,product_id,chunk_index,content,content_hash,token_count,char_count,embedding_id,similarity_score,hit_count,weight,metadata}],page,page_size,total}；不存在文档 99999 → 200 {items:[],total:0}（列表端点不校验文档存在性，观察项）；abc → 400「无效的文档ID」 | `src/api/knowledgeMerchant.js` | 18 |
| 660 | [x] | PUT | `/api/knowledge-merchant/chunks/{chunkId}` | 用户端 | path: chunkId / body: data | 200「更新成功」（回读一致）；content 空 → 400「内容不能为空」；**不存在 99999 → 404 NOT_FOUND_1002「分段不存在」（D-30，修复前 400 INVALID_PARAM_1001「分段不存在: chunk not found」）**；abc → 400「无效的分段ID」 | `src/api/knowledgeMerchant.js` | 21 |
> **缺陷 D-30（已修复）**：分段写接口把「分段不存在」误回 400 INVALID_PARAM_1001
> （split 还会把原始英文 "chunk not found" 直接抛给前端）。
> 根因：`replyChunkWriteError` 只区分治理直写与通用参数错误，gorm.ErrRecordNotFound 落了参数错误分支。
> 修复：该分支前加 `isNotFoundError` → 404 NOT_FOUND_1002「分段不存在」。PUT/DELETE/split 三处共用，一并生效。
> 验证：三个端点传不存在 id → 404；id 非法仍 400。

| 661 | [x] | DELETE | `/api/knowledge-merchant/chunks/{chunkId}` | 用户端 | path: chunkId | 真实分段 → 200「删除成功」；**不存在/重复删 → 404「分段不存在」（D-30，修复前 400「操作失败，请稍后重试」）** | `src/api/knowledgeMerchant.js` | 24 |
| 662 | [x] | POST | `/api/knowledge-merchant/chunks/{chunkId}/split` | 用户端 | path: chunkId / body: { parts } | 不存在 → 404「分段不存在」（D-30）；**parts 全空白或含空白项 → 400「拆分内容不能为空」且原分段保留（D-31，修复前 200「拆分成功」但原分段已删、新分段零插入——静默丢数据）**；合法 parts → 200「拆分成功」（1 段→2 段） | `src/api/knowledgeMerchant.js` | 27 |
> **缺陷 D-31（已修复，数据丢失级）**：`POST /chunks/{id}/split` 传全空白（或含空白项）的 parts 时
> 先删原分段、后在插入循环里跳过空白段 —— 结果是**接口回 200「拆分成功」，原分段没了、新分段一条没插入**，
> 文档内容被静默清空。修复：在任何删除动作之前先校验 parts 无空白项，返回 400「拆分内容不能为空」。
> 补回归测试 `TestSplitChunk_RejectsBlankPartsBeforeDelete`（全空白/混合空白/段数不足，断言拒绝发生在触碰仓储之前）。
> 验证：空白 parts → 400 且原分段仍在；正常 parts → 200（1 段拆成 2 段）。

| 663 | [x] | POST | `/api/knowledge-merchant/feedback` | 用户端 | body: data | 空 body → 400 INVALID_PARAM_1001「query 不能为空」；rating=5 → 400「rating 必须在 [-1, 0, 1]」；合法 {product_id,query,document_id,chunk_id,rating:1,comment} → 200「反馈已记录」 | `src/api/knowledgeMerchant.js` | 31 |
| 664 | [x] | GET | `/api/knowledge-merchant/feedbacks` | 用户端 | query: params | 200 {items:[{tenant_id,id,product_id,query,query_hash,document_id,chunk_id,rating,comment,operator,session_id,created_at}],page,page_size,stats:{bad,good,neutral,total}}；rating=1&keyword=催 过滤生效（total=1），rating=-1 → total=0 | `src/api/knowledgeMerchant.js` | 34 |
| 665 | [x] | POST | `/api/knowledge-merchant/tokens` | 用户端 | body: data | 空 body → 400「name 不能为空」；{name,product_id,scopes:["read"]} → 200 {id:7,token_plain:kbg_…,message:「Token 已创建，请妥善保存明文（仅显示一次）」,scopes,enabled:1,use_count:0} | `src/api/knowledgeMerchant.js` | 38 |
| 666 | [x] | GET | `/api/knowledge-merchant/tokens` | 用户端 | query: params | 200 {items:[{id,name,scopes,product_id,enabled,expires_at,last_used_at,use_count,created_by,created_at}]}；**不回 token_plain（明文仅创建时返回）**；product_id 过滤生效 | `src/api/knowledgeMerchant.js` | 41 |
| 667 | [x] | POST | `/api/knowledge-merchant/tokens/{tokenId}/revoke` | 用户端 | path: tokenId | 200「已吊销」；重复吊销 200 幂等；不存在 99999 → 404 NOT_FOUND_1002「吊销Token失败」；吊销后该 token 再调用外部导入 → 401 | `src/api/knowledgeMerchant.js` | 44 |
| 668 | [x] | POST | `/api/knowledge-merchant/external/import` | 用户端 | body: data / header: X-Knowledge-Token | 公开入口，仅 X-Knowledge-Token 认证（JWT → 401）：write token → 200 {job_no,status:completed,total:1,accepted:1,document_ids,async:false}；read-only token → 403 FORBIDDEN_2002；缺失/非法 token → 401 UNAUTHORIZED_2001；token 产品范围不符 → 403；**D-32：修复前无 token 空 body 先报 400「source 不能为空」（鉴权前泄露字段规则），修复后一律 401** | `src/api/knowledgeMerchant.js` | 48 |
> **缺陷 D-32（已修复）**：`POST /api/knowledge-merchant/external/import` 是免 JWT 的公开入口，
> 但控制器与 service 都先校验 `source`/`product_id`、后验 `X-Knowledge-Token` —— 未带 token 的空请求回
> 400「source 不能为空」，**在鉴权之前就暴露了字段规则**，也让人误以为要先去补参数而不是先拿 token。
> 修复：service 里把 ValidateToken / 租户钉定 / scope 校验提到最前，通过后才校验业务字段；
> 控制器删掉重复的字段校验（避免顺序被再次打乱）。
> 验证：无 token 的空 body / 缺 product_id → 401（修复前 400）；合法 write token → 200；read-only token → 403。

| 669 | [x] | GET | `/api/knowledge-merchant/external/jobs` | 用户端 | query: params | 200 {items:[{id,job_no,product_id,source,total_items,done_items,failed_items,status,payload,error_detail,operator,started_at,finished_at}],page,page_size,total}；异步导入（sync:false）落 job 行并列出（completed, done_items=1）；同步导入（sync:true）不建 job 行（返回的 job_no 不入列表，观察项） | `src/api/knowledgeMerchant.js` | 53 |
| 670 | [x] | GET | `/api/kuaishou/list` | 用户端 | query: page, page_size | 200 {list:[{id,title,description,image_url,redirect_url,domain_pool_id,short_link_url,short_code,tags,view_count,like_count,share_count,is_active,created_at,updated_at}]}；page/page_size 生效 | `src/api/kuaishouCard.js` | 4 |
| 671 | [x] | GET | `/api/kuaishou/{id}` | 用户端 | path: id | 200 详情；不存在 99999 → 404 NOT_FOUND_1002「资源不存在」 | `src/api/kuaishouCard.js` | 8 |
| 672 | [x] | POST | `/api/kuaishou/create` | 用户端 | body: data | 空 body → 400 INVALID_PARAM_1001（message 带 Go 结构体字段名 Key:'KuaishouCardCreateRequest.Title'...——BindJSON 的 A20 刻意口径，观察项）；**tags 是 string 不是数组**（传数组 400 unmarshal）；合法 → 200 创建 id=37 short_code=4hpd1C | `src/api/kuaishouCard.js` | 12 |
| 673 | [x] | PUT | `/api/kuaishou/update` | 用户端 | body: data | 200「更新成功」（title/is_active 生效）；不存在 999999 → 404「记录不存在」；**title 必填（只传 id+domain_pool_id 会 400 required）**；**D-33 修复前：任何更新都会删除旧短链并生成新 short_code（4hpd1C→pncrED），已分发的 /s/ 链接失效；修复后改标题 code 不变，仅 domain_pool_id 变化时重建（ZozfwJ→DeBSg3）** | `src/api/kuaishouCard.js` | 16 |
> **缺陷 D-33（已修复）**：快手卡片的「更新」每次都会删除旧短链并生成新 short_code——
> 用户只改标题/描述/启用状态，已分发出去的 `/s/xxxxxx` 推广链接就全部失效。
> 对照抖音卡片实现（只在 domain_pool_id 变化时重建），修复：`kuaishouCardService.Update`
> 记录 originalDomainPoolID，仅在域名池变化或历史卡片无短链时才调 `GenerateShortLink`。
> 验证：改 title → short_code 不变；改 domain_pool_id → 重建新码；显式 generate-short-link 仍可用。

| 674 | [x] | DELETE | `/api/kuaishou/delete/{id}` | 用户端 | path: id | 200「删除成功」；重复删 → 404 NOT_FOUND_1002「记录不存在」 | `src/api/kuaishouCard.js` | 20 |
| 675 | [x] | GET | `/api/kuaishou/view/{id}` | 用户端 | path: id | 200 返回卡片详情（路由 /kuaishou/view/:id → GetByID，不记浏览数；真实浏览计数走 /s/{short_code} 重定向链路，观察项） | `src/api/kuaishouCard.js` | 24 |
| 676 | [x] | POST | `/api/kuaishou/like/{id}` | 用户端 | path: id | 200「操作成功」 | `src/api/kuaishouCard.js` | 28 |
| 677 | [x] | POST | `/api/kuaishou/share/{id}` | 用户端 | path: id | 200 返回卡片实体 | `src/api/kuaishouCard.js` | 32 |
| 678 | [x] | GET | `/api/kuaishou/stats/card/{id}` | 用户端 | path: id / query: start_date?, end_date? | 200 {totalViews,dailyStats,recentActivity}；默认区间近 30 天（历史浏览 9/30 区间外 → totalViews=0 但 recentActivity 有记录）；带 start_date/end_date 查询正确（totalViews:1, dailyStats:[{date:2026-09-30,view:1}]） | `src/api/kuaishouCard.js` | 36 |
| 679 | [x] | GET | `/api/kuaishou/stats/overall` | 用户端 | - | 200 {totalCards:20,activeCards:17,totalViews,popularCards:[{id,title,viewCount,createdAt}...]}（totalViews 受默认 30 天区间影响，观察项） | `src/api/kuaishouCard.js` | 40 |
| 680 | [x] | POST | `/api/kuaishou/{id}/generate-short-link` | 用户端 | path: id | 不存在 → 404「资源不存在」；真实路径 → 200 返回卡片实体且 short_code 更新（2eGfxD）；与 #673 修复联动：仅显式调用或域名池变化时重建 | `src/api/kuaishouCard.js` | 44 |
| 681 | [x] | GET | `/api/lead-mining/config` | 用户端 | - | 200 {id:1,enabled:true,keywords:[],tags:[],requirement:"",channels:[],min_intent_score:50,model,created_at,updated_at} | `src/api/leadMining.js` | 4 |
| 682 | [x] | POST | `/api/lead-mining/config` | 用户端 | body: data | 空 body → 400「请求体不能为空：至少提供一个配置字段（如 keywords、requirement）」；部分更新 {min_intent_score:60} → 200 生效（响应 data 非完整快照 id:0/enabled:false，观察项）；测后还原 50 | `src/api/leadMining.js` | 8 |
| 683 | [x] | GET | `/api/live-codes/list` | 用户端 | query: page, page_size | 空表 200 {list:[],total:0}；创建后 total=1 | `src/api/livecode.js` | 4 |
| 684 | [x] | GET | `/api/live-codes/{id}` | 用户端 | path: id | 不存在 99999 → 404 NOT_FOUND_1002「记录不存在」；真实 → 200 详情（含 short_domain 嵌套对象） | `src/api/livecode.js` | 8 |
| 685 | [x] | POST | `/api/live-codes/create` | 用户端 | body: data | 空 body → 400（binding 文案带 Go 结构体名 CreateLiveCodeRequest.Name/...required，A20 刻意口径观察项）；必填 name/short_link/short_domain_id/entry_domain_id/landing_domain_id；用 domain_pool 可用域名（132/128/116）→ 200 创建 id=b879990a | `src/api/livecode.js` | 12 |
| 686 | [x] | PUT | `/api/live-codes/{id}/update` | 用户端 | path: id / body: data | 200 更新成功（name 改 v2 回读生效） | `src/api/livecode.js` | 16 |
| 687 | [x] | DELETE | `/api/live-codes/{id}/delete` | 用户端 | path: id | 200「删除成功」；重复删 → 404 NOT_FOUND_1002「记录不存在」 | `src/api/livecode.js` | 20 |
| 688 | [x] | GET | `/api/live-codes/{id}/stats` | 用户端 | path: id | 200 {live_code_id,total_views,today_views,total_clicks,today_clicks,total_qr_shown,total_qr_clicks,active_qr_count,total_qr_count,conversion_*}（新活码全 0） | `src/api/livecode.js` | 24 |
| 689 | [x] | GET | `/api/live-codes/{liveCodeId}/qrcodes` | 用户端 | path: id | 200 data=[]（无二维码）；创建后返回 1 条 | `src/api/livecode.js` | 28 |
| 690 | [x] | POST | `/api/live-codes/{liveCodeId}/qrcodes/create` | 用户端 | path: id / body: data | 空 body {} → 200 生成成功（默认参数即可）；返回 {id,live_code_id,image_url:"/uploads/qrcodes/*.png",qr_image_url,...}（PNG 落盘 uploads/qrcodes/） | `src/api/livecode.js` | 32 |
| 691 | [x] | GET | `/api/live-codes/qrcodes/{qrId}/stats` | 用户端 | path: qrId | 200 {qr_code_id,expire_days:30,view_count,click_count,expire_time:+30d,status:1,is_expired:false,is_daily_limit_reached:false,...} | `src/api/livecode.js` | 36 |
| 692 | [x] | POST | `/api/live-codes/{id}/share` | 用户端 | path: id / body: { platform } | {platform:wechat} → 200「分享成功」{short_link:"https://s10.example.com/apitest685",entry_link,landing_link,qr_image_path}（短链/落地/承接三段域名链路完整） | `src/api/livecode.js` | 40 |
| 693 | [x] | DELETE | `/api/live-codes/qrcodes/{id}/delete` | 用户端 | path: qrId | 200「删除成功」；重复删 → 404 NOT_FOUND_1002「二维码不存在」 | `src/api/livecode.js` | 44 |
| 694 | [x] | PUT | `/api/live-codes/qrcodes/{id}/update` | 用户端 | path: qrId / body: data | 200「更新成功」（name 改 apitest-qr） | `src/api/livecode.js` | 48 |
| 695 | [x] | GET | `/api/reports/llm/overview` | 用户端 | query: params | 200 {call_count:18,error_count:8,error_rate:0.44,cache_hit_rate,prompt_tokens:35095,completion_tokens:339,input_cost_cents,output_cost_cents,total_cost_cents,avg_latency_ms:605,p50_latency_ms,p95_latency_ms:1262}；on 模式 tenant 5 全 0（隔离正确） | `src/api/llmReport.js` | 7 |
| 696 | [x] | GET | `/api/reports/llm/timeseries` | 用户端 | query: params | 200 按日数组 [{date:"2026-10-31",call_count:2,error_count:2,error_rate:1,...},...]（10/31–11/06 真实调用数据） | `src/api/llmReport.js` | 11 |
| 697 | [x] | GET | `/api/reports/llm/breakdown` | 用户端 | query: by=model\|scenario\|channel\|error_class\|provider | 缺 by → 400 INVALID_PARAM_1001「by 参数仅支持 model\|scenario\|channel\|error_class\|provider」；by=model → 200 [{dim:"deepseek-chat",call_count:10,error_count:0,...},{dim:"qwen-max",...error_rate:1},...] | `src/api/llmReport.js` | 15 |
| 698 | [x] | GET | `/api/reports/llm/errors` | 用户端 | query: params | 200 {by_class:[{dim,error_class,call_count,error_count,error_rate,...}],page,page_size,rows:[{trace_id,scenario,provider,model,error_class:"auth",error_msg,...}]}；limit 生效（真实历史 401 invalid_api_key 错误明细） | `src/api/llmReport.js` | 19 |
| 699 | [x] | GET | `/api/llm/usage/daily` | 用户端 | query: window, group_by | 200 按日数组 [{stat_date,scenario,provider,call_count,failed_count,total_tokens,total_cost,avg_latency_ms}]；window=7d&group_by=model 生效；缺 group_by 默认返回（不报错） | `src/api/llmRouting.js` | 7 |
| 700 | [x] | GET | `/api/llm/models` | 用户端 | - | 200 [{name,display_name,base_url,model,api_key_set,enabled,key_invalid,quality_score,max_rpm,cost_per_1k_input/output,separate_pricing,vendor,source,source_available,...}]（deepseek/default 等 9 个） | `src/api/llmRouting.js` | 11 |
| 701 | [x] | GET | `/api/llm/models/{name}` | 用户端 | path: name | 200 详情同列表字段；不存在 → 404 "model not found" | `src/api/llmRouting.js` | 15 |
| 702 | [x] | POST | `/api/llm/models` | 用户端 | body: data | 空 body → 400 "name is required"；合法 {name,display_name,base_url,model,vendor} → 200 创建 apitest-model（enabled=false 默认，api_key_set=false） | `src/api/llmRouting.js` | 19 |
| 703 | [x] | PUT | `/api/llm/models/{name}` | 用户端 | path: name / body: data | 200 返回 name（display_name/max_rpm 部分更新生效）；不存在 → 400 "provider \"no-such\" not found"（注意是 400 非 404，观察项） | `src/api/llmRouting.js` | 23 |
| 704 | [x] | DELETE | `/api/llm/models/{name}` | 用户端 | path: name | 200 删除；重复删 → 404 "model not found" | `src/api/llmRouting.js` | 27 |
| 705 | [x] | PUT | `/api/llm/models/{name}` | 用户端 | path: name / body: { enabled } | 200 返回 name（enabled=false 切换生效，与 #703 同端点） | `src/api/llmRouting.js` | 31 |
| 706 | [x] | POST | `/api/llm/models/{name}/test` | 用户端 | path: name / body: { prompt } | 200 {success:false,error:"dial tcp 127.0.0.1:9999: connect: connection refused",latency_ms,content}（连通性探测 200+success 字段）；不存在 → 404 "model not found" | `src/api/llmRouting.js` | 35 |
| 707 | [x] | GET | `/api/llm/scene-routing` | 用户端 | - | 200 [{scenario,provider,fallbacks,cost_weight,max_latency,min_quality,version,weight}]（intent_recognize→sensenova/local-mlx 等） | `src/api/llmRouting.js` | 39 |
| 708 | [x] | GET | `/api/llm/scenarios` | 用户端 | - | 200 [{scenario,model,provider,fallbacks,version,weight}]（场景→当前模型映射） | `src/api/llmRouting.js` | 43 |
| 709 | [x] | PUT | `/api/llm/scene-routing` | 用户端 | body: payload | 200 更新 {scenario,provider,fallbacks}（version 递增 9→10；回读确认生效）；测后还原 sensenova | `src/api/llmRouting.js` | 48 |
| 710 | [x] | GET | `/api/llm/fallback` | 用户端 | - | 200 [{scenario,provider,fallbacks,version}]（有 fallback 的场景清单） | `src/api/llmRouting.js` | 52 |
| 711 | [x] | GET | `/api/llm/audit` | 用户端 | query: params | 200 [{action:"update_strategy",scenario,new_provider,prev_provider,new_fallbacks,operator,trace_id,version,...}]（倒序，operator=system） | `src/api/llmRouting.js` | 58 |
| 712 | [x] | GET | `/api/llm/stats` | 用户端 | - | 200 data={}（聚合空对象；真实数据走 #713/#714） | `src/api/llmRouting.js` | 62 |
| 713 | [x] | GET | `/api/llm/usage` | 用户端 | query: window | 200 {total_calls:29,total_success:29,total_failed:0,total_tokens:103428,total_cost:0,active_models:9,enabled_models:1,window_label:"24h",by_scenario:[...]} | `src/api/llmRouting.js` | 66 |
| 714 | [x] | GET | `/api/llm/cost-stats` | 用户端 | query: window | 200 {active_models,by_model:[{provider,call_count,total_tokens,total_cost}],by_provider,by_scenario,...} | `src/api/llmRouting.js` | 70 |
| 715 | [x] | GET | `/api/llm/health` | 用户端 | - | 200 {healthy_providers:0,providers:[{name,base_url,enabled,healthy,circuit_open,error_count,last_error,quality,vendor}]}（deepseek 熔断 open/health 404，local default 未启用） | `src/api/llmRouting.js` | 74 |
| 716 | [x] | GET | `/api/llm/model-type-stats` | 用户端 | query: window | 200 {by_provider_type:[{model_type:"local\|cloud",call_count,total_tokens,total_cost}],self_sufficiency,window}（cloud call_count=29） | `src/api/llmRouting.js` | 78 |
| 717 | [x] | GET | `/api/llm/egress-audit` | 用户端 | query: params | 200 {alerts:[],count:0} | `src/api/llmRouting.js` | 82 |
| 718 | [x] | GET | `/api/llm/egress-alerts` | 用户端 | query: params | 200 {alerts:[],count:0} | `src/api/llmRouting.js` | 86 |
| 719 | [x] | GET | `/api/manage/ltc/config` | 管理端 | - | 200 {cache_ttl_seconds:60,degraded:false,enabled:true,guarded_routes:{bill:0,collection:0,opportunity:0,outreach:0,payment:0,quote:1},guarded_total:1,kv_key:"ltc.config",max_bytes:8192,reach_rollout:{mode:shadow},reading_hints,source:kv,stage_status[6],stages_enabled{6},thresholds{4}}；无 token → 401 UNAUTHORIZED_2001 | `src/api/ltcConfig.js` | 11 |
| 720 | [x] | PUT | `/api/manage/ltc/config` | 管理端 | body: cfg | 严格校验（ParseLTCConfig 整份拒收）：缺 stages_enabled → 400「六阶段开关必须显式给出」；thresholds 空 → 400「缺省阈值会塌成 0」；不认识的阶段名 colleciton → 400 列出六阶段（防拼写错误静默无效）；不认识的阈值键 score → 400「合并成单一分数被 C5 禁止」；缺某项阈值 → 400「四项必须逐条给出」；**完整 body（enabled+stages_enabled+thresholds 四项）→ 200「ltc.config 已更新」data={audit_written:true,effective:{...}}**；测后按原值还原 | `src/api/ltcConfig.js` | 21 |
| 721 | [x] | GET | `/api/ltc-rates` | 用户端 | - | 200 {closure:{rate,numerator,denominator},collection:{...},overdue:{...}}（三率初始 0/0/0）；无 token → 401 | `src/api/ltcRates.js` | 5 |
| 722 | [x] | GET | `/api/marketing-flows` | 用户端 | query: params | 200 {list:[],page:1,page_size:10,total:0}（测试前空表） | `src/api/marketingFlow.js` | 4 |
| 723 | [x] | GET | `/api/marketing-flows/{id}` | 用户端 | path: id | 200 详情（id=6 draft，含 flow_data/trigger_config 字符串字段）；不存在 99999 → 404 NOT_FOUND_1002「记录不存在」 | `src/api/marketingFlow.js` | 7 |
| 724 | [x] | POST | `/api/marketing-flows` | 用户端 | body: data | 空 body → 400（binding 带 Go 结构体名 CreateFlowRequest.Name/TriggerType required，A20 观察项）；**必填 name+trigger_type（枚举 user_follow/message_receive/lead_create/lead_status_change/timer/tag_change）；flow_data.nodes 必须含 trigger 节点**，否则 400「流程必须有触发器节点」→ 合法创建 id=6 status=draft version=1 | `src/api/marketingFlow.js` | 10 |
| 725 | [x] | PUT | `/api/marketing-flows/{id}` | 用户端 | path: id / body: data | 200「更新成功」（name/description 生效，flow_data 保留） | `src/api/marketingFlow.js` | 13 |
| 726 | [x] | DELETE | `/api/marketing-flows/{id}` | 用户端 | path: id | 200「删除成功」；重复删 → 404「记录不存在」 | `src/api/marketingFlow.js` | 16 |
| 727 | [x] | POST | `/api/marketing-flows/{id}/activate` | 用户端 | path: id | 200「激活成功」（draft→active） | `src/api/marketingFlow.js` | 19 |
| 728 | [x] | POST | `/api/marketing-flows/{id}/pause` | 用户端 | path: id | 200「暂停成功」（active→paused）；暂停后 trigger → 400「流程未激活」 | `src/api/marketingFlow.js` | 22 |
| 729 | [x] | POST | `/api/marketing-flows/{id}/stop` | 用户端 | path: id | 200「停止成功」 | `src/api/marketingFlow.js` | 25 |
| 730 | [x] | GET | `/api/marketing-flows/{id}/executions` | 用户端 | path: id / query: page, page_size | 200 {list:[{id,flow_id,trigger_id:"manual-54",user_id,status,current_node,execution_data,error_message,started_at,completed_at}],page,page_size,total}（手动触发产生 running→failed 执行记录，error 归因「节点 发消息 执行失败：动作类型未指定」） | `src/api/marketingFlow.js` | 28 |
| 731 | [x] | GET | `/api/marketing-flows/{id}/stats` | 用户端 | path: id | 200 统计 {failed:1}（与执行记录一致） | `src/api/marketingFlow.js` | 31 |
| 732 | [x] | POST | `/api/marketing-flows/{id}/trigger` | 用户端 | path: id / body: {} | 200「流程已触发」data={id,flow_id,trigger_id:"manual-54",status:running}；**仅激活的流程可触发**（paused → 400「流程未激活」）；执行为异步，节点动作类型未知时执行记录标 failed | `src/api/marketingFlow.js` | 48 |
| 733 | [x] | GET | `/api/material/list` | 用户端 | query: params | 200 {list:[],total:0,page:1,limit:20}；上传后 total=1 | `src/api/material.js` | 4 |
| 734 | [x] | UPLOAD | `/api/material/upload` | 用户端 | body: formData | 缺 category_id → 400「请选择素材分类」；PNG + category_id → 200「上传成功」data={id,name,type:image,url:"/files/materials/2026/11/*.png",size,mime_type,hash,provider:obs,storage_path,license_id,user_id}；**环境前置：需在 obs_config（单数表名）配置一行 is_default=true 的存储，否则 500「上传素材失败」（D-34 观察项：前置配置缺失错误分类欠友好）** | `src/api/material.js` | 8 |
| 735 | [x] | DELETE | `/api/material/{id}` | 用户端 | path: id | 200 删除成功 data={deleted:true}；重复删 → 404 NOT_FOUND_1002「记录不存在」 | `src/api/material.js` | 12 |
| 736 | [x] | GET | `/api/material/categories` | 用户端 | - | 200 {list:[],total:0,page:1,limit:50}；创建后 1 条 | `src/api/material.js` | 16 |
| 737 | [x] | POST | `/api/material/categories` | 用户端 | body: data | 空 body → 400 INVALID_PARAM_1001「参数错误」；{name,type:image} → 200 创建 id=491956d2（license_id=system_admin,user_id,material_count:0） | `src/api/material.js` | 20 |
| 738 | [x] | PUT | `/api/material/categories/{id}` | 用户端 | path: id / body: data | 200 更新成功（改名回读生效）；不存在 → 404「记录不存在」 | `src/api/material.js` | 24 |
| 739 | [x] | DELETE | `/api/material/categories/{id}` | 用户端 | path: id | 200 删除成功 data={deleted:true} | `src/api/material.js` | 28 |
| 740 | [x] | GET | `/api/material/selector` | 用户端 | query: params | 200 {categories:[],materials:[],total:0}（选择器聚合视图） | `src/api/material.js` | 32 |
| 741 | [x] | POST | `/api/material/{id}/usage` | 用户端 | path: id / body: data | 200「更新成功」（usage_count +1，#742 today_usage_count 同步） | `src/api/material.js` | 36 |
| 742 | [x] | GET | `/api/material/stats` | 用户端 | - | 200 {total_materials:1,image_count:1,video_count,audio_count,file_count,total_size:69,total_usage_count:1,today_added_count:1,today_usage_count:0} | `src/api/material.js` | 40 |
| 743 | [x] | GET | `/api/mcp/credentials` | 用户端 | - | 200 data=[{id,client_id:"mcp-5fa0f9cd",name,enabled,last_used_at,created_at}]（4 条历史凭据；不回 api_key） | `src/api/mcpCredential.js` | 5 |
| 744 | [x] | POST | `/api/mcp/credentials` | 用户端 | body: data | 空 body 也允许创建（name 可空）→ 200 {id,client_id:"mcp-b56ba58d",api_key:"wgk_…",api_hint:"API Key 只显示这一次，请立即保存"}；带 name 创建 id=9 | `src/api/mcpCredential.js` | 6 |
| 745 | [x] | PUT | `/api/mcp/credentials/{id}` | 用户端 | path: id / body: data | 200「已更新」data={ok:true}（name/enabled 生效）；不存在 99999 → 404 NOT_FOUND_1002（文案掩码「操作失败，请稍后重试」，观察项） | `src/api/mcpCredential.js` | 7 |
| 746 | [x] | DELETE | `/api/mcp/credentials/{id}` | 用户端 | path: id | 200「已删除」；重复删 → 404 NOT_FOUND_1002「MCP 凭据不存在」（测试凭据 id=8/9 已清理） | `src/api/mcpCredential.js` | 8 |
| 747 | [x] | GET | `/api/menus` | 用户端 | - | 200 {all:true,tree:[{key,title,icon,roles,path?,children:[...]}]}（当前用户 admin 可见全量菜单树） | `src/api/menu.js` | 12 |
| 748 | [x] | GET | `/api/system/menus` | 用户端 | - | 200 {tree:[...]}（完整菜单注册表，el-tree 数据源；与 #747 同构但 all 语义不同） | `src/api/menu.js` | 17 |
| 749 | [x] | GET | `/api/system/roles/{code}/menus` | 用户端 | path: code | 200 {menu_keys:[...]}（admin 角色 40+ 键）；非法角色 nope/viewer → 400「角色不存在」（注册角色仅 admin/manager/sales/viewer/staff/customer_service 等，DB 仅有 staff/customer_service 行） | `src/api/menu.js` | 22 |
| 750 | [x] | PUT | `/api/system/roles/{code}/menus` | 用户端 | path: code / body: { menu_keys } | admin 角色 → 400「超管拥有全部菜单权限，无需分配」；staff 角色 → 200「保存角色菜单权限成功」{menu_keys:["workspace","inbox","approvalTask"]} 覆盖保存生效（测后已删除测试行还原） | `src/api/menu.js` | 27 |
| 751 | [x] | POST | `/api/message-hub/push` | 用户端 | body: data | 缺 platform → 400「invalid platform: 」；platform 枚举 wecom/personal_wx/douyin/kuaishou/xiaohongshu/xianyu/tiktok/jd/bilibili/weibo/wechat/weixin…；缺 account_id/msg_id/direction/msg_type 各 400（错误码 INVALID_PARAM_1001）→ 合法 {platform:douyin,account_id,msg_id,direction:outbound,msg_type:text,content,...} → 200「推送成功」data=完整 MessageHub（id=1489 status=pending） | `src/api/messageHub.js` | 5 |
| 752 | [x] | POST | `/api/message-hub/push-batch` | 用户端 | body: data | **请求体是顶层数组**（[{...},{...}]，传 {messages:[...]} 400「请求参数错误」）→ 200「批量推送完成」data={items:[...],errors:[null,null],success_count:2,failed_count:0}（逐条幂等写入） | `src/api/messageHub.js` | 9 |
| 753 | [x] | POST | `/api/message-hub/push-from-channel` | 用户端 | body: data | 同 push 语义（渠道入站转发）→ 200「推送成功」（direction=inbound，id=1492；平台枚举与 direction/msg_type 校验同 #751） | `src/api/messageHub.js` | 13 |
| 754 | [x] | GET | `/api/message-hub/list` | 用户端 | query: params | 200 {list,page,page_size,total:575}；page/page_size 生效；platform/direction/account_id 过滤参数可传 | `src/api/messageHub.js` | 17 |
| 755 | [x] | GET | `/api/message-hub/{id}` | 用户端 | path: id | 200 详情（完整实体）；不存在 → 404 | `src/api/messageHub.js` | 21 |
| 756 | [x] | POST | `/api/message-hub/{ids[0]}/read` | 用户端 | path: id / body: { ids } | 200「标记已读成功」data={updated:1}（body {ids:[…]}） | `src/api/messageHub.js` | 25 |
| 757 | [x] | POST | `/api/message-hub/{id}/recall` | 用户端 | path: id | inbound 消息 → 400 INVALID_PARAM_1001「only outbound messages can be recalled」；outbound 桥接渠道 → **409 MESSAGE_RECALL_UNSUPPORTED_8201「recall not supported for channel（桥接渠道 msgID 是合成键，不是平台消息标识）」**（错误码分类精确，B2 幂等语义见前端注释） | `src/api/messageHub.js` | 39 |
| 758 | [x] | GET | `/api/message-hub/stats` | 用户端 | query: params | 200 {total:575,inbound:435,outbound:140,unread:400,by_platform:{bilibili:7,douyin:133,jd:11,personal_wx:7,telegram:302,tiktok:2,wecom:1,weibo:7,xiaohongshu:105},by_direction,by_msg_type,by_account} | `src/api/messageHub.js` | 43 |
| 759 | [x] | GET | `/api/message-hub/platforms` | 用户端 | - | 200 {msg_types:[audio,card,event,file,image,link,location,text,video],platforms:[bilibili,custom,dingtalk,douyin,email,feishu,jd,kuaishou,personal_wx,qq,sms,telegram,tiktok,web_embed,wechat,wecom,weibo,...]} | `src/api/messageHub.js` | 47 |
| 760 | [x] | GET | `/api/module-market/list` | 公开 | - | 200（公开）{list:[{module_key:"core",name:"系统基座",category:"base",price_cents:0,billing_cycle,icon,summary,depends_on,menu_count:30,owned:true,owned_status},...]} | `src/api/moduleMarket.js` | 17 |
| 761 | [x] | GET | `/api/module-market/detail/{key}` | 公开 | path: key | **需 token**（无 token → 401 UNAUTHORIZED_2001，公开路由里唯一例外）；core → 200 详情同 #760 条目；不存在 → code=4302「模块不存在」 | `src/api/moduleMarket.js` | 20 |
| 762 | [x] | GET | `/api/module-market/payment-guide` | 公开 | - | 200 {payment_guide:"订单已提交，请联系平台运营完成付款；付款确认后权益自动开通。"} | `src/api/moduleMarket.js` | 23 |
| 763 | [x] | GET | `/api/tenant/regulatory-notice` | 公开 | - | 200 {agreement_version:"2026-10-17.v1",notice:"本平台为软件产品服务方，不参与商户与最终客户之间的具体交易..."} | `src/api/moduleMarket.js` | 26 |
| 764 | [x] | POST | `/api/module-market/order` | 公开 | body: data | code=4303「平台未连接，无法办理模块开通」（fail-closed，需平台连接后才能下单） | `src/api/moduleMarket.js` | 31 |
| 765 | [x] | GET | `/api/module-market/orders` | 公开 | query: params | 200 {list:[{tenant_id:30,order_no:"ORD...ZDFCCA",client_order_no,order_type:"subscription",status:"fulfilled",...}],...}（历史租户订单） | `src/api/moduleMarket.js` | 36 |
| 766 | [x] | GET | `/api/module-market/orders/{orderNo}` | 公开 | path: orderNo | 200 订单详情（真实 order_no）；不存在 → code=4004「订单不存在」 | `src/api/moduleMarket.js` | 39 |
| 767 | [x] | GET | `/api/module-market/orders/{orderNo}/pay-status` | 公开 | path: orderNo | code=4303「平台未连接…」（需平台连接查询支付状态） | `src/api/moduleMarket.js` | 43 |
| 768 | [x] | POST | `/api/module-market/orders/{orderNo}/pay` | 公开 | path: orderNo | code=4303「平台未连接…」（fail-closed） | `src/api/moduleMarket.js` | 48 |
| 769 | [x] | GET | `/api/subscription/status` | 用户端 | - | 200 {status:"trial",trial_expires_at:"2026-11-07T22:36:33+08:00",price_cents:900,fetched_at,locked:false,no_data:false,stale:true}（快照缓存语义；on 模式 tenant 5 同构） | `src/api/moduleMarket.js` | 56 |
| 770 | [x] | POST | `/api/subscription/renew` | 用户端 | body: data | code=4303「平台未连接，无法办理模块开通」（续费 fail-closed，同 #764） | `src/api/moduleMarket.js` | 59 |
| 771 | [x] | GET | `/api/modules/my` | 用户端 | - | 200 {list:[{module_key:"core",name:"系统基座",category:"base",price_cents:0,billing_cycle,icon,summary,depends_on,menu_keys:[system,siteConfig,...],...}]}（租户已开通模块及其菜单键） | `src/api/moduleMarket.js` | 64 |
| 772 | [x] | POST | `/api/modules/sync` | 用户端 | body: undefined | 200 {active_count:235,source:"cache",degraded:false,message:""}（模块权益同步，无平台连接时走缓存） | `src/api/moduleMarket.js` | 67 |
| 773 | [x] | GET | `/api/modules/sync-logs` | 用户端 | query: params | 200 {list:[{tenant_id,id:7811,action:"sync",status:"success",module_key,active_count:235,source:"cache",error_msg,duration_ms:1,created_at},...]}（倒序；#772 触发的记录入列） | `src/api/moduleMarket.js` | 70 |
| 774 | [x] | GET | `/api/llm/wallet` | 用户端 | - | 200 {merchant_key:"",input_tokens_remaining:0,output_tokens_remaining:0,low:false,expired:false,exists:false,platform_enabled:false,local_remainder:0}（无平台/无钱包时零值语义） | `src/api/moduleMarket.js` | 78 |
| 775 | [x] | GET | `/api/llm/plans` | 用户端 | - | 200 {list:[],total:0}（无平台连接时套餐列表为空） | `src/api/moduleMarket.js` | 81 |
| 776 | [x] | POST | `/api/llm/recharge` | 用户端 | body: data | 空 body / 合法 body → code=4303「平台未连接，无法办理模块开通」（fail-closed，同 #764） | `src/api/moduleMarket.js` | 84 |
| 777 | [x] | GET | `/api/monitor/trace-tree` | 用户端 | query: params | 200 {trace_id:"",conversation_id:"",channel:"",account_id:"",spans:null}（无 trace_id 参数时空结构；需带 trace_id 查询具体链路树） | `src/api/monitor.js` | 5 |
| 778 | [x] | GET | `/api/monitor/traces` | 用户端 | query: params | 200 [{trace_id:"tr-bc6a8377...",conversation_id:"sess_1793962971700691000_6bfbc21e",channel:"douyin",node_count:1,abnormal_count:0,first_at,last_at},...]（真实 trace 列表；page/page_size 生效） | `src/api/monitor.js` | 8 |
| 779 | [x] | GET | `/api/monitor/lifecycle` | 用户端 | query: params | 200 [{trace_id,conversation_id,node:"outbound_enqueue",node_label:"AI 回复出站入队",node_order:3,channel,direction,msg_id,input,output,...}]（按 trace_id 返回节点生命周期明细，带输入输出快照） | `src/api/monitor.js` | 11 |
| 780 | [x] | GET | `/api/monitor/health` | 用户端 | - | 200 {generated_at,inbound_rate_per_min:0.017,outbound_rate_per_min:0.05,pending_count:79,oldest_pending_min:96904,delivered_count:53,failed_count:0,...}（消息管道健康快照） | `src/api/monitor.js` | 14 |
| 781 | [x] | GET | `/api/monitor/node-health` | 用户端 | - | 200 {nodes:[{channel,node:"agent_turn",node_label:"Agent 一轮推理",total:4,abnormal:0,abnormal_rate,avg_duration_ms,p95_duration_ms,last_abnormal_at},...]} | `src/api/monitor.js` | 17 |
| 782 | [x] | GET | `/api/monitor/latency` | 用户端 | - | 200 []（当前窗口无延迟样本；结构为按时段/节点聚合数组） | `src/api/monitor.js` | 20 |
| 783 | [x] | GET | `/api/monitor/anomalies` | 用户端 | query: params | 200 {sync_gap:[{conversation_id,channel,message_count:4}...],stuck_reachable:[{conversation_id:"sess_1786878058431964190_9579dcd6",channel:"douyin",age_min:494}...]}（同步缺口/卡死会话异常检测） | `src/api/monitor.js` | 23 |
| 784 | [x] | GET | `/api/monitor/trace-eval/logs` | 用户端 | query: params | 200 [{tenant_id,id:571,trace_id,conversation_id,channel,score,dimensions:"{}",reason:"评估出错: LLM 返回无 JSON:...",...}]（真实评估日志，含 LLM 失败原因；limit 生效） | `src/api/monitor.js` | 26 |
| 785 | [x] | GET | `/api/monitor/knowledge-weights` | 用户端 | query: params | 200 {data:[{content:"云雀保温杯产品说明..."}]}（知识权重召回明细快照，monitor.js L29） | `src/api/monitor.js` | 29 |
| 786 | [x] | POST | `/api/monitor/trace-eval/trigger` | 用户端 | query: params | 200 {processed:0}（手动触发评估，无待评估 trace 时 0） | `src/api/monitor.js` | 32 |
| 787 | [x] | GET | `/api/rag/recall/snapshot` | 用户端 | - | 200 data={available:false,captured:null,summary:null}（尚无采集数据）；触发采集后 available=true + summary（window/hit_rate/recall/precision） | `src/api/monitor.js` | 36 |
| 788 | [x] | GET | `/api/rag/recall/snapshots` | 用户端 | query: params | 200 {list:[{id:203,avg_latency_ms,avg_precision,avg_recall,avg_similarity,low_recall_count,p95_latency_ms,payload:"{window...}",created_at},...]}（历史快照倒序，limit 生效） | `src/api/monitor.js` | 39 |
| 789 | [x] | POST | `/api/rag/recall/collect` | 用户端 | body: params | 200 data={window_start,window_end,total_queries:0,top_k_hit_rate,top_1_hit_rate,avg_recall,avg_precision,avg_similarity,avg_latency_ms,p95_latency_ms,zero_hit_count,low_recall_count}（手动采集一个窗口快照） | `src/api/monitor.js` | 42 |
| 790 | [x] | POST | `/api/rag/recall/start` | 用户端 | - | 200「已启动」data={interval:"5m0s",started:true}（定时采集器启动） | `src/api/monitor.js` | 45 |
| 791 | [x] | POST | `/api/rag/recall/stop` | 用户端 | - | 200「已停止」data={started:false} | `src/api/monitor.js` | 48 |
| 792 | [x] | GET | `/api/rag/recall/metrics` | 用户端 | query: start, end | 缺参数 → 400 INVALID_PARAM_1001「start/end 参数必填 (RFC3339)」；带 start/end → 200 {window_start,window_end,total_queries,avg_recall,avg_precision,avg_latency_ms,p99_latency_ms,zero_hit_count,low_recall_count} | `src/api/monitor.js` | 51 |
| 793 | [x] | GET | `/api/rag/recall/low-recall` | 用户端 | query: params | 200 data=[]（低召回查询列表，当前窗口无低召回样本） | `src/api/monitor.js` | 54 |
| 794 | [x] | GET | `/api/auth/notifications` | 用户端 | query: params | 200 {list:[{tenant_id,id:1218,user_id,type:"bad_case_pending",title,content,link,is_read,metadata,created_at,read_at}],...}（真实待归因坏例通知） | `src/api/notification.js` | 9 |
| 795 | [x] | POST | `/api/auth/notifications/{id}/read` | 用户端 | path: id | 200 标记已读（read_at 落盘）；不存在 99999 → 404 NOT_FOUND_1002「通知不存在或无权限」 | `src/api/notification.js` | 11 |
| 796 | [x] | POST | `/api/auth/notifications/read-all` | 用户端 | - | 200「已全部标记为已读」data={marked:2}；复读后全部 is_read=true | `src/api/notification.js` | 13 |
| 797 | [x] | GET | `/api/auth/notifications/unread-count` | 用户端 | - | 200 {count:2}（未读数；read-all 后应归零） | `src/api/notification.js` | 15 |
| 798 | [x] | POST | `/api/objection/handle` | 用户端 | body: data | 200「处理成功」data={category,category_name,confidence:0.4,template:{id:1,title:"R39验证话术",content,usage_count++,success_rate},templates:[...]}（关键词匹配话术模板，空 body 也返回兜底模板；usage_count 每次调用自增） | `src/api/objection.js` | 4 |
| 799 | [x] | POST | `/api/objection/classify` | 用户端 | body: data | 200「分类成功」data={category,category_name}（关键词分类器） | `src/api/objection.js` | 8 |
| 800 | [x] | GET | `/api/objection/categories` | 用户端 | - | 200 {list:[{category:status_quo,name:"维持现状异议"},{category:price,name:"价格异议"},{category:need},{category:trust},{category:timing},{category:compare},...]}（异议分类字典） | `src/api/objection.js` | 12 |
| 801 | [x] | POST | `/api/objection/usage` | 用户端 | body: data | 200「记录成功」（记录话术使用；空 body 亦 200——template_id 可选，观察项：不校验模板存在性） | `src/api/objection.js` | 16 |
| 802 | [x] | GET | `/api/obs/config` | 用户端 | query: params | 200 {list:[{id,name,provider,provider_name,access_key,secret_key:"***"（掩码）,bucket,region,endpoint,domain,path_prefix,config,max_size:104857600,max_count,status:"active",last_error,last_test_at,total_size,file_count,is_default,created_at}],...} | `src/api/obs.js` | 4 |
| 803 | [x] | GET | `/api/obs/config/{id}` | 用户端 | path: id | 200 详情（结构同 #802）；不存在 → 404 NOT_FOUND_1002 "NOT_FOUND" data="record not found" | `src/api/obs.js` | 8 |
| 804 | [x] | POST | `/api/obs/config` | 用户端 | body: data | 200「创建成功」（name/provider/endpoint 必填语义；local 可空密钥；domain 默认 /files；max_size 默认 100MB） | `src/api/obs.js` | 12 |
| 805 | [x] | PUT | `/api/obs/config/{id}` | 用户端 | path: id / body: data | 200「更新成功」（name 改 v2 回读生效） | `src/api/obs.js` | 16 |
| 806 | [x] | DELETE | `/api/obs/config/{id}` | 用户端 | path: id | 200「删除成功」；重复删 → 404 NOT_FOUND_1002「记录不存在」 | `src/api/obs.js` | 20 |
| 807 | [x] | POST | `/api/obs/config/{id}/test` | 用户端 | path: id | 200「连接测试成功」（local 驱动写读校验；last_test_at 更新） | `src/api/obs.js` | 24 |
| 808 | [x] | POST | `/api/obs/config/{id}/default` | 用户端 | path: id | 200「设为默认配置成功」（唯一默认约束：切换后旧默认自动取消） | `src/api/obs.js` | 28 |
| 809 | [x] | GET | `/api/obs/config/default` | 用户端 | - | 200 默认配置详情（无默认 → data=null 或 404，视实现） | `src/api/obs.js` | 32 |
| 810 | [x] | GET | `/api/observability/dashboard` | 用户端 | body: { window } | 200 {generated_at,window:"7d",ai_cost:{calls,success_calls,prompt_tokens,completion_tokens,total_tokens:103503,cost,avg_latency_ms,by_provider:[{key:"deepseek",calls:30,...}],by_scenario},channel_health,delivery,funnel}；window=24h 生效；**window=bogus 静默回退 7d（观察项：不报错）** | `src/api/observabilityDashboard.js` | 9 |
| 811 | [x] | GET | `/api/customer/oneid/list` | 用户端 | query: params | 200 {list:[{id,unified_id:"visitor:...:v_71b70p1nmvv775ii",phone,email,wechat_open_id,douyin_open_id,xiaohongshu_id,created_at,updated_at}],...}（真实 OneID 身份记录 20 条） | `src/api/oneid.js` | 4 |
| 812 | [x] | GET | `/api/customer/oneid/conflicts` | 用户端 | query: params | 200 {list:[],page,page_size,total:0}（无身份冲突记录） | `src/api/oneid.js` | 8 |
| 813 | [x] | POST | `/api/customer/oneid/merge` | 用户端 | body: data | 字段是 primary_id+secondary_id（单数）→ 合法 → 200「合并成功」data={primary:完整实体,merged_id}；同 ID → 400「不能合并同一客户」；不存在 → 400（业务错误文案）；合并后主 ID 保留全部行为轨迹 | `src/api/oneid.js` | 12 |
| 814 | [x] | POST | `/api/customer/oneid/conflicts/{id}/resolve` | 用户端 | path: id / body: data | 200「解决成功」（无冲突数据：conflicts 空，仅验证参数校验路径 400「请求参数错误」；99999+合法 body → 400/404 业务文案） | `src/api/oneid.js` | 16 |
| 815 | [x] | GET | `/api/customer-oneid/{customerId}/identities` | 用户端 | path: customerId | 200 {customer:{id,unified_id,phone,email,wechat_open_id,douyin_open_id,xiaohongshu_id,created_at,updated_at},identities:[]}（按 customerId 查身份聚合） | `src/api/oneid.js` | 20 |
| 816 | [x] | POST | `/api/customer-oneid/{customerId}/identities` | 用户端 | path: customerId / body: data | 200「链接成功」data={customer_id,identifiers:{...}}（添加身份标识；id_type/id_value body） | `src/api/oneid.js` | 24 |
| 817 | [x] | POST | `/api/customer/oneid/resolve` | 用户端 | body: data | 200「未找到匹配客户」data={customers:[],identifiers:{...},matched:false,normalized_ids:{phone,email,wechat_open_id,douyin_open_id,xiaohongshu_id}}（按标识解析 OneID，未匹配也 200） | `src/api/oneid.js` | 28 |
| 818 | [x] | GET | `/api/customer/oneid/stats` | 用户端 | - | 200 {total:132,with_phone:8,with_email:7,with_wechat:2,with_douyin:71,multi_identity:6} | `src/api/oneid.js` | 32 |
| 819 | [x] | GET | `/api/oneid/merge-rules` | 用户端 | - | 200 {built_in:[{id:1,name:"同手机号合并",field:phone,op:eq,priority:100,enabled,built_in,description,example}...共 6 条],custom:[],strategy:{primary_rule:"latest_active",conflict_behavior:"queue_review",window_start:"02:00",window_end:"05:00",post_merge_actions:["unify_tag","write_audit"]}} | `src/api/oneid.js` | 36 |
| 820 | [x] | POST | `/api/oneid/merge-rules` | 用户端 | body: data | **全量替换语义（MergeRuleSet）**：body 需 {built_in:[...],custom:[...],strategy:{primary_rule,conflict_behavior,...}}；built_in+custom 全空 → 400「至少需要 1 条规则」；primary_rule 只允许 latest_active/most_orders/manual；conflict_behavior 只允许 auto_merge/queue_review/skip；custom 规则需 name/field；保存成功回显规则集（测后已按默认 6 条还原） | `src/api/oneid.js` | 40 |
| 821 | [x] | GET | `/api/operation-logs` | 用户端 | query: params | 200 {list:[{id,user_id,username,action,module,resource,resource_id,ip,created_at,rollbackable}],page,page_size,total}（真实操作日志 id=71433 起；page/page_size 生效） | `src/api/operationLog.js` | 4 |
| 822 | [x] | GET | `/api/operation-logs/{id}` | 用户端 | path: id | 200 详情（id=71433）；不存在 → 404 NOT_FOUND_1002「日志不存在」 | `src/api/operationLog.js` | 7 |
| 823 | [x] | GET | `/api/operation-logs/statistics` | 用户端 | query: params | 200 {action_stats:{create:89,delete:9,merge:1,update:1},module_stats:{classify:2,config:7,...}}（按动作/模块聚合） | `src/api/operationLog.js` | 10 |
| 824 | [x] | GET | `/api/operation-logs/export` | 用户端 | query: params | 200 text/csv attachment「ID,Module,Action,Username,IP,CreatedAt」表头 + 数据行 | `src/api/operationLog.js` | 13 |
| 825 | [x] | POST | `/api/operation-logs/clean` | 用户端 | body: { beforeDate } | 200「清理完成」data={before:"2026-08-09..."}（beforeDate 早于全部日志时删 0 条，安全）；body {beforeDate} 必填 | `src/api/operationLog.js` | 16 |
| 826 | [x] | GET | `/api/manage/order-drafts` | 管理端 | query: params | **503 SERVICE_UNAVAILABLE_6003「订单草稿底座不可用（本进程未装配草稿运行时，开关 FF_LTC_ORDER_DRAFT_DB 为 off 或值无法识别；档位只在装配期读一次，改完须重启）」**（默认档未装配，与 #417 followup 同口径，开关开启即可用） | `src/api/orderDraft.js` | 32 |
| 827 | [x] | GET | `/api/manage/order-drafts/{id}` | 管理端 | path: id | 同 #826：503 未装配（拒绝路径正确；真实数据需 FF_LTC_ORDER_DRAFT_DB 开启+重启） | `src/api/orderDraft.js` | 36 |
| 828 | [x] | PATCH | `/api/manage/order-drafts/{id}` | 管理端 | path: id / body: updates | 同 #826：503 未装配 | `src/api/orderDraft.js` | 43 |
| 829 | [x] | POST | `/api/manage/order-drafts/{id}/confirm` | 管理端 | path: id | 同 #826：503 未装配 | `src/api/orderDraft.js` | 50 |
| 830 | [x] | POST | `/api/manage/order-drafts/{id}/cancel` | 管理端 | path: id / body: { reason } | 同 #826：503 未装配 | `src/api/orderDraft.js` | 55 |
| 831 | [x] | GET | `/api/agent/order-drafts/stats` | 用户端 | - | 同 #826：503 未装配（agent 端统计） | `src/api/orderDraft.js` | 62 |
| 832 | [x] | PUT | `/api/system/permissions/{id}/enabled` | 用户端 | path: id / body: { enabled } | 200「账号启用成功」（:id 是 system_users 用户 ID 非 permission 表；非数字 → 400「无效的 ID」） | `src/api/permission.js` | 4 |
| 833 | [x] | PUT | `/api/system/permissions/{id}/password` | 用户端 | path: id / body: { password } | 200「密码重置成功」（同上 :id 为用户 ID；空 password → 400「新密码不能为空」） | `src/api/permission.js` | 8 |
| 834 | [x] | GET | `/api/system/permissions/audit-logs` | 用户端 | query: params | 200 {list:[{id,user_id,username,action:"user.reset_password",module:role,resource:system_user,resource_id,ip,created_at,rollbackable}],...}（历史审计日志；user_id/action/page/page_size 过滤） | `src/api/permission.js` | 12 |
| 835 | [x] | GET | `/api/analytics/persona/staffs` | 用户端 | - | 200 {list:[{session_count:4,staff_id:1},...],total:3}（有会话的客服画像列表） | `src/api/persona.js` | 4 |
| 836 | [x] | GET | `/api/analytics/persona/staffs/{staffId}` | 用户端 | path: staffId | 200「报告生成成功」data={staff_id,staff_name,overall_score:28.57,items:[{tag:response_speed,name:响应速度,score,sample,trend},...]}（多维画像打分） | `src/api/persona.js` | 8 |
| 837 | [x] | GET | `/api/platform/message/latest` | 管理端 | - | 200 data 为空对象/空列表（平台未初始化，无平台消息） | `src/api/platform.js` | 5 |
| 838 | [x] | POST | `/api/platform/message/{messageId}/read` | 管理端 | path: messageId | 200「平台未初始化，返回空数据」data=null（幂等容忍） | `src/api/platform.js` | 9 |
| 839 | [x] | GET | `/api/platform-accounts` | 用户端 | query: params | 200 data=[{tenant_id,id,platform,account_id,account_name,account_avatar,config,status,last_sync_at,expires_at,created_at,updated_at},...]（跨租户可见，off 模式管理视角） | `src/api/platformAccount.js` | 5 |
| 840 | [x] | GET | `/api/platform-accounts/platforms` | 用户端 | - | 200 data=[{code:douyin,name:抖音},{kuaishou},{xiaohongshu},{xianyu},{tiktok}]（静态清单，5 平台） | `src/api/platformAccount.js` | 9 |

> **缺陷 D-35（已修复）**：DELETE /api/platform-accounts/{id} 对不存在 id 返回 200「删除成功」假成功。根因：`internal/repository/unified_message.go` platformAccountRepo.Delete 不校验 RowsAffected。修复：RowsAffected==0 时返回 gorm.ErrRecordNotFound，controller HandleDBError 映射 404「记录不存在」。
| 841 | [x] | GET | `/api/platform-accounts/{id}` | 用户端 | path: id | 200 详情（id=45 wechat）；99999 → 404 NOT_FOUND_1002「记录不存在」 | `src/api/platformAccount.js` | 9 |
| 842 | [x] | POST | `/api/platform-accounts` | 用户端 | body: data | 空 body → 400「请求参数错误：请检查字段类型与必填项」 | `src/api/platformAccount.js` | 9 |
| 843 | [x] | PUT | `/api/platform-accounts/{id}` | 用户端 | path: id / body: data | 99999 → 404「记录不存在」；真实账号指针语义更新成功 | `src/api/platformAccount.js` | 9 |
| 844 | [x] | DELETE | `/api/platform-accounts/{id}` | 用户端 | path: id | 真实删除 200「删除成功」；**不存在 99999 修复前 200 假成功 → D-35 修复后 404「记录不存在」** | `src/api/platformAccount.js` | 9 |
| 845 | [x] | POST | `/api/platform-accounts/{id}/login` | 用户端 | path: id / body: data | 空 body/不存在 id → 404「记录不存在」；真实账号 → 400 INVALID_PARAM_1001「平台 wechat 不支持服务端无头登录（CDP 自动回复通道已移除，登录态由桥接模块维护）」（能力下线明确语义） | `src/api/platformAccount.js` | 9 |
| 846 | [x] | GET | `/api/platform-accounts/{id}/status` | 用户端 | path: id / body: undefined | 99999 → 404「记录不存在」；真实账号 → 400「平台 wechat 不支持服务端登录态检查…」（同 845 口径） | `src/api/platformAccount.js` | 9 |
| 847 | [x] | GET | `/api/qq/accounts` | 用户端 | query: params | 200 {list:[],total:0}（当前无 QQ 账号） | `src/api/qqBot.js` | 4 |
| 848 | [x] | GET | `/api/qq/accounts/{id}` | 用户端 | path: id | 99999 → 404 NOT_FOUND_1002「账号不存在」 | `src/api/qqBot.js` | 8 |
| 849 | [x] | POST | `/api/qq/accounts` | 用户端 | body: data | 空 body → 400 INVALID_PARAM_1001「参数错误」（binding 列 AccountName/AppID/AppSecret required，A20 刻意口径）；合法 → 200 创建 id=5，app_secret 掩码 test***cret | `src/api/qqBot.js` | 12 |
| 850 | [x] | PUT | `/api/qq/accounts/{id}` | 用户端 | path: id / body: data | 改名 200 更新成功（指针语义）；99999 → 404「账号不存在」 | `src/api/qqBot.js` | 16 |
| 851 | [x] | DELETE | `/api/qq/accounts/{id}` | 用户端 | path: id | 删 id=5 → 200「删除成功」；重复删 → 404「账号不存在」 | `src/api/qqBot.js` | 20 |
| 852 | [x] | POST | `/api/qq/accounts/{id}/test-send` | 用户端 | path: id / body: data | 99999 → 404「账号不存在」（真实账号需外网 QQ API，环境阻塞） | `src/api/qqBot.js` | 24 |
| 853 | [x] | POST | `/api/qq/accounts/{id}/verify-callback` | 用户端 | path: id / body: {} | 99999 → 404「账号不存在」 | `src/api/qqBot.js` | 28 |
| 854 | [x] | POST | `/api/rag-config/products` | 用户端 | body: data | 空 body → 400「product name is required」；合法 {name,description,category} → 200 创建 id=9c74f8ac（RAG 产品实体全字段） | `src/api/ragProductConfig.js` | 12 |
| 855 | [x] | GET | `/api/rag-config/products` | 用户端 | query: params | 200 {list,total:14,page,page_size}（getRagProducts 压平数组语义，产品含 llm/embedding/rerank provider config） | `src/api/ragProductConfig.js` | 21 |
| 856 | [x] | GET | `/api/rag-config/products` | 用户端 | query: params | 同 #855（listProducts 是同路径的 normalizeList 封装，读数一致） | `src/api/ragProductConfig.js` | 25 |
| 857 | [x] | PUT | `/api/rag-config/products/{id}` | 用户端 | path: id / body: data | 改名 200「RAG product updated successfully」；不存在 → 404 NOT_FOUND_1002「failed to load original product: rag product not found」 | `src/api/ragProductConfig.js` | 29 |
| 858 | [x] | DELETE | `/api/rag-config/products/{id}` | 用户端 | path: id | 删 9c74f8ac → 200「RAG product deleted successfully」；重复删 → 404「RAG 产品不存在」 | `src/api/ragProductConfig.js` | 33 |
| 859 | [x] | GET | `/api/rag-config/accounts/config` | 用户端 | query: params | 200 code=0 message="No account config found"（无账号配置时空语义，data 空） | `src/api/ragProductConfig.js` | 37 |
| 860 | [x] | PUT | `/api/rag-config/accounts/config` | 用户端 | body: PlatformAccountConfig | platform 白名单枚举（douyin/kuaishou/xiaohongshu/xianyu），bogus/空 body → 400 INVALID_PARAM_1001「invalid platform, must be one of...」；合法 {platform,account_id,is_auto_reply_enabled,is_rag_enabled,rag_product_id,reply_rules,max_daily_queries} → 200「Configuration updated successfully」；is_rag_enabled+不存在产品 → 404「rag product not found」 | `src/api/ragProductConfig.js` | 41 |
| 861 | [x] | POST | `/api/rag-config/process-message` | 用户端 | body: {platform,account_id,message} | 空 body → 400（binding 列 required，A20 口径）；未配置账号 → 400「auto reply is not enabled for this account」（GET 未配置账号返回零值结构非 404，观察项）；规则回复路径：关键词命中 → reply=模板文案，未命中 → 兜底「感谢您的消息...」；RAG 路径（is_rag_enabled）→ 400「failed to process with rag: invalid LLM config: model is required」（产品未配 LLM，环境阻塞）；on 模式同语义 | `src/api/ragProductConfig.js` | 45 |
| 862 | [x] | GET | `/api/reach/pipelines` | 用户端 | query: params | 200 {list:[{tenant_id,id,name,description,channel,steps,retry_policy{backoff,interval_ms,max_interval_ms,max_retries},rate_limit{burst,cooldown_secs,daily_quota,per_user_limit,qps},status,version,total_runs,total_success,total_failure,created_at,updated_at}],...}（历史 id=20/15 等） | `src/api/reachPipeline.js` | 5 |
| 863 | [x] | POST | `/api/reach/pipelines` | 用户端 | body: data | 空 body → 400「请求参数错误：请检查字段类型与必填项」；最小 body {name,channel,steps:[audience,send]} → 200「创建成功」id=22 status=active version=1（retry_policy/rate_limit 填默认值） | `src/api/reachPipeline.js` | 8 |
| 864 | [x] | GET | `/api/reach/pipelines/{id}` | 用户端 | path: id | 200「查询成功」详情；不存在 99999 → 404 NOT_FOUND_1002「Pipeline 不存在」；abc → 400「无效的 ID」 | `src/api/reachPipeline.js` | 11 |
| 865 | [x] | PUT | `/api/reach/pipelines/{id}` | 用户端 | path: id / body: data | 全量更新语义（复用 CreatePipelineRequest）：部分 body {name} → 400；完整 body → 200「更新成功」；不存在 → 400「请求参数错误」（binding 先于存在性校验，观察项） | `src/api/reachPipeline.js` | 14 |
| 866 | [x] | DELETE | `/api/reach/pipelines/{id}` | 用户端 | path: id | 200「删除成功」data={id}；不存在 99999 → 404「pipeline not found」 | `src/api/reachPipeline.js` | 17 |
| 867 | [x] | POST | `/api/reach/pipelines/{id}/pause` | 用户端 | path: id / body: {} | 200「暂停成功」data={id}；不存在 → 404（文案掩码「操作失败，请稍后重试」，观察项） | `src/api/reachPipeline.js` | 20 |
| 868 | [x] | POST | `/api/reach/pipelines/{id}/resume` | 用户端 | path: id / body: {} | 200「恢复成功」data={id}；不存在 → 404（掩码文案，观察项） | `src/api/reachPipeline.js` | 23 |
| 869 | [x] | POST | `/api/reach/pipelines/{id}/archive` | 用户端 | path: id / body: {} | 200「归档成功」data={id} | `src/api/reachPipeline.js` | 26 |
| 870 | [x] | GET | `/api/reach/jobs` | 用户端 | query: params | 200 {list:[{tenant_id,id,pipeline_id,channel,customer_id,account_id,payload(含 _last_send),state,current_step,step_results,retry_count,max_retry,...}],...}（历史 job id=4 success） | `src/api/reachPipeline.js` | 30 |
| 871 | [x] | POST | `/api/reach/jobs` | 用户端 | body: data | 必填 pipeline_id/customer_id/payload（缺 payload 也 400）；run_at 可选（缺省立即）；合法 → 200「入队成功」state=pending；pipeline 不存在 → 400「请求参数错误」（binding 后置校验） | `src/api/reachPipeline.js` | 33 |
| 872 | [x] | GET | `/api/reach/jobs/{id}` | 用户端 | path: id | 200「查询成功」详情（dispatcher 已执行可见 step_results/output）；不存在 99999 → 404「任务不存在」；abc → 400「无效的 ID」 | `src/api/reachPipeline.js` | 36 |
| 873 | [x] | POST | `/api/reach/jobs/{id}/cancel` | 用户端 | path: id / body: {} | 仅 pending/running/retrying/rate_limited 可取消：pending → 200「取消成功」state=canceled；success/canceled → 400「job is not pending/running」；99999 → 400（同文案，不区分不存在，观察项） | `src/api/reachPipeline.js` | 39 |
| 874 | [x] | POST | `/api/reach/jobs/{id}/retry` | 用户端 | path: id / body: {} | 仅 failed 可重试：failed → 200「重试成功」state→pending error_message 清空；success/canceled → 400「job is not pending/running」 | `src/api/reachPipeline.js` | 42 |
| 875 | [x] | POST | `/api/reach/jobs/{id}/execute` | 用户端 | path: id / body: {} | pending → 200「执行成功」同步执行全部 steps（audience+send，email 发送成功 message_id）；99999 → 404「job not found」；非 pending → 400 | `src/api/reachPipeline.js` | 45 |
| 876 | [x] | GET | `/api/reach/stats` | 用户端 | query: params | 200 {active:3,canceled:1,failed:0,jobs:4,paused:0,pending:0,rate_limited:0,running:0,success:3,total:4} | `src/api/reachPipeline.js` | 49 |
| 877 | [x] | POST | `/api/reach/rate-limit/reset` | 用户端 | query: channel / body: {} | 缺 channel → 400「channel 必填」；channel=email → 200「重置成功」data={channel} | `src/api/reachPipeline.js` | 52 |
| 878 | [x] | GET | `/api/reach/deferred-outbound` | 用户端 | query: params | 200 {list:[],total:0}（当前无延迟出站记录） | `src/api/reachPipeline.js` | 58 |
| 879 | [x] | POST | `/api/reach/deferred-outbound/{id}/cancel` | 用户端 | path: id / body: {} | 不存在 99999 → 404 NOT_FOUND_1002「记录不存在或不可取消（仅 pending 可取消）」 | `src/api/reachPipeline.js` | 63 |
| 880 | [x] | GET | `/api/system/roles` | 用户端 | - | 200「获取角色列表成功」data=[{code,name,tag_type,description,is_system,member_count}]（admin 超管/member_count=35、customer_service、staff 等） | `src/api/role.js` | 10 |
| 881 | [x] | GET | `/api/system/roles/{code}` | 用户端 | path: code | 200 角色详情；不存在 nope → 400 INVALID_PARAM_1001「角色不存在」 | `src/api/role.js` | 15 |
| 882 | [x] | GET | `/api/system/roles/{code}/members` | 用户端 | path: code / query: params | 200 {list:[{tenant_id,id,username,email,phone,real_name,role,status,enabled,last_login,data_scope,department_id,team_id,...}],...}（staff 成员分页）；不存在角色 → 400「角色不存在」 | `src/api/role.js` | 22 |
| 883 | [x] | GET | `/api/config/runtime` | 用户端 | query: groups | 200 data=扁平 KV（frontend_ui.*、frontend_ws.*、knowledge.max_upload_file_size 等白名单组）；groups=bogus → 200 返回 knowledge 组默认（未知组静默忽略，白名单机制见 config_param_runtime.go） | `src/api/runtimeConfig.js` | 13 |
| 884 | [x] | GET | `/api/config/dictionaries/{parent}` | 用户端 | path: parent | 200 {parent,items:[{id,parent_code,code,name,description,sort_order,enabled,system_locked,...}],transitions}；不存在 parent → HTTP 200 + code=404「字典域不存在: xxx」（Envelope 风格，非 HTTP 404） | `src/api/runtimeConfig.js` | 23 |
| 885 | [x] | GET | `/api/manage/dictionaries` | 管理端 | - | 200 data=[{code,label,description,item_count,seeded}]（全部字典域目录：approval_status/bad_case_status/human_task_status 等） | `src/api/runtimeConfig.js` | 28 |
| 886 | [x] | GET | `/api/manage/dictionaries/{parent}` | 管理端 | path: parent | 200 {items,transitions}；不存在 → code=404 Envelope | `src/api/runtimeConfig.js` | 29 |
| 887 | [x] | PUT | `/api/manage/dictionaries/{parent}/items` | 管理端 | path: parent / body: item | 缺 code/name → code=400「参数错误: Key: 'Code'...」（Envelope，binding 带 Go 字段名 A20 口径）；upsert 到不存在域 nope → code=0（隐式创建域+项，观察项：不校验 parent 存在）；system_locked 项依赖 DB 值（approval_status.pending 实际 f，改名成功合理，测后还原）；enabled 指针语义 | `src/api/runtimeConfig.js` | 30 |
| 888 | [x] | PUT | `/api/manage/dictionaries/{parent}/transitions` | 管理端 | path: parent / body: { transitions } | 整体替换跃迁：{transitions:[{from,to,description}]} → code=0「ok」；再传 [] 清空恢复；测试域 nope 数据已清理 | `src/api/runtimeConfig.js` | 32 |
| 889 | [x] | GET | `/api/sales-workbench/overview` | 用户端 | body: { sales_id: salesId } | query 必给 sales_id（缺失 → 400「参数错误」）；sales_id=54 → 200 {sales_id,name,team,todos[],today{new_orders,new_revenue,follow_ups,conversions,conversion_rate,ai_deals},month{...},ai_product{...}} | `src/api/salesWorkbench.js` | 24 |
| 890 | [x] | GET | `/api/sales-workbench/team-dashboard` | 用户端 | body: { days } | days=7 → 200 {funnel:null(journey 未装配,null 分支口径),top_sales[],ai_productivity{...}}；days=0/非数 → 400「参数错误」 | `src/api/salesWorkbench.js` | 30 |
| 891 | [x] | GET | `/api/sales-workbench/champion` | 用户端 | body: { days } | days=30 → 200 {top_performers:null,common_tags:null,avg_conversion_rate:0,avg_deal_amount:0,recommended_sops:null,insights:[「暂无销售数据」],generated_at}（无销售数据空语义） | `src/api/salesWorkbench.js` | 37 |
| 892 | [x] | GET | `/api/sales-workbench/quick-actions` | 用户端 | - | 200 {list:[{id:new_draft,title:新建订单,icon,url:/dashboard/drafts,badge:0},{ai_assist,followup_today,...}]}（A10 静态 5 条快链） | `src/api/salesWorkbench.js` | 43 |
| 893 | [x] | GET | `/api/script-library/{scriptId}/versions` | 用户端 | path: scriptId | 200 {list,total}（真实 script 18 初始 0 版本；不存在 id 也 200 空列表——ListVersions 不校验脚本存在，观察项） | `src/api/scriptAb.js` | 4 |
| 894 | [x] | POST | `/api/script-library/{scriptId}/versions` | 用户端 | path: scriptId / body: data | body {note} 可选；创建快照并激活 → 200「新版本已发布并激活」{script_id:18,version:1→2,content 快照,status:active,created_by:54}（创建时以脚本当前内容为快照，body 的 content 不参与） | `src/api/scriptAb.js` | 5 |
| 895 | [x] | PUT | `/api/script-library/{scriptId}/versions/{versionId}/activate` | 用户端 | path: scriptId, versionId | version=1 → 200「版本已激活」{script_id,version}（回滚语义）；不存在 version=99 → 404「记录不存在」 | `src/api/scriptAb.js` | 7 |
| 896 | [x] | POST | `/api/script-library/{scriptId}/expire` | 用户端 | path: scriptId / body: data | 200「话术已过期下线」{script_id,status:expired}；测后 DB 还原 status=active/expires_at=NULL | `src/api/scriptAb.js` | 8 |
| 897 | [x] | GET | `/api/script-library/{scriptId}/ab-stats` | 用户端 | path: scriptId / query: params | 200 {attribution_hrs:48,buckets:null,config:{enabled,split_a:50,attribution_h:48},versions[]}（无曝光数据空语义；不存在脚本同样 200 空统计，观察项） | `src/api/scriptAb.js` | 9 |
| 898 | [x] | PUT | `/api/script-library/{scriptId}/ab-config` | 用户端 | path: scriptId / body: data | {} → 400「split_a 必须在 (0,100) 开区间内」；{enabled,split_a:60,attribution_h:24} → 200「AB 配置已保存」回显；测后还原 {enabled:true,split_a:50,attribution_h:48} | `src/api/scriptAb.js` | 10 |
| 899 | [x] | POST | `/api/script-ab/conversion` | 用户端 | body: data | 缺 one_id → 400（binding 带 Go 字段名 A20 口径）；{one_id,conversation_id,outcome:won} → 200 {recorded:true}（无曝光锚点时 MarkScriptExposuresConverted 0 行，幂等安全；归因写 script_exposure_logs 非独立 conversion 表） | `src/api/scriptAb.js` | 11 |
| 900 | [x] | GET | `/api/scripts` | 用户端 | query: params | 200「获取成功」{list:[{tenant_id,id,category,category_id,title,content,variables,tags,usage_count,rating,is_public,is_system,source,effectiveness_score,...}],...}（历史脚本 id=5/18 等） | `src/api/scriptTemplate.js` | 4 |
| 901 | [x] | GET | `/api/scripts/{id}` | 用户端 | path: id | 200 详情（id=18）；不存在 99999 → 404「记录不存在」 | `src/api/scriptTemplate.js` | 7 |
| 902 | [x] | POST | `/api/scripts` | 用户端 | body: data | 空 body → 400（binding 列 CreateScriptTemplateRequest.Category 等 required，A20 口径）；最小 {category,title,content} → 200 创建 id=42 | `src/api/scriptTemplate.js` | 10 |
| 903 | [x] | PUT | `/api/scripts/{id}` | 用户端 | path: id / body: data | 200「更新成功」回读一致 | `src/api/scriptTemplate.js` | 13 |
| 904 | [x] | DELETE | `/api/scripts/{id}` | 用户端 | path: id | 200「删除成功」；重复删 → 404「记录不存在」 | `src/api/scriptTemplate.js` | 16 |
| 905 | [x] | GET | `/api/scripts/categories` | 用户端 | - | 200「获取成功」data=[]（脚本分类表当前空） | `src/api/scriptTemplate.js` | 19 |
| 906 | [x] | GET | `/api/scripts/search` | 用户端 | query: params | keyword=部署 → 200 {list:[{id:32,社群问候,...}]}（keyword 命中 title/content） | `src/api/scriptTemplate.js` | 22 |
| 907 | [x] | GET | `/api/scripts/public` | 用户端 | - | 200「获取成功」{list:[],page,page_size,total:0}（无公开脚本） | `src/api/scriptTemplate.js` | 25 |
| 908 | [x] | POST | `/api/scripts/recommend` | 用户端 | body: data | 缺 message → 400（binding Message required）；{message} → 200「获取推荐成功」data=null（LLM/匹配无候选时 null，观察项） | `src/api/scriptTemplate.js` | 28 |
| 909 | [x] | POST | `/api/security/audit` | 用户端 | body: data | 空 body → 200 同步执行 8 项检查 → {id:71,audit_name:系统安全审计,risk_level:low,score:93,total_checks:8,passed:7,failed:0,warnings:1,status:done,items[...]} | `src/api/securityAudit.js` | 4 |
| 910 | [x] | GET | `/api/security/audit/list` | 用户端 | query: params | 200 {list:[...],...}（倒序，含刚创建 id=71） | `src/api/securityAudit.js` | 8 |
| 911 | [x] | GET | `/api/security/audit/{id}` | 用户端 | path: id | 200 详情；不存在 99999 → code=4004「审计记录不存在」（业务码风格，非 HTTP 404，观察项） | `src/api/securityAudit.js` | 12 |
| 912 | [x] | GET | `/api/shortlink/list` | 用户端 | query: params | 200 {list:[{id,short_code,original_url,title,description,domain_id,password,expire_time,click_count,status,status_str,...}],...}（历史 293/291 等） | `src/api/shortLink.js` | 5 |
| 913 | [x] | GET | `/api/shortlink/{id}` | 用户端 | path: id | 99999 → 404「记录不存在」 | `src/api/shortLink.js` | 9 |
| 914 | [x] | POST | `/api/shortlink/create` | 用户端 | body: data | 空 body → 400「请求参数错误」；**必填 short_code+original_url（url 格式校验，CreateShortLinkRequest）**→ 200「创建成功」id=300 | `src/api/shortLink.js` | 13 |
| 915 | [x] | PUT | `/api/shortlink/update` | 用户端 | body: data | {id,original_url,title} → 200「更新成功」；注意 body 不带 status 时状态被置 0（全量更新语义，观察项）需显式回传 status=1 | `src/api/shortLink.js` | 17 |
| 916 | [x] | DELETE | `/api/shortlink/delete/{id}` | 用户端 | path: id | 200「删除成功」；重复删 → 404「记录不存在」 | `src/api/shortLink.js` | 21 |
| 917 | [x] | POST | `/api/shortlink/access` | 用户端 | body: data | {short_code} → 200「访问成功」{original_url,title}（公开路由；真实跳转在 /s/{code} 链路）；不存在 → 404「短链不存在」 | `src/api/shortLink.js` | 25 |
| 918 | [x] | POST | `/api/shortlink/generate` | 用户端 | body: data | 空 body → 400（length 绑定 min=4,max=10）；{length:6} → 200「生成成功」{short_code:eR35Vt}（随机唯一短码） | `src/api/shortLink.js` | 29 |
| 919 | [x] | GET | `/api/shortlink/{id}/stats` | 用户端 | path: id / query: params | 200 {short_link_id,short_code,total_access,today_access,device_type_stats,daily_stats}（access 后 total_access=1）；不存在 → 404「记录不存在」 | `src/api/shortLink.js` | 33 |
| 920 | [x] | GET | `/api/shortlink/stats/all` | 用户端 | query: params | 200 {total_access:28,today_access:1,device_type_stats,daily_stats:[{date,count}...]（date 键为空串观察项）,short_link_stats[...]} | `src/api/shortLink.js` | 37 |
| 921 | [x] | POST | `/api/shortlink/{id}/share` | 用户端 | path: id / body: data | {platform:wechat} → 200「获取短链分享信息成功」{short_url:http://localhost:8204/s/xxx,qr_code:data:image/png;base64,...}；不存在 → 404「记录不存在」 | `src/api/shortLink.js` | 41 |
| 922 | [x] | GET | `/api/sms/config` | 用户端 | - | 200 {defaultProvider:aliyun,rateLimit:12,dailyLimit:1000,retryTimes:2,aliyun{accessKeyId:****掩码,signName},tencent{...},huawei{...}}（密钥掩码） | `src/api/sms.js` | 5 |
| 923 | [x] | POST | `/api/sms/config` | 用户端 | body: data | 保存成功 200；GET 回读掩码 | `src/api/sms.js` | 8 |
| 924 | [x] | GET | `/api/sms/list` | 用户端 | query: params | 200 {list:[{tenant_id,id:43,phone,content,provider,status:failed,errorCode:InvalidAccessKeyId.NotFound,...}],...}（历史发送记录 43 条） | `src/api/sms.js` | 12 |
| 925 | [x] | GET | `/api/sms/detail/{id}` | 用户端 | path: id | 99999 → 404「记录不存在」 | `src/api/sms.js` | 15 |
| 926 | [x] | POST | `/api/sms/resend/{id}` | 用户端 | path: id | 当前 22:00-8:00 夜间禁发 → 400「当前处于夜间禁发时段(22:00-8:00)，短信已拦截」（业务规则拦截，合规口径；不存在 id 同样先拦时段） | `src/api/sms.js` | 18 |
| 927 | [x] | POST | `/api/sms/send` | 用户端 | body: data | 空 body → 400；合法 body → 400「夜间禁发时段…已拦截」（同 #926 时段拦截） | `src/api/sms.js` | 21 |
| 928 | [x] | GET | `/api/sms/draft/list` | 用户端 | query: params | 200 {list:[{tenant_id,id,title,content,createdAt,updatedAt}],...}（历史 id=20 等） | `src/api/sms.js` | 25 |
| 929 | [x] | GET | `/api/sms/draft/{id}` | 用户端 | path: id | 200 详情；不存在 → 404「记录不存在」 | `src/api/sms.js` | 28 |
| 930 | [x] | POST | `/api/sms/draft` | 用户端 | body: data | {title,content} → 200 创建 id=23 | `src/api/sms.js` | 31 |
| 931 | [x] | PUT | `/api/sms/draft/{id}` | 用户端 | path: id / body: data | 200「success」 | `src/api/sms.js` | 34 |
| 932 | [x] | DELETE | `/api/sms/draft/{id}` | 用户端 | path: id | 200「success」；**重复删幂等 200**（非 404，观察项）；删后再 GET → 404 | `src/api/sms.js` | 37 |
| 933 | [x] | POST | `/api/sms/draft/{id}/send` | 用户端 | path: id / body: data | 缺 phone（JSON+form 双读）→ 400「手机号不能为空」；带 phone → 400「夜间禁发时段拦截」；不存在 99999+phone → 404「记录不存在」 | `src/api/sms.js` | 40 |
| 934 | [x] | GET | `/api/sms/job/list` | 用户端 | query: params | 200 {list:[{tenant_id,id:1,name,total:2,sent,failed,status:completed,scheduleTime,...}],...}；status/name 过滤参数在 dto 白名单（pending/running/paused/completed/failed） | `src/api/sms.js` | 44 |
| 935 | [x] | GET | `/api/sms/job/{id}` | 用户端 | path: id | 200 详情（job id=1）；99999 → 404「记录不存在」 | `src/api/sms.js` | 47 |
| 936 | [x] | POST | `/api/sms/job/{id}/pause` | 用户端 | path: id | completed 任务 → 400「只能暂停运行中的任务」（状态机校验，HandleServiceError 口径非 500）；running 任务 → 200；99999 → 404「记录不存在」 | `src/api/sms.js` | 50 |
| 937 | [x] | POST | `/api/sms/job/{id}/resume` | 用户端 | path: id | 非 paused → 400「只能继续已暂停的任务」；paused → 200 | `src/api/sms.js` | 53 |
| 938 | [x] | POST | `/api/sms/job/{id}/stop` | 用户端 | path: id | completed → 400「只能停止运行中、暂停或待执行的任务」；running → 200（停止后 status=failed）；测后删除测试 job | `src/api/sms.js` | 56 |
| 939 | [x] | POST | `/api/sms/job` | 用户端 | body: data | 必填 name/phoneList(min=1)/content(min=1,max=500)，scheduleTime 可选；空 body → 400；合法 → 200「success」（创建即 running，夜间时段不拦截 job 创建） | `src/api/sms.js` | 59 |
| 940 | [x] | DELETE | `/api/sms/job/{id}` | 用户端 | path: id | 99999 → 404「记录不存在」 | `src/api/sms.js` | 62 |
| 941 | [x] | GET | `/api/sms/job/{id}/records` | 用户端 | path: id / query: params | 200 {list:[{tenant_id,id,jobId,phone,content,status,errorCode,errorMsg,sendTime,...}],...}（job 1 明细 pending） | `src/api/sms.js` | 65 |
| 942 | [x] | GET | `/api/sop` | 用户端 | query: params | 200 {list:[{tenant_id,id:33,name:价格异议 SOP,scenario:objection_handling,trigger_type:intent,sop_graph{nodes,edges},version,is_active,...}],...}（真实 SOP 5 条） | `src/api/sopAgent.js` | 5 |
| 943 | [x] | POST | `/api/sop` | 用户端 | body: data | 空 body → 400；必填 name/scenario/sop_graph（SOPGraph binding）；合法 → 200「创建成功」id=40 version=1 is_active=true trigger_type 默认 auto；测后已删 | `src/api/sopAgent.js` | 9 |
| 944 | [x] | GET | `/api/sop/stats` | 用户端 | - | 200 {active:5,failed:839,inactive:0,running:0,success:1187,total:5}（执行统计） | `src/api/sopAgent.js` | 13 |
| 945 | [x] | GET | `/api/sop/match` | 用户端 | query: params | 200 data=[]（message/intent 均无命中时空数组，不报错） | `src/api/sopAgent.js` | 17 |
| 946 | [x] | GET | `/api/sop/executions` | 用户端 | query: params | 200 {list:[{tenant_id,id:2028,sop_id:39,customer_id,session_id,current_node,status:failed,execution_data{_message_content,_side_effects,...},started_at,...}],...} | `src/api/sopAgent.js` | 21 |
| 947 | [x] | GET | `/api/sop/executions/{id}` | 用户端 | path: id | 200 详情（redactExecution 脱敏）；99999 → 404「执行不存在」（ErrSOPExecNotFound 专门分支） | `src/api/sopAgent.js` | 25 |
| 948 | [x] | POST | `/api/sop/executions/{id}/pause` | 用户端 | path: id | 不存在 → 404（文案掩码「操作失败，请稍后重试」观察项；service 有 ErrSOPNotFound 前置） | `src/api/sopAgent.js` | 29 |
| 949 | [x] | POST | `/api/sop/executions/{id}/resume` | 用户端 | path: id | 不存在 → 404（同上掩码文案） | `src/api/sopAgent.js` | 33 |
> **缺陷 D-36（已修复）**：`POST /api/sop/executions/{id}/cancel` 对不存在 id 返回 200「取消成功」假成功（pause/resume 均有存在性前置，仅 Cancel 直接 UpdateFields 不查 RowsAffected）。修复：`internal/service/sop/sop.go` SOPService.Cancel 增加 GetByID 前置（gorm.ErrRecordNotFound → ErrSOPExecNotFound），`internal/controller/sop.go` Cancel 错误分支加 `errors.Is(err, service.ErrSOPExecNotFound)` → 404「执行不存在」（与 GetExecution 同文案）。重建+重启后复测：99999 → 404，真实执行 2028 取消正常。
| 950 | [x] | POST | `/api/sop/executions/{id}/cancel` | 用户端 | path: id | **D-36 修复后**：不存在 99999 → 404 NOT_FOUND_1002「执行不存在」（修复前 200「取消成功」假成功）；真实执行 2028 cancel → 200「取消成功」status=canceled（测后还原 failed） | `src/api/sopAgent.js` | 37 |
| 951 | [x] | GET | `/api/sop/{id}` | 用户端 | path: id | 200 详情；不存在 → 404「SOP 不存在」（掩码文案） | `src/api/sopAgent.js` | 41 |
| 952 | [x] | PUT | `/api/sop/{id}` | 用户端 | path: id / body: data | 200 更新（admin 路由） | `src/api/sopAgent.js` | 45 |
| 953 | [x] | DELETE | `/api/sop/{id}` | 用户端 | path: id | 200「删除成功」data={id}（测试 SOP 40 已删） | `src/api/sopAgent.js` | 49 |
| 954 | [x] | POST | `/api/sop/{id}/activate` | 用户端 | path: id | 200 激活 | `src/api/sopAgent.js` | 53 |
| 955 | [x] | POST | `/api/sop/{id}/deactivate` | 用户端 | path: id | 200 停用 | `src/api/sopAgent.js` | 57 |
| 956 | [x] | POST | `/api/sop/execute` | 用户端 | body: data | admin 路由；空 body → 400「请求参数错误」（执行入口参数校验） | `src/api/sopAgent.js` | 61 |
| 957 | [x] | POST | `/api/sop/step` | 用户端 | body: data | admin 路由；空 body → 400「请求参数错误」（步进入口参数校验） | `src/api/sopAgent.js` | 65 |
| 958 | [x] | GET | `/api/sop-templates` | 用户端 | query: params | 200 {list:[{id:4340,name:开场问候,intent:greeting,stage,default,template,vars,priority:99,confidence:0.9,hit_count,...}],...}（历史模板 4000+ 条） | `src/api/sopTemplate.js` | 5 |
| 959 | [x] | GET | `/api/sop-templates/{id}` | 用户端 | path: id | 200 详情；99999 → 404「SOP 模板不存在」 | `src/api/sopTemplate.js` | 9 |
| 960 | [x] | POST | `/api/sop-templates` | 用户端 | body: data | 空 body → 400；**必填 name/intent/stage/template/agent_id（sopTemplateCreateReq binding）**→ 200 创建 id=4369 | `src/api/sopTemplate.js` | 13 |
| 961 | [x] | PUT | `/api/sop-templates/{id}` | 用户端 | path: id / body: data | 200「更新成功」data={id} | `src/api/sopTemplate.js` | 17 |
| 962 | [x] | PATCH | `/api/sop-templates/{id}/toggle` | 用户端 | path: id / body: { enabled } | {enabled:false} → 200「状态已更新」data={enabled:false,id}；非法 id → 400「无效的 SOP 模板 ID」 | `src/api/sopTemplate.js` | 21 |
| 963 | [x] | DELETE | `/api/sop-templates/{id}` | 用户端 | path: id | 200「删除成功」data={id}；重复删 → 404「操作失败，请稍后重试」（掩码文案，观察项） | `src/api/sopTemplate.js` | 25 |
| 964 | [x] | POST | `/api/sop-templates/match` | 用户端 | body: data | 缺 agent_id → 400（binding）→「agent_id 必填且 > 0 (Task 16 强 1对1)」；{agent_id:1,intent:greeting,top_k:3} → 200「匹配成功」data=[]（按 agent 强 1对1 匹配） | `src/api/sopTemplate.js` | 29 |
| 965 | [x] | GET | `/api/system/info` | 用户端 | - | 200 {cpu_usage,memory_usage:82.7,disk_usage,uptime,server_time,hostname,go_version,num_cpu,num_goroutine,alloc_memory,sys_memory,platform_enabled,...} | `src/api/stats.js` | 7 |
| 966 | [x] | GET | `/api/system/info` | 用户端 | body: config | 与 #965 同路径同实现（前端两处封装，读数一致） | `src/api/system.js` | 6 |
| 967 | [x] | GET | `/api/system/config` | 用户端 | - | 200 {id:1,tenant_id:1,site_name,website_url,logo_url,theme_color:#2f5985,seo_*,service_*,icp_record,police_record,enable_register:true,enable_email_marketing:false,...} | `src/api/system.js` | 9 |
| 968 | [x] | POST | `/api/system/config` | 用户端 | body: data | 保存站点配置 200（测后未改动持久字段） | `src/api/system.js` | 12 |
| 969 | [x] | GET | `/api/system/users` | 用户端 | query: params | 200 {list:[{tenant_id,id,username,email,phone,real_name,role,status,enabled,last_login,data_scope,department_id,team_id,...}],...}分页 | `src/api/systemUser.js` | 4 |
| 970 | [x] | GET | `/api/system/users/{id}` | 用户端 | path: id | 200「获取账号详情成功」（id=54）；99999 → 404「用户不存在」 | `src/api/systemUser.js` | 8 |
| 971 | [x] | POST | `/api/system/users` | 用户端 | body: data | 空 body → 400「请求参数错误」（binding）；重复用户名 → 400「用户名已存在」；合法 body {username,password,name,role} → 200 创建成功（测试用户 apitest_user_971 id=99） | `systemUser.js L12` | 12 |
| 972 | [x] | PUT | `/api/system/users/{id}` | 用户端 | path: id / body: data | {id:99,name:v2,role} → 200「更新成功」；不存在 99999 → 404 NOT_FOUND_1002「用户不存在」 | `systemUser.js L16` | 16 |
| 973 | [x] | DELETE | `/api/system/users/{id}` | 用户端 | path: id | 删 99 → 200「删除成功」；重复删/99999 → 404「用户不存在」 | `systemUser.js L20` | 20 |
| 974 | [x] | GET | `/api/session-tags` | 用户端 | query: params | GET /api/session-tags → 200「获取成功」data=[{tenant_id,id,name,code,group,color,description,sort_order,rule_condition,created_at}]（租户 1 真实 auto_* 标签多条）；tagSegmentation.js L5 | `src/api/tagSegmentation.js` | 5 |
| 975 | [x] | PUT | `/api/session-tags/{id}` | 用户端 | path: id / body: body | PUT /api/session-tags/{id} → name+code 均必填（缺任一 400「请求参数错误：name 与 code 均为必填…」）；合法 → 200「更新成功」返回完整实体；不存在 99999 → 404「会话标签不存在: 99999」；tagSegmentation.js L12 | `src/api/tagSegmentation.js` | 12 |
| 976 | [x] | POST | `/api/session-tags` | 用户端 | body: data | POST /api/session-tags → 空 body 400「name 与 code 均为必填」；合法 {name,code,color} → 200「创建成功」id=77；tagSegmentation.js L16 | `src/api/tagSegmentation.js` | 16 |
| 977 | [x] | DELETE | `/api/session-tags/{id}` | 用户端 | path: id | DELETE /api/session-tags/{id} → 删 77 → 200「删除成功」；重复删 → 404「会话标签不存在: 77」；tagSegmentation.js L20 | `src/api/tagSegmentation.js` | 20 |
| 978 | [x] | GET | `/api/customer-360/tag-rules` | 用户端 | query: params | GET /api/customer-360/tag-rules → 200「获取成功」{items:[],total:0}（无规则）；tagSegmentation.js L24 | `src/api/tagSegmentation.js` | 24 |
| 979 | [x] | POST | `/api/customer-360/tag-rules` | 用户端 | body: data | POST /api/customer-360/tag-rules → 空 body 400「请求参数错误」；合法 {name,category,rule:{field,op,value},active} → 200「保存成功」返回 {id:uuid,name,category,source:auto,rule,active}；tagSegmentation.js L28 | `src/api/tagSegmentation.js` | 28 |
| 980 | [x] | PUT | `/api/customer-360/tag-rules/{id}` | 用户端 | path: id / body: data | PUT /api/customer-360/tag-rules/{id} → 改名/rule/active → 200「更新成功」；不存在 → 404「规则不存在」；tagSegmentation.js L32 | `src/api/tagSegmentation.js` | 32 |
| 981 | [x] | DELETE | `/api/customer-360/tag-rules/{id}` | 用户端 | path: id | DELETE /api/customer-360/tag-rules/{id} → 200「删除成功」{id}；重复删幂等 200（Delete 服务不校验存在性，观察项）；tagSegmentation.js L36 | `src/api/tagSegmentation.js` | 36 |
| 982 | [x] | GET | `/api/user-segment/layers` | 用户端 | - | GET /api/user-segment/layers → 200 5 层静态字典 [{layer:champion,name:冠军客户,desc},loyal/at_risk/potential/churn]；tagSegmentation.js L40 | `src/api/tagSegmentation.js` | 40 |
| 983 | [x] | GET | `/api/customer-360/tag-stats` | 用户端 | query: params | GET /api/customer-360/tag-stats → 200「获取成功」{total:0,by_category:[],by_source:[]}（无规则时全零）；tagSegmentation.js L44 | `src/api/tagSegmentation.js` | 44 |
| 984 | [x] | GET | `/api/telegram/accounts` | 用户端 | query: params | GET /api/telegram/accounts → 200 data={list:[{id,account_name,bot_username,bot_token_masked,webhook_url,webhook_enabled,ai_agent_enabled,last_sync_at,last_error_*,status,created_at}]}（真实 2 条历史账号）；telegram.js L4 | `src/api/telegram.js` | 4 |
| 985 | [x] | GET | `/api/telegram/accounts/{id}` | 用户端 | path: id | GET /api/telegram/accounts/{id} → 200 详情；99999 → 404「账号不存在」data=record not found；telegram.js L8 | `src/api/telegram.js` | 8 |
| 986 | [x] | POST | `/api/telegram/accounts` | 用户端 | body: data | POST /api/telegram/accounts → 空 body 400（binding 带 TelegramAccountCreateReq 结构体名，A20 口径）；bot_token 长度≠35 → 400「token 长度 X 不等于 35（Telegram 官方 token 固定 35 位）」；合法 35 位 → 200「创建成功（Bot Token 验证 + Webhook/Polling 配置正在后台自动处理中）」id=13；telegram.js L12 | `src/api/telegram.js` | 12 |
| 987 | [x] | PUT | `/api/telegram/accounts/{id}` | 用户端 | path: id / body: data | PUT /api/telegram/accounts/{id} → 改名/ai_agent_enabled → 200「更新成功」；99999 → 404；telegram.js L16 | `src/api/telegram.js` | 16 |
| 988 | [x] | DELETE | `/api/telegram/accounts/{id}` | 用户端 | path: id | DELETE /api/telegram/accounts/{id} → 删 13 → 200「删除成功」；重复删 → 404「账号不存在」；telegram.js L20 | `src/api/telegram.js` | 20 |
| 989 | [x] | POST | `/api/telegram/accounts/{id}/register-webhook` | 用户端 | path: id / body: data | POST /api/telegram/accounts/{id}/register-webhook → webhook_url 非 https → 400「scheme 必须是 https」；路径不以 /api/webhook/telegram/ 开头 → 400；合法格式 → 400「tg setWebhook: do request」（api.telegram.org 外网不可达，环境阻塞非缺陷）；telegram.js L24 | `src/api/telegram.js` | 24 |
| 990 | [x] | POST | `/api/telegram/accounts/{id}/test-send` | 用户端 | path: id / body: data | POST /api/telegram/accounts/{id}/test-send → 缺 chat_id/text → 400（binding）；chat_id 必须是 int64（传字符串 unmarshal 400）；合法 → 400「webhook 地址不可达/连接被拒」（外网不可达，环境阻塞）；telegram.js L28 | `src/api/telegram.js` | 28 |
| 991 | [x] | GET | `/api/telegram/gates` | 用户端 | query: params | GET /api/telegram/gates → 200 data={list:[{id,account_id,chat_id,chat_title,mode,enabled,verify_ttl_min,welcome_msg,...}]}（真实 gate id=4）；telegram.js L34 | `src/api/telegram.js` | 34 |
| 992 | [x] | POST | `/api/telegram/gates` | 用户端 | body: data | POST /api/telegram/gates → 空 body 400（AccountID/ChatID/Mode required）；mode 枚举 oneof=join_request\|mute_unlock（verify_link 400）；合法 join_request → 200「ok」id=6（enabled 默认 false）；telegram.js L38 | `src/api/telegram.js` | 38 |
| 993 | [x] | PUT | `/api/telegram/gates/{id}` | 用户端 | path: id / body: data | PUT /api/telegram/gates/{id} → 复用 telegramGateReq 全量校验（缺 account_id/chat_id/mode 400，全量语义）→ 全量 body → 200「ok」；telegram.js L42 | `src/api/telegram.js` | 42 |
| 994 | [x] | DELETE | `/api/telegram/gates/{id}` | 用户端 | path: id | DELETE /api/telegram/gates/{id} → 删 6 → 200「ok」；重复删 → 404「删除失败」；telegram.js L46 | `src/api/telegram.js` | 46 |
| 995 | [x] | GET | `/api/telegram/gates/{id}/members` | 用户端 | path: id / query: params | GET /api/telegram/gates/{id}/members → 200 {list:[],total:0}；99999 → 404「网关不存在」；telegram.js L50 | `src/api/telegram.js` | 50 |
| 996 | [x] | POST | `/api/telegram/gates/{id}/members/{memberId}/authorize` | 用户端 | path: id, memberId | POST /api/telegram/gates/{id}/members/{memberId}/authorize → 不存在成员 → 404「放行失败」（无真实待放行成员，路径验证通过）；telegram.js L54 | `src/api/telegram.js` | 54 |
| 997 | [x] | GET | `/api/template-market/list` | 用户端 | query: params / body: config | GET 200 {list:[{id:"card-douyin",kind:"template",name,category:"channel_card",summary,source,availability:"builtin",tags,price_cents}],...}（内置模板目录） | `src/api/templateMarket.js` | 15 |
| 998 | [x] | GET | `/api/template-market/categories` | 用户端 | body: undefined; config | GET 200 data=["agentic","assignment","channel_card","integration","live_code","mcp","reply","routing","whatsapp"]（分类白名单） | `src/api/templateMarket.js` | 18 |
| 999 | [x] | GET | `/api/template-market/detail/{id}` | 用户端 | path: id | GET 200 详情（card-douyin）；99999 → code=4004「模板/技能不存在」 | `src/api/templateMarket.js` | 22 |
| 1000 | [x] | POST | `/api/template-market/entries` | 用户端 | body: data | 空 body → code=4000「kind 必须是 template 或 skill」；缺 availability → 4000「availability 必须是 builtin/needs_module/coming_soon」；完整 → 200「已提交审核」status=pending | `src/api/templateMarket.js` | 28 |
| 1001 | [x] | GET | `/api/template-market/my/submissions` | 用户端 | body: undefined; config | GET 200 {list:[{id,kind,name,category,summary,source,availability,status:"pending",submitter_id:54}],total} | `src/api/templateMarket.js` | 32 |
| 1002 | [x] | GET | `/api/template-market/my/orders` | 用户端 | body: undefined; config | GET 200 {list:[{order_no,entry_id,entry_name,buyer_id,amount_cents,status:"paid",paid_at}],total} | `src/api/templateMarket.js` | 36 |
| 1003 | [x] | GET | `/api/template-market/my/owned` | 用户端 | body: undefined; config | GET 200 {list:[{entry:{...approved...},order:{...}}],total}（审核通过+下单后进入已拥有） | `src/api/templateMarket.js` | 40 |
| 1004 | [x] | POST | `/api/template-market/entries/{entryId}/order` | 用户端 | path: entryId / body: data \|\| {} | pending 条目 → 4009「当前状态不允许该操作」；approved 后 → 200「下单成功」{order:{order_no,status:"paid",amount_cents:0},duplicate:false,free_direct:true} | `src/api/templateMarket.js` | 44 |
| 1005 | [x] | POST | `/api/template-market/orders/{orderNo}/cancel` | 用户端 | path: orderNo | 已支付订单 → 4009「当前订单状态不允许该操作」；不存在 → 4004「订单不存在」 | `src/api/templateMarket.js` | 48 |
| 1006 | [x] | GET | `/api/template-market/admin/entries` | 用户端 | query: params / body: config | GET 200 {list:[...],total}（管理端全量条目，含 pending/approved/rejected） | `src/api/templateMarket.js` | 53 |
| 1007 | [x] | POST | `/api/template-market/admin/entries/{entryId}/review` | 用户端 | path: entryId / body: data | {action:approve} → 200「已处理」status=approved reviewed_by=54；{action:reject,reason} → 200 status=rejected reject_reason 落库 | `src/api/templateMarket.js` | 57 |
| 1008 | [x] | GET | `/api/template-market/admin/orders` | 用户端 | query: params / body: config | GET 200 {list:[{order_no,entry_name,buyer_id,status,paid_at}],total} | `src/api/templateMarket.js` | 60 |
| 1009 | [x] | POST | `/api/template-market/admin/orders/{orderNo}/mark-paid` | 用户端 | path: orderNo | 不存在 orderNo → 4004「订单不存在」 | `src/api/templateMarket.js` | 64 |
| 1010 | [x] | GET | `/api/tenant/info` | 用户端 | - | GET 200 {tenant_id:1,tenant_code:"tei6385",name:"E2E计费10191548326",status:1} | `src/api/tenant.js` | 8 |
| 1011 | [x] | GET | `/api/tiktok-card/list` | 用户端 | query: params | GET 200 {list:[{id,title,description,image_url,redirect_url,domain_pool_id,short_link_url,short_code,tags(string),view_count,like_count,share_count,is_active,...}],total} | `src/api/tiktokCard.js` | 4 |
| 1012 | [x] | GET | `/api/tiktok-card/{id}` | 用户端 | path: id | GET 200 详情；99999 → NOT_FOUND_1002「卡片不存在」 | `src/api/tiktokCard.js` | 8 |
| 1013 | [x] | POST | `/api/tiktok-card` | 用户端 | body: data | 空 body → INVALID_PARAM_1001（binding 文案带 TikTokCardCreateRequest.Title，A20 刻意口径）；合法 {title,description,redirect_url,tags} → 200「创建成功」id=28 | `src/api/tiktokCard.js` | 12 |
| 1014 | [x] | PUT | `/api/tiktok-card/{data_id}` | 用户端 | path: data_id / body: data | 200「更新成功」（PUT 全量语义：未传的 description/tags 被置空）；99999 → NOT_FOUND_1002「记录不存在」 | `src/api/tiktokCard.js` | 16 |
| 1015 | [x] | DELETE | `/api/tiktok-card/{id}` | 用户端 | path: id | 200「删除成功」；重复删 → NOT_FOUND_1002「记录不存在」 | `src/api/tiktokCard.js` | 20 |
| 1016 | [x] | POST | `/api/tiktok-card/generate-short-link` | 用户端 | body: { card_id: cardId } | {card_id} → 200「生成成功」short_code=gc2jtdjH、short_link_url=/s/gc2jtdjH 写入；不存在 → 404「资源不存在」 | `src/api/tiktokCard.js` | 24 |
| 1017 | [x] | GET | `/api/tiktok-card/stats/overall` | 用户端 | query: params | GET 200 {total_cards:25,active_cards:22,total_views,popular_cards:[{id,title,view_count,...}]} | `src/api/tiktokCard.js` | 28 |
| 1018 | [x] | GET | `/api/tiktok-card/{cardId}/stats` | 用户端 | path: cardId / query: params | GET 200 {card_id,title,view_count,daily_stats:[{date,view_count}...]}（默认近 7 天） | `src/api/tiktokCard.js` | 32 |
| 1019 | [x] | GET | `/api/admin/tuning/confidence/signals` | 用户端 | query: params | GET 200 {list:[{id,signal_id,session_id,intent_type,intent_conf,intent_conf_calibrated,entity_comp,ctx_relev,rag_qual,llm_entropy,aggregated_conf,dynamic_threshold,decision_band,temperature,...}]} | `src/api/tuning.js` | 4 |
| 1020 | [x] | GET | `/api/admin/tuning/confidence/signals/{id}` | 用户端 | path: id | GET 200 详情；99999 → NOT_FOUND_1002「signal不存在」 | `src/api/tuning.js` | 7 |
| 1021 | [x] | GET | `/api/admin/tuning/confidence/signals/stats` | 用户端 | query: params | GET 200 {bands:{auto:2,llm_fallback:29,review:19},days:7} | `src/api/tuning.js` | 10 |
| 1022 | [x] | GET | `/api/admin/tuning/confidence/calibrations` | 用户端 | query: params | GET 200 {list:[],page:1,size:20,total:0} | `src/api/tuning.js` | 13 |
| 1023 | [x] | GET | `/api/admin/tuning/confidence/policies` | 用户端 | query: params | GET 200 {list:[{policy_id,intent_type,base_threshold,customer_level_weight,timeslot_weight,agent_availability_weight,band_handoff_upper,band_fallback_upper,band_review_upper,review_sla_seconds,is_active,version}]} | `src/api/tuning.js` | 16 |
| 1024 | [x] | PUT | `/api/admin/tuning/confidence/policies` | 用户端 | body: data | 缺 version → INVALID_PARAM_1001（ThresholdPolicyRequest.Version min）；完整（version≥1）→ 200 更新 version+1；policy_id 不存在则插入（upsert 语义） | `src/api/tuning.js` | 19 |
| 1025 | [x] | GET | `/api/admin/tuning/humanize/scores` | 用户端 | query: params | GET 200 {list:[],page:1,size:2,total:0} | `src/api/tuning.js` | 23 |
| 1026 | [x] | GET | `/api/admin/tuning/humanize/scores/stats` | 用户端 | query: params | GET 200 {avg_score:0,days:7,passed_count:0,failed_count:0,pass_rate:0,total_count:0} | `src/api/tuning.js` | 26 |
| 1027 | [x] | GET | `/api/admin/tuning/humanize/baselines` | 用户端 | query: params | GET 200 {list:[]} | `src/api/tuning.js` | 29 |
| 1028 | [x] | GET | `/api/admin/tuning/humanize/low-quality` | 用户端 | query: params | GET 200 {list:[],page:1,size:20,total:0} | `src/api/tuning.js` | 32 |
| 1029 | [x] | GET | `/api/admin/tuning/feedback/events` | 用户端 | query: params | GET 200 {list:[{event_id,session_id,customer_id,event_type,signal_key,signal_value,weight,reward,ai_reply,customer_msg,...}]} | `src/api/tuning.js` | 36 |
| 1030 | [x] | GET | `/api/admin/tuning/feedback/events/stats` | 用户端 | query: params | GET 200 {days:7,signals:{}} | `src/api/tuning.js` | 39 |
| 1031 | [x] | GET | `/api/admin/tuning/feedback/dialogues` | 用户端 | query: params | GET 200 {list:[],next_cursor:"",page:1,size:2,total:0} | `src/api/tuning.js` | 42 |
| 1032 | [x] | GET | `/api/admin/tuning/prompt/candidates` | 用户端 | query: params | GET 200 {list:[],next_cursor:"",page:1,size:2,total:0} | `src/api/tuning.js` | 45 |
> **缺陷 D-38（已修复）**：`PUT /api/admin/tuning/prompt/candidates/{id}/status` 对不存在的候选回 200「成功」（回显 {id,status}），前端「批准/驳回」按钮会误以为已落库；同时 status 不校验枚举，拼错的值（aproved）会写进库，候选此后既不会被 A/B 选中也不会被淘汰。修复：`repository/feedback_loop.go` 的 UpdatePromptCandidateStatus 检查 RowsAffected==0 → gorm.ErrRecordNotFound；`service/tuning.go` 新增 `ErrInvalidCandidateStatus` 与六值白名单（draft/pending/approved/active/promoted/retired）；controller 分别映射 404「候选不存在」与 400「无效的候选状态：可选 …」。复测：99999 → 404；aproved → 400 列出可选值。
| 1033 | [x] | PUT | `/api/admin/tuning/prompt/candidates/{id}/status` | 用户端 | path: id / body: data | D-38 修复后：不存在 99999 → 404 NOT_FOUND_1002「候选不存在」（修复前 200 假成功）；status=aproved → 400「无效的候选状态：可选 draft/pending/approved/active/promoted/retired」 | `src/api/tuning.js` | 48 |
| 1034 | [x] | GET | `/api/admin/tuning/bandit/arms` | 用户端 | query: params | GET 200 {list:[],next_cursor:"",page:1,size:20,total:0} | `src/api/tuning.js` | 51 |
| 1035 | [x] | GET | `/api/unified-messages` | 用户端 | query: params | GET 200 {list:[{id:305,message_id,platform:"telegram",account_id,chat_id,sender_id,content,content_type,status,received_at,...}],total} | `src/api/unifiedMessage.js` | 5 |
> **缺陷 D-41（已修复）**：`GET /api/unified-messages/{id}` 对不存在的消息回 404，但文案是 gorm 的 `record not found` 被 S-36 原文掩码规则洗成「操作失败，请稍后重试」——运维看不出到底是「消息不存在」还是服务故障。修复：controller 改为固定文案「消息不存在」。复测：99999 → 404 NOT_FOUND_1002「消息不存在」。
| 1036 | [x] | GET | `/api/unified-messages/{id}` | 用户端 | path: id | GET 200 详情；99999 → D-41 修复后 404 NOT_FOUND_1002「消息不存在」（修复前文案被掩码成「操作失败，请稍后重试」） | `src/api/unifiedMessage.js` | 9 |
| 1037 | [x] | GET | `/api/unknown-questions` | 用户端 | query: params | GET 200 {list:[{id,question_hash,question,cluster_id,status,source,frequency,adopted_faq_id,reviewed_by,reviewed_at,...}]} | `src/api/unknownQuestion.js` | 7 |
| 1038 | [x] | GET | `/api/unknown-questions/stats` | 用户端 | - | GET 200 {adopt_rate:1,adopted:2,pending:0,total:2} | `src/api/unknownQuestion.js` | 10 |
| 1039 | [x] | GET | `/api/unknown-questions/{id}` | 用户端 | path: id | GET 99999 → NOT_FOUND_1002「目标不存在」 | `src/api/unknownQuestion.js` | 13 |
| 1040 | [x] | POST | `/api/unknown-questions/{id}/submit` | 用户端 | path: id | 99999 → NOT_FOUND_1002「未知问题不存在或状态已变更」；status=draft 的记录 → 200 {id,status:"pending_review"} | `src/api/unknownQuestion.js` | 16 |
> **缺陷 D-39（已修复）**：`POST /api/unknown-questions/{id}/review` 与 `/clusters/{id}/review` 把「目标不存在」（service 的 `reflux.ErrNotFound`）和入参错误一起回 400，客户端无法区分「记录已被删/不属于本租户」与「body 写错」；另外未知 action 复用「非法状态迁移」文案，排查方向会被误导到状态机。修复：controller 用 `errors.Is` 把 ErrNotFound 映射 404「目标不存在」；service 未知 action 改为「action 必须是 adopt/ignore/merge（收到 %q）」。复测：99999 → 404；action=bogus → 400 且文案点明 action。
| 1041 | [x] | POST | `/api/unknown-questions/{id}/review` | 用户端 | path: id / body: data | D-39 修复后：不存在 → 404「目标不存在」（修复前 400）；action=bogus → 400「action 必须是 adopt/ignore/merge（收到 \"bogus\"）」；adopt 缺 answer → 400「answer 必填」；记录 agent_id=0 → 400「agent_id 必填 (Task 15 强 1对1)」；agent_id=34 且 pending → 200 {id,faq_id,status:"adopted"} | `src/api/unknownQuestion.js` | 19 |
| 1042 | [x] | GET | `/api/unknown-questions/clusters` | 用户端 | query: params | GET 200 {list:[],page:1,page_size:20,total:0} | `src/api/unknownQuestion.js` | 23 |
| 1043 | [x] | GET | `/api/unknown-questions/clusters/{id}` | 用户端 | path: id | GET 99999 → NOT_FOUND_1002「目标不存在」 | `src/api/unknownQuestion.js` | 26 |
| 1044 | [x] | POST | `/api/unknown-questions/clusters/{id}/review` | 用户端 | path: id / body: data | D-39 修复后：99999 → 404「目标不存在」（修复前 400） | `src/api/unknownQuestion.js` | 29 |
| 1045 | [x] | GET | `/api/user-segment/rfm/list` | 用户端 | query: params | GET 200 {list:[{customer_id,unified_id,recency_days,frequency,monetary_total,avg_order_value,r_score,f_score,m_score,composite_score,segment,churn_risk_level,churn_score,...}]} | `src/api/userSegment.js` | 4 |
| 1046 | [x] | GET | `/api/user-segment/rfm/rule` | 用户端 | - | GET 200 {id:2,name:"",r_days_1..5,f_count_1..5,m_amount_1..5,is_active} | `src/api/userSegment.js` | 7 |
> **缺陷 D-40（已修复）**：`SaveRFMRuleRequest.Name` 标了 `binding:"required"`，但历史规则（id=2，2026-08-31 建）的 name 是空串——前端按 GET 回填后原样提交，改一个阈值就被打回 400，逼运营先编名字。同时 `UpdateRFMRule` 把仓储的 not-found 包成 `errors.New("规则不存在")`，只能落 400。修复：name 去 required（仅备注，不参与打分）；Update 保留 gorm.ErrRecordNotFound 语义，controller 用 errhttp.IsNotFoundError 映射 404「规则不存在」。复测：空 name 更新成功；99999 → 404。
| 1047 | [x] | POST | `/api/user-segment/rfm/rule` | 用户端 | body: data | D-40 修复后 name 可选：200「保存成功」id=27（修复前缺 name → 400 required）；测后已删 | `src/api/userSegment.js` | 10 |
| 1048 | [x] | PUT | `/api/user-segment/rfm/rule/{id}` | 用户端 | path: id / body: data | 200「更新成功」（全量更新）；99999 → D-40 修复后 404「规则不存在」（修复前 400） | `src/api/userSegment.js` | 13 |
| 1049 | [x] | GET | `/api/user-segment/rfm/user?user_id={userId}` | 用户端 | path: userId | GET ?user_id=<customer_id> → 200 客户 RFM；无记录 → NOT_FOUND_1002「未找到客户 RFM 信息」 | `src/api/userSegment.js` | 16 |
| 1050 | [x] | GET | `/api/user-segment/rfm/stats` | 用户端 | - | GET 200 {layer_count:{potential:138},segment_count:{potential:138},total_users:138} | `src/api/userSegment.js` | 19 |
| 1051 | [x] | POST | `/api/user-segment/rfm/calculate` | 用户端 | body: data | POST 200「计算完成」{updated_count:20} | `src/api/userSegment.js` | 22 |
| 1052 | [x] | GET | `/api/user-segment/layers` | 用户端 | - | GET 200 5 层字典（champion/loyal/at_risk/potential/churn） | `src/api/userSegment.js` | 25 |
| 1053 | [x] | GET | `/api/user-segments` | 用户端 | query: params | GET 200 {list:[{id,name,description,rules_json,trigger,size,created_at}],total} | `src/api/userSegment.js` | 29 |
| 1054 | [x] | DELETE | `/api/user-segment/rfm/rule/{id}` | 用户端 | path: id | 200「删除成功」；不存在/重复删 → NOT_FOUND_1002「记录不存在」 | `src/api/userSegment.js` | 41 |
| 1055 | [x] | GET | `/api/user-segment/rfm/list?segment_id={id}` | 用户端 | path: id / query: params | GET ?segment=potential → 200 {list:[...]}（分层过滤生效） | `src/api/userSegment.js` | 44 |
| 1056 | [x] | GET | `/api/users` | 用户端 | query: params | GET 200 {list:[{id,username,email,phone,real_name,role,status,last_login,created_at}],total} | `src/api/users.js` | 5 |
| 1057 | [x] | GET | `/api/users/{id}` | 用户端 | path: id | GET 200 详情；99999 → NOT_FOUND_1002「用户不存在」 | `src/api/users.js` | 9 |
| 1058 | [x] | POST | `/api/users` | 用户端 | body: data | 空 body → INVALID_PARAM_1001（Username/Password/Role required）；合法 → 200「创建用户成功」id=100；重复用户名 → 400「用户名已存在」 | `src/api/users.js` | 13 |
| 1059 | [x] | PUT | `/api/users/{id}` | 用户端 | path: id / body: data | 200「更新用户成功」；99999 → 404 | `src/api/users.js` | 17 |
| 1060 | [x] | DELETE | `/api/users/{id}` | 用户端 | path: id | 200「删除用户成功」；重复删 → NOT_FOUND_1002「用户不存在」 | `src/api/users.js` | 21 |
| 1061 | [x] | PUT | `/api/users/{id}/password` | 用户端 | path: id / body: data | body 字段是 password（不是 new_password）；缺 → INVALID_PARAM_1001「Password required」；合法 → 200 改密 | `src/api/users.js` | 25 |
| 1062 | [x] | POST | `/api/auth/login` | 公开 | body: data | 正确凭据 → 200「登录成功」data.token；错误 → UNAUTHORIZED_2001「用户名或密码错误」 | `src/api/users.js` | 29 |
| 1063 | [x] | POST | `/api/auth/mfa/verify` | 公开 | body: data | 缺 temp_token/code → INVALID_PARAM_1001（MFAVerifyRequest 字段名）；temp_token 无效 → UNAUTHORIZED_2001「临时令牌无效」 | `src/api/users.js` | 34 |
> **缺陷 D-37（已修复，安全）**：`GET /api/wecom/health/accounts/risks` 与 `/api/wecom/health/accounts/select` 直接返回 `model.WeComAccount`，**corp_secret 明文**（实测 `hack_secret`），而同模块 `GET /api/wecom/health/accounts` 已做掩码——脱敏工具 `controller/wecom_secret_mask.go` 齐全，只是这两个 handler 漏调（任何登录用户都能读走商户企微应用密钥）。修复：GetRiskAccounts 逐条 `maskWeComAccount(&list[i])`、SelectHealthyAccount 调 `maskWeComAccount(acc)`。复测三端点均返回 `****cret`。
| 1064 | [x] | GET | `/api/wecom/health/accounts` | 用户端 | - | GET 200 data=[{account:{...corp_secret:"****cret"...},health:{...}}]（密钥脱敏） | `src/api/wecomAccount.js` | 5 |
| 1065 | [x] | GET | `/api/wecom/health/accounts/risks` | 用户端 | - | D-37 修复后：200 风险账号列表，corp_secret="****cret" 掩码（修复前明文 hack_secret 泄漏） | `src/api/wecomAccount.js` | 9 |
| 1066 | [x] | GET | `/api/wecom/health/accounts/select` | 用户端 | - | D-37 修复后：200 最优账号，corp_secret="****cret" 掩码（修复前明文） | `src/api/wecomAccount.js` | 13 |
| 1067 | [x] | GET | `/api/wecom/health/accounts/summary` | 用户端 | - | GET 200 {total_accounts:1,online_count:1,offline_count:0,banned_count:0,warning_count:1,critical_count:0,avg_score:70,total_quota:500,total_used:0,accounts:[...]} | `src/api/wecomAccount.js` | 17 |
| 1068 | [x] | GET | `/api/wecom/health/accounts/{id}` | 用户端 | path: id | 200 {tenant_id,id:1,account_id:9,platform:"wecom",health_score:70,risk_level:"warning",login_state,quota_used,quota_total,quota_usage_rate,success_rate:100,error_count,last_error,metrics,reported_at,created_at}（path :id 是账号ID，取最新一条健康记录） | `src/api/wecomAccount.js` | 21 |
| 1069 | [x] | GET | `/api/wecom/health/accounts/{id}/history` | 用户端 | path: id / query: params | 200 {list:[同 1068 健康记录结构],page,page_size,total:1}（按上报时间倒序分页；:id 是账号ID） | `src/api/wecomAccount.js` | 25 |
| 1070 | [x] | POST | `/api/wecom/health/accounts/{id}` | 用户端 | path: id / body: data | body 可省 account_id（用 path）→ 200「上报成功」data=健康记录 {health_score,risk_level,login_state,quota_*,success_rate,reported_at}；缺 login_state 等仍可只上报分数 | `src/api/wecomAccount.js` | 29 |
| 1071 | [x] | POST | `/api/wecom/health/accounts/{id}/status` | 用户端 | path: id / body: data | 缺 login_state → 400 INVALID_PARAM_1001「请求参数错误」；{login_state:"online"} → 200「更新成功」（可选 risk 一并写 risk_level） | `src/api/wecomAccount.js` | 33 |
| 1072 | [x] | POST | `/api/wecom/health/accounts/{id}/quota/consume` | 用户端 | path: id / body: { count } | {count:1} → 200「消耗成功」data={account_id,quota_used}（quota_used 累加） | `src/api/wecomAccount.js` | 37 |
| 1073 | [x] | POST | `/api/wecom/health/accounts/quota/reset` | 用户端 | - | 200「重置成功」data={affected:1}（全部账号 quota_used 归零） | `src/api/wecomAccount.js` | 41 |
| 1074 | [x] | POST | `/api/wecom/messages/ingest` | 用户端 | body: data | 空 body → 400 INVALID_PARAM_1001「account_id, external_user_id are required」；{account_id:9,external_user_id:"apitest_wx_1",name,msg_type:"text",content} → 200「接入成功」data={hub:{id,platform:"wecom",direction:"inbound",conversation_id:"wecom-9-apitest_wx_1"},conversation:{id,platform,account_id,customer_id,unread_count,...}} | `src/api/wecomAccount.js` | 45 |
<br>
> **缺陷 D-42（已修复）**：`POST /api/wecom/messages/send` 把企微**上游**投递失败（errcode=40013 invalid corpid、网络不可达）统一回 400 `INVALID_PARAM_1001`，客户端会原样重试、运维也看不出该改凭据。根因：`internal/controller/wecom_health.go` SendMessage 错误分支固定 `response.Error(ctx, 400, err.Error())`。修复：① `internal/service/wecom_integration.go` 新增哨兵 `ErrWeComUpstreamSend`，出站调用失败处改 `fmt.Errorf("%w: wecom outbound api: %w", ErrWeComUpstreamSend, serr)`（双 %w 保留上游原始错误链）；② controller 用 `errors.Is(err, service.ErrWeComUpstreamSend)` → `response.ErrorWithCode(ctx, utils.ErrorCodeUpstreamFailure, http.StatusBadGateway, err.Error())`，其余错误维持 400。复测：合法 body → 502 `UPSTREAM_FAILURE_5005` 且 errcode 原文保留；空 body → 仍 400 `INVALID_PARAM_1001`。
| 1075 | [x] | POST | `/api/wecom/messages/send` | 用户端 | body: data | 空 body → 400 INVALID_PARAM_1001「account_id, external_user_id are required」（入参校验仍 400）；合法 body 但企微凭据无效 → **502 UPSTREAM_FAILURE_5005**「wecom upstream send failed: wecom outbound api: channel error []: wecom token errcode=40013 errmsg=invalid corpid…」（D-42 修复前误回 400 INVALID_PARAM_1001，上游失败被当成入参错误、客户端会原样重试） | `src/api/wecomAccount.js` | 49 |
| 1076 | [x] | POST | `/api/wecom/accounts` | 用户端 | body: data | 空 body → 400 INVALID_PARAM_1001「请求参数错误：Key: 'CreateAccountRequest.CorpID' … required」（A20 刻意口径：告知哪个字段不合格）；合法 {corp_id,corp_secret,agent_id,agent_secret,callback_token,encoding_aes_key} → 200「创建成功」id=15，corp_secret/agent_secret/callback_token 掩码 ****1076、encoding_aes_key 掩码 ****DEFG，默认 status:1/login_state:"offline"/daily_msg_quota:500/weight:100 | `src/api/wecomAccount.js` | 53 |
| 1077 | [x] | PUT | `/api/wecom/accounts/{id}` | 用户端 | path: id / body: data | {corp_id,agent_id,webhook_enabled} → 200「更新成功」agent_id 1000011→1000022、webhook_enabled true 生效，corp_secret 留空保持原值（指针/密钥保留语义）；99999 → 404 NOT_FOUND_1002（文案被掩码为「操作失败，请稍后重试」，观察项） | `src/api/wecomAccount.js` | 57 |
| 1078 | [x] | DELETE | `/api/wecom/accounts/{id}` | 用户端 | path: id | 200「删除成功」；重复删 → 404 NOT_FOUND_1002（同掩码文案） | `src/api/wecomAccount.js` | 61 |
| 1079 | [x] | GET | `/api/wecom/customers` | 用户端 | query: params | 200 {list:[],page:1,page_size:3,total:0}（按账号/页码分页；当前库无企微客户，空列表非缺陷） | `src/api/wecomAccount.js` | 65 |
| 1080 | [x] | GET | `/api/wecom/groups` | 用户端 | query: params | 200 {list:[],page,page_size,total:0} | `src/api/wecomAccount.js` | 69 |
| 1081 | [x] | GET | `/api/wecom/tags` | 用户端 | - | 200 data=[]（数组，非分页对象；当前无企微标签） | `src/api/wecomAccount.js` | 73 |
| 1082 | [x] | GET | `/api/wecom/messages` | 用户端 | query: params | 200 {list:[],page,page_size,total:0} | `src/api/wecomAccount.js` | 77 |
| 1083 | [x] | POST | `/api/wecom/accounts/{id}/sync-customers` | 用户端 | path: id | 账号 9（历史账号，corp 凭据无效）→ 502 UPSTREAM_FAILURE_5005「channel error [unknown]: wecom token errcode=40013 errmsg=invalid corpid…」（respondWeComError 分类正确；真实同步需有效 corp_id/secret，环境阻塞） | `src/api/wecomAccount.js` | 81 |
| 1084 | [x] | POST | `/api/wecom/accounts/{id}/sync-groups` | 用户端 | path: id | 同 1083 → 502 UPSTREAM_FAILURE_5005 errcode=40013 invalid corpid（环境阻塞：无有效企微凭据） | `src/api/wecomAccount.js` | 85 |
| 1085 | [x] | POST | `/api/wecom/accounts/{id}/sync-tags` | 用户端 | path: id | 同 1083 → 502 UPSTREAM_FAILURE_5005 errcode=40013 invalid corpid（环境阻塞） | `src/api/wecomAccount.js` | 89 |
| 1086 | [x] | POST | `/api/wecom/accounts/{id}/refresh` | 用户端 | path: id | 同 1083 → 502 UPSTREAM_FAILURE_5005 errcode=40013 invalid corpid（刷新 access_token 需有效凭据，环境阻塞） | `src/api/wecomAccount.js` | 93 |
| 1087 | [x] | POST | `/api/wecom/accounts/{id}/send-message` | 用户端 | path: id / body: data | 空 body → 400 INVALID_PARAM_1001「请求参数错误：Key: 'WeComSendMessageRequest.MsgType' … required」；合法 {to_user,msg_type:"text",content} → 502 UPSTREAM_FAILURE_5005 errcode=40013 invalid corpid（上游失败分类正确）；账号不存在 99999 → 404 NOT_FOUND_1002（掩码文案） | `src/api/wecomAccount.js` | 97 |
| 1088 | [x] | GET | `/api/whatsapp/accounts` | 用户端 | query: params | 200 data=[{tenant_id,id(uuid),name,remark,status:"pending",phone_id,token,app_id,app_secret,type:"cloud",created_at,updated_at}]（**数组非分页对象**，page/page_size 不入响应）；token/app_secret 出参已掩码（maskWhatsappAccountSecrets），不回明文 | `src/api/whatsapp.js` | 3 |
| 1089 | [x] | POST | `/api/whatsapp/accounts` | 用户端 | body: data | 空 body {} → 200 也创建成功（name/remark 均可选）；{name:"apitest_wa_1089",remark} → 200「创建账号成功」id=2a70622f-4c77-4e36-a8a0-3ee56dea5b56，status 默认 pending、type cloud，密钥字段空 | `src/api/whatsapp.js` | 5 |
| 1090 | [x] | POST | `/api/whatsapp/accounts/{id}/login/start` | 用户端 | path: id | 200 账号 → **410 WHATSAPP_LOGIN_RETIRED_8401**「whatsapp provider does not support this operation: 个人版扫码登录已下线（gowa 弃维护，W-3 换库收口），请使用 Cloud API 账号（PhoneID+Token）」（能力下线语义，非缺陷） | `src/api/whatsapp.js` | 6 |
| 1091 | [x] | GET | `/api/whatsapp/accounts/{id}/login/status` | 用户端 | path: id | 200 {logged_in:false,qr:""}；不存在 UUID 也返回 200 logged_in:false（不校验账号存在性，观察项）；非 UUID → 400 INVALID_PARAM_1001「账号ID错误」data="invalid UUID length: 3" | `src/api/whatsapp.js` | 7 |
| 1092 | [x] | PUT | `/api/whatsapp/accounts/{id}` | 用户端 | path: id / body: data | {name:"apitest_wa_v2",remark:"updated"} → 200「更新账号成功」回显新值；**响应 updated_at 未刷新仍等于 created_at（观察项，非功能故障）** | `src/api/whatsapp.js` | 8 |
| 1093 | [x] | DELETE | `/api/whatsapp/accounts/{id}` | 用户端 | path: id | 200「删除账号成功」；重复删 → 404 NOT_FOUND_1002「删除账号失败」 | `src/api/whatsapp.js` | 9 |
| 1094 | [x] | GET | `/api/whatsapp/drafts` | 用户端 | query: params | 200 {list:[{tenant_id,id,title,content,created_at,updated_at}],total}（page_size 缺省 10、封顶 50；历史草稿 title/content 多为空） | `src/api/whatsapp.js` | 11 |
| 1095 | [x] | POST | `/api/whatsapp/drafts` | 用户端 | body: data | {title,content} → 200「创建草稿成功」id=5d12be2f-2295-47f4-a9b4-4f9cc46a5c6e | `src/api/whatsapp.js` | 12 |
| 1096 | [x] | PUT | `/api/whatsapp/drafts/{id}` | 用户端 | path: id / body: data | {title,content} → 200「更新草稿成功」回显新值（updated_at 同样未刷新，观察项）；不存在 UUID → 404 NOT_FOUND_1002「草稿不存在」 | `src/api/whatsapp.js` | 13 |
| 1097 | [x] | DELETE | `/api/whatsapp/drafts/{id}` | 用户端 | path: id | 200「删除草稿成功」；重复删 → 404 NOT_FOUND_1002「删除草稿失败」 | `src/api/whatsapp.js` | 14 |
| 1098 | [x] | GET | `/api/whatsapp/jobs` | 用户端 | query: params | 200 {list:[],total:0}（当时无任务；响应不带 page/page_size） | `src/api/whatsapp.js` | 16 |
| 1099 | [x] | POST | `/api/whatsapp/jobs` | 用户端 | body: data | {draft_id} → 200「群发任务已创建」{id:uuid,status:"pending",total:0,success:0,failed:0}；空 body → 400 INVALID_PARAM_1001「草稿ID错误」data="invalid UUID length: 0" | `src/api/whatsapp.js` | 17 |
| 1100 | [x] | GET | `/api/whatsapp/jobs/{id}` | 用户端 | path: id | 200 任务详情 {draft_id,status,total,success,failed,created_at,updated_at}；不存在 UUID → 404 NOT_FOUND_1002「任务不存在」 | `src/api/whatsapp.js` | 18 |
| 1101 | [x] | DELETE | `/api/whatsapp/jobs/{id}` | 用户端 | path: id | 200「删除任务成功」；重复删 → 404 NOT_FOUND_1002「删除任务失败」 | `src/api/whatsapp.js` | 19 |
| 1102 | [x] | GET | `/api/whatsapp-cloud/accounts` | 用户端 | query: params | 200 data=[]（**数组非分页对象**；当前库无 Cloud 账号） | `src/api/whatsappCloud.js` | 6 |
| 1103 | [x] | GET | `/api/whatsapp-cloud/accounts/{id}` | 用户端 | path: id | 200 {id,account_name,phone_number_id,whatsapp_business_id,access_token_masked:"EAAp****1104",verify_token,app_secret_masked:"apps****1104",webhook_enabled,ai_agent_enabled,last_sync_at,last_error_*,status:1,created_at,updated_at}（密钥掩码，不回明文）；99999 → 404 NOT_FOUND_1002「账号不存在」data="record not found" | `src/api/whatsappCloud.js` | 10 |
| 1104 | [x] | POST | `/api/whatsapp-cloud/accounts` | 用户端 | body: data | 空 body → 400 INVALID_PARAM_1001「参数错误: Key: 'whatsAppCloudCreateReq.AccountName' … PhoneNumberID / WhatsAppBusinessID / AccessToken 均 required」（一次列出全部缺失字段）；合法 {account_name,phone_number_id,whatsapp_business_id,access_token,verify_token,app_secret} → 200「创建成功」id=4，access_token/app_secret 掩码 | `src/api/whatsappCloud.js` | 14 |
| 1105 | [x] | PUT | `/api/whatsapp-cloud/accounts/{id}` | 用户端 | path: id / body: data | 指针语义 {account_name:"apitest_cloud_v2",webhook_enabled:true} → 200「更新成功」，未传的密钥保持原值掩码仍 ****1104，updated_at 刷新；99999 → 404「账号不存在」 | `src/api/whatsappCloud.js` | 18 |
| 1106 | [x] | DELETE | `/api/whatsapp-cloud/accounts/{id}` | 用户端 | path: id | 200「删除成功」；重复删 → 404 NOT_FOUND_1002「账号不存在」 | `src/api/whatsappCloud.js` | 22 |
| 1107 | [x] | POST | `/api/whatsapp-cloud/accounts/{id}/test-send` | 用户端 | path: id / body: data | 空 body → 400 INVALID_PARAM_1001（ToPhone/Content required）；合法 {to_phone,content} → **502 UPSTREAM_FAILURE_5005「发送失败（渠道不可达或凭据无效）」**（外网/凭据不可用，环境阻塞；上游失败分类正确且不泄漏内部细节）；99999 → 404「账号不存在」 | `src/api/whatsappCloud.js` | 26 |
| 1108 | [x] | GET | `/api/workflows/versions` | 用户端 | body: {} | 200 {list,total,...} 分页；真实数据 id=5「测试」draft、id=2 wf-e2e-test active | `src/api/workflowOrchestrator.js` | 6 |
| 1109 | [x] | GET | `/api/workflows/versions` | 用户端 | body: { workflow_id: workflowIdOrParams } | 200 与无参同返回全量分页列表（服务端只读 query，GET body 的 workflow_id 被忽略——观察项：过滤需改用 query） | `src/api/workflowOrchestrator.js` | 9 |
| 1110 | [x] | GET | `/api/workflows/versions` | 用户端 | query: params | 200 分页生效（page/page_size）；带 ?workflow_id=wf-e2e-test 时 data 为数组（非分页对象） | `src/api/workflowOrchestrator.js` | 17 |
| 1111 | [x] | GET | `/api/workflows/versions/{id}` | 用户端 | path: id | 200 详情；99999 → 404 NOT_FOUND_1002「工作流版本不存在」 | `src/api/workflowOrchestrator.js` | 21 |
| 1112 | [x] | POST | `/api/workflows/versions` | 用户端 | body: data | 空 body 400「请求参数错误：请检查字段类型与必填项」；合法 {workflow_id,name,definition{edges,nodes}} → 200「创建成功」id=8 status=draft | `src/api/workflowOrchestrator.js` | 25 |
| 1113 | [x] | PUT | `/api/workflows/versions/{id}` | 用户端 | path: id / body: data | 200「更新成功」（name+definition 必填，全量语义）；99999 → 404（掩码文案） | `src/api/workflowOrchestrator.js` | 29 |
| 1114 | [x] | DELETE | `/api/workflows/versions/{id}` | 用户端 | path: id | 200「删除成功」；重复删 404（D-43 后） | `src/api/workflowOrchestrator.js` | 33 |
> **缺陷 D-43（已修复）**：`POST /api/workflows/versions/{id}/publish`、`/archive` 与 `DELETE /api/workflows/versions/{id}` 对不存在的 id 回 200「发布成功/归档成功/删除成功」假成功。根因：`internal/repository/workflow_orchestrator.go` 的 `UpdateStatus` / `DeleteByID` 忽略 `RowsAffected`（只返回 Update/Delete 的 Error）。修复：两方法在 `RowsAffected == 0` 时返回 `gorm.ErrRecordNotFound`；`internal/controller/workflow_orchestrator.go` 三处错误分支增加 `errhttp.IsNotFoundError(err)` → 404「工作流版本不存在」，其余仍走 ErrorFromDB。复测：99999 三端点均 404 NOT_FOUND_1002；真实版本 publish/delete 200、重复删 404。<br>
| 1115 | [x] | POST | `/api/workflows/versions/{id}/publish` | 用户端 | path: id | 200「发布成功」（draft/archived→published）；99999 → 404「工作流版本不存在」（D-43 后） | `src/api/workflowOrchestrator.js` | 37 |
| 1116 | [x] | POST | `/api/workflows/versions/{id}/archive` | 用户端 | path: id | 200「归档成功」；99999 → 404「工作流版本不存在」（D-43 后） | `src/api/workflowOrchestrator.js` | 41 |
| 1117 | [x] | POST | `/api/workflows/execute` | 用户端 | body: data | 空 body 400；需存在已发布版本（无 → 400「no published version found」）→ 200「执行成功」execution id=3 status=running | `src/api/workflowOrchestrator.js` | 45 |
| 1118 | [x] | GET | `/api/workflows/executions` | 用户端 | query: params | 200 {list,total}（id=3 running、历史 id=2 terminated） | `src/api/workflowOrchestrator.js` | 49 |
| 1119 | [x] | GET | `/api/workflows/executions/{id}` | 用户端 | path: id | 200 详情；99999 → 404「执行记录不存在」 | `src/api/workflowOrchestrator.js` | 53 |
| 1120 | [x] | GET | `/api/workflows/executions/{executionId}/nodes` | 用户端 | path: executionId | 200 data=[]（该执行无节点明细） | `src/api/workflowOrchestrator.js` | 57 |
| 1121 | [x] | POST | `/api/workflows/executions/{id}/stop` | 用户端 | path: id | 200「停止成功」；再次 stop → 409 WORKFLOW_STATE_CONFLICT_8301「execution is not in running state」；99999 → 404「执行不存在」 | `src/api/workflowOrchestrator.js` | 61 |
| 1122 | [x] | GET | `/api/xianyu/list` | 用户端 | query: params | 200 {list,total:18,page,page_size,total_page}（卡片字段 id/title/description/image_url/redirect_url/domain_pool_id/short_link_url/short_code/tags/like_count/share_count/view_count/is_active）；无 token 401 | `src/api/xianyuCard.js` | 4 |
| 1123 | [x] | GET | `/api/xianyu/{id}` | 用户端 | path: id | 200 详情；99999 → 404「记录不存在」 | `src/api/xianyuCard.js` | 8 |
| 1124 | [x] | POST | `/api/xianyu/create` | 用户端 | body: data | 空 body 400（binding 泄 XianyuCardCreateRequest.Title/ImageURL，A20 口径）；合法 → 200「操作成功」id=34 short_code=3u3SPW；必填 title+image_url | `src/api/xianyuCard.js` | 12 |
| 1125 | [x] | PUT | `/api/xianyu/update` | 用户端 | body: data | 200「更新成功」（**全量替换语义**：未传 description/tags/redirect_url 被清空，与 douyin/kuaishou 保留原值不同—观察项）；缺 title/image_url → 400；99999 → 404；不重建短链 | `src/api/xianyuCard.js` | 16 |
| 1126 | [x] | DELETE | `/api/xianyu/delete/{id}` | 用户端 | path: id | 200「删除成功」data={id}；重复删 → 404「记录不存在」 | `src/api/xianyuCard.js` | 20 |
| 1127 | [x] | GET | `/api/xianyu/view/{id}` | 用户端 | path: id | 200 卡片详情（等同 GetByID 不记浏览；真实埋点 POST /api/xianyu/:id/view 需卡片访问凭证，未带 401「缺少卡片访问凭证」）；99999 → 404 | `src/api/xianyuCard.js` | 24 |
| 1128 | [x] | GET | `/api/xianyu/stats/card/{id}` | 用户端 | path: id / query: params | 200 {chart{dates,views,clicks,shares},details{list,page,page_size,total},stats{total_views,total_clicks,total_shares,click_rate}}；默认窗口 7 天；**不存在 id 也 200 全零（观察项）**；id=abc → 400「卡片ID格式错误」 | `src/api/xianyuCard.js` | 28 |
| 1129 | [x] | GET | `/api/xianyu/stats/overall` | 用户端 | query: params | 200 {chart,overall{total_cards:19,active_cards:18,total_views,total_clicks,total_shares},ranking[{id,title,view_count,click_count,click_rate}]} | `src/api/xianyuCard.js` | 32 |
> **缺陷 D-44（已修复）**：`POST /api/{douyin,kuaishou,xianyu,tiktok}/generate-short-link` 原先「先删旧短链 → 生成短码 → `if card.RedirectURL == "" { return nil }` 提前返回」：卡片未配 redirect_url 时旧 `/s/xxx` 被删、新链未建、接口仍回 200（幂等重生成反而弄丢线上链接）。修复：新增 `service/cards/card_shortlink_errors.go` 的 `ErrCardRedirectURLRequired`，四家 GenerateShortLink 把空 redirect 校验提到删除旧链之前；删除前先 GetByID 确认旧链存在（悬空 short_link_id 容错）；controller 映射 400「卡片未配置跳转地址（redirect_url），无法生成短链」。实测 xianyu/douyin 清空 redirect 后 400 且旧链保留。
| 1130 | [x] | POST | `/api/xianyu/{id}/generate-short-link` | 用户端 | path: id | 正常 200 {short_code,short_link_url}；**无 redirect_url 时 400「卡片未配置跳转地址（redirect_url），无法生成短链」（D-44）**；99999 → 404 | `src/api/xianyuCard.js` | 36 |
| 1131 | [x] | GET | `/api/xiaohongshu/list` | 用户端 | query: params | 200 同构分页（19 张，id=29 short_code=Ajvxgw）；无 token 401 | `src/api/xiaohongshuCard.js` | 4 |
| 1132 | [x] | GET | `/api/xiaohongshu/{id}` | 用户端 | path: id | 200 详情；99999 → 404「记录不存在」 | `src/api/xiaohongshuCard.js` | 8 |
| 1133 | [x] | POST | `/api/xiaohongshu/create` | 用户端 | body: data | 空 body 400（XiaohongshuCardCreateRequest.ImageURL required，title 可选）；合法 → 200 id=34 short_code=H4boY7（redirect_url 空默认 https://www.xiaohongshu.com） | `src/api/xiaohongshuCard.js` | 12 |
| 1134 | [x] | PUT | `/api/xiaohongshu/update` | 用户端 | body: data | 200「更新成功」（全量替换，description 被清空）；99999 → 404「更新失败」 | `src/api/xiaohongshuCard.js` | 16 |
| 1135 | [x] | DELETE | `/api/xiaohongshu/delete/{id}` | 用户端 | path: id | 200「删除成功」；重复删 404 | `src/api/xiaohongshuCard.js` | 20 |
| 1136 | [x] | GET | `/api/xiaohongshu/view/{id}` | 用户端 | path: id | 200 详情；99999 → 404 | `src/api/xiaohongshuCard.js` | 24 |
| 1137 | [x] | GET | `/api/xiaohongshu/stats/card/{id}` | 用户端 | path: id / query: params | 200 {cardId,title,viewCount,dailyStats,recentActivity}（无数据时两者 null；带区间 → viewCount=1、dailyStats[{date,view:1}]、recentActivity[{id:1,action:view,ipAddress}]） | `src/api/xiaohongshuCard.js` | 28 |
| 1138 | [x] | GET | `/api/xiaohongshu/stats/overall` | 用户端 | query: params | 200 {totalCards:19,activeCards:19,totalViews:1,popularCards,dailyStats,recentActivity} | `src/api/xiaohongshuCard.js` | 32 |
> **缺陷 D-45（已修复）**：小红书 `generate-short-link` 原先用空壳 `&model.XiaohongshuCard{ID: uint(id)}` 调 service，redirect_url/title/domain_pool_id 全零值 → 被空 redirect 判断提前返回，接口 200 但从不建链。修复：service 新增 `GetCardModelByID`，controller 先取真实实体再生成并映射 400；实测清空 short_link_id 后生成 /s/Sb1Dy7，重复调用幂等同 code，99999 → 404。
| 1139 | [x] | POST | `/api/xiaohongshu/{id}/generate-short-link` | 用户端 | path: id | 无短链时 200 生成 /s/Sb1Dy7（D-45 修复后真实建链）；已绑定则幂等返回同 code；99999 → 404 | `src/api/xiaohongshuCard.js` | 36 |
