package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	dbUtil "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// 消息中台 - 业务错误码
var (
	ErrMessageHubInvalidPlatform   = errors.New("invalid platform")
	ErrMessageHubInvalidMsgID      = errors.New("invalid msg_id")
	ErrMessageHubInvalidContent    = errors.New("invalid content")
	ErrMessageHubInvalidAccount    = errors.New("invalid account_id")
	ErrMessageHubInvalidDirection  = errors.New("invalid direction")
	ErrMessageHubInvalidMsgType    = errors.New("invalid msg_type")
	ErrMessageHubEmptyMerchant     = errors.New("user_id is required")
	ErrMessageHubTooLarge          = errors.New("content too large")
	ErrMessageHubIdempotent        = errors.New("duplicate message (idempotent)")
	ErrMessageHubQueueFull         = errors.New("queue is full")
	ErrMessageHubStreamNotFound    = errors.New("stream not found")
	ErrMessageHubPartitionMismatch = errors.New("partition mismatch")
)

var messageHubPlatforms = map[string]bool{
	"wecom":       true,
	"personal_wx": true,
	"douyin":      true,
	"kuaishou":    true,
	"xiaohongshu": true,
	"xianyu":      true,
	"tiktok":      true,
	"whatsapp":    true,
	"sms":         true,
	"email":       true,
	"telegram":    true,
	"feishu":      true,
	"qq":          true,
	// 缺这三词是本表与真实数据面脱节留下的两处不同的坑，一并补：
	//   - dingtalk / wechat 有**真实入站路径**直接写 event.Channel
	//     （controller/wechat.go:292、service/dingtalk_app.go:214 → inbox_ingress_persist.go:53
	//     的 hubRepo.Create），全程不经 Normalize ⇒ 这两家的行今日就在库里。
	//     出站失败轨迹改走校验路径后，缺词等于"钉钉/公众号的投递失败永远落不了库"。
	//   - custom 无入站适配器（webhookInboundCapable 判 false），但 POST /api/chat/ingress
	//     的 channel 是自由文本（NormalizeEvent 只拒空串）⇒ 该值同样写得到库里。
	// 本表由 TestBF6_ChannelVocabularyCoversEveryOutboundChannel 锁成"渠道常量全集的镜像"：
	// 少一个词，将来接进来的渠道就会在"轨迹落库"这一步静默失败。
	"dingtalk": true,
	"wechat":   true,
	"custom":   true,
}

var messageHubMsgTypes = map[string]bool{
	"text":     true,
	"image":    true,
	"file":     true,
	"audio":    true,
	"video":    true,
	"link":     true,
	"card":     true,
	"location": true,
	// event：非消息的会话事件（TG 入群/退群事件行写的就是这个）。原先没有任何声明，
	// 只有 repo.Create 的旁路写得了它 —— 补进词表，让"hub 里会出现哪些类型"这一件事
	// 以本表为准，而不是以某段绕过校验的代码为准。
	"event": true,
}

