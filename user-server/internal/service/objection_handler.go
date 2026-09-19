package service

import (
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"log/slog"
	"math/rand"
	"sort"
	"strings"
	"sync"

	"hivemtk-user/internal/aiagent/llm"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/utils"
	"hivemtk-user/internal/repository"
)

// ScriptLibraryRepo 话术库仓储接口（供测试注入 mock）
type ScriptLibraryRepo interface {
	ListObjectionTemplates(ctx context.Context, objectionCategory string, limit int) ([]model.ScriptLibrary, error)
	IncrementUsageStats(ctx context.Context, templateID uint, success bool) error
}

type objectionDispatcher interface {
	Dispatch(ctx context.Context, req llm.DispatchRequest) (*llm.DispatchResult, error)
}

type ObjectionHandlerService struct {
	scriptRepo ScriptLibraryRepo

	dispatcher objectionDispatcher
}

// SetDispatcher 注入 LLM 兜底（路由装配点调用；nil 不影响既有行为）
func (s *ObjectionHandlerService) SetDispatcher(d objectionDispatcher) {
	s.dispatcher = d
}

// NewObjectionHandlerService 创建服务
// D10: 直接接入全局 dispatcher（审核修正①——装配点零改动；全局未初始化时兜底自动关闭）
func NewObjectionHandlerService() *ObjectionHandlerService {
	s := &ObjectionHandlerService{scriptRepo: repository.NewScriptLibraryRepository(repository.GetDB())}
	if d := llm.GetGlobalDispatcher(); d != nil {
		s.dispatcher = d
	}
	return s
}

// ObjectionCategory 异议类别
type ObjectionCategory string

const (
	ObjectionPrice     ObjectionCategory = "price"
	ObjectionNeed      ObjectionCategory = "need"
	ObjectionTrust     ObjectionCategory = "trust"
	ObjectionTiming    ObjectionCategory = "timing"
	ObjectionStatusQuo ObjectionCategory = "status_quo"
	ObjectionCompare   ObjectionCategory = "compare"
	ObjectionFeature   ObjectionCategory = "feature"
	ObjectionOther     ObjectionCategory = "other"
)

// ObjectionTemplate 异议处理模板
type ObjectionTemplate struct {
	ID          uint              `json:"id"`
	Category    ObjectionCategory `json:"category"`
	Keywords    []string          `json:"keywords"`
	Title       string            `json:"title"`
	Content     string            `json:"content"`
	UsageCount  int               `json:"usage_count"`
	SuccessRate float64           `json:"success_rate"`
}

var angerWords = []string{
	"生气", "愤怒", "气死", "气死我", "发火", "火大", "恼火", "大怒", "暴怒",
	"讨厌", "烦死", "烦", "闹心", "恶心", "郁闷", "憋屈", "窝火", "不爽",
	"骗", "骗子", "欺诈", "坑", "坑人", "宰", "宰客", "耍", "耍我", "耍人",
	"垃圾", "烂", "废物", "废", "混蛋", "白痴", "无语", "离谱", "荒唐",
	"投诉", "举报", "差评", "曝光", "告你", "报警", "打官司", "维权",
	"angry", "furious", "mad", "hate", "annoyed", "frustrated", "stupid",
	"混蛋啊", "搞什么", "什么东西", "什么鬼", "有病", "你有病",
	"太坑", "太烂", "太差", "太离谱", "太过分", "太过分", "再也不",
}

func detectAnger(text string) (anger bool, confidence float64) {
	t := strings.ToLower(text)
	hits := 0
	for _, w := range angerWords {
		if strings.Contains(t, strings.ToLower(w)) {
			hits++
		}
	}
	if hits == 0 {
		return false, 0
	}
	confidence = float64(hits) / 3.0
	if confidence > 1 {
		confidence = 1
	}
	return hits >= 2, confidence
}

