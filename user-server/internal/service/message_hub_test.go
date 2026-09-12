package service

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"hivemtk-user/internal/cache"
	"hivemtk-user/internal/model"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"
)

func setupMessageHubTestDB(t *testing.T) *gorm.DB {
	return testutil.NewTestDB(t,
		&model.MessageHub{},
	)
}

func newMessageHubTestService(t *testing.T) (*MessageHubService, *gorm.DB) {
	db := setupMessageHubTestDB(t)
	_mc1 := cache.NewMemoryCache()
	defer _mc1.Close()
	svc := NewMessageHubServiceWithDB(db, _mc1)
	return svc, db
}

var newReqCounter int64

func newReq() PushMessageRequest {
	newReqCounter++
	return PushMessageRequest{

		Platform:       "wecom",
		AccountID:      "acc-001",
		MsgID:          fmt.Sprintf("msg-%d-%d", time.Now().UnixNano(), newReqCounter),
		Direction:      "inbound",
		MsgType:        "text",
		SenderID:       "user-001",
		SenderName:     "张三",
		ReceiverID:     "acc-001",
		Content:        "你好",
		ConversationID: "conv-001",
		Extra:          map[string]any{"source": "test"},
	}
}

func TestValidPlatform_Supported(t *testing.T) {
	for _, p := range []string{"wecom", "personal_wx", "douyin", "kuaishou", "xiaohongshu", "xianyu", "tiktok", "whatsapp", "sms", "email"} {
		if !ValidPlatform(p) {
			t.Errorf("expected %s valid", p)
		}
	}
}

func TestValidPlatform_Unsupported(t *testing.T) {
	for _, p := range []string{"", "unknown", "facebook", "twitter", "wechat", "QQ", "weibo", "钉钉", "邮箱", "123"} {
		if ValidPlatform(p) {
			t.Errorf("expected %s invalid", p)
		}
	}
}

func TestValidMsgType_Supported(t *testing.T) {
	for _, m := range []string{"text", "image", "file", "audio", "video", "link", "card", "location"} {
		if !ValidMsgType(m) {
			t.Errorf("expected %s valid", m)
		}
	}
}

func TestValidMsgType_Unsupported(t *testing.T) {
	for _, m := range []string{"", "unknown", "sticker", "miniprogram", "voice", "doc", "voiceprint"} {
		if ValidMsgType(m) {
			t.Errorf("expected %s invalid", m)
		}
	}
}

func TestValidDirection_Supported(t *testing.T) {
	for _, d := range []string{"inbound", "outbound"} {
		if !ValidDirection(d) {
			t.Errorf("expected %s valid", d)
		}
	}
}

func TestValidDirection_Unsupported(t *testing.T) {
	for _, d := range []string{"", "in", "out", "bi-direction", "unknown"} {
		if ValidDirection(d) {
			t.Errorf("expected %s invalid", d)
		}
	}
}

func TestListPlatforms(t *testing.T) {
	list := ListPlatforms()
	if len(list) < 8 {
		t.Errorf("expected >= 8 platforms, got %d", len(list))
	}
}

func TestListMsgTypes(t *testing.T) {
	list := ListMsgTypes()
	if len(list) < 5 {
		t.Errorf("expected >= 5 msg types, got %d", len(list))
	}
}

func TestNormalize_InvalidPlatform(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	req.Platform = "unknown_platform"
	_, err := svc.Normalize(context.Background(), &req)
	if err == nil {
		t.Fatal("expected error for invalid platform")
	}
	if !strings.Contains(err.Error(), "invalid platform") {
		t.Errorf("expected invalid platform error, got %v", err)
	}
}

func TestNormalize_EmptyAccountID(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	req.AccountID = ""
	_, err := svc.Normalize(context.Background(), &req)
	if err == nil {
		t.Fatal("expected error for empty account_id")
	}
}

func TestNormalize_EmptyMsgID(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	req.MsgID = ""
	_, err := svc.Normalize(context.Background(), &req)
	if err == nil {
		t.Fatal("expected error for empty msg_id")
	}
}

func TestNormalize_InvalidDirection(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	req.Direction = "sideways"
	_, err := svc.Normalize(context.Background(), &req)
	if err == nil {
		t.Fatal("expected error for invalid direction")
	}
}

func TestNormalize_InvalidMsgType(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	req.MsgType = "sticker"
	_, err := svc.Normalize(context.Background(), &req)
	if err == nil {
		t.Fatal("expected error for invalid msg_type")
	}
}

func TestNormalize_EmptyTextContent(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	req.Content = "   "
	_, err := svc.Normalize(context.Background(), &req)
	if err == nil {
		t.Fatal("expected error for empty text content")
	}
}

