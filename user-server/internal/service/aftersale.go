package service

import (
	"context"
	"fmt"

	"hivemtk-user/internal/aiagent/agent/portcontract"
	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// AfterSaleRepositoryOps 售后单持久化边界。定义为接口，使"远端回写状态持久化失败"可被合成测试注入，
// 用于守住 EC-13「写失败不能吞为完成」不变量。
type AfterSaleRepositoryOps interface {
	Create(ctx context.Context, as *model.AfterSale) error
	Update(ctx context.Context, as *model.AfterSale) error
	ListByOrder(ctx context.Context, platform, orderID string) ([]model.AfterSale, error)
	ListByCustomer(ctx context.Context, phone string) ([]model.AfterSale, error)
}

// AfterSaleService 售后服务（客服侧发起售后 → 回写电商）。
//
// 这是客服系统对"订单"唯一允许写入的形态：写的是售后单（退款/退货/换货），
// 由电商执行落地；本服务只负责本地落库 + 状态跟踪。订单本身的创建/支付/履约
// 由外部电商负责，客服绝不触碰。
//
// EC-13（本地/模拟范围内的增量）：HTTPAfterSaleClient 会尽力解析 HTTP200 内的业务错误码；
// 远端调用失败时标记 unknown（禁止盲目重试），回写持久化失败会回退本地状态避免“写失败当完成”。
// 仍缺：严格的“本地立案/远端受理/处理中/资金成功”状态机与枚举校验、退款余额/币种一致性校验、
// 以及面向平台的真实 after-sale/退款流程编排（需 live 证据）。
//
// EC-14 (M14, not implemented): no reverse logistics / warehouse receipt / QC inspection /
// exchange SKU allocation or reship fulfillment workflow, no idempotency for warehouse/receipt
// events, and no per-step evidence storage here. "同意退货 ≠ 退款" and stepwise evidence
// (收货/质检/换货发运) with no duplicate shipment/refund on repeated events are future work.
//
// EC-16 (M16, not implemented / not live-closed): “淘宝/天猫、PDD、抖店”三平台动作矩阵扩展尚未在此闭环。
// 当前可观察事实：capregistry 仅 "douyin_shop" 登记 api_adapter 动作格 aftersale.write / refund.write
// （experimental，需平台侧预检与持久证据/执行绑定；legacy HTTPAfterSaleClient 路径仍会被
// authorizeAfterSaleSubmit 以 execution_target_unbound 拒绝）。"taobao"/"tmall" 无对应 commerce action
// cells 且订单改单侧对 direct_api 执行拒绝；"pdd" 无 commerce action cells。真实平台动作、PDD
// 赔付/取件、抖店买家确认、淘系限制分支等覆盖需按 M16 验收标准配套 live/simulated 测试与证据。
//
// EC-17 (M17, not implemented / not live-closed): jd_pop 在 capregistry 中声明 aftersale.write 为 official_ui
//（merchant workflow in 京麦），系统不自动提交 API；但本服务/订单/案件/审批/执行证据与结果回告的端到端闭环
// 仍未实现。xiaohongshu（千帆店）、kuaishou_shop、wechat_store、youzan 在代码库中无对应 commerce action cells
// 与执行适配，属于缺失（非“已接入待验证”）。
//
// EC-18 (M18, 未实现 / 未 live 关闭，见 docs/未来任务清单.md §7.1.8、§7.3.5):
//   - Shopify/WooCommerce：无平台键与 commerce action cells（refund.write/aftersale.write），亦无本仓库内
//     专用适配器；§7.3.5 所述 GraphQL/REST、网关手工退款等差异未在此实现，authorizeAfterSaleSubmit 会因
//     未登记能力格而拒绝（并叠加 execution_target_unbound 的既有限制）。
//   - TikTok Shop（tiktok_shop）：仅存在通用 "tiktok" DM 桥接登记；无 tiktok_shop commerce cells/适配器，
//     §7.3.5 的退货/退款/取消等原生路线未实现。
//   - Shopee/Lazada/eBay/Walmart US：Shopee、Walmart US 仅有桥接 DM（Experimental）；Lazada、eBay 无登记。
//     无对应售后/退款/退货的 commerce action cells 与执行适配器（按 §7.3.5 的官方工作台/原生路线亦未落地）。
//
// EC-19 (M19, 未实现 / 未 live 关闭，见 docs/未来任务清单.md §7.1.8、§7.3.5):
//   - Amazon（自履约/FBA/平台托管）、1688、速卖通、Etsy、得物（Poizon/DeWu）、苏宁官方工作台：无对应平台键、
//     无 commerce action cells 与售后/退款执行适配器；capregistry merchantWorkflowCells 仅 vipshop refund.observe（platform_managed）存在，
//     与「可使用的案件流程、指定角色/步骤/证据/超期升级」及「退款与取消、平台托管与卖家操作不混同」不等价。
//   - 即时零售分业态（部分退款/整单取消/配送中协商/医药专员等）：无实现与证据链。所有上述均为设计缺口，
//     不能通过本地/模拟 HTTP 客户端冒充平台官方工作台或托管流程完成。
type AfterSaleService struct {
	repo   AfterSaleRepositoryOps
	client AfterSaleExternalClient
}

// NewAfterSaleService 构造（无回写客户端，走 best-effort 本地落库）。
// 仓储由装配层注入：服务不再直取全局 db 句柄。
func NewAfterSaleService(repo AfterSaleRepositoryOps) *AfterSaleService {
	return NewAfterSaleServiceWithClient(repo, nil)
}

// NewAfterSaleServiceWithClient 构造（注入回写电商客户端）
func NewAfterSaleServiceWithClient(repo AfterSaleRepositoryOps, client AfterSaleExternalClient) *AfterSaleService {
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
			// EC-11 (M11): remote call failed -> outcome unknown. Do NOT auto-retry blindly (no idempotency guarantee).
			// Mark as unknown for manual reconciliation; automatic retries forbidden to avoid double refunds/side effects.
			logger.Errorf("[售后][EC-11] 回写电商失败（结果未知，禁止盲目重试，需人工对账/追踪）：id=%d order=%s err=%v", as.ID, as.OrderID, err)
			as.Status = "unknown"
			if uerr := s.repo.Update(ctx, as); uerr != nil {
				logger.Errorf("[售后][EC-11] 标记 unknown 状态持久化失败 id=%d: %v", as.ID, uerr)
			}
			return afterSaleToView(as), fmt.Errorf("aftersale external submission outcome unknown: %w", err)
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

// applyRemoteWriteBack 持久化远端回写状态；持久化失败时回退本地状态，守住 EC-13「写失败不能吞为完成」。
// 提取为独立方法使该不变量可被合成测试直接覆盖，不依赖 authorizeAfterSaleSubmit 硬闸门（legacy client 路径不可达）。
func (s *AfterSaleService) applyRemoteWriteBack(ctx context.Context, as *model.AfterSale, res *AfterSaleExternalResult) {
	localStatus := as.Status
	as.ExternalID = res.ExternalID
	if res.Status != "" {
		as.Status = res.Status
	}
	if err := s.repo.Update(ctx, as); err != nil {
		logger.Errorf("[售后] 更新回写结果失败 id=%d，回退本地状态 %s→%s：%v", as.ID, as.Status, localStatus, err)
		as.Status = localStatus // 外部单号已留痕，但远端状态未落库，不对外宣称资金成功
	}
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
