package service

// executor_split_test.go —— A1 拆分泳道补测：对拆分后落入 executor_helpers.go 的纯函数
// 做不连 DB 的单元锁定。这些函数此前分散在 1901 行的 executor.go 里、且没有直接测试；
// 拆分后集中于此，补上直接断言，防止后续移动/改写时静默漂移。

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/model"
)

func TestTaskPlatformIDNoFallback(t *testing.T) {
	// B21：task.platform 是权威，**空值不再回落 xiaohongshu**。
	// 旧实现返回 "xiaohongshu" 是 fail-open：漏填平台的抖音任务会静默拿小红书的
	// 定位表去点抖音页面。现在空值原样返回 ""，下游 platform.Get("") 报错（fails-loudly）。
	if got := taskPlatformID(&model.BrowserTask{}); got != "" {
		t.Errorf("空平台不得兜底，want \"\"，got %q", got)
	}
	if got := taskPlatformID(&model.BrowserTask{Platform: "  "}); got != "" {
		t.Errorf("纯空白平台不得兜底，want \"\"，got %q", got)
	}
	if got := taskPlatformID(&model.BrowserTask{Platform: "  douyin "}); got != "douyin" {
		t.Errorf("平台名应去空白，got %q", got)
	}
	if got := taskPlatformID(&model.BrowserTask{Platform: "xianyu"}); got != "xianyu" {
		t.Errorf("显式平台应原样透传，got %q", got)
	}
}

func TestSendErrText(t *testing.T) {
	if sendErrText(nil) != "" {
		t.Errorf("nil 应回空串，got %q", sendErrText(nil))
	}
	if got := sendErrText(errors.New("boom")); got != "boom" {
		t.Errorf("错误应回原文，got %q", got)
	}
}

func TestLastN(t *testing.T) {
	s := []string{"a", "b", "c", "d"}
	got := lastN(s, 2)
	if len(got) != 2 || got[0] != "c" || got[1] != "d" {
		t.Errorf("lastN(s,2) = %v，want [c d]", got)
	}
	if len(lastN(s, 10)) != 4 {
		t.Errorf("n 超长应原样返回")
	}
	if lastN(nil, 1) != nil {
		t.Errorf("nil 切片应原样返回 nil")
	}
}

func TestHistoryActionOf(t *testing.T) {
	if got := historyActionOf("[已折叠 1 步: x×1]"); got != "compacted" {
		t.Errorf("折叠行应归 compacted，got %q", got)
	}
	if got := historyActionOf("click #btn → success"); got != "click" {
		t.Errorf("首词应为动作名，got %q", got)
	}
	if got := historyActionOf("wait"); got != "wait" {
		t.Errorf("无空格行应原样返回，got %q", got)
	}
}

func TestAuditEventPayloadDropsEvidence(t *testing.T) {
	blob := auditEventPayload([]byte(`{"evidence":"正文节选","state":"verified"}`))
	var m map[string]any
	if err := json.Unmarshal(blob, &m); err != nil {
		t.Fatalf("回包应是合法 JSON: %v", err)
	}
	if _, ok := m["evidence"]; ok {
		t.Errorf("审计流不应带 evidence 键: %s", blob)
	}
	if m["state"] != "verified" {
		t.Errorf("evidence 之外的键应保留: %s", blob)
	}
	// 无 evidence 的对象 / 非对象 / 空：原样透传
	orig := []byte(`{"a":1}`)
	if got := string(auditEventPayload(orig)); got != string(orig) {
		t.Errorf("无 evidence 对象应原样，got %s", got)
	}
	nonObj := []byte(`[1,2]`)
	if got := string(auditEventPayload(nonObj)); got != string(nonObj) {
		t.Errorf("非对象回包应原样，got %s", got)
	}
	if got := auditEventPayload(nil); got != nil {
		t.Errorf("空回包应原样 nil，got %v", got)
	}
}

func TestSleepInterruptible(t *testing.T) {
	// d<=0：不睡眠，只反映 ctx 是否已取消
	if !sleepInterruptible(context.Background(), nil, 0) {
		t.Error("d<=0 且 ctx 未取消应返回 true")
	}
	cctx, cancel := context.WithCancel(context.Background())
	cancel()
	if sleepInterruptible(cctx, nil, 0) {
		t.Error("d<=0 且 ctx 已取消应返回 false")
	}
	// 正时长：stopCh / ctx 已关闭即被打断
	stop := make(chan struct{})
	close(stop)
	if sleepInterruptible(context.Background(), stop, time.Minute) {
		t.Error("stopCh 已关闭应立即返回 false")
	}
	cctx2, cancel2 := context.WithCancel(context.Background())
	cancel2()
	if sleepInterruptible(cctx2, make(chan struct{}), time.Minute) {
		t.Error("ctx 已取消应立即返回 false")
	}
	// 无打断：短时长睡满返回 true
	if !sleepInterruptible(context.Background(), make(chan struct{}), 10*time.Millisecond) {
		t.Error("无打断应睡满返回 true")
	}
}
