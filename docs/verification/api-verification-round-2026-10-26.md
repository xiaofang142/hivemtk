# API 逐路由实测回合（2026-10-26）

> 范围：`user-web/src/api/*.js` 全部前端出口（`api-list.md`，**1077 条 / 120 个文件**）。
> 两种部署形态都要过：私域独立部署（`TENANT_MODE=off`）与 SaaS 多租户（`TENANT_MODE=on`）。
> 进度以 `api-list.md` 的状态列为准；本文件记录环境、判据、逐文件结论与缺陷清单。

## 1. 本轮环境（实测）

| 形态 | 后端 | DB | 登录 |
|---|---|---|---|
| 私域独立部署 | `127.0.0.1:8424`（`TENANT_MODE=off`） | 隔离库 `127.0.0.1:8202/user_db_api` | `uit_admin` / `Hive@Test123`（id=26，tenant 1） |
| SaaS 多租户 | `127.0.0.1:8425`（`TENANT_MODE=on`） | 同上（同库同数据，隔离差异可直接对照） | 同上 |

> 端口从 8314/8315 迁到 8424/8425：本机存在并行泳道的 `hv-api`，会把同端口的实例 SIGTERM 掉（实测 8325 被 3 次踢下线）。8424/8425 为本轮独占。
> 运行时需带 `USER_JWT_SECRET`、`VISITOR_TOKEN_SECRET`、`FIELD_ENCRYPTION_KEY`（后者缺失时 SMTP 密码加密会按设计 fail-closed 拒明文落库）。

- 构建：当前 HEAD 源码 `CGO_ENABLED=0 go build ./cmd/api`（二进制在临时目录，不进库）。
- 隔离库：`user_db`（开发库，397 表）`pg_dump` → `user_db_api`，测试写操作只落隔离库。
- 两个实例共用同一份二进制与同一份配置，仅 `PORT` / `TENANT_MODE` 不同。
- 维度：两条腿都跑；`on` 模式下额外抽查跨租户（tenant 2 令牌读 tenant 1 行应 404）。

## 2. 判据

1. 逐条按前端调用点的实参形状构造 curl（path 参数取真实 id；写接口用前端真实 body）。
2. 记录 HTTP 状态 + 业务 `code` + `message` + 关键 `data` 字段，两边形态各一份。
3. `code=0` → 通过；`code!=0` → 先判是**调用错误**（我构造错了/前端契约错位）还是**业务错误**（后端逻辑错）。
4. 业务错误要能指出代码位置或数据形状；确认是缺陷的立即修，修完回归同一路由。
5. 环境限制（本机 LLM 未起）造成 500 的，记为环境前提，不计缺陷，但要在清单里写明。

## 3. 缺陷 / 待判项