func TestNormalize_EmptyImageContent(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	req.MsgType = "image"
	req.Content = ""
	_, err := svc.Normalize(context.Background(), &req)
	if err != nil {
		t.Errorf("image with empty content should be allowed, got %v", err)
	}
}

func TestNormalize_ContentTooLarge(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	svc.WithMaxContent(context.Background(), 100)
	req := newReq()
	req.Content = strings.Repeat("x", 200)
	_, err := svc.Normalize(context.Background(), &req)
	if err == nil {
		t.Fatal("expected error for content too large")
	}
}

func TestNormalize_CustomSentAt(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	custom := time.Date(2026, 1, 1, 12, 0, 0, 0, time.UTC)
	req.SentAt = &custom
	msg, err := svc.Normalize(context.Background(), &req)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if !msg.SentAt.Equal(custom) {
		t.Errorf("expected sent_at = %v, got %v", custom, msg.SentAt)
	}
}

func TestNormalize_TrimWhitespace(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	req.SenderID = "  user-001  "
	req.SenderName = "  张三  "
	msg, err := svc.Normalize(context.Background(), &req)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if msg.SenderID != "user-001" {
		t.Errorf("expected trimmed sender_id, got %q", msg.SenderID)
	}
	if msg.SenderName != "张三" {
		t.Errorf("expected trimmed sender_name, got %q", msg.SenderName)
	}
}

func TestNormalize_NilExtra(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	req.Extra = nil
	msg, err := svc.Normalize(context.Background(), &req)
	if err != nil {
		t.Fatalf("normalize: %v", err)
	}
	if msg.Extra == nil {
		t.Error("expected non-nil Extra after normalize")
	}
}

func TestNormalize_AllPlatforms(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	for _, p := range ListPlatforms() {
		req := newReq()
		req.Platform = p
		req.MsgID = fmt.Sprintf("msg-%s-%d", p, time.Now().UnixNano())
		_, err := svc.Normalize(context.Background(), &req)
		if err != nil {
			t.Errorf("platform %s normalize: %v", p, err)
		}
	}
}

func TestIdempotencyKey_Stable(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	k1 := svc.IdempotencyKey(context.Background(), "wecom", "acc-1", "msg-1")
	k2 := svc.IdempotencyKey(context.Background(), "wecom", "acc-1", "msg-1")
	if k1 != k2 {
		t.Errorf("expected same key, got %q vs %q", k1, k2)
	}
}

func TestIdempotencyKey_Different(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	k1 := svc.IdempotencyKey(context.Background(), "wecom", "acc-1", "msg-1")
	k2 := svc.IdempotencyKey(context.Background(), "wecom", "acc-2", "msg-1")
	if k1 == k2 {
		t.Error("expected different keys for different account")
	}
	k3 := svc.IdempotencyKey(context.Background(), "wecom", "acc-1", "msg-2")
	if k1 == k3 {
		t.Error("expected different keys for different msg")
	}
}

func TestCheckIdempotent_NewMessage(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	exist, _, err := svc.CheckIdempotent(context.Background(), "wecom", "acc-1", "msg-new")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if exist {
		t.Error("expected not exist for new message")
	}
}

func TestCheckIdempotent_DuplicateMessage(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	req.MsgID = "msg-dup-1"
	_, err := svc.Push(context.Background(), &req)
	if err != nil {
		t.Fatalf("first push: %v", err)
	}
	exist, id, err := svc.CheckIdempotent(context.Background(), "wecom", req.AccountID, "msg-dup-1")
	if err != nil {
		t.Fatalf("check: %v", err)
	}
	if !exist {
		t.Error("expected exist for duplicate message")
	}
	if id == 0 {
		t.Error("expected non-zero id for duplicate")
	}
}

func TestCheckIdempotent_DifferentAccount(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	req.MsgID = "msg-cross"
	_, _ = svc.Push(context.Background(), &req)
	exist, _, _ := svc.CheckIdempotent(context.Background(), "wecom", "different-acc", "msg-cross")
	if exist {
		t.Error("expected not exist for different account")
	}
}

func TestPush_Success(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	msg, err := svc.Push(context.Background(), &req)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if msg.ID == 0 {
		t.Error("expected non-zero id")
	}
}

func TestPush_DuplicateError(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	req.MsgID = "msg-push-dup"
	_, err := svc.Push(context.Background(), &req)
	if err != nil {
		t.Fatalf("first push: %v", err)
	}
	_, err = svc.Push(context.Background(), &req)
	if err != ErrMessageHubIdempotent {
		t.Errorf("expected ErrMessageHubIdempotent, got %v", err)
	}
}

