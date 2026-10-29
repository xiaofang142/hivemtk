package app

import (
	"context"
	"encoding/json"
	"fmt"
	"os"

	"gorm.io/gorm"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/service"

	"hivemtk-user/internal/repository"
)

// NewReachToolDepsWithAdapter 创建触达工具依赖（带真实 Adapter，打通全部业务渠道）。
//
// SendPipeline 的 ChannelAdapter 桥接【真实 adapter】，确保 reach.web.send 等工具
// 在生产/集成测试中真正调用 IntegrationReachAdapter.SendWeb（落库 + 实时推访客），
// 而非空壳 NoOp。同时注入合规提示钩子（service.LogComplianceReminder）。
//
// 调用方：ReachToolProvider / RegisterAgentReachTools 生产接线、端到端集成测试。
func NewReachToolDepsWithAdapter(db *gorm.DB, adapter tooluse.ReachAdapter) tooluse.ReachToolDeps {
	tooluse.SetComplianceReminderHook(service.LogComplianceReminder)
	return tooluse.ReachToolDeps{
		Adapter:      adapter,
		Pipeline:     &reachBatchPipelineAdapter{svc: service.NewReachPipelineService(repository.NewReachPipelineRepository(db))},
		DB:           db,
		SendPipeline: newReachSendPipelineWithDB(adapter, db),
	}
}

func newReachSendPipeline(adapter tooluse.ReachAdapter) tooluse.ReachSendPipelinePort {
	return newReachSendPipelineWithDB(adapter, nil)
}

// newReachSendPipelineWithDB 组装触达发送管线；db 非 nil 时接持久 quiet 延迟队列（D02），
// nil 时（旧调用方/测试）退回进程内队列——入队语义一致，仅重启丢队。
func newReachSendPipelineWithDB(adapter tooluse.ReachAdapter, db *gorm.DB) tooluse.ReachSendPipelinePort {

	cfg := service.NewDefaultRateLimitedPipelineConfigWithCache(&reachChannelAdapterBridge{adapter: adapter})
	// E1：挂上地区×行业合规策略判定器。
	//
	// 用全局单例而不是在这里 new 一个：判定器必须与 proactive 外发路径（工具/API/cron 共用
	// 出口 enforceOutboundGates）**共用同一份计数**，否则同一客户在两条路径上各有一份额度，
	// 日上限直接翻倍。策略本身仍来自参数中心，本行只是把「谁来做判定」接上。
	cfg.CompliancePolicy = service.GlobalCompliancePolicy()
	if db != nil {
		// D04：可信同意闸（撤回即生效；存量无凭据按 legacy_assertion 留痕，不批量伪造）
		// 与故障拒发的退订检查（DNC 状态查不清 ⇒ fail-closed）。
		cfg.ConsentChecker = service.NewContactConsentService(repository.NewContactConsentRepository(db))
		cfg.ConsentMode = service.ConsentModeRevocationOnly
		cfg.DoNotContactStrict = service.NewDoNotContactService(repository.NewCustomerDoNotContactRepository(db))
		// D05：意图级硬配额（PG 预占真源）。维度=每客户每日；上限走参数中心
		// reach/global_per_user_daily_limit，0=不设额，兜底 3（与种子 DefaultValue 及
		// reach_pipeline 的读点一致——此前这里写 0，参数行缺失时会静默变成"不设额"，
		// 与种子描述的行为相反，被 check-config-param-defaults 判 MISMATCH 抓出）。
		// 仅当调用方带 action_intent_id 幂等键时才实际预占。
		cfg.QuotaReserver = service.NewReachQuotaManager(repository.NewReachQuotaRepository(db))
		cfg.QuotaScopesFn = func(ctx context.Context, req *service.ReachSendRequest) []service.QuotaScopeDef {
			limit := service.GlobalConfigParam().GetInt(ctx, "reach", "global_per_user_daily_limit", 3)
			if limit <= 0 {
				return nil
			}
			who := req.CustomerID
			if v, ok := req.Metadata["one_id"]; ok && v != "" {
				who = v
			}
			if who == "" {
				who = req.RecipientID
			}
			if who == "" {
				return nil
			}
			return []service.QuotaScopeDef{{Key: "customer:" + who, Limit: limit}}
		}
	}
	if os.Getenv("REACH_DISABLE_QUIET_HOURS") != "true" {
		cfg.QuietHoursEnabled = true
		var sp service.SendPipeline
		if db != nil {
			// D02：生产路径用持久延迟队列，重启不丢队
			q := service.NewPersistentQuietHoursQueue(repository.NewReachDeferredOutboundRepository(db))
			cfg.QuietHoursDeferrer = q
			sp = service.NewSendPipeline(cfg)
			q.Start(context.Background(), sp)
			return &reachSendPipelineAdapter{sp: sp}
		}
		q := service.GetGlobalQuietHoursQueue()
		cfg.QuietHoursDeferrer = q
		sp = service.NewSendPipeline(cfg)
		q.Start(context.Background(), sp)
		return &reachSendPipelineAdapter{sp: sp}
	}
	sp := service.NewSendPipeline(cfg)
	return &reachSendPipelineAdapter{sp: sp}
}