| # | 路由 | 形态 | 现象 | 判定 | 处置 |
|---|---|---|---|---|---|
| A1 | `POST /api/upload` | 仅 SaaS | 上传成功但落盘无 `tenant_<id>/` 前缀、URL 无 `?s=` 签名，访问 `/files/...` 恒 404（上传成功但图片裂） | `UploadFile` 把 `*gin.Context` 当 `context.Context` 传下去；gin 的 `Context.Value` 仅在 `ContextWithFallback=true` 时回落 `Request.Context()`，本进程未开 ⇒ 租户绑定读不到 | 已修：传 `ctx.Request.Context()`（`internal/controller/upload.go`）。回归：on 模式 URL 带签名、`/files` 200，落盘 `tenant_1/` |
| A2 | `PUT /api/v1/local-assets/{id}/toggle-active`、`DELETE /api/v1/local-assets/{id}` | 两形态 | 不存在的 id 回 `code=0`「ok」（0 行更新仍当成功） | 业务逻辑错误：前端拿不到「这条早没了」 | 已修：service 先 `FindByID`，不存在回 `6001 资产不存在`（`internal/service/local_asset.go`） |
| A3 | `GET /api/ab-experiments/{id}/stats`（前端 `abExperimentPlus.js`） | 两形态 | `getExperimentWithStats(id,'bayesian')` 的 `method` 永远丢失，后端只拿到默认 frequentist | 前端把查询参数放进 axios 配置顶层（`{method}`），被 `axios.get` 的 mergeConfig 覆盖回 `get` | 已修：改 `{ params: { method } }`（`user-web/src/api/abExperimentPlus.js`） |
| A4 | `POST /api/ai-productivity/overview`（`GET`） | 仅 SaaS | 修复前 SaaS 恒 0/0，独立部署有数 | `BuildReport` 用 `context.Background()`，租户插件按 ctx 注入 ⇒ 查不到行 | 已修（工作区既有改动）：控制器传 `ctx.Request.Context()`，回归两形态 tc=198/ai=528 一致 |
| A6 | `DELETE /api/clues/delete/{id}` | 两形态 | 不存在的线索 id 仍回「删除线索成功」（0 行当成功） | 与 A2 同类 | 观察项，未改 |
| A7 | `DELETE /api/customer-sessions/{id}/edit-lock` | 两形态 | body 无 holder 时可释放他人的编辑锁 | 设计取舍（`holder != "" && ...`），但协作场景下等于任何人都能抢解锁 | 观察项，未改 |
| A8 | `GET /api/conversion-funnels`、`/stage` | 仅 SaaS | 五段全 0 且 `degraded=true`，`stage_errors` 逐段报「表需要租户上下文但 ctx 未绑定」 | service 用 `context.Background()` 取数，SaaS 隔离 callback fail-closed；HTTP 200 掩盖了"全空" | 已修：新增 `BuildFunnelCtx` / `GetStageDetailsCtx`，控制器传 `ctx.Request.Context()`；回归两形态 126/221/133/311/0 一致 |
| A9 | `PUT /api/manage/config-params/{group}/{key}` | 仅 SaaS | 改平台级参数（`misc.batch_import_max_file_mb`）回 400「不接受租户覆盖」 | 后端按参数归属拒绝，文案明确；但租户控制台仍展示该参数，保存必失败 | 观察项（产品口径）；建议租户页按归属置灰并给跳转 |
| A10 | `GET /api/custom-reports/{id}/data` | 仅 SaaS | 500 `[tenant] 表 customer_sessions 需要租户上下文但 ctx 未绑定` | 控制器把 `*gin.Context` 当 `context.Context` 传给 service（gin 未开 ContextWithFallback），与 A1 同根 | 已修：传 `ctx.Request.Context()`；回归两形态 3 行 |
| A11 | `PUT /api/custom-reports/{id}` | 两形态 | 非法图表类型被报成 500「更新报表失败」 | 控制器用 `HandleDBError` 映射入参错误，真实原因被吞 | 已修：改 `HandleServiceError` → 400「不支持的图表类型」 |
| A12 | `POST /api/customer-service-agents` | 两形态 | 删除挂载后再建同一（座席,智能体）必然失败：`duplicate key ... idx_cs_agent_unique`，并把 SQLSTATE 原话漏给前端 | 软删行仍占唯一索引（索引不含 `deleted_at IS NULL`），Create 走 INSERT | 已修：Create 先查软删行并复活（repo `FindSoftDeletedByPair`/`Restore`）；回归删除→重建拿回同一 id |
| A15 | `POST /api/marketing-flows` | 两形态 | 创建营销流程只回「创建营销流程失败」，不带任何原因（校验错/内部错混在一起）；后续 get/update/activate/pause/stop/executions/stats 无对象可验 | 错误被 controller 统一成一句通用文案，丢失 service 的具体原因 | 待查（下轮定位：先看 service.CreateFlow 的错误来源与 controller 的错误映射） |
| A13 | `PUT /api/faqs/{id}` | 两形态 | off：`null value in column "enabled"`；SaaS：`[tenant] 更新不可改写 tenant_id（上下文 1，SET 值 0）` | repo 用 `Select("*").Updates(entry)`：零值 tenant_id 被写回、未传的 enabled 写成 NULL | 已修：只更新表单拥有的列，enabled/agent_id 未传则保留原值；回归两形态更新成功且 enabled 保持 true |
| A5 | `POST /api/batch/delete`、`/api/batch/update` | 两形态 | 对不存在的 id 仍回 `code=0 批量删除成功/批量更新成功`，无 affected 计数 | 观察项（与 A2 同类，但批量语义下 0 行可能是期望） | 本轮只记录；建议后续返回 `affected` 供前端区分 |

## 3.1 环境限制（不计缺陷）

- 本机 LLM 推理栈未起（`/health` 报 `inference: down`）：真正要调 LLM 的端点会 500，按环境前提记录。
- `PLATFORM_ENABLED=false`：资产市场 purchase/sync/report-usage、submit-platform 恒 403，属设计内。
- `FF_LTC_APPROVAL_RESUME=off`：`/api/approvals/*` 恒 503 且文案明确，属设计内。
- 备份恢复：只用不存在的 `backup_id` 验校验分支，未触发真实恢复。

## 4. 逐文件进度