func TestPush_NilDB(t *testing.T) {
	_mc2 := cache.NewMemoryCache()
	defer _mc2.Close()
	svc := NewMessageHubServiceWithDB(nil, _mc2)
	req := newReq()

	_, err := svc.Push(context.Background(), &req)
	if err != nil {
		t.Errorf("nil db push should succeed via queue, got %v", err)
	}
}

func TestPush_QueueAfterDB(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	req.MsgID = "msg-q-1"
	_, err := svc.Push(context.Background(), &req)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if size := svc.Size(context.Background(), "wecom", req.AccountID); size < 1 {
		t.Errorf("expected queue size >= 1, got %d", size)
	}
}

func TestPush_QueueFull(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	svc.WithQueueSize(context.Background(), 2)
	for i := 0; i < 3; i++ {
		req := newReq()
		req.MsgID = fmt.Sprintf("full-%d", i)
		_, _ = svc.Push(context.Background(), &req)
	}
}

func TestPushBatch_AllSuccess(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	reqs := []PushMessageRequest{
		newReq(), newReq(), newReq(),
	}
	results, errs := svc.PushBatch(context.Background(), reqs)
	if len(results) != 3 || len(errs) != 3 {
		t.Fatalf("expected 3 results, got %d/%d", len(results), len(errs))
	}
	for i, e := range errs {
		if e != nil {
			t.Errorf("req %d err: %v", i, e)
		}
	}
}

func TestPushBatch_PartialError(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	r1 := newReq()
	r1.MsgID = "batch-dup"
	_, _ = svc.Push(context.Background(), &r1)
	r2 := newReq()
	r2.MsgID = "batch-2"
	reqs := []PushMessageRequest{r1, r2}
	results, errs := svc.PushBatch(context.Background(), reqs)
	if errs[0] != ErrMessageHubIdempotent {
		t.Errorf("expected first to be idempotent error, got %v", errs[0])
	}
	if errs[1] != nil {
		t.Errorf("expected second to succeed, got %v", errs[1])
	}
	if results[1] == nil {
		t.Error("expected second result to be non-nil")
	}
}

func TestList_DefaultPage(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	for i := 0; i < 25; i++ {
		r := newReq()
		r.MsgID = fmt.Sprintf("list-%d", i)
		_, _ = svc.Push(context.Background(), &r)
	}
	list, total, err := svc.List(context.Background(), ListQuery{})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if total != 25 {
		t.Errorf("expected total=25, got %d", total)
	}
	if len(list) != 20 {
		t.Errorf("expected page size 20, got %d", len(list))
	}
}

func TestList_FilterPlatform(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	r1 := newReq()
	r1.Platform = "wecom"
	r1.MsgID = "w1"
	_, _ = svc.Push(context.Background(), &r1)
	r2 := newReq()
	r2.Platform = "douyin"
	r2.MsgID = "d1"
	_, _ = svc.Push(context.Background(), &r2)
	list, total, _ := svc.List(context.Background(), ListQuery{Platform: "douyin"})
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
	if len(list) == 1 && list[0].Platform != "douyin" {
		t.Errorf("expected douyin, got %s", list[0].Platform)
	}
}

func TestList_FilterDirection(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	r1 := newReq()
	r1.Direction = "inbound"
	r1.MsgID = "i1"
	_, _ = svc.Push(context.Background(), &r1)
	r2 := newReq()
	r2.Direction = "outbound"
	r2.MsgID = "o1"
	_, _ = svc.Push(context.Background(), &r2)
	_, total, _ := svc.List(context.Background(), ListQuery{Direction: "outbound"})
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
}

func TestList_FilterMsgType(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	r1 := newReq()
	r1.MsgType = "text"
	r1.MsgID = "t1"
	_, _ = svc.Push(context.Background(), &r1)
	r2 := newReq()
	r2.MsgType = "image"
	r2.MsgID = "img1"
	_, _ = svc.Push(context.Background(), &r2)
	_, total, _ := svc.List(context.Background(), ListQuery{MsgType: "image"})
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
}

func TestList_FilterKeyword(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	r1 := newReq()
	r1.Content = "hello world"
	r1.MsgID = "kw1"
	_, _ = svc.Push(context.Background(), &r1)
	r2 := newReq()
	r2.Content = "你好世界"
	r2.MsgID = "kw2"
	_, _ = svc.Push(context.Background(), &r2)
	_, total, _ := svc.List(context.Background(), ListQuery{Keyword: "hello"})
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
}

func TestList_FilterIsRead(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	r1 := newReq()
	r1.MsgID = "rd1"
	m1, _ := svc.Push(context.Background(), &r1)
	_ = svc.MarkRead(context.Background(), []uint{m1.ID})
	r2 := newReq()
	r2.MsgID = "rd2"
	_, _ = svc.Push(context.Background(), &r2)
	read := true
	_, total, _ := svc.List(context.Background(), ListQuery{IsRead: &read})
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
}

