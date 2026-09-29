package bridge

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

func TestAckBridgeOutbox_DetailedItems_P3D(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := service.NewInboxIngressServiceWithDB(db, nil)
	h := NewBridgeIngestHandlerWithMock(
		func(ctx context.Context, ev *model.MessageEvent) (*service.InboxIngressResult, error) {
			return &service.InboxIngressResult{Accepted: true}, nil
		},
		func(ctx context.Context, ev *model.MessageEvent, direction string) error { return nil },
	)
	h.ingress = svc

	const (
		// channel 走 HTTP 入参（别名），platform 是 message_hub 里唯一可能出现的形态（规范名）：
		// 二者故意不同名 ⇒ 这一格同时锁住"ack 入口必须先把别名归一，否则一行都匹配不上"。
		channel   = "douyin_web"
		platform  = "douyin"
		accountID = "acc_p3d_1"
		conv      = "conv_p3d"
	)
	for _, c := range []string{"msg_a content", "msg_b content", "msg_c content"} {
		hub := &model.MessageHub{
			Platform:       platform,
			AccountID:      accountID,
			ConversationID: conv,
			MsgID:          "mh:" + c,
			MsgType:        "text",
			Content:        c,
			Direction:      "outbound",
			Status:         "pending",
		}
		if err := db.Create(hub).Error; err != nil {
			t.Fatalf("seed 失败: %v", err)
		}
	}

	if n, err := svc.AckOutboundDelivered(context.Background(), platform, accountID, []string{"mh:msg_b content"}); err != nil || n != 1 {
		t.Fatalf("首次 ack msg_b 应返回 1，实际 (%d, %v)", n, err)
	}

	body := `{"msg_ids":["mh:msg_a content","mh:msg_b content","mh:msg_c content","mh:msg_d content"],"status":"delivered"}`
	req := httptest.NewRequest("POST", "/api/bridge/outbox/ack?channel="+channel+"&account_id="+accountID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = req
	h.AckBridgeOutbox(c)
	if rr.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", rr.Code, rr.Body.String())
	}

	respStr := rr.Body.String()
	expects := []string{
		`"status":"ok"`,
		`"affected_count":2`,
		`"acked_items_count":2`,
		`"duplicate_count":1`,
		`"not_found_count":1`,
	}
	for _, e := range expects {
		if !strings.Contains(respStr, e) {
			t.Errorf("响应缺少 %q\n实际响应: %s", e, respStr)
		}
	}
	perMsgChecks := []string{
		`"msg_id":"mh:msg_a content","status":"acked"`,
		`"msg_id":"mh:msg_b content","status":"duplicate"`,
		`"msg_id":"mh:msg_c content","status":"acked"`,
		`"msg_id":"mh:msg_d content","status":"not_found"`,
	}
	for _, ck := range perMsgChecks {
		if !strings.Contains(respStr, ck) {
			t.Errorf("响应缺少 per-msg-id 详情 %q\n实际响应: %s", ck, respStr)
		}
	}
}

func TestAckBridgeOutbox_TooManyMsgIDs_P3D(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := service.NewInboxIngressServiceWithDB(db, nil)
	h := NewBridgeIngestHandlerWithMock(nil, nil)
	h.ingress = svc

	ids := make([]string, 501)
	for i := range ids {
		ids[i] = `mh:test`
	}
	body := `{"msg_ids":["` + strings.Join(ids, `","`) + `"],"status":"delivered"}`
	req := httptest.NewRequest("POST", "/api/bridge/outbox/ack?channel=douyin_web&account_id=acc", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = req
	h.AckBridgeOutbox(c)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("期望 400，实际 %d", rr.Code)
	}
	body2 := rr.Body.String()
	if !strings.Contains(body2, "too many msg_ids") {
		t.Errorf("响应缺少 'too many msg_ids' 提示: %s", body2)
	}
}

func TestAckOutboundDeliveredDetailed_CrossSession_P3D(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := service.NewInboxIngressServiceWithDB(db, nil)
	ctx := context.Background()
	const (
		channel   = "douyin_web"
		accountID = "acc_cross"
		content   = "cross session content"
	)
	for _, c := range []string{"conv_1", "conv_2"} {
		h := &model.MessageHub{
			Platform:       channel,
			AccountID:      accountID,
			ConversationID: c,
			MsgID:          "mh:cross",
			MsgType:        "text",
			Content:        content,
			Direction:      "outbound",
			Status:         "pending",
		}
		if err := db.Create(h).Error; err != nil {
			t.Fatalf("seed 失败: %v", err)
		}
	}
	res, err := svc.AckOutboundDeliveredDetailed(ctx, channel, accountID, []string{"mh:cross"}, "", "delivered", nil)
	if err != nil {
		t.Fatalf("AckOutboundDeliveredDetailed: %v", err)
	}
	if res.AffectedCount != 2 {
		t.Errorf("期望 affected=2（两条跨会话），实际 %d", res.AffectedCount)
	}
	if res.DuplicateCount != 0 {
		t.Errorf("期望 duplicate=0，实际 %d", res.DuplicateCount)
	}
	if res.NotFoundCount != 0 {
		t.Errorf("期望 not_found=0，实际 %d", res.NotFoundCount)
	}
	if len(res.Items) != 1 {
		t.Errorf("期望 items=1，实际 %d", len(res.Items))
	} else if res.Items[0].Status != "acked" {
		t.Errorf("期望 status=acked，实际 %s", res.Items[0].Status)
	}
}

