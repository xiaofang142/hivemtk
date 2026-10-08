package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/utils/logger"

	"golang.org/x/text/encoding/simplifiedchinese"
	"golang.org/x/text/transform"
	"gorm.io/gorm"
)

// KeywordMiningService 关键词蒸馏服务（GEO v2 新增）
//
// 负责：下拉词 API 抓取、模板化长尾词组合、LLM 意图分类、4 层漏斗统计。
// 是 KeywordService（LLM 造词）的补充——提供真实用户搜索数据。
type KeywordMiningService struct {
	kwRepo repository.GeoKeywordRepository
	db     *gorm.DB
	llm    *LLMAdapter
}

func NewKeywordMiningService(
	kwRepo repository.GeoKeywordRepository,
	db *gorm.DB,
	llm *LLMAdapter,
) *KeywordMiningService {
	return &KeywordMiningService{kwRepo: kwRepo, db: db, llm: llm}
}

// ────────────────────────────────────────────
// Suggest 引擎接口（5 引擎并发）
// ────────────────────────────────────────────

type SuggestEngine interface {
	Name() string
	Fetch(ctx context.Context, keyword string) ([]string, error)
}

// JSONP 剥壳：从最外层回调括号里取出实参。
//
// 不能用 `^\w+\((.*)\);?$` —— 它假设响应形如 `cb(...)`，而实测两类上游都不满足：
//   - bing/google：`if(typeof cb == 'function') cb({...});` 守卫里也有括号，
//     贪婪匹配会把「守卫的 (」当成回调的「(」，剥出 `typeof cb == 'function') cb({...}` 这种垃圾。
//   - baidu：`cb({q:"x",p:false,s:[...]});` 剥壳后仍是 JS 对象字面量（键无引号），不是 JSON。
//
// 这里只做「取最外层实参」这一件事，不去改内文引号；内文引号由 looseUnmarshal 处理。
// jsonpGuardKeywords 是响应里可能出现在回调实参之前的控制语句前缀。
// bing/google 的真实响应形如 `if(typeof cb == 'function') cb({...});`，
// 守卫里也有括号，直接取「第一个 ( 到最后一个 )」会剥出垃圾。
var jsonpGuardKeywords = map[string]bool{"if": true, "for": true, "while": true, "switch": true}

func stripJSONP(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return t
	}
	// 已是纯 JSON（对象或数组）就不要动，避免把合法输入改坏。
	if strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
		return t
	}
	// 先逐层跳过 if(...)/for(...) 之类守卫前缀，剥到真正的回调调用为止。
	for {
		kw, ok := leadingKeyword(t)
		if !ok || !jsonpGuardKeywords[kw] {
			break
		}
		open := strings.IndexByte(t, '(')
		closed := matchParen(t, open)
		if closed < 0 {
			break
		}
		t = strings.TrimSpace(t[closed+1:])
		if strings.HasPrefix(t, "{") || strings.HasPrefix(t, "[") {
			return t
		}
	}
	open := strings.IndexByte(t, '(')
	if open < 0 {
		return t
	}
	// 用配对匹配而不是「取最后一个 )」：bing 真实响应尾部带 `/* pageview_candidate */);`，
	// 取最后一个 ) 会落到那段注释的括号上，剥出以 `/*` 收尾的垃圾（invalid character '/'）。
	closed := matchParen(t, open)
	if closed <= open {
		return t
	}
	return t[open+1 : closed]
}

// leadingKeyword 取响应开头的标识符（若其后紧跟 '('）。
func leadingKeyword(s string) (string, bool) {
	i := 0
	if i >= len(s) || !isIdentStart(s[i]) {
		return "", false
	}
	for i < len(s) && isIdentPart(s[i]) {
		i++
	}
	kw := s[:i]
	j := i
	for j < len(s) && (s[j] == ' ' || s[j] == '\t') {
		j++
	}
	if j >= len(s) || s[j] != '(' {
		return "", false
	}
	return kw, true
}

