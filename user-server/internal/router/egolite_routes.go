package router

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/browsercore"
	"hivemtk-user/internal/egolite"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/utils/logger"
	hrepo "hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
	"hivemtk-user/internal/storage"
)

// SetupEgoLiteRoutes ego-lite 腿路由（设计文档 S3/S6/S7/S9；调研 §6.5 鉴权定案）。
//
// 端点总表：
//   - GET  /api/egolite/runner-ws          runner WS（eg_ token，免 JWT）
//   - GET  /api/egolite/runner/status      连接状态（用户看自己 / admin 全量）
//   - GET  /api/egolite/runner/tokens      我的 runner 令牌列表（只回 hint，不回明文）
//   - POST /api/egolite/runner/tokens      签发令牌（明文仅此一次返回）
//   - POST /api/egolite/runner/tokens/:id/revoke 撤销令牌（立即失效）
//   - POST /api/egolite/runner/token/reset eg_ 兜底 token 轮换（admin，老部署兼容）
//   - POST /api/egolite/flows              保存草稿（保存即扫描）
//   - GET  /api/egolite/flows              流程列表
//   - GET  /api/egolite/flows/:id          流程详情（含扫描报告）
//   - POST /api/egolite/flows/:id/rescan   重新扫描
//   - POST /api/egolite/flows/:id/publish  发布版本
//   - POST /api/egolite/flows/:id/submit   提交平台审核
//   - POST /api/egolite/flows/:id/dry-run  试跑（draft 通道，过扫描门）
//   - DELETE /api/egolite/flows/:id        删除流程
//   - GET/POST/DELETE /api/egolite/flows/:id/cron  流程定时触发器（specs/016 S10，只挂纯读流程）
//   - POST /api/egolite/executions         下发执行（draft=flow 试跑 / market=资产执行）
//   - GET  /api/egolite/executions         执行列表
//   - GET  /api/egolite/executions/:id     执行详情
//   - GET  /api/egolite/executions/:id/events 事件日志（增量游标 after_id；轮询兜底）
//   - GET  /api/egolite/executions/:id/stream 事件流 SSE（S-13；终态发 end 帧收流）
//   - POST /api/egolite/executions/:id/abort   停止
//   - POST /api/egolite/executions/:id/confirm 用户确认/拒绝（写操作闸）
//   - POST /api/egolite/executions/:id/redispatch 待下发执行重发（runner 上线后）
//   - GET  /api/egolite/assets             资产目录橱窗（用户端，只有 published 可见）
//   - POST /api/egolite/admin/assets/publish    平台端发布：审核用户流程 → 目录条目（admin）
//   - POST /api/egolite/admin/assets/pack       平台端发布：直接发脚本包（admin）
//   - GET  /api/egolite/admin/assets            平台端资产全量列表（admin，含 offline）
//   - POST /api/egolite/admin/assets/:id/offline 平台端资产下线（admin）
//   - GET  /api/egolite/admin/flows        审核工作台：跨用户按状态列流程（admin）
//   - POST /api/egolite/runner/telemetry   runner 遥测上报（S-9；eg_ token，默认拒绝白名单）
//   - GET  /api/egolite/admin/telemetry/summary 遥测看板（admin；只回聚合桶，无明细面）
//
// 「平台端」= 本实例管理端（role=admin），**不经 platform-server / PLATFORM_ENABLED /
// binary_asset 通道**：发布 = 往 egolite_assets 写一行，用户端立即可见可执行。
//
// 后台任务：确认 TTL + 硬超时看门狗（60s 扫描周期）。
func SetupEgoLiteRoutes(auth *gin.RouterGroup, engine *gin.Engine, gormDB *gorm.DB) {
	kvRepo := hrepo.NewPlatformConfigKVRepositoryWithDB(gormDB)
	registry := egolite.NewEgoRunnerRegistry()

	execRepo := egolite.NewEgoExecutionRepositoryWithDB(gormDB)
	evRepo := egolite.NewEgoEventRepositoryWithDB(gormDB)
	flowRepo := egolite.NewEgoFlowRepositoryWithDB(gormDB)
	assetRepo := egolite.NewEgoAssetRepositoryWithDB(gormDB)
	tokenRepo := egolite.NewEgoRunnerTokenRepositoryWithDB(gormDB)
	claimRepo := egolite.NewEgoWriteClaimRepositoryWithDB(gormDB)
	// 统一鉴权单点：令牌行（每用户多条）优先，KV 双候选兜底（老部署兼容）
	runnerAuth := egolite.NewEgoTokenAuthenticator(tokenRepo, kvRepo)

	execSvc := egolite.NewEgoExecutionService(execRepo, evRepo, flowRepo, assetRepo, claimRepo, registry)
	// S-13：执行事件广播（SSE 数据源）。IngestEvent 是唯一写入口，广播跟着它走。
	eventHub := egolite.NewEgoEventHub()
	execSvc.AttachEventHub(eventHub)
	// S-9 遥测：上报入口（eg_ token）+ 管理端聚合查询的取数仓库。
	telRepo := egolite.NewEgoTelemetryRepositoryWithDB(gormDB)
	// 通用执行能力（会话 + 轮次）：session 只做多轮聚合，事件先委托执行域再聚合。
	sessionRepo := egolite.NewEgoSessionRepositoryWithDB(gormDB)
	roundRepo := egolite.NewEgoRoundRepositoryWithDB(gormDB)
	sessionSvc := egolite.NewEgoSessionService(sessionRepo, roundRepo, execRepo, evRepo, registry, execSvc)
	tooluse.SetBrowserSessionPort(sessionSvc)
	flowSvc := egolite.NewEgoFlowService(flowRepo, execSvc)
	sessionSvc.AttachFlowService(flowSvc)
	// T21 日配额：会话/轮次/产物三维（quota.go）。注入 service（tooluse/workflow
	// 入口同享），产物闸直接挂在 runner 产物上传口。
	sessionSvc.AttachQuota(
		func(ctx context.Context, userID uint) error { return egolite.CheckSessionQuota(ctx, gormDB, userID) },
		func(ctx context.Context, userID uint) error { return egolite.CheckRoundQuota(ctx, gormDB, userID) },
	)
	assetSvc := egolite.NewEgoAssetCatalog(assetRepo, flowRepo)

	// 流程定时触发器（specs/016 S10，browser_cron_triggers 模式）：fire 闭包做
	// 触发前置（runner 在线 + 流程复查 + 写动作复查），再走与手跑同一 Start 入口
	// ——调度只是触发方式的差异，裁决链完全一致（服务端唯一事实源）。
	cronRepo := egolite.NewEgoFlowCronRepositoryWithDB(gormDB)
	cronSvc := egolite.NewEgoFlowCronService(cronRepo, flowRepo, func(ctx context.Context, userID, flowID uint) error {
		if err := registry.EnsureOnline(userID); err != nil {
			return err
		}
		f, err := flowRepo.GetForUser(ctx, flowID, userID)
		if err != nil {
			return err
		}
		if !f.ScanPassed {
			return fmt.Errorf("扫描门未通过（流程在挂载后被改红）")
		}
		var writes []string
		if json.Unmarshal(f.WritesJSON, &writes) == nil && len(writes) > 0 {
			return fmt.Errorf("流程已声明写动作，定时触发摘除（决策点 6）")
		}
		_, err = execSvc.Start(ctx, userID, egolite.StartRequest{
			Origin:    "draft",
			FlowID:    flowID,
			SpaceName: fmt.Sprintf("cron-flow-%d", flowID),
		})
		return err
	})
	go cronSvc.RestoreAll(context.Background())

	// AI 工具执行器注入（specs/016 S11）：tooluse.SetEgoLiteRunner → EnsureOnline + Start，
	// 与 browser_automation_routes.go 的 SetBrowserTaskRunner 同型
	tooluse.SetEgoLiteRunner(func(ctx context.Context, userID uint, req tooluse.EgoLiteRunRequest) (map[string]any, error) {
		if err := registry.EnsureOnline(userID); err != nil {
			return nil, err
		}
		e, err := execSvc.Start(ctx, userID, egolite.StartRequest{
			Origin:        map[bool]string{true: "draft", false: "market"}[req.FlowID > 0],
			FlowID:        req.FlowID,
			AssetID:       req.AssetID,
			Entry:         "",
			Params:        req.Params,
			SpaceName:     req.SpaceName,
			ConfirmPolicy: req.ConfirmPolicy,
			TimeoutSec:    req.TimeoutSec,
		})
		if err != nil {
			return nil, err
		}
		return map[string]any{"execution_id": e.ID, "state": e.State, "origin": e.Origin}, nil
	})

	// workflow egolite_run 节点注入（S-10）：与上面的 AI 工具同一条 Start 入口，
	// 差别只在触发面——workflow 的节点参数由编排配置给出，归属人由节点/触发载荷指定。
	service.SetWorkflowEgoLiteRunner(workflowEgoLiteAdapter{execSvc: execSvc, registry: registry})

	// workflow browser_session 节点注入：与 browser_task 同型（一轮 ops，异步派发）
	service.SetWorkflowBrowserSessionRunner(func(ctx context.Context, userID uint, sessionID string, ops []map[string]any) (map[string]any, error) {
		sid, err := strconv.ParseUint(sessionID, 10, 32)
		if err != nil || sid == 0 {
			return nil, fmt.Errorf("session_id 非法: %q", sessionID)
		}
		if userID == 0 {
			return nil, fmt.Errorf("browser_session 节点必须提供 user_id")
		}
		raw, _ := json.Marshal(ops)
		var typed []browsercore.Op
		if err := json.Unmarshal(raw, &typed); err != nil {
			return nil, fmt.Errorf("ops 形状非法: %w", err)
		}
		res, err := sessionSvc.Act(ctx, browsercore.RoundRequest{UserID: userID, SessionID: uint(sid), Ops: typed})
		if err != nil {
			return nil, err
		}
		return map[string]any{"session_id": res.Session.ID, "round": res.Round.Round, "state": res.Session.State}, nil
	})

	// 断连钩子：该用户全部非终态执行判 failed（FailActiveByUser）。
	// 独立实现实例——绝不复用 host-ws 的 FailRunningByUser（那是 browser_sessions 语义）。
	registry.SetDisconnectHook(func(ctx context.Context, userID uint) {
		execSvc.OnRunnerDisconnect(ctx, userID)
		sessionSvc.OnRunnerDisconnect(ctx, userID)
	})

	// 启动兜底：KV 无 eg_ token 则自动生成（fail-closed 不卡死首次部署，host-ws 先例）
	egolite.EnsureEgoTokenExists(context.Background(), kvRepo)

	// 资产清单拉取（T3：runner 侧 SyncPull 等价）。鉴权同 runner-ws：eg_ token 即身份——
	// 清单 = 已发布目录资产 + 该 token 归属用户自己的 published 流程。
	engine.GET("/api/egolite/assets/pull", func(c *gin.Context) {
		tok := c.Query("token")
		if tok == "" {
			tok = strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		}
		userID, ok := runnerAuth.Authenticate(c.Request.Context(), tok)
		if !ok {
			c.JSON(401, gin.H{"code": 1, "message": "runner token 无效"})
			return
		}
		assets, err := egolite.ListInstallableForUser(c.Request.Context(), flowRepo, assetRepo, userID)
		if err != nil {
			c.JSON(500, gin.H{"code": 1, "message": err.Error()})
			return
		}
		// format=zip（S-7 包通道）：同一份清单打成包下发，runner 原子解包后换入。
		// 默认仍是 JSON 清单——旧 runner 不认 zip，改了默认等于断兼容。
		// S-6：带签名的资产先验签（fail-close）；未配置信任公钥时，已签名资产一律拒。
		pub, kerr := egolite.LoadAssetTrustPubKeyFromEnv()
		if kerr != nil {
			c.JSON(400, gin.H{"code": 1, "message": kerr.Error()})
			return
		}
		if verr := egolite.VerifyPackageSignatures(assets, pub); verr != nil {
			c.JSON(400, gin.H{"code": 1, "message": verr.Error()})
			return
		}
		if c.Query("format") == "zip" {
			pkg, perr := egolite.BuildAssetPackage(assets)
			if perr != nil {
				c.JSON(400, gin.H{"code": 1, "message": perr.Error()})
				return
			}
			c.Data(200, "application/zip", pkg)
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": gin.H{"assets": assets}})
	})

	// runner 产物上传（截图）：eg_ token 即身份；只收图片（PNG/JPEG），
	// 8MB 上限；落 storage 本地驱动（与既有附件同一套目录/URL/租户前缀规则）。
	engine.POST("/api/egolite/runner/artifacts", func(c *gin.Context) {
		tok := c.Query("token")
		if tok == "" {
			tok = strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		}
		userID, ok := runnerAuth.Authenticate(c.Request.Context(), tok)
		if !ok {
			c.JSON(401, gin.H{"code": 1, "message": "runner token 无效"})
			return
		}
		const maxArtifact = 8 << 20
		c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxArtifact+1024)
		file, header, err := c.Request.FormFile("file")
		if err != nil {
			c.JSON(400, gin.H{"code": 1, "message": "缺少 file 字段或超出大小上限"})
			return
		}
		defer func() { _ = file.Close() }()
		if header.Size > maxArtifact {
			c.JSON(413, gin.H{"code": 1, "message": "产物超过 8MB 上限"})
			return
		}
		// T21 产物日配额：截图是最容易失控的采集面（脚本循环截图），总量闸在此。
		if qerr := egolite.CheckArtifactQuota(c.Request.Context(), gormDB, userID); qerr != nil {
			c.JSON(429, gin.H{"code": 1, "message": qerr.Error()})
			return
		}
		buf := &bytes.Buffer{}
		if _, err := io.Copy(buf, io.LimitReader(file, maxArtifact+1)); err != nil {
			c.JSON(500, gin.H{"code": 1, "message": "读取产物失败"})
			return
		}
		b := buf.Bytes()
		if len(b) > maxArtifact {
			c.JSON(413, gin.H{"code": 1, "message": "产物超过 8MB 上限"})
			return
		}
		ext := ""
		switch {
		case len(b) >= 8 && bytes.Equal(b[:8], []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}):
			ext = ".png"
		case len(b) >= 3 && b[0] == 0xff && b[1] == 0xd8 && b[2] == 0xff:
			ext = ".jpg"
		default:
			c.JSON(400, gin.H{"code": 1, "message": "只接受 PNG/JPEG 产物"})
			return
		}
		baseDir, publicPrefix, folder := storage.LocalSource()
		driver := storage.NewLocalDriver(baseDir, publicPrefix)
		name := fmt.Sprintf("ego-%d-%d%s", userID, time.Now().UnixNano(), ext)
		url, _, err := driver.UploadReader(c.Request.Context(), bytes.NewReader(b), int64(len(b)), folder+"/egolite", name)
		if err != nil {
			c.JSON(500, gin.H{"code": 1, "message": "产物落盘失败: " + err.Error()})
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": gin.H{"url": url, "bytes": len(b)}})
	})

	// trust.pem 分发通道（S-6 余量）：本实例信任公钥的获取口——安装器 --trust-url
	// 拉取落到 ~/.hivemtk/ego_trust.pem，runner 验签 fail-close 的公钥来源。
	// 公钥本来就是公开物，端点免鉴权；未配置信任公钥（全验签关闭的部署）回 404，
	// 安装器据此跳过。形状与 runner 解析器一致：base64 行 + 指纹注释。
	engine.GET("/api/egolite/assets/trust.pem", func(c *gin.Context) {
		pub, err := egolite.LoadAssetTrustPubKeyFromEnv()
		if err != nil {
			c.JSON(500, gin.H{"code": 1, "message": err.Error()})
			return
		}
		if pub == nil {
			c.JSON(404, gin.H{"code": 1, "message": "本实例未配置资产信任公钥（EGO_ASSET_TRUST_PUBLIC_KEY）"})
			return
		}
		c.String(200, "# hivemtk ego asset trust store (ed25519)\n# fingerprint=%s\n%s\n",
			egolite.PubKeyFingerprint(pub), base64.StdEncoding.EncodeToString(pub))
	})

	// runner 遥测上报（S-9）：eg_ token 即身份；body {"events":[...]}，每条样本在
	// IngestTelemetry 过默认拒绝白名单——页面内容类键在入库前丢弃并计数，这里不做
	// 第二次口径裁决。批次上限 500，超出截断并在响应里如实回 dropped/truncated。
	engine.POST("/api/egolite/runner/telemetry", func(c *gin.Context) {
		tok := c.Query("token")
		if tok == "" {
			tok = strings.TrimPrefix(c.GetHeader("Authorization"), "Bearer ")
		}
		userID, ok := runnerAuth.Authenticate(c.Request.Context(), tok)
		if !ok {
			c.JSON(401, gin.H{"code": 1, "message": "runner token 无效"})
			return
		}
		var body struct {
			Events []map[string]any `json:"events"`
		}
		if err := c.ShouldBindJSON(&body); err != nil || len(body.Events) == 0 {
			c.JSON(400, gin.H{"code": 1, "message": "events 必填（非空数组）"})
			return
		}
		accepted, droppedKeys, truncated, err := egolite.IngestTelemetry(c.Request.Context(), userID, telRepo, body.Events)
		if err != nil {
			c.JSON(500, gin.H{"code": 1, "message": "遥测入库失败: " + err.Error()})
			return
		}
		c.JSON(200, gin.H{"code": 0, "data": gin.H{
			"accepted": accepted, "dropped_keys": droppedKeys, "truncated": truncated,
		}})
	})

	// runner WS（不过 JWT——token 即身份，host-ws 同形状）
	wsHandler := egolite.NewEgoRunnerWSHandler(registry, kvRepo)
	wsHandler.SetTokenRepository(tokenRepo)
	wsHandler.AttachEventSink(sessionSvc)
	engine.GET("/api/egolite/runner-ws", wsHandler.Handle)

	// 看门狗（60s 扫描：confirm TTL + 硬超时；WatchdogSweep 幂等，扫不到就空转）
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		// 观测留存清理（T19/J-6 D6 收口）：挂进同一循环、日级节流——每个 UTC 日
		// 首次 tick 跑一次 RetentionSweep（幂等 UPDATE，重复执行 no-op）；
		// keepDays 从 config_params 读（≤0 = 永不清理档）。失败只告警，不拦看门狗主职责。
		lastRetentionDay := -1
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, _ = execSvc.WatchdogSweep(ctx)
			_, _ = sessionSvc.WatchdogSweep(ctx)
			if day := time.Now().UTC().YearDay(); day != lastRetentionDay {
				lastRetentionDay = day
				n, err := egolite.RetentionSweep(ctx, gormDB, egolite.ObserveRetentionDays(ctx, gormDB))
				if err != nil {
					logger.Warnf("[egolite] 观测留存清理失败（窗口内 %d 行未动）: %v", n, err)
				}
			}
			cancel()
		}
	}()

	eg := auth.Group("/egolite")
	{
		// ---- runner 通道 ----
		eg.GET("/runner/status", func(c *gin.Context) {
			userID := currentUID(c)
			isAdmin := c.GetString("role") == "admin"
			response2(c, registry.StatusFor(userID, isAdmin), "ok")
		})
		// ---- runner 令牌（每用户多条；明文仅在签发那一刻返回一次）----
		// 多用户各自 runner 的前提：KV 单值 token 只能绑一个 userID，第二台 runner
		// 连上就是顶号。行表让"每个用户把自己的浏览器接进基座"成立。
		eg.GET("/runner/tokens", func(c *gin.Context) {
			list, err := tokenRepo.ListByUser(c.Request.Context(), currentUID(c))
			if err != nil {
				c.JSON(500, gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": gin.H{"list": list}})
		})
		eg.POST("/runner/tokens", func(c *gin.Context) {
			var body struct {
				Label string `json:"label"`
			}
			_ = c.ShouldBindJSON(&body)
			plain, rec, err := tokenRepo.Issue(c.Request.Context(), currentUID(c), body.Label)
			if err != nil {
				c.JSON(500, gin.H{"code": 1, "message": "签发失败: " + err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": gin.H{"token": plain, "token_hint": rec.TokenHint, "id": rec.ID},
				"message": "令牌只显示这一次，请立即复制到 runner 配置"})
		})
		eg.POST("/runner/tokens/:id/revoke", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			if err := tokenRepo.Revoke(c.Request.Context(), uint(id), currentUID(c)); err != nil {
				c.JSON(400, gin.H{"code": 1, "message": "令牌不存在或已撤销"})
				return
			}
			c.JSON(200, gin.H{"code": 0, "message": "已撤销；该令牌立即失效"})
		})
		// 兼容口径：轮换 KV 兜底 token（老部署/单机自用；admin 限定）
		eg.POST("/runner/token/reset", func(c *gin.Context) {
			if c.GetString("role") != "admin" {
				c.JSON(403, gin.H{"code": 1, "message": "仅管理员可轮换兜底 runner token"})
				return
			}
			tok, err := egolite.RotateEgoToken(c.Request.Context(), kvRepo, 0)
			if err != nil {
				c.JSON(500, gin.H{"code": 1, "message": "轮换失败: " + err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": gin.H{"token": tok},
				"message": "已重置兜底 token；旧值仍可用，直到下一次重置"})
		})

		// ---- 流程域 ----
		eg.POST("/flows", func(c *gin.Context) {
			var req egolite.SaveRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(400, gin.H{"code": 1, "message": err.Error()})
				return
			}
			flowID := uint(0)
			if v := c.Query("id"); v != "" {
				if n, err := strconv.ParseUint(v, 10, 32); err == nil {
					flowID = uint(n)
				}
			}
			f, report, err := flowSvc.Save(c.Request.Context(), currentUID(c), flowID, req)
			if err != nil {
				c.JSON(400, gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": gin.H{"flow": f, "scan_report": report}})
		})
		eg.GET("/flows", func(c *gin.Context) {
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
			list, total, err := flowRepo.ListByUser(c.Request.Context(), currentUID(c), page, size)
			if err != nil {
				c.JSON(500, gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": gin.H{"list": list, "total": total}})
		})
		eg.GET("/flows/:id", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			f, err := flowRepo.GetForUser(c.Request.Context(), uint(id), currentUID(c))
			if err != nil {
				c.JSON(404, gin.H{"code": 1, "message": "流程不存在"})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": f})
		})
		eg.POST("/flows/:id/rescan", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			report, err := flowSvc.Rescan(c.Request.Context(), currentUID(c), uint(id))
			if err != nil {
				c.JSON(400, gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": report})
		})
		eg.POST("/flows/:id/publish", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			f, err := flowSvc.Publish(c.Request.Context(), currentUID(c), uint(id))
			if err != nil {
				c.JSON(400, gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": f})
		})
		// 试跑（draft 通道）：过扫描门后建执行——用户写脚本时唯一的"先看看效果"路径
		eg.POST("/flows/:id/dry-run", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			var body struct {
				Params     json.RawMessage `json:"params"`
				SpaceName  string          `json:"space_name"`
				TimeoutSec int             `json:"timeout_sec"`
			}
			_ = c.ShouldBindJSON(&body)
			e, err := flowSvc.DryRun(c.Request.Context(), currentUID(c), uint(id), body.Params, body.SpaceName, body.TimeoutSec)
			if err != nil {
				status := 400
				if err == egolite.ErrConcurrencyGate {
					status = 429
				}
				c.JSON(status, gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": e})
		})
		eg.DELETE("/flows/:id", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			if err := flowRepo.Delete(c.Request.Context(), uint(id), currentUID(c)); err != nil {
				c.JSON(404, gin.H{"code": 1, "message": "流程不存在"})
				return
			}
			c.JSON(200, gin.H{"code": 0, "message": "已删除"})
		})
		eg.POST("/flows/:id/submit", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			f, err := flowSvc.Submit(c.Request.Context(), currentUID(c), uint(id))
			if err != nil {
				c.JSON(400, gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": f})
		})

		// ---- 流程定时触发器（specs/016 S10；只挂纯读流程，service 闸 fail-close）----
		eg.GET("/flows/:id/cron", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			tr, err := cronSvc.Get(c.Request.Context(), currentUID(c), uint(id))
			if err != nil {
				c.JSON(404, gin.H{"code": 1, "message": "流程未挂载定时触发器"})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": tr})
		})
		eg.POST("/flows/:id/cron", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			var body struct {
				CronExpr string `json:"cron_expr"`
				TimeZone string `json:"time_zone"`
				Enabled  *bool  `json:"enabled"` // nil ⇒ 默认 true；显式 false 原样落库
			}
			if err := c.ShouldBindJSON(&body); err != nil || body.CronExpr == "" {
				c.JSON(400, gin.H{"code": 1, "message": "cron_expr 必填（5 段「分 时 日 月 周」）"})
				return
			}
			tr, err := cronSvc.Upsert(c.Request.Context(), currentUID(c), uint(id), egolite.UpsertCronRequest{
				CronExpr: body.CronExpr, TimeZone: body.TimeZone, Enabled: body.Enabled,
			})
			if err != nil {
				c.JSON(400, gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": tr, "message": "已挂载；纯读流程按表达式定时发起执行"})
		})
		eg.DELETE("/flows/:id/cron", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			if err := cronSvc.Delete(c.Request.Context(), currentUID(c), uint(id)); err != nil {
				c.JSON(404, gin.H{"code": 1, "message": "流程未挂载定时触发器"})
				return
			}
			c.JSON(200, gin.H{"code": 0, "message": "已摘除定时触发器"})
		})

		// ---- 执行域 ----
		eg.POST("/executions", func(c *gin.Context) {
			var req egolite.StartRequest
			if err := c.ShouldBindJSON(&req); err != nil {
				c.JSON(400, gin.H{"code": 1, "message": err.Error()})
				return
			}
			e, err := execSvc.Start(c.Request.Context(), currentUID(c), req)
			if err != nil {
				status := 400
				switch err {
				case egolite.ErrConcurrencyGate:
					status = 429
				case egolite.ErrAssetVersionConflict, egolite.ErrFlowScanGate:
					// 409：资源当前状态冲突（版本已更新/扫描门未过），非入参格式错
					status = 409
				}
				c.JSON(status, gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": e})
		})
		eg.GET("/executions", func(c *gin.Context) {
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
			list, total, err := execRepo.ListByUser(c.Request.Context(), currentUID(c), page, size)
			if err != nil {
				c.JSON(500, gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": gin.H{"list": list, "total": total}})
		})
		eg.GET("/executions/:id", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			e, err := execRepo.GetForUser(c.Request.Context(), uint(id), currentUID(c))
			if err != nil {
				c.JSON(404, gin.H{"code": 1, "message": "执行不存在"})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": e})
		})
		eg.GET("/executions/:id/events", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			// 归属校验先于读事件（越权与不存在同响应）
			if _, err := execRepo.GetForUser(c.Request.Context(), uint(id), currentUID(c)); err != nil {
				c.JSON(404, gin.H{"code": 1, "message": "执行不存在"})
				return
			}
			limit, _ := strconv.Atoi(c.DefaultQuery("limit", "200"))
			// 增量游标：after_id 传上次收到的最大事件 id，只回新增（Monitor 3s 轮询不重拉全量）
			afterID, _ := strconv.ParseUint(c.DefaultQuery("after_id", "0"), 10, 64)
			events, err := evRepo.ListByExecutionAfter(c.Request.Context(), uint(id), uint(afterID), limit)
			if err != nil {
				c.JSON(500, gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": gin.H{"list": events}})
		})
		// 事件流 SSE（S-13）：先补游标之后的历史，再随广播增量推；终态发 end 帧收流。
		eg.GET("/executions/:id/stream", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			streamEgoExecution(c, execRepo, evRepo, eventHub, currentUID(c), uint(id))
		})
		eg.POST("/executions/:id/redispatch", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			// runner 不在线时 Start 会把执行留在 created（待下发）；这是用户上线后的唯一重发口
			e, err := execSvc.Redispatch(c.Request.Context(), currentUID(c), uint(id))
			if err != nil {
				status := 400
				if err == egolite.ErrExecStateConflict {
					status = 409
				}
				c.JSON(status, gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": e, "message": "已重发（runner 不在线时保留在待下发态）"})
		})
		eg.POST("/executions/:id/abort", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			e, err := execSvc.Abort(c.Request.Context(), currentUID(c), uint(id), "用户停止")
			if err != nil {
				status := 400
				if err == egolite.ErrExecStateConflict {
					status = 409
				}
				c.JSON(status, gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": e})
		})
		eg.POST("/executions/:id/confirm", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			var body struct {
				Allow bool `json:"allow"`
			}
			_ = c.ShouldBindJSON(&body)
			e, err := execSvc.Confirm(c.Request.Context(), currentUID(c), uint(id), body.Allow)
			if err != nil {
				status := 400
				if err == egolite.ErrExecStateConflict {
					status = 409
				}
				c.JSON(status, gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": e})
		})

		// ---- 通用浏览器会话（M1/M2：ops 轮次协议）----
		eg.POST("/sessions", func(c *gin.Context) {
			var body struct {
				Goal          string   `json:"goal"`
				Engine        string   `json:"engine"`
				SpaceName     string   `json:"space_name"`
				Domains       []string `json:"domains"`
				WriteConfirm  string   `json:"write_confirm"`
				TimeoutSec    int      `json:"timeout_sec"`
				MaxRounds     int      `json:"max_rounds"`
				MaxActions    int      `json:"max_actions"`
				ConfirmTTLSec int      `json:"confirm_ttl_sec"`
			}
			if err := c.ShouldBindJSON(&body); err != nil {
				c.JSON(400, gin.H{"code": 1, "message": err.Error()})
				return
			}
			pol := browsercore.DefaultPolicy()
			pol.Domains = body.Domains
			if body.WriteConfirm != "" {
				pol.WriteConfirm = body.WriteConfirm
			}
			if body.TimeoutSec > 0 {
				pol.TimeoutSec = body.TimeoutSec
			}
			if body.MaxRounds > 0 {
				pol.MaxRounds = body.MaxRounds
			}
			if body.MaxActions > 0 {
				pol.MaxActions = body.MaxActions
			}
			if body.ConfirmTTLSec > 0 {
				pol.ConfirmTTLSec = body.ConfirmTTLSec
			}
			sess, err := sessionSvc.Open(c.Request.Context(), browsercore.OpenRequest{
				UserID: currentUID(c), Engine: body.Engine, Mode: browsercore.ModeOps,
				Goal: body.Goal, SpaceName: body.SpaceName, CreatedFrom: "ui", Policy: pol,
			})
			if err != nil {
				c.JSON(egoSessionStatus(err), gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": sess})
		})
		eg.GET("/sessions", func(c *gin.Context) {
			page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
			size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
			list, total, err := sessionSvc.List(c.Request.Context(), currentUID(c), page, size)
			if err != nil {
				c.JSON(500, gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": gin.H{"list": list, "total": total}})
		})
		eg.GET("/sessions/:id", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			sess, err := sessionSvc.Get(c.Request.Context(), currentUID(c), uint(id))
			if err != nil {
				c.JSON(egoSessionStatus(err), gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": sess})
		})
		eg.POST("/sessions/:id/round", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			var body struct {
				Ops     []browsercore.Op        `json:"ops"`
				Observe browsercore.ObserveSpec `json:"observe"`
			}
			if err := c.ShouldBindJSON(&body); err != nil {
				c.JSON(400, gin.H{"code": 1, "message": err.Error()})
				return
			}
			res, err := sessionSvc.Act(c.Request.Context(), browsercore.RoundRequest{
				UserID: currentUID(c), SessionID: uint(id), Ops: body.Ops, Observe: body.Observe,
			})
			if err != nil {
				c.JSON(egoSessionStatus(err), gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": res, "message": "已派发本轮；用 GET /sessions/:id/rounds 或 /observe 取结果"})
		})
		eg.POST("/sessions/:id/observe", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			var body struct {
				Round int `json:"round"`
			}
			_ = c.ShouldBindJSON(&body)
			info, err := sessionSvc.Observe(c.Request.Context(), browsercore.ObserveRequest{
				UserID: currentUID(c), SessionID: uint(id), Round: body.Round,
			})
			if err != nil {
				c.JSON(egoSessionStatus(err), gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": info})
		})
		eg.GET("/sessions/:id/rounds", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			limit, _ := strconv.Atoi(c.DefaultQuery("limit", "50"))
			list, err := sessionSvc.Rounds(c.Request.Context(), currentUID(c), uint(id), limit)
			if err != nil {
				c.JSON(egoSessionStatus(err), gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": gin.H{"list": list}})
		})
		// 审计导出（T22）：会话→轮次→事件→产物 一条链；owner 直开，admin 全量。
		// format=json（默认，完整嵌套）| csv（轮次扁平表）。内容与库直读一致。
		eg.GET("/sessions/:id/export", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			isAdmin := c.GetString("role") == "admin"
			switch c.DefaultQuery("format", "json") {
			case "csv":
				b, err := egolite.ExportSessionCSV(c.Request.Context(), gormDB, currentUID(c), uint(id), isAdmin)
				if err != nil {
					c.JSON(egoSessionStatus(err), gin.H{"code": 1, "message": err.Error()})
					return
				}
				c.Header("Content-Disposition", fmt.Sprintf("attachment; filename=ego-session-%d.csv", id))
				c.Data(200, "text/csv; charset=utf-8", b)
			default:
				exp, err := egolite.ExportSession(c.Request.Context(), gormDB, currentUID(c), uint(id), isAdmin)
				if err != nil {
					c.JSON(egoSessionStatus(err), gin.H{"code": 1, "message": err.Error()})
					return
				}
				c.JSON(200, gin.H{"code": 0, "data": exp})
			}
		})
		eg.POST("/sessions/:id/confirm", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			var body struct {
				Allow bool `json:"allow"`
			}
			_ = c.ShouldBindJSON(&body)
			sess, err := sessionSvc.Confirm(c.Request.Context(), currentUID(c), uint(id), body.Allow)
			if err != nil {
				c.JSON(egoSessionStatus(err), gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": sess})
		})
		eg.POST("/sessions/:id/abort", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			var body struct {
				Reason string `json:"reason"`
			}
			_ = c.ShouldBindJSON(&body)
			sess, err := sessionSvc.Abort(c.Request.Context(), currentUID(c), uint(id), body.Reason)
			if err != nil {
				c.JSON(egoSessionStatus(err), gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": sess})
		})
		eg.POST("/sessions/:id/save-as-flow", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			var body struct {
				Name string `json:"name"`
			}
			_ = c.ShouldBindJSON(&body)
			f, report, err := sessionSvc.SaveAsFlow(c.Request.Context(), currentUID(c), uint(id), body.Name)
			if err != nil {
				c.JSON(egoSessionStatus(err), gin.H{"code": 1, "message": err.Error(), "data": gin.H{"flow": f, "scan_report": report}})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": gin.H{"flow": f, "scan_report": report}})
		})
		eg.POST("/sessions/:id/finish", func(c *gin.Context) {
			id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
			var body struct {
				Keep []string `json:"keep"`
			}
			_ = c.ShouldBindJSON(&body)
			sess, err := sessionSvc.Finish(c.Request.Context(), currentUID(c), uint(id), body.Keep)
			if err != nil {
				c.JSON(egoSessionStatus(err), gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": sess})
		})

		// ---- 资产目录：用户端橱窗 ----
		// 只有 published + 扫描通过可见（offline 不出门）；执行侧仍以
		// POST /executions {origin:"market"} 为唯一发起口（服务端再校验一次）。
		eg.GET("/assets", func(c *gin.Context) {
			list, err := assetSvc.PublicList(c.Request.Context())
			if err != nil {
				c.JSON(500, gin.H{"code": 1, "message": err.Error()})
				return
			}
			c.JSON(200, gin.H{"code": 0, "data": gin.H{"list": list}})
		})

		// ---- 资产目录：平台端（= 本实例管理端，不依赖 platform-server） ----
		// 发布即"上架"：写 egolite_assets 一行（脚本快照 + 扫描结论 + sha256），
		// runner 下次拉清单即可见；用户点它就跑，闭环在 user-server 内。
		egAdmin := eg.Group("/admin", middleware.RequireAdminMiddleware())
		{
			// ego-browser 升级指令（specs/015 C10）：向全部在线 runner 广播 upgrade 帧；
			// runner 执行官方 `ego-browser upgrade` 并回传结果事件（能力升级归 ego 官方，
			// 本平台只做触发与结果监督）。
			egAdmin.POST("/runner/upgrade", func(c *gin.Context) {
				n := registry.BroadcastUpgrade()
				response2(c, map[string]any{"notified": n}, "已向在线 runner 下发升级指令")
			})
			// 审核发布（用户流程 submitted → 复扫 → 目录条目）
			egAdmin.POST("/assets/publish", func(c *gin.Context) {
				var body struct {
					FlowID uint `json:"flow_id"`
				}
				if err := c.ShouldBindJSON(&body); err != nil || body.FlowID == 0 {
					c.JSON(400, gin.H{"code": 1, "message": "flow_id 必填"})
					return
				}
				a, err := assetSvc.PublishFromFlow(c.Request.Context(), currentUID(c), body.FlowID)
				if err != nil {
					c.JSON(400, gin.H{"code": 1, "message": err.Error()})
					return
				}
				c.JSON(200, gin.H{"code": 0, "data": a})
			})
			// 直接发布脚本包（自包含通道：官方/基线资产）
			egAdmin.POST("/assets/pack", func(c *gin.Context) {
				var req egolite.PackPublishRequest
				if err := c.ShouldBindJSON(&req); err != nil {
					c.JSON(400, gin.H{"code": 1, "message": err.Error()})
					return
				}
				a, err := assetSvc.PublishFromPack(c.Request.Context(), currentUID(c), req)
				if err != nil {
					c.JSON(400, gin.H{"code": 1, "message": err.Error()})
					return
				}
				c.JSON(200, gin.H{"code": 0, "data": a})
			})
			// 审核工作台：跨用户按状态列流程（status 省略=全量，submitted=待审）
			egAdmin.GET("/flows", func(c *gin.Context) {
				page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
				size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
				list, total, err := flowRepo.ListByStatus(c.Request.Context(), c.Query("status"), page, size)
				if err != nil {
					c.JSON(500, gin.H{"code": 1, "message": err.Error()})
					return
				}
				c.JSON(200, gin.H{"code": 0, "data": gin.H{"list": list, "total": total}})
			})
			egAdmin.GET("/assets", func(c *gin.Context) {
				page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
				size, _ := strconv.Atoi(c.DefaultQuery("page_size", "20"))
				list, total, err := assetSvc.AdminList(c.Request.Context(), page, size)
				if err != nil {
					c.JSON(500, gin.H{"code": 1, "message": err.Error()})
					return
				}
				c.JSON(200, gin.H{"code": 0, "data": gin.H{"list": list, "total": total}})
			})
			egAdmin.POST("/assets/:id/offline", func(c *gin.Context) {
				id, _ := strconv.ParseUint(c.Param("id"), 10, 32)
				a, err := assetSvc.Offline(c.Request.Context(), uint(id))
				if err != nil {
					c.JSON(400, gin.H{"code": 1, "message": err.Error()})
					return
				}
				c.JSON(200, gin.H{"code": 0, "data": a})
			})
			// 遥测看板（S-9）：**只回聚合层**——按 kind 的 count/成功率/p95 桶，
			// 单条明细不出服务端（口径见 egolite/telemetry.go）。hours 上限 720。
			egAdmin.GET("/telemetry/summary", func(c *gin.Context) {
				hours, _ := strconv.Atoi(c.DefaultQuery("hours", "24"))
				buckets, total, truncated, err := egolite.TelemetrySummary(c.Request.Context(), telRepo, hours)
				if err != nil {
					c.JSON(500, gin.H{"code": 1, "message": err.Error()})
					return
				}
				c.JSON(200, gin.H{"code": 0, "data": gin.H{
					"window_hours": hours, "total": total, "truncated": truncated,
					"buckets": buckets,
				}})
			})
		}
	}
}

// currentUID 从 JWT 注入的 claims 取 user_id（host.go GetStatus 同取法 c.GetUint）
func currentUID(c *gin.Context) uint {
	return c.GetUint("user_id")
}

// response2 统一响应形状（response.Success 别名，避免 import 环）
func response2(c *gin.Context, data any, msg string) {
	c.JSON(200, gin.H{"code": 0, "message": msg, "data": data})
}

// egoSessionStatus 通用会话错误 → HTTP 状态（与既有执行域口径一致）
func egoSessionStatus(err error) int {
	switch {
	case errors.Is(err, browsercore.ErrNotFound), errors.Is(err, egolite.ErrSessionNotFound):
		return 404
	case errors.Is(err, browsercore.ErrGate):
		return 429
	case errors.Is(err, browsercore.ErrStateConflict), errors.Is(err, egolite.ErrEgoRunnerOffline):
		return 409
	case errors.Is(err, browsercore.ErrPolicyViolation), errors.Is(err, browsercore.ErrEngineUnavailable):
		return 400
	case errors.Is(err, egolite.ErrQuotaExceeded):
		return 429
	default:
		return 400
	}
}
