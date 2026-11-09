package service

// order_change_request.go TB-05：改单申请单业务面（能力路由 + 状态机）。
//
// 契约（冻结，对齐设计文档 C6）：
//   - 客服侧只写申请单；商业订单变更发生在获准后的外部系统侧；
//   - capability 创建时冻结：平台能力规则表判定（v1 静态规则 + 数据化预留）；
//   - v1 执行通路只开放 manual / buyer_initiated；direct_api / via_erp 显式
//     ErrCapabilityNotAvailable（真实凭证/连接器未接，不承诺未验证能力）；
//   - 订单必须存在于只读镜像（external_orders）；
//   - 状态机 pending→approved→executing→executed｜rejected；pending→rejected｜cancelled；
//     终态不可逆；非法迁移 4009；
//   - approve/reject/execute 全部留操作者（executor/approver 不可空，fail-closed）。

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	bizerr "hivemtk-user/internal/domain/errors"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// 平台能力路由结论（创建时冻结进申请单；v1 静态规则表，后续数据化）。
const (
	CapabilityDirectAPI     = "direct_api"      // 平台有直改 API（如京东 SOP 改址），需商家授权🔵
	CapabilityBuyerInitiate = "buyer_initiated" // 平台为买家发起型（抖音/闲鱼），客服引导买家自助
	CapabilityViaERP        = "via_erp"         // 经 ERP 通道执行（管家婆）
	CapabilityManual        = "manual"          // 派人工坐席在平台后台操作
)

// ErrCapabilityNotAvailable 能力通路未接（凭证/连接器未配），显式拒绝而非静默降级。
var ErrCapabilityNotAvailable = fmt.Errorf("该平台执行通路未接入（凭证/连接器待配置）")

// platformCapabilityRules 平台→能力路由（v1 静态规则；未知平台=manual 兜底）。
// 依据：京东 JOS 改址 API 公开存在（需商家授权）；抖音/闲鱼无卖家直改（买家发起型）；
// 管家婆=ERP 通道。外部事实来源见 docs/research/INTENT-TOOL-UNIFIED-BASE-DESIGN-2026-11.md。
var platformCapabilityRules = map[string]string{
	"jd":        CapabilityDirectAPI,
	"taobao":    CapabilityDirectAPI,
	"tmall":     CapabilityDirectAPI,
	"douyin":    CapabilityBuyerInitiate,
	"xianyu":    CapabilityBuyerInitiate,
	"grasp":     CapabilityViaERP,
	"guanjiapo": CapabilityViaERP,
}

// capabilityForPlatform 平台能力路由（未知平台 manual 兜底；数据化预留点）。
func capabilityForPlatform(platform string) string {
	if cap, ok := platformCapabilityRules[platform]; ok {
		return cap
	}
	return CapabilityManual
}

// OrderChangeService 改单申请单服务。
type OrderChangeService struct {
	repo      repository.OrderChangeRequestRepository
	orderRepo *repository.ExternalOrderRepository
}

func NewOrderChangeService(repo repository.OrderChangeRequestRepository, orderRepo *repository.ExternalOrderRepository) *OrderChangeService {
	if orderRepo == nil {
		orderRepo = repository.NewExternalOrderRepository()
	}
	return &OrderChangeService{repo: repo, orderRepo: orderRepo}
}

// OrderChangeCreateInput 创建入参。
type OrderChangeCreateInput struct {
	Platform   string         `json:"platform"`
	OrderID    string         `json:"order_id"`
	CustomerID string         `json:"customer_id"`
	SessionID  string         `json:"session_id"`
	ChangeType string         `json:"change_type"`
	Payload    map[string]any `json:"payload"`
	Reason     string         `json:"reason"`
}

// validChangeTypes 变更类别闭合枚举。
var validChangeTypes = map[string]bool{"address": true, "sku": true, "quantity": true, "note": true, "other": true}

