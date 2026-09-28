package tooluse

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// TestApprovalDeniedError_NamesTheKeyItChecked 拒绝提示必须带上**实际用于匹配的那个账号键**。
//
// 只说 "requires cold outreach approval" 时两种故障长得一模一样：
//   - owner=u12 确实没被授权 ⇒ 去授权；
//   - 这次调用压根没带身份（CallerID/AgentID 都空）⇒ 授权点到天亮也照旧被拒，
//     要修的是调用链上游传的上下文。
//
// 操作者按第一种读法处置，会在白名单里反复加同一个账号而看不到效果 —— 而现象（一直拒）
// 恰好不支持他怀疑第二种。
//
// 这也是 internal/approval 那边同一条纪律的延伸：那边的理由串会带 owner_id（空的话显示
// 成 ""），带 reason 的回调本来就是为归因存在的；装饰器这条主路径不给键，等于把已有的
// 那份事实源在出口处截掉。

// deniedHarness 同一份判据走两条接线形态：审批门挂链走装饰器（生产路径），
// Tool 包装（WithApprovalChecker）是另一条 —— 两条给的拒绝必须是同一句话，
// 否则换一种接线方式，操作者读到的处置方向就换一次。
func TestApprovalDeniedError_NamesTheKeyItChecked(t *testing.T) {
	cases := []struct {
		name    string
		tc      *ToolContext
		wantKey string
		// wantEmptyTail 空键那一支要说的话与"没被授权"正相反，两句不许互相冒充。
		wantEmptyTail bool
	}{
		{"CallerID 就是被问的那个键", &ToolContext{CallerID: "acct-9527"}, "acct-9527", false},
		// CallerID 空时回落到 AgentID：提示里必须是**回落之后**的那个值，
		// 写死 CallerID 的用例测不到这条（而它恰是"我明明给了账号为什么还拒"的一种真因）。
		{"CallerID 空 ⇒ 回落 AgentID", &ToolContext{AgentID: "agent-7"}, "agent-7", false},
		{"两个都空 ⇒ 说清没带身份", &ToolContext{}, "", true},
		{"没有 ToolContext ⇒ 同一句", nil, "", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ctx := context.Background()
			if c.tc != nil {
				ctx = WithToolContext(ctx, c.tc)
			}
			checker := &fakeApprovalChecker{approved: false}
			// runGate 走装饰器那条路，其夹具必须是 countingColdTool（它按这个类型取内层 handler）；
			// 包装那条路用 stubColdTool —— 两者同名同类别，才允许直接比两句拒绝文案。
			// 只取 err：被拒时 result 的 Success/Error 由 ErrorResult 从同一个 err 派生，
			// 再断言一遍是恒真式，锁不住任何东西。
			_, err := runGate(t, newCountingColdTool("reach.telegram.dm", CategoryReach), checker, false, ctx)
			if !errors.Is(err, ErrApprovalDenied) {
				t.Fatalf("装饰器路径 err = %v, want ErrApprovalDenied", err)
			}
			gateMsg := err.Error()

			// Tool 包装那条路必须说同一句话：直接比串，不各写一份判据。
			wrapped := WithApprovalChecker(newStubColdTool(), checker)
			_, err2 := wrapped.Execute(ctx, nil)
			if !errors.Is(err2, ErrApprovalDenied) {
				t.Fatalf("包装路径 err = %v, want ErrApprovalDenied", err2)
			}
			if err2.Error() != gateMsg {
				t.Errorf("两条接线形态的拒绝文案分叉了：装饰器=%q 包装=%q", gateMsg, err2.Error())
			}

			if c.wantEmptyTail {
				if !strings.Contains(gateMsg, "没有带上可归属的账号身份") {
					t.Errorf("空键时要说的是\"没带身份\"：%q", gateMsg)
				}
				// 反向对照：这一支不许同时带上"按 owner=... 查过"那句有键的话，
				// 否则两种故障又并回了一句。
				if strings.Contains(gateMsg, "白名单按 owner=") {
					t.Errorf("空键那一支不该出现带键的那句：%q", gateMsg)
				}
				// 空键那一支到此为止：下面两格判的是"带了键"的那一侧。
				return
			}
			if !strings.Contains(gateMsg, "owner=\""+c.wantKey+"\"") {
				t.Errorf("提示里要有实际被匹配的键 %q：%q", c.wantKey, gateMsg)
			}
			if strings.Contains(gateMsg, "没有带上可归属的账号身份") {
				t.Errorf("带了键的这一次不该说\"没带身份\"（两句是两种故障）：%q", gateMsg)
			}
		})
	}
}

// TestApprovalDeniedError_KeysAreWhatWasAsked 提示里的键与真的问给 checker 的键同源：
// 记一份假 checker 收到的参数，与错误串里的键逐字比。
// 少了这一格，"提示说了另一个键"（比如另算一次 approvalOwnerKey）在前面那几格照样绿。
func TestApprovalDeniedError_KeysAreWhatWasAsked(t *testing.T) {
	ctx := WithToolContext(context.Background(), &ToolContext{CallerID: "u1", AgentID: "agent-ignored"})
	checker := &fakeApprovalChecker{approved: false}

	_, err := runGate(t, newCountingColdTool("reach.telegram.dm", CategoryReach), checker, false, ctx)
	if !errors.Is(err, ErrApprovalDenied) {
		t.Fatalf("err = %v", err)
	}
	if checker.gotOwner != "u1" {
		t.Fatalf("前置条件不成立：checker 收到的键应为 u1，实际 %q", checker.gotOwner)
	}
	if !strings.Contains(err.Error(), "owner=\""+checker.gotOwner+"\"") {
		t.Errorf("提示里的键与真问给 checker 的那份不一致：%q vs %q", err.Error(), checker.gotOwner)
	}
}
