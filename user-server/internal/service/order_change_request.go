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
	"errors"
	"fmt"
	"strings"
	"time"

	bizerr "hivemtk-user/internal/domain/errors"

	"hivemtk-user/internal/aiagent/agent/tooluse"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// 平台能力路由结论（创建时冻结进申请单；v1 静态规则表，后续数据化）。
//
// EC-15/M15（docs/未来任务清单.md §7.1.8，见 M15「ERP/OMS/WMS写路由与回传确认」）：
// 未来将引入 Route.Resolve(action, orderOrigin) 选择唯一 writeOwner（原生/ERP）、记录
// ERP↔平台单号映射、传播链与最大等待；同一业务不得原生+ERP双写；ERP 成功但平台未变仍
// 为待核，传播超时转人工。当前实现不具备上述路由/回传闭环：CapabilityViaERP 仅为路由标签，
// 且下方 Execute 对 via_erp 直接拒绝（ErrCapabilityNotAvailable），属显式 fail-closed，
// 并非生产级 ERP 执行路径。
const (
	CapabilityDirectAPI     = "direct_api"      // 平台有直改 API（如京东 SOP 改址），需商家授权🔵
	CapabilityBuyerInitiate = "buyer_initiated" // 平台为买家发起型（抖音/闲鱼），客服引导买家自助
	CapabilityViaERP        = "via_erp"         // 经 ERP 通道执行（管家婆，路由标签；M15 落地前不可执行）
	CapabilityManual        = "manual"          // 派人工坐席在平台后台操作
)

// ErrCapabilityNotAvailable 能力通路未接（凭证/连接器未配），显式拒绝而非静默降级。
var ErrCapabilityNotAvailable = fmt.Errorf("该平台执行通路未接入（凭证/连接器待配置）")

