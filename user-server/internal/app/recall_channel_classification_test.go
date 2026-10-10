package app

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestRecallChannelClassification 钉住 Recall 的渠道分类口径。
//
// 背景：recall 从 NoOp（恒 ErrChannelNotImplemented）改成真实现后，
// reach_adapter_review_test.go 里那条断言失效 —— 它赌的是"任何渠道都报 not
// implemented"，而真实语义早已分成「桥接渠道 / 未配 db / 查不到行 / 无撤回接口 /
// 超出窗口 / 已实现」六类。本文件把后四类各自可断言的那一格固定下来，
// 避免下次再有人照 default 分支写断言而写出永远绿不了的用例。
func TestRecallChannelClassification(t *testing.T) {
	a := &IntegrationReachAdapter{}
	ctx := context.Background()

	t.Run("桥接渠道在查库之前就按不支持返回", func(t *testing.T) {
		// 桥接渠道的 msgID 是合成键，拿去查 message_hub 也查不到平台标识，
		// 所以这道闸必须排在 db 闸之前（:549-551）。
		for _, ch := range []string{"douyin", "kuaishou", "xhs", "tiktok", "xianyu"} {
			err := a.Recall(ctx, ch, "mh:deadbeef")
			if !errors.Is(err, ErrRecallNotSupported) {
				t.Errorf("Recall(%s) err = %v, want ErrRecallNotSupported", ch, err)
			}
		}
	})

	t.Run("未配 db 时报服务未装配，而不是渠道未实现", func(t *testing.T) {
		for _, ch := range []string{"sms", "email", "qq", "wecom"} {
			err := a.Recall(ctx, ch, "whatever")
			if !errors.Is(err, ErrIntegrationServiceNotConfigured) {
				t.Errorf("Recall(%s) err = %v, want ErrIntegrationServiceNotConfigured", ch, err)
			}
		}
	})

	t.Run("recallWindow 只认三家", func(t *testing.T) {
		want := map[string]time.Duration{
			"telegram": 48 * time.Hour,
			"feishu":   24 * time.Hour,
			"wecom":    24 * time.Hour,
		}
		for platform, wantWin := range want {
			got, ok := recallWindow(platform)
			if !ok {
				t.Errorf("recallWindow(%s) 应支持撤回", platform)
				continue
			}
			if got != wantWin {
				t.Errorf("recallWindow(%s) = %v, want %v", platform, got, wantWin)
			}
		}
		// 这三家之外一律"无平台主动撤回"——所以 Recall 的 switch default 分支
		// （ErrChannelNotImplemented）在真实数据上永远走不到。
		for _, platform := range []string{"sms", "email", "qq", "whatsapp", "douyin", "weixin"} {
			if win, ok := recallWindow(platform); ok {
				t.Errorf("recallWindow(%s) 应不支持撤回, got (%v, true)", platform, win)
			}
		}
	})

	t.Run("平台撤回实现尚未接通，三家也报不支持", func(t *testing.T) {
		// telegram/feishu/wecom 的 recallXxx 目前都是 stub（ErrRecallNotSupported），
		// 这条断言同时是"别把 stub 当实现"的哨兵。三个 stub 都不解引用 hub，故传 nil。
		for _, fn := range []func() error{
			func() error { return a.recallTelegram(context.Background(), nil) },
			func() error { return a.recallFeishu(context.Background(), nil) },
			func() error { return a.recallWeCom(context.Background(), nil) },
		} {
			if err := fn(); !errors.Is(err, ErrRecallNotSupported) {
				t.Errorf("平台撤回 stub err = %v, want ErrRecallNotSupported", err)
			}
		}
	})
}
