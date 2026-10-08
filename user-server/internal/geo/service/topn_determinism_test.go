package service

import (
	"context"
	"strconv"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/geo/model"
)

// GEO 报表里有一类共同形状：先把探针/验证数据累进 map，再把 map 摊成切片按计数降序排，
// 有的还要再截前 N 个。只比数值字段时，并列项的先后取自 map 迭代序——Go 每轮 range 都随机，
// 于是同一份数据两次请求能给出不同的榜单；带截断的那几处更糟，连「哪 N 个进榜单」这个集合本身都在漂。
//
// 本文件把这条不变量钉住：并列必须由名字升序兜底，结果可复现。
// 每个用例跑多轮，是因为单看一轮永远绿——随机性只有跨轮才暴露。

const determinismRounds = 200

func assertStableAcrossRounds(t *testing.T, name string, produce func() string) {
	t.Helper()
	first := produce()
	for round := 2; round <= determinismRounds; round++ {
		if got := produce(); got != first {
			t.Fatalf("%s 第 %d 轮与第 1 轮不一致（并列项顺序取自 map 迭代序＝每次刷新都在换榜单）：\n  第 1 轮=%q\n  第 %d 轮=%q",
				name, round, first, round, got)
		}
	}
}

func TestTopKeys_TieBrokenByName(t *testing.T) {
	m := map[string]int{"zeta": 7, "alpha": 7, "mid": 7, "beta": 3}
	assertStableAcrossRounds(t, "topKeys(全并列取前 2)", func() string {
		return strings.Join(topKeys(m, 2), ",")
	})
	// 同分取名字升序的两个：alpha、beta？beta 只有 3 分。应是 alpha、mid。
	got := topKeys(m, 2)
	if strings.Join(got, ",") != "alpha,mid" {
		t.Fatalf("同分应按名字升序取前 2，实际=%v", got)
	}
}

func TestAggregateKeywordDomainMatrix_TieBrokenByName(t *testing.T) {
	// a.com 与 b.com 各 2 次提及＝并列。原实现只比 count，截前 1 时赢家随 map 迭代序变。
	runs := []*model.GeoProbeRun{
		mkRun("e1", "q1", []string{"https://b.com/1", "https://a.com/1"}),
		mkRun("e1", "q1", []string{"https://a.com/2"}),
		mkRun("e1", "q2", []string{"https://b.com/2"}),
	}
	assertStableAcrossRounds(t, "关键词×域名矩阵(域名并列取前 2)", func() string {
		domains, rows := AggregateKeywordDomainMatrix(runs, 2, 2)
		var sb strings.Builder
		sb.WriteString(strings.Join(domains, ","))
		for _, r := range rows {
			sb.WriteString(" | " + r.Query + "=" + strconv.Itoa(r.Total))
		}
		return sb.String()
	})
	domains, _ := AggregateKeywordDomainMatrix(runs, 2, 1)
	if strings.Join(domains, ",") != "a.com" {
		t.Fatalf("并列时 Top-1 域名应是名字升序首位 a.com，实际=%v", domains)
	}
}

func TestAggregateCitationDomains_TieBrokenByName(t *testing.T) {
	// 四个域名各 1 次引用，全并列；顺序必须稳定且按域名升序。
	runs := []*model.GeoProbeRun{
		mkRun("e1", "q1", []string{"https://delta.com/1"}),
		mkRun("e1", "q2", []string{"https://alpha.com/1"}),
		mkRun("e1", "q3", []string{"https://charlie.com/1"}),
		mkRun("e1", "q4", []string{"https://bravo.com/1"}),
	}
	assertStableAcrossRounds(t, "引用域名统计(全并列)", func() string {
		out := []string{}
		for _, s := range AggregateCitationDomains(runs, "own.com", nil) {
			out = append(out, s.Domain)
		}
		return strings.Join(out, ",")
	})
	got := AggregateCitationDomains(runs, "own.com", nil)
	want := []string{"alpha.com", "bravo.com", "charlie.com", "delta.com"}
	for i := range want {
		if got[i].Domain != want[i] {
			t.Fatalf("并列域名应按名字升序，第 %d 位 got=%q want=%q", i, got[i].Domain, want[i])
		}
	}
}

