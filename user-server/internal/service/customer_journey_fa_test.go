package service

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
)

// faJourneyCache 共享假缓存：覆盖 GetJSON/SetJSON/Exists，其余嵌入接口委托
// （测试路径不触及 list/delete 等方法，nil 嵌入接口不被调用）。
type faJourneyCache struct {
	cache.Cache
	mu         sync.Mutex
	data       map[string][]byte
	readFault  bool
	writeFault bool
}

func newFAJourneyCache() *faJourneyCache {
	return &faJourneyCache{data: make(map[string][]byte)}
}

func (c *faJourneyCache) GetJSON(_ context.Context, key string, dest any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.readFault {
		return ErrJourneyCacheReadFault
	}
	raw, ok := c.data[key]
	if !ok {
		return errors.New("not found")
	}
	return json.Unmarshal(raw, dest)
}

func (c *faJourneyCache) SetJSON(_ context.Context, key string, value any, _ time.Duration) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.writeFault {
		return context.DeadlineExceeded
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return err
	}
	c.data[key] = raw
	return nil
}

func (c *faJourneyCache) Exists(_ context.Context, key string) (bool, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	_, ok := c.data[key]
	return ok, nil
}

// RPush 阶段索引写入（indexStageMember 调用）——测试不校验索引内容，空实现即可。
func (c *faJourneyCache) RPush(_ context.Context, _ string, _ any, _ time.Duration) error {
	return nil
}

// Set / LRange 阶段索引辅助（indexStageMember / ListByStage 调用），测试不校验，空实现。
func (c *faJourneyCache) Set(_ context.Context, _ string, _ any, _ time.Duration) error { return nil }
func (c *faJourneyCache) LRange(_ context.Context, _ string, _, _ int64) ([]string, error) {
	return []string{}, nil
}

func (c *faJourneyCache) setReadFault(v bool)  { c.mu.Lock(); c.readFault = v; c.mu.Unlock() }
func (c *faJourneyCache) setWriteFault(v bool) { c.mu.Lock(); c.writeFault = v; c.mu.Unlock() }

// faJourneySubscriber 记录订阅事件数
type faJourneySubscriber struct {
	mu    sync.Mutex
	count int
}

func (s *faJourneySubscriber) OnJourneyEvent(_ context.Context, _ string, _ *JourneyEvent) {
	s.mu.Lock()
	s.count++
	s.mu.Unlock()
}
func (s *faJourneySubscriber) events() int { s.mu.Lock(); defer s.mu.Unlock(); return s.count }

func faNewSvc(c *faJourneyCache) *CustomerJourneyService {
	return NewCustomerJourneyServiceWithCache(c)
}

// ① 读故障时 Touch/Transition 不初始化（历史不丢，不写默认态）
func TestFAJourney_ReadFaultDoesNotInitialize(t *testing.T) {
	ctx := context.Background()
	c := newFAJourneyCache()
	// 预置一条已有历史
	seed := &JourneyState{CustomerID: "cust-1", CurrentStage: StageQuoted, StageHistory: []JourneyEvent{{Type: "seed"}}, Version: 3}
	raw, _ := json.Marshal(seed)
	c.data[journeyStateKey("cust-1")] = raw

	c.setReadFault(true)
	svc := faNewSvc(c)

	// Transition 遇读故障应返回错误、不初始化、不写空历史
	_, err := svc.Transition(ctx, "cust-1", StageWon, "test", "op", "", nil)
	if err != ErrJourneyCacheReadFault {
		t.Fatalf("读故障应返回 ErrJourneyCacheReadFault, got %v", err)
	}
	// 缓存里的历史未被覆盖
	c.mu.Lock()
	got := c.data[journeyStateKey("cust-1")]
	c.mu.Unlock()
	if string(got) != string(raw) {
		t.Fatalf("读故障不应覆盖已有历史")
	}
}

