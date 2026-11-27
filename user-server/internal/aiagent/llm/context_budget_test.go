package llm

import "testing"

func TestProviderContextBudgetUsesAllDeclaredLimits(t *testing.T) {
	tests := []struct {
		name string
		cfg  ProviderConfig
		want int
	}{
		{
			name: "model_input_limit_wins",
			cfg: ProviderConfig{
				ContextWindow: 1_050_000, MaxInputTokens: 922_000, MaxOutputTokens: 128_000,
				ReasoningReserveTokens: 1024,
			},
			want: 922_000,
		},
		{
			name: "protocol_limit_wins",
			cfg: ProviderConfig{
				ContextWindow: 1_000_000, MaxInputTokens: 991_808, ProtocolInputLimit: 800_000,
				MaxOutputTokens: 131_072, ReasoningReserveTokens: 0,
			},
			want: 800_000,
		},
		{
			name: "window_math_wins",
			cfg: ProviderConfig{
				ContextWindow: 131_072, MaxInputTokens: 200_000, MaxOutputTokens: 16_384,
				ReasoningReserveTokens: 1024,
			},
			want: 131_072 - 8192 - 1024 - 512,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			budget, err := NewContextBudget(tt.cfg, 8192)
			if err != nil {
				t.Fatal(err)
			}
			if budget.EffectiveInput != tt.want || budget.TotalContext != tt.cfg.ContextWindow || budget.OutputReserve != 8192 {
				t.Fatalf("预算错误：%+v want input=%d", budget, tt.want)
			}
		})
	}
}

func TestProviderContextBudgetDefaultsAndFailClosed(t *testing.T) {
	budget, err := NewContextBudget(ProviderConfig{}, 0)
	if err != nil {
		t.Fatal(err)
	}
	if budget.TotalContext != 8192 || budget.OutputReserve != 2048 || budget.EffectiveInput <= 0 || budget.EffectiveInput >= budget.TotalContext {
		t.Fatalf("未知能力必须走保守默认：%+v", budget)
	}
	if _, err := NewContextBudget(ProviderConfig{ContextWindow: 2000}, 2048); err == nil {
		t.Fatal("输出预留与上下文不相容时必须拒绝，不能给出负预算")
	}
}

func TestProviderContextBudgetHasSafetyMargin(t *testing.T) {
	budget, err := NewContextBudget(ProviderConfig{ContextWindow: 100_000, MaxInputTokens: 99_000, MaxOutputTokens: 4096}, 4096)
	if err != nil {
		t.Fatal(err)
	}
	if budget.EffectiveInput != 100_000-4096-512 {
		t.Fatalf("必须扣除安全余量：got %d", budget.EffectiveInput)
	}
	if budget.TotalInputBudget() != budget.EffectiveInput {
		t.Fatalf("TotalInputBudget 应返回有效输入预算：%d vs %d", budget.TotalInputBudget(), budget.EffectiveInput)
	}
}
