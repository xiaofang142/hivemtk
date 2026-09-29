package bridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/service"

	"github.com/gin-gonic/gin"
)

func TestAckOutboundDeliveredDetailed_FailedStatus_P0_3(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := service.NewInboxIngressServiceWithDB(db, nil)
	ctx := context.Background()

	const (
		channel   = "douyin_web"
		accountID = "acc_failed"
	)
	hub := &model.MessageHub{
		Platform:       channel,
		AccountID:      accountID,
		ConversationID: "conv_fail",
		MsgID:          "mh:fail_1",
		MsgType:        "text",
		Content:        "fail content",
		Direction:      "outbound",
		Status:         "pending",
	}
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	res, err := svc.AckOutboundDeliveredDetailed(ctx, channel, accountID, []string{"mh:fail_1"}, "", "failed", nil)
	if err != nil {
		t.Fatalf("AckOutboundDeliveredDetailed: %v", err)
	}
	if res.FailedItemsCount != 1 {
		t.Errorf("期望 failed_items_count=1，实际 %d", res.FailedItemsCount)
	}
	if res.AckedItemsCount != 0 {
		t.Errorf("期望 acked_items_count=0，实际 %d", res.AckedItemsCount)
	}
	if len(res.Items) != 1 || res.Items[0].Status != "failed" {
		t.Errorf("期望 items[0].status=failed，实际 %+v", res.Items)
	}

	var got model.MessageHub
	if err := db.Where("msg_id = ?", "mh:fail_1").First(&got).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if got.Status != "failed" {
		t.Errorf("DB status 应为 failed，实际 %s", got.Status)
	}
}

func TestAckOutboundDeliveredDetailed_ConversationIDFilter_P0_1(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := service.NewInboxIngressServiceWithDB(db, nil)
	ctx := context.Background()

	const (
		channel   = "douyin_web"
		accountID = "acc_conv"
		msgID     = "mh:shared_msg"
	)
	for _, conv := range []string{"conv_A", "conv_B"} {
		h := &model.MessageHub{
			Platform:       channel,
			AccountID:      accountID,
			ConversationID: conv,
			MsgID:          msgID,
			MsgType:        "text",
			Content:        "shared content",
			Direction:      "outbound",
			Status:         "pending",
		}
		if err := db.Create(h).Error; err != nil {
			t.Fatalf("seed %s 失败: %v", conv, err)
		}
	}
	res, err := svc.AckOutboundDeliveredDetailed(ctx, channel, accountID, []string{msgID}, "conv_A", "delivered", nil)
	if err != nil {
		t.Fatalf("AckOutboundDeliveredDetailed: %v", err)
	}
	if res.AffectedCount != 1 {
		t.Errorf("期望 affected=1（仅 conv_A），实际 %d", res.AffectedCount)
	}

	var a, b model.MessageHub
	if err := db.Where("conversation_id = ?", "conv_A").First(&a).Error; err != nil {
		t.Fatalf("查 conv_A: %v", err)
	}
	if a.Status != "delivered" {
		t.Errorf("conv_A 应为 delivered，实际 %s", a.Status)
	}
	if err := db.Where("conversation_id = ?", "conv_B").First(&b).Error; err != nil {
		t.Fatalf("查 conv_B: %v", err)
	}
	if b.Status != "pending" {
		t.Errorf("conv_B 应保持 pending，实际 %s", b.Status)
	}
}