// inboundHubMsgTypeAliases 渠道官方入站消息类型名 → 中台类型词表。
//
// 必须有这张收口表：各渠道对同一件事各叫一名（企微/公众号 voice、飞书/WA/钉钉 audio、
// 钉钉 picture、WA document、飞书 media、公众号 shortvideo、飞书 post…），而中台词表
// 只有 text/image/file/audio/video/link/card/location/event。修复前的两种后果：
//   - 走 hub.Push 的渠道（企微）：Normalize 硬校验 ⇒ ErrMessageHubInvalidMsgType ⇒
//     dispatch 上抛，客户发的语音**连一行 hub 都没有**（不是类型标错，是消息蒸发）；
//   - 直接 repo.Create 的渠道（WA/公众号/飞书）：行落得下，但工作台按类型筛选
//     （repository 的 msg_type = ?）与 by_msg_type 统计永远筛不到这些行。
//
// 只收录各渠道官方词表里真实存在的名字；不在表里、也不在词表里的（官方类型还会新增）
// 由 InboundHubMsgType 兜到 text —— 宁可类型粗，也不能让一条真实消息被拒或被筛漏。
var inboundHubMsgTypeAliases = map[string]string{
	"voice":                model.MsgTypeAudio, // 企微 / 公众号
	"picture":              model.MsgTypeImage, // 钉钉
	"sticker":              model.MsgTypeImage, // 飞书表情 / WA 贴纸
	"document":             model.MsgTypeFile,  // WA
	"folder":               model.MsgTypeFile,  // 飞书文件夹
	"media":                model.MsgTypeVideo, // 飞书视频
	"shortvideo":           model.MsgTypeVideo, // 公众号 / 企微小视频
	"post":                 model.MsgTypeText,  // 飞书富文本（正文已被解出来，类型按文本走）
	"richtext":             model.MsgTypeText,  // 钉钉富文本
	"mixed":                model.MsgTypeText,  // 微信客服图文混排
	"merge_forward":        model.MsgTypeText,  // 飞书合并转发
	"system":               model.MsgTypeText,  // 飞书系统消息
	"interactive":          model.MsgTypeCard,  // 飞书卡片 / WA 互动消息
	"button":               model.MsgTypeCard,  // WA 按钮回复
	"order":                model.MsgTypeCard,  // WA 订单
	"product":              model.MsgTypeCard,  // WA 商品
	"hongbao":              model.MsgTypeCard,  // 以下为飞书卡片类
	"calendar":             model.MsgTypeCard,
	"general_calendar":     model.MsgTypeCard,
	"share_calendar_event": model.MsgTypeCard,
	"video_chat":           model.MsgTypeCard,
	"share_chat":           model.MsgTypeCard,
	"share_user":           model.MsgTypeCard,
	"miniprogram":          model.MsgTypeCard, // 公众号小程序卡片
	"todo":                 model.MsgTypeCard,
	"vote":                 model.MsgTypeCard,

	// 抖音 dop 私信（官方 message_type 全集里不在词表、也不是别名的 4 个）：
	// user_local_* 是"发送方本地文件"形态，emoji 是动图直链，retain_consult_card 是留资卡片。
	// 表外新类型由 InboundHubMsgType 兜到 text，不会拒消息，但类型会退化，故官方新增时要补这里。
	"user_local_image":    model.MsgTypeImage,
	"user_local_video":    model.MsgTypeVideo,
	"emoji":               model.MsgTypeImage,
	"retain_consult_card": model.MsgTypeCard,
}

// InboundHubMsgType 把渠道官方消息类型名映射进中台词表。
// 官方名恰好已在词表里的（image/audio/video/file/text/location…）原样返回。
func InboundHubMsgType(officialType string) string {
	t := strings.ToLower(strings.TrimSpace(officialType))
	if t == "" {
		return model.MsgTypeText
	}
	if mapped, ok := inboundHubMsgTypeAliases[t]; ok {
		return mapped
	}
	if messageHubMsgTypes[t] {
		return t
	}
	return model.MsgTypeText
}

var messageHubDirections = map[string]bool{
	"inbound":  true,
	"outbound": true,
}

// 消息中台常量
const (
	MessageHubDefaultIdemTTL    = 24 * time.Hour
	MessageHubDefaultMaxContent = 64 * 1024
	MessageHubDefaultQueueSize  = 10000
	MessageHubStreamKeyPrefix   = "msg:hub:stream:"
	MessageHubIdemKeyPrefix     = "msg:hub:idem:"
)

// PushMessageRequest 推送消息到中台
type PushMessageRequest struct {
	Platform       string         `json:"platform"`
	AccountID      string         `json:"account_id"`
	MsgID          string         `json:"msg_id"`
	Direction      string         `json:"direction"`
	MsgType        string         `json:"msg_type"`
	SenderID       string         `json:"sender_id"`
	SenderName     string         `json:"sender_name"`
	ReceiverID     string         `json:"receiver_id"`
	ReceiverName   string         `json:"receiver_name"`
	Content        string         `json:"content"`
	MediaURL       string         `json:"media_url"`
	ConversationID string         `json:"conversation_id"`
	IsGroup        bool           `json:"is_group"`
	GroupID        string         `json:"group_id"`
	IsAIReply      bool           `json:"is_ai_reply"`
	AIAgent        string         `json:"ai_agent"`
	Extra          map[string]any `json:"extra"`
	SentAt         *time.Time     `json:"sent_at"`
}