func TestList_FilterIsGroup(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	r1 := newReq()
	r1.IsGroup = true
	r1.MsgID = "g1"
	_, _ = svc.Push(context.Background(), &r1)
	r2 := newReq()
	r2.IsGroup = false
	r2.MsgID = "ng1"
	_, _ = svc.Push(context.Background(), &r2)
	isGroup := true
	_, total, _ := svc.List(context.Background(), ListQuery{IsGroup: &isGroup})
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
}

func TestList_FilterTimeRange(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	r1 := newReq()
	r1.MsgID = "tr1"
	past := time.Now().Add(-2 * time.Hour)
	r1.SentAt = &past
	_, _ = svc.Push(context.Background(), &r1)
	r2 := newReq()
	r2.MsgID = "tr2"
	now := time.Now()
	r2.SentAt = &now
	_, _ = svc.Push(context.Background(), &r2)
	start := time.Now().Add(-1 * time.Hour)
	_, total, _ := svc.List(context.Background(), ListQuery{StartTime: &start})
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
}

func TestList_PageSize(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	for i := 0; i < 50; i++ {
		r := newReq()
		r.MsgID = fmt.Sprintf("page-%d", i)
		_, _ = svc.Push(context.Background(), &r)
	}
	_, total1, _ := svc.List(context.Background(), ListQuery{Page: 1, PageSize: 10})
	_, total2, _ := svc.List(context.Background(), ListQuery{Page: 1, PageSize: 100})
	if total1 != 50 || total2 != 50 {
		t.Errorf("expected total=50, got %d/%d", total1, total2)
	}
}

func TestList_OrderBy(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	for i := 0; i < 5; i++ {
		r := newReq()
		r.MsgID = fmt.Sprintf("ord-%d", i)
		_, _ = svc.Push(context.Background(), &r)
	}
	_, _, err := svc.List(context.Background(), ListQuery{OrderBy: "sent_at ASC"})
	if err != nil {
		t.Errorf("expected no error for valid order by, got %v", err)
	}
	_, _, err = svc.List(context.Background(), ListQuery{OrderBy: "DROP TABLE"})
	if err != nil {
		t.Errorf("expected no error for invalid order by (whitelist), got %v", err)
	}
}

func TestList_FilterConversationID(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	r1 := newReq()
	r1.ConversationID = "conv-A"
	r1.MsgID = "ca1"
	_, _ = svc.Push(context.Background(), &r1)
	r2 := newReq()
	r2.ConversationID = "conv-B"
	r2.MsgID = "cb1"
	_, _ = svc.Push(context.Background(), &r2)
	_, total, _ := svc.List(context.Background(), ListQuery{ConversationID: "conv-B"})
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
}

func TestList_FilterSenderID(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	r1 := newReq()
	r1.SenderID = "sender-A"
	r1.MsgID = "sa1"
	_, _ = svc.Push(context.Background(), &r1)
	r2 := newReq()
	r2.SenderID = "sender-B"
	r2.MsgID = "sb1"
	_, _ = svc.Push(context.Background(), &r2)
	_, total, _ := svc.List(context.Background(), ListQuery{SenderID: "sender-A"})
	if total != 1 {
		t.Errorf("expected total=1, got %d", total)
	}
}

func TestGetByID_NotFound(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	msg, err := svc.GetByID(context.Background(), 999)
	if err != nil {
		t.Fatalf("expected no error for not found, got %v", err)
	}
	if msg != nil {
		t.Error("expected nil for not found")
	}
}

