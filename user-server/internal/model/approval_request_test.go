// approval_request_test.go T-P3-01 AC①：状态机跃迁表。
//
// 放在 model 包是因为这张表就是本卡的全部业务判据所在地（跃迁合法性）。
// 断言对象是**表的内容**而不是"某个函数没 panic"：状态机写错的那一种方式是
// "多放了一条 pending→pending"或"少了一条 pending→expired"，
// 只看函数返回 true/false 的用例分不出这两种。
package model

import (
	"reflect"
	"strconv"
	"strings"
	"testing"
)

func TestApprovalRequestStatusVocabulary(t *testing.T) {
	want := []string{"pending", "approved", "rejected", "expired"}
	if !reflect.DeepEqual(ApprovalStatuses, want) {
		t.Fatalf("状态值域漂移：期望 %v，实际 %v（卡面写死四态，加一态须先改裁定 C2）", want, ApprovalStatuses)
	}
	if got := ApprovalDecidedStatuses; len(got) != 3 {
		t.Fatalf("终态集合应有 3 个，实际 %d：%v", len(got), got)
	}
	// pending 绝不能在终态里：它在终态就等于"清扫永不处理任何行"。
	for _, s := range ApprovalDecidedStatuses {
		if s == ApprovalStatusPending {
			t.Fatalf("pending 被列进终态集合：%v", ApprovalDecidedStatuses)
		}
	}
	// 终态 ∪ {pending} 必须恰好覆盖值域：漏一个状态 = 那个状态既不能进也不能出（孤儿态）。
	cover := map[string]bool{ApprovalStatusPending: true}
	for _, s := range ApprovalDecidedStatuses {
		cover[s] = true
	}
	for _, s := range ApprovalStatuses {
		if !cover[s] {
			t.Errorf("状态 %q 既不在 pending 也不在终态集合，成了孤儿态", s)
		}
	}
}

// TestApprovalTransitionAllowed_Table 逐对枚举 4×4=16 种跃迁，合法集合必须**恰好**是三条。
//
// 双向断言（少了要红、多了也要红）是这张表的意义所在：只断"合法的那些确实合法"的用例，
// 在有人往表里加一条 rejected→approved 时照样绿 —— 而那正是"改判已拒的审批"这条路。
func TestApprovalTransitionAllowed_Table(t *testing.T) {
	legal := map[[2]string]bool{
		{ApprovalStatusPending, ApprovalStatusApproved}: true,
		{ApprovalStatusPending, ApprovalStatusRejected}: true,
		{ApprovalStatusPending, ApprovalStatusExpired}:  true,
	}
	var gotLegal [][2]string
	for _, from := range ApprovalStatuses {
		for _, to := range ApprovalStatuses {
			pair := [2]string{from, to}
			if ApprovalTransitionAllowed(from, to) != legal[pair] {
				t.Errorf("跃迁 %s→%s：表说 %t，期望 %t", from, to, ApprovalTransitionAllowed(from, to), legal[pair])
			}
			if legal[pair] {
				gotLegal = append(gotLegal, pair)
			}
		}
	}
	if len(gotLegal) != len(legal) {
		t.Fatalf("合法跃迁条数 %d，期望 %d（多出来的每一条都是一条越权改判的路）", len(gotLegal), len(legal))
	}
}

func TestApprovalTransitionAllowed_UnknownStatusIsDenied(t *testing.T) {
	for _, tc := range []struct{ from, to string }{
		{"", ApprovalStatusApproved},
		{ApprovalStatusPending, ""},
		{"", ""},
		{"fulfilled", ApprovalStatusApproved}, // 未来有人加第五态但忘了更新表 ⇒ 无路可走，而不是默认放行
		{ApprovalStatusPending, "RESUMED"},    // 大小写不同 = 未知状态
		{ApprovalStatusApproved, ApprovalStatusApproved},
	} {
		if ApprovalTransitionAllowed(tc.from, tc.to) {
			t.Errorf("未知/自跃迁 %q→%q 被判合法", tc.from, tc.to)
		}
	}
}

// TestApprovalTransitionTargets_OrderedAndCopied 观测视图用的那份列表：
// 顺序必须稳定（快照/差分比对按序比），且改返回值不能改掉表本身。
func TestApprovalTransitionTargets_OrderedAndCopied(t *testing.T) {
	want := []string{ApprovalStatusApproved, ApprovalStatusRejected, ApprovalStatusExpired}
	got := ApprovalTransitionTargets(ApprovalStatusPending)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("pending 的出边 %v，期望按 ApprovalStatuses 顺序的 %v", got, want)
	}
	got[0] = "tampered"
	if again := ApprovalTransitionTargets(ApprovalStatusPending); again[0] != ApprovalStatusApproved {
		t.Fatalf("返回值不是副本：调用方改了一次就把全进程的跃迁表改了（第二次拿到 %q）", again[0])
	}
	if unknown := ApprovalTransitionTargets("nope"); unknown == nil || len(unknown) != 0 {
		t.Fatalf("未知状态应返回空集（非 nil，视图直接 range 不判空），实际 %#v", unknown)
	}
}

