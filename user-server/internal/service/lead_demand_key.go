package service

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"

	"hivemtk-user/internal/model"
)

// LM-10：同群多需求按「实际主体 / 业务对象(项目) / 供需方向」独立分桶。
// 群 OneID 仍作为归档身份，但需求身份不按平台:发送人/群账号整体回查覆盖。
func leadRoleOf(jd *LeadJudgement) string {
	if jd == nil {
		return "unknown"
	}
	return normalizeLeadRole(jd.LeadRole)
}

// leadDemandProject 用判定摘要作为业务对象/项目的有界表示；过长时截断，保持键稳定。
func leadDemandProject(summary string) string {
	p := strings.TrimSpace(summary)
	if len([]rune(p)) > 32 {
		p = string([]rune(p)[:32])
	}
	return p
}

func leadDemandKeyFor(hub model.MessageHub, subject, project, direction string) string {
	scope := strings.TrimSpace(hub.Platform) + ":" + strings.TrimSpace(hub.AccountID)
	if hub.IsGroup {
		scope += ":g:" + strings.TrimSpace(hub.GroupID)
	} else {
		scope += ":c:" + strings.TrimSpace(hub.ConversationID)
	}
	s := strings.TrimSpace(subject)
	if s == "" {
		s = "unknown" // 未知主体独立待核，不凭昵称合并
	}
	p := strings.TrimSpace(project)
	if p == "" {
		p = "unknown"
	}
	d := strings.TrimSpace(direction)
	if d == "" {
		d = "unknown"
	}
	raw := fmt.Sprintf("%s|%s|%s|%s", scope, s, p, d)
	sum := sha256.Sum256([]byte(raw))
	return "demand:" + hex.EncodeToString(sum[:12])
}

// LeadEvidenceVersions 证据版本比较：同一线索只允许更高版本追加，
// 同版本不同消息按消息 ID 幂等，迟到旧版本不覆盖新证据。
type LeadEvidenceVersions struct {
	mu   sync.Mutex
	seen map[string]leadEvidenceState
}

type leadEvidenceState struct {
	version uint64
	ids     map[string]bool
}

func NewLeadEvidenceVersions() *LeadEvidenceVersions {
	return &LeadEvidenceVersions{seen: make(map[string]leadEvidenceState)}
}

func (v *LeadEvidenceVersions) ShouldAppend(clueID string, version uint64, messageID string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.seen == nil {
		v.seen = make(map[string]leadEvidenceState)
	}
	st := v.seen[clueID]
	if st.ids == nil {
		st.ids = make(map[string]bool)
	}
	if version <= st.version {
		return false // 同版本不同消息也不得重复追加，只有新版本可推进
	}
	st.version, st.ids = version, make(map[string]bool)
	if st.ids[messageID] {
		return false
	}
	st.ids[messageID] = true
	v.seen[clueID] = st
	return true
}
