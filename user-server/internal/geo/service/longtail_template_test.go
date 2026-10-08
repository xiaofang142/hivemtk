package service

import (
	"context"
	"regexp"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/geo/model"
)

// 本文件锁的是长尾模板的两条不变量：
//  1. 模板里的占位符只有 {seed} 与 {year} 两种——写第三种就会被原样落库成词；
//  2. 年份不写死在模板里——写死会让整个词库随日历过期。
//
// 起因是线上实测：`{seed}和XX对比哪个好` 里的 XX 是人写的「竞品」占位提示，
// 但展开只替换 {seed}，于是 geo_keywords 里落进 1135 条字面带 XX 的词
// （如「智能客服系统和XX对比哪个好」）——这类词不会被任何人搜索，
// 却会进漏斗统计、进内容任务，属于污染而非数据。

var placeholderRe = regexp.MustCompile(`\{([A-Za-z_][A-Za-z0-9_]*)\}`)
var literalYearRe = regexp.MustCompile(`(?:^|[^0-9])(1[89]\d{2}|20\d{2}|21\d{2})(?:[^0-9]|$)`)

func TestDefaultLongtailTemplates_OnlyKnownPlaceholders(t *testing.T) {
	known := map[string]bool{"seed": true, "year": true}
	for _, tpl := range DefaultLongtailTemplates {
		for _, m := range placeholderRe.FindAllStringSubmatch(tpl.Template, -1) {
			if !known[m[1]] {
				t.Fatalf("模板 %q 含未知占位符 {%s}：展开只认 {seed}/{year}，其余会原样落库成词", tpl.Template, m[1])
			}
		}
	}
}

func TestDefaultLongtailTemplates_NoHardcodedYear(t *testing.T) {
	// 年份一律走 {year}。模板里出现字面年份＝日历引信：次年这些词全部过期。
	for _, tpl := range DefaultLongtailTemplates {
		if m := literalYearRe.FindStringSubmatch(tpl.Template); m != nil {
			t.Fatalf("模板 %q 含字面年份 %s：应改用 {year}", tpl.Template, m[1])
		}
	}
}

func TestDefaultLongtailTemplates_NoPlaceholderLeftInExpandedKeyword(t *testing.T) {
	// 展开后仍带花括号＝把模板语法写进了词库；带字面 XX 是同类事故的另一形态。
	svc := &KeywordMiningService{}
	got, err := svc.CombineLongtail(context.Background(), []string{"智能客服系统"}, DefaultLongtailTemplates)
	if err != nil {
		t.Fatalf("组合长尾词失败：%v", err)
	}
	if len(got) != len(DefaultLongtailTemplates) {
		t.Fatalf("23 模板 × 1 种子词应展开 %d 条，实际 %d 条", len(DefaultLongtailTemplates), len(got))
	}
	year := time.Now().Format("2006")
	for _, kw := range got {
		if strings.ContainsAny(kw.Keyword, "{}") {
			t.Fatalf("展开结果残留占位符括号：%q", kw.Keyword)
		}
		if strings.Contains(kw.Keyword, "XX") {
			t.Fatalf("展开结果含字面 XX：%q", kw.Keyword)
		}
		if strings.Contains(kw.Keyword, "{seed}") {
			t.Fatalf("{seed} 未被替换：%q", kw.Keyword)
		}
		if strings.Contains(kw.Keyword, "{year}") {
			t.Fatalf("{year} 未被替换：%q", kw.Keyword)
		}
	}
	// 三条带 {year} 的模板必须真的带上当前年份。
	want := map[string]bool{
		year + "年智能客服系统对比评测":    true,
		year + "年智能客服系统推荐":      true,
		"中小企业智能客服系统选型指南" + year: true,
	}
	hit := map[string]bool{}
	for _, kw := range got {
		if want[kw.Keyword] {
			hit[kw.Keyword] = true
		}
	}
	if len(hit) != len(want) {
		t.Fatalf("带 {year} 的模板展开不全，命中 %d/%d：%v", len(hit), len(want), keywordsOf(got))
	}
}