func TestAckOutboundDeliveredDetailed_NotInScope_P0_8(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := service.NewInboxIngressServiceWithDB(db, nil)
	ctx := context.Background()

	const (
		channel    = "douyin_web"
		accountA   = "acc_A"
		accountB   = "acc_B"
		msgInOther = "mh:other_account_msg"
		msgMissing = "mh:truly_missing"
	)
	h := &model.MessageHub{
		Platform:       channel,
		AccountID:      accountB,
		ConversationID: "conv_B",
		MsgID:          msgInOther,
		MsgType:        "text",
		Content:        "owned by B",
		Direction:      "outbound",
		Status:         "pending",
	}
	if err := db.Create(h).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	res, err := svc.AckOutboundDeliveredDetailed(ctx, channel, accountA, []string{msgInOther, msgMissing}, "", "delivered", nil)
	if err != nil {
		t.Fatalf("AckOutboundDeliveredDetailed: %v", err)
	}
	if res.NotInScopeCount != 1 {
		t.Errorf("期望 not_in_scope_count=1（msgInOther 在 B 名下），实际 %d", res.NotInScopeCount)
	}
	if res.NotFoundCount != 1 {
		t.Errorf("期望 not_found_count=1（msgMissing 真不存在），实际 %d", res.NotFoundCount)
	}
	statusByMsgID := map[string]string{}
	for _, it := range res.Items {
		statusByMsgID[it.MsgID] = it.Status
	}
	if statusByMsgID[msgInOther] != "not_in_scope" {
		t.Errorf("msgInOther 应为 not_in_scope，实际 %s", statusByMsgID[msgInOther])
	}
	if statusByMsgID[msgMissing] != "not_found" {
		t.Errorf("msgMissing 应为 not_found，实际 %s", statusByMsgID[msgMissing])
	}
}

