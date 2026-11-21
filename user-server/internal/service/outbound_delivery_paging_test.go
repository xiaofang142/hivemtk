package service

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/capregistry"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/pkg/utils/logger"
	"hivemtk-user/internal/repository"

	"gorm.io/gorm"
)

func seedDeliveryPage(t *testing.T, db *gorm.DB, ctx context.Context, account string, count int, msgType string) []model.MessageHub {
	t.Helper()
	tenantID, _ := tenant.FromContext(ctx)
	rows := make([]model.MessageHub, count)
	for i := range rows {
		rows[i] = model.MessageHub{TenantID: tenantID, Platform: "douyin_shop", AccountID: account,
			Direction: "outbound", Status: "pending", MsgType: msgType, MsgID: fmt.Sprintf("%d-%s-%s-%d", tenantID, account, msgType, i)}
	}
	if err := db.WithContext(ctx).Create(&rows).Error; err != nil {
		t.Fatal(err)
	}
	// Read back PostgreSQL timestamps and defaults for full-row comparisons.
	if err := db.WithContext(ctx).Where("tenant_id = ? AND account_id = ? AND msg_type = ?", tenantID, account, msgType).Order("id").Find(&rows).Error; err != nil {
		t.Fatal(err)
	}
	return rows
}

func assertDeliveryRowUnchanged(t *testing.T, db *gorm.DB, ctx context.Context, want model.MessageHub) {
	t.Helper()
	var got model.MessageHub
	if err := db.WithContext(ctx).First(&got, want.ID).Error; err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("row %d changed: got %+v, want %+v", want.ID, got, want)
	}
}

func TestOutboundDelivery_PagesPastDeniedRowsAndHonorsLimit(t *testing.T) {
	for _, mode := range []string{"poll", "owed"} {
		t.Run(mode, func(t *testing.T) {
			db := testutil.NewTestDB(t, &model.MessageHub{})
			repo := repository.NewMessageHubRepositoryWithDB(db)
			ctx := localBridgeSubmitContext(t, tenant.WithTenantID(context.Background(), 501), "douyin_shop", "page", "text")
			// More than a full 64-row page, with the first text at the end of the next page.
			denied := seedDeliveryPage(t, db, ctx, "page", 127, "image")
			allowed := seedDeliveryPage(t, db, ctx, "page", 3, "text")
			other := tenant.WithTenantID(context.Background(), 502)
			otherRow := seedDeliveryPage(t, db, other, "page", 1, "text")[0]
			var locked []int64
			if err := db.Callback().Query().After("gorm:query").Register("test:bounded_delivery_page", func(tx *gorm.DB) {
				if tx.Statement.Table == "message_hub" && strings.Contains(tx.Statement.SQL.String(), "FOR UPDATE") {
					locked = append(locked, tx.RowsAffected)
				}
			}); err != nil {
				t.Fatal(err)
			}
			var ids []uint
			if mode == "poll" {
				svc := &InboxIngressService{outboundAuthorizer: capregistry.AuthorizeBridgeSubmission, hubRepo: repo}
				rows, err := svc.ClaimPendingOutbound(ctx, "douyin_shop", "page", 2)
				if err != nil {
					t.Errorf("poll failed: %v", err)
				}
				for _, row := range rows {
					if row.Status != "inflight" || row.ClaimedAt == nil || row.PushAttempts != 1 || !row.SentAt.IsZero() {
						t.Errorf("invalid claim facts: %+v", row)
					}
					ids = append(ids, row.ID)
				}
			} else {
				rows, err := NewOutboundDeliveryService(repo, capregistry.AuthorizeBridgeSubmission).FetchOutboundUndelivered(ctx, "douyin_shop", "page", InboxOutboundClaimTimeout, 2)
				if err != nil {
					t.Errorf("owed query failed: %v", err)
				}
				for _, row := range rows {
					ids = append(ids, row.ID)
					assertDeliveryRowUnchanged(t, db, ctx, row)
				}
			}
			if !reflect.DeepEqual(ids, []uint{allowed[0].ID, allowed[1].ID}) {
				t.Errorf("expected two ordered text IDs across pages, got %v", ids)
			}
			if len(locked) < 3 || locked[0] != 64 {
				t.Errorf("expected bounded locked pages, got %v", locked)
			}
			for _, size := range locked {
				if size > 64 {
					t.Errorf("lock exceeded page bound: %v", locked)
				}
			}
			for _, row := range denied {
				assertDeliveryRowUnchanged(t, db, ctx, row)
			}
			assertDeliveryRowUnchanged(t, db, ctx, allowed[2])
			assertDeliveryRowUnchanged(t, db, other, otherRow)
		})
	}
}