// platformCapabilityRules 平台→能力路由（v1 静态规则；未知平台=manual 兜底）。
// 证据级（二次审核 2026-11-10 补标）：
//   - jd=direct_api：京东 JOS 修改收货地址 API 公开实证（SOP 类型+等待出库+一单一次）；
//   - taobao/tmall=direct_api：【预留】本轮未做平台级取证（闲鱼 ISV 家族旁证），
//     v1 执行通路对 direct_api 统一显式拒绝，路由标签不构成可用性承诺；
//   - douyin/xianyu=buyer_initiated：平台事实（无卖家直改地址 API，买家发起型）；
//   - grasp=via_erp：管家婆云版 Open API 在案（辉煌单机版无公开 API）。
//
// EC-16（M16，G2-B，未完成/未 live 关闭）：目标扩展 淘宝/天猫、PDD、抖店“三核心平台”按固定动作矩阵，
// 共用业务账本与状态契约（依赖 EC-12/13 基线；EC-14/15 选用时再接入逆向/ERP 路由）。当前代码现状（本地审计）：
//   - capregistry 中仅 "douyin_shop"（产品键）登记了 api_adapter 商务动作格（order.address.change /
//     aftersale.write / refund.write，experimental），与本表 "douyin"(buyer_initiated) 属不同平台命名域，
//     尚未形成“抖店原生执行”与订单改单账本的闭环接线（见 connector/capregistry 相关实现与测试）。
//   - "taobao"/"tmall" 仅在此路由为 direct_api 且 Execute 显式拒绝；capregistry 未登记对应 commerce action
//     cells（仅有桥接 DM 能力登记），无真实 Adapter/凭证闭环。
//   - "pdd"(拼多多) 未在本表中，默认 manual；capregistry 仅桥接 DM，无 commerce action cells。
// 完成标准（见 docs/未来任务清单.md §7.1.8 EC-16）要求：每平台独立 scope/地区/状态测试；
// PDD 赔付/取件、抖店买家确认、淘系限制分支全覆盖。本文件/本包不构成 EC-16 完成证据。
//
// EC-17（M17，本机未完成/未 live 关闭，见 docs/未来任务清单.md §7.1.8）：
//   - jd_pop（capregistry 商务产品键，官方工作台声明见 merchantWorkflowCells: jd_pop aftersale.write official_ui）
//     与本表键 "jd"(direct_api, Execute 拒绝) 为不同命名域；尚无闭环的订单/案件/审批/执行证据链。
//   - xiaohongshu（小红书店/千帆店相关）：未在本表与 capregistry commerce cells 中登记；当前默认 manual，
//     无官方工作台/原生动作的系统化接入实现。
//   - kuaishou_shop：capregistry 有桥接 DM（Experimental），无 commerce action cells；本表未登记，默认 manual。
//   - wechat_store（微信小店）/ youzan（有赞）：代码库内无 platform 键/路由/适配器实现（当前缺失）。
//
// EC-18（M18，本机未完成/未 live 关闭，见 docs/未来任务清单.md §7.1.8 与 §7.3.5）：
//   - Shopify / WooCommerce（自有商城/Widget 路线，§7.3.5 描述原生 GraphQL/REST 与网关手工退款等）：
//     未在本路由表、capregistry commerce cells、连接器/适配器实现中登记；未知平台默认 manual，
//     无 API 执行闭环与真实运行证据。
//   - TikTok Shop（`tiktok_shop`，§7.3.5）：capregistry 仅有 `tiktok`（海外 DM 桥接）登记，
//     无 `tiktok_shop` commerce cells/适配器；当前无法按 §7.3.5 原生退/改/退款路线执行。
//   - Shopee / Lazada / eBay / Walmart US：仅 Shopee、Walmart US 存在桥接 DM 通道（Experimental，见
//     capregistry/channelgw）；均无 commerce action cells（订单改单/售后/退款/逆向）与 API/官方工作台执行
//     适配器闭环。Lazada、eBay 在代码库中无平台键/路由登记（缺失）。
//
// EC-19 (M19, 未实现 / 未 live 关闭，见 docs/未来任务清单.md §7.1.8、§7.3.5)：
//   - Amazon（自履约/平台托管/FBA 等）：无平台键、无 capregistry 商务/工作流 cells、无执行适配器；
//     路由表无条目时 capabilityForPlatform 返回 manual（兜底），但这不等于 M19 要求的「可使用案件流程、指定角色/步骤/证据/超期升级」。
//   - 1688 / 速卖通（AliExpress）：无平台键与执行闭环；1688 卖家视角约束需在后续实现中显式区分于买家查询。
//   - Etsy：无平台键/无 cells/无官方工作台向导流程实现（Shop Manager 退款向导、取消另建动作等）。
//   - 得物（Poizon/DeWu）：无平台键与执行闭环。
//   - 唯品会：capregistry 有 vipshop refund.observe（platform_managed）工作流声明；本路由表无 vipshop 键（改单侧默认 manual），
//     且与「平台托管与卖家操作不混同」的 M19 规则、完整案件流程/证据/升级链条仍未落地。
//   - 苏宁官方工作台：无平台键与执行闭环。
//   - 即时零售分业态（部分退款/整单取消/配送中协商/医药专员等）：无平台键与分业态流程实现。
// 约束：退款与取消、平台托管与卖家操作不得混同；M19 需按 §7.3.5 每行给出可使用案件流程与真实证据（本注释不构成完成）。
//
// 外部事实来源见 docs/research/INTENT-TOOL-UNIFIED-BASE-DESIGN-2026-11.md §五。
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
	// EC-07 note: tenant scoping is enforced by GORM callbacks from ctx (must carry tenant).
	// Buyer ownership is best-effort enforced at tool layer (order.modify_request) when CustomerID
	// is resolved from ToolContext; service does not re-validate buyer binding here.
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
		Status: "approved", ApprovedBy: operator, ApprovedAt: &now, ExpectStatus: "pending",
	}); err != nil {
		return nil, s.mapStatusErr(err)
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
		Status: "rejected", RejectedBy: operator, RejectReason: reason, RejectedAt: &now, ExpectStatus: "pending",
	}); err != nil {
		return nil, s.mapStatusErr(err)
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
		Status: "cancelled", CancelledAt: &now, ExpectStatus: "pending",
	}); err != nil {
		return nil, s.mapStatusErr(err)
	}
	return s.mustGet(ctx, id)
}

