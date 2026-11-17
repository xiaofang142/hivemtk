package service

import (
	"context"
	"strings"
	"testing"
)

func TestHumanizePolisher_P2_5_RemoveFlattery(t *testing.T) {
	p := NewHumanizePolisher()
	ctx := context.Background()

	type tc struct {
		name   string
		input  string
		expect string
		intent string
	}
	cases := []tc{
		{
			name:   "self_intro_1",
			input:  "作为 AI 助手，我需要更多上下文。",
			expect: "，我需要更多上下文。",
		},
		{
			name:   "self_intro_2",
			input:  "我是 AI，不能直接帮您下单。",
			expect: "，不能直接帮您下单。",
		},
		{
			name:   "courtesy_1",
			input:  "非常感谢您的咨询。我们的产品主要面向私域。",
			expect: "我们的产品主要面向私域。",
		},
		{
			name:   "courtesy_2",
			input:  "很高兴为您服务。HiveMTK 是 AGPL-3.0 协议。",
			expect: "HiveMTK 是 AGPL-3.0 协议。",
		},
		{
			name:   "courtesy_3",
			input:  "感谢您的提问。Gitee 主仓库地址是 xxx。",
			expect: "Gitee 主仓库地址是 xxx。",
		},
		{
			name:   "courtesy_4",
			input:  "期待您的回复。",
			expect: "",
		},
		{
			name:   "single_ack_1",
			input:  "可以的！😊 关于代码二开请查看 AGPL-3.0 协议。",
			expect: "关于代码二开请查看 AGPL-3.0 协议。",
		},
		{
			name:   "single_ack_2",
			input:  "好的！",
			expect: "",
		},
		{
			name:   "single_ack_3",
			input:  "没问题 😊 我来帮您看下。",
			expect: "我来帮您看下。",
		},
		{
			name:   "single_ack_4",
			input:  "当然可以😄 我们支持私有化部署。",
			expect: "我们支持私有化部署。",
		},
		{
			name:   "apology_1",
			input:  "很抱歉，我无法直接操作您的设备。",
			expect: "很抱歉，我无法直接操作您的设备。",
		},
		{
			name:   "apology_2",
			input:  "请您谅解。这是 HiveMTK 的私域部署说明。",
			expect: "这是 HiveMTK 的私域部署说明。",
		},
		{
			name:   "apology_3",
			input:  "造成不便深表歉意，请联系客服。",
			expect: "，请联系客服。",
		},
		{
			name:   "real_xhs_1",
			input:  "可以的！😊 关于代码二次开发，给您简单说明：\n\n- **开源协议**：HiveMTK 采用 **GNU AGPL-3.0**\n- **仓库地址**：Gitee 主仓库",
			expect: "关于代码二次开发，给您简单说明：\n\n- **开源协议**：HiveMTK 采用 **GNU AGPL-3.0**\n- **仓库地址**：Gitee 主仓库",
		},
		{
			name:   "real_xhs_2",
			input:  "可以的😊 HiveMTK 是私域单租户 AI 智能体客服系统。",
			expect: "HiveMTK 是私域单租户 AI 智能体客服系统。",
		},
		{
			name:   "complaint_leading_ack_preserved",
			input:  "好的，我来帮您处理这个售后问题。",
			expect: "好的，我来帮您处理这个售后问题。",
			intent: IntentComplaint,
		},
		{
			name:   "churn_leading_ack_preserved",
			input:  "好的，让我看一下您最近的订单。",
			expect: "好的，让我看一下您最近的订单。",
			intent: IntentChurn,
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pctx := &PolishContext{Platform: "xiaohongshu", Intent: c.intent}
			got, err := p.Polish(ctx, c.input, pctx)
			if err != nil {
				t.Fatalf("Polish unexpected error: %v", err)
			}
			if !strings.Contains(got, strings.TrimSpace(c.expect)) && strings.TrimSpace(c.expect) != "" {
				t.Errorf("removeAITraces(%q) = %q, want to contain %q", c.input, got, c.expect)
			}
			if c.intent != IntentComplaint && c.intent != IntentChurn && c.intent != IntentAfterSale {
				for _, trace := range []string{"作为 AI 助手", "可以的", "感谢您的", "很高兴为您服务", "请您谅解"} {
					if strings.Contains(got, trace) {
						t.Errorf("Polish(%q) = %q, still contains flattery %q", c.input, got, trace)
					}
				}
			}
		})
	}
}

func TestHumanizePolisher_P2_5_NormalContent_NotOverStripped(t *testing.T) {
	p := NewHumanizePolisher()
	ctx := context.Background()

	cases := []string{
		"可以的，我可以帮您查看具体功能。",
		"我们支持的功能是 AGPL-3.0 开源。",
		"好的，下面是您的订单详情。",
		"Hi, what can I do for you?",
	}
	for _, input := range cases {
		t.Run(input, func(t *testing.T) {
			pctx := &PolishContext{Platform: "xiaohongshu"}
			got, err := p.Polish(ctx, input, pctx)
			if err != nil {
				t.Fatalf("Polish unexpected error: %v", err)
			}
			if len([]rune(got)) == 0 {
				t.Errorf("Polish(%q) = %q, should not strip all content", input, got)
			}
		})
	}
}
