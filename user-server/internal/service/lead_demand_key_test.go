package service

import (
	"testing"

	"hivemtk-user/internal/model"
)

// LM-10 正例：同群甲乙各买同商品默认两条；甲买乙卖为两条可关联记录；同项目补条件只演进一条。
func TestLeadDemandKey_MultiSubjectAndDirection(t *testing.T) {
	base := model.MessageHub{Platform: "personal_wx", AccountID: "acc", ConversationID: "conv", IsGroup: true, GroupID: "g1"}
	a := leadDemandKeyFor(base, "甲", "原料A", "demand")
	b := leadDemandKeyFor(base, "乙", "原料A", "demand")
	c := leadDemandKeyFor(base, "乙", "原料A", "supply")
	if a == b || b == c {
		t.Fatalf("主体/方向不同应独立分桶: %s %s %s", a, b, c)
	}
	if leadDemandKeyFor(base, "甲", "原料A", "demand") != a {
		t.Fatal("同主体同项目同方向应稳定回查")
	}
}

// LM-10 反例：未知主体不按昵称合并；证据版本比较防并发覆盖，迟到旧版本不更新。
func TestLeadDemandKey_UnknownSubjectAndEvidenceVersion(t *testing.T) {
	base := model.MessageHub{Platform: "telegram", AccountID: "acc", IsGroup: true, GroupID: "g"}
	u1 := leadDemandKeyFor(base, "", "货", "demand")
	u2 := leadDemandKeyFor(base, "昵称甲", "货", "demand")
	if u1 == u2 {
		t.Fatal("未知主体不得凭昵称合并")
	}
	st := NewLeadEvidenceVersions()
	if !st.ShouldAppend("clue-1", 2, "m2") {
		t.Fatal("新版本应追加")
	}
	if st.ShouldAppend("clue-1", 2, "m3") {
		t.Fatal("同版本不同消息不得重复追加")
	}
	if st.ShouldAppend("clue-1", 1, "m0") {
		t.Fatal("迟到旧版本不得覆盖/追加")
	}
	if !st.ShouldAppend("clue-1", 3, "m4") {
		t.Fatal("更新版本应允许追加")
	}
}
