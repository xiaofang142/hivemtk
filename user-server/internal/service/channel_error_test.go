package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

func TestClassifyChannelError_TableDriven(t *testing.T) {
	cases := []struct {
		name       string
		raw        string
		wantCat    ChannelErrorCategory
		wantRetry  bool
		wantStatus int
	}{
		{"wa rate limit 131048", `wa send status 400: {"error":{"code":131048,"message":"too many messages"}}`, CategoryRateLimited, true, 400},
		{"http 429 plain", "send failed status 429: slow down", CategoryRateLimited, true, 429},
		{"tg retry_after", `tg send status 429: {"parameters":{"retry_after":31}}`, CategoryRateLimited, true, 429},
		{"wa re-engagement 131047", `wa send status 400: code 131047 re-engagement`, CategoryAuth, false, 400},
		{"http 401", "feishu api status 401: invalid access token", CategoryAuth, false, 401},
		{"http 403", "send failed status 403: forbidden", CategoryAuth, false, 403},
		{"http 400 plain", "send failed status 400: bad parameter", CategoryBadRequest, false, 400},
		{"http 404", "status 404: not found", CategoryBadRequest, false, 404},
		{"http 500", "status 500: internal", CategoryNetwork, true, 500},
		{"conn refused", "dial tcp: connection refused", CategoryNetwork, true, 0},
		{"timeout", "context deadline exceeded", CategoryNetwork, true, 0},
		{"unknown fallback", "some weird platform error", CategoryUnknown, true, 0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ce := AsChannelError(errors.New(c.raw))
			if ce.Category != c.wantCat {
				t.Fatalf("category=%s want %s (raw=%s)", ce.Category, c.wantCat, c.raw)
			}
			if ce.Retryable != c.wantRetry {
				t.Fatalf("retryable=%v want %v", ce.Retryable, c.wantRetry)
			}
			if c.wantStatus != 0 && ce.StatusCode != c.wantStatus {
				t.Fatalf("status=%d want %d", ce.StatusCode, c.wantStatus)
			}
		})
	}
}

func TestRetryDelaysFor(t *testing.T) {
	rl := &ChannelError{Category: CategoryRateLimited, Retryable: true, RetryAfter: 31 * time.Second}
	d := retryDelaysFor(rl)
	if len(d) != 1 || d[0] != 31*time.Second {
		t.Fatalf("rate limited should use retry_after 31s, got %v", d)
	}
	auth := &ChannelError{Category: CategoryAuth, Retryable: false}
	if d := retryDelaysFor(auth); d != nil {
		t.Fatalf("non-retryable should return nil, got %v", d)
	}
	net := &ChannelError{Category: CategoryNetwork, Retryable: true}
	d = retryDelaysFor(net)
	if len(d) != 3 {
		t.Fatalf("network should use default backoff, got %v", d)
	}
	if d := retryDelaysFor(nil); len(d) != 3 {
		t.Fatalf("nil error should use default backoff")
	}
}

func TestOutboundSendFailed_MarksTerminalState(t *testing.T) {
	db := testutil.NewTestDBOrSkip(t, &model.MessageHub{})
	repo := repository.NewMessageHubRepositoryWithDB(db)
	svc := &WebhookService{messageHubRepo: repo}
	ctx := context.Background()

	row := &model.MessageHub{MsgID: "wamid.T4AUTH", Platform: "whatsapp", AccountID: "a", Direction: "outbound", MsgType: "text", ConversationID: "c", Content: "x", SentAt: time.Now()}
	if err := db.Create(row).Error; err != nil {
		t.Fatalf("create: %v", err)
	}
	fresh, _ := repo.GetByID(ctx, row.ID)

	svc.outboundSendFailed(ctx, ChannelWhatsapp, "a", fresh, "x", errors.New("wa send status 401: invalid token"))
	got, _ := repo.GetByID(ctx, row.ID)
	if got.Status != "send_failed" {
		t.Fatalf("auth failure should mark send_failed, got %s", got.Status)
	}
	if cat, _ := got.Extra["send_failed_category"].(string); cat != string(CategoryAuth) {
		t.Fatalf("extra should record category, got %v", got.Extra)
	}

	row2 := &model.MessageHub{MsgID: "wamid.T4NET", Platform: "whatsapp", AccountID: "a", Direction: "outbound", MsgType: "text", ConversationID: "c", Content: "x", SentAt: time.Now()}
	if err := db.Create(row2).Error; err != nil {
		t.Fatalf("create2: %v", err)
	}
	fresh2, _ := repo.GetByID(ctx, row2.ID)
	svc.outboundSendFailed(ctx, ChannelWhatsapp, "a", fresh2, "x", errors.New("dial tcp: connection refused"))
	got2, _ := repo.GetByID(ctx, row2.ID)
	if got2.Status == "send_failed" {
		t.Fatalf("retryable network error must not mark terminal state")
	}

	svc.outboundSendFailed(ctx, ChannelWhatsapp, "a", nil, "x", errors.New("status 401"))
}
