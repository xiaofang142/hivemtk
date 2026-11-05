// push_record_test.go 推送编排的记账契约。
//
// 推送结果必须逐条落 geo_push_records（同一批共用一个 batch_id），
// 记账失败必须上抛——旧实现 `_ = tx.Create(...)` 把落库错误吞掉，事后无账可查。
package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/timeutil"
)

type stubPusher struct {
	name    string
	results []PushResult
	err     error
}

func (p *stubPusher) Name() string { return p.name }

func (p *stubPusher) Push(_ context.Context, urls []string) ([]PushResult, error) {
	if p.err != nil {
		return nil, p.err
	}
	out := make([]PushResult, len(urls))
	for i, u := range urls {
		if i < len(p.results) {
			out[i] = p.results[i]
		} else {
			out[i].Success = true
		}
		out[i].URL = u
	}
	return out, nil
}

type failingRecordRepo struct{ err error }

func (r *failingRecordRepo) BatchCreate(_ context.Context, _ []*model.GeoPushRecord) error {
	return r.err
}

func (r *failingRecordRepo) GoogleStats(_ context.Context, _ time.Time) (int64, int64, error) {
	return 0, 0, r.err
}

func newPushSvcForTest(t *testing.T, records repository.GeoPushRecordRepository, pushers map[string]Pusher) *PushService {
	t.Helper()
	db := testutil.NewTestDB(t, &model.GeoPushRecord{}, &model.GeoPusherConfig{})
	if db == nil {
		t.Fatal("测试库不可达：记账契约判不了（不 Skip，跳过等于没锁）")
	}
	return &PushService{
		configs:  repository.NewGeoPusherConfigRepositoryWithDB(db),
		records:  records,
		articles: repository.NewGeoArticleRepositoryWithDB(db),
		quota:    NewQuotaManager(),
		pushers:  pushers,
		quotaDay: timeutil.BusinessToday(),
	}
}

func TestPushURLs_逐条记账且同批共用BatchID(t *testing.T) {
	db := testutil.NewTestDB(t, &model.GeoPushRecord{}, &model.GeoPusherConfig{})
	if db == nil {
		t.Fatal("测试库不可达")
	}
	svc := &PushService{
		configs:  repository.NewGeoPusherConfigRepositoryWithDB(db),
		records:  repository.NewGeoPushRecordRepositoryWithDB(db),
		articles: repository.NewGeoArticleRepositoryWithDB(db),
		quota:    NewQuotaManager(),
		pushers: map[string]Pusher{"baidu": &stubPusher{
			name:    "baidu",
			results: []PushResult{{Success: true, RemainQuota: 99}, {Success: false, Error: "拒收"}},
		}},
		quotaDay: timeutil.BusinessToday(),
	}

	if err := svc.PushURLs(context.Background(), []string{"https://a.example/1", "https://a.example/2"}); err != nil {
		t.Fatalf("PushURLs: %v", err)
	}

	var rows []model.GeoPushRecord
	if err := db.Order("url ASC").Find(&rows).Error; err != nil {
		t.Fatalf("回查推送记录失败: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("推送记录条数 = %d，期望 2", len(rows))
	}
	if rows[0].BatchID == "" || rows[0].BatchID != rows[1].BatchID {
		t.Fatalf("同一次推送应共用 batch_id，实际 %q / %q", rows[0].BatchID, rows[1].BatchID)
	}
	if rows[0].SuccessCount != 1 || rows[0].RemainQuota != 99 {
		t.Fatalf("成功行记账不符: %+v", rows[0])
	}
	if rows[1].FailCount != 1 || rows[1].ErrorMsg != "拒收" {
		t.Fatalf("失败行记账不符: %+v", rows[1])
	}

	// 配额按成功数消耗（失败行不占额度）。
	if used := svc.quota.Used("baidu"); used != 1 {
		t.Fatalf("已用配额 = %d，期望 1", used)
	}
}

func TestPushURLs_记账失败必须上抛(t *testing.T) {
	svc := newPushSvcForTest(t, &failingRecordRepo{err: errors.New("db down")},
		map[string]Pusher{"baidu": &stubPusher{name: "baidu"}})

	err := svc.PushURLs(context.Background(), []string{"https://a.example/1"})
	if err == nil {
		t.Fatal("记账失败应返回错误，实际成功（静默丢审计）")
	}
	if !strings.Contains(err.Error(), "推送记录落库失败") {
		t.Fatalf("错误信息应指明落库失败，实际: %v", err)
	}
}

func TestPushURLs_引擎整体失败不记账(t *testing.T) {
	svc := newPushSvcForTest(t, &failingRecordRepo{err: errors.New("不应被调用")},
		map[string]Pusher{"baidu": &stubPusher{name: "baidu", err: errors.New("网络不通")}})

	if err := svc.PushURLs(context.Background(), []string{"https://a.example/1"}); err == nil {
		t.Fatal("所有引擎整体失败时不应返回成功")
	}
}

func TestValidatePlatforms_未启用平台必须报错(t *testing.T) {
	svc := &PushService{pushers: map[string]Pusher{"baidu": &stubPusher{name: "baidu"}}}

	if err := svc.ValidatePlatforms(context.Background(), []string{"baidu"}); err != nil {
		t.Fatalf("已启用平台不应报错: %v", err)
	}
	err := svc.ValidatePlatforms(context.Background(), []string{"baidu", "google"})
	if err == nil || !strings.Contains(err.Error(), "google") {
		t.Fatalf("未启用平台应报错并点名，实际: %v", err)
	}

	// 手动推送同样在落库前拒绝未知平台。
	if _, err := svc.PushManual(context.Background(), []string{"https://a.example/1"}, []string{"nope"}); err == nil {
		t.Fatal("PushManual 收到未启用平台应报错")
	}
}
