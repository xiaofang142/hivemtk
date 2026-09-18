package asset

import "testing"

func TestAssetTypeValid(t *testing.T) {
	for _, typ := range []AssetType{
		AssetTypeAgentPersona, AssetTypeSalesScript, AssetTypeABTestPlan,
		AssetTypeMarketingFlow, AssetTypeIndustrySOP,
	} {
		if !typ.Valid() {
			t.Errorf("%q.Valid() = false, want true", typ)
		}
		if typ.Label() == "" {
			t.Errorf("%q.Label() 不应为空", typ)
		}
	}
	if AssetType("bogus").Valid() {
		t.Error("bogus.Valid() = true, want false")
	}
	if AssetType("bogus").Label() != "" {
		t.Error("bogus.Label() 应为空")
	}
}

func TestIndustryValid(t *testing.T) {
	valid := []Industry{
		IndustryMeiZhuang, IndustryJiaoPei, IndustryYiMei, IndustryQiChe, IndustryJinRong,
		IndustryECig, IndustryAdult, IndustrySexHealth, IndustryCarRent,
		IndustryHomestay, IndustryFreight, IndustryImmigra,
	}
	for _, i := range valid {
		if !i.Valid() {
			t.Errorf("%q.Valid() = false, want true", i)
		}
	}
	for _, i := range []Industry{"", "彩票", "magic"} {
		if i.Valid() {
			t.Errorf("%q.Valid() = true, want false", i)
		}
	}
}

func TestAssetSource(t *testing.T) {
	for _, s := range []AssetSource{SourcePurchased, SourceManual, SourceSynced, SourceImported} {
		if s.Label() == "" {
			t.Errorf("%q.Label() 不应为空", s)
		}
	}
	if !SourcePurchased.IsFromPlatform() || !SourceSynced.IsFromPlatform() {
		t.Error("purchased/synced 应判定为平台来源")
	}
	if SourceManual.IsFromPlatform() || SourceImported.IsFromPlatform() {
		t.Error("manual/imported 不应判定为平台来源")
	}
}
