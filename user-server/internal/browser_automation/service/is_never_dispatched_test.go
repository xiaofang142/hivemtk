// isNeverDispatched —— CDP attach 阶段未成功 ⇒ 事件从未下发 ⇒ 零副作用。
//
// 这条分类是「修好挂死」的必要配套：cdp/input.js 的 attach deadline 让 comment_send
// 的失败结局第一次有了「一次都没点」这一形态，而 send 路径的归因链原本只认
// isCommandNeverOnWire / isInjectTimeout / isSendGateReject 三支早返。
//
// 漏判的实际代价（不是理论洁癖）：判成「结局未知」⇒ 台账落 unattributed ⇒
// 该文本进双发闸拦阻集合（guardResubmit 连 unattributed 一起拦）⇒ 用户看清页面
// 重跑时被告知「已有提交尝试」——而实际一次都没发。这是把基座修好之后
// 反而把文本锁死的坑。
package service

import (
	"errors"
	"testing"
)

func TestIsNeverDispatched(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{
			name: "attach 撞 deadline：withDebugger 是先 attach 再跑 fn，fn 压根没被调用",
			err:  errors.New("cdp_attach_deadline: 超过 8000ms 未回包"),
			want: true,
		},
		{
			name: "nil 不算",
			err:  nil,
			want: false,
		},
		// 下面三条是**必须为 false** 的：事件已入队进渲染进程=结局未知，
		// 当成「从未发生」会让 finalize 回查被跳过，直接放行双发。
		{
			name: "send 撞单条 deadline：事件已发出，结局未知",
			err:  errors.New("cdp_send_deadline: 超过 5000ms 未回包"),
			want: false,
		},
		{
			name: "整段命令撞总闸：轨迹/按键可能已部分下发",
			err:  errors.New("cdp_command_deadline: 超过 25000ms 未回包"),
			want: false,
		},
		{
			name: "click_unacked：press/release 已入队",
			err:  errors.New("click_unacked"),
			want: false,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isNeverDispatched(c.err); got != c.want {
				t.Fatalf("isNeverDispatched(%v)=%v，期望 %v", c.err, got, c.want)
			}
		})
	}
}

// isNeverExecuted 必须把 attach-deadline 一起带上，否则非 post_comment 的写步
// （click/type）撞 attach 超时会落成 unattributed，把该步的键永久锁死。
func TestIsNeverExecuted_CdpAttachDeadline(t *testing.T) {
	err := errors.New("cdp_attach_deadline: 超过 8000ms 未回包")
	if !isNeverExecuted(err) {
		t.Fatalf("isNeverExecuted(%v)=false：CDP 未 attach 被判成「发生过」⇒ 台账落 unattributed ⇒ 该步键被双发闸永久锁死", err)
	}
}

// 反向闸：已入队的结局未知态绝不能被 isNeverExecuted 放行为「从未发生」。
// 与 submit_enter_b18_test.go 同款立场（提交键失败必须留给人工核，绝不放行重发）。
func TestIsNeverExecuted_CdpQueuedStatesStayNotNeverExecuted(t *testing.T) {
	for _, msg := range []string{
		"cdp_send_deadline: 超过 5000ms 未回包",
		"cdp_command_deadline: 超过 25000ms 未回包",
		"click_unacked: press/release 失败原因 Internal error",
		"submit_key_not_dispatched: cdp_send_deadline",
	} {
		err := errors.New(msg)
		if isNeverExecuted(err) {
			t.Fatalf("isNeverExecuted(%v)=true：事件已入队被判成「从未发生」⇒ 台账留空 ⇒ 同键可直接重发即双发", err)
		}
	}
}