func TestAckOutboundDeliveredDetailed_ErrorField_P0_2(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := service.NewInboxIngressServiceWithDB(db, nil)
	ctx := context.Background()

	const (
		channel   = "douyin_web"
		accountID = "acc_err"
	)
	hub := &model.MessageHub{
		Platform:       channel,
		AccountID:      accountID,
		ConversationID: "conv_err",
		MsgID:          "mh:err",
		MsgType:        "text",
		Content:        "err content",
		Direction:      "outbound",
		Status:         "pending",
	}
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	perItem := map[string]service.BridgeOutboundAckInput{
		"mh:err": {MsgID: "mh:err", Status: "failed", Error: "send_timeout"},
	}
	res, err := svc.AckOutboundDeliveredDetailed(ctx, channel, accountID, []string{"mh:err"}, "", "failed", perItem)
	if err != nil {
		t.Fatalf("AckOutboundDeliveredDetailed: %v", err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("期望 1 item，实际 %d", len(res.Items))
	}
	if res.Items[0].Error != "send_timeout" {
		t.Errorf("期望 Error=send_timeout，实际 %s", res.Items[0].Error)
	}
	b, _ := json.Marshal(res.Items[0])
	if !strings.Contains(string(b), `"error":"send_timeout"`) {
		t.Errorf("JSON 序列化未含 error 字段: %s", string(b))
	}
}

func TestAckBridgeOutbox_V2Protocol_P0_1(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := service.NewInboxIngressServiceWithDB(db, nil)
	h := NewBridgeIngestHandlerWithMock(nil, nil)
	h.ingress = svc

	const (
		channel   = "douyin_web"
		accountID = "acc_v2"
	)
	hub := &model.MessageHub{
		Platform:       channel,
		AccountID:      accountID,
		ConversationID: "conv_v2",
		MsgID:          "mh:v2_test",
		MsgType:        "text",
		Content:        "v2 content",
		Direction:      "outbound",
		Status:         "pending",
	}
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	body := `{"v":2,"items":[{"msg_id":"mh:v2_test"}],"status":"delivered"}`
	req := httptest.NewRequest("POST", "/api/bridge/outbox/ack?channel="+channel+"&account_id="+accountID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = req
	h.AckBridgeOutbox(c)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("期望 400（v2 items[].conversation_id 必填），实际 %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), "conversation_id required") {
		t.Errorf("响应缺 'conversation_id required' 提示: %s", rr.Body.String())
	}
	body2 := `{"v":2,"items":[{"msg_id":"mh:v2_test","conversation_id":"conv_v2","status":"delivered"}]}`
	req2 := httptest.NewRequest("POST", "/api/bridge/outbox/ack?channel="+channel+"&account_id="+accountID, strings.NewReader(body2))
	req2.Header.Set("Content-Type", "application/json")
	rr2 := httptest.NewRecorder()
	c2, _ := gin.CreateTestContext(rr2)
	c2.Request = req2
	h.AckBridgeOutbox(c2)
	if rr2.Code != http.StatusOK {
		t.Fatalf("v2 完整请求期望 200，实际 %d: %s", rr2.Code, rr2.Body.String())
	}
	var resp map[string]any
	if err := json.Unmarshal(rr2.Body.Bytes(), &resp); err != nil {
		t.Fatalf("解析响应失败: %v", err)
	}
	if resp["status"] != "ok" {
		t.Errorf("期望 status=ok，实际 %v", resp["status"])
	}
	if _, ok := resp["acked_items_count"]; !ok {
		t.Errorf("响应缺 acked_items_count 字段")
	}
	if _, ok := resp["not_in_scope_count"]; !ok {
		t.Errorf("响应缺 not_in_scope_count 字段（P0-8）")
	}
	if _, ok := resp["failed_items_count"]; !ok {
		t.Errorf("响应缺 failed_items_count 字段（P0-3）")
	}
}

func TestAckBridgeOutbox_FailedStatus_P0_3(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := service.NewInboxIngressServiceWithDB(db, nil)
	h := NewBridgeIngestHandlerWithMock(nil, nil)
	h.ingress = svc

	const (
		channel   = "douyin_web"
		accountID = "acc_h_failed"
	)
	hub := &model.MessageHub{
		// 库里只可能是规范名（ingest 入口归一后才落 message_hub）；下面的 HTTP 请求仍带
		// 别名 douyin_web ⇒ 这一格同时锁住"ack 入口必须把别名归一后才能匹配上行"。
		Platform:       "douyin",
		AccountID:      accountID,
		ConversationID: "conv_h",
		MsgID:          "mh:h_failed",
		MsgType:        "text",
		Content:        "h failed content",
		Direction:      "outbound",
		Status:         "pending",
	}
	if err := db.Create(hub).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	body := `{"msg_ids":["mh:h_failed"],"status":"failed"}`
	req := httptest.NewRequest("POST", "/api/bridge/outbox/ack?channel="+channel+"&account_id="+accountID, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = req
	h.AckBridgeOutbox(c)
	if rr.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", rr.Code, rr.Body.String())
	}
	if !strings.Contains(rr.Body.String(), `"failed_items_count":1`) {
		t.Errorf("响应缺 failed_items_count=1: %s", rr.Body.String())
	}

	var got model.MessageHub
	if err := db.Where("msg_id = ?", "mh:h_failed").First(&got).Error; err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if got.Status != "failed" {
		t.Errorf("DB status 应为 failed，实际 %s", got.Status)
	}
}

func TestAckBridgeOutbox_InvalidStatus_P0_3(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewTestDB(t, &model.MessageHub{})
	svc := service.NewInboxIngressServiceWithDB(db, nil)
	h := NewBridgeIngestHandlerWithMock(nil, nil)
	h.ingress = svc
	body := `{"msg_ids":["m1"],"status":"weird"}`
	req := httptest.NewRequest("POST", "/api/bridge/outbox/ack?channel=douyin_web&account_id=acc", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = req
	h.AckBridgeOutbox(c)
	if rr.Code != http.StatusBadRequest {
		t.Fatalf("期望 400（非法 status），实际 %d", rr.Code)
	}
}

func TestAckOutboundDeliveredDetailed_InvalidStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewTestDB(t, &model.MessageHub{})
	svc := service.NewInboxIngressServiceWithDB(db, nil)
	_, err := svc.AckOutboundDeliveredDetailed(context.Background(), "douyin", "acc", []string{"m1"}, "", "weird", nil)
	if err == nil {
		t.Fatalf("期望返回 error（非法 status），实际 nil")
	}
}

func TestAckOutboundDeliveredDetailed_CrossAccountProbe_NotInScope_P0_6(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := service.NewInboxIngressServiceWithDB(db, nil)
	ctx := context.Background()

	if err := db.Create(&model.MessageHub{
		Platform:       "douyin_web",
		AccountID:      "acc_B",
		ConversationID: "conv_B",
		MsgID:          "m_probe",
		MsgType:        "text",
		Content:        "B content",
		Direction:      "outbound",
		Status:         "pending",
	}).Error; err != nil {
		t.Fatalf("seed B 失败: %v", err)
	}

	res, err := svc.AckOutboundDeliveredDetailed(ctx, "douyin_web", "acc_A", []string{"m_probe"}, "", "delivered", nil)
	if err != nil {
		t.Fatalf("A 探测失败: %v", err)
	}
	if res.NotInScopeCount != 1 {
		t.Errorf("期望 not_in_scope_count=1，实际 %d（items=%+v）", res.NotInScopeCount, res.Items)
	}
	if res.NotFoundCount != 0 {
		t.Errorf("期望 not_found_count=0（已分类为 not_in_scope），实际 %d", res.NotFoundCount)
	}
	if len(res.Items) != 1 || res.Items[0].Status != "not_in_scope" {
		t.Errorf("期望 items[0].status=not_in_scope，实际 %+v", res.Items)
	}

	var b model.MessageHub
	if err := db.Where("msg_id = ?", "m_probe").First(&b).Error; err != nil {
		t.Fatalf("查询 B 失败: %v", err)
	}
	if b.Status != "pending" {
		t.Errorf("B 的行不应被 A 探测操作影响，期望 pending，实际 %s", b.Status)
	}
}

func TestAckOutboundDeliveredDetailed_RealNotFound_NoLeak_P0_6(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := service.NewInboxIngressServiceWithDB(db, nil)
	ctx := context.Background()

	res, err := svc.AckOutboundDeliveredDetailed(ctx, "douyin_web", "acc_A", []string{"m_truly_missing"}, "", "delivered", nil)
	if err != nil {
		t.Fatalf("ack failed: %v", err)
	}
	if res.NotInScopeCount != 0 {
		t.Errorf("期望 not_in_scope_count=0（真不存在），实际 %d", res.NotInScopeCount)
	}
	if res.NotFoundCount != 1 {
		t.Errorf("期望 not_found_count=1，实际 %d", res.NotFoundCount)
	}
	if len(res.Items) != 1 || res.Items[0].Status != "not_found" {
		t.Errorf("期望 items[0].status=not_found，实际 %+v", res.Items)
	}
}

func TestAckBridgeOutbox_CrossAccountProbe_NotInScope_P0_6(t *testing.T) {
	gin.SetMode(gin.TestMode)
	db := testutil.NewTestDB(t, &model.MessageHub{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	svc := service.NewInboxIngressServiceWithDB(db, nil)
	h := NewBridgeIngestHandlerWithMock(nil, nil)
	h.ingress = svc

	if err := db.Create(&model.MessageHub{
		// 规范名入库才是现实形态（ingest 入口把别名归一后才落 message_hub）。
		// 下面请求仍带别名 douyin_web ⇒ 这一格顺带锁住"ack 入口也必须归一"。
		Platform:       "douyin",
		AccountID:      "acc_B",
		ConversationID: "conv_B",
		MsgID:          "m_probe_http",
		MsgType:        "text",
		Content:        "B content",
		Direction:      "outbound",
		Status:         "pending",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}

	body := `{"msg_ids":["m_probe_http"],"status":"delivered"}`
	req := httptest.NewRequest("POST", "/api/bridge/outbox/ack?channel=douyin_web&account_id=acc_A", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rr := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rr)
	c.Request = req
	h.AckBridgeOutbox(c)
	if rr.Code != http.StatusOK {
		t.Fatalf("期望 200，实际 %d: %s", rr.Code, rr.Body.String())
	}

	if !strings.Contains(rr.Body.String(), `"not_in_scope_count":1`) {
		t.Errorf("响应缺 not_in_scope_count=1: %s", rr.Body.String())
	}
	if strings.Contains(rr.Body.String(), "acc_B") {
		t.Errorf("响应不应泄露目标归属账号（B 的 account_id 不应出现在 A 的响应中）")
	}

	var b model.MessageHub
	if err := db.Where("msg_id = ?", "m_probe_http").First(&b).Error; err != nil {
		t.Fatalf("查询 B 失败: %v", err)
	}
	if b.Status != "pending" {
		t.Errorf("B 的行不应被 A 操作影响，期望 pending，实际 %s", b.Status)
	}
}
