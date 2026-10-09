// human_task_sla_wiring_test.go I2：待办 SLA 超时闭环装配层的实跑。
//
// 只测装配层能答的三件事：旗子解析的档位边界、off 档真的什么都不装、
// 以及"从 enforce 切回 off"时上一份扫描协程会被停掉（不是留在进程里继续发提醒）。
// 扫描/去重/升级的行为由 service 侧 human_task_sla_job_test.go 钉住。
package app

import (
	"testing"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
)

func humanTaskSLATestDB(t *testing.T) *gorm.DB {
	t.Helper()
	return testutil.NewTestDB(t, &model.HumanTask{}, &model.Notification{})
}

func TestParseHumanTaskSLAMode(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{"", "off"},
		{"off", "off"},
		{"false", "off"},
		{"0", "off"},
		{"no", "off"},
		{"none", "off"},
		{"disabled", "off"},
		{"shadow", "shadow"},
		{"observe", "shadow"},
		{"watch", "shadow"},
		{"sweep", "shadow"},
		{"log", "shadow"},
		{"  SHADOW  ", "shadow"},
		{"enforce", "enforce"},
		{"on", "enforce"},
		{"active", "enforce"},
		{"run", "enforce"},
		{"  ENFORCE ", "enforce"},
		// 布尔真值只到 shadow：按习惯写 =true 不该拿到"真写提醒"的能力。
		{"true", "shadow"},
		{"1", "shadow"},
		{"t", "shadow"},
		// 认不出的值判 off（含 yes —— ParseBool 不认它）。
		{"yes", "off"},
		{"banana", "off"},
		{"  ", "off"},
	}
	for _, c := range cases {
		if got := string(parseHumanTaskSLAMode(c.raw)); got != c.want {
			t.Errorf("parseHumanTaskSLAMode(%q)=%s，期望 %s", c.raw, got, c.want)
		}
	}
}

func TestInitHumanTaskSLA_OffDoesNotAssemble(t *testing.T) {
	t.Setenv(HumanTaskSLAJobFlagEnv, "off")
	t.Cleanup(StopHumanTaskSLA)
	database := humanTaskSLATestDB(t)

	if rt := InitHumanTaskSLA(database); rt != nil {
		t.Fatalf("off 档应返回 nil，实得 %+v", rt)
	}
	if currentHumanTaskSLARuntime() != nil {
		t.Error("off 档不该在全局留下运行时")
	}
}

func TestInitHumanTaskSLA_EnforceAssemblesAndStops(t *testing.T) {
	t.Setenv(HumanTaskSLAJobFlagEnv, "enforce")
	t.Cleanup(StopHumanTaskSLA)
	database := humanTaskSLATestDB(t)

	rt := InitHumanTaskSLA(database)
	if rt == nil {
		t.Fatal("enforce 档应装配运行时")
	}
	if rt.Mode() != "enforce" {
		t.Fatalf("Mode=%s，期望 enforce", rt.Mode())
	}
	if currentHumanTaskSLARuntime() != rt {
		t.Error("全局那份与返回的那份不是同一对象")
	}
	StopHumanTaskSLA()
	if currentHumanTaskSLARuntime() != nil {
		t.Error("Stop 后全局引用应清空")
	}
}

// 从 enforce 切回 off：不仅本次不装配，上一份扫描协程也必须被停掉 ——
// 否则运维把旗子关掉后，坐席还会继续收到提醒。
func TestInitHumanTaskSLA_SwitchingToOffStopsPrevious(t *testing.T) {
	t.Cleanup(StopHumanTaskSLA)
	database := humanTaskSLATestDB(t)

	t.Setenv(HumanTaskSLAJobFlagEnv, "enforce")
	rt := InitHumanTaskSLA(database)
	if rt == nil {
		t.Fatal("enforce 档应装配运行时")
	}

	t.Setenv(HumanTaskSLAJobFlagEnv, "off")
	if got := InitHumanTaskSLA(database); got != nil {
		t.Fatalf("切回 off 应返回 nil，实得 %+v", got)
	}
	if currentHumanTaskSLARuntime() != nil {
		t.Error("切回 off 后全局不该留有运行时")
	}
}