// MessageHubService 消息中台服务
type MessageHubService struct {
	repo        *repository.MessageHubRepository
	cache       cache.Cache
	mu          sync.RWMutex
	streams     map[string]*hubStream
	streamSize  int
	idemTTL     time.Duration
	maxContent  int
	subscribers []MessageSubscriber
	subMu       sync.RWMutex
}

type hubStream struct {
	mu        sync.Mutex
	cond      *sync.Cond
	messages  []*model.MessageHub
	closed    bool
	partition string
}

// MessageSubscriber 消息订阅者
type MessageSubscriber interface {
	OnMessage(ctx context.Context, msg *model.MessageHub) error
	Filter(msg *model.MessageHub) bool
}

// NewMessageHubService 创建消息中台服务(无参,内部用 dbUtil.GetDB())
func NewMessageHubService() *MessageHubService {
	return NewMessageHubServiceWithDB(dbUtil.GetDB(), nil)
}

// NewMessageHubServiceWithDB 创建带 DB 的消息中台服务(显式注入 db,兼容旧调用)
//
// 五层架构 §三.5：构造函数保留 db *gorm.DB 参数（调用方不变），
// 内部创建 repository 实例，service 不再持有 db。
// db 为 nil 时（如部分纯内存场景）repo 字段为 nil，方法调用做无操作短路。
func NewMessageHubServiceWithDB(db *gorm.DB, c cache.Cache) *MessageHubService {
	if c == nil {
		c = cache.GetGlobalCache()
	}
	var repo *repository.MessageHubRepository
	if db != nil {
		repo = repository.NewMessageHubRepositoryWithDB(db)
	}
	s := &MessageHubService{
		repo:       repo,
		cache:      c,
		streams:    make(map[string]*hubStream),
		streamSize: MessageHubDefaultQueueSize,
		idemTTL:    MessageHubDefaultIdemTTL,
		maxContent: MessageHubDefaultMaxContent,
	}
	return s
}

// WithIdemTTL 设置幂等 TTL
func (s *MessageHubService) WithIdemTTL(ctx context.Context, ttl time.Duration) *MessageHubService {
	s.idemTTL = ttl
	return s
}

// WithMaxContent 设置单条内容上限
func (s *MessageHubService) WithMaxContent(ctx context.Context, n int) *MessageHubService {
	s.maxContent = n
	return s
}

// WithQueueSize 设置队列容量
func (s *MessageHubService) WithQueueSize(ctx context.Context, n int) *MessageHubService {
	s.streamSize = n
	return s
}

// Subscribe 注册订阅者
func (s *MessageHubService) Subscribe(ctx context.Context, sub MessageSubscriber) {
	s.subMu.Lock()
	defer s.subMu.Unlock()
	s.subscribers = append(s.subscribers, sub)
}