func TestGetByID_Success(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	msg, _ := svc.Push(context.Background(), &req)
	got, err := svc.GetByID(context.Background(), msg.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.ID != msg.ID {
		t.Errorf("expected same id, got %v", got)
	}
}

func TestMarkRead_SingleID(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	msg, _ := svc.Push(context.Background(), &req)
	if err := svc.MarkRead(context.Background(), []uint{msg.ID}); err != nil {
		t.Fatalf("mark: %v", err)
	}
	got, _ := svc.GetByID(context.Background(), msg.ID)
	if !got.IsRead {
		t.Error("expected is_read=true")
	}
	if got.ReadAt == nil {
		t.Error("expected read_at non-nil")
	}
}

func TestMarkRead_MultipleIDs(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	var ids []uint
	for i := 0; i < 5; i++ {
		r := newReq()
		r.MsgID = fmt.Sprintf("mr-%d", i)
		m, _ := svc.Push(context.Background(), &r)
		ids = append(ids, m.ID)
	}
	if err := svc.MarkRead(context.Background(), ids); err != nil {
		t.Fatalf("mark: %v", err)
	}
	for _, id := range ids {
		got, _ := svc.GetByID(context.Background(), id)
		if !got.IsRead {
			t.Errorf("expected id %d read=true", id)
		}
	}
}

func TestMarkRead_EmptyIDs(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	if err := svc.MarkRead(context.Background(), []uint{}); err != nil {
		t.Errorf("expected no error for empty ids, got %v", err)
	}
}

func TestStats_Empty(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	stats, err := svc.GetStats(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("stats: %v", err)
	}
	if stats.Total != 0 {
		t.Errorf("expected total=0, got %d", stats.Total)
	}
}

func TestStats_TotalInboundOutbound(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	for i := 0; i < 3; i++ {
		r := newReq()
		r.Direction = "inbound"
		r.MsgID = fmt.Sprintf("i-%d", i)
		_, _ = svc.Push(context.Background(), &r)
	}
	for i := 0; i < 2; i++ {
		r := newReq()
		r.Direction = "outbound"
		r.MsgID = fmt.Sprintf("o-%d", i)
		_, _ = svc.Push(context.Background(), &r)
	}
	stats, _ := svc.GetStats(context.Background(), nil, nil)
	if stats.Total != 5 {
		t.Errorf("expected total=5, got %d", stats.Total)
	}
	if stats.Inbound != 3 {
		t.Errorf("expected inbound=3, got %d", stats.Inbound)
	}
	if stats.Outbound != 2 {
		t.Errorf("expected outbound=2, got %d", stats.Outbound)
	}
}

func TestStats_Unread(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	for i := 0; i < 4; i++ {
		r := newReq()
		r.MsgID = fmt.Sprintf("unread-%d", i)
		m, _ := svc.Push(context.Background(), &r)
		if i == 0 {
			_ = svc.MarkRead(context.Background(), []uint{m.ID})
		}
	}
	stats, _ := svc.GetStats(context.Background(), nil, nil)
	if stats.Unread != 3 {
		t.Errorf("expected unread=3, got %d", stats.Unread)
	}
}

func TestStats_ByPlatform(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	for _, p := range []string{"wecom", "wecom", "douyin", "douyin", "douyin"} {
		r := newReq()
		r.Platform = p
		r.MsgID = fmt.Sprintf("p-%s-%d", p, time.Now().UnixNano())
		_, _ = svc.Push(context.Background(), &r)
	}
	stats, _ := svc.GetStats(context.Background(), nil, nil)
	if stats.ByPlatform["wecom"] != 2 {
		t.Errorf("expected wecom=2, got %d", stats.ByPlatform["wecom"])
	}
	if stats.ByPlatform["douyin"] != 3 {
		t.Errorf("expected douyin=3, got %d", stats.ByPlatform["douyin"])
	}
}

func TestStats_ByMsgType(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	for _, mt := range []string{"text", "text", "image", "file"} {
		r := newReq()
		r.MsgType = mt
		r.MsgID = fmt.Sprintf("mt-%s-%d", mt, time.Now().UnixNano())
		_, _ = svc.Push(context.Background(), &r)
	}
	stats, _ := svc.GetStats(context.Background(), nil, nil)
	if stats.ByMsgType["text"] != 2 {
		t.Errorf("expected text=2, got %d", stats.ByMsgType["text"])
	}
}

func TestStats_Recent24h(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	r1 := newReq()
	r1.MsgID = "r24-1"
	r1.SentAt = ptrTime(time.Now().Add(-25 * time.Hour))
	_, _ = svc.Push(context.Background(), &r1)
	r2 := newReq()
	r2.MsgID = "r24-2"
	_, _ = svc.Push(context.Background(), &r2)
	stats, _ := svc.GetStats(context.Background(), nil, nil)
	if stats.Recent24h != 1 {
		t.Errorf("expected recent24h=1, got %d", stats.Recent24h)
	}
}

func TestStats_TimeRange(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	r1 := newReq()
	r1.MsgID = "range-1"
	r1.SentAt = ptrTime(time.Now().Add(-2 * time.Hour))
	_, _ = svc.Push(context.Background(), &r1)
	r2 := newReq()
	r2.MsgID = "range-2"
	_, _ = svc.Push(context.Background(), &r2)
	start := time.Now().Add(-1 * time.Hour)
	end := time.Now()
	stats, _ := svc.GetStats(context.Background(), &start, &end)
	if stats.Total != 1 {
		t.Errorf("expected total=1 in range, got %d", stats.Total)
	}
}

func ptrTime(t time.Time) *time.Time { return &t }

func TestConsume_EmptyPartition(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	msg, err := svc.Consume(context.Background(), "wecom", "unknown-acc", 0)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if msg != nil {
		t.Error("expected nil for empty partition")
	}
}

func TestConsume_OrderBySentAt(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	for i := 3; i >= 0; i-- {
		r := newReq()
		r.MsgID = fmt.Sprintf("ord-c-%d", i)
		r.SentAt = ptrTime(time.Now().Add(time.Duration(i) * time.Second))
		_, _ = svc.Push(context.Background(), &r)
	}
	got, _ := svc.Consume(context.Background(), "wecom", "acc-001", 0)
	if got == nil {
		t.Fatal("expected message")
	}
}

func TestConsume_PartitionIsolation(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	r1 := newReq()
	r1.AccountID = "acc-A"
	r1.MsgID = "p-a"
	_, _ = svc.Push(context.Background(), &r1)
	r2 := newReq()
	r2.AccountID = "acc-B"
	r2.MsgID = "p-b"
	_, _ = svc.Push(context.Background(), &r2)
	sizeA := svc.Size(context.Background(), "wecom", "acc-A")
	sizeB := svc.Size(context.Background(), "wecom", "acc-B")
	if sizeA < 1 || sizeB < 1 {
		t.Errorf("expected both partitions to have messages, got %d/%d", sizeA, sizeB)
	}
}

func TestPeek_Empty(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	msg, err := svc.Peek(context.Background(), "wecom", "nope")
	if err != nil {
		t.Fatalf("peek: %v", err)
	}
	if msg != nil {
		t.Error("expected nil peek for empty")
	}
}

func TestPeek_NonDestructive(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	r := newReq()
	r.MsgID = "peek-1"
	_, _ = svc.Push(context.Background(), &r)
	_, _ = svc.Peek(context.Background(), "wecom", "acc-001")
	_, _ = svc.Peek(context.Background(), "wecom", "acc-001")
	if size := svc.Size(context.Background(), "wecom", "acc-001"); size < 1 {
		t.Errorf("expected peek to be non-destructive, size=%d", size)
	}
}

func TestSize_Empty(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	if size := svc.Size(context.Background(), "wecom", "none"); size != 0 {
		t.Errorf("expected 0, got %d", size)
	}
}

func TestConvertFromChannel_Basic(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	raw := &RawChannelMessage{
		Platform:       "wecom",
		AccountID:      "acc-c1",
		MsgID:          "raw-1",
		From:           "user-x",
		FromName:       "User X",
		To:             "acc-c1",
		ToName:         "Agent",
		Content:        "hi",
		MsgType:        "text",
		ConversationID: "conv-c1",
	}
	req := svc.ConvertFromChannel(context.Background(), raw)

	if req.Direction != "inbound" {
		t.Errorf("expected direction=inbound, got %s", req.Direction)
	}
	if req.SenderID != "user-x" {
		t.Errorf("expected sender_id=user-x, got %s", req.SenderID)
	}
}

func TestConvertFromChannel_DefaultMsgType(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	raw := &RawChannelMessage{Platform: "wecom", AccountID: "a", MsgID: "m", Content: "c"}
	req := svc.ConvertFromChannel(context.Background(), raw)
	if req.MsgType != "text" {
		t.Errorf("expected default text, got %s", req.MsgType)
	}
}

func TestConvertFromChannel_PreservesExtra(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	raw := &RawChannelMessage{
		Platform: "wecom", AccountID: "a", MsgID: "m", Content: "c",
		Extra: map[string]any{"k1": "v1", "n": 42},
	}
	req := svc.ConvertFromChannel(context.Background(), raw)
	if req.Extra["k1"] != "v1" {
		t.Error("expected extra preserved")
	}
	if req.Extra["n"] != 42 {
		t.Error("expected int extra preserved")
	}
}

func TestConvertFromChannel_AllPlatforms(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	for _, p := range ListPlatforms() {
		raw := &RawChannelMessage{Platform: p, AccountID: "a", MsgID: "m-" + p, Content: "c"}
		req := svc.ConvertFromChannel(context.Background(), raw)
		if req.Platform != p {
			t.Errorf("platform %s not preserved", p)
		}
	}
}

func TestMarshalUnmarshal(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	orig := &model.MessageHub{

		MsgID: "m1", Direction: "inbound", MsgType: "text", Content: "hi",
		Extra: model.JSONMap{"k": "v"},
	}
	data, err := svc.MarshalToJSON(context.Background(), orig)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if data == "" {
		t.Fatal("expected non-empty data")
	}
	got, err := svc.UnmarshalFromJSON(context.Background(), data)
	if err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Content != orig.Content {
		t.Errorf("expected content=%s, got %s", orig.Content, got.Content)
	}
}

func TestUnmarshal_InvalidJSON(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	_, err := svc.UnmarshalFromJSON(context.Background(), "{invalid")
	if err == nil {
		t.Error("expected error for invalid json")
	}
}

type testSub struct {
	mu     sync.Mutex
	calls  []*model.MessageHub
	filter func(*model.MessageHub) bool
}

func (s *testSub) OnMessage(_ context.Context, msg *model.MessageHub) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, msg)
	return nil
}

