// capture_test.go 承接服务：链接创建、公开问答配额、留资与首响视图。
package service

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/geo/dto"
	geomodel "hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	mainmodel "hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

const captureTenant = uint(403)

func captureSetupDB(t *testing.T) *gorm.DB {
	t.Helper()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() {
		tenant.ResetRegistryForTest()
		tenant.SetMode(tenant.ModeOff)
	})
	db := testutil.NewTestDB(t, &geomodel.GeoCaptureLink{}, &mainmodel.Clue{}, &mainmodel.CustomerSession{})
	if db == nil {
		t.Fatal("测试库不可达：承接服务契约判不了")
	}
	return db
}

type fakeKB struct{ calls int }

func (f *fakeKB) AskPublic(_ context.Context, q string) (*dto.KBAskResponse, error) {
	f.calls++
	return &dto.KBAskResponse{Answer: "答：" + q, Sources: []string{"知识库"}}, nil
}

type fakeShortLinks struct {
	created int
	deleted int
	failOn  string
}

func (f *fakeShortLinks) CreateCaptureShortLink(_ context.Context, code, _, _ string) (*ShortLinkRef, error) {
	if f.failOn == code {
		return nil, errors.New("短码冲突")
	}
	f.created++
	return &ShortLinkRef{ID: uint(f.created), Code: code, URL: "https://go.example.test/api/s/" + code}, nil
}

func (f *fakeShortLinks) DeleteShortLink(_ context.Context, _ uint) error {
	f.deleted++
	return nil
}

func newCaptureSvc(t *testing.T, linkRepo repository.GeoCaptureLinkRepository, kb KnowledgeAskPort, lead LeadCapturePort, shorts ShortLinkPort) *CaptureService {
	t.Helper()
	svc := NewCaptureService(linkRepo, kb, lead, shorts, "https://go.example.test")
	svc.now = func() time.Time { return time.Date(2026, 11, 5, 10, 0, 0, 0, time.UTC) }
	return svc
}

func TestCaptureService_Create(t *testing.T) {
	db := captureSetupDB(t)
	ctx := tenant.WithTenantID(context.Background(), captureTenant)
	repo := repository.NewGeoCaptureLinkRepositoryWithDB(db)
	shorts := &fakeShortLinks{}
	svc := newCaptureSvc(t, repo, &fakeKB{}, nil, shorts)

	view, err := svc.Create(ctx, CaptureLinkInput{
		TargetURL: "https://example.com/geo",
		Keyword:   "geo 优化",
		ArticleID: "art-7",
	}, "tester")
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}
	if len(view.Code) != 10 || view.LandingPath != "/api/public/geo/l/"+view.Code {
		t.Fatalf("承接码/落地路径不对: %+v", view)
	}
	if view.LandingURL != "https://go.example.test/api/public/geo/l/"+view.Code {
		t.Fatalf("落地 URL 不对: %s", view.LandingURL)
	}
	if view.ShortLinkID == 0 || !strings.Contains(view.ShortURL, "/api/s/g") {
		t.Fatalf("短链未创建: %+v", view)
	}
	if view.Title != "geo 优化" || view.CreatedBy != "tester" {
		t.Fatalf("默认标题/创建人不对: %+v", view)
	}
	if view.SourceID != "geo_link:"+view.Code {
		t.Fatalf("无归因链时 source_id 不对: %s", view.SourceID)
	}
	if _, err := svc.Create(ctx, CaptureLinkInput{TargetURL: "ftp://example.com"}, ""); err == nil {
		t.Fatal("非 http(s) 目标应被拒绝")
	}
	if _, err := svc.Create(ctx, CaptureLinkInput{TargetURL: "https://example.com", Code: "bad code!"}, ""); err == nil {
		t.Fatal("非法承接码应被拒绝")
	}
}

