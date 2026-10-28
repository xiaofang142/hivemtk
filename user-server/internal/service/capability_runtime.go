// capability_runtime.go 能力运行视图（D01/F01）。
//
// 静态事实源在 capregistry（平台×能力登记，测试钉死文档）；本服务把它与运行时
// 状态组合：implemented（本二进制有真实执行路径）× configured（已配置账号/连接器）
// × ready，并给非 ready 一个机器可读 reason_code。前端/工具据此区分
// "平台原生不支持"、"实现了但没配账号"、"可用"三类，而不是同一个报错。
package service

import (
	"context"
	"sync"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/repository"
)

// 非.ready 的机器可读原因码
const (
	reasonNoRuntimePath          = "no_runtime_path"
	reasonNotConfiguredNoAccount = "not_configured_no_account"
)

// CapabilityRuntimeService 能力运行视图
type CapabilityRuntimeService struct {
	repo   *repository.ChannelOverviewRepository
	kbRepo *repository.ExternalKBConnectorRepo
}

// NewCapabilityRuntimeService 构造
func NewCapabilityRuntimeService(repo *repository.ChannelOverviewRepository, kbRepo *repository.ExternalKBConnectorRepo) *CapabilityRuntimeService {
	return &CapabilityRuntimeService{repo: repo, kbRepo: kbRepo}
}

// runtimeImplemented 运行时执行路径判定：返回该平台×能力在本二进制里是否有
// 真实可调用路径。这里必须与 capregistry/channels.go 的登记一一对应——
// 登记了却返回 false 会被契约测试抓出（声明支持但 Adapter 缺失 = 测试红）。
func runtimeImplemented(e capregistry.Entry) bool {
	switch e.Cap {
	case capregistry.CapDMReceive, capregistry.CapDMSend:
		switch e.Platform {
		case "douyin", "xiaohongshu", "kuaishou", "tiktok", "xianyu":
			return e.Via == "bridge" // bridge 收发链路（inbox_ingress / outbox）
		case "wecom", "telegram":
			return e.Via == "api" || e.Via == "webhook"
		default:
			return false
		}
	case capregistry.CapWebhook:
		switch e.Platform {
		case "wecom", "telegram", "feishu", "whatsapp":
			return e.Via == "webhook"
		default:
			return false
		}
	case capregistry.CapLeadMine:
		return e.Platform == "douyin" || e.Platform == "xiaohongshu" // unified-miner
	case capregistry.CapSMS:
		return e.Platform == "sms" && e.Via == "aliyun-sms"
	case capregistry.CapEmail:
		return e.Platform == "email" && e.Via == "smtp"
	case capregistry.CapKBSync:
		return e.Platform == "ima" || e.Platform == "webhttp" // kbconnector type 注册制
	default:
		return false
	}
}

// platformConfigured 该平台是否已有可用账号/连接器配置
func (s *CapabilityRuntimeService) platformConfigured(ctx context.Context, platform string) bool {
	if s.repo == nil {
		return false
	}
	var (
		n   int64
		err error
	)
	switch platform {
	case "telegram":
		n, err = s.repo.CountTelegram(ctx)
	case "wecom":
		n, err = s.repo.CountWeCom(ctx)
	case "feishu":
		n, err = s.repo.CountFeishu(ctx)
	case "whatsapp":
		n, err = s.repo.CountWhatsApp(ctx)
	case "sms":
		n, err = s.repo.CountSMS(ctx)
	case "email":
		n, err = s.repo.CountEmail(ctx)
	case "douyin", "xiaohongshu", "kuaishou", "tiktok", "xianyu":
		n, err = s.repo.CountBridge(ctx, platform)
	case "ima", "webhttp":
		// kb.sync：对应类型的启用连接器存在即视为已配置
		return s.kbConnectorConfigured(ctx, platform)
	default:
		return false
	}
	return err == nil && n > 0
}

func (s *CapabilityRuntimeService) kbConnectorConfigured(ctx context.Context, provider string) bool {
	if s.kbRepo == nil {
		return false
	}
	rows, err := s.kbRepo.List(ctx, 100)
	if err != nil {
		return false
	}
	for i := range rows {
		if rows[i].Type == provider && rows[i].Enabled {
			return true
		}
	}
	return false
}

// Describe 返回全部登记能力的运行视图（结果按平台+能力排序，capregistry.All 已保证）。
func (s *CapabilityRuntimeService) Describe(ctx context.Context) dto.CapabilityRuntime {
	entries := capregistry.All()
	out := dto.CapabilityRuntime{
		Capabilities: make([]dto.CapabilityRuntimeView, 0, len(entries)),
		Total:        len(entries),
	}
	for _, e := range entries {
		view := dto.CapabilityRuntimeView{
			Platform:    e.Platform,
			Capability:  string(e.Cap),
			Via:         e.Via,
			Note:        e.Note,
			Implemented: runtimeImplemented(e),
		}
		if view.Implemented {
			view.Configured = s.platformConfigured(ctx, e.Platform)
			view.Ready = view.Configured
			if !view.Configured {
				view.ReasonCode = reasonNotConfiguredNoAccount
			}
		} else {
			view.ReasonCode = reasonNoRuntimePath
		}
		if view.Ready {
			out.ReadyCount++
		}
		out.Capabilities = append(out.Capabilities, view)
	}
	return out
}

// capabilityContractOnce 契约测试只跑一次探针（并发安全由测试串行保证，这里兜底）
var capabilityContractOnce sync.Once
