package sop

import (
	"testing"

	"hivemtk-user/internal/model"
)

func TestSOPABTestConfig_Validate(t *testing.T) {
	cases := []struct {
		name    string
		cfg     SOPABTestConfig
		wantErr bool
	}{
		{
			name:    "disabled",
			cfg:     SOPABTestConfig{Enabled: false},
			wantErr: false,
		},
		{
			name: "valid_two_variants",
			cfg: SOPABTestConfig{
				Enabled: true,
				Variants: []SOPABTestVariant{
					{Name: "A", Weight: 50},
					{Name: "B", Weight: 50},
				},
			},
			wantErr: false,
		},
		{
			name: "valid_three_variants",
			cfg: SOPABTestConfig{
				Enabled: true,
				Variants: []SOPABTestVariant{
					{Name: "A", Weight: 70},
					{Name: "B", Weight: 20},
					{Name: "C", Weight: 10},
				},
			},
			wantErr: false,
		},
		{
			name: "only_one_variant",
			cfg: SOPABTestConfig{
				Enabled: true,
				Variants: []SOPABTestVariant{
					{Name: "A", Weight: 100},
				},
			},
			wantErr: true,
		},
		{
			name: "weight_sum_not_100",
			cfg: SOPABTestConfig{
				Enabled: true,
				Variants: []SOPABTestVariant{
					{Name: "A", Weight: 60},
					{Name: "B", Weight: 50},
				},
			},
			wantErr: true,
		},
		{
			name: "duplicate_name",
			cfg: SOPABTestConfig{
				Enabled: true,
				Variants: []SOPABTestVariant{
					{Name: "A", Weight: 50},
					{Name: "A", Weight: 50},
				},
			},
			wantErr: true,
		},
		{
			name: "empty_name",
			cfg: SOPABTestConfig{
				Enabled: true,
				Variants: []SOPABTestVariant{
					{Name: "", Weight: 50},
					{Name: "B", Weight: 50},
				},
			},
			wantErr: true,
		},
		{
			name: "zero_weight",
			cfg: SOPABTestConfig{
				Enabled: true,
				Variants: []SOPABTestVariant{
					{Name: "A", Weight: 0},
					{Name: "B", Weight: 100},
				},
			},
			wantErr: true,
		},
		{
			name: "negative_weight",
			cfg: SOPABTestConfig{
				Enabled: true,
				Variants: []SOPABTestVariant{
					{Name: "A", Weight: -10},
					{Name: "B", Weight: 110},
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateSOPABTestConfig(tt.cfg)
			if (err != nil) != tt.wantErr {
				t.Errorf("[%s] err=%v wantErr=%v", tt.name, err, tt.wantErr)
			}
		})
	}
}

func TestSelectVariant_Consistency(t *testing.T) {
	cfg := SOPABTestConfig{
		Enabled: true,
		Variants: []SOPABTestVariant{
			{Name: "A", Weight: 50},
			{Name: "B", Weight: 50},
		},
	}

	customerID := "customer_12345"
	first := SelectSOPABTestVariant(cfg, customerID)
	for i := 0; i < 100; i++ {
		v := SelectSOPABTestVariant(cfg, customerID)
		if v.Name != first.Name {
			t.Fatalf("一致性失败：第 %d 次返回 %s，期望 %s", i+1, v.Name, first.Name)
		}
	}
}

func TestSelectVariant_Disabled(t *testing.T) {
	cfg := SOPABTestConfig{Enabled: false}
	v := SelectSOPABTestVariant(cfg, "any_customer")
	if v.Name != "" {
		t.Errorf("disabled config should return empty variant, got %s", v.Name)
	}
}

func TestSelectVariant_Distribution(t *testing.T) {
	cfg := SOPABTestConfig{
		Enabled: true,
		Variants: []SOPABTestVariant{
			{Name: "A", Weight: 50},
			{Name: "B", Weight: 50},
		},
	}

	counts := map[string]int{"A": 0, "B": 0}
	for i := 0; i < 1000; i++ {
		customerID := "customer_" + string(rune('a'+(i%26))) + string(rune('a'+(i%7)))
		v := SelectSOPABTestVariant(cfg, customerID)
		if v.Name != "A" && v.Name != "B" {
			t.Fatalf("unexpected variant: %s", v.Name)
		}
		counts[v.Name]++
	}

	if intAbs(counts["A"]-500) > 150 {
		t.Errorf("A 分布偏差过大：%d（期望 500±150）", counts["A"])
	}
	if intAbs(counts["B"]-500) > 150 {
		t.Errorf("B 分布偏差过大：%d（期望 500±150）", counts["B"])
	}
	t.Logf("1000 客户分布：A=%d, B=%d", counts["A"], counts["B"])
}

func TestSelectVariant_ThreeWayDistribution(t *testing.T) {
	cfg := SOPABTestConfig{
		Enabled: true,
		Variants: []SOPABTestVariant{
			{Name: "A", Weight: 70},
			{Name: "B", Weight: 20},
			{Name: "C", Weight: 10},
		},
	}

	counts := map[string]int{"A": 0, "B": 0, "C": 0}
	for i := 0; i < 1000; i++ {
		customerID := "customer_" + string(rune('a'+(i%26))) + string(rune('a'+(i%7)))
		v := SelectSOPABTestVariant(cfg, customerID)
		counts[v.Name]++
	}

	if intAbs(counts["A"]-700) > 150 {
		t.Errorf("A 分布偏差过大：%d（期望 700±150）", counts["A"])
	}
	if intAbs(counts["B"]-200) > 100 {
		t.Errorf("B 分布偏差过大：%d（期望 200±100）", counts["B"])
	}
	if intAbs(counts["C"]-100) > 80 {
		t.Errorf("C 分布偏差过大：%d（期望 100±80）", counts["C"])
	}
	t.Logf("1000 客户三分分布：A=%d, B=%d, C=%d", counts["A"], counts["B"], counts["C"])
}

func TestSelectVariant_DifferentSalts(t *testing.T) {
	cfg1 := SOPABTestConfig{
		Enabled: true,
		Salt:    "customer_id",
		Variants: []SOPABTestVariant{
			{Name: "A", Weight: 50},
			{Name: "B", Weight: 50},
		},
	}
	cfg2 := SOPABTestConfig{
		Enabled: true,
		Salt:    "session_id",
		Variants: []SOPABTestVariant{
			{Name: "A", Weight: 50},
			{Name: "B", Weight: 50},
		},
	}

	differentCount := 0
	for i := 0; i < 100; i++ {
		customerID := "customer_" + string(rune('a'+(i%26)))
		v1 := SelectSOPABTestVariant(cfg1, customerID)
		v2 := SelectSOPABTestVariant(cfg2, customerID)
		if v1.Name != v2.Name {
			differentCount++
		}
	}

	if differentCount < 20 {
		t.Errorf("不同 salt 应产生更多不同分流：仅 %d/100 不同", differentCount)
	}
	t.Logf("100 客户在不同 salt 下：%d 个分流结果不同", differentCount)
}

func TestParseSOPABTestConfig_Empty(t *testing.T) {
	cfg := ParseSOPABTestConfig(nil)
	if cfg.Enabled {
		t.Error("expected disabled for nil config")
	}

	cfg = ParseSOPABTestConfig(model.JSONMap{})
	if cfg.Enabled {
		t.Error("expected disabled for empty config")
	}
}

func TestParseSOPABTestConfig_Valid(t *testing.T) {
	raw := model.JSONMap{
		"enabled": true,
		"salt":    "test_salt",
		"variants": []any{
			map[string]any{"name": "A", "weight": float64(60), "sop_graph_id": float64(0)},
			map[string]any{"name": "B", "weight": float64(40), "sop_graph_id": float64(2)},
		},
	}
	cfg := ParseSOPABTestConfig(raw)
	if !cfg.Enabled {
		t.Fatal("expected enabled=true")
	}
	if cfg.Salt != "test_salt" {
		t.Errorf("expected salt=test_salt, got %s", cfg.Salt)
	}
	if len(cfg.Variants) != 2 {
		t.Fatalf("expected 2 variants, got %d", len(cfg.Variants))
	}
	if cfg.Variants[0].Name != "A" || cfg.Variants[0].Weight != 60 {
		t.Errorf("variant A mismatch: %+v", cfg.Variants[0])
	}
	if cfg.Variants[1].Name != "B" || cfg.Variants[1].Weight != 40 || cfg.Variants[1].SOPGraphID != 2 {
		t.Errorf("variant B mismatch: %+v", cfg.Variants[1])
	}

	if err := ValidateSOPABTestConfig(cfg); err != nil {
		t.Errorf("validation failed: %v", err)
	}
}

func intAbs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}
