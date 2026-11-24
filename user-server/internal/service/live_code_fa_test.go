package service

import (
	"bytes"
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

// faLiveCodeMemStore 内存 QR 存储，记录 name→data，用于断言生成了真实 PNG。
type faLiveCodeMemStore struct {
	files map[string][]byte
}

func (m *faLiveCodeMemStore) Save(name string, data []byte) (string, error) {
	if m.files == nil {
		m.files = make(map[string][]byte)
	}
	m.files[name] = append([]byte(nil), data...)
	return "mem://" + name, nil
}

func faLiveCodeDB(t *testing.T) *gorm.DB {
	database := testutil.NewTestDB(t,
		&model.LiveCode{}, &model.LiveCodeQR{}, &model.LiveCodeQRStat{},
		&model.LiveCodeClickLog{}, &model.DomainPool{},
	)
	db.SetTestDB(database)
	return database
}

func faNewLiveCodeSvc(database *gorm.DB, store *faLiveCodeMemStore, now time.Time) *liveCodeService {
	return &liveCodeService{
		liveCodeRepo: repository.NewLiveCodeRepository(database),
		qrCodeRepo:   repository.NewLiveCodeQRRepository(database),
		qrStatRepo:   repository.NewLiveCodeQRRepository(database),
		clickLogRepo: repository.NewLiveCodeClickLogRepository(database),
		domainRepo:   repository.NewDomainPoolRepository(database),
		qrStore:      store,
		nowFn:        func() time.Time { return now },
	}
}

func faCreateLiveCode(t *testing.T, database *gorm.DB, shortLink string) *model.LiveCode {
	lc := &model.LiveCode{
		Name:            "fa-live-" + shortLink,
		ShortLink:       shortLink,
		ShortDomainID:   0,
		EntryDomainID:   0,
		LandingDomainID: 0,
		Status:          1,
	}
	if err := database.Create(lc).Error; err != nil {
		t.Fatalf("Create LiveCode: %v", err)
	}
	return lc
}

// FA-LIVECODE-01：GenerateQRCode 生成真实 PNG 并存储，DB 记录的目标正确。
func TestFALiveCode_GenerateQRCodeProducesRealArtifact(t *testing.T) {
	database := faLiveCodeDB(t)
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	store := &faLiveCodeMemStore{}
	svc := faNewLiveCodeSvc(database, store, now)
	lc := faCreateLiveCode(t, database, "fa-gen-001")

	resp, err := svc.GenerateQRCode(context.Background(), lc.ID, &dto.GenerateQRCodeRequest{ExpireDays: 30, Status: 1})
	if err != nil {
		t.Fatalf("GenerateQRCode: %v", err)
	}
	if resp == nil || resp.ImageURL == "" {
		t.Fatalf("应返回 ImageURL, got %+v", resp)
	}
	// memStore 中确实存了真实 PNG（PNG 魔数 89 50 4E 47）。
	var found []byte
	for _, data := range store.files {
		if len(data) >= 4 && bytes.Equal(data[:4], []byte{0x89, 0x50, 0x4E, 0x47}) {
			found = data
			break
		}
	}
	if len(found) == 0 {
		t.Fatalf("未找到真实 PNG 制品, files=%d", len(store.files))
	}
	// DB 中 QRContent 为活码目标。
	var qrs []*model.LiveCodeQR
	if err := database.Where("live_code_id = ?", lc.ID).Find(&qrs).Error; err != nil {
		t.Fatalf("查询 QR: %v", err)
	}
	if len(qrs) != 1 {
		t.Fatalf("应有 1 条 QR, got %d", len(qrs))
	}
	want := liveCodeFullEntryLink(lc)
	if want == "" {
		want = "livecode://" + lc.ID
	}
	if qrs[0].QRContent != want {
		t.Fatalf("QRContent=%q want %q", qrs[0].QRContent, want)
	}
}

// FA-LIVECODE-02：到期/达限真实判定。
func TestFALiveCode_ExpiryAndDailyLimitReal(t *testing.T) {
	database := faLiveCodeDB(t)
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	svc := faNewLiveCodeSvc(database, &faLiveCodeMemStore{}, now)
	lc := faCreateLiveCode(t, database, "fa-exp-001")

	// 昨日创建 + ExpireDays=1 → 已过期。
	yesterday := now.AddDate(0, 0, -1)
	qr := &model.LiveCodeQR{
		ID:          "fa-exp-qr-1",
		LiveCodeID:  lc.ID,
		QRType:      "entry",
		QRContent:   liveCodeFullEntryLink(lc),
		ImageURL:    "mem://fa-exp-qr-1",
		DailyLimit:  200,
		ExpireDays:  1,
		Status:      1,
		CreatedAt:   yesterday,
	}
	if err := database.Create(qr).Error; err != nil {
		t.Fatalf("Create QR: %v", err)
	}
	stats, err := svc.GetQRStats(context.Background(), qr.ID)
	if err != nil {
		t.Fatalf("GetQRStats: %v", err)
	}
	if !stats.IsExpired {
		t.Fatalf("昨日创建 ExpireDays=1 应 IsExpired=true, got false (expire=%v now=%v)", stats.ExpireTime, now)
	}
	if stats.ExpireTime.After(now) {
		t.Fatalf("ExpireTime 应为 CreatedAt+ExpireDays=%v, got %v", yesterday.AddDate(0,0,1), stats.ExpireTime)
	}
}

// FA-LIVECODE-02：Share 不选择已过期 QR。
func TestFALiveCode_ShareSkipsExpiredQR(t *testing.T) {
	database := faLiveCodeDB(t)
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	svc := faNewLiveCodeSvc(database, &faLiveCodeMemStore{}, now)
	lc := faCreateLiveCode(t, database, "fa-share-001")

	qr := &model.LiveCodeQR{
		ID:         "fa-share-qr-1",
		LiveCodeID: lc.ID,
		QRType:     "entry",
		QRContent:  liveCodeFullEntryLink(lc),
		ImageURL:   "mem://fa-share-qr-1",
		DailyLimit: 200,
		ExpireDays: 1,
		Status:     1,
		CreatedAt:  now.AddDate(0, 0, -5), // 早已过期
	}
	if err := database.Create(qr).Error; err != nil {
		t.Fatalf("Create QR: %v", err)
	}
	_, err := svc.Share(context.Background(), lc.ID, &dto.ShareLiveCodeRequest{})
	if err == nil {
		t.Fatalf("仅有过期的 Status=1 QR 时 Share 应报错")
	}
}

// FA-LIVECODE-02：RotateLiveCodes 已有可用 QR 时不再追加。
func TestFALiveCode_RotateDoesNotAppendWhenUsableExists(t *testing.T) {
	database := faLiveCodeDB(t)
	now := time.Date(2026, 11, 23, 10, 0, 0, 0, time.UTC)
	store := &faLiveCodeMemStore{}
	svc := faNewLiveCodeSvc(database, store, now)
	lc := faCreateLiveCode(t, database, "fa-rot-001")

	// 一个当前可用的 QR（今天创建、ExpireDays=30）。
	qr := &model.LiveCodeQR{
		ID:         "fa-rot-qr-1",
		LiveCodeID: lc.ID,
		QRType:     "entry",
		QRContent:  liveCodeFullEntryLink(lc),
		ImageURL:   "mem://fa-rot-qr-1",
		DailyLimit: 200,
		ExpireDays: 30,
		Status:     1,
		CreatedAt:  now,
	}
	if err := database.Create(qr).Error; err != nil {
		t.Fatalf("Create QR: %v", err)
	}
	if err := svc.RotateLiveCodes(context.Background()); err != nil {
		t.Fatalf("RotateLiveCodes: %v", err)
	}
	var count int64
	if err := database.Model(&model.LiveCodeQR{}).Where("live_code_id = ?", lc.ID).Count(&count).Error; err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Fatalf("已有可用 QR 时不应追加空 QR, got %d", count)
	}
}
