package service

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// 模板/技能市场写侧闭环（specs/013 S-22：发布-审核-支付）。
//
// 职责：只读橱窗（template_market.go）之外的写三段——
//  1. 发布入口：Submit 提审（登录用户），落 pending；
//  2. 审核台：Review（通过/驳回）、Disable/Enable（下架/复架），admin 档；
//  3. 支付订单接线：CreateOrder（免费直通 / 付费待支付）、MarkPaid（线下收款
//     确认，admin 档）、CancelOrder（买家撤单）。
//
// 本仓没有聚合支付通道：把「收款确认」做成显式管理动作而不是假装有支付回调
// （与 module_market 的线下付款指引同一条思路，差别只在确认动作落在本地审核台）。

// 写侧业务哨兵（controller 据此映射业务码，不落 500）。
var (
	// ErrMarketEntryDuplicate ID 与既有条目（含进程内种子）撞名
	ErrMarketEntryDuplicate = errors.New("市场条目 ID 已存在")
	// ErrMarketEntryNotFoundInDB 数据库目录中查无此条目（与只读橱窗的
	// ErrMarketEntryNotFound 分开：后者含种子命名空间，本哨兵只管写侧落库查无）
	ErrMarketEntryNotFoundInDB = errors.New("提交的条目不存在或已删除")
	// ErrMarketEntryStateNotAllowed 状态跃迁不合法（重复审核/越态操作）
	ErrMarketEntryStateNotAllowed = errors.New("当前状态不允许该操作")
	// ErrMarketOrderNotFound 订单查无
	ErrMarketOrderNotFound = errors.New("订单不存在")
	// ErrMarketOrderStateNotAllowed 订单状态跃迁不合法（重复确认/取消已支付单）
	ErrMarketOrderStateNotAllowed = errors.New("当前订单状态不允许该操作")
	// ErrMarketOrderDuplicate 同条目已有活跃订单（待支付/已支付）
	ErrMarketOrderDuplicate = errors.New("该条目已有进行中的订单")
	// ErrMarketOrderNoContact 线下收款路径缺联系方式（运营联系不上买家=流程断链）
	ErrMarketOrderNoContact = errors.New("付费条目下单需留下联系电话，以便确认收款")
)

