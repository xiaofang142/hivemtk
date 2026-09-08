package service

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"gorm.io/gorm"

	_db "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

func init() { _db.RegisterExtraModels(&repository.AbExposure{}) }

// 变体常量
const (
	AbVariantControl   = "control"
	AbVariantTreatment = "treatment"
)

// AbExposureBuffer 曝光异步落库缓冲容量
const AbExposureBuffer = 1024

// AbExposure 曝光/转化记录表（结构体权威定义在 repository，此处类型别名保持既有 API 兼容）
type AbExposure = repository.AbExposure

func fnv1a32(s string) uint32 {
	h := uint32(2166136261)
	for i := 0; i < len(s); i++ {
		h ^= uint32(s[i])
		h *= 16777619
	}
	return h
}

// Assign 稳定分桶：同 (expID, customerID) 恒定返回同一变体
func Assign(expID, customerID string) string {
	if fnv1a32(expID+":"+customerID)%100 < 50 {
		return AbVariantControl
	}
	return AbVariantTreatment
}

// AbVariantSummary 变体汇总
type AbVariantSummary struct {
	Variant        string  `json:"variant"`
	Exposed        int64   `json:"exposed"`
	Converted      int64   `json:"converted"`
	ConversionRate float64 `json:"conversion_rate"`
}

// ABExperiment A/B 实验框架（曝光落库 + 转化回填 + 汇总）
type ABExperiment struct {
	exposureRepo *repository.ABExposureRepository
	ch           chan AbExposure
	done         chan struct{}
	cancel       context.CancelFunc
	wg           sync.WaitGroup
	stopOnce     sync.Once
	DroppedCount atomic.Int64
}

// NewABExperiment 构造并启动异步落库 worker（db 为 nil 时纯内存模式）
func NewABExperiment(db *gorm.DB, bufferSize int) *ABExperiment {
	if bufferSize <= 0 {
		bufferSize = AbExposureBuffer
	}
	a := &ABExperiment{
		exposureRepo: repository.NewABExposureRepositoryWithDB(db),
		ch:           make(chan AbExposure, bufferSize),
		done:         make(chan struct{}),
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.cancel = cancel
	a.wg.Add(1)
	go func() {
		defer a.wg.Done()
		for {
			select {
			case <-ctx.Done():
				close(a.done)
				return
			case e := <-a.ch:
				a.insert(e)
			}
		}
	}()
	return a
}

func (a *ABExperiment) insert(e AbExposure) {
	if a.exposureRepo == nil {
		return
	}
	if err := a.exposureRepo.Create(context.Background(), &e); err != nil {
		logger.Errorf("[T-7] 曝光落库失败 exp=%s cust=%s err=%v", e.ExperimentID, e.CustomerID, err)
	}
}

// LogExposure 异步记录曝光（fire-and-forget：缓冲满丢弃并计数，绝不阻塞）
func (a *ABExperiment) LogExposure(expID, variant, customerID, sessionID string) {
	if a == nil {
		return
	}
	select {
	case a.ch <- AbExposure{
		ExperimentID: expID,
		Variant:      variant,
		CustomerID:   customerID,
		SessionID:    sessionID,
		ExposedAt:    time.Now(),
	}:
	default:
		a.DroppedCount.Add(1)
	}
}

// MarkConversion 回填转化时间（首次转化生效；nil db 安全跳过）
func (a *ABExperiment) MarkConversion(expID, customerID string) {
	if a == nil || a.exposureRepo == nil {
		return
	}
	if err := a.exposureRepo.MarkConversion(context.Background(), expID, customerID, time.Now()); err != nil {
		logger.Errorf("[T-7] 转化回填失败 exp=%s cust=%s err=%v", expID, customerID, err)
	}
}

// Summaries 汇总窗口内各变体曝光数/转化数/转化率
func (a *ABExperiment) Summaries(expID string, window time.Duration) map[string]AbVariantSummary {
	res := map[string]AbVariantSummary{
		AbVariantControl:   {Variant: AbVariantControl},
		AbVariantTreatment: {Variant: AbVariantTreatment},
	}
	if a == nil || a.exposureRepo == nil {
		return res
	}
	since := time.Now().Add(-window)
	rows, err := a.exposureRepo.ListSince(context.Background(), expID, since)
	if err != nil {
		logger.Errorf("[T-7] 汇总查询失败 exp=%s err=%v", expID, err)
		return res
	}
	for _, r := range rows {
		s, ok := res[r.Variant]
		if !ok {
			s = AbVariantSummary{Variant: r.Variant}
		}
		s.Exposed++
		if r.ConvertedAt != nil {
			s.Converted++
		}
		res[r.Variant] = s
	}
	for k, s := range res {
		if s.Exposed > 0 {
			s.ConversionRate = float64(s.Converted) / float64(s.Exposed)
		}
		res[k] = s
	}
	return res
}

// Stop 幂等停止：取消 worker 并等待退出（缓冲中未落库曝光不保证写入）
func (a *ABExperiment) Stop() {
	a.stopOnce.Do(func() {
		if a.cancel != nil {
			a.cancel()
		}
	})
	a.wg.Wait()
}

var (
	abExperimentOnce sync.Once
	abExperiment     *ABExperiment
)

// InitABExperiment 全局初始化（main 装配阶段调用一次）
func InitABExperiment(db *gorm.DB) *ABExperiment {
	abExperimentOnce.Do(func() {
		abExperiment = NewABExperiment(db, AbExposureBuffer)
	})
	return abExperiment
}

// GetABExperiment 获取全局实例（未初始化返回 nil）
func GetABExperiment() *ABExperiment { return abExperiment }