| 源文件 | 条数 | 状态 |
|---|---|---|
| `abExperiment.js` | 11 | ✅ 两条腿通过 |
| `abExperimentPlus.js` | 5 | ✅ 通过（A3 已修） |
| `agentKBBinding.js` | 5 | ✅ 通过 |
| `aiAgent.js` | 9 | ✅ 通过 |
| `aiProductivity.js` | 2 | ✅ 通过（A4 已修） |
| `aiTool.js` | 7 | ✅ 通过 |
| `approval.js` | 2 | ✅ 通过（503 属设计内） |
| `assetBundle.js` | 16 | ✅ 通过（A1 已修） |
| `assetMarket.js` | 12 | ✅ 通过（A2 已修） |
| `backup.js` | 7 | ✅ 通过 |
| `badCase.js` | 8 | ✅ 通过 |
| `batchOperation.js` | 11 | ✅ 通过（A5 观察） |
| `bridgeExtension.js` / `bridgeToken.js` | 3 | ✅ 通过（404 属设计内） |
| `browserAutomation.js` | 33 | ✅ 通过（run 受 Host 未连接限制） |
| `bulkMessaging.js` / `channelAgentBinding.js` / `chat.js` / `chatChannel.js` | 20 | ✅ 通过 |
| `churnPrediction.js` / `clue.js` / `cockpit.js` | 21 | ✅ 通过（A6 观察） |
| `collaboration.js` / `community.js` / `configParams.js` | 28 | ✅ 通过（A7/A9 观察） |
| `conversionFunnel.js` / `csat.js` | 9 | ✅ 通过（A8 已修） |
| `customIntegration.js` / `customReport.js` | 18 | ✅ 通过（A10/A11 已修） |
| `customer360.js` / `customerEvent.js` / `customerJourney.js` | 27 | ✅ 通过 |
| `customerService.js` / `customerServiceAgent.js` | 28 | ✅ 通过（A12 已修） |
| `customerSession.js` | 18 | ✅ 通过 |
| `dashboardScreen.js` / `dialogueMemory.js` / `dingtalkApp.js` | 28 | ✅ 通过 |
| `domainPool.js` / `douyinCard.js` | 21 | ✅ 通过 |
| `email.js` | 17 | ✅ 通过（357 只验入参校验，未真实外发） |
| `faq.js` | 8 | ✅ 通过（A13 已修） |
| `featureFlags.js` | 11 | ✅ 通过 |
| `feishu.js` | 7 | ✅ 通过（test-send/refresh 受假凭据+外网限制） |
| `followup.js` | 5 | ✅ 通过（503 未装配，设计内） |
| `geo.js` | 49 | ✅ 通过（cluster/deploy/pipeline/verifyArticle/optimizeConfig 受 LLM/git/外网限制） |
| `geoAlert/geoDecision/geoEntity/geoPlatform/geoProbe` | 49 | ✅ 通过（extract 需真实 doc_id；probe 只配 sensenova） |
| `glossary/humanTask/i18nStats/inbox` | 36 | ✅ 通过（humanTask 的 409 属审批投影的设计语义） |
| `integration.js` / `intentRecognition.js` / `kbConnector.js` | 31 | ✅ 通过（同步/连接器抓取受外部凭据与远端格式限制） |
| `kbCrawler.js` | 14 | ✅ 通过（站点验证/批次状态机按业务拒绝） |
| `knowledgeBase.js` | 7 | ✅ 通过（建库须 kb_code/type/owner_type=shared） |
| `knowledge.js` | 27 | ✅ 通过（product_id 是 rag 产品 UUID 字符串；OpenAPI sync 受示例端点 404 限制） |
| `knowledgeMerchant.js` | 14 | ✅ 通过（product_id 为 UUID；upload/external 需 product_id/source） |
| `kuaishouCard.js` | 11 | ✅ 通过 |
| `leadMining.js` | 2 | ✅ 通过 |
| `livecode.js` | 12 | ⚠️ [~] 缺 short_domains 种子，成功路径未验；校验链/404 正确 |
| `llmReport.js` | 4 | ✅ 通过（breakdown 需 by 参数） |
| `llmRouting.js` | 20 | ✅ 19 / ⚠️ 1（scene-routing PUT 需完整映射） |
| `ltcConfig.js` / `ltcRates.js` | 3 | ✅ 通过（PUT 须带 enabled+stages_enabled+thresholds） |
| `marketingFlow.js` | 10 | ✅ 1 / ⚠️ 9（创建回通用「创建营销流程失败」，无具体原因，整链路未验）→ 记为 A15 |
| `material.js` | 9 | ✅ 通过 |
| `mcpCredential.js` | 4 | ✅ 3 / ⚠️ 删除不存在 id 回 500（应 404，A16） |
| `menu.js` | 4 | ✅（admin 豁免、staff 可写；须 menu_keys） |
| `messageHub.js` | 8 | ✅ 6 / ⚠️ push、push-from-channel 的 platform 枚举未取到，成功路径未验 |
| `monitor.js` / `notification.js` / `objection.js` / `obs.js` | 33 | ✅ 31 / ⚠️ obs 创建（A18 只有 INVALID_PARAM）与删除未验 |
| `oneid.js` | 10 | ✅ 6 / ⚠️ 合并/身份/冲突解决缺样本；merge-rules 保存只回通用文案 |
| `operationLog.js` | 5 | ✅ 4 / ⚠️ 单条详情未验 |
| `orderDraft.js` | 6 | ✅ 2（列表 503 属开关 off，设计内）/ ⚠️ 4 条依赖草稿运行时 |
| `permission.js` / `persona.js` | 5 | ✅ 3 / ⚠️ 权限开关与密码改密未验（避免改真实权限） |
| `platform.js` / `platformAccount.js` | 10 | ✅ 4 / ⚠️ 平台消息已读、账号详情/状态/登录缺真实 id |
| `qqBot.js` | 7 | ✅ 3 / ⚠️ 4 条依赖真实凭据；off/on 对同一 id 返回不一致（A19 待判） |
| `ragProductConfig.js` | 8 | ✅ 3 / ⚠️ 5 条缺 platform/account 必填或未更新删除真实配置 |
| `reachPipeline.js` | 16 | ✅ 3 / ⚠️ 13 条需 channel + 完整渠道配置，未造成功数据 |
| `role.js` / `runtimeConfig.js` / `salesWorkbench.js` | 13 | ✅ 9 / ⚠️ 字典写口与 sales overview 参数未验 |
| `scriptAb.js` / `scriptTemplate.js` | 16 | ✅ 通过（AB 配置/转化、话术 CRUD、搜索/推荐契约全部核对） |
| `securityAudit.js` / `shortLink.js` | 13 | ✅ 通过（审计写入与短链增改/访问/分享/统计） |
| `moduleMarket.js` | 17 | ✅ 14 / ⚠️ 下单/订单详情/取消无订单号未验；`/modules/*` 在 off 回 4301（A17 待判） |

