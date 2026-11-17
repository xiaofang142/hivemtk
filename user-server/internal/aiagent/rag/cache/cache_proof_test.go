package ragcache

import (
	"context"
	"errors"
	"math"
	"testing"
	"time"
)

func TestCacheProof_UnconfirmedKnowledgeCannotServeAnswer(t *testing.T) {
	for _, tc := range []struct {
		name string
		meta KBMetaReader
	}{
		{name: "缺少知识读取器", meta: nil},
		{name: "读取故障", meta: &fakeKBMeta{err: errors.New("测试故障")}},
		{name: "知识版本回退", meta: &fakeKBMeta{updatedAt: base.Add(-time.Hour)}},
		{name: "知识版本未知", meta: &fakeKBMeta{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			vec := []float32{1, 0}
			store := &fakeStore{exact: map[string]*Entry{
				testKB + "|" + testPV + "|" + vecKey(vec): {
					ID: 1, KBID: testKB, PromptVersion: testPV, QueryVector: vec,
					Answer: testAns, CreatedAt: base, KBUpdatedAt: base,
				},
			}}
			result, err := newTestService(store, tc.meta).Lookup(context.Background(), LookupRequest{
				KBID: testKB, PromptVersion: testPV, QueryVector: vec,
			})
			if err != nil || result == nil || result.Tier != TierMiss || result.Answer != "" {
				t.Fatalf("知识版本未被确认时必须回源：结果=%+v，错误=%v", result, err)
			}
			if tc.name == "读取故障" && len(store.deleted) != 0 {
				t.Fatal("暂时读取故障不得删除有效缓存")
			}
		})
	}
}

func TestCacheProof_InvalidVectorCannotBecomeExactConfidence(t *testing.T) {
	for _, vec := range [][]float32{{0, 0}, {float32(math.NaN()), 1}, {float32(math.Inf(1)), 1}} {
		store := &fakeStore{exact: map[string]*Entry{
			testKB + "|" + testPV + "|" + vecKey(vec): {
				ID: 1, KBID: testKB, PromptVersion: testPV, QueryVector: vec,
				Answer: testAns, CreatedAt: base, KBUpdatedAt: base,
			},
		}}
		result, err := newTestService(store, &fakeKBMeta{updatedAt: base}).Lookup(context.Background(), LookupRequest{
			KBID: testKB, PromptVersion: testPV, QueryVector: vec,
		})
		if err != nil || result == nil || result.Tier != TierMiss {
			t.Fatalf("非法向量不能因精确键相同而成为满置信缓存：%+v，%v", result, err)
		}
	}
}

func TestCacheProof_GenerationVersionCannotBeRelabeled(t *testing.T) {
	for _, tc := range []struct {
		name       string
		current    time.Time
		wantStored bool
	}{
		{name: "版本未变化正常缓存", current: base, wantStored: true},
		{name: "生成期间知识更新", current: base.Add(time.Second)},
		{name: "生成期间知识回退", current: base.Add(-time.Second)},
		{name: "知识时间未知", current: time.Time{}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &fakeStore{}
			sourceVersion := base
			service := newTestService(store, &fakeKBMeta{updatedAt: tc.current})
			err := service.Store(context.Background(), StoreRequest{
				KBID: testKB, PromptVersion: testPV, QueryVector: []float32{1, 0},
				Answer: testAns, FromKnowledgeBase: true, SourceKBUpdatedAt: &sourceVersion,
			})
			if err != nil {
				t.Fatalf("放弃缓存不应使回答失败：%v", err)
			}
			if (len(store.semantic) == 1) != tc.wantStored {
				t.Fatalf("旧答案不得被赋予新知识版本：实际写入=%d", len(store.semantic))
			}
		})
	}
}

type proofUnscopedStore struct {
	Store
	entry *Entry
}

func (s *proofUnscopedStore) GetExact(context.Context, string, string, []float32) (*Entry, error) {
	return s.entry, nil
}

func (s *proofUnscopedStore) GetSemantic(context.Context, string, string, []float32, float64) (*Entry, error) {
	return s.entry, nil
}

func TestCacheProof_ReturnedEntryMustMatchRequestedScopeAndFacts(t *testing.T) {
	for _, tc := range []struct {
		name string
		edit func(*Entry)
	}{
		{name: "其他知识库", edit: func(e *Entry) { e.KBID = "other-kb" }},
		{name: "其他配置版本", edit: func(e *Entry) { e.PromptVersion = "other-prompt" }},
		{name: "失真缓存向量", edit: func(e *Entry) { e.QueryVector = []float32{float32(math.NaN()), 0} }},
		{name: "不同精确向量", edit: func(e *Entry) { e.QueryVector = []float32{0, 1} }},
		{name: "拒答哨兵", edit: func(e *Entry) { e.Answer = " NO_REPLY " }},
		{name: "个性变量", edit: func(e *Entry) { e.Answer = "订单 {order_id} 已经发货" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			entry := &Entry{KBID: testKB, PromptVersion: testPV, QueryVector: []float32{1, 0}, Answer: testAns, KBUpdatedAt: base}
			tc.edit(entry)
			store := &proofUnscopedStore{Store: &fakeStore{}, entry: entry}
			result, err := newTestService(store, &fakeKBMeta{updatedAt: base}).Lookup(context.Background(), LookupRequest{
				KBID: testKB, PromptVersion: testPV, QueryVector: []float32{1, 0},
			})
			if err != nil || result == nil || result.Tier != TierMiss || result.Answer != "" {
				t.Fatalf("存储返回的不可信条目不得越过服务校验：结果=%+v，错误=%v", result, err)
			}
		})
	}
}

func TestCacheProof_NonFiniteThresholdCannotLoosenSemanticGate(t *testing.T) {
	for _, threshold := range []float64{math.NaN(), math.Inf(1), math.Inf(-1)} {
		query, cached := cosPair(0.9)
		store := &fakeStore{semantic: []*Entry{{KBID: testKB, PromptVersion: testPV,
			QueryVector: cached, Answer: testAns, KBUpdatedAt: base}}}
		service := NewFAQAnswerCacheService(store, &fakeKBMeta{updatedAt: base}, threshold, 0)
		result, err := service.Lookup(context.Background(), LookupRequest{KBID: testKB, PromptVersion: testPV, QueryVector: query})
		if err != nil || result == nil || result.Tier != TierMiss || math.IsNaN(service.threshold) || math.IsInf(service.threshold, 0) {
			t.Fatalf("非法配置不得绕过0.95语义门槛：阈值=%v，结果=%+v，错误=%v", service.threshold, result, err)
		}
	}
}
