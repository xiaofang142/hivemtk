package egolite

import (
	"encoding/json"
	"testing"

	"hivemtk-user/internal/model"

	"gorm.io/datatypes"
)

// T3 用户端分发：清单构建的过滤纪律 + slug 安全化。

func flowFor(id uint, name, status string, scanPassed bool, domains []string, script string) *model.EgoliteFlow {
	d, _ := json.Marshal(domains)
	return &model.EgoliteFlow{
		ID: id, UserID: 1, Name: name, Status: status, ScanPassed: scanPassed,
		DomainsJSON: datatypes.JSON(d), Script: script, Version: 1,
	}
}

func TestBuildInstallableAssetsFilters(t *testing.T) {
	flows := []*model.EgoliteFlow{
		flowFor(1, "github repo search", model.EgoFlowPublished, true, []string{"github.com"}, "console.log(1)"),
		flowFor(2, "draft flow", model.EgoFlowDraft, true, []string{"github.com"}, "x"),         // 非 published
		flowFor(3, "submitted flow", model.EgoFlowSubmitted, true, []string{"github.com"}, "x"), // 审核中
		flowFor(4, "scan failed", model.EgoFlowPublished, false, []string{"github.com"}, "x"),   // 扫描未过
		flowFor(5, "no domains", model.EgoFlowPublished, true, nil, "x"),                        // 无域名
		flowFor(6, "empty script", model.EgoFlowPublished, true, []string{"a.com"}, ""),         // 空脚本
	}
	got, err := BuildInstallableAssets(flows)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("只有 #1 应出门，got %d: %+v", len(got), got)
	}
	a := got[0]
	if a.ID != "github-repo-search" {
		t.Fatalf("id 应 slug 化，got %q", a.ID)
	}
	if a.Entry != "tools/github-repo-search.js" {
		t.Fatalf("entry 应在 tools/ 下，got %q", a.Entry)
	}
	if a.ScriptHash == "" {
		t.Fatal("必须带 script_hash（runner 幂等比对键）")
	}
	if a.Version != 1 || a.FlowID != 1 {
		t.Fatalf("版本/flow id 透传失败: %+v", a)
	}
}

func TestSlugifySafety(t *testing.T) {
	cases := map[string]string{
		"github search":    "github-search",
		"../../etc/passwd": "etcpasswd", // 斜杠丢弃（不拼出新路径段）
		"中文名字":             "",
		"UPPER_Case-123":   "upper_case-123",
		"a b.c":            "a-b-c",
		"---":              "",
	}
	for in, want := range cases {
		if got := slugify(in); got != want {
			t.Errorf("slugify(%q)=%q want %q", in, got, want)
		}
	}
}
