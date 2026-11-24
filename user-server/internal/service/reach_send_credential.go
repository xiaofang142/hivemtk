package service

// reach_send_credential.go FA-REACH-02：批量作业"发送凭据"耐久协议。
//
// 背景（清单 FA-REACH-02）：executeJobCore 每次把 StepResults 清空从头跑，
// 真实发送（dispatchOutbound → sender.SendReach）之后才 SaveJob；发送成功但
// 跟踪/报告失败→作业置 retrying，恢复时再次从头执行→真实发送器被二次调用。
// 进程在"发送"与"落库"之间崩溃同理。email 分支返回的还是异步入队 ID，
// 不能当作 SMTP 已送达凭据。
//
// 协议（按租户/作业/发送意图把凭据存进 job.Payload）：
//   - 发送前：写入 state=sending 的凭据并 SaveJob（耐久）。这是"崩溃窗口"。
//   - 发送成功：写 state=accepted + message_id + delivery，再 SaveJob。
//   - 发送失败：写 state=failed + reason，再 SaveJob。
//   - 恢复：accepted→跳过真实发送、复用 message_id；sending/unknown→
//     结果未知，绝不盲重发，返回 ErrReachSendUnknown 由人工对账；
//     failed/无凭据→可安全发送。
// delivery 区分 email 的"已排队(queued)"与其它渠道的"平台已受理(sent)"，
// 不把入队 ID 当真实送达。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"hivemtk-user/internal/model"
)

const (
	reachSendCredentialKey = "_send_credential"

	reachSendStateSending  = "sending"
	reachSendStateAccepted = "accepted"
	reachSendStateFailed   = "failed"
	reachSendStateUnknown  = "unknown"

	reachSendDeliveryQueued = "queued"
	reachSendDeliverySent   = "sent"
)

// ErrReachSendUnknown 发送结果未知（崩溃窗口/对账未完成）：恢复路径不得盲重发。
var ErrReachSendUnknown = errors.New("reach send result unknown: 发送结果未知，需对账，不自动重发")

// reachSendCredential 是持久在 job.Payload 里的发送凭据。
type reachSendCredential struct {
	IntentKey string `json:"intent_key"`          // 发送意图（租户+作业+渠道+账号+客户+内容）
	State     string `json:"state"`               // sending/accepted/failed/unknown
	MessageID string `json:"message_id,omitempty"` // 平台受理/入队凭据
	Channel   string `json:"channel"`
	Delivery  string `json:"delivery,omitempty"` // queued(email)/sent(其它)
	Reason    string `json:"reason,omitempty"`
	UpdatedAt string `json:"updated_at"`
}

// reachSendIntentKey 计算稳定的发送意图键：同一作业、同一渠道/账号/客户/正文
// 视为同一意图；内容变化意味着新意图（应重新走完整发送）。
func reachSendIntentKey(tenantID uint, jobID uint, channel, accountID, customerID, content string) string {
	raw := fmt.Sprintf("%d|%d|%s|%s|%s|%s", tenantID, jobID, channel, accountID, customerID, content)
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// reachSendDeliveryFor 按渠道给出"送达"口径：email 只拿到异步入队 ID，
// 记 queued；其它渠道记平台已受理 sent。二者不可混同。
func reachSendDeliveryFor(channel string) string {
	if channel == "email" {
		return reachSendDeliveryQueued
	}
	return reachSendDeliverySent
}

// getReachSendCredential 读凭据；无/损坏返回 nil（视为未发送过，可安全走发送）。
func getReachSendCredential(job *model.ReachJob) *reachSendCredential {
	if job == nil || job.Payload == nil {
		return nil
	}
	raw, ok := job.Payload[reachSendCredentialKey]
	if !ok || raw == nil {
		return nil
	}
	var b []byte
	switch v := raw.(type) {
	case []byte:
		b = v
	case string:
		b = []byte(v)
	default:
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil
		}
		b = encoded
	}
	var cred reachSendCredential
	if json.Unmarshal(b, &cred) != nil || cred.IntentKey == "" {
		return nil
	}
	return &cred
}

// setReachSendCredential 把凭据写回 job.Payload（仅内存，落库由调用方 SaveJob 完成）。
func setReachSendCredential(job *model.ReachJob, cred *reachSendCredential) {
	if job == nil || cred == nil {
		return
	}
	if job.Payload == nil {
		job.Payload = model.JSONMap{}
	}
	cred.UpdatedAt = time.Now().Format(time.RFC3339Nano)
	job.Payload[reachSendCredentialKey] = *cred
}

// applyAcceptedCredentialToLastSend 把已受理凭据回填 _last_send，
// 让后续 track_result/report 步骤无需真实重发即可读取 message_id。
func applyAcceptedCredentialToLastSend(job *model.ReachJob, cred *reachSendCredential) {
	if job == nil || cred == nil || cred.State != reachSendStateAccepted {
		return
	}
	if job.Payload == nil {
		job.Payload = model.JSONMap{}
	}
	job.Payload["_last_send"] = map[string]any{
		"message_id": cred.MessageID,
		"channel":    cred.Channel,
		"delivery":   cred.Delivery,
		"sent_at":    cred.UpdatedAt,
		"reused":     true,
	}
}