// Normalize 校验并标准化消息
func (s *MessageHubService) Normalize(ctx context.Context, req *PushMessageRequest) (*model.MessageHub, error) {
	if false {
		return nil, ErrMessageHubEmptyMerchant
	}
	if !messageHubPlatforms[req.Platform] {
		return nil, fmt.Errorf("%w: %s", ErrMessageHubInvalidPlatform, req.Platform)
	}
	if req.AccountID == "" {
		return nil, ErrMessageHubInvalidAccount
	}
	if req.MsgID == "" {
		return nil, ErrMessageHubInvalidMsgID
	}
	if !messageHubDirections[req.Direction] {
		return nil, fmt.Errorf("%w: %s", ErrMessageHubInvalidDirection, req.Direction)
	}
	if !messageHubMsgTypes[req.MsgType] {
		return nil, fmt.Errorf("%w: %s", ErrMessageHubInvalidMsgType, req.MsgType)
	}
	if len(req.Content) > s.maxContent {
		return nil, ErrMessageHubTooLarge
	}
	if req.MsgType == "text" && strings.TrimSpace(req.Content) == "" {
		return nil, ErrMessageHubInvalidContent
	}

	sentAt := time.Now()
	if req.SentAt != nil && !req.SentAt.IsZero() {
		sentAt = *req.SentAt
	}

	extra := model.JSONMap{}
	if req.Extra != nil {
		for k, v := range req.Extra {
			extra[k] = v
		}
	}

	return &model.MessageHub{

		MsgID:          req.MsgID,
		Platform:       req.Platform,
		AccountID:      req.AccountID,
		Direction:      req.Direction,
		MsgType:        req.MsgType,
		SenderID:       strings.TrimSpace(req.SenderID),
		SenderName:     strings.TrimSpace(req.SenderName),
		ReceiverID:     strings.TrimSpace(req.ReceiverID),
		ReceiverName:   strings.TrimSpace(req.ReceiverName),
		Content:        req.Content,
		MediaURL:       req.MediaURL,
		ConversationID: req.ConversationID,
		IsGroup:        req.IsGroup,
		GroupID:        req.GroupID,
		IsAIReply:      req.IsAIReply,
		AIAgent:        req.AIAgent,
		IsRead:         false,
		SentAt:         sentAt,
		Extra:          extra,
	}, nil
}

// IdempotencyKey 计算幂等键
func (s *MessageHubService) IdempotencyKey(ctx context.Context, platform, accountID, msgID string) string {
	h := sha256.Sum256([]byte(platform + "|" + accountID + "|" + msgID))
	return MessageHubIdemKeyPrefix + hex.EncodeToString(h[:])
}

// CheckIdempotent 检查是否幂等（已存在返回 true, 已存在记录 id）
func (s *MessageHubService) CheckIdempotent(ctx context.Context, platform, accountID, msgID string) (bool, uint, error) {
	if s.repo == nil {
		return false, 0, nil
	}
	existing, err := s.repo.GetByPlatformAccountMsgID(ctx, platform, accountID, msgID)
	if err == nil && existing != nil {
		return true, existing.ID, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return false, 0, err
	}
	idemKey := s.IdempotencyKey(ctx, platform, accountID, msgID)
	if s.cache != nil {
		_ = s.cache.Set(ctx, idemKey, "1", s.idemTTL)
	}
	return false, 0, nil
}

// Push 推送消息到中台
func (s *MessageHubService) Push(ctx context.Context, req *PushMessageRequest) (*model.MessageHub, error) {
	msg, err := s.Normalize(ctx, req)
	if err != nil {
		return nil, err
	}
	exist, _, err := s.CheckIdempotent(ctx, msg.Platform, msg.AccountID, msg.MsgID)
	if err != nil {
		return nil, err
	}
	if exist {
		return nil, ErrMessageHubIdempotent
	}
	if s.repo != nil {
		if err := s.repo.Create(ctx, msg); err != nil {
			if strings.Contains(err.Error(), "duplicate") || strings.Contains(err.Error(), "UNIQUE") {
				return nil, ErrMessageHubIdempotent
			}
			return nil, err
		}
	}
	if err := s.enqueue(ctx, msg); err != nil {
		return msg, err
	}
	s.notify(ctx, msg)
	return msg, nil
}

// PushSendFailureTrace 落一条「投递失败」的出站轨迹：只写库，不进 stream、不通知订阅者。
//
// 与 Push 的差别是刻意的：这条消息客户从未收到，推给订阅者（坐席实时视图、会话镜像）
// 等于宣告一次没有发生的投递；它的用途是审计留痕，以及让「是否已回复」的判定
// 有反证可查（HasUnrepliedCustomerMessage 会排除 send_failed 出站行）。
func (s *MessageHubService) PushSendFailureTrace(ctx context.Context, req *PushMessageRequest, failureReason string) (*model.MessageHub, error) {
	if s.repo == nil {
		return nil, nil
	}
	msg, err := s.Normalize(ctx, req)
	if err != nil {
		return nil, err
	}
	msg.Status = "send_failed"
	msg.Extra["send_failed_at"] = time.Now().Format(time.RFC3339)
	if failureReason != "" {
		msg.Extra["send_failed_reason"] = failureReason
	}
	if err := s.repo.Create(ctx, msg); err != nil {
		return nil, err
	}
	return msg, nil
}