func TestOutboundDelivery_PollReturnsCommittedClaimsOnLaterPageError(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	ctx := localBridgeSubmitContext(t, tenant.WithTenantID(context.Background(), 501), "douyin_shop", "partial", "text")
	texts := seedDeliveryPage(t, db, ctx, "partial", 1, "text")
	seedDeliveryPage(t, db, ctx, "partial", 64, "image")
	svc := &InboxIngressService{outboundAuthorizer: capregistry.AuthorizeBridgeSubmission, hubRepo: repository.NewMessageHubRepositoryWithDB(db)}
	pageErr := errors.New("later locked page failed")
	queries := 0
	if err := db.Callback().Query().After("gorm:query").Register("test:fail_later_delivery_page", func(tx *gorm.DB) {
		if sql := tx.Statement.SQL.String(); tx.Statement.Table == "message_hub" && strings.Contains(sql, "FOR UPDATE") && !strings.Contains(sql, "SKIP LOCKED") {
			queries++
			if queries == 2 {
				tx.AddError(pageErr)
			}
		}
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := svc.ClaimPendingOutbound(ctx, "douyin_shop", "partial", 2)
	if !errors.Is(err, pageErr) || len(rows) != 1 || rows[0].ID != texts[0].ID {
		t.Fatalf("must return committed claims alongside the later-page error: rows=%+v err=%v", rows, err)
	}
	assertDeliveryRowUnchanged(t, db, ctx, *rows[0])
}

func TestOutboundDelivery_AutomaticReclaimStaysDisabled(t *testing.T) {
	t.Cleanup(func() { outboundReclaimDisabledOnce = sync.Once{} })
	path := filepath.Join(t.TempDir(), "startup.log")
	logger.InitLogger(logger.LoggingConfig{Format: "json", Output: "file", File: path})
	t.Cleanup(func() { logger.InitLogger(logger.DefaultConfig()) })
	ReportOutboundReclaimDisabled()
	ReportOutboundReclaimDisabled()
	log, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(log), `"reason_code":"outbound_reclaim_evidence_source_missing"`) != 1 {
		t.Fatalf("expected a single disabled-state signal, got %s", log)
	}
}

func TestOutboundDelivery_PageWriteErrorRollsBack(t *testing.T) {
	for _, mode := range []string{"poll", "owed"} {
		t.Run(mode, func(t *testing.T) {
			db := testutil.NewTestDB(t, &model.MessageHub{})
			ctx := localBridgeSubmitContext(t, tenant.WithTenantID(context.Background(), 501), "douyin_shop", "write-error", "text")
			row := seedDeliveryPage(t, db, ctx, "write-error", 1, "text")[0]
			writeErr := errors.New("page exhaustion write failed")
			if err := db.Callback().Update().After("gorm:update").Register("test:fail_delivery_page_write", func(tx *gorm.DB) {
				if tx.Statement.Table == "message_hub" && strings.Contains(tx.Statement.SQL.String(), "push_error") {
					tx.AddError(writeErr)
				}
			}); err != nil {
				t.Fatal(err)
			}
			delivery := NewOutboundDeliveryService(repository.NewMessageHubRepositoryWithDB(db), capregistry.AuthorizeBridgeSubmission)
			fetch := delivery.ClaimPendingOutbound
			if mode == "owed" {
				fetch = delivery.FetchOutboundUndelivered
			}
			rows, err := fetch(ctx, row.Platform, row.AccountID, InboxOutboundClaimTimeout, 1)
			if !errors.Is(err, writeErr) || len(rows) != 0 {
				t.Errorf("expected the page write error and no rows, got rows=%v err=%v", rows, err)
			}
			assertDeliveryRowUnchanged(t, db, ctx, row)
		})
	}
}

func TestOutboundDelivery_ExplicitReclaimProcessesMixedRows(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	ctx := localBridgeSubmitContext(t, tenant.WithTenantID(context.Background(), 501), "douyin_shop", "reclaim-mixed", "text")
	images := seedDeliveryPage(t, db, ctx, "reclaim-mixed", 1, "image")
	texts := seedDeliveryPage(t, db, ctx, "reclaim-mixed", 3, "text")
	stale := time.Now().Add(-2 * OutboundStaleAfterDefault)
	for id, update := range map[uint]map[string]any{
		images[0].ID: {"status": "inflight", "claimed_at": stale, "push_attempts": repository.MaxOutboundPushAttempts, "created_at": stale},
		texts[0].ID:  {"status": "inflight", "claimed_at": stale, "push_attempts": 1},
		texts[1].ID:  {"push_attempts": repository.MaxOutboundPushAttempts},
		texts[2].ID:  {"created_at": stale},
	} {
		if err := db.WithContext(ctx).Model(&model.MessageHub{}).Where("id = ?", id).Updates(update).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := db.WithContext(ctx).First(&images[0], images[0].ID).Error; err != nil {
		t.Fatal(err)
	}
	task := NewOutboundDeliveryService(repository.NewMessageHubRepositoryWithDB(db), capregistry.AuthorizeBridgeSubmission)
	r, e, a, err := task.ReclaimOutbound(ctx, InboxOutboundClaimTimeout, OutboundStaleAfterDefault)
	if err != nil || r != 1 || e != 1 || a != 1 {
		t.Errorf("expected one of each approved transition: %d/%d/%d err=%v", r, e, a, err)
	}
	assertDeliveryRowUnchanged(t, db, ctx, images[0])
}

func TestOutboundDelivery_PollAndSSEHaveSingleClaimWinner(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	repo := repository.NewMessageHubRepositoryWithDB(db)
	svc := &InboxIngressService{outboundAuthorizer: capregistry.AuthorizeBridgeSubmission, hubRepo: repo}
	ctx := localBridgeSubmitContext(t, tenant.WithTenantID(context.Background(), 501), "douyin_shop", "single-winner", "text")
	seedDeliveryPage(t, db, ctx, "single-winner", 1, "image")
	row := seedDeliveryPage(t, db, ctx, "single-winner", 1, "text")[0]
	type result struct {
		count int
		err   error
	}
	ready := make(chan struct{})
	results := make(chan result, 2)
	utils.SafeGo(ctx, "test poll claim", func(ctx context.Context) {
		<-ready
		rows, err := svc.ClaimPendingOutbound(ctx, row.Platform, row.AccountID, 1)
		results <- result{len(rows), err}
	})
	utils.SafeGo(ctx, "test SSE claim", func(ctx context.Context) {
		<-ready
		claimed, err := NewOutboundDeliveryService(repo, capregistry.AuthorizeBridgeSubmission).ClaimOutboundForPush(ctx, uint64(row.ID), row.Platform, row.AccountID, row.MsgType, InboxOutboundClaimTimeout)
		n := 0
		if claimed {
			n++
		}
		results <- result{n, err}
	})
	close(ready)
	var count int
	for range 2 {
		res := <-results
		if res.err != nil {
			var denied interface{ CapabilityReasonCode() string }
			if !errors.As(res.err, &denied) || denied.CapabilityReasonCode() != "request_action_mismatch" {
				t.Errorf("unexpected claim error: %v", res.err)
			}
		}
		count += res.count
	}
	if err := db.WithContext(ctx).First(&row, row.ID).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 || row.PushAttempts != 1 || row.Status != "inflight" {
		t.Fatalf("expected exactly one claim and one used attempt: count=%d row=%+v", count, row)
	}
}