// Create 创建申请单：订单镜像校验（不编造）+ 能力路由冻结 + pending 初始态。
func (s *OrderChangeService) Create(ctx context.Context, in OrderChangeCreateInput) (*model.OrderChangeRequest, error) {
	if in.Platform == "" || in.OrderID == "" {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "platform 与 order_id 为必填")
	}
	if !validChangeTypes[in.ChangeType] {
		return nil, bizerr.New(bizerr.CodeParamInvalid, fmt.Sprintf("change_type %q 不在枚举内（address/sku/quantity/note/other）", in.ChangeType))
	}
	if len(in.Payload) == 0 {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "payload 不能为空（变更内容）")
	}
	// 订单必须存在于只读镜像（不编造订单）；库未就绪（单测无库）跳过镜像校验
	if s.orderRepo != nil && repository.DBReady() {
		view, err := s.orderRepo.GetByOrderID(ctx, in.Platform, in.OrderID)
		if err != nil {
			return nil, err
		}
		if view == nil {
			return nil, bizerr.New(bizerr.CodeNotFound, fmt.Sprintf("订单镜像中未找到 %s/%s，无法创建改单申请", in.Platform, in.OrderID))
		}
	}
	buf, err := json.Marshal(in.Payload)
	if err != nil {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "payload 序列化失败")
	}
	rec := &model.OrderChangeRequest{
		ChangeNo:    fmt.Sprintf("chg_%d_%s", time.Now().UnixNano(), in.ChangeType),
		Platform:    in.Platform,
		OrderID:     in.OrderID,
		CustomerID:  in.CustomerID,
		SessionID:   in.SessionID,
		ChangeType:  in.ChangeType,
		PayloadJSON: string(buf),
		Reason:      in.Reason,
		Capability:  capabilityForPlatform(in.Platform),
		Status:      "pending",
	}
	if err := s.repo.Create(ctx, rec); err != nil {
		return nil, err
	}
	return rec, nil
}

// Approve pending→approved（操作者必填）。
func (s *OrderChangeService) Approve(ctx context.Context, id uint, operator string) (*model.OrderChangeRequest, error) {
	if operator == "" {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "审批操作者必填（留痕 fail-closed）")
	}
	rec, err := s.mustGet(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec.Status != "pending" {
		return nil, bizerr.New(bizerr.CodeConflict, fmt.Sprintf("仅 pending 可审批，当前 %s", rec.Status))
	}
	now := time.Now()
	if err := s.repo.UpdateStatus(ctx, id, repository.OrderChangeStatusPatch{
		Status: "approved", ApprovedBy: operator, ApprovedAt: &now,
	}); err != nil {
		return nil, err
	}
	return s.mustGet(ctx, id)
}

// Reject pending→rejected（原因必填）。
func (s *OrderChangeService) Reject(ctx context.Context, id uint, operator, reason string) (*model.OrderChangeRequest, error) {
	if operator == "" || reason == "" {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "拒绝操作者与原因必填")
	}
	rec, err := s.mustGet(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec.Status != "pending" {
		return nil, bizerr.New(bizerr.CodeConflict, fmt.Sprintf("仅 pending 可拒绝，当前 %s", rec.Status))
	}
	now := time.Now()
	if err := s.repo.UpdateStatus(ctx, id, repository.OrderChangeStatusPatch{
		Status: "rejected", RejectedBy: operator, RejectReason: reason, RejectedAt: &now,
	}); err != nil {
		return nil, err
	}
	return s.mustGet(ctx, id)
}

// Cancel pending→cancelled。
func (s *OrderChangeService) Cancel(ctx context.Context, id uint, operator string) (*model.OrderChangeRequest, error) {
	if operator == "" {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "取消操作者必填")
	}
	rec, err := s.mustGet(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec.Status != "pending" {
		return nil, bizerr.New(bizerr.CodeConflict, fmt.Sprintf("仅 pending 可取消，当前 %s", rec.Status))
	}
	now := time.Now()
	if err := s.repo.UpdateStatus(ctx, id, repository.OrderChangeStatusPatch{
		Status: "cancelled", CancelledAt: &now,
	}); err != nil {
		return nil, err
	}
	return s.mustGet(ctx, id)
}

