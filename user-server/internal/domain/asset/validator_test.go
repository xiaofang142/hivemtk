package asset

import "testing"

func jsonOf(s string) []byte { return []byte(s) }

func TestValidateAssetDataBadJSON(t *testing.T) {
	if err := ValidateAssetData(AssetTypeAgentPersona, jsonOf(`{oops`)); err == nil {
		t.Fatal("非法 JSON 应报错")
	}
}

func TestValidateAssetTypeConsistency(t *testing.T) {
	data := jsonOf(`{"asset_type":"sales_script","name":"a","scripts":[]}`)
	if err := ValidateAssetData(AssetTypeSalesScript, data); err != nil {
		t.Fatalf("asset_type 一致应通过: %v", err)
	}
	if err := ValidateAssetData(AssetTypeAgentPersona, data); err == nil {
		t.Fatal("asset_type 与路径不一致应报错")
	}
	// 无 asset_type 字段 → 跳过一致性校验
	anon := jsonOf(`{"name":"a","system_prompt":"p"}`)
	if err := ValidateAssetData(AssetTypeAgentPersona, anon); err != nil {
		t.Fatalf("缺省 asset_type 应跳过一致性校验: %v", err)
	}
	// 空字符串同样跳过
	blank := jsonOf(`{"asset_type":"","name":"a","system_prompt":"p"}`)
	if err := ValidateAssetData(AssetTypeAgentPersona, blank); err != nil {
		t.Fatalf("空 asset_type 应跳过一致性校验: %v", err)
	}
}

func TestValidateIndustry(t *testing.T) {
	ok := jsonOf(`{"name":"a","system_prompt":"p","industry":"美妆"}`)
	if err := ValidateAssetData(AssetTypeAgentPersona, ok); err != nil {
		t.Fatalf("合法行业应通过: %v", err)
	}
	ext := jsonOf(`{"name":"a","system_prompt":"p","industry":"货代"}`)
	if err := ValidateAssetData(AssetTypeAgentPersona, ext); err != nil {
		t.Fatalf("扩展行业(货代)应通过: %v", err)
	}
	bad := jsonOf(`{"name":"a","system_prompt":"p","industry":"彩票"}`)
	if err := ValidateAssetData(AssetTypeAgentPersona, bad); err == nil {
		t.Fatal("非法行业应报错")
	}
	empty := jsonOf(`{"name":"a","system_prompt":"p","industry":""}`)
	if err := ValidateAssetData(AssetTypeAgentPersona, empty); err != nil {
		t.Fatalf("空行业应跳过: %v", err)
	}
}

func TestValidateRequiredKeysPerType(t *testing.T) {
	tests := []struct {
		name    string
		typ     AssetType
		full    string
		missing string
	}{
		{
			name:    "agent_persona",
			typ:     AssetTypeAgentPersona,
			full:    `{"name":"n","system_prompt":"p"}`,
			missing: `{"name":"n"}`,
		},
		{
			name:    "sales_script",
			typ:     AssetTypeSalesScript,
			full:    `{"name":"n","scripts":["s1"]}`,
			missing: `{"name":"n"}`,
		},
		{
			name:    "ab_test_plan",
			typ:     AssetTypeABTestPlan,
			full:    `{"name":"n","variants":[{"id":"a"}]}`,
			missing: `{"name":"n"}`,
		},
		{
			name:    "marketing_workflow",
			typ:     AssetTypeMarketingFlow,
			full:    `{"name":"n","steps":[{"id":1}]}`,
			missing: `{"steps":[]}`,
		},
		{
			name:    "industry_sop",
			typ:     AssetTypeIndustrySOP,
			full:    `{"name":"n","steps":[{"id":1}]}`,
			missing: `{"name":"n"}`,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := ValidateAssetData(tt.typ, jsonOf(tt.full)); err != nil {
				t.Fatalf("完整数据应通过: %v", err)
			}
			if err := ValidateAssetData(tt.typ, jsonOf(tt.missing)); err == nil {
				t.Fatalf("缺少必填字段应报错")
			}
		})
	}
}

func TestValidateUnknownType(t *testing.T) {
	err := ValidateAssetData(AssetType("bogus"), jsonOf(`{"name":"n"}`))
	if err == nil || err.Error() != "未知资产类型" {
		t.Fatalf("未知类型应报「未知资产类型」，得到: %v", err)
	}
}
