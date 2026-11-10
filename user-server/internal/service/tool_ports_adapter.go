package service

import (
	"context"
	"errors"
	"strings"
	"time"

	"hivemtk-user/internal/aiagent/agent/portcontract"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// CustomerPortAdapter 适配 CustomerService → portcontract.CustomerPort
type CustomerPortAdapter struct {
	svc *CustomerService
}

// NewCustomerPortAdapter 构造
func NewCustomerPortAdapter(svc *CustomerService) *CustomerPortAdapter {
	if svc == nil {
		svc = NewCustomerService()
	}
	return &CustomerPortAdapter{svc: svc}
}

func (a *CustomerPortAdapter) GetCustomerProfile(customerID string) (*portcontract.CustomerProfileView, error) {
	p, err := a.svc.GetCustomerProfile(context.Background(), customerID)
	if err != nil {
		if errors.Is(err, ErrCustomerNotFound) {
			return nil, portcontract.ErrCustomerNotFound
		}
		return nil, err
	}
	if p == nil {
		return nil, nil
	}
	return &portcontract.CustomerProfileView{
		Customer:     p.Customer,
		RecentEvents: p.RecentEvents,
		Tags:         p.Tags,
	}, nil
}

func (a *CustomerPortAdapter) CreateOrUpdate(identity *portcontract.CustomerIdentity) (*model.Customer, error) {
	if identity == nil {
		return nil, ErrInvalidDTO
	}
	return a.svc.CreateOrUpdate(context.Background(), &CustomerDTO{
		Phone:         identity.Phone,
		Email:         identity.Email,
		WechatOpenID:  identity.WechatOpenID,
		DouyinOpenID:  identity.DouyinOpenID,
		XiaohongshuID: identity.XiaohongshuID,
	})
}

func (a *CustomerPortAdapter) MergeCustomers(primaryID, secondaryID string) error {
	return a.svc.MergeCustomers(context.Background(), primaryID, secondaryID)
}

func (a *CustomerPortAdapter) AddTags(customerID string, tags []string) error {
	return a.svc.AddTags(context.Background(), customerID, tags)
}

func (a *CustomerPortAdapter) RemoveTags(customerID string, tags []string) error {
	return a.svc.RemoveTags(context.Background(), customerID, tags)
}

// SessionPortAdapter 适配 CustomerSessionService → portcontract.SessionPort
type SessionPortAdapter struct {
	svc *CustomerSessionService
}

// NewSessionPortAdapter 构造
func NewSessionPortAdapter(svc *CustomerSessionService) *SessionPortAdapter {
	if svc == nil {
		svc = NewCustomerSessionService()
	}
	return &SessionPortAdapter{svc: svc}
}

func (a *SessionPortAdapter) CreateSession(ctx context.Context, in *portcontract.CreateSessionInput) (*model.CustomerSession, error) {
	if in == nil {
		return nil, ErrInvalidDTO
	}
	return a.svc.CreateSession(ctx, &CreateSessionRequest{
		Platform:  model.Platform(in.Platform),
		AccountID: in.AccountID,
		UserID:    in.UserID,
		UserName:  in.UserName,
		UserPhone: in.UserPhone,
		UserEmail: in.UserEmail,
	})
}

func (a *SessionPortAdapter) GetMessages(sessionID string, page, pageSize int) ([]*model.SessionMessage, int64, error) {
	return a.svc.GetMessages(context.Background(), sessionID, page, pageSize)
}

func (a *SessionPortAdapter) SendMessage(ctx context.Context, in *portcontract.SendMessageInput) (*model.SessionMessage, error) {
	if in == nil {
		return nil, ErrInvalidDTO
	}
	ct := model.MessageType(in.ContentType)
	if ct == "" {
		ct = model.MessageTypeText
	}
	// media_url 列宽收口（F2）：session_messages.media_url varchar(500)。
	return a.svc.SendMessage(ctx, &SendMessageRequest{
		SessionID:   in.SessionID,
		SenderType:  in.SenderType,
		SenderID:    in.SenderID,
		SenderName:  in.SenderName,
		Content:     in.Content,
		ContentType: ct,
		MediaURL:    model.TruncateMediaURL(strings.TrimSpace(in.MediaURL)),
	})
}

// FollowUpPortAdapter 适配 FollowUpService → portcontract.FollowUpPort
type FollowUpPortAdapter struct {
	svc *FollowUpService
}

// NewFollowUpPortAdapter 构造
func NewFollowUpPortAdapter(svc *FollowUpService) *FollowUpPortAdapter {
	if svc == nil {
		svc = NewFollowUpService(NewCustomerJourneyService())
	}
	return &FollowUpPortAdapter{svc: svc}
}

func (a *FollowUpPortAdapter) Schedule(ctx context.Context, customerID, ownerID, reminderType string, dueIn time.Duration, opts *portcontract.FollowUpScheduleOptions) (any, error) {
	rType := ReminderType(reminderType)
	var so *ScheduleOptions
	if opts != nil {
		so = &ScheduleOptions{
			Title:       opts.Title,
			Description: opts.Note,
			Priority:    parseReminderPriority(opts.Priority),
		}
	}
	return a.svc.Schedule(ctx, customerID, ownerID, rType, dueIn, so)
}

func parseReminderPriority(p string) ReminderPriority {
	switch p {
	case "low":
		return PriorityLow
	case "high":
		return PriorityHigh
	case "urgent":
		return PriorityUrgent
	default:
		return PriorityNormal
	}
}

func (a *FollowUpPortAdapter) CompleteWithResult(reminderID string, result, note string) error {
	return a.svc.CompleteWithResult(context.Background(), reminderID, FollowUpResult(result), note)
}

func (a *FollowUpPortAdapter) Cancel(reminderID string) error {
	return a.svc.Cancel(context.Background(), reminderID)
}

func (a *FollowUpPortAdapter) ResultInfo(result string) (stage string, ok bool) {
	info, ok := FollowUpResultInfo[FollowUpResult(result)]
	if !ok {
		return "", false
	}
	return string(info.TargetStage), true
}

// ----- OrderPort -----
// OrderPortAdapter 适配 ExternalOrderRepository → portcontract.OrderPort
// 订单是外部电商同步进来的只读镜像，客服只查询（查单 / 客户 360 视图）。
type OrderPortAdapter struct {
	repo *repository.ExternalOrderRepository
}

// NewOrderPortAdapter 构造
func NewOrderPortAdapter(repo *repository.ExternalOrderRepository) *OrderPortAdapter {
	if repo == nil {
		repo = repository.NewExternalOrderRepository()
	}
	return &OrderPortAdapter{repo: repo}
}

func externalOrderToView(o *model.ExternalOrder) *portcontract.OrderView {
	if o == nil {
		return nil
	}
	return &portcontract.OrderView{
		Platform:     o.Platform,
		OrderID:      o.OrderID,
		OrderNo:      o.OrderNo,
		UserName:     o.UserName,
		UserPhone:    o.UserPhone,
		TotalAmount:  o.TotalAmount,
		PayAmount:    o.PayAmount,
		Status:       o.Status,
		PayTime:      fmtTime(o.PayTime),
		ShipTime:     fmtTime(o.ShipTime),
		CompleteTime: fmtTime(o.CompleteTime),
		Items:        o.Items,
	}
}

func fmtTime(t *time.Time) string {
	if t == nil {
		return ""
	}
	return t.Format("2006-01-02 15:04:05")
}

func (a *OrderPortAdapter) LookupByOrderID(ctx context.Context, platform, orderID string) (*portcontract.OrderView, error) {
	o, err := a.repo.GetByOrderID(ctx, platform, orderID)
	if err != nil {
		return nil, err
	}
	return externalOrderToView(o), nil
}

func (a *OrderPortAdapter) LookupByCustomer(ctx context.Context, phone, name string) ([]*portcontract.OrderView, error) {
	list, err := a.repo.GetByCustomer(ctx, phone, name)
	if err != nil {
		return nil, err
	}
	views := make([]*portcontract.OrderView, 0, len(list))
	for _, o := range list {
		views = append(views, externalOrderToView(o))
	}
	return views, nil
}

// ----- AfterSalePort -----
// AfterSalePortAdapter 适配 AfterSaleService → portcontract.AfterSalePort
// 售后单是客服侧唯一允许对"订单"写入的入口（发起退款/退货，回写电商）。
type AfterSalePortAdapter struct {
	svc *AfterSaleService
}

// NewAfterSalePortAdapter 构造
func NewAfterSalePortAdapter(svc *AfterSaleService) *AfterSalePortAdapter {
	// 不再在适配器里兜底自建服务：那会用全局 db 句柄绕过装配层的注入。
	if svc == nil {
		panic("NewAfterSalePortAdapter: svc 不能为 nil（由装配层注入）")
	}
	return &AfterSalePortAdapter{svc: svc}
}

func (a *AfterSalePortAdapter) Create(ctx context.Context, req *portcontract.AfterSaleRequest) (*portcontract.AfterSaleView, error) {
	return a.svc.Create(ctx, req)
}

func (a *AfterSalePortAdapter) Query(ctx context.Context, platform, orderID, customerPhone string) ([]*portcontract.AfterSaleView, error) {
	return a.svc.Query(ctx, platform, orderID, customerPhone)
}