// TestGetByMsgIDsInScope_OwnershipIsolation_P3D 验证 GetByMsgIDsInScope 严格按 (platform, account_id) 隔离。
func TestGetByMsgIDsInScope_OwnershipIsolation_P3D(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	hubRepo := repository.NewMessageHubRepositoryWithDB(db)
	ctx := context.Background()
	for _, c := range []struct {
		acc, msg string
	}{
		{"acc_A", "mh:msgA"},
		{"acc_B", "mh:msgB"},
	} {
		h := &model.MessageHub{
			Platform:       "douyin_web",
			AccountID:      c.acc,
			ConversationID: "conv",
			MsgID:          c.msg,
			MsgType:        "text",
			Content:        c.msg,
			Direction:      "outbound",
			Status:         "pending",
		}
		if err := db.Create(h).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	rows, err := hubRepo.GetByMsgIDsInScope(ctx, "douyin_web", "acc_A", []string{"mh:msgA", "mh:msgB"})
	if err != nil {
		t.Fatalf("GetByMsgIDsInScope: %v", err)
	}
	if len(rows) != 1 {
		t.Errorf("期望 1 条（只 accountA 的 msgA），实际 %d", len(rows))
	}
	if len(rows) > 0 && rows[0].MsgID != "mh:msgA" {
		t.Errorf("期望 msgA，实际 %s", rows[0].MsgID)
	}
}

func TestAckOutboundDeliveredDetailed_ConcurrentDoubleAck_P4(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := service.NewInboxIngressServiceWithDB(db, nil)
	ctx := context.Background()
	const (
		channel   = "douyin_web"
		accountID = "acc_race"
		msgID     = "mh:race_msg"
	)
	hub := &model.MessageHub{
		Platform:       channel,
		AccountID:      accountID,
		ConversationID: "conv_race",
		MsgID:          msgID,
		MsgType:        "text",
		Content:        "race content",
		Direction:      "outbound",
		Status:         "pending",
	}
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	const N = 10
	results := make([]*service.AckOutboundResult, N)
	errs := make([]error, N)
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(idx int) {
			defer wg.Done()
			r, e := svc.AckOutboundDeliveredDetailed(ctx, channel, accountID, []string{msgID}, "", "delivered", nil)
			results[idx] = r
			errs[idx] = e
		}(i)
	}
	wg.Wait()
	totalAcked := 0
	totalDup := 0
	for i, r := range results {
		if errs[i] != nil {
			t.Errorf("worker %d err: %v", i, errs[i])
			continue
		}
		if r == nil {
			t.Errorf("worker %d nil result", i)
			continue
		}
		totalAcked += r.AckedItemsCount
		totalDup += r.DuplicateCount
	}
	if totalAcked != 1 {
		t.Errorf("期望总 acked_items_count=1（仅 1 个真正翻转），实际 %d", totalAcked)
	}
	if totalDup != N-1 {
		t.Errorf("期望总 duplicate=%d（其余 9 个幂等跳过），实际 %d", N-1, totalDup)
	}
}

func TestAckOutboundDeliveredDetailed_HubRepoNil_P4(t *testing.T) {
	svc := service.NewInboxIngressServiceWithDB(nil, nil)
	r, err := svc.AckOutboundDeliveredDetailed(context.Background(), "douyin", "acc", []string{"m1"}, "", "delivered", nil)
	if err == nil {
		t.Fatalf("hubRepo nil 应返回 error，实际 (result=%+v, err=nil)", r)
	}
	if r != nil {
		t.Errorf("hubRepo nil 应返 nil result，实际 %+v", r)
	}
}

func TestAckOutboundDeliveredDetailed_DuplicateMsgIDInput_P4(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := service.NewInboxIngressServiceWithDB(db, nil)
	ctx := context.Background()
	hub := &model.MessageHub{
		Platform:       "douyin",
		AccountID:      "acc_dup",
		ConversationID: "c",
		MsgID:          "mh:dup",
		MsgType:        "text",
		Content:        "dup content",
		Direction:      "outbound",
		Status:         "pending",
	}
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	r, err := svc.AckOutboundDeliveredDetailed(ctx, "douyin", "acc_dup", []string{"mh:dup", "mh:dup", "mh:dup"}, "", "delivered", nil)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if len(r.Items) != 1 {
		t.Errorf("期望 items=1（去重后），实际 %d", len(r.Items))
	}
	if r.AckedItemsCount != 1 {
		t.Errorf("期望 acked_items_count=1，实际 %d", r.AckedItemsCount)
	}
}

