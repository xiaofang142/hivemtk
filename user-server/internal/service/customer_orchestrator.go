package service

import (
	"context"
	"fmt"
	"runtime/debug"
	"sync"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
)

// ClueScoreRFMUpdater 线索评分 RFM 回流端口（F- 依赖倒置）。
// 由 *ClueScoreService 实现，在 main.go 装配阶段注入。
type ClueScoreRFMUpdater interface {
	UpdateByCustomerRFM(ctx context.Context, customerID string, segment string, compositeScore int) error
}

// SegmentRecomputer 分群重算端口（F- 依赖倒置）。
// 由 *SegmentService 实现，在 main.go 装配阶段注入。
type SegmentRecomputer interface {
	RecomputeForCustomer(ctx context.Context, customerID string) error
}

// CustomerOrchestrator 客户业务编排层
type CustomerOrchestrator struct {
	journey       *CustomerJourneyService
	tagger        *AutoTagger
	cache         cache.Cache
	clueScoreUpd  ClueScoreRFMUpdater
	segmentRecomp SegmentRecomputer
}

// NewCustomerOrchestrator 创建客户业务编排层实例。
// 默认依赖在内部创建；外部可通过 SetXxx 替换为测试 mock。
func NewCustomerOrchestrator() *CustomerOrchestrator {
	return &CustomerOrchestrator{
		journey: NewCustomerJourneyService(),
		tagger:  NewAutoTagger(),
		cache:   cache.GetGlobalCache(),
	}
}

// NewCustomerOrchestratorWithDeps 使用指定依赖创建（用于测试）。
func NewCustomerOrchestratorWithDeps(
	journey *CustomerJourneyService,
	tagger *AutoTagger,
	c cache.Cache,
) *CustomerOrchestrator {
	return &CustomerOrchestrator{
		journey: journey,
		tagger:  tagger,
		cache:   c,
	}
}

// SetJourneyService 注入客户旅程服务。
func (o *CustomerOrchestrator) SetJourneyService(j *CustomerJourneyService) {
	o.journey = j
}

// SetTagger 注入自动标签服务。
func (o *CustomerOrchestrator) SetTagger(t *AutoTagger) {
	o.tagger = t
}

// SetCache 注入缓存实例。
func (o *CustomerOrchestrator) SetCache(c cache.Cache) {
	o.cache = c
}

// SetClueScoreUpdater 注入线索评分 RFM 回流端口（F-）。
func (o *CustomerOrchestrator) SetClueScoreUpdater(u ClueScoreRFMUpdater) {
	o.clueScoreUpd = u
}

// SetSegmentRecomputer 注入分群重算端口（F-）。
func (o *CustomerOrchestrator) SetSegmentRecomputer(r SegmentRecomputer) {
	o.segmentRecomp = r
}

// OnCustomerCreated 客户创建后联动：
//  1. JourneyService.Touch 记录首次互动（如客户已存在则更新 LastTouchAt）
//  2. 标签初始化（AutoTagger.EvaluateAndTag 评估规则后打标）
//
// 调用方：CustomerService.CreateOrUpdate（仅新建分支调用）
func (o *CustomerOrchestrator) OnCustomerCreated(ctx context.Context, customer *model.Customer) {
	if o == nil || customer == nil || customer.ID == "" {
		return
	}
	if o.journey != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("orchestrator.OnCustomerCreated: journey.Touch panic: %v\n%s", r, debug.Stack())
				}
			}()
			o.journey.Touch(ctx, customer.ID, "system_create")
		}()
	}
	if o.tagger != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("orchestrator.OnCustomerCreated: tagger.EvaluateAndTag panic: %v\n%s", r, debug.Stack())
				}
			}()
			if err := o.tagger.EvaluateAndTag(ctx, customer.ID); err != nil {
				logger.Errorf("orchestrator.OnCustomerCreated: tagger.EvaluateAndTag error: %v", err)
			}
		}()
	}
}

