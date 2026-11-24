package service

import (
	"context"
	"strings"
	"testing"
)

func TestDetectTelegramIntent(t *testing.T) {
	cases := []struct {
		name     string
		text     string
		minScore int
		wantOpp  bool
		wantSig  string
	}{
		{name: "空消息", text: "", minScore: 0, wantOpp: false},
		{name: "普通寒暄", text: "大家好呀", minScore: 8, wantOpp: false},
		{name: "高意向-中文", text: "请问这个产品价格多少？可以批发吗", minScore: 60, wantOpp: true, wantSig: "价格"},
		{name: "中意向", text: "我想了解一下你们的产品", minScore: 20, wantOpp: false, wantSig: "了解"},
		{name: "优惠咨询", text: "有什么优惠活动吗", minScore: 20, wantOpp: false, wantSig: "优惠"},
		{name: "联系方式信号", text: "加我微信 vx:abc123 聊聊", minScore: 40, wantOpp: true, wantSig: "联系方式"},
		{name: "邮箱联系方式", text: "please contact me at sales@example.com", minScore: 40, wantOpp: true, wantSig: "联系方式"},
		{name: "高意向-英文", text: "Hi, how much is the price? I want to buy wholesale", minScore: 90, wantOpp: true, wantSig: "how much"},
		{name: "命令不算意向", text: "/start", minScore: 8, wantOpp: false},
		{name: "纯表情", text: "😀😀😀", minScore: 8, wantOpp: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			score, signals, isOpp := DetectTelegramIntent(c.text)
			if score < c.minScore {
				t.Fatalf("text=%q score=%d 期望 >= %d", c.text, score, c.minScore)
			}
			if isOpp != c.wantOpp {
				t.Fatalf("text=%q isOpp=%v 期望 %v (score=%d signals=%v)", c.text, isOpp, c.wantOpp, score, signals)
			}
			if c.wantSig != "" {
				hit := false
				for _, s := range signals {
					if strings.Contains(s, c.wantSig) {
						hit = true
						break
					}
				}
				if !hit {
					t.Fatalf("text=%q 期望命中信号含 %q，实际 signals=%v", c.text, c.wantSig, signals)
				}
			}
		})
	}
}

func TestDetectTelegramIntent_ScoreCap(t *testing.T) {
	score, _, _ := DetectTelegramIntent("how much price buy wholesale distributor reseller procurement")
	if score != 100 {
		t.Fatalf("期望封顶 100，实际 %d", score)
	}
}

func TestFormatAndParseTelegramLeadScore(t *testing.T) {
	desc := formatTelegramLeadDesc("销售交流群", "我想买", 82, []string{"价格", "合作"}, true)
	if !strings.Contains(desc, "[意向分:82]") {
		t.Fatalf("desc 未内嵌意向分: %s", desc)
	}
	if !strings.Contains(desc, "群发言商机") {
		t.Fatalf("商机应标记为「群发言商机」: %s", desc)
	}
	if got := formatTelegramLeadDesc("群", "hi", 8, nil, false); !strings.Contains(got, "群发言线索") {
		t.Fatalf("普通线索应标记为「群发言线索」: %s", got)
	}
}

func TestTelegramLeadAccountKey(t *testing.T) {
	if k := telegramLeadAccountKey("alice", "123"); k != "@alice" {
		t.Fatalf("username 优先，期望 @alice 实际 %q", k)
	}
	if k := telegramLeadAccountKey("@bob", "456"); k != "@bob" {
		t.Fatalf("username 带 @ 应原样保留，实际 %q", k)
	}
	if k := telegramLeadAccountKey("", "789"); k != "tg:789" {
		t.Fatalf("无 username 回退 tg:<id>，实际 %q", k)
	}
	if k := telegramLeadAccountKey("", "0"); k != "" {
		t.Fatalf("id 为 0 应返回空（不建线索），实际 %q", k)
	}
	if k := telegramLeadAccountKey("", ""); k != "" {
		t.Fatalf("全空应返回空，实际 %q", k)
	}
}

func TestIsTelegramBotMentioned(t *testing.T) {
	cases := []struct {
		name        string
		text        string
		botUsername string
		want        bool
	}{
		{name: "空 username 不识别", text: "hello @mybot", botUsername: "", want: false},
		{name: "精确 @username", text: "请问 @mybot 怎么买", botUsername: "mybot", want: true},
		{name: "大小写不敏感", text: "hi @MyBot 在吗", botUsername: "mybot", want: true},
		{name: "无 @提及", text: "请问价格多少", botUsername: "mybot", want: false},
		{name: "提及别人", text: "找 @otherbot 吧", botUsername: "mybot", want: false},
		{name: "用户名带前缀 @ 不匹配", text: "hi @mybotX", botUsername: "mybot", want: false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := isTelegramBotMentioned(c.text, c.botUsername); got != c.want {
				t.Fatalf("isTelegramBotMentioned(%q,%q)=%v 期望 %v", c.text, c.botUsername, got, c.want)
			}
		})
	}
}

func TestTgLeadOutreachAllowed(t *testing.T) {
	// -count=N 隔离：冷却键落在全局缓存（TTL 30 分钟），先清理上一轮残留
	cleanupTgOutreachKeysForTest(t, "acc1", "-100", "u1", "u2")
	svc := &WebhookService{}

	if !svc.tgLeadOutreachAllowed(context.Background(), "acc1", "-100", "u1") {
		t.Fatalf("首次应允许触达")
	}
	if svc.tgLeadOutreachAllowed(context.Background(), "acc1", "-100", "u1") {
		t.Fatalf("冷却期内不应重复触达同一发言者")
	}
	if !svc.tgLeadOutreachAllowed(context.Background(), "acc1", "-100", "u2") {
		t.Fatalf("不同发言者不受前者冷却影响")
	}
}