var objectionRules = []struct {
	Keywords []string
	Category ObjectionCategory
	Name     string
}{
	{Keywords: []string{"再想想", "暂时不用", "目前挺好", "挺好的", "不需要了", "用不着", "维持现状", "就用现在的"}, Category: ObjectionStatusQuo, Name: "维持现状异议"},
	{Keywords: []string{"贵", "太贵", "便宜点", "折扣", "降价", "优惠", "price", "expensive"}, Category: ObjectionPrice, Name: "价格异议"},
	{Keywords: []string{"不需要", "没需求", "已经有了", "用不上", "don't need"}, Category: ObjectionNeed, Name: "需求异议"},
	{Keywords: []string{"骗子", "假的", "骗人", "不靠谱", "安全", "trust", "骗"}, Category: ObjectionTrust, Name: "信任异议"},
	{Keywords: []string{"考虑一下", "下次", "以后", "再看看", "later", "think"}, Category: ObjectionTiming, Name: "时机异议"},
	{Keywords: []string{"其他家", "别家", "对比", "其他品牌", "competitor", "compare"}, Category: ObjectionCompare, Name: "比较异议"},
	{Keywords: []string{"功能", "不支持", "做不到", "没有这个", "feature", "can't"}, Category: ObjectionFeature, Name: "特性异议"},
}

const (
	confidenceMultiHit  = 0.90
	confidenceSingleHit = 0.70
	confidenceFallback  = 0.40
)

// Classify 异议分类
func (s *ObjectionHandlerService) Classify(ctx context.Context, text string) (ObjectionCategory, string) {
	category, name, _ := s.classifyWithConfidence(ctx, text)
	return category, name
}

func (s *ObjectionHandlerService) classifyWithConfidence(ctx context.Context, text string) (ObjectionCategory, string, float64) {
	t := strings.ToLower(text)
	for _, rule := range objectionRules {
		hits := 0
		for _, kw := range rule.Keywords {
			if strings.Contains(t, strings.ToLower(kw)) {
				hits++
			}
		}
		if hits > 0 {
			conf := confidenceSingleHit
			if hits >= 2 {
				conf = confidenceMultiHit
			}
			return rule.Category, rule.Name, conf
		}
	}
	return ObjectionOther, "其他异议", confidenceFallback
}

// HandleRequest 异议处理请求
type HandleRequest struct {
	Text           string `json:"text"`
	Category       string `json:"category"`
	OneID          string `json:"one_id"`
	CustomerID     string `json:"customer_id"`
	ConversationID string `json:"conversation_id"`
	TraceID        string `json:"trace_id"`
}

// HandleResponse 异议处理响应
type HandleResponse struct {
	Category     ObjectionCategory   `json:"category"`
	CategoryName string              `json:"category_name"`
	Confidence   float64             `json:"confidence"`
	Template     *ObjectionTemplate  `json:"template,omitempty"`
	Templates    []ObjectionTemplate `json:"templates"`
	Suggestion   string              `json:"suggestion"`
	Acknowledge  string              `json:"acknowledge,omitempty"`
	Clarify      string              `json:"clarify_question,omitempty"`
	// D10: LLM 兜底补充字段。IsGenuine 用 *bool（nil=未评估，false=拒绝伪装）；
	// LLMCategory 保留 LLM 原始分类（is_genuine=false 归 Other 后供运营分析）。
	IsGenuine   *bool             `json:"is_genuine,omitempty"`
	LLMCategory ObjectionCategory `json:"llm_category,omitempty"`
	Source      string            `json:"source,omitempty"`
}