// PushBatch 批量推送
func (s *MessageHubService) PushBatch(ctx context.Context, reqs []PushMessageRequest) ([]*model.MessageHub, []error) {
	results := make([]*model.MessageHub, 0, len(reqs))
	errs := make([]error, 0, len(reqs))
	for _, r := range reqs {
		msg, err := s.Push(ctx, &r)
		results = append(results, msg)
		errs = append(errs, err)
	}
	return results, errs
}

// ListQuery 列表查询条件
type ListQuery struct {
	Platform       string
	AccountID      string
	ConversationID string
	SenderID       string
	Direction      string
	MsgType        string
	Keyword        string
	IsRead         *bool
	IsGroup        *bool
	StartTime      *time.Time
	EndTime        *time.Time
	Page           int
	PageSize       int
	OrderBy        string
}

// List 列表查询
func (s *MessageHubService) List(ctx context.Context, q ListQuery) ([]*model.MessageHub, int64, error) {
	if s.repo == nil {
		return nil, 0, nil
	}
	return s.repo.ListByHubQuery(ctx, repository.HubListQuery{
		Platform:       q.Platform,
		AccountID:      q.AccountID,
		ConversationID: q.ConversationID,
		SenderID:       q.SenderID,
		Direction:      q.Direction,
		MsgType:        q.MsgType,
		Keyword:        q.Keyword,
		IsRead:         q.IsRead,
		IsGroup:        q.IsGroup,
		StartTime:      q.StartTime,
		EndTime:        q.EndTime,
		Page:           q.Page,
		PageSize:       q.PageSize,
		OrderBy:        q.OrderBy,
	})
}

// GetByID 详情
func (s *MessageHubService) GetByID(ctx context.Context, id uint) (*model.MessageHub, error) {
	if s.repo == nil {
		return nil, nil
	}
	msg, err := s.repo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return msg, nil
}

// MarkRead 标记已读
func (s *MessageHubService) MarkRead(ctx context.Context, ids []uint) error {
	if s.repo == nil || len(ids) == 0 {
		return nil
	}
	return s.repo.MarkReadByIDs(ctx, ids)
}

// Stats 统计
type HubStats struct {
	Total       int64            `json:"total"`
	Inbound     int64            `json:"inbound"`
	Outbound    int64            `json:"outbound"`
	Unread      int64            `json:"unread"`
	ByPlatform  map[string]int64 `json:"by_platform"`
	ByDirection map[string]int64 `json:"by_direction"`
	ByMsgType   map[string]int64 `json:"by_msg_type"`
	ByAccount   map[string]int64 `json:"by_account"`
	Recent24h   int64            `json:"recent_24h"`
}

// GetStats 统计
func (s *MessageHubService) GetStats(ctx context.Context, start, end *time.Time) (*HubStats, error) {
	if s.repo == nil {
		return &HubStats{ByPlatform: map[string]int64{}, ByDirection: map[string]int64{}, ByMsgType: map[string]int64{}, ByAccount: map[string]int64{}}, nil
	}
	res, err := s.repo.GetHubStats(ctx, start, end)
	if err != nil {
		return nil, err
	}
	if res == nil {
		return &HubStats{ByPlatform: map[string]int64{}, ByDirection: map[string]int64{}, ByMsgType: map[string]int64{}, ByAccount: map[string]int64{}}, nil
	}
	return &HubStats{
		Total:       res.Total,
		Inbound:     res.Inbound,
		Outbound:    res.Outbound,
		Unread:      res.Unread,
		ByPlatform:  res.ByPlatform,
		ByDirection: res.ByDirection,
		ByMsgType:   res.ByMsgType,
		ByAccount:   res.ByAccount,
		Recent24h:   res.Recent24h,
	}, nil
}

