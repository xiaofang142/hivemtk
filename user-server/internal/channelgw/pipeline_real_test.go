package channelgw

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"
)

func newRealPipeline(t *testing.T) (IngressPipeline, *cache.MemoryCache) {
	t.Helper()
	database := testutil.NewTestDB(t, &model.MessageHub{})
	db.SetTestDB(database)
	t.Cleanup(func() { db.SetTestDB(nil) })
	c := cache.NewMemoryCacheWithLimit(1024)
	t.Cleanup(func() { c.Close() })
	ingress := service.NewInboxIngressService()
	return NewPipeline(ingress), c
}

// TestPipeline_IngestBatch_RealService 覆盖 ingress != nil 分支
func TestPipeline_IngestBatch_RealService(t *testing.T) {
	p, _ := newRealPipeline(t)
	ctx := context.Background()
	now := time.Now()

	evs := []*model.MessageEvent{{
		EventID:        "p-ib-001",
		SessionID:      "sess-p-001",
		Channel:        model.ChannelDouyin,
		SenderID:       "cust-1",
		MsgType:        "text",
		Content:        "hello pipeline",
		ConversationID: "conv-p-001",
		Timestamp:      now,
	}}
	res, err := p.IngestBatch(ctx, evs)
	if err != nil {
		t.Fatalf("IngestBatch 不应失败: %v", err)
	}
	if res == nil {
		t.Fatal("IngestBatch 返回 nil result")
	}
	if len(res.PerEvent) == 0 {
		t.Fatal("IngestBatch 应返回至少一条 result")
	}
}

// TestPipeline_PersistHistory_RealService 覆盖 ingress != nil 分支
func TestPipeline_PersistHistory_RealService(t *testing.T) {
	p, _ := newRealPipeline(t)
	ctx := context.Background()

	ev := &model.MessageEvent{
		EventID:        "p-hist-001",
		SessionID:      "sess-p-hist",
		Channel:        model.ChannelDouyin,
		SenderID:       "cust-2",
		MsgType:        "text",
		Content:        "history msg",
		ConversationID: "conv-p-hist-001",
		Timestamp:      time.Now(),
	}
	if err := p.PersistHistory(ctx, ev, "inbound"); err != nil {
		t.Fatalf("PersistHistory 不应失败: %v", err)
	}
}

// TestPipeline_ClaimAndAck_RealService 覆盖 ClaimOutbound + AckOutbound 的 ingress != nil 分支
func TestPipeline_ClaimAndAck_RealService(t *testing.T) {
	p, _ := newRealPipeline(t)
	ctx := context.Background()

	hubs, err := p.ClaimOutbound(tenant.WithTenantID(ctx, 1), model.ChannelDouyin, "acc-3", 10)
	if err != nil {
		t.Fatalf("ClaimOutbound 不应失败: %v", err)
	}
	_ = hubs

	n, err := p.AckOutbound(ctx, model.ChannelDouyin, "acc-3", []string{"p-ack-nope"})
	if err != nil {
		t.Fatalf("AckOutbound 不应失败: %v", err)
	}
	_ = n
}
