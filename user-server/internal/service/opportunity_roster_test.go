// opportunity_roster_test.go T-P4-05 AC③ 的**生产实现侧**：在册销售名单从哪来。
//
// 为什么这一层值得单独测：分配器（opportunity_assign_test.go）只认 SalesRoster 接口，
// 它无从知道"名单为空"到底是真没人还是查错了表。而那恰恰是本适配器唯一会犯的错——
// sales_events 里有十种 event_type，传错一个常量，接口返回 (nil, nil)，
// 分配器就会认认真真地给每一张新商机写下"在册销售为空，本单未分配归属"。
// 那条理由读起来像个事实，所以这个错可以存活很久。
//
// 另一条判据来自同名先例的反面：SalesEventStatsService.allProfiles 把 repo 错误吞成 nil，
// 看板少几行没人出事；本适配器的错误如果也被吞掉，直接后果是**分配决策**建在"没人"上，
// 所以错误必须原样上抛，用例专门钉这一条。
package service

import (
	"context"
	"errors"
	"testing"

	"hivemtk-user/internal/model"
)

// rosterRepo 记录调用参数的销售事件仓库替身。
// Create 不存在于本路径，签名照接口补齐。
type rosterRepo struct {
	events  []*model.SalesEvent
	err     error
	calls   int
	gotType string
	gotOwn  string
	gotSinc int64
}

func (r *rosterRepo) Create(ctx context.Context, ev *model.SalesEvent) error { return nil }

func (r *rosterRepo) ListByType(ctx context.Context, eventType, ownerID string, sinceUnix int64) ([]*model.SalesEvent, error) {
	r.calls++
	r.gotType = eventType
	r.gotOwn = ownerID
	r.gotSinc = sinceUnix
	return r.events, r.err
}

func profileEvent(ownerID string) *model.SalesEvent {
	return &model.SalesEvent{EventType: model.SalesEventTypeSalesProfile, OwnerID: ownerID}
}

// TestRosterQueriesAllProfileEvents 钉住三个入参：查档案事件、不限销售、不限时间。
func TestRosterQueriesAllProfileEvents(t *testing.T) {
	repo := &rosterRepo{events: []*model.SalesEvent{profileEvent("alpha")}}
	ids, err := NewSalesEventRoster(repo).ActiveSalesIDs(context.Background())
	if err != nil {
		t.Fatalf("读名单失败: %v", err)
	}
	if repo.calls != 1 {
		t.Fatalf("ListByType 调用 %d 次，期望 1 次", repo.calls)
	}
	if repo.gotType != model.SalesEventTypeSalesProfile {
		t.Errorf("查询事件类型 %q：传错常量会读到空名单，而空名单会被当成「确实没有销售」而不是故障", repo.gotType)
	}
	if repo.gotOwn != "" {
		t.Errorf("名单查询带了销售过滤条件 %q：在册名单必须是全量的，按人过滤是负载侧的事", repo.gotOwn)
	}
	if repo.gotSinc != 0 {
		t.Errorf("名单查询带了起始时间 %d：加时间窗会把「今年没注册过档案的老销售」当成不在册", repo.gotSinc)
	}
	if len(ids) != 1 || ids[0] != "alpha" {
		t.Errorf("名单 %v，期望 [alpha]", ids)
	}
}

func TestRosterMapsOwnerIDsInReadOrder(t *testing.T) {
	repo := &rosterRepo{events: []*model.SalesEvent{
		profileEvent("beta"), profileEvent("alpha"), profileEvent("beta"),
	}}
	ids, err := NewSalesEventRoster(repo).ActiveSalesIDs(context.Background())
	if err != nil {
		t.Fatalf("读名单失败: %v", err)
	}
	// 去重与排序是分配器（sanitizeSalesIDs）的活，本层不重复做一遍：
	// 两处各排一次会在其中一处改了判据时看不出来，而"谁该被洗"这件事只能有一个答案。
	if len(ids) != 3 || ids[0] != "beta" || ids[1] != "alpha" || ids[2] != "beta" {
		t.Errorf("名单 %v，期望原样返回 [beta alpha beta]", ids)
	}
}

// TestRosterPropagatesRepoError 与 allProfiles 的吞错刻意相反，理由见文件头。
func TestRosterPropagatesRepoError(t *testing.T) {
	want := errors.New("boom")
	repo := &rosterRepo{err: want, events: []*model.SalesEvent{profileEvent("alpha")}}
	ids, err := NewSalesEventRoster(repo).ActiveSalesIDs(context.Background())
	if !errors.Is(err, want) {
		t.Fatalf("仓库错误被吞/改写: %v", err)
	}
	if ids != nil {
		// 出错时还交出半截名单，是最难查的一种坏法：分配器会拿它去比负载。
		t.Errorf("读失败却返回了名单 %v：故障路径必须交出空，让上层只能走「上抛」这一支", ids)
	}
}

func TestRosterEmptyRosterIsNotAnError(t *testing.T) {
	repo := &rosterRepo{events: []*model.SalesEvent{}}
	ids, err := NewSalesEventRoster(repo).ActiveSalesIDs(context.Background())
	if err != nil {
		t.Fatalf("「确实没有销售」不该报故障: %v", err)
	}
	if len(ids) != 0 {
		t.Errorf("名单 %v，期望空", ids)
	}
}

// TestRosterNilRepoIsError 未装配必须落在故障支，不能落在空名单支。
func TestRosterNilRepoIsError(t *testing.T) {
	ids, err := NewSalesEventRoster(nil).ActiveSalesIDs(context.Background())
	if err == nil {
		t.Error("未装配的名单适配器返回了成功：装配漏项会被读成「这个商户没有销售」")
	}
	if ids != nil {
		t.Errorf("未装配却给出名单 %v", ids)
	}
}
