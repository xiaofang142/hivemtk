package service

import (
	"strings"
	"unicode"
)

type SentimentResult struct {
	Positive int
	Negative int
	Neutral  int
	Label    string
}

var positiveWords = map[string]struct{}{
	"好": {}, "好的": {}, "不错": {}, "满意": {}, "喜欢": {}, "爱": {}, "棒": {}, "精彩": {},
	"优秀": {}, "完美": {}, "支持": {}, "赞": {}, "牛": {}, "顶": {}, "推荐": {},
	"实惠": {}, "划算": {}, "优惠": {}, "便宜": {}, "值得": {}, "超值": {},
	"快": {}, "迅速": {}, "高效": {}, "专业": {}, "靠谱": {}, "放心": {}, "安全": {},
	"热情": {}, "耐心": {}, "周到": {}, "贴心": {}, "感动": {}, "感谢": {}, "谢谢": {}, "感激": {},
	"成功": {}, "赢": {}, "赚钱": {}, "盈利": {}, "增长": {}, "提升": {}, "进步": {}, "发展": {},
	"强大": {}, "领先": {}, "创新": {}, "独特": {}, "好用": {}, "易用": {}, "简单": {}, "方便": {},
	"流畅": {}, "稳定": {}, "可靠": {}, "性能": {}, "性价比": {}, "物超所值": {}, "惊喜": {},
	"开心": {}, "高兴": {}, "愉快": {}, "欣慰": {}, "安心": {}, "舒心": {},
	"实在": {}, "真诚": {}, "友善": {}, "亲切": {}, "和气": {}, "温暖": {}, "舒服": {},
	"实用": {}, "耐用": {}, "高质": {}, "优质": {}, "高级": {}, "精美": {}, "漂亮": {},
	"清楚": {}, "清晰": {}, "详细": {}, "准确": {}, "到位": {}, "杰出": {},
	"positive": {}, "great": {}, "good": {}, "excellent": {}, "amazing": {}, "wonderful": {},
	"love": {}, "happy": {}, "best": {}, "perfect": {}, "nice": {}, "cool": {}, "awesome": {},
	"thanks": {}, "thank you": {}, "recommend": {}, "helpful": {}, "supportive": {}, "fast": {},
	"professional": {}, "reliable": {}, "easy": {}, "simple": {}, "smooth": {}, "stable": {},
	"win": {}, "success": {}, "growth": {}, "improve": {}, "better": {}, "strong": {}, "powerful": {},
}

var negativeWords = map[string]struct{}{
	"差": {}, "不好": {}, "糟糕": {}, "失望": {}, "讨厌": {}, "恨": {}, "垃圾": {}, "烂": {},
	"难用": {}, "卡": {}, "慢": {}, "贵": {}, "太贵": {}, "坑": {}, "骗": {}, "骗子": {},
	"假货": {}, "劣质": {}, "假冒": {}, "上当": {}, "虚假": {}, "夸大": {}, "后悔": {},
	"退货": {}, "退款": {}, "投诉": {}, "差评": {}, "举报": {}, "客服差": {}, "不理人": {},
	"很慢": {}, "超级慢": {}, "排队": {}, "等待": {}, "敷衍": {}, "推诿": {},
	"生气": {}, "愤怒": {}, "气人": {}, "烦": {}, "闹心": {}, "恶心": {}, "郁闷": {}, "憋屈": {},
	"不满": {}, "不开心": {}, "不愉快": {}, "不爽": {}, "窝火": {}, "恼火": {}, "火大": {}, "无语": {},
	"次": {}, "废": {}, "坏": {}, "坏了": {}, "故障": {}, "问题": {},
	"bug": {}, "错误": {}, "失败": {}, "崩溃": {}, "丢失": {}, "泄露": {},
	"无效": {}, "没用": {}, "白费": {}, "浪费": {}, "坑钱": {}, "宰客": {}, "乱收费": {},
	"negative": {}, "bad": {}, "worst": {}, "terrible": {}, "awful": {}, "horrible": {},
	"hate": {}, "angry": {}, "sad": {}, "disappointed": {}, "frustrated": {}, "annoyed": {},
	"slow": {}, "expensive": {}, "broken": {}, "buggy": {}, "useless": {}, "waste": {},
	"fail": {}, "failed": {}, "error": {}, "crash": {}, "lose": {}, "problem": {},
	"complaint": {}, "refund": {}, "return": {}, "fake": {}, "scam": {}, "cheat": {},
	"rude": {}, "ignored": {}, "delay": {},
	"难": {}, "复杂": {}, "麻烦": {}, "繁琐": {}, "混乱": {}, "脏": {},
	"绝望": {}, "打击": {}, "困难": {}, "棘手": {}, "痛": {},
}

func tokenize(text string) []string {
	text = strings.ToLower(strings.TrimSpace(text))
	if text == "" {
		return nil
	}
	var tokens []string
	var sb strings.Builder
	flush := func() {
		if sb.Len() > 0 {
			tokens = append(tokens, sb.String())
			sb.Reset()
		}
	}
	for _, r := range text {
		if unicode.Is(unicode.Han, r) {
			flush()
			tokens = append(tokens, string(r))
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			sb.WriteRune(r)
			continue
		}
		flush()
	}
	flush()
	var merged []string
	mergeWindows := []int{2, 3}
	hasHan := strings.ContainsFunc(text, func(r rune) bool { return unicode.Is(unicode.Han, r) })
	if hasHan {
		for _, w := range mergeWindows {
			for i := 0; i+w <= len(tokens); i++ {
				merged = append(merged, strings.Join(tokens[i:i+w], ""))
			}
		}
	}
	tokens = append(tokens, merged...)
	return tokens
}