// Execute approved→dispatched（当前实现）再经 CompleteExecution→executed，按冻结的能力路由分派。
// v1 通路：manual=派坐席留痕；buyer_initiated=引导话术标记；direct_api/via_erp 显式不可用。
// TODO(EC-10/M10): 未来应落到统一 Operation 账本：记录 attempt、lease、写路由、外部幂等键/请求ID、
// 结果核验与 unknown 处理；当前 dispatched/executed 为人工回填语义，不等价于平台核验完成。
// EC-20 note: any future “操作工作台/异常工作台” UI must label evidence clearly:
// manual/attested (人工回填/证明) vs API/platform-verified (平台核验) — never conflate them
// (per M20/EC-20 acceptance: “人工证据与API核验标签可辨”).
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
		return s.dispatchExecute(ctx, rec.ID, operator, "已派人工坐席在平台后台处理（manual）。派工完成≠实际改单成功：坐席完成平台侧修改后必须调完成回填（CompleteExecution）写明实际结果，申请单才置 executed。")
	case CapabilityBuyerInitiate:
		return s.dispatchExecute(ctx, rec.ID, operator, "该平台为买家发起型变更（buyer_initiated）：本申请单转为引导工单——由坐席向客户发送平台自助修改引导（话术模板待配置）。派工完成≠实际改单成功：客户完成平台侧修改并确认后必须调完成回填（CompleteExecution）写明实际结果。")
	case CapabilityDirectAPI, CapabilityViaERP:
		// 显式拒绝而非静默降级：真实凭证/连接器未接（🔵），不承诺未验证能力。
		// EC-15/M15: via_erp 的“唯一写归属 + 平台回传/传播确认”尚未实现；在未实现前拒绝执行可避免
		// 原生侧与 ERP 侧“双写/重复提交”的误操作面（设计目标：同业务不走原生+ERP双写）。
		// 注意：当前拒绝不等于已具备 M15 所需的“ERP 成功但平台未变仍待核/超时转人工”闭环。
		return nil, bizerr.New(bizerr.CodeConflict, fmt.Sprintf("capability=%s: %v", rec.Capability, ErrCapabilityNotAvailable))
	default:
		return nil, bizerr.New(bizerr.CodeConflict, fmt.Sprintf("未知能力路由 %q", rec.Capability))
	}
}

// dispatchExecute approved→dispatched：派工/引导完成留痕，实际执行结果待回填（FA-09）。
func (s *OrderChangeService) dispatchExecute(ctx context.Context, id uint, operator, note string) (*model.OrderChangeRequest, error) {
	now := time.Now()
	if err := s.repo.UpdateStatus(ctx, id, repository.OrderChangeStatusPatch{
		Status: "dispatched", DispatchedBy: operator, DispatchedAt: &now, ExecutorNote: note, ExpectStatus: "approved",
	}); err != nil {
		return nil, s.mapStatusErr(err)
	}
	logger.Infof("[order_change] 申请单 %d 已派工（%s / %s）", id, operator, note)
	return s.mustGet(ctx, id)
}

// CompleteExecution dispatched→executed：实际改单结果回填（FA-09）。
// 结果说明必填——没有回填证据不得显示"实际改单成功"。
func (s *OrderChangeService) CompleteExecution(ctx context.Context, id uint, operator, resultNote string) (*model.OrderChangeRequest, error) {
	resultNote = strings.TrimSpace(resultNote)
	if operator == "" || resultNote == "" {
		return nil, bizerr.New(bizerr.CodeParamInvalid, "完成回填操作者与实际结果说明必填（无回填证据不得置 executed）")
	}
	rec, err := s.mustGet(ctx, id)
	if err != nil {
		return nil, err
	}
	if rec.Status != "dispatched" {
		return nil, bizerr.New(bizerr.CodeConflict, fmt.Sprintf("仅 dispatched 可回填完成，当前 %s", rec.Status))
	}
	now := time.Now()
	if err := s.repo.UpdateStatus(ctx, id, repository.OrderChangeStatusPatch{
		Status: "executed", ExecutedBy: operator, ExecutedAt: &now,
		ExecutorNote: "执行回填：" + resultNote, ExpectStatus: "dispatched",
	}); err != nil {
		return nil, s.mapStatusErr(err)
	}
	logger.Infof("[order_change] 申请单 %d 执行结果已回填（%s / %s）", id, operator, resultNote)
	return s.mustGet(ctx, id)
}

// mapStatusErr CAS 落空与记录缺失统一映射为业务冲突（带当前状态）。
func (s *OrderChangeService) mapStatusErr(err error) error {
	if errors.Is(err, repository.ErrOrderChangeStatusConflict) {
		return bizerr.New(bizerr.CodeConflict, "状态已被并发操作迁移，请刷新后重试")
	}
	return err
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
