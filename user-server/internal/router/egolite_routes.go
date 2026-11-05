package router

// ego-lite 腿路由装配（设计文档 S3/S6/S7/S9；调研 §6.5 鉴权定案）。
//
// 2026-11-05 二次审计：本文件只做依赖装配与 URL→Controller 映射；
// 所有处理器已迁入 internal/controller/egolite_controller*.go。
//
// 端点总表：
//   - GET  /api/egolite/runner-ws          runner WS（eg_ token，免 JWT）
//   - GET  /api/egolite/runner/status      连接状态（用户看自己 / admin 全量）
//   - GET  /api/egolite/runner/tokens      我的 runner 令牌列表（只回 hint，不回明文）
//   - POST /api/egolite/runner/tokens      签发令牌（明文仅此一次返回）
//   - POST /api/egolite/runner/tokens/:id/revoke 撤销（立即失效）
//   - POST /api/egolite/runner/token/reset eg_ 兜底 token 轮换（admin，老部署兼容）
//   - POST /api/egolite/flows              保存草稿（保存即扫描）
//   - GET  /api/egolite/flows              流程列表
//   - GET  /api/egolite/flows/:id          流程详情
//   - POST /api/egolite/flows/:id/rescan   复扫
//   - POST /api/egolite/flows/:id/publish  发布
//   - POST /api/egolite/flows/:id/dry-run  试跑（draft 通道）
//   - DELETE /api/egolite/flows/:id        删除（连带摘除定时触发器）
//   - POST /api/egolite/flows/:id/submit   提交平台审核
//   - GET/POST/DELETE /api/egolite/flows/:id/cron 定时触发器
//   - POST /api/egolite/executions         发起执行
//   - GET  /api/egolite/executions         执行列表
//   - GET  /api/egolite/executions/:id     执行详情
//   - GET  /api/egolite/executions/:id/events  事件增量
//   - GET  /api/egolite/executions/:id/stream  SSE 事件流
//   - POST /api/egolite/executions/:id/redispatch|abort|confirm
//   - POST /api/egolite/sessions           打开浏览器会话
//   - GET  /api/egolite/sessions           会话列表
//   - GET  /api/egolite/sessions/:id       会话详情
//   - POST /api/egolite/sessions/:id/round|observe|confirm|abort|save-as-flow|finish
//   - GET  /api/egolite/sessions/:id/rounds|export
//   - GET  /api/egolite/assets             用户端资产橱窗
//   - POST /api/egolite/admin/runner/upgrade  升级指令广播
//   - POST /api/egolite/admin/assets/publish|pack  审核发布/直接发布
//   - GET  /api/egolite/admin/flows|assets   审核工作台
//   - POST /api/egolite/admin/assets/:id/offline
//   - GET  /api/egolite/admin/telemetry/summary
//   - GET  /api/egolite/assets/pull          runner 资产清单（eg_ token）
//   - POST /api/egolite/runner/artifacts     runner 产物上传（eg_ token）
//   - GET  /api/egolite/assets/trust.pem     资产信任公钥（公开）
//   - POST /api/egolite/runner/telemetry     runner 遥测上报（eg_ token）

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/browsercore"
	"hivemtk-user/internal/controller"
	"hivemtk-user/internal/egolite"
	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/pkg/utils/logger"
	hrepo "hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"
)