// Handle 处理异议
func (s *ObjectionHandlerService) Handle(ctx context.Context, req HandleRequest) (*HandleResponse, error) {
	category, name, confidence := s.classifyWithConfidence(ctx, req.Text)

	var isGenuine *bool
	var llmCategory ObjectionCategory
	source := "rule"
	if s.dispatcher != nil && confidence == confidenceSingleHit {
		if cat2, conf2, genuine, ok := s.classifyByLLM(ctx, req.Text); ok {
			isGenuine = &genuine
			llmCategory = cat2
			if genuine {
				category, name, confidence = cat2, s.categoryName(cat2), conf2
				source = "llm"
			} else {

				category, name, confidence = ObjectionOther, "其他异议", confidenceFallback
				source = "llm"
			}
		}
	}
	resp := &HandleResponse{
		Category:     category,
		CategoryName: name,
		Confidence:   confidence,
		Templates:    make([]ObjectionTemplate, 0),
		IsGenuine:    isGenuine,
		LLMCategory:  llmCategory,
		Source:       source,
	}

	if req.Category == "" {
		req.Category = string(category)
	}

	var scripts []model.ScriptLibrary
	if s.scriptRepo != nil {
		scripts, _ = s.scriptRepo.ListObjectionTemplates(ctx, req.Category, 5)
	}

	for _, sc := range scripts {
		ot := ObjectionTemplate{
			ID:          sc.ID,
			Category:    ObjectionCategory(sc.Category),
			Title:       sc.Title,
			Content:     sc.Content,
			UsageCount:  sc.UsageCount,
			SuccessRate: sc.ConversionRate,
		}
		if sc.Tags != nil {
			kws := make([]string, len(sc.Tags))
			for i, t := range sc.Tags {
				if s, ok := t.(string); ok {
					kws[i] = s
				} else {
					kws[i] = ""
				}
			}
			ot.Keywords = kws
		}
		resp.Templates = append(resp.Templates, ot)
	}

	if len(resp.Templates) > 0 {
		resp.Template = &resp.Templates[0]

		var exposure *scriptExposure
		if req.OneID != "" {
			exposure = &scriptExposure{
				version:        reqVersionOf(scripts, resp.Template.ID),
				oneID:          req.OneID,
				customerID:     req.CustomerID,
				conversationID: req.ConversationID,
				traceID:        req.TraceID,
			}
		}
		s.recordUsageAsync(ctx, resp.Template.ID, exposure)
	}

	if len(resp.Templates) == 0 {
		resp.Suggestion = s.defaultSuggestion(ctx, category, req.Text)
	}

	resp.Acknowledge = pickAcknowledge(category, ackSeedID(resp.Template), req.Text)
	if q, ok := exploreClarifyQuestions[category]; ok {
		resp.Clarify = q
	}

	sort.Slice(resp.Templates, func(i, j int) bool {
		return resp.Templates[i].UsageCount > resp.Templates[j].UsageCount
	})

	if angry, conf := detectAnger(req.Text); angry && conf >= 0.6 {
		apology := "非常抱歉给您带来了不愉快的体验，"
		if resp.Suggestion != "" {
			resp.Suggestion = apology + resp.Suggestion
		}
		if resp.Acknowledge == "" {
			resp.Acknowledge = apology
		} else {
			resp.Acknowledge = apology + resp.Acknowledge
		}
	}

	return resp, nil
}

func (s *ObjectionHandlerService) defaultSuggestion(ctx context.Context, cat ObjectionCategory, text string) string {
	switch cat {
	case ObjectionPrice:
		return "理解客户对价格的关注，先肯定产品价值再谈价格。强调性价比、长期收益、对比其他方案的 TCO。可提供分期方案或试用。"
	case ObjectionNeed:
		return "通过案例和数据说明产品在类似场景的解决效果。引导客户描述痛点，深入了解需求。"
	case ObjectionTrust:
		return "提供资质证书、用户案例、品牌背书、第三方评测。先建立信任再谈产品。"
	case ObjectionTiming:
		return "不要强推，先记录需求。设置 7 天跟进，提供限时优惠引导立即行动。"
	case ObjectionStatusQuo:
		return "不要急于说服，先认同现状的合理性，再挖掘现状中的隐性成本与不满点。提供低门槛试用或小步替换方案，避免正面否定客户当前选择。"
	case ObjectionCompare:
		return "了解客户对比的具体产品，从差异化优势切入，不要贬低竞品。"
	case ObjectionFeature:
		return "深入了解客户的实际使用场景。如确实不支持，可推荐相近功能或定制方案。"
	default:
		return "倾听客户异议背后的真实顾虑，不要急于反驳，先共情再解释。"
	}
}

// ListCategories 列出所有类别
func (s *ObjectionHandlerService) ListCategories(ctx context.Context) []map[string]string {
	out := make([]map[string]string, 0, len(objectionRules))
	for _, r := range objectionRules {
		out = append(out, map[string]string{
			"category": string(r.Category),
			"name":     r.Name,
		})
	}
	return out
}

