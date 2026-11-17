package service

import (
	"context"
	"strings"
	"testing"
)

func TestReplyPolicy_AllGenerationPathsPreserveNecessaryFacts(t *testing.T) {
	e := &SalesEngine{}
	req := &SalesRequest{UserMessage: "部署需要什么配置、如何备份以及费用条件？", Platform: "douyin", Config: &SalesEngineConfig{Persona: "合成助手"}}
	prompts := map[string]string{
		"普通系统提示": e.plainDispatchSystemPrompt(context.Background(), req, "zh", nil),
		"工具系统提示": buildAgentSystemPrompt("合成助手", nil, nil, nil, nil, nil, ""),
		"普通用户提示": e.buildPrompt(req, nil, nil, nil, "default", nil, nil, nil),
	}
	for name, prompt := range prompts {
		t.Run(name, func(t *testing.T) {
			for _, requirement := range []string{"完整性优先", "当前问题", "必要条件", "单条", "不得截断"} {
				if !strings.Contains(prompt, requirement) {
					t.Errorf("生成路径缺少共同质量约束：%s", requirement)
				}
			}
			if strings.Contains(prompt, "≤80 字") {
				t.Error("不能以硬字数限制省略多问的必要条件")
			}
		})
	}
}
