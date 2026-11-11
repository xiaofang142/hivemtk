// reflux collector：触发源异步沉淀 unknown_questions（T1）。
//
// 有界队列 + 后台 goroutine 消费：问答主链路只做非阻塞入队；
// 队满丢弃并计数（丢弃率日志），panic 由 SafeGo 兜底。
package reflux

import (
	"context"
	"sync"
	"sync/atomic"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"
)

// Event 一次未知问题触发事件
type Event struct {
	Question     string // 原始问句
	Source       string // model.UQSource*
	SourceID     string // signal_id / decision_id / bad_case_id / csat_id
	SessionID    string
	CustomerID   string
	AgentID      uint
	IntentType   string
	LowConfScore float64
}

// Collector 异步收集器（2026-10-25：句柄下沉，db 为构造瞬时参量）
type Collector struct {
	repo  *repository.RefluxRepository
	ch    chan Event
	wg    sync.WaitGroup
	drops int64 // 队满丢弃计数（观测丢弃率）

	mu        sync.Mutex
	clusterer *Clusterer
	enabled   bool
}

// NewCollector 构造并启动消费 goroutine。
// queueSize 有界队列容量；refluxEnabled=false 时仅建结构不消费（灰度开关）。
func NewCollector(repo *repository.RefluxRepository, queueSize int, refluxEnabled bool) *Collector {
	if queueSize <= 0 {
		queueSize = 512
	}
	c := &Collector{repo: repo, ch: make(chan Event, queueSize), enabled: refluxEnabled}
	if refluxEnabled {
		c.wg.Add(1)
		go c.consumeLoop()
	}
	return c
}

// SetClusterer 注入归簇器（延迟装配；nil = 只沉淀不归簇）
func (c *Collector) SetClusterer(cl *Clusterer) {
	c.mu.Lock()
	c.clusterer = cl
	c.mu.Unlock()
}

// Enabled 灰度开关状态
func (c *Collector) Enabled() bool { return c.enabled }

// Stop 停止消费（等待队列排空或超时）
func (c *Collector) Stop() {
	if !c.enabled {
		return
	}
	c.enabled = false
	done := make(chan struct{})
	go func() { c.wg.Wait(); close(done) }() // gospawn-ok: 等待型 helper（wg.Wait→close done，CC-01 白名单），无业务 panic 面
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		logger.Warnf("[reflux] collector stop timeout, queue not drained")
	}
}

// Emit 非阻塞入队；队满丢弃并计数。任何调用方（问答主链路）都不应被阻塞。
func (c *Collector) Emit(ev Event) {
	if c == nil || !c.enabled || ev.Question == "" {
		return
	}
	select {
	case c.ch <- ev:
	default:
		atomic.AddInt64(&c.drops, 1)
	}
}

// Drops 丢弃计数（观测用）
func (c *Collector) Drops() int64 { return atomic.LoadInt64(&c.drops) }

func (c *Collector) consumeLoop() {
	defer c.wg.Done()
	for ev := range c.ch {
		c.processOne(ev)
	}
}

// processOne 单事件处理：upsert 沉淀 + 归簇。失败仅记日志（不重试——下一同问句事件会再来）。
func (c *Collector) processOne(ev Event) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	hash := QuestionHash(ev.Question)
	now := time.Now()
	// upsert：命中指纹则频次+1、刷新 last_seen；否则新建 draft。
	row := model.UnknownQuestion{
		TenantID:     0,
		QuestionHash: hash,
		Question:     ev.Question,
		Status:       model.UQStatusDraft,
		Source:       ev.Source,
		SourceID:     ev.SourceID,
		SessionID:    ev.SessionID,
		CustomerID:   ev.CustomerID,
		AgentID:      ev.AgentID,
		IntentType:   ev.IntentType,
		LowConfScore: ev.LowConfScore,
		Frequency:    1,
		FirstSeenAt:  now,
		LastSeenAt:   now,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if err := c.repo.UpsertUnknownQuestion(ctx, &row); err != nil {
		logger.Errorf("[reflux] upsert unknown_question: %v (q=%q)", err, ev.Question)
		return
	}

	// 归簇（trgm 相似；失败不阻塞沉淀）
	if c.clusterer != nil {
		c.mu.Lock()
		cl := c.clusterer
		c.mu.Unlock()
		if err := cl.Assign(ctx, hash, ev.Question); err != nil {
			logger.Warnf("[reflux] cluster assign: %v", err)
		}
	}
}

// RefluxEmitter 回流收集器最小接口（消费方依赖此接口而非 *Collector，
// 便于测试注入与 nil 关闭语义）
type RefluxEmitter interface {
	Emit(ev Event)
}
