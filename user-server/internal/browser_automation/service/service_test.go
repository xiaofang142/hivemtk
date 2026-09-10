package service

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/browser_automation/model"
)

func TestValidateURL(t *testing.T) {
	cases := []struct {
		url    string
		wantOK bool
	}{
		{"https://example.com", true},
		{"http://example.com/path?q=1", true},
		{"https://example.com:8080/x", true},
		{"chrome://extensions", false},
		{"file:///etc/passwd", false},
		{"ftp://example.com", false},
		{"javascript:alert(1)", false},
		{"", false},
		{"not-a-url", false},
		{"http://", false},
	}
	for _, c := range cases {
		err := ValidateURL(c.url)
		if (err == nil) != c.wantOK {
			t.Errorf("ValidateURL(%q) err=%v wantOK=%v", c.url, err, c.wantOK)
		}
	}
}

func TestToSixField(t *testing.T) {
	cases := map[string]string{
		"*/5 * * * *":   "0 */5 * * * *",
		"0 9 * * 1-5":   "0 0 9 * * 1-5",
		"0 */5 * * * *": "0 */5 * * * *", // 已是 6 段不动
	}
	for in, want := range cases {
		if got := toSixField(in); got != want {
			t.Errorf("toSixField(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidateCronExpr(t *testing.T) {
	valid := []string{"*/5 * * * *", "0 9 * * 1-5", "30 3 * * *"}
	invalid := []string{"bad", "* * *", "60/5 * * * *"}
	for _, e := range valid {
		if err := ValidateCronExpr(e); err != nil {
			t.Errorf("ValidateCronExpr(%q) unexpected err: %v", e, err)
		}
	}
	for _, e := range invalid {
		if err := ValidateCronExpr(e); err == nil {
			t.Errorf("ValidateCronExpr(%q) should fail", e)
		}
	}
}

func TestParseSteps(t *testing.T) {
	steps, err := ParseSteps([]byte(`[{"action":"click","target":"#submit","retry_count":2},{"action":"wait","ms":1000}]`))
	if err != nil {
		t.Fatalf("ParseSteps err: %v", err)
	}
	if len(steps) != 2 {
		t.Fatalf("want 2 steps, got %d", len(steps))
	}
	if steps[0].RetryCount != 2 || steps[0].Target != "#submit" {
		t.Errorf("step0 解析错误: %+v", steps[0])
	}
	if steps[1].Ms != 1000 {
		t.Errorf("step1 Ms 解析错误: %+v", steps[1].Ms)
	}

	// 空 steps
	empty, err := ParseSteps(nil)
	if err != nil || len(empty) != 0 {
		t.Errorf("ParseSteps(nil) = %v, %v", empty, err)
	}

	// 非法 JSON
	if _, err := ParseSteps([]byte(`{bad`)); err == nil {
		t.Error("ParseSteps(bad json) should fail")
	}
}

// ---- Brain 迭代上限常量与 plan 解析 ----

func TestMaxBrainIterationsBound(t *testing.T) {
	if maxBrainIterations <= 0 || maxBrainIterations > 50 {
		t.Errorf("maxBrainIterations = %d, 应在 1..50", maxBrainIterations)
	}
}

// executeBrain 空转测试：brain=nil 直接报“Brain 服务未装配”，不开任何 Host 命令
func TestExecuteBrainNilBrain(t *testing.T) {
	e := &Executor{}
	task := &model.BrowserTask{BrainMode: true, BrainGoal: "g", TimeoutSec: 10, Url: "https://example.com"}
	session := &model.BrowserSession{}
	stopCh := make(chan struct{})

	success, failed, msg := e.executeBrain(context.Background(), task, session, stopCh)
	if success != 0 || failed != 0 {
		t.Errorf("nil brain 应 0/0，got %d/%d", success, failed)
	}
	if msg == "" {
		t.Error("nil brain 应返回失败原因")
	}
}

// SetRetryRunner 装配后 runRetry 走注入函数
func TestSetRetryRunnerWiring(t *testing.T) {
	called := make(chan int, 1)
	SetRetryRunner(func(_ context.Context, tid, _ uint, _ int) error {
		called <- int(tid)
		return nil
	})
	defer SetRetryRunner(nil)

	f := &FeedbackService{}
	task := &model.BrowserTask{ID: 77}
	if err := f.runRetry(context.Background(), task, 2); err != nil {
		t.Fatalf("runRetry err: %v", err)
	}
	select {
	case id := <-called:
		if id != 77 {
			t.Errorf("runner 收到 taskID=%d, want 77", id)
		}
	case <-time.After(time.Second):
		t.Fatal("runner 未被调用")
	}

	// 未装配时报错而非 panic
	SetRetryRunner(nil)
	f2 := &FeedbackService{}
	if err := f2.runRetry(context.Background(), task, 1); err == nil {
		t.Error("未装配 runner 应返回错误")
	}
}

// cron 触发体使用 trigger 主键更新（cur.ID）—— 静态约束回归：防再传 taskID
func TestCronUpdateTimesUsesTriggerID(t *testing.T) {
	src, err := os.ReadFile("cron.go")
	if err != nil {
		t.Skip("cron.go 不可读")
	}
	if !strings.Contains(string(src), "UpdateTimes(ctx, cur.ID") {
		t.Error("registerWithUser 必须以 cur.ID（trigger 主键）更新 last_run_at，防再传 taskID 错改其他 trigger")
	}
}
