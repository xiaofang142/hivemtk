package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
)

// LM-05（后端规范化契约，不接 sagex_upload.go）：
// 把 Android 已上报的 raw_notification.messages / historic_messages 数组转成
// 保留成员、时间、来源方向依据的规范消息。未知方向保持 unknown，不凭昵称推断
// 客户/本人；重叠消息按源消息级 ID 幂等去重。
//
// 边界：本文件只提供纯函数；接入 MessageEvent/MessageHub、源消息级落库幂等、
// Android 真机采集范围（通知不等于会话数据库）需在授权会话另行施工与验证。
type SageXNotificationUpload struct {
	EventID          string
	Messages         []SageXStyleMessage
	HistoricMessages []SageXStyleMessage
	ParseStatus      string
	TruncatedFields  []string
}

// NormalizedSageXMessage 单条规范消息。
type NormalizedSageXMessage struct {
	SourceMessageID string `json:"source_message_id"`
	Content         string `json:"content"`
	MemberID        string `json:"member_id"`
	MemberName      string `json:"member_name"`
	At              int64  `json:"at"`
	Direction       string `json:"direction"`       // unknown / inbound / outbound（仅上报明确给出时使用）
	SourceCoverage  string `json:"source_coverage"` // live / historic
}

// NormalizedSageXNotification 一次通知的规范化结果 + 采集完整性标记。
type NormalizedSageXNotification struct {
	EventID              string                   `json:"event_id"`
	Messages             []NormalizedSageXMessage `json:"messages"`
	ParseStatus          string                   `json:"parse_status"`
	TruncatedFields      []string                 `json:"truncated_fields"`
	CollectionIncomplete bool                     `json:"collection_incomplete"`
}

// NormalizeSageXNotificationMessages 规范化已上报数组。
func NormalizeSageXNotificationMessages(in SageXNotificationUpload) NormalizedSageXNotification {
	out := NormalizedSageXNotification{
		EventID:         strings.TrimSpace(in.EventID),
		ParseStatus:     strings.TrimSpace(in.ParseStatus),
		TruncatedFields: append([]string(nil), in.TruncatedFields...),
		Messages:        make([]NormalizedSageXMessage, 0, len(in.Messages)+len(in.HistoricMessages)),
	}
	if out.ParseStatus == "" {
		out.ParseStatus = "unknown"
	}
	if out.ParseStatus != "ok" || len(in.TruncatedFields) > 0 {
		out.CollectionIncomplete = true
	}
	seen := make(map[string]bool, len(in.Messages)+len(in.HistoricMessages))
	add := func(m SageXStyleMessage, coverage string) {
		content := strings.TrimSpace(m.Text)
		if content == "" || strings.HasPrefix(content, "[图片]") {
			return
		}
		memberID := strings.TrimSpace(m.SenderKey)
		memberName := strings.TrimSpace(m.SenderName)
		// 源消息级幂等键：事件前缀 + 时间 + 成员 + 文本；不依赖昵称或安装 ID。
		raw := fmt.Sprintf("%s|%d|%s|%s", out.EventID, m.Timestamp, memberID, content)
		sum := sha256.Sum256([]byte(raw))
		id := hex.EncodeToString(sum[:16])
		if seen[id] {
			return
		}
		seen[id] = true
		out.Messages = append(out.Messages, NormalizedSageXMessage{
			SourceMessageID: id,
			Content:         content,
			MemberID:        memberID,
			MemberName:      memberName,
			At:              m.Timestamp,
			Direction:       "unknown", // 上报未给出方向依据，保持未知
			SourceCoverage:  coverage,
		})
	}
	for _, m := range in.HistoricMessages {
		add(m, "historic")
	}
	for _, m := range in.Messages {
		add(m, "live")
	}
	sort.SliceStable(out.Messages, func(i, j int) bool {
		if out.Messages[i].At == out.Messages[j].At {
			return out.Messages[i].SourceMessageID < out.Messages[j].SourceMessageID
		}
		return out.Messages[i].At < out.Messages[j].At
	})
	return out
}