// RecordUsage 记录使用（学习闭环）
func (s *ObjectionHandlerService) RecordUsage(ctx context.Context, templateID uint, success bool) error {
	if s.scriptRepo == nil {
		return nil
	}
	return s.scriptRepo.IncrementUsageStats(ctx, templateID, success)
}

var scriptABOnce sync.Once
var scriptABSvc *ScriptABService

func getScriptABService() *ScriptABService {
	scriptABOnce.Do(func() {
		repo := repository.NewScriptLibraryRepository(repository.GetDB())
		if repo == nil {
			return
		}
		scriptABSvc = NewScriptABService(repo)
	})
	return scriptABSvc
}

// RecordScriptExposure T-7 曝光记录入口：fire-and-forget，绝不影响主链路
//
// version/oneID/customerID/conversationID/traceID 任一关键位缺失时静默跳过。
func RecordScriptExposure(scriptID uint, version int, oneID string, customerID string, conversationID, traceID string) {
	svc := getScriptABService()
	if svc == nil || scriptID == 0 || oneID == "" {
		return
	}
	go func() {
		defer func() {
			if r := recover(); r != nil {
				slog.Warn("[ScriptAB] 曝光记录 panic recovered", "script_id", scriptID, "panic", r)
			}
		}()
		svc.RecordExposure(scriptID, version, oneID, customerID, conversationID, traceID)
	}()
}

func (s *ObjectionHandlerService) recordUsageAsync(ctx context.Context, templateID uint, exposure *scriptExposure) {
	if s == nil || s.scriptRepo == nil || templateID == 0 {
		return
	}
	detached := context.WithoutCancel(ctx)
	go func() {
		if err := s.scriptRepo.IncrementUsageStats(detached, templateID, false); err != nil {
			slog.Warn("objection handler auto record usage failed", "template_id", templateID, "error", err)
		}
	}()
	if exposure != nil {
		RecordScriptExposure(templateID, exposure.version, exposure.oneID, exposure.customerID, exposure.conversationID, exposure.traceID)
	}
}

type scriptExposure struct {
	version        int
	oneID          string
	customerID     string
	conversationID string
	traceID        string
}

func reqVersionOf(scripts []model.ScriptLibrary, templateID uint) int {
	for _, sc := range scripts {
		if sc.ID == templateID {
			if sc.Version > 0 {
				return sc.Version
			}
			return fallbackVersionOf(templateID)
		}
	}
	return fallbackVersionOf(templateID)
}

func fallbackVersionOf(templateID uint) int {
	if templateID == 0 {
		return 1
	}
	versionCacheMu.RLock()
	if v, ok := versionCache[templateID]; ok && v > 0 {
		versionCacheMu.RUnlock()
		return v
	}
	versionCacheMu.RUnlock()

	// 句柄必须在 spawning 之前解析：repository.GetDB() 读的是 pkg/db 的包级全局，
	// 放进下面的异步体就会与别的 goroutine（测试里 100 处 db.SetTestDB，进程里任何一次
	// 重初始化）抢同一地址。同形状的 fire-and-forget 见 session_chain.go:TriggerCSATOnClose。
	handle := repository.GetDB()
	go func(id uint) {
		defer func() { _ = recover() }()
		repo := repository.NewScriptLibraryRepository(handle)
		if repo == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), utils.ShortTimeout)
		defer cancel()
		if v, err := repo.MaxScriptVersion(ctx, id); err == nil && v > 0 {
			versionCacheMu.Lock()
			if len(versionCache) > 1024 {
				versionCache = make(map[uint]int)
			}
			versionCache[id] = v
			versionCacheMu.Unlock()
		}
	}(templateID)
	return 1
}

var (
	versionCache   = make(map[uint]int)
	versionCacheMu sync.RWMutex
)