func (s *MessageHubService) partitionKey(ctx context.Context, platform, accountID string) string {
	return platform + ":" + accountID
}

func (s *MessageHubService) enqueue(ctx context.Context, msg *model.MessageHub) error {
	key := s.partitionKey(ctx, msg.Platform, msg.AccountID)
	s.mu.Lock()
	stream, ok := s.streams[key]
	if !ok {
		stream = &hubStream{partition: key, messages: make([]*model.MessageHub, 0, 64)}
		stream.cond = sync.NewCond(&stream.mu)
		s.streams[key] = stream
	}
	s.mu.Unlock()

	stream.mu.Lock()
	defer stream.mu.Unlock()
	if stream.closed {
		return errors.New("stream closed")
	}
	if len(stream.messages) >= s.streamSize {
		return ErrMessageHubQueueFull
	}
	stream.messages = append(stream.messages, msg)
	stream.cond.Broadcast()
	return nil
}

// Consume 消费一个分区的下一条消息（按 sent_at 顺序，最多 wait 等待）
func (s *MessageHubService) Consume(ctx context.Context, platform, accountID string, wait time.Duration) (*model.MessageHub, error) {
	key := s.partitionKey(ctx, platform, accountID)

	deadline := time.Now().Add(wait)
	for {
		s.mu.RLock()
		stream, ok := s.streams[key]
		s.mu.RUnlock()
		if ok {
			return s.consumeFromStream(ctx, stream, wait)
		}
		if wait <= 0 || time.Now().After(deadline) {
			if s.repo != nil {
				msg, err := s.repo.GetLastByPlatformAccount(ctx, platform, accountID)
				if err == nil && msg != nil {
					return msg, nil
				}
				if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
					return nil, err
				}
			}
			return nil, nil
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
}

func (s *MessageHubService) consumeFromStream(ctx context.Context, stream *hubStream, wait time.Duration) (*model.MessageHub, error) {
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if len(stream.messages) == 0 {
		if wait <= 0 {
			return nil, nil
		}
		done := make(chan struct{})
		go func() {
			timer := time.NewTimer(wait)
			defer timer.Stop()
			select {
			case <-timer.C:
			case <-ctx.Done():
			}
			stream.mu.Lock()
			stream.cond.Broadcast()
			stream.mu.Unlock()
			close(done)
		}()
		stream.cond.Wait()
		<-done
	}
	if len(stream.messages) == 0 {
		return nil, nil
	}
	idx := 0
	for i, m := range stream.messages {
		if m.SentAt.Before(stream.messages[idx].SentAt) {
			idx = i
		}
	}
	msg := stream.messages[idx]
	stream.messages = append(stream.messages[:idx], stream.messages[idx+1:]...)
	return msg, nil
}

// Peek 预览一个分区的下一条消息（不取出）
func (s *MessageHubService) Peek(ctx context.Context, platform, accountID string) (*model.MessageHub, error) {
	key := s.partitionKey(ctx, platform, accountID)
	s.mu.RLock()
	stream, ok := s.streams[key]
	s.mu.RUnlock()
	if !ok || len(stream.messages) == 0 {
		return nil, nil
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	if len(stream.messages) == 0 {
		return nil, nil
	}
	return stream.messages[0], nil
}

// Size 获取分区队列长度
func (s *MessageHubService) Size(ctx context.Context, platform, accountID string) int {
	key := s.partitionKey(ctx, platform, accountID)
	s.mu.RLock()
	stream, ok := s.streams[key]
	s.mu.RUnlock()
	if !ok {
		return 0
	}
	stream.mu.Lock()
	defer stream.mu.Unlock()
	return len(stream.messages)
}

func (s *MessageHubService) notify(ctx context.Context, msg *model.MessageHub) {
	s.subMu.RLock()
	subs := make([]MessageSubscriber, len(s.subscribers))
	copy(subs, s.subscribers)
	s.subMu.RUnlock()
	// 订阅者异步处理不能复用调用方 ctx（notify 常在 HTTP 请求链路中触发，
	// 请求返回即取消，订阅方处理会被掐断半途），脱离取消链 + 显式超时
	asyncCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
	defer cancel()
	for _, sub := range subs {
		go func(sub MessageSubscriber) {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("[message_hub] notify subscriber panic recovered: %v", r)
				}
			}()
			if sub.Filter(msg) {
				if err := sub.OnMessage(asyncCtx, msg); err != nil {
					logger.Errorf("[message_hub] subscriber OnMessage error: %v", err)
				}
			}
		}(sub)
	}
}

