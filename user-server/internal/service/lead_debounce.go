package service

import (
	"context"
	"sync"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/utils/logger"
)

// LeadDebounceService 线索检测防抖去重（FR-014）。
//
// 不新造一套窗口配置：直接复用 ai_debounce.go 的 aiDebounceSeconds(ctx, event)
// ——它统一读取参数中心 BridgeAIDebounceSeconds + AI_DEBOUNCE_SECONDS + 事件级覆盖，
// 并受 aiDebounceMaxSeconds 上限约束。进程内去重表与既有
// lead_mining_notify_external.go 的同族窗口口径一致，按租户隔离 key。
type LeadDebounceService struct {
	mu   sync.Mutex
	seen map[string]time.Time
}

func NewLeadDebounceService() *LeadDebounceService {
	return &LeadDebounceService{seen: make(map[string]time.Time)}
}

// leadDebounceSingleton 进程内共享的去重表；每次处理都 new 会让窗口永远为空。
var leadDebounceSingleton = NewLeadDebounceService()

// LeadDebounce 返回进程级共享的防抖服务。
func LeadDebounce() *LeadDebounceService { return leadDebounceSingleton }

// TryClaim 尝试占用去重窗口。同 key 在窗口内已占用返回 false（调用方跳过本次）。
// evt 只用于取窗口秒数；传 nil 时用系统默认窗口。
func (s *LeadDebounceService) TryClaim(ctx context.Context, evt *model.MessageEvent, parts ...string) bool {
	if s == nil {
		return true
	}
	seconds := aiDebounceSeconds(ctx, evt) // 复用 ai_debounce.go 的唯一窗口来源
	if seconds <= 0 {
		return true
	}
	tid, ok := tenant.FromContext(ctx)
	if !ok || tid == 0 {
		if tenant.IsMultiTenant() {
			logger.Ctx(ctx).Warn().Msg("[lead-debounce] 缺租户上下文，拒绝占用去重窗口")
			return false
		}
		tid = tenant.DefaultTenantID
	}
	key := leadDebounceKey(tid, parts...)
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if last, found := s.seen[key]; found && now.Sub(last) < time.Duration(seconds)*time.Second {
		return false
	}
	// 惰性清理，避免长期运行的进程无界增长。
	if len(s.seen) > 4096 {
		cutoff := now.Add(-time.Duration(seconds) * time.Second)
		for k, t := range s.seen {
			if t.Before(cutoff) {
				delete(s.seen, k)
			}
		}
	}
	s.seen[key] = now
	return true
}

func leadDebounceKey(tenantID uint, parts ...string) string {
	key := "lead:" + tenantString(tenantID)
	for _, p := range parts {
		key += "|" + p
	}
	return key
}

// LeadDebounceKeyForTenant 显式带租户维度的 key 生成器，供调用方组装 parts 时使用。
func LeadDebounceKeyForTenant(tenantID uint, accountID, conversationID, contentHash string) []string {
	return []string{
		tenantString(tenantID), accountID, conversationID, contentHash,
	}
}

func tenantString(id uint) string {
	if id == 0 {
		return "0"
	}
	const digits = "0123456789"
	var buf [20]byte
	i := len(buf)
	for id > 0 {
		i--
		buf[i] = digits[id%10]
		id /= 10
	}
	return string(buf[i:])
}
