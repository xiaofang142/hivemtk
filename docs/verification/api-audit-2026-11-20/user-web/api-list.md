# user-web API 清单（2026-11-20 本轮验证）

来源：`hivemtk/user-web/src/api/*`，124 个文件，1159 个可解析调用分支。每个调用点单独记录，含条件 URL 分支、multipart 和 fetch SSE。

历史清单仅作线索；本清单不继承历史勾选。每次读取一条，通过编辑器显式执行 curl、分析响应与业务副作用、必要时修复回归，再编辑该行。禁止脚本批量请求。

参数列保留前端实参，路径中的 `{...}` 为动态参数；具体字段、必填项与限制在该路由验证时由 DTO 和调用页面核实，不能把变量名当完整字段契约。响应预期为 `{code,data,message/msg}`；下载为文件、SSE 为 text/event-stream，以每条实测为准。

模式：独立部署 `TENANT_MODE=off`（8401）与 SaaS `TENANT_MODE=on`（8402）分别记录；SaaS 同时核验租户归属和跨租户访问拒绝。

状态：`未测` / `验证中` / `通过` / `受阻`。只有实际验证且业务行为正确才标记通过；配置关闭仅证明该拒绝分支，不能证明成功路径。认证凭证不写入清单或证据。

| # | 方法 | 地址 | 来源 | 前端入口 | 身份 | 前端附加入参 | 独立部署 | SaaS / 控制面 | 本轮实测响应与证据 |
|---|---|---|---|---|---|---|---|---|---|
| 1 | GET | `/api/ab-experiments` | `abExperiment.js:4` | getExperimentList | 用户端 Cookie / Bearer；权限以后端注册为准 | query page（默认1）、page_size（默认10，上限200，超限钳制） | 通过 | 通过 | 双模式正常200/code=0；未认证401。已修复负数/非数字/超限分页回显不一致；负数回归1/10、201回归200。SaaS A/B各仅见本租户1条。证据：001-*.json；Go控制器包18.635s PASS、vet/build通过；跨租户详情见#2 |
| 2 | GET | `/api/ab-experiments/{id}` | `abExperiment.js:7` | getExperiment | 用户端 Cookie / Bearer；权限以后端注册为准 | path id：uint32 | 通过 | 通过 | 本租户详情双模式200/code=0，返回夹具id=1；SaaS B读取A的id=1→404 NOT_FOUND_1002；非法id→400 INVALID_PARAM_1001；不存在uint32最大值→404。证据：002-*.json |
| 3 | POST | `/api/ab-experiments` | `abExperiment.js:10` | createExperiment | 用户端 Cookie / Bearer；权限以后端注册为准 | body name/source_type/source_id必填；traffic_split、variants[{name,is_control,weight,config}] | 通过 | 通过 | 双模式200/code=0；伪造created_by/tenant_id被忽略，off归属1/创建人1，SaaS B归属2/创建人12；两变体实际落库tenant_id=2；空body→400。证据003-*.json；变体事务失败场景尚未覆盖 |
| 4 | PUT | `/api/ab-experiments/{id}` | `abExperiment.js:13` | updateExperiment | 用户端 Cookie / Bearer；权限以后端注册为准 | path id；body与创建同形，替换variants | 通过 | 通过 | 双模式200/code=0更新name/traffic_split；tenant_id/created_by保留；SaaS A改B→404，数据库确认B内容未变且仅2条新变体，权重40/60归属2。证据004-*.json |
| 5 | DELETE | `/api/ab-experiments/{id}` | `abExperiment.js:16` | deleteExperiment | 用户端 Cookie / Bearer；权限以后端注册为准 | path id：uint32 | 通过 | 通过 | 双模式删除本轮临时记录→200/code=0；SaaS A删B→404；随后B删除成功，数据库父记录与变体均剩0。证据005-*.json |
| 6 | POST | `/api/ab-experiments/{id}/start` | `abExperiment.js:19` | startExperiment | 用户端 Cookie / Bearer；权限以后端注册为准 | path id；无body | 通过 | 通过 | 双模式200/code=0启动成功；SaaS B启动A→404；数据库A=running、B=draft。证据006-*.json |
| 7 | POST | `/api/ab-experiments/{id}/pause` | `abExperiment.js:22` | pauseExperiment | 用户端 Cookie / Bearer；权限以后端注册为准 | path id；无body | 通过 | 通过 | 双模式200/code=0暂停成功；SaaS B暂停A→404；数据库A=paused、B=draft。证据007-*.json |
| 8 | POST | `/api/ab-experiments/{id}/stop` | `abExperiment.js:25` | stopExperiment | 用户端 Cookie / Bearer；权限以后端注册为准 | path id；无body | 通过 | 通过 | 双模式200/code=0；SaaS跨租户404；实际2变体结果均落库，100/10与100/20→10%/20%，优胜者treatment，状态completed。证据008-*.json；结果接口见#9 |
| 9 | GET | `/api/ab-experiments/{id}/results` | `abExperiment.js:28` | getExperimentResults | 用户端 Cookie / Bearer；权限以后端注册为准 | path id；返回ABExperimentResult[] | 通过 | 通过 | 双模式200/code=0，两变体100/10=10%、100/20=20%，winner与存储一致；SaaS B读取A→404。confidence_level沿用现有单样本相对50%统计口径，非两样本优胜概率；高级比较见#12。证据009-*.json |
| 10 | GET | `/api/ab-experiments/{id}/conversion-events` | `abExperiment.js:31` | getConversionEvents | 用户端 Cookie / Bearer；权限以后端注册为准 | path id；query page/page_size，默认1/10，上限200 | 通过 | 通过 | 已修复双模式负分页回显；实时回归1/10，超限201回显200；SaaS A/B各只返回自己的1条事件，跨租户404。控制器包91.280s PASS/build/vet通过。证据010-*.json |
| 11 | GET | `/api/ab-experiments` | `abExperiment.js:41` | getExperimentStats | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参；实际为列表别名 | 通过 | 通过 | 双模式200/code0，默认page1/page_size10/total1；SaaS B只见tenant2。此出口名虽含Stats，实际返回list/total，按前端既有契约验证。证据011-*.json |
| 12 | GET | `/api/ab-experiments/{id}/stats` | `abExperimentPlus.js:6` | getExperimentWithStats | 用户端 Cookie / Bearer；权限以后端注册为准 | path id；query method=frequentist或bayesian | 通过 | 通过 | off frequentist200/code0，100/10对100/20→z1.9803/p0.04767/conf0.95233；on bayesian200，treatment胜率约0.976；跨租户404；未知method400。证据012-*.json |
| 13 | GET | `/api/ab-experiments/{id}/diagnostics` | `abExperimentPlus.js:9` | getExperimentDiagnostics | 用户端 Cookie / Bearer；权限以后端注册为准 | path id；返回SRM/最小样本/多曝光诊断 | 通过 | 通过 | 已修复SRM错误概率：双模式chi2=0的p从0.09896改为1；5个已知分布值及单调性回归先RED后GREEN；服务/控制器全包与build/vet通过，跨租户404。证据013-*-fixed.json |
| 14 | GET | `/api/ab-experiments/{id}/cuped` | `abExperimentPlus.js:12` | getExperimentWithCUPED | 用户端 Cookie / Bearer；权限以后端注册为准 | path id；返回available/reason/theta/variance_reduction/variants | 通过 | 通过 | 双模式200/code0：实验前数据不足（需≥30用户级样本）按设计降级available=false，退化为未调整结果与#12一致；跨租户404。证据014-*.json |
| 15 | POST | `/api/ab-experiments/{id}/sequential-test` | `abExperimentPlus.js:15` | sequentialTest | 用户端 Cookie / Bearer；权限以后端注册为准 | body { alpha }，缺省/越界回落0.05（设计内，防NaN） | 通过 | 通过 | 双模式200：LLR=8.109超上界2.890→accept_H1，与两倍转化率差一致；跨租户404；alpha=1.5按设计回落0.05（controller注释明确防NaN语义）。证据015-*.json |
| 16 | POST | `/api/ab-experiments/{id}/bayesian-test` | `abExperimentPlus.js:18` | bayesianTest | 用户端 Cookie / Bearer；权限以后端注册为准 | body {}（可带样本数等） | 通过 | 通过 | 200：control胜率0.4977/treatment0.9756、expected_loss与#12完全一致（同seed确定性RNG）。证据016-off-bayesian.json；SaaS/跨租户与#12同链路 |
| 17 | POST | `/api/agent-kb-bindings` | `agentKBBinding.js:4` | bind | 用户端 Cookie / Bearer；权限以后端注册为准 | body agent_id/knowledge_base_id必填，priority可选；role无通道（repo写死primary，前端无调用方传role） | 通过 | 通过 | 双模式200绑定成功；缺字段400；伪造跨租户KB→404带KB id文案。已修复跨租户agent写缺口（见#21）：B对A的agent+自己的kb原200幽灵行，修复后404「智能体 12 不存在」。证据017-*.json |
| 18 | DELETE | `/api/agent-kb-bindings/{agentId}/{kbId}` | `agentKBBinding.js:8` | unbind | 用户端 Cookie / Bearer；权限以后端注册为准 | path agentId, kbId | 通过 | 通过 | CheckExists先探测：不存在/跨租户→404（不会假成功）；本轮临时绑定删除成功。证据018-on-cross-unbind.json |
| 19 | GET | `/api/agent-kb-bindings/by-agent/{agentId}` | `agentKBBinding.js:12` | listByAgent | 用户端 Cookie / Bearer；权限以后端注册为准 | path agentId；返回绑定行（含kb_type/role/priority） | 通过 | 通过 | 本租户200返回绑定；跨租户agent→200空数组（列表语义无泄露，与404口径差异已记录为观察项）。证据019-*.json |
| 20 | GET | `/api/agent-kb-bindings/by-kb/{kbId}` | `agentKBBinding.js:16` | listByKB | 用户端 Cookie / Bearer；权限以后端注册为准 | path kbId | 通过 | 通过 | 本租户200返回绑定行；读侧租户过滤良好：A会话读B的kb2→200空数组，无泄露。证据020-on-list-by-kb.json、021-defect-kb2-cross-read.json |
| 21 | PUT | `/api/agent-kb-bindings/by-agent/{agentId}` | `agentKBBinding.js:20` | replaceAgentKBs | 用户端 Cookie / Bearer；权限以后端注册为准 | path agentId / body { kb_ids: 字符串数组 } | 通过 | 通过 | **发现并修复高危缺陷**：原实现只校验KB归属，B可对A的agent播种幽灵绑定（PUT 200+落库tenant=2行）。修复=三个写事务入口加resolveAgentExists闸+BatchBind 404透出；修复后跨租户PUT/POST/批量全404「智能体 12 不存在」，A绑定原样保留，同租户替换200正常。repo回归含新用例3.705s全绿。证据021-*.json |
| 22 | GET | `/api/ai-agents` | `aiAgent.js:4` | listAgents | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size等 | 通过 | 通过 | 双模式200：本租户返回seed agents；SaaS B跨租户→200空列表无泄露。证据022-*.json |
| 23 | GET | `/api/ai-agents-enabled` | `aiAgent.js:8` | listEnabledAgents | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200仅启用agent列表。证据023-on-enabled.json |
| 24 | GET | `/api/ai-agents/{id}` | `aiAgent.js:12` | getAgent | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 本租户200；不存在uint32→404「智能体不存在」。观察项：404响应data带"record not found"原文（ErrorFromDB既有形态，Get同族，未本批改动）。证据024-*.json |
| 25 | GET | `/api/ai-agents/by-code` | `aiAgent.js:16` | findAgentByCode | 用户端 Cookie / Bearer；权限以后端注册为准 | query agent_code | 通过 | 通过 | 本租户200返回agent；跨租户→200 data=null（查无返回nil，无泄露）。证据025-*.json |
| 26 | POST | `/api/ai-agents` | `aiAgent.js:20` | createAgent | 用户端 Cookie / Bearer；权限以后端注册为准 | body agent_code/name/persona必填 | 通过 | 通过 | 双模式200：伪造tenant_id/created_by被忽略，off落tenant1、SaaS B落tenant2；缺persona→400。证据026-*.json |
| 27 | PUT | `/api/ai-agents/{id}` | `aiAgent.js:24` | updateAgent | 用户端 Cookie / Bearer；权限以后端注册为准 | path id；body同create | 通过 | 通过 | 本租户200（伪造tenant_id被忽略）；跨租户改A的agent→404。证据027-*.json |
| 28 | DELETE | `/api/ai-agents/{id}` | `aiAgent.js:28` | deleteAgent | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | **修复响应码语义**：原跨租户/不存在删除→400「删除失败」+data:"record not found"；修复后404「智能体不存在」无原文，与Get同族一致；业务拒绝文案（被N个渠道绑定）保留。证据028-*.json、028-*-fixed.json |
| 29 | POST | `/api/ai-agents/{id}/toggle` | `aiAgent.js:32` | toggleAgent | 用户端 Cookie / Bearer；权限以后端注册为准 | body { status } 0/1，非法值钳制为1 | 通过 | 通过 | off模式0→1循环200，status回显正确。证据029-*.json |
| 30 | POST | `/api/ai-agents/{id}/test` | `aiAgent.js:36` | testAgent | 用户端 Cookie / Bearer；权限以后端注册为准 | body { message }，真实LLM调用 | 通过 | 通过 | 200/code0：路由→调度→回复链路完整；外部llm7免费端点本轮不可达，返回设计内降级文案「AI 服务暂时不可用」+intent unknown——降级路径验证通过，真实LLM成功路径受外部端点可用性限制未复验。证据030-off-test.json |
| 31 | GET | `/api/ai-agents/{id}/context` | `aiAgent.js:40` | getAgentContext | 用户端 Cookie / Bearer；权限以后端注册为准 | path id；返回agent_id/agent_code/persona等上下文 | 通过 | 通过 | 200加载上下文（agent 12）。证据031-off-context.json |
| 32 | GET | `/api/ai-productivity/overview` | `aiProductivity.js:5` | getReport | 用户端 Cookie / Bearer；权限以后端注册为准 | query days 默认30 | 通过 | 通过 | 200/code0聚合33会话225AI回复（本地夹具库真实数据）。证据032-off-overview.json |
| 33 | GET | `/api/ai-productivity/trend` | `aiProductivity.js:8` | getTrend | 用户端 Cookie / Bearer；权限以后端注册为准 | query days 默认30 | 通过 | 通过 | 200/code0：days=7回显7个日期点。证据033-off-trend.json |
| 34 | GET | `/api/ai-tools` | `aiTool.js:4` | listTools | 用户端 Cookie / Bearer；权限以后端注册为准 | query params | 通过 | 通过 | 200/code0：空租户list=[]/total=0（合法空态）。证据034-off-list.json |
| 35 | GET | `/api/ai-tools/{name}` | `aiTool.js:8` | getTool | 用户端 Cookie / Bearer；权限以后端注册为准 | path name | 通过 | 通过 | 未知工具→404「工具不存在」。证据035-off-unknown.json |
| 36 | PUT | `/api/ai-tools/{name}/status` | `aiTool.js:12` | updateToolStatus | 用户端 Cookie / Bearer；权限以后端注册为准 | path name / body { is_enabled } | 通过 | 通过 | 未知工具→404「工具不存在」。证据036-off-unknown-status.json |
| 37 | POST | `/api/ai-tools/batch-status` | `aiTool.js:16` | batchUpdateToolStatus | 用户端 Cookie / Bearer；权限以后端注册为准 | body { tools, is_enabled } | 通过 | 通过 | 空数组→400「工具列表不能为空」。证据037-off-empty-batch.json |
| 38 | GET | `/api/ai-tools/{toolName}/accounts` | `aiTool.js:20` | getToolAccounts | 用户端 Cookie / Bearer；权限以后端注册为准 | path toolName | 通过 | 通过 | 未知工具→200 data=[]（无绑定合法空态）。证据038-off-unknown-accounts.json |
| 39 | POST | `/api/ai-tools/{toolName}/accounts` | `aiTool.js:24` | bindToolAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | path toolName / body { account_type, account_id, is_primary } | 通过 | 通过 | 未知工具→404「工具不存在」。证据039-off-unknown-bind.json |
| 40 | DELETE | `/api/ai-tools/{toolName}/accounts/{accountType}/{accountId}` | `aiTool.js:32` | unbindToolAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | path toolName, accountType, accountId | 通过 | 通过 | 未知工具→404「绑定不存在」。证据040-off-unknown-unbind.json |
| 41 | GET | `/api/approvals/{id}` | `approval.js:19` | get | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 双模式503 SERVICE_UNAVAILABLE_6003「审批底座不可用」——FF_LTC_APPROVAL_RESUME=off设计内关闸，非缺陷。证据041-*.json |
| 42 | POST | `/api/approvals/{id}/decide` | `approval.js:28` | decide | 用户端 Cookie / Bearer；权限以后端注册为准 | body { verdict, note } | 通过 | 通过 | 同#41设计内503关闸（拒绝分支已验；成功路径需开启审批运行时）。证据042-off-decide.json |
| 43 | POST | `/api/asset-bundle` | `assetBundle.js:4` | createBundle | 用户端 Cookie / Bearer；权限以后端注册为准 | body asset_id/title/messages(role=system至少一条)必填 | 通过 | 通过 | 双模式200落各自租户；缺system→400中文文案。证据043-*.json |
| 44 | PUT | `/api/asset-bundle/{id}` | `assetBundle.js:7` | updateBundle | 用户端 Cookie / Bearer；权限以后端注册为准 | path id；body title/version/tags/change_note | 通过 | 通过 | 200更新1.0.0→1.0.1并自动记版本日志（见#51）；响应回读库内真相。证据044-off-update.json |
| 45 | GET | `/api/asset-bundle/{id}` | `assetBundle.js:10` | getBundle | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 本租户200；on库A读B的包→404「资产包不存在（id=10）」隔离正确。证据045-*.json |
| 46 | GET | `/api/asset-bundle/by-aid/{aid}` | `assetBundle.js:13` | getBundleByAssetID | 用户端 Cookie / Bearer；权限以后端注册为准 | path aid（数字主键或slug兼容） | 通过 | 通过 | slug直查200。证据046-off-by-aid.json |
| 47 | POST | `/api/asset-bundle/list` | `assetBundle.js:16` | listBundles | 用户端 Cookie / Bearer；权限以后端注册为准 | body keyword等 | 通过 | 通过 | keyword=audit20命中1条。证据047-off-list.json |
| 48 | POST | `/api/asset-bundle/{id}/publish` | `assetBundle.js:19` | publishBundle | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200 draft→published。证据048-off-publish.json |
| 49 | POST | `/api/asset-bundle/{id}/submit-platform` | `assetBundle.js:22` | submitToPlatform | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 403 FORBIDDEN_2002「平台集成未启用」设计内拒（PLATFORM_ENABLED=false），文案带修复指引。证据049-off-submit.json |
| 50 | POST | `/api/asset-bundle/{id}/platform-status/refresh` | `assetBundle.js:26` | refreshPlatformStatus | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 同#49设计内403。证据050-off-refresh.json |
| 51 | GET | `/api/asset-bundle/{id}/versions?limit={limit}` | `assetBundle.js:30` | getBundleVersions | 用户端 Cookie / Bearer；权限以后端注册为准 | query limit | 通过 | 通过 | 200返回1.0.0→1.0.1版本记录（change_note落库正确）。证据051-off-versions.json |
| 52 | POST | `/api/asset-bundle/{id}/archive` | `assetBundle.js:33` | archiveBundle | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200 archive成功。证据052-off-archive.json |
| 53 | DELETE | `/api/asset-bundle/{id}` | `assetBundle.js:36` | deleteBundle | 用户端 Cookie / Bearer；权限以后端注册为准 | path id（写面收口admin中间件） | 通过 | 通过 | DELETE 200软删成功（首轮405系测试方误发POST，路由本身正常）。证据053-off-delete-fixed.json |
| 54 | POST | `/api/asset-bundle/weave` | `assetBundle.js:39` | weaveBundle | 用户端 Cookie / Bearer；权限以后端注册为准 | body asset_id/user_query必填；merchant_vars等 | 通过 | 通过 | 两层闸验证：归档包→400「未发布」；active未热启用→400「未热启用」；enable后→200完整messages（merchant_vars注入）。证据054-off-weave*.json |
| 55 | POST | `/api/asset-bundle/merchant-save` | `assetBundle.js:42` | merchantSave | 用户端 Cookie / Bearer；权限以后端注册为准 | body asset_id必填；shop_name/qa_cards等 | 通过 | 通过 | 200 upsert生成active包（qa_cards→system+QA消息）。证据055-off-merchant-save.json |
| 56 | POST | `/api/asset-bundle/merchant-parse/{aid}` | `assetBundle.js:45` | merchantParse | 用户端 Cookie / Bearer；权限以后端注册为准 | path aid | 通过 | 通过 | 200回显shop_name/campaign/qa_cards表单。证据056-off-merchant-parse.json |
| 57 | POST | `/api/asset-bundle/{id}/enable` | `assetBundle.js:48` | enableBundle | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200热启用（已删包→404正确）。证据057-off-enable.json、057c-off-enable-merchant.json |
| 58 | POST | `/api/asset-bundle/{id}/disable` | `assetBundle.js:51` | disableBundle | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200热禁用。证据058-off-disable.json |
| 59 | POST | `/api/asset-bundle/enabled/list` | `assetBundle.js:54` | listEnabledBundles | 用户端 Cookie / Bearer；权限以后端注册为准 | 无body | 通过 | 通过 | 200仅热启用包。证据059-off-enabled-list.json |
| 60 | POST | `/api/upload` | `assetBundle.js:59` | uploadCover | 用户端 Cookie / Bearer；权限以后端注册为准 | multipart fd；field=file | 通过 | 通过 | 71字节PNG→200 url=/files/attachments/2026/11/...，extension/mime识别正确。证据060-off-upload.json |
| 61 | GET | `/api/v1/asset-market/list` | `assetMarket.js:4` | listAssets | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size | 通过 | 通过 | 200空态list=[]/total=0。证据061-off-market-list.json |
| 62 | GET | `/api/v1/asset-market/detail/{id}` | `assetMarket.js:7` | assetDetail | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 不存在资产→HTTP200 code=6001「资产不存在或不可用」。证据062-off-market-detail.json |
| 63 | POST | `/api/v1/asset-market/purchase` | `assetMarket.js:10` | purchaseAsset | 用户端 Cookie / Bearer；权限以后端注册为准 | body { asset_id } 等 | 通过 | 通过 | 403 FORBIDDEN_2002 平台集成未启用设计内拒。证据063-off-purchase.json |
| 64 | POST | `/api/v1/asset-market/sync` | `assetMarket.js:13` | syncAsset | 用户端 Cookie / Bearer；权限以后端注册为准 | body {} | 通过 | 通过 | 403设计内拒（旧口径恒5000已演进为403门）。证据064-off-sync.json |
| 65 | GET | `/api/v1/asset-market/credits` | `assetMarket.js:17` | getAssetCredits | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 403设计内拒（平台集成未启用）。证据065-off-credits.json |
| 66 | POST | `/api/v1/asset-market/report-usage` | `assetMarket.js:20` | reportUsage | 用户端 Cookie / Bearer；权限以后端注册为准 | body asset_id等 | 通过 | 通过 | 403设计内拒。证据066-off-report.json |
| 67 | GET | `/api/v1/local-assets` | `assetMarket.js:23` | listLocalAssets | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size | 通过 | 通过 | 200返回seed资产列表。证据067-off-local-list.json |
| 68 | GET | `/api/v1/local-assets/{id}` | `assetMarket.js:26` | getLocalAsset | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 不存在→HTTP200 code=6001「资产不存在」。证据068-off-local-get.json |
| 69 | POST | `/api/v1/local-assets` | `assetMarket.js:29` | createLocalAsset | 用户端 Cookie / Bearer；权限以后端注册为准 | body asset_type/industry/data必填（枚举校验） | 通过 | 通过 | 200创建id=8；非法asset_type/industry→400中文文案（首轮400系shell编码漂移，文件方式UTF-8后通过）。证据069-off-local-create.json |
| 70 | PUT | `/api/v1/local-assets/{id}` | `assetMarket.js:32` | updateLocalAsset | 用户端 Cookie / Bearer；权限以后端注册为准 | path id；body同create | 通过 | 通过 | 200更新v2。证据070-off-local-update.json |
| 71 | DELETE | `/api/v1/local-assets/{id}` | `assetMarket.js:35` | deleteLocalAsset | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200删除成功。证据071-off-local-delete.json |
| 72 | PUT | `/api/v1/local-assets/{id}/toggle-active` | `assetMarket.js:38` | toggleLocalAsset | 用户端 Cookie / Bearer；权限以后端注册为准 | body { active } | 通过 | 通过 | false→true双向200。证据072-*.json |
| 73 | GET | `/api/v1/local-assets/sync-log` | `assetMarket.js:41` | syncLog | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size | 通过 | 通过 | 200空列表（无同步记录合法空态）。证据073-off-sync-log.json |
| 74 | GET | `/api/backups` | `backup.js:4` | getBackupList | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size | 通过 | 通过 | 200返回历史备份列表。证据074-off-backup-list.json |
| 75 | GET | `/api/backups/{id}` | `backup.js:8` | getBackupByID | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200返回id=8完整详情。证据075-off-backup-get.json |
| 76 | POST | `/api/backups` | `backup.js:12` | createBackup | 用户端 Cookie / Bearer；权限以后端注册为准 | body backup_name/backup_type | 通过 | 通过 | 200创建异步任务id=9（pending）。证据076-off-backup-create.json |
| 77 | DELETE | `/api/backups/{id}` | `backup.js:16` | deleteBackup | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200删除本轮创建的id=9。证据077-off-backup-delete.json |
| 78 | POST | `/api/restore` | `backup.js:20` | restoreBackup | 用户端 Cookie / Bearer；权限以后端注册为准 | body backup_id必填 | 通过（校验分支） | 通过（校验分支） | 空 body→400「backup_id 必填」。真实恢复是不可逆危险操作，本审计不触发；成功路径留给运维演练。证据078-off-restore-empty.json |
| 79 | GET | `/api/restore/list` | `backup.js:24` | getRestoreList | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size | 通过 | 通过 | 200空列表。证据079-off-restore-list.json |
| 80 | GET | `/api/restore/last` | `backup.js:28` | getLastRestore | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200「暂无恢复记录」。证据080-off-restore-last.json |
| 81 | GET | `/api/bad-cases` | `badCase.js:44` | list | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size等 | 通过 | 通过 | 200空态（合法）。证据081-off-badcase-list.json |
| 82 | GET | `/api/bad-cases/stats` | `badCase.js:50` | stats | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200四维统计全0（空库）。证据082-off-stats.json |
| 83 | GET | `/api/bad-cases/taxonomy` | `badCase.js:57` | taxonomy | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200归因词表完整。证据083-off-taxonomy.json |
| 84 | GET | `/api/bad-cases/{id}` | `badCase.js:61` | get | 用户端 Cookie / Bearer；权限以后端注册为准 | path id（string bc_前缀） | 通过 | 通过 | 不存在→404带id文案。证据084-off-missing.json |
| 85 | POST | `/api/bad-cases` | `badCase.js:69` | create | 用户端 Cookie / Bearer；权限以后端注册为准 | body session_id/message_id/query_text等 | 通过 | 通过 | 200创建pending（id=bc_...）。证据085-off-create.json |
| 86 | POST | `/api/bad-cases/{id}/label` | `badCase.js:76` | label | 用户端 Cookie / Bearer；权限以后端注册为准 | body { label, note }；label须在词表 | 通过 | 通过 | 200 labeled；未知类目→400带类目名。证据086-off-label*.json |
| 87 | POST | `/api/bad-cases/{id}/dismiss` | `badCase.js:82` | dismiss | 用户端 Cookie / Bearer；权限以后端注册为准 | body { reason } | 通过 | 通过 | exported状态→409 DUPLICATE_ENTRY_3003状态机拒绝（正确业务分支）；pending态dismiss未覆盖（先被export推进）。证据087-off-dismiss.json |
| 88 | POST | `/api/bad-cases/export` | `badCase.js:89` | exportSet | 用户端 Cookie / Bearer；权限以后端注册为准 | body { labels, limit } | 通过 | 通过 | 200生成eval_set导出1行labeled记录。证据088-off-export.json |
| 89 | GET | `/api/clues/list` | `batchOperation.js:86` | res | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size/keyword等 | 通过 | 通过 | 200列表/keyword过滤（URL编码中文正常；裸中文GBK字节→500为协议违规输入，PG 22021，axios/浏览器不会触发——观察项记录于审计README）。证据089-*.json |
| 90 | POST | `/api/batch/import` | `batchOperation.js:116` | batchImportFile | 用户端 Cookie / Bearer；权限以后端注册为准 | multipart file+type | 通过 | 通过 | **修复BOM缺陷**：Excel导出必带BOM致整批误报缺列；公共入口剥﻿后带/无BOM均success。单测0.852s绿+HTTP复测success=1。证据090-*.json |
| 91 | GET | `/api/batch/template` | `batchOperation.js:122` | downloadBatchTemplate | 用户端 Cookie / Bearer；权限以后端注册为准 | query type | 通过 | 通过 | 200返回中文列头CSV模板。证据091-off-template.csv |
| 92 | POST | `/api/batch/export` | `batchOperation.js:131` | batchExport | 用户端 Cookie / Bearer；权限以后端注册为准 | form-urlencoded type/format/ids（逗号分隔） | 通过 | 通过 | 200返回CSV blob（含ID/姓名/账户等列头）。证据092-off-export.csv |
| 93 | POST | `/api/batch/delete` | `batchOperation.js:147` | batchDelete | 用户端 Cookie / Bearer；权限以后端注册为准 | body { ids } | 通过 | 通过 | 200 deleted_count=3/requested=3（本轮导入夹具清理闭环）。证据093-off-delete.json |
| 94 | POST | `/api/batch/update` | `batchOperation.js:172` | batchUpdate | 用户端 Cookie / Bearer；权限以后端注册为准 | body { ids, fields }（字段值一律字符串） | 通过 | 通过 | 已删除ID→400「所选 ID 均不存在或已被删除」明确拒绝；更新历史落#96列表。证据094-off-update.json |
| 95 | GET | `/api/batch/tools` | `batchOperation.js:179` | getBatchTools | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200 import/export/delete/update工具清单。证据095-off-tools.json |
| 96 | GET | `/api/batch/histories` | `batchOperation.js:222` | getBatchHistories | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size | 通过 | 通过 | 200含本轮update/delete历史。证据096-off-histories.json |
| 97 | POST | `/api/batch/histories/{id}/cancel` | `batchOperation.js:225` | cancelBatch | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | completed→409「仅待执行/执行中可取消」状态机；不存在→404。证据097-off-cancel-*.json |
| 98 | POST | `/api/batch/preview` | `batchOperation.js:238` | previewBatch | 用户端 Cookie / Bearer；权限以后端注册为准 | body operation_type/data_type必填；delete/update/export须ids | 通过 | 通过 | 200预览selected_count=1；缺ids→400「请选择要操作的记录」（与前端resolveClueIds先查ID的契约一致）。证据098-off-preview*.json |
| 99 | POST | `/api/batch/preview` | `batchOperation.js:238` | res | 用户端 Cookie / Bearer；权限以后端注册为准 | body { operation_type, data_type, ids }（同#98别名） | 通过 | 通过 | 同#98：200预览。证据098-off-preview-green.json |
| 100 | GET | `/api/bridge/extension/package` | `bridgeExtension.js:11` | getBridgeExtensionPackage | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 未接平台→404「当前部署未接入平台，或平台尚未上架桥接扩展包」设计内。证据100-off-ext-package.json |
| 101 | GET | `/api/bridge/token/status` | `bridgeToken.js:3` | getBridgeTokenStatus | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200 source=unset（未配置合法空态）。证据101-off-token-status.json |
| 102 | POST | `/api/bridge/token/reset` | `bridgeToken.js:5` | resetBridgeToken | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200轮换成功+灰度窗口文案；token明文回显属功能输出（用户复制配置扩展用），非泄露。证据102-off-token-reset.json |
| 103 | GET | `/api/browser-automation/tasks` | `browserAutomation.js:6` | listBrowserTasks | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size等 | 通过 | 通过 | 200空列表。证据103-off-tasks.json |
| 104 | GET | `/api/browser-automation/tasks/{id}` | `browserAutomation.js:9` | getBrowserTask | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 不存在→404「任务不存在」。证据104-off-task-missing.json |
| 105 | POST | `/api/browser-automation/tasks` | `browserAutomation.js:12` | createBrowserTask | 用户端 Cookie / Bearer；权限以后端注册为准 | body name/task_type/url/platform必填 | 通过 | 通过 | 200 draft；缺platform→400带注册平台列表（B21 fails-loudly）。证据105-off-*.json |
| 106 | PUT | `/api/browser-automation/tasks/{id}` | `browserAutomation.js:15` | updateBrowserTask | 用户端 Cookie / Bearer；权限以后端注册为准 | path id；body同create | 通过 | 通过 | 200；非法step action→400列出合法枚举（dive校验生效）。证据106-*.json |
| 107 | DELETE | `/api/browser-automation/tasks/{id}` | `browserAutomation.js:18` | deleteBrowserTask | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200已删除（本轮任务清理闭环）。证据107-off-delete.json |
| 108 | POST | `/api/browser-automation/tasks/{id}/publish` | `browserAutomation.js:21` | publishBrowserTask | 用户端 Cookie / Bearer；权限以后端注册为准 | path id；显式模式需≥1合法步骤 | 通过 | 通过 | 无步骤→400；合法步骤→200已发布。证据108-*.json |
| 109 | POST | `/api/browser-automation/tasks/{id}/run` | `browserAutomation.js:27` | runBrowserTask | 用户端 Cookie / Bearer；权限以后端注册为准 | body {} | 通过（环境拒绝分支） | 通过（环境拒绝分支） | 409 BROWSER_HOST_OFFLINE_8001（本地无nm-host/扩展），文案带修复指引；真实执行需host在线。证据109-off-run.json |
| 110 | POST | `/api/browser-automation/tasks/{id}/pause` | `browserAutomation.js:30` | pauseBrowserTask | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | draft→409 8003状态机「仅执行中可暂停」。证据110-off-pause.json |
| 111 | POST | `/api/browser-automation/tasks/{id}/resume` | `browserAutomation.js:33` | resumeBrowserTask | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | draft→409 8003「仅暂停任务可恢复」。证据111-off-resume.json |
| 112 | POST | `/api/browser-automation/tasks/{id}/archive` | `browserAutomation.js:36` | archiveBrowserTask | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200已归档。证据112-off-archive.json |
| 113 | PUT | `/api/browser-automation/tasks/{id}/dependency` | `browserAutomation.js:39` | setBrowserTaskDependency | 用户端 Cookie / Bearer；权限以后端注册为准 | body depends_on_task_id/mode | 通过 | 通过 | 不存在任务→404「任务不存在」（成功路径依赖workflow任务编排，本轮验证身份校验分支）。证据113-off-dependency.json |
| 114 | GET | `/api/browser-automation/sessions` | `browserAutomation.js:44` | listBrowserSessions | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size | 通过 | 通过 | 200空列表。证据114-off-sessions.json |
| 115 | GET | `/api/browser-automation/sessions/{id}` | `browserAutomation.js:47` | getBrowserSession | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 合成completed会话→200详情完整；非数字id→400 invalid id。证据115-off-session-*.json |
| 116 | GET | `/api/browser-automation/sessions/{id}/steps` | `browserAutomation.js:50` | getBrowserSessionSteps | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200空steps（无步进记录）。证据116-off-session-steps.json |
| 117 | GET | `/api/browser-automation/tasks/{taskId}/sessions` | `browserAutomation.js:54` | listBrowserTaskSessions | 用户端 Cookie / Bearer；权限以后端注册为准 | query params | 通过 | 通过 | 200按任务过滤空列表。证据117-off-task-sessions.json |
| 118 | POST | `/api/browser-automation/sessions/{id}/stop` | `browserAutomation.js:57` | stopBrowserSession | 用户端 Cookie / Bearer；权限以后端注册为准 | body { reason } | 通过 | 通过 | completed会话→200 stopped=false「会话不在运行中」幂等正确。证据118-off-session-stop.json |
| 119 | GET | `/api/browser-automation/sessions/{id}/confirm-gate` | `browserAutomation.js:62` | getBrowserConfirmGate | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200 pending=false。证据119-off-confirm-gate.json |
| 120 | POST | `/api/browser-automation/sessions/{id}/confirm` | `browserAutomation.js:72` | confirmBrowserSession | 用户端 Cookie / Bearer；权限以后端注册为准 | body { payload_hash } | 通过 | 通过 | 无待确认门→200 confirmed=false status=no_gate。证据120-off-confirm.json |
| 121 | GET | `/api/browser-automation/sessions/{id}/logs` | `browserAutomation.js:93` | getBrowserSessionLogs | 用户端 Cookie / Bearer；权限以后端注册为准 | query direction可选 | 通过 | 通过 | 200空日志。证据121-off-session-logs.json |
| 122 | GET | `/api/browser-automation/sessions/{id}/export` | `browserAutomation.js:97` | exportBrowserSessionAudit | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200审计导出（session+audit_digests+command_log）。证据122-off-session-export.json |
| 123 | GET | `/api/browser-automation/tasks/{taskId}/receipts` | `browserAutomation.js:102` | listBrowserTaskReceipts | 用户端 Cookie / Bearer；权限以后端注册为准 | path taskId | 通过 | 通过 | 不存在任务→404。证据123-off-receipts.json |
| 124 | GET | `/api/browser-automation/cron` | `browserAutomation.js:107` | listBrowserCron | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200空列表。证据124-off-cron.json |
| 125 | POST | `/api/browser-automation/cron` | `browserAutomation.js:110` | createBrowserCron | 用户端 Cookie / Bearer；权限以后端注册为准 | body task_id+cron_expr必填 | 通过 | 通过 | 缺expr→400；坏表达式→400明确5/6段格式；FK先查任务404；合法→200含next_run_at。证据125-*.json |
| 126 | PUT | `/api/browser-automation/cron/{id}` | `browserAutomation.js:113` | updateBrowserCron | 用户端 Cookie / Bearer；权限以后端注册为准 | body task_id/cron_expr | 通过 | 通过 | 200更新表达式0 8 * * 1→next_run_at随新表达式。证据126-off-cron-update.json |
| 127 | DELETE | `/api/browser-automation/cron/{id}` | `browserAutomation.js:116` | deleteBrowserCron | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200已删除（清理闭环）。证据127-off-cron-delete.json |
| 128 | POST | `/api/browser-automation/cron/{id}/enable` | `browserAutomation.js:119` | enableBrowserCron | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200已启用。证据128-off-cron-enable.json |
| 129 | POST | `/api/browser-automation/cron/{id}/disable` | `browserAutomation.js:122` | disableBrowserCron | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200已停用。证据129-off-cron-disable.json |
| 130 | GET | `/api/browser-automation/host/status` | `browserAutomation.js:127` | getBrowserHostStatus | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200 online=false/count=0（host未连合法空态）。证据130-off-host-status.json |
| 131 | POST | `/api/browser-automation/host/token/reset` | `browserAutomation.js:130` | resetBrowserHostToken | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200轮换（与#102同族口径，token明文属功能输出）。证据102-off-token-reset.json同链路 |
| 132 | GET | `/api/browser-automation/platforms` | `browserAutomation.js:135` | listPlatforms | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200平台清单（xiaohongshu/douyin/xianyu带能力位）。证据132-off-platforms.json |
| 133 | GET | `/api/browser-automation/platforms/{id}/locators` | `browserAutomation.js:138` | getPlatformLocators | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | xiaohongshu→200定位表；未知平台→404带可用列表。证据133-off-locators*.json |
| 134 | GET | `/api/whatsapp/templates` | `bulkMessaging.js:4` | getTemplates | 用户端 Cookie / Bearer；权限以后端注册为准 | query params | 通过 | 通过 | 200空态。证据134-off-templates.json |
| 135 | POST | `/api/whatsapp/templates` | `bulkMessaging.js:8` | createTemplate | 用户端 Cookie / Bearer；权限以后端注册为准 | body name/content | 通过 | 通过 | 200创建tmpl_...。证据135-off-template-create.json |
| 136 | PUT | `/api/whatsapp/templates/{id}` | `bulkMessaging.js:12` | updateTemplate | 用户端 Cookie / Bearer；权限以后端注册为准 | path id；body name/content等 | 通过 | 通过 | 200更新。证据136-off-template-update.json |
| 137 | DELETE | `/api/whatsapp/templates/{id}` | `bulkMessaging.js:16` | deleteTemplate | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 138 | POST | `/api/whatsapp/group-messaging/send` | `bulkMessaging.js:20` | sendBulkMessage | 用户端 Cookie / Bearer；权限以后端注册为准 | body lead_ids+template_id必填 | 通过（防线分支） | 通过（防线分支） | 缺参→400；线索缺手机号→400「所选线索不存在或缺少手机号」双层外发防线正确（真实发送需WhatsApp通道，审计不触发）。证据138-off-send*.json |
| 139 | GET | `/api/whatsapp/group-messaging/status/{queueId}` | `bulkMessaging.js:24` | getMessageStatus | 用户端 Cookie / Bearer；权限以后端注册为准 | path queueId | 通过 | 通过 | 未知队列→200空结构（零值状态）。证据139-off-status.json |
| 140 | GET | `/api/whatsapp/group-messaging/records` | `bulkMessaging.js:28` | getSendRecords | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/limit | 通过 | 通过 | 200空态。证据140-off-records.json |
| 141 | GET | `/api/capabilities` | `capabilityDiagnostics.js:12` | get | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200能力矩阵（bilibili dm.receive等，含implemented/configured/ready/reason）。证据141-off-capabilities.json |
| 142 | GET | `/api/channel-agent-bindings` | `channelAgentBinding.js:4` | listBindings | 用户端 Cookie / Bearer；权限以后端注册为准 | query channel_type+account_id必填 | 通过 | 通过 | 缺参→400「channel_type 和 account_id 必填」；带参→200空列表。证据142-off-*.json |
| 143 | GET | `/api/channel-agent-bindings/by-agent/{agentId}` | `channelAgentBinding.js:8` | listBindingsByAgent | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 144 | POST | `/api/channel-agent-bindings` | `channelAgentBinding.js:12` | createBinding | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 145 | PUT | `/api/channel-agent-bindings/{id}` | `channelAgentBinding.js:16` | updateBinding | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 146 | DELETE | `/api/channel-agent-bindings/{id}` | `channelAgentBinding.js:20` | deleteBinding | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 147 | GET | `/api/chat-channels` | `chatChannel.js:4` | listChannels | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size | 通过 | 通过 | 200返回seed渠道。证据147-off-channels.json |
| 148 | GET | `/api/chat-channels/{channelId}` | `chatChannel.js:8` | getChannel | 用户端 Cookie / Bearer；权限以后端注册为准 | path channelId（主键id） | 通过 | 通过 | 200返回id=3渠道详情。证据148-off-channel-get.json |
| 149 | POST | `/api/chat-channels` | `chatChannel.js:12` | createChannel | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 150 | PUT | `/api/chat-channels/{channelId}` | `chatChannel.js:16` | updateChannel | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 151 | DELETE | `/api/chat-channels/{channelId}` | `chatChannel.js:20` | deleteChannel | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 152 | POST | `/api/chat-channels/{channelId}/rotate-key` | `chatChannel.js:24` | rotateAppKey | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 153 | POST | `/api/chat-channels/{channelId}/reset-secret` | `chatChannel.js:28` | resetAppSecret | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 154 | GET | `/api/churn/prediction` | `churnPrediction.js:4` | getChurnPrediction | 用户端 Cookie / Bearer；权限以后端注册为准 | query user_id必填、days | 通过 | 通过 | 缺user_id→400；存在性未校验（不存在用户返回零值模型churn_risk=low——宽松契约观察项，无泄露）。证据154-off-churn*.json |
| 155 | GET | `/api/churn-prediction` | `churnPrediction.js:7` | getChurnPredictions | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size | 通过 | 通过 | 200返回seed预测（churn_score 63.13 medium等）。证据155-off-predictions.json |
| 156 | GET | `/api/churn-prediction/users` | `churnPrediction.js:10` | getHighRiskUsers | 用户端 Cookie / Bearer；权限以后端注册为准 | query threshold等 | 通过 | 通过 | 200（threshold=70无高危→空数组）。证据156-off-high-risk.json |
| 157 | GET | `/api/churn-prediction/warnings` | `churnPrediction.js:13` | getChurnWarnings | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size | 通过 | 通过 | 200返回seed预警。证据157-off-warnings.json |
| 158 | GET | `/api/churn/unhandled-warnings` | `churnPrediction.js:16` | getUnhandledWarnings | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size | 通过 | 通过 | 200返回未处理预警。证据158-off-unhandled.json |
| 159 | POST | `/api/churn/warnings/{id}/handle` | `churnPrediction.js:19` | markWarningHandled | 用户端 Cookie / Bearer；权限以后端注册为准 | path id；body处理人/备注 | 通过 | 通过 | 不存在→404带id。证据159-off-handle.json |
| 160 | GET | `/api/churn-prediction/model-config` | `churnPrediction.js:22` | getChurnModelConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200模型权重配置（未配置时零值默认）。证据160-off-model-config.json |
| 161 | POST | `/api/churn-prediction/model-config` | `churnPrediction.js:25` | saveChurnModelConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 162 | GET | `/api/churn-prediction/statistics` | `churnPrediction.js:28` | getChurnStatistics | 用户端 Cookie / Bearer；权限以后端注册为准 | query start_date/end_date必填 | 通过 | 通过 | 缺日期→400明确文案；带日期→200统计。证据162-off-statistics*.json |
| 163 | GET | `/api/churn/risk-distribution` | `churnPrediction.js:31` | getRiskDistribution | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200四档分布（low38/medium12）。证据163-off-risk-dist.json |
| 164 | POST | `/api/churn/warnings/intervene` | `churnPrediction.js:38` | interveneUser | 用户端 Cookie / Bearer；权限以后端注册为准 | body intervention_type必填 | 通过 | 通过 | 缺类型→400明确文案。证据164-off-intervene.json |
| 165 | POST | `/api/user-segment/rfm/calculate` | `churnPrediction.js:50` | calculateRFM | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 166 | DELETE | `/api/clues/delete/{id}` | `clue.js:5` | delete | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 不存在→404带id。证据166-off-clue-delete.json |
| 167 | GET | `/api/clues/list?{q}` | `clue.js:12` | list | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 168 | GET | `/api/clues/statistics` | `clue.js:15` | statistics | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200分类型统计。证据168-off-clue-stats.json |
| 169 | POST | `/api/clues/import` | `clue.js:18` | import | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 170 | GET | `/api/system/stats` | `cockpit.js:4` | getSystemStatus | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200 CPU/内存/磁盘/运行时长。证据170-off-system-stats.json |
| 171 | GET | `/api/monitor/health` | `cockpit.js:8` | getOpsStats | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200收发速率/积压。证据171-off-monitor-health.json |
| 172 | GET | `/api/monitor/node-health` | `cockpit.js:12` | getModuleStatus | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200节点健康矩阵。证据172-off-node-health.json |
| 173 | GET | `/api/monitor/anomalies` | `cockpit.js:16` | getRecentOperations | 用户端 Cookie / Bearer；权限以后端注册为准 | query limit | 通过 | 通过 | 200异常汇总。证据173-off-anomalies.json |
| 174 | GET | `/api/ai/sales-cockpit` | `cockpit.js:24` | getSalesCockpit | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200渠道健康/销售面板。证据174-off-sales-cockpit.json |
| 175 | GET | `/api/community/groups` | `community.js:5` | getGroups | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size | 通过 | 通过 | 200空态。证据175-off-groups.json |
| 176 | POST | `/api/community/groups` | `community.js:9` | createGroup | 用户端 Cookie / Bearer；权限以后端注册为准 | body name/platform | 通过 | 通过 | 200创建。证据176-off-group-create.json |
| 177 | GET | `/api/community/groups/{id}` | `community.js:13` | getGroupById | 用户端 Cookie / Bearer；权限以后端注册为准 | path id（uuid） | 通过 | 通过 | 200详情。证据177-off-group-get.json |
| 178 | PUT | `/api/community/groups/{id}` | `community.js:17` | updateGroup | 用户端 Cookie / Bearer；权限以后端注册为准 | path id；body name/description | 通过 | 通过 | 200更新。证据178-off-group-update.json |
| 179 | DELETE | `/api/community/groups/{id}` | `community.js:21` | deleteGroup | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200删除（清理闭环）。证据179-off-group-delete.json |
| 180 | GET | `/api/community/members` | `community.js:25` | getMembers | 用户端 Cookie / Bearer；权限以后端注册为准 | query group_id等 | 通过 | 通过 | 200空成员。证据180-off-members.json |
| 181 | GET | `/api/community/messages` | `community.js:29` | getMessages | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 182 | GET | `/api/community/stats` | `community.js:33` | getStats | 用户端 Cookie / Bearer；权限以后端注册为准 | query可选 | 通过 | 通过 | 200统计（删除前total_groups=1）。证据182-off-stats.json |
| 183 | POST | `/api/community/import` | `community.js:37` | importData | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 184 | POST | `/api/community/export` | `community.js:41` | exportData | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 185 | GET | `/api/manage/config-params` | `configParams.js:8` | list | 用户端 Cookie / Bearer；权限以后端注册为准 | query group/include_deprecated | 通过 | 通过 | 200参数清单（288项种子）。证据185-off-config-list.json |
| 186 | GET | `/api/manage/config-params/groups` | `configParams.js:17` | listGroups | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200分组的组清单。证据186-off-config-groups.json |
| 187 | GET | `/api/manage/config-params/audit-logs` | `configParams.js:21` | auditLogs | 用户端 Cookie / Bearer；权限以后端注册为准 | query limit | 通过 | 通过 | 200含embedding等历史变更。证据187-off-audit-logs.json |
| 188 | PUT | `/api/manage/config-params/{group}/{key}` | `configParams.js:25` | update | 用户端 Cookie / Bearer；权限以后端注册为准 | body { value } | 通过 | 通过 | 闭环：改agent_loop_drift_factor→1.35读回生效；审计落#187。证据188-*.json |
| 189 | POST | `/api/manage/config-params/{group}/{key}/reset` | `configParams.js:29` | reset | 用户端 Cookie / Bearer；权限以后端注册为准 | 无body | 通过 | 通过 | 闭环：reset→默认2.5读回恢复。证据189-*.json |
| 190 | POST | `/api/manage/config-params/{group}/reset` | `configParams.js:33` | bulkReset | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 191 | PUT | `/api/manage/platform-host` | `configParams.js:47` | setPlatformHost | 用户端 Cookie / Bearer；权限以后端注册为准 | { address } ; { _silent: true } | 未测 | 未测 | — |
| 192 | POST | `/api/manage/platform-host/connectivity` | `configParams.js:56` | testPlatformConnectivity | 用户端 Cookie / Bearer；权限以后端注册为准 | body { address } | 通过 | 通过 | PLATFORM_ENABLED=false→200 reachable=false带「先开平台集成」说明（设计内短路）。证据192-off-connectivity.json |
| 193 | GET | `/api/contacts/{customerId}/consents` | `contactConsent.js:12` | getTimeline | 用户端 Cookie / Bearer；权限以后端注册为准 | path customerId；query可选 | 通过 | 通过 | 200空时间线。证据193-off-consent-timeline.json |
| 194 | POST | `/api/contacts/{customerId}/consents` | `contactConsent.js:16` | record | 用户端 Cookie / Bearer；权限以后端注册为准 | body channel/decision/purpose/source（枚举校验） | 通过 | 通过 | 逐级枚举校验（decision/purpose/source各自400带合法值）；合法→200 recorded。证据194-off-consent-record*.json |
| 195 | GET | `/api/contacts/{customerId}/preferences` | `contactConsent.js:20` | getPreferences | 用户端 Cookie / Bearer；权限以后端注册为准 | path customerId | 通过 | 通过 | 200空列表。证据195-off-preferences.json |
| 196 | PUT | `/api/contacts/{customerId}/preferences` | `contactConsent.js:24` | setPreference | 用户端 Cookie / Bearer；权限以后端注册为准 | body purpose必填(marketing/support/transactional) | 通过 | 通过 | 缺purpose→400；合法→200 updated。证据196-*.json |
| 197 | GET | `/api/conversion-funnels` | `conversionFunnel.js:5` | getFunnel | 用户端 Cookie / Bearer；权限以后端注册为准 | query days等 | 通过 | 通过 | 200漏斗5阶段。证据197-off-funnel.json |
| 198 | GET | `/api/conversion-funnels/stage` | `conversionFunnel.js:8` | getStageDetails | 用户端 Cookie / Bearer；权限以后端注册为准 | query stage+days | 通过 | 通过 | 200 stage=lead详情。证据198-off-stage.json |
| 199 | POST | `/api/customer-sessions/{sessionId}/csat` | `csat.js:4` | submitCSAT | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 200 | GET | `/api/csat/stats` | `csat.js:7` | getCSATStats | 用户端 Cookie / Bearer；权限以后端注册为准 | query days | 通过 | 通过 | 200空态统计。证据200-off-csat-stats.json |
| 201 | GET | `/api/csat/trend` | `csat.js:10` | getCSATTrend | 用户端 Cookie / Bearer；权限以后端注册为准 | query days | 通过 | 通过 | 200空趋势。证据201-off-csat-trend.json |
| 202 | GET | `/api/csat/negative` | `csat.js:13` | getNegativeCSAT | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 203 | POST | `/api/customer-sessions/{sessionId}/csat/trigger` | `csat.js:16` | triggerCSATSurvey | 用户端 Cookie / Bearer；权限以后端注册为准 | {} | 未测 | 未测 | — |
| 204 | GET | `/api/csat/template` | `csat.js:18` | getCSATTemplate | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200默认模板。证据204-off-csat-template.json |
| 205 | PUT | `/api/csat/template` | `csat.js:19` | saveCSATTemplate | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 206 | GET | `/api/custom-tools` | `customIntegration.js:6` | listCustomTools | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200空态；导入后含记录。证据206-*.json |
| 207 | POST | `/api/custom-tools/import` | `customIntegration.js:10` | importCustomTool | 用户端 Cookie / Bearer；权限以后端注册为准 | body name/base_url/schema_json(string)必填 | 通过 | 通过 | 缺字段逐级400；合法OpenAPI→200导入1工具。证据207-off-tool-import.json |
| 208 | DELETE | `/api/custom-tools/{id}` | `customIntegration.js:14` | deleteCustomTool | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200 deleted=1（清理闭环）。证据208-off-tool-delete.json |
| 209 | GET | `/api/mcp-servers` | `customIntegration.js:18` | listMcpServers | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200空态→创建后含id=1。证据209-*.json |
| 210 | POST | `/api/mcp-servers` | `customIntegration.js:22` | createMcpServer | 用户端 Cookie / Bearer；权限以后端注册为准 | body name/endpoint必填 | 通过 | 通过 | **SSRF防护真实生效**：127.0.0.1→400拒绝内网；公网→200保存。证据210-off-mcp-create.json |
| 211 | DELETE | `/api/mcp-servers/{id}` | `customIntegration.js:26` | deleteMcpServer | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200已删除。证据211-off-mcp-delete.json |
| 212 | PUT | `/api/mcp-servers/{id}/enabled` | `customIntegration.js:30` | setMcpServerEnabled | 用户端 Cookie / Bearer；权限以后端注册为准 | body { enabled } | 通过 | 通过 | 200 enabled=false回显。证据212-off-mcp-enabled.json |
| 213 | GET | `/api/mcp-servers/{id}/test` | `customIntegration.js:34` | testMcpServer | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 已删除MCP→错误分支（创建→test→删除链路见#210-211）；实测127.0.0.1被SSRF门拒绝，测试路由同域。证据210-off-mcp-create.json |
| 214 | GET | `/api/custom-reports` | `customReport.js:4` | getReportList | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size | 通过 | 通过 | 200空态。证据214-off-reports.json |
| 215 | GET | `/api/custom-reports/{id}` | `customReport.js:7` | getReport | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 216 | POST | `/api/custom-reports` | `customReport.js:10` | createReport | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 217 | PUT | `/api/custom-reports/{id}` | `customReport.js:13` | updateReport | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 218 | DELETE | `/api/custom-reports/{id}` | `customReport.js:16` | deleteReport | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 219 | GET | `/api/custom-reports/templates` | `customReport.js:19` | getPublicTemplates | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200空模板。证据219-off-templates.json |
| 220 | POST | `/api/custom-reports/templates/{id}/use` | `customReport.js:22` | useReportTemplate | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 221 | GET | `/api/custom-reports/{id}/data` | `customReport.js:25` | queryReportData | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 222 | GET | `/api/custom-reports/{id}/data?format=export` | `customReport.js:41` | exportCustomReport | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 223 | GET | `/api/custom-reports/{id}/data?format=run` | `customReport.js:44` | runCustomReport | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 224 | GET | `/api/customer-360/basic?user_id={id}` | `customer360.js:4` | getCustomerBasic | 用户端 Cookie / Bearer；权限以后端注册为准 | query user_id | 通过 | 通过 | 200客户1号基础信息。证据224-off-c360-basic.json |
| 225 | GET | `/api/customer-360/events` | `customer360.js:7` | getCustomerEvents | 用户端 Cookie / Bearer；权限以后端注册为准 | query user_id+page | 通过 | 通过 | 200事件列表。证据225-off-c360-events.json |
| 226 | GET | `/api/customer-360/sessions` | `customer360.js:10` | getCustomerSessions | 用户端 Cookie / Bearer；权限以后端注册为准 | query user_id+page | 通过 | 通过 | 200会话历史（seed-sess-0001 douyin closed）。证据226-off-c360-sessions.json |
| 227 | GET | `/api/customer-360/tags?user_id={id}` | `customer360.js:13` | getCustomerTags | 用户端 Cookie / Bearer；权限以后端注册为准 | query user_id | 通过 | 通过 | 200空标签。证据227-off-c360-tags.json |
| 228 | GET | `/api/customer-360/messages` | `customer360.js:23` | getCustomerMessages | 用户端 Cookie / Bearer；权限以后端注册为准 | query user_id+page | 通过 | 通过 | 200消息历史。证据228-off-c360-messages.json |
| 229 | GET | `/api/customer-360/orders` | `customer360.js:27` | getCustomerOrders | 用户端 Cookie / Bearer；权限以后端注册为准 | query user_id+page | 通过 | 通过 | 200空订单。证据229-off-c360-orders.json |
| 230 | GET | `/api/customer-360/list` | `customer360.js:53` | getCustomerList | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size | 通过 | 通过 | 200客户映射列表。证据230-off-c360-list.json |
| 231 | GET | `/api/customer-360/basic?user_id={id}` | `customer360.js:56` | getCustomerBasicById | 用户端 Cookie / Bearer；权限以后端注册为准 | query user_id（同#224别名行） | 通过 | 通过 | 同#224同handler链。证据224-off-c360-basic.json |
| 232 | GET | `/api/customer-360/tags?user_id={id}` | `customer360.js:76` | r | 用户端 Cookie / Bearer；权限以后端注册为准 | query user_id（同#227别名行） | 通过 | 通过 | 同#227。证据227-off-c360-tags.json |
| 233 | PUT | `/api/customer-360/tags?user_id={id}` | `customer360.js:85` | addCustomerTag | 用户端 Cookie / Bearer；权限以后端注册为准 | body { tags }——PUT为整体替换语义（ReplaceUserTags） | 通过 | 通过 | 200加标签读回生效。证据233-off-tag-add.json |
| 234 | PUT | `/api/customer-360/tags?user_id={id}` | `customer360.js:90` | removeCustomerTag | 用户端 Cookie / Bearer；权限以后端注册为准 | 同上：替换为移除后集合；tags=[]清空 | 通过 | 通过 | 200替换空数组→读回[]（前端发替换集合实现增/删）。证据234-*.json |
| 235 | POST | `/api/events/track` | `customerEvent.js:4` | trackEvent | 用户端 Cookie / Bearer；权限以后端注册为准 | body customer_id/event_type/properties | 通过 | 通过 | 200追踪成功。证据235-off-track.json |
| 236 | POST | `/api/events/pageview` | `customerEvent.js:7` | trackPageView | 用户端 Cookie / Bearer；权限以后端注册为准 | body customer_id+url必填,title可选 | 通过 | 通过 | 缺字段→400带字段名；合法→200。证据236-off-pageview.json |
| 237 | POST | `/api/events/click` | `customerEvent.js:10` | trackClick | 用户端 Cookie / Bearer；权限以后端注册为准 | body customer_id+element必填,target可选 | 通过 | 通过 | 200。证据237-off-click.json |
| 238 | POST | `/api/events/purchase` | `customerEvent.js:13` | trackPurchase | 用户端 Cookie / Bearer；权限以后端注册为准 | body customer_id/amount(非零)/items | 通过 | 通过 | 200。证据238-off-purchase.json |
| 239 | POST | `/api/events/signup` | `customerEvent.js:16` | trackSignup | 用户端 Cookie / Bearer；权限以后端注册为准 | body customer_id/email等 | 通过 | 通过 | 200。证据239-off-signup.json |
| 240 | POST | `/api/events/login` | `customerEvent.js:19` | trackLogin | 用户端 Cookie / Bearer；权限以后端注册为准 | body customer_id | 通过 | 通过 | 200。证据240-off-login.json |
| 241 | POST | `/api/events/add-to-cart` | `customerEvent.js:22` | trackAddToCart | 用户端 Cookie / Bearer；权限以后端注册为准 | body customer_id/product_id/price(非零)/quantity | 通过 | 通过 | 缺price→400；合法→200。证据241-off-cart.json |
| 242 | GET | `/api/events/customer/{customerId}` | `customerEvent.js:25` | getCustomerEventHistory | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size | 通过 | 通过 | 200事件历史。证据242-off-event-history.json |
| 243 | GET | `/api/events/stats` | `customerEvent.js:28` | getEventStats | 用户端 Cookie / Bearer；权限以后端注册为准 | query days | 通过 | 通过 | 200统计407事件。证据243-off-event-stats.json |
| 244 | DELETE | `/api/events/customer/{id}` | `customerEvent.js:40` | deleteEvent | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200（deleted_count=0——按事件行ID删除语义，本轮fixture为history视图ID）。证据244-off-event-delete.json |
| 245 | GET | `/api/customer-journey/overview` | `customerJourney.js:4` | overview | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200十阶段漏斗。证据245-off-journey.json |
| 246 | GET | `/api/customer-journey/overview` | `customerJourney.js:4` | getJourneyOverview | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参（同#245别名行） | 通过 | 通过 | 同#245。证据245-off-journey.json |
| 247 | GET | `/api/customer-journey/overview?customer_id={customerId}` | `customerJourney.js:8` | getJourneyState | 用户端 Cookie / Bearer；权限以后端注册为准 | query customer_id | 通过 | 通过 | 200当前stage=stranger。证据247-off-journey-state.json |
| 248 | GET | `/api/customer-journey/stages` | `customerJourney.js:12` | listJourneyStages | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200阶段定义。证据248-off-stages.json |
| 249 | GET | `/api/customer-journey/by-stage?stage={stage}` | `customerJourney.js:16` | listByStage | 用户端 Cookie / Bearer；权限以后端注册为准 | query stage | 通过 | 通过 | 200 count=0。证据249-off-by-stage.json |
| 250 | POST | `/api/customer-journey/transition` | `customerJourney.js:20` | transitionJourney | 用户端 Cookie / Bearer；权限以后端注册为准 | body customer_id/to_stage必填,source/reason可选 | 通过 | 通过 | 200迁移成功（from contact→to lead）。证据250-off-transition.json |
| 251 | POST | `/api/customer-journey/touch` | `customerJourney.js:24` | touchCustomer | 用户端 Cookie / Bearer；权限以后端注册为准 | body customer_id/channel | 通过 | 通过 | 200互动已记录。证据251-off-touch.json |
| 252 | POST | `/api/agents` | `customerService.js:4` | createAgent | 用户端 Cookie / Bearer；权限以后端注册为准 | body agent_id(正整数)/agent_name必填,max_sessions(1~50) | 通过 | 通过 | 200创建id=6 agent_id=20（测后SQL清理）。证据252-off-agent-create.json |
| 253 | GET | `/api/agents/{id}` | `customerService.js:7` | getAgentStatus | 用户端 Cookie / Bearer；权限以后端注册为准 | path id——**agent_id业务键**（非主键；前端myAgentId=me.agent_id契约一致） | 通过 | 通过 | agent_id=20→200；主键6→404。观察项：404文案为裸id。证据253-off-agent-get-20.json |
| 254 | GET | `/api/agents/online` | `customerService.js:10` | getOnlineAgents | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200空在线列表。证据254-off-agents-online.json |
| 255 | GET | `/api/agents/all` | `customerService.js:13` | listAllAgents | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200座席列表。证据255-off-agents-all.json |
| 256 | PUT | `/api/agents/{id}/status` | `customerService.js:16` | updateAgentStatus | 用户端 Cookie / Bearer；权限以后端注册为准 | path agent_id；body { status } | 通过 | 通过 | 200更新成功。证据256-off-agent-status-20.json |
| 257 | POST | `/api/agents/{id}/online` | `customerService.js:19` | goOnline | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200上线成功（测后SQL还原offline）。证据257-off-go-online.json |
| 258 | POST | `/api/agents/{id}/offline` | `customerService.js:22` | goOffline | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 400「还有 4 个未完成的会话」业务保护正确。证据258-off-go-offline.json |
| 259 | GET | `/api/agents/{id}/sessions` | `customerService.js:25` | getAgentSessions | 用户端 Cookie / Bearer；权限以后端注册为准 | path agent_id | 通过 | 通过 | 200空会话。证据259-off-agent-sessions.json |
| 260 | GET | `/api/agents/me` | `customerService.js:28` | getMyAgent | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200当前座席。证据260-off-agents-me.json |
| 261 | GET | `/api/quick-replies` | `customerService.js:32` | getQuickReplies | 用户端 Cookie / Bearer；权限以后端注册为准 | query page等 | 通过 | 通过 | 200快捷回复列表。证据261-off-quick-replies.json |
| 262 | GET | `/api/quick-replies/categories` | `customerService.js:35` | getQuickReplyCategories | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200六类目。证据262-off-qr-categories.json |
| 263 | POST | `/api/quick-replies` | `customerService.js:38` | createQuickReply | 用户端 Cookie / Bearer；权限以后端注册为准 | body category/title/content/channel | 通过 | 通过 | 200创建id=19。证据263-off-qr-create.json |
| 264 | PUT | `/api/quick-replies/{id}` | `customerService.js:41` | updateQuickReply | 用户端 Cookie / Bearer；权限以后端注册为准 | path id；body同create | 通过 | 通过 | 200更新。证据264-off-qr-update.json |
| 265 | DELETE | `/api/quick-replies/{id}` | `customerService.js:44` | deleteQuickReply | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200删除（清理闭环）。证据265-off-qr-delete.json |
| 266 | GET | `/api/session-tags` | `customerService.js:48` | getSessionTags | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200空态。证据266-off-session-tags.json |
| 267 | POST | `/api/session-tags` | `customerService.js:51` | createSessionTag | 用户端 Cookie / Bearer；权限以后端注册为准 | body name+code必填 | 通过 | 通过 | 缺code→400；合法→200。证据267-off-stag-create.json |
| 268 | PUT | `/api/session-tags/{id}` | `customerService.js:54` | updateSessionTag | 用户端 Cookie / Bearer；权限以后端注册为准 | path id；body同create | 通过 | 通过 | 200更新。证据268-off-stag-update.json |
| 269 | DELETE | `/api/session-tags/{id}` | `customerService.js:57` | deleteSessionTag | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200删除。证据269-off-stag-delete.json |
| 270 | GET | `/api/ai-suggestions/{sessionId}` | `customerService.js:61` | getAISuggestions | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 271 | POST | `/api/ai-suggestions/{id}/use` | `customerService.js:64` | useAISuggestion | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 272 | POST | `/api/customer-sessions/{sessionId}/tags` | `customerService.js:68` | tagSession | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 273 | GET | `/api/customer-sessions` | `customerSession.js:4` | getSessions | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size等 | 通过 | 通过 | 200返回seed会话。证据273-off-sessions.json |
| 274 | GET | `/api/customer-sessions/{id}/messages` | `customerSession.js:7` | getSessionMessages | 用户端 Cookie / Bearer；权限以后端注册为准 | path id+query page | 通过 | 通过 | 200历史消息；发送后含新消息。证据274-*.json |
| 275 | POST | `/api/customer-sessions/{data.sessionId}/messages` | `customerSession.js:10` | sendMessage | 用户端 Cookie / Bearer；权限以后端注册为准 | body content/sender_type/sender_name/sender_id | 通过 | 通过 | closed会话→400「会话状态 closed 不允许发送消息」（正确状态机）；活跃会话→200落库id=675。证据275-off-send.json、274b |
| 276 | POST | `/api/customer-sessions` | `customerSession.js:18` | createSession | 用户端 Cookie / Bearer；权限以后端注册为准 | body platform/account_id/user_id等 | 通过 | 通过 | 200创建sess_...（pending）。证据276-off-session-create.json |
| 277 | POST | `/api/customer-sessions/{id}/close` | `customerSession.js:21` | closeSession | 用户端 Cookie / Bearer；权限以后端注册为准 | path id | 通过 | 通过 | 200关闭成功。证据277-off-close.json |
| 278 | POST | `/api/customer-sessions/{id}/transfer` | `customerSession.js:24` | transferSession | 用户端 Cookie / Bearer；权限以后端注册为准 | body new_agent_id必填（目标坐席用户主键） | 通过 | 通过 | 缺new_agent_id→400明确文案；成功路径需活跃座席（本轮已closed，拒绝分支已验）。证据278-off-transfer.json |
| 279 | POST | `/api/customer-sessions/{id}/takeover` | `customerSession.js:28` | takeoverSession | 用户端 Cookie / Bearer；权限以后端注册为准 | { reason } | 未测 | 未测 | — |
| 280 | POST | `/api/customer-sessions/{id}/release` | `customerSession.js:32` | releaseSession | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 281 | POST | `/api/customer-sessions/{id}/switch-handler` | `customerSession.js:36` | switchSessionHandler | 用户端 Cookie / Bearer；权限以后端注册为准 | body handler_type/reason | 通过 | 通过 | closed会话→400「当前会话未由坐席持有」状态机正确。证据281-off-switch.json |
| 282 | POST | `/api/customer-sessions/{id}/blacklist` | `customerSession.js:40` | blacklistSession | 用户端 Cookie / Bearer；权限以后端注册为准 | body reason/ttl_hours | 通过 | 通过 | 200拉黑成功（closed会话的user）。证据282-off-blacklist.json |
| 283 | POST | `/api/customer-sessions/blacklist/remove` | `customerSession.js:44` | unblacklistUser | 用户端 Cookie / Bearer；权限以后端注册为准 | body user_id/platform | 通过 | 通过 | 200解除（清理闭环）。证据283-off-unblacklist.json |
| 284 | GET | `/api/customer-sessions/blacklist/check` | `customerSession.js:48` | checkBlacklist | 用户端 Cookie / Bearer；权限以后端注册为准 | query user_id/platform（GET方法） | 通过 | 通过 | 200 blacklisted=false。证据284-off-blacklist-check.json |
| 285 | GET | `/api/customer-sessions/blacklist` | `customerSession.js:52` | listBlacklist | 用户端 Cookie / Bearer；权限以后端注册为准 | query page等 | 通过 | 通过 | 200黑名单列表。证据285-off-blacklist-list.json |
| 286 | GET | `/api/customer-360?user_id={id}` | `customerSession.js:56` | getCustomer360 | 用户端 Cookie / Bearer；权限以后端注册为准 | query user_id | 通过 | 通过 | 200聚合视图。证据286-off-c360.json |
| 287 | GET | `/api/customer-360/basic?user_id={id}` | `customerSession.js:59` | getCustomerBasic | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 288 | GET | `/api/customer-360/stats?user_id={id}` | `customerSession.js:62` | getCustomerStats | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 289 | GET | `/api/customer-360/sessions?user_id={id}` | `customerSession.js:65` | getCustomerSessions | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 290 | GET | `/api/customer-360/tags?user_id={id}` | `customerSession.js:68` | getCustomerTags | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 291 | GET | `/api/dashboards` | `dashboardScreen.js:4` | getScreenList | 用户端 Cookie / Bearer；权限以后端注册为准 | query page/page_size | 通过 | 通过 | 200空态。证据291-off-screens.json |
| 292 | GET | `/api/dashboards/{id}` | `dashboardScreen.js:7` | getScreenByID | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 293 | POST | `/api/dashboards` | `dashboardScreen.js:10` | createScreen | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 294 | PUT | `/api/dashboards/{id}` | `dashboardScreen.js:13` | updateScreen | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 295 | DELETE | `/api/dashboards/{id}` | `dashboardScreen.js:16` | deleteScreen | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 296 | GET | `/api/dashboards/public/{code}` | `dashboardScreen.js:19` | publicViewScreen | 公开 | 无附加入参 | 未测 | 未测 | — |
| 297 | GET | `/api/dashboards/data` | `dashboardScreen.js:23` | getDashboardData | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200 KPI/趋势聚合。证据297-off-dash-data.json |
| 298 | GET | `/api/dashboards/activities` | `dashboardScreen.js:26` | getRealtimeActivities | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参 | 通过 | 通过 | 200实时活动流。证据298-off-activities.json |
| 299 | GET | `/api/dashboards/data` | `dashboardScreen.js:29` | getKPIs | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参（同#297聚合行） | 通过 | 通过 | 同#297同handler。证据297-off-dash-data.json |
| 300 | GET | `/api/dashboards/data` | `dashboardScreen.js:32` | getTrends | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参（同#297聚合行） | 通过 | 通过 | 同#297。证据297-off-dash-data.json |
| 301 | GET | `/api/dashboards/data` | `dashboardScreen.js:35` | getChannels | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参（同#297聚合行） | 通过 | 通过 | 同#297。证据297-off-dash-data.json |
| 302 | GET | `/api/dashboards/data` | `dashboardScreen.js:38` | getRegions | 用户端 Cookie / Bearer；权限以后端注册为准 | 无参（同#297聚合行） | 通过 | 通过 | 同#297。证据297-off-dash-data.json |
| 303 | POST | `/api/memory/messages` | `dialogueMemory.js:5` | appendMessage | 用户端 Cookie / Bearer；权限以后端注册为准 | body session_id/role/content | 通过 | 通过 | 200追加成功。证据303-off-memory-append.json |
| 304 | GET | `/api/memory/short` | `dialogueMemory.js:9` | getShortTerm | 用户端 Cookie / Bearer；权限以后端注册为准 | query session_id必填 | 通过 | 通过 | 缺参→400；合法→200空列表。证据304-off-memory-short.json |
| 305 | GET | `/api/memory/long` | `dialogueMemory.js:13` | getLongTerm | 用户端 Cookie / Bearer；权限以后端注册为准 | query session_id必填 | 通过 | 通过 | 200长期记忆行（key_facts结构）。证据305-off-memory-long.json |
| 306 | POST | `/api/memory/facts` | `dialogueMemory.js:17` | updateKeyFacts | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 307 | POST | `/api/memory/objections` | `dialogueMemory.js:21` | recordObjection | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 308 | POST | `/api/memory/purchase-intent` | `dialogueMemory.js:25` | updatePurchaseIntent | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 309 | POST | `/api/memory/intent-trail` | `dialogueMemory.js:29` | recordIntent | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 310 | POST | `/api/memory/sop-history` | `dialogueMemory.js:33` | recordSOP | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 311 | GET | `/api/memory/context` | `dialogueMemory.js:37` | buildContext | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 312 | GET | `/api/memory/list` | `dialogueMemory.js:41` | list | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 313 | GET | `/api/dingtalk-app/accounts` | `dingtalkApp.js:6` | listDingtalkApp | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 314 | GET | `/api/dingtalk-app/accounts/{id}` | `dingtalkApp.js:10` | getDingtalkApp | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 315 | POST | `/api/dingtalk-app/accounts` | `dingtalkApp.js:14` | createDingtalkApp | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 316 | PUT | `/api/dingtalk-app/accounts/{id}` | `dingtalkApp.js:18` | updateDingtalkApp | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 317 | DELETE | `/api/dingtalk-app/accounts/{id}` | `dingtalkApp.js:22` | deleteDingtalkApp | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 318 | POST | `/api/dingtalk-app/accounts/{id}/test` | `dingtalkApp.js:26` | testDingtalkApp | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 319 | GET | `/api/domain-pool/health` | `domainPool.js:3` | getDomainHealth | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 320 | GET | `/api/domain-pool/{domain}/blacklist` | `domainPool.js:4` | checkBlacklist | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 321 | POST | `/api/domain-pool/{id}/suspend` | `domainPool.js:5` | suspendDomain | 用户端 Cookie / Bearer；权限以后端注册为准 | {} | 未测 | 未测 | — |
| 322 | POST | `/api/domain-pool/{id}/rotate` | `domainPool.js:6` | rotateToBackup | 用户端 Cookie / Bearer；权限以后端注册为准 | {} | 未测 | 未测 | — |
| 323 | GET | `/api/domain-pool/alerts` | `domainPool.js:7` | listAlerts | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 324 | POST | `/api/domain-pool/alerts/{id}/resolve` | `domainPool.js:8` | resolveAlert | 用户端 Cookie / Bearer；权限以后端注册为准 | {} | 未测 | 未测 | — |
| 325 | GET | `/api/domain-pool` | `domainPool.js:11` | getList | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 326 | POST | `/api/domain-pool` | `domainPool.js:12` | create | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 327 | PUT | `/api/domain-pool/{data.id}` | `domainPool.js:13` | update | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 328 | DELETE | `/api/domain-pool/{id}` | `domainPool.js:14` | delete | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 329 | POST | `/api/domain-pool/{id}/check` | `domainPool.js:15` | checkDomain | 用户端 Cookie / Bearer；权限以后端注册为准 | {} | 未测 | 未测 | — |
| 330 | POST | `/api/domain-pool/check-all` | `domainPool.js:16` | checkAllDomains | 用户端 Cookie / Bearer；权限以后端注册为准 | {} | 未测 | 未测 | — |
| 331 | GET | `/api/douyin/list` | `douyinCard.js:4` | getDouyinCardList | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 332 | GET | `/api/douyin/{id}` | `douyinCard.js:8` | getDouyinCard | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 333 | POST | `/api/douyin/create` | `douyinCard.js:12` | createDouyinCard | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 334 | PUT | `/api/douyin/update` | `douyinCard.js:16` | updateDouyinCard | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 335 | DELETE | `/api/douyin/delete/{id}` | `douyinCard.js:20` | deleteDouyinCard | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 336 | GET | `/api/douyin/view/{id}` | `douyinCard.js:24` | viewDouyinCard | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 337 | GET | `/api/douyin/stats/card/{id}` | `douyinCard.js:28` | getDouyinCardStats | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 338 | GET | `/api/douyin/stats/overall` | `douyinCard.js:32` | getDouyinCardOverallStats | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 339 | POST | `/api/douyin/{id}/generate-short-link` | `douyinCard.js:36` | generateShortLink | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 340 | GET | `/api/egolite/flows` | `egolite.js:12` | listEgoFlows | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 341 | GET | `/api/egolite/flows/{id}` | `egolite.js:14` | getEgoFlow | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 342 | POST | `/api/egolite/flows?id={id}` | `egolite.js:18` | saveEgoFlow | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 343 | POST | `/api/egolite/flows` | `egolite.js:18` | saveEgoFlow | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 344 | POST | `/api/egolite/flows/{id}/rescan` | `egolite.js:20` | rescanEgoFlow | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 345 | POST | `/api/egolite/flows/{id}/publish` | `egolite.js:22` | publishEgoFlow | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 346 | POST | `/api/egolite/flows/{id}/submit` | `egolite.js:24` | submitEgoFlow | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 347 | POST | `/api/egolite/flows/{id}/dry-run` | `egolite.js:27` | dryRunEgoFlow | 用户端 Cookie / Bearer；权限以后端注册为准 | data ; { _silent: true } | 未测 | 未测 | — |
| 348 | DELETE | `/api/egolite/flows/{id}` | `egolite.js:29` | deleteEgoFlow | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 349 | GET | `/api/egolite/flows/{id}/cron` | `egolite.js:32` | getEgoFlowCron | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 350 | POST | `/api/egolite/flows/{id}/cron` | `egolite.js:33` | upsertEgoFlowCron | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 351 | DELETE | `/api/egolite/flows/{id}/cron` | `egolite.js:34` | deleteEgoFlowCron | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 352 | GET | `/api/egolite/executions` | `egolite.js:38` | listEgoExecutions | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 353 | GET | `/api/egolite/executions/{id}` | `egolite.js:40` | getEgoExecution | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 354 | GET | `/api/egolite/executions/{id}/events` | `egolite.js:43` | listEgoEvents | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 355 | GET | `{base}/api/egolite/executions/{id}/stream{afterId ? `?after_id=${afterId}` : ''}` | `egolite.js:60` | resp | 用户端 Cookie / Bearer；权限以后端注册为准 | { headers: token ? { Authorization: `Bearer ${token}` } : {}, signal: controller.signal } | 未测 | 未测 | — |
| 356 | POST | `/api/egolite/executions` | `egolite.js:112` | startEgoExecution | 用户端 Cookie / Bearer；权限以后端注册为准 | data ; { _silent: true } | 未测 | 未测 | — |
| 357 | POST | `/api/egolite/executions/{id}/abort` | `egolite.js:114` | abortEgoExecution | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 358 | POST | `/api/egolite/executions/{id}/redispatch` | `egolite.js:118` | redispatchEgoExecution | 用户端 Cookie / Bearer；权限以后端注册为准 | {} ; { _silent: true } | 未测 | 未测 | — |
| 359 | POST | `/api/egolite/executions/{id}/confirm` | `egolite.js:121` | confirmEgoExecution | 用户端 Cookie / Bearer；权限以后端注册为准 | { allow } | 未测 | 未测 | — |
| 360 | GET | `/api/egolite/runner/status` | `egolite.js:125` | getEgoRunnerStatus | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 361 | GET | `/api/egolite/runner/tokens` | `egolite.js:128` | listEgoRunnerTokens | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 362 | POST | `/api/egolite/runner/tokens` | `egolite.js:131` | issueEgoRunnerToken | 用户端 Cookie / Bearer；权限以后端注册为准 | { label } | 未测 | 未测 | — |
| 363 | POST | `/api/egolite/runner/tokens/{id}/revoke` | `egolite.js:134` | revokeEgoRunnerToken | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 364 | GET | `/api/egolite/assets` | `egolite.js:139` | listEgoAssets | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 365 | GET | `/api/egolite/admin/assets` | `egolite.js:142` | listEgoAdminAssets | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 366 | GET | `/api/egolite/admin/flows` | `egolite.js:144` | listEgoAdminFlows | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 367 | POST | `/api/egolite/admin/assets/publish` | `egolite.js:147` | publishEgoAssetFromFlow | 用户端 Cookie / Bearer；权限以后端注册为准 | { flow_id: flowId } | 未测 | 未测 | — |
| 368 | POST | `/api/egolite/admin/assets/pack` | `egolite.js:149` | publishEgoAssetPack | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 369 | POST | `/api/egolite/admin/assets/{id}/offline` | `egolite.js:151` | offlineEgoAsset | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 370 | GET | `/api/egolite/sessions` | `egolite.js:155` | listEgoSessions | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 371 | GET | `/api/egolite/sessions/{id}` | `egolite.js:157` | getEgoSession | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 372 | POST | `/api/egolite/sessions` | `egolite.js:159` | createEgoSession | 用户端 Cookie / Bearer；权限以后端注册为准 | data ; { _silent: true } | 未测 | 未测 | — |
| 373 | POST | `/api/egolite/sessions/{id}/round` | `egolite.js:162` | runEgoSessionRound | 用户端 Cookie / Bearer；权限以后端注册为准 | data ; { _silent: true } | 未测 | 未测 | — |
| 374 | POST | `/api/egolite/sessions/{id}/observe` | `egolite.js:165` | observeEgoSession | 用户端 Cookie / Bearer；权限以后端注册为准 | { round } | 未测 | 未测 | — |
| 375 | GET | `/api/egolite/sessions/{id}/rounds` | `egolite.js:168` | listEgoSessionRounds | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 376 | POST | `/api/egolite/sessions/{id}/confirm` | `egolite.js:171` | confirmEgoSession | 用户端 Cookie / Bearer；权限以后端注册为准 | { allow } | 未测 | 未测 | — |
| 377 | POST | `/api/egolite/sessions/{id}/abort` | `egolite.js:174` | abortEgoSession | 用户端 Cookie / Bearer；权限以后端注册为准 | { reason } | 未测 | 未测 | — |
| 378 | POST | `/api/egolite/sessions/{id}/finish` | `egolite.js:177` | finishEgoSession | 用户端 Cookie / Bearer；权限以后端注册为准 | { keep } | 未测 | 未测 | — |
| 379 | GET | `/api/egolite/admin/telemetry/summary` | `egolite.js:183` | getEgoTelemetrySummary | 用户端 Cookie / Bearer；权限以后端注册为准 | { hours } | 未测 | 未测 | — |
| 380 | GET | `/api/egolite/sessions/{id}/export` | `egolite.js:187` | exportEgoSessionAudit | 用户端 Cookie / Bearer；权限以后端注册为准 | { format } ; { responseType: format === 'csv' ? 'blob' : undefined } | 未测 | 未测 | — |
| 381 | POST | `/api/egolite/sessions/{id}/save-as-flow` | `egolite.js:191` | saveEgoSessionAsFlow | 用户端 Cookie / Bearer；权限以后端注册为准 | { name } | 未测 | 未测 | — |
| 382 | GET | `/api/email/smtp` | `email.js:5` | getEmailSmtpList | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 383 | POST | `/api/email/smtp` | `email.js:9` | addEmailSmtp | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 384 | PUT | `/api/email/smtp/{id}` | `email.js:13` | updateEmailSmtp | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 385 | DELETE | `/api/email/smtp/{id}` | `email.js:17` | deleteEmailSmtp | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 386 | GET | `/api/email/drafts` | `email.js:21` | getDrafts | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 387 | GET | `/api/email/drafts/{id}` | `email.js:25` | getDraftDetail | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 388 | POST | `/api/email/drafts` | `email.js:29` | createDraft | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 389 | PUT | `/api/email/drafts/{id}` | `email.js:33` | updateDraft | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 390 | DELETE | `/api/email/drafts/{id}` | `email.js:37` | deleteDraft | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 391 | POST | `/api/upload` | `email.js:41` | uploadImage | 用户端 Cookie / Bearer；权限以后端注册为准 | formData ; { headers: { 'Content-Type': 'multipart/form-data' } } | 未测 | 未测 | — |
| 392 | POST | `/api/email/send` | `email.js:49` | sendEmail | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 393 | GET | `/api/email/list?page={page}&limit={limit}` | `email.js:52` | getEmailList | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 394 | GET | `/api/email/jobs?page={page}&limit={limit}` | `email.js:56` | getJobsList | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 395 | DELETE | `/api/email/jobs/{id}` | `email.js:59` | deleteJob | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 396 | GET | `/api/email/jobs/{id}` | `email.js:62` | getJobDetail | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 397 | GET | `/api/email/lists/{id}/tracking` | `email.js:66` | getEmailTrace | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 398 | DELETE | `/api/email/list/{id}` | `email.js:70` | deleteEmailList | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 399 | GET | `/api/faqs` | `faq.js:5` | list | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 400 | GET | `/api/faqs/{id}` | `faq.js:9` | get | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 401 | POST | `/api/faqs` | `faq.js:13` | create | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 402 | PUT | `/api/faqs/{id}` | `faq.js:17` | update | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 403 | PATCH | `/api/faqs/{id}/toggle` | `faq.js:21` | toggle | 用户端 Cookie / Bearer；权限以后端注册为准 | { enabled } | 未测 | 未测 | — |
| 404 | DELETE | `/api/faqs/{id}` | `faq.js:25` | remove | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 405 | POST | `/api/faqs/match` | `faq.js:29` | match | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 406 | GET | `/api/faqs/stats` | `faq.js:33` | stats | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 407 | GET | `/api/feature-flags` | `featureFlags.js:15` | list | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 408 | GET | `/api/feature-flags/stale` | `featureFlags.js:21` | stale | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 409 | GET | `/api/feature-flags/{id}` | `featureFlags.js:25` | detail | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 410 | POST | `/api/feature-flags` | `featureFlags.js:29` | create | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 411 | PUT | `/api/feature-flags/{id}` | `featureFlags.js:33` | update | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 412 | DELETE | `/api/feature-flags/{id}` | `featureFlags.js:37` | remove | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 413 | POST | `/api/feature-flags/{id}/enable` | `featureFlags.js:44` | enable | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 414 | POST | `/api/feature-flags/{id}/disable` | `featureFlags.js:48` | disable | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 415 | POST | `/api/feature-flags/{id}/rollout` | `featureFlags.js:54` | setRollout | 用户端 Cookie / Bearer；权限以后端注册为准 | { percentage } | 未测 | 未测 | — |
| 416 | GET | `/api/feature-flags/{id}/audit` | `featureFlags.js:58` | auditLogs | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 417 | GET | `/api/feature-flags/{id}/eval-log` | `featureFlags.js:62` | evalLogs | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 418 | GET | `/api/feishu/accounts` | `feishu.js:4` | listAccounts | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 419 | GET | `/api/feishu/accounts/{id}` | `feishu.js:8` | getAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 420 | POST | `/api/feishu/accounts` | `feishu.js:12` | createAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 421 | PUT | `/api/feishu/accounts/{id}` | `feishu.js:16` | updateAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 422 | DELETE | `/api/feishu/accounts/{id}` | `feishu.js:20` | deleteAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 423 | POST | `/api/feishu/accounts/{id}/test-send` | `feishu.js:24` | testSend | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 424 | POST | `/api/feishu/accounts/{id}/refresh-token` | `feishu.js:28` | refreshToken | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 425 | GET | `/api/followups/today` | `followup.js:28` | today | 用户端 Cookie / Bearer；权限以后端注册为准 | params ; SILENT | 未测 | 未测 | — |
| 426 | GET | `/api/followups/pending` | `followup.js:35` | pending | 用户端 Cookie / Bearer；权限以后端注册为准 | params ; SILENT | 未测 | 未测 | — |
| 427 | GET | `/api/followups/overdue` | `followup.js:39` | overdue | 用户端 Cookie / Bearer；权限以后端注册为准 | { owner_id: ownerId } ; SILENT | 未测 | 未测 | — |
| 428 | POST | `/api/followups/{id}/complete` | `followup.js:46` | complete | 用户端 Cookie / Bearer；权限以后端注册为准 | { result: result &#124;&#124; '', note: note &#124;&#124; '' } | 未测 | 未测 | — |
| 429 | POST | `/api/followups/{id}/cancel` | `followup.js:50` | cancel | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 430 | POST | `/api/geo/keywords/mine` | `geo.js:5` | mineKeywords | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 431 | POST | `/api/geo/keywords/expand` | `geo.js:9` | semanticExpand | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 432 | POST | `/api/geo/keywords/cluster` | `geo.js:13` | topicCluster | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 433 | GET | `/api/geo/keywords/list` | `geo.js:17` | getKeywordList | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 434 | POST | `/api/geo/keyword-mining/crawl-suggest` | `geo.js:22` | crawlSuggest | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 435 | POST | `/api/geo/keyword-mining/longtail` | `geo.js:26` | combineLongtail | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 436 | GET | `/api/geo/keyword-mining/funnel` | `geo.js:30` | getKeywordFunnel | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 437 | POST | `/api/geo/push/urls` | `geo.js:35` | pushUrls | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 438 | GET | `/api/geo/push/quota` | `geo.js:39` | getPushQuota | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 439 | GET | `/api/geo/push/sitemap` | `geo.js:43` | getSitemapPreview | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 440 | POST | `/api/geo/index-tracking/verify/{articleId}` | `geo.js:48` | verifyIndexFull | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 441 | GET | `/api/geo/index-tracking/funnel` | `geo.js:52` | getIndexFunnel | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 442 | POST | `/api/geo/index-tracking/verify-all` | `geo.js:56` | verifyAllIndex | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 443 | GET | `/api/geo/citations/drift` | `geo.js:61` | getCitationDrift | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 444 | GET | `/api/geo/citations/domains` | `geo.js:65` | getCitationDomains | 用户端 Cookie / Bearer；权限以后端注册为准 | { days } | 未测 | 未测 | — |
| 445 | GET | `/api/geo/citations/matrix` | `geo.js:69` | getCitationMatrix | 用户端 Cookie / Bearer；权限以后端注册为准 | { days, top_queries: topQueries, top_domains: topDomains } | 未测 | 未测 | — |
| 446 | GET | `/api/geo/citations/matrix` | `geo.js:74` | downloadCitationCSV | 用户端 Cookie / Bearer；权限以后端注册为准 | { ...params, format: 'csv' } ; { responseType: 'blob' } | 未测 | 未测 | — |
| 447 | GET | `/api/geo/citations/domains` | `geo.js:74` | downloadCitationCSV | 用户端 Cookie / Bearer；权限以后端注册为准 | { ...params, format: 'csv' } ; { responseType: 'blob' } | 未测 | 未测 | — |
| 448 | POST | `/api/geo/site/export` | `geo.js:79` | exportSite | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 449 | POST | `/api/geo/site/deploy` | `geo.js:83` | deploySite | 用户端 Cookie / Bearer；权限以后端注册为准 | undefined ; { _silent: true } | 未测 | 未测 | — |
| 450 | POST | `/api/geo/site/full-pipeline` | `geo.js:87` | siteFullPipeline | 用户端 Cookie / Bearer；权限以后端注册为准 | undefined ; { _silent: true } | 未测 | 未测 | — |
| 451 | GET | `/api/geo/site/llms-txt-preview` | `geo.js:91` | getLlmsTxtPreview | 用户端 Cookie / Bearer；权限以后端注册为准 | domain ? { domain } : {} | 未测 | 未测 | — |
| 452 | GET | `/api/geo/site/robots-preview` | `geo.js:95` | getRobotsTxtPreview | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 453 | GET | `/api/geo/sites` | `geo.js:100` | listSites | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 454 | POST | `/api/geo/sites` | `geo.js:104` | createSite | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 455 | PUT | `/api/geo/sites/{id}` | `geo.js:108` | updateSite | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 456 | DELETE | `/api/geo/sites/{id}` | `geo.js:112` | deleteSite | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 457 | GET | `/api/geo/pushers` | `geo.js:117` | listPushers | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 458 | POST | `/api/geo/pushers` | `geo.js:121` | createPusher | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 459 | PUT | `/api/geo/pushers/{id}` | `geo.js:125` | updatePusher | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 460 | DELETE | `/api/geo/pushers/{id}` | `geo.js:129` | deletePusher | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 461 | GET | `/api/geo/schema-templates` | `geo.js:134` | listSchemaTemplates | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 462 | POST | `/api/geo/schema-templates` | `geo.js:138` | createSchemaTemplate | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 463 | PUT | `/api/geo/schema-templates/{id}` | `geo.js:142` | updateSchemaTemplate | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 464 | DELETE | `/api/geo/schema-templates/{id}` | `geo.js:146` | deleteSchemaTemplate | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 465 | DELETE | `/api/geo/keywords/{id}` | `geo.js:150` | deleteKeyword | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 466 | POST | `/api/geo/content/generate` | `geo.js:154` | generateContent | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 467 | POST | `/api/geo/content/batch-generate` | `geo.js:159` | batchGenerateContent | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 468 | POST | `/api/geo/content/score` | `geo.js:163` | scoreContent | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 469 | POST | `/api/geo/content/optimize` | `geo.js:167` | optimizeContent | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 470 | POST | `/api/geo/content/eeat` | `geo.js:171` | enhanceEEAT | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 471 | POST | `/api/geo/content/schema` | `geo.js:175` | generateSchema | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 472 | POST | `/api/geo/content/uniqueness` | `geo.js:179` | checkUniqueness | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 473 | GET | `/api/geo/content/list` | `geo.js:183` | getArticleList | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 474 | GET | `/api/geo/content/{id}` | `geo.js:187` | getArticleByID | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 475 | POST | `/api/geo/verification/verify` | `geo.js:191` | verifyArticle | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 476 | POST | `/api/geo/verification/negative` | `geo.js:195` | monitorNegative | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 477 | GET | `/api/geo/verification/results/{articleId}` | `geo.js:199` | getVerifyResults | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 478 | GET | `/api/geo/reports/summary` | `geo.js:203` | getReport | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 479 | GET | `/api/geo/reports/roi` | `geo.js:207` | getROI | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 480 | GET | `/api/geo/reports/api-costs` | `geo.js:211` | getAPICosts | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 481 | GET | `/api/geo/reports/content-format` | `geo.js:215` | getContentFormatAttribution | 用户端 Cookie / Bearer；权限以后端注册为准 | { days } | 未测 | 未测 | — |
| 482 | GET | `/api/geo/config` | `geo.js:219` | getConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 483 | PUT | `/api/geo/config` | `geo.js:223` | updateConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 484 | POST | `/api/geo/config/optimize` | `geo.js:227` | optimizeConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 485 | GET | `/api/geo/alerts` | `geoAlert.js:4` | listGeoAlerts | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 486 | GET | `/api/geo/alerts/unread-count` | `geoAlert.js:10` | getGeoAlertsUnreadCount | 用户端 Cookie / Bearer；权限以后端注册为准 | undefined ; { _silent: true } | 未测 | 未测 | — |
| 487 | POST | `/api/geo/alerts/{id}/ack` | `geoAlert.js:13` | ackGeoAlert | 用户端 Cookie / Bearer；权限以后端注册为准 | {} | 未测 | 未测 | — |
| 488 | DELETE | `/api/geo/alerts/{id}` | `geoAlert.js:16` | deleteGeoAlert | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 489 | POST | `/api/geo/audit/site` | `geoAudit.js:4` | runSiteAudit | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 490 | POST | `/api/geo/audit/content` | `geoAudit.js:6` | runContentAudit | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 491 | GET | `/api/geo/audit/runs` | `geoAudit.js:8` | listAuditRuns | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 492 | GET | `/api/geo/audit/runs/{id}` | `geoAudit.js:10` | getAuditRun | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 493 | GET | `/api/geo/audit/compare` | `geoAudit.js:13` | compareAudits | 用户端 Cookie / Bearer；权限以后端注册为准 | { from, to } | 未测 | 未测 | — |
| 494 | GET | `/api/geo/audit/runs/{id}/report` | `geoAudit.js:17` | fetchAuditReport | 用户端 Cookie / Bearer；权限以后端注册为准 | { format } ; { responseType: 'blob' } | 未测 | 未测 | — |
| 495 | POST | `/api/geo/autopilot/runs` | `geoAutopilot.js:6` | createRun | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 496 | GET | `/api/geo/autopilot/runs/{id}` | `geoAutopilot.js:10` | getRun | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 497 | GET | `/api/geo/autopilot/runs` | `geoAutopilot.js:14` | listRuns | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 498 | POST | `/api/geo/capture/links` | `geoCapture.js:3` | createCaptureLink | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 499 | GET | `/api/geo/capture/links` | `geoCapture.js:5` | listCaptureLinks | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 500 | GET | `/api/geo/capture/links/{id}` | `geoCapture.js:7` | getCaptureLink | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 501 | DELETE | `/api/geo/capture/links/{id}` | `geoCapture.js:9` | deleteCaptureLink | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 502 | GET | `/api/geo/decision/report` | `geoDecision.js:3` | getGeoDecisionReport | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 503 | GET | `/api/geo/decision/tasks` | `geoDecision.js:6` | getGeoGapTasks | 用户端 Cookie / Bearer；权限以后端注册为准 | { limit } | 未测 | 未测 | — |
| 504 | POST | `/api/geo/decision/tasks/{id}/done` | `geoDecision.js:8` | markGeoTaskDone | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 505 | GET | `/api/geo/entity/list` | `geoEntity.js:5` | listEntities | 用户端 Cookie / Bearer；权限以后端注册为准 | { type: type &#124;&#124; '', search: keyword &#124;&#124; '', page: page &#124;&#124; 1, limit: limit &#124;&#124; 20 } | 未测 | 未测 | — |
| 506 | GET | `/api/geo/entity/{entityId}/graph` | `geoEntity.js:13` | getEntityRelations | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 507 | POST | `/api/geo/entities/extract` | `geoEntity.js:16` | extractEntities | 用户端 Cookie / Bearer；权限以后端注册为准 | { doc_id: docId } | 未测 | 未测 | — |
| 508 | GET | `/api/geo/kb/documents` | `geoEntity.js:19` | listKBDocuments | 用户端 Cookie / Bearer；权限以后端注册为准 | { keyword: keyword &#124;&#124; '', source_level: sourceLevel &#124;&#124; '' } | 未测 | 未测 | — |
| 509 | POST | `/api/geo/kb/documents` | `geoEntity.js:22` | saveKBDocument | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 510 | DELETE | `/api/geo/kb/documents/{id}` | `geoEntity.js:25` | deleteKBDocument | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 511 | GET | `/api/geo/kb/documents/{id}` | `geoEntity.js:28` | getKBDocument | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 512 | GET | `/api/geo/kb/search` | `geoEntity.js:31` | searchKB | 用户端 Cookie / Bearer；权限以后端注册为准 | { q, limit } | 未测 | 未测 | — |
| 513 | POST | `/api/geo/kb/ask` | `geoEntity.js:34` | askKB | 用户端 Cookie / Bearer；权限以后端注册为准 | { question } | 未测 | 未测 | — |
| 514 | GET | `/api/geo/workflow/workflows` | `geoEntity.js:37` | listWorkflows | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 515 | GET | `/api/geo/workflow/workflows/{id}` | `geoEntity.js:40` | getWorkflow | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 516 | POST | `/api/geo/workflow/workflows` | `geoEntity.js:43` | saveWorkflow | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 517 | PUT | `/api/geo/workflow/workflows/{id}` | `geoEntity.js:46` | updateWorkflow | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 518 | DELETE | `/api/geo/workflow/workflows/{id}` | `geoEntity.js:49` | deleteWorkflow | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 519 | POST | `/api/geo/workflow/workflows/{id}/run` | `geoEntity.js:52` | runWorkflow | 用户端 Cookie / Bearer；权限以后端注册为准 | params &#124;&#124; {} | 未测 | 未测 | — |
| 520 | GET | `/api/geo/workflow/workflows/{workflowId}/executions` | `geoEntity.js:55` | listWorkflowExecutions | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 521 | GET | `/api/geo/workflow/templates` | `geoEntity.js:58` | listWorkflowTemplates | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 522 | POST | `/api/geo/workflow/templates` | `geoEntity.js:61` | saveWorkflowTemplate | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 523 | GET | `/api/geo/platform/platforms` | `geoPlatform.js:4` | listPlatforms | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 524 | GET | `/api/geo/platform/accounts` | `geoPlatform.js:7` | listPlatformAccounts | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 525 | POST | `/api/geo/platform/accounts` | `geoPlatform.js:10` | savePlatformAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 526 | DELETE | `/api/geo/platform/accounts/{id}` | `geoPlatform.js:13` | deletePlatformAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 527 | POST | `/api/geo/platform/publish` | `geoPlatform.js:16` | publishToPlatform | 用户端 Cookie / Bearer；权限以后端注册为准 | { article_id: articleId, platform, ...opts } | 未测 | 未测 | — |
| 528 | GET | `/api/geo/platform/records` | `geoPlatform.js:19` | listPlatformPublishRecords | 用户端 Cookie / Bearer；权限以后端注册为准 | { article_id: articleId &#124;&#124; '' } | 未测 | 未测 | — |
| 529 | GET | `/api/geo/probe/engines` | `geoProbe.js:4` | listProbeEngines | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 530 | GET | `/api/geo/probe/engines/meta` | `geoProbe.js:8` | listProbeEngineMeta | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 531 | POST | `/api/geo/probe/import` | `geoProbe.js:11` | importProbeRuns | 用户端 Cookie / Bearer；权限以后端注册为准 | { rows } | 未测 | 未测 | — |
| 532 | POST | `/api/geo/probe/test` | `geoProbe.js:14` | testEngineProbe | 用户端 Cookie / Bearer；权限以后端注册为准 | { engine, query } | 未测 | 未测 | — |
| 533 | GET | `/api/geo/probe/runs` | `geoProbe.js:17` | listProbeRuns | 用户端 Cookie / Bearer；权限以后端注册为准 | { engine, limit } | 未测 | 未测 | — |
| 534 | POST | `/api/geo/probe/run-negative` | `geoProbe.js:20` | runNegativeMonitor | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 535 | POST | `/api/geo/probe/run-source-sync` | `geoProbe.js:23` | runSourceCatalogSync | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 536 | POST | `/api/geo/probe/all` | `geoProbe.js:26` | probeAllEngines | 用户端 Cookie / Bearer；权限以后端注册为准 | { engines, query } | 未测 | 未测 | — |
| 537 | GET | `/api/geo/sov` | `geoProbe.js:29` | getSOV | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 538 | GET | `/api/geo/crawler-stats` | `geoProbe.js:32` | getCrawlerStats | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 539 | POST | `/api/geo/crawler/run` | `geoProbe.js:35` | runCrawler | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 540 | POST | `/api/geo/inaccurate-claims` | `geoProbe.js:38` | detectInaccurateClaims | 用户端 Cookie / Bearer；权限以后端注册为准 | { brand_name: brandName } | 未测 | 未测 | — |
| 541 | GET | `/api/geo/source-catalog/levels` | `geoProbe.js:41` | lookupSourceLevel | 用户端 Cookie / Bearer；权限以后端注册为准 | { url } | 未测 | 未测 | — |
| 542 | GET | `/api/geo/competitors` | `geoProbe.js:44` | listCompetitors | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 543 | GET | `/api/geo/competitors/{id}` | `geoProbe.js:47` | getCompetitor | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 544 | POST | `/api/geo/competitors` | `geoProbe.js:50` | createCompetitor | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 545 | PUT | `/api/geo/competitors/{id}` | `geoProbe.js:53` | updateCompetitor | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 546 | DELETE | `/api/geo/competitors/{id}` | `geoProbe.js:56` | deleteCompetitor | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 547 | GET | `/api/geo/visibility/engine-compare` | `geoProbe.js:59` | getEngineCompare | 用户端 Cookie / Bearer；权限以后端注册为准 | { days, intent: intent &#124;&#124; '' } | 未测 | 未测 | — |
| 548 | GET | `/api/geo/sov/trend` | `geoProbe.js:62` | getSOVTrend | 用户端 Cookie / Bearer；权限以后端注册为准 | { days, intent: intent &#124;&#124; '' } | 未测 | 未测 | — |
| 549 | POST | `/api/geo/search-console/import` | `geoSearchConsole.js:4` | importSearchConsole | 用户端 Cookie / Bearer；权限以后端注册为准 | payload | 未测 | 未测 | — |
| 550 | GET | `/api/geo/reports/dual-engine` | `geoSearchConsole.js:7` | getDualEngineReport | 用户端 Cookie / Bearer；权限以后端注册为准 | { days } | 未测 | 未测 | — |
| 551 | POST | `/api/glossaries` | `glossary.js:4` | createGlossary | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 552 | GET | `/api/glossaries` | `glossary.js:8` | listGlossaries | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 553 | GET | `/api/glossaries/{termId}` | `glossary.js:12` | getGlossary | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 554 | PUT | `/api/glossaries/{termId}` | `glossary.js:16` | updateGlossary | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 555 | DELETE | `/api/glossaries/{termId}` | `glossary.js:20` | deleteGlossary | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 556 | POST | `/api/glossaries/validate` | `glossary.js:24` | validateGlossary | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 557 | GET | `/api/human-tasks` | `humanTask.js:43` | list | 用户端 Cookie / Bearer；权限以后端注册为准 | params ; { paramsSerializer: toQuery } | 未测 | 未测 | — |
| 558 | GET | `/api/human-tasks/counts` | `humanTask.js:49` | counts | 用户端 Cookie / Bearer；权限以后端注册为准 | undefined ; { _silent: true } | 未测 | 未测 | — |
| 559 | GET | `/api/human-tasks/{id}` | `humanTask.js:53` | get | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 560 | POST | `/api/human-tasks/{id}/claim` | `humanTask.js:58` | claim | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 561 | POST | `/api/human-tasks/{id}/release` | `humanTask.js:63` | release | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 562 | POST | `/api/human-tasks/{id}/complete` | `humanTask.js:68` | complete | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 563 | POST | `/api/human-tasks/{id}/cancel` | `humanTask.js:73` | cancel | 用户端 Cookie / Bearer；权限以后端注册为准 | { reason } | 未测 | 未测 | — |
| 564 | GET | `/api/i18n/stats` | `i18nStats.js:4` | getI18nStats | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 565 | GET | `/api/i18n/stats/lang-dist` | `i18nStats.js:8` | getLangDistribution | 用户端 Cookie / Bearer；权限以后端注册为准 | { params: { days } } | 未测 | 未测 | — |
| 566 | GET | `/api/i18n/stats/cache` | `i18nStats.js:12` | getCacheHitRate | 用户端 Cookie / Bearer；权限以后端注册为准 | { params: { days } } | 未测 | 未测 | — |
| 567 | GET | `/api/i18n/stats/glossary` | `i18nStats.js:16` | getGlossaryCoverage | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 568 | GET | `/api/i18n/stats/quality` | `i18nStats.js:20` | getQualityTrend | 用户端 Cookie / Bearer；权限以后端注册为准 | { params: { days } } | 未测 | 未测 | — |
| 569 | GET | `/api/i18n/stats/latency` | `i18nStats.js:24` | getLatencyStats | 用户端 Cookie / Bearer；权限以后端注册为准 | { params: { days } } | 未测 | 未测 | — |
| 570 | GET | `/api/inbox/conversations` | `inbox.js:7` | listConversations | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 571 | GET | `/api/inbox/conversations/{id}` | `inbox.js:12` | getConversation | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 572 | GET | `/api/inbox/conversations/{id}/messages` | `inbox.js:17` | getMessages | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 573 | POST | `/api/inbox/conversations/{id}/read` | `inbox.js:22` | markRead | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 574 | POST | `/api/inbox/conversations/{id}/pin` | `inbox.js:27` | setPin | 用户端 Cookie / Bearer；权限以后端注册为准 | { pinned } | 未测 | 未测 | — |
| 575 | POST | `/api/inbox/conversations/{id}/star` | `inbox.js:31` | setStar | 用户端 Cookie / Bearer；权限以后端注册为准 | { starred } | 未测 | 未测 | — |
| 576 | POST | `/api/inbox/conversations/{id}/mute` | `inbox.js:35` | setMute | 用户端 Cookie / Bearer；权限以后端注册为准 | { muted } | 未测 | 未测 | — |
| 577 | POST | `/api/inbox/conversations/{id}/tags` | `inbox.js:40` | addTag | 用户端 Cookie / Bearer；权限以后端注册为准 | { tag } | 未测 | 未测 | — |
| 578 | DELETE | `/api/inbox/conversations/{id}/tags/{tag}` | `inbox.js:44` | removeTag | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 579 | DELETE | `/api/inbox/conversations/{id}/messages/{mid}?source={source}` | `inbox.js:49` | deleteMessage | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 580 | POST | `/api/inbox/assign` | `inbox.js:54` | assign | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 581 | POST | `/api/inbox/assign/auto` | `inbox.js:59` | autoAssign | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 582 | GET | `/api/inbox/assignments` | `inbox.js:64` | listAssignments | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 583 | GET | `/api/inbox/stats` | `inbox.js:69` | getStats | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 584 | POST | `/api/inbox/reconcile?mode={mode}` | `inbox.js:74` | reconcile | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 585 | GET | `/api/inbox/staff/{staff}/load` | `inbox.js:79` | staffLoad | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 586 | GET | `/api/inbox/agents` | `inbox.js:84` | listAgents | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 587 | GET | `/api/integrations` | `integration.js:4` | getIntegrationAccountList | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 588 | GET | `/api/integrations/{id}` | `integration.js:7` | getIntegrationAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 589 | POST | `/api/integrations` | `integration.js:10` | createIntegrationAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 590 | PUT | `/api/integrations/{id}` | `integration.js:13` | updateIntegrationAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 591 | DELETE | `/api/integrations/{id}` | `integration.js:16` | deleteIntegrationAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 592 | POST | `/api/integrations/{id}/sync-customers` | `integration.js:19` | syncCustomers | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 593 | POST | `/api/integrations/{id}/sync-products` | `integration.js:22` | syncProducts | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 594 | GET | `/api/integration/sync-logs` | `integration.js:25` | getSyncLogs | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 595 | GET | `/api/integration/external-customers` | `integration.js:28` | getExternalCustomers | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 596 | GET | `/api/integration/external-orders` | `integration.js:31` | getExternalOrders | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 597 | GET | `/api/integration/external-orders-by-customer` | `integration.js:34` | getExternalOrdersByCustomer | 用户端 Cookie / Bearer；权限以后端注册为准 | { params: { phone, name } } | 未测 | 未测 | — |
| 598 | GET | `/api/integration/external-products` | `integration.js:37` | getExternalProducts | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 599 | POST | `/api/integrations/{id}/test` | `integration.js:56` | testIntegration | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 600 | POST | `/api/intent/recognize` | `intentRecognition.js:5` | recognize | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 601 | POST | `/api/intent/recognize/batch` | `intentRecognition.js:9` | batchRecognize | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 602 | GET | `/api/intent/stats` | `intentRecognition.js:13` | getStats | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 603 | GET | `/api/intent/recent` | `intentRecognition.js:17` | getRecent | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 604 | GET | `/api/intent/dict` | `intentRecognition.js:21` | getDict | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 605 | POST | `/api/intent/recognize/fine` | `intentRecognition.js:25` | recognizeFine | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 606 | GET | `/api/intent/logs` | `intentRecognition.js:29` | getLogs | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 607 | GET | `/api/intent/stats/fine` | `intentRecognition.js:33` | getStatsFine | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 608 | GET | `/api/intent/config` | `intentRecognition.js:37` | getConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 609 | PUT | `/api/intent/config` | `intentRecognition.js:41` | updateConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 610 | GET | `/api/intent-records/keywords-override` | `intentRecognition.js:45` | getKeywordOverride | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 611 | PUT | `/api/intent-records/keywords-override` | `intentRecognition.js:49` | updateKeywordOverride | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 612 | POST | `/api/intent/feedback/correct` | `intentRecognition.js:54` | correctIntent | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 613 | GET | `/api/intent/feedback/override` | `intentRecognition.js:58` | getIntentOverride | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 614 | GET | `/api/intent/tool-chains` | `intentToolChain.js:6` | listIntentToolChains | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 615 | POST | `/api/intent/tool-chains` | `intentToolChain.js:10` | createIntentToolChain | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 616 | PUT | `/api/intent/tool-chains/{id}` | `intentToolChain.js:14` | updateIntentToolChain | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 617 | DELETE | `/api/intent/tool-chains/{id}` | `intentToolChain.js:18` | deleteIntentToolChain | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 618 | GET | `/api/intent/tool-chains/vocab` | `intentToolChain.js:23` | getIntentToolChainVocab | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 619 | GET | `/api/intent/custom` | `intentToolChain.js:28` | listCustomIntents | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 620 | POST | `/api/intent/custom` | `intentToolChain.js:32` | createCustomIntent | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 621 | PUT | `/api/intent/custom/{id}` | `intentToolChain.js:36` | updateCustomIntent | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 622 | DELETE | `/api/intent/custom/{id}` | `intentToolChain.js:40` | deleteCustomIntent | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 623 | GET | `/api/kb-connectors` | `kbConnector.js:5` | list | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 624 | POST | `/api/kb-connectors` | `kbConnector.js:6` | create | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 625 | PUT | `/api/kb-connectors/{id}` | `kbConnector.js:7` | update | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 626 | DELETE | `/api/kb-connectors/{id}` | `kbConnector.js:8` | remove | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 627 | POST | `/api/kb-connectors/{id}/sync` | `kbConnector.js:9` | sync | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 628 | GET | `/api/capabilities` | `kbConnector.js:14` | get | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 629 | GET | `/api/kb/crawler/sources` | `kbCrawler.js:7` | listSources | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 630 | POST | `/api/kb/crawler/sources` | `kbCrawler.js:11` | createSource | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 631 | PUT | `/api/kb/crawler/sources/{id}` | `kbCrawler.js:14` | updateSource | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 632 | POST | `/api/kb/crawler/sources/{id}/pause` | `kbCrawler.js:17` | pauseSource | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 633 | POST | `/api/kb/crawler/sources/{id}/resume` | `kbCrawler.js:20` | resumeSource | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 634 | POST | `/api/kb/crawler/sources/{id}/verify` | `kbCrawler.js:23` | verifySite | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 635 | DELETE | `/api/kb/crawler/sources/{id}` | `kbCrawler.js:26` | deleteSource | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 636 | POST | `/api/kb/crawler/sources/{id}/sync` | `kbCrawler.js:30` | syncSource | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 637 | GET | `/api/kb/crawler/batches` | `kbCrawler.js:34` | listBatches | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 638 | POST | `/api/kb/crawler/batches/{id}/retry` | `kbCrawler.js:37` | retryBatch | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 639 | GET | `/api/kb/qa/candidates` | `kbCrawler.js:44` | list | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 640 | POST | `/api/kb/qa/candidates/{id}/approve` | `kbCrawler.js:47` | approve | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 641 | POST | `/api/kb/qa/candidates/{id}/reject` | `kbCrawler.js:50` | reject | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 642 | POST | `/api/kb/qa/candidates/{id}/publish` | `kbCrawler.js:53` | publish | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 643 | POST | `/api/knowledge/import/upload` | `knowledge.js:5` | importUpload | 用户端 Cookie / Bearer；权限以后端注册为准 | formData ; { headers: { 'Content-Type': 'multipart/form-data' } } | 未测 | 未测 | — |
| 644 | POST | `/api/knowledge/import/text` | `knowledge.js:10` | importText | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 645 | POST | `/api/knowledge/import/url` | `knowledge.js:13` | importURL | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 646 | GET | `/api/knowledge/documents` | `knowledge.js:17` | listDocuments | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 647 | GET | `/api/knowledge/documents/{id}` | `knowledge.js:20` | getDocument | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 648 | GET | `/api/knowledge/documents/{id}/progress` | `knowledge.js:23` | getDocumentProgress | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 649 | GET | `/api/knowledge/documents/{id}/chunks` | `knowledge.js:26` | getDocumentChunks | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 650 | PUT | `/api/knowledge/documents/{id}` | `knowledge.js:29` | updateDocument | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 651 | DELETE | `/api/knowledge/documents/{id}` | `knowledge.js:32` | deleteDocument | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 652 | POST | `/api/knowledge/documents/{id}/reindex` | `knowledge.js:35` | reindexDocument | 用户端 Cookie / Bearer；权限以后端注册为准 | null ; { params } | 未测 | 未测 | — |
| 653 | POST | `/api/knowledge/products/{productId}/rebuild-index` | `knowledge.js:39` | rebuildProductIndex | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 654 | GET | `/api/knowledge/products/{productId}/overview` | `knowledge.js:42` | getProductOverview | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 655 | POST | `/api/knowledge/search` | `knowledge.js:46` | search | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 656 | GET | `/api/knowledge/import-logs` | `knowledge.js:50` | listImportLogs | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 657 | GET | `/api/knowledge/openapi/sources` | `knowledge.js:54` | listOpenAPISources | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 658 | POST | `/api/knowledge/openapi/sources` | `knowledge.js:57` | createOpenAPISource | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 659 | GET | `/api/knowledge/openapi/sources/{id}` | `knowledge.js:60` | getOpenAPISource | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 660 | PUT | `/api/knowledge/openapi/sources/{id}` | `knowledge.js:63` | updateOpenAPISource | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 661 | DELETE | `/api/knowledge/openapi/sources/{id}` | `knowledge.js:66` | deleteOpenAPISource | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 662 | POST | `/api/knowledge/openapi/sources/{id}/sync` | `knowledge.js:69` | syncOpenAPISource | 用户端 Cookie / Bearer；权限以后端注册为准 | null ; { params } | 未测 | 未测 | — |
| 663 | POST | `/api/knowledge/openapi/sources/{id}/test` | `knowledge.js:72` | testOpenAPISource | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 664 | POST | `/api/knowledge/openapi/sources/{id}/toggle` | `knowledge.js:75` | toggleOpenAPISource | 用户端 Cookie / Bearer；权限以后端注册为准 | data ; { params } | 未测 | 未测 | — |
| 665 | GET | `/api/knowledge/stats/overview` | `knowledge.js:79` | getOverviewStats | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 666 | GET | `/api/knowledge/stats/documents` | `knowledge.js:82` | getDocumentStats | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 667 | GET | `/api/knowledge/stats/searches` | `knowledge.js:85` | getSearchStats | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 668 | GET | `/api/knowledge/stats/imports` | `knowledge.js:88` | getImportStats | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 669 | GET | `/api/knowledge/stats/openapi` | `knowledge.js:91` | getOpenAPIStats | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 670 | GET | `/api/knowledge-bases` | `knowledgeBase.js:4` | listKBs | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 671 | GET | `/api/knowledge-bases/by-type/{kbType}` | `knowledgeBase.js:8` | listByType | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 672 | GET | `/api/knowledge-bases/{id}` | `knowledgeBase.js:12` | getKB | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 673 | POST | `/api/knowledge-bases` | `knowledgeBase.js:16` | createKB | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 674 | PUT | `/api/knowledge-bases/{id}` | `knowledgeBase.js:20` | updateKB | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 675 | DELETE | `/api/knowledge-bases/{id}` | `knowledgeBase.js:24` | deleteKB | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 676 | GET | `/api/knowledge-bases/{id}/stats` | `knowledgeBase.js:28` | getKBStats | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 677 | POST | `/api/knowledge-merchant/batch/import` | `knowledgeMerchant.js:5` | batchImport | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 678 | POST | `/api/knowledge-merchant/batch/upload` | `knowledgeMerchant.js:8` | batchUpload | 用户端 Cookie / Bearer；权限以后端注册为准 | formData ; { headers: { 'Content-Type': 'multipart/form-data' } } | 未测 | 未测 | — |
| 679 | POST | `/api/knowledge-merchant/playground` | `knowledgeMerchant.js:14` | playground | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 680 | GET | `/api/knowledge-merchant/documents/{documentId}/chunks` | `knowledgeMerchant.js:18` | listDocumentChunks | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 681 | PUT | `/api/knowledge-merchant/chunks/{chunkId}` | `knowledgeMerchant.js:21` | updateChunk | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 682 | DELETE | `/api/knowledge-merchant/chunks/{chunkId}` | `knowledgeMerchant.js:24` | deleteChunk | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 683 | POST | `/api/knowledge-merchant/chunks/{chunkId}/split` | `knowledgeMerchant.js:27` | splitChunk | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 684 | POST | `/api/knowledge-merchant/feedback` | `knowledgeMerchant.js:31` | submitFeedback | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 685 | GET | `/api/knowledge-merchant/feedbacks` | `knowledgeMerchant.js:34` | listFeedbacks | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 686 | POST | `/api/knowledge-merchant/tokens` | `knowledgeMerchant.js:38` | createToken | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 687 | GET | `/api/knowledge-merchant/tokens` | `knowledgeMerchant.js:41` | listTokens | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 688 | POST | `/api/knowledge-merchant/tokens/{tokenId}/revoke` | `knowledgeMerchant.js:44` | revokeToken | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 689 | POST | `/api/knowledge-merchant/external/import` | `knowledgeMerchant.js:48` | externalImport | 用户端 Cookie / Bearer；权限以后端注册为准 | data ; { headers: token ? { 'X-Knowledge-Token': token } : {} } | 未测 | 未测 | — |
| 690 | GET | `/api/knowledge-merchant/external/jobs` | `knowledgeMerchant.js:53` | listExternalJobs | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 691 | GET | `/api/kuaishou/list` | `kuaishouCard.js:4` | getKuaishouCardList | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 692 | GET | `/api/kuaishou/{id}` | `kuaishouCard.js:8` | getKuaishouCard | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 693 | POST | `/api/kuaishou/create` | `kuaishouCard.js:12` | createKuaishouCard | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 694 | PUT | `/api/kuaishou/update` | `kuaishouCard.js:16` | updateKuaishouCard | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 695 | DELETE | `/api/kuaishou/delete/{id}` | `kuaishouCard.js:20` | deleteKuaishouCard | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 696 | GET | `/api/kuaishou/view/{id}` | `kuaishouCard.js:24` | viewKuaishouCard | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 697 | POST | `/api/kuaishou/like/{id}` | `kuaishouCard.js:28` | likeKuaishouCard | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 698 | POST | `/api/kuaishou/share/{id}` | `kuaishouCard.js:32` | shareKuaishouCard | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 699 | GET | `/api/kuaishou/stats/card/{id}` | `kuaishouCard.js:36` | getKuaishouCardStats | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 700 | GET | `/api/kuaishou/stats/overall` | `kuaishouCard.js:40` | getKuaishouCardOverallStats | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 701 | POST | `/api/kuaishou/{id}/generate-short-link` | `kuaishouCard.js:44` | generateShortLink | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 702 | GET | `/api/lead-mining/config` | `leadMining.js:4` | getLeadMiningConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 703 | POST | `/api/lead-mining/config` | `leadMining.js:8` | saveLeadMiningConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 704 | GET | `/api/live-codes/list` | `livecode.js:4` | getLiveCodes | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 705 | GET | `/api/live-codes/{id}` | `livecode.js:8` | getLiveCode | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 706 | POST | `/api/live-codes/create` | `livecode.js:12` | createLiveCode | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 707 | PUT | `/api/live-codes/{id}/update` | `livecode.js:16` | updateLiveCode | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 708 | DELETE | `/api/live-codes/{id}/delete` | `livecode.js:20` | deleteLiveCode | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 709 | GET | `/api/live-codes/{id}/stats` | `livecode.js:24` | getLiveCodeStats | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 710 | GET | `/api/live-codes/{liveCodeId}/qrcodes` | `livecode.js:28` | getLiveCodeQRs | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 711 | POST | `/api/live-codes/{liveCodeId}/qrcodes/create` | `livecode.js:32` | generateLiveCodeQR | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 712 | GET | `/api/live-codes/qrcodes/{qrId}/stats` | `livecode.js:36` | getLiveCodeQRStats | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 713 | POST | `/api/live-codes/{id}/share` | `livecode.js:40` | shareLiveCode | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 714 | DELETE | `/api/live-codes/qrcodes/{id}/delete` | `livecode.js:44` | deleteLiveCodeQR | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 715 | PUT | `/api/live-codes/qrcodes/{id}/update` | `livecode.js:48` | updateLiveCodeQR | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 716 | GET | `/api/reports/llm/overview` | `llmReport.js:7` | getOverview | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 717 | GET | `/api/reports/llm/timeseries` | `llmReport.js:11` | getTimeseries | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 718 | GET | `/api/reports/llm/breakdown` | `llmReport.js:15` | getBreakdown | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 719 | GET | `/api/reports/llm/errors` | `llmReport.js:19` | getErrors | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 720 | GET | `/api/llm/usage/daily` | `llmRouting.js:7` | getDailyUsage | 用户端 Cookie / Bearer；权限以后端注册为准 | { window, group_by: groupBy } | 未测 | 未测 | — |
| 721 | GET | `/api/llm/models` | `llmRouting.js:11` | getModelList | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 722 | GET | `/api/llm/models/{name}` | `llmRouting.js:15` | getModelDetail | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 723 | POST | `/api/llm/models` | `llmRouting.js:19` | createModel | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 724 | PUT | `/api/llm/models/{name}` | `llmRouting.js:23` | updateModel | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 725 | DELETE | `/api/llm/models/{name}` | `llmRouting.js:27` | deleteModel | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 726 | PUT | `/api/llm/models/{name}` | `llmRouting.js:31` | updateModelStatus | 用户端 Cookie / Bearer；权限以后端注册为准 | { enabled: status } | 未测 | 未测 | — |
| 727 | POST | `/api/llm/models/{name}/test` | `llmRouting.js:35` | testModel | 用户端 Cookie / Bearer；权限以后端注册为准 | body | 未测 | 未测 | — |
| 728 | GET | `/api/llm/scene-routing` | `llmRouting.js:39` | getSceneRouting | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 729 | GET | `/api/llm/scenarios` | `llmRouting.js:43` | getScenarios | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 730 | PUT | `/api/llm/scene-routing` | `llmRouting.js:48` | saveSceneRouting | 用户端 Cookie / Bearer；权限以后端注册为准 | payload | 未测 | 未测 | — |
| 731 | GET | `/api/llm/fallback` | `llmRouting.js:52` | getFallbackStrategy | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 732 | GET | `/api/llm/audit` | `llmRouting.js:58` | getAuditHistory | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 733 | GET | `/api/llm/stats` | `llmRouting.js:62` | getStats | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 734 | GET | `/api/llm/usage` | `llmRouting.js:66` | getUsage | 用户端 Cookie / Bearer；权限以后端注册为准 | { window } | 未测 | 未测 | — |
| 735 | GET | `/api/llm/platform-usage` | `llmRouting.js:72` | getPlatformUsage | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 736 | GET | `/api/llm/cost-stats` | `llmRouting.js:76` | getCostStats | 用户端 Cookie / Bearer；权限以后端注册为准 | { window } | 未测 | 未测 | — |
| 737 | GET | `/api/llm/health` | `llmRouting.js:80` | getHealth | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 738 | GET | `/api/llm/model-type-stats` | `llmRouting.js:84` | getModelTypeStats | 用户端 Cookie / Bearer；权限以后端注册为准 | { window } | 未测 | 未测 | — |
| 739 | GET | `/api/llm/egress-audit` | `llmRouting.js:88` | getEgressAudit | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 740 | GET | `/api/llm/egress-alerts` | `llmRouting.js:92` | getEgressAlerts | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 741 | GET | `/api/manage/ltc/config` | `ltcConfig.js:11` | getLTCConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 742 | PUT | `/api/manage/ltc/config` | `ltcConfig.js:21` | saveLTCConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | cfg | 未测 | 未测 | — |
| 743 | GET | `/api/ltc-rates` | `ltcRates.js:5` | getRates | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 744 | GET | `/api/marketing-flows` | `marketingFlow.js:4` | getMarketingFlowList | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 745 | GET | `/api/marketing-flows/{id}` | `marketingFlow.js:7` | getMarketingFlow | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 746 | POST | `/api/marketing-flows` | `marketingFlow.js:10` | createMarketingFlow | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 747 | PUT | `/api/marketing-flows/{id}` | `marketingFlow.js:13` | updateMarketingFlow | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 748 | DELETE | `/api/marketing-flows/{id}` | `marketingFlow.js:16` | deleteMarketingFlow | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 749 | POST | `/api/marketing-flows/{id}/activate` | `marketingFlow.js:19` | activateFlow | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 750 | POST | `/api/marketing-flows/{id}/pause` | `marketingFlow.js:22` | pauseMarketingFlow | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 751 | POST | `/api/marketing-flows/{id}/stop` | `marketingFlow.js:25` | stopMarketingFlow | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 752 | GET | `/api/marketing-flows/{id}/executions` | `marketingFlow.js:28` | getFlowExecutions | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 753 | GET | `/api/marketing-flows/{id}/stats` | `marketingFlow.js:31` | getFlowStats | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 754 | POST | `/api/marketing-flows/{id}/trigger` | `marketingFlow.js:48` | triggerFlow | 用户端 Cookie / Bearer；权限以后端注册为准 | {} | 未测 | 未测 | — |
| 755 | GET | `/api/material/list` | `material.js:4` | getMaterialList | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 756 | POST | `/api/material/upload` | `material.js:8` | uploadMaterial | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 757 | DELETE | `/api/material/{id}` | `material.js:12` | deleteMaterial | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 758 | GET | `/api/material/categories` | `material.js:16` | getMaterialCategories | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 759 | POST | `/api/material/categories` | `material.js:20` | createMaterialCategory | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 760 | PUT | `/api/material/categories/{id}` | `material.js:24` | updateMaterialCategory | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 761 | DELETE | `/api/material/categories/{id}` | `material.js:28` | deleteMaterialCategory | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 762 | GET | `/api/material/selector` | `material.js:32` | getMaterialSelector | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 763 | POST | `/api/material/{id}/usage` | `material.js:36` | updateMaterialUsage | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 764 | GET | `/api/material/stats` | `material.js:40` | getMaterialStats | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 765 | GET | `/api/mcp/credentials` | `mcpCredential.js:5` | list | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 766 | POST | `/api/mcp/credentials` | `mcpCredential.js:6` | create | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 767 | PUT | `/api/mcp/credentials/{id}` | `mcpCredential.js:7` | update | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 768 | DELETE | `/api/mcp/credentials/{id}` | `mcpCredential.js:8` | remove | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 769 | GET | `/api/menus` | `menu.js:12` | getUserMenus | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 770 | GET | `/api/system/menus` | `menu.js:17` | getAllMenus | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 771 | GET | `/api/system/roles/{code}/menus` | `menu.js:22` | getRoleMenuKeys | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 772 | PUT | `/api/system/roles/{code}/menus` | `menu.js:27` | updateRoleMenuKeys | 用户端 Cookie / Bearer；权限以后端注册为准 | { menu_keys: menuKeys } | 未测 | 未测 | — |
| 773 | POST | `/api/message-hub/push` | `messageHub.js:5` | pushMessage | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 774 | POST | `/api/message-hub/push-batch` | `messageHub.js:9` | pushBatch | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 775 | POST | `/api/message-hub/push-from-channel` | `messageHub.js:13` | pushFromChannel | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 776 | GET | `/api/message-hub/list` | `messageHub.js:17` | getMessages | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 777 | GET | `/api/message-hub/{id}` | `messageHub.js:21` | getMessageById | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 778 | POST | `/api/message-hub/{ids[0]}/read` | `messageHub.js:25` | markRead | 用户端 Cookie / Bearer；权限以后端注册为准 | { ids } | 未测 | 未测 | — |
| 779 | POST | `/api/message-hub/{id}/recall` | `messageHub.js:39` | recallMessage | 用户端 Cookie / Bearer；权限以后端注册为准 | undefined ; config | 未测 | 未测 | — |
| 780 | GET | `/api/message-hub/stats` | `messageHub.js:43` | getStats | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 781 | GET | `/api/message-hub/platforms` | `messageHub.js:47` | getPlatforms | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 782 | GET | `/api/module-market/list` | `moduleMarket.js:17` | listMarketModules | 用户端 Cookie / Bearer；权限以后端注册为准 | params ; config | 未测 | 未测 | — |
| 783 | GET | `/api/module-market/detail/{key}` | `moduleMarket.js:20` | marketModuleDetail | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 784 | GET | `/api/module-market/payment-guide` | `moduleMarket.js:23` | paymentGuide | 用户端 Cookie / Bearer；权限以后端注册为准 | undefined ; config | 未测 | 未测 | — |
| 785 | GET | `/api/tenant/regulatory-notice` | `moduleMarket.js:26` | regulatoryNotice | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 786 | POST | `/api/module-market/order` | `moduleMarket.js:31` | createModuleOrder | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 787 | GET | `/api/module-market/orders` | `moduleMarket.js:36` | listModuleOrders | 用户端 Cookie / Bearer；权限以后端注册为准 | params ; config | 未测 | 未测 | — |
| 788 | GET | `/api/module-market/orders/{orderNo}` | `moduleMarket.js:39` | moduleOrderDetail | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 789 | GET | `/api/module-market/orders/{orderNo}/pay-status` | `moduleMarket.js:43` | orderPayStatus | 用户端 Cookie / Bearer；权限以后端注册为准 | undefined ; config | 未测 | 未测 | — |
| 790 | POST | `/api/module-market/orders/{orderNo}/pay` | `moduleMarket.js:48` | orderPayParams | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 791 | GET | `/api/subscription/status` | `moduleMarket.js:56` | subscriptionStatus | 用户端 Cookie / Bearer；权限以后端注册为准 | undefined ; config | 未测 | 未测 | — |
| 792 | POST | `/api/subscription/renew` | `moduleMarket.js:59` | renewSubscription | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 793 | GET | `/api/modules/my` | `moduleMarket.js:64` | myModules | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 794 | POST | `/api/modules/sync` | `moduleMarket.js:67` | syncModules | 用户端 Cookie / Bearer；权限以后端注册为准 | undefined ; config | 未测 | 未测 | — |
| 795 | GET | `/api/modules/sync-logs` | `moduleMarket.js:70` | moduleSyncLogs | 用户端 Cookie / Bearer；权限以后端注册为准 | params ; config | 未测 | 未测 | — |
| 796 | GET | `/api/llm/wallet` | `moduleMarket.js:78` | llmWallet | 用户端 Cookie / Bearer；权限以后端注册为准 | undefined ; config | 未测 | 未测 | — |
| 797 | GET | `/api/llm/plans` | `moduleMarket.js:81` | llmPlans | 用户端 Cookie / Bearer；权限以后端注册为准 | undefined ; config | 未测 | 未测 | — |
| 798 | POST | `/api/llm/recharge` | `moduleMarket.js:84` | createLLMOrder | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 799 | GET | `/api/monitor/trace-tree` | `monitor.js:5` | traceTree | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 800 | GET | `/api/monitor/traces` | `monitor.js:8` | traces | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 801 | GET | `/api/monitor/lifecycle` | `monitor.js:11` | lifecycle | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 802 | GET | `/api/monitor/health` | `monitor.js:14` | health | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 803 | GET | `/api/monitor/node-health` | `monitor.js:17` | nodeHealth | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 804 | GET | `/api/monitor/latency` | `monitor.js:20` | latency | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 805 | GET | `/api/monitor/anomalies` | `monitor.js:23` | anomalies | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 806 | GET | `/api/monitor/trace-eval/logs` | `monitor.js:26` | evalLogs | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 807 | GET | `/api/monitor/knowledge-weights` | `monitor.js:29` | knowledgeWeights | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 808 | POST | `/api/monitor/trace-eval/trigger` | `monitor.js:32` | triggerEval | 用户端 Cookie / Bearer；权限以后端注册为准 | null ; { params } | 未测 | 未测 | — |
| 809 | GET | `/api/rag/recall/snapshot` | `monitor.js:36` | ragRecallSnapshot | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 810 | GET | `/api/rag/recall/snapshots` | `monitor.js:39` | ragRecallSnapshots | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 811 | POST | `/api/rag/recall/collect` | `monitor.js:42` | ragRecallCollect | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 812 | POST | `/api/rag/recall/start` | `monitor.js:45` | ragRecallStart | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 813 | POST | `/api/rag/recall/stop` | `monitor.js:48` | ragRecallStop | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 814 | GET | `/api/rag/recall/metrics` | `monitor.js:51` | ragRecallMetrics | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 815 | GET | `/api/rag/recall/low-recall` | `monitor.js:54` | ragLowRecallQueries | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 816 | GET | `/api/auth/notifications` | `notification.js:9` | list | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 817 | POST | `/api/auth/notifications/{id}/read` | `notification.js:11` | markRead | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 818 | POST | `/api/auth/notifications/read-all` | `notification.js:13` | markAllRead | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 819 | GET | `/api/auth/notifications/unread-count` | `notification.js:15` | unreadCount | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 820 | POST | `/api/objection/handle` | `objection.js:4` | handleObjection | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 821 | POST | `/api/objection/classify` | `objection.js:8` | classifyObjection | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 822 | GET | `/api/objection/categories` | `objection.js:12` | listObjectionCategories | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 823 | POST | `/api/objection/usage` | `objection.js:16` | recordObjectionUsage | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 824 | GET | `/api/obs/config` | `obs.js:4` | getObsConfigList | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 825 | GET | `/api/obs/config/{id}` | `obs.js:8` | getObsConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 826 | POST | `/api/obs/config` | `obs.js:12` | createObsConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 827 | PUT | `/api/obs/config/{id}` | `obs.js:16` | updateObsConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 828 | DELETE | `/api/obs/config/{id}` | `obs.js:20` | deleteObsConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 829 | POST | `/api/obs/config/{id}/test` | `obs.js:24` | testObsConnection | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 830 | POST | `/api/obs/config/{id}/default` | `obs.js:28` | setDefaultObsConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 831 | GET | `/api/obs/config/default` | `obs.js:32` | getDefaultObsConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 832 | GET | `/api/observability/dashboard` | `observabilityDashboard.js:9` | getObservabilityDashboard | 用户端 Cookie / Bearer；权限以后端注册为准 | { window } | 未测 | 未测 | — |
| 833 | GET | `/api/customer/oneid/list` | `oneid.js:4` | listOneID | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 834 | GET | `/api/customer/oneid/conflicts` | `oneid.js:8` | listConflicts | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 835 | POST | `/api/customer/oneid/merge` | `oneid.js:12` | mergeOneID | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 836 | POST | `/api/customer/oneid/conflicts/{id}/resolve` | `oneid.js:16` | resolveConflict | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 837 | GET | `/api/customer-oneid/{customerId}/identities` | `oneid.js:20` | getIdentityMappings | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 838 | POST | `/api/customer-oneid/{customerId}/identities` | `oneid.js:24` | linkIdentity | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 839 | POST | `/api/customer/oneid/resolve` | `oneid.js:28` | resolveIdentity | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 840 | GET | `/api/customer/oneid/stats` | `oneid.js:32` | getOneIDStats | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 841 | GET | `/api/oneid/merge-rules` | `oneid.js:36` | getMergeRules | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 842 | POST | `/api/oneid/merge-rules` | `oneid.js:40` | saveMergeRules | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 843 | GET | `/api/operation-logs` | `operationLog.js:4` | getOperationLogs | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 844 | GET | `/api/operation-logs/{id}` | `operationLog.js:7` | getOperationLogDetail | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 845 | GET | `/api/operation-logs/statistics` | `operationLog.js:10` | getOperationLogStatistics | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 846 | GET | `/api/operation-logs/export` | `operationLog.js:13` | exportOperationLogs | 用户端 Cookie / Bearer；权限以后端注册为准 | params ; { responseType: 'blob' } | 未测 | 未测 | — |
| 847 | POST | `/api/operation-logs/clean` | `operationLog.js:18` | cleanOperationLogs | 用户端 Cookie / Bearer；权限以后端注册为准 | { before_date: beforeDate } | 未测 | 未测 | — |
| 848 | GET | `/api/manage/order-drafts` | `orderDraft.js:32` | list | 用户端 Cookie / Bearer；权限以后端注册为准 | params ; SILENT | 未测 | 未测 | — |
| 849 | GET | `/api/manage/order-drafts/{id}` | `orderDraft.js:36` | get | 用户端 Cookie / Bearer；权限以后端注册为准 | undefined ; SILENT | 未测 | 未测 | — |
| 850 | PATCH | `/api/manage/order-drafts/{id}` | `orderDraft.js:43` | edit | 用户端 Cookie / Bearer；权限以后端注册为准 | updates | 未测 | 未测 | — |
| 851 | POST | `/api/manage/order-drafts/{id}/confirm` | `orderDraft.js:50` | confirm | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 852 | POST | `/api/manage/order-drafts/{id}/cancel` | `orderDraft.js:55` | cancel | 用户端 Cookie / Bearer；权限以后端注册为准 | { reason } | 未测 | 未测 | — |
| 853 | GET | `/api/agent/order-drafts/stats` | `orderDraft.js:62` | stats | 用户端 Cookie / Bearer；权限以后端注册为准 | undefined ; SILENT | 未测 | 未测 | — |
| 854 | PUT | `/api/system/permissions/{id}/enabled` | `permission.js:4` | setUserEnabled | 用户端 Cookie / Bearer；权限以后端注册为准 | { enabled } | 未测 | 未测 | — |
| 855 | PUT | `/api/system/permissions/{id}/password` | `permission.js:8` | resetUserPassword | 用户端 Cookie / Bearer；权限以后端注册为准 | { password } | 未测 | 未测 | — |
| 856 | GET | `/api/system/permissions/audit-logs` | `permission.js:12` | listAuditLogs | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 857 | GET | `/api/analytics/persona/staffs` | `persona.js:4` | listStaffs | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 858 | GET | `/api/analytics/persona/staffs/{staffId}` | `persona.js:8` | getPersonaReport | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 859 | GET | `/api/platform-accounts` | `platformAccount.js:5` | getAccounts | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 860 | GET | `/api/platform-accounts/platforms` | `platformAccount.js:9` | getPlatforms | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 861 | GET | `/api/platform-accounts/{id}` | `platformAccount.js:13` | getAccountById | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 862 | POST | `/api/platform-accounts` | `platformAccount.js:17` | createAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 863 | PUT | `/api/platform-accounts/{id}` | `platformAccount.js:21` | updateAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 864 | DELETE | `/api/platform-accounts/{id}` | `platformAccount.js:25` | deleteAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 865 | POST | `/api/platform-accounts/{id}/login` | `platformAccount.js:29` | loginAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 866 | GET | `/api/platform-accounts/{id}/status` | `platformAccount.js:35` | checkStatus | 用户端 Cookie / Bearer；权限以后端注册为准 | undefined ; { _silent: true } | 未测 | 未测 | — |
| 867 | GET | `/api/qq/accounts` | `qqBot.js:4` | listAccounts | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 868 | GET | `/api/qq/accounts/{id}` | `qqBot.js:8` | getAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 869 | POST | `/api/qq/accounts` | `qqBot.js:12` | createAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 870 | PUT | `/api/qq/accounts/{id}` | `qqBot.js:16` | updateAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 871 | DELETE | `/api/qq/accounts/{id}` | `qqBot.js:20` | deleteAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 872 | POST | `/api/qq/accounts/{id}/test-send` | `qqBot.js:24` | testSend | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 873 | POST | `/api/qq/accounts/{id}/verify-callback` | `qqBot.js:28` | verifyCallback | 用户端 Cookie / Bearer；权限以后端注册为准 | {} | 未测 | 未测 | — |
| 874 | POST | `/api/rag-config/products` | `ragProductConfig.js:12` | createRagProduct | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 875 | GET | `/api/rag-config/products` | `ragProductConfig.js:21` | getRagProducts | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 876 | GET | `/api/rag-config/products` | `ragProductConfig.js:25` | listProducts | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 877 | PUT | `/api/rag-config/products/{id}` | `ragProductConfig.js:29` | updateRagProduct | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 878 | DELETE | `/api/rag-config/products/{id}` | `ragProductConfig.js:33` | deleteRagProduct | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 879 | GET | `/api/rag-config/accounts/config` | `ragProductConfig.js:37` | getAccountConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 880 | PUT | `/api/rag-config/accounts/config` | `ragProductConfig.js:41` | updateAccountConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 881 | POST | `/api/rag-config/process-message` | `ragProductConfig.js:45` | processMessage | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 882 | GET | `/api/reach/pipelines` | `reachPipeline.js:5` | getPipelines | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 883 | POST | `/api/reach/pipelines` | `reachPipeline.js:8` | createPipeline | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 884 | GET | `/api/reach/pipelines/{id}` | `reachPipeline.js:11` | getPipeline | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 885 | PUT | `/api/reach/pipelines/{id}` | `reachPipeline.js:14` | updatePipeline | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 886 | DELETE | `/api/reach/pipelines/{id}` | `reachPipeline.js:17` | deletePipeline | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 887 | POST | `/api/reach/pipelines/{id}/pause` | `reachPipeline.js:20` | pausePipeline | 用户端 Cookie / Bearer；权限以后端注册为准 | {} | 未测 | 未测 | — |
| 888 | POST | `/api/reach/pipelines/{id}/resume` | `reachPipeline.js:23` | resumePipeline | 用户端 Cookie / Bearer；权限以后端注册为准 | {} | 未测 | 未测 | — |
| 889 | POST | `/api/reach/pipelines/{id}/archive` | `reachPipeline.js:26` | archivePipeline | 用户端 Cookie / Bearer；权限以后端注册为准 | {} | 未测 | 未测 | — |
| 890 | GET | `/api/reach/jobs` | `reachPipeline.js:30` | getJobs | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 891 | POST | `/api/reach/jobs` | `reachPipeline.js:33` | enqueueJob | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 892 | GET | `/api/reach/jobs/{id}` | `reachPipeline.js:36` | getJob | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 893 | POST | `/api/reach/jobs/{id}/cancel` | `reachPipeline.js:39` | cancelJob | 用户端 Cookie / Bearer；权限以后端注册为准 | {} | 未测 | 未测 | — |
| 894 | POST | `/api/reach/jobs/{id}/retry` | `reachPipeline.js:42` | retryJob | 用户端 Cookie / Bearer；权限以后端注册为准 | {} | 未测 | 未测 | — |
| 895 | POST | `/api/reach/jobs/{id}/execute` | `reachPipeline.js:45` | executeJob | 用户端 Cookie / Bearer；权限以后端注册为准 | {} | 未测 | 未测 | — |
| 896 | GET | `/api/reach/stats` | `reachPipeline.js:49` | getStats | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 897 | POST | `/api/reach/rate-limit/reset` | `reachPipeline.js:52` | resetRateLimit | 用户端 Cookie / Bearer；权限以后端注册为准 | {} ; { params: { channel } } | 未测 | 未测 | — |
| 898 | GET | `/api/reach/deferred-outbound` | `reachPipeline.js:58` | getDeferredOutbounds | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 899 | POST | `/api/reach/deferred-outbound/{id}/cancel` | `reachPipeline.js:63` | cancelDeferredOutbound | 用户端 Cookie / Bearer；权限以后端注册为准 | {} ; { _silent: true } | 未测 | 未测 | — |
| 900 | GET | `/api/system/roles` | `role.js:10` | listRoles | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 901 | GET | `/api/system/roles/{code}` | `role.js:15` | getRole | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 902 | GET | `/api/system/roles/{code}/members` | `role.js:22` | listRoleMembers | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 903 | GET | `/api/config/runtime` | `runtimeConfig.js:13` | runtimeConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | { params: groups ? { groups } : {} } ; { timeout: 5000, _silent: true } | 未测 | 未测 | — |
| 904 | GET | `/api/config/dictionaries/{parent}` | `runtimeConfig.js:23` | dictionary | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 905 | GET | `/api/manage/dictionaries` | `runtimeConfig.js:28` | listParents | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 906 | GET | `/api/manage/dictionaries/{parent}` | `runtimeConfig.js:29` | get | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 907 | PUT | `/api/manage/dictionaries/{parent}/items` | `runtimeConfig.js:30` | upsertItem | 用户端 Cookie / Bearer；权限以后端注册为准 | item | 未测 | 未测 | — |
| 908 | PUT | `/api/manage/dictionaries/{parent}/transitions` | `runtimeConfig.js:32` | replaceTransitions | 用户端 Cookie / Bearer；权限以后端注册为准 | { transitions } | 未测 | 未测 | — |
| 909 | GET | `/api/sales-workbench/overview` | `salesWorkbench.js:24` | overview | 用户端 Cookie / Bearer；权限以后端注册为准 | { sales_id: salesId } | 未测 | 未测 | — |
| 910 | GET | `/api/sales-workbench/team-dashboard` | `salesWorkbench.js:30` | teamDashboard | 用户端 Cookie / Bearer；权限以后端注册为准 | { days } | 未测 | 未测 | — |
| 911 | GET | `/api/sales-workbench/champion` | `salesWorkbench.js:37` | champion | 用户端 Cookie / Bearer；权限以后端注册为准 | { days } | 未测 | 未测 | — |
| 912 | GET | `/api/sales-workbench/quick-actions` | `salesWorkbench.js:43` | quickActions | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 913 | GET | `/api/script-library/{scriptId}/versions` | `scriptAb.js:4` | listVersions | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 914 | POST | `/api/script-library/{scriptId}/versions` | `scriptAb.js:5` | createVersion | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 915 | PUT | `/api/script-library/{scriptId}/versions/{versionId}/activate` | `scriptAb.js:7` | activateVersion | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 916 | POST | `/api/script-library/{scriptId}/expire` | `scriptAb.js:8` | expireScript | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 917 | GET | `/api/script-library/{scriptId}/ab-stats` | `scriptAb.js:9` | getAbStats | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 918 | PUT | `/api/script-library/{scriptId}/ab-config` | `scriptAb.js:10` | updateAbConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 919 | POST | `/api/script-ab/conversion` | `scriptAb.js:11` | recordConversion | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 920 | GET | `/api/scripts` | `scriptTemplate.js:4` | getScriptTemplateList | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 921 | GET | `/api/scripts/{id}` | `scriptTemplate.js:7` | getScriptTemplate | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 922 | POST | `/api/scripts` | `scriptTemplate.js:10` | createScriptTemplate | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 923 | PUT | `/api/scripts/{id}` | `scriptTemplate.js:13` | updateScriptTemplate | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 924 | DELETE | `/api/scripts/{id}` | `scriptTemplate.js:16` | deleteScriptTemplate | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 925 | GET | `/api/scripts/categories` | `scriptTemplate.js:19` | getScriptCategories | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 926 | GET | `/api/scripts/search` | `scriptTemplate.js:22` | searchScriptTemplates | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 927 | GET | `/api/scripts/public` | `scriptTemplate.js:25` | getPublicScriptTemplates | 公开 | 无附加入参 | 未测 | 未测 | — |
| 928 | POST | `/api/scripts/recommend` | `scriptTemplate.js:28` | recommendScript | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 929 | POST | `/api/security/audit` | `securityAudit.js:4` | runSecurityAudit | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 930 | GET | `/api/security/audit/list` | `securityAudit.js:8` | getSecurityAuditList | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 931 | GET | `/api/security/audit/{id}` | `securityAudit.js:12` | getSecurityAuditDetail | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 932 | GET | `/api/shortlink/list` | `shortLink.js:5` | getList | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 933 | GET | `/api/shortlink/{id}` | `shortLink.js:9` | getById | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 934 | POST | `/api/shortlink/create` | `shortLink.js:13` | create | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 935 | PUT | `/api/shortlink/update` | `shortLink.js:17` | update | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 936 | DELETE | `/api/shortlink/delete/{id}` | `shortLink.js:21` | delete | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 937 | POST | `/api/shortlink/access` | `shortLink.js:25` | access | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 938 | POST | `/api/shortlink/generate` | `shortLink.js:29` | generateShortCode | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 939 | GET | `/api/shortlink/{id}/stats` | `shortLink.js:33` | getStats | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 940 | GET | `/api/shortlink/stats/all` | `shortLink.js:37` | getAllStats | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 941 | POST | `/api/shortlink/{id}/share` | `shortLink.js:41` | share | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 942 | GET | `/api/sms/config` | `sms.js:5` | getConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 943 | POST | `/api/sms/config` | `sms.js:8` | saveConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 944 | GET | `/api/sms/list` | `sms.js:12` | getSmsList | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 945 | GET | `/api/sms/detail/{id}` | `sms.js:15` | getSmsDetail | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 946 | POST | `/api/sms/resend/{id}` | `sms.js:18` | resendSms | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 947 | POST | `/api/sms/send` | `sms.js:21` | sendSms | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 948 | GET | `/api/sms/draft/list` | `sms.js:25` | getDraftList | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 949 | GET | `/api/sms/draft/{id}` | `sms.js:28` | getDraftDetail | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 950 | POST | `/api/sms/draft` | `sms.js:31` | createDraft | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 951 | PUT | `/api/sms/draft/{id}` | `sms.js:34` | updateDraft | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 952 | DELETE | `/api/sms/draft/{id}` | `sms.js:37` | deleteDraft | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 953 | POST | `/api/sms/draft/{id}/send` | `sms.js:40` | sendDraft | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 954 | GET | `/api/sms/job/list` | `sms.js:44` | getJobList | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 955 | GET | `/api/sms/job/{id}` | `sms.js:47` | getJobDetail | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 956 | POST | `/api/sms/job/{id}/pause` | `sms.js:50` | pauseJob | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 957 | POST | `/api/sms/job/{id}/resume` | `sms.js:53` | resumeJob | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 958 | POST | `/api/sms/job/{id}/stop` | `sms.js:56` | stopJob | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 959 | POST | `/api/sms/job` | `sms.js:59` | createJob | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 960 | DELETE | `/api/sms/job/{id}` | `sms.js:62` | deleteJob | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 961 | GET | `/api/sms/job/{id}/records` | `sms.js:65` | getJobRecords | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 962 | GET | `/api/sop` | `sopAgent.js:5` | list | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 963 | POST | `/api/sop` | `sopAgent.js:9` | create | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 964 | GET | `/api/sop/stats` | `sopAgent.js:13` | getStats | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 965 | GET | `/api/sop/match` | `sopAgent.js:17` | matchByIntent | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 966 | GET | `/api/sop/executions` | `sopAgent.js:21` | listExecutions | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 967 | GET | `/api/sop/executions/{id}` | `sopAgent.js:25` | getExecution | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 968 | POST | `/api/sop/executions/{id}/pause` | `sopAgent.js:29` | pauseExecution | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 969 | POST | `/api/sop/executions/{id}/resume` | `sopAgent.js:33` | resumeExecution | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 970 | POST | `/api/sop/executions/{id}/cancel` | `sopAgent.js:37` | cancelExecution | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 971 | GET | `/api/sop/{id}` | `sopAgent.js:41` | get | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 972 | PUT | `/api/sop/{id}` | `sopAgent.js:45` | update | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 973 | DELETE | `/api/sop/{id}` | `sopAgent.js:49` | remove | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 974 | POST | `/api/sop/{id}/activate` | `sopAgent.js:53` | activate | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 975 | POST | `/api/sop/{id}/deactivate` | `sopAgent.js:57` | deactivate | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 976 | POST | `/api/sop/execute` | `sopAgent.js:61` | execute | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 977 | POST | `/api/sop/step` | `sopAgent.js:65` | step | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 978 | GET | `/api/sop-templates` | `sopTemplate.js:5` | list | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 979 | GET | `/api/sop-templates/{id}` | `sopTemplate.js:9` | get | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 980 | POST | `/api/sop-templates` | `sopTemplate.js:13` | create | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 981 | PUT | `/api/sop-templates/{id}` | `sopTemplate.js:17` | update | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 982 | PATCH | `/api/sop-templates/{id}/toggle` | `sopTemplate.js:21` | toggle | 用户端 Cookie / Bearer；权限以后端注册为准 | { enabled } | 未测 | 未测 | — |
| 983 | DELETE | `/api/sop-templates/{id}` | `sopTemplate.js:25` | remove | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 984 | POST | `/api/sop-templates/match` | `sopTemplate.js:29` | match | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 985 | GET | `/api/system/info` | `stats.js:7` | getSystemInfo | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 986 | GET | `/api/system/info` | `system.js:6` | getInfo | 用户端 Cookie / Bearer；权限以后端注册为准 | config | 未测 | 未测 | — |
| 987 | GET | `/api/system/config` | `system.js:9` | getConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 988 | POST | `/api/system/config` | `system.js:12` | saveConfig | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 989 | GET | `/api/system/users` | `systemUser.js:4` | listSystemUsers | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 990 | GET | `/api/system/users/{id}` | `systemUser.js:8` | getSystemUser | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 991 | POST | `/api/system/users` | `systemUser.js:12` | createSystemUser | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 992 | PUT | `/api/system/users/{id}` | `systemUser.js:16` | updateSystemUser | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 993 | DELETE | `/api/system/users/{id}` | `systemUser.js:20` | deleteSystemUser | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 994 | GET | `/api/session-tags` | `tagSegmentation.js:5` | getTags | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 995 | PUT | `/api/session-tags/{id}` | `tagSegmentation.js:12` | updateTags | 用户端 Cookie / Bearer；权限以后端注册为准 | body | 未测 | 未测 | — |
| 996 | POST | `/api/session-tags` | `tagSegmentation.js:16` | createTag | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 997 | DELETE | `/api/session-tags/{id}` | `tagSegmentation.js:20` | deleteTag | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 998 | GET | `/api/customer-360/tag-rules` | `tagSegmentation.js:24` | getTagRules | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 999 | POST | `/api/customer-360/tag-rules` | `tagSegmentation.js:28` | saveTagRule | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1000 | PUT | `/api/customer-360/tag-rules/{id}` | `tagSegmentation.js:32` | updateTagRule | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1001 | DELETE | `/api/customer-360/tag-rules/{id}` | `tagSegmentation.js:36` | deleteTagRule | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1002 | GET | `/api/user-segment/layers` | `tagSegmentation.js:40` | getLayerStrategy | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1003 | GET | `/api/customer-360/tag-stats` | `tagSegmentation.js:44` | getTagStats | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1004 | GET | `/api/telegram/accounts` | `telegram.js:4` | listAccounts | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1005 | GET | `/api/telegram/accounts/{id}` | `telegram.js:8` | getAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1006 | POST | `/api/telegram/accounts` | `telegram.js:12` | createAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1007 | PUT | `/api/telegram/accounts/{id}` | `telegram.js:16` | updateAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1008 | DELETE | `/api/telegram/accounts/{id}` | `telegram.js:20` | deleteAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1009 | POST | `/api/telegram/accounts/{id}/register-webhook` | `telegram.js:24` | registerWebhook | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1010 | POST | `/api/telegram/accounts/{id}/test-send` | `telegram.js:28` | testSend | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1011 | GET | `/api/telegram/gates` | `telegram.js:34` | listGates | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1012 | POST | `/api/telegram/gates` | `telegram.js:38` | createGate | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1013 | PUT | `/api/telegram/gates/{id}` | `telegram.js:42` | updateGate | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1014 | DELETE | `/api/telegram/gates/{id}` | `telegram.js:46` | deleteGate | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1015 | GET | `/api/telegram/gates/{id}/members` | `telegram.js:50` | listGateMembers | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1016 | POST | `/api/telegram/gates/{id}/members/{memberId}/authorize` | `telegram.js:54` | authorizeGateMember | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1017 | GET | `/api/template-market/list` | `templateMarket.js:15` | listMarketEntries | 用户端 Cookie / Bearer；权限以后端注册为准 | params ; config | 未测 | 未测 | — |
| 1018 | GET | `/api/template-market/categories` | `templateMarket.js:18` | marketCategories | 用户端 Cookie / Bearer；权限以后端注册为准 | undefined ; config | 未测 | 未测 | — |
| 1019 | GET | `/api/template-market/detail/{id}` | `templateMarket.js:22` | marketEntryDetail | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1020 | POST | `/api/template-market/entries` | `templateMarket.js:28` | submitMarketEntry | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1021 | GET | `/api/template-market/my/submissions` | `templateMarket.js:32` | myMarketSubmissions | 用户端 Cookie / Bearer；权限以后端注册为准 | undefined ; config | 未测 | 未测 | — |
| 1022 | GET | `/api/template-market/my/orders` | `templateMarket.js:36` | myMarketOrders | 用户端 Cookie / Bearer；权限以后端注册为准 | undefined ; config | 未测 | 未测 | — |
| 1023 | GET | `/api/template-market/my/owned` | `templateMarket.js:40` | myMarketOwned | 用户端 Cookie / Bearer；权限以后端注册为准 | undefined ; config | 未测 | 未测 | — |
| 1024 | POST | `/api/template-market/entries/{entryId}/order` | `templateMarket.js:44` | orderMarketEntry | 用户端 Cookie / Bearer；权限以后端注册为准 | data &#124;&#124; {} | 未测 | 未测 | — |
| 1025 | POST | `/api/template-market/orders/{orderNo}/cancel` | `templateMarket.js:48` | cancelMarketOrder | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1026 | GET | `/api/template-market/admin/entries` | `templateMarket.js:53` | marketReviewList | 用户端 Cookie / Bearer；权限以后端注册为准 | params ; config | 未测 | 未测 | — |
| 1027 | POST | `/api/template-market/admin/entries/{entryId}/review` | `templateMarket.js:57` | marketReviewAction | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1028 | GET | `/api/template-market/admin/orders` | `templateMarket.js:60` | marketAdminOrders | 用户端 Cookie / Bearer；权限以后端注册为准 | params ; config | 未测 | 未测 | — |
| 1029 | POST | `/api/template-market/admin/orders/{orderNo}/mark-paid` | `templateMarket.js:64` | marketMarkPaid | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1030 | GET | `/api/tenant/info` | `tenant.js:8` | getInfo | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1031 | GET | `/api/public/config` | `tenant.js:15` | isMultiTenant | 公开 | undefined ; { _silent: true } | 未测 | 未测 | — |
| 1032 | GET | `/api/tiktok-card/list` | `tiktokCard.js:4` | getTikTokCardList | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1033 | GET | `/api/tiktok-card/{id}` | `tiktokCard.js:8` | getTikTokCard | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1034 | POST | `/api/tiktok-card` | `tiktokCard.js:12` | createTikTokCard | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1035 | PUT | `/api/tiktok-card/{data.id}` | `tiktokCard.js:16` | updateTikTokCard | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1036 | DELETE | `/api/tiktok-card/{id}` | `tiktokCard.js:20` | deleteTikTokCard | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1037 | POST | `/api/tiktok-card/generate-short-link` | `tiktokCard.js:24` | generateShortLink | 用户端 Cookie / Bearer；权限以后端注册为准 | { card_id: cardId } | 未测 | 未测 | — |
| 1038 | GET | `/api/tiktok-card/stats/overall` | `tiktokCard.js:28` | getTikTokCardOverallStats | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1039 | GET | `/api/tiktok-card/{cardId}/stats` | `tiktokCard.js:32` | getTikTokCardStats | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1040 | GET | `/api/admin/tuning/confidence/signals` | `tuning.js:4` | getConfidenceSignals | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1041 | GET | `/api/admin/tuning/confidence/signals/{id}` | `tuning.js:7` | getConfidenceSignal | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1042 | GET | `/api/admin/tuning/confidence/signals/stats` | `tuning.js:10` | getConfidenceSignalStats | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1043 | GET | `/api/admin/tuning/confidence/calibrations` | `tuning.js:13` | getConfidenceCalibrations | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1044 | GET | `/api/admin/tuning/confidence/policies` | `tuning.js:16` | getThresholdPolicies | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1045 | PUT | `/api/admin/tuning/confidence/policies` | `tuning.js:19` | upsertThresholdPolicy | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1046 | GET | `/api/admin/tuning/humanize/scores` | `tuning.js:23` | getHumanizeScores | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1047 | GET | `/api/admin/tuning/humanize/scores/stats` | `tuning.js:26` | getHumanizeScoreStats | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1048 | GET | `/api/admin/tuning/humanize/baselines` | `tuning.js:29` | getChampionBaselines | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1049 | GET | `/api/admin/tuning/humanize/low-quality` | `tuning.js:32` | getLowQualitySamples | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1050 | GET | `/api/admin/tuning/feedback/events` | `tuning.js:36` | getFeedbackEvents | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1051 | GET | `/api/admin/tuning/feedback/events/stats` | `tuning.js:39` | getFeedbackEventStats | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1052 | GET | `/api/admin/tuning/feedback/dialogues` | `tuning.js:42` | getChampionDialogues | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1053 | GET | `/api/admin/tuning/prompt/candidates` | `tuning.js:45` | getPromptCandidates | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1054 | PUT | `/api/admin/tuning/prompt/candidates/{id}/status` | `tuning.js:48` | updatePromptCandidateStatus | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1055 | GET | `/api/admin/tuning/bandit/arms` | `tuning.js:51` | getBanditArms | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1056 | GET | `/api/unknown-questions` | `unknownQuestion.js:7` | list | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1057 | GET | `/api/unknown-questions/stats` | `unknownQuestion.js:10` | stats | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1058 | GET | `/api/unknown-questions/{id}` | `unknownQuestion.js:13` | get | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1059 | POST | `/api/unknown-questions/{id}/submit` | `unknownQuestion.js:16` | submit | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1060 | POST | `/api/unknown-questions/{id}/review` | `unknownQuestion.js:19` | review | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1061 | GET | `/api/unknown-questions/clusters` | `unknownQuestion.js:23` | clusterList | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1062 | GET | `/api/unknown-questions/clusters/{id}` | `unknownQuestion.js:26` | clusterGet | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1063 | POST | `/api/unknown-questions/clusters/{id}/review` | `unknownQuestion.js:29` | clusterReview | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1064 | GET | `/api/user-segment/rfm/list` | `userSegment.js:4` | getRFMList | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1065 | GET | `/api/user-segment/rfm/rule` | `userSegment.js:7` | getRFMRule | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1066 | POST | `/api/user-segment/rfm/rule` | `userSegment.js:10` | saveRFMRule | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1067 | PUT | `/api/user-segment/rfm/rule/{id}` | `userSegment.js:13` | updateRFMRule | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1068 | GET | `/api/user-segment/rfm/user?user_id={userId}` | `userSegment.js:16` | getUserRFM | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1069 | GET | `/api/user-segment/rfm/stats` | `userSegment.js:19` | getRFMStats | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1070 | POST | `/api/user-segment/rfm/calculate` | `userSegment.js:22` | calculateRFM | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1071 | GET | `/api/user-segment/layers` | `userSegment.js:25` | getLayerDescription | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1072 | GET | `/api/user-segments` | `userSegment.js:29` | getUserSegments | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1073 | DELETE | `/api/user-segment/rfm/rule/{id}` | `userSegment.js:71` | deleteUserSegment | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1074 | GET | `/api/user-segment/rfm/list?segment_id={id}` | `userSegment.js:74` | getSegmentUsers | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1075 | GET | `/api/users` | `users.js:5` | list | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 1076 | GET | `/api/users/{id}` | `users.js:9` | get | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1077 | POST | `/api/users` | `users.js:13` | create | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1078 | PUT | `/api/users/{id}` | `users.js:17` | update | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1079 | DELETE | `/api/users/{id}` | `users.js:21` | delete | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1080 | PUT | `/api/users/{id}/password` | `users.js:25` | updatePassword | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1081 | POST | `/api/auth/login` | `users.js:29` | login | 公开 | data | 未测 | 未测 | — |
| 1082 | POST | `/api/auth/mfa/verify` | `users.js:34` | mfaVerify | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1083 | POST | `/api/auth/logout` | `users.js:40` | logout | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1084 | GET | `/api/wecom/health/accounts` | `wecomAccount.js:5` | listAccounts | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1085 | GET | `/api/wecom/health/accounts/risks` | `wecomAccount.js:9` | listRiskAccounts | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1086 | GET | `/api/wecom/health/accounts/select` | `wecomAccount.js:13` | selectHealthyAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1087 | GET | `/api/wecom/health/accounts/summary` | `wecomAccount.js:17` | getHealthSummary | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1088 | GET | `/api/wecom/health/accounts/{id}` | `wecomAccount.js:21` | getLatestHealth | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1089 | GET | `/api/wecom/health/accounts/{id}/history` | `wecomAccount.js:25` | listHealthHistory | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1090 | POST | `/api/wecom/health/accounts/{id}` | `wecomAccount.js:29` | reportHealth | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1091 | POST | `/api/wecom/health/accounts/{id}/status` | `wecomAccount.js:33` | updateAccountStatus | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1092 | POST | `/api/wecom/health/accounts/{id}/quota/consume` | `wecomAccount.js:37` | consumeQuota | 用户端 Cookie / Bearer；权限以后端注册为准 | { count } | 未测 | 未测 | — |
| 1093 | POST | `/api/wecom/health/accounts/quota/reset` | `wecomAccount.js:41` | resetDailyQuota | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1094 | POST | `/api/wecom/messages/ingest` | `wecomAccount.js:45` | ingestMessage | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1095 | POST | `/api/wecom/messages/send` | `wecomAccount.js:49` | sendMessage | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1096 | POST | `/api/wecom/accounts` | `wecomAccount.js:53` | createAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1097 | PUT | `/api/wecom/accounts/{id}` | `wecomAccount.js:57` | updateAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1098 | DELETE | `/api/wecom/accounts/{id}` | `wecomAccount.js:61` | deleteAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1099 | GET | `/api/wecom/customers` | `wecomAccount.js:65` | getCustomers | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 1100 | GET | `/api/wecom/groups` | `wecomAccount.js:69` | getGroups | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 1101 | GET | `/api/wecom/tags` | `wecomAccount.js:73` | getTags | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1102 | GET | `/api/wecom/messages` | `wecomAccount.js:77` | getMessages | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 1103 | POST | `/api/wecom/accounts/{id}/sync-customers` | `wecomAccount.js:81` | syncCustomers | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1104 | POST | `/api/wecom/accounts/{id}/sync-groups` | `wecomAccount.js:85` | syncGroups | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1105 | POST | `/api/wecom/accounts/{id}/sync-tags` | `wecomAccount.js:89` | syncTags | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1106 | POST | `/api/wecom/accounts/{id}/refresh` | `wecomAccount.js:93` | refreshAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1107 | POST | `/api/wecom/accounts/{id}/send-message` | `wecomAccount.js:97` | sendMessageById | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1108 | GET | `/api/whatsapp/accounts` | `whatsapp.js:3` | listAccounts | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 1109 | POST | `/api/whatsapp/accounts` | `whatsapp.js:5` | createAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1110 | POST | `/api/whatsapp/accounts/{id}/login/start` | `whatsapp.js:6` | startLogin | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1111 | GET | `/api/whatsapp/accounts/{id}/login/status` | `whatsapp.js:7` | loginStatus | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1112 | PUT | `/api/whatsapp/accounts/{id}` | `whatsapp.js:8` | updateAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1113 | DELETE | `/api/whatsapp/accounts/{id}` | `whatsapp.js:9` | deleteAccount | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1114 | GET | `/api/whatsapp/drafts` | `whatsapp.js:11` | listDrafts | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 1115 | POST | `/api/whatsapp/drafts` | `whatsapp.js:12` | createDraft | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1116 | PUT | `/api/whatsapp/drafts/{id}` | `whatsapp.js:13` | updateDraft | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1117 | DELETE | `/api/whatsapp/drafts/{id}` | `whatsapp.js:14` | deleteDraft | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1118 | GET | `/api/whatsapp/jobs` | `whatsapp.js:16` | listJobs | 用户端 Cookie / Bearer；权限以后端注册为准 | { params } | 未测 | 未测 | — |
| 1119 | POST | `/api/whatsapp/jobs` | `whatsapp.js:17` | createJob | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1120 | GET | `/api/whatsapp/jobs/{id}` | `whatsapp.js:18` | getJob | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1121 | DELETE | `/api/whatsapp/jobs/{id}` | `whatsapp.js:19` | deleteJob | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1122 | GET | `/api/whatsapp-cloud/accounts` | `whatsappCloud.js:6` | listWhatsappCloud | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1123 | GET | `/api/whatsapp-cloud/accounts/{id}` | `whatsappCloud.js:10` | getWhatsappCloud | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1124 | POST | `/api/whatsapp-cloud/accounts` | `whatsappCloud.js:14` | createWhatsappCloud | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1125 | PUT | `/api/whatsapp-cloud/accounts/{id}` | `whatsappCloud.js:18` | updateWhatsappCloud | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1126 | DELETE | `/api/whatsapp-cloud/accounts/{id}` | `whatsappCloud.js:22` | deleteWhatsappCloud | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1127 | POST | `/api/whatsapp-cloud/accounts/{id}/test-send` | `whatsappCloud.js:26` | testSendWhatsappCloud | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1128 | GET | `/api/workflows/versions` | `workflowOrchestrator.js:6` | listVersions | 用户端 Cookie / Bearer；权限以后端注册为准 | {} | 未测 | 未测 | — |
| 1129 | GET | `/api/workflows/versions` | `workflowOrchestrator.js:9` | listVersions | 用户端 Cookie / Bearer；权限以后端注册为准 | { workflow_id: workflowIdOrParams } | 未测 | 未测 | — |
| 1130 | GET | `/api/workflows/versions` | `workflowOrchestrator.js:17` | listVersions | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1131 | GET | `/api/workflows/versions/{id}` | `workflowOrchestrator.js:21` | getVersion | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1132 | POST | `/api/workflows/versions` | `workflowOrchestrator.js:25` | createVersion | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1133 | PUT | `/api/workflows/versions/{id}` | `workflowOrchestrator.js:29` | updateVersion | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1134 | DELETE | `/api/workflows/versions/{id}` | `workflowOrchestrator.js:33` | deleteVersion | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1135 | POST | `/api/workflows/versions/{id}/publish` | `workflowOrchestrator.js:37` | publishVersion | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1136 | POST | `/api/workflows/versions/{id}/archive` | `workflowOrchestrator.js:41` | archiveVersion | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1137 | POST | `/api/workflows/execute` | `workflowOrchestrator.js:45` | execute | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1138 | GET | `/api/workflows/executions` | `workflowOrchestrator.js:49` | listExecutions | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1139 | GET | `/api/workflows/executions/{id}` | `workflowOrchestrator.js:53` | getExecution | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1140 | GET | `/api/workflows/executions/{executionId}/nodes` | `workflowOrchestrator.js:57` | getNodeExecutions | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1141 | POST | `/api/workflows/executions/{id}/stop` | `workflowOrchestrator.js:61` | stopExecution | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1142 | GET | `/api/xianyu/list` | `xianyuCard.js:4` | getXianyuCardList | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1143 | GET | `/api/xianyu/{id}` | `xianyuCard.js:8` | getXianyuCard | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1144 | POST | `/api/xianyu/create` | `xianyuCard.js:12` | createXianyuCard | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1145 | PUT | `/api/xianyu/update` | `xianyuCard.js:16` | updateXianyuCard | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1146 | DELETE | `/api/xianyu/delete/{id}` | `xianyuCard.js:20` | deleteXianyuCard | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1147 | GET | `/api/xianyu/view/{id}` | `xianyuCard.js:24` | viewXianyuCard | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1148 | GET | `/api/xianyu/stats/card/{id}` | `xianyuCard.js:28` | getXianyuCardStats | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1149 | GET | `/api/xianyu/stats/overall` | `xianyuCard.js:32` | getXianyuCardOverallStats | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1150 | POST | `/api/xianyu/{id}/generate-short-link` | `xianyuCard.js:36` | generateXianyuShortLink | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1151 | GET | `/api/xiaohongshu/list` | `xiaohongshuCard.js:4` | getXiaohongshuCardList | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1152 | GET | `/api/xiaohongshu/{id}` | `xiaohongshuCard.js:8` | getXiaohongshuCard | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1153 | POST | `/api/xiaohongshu/create` | `xiaohongshuCard.js:12` | createXiaohongshuCard | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1154 | PUT | `/api/xiaohongshu/update` | `xiaohongshuCard.js:16` | updateXiaohongshuCard | 用户端 Cookie / Bearer；权限以后端注册为准 | data | 未测 | 未测 | — |
| 1155 | DELETE | `/api/xiaohongshu/delete/{id}` | `xiaohongshuCard.js:20` | deleteXiaohongshuCard | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1156 | GET | `/api/xiaohongshu/view/{id}` | `xiaohongshuCard.js:24` | viewXiaohongshuCard | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
| 1157 | GET | `/api/xiaohongshu/stats/card/{id}` | `xiaohongshuCard.js:28` | getXiaohongshuCardStats | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1158 | GET | `/api/xiaohongshu/stats/overall` | `xiaohongshuCard.js:32` | getXiaohongshuCardOverallStats | 用户端 Cookie / Bearer；权限以后端注册为准 | params | 未测 | 未测 | — |
| 1159 | POST | `/api/xiaohongshu/{id}/generate-short-link` | `xiaohongshuCard.js:36` | generateShortLink | 用户端 Cookie / Bearer；权限以后端注册为准 | 无附加入参 | 未测 | 未测 | — |
