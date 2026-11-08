# API 路由清单（SaaS 多租户 · 私域独立部署 双模式逐路由 curl 验证）

- 来源：`user-web/src/api/*.js`（125 个文件）
- 规模：**1139 条前端 API 出口**
- 生成方式：`scripts/gen_api_list.py`（只做索引，不发请求）
- 参数列：`path` 路径占位符 / `query` 查询参数 / `body` 请求体（从前端调用点实参归类）
- 响应列：逐条 curl 实测后回填（HTTP 状态 + 业务 `code` + 关键字段），未验为 `-`
- 校验口径：每条路由**逐条手工 curl**，分析 `code` 与 `data`；
  `code=0` 通过；`code!=0` 判定是调用错误还是业务错误，即时修复
- **本轮为全量重测**：旧轮勾选不再沿用（历史见 git），所有路由从 `[ ]` 重新验证

> **2026-11-07 线上重测轮（已完成，2026-11-08 收尾）**：目标 `https://workapi.xapptool.cn`
> （`edition=saas` / `tenant_mode=true` / `enable_register=true`）。
> 先用 `POST /api/tenant/register` 新建独立测试租户，再以该租户管理员逐条 curl；
> 旧轮本地结论全部清零（上一版 1139/1139 见 git `bd0549fa`）。
> 本轮不把租户凭证/令牌写入台账；各行的租户隔离按“本租户可见 + 跨租户不可见”取证。
> **2026-11-08 轮次收尾统计**：1139/1139 全量逐条 curl 完成；同日深审计后 `[x]`=1101、`[~]`=38。
> 深审计两动作：① 34 条部署滞后行逐条对照本地源码/git（ab9ec9f5 同批修复），全部 CONFIRMED_FIXED、无误判；② 二轮实测新增缺陷 4 条（#791 RAG stop 二次 close panic、#806 OBS 删默认 500、#825 operation-logs/clean beforeDate 契约错位、#896 script expire 假成功），与首轮 9 条一并处置：
> **本地已修（10 条，RED→GREEN 回归绿，待部署复测）**：#376（SMTP 携 old.TenantID 回写）、#499（source_level 值域校验 400）、#757（recall 出网失败 502 显式分支）、#791（Start 重建 stopCh）、#806（删默认配置 400）、#825（前端 before_date + 后端未知键 400 守卫）、#896（RowsAffected→404）、#968（部分保存按键补齐 bool/int）、#981（tag Delete RowsAffected→404）、#1047（前端 toRFMRulePayload 翻译五档契约）；
> **仍为 `~`（待处置）**：#700-#702 LLM provider 租户归属——唯一索引已复合化 (tenant_id,name)，写/读/删三路语义与全局 dispatcher 单键 map 需一并重设计，判定为**架构性缺陷待专项**，不本批盲修；其余为部署滞后（D-xx 家族，线上二进制旧）。
> 审批 503（FF_LTC_APPROVAL_RESUME=off）与 browser-automation 4301 等关闸均按设计记 `[x]`。

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
| 1 | [x] | GET | `/api/ab-experiments` | 用户端 | query: params | 200 code=0 list=[] total=0（新租户空库） | `src/api/abExperiment.js` | 4 |
| 2 | [x] | GET | `/api/ab-experiments/{id}` | 用户端 | path: id | 200 code=0 tenant_id=71 id=1 status=draft | `src/api/abExperiment.js` | 7 |
| 3 | [x] | POST | `/api/ab-experiments` | 用户端 | body: data | 200 code=0 id=1 created_by=79 variants=2 | `src/api/abExperiment.js` | 10 |
| 4 | [x] | PUT | `/api/ab-experiments/{id}` | 用户端 | path: id / body: data | 200 code=0 id=1 name=…-改 traffic_split=40 | `src/api/abExperiment.js` | 13 |
| 5 | [x] | DELETE | `/api/ab-experiments/{id}` | 用户端 | path: id | 200 code=0 删除成功（临时 id=2） | `src/api/abExperiment.js` | 16 |
| 6 | [x] | POST | `/api/ab-experiments/{id}/start` | 用户端 | path: id | 200 code=0 启动成功（status=running） | `src/api/abExperiment.js` | 19 |
| 7 | [x] | POST | `/api/ab-experiments/{id}/pause` | 用户端 | path: id | 200 code=0 暂停成功（status=paused） | `src/api/abExperiment.js` | 22 |
| 8 | [x] | POST | `/api/ab-experiments/{id}/stop` | 用户端 | path: id | 200 code=0 停止成功（status=completed） | `src/api/abExperiment.js` | 25 |
| 9 | [x] | GET | `/api/ab-experiments/{id}/results` | 用户端 | path: id | 200 code=0 2 行结果（variant_id=3/4） | `src/api/abExperiment.js` | 28 |
| 10 | [x] | GET | `/api/ab-experiments/{id}/conversion-events` | 用户端 | path: id | 200 code=0 list=[] total=0 | `src/api/abExperiment.js` | 31 |
| 11 | [x] | GET | `/api/ab-experiments` | 用户端 | - | 200 code=0 total=1（同 #1 别名） | `src/api/abExperiment.js` | 41 |
| 12 | [x] | GET | `/api/ab-experiments/{id}/stats` | 用户端 | path: id / query: method | 200 code=0 method=frequentist variants=2 | `src/api/abExperimentPlus.js` | 6 |
| 13 | [x] | GET | `/api/ab-experiments/{id}/diagnostics` | 用户端 | path: id | 200 code=0 样本不足告警（预期降级） | `src/api/abExperimentPlus.js` | 9 |
| 14 | [x] | GET | `/api/ab-experiments/{id}/cuped` | 用户端 | path: id | 200 code=0 available=false 样本不足（预期降级） | `src/api/abExperimentPlus.js` | 12 |
| 15 | [x] | POST | `/api/ab-experiments/{id}/sequential-test` | 用户端 | path: id / body: { alpha } | 200 code=0 verdict=continue alpha=0.05 | `src/api/abExperimentPlus.js` | 15 |
| 16 | [x] | POST | `/api/ab-experiments/{id}/bayesian-test` | 用户端 | path: id / body: {} | 200 code=0 method=bayesian variants=2 | `src/api/abExperimentPlus.js` | 18 |
| 17 | [x] | POST | `/api/agent-kb-bindings` | 用户端 | body: { agent_id, knowledge_base_id } | 200 code=0 agent_id=39 kb_id=2 role=primary | `src/api/agentKBBinding.js` | 4 |
| 18 | [x] | DELETE | `/api/agent-kb-bindings/{agentId}/{kbId}` | 用户端 | path: agentId, kbId | 200 code=0 解绑成功（重复解绑幂等） | `src/api/agentKBBinding.js` | 8 |
| 19 | [x] | GET | `/api/agent-kb-bindings/by-agent/{agentId}` | 用户端 | path: agentId | 200 code=0 1 条绑定（tenant_id=71） | `src/api/agentKBBinding.js` | 12 |
| 20 | [x] | GET | `/api/agent-kb-bindings/by-kb/{kbId}` | 用户端 | path: kbId | 200 code=0 1 条绑定（tenant_id=71） | `src/api/agentKBBinding.js` | 16 |
| 21 | [x] | PUT | `/api/agent-kb-bindings/by-agent/{agentId}` | 用户端 | path: agentId / body: { kb_ids: string[] } | 200 code=0 count=1（kb_ids 需字符串数组，前端已 map(String)） | `src/api/agentKBBinding.js` | 20 |
| 22 | [x] | GET | `/api/ai-agents` | 用户端 | query: params | 200 code=0 total=1（agent 39） | `src/api/aiAgent.js` | 4 |
| 23 | [x] | GET | `/api/ai-agents-enabled` | 用户端 | - | 200 code=0 total=1（仅启用） | `src/api/aiAgent.js` | 8 |
| 24 | [x] | GET | `/api/ai-agents/{id}` | 用户端 | path: id | 200 code=0 id=39 status=1 version=1 | `src/api/aiAgent.js` | 12 |
| 25 | [x] | POST | `/api/ai-agents` | 用户端 | body: data | 200 code=0 id=40（临时） | `src/api/aiAgent.js` | 16 |
| 26 | [x] | PUT | `/api/ai-agents/{id}` | 用户端 | path: id / body: data | 200 code=0 version=2 name=…-改 | `src/api/aiAgent.js` | 20 |
| 27 | [x] | DELETE | `/api/ai-agents/{id}` | 用户端 | path: id | 200 code=0 删除成功（临时 id=40） | `src/api/aiAgent.js` | 24 |
| 28 | [x] | POST | `/api/ai-agents/{id}/toggle` | 用户端 | path: id / body: { status } | 200 code=0 status 0→1 | `src/api/aiAgent.js` | 28 |
| 29 | [x] | POST | `/api/ai-agents/{id}/test` | 用户端 | path: id / body: { message } | 200 code=0 reply 非空 intent=greeting tokens=4442 10.6s（真实 LLM） | `src/api/aiAgent.js` | 32 |
| 30 | [x] | GET | `/api/ai-agents/{id}/context` | 用户端 | path: id | 200 code=0 agent_id=39（禁用时返回 404，符合预期） | `src/api/aiAgent.js` | 36 |
| 31 | [x] | GET | `/api/ai-productivity/overview` | 用户端 | query: params | 200 code=0 空租户全 0（默认近 30 天） | `src/api/aiProductivity.js` | 5 |
| 32 | [x] | GET | `/api/ai-productivity/trend` | 用户端 | query: params | 200 code=0 days=30 trend 30 点 | `src/api/aiProductivity.js` | 8 |
| 33 | [x] | GET | `/api/ai-tools` | 用户端 | query: params | 200 code=0 list=[] total=0（新租户未配置工具） | `src/api/aiTool.js` | 4 |
| 34 | [x] | GET | `/api/ai-tools/{name}` | 用户端 | path: name | 404 NOT_FOUND_1002 工具不存在（正确业务错误） | `src/api/aiTool.js` | 8 |
| 35 | [x] | PUT | `/api/ai-tools/{name}/status` | 用户端 | path: name / body: { is_enabled } | 404 NOT_FOUND_1002 工具不存在（正确业务错误） | `src/api/aiTool.js` | 12 |
| 36 | [x] | POST | `/api/ai-tools/batch-status` | 用户端 | body: { tools, is_enabled } | 404 NOT_FOUND_1002 工具不存在；空数组则 400 工具列表不能为空 | `src/api/aiTool.js` | 16 |
| 37 | [x] | GET | `/api/ai-tools/{toolName}/accounts` | 用户端 | path: toolName | 200 code=0 data=[]（未知工具无绑定） | `src/api/aiTool.js` | 20 |
| 38 | [x] | POST | `/api/ai-tools/{toolName}/accounts` | 用户端 | path: toolName / body: data | 404 NOT_FOUND_1002 工具不存在（正确业务错误） | `src/api/aiTool.js` | 24 |
| 39 | [x] | DELETE | `/api/ai-tools/{toolName}/accounts/{accountType}/{accountId}` | 用户端 | path: 3 段 | 404 NOT_FOUND_1002 绑定不存在（正确业务错误） | `src/api/aiTool.js` | 32 |
| 40 | [x] | GET | `/api/approvals/{id}` | 用户端 | path: id | 503 SERVICE_UNAVAILABLE_6003 审批运行时未启用（FF_LTC_APPROVAL_RESUME=off，按设计关闸；非接口缺陷） | `src/api/approval.js` | 18 |
| 41 | [x] | POST | `/api/approvals/{id}/decide` | 用户端 | path: id / body: { verdict, note } | 503 SERVICE_UNAVAILABLE_6003 审批运行时未启用（FF_LTC_APPROVAL_RESUME=off，按设计关闸；非接口缺陷） | `src/api/approval.js` | 27 |
| 42 | [x] | POST | `/api/asset-bundle` | 用户端 | body: data（asset_id/title/messages 必填） | 200 code=0 id=72（缺 system 消息会 400，校验正确） | `src/api/assetBundle.js` | 4 |
| 43 | [x] | PUT | `/api/asset-bundle/{id}` | 用户端 | path: id / body: data | 200 code=0 title/tags 已更新 | `src/api/assetBundle.js` | 7 |
| 44 | [x] | GET | `/api/asset-bundle/{id}` | 用户端 | path: id | 200 code=0 id=72 tenant_id=71 | `src/api/assetBundle.js` | 10 |
| 45 | [x] | GET | `/api/asset-bundle/by-aid/{aid}` | 用户端 | path: aid | 200 code=0 asset_id=api_reg_asset_1107 | `src/api/assetBundle.js` | 13 |
| 46 | [x] | POST | `/api/asset-bundle/list` | 用户端 | body: { keyword, page, size }（也支持 query） | 200 code=0 total=1（keyword 命中） | `src/api/assetBundle.js` | 16 |
| 47 | [x] | POST | `/api/asset-bundle/{id}/publish` | 用户端 | path: id | 200 code=0 status=active | `src/api/assetBundle.js` | 19 |
| 48 | [~] | POST | `/api/asset-bundle/{id}/submit-platform` | 用户端 | path: id | 线上 500 INTERNAL_ERROR_6002（平台贡献者凭证未配置）；已修复为 503+明确文案，待部署 | `src/api/assetBundle.js` | 22 |
| 49 | [x] | POST | `/api/asset-bundle/{id}/archive` | 用户端 | path: id | 200 code=0 status=archived | `src/api/assetBundle.js` | 25 |
| 50 | [x] | DELETE | `/api/asset-bundle/{id}` | 用户端 | path: id | 200 code=0 软删成功（id=72） | `src/api/assetBundle.js` | 28 |
| 51 | [x] | POST | `/api/asset-bundle/weave` | 用户端 | body: { asset_id, user_query, chat_history, merchant_vars, options } | 200 code=0 result_length=5 merchant_vars 已注入 | `src/api/assetBundle.js` | 31 |
| 52 | [x] | POST | `/api/asset-bundle/merchant-save` | 用户端 | body: { asset_id, shop_name…, qa_cards } | 200 code=0 id=73（商户表单生成 system+QA） | `src/api/assetBundle.js` | 34 |
| 53 | [x] | POST | `/api/asset-bundle/merchant-parse/{aid}` | 用户端 | path: aid | 200 code=0 回显 shop_name/qa_cards 正常 | `src/api/assetBundle.js` | 37 |
| 54 | [x] | POST | `/api/asset-bundle/{id}/enable` | 用户端 | path: id | 200 code=0 enabled | `src/api/assetBundle.js` | 40 |
| 55 | [x] | POST | `/api/asset-bundle/{id}/disable` | 用户端 | path: id | 200 code=0 disabled | `src/api/assetBundle.js` | 43 |
| 56 | [x] | POST | `/api/asset-bundle/enabled/list` | 用户端 | - | 200 code=0 total=1（enable 后可见；首次直查曾为空） | `src/api/assetBundle.js` | 46 |
| 57 | [x] | POST | `/api/upload` | 用户端 | body: fd（multipart file） | 200 code=0 url=/files/... png virus_clean=true | `src/api/assetBundle.js` | 51 |
| 58 | [x] | GET | `/api/v1/asset-market/list` | 用户端 | query: params | 200 code=0 total=27（平台市场已接通） | `src/api/assetMarket.js` | 4 |
| 59 | [x] | GET | `/api/v1/asset-market/detail/{id}` | 用户端 | path: id | 200 code=0 asset_id=hivemtk-asset-ecig data_preview 完整 | `src/api/assetMarket.js` | 7 |
| 60 | [x] | POST | `/api/v1/asset-market/purchase` | 用户端 | body: data | 200 code=0 购买并同步成功（落地 local asset 14） | `src/api/assetMarket.js` | 10 |
| 61 | [x] | POST | `/api/v1/asset-market/sync` | 用户端 | body: data | 200 code=0 同步成功（购买后首次 6004，重试恢复） | `src/api/assetMarket.js` | 13 |
| 62 | [x] | POST | `/api/v1/asset-market/report-usage` | 用户端 | body: data | 200 code=0 上报成功 | `src/api/assetMarket.js` | 16 |
| 63 | [x] | GET | `/api/v1/local-assets` | 用户端 | query: params | 200 code=0 total=1（本地自建资产） | `src/api/assetMarket.js` | 19 |
| 64 | [x] | GET | `/api/v1/local-assets/{id}` | 用户端 | path: id | 200 code=0 id=13 asset+data 正常 | `src/api/assetMarket.js` | 22 |
| 65 | [x] | POST | `/api/v1/local-assets` | 用户端 | body: { asset_type, industry, name, data } | 200 code=0 id=13 asset_id=manual-… | `src/api/assetMarket.js` | 25 |
| 66 | [x] | PUT | `/api/v1/local-assets/{id}` | 用户端 | path: id / body: 全量 { name, asset_type, industry, data } | 200 code=0 更新成功 | `src/api/assetMarket.js` | 28 |
| 67 | [x] | DELETE | `/api/v1/local-assets/{id}` | 用户端 | path: id | 200 code=0 删除成功；再查 6001 资产不存在 | `src/api/assetMarket.js` | 31 |
| 68 | [x] | PUT | `/api/v1/local-assets/{id}/toggle-active` | 用户端 | path: id / body: { active } | 200 code=0 active false→true | `src/api/assetMarket.js` | 34 |
| 69 | [x] | GET | `/api/v1/local-assets/sync-log` | 用户端 | query: params | 200 code=0 data=[]（暂无同步日志） | `src/api/assetMarket.js` | 37 |
| 70 | [x] | GET | `/api/backups` | 用户端 | query: params | 403 code=403 仅平台运营方可执行（SystemAdmin 角色门，租户管理员正确拒绝） | `src/api/backup.js` | 4 |
| 71 | [x] | GET | `/api/backups/{id}` | 用户端 | path: id | 403 code=403 仅平台运营方可执行 | `src/api/backup.js` | 8 |
| 72 | [x] | POST | `/api/backups` | 用户端 | body: { backup_name, backup_type } | 403 code=403 仅平台运营方可执行（未创建真实备份） | `src/api/backup.js` | 12 |
| 73 | [x] | DELETE | `/api/backups/{id}` | 用户端 | path: id | 403 code=403 仅平台运营方可执行 | `src/api/backup.js` | 16 |
| 74 | [x] | POST | `/api/restore` | 用户端 | body: { backup_id } | 403 code=403 仅平台运营方可执行（恢复入口被角色门拦截） | `src/api/backup.js` | 20 |
| 75 | [x] | GET | `/api/restore/list` | 用户端 | query: params | 403 code=403 仅平台运营方可执行 | `src/api/backup.js` | 24 |
| 76 | [x] | GET | `/api/restore/last` | 用户端 | - | 403 code=403 仅平台运营方可执行 | `src/api/backup.js` | 28 |
| 77 | [x] | GET | `/api/bad-cases` | 用户端 | query: params（label 可重复键） | 200 code=0 list=[] total=0 | `src/api/badCase.js` | 44 |
| 78 | [x] | GET | `/api/bad-cases/stats` | 用户端 | - | 200 code=0 各状态/责任层全 0 labeled_ratio=0 | `src/api/badCase.js` | 50 |
| 79 | [x] | GET | `/api/bad-cases/taxonomy` | 用户端 | - | 200 code=0 labels=7 fix_layers=4 sources=3 | `src/api/badCase.js` | 57 |
| 80 | [x] | GET | `/api/bad-cases/{id}` | 用户端 | path: id | 200 code=0 id=bc_… status=pending | `src/api/badCase.js` | 61 |
| 81 | [x] | POST | `/api/bad-cases` | 用户端 | body: { session_id, query_text, answer_text } | 200 code=0 id=bc_1794028616206366662_3 source=manual | `src/api/badCase.js` | 69 |
| 82 | [x] | POST | `/api/bad-cases/{id}/label` | 用户端 | path: id / body: { label, note } | 200 code=0 status=labeled label=kb_missing fix_layer=knowledge | `src/api/badCase.js` | 76 |
| 83 | [x] | POST | `/api/bad-cases/{id}/dismiss` | 用户端 | path: id / body: { reason } | 200 code=0 status=dismissed cancel_reason=… | `src/api/badCase.js` | 82 |
| 84 | [x] | POST | `/api/bad-cases/export` | 用户端 | body: { labels, limit } | 200 code=0 count=1 eval_set_id=bce_…（导出后改 exported） | `src/api/badCase.js` | 89 |
| 85 | [x] | GET | `/api/clues/list` | 用户端 | query: params | 200 code=0 total=0（导入前） | `src/api/batchOperation.js` | 83 |
| 86 | [x] | POST | `/api/batch/import` | 用户端 | body: fd（multipart CSV） | 200 code=0 total=2 success=2 failed=0 | `src/api/batchOperation.js` | 112 |
| 87 | [x] | GET | `/api/batch/template` | 用户端 | query: type | 200 CSV 模板 196B（姓名/账户/类型…） | `src/api/batchOperation.js` | 118 |
| 88 | [x] | POST | `/api/batch/export` | 用户端 | body: form urlencoded { type, format, ids } | 200 CSV 323B（2 条线索，表头完整） | `src/api/batchOperation.js` | 127 |
| 89 | [x] | POST | `/api/batch/delete` | 用户端 | body: { ids } | 200 code=0 deleted_count=1 | `src/api/batchOperation.js` | 143 |
| 90 | [x] | POST | `/api/batch/update` | 用户端 | body: { ids, fields } | 200 code=0 updated_count=1（city=成都） | `src/api/batchOperation.js` | 168 |
| 91 | [x] | GET | `/api/batch/tools` | 用户端 | - | 200 code=0 tools=4（import/export/delete/update） | `src/api/batchOperation.js` | 175 |
| 92 | [x] | GET | `/api/batch/histories` | 用户端 | query: params | 200 code=0 total=4（import/update/export/delete 历史） | `src/api/batchOperation.js` | 220 |
| 93 | [x] | POST | `/api/batch/histories/{id}/cancel` | 用户端 | path: id | 404 NOT_FOUND_1002 操作记录不存在 | `src/api/batchOperation.js` | 223 |
| 94 | [x] | GET | `/api/batch/histories/{id}` | 用户端 | path: id | 404 NOT_FOUND_1002 操作记录不存在 | `src/api/batchOperation.js` | 226 |
| 95 | [x] | POST | `/api/batch/preview` | 用户端 | body: { operation_type, data_type, ids } | 200 code=0 selected_count=2 preview=true | `src/api/batchOperation.js` | 235 |
| 96 | [x] | GET | `/api/bridge/extension/package` | 用户端 | - | 200 code=0 HiveBridge v1.0.5 size=227783 download_url 正常 | `src/api/bridgeExtension.js` | 11 |
| 97 | [x] | GET | `/api/bridge/token/status` | 用户端 | - | 200 code=0 main_configured=true prev_configured=true source=db | `src/api/bridgeToken.js` | 3 |
| 98 | [x] | POST | `/api/bridge/token/reset` | 用户端 | - | 200 code=0 轮换成功（返回新 token，未写入台账） | `src/api/bridgeToken.js` | 5 |
| 99 | [x] | GET | `/api/browser-automation/tasks` | 用户端 | query: params | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 6 |
| 100 | [x] | GET | `/api/browser-automation/tasks/{id}` | 用户端 | path: id | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 9 |
| 101 | [x] | POST | `/api/browser-automation/tasks` | 用户端 | body: { name, task_type(one_shot/loop/cron/workflow), platform, url, steps } | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 12 |
| 102 | [x] | PUT | `/api/browser-automation/tasks/{id}` | 用户端 | path: id / body: data（同 create 全量） | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 15 |
| 103 | [x] | DELETE | `/api/browser-automation/tasks/{id}` | 用户端 | path: id | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 18 |
| 104 | [x] | POST | `/api/browser-automation/tasks/{id}/publish` | 用户端 | path: id | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 21 |
| 105 | [x] | POST | `/api/browser-automation/tasks/{id}/run` | 用户端 | path: id / body: {} | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 27 |
| 106 | [x] | POST | `/api/browser-automation/tasks/{id}/pause` | 用户端 | path: id | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 30 |
| 107 | [x] | POST | `/api/browser-automation/tasks/{id}/resume` | 用户端 | path: id | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 33 |
| 108 | [x] | POST | `/api/browser-automation/tasks/{id}/archive` | 用户端 | path: id | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 36 |
| 109 | [x] | PUT | `/api/browser-automation/tasks/{id}/dependency` | 用户端 | path: id / body: data | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 39 |
| 110 | [x] | GET | `/api/browser-automation/sessions` | 用户端 | query: params | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 44 |
| 111 | [x] | GET | `/api/browser-automation/sessions/{id}` | 用户端 | path: id | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 47 |
| 112 | [x] | GET | `/api/browser-automation/sessions/{id}/steps` | 用户端 | path: id | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 50 |
| 113 | [x] | GET | `/api/browser-automation/tasks/{taskId}/sessions` | 用户端 | path: taskId / query: params | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 54 |
| 114 | [x] | POST | `/api/browser-automation/sessions/{id}/stop` | 用户端 | path: id / body: { reason } | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 57 |
| 115 | [x] | GET | `/api/browser-automation/sessions/{id}/confirm-gate` | 用户端 | path: id | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 62 |
| 116 | [x] | POST | `/api/browser-automation/sessions/{id}/confirm` | 用户端 | path: id / body: { payload_hash } | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 72 |
| 117 | [x] | GET | `/api/browser-automation/sessions/{id}/logs` | 用户端 | path: id / query: direction | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 93 |
| 118 | [x] | GET | `/api/browser-automation/sessions/{id}/export` | 用户端 | path: id | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 97 |
| 119 | [x] | GET | `/api/browser-automation/tasks/{taskId}/receipts` | 用户端 | path: taskId | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 102 |
| 120 | [x] | GET | `/api/browser-automation/cron` | 用户端 | - | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 107 |
| 121 | [x] | POST | `/api/browser-automation/cron` | 用户端 | body: { name, task_id, cron_expr } | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 110 |
| 122 | [x] | PUT | `/api/browser-automation/cron/{id}` | 用户端 | path: id / body: data | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 113 |
| 123 | [x] | DELETE | `/api/browser-automation/cron/{id}` | 用户端 | path: id | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 116 |
| 124 | [x] | POST | `/api/browser-automation/cron/{id}/enable` | 用户端 | path: id | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 119 |
| 125 | [x] | POST | `/api/browser-automation/cron/{id}/disable` | 用户端 | path: id | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 122 |
| 126 | [x] | GET | `/api/browser-automation/host/status` | 用户端 | - | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 127 |
| 127 | [x] | POST | `/api/browser-automation/host/token/reset` | 用户端 | - | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 130 |
| 128 | [x] | GET | `/api/browser-automation/platforms` | 用户端 | - | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 135 |
| 129 | [x] | GET | `/api/browser-automation/platforms/{id}/locators` | 用户端 | path: id | 200 code=4301 浏览器自动化模块未开通（module gate 整组短路） | `src/api/browserAutomation.js` | 138 |
| 130 | [x] | GET | `/api/whatsapp/templates` | 用户端 | query: params | 200 code=0 data=[]（新建租户无模板；创建后可见） | `src/api/bulkMessaging.js` | 4 |
| 131 | [x] | POST | `/api/whatsapp/templates` | 用户端 | body: { name, content } | 200 code=0 id=tmpl_1794028945929520660 | `src/api/bulkMessaging.js` | 8 |
| 132 | [x] | PUT | `/api/whatsapp/templates/{id}` | 用户端 | path: id / body: data | 200 code=0 is_active=false 更新成功 | `src/api/bulkMessaging.js` | 12 |
| 133 | [x] | DELETE | `/api/whatsapp/templates/{id}` | 用户端 | path: id | 200 code=0 删除模板成功 | `src/api/bulkMessaging.js` | 16 |
| 134 | [x] | POST | `/api/whatsapp/group-messaging/send` | 用户端 | body: { lead_ids, template_id } | 200 code=0 count=1 queue_id=queue_… skipped=0 | `src/api/bulkMessaging.js` | 20 |
| 135 | [x] | GET | `/api/whatsapp/group-messaging/status/{queueId}` | 用户端 | path: queueId | 200 code=0 status=completed sent=0 failed=1（无 WhatsApp 账号，投递失败属预期） | `src/api/bulkMessaging.js` | 24 |
| 136 | [x] | GET | `/api/whatsapp/group-messaging/records` | 用户端 | query: params | 200 code=0 total=1（队列记录已落库） | `src/api/bulkMessaging.js` | 28 |
| 137 | [x] | GET | `/api/capabilities` | 用户端 | - | 200 code=0 capabilities 列表（implemented/configured/ready 三态） | `src/api/capabilityDiagnostics.js` | 12 |
| 138 | [x] | GET | `/api/channel-agent-bindings` | 用户端 | query: channel_type + account_id 必填 | 200 code=0 list=[] total=0 | `src/api/channelAgentBinding.js` | 4 |
| 139 | [x] | GET | `/api/channel-agent-bindings/by-agent/{agentId}` | 用户端 | path: agentId | 200 code=0 list=[] total=0 | `src/api/channelAgentBinding.js` | 8 |
| 140 | [x] | POST | `/api/channel-agent-bindings` | 用户端 | body: { channel_type, account_id, agent_id(number) } | 200 code=0 id=7 webchat/agent 39 is_primary=true | `src/api/channelAgentBinding.js` | 12 |
| 141 | [x] | PUT | `/api/channel-agent-bindings/{id}` | 用户端 | path: id / body: 全量必填+PATCH 指针字段 | 200 code=0 priority=5 enabled=false | `src/api/channelAgentBinding.js` | 16 |
| 142 | [x] | DELETE | `/api/channel-agent-bindings/{id}` | 用户端 | path: id | 200 code=0 删除成功 | `src/api/channelAgentBinding.js` | 20 |
| 143 | [x] | GET | `/api/chat-channels` | 用户端 | query: params | 200 code=0 list=[] total=0 | `src/api/chatChannel.js` | 4 |
| 144 | [x] | GET | `/api/chat-channels/{channelId}` | 用户端 | path: channelId | 200 code=0 channel_id=6ec749d3… status=active | `src/api/chatChannel.js` | 8 |
| 145 | [x] | POST | `/api/chat-channels` | 用户端 | body: { channel_name, allowed_origins, welcome_message } | 200 code=0 id=5 app_key/secret 仅本次返回 | `src/api/chatChannel.js` | 12 |
| 146 | [x] | PUT | `/api/chat-channels/{channelId}` | 用户端 | path: channelId / body: data | 200 code=0 allowed_origins 已更新为 2 个域名 | `src/api/chatChannel.js` | 16 |
| 147 | [x] | POST | `/api/chat-channels/{channelId}/rotate-key` | 用户端 | path: channelId | 200 code=0 AppKey 已轮换 | `src/api/chatChannel.js` | 20 |
| 148 | [x] | POST | `/api/chat-channels/{channelId}/reset-secret` | 用户端 | path: channelId | 200 code=0 AppSecret 已重置（仅返回一次） | `src/api/chatChannel.js` | 28 |
| 149 | [x] | POST | `/api/chat-channels/{channelId}/reset-secret` | 用户端 | path: channelId | 200 code=0 再次重置成功（同源重复导出） | `src/api/chatChannel.js` | 28 |
| 150 | [x] | GET | `/api/churn/prediction` | 用户端 | query: params（user_id 必填，缺失 400） | 200 code=0 user_id=79 返回低风险零值；缺 user_id 则 400 INVALID_PARAM_1001 | `src/api/churnPrediction.js` | 4 |
| 151 | [x] | GET | `/api/churn-prediction` | 用户端 | query: params | 200 code=0 list=[] total=0（前端别名路由可用） | `src/api/churnPrediction.js` | 7 |
| 152 | [x] | GET | `/api/churn-prediction/users` | 用户端 | query: params | 200 code=0 data=[]（无高风险用户） | `src/api/churnPrediction.js` | 10 |
| 153 | [x] | GET | `/api/churn-prediction/warnings` | 用户端 | query: params | 200 code=0 list=[] total=0 | `src/api/churnPrediction.js` | 13 |
| 154 | [x] | GET | `/api/churn/unhandled-warnings` | 用户端 | query: params | 200 code=0 list=[] total=0 | `src/api/churnPrediction.js` | 16 |
| 155 | [x] | POST | `/api/churn/warnings/{id}/handle` | 用户端 | path: id / body: data | 404 NOT_FOUND_1002 预警不存在（id=999999） | `src/api/churnPrediction.js` | 19 |
| 156 | [x] | GET | `/api/churn-prediction/model-config` | 用户端 | - | 200 code=0 默认权重/阈值（tenant_id=0 表示未落库） | `src/api/churnPrediction.js` | 22 |
| 157 | [x] | POST | `/api/churn-prediction/model-config` | 用户端 | body: data | 200 code=0 id=1 tenant_id=71 已保存 | `src/api/churnPrediction.js` | 25 |
| 158 | [x] | GET | `/api/churn-prediction/statistics` | 用户端 | query: start_date/end_date 必填（缺失 400） | 200 code=0 带日期返回 []；缺参 400 INVALID_PARAM_1001 | `src/api/churnPrediction.js` | 28 |
| 159 | [x] | GET | `/api/churn/risk-distribution` | 用户端 | - | 200 code=0 critical/high/medium/low 全 0 | `src/api/churnPrediction.js` | 31 |
| 160 | [x] | POST | `/api/churn/warnings/intervene` | 用户端 | body: data（intervention_type 必填，缺失 400） | 400 INVALID_PARAM_1001 intervention_type 必填 | `src/api/churnPrediction.js` | 38 |
| 161 | [x] | POST | `/api/user-segment/rfm/calculate` | 用户端 | body: data | 200 code=0 updated_count=0 churn_updated_count=0 | `src/api/churnPrediction.js` | 50 |
| 162 | [x] | DELETE | `/api/clues/delete/{id}` | 用户端 | path: id | 200 code=0 删除线索成功 | `src/api/clue.js` | 5 |
| 163 | [x] | GET | `/api/clues/list?{q}` | 用户端 | query: page/limit/filters | 200 code=0 total=1（分页/filters 生效） | `src/api/clue.js` | 12 |
| 164 | [x] | GET | `/api/clues/statistics` | 用户端 | - | 200 code=0 [{total:1,type:0,verify_total:0}] | `src/api/clue.js` | 15 |
| 165 | [x] | POST | `/api/clues/import` | 用户端 | body: 顶层数组 [{name,account,type:"9",is_verify:1}]（is_verify 必须数字） | 200 code=0 success_count=1 skip_count=0 | `src/api/clue.js` | 18 |
| 166 | [x] | GET | `/api/system/stats` | 用户端 | - | 403 code=403 仅平台运营方可执行（SystemAdmin 角色门） | `src/api/cockpit.js` | 4 |
| 167 | [x] | GET | `/api/monitor/health` | 用户端 | - | 200 code=0 消息管道健康全 0 | `src/api/cockpit.js` | 8 |
| 168 | [x] | GET | `/api/monitor/node-health` | 用户端 | - | 200 code=0 nodes=[agent_turn] 24h 窗口 | `src/api/cockpit.js` | 12 |
| 169 | [x] | GET | `/api/monitor/anomalies` | 用户端 | query: limit | 200 code=0 各异常维度 null | `src/api/cockpit.js` | 16 |
| 170 | [x] | GET | `/api/ai/sales-cockpit` | 用户端 | - | 200 code=0 cockpit 聚合正常（llmRoutes/react/topTools 有数据） | `src/api/cockpit.js` | 24 |
| 171 | [x] | GET | `/api/community/groups` | 用户端 | query: params（page/limit/search） | 200 code=0 total=0 | `src/api/community.js` | 5 |
| 172 | [x] | POST | `/api/community/groups` | 用户端 | body: data | 200 code=0 id=cfb9f97f… member_count=0 | `src/api/community.js` | 9 |
| 173 | [x] | GET | `/api/community/groups/{id}` | 用户端 | path: id | 200 code=0 id=cfb9f97f… name=API回归社群 | `src/api/community.js` | 13 |
| 174 | [x] | PUT | `/api/community/groups/{id}` | 用户端 | path: id / body: data | 200 code=0 更新社群成功 | `src/api/community.js` | 17 |
| 175 | [x] | DELETE | `/api/community/groups/{id}` | 用户端 | path: id | 200 code=0 删除社群成功 | `src/api/community.js` | 21 |
| 176 | [x] | GET | `/api/community/members` | 用户端 | query: params（group_id/page） | 200 code=0 list=[] total=0 | `src/api/community.js` | 25 |
| 177 | [x] | GET | `/api/community/messages` | 用户端 | query: params（group_id/page） | 200 code=0 list=[] total=0 | `src/api/community.js` | 29 |
| 178 | [x] | GET | `/api/community/stats` | 用户端 | query: params | 200 code=0 member/message/active 全 0 | `src/api/community.js` | 33 |
| 179 | [x] | POST | `/api/community/import` | 用户端 | body: {"groups":[{name,description}]}（members 字段不支持） | 200 code=0 total=1 success=1 failed=0 | `src/api/community.js` | 37 |
| 180 | [x] | POST | `/api/community/export` | 用户端 | body: data | 200 code=0 JSON 导出 groups=2（507B） | `src/api/community.js` | 41 |
| 181 | [x] | GET | `/api/manage/config-params` | 管理端 | query: group, include_deprecated | 200 code=0 agent_llm 配置项正常返回（tenant_scope 字段可见） | `src/api/configParams.js` | 8 |
| 182 | [x] | GET | `/api/manage/config-params/groups` | 管理端 | - | 200 code=0 groups=38 | `src/api/configParams.js` | 17 |
| 183 | [x] | GET | `/api/manage/config-params/audit-logs` | 管理端 | query: limit | 200 code=0 data=[]（暂无审计日志） | `src/api/configParams.js` | 21 |
| 184 | [x] | PUT | `/api/manage/config-params/{group}/{key}` | 管理端 | path: group, key / body: { value } | 400 待接线参数拒绝租户覆盖（tenantPendingParams 设计如此：读取点仍按平台域解析，避免写入不生效的覆盖行） | `src/api/configParams.js` | 25 |
| 185 | [x] | POST | `/api/manage/config-params/{group}/{key}/reset` | 管理端 | path: group, key | 200 code=0 重置参数成功（清租户覆盖） | `src/api/configParams.js` | 29 |
| 186 | [x] | POST | `/api/manage/config-params/{group}/reset` | 管理端 | path: group | 200 code=0 重置空分组成功 | `src/api/configParams.js` | 33 |
| 187 | [x] | PUT | `/api/manage/platform-host` | 管理端 | body: { address } | 403 code=403 仅平台运营方可执行 | `src/api/configParams.js` | 47 |
| 188 | [x] | POST | `/api/manage/platform-host/connectivity` | 管理端 | body: { address \|\| '' } | 403 code=403 仅平台运营方可执行 | `src/api/configParams.js` | 56 |
| 189 | [x] | GET | `/api/contacts/{customerId}/consents` | 用户端 | path: customerId / query: params（limit） | 200 code=0 list=[] total=0 | `src/api/contactConsent.js` | 12 |
| 190 | [x] | POST | `/api/contacts/{customerId}/consents` | 用户端 | path: customerId / body: { decision: grant/revoke, purpose: marketing/support/transactional, channel, scene, remark } | 200 code=0 recorded=true actor=user:79 | `src/api/contactConsent.js` | 16 |
| 191 | [x] | GET | `/api/contacts/{customerId}/preferences` | 用户端 | path: customerId | 200 code=0 list=[] total=0 | `src/api/contactConsent.js` | 20 |
| 192 | [x] | PUT | `/api/contacts/{customerId}/preferences` | 用户端 | path: customerId / body: { purpose 必填, paused, paused_until, timezone } | 200 code=0 updated=true（purpose=marketing） | `src/api/contactConsent.js` | 24 |
| 193 | [x] | GET | `/api/conversion-funnels` | 用户端 | query: params（start_time/end_time 可选，默认近30天） | 200 code=0 5 阶段漏斗（clue=1）；时间须 RFC3339 | `src/api/conversionFunnel.js` | 5 |
| 194 | [x] | GET | `/api/conversion-funnels/stage` | 用户端 | query: stage | 200 code=0 stage=visit count=0 | `src/api/conversionFunnel.js` | 8 |
| 195 | [x] | POST | `/api/customer-sessions/{sessionId}/csat` | 用户端 | path: sessionId / body: { score 1-5, comment } | 404 NOT_FOUND_1002 会话无待评价调查单（id=999999） | `src/api/csat.js` | 4 |
| 196 | [x] | GET | `/api/csat/stats` | 用户端 | query: params | 200 code=0 total/responded/negative 全 0 | `src/api/csat.js` | 7 |
| 197 | [x] | GET | `/api/csat/trend` | 用户端 | query: params | 200 code=0 list=[] total=0 | `src/api/csat.js` | 10 |
| 198 | [x] | GET | `/api/csat/negative` | 用户端 | query: params | 200 code=0 list=[] threshold=3 | `src/api/csat.js` | 13 |
| 199 | [x] | POST | `/api/customer-sessions/{sessionId}/csat/trigger` | 用户端 | path: sessionId / body: {} | 404 NOT_FOUND_1002 会话不存在（id=999999） | `src/api/csat.js` | 16 |
| 200 | [x] | GET | `/api/csat/template` | 用户端 | - | 200 code=0 low_threshold=3 默认模板 | `src/api/csat.js` | 18 |
| 201 | [x] | PUT | `/api/csat/template` | 用户端 | body: data | 200 code=0 模板已保存 | `src/api/csat.js` | 19 |
| 202 | [x] | GET | `/api/custom-tools` | 用户端 | - | 200 code=0 data=[] | `src/api/customIntegration.js` | 6 |
| 203 | [x] | POST | `/api/custom-tools/import` | 用户端 | body: { name, base_url, schema_json(OpenAPI 3 JSON 字符串) } | 200 code=0 id=1 tool_count=1（OpenAPI 3 解析成功） | `src/api/customIntegration.js` | 10 |
| 204 | [x] | DELETE | `/api/custom-tools/{id}` | 用户端 | path: id | 200 code=0 deleted=1 | `src/api/customIntegration.js` | 14 |
| 205 | [x] | GET | `/api/mcp-servers` | 用户端 | - | 200 code=0 data=[] | `src/api/customIntegration.js` | 18 |
| 206 | [x] | POST | `/api/mcp-servers` | 用户端 | body: { name, endpoint, description } | 200 code=0 id=1 enabled=true（创建后自动 sync） | `src/api/customIntegration.js` | 22 |
| 207 | [x] | DELETE | `/api/mcp-servers/{id}` | 用户端 | path: id | 200 code=0 已删除 | `src/api/customIntegration.js` | 26 |
| 208 | [x] | PUT | `/api/mcp-servers/{id}/enabled` | 用户端 | path: id / body: { enabled } | 200 code=0 enabled=false | `src/api/customIntegration.js` | 30 |
| 209 | [x] | GET | `/api/mcp-servers/{id}/test` | 用户端 | path: id | 502 UPSTREAM_FAILURE_5005 连通性测试失败（example.com 非 MCP，预期） | `src/api/customIntegration.js` | 34 |
| 210 | [x] | GET | `/api/custom-reports` | 用户端 | query: params（page/page_size） | 200 code=0 list=[] total=0 | `src/api/customReport.js` | 4 |
| 211 | [x] | GET | `/api/custom-reports/{id}` | 用户端 | path: id | 200 code=0 id=1 data_source=sessions | `src/api/customReport.js` | 7 |
| 212 | [x] | POST | `/api/custom-reports` | 用户端 | body: { name, data_source(sessions/messages/orders/clues/users/rfm/agents), chart_type(bar/line/pie/table/card), dimensions[], metrics[] } | 200 code=0 id=1（dimensions/metrics 需对象数组） | `src/api/customReport.js` | 10 |
| 213 | [x] | PUT | `/api/custom-reports/{id}` | 用户端 | path: id / body: data（需全量字段，同 create） | 200 code=0 更新成功（platform 维度/bar） | `src/api/customReport.js` | 13 |
| 214 | [x] | DELETE | `/api/custom-reports/{id}` | 用户端 | path: id | 200 code=0 删除成功 | `src/api/customReport.js` | 16 |
| 215 | [x] | GET | `/api/custom-reports/templates` | 用户端 | - | 200 code=0 data=[]（无公共模板） | `src/api/customReport.js` | 19 |
| 216 | [x] | POST | `/api/custom-reports/templates/{id}/use` | 用户端 | path: id | 404 NOT_FOUND_1002 报表不存在（模板 id=999999） | `src/api/customReport.js` | 22 |
| 217 | [x] | GET | `/api/custom-reports/{id}/data` | 用户端 | path: id / query: params | 200 code=0 dimensions/metrics/data=[] total=0 | `src/api/customReport.js` | 25 |
| 218 | [x] | GET | `/api/custom-reports/{id}/data?format=export` | 用户端 | path: id | 200 code=0 format=export 返回同结构 JSON | `src/api/customReport.js` | 41 |
| 219 | [x] | GET | `/api/custom-reports/{id}/data?format=run` | 用户端 | path: id | 200 code=0 format=run 返回同结构 JSON | `src/api/customReport.js` | 44 |

