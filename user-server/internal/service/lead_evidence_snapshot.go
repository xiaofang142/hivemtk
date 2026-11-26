package service

import (
	"strings"

	"hivemtk-user/internal/model"
)

// LeadEvidenceSnapshot 真实消息证据快照（LM-12）。
// 只收真实 hub 行的 MsgID，合成 history_N / 伪造 ID 不属于快照，不能作为结论引用。
type LeadEvidenceSnapshot struct {
	Messages  []model.MessageHub
	byID      map[string]bool
	Watermark uint64
	Version   uint64
}

func NewLeadEvidenceSnapshot(hubs []model.MessageHub) *LeadEvidenceSnapshot {
	s := &LeadEvidenceSnapshot{byID: make(map[string]bool)}
	seen := make(map[string]bool)
	for _, h := range hubs {
		id := strings.TrimSpace(h.MsgID)
		if id == "" || seen[id] || strings.HasPrefix(id, "history_") {
			continue
		}
		seen[id] = true
		s.byID[id] = true
		s.Messages = append(s.Messages, h)
		if uint64(h.ID) > s.Watermark {
			s.Watermark = uint64(h.ID)
		}
	}
	return s
}

// ReferencesValid 引用必须全部属于授权快照；空引用/合成 ID/伪造 ID 一律拒绝。
func (s *LeadEvidenceSnapshot) ReferencesValid(ids []string) bool {
	if s == nil || len(ids) == 0 {
		return false
	}
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" || strings.HasPrefix(id, "history_") || !s.byID[id] {
			return false
		}
	}
	return true
}

// RealMessageIDs 快照内全部真实消息 ID（排除合成 history_N），用于证据落库与引用校验。
func (s *LeadEvidenceSnapshot) RealMessageIDs() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.Messages))
	for _, m := range s.Messages {
		id := strings.TrimSpace(m.MsgID)
		if id != "" && !strings.HasPrefix(id, "history_") {
			out = append(out, id)
		}
	}
	return out
}
