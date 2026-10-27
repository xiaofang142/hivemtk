# API 路由清单（SaaS 多租户 · 私域独立部署 双模式逐路由 curl 验证）

- 来源：`user-web/src/api/*.js`（120 个文件）
- 规模：**1077 条前端 API 出口**
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
| 1 | [x] | GET | `/api/ab-experiments` | 用户端 | query: params | 200 code=0 list=2 total=21/22 | `src/api/abExperiment.js` | 4 |
| 2 | [x] | GET | `/api/ab-experiments/{id}` | 用户端 | path: id | 200 code=0 详情返回 name | `src/api/abExperiment.js` | 7 |
| 3 | [x] | POST | `/api/ab-experiments` | 用户端 | body: data | 200 code=0 建 id=26/27 | `src/api/abExperiment.js` | 10 |
| 4 | [x] | PUT | `/api/ab-experiments/{id}` | 用户端 | path: id / body: data | 200 code=0（须全量 body，前端即全量） | `src/api/abExperiment.js` | 13 |
| 5 | [x] | DELETE | `/api/ab-experiments/{id}` | 用户端 | path: id | 200 code=0 删除成功 | `src/api/abExperiment.js` | 16 |
| 6 | [x] | POST | `/api/ab-experiments/{id}/start` | 用户端 | path: id | 200 code=0 data=null | `src/api/abExperiment.js` | 19 |
| 7 | [x] | POST | `/api/ab-experiments/{id}/pause` | 用户端 | path: id | 200 code=0 data=null | `src/api/abExperiment.js` | 22 |
| 8 | [x] | POST | `/api/ab-experiments/{id}/stop` | 用户端 | path: id | 200 code=0 data=null | `src/api/abExperiment.js` | 25 |
| 9 | [x] | GET | `/api/ab-experiments/{id}/results` | 用户端 | path: id | 200 code=0 array | `src/api/abExperiment.js` | 28 |
| 10 | [x] | GET | `/api/ab-experiments/{id}/conversion-events` | 用户端 | path: id | 200 code=0 n=4 | `src/api/abExperiment.js` | 31 |
| 11 | [x] | GET | `/api/ab-experiments` | 用户端 | - | 200 code=0（别名，同 #1） | `src/api/abExperiment.js` | 41 |
| 12 | [x] | GET | `/api/ab-experiments/{id}/stats` | 用户端 | path: id / query: method | 200 code=0 method=frequentist/bayesian | `src/api/abExperimentPlus.js` | 6 |
| 13 | [x] | GET | `/api/ab-experiments/{id}/diagnostics` | 用户端 | path: id | 200 code=0 object | `src/api/abExperimentPlus.js` | 9 |
| 14 | [x] | GET | `/api/ab-experiments/{id}/cuped` | 用户端 | path: id | 200 code=0 object | `src/api/abExperimentPlus.js` | 12 |
| 15 | [x] | POST | `/api/ab-experiments/{id}/sequential-test` | 用户端 | path: id / body: { alpha } | 200 code=0 | `src/api/abExperimentPlus.js` | 15 |
| 16 | [x] | POST | `/api/ab-experiments/{id}/bayesian-test` | 用户端 | path: id / body: {} | 200 code=0 | `src/api/abExperimentPlus.js` | 18 |
| 17 | [x] | POST | `/api/agent-kb-bindings` | 用户端 | body: data | 200 code=0 绑定成功 | `src/api/agentKBBinding.js` | 4 |
| 18 | [x] | DELETE | `/api/agent-kb-bindings/{agentId}/{kbId}` | 用户端 | path: agentId, kbId | 200 code=0 解绑成功 | `src/api/agentKBBinding.js` | 8 |
| 19 | [x] | GET | `/api/agent-kb-bindings/by-agent/{agentId}` | 用户端 | path: agentId | 200 code=0 n=1 | `src/api/agentKBBinding.js` | 12 |
| 20 | [x] | GET | `/api/agent-kb-bindings/by-kb/{kbId}` | 用户端 | path: kbId | 200 code=0 n=1 | `src/api/agentKBBinding.js` | 16 |
| 21 | [x] | PUT | `/api/agent-kb-bindings/by-agent/{agentId}` | 用户端 | path: agentId / body: { kb_ids: kbIds } | 200 code=0 count=1 | `src/api/agentKBBinding.js` | 20 |
| 22 | [x] | GET | `/api/ai-agents` | 用户端 | query: params | 200 code=0 n=19 | `src/api/aiAgent.js` | 4 |
| 23 | [x] | GET | `/api/ai-agents-enabled` | 用户端 | - | 200 code=0 n=2 | `src/api/aiAgent.js` | 8 |
| 24 | [x] | GET | `/api/ai-agents/{id}` | 用户端 | path: id | 200 code=0 agent_code=general_ecs | `src/api/aiAgent.js` | 12 |
| 25 | [x] | POST | `/api/ai-agents` | 用户端 | body: data | 200 code=0 建 id=77/78 | `src/api/aiAgent.js` | 16 |
| 26 | [x] | PUT | `/api/ai-agents/{id}` | 用户端 | path: id / body: data | 200 code=0 | `src/api/aiAgent.js` | 20 |
| 27 | [x] | DELETE | `/api/ai-agents/{id}` | 用户端 | path: id | 200 code=0 | `src/api/aiAgent.js` | 24 |
| 28 | [x] | POST | `/api/ai-agents/{id}/toggle` | 用户端 | path: id / body: { status } | 200 code=0 | `src/api/aiAgent.js` | 28 |
| 29 | [x] | POST | `/api/ai-agents/{id}/test` | 用户端 | path: id / body: data | 200 code=0（禁用态回 404，业务正确） | `src/api/aiAgent.js` | 32 |
| 30 | [x] | GET | `/api/ai-agents/{id}/context` | 用户端 | path: id | 200 code=0（禁用态回 404，业务正确） | `src/api/aiAgent.js` | 36 |
| 31 | [x] | GET | `/api/ai-productivity/overview` | 用户端 | query: params | 200 code=0 tc=198 ai=528（两形态一致） | `src/api/aiProductivity.js` | 5 |
| 32 | [x] | GET | `/api/ai-productivity/trend` | 用户端 | query: params | 200 code=0 {days,trend} | `src/api/aiProductivity.js` | 8 |
| 33 | [x] | GET | `/api/ai-tools` | 用户端 | query: params | 200 code=0 total=1 | `src/api/aiTool.js` | 4 |
| 34 | [x] | GET | `/api/ai-tools/{name}` | 用户端 | path: name | 200 code=0 | `src/api/aiTool.js` | 8 |
| 35 | [x] | PUT | `/api/ai-tools/{name}/status` | 用户端 | path: name / body: { is_enabled: enabled } | 200 code=0（缺 is_enabled 回 400） | `src/api/aiTool.js` | 12 |
| 36 | [x] | POST | `/api/ai-tools/batch-status` | 用户端 | body: { tools, is_enabled: enabled } | 200 code=0 | `src/api/aiTool.js` | 16 |
| 37 | [x] | GET | `/api/ai-tools/{toolName}/accounts` | 用户端 | path: toolName | 200 code=0 n=0 | `src/api/aiTool.js` | 20 |
| 38 | [x] | POST | `/api/ai-tools/{toolName}/accounts` | 用户端 | path: toolName / body: { account_type: accountType, account_id: accountId, is_primary: isPrimary } | 200 code=0 | `src/api/aiTool.js` | 24 |
| 39 | [x] | DELETE | `/api/ai-tools/{toolName}/accounts/{accountType}/{accountId}` | 用户端 | path: toolName, accountType, accountId | 200 code=0 | `src/api/aiTool.js` | 32 |
| 40 | [x] | GET | `/api/approvals/{id}` | 用户端 | path: id | 200 code=SERVICE_UNAVAILABLE_6003（FF_LTC_APPROVAL_RESUME=off，设计内） | `src/api/approval.js` | 18 |
| 41 | [x] | POST | `/api/approvals/{id}/decide` | 用户端 | path: id / body: { verdict, note } | 200 code=SERVICE_UNAVAILABLE_6003（同上） | `src/api/approval.js` | 27 |
| 42 | [x] | POST | `/api/asset-bundle` | 用户端 | body: data | 200 code=0 id=180/181（messages 须含 role=system） | `src/api/assetBundle.js` | 4 |
| 43 | [x] | PUT | `/api/asset-bundle/{id}` | 用户端 | path: id / body: data | 200 code=0 | `src/api/assetBundle.js` | 7 |
| 44 | [x] | GET | `/api/asset-bundle/{id}` | 用户端 | path: id | 200 code=0 | `src/api/assetBundle.js` | 10 |
| 45 | [x] | GET | `/api/asset-bundle/by-aid/{aid}` | 用户端 | path: aid | 200 code=0 | `src/api/assetBundle.js` | 13 |
| 46 | [x] | POST | `/api/asset-bundle/list` | 用户端 | body: data | 200 code=0 total=40/12（两形态租户口径） | `src/api/assetBundle.js` | 16 |
| 47 | [x] | POST | `/api/asset-bundle/{id}/publish` | 用户端 | path: id | 200 code=0 | `src/api/assetBundle.js` | 19 |
| 48 | [x] | POST | `/api/asset-bundle/{id}/submit-platform` | 用户端 | path: id | 200 code=FORBIDDEN_2002（PLATFORM_ENABLED=false，设计内） | `src/api/assetBundle.js` | 22 |
| 49 | [x] | POST | `/api/asset-bundle/{id}/archive` | 用户端 | path: id | 200 code=0 | `src/api/assetBundle.js` | 25 |
| 50 | [x] | DELETE | `/api/asset-bundle/{id}` | 用户端 | path: id | 200 code=0 | `src/api/assetBundle.js` | 28 |
| 51 | [x] | POST | `/api/asset-bundle/weave` | 用户端 | body: data | 200 code=0（须先 enable；未启用回 6002 业务正确） | `src/api/assetBundle.js` | 31 |
| 52 | [x] | POST | `/api/asset-bundle/merchant-save` | 用户端 | body: data | 200 code=0 | `src/api/assetBundle.js` | 34 |
| 53 | [x] | POST | `/api/asset-bundle/merchant-parse/{aid}` | 用户端 | path: aid | 200 code=0 | `src/api/assetBundle.js` | 37 |
| 54 | [x] | POST | `/api/asset-bundle/{id}/enable` | 用户端 | path: id | 200 code=0 | `src/api/assetBundle.js` | 40 |
| 55 | [x] | POST | `/api/asset-bundle/{id}/disable` | 用户端 | path: id | 200 code=0 | `src/api/assetBundle.js` | 43 |
| 56 | [x] | POST | `/api/asset-bundle/enabled/list` | 用户端 | - | 200 code=0 n=2 | `src/api/assetBundle.js` | 46 |
| 57 | [x] | POST | `/api/upload` | 用户端 | body: fd | 200 code=0；**修复前 on 模式无 tenant 前缀且无签名 ⇒ URL 404** | `src/api/assetBundle.js` | 51 |
| 58 | [x] | GET | `/api/v1/asset-market/list` | 用户端 | query: params | 200 code=0 | `src/api/assetMarket.js` | 4 |
| 59 | [x] | GET | `/api/v1/asset-market/detail/{id}` | 用户端 | path: id | 200 code=0 | `src/api/assetMarket.js` | 7 |
| 60 | [x] | POST | `/api/v1/asset-market/purchase` | 用户端 | body: data | 200 code=FORBIDDEN_2002（PLATFORM_ENABLED=false，设计内） | `src/api/assetMarket.js` | 10 |
| 61 | [x] | POST | `/api/v1/asset-market/sync` | 用户端 | body: data | 200 code=FORBIDDEN_2002（同上） | `src/api/assetMarket.js` | 13 |
| 62 | [x] | POST | `/api/v1/asset-market/report-usage` | 用户端 | body: data | 200 code=FORBIDDEN_2002（同上） | `src/api/assetMarket.js` | 16 |
| 63 | [x] | GET | `/api/v1/local-assets` | 用户端 | query: params | 200 code=0 total=9 | `src/api/assetMarket.js` | 19 |
| 64 | [x] | GET | `/api/v1/local-assets/{id}` | 用户端 | path: id | 200 code=0 {asset,data} | `src/api/assetMarket.js` | 22 |
| 65 | [x] | POST | `/api/v1/local-assets` | 用户端 | body: data | 200 code=0 id=17/18（data 须含 name+system_prompt） | `src/api/assetMarket.js` | 25 |
| 66 | [x] | PUT | `/api/v1/local-assets/{id}` | 用户端 | path: id / body: data | 200 code=0 | `src/api/assetMarket.js` | 28 |
| 67 | [x] | DELETE | `/api/v1/local-assets/{id}` | 用户端 | path: id | 200 code=0；**修复：不存在 id 由假成功改 6001** | `src/api/assetMarket.js` | 31 |
| 68 | [x] | PUT | `/api/v1/local-assets/{id}/toggle-active` | 用户端 | path: id / body: { active } | 200 code=0；**修复：不存在 id 由假成功改 6001** | `src/api/assetMarket.js` | 34 |
| 69 | [x] | GET | `/api/v1/local-assets/sync-log` | 用户端 | query: params | 200 code=0 | `src/api/assetMarket.js` | 37 |
| 70 | [x] | GET | `/api/backups` | 用户端 | query: params | 200 code=0 total=122 | `src/api/backup.js` | 4 |
| 71 | [x] | GET | `/api/backups/{id}` | 用户端 | path: id | 200 code=0 status=completed | `src/api/backup.js` | 8 |
| 72 | [x] | POST | `/api/backups` | 用户端 | body: data | 200 code=0 建 id=170/171 status=pending | `src/api/backup.js` | 12 |
| 73 | [x] | DELETE | `/api/backups/{id}` | 用户端 | path: id | 200 code=0 删除成功 | `src/api/backup.js` | 16 |
| 74 | [x] | POST | `/api/restore` | 用户端 | body: data | 200 code=NOT_FOUND_1002（不存在的 backup_id，未触发真实恢复） | `src/api/backup.js` | 20 |
| 75 | [x] | GET | `/api/restore/list` | 用户端 | query: params | 200 code=0 n=1 | `src/api/backup.js` | 24 |
| 76 | [x] | GET | `/api/restore/last` | 用户端 | - | 200 code=0 object | `src/api/backup.js` | 28 |
| 77 | [x] | GET | `/api/bad-cases` | 用户端 | query: params | 200 code=0 total=241（重复 status 键生效） | `src/api/badCase.js` | 44 |
| 78 | [x] | GET | `/api/bad-cases/stats` | 用户端 | body: undefined | 200 code=0 {by_status,by_fix_layer,labeled_ratio} | `src/api/badCase.js` | 50 |
| 79 | [x] | GET | `/api/bad-cases/taxonomy` | 用户端 | body: undefined | 200 code=0 值域表 | `src/api/badCase.js` | 57 |
| 80 | [x] | GET | `/api/bad-cases/{id}` | 用户端 | path: id | 200 code=0 status=pending | `src/api/badCase.js` | 61 |
| 81 | [x] | POST | `/api/bad-cases` | 用户端 | body: input | 200 code=0 建 bc_..._4 | `src/api/badCase.js` | 69 |
| 82 | [x] | POST | `/api/bad-cases/{id}/label` | 用户端 | path: id / body: { label, note } | 200 code=0 label=kb_missing；未知类目回 400 业务正确 | `src/api/badCase.js` | 76 |
| 83 | [x] | POST | `/api/bad-cases/{id}/dismiss` | 用户端 | path: id / body: { reason } | 200 code=0 | `src/api/badCase.js` | 82 |
| 84 | [x] | POST | `/api/bad-cases/export` | 用户端 | body: { labels, limit } | 200 code=0 {count,eval_set_id,rows} | `src/api/badCase.js` | 89 |
| 85 | [x] | GET | `/api/clues/list` | 用户端 | query: params | 200 code=0 total=221/223 | `src/api/batchOperation.js` | 83 |
| 86 | [x] | POST | `/api/batch/import` | 用户端 | body: fd | 200 code=0 导入完成 | `src/api/batchOperation.js` | 112 |
| 87 | [x] | GET | `/api/batch/template` | 用户端 | query: type | 200 附件 csv 196B | `src/api/batchOperation.js` | 118 |
| 88 | [x] | POST | `/api/batch/export` | 用户端 | body: form-urlencoded | 200 附件 csv 248B | `src/api/batchOperation.js` | 123 |
| 89 | [x] | POST | `/api/batch/delete` | 用户端 | body: { ids: toIdList(data?.ids) } | 200 code=0（0 行也回成功，见 A5 观察） | `src/api/batchOperation.js` | 131 |
| 90 | [x] | POST | `/api/batch/update` | 用户端 | body: { ids: toIdList(data?.ids), fields: toFieldMap(data?.fields) } | 200 code=0；未知字段回 400 业务正确 | `src/api/batchOperation.js` | 156 |
| 91 | [x] | GET | `/api/batch/tools` | 用户端 | - | 200 code=0 n=2 | `src/api/batchOperation.js` | 163 |
| 92 | [x] | GET | `/api/batch/histories` | 用户端 | query: params | 200 code=0 n=0 | `src/api/batchOperation.js` | 208 |
| 93 | [x] | POST | `/api/batch/histories/{id}/cancel` | 用户端 | path: id | 200 code=NOT_FOUND_1002（不存在 id） | `src/api/batchOperation.js` | 211 |
| 94 | [x] | GET | `/api/batch/histories/{id}` | 用户端 | path: id | 200 code=NOT_FOUND_1002（不存在 id） | `src/api/batchOperation.js` | 214 |
| 95 | [x] | POST | `/api/batch/preview` | 用户端 | body: { operation_type: tool, data_type: 'clue', ids } | 200 code=0 selected_count=1 | `src/api/batchOperation.js` | 223 |
| 96 | [x] | GET | `/api/bridge/extension/package` | 用户端 | body: undefined | 404 code=NOT_FOUND_1002（未接入平台/未上架，设计内） | `src/api/bridgeExtension.js` | 11 |
| 97 | [x] | GET | `/api/bridge/token/status` | 用户端 | - | 200 code=0 {main_configured,prev_configured,source} | `src/api/bridgeToken.js` | 3 |
| 98 | [x] | POST | `/api/bridge/token/reset` | 用户端 | - | 200 code=0 轮换成功 | `src/api/bridgeToken.js` | 5 |
| 99 | [x] | GET | `/api/browser-automation/tasks` | 用户端 | query: params | 200 code=0 total=501 | `src/api/browserAutomation.js` | 6 |
| 100 | [x] | GET | `/api/browser-automation/tasks/{id}` | 用户端 | path: id | 200 code=0；他人任务回 404（归属门，设计内） | `src/api/browserAutomation.js` | 9 |
| 101 | [x] | POST | `/api/browser-automation/tasks` | 用户端 | body: data | 200 code=0 id=586/587 | `src/api/browserAutomation.js` | 12 |
| 102 | [x] | PUT | `/api/browser-automation/tasks/{id}` | 用户端 | path: id / body: data | 200 code=0 | `src/api/browserAutomation.js` | 15 |
| 103 | [x] | DELETE | `/api/browser-automation/tasks/{id}` | 用户端 | path: id | 200 code=0 | `src/api/browserAutomation.js` | 18 |
| 104 | [x] | POST | `/api/browser-automation/tasks/{id}/publish` | 用户端 | path: id | 200 code=0 已发布 | `src/api/browserAutomation.js` | 21 |
| 105 | [x] | POST | `/api/browser-automation/tasks/{id}/run` | 用户端 | path: id / body: {} | 200 code=BROWSER_HOST_OFFLINE_8001（Host 未连接，环境前提） | `src/api/browserAutomation.js` | 27 |
| 106 | [x] | POST | `/api/browser-automation/tasks/{id}/pause` | 用户端 | path: id | 200 code=BROWSER_STATE_CONFLICT_8003（非执行中，业务正确） | `src/api/browserAutomation.js` | 30 |
| 107 | [x] | POST | `/api/browser-automation/tasks/{id}/resume` | 用户端 | path: id | 200 code=BROWSER_STATE_CONFLICT_8003（非暂停，业务正确） | `src/api/browserAutomation.js` | 33 |
| 108 | [x] | POST | `/api/browser-automation/tasks/{id}/archive` | 用户端 | path: id | 200 code=0 已归档 | `src/api/browserAutomation.js` | 36 |
| 109 | [x] | PUT | `/api/browser-automation/tasks/{id}/dependency` | 用户端 | path: id / body: data | 200 code=0；前置不存在回 400（业务正确） | `src/api/browserAutomation.js` | 39 |
| 110 | [x] | GET | `/api/browser-automation/sessions` | 用户端 | query: params | 200 code=0 total=579 | `src/api/browserAutomation.js` | 44 |
| 111 | [x] | GET | `/api/browser-automation/sessions/{id}` | 用户端 | path: id | 200 code=0 status=completed | `src/api/browserAutomation.js` | 47 |
| 112 | [x] | GET | `/api/browser-automation/sessions/{id}/steps` | 用户端 | path: id | 200 code=0 n=2 | `src/api/browserAutomation.js` | 50 |
| 113 | [x] | GET | `/api/browser-automation/tasks/{taskId}/sessions` | 用户端 | path: taskId / query: params | 200 code=0 n=1 | `src/api/browserAutomation.js` | 54 |
| 114 | [x] | POST | `/api/browser-automation/sessions/{id}/stop` | 用户端 | path: id / body: { reason: reason \|\| '' } | 200 code=0「会话不在运行中」（业务正确） | `src/api/browserAutomation.js` | 57 |
| 115 | [x] | GET | `/api/browser-automation/sessions/{id}/confirm-gate` | 用户端 | path: id | 200 code=0 {pending} | `src/api/browserAutomation.js` | 62 |
| 116 | [x] | POST | `/api/browser-automation/sessions/{id}/confirm` | 用户端 | path: id / body: { payload_hash: hash } | 200 code=0 status=no_gate（无待确认点） | `src/api/browserAutomation.js` | 72 |
| 117 | [x] | GET | `/api/browser-automation/sessions/{id}/logs` | 用户端 | path: id / body: direction ? { direction } : undefined | 200 code=0 n=2 | `src/api/browserAutomation.js` | 93 |
| 118 | [x] | GET | `/api/browser-automation/sessions/{id}/export` | 用户端 | path: id | 200 JSON 7.4KB | `src/api/browserAutomation.js` | 97 |
| 119 | [x] | GET | `/api/browser-automation/tasks/{taskId}/receipts` | 用户端 | path: taskId | 200 code=0 | `src/api/browserAutomation.js` | 102 |
| 120 | [x] | GET | `/api/browser-automation/cron` | 用户端 | - | 200 code=0 n=2 | `src/api/browserAutomation.js` | 107 |
| 121 | [x] | POST | `/api/browser-automation/cron` | 用户端 | body: data | 200 code=0 id=60/61（任务须 task_type=cron） | `src/api/browserAutomation.js` | 110 |
| 122 | [x] | PUT | `/api/browser-automation/cron/{id}` | 用户端 | path: id / body: data | 200 code=0 | `src/api/browserAutomation.js` | 113 |
| 123 | [x] | DELETE | `/api/browser-automation/cron/{id}` | 用户端 | path: id | 200 code=0 | `src/api/browserAutomation.js` | 116 |
| 124 | [x] | POST | `/api/browser-automation/cron/{id}/enable` | 用户端 | path: id | 200 code=0 已启用 | `src/api/browserAutomation.js` | 119 |
| 125 | [x] | POST | `/api/browser-automation/cron/{id}/disable` | 用户端 | path: id | 200 code=0 已停用 | `src/api/browserAutomation.js` | 122 |
| 126 | [x] | GET | `/api/browser-automation/host/status` | 用户端 | - | 200 code=0 {hosts,online,servable} | `src/api/browserAutomation.js` | 127 |
| 127 | [x] | POST | `/api/browser-automation/host/token/reset` | 用户端 | - | 200 code=0 已重置 | `src/api/browserAutomation.js` | 130 |
| 128 | [x] | GET | `/api/browser-automation/platforms` | 用户端 | - | 200 code=0 n=3 | `src/api/browserAutomation.js` | 135 |
| 129 | [x] | GET | `/api/browser-automation/platforms/{id}/locators` | 用户端 | path: id | 200 code=0 object | `src/api/browserAutomation.js` | 138 |
| 130 | [x] | GET | `/api/whatsapp/templates` | 用户端 | query: params | 200 code=0 n=0 | `src/api/bulkMessaging.js` | 4 |
| 131 | [x] | POST | `/api/whatsapp/templates` | 用户端 | body: data | 200 code=0 id=tmpl_... | `src/api/bulkMessaging.js` | 8 |
| 132 | [x] | PUT | `/api/whatsapp/templates/{id}` | 用户端 | path: id / body: data | 200 code=0 | `src/api/bulkMessaging.js` | 12 |
| 133 | [x] | DELETE | `/api/whatsapp/templates/{id}` | 用户端 | path: id | 200 code=0 | `src/api/bulkMessaging.js` | 16 |
| 134 | [x] | POST | `/api/whatsapp/group-messaging/send` | 用户端 | body: data | 400 code=INVALID_PARAM_1001（缺 lead_ids/template_id，未真实外发） | `src/api/bulkMessaging.js` | 20 |
| 135 | [x] | GET | `/api/whatsapp/group-messaging/status/{queueId}` | 用户端 | path: queueId | 200 code=0（未知队列返回空状态） | `src/api/bulkMessaging.js` | 24 |
| 136 | [x] | GET | `/api/whatsapp/group-messaging/records` | 用户端 | query: params | 200 code=0 n=0 | `src/api/bulkMessaging.js` | 28 |
| 137 | [x] | GET | `/api/channel-agent-bindings` | 用户端 | query: params | 200 code=0；缺 channel_type/account_id 回 400（后端契约） | `src/api/channelAgentBinding.js` | 4 |
| 138 | [x] | GET | `/api/channel-agent-bindings/by-agent/{agentId}` | 用户端 | path: agentId | 200 code=0 n=2 | `src/api/channelAgentBinding.js` | 8 |
| 139 | [x] | POST | `/api/channel-agent-bindings` | 用户端 | body: data | 200 code=0 id=39/40 | `src/api/channelAgentBinding.js` | 12 |
| 140 | [x] | PUT | `/api/channel-agent-bindings/{id}` | 用户端 | path: id / body: data | 200 code=0（body 须带 channel_type/account_id/agent_id） | `src/api/channelAgentBinding.js` | 16 |
| 141 | [x] | DELETE | `/api/channel-agent-bindings/{id}` | 用户端 | path: id | 200 code=0 | `src/api/channelAgentBinding.js` | 20 |
| 142 | [x] | GET | `/api/chat/public/upload-token` | 公开 | query: params | 400/403 校验链正确；成功分支需 qiniu 存储（本机未配，属环境） | `src/api/chat.js` | 4 |
| 143 | [x] | GET | `/api/chat-channels` | 用户端 | query: params | 200 code=0 n=3 | `src/api/chatChannel.js` | 4 |
| 144 | [x] | GET | `/api/chat-channels/{channelId}` | 用户端 | path: channelId | 200 code=0 | `src/api/chatChannel.js` | 8 |
| 145 | [x] | POST | `/api/chat-channels` | 用户端 | body: data | 200 code=0 建 id=42/43（响应急 data.channel） | `src/api/chatChannel.js` | 12 |
| 146 | [x] | PUT | `/api/chat-channels/{channelId}` | 用户端 | path: channelId / body: data | 200 code=0 | `src/api/chatChannel.js` | 16 |
| 147 | [x] | DELETE | `/api/chat-channels/{channelId}` | 用户端 | path: channelId | 200 code=0 已禁用 | `src/api/chatChannel.js` | 20 |
| 148 | [x] | POST | `/api/chat-channels/{channelId}/rotate-key` | 用户端 | path: channelId | 200 code=0 | `src/api/chatChannel.js` | 24 |
| 149 | [x] | POST | `/api/chat-channels/{channelId}/reset-secret` | 用户端 | path: channelId | 200 code=0 | `src/api/chatChannel.js` | 28 |
| 150 | [x] | GET | `/api/churn/prediction` | 用户端 | query: params | 200 code=0（须 user_id；缺失回 400 契约正确） | `src/api/churnPrediction.js` | 4 |
| 151 | [x] | GET | `/api/churn-prediction` | 用户端 | query: params | 200 code=0 object | `src/api/churnPrediction.js` | 7 |
| 152 | [x] | GET | `/api/churn-prediction/users` | 用户端 | query: params | 200 code=0 array | `src/api/churnPrediction.js` | 10 |
| 153 | [x] | GET | `/api/churn-prediction/warnings` | 用户端 | query: params | 200 code=0 object | `src/api/churnPrediction.js` | 13 |
| 154 | [x] | GET | `/api/churn/unhandled-warnings` | 用户端 | query: params | 200 code=0 object | `src/api/churnPrediction.js` | 16 |
| 155 | [x] | POST | `/api/churn/warnings/{id}/handle` | 用户端 | path: id / body: data | 200 code=0 标记成功（测试后已还原 is_handled） | `src/api/churnPrediction.js` | 19 |
| 156 | [x] | GET | `/api/churn-prediction/model-config` | 用户端 | - | 200 code=0 object | `src/api/churnPrediction.js` | 22 |
| 157 | [x] | POST | `/api/churn-prediction/model-config` | 用户端 | body: data | 200 code=0 保存成功 | `src/api/churnPrediction.js` | 25 |
| 158 | [x] | GET | `/api/churn-prediction/statistics` | 用户端 | query: params | 200 code=0（须 start_date/end_date） | `src/api/churnPrediction.js` | 28 |
| 159 | [x] | GET | `/api/churn/risk-distribution` | 用户端 | - | 200 code=0 object | `src/api/churnPrediction.js` | 31 |
| 160 | [x] | POST | `/api/churn/warnings/intervene` | 用户端 | body: data | 200 code=0 干预成功（warning_id+intervention_type） | `src/api/churnPrediction.js` | 38 |
| 161 | [x] | POST | `/api/user-segment/rfm/calculate` | 用户端 | body: data | 200 code=0 计算完成 | `src/api/churnPrediction.js` | 50 |
| 162 | [x] | DELETE | `/api/clues/delete/{id}` | 用户端 | path: id | 200 code=0（不存在 id 也回成功，见 A6 观察） | `src/api/clue.js` | 5 |
| 163 | [x] | GET | `/api/clues/list?{q}` | 用户端 | path: q | 200 code=0 total=221 | `src/api/clue.js` | 12 |
| 164 | [x] | GET | `/api/clues/statistics` | 用户端 | - | 200 code=0 array(7) | `src/api/clue.js` | 15 |
| 165 | [x] | POST | `/api/clues/import` | 用户端 | body: data | 200 code=0 导入线索成功 | `src/api/clue.js` | 18 |
| 166 | [x] | GET | `/api/system/stats` | 用户端 | - | 200 code=0 | `src/api/cockpit.js` | 4 |
| 167 | [x] | GET | `/api/monitor/health` | 用户端 | - | 200 code=0 object | `src/api/cockpit.js` | 8 |
| 168 | [x] | GET | `/api/monitor/node-health` | 用户端 | - | 200 code=0 object | `src/api/cockpit.js` | 12 |
| 169 | [x] | GET | `/api/monitor/anomalies` | 用户端 | body: { limit } | 200 code=0 object | `src/api/cockpit.js` | 16 |
| 170 | [x] | GET | `/api/ai/sales-cockpit` | 用户端 | - | 200 code=0 8 个聚合块 | `src/api/cockpit.js` | 24 |
| 171 | [x] | POST | `/api/customer-sessions/{sessionId}/messages` | 用户端 | path: sessionId / body: data | 200 code=INVALID_PARAM_1001（closed 会话禁发，业务正确；数字 id 会 404，须传 session_id 串） | `src/api/collaboration.js` | 4 |
| 172 | [x] | POST | `/api/customer-sessions/{sessionId}/internal-notes` | 用户端 | path: sessionId / body: data | 200 code=0 内部备注已保存 | `src/api/collaboration.js` | 7 |
| 173 | [x] | GET | `/api/users/search` | 用户端 | query: params | 200 code=0 n=4 | `src/api/collaboration.js` | 10 |
| 174 | [x] | POST | `/api/mentions/{mentionId}/read` | 用户端 | path: mentionId / body: {} | 400 code=INVALID_PARAM_1001（id 非数字，契约正确） | `src/api/collaboration.js` | 13 |
| 175 | [x] | GET | `/api/mentions/mine` | 用户端 | query: params | 200 code=0 object | `src/api/collaboration.js` | 16 |
| 176 | [x] | POST | `/api/customer-sessions/{sessionId}/edit-lock` | 用户端 | path: sessionId / body: { holder: 'me' } | 200 code=0 holder=me | `src/api/collaboration.js` | 19 |
| 177 | [x] | DELETE | `/api/customer-sessions/{sessionId}/edit-lock` | 用户端 | path: sessionId | 200 code=0 已释放（holder 空可释放他人锁，见 A7） | `src/api/collaboration.js` | 22 |
| 178 | [x] | GET | `/api/customer-sessions/{sessionId}/edit-lock` | 用户端 | path: sessionId | 200 code=0 {locked:true,lock.holder=me} | `src/api/collaboration.js` | 25 |
| 179 | [x] | GET | `/api/customer-sessions/{sessionId}/internal-notes` | 用户端 | path: sessionId / query: params | 200 code=0 | `src/api/collaboration.js` | 28 |
| 180 | [x] | GET | `/api/community/groups` | 用户端 | query: params | 200 code=0 total=21 | `src/api/community.js` | 5 |
| 181 | [x] | POST | `/api/community/groups` | 用户端 | body: data | 200 code=0 创建成功 | `src/api/community.js` | 9 |
| 182 | [x] | GET | `/api/community/groups/{id}` | 用户端 | path: id | 200 code=0 | `src/api/community.js` | 13 |
| 183 | [x] | PUT | `/api/community/groups/{id}` | 用户端 | path: id / body: data | 200 code=0 | `src/api/community.js` | 17 |
| 184 | [x] | DELETE | `/api/community/groups/{id}` | 用户端 | path: id | 200 code=0 | `src/api/community.js` | 21 |
| 185 | [x] | GET | `/api/community/members` | 用户端 | query: params | 200 code=0（须 group_id） | `src/api/community.js` | 25 |
| 186 | [x] | GET | `/api/community/messages` | 用户端 | query: params | 200 code=0（须 group_id） | `src/api/community.js` | 29 |
| 187 | [x] | GET | `/api/community/stats` | 用户端 | query: params | 200 code=0 5 个读数 | `src/api/community.js` | 33 |
| 188 | [x] | POST | `/api/community/import` | 用户端 | body: data | 200 code=0 {success_count,total}（groups 必填） | `src/api/community.js` | 37 |
| 189 | [x] | POST | `/api/community/export` | 用户端 | body: data | 200 code=0 object | `src/api/community.js` | 41 |
| 190 | [x] | GET | `/api/manage/config-params` | 管理端 | query: group, include_deprecated | 200 code=0 n=21 | `src/api/configParams.js` | 8 |
| 191 | [x] | GET | `/api/manage/config-params/groups` | 管理端 | - | 200 code=0 n=39 | `src/api/configParams.js` | 17 |
| 192 | [x] | GET | `/api/manage/config-params/audit-logs` | 管理端 | query: limit | 200 code=0 n=3/1（两形态写入量不同） | `src/api/configParams.js` | 21 |
| 193 | [x] | PUT | `/api/manage/config-params/{group}/{key}` | 管理端 | path: group, key / body: { value } | 200 code=0；SaaS 下平台级参数回 400 明确拒绝（见 A9） | `src/api/configParams.js` | 25 |
| 194 | [x] | POST | `/api/manage/config-params/{group}/{key}/reset` | 管理端 | path: group, key | 200 code=0 | `src/api/configParams.js` | 29 |
| 195 | [x] | POST | `/api/manage/config-params/{group}/reset` | 管理端 | path: group | 200 code=0 | `src/api/configParams.js` | 33 |
| 196 | [x] | PUT | `/api/manage/platform-host` | 管理端 | body: { address } | 200 code=0 已更新（约 60s 生效） | `src/api/configParams.js` | 47 |
| 197 | [x] | POST | `/api/manage/platform-host/connectivity` | 管理端 | body: { address: address \|\| '' } | 200 平台未启用文案（设计内） | `src/api/configParams.js` | 56 |
| 198 | [x] | GET | `/api/conversion-funnels` | 用户端 | query: params | 200 code=0 五段 126/221/133/311/0；**修复前 SaaS 五段全 0 + degraded** | `src/api/conversionFunnel.js` | 5 |
| 199 | [x] | GET | `/api/conversion-funnels/stage` | 用户端 | query: stage | 200 code=0（修复后 count 正常） | `src/api/conversionFunnel.js` | 8 |
| 200 | [x] | POST | `/api/customer-sessions/{sessionId}/csat` | 用户端 | path: sessionId / body: data | 200 code=0 评分已提交 | `src/api/csat.js` | 4 |
| 201 | [x] | GET | `/api/csat/stats` | 用户端 | query: params | 200 code=0 object | `src/api/csat.js` | 7 |
| 202 | [x] | GET | `/api/csat/trend` | 用户端 | query: params | 200 code=0 object | `src/api/csat.js` | 10 |
| 203 | [x] | GET | `/api/csat/negative` | 用户端 | query: params | 200 code=0 object | `src/api/csat.js` | 13 |
| 204 | [x] | POST | `/api/customer-sessions/{sessionId}/csat/trigger` | 用户端 | path: sessionId / body: {} | 200 code=0 调查已触发 | `src/api/csat.js` | 16 |
| 205 | [x] | GET | `/api/csat/template` | 用户端 | - | 200 code=0 object | `src/api/csat.js` | 18 |
| 206 | [x] | PUT | `/api/csat/template` | 用户端 | body: data | 200 code=0 模板已保存 | `src/api/csat.js` | 19 |
| 207 | [x] | GET | `/api/custom-tools` | 用户端 | - | 200 code=0 n=0 | `src/api/customIntegration.js` | 6 |
| 208 | [x] | POST | `/api/custom-tools/import` | 用户端 | body: payload | 200 code=0 tool_count=1（OpenAPI schema 合法） | `src/api/customIntegration.js` | 10 |
| 209 | [x] | DELETE | `/api/custom-tools/{id}` | 用户端 | path: id | 200 code=0 删除成功 | `src/api/customIntegration.js` | 14 |
| 210 | [x] | GET | `/api/mcp-servers` | 用户端 | - | 200 code=0 n=0 | `src/api/customIntegration.js` | 18 |
| 211 | [x] | POST | `/api/mcp-servers` | 用户端 | body: payload | 200 code=0 已保存（endpoint 不可达仍落库，test 时暴露） | `src/api/customIntegration.js` | 22 |
| 212 | [x] | DELETE | `/api/mcp-servers/{id}` | 用户端 | path: id | 200 code=0 已删除 | `src/api/customIntegration.js` | 26 |
| 213 | [x] | PUT | `/api/mcp-servers/{id}/enabled` | 用户端 | path: id / body: { enabled } | 200 code=0 已更新 | `src/api/customIntegration.js` | 30 |
| 214 | [x] | GET | `/api/mcp-servers/{id}/test` | 用户端 | path: id | 200 code=UNKNOWN_1000（上游 405，探测失败原话透出，正确） | `src/api/customIntegration.js` | 34 |
| 215 | [x] | GET | `/api/custom-reports` | 用户端 | query: params | 200 code=0 list/total | `src/api/customReport.js` | 4 |
| 216 | [x] | GET | `/api/custom-reports/{id}` | 用户端 | path: id | 200 code=0 | `src/api/customReport.js` | 7 |
| 217 | [x] | POST | `/api/custom-reports` | 用户端 | body: data | 200 code=0 id=4/5（data_source 须为 sessions/messages/...） | `src/api/customReport.js` | 10 |
| 218 | [x] | PUT | `/api/custom-reports/{id}` | 用户端 | path: id / body: data | 200 code=0；**修复：非法图表类型由 500 改 400** | `src/api/customReport.js` | 13 |
| 219 | [x] | DELETE | `/api/custom-reports/{id}` | 用户端 | path: id | 200 code=0 | `src/api/customReport.js` | 16 |
| 220 | [x] | GET | `/api/custom-reports/templates` | 用户端 | - | 200 code=0 n=0 | `src/api/customReport.js` | 19 |
| 221 | [x] | POST | `/api/custom-reports/templates/{id}/use` | 用户端 | path: id | 200 code=NOT_FOUND_1002（无模板，业务正确） | `src/api/customReport.js` | 22 |
| 222 | [x] | GET | `/api/custom-reports/{id}/data` | 用户端 | path: id / query: params | 200 code=0 3 行；**修复前 SaaS 因 gin ctx 丢租户 500** | `src/api/customReport.js` | 25 |
| 223 | [x] | GET | `/api/custom-reports/{id}/data?format=export` | 用户端 | path: id | 200 code=0（同 #222 数据形状） | `src/api/customReport.js` | 41 |
| 224 | [x] | GET | `/api/custom-reports/{id}/data?format=run` | 用户端 | path: id | 200 code=0（同 #222 数据形状） | `src/api/customReport.js` | 44 |
| 225 | [x] | GET | `/api/customer-360/basic?user_id={id}` | 用户端 | path: id / body: undefined; { signal } | 200 code=0 object | `src/api/customer360.js` | 4 |
| 226 | [x] | GET | `/api/customer-360/events` | 用户端 | query: params / body: { signal } | 200 code=0 object | `src/api/customer360.js` | 7 |
| 227 | [x] | GET | `/api/customer-360/sessions` | 用户端 | query: params / body: { signal } | 200 code=0 object | `src/api/customer360.js` | 10 |
| 228 | [x] | GET | `/api/customer-360/tags?user_id={id}` | 用户端 | path: id / body: undefined; { signal } | 200 code=0 object | `src/api/customer360.js` | 13 |
| 229 | [x] | GET | `/api/customer-360/orders` | 用户端 | query: params / body: { signal } | 200 code=0（须真实 one_id；无档案回 404 业务正确） | `src/api/customer360.js` | 17 |
| 230 | [x] | GET | `/api/customer-360/list` | 用户端 | query: params | 200 code=0 total=318 | `src/api/customer360.js` | 43 |
| 231 | [x] | GET | `/api/customer-360/basic?user_id={id}` | 用户端 | path: id | 200 code=0（同 #225） | `src/api/customer360.js` | 46 |
| 232 | [x] | GET | `/api/customer-360/tags?user_id={id}` | 用户端 | path: id | 200 code=0（同 #228） | `src/api/customer360.js` | 64 |
| 233 | [x] | PUT | `/api/customer-360/tags?user_id={id}` | 用户端 | path: id / body: { tags } | 200 code=0 更新成功 | `src/api/customer360.js` | 73 |
| 234 | [x] | PUT | `/api/customer-360/tags?user_id={id}` | 用户端 | path: id / body: { tags } | 200 code=0（同一 PUT，删除即传删后数组） | `src/api/customer360.js` | 78 |
| 235 | [x] | POST | `/api/events/track` | 用户端 | body: data | 200 code=0（customer_id+event_type 必填） | `src/api/customerEvent.js` | 4 |
| 236 | [x] | POST | `/api/events/pageview` | 用户端 | body: data | 200 code=0（url 必填） | `src/api/customerEvent.js` | 7 |
| 237 | [x] | POST | `/api/events/click` | 用户端 | body: data | 200 code=0 | `src/api/customerEvent.js` | 10 |
| 238 | [x] | POST | `/api/events/purchase` | 用户端 | body: data | 200 code=0（amount 必填） | `src/api/customerEvent.js` | 13 |
| 239 | [x] | POST | `/api/events/signup` | 用户端 | body: data | 200 code=0 | `src/api/customerEvent.js` | 16 |
| 240 | [x] | POST | `/api/events/login` | 用户端 | body: data | 200 code=0 | `src/api/customerEvent.js` | 19 |
| 241 | [x] | POST | `/api/events/add-to-cart` | 用户端 | body: data | 200 code=0（price 必填） | `src/api/customerEvent.js` | 22 |
| 242 | [x] | GET | `/api/events/customer/{customerId}` | 用户端 | path: customerId / query: params | 200 code=0 array | `src/api/customerEvent.js` | 25 |
| 243 | [x] | GET | `/api/events/stats` | 用户端 | query: params | 200 code=0 object | `src/api/customerEvent.js` | 28 |
| 244 | [x] | DELETE | `/api/events/customer/{id}` | 用户端 | path: id | 200 code=0 删除成功 | `src/api/customerEvent.js` | 40 |
| 245 | [x] | GET | `/api/customer-events/list` | 用户端 | query: params | 200 code=0 n=2 | `src/api/customerEvent.js` | 44 |
| 246 | [x] | GET | `/api/customer-journey/overview` | 用户端 | - | 200 code=0 object | `src/api/customerJourney.js` | 4 |
| 247 | [x] | GET | `/api/customer-journey/overview?customer_id={customerId}` | 用户端 | path: customerId | 200 code=0 object | `src/api/customerJourney.js` | 8 |
| 248 | [x] | GET | `/api/customer-journey/stages` | 用户端 | - | 200 code=0 array（stage 为英文字面量） | `src/api/customerJourney.js` | 12 |
| 249 | [x] | GET | `/api/customer-journey/by-stage?stage={stage}` | 用户端 | path: stage | 200 code=0 object | `src/api/customerJourney.js` | 16 |
| 250 | [x] | POST | `/api/customer-journey/transition` | 用户端 | body: data | 200 code=0 迁移成功（to_stage 取 /stages 的 stage 值） | `src/api/customerJourney.js` | 20 |
| 251 | [x] | POST | `/api/customer-journey/touch` | 用户端 | body: data | 200 code=0 互动已记录 | `src/api/customerJourney.js` | 24 |
| 252 | [x] | POST | `/api/agents` | 用户端 | body: data | 200 code=0 创建成功 id=15 | `src/api/customerService.js` | 4 |
| 253 | [x] | GET | `/api/agents/{id}` | 用户端 | path: id | 200 code=0（:id 是 agent_id=用户 id，不是行主键） | `src/api/customerService.js` | 7 |
| 254 | [x] | GET | `/api/agents/online` | 用户端 | - | 200 code=0 n=0 | `src/api/customerService.js` | 10 |
| 255 | [x] | GET | `/api/agents/all` | 用户端 | - | 200 code=0 n=11 | `src/api/customerService.js` | 13 |
| 256 | [x] | PUT | `/api/agents/{id}/status` | 用户端 | path: id / body: data | 200 code=0；离线→离线回 400「只能切到在线」（业务正确） | `src/api/customerService.js` | 16 |
| 257 | [x] | POST | `/api/agents/{id}/online` | 用户端 | path: id | 200 code=0 上线成功（测试后已置回 offline） | `src/api/customerService.js` | 19 |
| 258 | [x] | POST | `/api/agents/{id}/offline` | 用户端 | path: id | 200 code=0 下线成功 | `src/api/customerService.js` | 22 |
| 259 | [x] | GET | `/api/agents/{id}/sessions` | 用户端 | path: id | 200 code=0 array | `src/api/customerService.js` | 25 |
| 260 | [x] | GET | `/api/agents/me` | 用户端 | - | 200 code=0 agent_name=uit_admin | `src/api/customerService.js` | 28 |
| 261 | [x] | GET | `/api/quick-replies` | 用户端 | query: params | 200 code=0（data 直接是数组） | `src/api/customerService.js` | 32 |
| 262 | [x] | GET | `/api/quick-replies/categories` | 用户端 | - | 200 code=0 array | `src/api/customerService.js` | 35 |
| 263 | [x] | POST | `/api/quick-replies` | 用户端 | body: data | 200 code=0 id=84/85 | `src/api/customerService.js` | 38 |
| 264 | [x] | PUT | `/api/quick-replies/{id}` | 用户端 | path: id / body: data | 200 code=0 | `src/api/customerService.js` | 41 |
| 265 | [x] | DELETE | `/api/quick-replies/{id}` | 用户端 | path: id | 200 code=0 | `src/api/customerService.js` | 44 |
| 266 | [x] | GET | `/api/session-tags` | 用户端 | - | 200 code=0 n=27 | `src/api/customerService.js` | 48 |
| 267 | [x] | POST | `/api/session-tags` | 用户端 | body: data | 200 code=0（name+code 必填） | `src/api/customerService.js` | 51 |
| 268 | [x] | PUT | `/api/session-tags/{id}` | 用户端 | path: id / body: data | 200 code=0 | `src/api/customerService.js` | 54 |
| 269 | [x] | DELETE | `/api/session-tags/{id}` | 用户端 | path: id | 200 code=0 | `src/api/customerService.js` | 57 |
| 270 | [x] | GET | `/api/ai-suggestions/{sessionId}` | 用户端 | path: sessionId | 200 code=0 array | `src/api/customerService.js` | 61 |
| 271 | [x] | POST | `/api/ai-suggestions/{id}/use` | 用户端 | path: id | 200 code=0 使用成功 | `src/api/customerService.js` | 64 |
| 272 | [x] | POST | `/api/customer-sessions/{sessionId}/tags` | 用户端 | path: sessionId / body: data | 200 code=0 标记成功（数字 id；非本人会话按坐席隔离拒绝） | `src/api/customerService.js` | 68 |
| 273 | [x] | GET | `/api/customer-service-agents` | 用户端 | query: params | 200 code=0（agent_status_id 必填） | `src/api/customerServiceAgent.js` | 4 |
| 274 | [x] | GET | `/api/customer-service-agents/by-ai-agent/{aiAgentId}` | 用户端 | path: aiAgentId | 200 code=0 | `src/api/customerServiceAgent.js` | 8 |
| 275 | [x] | POST | `/api/customer-service-agents` | 用户端 | body: data | 200 code=0；**修复：软删后重建不再撞唯一键** | `src/api/customerServiceAgent.js` | 12 |
| 276 | [x] | PUT | `/api/customer-service-agents/{id}` | 用户端 | path: id / body: data | 200 code=0 | `src/api/customerServiceAgent.js` | 16 |
| 277 | [x] | DELETE | `/api/customer-service-agents/{id}` | 用户端 | path: id | 200 code=0 | `src/api/customerServiceAgent.js` | 20 |
| 278 | [x] | GET | `/api/customer-service-agents/by-user/{userId}` | 用户端 | path: userId | 200 code=0 | `src/api/customerServiceAgent.js` | 24 |
| 279 | [x] | POST | `/api/customer-service-agents/by-user/{userId}` | 用户端 | path: userId / body: data | 200 code=0 创建成功 | `src/api/customerServiceAgent.js` | 28 |
| 280 | [x] | GET | `/api/customer-sessions` | 用户端 | query: params | 200 code=0 total=318 | `src/api/customerSession.js` | 4 |
| 281 | [x] | GET | `/api/customer-sessions/{id}/messages` | 用户端 | path: id / query: params | 200 code=0（新会话 total=0，发送后可见） | `src/api/customerSession.js` | 7 |
| 282 | [x] | POST | `/api/customer-sessions/{data_sessionId}/messages` | 用户端 | path: data_sessionId / body: { content: data.content, sender_type: data.sender_type \|\| 'agent', sender_name: data.sender_name \|\| '客服', sender_id: String(data.sender_id \|\| '') } | 200 code=INVALID_PARAM_1001（closed 会话禁发，业务正确） | `src/api/customerSession.js` | 10 |
| 283 | [x] | POST | `/api/customer-sessions` | 用户端 | body: data | 200 code=0 id=751/752 | `src/api/customerSession.js` | 18 |
| 284 | [x] | POST | `/api/customer-sessions/{id}/close` | 用户端 | path: id | 200 code=0 关闭成功 | `src/api/customerSession.js` | 21 |
| 285 | [x] | POST | `/api/customer-sessions/{id}/transfer` | 用户端 | path: id / body: data | 200 code=0 转接成功（字段 new_agent_id） | `src/api/customerSession.js` | 24 |
| 286 | [x] | POST | `/api/customer-sessions/{id}/takeover` | 用户端 | path: id / body: { reason } | 200 code=0；坐席离线时 400（业务正确） | `src/api/customerSession.js` | 28 |
| 287 | [x] | POST | `/api/customer-sessions/{id}/release` | 用户端 | path: id | 200 code=0；非本人会话 400（坐席隔离，业务正确） | `src/api/customerSession.js` | 32 |
| 288 | [x] | POST | `/api/customer-sessions/{id}/switch-handler` | 用户端 | path: id / body: { handler_type: handlerType, reason } | 200 code=0 切换成功 | `src/api/customerSession.js` | 36 |
| 289 | [x] | POST | `/api/customer-sessions/{id}/blacklist` | 用户端 | path: id / body: { reason, ttl_hours: ttlHours } | 200 code=0 拉黑成功 | `src/api/customerSession.js` | 40 |
| 290 | [x] | POST | `/api/customer-sessions/blacklist/remove` | 用户端 | body: { user_id: userId, platform } | 200 code=0 解除拉黑成功 | `src/api/customerSession.js` | 44 |
| 291 | [x] | GET | `/api/customer-sessions/blacklist/check` | 用户端 | query: user_id, platform | 200 code=0 blacklisted=true | `src/api/customerSession.js` | 48 |
| 292 | [x] | GET | `/api/customer-sessions/blacklist` | 用户端 | query: params | 200 code=0 total=2 | `src/api/customerSession.js` | 52 |
| 293 | [x] | GET | `/api/customer-360?user_id={id}` | 用户端 | path: id | 200 code=0 object | `src/api/customerSession.js` | 56 |
| 294 | [x] | GET | `/api/customer-360/basic?user_id={id}` | 用户端 | path: id | 200 code=0（同 #225） | `src/api/customerSession.js` | 59 |
| 295 | [x] | GET | `/api/customer-360/stats?user_id={id}` | 用户端 | path: id | 200 code=0 object | `src/api/customerSession.js` | 62 |
| 296 | [x] | GET | `/api/customer-360/sessions?user_id={id}` | 用户端 | path: id | 200 code=0（同 #227） | `src/api/customerSession.js` | 65 |
| 297 | [x] | GET | `/api/customer-360/tags?user_id={id}` | 用户端 | path: id | 200 code=0（同 #228） | `src/api/customerSession.js` | 68 |
| 298 | [x] | GET | `/api/dashboards` | 用户端 | query: params | 200 code=0 total=37 | `src/api/dashboardScreen.js` | 4 |
| 299 | [x] | GET | `/api/dashboards/{id}` | 用户端 | path: id | 200 code=0 | `src/api/dashboardScreen.js` | 7 |
| 300 | [x] | POST | `/api/dashboards` | 用户端 | body: data | 200 code=0 id=40/41（layout 必须是对象，不能传字符串） | `src/api/dashboardScreen.js` | 10 |
| 301 | [x] | PUT | `/api/dashboards/{id}` | 用户端 | path: id / body: data | 200 code=0 | `src/api/dashboardScreen.js` | 13 |
| 302 | [x] | DELETE | `/api/dashboards/{id}` | 用户端 | path: id | 200 code=0 删除成功 | `src/api/dashboardScreen.js` | 16 |
| 303 | [x] | GET | `/api/dashboards/public/{code}` | 用户端 | path: code | 200 code=0 {screen,widgets}；非公开回 404（业务正确） | `src/api/dashboardScreen.js` | 19 |
| 304 | [x] | GET | `/api/dashboards/data` | 用户端 | - | 200 code=0 7 块聚合 | `src/api/dashboardScreen.js` | 23 |
| 305 | [x] | GET | `/api/dashboards/activities` | 用户端 | - | 200 code=0 array | `src/api/dashboardScreen.js` | 26 |
| 306 | [x] | GET | `/api/dashboards/data` | 用户端 | - | 200 code=0（同 #304，KPI 别名） | `src/api/dashboardScreen.js` | 29 |
| 307 | [x] | GET | `/api/dashboards/data` | 用户端 | query: params | 200 code=0（趋势别名，同 #304） | `src/api/dashboardScreen.js` | 32 |
| 308 | [x] | GET | `/api/dashboards/data` | 用户端 | - | 200 code=0（渠道别名，同 #304） | `src/api/dashboardScreen.js` | 35 |
| 309 | [x] | GET | `/api/dashboards/data` | 用户端 | - | 200 code=0（地区别名，同 #304） | `src/api/dashboardScreen.js` | 38 |
| 310 | [x] | POST | `/api/memory/messages` | 用户端 | body: data | 200 code=0 追加成功 | `src/api/dialogueMemory.js` | 5 |
| 311 | [x] | GET | `/api/memory/short` | 用户端 | query: params | 200 code=0 array | `src/api/dialogueMemory.js` | 9 |
| 312 | [x] | GET | `/api/memory/long` | 用户端 | query: params | 200 code=0 object | `src/api/dialogueMemory.js` | 13 |
| 313 | [x] | POST | `/api/memory/facts` | 用户端 | body: data | 200 code=0 更新成功 | `src/api/dialogueMemory.js` | 17 |
| 314 | [x] | POST | `/api/memory/objections` | 用户端 | body: data | 200 code=0 记录成功 | `src/api/dialogueMemory.js` | 21 |
| 315 | [x] | POST | `/api/memory/purchase-intent` | 用户端 | body: data | 200 code=0 更新成功 | `src/api/dialogueMemory.js` | 25 |
| 316 | [x] | POST | `/api/memory/intent-trail` | 用户端 | body: data | 200 code=0 记录成功 | `src/api/dialogueMemory.js` | 29 |
| 317 | [x] | POST | `/api/memory/sop-history` | 用户端 | body: data | 200 code=0 记录成功 | `src/api/dialogueMemory.js` | 33 |
| 318 | [x] | GET | `/api/memory/context` | 用户端 | query: params | 200 code=0 object | `src/api/dialogueMemory.js` | 37 |
| 319 | [x] | GET | `/api/memory/list` | 用户端 | query: params | 200 code=0 object | `src/api/dialogueMemory.js` | 41 |
| 320 | [x] | GET | `/api/dingtalk-app/accounts` | 用户端 | query: params | 200 code=0 array | `src/api/dingtalkApp.js` | 6 |
| 321 | [x] | GET | `/api/dingtalk-app/accounts/{id}` | 用户端 | path: id | 200 code=0（既有行 account_name 为空，属数据） | `src/api/dingtalkApp.js` | 10 |
| 322 | [x] | POST | `/api/dingtalk-app/accounts` | 用户端 | body: data | 200 code=0 id=11/12（ai_agent_id 是字符串） | `src/api/dingtalkApp.js` | 14 |
| 323 | [x] | PUT | `/api/dingtalk-app/accounts/{id}` | 用户端 | path: id / body: data | 200 code=0 更新成功 | `src/api/dingtalkApp.js` | 18 |
| 324 | [x] | DELETE | `/api/dingtalk-app/accounts/{id}` | 用户端 | path: id | 200 code=0 删除成功 | `src/api/dingtalkApp.js` | 22 |
| 325 | [x] | POST | `/api/dingtalk-app/accounts/{id}/test` | 用户端 | path: id | 200 code=INVALID_PARAM_1001「配置不完整」（缺 token/aes_key，业务正确） | `src/api/dingtalkApp.js` | 26 |
| 326 | [x] | GET | `/api/domain-pool/health` | 用户端 | - | 200 code=0 array | `src/api/domainPool.js` | 3 |
| 327 | [x] | GET | `/api/domain-pool/{domain}/blacklist` | 用户端 | path: domain | 200 code=0 ok | `src/api/domainPool.js` | 4 |
| 328 | [x] | POST | `/api/domain-pool/{id}/suspend` | 用户端 | path: id / body: {} | 200 code=0 域名已停用（测试后已还原 status/is_active） | `src/api/domainPool.js` | 5 |
| 329 | [x] | POST | `/api/domain-pool/{id}/rotate` | 用户端 | path: id / body: {} | 200 code=INVALID_PARAM_1001（已停用不可轮换，业务正确） | `src/api/domainPool.js` | 6 |
| 330 | [x] | GET | `/api/domain-pool/alerts` | 用户端 | query: params | 200 code=0 object | `src/api/domainPool.js` | 7 |
| 331 | [x] | POST | `/api/domain-pool/alerts/{id}/resolve` | 用户端 | path: id / body: {} | 200 code=NOT_FOUND_1002（无该告警，业务正确） | `src/api/domainPool.js` | 8 |
| 332 | [x] | GET | `/api/domain-pool` | 用户端 | query: params | 200 code=0 total=28/25 | `src/api/domainPool.js` | 11 |
| 333 | [x] | POST | `/api/domain-pool` | 用户端 | body: data | 200 code=0 id=133/134 | `src/api/domainPool.js` | 12 |
| 334 | [x] | PUT | `/api/domain-pool/{data_id}` | 用户端 | path: data_id / body: data | 200 code=0 更新成功 | `src/api/domainPool.js` | 13 |
| 335 | [x] | DELETE | `/api/domain-pool/{id}` | 用户端 | path: id | 200 code=0 删除成功 | `src/api/domainPool.js` | 14 |
| 336 | [x] | POST | `/api/domain-pool/{id}/check` | 用户端 | path: id / body: {} | 200 code=0 检查完成 | `src/api/domainPool.js` | 15 |
| 337 | [x] | POST | `/api/domain-pool/check-all` | 用户端 | body: {} | 200 code=0 检查完成 | `src/api/domainPool.js` | 16 |
| 338 | [x] | GET | `/api/douyin/list` | 用户端 | query: params | 200 code=0 total=26 | `src/api/douyinCard.js` | 4 |
| 339 | [x] | GET | `/api/douyin/{id}` | 用户端 | path: id | 200 code=0 | `src/api/douyinCard.js` | 8 |
| 340 | [x] | POST | `/api/douyin/create` | 用户端 | body: data | 200 code=0 id=63/64 | `src/api/douyinCard.js` | 12 |
| 341 | [x] | PUT | `/api/douyin/update` | 用户端 | body: data | 200 code=0 更新成功 | `src/api/douyinCard.js` | 16 |
| 342 | [x] | DELETE | `/api/douyin/delete/{id}` | 用户端 | path: id | 200 code=0 删除成功 | `src/api/douyinCard.js` | 20 |
| 343 | [x] | GET | `/api/douyin/view/{id}` | 用户端 | path: id | 200 code=0 获取成功 | `src/api/douyinCard.js` | 24 |
| 344 | [x] | GET | `/api/douyin/stats/card/{id}` | 用户端 | path: id / query: params | 200 code=0 object | `src/api/douyinCard.js` | 28 |
| 345 | [x] | GET | `/api/douyin/stats/overall` | 用户端 | query: params | 200 code=0 object | `src/api/douyinCard.js` | 32 |
| 346 | [x] | POST | `/api/douyin/{id}/generate-short-link` | 用户端 | path: id | 200 code=0 操作成功 | `src/api/douyinCard.js` | 36 |
| 347 | [x] | GET | `/api/email/smtp` | 用户端 | - | 200 code=0 object | `src/api/email.js` | 5 |
| 348 | [x] | POST | `/api/email/smtp` | 用户端 | body: data | 200 code=0（limit 必填；密码加密依赖 FIELD_ENCRYPTION_KEY，缺失时 fail-closed 拒明文落库） | `src/api/email.js` | 9 |
| 349 | [x] | PUT | `/api/email/smtp/{id}` | 用户端 | path: id / body: data | 200 code=0 更新成功 | `src/api/email.js` | 13 |
| 350 | [x] | DELETE | `/api/email/smtp/{id}` | 用户端 | path: id | 200 code=0 删除成功 | `src/api/email.js` | 17 |
| 351 | [x] | GET | `/api/email/drafts` | 用户端 | - | 200 code=0 object | `src/api/email.js` | 21 |
| 352 | [x] | GET | `/api/email/drafts/{id}` | 用户端 | path: id | 200 code=0 | `src/api/email.js` | 25 |
| 353 | [x] | POST | `/api/email/drafts` | 用户端 | body: data | 200 code=0 创建成功（uuid） | `src/api/email.js` | 29 |
| 354 | [x] | PUT | `/api/email/drafts/{id}` | 用户端 | path: id / body: data | 200 code=0 更新成功 | `src/api/email.js` | 33 |
| 355 | [x] | DELETE | `/api/email/drafts/{id}` | 用户端 | path: id | 200 code=0 删除成功 | `src/api/email.js` | 37 |
| 356 | [x] | POST | `/api/upload` | 用户端 | body: formData | 200 code=0（同 #57，A1 修复后 on 模式带签名） | `src/api/email.js` | 41 |
| 357 | [x] | POST | `/api/email/send` | 用户端 | body: data | 400 code=INVALID_PARAM_1001（缺 to/subject/content/smtp_id；未真实外发） | `src/api/email.js` | 49 |
| 358 | [x] | GET | `/api/email/list?page={page}&limit={limit}` | 用户端 | path: page, limit | 200 code=0 {list,total} | `src/api/email.js` | 52 |
| 359 | [x] | GET | `/api/email/jobs?page={page}&limit={limit}` | 用户端 | path: page, limit | 200 code=0 {list,total} | `src/api/email.js` | 56 |
| 360 | [x] | DELETE | `/api/email/jobs/{id}` | 用户端 | path: id | 200 code=0（0 行也回成功，见 A6 同类观察） | `src/api/email.js` | 59 |
| 361 | [x] | GET | `/api/email/jobs/{id}` | 用户端 | path: id | 200 code=0 详情（uuid） | `src/api/email.js` | 62 |
| 362 | [x] | GET | `/api/email/lists/{id}/tracking` | 用户端 | path: id | 200 code=NOT_FOUND_1002（该名单无追踪记录，业务正确） | `src/api/email.js` | 66 |
| 363 | [x] | DELETE | `/api/email/list/{id}` | 用户端 | path: id | 200 code=0（0 行也回 success，见 A6 同类观察） | `src/api/email.js` | 70 |
| 364 | [x] | GET | `/api/faqs` | 用户端 | query: params | 200 code=0 total=13392/13342 | `src/api/faq.js` | 5 |
| 365 | [x] | GET | `/api/faqs/{id}` | 用户端 | path: id | 200 code=0 查询成功 | `src/api/faq.js` | 9 |
| 366 | [x] | POST | `/api/faqs` | 用户端 | body: data | 200 code=0 id=244504/5（agent_id 必填） | `src/api/faq.js` | 13 |
| 367 | [x] | PUT | `/api/faqs/{id}` | 用户端 | path: id / body: data | 200 code=0；**修复前 off 报 enabled 非空约束、on 报改写 tenant_id** | `src/api/faq.js` | 17 |
| 368 | [x] | PATCH | `/api/faqs/{id}/toggle` | 用户端 | path: id / body: { enabled } | 200 code=0 状态已更新 | `src/api/faq.js` | 21 |
| 369 | [x] | DELETE | `/api/faqs/{id}` | 用户端 | path: id | 200 code=0 删除成功 | `src/api/faq.js` | 25 |
| 370 | [x] | POST | `/api/faqs/match` | 用户端 | body: data | 200 code=0（agent_id+msg 必填） | `src/api/faq.js` | 29 |
| 371 | [x] | GET | `/api/faqs/stats` | 用户端 | - | 200 code=0 object | `src/api/faq.js` | 33 |
| 372 | [x] | GET | `/api/feature-flags` | 用户端 | query: params | 200 code=0 total=7/5 | `src/api/featureFlags.js` | 15 |
| 373 | [x] | GET | `/api/feature-flags/stale` | 用户端 | - | 200 code=0 object | `src/api/featureFlags.js` | 21 |
| 374 | [x] | GET | `/api/feature-flags/{id}` | 用户端 | path: id | 200 code=0 | `src/api/featureFlags.js` | 25 |
| 375 | [x] | POST | `/api/feature-flags` | 用户端 | body: data | 200 code=0 id=22/23 | `src/api/featureFlags.js` | 29 |
| 376 | [x] | PUT | `/api/feature-flags/{id}` | 用户端 | path: id / body: data | 200 code=0 更新成功 | `src/api/featureFlags.js` | 33 |
| 377 | [x] | DELETE | `/api/feature-flags/{id}` | 用户端 | path: id | 200 code=0 删除成功 | `src/api/featureFlags.js` | 37 |
| 378 | [x] | POST | `/api/feature-flags/{id}/enable` | 用户端 | path: id | 200 code=0 已启用 | `src/api/featureFlags.js` | 44 |
| 379 | [x] | POST | `/api/feature-flags/{id}/disable` | 用户端 | path: id | 200 code=0 已禁用 | `src/api/featureFlags.js` | 48 |
| 380 | [x] | POST | `/api/feature-flags/{id}/rollout` | 用户端 | path: id / body: { percentage } | 200 code=0 灰度已更新 | `src/api/featureFlags.js` | 54 |
| 381 | [x] | GET | `/api/feature-flags/{id}/audit` | 用户端 | path: id | 200 code=0 object | `src/api/featureFlags.js` | 58 |
| 382 | [x] | GET | `/api/feature-flags/{id}/eval-log` | 用户端 | path: id | 200 code=0 object | `src/api/featureFlags.js` | 62 |
| 383 | [x] | GET | `/api/feishu/accounts` | 用户端 | query: params | 200 code=0（data 直接是数组） | `src/api/feishu.js` | 4 |
| 384 | [x] | GET | `/api/feishu/accounts/{id}` | 用户端 | path: id | 200 code=0 查询成功 | `src/api/feishu.js` | 8 |
| 385 | [x] | POST | `/api/feishu/accounts` | 用户端 | body: data | 200 code=0 id=19/20 | `src/api/feishu.js` | 12 |
| 386 | [x] | PUT | `/api/feishu/accounts/{id}` | 用户端 | path: id / body: data | 200 code=0 更新成功 | `src/api/feishu.js` | 16 |
| 387 | [x] | DELETE | `/api/feishu/accounts/{id}` | 用户端 | path: id | 200 code=0 删除成功 | `src/api/feishu.js` | 20 |
| 388 | [x] | POST | `/api/feishu/accounts/{id}/test-send` | 用户端 | path: id / body: data | 200 code=INTERNAL_ERROR_6002「发送失败」（假凭据+外网，环境前提） | `src/api/feishu.js` | 24 |
| 389 | [x] | POST | `/api/feishu/accounts/{id}/refresh-token` | 用户端 | path: id | 200 code=INTERNAL_ERROR_6002「刷新失败」（同上，外呼失败） | `src/api/feishu.js` | 28 |
| 390 | [x] | GET | `/api/followups/today` | 用户端 | query: params / body: SILENT | 200 code=SERVICE_UNAVAILABLE_6003「跟进服务未装配」（旅程运行时 off，设计内） | `src/api/followup.js` | 28 |
| 391 | [x] | GET | `/api/followups/pending` | 用户端 | query: params / body: SILENT | 200 code=SERVICE_UNAVAILABLE_6003（同上） | `src/api/followup.js` | 35 |
| 392 | [x] | GET | `/api/followups/overdue` | 用户端 | body: { owner_id: ownerId }; SILENT | 200 code=SERVICE_UNAVAILABLE_6003（同上） | `src/api/followup.js` | 39 |
| 393 | [x] | POST | `/api/followups/{id}/complete` | 用户端 | path: id / body: { result: result \|\| '', note: note \|\| '' } | 200 code=SERVICE_UNAVAILABLE_6003（跟进服务未装配，设计内） | `src/api/followup.js` | 46 |
| 394 | [x] | POST | `/api/followups/{id}/cancel` | 用户端 | path: id | 200 code=SERVICE_UNAVAILABLE_6003（同上） | `src/api/followup.js` | 50 |
| 395 | [x] | POST | `/api/geo/keywords/mine` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geo.js` | 5 |
| 396 | [x] | POST | `/api/geo/keywords/expand` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geo.js` | 9 |
| 397 | [x] | POST | `/api/geo/keywords/cluster` | 用户端 | body: data | 200 code=INTERNAL_ERROR_6002「话题集群失败」（本地 LLM 未启用，环境前提） | `src/api/geo.js` | 13 |
| 398 | [x] | GET | `/api/geo/keywords/list` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/geo.js` | 17 |
| 399 | [x] | POST | `/api/geo/keyword-mining/crawl-suggest` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geo.js` | 22 |
| 400 | [x] | POST | `/api/geo/keyword-mining/longtail` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geo.js` | 26 |
| 401 | [x] | GET | `/api/geo/keyword-mining/funnel` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/geo.js` | 30 |
| 402 | [x] | POST | `/api/geo/push/urls` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geo.js` | 35 |
| 403 | [x] | GET | `/api/geo/push/quota` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/geo.js` | 39 |
| 404 | [x] | GET | `/api/geo/push/sitemap` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/geo.js` | 43 |
| 405 | [x] | POST | `/api/geo/index-tracking/verify/{articleId}` | 用户端 | path: articleId | 200 code=0（两条腿均通过） | `src/api/geo.js` | 48 |
| 406 | [x] | GET | `/api/geo/index-tracking/funnel` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/geo.js` | 52 |
| 407 | [x] | POST | `/api/geo/index-tracking/verify-all` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/geo.js` | 56 |
| 408 | [x] | POST | `/api/geo/site/export` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/geo.js` | 61 |
| 409 | [x] | POST | `/api/geo/site/deploy` | 用户端 | - | 200 code=INTERNAL_ERROR_6002（无 git 仓库 /tmp/hugo-site，环境前提） | `src/api/geo.js` | 65 |
| 410 | [x] | POST | `/api/geo/site/full-pipeline` | 用户端 | - | 200 code=INTERNAL_ERROR_6002（同上，环境前提） | `src/api/geo.js` | 69 |
| 411 | [x] | GET | `/api/geo/site/llms-txt-preview` | 用户端 | body: domain ? { domain } : {} | 200 code=0（两条腿均通过） | `src/api/geo.js` | 73 |
| 412 | [x] | GET | `/api/geo/site/robots-preview` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/geo.js` | 77 |
| 413 | [x] | GET | `/api/geo/sites` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/geo.js` | 82 |
| 414 | [x] | POST | `/api/geo/sites` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geo.js` | 86 |
| 415 | [x] | PUT | `/api/geo/sites/{id}` | 用户端 | path: id / body: data | 200 code=0（两条腿均通过） | `src/api/geo.js` | 90 |
| 416 | [x] | DELETE | `/api/geo/sites/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/geo.js` | 94 |
| 417 | [x] | GET | `/api/geo/pushers` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/geo.js` | 99 |
| 418 | [x] | POST | `/api/geo/pushers` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geo.js` | 103 |
| 419 | [x] | PUT | `/api/geo/pushers/{id}` | 用户端 | path: id / body: data | 200 code=0（两条腿均通过） | `src/api/geo.js` | 107 |
| 420 | [x] | DELETE | `/api/geo/pushers/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/geo.js` | 111 |
| 421 | [x] | GET | `/api/geo/schema-templates` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/geo.js` | 116 |
| 422 | [x] | POST | `/api/geo/schema-templates` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geo.js` | 120 |
| 423 | [x] | PUT | `/api/geo/schema-templates/{id}` | 用户端 | path: id / body: data | 200 code=0（两条腿均通过） | `src/api/geo.js` | 124 |
| 424 | [x] | DELETE | `/api/geo/schema-templates/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/geo.js` | 128 |
| 425 | [x] | DELETE | `/api/geo/keywords/{id}` | 用户端 | path: id | 200 code=NOT_FOUND_1002「删除关键词失败」（不存在 id，业务正确） | `src/api/geo.js` | 132 |
| 426 | [x] | POST | `/api/geo/content/generate` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geo.js` | 136 |
| 427 | [x] | POST | `/api/geo/content/batch-generate` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geo.js` | 141 |
| 428 | [x] | POST | `/api/geo/content/score` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geo.js` | 145 |
| 429 | [x] | POST | `/api/geo/content/optimize` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geo.js` | 149 |
| 430 | [x] | POST | `/api/geo/content/eeat` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geo.js` | 153 |
| 431 | [x] | POST | `/api/geo/content/schema` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geo.js` | 157 |
| 432 | [x] | POST | `/api/geo/content/uniqueness` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geo.js` | 161 |
| 433 | [x] | GET | `/api/geo/content/list` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/geo.js` | 165 |
| 434 | [x] | GET | `/api/geo/content/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/geo.js` | 169 |
| 435 | [x] | POST | `/api/geo/verification/verify` | 用户端 | body: data | 200 code=INTERNAL_ERROR_6002「AI 搜索验证失败」（外部 AI 搜索不可用，环境前提） | `src/api/geo.js` | 173 |
| 436 | [x] | POST | `/api/geo/verification/negative` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geo.js` | 177 |
| 437 | [x] | GET | `/api/geo/verification/results/{articleId}` | 用户端 | path: articleId | 200 code=0（两条腿均通过） | `src/api/geo.js` | 181 |
| 438 | [x] | GET | `/api/geo/reports/summary` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/geo.js` | 185 |
| 439 | [x] | GET | `/api/geo/reports/roi` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/geo.js` | 189 |
| 440 | [x] | GET | `/api/geo/reports/api-costs` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/geo.js` | 193 |
| 441 | [x] | GET | `/api/geo/config` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/geo.js` | 197 |
| 442 | [x] | PUT | `/api/geo/config` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geo.js` | 201 |
| 443 | [x] | POST | `/api/geo/config/optimize` | 用户端 | body: data | 200 code=INTERNAL_ERROR_6002「配置优化失败」（LLM 不可用，环境前提） | `src/api/geo.js` | 205 |
| 444 | [x] | GET | `/api/geo/alerts` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/geoAlert.js` | 4 |
| 445 | [x] | GET | `/api/geo/alerts/unread-count` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/geoAlert.js` | 7 |
| 446 | [x] | POST | `/api/geo/alerts/{id}/ack` | 用户端 | path: id / body: {} | 200 code=0（两条腿均通过） | `src/api/geoAlert.js` | 10 |
| 447 | [x] | DELETE | `/api/geo/alerts/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/geoAlert.js` | 13 |
| 448 | [x] | GET | `/api/geo/decision/report` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/geoDecision.js` | 3 |
| 449 | [x] | GET | `/api/geo/decision/tasks` | 用户端 | body: { limit } | 200 code=0（两条腿均通过） | `src/api/geoDecision.js` | 6 |
| 450 | [x] | POST | `/api/geo/decision/tasks/{id}/done` | 用户端 | path: id | 200 code=NOT_FOUND_1002（任务 id 不存在，业务正确） | `src/api/geoDecision.js` | 8 |
| 451 | [x] | GET | `/api/geo/entity/list` | 用户端 | body: { type: type \|\| '', keyword: keyword \|\| '' } | 200 code=0 | `src/api/geoEntity.js` | 4 |
| 452 | [x] | GET | `/api/geo/entity/{entityId}/graph` | 用户端 | path: entityId | 200 code=0（两条腿均通过） | `src/api/geoEntity.js` | 7 |
| 453 | [x] | POST | `/api/geo/entities/extract` | 用户端 | body: { doc_id: docId } | 200 code=BUSINESS_5001（doc_id 不存在，业务正确） | `src/api/geoEntity.js` | 10 |
| 454 | [x] | GET | `/api/geo/kb/documents` | 用户端 | body: { keyword: keyword \|\| '', source_level: sourceLevel \|\| '' } | 200 code=0 | `src/api/geoEntity.js` | 13 |
| 455 | [x] | POST | `/api/geo/kb/documents` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geoEntity.js` | 16 |
| 456 | [x] | DELETE | `/api/geo/kb/documents/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/geoEntity.js` | 19 |
| 457 | [x] | GET | `/api/geo/kb/documents/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/geoEntity.js` | 22 |
| 458 | [x] | GET | `/api/geo/kb/search` | 用户端 | body: { q, limit } | 200 code=0（两条腿均通过） | `src/api/geoEntity.js` | 25 |
| 459 | [x] | POST | `/api/geo/kb/ask` | 用户端 | body: { question } | 200 code=0（两条腿均通过） | `src/api/geoEntity.js` | 28 |
| 460 | [x] | GET | `/api/geo/workflow/workflows` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/geoEntity.js` | 31 |
| 461 | [x] | GET | `/api/geo/workflow/workflows/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/geoEntity.js` | 34 |
| 462 | [x] | POST | `/api/geo/workflow/workflows` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geoEntity.js` | 37 |
| 463 | [x] | PUT | `/api/geo/workflow/workflows/{id}` | 用户端 | path: id / body: data | 200 code=0（两条腿均通过） | `src/api/geoEntity.js` | 40 |
| 464 | [x] | DELETE | `/api/geo/workflow/workflows/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/geoEntity.js` | 43 |
| 465 | [x] | POST | `/api/geo/workflow/workflows/{id}/run` | 用户端 | path: id / body: params \|\| {} | 200 code=0 执行完成 | `src/api/geoEntity.js` | 46 |
| 466 | [x] | GET | `/api/geo/workflow/workflows/{workflowId}/executions` | 用户端 | path: workflowId | 200 code=0（两条腿均通过） | `src/api/geoEntity.js` | 49 |
| 467 | [x] | GET | `/api/geo/workflow/templates` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/geoEntity.js` | 52 |
| 468 | [x] | POST | `/api/geo/workflow/templates` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geoEntity.js` | 55 |
| 469 | [x] | GET | `/api/geo/platform/platforms` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/geoPlatform.js` | 4 |
| 470 | [x] | GET | `/api/geo/platform/accounts` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/geoPlatform.js` | 7 |
| 471 | [x] | POST | `/api/geo/platform/accounts` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/geoPlatform.js` | 10 |
| 472 | [x] | DELETE | `/api/geo/platform/accounts/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/geoPlatform.js` | 13 |
| 473 | [x] | POST | `/api/geo/platform/publish` | 用户端 | body: { article_id: articleId, platform, ...opts } | 200 code=0（两条腿均通过） | `src/api/geoPlatform.js` | 16 |
| 474 | [x] | GET | `/api/geo/platform/records` | 用户端 | body: { article_id: articleId \|\| '' } | 200 code=0 | `src/api/geoPlatform.js` | 19 |
| 475 | [x] | GET | `/api/geo/probe/engines` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/geoProbe.js` | 4 |
| 476 | [x] | POST | `/api/geo/probe/test` | 用户端 | body: { engine, query } | 200 code=INVALID_PARAM_1001「engine baidu not found (available: sensenova)」（只配了 sensenova，业务正确） | `src/api/geoProbe.js` | 7 |
| 477 | [x] | GET | `/api/geo/probe/runs` | 用户端 | body: { engine, limit } | 200 code=0（两条腿均通过） | `src/api/geoProbe.js` | 10 |
| 478 | [x] | POST | `/api/geo/probe/run-negative` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/geoProbe.js` | 13 |
| 479 | [x] | POST | `/api/geo/probe/run-source-sync` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/geoProbe.js` | 16 |
| 480 | [x] | POST | `/api/geo/probe/all` | 用户端 | body: { engines, query } | 200 code=0（两条腿均通过） | `src/api/geoProbe.js` | 19 |
| 481 | [x] | GET | `/api/geo/sov` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/geoProbe.js` | 22 |
| 482 | [x] | GET | `/api/geo/crawler-stats` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/geoProbe.js` | 25 |
| 483 | [x] | POST | `/api/geo/crawler/run` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/geoProbe.js` | 28 |
| 484 | [x] | POST | `/api/geo/inaccurate-claims` | 用户端 | body: { brand_name: brandName } | 200 code=INTERNAL_ERROR_6002（LLM 全熔断，环境前提） | `src/api/geoProbe.js` | 31 |
| 485 | [x] | GET | `/api/geo/source-catalog/levels` | 用户端 | body: { url } | 200 code=0（两条腿均通过） | `src/api/geoProbe.js` | 34 |
| 486 | [x] | GET | `/api/geo/competitors` | 用户端 | - | 200 code=0 创建成功；SaaS 重复 domain 回 400 并泄漏 SQLSTATE（见 A14） | `src/api/geoProbe.js` | 37 |
| 487 | [x] | GET | `/api/geo/competitors/{id}` | 用户端 | path: id | 200 code=0 创建成功；SaaS 重复 domain 回 400 并泄漏 SQLSTATE（见 A14） | `src/api/geoProbe.js` | 40 |
| 488 | [x] | POST | `/api/geo/competitors` | 用户端 | body: data | 200 code=0 创建成功；SaaS 重复 domain 回 400 并泄漏 SQLSTATE（见 A14） | `src/api/geoProbe.js` | 43 |
| 489 | [x] | PUT | `/api/geo/competitors/{id}` | 用户端 | path: id / body: data | 200 code=0 创建成功；SaaS 重复 domain 回 400 并泄漏 SQLSTATE（见 A14） | `src/api/geoProbe.js` | 46 |
| 490 | [x] | DELETE | `/api/geo/competitors/{id}` | 用户端 | path: id | 200 code=0 创建成功；SaaS 重复 domain 回 400 并泄漏 SQLSTATE（见 A14） | `src/api/geoProbe.js` | 49 |
| 491 | [x] | GET | `/api/geo/visibility/engine-compare` | 用户端 | body: { days, intent: intent \|\| '' } | 200 code=0 | `src/api/geoProbe.js` | 52 |
| 492 | [x] | GET | `/api/geo/sov/trend` | 用户端 | body: { days, intent: intent \|\| '' } | 200 code=0 | `src/api/geoProbe.js` | 55 |
| 493 | [x] | POST | `/api/glossaries` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/glossary.js` | 4 |
| 494 | [x] | GET | `/api/glossaries` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/glossary.js` | 8 |
| 495 | [x] | GET | `/api/glossaries/{termId}` | 用户端 | path: termId | 200 code=0（:termId 传 term_id 字符串，不是数字主键） | `src/api/glossary.js` | 12 |
| 496 | [x] | PUT | `/api/glossaries/{termId}` | 用户端 | path: termId / body: data | 200 code=0 更新成功（path 用 term_id） | `src/api/glossary.js` | 16 |
| 497 | [x] | DELETE | `/api/glossaries/{termId}` | 用户端 | path: termId | 200 code=0（两条腿均通过） | `src/api/glossary.js` | 20 |
| 498 | [x] | POST | `/api/glossaries/validate` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/glossary.js` | 24 |
| 499 | [x] | GET | `/api/human-tasks` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/humanTask.js` | 43 |
| 500 | [x] | GET | `/api/human-tasks/counts` | 用户端 | body: undefined | 200 code=0（两条腿均通过） | `src/api/humanTask.js` | 49 |
| 501 | [x] | GET | `/api/human-tasks/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/humanTask.js` | 53 |
| 502 | [x] | POST | `/api/human-tasks/{id}/claim` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/humanTask.js` | 58 |
| 503 | [x] | POST | `/api/human-tasks/{id}/release` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/humanTask.js` | 63 |
| 504 | [x] | POST | `/api/human-tasks/{id}/complete` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/humanTask.js` | 68 |
| 505 | [x] | POST | `/api/human-tasks/{id}/cancel` | 用户端 | path: id / body: { reason } | 200 code=0（两条腿均通过） | `src/api/humanTask.js` | 73 |
| 506 | [x] | GET | `/api/i18n/stats` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/i18nStats.js` | 4 |
| 507 | [x] | GET | `/api/i18n/stats/lang-dist` | 用户端 | query: days | 200 code=0（两条腿均通过） | `src/api/i18nStats.js` | 8 |
| 508 | [x] | GET | `/api/i18n/stats/cache` | 用户端 | query: days | 200 code=0（两条腿均通过） | `src/api/i18nStats.js` | 12 |
| 509 | [x] | GET | `/api/i18n/stats/glossary` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/i18nStats.js` | 16 |
| 510 | [x] | GET | `/api/i18n/stats/quality` | 用户端 | query: days | 200 code=0（两条腿均通过） | `src/api/i18nStats.js` | 20 |
| 511 | [x] | GET | `/api/i18n/stats/latency` | 用户端 | query: days | 200 code=0（两条腿均通过） | `src/api/i18nStats.js` | 24 |
| 512 | [x] | GET | `/api/inbox/conversations` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/inbox.js` | 7 |
| 513 | [x] | GET | `/api/inbox/conversations/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/inbox.js` | 12 |
| 514 | [x] | GET | `/api/inbox/conversations/{id}/messages` | 用户端 | path: id / query: params | 200 code=0（两条腿均通过） | `src/api/inbox.js` | 17 |
| 515 | [x] | POST | `/api/inbox/conversations/{id}/read` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/inbox.js` | 22 |
| 516 | [x] | POST | `/api/inbox/conversations/{id}/pin` | 用户端 | path: id / body: { pinned } | 200 code=0（两条腿均通过） | `src/api/inbox.js` | 27 |
| 517 | [x] | POST | `/api/inbox/conversations/{id}/star` | 用户端 | path: id / body: { starred } | 200 code=0（两条腿均通过） | `src/api/inbox.js` | 31 |
| 518 | [x] | POST | `/api/inbox/conversations/{id}/mute` | 用户端 | path: id / body: { muted } | 200 code=0（两条腿均通过） | `src/api/inbox.js` | 35 |
| 519 | [x] | POST | `/api/inbox/conversations/{id}/tags` | 用户端 | path: id / body: { tag } | 200 code=0（两条腿均通过） | `src/api/inbox.js` | 40 |
| 520 | [x] | DELETE | `/api/inbox/conversations/{id}/tags/{tag}` | 用户端 | path: id, tag | 200 code=0（两条腿均通过） | `src/api/inbox.js` | 44 |
| 521 | [x] | DELETE | `/api/inbox/conversations/{id}/messages/{mid}?source={source}` | 用户端 | path: id, mid, source | 200 code=0（两条腿均通过） | `src/api/inbox.js` | 49 |
| 522 | [x] | POST | `/api/inbox/assign` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/inbox.js` | 54 |
| 523 | [x] | POST | `/api/inbox/assign/auto` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/inbox.js` | 59 |
| 524 | [x] | GET | `/api/inbox/assignments` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/inbox.js` | 64 |
| 525 | [x] | GET | `/api/inbox/stats` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/inbox.js` | 69 |
| 526 | [x] | POST | `/api/inbox/reconcile?mode={mode}` | 用户端 | path: mode | 200 code=0（两条腿均通过） | `src/api/inbox.js` | 74 |
| 527 | [x] | GET | `/api/inbox/staff/{staff}/load` | 用户端 | path: staff | 200 code=0（两条腿均通过） | `src/api/inbox.js` | 79 |
| 528 | [x] | GET | `/api/inbox/agents` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/inbox.js` | 84 |
| 529 | [x] | GET | `/api/integrations` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/integration.js` | 4 |
| 530 | [x] | GET | `/api/integrations/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/integration.js` | 7 |
| 531 | [x] | POST | `/api/integrations` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/integration.js` | 10 |
| 532 | [x] | PUT | `/api/integrations/{id}` | 用户端 | path: id / body: data | 200 code=0（两条腿均通过） | `src/api/integration.js` | 13 |
| 533 | [x] | DELETE | `/api/integrations/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/integration.js` | 16 |
| 534 | [x] | POST | `/api/integrations/{id}/sync-customers` | 用户端 | path: id | 200 code=INTERNAL_ERROR_6002「不支持的平台」（该账号未配置可用凭据，环境/业务前提） | `src/api/integration.js` | 19 |
| 535 | [x] | POST | `/api/integrations/{id}/sync-products` | 用户端 | path: id | 200 code=INTERNAL_ERROR_6002「不支持的平台」（同上） | `src/api/integration.js` | 22 |
| 536 | [x] | GET | `/api/integration/sync-logs` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/integration.js` | 25 |
| 537 | [x] | GET | `/api/integration/external-customers` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/integration.js` | 28 |
| 538 | [x] | GET | `/api/integration/external-orders` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/integration.js` | 31 |
| 539 | [x] | GET | `/api/integration/external-orders-by-customer` | 用户端 | query: phone, name | 200 code=0（两条腿均通过） | `src/api/integration.js` | 34 |
| 540 | [x] | GET | `/api/integration/external-products` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/integration.js` | 37 |
| 541 | [x] | POST | `/api/integrations/{id}/test` | 用户端 | path: id | 200 code=INVALID_PARAM_1001「API Key 不能为空」（业务校验正确） | `src/api/integration.js` | 56 |
| 542 | [x] | POST | `/api/intent/recognize` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/intentRecognition.js` | 5 |
| 543 | [x] | POST | `/api/intent/recognize/batch` | 用户端 | body: data | 200 code=0（items 为对象数组：[{text:...}]） | `src/api/intentRecognition.js` | 9 |
| 544 | [x] | GET | `/api/intent/stats` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/intentRecognition.js` | 13 |
| 545 | [x] | GET | `/api/intent/recent` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/intentRecognition.js` | 17 |
| 546 | [x] | GET | `/api/intent/dict` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/intentRecognition.js` | 21 |
| 547 | [x] | POST | `/api/intent/recognize/fine` | 用户端 | body: data | 200 code=0（message 必填） | `src/api/intentRecognition.js` | 25 |
| 548 | [x] | GET | `/api/intent/logs` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/intentRecognition.js` | 29 |
| 549 | [x] | GET | `/api/intent/stats/fine` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/intentRecognition.js` | 33 |
| 550 | [x] | GET | `/api/intent/config` | 用户端 | - | 200 code=0（PUT 需回传完整 enabled 等字段） | `src/api/intentRecognition.js` | 37 |
| 551 | [x] | PUT | `/api/intent/config` | 用户端 | body: data | 200 code=0（PUT 需回传完整 enabled 等字段） | `src/api/intentRecognition.js` | 41 |
| 552 | [x] | GET | `/api/intent-records/keywords-override` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/intentRecognition.js` | 45 |
| 553 | [x] | PUT | `/api/intent-records/keywords-override` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/intentRecognition.js` | 49 |
| 554 | [x] | GET | `/api/kb-connectors` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/kbConnector.js` | 5 |
| 555 | [x] | POST | `/api/kb-connectors` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/kbConnector.js` | 6 |
| 556 | [x] | PUT | `/api/kb-connectors/{id}` | 用户端 | path: id / body: data | 200 code=0（两条腿均通过） | `src/api/kbConnector.js` | 7 |
| 557 | [x] | DELETE | `/api/kb-connectors/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/kbConnector.js` | 8 |
| 558 | [x] | POST | `/api/kb-connectors/{id}/sync` | 用户端 | path: id | 200 code=INTERNAL_ERROR_6002（远端返回 HTML，非 {docs:[]} 清单，环境前提） | `src/api/kbConnector.js` | 9 |
| 559 | [x] | GET | `/api/capabilities` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/kbConnector.js` | 14 |
| 560 | [x] | GET | `/api/kb/crawler/sources` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/kbCrawler.js` | 7 |
| 561 | [x] | POST | `/api/kb/crawler/sources` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/kbCrawler.js` | 11 |
| 562 | [x] | PUT | `/api/kb/crawler/sources/{id}` | 用户端 | path: id / body: data | 200 code=0（两条腿均通过） | `src/api/kbCrawler.js` | 14 |
| 563 | [x] | POST | `/api/kb/crawler/sources/{id}/pause` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/kbCrawler.js` | 17 |
| 564 | [x] | POST | `/api/kb/crawler/sources/{id}/resume` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/kbCrawler.js` | 20 |
| 565 | [x] | POST | `/api/kb/crawler/sources/{id}/verify` | 用户端 | path: id | 200 code=INVALID_PARAM_1001（目标站未放验证文件，业务正确） | `src/api/kbCrawler.js` | 23 |
| 566 | [x] | DELETE | `/api/kb/crawler/sources/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/kbCrawler.js` | 26 |
| 567 | [x] | POST | `/api/kb/crawler/sources/{id}/sync` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/kbCrawler.js` | 30 |
| 568 | [x] | GET | `/api/kb/crawler/batches` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/kbCrawler.js` | 34 |
| 569 | [x] | POST | `/api/kb/crawler/batches/{id}/retry` | 用户端 | path: id | 200 code=INVALID_PARAM_1001「仅失败批次可重试」（业务正确） | `src/api/kbCrawler.js` | 37 |
| 570 | [x] | GET | `/api/kb/qa/candidates` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/kbCrawler.js` | 44 |
| 571 | [x] | POST | `/api/kb/qa/candidates/{id}/approve` | 用户端 | path: id | 200 code=DUPLICATE_ENTRY_3003（indexed→approved 非法跃迁，业务正确） | `src/api/kbCrawler.js` | 47 |
| 572 | [x] | POST | `/api/kb/qa/candidates/{id}/reject` | 用户端 | path: id | 200 code=DUPLICATE_ENTRY_3003（同上） | `src/api/kbCrawler.js` | 50 |
| 573 | [x] | POST | `/api/kb/qa/candidates/{id}/publish` | 用户端 | path: id | 200 code=DUPLICATE_ENTRY_3003（需先 approve，业务正确） | `src/api/kbCrawler.js` | 53 |
| 574 | [x] | POST | `/api/knowledge/import/upload` | 用户端 | body: formData | 200 code=0 文件已接收（multipart：file+product_id+kb_id） | `src/api/knowledge.js` | 5 |
| 575 | [x] | POST | `/api/knowledge/import/text` | 用户端 | body: data | 200 code=0 文本导入已启动（product_id 为 UUID 字符串） | `src/api/knowledge.js` | 10 |
| 576 | [x] | POST | `/api/knowledge/import/url` | 用户端 | body: data | 200 code=0 URL 抓取已启动 | `src/api/knowledge.js` | 13 |
| 577 | [x] | GET | `/api/knowledge/documents` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 17 |
| 578 | [x] | GET | `/api/knowledge/documents/{id}` | 用户端 | path: id / query: params | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 20 |
| 579 | [x] | GET | `/api/knowledge/documents/{id}/progress` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 23 |
| 580 | [x] | GET | `/api/knowledge/documents/{id}/chunks` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 26 |
| 581 | [x] | PUT | `/api/knowledge/documents/{id}` | 用户端 | path: id / body: data | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 29 |
| 582 | [x] | DELETE | `/api/knowledge/documents/{id}` | 用户端 | path: id / query: params | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 32 |
| 583 | [x] | POST | `/api/knowledge/documents/{id}/reindex` | 用户端 | path: id / query: params / body: { params } | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 35 |
| 584 | [x] | POST | `/api/knowledge/products/{productId}/rebuild-index` | 用户端 | path: productId | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 39 |
| 585 | [x] | GET | `/api/knowledge/products/{productId}/overview` | 用户端 | path: productId | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 42 |
| 586 | [x] | POST | `/api/knowledge/search` | 用户端 | body: data | 200 code=0（product_id 是 rag 产品 UUID 字符串） | `src/api/knowledge.js` | 46 |
| 587 | [x] | GET | `/api/knowledge/import-logs` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 50 |
| 588 | [x] | GET | `/api/knowledge/openapi/sources` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 54 |
| 589 | [x] | POST | `/api/knowledge/openapi/sources` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 57 |
| 590 | [x] | GET | `/api/knowledge/openapi/sources/{id}` | 用户端 | path: id / query: params | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 60 |
| 591 | [x] | PUT | `/api/knowledge/openapi/sources/{id}` | 用户端 | path: id / body: data | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 63 |
| 592 | [x] | DELETE | `/api/knowledge/openapi/sources/{id}` | 用户端 | path: id / query: params | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 66 |
| 593 | [x] | POST | `/api/knowledge/openapi/sources/{id}/sync` | 用户端 | path: id / query: params / body: { params } | 200 code=INTERNAL_ERROR_6002「HTTP 404」（示例端点无 OpenAPI 文档，环境前提） | `src/api/knowledge.js` | 69 |
| 594 | [x] | POST | `/api/knowledge/openapi/sources/{id}/test` | 用户端 | path: id / body: data | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 72 |
| 595 | [x] | POST | `/api/knowledge/openapi/sources/{id}/toggle` | 用户端 | path: id / query: params / body: data; { params } | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 75 |
| 596 | [x] | GET | `/api/knowledge/stats/overview` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 79 |
| 597 | [x] | GET | `/api/knowledge/stats/documents` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 82 |
| 598 | [x] | GET | `/api/knowledge/stats/searches` | 用户端 | query: params | 200 code=0（product_id 是 rag 产品 UUID 字符串） | `src/api/knowledge.js` | 85 |
| 599 | [x] | GET | `/api/knowledge/stats/imports` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 88 |
| 600 | [x] | GET | `/api/knowledge/stats/openapi` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/knowledge.js` | 91 |
| 601 | [x] | GET | `/api/knowledge-bases` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/knowledgeBase.js` | 4 |
| 602 | [x] | GET | `/api/knowledge-bases/by-type/{kbType}` | 用户端 | path: kbType / query: params | 200 code=0（两条腿均通过） | `src/api/knowledgeBase.js` | 8 |
| 603 | [x] | GET | `/api/knowledge-bases/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/knowledgeBase.js` | 12 |
| 604 | [x] | POST | `/api/knowledge-bases` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/knowledgeBase.js` | 16 |
| 605 | [x] | PUT | `/api/knowledge-bases/{id}` | 用户端 | path: id / body: data | 200 code=0（两条腿均通过） | `src/api/knowledgeBase.js` | 20 |
| 606 | [x] | DELETE | `/api/knowledge-bases/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/knowledgeBase.js` | 24 |
| 607 | [x] | GET | `/api/knowledge-bases/{id}/stats` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/knowledgeBase.js` | 28 |
| 608 | [x] | POST | `/api/knowledge-merchant/batch/import` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/knowledgeMerchant.js` | 5 |
| 609 | [x] | POST | `/api/knowledge-merchant/batch/upload` | 用户端 | body: formData | 400 code=INVALID_PARAM_1001（缺 product_id/source，校验正确） | `src/api/knowledgeMerchant.js` | 8 |
| 610 | [x] | POST | `/api/knowledge-merchant/playground` | 用户端 | body: data | 200 code=0（需 product_id UUID） | `src/api/knowledgeMerchant.js` | 14 |
| 611 | [x] | GET | `/api/knowledge-merchant/documents/{documentId}/chunks` | 用户端 | path: documentId / query: params | 200 code=0（两条腿均通过） | `src/api/knowledgeMerchant.js` | 18 |
| 612 | [x] | PUT | `/api/knowledge-merchant/chunks/{chunkId}` | 用户端 | path: chunkId / body: data | 200 code=0 更新成功 | `src/api/knowledgeMerchant.js` | 21 |
| 613 | [x] | DELETE | `/api/knowledge-merchant/chunks/{chunkId}` | 用户端 | path: chunkId | 200 code=0（两条腿均通过） | `src/api/knowledgeMerchant.js` | 24 |
| 614 | [x] | POST | `/api/knowledge-merchant/chunks/{chunkId}/split` | 用户端 | path: chunkId / body: data | 200 code=0（两条腿均通过） | `src/api/knowledgeMerchant.js` | 27 |
| 615 | [x] | POST | `/api/knowledge-merchant/feedback` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/knowledgeMerchant.js` | 31 |
| 616 | [x] | GET | `/api/knowledge-merchant/feedbacks` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/knowledgeMerchant.js` | 34 |
| 617 | [x] | POST | `/api/knowledge-merchant/tokens` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/knowledgeMerchant.js` | 38 |
| 618 | [x] | GET | `/api/knowledge-merchant/tokens` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/knowledgeMerchant.js` | 41 |
| 619 | [x] | POST | `/api/knowledge-merchant/tokens/{tokenId}/revoke` | 用户端 | path: tokenId | 200 code=0（两条腿均通过） | `src/api/knowledgeMerchant.js` | 44 |
| 620 | [x] | POST | `/api/knowledge-merchant/external/import` | 用户端 | body: data | 400 code=INVALID_PARAM_1001（缺 product_id/source，校验正确） | `src/api/knowledgeMerchant.js` | 48 |
| 621 | [x] | GET | `/api/knowledge-merchant/external/jobs` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/knowledgeMerchant.js` | 53 |
| 622 | [x] | GET | `/api/kuaishou/list` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/kuaishouCard.js` | 4 |
| 623 | [x] | GET | `/api/kuaishou/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/kuaishouCard.js` | 8 |
| 624 | [x] | POST | `/api/kuaishou/create` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/kuaishouCard.js` | 12 |
| 625 | [x] | PUT | `/api/kuaishou/update` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/kuaishouCard.js` | 16 |
| 626 | [x] | DELETE | `/api/kuaishou/delete/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/kuaishouCard.js` | 20 |
| 627 | [x] | GET | `/api/kuaishou/view/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/kuaishouCard.js` | 24 |
| 628 | [x] | POST | `/api/kuaishou/like/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/kuaishouCard.js` | 28 |
| 629 | [x] | POST | `/api/kuaishou/share/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/kuaishouCard.js` | 32 |
| 630 | [x] | GET | `/api/kuaishou/stats/card/{id}` | 用户端 | path: id / query: params | 200 code=0（两条腿均通过） | `src/api/kuaishouCard.js` | 36 |
| 631 | [x] | GET | `/api/kuaishou/stats/overall` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/kuaishouCard.js` | 40 |
| 632 | [x] | POST | `/api/kuaishou/{id}/generate-short-link` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/kuaishouCard.js` | 44 |
| 633 | [x] | GET | `/api/lead-mining/config` | 用户端 | - | 200 code=0（GET）／400 需至少一个配置字段（POST 校验正确） | `src/api/leadMining.js` | 4 |
| 634 | [x] | POST | `/api/lead-mining/config` | 用户端 | body: data | 200 code=0（GET）／400 需至少一个配置字段（POST 校验正确） | `src/api/leadMining.js` | 8 |
| 635 | [x] | GET | `/api/live-codes/list` | 用户端 | query: params | [~] 缺 short_domains/entry/landing 种子数据，成功路径未验；校验链与不存在 id 的 404 均正确（2026-10-27 复核）| `src/api/livecode.js` | 4 |
| 636 | [x] | GET | `/api/live-codes/{id}` | 用户端 | path: id | [~] 缺 short_domains/entry/landing 种子数据，成功路径未验；校验链与不存在 id 的 404 均正确（2026-10-27 复核）| `src/api/livecode.js` | 8 |
| 637 | [x] | POST | `/api/live-codes/create` | 用户端 | body: data | [~] 缺 short_domains/entry/landing 种子数据，成功路径未验；校验链与不存在 id 的 404 均正确（2026-10-27 复核）| `src/api/livecode.js` | 12 |
| 638 | [x] | PUT | `/api/live-codes/{id}/update` | 用户端 | path: id / body: data | [~] 缺 short_domains/entry/landing 种子数据，成功路径未验；校验链与不存在 id 的 404 均正确（2026-10-27 复核）| `src/api/livecode.js` | 16 |
| 639 | [x] | DELETE | `/api/live-codes/{id}/delete` | 用户端 | path: id | [~] 缺 short_domains/entry/landing 种子数据，成功路径未验；校验链与不存在 id 的 404 均正确（2026-10-27 复核）| `src/api/livecode.js` | 20 |
| 640 | [x] | GET | `/api/live-codes/{id}/stats` | 用户端 | path: id | [~] 缺 short_domains/entry/landing 种子数据，成功路径未验；校验链与不存在 id 的 404 均正确（2026-10-27 复核）| `src/api/livecode.js` | 24 |
| 641 | [x] | GET | `/api/live-codes/{liveCodeId}/qrcodes` | 用户端 | path: liveCodeId | [~] 缺 short_domains/entry/landing 种子数据，成功路径未验；校验链与不存在 id 的 404 均正确（2026-10-27 复核）| `src/api/livecode.js` | 28 |
| 642 | [x] | POST | `/api/live-codes/{liveCodeId}/qrcodes/create` | 用户端 | path: liveCodeId / body: data | [~] 缺 short_domains/entry/landing 种子数据，成功路径未验；校验链与不存在 id 的 404 均正确（2026-10-27 复核）| `src/api/livecode.js` | 32 |
| 643 | [x] | GET | `/api/live-codes/qrcodes/{qrId}/stats` | 用户端 | path: qrId | [~] 缺 short_domains/entry/landing 种子数据，成功路径未验；校验链与不存在 id 的 404 均正确（2026-10-27 复核）| `src/api/livecode.js` | 36 |
| 644 | [x] | POST | `/api/live-codes/{id}/share` | 用户端 | path: id / body: data | [~] 缺 short_domains/entry/landing 种子数据，成功路径未验；校验链与不存在 id 的 404 均正确（2026-10-27 复核）| `src/api/livecode.js` | 40 |
| 645 | [x] | DELETE | `/api/live-codes/qrcodes/{id}/delete` | 用户端 | path: id | [~] 缺 short_domains/entry/landing 种子数据，成功路径未验；校验链与不存在 id 的 404 均正确（2026-10-27 复核）| `src/api/livecode.js` | 44 |
| 646 | [x] | PUT | `/api/live-codes/qrcodes/{id}/update` | 用户端 | path: id / body: data | [~] 缺 short_domains/entry/landing 种子数据，成功路径未验；校验链与不存在 id 的 404 均正确（2026-10-27 复核）| `src/api/livecode.js` | 48 |
| 647 | [x] | GET | `/api/reports/llm/overview` | 用户端 | query: params | 200 code=0 | `src/api/llmReport.js` | 7 |
| 648 | [x] | GET | `/api/reports/llm/timeseries` | 用户端 | query: params | 200 code=0 | `src/api/llmReport.js` | 11 |
| 649 | [x] | GET | `/api/reports/llm/breakdown` | 用户端 | query: params | 200 code=0（需 by 参数） | `src/api/llmReport.js` | 15 |
| 650 | [x] | GET | `/api/reports/llm/errors` | 用户端 | query: params | 200 code=0 | `src/api/llmReport.js` | 19 |
| 651 | [x] | GET | `/api/llm/usage/daily` | 用户端 | body: { window, group_by: groupBy } | 200 code=0（两条腿均通过） | `src/api/llmRouting.js` | 7 |
| 652 | [x] | GET | `/api/llm/models` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/llmRouting.js` | 11 |
| 653 | [x] | GET | `/api/llm/models/{name}` | 用户端 | path: name | 200 code=0（两条腿均通过） | `src/api/llmRouting.js` | 15 |
| 654 | [x] | POST | `/api/llm/models` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/llmRouting.js` | 19 |
| 655 | [x] | PUT | `/api/llm/models/{name}` | 用户端 | path: name / body: data | 200 code=0（两条腿均通过） | `src/api/llmRouting.js` | 23 |
| 656 | [x] | DELETE | `/api/llm/models/{name}` | 用户端 | path: name | 200 code=0（两条腿均通过） | `src/api/llmRouting.js` | 27 |
| 657 | [x] | PUT | `/api/llm/models/{name}` | 用户端 | path: name / body: { enabled: status } | 200 code=0（两条腿均通过） | `src/api/llmRouting.js` | 31 |
| 658 | [x] | POST | `/api/llm/models/{name}/test` | 用户端 | path: name / body: body | 200 code=0（模型连通性测试） | `src/api/llmRouting.js` | 35 |
| 659 | [x] | GET | `/api/llm/scene-routing` | 用户端 | - | GET 200 code=0；PUT 空 body 400（需完整场景映射） | `src/api/llmRouting.js` | 39 |
| 660 | [x] | GET | `/api/llm/scenarios` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/llmRouting.js` | 43 |
| 661 | [x] | PUT | `/api/llm/scene-routing` | 用户端 | body: payload | GET 200 code=0；PUT 空 body 400（需完整场景映射）（2026-10-27 复核）| `src/api/llmRouting.js` | 48 |
| 662 | [x] | GET | `/api/llm/fallback` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/llmRouting.js` | 52 |
| 663 | [x] | GET | `/api/llm/audit` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/llmRouting.js` | 58 |
| 664 | [x] | GET | `/api/llm/stats` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/llmRouting.js` | 62 |
| 665 | [x] | GET | `/api/llm/usage` | 用户端 | body: { window } | 200 code=0（两条腿均通过） | `src/api/llmRouting.js` | 66 |
| 666 | [x] | GET | `/api/llm/cost-stats` | 用户端 | body: { window } | 200 code=0（两条腿均通过） | `src/api/llmRouting.js` | 70 |
| 667 | [x] | GET | `/api/llm/health` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/llmRouting.js` | 74 |
| 668 | [x] | GET | `/api/llm/model-type-stats` | 用户端 | body: { window } | 200 code=0（两条腿均通过） | `src/api/llmRouting.js` | 78 |
| 669 | [x] | GET | `/api/llm/egress-audit` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/llmRouting.js` | 82 |
| 670 | [x] | GET | `/api/llm/egress-alerts` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/llmRouting.js` | 86 |
| 671 | [x] | GET | `/api/manage/ltc/config` | 管理端 | - | GET/PUT 200 code=0（PUT 须带 enabled+stages_enabled+thresholds） | `src/api/ltcConfig.js` | 11 |
| 672 | [x] | PUT | `/api/manage/ltc/config` | 管理端 | body: cfg | GET/PUT 200 code=0（PUT 须带 enabled+stages_enabled+thresholds） | `src/api/ltcConfig.js` | 21 |
| 673 | [x] | GET | `/api/ltc-rates` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/ltcRates.js` | 5 |
| 674 | [x] | GET | `/api/marketing-flows` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/marketingFlow.js` | 4 |
| 675 | [x] | GET | `/api/marketing-flows/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/marketingFlow.js` | 7 |
| 676 | [x] | POST | `/api/marketing-flows` | 用户端 | body: data | [~] 依赖创建；创建回通用 500 文案（缺原因），未验（2026-10-27 复核）| `src/api/marketingFlow.js` | 10 |
| 677 | [x] | PUT | `/api/marketing-flows/{id}` | 用户端 | path: id / body: data | [~] 依赖创建；创建回通用 500 文案（缺原因），未验（2026-10-27 复核）| `src/api/marketingFlow.js` | 13 |
| 678 | [x] | DELETE | `/api/marketing-flows/{id}` | 用户端 | path: id | [~] 依赖创建；创建回通用 500 文案（缺原因），未验（2026-10-27 复核）| `src/api/marketingFlow.js` | 16 |
| 679 | [x] | POST | `/api/marketing-flows/{id}/activate` | 用户端 | path: id | [~] 依赖创建；创建回通用 500 文案（缺原因），未验（2026-10-27 复核）| `src/api/marketingFlow.js` | 19 |
| 680 | [x] | POST | `/api/marketing-flows/{id}/pause` | 用户端 | path: id | [~] 依赖创建；创建回通用 500 文案（缺原因），未验（2026-10-27 复核）| `src/api/marketingFlow.js` | 22 |
| 681 | [x] | POST | `/api/marketing-flows/{id}/stop` | 用户端 | path: id | [~] 依赖创建；创建回通用 500 文案（缺原因），未验（2026-10-27 复核）| `src/api/marketingFlow.js` | 25 |
| 682 | [x] | GET | `/api/marketing-flows/{id}/executions` | 用户端 | path: id | [~] 依赖创建；创建回通用 500 文案（缺原因），未验（2026-10-27 复核）| `src/api/marketingFlow.js` | 28 |
| 683 | [x] | GET | `/api/marketing-flows/{id}/stats` | 用户端 | path: id | [~] 依赖创建；创建回通用 500 文案（缺原因），未验（2026-10-27 复核）| `src/api/marketingFlow.js` | 31 |
| 684 | [x] | GET | `/api/material/list` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/material.js` | 4 |
| 685 | [x] | UPLOAD | `/api/material/upload` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/material.js` | 8 |
| 686 | [x] | DELETE | `/api/material/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/material.js` | 12 |
| 687 | [x] | GET | `/api/material/categories` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/material.js` | 16 |
| 688 | [x] | POST | `/api/material/categories` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/material.js` | 20 |
| 689 | [x] | PUT | `/api/material/categories/{id}` | 用户端 | path: id / body: data | 200 code=0（两条腿均通过） | `src/api/material.js` | 24 |
| 690 | [x] | DELETE | `/api/material/categories/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/material.js` | 28 |
| 691 | [x] | GET | `/api/material/selector` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/material.js` | 32 |
| 692 | [x] | POST | `/api/material/{id}/usage` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/material.js` | 36 |
| 693 | [x] | GET | `/api/material/stats` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/material.js` | 40 |
| 694 | [x] | GET | `/api/mcp/credentials` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/mcpCredential.js` | 5 |
| 695 | [x] | POST | `/api/mcp/credentials` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/mcpCredential.js` | 6 |
| 696 | [x] | PUT | `/api/mcp/credentials/{id}` | 用户端 | path: id / body: data | 200 code=0（两条腿均通过） | `src/api/mcpCredential.js` | 7 |
| 697 | [x] | DELETE | `/api/mcp/credentials/{id}` | 用户端 | path: id | DELETE 不存在 id 回 500「record not found」（应 404）→ A16 | `src/api/mcpCredential.js` | 8 |
| 698 | [x] | GET | `/api/menus` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/menu.js` | 12 |
| 699 | [x] | GET | `/api/system/menus` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/menu.js` | 17 |
| 700 | [x] | GET | `/api/system/roles/{code}/menus` | 用户端 | path: code | 200 code=0（两条腿均通过） | `src/api/menu.js` | 22 |
| 701 | [x] | PUT | `/api/system/roles/{code}/menus` | 用户端 | path: code / body: { menu_keys: menuKeys } | 200 code=0（admin 豁免，用 staff 验证；须 menu_keys） | `src/api/menu.js` | 27 |
| 702 | [x] | POST | `/api/message-hub/push` | 用户端 | body: data | [~] 成功路径未验（platform 枚举/订单号缺失）（2026-10-27 复核）| `src/api/messageHub.js` | 5 |
| 703 | [x] | POST | `/api/message-hub/push-batch` | 用户端 | body: data | 200 code=0（body 为 JSON 数组） | `src/api/messageHub.js` | 9 |
| 704 | [x] | POST | `/api/message-hub/push-from-channel` | 用户端 | body: data | [~] 成功路径未验（platform 枚举/订单号缺失）（2026-10-27 复核）| `src/api/messageHub.js` | 13 |
| 705 | [x] | GET | `/api/message-hub/list` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/messageHub.js` | 17 |
| 706 | [x] | GET | `/api/message-hub/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/messageHub.js` | 21 |
| 707 | [x] | POST | `/api/message-hub/{ids[0]}/read` | 用户端 | path: ids[0] / body: { ids } | 200 code=0（两条腿均通过） | `src/api/messageHub.js` | 25 |
| 708 | [x] | GET | `/api/message-hub/stats` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/messageHub.js` | 29 |
| 709 | [x] | GET | `/api/message-hub/platforms` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/messageHub.js` | 33 |
| 710 | [x] | GET | `/api/module-market/list` | 公开 | query: params / body: config | 200 code=0（两条腿均通过） | `src/api/moduleMarket.js` | 17 |
| 711 | [x] | GET | `/api/module-market/detail/{key}` | 公开 | path: key | 200 code=0（两条腿均通过） | `src/api/moduleMarket.js` | 20 |
| 712 | [x] | GET | `/api/module-market/payment-guide` | 公开 | body: undefined; config | 200 code=0（两条腿均通过） | `src/api/moduleMarket.js` | 23 |
| 713 | [x] | GET | `/api/tenant/regulatory-notice` | 公开 | - | 200 code=0（两条腿均通过） | `src/api/moduleMarket.js` | 26 |
| 714 | [x] | POST | `/api/module-market/order` | 公开 | body: data | [~] 成功路径未验（platform 枚举/订单号缺失）（2026-10-27 复核）| `src/api/moduleMarket.js` | 31 |
| 715 | [x] | GET | `/api/module-market/orders` | 公开 | query: params / body: config | 200 code=0（两条腿均通过） | `src/api/moduleMarket.js` | 36 |
| 716 | [x] | GET | `/api/module-market/orders/{orderNo}` | 公开 | path: orderNo | [~] 成功路径未验（platform 枚举/订单号缺失）（2026-10-27 复核）| `src/api/moduleMarket.js` | 39 |
| 717 | [x] | GET | `/api/module-market/orders/{orderNo}/pay-status` | 公开 | path: orderNo / body: undefined; config | [~] 成功路径未验（platform 枚举/订单号缺失）（2026-10-27 复核）| `src/api/moduleMarket.js` | 43 |
| 718 | [x] | POST | `/api/module-market/orders/{orderNo}/pay` | 公开 | path: orderNo | 200 code=0（两条腿均通过） | `src/api/moduleMarket.js` | 48 |
| 719 | [x] | GET | `/api/subscription/status` | 用户端 | body: undefined; config | 200 code=0（两条腿均通过） | `src/api/moduleMarket.js` | 56 |
| 720 | [x] | POST | `/api/subscription/renew` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/moduleMarket.js` | 59 |
| 721 | [x] | GET | `/api/modules/my` | 用户端 | - | SaaS 200；off 回 4301 未开通（见 A17） | `src/api/moduleMarket.js` | 64 |
| 722 | [x] | POST | `/api/modules/sync` | 用户端 | body: undefined; config | SaaS 200；off 回 4301 未开通（见 A17） | `src/api/moduleMarket.js` | 67 |
| 723 | [x] | GET | `/api/modules/sync-logs` | 用户端 | query: params / body: config | SaaS 200；off 回 4301 未开通（见 A17） | `src/api/moduleMarket.js` | 70 |
| 724 | [x] | GET | `/api/llm/wallet` | 用户端 | body: undefined; config | 200 code=0（两条腿均通过） | `src/api/moduleMarket.js` | 78 |
| 725 | [x] | GET | `/api/llm/plans` | 用户端 | body: undefined; config | 200 code=0（两条腿均通过） | `src/api/moduleMarket.js` | 81 |
| 726 | [x] | POST | `/api/llm/recharge` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/moduleMarket.js` | 84 |
| 727 | [x] | GET | `/api/monitor/trace-tree` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/monitor.js` | 5 |
| 728 | [x] | GET | `/api/monitor/traces` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/monitor.js` | 8 |
| 729 | [x] | GET | `/api/monitor/lifecycle` | 用户端 | query: params | 200 code=0（须 conversation_id 或 trace_id） | `src/api/monitor.js` | 11 |
| 730 | [x] | GET | `/api/monitor/health` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/monitor.js` | 14 |
| 731 | [x] | GET | `/api/monitor/node-health` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/monitor.js` | 17 |
| 732 | [x] | GET | `/api/monitor/latency` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/monitor.js` | 20 |
| 733 | [x] | GET | `/api/monitor/anomalies` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/monitor.js` | 23 |
| 734 | [x] | GET | `/api/monitor/trace-eval/logs` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/monitor.js` | 26 |
| 735 | [x] | GET | `/api/monitor/knowledge-weights` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/monitor.js` | 29 |
| 736 | [x] | POST | `/api/monitor/trace-eval/trigger` | 用户端 | query: params / body: { params } | 200 code=0（两条腿均通过） | `src/api/monitor.js` | 32 |
| 737 | [x] | GET | `/api/rag/recall/snapshot` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/monitor.js` | 36 |
| 738 | [x] | GET | `/api/rag/recall/snapshots` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/monitor.js` | 39 |
| 739 | [x] | POST | `/api/rag/recall/collect` | 用户端 | body: params | 200 code=0（两条腿均通过） | `src/api/monitor.js` | 42 |
| 740 | [x] | POST | `/api/rag/recall/start` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/monitor.js` | 45 |
| 741 | [x] | POST | `/api/rag/recall/stop` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/monitor.js` | 48 |
| 742 | [x] | GET | `/api/rag/recall/metrics` | 用户端 | query: params | 200 code=0（须 start/end RFC3339） | `src/api/monitor.js` | 51 |
| 743 | [x] | GET | `/api/rag/recall/low-recall` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/monitor.js` | 54 |
| 744 | [x] | GET | `/api/auth/notifications` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/notification.js` | 9 |
| 745 | [x] | POST | `/api/auth/notifications/{id}/read` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/notification.js` | 11 |
| 746 | [x] | POST | `/api/auth/notifications/read-all` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/notification.js` | 13 |
| 747 | [x] | GET | `/api/auth/notifications/unread-count` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/notification.js` | 15 |
| 748 | [x] | POST | `/api/objection/handle` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/objection.js` | 4 |
| 749 | [x] | POST | `/api/objection/classify` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/objection.js` | 8 |
| 750 | [x] | GET | `/api/objection/categories` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/objection.js` | 12 |
| 751 | [x] | POST | `/api/objection/usage` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/objection.js` | 16 |
| 752 | [x] | GET | `/api/obs/config` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/obs.js` | 4 |
| 753 | [x] | GET | `/api/obs/config/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/obs.js` | 8 |
| 754 | [x] | POST | `/api/obs/config` | 用户端 | body: data | ⚠️ [~] 创建仅回「INVALID_PARAM」，未给字段原因；payload 形状待查（A18）（2026-10-27 复核）| `src/api/obs.js` | 12 |
| 755 | [x] | PUT | `/api/obs/config/{id}` | 用户端 | path: id / body: data | 200 code=0（两条腿均通过） | `src/api/obs.js` | 16 |
| 756 | [x] | DELETE | `/api/obs/config/{id}` | 用户端 | path: id | ⚠️ [~] 未删除既有真实配置（避免破坏），路径/权限未验（2026-10-27 复核）| `src/api/obs.js` | 20 |
| 757 | [x] | POST | `/api/obs/config/{id}/test` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/obs.js` | 24 |
| 758 | [x] | POST | `/api/obs/config/{id}/default` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/obs.js` | 28 |
| 759 | [x] | GET | `/api/obs/config/default` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/obs.js` | 32 |
| 760 | [x] | GET | `/api/observability/dashboard` | 用户端 | body: { window } | 200 code=0（两条腿均通过） | `src/api/observabilityDashboard.js` | 9 |
| 761 | [x] | GET | `/api/customer/oneid/list` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/oneid.js` | 4 |
| 762 | [x] | GET | `/api/customer/oneid/conflicts` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/oneid.js` | 8 |
| 763 | [x] | POST | `/api/customer/oneid/merge` | 用户端 | body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/oneid.js` | 12 |
| 764 | [x] | POST | `/api/customer/oneid/conflicts/{id}/resolve` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/oneid.js` | 16 |
| 765 | [x] | GET | `/api/customer-oneid/{customerId}/identities` | 用户端 | path: customerId | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/oneid.js` | 20 |
| 766 | [x] | POST | `/api/customer-oneid/{customerId}/identities` | 用户端 | path: customerId / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/oneid.js` | 24 |
| 767 | [x] | POST | `/api/customer/oneid/resolve` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/oneid.js` | 28 |
| 768 | [x] | GET | `/api/customer/oneid/stats` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/oneid.js` | 32 |
| 769 | [x] | GET | `/api/oneid/merge-rules` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/oneid.js` | 36 |
| 770 | [x] | POST | `/api/oneid/merge-rules` | 用户端 | body: data | ⚠️ [~] 400「保存合并规则失败」（通用文案，缺原因）（2026-10-27 复核）| `src/api/oneid.js` | 40 |
| 771 | [x] | GET | `/api/operation-logs` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/operationLog.js` | 4 |
| 772 | [x] | GET | `/api/operation-logs/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/operationLog.js` | 7 |
| 773 | [x] | GET | `/api/operation-logs/statistics` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/operationLog.js` | 10 |
| 774 | [x] | GET | `/api/operation-logs/export` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/operationLog.js` | 13 |
| 775 | [x] | POST | `/api/operation-logs/clean` | 用户端 | body: { beforeDate } | 200 code=0（两条腿均通过） | `src/api/operationLog.js` | 16 |
| 776 | [x] | GET | `/api/manage/order-drafts` | 管理端 | query: params / body: SILENT | 200 code=SERVICE_UNAVAILABLE_6003（FF_LTC_ORDER_DRAFT_DB=off，设计内） | `src/api/orderDraft.js` | 32 |
| 777 | [x] | GET | `/api/manage/order-drafts/{id}` | 管理端 | path: id / body: undefined; SILENT | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/orderDraft.js` | 36 |
| 778 | [x] | PATCH | `/api/manage/order-drafts/{id}` | 管理端 | path: id / body: updates | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/orderDraft.js` | 43 |
| 779 | [x] | POST | `/api/manage/order-drafts/{id}/confirm` | 管理端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/orderDraft.js` | 50 |
| 780 | [x] | POST | `/api/manage/order-drafts/{id}/cancel` | 管理端 | path: id / body: { reason } | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/orderDraft.js` | 55 |
| 781 | [x] | GET | `/api/agent/order-drafts/stats` | 用户端 | body: undefined; SILENT | 200 code=0（两条腿均通过） | `src/api/orderDraft.js` | 62 |
| 782 | [x] | PUT | `/api/system/permissions/{id}/enabled` | 用户端 | path: id / body: { enabled } | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/permission.js` | 4 |
| 783 | [x] | PUT | `/api/system/permissions/{id}/password` | 用户端 | path: id / body: { password } | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/permission.js` | 8 |
| 784 | [x] | GET | `/api/system/permissions/audit-logs` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/permission.js` | 12 |
| 785 | [x] | GET | `/api/analytics/persona/staffs` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/persona.js` | 4 |
| 786 | [x] | GET | `/api/analytics/persona/staffs/{staffId}` | 用户端 | path: staffId | 200 code=0（两条腿均通过） | `src/api/persona.js` | 8 |
| 787 | [x] | GET | `/api/platform/message/latest` | 管理端 | - | 200 code=0（两条腿均通过） | `src/api/platform.js` | 5 |
| 788 | [x] | POST | `/api/platform/message/{messageId}/read` | 管理端 | path: messageId | ⚠️ [~] 未取到消息 id，已读未验（2026-10-27 复核）| `src/api/platform.js` | 9 |
| 789 | [x] | GET | `/api/platform-accounts` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/platformAccount.js` | 5 |
| 790 | [x] | GET | `/api/platform-accounts/platforms` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/platformAccount.js` | 9 |
| 791 | [x] | GET | `/api/platform-accounts/{id}` | 用户端 | path: id | ⚠️ [~] id=2 不存在，换真实 id 未验（2026-10-27 复核）| `src/api/platformAccount.js` | 13 |
| 792 | [x] | POST | `/api/platform-accounts` | 用户端 | body: data | ⚠️ [~] id=2 不存在，换真实 id 未验（2026-10-27 复核）| `src/api/platformAccount.js` | 17 |
| 793 | [x] | PUT | `/api/platform-accounts/{id}` | 用户端 | path: id / body: data | ⚠️ [~] id=2 不存在，换真实 id 未验（2026-10-27 复核）| `src/api/platformAccount.js` | 21 |
| 794 | [x] | DELETE | `/api/platform-accounts/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/platformAccount.js` | 25 |
| 795 | [x] | POST | `/api/platform-accounts/{id}/login` | 用户端 | path: id / body: data | 200 code=0（两条腿均通过） | `src/api/platformAccount.js` | 29 |
| 796 | [x] | GET | `/api/platform-accounts/{id}/status` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/platformAccount.js` | 33 |
| 797 | [x] | GET | `/api/qq/accounts` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/qqBot.js` | 4 |
| 798 | [x] | GET | `/api/qq/accounts/{id}` | 用户端 | path: id | 200 code=0（SaaS 通过；off 回 404「账号不存在」→ A19 待判）（2026-10-27 复核）| `src/api/qqBot.js` | 8 |
| 799 | [x] | POST | `/api/qq/accounts` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/qqBot.js` | 12 |
| 800 | [x] | PUT | `/api/qq/accounts/{id}` | 用户端 | path: id / body: data | 200 code=0（两条腿均通过） | `src/api/qqBot.js` | 16 |
| 801 | [x] | DELETE | `/api/qq/accounts/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/qqBot.js` | 20 |
| 802 | [x] | POST | `/api/qq/accounts/{id}/test-send` | 用户端 | path: id / body: data | ⚠️ [~] 依赖真实 QQ 凭据/callback，未验（2026-10-27 复核）| `src/api/qqBot.js` | 24 |
| 803 | [x] | POST | `/api/qq/accounts/{id}/verify-callback` | 用户端 | path: id / body: {} | ⚠️ [~] 依赖真实 QQ 凭据/callback，未验（2026-10-27 复核）| `src/api/qqBot.js` | 28 |
| 804 | [x] | POST | `/api/rag-config/products` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/ragProductConfig.js` | 12 |
| 805 | [x] | GET | `/api/rag-config/products` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/ragProductConfig.js` | 21 |
| 806 | [x] | GET | `/api/rag-config/products` | 用户端 | query: params | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/ragProductConfig.js` | 25 |
| 807 | [x] | PUT | `/api/rag-config/products/{id}` | 用户端 | path: id / body: data | ⚠️ [~] 空 body 400（platform 枚举 douyin/kuaishou/xiaohongshu/xianyu）（2026-10-27 复核）| `src/api/ragProductConfig.js` | 29 |
| 808 | [x] | DELETE | `/api/rag-config/products/{id}` | 用户端 | path: id | ⚠️ [~] 缺 platform/account_id 等必填，成功路径未验（2026-10-27 复核）| `src/api/ragProductConfig.js` | 33 |
| 809 | [x] | GET | `/api/rag-config/accounts/config` | 用户端 | query: params | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/ragProductConfig.js` | 37 |
| 810 | [x] | PUT | `/api/rag-config/accounts/config` | 用户端 | body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/ragProductConfig.js` | 41 |
| 811 | [x] | POST | `/api/rag-config/process-message` | 用户端 | body: data | ⚠️ [~] 缺 platform/account_id 等必填，成功路径未验（2026-10-27 复核）| `src/api/ragProductConfig.js` | 45 |
| 812 | [x] | GET | `/api/reach/pipelines` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/reachPipeline.js` | 5 |
| 813 | [x] | POST | `/api/reach/pipelines` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/reachPipeline.js` | 8 |
| 814 | [x] | GET | `/api/reach/pipelines/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/reachPipeline.js` | 11 |
| 815 | [x] | PUT | `/api/reach/pipelines/{id}` | 用户端 | path: id / body: data | 400 channel 必填（校验正确，其余路径未验）（2026-10-27 复核）| `src/api/reachPipeline.js` | 14 |
| 816 | [x] | DELETE | `/api/reach/pipelines/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/reachPipeline.js` | 17 |
| 817 | [x] | POST | `/api/reach/pipelines/{id}/pause` | 用户端 | path: id / body: {} | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/reachPipeline.js` | 20 |
| 818 | [x] | POST | `/api/reach/pipelines/{id}/resume` | 用户端 | path: id / body: {} | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/reachPipeline.js` | 23 |
| 819 | [x] | POST | `/api/reach/pipelines/{id}/archive` | 用户端 | path: id / body: {} | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/reachPipeline.js` | 26 |
| 820 | [x] | GET | `/api/reach/jobs` | 用户端 | query: params | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/reachPipeline.js` | 30 |
| 821 | [x] | POST | `/api/reach/jobs` | 用户端 | body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/reachPipeline.js` | 33 |
| 822 | [x] | GET | `/api/reach/jobs/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/reachPipeline.js` | 36 |
| 823 | [x] | POST | `/api/reach/jobs/{id}/cancel` | 用户端 | path: id / body: {} | ⚠️ [~] 需 channel+完整渠道配置，成功路径未验（2026-10-27 复核）| `src/api/reachPipeline.js` | 39 |
| 824 | [x] | POST | `/api/reach/jobs/{id}/retry` | 用户端 | path: id / body: {} | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/reachPipeline.js` | 42 |
| 825 | [x] | POST | `/api/reach/jobs/{id}/execute` | 用户端 | path: id / body: {} | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/reachPipeline.js` | 45 |
| 826 | [x] | GET | `/api/reach/stats` | 用户端 | query: params | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/reachPipeline.js` | 49 |
| 827 | [x] | POST | `/api/reach/rate-limit/reset` | 用户端 | query: channel / body: {}; { params: { channel } } | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/reachPipeline.js` | 52 |
| 828 | [x] | GET | `/api/system/roles` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/role.js` | 10 |
| 829 | [x] | GET | `/api/system/roles/{code}` | 用户端 | path: code | 200 code=0（两条腿均通过） | `src/api/role.js` | 15 |
| 830 | [x] | GET | `/api/system/roles/{code}/members` | 用户端 | path: code / query: params | 200 code=0（两条腿均通过） | `src/api/role.js` | 22 |
| 831 | [x] | GET | `/api/config/runtime` | 用户端 | query: groups | 200 code=0（两条腿均通过） | `src/api/runtimeConfig.js` | 13 |
| 832 | [x] | GET | `/api/config/dictionaries/{parent}` | 用户端 | path: parent | 200 code=0（两条腿均通过） | `src/api/runtimeConfig.js` | 23 |
| 833 | [x] | GET | `/api/manage/dictionaries` | 管理端 | - | 200 code=0（两条腿均通过） | `src/api/runtimeConfig.js` | 28 |
| 834 | [x] | GET | `/api/manage/dictionaries/{parent}` | 管理端 | path: parent | 200 code=0（两条腿均通过） | `src/api/runtimeConfig.js` | 29 |
| 835 | [x] | PUT | `/api/manage/dictionaries/{parent}/items` | 管理端 | path: parent / body: item | 404「字典域不存在」（用真实 parent 未复验）（2026-10-27 复核）| `src/api/runtimeConfig.js` | 30 |
| 836 | [x] | PUT | `/api/manage/dictionaries/{parent}/transitions` | 管理端 | path: parent / body: { transitions } | 404「字典域不存在」（用真实 parent 未复验）（2026-10-27 复核）| `src/api/runtimeConfig.js` | 32 |
| 837 | [x] | GET | `/api/sales-workbench/overview` | 用户端 | body: { sales_id: salesId } | ⚠️ [~] overview 需时间/人员参数（2026-10-27 复核）| `src/api/salesWorkbench.js` | 24 |
| 838 | [x] | GET | `/api/sales-workbench/team-dashboard` | 用户端 | body: { days } | 200 code=0（两条腿均通过） | `src/api/salesWorkbench.js` | 30 |
| 839 | [x] | GET | `/api/sales-workbench/champion` | 用户端 | body: { days } | 200 code=0（两条腿均通过） | `src/api/salesWorkbench.js` | 37 |
| 840 | [x] | GET | `/api/sales-workbench/quick-actions` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/salesWorkbench.js` | 43 |
| 841 | [x] | GET | `/api/script-library/{scriptId}/versions` | 用户端 | path: scriptId | 200 code=0（两条腿均通过） | `src/api/scriptAb.js` | 4 |
| 842 | [x] | POST | `/api/script-library/{scriptId}/versions` | 用户端 | path: scriptId / body: data | 200 code=0（两条腿均通过） | `src/api/scriptAb.js` | 5 |
| 843 | [x] | PUT | `/api/script-library/{scriptId}/versions/{versionId}/activate` | 用户端 | path: scriptId, versionId | 200 code=0（两条腿均通过） | `src/api/scriptAb.js` | 7 |
| 844 | [x] | POST | `/api/script-library/{scriptId}/expire` | 用户端 | path: scriptId / body: data | 200 code=0 已下线（测试对象 script 4，已在隔离库） | `src/api/scriptAb.js` | 8 |
| 845 | [x] | GET | `/api/script-library/{scriptId}/ab-stats` | 用户端 | path: scriptId / query: params | 200 code=0（两条腿均通过） | `src/api/scriptAb.js` | 9 |
| 846 | [x] | PUT | `/api/script-library/{scriptId}/ab-config` | 用户端 | path: scriptId / body: data | 400「split_a 必须在 (0,100)」（校验正确） | `src/api/scriptAb.js` | 10 |
| 847 | [x] | POST | `/api/script-ab/conversion` | 用户端 | body: data | 400 缺 one_id（校验正确） | `src/api/scriptAb.js` | 11 |
| 848 | [x] | GET | `/api/scripts` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/scriptTemplate.js` | 4 |
| 849 | [x] | GET | `/api/scripts/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/scriptTemplate.js` | 7 |
| 850 | [x] | POST | `/api/scripts` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/scriptTemplate.js` | 10 |
| 851 | [x] | PUT | `/api/scripts/{id}` | 用户端 | path: id / body: data | 200 code=0（两条腿均通过） | `src/api/scriptTemplate.js` | 13 |
| 852 | [x] | DELETE | `/api/scripts/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/scriptTemplate.js` | 16 |
| 853 | [x] | GET | `/api/scripts/categories` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/scriptTemplate.js` | 19 |
| 854 | [x] | GET | `/api/scripts/search` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/scriptTemplate.js` | 22 |
| 855 | [x] | GET | `/api/scripts/public` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/scriptTemplate.js` | 25 |
| 856 | [x] | POST | `/api/scripts/recommend` | 用户端 | body: data | 400 缺 message（校验正确） | `src/api/scriptTemplate.js` | 28 |
| 857 | [x] | POST | `/api/security/audit` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/securityAudit.js` | 4 |
| 858 | [x] | GET | `/api/security/audit/list` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/securityAudit.js` | 8 |
| 859 | [x] | GET | `/api/security/audit/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/securityAudit.js` | 12 |
| 860 | [x] | GET | `/api/shortlink/list` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/shortLink.js` | 5 |
| 861 | [x] | GET | `/api/shortlink/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/shortLink.js` | 9 |
| 862 | [x] | POST | `/api/shortlink/create` | 用户端 | body: data | 400 缺 short_code（校验正确） | `src/api/shortLink.js` | 13 |
| 863 | [x] | PUT | `/api/shortlink/update` | 用户端 | body: data | 400 缺 id（校验正确） | `src/api/shortLink.js` | 17 |
| 864 | [x] | DELETE | `/api/shortlink/delete/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/shortLink.js` | 21 |
| 865 | [x] | POST | `/api/shortlink/access` | 用户端 | body: data | 400 缺 short_code（校验正确） | `src/api/shortLink.js` | 25 |
| 866 | [x] | POST | `/api/shortlink/generate` | 用户端 | body: data | 400 缺 short_code（校验正确） | `src/api/shortLink.js` | 29 |
| 867 | [x] | GET | `/api/shortlink/{id}/stats` | 用户端 | path: id / query: params | 200 code=0（两条腿均通过） | `src/api/shortLink.js` | 33 |
| 868 | [x] | GET | `/api/shortlink/stats/all` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/shortLink.js` | 37 |
| 869 | [x] | POST | `/api/shortlink/{id}/share` | 用户端 | path: id / body: data | 200 code=0（两条腿均通过） | `src/api/shortLink.js` | 41 |
| 870 | [x] | GET | `/api/sms/config` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/sms.js` | 5 |
| 871 | [x] | POST | `/api/sms/config` | 用户端 | body: data | 400 精确列出 default_provider/rate_limit/daily_limit（校验正确，未保存） | `src/api/sms.js` | 8 |
| 872 | [x] | GET | `/api/sms/list` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/sms.js` | 12 |
| 873 | [x] | GET | `/api/sms/detail/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/sms.js` | 15 |
| 874 | [x] | POST | `/api/sms/resend/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/sms.js` | 18 |
| 875 | [x] | POST | `/api/sms/send` | 用户端 | body: data | 400 精确列出 phone/content（校验正确，未真实外发） | `src/api/sms.js` | 21 |
| 876 | [x] | GET | `/api/sms/draft/list` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/sms.js` | 25 |
| 877 | [x] | GET | `/api/sms/draft/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/sms.js` | 28 |
| 878 | [x] | POST | `/api/sms/draft` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/sms.js` | 31 |
| 879 | [x] | PUT | `/api/sms/draft/{id}` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/sms.js` | 34 |
| 880 | [x] | DELETE | `/api/sms/draft/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/sms.js` | 37 |
| 881 | [x] | POST | `/api/sms/draft/{id}/send` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/sms.js` | 40 |
| 882 | [x] | GET | `/api/sms/job/list` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/sms.js` | 44 |
| 883 | [x] | GET | `/api/sms/job/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/sms.js` | 47 |
| 884 | [x] | POST | `/api/sms/job/{id}/pause` | 用户端 | path: id | 404「记录不存在」（业务正确） | `src/api/sms.js` | 50 |
| 885 | [x] | POST | `/api/sms/job/{id}/resume` | 用户端 | path: id | 404「记录不存在」（业务正确） | `src/api/sms.js` | 53 |
| 886 | [x] | POST | `/api/sms/job/{id}/stop` | 用户端 | path: id | 404「记录不存在」（业务正确） | `src/api/sms.js` | 56 |
| 887 | [x] | POST | `/api/sms/job` | 用户端 | body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/sms.js` | 59 |
| 888 | [x] | DELETE | `/api/sms/job/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/sms.js` | 62 |
| 889 | [x] | GET | `/api/sms/job/{id}/records` | 用户端 | path: id / query: params | GET 200 code=0（POST 同一路径 405，前端用的是 GET） | `src/api/sms.js` | 65 |
| 890 | [x] | GET | `/api/sop` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/sopAgent.js` | 5 |
| 891 | [x] | POST | `/api/sop` | 用户端 | body: data | 400 精确列出必填字段（校验正确，未造数据） | `src/api/sopAgent.js` | 9 |
| 892 | [x] | GET | `/api/sop/stats` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/sopAgent.js` | 13 |
| 893 | [x] | GET | `/api/sop/match` | 用户端 | query: params | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/sopAgent.js` | 17 |
| 894 | [x] | GET | `/api/sop/executions` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/sopAgent.js` | 21 |
| 895 | [x] | GET | `/api/sop/executions/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/sopAgent.js` | 25 |
| 896 | [x] | POST | `/api/sop/executions/{id}/pause` | 用户端 | path: id | 404「record not found」（不存在执行，业务正确） | `src/api/sopAgent.js` | 29 |
| 897 | [x] | POST | `/api/sop/executions/{id}/resume` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/sopAgent.js` | 33 |
| 898 | [x] | POST | `/api/sop/executions/{id}/cancel` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/sopAgent.js` | 37 |
| 899 | [x] | GET | `/api/sop/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/sopAgent.js` | 41 |
| 900 | [x] | PUT | `/api/sop/{id}` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/sopAgent.js` | 45 |
| 901 | [x] | DELETE | `/api/sop/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/sopAgent.js` | 49 |
| 902 | [x] | POST | `/api/sop/{id}/activate` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/sopAgent.js` | 53 |
| 903 | [x] | POST | `/api/sop/{id}/deactivate` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/sopAgent.js` | 57 |
| 904 | [x] | POST | `/api/sop/execute` | 用户端 | body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/sopAgent.js` | 61 |
| 905 | [x] | POST | `/api/sop/step` | 用户端 | body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/sopAgent.js` | 65 |
| 906 | [x] | GET | `/api/sop-templates` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/sopTemplate.js` | 5 |
| 907 | [x] | GET | `/api/sop-templates/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/sopTemplate.js` | 9 |
| 908 | [x] | POST | `/api/sop-templates` | 用户端 | body: data | 400 精确列出必填字段（校验正确，未造数据） | `src/api/sopTemplate.js` | 13 |
| 909 | [x] | PUT | `/api/sop-templates/{id}` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/sopTemplate.js` | 17 |
| 910 | [x] | PATCH | `/api/sop-templates/{id}/toggle` | 用户端 | path: id / body: { enabled } | 200 code=0（两条腿均通过） | `src/api/sopTemplate.js` | 21 |
| 911 | [x] | DELETE | `/api/sop-templates/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/sopTemplate.js` | 25 |
| 912 | [x] | POST | `/api/sop-templates/match` | 用户端 | body: data | 400 精确列出 intent/agent_id（校验正确） | `src/api/sopTemplate.js` | 29 |
| 913 | [x] | GET | `/api/system/info` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/stats.js` | 7 |
| 914 | [x] | GET | `/api/system/info` | 用户端 | body: config | 200 code=0（两条腿均通过） | `src/api/system.js` | 6 |
| 915 | [x] | GET | `/api/system/config` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/system.js` | 9 |
| 916 | [x] | POST | `/api/system/config` | 用户端 | body: data | 400「请求体不能为空：至少提供一个配置字段」（校验正确） | `src/api/system.js` | 12 |
| 917 | [x] | GET | `/api/system/users` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/systemUser.js` | 4 |
| 918 | [x] | GET | `/api/system/users/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/systemUser.js` | 8 |
| 919 | [x] | POST | `/api/system/users` | 用户端 | body: data | 400「无效的请求参数」（校验正确，但文案未给字段，同类可改进） | `src/api/systemUser.js` | 12 |
| 920 | [x] | PUT | `/api/system/users/{id}` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/systemUser.js` | 16 |
| 921 | [x] | DELETE | `/api/system/users/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/systemUser.js` | 20 |
| 922 | [x] | GET | `/api/session-tags` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/tagSegmentation.js` | 5 |
| 923 | [x] | PUT | `/api/session-tags/{id}` | 用户端 | path: id / body: body | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/tagSegmentation.js` | 12 |
| 924 | [x] | POST | `/api/session-tags` | 用户端 | body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/tagSegmentation.js` | 16 |
| 925 | [x] | DELETE | `/api/session-tags/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/tagSegmentation.js` | 20 |
| 926 | [x] | GET | `/api/customer-360/tag-rules` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/tagSegmentation.js` | 24 |
| 927 | [x] | POST | `/api/customer-360/tag-rules` | 用户端 | body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/tagSegmentation.js` | 28 |
| 928 | [x] | PUT | `/api/customer-360/tag-rules/{id}` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/tagSegmentation.js` | 32 |
| 929 | [x] | DELETE | `/api/customer-360/tag-rules/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/tagSegmentation.js` | 36 |
| 930 | [x] | GET | `/api/user-segment/layers` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/tagSegmentation.js` | 40 |
| 931 | [x] | GET | `/api/customer-360/tag-stats` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/tagSegmentation.js` | 44 |
| 932 | [x] | GET | `/api/telegram/accounts` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/telegram.js` | 4 |
| 933 | [x] | GET | `/api/telegram/accounts/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/telegram.js` | 8 |
| 934 | [x] | POST | `/api/telegram/accounts` | 用户端 | body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/telegram.js` | 12 |
| 935 | [x] | PUT | `/api/telegram/accounts/{id}` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/telegram.js` | 16 |
| 936 | [x] | DELETE | `/api/telegram/accounts/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/telegram.js` | 20 |
| 937 | [x] | POST | `/api/telegram/accounts/{id}/register-webhook` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/telegram.js` | 24 |
| 938 | [x] | POST | `/api/telegram/accounts/{id}/test-send` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/telegram.js` | 28 |
| 939 | [x] | GET | `/api/telegram/gates` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/telegram.js` | 34 |
| 940 | [x] | POST | `/api/telegram/gates` | 用户端 | body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/telegram.js` | 38 |
| 941 | [x] | PUT | `/api/telegram/gates/{id}` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/telegram.js` | 42 |
| 942 | [x] | DELETE | `/api/telegram/gates/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/telegram.js` | 46 |
| 943 | [x] | GET | `/api/telegram/gates/{id}/members` | 用户端 | path: id / query: params | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/telegram.js` | 50 |
| 944 | [x] | POST | `/api/telegram/gates/{id}/members/{memberId}/authorize` | 用户端 | path: id, memberId | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/telegram.js` | 54 |
| 945 | [x] | GET | `/api/template-market/list` | 用户端 | query: params / body: config | 200 code=0（两条腿均通过） | `src/api/templateMarket.js` | 15 |
| 946 | [x] | GET | `/api/template-market/categories` | 用户端 | body: undefined; config | 200 code=0（两条腿均通过） | `src/api/templateMarket.js` | 18 |
| 947 | [x] | GET | `/api/template-market/detail/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/templateMarket.js` | 22 |
| 948 | [x] | GET | `/api/tenant/info` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/tenant.js` | 8 |
| 949 | [x] | GET | `/api/tiktok-card/list` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/tiktokCard.js` | 4 |
| 950 | [x] | GET | `/api/tiktok-card/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/tiktokCard.js` | 8 |
| 951 | [x] | POST | `/api/tiktok-card` | 用户端 | body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/tiktokCard.js` | 12 |
| 952 | [x] | PUT | `/api/tiktok-card/{data_id}` | 用户端 | path: data_id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/tiktokCard.js` | 16 |
| 953 | [x] | DELETE | `/api/tiktok-card/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/tiktokCard.js` | 20 |
| 954 | [x] | POST | `/api/tiktok-card/generate-short-link` | 用户端 | body: { card_id: cardId } | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/tiktokCard.js` | 24 |
| 955 | [x] | GET | `/api/tiktok-card/stats/overall` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/tiktokCard.js` | 28 |
| 956 | [x] | GET | `/api/tiktok-card/{cardId}/stats` | 用户端 | path: cardId / query: params | 200 code=0（两条腿均通过） | `src/api/tiktokCard.js` | 32 |
| 957 | [x] | GET | `/api/admin/tuning/confidence/signals` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/tuning.js` | 4 |
| 958 | [x] | GET | `/api/admin/tuning/confidence/signals/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/tuning.js` | 7 |
| 959 | [x] | GET | `/api/admin/tuning/confidence/signals/stats` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/tuning.js` | 10 |
| 960 | [x] | GET | `/api/admin/tuning/confidence/calibrations` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/tuning.js` | 13 |
| 961 | [x] | GET | `/api/admin/tuning/confidence/policies` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/tuning.js` | 16 |
| 962 | [x] | PUT | `/api/admin/tuning/confidence/policies` | 用户端 | body: data | 400 精确列出 policy_id 等必填（校验正确） | `src/api/tuning.js` | 19 |
| 963 | [x] | GET | `/api/admin/tuning/humanize/scores` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/tuning.js` | 23 |
| 964 | [x] | GET | `/api/admin/tuning/humanize/scores/stats` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/tuning.js` | 26 |
| 965 | [x] | GET | `/api/admin/tuning/humanize/baselines` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/tuning.js` | 29 |
| 966 | [x] | GET | `/api/admin/tuning/humanize/low-quality` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/tuning.js` | 32 |
| 967 | [x] | GET | `/api/admin/tuning/feedback/events` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/tuning.js` | 36 |
| 968 | [x] | GET | `/api/admin/tuning/feedback/events/stats` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/tuning.js` | 39 |
| 969 | [x] | GET | `/api/admin/tuning/feedback/dialogues` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/tuning.js` | 42 |
| 970 | [x] | GET | `/api/admin/tuning/prompt/candidates` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/tuning.js` | 45 |
| 971 | [x] | PUT | `/api/admin/tuning/prompt/candidates/{id}/status` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/tuning.js` | 48 |
| 972 | [x] | GET | `/api/admin/tuning/bandit/arms` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/tuning.js` | 51 |
| 973 | [x] | GET | `/api/unified-messages` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/unifiedMessage.js` | 5 |
| 974 | [x] | GET | `/api/unified-messages/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/unifiedMessage.js` | 9 |
| 975 | [x] | GET | `/api/unknown-questions` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/unknownQuestion.js` | 7 |
| 976 | [x] | GET | `/api/unknown-questions/stats` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/unknownQuestion.js` | 10 |
| 977 | [x] | GET | `/api/unknown-questions/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/unknownQuestion.js` | 13 |
| 978 | [x] | POST | `/api/unknown-questions/{id}/submit` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/unknownQuestion.js` | 16 |
| 979 | [x] | POST | `/api/unknown-questions/{id}/review` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/unknownQuestion.js` | 19 |
| 980 | [x] | GET | `/api/unknown-questions/clusters` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/unknownQuestion.js` | 23 |
| 981 | [x] | GET | `/api/unknown-questions/clusters/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/unknownQuestion.js` | 26 |
| 982 | [x] | POST | `/api/unknown-questions/clusters/{id}/review` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/unknownQuestion.js` | 29 |
| 983 | [x] | GET | `/api/user-segment/rfm/list` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/userSegment.js` | 4 |
| 984 | [x] | GET | `/api/user-segment/rfm/rule` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/userSegment.js` | 7 |
| 985 | [x] | POST | `/api/user-segment/rfm/rule` | 用户端 | body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/userSegment.js` | 10 |
| 986 | [x] | PUT | `/api/user-segment/rfm/rule/{id}` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/userSegment.js` | 13 |
| 987 | [x] | GET | `/api/user-segment/rfm/user?user_id={userId}` | 用户端 | path: userId | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/userSegment.js` | 16 |
| 988 | [x] | GET | `/api/user-segment/rfm/stats` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/userSegment.js` | 19 |
| 989 | [x] | POST | `/api/user-segment/rfm/calculate` | 用户端 | body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/userSegment.js` | 22 |
| 990 | [x] | GET | `/api/user-segment/layers` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/userSegment.js` | 25 |
| 991 | [x] | GET | `/api/user-segments` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/userSegment.js` | 29 |
| 992 | [x] | DELETE | `/api/user-segment/rfm/rule/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/userSegment.js` | 41 |
| 993 | [x] | GET | `/api/user-segment/rfm/list?segment_id={id}` | 用户端 | path: id / query: params | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/userSegment.js` | 44 |
| 994 | [x] | GET | `/api/users` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/users.js` | 5 |
| 995 | [x] | GET | `/api/users/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/users.js` | 9 |
| 996 | [x] | POST | `/api/users` | 用户端 | body: data | 400「无效的请求参数」（校验正确但未给字段，同类可改进）（2026-10-27 复核）| `src/api/users.js` | 13 |
| 997 | [x] | PUT | `/api/users/{id}` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/users.js` | 17 |
| 998 | [x] | DELETE | `/api/users/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/users.js` | 21 |
| 999 | [x] | PUT | `/api/users/{id}/password` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/users.js` | 25 |
| 1000 | [x] | POST | `/api/auth/login` | 公开 | body: data | 200 code=0 登录成功（公开口，两形态一致） | `src/api/users.js` | 29 |
| 1001 | [x] | POST | `/api/auth/mfa/verify` | 公开 | body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/users.js` | 34 |
| 1002 | [x] | GET | `/api/wecom/health/accounts` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/wecomAccount.js` | 5 |
| 1003 | [x] | GET | `/api/wecom/health/accounts/risks` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/wecomAccount.js` | 9 |
| 1004 | [x] | GET | `/api/wecom/health/accounts/select` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/wecomAccount.js` | 13 |
| 1005 | [x] | GET | `/api/wecom/health/accounts/summary` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/wecomAccount.js` | 17 |
| 1006 | [x] | GET | `/api/wecom/health/accounts/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/wecomAccount.js` | 21 |
| 1007 | [x] | GET | `/api/wecom/health/accounts/{id}/history` | 用户端 | path: id / query: params | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/wecomAccount.js` | 25 |
| 1008 | [x] | POST | `/api/wecom/health/accounts/{id}` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/wecomAccount.js` | 29 |
| 1009 | [x] | POST | `/api/wecom/health/accounts/{id}/status` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/wecomAccount.js` | 33 |
| 1010 | [x] | POST | `/api/wecom/health/accounts/{id}/quota/consume` | 用户端 | path: id / body: { count } | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/wecomAccount.js` | 37 |
| 1011 | [x] | POST | `/api/wecom/health/accounts/quota/reset` | 用户端 | - | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/wecomAccount.js` | 41 |
| 1012 | [x] | POST | `/api/wecom/messages/ingest` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/wecomAccount.js` | 45 |
| 1013 | [x] | POST | `/api/wecom/messages/send` | 用户端 | body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/wecomAccount.js` | 49 |
| 1014 | [x] | POST | `/api/wecom/accounts` | 用户端 | body: data | 400 精确列出 corp_id 等必填（校验正确）（2026-10-27 复核）| `src/api/wecomAccount.js` | 53 |
| 1015 | [x] | PUT | `/api/wecom/accounts/{id}` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/wecomAccount.js` | 57 |
| 1016 | [x] | DELETE | `/api/wecom/accounts/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/wecomAccount.js` | 61 |
| 1017 | [x] | GET | `/api/wecom/customers` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/wecomAccount.js` | 65 |
| 1018 | [x] | GET | `/api/wecom/groups` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/wecomAccount.js` | 69 |
| 1019 | [x] | GET | `/api/wecom/tags` | 用户端 | - | 200 code=0（两条腿均通过） | `src/api/wecomAccount.js` | 73 |
| 1020 | [x] | GET | `/api/wecom/messages` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/wecomAccount.js` | 77 |
| 1021 | [x] | POST | `/api/wecom/accounts/{id}/sync-customers` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/wecomAccount.js` | 81 |
| 1022 | [x] | POST | `/api/wecom/accounts/{id}/sync-groups` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/wecomAccount.js` | 85 |
| 1023 | [x] | POST | `/api/wecom/accounts/{id}/sync-tags` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/wecomAccount.js` | 89 |
| 1024 | [x] | POST | `/api/wecom/accounts/{id}/refresh` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/wecomAccount.js` | 93 |
| 1025 | [x] | POST | `/api/wecom/accounts/{id}/send-message` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/wecomAccount.js` | 97 |
| 1026 | [x] | GET | `/api/whatsapp/accounts` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/whatsapp.js` | 3 |
| 1027 | [x] | POST | `/api/whatsapp/accounts` | 用户端 | body: data | 200 创建账号成功（已清理用途仅测试） | `src/api/whatsapp.js` | 5 |
| 1028 | [x] | POST | `/api/whatsapp/accounts/{id}/login/start` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/whatsapp.js` | 6 |
| 1029 | [x] | GET | `/api/whatsapp/accounts/{id}/login/status` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/whatsapp.js` | 7 |
| 1030 | [x] | PUT | `/api/whatsapp/accounts/{id}` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/whatsapp.js` | 8 |
| 1031 | [x] | DELETE | `/api/whatsapp/accounts/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/whatsapp.js` | 9 |
| 1032 | [x] | GET | `/api/whatsapp/drafts` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/whatsapp.js` | 11 |
| 1033 | [x] | POST | `/api/whatsapp/drafts` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/whatsapp.js` | 12 |
| 1034 | [x] | PUT | `/api/whatsapp/drafts/{id}` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/whatsapp.js` | 13 |
| 1035 | [x] | DELETE | `/api/whatsapp/drafts/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/whatsapp.js` | 14 |
| 1036 | [x] | GET | `/api/whatsapp/jobs` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/whatsapp.js` | 16 |
| 1037 | [x] | POST | `/api/whatsapp/jobs` | 用户端 | body: data | 200 code=0（两条腿均通过） | `src/api/whatsapp.js` | 17 |
| 1038 | [x] | GET | `/api/whatsapp/jobs/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/whatsapp.js` | 18 |
| 1039 | [x] | DELETE | `/api/whatsapp/jobs/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/whatsapp.js` | 19 |
| 1040 | [x] | GET | `/api/whatsapp-cloud/accounts` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/whatsappCloud.js` | 6 |
| 1041 | [x] | GET | `/api/whatsapp-cloud/accounts/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/whatsappCloud.js` | 10 |
| 1042 | [x] | POST | `/api/whatsapp-cloud/accounts` | 用户端 | body: data | 400「参数错误」（校验正确但未给字段，同类可改进）（2026-10-27 复核）| `src/api/whatsappCloud.js` | 14 |
| 1043 | [x] | PUT | `/api/whatsapp-cloud/accounts/{id}` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/whatsappCloud.js` | 18 |
| 1044 | [x] | DELETE | `/api/whatsapp-cloud/accounts/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/whatsappCloud.js` | 22 |
| 1045 | [x] | POST | `/api/whatsapp-cloud/accounts/{id}/test-send` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/whatsappCloud.js` | 26 |
| 1046 | [x] | GET | `/api/workflows/versions` | 用户端 | body: {} | 200 code=0（两条腿均通过） | `src/api/workflowOrchestrator.js` | 6 |
| 1047 | [x] | GET | `/api/workflows/versions` | 用户端 | body: { workflow_id: workflowIdOrParams } | 200 code=0（两条腿均通过） | `src/api/workflowOrchestrator.js` | 9 |
| 1048 | [x] | GET | `/api/workflows/versions` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/workflowOrchestrator.js` | 17 |
| 1049 | [x] | GET | `/api/workflows/versions/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/workflowOrchestrator.js` | 21 |
| 1050 | [x] | POST | `/api/workflows/versions` | 用户端 | body: data | 400 精确列出 workflow_id 等必填（校验正确） | `src/api/workflowOrchestrator.js` | 25 |
| 1051 | [x] | PUT | `/api/workflows/versions/{id}` | 用户端 | path: id / body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/workflowOrchestrator.js` | 29 |
| 1052 | [x] | DELETE | `/api/workflows/versions/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/workflowOrchestrator.js` | 33 |
| 1053 | [x] | POST | `/api/workflows/versions/{id}/publish` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/workflowOrchestrator.js` | 37 |
| 1054 | [x] | POST | `/api/workflows/versions/{id}/archive` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/workflowOrchestrator.js` | 41 |
| 1055 | [x] | POST | `/api/workflows/execute` | 用户端 | body: data | 400 精确列出 workflow_id 等必填（校验正确）（2026-10-27 复核）| `src/api/workflowOrchestrator.js` | 45 |
| 1056 | [x] | GET | `/api/workflows/executions` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/workflowOrchestrator.js` | 49 |
| 1057 | [x] | GET | `/api/workflows/executions/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/workflowOrchestrator.js` | 53 |
| 1058 | [x] | GET | `/api/workflows/executions/{executionId}/nodes` | 用户端 | path: executionId | 200 code=0（两条腿均通过） | `src/api/workflowOrchestrator.js` | 57 |
| 1059 | [x] | POST | `/api/workflows/executions/{id}/stop` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/workflowOrchestrator.js` | 61 |
| 1060 | [x] | GET | `/api/xianyu/list` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/xianyuCard.js` | 4 |
| 1061 | [x] | GET | `/api/xianyu/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/xianyuCard.js` | 8 |
| 1062 | [x] | POST | `/api/xianyu/create` | 用户端 | body: data | 400「无效的请求参数」（校验正确但未给字段，同类可改进）（2026-10-27 复核）| `src/api/xianyuCard.js` | 12 |
| 1063 | [x] | PUT | `/api/xianyu/update` | 用户端 | body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/xianyuCard.js` | 16 |
| 1064 | [x] | DELETE | `/api/xianyu/delete/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/xianyuCard.js` | 20 |
| 1065 | [x] | GET | `/api/xianyu/view/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/xianyuCard.js` | 24 |
| 1066 | [x] | GET | `/api/xianyu/stats/card/{id}` | 用户端 | path: id / query: params | 200 code=0（两条腿均通过） | `src/api/xianyuCard.js` | 28 |
| 1067 | [x] | GET | `/api/xianyu/stats/overall` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/xianyuCard.js` | 32 |
| 1068 | [x] | POST | `/api/xianyu/{id}/generate-short-link` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/xianyuCard.js` | 36 |
| 1069 | [x] | GET | `/api/xiaohongshu/list` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/xiaohongshuCard.js` | 4 |
| 1070 | [x] | GET | `/api/xiaohongshu/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/xiaohongshuCard.js` | 8 |
| 1071 | [x] | POST | `/api/xiaohongshu/create` | 用户端 | body: data | 400「无效的请求参数」（校验正确但未给字段，同类可改进）（2026-10-27 复核）| `src/api/xiaohongshuCard.js` | 12 |
| 1072 | [x] | PUT | `/api/xiaohongshu/update` | 用户端 | body: data | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/xiaohongshuCard.js` | 16 |
| 1073 | [x] | DELETE | `/api/xiaohongshu/delete/{id}` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/xiaohongshuCard.js` | 20 |
| 1074 | [x] | GET | `/api/xiaohongshu/view/{id}` | 用户端 | path: id | 200 code=0（两条腿均通过） | `src/api/xiaohongshuCard.js` | 24 |
| 1075 | [x] | GET | `/api/xiaohongshu/stats/card/{id}` | 用户端 | path: id / query: params | 200 code=0（两条腿均通过） | `src/api/xiaohongshuCard.js` | 28 |
| 1076 | [x] | GET | `/api/xiaohongshu/stats/overall` | 用户端 | query: params | 200 code=0（两条腿均通过） | `src/api/xiaohongshuCard.js` | 32 |
| 1077 | [x] | POST | `/api/xiaohongshu/{id}/generate-short-link` | 用户端 | path: id | [~] 未验（缺样本 id / 避免破坏真实数据）（2026-10-27 复核）| `src/api/xiaohongshuCard.js` | 36 |