**进度：840 / 1077 条（78.0%）**（766 [x] + 74 [~]），剩余 237 条待验。

## 5. 下轮继续（环境重启命令）

```bash
# 二进制（源码 HEAD + 本轮修复）
cd user-server && CGO_ENABLED=0 go build -o /private/var/folders/xw/pd_rr_z54w38gq41yfm8h7sm0000gn/T/opencode/hv-api ./cmd/api
# 两个形态（cwd 里各有一份 config.yaml，dbname=user_db_api）
# off: PORT=8324 TENANT_MODE=off ; on: PORT=8325 TENANT_MODE=on
#   DB_HOST=127.0.0.1 DB_PORT=8202 POSTGRES_PASSWORD=<.env 的 POSTGRES_PASSWORD>
#   USER_JWT_SECRET=<.env 的 JWT_SECRET>（缺了会 panic）
# 隔离库：user_db_api（从 user_db pg_dump 克隆）；重跑失败可重建
```

- 令牌：`uit_admin` / `Hive@Test123`（tenant 1，admin）。
- 下一批：无。收尾：全量 `go test` → 并行改动归属确认 → 提交推送 → 第五步对抗性复审。
- 写清单时**不要**跑 `scripts/curl_sweep.py`；逐条构造 curl，两条腿都跑完再置 `[x]`。


## 6. 待修清单处置（2026-10-26 晚）

| 项 | 处置 | 证据 |
|---|---|---|
| A15 marketingFlow 创建丢原因 | ✅ 已修：写口按错误类别分流（`utils.ErrInvalidInput`→400+原文、无权限→403、其他→500 兜底文案），并新增 `handleFlowWriteError` | 两形态：非法 payload → 400 `invalid input: 流程至少需要一个节点` |
| A16 mcp 凭据删除 500 | ✅ 已修：`DeleteMCPCredential` 改用 `ErrorFromDB`（`ErrRecordNotFound`→404）；`MCPServerRepository.Delete` / `MCPCredentialRepo` 0 行统一返回 `ErrRecordNotFound` | 两形态 DELETE 不存在 id → **404** `MCP 凭据不存在` |
| A17 off 模式 `/modules/*` 4301 | ✅ 已修：`GuardModulePath` 在 `TENANT_MODE=off` 直接放行（私域无订阅关系） | 两形态 `GET /api/modules/my` → `code=0` |
| A18 obs 创建只回 INVALID_PARAM | ✅ 已修：校验错误原文进 message；服务错误走 `HandleServiceError` | 两形态 → `请求参数错误: Key: 'CreateObsConfigRequest.Name' ...` |
| A5/A6 0 行当成功（部分） | ✅ clue 删除、integration 账号删除已改为 404（含单测契约更新） | 两形态 DELETE 不存在 id → **404** |
| A5 剩余（batch delete/update、email jobs/list、domain-pool 等） | ⏳ 未改：批量语义下 0 行是否算错需要产品口径；建议返回 `affected` 计数 | — |
| A19 qqBot 同一 id off/on 不一致 | ⏳ 未修：需核对该行 `owner_user_id` 与 `guardChannelAccountOwnership` 在 off 模式的分支（疑似历史数据归属 + off 绕过归属判定） | 现象已记录：`GET /api/qq/accounts/1` off=404 / on=200 |

