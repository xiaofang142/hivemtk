package service

// marketing_flow_send_gate.go FA-20：营销流程发送型动作的统一闸口接缝。
//
// EC-27（模板/素材审核、变量校验、渠道政策、过期停发与证据链）相关缺口说明：
//  - 本闸口只校验流程状态（暂停/停用）与传输配置完整性，尚未做模板审核状态、
//    变量 schema 校验、权益/价格/库存有效期核验、渠道政策裁决；
//  - 过期权益/售罄不会在此阻断发送，AI 草稿也未强制经人工审核；
//  - 白名单测试发送与生产发送路径尚未在最外层物理隔离；
//  - 渲染内容版本/审核人/平台模板 ID/发送证据链尚未绑定到 DeliveryAttempt。
// 这些能力归属 EC-27（M27），当前闸口保持 fail-closed 的最小状态栅栏，不宣称已具备
// 模板审核或渠道政策裁决能力。
//
// EC-28（M28 旅程/实验编排）交叉引用：营销实验/旅程分支执行不得绕过本闸口与判定链；
//   控制组/holdout 不得进入营销派发路径；撤回/暂停对在途执行也生效；重复触发/换渠
//   不能突破压力上限（reach_quota）；分支节点落库失败不得派副作用（executeFlow 失败语义）。
//
// 职责边界（与 internal/service/marketing_flow_reach_gate.go 的判定链拼成一条链）：
//   - 本文件（content/service 层）：发送器的注入点（短信沿用 SetSmsSender、邮件新增
//     SetEmailSender）、执行期流程引用的 ctx 传递、发送前的流程状态栅栏（撤回/暂停）。
//   - internal/service 层：注册「闸门版」发送器——DNC → W-1 审批 → E1 合规判定后
//     才允许真实投递；未装配闸门 = fail-closed 拒发。
//
// 为什么栅栏在 content 层而不是判定链层：流程状态读的是本层的 MarketingFlowRepository，
// 判定链层（internal/service）拿不到它；而发送器注册点在 internal/service（它 import
// 本包，反向 import 会成环）。两层各守一段，测试各自钉住。

import (
	"context"
	"errors"
	"fmt"

	"hivemtk-user/internal/content/model"
)

// MarketingFlowEmailConfig 营销流程邮件动作的传输配置（由动作节点 config 解析而来）。
// 判定链通过 SetEmailSender 注入的实现拿到它后才允许拨号；本包不再直接接触 SMTP。
type MarketingFlowEmailConfig struct {
	Host    string
	Port    int
	User    string
	Pass    string
	From    string
	To      string
	Subject string
	Body    string
	IsHTML  bool
}

// emailSenderFunc 营销流程邮件传输注入点。nil = 未注册 ⇒ 邮件动作 fail-closed。
var emailSenderFunc func(ctx context.Context, cfg MarketingFlowEmailConfig) error

// SetEmailSender 注册营销流程邮件发送器（internal/service 装配时调用）。
// 与 SetSmsSender 同一模式：判定与传输都收拢在注册实现里，动作层只见注入点。
func SetEmailSender(fn func(ctx context.Context, cfg MarketingFlowEmailConfig) error) {
	emailSenderFunc = fn
}

// MarketingFlowRefKeyType 执行期流程引用的 ctx 键（未导出类型防碰撞）。
type MarketingFlowRefKeyType struct{}

// WithMarketingFlowRef 把执行中的流程挂到 ctx 上：发送型动作据此在发送时点复核
// 流程状态，让「撤回/暂停」对在途执行也生效，而不只在触发入口生效。
func WithMarketingFlowRef(ctx context.Context, flow *model.MarketingFlow) context.Context {
	if flow == nil {
		return ctx
	}
	return context.WithValue(ctx, MarketingFlowRefKeyType{}, flow)
}

func marketingFlowRefFromContext(ctx context.Context) *model.MarketingFlow {
	flow, _ := ctx.Value(MarketingFlowRefKeyType{}).(*model.MarketingFlow)
	return flow
}

// ensureFlowActiveAtSend 发送型动作的状态栅栏：流程在执行中途被暂停/停用（或读取失败）
// 时拒绝发送。仅在流程已有持久化 ID 时生效——内存构造的流程图（如测试夹具）没有 ID，
// 无从读库也不属于生产路径；生产路径 TriggerFlow 只放行 active 流程，ID 恒非零。
func (s *MarketingFlowService) ensureFlowActiveAtSend(ctx context.Context) error {
	ref := marketingFlowRefFromContext(ctx)
	if ref == nil || ref.ID == 0 || s.flowRepo == nil {
		return nil
	}
	current, err := s.flowRepo.GetByID(ctx, ref.ID)
	if err != nil {
		// 读不到当前状态就不能证明「仍被授权外发」：fail-closed，宁可不发。
		return fmt.Errorf("发送前读取流程状态失败，为防未授权外发已跳过：%w", err)
	}
	if current == nil || current.Status != model.FlowStatusActive {
		status := ""
		if current != nil {
			status = string(current.Status)
		}
		return fmt.Errorf("营销流程已被暂停或停用（status=%q），发送动作已跳过", status)
	}
	return nil
}

// buildMarketingFlowEmailConfig 从动作节点 config 解析邮件传输配置。
// 校验口径与改造前的直发实现逐字一致（缺失即报错，不给默认密码/默认收件人）。
func buildMarketingFlowEmailConfig(config map[string]any) (MarketingFlowEmailConfig, error) {
	cfg := MarketingFlowEmailConfig{}

	cfg.Host, _ = config["smtp_host"].(string)
	cfg.User, _ = config["smtp_user"].(string)
	cfg.Pass, _ = config["smtp_pass"].(string)

	if cfg.Host == "" {
		return cfg, errors.New("SMTP 主机未配置，请在动作配置或环境变量中设置 smtp_host")
	}
	if cfg.User == "" || cfg.Pass == "" {
		return cfg, errors.New("SMTP 用户名或密码未配置")
	}

	switch v := config["smtp_port"].(type) {
	case int:
		cfg.Port = v
	case float64:
		cfg.Port = int(v)
	default:
		cfg.Port = 587
	}

	cfg.To, _ = config["to"].(string)
	if cfg.To == "" {
		return cfg, errors.New("收件人邮箱未指定")
	}

	cfg.Subject, _ = config["subject"].(string)
	if cfg.Subject == "" {
		cfg.Subject = "营销邮件"
	}

	cfg.Body, _ = config["body"].(string)
	if cfg.Body == "" {
		return cfg, errors.New("邮件正文未指定")
	}

	cfg.From, _ = config["from"].(string)
	if cfg.From == "" {
		cfg.From = cfg.User
	}

	if isHTML, ok := config["is_html"].(bool); ok {
		cfg.IsHTML = isHTML
	}
	return cfg, nil
}