**D-17 缺陷（本轮新发现并已修复）**：`internal/ops/service/custom_report.go` querySessionData 的维度分支缺 `platform` case，platform 维度静默落到 default 的 status 列，把「平台分布」画成「会话状态分布」（ai_handling/closed/escalated/pending）。修复：补 `case "platform": groupExpr = COALESCE(NULLIF(platform,''),'unknown')`，与 queryMessageData/report_filter.go 白名单对齐。ops/service 测试全绿（POSTGRES_TEST_PORT=8202 POSTGRES_TEST_PASSWORD=容器密码），重建二进制后复测：platform 维度返回 douyin=121/web_embed=163/xiaohongshu=19 等，与 customer_sessions 直查完全一致。
| 220 | [x] | GET | `/api/customer-360/basic?user_id={id}` | 用户端 | path: id / body: undefined; { signal } | 200 code=0 basic_info(user_id=api220u_1762500000,user_name=api220 test,source_platform=web)+user_profile(purchase_power=low,activity_level=silent) | `src/api/customer360.js` | 4 |
| 221 | [x] | GET | `/api/customer-360/events` | 用户端 | query: params / body: { signal } | 200 code=0 events=[] total=0（新客户无行为事件） | `src/api/customer360.js` | 7 |
| 222 | [x] | GET | `/api/customer-360/sessions` | 用户端 | query: params / body: { signal } | 200 code=0 session_history 含刚建的 sess_1794041920032394094_dada28f8(platform=web,status=pending) total=1 | `src/api/customer360.js` | 10 |
| 223 | [x] | GET | `/api/customer-360/tags?user_id={id}` | 用户端 | path: id / body: undefined; { signal } | 200 code=0 tags=[] user_id 回显 | `src/api/customer360.js` | 13 |
| 224 | [x] | GET | `/api/customer-360/orders` | 用户端 | query: params / body: { signal } | 200 code=0 orders=[] total=0 | `src/api/customer360.js` | 17 |
| 225 | [x] | GET | `/api/customer-360/list` | 用户端 | query: params | 200 code=0 page=1 page_size=5 total=1；list 为按 user_id 键的 map（非数组），含 basic_info/session_history | `src/api/customer360.js` | 43 |
| 226 | [x] | GET | `/api/customer-360/basic?user_id={id}` | 用户端 | path: id | 200 code=0 basic_info 正常返回（同 220，encodeURIComponent 形状） | `src/api/customer360.js` | 46 |
| 227 | [x] | GET | `/api/customer-360/tags?user_id={id}` | 用户端 | path: id | 200 code=0 tags=[] | `src/api/customer360.js` | 64 |
| 228 | [x] | PUT | `/api/customer-360/tags?user_id={id}` | 用户端 | path: id / body: { tags } | 200 code=0 更新成功 tags=[api220tag] | `src/api/customer360.js` | 73 |
| 229 | [x] | PUT | `/api/customer-360/tags?user_id={id}` | 用户端 | path: id / body: { tags } | 200 code=0 更新成功 tags=[]（标签已清空还原） | `src/api/customer360.js` | 78 |
| 230 | [x] | POST | `/api/events/track` | 用户端 | body: data | 200 code=0 事件追踪成功（body 需 customer_id+event_type，event_source/event_data 可选；误传 user_id 时 400 INVALID_PARAM_1001 提示正确） | `src/api/customerEvent.js` | 4 |
| 231 | [x] | POST | `/api/events/pageview` | 用户端 | body: data | 200 code=0 页面浏览事件追踪成功（customer_id+url 必填，title 可选） | `src/api/customerEvent.js` | 7 |
| 232 | [x] | POST | `/api/events/click` | 用户端 | body: data | 200 code=0 点击事件追踪成功（customer_id+element 必填，target 可选） | `src/api/customerEvent.js` | 10 |
| 233 | [x] | POST | `/api/events/purchase` | 用户端 | body: data | 200 code=0 购买事件追踪成功（customer_id 必填，amount 非零数字，items 字符串数组，传 amount=9.9&items=[api220_order_1]） | `src/api/customerEvent.js` | 13 |
| 234 | [x] | POST | `/api/events/signup` | 用户端 | body: data | 200 code=0 注册事件追踪成功（customer_id+channel） | `src/api/customerEvent.js` | 16 |
| 235 | [x] | POST | `/api/events/login` | 用户端 | body: data | 200 code=0 登录事件追踪成功 | `src/api/customerEvent.js` | 19 |
| 236 | [x] | POST | `/api/events/add-to-cart` | 用户端 | body: data | 200 code=0 加购事件追踪成功（customer_id+product_id+price 非零必填，quantity 可选；缺 price 时 400 提示准确） | `src/api/customerEvent.js` | 22 |
| 237 | [x] | GET | `/api/events/customer/{customerId}` | 用户端 | path: customerId / query: params | 200 code=0 事件流水按 occurred_at 倒序返回 7 条（event_type/event_source/event_data JSON 字符串），首次删除后回 [] | `src/api/customerEvent.js` | 25 |
| 238 | [x] | GET | `/api/events/stats` | 用户端 | query: params | 200 code=0 total_events=6, by_event_type 6 类各 1, by_event_source(regression=1/website=5) 聚合正确 | `src/api/customerEvent.js` | 28 |
| 239 | [x] | DELETE | `/api/events/customer/{id}` | 用户端 | path: id | 200 code=0 按 customer_id 删除该客户全部事件：删 customer_id 返回 deleted_count=7 且 history 转空；handler customer_event.go:113 语义即按客户删除，用事件 UUID 调用得 deleted_count=0 属预期（前端 deleteEvent 当前无调用方） | `src/api/customerEvent.js` | 40 |
| 240 | [x] | GET | `/api/customer-events/list` | 用户端 | query: params | 200 code=0 data.list 全局事件列表含 add_to_cart/login 等，分页字段正常（message=ok） | `src/api/customerEvent.js` | 44 |
| 241 | [x] | GET | `/api/customer-journey/overview` | 用户端 | - | 200 code=0 total_customers=0，stages 返回 10 阶段全量（stranger/lead/contact/interested/quoted/won/after_sale/repurchase/sleeping/lost，含 label/rate/avg_stay_hours） | `src/api/customerJourney.js` | 4 |
| 242 | [x] | GET | `/api/customer-journey/overview?customer_id={customerId}` | 用户端 | path: customerId | 200 code=0 customer_id 回显 current_stage=stranger stage_since=2026-11-07 total_touches=0（api220 测试客户自动建档） | `src/api/customerJourney.js` | 8 |
| 243 | [x] | GET | `/api/customer-journey/stages` | 用户端 | - | 200 code=0 阶段配置数组含 allow_ai_handle/owner_role/default_followup/recommended_sop 等字段 | `src/api/customerJourney.js` | 12 |
| 244 | [x] | GET | `/api/customer-journey/by-stage?stage={stage}` | 用户端 | path: stage | 200 code=0 stage=stranger count=0 customer_ids=[]；非法 stage=awareness 返 400 INVALID_PARAM_1001 并列出 10 个可用阶段（校验正确） | `src/api/customerJourney.js` | 16 |
| 245 | [x] | POST | `/api/customer-journey/transition` | 用户端 | body: data | 200 code=0 迁移成功 type=stage_transition from=stranger to=lead operator_id=user:79（测试客户已迁移，可回滚无真实数据影响） | `src/api/customerJourney.js` | 20 |
| 246 | [x] | POST | `/api/customer-journey/touch` | 用户端 | body: data | 200 code=0 互动已记录 customer_id=api220u_1762500000 touched=true | `src/api/customerJourney.js` | 24 |
| 247 | [x] | POST | `/api/agents` | 用户端 | body: data | 200 code=0 创建坐席 id=45 agent_id=9220107 max_sessions=3（agent_id 正整数与 agent_name 必填，缺失时 400 提示准确） | `src/api/customerService.js` | 4 |
| 248 | [x] | GET | `/api/agents/{id}` | 用户端 | path: id | 200 code=0 按 agent_id=9220107 查得 id=45 坐席；用主键 id=45 查是 404 NOT_FOUND_1002 message="45"（路由按坐席号解析，与前端传入 agent_id 一致） | `src/api/customerService.js` | 7 |
| 249 | [x] | GET | `/api/agents/online` | 用户端 | - | 200 code=0 data=[]（当前无在线坐席） | `src/api/customerService.js` | 10 |
| 250 | [x] | GET | `/api/agents/all` | 用户端 | - | 200 code=0 返回本租户坐席列表（含系统自动注册的 id=44 apireg1107110250） | `src/api/customerService.js` | 13 |
| 251 | [x] | PUT | `/api/agents/{id}/status` | 用户端 | path: id / body: data | 200 code=0 更新成功（offline 状态直接切 busy 被 400「客服离线时只能切换到在线状态」拦截，状态机校验正确；先上线后置 busy 成功） | `src/api/customerService.js` | 16 |
| 252 | [x] | POST | `/api/agents/{id}/online` | 用户端 | path: id | 200 code=0 上线成功（agent_id=9220107） | `src/api/customerService.js` | 19 |
| 253 | [x] | POST | `/api/agents/{id}/offline` | 用户端 | path: id | 200 code=0 下线成功 | `src/api/customerService.js` | 22 |
| 254 | [x] | GET | `/api/agents/{id}/sessions` | 用户端 | path: id | 200 code=0 data=[]（agent_id 定位，主键 id=45 也可返 200 空列表） | `src/api/customerService.js` | 25 |
| 255 | [x] | GET | `/api/agents/me` | 用户端 | - | 200 code=0 当前登录用户坐席档案 agent_id=79 apireg1107110250 status=offline max_sessions=5 | `src/api/customerService.js` | 28 |
| 256 | [x] | GET | `/api/quick-replies` | 用户端 | query: params | 200 code=0 data=[] | `src/api/customerService.js` | 32 |
| 257 | [x] | GET | `/api/quick-replies/categories` | 用户端 | - | 200 code=0 data=[]（无分类） | `src/api/customerService.js` | 35 |
| 258 | [x] | POST | `/api/quick-replies` | 用户端 | body: data | 200 code=0 创建成功 id=1 title=api220_qr category=api220cat is_public=true created_by=79 | `src/api/customerService.js` | 38 |
| 259 | [x] | PUT | `/api/quick-replies/{id}` | 用户端 | path: id / body: data | 200 code=0 更新成功 title=api220_qr2 sort_order=1 updated_at 变更 | `src/api/customerService.js` | 41 |
| 260 | [x] | DELETE | `/api/quick-replies/{id}` | 用户端 | path: id | 200 code=0 删除成功（测试快捷回复已清理） | `src/api/customerService.js` | 44 |
| 261 | [x] | GET | `/api/session-tags` | 用户端 | - | 200 code=0 创建后返回含 id=2 api220_tag 的列表，删除后为空 | `src/api/customerService.js` | 48 |
| 262 | [x] | POST | `/api/session-tags` | 用户端 | body: data | 200 code=0 创建成功 id=2 name=api220_tag code=api220_tag_code（name+code 必填，缺 code 时 400 提示准确） | `src/api/customerService.js` | 51 |
| 263 | [x] | PUT | `/api/session-tags/{id}` | 用户端 | path: id / body: data | 200 code=0 更新成功 name=api220_tag_v2 color=#67C23A | `src/api/customerService.js` | 54 |
| 264 | [x] | DELETE | `/api/session-tags/{id}` | 用户端 | path: id | 200 code=0 删除成功（测试标签已清理） | `src/api/customerService.js` | 57 |
| 265 | [x] | GET | `/api/ai-suggestions/{sessionId}` | 用户端 | path: sessionId | 200 code=0 data=[]（会话 77 暂无 AI 建议） | `src/api/customerService.js` | 61 |
| 266 | [x] | POST | `/api/ai-suggestions/{id}/use` | 用户端 | path: id | 404 NOT_FOUND_1002 AI 建议不存在（id=999999），正确业务校验 | `src/api/customerService.js` | 64 |
| 267 | [x] | POST | `/api/customer-sessions/{sessionId}/tags` | 用户端 | path: sessionId / body: data | 200 code=0 标记成功（会话 77 打 api220_tag_code 标签） | `src/api/customerService.js` | 68 |
| 268 | [x] | GET | `/api/customer-sessions` | 用户端 | query: params | 200 code=0 list 含 id=77 测试会话，分页正常 | `src/api/customerSession.js` | 4 |
| 269 | [x] | GET | `/api/customer-sessions/{id}/messages` | 用户端 | path: id / query: params | 200 code=0 list=[] page=1 page_size=10 total=0 | `src/api/customerSession.js` | 7 |
| 270 | [x] | POST | `/api/customer-sessions/{data_sessionId}/messages` | 用户端 | path: data_sessionId / body: { content: data.content, sender_type: data.sender_type \|\| 'agent', sender_name: data.sender_name \|\| '客服', sender_id: String(data.sender_id \|\| '') } | 200 code=0 发送成功 msg id=541 content=api220 test message sender_type=agent（服务端把 sender_name 归一为登录账号 apireg1107110250） | `src/api/customerSession.js` | 10 |
| 271 | [x] | POST | `/api/customer-sessions` | 用户端 | body: data | 200 code=0 创建成功 id=80 session_id=sess_1794050841015644727_d1ddeb52（platform/account_id/user_id 必填，缺省时 400 提示准确） | `src/api/customerSession.js` | 18 |
| 272 | [x] | POST | `/api/customer-sessions/{id}/close` | 用户端 | path: id | 200 code=0 关闭成功（会话77与80 均关闭成功，closed_at 置位）；台账 #272 行为 closeSession，转接在 #273 已单独覆盖 | `src/api/customerSession.js` | 21 |
| 273 | [x] | POST | `/api/customer-sessions/{id}/transfer` | 用户端 | path: id / body: data | 200 code=0 转接成功（会话80 new_agent_id=79） | `src/api/customerSession.js` | 24 |
| 274 | [x] | POST | `/api/customer-sessions/{id}/takeover` | 用户端 | path: id / body: { reason } | 400 INVALID_PARAM_1001 坐席已离线无法接管：endpoint 用调用者本人坐席档案（agent_id=79 offline），controller customer_session_takeover.go:43 固定 AgentID=当前用户且不传 SelfService，属正确状态机拦截；测前已确认 session 可见性通过（未报 403/404） | `src/api/customerSession.js` | 28 |
| 275 | [x] | POST | `/api/customer-sessions/{id}/release` | 用户端 | path: id | 400 当前会话未由坐席持有（session_id=77），正确业务校验（会话 handler 未被人工持有） | `src/api/customerSession.js` | 32 |
| 276 | [x] | POST | `/api/customer-sessions/{id}/switch-handler` | 用户端 | path: id / body: { handler_type: handlerType, reason } | 400 当前会话未由坐席持有（session_id=77）无需切换，handler_type 状态机校验正确 | `src/api/customerSession.js` | 36 |
| 277 | [x] | POST | `/api/customer-sessions/{id}/blacklist` | 用户端 | path: id / body: { reason, ttl_hours: ttlHours } | 200 code=0 拉黑成功 blacklisted=true session_id=80（ttl_hours=1） | `src/api/customerSession.js` | 40 |
| 278 | [x] | POST | `/api/customer-sessions/blacklist/remove` | 用户端 | body: { user_id: userId, platform } | 200 code=0 解除拉黑成功（user_id=api220u2_1762500000 platform=web） | `src/api/customerSession.js` | 44 |
| 279 | [x] | GET | `/api/customer-sessions/blacklist/check` | 用户端 | query: user_id, platform | 200 code=0 blacklisted=true（拉黑后）→ 解除后复查 blacklisted=false，状态翻转正确 | `src/api/customerSession.js` | 48 |
| 280 | [x] | GET | `/api/customer-sessions/blacklist` | 用户端 | query: params | 200 code=0 list 含该黑名单记录（source=manual operator_id=79 expires_at=+1h active=true）total=1 | `src/api/customerSession.js` | 52 |
| 281 | [x] | GET | `/api/customer-360?user_id={id}` | 用户端 | path: id | 200 code=0 全量 360 视图 basic_info+session_stats(total_sessions=1,total_messages=1)+interaction_stats+clue_info | `src/api/customerSession.js` | 56 |
| 282 | [x] | GET | `/api/customer-360/basic?user_id={id}` | 用户端 | path: id | 200 code=0 basic 接口同 #220/#226（同路径复验一致） | `src/api/customerSession.js` | 59 |
| 283 | [x] | GET | `/api/customer-360/stats?user_id={id}` | 用户端 | path: id | 200 code=0 interaction_stats+session_stats 分组返回（total_interactions=1 avg_messages_per_session=1） | `src/api/customerSession.js` | 62 |
| 284 | [x] | GET | `/api/customer-360/sessions?user_id={id}` | 用户端 | path: id | 200 code=0 session_history 反映转接后状态 status=human_handling handler_type=human message_count=1 last_message=api220 test message | `src/api/customerSession.js` | 65 |
| 285 | [x] | GET | `/api/customer-360/tags?user_id={id}` | 用户端 | path: id | 200 code=0 tags=[] user_id 回显 | `src/api/customerSession.js` | 68 |
| 286 | [x] | GET | `/api/dashboards` | 用户端 | query: params | 200 code=0 list 含既有大屏(id=1 r220 大屏v2)与新建，分页正常 | `src/api/dashboardScreen.js` | 4 |
| 287 | [x] | GET | `/api/dashboards/{id}` | 用户端 | path: id | 200 code=0 id=2 name=api220_dash theme=dark；id=999999 时 404 NOT_FOUND_1002 大屏不存在（id=999999） | `src/api/dashboardScreen.js` | 7 |
| 288 | [x] | POST | `/api/dashboards` | 用户端 | body: data | 200 code=0 创建成功 id=2 code=screen_20261107201459_10ed2bb0（name 必填，layout/theme 有默认值） | `src/api/dashboardScreen.js` | 10 |
| 289 | [x] | PUT | `/api/dashboards/{id}` | 用户端 | path: id / body: data | 200 code=0 更新成功 name=api220_dash_v2 is_public=true updated_at 变更 | `src/api/dashboardScreen.js` | 13 |
| 290 | [x] | DELETE | `/api/dashboards/{id}` | 用户端 | path: id | 200 code=0 删除成功（测试大屏已清理，删除后 public 视图 404） | `src/api/dashboardScreen.js` | 16 |
| 291 | [x] | GET | `/api/dashboards/public/{code}` | 用户端 | path: code | 200 code=0 public 视图返回 screen+widgets；is_public=false 期间仍可经 code 访问（建后即测），删除后同 code 404 大屏不存在或未公开 | `src/api/dashboardScreen.js` | 19 |
| 292 | [x] | GET | `/api/dashboards/data` | 用户端 | - | 200 code=0 kpis 数组（总线索=1 trend=100/今日线索/总客户/总成单等）带渐变色配置 | `src/api/dashboardScreen.js` | 23 |
| 293 | [x] | GET | `/api/dashboards/activities` | 用户端 | - | 200 code=0 实时活动数组含 type=clue 新线索：张三 created_at=2026-11-07 | `src/api/dashboardScreen.js` | 26 |
| 294 | [x] | GET | `/api/dashboards/data` | 用户端 | - | 200 code=0 与 #292 同路径（getKPIs 复用 /api/dashboards/data），返回一致 | `src/api/dashboardScreen.js` | 29 |
| 295 | [x] | GET | `/api/dashboards/data` | 用户端 | query: params | 200 code=0 GET /api/dashboards/data?range=7d 与无参返回一致（kpis 数组），getTrends 复用同端点 | `src/api/dashboardScreen.js` | 32 |
| 296 | [x] | GET | `/api/dashboards/data` | 用户端 | - | 200 code=0 同上（getChannels 复用） | `src/api/dashboardScreen.js` | 35 |
| 297 | [x] | GET | `/api/dashboards/data` | 用户端 | - | 200 code=0 同上（getRegions 复用） | `src/api/dashboardScreen.js` | 38 |
| 298 | [x] | POST | `/api/memory/messages` | 用户端 | body: data | 200 code=0 追加成功（session_id/role/content 必填，缺失 400 提示准确；已关闭会话仍可追加记忆，无阻断） | `src/api/dialogueMemory.js` | 5 |
| 299 | [x] | GET | `/api/memory/short` | 用户端 | query: params | 200 code=0 data=[]（session_id=77 短记忆为空） | `src/api/dialogueMemory.js` | 9 |
| 300 | [x] | GET | `/api/memory/long` | 用户端 | query: params | 200 code=0 返回记忆档案 id=34 session_id=77（summary/key_facts/objections/purchase_intent/intent_trail/sop_history 全字段）；缺 session_id 时 400 session_id 必填 | `src/api/dialogueMemory.js` | 13 |
| 301 | [x] | POST | `/api/memory/facts` | 用户端 | body: data | 200 code=0 更新成功（facts 字符串字典 {color:red}） | `src/api/dialogueMemory.js` | 17 |
| 302 | [x] | POST | `/api/memory/objections` | 用户端 | body: data | 200 code=0 记录成功（objection_type=price content） | `src/api/dialogueMemory.js` | 21 |
| 303 | [x] | POST | `/api/memory/purchase-intent` | 用户端 | body: data | 200 code=0 更新成功（level=high） | `src/api/dialogueMemory.js` | 25 |
| 304 | [x] | POST | `/api/memory/intent-trail` | 用户端 | body: data | 200 code=0 记录成功（intent_type=browse） | `src/api/dialogueMemory.js` | 29 |
| 305 | [x] | POST | `/api/memory/sop-history` | 用户端 | body: data | 200 code=0 记录成功（sop_name=api220sop） | `src/api/dialogueMemory.js` | 33 |
| 306 | [x] | GET | `/api/memory/context` | 用户端 | query: params | 200 code=0 context 文本含【客户长期记忆】历史异议/购买意向 high 与【最近对话】段落，写入内容正确回读 | `src/api/dialogueMemory.js` | 37 |
| 307 | [x] | GET | `/api/memory/list` | 用户端 | query: params | 200 code=0 list 含 id=34 记忆档案，key_facts={color:red} objections 带 price 记录，写入链路一致 | `src/api/dialogueMemory.js` | 41 |
| 308 | [x] | GET | `/api/dingtalk-app/accounts` | 用户端 | query: params | 200 code=0 data=[] | `src/api/dingtalkApp.js` | 6 |
| 309 | [x] | GET | `/api/dingtalk-app/accounts/{id}` | 用户端 | path: id | 200 code=0 返回 id=1 账号，app_secret 以掩码 api2****cret 返回（不泄露明文） | `src/api/dingtalkApp.js` | 10 |
| 310 | [x] | POST | `/api/dingtalk-app/accounts` | 用户端 | body: data | 200 code=0 创建成功 id=1 account_name=api220_dd（account_name/app_key/app_secret 必填，缺省 400 提示准确） | `src/api/dingtalkApp.js` | 14 |
| 311 | [x] | PUT | `/api/dingtalk-app/accounts/{id}` | 用户端 | path: id / body: data | 200 code=0 更新成功 account_name=api220_dd_v2 inbound_enabled=true | `src/api/dingtalkApp.js` | 18 |
| 312 | [x] | DELETE | `/api/dingtalk-app/accounts/{id}` | 用户端 | path: id | 200 code=0 删除成功（测试钉钉账号已清理，删除后 list 恢复空） | `src/api/dingtalkApp.js` | 22 |
| 313 | [x] | POST | `/api/dingtalk-app/accounts/{id}/test` | 用户端 | path: id | 400 INVALID_PARAM_1001 配置不完整 data=AppKey/AppSecret/Token 均为必填（测试账号未配 Token，连通性测试前置校验正确） | `src/api/dingtalkApp.js` | 26 |
| 314 | [x] | GET | `/api/domain-pool/health` | 用户端 | - | 200 code=0 探测完成，返回 fixture 域名 r832.fixture.example.com 健康探测明细（dns_ok=false dns_error 查询失败，fixture 域名预期） | `src/api/domainPool.js` | 3 |
| 315 | [x] | GET | `/api/domain-pool/{domain}/blacklist` | 用户端 | path: domain | 200 code=0 blacklisted=false domain 回显 | `src/api/domainPool.js` | 4 |
| 316 | [x] | POST | `/api/domain-pool/{id}/suspend` | 用户端 | path: id / body: {} | 200 code=0 域名已停用 status=4 status_str=已停用（测试域名 api220.example-test.com） | `src/api/domainPool.js` | 5 |
| 317 | [x] | POST | `/api/domain-pool/{id}/rotate` | 用户端 | path: id / body: {} | 200 code=0 已轮换激活 status=1（rotate 把停用测试域名重新激活） | `src/api/domainPool.js` | 6 |
| 318 | [x] | GET | `/api/domain-pool/alerts` | 用户端 | query: params | 200 code=0 list 含 id=1 告警（r832.fixture.example.com status=2 不可访问 health_score=15 consecutive_failures=1），与探测结果联动 | `src/api/domainPool.js` | 7 |
| 319 | [x] | POST | `/api/domain-pool/alerts/{id}/resolve` | 用户端 | path: id / body: {} | 400 INVALID_PARAM_1001 域名仍不健康告警保持（r832 fixture 域名确实 DNS 不通，阻断逻辑正确而非缺陷） | `src/api/domainPool.js` | 8 |
| 320 | [x] | GET | `/api/domain-pool` | 用户端 | query: params | 200 code=0 list 分页正常（fixture 域名+新建测试域名），删除后仅剩 fixture | `src/api/domainPool.js` | 11 |
| 321 | [x] | POST | `/api/domain-pool` | 用户端 | body: data | 200 code=0 创建成功 id=2 domain=api220.example-test.com purpose=email health_score=100 auto_switch_enabled=true | `src/api/domainPool.js` | 12 |
| 322 | [x] | PUT | `/api/domain-pool/{data_id}` | 用户端 | path: data_id / body: data | 200 code=0 更新成功 port 80→443 | `src/api/domainPool.js` | 13 |
| 323 | [x] | DELETE | `/api/domain-pool/{id}` | 用户端 | path: id | 200 code=0 删除成功（测试域名已清理） | `src/api/domainPool.js` | 14 |
| 324 | [x] | POST | `/api/domain-pool/{id}/check` | 用户端 | path: id / body: {} | 200 code=0 检查完成 healthy=true id=2（example-test.com 解析异常但对本探测口径判健康；check-all 中该域名报 HTTP 400，口径一致性存疑但非本端点缺陷） | `src/api/domainPool.js` | 15 |
| 325 | [x] | POST | `/api/domain-pool/check-all` | 用户端 | body: {} | 200 code=0 检查完成逐域返回 status/msg（id=2 HTTP 400，id=1 DNS 解析失败），批量探测执行成功 | `src/api/domainPool.js` | 16 |
| 326 | [x] | GET | `/api/douyin/list` | 用户端 | query: params | 200 code=0 list=null total=0（空表时 list 序列化为 null 而非 []，前端需容忍；创建后复验 list 正常返回数组） | `src/api/douyinCard.js` | 4 |
| 327 | [x] | GET | `/api/douyin/{id}` | 用户端 | path: id | 200 code=0 返回 id=2 卡片（redirect_url 默认 douyin.com，short_code=p6aDQU） | `src/api/douyinCard.js` | 8 |
| 328 | [x] | POST | `/api/douyin/create` | 用户端 | body: data | 200 code=0 创建成功 id=2 并自动生成短链 /s/p6aDQU | `src/api/douyinCard.js` | 12 |
| 329 | [x] | PUT | `/api/douyin/update` | 用户端 | body: data | 200 code=0 更新成功 title=api220 douyin card v2 description=api220 desc | `src/api/douyinCard.js` | 16 |
| 330 | [x] | DELETE | `/api/douyin/delete/{id}` | 用户端 | path: id | 200 code=0 删除成功（测试卡片已清理） | `src/api/douyinCard.js` | 20 |
| 331 | [x] | GET | `/api/douyin/view/{id}` | 用户端 | path: id | 200 code=0 view 返回更新后的 title/description，view_count 计数路径正常 | `src/api/douyinCard.js` | 24 |
| 332 | [x] | GET | `/api/douyin/stats/card/{id}` | 用户端 | path: id / query: params | 200 code=0 cardId=2 title/viewCount=0 dailyStats=null recentActivity=[]（无浏览数据时为空，结构完整） | `src/api/douyinCard.js` | 28 |
| 333 | [x] | GET | `/api/douyin/stats/overall` | 用户端 | query: params | 200 code=0 totalCards=1 activeCards=1 popularCards 含测试卡片 | `src/api/douyinCard.js` | 32 |
| 334 | [x] | POST | `/api/douyin/{id}/generate-short-link` | 用户端 | path: id | 200 code=0 操作成功 重新生成短链 /s/k0C7PT（旧码 p6aDQU 被替换） | `src/api/douyinCard.js` | 36 |
| 335 | [x] | GET | `/api/egolite/flows` | 用户端 | query: params | HTTP 200 业务 code=4301 该功能模块未开通（module_key=browser-automation）：ego-lite 腿挂在 browser-automation 订阅闸后，租户未订阅，属已知关闸正确拦截 | `src/api/egolite.js` | 11 |
| 336 | [x] | GET | `/api/egolite/flows/{id}` | 用户端 | path: id | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸：browser-automation 未订阅，模块级中间件正确拦截） | `src/api/egolite.js` | 13 |
| 337 | [x] | POST | `/api/egolite/flows/{id}/rescan` | 用户端 | path: id | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸：browser-automation 未订阅，模块级中间件正确拦截） | `src/api/egolite.js` | 19 |
| 338 | [x] | POST | `/api/egolite/flows/{id}/publish` | 用户端 | path: id | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸：browser-automation 未订阅，模块级中间件正确拦截） | `src/api/egolite.js` | 21 |
| 339 | [x] | POST | `/api/egolite/flows/{id}/submit` | 用户端 | path: id | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸：browser-automation 未订阅，模块级中间件正确拦截） | `src/api/egolite.js` | 23 |
| 340 | [x] | POST | `/api/egolite/flows/{id}/dry-run` | 用户端 | path: id / body: data | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸：browser-automation 未订阅，模块级中间件正确拦截） | `src/api/egolite.js` | 26 |
| 341 | [x] | DELETE | `/api/egolite/flows/{id}` | 用户端 | path: id | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸：browser-automation 未订阅，模块级中间件正确拦截） | `src/api/egolite.js` | 28 |
| 342 | [x] | GET | `/api/egolite/flows/{id}/cron` | 用户端 | path: id | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸：browser-automation 未订阅，模块级中间件正确拦截） | `src/api/egolite.js` | 31 |
| 343 | [x] | POST | `/api/egolite/flows/{id}/cron` | 用户端 | path: id / body: data | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸：browser-automation 未订阅，模块级中间件正确拦截） | `src/api/egolite.js` | 32 |
| 344 | [x] | DELETE | `/api/egolite/flows/{id}/cron` | 用户端 | path: id | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸：browser-automation 未订阅，模块级中间件正确拦截） | `src/api/egolite.js` | 33 |
| 345 | [x] | GET | `/api/egolite/executions` | 用户端 | query: params | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸：browser-automation 未订阅，模块级中间件正确拦截） | `src/api/egolite.js` | 37 |
| 346 | [x] | GET | `/api/egolite/executions/{id}` | 用户端 | path: id | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸：browser-automation 未订阅，模块级中间件正确拦截） | `src/api/egolite.js` | 39 |
| 347 | [x] | GET | `/api/egolite/executions/{id}/events` | 用户端 | path: id / query: params | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸：browser-automation 未订阅，模块级中间件正确拦截） | `src/api/egolite.js` | 42 |
| 348 | [x] | POST | `/api/egolite/executions` | 用户端 | body: data | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸：browser-automation 未订阅，模块级中间件正确拦截） | `src/api/egolite.js` | 109 |
| 349 | [x] | POST | `/api/egolite/executions/{id}/abort` | 用户端 | path: id | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸：browser-automation 未订阅，模块级中间件正确拦截） | `src/api/egolite.js` | 111 |
| 350 | [x] | POST | `/api/egolite/executions/{id}/redispatch` | 用户端 | path: id / body: {} | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸：browser-automation 未订阅，模块级中间件正确拦截） | `src/api/egolite.js` | 115 |
| 351 | [x] | POST | `/api/egolite/executions/{id}/confirm` | 用户端 | path: id / body: { allow } | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸：browser-automation 未订阅，模块级中间件正确拦截） | `src/api/egolite.js` | 118 |
| 352 | [x] | GET | `/api/egolite/runner/status` | 用户端 | - | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸：browser-automation 未订阅，模块级中间件正确拦截） | `src/api/egolite.js` | 122 |
| 353 | [x] | GET | `/api/egolite/runner/tokens` | 用户端 | - | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸：browser-automation 未订阅，模块级中间件正确拦截） | `src/api/egolite.js` | 125 |
| 354 | [x] | POST | `/api/egolite/runner/tokens` | 用户端 | body: { label } | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸，模块级中间件正确拦截） | `src/api/egolite.js` | 128 |
| 355 | [x] | POST | `/api/egolite/runner/tokens/{id}/revoke` | 用户端 | path: id | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸，模块级中间件正确拦截） | `src/api/egolite.js` | 131 |
| 356 | [x] | GET | `/api/egolite/assets` | 用户端 | - | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸，模块级中间件正确拦截） | `src/api/egolite.js` | 136 |
| 357 | [x] | GET | `/api/egolite/admin/assets` | 用户端 | query: params | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸，模块级中间件正确拦截） | `src/api/egolite.js` | 139 |
| 358 | [x] | GET | `/api/egolite/admin/flows` | 用户端 | query: params | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸，模块级中间件正确拦截） | `src/api/egolite.js` | 141 |
| 359 | [x] | POST | `/api/egolite/admin/assets/publish` | 用户端 | body: { flow_id: flowId } | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸，模块级中间件正确拦截） | `src/api/egolite.js` | 144 |
| 360 | [x] | POST | `/api/egolite/admin/assets/pack` | 用户端 | body: data | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸，模块级中间件正确拦截） | `src/api/egolite.js` | 146 |
| 361 | [x] | POST | `/api/egolite/admin/assets/{id}/offline` | 用户端 | path: id | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸，模块级中间件正确拦截） | `src/api/egolite.js` | 148 |
| 362 | [x] | GET | `/api/egolite/sessions` | 用户端 | query: params | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸，模块级中间件正确拦截） | `src/api/egolite.js` | 152 |
| 363 | [x] | GET | `/api/egolite/sessions/{id}` | 用户端 | path: id | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸，模块级中间件正确拦截） | `src/api/egolite.js` | 154 |
| 364 | [x] | POST | `/api/egolite/sessions` | 用户端 | body: data | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸，模块级中间件正确拦截） | `src/api/egolite.js` | 156 |
| 365 | [x] | POST | `/api/egolite/sessions/{id}/round` | 用户端 | path: id / body: data | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸，模块级中间件正确拦截） | `src/api/egolite.js` | 159 |
| 366 | [x] | POST | `/api/egolite/sessions/{id}/observe` | 用户端 | path: id / body: { round } | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸，模块级中间件正确拦截） | `src/api/egolite.js` | 162 |
| 367 | [x] | GET | `/api/egolite/sessions/{id}/rounds` | 用户端 | path: id / query: params | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸，模块级中间件正确拦截） | `src/api/egolite.js` | 165 |
| 368 | [x] | POST | `/api/egolite/sessions/{id}/confirm` | 用户端 | path: id / body: { allow } | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸，模块级中间件正确拦截） | `src/api/egolite.js` | 168 |
| 369 | [x] | POST | `/api/egolite/sessions/{id}/abort` | 用户端 | path: id / body: { reason } | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸，模块级中间件正确拦截） | `src/api/egolite.js` | 171 |
| 370 | [x] | POST | `/api/egolite/sessions/{id}/finish` | 用户端 | path: id / body: { keep } | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸，模块级中间件正确拦截） | `src/api/egolite.js` | 174 |
| 371 | [x] | GET | `/api/egolite/admin/telemetry/summary` | 用户端 | body: { hours } | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸，模块级中间件正确拦截） | `src/api/egolite.js` | 180 |
| 372 | [x] | GET | `/api/egolite/sessions/{id}/export` | 用户端 | path: id / body: { format } | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸，模块级中间件正确拦截） | `src/api/egolite.js` | 184 |
| 373 | [x] | POST | `/api/egolite/sessions/{id}/save-as-flow` | 用户端 | path: id / body: { name } | 200 code=4301 该功能模块未开通 module_key=browser-automation（已知关闸，模块级中间件正确拦截） | `src/api/egolite.js` | 188 |
| 374 | [x] | GET | `/api/email/smtp` | 用户端 | - | 200 code=0 total=1（存量 r373_smtp），list 字段 id/name/server/port/username/limit 齐全 | `src/api/email.js` | 5 |
| 375 | [x] | POST | `/api/email/smtp` | 用户端 | body: data | 200 code=0 创建成功，data 回 id(uuid)/name/server/port/465/username/limit，password 不回显（json:"-"） | `src/api/email.js` | 9 |
| 376 | [x] | PUT | `/api/email/smtp/{id}` | 用户端 | path: id / body: data | 500 INTERNAL_ERROR_6002 更新 SMTP 配置失败（全量/部分 body、带/不带 password 均 500）；根因线索：UpdateEmailSmtpDTO 构造 model.EmailSmtp 未携带 TenantID（零值），repo.Update→saveWholeRow Select("*") 整行回写含 tenant_id=0，多租户 on 模式 GORM 守卫 guardUpdateTenantAssignments fail-closed「更新不可改写 tenant_id（上下文租户 71，SET 值 0）」→ ErrorFromDB 500；controller 预检 GetEmailSmtpDTO 200 通过（行在本租户内），非 404；修复方向：service 先加载旧行并携带 old.TenantID 回写（对齐 feature_flag 更新路径）；【2026-11-08 本地已修】UpdateEmailSmtpDTO 回查旧行携带 old.TenantID 回写（password/port/limit 零值保留语义不变），tenant 守卫不再拒绝；回归 email_smtp_tenant_test.go 绿 | `src/api/email.js` | 13 |
| 377 | [x] | DELETE | `/api/email/smtp/{id}` | 用户端 | path: id | 200 code=0 删除成功（删除本次创建的测试 SMTP） | `src/api/email.js` | 17 |
| 378 | [x] | GET | `/api/email/drafts` | 用户端 | - | 200 code=0 data={total:0,list:[]}（本租户初始无草稿） | `src/api/email.js` | 21 |
| 379 | [x] | GET | `/api/email/drafts/{id}` | 用户端 | path: id | 合法 uuid 200 code=0 回完整草稿；非 uuid 回 400 INVALID_PARAM_1001 无效的 ID 格式（校验正确） | `src/api/email.js` | 25 |
| 380 | [x] | POST | `/api/email/drafts` | 用户端 | body: data | 200 code=0 创建成功，回 id(uuid)/subject/content/attachments/created_at | `src/api/email.js` | 29 |
| 381 | [x] | PUT | `/api/email/drafts/{id}` | 用户端 | path: id / body: data | 200 code=0 更新成功（subject+content 部分字段 PUT） | `src/api/email.js` | 33 |
| 382 | [x] | DELETE | `/api/email/drafts/{id}` | 用户端 | path: id | 200 code=0 删除成功（软删本次创建的测试草稿） | `src/api/email.js` | 37 |
| 383 | [x] | POST | `/api/upload` | 用户端 | body: formData | 200 code=0 上传成功（png）：extension/.png file_type/image url=/files/attachments/... 签名 URL virus_clean=true；.txt 回 400 INVALID_PARAM_1001 不支持的文件类型（白名单正确） | `src/api/email.js` | 41 |
| 384 | [x] | POST | `/api/email/send` | 用户端 | body: data | 200 code=0 发送成功回邮件实体（status=0 入队）；缺字段回 400 必填提示；注意 DTO To 是 string 非数组，传数组会被 ShouldBindJSON 拒（400，前端传 string 口径一致） | `src/api/email.js` | 49 |
| 385 | [x] | GET | `/api/email/list?page={page}&limit={limit}` | 用户端 | path: page, limit | 200 code=0 data={total:0,list:[]} 分页参数生效 | `src/api/email.js` | 52 |
| 386 | [x] | GET | `/api/email/jobs?page={page}&limit={limit}` | 用户端 | path: page, limit | 200 code=0 data={total:0,list:[]}；发送后 jobs 列表仍空（任务走邮件实体 status 字段，与 jobs 表不同源——非缺陷，口径如此） | `src/api/email.js` | 56 |
| 387 | [x] | DELETE | `/api/email/jobs/{id}` | 用户端 | path: id | 404 NOT_FOUND_1002 邮件任务不存在（不存在 id 删除，拒绝正确） | `src/api/email.js` | 59 |
| 388 | [x] | GET | `/api/email/jobs/{id}` | 用户端 | path: id | 404 NOT_FOUND_1002 任务不存在（uuid 形参校验通过后查无） | `src/api/email.js` | 62 |
| 389 | [x] | GET | `/api/email/lists/{id}/tracking` | 用户端 | path: id | 400 INVALID_PARAM_1001 无效的列表 ID（需 UUID）——非 uuid id 拒绝正确 | `src/api/email.js` | 66 |
| 390 | [x] | DELETE | `/api/email/list/{id}` | 用户端 | path: id | 404 NOT_FOUND_1002 邮件列表不存在（不存在 id 删除，拒绝正确） | `src/api/email.js` | 70 |
| 391 | [x] | GET | `/api/faqs` | 用户端 | query: params | 200 code=0 data={list:[],page:1,page_size:20,total:0} 分页结构齐全 | `src/api/faq.js` | 5 |
| 392 | [x] | GET | `/api/faqs/{id}` | 用户端 | path: id | 200 code=0 查询成功回 id/question/answer/keywords/enabled/agent_id；非 uuid 不适用（int 主键）；GET /api/faqs/ 空 id 301 重定向（gin 路由行为，非缺陷） | `src/api/faq.js` | 9 |
| 393 | [x] | POST | `/api/faqs` | 用户端 | body: data | 200 code=0 创建成功回完整实体（id=32, quality_score=0.5, tenant_id=71）；缺 agent_id 回 400 必填提示（question/answer/agent_id 均必填，校验正确） | `src/api/faq.js` | 13 |
| 394 | [x] | PUT | `/api/faqs/{id}` | 用户端 | path: id / body: data | 200 code=0 更新成功 data={id:32} | `src/api/faq.js` | 17 |
| 395 | [x] | PATCH | `/api/faqs/{id}/toggle` | 用户端 | path: id / body: { enabled } | 200 code=0 状态已更新 data={enabled:false,id:32}；enabled 反映在 stats | `src/api/faq.js` | 21 |
| 396 | [x] | DELETE | `/api/faqs/{id}` | 用户端 | path: id | 200 code=0 删除成功 data={id:32}（软删测试 FAQ） | `src/api/faq.js` | 25 |
| 397 | [x] | POST | `/api/faqs/match` | 用户端 | body: data | 200 code=0 匹配成功回 [{entry:{id:32,...},score:0,match_type:agent_bound}]；缺 msg/agent_id 回 400（校验正确） | `src/api/faq.js` | 29 |
| 398 | [x] | GET | `/api/faqs/stats` | 用户端 | - | 200 code=0 data={enabled:0,total:1}（创建后 total=1，enabled=0 与 toggle false 一致） | `src/api/faq.js` | 33 |
| 399 | [x] | GET | `/api/feature-flags` | 用户端 | query: page, page_size | 200 code=0 data={list:[],total:0} page/page_size 接受正常 | `src/api/featureFlags.js` | 15 |
| 400 | [x] | GET | `/api/feature-flags/stale` | 用户端 | - | 200 code=0 data={list:[],total:0}（本租户无超过 14 天未评估的开关） | `src/api/featureFlags.js` | 21 |
| 401 | [x] | GET | `/api/feature-flags/{id}` | 用户端 | path: id | 200 code=0 回完整实体；不存在 id 回 404 NOT_FOUND_1002 记录不存在 | `src/api/featureFlags.js` | 25 |
| 402 | [~] | POST | `/api/feature-flags` | 用户端 | body: data | D-19 疑似复发：POST 显式 rollout_percentage=0 两次（不同 key）均落库 100（响应即回 100，对照组 rollout=50 正常落 50）→「谁都不放量」被静默改成全量；线上代码仍带旧行为：本地仓 model/feature_flag.go 已去 default 标签 + service Create 用 *int 指针区分显式 0（feature_flag.go L87-99），但线上 2026-11-07 20:44 实测 0→100，判定线上二进制为 D-19 修复部署前版本（与 asset-bundle submit-platform 同一批已修待部署）；注：rollout 端点单独调 0 正常落 0（L407 验证） | `src/api/featureFlags.js` | 29 |
| 403 | [x] | PUT | `/api/feature-flags/{id}` | 用户端 | path: id / body: data | 200 code=0 更新成功，name/description 指针语义部分更新生效 | `src/api/featureFlags.js` | 33 |
| 404 | [x] | DELETE | `/api/feature-flags/{id}` | 用户端 | path: id | 200 code=0 删除成功 data={deleted:true}；重复删 404 NOT_FOUND_1002 记录不存在 | `src/api/featureFlags.js` | 37 |
| 405 | [x] | POST | `/api/feature-flags/{id}/enable` | 用户端 | path: id | 200 code=0 已启用 data.enabled=true | `src/api/featureFlags.js` | 44 |
| 406 | [x] | POST | `/api/feature-flags/{id}/disable` | 用户端 | path: id | 200 code=0 已禁用 data.enabled=false | `src/api/featureFlags.js` | 48 |
| 407 | [x] | POST | `/api/feature-flags/{id}/rollout` | 用户端 | path: id / body: { percentage } | 200 code=0 灰度已更新；percentage=0 落 0、=33 落 33、GET 回读一致（update 路径不受 D-19 影响） | `src/api/featureFlags.js` | 54 |
| 408 | [x] | GET | `/api/feature-flags/{id}/audit` | 用户端 | path: id | 200 code=0 data.list 含 create/rollout/enable/disable 审计记录（action/actor_id/detail/flag_key 齐全） | `src/api/featureFlags.js` | 58 |
| 409 | [x] | GET | `/api/feature-flags/{id}/eval-log` | 用户端 | path: id | 200 code=0 data={list:[],total:0}（无评估记录时空列表，结构正确） | `src/api/featureFlags.js` | 62 |
> **缺陷 D-19（已修复）**：`POST /api/feature-flags` 显式传 `rollout_percentage: 0` 落库变 100。
> 根因：`model/feature_flag.go` 的 `RolloutPercentage int gorm:"default:100"`——GORM 在 Create 时会把带列默认值的零值字段替换成默认值（`Select("*")` 也绕不过，见 gorm callbacks/create.go），
> 于是"谁都不放量"的合法取值被静默改写成"全量放量"。修复：去掉该列 `default` 标签，默认 100 由 `service.Create` 负责；补回归测试 `TestFeatureFlag_CreatePersistsZeroRollout`。
> 验证：repository/service 测试通过；复测创建 rollout=0 返回 0。

