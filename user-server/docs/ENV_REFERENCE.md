# 环境变量参考（机械生成，勿手改）

> 由 `scripts/gen_env_reference.go` 扫描 internal/ cmd/（非 _test.go）与 config.yaml 生成。
> 再生成：`go run ./scripts/gen_env_reference.go`；CI 校验：`go run ./scripts/gen_env_reference.go -check`。
> 测试基建专用键（POSTGRES_TEST_* / TESTDB_* / TEST_ADMIN_* / CI 等）不属部署配置面，未收录。
> <!-- envref-fingerprint: 136 keys -->

| 键 | 说明 | 读取点 |
|---|---|---|
| `ADMIN_CONFIG_FILE` |  | `internal/config/admin.go:61` |
| `ADMIN_EMAIL` |  | `internal/config/admin.go:91` |
| `ADMIN_REAL_NAME` |  | `internal/config/admin.go:94` |
| `ADMIN_USERNAME` |  | `internal/config/admin.go:88` |
| `AGENT_RUNTIME_BUS_ENABLED` |  | `cmd/api/main.go:861` |
| `AI_DEBOUNCE_SECONDS` |  | `internal/service/ai_debounce.go:28` |
| `ALLOWED_WS_ORIGINS` |  | `internal/config/ws_origin.go:47` |
| `ALLOW_INSECURE_TELEGRAM_WEBHOOK` | 危险：允许 Telegram webhook 免签（dev 双护栏） | `internal/channelbot/telegram/telegram.go:550<br>internal/service/webhook.go:683` |
| `ALLOW_INSECURE_WEBHOOK` | 危险：允许 webhook 免 HTTPS（dev） | `internal/service/webhook.go:663<br>internal/service/webhook.go:683` |
| `ALLOW_SELF_RESTART` | 允许进程自重启（离线升级面） | `internal/controller/system_ops.go:59` |
| `APP_ENV` |  | `internal/pkg/db/db.go:64<br>internal/service/webhook.go:682` |
| `AUTH_COOKIE_SECURE` |  | `internal/middleware/authcookie.go:37` |
| `AUTO_LOGIN_ENABLED` |  | `internal/config/admin.go:98` |
| `BACKUP_BASE_DIR` |  | `internal/storage/backup_source.go:25` |
| `BRIDGE_INGEST_AUTH` |  | `internal/middleware/bridge_ingress_guard.go:57` |
| `BRIDGE_INGEST_TOKEN` | 桥接入栈令牌；未配置 503 fail-closed | `internal/middleware/bridge_ingress_guard.go:29<br>internal/service/bridge_token.go:52` |
| `BRIDGE_INGEST_TOKEN_PREV` | 桥接令牌轮换期的旧令牌（双读） | `internal/middleware/bridge_ingress_guard.go:32` |
| `BROWSER_JEV_API_KEY` |  | `internal/browser_automation/service/jev.go:72` |
| `BROWSER_JEV_ENABLED` |  | `internal/browser_automation/service/jev.go:75` |
| `BROWSER_JEV_ENDPOINT` |  | `internal/browser_automation/service/jev.go:71` |
| `BROWSER_JEV_MODEL` |  | `internal/browser_automation/service/jev.go:73` |
| `BRUTE_FORCE_DISABLED` | 危险：置 1/true 关闭登录爆破防护（仅诊断临时用） | `internal/middleware/brute_force.go:17<br>internal/middleware/brute_force.go:17` |
| `CHANNEL_WS_ALLOWED_ORIGINS` |  | `internal/channelgw/ws.go:100` |
| `CHAT_MEDIA_URL_EXTRA_HOSTS` |  | `internal/service/chat_visitor.go:920` |
| `CONFIG_PATH` |  | `internal/config/server.go:535` |
| `CONTENT_GUARD_API_ENDPOINT` |  | `internal/app/content_guard_wiring.go:129` |
| `CONTENT_GUARD_API_HOSTS` |  | `internal/app/content_guard_wiring.go:135` |
| `CONTENT_GUARD_API_TOKEN` |  | `internal/app/content_guard_wiring.go:142` |
| `CONTRIBUTOR_DEV` |  | `internal/platform/contributor_client.go:88<br>internal/platform/contributor_client.go:91` |
| `CORS_ALLOWED_EXTENSIONS` |  | `internal/router/router.go:115` |
| `CORS_ALLOW_ORIGINS_USER` |  | `internal/router/router.go:34` |
| `DB_APP_PASSWORD` |  | `internal/pkg/db/db.go:41` |
| `DB_HOST` |  | `config.yaml:71` |
| `DB_PORT` |  | `config.yaml:72` |
| `DB_USER` |  | `internal/pkg/db/db.go:39` |
| `DEEPSEEK_API_KEY` |  | `cmd/geo-run/main.go:74` |
| `DEFAULT_CREDENTIALS_HINT` |  | `internal/config/admin.go:114` |
| `DOUBAO_API_KEY` |  | `cmd/geo-run/main.go:76` |
| `EGOLITE_ASSET_TRUST_PUBKEY` |  | `internal/egolite/asset_package.go:206` |
| `EGO_RUNNER_CONF` |  | `cmd/ego-runner/main.go:63` |
| `EMAIL_WEBHOOK_SECRET` | 邮件 webhook HMAC 密钥 | `internal/controller/email_open_tracker.go:26<br>internal/controller/sms_unsubscribe.go:55` |
| `EMBEDDING_MODEL` |  | `cmd/embedding-server/main.go:93` |
| `EMBED_SDK_DIST` |  | `internal/router/embed_static_routes.go:31` |
| `ENABLE_DEBUG_ROUTES` |  | `internal/router/router.go:250` |
| `ENABLE_SWAGGER` |  | `internal/router/swagger.go:22` |
| `ERNIE_API_KEY` |  | `cmd/geo-run/main.go:77` |
| `EXPOSURE_DIAL_TIMEOUT_MS` |  | `internal/security/network_exposure_guard.go:26` |
| `FF_LTC_APPROVAL_GATE` |  | `internal/app/approval_wiring.go:239` |
| `FF_LTC_APPROVAL_RESUME` |  | `internal/app/approval_runtime_wiring.go:133` |
| `FF_LTC_COLLECTION_JOB` |  | `internal/service/collection_job.go:262<br>internal/service/collection_job.go:320` |
| `FF_LTC_HANDOFF_SLA_JOB` |  | `internal/app/human_task_sla_wiring.go:90` |
| `FF_LTC_KB_CANARY` |  | `internal/service/kb_canary.go:76` |
| `FF_LTC_KB_CHANGE_GATE` |  | `internal/pkg/kbrelease/kbrelease.go:76` |
| `FF_LTC_ORDER_DRAFT_DB` |  | `internal/app/order_draft_wiring.go:204<br>internal/app/order_draft_wiring.go:415` |
| `FF_LTC_REACH_GATE` |  | `internal/app/reach_gate_wiring.go:366` |
| `FF_LTC_RECOVERY_WORKER` |  | `internal/service/recovery_queue_worker.go:153` |
| `FF_LTC_SALES_TRIGGER` |  | `internal/app/order_draft_wiring.go:268<br>internal/app/order_draft_wiring.go:418` |
| `FF_TOOL_AUDIT_DB` |  | `internal/app/tool_audit_wiring.go:102` |
| `FF_TOOL_PERMISSION_ENFORCE` |  | `internal/app/permission_wiring.go:129` |
| `FILES_OFF_MODE_REQUIRE_SIGN` |  | `internal/storage/fileurl.go:342` |
| `FRONTEND_URL` |  | `internal/service/password_reset.go:211` |
| `GEO_ALLOW_PRIVATE_PUBLISH_HOSTS` |  | `internal/geo/service/platform_outbound.go:27<br>internal/geo/service/platform_outbound.go:95` |
| `GEO_BATCH` |  | `cmd/geo-run/main.go:284` |
| `GEO_GITHUB_TOKEN` | GitHub 部署令牌 | `internal/geo/service/platform.go:128<br>internal/geo/service/platform.go:419` |
| `GEO_GITHUB_USER` |  | `internal/geo/service/platform.go:424` |
| `GEO_SEED_DSN` |  | `cmd/seedkw/main.go:14` |
| `GEO_SITE_BASE_URL` |  | `internal/config/website.go:31` |
| `GEO_SITE_ROOT` |  | `internal/geo/service/geo_site.go:21` |
| `GIN_MODE` |  | `cmd/api/main.go:467<br>internal/config/init.go:56<br>internal/pkg/db/db.go:64<br>…共 4 处` |
| `HIVEMTK_SECRETS_DIR` |  | `cmd/api/main.go:344` |
| `HIVE_MTK_HOST_TOKEN` |  | `cmd/nm-host/main.go:105` |
| `HIVE_MTK_WS_URL` |  | `cmd/nm-host/main.go:94` |
| `HTTPS_PROXY` |  | `internal/config/server.go:347` |
| `HTTP_PROXY` |  | `internal/config/server.go:347` |
| `INSTALL_LOCK_PATH` |  | `internal/system/install/install.go:46` |
| `IS_TEST_MODE` |  | `cmd/api/main.go:152<br>internal/service/memory_system.go:114<br>internal/service/wecom_integration.go:183` |
| `JWT_SECRET` | 登录 JWT 签名密钥（≥32 字符） | `internal/middleware/permission.go:110<br>internal/pkg/utils/jwt.go:66` |
| `K_TRACE_LLM_REFINE` |  | `internal/service/lead_intent_llm.go:60` |
| `LLM_DEBUG_DUMP_DIR` |  | `internal/aiagent/llm/llm.go:344` |
| `MARKETING_WEBHOOK_ALLOW_INSECURE` | 危险：营销 webhook 免签（dev） | `internal/content/service/marketing_flow_action.go:719` |
| `MAX_JSON_BODY_MB` |  | `internal/middleware/body_limit.go:77` |
| `MCP_TOKEN` | MCP 网关令牌 | `internal/middleware/mcp_guard.go:48` |
| `MCP_TOKEN_PREV` | MCP 令牌轮换旧值（双读） | `internal/middleware/mcp_guard.go:51` |
| `MERCHANT_API_SECRET` | 商户 API 共享 HMAC 密钥（平台端） | `internal/platform/client.go:387<br>internal/platform/client.go:88` |
| `MERCHANT_STATE_DIR` | 商户身份密钥文件目录 | `internal/platform/sync.go:38` |
| `MOCK_AI_DELAY_MS` |  | `cmd/bridge-mock/main.go:141` |
| `MOCK_AI_REPLY` |  | `cmd/bridge-mock/main.go:149` |
| `MOCK_PORT` |  | `cmd/bridge-mock/main.go:136` |
| `MODE` |  | `internal/service/webhook.go:682` |
| `ONEID_SALT` | OneID 身份归并盐 | `internal/identity/normalize.go:25` |
| `ORDER_WEBHOOK_NONCE_STRICT` |  | `internal/middleware/webhook_signature.go:181` |
| `PERF_PASSWORD` |  | `cmd/perf/main.go:32` |
| `PERF_USERNAME` |  | `cmd/perf/main.go:31` |
| `PGSERVICE` |  | `internal/pkg/testutil/testdb_config.go:29` |
| `PGSERVICEFILE` |  | `internal/pkg/testutil/testdb_config.go:29` |
| `PLATFORM_API_HOST` |  | `internal/config/platform.go:259` |
| `PLATFORM_API_URL` |  | `config.yaml:18<br>internal/config/platform.go:261<br>internal/platform/llm_proxy_client.go:119` |
| `PLATFORM_CLOUD_URL` |  | `internal/config/platform.go:89` |
| `PLATFORM_CONFIG_PATH` |  | `internal/config/platform.go:234<br>internal/config/platform.go:238` |
| `PLATFORM_ENABLED` |  | `cmd/api/main.go:427<br>cmd/api/main.go:433<br>internal/config/platform.go:121` |
| `PLATFORM_MERCHANT_KEY` |  | `internal/platform/asset_market_client.go:36<br>internal/platform/llm_proxy_client.go:128` |
| `PORT` |  | `cmd/api/main.go:836<br>internal/config/ports.go:85` |
| `POSTGRES_PASSWORD` |  | `cmd/seed/main.go:212<br>cmd/websitekb/main.go:43<br>internal/config/server.go:580<br>…共 6 处` |
| `PUBLIC_BASE_URL` |  | `cmd/api/main.go:158<br>config.yaml:37<br>internal/config/server.go:436<br>…共 5 处` |
| `QWEN_API_KEY` |  | `cmd/geo-run/main.go:75` |
| `REDIS_DB` |  | `cmd/api/main.go:82` |
| `REDIS_HOST` |  | `cmd/api/main.go:73` |
| `REDIS_PASSWORD` |  | `cmd/api/main.go:89` |
| `REDIS_PORT` |  | `cmd/api/main.go:77` |
| `REFLUX_ENABLED` |  | `internal/app/sales_engine_factory.go:159` |
| `REQUIRE_PRIVATE_NETWORK` |  | `cmd/api/main.go:158<br>internal/security/network_exposure_guard.go:31` |
| `RESTORE_TMP_DIR` |  | `internal/storage/backup_source.go:29` |
| `SERVER_API_BASE` |  | `internal/config/server.go:493` |
| `SERVER_HOST` |  | `cmd/api/main.go:836` |
| `SHOW_DEFAULT_CREDENTIALS` |  | `internal/config/admin.go:109` |
| `SITE_TENANT_CODE` |  | `internal/controller/auth_register.go:64` |
| `SMS_ALLOW_NIGHT_SEND` |  | `internal/service/sms/sms.go:295` |
| `SSE_CORS_ALLOW_ORIGINS` |  | `internal/router/router.go:36` |
| `STORAGE_LOCAL_BASE_DIR` |  | `internal/storage/attachment_source.go:32<br>internal/storage/factory.go:71` |
| `STORAGE_LOCAL_PUBLIC_URL` |  | `internal/storage/attachment_source.go:42<br>internal/storage/factory.go:82` |
| `TELEGRAM_POLLING_ENABLED` |  | `internal/service/telegram_polling.go:229<br>internal/service/telegram_polling.go:276` |
| `TENANT_MODE` |  | `internal/pkg/tenant/tenant.go:68` |
| `TOOL_PERMISSION_DEFAULT_DENY` |  | `internal/aiagent/agent/tooluse/permission_whitelist.go:143` |
| `TOOL_PERMISSION_WHITELIST_SEED_FILE` |  | `internal/aiagent/agent/tooluse/permission_whitelist.go:95` |
| `TOOL_RISK_STATS_FLUSH_INTERVAL` |  | `internal/app/permission_wiring.go:296` |
| `TRUSTED_PROXIES` |  | `internal/middleware/client_ip_guard.go:64` |
| `UPLOAD_FOLDER` |  | `internal/storage/attachment_source.go:37` |
| `UPLOAD_MAX_SIZE` |  | `internal/controller/upload.go:112` |
| `USER` |  | `internal/pkg/utils/fingerprint/fingerprint.go:47<br>internal/pkg/utils/fingerprint/fingerprint.go:48` |
| `USER_JWT_SECRET` | 登录 JWT 签名密钥（config.yaml 展开，≥32 字符） | `internal/pkg/utils/jwt.go:64` |
| `USER_WEB_DIST` | （见 example） | `internal/router/embed_static_routes.go:16` |
| `USE_DEFAULT_ADMIN` |  | `internal/config/admin.go:103` |
| `VISITOR_TOKEN_SECRET` | 访客 token HMAC 密钥 | `config.yaml:49` |
| `WECOM_ALLOW_OUTBOUND` |  | `internal/service/wecom_integration.go:183` |
| `WECOM_DISABLE_OUTBOUND` |  | `internal/service/wecom_integration.go:182` |
| `WS_AGENT_ALLOW_ALL_USERS` | 危险：置 true 跳过 WS 坐席身份闸（仅本地调试） | `internal/websocket/handler.go:143` |
