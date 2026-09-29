// kb_release_test.go T-P9-02：变更三张表的形状与跃迁判据（模型层，无库）。
//
// 本文件锁的是四件"只有这里能锁"的事：
//  1. op / status 的值域与判据同源（service 与表驱动测试都问 IsValidKBChange*，
//     加一个动作值而不加判据，判据会静默放行新值）；
//  2. 跃迁表**恰好**答那三句：pending 能去 applied/withdrawn，applied 与 withdrawn 是终态；
//     特别是 applied→withdrawn 必须为假 —— 它是"已上线的内容只能再提一条 retire 走审批"
//     这条口径的全部实现，放开它等于给运营开一条绕过审批的下线通道（AC① 的反例）；
//  3. 表名与叶子包里那段裸 SQL 的锚点一致（hiddenPredicate 写死了 kb_releases 与
//     knowledge_chunks 两个裸表名，改 TableName() 不会让任何编译报错，只会让闸门静默失效）；
//  4. 刻意没有 rejected/expired 两个状态值 —— 这是一条**负断言**：批没批的唯一事实源是
//     approval_requests，本表再记一份就会出现永远查不出来的分歧。它必须是"值域里没有"，
//     而不是"没人往那一列写过"。
package model

import "testing"

func TestKBChangeModel_OpValueDomain(t *testing.T) {
	if len(KBChangeOps) != 3 {
		t.Fatalf("动作值域应为 3 项，实得 %d：%v", len(KBChangeOps), KBChangeOps)
	}
	for _, op := range KBChangeOps {
		if !IsValidKBChangeOp(op) {
			t.Errorf("%q 在值域里却判不合法（判据与值域分叉）", op)
		}
	}
	cases := map[string]bool{
		"":        false,
		"ADD":     false, // 大小写不放过：service.normalize 先 ToLower，越过它的写入方必须被挡
		"add ":    false,
		"delete":  false,
		"insert":  false,
		"update":  false,
		"revise":  true,
		"retire":  true,
		"pending": false, // 状态值不是动作值：两套值域不许互串
	}
	for in, want := range cases {
		if got := IsValidKBChangeOp(in); got != want {
			t.Errorf("IsValidKBChangeOp(%q)=%t，期望 %t", in, got, want)
		}
	}
}

func TestKBChangeModel_StatusValueDomain(t *testing.T) {
	if len(KBChangeStatuses) != 3 {
		t.Fatalf("状态值域应为 3 项，实得 %d：%v", len(KBChangeStatuses), KBChangeStatuses)
	}
	for _, s := range KBChangeStatuses {
		if !IsValidKBChangeStatus(s) {
			t.Errorf("%q 在值域里却判不合法", s)
		}
	}
	// 负断言：审批侧的结论词绝不允许出现在本表值域里（唯一事实源在 approval_requests）。
	for _, forbidden := range []string{"rejected", "expired", "approved", "apply_failed", "unknown"} {
		if IsValidKBChangeStatus(forbidden) {
			t.Errorf("状态值域里出现了 %q：审批结论不许在变更行上复制一份", forbidden)
		}
		if IsValidKBChangeOp(forbidden) {
			t.Errorf("动作值域里出现了 %q", forbidden)
		}
	}
}