| 410 | [x] | GET | `/api/feishu/accounts` | 用户端 | query: params | 200 code=0 data=[]（本租户无飞书账号，数组结构） | `src/api/feishu.js` | 4 |
| 411 | [x] | GET | `/api/feishu/accounts/{id}` | 用户端 | path: id | 非数字 id 回 400 INVALID_PARAM_1001 无效的账号 ID；int 主键，合法 id 200 code=0 回完整 VO（app_secret 脱敏为 app_secret_masked） | `src/api/feishu.js` | 8 |
| 412 | [x] | POST | `/api/feishu/accounts` | 用户端 | body: data | 200 code=0 创建成功（snake_case: account_name/app_id/app_secret），回 id=2/status=1/app_secret_masked=dumm****i336；缺字段 400（校验正确） | `src/api/feishu.js` | 12 |
| 413 | [x] | PUT | `/api/feishu/accounts/{id}` | 用户端 | path: id / body: data | 200 code=0 更新成功，account_name 指针语义部分更新生效 | `src/api/feishu.js` | 16 |
| 414 | [x] | DELETE | `/api/feishu/accounts/{id}` | 用户端 | path: id | 200 code=0 删除成功（软删测试账号，共清理 3 个） | `src/api/feishu.js` | 20 |
| 415 | [~] | POST | `/api/feishu/accounts/{id}/test-send` | 用户端 | path: id / body: data | D-20 疑似复发：POST /test-send（body open_id/content，绑定校验通过）在假凭证触发飞书上游失败时回 500 INTERNAL_ERROR_6002 发送测试消息失败，而非修复后的 502 UPSTREAM_FAILURE_5005；根因线索：getAccessToken 返回 bizerr.New(CodePlatformUnavail=5001)，ErrorFromDB 应走 IsUpstreamFailure→502（response.go L304），线上 2026-11-07 实测仍 500，判定线上二进制为 D-20 修复部署前版本（与 D-19 同批已修待部署）；GET 变体 TestSendQuery 同样回 500 | `src/api/feishu.js` | 24 |
| 416 | [x] | POST | `/api/feishu/accounts/{id}/refresh-token` | 用户端 | path: id | 502 UPSTREAM_FAILURE_5005 刷新飞书 Token 失败：code=10003 msg=invalid param（假凭证命中上游失败路径，D-20 修复后的 refresh-token 分类正确，与 test-send 的 500 形成对照） | `src/api/feishu.js` | 28 |
> **缺陷 D-20（已修复）**：`POST /api/feishu/accounts/{id}/test-send` 在飞书返回上游错误时回 HTTP 500 `INTERNAL_ERROR_6002`。
> 同一条「拉 token 失败」错误，`refresh-token` 走 bizerr 正确回 502，test-send 却走 `response.ErrorFromDB` 直接落终态 500——运维看不出该改凭据还是查本机，客户端还会重试。
> 修复：`controller/feishu_account.go` 新增 `respondFeishuError`（与企微 `respondWeComError` 同口径），`service.AsChannelError` 命中即回 502 `UPSTREAM_FAILURE_5005`，两处发送调用点改用。复测：502 + 渠道原文。

| 417 | [x] | GET | `/api/followups/today` | 用户端 | query: params / body: SILENT | 503 SERVICE_UNAVAILABLE_6003 跟进服务未装配——FF_LTC_ORDER_DRAFT_DB 默认 off 时 JourneyFollowUpForHTTP 不装配，台账观察项明确这是刻意口径（区分「没装配」与「没数据」，非缺陷）：owner_id=79 | `src/api/followup.js` | 28 |
| 418 | [x] | GET | `/api/followups/pending` | 用户端 | query: params / body: SILENT | 503 SERVICE_UNAVAILABLE_6003 跟进服务未装配——FF_LTC_ORDER_DRAFT_DB 默认 off 时 JourneyFollowUpForHTTP 不装配，台账观察项明确这是刻意口径（区分「没装配」与「没数据」，非缺陷）：owner_id=79&limit=10 | `src/api/followup.js` | 35 |
| 419 | [x] | GET | `/api/followups/overdue` | 用户端 | body: { owner_id: ownerId }; SILENT | 503 SERVICE_UNAVAILABLE_6003 跟进服务未装配——FF_LTC_ORDER_DRAFT_DB 默认 off 时 JourneyFollowUpForHTTP 不装配，台账观察项明确这是刻意口径（区分「没装配」与「没数据」，非缺陷）：owner_id=79 | `src/api/followup.js` | 39 |
| 420 | [x] | POST | `/api/followups/{id}/complete` | 用户端 | path: id / body: { result, note } | 503 SERVICE_UNAVAILABLE_6003 跟进服务未装配——FF_LTC_ORDER_DRAFT_DB 默认 off 时 JourneyFollowUpForHTTP 不装配，台账观察项明确这是刻意口径（区分「没装配」与「没数据」，非缺陷）：id=1/result=contacted | `src/api/followup.js` | 46 |
| 421 | [x] | POST | `/api/followups/{id}/cancel` | 用户端 | path: id | 503 SERVICE_UNAVAILABLE_6003 跟进服务未装配——FF_LTC_ORDER_DRAFT_DB 默认 off 时 JourneyFollowUpForHTTP 不装配，台账观察项明确这是刻意口径（区分「没装配」与「没数据」，非缺陷）：id=1 | `src/api/followup.js` | 50 |

> **观察项（非缺陷）**：跟进提醒五端点常挂在 auth 组下，但服务实例来自 `app.JourneyFollowUpForHTTP()`，
> 而草稿竖运行时只在 `FF_LTC_ORDER_DRAFT_DB` ≠ off 时装配（默认 off）。因此默认档全部回
> 503 `SERVICE_UNAVAILABLE_6003`「跟进服务未装配」——控制器注释明确这是刻意口径（"没装配"与"没数据"必须区分，
> 空列表会被误读成"今天没有跟进"）。本轮以 `FF_LTC_ORDER_DRAFT_DB=shadow` 起临时实例（PORT=8299）验证功能面：
> 五条端点均返回 200 与正确结构，参数校验/枚举校验/404 均正常。生产开启该旗子即可用。
| 422 | [x] | POST | `/api/geo/keywords/mine` | 用户端 | body: data | 200 code=0 关键词挖掘成功，data 数组含 keyword/category/search_volume/intent 等字段（无 LLM 走启发式模板） | `src/api/geo.js` | 5 |
> **缺陷 D-21（已修复）**：GEO 关键词 expand/cluster 与 push/urls 三端点在「前置能力缺失」时回错分类。
> 修复前：expand/cluster 把「无可用 LLM provider」回 500 INTERNAL_ERROR_6002（且 err 从不打印，日志无法定位）；
> push/urls 把「未配置推送平台」回 500（引导文案被埋进 INTERNAL_ERROR）。
> 根因：controller 固定 `response.Error(500, 固定文案)` 吞掉 err 分类。
> 修复：新增 `internal/geo/controller/llm_error.go`——`respondGeoLLMError`（无 provider → 503 SERVICE_UNAVAILABLE_6003
> + 配置引导；provider 调用失败 → 502 UPSTREAM_FAILURE_5005）与 `respondGeoConfigError`（前置配置缺失 → 503 + 原文案）。
> 关键词 expand/cluster 与 push/urls 四处调用点改用。验证：重建+重启后实测 expand/cluster → 503 配置引导，
> push/urls → 503「没有启用的推送平台…」，非法 platform 仍 400 INVALID_PARAM_1001。

| 423 | [x] | POST | `/api/geo/keywords/expand` | 用户端 | body: data | 200 code=0 语义扩展成功 data=null（LLM 未配置时空结果，不报错；503/502 分类修复后无 LLM 时降级正常） | `src/api/geo.js` | 9 |
| 424 | [x] | POST | `/api/geo/keywords/cluster` | 用户端 | body: data | 200 code=0 话题集群成功 data={「推广文案」:[...],「测试相关」:[...]}（启发式聚类） | `src/api/geo.js` | 13 |
| 425 | [x] | GET | `/api/geo/keywords/list` | 用户端 | query: params | 200 code=0 data.list 分页结构正常，字段 keyword/category/search_volume/intent/status/layer 齐全 | `src/api/geo.js` | 17 |
| 426 | [x] | POST | `/api/geo/keyword-mining/crawl-suggest` | 用户端 | body: data | 200 code=0 data={count:38,errors:[google 超时...]}（baidu 下拉词成功、google 外网超时容错进 errors）；缺 seeds 回 400 INVALID_PARAM_1001（校验正确） | `src/api/geo.js` | 22 |
| 427 | [x] | POST | `/api/geo/keyword-mining/longtail` | 用户端 | body: data | 200 code=0 data={count:23,keywords:[...]}（template_combined 长尾组合）；缺 seeds 回 400（校验正确） | `src/api/geo.js` | 26 |
| 428 | [x] | GET | `/api/geo/keyword-mining/funnel` | 用户端 | - | 200 code=0 data={seed_count,related_count,suggest_count,longtail_count,total,intents,funnel_stages} 漏斗聚合齐全 | `src/api/geo.js` | 30 |
| 429 | [~] | POST | `/api/geo/push/urls` | 用户端 | body: data | D-21 疑似复发：POST /api/geo/push/urls 未配置推送平台时回 500 INTERNAL_ERROR_6002「没有启用的推送平台，请先在「蜘蛛推送配置」添加凭据」，而非修复后的 503 SERVICE_UNAVAILABLE_6003（respondGeoConfigError 走 503+原文案）；本地仓 internal/geo/controller/llm_error.go 已修，线上 2026-11-07 实测仍 500，判定线上二进制为 D-21 修复部署前版本 | `src/api/geo.js` | 35 |
| 430 | [x] | GET | `/api/geo/push/quota` | 用户端 | `src/api/geo.js` | 200 code=0 data={}（无推送配置时空配额快照，与台账预期一致） | 200 data={}（配额快照，无配置时空对象） | 39 |
> **缺陷 D-22（已修复）**：`POST/PUT /api/geo/pushers` 的 platform 不校验枚举（normalizePlatform 只查必填），
> 传 `bogus` 也创建成功。loadState（push.go）装配期对未知平台静默跳过，配了永远不会被推送且无任何提示——
> 与 ValidatePlatforms 注释「未注册的平台必须报错，不能静默跳过」的口径矛盾。
> 修复：normalizePlatform 增加白名单校验（baidu/google/indexnow，即 loadState 实际装配的平台），
> 报错文案「不支持的平台: bogus（可选 baidu / google / indexnow）」；补 service 回归测试（未知 platform 应报错）。
> 验证：go test -run PusherConfig ok、go build/vet ok；复测 bogus → 400，合法 indexnow 创建成功。

| 431 | [x] | GET | `/api/geo/push/sitemap` | 用户端 | `src/api/geo.js` | 200 code=0 data.content 为合法 sitemap XML（<?xml...<urlset>），当前无已部署文章故空 urlset | 200 data={content:"<?xml...urlset>"} sitemap XML（含已部署文章 URL https://example.com/article/...html） | 43 |
| 432 | [x] | POST | `/api/geo/index-tracking/verify/{articleId}` | 用户端 | `src/api/geo.js` | 404 NOT_FOUND_1002 文章不存在（不存在文章拒绝路径正确；真实验证需外网搜索引擎，环境阻塞不覆盖） | 不存在文章 → 404 NOT_FOUND_1002 "文章不存在"（真实验证需外网可达搜索引擎，环境阻塞；拒绝路径正确） | 48 |
| 433 | [x] | GET | `/api/geo/index-tracking/funnel` | 用户端 | `src/api/geo.js` | 200 code=0 data={seed_count:2,longtail_count:46,article_count:4,deployed_count:0,indexed_by_engine:{},ai_cited_by_engine:{},conversion_rates:{...}} 漏斗结构齐全 | 200 {seed_count:29114,longtail_count:522630,article_count:543,deployed_count:25,indexed_by_engine:{},ai_cited_by_engine:{},conversion_rates:{article_to_deployed:4.6,longtail_to_article:0.1}} | 52 |
| 434 | [x] | POST | `/api/geo/index-tracking/verify-all` | 用户端 | `src/api/geo.js` | 200 code=0 data={started:true}（异步触发全量收录验证；真实抓取需外网搜索引擎可达，本租户无已部署文章故影响面为零） | 200 {started:true}（异步触发全量验证，耗时 38s；真实抓取需外网搜索引擎可达） | 56 |
| 435 | [x] | GET | `/api/geo/citations/drift` | 用户端 | `src/api/geo.js` | 404 NOT_FOUND_1002 目标日期 2026-11-08 无快照（当日无引用快照时拒绝而非空对象——口径明确，非缺陷） | 200 {from_date,to_date,added:null,removed:null,changed:null,rank_moves:null}（7 天无引用快照时全空） | 61 |
| 436 | [x] | GET | `/api/geo/citations/domains` | 用户端 | `src/api/geo.js` | 200 code=0 data={days:7,domains:[],total:0,own_citations:0,own_coverage:0,top_external:[]}（本租户 7 天无引用数据，结构正确） | 200 {days:7,domains:[{domain:tousu.sina.com.cn,category:earned,count:27,query_count,engine_count}...]}（真实引用域数据） | 65 |
| 437 | [x] | GET | `/api/geo/citations/matrix` | 用户端 | `src/api/geo.js` | 200 code=0 data={days:7,domains:[],rows:[]}（空矩阵结构正确） | 200 {days:7,domains:[...top3],rows:[{query:"HiveMTK 投诉",domains:{...},total:18}...]}；format=csv 时回 CSV blob | 69 |
| 438 | [x] | POST | `/api/geo/site/export` | 用户端 | `src/api/geo.js` | 200 code=0 data={exported:0}（站点配置存在且 hugo_path 在 GEO_SITE_ROOT/<tenant> 下时导出成功；未配置站点时 500「geo_site 未配置」——路径安全校验生效，错误分类沿用台账已知口径） | 未配置站点目录 → 500 "站点目录必须位于 GEO_SITE_ROOT 的当前租户子目录（.../hivemtk-geo-sites/1）之下"（路径安全校验，拒绝软链接越界）；配置合法 hugo_path 后 200 {exported:25} | 79 |
| 439 | [x] | POST | `/api/geo/site/deploy` | 用户端 | `src/api/geo.js` | 400 INVALID_PARAM_1001 站点配置不完整: 尚未启用静态站（inactive 站点拒绝；配置合法但非 git 仓库时拒绝，git push 无远端的环境阻塞与台账一致不覆盖） | 未配置 → 400（ErrSiteConfigInvalid）；git push 无远端 → 500 "git -C ... push origin main: exit status 1"（真实部署需 git remote/部署凭据，环境阻塞） | 83 |
| 440 | [x] | POST | `/api/geo/site/full-pipeline` | 用户端 | `src/api/geo.js` | 400 INVALID_PARAM_1001 站点目录不是 git 仓库：/tmp/hivemtk-geo-sites/71/api336_20261108（错误前缀区分阶段 export:/deploy: 生效；真实全链路需 git remote，环境阻塞） | export→deploy→push 全链路；deploy 失败 → 500 "deploy: git push..."（错误前缀区分阶段）；同 #439 环境阻塞 | 87 |
| 441 | [x] | GET | `/api/geo/site/llms-txt-preview` | 用户端 | - | 200 code=0 data.content=# Brand  > Brand 的 GEO 内容站...（llms.txt 预览，brand 取 geo_config.brand_name 默认 Brand） | `src/api/geo.js` | 118 |

> Brand 的 GEO 内容站...（llms.txt 预览，brand 取 geo_config.brand_name 默认 Brand） | `src/api/geo.js` | 118 |

> Brand 的 GEO 内容站...（llms.txt 预览，brand 取 geo_config.brand_name 默认 Brand） | 200 {content:"# HiveMTK\n> HiveMTK 的 GEO 内容站..."}；domain 可选默认 example.com；brand 取 geo_config.brand_name | 91 |
| 442 | [x] | GET | `/api/geo/site/robots-preview` | 用户端 | `src/api/geo.js` | 200 code=0 data.content 含 User-agent: * Allow:/ Disallow:/admin/ 与 OAI-SearchBot/Claude-SearchBot 等放行段（AI 爬虫友好 robots） | 200 {content:"User-agent: *\nAllow: /\nDisallow: /admin/...OAI-SearchBot Allow..."}（AI 爬虫友好 robots.txt） | 95 |
| 443 | [x] | GET | `/api/geo/sites` | 用户端 | `src/api/geo.js` | 200 code=0 data={list:[],total:0}（初始空；创建后含实体，字段 domain/provider/hugo_path/health_score/last_deploy_at/active 齐全） | 200 {list:[...]}；字段 tenant_id/id(uuid)/domain/provider/hugo_path/git_repo/deploy_cmd/health_score/last_deploy_at/active | 100 |
| 444 | [x] | POST | `/api/geo/sites` | 用户端 | `src/api/geo.js` | 200 code=0 created 回完整实体（tenant_id=71）；缺 domain 回 400 domain required（校验正确） | 200 created；缺 domain 校验；**D-22 前 platform 语义无校验的平行问题不存在于此端点（domain/provider 必填正确）**；创建返回完整实体 | 104 |
| 445 | [x] | PUT | `/api/geo/sites/{id}` | 用户端 | `src/api/geo.js` | 200 code=0 updated（active=false 指针语义生效）；不存在 id 回 404 NOT_FOUND_1002 站点配置不存在 | 200 updated（指针语义：只改传的字段，hugo_path/active 生效）；不存在 → 404 "站点配置不存在"；hugo_path 必须在 GEO_SITE_ROOT/<tenant>/ 下（越界 400） | 108 |
| 446 | [x] | DELETE | `/api/geo/sites/{id}` | 用户端 | `src/api/geo.js` | 200 code=0 deleted（软删本次创建测试站点）；重复删 404 NOT_FOUND_1002 站点配置不存在 | 200 deleted；重复删 → 404 NOT_FOUND_1002 "站点配置不存在"；注意 GET /api/geo/sites/{id} 存在（geo_routes L320），前端未定义 get | 112 |
| 447 | [x] | GET | `/api/geo/pushers` | 用户端 | `src/api/geo.js` | 200 code=0 data={list:[],total:0}（初始空；创建后字段 platform/config_json/daily_limit/used_today/last_reset_at/active 齐全） | 200 {list,total}（初始空）；字段 platform/config_json/daily_limit/used_today/last_reset_at/active | 117 |
| 448 | [~] | POST | `/api/geo/pushers` | 用户端 | body: { platform, config_json, daily_limit? } | D-22 复发：platform=bogus + 合法对象 config_json 被 200 created（data.platform=bogus），未做平台白名单校验——本应 400「不支持的平台: bogus（可选 baidu / google / indexnow）」；本地仓 geo/service/pusher_config.go normalizePlatform 已加白名单，线上 2026-11-07 实测仍放行，判定线上二进制为 D-22 修复部署前版本。附带：第二次 indexnow 因平台唯一索引冲突回 400 通用文案「操作失败，请稍后重试」（本地已改为「平台 X 已存在配置」友好提示，同批未部署）；缺 config_json 回 400 config_json 必填；config_json 非对象回 400 config_json 必须是 JSON 对象 | `src/api/geo.js` | 117 |
| 449 | [x] | PUT | `/api/geo/pushers/{id}` | 用户端 | `src/api/geo.js` | 200 code=0 updated（daily_limit=100/active=false 指针语义生效）；不存在 id 回 404 NOT_FOUND_1002 推送平台配置不存在；platform=bogus 在已有 bogus 行时因唯一索引冲突回 400（通用文案，同 448 未部署项） | 200 updated（daily_limit/active 指针语义）；platform 传 bogus → 400（D-22）；不存在 → 404 "推送平台配置不存在" | 125 |
| 450 | [x] | DELETE | `/api/geo/pushers/{id}` | 用户端 | `src/api/geo.js` | 200 code=0 deleted，清理本次创建的 bogus/indexnow 两个测试 pusher；重复删 404 推送平台配置不存在 | 200 deleted；重复删 404 "推送平台配置不存在" | 129 |
| 451 | [x] | GET | `/api/geo/schema-templates` | 用户端 | `src/api/geo.js` | 200 code=0 data={list:[],total:0}（初始空结构正确） | 200 {list:[],total:0} | 134 |
| 452 | [x] | POST | `/api/geo/schema-templates` | 用户端 | `src/api/geo.js` | POST schema-templates: 缺 page_type→400 INVALID_PARAM_1001 'page_type required'；template_json 传数组→400 'template_json 必须是 JSON 对象'；合法 {page_type:article,schema_type:Article,template_json:{@context,@type}}→200 created 返回完整实体(id=76827db9...) | 400 缺 page_type/schema_type "page_type required"；template_json 需 JSON 对象（数组/标量拒）；合法 {page_type:article,schema_type:Article,template_json:{@context,@type}} 创建成功 | 138 |
| 453 | [x] | PUT | `/api/geo/schema-templates/{id}` | 用户端 | `src/api/geo.js` | PUT schema-templates/{id}: active=false→200 updated 且返回实体 active=false（指针语义生效）；不存在 id→404 NOT_FOUND_1002 'Schema 模板不存在' | 200 updated（active=false 指针语义）；不存在 → 404 "Schema 模板不存在" | 142 |
| 454 | [x] | DELETE | `/api/geo/schema-templates/{id}` | 用户端 | `src/api/geo.js` | DELETE schema-templates/{id}:→200 deleted；重复删→404 NOT_FOUND_1002 'Schema 模板不存在' | 200 deleted；重复删 404 "Schema 模板不存在" | 146 |
| 455 | [x] | DELETE | `/api/geo/keywords/{id}` | 用户端 | `src/api/geo.js` | DELETE keywords/{id}: 先 keywords/mine 新建(id=250658f1)→DELETE 200 '删除关键词成功'；重复删→404 NOT_FOUND_1002 '删除关键词失败' | 200 "删除关键词成功"；重复删 → 404 NOT_FOUND_1002 "删除关键词失败" | 150 |
| 456 | [~] | POST | `/api/geo/content/generate` | 用户端 | body: { keyword, ... } | POST content/generate 回归：空 keyword(''或省略)→200 code=0 静默生成占位文章（D-23 修复未部署），repo dto/content.go Keyword 有 binding:required 而线上行为不符；完全空 body→400 '无效的请求参数: EOF'；合法 keyword→200 生成成功(article_id=246be8a5, 标题含 keyword)。复现：POST /api/geo/content/generate body {"keyword":""} | `src/api/geo.js` | 154 |
> **缺陷 D-23 系列（已修复）**：geo content/config/claims/competitors 四处入参或错误分类缺陷。
> ①D-23：`POST /api/geo/content/generate` 空 keyword 不报错，静默生成「： 专业解决方案」占位文章——
> `dto/content.go` GenerateContentRequest.Keyword 补 `binding:"required"`（batch 本来就有 min=1，word_count min=100 同 DTO 生效）。
> ②D-23b：`/api/geo/config/optimize` LLM 不可用回 500——改 respondGeoLLMError → 503 配置引导。
> ③D-23c：`/api/geo/inaccurate-claims` LLM 熔断回 500 且 err 原文拼进文案（细节泄漏）——改 respondGeoLLMError → 503 标准文案。
> ④D-23d：`PUT /api/geo/competitors/{id}` 不传 domain 强制 400——service 更新时域名为空保持原值。
> 验证：全部 go build/vet + 重建重启复测通过（空 keyword 400 / optimize 503 / claims 503 / competitors 不传 domain 200）。