func TestGetByMsgIDsInScope_OnlyOutbound_P4(t *testing.T) {
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	hubRepo := repository.NewMessageHubRepositoryWithDB(db)
	ctx := context.Background()
	for _, c := range []struct {
		direction, msg, conv string
	}{

		{"inbound", "mh:shared", "c_in"},
		{"outbound", "mh:shared", "c"},
	} {
		h := &model.MessageHub{
			Platform:       "douyin",
			AccountID:      "acc_d",
			ConversationID: c.conv,
			MsgID:          c.msg,
			MsgType:        "text",
			Content:        c.msg,
			Direction:      c.direction,
			Status:         "pending",
		}
		if err := db.Create(h).Error; err != nil {
			t.Fatalf("seed: %v", err)
		}
	}
	rows, err := hubRepo.GetByMsgIDsInScope(ctx, "douyin", "acc_d", []string{"mh:shared"})
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	outboundCount := 0
	for _, h := range rows {
		if h.Direction == "outbound" {
			outboundCount++
		}
	}
	if outboundCount != 1 {
		t.Errorf("期望仅返回 1 条 outbound，实际 %d 条（rows=%+v）", outboundCount, rows)
	}
}

// v2 items[] 的 status 是逐项带的，入口那道 v1 status 校验拦不到它。修复前它一路走到 service
// 报错回 500，而 v2 是按 (conversation_id, status) 分组后遍历 map 逐组落库，遍历序随机 ⇒
// 实测同一个请求 20 次里 15 次把好项落了库、5 次一格没落，客户端只看到一个不带原因的 500。
// 这一格锁住两件事：整批在任何写入之前被 400 拒掉，且报错文案点名到底是哪个值不认。
func TestAckBridgeOutbox_V2UnknownStatus_RejectsWholeBatchBeforeWrite_P3D(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := service.NewInboxIngressServiceWithDB(db, nil)
	h := NewBridgeIngestHandlerWithMock(nil, nil)
	h.ingress = svc

	const (
		channel   = "douyin"
		accountID = "acc_v2_bad_status"
	)
	seed := []struct{ msg, conv string }{
		{"mh:v2bs_a", "conv_v2bs_1"},
		{"mh:v2bs_b", "conv_v2bs_2"},
		{"mh:v2bs_c", "conv_v2bs_1"},
	}
	for _, s := range seed {
		hub := &model.MessageHub{
			Platform:       channel,
			AccountID:      accountID,
			ConversationID: s.conv,
			MsgID:          s.msg,
			MsgType:        "text",
			Content:        "v2 bad status content " + s.msg,
			Direction:      "outbound",
			Status:         "pending",
		}
		if err := db.Create(hub).Error; err != nil {
			t.Fatalf("seed %s 失败: %v", s.msg, err)
		}
	}

	// 两个合法项分布在两个 conversation_id（两个分组）+ 一个未知状态项
	body := `{"v":2,"items":[` +
		`{"msg_id":"mh:v2bs_a","conversation_id":"conv_v2bs_1","status":"delivered"},` +
		`{"msg_id":"mh:v2bs_b","conversation_id":"conv_v2bs_2","status":"delivered"},` +
		`{"msg_id":"mh:v2bs_c","conversation_id":"conv_v2bs_1","status":"shipped"}]}`
	req := httptest.NewRequest("POST", "/api/bridge/outbox/ack?channel="+channel+"&account_id="+accountID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = req
	h.AckBridgeOutbox(c)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("期望 400（入参错误），实际 %d: %s", rr.Code, rr.Body.String())
	}
	respStr := rr.Body.String()
	// 响应体里值是带 JSON 转义的（\"shipped\"），断言只取裸词，不把转义形状写死
	for _, want := range []string{`"status":"error"`, `invalid status`, `shipped`} {
		if !strings.Contains(respStr, want) {
			t.Errorf("响应缺少 %q\n实际响应: %s", want, respStr)
		}
	}

	// 整批没落：三行都还停在 pending（合法项也不许被单独收口，否则响应说失败、库里却变了）
	var changed int64
	if err := db.Model(&model.MessageHub{}).
		Where("account_id = ? AND status <> ?", accountID, "pending").
		Count(&changed).Error; err != nil {
		t.Fatalf("统计失败: %v", err)
	}
	if changed != 0 {
		t.Errorf("未知状态请求后仍有 %d 行被改写，期望 0（整批拒在写之前）", changed)
	}

	// 同一个请求重复 20 次，读数必须恒定（修复前这里是 15/5 摆动）
	for i := 0; i < 20; i++ {
		req := httptest.NewRequest("POST", "/api/bridge/outbox/ack?channel="+channel+"&account_id="+accountID, strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		rr := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rr)
		c.Request = req
		h.AckBridgeOutbox(c)
		if rr.Code != http.StatusBadRequest {
			t.Fatalf("第 %d 次重放期望恒 400，实际 %d: %s", i, rr.Code, rr.Body.String())
		}
	}
	if err := db.Model(&model.MessageHub{}).
		Where("account_id = ? AND status <> ?", accountID, "pending").
		Count(&changed).Error; err != nil {
		t.Fatalf("重放后统计失败: %v", err)
	}
	if changed != 0 {
		t.Errorf("重放 20 次后有 %d 行被改写，期望 0", changed)
	}
}