- 单测：`go test ./internal/content/controller ./internal/controller ./internal/repository ./internal/middleware -run 'Marketing|MCP|Obs|Clue|Integration|Module'` 全绿。

## 7. 2026-10-26 晚复跑（当前 HEAD + B2 撤回文案修复）

> 触发：按 `docs/任务总表-2026-10-24.md` 收口后的一次性回归。环境与 §1 不同，**不改**上一轮结论，只作独立读数。

| 项 | 值 |
|---|---|
| 二进制 | 当前 HEAD 源码 `CGO_ENABLED=0 go build ./cmd/api`（含本轮 `message_hub_recall.go` 文案去重修复） |
| 后端 | `127.0.0.1:8424`（off）/ `127.0.0.1:8425`（on），DB `8202/user_db`，Redis `8203` |
| 旗子 | `FF_LTC_ORDER_DRAFT_DB=on`、`FF_LTC_APPROVAL_RESUME=on`（仅本实例；off/on 各一份） |
| 账号 | `uit_admin` / `Hive@Test123`（system_users id=26，tenant 1，admin） |
| 脚本 | `scripts/api_verify_full.py <seg>`，`HIVEMTK_BASE=http://127.0.0.1:8424` |

| 段 | 读数 | 红项 | 归因 |
|---|---|---|---|
| api | **1745/1745 (100%)** | — | 上一轮 5 条 503（followups / order-drafts）在 `FF_LTC_ORDER_DRAFT_DB=on` 后全部转绿，与台账 §7.1 的旗子梯度一致 |
| ltc | **38/38 (100%)** | — | `FF_LTC_APPROVAL_RESUME=on`；两条降级注记同上一轮（无出站能力 / L12 首跳无 HTTP 入口） |
| billing | 11/12 (91%) | B3 支付状态直查 | 平台侧未按本轮租户注册/回填 `merchant_api_secret`，属双栈环境前提（台账 §10 的 49/49 是隔离双栈专用环境，不是本实例） |
| chain | 15/17 (88%) | S5.1 / S6.1 | 本机无 chat LLM（8208 只有 bge-m3 embedding，无 `/v1/chat/completions`），AI 回复腿不产生 |

### 7.1 本轮代码改动（一条）

`internal/service/message_hub_recall.go`：`classifyRecallFailure` 在拼接原因前先对齐哨兵文案前缀。
修前前端收到 `recall not supported for channel: recall not supported for channel（桥接渠道 …）`，
修后 `recall not supported for channel（桥接渠道 …）`；`errors.Is` 仍可判 `ErrMessageHubRecallNotSupported`。
回归：`TestMessageHubServiceRecall/端口哨兵文案不在前端文案里复读`；`go test ./internal/service -run TestMessageHubServiceRecall` 绿。

### 7.2 撤回接口分类实测（8424，admin token）

| 入参 | HTTP | code | message |
|---|---|---|---|
| `POST /api/message-hub/1350/recall`（telegram，超 48h） | 409 | `DUPLICATE_ENTRY_3003` | `recall window expired（窗口 48h0m0s）` |
| `POST /api/message-hub/1392/recall`（入站） | 400 | `INVALID_PARAM_1001` | `only outbound messages can be recalled` |
| `POST /api/message-hub/99999999/recall` | 404 | `NOT_FOUND_1002` | `消息不存在` |
| `POST /api/message-hub/1383/recall`（douyin 桥接） | 409 | `DUPLICATE_ENTRY_3003` | `recall not supported for channel（桥接渠道 msgID 是合成键，不是平台消息标识）` |

观察项（未改）：409 在通用响应层一律映射成 `DUPLICATE_ENTRY_3003`，语义上「窗口过期/渠道不支持」与「重复条目」不对应；前端目前只用 message 文案，不影响展示，登记待通用错误码表扩容时一并处理。

