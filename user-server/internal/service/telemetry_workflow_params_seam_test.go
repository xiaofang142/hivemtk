package service

import (
	"testing"
)

// telemetry+workflow 本批加的 seam 逐格钉死：未注入 == 代码兜底、注入生效、
// 非法值（非正数/非正 duration）回落兜底、setter 传 nil 等于撤销注入。
//
// 非正值回落这一格不是形式主义：每一条配 0 都会把对应能力整条废掉，而且废法各不相同
// ——子流程深度 0 会让所有含子流程的工作流失败、SOP 并发 0 会让调度器永久停摆、
// 轨迹条数 0 会让执行在界面上完全失明。所以每条都单独写清它坏成什么样。
func TestTelemetryWorkflowIntSeams(t *testing.T) {
	cases := []struct {
		name     string
		defaultV int
		inject   func(func() int)
		read     func() int
		why      string
	}{
		{
			name: "MaxSubflowDepth", defaultV: DefaultMaxSubflowDepth,
			inject: SetMaxSubflowDepthProvider, read: MaxSubflowDepth,
			why: "0 会让第一层子流程就判超限，含子流程的工作流全部失败",
		},
		{
			name: "maxWorkflowSteps", defaultV: DefaultMaxWorkflowSteps,
			inject: SetMaxWorkflowStepsProvider, read: maxWorkflowSteps,
			why: "0 会在第一步就判超限，所有工作流全部失败",
		},
		{
			name: "maxRunningPerSOP", defaultV: DefaultMaxRunningPerSOP,
			inject: SetMaxRunningPerSOPProvider, read: maxRunningPerSOP,
			why: "0 会让 count<=0 恒成立，调度器对所有 SOP 永久停摆",
		},
		{
			name: "maxExecutedNodeTrace", defaultV: DefaultMaxExecutedNodeTrace,
			inject: SetMaxExecutedNodeTraceProvider, read: maxExecutedNodeTrace,
			why: "0 会让第一条轨迹就被丢弃，整次执行在界面上看不到任何节点",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			reset := func() { tc.inject(nil) }
			t.Cleanup(reset)
			reset()

			if got := tc.read(); got != tc.defaultV {
				t.Fatalf("未注入时 = %v，期望代码兜底 %v", got, tc.defaultV)
			}

			tc.inject(func() int { return 7 })
			if got := tc.read(); got != 7 {
				t.Errorf("注入 7 未生效：got %v", got)
			}

			tc.inject(func() int { return 0 })
			if got := tc.read(); got != tc.defaultV {
				t.Errorf("注入 0 未回落兜底：got %v，期望 %v（%s）", got, tc.defaultV, tc.why)
			}
			tc.inject(func() int { return -3 })
			if got := tc.read(); got != tc.defaultV {
				t.Errorf("注入 -3 未回落兜底：got %v，期望 %v", got, tc.defaultV)
			}

			tc.inject(nil)
			if got := tc.read(); got != tc.defaultV {
				t.Errorf("传 nil 撤销注入后 = %v，期望 %v", got, tc.defaultV)
			}
		})
	}
}

func TestNodeHealthWindowAndPollIntervalSeams(t *testing.T) {
	// 这两条在别的包（internal/monitor / internal/pkg/featureflag），
	// 各自包里有自己的用例；这里只钉 internal/service 内不重复的部分：
	// MaxSubflowDepth 是导出读取口，ProbeMaxSubflowDepth 必须与它一致。
	// 装配层测试用 ProbeMaxSubflowDepth，钉住两者不会各走各的。
	reset := func() { SetMaxSubflowDepthProvider(nil) }
	t.Cleanup(reset)
	reset()

	SetMaxSubflowDepthProvider(func() int { return 11 })
	if MaxSubflowDepth() != 11 || ProbeMaxSubflowDepth() != 11 {
		t.Errorf("MaxSubflowDepth=%v ProbeMaxSubflowDepth=%v，应同为 11", MaxSubflowDepth(), ProbeMaxSubflowDepth())
	}
}
