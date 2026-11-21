package service

// marketing_flow_reach_gate.go FA-20：营销流程外发动作的统一判定链注册实现。
//
// 在此之前营销流程的 send_sms/send_email 动作各有一条裸发通道：短信直调 SmsService、
// 邮件直接 smtp.SendMail——两者都不经过审批（W-1）、地区行业合规（E1）与退订（DNC）判定，
// 与 AI 工具、主动触达、批量管线的外发纪律不一致。
//
// 本文件把两条通道收拢到与 ReachByCustomer 显式手机号/邮箱路径**同一出口**：
//
//	DNC → enforceOutboundGates（W-1 审批 → E1 合规：静默时段/日上限）→ 真实投递
//
// 装配约束：判定需要一个 ProactiveReachService 实例，由 app 包在 router.Setup 之后
// （W-1 checker 就绪后）经 AttachMarketingFlowReachGate 注入；未注入 = fail-closed，
// 营销流程的短信/邮件动作一律拒发——没有判定链的外发通道不允许存在。

import (
	"context"
	"fmt"
	"net/smtp"
	"strings"
	"sync"

	contentsvc "hivemtk-user/internal/content/service"
)

// marketingFlowGateMu 保护 reach 实例的读写。装配发生在启动期，但 AttachReachGate 的
// 既有纪律是"装配点可被重复调用"，写入不能假设发生在收流量之前。
var marketingFlowGateMu sync.RWMutex

// marketingFlowGateReach 营销流程外发判定用的触达服务实例（nil = 未装配 ⇒ 拒发）。
var marketingFlowGateReach *ProactiveReachService

// marketingFlowSmsTransport / marketingFlowEmailTransport 真实投递的注入点。
// 生产缺省分别是 lazySendSms（租户绑定 + SmsService）与营销流程 SMTP 传输；
// 测试替换为计数替身，断言「判定链拒绝 ⇒ 传输零调用」。
var (
	marketingFlowSmsTransport   = lazySendSms
	marketingFlowEmailTransport = marketingFlowSMTPTransport
)

func init() {
	// 与改造前的 marketing_flow_sms_init.go init() 同一注册时机；邮件发送器为 FA-20 新增。
	contentsvc.SetSmsSender(gatedMarketingFlowSms)
	contentsvc.SetEmailSender(gatedMarketingFlowEmail)
}

// AttachMarketingFlowReachGate 注入营销流程外发判定用的触达服务实例。
// 传 nil 视为拆卸：判定链缺失时注册的发送器 fail-closed，与 AttachReachGate
// 旗子 off 时把钩子归零是同一口径——绝不留一条"看起来装了门其实没判"的通道。
func AttachMarketingFlowReachGate(reach *ProactiveReachService) {
	marketingFlowGateMu.Lock()
	defer marketingFlowGateMu.Unlock()
	marketingFlowGateReach = reach
}

func marketingFlowGateInstance() *ProactiveReachService {
	marketingFlowGateMu.RLock()
	defer marketingFlowGateMu.RUnlock()
	return marketingFlowGateReach
}

// enforceMarketingFlowOutboundGates 营销流程外发的判定链唯一出口。
// 顺序与 ReachByCustomer 显式手机号/邮箱路径一致：DNC → 审批（W-1）→ 合规（E1）。
// 未装配判定实例时 fail-closed：错误必须可诊断，不能退化成静默成功。
func enforceMarketingFlowOutboundGates(ctx context.Context, channel, recipient, dncKey string) error {
	reach := marketingFlowGateInstance()
	if reach == nil {
		return fmt.Errorf("营销流程外发闸门未装配，已拒发（channel=%s）——外发必须经过统一判定链", channel)
	}
	if reach.checkDoNotContact(ctx, "", channel, dncKey) {
		return fmt.Errorf("%w: 收件对象已退订 %s 渠道，营销流程发送已跳过", ErrDoNotContact, channel)
	}
	return reach.enforceOutboundGates(ctx, ReachSubject{
		Key:       reachApprovalKey("", "", channel, recipient),
		Channel:   channel,
		Recipient: recipient,
	})
}

// gatedMarketingFlowSms 营销流程短信发送器：判定链通过后才走真实短信通道。
func gatedMarketingFlowSms(ctx context.Context, phone, content string) error {
	if phone == "" || content == "" {
		// 与 lazySendSms 的入参校验同口径，只是提前到判定之前。
		return fmt.Errorf("营销流程短信入参不完整（phone/content 必填）")
	}
	if err := enforceMarketingFlowOutboundGates(ctx, "sms", phone, phone); err != nil {
		return err
	}
	return marketingFlowSmsTransport(ctx, phone, content)
}

// gatedMarketingFlowEmail 营销流程邮件发送器：判定链通过后才走真实 SMTP 传输。
func gatedMarketingFlowEmail(ctx context.Context, cfg contentsvc.MarketingFlowEmailConfig) error {
	normalized := NormalizeEmail(cfg.To)
	if err := enforceMarketingFlowOutboundGates(ctx, "email", normalized, "email:"+normalized); err != nil {
		return err
	}
	return marketingFlowEmailTransport(ctx, cfg)
}

// marketingFlowSMTPTransport 营销流程邮件的真实传输。
// MIME 构造与拨号从原 sendActionSendEmail 原样搬移（行为不变，位置随闸口走）。
func marketingFlowSMTPTransport(_ context.Context, cfg contentsvc.MarketingFlowEmailConfig) error {
	mime := "MIME-version: 1.0\r\n"
	contentType := "Content-Type: text/html; charset=\"UTF-8\"\r\n"
	if !cfg.IsHTML {
		contentType = "Content-Type: text/plain; charset=\"UTF-8\"\r\n"
	}

	mailHeader := fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n%s%s\r\n",
		strings.NewReplacer("\r", "", "\n", "").Replace(cfg.From),
		strings.NewReplacer("\r", "", "\n", "").Replace(cfg.To),
		strings.NewReplacer("\r", "", "\n", "").Replace(cfg.Subject),
		mime, contentType)
	message := mailHeader + cfg.Body

	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	auth := smtp.PlainAuth("", cfg.User, cfg.Pass, cfg.Host)

	return smtp.SendMail(addr, auth, cfg.From, []string{cfg.To}, []byte(message))
}
