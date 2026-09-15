package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"hivemtk-user/internal/dto"
)

// ABTestPlan AB 测试方案
type ABTestPlan struct {
	ID             string                   `json:"id"`
	Name           string                   `json:"name"`
	Industry       string                   `json:"industry"`
	ExperimentType string                   `json:"experiment_type"`
	Hypothesis     string                   `json:"hypothesis"`
	Variants       []map[string]interface{} `json:"variants"`
	TrafficSplit   []float64                `json:"traffic_split"`
	DurationDays   int                      `json:"duration_days"`
	MinSampleSize  int                      `json:"min_sample_size"`
}

// ABTestLoader AB 测试加载器
type ABTestLoader struct {
}

func NewABTestLoader() *ABTestLoader {
	return &ABTestLoader{}
}

func (l *ABTestLoader) LoadPlan(ctx context.Context, planID string) (*ABTestPlan, error) {
	if data, found := LoadAssetFromDB("ab_test_plan", planID); found {
		var p ABTestPlan
		if err := json.Unmarshal(data, &p); err == nil {
			p.ID = planID
			return &p, nil
		}
		slog.Warn("ABTestLoader invalid JSON, fallback", "asset_id", planID)
	}
	return loadDefaultABTest(planID)
}

func (l *ABTestLoader) ListAllPlans(ctx context.Context) ([]*ABTestPlan, error) {
	var result []*ABTestPlan
	rows, _ := ListAssetsFromDB("ab_test_plan")
	seen := map[string]bool{}
	for _, r := range rows {
		var p ABTestPlan
		if err := json.Unmarshal(r.Data, &p); err == nil {
			p.ID = r.AssetID
			p.Name = r.Name
			result = append(result, &p)
			seen[p.ID] = true
		}
	}
	for id, p := range defaultABTests() {
		if !seen[id] {
			result = append(result, p)
		}
	}
	return result, nil
}

func loadDefaultABTest(id string) (*ABTestPlan, error) {
	if p, ok := defaultABTests()[id]; ok {
		return p, nil
	}
	return nil, errors.New("ab test plan not found: " + id)
}

func defaultABTests() map[string]*ABTestPlan {
	return map[string]*ABTestPlan{
		"default-greeting-ab": {
			ID:             "default-greeting-ab",
			Name:           "默认破冰话术 A/B",
			ExperimentType: "script_variant",
			Hypothesis:     "更亲切的破冰话术能提升转化",
			Variants: []map[string]interface{}{
				{"key": "A", "name": "标准版", "config": map[string]string{"greeting": "您好，请问有什么可以帮您？"}},
				{"key": "B", "name": "亲切版", "config": map[string]string{"greeting": "嗨～很高兴认识你！今天想了解点什么呢？"}},
			},
			TrafficSplit:  []float64{0.5, 0.5},
			DurationDays:  14,
			MinSampleSize: 1000,
		},
	}
}

func (p *ABTestPlan) ToSOPABTestConfig(ctx context.Context) dto.SOPABTestConfig {
	cfg := dto.SOPABTestConfig{
		Enabled: true,
	}
	for i, v := range p.Variants {
		key, _ := v["key"].(string)
		if key == "" {
			key = fmt.Sprintf("variant_%d", i+1)
		}
		name, _ := v["name"].(string)
		if name == "" {
			name = key
		}
		w := 0
		if i < len(p.TrafficSplit) {
			w = int(p.TrafficSplit[i] * 100)
		}
		cfg.Variants = append(cfg.Variants, dto.SOPABTestVariant{
			Name:   name,
			Weight: w,
		})
	}
	return cfg
}