func (s *testSub) Filter(msg *model.MessageHub) bool {
	if s.filter == nil {
		return true
	}
	return s.filter(msg)
}

func (s *testSub) Count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.calls)
}

func TestSubscriber_ReceivesAll(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	sub := &testSub{}
	svc.Subscribe(context.Background(), sub)
	for i := 0; i < 3; i++ {
		r := newReq()
		r.MsgID = fmt.Sprintf("sub-%d", i)
		_, _ = svc.Push(context.Background(), &r)
	}
	time.Sleep(100 * time.Millisecond)
	if sub.Count() < 1 {
		t.Errorf("expected subscriber to receive messages, got %d", sub.Count())
	}
}

func TestSubscriber_Filtered(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	sub := &testSub{filter: func(m *model.MessageHub) bool { return m.Platform == "wecom" }}
	svc.Subscribe(context.Background(), sub)
	r1 := newReq()
	r1.MsgID = "f-1"
	r1.Platform = "wecom"
	_, _ = svc.Push(context.Background(), &r1)
	r2 := newReq()
	r2.MsgID = "f-2"
	r2.Platform = "douyin"
	_, _ = svc.Push(context.Background(), &r2)
	time.Sleep(100 * time.Millisecond)
	if sub.Count() != 1 {
		t.Errorf("expected 1 filtered message, got %d", sub.Count())
	}
}

