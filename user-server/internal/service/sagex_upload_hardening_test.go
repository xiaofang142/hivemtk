package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

type hardeningInstRepo struct {
	rows          []*model.SageXInstallation
	getErr        error
	createErr     error
	updateErr     error
	touchErr      error
	forceConflict bool
	createHook    func(*model.SageXInstallation)
	getCalls      int
	createCalls   int
	updateCalls   int
	touchCalls    int
	legacyGets    int
	contexts      []context.Context
}

func (r *hardeningInstRepo) Create(ctx context.Context, inst *model.SageXInstallation) error {
	_, err := r.CreateIfAbsent(ctx, inst)
	return err
}

func (r *hardeningInstRepo) CreateIfAbsent(ctx context.Context, inst *model.SageXInstallation) (bool, error) {
	r.createCalls++
	r.contexts = append(r.contexts, ctx)
	if r.createHook != nil {
		r.createHook(inst)
	}
	if r.createErr != nil {
		return false, r.createErr
	}
	if r.forceConflict {
		return false, nil
	}
	cp := *inst
	cp.ID = uint(len(r.rows) + 1)
	r.rows = append(r.rows, &cp)
	return true, nil
}

func (r *hardeningInstRepo) Update(_ context.Context, inst *model.SageXInstallation) error {
	r.updateCalls++
	if r.updateErr != nil {
		return r.updateErr
	}
	for i, row := range r.rows {
		if row.ID == inst.ID {
			cp := *inst
			r.rows[i] = &cp
			break
		}
	}
	return nil
}

func (r *hardeningInstRepo) GetByID(context.Context, uint) (*model.SageXInstallation, error) {
	return nil, nil
}

func (r *hardeningInstRepo) GetByToken(context.Context, string) (*model.SageXInstallation, error) {
	return nil, nil
}

func (r *hardeningInstRepo) GetByInstallationID(ctx context.Context, installationID string) (*model.SageXInstallation, error) {
	r.legacyGets++
	return r.GetByTenantAndInstallationID(ctx, 0, installationID)
}

func (r *hardeningInstRepo) GetByTenantAndInstallationID(
	ctx context.Context,
	tenantID uint,
	installationID string,
) (*model.SageXInstallation, error) {
	r.getCalls++
	r.contexts = append(r.contexts, ctx)
	if r.getErr != nil {
		return nil, r.getErr
	}
	for _, row := range r.rows {
		if row.InstallationID == installationID && (tenantID == 0 || row.TenantID == tenantID) {
			cp := *row
			return &cp, nil
		}
	}
	return nil, nil
}

func (r *hardeningInstRepo) TouchLastSeen(context.Context, uint, time.Time) error {
	r.touchCalls++
	return r.touchErr
}

type hardeningReceiptRepo struct {
	mu       sync.Mutex
	receipts map[string]*model.SageXEventReceipt
}

func (r *hardeningReceiptRepo) GetByInstallationAndEventID(_ context.Context, tenantID uint, installationID, eventID string) (*model.SageXEventReceipt, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	key := fmt.Sprintf("%d|%s|%s", tenantID, installationID, eventID)
	if row := r.receipts[key]; row != nil {
		cp := *row
		return &cp, nil
	}
	return nil, nil
}

func (r *hardeningReceiptRepo) Create(_ context.Context, receipt *model.SageXEventReceipt) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.receipts == nil {
		r.receipts = map[string]*model.SageXEventReceipt{}
	}
	key := fmt.Sprintf("%d|%s|%s", receipt.TenantID, receipt.InstallationID, receipt.EventID)
	cp := *receipt
	r.receipts[key] = &cp
	return nil
}

func (r *hardeningReceiptRepo) Update(context.Context, *model.SageXEventReceipt) error {
	return nil
}

type hardeningIngress struct {
	mu    sync.Mutex
	calls int
}

func (i *hardeningIngress) HandleIngressMessage(context.Context, *model.MessageEvent) (*InboxIngressResult, error) {
	i.mu.Lock()
	defer i.mu.Unlock()
	i.calls++
	return &InboxIngressResult{Accepted: true}, nil
}

func hardeningUploadRequest(installationID string) *SageXUploadRequest {
	return &SageXUploadRequest{
		SchemaVersion:  SageXSchemaVersion,
		BatchID:        "batch-hardening",
		InstallationID: installationID,
		Events: []SageXEventItem{{
			EventID:      "event-hardening",
			EventType:    "notification.posted",
			Source:       SageXSource{Package: "com.tencent.mobileqq"},
			Conversation: SageXConversation{Type: "private", Title: "Alice"},
			Message:      SageXMessage{Text: "hi"},
		}},
	}
}

