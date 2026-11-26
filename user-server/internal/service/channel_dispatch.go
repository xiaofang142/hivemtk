package service

import (
	"context"
	"errors"
	"fmt"
	"hash/fnv"
	"strconv"
	"strings"
	"sync/atomic"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// ChannelDispatchService 账号自动均衡收发（路由2）。
// 策略：会话粘性优先（已有会话绑定账号）-> 启用账号 round-robin。
// 五层纪律：数据访问全部经 repository，本层不持有 *gorm.DB。
type ChannelDispatchService struct {
	channelRepo repository.ChannelRepository
	convRepo    ChannelDispatchConversationReader
	accRepo     repository.ChannelAccountRepository
	// gate CH-05/CH-10：真实运行入口的启停闸门。
	// 默认生产构造注入「行状态闸门」（渠道主表 + 账号状态）；需要租户 KV 启停
	// 的入口用 WithGate 显式注入完整 RequireRunnable 口径（见 channelRowStatusGate 注释）。
	// WithRepos 构造保持 nil，兼容既有只测选号语义的用例。
	gate ChannelRunnableGate
	rr   atomic.Uint64
}

// channelRowStatusGate 是默认生产构造使用的轻量闸门：只校验渠道主表 Enabled + 账号 Status。
//
// 为何不默认注入完整 RequireRunnable（租户 KV）：租户 KV 依赖 system_config_kv 表，
// 存量/夹具环境可能尚未初始化该表；若默认 fail-closed，会把「未配置 KV」误判为停用，
// 直接拦断既有 AI 出站（CS06 全链回归实证：system_config_kv 不存在 ⇒ 出站 leg 静默失败）。
// 因此默认闸门只守两行状态，租户 KV 启停由显式 WithGate 装配到已完成预设初始化的入口。
type channelRowStatusGate struct {
	reader ChannelStatusReader
}

func (g *channelRowStatusGate) RequireRunnable(ctx context.Context, tenantID, channelID, accountID uint) (*ResolvedChannelAccount, error) {
	if tenantID == 0 || channelID == 0 || accountID == 0 {
		return nil, ErrChannelAccountInvalid
	}
	if g == nil || g.reader == nil || !g.reader.Available() {
		return nil, repository.ErrChannelStoreUnavailable
	}
	ch, err := g.reader.ChannelByID(ctx, tenantID, channelID)
	if err != nil {
		return nil, err
	}
	if ch == nil || !ch.Enabled {
		return nil, ErrChannelDisabled
	}
	acc, err := g.reader.AccountByID(ctx, tenantID, accountID)
	if err != nil {
		return nil, err
	}
	if acc == nil || acc.Status != 1 || acc.ChannelID != channelID {
		return nil, ErrChannelAccountDisabled
	}
	return &ResolvedChannelAccount{
		TenantID: tenantID, ChannelID: channelID, AccountID: accountID,
		ChannelCode: ch.Code, IdentifyCode: acc.IdentifyCode,
	}, nil
}

// ChannelDispatchConversationReader 是渠道分发只需要的最小会话查询端口。
type ChannelDispatchConversationReader interface {
	FindLatestByPlatformAndPeer(
		ctx context.Context,
		tenantID uint,
		platform string,
		byConversationID string,
		peer string,
	) (*model.InboxConversation, error)
}

// NewChannelDispatchService 使用全局 DB 对应的默认仓储构造。
// 生产口默认装配 CH-05 行状态闸门：停用渠道/账号在 bridge 出站、主动触达、
// 宏、webhook 出站等各调用方自动生效，无需各入口各自接线；租户 KV 启停
// 通过 WithGate 显式装配到预设已初始化的入口（见 channelRowStatusGate）。
func NewChannelDispatchService() *ChannelDispatchService {
	chRepo := repository.NewChannelRepository()
	accRepo := repository.NewChannelAccountRepository()
	return NewChannelDispatchServiceWithGate(
		chRepo,
		repository.NewInboxConversationRepository(),
		accRepo,
		&channelRowStatusGate{reader: NewChannelStatusReader(chRepo, accRepo)},
	)
}

// NewChannelDispatchServiceWithRepos 用显式仓储构造（装配点/测试用）。
// 不注入闸门，保留纯选号语义（粘性/分页/轮转）的既有契约。
func NewChannelDispatchServiceWithRepos(
	channelRepo repository.ChannelRepository,
	convRepo ChannelDispatchConversationReader,
	accRepo repository.ChannelAccountRepository,
) *ChannelDispatchService {
	return NewChannelDispatchServiceWithGate(channelRepo, convRepo, accRepo, nil)
}

// NewChannelDispatchServiceWithGate 注入显式仓储 + 运行闸门（装配点/测试用）。
func NewChannelDispatchServiceWithGate(
	channelRepo repository.ChannelRepository,
	convRepo ChannelDispatchConversationReader,
	accRepo repository.ChannelAccountRepository,
	gate ChannelRunnableGate,
) *ChannelDispatchService {
	return &ChannelDispatchService{
		channelRepo: channelRepo,
		convRepo:    convRepo,
		accRepo:     accRepo,
		gate:        gate,
	}
}

// runnable CH-05：闸门命中即按注入口径校验渠道/账号（默认行状态，WithGate 可含租户 KV）；
// 闸门缺席为纯选号模式。
func (s *ChannelDispatchService) runnable(ctx context.Context, tenantID, channelID, accountID uint) error {
	if s == nil || s.gate == nil {
		return nil
	}
	_, err := s.gate.RequireRunnable(ctx, tenantID, channelID, accountID)
	return err
}

type DispatchPickInput struct {
	TenantID           uint
	Channel            string // platform code, e.g. wechat
	ChannelID          uint
	ConversationID     string
	CustomerID         string
	PreferredAccountID uint
}

type DispatchPickResult struct {
	AccountID    uint   `json:"account_id"`
	AccountName  string `json:"account_name"`
	IdentifyCode string `json:"identify_code"`
	Reason       string `json:"reason"`
}

// PickAccount 为发送选择账号。
func (s *ChannelDispatchService) PickAccount(ctx context.Context, in DispatchPickInput) (*DispatchPickResult, error) {
	if in.TenantID == 0 {
		if tid, ok := tenant.FromContext(ctx); ok {
			in.TenantID = tid
		}
	}
	if in.TenantID == 0 {
		return nil, fmt.Errorf("tenant required")
	}
	if in.ChannelID == 0 && in.Channel != "" {
		ch, err := s.channelRepo.GetByTenantAndCode(ctx, in.TenantID, in.Channel)
		if err != nil {
			return nil, fmt.Errorf("resolve dispatch channel: %w", err)
		}
		if ch != nil {
			in.ChannelID = ch.ID
		}
	}
	if in.ChannelID == 0 {
		return nil, fmt.Errorf("channel required")
	}

	// 1) 会话粘性：已有会话绑定账号
	if in.ConversationID != "" || in.CustomerID != "" {
		peer := in.CustomerID
		if in.ConversationID != "" {
			peer = in.ConversationID
		}
		conv, err := s.convRepo.FindLatestByPlatformAndPeer(ctx, in.TenantID, in.Channel, in.ConversationID, peer)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("query sticky conversation: %w", err)
		}
		if err == nil && conv != nil && conv.AccountID != "" {
			// account_id in inbox conversation is string
			var aid uint
			_, _ = fmt.Sscanf(conv.AccountID, "%d", &aid)
			if aid > 0 {
				acc, err := s.accRepo.GetByID(ctx, in.TenantID, aid)
				if err != nil {
					return nil, fmt.Errorf("query sticky account: %w", err)
				}
				// 粘性命中必须同时校验渠道：错误渠道的粘性记录显式拒绝，
				// 不跨渠道串发，也不静默换到别的账号（CH-10）。
				if acc != nil && acc.ChannelID != in.ChannelID {
					return nil, fmt.Errorf("sticky account %d belongs to channel %d, expected %d", aid, acc.ChannelID, in.ChannelID)
				}
				if acc != nil && acc.Status == 1 {
					// 显式会话粘性：闸门拒绝即显式失败，不静默换号（CH-10）。
					if err := s.runnable(ctx, in.TenantID, in.ChannelID, acc.ID); err != nil {
						return nil, fmt.Errorf("sticky account %d not runnable: %w", acc.ID, err)
					}
					return &DispatchPickResult{AccountID: acc.ID, AccountName: acc.Name, IdentifyCode: acc.IdentifyCode, Reason: "sticky"}, nil
				}
			}
		}
	}

	// 2) preferred
	if in.PreferredAccountID > 0 {
		acc, err := s.accRepo.GetByID(ctx, in.TenantID, in.PreferredAccountID)
		if err != nil {
			return nil, fmt.Errorf("query preferred account: %w", err)
		}
		if acc != nil && acc.Status == 1 && acc.ChannelID == in.ChannelID {
			// 显式 preferred 与会话粘性同语义：闸门拒绝显式失败，不悄悄换号。
			if err := s.runnable(ctx, in.TenantID, in.ChannelID, acc.ID); err != nil {
				return nil, fmt.Errorf("preferred account %d not runnable: %w", acc.ID, err)
			}
			return &DispatchPickResult{AccountID: acc.ID, AccountName: acc.Name, IdentifyCode: acc.IdentifyCode, Reason: "preferred"}, nil
		}
	}

	// 3) round-robin among active accounts（分页拉取全部候选，不漏第 500 条之后的账号）
	status := 1
	const pageSize = 500
	var accs []model.ChannelAccount
	for offset := 0; ; offset += pageSize {
		page, err := s.accRepo.List(ctx, repository.ChannelAccountListOptions{
			TenantID: in.TenantID, ChannelID: in.ChannelID, Status: &status,
			Limit: pageSize, Offset: offset,
		})
		if err != nil {
			return nil, err
		}
		accs = append(accs, page...)
		if len(page) < pageSize {
			break
		}
	}
	if len(accs) == 0 {
		return nil, fmt.Errorf("no active account")
	}
	start := int(s.rr.Add(1)-1) % len(accs)
	// deterministic offset by conversation for better distribution
	if in.ConversationID != "" {
		h := fnv.New32a()
		_, _ = h.Write([]byte(in.ConversationID))
		start = int(h.Sum32()) % len(accs)
	}
	// CH-05/CH-10：轮转候选逐个过闸门。停用/故障/租户关闭的账号被跳过，
	// 保证选中、获取下发、执行与回执始终归同一启用账号；全被拒时显式失败。
	for k := 0; k < len(accs); k++ {
		acc := accs[(start+k)%len(accs)]
		if err := s.runnable(ctx, in.TenantID, in.ChannelID, acc.ID); err != nil {
			continue
		}
		return &DispatchPickResult{AccountID: acc.ID, AccountName: acc.Name, IdentifyCode: acc.IdentifyCode, Reason: "round_robin"}, nil
	}
	return nil, fmt.Errorf("no runnable account")
}

