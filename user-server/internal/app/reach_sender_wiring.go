package app

import (
	"context"
	"errors"
	"fmt"
	"os"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/bridge"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

// batchApprovalKey 与 service.reachApprovalKey 同语义（客户身份优先，退到 渠道:收件人）。
func batchApprovalKey(oneID, customerID, channel, recipient string) string {
	if oneID != "" {
		return oneID
	}
	if customerID != "" {
		return customerID
	}
	if recipient == "" {
		return ""
	}
	return channel + ":" + recipient
}

// batchReachApprovalGate FA-REACH-01：批量触达的审批闸，与 HTTP/cron 外发腿共用
// 同一 W-1 裁决源（approvalCheckerRef + reachPreSendGate），不另起一套判定。
// 返回非 nil 即零外发；shadow 态只记账不拦（与 reachPreSendGate 同口径）。
func batchReachApprovalGate(ctx context.Context, req *service.ReachSendRequest) error {
	if req == nil {
		return errors.New("reach send gate: nil request")
	}
	mode := parseReachGateMode(os.Getenv(ReachGateFlagEnv))
	gate := &reachPreSendGate{mode: mode, checker: approvalCheckerRef}
	sub := service.ReachSubject{
		Channel:   req.Channel,
		Recipient: req.RecipientID,
	}
	sub.Key = batchApprovalKey("", req.CustomerID, req.Channel, req.RecipientID)
	if v, ok := req.Metadata["one_id"]; ok && v != "" {
		sub.OneID = v
		sub.Key = batchApprovalKey(v, req.CustomerID, req.Channel, req.RecipientID)
	}
	allowed, reason := gate.CheckReachPreSend(ctx, sub)
	if !allowed && approvalDenialBlocks(mode, reason) {
		return fmt.Errorf("%w: 批量触达未获外发授权（%s）", service.ErrReachApprovalDenied, reason)
	}
	return nil
}

type pipelineReachSender struct {
	inner  *IntegrationReachAdapter
	bridge *bridge.BridgeReachAdapter
	// gate FA-REACH-01：所有批量外发统一经过的强制闸口（审批/DNC/同意/配额/合规）。
	// 非 nil 时 SendReach 先过闸，零外发的裁决在这里落地；gate 的 ChannelAdapter
	// 正是本类型的 sendDirect，因此绝不递归。nil 仅在无 db 的旧测试里作兜底。
	gate service.SendPipeline
}

var _ service.ReachSender = (*pipelineReachSender)(nil)
var _ service.ChannelAdapter = (*pipelineReachSender)(nil)

// NewPipelineReachSender 构造调度器的真实发送器：inner 负责 telegram/whatsapp/feishu/web/
// wecom/dingtalk/sms/email/wechat，bridge 负责抖音/快手/小红书/tiktok/闲鱼。
//
// 这里刻意不返回 nil，也没有"构造失败就降级成占位发送"那一档：NewIntegrationReachAdapterFromDB
// 在 db 为 nil 时交回的是零值 adapter，各渠道方法对自己那条回哨兵错。调度器因此永远拿到一个
// 会老实报错的发送器，而不是一个把没出网的作业记成已投递的替身。
func NewPipelineReachSender(db *gorm.DB) *pipelineReachSender {
	inner := NewIntegrationReachAdapterFromDB(db)
	p := &pipelineReachSender{
		inner:  inner,
		bridge: bridge.NewBridgeReachAdapter(inner, GetBridgeIngressSvc()),
	}
	// FA-REACH-01：批量发送器不再直连适配器，统一走带闸口的 SendPipeline。
	// 闸口的 ChannelAdapter = p 本身（Send→sendDirect，不递归）；DNC/同意/配额/合规
	// 用与工具/API 同一套 db 装配，审批走 MandatoryGate（不可被 Steps 裁掉）。
	cfg := service.DefaultSendPipelineConfig(p)
	if db != nil {
		cfg.ConsentChecker = service.NewContactConsentService(repository.NewContactConsentRepository(db))
		cfg.ConsentMode = service.ConsentModeRevocationOnly
		cfg.DoNotContactStrict = service.NewDoNotContactService(repository.NewCustomerDoNotContactRepository(db))
		cfg.CompliancePolicy = service.GlobalCompliancePolicy()
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
	cfg.MandatoryGate = batchReachApprovalGate
	p.gate = service.NewSendPipeline(cfg)
	return p
}

// Send 实现 service.ChannelAdapter：闸口放行后的真实投递（唯一出网点 = sendDirect）。
func (p *pipelineReachSender) Send(ctx context.Context, req *service.ReachSendRequest) (string, error) {
	if req == nil {
		return "", errors.New("nil reach send request")
	}
	return p.sendDirect(ctx, req.Channel, req.AccountID, req.RecipientID, req.Content)
}

// SendReach 批量触达入口：统一构造发送意图并过闸，裁决拒绝则零外发。
func (p *pipelineReachSender) SendReach(ctx context.Context, channel, accountID, to, content string) (string, error) {
	if p.gate != nil {
		resp := p.gate.Send(ctx, &service.ReachSendRequest{
			Channel:     channel,
			AccountID:   accountID,
			RecipientID: to,
			CustomerID:  to,
			Content:     content,
			MsgType:     "text",
		})
		if !resp.Success {
			if resp.Err != nil {
				return "", resp.Err
			}
			if resp.Error == "" {
				return "", errors.New("reach send gate denied")
			}
			return "", errors.New(resp.Error)
		}
		return resp.MessageID, nil
	}
	return p.sendDirect(ctx, channel, accountID, to, content)
}

// sendDirect 保留原逐渠道路由（闸口放行后才调用；gate 的 ChannelAdapter 也走这里）。
func (p *pipelineReachSender) sendDirect(ctx context.Context, channel, accountID, to, content string) (string, error) {
	switch channel {
	case "telegram":
		return p.inner.SendTelegram(ctx, accountID, to, content)
	case "whatsapp":
		return p.inner.SendWhatsApp(ctx, accountID, to, content)
	case "feishu":
		return p.inner.SendFeishu(ctx, accountID, to, content)
	case "web":
		return p.inner.SendWeb(ctx, accountID, content)
	case "wecom":
		return p.inner.SendWeCom(ctx, accountID, to, "text", content)
	case "dingtalk":
		return p.inner.SendDingTalk(ctx, accountID, "text", content)
	case "sms":
		return p.inner.SendSMS(ctx, to, content, "", nil)
	case "email":
		return p.inner.SendEmail(ctx, to, "触达消息", content, nil)
	case "wechat":
		return p.inner.SendWeixin(ctx, to, "text", content)
	case "card":
		// 卡片在批量管道里发不出去，缺的是参数而不是实现：SendCard 要 card_id
		// （卡片后台的数字 id），而 ReachSender 这条端口只递 (渠道, 账号, 收件人, 文本)，
		// 文本由 prepareContent 拼出来，装不了一个数字 id。单条卡片外发走 reach.card.send
		// 工具那条（它有 card_id 的来源）。管道要支持卡片，得先定"card_id 从 job 的哪格来"，
		// 在那之前这里必须报错：调度器不能把没出网的作业记成已投递。
		return "", errors.New("card: 批量管道没有 card_id 的来源，卡片外发请走 reach.card.send 工具")
	case "douyin", "kuaishou", "xiaohongshu", "tiktok", "xianyu",
		"jd", "bilibili", "weibo", "personal_wx":
		if p.bridge == nil {
			return "", fmt.Errorf("bridge 适配器未接线，无法触达渠道 %s", channel)
		}
		switch channel {
		case "douyin":
			return p.bridge.SendDouyin(ctx, accountID, to, "text", content)
		case "kuaishou":
			return p.bridge.SendKuaishou(ctx, accountID, to, "text", content)
		case "xiaohongshu":
			return p.bridge.SendXHS(ctx, accountID, to, "text", content)
		case "tiktok":
			return p.bridge.SendTikTok(ctx, accountID, to, "text", content)
		case "xianyu":
			return p.bridge.SendXianyu(ctx, accountID, to, "text", content)
		case "jd":
			return p.bridge.SendJD(ctx, accountID, to, "text", content)
		case "bilibili":
			return p.bridge.SendBilibili(ctx, accountID, to, "text", content)
		case "weibo":
			return p.bridge.SendWeibo(ctx, accountID, to, "text", content)
		case "personal_wx":
			return p.bridge.SendPersonalWX(ctx, accountID, to, "text", content)
		}
	}
	return "", fmt.Errorf("unsupported channel: %s", channel)
}

// RegisterWeixinSender 把公众号发送服务交给 tooluse 层的全局注册中心。
//
// 只剩这一条走注册中心：IntegrationReachAdapter.SendWeixin 要用 service 包的公众号服务，
// 而 tooluse 不能反向 import service（会成环），所以由装配点注入一次实现、由适配器读取。
// 其余渠道的发送服务由 NewIntegrationReachAdapterFromDB 按 db 自己构造，不经过这里。
func RegisterWeixinSender(db *gorm.DB) {
	wechatSvc := service.NewWechatService()
	tooluse.GlobalServiceRegistry().RegisterWechat(&wechatLikeAdapter{svc: wechatSvc})
}

type wechatLikeAdapter struct {
	svc *service.WechatService
}

func (a *wechatLikeAdapter) SendCustomMessage(ctx context.Context, accountID uint, openID, msgType, content string) (string, error) {
	return a.svc.SendCustomMessage(ctx, accountID, openID, msgType, content)
}
