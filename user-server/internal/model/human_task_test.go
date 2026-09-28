// human_task_test.go 统一待办模型的表驱动用例（T-P3-03 AC①）。
//
// 本文件要证明的是一句很容易被写歪的话：**三类待办共用一套状态机，差异只在数据上**。
// 共用 = 同一条 (from,action)→to 的表对三类都给同样答案；
// 独立 = SLA 各占一列，填了别人的列就是错。
package model

import (
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestHumanTaskTableNameAndKeyShape(t *testing.T) {
	var zero HumanTask
	if got := zero.TableName(); got != "human_tasks" {
		t.Errorf("表名 = %q，期望 human_tasks", got)
	}
	ft, ok := reflect.TypeOf(zero).FieldByName("ID")
	if !ok {
		t.Fatal("没有 ID 字段")
	}
	tag := ft.Tag.Get("gorm")
	if !strings.Contains(tag, "primaryKey") || !strings.Contains(tag, "type:text") {
		t.Errorf("ID 应是 text 主键，实际 tag = %q", tag)
	}
}

// TestHumanTaskOpenPredicateMatchesIndexSQL 钉住"开放态"这件事的多处写法必须一致：
// Go 侧的 HumanTaskOpenStatuses / HumanTaskTerminalStatuses（未读聚合、幂等判据用它）、
// gorm tag 里那条部分唯一索引的 WHERE 谓词（库里真正拦重复的是它）、以及建好之后的
// pg 索引定义（仓储用例里比对）。
//
// 少了这条断言，几处可以各自漂移：谓词少一个状态 = 库里允许两行"开放"而代码只认一行，
// AC② 的"不重复投递"当场破掉，而且只在特定状态下破（最难查的那种）。
func TestHumanTaskOpenPredicateMatchesIndexSQL(t *testing.T) {
	pred := HumanTaskOpenPredicateSQL()
	if pred != "status <> 'done' AND status <> 'cancelled'" {
		t.Fatalf("开放态谓词 = %q，期望逐字等于两个终态取反", pred)
	}
	if strings.Contains(pred, ",") {
		t.Fatal("谓词里出现了逗号：GORM 按逗号切 tag，带逗号会被劈坏、AutoMigrate 直接建表失败")
	}
	for _, f := range []string{"SubjectType", "SubjectID"} {
		field, _ := reflect.TypeOf(HumanTask{}).FieldByName(f)
		gormTag := field.Tag.Get("gorm")
		if !strings.Contains(gormTag, "uq_human_task_open") {
			t.Errorf("%s 没进 uq_human_task_open：幂等键少了这一列，同一件事可以重复投递", f)
			continue
		}
		if !strings.Contains(gormTag, pred) {
			t.Errorf("%s 的索引谓词 %q 与 Go 侧的开放态定义 %q 不一致", f, gormTag, pred)
		}
	}
	// 两侧划分必须互补且不相交，并且与逐状态判定一致。
	if len(HumanTaskOpenStatuses)+len(HumanTaskTerminalStatuses) != len(HumanTaskStatuses) {
		t.Errorf("开放态 %d + 终态 %d ≠ 值域 %d：有状态没被归类",
			len(HumanTaskOpenStatuses), len(HumanTaskTerminalStatuses), len(HumanTaskStatuses))
	}
	for _, s := range HumanTaskStatuses {
		open := HumanTaskIsOpen(s)
		terminal := contains(HumanTaskTerminalStatuses, s)
		if open == terminal {
			t.Errorf("状态 %s 的两侧判定都是 %v（应恰好一边真）", s, open)
		}
		for _, list := range [][]string{HumanTaskOpenStatuses, HumanTaskTerminalStatuses} {
			if contains(list, s) && !contains(HumanTaskStatuses, s) {
				t.Errorf("子集里有值域外的状态")
			}
		}
	}
}

func contains(xs []string, v string) bool {
	for _, x := range xs {
		if x == v {
			return true
		}
	}
	return false
}

// TestHumanTaskStatusVocabulary 值域逐字固定：四态、无 expired。
//
// "逾期"不是一个状态而是一句读数（sla_* 时刻已过），这条断言把那个决定钉在测试里：
// 有人觉得"该有个 expired"时会先撞到这里，而不是在库里加一列然后两份事实源并存。
func TestHumanTaskStatusVocabulary(t *testing.T) {
	want := []string{HumanTaskStatusPending, HumanTaskStatusClaimed, HumanTaskStatusDone, HumanTaskStatusCancelled}
	if !reflect.DeepEqual(HumanTaskStatuses, want) {
		t.Errorf("状态值域 = %v，期望 %v", HumanTaskStatuses, want)
	}
	for _, s := range HumanTaskStatuses {
		if !HumanTaskStatusKnown(s) {
			t.Errorf("%s 在值域里却不被判已知", s)
		}
	}
	if HumanTaskStatusKnown("expired") {
		t.Error("expired 不该是待办状态：逾期的待办仍是待处理，翻成终态会让它从列表消失而事情没做")
	}
	if HumanTaskStatusKnown("") {
		t.Error("空串不该被判已知状态")
	}
	// 终态不该算开放：开放态集合是幂等键与未读数的口径，混进终态等于批完的待办还占坑。
	for _, s := range []string{HumanTaskStatusDone, HumanTaskStatusCancelled} {
		if HumanTaskIsOpen(s) {
			t.Errorf("%s 是终态却仍算开放", s)
		}
	}
	for _, s := range HumanTaskOpenStatuses {
		if !HumanTaskIsOpen(s) {
			t.Errorf("%s 在开放态集合里却不被判开放", s)
		}
	}
}

// TestHumanTaskKindVocabulary 三类逐字固定（与 C3 裁定原文一致）。
func TestHumanTaskKindVocabulary(t *testing.T) {
	want := []string{
		HumanTaskKindConversationHandoff,
		HumanTaskKindApproval,
		HumanTaskKindCollectionEscalation,
	}
	if !reflect.DeepEqual(HumanTaskKinds, want) {
		t.Errorf("kind 值域 = %v，期望 %v", HumanTaskKinds, want)
	}
	if HumanTaskKindKnown("sms_escalation") {
		t.Error("未登记的四类不该被当作已知")
	}
	if HumanTaskSLAField("sms_escalation") != "" {
		t.Error("未知 kind 不该有 SLA 列")
	}
}

// TestHumanTaskStateMachineSharedAcrossKinds 本卡 AC① 的前半句：共用状态机。
//
// 审批类从待办侧摘掉 complete/cancel 之后，这句话要拆成两半说，否则它会和"审批类不许从待办侧 complete/cancel"打架：
//
//   - **状态怎么跃迁**（(from,action) → to）三类必须逐字相同，一条私有的边都不许有；
//   - **哪一类准不准用这个动作**是另一根轴（humanTaskActionKinds），它的差异只许表现为
//     "少几条路"，绝不许表现为"同样的路落到不同的态"。
//
// 所以判据写成双向包含：kind 侧放行 ⟹ 共享表里有同一条边且目标态相同；
// 而对这一类开放该动作时 ⟹ 结论恰好就是共享表的那一个。
// 将来有人给某一类偷偷加一条边或改一个目标态，这里会红。
func TestHumanTaskStateMachineSharedAcrossKinds(t *testing.T) {
	for _, from := range HumanTaskStatuses {
		for _, action := range HumanTaskActions {
			sharedTo, sharedOK := HumanTaskCloseTarget(from, action)
			if sharedOK != (sharedTo != "") {
				t.Errorf("共享状态机自相矛盾：(%s,%s) ok=%v 却给出目标态 %q", from, action, sharedOK, sharedTo)
			}
			if sharedOK && !HumanTaskStatusKnown(sharedTo) {
				t.Errorf("(%s,%s) 落到未知状态 %q", from, action, sharedTo)
			}
			for _, kind := range HumanTaskKinds {
				to, ok := HumanTaskTransitionAllowed(kind, from, action)
				if ok && !sharedOK {
					t.Errorf("状态机不再共用：%s 在 (%s,%s) 上偷偷有一条共享表里没有的边 → %q", kind, from, action, to)
				}
				if ok && to != sharedTo {
					t.Errorf("状态机不再共用：(%s,%s) 共享表给 %q，%s 却给 %q", from, action, sharedTo, kind, to)
				}
				if HumanTaskActionAllowed(kind, action) && ok != sharedOK {
					t.Errorf("%s 对该动作是开放的，(%s,%s) 的结论却应是 (%q,%v)，实际 (%q,%v)——状态那一维被加了类别特判",
						kind, from, action, sharedTo, sharedOK, to, ok)
				}
			}
		}
	}
}

// TestHumanTaskClaimIsAKindDifference 抢占差异是**数据**不是第二套状态机：
// 除 claim/release 的适用范围外，三类的可达状态集合必须完全相同。
func TestHumanTaskClaimIsAKindDifference(t *testing.T) {
	reach := func(kind string) []string {
		var out []string
		for _, from := range HumanTaskStatuses {
			for _, action := range HumanTaskActions {
				if to, ok := HumanTaskTransitionAllowed(kind, from, action); ok {
					out = append(out, from+"→"+to)
				}
			}
		}
		return out
	}
	handoff := reach(HumanTaskKindConversationHandoff)
	// 会话类 6 条边 = 催收类的 4 条 + pending→claimed（认领）+ claimed→pending（释放）。
	if len(handoff) != 6 {
		t.Errorf("会话类可达边数 = %d，期望 6：%v", len(handoff), handoff)
	}
	if got := reach(HumanTaskKindCollectionEscalation); len(got) != 4 {
		t.Errorf("催收类可达边 = %v，期望只剩 complete/cancel 那 4 条", got)
	}
	// 审批类**一条对外边都没有**：它的四个动词全不开放 —— 认领/释放被 C3 挡住，
	// 完成/撤销被"只能随审批行落定而收口"挡住（见 humanTaskActionKinds 的注释）。
	// 这一格守的是：以前它有 4 条边，坐席 POST /complete 拿到 200，
	// 而 approval_requests 那一行原样停在 pending。
	if got := reach(HumanTaskKindApproval); len(got) != 0 {
		t.Errorf("审批类可达边 = %v，期望零条（对外没有任何一颗按钮，裁决归 /api/approvals/:id/decide）", got)
	}
}

// TestHumanTaskApprovalStillCloseableBySystem 摘掉审批类的对外边之后，系统按业务身份
// 收口的那两条路必须照旧走得通。
//
// 这条是上一切的反向守卫：CloseForApproval（裁决/到期收口）走的是 CompleteOpenBySubject /
// CancelOpenBySubject，它们与对外动作表**共用**过同一张 kind 判据 —— 当时把 complete/cancel
// 从审批类摘掉，实测结果是裁决后待办静静留在池子里（service 层回 (row,false,nil)，不报错）。
// 一张表管两件不同的事，就会在关掉其中一个用途时把另一个用途一起关掉，而且没有声音。
// 本用例把"这必须是两张判据"钉成用例而不是注释。
func TestHumanTaskApprovalStillCloseableBySystem(t *testing.T) {
	for _, from := range HumanTaskOpenStatuses {
		if to, ok := HumanTaskCloseTarget(from, HumanTaskActionComplete); !ok || to != HumanTaskStatusDone {
			t.Errorf("系统收口 %s complete → (%q,%v)，期望 (%s,true)", from, to, ok, HumanTaskStatusDone)
		}
		if to, ok := HumanTaskCloseTarget(from, HumanTaskActionCancel); !ok || to != HumanTaskStatusCancelled {
			t.Errorf("系统收口 %s cancel → (%q,%v)，期望 (%s,true)", from, to, ok, HumanTaskStatusCancelled)
		}
		// 同状态下人侧仍然被拒（两类审批动作都不许从这里过）。
		if _, ok := HumanTaskTransitionAllowed(HumanTaskKindApproval, from, HumanTaskActionComplete); ok {
			t.Errorf("审批类 %s 的 complete 仍被对外动作表放行", from)
		}
	}
	// 终态仍然拦得住系统收口：不是"审批特殊"，是状态机没有出边。
	for _, from := range []string{HumanTaskStatusDone, HumanTaskStatusCancelled, "", "expired"} {
		for _, action := range []string{HumanTaskActionComplete, HumanTaskActionCancel} {
			if to, ok := HumanTaskCloseTarget(from, action); ok {
				t.Errorf("系统收口从 %q 执行 %s 竟放行到 %q（终态无出边）", from, action, to)
			}
		}
	}
	// 口型由调用方限定：本函数只被 complete/cancel 两条系统收口路径调用（见 service 层
	// 的 Complete/CancelOpenBySubject）。它答的是"这个状态下有没有这条边"，
	// 不答"这个动作对不对这一类开放" —— 后者正是上面那些断言分给 TransitionAllowed 的部分。
	if _, ok := HumanTaskCloseTarget(HumanTaskStatusClaimed, HumanTaskActionClaim); ok {
		t.Error("claimed --claim--> 不该存在（那是重复认领，判据在 CAS 那一侧）")
	}
}

// TestHumanTaskCompleteCancelLandOnTerminals 两条终态边逐格实测（正向）：
// 会话类与催收类由人在界面上点，审批类**只能**由系统按业务身份收口。
//
// 审批类从"三类共用"里被摘出来；它的反向守卫（摘掉边系统收口仍然走得通）
// 在 TestHumanTaskApprovalStillCloseableBySystem，两条合起来才是完整判据：
// 只留前者会把正常路径关掉，只留后者拦不住人从待办侧拆闸门。
func TestHumanTaskCompleteCancelLandOnTerminals(t *testing.T) {
	for _, kind := range []string{HumanTaskKindConversationHandoff, HumanTaskKindCollectionEscalation} {
		for _, from := range HumanTaskOpenStatuses {
			if to, ok := HumanTaskTransitionAllowed(kind, from, HumanTaskActionComplete); !ok || to != HumanTaskStatusDone {
				t.Errorf("%s/%s complete → (%q,%v)，期望 (%s,true)", kind, from, to, ok, HumanTaskStatusDone)
			}
			if to, ok := HumanTaskTransitionAllowed(kind, from, HumanTaskActionCancel); !ok || to != HumanTaskStatusCancelled {
				t.Errorf("%s/%s cancel → (%q,%v)，期望 (%s,true)", kind, from, to, ok, HumanTaskStatusCancelled)
			}
		}
	}
	for _, from := range HumanTaskOpenStatuses {
		for _, action := range []string{HumanTaskActionComplete, HumanTaskActionCancel} {
			if to, ok := HumanTaskTransitionAllowed(HumanTaskKindApproval, from, action); ok {
				t.Errorf("审批类 %s 的 %s 被放行到 %s：待办侧关掉一条还在等的审批 = 拆闸门（裁决请走 /api/approvals/:id/decide）",
					from, action, to)
			}
		}
	}
}

// TestHumanTaskClaimReleaseMatrix 抢占差异逐格实测：只有会话类可以认领/释放。
//
// 这条断言守的是 C3 那句"审批不可抢占只能裁决"。把 claim 开放给审批类，等于在
// approval_requests 之外再造一条"谁有权批"的通路 —— 而那边根本不认认领这件事。
func TestHumanTaskClaimReleaseMatrix(t *testing.T) {
	cases := []struct {
		kind    string
		claimOK bool
	}{
		{HumanTaskKindConversationHandoff, true},
		{HumanTaskKindApproval, false},
		{HumanTaskKindCollectionEscalation, false},
	}
	for _, c := range cases {
		to, ok := HumanTaskTransitionAllowed(c.kind, HumanTaskStatusPending, HumanTaskActionClaim)
		if ok != c.claimOK {
			t.Errorf("%s pending claim → (%q,%v)，期望 ok=%v", c.kind, to, ok, c.claimOK)
		}
		if c.claimOK && to != HumanTaskStatusClaimed {
			t.Errorf("%s 认领应到 claimed，实际 (%q,%v)", c.kind, to, ok)
		}
		to, ok = HumanTaskTransitionAllowed(c.kind, HumanTaskStatusClaimed, HumanTaskActionRelease)
		if ok != c.claimOK {
			t.Errorf("%s claimed release → (%q,%v)，期望 ok=%v", c.kind, to, ok, c.claimOK)
		}
		if c.claimOK && to != HumanTaskStatusPending {
			t.Errorf("%s 释放应退回 pending，实际 (%q,%v)", c.kind, to, ok)
		}
	}
}

// TestHumanTaskTerminalsHaveNoWayOut 终态无出边（反向测试的主体）。
//
// 任何一条从 done/cancelled 出发的边都不许存在：处理完的事再动一次 = 同一件事两条处理记录，
// 待办中心的"我处理过几件"当场不可信。要再处理只能新建一行（那样次数才看得见）。
func TestHumanTaskTerminalsHaveNoWayOut(t *testing.T) {
	for _, kind := range HumanTaskKinds {
		for _, from := range []string{HumanTaskStatusDone, HumanTaskStatusCancelled} {
			for _, action := range HumanTaskActions {
				if to, ok := HumanTaskTransitionAllowed(kind, from, action); ok {
					t.Errorf("%s 从终态 %s 执行 %s 竟被放行到 %s", kind, from, action, to)
				}
			}
		}
	}
}

// TestHumanTaskUnknownValuesRejected 表里没有的路 = 拒，不是默认放行。
func TestHumanTaskUnknownValuesRejected(t *testing.T) {
	cases := []struct{ kind, from, action string }{
		{"", HumanTaskStatusPending, HumanTaskActionComplete},
		{HumanTaskKindConversationHandoff, "", HumanTaskActionComplete},
		{HumanTaskKindConversationHandoff, HumanTaskStatusPending, ""},
		{"handoff", HumanTaskStatusPending, HumanTaskActionClaim},   // 近似拼写：不该被认成 conversation_handoff
		{HumanTaskKindApproval, "expired", HumanTaskActionComplete}, // 已不存在的状态值
		{HumanTaskKindApproval, HumanTaskStatusPending, "approve"},  // 动作不是裁决，裁决归 approval_requests
		{HumanTaskKindConversationHandoff, HumanTaskStatusPending, "escalate"},
	}
	for _, c := range cases {
		if to, ok := HumanTaskTransitionAllowed(c.kind, c.from, c.action); ok {
			t.Errorf("(%q,%q,%q) 竟被放行到 %q", c.kind, c.from, c.action, to)
		}
	}
}

// TestHumanTaskTablesCoverEveryDeclaredValue 值域表与跃迁表不许各自漂移。
//
// 具体防的是：新加一个动作常量却忘了进跃迁表（那它永远不可用，而列表接口照样把它当合法动作），
// 或新加一个状态却没接边（那它是个黑洞）。
func TestHumanTaskTablesCoverEveryDeclaredValue(t *testing.T) {
	for _, kind := range HumanTaskKinds {
		for _, action := range HumanTaskActions {
			if !humanTaskActionKinds[action][kind] {
				continue
			}
			hit := false
			for _, from := range HumanTaskStatuses {
				if _, ok := HumanTaskTransitionAllowed(kind, from, action); ok {
					hit = true
				}
			}
			if !hit {
				t.Errorf("动作 %s 登记可用于 %s，却在状态机上无路可走", action, kind)
			}
		}
	}
	for _, action := range HumanTaskActions {
		if len(humanTaskActionKinds[action]) == 0 {
			t.Errorf("动作 %s 没有任何可用类别：等于没实现", action)
		}
		for kind := range humanTaskActionKinds[action] {
			if !HumanTaskKindKnown(kind) {
				t.Errorf("动作 %s 登记了未知类别 %q", action, kind)
			}
		}
	}
}

// TestHumanTaskSLAFieldPerKind AC① 后半句：SLA 字段独立，且由 kind 唯一决定。
func TestHumanTaskSLAFieldPerKind(t *testing.T) {
	cases := map[string]string{
		HumanTaskKindConversationHandoff:  "sla_first_response_at",
		HumanTaskKindApproval:             "sla_decide_at",
		HumanTaskKindCollectionEscalation: "sla_escalate_at",
	}
	seen := map[string]bool{}
	for kind, want := range cases {
		got := HumanTaskSLAField(kind)
		if got != want {
			t.Errorf("%s 的 SLA 列 = %q，期望 %q", kind, got, want)
		}
		if seen[got] {
			t.Errorf("两类共用 SLA 列 %q：一列两义正是 C3 说的污染", got)
		}
		seen[got] = true
	}
}

// TestHumanTaskSLAUnsupportedFields 填了别人的列要能被点名。
//
// 三档各测两种：只填自己那一列（合规，返回**空切片而非 nil**，端点直接渲染时不必判 nil）、
// 填了别家的一列或多列（逐列点名）。
func TestHumanTaskSLAUnsupportedFields(t *testing.T) {
	at := func() *time.Time {
		v := time.Date(2026, 9, 20, 10, 0, 0, 0, time.UTC)
		return &v
	}
	cases := []struct {
		name string
		task HumanTask
		want []string
	}{
		{
			name: "会话档只填首响",
			task: HumanTask{Kind: HumanTaskKindConversationHandoff, SlaFirstResponseAt: at()},
			want: []string{},
		},
		{
			name: "会话档串了审批档",
			task: HumanTask{Kind: HumanTaskKindConversationHandoff, SlaDecideAt: at()},
			want: []string{"sla_decide_at"},
		},
		{
			name: "审批档串了另两档",
			task: HumanTask{Kind: HumanTaskKindApproval, SlaFirstResponseAt: at(), SlaEscalateAt: at()},
			want: []string{"sla_first_response_at", "sla_escalate_at"},
		},
		{
			name: "催收档只填升级",
			task: HumanTask{Kind: HumanTaskKindCollectionEscalation, SlaEscalateAt: at()},
			want: []string{},
		},
		{
			name: "三档全填（会话档视角）",
			task: HumanTask{Kind: HumanTaskKindConversationHandoff, SlaFirstResponseAt: at(), SlaDecideAt: at(), SlaEscalateAt: at()},
			want: []string{"sla_decide_at", "sla_escalate_at"},
		},
		{
			name: "一列不填也算合规",
			task: HumanTask{Kind: HumanTaskKindApproval},
			want: []string{},
		},
		{
			// 判的是"填了别人的列"，不是"哪些列是别人的"：未知类别没有自己的列，
			// 于是它填上的那一列就是越界的那一列（只点名真填了的）。
			name: "未知类别：填了的列一律越界",
			task: HumanTask{Kind: "escalation", SlaEscalateAt: at()},
			want: []string{"sla_escalate_at"},
		},
		{
			name: "未知类别：三列全填则全越界",
			task: HumanTask{Kind: "escalation", SlaFirstResponseAt: at(), SlaDecideAt: at(), SlaEscalateAt: at()},
			want: []string{"sla_first_response_at", "sla_decide_at", "sla_escalate_at"},
		},
	}
	for _, c := range cases {
		got := HumanTaskSLAUnsupportedFields(&c.task)
		if got == nil {
			t.Errorf("%s：返回 nil，期望空切片（端点渲染要判 nil）", c.name)
			continue
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s：越界列 = %v，期望 %v", c.name, got, c.want)
		}
	}
}
