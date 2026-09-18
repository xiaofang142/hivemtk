package templates

import (
	"encoding/json"
	"testing"

	"hivemtk-user/internal/model"
)

func TestAllPresets(t *testing.T) {
	list := All()
	if len(list) != 7 {
		t.Fatalf("预置模板应为 7 个，实际 %d", len(list))
	}
	seen := make(map[string]bool, len(list))
	for _, tpl := range list {
		t.Run(tpl.Code, func(t *testing.T) {
			if seen[tpl.Code] {
				t.Fatalf("模板 Code 重复: %s", tpl.Code)
			}
			seen[tpl.Code] = true
			for _, f := range []struct{ name, val string }{
				{"Code", tpl.Code}, {"Name", tpl.Name}, {"Platform", tpl.Platform},
				{"Category", tpl.Category}, {"Version", tpl.Version}, {"APIBase", tpl.APIBase},
				{"AuthType", tpl.AuthType}, {"DocURL", tpl.DocURL},
			} {
				if f.val == "" {
					t.Errorf("%s 不应为空", f.name)
				}
			}
			if !tpl.BuiltIn || !tpl.Enabled {
				t.Errorf("预置模板应 BuiltIn && Enabled: %+v", tpl)
			}

			var auth map[string]any
			if err := json.Unmarshal([]byte(tpl.AuthConfig), &auth); err != nil {
				t.Errorf("AuthConfig 非法 JSON: %v", err)
			}
			var maps []model.FieldMapping
			if err := json.Unmarshal([]byte(tpl.FieldMaps), &maps); err != nil {
				t.Fatalf("FieldMaps 非法 JSON: %v", err)
			}
			if len(maps) == 0 {
				t.Fatal("FieldMaps 不应为空")
			}
			for _, m := range maps {
				if m.Source == "" || m.Target == "" {
					t.Errorf("字段映射缺少 Source/Target: %+v", m)
				}
			}
			var eps []model.EndpointConfig
			if err := json.Unmarshal([]byte(tpl.Endpoints), &eps); err != nil {
				t.Fatalf("Endpoints 非法 JSON: %v", err)
			}
			if len(eps) == 0 {
				t.Fatal("Endpoints 不应为空")
			}
			for _, e := range eps {
				if e.Name == "" || e.Method == "" || e.Path == "" {
					t.Errorf("端点缺少 Name/Method/Path: %+v", e)
				}
			}
		})
	}
}

func TestAllReturnsFreshCopies(t *testing.T) {
	a := All()
	b := All()
	if a[0] == b[0] {
		t.Fatal("All() 每次应返回新实例，避免调用方改坏共享模板")
	}
	a[0].Name = "被改写"
	if All()[0].Name == "被改写" {
		t.Fatal("模板实例被外部修改后污染了后续返回")
	}
}