// matchParen 返回与 s[open] 这个左括号配对的右括号下标，考虑字符串字面量与转义。
// 找不到返回 -1（畸形输入，不 panic）。
func matchParen(s string, open int) int {
	if open < 0 || open >= len(s) || s[open] != '(' {
		return -1
	}
	depth := 0
	inStr := false
	esc := false
	for i := open; i < len(s); i++ {
		c := s[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '(':
			depth++
		case ')':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// looseUnmarshal 宽松 JSON 解析：容忍 JS 对象字面量里未加引号的键。
//
// 存在的唯一理由是百度 suggestion 接口返回 `cb({q:"x",p:false,s:[...]});` ——
// 键不带引号，标准 encoding/json 直接报 invalid character 'q'。
// 做法是把「标识符形式的键」补上双引号后再走标准解析；补引号只在
// `{` 或 `,` 之后且后面跟合法标识符时发生，字符串字面量内部不受影响。
func looseUnmarshal(data []byte, v any) error {
	if err := json.Unmarshal(data, v); err == nil {
		return nil
	}
	fixed := quoteBareJSONKeys(string(data))
	if fixed == string(data) {
		return json.Unmarshal(data, v) // 没能修，返回原始错误，别用被改过的内容掩盖真实原因
	}
	if err2 := json.Unmarshal([]byte(fixed), v); err2 == nil {
		return nil
	} else {
		return err2
	}
}

// quoteBareJSONKeys 给未加引号的 JSON 键补上双引号（仅键位置）。
func quoteBareJSONKeys(s string) string {
	var b strings.Builder
	b.Grow(len(s) + 16)
	inStr := false
	esc := false
	// expectKey 为真表示当前位置处于「一个键即将开始」的位置。
	expectKey := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if inStr {
			b.WriteByte(c)
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		if c == '"' {
			inStr = true
			b.WriteByte(c)
			expectKey = false
			continue
		}
		if c == '{' || c == ',' {
			b.WriteByte(c)
			expectKey = true
			continue
		}
		if c == ':' || c == ' ' || c == '\t' || c == '\n' || c == '\r' {
			// 值位置/空白：原样透传，且键阶段结束
			if c == ':' {
				expectKey = false
			}
			b.WriteByte(c)
			continue
		}
		if expectKey && isIdentStart(c) {
			j := i
			for j < len(s) && isIdentPart(s[j]) {
				j++
			}
			b.WriteByte('"')
			b.WriteString(s[i:j])
			b.WriteByte('"')
			i = j - 1
			expectKey = false
			continue
		}
		b.WriteByte(c)
	}
	return b.String()
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

func isIdentPart(c byte) bool { return isIdentStart(c) || (c >= '0' && c <= '9') }

// firstJSONArg 取出实参里的第一个**完整** JSON 值，它之后的内容（多余实参、行尾注释）一律丢掉。
//
// 两类上游都需要它，而失效方式不同：
//   - google/sogou：回调带多个实参，如 `cb(["q",["a","b"],[],{...}],-1)`，
//     整段丢给 json.Unmarshal 会报 invalid character ',' after top-level value；
//   - bing：注释写在**回调括号内**——`cb({"AS":{...}}/* pageview_candidate */);`，
//     剥壳拿到的是 `{...}/* … */`，同样报 "after top-level value"（红因是 '/'）。
//
// 所以判据不能是「按顶层逗号切」，必须是「扫到第一个闭合的值就收口」。
// 只取第一个值是因为下拉词恒在第一个参数里（google 第一参数是 [query,[suggests],...]）。
func firstJSONArg(s string) string {
	t := strings.TrimSpace(s)
	if t == "" {
		return t
	}
	if t[0] != '[' && t[0] != '{' {
		return t
	}
	depth := 0
	inStr := false
	esc := false
	for i := 0; i < len(t); i++ {
		c := t[i]
		if inStr {
			switch {
			case esc:
				esc = false
			case c == '\\':
				esc = true
			case c == '"':
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case '[', '{':
			depth++
		case ']', '}':
			depth--
			if depth == 0 {
				return t[:i+1]
			}
		}
	}
	// 始终没闭合：原样交回，让下游报出真实解析错，而不是在这里造一个空结果。
	return t
}

// BaiduSuggestEngine 百度下拉词
type BaiduSuggestEngine struct{ client *http.Client }

func (e *BaiduSuggestEngine) Name() string { return "baidu" }
func (e *BaiduSuggestEngine) Fetch(ctx context.Context, kw string) ([]string, error) {
	u := fmt.Sprintf("https://suggestion.baidu.com/su?wd=%s&cb=cb&ie=utf-8", url.QueryEscape(kw))
	return fetchJSONPArray(ctx, e.client, u, "s")
}

// BingSuggestEngine Bing 下拉词
type BingSuggestEngine struct{ client *http.Client }

func (e *BingSuggestEngine) Name() string { return "bing" }
func (e *BingSuggestEngine) Fetch(ctx context.Context, kw string) ([]string, error) {
	u := fmt.Sprintf("https://api.bing.com/qsonhs.aspx?q=%s&type=cb&cb=cb", url.QueryEscape(kw))
	body, err := fetchRaw(ctx, e.client, u)
	if err != nil {
		return nil, err
	}
	body = firstJSONArg(stripJSONP(body))
	var resp struct {
		AS struct {
			Results []struct {
				Suggests []struct{ Txt string } `json:"Suggests"`
			} `json:"Results"`
		} `json:"AS"`
	}
	if err := looseUnmarshal([]byte(body), &resp); err != nil {
		return nil, err
	}
	out := []string{}
	for _, r := range resp.AS.Results {
		for _, s := range r.Suggests {
			if s.Txt != "" {
				out = append(out, s.Txt)
			}
		}
	}
	return out, nil
}

// GoogleSuggestEngine Google 下拉词
type GoogleSuggestEngine struct{ client *http.Client }

func (e *GoogleSuggestEngine) Name() string { return "google" }
func (e *GoogleSuggestEngine) Fetch(ctx context.Context, kw string) ([]string, error) {
	u := fmt.Sprintf("https://suggestqueries.google.com/complete/search?client=firefox&hl=zh-CN&q=%s&callback=cb", url.QueryEscape(kw))
	body, err := fetchRaw(ctx, e.client, u)
	if err != nil {
		return nil, err
	}
	// google 回调带多个实参（真实形状 `cb && cb(["q",["a"],[],{...}])`），
	// 只取第一实参——下拉词恒在第一参数里。
	body = firstJSONArg(stripJSONP(body))
	var arr []any
	if err := looseUnmarshal([]byte(body), &arr); err != nil {
		return nil, err
	}
	if len(arr) >= 2 {
		if items, ok := arr[1].([]any); ok {
			out := []string{}
			for _, it := range items {
				if s, ok := it.(string); ok && s != "" {
					out = append(out, s)
				}
			}
			return out, nil
		}
	}
	return nil, nil
}

// So360SuggestEngine 360 下拉词
type So360SuggestEngine struct{ client *http.Client }

func (e *So360SuggestEngine) Name() string { return "360" }
func (e *So360SuggestEngine) Fetch(ctx context.Context, kw string) ([]string, error) {
	u := fmt.Sprintf("https://sug.so.360.cn/suggest?format=json&word=%s&callback=cb", url.QueryEscape(kw))
	body, err := fetchRaw(ctx, e.client, u)
	if err != nil {
		return nil, err
	}
	body = stripJSONP(body)
	var resp struct {
		Result []struct{ Word string } `json:"result"`
	}
	if err := looseUnmarshal([]byte(body), &resp); err != nil {
		return nil, err
	}
	out := []string{}
	for _, r := range resp.Result {
		if r.Word != "" {
			out = append(out, r.Word)
		}
	}
	return out, nil
}

// SogouSuggestEngine 搜狗下拉词
type SogouSuggestEngine struct{ client *http.Client }

func (e *SogouSuggestEngine) Name() string { return "sogou" }

// Fetch 搜狗线上响应形如 `window.sogou.sug(["q",["a","b"],…],-1)`，三处都得处理：
//   - 回调名带点（`window.sogou.sug`），不是 `^\w+\(` 那种裸标识符；
//   - 回调带第二个实参 `-1`；
//   - 正文是 GBK（无 utf-8 开关参数，实测六种后缀一律仍回 GBK），
//     由 fetchRaw 统一解码成 UTF-8 —— 必须在解码之后才做括号配对，
//     因为 GBK 的尾字节可以落在 `[`/`]` 的字节值上。
func (e *SogouSuggestEngine) Fetch(ctx context.Context, kw string) ([]string, error) {
	u := fmt.Sprintf("https://sor.html5.qq.com/api/getsug?key=%s", url.QueryEscape(kw))
	body, err := fetchRaw(ctx, e.client, u)
	if err != nil {
		return nil, err
	}
	body = firstJSONArg(stripJSONP(body))
	var arr []any
	if err := looseUnmarshal([]byte(body), &arr); err != nil {
		return nil, err
	}
	if len(arr) >= 2 {
		if items, ok := arr[1].([]any); ok {
			out := []string{}
			for _, it := range items {
				if s, ok := it.(string); ok && s != "" {
					out = append(out, s)
				}
			}
			return out, nil
		}
	}
	return nil, nil
}

func newHTTPClient() *http.Client {
	return &http.Client{Timeout: 8 * time.Second}
}

func fetchRaw(ctx context.Context, client *http.Client, u string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	// 非 2xx 直接判错：否则上游 403/500 的 HTML 错误页会被下游当 JSONP 剥壳再解析，
	// 最终报「invalid character '<'」这种与真实原因无关的错，排查成本极高。
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		snippet := strings.TrimSpace(toUTF8(b))
		if r := []rune(snippet); len(r) > 120 {
			snippet = string(r[:120]) + "…"
		}
		return "", fmt.Errorf("上游返回 HTTP %d：%s", resp.StatusCode, snippet)
	}
	return toUTF8(b), nil
}

// toUTF8 把上游响应统一收成 UTF-8 字符串。
//
// 唯一需要它的地方是搜狗：它固定返回 GBK，而 encoding/json 要求合法 UTF-8，
// 且 GBK 的尾字节可以是 `[`/`]` 的字节值 ⇒ 结构扫描必须在解码之后做。
// 已是合法 UTF-8 的正文（其余四引擎）原样返回，不做二次转换。
// 两边都不像的碎字节走 GBK 解码器的替换语义（认不出的字节变 U+FFFD，实测不报错）：
// 输入本就是垃圾，下游的 JSON 解析照样会失败并报出真实原因，这里只保证不 panic、
// 也不把垃圾洗成看着合法的正文。err 分支留着是因为它属于外部输入这一侧，
// 宁可退回原串（错误信息里还能看见真实字节），不要拿半截解码结果冒充上游说的话。
func toUTF8(b []byte) string {
	if utf8.Valid(b) {
		return string(b)
	}
	out, _, err := transform.Bytes(simplifiedchinese.GBK.NewDecoder(), b)
	if err != nil {
		return string(b)
	}
	return string(out)
}

func fetchJSONPArray(ctx context.Context, client *http.Client, u, field string) ([]string, error) {
	body, err := fetchRaw(ctx, client, u)
	if err != nil {
		return nil, err
	}
	body = stripJSONP(body)
	var m map[string]any
	if err := looseUnmarshal([]byte(body), &m); err != nil {
		return nil, err
	}
	val, ok := m[field]
	if !ok {
		return nil, nil
	}
	arr, ok := val.([]any)
	if !ok {
		return nil, nil
	}
	out := []string{}
	for _, it := range arr {
		if s, ok := it.(string); ok && s != "" {
			out = append(out, s)
		}
	}
	return out, nil
}

// ────────────────────────────────────────────
// 公开方法
// ────────────────────────────────────────────

// allEngines 返回全部 5 个引擎实现
func (s *KeywordMiningService) allEngines() []SuggestEngine {
	return []SuggestEngine{
		&BaiduSuggestEngine{client: newHTTPClient()},
		&BingSuggestEngine{client: newHTTPClient()},
		&GoogleSuggestEngine{client: newHTTPClient()},
		&So360SuggestEngine{client: newHTTPClient()},
		&SogouSuggestEngine{client: newHTTPClient()},
	}
}

// SuggestOutcome 一次下拉词抓取的结果 + 诊断。
//
// 带着 PerEngine/Errors 而不是只带关键词，是因为「5 引擎全挂」和「这个词真没下拉词」
// 在只有 count 的响应里长得一模一样（都是 0），而前者是故障、后者是正常。
// 本轮改造前排查就是这么卡的：错误只在服务端 Warn 日志里，调用方看不见。
type SuggestOutcome struct {
	Keywords  []*model.GeoKeyword `json:"keywords"`
	PerEngine map[string]int      `json:"per_engine"`
	Errors    []string            `json:"errors"`
}

// suggestResult 单条下拉词抓取产出（含定位三元组）。
//
// seedIdx/engIdx/kwIdx 存在的理由：抓取是并发的，results 的到达序＝各引擎的完成序，每轮都不同。
// 不带三元组时「谁先到」决定了落库行的 source 与 suggest_engines 的元素序，
// ⇒ 同一个种子两次爬取能把同一个词的来源写成不同值，接口返回的词序也在抖。
type suggestResult struct {
	keyword string
	source  string
	engine  string
	seed    string
	err     error
	seedIdx int
	engIdx  int
	kwIdx   int
}

// sortSuggestResults 按 (种子, 引擎, 引擎内序号) 定序，让抓取结果与调度时序无关。
func sortSuggestResults(results []suggestResult) {
	sort.Slice(results, func(i, j int) bool {
		a, b := results[i], results[j]
		if a.seedIdx != b.seedIdx {
			return a.seedIdx < b.seedIdx
		}
		if a.engIdx != b.engIdx {
			return a.engIdx < b.engIdx
		}
		return a.kwIdx < b.kwIdx
	})
}

// mergeSuggestResults 去重合并成落库集合。
// 调用前须先 sortSuggestResults：seen 只做「这个词见过没」的索引（合并引擎列表时要拿原行指针改），
// 返回集合按首次出现顺序追加——直接从 map 摊出切片的话，range 序每轮随机。
func mergeSuggestResults(results []suggestResult) *SuggestOutcome {
	outcome := &SuggestOutcome{PerEngine: map[string]int{}, Errors: []string{}}
	seen := map[string]*model.GeoKeyword{}
	order := make([]*model.GeoKeyword, 0, len(results))
	for _, r := range results {
		if r.err != nil {
			outcome.Errors = append(outcome.Errors, r.err.Error())
			logger.Warnf("suggest 抓取错误: %v", r.err)
			continue
		}
		kw := strings.TrimSpace(r.keyword)
		if kw == "" {
			continue
		}
		outcome.PerEngine[r.engine]++
		if existing, ok := seen[kw]; ok {
			// 合并 engines：仅新引擎追加，SuggestCount 口径=覆盖引擎数
			var engs []string
			_ = json.Unmarshal([]byte(existing.SuggestEngines), &engs)
			have := map[string]bool{}
			for _, e := range engs {
				have[e] = true
			}
			if !have[r.engine] {
				engs = append(engs, r.engine)
				b, _ := json.Marshal(engs)
				existing.SuggestEngines = string(b)
				existing.SuggestCount++
			}
			continue
		}
		engs, _ := json.Marshal([]string{r.engine})
		row := &model.GeoKeyword{
			Keyword:        kw,
			Source:         r.source,
			Category:       "suggest",
			Layer:          "suggest",
			ParentKeyword:  r.seed,
			Intent:         "info",
			FunnelStage:    "cognitive",
			Status:         "active",
			SuggestEngines: string(engs),
			SuggestCount:   1,
		}
		seen[kw] = row
		order = append(order, row)
	}

	outcome.Keywords = order
	return outcome
}

// CrawlSuggest 并发抓取 5 引擎下拉词
// 每个种子词 × 每个引擎 = 一次请求
// 结果去重合并，source 标记为 suggest_<engine>
// PerEngine 是**去重前**各引擎的原始条数（诊断口径），Keywords 是去重后的落库集合。
// 不返回 error：单引擎失败已经逐条收进 Errors，整批不存在「失败」这一态。
func (s *KeywordMiningService) CrawlSuggest(ctx context.Context, seedWords []string, engines []string) *SuggestOutcome {
	engineMap := map[string]SuggestEngine{}
	for _, e := range s.allEngines() {
		engineMap[e.Name()] = e
	}

	targetEngines := []SuggestEngine{}
	for _, name := range engines {
		if e, ok := engineMap[name]; ok {
			targetEngines = append(targetEngines, e)
		}
	}
	if len(targetEngines) == 0 {
		targetEngines = s.allEngines()
	}

	var (
		wg      sync.WaitGroup
		mu      sync.Mutex
		results []suggestResult
	)

	for si, seed := range seedWords {
		for ei, eng := range targetEngines {
			wg.Add(1)
			go func(seed string, eng SuggestEngine, si, ei int) {
				defer wg.Done()
				suggests, err := eng.Fetch(ctx, seed)
				mu.Lock()
				defer mu.Unlock()
				if err != nil {
					results = append(results, suggestResult{err: fmt.Errorf("[%s] %s: %w", eng.Name(), seed, err), seedIdx: si, engIdx: ei})
					return
				}
				for ki, kw := range suggests {
					results = append(results, suggestResult{
						keyword: kw, source: "suggest_" + eng.Name(), engine: eng.Name(), seed: seed,
						seedIdx: si, engIdx: ei, kwIdx: ki,
					})
				}
			}(seed, eng, si, ei)
		}
	}
	wg.Wait()

	sortSuggestResults(results)
	return mergeSuggestResults(results)
}

// LongtailTemplate 长尾词模板
type LongtailTemplate struct {
	Template    string // 含 {seed} 占位符，可选 {year}（展开时替换为当前年份）
	QueryIntent string // 映射到 GeoKeyword.QueryIntent
	FunnelStage string // 映射到 GeoKeyword.FunnelStage
}

// DefaultLongtailTemplates 默认 23 个模板（6 意图覆盖 how_to/comparison/recommendation/problem/pricing/case_study）
var DefaultLongtailTemplates = []LongtailTemplate{
	// how_to
	{"如何选择{seed}系统", "how_to", "cognitive"},
	{"{seed}系统怎么选", "how_to", "cognitive"},
	{"{seed}选型指南", "how_to", "evaluation"},
	{"中小企业怎么选{seed}", "how_to", "evaluation"},
	// comparison
	{"{seed}免费版和付费版区别", "comparison", "evaluation"},
	{"{seed}和主流产品对比哪个好", "comparison", "evaluation"},
	{"{seed}优缺点分析", "comparison", "evaluation"},
	{"{year}年{seed}对比评测", "comparison", "evaluation"},
	// recommendation
	{"{year}年{seed}推荐", "recommendation", "evaluation"},
	{"{seed}系统排名TOP10", "recommendation", "evaluation"},
	{"中小企业{seed}选型指南{year}", "recommendation", "decision"},
	{"免费{seed}推荐", "recommendation", "decision"},
	// problem
	{"{seed}实施失败的原因", "problem", "cognitive"},
	{"{seed}常见问题解答", "problem", "cognitive"},
	{"{seed}使用痛点", "problem", "cognitive"},
	{"{seed}避坑指南", "problem", "cognitive"},
	// pricing
	{"{seed}多少钱一年", "pricing", "decision"},
	{"{seed}价格对比", "pricing", "decision"},
	{"{seed}免费吗", "pricing", "decision"},
	{"{seed}计费方式", "pricing", "decision"},
	// case_study
	{"{seed}实施案例", "case_study", "retention"},
	{"{seed}成功案例", "case_study", "retention"},
	{"{seed}客户落地实践", "case_study", "retention"},
}

// longtailTemplatesDefaultJSON 缺省 23 模板 JSON（DB 为空/故障时兜底）
var longtailTemplatesDefaultOnce sync.Once
var longtailTemplatesDefaultJSON string

func defaultLongtailTemplatesJSON() string {
	longtailTemplatesDefaultOnce.Do(func() {
		b, err := json.Marshal(DefaultLongtailTemplates)
		if err != nil {
			b = []byte("[]")
		}
		longtailTemplatesDefaultJSON = string(b)
	})
	return longtailTemplatesDefaultJSON
}

// ActiveLongtailTemplates DB 优先读取长尾模板（geo_dicts/longtail_templates.templates），
// 缺行自动播种；DB 故障或空表时 fail-open 回内置 DefaultLongtailTemplates。
// 导出是因为 controller 也要走这条字典入口：它原先直引内置切片，管理员改字典在这条路上不生效。
func ActiveLongtailTemplates() []LongtailTemplate {
	var rows []LongtailTemplate
	if err := DictJSON(model.DictCategoryLongtailTemplates, "templates", defaultLongtailTemplatesJSON(), &rows); err != nil || len(rows) == 0 {
		return DefaultLongtailTemplates
	}
	return rows
}

// CombineLongtail 模板化长尾词组合
// seedWords: 种子词列表
// templates: 不传则用 DB 优先的 ActiveLongtailTemplates（兜底 DefaultLongtailTemplates）
func (s *KeywordMiningService) CombineLongtail(ctx context.Context, seedWords []string, templates []LongtailTemplate) ([]*model.GeoKeyword, error) {
	if len(templates) == 0 {
		templates = ActiveLongtailTemplates()
	}

	// 年份在展开时注入，不写死进模板：写死会让词库随日历过期（历史上 3 个模板硬编码 2026）。
	year := time.Now().Format("2006")

	// 去重用 map 只作为「是否已见过」的集合，结果集本身按 种子→模板 的展开顺序追加：
	// 先前直接从 map 摊出切片，Go 每轮随机化 range 顺序 ⇒ 同一个请求两次返回的词序不同，
	// 前端列表与落库顺序都在抖。
	seen := make(map[string]struct{}, len(seedWords)*len(templates))
	out := make([]*model.GeoKeyword, 0, len(seedWords)*len(templates))
	for _, seed := range seedWords {
		for _, tpl := range templates {
			kw := strings.ReplaceAll(tpl.Template, "{seed}", seed)
			kw = strings.ReplaceAll(kw, "{year}", year)
			if kw == "" {
				continue
			}
			// 模板只认 {seed}/{year}；残留花括号说明管理员写了个引擎不认识的占位符，
			// 原样落库就等于把模板语法写进词库（历史缺陷：`{seed}和XX对比哪个好` 生成过上千条含字面 XX 的词）。
			if strings.ContainsAny(kw, "{}") {
				continue
			}
			if _, dup := seen[kw]; dup {
				continue
			}
			seen[kw] = struct{}{}
			out = append(out, &model.GeoKeyword{
				Keyword:       kw,
				Source:        "template_combined",
				Category:      "longtail",
				Layer:         "longtail",
				ParentKeyword: seed,
				QueryIntent:   tpl.QueryIntent,
				FunnelStage:   tpl.FunnelStage,
				Intent:        tpl.QueryIntent,
				Status:        "active",
			})
		}
	}

	return out, nil
}

// ClassifyIntent LLM 分类 query_intent
// 批量处理，返回 keyword → intent 映射
func (s *KeywordMiningService) ClassifyIntent(ctx context.Context, keywords []string) (map[string]string, error) {
	result := map[string]string{}
	// 简单启发式分类（零 LLM 成本，效果足够好）
	intentRules := []struct {
		intent string
		words  []string
	}{
		{"pricing", []string{"多少钱", "价格", "费用", "计费", "免费吗", "多少钱一年", "报价"}},
		{"comparison", []string{"对比", "区别", "哪个好", "vs", "和.*比"}},
		{"recommendation", []string{"推荐", "排行", "TOP", "排名", "选型指南"}},
		{"problem", []string{"失败", "问题", "痛点", "坑", "误区", "常见"}},
		{"how_to", []string{"如何", "怎么", "怎样", "方法", "步骤", "指南"}},
		{"case_study", []string{"案例", "实施", "落地", "实践", "成功"}},
	}
	for _, kw := range keywords {
		assigned := "recommendation" // 默认
		for _, rule := range intentRules {
			for _, w := range rule.words {
				if strings.Contains(kw, w) {
					assigned = rule.intent
					break
				}
			}
			if assigned != "recommendation" {
				break
			}
		}
		result[kw] = assigned
	}
	return result, nil
}

// SaveMiningResults 将 suggest/longtail 挖掘结果落库（补全链路断点：挖掘→持久化→站位）
//
//  1. 种子词确保存在 Layer='seed' 行（站位的锚点）；
//  2. 结果词按 ParentKeyword 回填 ParentID，形成 seed→suggest/longtail 派生树；
//  3. QueryIntent 缺失时用启发式 ClassifyIntent 补全；
//  4. 库中已存在的词合并引擎覆盖（SuggestEngines/SuggestCount），不重复插入。
//
// 返回实际插入+更新的词数。
func (s *KeywordMiningService) SaveMiningResults(ctx context.Context, seeds []string, results []*model.GeoKeyword) (int, error) {
	if len(results) == 0 {
		return 0, nil
	}
	now := time.Now()

	texts := make([]string, 0, len(results)+len(seeds)*2)
	for _, seed := range seeds {
		seed = strings.TrimSpace(seed)
		if seed != "" {
			texts = append(texts, seed)
		}
	}
	for _, r := range results {
		texts = append(texts, r.Keyword)
		if r.ParentKeyword != "" {
			texts = append(texts, r.ParentKeyword)
		}
	}

	var existing []model.GeoKeyword
	if err := s.db.Model(&model.GeoKeyword{}).
		Where("keyword IN ?", texts).
		Find(&existing).Error; err != nil {
		return 0, err
	}
	byText := make(map[string]*model.GeoKeyword, len(existing))
	for i := range existing {
		byText[existing[i].Keyword] = &existing[i]
	}

	// 1. 种子词站位：不存在则创建 Layer='seed' 行；已存在则刷新 LastMinedAt
	seedRows := make([]*model.GeoKeyword, 0, len(seeds))
	seenSeed := map[string]bool{}
	staleSeedIDs := make([]string, 0, len(seeds))
	for _, seed := range seeds {
		seed = strings.TrimSpace(seed)
		if seed == "" || seenSeed[seed] {
			continue
		}
		seenSeed[seed] = true
		if ex, ok := byText[seed]; ok {
			if ex.Layer == "" || ex.Layer == "seed" {
				staleSeedIDs = append(staleSeedIDs, ex.ID)
			}
			continue
		}
		seedRows = append(seedRows, &model.GeoKeyword{
			Keyword:     seed,
			Category:    "seed",
			Source:      "manual_seed",
			Layer:       "seed",
			Status:      "active",
			LastMinedAt: &now,
		})
	}
	if len(seedRows) > 0 {
		if err := s.kwRepo.BatchCreate(seedRows); err != nil {
			return 0, err
		}
		for _, row := range seedRows {
			byText[row.Keyword] = row
		}
	}
	if len(staleSeedIDs) > 0 {
		if err := s.db.Model(&model.GeoKeyword{}).Where("id IN ?", staleSeedIDs).
			Updates(map[string]any{"last_mined_at": now, "updated_at": now}).Error; err != nil {
			return 0, err
		}
	}

	// 2. 意图补全（零成本启发式）
	uncategorized := make([]string, 0, len(results))
	for _, r := range results {
		if r.QueryIntent == "" {
			uncategorized = append(uncategorized, r.Keyword)
		}
	}
	if len(uncategorized) > 0 {
		if intents, err := s.ClassifyIntent(ctx, uncategorized); err == nil {
			for _, r := range results {
				if r.QueryIntent == "" {
					if intent, ok := intents[r.Keyword]; ok {
						r.QueryIntent = intent
						r.Intent = intent
					}
				}
			}
		}
	}

	// 3. 站位（ParentID 回填）+ 分类：新词插入 / 已有词合并更新
	toCreate := make([]*model.GeoKeyword, 0, len(results))
	toSave := make([]*model.GeoKeyword, 0, len(results))
	processed := map[string]bool{}
	for _, r := range results {
		if r.Keyword == "" || processed[r.Keyword] {
			continue
		}
		processed[r.Keyword] = true

		if r.ParentID == "" && r.ParentKeyword != "" {
			if parent, ok := byText[r.ParentKeyword]; ok {
				r.ParentID = parent.ID
			}
		}
		r.LastMinedAt = &now

		ex, ok := byText[r.Keyword]
		if !ok {
			toCreate = append(toCreate, r)
			continue
		}
		mergeMiningResult(ex, r, now)
		toSave = append(toSave, ex)
	}

	saved := 0
	if len(toCreate) > 0 {
		if err := s.kwRepo.BatchCreate(toCreate); err != nil {
			return saved, err
		}
		saved += len(toCreate)
	}
	for _, ex := range toSave {
		if err := s.db.Model(&model.GeoKeyword{}).Where("id = ?", ex.ID).Updates(map[string]any{
			"layer":           ex.Layer,
			"parent_id":       ex.ParentID,
			"parent_keyword":  ex.ParentKeyword,
			"query_intent":    ex.QueryIntent,
			"intent":          ex.Intent,
			"funnel_stage":    ex.FunnelStage,
			"suggest_engines": ex.SuggestEngines,
			"suggest_count":   ex.SuggestCount,
			"last_mined_at":   ex.LastMinedAt,
			"updated_at":      now,
		}).Error; err != nil {
			return saved, err
		}
		saved++
	}
	return saved, nil
}

// mergeMiningResult 将新挖掘行 r 的信息合并进库中已有行 ex
func mergeMiningResult(ex, r *model.GeoKeyword, now time.Time) {
	if r.Layer == "suggest" {
		var engs []string
		_ = json.Unmarshal([]byte(ex.SuggestEngines), &engs)
		have := map[string]bool{}
		for _, e := range engs {
			have[e] = true
		}
		var newEngs []string
		_ = json.Unmarshal([]byte(r.SuggestEngines), &newEngs)
		added := 0
		for _, e := range newEngs {
			if !have[e] {
				engs = append(engs, e)
				have[e] = true
				added++
			}
		}
		if added > 0 {
			b, _ := json.Marshal(engs)
			ex.SuggestEngines = string(b)
			ex.SuggestCount += added
		}
	}
	if ex.ParentID == "" {
		ex.ParentID = r.ParentID
		ex.ParentKeyword = r.ParentKeyword
	}
	if ex.QueryIntent == "" && r.QueryIntent != "" {
		ex.QueryIntent = r.QueryIntent
		ex.Intent = r.Intent
	}
	if ex.Layer == "" && r.Layer != "" {
		ex.Layer = r.Layer
	}
	if ex.FunnelStage == "" {
		ex.FunnelStage = r.FunnelStage
	}
	ex.LastMinedAt = &now
}

// KeywordFunnel 漏斗统计
type KeywordFunnel struct {
	SeedCount      int            `json:"seed_count"`
	RelatedCount   int            `json:"related_count"`
	SuggestCount   int            `json:"suggest_count"`
	LongtailCount  int            `json:"longtail_count"`
	Total          int            `json:"total"`
	Intents        map[string]int `json:"intents"`
	FunnelStages   map[string]int `json:"funnel_stages"`
	SuggestEngines map[string]int `json:"suggest_engines"`
}

// BuildFunnel 从 DB 构建 4 层漏斗统计
func (s *KeywordMiningService) BuildFunnel(ctx context.Context) (*KeywordFunnel, error) {
	f := &KeywordFunnel{
		Intents:        map[string]int{},
		FunnelStages:   map[string]int{},
		SuggestEngines: map[string]int{},
	}

	// 按 layer 分组
	type row struct {
		Layer string `gorm:"column:layer"`
		Cnt   int64  `gorm:"column:cnt"`
	}
	var rows []row
	if err := s.db.Model(&model.GeoKeyword{}).
		Select("COALESCE(layer,'seed') as layer, COUNT(*) as cnt").
		Where("status = ?", "active").
		Group("COALESCE(layer,'seed')").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		switch r.Layer {
		case "seed":
			f.SeedCount = int(r.Cnt)
		case "related":
			f.RelatedCount = int(r.Cnt)
		case "suggest":
			f.SuggestCount = int(r.Cnt)
		case "longtail":
			f.LongtailCount = int(r.Cnt)
		}
		f.Total += int(r.Cnt)
	}

	// 按 query_intent 分组
	var irows []row
	if err := s.db.Model(&model.GeoKeyword{}).
		Select("COALESCE(query_intent,'unknown') as layer, COUNT(*) as cnt").
		Where("status = ?", "active").
		Group("COALESCE(query_intent,'unknown')").
		Scan(&irows).Error; err != nil {
		return nil, err
	}
	for _, r := range irows {
		f.Intents[r.Layer] = int(r.Cnt)
	}

	// 按 funnel_stage 分组
	var srows []row
	if err := s.db.Model(&model.GeoKeyword{}).
		Select("COALESCE(funnel_stage,'unknown') as layer, COUNT(*) as cnt").
		Where("status = ?", "active").
		Group("COALESCE(funnel_stage,'unknown')").
		Scan(&srows).Error; err != nil {
		return nil, err
	}
	for _, r := range srows {
		f.FunnelStages[r.Layer] = int(r.Cnt)
	}

	// 按引擎聚合 suggest 层覆盖（suggest_engines 为 JSON 数组字符串）
	var engRows []struct {
		Engines string `gorm:"column:engines"`
	}
	if err := s.db.Model(&model.GeoKeyword{}).
		Select("suggest_engines as engines").
		Where("status = ? AND layer = ? AND suggest_engines <> ''", "active", "suggest").
		Scan(&engRows).Error; err != nil {
		return nil, err
	}
	for _, er := range engRows {
		var engs []string
		if err := json.Unmarshal([]byte(er.Engines), &engs); err != nil {
			continue
		}
		seen := map[string]bool{}
		for _, e := range engs {
			if e == "" || seen[e] {
				continue
			}
			seen[e] = true
			f.SuggestEngines[e]++
		}
	}

	return f, nil
}
