package service

import (
	"context"
	"sync"
	"time"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// FeedbackLearner 反馈学习器
// 商业价值：智能体不是一次性的，每次客户反馈/人工接管/数据积累都让 AI 越来越懂
type FeedbackLearner struct {
	recordRepo  *repository.FeedbackRecordRepository
	mu          sync.RWMutex
	intentCache map[string]*IntentStats
	sopCache    map[string]*SOPStats
}

// IntentStats 意图统计
type IntentStats struct {
	IntentType    string
	TotalCount    int
	SuccessCount  int
	FailCount     int
	AvgConfidence float64
	LastUpdated   time.Time
}

// SOPStats SOP 表现
type SOPStats struct {
	SOPName      string
	TotalUsed    int
	PositiveRate float64
	AvgTokens    int
	LastUsed     time.Time
}

// FeedbackRecord 反馈记录
type FeedbackRecord struct {
	SessionID      string    `json:"session_id"`
	CustomerID     string    `json:"customer_id"`
	IntentType     string    `json:"intent_type"`
	Confidence     float64   `json:"confidence"`
	SOPName        string    `json:"sop_name"`
	AIReply        string    `json:"ai_reply"`
	HumanReply     string    `json:"human_reply,omitempty"`
	CustomerAccept bool      `json:"customer_accept"`
	Transferred    bool      `json:"transferred"`
	TransferReason string    `json:"transfer_reason,omitempty"`
	Tokens         int       `json:"tokens"`
	LatencyMs      int       `json:"latency_ms"`
	CreatedAt      time.Time `json:"created_at"`
}

// NewFeedbackLearner 创建反馈学习器
func NewFeedbackLearner(db *gorm.DB) *FeedbackLearner {
	return &FeedbackLearner{
		recordRepo:  repository.NewFeedbackRecordRepository(db),
		intentCache: make(map[string]*IntentStats),
		sopCache:    make(map[string]*SOPStats),
	}
}

func (f *FeedbackLearner) RecordFeedback(ctx context.Context, record *FeedbackRecord) error {
	if record == nil {
		return nil
	}
	if record.CreatedAt.IsZero() {
		record.CreatedAt = time.Now()
	}
	if f.recordRepo != nil {
		orm := &model.FeedbackRecordORM{
			SessionID:      record.SessionID,
			CustomerID:     record.CustomerID,
			IntentType:     record.IntentType,
			Confidence:     record.Confidence,
			SOPName:        record.SOPName,
			AIReply:        record.AIReply,
			HumanReply:     record.HumanReply,
			CustomerAccept: record.CustomerAccept,
			Transferred:    record.Transferred,
			TransferReason: record.TransferReason,
			Tokens:         record.Tokens,
			LatencyMs:      record.LatencyMs,
			CreatedAt:      record.CreatedAt,
		}
		if err := f.recordRepo.Create(ctx, orm); err != nil {
			logger.Ctx(ctx).Warn().Err(err).Str("session_id", record.SessionID).
				Msg("[feedback_learner] persist feedback record failed, fallback to in-memory only")
		}
	}
	f.updateIntentCache(ctx, record)
	f.updateSOPCache(ctx, record)
	return nil
}

func (f *FeedbackLearner) updateIntentCache(ctx context.Context, record *FeedbackRecord) {
	if record.IntentType == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	stats, ok := f.intentCache[record.IntentType]
	if !ok {
		stats = &IntentStats{IntentType: record.IntentType}
		f.intentCache[record.IntentType] = stats
	}
	stats.TotalCount++
	stats.AvgConfidence = (stats.AvgConfidence*float64(stats.TotalCount-1) + record.Confidence) / float64(stats.TotalCount)
	if record.CustomerAccept && !record.Transferred {
		stats.SuccessCount++
	} else {
		stats.FailCount++
	}
	stats.LastUpdated = time.Now()
}

func (f *FeedbackLearner) updateSOPCache(ctx context.Context, record *FeedbackRecord) {
	if record.SOPName == "" {
		return
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	stats, ok := f.sopCache[record.SOPName]
	if !ok {
		stats = &SOPStats{SOPName: record.SOPName}
		f.sopCache[record.SOPName] = stats
	}
	stats.TotalUsed++
	if record.CustomerAccept && !record.Transferred {
		stats.PositiveRate = (stats.PositiveRate*float64(stats.TotalUsed-1) + 1.0) / float64(stats.TotalUsed)
	} else {
		stats.PositiveRate = (stats.PositiveRate * float64(stats.TotalUsed-1)) / float64(stats.TotalUsed)
	}
	stats.AvgTokens = (stats.AvgTokens*(stats.TotalUsed-1) + record.Tokens) / stats.TotalUsed
	stats.LastUsed = time.Now()
}

// GetIntentStats 获取意图统计
func (f *FeedbackLearner) GetIntentStats(ctx context.Context, intentType string) *IntentStats {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if stats, ok := f.intentCache[intentType]; ok {
		copy := *stats
		return &copy
	}
	return nil
}

// GetAllIntentStats 获取所有意图统计
func (f *FeedbackLearner) GetAllIntentStats(ctx context.Context) []*IntentStats {
	f.mu.RLock()
	defer f.mu.RUnlock()
	stats := make([]*IntentStats, 0, len(f.intentCache))
	for _, s := range f.intentCache {
		copy := *s
		stats = append(stats, &copy)
	}
	return stats
}

// GetSOPStats 获取 SOP 统计
func (f *FeedbackLearner) GetSOPStats(ctx context.Context, sopName string) *SOPStats {
	f.mu.RLock()
	defer f.mu.RUnlock()
	if stats, ok := f.sopCache[sopName]; ok {
		copy := *stats
		return &copy
	}
	return nil
}

// SuggestBestSOP 建议最佳 SOP（基于历史表现）
func (f *FeedbackLearner) SuggestBestSOP(ctx context.Context, intentType string) string {
	return ""
}

// SuggestConfidenceFloor 建议该意图的最低置信度阈值
// 历史数据：投诉类意图置信度低时容易误判，建议提高阈值
func (f *FeedbackLearner) SuggestConfidenceFloor(ctx context.Context, intentType string) float64 {
	stats := f.GetIntentStats(ctx, intentType)
	if stats == nil || stats.TotalCount < 10 {
		return 0.5
	}
	successRate := float64(stats.SuccessCount) / float64(stats.TotalCount)
	if successRate >= 0.8 {
		return 0.4
	} else if successRate >= 0.6 {
		return 0.55
	} else {
		return 0.7
	}
}