func TestCombineLongtail_SubstitutesCurrentYear(t *testing.T) {
	svc := &KeywordMiningService{}
	year := time.Now().Format("2006")
	got, err := svc.CombineLongtail(context.Background(), []string{"SCRM"}, []LongtailTemplate{
		{Template: "{year}年{seed}推荐", QueryIntent: "recommendation", FunnelStage: "evaluation"},
	})
	if err != nil {
		t.Fatalf("组合失败：%v", err)
	}
	if len(got) != 1 {
		t.Fatalf("应展开 1 条，实际 %d", len(got))
	}
	want := year + "年SCRM推荐"
	if got[0].Keyword != want {
		t.Fatalf("{year} 展开错：got=%q want=%q", got[0].Keyword, want)
	}
}

func TestCombineLongtail_DropsUnknownPlaceholder(t *testing.T) {
	// 管理员在 DB 里写了引擎不认识的占位符时，丢弃该词而不是落库字面花括号。
	svc := &KeywordMiningService{}
	got, err := svc.CombineLongtail(context.Background(), []string{"SCRM"}, []LongtailTemplate{
		{Template: "{seed}和{brand}对比哪个好", QueryIntent: "comparison", FunnelStage: "evaluation"},
		{Template: "{seed}怎么选", QueryIntent: "how_to", FunnelStage: "cognitive"},
	})
	if err != nil {
		t.Fatalf("组合失败：%v", err)
	}
	if len(got) != 1 {
		t.Fatalf("含未知占位符的模板应被丢弃，只留 1 条，实际 %d：%v", len(got), keywordsOf(got))
	}
	if got[0].Keyword != "SCRM怎么选" {
		t.Fatalf("留下的应是合法模板，实际=%q", got[0].Keyword)
	}
}

func TestCombineLongtail_CaseStudyTemplatesAreSearchable(t *testing.T) {
	// 「某企业」不是任何人会输入的搜索词，属 filler；comparison 里的 XX 同理。
	// 这里锁住两条已改写的模板不再回到旧形态。
	for _, tpl := range DefaultLongtailTemplates {
		if strings.Contains(tpl.Template, "某企业") || strings.Contains(tpl.Template, "XX") {
			t.Fatalf("模板回退成不可搜索的 filler 形态：%q", tpl.Template)
		}
	}
}

// keywordsOf 把展开结果转成词列表，只用于失败信息可读。
func keywordsOf(rows []*model.GeoKeyword) []string {
	out := make([]string, 0, len(rows))
	for _, r := range rows {
		out = append(out, r.Keyword)
	}
	return out
}

func TestCombineLongtail_OrderIsExpansionOrder(t *testing.T) {
	// 结果顺序必须等于「种子→模板」的展开顺序，且重复词只留第一次出现的那条。
	// 此前函数把去重用的 map 直接摊成切片返回，Go 每轮 range 随机 ⇒ 同一请求两次
	// 返回同样 23 条却顺序不同（实测复现于长尾接口的两次连续调用）。
	svc := &KeywordMiningService{}
	tpls := []LongtailTemplate{
		{Template: "{seed}甲", QueryIntent: "how_to"},
		{Template: "{seed}甲"}, // 与上一条展开后同词，应被去重丢弃
		{Template: "{seed}乙", QueryIntent: "comparison"},
		{Template: "{seed}丙", QueryIntent: "decision"},
	}
	got, err := svc.CombineLongtail(context.Background(), []string{"S1", "S2"}, tpls)
	if err != nil {
		t.Fatalf("组合失败：%v", err)
	}
	want := []string{"S1甲", "S1乙", "S1丙", "S2甲", "S2乙", "S2丙"}
	if strings.Join(keywordsOf(got), ",") != strings.Join(want, ",") {
		t.Fatalf("顺序或去重不符：\n  got =%v\n  want=%v", keywordsOf(got), want)
	}
	// 去重后保留的必须是首次出现那条（带着首次的意图），不是被覆盖后的空意图。
	if got[0].QueryIntent != "how_to" {
		t.Fatalf("重复词应保留首次出现的行，意图应为 how_to，实际=%q", got[0].QueryIntent)
	}
}

func TestCombineLongtail_OrderStableAcrossRounds(t *testing.T) {
	svc := &KeywordMiningService{}
	tpls := []LongtailTemplate{{Template: "{seed}甲"}, {Template: "{seed}乙"}, {Template: "{seed}丙"}}
	seeds := []string{"A", "B", "C", "D"}
	assertStableAcrossRounds(t, "CombineLongtail(4 种子×3 模板)", func() string {
		got, err := svc.CombineLongtail(context.Background(), seeds, tpls)
		if err != nil {
			t.Fatalf("组合失败：%v", err)
		}
		return strings.Join(keywordsOf(got), ",")
	})
}
