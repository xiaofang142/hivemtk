package translation

import (
	"testing"
	"time"

	"hivemtk-user/internal/dto"
	"hivemtk-user/internal/model"
)

func TestFromGlossaryModelList(t *testing.T) {
	if got := FromGlossaryModelList(nil); got == nil || len(got) != 0 {
		t.Fatalf("nil 输入应返回空切片，得 %v", got)
	}
	if got := FromGlossaryModelList([]*model.Glossary{}); len(got) != 0 {
		t.Fatalf("空切片应返回空，得 %v", got)
	}

	now := time.Now()
	list := []*model.Glossary{
		{
			ID: 1, TermID: "vpn", Category: "tech", Preserve: true,
			Translations: model.JSONMap{"en": "VPN", "ja": 42}, // ja 非字符串 → 空串
			Status:       "active", CreatedAt: now, UpdatedAt: now,
		},
		nil, // 容错：nil 条目映射为 nil 响应
	}
	got := FromGlossaryModelList(list)
	if len(got) != 2 {
		t.Fatalf("长度应 2，得 %d", len(got))
	}
	if got[0].TermID != "vpn" || got[0].Translations["en"] != "VPN" {
		t.Fatalf("首条转换错误: %+v", got[0])
	}
	if v, ok := got[0].Translations["ja"]; !ok || v != "" {
		t.Fatalf("非字符串译文应映射为空串，得 %q %v", v, ok)
	}
	if got[1] != nil {
		t.Fatal("nil model 应得 nil 响应")
	}
}

func TestToGlossaryModelDefaults(t *testing.T) {
	m := ToGlossaryModel(&dto.GlossaryRequest{
		TermID: "t1", Category: "c", Translations: map[string]string{"en": "x"},
	})
	if m.Status != "active" {
		t.Fatalf("缺省 Status 应为 active，得 %q", m.Status)
	}
	if m.Translations["en"] != "x" {
		t.Fatalf("译文丢失: %v", m.Translations)
	}
	if ToGlossaryModel(nil) != nil || ToGlossaryModelUpdate(nil) != nil {
		t.Fatal("nil 输入应返回 nil")
	}
	up := ToGlossaryModelUpdate(&dto.GlossaryUpdateRequest{
		Category: "c2", Status: "disabled", Translations: map[string]string{"zh": "中"},
	})
	if up.Status != "disabled" || up.Category != "c2" {
		t.Fatalf("Update 映射错误: %+v", up)
	}
	if FromGlossaryModel(nil) != nil {
		t.Fatal("FromGlossaryModel(nil) 应为 nil")
	}
}