func TestSageXUploadHardeningUsesTenantAwareAtomicPath(t *testing.T) {
	repo := &hardeningInstRepo{}
	ingress := &hardeningIngress{}
	svc := newSageXUploadTestService(repo, &hardeningReceiptRepo{}, ingress)

	if _, err := svc.HandleUpload(context.Background(), sageXUploadIdentifyCode, hardeningUploadRequest("installation-a"), 10); err != nil {
		t.Fatalf("upload failed: %v", err)
	}
	if repo.legacyGets != 0 {
		t.Fatalf("upload used legacy GetByInstallationID %d times", repo.legacyGets)
	}
	if repo.createCalls != 1 || repo.getCalls < 1 {
		t.Fatalf("upload must use tenant-aware lookup + CreateIfAbsent: get=%d create=%d", repo.getCalls, repo.createCalls)
	}
	if len(repo.rows) != 1 || repo.rows[0].TenantID != 1 {
		t.Fatalf("unexpected installation rows: %+v", repo.rows)
	}
}

func TestSageXUploadHardeningRejectsDisabledInstallation(t *testing.T) {
	repo := &hardeningInstRepo{rows: []*model.SageXInstallation{{
		ID: 1, TenantID: 1, InstallationID: "disabled-id", AccountID: "1", Token: "secret-token", Status: 0,
	}}}
	ingress := &hardeningIngress{}
	svc := newSageXUploadTestService(repo, &hardeningReceiptRepo{}, ingress)

	_, err := svc.HandleUpload(context.Background(), sageXUploadIdentifyCode, hardeningUploadRequest("disabled-id"), 10)
	if !errors.Is(err, ErrSageXInstallationDisabled) {
		t.Fatalf("disabled installation error=%v", err)
	}
	if ingress.calls != 0 || repo.touchCalls != 0 {
		t.Fatalf("disabled installation processed: ingress=%d touch=%d", ingress.calls, repo.touchCalls)
	}
}

func TestSageXUploadHardeningRejectsAccountMismatch(t *testing.T) {
	repo := &hardeningInstRepo{rows: []*model.SageXInstallation{{
		ID: 1, TenantID: 1, InstallationID: "shared-id", AccountID: "999", Token: "secret-token", Status: 1,
	}}}
	ingress := &hardeningIngress{}
	svc := newSageXUploadTestService(repo, &hardeningReceiptRepo{}, ingress)

	_, err := svc.HandleUpload(context.Background(), sageXUploadIdentifyCode, hardeningUploadRequest("shared-id"), 10)
	if !errors.Is(err, ErrSageXInstallationForbidden) {
		t.Fatalf("account mismatch error=%v", err)
	}
	if ingress.calls != 0 || repo.updateCalls != 0 || repo.touchCalls != 0 {
		t.Fatalf("account mismatch processed: ingress=%d update=%d touch=%d", ingress.calls, repo.updateCalls, repo.touchCalls)
	}
}

func TestSageXUploadHardeningPropagatesBackfillError(t *testing.T) {
	repo := &hardeningInstRepo{
		rows:      []*model.SageXInstallation{{ID: 1, TenantID: 1, InstallationID: "backfill-id", Token: "secret-token", Status: 1}},
		updateErr: errors.New("database unavailable"),
	}
	ingress := &hardeningIngress{}
	svc := newSageXUploadTestService(repo, &hardeningReceiptRepo{}, ingress)

	_, err := svc.HandleUpload(context.Background(), sageXUploadIdentifyCode, hardeningUploadRequest("backfill-id"), 10)
	if err == nil || errors.Is(err, ErrSageXUnauthorized) {
		t.Fatalf("backfill error must be visible: %v", err)
	}
	if repo.updateCalls != 1 || ingress.calls != 0 || repo.touchCalls != 0 {
		t.Fatalf("backfill failure continued: update=%d ingress=%d touch=%d", repo.updateCalls, ingress.calls, repo.touchCalls)
	}
}

func TestSageXUploadHardeningPropagatesTouchError(t *testing.T) {
	repo := &hardeningInstRepo{
		rows:     []*model.SageXInstallation{{ID: 1, TenantID: 1, InstallationID: "touch-id", AccountID: "1", Token: "secret-token", Status: 1}},
		touchErr: errors.New("database unavailable"),
	}
	ingress := &hardeningIngress{}
	svc := newSageXUploadTestService(repo, &hardeningReceiptRepo{}, ingress)

	_, err := svc.HandleUpload(context.Background(), sageXUploadIdentifyCode, hardeningUploadRequest("touch-id"), 10)
	if err == nil || errors.Is(err, ErrSageXUnauthorized) {
		t.Fatalf("touch error must be visible: %v", err)
	}
	if repo.touchCalls != 1 || ingress.calls != 0 {
		t.Fatalf("touch failure continued: touch=%d ingress=%d", repo.touchCalls, ingress.calls)
	}
}

