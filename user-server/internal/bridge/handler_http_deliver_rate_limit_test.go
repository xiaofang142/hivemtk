package bridge

// 服务端下发限速兜底（DeliverRateLimitPerMin）全链路门禁测试。
//
// 架构文档 §4.2「限速（后端）：兜底 60/min（防 AI 失控）」承诺的行为锁：
//   - 近 60s 认领数达到阈值 → ClaimPendingOutbound 返回空（不动 pending 行、不报错）
//   - 参数置 0（停用兜底）→ 正常认领
// 计数口径 = outbound 行 claimed_at（认领即「交给桥端去发」，ack/终态行保留 claimed_at）。
// DB 门禁：缺 POSTGRES_TEST_PASSWORD 时 fail-closed 跳过（环境问题非代码问题，AGENTS §3 口径）。

import (
	"context"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/service"
)

func TestClaimPendingOutbound_DeliverRateLimit_ThrottlesAndRecovers(t *testing.T) {
	db := setupBridgeTestDB(t, &model.MessageHub{}, &model.ConfigParam{})
	if err := db.Exec("DELETE FROM message_hub").Error; err != nil {
		t.Fatalf("清理失败: %v", err)
	}
	if err := db.Exec("DELETE FROM config_params").Error; err != nil {
		t.Fatalf("清理参数失败: %v", err)
	}

	const (
		channel   = "douyin"
		accountID = "acc_rate_limit"
	)

	// 参数行：阈值 1（窗口内认领 1 次即拦）。
	param := &model.ConfigParam{
		Group:        "bridge",
		Key:          service.BridgeDeliverRateLimitParam,
		Name:         "服务端下发限速兜底",
		ValueType:    "int",
		Value:        "1",
		DefaultValue: "1",
	}
	if err := db.Create(param).Error; err != nil {
		t.Fatalf("seed 参数失败: %v", err)
	}

	// 近窗「已认领」行（claimed_at=now，模拟 1 分钟内已被交给桥端一次）+
	// 一条待下发 pending 行。
	claimed := &model.MessageHub{
		Platform: channel, AccountID: accountID, ConversationID: "conv_rl",
		MsgID: "mh:rl_claimed", MsgType: "text", Content: "已认领",
		Direction: "outbound", Status: "delivered", ClaimedAt: ptrTime(time.Now()),
	}
	pending := &model.MessageHub{
		Platform: channel, AccountID: accountID, ConversationID: "conv_rl",
		MsgID: "mh:rl_pending", MsgType: "text", Content: "待下发",
		Direction: "outbound", Status: "pending",
	}
	if err := db.Create(claimed).Error; err != nil {
		t.Fatalf("seed claimed 行失败: %v", err)
	}
	if err := db.Create(pending).Error; err != nil {
		t.Fatalf("seed pending 行失败: %v", err)
	}

	// 参数服务读测试库（SetTestDB 已注入 dbUtil）。
	service.SetGlobalForTest(service.NewConfigParamService())
	t.Cleanup(func() { service.SetGlobalForTest(nil) })

	svc := service.NewInboxIngressService()
	ctx := context.Background()

	rows, err := svc.ClaimPendingOutbound(ctx, channel, accountID, 50)
	if err != nil {
		t.Fatalf("ClaimPendingOutbound（限速窗内）: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("近窗认领数已达阈值，期望返回空（兜底拦截），实际 %d 行", len(rows))
	}

	// pending 行必须原样保留（拦截不落失败态、不丢行）。
	var kept model.MessageHub
	if err := db.Where("msg_id = ?", "mh:rl_pending").First(&kept).Error; err != nil {
		t.Fatalf("查询 pending 行失败: %v", err)
	}
	if kept.Status != "pending" {
		t.Errorf("被拦截的 pending 行状态应保持 pending，实际 %s", kept.Status)
	}

	// 参数置 0（停用兜底）→ 同一 pending 行正常认领。
	if err := db.Model(&model.ConfigParam{}).
		Where("param_group = ? AND key = ?", "bridge", service.BridgeDeliverRateLimitParam).
		Update("param_value", "0").Error; err != nil {
		t.Fatalf("改参数失败: %v", err)
	}
	service.SetGlobalForTest(service.NewConfigParamService())

	rows, err = svc.ClaimPendingOutbound(ctx, channel, accountID, 50)
	if err != nil {
		t.Fatalf("ClaimPendingOutbound（停用兜底后）: %v", err)
	}
	if len(rows) != 1 || rows[0].MsgID != "mh:rl_pending" {
		t.Fatalf("停用兜底后应正常认领 pending 行，实际 %d 行", len(rows))
	}
}

func ptrTime(t time.Time) *time.Time { return &t }
