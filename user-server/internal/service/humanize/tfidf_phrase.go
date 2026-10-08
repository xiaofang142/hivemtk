package humanize

import (
	"math"
	"sort"
	"unicode"
)

// PhraseType 短语类型
type PhraseType string

const (
	PhraseTypeAction       PhraseType = "action"
	PhraseTypeEmpathy      PhraseType = "empathy"
	PhraseTypeProfessional PhraseType = "professional"
	PhraseTypePersuasion   PhraseType = "persuasion"
	PhraseTypeGeneral      PhraseType = "general"
)

// TFIDFPhraseExtractor TF-IDF 短语提取器
type TFIDFPhraseExtractor struct {
	stopWords map[string]bool
}

// NewTFIDFPhraseExtractor 构造
func NewTFIDFPhraseExtractor() *TFIDFPhraseExtractor {
	return &TFIDFPhraseExtractor{
		stopWords: map[string]bool{
			"的": true, "了": true, "是": true, "在": true, "我": true, "你": true,
			"他": true, "她": true, "这": true, "那": true, "和": true, "与": true,
			"也": true, "都": true, "就": true, "而": true, "及": true, "或": true,
			"啊": true, "呢": true, "啦": true, "呀": true, "嘛": true, "哦": true,
			"对": true, "嗯": true, "哈": true, "嘿": true,
		},
	}
}

// TFIDFPhrase TF-IDF 短语结果
type TFIDFPhrase struct {
	Phrase     string
	TFIDFScore float64
	TF         int
	DF         int
	PhraseType PhraseType
	Rank       int
}

// Extract 从对话集合中提取 top-N TF-IDF 短语
//
// 流程：
//  1. 对每条对话用 2-4 字滑窗分词
//  2. 计算 TF（词频）和 DF（文档频率）
//  3. TF-IDF = TF * log(N / DF)
//  4. 按短语类型分类
//  5. 按 TF-IDF 降序取 top-N
func (e *TFIDFPhraseExtractor) Extract(messages []ChampionMessage, topN int) []TFIDFPhrase {
	if len(messages) == 0 || topN <= 0 {
		return nil
	}
	N := float64(len(messages))
	tfMap := make(map[string]int)
	dfMap := make(map[string]int)
	phraseTypeMap := make(map[string]PhraseType)
	for _, msg := range messages {
		phrases := e.extractPhrases(msg.Content)
		seenInDoc := make(map[string]bool)
		for _, p := range phrases {
			tfMap[p]++
			if !seenInDoc[p] {
				dfMap[p]++
				seenInDoc[p] = true
			}
			phraseTypeMap[p] = e.classifyPhrase(p)
		}
	}

	type scored struct {
		phrase string
		score  float64
		tf     int
		df     int
		ptype  PhraseType
	}
	var all []scored
	for phrase, tf := range tfMap {
		df := dfMap[phrase]
		if df == 0 {
			continue
		}
		idf := math.Log(N / float64(df))
		score := float64(tf) * idf
		if score <= 0 {
			continue
		}
		all = append(all, scored{phrase, score, tf, df, phraseTypeMap[phrase]})
	}
	sort.Slice(all, func(i, j int) bool {
		// all 由 tfMap(map) 摊出，只比 score 时同分词组先后随 map 迭代序变；
		// 下面还要截 topN ⇒ 选出的强调词组本身会漂，必须用词组名升序把顺序钉死。
		if all[i].score != all[j].score {
			return all[i].score > all[j].score
		}
		return all[i].phrase < all[j].phrase
	})
	if len(all) > topN {
		all = all[:topN]
	}
	out := make([]TFIDFPhrase, 0, len(all))
	for i, s := range all {
		out = append(out, TFIDFPhrase{
			Phrase:     s.phrase,
			TFIDFScore: math.Round(s.score*100000) / 100000,
			TF:         s.tf,
			DF:         s.df,
			PhraseType: s.ptype,
			Rank:       i + 1,
		})
	}
	return out
}

func (e *TFIDFPhraseExtractor) extractPhrases(text string) []string {
	runes := []rune(text)
	if len(runes) < 2 {
		return nil
	}
	var out []string
	seen := make(map[string]bool)
	for size := 2; size <= 4; size++ {
		for i := 0; i+size <= len(runes); i++ {
			phrase := string(runes[i : i+size])
			if e.stopWords[string(runes[i])] || e.stopWords[string(runes[i+size-1])] {
				continue
			}
			if containsPunct(phrase) {
				continue
			}
			if !seen[phrase] {
				seen[phrase] = true
				out = append(out, phrase)
			}
		}
	}
	return out
}

func (e *TFIDFPhraseExtractor) classifyPhrase(phrase string) PhraseType {
	actionWords := []string{"下单", "拍下", "入手", "试试", "咨询", "联系", "回复"}
	empathyWords := []string{"理解", "抱歉", "恭喜", "放心", "明白", "感谢"}
	profWords := []string{"成分", "肤质", "保湿", "参数", "续航", "面料", "版型"}
	persuasWords := []string{"优惠", "折扣", "活动", "省", "划算", "限时", "包邮"}
	if containsAny(phrase, actionWords) {
		return PhraseTypeAction
	}
	if containsAny(phrase, empathyWords) {
		return PhraseTypeEmpathy
	}
	if containsAny(phrase, profWords) {
		return PhraseTypeProfessional
	}
	if containsAny(phrase, persuasWords) {
		return PhraseTypePersuasion
	}
	return PhraseTypeGeneral
}

func containsPunct(s string) bool {
	for _, r := range s {
		if r == '。' || r == '！' || r == '？' || r == '.' || r == '!' || r == '?' ||
			r == ',' || r == '，' || r == '；' || r == ';' || r == '：' || r == ':' ||
			r == '"' || r == '\'' ||
			unicode.IsSpace(r) {
			return true
		}
	}
	return false
}

func containsAny(s string, words []string) bool {
	for _, w := range words {
		if len(w) > 0 && containsSubstring(s, w) {
			return true
		}
	}
	return false
}

func containsSubstring(s, sub string) bool {
	if len(sub) == 0 {
		return false
	}
	sRunes := []rune(s)
	subRunes := []rune(sub)
	if len(subRunes) > len(sRunes) {
		return false
	}
	for i := 0; i+len(subRunes) <= len(sRunes); i++ {
		match := true
		for j := range subRunes {
			if sRunes[i+j] != subRunes[j] {
				match = false
				break
			}
		}
		if match {
			return true
		}
	}
	return false
}