func TestGenerateMsgID_Unique(t *testing.T) {
	seen := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := GenerateMsgID("wecom", "acc")
		if seen[id] {
			t.Fatalf("duplicate msg_id: %s", id)
		}
		seen[id] = true
	}
}

func TestGenerateMsgID_Format(t *testing.T) {
	id := GenerateMsgID("wecom", "acc-1")
	parts := strings.Split(id, "-")
	if len(parts) < 3 {
		t.Errorf("expected platform-account-uuid format, got %s", id)
	}
	if !strings.HasPrefix(id, "wecom-acc-1-") {
		t.Errorf("expected prefix wecom-acc-1-, got %s", id)
	}
}

func TestWithIdemTTL(t *testing.T) {
	_mc3 := cache.NewMemoryCache()
	defer _mc3.Close()
	svc := NewMessageHubServiceWithDB(nil, _mc3)
	svc.WithIdemTTL(context.Background(), 1*time.Hour)
	if svc.idemTTL != 1*time.Hour {
		t.Errorf("expected 1h, got %v", svc.idemTTL)
	}
}

func TestWithMaxContent(t *testing.T) {
	_mc4 := cache.NewMemoryCache()
	defer _mc4.Close()
	svc := NewMessageHubServiceWithDB(nil, _mc4)
	svc.WithMaxContent(context.Background(), 1000)
	if svc.maxContent != 1000 {
		t.Errorf("expected 1000, got %d", svc.maxContent)
	}
}

func TestWithQueueSize(t *testing.T) {
	_mc5 := cache.NewMemoryCache()
	defer _mc5.Close()
	svc := NewMessageHubServiceWithDB(nil, _mc5)
	svc.WithQueueSize(context.Background(), 500)
	if svc.streamSize != 500 {
		t.Errorf("expected 500, got %d", svc.streamSize)
	}
}

func TestPush_Concurrent(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	for i := 0; i < 20; i++ {
		r := newReq()
		r.MsgID = fmt.Sprintf("conc-%d", i)
		_, err := svc.Push(context.Background(), &r)
		if err != nil {
			t.Fatalf("push %d: %v", i, err)
		}
	}

	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, total, _ := svc.List(context.Background(), ListQuery{})
			if total != 20 {
				t.Errorf("expected 20 messages, got %d", total)
			}
		}()
	}
	wg.Wait()
}

func TestConsume_ConcurrentSafe(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	for i := 0; i < 10; i++ {
		r := newReq()
		r.AccountID = "cons-acc"
		r.MsgID = fmt.Sprintf("cons-%d", i)
		_, _ = svc.Push(context.Background(), &r)
	}
	var wg sync.WaitGroup
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _ = svc.Consume(context.Background(), "wecom", "cons-acc", 100*time.Millisecond)
		}()
	}
	wg.Wait()
}

