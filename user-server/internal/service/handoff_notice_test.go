package service

import (
	"context"
	"os"
	"regexp"
	"strings"
	"testing"

	agent_runtime "hivemtk-user/internal/aiagent/agent/runtime"
)

// 转人工那轮该不该给客户一句话、给哪一句，是这组用例锁死的三件事：
// 客户照兜底文案回了「转人工」以后最怕的两种形状 —— 一句回执都没有（死路），
// 以及回执把没发生的事说成发生了（没人接却说"已转接"）。

// TestTransferToHuman_NoticeOnlyOnNewHandoff 首轮转接给公告、已在人工手里的那轮不给。
func TestTransferToHuman_NoticeOnlyOnNewHandoff(t *testing.T) {
	database := newHandoffProduceDB(t)
	o := newHandoffOrchestrator(t, database)
	ctx := context.Background()

	session := seedHandoffSession(t, database, "sess_notice_first", "oneid_notice_1")
	// newHandoffOrchestrator 显式把 assignmentSvc 摘了（自动分派与这组用例的判据无关），
	// 所以这里走的是"没人被叫上线"那一档 —— 断言它给的是排队文案而不是"已转接"。
	result := &HandleResult{}
	if err := o.transferToHuman(ctx, session, "客户显式要求转人工", result); err != nil {
		t.Fatalf("转人工失败: %v", err)
	}
	if result.TransferNotice != agent_runtime.HandoffQueueNotice {
		t.Errorf("首轮公告 = %q，期望排队档 %q", result.TransferNotice, agent_runtime.HandoffQueueNotice)
	}
	if result.TransferNotice == "" {
		t.Error("首轮转接没有任何公告 ⇒ 客户回了「转人工」依旧听不见回声")
	}

	// session 此刻已被改成人处理中；真实场景是下一轮入站重新读出的那一行。
	again := &HandleResult{}
	if err := o.transferToHuman(ctx, session, "客户显式要求转人工", again); err != nil {
		t.Fatalf("第二次调用失败: %v", err)
	}
	if again.TransferNotice != "" {
		t.Errorf("已在人工手里仍播报 %q ⇒ 客户等待期每发一条就多一句公告", again.TransferNotice)
	}

	// result 传 nil 是后台补投待办那一类调用方，不许因此炸掉。
	if err := o.transferToHuman(ctx, seedHandoffSession(t, database, "sess_notice_nil", "oneid_notice_2"), "低置信度", nil); err != nil {
		t.Fatalf("result 为 nil 的调用失败: %v", err)
	}
}

// TestHandoffNoticeFor_PicksCopyByAssignmentOutcome 两档文案按"有没有真叫上坐席"分流。
func TestHandoffNoticeFor_PicksCopyByAssignmentOutcome(t *testing.T) {
	if got := handoffNoticeFor(true); got != agent_runtime.HandoffCustomerNotice {
		t.Errorf("assigned=true 给的是 %q，期望 %q", got, agent_runtime.HandoffCustomerNotice)
	}
	if got := handoffNoticeFor(false); got != agent_runtime.HandoffQueueNotice {
		t.Errorf("assigned=false 给的是 %q，期望 %q", got, agent_runtime.HandoffQueueNotice)
	}
	if agent_runtime.HandoffCustomerNotice == agent_runtime.HandoffQueueNotice {
		t.Error("两档文案相同 ⇒ 分档白做，客户与运营都读不出到底有没有人被叫上线")
	}
}

// TestHandoffNotices_DoNotLeakInternalReason 公告里不许出现内部判据原文。
// TransferReason 那些串（置信度、引擎、连续回复上限）是给 handoff_decisions 与待办标题看的。
func TestHandoffNotices_DoNotLeakInternalReason(t *testing.T) {
	both := []string{agent_runtime.HandoffCustomerNotice, agent_runtime.HandoffQueueNotice}
	for _, internal := range []string{"置信度", "引擎", "上限", "降级", "待办", "veto"} {
		for _, notice := range both {
			if strings.Contains(notice, internal) {
				t.Errorf("公告 %q 含内部判据字样 %q", notice, internal)
			}
		}
	}
}

// TestHandoffNotices_NotCountedAsDegraded 转接公告不许被压测脚本识别成"AI 降级回复"。
//
// 判据的**唯一事实源**是 scripts/simulate/ai_quality.py 的 DEGRADED_MARKERS：
// 这里现场把它抽出来比对，而不是在 Go 里另抄一份清单 —— 抄的那份会随 Python 侧改动失真，
// 而这类"看着像有用的死清单"正是历史上把正常应答读成降级、把降级读成正常的那只手。
func TestHandoffNotices_NotCountedAsDegraded(t *testing.T) {
	markers := loadDegradedMarkers(t)
	if len(markers) == 0 {
		t.Fatal("一条降级锚都没抽到 ⇒ 抽取脚本失效，本用例退成空跑（宁可红，不可假绿）")
	}
	for _, notice := range []string{agent_runtime.HandoffCustomerNotice, agent_runtime.HandoffQueueNotice} {
		for _, m := range markers {
			if strings.Contains(notice, m) {
				t.Errorf("公告 %q 命中降级锚 %q ⇒ 压测会把这条正常转接回执统计成 AI 降级", notice, m)
			}
		}
	}
}

func loadDegradedMarkers(t *testing.T) []string {
	t.Helper()
	const path = "../../scripts/simulate/ai_quality.py"
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读不到 %s（事实源挪位就去看它搬去哪了，别在这儿改路径凑数）: %v", path, err)
	}
	block := regexp.MustCompile(`(?s)DEGRADED_MARKERS\s*=\s*\[(.*?)\]`).FindSubmatch(raw)
	if len(block) != 2 {
		t.Fatalf("%s 里没找到 DEGRADED_MARKERS = [...] 这个形状，判据已失效", path)
	}
	var markers []string
	for _, m := range regexp.MustCompile(`"([^"]+)"`).FindAllStringSubmatch(string(block[1]), -1) {
		markers = append(markers, m[1])
	}
	return markers
}