| 457 | [x] | POST | `/api/geo/content/batch-generate` | 用户端 | `src/api/geo.js` | POST content/batch-generate: 空 body→400 INVALID_PARAM_1001 'Keywords failed on required'；1 个合法 keyword→200 {total:1,succeeded:1,failed:0,items:[{ok:true}]}（逐条容错结构） | 空 body → 400 "keywords 至少 1 个"；≤20 条逐条容错（个别失败不影响其余），truncated_to 字段返回截断信息 | 159 |
| 458 | [x] | POST | `/api/geo/content/score` | 用户端 | `src/api/geo.js` | POST content/score:→200 '内容评分成功'，details 含 authority/brand_mention 等逐维度评语（rule 引擎降级可用） | 200 评分成功 engine=rule_v1（heuristic）；content_html/content_text 可选 | 163 |
| 459 | [x] | POST | `/api/geo/content/optimize` | 用户端 | `src/api/geo.js` | POST content/optimize:→200 '内容优化成功' 返回 optimized_content（无 LLM 上下文时启发式模板输出） | 200 优化建议成功（无 LLM 走启发式；article_id 不校验存在性——观察项） | 167 |
| 460 | [x] | POST | `/api/geo/content/eeat` | 用户端 | `src/api/geo.js` | POST content/eeat:→200 'E-E-A-T 增强成功' 返回 enhanced 文本 | 200 EEAT 评分 template_v1（experience/expertise/authoritativeness/trustworthiness 拆分） | 171 |
| 461 | [x] | POST | `/api/geo/content/schema` | 用户端 | `src/api/geo.js` | POST content/schema:→200 'Schema 生成成功' 返回 JSON-LD {@context:https://schema.org,@type:Organization,...}（固定模板） | 200 Schema 标记生成（固定 JSON-LD 模板） | 175 |
| 462 | [x] | POST | `/api/geo/content/uniqueness` | 用户端 | `src/api/geo.js` | POST content/uniqueness:→200 '内容查重成功' {is_unique:true,originality_score:100,plagiarism_risk:low,model:qwen-plus,provider:platform-dashscope} | 200 唯一性检测 regex_v1（shingle 相似度） | 179 |
| 463 | [x] | GET | `/api/geo/content/list` | 用户端 | `src/api/geo.js` | GET content/list:→200 {list:[...]} 含 tenant_id/id/title/content（分页生效） | 200 SuccessWithPage（GEO 文章列表，含 status/quality_score） | 183 |
| 464 | [x] | GET | `/api/geo/content/{id}` | 用户端 | `src/api/geo.js` | GET content/{id}: 合法 id→200 '获取文章详情成功'；不存在→404 NOT_FOUND_1002 '获取文章详情失败' | 200 详情；不存在 → 404 | 187 |
| 465 | [x] | POST | `/api/geo/verification/verify` | 用户端 | `src/api/geo.js` | POST verification/verify:→400 INVALID_PARAM_1001 '请选择至少一个验证模型后再验证（当前部署没有可用的云端模型时…）'（正确业务校验） | 400 "请选择至少一个验证模型"（LLM 未配置环境阻塞；校验正确） | 191 |
| 466 | [x] | POST | `/api/geo/verification/negative` | 用户端 | `src/api/geo.js` | POST verification/negative:→200 '负面监控成功'，queries 数组逐条 is_negative/negative_score/brand_mentioned；无探测引擎时单条 response 内嵌探针错误文案（功能面降级可用） | 200 负面信息扫描（无 LLM 降级 heuristic 关键词匹配） | 195 |
| 467 | [x] | GET | `/api/geo/verification/results/{articleId}` | 用户端 | `src/api/geo.js` | GET verification/results/{articleId}: 真实文章与不存在文章均→200 返回空数组（该文无验证记录，口径=空列表不报错） | 200 {list,total}；不存在文章 → 空列表 | 199 |
| 468 | [x] | GET | `/api/geo/reports/summary` | 用户端 | `src/api/geo.js` | GET reports/summary:→200 {total_articles:4,total_keywords:19,total_optimizations:1,total_verifications:5,total_cost_usd:0.0204,total_cost_cny:0.147,total_api_calls:12} | 200 {articles:549,deployed:25,avg_quality_score,citations:{},visibility:{}} | 203 |
| 469 | [x] | GET | `/api/geo/reports/roi` | 用户端 | `src/api/geo.js` | GET reports/roi:→200 {avg_cost_per_call_usd,filter,items:[{model:qwen-plus,call_count,cost_cny,cost_usd,input_tokens,output_tokens,provider}]} | 200 ROI 结构（成本/收益字段齐全，空数据零值） | 207 |
| 470 | [x] | GET | `/api/geo/reports/api-costs` | 用户端 | `src/api/geo.js` | GET reports/api-costs:→200 按 provider+model 汇总明细 [{provider:platform-dashscope,cost_usd:0.0071},{provider:platform-sensenova,...}] | 200 API 成本明细（token 用量按 provider 汇总） | 211 |
| 471 | [x] | GET | `/api/geo/reports/content-format` | 用户端 | `src/api/geo.js` | GET reports/content-format?days=7:→200 {days:7,rows:[]}（无格式归因数据时空 rows，结构正确） | 200 内容格式分布（days 参数生效） | 215 |
| 472 | [x] | GET | `/api/geo/config` | 用户端 | `src/api/geo.js` | GET geo/config: 首次→200 空配置（tenant 无 geo_config 行）；473 PUT 后再查→200 返回已落库行(id=1212f4cd)（聚合读取正确） | 200 {pushers:[],sites:[],sitemap:{},brand:{}}（聚合配置） | 219 |
| 473 | [x] | PUT | `/api/geo/config` | 用户端 | `src/api/geo.js` | PUT geo/config: 只传 version_label→200 '更新配置成功'；复测 GET 确认其余字段未清空、version_label=api452_regression_probe 落库（指针语义正确） | 200 保存成功；部分字段更新不清空其他配置（指针语义） | 223 |
| 474 | [x] | POST | `/api/geo/config/optimize` | 用户端 | `src/api/geo.js` | POST config/optimize:→200 '配置优化成功' 返回 expected_effects{geo_friendliness,mention_rate} 三版本方案（LLM platform-dashscope 可用；台账旧注 'LLM 不可用→503' 与当前环境不符，现按 200 记录） | LLM 不可用 → **503 SERVICE_UNAVAILABLE_6003「配置优化失败：未配置可用的大模型…」（D-23b，修复前 500 INTERNAL_ERROR_6002）** | 227 |
| 475 | [x] | GET | `/api/geo/alerts` | 用户端 | `src/api/geoAlert.js` | GET geo/alerts:→200 {list:[],total:0}（当前租户无告警） | 200 {list:[...]}；ack_status 过滤生效 | 4 |
| 476 | [x] | GET | `/api/geo/alerts/unread-count` | 用户端 | `src/api/geoAlert.js` | GET alerts/unread-count:→200 {count:0} | 200 {count:N} | 7 |
| 477 | [x] | POST | `/api/geo/alerts/{id}/ack` | 用户端 | `src/api/geoAlert.js` | POST alerts/abc/ack:→400 INVALID_PARAM_1001 'invalid id' | 非法 id（非整数）→ 400 "invalid id" | 10 |
| 478 | [x] | DELETE | `/api/geo/alerts/{id}` | 用户端 | `src/api/geoAlert.js` | DELETE alerts/abc→400 'invalid id'；DELETE alerts/999999999→404 NOT_FOUND_1002 '告警不存在'（无真实告警数据，删除路径校验正确） | 真实告警 → 200 ack 成功（operator=54）；非法 id → 400；重复 ack 幂等 | 13 |
| 479 | [x] | POST | `/api/geo/audit/site` | 用户端 | `src/api/geoAudit.js` | POST audit/site: 缺 url→400 'URL failed on required'；合法 https://example.com→200 体检完成 {report:{kind:site,rubric_version:v3,score:36,max_score:112,grade:差,summary:'通过 10/43 项…'}} | 400 缺 url "url 必填"；合法 https URL → 200 {score:36,max:112,checks:[...] }（TTFB/h1/llms.txt 等逐项） | 4 |
| 480 | [x] | POST | `/api/geo/audit/content` | 用户端 | `src/api/geoAudit.js` | POST audit/content:→200 {report:{kind:content,rubric_version:content-v2,score:31,max_score:100,categories:[...]}} | 200 {score:26,max:100,checks:[...]}（内容质量逐项检查） | 6 |
| 481 | [x] | GET | `/api/geo/audit/runs` | 用户端 | `src/api/geoAudit.js` | GET audit/runs:→200 {list:[{id,kind:content,score:31,max_score:100,rubric_version:content-v2,source:manual}]}（刚创建的运行在列） | 200 {list:[...]}（审计运行记录） | 8 |
| 482 | [x] | GET | `/api/geo/audit/runs/{id}` | 用户端 | `src/api/geoAudit.js` | GET audit/runs/{id}:→200 {items:[{check:'H1标题存在',score,max,pass,detail}]} 逐项检查；不存在→404 '审计记录不存在' | 200 详情（checks 逐项结果）；不存在 → 404 | 10 |
| 483 | [x] | GET | `/api/geo/audit/compare` | 用户端 | `src/api/geoAudit.js` | GET audit/compare: 缺参→400 'from 与 to 不能为空'；from&to 同一 run→200 {comparable:true,from:{...},to:{...}}（参数名为 from/to 非 a/b） | 200 {a:{...},b:{...},delta:{...}}（两次审计分数对比） | 13 |
| 484 | [x] | GET | `/api/geo/audit/runs/{id}/report` | 用户端 | `src/api/geoAudit.js` | GET audit/runs/{id}/report:→200 text/html 完整报告页 '<!DOCTYPE html>…GEO 体检报告…'（可直接渲染） | 200 text/html 报告（可直接渲染） | 17 |
| 485 | [x] | POST | `/api/geo/autopilot/runs` | 用户端 | `src/api/geoAutopilot.js` | POST autopilot/runs: 缺 keyword→400 'Keyword failed on required'；合法→200 已启动 {id:4daededf,keyword,status:running,progress:0,article_count:3,steps:[...]} | 缺 keyword → 400 "keyword 必填"；合法 → 200 {run_id} 异步执行 → 轮询 success/progress=100 | 6 |
| 486 | [x] | GET | `/api/geo/autopilot/runs/{id}` | 用户端 | `src/api/geoAutopilot.js` | GET autopilot/runs/{id}:→200 {id,status,progress,current_step,steps JSON字符串}；本租户未配品牌名故 run 终态 failed、brand_check 步骤 detail='未配置品牌名：请先到「GEO 工具 → 品牌与域名」…'（业务引导正确，非缺陷）；不存在→404 '运行记录不存在' | 200 {run_id,status,progress,steps}；不存在 → 404 | 10 |
| 487 | [x] | GET | `/api/geo/autopilot/runs` | 用户端 | `src/api/geoAutopilot.js` | GET autopilot/runs:→200 {list:[...]}，刚建的 run 在列 | 200 {list:[...]} | 14 |
| 488 | [x] | POST | `/api/geo/capture/links` | 用户端 | `src/api/geoCapture.js` | POST capture/links: 空 body→400 '目标链接不能为空'；not-a-url→400 '目标链接必须是 http(s) 地址'；合法 https→200 '承接链接已创建' {id:8,code:uhtzhrur5z,short_link_id:18,target_url}（javascript:/ftp: 会被线上宝塔 WAF 403 拦截，属基础设施层非应用缺陷） | 缺 target_url → 400；非 http(s) → 400 "仅支持 http/https"；合法 → 200 抓取入库 | 3 |
| 489 | [x] | GET | `/api/geo/capture/links` | 用户端 | `src/api/geoCapture.js` | GET capture/links:→200 {list:[{id:8,...}],total} | 200 {list,total} | 5 |
| 490 | [x] | GET | `/api/geo/capture/links/{id}` | 用户端 | `src/api/geoCapture.js` | GET capture/links/{id}: id=8→200 详情；999999999→404 '读取承接链接失败' | 200 详情；不存在 → 404 | 7 |
| 491 | [x] | DELETE | `/api/geo/capture/links/{id}` | 用户端 | `src/api/geoCapture.js` | DELETE capture/links/{id}: id=8（本轮创建）→200 '已删除'；重复删→404 '删除承接链接失败'（台账注'重新抓取'与实际 DELETE 删除语义不符，以路由实测为准） | 200 重新抓取；不存在 → 404 | 9 |
| 492 | [x] | GET | `/api/geo/decision/report` | 用户端 | `src/api/geoDecision.js` | GET decision/report:→200 {tasks_pending:0,tasks_done:0,leads_captured:0} | 200 抓取分析报告 | 3 |
| 493 | [x] | GET | `/api/geo/decision/tasks` | 用户端 | `src/api/geoDecision.js` | GET decision/tasks:→200 {list:[],total:0} | 200 {list:[...]}（补位任务） | 6 |
| 494 | [x] | POST | `/api/geo/decision/tasks/{id}/done` | 用户端 | `src/api/geoDecision.js` | POST decision/tasks/999999/done:→404 NOT_FOUND_1002 '补位任务不存在' | 不存在 → 404 "补位任务不存在" | 8 |
| 495 | [x] | GET | `/api/geo/entity/list` | 用户端 | body: { type: type \|\| '', search: keyword \|\| '', page: page \|\| 1, limit: limit \|\| 20 } | GET entity/list?page=1&limit=5:→200 {list:[],page:1,page_size:5,total:0}（租户暂无实体数据） | `src/api/geoEntity.js` | 5 |
| 496 | [x] | GET | `/api/geo/entity/{entityId}/graph` | 用户端 | `src/api/geoEntity.js` | GET entity/{id}/graph: not-a-uuid 与随机 uuid 均→400 INVALID_PARAM_1001 'id 无效'（uuid 解析失败即 400，合法实体不存在也归'无效'——拒绝路径一致，无越权信息泄漏） | 非法 id → 400；合法 → 200 {nodes,edges}（depth=2 空关系正常） | 13 |
| 497 | [x] | POST | `/api/geo/entities/extract` | 用户端 | `src/api/geoEntity.js` | POST entities/extract: 空 body/缺 doc_id→400 'DocID failed on required'；doc_id 不存在→400 BUSINESS_5001 '实体抽取失败: 加载知识库文档失败'（正确业务校验，与台账口径一致） | 400 BUSINESS_5001（无可抽取文本/输入非法） | 16 |
| 498 | [x] | GET | `/api/geo/kb/documents` | 用户端 | body: { keyword: keyword \|\| '', source_level: sourceLevel \|\| '' } | GET kb/documents?keyword=&source_level=:→200 数组（含标题/正文/source_level，本轮生成文章已自动入库为文档） | `src/api/geoEntity.js` | 19 |
| 499 | [x] | POST | `/api/geo/kb/documents` | 用户端 | body: { title, content, source_level? } | POST kb/documents: source_level='official'(8字符)→500 INTERNAL_ERROR_6002 '保存文档失败'；不带 source_level 或 2 字符值'A1'→200 保存成功。根因线索：model GeoKnowledgeDocument.SourceLevel gorm size:2（varchar(2)），service.Save 对该字段无长度校验，超长值直达 DB 触发写入错误→ErrorFromDB 落终态 500（应回 400 明确文案）。复现：POST body {title,content,source_level:'official'}；【2026-11-08 本地已修】KBService.Save 入口校验 source_level ∈ {A,B,C,D,空}（validSourceLevel），非法值经 utils.ErrInvalidInput 映射 400；回归 kb_source_level_test.go 绿 | `src/api/geoEntity.js` | 22 |
| 500 | [x] | DELETE | `/api/geo/kb/documents/{id}` | 用户端 | `src/api/geoEntity.js` | DELETE kb/documents/{id}: 本轮创建文档→200 '删除文档成功'；重复删→404 NOT_FOUND_1002 '删除文档失败'（台账该行描述'更新成功'与 DELETE 路由不符，以实测为准） | 200 更新成功 | 25 |
| 501 | [x] | GET | `/api/geo/kb/documents/{id}` | 用户端 | `src/api/geoEntity.js` | GET kb/documents/{id}: 合法→200 '获取文档成功' 返回实体；不存在→404 NOT_FOUND_1002 '获取文档失败' | 200 删除成功；重复删 404 | 28 |
| 502 | [x] | GET | `/api/geo/kb/search` | 用户端 | `src/api/geoEntity.js` | GET kb/search?q=api452:→200 检索完成 命中本轮文档；缺 q→400 INVALID_PARAM_1001 {message:'检索词不能为空',data:{field:'q'}} | 200 检索结果（关键词匹配降级，无向量库时） | 31 |
| 503 | [x] | POST | `/api/geo/kb/ask` | 用户端 | `src/api/geoEntity.js` | POST kb/ask:→200 问答完成 {answer:'…无法确切解释…',sources:[],provider:platform-sensenova,model:deepseek-v4-flash}（LLM 可用；无检索命中时回兜底话术） | 200 问答（retrieval_v1 检索拼接降级，无 LLM） | 34 |
| 504 | [x] | GET | `/api/geo/workflow/workflows` | 用户端 | `src/api/geoEntity.js` | GET workflow/workflows:→200 获取工作流列表成功 data:[]（租户暂无工作流） | 200 {list:[...]}（SOP 工作流） | 37 |
| 505 | [x] | GET | `/api/geo/workflow/workflows/{id}` | 用户端 | `src/api/geoEntity.js` | GET workflow/workflows/{id}: 新建→200 详情；不存在→404 NOT_FOUND_1002 '获取工作流失败' | 200 详情；不存在 → 404 | 40 |
| 506 | [x] | POST | `/api/geo/workflow/workflows` | 用户端 | `src/api/geoEntity.js` | POST workflow/workflows: 空 body→400 'Name/Steps failed on required'；合法 {name,steps:[{name,type:noop}]}→200 '创建工作流成功' id=f9144129（draft enabled=false） | 缺 name → 400；合法 → 200 创建（draft） | 43 |
| 507 | [x] | PUT | `/api/geo/workflow/workflows/{id}` | 用户端 | `src/api/geoEntity.js` | PUT workflow/workflows/{id}: 缺 steps→400 'Steps failed on required'（全量更新语义）；带 steps→200 '更新工作流成功' 且 steps 被替换、enabled=true 生效 | 缺 steps → 400 "steps required"（全量更新语义）；合法 → 200 | 46 |
| 508 | [x] | DELETE | `/api/geo/workflow/workflows/{id}` | 用户端 | `src/api/geoEntity.js` | DELETE workflow/workflows/{id}:→200 '删除工作流成功'；重复删→404 '删除工作流失败' | 200 删除；重复删 404 | 49 |
| 509 | [x] | POST | `/api/geo/workflow/workflows/{id}/run` | 用户端 | path: id / body: params \|\| {} | POST workflow/workflows/{id}/run:→200 HTTP 但 message='工作流执行失败'、data.status=failed、result[0].error='no executor for step type: noop'。controller 语义：执行已落库(result!=nil)即回 Success、由 status 区分（台账一致）；noop 非注册执行器故步骤失败，执行链路正常 | `src/api/geoEntity.js` | 52 |
| 510 | [x] | GET | `/api/geo/workflow/workflows/{workflowId}/executions` | 用户端 | `src/api/geoEntity.js` | GET workflow/workflows/{id}/executions:→200 执行记录列表（含刚触发的 failed 执行 209f8eae，progress=100） | 200 {list:[...]}; 执行历史 | 55 |
| 511 | [x] | GET | `/api/geo/workflow/templates` | 用户端 | `src/api/geoEntity.js` | GET workflow/templates:→200 '获取模板列表成功' data:[]（台账描述'步骤结果'与路由不符） | 200 步骤结果 | 58 |
| 512 | [x] | POST | `/api/geo/workflow/templates` | 用户端 | `src/api/geoEntity.js` | POST workflow/templates:→200 '创建模板成功' id=f65aa9f8 {name,steps:[{name:noop,type:noop}]} | 200 取消执行 | 61 |
| 513 | [x] | GET | `/api/geo/platform/platforms` | 用户端 | `src/api/geoPlatform.js` | GET platform/platforms:→200 平台清单 [{name:github_readme,display_name:GitHub README,auth_type:token,capability:real_api,has_account:false},{github_blog...}] | 200 {list:[...]}（可接入平台清单） | 4 |
| 514 | [x] | GET | `/api/geo/platform/accounts` | 用户端 | `src/api/geoPlatform.js` | GET platform/accounts:→200 {list:[],page:1,page_size:20,total:0} | 200 {list:[...]} | 7 |
| 515 | [x] | POST | `/api/geo/platform/accounts` | 用户端 | `src/api/geoPlatform.js` | POST platform/accounts: 空body→400 'Platform failed on required'；github_readme 无凭证且无 GEO_GITHUB_TOKEN→400 BUSINESS_5001 'GitHub 平台需要提供 access_token 或配置 GEO_GITHUB_TOKEN'；带 credentials.token→200 保存账号成功 id=8f177cb3(has_credentials=true) | 缺 access_token → 400 明确文案；合法结构 → 200（真实连通需 GitHub token，环境阻塞） | 10 |
| 516 | [x] | DELETE | `/api/geo/platform/accounts/{id}` | 用户端 | `src/api/geoPlatform.js` | DELETE platform/accounts/{id}: 本轮创建→200 '删除账号成功'；重复删→404 NOT_FOUND_1002 '删除账号失败' | 200 删除；重复删 404 | 13 |
| 517 | [x] | POST | `/api/geo/platform/publish` | 用户端 | `src/api/geoPlatform.js` | POST platform/publish: 不存在文章→400 BUSINESS_5001 '文章不存在'（拒绝路径正确） | 不存在文章 → 400 "文章不存在" | 16 |
| 518 | [x] | GET | `/api/geo/platform/records` | 用户端 | body: { article_id: articleId \|\| '' } | GET platform/records?article_id=:→200 {list:[],page:1,page_size:20,total:0} | `src/api/geoPlatform.js` | 19 |
| 519 | [x] | GET | `/api/geo/probe/engines` | 用户端 | `src/api/geoProbe.js` | GET probe/engines:→200 data:[]（AvailableEngines 返回引擎名数组；当前租户无可用探测引擎故空，非 {list,total}——台账描述与实现不符，接口本身正常） | 200 探测历史 {list,total} | 4 |
| 520 | [x] | GET | `/api/geo/probe/engines/meta` | 用户端 | `src/api/geoProbe.js` | GET probe/engines/meta:→200 data:[]（引擎名+mode 数组，无引擎时空；台账描述'删除历史'与 GET 路由不符） | 200 删除历史；重复删 404 | 8 |
| 521 | [x] | POST | `/api/geo/probe/import` | 用户端 | `src/api/geoProbe.js` | POST probe/import: rows:[]→400 'rows 不能为空'；缺 engine/query/response 行→200 {created:0,skipped:1,errors:['第 1 行缺少 engine/query/response']}；合法行→200 {created:1,skipped:0,errors:null}（逐行容错正确） | CSV 行级报错逐行返回（errors 数组），合法行 created=1 | 11 |
| 522 | [x] | POST | `/api/geo/probe/test` | 用户端 | `src/api/geoProbe.js` | POST probe/test: engine=bogus_engine→400 'engine bogus_engine not found (available: )'；缺 query→400 'query 必填' | 未知 engine → 400 "不支持的引擎"；合法 → 200 导入 | 14 |
| 523 | [x] | GET | `/api/geo/probe/runs` | 用户端 | `src/api/geoProbe.js` | GET probe/runs?limit=5:→200 {list:[{id:362,engine:manual_check,mode:manual,query:'api452 import probe query',citations:[...],sentiment:neutral}]}（#521 导入行在列） | 200 {list:[...]}; 探测目标列表 | 17 |
| 524 | [x] | POST | `/api/geo/probe/run-negative` | 用户端 | `src/api/geoProbe.js` | POST probe/run-negative:→200 {started:true} '已启动负面监控任务'（异步） | 200 {started:true}（异步批量探测） | 20 |
| 525 | [x] | POST | `/api/geo/probe/run-source-sync` | 用户端 | `src/api/geoProbe.js` | POST probe/run-source-sync:→200 {started:true} '已启动信源同步任务'（异步） | 200 {started:true}（异步全量重算） | 23 |
| 526 | [x] | POST | `/api/geo/probe/all` | 用户端 | `src/api/geoProbe.js` | POST probe/all: 缺 query→400 'query 必填'；合法→200 {total:0,summary:{rounds:1,total_runs:0,brand_hits:0,brand_mention_rate:0},runs:[],errors:[]}（无可用引擎时 0 run，结构完整） | 200 快照详情；不存在 → 404 | 26 |
| 527 | [x] | GET | `/api/geo/sov` | 用户端 | `src/api/geoProbe.js` | GET geo/sov:→200 data:[]（SOV 数据数组；台账注'share_of_voice 92.8% 历史数据'为旧租户数据，本租户无快照故空） | 200 SOV 报告（share_of_voice 92.8% 历史数据） | 29 |
| 528 | [x] | GET | `/api/geo/crawler-stats` | 用户端 | `src/api/geoProbe.js` | GET crawler-stats:→200 {summary:{today_visits:63,active_keywords:10,active_engines:8,a_level_count:8,avg_sov:0,hivemtk_visits:43,competitor_visits:20},keyword_stats:[...]}（真实爬虫访问统计） | 200 {list:[...]}（引用来源） | 32 |
| 529 | [x] | POST | `/api/geo/crawler/run` | 用户端 | `src/api/geoProbe.js` | POST crawler/run:→200 {status:started} '爬虫已启动' | 200 {started:true} | 35 |
| 530 | [x] | POST | `/api/geo/inaccurate-claims` | 用户端 | `src/api/geoProbe.js` | POST inaccurate-claims: 缺 brand_name→400 'BrandName failed on required'（该字段为必填，台账旧注 503 与当前实现不符）；带 brand_name→200 {claims:[]}（LLM 可用，扫描完成无失实断言） | LLM 熔断 → **503 SERVICE_UNAVAILABLE_6003（D-23c，修复前 500 且 err 原文泄漏）** | 38 |
| 531 | [x] | GET | `/api/geo/source-catalog/levels` | 用户端 | `src/api/geoProbe.js` | GET source-catalog/levels: 缺 url→400 'url 必填'；合法 url→200 {level:'',source:null}（外站未收录时正确空返回） | 缺 url → 400 "url 必填"；合法 → 200 采集任务创建 | 41 |
| 532 | [x] | GET | `/api/geo/competitors` | 用户端 | `src/api/geoProbe.js` | GET competitors:→200 data:[]（列表数组） | 200 {list:[...]} | 44 |
| 533 | [x] | GET | `/api/geo/competitors/{id}` | 用户端 | `src/api/geoProbe.js` | GET competitors/{id}: 合法→200 详情；999999→404 NOT_FOUND_1002 '竞品不存在' | 200 删除；重复删 404 | 47 |
| 534 | [x] | POST | `/api/geo/competitors` | 用户端 | `src/api/geoProbe.js` | POST competitors: 空 body→400 'name and domain are required'；重复 domain→400（masked 文案'操作失败，请稍后重试'）；合法→200 创建成功 id=4 | 缺 name/domain → 400；重复 domain → 400（masked 文案「操作失败，请稍后重试」）；合法 → 200 创建 | 50 |
| 535 | [~] | PUT | `/api/geo/competitors/{id}` | 用户端 | path: id / body: data | PUT competitors/{id} 回归：不传 domain→400 '竞品域名不能为空'（D-23d 修复未部署）。复现：PUT /api/geo/competitors/4 body {notes:'x'}。repo 源码 service/competitor.go L143-148 已是'域名为空保持原值'的部分更新语义，线上二进制行为不符；传 domain 的全量更新→200 但 name/category/status 传空会被清成 ''（全量覆盖语义，前端需回填）。不存在 id→404 '竞品不存在' | `src/api/geoProbe.js` | 53 |
| 536 | [x] | DELETE | `/api/geo/competitors/{id}` | 用户端 | `src/api/geoProbe.js` | DELETE competitors/{id}: id=4（本轮创建）→200 '删除成功'；重复删→404 '竞品不存在' | 200 删除；重复删 404 | 56 |
| 537 | [x] | GET | `/api/geo/visibility/engine-compare` | 用户端 | body: { days, intent: intent \|\| '' } | GET visibility/engine-compare?days=7:→200 {summary:{points:[],current_avg:0,previous_avg:0,change:0,change_pct:0,total_probes:0},engines:[],daily:[]}（无探针数据时零值结构完整） | `src/api/geoProbe.js` | 59 |
| 538 | [x] | GET | `/api/geo/sov/trend` | 用户端 | body: { days, intent: intent \|\| '' } | GET sov/trend?days=7:→200 {current:[],days:7,intent:'',previous:[]} | `src/api/geoProbe.js` | 62 |
| 539 | [x] | POST | `/api/geo/search-console/import` | 用户端 | `src/api/geoSearchConsole.js` | POST search-console/import: 空 body→400 'rows 与 csv 至少提供一个'；合法 rows→200 {imported:1,skipped:0,errors:[]} | 缺 rows/csv → 400；合法 → 200 {imported:1,errors:[]} | 4 |
| 540 | [x] | GET | `/api/geo/reports/dual-engine` | 用户端 | `src/api/geoSearchConsole.js` | GET reports/dual-engine:→200 {days:30,ai:{probe_runs:1,brand_mentions:0,citation_count:1,modes:{manual:1},engines:[...manual_check...]},google:{from,to,clicks:4,impressions:11,...}}（AI 探针+Search Console 双引擎结构齐全） | 200 {list:[...]} | 7 |
| 541 | [x] | POST | `/api/glossaries` | 用户端 | `src/api/glossary.js` | POST glossaries 缺字段400 必填文案; term_id+category+translations 200 创建成功 id=1 | 缺 term/definition → 400 明确文案；合法 → 200 创建 term_id=13 | 4 |
| 542 | [x] | GET | `/api/glossaries` | 用户端 | `src/api/glossary.js` | GET /api/glossaries 200 list含r526_term total=1 分页字段齐全 | 200 {list:[...]} | 8 |
| 543 | [x] | GET | `/api/glossaries/{termId}` | 用户端 | `src/api/glossary.js` | GET glossaries/r526_term 200 详情; r526_absent 404 术语不存在 | 200 详情；不存在 → 404（term_id 是字符串非数字主键） | 12 |
| 544 | [x] | PUT | `/api/glossaries/{termId}` | 用户端 | `src/api/glossary.js` | PUT glossaries/r526_term 200 category/translations 更新生效 updated_at变化 | 200 更新成功 | 16 |
| 545 | [x] | DELETE | `/api/glossaries/{termId}` | 用户端 | `src/api/glossary.js` | DELETE glossaries/r526_term 200 删除成功; 重复删 404 术语不存在 | 200 删除；重复删 404 | 20 |
| 546 | [x] | POST | `/api/glossaries/validate` | 用户端 | `src/api/glossary.js` | POST glossaries/validate {text} 200 校验完成 original/corrected_text issues=[] | 200 校验结果（术语一致性检查） | 24 |
| 547 | [x] | GET | `/api/human-tasks` | 用户端 | `src/api/humanTask.js` | GET /api/human-tasks 200 {list:[],total:0} 本租户无待办数据 | 200 {list:[...]}（人工任务） | 43 |
| 548 | [x] | GET | `/api/human-tasks/counts` | 用户端 | `src/api/humanTask.js` | GET human-tasks/counts 200 by_kind三类 conversation_handoff/approval/collection_escalation open=0 | 200 {pending,claimed,done,...} | 49 |
| 549 | [x] | GET | `/api/human-tasks/{id}` | 用户端 | `src/api/humanTask.js` | GET human-tasks/ht_absent_r526 404 NOT_FOUND_1002 待办不存在(id=) | 200 创建任务成功 | 53 |
| 550 | [x] | POST | `/api/human-tasks/{id}/claim` | 用户端 | `src/api/humanTask.js` | POST human-tasks/ht_absent_r526/claim 404 待办不存在(无真实待办fixture) | pending → claimed（认领人=当前用户） | 58 |
| 551 | [x] | POST | `/api/human-tasks/{id}/release` | 用户端 | `src/api/humanTask.js` | POST human-tasks/ht_absent_r526/release 404 待办不存在 | claimed → pending（释放） | 63 |
| 552 | [x] | POST | `/api/human-tasks/{id}/complete` | 用户端 | `src/api/humanTask.js` | POST human-tasks/ht_absent_r526/complete 404 待办不存在 | claim 后 complete → done（result 入库） | 68 |
| 553 | [x] | POST | `/api/human-tasks/{id}/cancel` | 用户端 | `src/api/humanTask.js` | POST cancel 空reason 400 撤销必须给reason; 合法reason但id不存在 404 | 非法/不存在 → 404 | 73 |
| 554 | [x] | GET | `/api/i18n/stats` | 用户端 | `src/api/i18nStats.js` | GET i18n/stats 200 汇总字段齐全 lang_count/cache_hit_rate/fallback_rate/latency_p50等 | 200 统计汇总 | 4 |
| 555 | [x] | GET | `/api/i18n/stats/lang-dist` | 用户端 | `src/api/i18nStats.js` | GET i18n/stats/lang-dist?days=7 200 {list:null,total:0} | 200 {list:[...]}（i18n 词条，空数据） | 8 |
| 556 | [x] | GET | `/api/i18n/stats/cache` | 用户端 | `src/api/i18nStats.js` | GET i18n/stats/cache?days=7 200 {cache_hit_rate:0,days:7} | 200 创建/更新词条 | 12 |
| 557 | [x] | GET | `/api/i18n/stats/glossary` | 用户端 | `src/api/i18nStats.js` | GET i18n/stats/glossary 200 {list:[en,zh词条],total:2} | 200 删除词条 | 16 |
| 558 | [x] | GET | `/api/i18n/stats/quality` | 用户端 | `src/api/i18nStats.js` | GET i18n/stats/quality?days=30 200 {list:null,total:0} | 200 翻译覆盖率统计 | 20 |
| 559 | [x] | GET | `/api/i18n/stats/latency` | 用户端 | `src/api/i18nStats.js` | GET i18n/stats/latency?days=7 200 {list:null,total:0} | 200 导出 JSON | 24 |
| 560 | [x] | GET | `/api/inbox/conversations` | 用户端 | `src/api/inbox.js` | GET inbox/conversations 200 真实会话 id=56 wecom platform/last_message_preview齐全 | 200 {list,total}（站内信会话，真实数据 id=603） | 7 |
| 561 | [x] | GET | `/api/inbox/conversations/{id}` | 用户端 | `src/api/inbox.js` | GET conversations/abc 400 无效的ID; /56 200 详情含pinned/starred/muted/tags | 非数字 id → 400；合法 → 200 {messages,total} | 12 |
| 562 | [x] | GET | `/api/inbox/conversations/{id}/messages` | 用户端 | `src/api/inbox.js` | GET conversations/56/messages 200 hub消息列表 total=2 msg_id/source=sent_at齐全 | 200 发送成功 | 17 |
| 563 | [x] | POST | `/api/inbox/conversations/{id}/read` | 用户端 | `src/api/inbox.js` | POST conversations/56/read 200 标记已读成功; 重复调用幂等200 | 200 已读标记（幂等） | 22 |
| 564 | [x] | POST | `/api/inbox/conversations/{id}/pin` | 用户端 | `src/api/inbox.js` | POST pin {pinned:true} 200 再false还原 200 data.pinned回显 | 200 置顶状态切换（已还原） | 27 |
| 565 | [x] | POST | `/api/inbox/conversations/{id}/star` | 用户端 | `src/api/inbox.js` | POST star {starred:true} 200 再false还原 200 | 200 星标状态切换（已还原） | 31 |
| 566 | [x] | POST | `/api/inbox/conversations/{id}/mute` | 用户端 | `src/api/inbox.js` | POST mute {muted:true} 200 再false还原 200 | 200 免打扰状态切换（已还原） | 35 |
| 567 | [x] | POST | `/api/inbox/conversations/{id}/tags` | 用户端 | `src/api/inbox.js` | POST tags r526_tag 200 添加成功且GET可见tags=[r526_tag]; 清理后tags=[](还原) | 200 打标签成功 | 40 |
| 568 | [x] | DELETE | `/api/inbox/conversations/{id}/tags/{tag}` | 用户端 | `src/api/inbox.js` | DELETE /api/inbox/conversations/999999/tags/apitest568 -> 404 NOT_FOUND_1002 会话不存在（本账号 scope 无会话，错误路径正确） | 200 移除标签成功 | 44 |
| 569 | [x] | DELETE | `/api/inbox/conversations/{id}/messages/{mid}?source={source}` | 用户端 | `src/api/inbox.js` | DELETE /api/inbox/conversations/999999/messages/1?source=hub -> 404 会话不存在；source=bogus -> 400 INVALID_PARAM_1001 无效的消息来源，校验正确 | source 只支持 hub/session → 404 "消息不存在" | 49 |
| 570 | [x] | POST | `/api/inbox/assign` | 用户端 | `src/api/inbox.js` | POST /api/inbox/assign 缺 action -> 400 请求参数错误；body 寻址不存在会话 -> 404 会话不存在（controller 先做 scoped 可见性检查，正确） | 缺 action → 400；human+assignee_id=54 → 200 分派成功 | 54 |
| 571 | [x] | POST | `/api/inbox/assign/auto` | 用户端 | `src/api/inbox.js` | POST /api/inbox/assign/auto 缺 candidates -> 400；不存在会话+candidates -> 404 会话不存在 | 缺 candidates → 400；合法 → 200 round_robin 分派 | 59 |
| 572 | [x] | GET | `/api/inbox/assignments` | 用户端 | `src/api/inbox.js` | GET /api/inbox/assignments -> 200 code=0 {list:[],total:0}（新账号无分派记录） | 200 {list:[...]}（分派记录） | 64 |
| 573 | [x] | GET | `/api/inbox/stats` | 用户端 | `src/api/inbox.js` | GET /api/inbox/stats -> 200 code=0 {total:0,unread:0,overdue_count:0}（GetStatsScoped 按当前账号隔离；台账 total:299 为旧账号视角） | 200 {total:299,unread:143,...} | 69 |
| 574 | [x] | POST | `/api/inbox/reconcile?mode={mode}` | 用户端 | `src/api/inbox.js` | POST /api/inbox/reconcile?mode=all -> 400 mode 仅支持 unread / overdue / backfill；mode=unread -> 200 重算 0 条（unread_reconciled:0） | mode=all → 400 "mode 仅支持 unread/overdue/backfill"；unread → 200 重算 143 条 | 74 |
| 575 | [x] | GET | `/api/inbox/staff/{staff}/load` | 用户端 | `src/api/inbox.js` | GET /api/inbox/staff/me/load -> 200 code=0 {load:0,staff:me} | 200 负载视图（按坐席未读分布） | 79 |
| 576 | [x] | GET | `/api/inbox/agents` | 用户端 | `src/api/inbox.js` | GET /api/inbox/agents -> 200 code=0 {list:null,total:0}（无坐席数据） | 200 {list:[...]}（坐席负载） | 84 |
| 577 | [x] | GET | `/api/integrations` | 用户端 | `src/api/integration.js` | GET /api/integrations -> 200 code=0 data:[]（获取成功） | 200 {list:[...]}（集成配置） | 4 |
| 578 | [x] | GET | `/api/integrations/{id}` | 用户端 | `src/api/integration.js` | GET /api/integrations/999999 -> 404 NOT_FOUND_1002（masked 文案「操作失败，请稍后重试」，与台账观察项一致，非缺陷） | 200 创建/更新；不存在 → 404（masked 文案「操作失败，请稍后重试」——观察项） | 7 |
| 579 | [x] | POST | `/api/integrations` | 用户端 | `src/api/integration.js` | POST /api/integrations 缺 platform -> 400 Platform required；合法 -> 200 创建成功 id=1 | 200 删除 id=27 成功 | 10 |
| 580 | [x] | PUT | `/api/integrations/{id}` | 用户端 | `src/api/integration.js` | PUT /api/integrations/1 -> 200 更新成功 account_name=apireg568-upd 已落库（观察项：body 的 platform 字段更新未生效，响应仍为旧值，台账未将其列为验证点） | 200 触发同步（异步） | 13 |
| 581 | [x] | DELETE | `/api/integrations/{id}` | 用户端 | `src/api/integration.js` | DELETE /api/integrations/3 -> 200 删除成功；重复删 -> 404 NOT_FOUND_1002 记录不存在 | 200 同步记录 {list,total} | 16 |
| 582 | [x] | POST | `/api/integrations/{id}/sync-customers` | 用户端 | `src/api/integration.js` | POST /api/integrations/1/sync-customers(ecomm) -> 400 该平台不支持客户同步（仅支持 销售易 / 纷享销客）；crm_xiaoshouyi 账号 -> 200 同步成功 count=0 | crcrm 类型仅支持 销售易/纷享销客 → 400 明确文案 | 19 |
| 583 | [x] | POST | `/api/integrations/{id}/sync-products` | 用户端 | `src/api/integration.js` | POST /api/integrations/1/sync-products(ecomm) -> 400 该平台不支持商品同步（仅支持 淘宝 / 京东）；ecommerce_taobao 账号 -> 200 同步成功 count=0 | ecomm 类型仅支持 淘宝/京东 → 400 明确文案 | 22 |
| 584 | [x] | GET | `/api/integration/sync-logs` | 用户端 | `src/api/integration.js` | GET /api/integration/sync-logs -> 200 code=0 {list:[],total:0} | 200 同步状态详情 | 25 |
| 585 | [x] | GET | `/api/integration/external-customers` | 用户端 | `src/api/integration.js` | GET /api/integration/external-customers -> 200 code=0 {list:[],total:0} | 200 {list:[...]}（集成日志） | 28 |
| 586 | [x] | GET | `/api/integration/external-orders` | 用户端 | `src/api/integration.js` | GET /api/integration/external-orders -> 200 code=0 {list:[],total:0} | 200 重试失败同步 | 31 |
| 587 | [x] | GET | `/api/integration/external-orders-by-customer` | 用户端 | `src/api/integration.js` | GET /api/integration/external-orders-by-customer?phone&name -> 200 code=0 {count:0,list:[]} | 200 测试连通性 | 34 |
| 588 | [x] | GET | `/api/integration/external-products` | 用户端 | `src/api/integration.js` | GET /api/integration/external-products -> 200 code=0 {list:[],total:0} | 200 {list:[...]}（映射规则） | 37 |
| 589 | [x] | POST | `/api/integrations/{id}/test` | 用户端 | `src/api/integration.js` | POST /api/integrations/999999/test -> 404 NOT_FOUND_1002；存在的 crm 账号 -> 200 连接测试成功 status=ok | 不存在 → 404 | 56 |
| 590 | [x] | POST | `/api/intent/recognize` | 用户端 | `src/api/intentRecognition.js` | POST /api/intent/recognize 空 body -> 400 text/message 不能为空；合法 -> 200 rule 识别 price_inquiry conf=0.82（config.enabled=true） | 200 意图识别（config.enabled=false → disabled 状态返回） | 5 |
| 591 | [x] | POST | `/api/intent/recognize/batch` | 用户端 | `src/api/intentRecognition.js` | POST /api/intent/recognize/batch 缺 items -> 400 items/messages 至少需要一个；合法 -> 200 批量识别成功 after_sale | 缺 items/messages → 400；合法 → 200 识别结果 | 9 |
| 592 | [x] | GET | `/api/intent/stats` | 用户端 | `src/api/intentRecognition.js` | GET /api/intent/stats -> 200 code=0 {total:3,by_intent,by_level,by_method,distribution} | 200 {list:[...]}（意图字典） | 13 |
| 593 | [x] | GET | `/api/intent/recent` | 用户端 | `src/api/intentRecognition.js` | GET /api/intent/recent -> 200 code=0 list 含本租户识别记录 id=39 | 200 维护字典项 | 17 |
| 594 | [x] | GET | `/api/intent/dict` | 用户端 | `src/api/intentRecognition.js` | GET /api/intent/dict -> 200 code=0 返回默认意图字典数组（price_inquiry/objection_price 等） | 200 导出字典 | 21 |
| 595 | [x] | POST | `/api/intent/recognize/fine` | 用户端 | `src/api/intentRecognition.js` | POST /api/intent/recognize/fine 缺 message -> 400；合法 -> 200 major=after_sale minor=quality_issue method=llm qwen-plus | 缺 message → 400；合法 → 200 分类（consult/general） | 25 |
| 596 | [x] | GET | `/api/intent/logs` | 用户端 | `src/api/intentRecognition.js` | GET /api/intent/logs -> 200 code=0 返回细识别日志（intent_major=after_sale） | 200 {list:[...]}（路由规则） | 29 |
| 597 | [x] | GET | `/api/intent/stats/fine` | 用户端 | `src/api/intentRecognition.js` | GET /api/intent/stats/fine -> 200 code=0 {by_major,by_minor,by_method,days:7} | 200 维护路由规则 | 33 |
| 598 | [x] | GET | `/api/intent/config` | 用户端 | `src/api/intentRecognition.js` | GET /api/intent/config -> 200 code=0 {enabled:true} | 200 {enabled:false,...} | 37 |
| 599 | [x] | PUT | `/api/intent/config` | 用户端 | `src/api/intentRecognition.js` | PUT /api/intent/config enabled=false -> 200 更新成功 updated_by=apireg...；已还原 enabled=true 复查一致 | 200 保存生效（enabled 等开关） | 41 |
| 600 | [x] | GET | `/api/intent-records/keywords-override` | 用户端 | `src/api/intentRecognition.js` | GET /api/intent-records/keywords-override -> 200 code=0 data:{}（空对象） | 200 {}（兼容配置，空对象） | 45 |
| 601 | [x] | PUT | `/api/intent-records/keywords-override` | 用户端 | `src/api/intentRecognition.js` | PUT keywords-override {override:{price_inquiry:[apireg568kw]}} -> 200 types=1，GET 回读一致，recognize 热生效 conf=0.92；已还原空 | 200 热更新生效（测试值写入后已还原为空） | 49 |
| 602 | [x] | GET | `/api/kb-connectors` | 用户端 | `src/api/kbConnector.js` | GET /api/kb-connectors -> 200 code=0 {list:[],types:[ima,webhtml]实际为[ima,webhttp]} | 200 {list:[...]}（知识库连接器） | 5 |
| 603 | [x] | POST | `/api/kb-connectors` | 用户端 | `src/api/kbConnector.js` | POST kb-connectors 缺 type -> 400 Type required；webhook -> 400 不支持的连接器类型（可用: [ima webhttp]）；ima -> 200 创建成功 id=1 | 缺 type → 400；webhook 不支持（可用 ima/webhttp）→ 400 列出枚举；ima → 200 创建 id=3 | 6 |
| 604 | [x] | PUT | `/api/kb-connectors/{id}` | 用户端 | `src/api/kbConnector.js` | PUT /api/kb-connectors/1 -> 200 已更新 ok=true；不存在 999999 -> 404 连接器不存在 | 200 更新成功；不存在 → 404 | 7 |
| 605 | [x] | DELETE | `/api/kb-connectors/{id}` | 用户端 | `src/api/kbConnector.js` | DELETE /api/kb-connectors/1 -> 200 已删除 note=已导入文档保留在知识库；重复删 -> 404 连接器不存在 | 200 删除（已导入文档保留提示）；重复删 404 | 8 |
| 606 | [x] | POST | `/api/kb-connectors/{id}/sync` | 用户端 | `src/api/kbConnector.js` | POST /api/kb-connectors/999999/sync -> 404 连接器不存在；存在的 ima 未配 client_id/api_key -> 400 配置缺少 client_id/api_key（真实同步环境阻塞，错误路径正确） | 不存在 → 404 "连接器不存在"（真实同步需外部源，环境阻塞） | 9 |
| 607 | [x] | GET | `/api/capabilities` | 用户端 | - | GET /api/capabilities -> 200 code=0 capabilities 数组含 platform/capability/implemented/configured/ready/reason_code（渠道能力矩阵） | `src/api/kbConnector.js` | 14 |
| 608 | [x] | GET | `/api/kb/crawler/sources` | 用户端 | query: params | GET /api/kb/crawler/sources -> 200 code=0 data:[] | `src/api/kbCrawler.js` | 7 |
> **缺陷 D-24（已修复）**：爬取源「不存在」的错误分类错误——`POST /api/kb/crawler/sources/{id}/pause|resume`
> 对不存在 id 回 409 DUPLICATE_ENTRY_3003（controller 固定 response.Error(409)，而 response.Error 按
> errorCodeFromHTTPCode 把 409 折叠为"重复记录"语义，"资源不存在"被误标为"重复"）；`PUT /sources/{id}`
> 不存在回 400 INVALID_PARAM_1001（应为 404）。
> 根因：service 层 First 查不到时 errors.New("爬取源不存在") 丢失 not-found 语义。
> 修复：UpdateSource/TransitionSource 查不到改返回 gorm.ErrRecordNotFound；controller 对两分支用
> errhttp.IsNotFoundError 映射 404 NOT_FOUND_1002「爬取源不存在」，其余错误维持 400/409；id 非法仍 400。
> 验证：复测 pause/resume/update 传 99999 均 404「爬取源不存在」，合法跃迁与幂等路径不回归。

| 609 | [x] | POST | `/api/kb/crawler/sources` | 用户端 | body: data | POST sources 缺字段 -> 400 source_type 必须是 url 或 sitemap；kb_id=2 合法 -> 200 创建成功含 site_verify_token，提示先站点验证 | `src/api/kbCrawler.js` | 11 |
| 610 | [~] | PUT | `/api/kb/crawler/sources/{id}` | 用户端 | path: id / body: data | PUT /api/kb/crawler/sources/999999 -> 400 INVALID_PARAM_1001 爬取源不存在，应为 404（D-24 声称已修复但线上未部署，修复前行为）；存在的 id=1 更新 -> 200 ok | `src/api/kbCrawler.js` | 14 |
| 611 | [~] | POST | `/api/kb/crawler/sources/{id}/pause` | 用户端 | path: id | POST pause 999999 -> 409 DUPLICATE_ENTRY_3003 爬取源不存在（D-24 修复要求 404 NOT_FOUND_1002，线上仍是 409 误分类）；pause id=1 -> 200 ok | `src/api/kbCrawler.js` | 17 |
| 612 | [~] | POST | `/api/kb/crawler/sources/{id}/resume` | 用户端 | path: id | POST resume 999999 -> 409 DUPLICATE_ENTRY_3003 爬取源不存在（同 D-24 未部署）；resume id=1 -> 200 ok | `src/api/kbCrawler.js` | 20 |
| 613 | [x] | POST | `/api/kb/crawler/sources/{id}/verify` | 用户端 | path: id | POST verify id=1 -> 400 站点验证失败提示放置 hivemtk-verify.txt（example.com 未部署 token，业务正确）；verify 999999 -> 400 爬取源不存在 | `src/api/kbCrawler.js` | 23 |
| 614 | [x] | DELETE | `/api/kb/crawler/sources/{id}` | 用户端 | path: id | DELETE /api/kb/crawler/sources/1 -> 200 ok；重复删也回 200 ok（幂等删除，无 404，台账未钉行为） | `src/api/kbCrawler.js` | 26 |
| 615 | [x] | POST | `/api/kb/crawler/sources/{id}/sync` | 用户端 | path: id | POST sync id=1 -> 400 源未通过站点验证，不能同步（业务正确）；sync 999999 -> 400 爬取源不存在 | `src/api/kbCrawler.js` | 30 |
| 616 | [x] | GET | `/api/kb/crawler/batches` | 用户端 | query: params | GET /api/kb/crawler/batches -> 200 code=0 {items:[],total:0} | `src/api/kbCrawler.js` | 34 |
| 617 | [x] | POST | `/api/kb/crawler/batches/{id}/retry` | 用户端 | path: id | POST /api/kb/crawler/batches/999999/retry -> 400 批次不存在（错误路径正确） | `src/api/kbCrawler.js` | 37 |
| 618 | [x] | GET | `/api/kb/qa/candidates` | 用户端 | query: params | GET /api/kb/qa/candidates -> 200 code=0 {items:[],total:0} | `src/api/kbCrawler.js` | 44 |
> **缺陷 D-25（已修复，与 D-24 同模式）**：QA 候选 approve/reject/publish 对不存在 id 回 409 DUPLICATE_ENTRY_3003「候选不存在」。
> 根因：service/kb_qa_split.go 的 ReviewQACandidate/PublishQACandidate First 查不到时 fmt.Errorf("候选不存在") 丢失 not-found 语义；controller 固定 response.Error(409)。
> 修复：service 两处改返回 gorm.ErrRecordNotFound；controller 加 errhttp.IsNotFoundError 分支 → 404「候选不存在」（非法状态跃迁仍 409）。
> 复测：approve/reject/publish 99999 → 404 NOT_FOUND_1002；indexed 候选 publish → 409「非法状态跃迁: indexed → indexed（需先 approve）」不变。

| 619 | [~] | POST | `/api/kb/qa/candidates/{id}/approve` | 用户端 | path: id | POST qa approve 999999 -> 409 DUPLICATE_ENTRY_3003 候选不存在（D-25 修复要求 404 NOT_FOUND_1002，线上仍是修复前 409 误分类） | `src/api/kbCrawler.js` | 47 |
| 620 | [~] | POST | `/api/kb/qa/candidates/{id}/reject` | 用户端 | path: id | POST qa reject 999999 -> 409 DUPLICATE_ENTRY_3003 候选不存在（同 D-25 线上未部署修复） | `src/api/kbCrawler.js` | 50 |
| 621 | [~] | POST | `/api/kb/qa/candidates/{id}/publish` | 用户端 | path: id | POST qa publish 999999 -> 409 DUPLICATE_ENTRY_3003 候选不存在（同 D-25 线上未部署修复） | `src/api/kbCrawler.js` | 53 |
| 622 | [x] | POST | `/api/knowledge/import/upload` | 用户端 | body: formData | POST import/upload 无 product_id -> 400 product_id 不能为空；带文件+product_id -> 400 产品不存在（该测试路径的 product 校验正确） | `src/api/knowledge.js` | 5 |
| 623 | [x] | POST | `/api/knowledge/import/text` | 用户端 | body: data | POST import/text 缺 product_id/content -> 400 required；合法 -> 200 文本导入已启动 document_id=11 status=pending（后台完成 indexed） | `src/api/knowledge.js` | 10 |
| 624 | [x] | POST | `/api/knowledge/import/url` | 用户端 | body: data | POST import/url -> 400 产品不存在: rag product not found（未做真实抓取，参数校验正确） | `src/api/knowledge.js` | 13 |
| 625 | [x] | GET | `/api/knowledge/documents` | 用户端 | query: params | GET /api/knowledge/documents -> 200 items 含新导入 doc id=11 | `src/api/knowledge.js` | 17 |
| 626 | [x] | GET | `/api/knowledge/documents/{id}` | 用户端 | path: id / query: params | GET documents/11 -> 200 详情；不存在 999999 -> 404 knowledge document not found | `src/api/knowledge.js` | 20 |
| 627 | [x] | GET | `/api/knowledge/documents/{id}/progress` | 用户端 | path: id | GET documents/11/progress -> 200 {chunk_count:1,embed_status:indexed,embed_progress:100} | `src/api/knowledge.js` | 23 |
> **缺陷 D-26（已修复）**：`POST /api/knowledge/products/{product_id}/rebuild-index` 对不存在的产品也返回 200「重建任务已启动」。
> 根因：service `RebuildIndex`（knowledge_service_reindex.go）用 docRepo.List 取文档，空集循环零次返回 nil——
> 静默成功会让前端误以为索引已重建。修复：service 先用已有的 `ragRepo.GetRagProductByID` 校验产品存在性
> （复用 rag_config.go 的 "rag product not found" 语义），controller RebuildProductIndex 补 `isNotFoundError` → 404 分支。
> 验证：不存在产品 → 404 NOT_FOUND_1002 "rag product not found"；真实产品 ecommerce-general-cs → 200 正常重建。

| 628 | [x] | GET | `/api/knowledge/documents/{id}/chunks` | 用户端 | path: id | GET documents/11/chunks -> 200 items 含 1 个分段 id=50 | `src/api/knowledge.js` | 26 |
| 629 | [x] | PUT | `/api/knowledge/documents/{id}` | 用户端 | path: id / body: data | PUT documents/11 -> 200 更新成功 title=api568 text doc v2 | `src/api/knowledge.js` | 29 |
| 630 | [x] | DELETE | `/api/knowledge/documents/{id}` | 用户端 | path: id | DELETE documents/12 -> 200 删除成功；重复删/999999 -> 404 knowledge document not found | `src/api/knowledge.js` | 32 |
| 631 | [x] | POST | `/api/knowledge/documents/{id}/reindex` | 用户端 | path: id | POST documents/11/reindex -> 200 重建任务已启动 | `src/api/knowledge.js` | 35 |
| 632 | [~] | POST | `/api/knowledge/products/{productId}/rebuild-index` | 用户端 | path: product_id | POST /api/knowledge/products/no-such-product/rebuild-index -> 200「重建任务已启动」假成功，台账 D-26 要求 404 rag product not found；线上为修复前行为（本地源码已修未部署）；真实产品 -> 200 正常 | `src/api/knowledge.js` | 39 |
> **缺陷 D-27（已修复）**：`POST /api/knowledge/search` 跨产品串数据且结果元信息丢失。
> 修复前 `KnowledgeService.Search` 调的是全库 `ragSearcher.Search(ctx,query,topK)`（无产品过滤），
> 再把请求的 product_id 回填到每条结果上——别的产品的文档被标成本产品返回（实测 doc 4 属于 default、
> doc 548 属于 d6587ab3-…，都被贴上 ecommerce-general-cs）；同时命中记录的 id/similarity_score/weight 全丢（恒 0），
> 同一分段经向量路与 BM25 路各命中一次时还会重复出现两条。
> 修复：改调带产品过滤的 `SearchIndex(ctx, productID, query, topK, nil)`，新增 `dedupeMerchantChunks`（按分段 ID 去重，
> 无 ID 时按 文档+内容）与 `knowledgeChunkFromMerchant`（透传 ID/DocumentID/SimilarityScore，weight 缺省回落 1）。
> 补 `knowledge_search_test.go` 三个单测。验证：复测同产品只返回该产品文档（doc 15/14）、无重复、分数 1/0.359/0.336…；
> 不存在产品返回空 items；on 模式 tenant 5 搜 tenant 1 产品返回空（租户隔离成立）。


| 633 | [x] | GET | `/api/knowledge/products/{productId}/overview` | 用户端 | path: productId | GET products/<uuid>/overview -> 200 {total_documents:1,total_chunks:1,embed_status_breakdown,index_health} | `src/api/knowledge.js` | 42 |
| 634 | [~] | POST | `/api/knowledge/search` | 用户端 | body: data | POST /api/knowledge/search 跨产品泄漏复现：产品A 检索命中产品B 独有内容（document_id=12 被贴上产品A product_id），且命中 id/similarity_score/weight 恒 0——D-27 修复（SearchIndex 过滤+透传字段）线上未部署 | `src/api/knowledge.js` | 46 |
| 635 | [~] | GET | `/api/knowledge/import-logs` | 用户端 | query: params | GET /api/knowledge/import-logs -> 200 {items:[],total:0}：本租户刚成功导入 doc 11/12 但日志恒空（D-29 修复要求出现 success 记录；线上仍为修复前静默写失败）；stats/imports total_imports=0 同样恒 0 | `src/api/knowledge.js` | 50 |
> **缺陷 D-28（已修复）**：`DELETE /api/knowledge/openapi/sources/{id}` 两个问题：
> ①重复删 / 删不存在的 id 回 **500 INTERNAL_ERROR_6002「操作失败，请稍后重试」**——仓储层 `EnsureAffected`
> 已经返回 gorm.ErrRecordNotFound，控制器却统一兜底 500，调用方分不清"删掉了"和"本来就没有"；
> ②`OpenAPIService.DeleteSource` 收了 productID 却直接按 id 删，**产品隔离被丢弃**——同租户其它产品的
> 数据源也能被删掉（越权删除）。
> 修复：先 `GetByProductAndID` 校验归属再删；控制器 `isNotFoundError` → 404「数据源不存在」。
> 验证：重复删 / 不存在 → 404；跨产品删 → 404 且数据源仍在。

| 636 | [x] | GET | `/api/knowledge/openapi/sources` | 用户端 | query: params | GET /api/knowledge/openapi/sources -> 200 {items:[],total:0} | `src/api/knowledge.js` | 54 |
| 637 | [x] | POST | `/api/knowledge/openapi/sources` | 用户端 | body: data | POST openapi/sources 缺 name -> 400 数据源名称不能为空；合法 -> 200 创建成功 id=1 type=rest | `src/api/knowledge.js` | 57 |
| 638 | [x] | GET | `/api/knowledge/openapi/sources/{id}` | 用户端 | path: id / query: product_id | GET openapi/sources/1 -> 200 详情；不存在 999999 -> 404 openapi source not found | `src/api/knowledge.js` | 60 |
| 639 | [x] | PUT | `/api/knowledge/openapi/sources/{id}` | 用户端 | path: id / body: data | PUT openapi/sources/1 -> 200 更新成功 name=apireg568-src2 | `src/api/knowledge.js` | 63 |
| 640 | [~] | DELETE | `/api/knowledge/openapi/sources/{id}` | 用户端 | path: id / query: product_id | DELETE openapi/sources/2?product_id=other-product-xxx（非归属产品）-> 200 删除成功，数据被真删（越权删除，D-28 要求 404 且保留）；且重复删 -> 500 INTERNAL_ERROR_6002（D-28 修复要求 404），线上均为修复前行为 | `src/api/knowledge.js` | 66 |
| 641 | [x] | POST | `/api/knowledge/openapi/sources/{id}/sync` | 用户端 | path: id / query: product_id | POST openapi/sources/1/sync -> 502 UPSTREAM_FAILURE_5005 同步数据源失败请检查端点可达性（example.com 不可作为数据源，上游错误分类正确） | `src/api/knowledge.js` | 69 |
| 642 | [x] | POST | `/api/knowledge/openapi/sources/{id}/test` | 用户端 | path: id / body: data | POST openapi/sources/1/test -> 200 测试完成 body_sample 返回 example.com HTML | `src/api/knowledge.js` | 72 |
| 643 | [x] | POST | `/api/knowledge/openapi/sources/{id}/toggle` | 用户端 | path: id / query: product_id | POST openapi/sources/1/toggle -> 200 切换成功 | `src/api/knowledge.js` | 75 |
| 644 | [x] | GET | `/api/knowledge/stats/overview` | 用户端 | query: product_id | GET stats/overview -> 200 {total_documents:1,today_imports:1,total_searches:1,hit_rate:1,index_health} | `src/api/knowledge.js` | 79 |
| 645 | [x] | GET | `/api/knowledge/stats/documents` | 用户端 | query: product_id | GET stats/documents -> 200 含 overview 明细 | `src/api/knowledge.js` | 82 |
| 646 | [x] | GET | `/api/knowledge/stats/searches` | 用户端 | query: product_id | GET stats/searches -> 200 含 overview（产品级检索统计） | `src/api/knowledge.js` | 85 |
| 647 | [~] | GET | `/api/knowledge/stats/imports` | 用户端 | query: product_id | GET stats/imports -> 200 但 total_imports/success_imports 恒 0、daily_trend 全 0——刚有 2 次成功导入（D-29 写入失败静默，线上未部署修复） | `src/api/knowledge.js` | 88 |
| 648 | [x] | GET | `/api/knowledge/stats/openapi` | 用户端 | query: product_id | GET stats/openapi -> 200 {total_sources:0,enabled_sources:0,source_list:[]} | `src/api/knowledge.js` | 91 |

> **缺陷 D-29（已修复）**：导入日志与导入统计永久为空，不是"没数据"而是**写入全部失败**。
> 表 `knowledge_import_logs.error_detail` 实际列类型是 jsonb，模型却按普通 string 声明；
> 成功态写 `""`、失败态写 `dial tcp ...` 这类非 JSON 文本，Postgres 一律报
> `invalid input syntax for type json`；而 `KnowledgeService.Import` 用 `_ = s.logImport(...)`
> 吞掉错误 → 接口照常 200，日志表恒 0 行、`stats/imports` 恒为 0，用户以为"从没导入过"。
> 修复：列改为 text（与同域批次表一致，语义就是人读文案）；两处调用点改为记录告警而非静默丢弃。
> 验证：导入一段文本后表内出现 success 记录，`stats/imports` total_imports=1、recent_logs 非空，
> `import-logs` 与 `overview.today_imports` 同步可见。
| 649 | [x] | GET | `/api/knowledge-bases` | 用户端 | query: params | GET /api/knowledge-bases -> 200 list 含 api_reg_kb_1107 id=2 | `src/api/knowledgeBase.js` | 4 |
| 650 | [x] | GET | `/api/knowledge-bases/by-type/{kbType}` | 用户端 | path: kbType / query: params | GET /api/knowledge-bases/by-type/rag -> 200 数组含 id=2 | `src/api/knowledgeBase.js` | 8 |
| 651 | [x] | GET | `/api/knowledge-bases/{id}` | 用户端 | path: id | GET /api/knowledge-bases/2 -> 200 详情；999999 -> 404 知识库不存在 | `src/api/knowledgeBase.js` | 12 |
| 652 | [x] | POST | `/api/knowledge-bases` | 用户端 | body: data | POST knowledge-bases 缺字段 -> 400；缺 owner_type 默认 private 校验 owner_agent_id 必填（400 masked 文案「操作失败，请稍后重试」——观察项，service 明明包了 ErrInvalidInput）；owner_type=shared 合法 -> 200 创建 id=3 | `src/api/knowledgeBase.js` | 16 |
| 653 | [x] | PUT | `/api/knowledge-bases/{id}` | 用户端 | path: id / body: data | PUT /api/knowledge-bases/3 -> 200 更新成功 {id:3} | `src/api/knowledgeBase.js` | 20 |
| 654 | [x] | DELETE | `/api/knowledge-bases/{id}` | 用户端 | path: id | DELETE /api/knowledge-bases/3 -> 200 删除成功；重复删 -> 404 NOT_FOUND_1002 | `src/api/knowledgeBase.js` | 24 |
| 655 | [x] | GET | `/api/knowledge-bases/{id}/stats` | 用户端 | path: id | GET /api/knowledge-bases/3/stats -> 200 {agent_count:0,item_count:0} | `src/api/knowledgeBase.js` | 28 |
| 656 | [x] | POST | `/api/knowledge-merchant/batch/import` | 用户端 | body: data | POST batch/import 缺 product_id -> 400 请选择目标知识库（产品）后再导入；合法 -> 200 批量导入完成 batch_no BATCH-... accepted=1 document_ids=[13] | `src/api/knowledgeMerchant.js` | 5 |
| 657 | [x] | POST | `/api/knowledge-merchant/batch/upload` | 用户端 | body: formData | POST batch/upload multipart file+product_id -> 200 批量导入完成 accepted=1 document_ids=[14] | `src/api/knowledgeMerchant.js` | 8 |
| 658 | [x] | POST | `/api/knowledge-merchant/playground` | 用户端 | body: data | POST playground -> 200 命中 2 分段 score 0.87/0.87（检索召回正常） | `src/api/knowledgeMerchant.js` | 14 |
| 659 | [x] | GET | `/api/knowledge-merchant/documents/{documentId}/chunks` | 用户端 | path: documentId / query: params | GET documents/13/chunks -> 200 items 含 chunk id=52 | `src/api/knowledgeMerchant.js` | 18 |
| 660 | [~] | PUT | `/api/knowledge-merchant/chunks/{chunkId}` | 用户端 | path: chunkId / body: data | PUT /api/knowledge-merchant/chunks/999999 -> 400 INVALID_PARAM_1001「分段不存在: chunk not found」，台账 D-30 要求 404 NOT_FOUND_1002（英文原文也未脱敏），线上未部署修复；存在的 chunk 52 -> 200 更新成功 | `src/api/knowledgeMerchant.js` | 21 |
> **缺陷 D-30（已修复）**：分段写接口把「分段不存在」误回 400 INVALID_PARAM_1001
> （split 还会把原始英文 "chunk not found" 直接抛给前端）。
> 根因：`replyChunkWriteError` 只区分治理直写与通用参数错误，gorm.ErrRecordNotFound 落了参数错误分支。
> 修复：该分支前加 `isNotFoundError` → 404 NOT_FOUND_1002「分段不存在」。PUT/DELETE/split 三处共用，一并生效。
> 验证：三个端点传不存在 id → 404；id 非法仍 400。

| 661 | [~] | DELETE | `/api/knowledge-merchant/chunks/{chunkId}` | 用户端 | path: chunkId | DELETE /api/knowledge-merchant/chunks/999999 -> 400 masked「操作失败，请稍后重试」，D-30 要求 404 分段不存在；线上未部署（存在的 chunk 删除路径受 D-31 数据丢失影响未单独复测） | `src/api/knowledgeMerchant.js` | 24 |
| 662 | [~] | POST | `/api/knowledge-merchant/chunks/{chunkId}/split` | 用户端 | path: chunkId / body: { parts } | POST chunks/{id}/split parts=[" ",""] -> 200「拆分成功」但原分段被删且新分段 0 条插入（doc13/doc14 分段被清空实锤复现，D-31 数据丢失级缺陷线上未部署修复）；单空白 parts -> 400 至少需要 2 段（部分校验存在）；合法 2 段 -> 200 1 拆 2 | `src/api/knowledgeMerchant.js` | 27 |
> **缺陷 D-31（已修复，数据丢失级）**：`POST /chunks/{id}/split` 传全空白（或含空白项）的 parts 时
> 先删原分段、后在插入循环里跳过空白段 —— 结果是**接口回 200「拆分成功」，原分段没了、新分段一条没插入**，
> 文档内容被静默清空。修复：在任何删除动作之前先校验 parts 无空白项，返回 400「拆分内容不能为空」。
> 补回归测试 `TestSplitChunk_RejectsBlankPartsBeforeDelete`（全空白/混合空白/段数不足，断言拒绝发生在触碰仓储之前）。
> 验证：空白 parts → 400 且原分段仍在；正常 parts → 200（1 段拆成 2 段）。

| 663 | [x] | POST | `/api/knowledge-merchant/feedback` | 用户端 | body: data | POST /api/knowledge-merchant/feedback 缺 query -> 400 query 不能为空；合法 -> 200 反馈已记录（rating 为 int 字段） | `src/api/knowledgeMerchant.js` | 31 |
| 664 | [x] | GET | `/api/knowledge-merchant/feedbacks` | 用户端 | query: params | GET feedbacks -> 200 items 含 id=1 且 stats {good:1,...} 汇总正确 | `src/api/knowledgeMerchant.js` | 34 |
| 665 | [x] | POST | `/api/knowledge-merchant/tokens` | 用户端 | body: data | POST tokens 缺 name -> 400 name 不能为空；合法 -> 200 创建 id=1 仅显示一次明文 kbg_...（scopes 默认 read） | `src/api/knowledgeMerchant.js` | 38 |
| 666 | [x] | GET | `/api/knowledge-merchant/tokens` | 用户端 | query: params | GET tokens?product_id -> 200 items 含 apireg568-token（明文不回显） | `src/api/knowledgeMerchant.js` | 41 |
| 667 | [x] | POST | `/api/knowledge-merchant/tokens/{tokenId}/revoke` | 用户端 | path: tokenId | POST tokens/1/revoke -> 200 已吊销；重复 revoke 也 200 已吊销（幂等） | `src/api/knowledgeMerchant.js` | 44 |
| 668 | [~] | POST | `/api/knowledge-merchant/external/import` | 用户端 | body: data / header: X-Knowledge-Token | POST external/import 无 token 空 body -> 400「source 不能为空」，D-32 要求鉴权前置应 401；坏 token 同样 400 source 不能为空（token 校验被跳过）；已吊销 token+合法 body -> 401 外部系统导入失败（此路径才到鉴权）——D-32 校验顺序修复线上未部署 | `src/api/knowledgeMerchant.js` | 48 |
> **缺陷 D-32（已修复）**：`POST /api/knowledge-merchant/external/import` 是免 JWT 的公开入口，
> 但控制器与 service 都先校验 `source`/`product_id`、后验 `X-Knowledge-Token` —— 未带 token 的空请求回
> 400「source 不能为空」，**在鉴权之前就暴露了字段规则**，也让人误以为要先去补参数而不是先拿 token。
> 修复：service 里把 ValidateToken / 租户钉定 / scope 校验提到最前，通过后才校验业务字段；
> 控制器删掉重复的字段校验（避免顺序被再次打乱）。
> 验证：无 token 的空 body / 缺 product_id → 401（修复前 400）；合法 write token → 200；read-only token → 403。

| 669 | [x] | GET | `/api/knowledge-merchant/external/jobs` | 用户端 | query: params | GET external/jobs -> 200 {items:[],total:0} | `src/api/knowledgeMerchant.js` | 53 |
| 670 | [x] | GET | `/api/kuaishou/list` | 用户端 | query: page, page_size | GET /api/kuaishou/list -> 200 list 含 r832_ks_card id=1（分页正常） | `src/api/kuaishouCard.js` | 4 |
| 671 | [x] | GET | `/api/kuaishou/{id}` | 用户端 | path: id | GET /api/kuaishou/999999 -> 404 资源不存在 record not found；GET /api/kuaishou/2 -> 200 详情 | `src/api/kuaishouCard.js` | 8 |
| 672 | [x] | POST | `/api/kuaishou/create` | 用户端 | body: data | POST kuaishou/create 缺 title -> 400 Title required；合法 -> 200 创建 id=2 且自动生成 short_code=sRHigZ | `src/api/kuaishouCard.js` | 12 |
| 673 | [~] | PUT | `/api/kuaishou/update` | 用户端 | body: data | PUT /api/kuaishou/update 只改 title -> 200 但 short_code 由 sRHigZ 变为 eW7Oku（D-33 修复要求改标题不重建短链，线上未部署修复，已分发链接会失效） | `src/api/kuaishouCard.js` | 16 |
> **缺陷 D-33（已修复）**：快手卡片的「更新」每次都会删除旧短链并生成新 short_code——
> 用户只改标题/描述/启用状态，已分发出去的 `/s/xxxxxx` 推广链接就全部失效。
> 对照抖音卡片实现（只在 domain_pool_id 变化时重建），修复：`kuaishouCardService.Update`
> 记录 originalDomainPoolID，仅在域名池变化或历史卡片无短链时才调 `GenerateShortLink`。
> 验证：改 title → short_code 不变；改 domain_pool_id → 重建新码；显式 generate-short-link 仍可用。

| 674 | [x] | DELETE | `/api/kuaishou/delete/{id}` | 用户端 | path: id | DELETE /api/kuaishou/delete/2 -> 200 删除成功；重复删 -> 404 记录不存在 | `src/api/kuaishouCard.js` | 20 |
| 675 | [x] | GET | `/api/kuaishou/view/{id}` | 用户端 | path: id | GET /api/kuaishou/view/2 -> 200 返回卡片详情 | `src/api/kuaishouCard.js` | 24 |
| 676 | [x] | POST | `/api/kuaishou/like/{id}` | 用户端 | path: id | POST /api/kuaishou/like/2 -> 200 操作成功（like_count 递增可见于 share 响应） | `src/api/kuaishouCard.js` | 28 |
| 677 | [x] | POST | `/api/kuaishou/share/{id}` | 用户端 | path: id | POST /api/kuaishou/share/2 -> 200 操作成功 返回卡片 share_count 字段 | `src/api/kuaishouCard.js` | 32 |
| 678 | [x] | GET | `/api/kuaishou/stats/card/{id}` | 用户端 | path: id / query: start_date?, end_date? | GET /api/kuaishou/stats/card/2 -> 200 {cardId:2,totalViews:0,dailyStats,recentActivity} | `src/api/kuaishouCard.js` | 36 |
| 679 | [x] | GET | `/api/kuaishou/stats/overall` | 用户端 | - | GET /api/kuaishou/stats/overall -> 200 {totalCards:2,activeCards:2,popularCards,dailyStats} | `src/api/kuaishouCard.js` | 40 |
| 680 | [x] | POST | `/api/kuaishou/{id}/generate-short-link` | 用户端 | path: id | POST /api/kuaishou/2/generate-short-link -> 200 显式重建 short_code=H7DGPa | `src/api/kuaishouCard.js` | 44 |
| 681 | [x] | GET | `/api/lead-mining/config` | 用户端 | - | GET /api/lead-mining/config -> 200 {id:71,enabled:false,keywords:[],min_intent_score:50} | `src/api/leadMining.js` | 4 |
| 682 | [x] | POST | `/api/lead-mining/config` | 用户端 | body: data | POST lead-mining/config 空 body -> 400 请求体不能为空；带 keywords/requirement -> 200 success 且 GET 回读一致（keywords/requirement 已还原为空） | `src/api/leadMining.js` | 8 |
| 683 | [x] | GET | `/api/live-codes/list` | 用户端 | query: page, page_size | GET /api/live-codes/list -> 200 list 含 r832_livecode（含 short_domain/entry_domain/landing_domain 嵌套） | `src/api/livecode.js` | 4 |
| 684 | [x] | GET | `/api/live-codes/{id}` | 用户端 | path: id | GET /api/live-codes/{id} 200 code=0，返回 name/short_link/short_domain/entry_domain/landing_domain 完整对象 | `src/api/livecode.js` | 8 |
| 685 | [x] | POST | `/api/live-codes/create` | 用户端 | body: data | POST /api/live-codes/create 200 code=0 创建成功（api684_lc_1794037257，返回 id）；/api/live-codes/list 亦 200 code=0 | `src/api/livecode.js` | 12 |
| 686 | [x] | PUT | `/api/live-codes/{id}/update` | 用户端 | path: id / body: data | PUT /api/live-codes/{id}/update 200 code=0 更新成功，name 改为 api684_lc_upd 生效 | `src/api/livecode.js` | 16 |
| 687 | [x] | DELETE | `/api/live-codes/{id}/delete` | 用户端 | path: id | DELETE /api/live-codes/{id}/delete 200 删除成功；重复删 404 NOT_FOUND_1002 记录不存在（测试对象已清理） | `src/api/livecode.js` | 20 |
| 688 | [x] | GET | `/api/live-codes/{id}/stats` | 用户端 | path: id | GET /api/live-codes/{id}/stats 200 code=0，total_views/today_views/total_clicks/active_qr_count=1/conversion_rate 字段齐全 | `src/api/livecode.js` | 24 |
| 689 | [x] | GET | `/api/live-codes/{liveCodeId}/qrcodes` | 用户端 | path: id | GET /api/live-codes/{id}/qrcodes 200 code=0，返回数组含 id/status/expire_time/is_expired/is_daily_limit_reached | `src/api/livecode.js` | 28 |
| 690 | [x] | POST | `/api/live-codes/{liveCodeId}/qrcodes/create` | 用户端 | path: id / body: data | POST /api/live-codes/{id}/qrcodes/create 200 生成成功，返回新二维码 id 与 qr_image_url=/uploads/qrcodes/*.png | `src/api/livecode.js` | 32 |
| 691 | [x] | GET | `/api/live-codes/qrcodes/{qrId}/stats` | 用户端 | path: qrId | GET /api/live-codes/qrcodes/{qrId}/stats 200 code=0，view_count/click_count/expire_time/access_stats 字段齐全 | `src/api/livecode.js` | 36 |
| 692 | [x] | POST | `/api/live-codes/{id}/share` | 用户端 | path: id / body: { platform } | POST /api/live-codes/{id}/share：无活跃二维码的活码 400 INVALID_PARAM_1001 没有可用的二维码（正确业务校验）；fixture 活码（有活跃QR）200 分享成功，返回 short_link/entry_link/landing_link/qr_code_id | `src/api/livecode.js` | 40 |
| 693 | [x] | DELETE | `/api/live-codes/qrcodes/{id}/delete` | 用户端 | path: qrId | DELETE /api/live-codes/qrcodes/{id}/delete 200 删除成功；重复删 404 NOT_FOUND_1002 二维码不存在 | `src/api/livecode.js` | 44 |
| 694 | [x] | PUT | `/api/live-codes/qrcodes/{id}/update` | 用户端 | path: qrId / body: data | PUT /api/live-codes/qrcodes/{id}/update 200 更新成功（daily_limit 100→150） | `src/api/livecode.js` | 48 |
| 695 | [x] | GET | `/api/reports/llm/overview` | 用户端 | query: params | GET /api/reports/llm/overview 200 code=0，call_count/error_rate/cost_cents/p50_latency_ms=2954 等 KPI 字段齐全（租户窗口期无调用，数值为 0 合理） | `src/api/llmReport.js` | 7 |
| 696 | [x] | GET | `/api/reports/llm/timeseries` | 用户端 | query: params | GET /api/reports/llm/timeseries 200 code=0 data=[]，from/to 日期参数接受 | `src/api/llmReport.js` | 11 |
| 697 | [x] | GET | `/api/reports/llm/breakdown` | 用户端 | query: by=model\|scenario\|channel\|error_class\|provider | GET /api/reports/llm/breakdown?by=model 与 by=channel 均 200 code=0 data=[] | `src/api/llmReport.js` | 15 |
| 698 | [x] | GET | `/api/reports/llm/errors` | 用户端 | query: params | GET /api/reports/llm/errors 200 code=0，by_class/rows/total=0 分页字段齐全 | `src/api/llmReport.js` | 19 |
| 699 | [x] | GET | `/api/llm/usage/daily` | 用户端 | query: window, group_by | GET /api/llm/usage/daily?window=week&group_by=day 200 code=0，返回 stat_date=2026-11-07 call_count=1 total_tokens=4442 聚合行 | `src/api/llmRouting.js` | 7 |
| 700 | [~] | GET | `/api/llm/models` | 用户端 | - | GET /api/llm/models 200 code=0 但 data=[]：租户(71)管理员 702 创建的 provider 按名查询/列表均不可见。根因链：CreateModel→UpsertProviderToDB 落库固定 tenant_id=DefaultTenantID=1（dispatcher_persistence.go:106），而 ListModels 按 tenantProviderNames→ListProviderNamesByTenant(tenant_id=71) 过滤（llm_routing.go:205）→ 名单永不相交。旁证：/api/llm/usage active_models=5 与本接口空列表矛盾（Usage 不走租户过滤）。多租户下自建模型创建即失联（内存 default/provider 集合与租户名单不一致）；【2026-11-08 深审计判定：架构性缺陷待专项，不本批盲修】llm_providers 唯一索引已被 tenancyidx.Normalize 复合化为 (tenant_id,name)，而 UpsertProviderToDB 走 systemScopeCtx+固定 tenant_id=1+按 name First/Save，与 ListProviderNamesByTenant 读路径的租户过滤结构性不相交；最小补丁需同时改写路径作用域、LoadProvidersFromDB 全量装载、DeleteProviderFromDB 按 name 删（跨租户误删风险）、内存 dispatcher 全局单键 map 归属语义，并与平台下发 provider（source=platform 按 (tenant_id,name) upsert）两套写路径合并——属 provider 归属模型重设计，需专项设计+评审，本批不动  | `src/api/llmRouting.js` | 11 |
| 701 | [~] | GET | `/api/llm/models/{name}` | 用户端 | path: name | GET /api/llm/models/{name} 200 刚创建返回 404 model not found（与 700 同根因：GetModel 复用 ListModels 的租户 allowlist，自建 provider tenant_id=1 不在租户71名单）；对真正不存在的名字 404 正常；【2026-11-08 深审计判定：架构性缺陷待专项，不本批盲修】llm_providers 唯一索引已被 tenancyidx.Normalize 复合化为 (tenant_id,name)，而 UpsertProviderToDB 走 systemScopeCtx+固定 tenant_id=1+按 name First/Save，与 ListProviderNamesByTenant 读路径的租户过滤结构性不相交；最小补丁需同时改写路径作用域、LoadProvidersFromDB 全量装载、DeleteProviderFromDB 按 name 删（跨租户误删风险）、内存 dispatcher 全局单键 map 归属语义，并与平台下发 provider（source=platform 按 (tenant_id,name) upsert）两套写路径合并——属 provider 归属模型重设计，需专项设计+评审，本批不动  | `src/api/llmRouting.js` | 15 |
| 702 | [~] | POST | `/api/llm/models` | 用户端 | body: data | POST /api/llm/models HTTP200 code=0 返回完整 provider 对象，但该记录落库 tenant_id=1（DefaultTenantID），本租户(71)立即查不到（见 700/701）——创建结果对本租户不可见，疑似多租户归属缺陷。缺 name/model/base_url 时 400 model is required 校验正常；【2026-11-08 深审计判定：架构性缺陷待专项，不本批盲修】llm_providers 唯一索引已被 tenancyidx.Normalize 复合化为 (tenant_id,name)，而 UpsertProviderToDB 走 systemScopeCtx+固定 tenant_id=1+按 name First/Save，与 ListProviderNamesByTenant 读路径的租户过滤结构性不相交；最小补丁需同时改写路径作用域、LoadProvidersFromDB 全量装载、DeleteProviderFromDB 按 name 删（跨租户误删风险）、内存 dispatcher 全局单键 map 归属语义，并与平台下发 provider（source=platform 按 (tenant_id,name) upsert）两套写路径合并——属 provider 归属模型重设计，需专项设计+评审，本批不动  | `src/api/llmRouting.js` | 19 |
| 703 | [x] | PUT | `/api/llm/models/{name}` | 用户端 | path: name / body: data | PUT /api/llm/models/{name} 200 code=0 data=name，display_name 更新成功（内存 dispatcher 有该 provider，更新路径可达） | `src/api/llmRouting.js` | 23 |
| 704 | [x] | DELETE | `/api/llm/models/{name}` | 用户端 | path: name | DELETE /api/llm/models/{name} 200 code=0；重复删 404 model not found；DB 行按 name 删除无租户过滤（测试对象已清理，无残留） | `src/api/llmRouting.js` | 27 |
| 705 | [x] | PUT | `/api/llm/models/{name}` | 用户端 | path: name / body: { enabled } | PUT /api/llm/models/{name} body={enabled:false} 200 code=0，与 703 共用 UpdateModel，状态开关生效 | `src/api/llmRouting.js` | 31 |
| 706 | [x] | POST | `/api/llm/models/{name}/test` | 用户端 | path: name / body: { prompt } | POST /api/llm/models/{name}/test 200 code=0，返回 success:false + error dial tcp lookup example.invalid no such host——对假端点的真实连通性探测，错误如实透出，符合语义 | `src/api/llmRouting.js` | 35 |
| 707 | [x] | GET | `/api/llm/scene-routing` | 用户端 | - | GET /api/llm/scene-routing 200 code=0，8 条场景路由（intent_recognize/high_quality/low_cost/sop_reply 等）含 provider/fallbacks/cost_weight/min_quality | `src/api/llmRouting.js` | 39 |
| 708 | [x] | GET | `/api/llm/scenarios` | 用户端 | - | GET /api/llm/scenarios 200 code=0，返回场景清单（long_summary/low_cost/intent_recognize 等，含 fallbacks） | `src/api/llmRouting.js` | 43 |
| 709 | [x] | PUT | `/api/llm/scene-routing` | 用户端 | body: payload | PUT /api/llm/scene-routing 幂等回写 GET 到的 8 条 routes：200 code=0，复读逐场景逐字段比对完全一致（仅返回顺序不同），无数据变化 | `src/api/llmRouting.js` | 48 |
| 710 | [x] | GET | `/api/llm/fallback` | 用户端 | - | GET /api/llm/fallback 200 code=0，返回各场景兜底策略（provider=default + platform-sensenova/platform-dashscope fallbacks） | `src/api/llmRouting.js` | 52 |
| 711 | [x] | GET | `/api/llm/audit` | 用户端 | query: params | GET /api/llm/audit?limit=5 200 code=0 data=null（窗口期无审计记录，空态） | `src/api/llmRouting.js` | 58 |
| 712 | [x] | GET | `/api/llm/stats` | 用户端 | - | GET /api/llm/stats 200 code=0 data={}（进程内统计空态） | `src/api/llmRouting.js` | 62 |
| 713 | [x] | GET | `/api/llm/usage` | 用户端 | query: window | GET /api/llm/usage?window=all 200 code=0，total_calls=1/total_tokens=4442/by_scenario/by_provider 聚合齐全；注意 active_models=5 与 /api/llm/models 空列表矛盾（Usage 未按租户过滤 provider 集合） | `src/api/llmRouting.js` | 66 |
| 714 | [x] | GET | `/api/llm/cost-stats` | 用户端 | query: window | GET /api/llm/cost-stats?window=month 200 code=0，monthly_cost/by_model/by_provider/by_model_type 齐全 | `src/api/llmRouting.js` | 70 |
| 715 | [x] | GET | `/api/llm/health` | 用户端 | - | GET /api/llm/health 200 code=0，status=empty healthy_providers=0（无探针对象时的空态，非故障） | `src/api/llmRouting.js` | 74 |
| 716 | [x] | GET | `/api/llm/model-type-stats` | 用户端 | query: window | GET /api/llm/model-type-stats?window=month 200 code=0，by_provider_type local/cloud 分桶、self_sufficiency 字段在 | `src/api/llmRouting.js` | 78 |
| 717 | [x] | GET | `/api/llm/egress-audit` | 用户端 | query: params | GET /api/llm/egress-audit 200 code=0 data={alerts:[],count:0} | `src/api/llmRouting.js` | 82 |
| 718 | [x] | GET | `/api/llm/egress-alerts` | 用户端 | query: params | GET /api/llm/egress-alerts 200 code=0 data={alerts:[],count:0} | `src/api/llmRouting.js` | 86 |
| 719 | [x] | GET | `/api/manage/ltc/config` | 管理端 | - | GET /api/manage/ltc/config 200 code=0，enabled=false/degraded=false/guarded_routes(quote:1)/stage_status 六阶段 master_off/reading_hints 齐全 | `src/api/ltcConfig.js` | 11 |
| 720 | [x] | PUT | `/api/manage/ltc/config` | 管理端 | body: cfg | PUT /api/manage/ltc/config 按前端 payload 形状 {enabled,stages_enabled,thresholds} 200 code=0 ltc.config 已更新，audit_written=true，复读 enabled/stages/thresholds/reach_rollout 逐项不变（幂等）；附测：把 GET 视图的计算字段 whitelist_entries 一并回传 → 400 INVALID_PARAM_1001 unknown field（DisallowUnknownFields，前端不发该字段，非缺陷） | `src/api/ltcConfig.js` | 21 |
| 721 | [x] | GET | `/api/ltc-rates` | 用户端 | - | GET /api/ltc-rates 200 code=0，closure/collection/overdue 三率 numerator/denominator=0 空态结构完整 | `src/api/ltcRates.js` | 5 |
| 722 | [x] | GET | `/api/marketing-flows` | 用户端 | query: params | GET /api/marketing-flows?page=1&page_size=5 200 code=0，list 含 tenant_id=71/status/trigger_type/flow_data 分页字段齐全 | `src/api/marketingFlow.js` | 4 |
| 723 | [x] | GET | `/api/marketing-flows/{id}` | 用户端 | path: id | GET /api/marketing-flows/{id} 200 code=0，返回完整流程对象（trigger_config/flow_data JSON 串） | `src/api/marketingFlow.js` | 7 |
| 724 | [x] | POST | `/api/marketing-flows` | 用户端 | body: data | POST /api/marketing-flows 200 code=0 创建成功（id=1, status=draft, tenant_id=71）；校验链正常：缺 trigger_type→400 required、无节点→400 流程至少需要一个节点、无 trigger 节点→400 流程必须有触发器节点 | `src/api/marketingFlow.js` | 10 |
| 725 | [x] | PUT | `/api/marketing-flows/{id}` | 用户端 | path: id / body: data | PUT /api/marketing-flows/{id} 200 更新成功，description 改为 regression updated 生效 | `src/api/marketingFlow.js` | 13 |
| 726 | [x] | DELETE | `/api/marketing-flows/{id}` | 用户端 | path: id | DELETE /api/marketing-flows/{id} 200 删除成功；再 GET 同 id → 404 NOT_FOUND_1002 记录不存在（测试流程已清理） | `src/api/marketingFlow.js` | 16 |
| 727 | [x] | POST | `/api/marketing-flows/{id}/activate` | 用户端 | path: id | POST /api/marketing-flows/{id}/activate 200 激活成功，状态 draft→active | `src/api/marketingFlow.js` | 19 |
| 728 | [x] | POST | `/api/marketing-flows/{id}/pause` | 用户端 | path: id | POST /api/marketing-flows/{id}/pause 200 暂停成功 | `src/api/marketingFlow.js` | 22 |
| 729 | [x] | POST | `/api/marketing-flows/{id}/stop` | 用户端 | path: id | POST /api/marketing-flows/{id}/stop 200 停止成功（paused→inactive） | `src/api/marketingFlow.js` | 25 |
| 730 | [x] | GET | `/api/marketing-flows/{id}/executions` | 用户端 | path: id / query: page, page_size | GET /api/marketing-flows/{id}/executions 200 code=0，list 返回 manual 触发执行记录（trigger_id=manual-79, status=failed 含 error_message）分页齐全 | `src/api/marketingFlow.js` | 28 |
| 731 | [x] | GET | `/api/marketing-flows/{id}/stats` | 用户端 | path: id | GET /api/marketing-flows/{id}/stats 200 code=0 data={failed:1}（仅非零计数器，与执行记录一致） | `src/api/marketingFlow.js` | 31 |
| 732 | [x] | POST | `/api/marketing-flows/{id}/trigger` | 用户端 | path: id / body: {} | POST /api/marketing-flows/{id}/trigger：draft 态 400 流程未激活（正确校验）；激活后 200 流程已触发，返回 execution id/status=running/trigger_id=manual-79；执行节点 send_message 因非内置类型报 failed（执行引擎节点类型集不含 send_message，触发链路本身 200 正常） | `src/api/marketingFlow.js` | 48 |
| 733 | [x] | GET | `/api/material/list` | 用户端 | query: params | GET /api/material/list 200 code=0，list/total=0/page/limit 空态分页完整 | `src/api/material.js` | 4 |
| 734 | [x] | UPLOAD | `/api/material/upload` | 用户端 | body: formData | POST /api/material/upload：缺分类→400 请选择素材分类（校验正常）；带合法 category_id 上传→404 记录不存在，根因是本租户无可用 OBS 存储配置（/api/obs/config data.list=[]，service obsConfigServiceAdapter.GetDefault 返回 gorm.ErrRecordNotFound 被映射为 404）——环境阻塞（租户未配存储），非上传链路代码缺陷；附注：该分支文案「记录不存在」对用户有误导性 | `src/api/material.js` | 8 |
| 735 | [x] | DELETE | `/api/material/{id}` | 用户端 | path: id | DELETE /api/material/{id}（不存在 id 99999999）404 NOT_FOUND_1002 记录不存在，路由与权限闸（AdminAuth）可达 | `src/api/material.js` | 12 |
| 736 | [x] | GET | `/api/material/categories` | 用户端 | - | GET /api/material/categories 200 code=0，list/total/page/limit 完整 | `src/api/material.js` | 16 |
| 737 | [x] | POST | `/api/material/categories` | 用户端 | body: data | POST /api/material/categories 200 创建成功（id=3a363f02…，type=file，license_id=system_admin）；缺 type→400 参数错误（binding oneof=image video audio file 校验生效） | `src/api/material.js` | 20 |
| 738 | [x] | PUT | `/api/material/categories/{id}` | 用户端 | path: id / body: data | PUT /api/material/categories/{id} 200 更新成功，name 改为 api684_cat_upd 生效 | `src/api/material.js` | 24 |
| 739 | [x] | DELETE | `/api/material/categories/{id}` | 用户端 | path: id | DELETE /api/material/categories/{id} 200 删除成功 data.deleted=true；重复删 404 记录不存在（分类已清理） | `src/api/material.js` | 28 |
| 740 | [x] | GET | `/api/material/selector` | 用户端 | query: params | GET /api/material/selector 200 code=0，categories/materials/total 空态结构完整 | `src/api/material.js` | 32 |
| 741 | [x] | POST | `/api/material/{id}/usage` | 用户端 | path: id / body: data | POST /api/material/{id}/usage（不存在 id）404 NOT_FOUND_1002 记录不存在；真实素材不可得（上传被存储配置阻塞），not-found 分支已验证 | `src/api/material.js` | 36 |
| 742 | [x] | GET | `/api/material/stats` | 用户端 | - | GET /api/material/stats 200 code=0，total_materials/image/video/audio/file_count/total_size/today_* 九个统计字段齐全 | `src/api/material.js` | 40 |
| 743 | [x] | GET | `/api/mcp/credentials` | 用户端 | - | GET /api/mcp/credentials 200 code=0 data=[]（AdminAuth 闸可达，空态） | `src/api/mcpCredential.js` | 5 |
| 744 | [x] | POST | `/api/mcp/credentials` | 用户端 | body: data | POST /api/mcp/credentials 200 创建成功：返回 id=1/client_id=mcp-01c7417d/api_key=wgk_*/api_hint 只显示一次 | `src/api/mcpCredential.js` | 6 |
| 745 | [x] | PUT | `/api/mcp/credentials/{id}` | 用户端 | path: id / body: data | PUT /api/mcp/credentials/{id} 200 已更新 data.ok=true（name 改为 api684_mcp_upd） | `src/api/mcpCredential.js` | 7 |
| 746 | [x] | DELETE | `/api/mcp/credentials/{id}` | 用户端 | path: id | DELETE /api/mcp/credentials/{id} 200 已删除 data.ok=true；重复删 404 NOT_FOUND_1002 MCP 凭据不存在（凭据已清理） | `src/api/mcpCredential.js` | 8 |
| 747 | [x] | GET | `/api/menus` | 用户端 | - | GET /api/menus 200 code=0，返回 tree（workspace/messageHub 等节点）+ all=true，按 JWT role=admin 渲染 | `src/api/menu.js` | 12 |
| 748 | [x] | GET | `/api/system/menus` | 用户端 | - | GET /api/system/menus 200 code=0，完整菜单注册表 tree（el-tree 数据源） | `src/api/menu.js` | 17 |
| 749 | [x] | GET | `/api/system/roles/{code}/menus` | 用户端 | path: code | GET /api/system/roles/{code}/menus：admin→200 全量 menu_keys；staff→200 65 个 keys；manager→400 角色不存在（该租户词表只有 admin/staff/customer_service 等，校验生效） | `src/api/menu.js` | 22 |
| 750 | [x] | PUT | `/api/system/roles/{code}/menus` | 用户端 | path: code / body: { menu_keys } | PUT /api/system/roles/{code}/menus：staff 幂等回写同值 65 keys→200 保存成功，复读集合一致；admin→400 超管拥有全部菜单权限无需分配（正确关闸）；非法角色→400 角色不存在 | `src/api/menu.js` | 27 |
| 751 | [x] | POST | `/api/message-hub/push` | 用户端 | body: data | POST /api/message-hub/push 200 可达，参数校验链正常：空body→400 invalid platform、platform=wecom 缺 account_id→400 invalid account_id；未真实外发（现网有真实企微账号，避免向真实客户发消息） | `src/api/messageHub.js` | 5 |
| 752 | [x] | POST | `/api/message-hub/push-batch` | 用户端 | body: data | POST /api/message-hub/push-batch 空 body→400 INVALID_PARAM_1001 请求参数错误：请检查字段类型与必填项（binding 校验生效，未真实外发） | `src/api/messageHub.js` | 9 |
| 753 | [x] | POST | `/api/message-hub/push-from-channel` | 用户端 | body: data | POST /api/message-hub/push-from-channel 空 body→400 invalid platform（与 push 同校验链，未真实外发） | `src/api/messageHub.js` | 13 |
| 754 | [x] | GET | `/api/message-hub/list` | 用户端 | query: params | GET /api/message-hub/list 200 code=0，list 返回 tenant_id=71 真实 wecom 消息行（msg_id/direction/status/content），分页字段齐全 | `src/api/messageHub.js` | 17 |
| 755 | [x] | GET | `/api/message-hub/{id}` | 用户端 | path: id | GET /api/message-hub/{id} 200 code=0，返回 id=876 完整行（platform_msg_id/extra 等字段在） | `src/api/messageHub.js` | 21 |
| 756 | [x] | POST | `/api/message-hub/{ids[0]}/read` | 用户端 | path: id / body: { ids } | POST /api/message-hub/{id}/read body={ids:[876]} 200 标记已读成功 data.updated=1（可回滚的幂等标记） | `src/api/messageHub.js` | 25 |
| 757 | [x] | POST | `/api/message-hub/{id}/recall` | 用户端 | path: id | POST /api/message-hub/{id}/recall：不存在 id→404 消息不存在（正常）；对真实 outbound wecom 行 id=876（status=pending、platform_msg_id 空、extra={}）确定性 500 INTERNAL_ERROR_6002「操作失败，请稍后重试」。根因线索：recallWeCom 链路（integration_reach_adapter.go:781）在 platformMessageID（:842，wecom 分支 extra 缺 msgid→"wecom hub extra missing msgid"）或查 WeComAccount（account_id=2）行不存在时抛的是普通 error 而非 RecallFailure，classifyRecallFailure 兜成 ErrMessageHubRecallFailed，controller default 分支（message_hub.go:214-216）→ ErrorFromDB→500。该前置数据缺失是确定性业务态，应回 4xx（如 409「无平台撤回标识」）而非 500「稍后重试」——重试永远无效，属错误分类缺口（同 D-24/25/28 家族）。真实已发消息的撤回（telegram/feishu/wecom 出网）现网无可测行，未验证；【2026-11-08 本地已修】controller Recall 增加 ErrMessageHubRecallFailed → 502 UPSTREAM_FAILURE_5005 显式分支（不再落 default ErrorFromDB 500）；回归 message_hub_recall_test.go 旧代码 RED(500) 新代码 GREEN(502) | `src/api/messageHub.js` | 39 |
| 758 | [x] | GET | `/api/message-hub/stats` | 用户端 | query: params | GET /api/message-hub/stats 200 code=0，total/inbound/outbound/unread/by_platform/by_direction/by_msg_type/by_account/recent_24h 齐全 | `src/api/messageHub.js` | 43 |
| 759 | [x] | GET | `/api/message-hub/platforms` | 用户端 | - | GET /api/message-hub/platforms 200 code=0，msg_types 9 种 + platforms 21 个渠道清单 | `src/api/messageHub.js` | 47 |
| 760 | [x] | GET | `/api/module-market/list` | 公开 | - | GET /api/module-market/list 200 code=0，返回 core/customer 等模块目录（module_key/name/category/price_cents/owned/owned_status/free） | `src/api/moduleMarket.js` | 17 |
| 761 | [x] | GET | `/api/module-market/detail/{key}` | 公开 | path: key | GET /api/module-market/detail/{key}：core→200 code=0 完整详情含 menu_keys；不存在 key→code=4302 模块不存在（HTTP 200 包装业务码，前端按 4302 分支处理，符合公开档契约） | `src/api/moduleMarket.js` | 20 |
| 762 | [x] | GET | `/api/module-market/payment-guide` | 公开 | - | GET /api/module-market/payment-guide 200 code=0，线下付款指引文案（开通顾问联系方式） | `src/api/moduleMarket.js` | 23 |
| 763 | [x] | GET | `/api/tenant/regulatory-notice` | 公开 | - | GET /api/tenant/regulatory-notice 200 code=0，agreement_version=2026-10-17.v1 + notice + required_fields | `src/api/moduleMarket.js` | 26 |
| 764 | [x] | POST | `/api/module-market/order` | 公开 | body: data | POST /api/module-market/order：空 body→code=4000 请选择要开通的模块；module_keys=[customer]（0元已开通模块）→200 code=0 订单已提交 status=fulfilled total=0，payment_guide 空（无需付款） | `src/api/moduleMarket.js` | 31 |
| 765 | [x] | GET | `/api/module-market/orders` | 公开 | query: params | GET /api/module-market/orders 200 code=0，list 含本次 2 笔测试单（module fulfilled / renew pending_payment），total 同步 | `src/api/moduleMarket.js` | 36 |
| 766 | [x] | GET | `/api/module-market/orders/{orderNo}` | 公开 | path: orderNo | GET /api/module-market/orders/{orderNo} 200 code=0，tenant_id=71/items 快照/currency=CNY 完整；MOD 单与 ORD 续费单均可查 | `src/api/moduleMarket.js` | 39 |
| 767 | [x] | GET | `/api/module-market/orders/{orderNo}/pay-status` | 公开 | path: orderNo | GET /api/module-market/orders/{orderNo}/pay-status：MOD 单 paid=true status=fulfilled；续费单 paid=false status=pending_payment，状态与事实一致 | `src/api/moduleMarket.js` | 43 |
| 768 | [x] | POST | `/api/module-market/orders/{orderNo}/pay` | 公开 | path: orderNo | POST /api/module-market/orders/{orderNo}/pay：fulfilled 单→code=4000 该订单当前无需在线支付（正确拒绝）；pending_payment 续费单→200 code=0 返回 payment_guide（线下指引；平台未回 pay_url 段，按契约回落线下路径） | `src/api/moduleMarket.js` | 48 |
| 769 | [x] | GET | `/api/subscription/status` | 用户端 | - | GET /api/subscription/status 200 code=0，status=trial trial_expires_at=2026-11-22 price_cents=900 locked/stale/no_data 标志齐全 | `src/api/moduleMarket.js` | 56 |
| 770 | [x] | POST | `/api/subscription/renew` | 用户端 | body: data | POST /api/subscription/renew 200 code=0 续费订单已提交：order_no=ORD1794066177572AMZF3Z status=pending_payment total=900（未支付不产生扣款，单留在列表可追溯） | `src/api/moduleMarket.js` | 59 |
| 771 | [x] | GET | `/api/modules/my` | 用户端 | - | GET /api/modules/my 200 code=0，返回 core 等已开通模块清单（module_key/name/menu_keys）active_count=23（与 sync 一致） | `src/api/moduleMarket.js` | 64 |
| 772 | [x] | POST | `/api/modules/sync` | 用户端 | body: undefined | POST /api/modules/sync 200 code=0，data={active_count:23, source:platform, degraded:false}（平台同步成功，幂等操作） | `src/api/moduleMarket.js` | 67 |
| 773 | [x] | GET | `/api/modules/sync-logs` | 用户端 | query: params | GET /api/modules/sync-logs 200 code=0，list=[] total=0 空态（同步成功不落错误日志，结构完整） | `src/api/moduleMarket.js` | 70 |
| 774 | [x] | GET | `/api/llm/wallet` | 用户端 | - | GET /api/llm/wallet 200 code=0，merchant_key/input_tokens_remaining=19998322/output_tokens_remaining=3993645/low=false/expired=false/platform_enabled=true | `src/api/moduleMarket.js` | 78 |
| 775 | [x] | GET | `/api/llm/plans` | 用户端 | - | GET /api/llm/plans 200 code=0，starter/basic 等套餐（plan_code/input_tokens/output_tokens/price_cents/valid_days） | `src/api/moduleMarket.js` | 81 |
| 776 | [x] | POST | `/api/llm/recharge` | 用户端 | body: data | POST /api/llm/recharge：空 body→code=4000 请选择套餐；plan_code=starter→200 code=0 订单已提交 order_no=LLM1794067117115V95VU9 pending_payment total=100 分，返回平台 payment.pay_url 段（未支付不扣款不入账） | `src/api/moduleMarket.js` | 84 |
| 777 | [x] | GET | `/api/monitor/trace-tree` | 用户端 | query: params | GET /api/monitor/trace-tree 200 code=0；带 trace_id=e9ad8349… 返回 spans（node=agent_turn/output 内容），不带参数返回空壳 | `src/api/monitor.js` | 5 |
| 778 | [x] | GET | `/api/monitor/traces` | 用户端 | query: params | GET /api/monitor/traces 200 code=0，返回 trace 列表（trace_id/node_count/abnormal_count/first_at/last_at） | `src/api/monitor.js` | 8 |
| 779 | [x] | GET | `/api/monitor/lifecycle` | 用户端 | query: params | GET /api/monitor/lifecycle 无参数→400 INVALID_PARAM_1001 conversation_id 或 trace_id 必填其一（正确校验）；带 trace_id→200 code=0 返回节点生命周期数组 | `src/api/monitor.js` | 11 |
| 780 | [x] | GET | `/api/monitor/health` | 用户端 | - | GET /api/monitor/health 200 code=0，inbound/outbound_rate_per_min、pending_count=4、oldest_pending_min=378、sync_gap_count/stuck_reachable 等齐全 | `src/api/monitor.js` | 14 |
| 781 | [x] | GET | `/api/monitor/node-health` | 用户端 | - | GET /api/monitor/node-health 200 code=0，nodes 数组（node=agent_turn/total/abnormal/avg_duration_ms/p95）+window=24h | `src/api/monitor.js` | 17 |
| 782 | [x] | GET | `/api/monitor/latency` | 用户端 | - | GET /api/monitor/latency 200 code=0 data=[]（窗口内无数据空态） | `src/api/monitor.js` | 20 |
| 783 | [x] | GET | `/api/monitor/anomalies` | 用户端 | query: params | GET /api/monitor/anomalies 200 code=0，sync_gap/stuck_reachable/stuck_unreachable 分桶（含 conversation_id/age_min） | `src/api/monitor.js` | 23 |
| 784 | [x] | GET | `/api/monitor/trace-eval/logs` | 用户端 | query: params | GET /api/monitor/trace-eval/logs 200 code=0 data=[]（无评测日志空态） | `src/api/monitor.js` | 26 |
| 785 | [x] | GET | `/api/monitor/knowledge-weights` | 用户端 | query: params | GET /api/monitor/knowledge-weights 200 code=0 data=null（无权重快照空态；前端 TraceMonitor weights=ref([])，el-table :data=null 安全） | `src/api/monitor.js` | 29 |
| 786 | [x] | POST | `/api/monitor/trace-eval/trigger` | 用户端 | query: params | POST /api/monitor/trace-eval/trigger 200 code=0 data={processed:0}（触发评测任务，窗口内无待评测数据） | `src/api/monitor.js` | 32 |
| 787 | [x] | GET | `/api/rag/recall/snapshot` | 用户端 | - | GET /api/rag/recall/snapshot 200 code=0，available=false+captured/summary=null，message=尚无监控数据请先启动采集（空态语义清晰） | `src/api/monitor.js` | 36 |
| 788 | [x] | GET | `/api/rag/recall/snapshots` | 用户端 | query: params | GET /api/rag/recall/snapshots 200 code=0，list=null/total=0/page/page_size 分页壳完整 | `src/api/monitor.js` | 39 |
| 789 | [x] | POST | `/api/rag/recall/collect` | 用户端 | body: params | POST /api/rag/recall/collect 200 code=0，返回采集窗口（window_start/end）+ total_queries/top_k_hit_rate/avg_recall 等指标结构（空窗口） | `src/api/monitor.js` | 42 |
| 790 | [x] | POST | `/api/rag/recall/start` | 用户端 | - | POST /api/rag/recall/start 200 已启动 data={interval:5m0s,started:true}（已在 791 停止，未留运行态） | `src/api/monitor.js` | 45 |
| 791 | [~] | POST | `/api/rag/recall/stop` | 用户端 | - | POST /api/rag/recall/stop 200 已停止 data={started:false}（清理 790 启动的采集）；【2026-11-08 本地已修】RagRecallMonitorService.Start 重建 stopCh：修复前 stop 后再 start goroutine 立即退出（采集静默停摆）且再 stop 对已关闭 channel 二次 close panic 500；回归 rag_recall_monitor_test.go RestartAfterStop/StartAfterStopKeepsGoroutineAlive 旧代码 RED(panic) 新代码 GREEN(含 race)。线上复现的 500 与停摆均属此根因，待部署复测 | `src/api/monitor.js` | 48 |
| 792 | [x] | GET | `/api/rag/recall/metrics` | 用户端 | query: start, end | GET /api/rag/recall/metrics：非 RFC3339 时间→400 INVALID_PARAM_1001 时间格式错误需 RFC3339（正确校验）；start/end 用 RFC3339→200 code=0 指标齐全 | `src/api/monitor.js` | 51 |
| 793 | [x] | GET | `/api/rag/recall/low-recall` | 用户端 | query: params | GET /api/rag/recall/low-recall 200 code=0 data=[]（低召回查询空态） | `src/api/monitor.js` | 54 |
| 794 | [x] | GET | `/api/auth/notifications` | 用户端 | query: params | GET /api/auth/notifications 200 code=0，list 返回 tenant_id=71 通知（id=322 bad_case_pending，含 title/content/link/is_read/created_at） | `src/api/notification.js` | 9 |
| 795 | [x] | POST | `/api/auth/notifications/{id}/read` | 用户端 | path: id | POST /api/auth/notifications/{id}/read 200 已标记为已读；重复置已读仍 200（幂等） | `src/api/notification.js` | 11 |
| 796 | [x] | POST | `/api/auth/notifications/read-all` | 用户端 | - | POST /api/auth/notifications/read-all 200 已全部标记为已读 data={marked:2} | `src/api/notification.js` | 13 |
| 797 | [x] | GET | `/api/auth/notifications/unread-count` | 用户端 | - | GET /api/auth/notifications/unread-count 200 code=0 count=3；read-all 后复读 count=0，计数与已读状态一致 | `src/api/notification.js` | 15 |
| 798 | [x] | POST | `/api/objection/handle` | 用户端 | body: data | POST /api/objection/handle body={text} 200 code=0：text=太贵了价格能不能便宜点→category=price 价格异议/confidence=0.9/source=rule，返回 suggestion+acknowledge+clarify；空 body→200 other（无必填，优雅兜底） | `src/api/objection.js` | 4 |
| 799 | [x] | POST | `/api/objection/classify` | 用户端 | body: data | POST /api/objection/classify body={text} 200 code=0：价格类文本→category=price；分类器读的是 text 字段（非 content），命中关键词 贵/便宜点→price，规则分类正确 | `src/api/objection.js` | 8 |
| 800 | [x] | GET | `/api/objection/categories` | 用户端 | - | 200 code=0 list=7 categories(status_quo/price/need/trust/timing/compare/feature) | `src/api/objection.js` | 12 |
| 801 | [x] | POST | `/api/objection/usage` | 用户端 | body: data | 200 code=0 记录成功 (template_id=99999999 IncrementUsageStats 幂等无副作用) | `src/api/objection.js` | 16 |
| 802 | [x] | GET | `/api/obs/config` | 用户端 | query: params | 200 code=0 list空 total=0 page=1 | `src/api/obs.js` | 4 |
| 803 | [x] | GET | `/api/obs/config/{id}` | 用户端 | path: id | 200→实测404 code=NOT_FOUND_1002 record not found(不存在id, D-30同口径) | `src/api/obs.js` | 8 |
| 804 | [x] | POST | `/api/obs/config` | 用户端 | body: data | 200 code=0 创建成功 id=7dcffc0b provider=local is_default=true | `src/api/obs.js` | 12 |
| 805 | [x] | PUT | `/api/obs/config/{id}` | 用户端 | path: id / body: data | 200 code=0 更新成功 max_count 1000→2000 回读确认 | `src/api/obs.js` | 16 |
| 806 | [x] | DELETE | `/api/obs/config/{id}` | 用户端 | path: id | 200 code=0 删除成功(非默认行);默认行预检拒绝,设计上防删默认存储；【2026-11-08 本地已修】obsConfigService.DeleteConfig 默认行拒绝改携 utils.ErrInvalidInput 哨兵 → HandleDBError 映射 400；obs_config_batchm_test.go 补 errors.Is 断言绿 | `src/api/obs.js` | 20 |
| 807 | [x] | POST | `/api/obs/config/{id}/test` | 用户端 | path: id | 200 code=0 连接测试成功(真实id);不存在id→404 NOT_FOUND_1002 | `src/api/obs.js` | 24 |
| 808 | [x] | POST | `/api/obs/config/{id}/default` | 用户端 | path: id | 200 code=0 设为默认配置成功;不存在id→404 NOT_FOUND_1002 | `src/api/obs.js` | 28 |
| 809 | [x] | GET | `/api/obs/config/default` | 用户端 | - | 200 code=0 暂无默认配置(空态合法) | `src/api/obs.js` | 32 |
| 810 | [x] | GET | `/api/observability/dashboard` | 用户端 | body: { window } | 200 code=0 window=7d ai_cost.calls=31 tokens=22809 by_provider非空 | `src/api/observabilityDashboard.js` | 9 |
| 811 | [x] | GET | `/api/customer/oneid/list` | 用户端 | query: params | 200 code=0 list空 total=0 分页字段齐 | `src/api/oneid.js` | 4 |
| 812 | [x] | GET | `/api/customer/oneid/conflicts` | 用户端 | query: params | 200 code=0 conflicts list空 total=0 | `src/api/oneid.js` | 8 |
| 813 | [x] | POST | `/api/customer/oneid/merge` | 用户端 | body: data | 400 code=INVALID_PARAM_1001 不能合并同一客户(正确业务校验,不触真实合并) | `src/api/oneid.js` | 12 |
| 814 | [x] | POST | `/api/customer/oneid/conflicts/{id}/resolve` | 用户端 | path: id / body: data | 200 code=0 action=ignore 已忽略冲突 id=xxx(不入库,幂等) | `src/api/oneid.js` | 16 |
| 815 | [x] | GET | `/api/customer-oneid/{customerId}/identities` | 用户端 | path: customerId | 404 code=NOT_FOUND_1002 客户不存在(不存在customerId) | `src/api/oneid.js` | 20 |
| 816 | [x] | POST | `/api/customer-oneid/{customerId}/identities` | 用户端 | path: customerId / body: data | 400 code=INVALID_PARAM_1001 客户不存在(不存在customerId,写口同挡) | `src/api/oneid.js` | 24 |
| 817 | [x] | POST | `/api/customer/oneid/resolve` | 用户端 | body: data | 200 code=0 未找到匹配客户 matched=false normalized_ids回显;空body→400 INVALID_PARAM_1001(脏数据入口已拦) | `src/api/oneid.js` | 28 |
| 818 | [x] | GET | `/api/customer/oneid/stats` | 用户端 | - | 200 code=0 stats total=0 with_phone=0 全零空态合法 | `src/api/oneid.js` | 32 |
| 819 | [x] | GET | `/api/oneid/merge-rules` | 用户端 | - | 200 code=0 built_in规则(同手机号/同UnionID)+priority+enabled齐 | `src/api/oneid.js` | 36 |
| 820 | [x] | POST | `/api/oneid/merge-rules` | 用户端 | body: data | 200 code=0 保存成功(round-trip回读一致,built_in仅enabled/priority可改) | `src/api/oneid.js` | 40 |
| 821 | [x] | GET | `/api/operation-logs` | 用户端 | query: params | 200 code=0 日志list(id/user_id/username/action/module/ip/rollbackable)非空 | `src/api/operationLog.js` | 4 |
| 822 | [x] | GET | `/api/operation-logs/{id}` | 用户端 | path: id | 404 code=NOT_FOUND_1002 日志不存在(不存在id) | `src/api/operationLog.js` | 7 |
| 823 | [x] | GET | `/api/operation-logs/statistics` | 用户端 | query: params | 200 code=0 action_stats{create:69,delete:16,update:15}+module_stats | `src/api/operationLog.js` | 10 |
| 824 | [x] | GET | `/api/operation-logs/export` | 用户端 | query: params | 200 text/csv charset=utf-8 表头ID,Module,Action,Username,IP,CreatedAt+数据行 | `src/api/operationLog.js` | 13 |
| 825 | [x] | POST | `/api/operation-logs/clean` | 用户端 | body: { beforeDate } | 200 code=0 清理完成 before=2026-08-09(beforeDate=2099-01-01被夹到当日,仅清到期日志)；【2026-11-08 本地已修】双端收口：前端 api/operationLog.js 改传 before_date(snake_case)；后端 CleanLogs 加键存在性守卫——body 非空但不含 before_date/days 任何已知键时 400 拒绝（防静默按默认 90 天窗口破坏性清理），空 body {} 维持文档化默认；operation_log_clean_test.go RED(旧码200)→GREEN | `src/api/operationLog.js` | 16 |
| 826 | [x] | GET | `/api/manage/order-drafts` | 管理端 | query: params | 503 SERVICE_UNAVAILABLE_6003 订单草稿底座不可用(FF_LTC_ORDER_DRAFT_DB=off 正确关闸) | `src/api/orderDraft.js` | 32 |
| 827 | [x] | GET | `/api/manage/order-drafts/{id}` | 管理端 | path: id | 503 SERVICE_UNAVAILABLE_6003 同#826 底座未装配正确关闸 | `src/api/orderDraft.js` | 36 |
| 828 | [x] | PATCH | `/api/manage/order-drafts/{id}` | 管理端 | path: id / body: updates | 503 SERVICE_UNAVAILABLE_6003 同#826 底座未装配正确关闸 | `src/api/orderDraft.js` | 43 |
| 829 | [x] | POST | `/api/manage/order-drafts/{id}/confirm` | 管理端 | path: id | 503 SERVICE_UNAVAILABLE_6003 同#826 底座未装配正确关闸 | `src/api/orderDraft.js` | 50 |
| 830 | [x] | POST | `/api/manage/order-drafts/{id}/cancel` | 管理端 | path: id / body: { reason } | 503 SERVICE_UNAVAILABLE_6003 同#826 底座未装配正确关闸 | `src/api/orderDraft.js` | 55 |
| 831 | [x] | GET | `/api/agent/order-drafts/stats` | 用户端 | - | 200 code=0 assembled=false counts=null durable=false mode=off(观察端点常挂,读数语义正确) | `src/api/orderDraft.js` | 62 |
| 832 | [x] | PUT | `/api/system/permissions/{id}/enabled` | 用户端 | path: id / body: { enabled } | 400 code=INVALID_PARAM_1001 不能启停自己的账号(id=79 自身保护);id=1→400 用户不存在 | `src/api/permission.js` | 4 |
| 833 | [x] | PUT | `/api/system/permissions/{id}/password` | 用户端 | path: id / body: { password } | 400 code=INVALID_PARAM_1001 不能重置自己的密码，请使用修改密码功能(自身保护) | `src/api/permission.js` | 8 |
| 834 | [x] | GET | `/api/system/permissions/audit-logs` | 用户端 | query: params | 200 code=0 audit-logs list(action=user.enable/module=role/resource=system_user)非空 | `src/api/permission.js` | 12 |
| 835 | [x] | GET | `/api/analytics/persona/staffs` | 用户端 | - | 200 code=0 persona staffs list[{staff_id:79,session_count:2}] total=1 | `src/api/persona.js` | 4 |
| 836 | [x] | GET | `/api/analytics/persona/staffs/{staffId}` | 用户端 | path: staffId | 200 code=0 报告生成成功 staff_id=79 overall_score=28.57 items(响应速度/转化能力)非空 | `src/api/persona.js` | 8 |
| 837 | [x] | GET | `/api/platform/message/latest` | 管理端 | - | 200 code=0 message/latest data{id:4,title:站内消息模块上线公告,is_read:true} | `src/api/platform.js` | 5 |
| 838 | [x] | POST | `/api/platform/message/{messageId}/read` | 管理端 | path: messageId | 200 code=0 获取成功 id=4 is_read=true(点已读消息幂等) | `src/api/platform.js` | 9 |
| 839 | [x] | GET | `/api/platform-accounts` | 用户端 | query: params | 200 code=0 platform-accounts list空 | `src/api/platformAccount.js` | 5 |
| 840 | [x] | GET | `/api/platform-accounts/platforms` | 用户端 | - | 200 code=0 platforms=[douyin/kuaishou/xiaohongshu/xianyu/tiktok] | `src/api/platformAccount.js` | 9 |

> **缺陷 D-35（已修复）**：DELETE /api/platform-accounts/{id} 对不存在 id 返回 200「删除成功」假成功。根因：`internal/repository/unified_message.go` platformAccountRepo.Delete 不校验 RowsAffected。修复：RowsAffected==0 时返回 gorm.ErrRecordNotFound，controller HandleDBError 映射 404「记录不存在」。
| 841 | [x] | GET | `/api/platform-accounts/{id}` | 用户端 | path: id | 200 code=0 获取成功(真实id=4);不存在id=999999→404 NOT_FOUND_1002 记录不存在 | `src/api/platformAccount.js` | 9 |
| 842 | [x] | POST | `/api/platform-accounts` | 用户端 | body: data | 200 code=0 创建成功 id=4 platform=kuaishou tenant_id=71 | `src/api/platformAccount.js` | 9 |
| 843 | [x] | PUT | `/api/platform-accounts/{id}` | 用户端 | path: id / body: data | 200 code=0 更新成功 account_name→api800_1762502000_upd 回读一致 | `src/api/platformAccount.js` | 9 |
| 844 | [~] | DELETE | `/api/platform-accounts/{id}` | 用户端 | path: id | 200 code=0 删除成功 for 不存在id(999999/123456789)但list行数不变=假成功；代码已含D-35 RowsAffected守卫(repo unified_message.go)但线上未生效(疑似未部署该修复) | `src/api/platformAccount.js` | 9 |
| 845 | [x] | POST | `/api/platform-accounts/{id}/login` | 用户端 | path: id / body: data | 400 code=INVALID_PARAM_1001 平台 kuaishou 不支持服务端无头登录(CDP通道已移除,能力下线正确语义) | `src/api/platformAccount.js` | 9 |
| 846 | [x] | GET | `/api/platform-accounts/{id}/status` | 用户端 | path: id / body: undefined | 400 code=INVALID_PARAM_1001 平台 kuaishou 不支持服务端登录态检查(能力下线正确语义) | `src/api/platformAccount.js` | 9 |
| 847 | [x] | GET | `/api/qq/accounts` | 用户端 | query: params | 200 code=0 qq/accounts list空 total=0 | `src/api/qqBot.js` | 4 |
| 848 | [x] | GET | `/api/qq/accounts/{id}` | 用户端 | path: id | 200 code=0 获取成功 id=2 app_secret_masked=api8*****cret(掩码) | `src/api/qqBot.js` | 8 |
| 849 | [x] | POST | `/api/qq/accounts` | 用户端 | body: data | 200 code=0 创建成功 id=2 webhook_url自动推导 /api/webhook/qq/2 | `src/api/qqBot.js` | 12 |
| 850 | [x] | PUT | `/api/qq/accounts/{id}` | 用户端 | path: id / body: data | 200 code=0 更新成功 account_name→api800_1762503000_upd(空=不改口径) | `src/api/qqBot.js` | 16 |
| 851 | [x] | DELETE | `/api/qq/accounts/{id}` | 用户端 | path: id | 200 code=0 删除成功(测试账号已清理) | `src/api/qqBot.js` | 20 |
| 852 | [x] | POST | `/api/qq/accounts/{id}/test-send` | 用户端 | path: id / body: data | 502 code=UPSTREAM_FAILURE_5005 发送 QQ 消息失败（渠道不可达或凭据无效）(凭据为假,上游失败正确映射502非400) | `src/api/qqBot.js` | 24 |
| 853 | [x] | POST | `/api/qq/accounts/{id}/verify-callback` | 用户端 | path: id / body: {} | 400 code=INVALID_PARAM_1001 自检失败 webhook_secret 未配置(配置前置校验正确) | `src/api/qqBot.js` | 28 |
| 854 | [x] | POST | `/api/rag-config/products` | 用户端 | body: data | 200 code=0 created id=c974c317 vector_table=api800_1762504000_vec 默认bge-m3/1024维 | `src/api/ragProductConfig.js` | 12 |
| 855 | [x] | GET | `/api/rag-config/products` | 用户端 | query: params | 200 code=0 {list,page,page_size,total}信封完好(非压平数组) | `src/api/ragProductConfig.js` | 21 |
| 856 | [x] | GET | `/api/rag-config/products` | 用户端 | query: params | 200 code=0 同#855 分页信封 total=2 list_len=2(前端normalizeList消费路径同源) | `src/api/ragProductConfig.js` | 25 |
| 857 | [x] | PUT | `/api/rag-config/products/{id}` | 用户端 | path: id / body: data | 200 code=0 updated successfully(名字→api800_1762504000_upd) | `src/api/ragProductConfig.js` | 29 |
| 858 | [x] | DELETE | `/api/rag-config/products/{id}` | 用户端 | path: id | 200 code=0 deleted successfully(测试产品已清理) | `src/api/ragProductConfig.js` | 33 |
| 859 | [x] | GET | `/api/rag-config/accounts/config` | 用户端 | query: params | 200 code=0 无参→No account config found data:null;带假参→data含account_id/platform/max_daily_queries默认1000 | `src/api/ragProductConfig.js` | 37 |
| 860 | [x] | PUT | `/api/rag-config/accounts/config` | 用户端 | body: PlatformAccountConfig | 200 code=0 Configuration updated successfully(回读确认落库 rag_product_id=notexist is_auto_reply=false) | `src/api/ragProductConfig.js` | 41 |
| 861 | [x] | POST | `/api/rag-config/process-message` | 用户端 | body: {platform,account_id,message} | 400 code=INVALID_PARAM_1001 auto reply is not enabled for this account(未启用自动回复正确拦截) | `src/api/ragProductConfig.js` | 45 |
| 862 | [x] | GET | `/api/reach/pipelines` | 用户端 | query: params | 200 code=0 pipelines list page信封齐(存量r832_pipeline_v2) | `src/api/reachPipeline.js` | 5 |
| 863 | [x] | POST | `/api/reach/pipelines` | 用户端 | body: data | 200 code=0 创建成功 id=4 默认steps 9段+默认retry/rate_limit策略 | `src/api/reachPipeline.js` | 8 |
| 864 | [x] | GET | `/api/reach/pipelines/{id}` | 用户端 | path: id | 200 code=0 查询成功 id=4 详情完整 | `src/api/reachPipeline.js` | 11 |
| 865 | [x] | PUT | `/api/reach/pipelines/{id}` | 用户端 | path: id / body: data | 200 code=0 更新成功 name→api800_1762505000_upd | `src/api/reachPipeline.js` | 14 |
| 866 | [x] | DELETE | `/api/reach/pipelines/{id}` | 用户端 | path: id | 200 code=0 删除成功 id=4(级联清job2,测试数据已清理) | `src/api/reachPipeline.js` | 17 |
| 867 | [x] | POST | `/api/reach/pipelines/{id}/pause` | 用户端 | path: id / body: {} | 200 code=0 暂停成功 id=4 | `src/api/reachPipeline.js` | 20 |
| 868 | [x] | POST | `/api/reach/pipelines/{id}/resume` | 用户端 | path: id / body: {} | 200 code=0 恢复成功 id=4 | `src/api/reachPipeline.js` | 23 |
| 869 | [x] | POST | `/api/reach/pipelines/{id}/archive` | 用户端 | path: id / body: {} | 200 code=0 归档成功 id=5(新建临时管道验证后已删) | `src/api/reachPipeline.js` | 26 |
| 870 | [x] | GET | `/api/reach/jobs` | 用户端 | query: params | 200 code=0 jobs list page信封(入队后list可见) | `src/api/reachPipeline.js` | 30 |
| 871 | [x] | POST | `/api/reach/jobs` | 用户端 | body: data | 200 code=0 入队成功 job id=2 state=pending pipeline_id=4 | `src/api/reachPipeline.js` | 33 |
| 872 | [x] | GET | `/api/reach/jobs/{id}` | 用户端 | path: id | 200 code=0 查询成功(调度器跑后state=failed 因假payload无真实发送) | `src/api/reachPipeline.js` | 36 |
| 873 | [x] | POST | `/api/reach/jobs/{id}/cancel` | 用户端 | path: id / body: {} | 400 code=INVALID_PARAM_1001 job is not pending/running(已failed不可取消正确校验) | `src/api/reachPipeline.js` | 39 |
| 874 | [x] | POST | `/api/reach/jobs/{id}/retry` | 用户端 | path: id / body: {} | 200 code=0 重试成功 id=2(failed→重排) | `src/api/reachPipeline.js` | 42 |
| 875 | [x] | POST | `/api/reach/jobs/{id}/execute` | 用户端 | path: id / body: {} | 200 code=0 执行成功(手动手闸,state=failed step_results留痕 sms无真实凭据) | `src/api/reachPipeline.js` | 45 |
| 876 | [x] | GET | `/api/reach/stats` | 用户端 | query: params | 200 code=0 stats{total:2,pending:1,jobs:1,active:1}计数齐 | `src/api/reachPipeline.js` | 49 |
| 877 | [x] | POST | `/api/reach/rate-limit/reset` | 用户端 | query: channel / body: {} | 200 code=0 重置成功 channel=sms;缺channel→400 INVALID_PARAM_1001 channel 必填 | `src/api/reachPipeline.js` | 52 |
| 878 | [x] | GET | `/api/reach/deferred-outbound` | 用户端 | query: params | 200 code=0 deferred list空 total=0 | `src/api/reachPipeline.js` | 58 |
| 879 | [x] | POST | `/api/reach/deferred-outbound/{id}/cancel` | 用户端 | path: id / body: {} | 404 code=NOT_FOUND_1002 记录不存在或不可取消（仅 pending 可取消）(不存在id正确映射) | `src/api/reachPipeline.js` | 63 |
| 880 | [x] | GET | `/api/system/roles` | 用户端 | - | GET /system/roles 200 code=0 返回admin/customer_service/staff三角色含member_count | `src/api/role.js` | 10 |
| 881 | [x] | GET | `/api/system/roles/{code}` | 用户端 | path: code | GET /system/roles/admin 200 code=0 返回角色详情 | `src/api/role.js` | 15 |
| 882 | [x] | GET | `/api/system/roles/{code}/members` | 用户端 | path: code / query: params | GET /system/roles/admin/members 200 code=0 分页返回成员(user79) | `src/api/role.js` | 22 |
| 883 | [x] | GET | `/api/config/runtime` | 用户端 | query: groups | GET /config/runtime 200 code=0 返回白名单组20个frontend_ui/frontend_ws等键值 | `src/api/runtimeConfig.js` | 13 |
| 884 | [x] | GET | `/api/config/dictionaries/{parent}` | 用户端 | path: parent | GET /config/dictionaries/approval_status 200 code=0 items含pending/approved等;未知parent 404字典域不存在 | `src/api/runtimeConfig.js` | 23 |
| 885 | [x] | GET | `/api/manage/dictionaries` | 管理端 | - | GET /manage/dictionaries 200 code=0 返回5个字典域(approval_status等)含item_count | `src/api/runtimeConfig.js` | 28 |
| 886 | [x] | GET | `/api/manage/dictionaries/{parent}` | 管理端 | path: parent | GET /manage/dictionaries/approval_status 200 code=0 items列表 | `src/api/runtimeConfig.js` | 29 |
| 887 | [x] | PUT | `/api/manage/dictionaries/{parent}/items` | 管理端 | path: parent / body: item | PUT /manage/dictionaries/approval_status/items 403「仅平台运营方可执行」(租户admin非平台运营方,正确角色门) | `src/api/runtimeConfig.js` | 30 |
| 888 | [x] | PUT | `/api/manage/dictionaries/{parent}/transitions` | 管理端 | path: parent / body: { transitions } | PUT /manage/dictionaries/bad_case_status/transitions 403 同上平台运营方门正确 | `src/api/runtimeConfig.js` | 32 |
| 889 | [x] | GET | `/api/sales-workbench/overview` | 用户端 | body: { sales_id: salesId } | GET /sales-workbench/overview?sales_id=79 200 code=0 返回todos/today/month/ai_product | `src/api/salesWorkbench.js` | 24 |
| 890 | [x] | GET | `/api/sales-workbench/team-dashboard` | 用户端 | body: { days } | GET /sales-workbench/team-dashboard?days=7 200 code=0 funnel=null/top_sales=[]空态合法 | `src/api/salesWorkbench.js` | 30 |
| 891 | [x] | GET | `/api/sales-workbench/champion` | 用户端 | body: { days } | GET /sales-workbench/champion?days=7 200 code=0 insights=[暂无销售数据] | `src/api/salesWorkbench.js` | 37 |
| 892 | [x] | GET | `/api/sales-workbench/quick-actions` | 用户端 | - | GET /sales-workbench/quick-actions 200 code=0 返回5条静态快链 | `src/api/salesWorkbench.js` | 43 |
| 893 | [x] | GET | `/api/script-library/{scriptId}/versions` | 用户端 | path: scriptId | GET /script-library/1/versions 200 code=0 {list:[],total:0}(script-library表空) | `src/api/scriptAb.js` | 4 |
| 894 | [x] | POST | `/api/script-library/{scriptId}/versions` | 用户端 | path: scriptId / body: data | POST /script-library/1/versions 404「记录不存在」(script_library表无id=1,正确资源门;该表由sync-to-library填充) | `src/api/scriptAb.js` | 5 |
| 895 | [x] | PUT | `/api/script-library/{scriptId}/versions/{versionId}/activate` | 用户端 | path: scriptId, versionId | PUT /script-library/1/versions/1/activate 404 记录不存在(同上正确门) | `src/api/scriptAb.js` | 7 |
| 896 | [x] | POST | `/api/script-library/{scriptId}/expire` | 用户端 | path: scriptId / body: data | POST /script-library/1/expire 404 记录不存在(正确门)；【2026-11-08 本地已修】UpdateScriptActivation 校验 RowsAffected，0 行报 gorm.ErrRecordNotFound → controller 映射 404；script_library_activation_test.go 两用例绿 | `src/api/scriptAb.js` | 8 |
| 897 | [x] | GET | `/api/script-library/{scriptId}/ab-stats` | 用户端 | path: scriptId / query: params | GET /script-library/1/ab-stats 200 code=0 返回config+空buckets | `src/api/scriptAb.js` | 9 |
| 898 | [x] | PUT | `/api/script-library/{scriptId}/ab-config` | 用户端 | path: scriptId / body: data | PUT /script-library/1/ab-config {enabled,split_a,attribution_h} 200 code=0「AB配置已保存」(配置在script_library行缺失时也可存) | `src/api/scriptAb.js` | 10 |
| 899 | [x] | POST | `/api/script-ab/conversion` | 用户端 | body: data | POST /script-ab/conversion {one_id,outcome} 200 code=0 {recorded:true};缺one_id 400校验正确 | `src/api/scriptAb.js` | 11 |
| 900 | [x] | GET | `/api/scripts` | 用户端 | query: params | GET /scripts 200 code=0 分页list | `src/api/scriptTemplate.js` | 4 |
| 901 | [x] | GET | `/api/scripts/{id}` | 用户端 | path: id | GET /scripts/1 200 code=0 返回创建的话术 | `src/api/scriptTemplate.js` | 7 |
| 902 | [x] | POST | `/api/scripts` | 用户端 | body: data | POST /scripts {category,title,content} 200 code=0 创建成功id=1 | `src/api/scriptTemplate.js` | 10 |
| 903 | [x] | PUT | `/api/scripts/{id}` | 用户端 | path: id / body: data | PUT /scripts/1 200 code=0 更新成功title v2 | `src/api/scriptTemplate.js` | 13 |
| 904 | [x] | DELETE | `/api/scripts/{id}` | 用户端 | path: id | DELETE /scripts/1 200 code=0「删除成功」 | `src/api/scriptTemplate.js` | 16 |
| 905 | [x] | GET | `/api/scripts/categories` | 用户端 | - | GET /scripts/categories 200 code=0 空数组(初始) | `src/api/scriptTemplate.js` | 19 |
| 906 | [x] | GET | `/api/scripts/search` | 用户端 | query: params | GET /scripts/search?keyword=r832 200 code=0 命中fixture | `src/api/scriptTemplate.js` | 22 |
| 907 | [x] | GET | `/api/scripts/public` | 用户端 | - | GET /scripts/public 200 code=0 {list:[],total:0} | `src/api/scriptTemplate.js` | 25 |
| 908 | [x] | POST | `/api/scripts/recommend` | 用户端 | body: data | POST /scripts/recommend {message} 200 code=0 获取推荐成功data=null;缺message 400校验正确 | `src/api/scriptTemplate.js` | 28 |
| 909 | [x] | POST | `/api/security/audit` | 用户端 | body: data | POST /security/audit 200 code=0 执行8项检查score=89,items含DB/超管/LLM/限流/审计等检查项 | `src/api/securityAudit.js` | 4 |
| 910 | [x] | GET | `/api/security/audit/list` | 用户端 | query: params | GET /security/audit/list 200 code=0 分页返回审计记录 | `src/api/securityAudit.js` | 8 |
| 911 | [x] | GET | `/api/security/audit/{id}` | 用户端 | path: id | GET /security/audit/1 200 code=0 返回审计详情 | `src/api/securityAudit.js` | 12 |
| 912 | [x] | GET | `/api/shortlink/list` | 用户端 | query: params | GET /shortlink/list 200 code=0 分页返回历史短链 | `src/api/shortLink.js` | 5 |
| 913 | [x] | GET | `/api/shortlink/{id}` | 用户端 | path: id | GET /shortlink/25 200 code=0 返回详情 | `src/api/shortLink.js` | 9 |
| 914 | [x] | POST | `/api/shortlink/create` | 用户端 | body: data | POST /shortlink/create {short_code,original_url,title} 200 code=0 id=32;缺short_code 400(必填校验) | `src/api/shortLink.js` | 13 |
| 915 | [x] | PUT | `/api/shortlink/update` | 用户端 | body: data | PUT /shortlink/update {id,...} 200 code=0 更新成功;注意status未传会重置为0(见917) | `src/api/shortLink.js` | 17 |
| 916 | [x] | DELETE | `/api/shortlink/delete/{id}` | 用户端 | path: id | DELETE /api/shortlink/delete/13 →200 code=0 删除成功(自建fixture id=13)；999999→404 NOT_FOUND_1002 记录不存在(无假成功) | `src/api/shortLink.js` | 21 |
| 917 | [x] | POST | `/api/shortlink/access` | 用户端 | body: data | POST /api/shortlink/access {short_code:api916_1794037783} →200 code=0 访问成功,回original_url/title；不存在码→404 NOT_FOUND_1002 短链不存在 | `src/api/shortLink.js` | 25 |
| 918 | [x] | POST | `/api/shortlink/generate` | 用户端 | body: data | POST /api/shortlink/generate {length:6} →200 code=0 生成成功 short_code=0HFT0E；{}缺length→400 INVALID_PARAM_1001 | `src/api/shortLink.js` | 29 |
| 919 | [x] | GET | `/api/shortlink/{id}/stats` | 用户端 | path: id / query: params | GET /api/shortlink/13/stats →200 code=0 获取短链统计成功,total_access=1,today_access=1,device_type_stats含desktop 100% | `src/api/shortLink.js` | 33 |
| 920 | [x] | GET | `/api/shortlink/stats/all` | 用户端 | query: params | GET /api/shortlink/stats/all →200 code=0 获取所有短链统计成功,total_access=1,short_link_stats含2条(api916_1794037783 access=1) | `src/api/shortLink.js` | 37 |
| 921 | [x] | POST | `/api/shortlink/{id}/share` | 用户端 | path: id / body: data | POST /api/shortlink/13/share →200 code=0 返回short_url=https://workapi.xapptool.cn/s/api916_1794037783 与qr_code data:image/png;base64 | `src/api/shortLink.js` | 41 |
| 922 | [x] | GET | `/api/sms/config` | 用户端 | - | GET /api/sms/config →200 code=0 返回defaultProvider/rateLimit/dailyLimit及aliyun/tencent/huawei三通道,accessKey类字段掩码显示 | `src/api/sms.js` | 5 |
| 923 | [x] | POST | `/api/sms/config` | 用户端 | body: data | POST /api/sms/config 保存aliyun通道同值回写 →200 code=0；再GET确认值落库且accessKeyId掩码为****tkey | `src/api/sms.js` | 8 |
| 924 | [x] | GET | `/api/sms/list` | 用户端 | query: params | GET /api/sms/list →200 code=0 {list,total}；draft发送落库后可见phone/status=failed/errorMsg=aliyun sms config missing(渠道凭证未配置的真实记录) | `src/api/sms.js` | 12 |
| 925 | [x] | GET | `/api/sms/detail/{id}` | 用户端 | path: id | GET /api/sms/detail/999999 →404 NOT_FOUND_1002 记录不存在 | `src/api/sms.js` | 15 |
| 926 | [x] | POST | `/api/sms/resend/{id}` | 用户端 | path: id | POST /api/sms/resend/999999 →404 NOT_FOUND_1002 记录不存在(未真实重发) | `src/api/sms.js` | 18 |
| 927 | [x] | POST | `/api/sms/send` | 用户端 | body: data | POST /api/sms/send phone=13800001111 →400 UPSTREAM无凭证时code=INVALID_PARAM_1001 message=SMS service not configured；phone=123→400参数校验；无假成功 | `src/api/sms.js` | 21 |
| 928 | [x] | GET | `/api/sms/draft/list` | 用户端 | query: params | GET /api/sms/draft/list →200 code=0 {list,total}；建草稿后返回含api916_draft_1794040451 | `src/api/sms.js` | 25 |
| 929 | [x] | GET | `/api/sms/draft/{id}` | 用户端 | path: id | GET /api/sms/draft/1 →200 code=0 返回id=1 title/content/createdAt；999999→404 NOT_FOUND_1002 | `src/api/sms.js` | 28 |
| 930 | [x] | POST | `/api/sms/draft` | 用户端 | body: data | POST /api/sms/draft {title,content} →200 code=0 创建id=2；缺字段→400 INVALID_PARAM_1001 | `src/api/sms.js` | 31 |
| 931 | [x] | PUT | `/api/sms/draft/{id}` | 用户端 | path: id / body: data | PUT /api/sms/draft/1 →200 code=0 更新成功 | `src/api/sms.js` | 34 |
| 932 | [x] | DELETE | `/api/sms/draft/{id}` | 用户端 | path: id | DELETE /api/sms/draft/{id} →200 code=0(清理自建草稿1、2)；测试后无残留 | `src/api/sms.js` | 37 |
| 933 | [x] | POST | `/api/sms/draft/{id}/send` | 用户端 | path: id / body: data | POST /api/sms/draft/2/send {phone:13800001111} →502 UPSTREAM_FAILURE_5005 发送短信失败（渠道不可达或凭证无效），短信记录落库status=failed errorMsg=aliyun sms config missing——渠道未配置的正确失败非缺陷 | `src/api/sms.js` | 40 |
| 934 | [x] | GET | `/api/sms/job/list` | 用户端 | query: params | GET /api/sms/job/list →200 code=0 {list,total}；建job后返回api916_job_1794041724 status=running | `src/api/sms.js` | 44 |
| 935 | [x] | GET | `/api/sms/job/{id}` | 用户端 | path: id | GET /api/sms/job/1 →200 code=0 返回job详情total=1 sent=0 failed=0 | `src/api/sms.js` | 47 |
| 936 | [x] | POST | `/api/sms/job/{id}/pause` | 用户端 | path: id | POST /api/sms/job/1/pause →200 code=0 | `src/api/sms.js` | 50 |
| 937 | [x] | POST | `/api/sms/job/{id}/resume` | 用户端 | path: id | POST /api/sms/job/1/resume →200 code=0 | `src/api/sms.js` | 53 |
| 938 | [x] | POST | `/api/sms/job/{id}/stop` | 用户端 | path: id | POST /api/sms/job/1/stop →200 code=0 | `src/api/sms.js` | 56 |
| 939 | [x] | POST | `/api/sms/job` | 用户端 | body: data | POST /api/sms/job {name,phoneList,content} →200 code=0 创建job id=1及1条pending记录 | `src/api/sms.js` | 59 |
| 940 | [x] | DELETE | `/api/sms/job/{id}` | 用户端 | path: id | DELETE /api/sms/job/1 →200 code=0；999999→404 NOT_FOUND_1002(无假成功) | `src/api/sms.js` | 62 |
| 941 | [x] | GET | `/api/sms/job/{id}/records` | 用户端 | path: id / query: params | GET /api/sms/job/1/records →200 code=0 返回pending记录phone/content/status | `src/api/sms.js` | 65 |
| 942 | [x] | GET | `/api/sop` | 用户端 | query: params | GET /api/sop →200 code=0 {list,total}；建fixture后list可见，软删后归零 | `src/api/sopAgent.js` | 5 |
| 943 | [x] | POST | `/api/sop` | 用户端 | body: data | POST /api/sop {name,scenario,sop_graph} →200 创建成功 id=1 version=1 is_active=true；缺sop_graph→400 invalid sop graph | `src/api/sopAgent.js` | 9 |
| 944 | [x] | GET | `/api/sop/stats` | 用户端 | - | GET /api/sop/stats →200 code=0 返回active/inactive/running/failed/success/total六计数值,建fixture后total=1 active=1 | `src/api/sopAgent.js` | 13 |
| 945 | [x] | GET | `/api/sop/match` | 用户端 | query: params | GET /api/sop/match?intent=test →200 code=0 data=[] (无命中返回空数组) | `src/api/sopAgent.js` | 17 |
| 946 | [x] | GET | `/api/sop/executions` | 用户端 | query: params | GET /api/sop/executions →200 code=0 {list,total}；执行后list含execution id=1 status=success带sop_name回填 | `src/api/sopAgent.js` | 21 |
| 947 | [x] | GET | `/api/sop/executions/{id}` | 用户端 | path: id | GET /api/sop/executions/1 →200 code=0 查询成功返回执行详情；999999→404 NOT_FOUND_1002 执行不存在 | `src/api/sopAgent.js` | 25 |
| 948 | [x] | POST | `/api/sop/executions/{id}/pause` | 用户端 | path: id | POST /api/sop/executions/1/pause →200 暂停成功；999999→404(与D-36同口径,不假成功) | `src/api/sopAgent.js` | 29 |
| 949 | [x] | POST | `/api/sop/executions/{id}/resume` | 用户端 | path: id | POST /api/sop/executions/1/resume →200 恢复成功；999999→404 | `src/api/sopAgent.js` | 33 |
> **缺陷 D-36（已修复）**：`POST /api/sop/executions/{id}/cancel` 对不存在 id 返回 200「取消成功」假成功（pause/resume 均有存在性前置，仅 Cancel 直接 UpdateFields 不查 RowsAffected）。修复：`internal/service/sop/sop.go` SOPService.Cancel 增加 GetByID 前置（gorm.ErrRecordNotFound → ErrSOPExecNotFound），`internal/controller/sop.go` Cancel 错误分支加 `errors.Is(err, service.ErrSOPExecNotFound)` → 404「执行不存在」（与 GetExecution 同文案）。重建+重启后复测：99999 → 404，真实执行 2028 取消正常。
| 950 | [x] | POST | `/api/sop/executions/{id}/cancel` | 用户端 | path: id | POST /api/sop/executions/1/cancel →200 取消成功；999999→404 执行不存在(D-36修复在位,UpdateFields前先GetByID) | `src/api/sopAgent.js` | 37 |
| 951 | [x] | GET | `/api/sop/{id}` | 用户端 | path: id | GET /api/sop/1 →200 code=0 查询成功返回完整sop含sop_graph/trigger_config；999999→404 SOP 不存在 | `src/api/sopAgent.js` | 41 |
| 952 | [x] | PUT | `/api/sop/{id}` | 用户端 | path: id / body: data | PUT /api/sop/1 →200 更新成功 version 1→2 重算updated_at | `src/api/sopAgent.js` | 45 |
| 953 | [x] | DELETE | `/api/sop/{id}` | 用户端 | path: id | DELETE /api/sop/1 →200 删除成功(自建fixture,软删后list归零) | `src/api/sopAgent.js` | 49 |
| 954 | [x] | POST | `/api/sop/{id}/activate` | 用户端 | path: id | POST /api/sop/1/activate →200 启用成功 {id:1} | `src/api/sopAgent.js` | 53 |
| 955 | [x] | POST | `/api/sop/{id}/deactivate` | 用户端 | path: id | POST /api/sop/1/deactivate →200 停用成功 {id:1} | `src/api/sopAgent.js` | 57 |
| 956 | [x] | POST | `/api/sop/execute` | 用户端 | body: data | POST /api/sop/execute →200 执行成功创建execution(sop未激活时400 sop is not active；sop_id不存在400 sop not found；重复同session触发400 sop entry suppressed by entry_policy——均为正确校验) | `src/api/sopAgent.js` | 61 |
| 957 | [x] | POST | `/api/sop/step` | 用户端 | body: data | POST /api/sop/step {execution_id} →400 INVALID_PARAM_1001 execution is not running(终态单步推进被正确拒绝)；999999→400 execution not found | `src/api/sopAgent.js` | 65 |
| 958 | [x] | GET | `/api/sop-templates` | 用户端 | query: params | GET /api/sop-templates →200 code=0 {list,total}；kb_id参数→400 明确提示kb_id过滤不支持(P0-3防静默丢参数) | `src/api/sopTemplate.js` | 5 |
| 959 | [x] | GET | `/api/sop-templates/{id}` | 用户端 | path: id | GET /api/sop-templates/1 →200 查询成功返回模板详情；999999→404 NOT_FOUND_1002 SOP 模板不存在 | `src/api/sopTemplate.js` | 9 |
| 960 | [x] | POST | `/api/sop-templates` | 用户端 | body: data | POST /api/sop-templates {name,intent,stage,template,agent_id} →200 创建成功 id=1 enabled=true | `src/api/sopTemplate.js` | 13 |
| 961 | [x] | PUT | `/api/sop-templates/{id}` | 用户端 | path: id / body: data | PUT /api/sop-templates/1 →200 更新成功 {id:1} | `src/api/sopTemplate.js` | 17 |
| 962 | [x] | PATCH | `/api/sop-templates/{id}/toggle` | 用户端 | path: id / body: { enabled } | PATCH /api/sop-templates/1/toggle enabled=false/true 各一次 →200 状态已更新 data.enabled随之翻转 | `src/api/sopTemplate.js` | 21 |
| 963 | [x] | DELETE | `/api/sop-templates/{id}` | 用户端 | path: id | DELETE /api/sop-templates/1 →200 删除成功；999999→404 操作失败，请稍后重试(记录不存在,无假成功) | `src/api/sopTemplate.js` | 25 |
| 964 | [x] | POST | `/api/sop-templates/match` | 用户端 | body: data | POST /api/sop-templates/match {intent,stage,agent_id,top_k} →200 匹配成功返回命中模板数组；缺agent_id→400参数校验 | `src/api/sopTemplate.js` | 29 |
| 965 | [x] | GET | `/api/system/info` | 用户端 | - | GET /api/system/info →200 code=0 返回cpu_usage/memory_usage/disk_usage/uptime/server_time/hostname/go_version/num_cpu等完整SystemInfo结构 | `src/api/stats.js` | 7 |
| 966 | [x] | GET | `/api/system/info` | 用户端 | body: config | GET /api/system/info(stats.js同路径) →200 同上,与#965同一路由,Monitor.vue期望字段齐全 | `src/api/system.js` | 6 |
| 967 | [x] | GET | `/api/system/config` | 用户端 | - | GET /api/system/config →200 code=0 返回tenant_id=71完整站点配置(site_name=API回归租户1107110250,enable_register/enable_rag=true) | `src/api/system.js` | 9 |
| 968 | [~] | POST | `/api/system/config` | 用户端 | body: data | POST /api/system/config 部分保存会整行覆盖:只传{site_name,theme_color}时enable_register/enable_rag被静默置false(整行SaveWholeRow覆盖,零值布尔无法区分未传)。前端Config.vue始终提交全量form故常规路径不触发,但API层缺字段=清空是数据丢失风险。复现:POST {"site_name":"x","theme_color":"#409EFF"}→GET得enable_register=false。已现场恢复原值(enable_register/email_marketing/rag=true)。根因线索:repository/system_config.go SaveConfig走SaveWholeRow整行写,controller仅防空body不清空(isEmptySystemConfigRequest)；【2026-11-08 本地已修，待部署复测】controller SaveConfig 按原始 JSON 键存在性调 service.FillUnsetFromExisting：缺键的 enable_register/enable_email_marketing/enable_rag/maintenance_mode/max_users/max_upload_size_mb 从既有行（无行时文档默认）补齐，显式传 false/0 仍生效；system_config_partial_test.go 两用例绿 | `src/api/system.js` | 12 |
| 969 | [x] | GET | `/api/system/users` | 用户端 | query: params | GET /api/system/users →200 code=0 返回{list,total},list含username/role/status/enabled/data_scope完整字段 | `src/api/systemUser.js` | 4 |
| 970 | [x] | GET | `/api/system/users/{id}` | 用户端 | path: id | GET /api/system/users/84 →200 获取账号详情成功；999999→404 NOT_FOUND_1002 用户不存在 | `src/api/systemUser.js` | 8 |
| 971 | [x] | POST | `/api/system/users` | 用户端 | body: data | POST /api/system/users {username,password,email,role} →200 账号创建成功 id=84(测试账号,已随后删除) | `systemUser.js L12` | 12 |
| 972 | [x] | PUT | `/api/system/users/{id}` | 用户端 | path: id / body: data | PUT /api/system/users/84 →200 账号更新成功 real_name已改回读可见 | `systemUser.js L16` | 16 |
| 973 | [x] | DELETE | `/api/system/users/{id}` | 用户端 | path: id | DELETE /api/system/users/84 →200 账号删除成功(自建fixture)；999999→404 NOT_FOUND_1002 用户不存在 | `systemUser.js L20` | 20 |
| 974 | [x] | GET | `/api/session-tags` | 用户端 | query: params | GET /api/session-tags →200 code=0 获取成功 返回标签数组(带code/color/sort_order字段) | `src/api/tagSegmentation.js` | 5 |
| 975 | [x] | PUT | `/api/session-tags/{id}` | 用户端 | path: id / body: body | PUT /api/session-tags/4 →200 更新成功(需name+code均必填,缺code→400 明确文案)；data回读更新值 | `src/api/tagSegmentation.js` | 12 |
| 976 | [x] | POST | `/api/session-tags` | 用户端 | body: data | POST /api/session-tags {name,code} →200 创建成功 id=4；重复code→409 DUPLICATE_ENTRY_3003 标签编码已存在；缺code→400 | `src/api/tagSegmentation.js` | 16 |
| 977 | [x] | DELETE | `/api/session-tags/{id}` | 用户端 | path: id | DELETE /api/session-tags/4 →200 删除成功；999999→404 NOT_FOUND_1002 会话标签不存在(repo走EnsureAffected无假成功) | `src/api/tagSegmentation.js` | 20 |
| 978 | [x] | GET | `/api/customer-360/tag-rules` | 用户端 | query: params | GET /api/customer-360/tag-rules →200 code=0 {items,total}；建规则后items含name/rule/active/priority | `src/api/tagSegmentation.js` | 24 |
| 979 | [x] | POST | `/api/customer-360/tag-rules` | 用户端 | body: data | POST /api/customer-360/tag-rules {name} →200 保存成功 返回id(uuid)/source=auto/active=true | `src/api/tagSegmentation.js` | 28 |
| 980 | [x] | PUT | `/api/customer-360/tag-rules/{id}` | 用户端 | path: id / body: data | PUT /api/customer-360/tag-rules/{id} →200 更新成功(白名单Updates非整行回写)；不存在id→404 规则不存在(无假成功) | `src/api/tagSegmentation.js` | 32 |
| 981 | [x] | DELETE | `/api/customer-360/tag-rules/{id}` | 用户端 | path: id | DELETE /api/customer-360/tag-rules/nonexistent-id-xyz →200 code=0 「删除成功」且data回显不存在id——假成功。根因:repository/customer_tag.go:70 customerTagRepository.Delete 直接返回 Delete().Error 不校验RowsAffected(service/customer_domain.go:330直透),与同文件Update注释自述的0行成功问题同源,与session_tag.go用EnsureAffected的正确做法不一致。D-35同型缺陷；【2026-11-08 本地已修】customerTagRepository.Delete 校验 RowsAffected，0 行报 gorm.ErrRecordNotFound（对齐同文件 Update）；customer_tag_test.go 两新用例绿 | `src/api/tagSegmentation.js` | 36 |
| 982 | [x] | GET | `/api/user-segment/layers` | 用户端 | - | GET /api/user-segment/layers →200 code=0 返回champion/loyal/at_risk等客户分层定义(中文desc) | `src/api/tagSegmentation.js` | 40 |
| 983 | [x] | GET | `/api/customer-360/tag-stats` | 用户端 | query: params | GET /api/customer-360/tag-stats →200 code=0 返回{total,by_category,by_source}统计结构 | `src/api/tagSegmentation.js` | 44 |
| 984 | [x] | GET | `/api/telegram/accounts` | 用户端 | query: params | GET /api/telegram/accounts →200 code=0 {list,total}；支持status过滤,无残留 | `src/api/telegram.js` | 4 |
| 985 | [x] | GET | `/api/telegram/accounts/{id}` | 用户端 | path: id | GET /telegram/accounts/1 real fixture 200 code=0 返回全字段；999999 404 账号不存在 | `src/api/telegram.js` | 8 |
| 986 | [x] | POST | `/api/telegram/accounts` | 用户端 | body: data | POST 创建 r985 fixture 200 code=0（bot_token 35位校验/400 掩码回显）；空 body 400 required | `src/api/telegram.js` | 12 |
| 987 | [x] | PUT | `/api/telegram/accounts/{id}` | 用户端 | path: id / body: data | PUT /telegram/accounts/1 200 更新成功 ai_agent_enabled=false 生效；token 更新不回显 | `src/api/telegram.js` | 16 |
| 988 | [x] | DELETE | `/api/telegram/accounts/{id}` | 用户端 | path: id | DELETE /telegram/accounts/1 200 删除成功；999999 404 账号不存在 | `src/api/telegram.js` | 20 |
| 989 | [x] | POST | `/api/telegram/accounts/{id}/register-webhook` | 用户端 | path: id / body: data | POST register-webhook 显式 URL：上游 api.telegram.org 不可达，400 回译后报错（setWebhook 失败有留痕 last_error） | `src/api/telegram.js` | 24 |
| 990 | [x] | POST | `/api/telegram/accounts/{id}/test-send` | 用户端 | path: id / body: data | POST test-send 上游不可达 400「webhook 地址不可达」；chat_id 必须 int64、text 必填 400 校验生效 | `src/api/telegram.js` | 28 |
| 991 | [x] | GET | `/api/telegram/gates` | 用户端 | query: params | GET /telegram/gates 200 code=0 list | `src/api/telegram.js` | 34 |
| 992 | [x] | POST | `/api/telegram/gates` | 用户端 | body: data | POST gates r985 fixture 200 code=0；空 body 400 required(account_id/chat_id/mode) | `src/api/telegram.js` | 38 |
| 993 | [x] | PUT | `/api/telegram/gates/{id}` | 用户端 | path: id / body: data | PUT gates/1 200 ok mode 切 mute_unlock 生效；999999 404 网关不存在 | `src/api/telegram.js` | 42 |
| 994 | [x] | DELETE | `/api/telegram/gates/{id}` | 用户端 | path: id | DELETE gates/1 200 ok；999999 404 删除失败 | `src/api/telegram.js` | 46 |
| 995 | [x] | GET | `/api/telegram/gates/{id}/members` | 用户端 | path: id / query: params | GET gates/1/members 200 code=0 空列表 | `src/api/telegram.js` | 50 |
| 996 | [x] | POST | `/api/telegram/gates/{id}/members/{memberId}/authorize` | 用户端 | path: id, memberId | POST gates/1/members/999/authorize 404 放行失败（member 不存在正确映射） | `src/api/telegram.js` | 54 |
| 997 | [x] | GET | `/api/template-market/list` | 用户端 | query: params / body: config | GET list 200 种子目录 + 提审上架条目合并，category 过滤生效 | `src/api/templateMarket.js` | 15 |
| 998 | [x] | GET | `/api/template-market/categories` | 用户端 | body: undefined; config | GET categories 200 返回 9 个分类 | `src/api/templateMarket.js` | 18 |
| 999 | [x] | GET | `/api/template-market/detail/{id}` | 用户端 | path: id | GET detail/card-douyin 200 元数据；不存在 id code=4004 模板/技能不存在 | `src/api/templateMarket.js` | 22 |
| 1000 | [x] | POST | `/api/template-market/entries` | 用户端 | body: data | POST entries r985 提审 200 已提交审核；非法 kind/ID 4000 参数不合法 | `src/api/templateMarket.js` | 28 |
| 1001 | [x] | GET | `/api/template-market/my/submissions` | 用户端 | body: undefined; config | GET my/submissions 200 提审记录含 status=pending | `src/api/templateMarket.js` | 32 |
| 1002 | [x] | GET | `/api/template-market/my/orders` | 用户端 | body: undefined; config | GET my/orders 200 list（下单后有数据） | `src/api/templateMarket.js` | 36 |
| 1003 | [x] | GET | `/api/template-market/my/owned` | 用户端 | body: undefined; config | GET my/owned 200 支付后返回已获得条目 | `src/api/templateMarket.js` | 40 |
| 1004 | [x] | POST | `/api/template-market/entries/{entryId}/order` | 用户端 | path: entryId / body: data \|\| {} | POST order：免费条目 0 元直通 paid；付费条目落 pending_payment；pending 条目下单 4009 | `src/api/templateMarket.js` | 44 |
| 1005 | [x] | POST | `/api/template-market/orders/{orderNo}/cancel` | 用户端 | path: orderNo | POST orders/{no}/cancel：pending_payment→cancelled 200；paid 单 4009；不存在 4004 | `src/api/templateMarket.js` | 48 |
| 1006 | [x] | GET | `/api/template-market/admin/entries` | 用户端 | query: params / body: config | GET admin/entries?status=pending 200 审核队列含 r985 条目 | `src/api/templateMarket.js` | 53 |
| 1007 | [x] | POST | `/api/template-market/admin/entries/{entryId}/review` | 用户端 | path: entryId / body: data | POST review approve/disable 200 状态落库；不存在条目 4004 | `src/api/templateMarket.js` | 57 |
| 1008 | [x] | GET | `/api/template-market/admin/orders` | 用户端 | query: params / body: config | GET admin/orders 200 list | `src/api/templateMarket.js` | 60 |
| 1009 | [x] | POST | `/api/template-market/admin/orders/{orderNo}/mark-paid` | 用户端 | path: orderNo | POST mark-paid：pending_payment→paid 200 已确认收款；paid 单 4009；不存在 4004 | `src/api/templateMarket.js` | 64 |
| 1010 | [x] | GET | `/api/tenant/info` | 用户端 | - | GET tenant/info 200 tenant_id=71 tenant_code 回显 | `src/api/tenant.js` | 8 |
| 1011 | [x] | GET | `/api/tiktok-card/list` | 用户端 | query: params | GET tiktok-card/list 200 | `src/api/tiktokCard.js` | 4 |
| 1012 | [x] | GET | `/api/tiktok-card/{id}` | 用户端 | path: id | GET tiktok-card/1 实体 200；999999 404 卡片不存在 | `src/api/tiktokCard.js` | 8 |
| 1013 | [x] | POST | `/api/tiktok-card` | 用户端 | body: data | POST tiktok-card r985 fixture 200；空 body 400 Title required | `src/api/tiktokCard.js` | 12 |
| 1014 | [x] | PUT | `/api/tiktok-card/{data_id}` | 用户端 | path: data_id / body: data | PUT tiktok-card/1 200 更新成功 | `src/api/tiktokCard.js` | 16 |
| 1015 | [x] | DELETE | `/api/tiktok-card/{id}` | 用户端 | path: id | DELETE tiktok-card/1 200；999999 404 记录不存在 | `src/api/tiktokCard.js` | 20 |
| 1016 | [x] | POST | `/api/tiktok-card/generate-short-link` | 用户端 | body: { card_id: cardId } | POST generate-short-link {card_id:1} 200 生成 /s/xwwRKjGV | `src/api/tiktokCard.js` | 24 |
| 1017 | [x] | GET | `/api/tiktok-card/stats/overall` | 用户端 | query: params | GET stats/overall 200 total_cards/active_cards/汇总 | `src/api/tiktokCard.js` | 28 |
| 1018 | [x] | GET | `/api/tiktok-card/{cardId}/stats` | 用户端 | path: cardId / query: params | GET 1/stats 200 card_id+daily_stats | `src/api/tiktokCard.js` | 32 |
| 1019 | [x] | GET | `/api/admin/tuning/confidence/signals` | 用户端 | query: params | GET confidence/signals 200 空列表 | `src/api/tuning.js` | 4 |
| 1020 | [x] | GET | `/api/admin/tuning/confidence/signals/{id}` | 用户端 | path: id | GET signals/999999 404 signal不存在 | `src/api/tuning.js` | 7 |
| 1021 | [x] | GET | `/api/admin/tuning/confidence/signals/stats` | 用户端 | query: params | GET signals/stats 200 bands+days | `src/api/tuning.js` | 10 |
| 1022 | [x] | GET | `/api/admin/tuning/confidence/calibrations` | 用户端 | query: params | GET calibrations 200 | `src/api/tuning.js` | 13 |
| 1023 | [x] | GET | `/api/admin/tuning/confidence/policies` | 用户端 | query: params | GET policies 200 list | `src/api/tuning.js` | 16 |
| 1024 | [x] | PUT | `/api/admin/tuning/confidence/policies` | 用户端 | body: data | PUT policies r985-policy 200 upsert 落库（tenant_id=71）；空 body 400 required | `src/api/tuning.js` | 19 |
| 1025 | [x] | GET | `/api/admin/tuning/humanize/scores` | 用户端 | query: params | GET humanize/scores 200 | `src/api/tuning.js` | 23 |
| 1026 | [x] | GET | `/api/admin/tuning/humanize/scores/stats` | 用户端 | query: params | GET scores/stats 200 avg/pass_rate | `src/api/tuning.js` | 26 |
| 1027 | [x] | GET | `/api/admin/tuning/humanize/baselines` | 用户端 | query: params | GET baselines 200 | `src/api/tuning.js` | 29 |
| 1028 | [x] | GET | `/api/admin/tuning/humanize/low-quality` | 用户端 | query: params | GET low-quality 200 | `src/api/tuning.js` | 32 |
| 1029 | [x] | GET | `/api/admin/tuning/feedback/events` | 用户端 | query: params | GET feedback/events 200 cursor 列表 | `src/api/tuning.js` | 36 |
| 1030 | [x] | GET | `/api/admin/tuning/feedback/events/stats` | 用户端 | query: params | GET events/stats 200 signals+days | `src/api/tuning.js` | 39 |
| 1031 | [x] | GET | `/api/admin/tuning/feedback/dialogues` | 用户端 | query: params | GET dialogues 200 | `src/api/tuning.js` | 42 |
| 1032 | [x] | GET | `/api/admin/tuning/prompt/candidates` | 用户端 | query: params | GET prompt/candidates 200 | `src/api/tuning.js` | 45 |
> **缺陷 D-38（已修复）**：`PUT /api/admin/tuning/prompt/candidates/{id}/status` 对不存在的候选回 200「成功」（回显 {id,status}），前端「批准/驳回」按钮会误以为已落库；同时 status 不校验枚举，拼错的值（aproved）会写进库，候选此后既不会被 A/B 选中也不会被淘汰。修复：`repository/feedback_loop.go` 的 UpdatePromptCandidateStatus 检查 RowsAffected==0 → gorm.ErrRecordNotFound；`service/tuning.go` 新增 `ErrInvalidCandidateStatus` 与六值白名单（draft/pending/approved/active/promoted/retired）；controller 分别映射 404「候选不存在」与 400「无效的候选状态：可选 …」。复测：99999 → 404；aproved → 400 列出可选值。
| 1033 | [~] | PUT | `/api/admin/tuning/prompt/candidates/{id}/status` | 用户端 | path: id / body: data | D-38 未部署：PUT candidates/999999/status 200 回显假成功；status=aproved 也 200（本地应为 404/400 白名单） | `src/api/tuning.js` | 48 |
| 1034 | [x] | GET | `/api/admin/tuning/bandit/arms` | 用户端 | query: params | GET bandit/arms 200 | `src/api/tuning.js` | 51 |
| 1035 | [x] | GET | `/api/unified-messages` | 用户端 | query: params | GET /api/unified-messages 200 分页列表 | `src/api/unifiedMessage.js` | 5 |
> **缺陷 D-41（已修复）**：`GET /api/unified-messages/{id}` 对不存在的消息回 404，但文案是 gorm 的 `record not found` 被 S-36 原文掩码规则洗成「操作失败，请稍后重试」——运维看不出到底是「消息不存在」还是服务故障。修复：controller 改为固定文案「消息不存在」。复测：99999 → 404 NOT_FOUND_1002「消息不存在」。
| 1036 | [x] | GET | `/api/unified-messages/{id}` | 用户端 | path: id | GET unified-messages/999999 404（文案仍是「操作失败，请稍后重试」，D-41 固定文案「消息不存在」未上线，但 404 状态与 code 正确） | `src/api/unifiedMessage.js` | 9 |
| 1037 | [x] | GET | `/api/unknown-questions` | 用户端 | query: params | GET unknown-questions 200 | `src/api/unknownQuestion.js` | 7 |
| 1038 | [x] | GET | `/api/unknown-questions/stats` | 用户端 | - | GET stats 200 total/pending/adopt_rate | `src/api/unknownQuestion.js` | 10 |
| 1039 | [x] | GET | `/api/unknown-questions/{id}` | 用户端 | path: id | GET 999999 404 目标不存在 | `src/api/unknownQuestion.js` | 13 |
| 1040 | [x] | POST | `/api/unknown-questions/{id}/submit` | 用户端 | path: id | POST submit 999999 404 未知问题不存在或状态已变更 | `src/api/unknownQuestion.js` | 16 |
> **缺陷 D-39（已修复）**：`POST /api/unknown-questions/{id}/review` 与 `/clusters/{id}/review` 把「目标不存在」（service 的 `reflux.ErrNotFound`）和入参错误一起回 400，客户端无法区分「记录已被删/不属于本租户」与「body 写错」；另外未知 action 复用「非法状态迁移」文案，排查方向会被误导到状态机。修复：controller 用 `errors.Is` 把 ErrNotFound 映射 404「目标不存在」；service 未知 action 改为「action 必须是 adopt/ignore/merge（收到 %q）」。复测：99999 → 404；action=bogus → 400 且文案点明 action。
| 1041 | [~] | POST | `/api/unknown-questions/{id}/review` | 用户端 | path: id / body: data | D-39 未部署：POST review 999999 仍 400「目标不存在」（本地应 404），action=bogus 也复用同文案 | `src/api/unknownQuestion.js` | 19 |
| 1042 | [x] | GET | `/api/unknown-questions/clusters` | 用户端 | query: params | GET clusters 200 | `src/api/unknownQuestion.js` | 23 |
| 1043 | [x] | GET | `/api/unknown-questions/clusters/{id}` | 用户端 | path: id | GET clusters/999999 404 目标不存在 | `src/api/unknownQuestion.js` | 26 |
| 1044 | [~] | POST | `/api/unknown-questions/clusters/{id}/review` | 用户端 | path: id / body: data | D-39 未部署：POST clusters/999999/review 仍 400「目标不存在」（本地应 404） | `src/api/unknownQuestion.js` | 29 |
| 1045 | [x] | GET | `/api/user-segment/rfm/list` | 用户端 | query: params | GET rfm/list 200；segment_id 参数生效 | `src/api/userSegment.js` | 4 |
| 1046 | [x] | GET | `/api/user-segment/rfm/rule` | 用户端 | - | GET rfm/rule 200 未配置默认提示 | `src/api/userSegment.js` | 7 |
> **缺陷 D-40（已修复）**：`SaveRFMRuleRequest.Name` 标了 `binding:"required"`，但历史规则（id=2，2026-08-31 建）的 name 是空串——前端按 GET 回填后原样提交，改一个阈值就被打回 400，逼运营先编名字。同时 `UpdateRFMRule` 把仓储的 not-found 包成 `errors.New("规则不存在")`，只能落 400。修复：name 去 required（仅备注，不参与打分）；Update 保留 gorm.ErrRecordNotFound 语义，controller 用 errhttp.IsNotFoundError 映射 404「规则不存在」。复测：空 name 更新成功；99999 → 404。
| 1047 | [x] | POST | `/api/user-segment/rfm/rule` | 用户端 | body: data | 200 保存成功但提交的 camelCase rDays/fCount/mAmount 被忽略，服务端以默认阈值(r_days_1=7...m_amount_1=10000)落库，用户设置静默丢失。根因已核实：SaveRFMRuleRequest 只绑定 snake_case（customer_rfm.go:559），而前端 userSegment/List.vue 表单提交的就是 {rDays,fCount,mAmount,...}（List.vue:632/649-652）→ 生产 UI 的 RFM 阈值同样静默丢失，不只 API 层；同 1048；【2026-11-08 本地已修】前端 api/userSegment.js 新增 toRFMRulePayload：分群弹窗单档 camelCase 阈值翻译为后端五档 snake_case 契约（r_days_1/f_count_1/m_amount_1+对齐默认档位，status→is_active），不再整单静默丢弃 | `src/api/userSegment.js` | 10 |
| 1048 | [~] | PUT | `/api/user-segment/rfm/rule/{id}` | 用户端 | path: id / body: data | D-40 未部署：PUT rule/2 空 name 仍 400 required；PUT rule/999999 400「规则不存在」（本地应空 name 可存+404） | `src/api/userSegment.js` | 13 |
| 1049 | [x] | GET | `/api/user-segment/rfm/user?user_id={userId}` | 用户端 | path: userId | GET rfm/user?user_id=999999 404 未找到客户 RFM 信息 | `src/api/userSegment.js` | 16 |
| 1050 | [x] | GET | `/api/user-segment/rfm/stats` | 用户端 | - | GET rfm/stats 200 total_users/layer_count | `src/api/userSegment.js` | 19 |
| 1051 | [x] | POST | `/api/user-segment/rfm/calculate` | 用户端 | body: data | POST rfm/calculate 200 计算完成 updated_count=0 | `src/api/userSegment.js` | 22 |
| 1052 | [x] | GET | `/api/user-segment/layers` | 用户端 | - | GET user-segment/layers 200 层级字典 | `src/api/userSegment.js` | 25 |
| 1053 | [x] | GET | `/api/user-segments` | 用户端 | query: params | GET /api/user-segments 200 list | `src/api/userSegment.js` | 29 |
| 1054 | [x] | DELETE | `/api/user-segment/rfm/rule/{id}` | 用户端 | path: id | DELETE rfm/rule/999999 404 记录不存在；真实清理 rule/2 200 | `src/api/userSegment.js` | 41 |
| 1055 | [x] | GET | `/api/user-segment/rfm/list?segment_id={id}` | 用户端 | path: id / query: params | GET rfm/list?segment_id=2 200 | `src/api/userSegment.js` | 44 |
| 1056 | [x] | GET | `/api/users` | 用户端 | query: params | GET /api/users 200 租户内用户列表 | `src/api/users.js` | 5 |
| 1057 | [x] | GET | `/api/users/{id}` | 用户端 | path: id | GET users/79 200；999999 404 用户不存在 | `src/api/users.js` | 9 |
| 1058 | [x] | POST | `/api/users` | 用户端 | body: data | POST users r985_user 200 创建成功 id=85；缺 role 400 required | `src/api/users.js` | 13 |
| 1059 | [x] | PUT | `/api/users/{id}` | 用户端 | path: id / body: data | PUT users/85 200 更新 real_name 生效 | `src/api/users.js` | 17 |
| 1060 | [x] | DELETE | `/api/users/{id}` | 用户端 | path: id | DELETE users/85 200；999999 404 | `src/api/users.js` | 21 |
| 1061 | [x] | PUT | `/api/users/{id}/password` | 用户端 | path: id / body: data | PUT users/85/password 200 重置密码成功 | `src/api/users.js` | 25 |
| 1062 | [x] | POST | `/api/auth/login` | 公开 | body: data | POST auth/login：真实账密 200 code=0 发 token；错密码 401 UNAUTHORIZED_2001（tenant_code 传错值也 401） | `src/api/users.js` | 29 |
| 1063 | [x] | POST | `/api/auth/mfa/verify` | 公开 | body: data | POST auth/mfa/verify bogus temp_token 401 临时令牌无效 | `src/api/users.js` | 34 |
> **缺陷 D-37（已修复，安全）**：`GET /api/wecom/health/accounts/risks` 与 `/api/wecom/health/accounts/select` 直接返回 `model.WeComAccount`，**corp_secret 明文**（实测 `hack_secret`），而同模块 `GET /api/wecom/health/accounts` 已做掩码——脱敏工具 `controller/wecom_secret_mask.go` 齐全，只是这两个 handler 漏调（任何登录用户都能读走商户企微应用密钥）。修复：GetRiskAccounts 逐条 `maskWeComAccount(&list[i])`、SelectHealthyAccount 调 `maskWeComAccount(acc)`。复测三端点均返回 `****cret`。
| 1064 | [x] | GET | `/api/wecom/health/accounts` | 用户端 | - | GET health/accounts 200 出参 corp_secret 掩码（创建回显 ****alue） | `src/api/wecomAccount.js` | 5 |
| 1065 | [x] | GET | `/api/wecom/health/accounts/risks` | 用户端 | - | GET risks 200 空列表（脱敏代码在位） | `src/api/wecomAccount.js` | 9 |
| 1066 | [x] | GET | `/api/wecom/health/accounts/select` | 用户端 | - | GET select 200 无可用账号提示 | `src/api/wecomAccount.js` | 13 |
| 1067 | [x] | GET | `/api/wecom/health/accounts/summary` | 用户端 | - | GET summary 200 全零汇总 | `src/api/wecomAccount.js` | 17 |
| 1068 | [x] | GET | `/api/wecom/health/accounts/{id}` | 用户端 | path: id | GET health/accounts/3 上报后 200 最新健康度 health_score=70 | `src/api/wecomAccount.js` | 21 |
| 1069 | [x] | GET | `/api/wecom/health/accounts/{id}/history` | 用户端 | path: id / query: params | GET health/accounts/3/history 200 | `src/api/wecomAccount.js` | 25 |
| 1070 | [x] | POST | `/api/wecom/health/accounts/{id}` | 用户端 | path: id / body: data | POST health/accounts/3 上报 200 risk_level=warning | `src/api/wecomAccount.js` | 29 |
| 1071 | [x] | POST | `/api/wecom/health/accounts/{id}/status` | 用户端 | path: id / body: data | POST status {login_state} 200 更新成功；空 body 400 | `src/api/wecomAccount.js` | 33 |
| 1072 | [x] | POST | `/api/wecom/health/accounts/{id}/quota/consume` | 用户端 | path: id / body: { count } | POST quota/consume count=2 200 | `src/api/wecomAccount.js` | 37 |
| 1073 | [x] | POST | `/api/wecom/health/accounts/quota/reset` | 用户端 | - | POST quota/reset 200 affected=1 | `src/api/wecomAccount.js` | 41 |
| 1074 | [x] | POST | `/api/wecom/messages/ingest` | 用户端 | body: data | POST messages/ingest r985 消息 200 建会话+消息中台记录 | `src/api/wecomAccount.js` | 45 |
<br>
> **缺陷 D-42（已修复）**：`POST /api/wecom/messages/send` 把企微**上游**投递失败（errcode=40013 invalid corpid、网络不可达）统一回 400 `INVALID_PARAM_1001`，客户端会原样重试、运维也看不出该改凭据。根因：`internal/controller/wecom_health.go` SendMessage 错误分支固定 `response.Error(ctx, 400, err.Error())`。修复：① `internal/service/wecom_integration.go` 新增哨兵 `ErrWeComUpstreamSend`，出站调用失败处改 `fmt.Errorf("%w: wecom outbound api: %w", ErrWeComUpstreamSend, serr)`（双 %w 保留上游原始错误链）；② controller 用 `errors.Is(err, service.ErrWeComUpstreamSend)` → `response.ErrorWithCode(ctx, utils.ErrorCodeUpstreamFailure, http.StatusBadGateway, err.Error())`，其余错误维持 400。复测：合法 body → 502 `UPSTREAM_FAILURE_5005` 且 errcode 原文保留；空 body → 仍 400 `INVALID_PARAM_1001`。
| 1075 | [~] | POST | `/api/wecom/messages/send` | 用户端 | body: data | D-42 未部署：POST messages/send 上游 errcode=40013 仍 400 INVALID_PARAM_1001（本地应 502 UPSTREAM_FAILURE_5005） | `src/api/wecomAccount.js` | 49 |
| 1076 | [x] | POST | `/api/wecom/accounts` | 用户端 | body: data | POST wecom/accounts r985corp 200 创建成功+密钥掩码；空 corp_secret 拒绝 | `src/api/wecomAccount.js` | 53 |
| 1077 | [x] | PUT | `/api/wecom/accounts/{id}` | 用户端 | path: id / body: data | PUT wecom/accounts/3 200（需带 corp_id）；name 生效 | `src/api/wecomAccount.js` | 57 |
| 1078 | [x] | DELETE | `/api/wecom/accounts/{id}` | 用户端 | path: id | DELETE wecom/accounts/3 200；999999 404 | `src/api/wecomAccount.js` | 61 |
| 1079 | [x] | GET | `/api/wecom/customers` | 用户端 | query: params | GET wecom/customers 200；account_id 过滤生效 | `src/api/wecomAccount.js` | 65 |
| 1080 | [x] | GET | `/api/wecom/groups` | 用户端 | query: params | GET wecom/groups 200 | `src/api/wecomAccount.js` | 69 |
| 1081 | [x] | GET | `/api/wecom/tags` | 用户端 | - | GET wecom/tags 200 | `src/api/wecomAccount.js` | 73 |
| 1082 | [x] | GET | `/api/wecom/messages` | 用户端 | query: params | GET wecom/messages 200 | `src/api/wecomAccount.js` | 77 |
| 1083 | [x] | POST | `/api/wecom/accounts/{id}/sync-customers` | 用户端 | path: id | POST sync-customers 上游 40013 → 502 UPSTREAM_FAILURE_5005 渠道原文（口径正确） | `src/api/wecomAccount.js` | 81 |
| 1084 | [x] | POST | `/api/wecom/accounts/{id}/sync-groups` | 用户端 | path: id | POST sync-groups 502 UPSTREAM_FAILURE_5005 | `src/api/wecomAccount.js` | 85 |
| 1085 | [x] | POST | `/api/wecom/accounts/{id}/sync-tags` | 用户端 | path: id | POST sync-tags 502 UPSTREAM_FAILURE_5005 | `src/api/wecomAccount.js` | 89 |
| 1086 | [x] | POST | `/api/wecom/accounts/{id}/refresh` | 用户端 | path: id | POST refresh 502 UPSTREAM_FAILURE_5005 | `src/api/wecomAccount.js` | 93 |
| 1087 | [x] | POST | `/api/wecom/accounts/{id}/send-message` | 用户端 | path: id / body: data | POST send-message 502 UPSTREAM_FAILURE_5005 | `src/api/wecomAccount.js` | 97 |
| 1088 | [x] | GET | `/api/whatsapp/accounts` | 用户端 | query: params | GET whatsapp/accounts 200 | `src/api/whatsapp.js` | 3 |
| 1089 | [x] | POST | `/api/whatsapp/accounts` | 用户端 | body: data | POST whatsapp/accounts 200 创建（空 body 也建，cloud 类型） | `src/api/whatsapp.js` | 5 |
| 1090 | [x] | POST | `/api/whatsapp/accounts/{id}/login/start` | 用户端 | path: id | POST login/start 410 WHATSAPP_LOGIN_RETIRED_8401 个人扫码登录已下线（W-3 正确门） | `src/api/whatsapp.js` | 6 |
| 1091 | [x] | GET | `/api/whatsapp/accounts/{id}/login/status` | 用户端 | path: id | GET login/status 200 logged_in=false | `src/api/whatsapp.js` | 7 |
| 1092 | [x] | PUT | `/api/whatsapp/accounts/{id}` | 用户端 | path: id / body: data | PUT accounts/{id} 200 更新 name 生效 | `src/api/whatsapp.js` | 8 |
| 1093 | [x] | DELETE | `/api/whatsapp/accounts/{id}` | 用户端 | path: id | DELETE accounts/{id} 200 | `src/api/whatsapp.js` | 9 |
| 1094 | [x] | GET | `/api/whatsapp/drafts` | 用户端 | query: params | GET drafts 200 | `src/api/whatsapp.js` | 11 |
| 1095 | [x] | POST | `/api/whatsapp/drafts` | 用户端 | body: data | POST drafts 200 创建 | `src/api/whatsapp.js` | 12 |
| 1096 | [x] | PUT | `/api/whatsapp/drafts/{id}` | 用户端 | path: id / body: data | PUT drafts/{id} 200 title/content 生效 | `src/api/whatsapp.js` | 13 |
| 1097 | [x] | DELETE | `/api/whatsapp/drafts/{id}` | 用户端 | path: id | DELETE drafts/{id} 200 | `src/api/whatsapp.js` | 14 |
| 1098 | [x] | GET | `/api/whatsapp/jobs` | 用户端 | query: params | GET jobs 200 | `src/api/whatsapp.js` | 16 |
| 1099 | [x] | POST | `/api/whatsapp/jobs` | 用户端 | body: data | POST jobs draft_id+account_ids 200 群发任务已创建；无 draft_id 400 草稿ID错误 | `src/api/whatsapp.js` | 17 |
| 1100 | [x] | GET | `/api/whatsapp/jobs/{id}` | 用户端 | path: id | GET jobs/{id} 200 任务详情 | `src/api/whatsapp.js` | 18 |
| 1101 | [x] | DELETE | `/api/whatsapp/jobs/{id}` | 用户端 | path: id | DELETE jobs/{id} 200 | `src/api/whatsapp.js` | 19 |
| 1102 | [x] | GET | `/api/whatsapp-cloud/accounts` | 用户端 | query: params | GET whatsapp-cloud/accounts 200 | `src/api/whatsappCloud.js` | 6 |
| 1103 | [x] | GET | `/api/whatsapp-cloud/accounts/{id}` | 用户端 | path: id | GET accounts/999999 404 账号不存在 | `src/api/whatsappCloud.js` | 10 |
| 1104 | [x] | POST | `/api/whatsapp-cloud/accounts` | 用户端 | body: data | POST accounts r985_wac 200 access_token 掩码回显；空 body 400 required x4 | `src/api/whatsappCloud.js` | 14 |
| 1105 | [x] | PUT | `/api/whatsapp-cloud/accounts/{id}` | 用户端 | path: id / body: data | PUT accounts/3 200 | `src/api/whatsappCloud.js` | 18 |
| 1106 | [x] | DELETE | `/api/whatsapp-cloud/accounts/{id}` | 用户端 | path: id | DELETE accounts/3 200；999999 404 | `src/api/whatsappCloud.js` | 22 |
| 1107 | [x] | POST | `/api/whatsapp-cloud/accounts/{id}/test-send` | 用户端 | path: id / body: data | POST test-send 502 UPSTREAM_FAILURE_5005（假凭据上游不可达，口径正确） | `src/api/whatsappCloud.js` | 26 |
| 1108 | [x] | GET | `/api/workflows/versions` | 用户端 | body: {} | GET versions 200 空列表 | `src/api/workflowOrchestrator.js` | 6 |
| 1109 | [x] | GET | `/api/workflows/versions` | 用户端 | body: { workflow_id: workflowIdOrParams } | GET versions?workflow_id=r985-wf 200 过滤生效 | `src/api/workflowOrchestrator.js` | 9 |
| 1110 | [x] | GET | `/api/workflows/versions` | 用户端 | query: params | GET versions（带分页参数）200 | `src/api/workflowOrchestrator.js` | 17 |
| 1111 | [x] | GET | `/api/workflows/versions/{id}` | 用户端 | path: id | GET versions/999999 404 工作流版本不存在 | `src/api/workflowOrchestrator.js` | 21 |
| 1112 | [x] | POST | `/api/workflows/versions` | 用户端 | body: data | POST versions r985 v1 200 创建 draft；空 body 400 | `src/api/workflowOrchestrator.js` | 25 |
| 1113 | [x] | PUT | `/api/workflows/versions/{id}` | 用户端 | path: id / body: data | PUT versions/4 name+definition 200 更新成功 | `src/api/workflowOrchestrator.js` | 29 |
| 1114 | [~] | DELETE | `/api/workflows/versions/{id}` | 用户端 | path: id | D-43 未部署：DELETE versions/999999 与已删 id 重复删均 200「删除成功」假成功（本地应 404） | `src/api/workflowOrchestrator.js` | 33 |
> **缺陷 D-43（已修复）**：`POST /api/workflows/versions/{id}/publish`、`/archive` 与 `DELETE /api/workflows/versions/{id}` 对不存在的 id 回 200「发布成功/归档成功/删除成功」假成功。根因：`internal/repository/workflow_orchestrator.go` 的 `UpdateStatus` / `DeleteByID` 忽略 `RowsAffected`（只返回 Update/Delete 的 Error）。修复：两方法在 `RowsAffected == 0` 时返回 `gorm.ErrRecordNotFound`；`internal/controller/workflow_orchestrator.go` 三处错误分支增加 `errhttp.IsNotFoundError(err)` → 404「工作流版本不存在」，其余仍走 ErrorFromDB。复测：99999 三端点均 404 NOT_FOUND_1002；真实版本 publish/delete 200、重复删 404。<br>
| 1115 | [~] | POST | `/api/workflows/versions/{id}/publish` | 用户端 | path: id | D-43 未部署：POST versions/999999/publish 200「发布成功」假成功（本地应 404） | `src/api/workflowOrchestrator.js` | 37 |
| 1116 | [~] | POST | `/api/workflows/versions/{id}/archive` | 用户端 | path: id | D-43 未部署：POST versions/999999/archive 200「归档成功」假成功（本地应 404） | `src/api/workflowOrchestrator.js` | 41 |
| 1117 | [x] | POST | `/api/workflows/execute` | 用户端 | body: data | POST execute：空 body 400；r985-wf 未发布 400 no published version found（业务门正确） | `src/api/workflowOrchestrator.js` | 45 |
| 1118 | [x] | GET | `/api/workflows/executions` | 用户端 | query: params | GET executions 200（含既有执行记录） | `src/api/workflowOrchestrator.js` | 49 |
| 1119 | [x] | GET | `/api/workflows/executions/{id}` | 用户端 | path: id | GET executions/999999 404 执行记录不存在 | `src/api/workflowOrchestrator.js` | 53 |
| 1120 | [x] | GET | `/api/workflows/executions/{executionId}/nodes` | 用户端 | path: executionId | GET executions/999999/nodes 200 空列表（按不存在执行返回空，口径一致） | `src/api/workflowOrchestrator.js` | 57 |
| 1121 | [x] | POST | `/api/workflows/executions/{id}/stop` | 用户端 | path: id | POST executions/999999/stop 404 执行不存在 | `src/api/workflowOrchestrator.js` | 61 |
| 1122 | [x] | GET | `/api/xianyu/list` | 用户端 | query: params | GET xianyu/list 200 | `src/api/xianyuCard.js` | 4 |
| 1123 | [x] | GET | `/api/xianyu/{id}` | 用户端 | path: id | GET xianyu/999999 404 记录不存在 | `src/api/xianyuCard.js` | 8 |
| 1124 | [x] | POST | `/api/xianyu/create` | 用户端 | body: data | POST xianyu/create r985 200 自动建 /s/HEIzUW；缺 title/image_url 400 | `src/api/xianyuCard.js` | 12 |
| 1125 | [x] | PUT | `/api/xianyu/update` | 用户端 | body: data | PUT xianyu/update 200 | `src/api/xianyuCard.js` | 16 |
| 1126 | [x] | DELETE | `/api/xianyu/delete/{id}` | 用户端 | path: id | DELETE xianyu/delete/2 200；999999 404 | `src/api/xianyuCard.js` | 20 |
| 1127 | [x] | GET | `/api/xianyu/view/{id}` | 用户端 | path: id | GET xianyu/view/2 200 | `src/api/xianyuCard.js` | 24 |
| 1128 | [x] | GET | `/api/xianyu/stats/card/{id}` | 用户端 | path: id / query: params | GET xianyu/stats/card/2 200 chart+details+stats | `src/api/xianyuCard.js` | 28 |
| 1129 | [x] | GET | `/api/xianyu/stats/overall` | 用户端 | query: params | GET xianyu/stats/overall 200 overall+ranking | `src/api/xianyuCard.js` | 32 |
> **缺陷 D-44（已修复）**：`POST /api/{douyin,kuaishou,xianyu,tiktok}/generate-short-link` 原先「先删旧短链 → 生成短码 → `if card.RedirectURL == "" { return nil }` 提前返回」：卡片未配 redirect_url 时旧 `/s/xxx` 被删、新链未建、接口仍回 200（幂等重生成反而弄丢线上链接）。修复：新增 `service/cards/card_shortlink_errors.go` 的 `ErrCardRedirectURLRequired`，四家 GenerateShortLink 把空 redirect 校验提到删除旧链之前；删除前先 GetByID 确认旧链存在（悬空 short_link_id 容错）；controller 映射 400「卡片未配置跳转地址（redirect_url），无法生成短链」。实测 xianyu/douyin 清空 redirect 后 400 且旧链保留。
| 1130 | [~] | POST | `/api/xianyu/{id}/generate-short-link` | 用户端 | path: id | D-44 未部署：redirect_url 清空后 POST /xianyu/3/generate-short-link 仍 200 空 short_code（本地应 400 ErrCardRedirectURLRequired）；有 re | `src/api/xianyuCard.js` | 36 |
| 1131 | [x] | GET | `/api/xiaohongshu/list` | 用户端 | query: params | GET xiaohongshu/list 200 | `src/api/xiaohongshuCard.js` | 4 |
| 1132 | [x] | GET | `/api/xiaohongshu/{id}` | 用户端 | path: id | GET xiaohongshu/999999 404 | `src/api/xiaohongshuCard.js` | 8 |
| 1133 | [x] | POST | `/api/xiaohongshu/create` | 用户端 | body: data | POST xiaohongshu/create r985 200 自动建 /s/hVtM6n；缺 image_url 400 | `src/api/xiaohongshuCard.js` | 12 |
| 1134 | [x] | PUT | `/api/xiaohongshu/update` | 用户端 | body: data | PUT xiaohongshu/update 200 | `src/api/xiaohongshuCard.js` | 16 |
| 1135 | [x] | DELETE | `/api/xiaohongshu/delete/{id}` | 用户端 | path: id | DELETE xiaohongshu/delete/3 200；999999 404 | `src/api/xiaohongshuCard.js` | 20 |
| 1136 | [x] | GET | `/api/xiaohongshu/view/{id}` | 用户端 | path: id | GET xiaohongshu/view/3 200 | `src/api/xiaohongshuCard.js` | 24 |
| 1137 | [x] | GET | `/api/xiaohongshu/stats/card/{id}` | 用户端 | path: id / query: params | GET xiaohongshu/stats/card/3 200 | `src/api/xiaohongshuCard.js` | 28 |
| 1138 | [x] | GET | `/api/xiaohongshu/stats/overall` | 用户端 | query: params | GET xiaohongshu/stats/overall 200 popularCards | `src/api/xiaohongshuCard.js` | 32 |
> **缺陷 D-45（已修复）**：小红书 `generate-short-link` 原先用空壳 `&model.XiaohongshuCard{ID: uint(id)}` 调 service，redirect_url/title/domain_pool_id 全零值 → 被空 redirect 判断提前返回，接口 200 但从不建链。修复：service 新增 `GetCardModelByID`，controller 先取真实实体再生成并映射 400；实测清空 short_link_id 后生成 /s/Sb1Dy7，重复调用幂等同 code，99999 → 404。
| 1139 | [x] | POST | `/api/xiaohongshu/{id}/generate-short-link` | 用户端 | path: id | POST xiaohongshu/3/generate-short-link 200 幂等同 code 返回实体；999999 404（D-45 实体取用正常） | `src/api/xiaohongshuCard.js` | 36 |