// ResolveExplicitOutboundAccount 解析出站账号：只有空账号允许自动分派；bridge 显式账号
// 必须精确命中租户内启用账号与渠道，任何无效或仓储错误都原样返回，绝不退化为其他账号。
func (s *ChannelDispatchService) ResolveExplicitOutboundAccount(ctx context.Context, tenantID uint, channel, accountID string) (string, error) {
	accountID = strings.TrimSpace(accountID)
	if !isBridgeOutboundChannel(WebhookChannel(channel)) {
		return accountID, nil
	}
	if s == nil || s.accRepo == nil || s.channelRepo == nil {
		return "", fmt.Errorf("outbound account resolver unavailable")
	}
	if tenantID == 0 {
		if tid, ok := tenant.FromContext(ctx); ok {
			tenantID = tid
		}
	}
	if tenantID == 0 {
		return "", fmt.Errorf("outbound account tenant required")
	}
	if accountID == "" {
		pick, err := s.PickAccount(ctx, DispatchPickInput{TenantID: tenantID, Channel: channel})
		if err != nil {
			return "", fmt.Errorf("pick outbound account: %w", err)
		}
		if pick == nil || pick.AccountID == 0 {
			return "", fmt.Errorf("pick outbound account: no active account")
		}
		return strconv.FormatUint(uint64(pick.AccountID), 10), nil
	}

	id, err := strconv.ParseUint(accountID, 10, 32)
	if err != nil || id == 0 {
		return "", fmt.Errorf("invalid explicit outbound account ID %q", accountID)
	}
	acc, err := s.accRepo.GetByID(ctx, tenantID, uint(id))
	if err != nil {
		return "", fmt.Errorf("query explicit outbound account: %w", err)
	}
	if acc == nil {
		return "", fmt.Errorf("explicit outbound account %s not found", accountID)
	}
	if acc.Status != 1 {
		return "", fmt.Errorf("explicit outbound account %s is disabled", accountID)
	}
	ch, err := s.channelRepo.GetByID(ctx, tenantID, acc.ChannelID)
	if err != nil {
		return "", fmt.Errorf("query explicit outbound channel: %w", err)
	}
	if ch == nil {
		return "", fmt.Errorf("explicit outbound channel %d not found or disabled by edition", acc.ChannelID)
	}
	requested := strings.TrimSpace(channel)
	if !strings.EqualFold(strings.TrimSpace(ch.Code), requested) && strconv.FormatUint(uint64(ch.ID), 10) != requested {
		return "", fmt.Errorf("explicit outbound account %s channel mismatch: account=%s requested=%s", accountID, ch.Code, requested)
	}
	// CH-05/CH-10：显式账号也要过运行闸门（默认渠道主表 + 账号状态；显式
	// WithGate 装配时可含租户 KV），任一关闭/故障即拒绝，不得借旧连接继续下发。
	if err := s.runnable(ctx, tenantID, acc.ChannelID, acc.ID); err != nil {
		return "", fmt.Errorf("explicit outbound account %s not runnable: %w", accountID, err)
	}
	return strconv.FormatUint(uint64(acc.ID), 10), nil
}