type reachSendPipelineAdapter struct {
	sp service.SendPipeline
}

func (a *reachSendPipelineAdapter) Send(ctx context.Context, req *tooluse.ReachSendRequest) *tooluse.ReachSendResponse {
	resp := a.sp.Send(ctx, toServiceReachRequest(req))
	return fromServiceSendResponse(resp)
}

type reachBatchPipelineAdapter struct {
	svc *service.ReachPipelineService
}

func (a *reachBatchPipelineAdapter) EnqueueJob(ctx context.Context, req *tooluse.ReachJobRequest) (tooluse.ReachJobView, error) {
	job, err := a.svc.EnqueueJob(ctx, &service.EnqueueJobRequest{
		PipelineID: req.PipelineID,
		Channel:    req.Channel,
		CustomerID: req.CustomerID,
		AccountID:  req.AccountID,
		Payload:    req.Payload,
		MaxRetry:   req.MaxRetry,
		RunAt:      req.RunAt,
	})
	if err != nil {
		return tooluse.ReachJobView{}, err
	}
	return tooluse.ReachJobView{ID: job.ID, State: job.State}, nil
}

func (a *reachBatchPipelineAdapter) ListJobs(ctx context.Context, channel, state string, page, pageSize int) ([]any, int64, error) {
	jobs, total, err := a.svc.ListJobs(ctx, channel, state, page, pageSize)
	if err != nil {
		return nil, 0, err
	}
	out := make([]any, 0, len(jobs))
	for i := range jobs {
		raw, err := json.Marshal(&jobs[i])
		if err == nil {
			var m map[string]any
			if json.Unmarshal(raw, &m) == nil {
				out = append(out, m)
				continue
			}
		}
		out = append(out, map[string]any{"id": jobs[i].ID, "state": jobs[i].State})
	}
	return out, total, nil
}

type reachChannelAdapterBridge struct {
	adapter tooluse.ReachAdapter
}

