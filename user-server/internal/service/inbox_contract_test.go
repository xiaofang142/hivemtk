package service

import (
	"errors"
	"testing"
	"time"
)

// LM-16 正例：稳定游标按时间+ID 排序，同时间不漂移。
func TestInboxMessageCursor_Stable(t *testing.T) {
	ts := time.Unix(100, 0)
	a := inboxMessageCursor(ts, 2)
	b := inboxMessageCursor(ts, 10)
	if a == b || a >= b {
		t.Fatalf("同时间消息游标必须按 ID 区分且稳定: %s %s", a, b)
	}
	if inboxMessageCursor(ts.Add(time.Second), 1) <= b {
		t.Fatal("时间优先的游标顺序错误")
	}
}

// LM-16 反例：来源失败显示 unavailable，与正常无消息分开；缺失通知不等于无消息。
func TestInboxSourceState_FailureVsEmpty(t *testing.T) {
	if got := inboxSourceState(nil); got != "complete" {
		t.Fatalf("正常查询应为 complete，实际 %q", got)
	}
	if got := inboxSourceState(errors.New("db down")); got != "unavailable" {
		t.Fatalf("查询失败应显示不可用，实际 %q", got)
	}
}
