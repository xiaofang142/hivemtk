# 环境变量参考（机械生成，勿手改）

> 由 `scripts/gen_env_reference.go` 扫描 internal/ cmd/（非 _test.go）与 config.yaml 生成。
> 再生成：`go run ./scripts/gen_env_reference.go`；CI 校验：`go run ./scripts/gen_env_reference.go -check`。
> 测试基建专用键（POSTGRES_TEST_* / TESTDB_* / TEST_ADMIN_* / CI 等）不属部署配置面，未收录。
> <!-- envref-fingerprint: 187 keys -->

| 键 | 说明 | 读取点 |
|---|---|---|
| `ADMIN_CONFIG_FILE` |  | `internal\config\admin.go:61` |
| `ADMIN_EMAIL` |  | `internal\config\admin.go:91` |
| `ADMIN_REAL_NAME` |  | `internal\config\admin.go:94` |
| `ADMIN_USERNAME` |  | `internal\config\admin.go:88` |
| `AGENT_RUNTIME_BUS_ENABLED` |  | `cmd\api\main.go:781` |
| `AI_DEBOUNCE_SECONDS` |  | `internal\service\ai_debounce.go:24` |
| `ALERT_EMAIL_ENABLED` |  | `cmd\api\main.go:656` |
| `ALERT_EMAIL_RECIPIENTS` |  | `cmd\api\main.go:657` |
| `ALERT_WEBHOOK_URL` |  | `cmd\api\main.go:664<br>internal\router\service_routes.go:412` |
| `ALLOWED_WS_ORIGINS` |  | `internal\config\ws_origin.go:47` |
| `ALLOW_INSECURE_TELEGRAM_WEBHOOK` | 危险：允许 Telegram webhook 免签（dev 双护栏） | `internal\channelbot\telegram\telegram.go:549<br>internal\service\webhook.go:649` |
| `ALLOW_INSECURE_WEBHOOK` | 危险：允许 webhook 免 HTTPS（dev） | `internal\service\webhook.go:629<br>internal\service\webhook.go:649` |
| `ALLOW_SELF_RESTART` | 允许进程自重启（离线升级面） | `internal\controller\system_ops.go:59` |
| `APP_ENV` |  | `internal\pkg\db\db.go:54<br>internal\service\webhook.go:648` |
| `AUTO_LOGIN_ENABLED` |  | `internal\config\admin.go:98` |
| `BACKUP_BASE_DIR` |  | `internal\storage\backup_source.go:25` |
| `BRAIN_TOKEN_BUDGET` |  | `internal\browser_automation\service\brain_constants.go:17` |
| `BRIDGE_INGEST_AUTH` |  | `internal\middleware\bridge_ingress_guard.go:57` |
| `BRIDGE_INGEST_TOKEN` | 桥接入栈令牌；未配置 503 fail-closed | `internal\middleware\bridge_ingress_guard.go:29<br>internal\service\bridge_token.go:52` |
| `BRIDGE_INGEST_TOKEN_PREV` | 桥接令牌轮换期的旧令牌（双读） | `internal\middleware\bridge_ingress_guard.go:32` |
| `BROWSER_JEV_API_KEY` |  | `internal\browser_automation\service\jev.go:72` |
| `BROWSER_JEV_ENABLED` |  | `internal\browser_automation\service\jev.go:75` |
| `BROWSER_JEV_ENDPOINT` |  | `internal\browser_automation\service\jev.go:71` |
| `BROWSER_JEV_MODEL` |  | `internal\browser_automation\service\jev.go:73` |
| `BRUTE_FORCE_DISABLED` | 危险：置 1/true 关闭登录爆破防护（仅诊断临时用） | `internal\middleware\brute_force.go:16<br>internal\middleware\brute_force.go:16` |
| `CHANNEL_WS_ALLOWED_ORIGINS` |  | `internal\channelgw\ws.go:99` |
| `CONTENT_GUARD_API_ENDPOINT` |  | `internal\app\content_guard_wiring.go:129` |
| `CONTENT_GUARD_API_HOSTS` |  | `internal\app\content_guard_wiring.go:135` |
| `CONTENT_GUARD_API_TOKEN` |  | `internal\app\content_guard_wiring.go:142` |
| `CONTRIBUTOR_DEV` |  | `internal\platform\contributor_client.go:88` |
| `CORS_ALLOWED_EXTENSIONS` |  | `internal\router\router.go:114` |
| `CORS_ALLOW_ORIGINS_USER` |  | `internal\router\router.go:33` |
| `DB_HOST` |  | `config.yaml:66` |
| `DB_PORT` |  | `config.yaml:67` |
| `DEEPL_API_KEY` | DeepL 翻译密钥 | `config.yaml:172` |
| `DEEPSEEK_API_KEY` |  | `cmd\geo-run\main.go:74` |
| `DEFAULT_CREDENTIALS_HINT` |  | `internal\config\admin.go:114` |
| `DISABLE_AI_QUIET_HOURS` |  | `internal\service\webhook_outbound.go:82` |
| `DOUBAO_API_KEY` |  | `cmd\geo-run\main.go:76` |
| `EGOLITE_ASSET_TRUST_PUBKEY` |  | `internal\egolite\asset_package.go:206` |
| `EGO_RUNNER_CONF` |  | `cmd\ego-runner\main.go:63` |
| `EMAIL_163_PASSWORD` |  | `internal\email\service\email_send.go:501` |
| `EMAIL_163_SMTP_HOST` |  | `internal\email\service\email_send.go:502` |
| `EMAIL_163_USER` |  | `internal\email\service\email_send.go:500` |
| `EMAIL_WEBHOOK_SECRET` | 邮件 webhook HMAC 密钥 | `internal\controller\email_open_tracker.go:26<br>internal\controller\sms_unsubscribe.go:55` |
| `EMBEDDING_ALLOW_FALLBACK` |  | `config.yaml:104<br>internal\aiagent\llm\embedding.go:199` |
| `EMBEDDING_API_KEY` | Embedding 端点密钥 | `internal\aiagent\llm\embedding.go:239` |
| `EMBEDDING_BASE_URL` |  | `config.yaml:100<br>internal\aiagent\llm\embedding.go:184` |
| `EMBEDDING_CONCURRENCY` |  | `internal\aiagent\llm\embedding.go:105` |
| `EMBEDDING_DIM` |  | `internal\aiagent\llm\embedding.go:193` |
| `EMBEDDING_MODEL` |  | `internal\aiagent\llm\embedding.go:187` |
| `EMBEDDING_PROTOCOL` |  | `config.yaml:103<br>internal\aiagent\llm\embedding.go:190` |
| `EMBED_SDK_DIST` |  | `internal\router\embed_static_routes.go:31` |
| `ENABLE_DEBUG_ROUTES` |  | `internal\router\router.go:229` |
| `ENABLE_SWAGGER` |  | `internal\router\swagger.go:22` |
| `ERNIE_API_KEY` |  | `cmd\geo-run\main.go:77` |
| `EXPOSURE_DIAL_TIMEOUT_MS` |  | `internal\security\network_exposure_guard.go:26` |
| `FEISHU_APP_ID` |  | `internal\aiagent\knowledge\service\knowledge_merchant_external.go:270` |
| `FEISHU_APP_SECRET` | 飞书应用 secret | `internal\aiagent\knowledge\service\knowledge_merchant_external.go:271` |
| `FF_LTC_APPROVAL_GATE` |  | `internal\app\approval_wiring.go:239` |
| `FF_LTC_APPROVAL_RESUME` |  | `internal\app\approval_runtime_wiring.go:133` |
| `FF_LTC_COLLECTION_JOB` |  | `internal\service\collection_job.go:262<br>internal\service\collection_job.go:320` |
| `FF_LTC_HANDOFF_SLA_JOB` |  | `internal\app\human_task_sla_wiring.go:90` |
| `FF_LTC_KB_CANARY` |  | `internal\service\kb_canary.go:76` |
| `FF_LTC_KB_CHANGE_GATE` |  | `internal\pkg\kbrelease\kbrelease.go:76` |
| `FF_LTC_ORDER_DRAFT_DB` |  | `internal\app\order_draft_wiring.go:204<br>internal\app\order_draft_wiring.go:415` |
| `FF_LTC_REACH_GATE` |  | `internal\app\reach_gate_wiring.go:366` |
| `FF_LTC_RECOVERY_WORKER` |  | `internal\service\recovery_queue_worker.go:153` |
| `FF_LTC_SALES_TRIGGER` |  | `internal\app\order_draft_wiring.go:268<br>internal\app\order_draft_wiring.go:418` |
| `FF_TOOL_AUDIT_DB` |  | `internal\app\tool_audit_wiring.go:102` |
| `FF_TOOL_PERMISSION_ENFORCE` |  | `internal\app\permission_wiring.go:128` |
| `FILE_URL_TTL` |  | `internal\storage\fileurl.go:204` |
| `FRONTEND_URL` |  | `internal\service\password_reset.go:203` |
| `GEO_ALLOW_PRIVATE_PUBLISH_HOSTS` |  | `internal\geo\service\platform_outbound.go:27<br>internal\geo\service\platform_outbound.go:95` |
| `GEO_BATCH` |  | `cmd\geo-run\main.go:284` |
| `GEO_GITHUB_TOKEN` | GitHub 部署令牌 | `internal\geo\service\platform.go:128<br>internal\geo\service\platform.go:419` |
| `GEO_GITHUB_USER` |  | `internal\geo\service\platform.go:424` |
| `GEO_SEED_DSN` |  | `cmd\seedkw\main.go:14` |
| `GEO_SITE_BASE_URL` |  | `internal\config\website.go:31` |
| `GEO_SITE_ROOT` |  | `internal\geo\service\geo_site.go:21` |
| `GIN_MODE` |  | `cmd\api\main.go:431<br>internal\config\init.go:56<br>internal\pkg\db\db.go:54<br>…共 4 处` |
| `HEALTH_EMBEDDING_CRITICAL` |  | `internal\health\health.go:168<br>internal\health\health.go:218` |
| `HIVEMTK_SECRETS_DIR` |  | `cmd\api\main.go:311` |
| `HIVE_MTK_HOST_TOKEN` |  | `cmd\nm-host\main.go:105` |
| `HIVE_MTK_WS_URL` |  | `cmd\nm-host\main.go:94` |
| `HTTPS_PROXY` |  | `internal\config\server.go:365` |
| `HTTP_PROXY` |  | `internal\config\server.go:365` |
| `IMAGE_DESCRIBE_MODEL` |  | `internal\etl\image_describe.go:32` |
| `IMAGE_DESCRIBE_URL` |  | `internal\etl\image_describe.go:29` |
| `INSTALL_LOCK_PATH` |  | `internal\system\install\install.go:46` |
| `IS_TEST_MODE` |  | `cmd\api\main.go:138<br>internal\service\memory_system.go:114<br>internal\service\wecom_integration.go:183` |
| `JWT_SECRET` | 登录 JWT 签名密钥（≥32 字符） | `internal\middleware\permission.go:137<br>internal\pkg\utils\jwt.go:66` |
| `K_TRACE_LLM_REFINE` |  | `internal\service\lead_intent_llm.go:60` |
| `LLM_API_KEY` |  | `config.yaml:137<br>internal\aiagent\llm\llm.go:212<br>internal\aiagent\llm\llm.go:637<br>…共 4 处` |
| `LLM_BASE_URL` |  | `config.yaml:130<br>internal\aiagent\llm\llm.go:215<br>internal\aiagent\llm\llm.go:623<br>…共 4 处` |
| `LLM_DEBUG_DUMP_DIR` |  | `internal\aiagent\llm\llm.go:364` |
| `LLM_MODEL` |  | `config.yaml:131<br>internal\aiagent\llm\llm.go:220<br>internal\aiagent\llm\llm.go:629` |
| `MARKETING_WEBHOOK_ALLOW_INSECURE` | 危险：营销 webhook 免签（dev） | `internal\content\service\marketing_flow_action.go:704` |
| `MAX_JSON_BODY_MB` |  | `internal\middleware\body_limit.go:77` |
| `MCP_TOKEN` | MCP 网关令牌 | `internal\middleware\mcp_guard.go:48` |
| `MCP_TOKEN_PREV` | MCP 令牌轮换旧值（双读） | `internal\middleware\mcp_guard.go:51` |
| `MERCHANT_API_SECRET` | 商户 API 共享 HMAC 密钥（平台端） | `internal\platform\client.go:457<br>internal\platform\client.go:92` |
| `MERCHANT_STATE_DIR` | 商户身份密钥文件目录 | `internal\platform\sync.go:38` |
| `MOCK_AI_DELAY_MS` |  | `cmd\bridge-mock\main.go:141` |
| `MOCK_AI_REPLY` |  | `cmd\bridge-mock\main.go:149` |
| `MOCK_PORT` |  | `cmd\bridge-mock\main.go:136` |
| `MODE` |  | `internal\service\webhook.go:648` |
| `MTK_HUMANIZE_EVAL_DISABLED` |  | `internal\service\humanize_init.go:66` |
| `MTK_HUMANIZE_EVAL_THRESHOLD` |  | `internal\service\humanize_init.go:96` |
| `NOTION_API_KEY` | Notion API key | `internal\aiagent\knowledge\service\knowledge_merchant_external.go:349` |
| `ONEID_SALT` | OneID 身份归并盐 | `internal\identity\normalize.go:25` |
| `OPENAI_API_KEY` | OpenAI 兼容端点密钥（LLM 池） | `internal\content\service\ai_content.go:395` |
| `OPENAI_BASE_URL` |  | `internal\content\service\ai_content.go:402` |
| `ORDER_WEBHOOK_NONCE_STRICT` |  | `internal\middleware\webhook_signature.go:181` |
| `PARSER_SIDECAR_URL` |  | `internal\etl\parser_seam.go:26` |
| `PERF_PASSWORD` |  | `cmd\perf\main.go:32` |
| `PERF_USERNAME` |  | `cmd\perf\main.go:31` |
| `PGSERVICE` |  | `internal\pkg\testutil\testdb_config.go:29` |
| `PGSERVICEFILE` |  | `internal\pkg\testutil\testdb_config.go:29` |
| `PLATFORM_API_HOST` |  | `internal\config\platform.go:256` |
| `PLATFORM_API_URL` |  | `config.yaml:18<br>internal\config\platform.go:258<br>internal\platform\llm_proxy_client.go:119` |
| `PLATFORM_CLOUD_URL` |  | `internal\config\platform.go:86` |
| `PLATFORM_CONFIG_PATH` |  | `internal\config\platform.go:231<br>internal\config\platform.go:235` |
| `PLATFORM_ENABLED` |  | `cmd\api\main.go:392<br>cmd\api\main.go:398<br>internal\config\platform.go:118` |
| `PLATFORM_MERCHANT_KEY` |  | `internal\platform\asset_market_client.go:36<br>internal\platform\llm_proxy_client.go:128` |
| `PORT` |  | `cmd\api\main.go:760<br>internal\config\ports.go:85` |
| `POSTGRES_PASSWORD` |  | `cmd\seed\main.go:190<br>cmd\websitekb\main.go:43<br>internal\config\server.go:623<br>…共 6 处` |
| `PUBLIC_BASE_URL` |  | `cmd\api\main.go:144<br>config.yaml:35<br>internal\config\server.go:448<br>…共 4 处` |
| `QINIU_ACCESS_KEY` |  | `config.yaml:154` |
| `QINIU_BUCKET` |  | `config.yaml:156` |
| `QINIU_DOMAIN` |  | `config.yaml:157` |
| `QINIU_SECRET_KEY` | 七牛云 secret | `config.yaml:155` |
| `QQ_API_BASE_URL` |  | `internal\service\qq_account.go:228` |
| `QQ_EMAIL` |  | `internal\email\service\email_send.go:515` |
| `QQ_EMAIL_PASSWORD` | QQ 邮箱授权码 | `internal\email\service\email_send.go:516` |
| `QWEN_API_KEY` |  | `cmd\geo-run\main.go:75` |
| `RAG_CONTEXTUAL_ENHANCE` |  | `internal\aiagent\knowledge\service\knowledge_service_contextual.go:17` |
| `REACH_DISABLE_QUIET_HOURS` |  | `internal\app\reach_tool_wiring.go:85` |
| `REDIS_DB` |  | `cmd\api\main.go:82` |
| `REDIS_HOST` |  | `cmd\api\main.go:73` |
| `REDIS_PASSWORD` |  | `cmd\api\main.go:89` |
| `REDIS_PORT` |  | `cmd\api\main.go:77` |
| `REFLUX_ENABLED` |  | `internal\app\sales_engine_factory.go:149` |
| `REQUIRE_PRIVATE_NETWORK` |  | `cmd\api\main.go:144<br>internal\security\network_exposure_guard.go:31` |
| `RERANK_API_KEY` | Rerank 端点密钥 | `internal\aiagent\rag\retrieval\rerank.go:201` |
| `RERANK_BASE_URL` |  | `config.yaml:117<br>internal\aiagent\rag\retrieval\rerank.go:175` |
| `RERANK_ENABLED` |  | `internal\aiagent\rag\retrieval\rerank.go:184` |
| `RERANK_MODEL` |  | `config.yaml:118<br>internal\aiagent\rag\retrieval\rerank.go:178` |
| `RERANK_PROTOCOL` |  | `config.yaml:119<br>internal\aiagent\rag\retrieval\rerank.go:181` |
| `RERANK_TIMEOUT` |  | `internal\aiagent\rag\retrieval\rerank.go:195` |
| `RESTORE_TMP_DIR` |  | `internal\storage\backup_source.go:29` |
| `SERVER_API_BASE` |  | `internal\config\server.go:559` |
| `SERVER_HOST` |  | `cmd\api\main.go:760` |
| `SHOW_DEFAULT_CREDENTIALS` |  | `internal\config\admin.go:109` |
| `SITE_TENANT_CODE` |  | `internal\controller\auth_register.go:64` |
| `SMS_ALLOW_NIGHT_SEND` |  | `internal\service\sms\sms.go:295` |
| `SMTP_FROM` |  | `internal\service\email\email.go:239` |
| `SMTP_FROM_NAME` |  | `internal\service\email\email.go:240` |
| `SMTP_HOST` |  | `internal\email\service\email_send.go:526<br>internal\service\email\email.go:224` |
| `SMTP_PASSWORD` | SMTP 口令 | `internal\email\service\email_send.go:528<br>internal\service\email\email.go:238` |
| `SMTP_PORT` |  | `internal\email\service\email_send.go:530<br>internal\service\email\email.go:228` |
| `SMTP_SSL` |  | `internal\service\email\email.go:232` |
| `SMTP_USER` |  | `internal\email\service\email_send.go:527<br>internal\service\email\email.go:237` |
| `SSE_CORS_ALLOW_ORIGINS` |  | `internal\router\router.go:35` |
| `STORAGE_LOCAL_BASE_DIR` |  | `internal\storage\attachment_source.go:32<br>internal\storage\factory.go:33` |
| `STORAGE_LOCAL_PUBLIC_URL` |  | `internal\storage\attachment_source.go:42<br>internal\storage\factory.go:41` |
| `TELEGRAM_POLLING_ENABLED` |  | `internal\service\telegram_polling.go:225<br>internal\service\telegram_polling.go:272` |
| `TENANT_MODE` |  | `internal\pkg\tenant\tenant.go:68` |
| `TOOL_PERMISSION_DEFAULT_DENY` |  | `internal\aiagent\agent\tooluse\permission_whitelist.go:141` |
| `TOOL_PERMISSION_WHITELIST_SEED_FILE` |  | `internal\aiagent\agent\tooluse\permission_whitelist.go:95` |
| `TOOL_RISK_STATS_FLUSH_INTERVAL` |  | `internal\app\permission_wiring.go:294` |
| `TRUSTED_PROXIES` |  | `internal\middleware\client_ip_guard.go:64` |
| `UPLOAD_FOLDER` |  | `internal\storage\attachment_source.go:37` |
| `UPLOAD_MAX_SIZE` |  | `internal\controller\upload.go:112` |
| `UPLOAD_VIRUS_SCAN` |  | `internal\controller\upload.go:131` |
| `USER` |  | `internal\pkg\utils\fingerprint\fingerprint.go:47<br>internal\pkg\utils\fingerprint\fingerprint.go:48` |
| `USER_JWT_SECRET` | 登录 JWT 签名密钥（config.yaml 展开，≥32 字符） | `config.yaml:43<br>internal\pkg\utils\jwt.go:64` |
| `USER_WEB_DIST` | （见 example） | `internal\router\embed_static_routes.go:16` |
| `USE_DEFAULT_ADMIN` |  | `internal\config\admin.go:103` |
| `VIRUS_SCAN_URL` |  | `internal\controller\upload.go:134` |
| `VISITOR_TOKEN_SECRET` | 访客 token HMAC 密钥 | `config.yaml:44` |
| `WEBHOOK_RECOVERY_ENABLED` |  | `internal\service\webhook_recovery.go:70` |
| `WECOM_ALLOW_OUTBOUND` |  | `internal\service\wecom_integration.go:183` |
| `WECOM_DISABLE_OUTBOUND` |  | `internal\service\wecom_integration.go:182` |
| `WHATSAPP_FALLBACK_TEMPLATE` |  | `internal\service\webhook_outbound.go:756` |
| `WHATSAPP_FALLBACK_TEMPLATE_LANG` |  | `internal\service\webhook_outbound.go:775` |
| `WS_AGENT_ALLOW_ALL_USERS` | 危险：置 true 跳过 WS 坐席身份闸（仅本地调试） | `internal\websocket\handler.go:142` |
