package service

import (
	"context"
	"strings"
	"testing"
)

func TestReplyPolicy_DefaultPolishDoesNotCutNecessaryTail(t *testing.T) {
	p := NewHumanizePolisher()
	input := strings.Repeat("合成部署步骤需先核对模型与数据量。", 20) + "删除旧数据前必须确认备份可恢复，费用以正式报价为准。"
	got, err := p.Polish(context.Background(), input, &PolishContext{Platform: "douyin"})
	if err != nil || got != input {
		t.Fatalf("默认润色不得截掉尾部条件或改写事实：err=%v，原长=%d，结果长=%d", err, len([]rune(input)), len([]rune(got)))
	}
}

func TestReplyPolicy_PolishPreservesCapabilityAndQuotedText(t *testing.T) {
	for _, input := range []string{
		"很抱歉，我无法直接操作您的设备。请由管理员确认后执行。",
		"我的能力有限，无法确认具体报价，请以正式报价为准。",
		"客户原话是“很抱歉，我无法继续”，不要把它改成同意。",
		"作为一个备份步骤，需要先验证可恢复性。",
	} {
		t.Run(input, func(t *testing.T) {
			got, err := NewHumanizePolisher().Polish(context.Background(), input, nil)
			if err != nil || got != input {
				t.Fatalf("润色不能反转能力限制或改动正文引用：err=%v，got=%q", err, got)
			}
		})
	}
}
