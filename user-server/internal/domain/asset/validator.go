package asset

import (
	"encoding/json"
	"errors"
)

// ValidateAssetData 校验资产 JSON
func ValidateAssetData(t AssetType, data []byte) error {
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		return errors.New("资产 JSON 格式错误")
	}
	assetType, _ := m["asset_type"].(string)
	if assetType != "" && AssetType(assetType) != t {
		return errors.New("asset_type 字段与路径不一致")
	}
	if industry, ok := m["industry"].(string); ok && industry != "" {
		if !Industry(industry).Valid() {
			return errors.New("industry 不在支持列表内（美妆/教培/医美/汽车/金融/电子烟/成人用品/两性健康/租车/民宿/货代/移民）")
		}
	}
	switch t {
	case AssetTypeAgentPersona:
		return requireKeys(m, "name", "system_prompt")
	case AssetTypeSalesScript:
		return requireKeys(m, "name", "scripts")
	case AssetTypeABTestPlan:
		return requireKeys(m, "name", "variants")
	case AssetTypeMarketingFlow:
		return requireKeys(m, "name", "steps")
	case AssetTypeIndustrySOP:
		return requireKeys(m, "name", "steps")
	}
	return errors.New("未知资产类型")
}

func requireKeys(m map[string]interface{}, keys ...string) error {
	for _, k := range keys {
		if _, ok := m[k]; !ok {
			return errors.New("缺少必填字段: " + k)
		}
	}
	return nil
}