func TestEndToEnd_PushListReadStats(t *testing.T) {
	svc, _ := newMessageHubTestService(t)

	var ids []uint
	for i := 0; i < 10; i++ {
		r := newReq()
		r.MsgID = fmt.Sprintf("e2e-%d", i)
		m, err := svc.Push(context.Background(), &r)
		if err != nil {
			t.Fatalf("push %d: %v", i, err)
		}
		ids = append(ids, m.ID)
	}
	list, total, _ := svc.List(context.Background(), ListQuery{})
	if total != 10 || len(list) != 10 {
		t.Errorf("expected 10/10, got %d/%d", total, len(list))
	}
	_ = svc.MarkRead(context.Background(), ids[:5])
	stats, _ := svc.GetStats(context.Background(), nil, nil)
	if stats.Total != 10 {
		t.Errorf("expected total=10, got %d", stats.Total)
	}
	if stats.Unread != 5 {
		t.Errorf("expected unread=5, got %d", stats.Unread)
	}
}

func TestEndToEnd_IdempotentPush(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	r := newReq()
	r.MsgID = "idem-e2e"
	for i := 0; i < 5; i++ {
		_, err := svc.Push(context.Background(), &r)
		if i == 0 {
			if err != nil {
				t.Fatalf("first push: %v", err)
			}
		} else {
			if err != ErrMessageHubIdempotent {
				t.Errorf("expected idempotent error, got %v", err)
			}
		}
	}
	_, total, _ := svc.List(context.Background(), ListQuery{})
	if total != 1 {
		t.Errorf("expected 1 message, got %d", total)
	}
}

func TestEndToEnd_FromChannel(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	raw := &RawChannelMessage{
		Platform: "wecom", AccountID: "agent-1", MsgID: "ch-msg-1",
		From: "customer-1", FromName: "Customer", Content: "我想要咨询",
		MsgType: "text", ConversationID: "conv-ch-1",
	}
	req := svc.ConvertFromChannel(context.Background(), raw)
	msg, err := svc.Push(context.Background(), req)
	if err != nil {
		t.Fatalf("push: %v", err)
	}
	if msg.SenderID != "customer-1" {
		t.Errorf("expected sender=customer-1, got %s", msg.SenderID)
	}
	if msg.Direction != "inbound" {
		t.Errorf("expected direction=inbound, got %s", msg.Direction)
	}
}

func TestPush_AllRequiredFieldsEmpty(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := PushMessageRequest{}
	_, err := svc.Push(context.Background(), &req)
	if err == nil {
		t.Error("expected error for empty request")
	}
}

func TestNormalize_ChinesePlatformName(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	req.Platform = "微信"
	_, err := svc.Normalize(context.Background(), &req)
	if err == nil {
		t.Error("expected error for chinese platform name")
	}
}

func TestNormalize_UppercasePlatform(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	req.Platform = "WECOM"
	_, err := svc.Normalize(context.Background(), &req)
	if err == nil {
		t.Error("expected error for uppercase platform (case sensitive)")
	}
}

func TestNormalize_DefaultSentAt(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	before := time.Now()
	req := newReq()
	msg, _ := svc.Normalize(context.Background(), &req)
	after := time.Now()
	if msg.SentAt.Before(before) || msg.SentAt.After(after) {
		t.Errorf("expected sent_at between %v and %v, got %v", before, after, msg.SentAt)
	}
}

func TestNormalize_GroupMessage(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	req.IsGroup = true
	req.GroupID = "group-001"
	msg, _ := svc.Normalize(context.Background(), &req)
	if !msg.IsGroup {
		t.Error("expected is_group=true")
	}
	if msg.GroupID != "group-001" {
		t.Errorf("expected group_id=group-001, got %s", msg.GroupID)
	}
}

func TestNormalize_AIReply(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	req := newReq()
	req.Direction = "outbound"
	req.IsAIReply = true
	req.AIAgent = "sales_champion_v1"
	msg, _ := svc.Normalize(context.Background(), &req)
	if !msg.IsAIReply {
		t.Error("expected is_ai_reply=true")
	}
	if msg.AIAgent != "sales_champion_v1" {
		t.Errorf("expected ai_agent=sales_champion_v1, got %s", msg.AIAgent)
	}
}

func TestConsume_WithDBFallback(t *testing.T) {
	svc, db := newMessageHubTestService(t)
	db.Create(&model.MessageHub{
		MsgID:     "db-1",
		Platform:  "wecom",
		AccountID: "db-acc",
		Direction: "inbound",
		MsgType:   "text",
		Content:   "from db",
		SentAt:    time.Now(),
	})
	msg, err := svc.Consume(context.Background(), "wecom", "db-acc", 0)
	if err != nil {
		t.Fatalf("consume: %v", err)
	}
	if msg == nil || msg.MsgID != "db-1" {
		t.Errorf("expected msg from db, got %v", msg)
	}
}

func TestConsume_ContextCancel(t *testing.T) {
	svc, _ := newMessageHubTestService(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := svc.Consume(ctx, "wecom", "cancel-acc", 1*time.Second)
	if err == nil {
		t.Error("expected context cancellation error")
	}
}