// marketEntryIDRe 目录 ID 字符集：小写字母/数字/连字符，2-64 位
// （ID 进公开 URL，也和种子命名空间共处，不放行任意文本）。
var marketEntryIDRe = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{1,63}$`)

// MarketEntryInput 发布入口入参。
type MarketEntryInput struct {
	ID           string   `json:"id"` // 可选；缺省自动生成
	Kind         string   `json:"kind"`
	Name         string   `json:"name"`
	Category     string   `json:"category"`
	Summary      string   `json:"summary"`
	Source       string   `json:"source"`
	Availability string   `json:"availability"`
	Tags         []string `json:"tags"`
	PriceCents   int64    `json:"price_cents"`
}

// MarketEntryService 写侧闭环服务。
type MarketEntryService struct {
	repo *repository.MarketEntryRepository
	// seed 命名空间：提审 ID 查重要把进程内种子也算上（目录主键全库唯一）。
	seed repository.TemplateMarketRepository
}

// NewMarketEntryService 构造（全局句柄）。
func NewMarketEntryService() *MarketEntryService {
	return &MarketEntryService{
		repo: repository.NewMarketEntryRepositoryDefault(),
		seed: repository.NewMemoryTemplateMarketRepository(),
	}
}

// NewMarketEntryServiceWith 显式注入（测试用）。
func NewMarketEntryServiceWith(repo *repository.MarketEntryRepository, seed repository.TemplateMarketRepository) *MarketEntryService {
	return &MarketEntryService{repo: repo, seed: seed}
}

// Submit 发布入口：提审一条目录条目（落 pending，进审核台）。
//
// rejected/disabled 的复提走同一入口：带上同 ID 重新提审，状态条件跃迁回 pending
// （跃迁表里 rejected→pending、disabled→pending 都合法）；命中不到行则按新条目落库。
func (s *MarketEntryService) Submit(ctx context.Context, submitterID int64, in MarketEntryInput) (*model.MarketCatalogEntry, error) {
	if err := validateMarketEntryInput(in); err != nil {
		return nil, err
	}
	id := strings.TrimSpace(in.ID)
	if id == "" {
		id = newMarketEntryID()
	}
	if !marketEntryIDRe.MatchString(id) {
		return nil, fmt.Errorf("%w：ID 只允许小写字母/数字/连字符（2-64 位）", ErrMarketEntryInvalidParam)
	}
	// 查重双源：进程内种子（card-douyin 等内置命名空间）+ 数据库（任意状态都占位，
	// 驳回的 ID 也不许别人抢——否则驳回单复活时撞车）。
	if e, err := s.seed.Get(ctx, id); err != nil {
		return nil, err
	} else if e != nil {
		return nil, ErrMarketEntryDuplicate
	}
	if cnt, err := s.repo.CountEntriesByID(ctx, id); err != nil {
		return nil, err
	} else if cnt > 0 {
		// 同一提交人复提（rejected/disabled → pending）：更新元数据后条件跃迁。
		row, err := s.repo.GetEntry(ctx, id)
		if err != nil {
			return nil, err
		}
		if row == nil {
			return nil, ErrMarketEntryDuplicate // 计数为 0>0 矛盾，防御
		}
		if row.SubmitterID != submitterID {
			return nil, ErrMarketEntryDuplicate
		}
		applyMarketEntryInput(row, in)
		ok, err := s.repo.UpdateEntryStatus(ctx, id, row.Status, model.MarketEntryPending, map[string]any{
			"name": row.Name, "kind": row.Kind, "category": row.Category,
			"summary": row.Summary, "source": row.Source,
			"availability": row.Availability, "tags": row.Tags,
			"price_cents": row.PriceCents, "reject_reason": "",
		})
		if err != nil {
			return nil, err
		}
		if !ok {
			return nil, ErrMarketEntryStateNotAllowed
		}
		return s.repo.GetEntry(ctx, id)
	}

	row := &model.MarketCatalogEntry{
		ID: id, Status: model.MarketEntryPending, SubmitterID: submitterID,
	}
	applyMarketEntryInput(row, in)
	if err := s.repo.CreateEntry(ctx, row); err != nil {
		return nil, err
	}
	return row, nil
}

// ErrMarketEntryInvalidParam 参数不合法（与哨兵并列，便于 errors.Is 判定）。
var ErrMarketEntryInvalidParam = errors.New("市场条目参数不合法")

// Review 审核台：通过（approved，落 EnabledAt）或驳回（rejected，落理由）。
func (s *MarketEntryService) Review(ctx context.Context, reviewerID int64, id string, approve bool, reason string) (*model.MarketCatalogEntry, error) {
	row, err := s.mustEntry(ctx, id)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var to string
	patch := map[string]any{"reviewed_by": reviewerID}
	if approve {
		to = model.MarketEntryApproved
		patch["enabled_at"] = now
		patch["disabled_at"] = nil
		patch["reject_reason"] = ""
	} else {
		if strings.TrimSpace(reason) == "" {
			return nil, fmt.Errorf("%w：驳回必须给出理由", ErrMarketEntryInvalidParam)
		}
		to = model.MarketEntryRejected
		patch["reject_reason"] = strings.TrimSpace(reason)
	}
	return s.transitEntry(ctx, row, to, patch)
}

// Disable 审核台下架（approved → disabled）。
func (s *MarketEntryService) Disable(ctx context.Context, reviewerID int64, id string) (*model.MarketCatalogEntry, error) {
	row, err := s.mustEntry(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.transitEntry(ctx, row, model.MarketEntryDisabled, map[string]any{
		"reviewed_by": reviewerID, "disabled_at": time.Now(),
	})
}

// Enable 审核台复架（disabled → approved）。
func (s *MarketEntryService) Enable(ctx context.Context, reviewerID int64, id string) (*model.MarketCatalogEntry, error) {
	row, err := s.mustEntry(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.transitEntry(ctx, row, model.MarketEntryApproved, map[string]any{
		"reviewed_by": reviewerID, "enabled_at": time.Now(), "disabled_at": nil,
	})
}

// ListForReview 审核台队列（status 为空 = 全部状态）。
func (s *MarketEntryService) ListForReview(ctx context.Context, status string) ([]*model.MarketCatalogEntry, error) {
	return s.repo.ListEntriesByStatus(ctx, strings.TrimSpace(status), 200)
}

func (s *MarketEntryService) mustEntry(ctx context.Context, id string) (*model.MarketCatalogEntry, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, ErrMarketEntryNotFoundInDB
	}
	row, err := s.repo.GetEntry(ctx, id)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrMarketEntryNotFoundInDB
	}
	return row, nil
}

func (s *MarketEntryService) transitEntry(ctx context.Context, row *model.MarketCatalogEntry, to string, patch map[string]any) (*model.MarketCatalogEntry, error) {
	if !model.MarketEntryTransitionAllowed(row.Status, to) {
		return nil, ErrMarketEntryStateNotAllowed
	}
	ok, err := s.repo.UpdateEntryStatus(ctx, row.ID, row.Status, to, patch)
	if err != nil {
		return nil, err
	}
	if !ok {
		// 条件更新 0 行：并发下别人先动了状态。
		return nil, ErrMarketEntryStateNotAllowed
	}
	return s.repo.GetEntry(ctx, row.ID)
}

func validateMarketEntryInput(in MarketEntryInput) error {
	if !model.MarketEntryKindAvailable(in.Kind) {
		return fmt.Errorf("%w：kind 必须是 template 或 skill", ErrMarketEntryInvalidParam)
	}
	if strings.TrimSpace(in.Name) == "" {
		return fmt.Errorf("%w：名称不能为空", ErrMarketEntryInvalidParam)
	}
	if len(in.Name) > 128 {
		return fmt.Errorf("%w：名称最长 128 字", ErrMarketEntryInvalidParam)
	}
	if strings.TrimSpace(in.Category) == "" {
		return fmt.Errorf("%w：分类不能为空", ErrMarketEntryInvalidParam)
	}
	if len(in.Category) > 32 {
		return fmt.Errorf("%w：分类最长 32 字", ErrMarketEntryInvalidParam)
	}
	if strings.TrimSpace(in.Source) == "" {
		return fmt.Errorf("%w：来源（事实源）不能为空——目录条目必须指向真实实现", ErrMarketEntryInvalidParam)
	}
	if len(in.Source) > 255 {
		return fmt.Errorf("%w：来源最长 255 字", ErrMarketEntryInvalidParam)
	}
	if !model.MarketEntryAvailabilityAvailable(in.Availability) {
		return fmt.Errorf("%w：availability 必须是 builtin/needs_module/coming_soon", ErrMarketEntryInvalidParam)
	}
	if in.PriceCents < 0 {
		return fmt.Errorf("%w：售价不能为负", ErrMarketEntryInvalidParam)
	}
	return nil
}

func applyMarketEntryInput(row *model.MarketCatalogEntry, in MarketEntryInput) {
	row.Kind = in.Kind
	row.Name = strings.TrimSpace(in.Name)
	row.Category = strings.TrimSpace(in.Category)
	row.Summary = in.Summary
	row.Source = strings.TrimSpace(in.Source)
	row.Availability = in.Availability
	row.Tags = marshalMarketTags(in.Tags)
	row.PriceCents = in.PriceCents
}

// marshalMarketTags tags 落 jsonb 数组；nil 落 []（列 NOT NULL DEFAULT '[]'）。
func marshalMarketTags(tags []string) string {
	clean := make([]string, 0, len(tags))
	for _, t := range tags {
		if t = strings.TrimSpace(t); t != "" {
			clean = append(clean, t)
		}
	}
	return toJSONString(clean)
}

// ── 支付订单接线 ────────────────────────────────────────────────────────────

// MarketOrderInput 下单入参。
type MarketEntryOrderInput struct {
	ContactPhone string `json:"contact_phone"`
	Remark       string `json:"remark"`
}

// MarketOrderCreated 下单结果。
type MarketOrderCreated struct {
	Order      *model.MarketEntryOrder `json:"order"`
	Duplicate  bool                    `json:"duplicate"`   // 命中既有活跃单（幂等回显）
	FreeDirect bool                    `json:"free_direct"` // 0 元直通：直接已获得
	// PaymentGuide 线下付款指引（付费单恒非空；免费单为空）
	PaymentGuide string `json:"payment_guide"`
}

// MarketPaymentGuide 付费单的线下付款指引（与 module_market 的兜底文案同思路，
// 但确认动作在本仓审核台而不是平台）。
const MarketPaymentGuide = "订单已提交。请联系平台运营完成付款，收款确认后条目自动进入「我的已获得」。"

// CreateOrder 支付订单接线：为已上架条目建购买单。
//
// 免费条目（price_cents=0）0 元直通 paid（对齐模块市场 0 元单直通语义）；
// 付费条目落 pending_payment，等审核台 MarkPaid 线下确认。同一买家同一活跃单
// （待支付/已支付）幂等回显，不重复建单。
func (s *MarketEntryService) CreateOrder(ctx context.Context, buyerID int64, entryID string, in MarketEntryOrderInput) (*MarketOrderCreated, error) {
	row, err := s.mustEntry(ctx, entryID)
	if err != nil {
		return nil, err
	}
	if row.Status != model.MarketEntryApproved {
		return nil, ErrMarketEntryStateNotAllowed // 未上架/已下架条目不可购买
	}
	// 幂等：已有活跃单直接回显（重复点击/重试不重复建单）。
	if cnt, err := s.repo.CountOrdersByBuyerAndEntry(ctx, buyerID, row.ID); err != nil {
		return nil, err
	} else if cnt > 0 {
		return &MarketOrderCreated{Duplicate: true, PaymentGuide: MarketPaymentGuide}, nil
	}

	contact := strings.TrimSpace(in.ContactPhone)
	if row.PriceCents > 0 && contact == "" {
		return nil, ErrMarketOrderNoContact
	}
	order := &model.MarketEntryOrder{
		OrderNo:      newMarketOrderNo(),
		EntryID:      row.ID,
		EntryName:    row.Name,
		BuyerID:      buyerID,
		AmountCents:  row.PriceCents,
		Status:       model.MarketOrderPendingPayment,
		ContactPhone: contact,
		Remark:       strings.TrimSpace(in.Remark),
	}
	if row.PriceCents == 0 {
		order.Status = model.MarketOrderPaid // 0 元直通
		now := time.Now()
		order.PaidAt = &now
		order.PaidBy = buyerID // 领取动作即确认，无收款环节
	}
	if err := s.repo.CreateOrder(ctx, order); err != nil {
		return nil, err
	}
	guide := ""
	if order.AmountCents > 0 {
		guide = MarketPaymentGuide
	}
	return &MarketOrderCreated{
		Order: order, FreeDirect: order.AmountCents == 0, PaymentGuide: guide,
	}, nil
}

// MarkPaid 审核台收款确认（pending_payment → paid）。
func (s *MarketEntryService) MarkPaid(ctx context.Context, operatorID int64, orderNo string) (*model.MarketEntryOrder, error) {
	return s.transitOrder(ctx, operatorID, orderNo, model.MarketOrderPaid, map[string]any{"paid_at": time.Now()})
}

// CancelOrder 买家撤单（pending_payment → cancelled；只许动自己的待支付单）。
func (s *MarketEntryService) CancelOrder(ctx context.Context, buyerID int64, orderNo string) (*model.MarketEntryOrder, error) {
	orderNo = strings.TrimSpace(orderNo)
	if orderNo == "" {
		return nil, ErrMarketOrderNotFound
	}
	row, err := s.repo.GetOrderByNo(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrMarketOrderNotFound
	}
	if row.BuyerID != buyerID {
		return nil, ErrMarketOrderNotFound // 别人的单按「查无」口径回，不泄露存在性
	}
	if !model.MarketOrderTransitionAllowed(row.Status, model.MarketOrderCancelled) {
		return nil, ErrMarketOrderStateNotAllowed
	}
	ok, err := s.repo.UpdateOrderStatus(ctx, orderNo, row.Status, model.MarketOrderCancelled, nil)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrMarketOrderStateNotAllowed
	}
	return s.repo.GetOrderByNo(ctx, orderNo)
}

// ListMyOrders 买家订单列表。
func (s *MarketEntryService) ListMyOrders(ctx context.Context, buyerID int64) ([]*model.MarketEntryOrder, error) {
	return s.repo.ListOrdersByBuyer(ctx, buyerID, 200)
}

// ListMySubmissions 提交人的发布列表。
func (s *MarketEntryService) ListMySubmissions(ctx context.Context, submitterID int64) ([]*model.MarketCatalogEntry, error) {
	return s.repo.ListEntriesBySubmitter(ctx, submitterID)
}

// ListOwnedEntries 买家已获得（订单 paid）的目录条目——
// 已获得的按下单时点排，附带订单号，页面不需要再反查。
type OwnedEntry struct {
	Entry      *model.MarketCatalogEntry `json:"entry"`
	OrderNo    string                    `json:"order_no"`
	ObtainedAt *time.Time                `json:"obtained_at"`
}

// ListOwnedEntriesWithMeta 已获得列表（条目 + 单号 + 获得时点）。
func (s *MarketEntryService) ListOwnedEntriesWithMeta(ctx context.Context, buyerID int64) ([]*OwnedEntry, error) {
	orders, err := s.repo.ListOrdersByBuyer(ctx, buyerID, 200)
	if err != nil {
		return nil, err
	}
	out := make([]*OwnedEntry, 0, len(orders))
	for _, o := range orders {
		if o.Status != model.MarketOrderPaid {
			continue
		}
		row, err := s.repo.GetEntry(ctx, o.EntryID)
		if err != nil {
			return nil, err
		}
		if row == nil {
			continue // 条目被物理删除时不炸页面（快照名在订单上）
		}
		out = append(out, &OwnedEntry{Entry: row, OrderNo: o.OrderNo, ObtainedAt: o.PaidAt})
	}
	return out, nil
}

// ListAllOrders 管理端订单列表（status 为空 = 全部）。
func (s *MarketEntryService) ListAllOrders(ctx context.Context, status string) ([]*model.MarketEntryOrder, error) {
	return s.repo.ListAllOrders(ctx, strings.TrimSpace(status), 200)
}

func (s *MarketEntryService) transitOrder(ctx context.Context, operatorID int64, orderNo, to string, patch map[string]any) (*model.MarketEntryOrder, error) {
	orderNo = strings.TrimSpace(orderNo)
	if orderNo == "" {
		return nil, ErrMarketOrderNotFound
	}
	row, err := s.repo.GetOrderByNo(ctx, orderNo)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return nil, ErrMarketOrderNotFound
	}
	if !model.MarketOrderTransitionAllowed(row.Status, to) {
		return nil, ErrMarketOrderStateNotAllowed
	}
	if patch == nil {
		patch = map[string]any{}
	}
	patch["paid_by"] = operatorID
	ok, err := s.repo.UpdateOrderStatus(ctx, orderNo, row.Status, to, patch)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, ErrMarketOrderStateNotAllowed
	}
	return s.repo.GetOrderByNo(ctx, orderNo)
}

// ── 编号生成 ────────────────────────────────────────────────────────────────

// newMarketEntryID 自动目录 ID：mkt-<unixmilli>-<rand4>（可读、URL 安全、碰撞面小）。
func newMarketEntryID() string {
	return fmt.Sprintf("mkt-%d-%s", time.Now().UnixMilli(), randomHex(2))
}

// newMarketOrderNo 订单号：TM<unixmilli><rand5>（对齐 module_orders 的 MO 形态）。
func newMarketOrderNo() string {
	return fmt.Sprintf("TM%d%s", time.Now().UnixMilli(), strings.ToUpper(randomHex(5)))
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return fmt.Sprintf("%0*x", n*2, time.Now().UnixNano()%(int64(1)<<(8*n*4)))
	}
	return hex.EncodeToString(buf)
}