// GenerateMsgID 生成标准 msg_id (用于 outbound 主动消息)
func GenerateMsgID(platform, accountID string) string {
	return fmt.Sprintf("%s-%s-%s", platform, accountID, uuid.NewString())
}

// 标准化不同渠道的原始消息到 PushMessageRequest
type RawChannelMessage struct {
	Platform       string         `json:"platform"`
	AccountID      string         `json:"account_id"`
	MsgID          string         `json:"msg_id"`
	From           string         `json:"from"`
	FromName       string         `json:"from_name"`
	To             string         `json:"to"`
	ToName         string         `json:"to_name"`
	Content        string         `json:"content"`
	MsgType        string         `json:"msg_type"`
	MediaURL       string         `json:"media_url"`
	ConversationID string         `json:"conversation_id"`
	IsGroup        bool           `json:"is_group"`
	GroupID        string         `json:"group_id"`
	SentAt         *time.Time     `json:"sent_at"`
	Extra          map[string]any `json:"extra"`
}

// ConvertFromChannel 渠道原始消息 → PushMessageRequest
func (s *MessageHubService) ConvertFromChannel(ctx context.Context, raw *RawChannelMessage) *PushMessageRequest {
	if raw.MsgType == "" {
		raw.MsgType = "text"
	}
	return &PushMessageRequest{

		Platform:       raw.Platform,
		AccountID:      raw.AccountID,
		MsgID:          raw.MsgID,
		Direction:      "inbound",
		MsgType:        raw.MsgType,
		SenderID:       raw.From,
		SenderName:     raw.FromName,
		ReceiverID:     raw.To,
		ReceiverName:   raw.ToName,
		Content:        raw.Content,
		MediaURL:       raw.MediaURL,
		ConversationID: raw.ConversationID,
		IsGroup:        raw.IsGroup,
		GroupID:        raw.GroupID,
		SentAt:         raw.SentAt,
		Extra:          raw.Extra,
	}
}

// MarshalToJSON 序列化（用于 Redis 队列）
func (s *MessageHubService) MarshalToJSON(ctx context.Context, msg *model.MessageHub) (string, error) {
	b, err := json.Marshal(msg)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// UnmarshalFromJSON 反序列化
func (s *MessageHubService) UnmarshalFromJSON(ctx context.Context, data string) (*model.MessageHub, error) {
	var msg model.MessageHub
	if err := json.Unmarshal([]byte(data), &msg); err != nil {
		return nil, err
	}
	return &msg, nil
}

// ValidPlatform 是否支持该平台
func ValidPlatform(platform string) bool {
	return messageHubPlatforms[platform]
}

// ValidMsgType 是否支持该消息类型
func ValidMsgType(t string) bool {
	return messageHubMsgTypes[t]
}

// ValidDirection 是否支持该方向
func ValidDirection(d string) bool {
	return messageHubDirections[d]
}

// ListPlatforms 列出支持的平台
func ListPlatforms() []string {
	out := make([]string, 0, len(messageHubPlatforms))
	for k := range messageHubPlatforms {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ListMsgTypes 列出支持的消息类型
func ListMsgTypes() []string {
	out := make([]string, 0, len(messageHubMsgTypes))
	for k := range messageHubMsgTypes {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