func TestShareOfVoice_TieBrokenByName(t *testing.T) {
	// 三个品牌各被提及 1 次 ⇒ SOV 全等；顺序必须可复现。
	svc := &GeoDecisionAnalyticsService{
		configRepo: &stubConfigRepo{cfg: &model.GeoConfig{
			BrandName: "蜂巢", Competitors: "竞品A、竞品B",
		}},
		probeRepo: &stubProbeRepo{runs: []*model.GeoProbeRun{
			probeRun("蜂巢怎么样", "值得一试", "neutral"),
			probeRun("竞品A怎么样", "也不错", "neutral"),
			probeRun("竞品B如何", "一般", "neutral"),
		}},
		crawler: stubCrawlerRepo{},
	}
	assertStableAcrossRounds(t, "GetShareOfVoice(品牌并列)", func() string {
		entries, err := svc.GetShareOfVoice(context.Background(), "")
		if err != nil {
			t.Fatalf("GetShareOfVoice: %v", err)
		}
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Brand)
		}
		return strings.Join(names, ",")
	})
}

// stubVerifyRepo 只喂 GetByBrandName，其余方法不被本用例的路径使用。
type stubVerifyRepo struct{ rows []*model.GeoVerifyResult }

func (r *stubVerifyRepo) ListAllForSOV(_ context.Context, _ string) ([]*model.GeoVerifyResult, error) {
	return r.rows, nil
}
func (r *stubVerifyRepo) Create(_ *model.GeoVerifyResult) error { return nil }
func (r *stubVerifyRepo) GetByArticleID(_ string) ([]*model.GeoVerifyResult, error) {
	return nil, nil
}
func (r *stubVerifyRepo) GetByBrandName(_ string) ([]*model.GeoVerifyResult, error) {
	return r.rows, nil
}
func (r *stubVerifyRepo) GetList(_ string, _, _ int) ([]*model.GeoVerifyResult, int64, error) {
	return nil, 0, nil
}
func (r *stubVerifyRepo) GetStatistics() ([]map[string]any, error) { return nil, nil }

func TestHighValueKeywords_TieBrokenByName(t *testing.T) {
	// 25 个词各出现 1 次且都被提及 ⇒ HighValueScore 全等（10.0）。
	// 实现取前 20 个进「高价值词」：没有名字兜底时，被截掉的是哪 5 个每轮随机。
	rows := make([]*model.GeoVerifyResult, 0, 25)
	names := make([]string, 0, 25)
	for i := 0; i < 25; i++ {
		q := "kw-" + string(rune('A'+i))
		names = append(names, q)
		rows = append(rows, &model.GeoVerifyResult{
			Query: q, BrandMentioned: true, CreatedAt: time.Now(),
		})
	}
	svc := NewKeywordEnhanceService(nil, &stubVerifyRepo{rows: rows}, nil)
	assertStableAcrossRounds(t, "高价值关键词(25 个同分截前 20)", func() string {
		resp, err := svc.AnalyzeHistoricalPerformance(context.Background(), "蜂巢")
		if err != nil {
			t.Fatalf("AnalyzeHistoricalPerformance: %v", err)
		}
		got := []string{}
		for _, p := range resp.HighValueKeywords {
			got = append(got, p.Keyword)
		}
		return strings.Join(got, ",")
	})
	resp, err := svc.AnalyzeHistoricalPerformance(context.Background(), "蜂巢")
	if err != nil {
		t.Fatalf("AnalyzeHistoricalPerformance: %v", err)
	}
	if len(resp.HighValueKeywords) != 20 {
		t.Fatalf("25 个同分词应截前 20，实际 %d", len(resp.HighValueKeywords))
	}
	// 截断后留下的必须是名字升序的前 20 个（kw-A..kw-T），被丢弃的是 kw-U..kw-Y。
	if first, last := resp.HighValueKeywords[0].Keyword, resp.HighValueKeywords[19].Keyword; first != "kw-A" || last != "kw-T" {
		t.Fatalf("同分截断应按关键词升序取前 20，实际首=%q 末=%q", first, last)
	}
}