func SetupEgoLiteRoutes(auth *gin.RouterGroup, engine *gin.Engine, gormDB *gorm.DB) {
	kvRepo := hrepo.NewPlatformConfigKVRepositoryWithDB(gormDB)
	registry := egolite.NewEgoRunnerRegistry()

	execRepo := egolite.NewEgoExecutionRepositoryWithDB(gormDB)
	evRepo := egolite.NewEgoEventRepositoryWithDB(gormDB)
	flowRepo := egolite.NewEgoFlowRepositoryWithDB(gormDB)
	assetRepo := egolite.NewEgoAssetRepositoryWithDB(gormDB)
	tokenRepo := egolite.NewEgoRunnerTokenRepositoryWithDB(gormDB)
	claimRepo := egolite.NewEgoWriteClaimRepositoryWithDB(gormDB)

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

	ctl := controller.NewEgoLiteController(
		egolite.NewEgoRunnerAdminService(tokenRepo, kvRepo, registry),
		execSvc,
		flowSvc,
		assetSvc,
		cronSvc,
		sessionSvc,
		egolite.NewEgoTelemetryService(telRepo),
		egolite.NewEgoGovernanceServiceWithDB(gormDB),
	)

	// ---- runner 通道（eg_ token 即身份，免 JWT）----
	engine.GET("/api/egolite/assets/pull", ctl.PullAssets)
	engine.POST("/api/egolite/runner/artifacts", ctl.UploadArtifact)
	engine.GET("/api/egolite/assets/trust.pem", ctl.TrustPEM)
	engine.POST("/api/egolite/runner/telemetry", ctl.IngestTelemetry)

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
		govSvc := egolite.NewEgoGovernanceServiceWithDB(gormDB)
		for range ticker.C {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			_, _ = execSvc.WatchdogSweep(ctx)
			_, _ = sessionSvc.WatchdogSweep(ctx)
			if day := time.Now().UTC().YearDay(); day != lastRetentionDay {
				lastRetentionDay = day
				n, err := govSvc.SweepRetention(ctx)
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
		eg.GET("/runner/status", ctl.RunnerStatus)
		// ---- runner 令牌（每用户多条；明文仅在签发那一刻返回一次）----
		// 多用户各自 runner 的前提：KV 单值 token 只能绑一个 userID，第二台 runner
		// 连上就是顶号。行表让"每个用户把自己的浏览器接进基座"成立。
		eg.GET("/runner/tokens", ctl.ListRunnerTokens)
		eg.POST("/runner/tokens", ctl.IssueRunnerToken)
		eg.POST("/runner/tokens/:id/revoke", ctl.RevokeRunnerToken)
		// 兼容口径：轮换 KV 兜底 token（老部署/单机自用；admin 限定）
		eg.POST("/runner/token/reset", ctl.ResetRunnerToken)

		// ---- 流程域 ----
		eg.POST("/flows", ctl.SaveFlow)
		eg.GET("/flows", ctl.ListFlows)
		eg.GET("/flows/:id", ctl.GetFlow)
		eg.POST("/flows/:id/rescan", ctl.RescanFlow)
		eg.POST("/flows/:id/publish", ctl.PublishFlow)
		// 试跑（draft 通道）：过扫描门后建执行——用户写脚本时唯一的"先看看效果"路径
		eg.POST("/flows/:id/dry-run", ctl.DryRunFlow)
		eg.DELETE("/flows/:id", ctl.DeleteFlow)
		eg.POST("/flows/:id/submit", ctl.SubmitFlow)

		// ---- 流程定时触发器（specs/016 S10；只挂纯读流程，service 闸 fail-close）----
		eg.GET("/flows/:id/cron", ctl.GetFlowCron)
		eg.POST("/flows/:id/cron", ctl.UpsertFlowCron)
		eg.DELETE("/flows/:id/cron", ctl.DeleteFlowCron)

		// ---- 执行域 ----
		eg.POST("/executions", ctl.StartExecution)
		eg.GET("/executions", ctl.ListExecutions)
		eg.GET("/executions/:id", ctl.GetExecution)
		eg.GET("/executions/:id/events", ctl.ListExecutionEvents)
		// 事件流 SSE（S-13）：先补游标之后的历史，再随广播增量推；终态发 end 帧收流。
		eg.GET("/executions/:id/stream", ctl.StreamExecution)
		eg.POST("/executions/:id/redispatch", ctl.RedispatchExecution)
		eg.POST("/executions/:id/abort", ctl.AbortExecution)
		eg.POST("/executions/:id/confirm", ctl.ConfirmExecution)

		// ---- 通用浏览器会话（M1/M2：ops 轮次协议）----
		eg.POST("/sessions", ctl.OpenSession)
		eg.GET("/sessions", ctl.ListSessions)
		eg.GET("/sessions/:id", ctl.GetSession)
		eg.POST("/sessions/:id/round", ctl.SessionRound)
		eg.POST("/sessions/:id/observe", ctl.SessionObserve)
		eg.GET("/sessions/:id/rounds", ctl.SessionRounds)
		// 审计导出（T22）：会话→轮次→事件→产物 一条链；owner 直开，admin 全量。
		// format=json（默认，完整嵌套）| csv（轮次扁平表）。内容与库直读一致。
		eg.GET("/sessions/:id/export", ctl.ExportSession)
		eg.POST("/sessions/:id/confirm", ctl.SessionConfirm)
		eg.POST("/sessions/:id/abort", ctl.SessionAbort)
		eg.POST("/sessions/:id/save-as-flow", ctl.SaveSessionAsFlow)
		eg.POST("/sessions/:id/finish", ctl.FinishSession)

		// ---- 资产目录：用户端橱窗 ----
		// 只有 published + 扫描通过可见（offline 不出门）；执行侧仍以
		// POST /executions {origin:"market"} 为唯一发起口（服务端再校验一次）。
		eg.GET("/assets", ctl.ListAssets)

		// ---- 资产目录：平台端（= 本实例管理端，不依赖 platform-server） ----
		// 发布即"上架"：写 egolite_assets 一行（脚本快照 + 扫描结论 + sha256），
		// runner 下次拉清单即可见；用户点它就跑，闭环在 user-server 内。
		egAdmin := eg.Group("/admin", middleware.RequireAdminMiddleware())
		{
			// ego-browser 升级指令（specs/015 C10）：向全部在线 runner 广播 upgrade 帧；
			// runner 执行官方 `ego-browser upgrade` 并回传结果事件（能力升级归 ego 官方，
			// 本平台只做触发与结果监督）。
			egAdmin.POST("/runner/upgrade", ctl.BroadcastUpgrade)
			// 审核发布（用户流程 submitted → 复扫 → 目录条目）
			egAdmin.POST("/assets/publish", ctl.PublishAssetFromFlow)
			// 直接发布脚本包（自包含通道：官方/基线资产）
			egAdmin.POST("/assets/pack", ctl.PublishAssetPack)
			// 审核工作台：跨用户按状态列流程（status 省略=全量，submitted=待审）
			egAdmin.GET("/flows", ctl.ListAdminFlows)
			egAdmin.GET("/assets", ctl.ListAdminAssets)
			egAdmin.POST("/assets/:id/offline", ctl.OfflineAsset)
			// 遥测看板（S-9）：**只回聚合层**——按 kind 的 count/成功率/p95 桶，
			// 单条明细不出服务端（口径见 egolite/telemetry.go）。hours 上限 720。
			egAdmin.GET("/telemetry/summary", ctl.TelemetrySummary)
		}
	}
}
