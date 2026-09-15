package service

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func TestDispatchWhatsAppStatuses_AllStates(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := newTestWebhookService(db)
	ctx := context.Background()

	const account = "wa-acc-t3"
	mkRow := func(msgID string) *model.MessageHub {
		row := &model.MessageHub{MsgID: msgID, Platform: "whatsapp", AccountID: account, Direction: "outbound", MsgType: "text", ConversationID: "c", Content: "x", SentAt: time.Now()}
		if err := db.Create(row).Error; err != nil {
			t.Fatalf("create: %v", err)
		}
		return row
	}
	sent := mkRow("wamid.SENT1")
	delivered := mkRow("wamid.DELIV1")
	read := mkRow("wamid.READ1")
	failed := mkRow("wamid.FAIL1")

	statusPayload := func(entries string) []byte {
		return []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA","changes":[{"field":"messages","value":{"metadata":{"phone_number_id":"PNID"},"statuses":[` + entries + `]}}]}]}`)
	}

	handled, err := svc.dispatchWhatsAppStatuses(ctx, account, statusPayload(`{"id":"wamid.SENT1","status":"sent","timestamp":"1700000000","recipient_id":"8613800000000"}`))
	if err != nil || !handled {
		t.Fatalf("sent handled=%v err=%v", handled, err)
	}
	handled, _ = svc.dispatchWhatsAppStatuses(ctx, account, statusPayload(`{"id":"wamid.DELIV1","status":"delivered","timestamp":"1700000001","recipient_id":"8613800000000"}`))
	if !handled {
		t.Fatal("delivered should be handled")
	}
	handled, _ = svc.dispatchWhatsAppStatuses(ctx, account, statusPayload(`{"id":"wamid.READ1","status":"read","timestamp":"1700000002","recipient_id":"8613800000000"}`))
	if !handled {
		t.Fatal("read should be handled")
	}
	handled, _ = svc.dispatchWhatsAppStatuses(ctx, account, statusPayload(`{"id":"wamid.FAIL1","status":"failed","timestamp":"1700000003","recipient_id":"8613800000000","errors":[{"code":131047,"title":"Re-engagement message","message":"More than 24 hours have passed"}]}`))
	if !handled {
		t.Fatal("failed should be handled")
	}

	repo := repository.NewMessageHubRepositoryWithDB(db)
	gotSent, _ := repo.GetByID(ctx, sent.ID)
	if gotSent.Status != "sent" {
		t.Fatalf("sent 行应置 sent, got %s", gotSent.Status)
	}
	gotDeliv, _ := repo.GetByID(ctx, delivered.ID)
	if gotDeliv.Status != "delivered" {
		t.Fatalf("delivered 行应置 delivered, got %s", gotDeliv.Status)
	}
	gotRead, _ := repo.GetByID(ctx, read.ID)
	if !gotRead.IsRead || gotRead.ReadAt == nil {
		t.Fatalf("read 行应置 IsRead+ReadAt, got %+v", gotRead)
	}
	gotFail, _ := repo.GetByID(ctx, failed.ID)
	if gotFail.Status != "send_failed" {
		t.Fatalf("failed 行应置 send_failed, got %s", gotFail.Status)
	}
	if v, ok := gotFail.Extra["delivery_error"].(string); !ok || v == "" {
		t.Fatalf("failed 行应记录 delivery_error, extra=%v", gotFail.Extra)
	}
}

func TestDispatchWhatsAppStatuses_MissAndPassthrough(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := newTestWebhookService(db)
	ctx := context.Background()

	handled, err := svc.dispatchWhatsAppStatuses(ctx, "wa-acc-t3b", []byte(`{"entry":[{"changes":[{"value":{"statuses":[{"id":"wamid.UNKNOWN9","status":"delivered"}]}}]}]}`))
	if err != nil || !handled {
		t.Fatalf("unknown wamid 应静默处理 handled=%v err=%v", handled, err)
	}

	handled, err = svc.dispatchWhatsAppStatuses(ctx, "wa-acc-t3b", []byte(`{"entry":[{"changes":[{"value":{"messages":[{"from":"86138","id":"wamid.INMSG1","timestamp":"1","type":"text","text":{"body":"hi"}}]}}]}]}`))
	if err != nil {
		t.Fatalf("msg payload err: %v", err)
	}
	if handled {
		t.Fatal("messages 推送不应被 statuses 分支吞掉")
	}

	handled, _ = svc.dispatchWhatsAppStatuses(ctx, "wa-acc-t3b", []byte(`not-json`))
	if handled {
		t.Fatal("invalid payload should passthrough")
	}
}

func TestUpdateDeliveryStatus_TerminalStateGuard(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	repo := repository.NewMessageHubRepositoryWithDB(db)
	ctx := context.Background()
	row := &model.MessageHub{MsgID: "wamid.ST9", Platform: "whatsapp", AccountID: "a", Direction: "outbound", MsgType: "text", ConversationID: "c", Content: "x", SentAt: time.Now()}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.UpdateDeliveryStatus(ctx, "whatsapp", "a", "wamid.ST9", "weird_status", ""); err != nil {
		t.Fatalf("unknown status should be no-op: %v", err)
	}
	got, _ := repo.GetByID(ctx, row.ID)
	if got.Status == "weird_status" {
		t.Fatalf("unknown status must not be written")
	}

	if err := repo.UpdateDeliveryStatus(ctx, "whatsapp", "a", "wamid.ST9", "failed", "boom"); err != nil {
		t.Fatalf("failed: %v", err)
	}
	if err := repo.UpdateDeliveryStatus(ctx, "whatsapp", "a", "wamid.ST9", "sent", ""); err != nil {
		t.Fatalf("late sent: %v", err)
	}
	got2, _ := repo.GetByID(ctx, row.ID)
	if got2.Status != "send_failed" {
		t.Fatalf("terminal send_failed must not be flipped by late sent, got %s", got2.Status)
	}
}

func TestDispatchWhatsAppStatuses_MixedPayloadPassesMessages(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	svc := newTestWebhookService(db)
	ctx := context.Background()
	row := &model.MessageHub{MsgID: "wamid.MIX1", Platform: "whatsapp", AccountID: "wa-mix", Direction: "outbound", MsgType: "text", ConversationID: "c", Content: "x", SentAt: time.Now()}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	mixed := []byte(`{"object":"whatsapp_business_account","entry":[{"id":"WABA","changes":[{"field":"messages","value":{"metadata":{"phone_number_id":"PN"},"statuses":[{"id":"wamid.MIX1","status":"delivered"}],"messages":[{"from":"86138","id":"wamid.INMIX","timestamp":"1","type":"text","text":{"body":"hello"}}]}}]}]}`)
	handled, err := svc.dispatchWhatsAppStatuses(ctx, "wa-mix", mixed)
	if err != nil {
		t.Fatalf("mixed payload err: %v", err)
	}
	if handled {
		t.Fatal("混合推送必须放行消息主管线（handled=false）")
	}
	repo := repository.NewMessageHubRepositoryWithDB(db)
	got, _ := repo.GetByID(ctx, row.ID)
	if got.Status != "delivered" {
		t.Fatalf("混合推送的回执仍应被消费, got %s", got.Status)
	}
}