// OnCustomerEvent 客户事件后联动：
//
// EC-24 不变量（服务事件先行，营销后置）：本函数仅在服务事实事件已落库后联动旅程/标签，
//
//	 不得触发营销外发；营销链路必须独立晚于服务链路，且失败不得回写服务状态。
//	1. JourneyService.Touch 记录互动（不改变阶段）
//	2. JourneyService.Transition 根据事件类型自动迁移阶段（purchase → won）
//	3. TagService.AutoTag 重新评估标签
//
// 调用方：EventTracker.Track
func (o *CustomerOrchestrator) OnCustomerEvent(ctx context.Context, customerID string, event *model.CustomerEvent) {
	if o == nil || customerID == "" || event == nil {
		return
	}
	if o.journey != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("orchestrator.OnCustomerEvent: journey.Touch panic: %v\n%s", r, debug.Stack())
				}
			}()
			o.journey.Touch(ctx, customerID, string(event.EventSource))
		}()
	}
	if o.journey != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("orchestrator.OnCustomerEvent: journey.Transition panic: %v\n%s", r, debug.Stack())
				}
			}()
			targetStage := stageForEvent(event.EventType)
			if targetStage != "" {
				if _, err := o.journey.Transition(ctx, customerID, targetStage,
					string(event.EventSource), "system", "auto_from_event", nil); err != nil {
					logger.Errorf("orchestrator.OnCustomerEvent: journey.Transition error: %v", err)
				}
			}
		}()
	}
	if o.tagger != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("orchestrator.OnCustomerEvent: tagger.ProcessEvent panic: %v\n%s", r, debug.Stack())
				}
			}()
			if err := o.tagger.ProcessEvent(ctx, event); err != nil {
				logger.Errorf("orchestrator.OnCustomerEvent: tagger.ProcessEvent error: %v", err)
			}
		}()
	}
}

// OnRFMComputed RFM 计算完成后联动：
//  1. 失效 360 缓存（避免读到旧 RFM 数据）
//  2. 更新 RFM 标签（champion/loyal/at_risk/churn/potential）
//
// 3. 回流线索评分（F-：调 ClueScoreService.UpdateByCustomerRFM）
// 4. 触发分群重算（F-：调 SegmentService.RecomputeForCustomer）
//
// 调用方：CustomerRFMService.ComputeForCustomer
func (o *CustomerOrchestrator) OnRFMComputed(ctx context.Context, customerID string, segment string) {
	if o == nil || customerID == "" {
		return
	}
	if o.cache != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("orchestrator.OnRFMComputed: cache.Delete panic: %v\n%s", r, debug.Stack())
				}
			}()
			key := fmt.Sprintf("customer_360:%s", customerID)
			if err := o.cache.Delete(ctx, key); err != nil {
				logger.Errorf("orchestrator.OnRFMComputed: cache.Delete(%s) error: %v", key, err)
			}
		}()
	}
	if o.tagger != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("orchestrator.OnRFMComputed: tagger.EvaluateAndTag panic: %v\n%s", r, debug.Stack())
				}
			}()
			if err := o.tagger.EvaluateAndTag(ctx, customerID); err != nil {
				logger.Errorf("orchestrator.OnRFMComputed: tagger.EvaluateAndTag error: %v", err)
			}
		}()
	}
	if o.clueScoreUpd != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("orchestrator.OnRFMComputed: clueScoreUpd.UpdateByCustomerRFM panic: %v\n%s", r, debug.Stack())
				}
			}()
			if err := o.clueScoreUpd.UpdateByCustomerRFM(ctx, customerID, segment, 0); err != nil {
				logger.Errorf("orchestrator.OnRFMComputed: clueScoreUpd.UpdateByCustomerRFM error: %v", err)
			}
		}()
	}
	if o.segmentRecomp != nil {
		func() {
			defer func() {
				if r := recover(); r != nil {
					logger.Errorf("orchestrator.OnRFMComputed: segmentRecomp.RecomputeForCustomer panic: %v\n%s", r, debug.Stack())
				}
			}()
			if err := o.segmentRecomp.RecomputeForCustomer(ctx, customerID); err != nil {
				logger.Errorf("orchestrator.OnRFMComputed: segmentRecomp.RecomputeForCustomer error: %v", err)
			}
		}()
	}
}

func stageForEvent(eventType model.EventType) JourneyStage {
	switch eventType {
	case model.EventTypeSignup:
		return StageLead
	case model.EventTypeLogin, model.EventTypePageView, model.EventTypeClick:
		return StageContact
	case model.EventTypeAddToCart:
		return StageInterested
	case model.EventTypePurchase:
		return StageWon
	default:
		return ""
	}
}

var (
	globalOrch *CustomerOrchestrator
	globalMu   sync.RWMutex
)

// SetGlobalOrchestrator 设置全局客户业务编排层（main.go 启动阶段调用一次）。
func SetGlobalOrchestrator(o *CustomerOrchestrator) {
	globalMu.Lock()
	defer globalMu.Unlock()
	globalOrch = o
}

// GetGlobalOrchestrator 获取全局客户业务编排层（未初始化返回 nil）。
func GetGlobalOrchestrator() *CustomerOrchestrator {
	globalMu.RLock()
	defer globalMu.RUnlock()
	return globalOrch
}
