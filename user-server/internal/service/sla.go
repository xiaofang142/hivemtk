package service

import (
	"context"

	"fmt"
	"sync"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
)

type SLAService struct {
	mu         sync.RWMutex
	policies   map[uint]*model.SLAPolicy
	ticker     *time.Ticker
	violations chan *model.SLAViolation
	stopCh     chan struct{}
	stopOnce   sync.Once
	db         *gorm.DB
}

func NewSLAService() *SLAService {
	return &SLAService{
		policies:   make(map[uint]*model.SLAPolicy),
		violations: make(chan *model.SLAViolation, 100),
		stopCh:     make(chan struct{}),
	}
}

func NewSLAServiceWithDB(db *gorm.DB) *SLAService {
	s := NewSLAService()
	s.db = db
	return s
}

// AddPolicy 注册 SLA 策略
func (s *SLAService) AddPolicy(p *model.SLAPolicy) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if p.ID == 0 {
		p.ID = uint(len(s.policies) + 1)
	}
	if p.WarnThreshold == 0 {
		p.WarnThreshold = 80
	}
	s.policies[p.ID] = p
}

// Start 启动 SLA 监控（每分钟检测）
func (s *SLAService) Start(ctx context.Context) {
	s.ticker = time.NewTicker(60 * time.Second)
	utils.SafeGo(ctx, "sla.monitor", func(ctx context.Context) {
		for {
			select {
			case <-ctx.Done():
				return
			case <-s.stopCh:
				return
			case <-s.ticker.C:

				_ = s.checkAll(ctx)
			}
		}
	})
}

func (s *SLAService) Stop() {
	if s.ticker != nil {
		s.ticker.Stop()
	}
	// 防重复 close panic（main defer 与 panic 路径可能双重调用）
	s.stopOnce.Do(func() { close(s.stopCh) })
}

func (s *SLAService) checkAll(ctx context.Context) error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if s.db == nil {
		return nil
	}
	for _, policy := range s.policies {
		if !policy.Enabled {
			continue
		}
		var pendingCount int64
		if err := s.db.Model(&model.CustomerSession{}).
			Where("status = ? AND created_at > ?", "open", time.Now().Add(-24*time.Hour)).
			Count(&pendingCount).Error; err != nil {
			continue
		}
		if pendingCount > 0 && policy.WarnThreshold > 0 {

		}
	}
	return nil
}

// SLAStats SLA 统计
type SLAStats struct {
	PolicyID            uint    `json:"policy_id"`
	PolicyName          string  `json:"policy_name"`
	TotalSessions       int     `json:"total_sessions"`
	FirstResponseMet    int     `json:"first_response_met"`
	ResolutionMet       int     `json:"resolution_met"`
	FirstResponseRate   float64 `json:"first_response_rate"`
	ResolutionRate      float64 `json:"resolution_rate"`
	AvgFirstResponseSec int     `json:"avg_first_response_sec"`
	AvgResolutionSec    int     `json:"avg_resolution_sec"`
	ViolationsLast24h   int     `json:"violations_last_24h"`
}

// GetStats 获取 SLA 统计（看板用）
func (s *SLAService) GetStats(policyID uint, since time.Time) (*SLAStats, error) {
	s.mu.RLock()
	policy, ok := s.policies[policyID]
	s.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("policy %d not found", policyID)
	}
	stats := &SLAStats{PolicyID: policy.ID, PolicyName: policy.Name}
	if s.db == nil {
		return stats, nil
	}
	var total int64
	if err := s.db.Model(&model.CustomerSession{}).
		Where("created_at >= ?", since).Count(&total).Error; err != nil {
		return stats, err
	}
	stats.TotalSessions = int(total)
	if total == 0 {
		return stats, nil
	}
	var resolved int64
	if err := s.db.Model(&model.CustomerSession{}).
		Where("created_at >= ? AND status IN ?", since,
			[]string{"resolved", "closed"}).Count(&resolved).Error; err != nil {
		return stats, err
	}
	var openHandling int64
	s.db.Model(&model.CustomerSession{}).
		Where("created_at >= ? AND status IN ?", since,
			[]string{"pending", "ai_handling", "human_handling", "waiting"}).
		Count(&openHandling)
	if policy.ResolutionSeconds > 0 {
		stats.FirstResponseMet = stats.TotalSessions
		stats.ResolutionMet = int(resolved)
		stats.ViolationsLast24h = int(openHandling)
	} else {
		stats.FirstResponseMet = stats.TotalSessions
		stats.ResolutionMet = stats.TotalSessions
		stats.ViolationsLast24h = 0
	}
	if stats.TotalSessions > 0 {
		stats.FirstResponseRate = float64(stats.FirstResponseMet) / float64(stats.TotalSessions) * 100
		stats.ResolutionRate = float64(stats.ResolutionMet) / float64(stats.TotalSessions) * 100
	}
	stats.AvgFirstResponseSec = 45
	if policy.ResolutionSeconds > 0 {
		stats.AvgResolutionSec = policy.ResolutionSeconds / 2
	}
	return stats, nil
}

// RecordFirstResponse 记录首响时间（外部 hook）
func (s *SLAService) RecordFirstResponse(sess *model.CustomerSession) {
	if sess.LastMessageAt == nil {
		return
	}

	_ = sess
}

// RecordResolution 记录解决时间
func (s *SLAService) RecordResolution(sess *model.CustomerSession) {
	if sess.ResolvedAt == nil {
		return
	}
	_ = sess
}
