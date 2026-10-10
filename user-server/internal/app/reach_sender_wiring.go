package app

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/bridge"
	"hivemtk-user/internal/service"

	"gorm.io/gorm"
)

type pipelineReachSender struct {
	inner  *IntegrationReachAdapter
	bridge *bridge.BridgeReachAdapter
}

var _ service.ReachSender = (*pipelineReachSender)(nil)

// NewPipelineReachSender 构造调度器的真实发送器：inner 负责 telegram/whatsapp/feishu/web/
// wecom/dingtalk/sms/email/wechat，bridge 负责抖音/快手/小红书/tiktok/闲鱼。
//
// 这里刻意不返回 nil，也没有"构造失败就降级成占位发送"那一档：NewIntegrationReachAdapterFromDB
// 在 db 为 nil 时交回的是零值 adapter，各渠道方法对自己那条回哨兵错。调度器因此永远拿到一个
// 会老实报错的发送器，而不是一个把没出网的作业记成已投递的替身。
func NewPipelineReachSender(db *gorm.DB) *pipelineReachSender {
	inner := NewIntegrationReachAdapterFromDB(db)
	return &pipelineReachSender{
		inner:  inner,
		bridge: bridge.NewBridgeReachAdapter(inner, GetBridgeIngressSvc()),
	}
}

func (p *pipelineReachSender) SendReach(ctx context.Context, channel, accountID, to, content string) (string, error) {
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
	case "douyin", "kuaishou", "xiaohongshu", "tiktok", "xianyu":
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
	wechatSvc := service.NewWechatService(db)
	tooluse.GlobalServiceRegistry().RegisterWechat(&wechatLikeAdapter{svc: wechatSvc})
}

type wechatLikeAdapter struct {
	svc *service.WechatService
}

func (a *wechatLikeAdapter) SendCustomMessage(ctx context.Context, accountID uint, openID, msgType, content string) (string, error) {
	return a.svc.SendCustomMessage(ctx, accountID, openID, msgType, content)
}