func TestCaptureService_Create_RevivesDeletedCode(t *testing.T) {
	db := captureSetupDB(t)
	ctx := tenant.WithTenantID(context.Background(), captureTenant)
	repo := repository.NewGeoCaptureLinkRepositoryWithDB(db)
	shorts := &fakeShortLinks{}
	svc := newCaptureSvc(t, repo, &fakeKB{}, nil, shorts)

	first, err := svc.Create(ctx, CaptureLinkInput{TargetURL: "https://example.com/one", Code: "reusecode"}, "tester")
	if err != nil {
		t.Fatalf("首次创建失败: %v", err)
	}
	if err := repo.Delete(ctx, first.ID); err != nil {
		t.Fatalf("删除失败: %v", err)
	}
	second, err := svc.Create(ctx, CaptureLinkInput{TargetURL: "https://example.com/two", Code: "reusecode"}, "tester2")
	if err != nil {
		t.Fatalf("删除后同码重建失败: %v", err)
	}
	if second.ID != first.ID {
		t.Fatalf("应复活原行，id %d → %d", first.ID, second.ID)
	}
	if second.TargetURL != "https://example.com/two" || second.CreatedBy != "tester2" {
		t.Fatalf("复活后字段未覆盖: %+v", second)
	}
	if second.Clicks != 0 || second.Leads != 0 {
		t.Fatalf("复活后计数未清零: %+v", second)
	}
}

func TestCaptureService_AskQuotaAndLeadRollback(t *testing.T) {
	db := captureSetupDB(t)
	ctx := tenant.WithTenantID(context.Background(), captureTenant)
	repo := repository.NewGeoCaptureLinkRepositoryWithDB(db)
	kb := &fakeKB{}
	var leadFail bool
	lead := CaptureLeadFunc(func(_ context.Context, contact, _, _, _ string) (string, error) {
		if leadFail {
			return "", errors.New("clue down")
		}
		return "clue-" + contact, nil
	})
	svc := newCaptureSvc(t, repo, kb, lead, nil)

	view, err := svc.Create(ctx, CaptureLinkInput{TargetURL: "https://example.com", Keyword: "geo"}, "")
	if err != nil {
		t.Fatalf("创建失败: %v", err)
	}

	for i := 0; i < CaptureAskDailyLimit; i++ {
		if _, err := svc.Ask(ctx, view.Code, "这个适合我吗"); err != nil {
			t.Fatalf("第 %d 次问答失败: %v", i+1, err)
		}
	}
	if _, err := svc.Ask(ctx, view.Code, "再问一次"); err == nil {
		t.Fatal("超过每日问答配额仍放行")
	}
	if _, err := svc.Ask(ctx, view.Code, "短"); err == nil {
		t.Fatal("过短问题应被拒绝")
	}

	clueID, err := svc.SubmitLead(ctx, view.Code, "13800000000", "phone", "想了解报价")
	if err != nil || clueID == "" {
		t.Fatalf("留资失败: clue=%s err=%v", clueID, err)
	}
	leadFail = true
	if _, err := svc.SubmitLead(ctx, view.Code, "13900000000", "phone", ""); err == nil {
		t.Fatal("线索落库失败应上抛")
	}
	leadFail = false
	// 失败的那次已回滚，配额仍可用。
	if _, err := svc.SubmitLead(ctx, view.Code, "13700000000", "phone", ""); err != nil {
		t.Fatalf("失败后配额未回滚: %v", err)
	}

	detail, err := svc.Get(ctx, view.ID)
	if err != nil {
		t.Fatalf("详情失败: %v", err)
	}
	if detail.Leads != 2 {
		t.Fatalf("线索计数应只含成功 2 条，实际 %d", detail.Leads)
	}
	if detail.Connected {
		t.Fatal("没有会话时不应显示已接入")
	}
}

func TestBusinessDayUsesChinaCalendar(t *testing.T) {
	// UTC 11-04 17:30 = 北京 11-05 01:30，配额应算 11-05。
	got := businessDay(time.Date(2026, 11, 4, 17, 30, 0, 0, time.UTC))
	if got.Format("2006-01-02") != "2026-11-05" {
		t.Fatalf("businessDay=%s，期望按中国日历算 2026-11-05", got.Format("2006-01-02"))
	}
	if got.Location() != time.UTC || got.Hour() != 0 {
		t.Fatalf("date 值应以 UTC 零点承载，实际 %s", got)
	}
}