// Execute approved→executing→executed，按冻结的能力路由分派。
// v1 通路：manual=派坐席留痕；buyer_initiated=引导话术标记；direct_api/via_erp 显式不可用。
func (s *OrderChangeService) Execute(ctx context.Context, id uint, operator string) (*model.OrderChangeRequest, error) {
	if operator == "" {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "执行操作者必填")
	}
	rec, err := s.mustGet(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec.Status != "approved" {
		return nil, bizerr.New(bizerr.CodeConflict, fmt.Sprintf("仅 approved 可执行，当前 %s", rec.Status))
	}
	switch rec.Capability {
	case CapabilityManual:
		return s.finishExecute(ctx, rec.ID, operator, "已派人工坐席在平台后台处理（manual），完成后请回填执行结果。")
	case CapabilityBuyerInitiate:
		return s.finishExecute(ctx, rec.ID, operator, "该平台为买家发起型变更（buyer_initiated）：已按引导话术请买家自助修改，客服侧留单跟踪。")
	case CapabilityDirectAPI, CapabilityViaERP:
		// 显式拒绝而非静默降级：真实凭证/连接器未接（🔵），不承诺未验证能力。
		return nil, bizerr.New(bizerr.CodeConflict, fmt.Sprintf("capability=%s: %v", rec.Capability, ErrCapabilityNotAvailable))
	default:
		return nil, bizerr.New(bizerr.CodeConflict, fmt.Sprintf("未知能力路由 %q", rec.Capability))
	}
}

func (s *OrderChangeService) finishExecute(ctx context.Context, id uint, operator, note string) (*model.OrderChangeRequest, error) {
	now := time.Now()
	if err := s.repo.UpdateStatus(ctx, id, repository.OrderChangeStatusPatch{
		Status: "executed", ExecutedBy: operator, ExecutedAt: &now, ExecutorNote: note,
	}); err != nil {
		return nil, err
	}
	logger.Infof("[order_change] 申请单 %d 已执行（%s / %s）", id, operator, note)
	return s.mustGet(ctx, id)
}

func (s *OrderChangeService) mustGet(ctx context.Context, id uint) (*model.OrderChangeRequest, error) {
	rec, err := s.repo.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec == nil {
		return nil, bizerr.New(bizerr.CodeNotFound, "改单申请单不存在")
	}
	return rec, nil
}

// Get / List 读面。
func (s *OrderChangeService) Get(ctx context.Context, id uint) (*model.OrderChangeRequest, error) {
	return s.mustGet(ctx, id)
}

func (s *OrderChangeService) List(ctx context.Context, status string, page, pageSize int) ([]model.OrderChangeRequest, int64, error) {
	return s.repo.List(ctx, status, page, pageSize)
}

// ---- tooluse.OrderChangeRequestPort 适配（AI 侧只建申请单；审批/执行走管理面） ----

// OrderChangeRequestPortAdapter 适配 OrderChangeService → tooluse.OrderChangeRequestPort。
type OrderChangeRequestPortAdapter struct {
	svc *OrderChangeService
}

func NewOrderChangeRequestPortAdapter(svc *OrderChangeService) *OrderChangeRequestPortAdapter {
	return &OrderChangeRequestPortAdapter{svc: svc}
}

func (a *OrderChangeRequestPortAdapter) CreateChangeRequest(ctx context.Context, in tooluse.OrderChangeRequestInput) (map[string]any, error) {
	rec, err := a.svc.Create(ctx, OrderChangeCreateInput{
		Platform:   in.Platform,
		OrderID:    in.OrderID,
		CustomerID: in.CustomerID,
		SessionID:  in.SessionID,
		ChangeType: in.ChangeType,
		Payload:    in.Payload,
		Reason:     in.Reason,
	})
	if err != nil {
		return nil, err
	}
	// 返回摘要（不含全量载荷，避免大对象回流 LLM）
	return map[string]any{
		"change_no":  rec.ChangeNo,
		"status":     rec.Status,
		"capability": rec.Capability,
		"hint":       "申请单已登记，等待审批；审批通过后按平台能力路由执行（" + rec.Capability + "）",
	}, nil
}

// ---- 进程级单例（工具提供者消费入口；app 装配写入） ----

var globalOrderChangeSvc *OrderChangeService

// SetOrderChangeService 装配层注入（app.InitOrderChangeRuntime）。
func SetOrderChangeService(svc *OrderChangeService) {
	globalOrderChangeSvc = svc
}

// GetOrderChangeService 取单例（未装配返回 nil；工具提供者 nil-safe 不注册工具）。
func GetOrderChangeService() *OrderChangeService {
	return globalOrderChangeSvc
}