func TestApprovalDecidedAutomatically(t *testing.T) {
	// 精确等值：前缀匹配会把人工账号 "policy:alice" 读成策略放行，
	// 于是"人工裁决了多少条"这个数会随账号命名而变。
	for _, by := range []string{ApprovalDecidedByPolicy, ApprovalDecidedByTTL} {
		if !ApprovalDecidedAutomatically(by) {
			t.Errorf("系统保留值 %q 未被判为自动裁决", by)
		}
	}
	for _, by := range []string{"", "alice", "policy:alice", "system", "ttl", "Policy:auto", "policy:auto "} {
		if ApprovalDecidedAutomatically(by) {
			t.Errorf("%q 不是系统保留值却被判为自动裁决（人工裁决率会被这类命名算错）", by)
		}
	}
}

// TestApprovalRequestSchemaShape 列与索引标签的**声明**形状。
//
// 这里只断 gorm 标签（不连库）：真实索引由 repository 包查 pg_indexes 断言。
// 两份都要有 —— 这份拦"标签写错/写丢"，那份拦"AutoMigrate 没把它建成部分索引"。
func TestApprovalRequestSchemaShape(t *testing.T) {
	st := reflect.TypeOf(ApprovalRequest{})
	for _, name := range []string{
		"ID", "SubjectType", "SubjectID", "PolicyKey", "Status", "ResumeToken",
		"ExpiresAt", "DecidedBy", "DecidedAt", "DecisionNote", "CreatedAt", "UpdatedAt",
	} {
		if _, ok := st.FieldByName(name); !ok {
			t.Errorf("字段 %s 不存在（列清单/仓储写集合会跟着漂）", name)
		}
	}
	if got := (ApprovalRequest{}).TableName(); got != "approval_requests" {
		t.Fatalf("表名 %q，期望 approval_requests", got)
	}
	// 身份三列与 status 都不该是指针：它们决定幂等键与状态机，留 NULL 会让
	// "没有 subject_type"的行合法存在，而这样的行既去不了待办中心也回不去幂等键。
	for _, name := range []string{"SubjectType", "SubjectID", "PolicyKey", "Status", "ResumeToken"} {
		if kindOf(t, st, name) == reflect.Ptr {
			t.Errorf("%s 一是指针：NULL 与空串会塌成两种「没人管」的行", name)
		}
	}
	// 时间戳里只有裁决/过期两个是"可能没有"，必须可空。
	for _, name := range []string{"ExpiresAt", "DecidedAt"} {
		if kindOf(t, st, name) != reflect.Ptr {
			t.Errorf("%s 应可空：零值时间无法区分「未设置」和「1970 年就到期了」", name)
		}
	}

	// 幂等键三列必须在**同一个**部分唯一索引上，且带 pending 谓词。
	// 少一列 = 键少一维（policy_key 掉出去 ⇒ 一道审批顺手批了另一道）；
	// 丢谓词 = 已裁决的行永远占着键位，同一对象修正后再申请会当场撞死（AC③ 的反面）。
	for _, tc := range []struct {
		field, column string
		prio          int
	}{
		{"SubjectType", "subject_type", 1},
		{"SubjectID", "subject_id", 2},
		{"PolicyKey", "policy_key", 3},
	} {
		tag := gormTagOf(t, st, tc.field)
		if !strings.Contains(tag, "uniqueIndex:uq_approval_request_open") {
			t.Errorf("列 %s 未挂 uq_approval_request_open：%s", tc.column, tag)
		}
		// priority 与列名都参与索引语义：顺序变了 = 键的字段顺序变了（虽不改去重结果，
		// 但会让 pg_indexes 里的定义与仓储测试里比对的字符串漂移）。
		if !strings.Contains(tag, "priority:"+strconv.Itoa(tc.prio)) {
			t.Errorf("列 %s 的 priority 不是 %d：%s（三列顺序变了 = 索引键顺序变了，谓词不变但去重语义会跟着漂）", tc.column, tc.prio, tag)
		}
		if !strings.Contains(tag, "where:status = 'pending'") {
			t.Errorf("列 %s 没带 pending 谓词：%s", tc.column, tag)
		}
	}
	// 凭证索引必须部分（<> ''）：auto-approve 的行 token 留空且可以有很多条。
	rt := gormTagOf(t, st, "ResumeToken")
	if !strings.Contains(rt, "uniqueIndex:uq_approval_request_token") || !strings.Contains(rt, "where:resume_token <> ''") {
		t.Errorf("resume_token 索引形状不对：%s", rt)
	}
	// 三列的谓词必须逐字一致：GORM 只需**任一**列带 where 就把谓词写进 DDL，
	// 于是"只在一列上改谓词"这种错法在 DDL 层看不出差别，只能在这里比字符串。
	preds := map[string]bool{}
	for _, f := range []string{"SubjectType", "SubjectID", "PolicyKey"} {
		tag := gormTagOf(t, st, f)
		i := strings.Index(tag, "where:")
		if i < 0 {
			t.Fatalf("%s 标签里没有 where:：%s", f, tag)
		}
		preds[tag[i:]] = true
	}
	if len(preds) != 1 {
		t.Errorf("幂等键三列的索引谓词不一致（GORM 会取其中一份进 DDL，另两份是死字）：%v", preds)
	}
}

func kindOf(t *testing.T, st reflect.Type, field string) reflect.Kind {
	t.Helper()
	f, ok := st.FieldByName(field)
	if !ok {
		t.Fatalf("字段 %s 不存在", field)
	}
	return f.Type.Kind()
}

func gormTagOf(t *testing.T, st reflect.Type, field string) string {
	t.Helper()
	f, ok := st.FieldByName(field)
	if !ok {
		t.Fatalf("字段 %s 不存在", field)
	}
	return f.Tag.Get("gorm")
}
