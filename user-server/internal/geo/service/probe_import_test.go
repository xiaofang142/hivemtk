package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	pkgdb "hivemtk-user/internal/pkg/db"
	"hivemtk-user/internal/pkg/testutil"
)

func TestPerplexityProbe_RealModeAndCitations(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"choices":[{"message":{"content":"HiveMtk 是 AI 销售平台，性价比较高。\n[SENTIMENT: positive]"}}],"citations":["https://example.com/a","https://example.com/b"]}`)
	}))
	defer srv.Close()

	p := &perplexityProbe{name: "perplexity", endpoint: srv.URL, model: "sonar"}
	res, err := p.Probe(context.Background(), "HiveMtk 怎么样")
	if err != nil {
		t.Fatalf("真实探针失败: %v", err)
	}
	if res.Mode != ProbeModeReal || res.Simulated {
		t.Fatalf("Perplexity 必须是 real 且非模拟: %+v", res)
	}
	if len(res.Citations) != 2 || res.Citations[0].URL != "https://example.com/a" {
		t.Fatalf("citations 解析错误: %+v", res.Citations)
	}
	if res.Sentiment != "positive" {
		t.Fatalf("情感解析错误: %s", res.Sentiment)
	}
}

func TestParseProbeCitations_ObjectShape(t *testing.T) {
	cites := parseProbeCitations([]byte(`[{"url":"https://a.example","title":"A"}]`))
	if len(cites) != 1 || cites[0].Title != "A" {
		t.Fatalf("对象形状解析失败: %+v", cites)
	}
}

func TestProbeService_ImportRuns(t *testing.T) {
	database := testutil.NewTestDB(t,
		&model.GeoProbeRun{}, &model.GeoConfig{}, &model.GeoEntity{}, &model.GeoEntityRelation{})
	if database == nil {
		t.Fatal("测试库不可达：导入用例判不了")
	}
	pkgdb.SetTestDB(database)
	repo := repository.NewGeoProbeRunRepositoryWithDB(database)
	svc := NewProbeService(nil, repo)

	mention := true
	created, skipped, errs := svc.ImportRuns(context.Background(), []ProbeImportRow{
		{Engine: "perplexity", Query: "HiveMtk 怎么样", Response: "推荐 HiveMtk", Citations: []string{"https://example.com/a"}, BrandMentioned: &mention, Sentiment: "positive"},
		{Engine: "perplexity", Query: "HiveMtk 怎么样", Response: "推荐 HiveMtk"}, // 同批重复
		{Engine: "", Query: "x", Response: "y"}, // 缺 engine
	})
	if created != 1 || skipped != 2 {
		t.Fatalf("导入计数错误: created=%d skipped=%d errs=%v", created, skipped, errs)
	}
	var runs []*model.GeoProbeRun
	if err := database.Find(&runs).Error; err != nil {
		t.Fatalf("查导入结果失败: %v", err)
	}
	if len(runs) != 1 || runs[0].Mode != ProbeModeManual {
		t.Fatalf("导入行必须标 manual: %+v", runs)
	}
	if runs[0].Sentiment != "positive" || !runs[0].BrandMentioned {
		t.Fatalf("导入字段错误: %+v", runs[0])
	}
}
