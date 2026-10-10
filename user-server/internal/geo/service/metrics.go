package service

import (
	"regexp"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// StructureElements 内容结构元素
type StructureElements struct {
	Headings   int `json:"headings"`
	Lists      int `json:"lists"`
	CodeBlocks int `json:"code_blocks"`
	FAQPairs   int `json:"faq_pairs"`
	Tables     int `json:"tables"`
	Quotes     int `json:"quotes"`
}

// MetricsResult 内容质量指标结果
type MetricsResult struct {
	Keyword             string            `json:"keyword,omitempty"`
	TextLength          int               `json:"text_length"`
	TextLengthNoSpace   int               `json:"text_length_no_space"`
	TrustSignals        int               `json:"trust_signals"`
	Citations           int               `json:"citations"`
	BrandMentions       int               `json:"brand_mentions"`
	AllMentions         int               `json:"all_mentions"`
	TrustDensity        float64           `json:"trust_density"`
	CitationShare       float64           `json:"citation_share"`
	AuthorityScore      float64           `json:"authority_score"`
	EngagementPotential float64           `json:"engagement_potential"`
	Structure           StructureElements `json:"structure"`
}

// MetricsService GEO 内容质量指标服务（迁移自 AIGEOTOOLS metrics/service.go）
type MetricsService struct{}

// NewMetricsService 创建内容质量指标服务
func NewMetricsService() *MetricsService {
	return &MetricsService{}
}

var (
	headingRe      = regexp.MustCompile(`(?m)^#{1,6}\s+.+`)
	listItemRe     = regexp.MustCompile(`^\s*(?:[-*+]|\d+[.)、])\s+`)
	codeFenceRe    = regexp.MustCompile("(?m)^```")
	faqQRe         = regexp.MustCompile(`^\s*[Qq][：:].*`)
	tableRowRe     = regexp.MustCompile(`^\s*\|.*\|`)
	quoteRe        = regexp.MustCompile(`^>\s+.+`)
	percentRe      = regexp.MustCompile(`\d+(\.\d+)?%`)
	decimalRe      = regexp.MustCompile(`\d+\.\d+`)
	trustSignalRe  = regexp.MustCompile(`(?:根据|参考|来自|据)\s*[^，。,；;\n]{2,30}(?:报告|研究|数据|统计|调查|分析)`)
	dataSignalRe   = regexp.MustCompile(`\d+%|\d+\.\d+%|约\d+%|超过\d+%|达到\d+%|\d+倍|\d+个|\d+项|\d+次|\d+年|\d+月`)
	caseSignalRe   = regexp.MustCompile(`案例[：:][^，。；\n]{5,100}|例如[^，。；\n]{5,100}|以[^，。；\n]{2,30}为例`)
	brandMentionRe = regexp.MustCompile(`\b[A-Z][a-zA-Z0-9]{2,20}\b`)

	brandReCache sync.Map
)

func brandRegex(brand string) *regexp.Regexp {
	if v, ok := brandReCache.Load(brand); ok {
		return v.(*regexp.Regexp)
	}
	re := regexp.MustCompile(`\b` + regexp.QuoteMeta(brand) + `\b`)
	v, _ := brandReCache.LoadOrStore(brand, re)
	return v.(*regexp.Regexp)
}

// DefaultPositionDedupWindow 品牌词命中去重窗口的代码兜底（参数中心
// telemetry.geo_position_window 未配置时用）。单位是**字符偏移**不是条数：
// 相邻两处命中的间隔小于该值时算同一处提及（"Apple 发布会" 不会被数成两次）。
const DefaultPositionDedupWindow = 20

var positionDedupWindowProvider func() int

// SetPositionDedupWindowProvider 注入命中去重窗口（字符）；传 nil 视为不注入。
func SetPositionDedupWindowProvider(fn func() int) { positionDedupWindowProvider = fn }

// positionDedupWindow 生效的去重窗口。非正值一律回落兜底——0 会让每一次命中都被
// 算成独立提及，品牌提及量成倍虚高。
func positionDedupWindow() int {
	if p := positionDedupWindowProvider; p != nil {
		if n := p(); n > 0 {
			return n
		}
	}
	return DefaultPositionDedupWindow
}

type matchPos struct{ start, end int }

func dedupMatches(allMatches []matchPos) int {
	if len(allMatches) == 0 {
		return 0
	}
	sort.Slice(allMatches, func(i, j int) bool {
		return allMatches[i].start < allMatches[j].start
	})
	count := 0
	w := positionDedupWindow()
	prevEnd := -w - 1
	for _, m := range allMatches {
		if m.start-prevEnd > w {
			count++
			prevEnd = m.end
		}
	}
	return count
}

func (s *MetricsService) countMatches(content string, re *regexp.Regexp) int {
	locs := re.FindAllStringIndex(content, -1)
	matches := make([]matchPos, 0, len(locs))
	for _, loc := range locs {
		matches = append(matches, matchPos{start: loc[0], end: loc[1]})
	}
	return dedupMatches(matches)
}

// CountTrustSignals 计算可信度信号数量
func (s *MetricsService) CountTrustSignals(content string) int {
	if content == "" {
		return 0
	}
	count := s.countMatches(content, trustSignalRe)
	count += s.countMatches(content, dataSignalRe)
	count += s.countMatches(content, caseSignalRe)
	return count
}

// CountCitations 计算引用数量
func (s *MetricsService) CountCitations(content string) int {
	if content == "" {
		return 0
	}
	return s.countMatches(content, trustSignalRe)
}

// CountBrandMentions 计算品牌提及次数
func (s *MetricsService) CountBrandMentions(content, brand string) int {
	if content == "" || brand == "" {
		return 0
	}
	return len(brandRegex(brand).FindAllString(content, -1))
}

// CountAllMentions 计算所有专有名词提及
func (s *MetricsService) CountAllMentions(content string) int {
	if content == "" {
		return 0
	}
	return len(brandMentionRe.FindAllString(content, -1))
}

// CountStructureElements 计算结构元素
func (s *MetricsService) CountStructureElements(content string) StructureElements {
	var se StructureElements
	if content == "" {
		return se
	}
	for _, line := range strings.Split(content, "\n") {
		trimmed := strings.TrimSpace(line)
		if headingRe.MatchString(trimmed) {
			se.Headings++
		}
		if listItemRe.MatchString(trimmed) {
			se.Lists++
		}
		if faqQRe.MatchString(trimmed) {
			se.FAQPairs++
		}
		if tableRowRe.MatchString(trimmed) {
			se.Tables++
		}
		if quoteRe.MatchString(trimmed) {
			se.Quotes++
		}
	}
	se.CodeBlocks = len(codeFenceRe.FindAllString(content, -1)) / 2
	return se
}

func (s *MetricsService) textLengthNoSpace(content string) int {
	length := 0
	for _, r := range content {
		if !unicode.IsSpace(r) {
			length++
		}
	}
	return length
}

// CountDataPoints 计算数据点数量
func (s *MetricsService) CountDataPoints(content string) int {
	if content == "" {
		return 0
	}
	return len(percentRe.FindAllString(content, -1)) + len(decimalRe.FindAllString(content, -1))
}

func minFloat(a, b float64) float64 {
	if a < b {
		return a
	}
	return b
}

// CalculateAuthorityScore 计算权威度得分
func (s *MetricsService) CalculateAuthorityScore(content string) float64 {
	if content == "" {
		return 0
	}
	citations := s.CountCitations(content)
	trustSignals := s.CountTrustSignals(content)
	noSpace := s.textLengthNoSpace(content)
	if noSpace == 0 {
		return 0
	}
	citationScore := minFloat(float64(citations)*5.0, 30.0)
	trustDensity := float64(trustSignals) / float64(noSpace) * 1000
	trustScore := minFloat(trustDensity*4.0, 40.0)
	dataScore := minFloat(float64(s.CountDataPoints(content))*2.0, 30.0)
	score := citationScore + trustScore + dataScore
	if score > 100 {
		score = 100
	}
	return score
}

// CalculateEngagementPotential 计算互动潜力
func (s *MetricsService) CalculateEngagementPotential(content string) float64 {
	if content == "" {
		return 0
	}
	se := s.CountStructureElements(content)
	score := minFloat(float64(se.Headings)*2.0, 20.0)
	score += minFloat(float64(se.Lists)*1.5, 25.0)
	score += minFloat(float64(se.FAQPairs)*3.0, 25.0)
	score += minFloat(float64(se.CodeBlocks)*5.0, 15.0)
	score += minFloat(float64(se.Tables)*2.0, 10.0)
	score += minFloat(float64(se.Quotes)*1.0, 5.0)
	if score > 100 {
		score = 100
	}
	return score
}

// Analyze 分析内容质量指标
func (s *MetricsService) Analyze(content, keyword, brand string) MetricsResult {
	trustSignals := s.CountTrustSignals(content)
	citations := s.CountCitations(content)
	brandMentions := s.CountBrandMentions(content, brand)
	allMentions := s.CountAllMentions(content)
	structure := s.CountStructureElements(content)
	textLen := len(content)
	textLenNoSpace := s.textLengthNoSpace(content)

	trustDensity := 0.0
	if textLenNoSpace > 0 {
		trustDensity = float64(trustSignals) / float64(textLenNoSpace) * 100
	}
	citationShare := 0.0
	if allMentions > 0 {
		citationShare = float64(brandMentions) / float64(allMentions) * 100
	}
	return MetricsResult{
		Keyword:             keyword,
		TextLength:          textLen,
		TextLengthNoSpace:   textLenNoSpace,
		TrustSignals:        trustSignals,
		Citations:           citations,
		BrandMentions:       brandMentions,
		AllMentions:         allMentions,
		TrustDensity:        trustDensity,
		CitationShare:       citationShare,
		AuthorityScore:      s.CalculateAuthorityScore(content),
		EngagementPotential: s.CalculateEngagementPotential(content),
		Structure:           structure,
	}
}

// ProbePositionDedupWindow 导出当前生效的命中去重窗口（仅供装配层测试断言读取口）。
func ProbePositionDedupWindow() int { return positionDedupWindow() }