func TestSageXUploadHardeningRejectsOversizedInstallationID(t *testing.T) {
	repo := &hardeningInstRepo{}
	svc := newSageXUploadTestService(repo, &hardeningReceiptRepo{}, &hardeningIngress{})

	_, err := svc.HandleUpload(context.Background(), sageXUploadIdentifyCode, hardeningUploadRequest(strings.Repeat("x", 101)), 10)
	if !errors.Is(err, ErrSageXInvalidInstallationID) {
		t.Fatalf("oversized installation ID error=%v", err)
	}
	if repo.getCalls != 0 || repo.createCalls != 0 {
		t.Fatalf("oversized ID accessed repository: get=%d create=%d", repo.getCalls, repo.createCalls)
	}
}

func TestSageXUploadHardeningConflictRereadSameOwner(t *testing.T) {
	repo := &hardeningInstRepo{forceConflict: true}
	repo.createHook = func(*model.SageXInstallation) {
		repo.rows = []*model.SageXInstallation{{
			ID: 1, TenantID: 1, InstallationID: "race-id", AccountID: "1", Token: "race-token", Status: 1,
		}}
	}
	svc := newSageXUploadTestService(repo, &hardeningReceiptRepo{}, &hardeningIngress{})

	if _, err := svc.HandleUpload(context.Background(), sageXUploadIdentifyCode, hardeningUploadRequest("race-id"), 10); err != nil {
		t.Fatalf("same-owner conflict must re-read successfully: %v", err)
	}
	if repo.getCalls != 2 || repo.createCalls != 1 {
		t.Fatalf("conflict path calls get=%d create=%d", repo.getCalls, repo.createCalls)
	}
}

func TestSageXUploadHardeningConflictRereadRejectsDisabled(t *testing.T) {
	repo := &hardeningInstRepo{forceConflict: true}
	repo.createHook = func(*model.SageXInstallation) {
		repo.rows = []*model.SageXInstallation{{
			ID: 1, TenantID: 1, InstallationID: "race-disabled-id", AccountID: "1", Token: "race-token", Status: 0,
		}}
	}
	svc := newSageXUploadTestService(repo, &hardeningReceiptRepo{}, &hardeningIngress{})

	_, err := svc.HandleUpload(context.Background(), sageXUploadIdentifyCode, hardeningUploadRequest("race-disabled-id"), 10)
	if !errors.Is(err, ErrSageXInstallationDisabled) {
		t.Fatalf("conflict re-read disabled error=%v", err)
	}
}

func TestSageXUploadHardeningUniqueErrorRereadsOwner(t *testing.T) {
	repo := &hardeningInstRepo{createErr: errors.New("duplicate key value violates unique constraint")}
	repo.createHook = func(*model.SageXInstallation) {
		repo.rows = []*model.SageXInstallation{{
			ID: 1, TenantID: 1, InstallationID: "unique-race-id", AccountID: "1", Token: "stored-token", Status: 1,
		}}
	}
	svc := newSageXUploadTestService(repo, &hardeningReceiptRepo{}, &hardeningIngress{})

	if _, err := svc.HandleUpload(context.Background(), sageXUploadIdentifyCode, hardeningUploadRequest("unique-race-id"), 10); err != nil {
		t.Fatalf("unique conflict must re-read stored owner: %v", err)
	}
	if repo.getCalls != 2 {
		t.Fatalf("unique conflict lookup calls=%d, want initial lookup + re-read", repo.getCalls)
	}
}

func TestSageXUploadHardeningConcurrentSameInstallationSingleRow(t *testing.T) {
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(tenant.ModeOff) })

	db := testutil.NewTestDB(t, &model.SageXInstallation{})
	instRepo := repository.NewSageXInstallationRepository()
	instRepo.SetDB(db)
	svc := newSageXUploadTestService(instRepo, &hardeningReceiptRepo{}, &hardeningIngress{})

	const workers = 12
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			req := hardeningUploadRequest("concurrent-upload-id")
			req.BatchID = fmt.Sprintf("batch-%d", worker)
			req.Events[0].EventID = fmt.Sprintf("event-%d", worker)
			_, err := svc.HandleUpload(context.Background(), sageXUploadIdentifyCode, req, 10)
			errs <- err
		}(i)
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent upload failed: %v", err)
		}
	}

	ctx := tenant.WithTenantID(context.Background(), 1)
	var rows []model.SageXInstallation
	if err := db.WithContext(ctx).Where("installation_id = ?", "concurrent-upload-id").Find(&rows).Error; err != nil {
		t.Fatalf("query concurrent rows: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("concurrent upload created %d rows, want 1", len(rows))
	}
}