// TestKBChangeModel_TransitionTable 逐格钉跃迁表。
//
// 表驱动成"每一对都断言一次期望值"而不是"只断言合法的那几条"：跃迁表是 map 套 map，
// 缺项与项值为假都返回 false，只测合法格的话，删掉一行 withdraw 通路也照样全绿。
func TestKBChangeModel_TransitionTable(t *testing.T) {
	type tr struct{ from, to string }
	want := map[tr]bool{
		{KBChangeStatusPending, KBChangeStatusApplied}:     true,
		{KBChangeStatusPending, KBChangeStatusWithdrawn}:   true,
		{KBChangeStatusPending, KBChangeStatusPending}:     false, // 自跃迁不算合法（CAS 靠它判"状态没变"）
		{KBChangeStatusApplied, KBChangeStatusPending}:     false, // 回滚不把 applied 翻回 pending
		{KBChangeStatusApplied, KBChangeStatusWithdrawn}:   false, // 已发布的撤不掉：只能再提一条 retire
		{KBChangeStatusApplied, KBChangeStatusApplied}:     false,
		{KBChangeStatusWithdrawn, KBChangeStatusPending}:   false, // withdrawn 是终态
		{KBChangeStatusWithdrawn, KBChangeStatusApplied}:   false,
		{KBChangeStatusWithdrawn, KBChangeStatusWithdrawn}: false,
		{"", KBChangeStatusApplied}:                        false,
		{"bogus", KBChangeStatusWithdrawn}:                 false,
	}
	for pair, expect := range want {
		if got := KBChangeTransitionAllowed(pair.from, pair.to); got != expect {
			t.Errorf("KBChangeTransitionAllowed(%q→%q)=%t，期望 %t", pair.from, pair.to, got, expect)
		}
	}
}

// TestKBChangeModel_TransitionTableCoversStatuses 跃迁表的键集合必须与状态值域同源。
//
// 只测上面那张表会漏掉一种坏法：给值域加第四个状态却忘了给它开跃迁行 —— 于是那行的状态
// 永远出不去（读起来像"卡在某个没人认识的中间态"）。这条断言把它变成一次编译期之外的红。
func TestKBChangeModel_TransitionTableCoversStatuses(t *testing.T) {
	for _, s := range KBChangeStatuses {
		if _, ok := kbChangeLegalTransitions[s]; !ok {
			t.Errorf("状态 %q 在跃迁表里没有键（新加状态忘了开跃迁行）", s)
		}
	}
	if len(kbChangeLegalTransitions) != len(KBChangeStatuses) {
		t.Errorf("跃迁表有 %d 个键而值域有 %d 个状态：两边不同源",
			len(kbChangeLegalTransitions), len(KBChangeStatuses))
	}
}

// TestKBChangeModel_TableNames 表名钉死（叶子包的裸 SQL 靠它们定位）。
func TestKBChangeModel_TableNames(t *testing.T) {
	cases := []struct{ got, want string }{
		{(KBChangeRequest{}).TableName(), "kb_change_requests"},
		{(KBRelease{}).TableName(), "kb_releases"},
		{(KBChangeAuditLog{}).TableName(), "kb_change_audit_logs"},
		{(KnowledgeChunk{}).TableName(), "knowledge_chunks"},
	}
	for _, c := range cases {
		if c.got != c.want {
			t.Errorf("表名=%q，期望 %q（kbrelease.hiddenPredicate 与发布 SQL 里写死的是后者）", c.got, c.want)
		}
	}
}

// TestKBChangeModel_AuditActionsAreDistinct 审计动作值域：六项、互不重复，
// 且每一项都有对应常量（少一项就是留痕少一类，而 AC③ 要的正是"每个动作都有一条"）。
func TestKBChangeModel_AuditActionsAreDistinct(t *testing.T) {
	want := []string{
		KBAuditSubmitted, KBAuditWithdrawn, KBAuditPublished,
		KBAuditRollback, KBAuditRestored, KBAuditGoverned,
	}
	if len(KBChangeAuditActions) != len(want) {
		t.Fatalf("审计动作值域应为 %d 项，实得 %d：%v", len(want), len(KBChangeAuditActions), KBChangeAuditActions)
	}
	seen := make(map[string]bool, len(want))
	for i, a := range KBChangeAuditActions {
		if a != want[i] {
			t.Errorf("第 %d 项=%q，期望 %q（值域顺序即事件顺序，重排会打乱审计读的口径）", i, a, want[i])
		}
		if seen[a] {
			t.Errorf("审计动作 %q 重复", a)
		}
		seen[a] = true
		if len(a) > 24 {
			t.Errorf("审计动作 %q 长 %d 超列宽 varchar(24)", a, len(a))
		}
	}
}
