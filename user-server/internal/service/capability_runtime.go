// capability_runtime.go 能力运行视图（D01/F01）。
//
// 静态事实源在 capregistry（平台×能力登记，测试钉死文档）；本服务把它与运行时
// 状态组合：implemented（本二进制有真实执行路径）× configured（已配置账号/连接器）
// × ready，并给非 ready 一个机器可读 reason_code。桥接发送的账号配置与
// 店铺动作授权分别判定，平台汇总无法替代精确店铺查询。
package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"
)

var ErrCapabilityTenantContextMissing = errors.New("capability resolution requires tenant context")

// 未就绪时的机器可读原因码。
const (
	reasonNoRuntimePath            = "no_runtime_path"
	reasonNotConfiguredNoAccount   = "not_configured_no_account"
	reasonCapabilityMappingMissing = "capability_mapping_missing"
)

// CapabilityRuntimeService 能力运行视图
type CapabilityRuntimeService struct {
	repo   *repository.ChannelOverviewRepository
	kbRepo *repository.ExternalKBConnectorRepo
	cells  capregistry.CapabilityEvidenceReader
}

// NewCapabilityRuntimeService 构造
func NewCapabilityRuntimeService(repo *repository.ChannelOverviewRepository, kbRepo *repository.ExternalKBConnectorRepo, cells ...capregistry.CapabilityEvidenceReader) *CapabilityRuntimeService {
	svc := &CapabilityRuntimeService{repo: repo, kbRepo: kbRepo}
	if len(cells) == 1 {
		svc.cells = cells[0]
	}
	return svc
}

// runtimeImplemented 运行时执行路径判定：返回该平台×能力在本二进制里是否有
// 真实可调用路径。这里必须与 capregistry/channels.go 的登记一一对应——
// 登记了却返回 false 会被契约测试抓出（声明支持但 Adapter 缺失 = 测试红）。
func runtimeImplemented(e capregistry.Entry) bool {
	switch e.Cap {
	case capregistry.CapDMReceive, capregistry.CapDMSend:
		switch e.Platform {
		case "douyin", "xiaohongshu", "kuaishou", "tiktok", "xianyu",
			"jd", "bilibili", "weibo", "personal_wx", "pdd",
			"taobao", "douyin_shop", "kuaishou_shop", "shopee", "walmart_us":
			return e.Via == "bridge" // bridge 收发链路（inbox_ingress / outbox）
		case "wecom", "telegram", "wechat",
			// 025 平台批：wecomcs/onebot/slack/wecombot/custom 的入站与出站
			// 都由通用 webhook 路由 + 出站 sender 承载（onebot 另有反向 WS）。
			"wecomcs", "onebot", "slack", "wecombot", "custom":
			return e.Via == "api" || e.Via == "webhook" || e.Via == "ws"
		default:
			return false
		}
	case capregistry.CapWebhook:
		switch e.Platform {
		case "wecom", "telegram", "feishu", "whatsapp", "wechat",
			"wecomcs", "onebot", "slack", "wecombot", "custom":
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
	case "wecomcs":
		n, err = s.repo.CountWeComCS(ctx)
	case "onebot":
		n, err = s.repo.CountOneBot(ctx)
	case "slack":
		n, err = s.repo.CountSlack(ctx)
	case "wecombot":
		n, err = s.repo.CountWeCombBot(ctx)
	case "custom":
		n, err = s.repo.CountCustom(ctx)
	case "wecom":
		n, err = s.repo.CountWeCom(ctx)
	case "wechat", "weixin":
		n, err = s.repo.CountWechat(ctx)
	case "feishu":
		n, err = s.repo.CountFeishu(ctx)
	case "whatsapp":
		n, err = s.repo.CountWhatsApp(ctx)
	case "sms":
		n, err = s.repo.CountSMS(ctx)
	case "email":
		n, err = s.repo.CountEmail(ctx)
	case "douyin", "xiaohongshu", "kuaishou", "tiktok", "xianyu",
		"jd", "bilibili", "weibo", "personal_wx", "pdd",
		"taobao", "douyin_shop", "kuaishou_shop", "shopee", "walmart_us":
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
			switch {
			case !view.Configured:
				view.ReasonCode = reasonNotConfiguredNoAccount
			case e.Cap == capregistry.CapDMSend && e.Via == capregistry.ModeBridge:
				// 平台行没有店铺和动作格身份，单店上下文证据也不能覆盖该平台的全部发送。
				view.ReasonCode = reasonCapabilityMappingMissing
			default:
				view.Ready = true
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

// ResolveCell resolves one exact shop/action tuple in tenant scope. It uses only
// trusted server-side evidence. Without it, the response is explicitly blocked.
func (s *CapabilityRuntimeService) ResolveCell(ctx context.Context, shopID, product, region, action, carrier string) (capregistry.CellResolution, error) {
	return s.resolveCellAt(ctx, shopID, product, region, action, carrier, time.Now().UTC())
}

func (s *CapabilityRuntimeService) resolveCellAt(ctx context.Context, shopID, product, region, action, carrier string, now time.Time) (capregistry.CellResolution, error) {
	tenantID, ok := tenant.FromContext(ctx)
	if !ok || tenant.IsSystemScope(ctx) {
		return capregistry.CellResolution{}, ErrCapabilityTenantContextMissing
	}
	key := capregistry.CellKey{
		TenantID: tenantID, ShopID: strings.TrimSpace(shopID), Product: strings.TrimSpace(product),
		Region: strings.TrimSpace(region), Action: strings.TrimSpace(action), Carrier: strings.TrimSpace(carrier),
	}
	if s.cells != nil {
		return (capregistry.PersistentCapabilityResolver{Reader: s.cells}).Resolve(ctx, key, now), nil
	}
	return capregistry.ResolveCell(key, nil, now), nil
}