func (b *reachChannelAdapterBridge) Send(ctx context.Context, req *service.ReachSendRequest) (string, error) {
	if b.adapter == nil {
		return "", service.ErrSendChannelNotConfig
	}
	switch req.Channel {
	case "sms":
		return b.adapter.SendSMS(ctx, req.RecipientID, req.Content, req.TemplateID, req.Params)
	case "email":
		return b.adapter.SendEmail(ctx, req.RecipientID, req.Subject, req.Content, req.Attachments)
	case "wecom":
		return b.adapter.SendWeCom(ctx, req.AccountID, req.RecipientID, req.MsgType, req.Content)
	case "weixin":
		return b.adapter.SendWeixin(ctx, req.RecipientID, req.MsgType, req.Content)
	case "douyin", "douyin_web":
		return b.adapter.SendDouyin(ctx, req.AccountID, req.RecipientID, req.MsgType, req.Content)
	case "kuaishou", "kuaishou_web":
		return b.adapter.SendKuaishou(ctx, req.AccountID, req.RecipientID, req.MsgType, req.Content)
	case "xiaohongshu", "xhs", "xhs_web":
		return b.adapter.SendXHS(ctx, req.AccountID, req.RecipientID, req.MsgType, req.Content)
	case "tiktok", "tiktok_web":
		return b.adapter.SendTikTok(ctx, req.AccountID, req.RecipientID, req.MsgType, req.Content)
	case "xianyu", "xianyu_web":
		return b.adapter.SendXianyu(ctx, req.AccountID, req.RecipientID, req.MsgType, req.Content)
	case "dingtalk":
		return b.adapter.SendDingTalk(ctx, req.RecipientID, req.MsgType, req.Content)
	case "telegram":
		return b.adapter.SendTelegram(ctx, req.AccountID, req.RecipientID, req.Content)
	case "whatsapp":
		return b.adapter.SendWhatsApp(ctx, req.AccountID, req.RecipientID, req.Content)
	case "feishu":
		return b.adapter.SendFeishu(ctx, req.AccountID, req.RecipientID, req.Content)
	case "web":
		return b.adapter.SendWeb(ctx, req.RecipientID, req.Content)
	case "card":
		subchannel := "douyin"
		if req.Metadata != nil {
			if sc, ok := req.Metadata["subchannel"]; ok && sc != "" {
				subchannel = sc
			}
		}
		return b.adapter.SendCard(ctx, subchannel, req.AccountID, req.RecipientID, req.CardID)
	default:
		return "", fmt.Errorf("unknown channel: %s", req.Channel)
	}
}

func toServiceReachRequest(req *tooluse.ReachSendRequest) *service.ReachSendRequest {
	if req == nil {
		return nil
	}
	out := &service.ReachSendRequest{
		Channel:     req.Channel,
		AccountID:   req.AccountID,
		RecipientID: req.RecipientID,
		CustomerID:  req.CustomerID,
		OperatorID:  req.OperatorID,
		MsgType:     req.MsgType,
		Content:     req.Content,
		Subject:     req.Subject,
		TemplateID:  req.TemplateID,
		Params:      req.Params,
		Attachments: req.Attachments,
		CardID:      req.CardID,
		Metadata:    req.Metadata,
	}
	if req.Fallback != nil {
		out.Fallback = &service.FallbackConfig{
			Enabled:       req.Fallback.Enabled,
			BackupChannel: req.Fallback.BackupChannel,
			BackupAccount: req.Fallback.BackupAccount,
			MaxAttempts:   req.Fallback.MaxAttempts,
		}
	}
	return out
}

func fromServiceSendResponse(resp *service.SendResponse) *tooluse.ReachSendResponse {
	if resp == nil {
		return nil
	}
	out := &tooluse.ReachSendResponse{
		Success:        resp.Success,
		MessageID:      resp.MessageID,
		Channel:        resp.Channel,
		AccountID:      resp.AccountID,
		FallbackUsed:   resp.FallbackUsed,
		PrimaryChannel: resp.PrimaryChannel,
		RetryCount:     resp.RetryCount,
		Error:          resp.Error,
		DurationMs:     resp.DurationMs,
		SentAt:         resp.SentAt,
	}
	if len(resp.StepResults) > 0 {
		out.StepResults = make([]tooluse.ReachSendStepLog, len(resp.StepResults))
		for i, s := range resp.StepResults {
			out.StepResults[i] = tooluse.ReachSendStepLog{
				Step:       s.Step,
				Success:    s.Success,
				StartedAt:  s.StartedAt,
				EndedAt:    s.EndedAt,
				DurationMs: s.DurationMs,
				Output:     s.Output,
				Error:      s.Error,
				Skipped:    s.Skipped,
			}
		}
	}
	return out
}
