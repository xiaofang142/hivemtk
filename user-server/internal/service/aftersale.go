package service

import (
	"context"
	"fmt"

	"hivemtk-user/internal/aiagent/agent/portcontract"
	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// AfterSaleService 售后服务（客服侧发起售后 → 回写电商）。
//
// 这是客服系统对"订单"唯一允许写入的形态：写的是售后单（退款/退货/换货），
// 由电商执行落地；本服务只负责本地落库 + 状态跟踪。订单本身的创建/支付/履约
// 由外部电商负责，客服绝不触碰。
type AfterSaleService struct {
	repo   *repository.AfterSaleRepository
	client AfterSaleExternalClient
}

// NewAfterSaleService 构造（无回写客户端，走 best-effort 本地落库）。
// 仓储由装配层注入：服务不再直取全局 db 句柄。
func NewAfterSaleService(repo *repository.AfterSaleRepository) *AfterSaleService {
	return NewAfterSaleServiceWithClient(repo, nil)
}

// NewAfterSaleServiceWithClient 构造（注入回写电商客户端）
func NewAfterSaleServiceWithClient(repo *repository.AfterSaleRepository, client AfterSaleExternalClient) *AfterSaleService {
	return &AfterSaleService{repo: repo, client: client}
}

// Create 发起售后：本地落库 + （可选）回写电商。
//
// 回写说明：优先使用注入的 AfterSaleExternalClient；否则按数据库 system_config_kv
// [agent.tool_integrations].after_sale 配置（见 tool_integration_config.go）按需构造客户端。
// 配置启用且基地址非空（client.Configured()=true）时，把售后请求推给电商执行落地，
// 用返回的电商侧售后单号(external_id) + 状态更新本记录；否则走 best-effort 本地落库，
// 状态等待电商 Webhook 回推或定时拉取刷新（与旧行为一致）。
// 后台写入配置后立即对新请求生效，无需重启。
func (s *AfterSaleService) Create(ctx context.Context, req *portcontract.AfterSaleRequest) (*portcontract.AfterSaleView, error) {
	if req == nil || req.OrderID == "" {
		return nil, fmt.Errorf("order_id 不能为空")
	}
	if req.Type == "" {
		req.Type = portcontract.AfterSaleRefund
	}
	client := s.resolveClient(ctx)
	if client != nil && client.Configured() {
		if err := authorizeAfterSaleSubmit(ctx, req); err != nil {
			return nil, err
		}
	}
	as := &model.AfterSale{
		Platform:      req.Platform,
		OrderID:       req.OrderID,
		CustomerPhone: req.CustomerPhone,
		CustomerName:  req.CustomerName,
		Type:          req.Type,
		Reason:        req.Reason,
		Amount:        req.Amount,
		Status:        portcontract.AfterSalePending,
	}
	if err := s.repo.Create(ctx, as); err != nil {
		return nil, err
	}
	if client != nil && client.Configured() {
		if res, err := client.Create(ctx, req); err != nil {
			if capregistry.IsSubmissionDenied(err) {
				return nil, err
			}
			// Local case remains durable, but callers must see the failed external operation.
			logger.Warnf("[售后] 外部提交结果待核 case_id=%d", as.ID)
			return afterSaleToView(as), fmt.Errorf("aftersale external submission outcome unknown")
		} else if res != nil && res.ExternalID != "" {
			as.ExternalID = res.ExternalID
			if res.Status != "" {
				as.Status = res.Status
			}
			if err := s.repo.Update(ctx, as); err != nil {
				logger.Warnf("[售后] 回写结果保存失败 case_id=%d", as.ID)
				return afterSaleToView(as), fmt.Errorf("aftersale external result persistence failed")
			}
		}
	}
	return afterSaleToView(as), nil
}

func (s *AfterSaleService) resolveClient(ctx context.Context) AfterSaleExternalClient {
	if s.client != nil && s.client.Configured() {
		return s.client
	}
	cfg, err := LoadToolIntegrationConfig(ctx)
	if err != nil || cfg == nil || !cfg.AfterSale.Enabled || cfg.AfterSale.BaseURL == "" {
		return nil
	}
	return NewAfterSaleExternalClientFromConfig(cfg.AfterSale)
}

// Query 查询售后单（按 平台+订单号 或 客户手机）
func (s *AfterSaleService) Query(ctx context.Context, platform, orderID, customerPhone string) ([]*portcontract.AfterSaleView, error) {
	var records []model.AfterSale
	var err error
	if orderID != "" {
		records, err = s.repo.ListByOrder(ctx, platform, orderID)
	} else {
		records, err = s.repo.ListByCustomer(ctx, customerPhone)
	}
	if err != nil {
		return nil, err
	}
	views := make([]*portcontract.AfterSaleView, 0, len(records))
	for i := range records {
		views = append(views, afterSaleToView(&records[i]))
	}
	return views, nil
}

func afterSaleToView(as *model.AfterSale) *portcontract.AfterSaleView {
	if as == nil {
		return nil
	}
	return &portcontract.AfterSaleView{
		ID:            as.ID,
		Platform:      as.Platform,
		OrderID:       as.OrderID,
		CustomerPhone: as.CustomerPhone,
		CustomerName:  as.CustomerName,
		Type:          as.Type,
		Reason:        as.Reason,
		Amount:        as.Amount,
		Status:        as.Status,
		ExternalID:    as.ExternalID,
	}
}
