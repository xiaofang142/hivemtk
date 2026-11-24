// 渠道别名的三入口一致性：ingest / outbox / ack 必须按同一个名字认账号。
//
// `message_hub.platform` 只存规范名（ingest 入口就归一了），所以任何还带着别名
// （xhs / douyin_web / kuaishou_web / …）的调用方，一旦 ack 侧不归一：
//
//	认领到的行永远匹配不上 → 每格报 not_found 却仍回 HTTP 200 status:ok
//	→ 那一行留在"欠交付"集合里 → 30s 认领租约到期后同一条回复被再次下发
//	→ 客户每隔半分钟收到一条一模一样的 AI 回复，直到 20 次预算用完、落 failed。
//
// 实测（2026-09-28，活实例 + 别名 douyin_web）：第 1 次下发 → 别名 ack
// `acked_items_count:0 not_found_count:1` → 行停在 inflight → 35s 后同一条又被下发。
//
// 这里断言的第 3 步（租约到期后复查）就是那把有牙的尺子：不归一的话它会拿到 1 条。
package bridge

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type outboxHTTPResult struct {
	status   int
	messages []struct {
		MsgID string `json:"msg_id"`
	}
	ackedItemsCount int
	notFoundCount   int
	body            string
}

// pollOutbox 走真实 HTTP 入口取待下行（别名入参原样传，归不归一是被测量）。
func pollOutbox(t *testing.T, h *BridgeIngestHandler, identifyCode, channel, accountID string) outboxHTTPResult {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("GET",
		"/api/bridge/outbox?channel="+channel+"&account_id="+accountID+"&limit=50", nil)
	req.Header.Set("X-Channel-Identify-Code", identifyCode)
	req = req.WithContext(localBridgeDeliveryContext(t, tenant.WithTenantID(context.Background(), 1), NormalizeBridgeChannel(channel), accountID, "text", ""))
	c, _ := gin.CreateTestContext(rr)
	c.Request = req
	h.GetBridgeOutbox(c)

	var parsed struct {
		Status   string `json:"status"`
		Messages []struct {
			MsgID string `json:"msg_id"`
		} `json:"messages"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("outbox 响应解析失败: %v body=%s", err, rr.Body.String())
	}
	return outboxHTTPResult{status: rr.Code, messages: parsed.Messages, body: rr.Body.String()}
}

func ackOutbox(t *testing.T, h *BridgeIngestHandler, identifyCode, channel, accountID, body string) outboxHTTPResult {
	t.Helper()
	rr := httptest.NewRecorder()
	req := httptest.NewRequest("POST",
		"/api/bridge/outbox/ack?channel="+channel+"&account_id="+accountID,
		strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Channel-Identify-Code", identifyCode)
	c, _ := gin.CreateTestContext(rr)
	c.Request = req
	h.AckBridgeOutbox(c)

	var parsed struct {
		AckedItemsCount int `json:"acked_items_count"`
		NotFoundCount   int `json:"not_found_count"`
	}
	if err := json.Unmarshal(rr.Body.Bytes(), &parsed); err != nil {
		t.Fatalf("ack 响应解析失败: %v body=%s", err, rr.Body.String())
	}
	return outboxHTTPResult{
		status: rr.Code, ackedItemsCount: parsed.AckedItemsCount,
		notFoundCount: parsed.NotFoundCount, body: rr.Body.String(),
	}
}

// ackLoopDB 建一条规范名入库的待下行，返回 handler 与句柄。
func ackLoopDB(t *testing.T, channel, msgID, accountName string) (*BridgeIngestHandler, *gorm.DB, bridgeIdentifyFixture) {
	t.Helper()
	db, h, fixture := newCanonicalAckHandler(t, channel, accountName)
	accountID := fixture.AccountID
	// 在线位刷新会打到 bridge_accounts（本夹具不建这张表）；这里只测渠道身份，置空全局。
	prevRepo := GlobalBridgeAccountRepo
	GlobalBridgeAccountRepo = nil
	t.Cleanup(func() { GlobalBridgeAccountRepo = prevRepo })

	// 落库形态与真实 ingest 入口一致：只可能是规范名，别名不会出现在这一列。
	if err := db.Create(&model.MessageHub{
		Platform:       channel,
		AccountID:      accountID,
		ConversationID: "conv_alias_ack",
		MsgID:          msgID,
		MsgType:        "text",
		Content:        "您好，这边支持七天无理由退货",
		Direction:      "outbound",
		Status:         "pending",
	}).Error; err != nil {
		t.Fatalf("seed: %v", err)
	}
	return h, db, fixture
}

func TestAckBridgeOutbox_AliasChannel_ClosesOutboxLoop(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const alias = "douyin_web" // 用已登记的候选格验证别名归一与 ACK 闭环。
	h, db, fixture := ackLoopDB(t, model.ChannelDouyin, "mh:alias_ack_1", "acc_alias_ack")
	accountID := fixture.AccountID

	first := pollOutbox(t, h, fixture.Code, alias, accountID)
	if first.status != http.StatusOK || len(first.messages) != 1 {
		t.Fatalf("别名轮询应拿到 1 条待下行，实际 status=%d n=%d body=%s",
			first.status, len(first.messages), first.body)
	}

	res := ackOutbox(t, h, fixture.Code, alias, accountID,
		`{"msg_ids":["mh:alias_ack_1"],"status":"delivered"}`)
	if res.status != http.StatusOK {
		t.Fatalf("ack status=%d body=%s", res.status, res.body)
	}
	if res.ackedItemsCount != 1 {
		t.Errorf("别名 ack acked_items_count=%d 期望 1；body=%s（0 就意味着那一行永远 ack 不掉）",
			res.ackedItemsCount, res.body)
	}
	if res.notFoundCount != 0 {
		t.Errorf("别名 ack not_found_count=%d 期望 0（not_found 是\"客户会被反复轰炸\"的入口判据）", res.notFoundCount)
	}

	// 把认领时间推到租约之外，等价于线上"30s 后扩展又轮询了一次"。
	if err := db.Exec("UPDATE message_hub SET claimed_at = now() - interval '5 minutes' WHERE msg_id = ?",
		"mh:alias_ack_1").Error; err != nil {
		t.Fatalf("模拟租约到期失败: %v", err)
	}
	second := pollOutbox(t, h, fixture.Code, alias, accountID)
	if len(second.messages) != 0 {
		t.Errorf("租约到期后别名轮询又拿到 %d 条 ⇒ 客户会再收到同一条回复；body=%s",
			len(second.messages), second.body)
	}

	var got model.MessageHub
	if err := db.Where("msg_id = ?", "mh:alias_ack_1").First(&got).Error; err != nil {
		t.Fatalf("回读失败: %v", err)
	}
	if got.Status != model.BridgeAckStatusDelivered {
		t.Errorf("DB status=%q 期望 delivered（停在 inflight 就是等着被重投）", got.Status)
	}
}

// v2 items[] 是另一条分支（按 conversation_id 分组后各自 ack），同样得吃到归一。
func TestAckBridgeOutbox_AliasChannel_V2ItemsAlsoNormalized(t *testing.T) {
	gin.SetMode(gin.TestMode)
	h, _, fixture := ackLoopDB(t, model.ChannelXHS, "mh:alias_ack_2", "acc_alias_ack_v2")

	res := ackOutbox(t, h, fixture.Code, "xiaohongshu_web", fixture.AccountID,
		`{"v":2,"items":[{"msg_id":"mh:alias_ack_2","conversation_id":"conv_alias_ack","status":"delivered"}]}`)
	if res.ackedItemsCount != 1 {
		t.Errorf("v2 分支别名 ack acked_items_count=%d 期望 1；body=%s", res.ackedItemsCount, res.body)
	}
}