var acknowledgeTemplates = map[ObjectionCategory][]string{
	ObjectionPrice: {
		"价格确实是大家都会关心的，您有这个顾虑很正常。",
		"理解您对成本的谨慎，这钱要花得值才好。",
		"嗯，预算方面多考虑是对的，我说明白您再判断。",
	},
	ObjectionNeed: {
		"明白您的意思，觉得现在用不上也是实情。",
		"理解，需求这东西确实因人而异。",
		"您说暂时没这方面需要，我先把情况讲清楚。",
	},
	ObjectionTrust: {
		"信任需要慢慢建立，您谨慎是对的。",
		"您的担心可以理解，合作前多了解是应该的。",
		"换作是我也会先确认靠不靠谱。",
	},
	ObjectionTiming: {
		"好的，不着急，时机确实要自己把握。",
		"理解您想再等等，这个节奏没问题。",
		"行，先放一放也合理，我先把关键信息给您留底。",
	},
	ObjectionStatusQuo: {
		"现在的方式用得挺好就不想折腾，这很正常。",
		"理解，稳定跑着的东西谁都不想轻易换。",
		"嗯，现状能凑合就先不动，是很务实的想法。",
	},
	ObjectionCompare: {
		"多方对比再做决定是对的，您应该多看看。",
		"了解，货比三家不吃亏。",
		"您去比较很正常，我把我们的特点说清楚供您参考。",
	},
	ObjectionFeature: {
		"您提到的这点确实关键，功能匹配度最重要。",
		"明白，具体能不能做到您关心的事，我来确认下。",
		"这个顾虑实际，功能不合用买回来也是摆设。",
	},
}

var exploreClarifyQuestions = map[ObjectionCategory]string{
	ObjectionPrice: "方便问一下，您是觉得超出预算了，还是和同类产品对比后觉得偏高呢？",
	ObjectionTrust: "方便说说主要担心哪方面吗？比如效果、售后还是服务保障？",
}

func ackSeedID(tpl *ObjectionTemplate) uint {
	if tpl != nil && tpl.ID > 0 {
		return tpl.ID
	}
	return 0
}

func pickAcknowledge(cat ObjectionCategory, seedID uint, text string) string {
	tpls, ok := acknowledgeTemplates[cat]
	if !ok || len(tpls) == 0 {
		return ""
	}
	var seed int64
	if seedID > 0 {
		seed = int64(seedID)
	} else {
		h := fnv.New32a()
		_, _ = h.Write([]byte(text))
		seed = int64(h.Sum32())
	}
	idx := int(rand.New(rand.NewSource(seed)).Int63()) % len(tpls)
	return tpls[idx]
}

func (s *ObjectionHandlerService) categoryName(cat ObjectionCategory) string {
	for _, def := range objectionRules {
		if def.Category == cat {
			return def.Name
		}
	}
	return "其他异议"
}

func (s *ObjectionHandlerService) classifyByLLM(ctx context.Context, text string) (ObjectionCategory, float64, bool, bool) {
	catList := make([]string, 0, len(objectionRules))
	for _, r := range objectionRules {
		catList = append(catList, fmt.Sprintf("%s: %s", r.Category, r.Name))
	}
	prompt := fmt.Sprintf(`你是销售异议分类专家。判断客户消息是否为真实异议（而非拒绝/敷衍伪装），并归类。

【客户消息】: %s

【异议类别】:
%s
other: 无法归类

【输出要求】(严格 JSON):
{"category": "类别之一", "confidence": 0.0-1.0, "is_genuine": true/false}
is_genuine=false 表示"太贵了买不起"实为委婉拒绝等伪装场景。`, text, strings.Join(catList, "\n"))

	result, err := s.dispatcher.Dispatch(ctx, llm.DispatchRequest{
		Scenario:    llm.ScenarioIntentRecognize,
		Prompt:      prompt,
		JSONMode:    true,
		MaxTokens:   200,
		Temperature: 0,
	})
	if err != nil {
		return "", 0, false, false
	}
	var parsed struct {
		Category   string  `json:"category"`
		Confidence float64 `json:"confidence"`
		IsGenuine  bool    `json:"is_genuine"`
	}
	if err := json.Unmarshal([]byte(extractJSONFromStr(result.Content)), &parsed); err != nil {
		return "", 0, false, false
	}
	valid := false
	for _, r := range objectionRules {
		if ObjectionCategory(parsed.Category) == r.Category {
			valid = true
			break
		}
	}
	if parsed.Category == "other" {
		valid = true
	}
	if !valid || parsed.Confidence <= 0 || parsed.Confidence > 1 {
		return "", 0, false, false
	}
	return ObjectionCategory(parsed.Category), parsed.Confidence, parsed.IsGenuine, true
}
