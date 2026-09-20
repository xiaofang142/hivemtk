package service

// 商机段取数失败的**告警**行为锁（T-P4-06）。
//
// 为什么单独给这一段加告警锁、而前四段没有：本卡把 opportunities 接进漏斗之后，
// 这一格的 0 有两条来路（真没商机 / 表没建起来或接缝没装配），而看板不区分它们。
// 「未告警即视同成功」是本项目的验收口径 R-①，所以告警本身就是要守的行为，
// 不是可选的调试输出 —— 把它删掉必须有一条用例红。
//
// 抓日志的写法照 internal/app/tool_circuit_breaker_wiring_test.go：换 os.Stdout 之外
// 还必须重建全局日志器，因为 GetLogger 会把实例连同当时的 os.Stdout 一起缓存。

import (
	"io"
	"os"
	"strings"
	"testing"

	"hivemtk-user/internal/pkg/utils/logger"
)

// captureFunnelLogs 把全局日志器指向管道，返回"关掉写端并读回全部已写内容"的闭包。
// 还原顺序与设置相反（先 InitLogger 再 os.Stdout），且都在 t.Cleanup 里 ——
// 同一个测试包里后面的用例还要用真 stdout。
func captureFunnelLogs(t *testing.T) func() string {
	t.Helper()
	oldOut := os.Stdout
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("管道创建失败：%v", err)
	}
	os.Stdout = w
	logger.InitLogger(logger.LoggingConfig{Level: "info", Format: "json", Output: "stdout"})
	restoreDone := false
	t.Cleanup(func() {
		if restoreDone {
			return
		}
		_ = w.Close()
		os.Stdout = oldOut
		logger.InitLogger(logger.DefaultConfig())
	})

	return func() string {
		t.Helper()
		if err := w.Close(); err != nil {
			t.Fatalf("关闭写端失败：%v", err)
		}
		os.Stdout = oldOut
		logger.InitLogger(logger.DefaultConfig())
		restoreDone = true
		captured, readErr := io.ReadAll(r)
		if readErr != nil {
			t.Fatalf("读取日志失败：%v", readErr)
		}
		return string(captured)
	}
}

func TestConversionFunnel_OpportunityLegFailureIsWarned(t *testing.T) {
	database := setupFunnelTestDB(t)
	seedFunnelRows(t, database)
	// 只破商机这一条腿：其余四段的数据源保持可用。
	if err := database.Exec(`DROP TABLE IF EXISTS opportunities`).Error; err != nil {
		t.Fatalf("删除 opportunities 失败：%v", err)
	}

	svc := NewConversionFunnelService()
	readLogs := captureFunnelLogs(t)

	report, err := svc.BuildFunnel(funnelFrom, funnelTo)
	if err != nil {
		t.Fatalf("商机段取数失败不应外抛，实际 err=%v", err)
	}
	det, err := svc.GetStageDetails("opportunity", funnelFrom, funnelTo)
	if err != nil {
		t.Fatalf("商机详情取数失败不应外抛，实际 err=%v", err)
	}
	logged := readLogs()
	t.Logf("捕获到的漏斗日志：\n%s", logged)

	// 两条腿（汇总 + 详情）各须一条告警：少一条就是"某一条腿的失败悄悄无声"。
	if got := strings.Count(logged, "商机段取数失败"); got != 1 {
		t.Errorf("汇总腿的取数告警应恰好 1 条，实际 %d 条", got)
	}
	if got := strings.Count(logged, "商机详情取数失败"); got != 1 {
		t.Errorf("详情腿的取数告警应恰好 1 条，实际 %d 条", got)
	}
	if !strings.Contains(logged, `"level":"warn"`) {
		t.Errorf("告警须为 warn 级（error 会误升为故障、info 在生产级别下等于不告警）：\n%s", logged)
	}
	// 告警得带上真正的失败原因，否则运维只看得到"有问题"而看不到是什么问题。
	if !strings.Contains(logged, "opportunities") {
		t.Errorf("告警内容应包含失败原因（含表名 opportunities），实际：%s", logged)
	}

	// 与既有那条锁互为对照：告警不影响返回值。
	// 按阶段名取段而不是按下标 —— 本卡刚在前端和另一条用例里修掉的就是"末段"这个位置假设。
	var leg *FunnelStage
	for i := range report.Stages {
		if report.Stages[i].Stage == "opportunity" {
			leg = &report.Stages[i]
		}
	}
	if leg == nil {
		t.Fatal("响应里没有商机段（阶段名漂移了？）")
	}
	if leg.Count != 0 || det.Count != 0 {
		t.Fatalf("取数失败时商机两段都应回 0，实际 汇总=%d 详情=%d", leg.Count, det.Count)
	}
}