// ② 写失败时不 putL1、不发布虚假阶段事件
func TestFAJourney_WriteFailureNoEvent(t *testing.T) {
	ctx := context.Background()
	c := newFAJourneyCache()
	svc := faNewSvc(c)
	sub := &faJourneySubscriber{}
	svc.AddSubscriber(ctx, sub)

	// 先正常初始化一条
	svc.Transition(ctx, "cust-2", StageLead, "test", "op", "", nil)
	// 等待初始化阶段的异步订阅者事件送达（SafeGo 异步），否则 before 可能取到 0，
	// 延迟到达的初始化事件会让 got=1 触发假阳性。
	deadline := time.Now().Add(500 * time.Millisecond)
	for sub.events() == 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	before := sub.events()

	c.setWriteFault(true)
	_, err := svc.Transition(ctx, "cust-2", StageContact, "test", "op", "", nil)
	if err == nil {
		t.Fatalf("写失败应返回错误")
	}
	// 订阅者不应收到虚假事件
	if got := sub.events(); got != before {
		t.Fatalf("写失败不应发布阶段事件, before=%d got=%d", before, got)
	}
	// L1 不应被推进到新阶段
	st, ok := svc.getLiveL1(journeyScope(ctx), "cust-2")
	if ok && st.CurrentStage == StageContact {
		t.Fatalf("写失败不应更新 L1 到新阶段")
	}
}

// ③ CAS 冲突拒覆盖
func TestFAJourney_CASConflictRejects(t *testing.T) {
	ctx := context.Background()
	c := newFAJourneyCache()
	svcA := faNewSvc(c)
	svcB := faNewSvc(c)

	// A 建初始态
	svcA.Transition(ctx, "cust-3", StageLead, "test", "op", "", nil)

	// B 读到旧版本（v1），A 先推进到 v2
	base, found, err := svcB.loadAuthoritativeE(ctx, "cust-3")
	if err != nil || !found {
		t.Fatalf("B 读初始态失败: %v %v", err, found)
	}
	svcA.Transition(ctx, "cust-3", StageContact, "test", "op", "", nil) // A → v2

	// B 用旧版本做 CAS 应冲突
	base.CurrentStage = StageQuoted
	base.Version++                                        // 模拟本地推进
	err = svcB.persistStateCAS(ctx, base, base.Version-1) // expected = 旧版本
	if err != ErrJourneyVersionConflict {
		t.Fatalf("CAS 冲突应返回 ErrJourneyVersionConflict, got %v", err)
	}
}

func TestFAJourney_CASReadFaultDoesNotWrite(t *testing.T) {
	ctx := context.Background()
	c := newFAJourneyCache()
	c.setReadFault(true)
	svc := faNewSvc(c)

	err := svc.persistStateCAS(ctx, &JourneyState{
		CustomerID: "cust-read-fault", CurrentStage: StageLead, Version: 1,
	}, 0)
	if err != ErrJourneyCacheReadFault {
		t.Fatalf("CAS 读故障应返回 ErrJourneyCacheReadFault, got %v", err)
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.data[journeyStateKey("cust-read-fault")]; exists {
		t.Fatal("CAS 读故障不应写入新状态")
	}
}

// ④ 新客户默认初始化
func TestFAJourney_NewCustomerDefaults(t *testing.T) {
	ctx := context.Background()
	c := newFAJourneyCache()
	svc := faNewSvc(c)
	st := svc.GetState(ctx, "brand-new")
	if st == nil || st.CustomerID != "brand-new" || st.CurrentStage != StageStranger {
		t.Fatalf("新客户应默认初始化为 StageStranger, got %+v", st)
	}
}

// ⑤ 重启（新 service 实例共享 cache）状态历史不丢
func TestFAJourney_RestartKeepsHistory(t *testing.T) {
	ctx := context.Background()
	c := newFAJourneyCache()
	svc1 := faNewSvc(c)
	svc1.Transition(ctx, "cust-5", StageLead, "test", "op", "", nil)
	svc1.Transition(ctx, "cust-5", StageContact, "test", "op", "", nil)

	// 模拟重启：全新 service 实例共享同一缓存
	svc2 := faNewSvc(c)
	st := svc2.GetState(ctx, "cust-5")
	if st == nil || st.CurrentStage != StageContact {
		t.Fatalf("重启后阶段应保留 StageContact, got %+v", st)
	}
	if len(st.StageHistory) < 2 {
		t.Fatalf("重启后历史不应丢失, got %d events", len(st.StageHistory))
	}
}
