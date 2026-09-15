package service

import (
	"context"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/pkg/testutil"

	"gorm.io/gorm"

	"hivemtk-user/internal/model"
)

func setupIntentTestDB(t *testing.T) *gorm.DB {
	// ⚠️ 只迁移 IntentRecord，**不得**把 &model.IntentLog{} 加进来。
	//
	// IntentLog 是 intent_records 的列映射视图（见 model/intent_log.go 顶部注释，
	// OPT-DB-10：intent_logs 已并入统一意图表 intent_records，source='fine_grained'），
	// 其列结构由迁移 v3.22.3 保证，明确「禁止加入 automigrate 清单」。
	//
	// 违反后果：两个结构体都声明 TableName()="intent_records"，AutoMigrate 先 DROP
	// 同一张表再按两个模型分别建列，最终表结构以 IntentLog 为准，丢掉
	// message_id / confidence_level / entities / sentiment / llm_model / cost_tokens / deleted_at，
	// 于是 IntentRecordRepository.Create 报
	//   column "message_id" of relation "intent_records" does not exist (SQLSTATE 42703)
	// 导致本包 11 个用例长期失败。详见 TASKS_AUDIT_2026-09-16.md · TEST-03。
	return testutil.NewTestDB(t,
		&model.IntentRecord{},
	)
}

func newIntentRecognizer(t *testing.T) (*IntentRecognizer, *gorm.DB) {
	db := setupIntentTestDB(t)
	return NewIntentRecognizer(db, nil, nil), db
}

func waitForIntentCount(t *testing.T, rec *IntentRecognizer, customerID string, want int, timeout time.Duration) []model.IntentRecord {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for {
		list, _ := rec.GetRecentIntents(context.Background(), customerID, 1000)
		if len(list) >= want {
			return list
		}
		if time.Now().After(deadline) {
			return list
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// 1. 完全匹配 - 询价
func TestRecognizeRule_PriceInquiryExact(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, err := rec.Recognize(context.Background(), "s-1", "u-1", "这个多少钱？")
	if err != nil {
		t.Fatal(err)
	}
	if r.IntentType != IntentPriceInquiry {
		t.Errorf("expected price_inquiry, got %s", r.IntentType)
	}
	if r.Method != "rule" {
		t.Errorf("expected rule, got %s", r.Method)
	}
}

// 2. 关键词匹配 - 询价
func TestRecognizeRule_PriceInquiryKeyword(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "你们价格怎么样")
	if r.IntentType != IntentPriceInquiry {
		t.Errorf("expected price_inquiry, got %s", r.IntentType)
	}
}

// 3. 关键词匹配 - 优惠
func TestRecognizeRule_PriceInquiryDiscount(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "有优惠吗")
	if r.IntentType != IntentPriceInquiry {
		t.Errorf("expected price_inquiry, got %s", r.IntentType)
	}
}

// 4. 关键词匹配 - 折扣
func TestRecognizeRule_PriceInquiryDiscount2(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "有什么折扣")
	if r.IntentType != IntentPriceInquiry {
		t.Errorf("expected price_inquiry, got %s", r.IntentType)
	}
}

// 5. 关键词匹配 - 报价
func TestRecognizeRule_PriceInquiryQuote(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "给我报价")
	if r.IntentType != IntentPriceInquiry {
		t.Errorf("expected price_inquiry, got %s", r.IntentType)
	}
}

// 6. 价格异议 - 太贵了
func TestRecognizeRule_PriceObjectionExpensive(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "这个东西太贵了")
	if r.IntentType != IntentObjectionPrice {
		t.Errorf("expected objection_price, got %s", r.IntentType)
	}
	if r.Sentiment != "negative" {
		t.Errorf("expected negative, got %s", r.Sentiment)
	}
}

// 7. 价格异议 - 价格高
func TestRecognizeRule_PriceObjectionHigh(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "价格有点高")
	if r.IntentType != IntentObjectionPrice {
		t.Errorf("expected objection_price, got %s", r.IntentType)
	}
}

// 8. 价格异议 - 不值
func TestRecognizeRule_PriceObjectionNotWorth(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "感觉不值这个价")
	if r.IntentType != IntentObjectionPrice {
		t.Errorf("expected objection_price, got %s", r.IntentType)
	}
}

// 9. 价格异议 - 不划算
func TestRecognizeRule_PriceObjectionNotCost(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "感觉不划算啊")
	if r.IntentType != IntentObjectionPrice {
		t.Errorf("expected objection_price, got %s", r.IntentType)
	}
}

// 10. 需求异议 - 不需要
func TestRecognizeRule_NeedObjectionNo(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "我暂时不需要")
	if r.IntentType != IntentObjectionNeed {
		t.Errorf("expected objection_need, got %s", r.IntentType)
	}
}

// 11. 需求异议 - 再考虑
func TestRecognizeRule_NeedObjectionConsider(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "我再考虑一下")
	if r.IntentType != IntentObjectionNeed {
		t.Errorf("expected objection_need, got %s", r.IntentType)
	}
}

// 12. 需求异议 - 看看
func TestRecognizeRule_NeedObjectionLook(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "我先看看吧")
	if r.IntentType != IntentObjectionNeed {
		t.Errorf("expected objection_need, got %s", r.IntentType)
	}
}

// 13. 信任异议 - 骗子
func TestRecognizeRule_TrustObjectionCheat(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "不会是骗子吧")
	if r.IntentType != IntentObjectionTrust {
		t.Errorf("expected objection_trust, got %s", r.IntentType)
	}
}

// 14. 信任异议 - 靠谱吗
func TestRecognizeRule_TrustObjectionReliable(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "你们靠谱吗")
	if r.IntentType != IntentObjectionTrust {
		t.Errorf("expected objection_trust, got %s", r.IntentType)
	}
}

// 15. 信任异议 - 信不过
func TestRecognizeRule_TrustObjectionBelieve(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "这个信不过")
	if r.IntentType != IntentObjectionTrust {
		t.Errorf("expected objection_trust, got %s", r.IntentType)
	}
}

// 16. 竞品异议 - 别家
func TestRecognizeRule_CompetitorObjectionOther(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "别家比你们便宜")
	if r.IntentType != IntentObjectionCompetitor {
		t.Errorf("expected objection_competitor, got %s", r.IntentType)
	}
}

// 17. 竞品异议 - 对比
func TestRecognizeRule_CompetitorObjectionCompare(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "对比一下别的家")
	if r.IntentType != IntentObjectionCompetitor {
		t.Errorf("expected objection_competitor, got %s", r.IntentType)
	}
}

// 18. 时机异议 - 过段时间
func TestRecognizeRule_TimingObjectionLater(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "过段时间再说吧")
	if r.IntentType != IntentObjectionTiming {
		t.Errorf("expected objection_timing, got %s", r.IntentType)
	}
}

// 19. 时机异议 - 不急
func TestRecognizeRule_TimingObjectionNotRush(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "不急")
	if r.IntentType != IntentObjectionTiming {
		t.Errorf("expected objection_timing, got %s", r.IntentType)
	}
}

// 20. 时机异议 - 忙
func TestRecognizeRule_TimingObjectionBusy(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "最近比较忙")
	if r.IntentType != IntentObjectionTiming {
		t.Errorf("expected objection_timing, got %s", r.IntentType)
	}
}

// 21. 购买意向 - 怎么买
func TestRecognizeRule_PurchaseHowToBuy(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "怎么买？")
	if r.IntentType != IntentPurchase {
		t.Errorf("expected purchase, got %s", r.IntentType)
	}
	if r.Sentiment != "positive" {
		t.Errorf("expected positive, got %s", r.Sentiment)
	}
}

// 22. 购买意向 - 下单
func TestRecognizeRule_PurchaseOrder(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "我要下单")
	if r.IntentType != IntentPurchase {
		t.Errorf("expected purchase, got %s", r.IntentType)
	}
}

// 23. 购买意向 - 付款
func TestRecognizeRule_PurchasePay(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "怎么付款")
	if r.IntentType != IntentPurchase {
		t.Errorf("expected purchase, got %s", r.IntentType)
	}
}

// 24. 购买意向 - 拍了
func TestRecognizeRule_PurchasePlaced(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "我直接拍了啊")
	if r.IntentType != IntentPurchase {
		t.Errorf("expected purchase, got %s", r.IntentType)
	}
}

// 25. 产品咨询 - 功能
func TestRecognizeRule_ProductFeature(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "这个产品有什么功能")
	if r.IntentType != IntentAskProduct {
		t.Errorf("expected ask_product, got %s", r.IntentType)
	}
}

// 26. 产品咨询 - 效果
func TestRecognizeRule_ProductEffect(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "效果怎么样")
	if r.IntentType != IntentAskProduct {
		t.Errorf("expected ask_product, got %s", r.IntentType)
	}
}

// 27. 产品咨询 - 怎么用
func TestRecognizeRule_ProductHowToUse(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "怎么用？")
	if r.IntentType != IntentAskProduct {
		t.Errorf("expected ask_product, got %s", r.IntentType)
	}
}

// 28. 服务咨询 - 售后
func TestRecognizeRule_ServiceAfterSale(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "售后怎么保障")
	if r.IntentType != IntentAskService {
		t.Errorf("expected ask_service, got %s", r.IntentType)
	}
}

// 29. 服务咨询 - 发货
func TestRecognizeRule_ServiceShip(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "多久能发货")
	if r.IntentType != IntentAskService {
		t.Errorf("expected ask_service, got %s", r.IntentType)
	}
}

// 30. 服务咨询 - 物流
func TestRecognizeRule_ServiceLogistics(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "物流怎么查")
	if r.IntentType != IntentAskService {
		t.Errorf("expected ask_service, got %s", r.IntentType)
	}
}

// 31. 售后问题 - 坏了
func TestRecognizeRule_AfterSaleBroken(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "我买的东西坏了")
	if r.IntentType != IntentAfterSale {
		t.Errorf("expected after_sale, got %s", r.IntentType)
	}
}

// 32. 售后问题 - 退货
func TestRecognizeRule_AfterSaleReturn(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "想退货怎么办")
	if r.IntentType != IntentAfterSale {
		t.Errorf("expected after_sale, got %s", r.IntentType)
	}
}

// 33. 售后问题 - 故障
func TestRecognizeRule_AfterSaleFault(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "故障了用不了")
	if r.IntentType != IntentAfterSale {
		t.Errorf("expected after_sale, got %s", r.IntentType)
	}
}

// 34. 流失倾向 - 别再发了
func TestRecognizeRule_ChurnStop(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "别再发了")
	if r.IntentType != IntentChurn {
		t.Errorf("expected churn, got %s", r.IntentType)
	}
	if r.Sentiment != "negative" {
		t.Errorf("expected negative, got %s", r.Sentiment)
	}
}

// 35. 流失倾向 - 拉黑
func TestRecognizeRule_ChurnBlock(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "我要拉黑你")
	if r.IntentType != IntentChurn {
		t.Errorf("expected churn, got %s", r.IntentType)
	}
}

// 36. 流失倾向 - 退订
func TestRecognizeRule_ChurnUnsubscribe(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "我要退订")
	if r.IntentType != IntentChurn {
		t.Errorf("expected churn, got %s", r.IntentType)
	}
}

// 37. 社交 - 在吗
func TestRecognizeRule_SocialHello(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "在吗？")
	if r.IntentType != IntentSocial {
		t.Errorf("expected social, got %s", r.IntentType)
	}
}

// 38. 打招呼 - 你好（D07: greeting 入词典，纯问候归 greeting）
func TestRecognizeRule_SocialGreet(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "你好")
	if r.IntentType != IntentGreeting {
		t.Errorf("expected greeting, got %s", r.IntentType)
	}
}

// 39. 打招呼 - hi（D07）
func TestRecognizeRule_SocialHi(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "hi")
	if r.IntentType != IntentGreeting {
		t.Errorf("expected greeting, got %s", r.IntentType)
	}
}

// 40. 投诉 - 投诉
func TestRecognizeRule_ComplaintBasic(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "我要投诉你")
	if r.IntentType != IntentComplaint {
		t.Errorf("expected complaint, got %s", r.IntentType)
	}
	if r.Sentiment != "negative" {
		t.Errorf("expected negative, got %s", r.Sentiment)
	}
}

// 41. 投诉 - 差评
func TestRecognizeRule_ComplaintBadReview(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "差评！")
	if r.IntentType != IntentComplaint {
		t.Errorf("expected complaint, got %s", r.IntentType)
	}
}

// 42. 投诉 - 垃圾
func TestRecognizeRule_ComplaintTrash(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "垃圾服务")
	if r.IntentType != IntentComplaint {
		t.Errorf("expected complaint, got %s", r.IntentType)
	}
}

// 43. 空文本
func TestRecognize_EmptyText(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "")
	if r == nil {
		t.Fatal("expected non-nil result")
	}
	if r.IntentType != IntentUnknown {
		t.Errorf("expected unknown, got %s", r.IntentType)
	}
}

// 44. 纯空格
func TestRecognize_OnlySpaces(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "    ")
	if r == nil {
		t.Fatal("expected non-nil result")
	}
}

// 45. 兜底 - 完全无关文本
func TestRecognize_UnknownFallback(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "xxxxx yyyyy zzzz")
	if r == nil {
		t.Fatal("expected non-nil result")
	}
	if r.IntentType != IntentUnknown {
		t.Errorf("expected unknown, got %s", r.IntentType)
	}
	if r.Confidence <= 0 {
		t.Errorf("expected >0, got %f", r.Confidence)
	}
}

// 46. 大小写不敏感
func TestRecognize_CaseInsensitive(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "HI")
	if r.IntentType != IntentGreeting {
		t.Errorf("expected greeting, got %s", r.IntentType)
	}
}

// 47. 包含数字的文本
func TestRecognize_WithNumbers(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "这个多少钱 199元")
	if r.IntentType != IntentPriceInquiry {
		t.Errorf("expected price_inquiry, got %s", r.IntentType)
	}
}

// 48. 多意图文本
func TestRecognize_MultipleIntents(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "多少钱 太贵了")
	if r == nil {
		t.Fatal("expected non-nil result")
	}
	if r.IntentType == "" {
		t.Error("expected non-empty intent")
	}
}

// 49. 完全匹配高置信度
func TestRecognize_ExactHighConfidence(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "这个多少钱？")
	if r.Confidence < 0.9 {
		t.Errorf("expected high confidence, got %f", r.Confidence)
	}
	if r.ConfidenceLevel != "high" {
		t.Errorf("expected high level, got %s", r.ConfidenceLevel)
	}
}

// 50. 关键词匹配中等置信度
func TestRecognize_KeywordMediumConfidence(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "价格")
	if r.Confidence < 0.6 {
		t.Errorf("expected medium, got %f", r.Confidence)
	}
}

// 51. 多关键词高置信度
func TestRecognize_MultipleKeywordConfidence(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "价格太贵了不划算")
	if r.Confidence < 0.8 {
		t.Errorf("expected high, got %f", r.Confidence)
	}
}

// 52. 识别记录入库
func TestRecognize_Persist(t *testing.T) {
	rec, db := newIntentRecognizer(t)
	rec.Recognize(context.Background(), "s-1", "u-1", "多少钱")
	time.Sleep(200 * time.Millisecond)
	var count int64
	db.Model(&model.IntentRecord{}).Count(&count)
	if count == 0 {
		t.Error("expected record to be saved")
	}
}

// 53. 多次识别累加
func TestRecognize_MultiplePersist(t *testing.T) {
	rec, db := newIntentRecognizer(t)
	for i := 0; i < 5; i++ {
		rec.Recognize(context.Background(), "s-1", "u-1", "多少钱")
	}
	time.Sleep(200 * time.Millisecond)
	var count int64
	db.Model(&model.IntentRecord{}).Count(&count)
	if count < 5 {
		t.Errorf("expected >=5, got %d", count)
	}
}

// 54. 记录字段完整
func TestRecognize_RecordFields(t *testing.T) {
	rec, db := newIntentRecognizer(t)
	rec.Recognize(context.Background(), "s-1", "u-1", "多少钱")
	time.Sleep(200 * time.Millisecond)
	var r model.IntentRecord
	db.First(&r)

	if r.SessionID != "s-1" {
		t.Errorf("expected s-1, got %s", r.SessionID)
	}
	if r.CustomerID != "u-1" {
		t.Errorf("expected u-1, got %s", r.CustomerID)
	}
	if r.RawText != "多少钱" {
		t.Errorf("expected 多少钱, got %s", r.RawText)
	}
	if r.IntentType != IntentPriceInquiry {
		t.Errorf("expected price_inquiry, got %s", r.IntentType)
	}
}

// 55. 统计为空
func TestIntentStats_Empty(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	stats, _ := rec.GetIntentStats(context.Background(), 7)
	if len(stats) != 0 {
		t.Errorf("expected 0, got %d", len(stats))
	}
}

// 56. 统计
func TestStats_Populated(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	rec.Recognize(context.Background(), "s-1", "u-1", "多少钱")
	rec.Recognize(context.Background(), "s-1", "u-1", "太贵了")
	rec.Recognize(context.Background(), "s-1", "u-1", "太贵了")
	time.Sleep(200 * time.Millisecond)
	stats, _ := rec.GetIntentStats(context.Background(), 7)
	if stats[IntentPriceInquiry] != 1 {
		t.Errorf("expected 1, got %d", stats[IntentPriceInquiry])
	}
	if stats[IntentObjectionPrice] != 2 {
		t.Errorf("expected 2, got %d", stats[IntentObjectionPrice])
	}
}

// 57. 统计 days=0
func TestStats_ZeroDays(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	rec.Recognize(context.Background(), "s-1", "u-1", "多少钱")
	time.Sleep(200 * time.Millisecond)
	stats, _ := rec.GetIntentStats(context.Background(), 0)
	_ = stats
}

// 59. 客户近期意图
func TestRecentIntents_Basic(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	rec.Recognize(context.Background(), "s-1", "u-1", "多少钱")
	rec.Recognize(context.Background(), "s-1", "u-1", "太贵了")
	list := waitForIntentCount(t, rec, "u-1", 2, 3*time.Second)
	if len(list) != 2 {
		t.Errorf("expected 2, got %d", len(list))
	}
}

// 60. 客户隔离
func TestRecentIntents_CustomerIsolation(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	rec.Recognize(context.Background(), "s-1", "u-1", "多少钱")
	rec.Recognize(context.Background(), "s-1", "u-2", "多少钱")
	list := waitForIntentCount(t, rec, "u-1", 1, 3*time.Second)
	if len(list) != 1 {
		t.Errorf("expected 1, got %d", len(list))
	}
}

// 61. 客户限制
func TestRecentIntents_Limit(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	for i := 0; i < 10; i++ {
		rec.Recognize(context.Background(), "s-1", "u-1", "多少钱")
	}
	waitForIntentCount(t, rec, "u-1", 10, 5*time.Second)
	list, _ := rec.GetRecentIntents(context.Background(), "u-1", 3)
	if len(list) != 3 {
		t.Errorf("expected 3, got %d", len(list))
	}
}

// 62. 按时间倒序
func TestRecentIntents_OrderDesc(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	rec.Recognize(context.Background(), "s-1", "u-1", "多少钱")
	time.Sleep(50 * time.Millisecond)
	rec.Recognize(context.Background(), "s-1", "u-1", "太贵了")
	list := waitForIntentCount(t, rec, "u-1", 2, 3*time.Second)
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}
	if list[0].IntentType != IntentObjectionPrice {
		t.Errorf("expected latest first, got %s", list[0].IntentType)
	}
}

// 63. 情感 - 流失
func TestSentiment_Churn(t *testing.T) {
	if inferSentiment(IntentChurn) != "negative" {
		t.Error("expected negative")
	}
}

// 64. 情感 - 投诉
func TestSentiment_Complaint(t *testing.T) {
	if inferSentiment(IntentComplaint) != "negative" {
		t.Error("expected negative")
	}
}

// 65. 情感 - 购买
func TestSentiment_Purchase(t *testing.T) {
	if inferSentiment(IntentPurchase) != "positive" {
		t.Error("expected positive")
	}
}

// 66. 情感 - 询价
func TestSentiment_PriceInquiry(t *testing.T) {
	if inferSentiment(IntentPriceInquiry) != "positive" {
		t.Error("expected positive")
	}
}

// 67. 情感 - 社交
func TestSentiment_Social(t *testing.T) {
	if inferSentiment(IntentSocial) != "neutral" {
		t.Error("expected neutral")
	}
}

// 68. 情感 - 售后
func TestSentiment_AfterSale(t *testing.T) {
	if inferSentiment(IntentAfterSale) != "neutral" {
		t.Error("expected neutral")
	}
}

// 69. 情感 - 未知
func TestSentiment_Unknown(t *testing.T) {
	if inferSentiment(IntentUnknown) != "neutral" {
		t.Error("expected neutral")
	}
}

// 70. extractJSON - 标准JSON
func TestExtractJSON_Normal(t *testing.T) {
	s := `prefix {"a": 1} suffix`
	got := extractJSONFromStr(s)
	if got != `{"a": 1}` {
		t.Errorf("expected %s, got %s", `{"a": 1}`, got)
	}
}

// 71. extractJSON - 多JSON
func TestExtractJSON_Multiple(t *testing.T) {
	s := `noise {"a": 1} more {"b": 2}`
	got := extractJSONFromStr(s)
	if !strings.Contains(got, `"b"`) {
		t.Errorf("expected b, got %s", got)
	}
}

// 72. extractJSON - 纯JSON
func TestExtractJSON_Pure(t *testing.T) {
	s := `{"a": 1}`
	got := extractJSONFromStr(s)
	if got != `{"a": 1}` {
		t.Errorf("got %s", got)
	}
}

// 73. extractJSON - 无JSON
func TestExtractJSON_Empty(t *testing.T) {
	s := `no json here`
	got := extractJSONFromStr(s)
	if got != s {
		t.Errorf("expected unchanged, got %s", got)
	}
}

// 74. extractJSON - 嵌套
func TestExtractJSON_Nested(t *testing.T) {
	s := `pre {"a": {"b": 1}} post`
	got := extractJSONFromStr(s)
	if !strings.Contains(got, `"b"`) {
		t.Errorf("expected nested, got %s", got)
	}
}

// 75. entitiesToMap - 空
func TestEntitiesToMap_Empty(t *testing.T) {
	m := entitiesToMap(nil)
	if m == nil {
		t.Error("expected non-nil")
	}
}

// 76. entitiesToMap - JSON
func TestEntitiesToMap_JSON(t *testing.T) {
	m := entitiesToMap([]byte(`{"a": 1}`))
	if m["a"] != float64(1) {
		t.Errorf("expected 1, got %v", m["a"])
	}
}

// 77. entitiesToMap - 错误JSON
func TestEntitiesToMap_BadJSON(t *testing.T) {
	m := entitiesToMap([]byte(`bad`))
	if m == nil {
		t.Error("expected non-nil")
	}
}

// 78. recognizeByRule 优先级 - 完全匹配
func TestRecognizeByRule_ExactPriority(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r := rec.recognizeByRule(context.Background(), "这个多少钱？")
	if r == nil {
		t.Fatal("expected non-nil")
	}
	if r.Confidence < 0.9 {
		t.Errorf("expected high, got %f", r.Confidence)
	}
}

// 79. recognizeByRule 关键词降级
func TestRecognizeByRule_KeywordFallback(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r := rec.recognizeByRule(context.Background(), "随便问一下价格")
	if r == nil {
		t.Fatal("expected non-nil")
	}
}

// 80. recognizeByRule 零匹配
func TestRecognizeByRule_NoMatch(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r := rec.recognizeByRule(context.Background(), "xxxxxxxxxxxx")
	if r != nil {
		t.Error("expected nil for no match")
	}
}

// 81. recognizeByRule 空格trim
func TestRecognizeByRule_TrimSpaces(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r := rec.recognizeByRule(context.Background(), "   多少钱   ")
	if r == nil {
		t.Fatal("expected non-nil")
	}
}

// 82. recognizeByRule 置信度封顶
func TestRecognizeByRule_ConfCapped(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r := rec.recognizeByRule(context.Background(), "价格太贵了不划算不值这个价")
	if r == nil {
		t.Fatal("expected non-nil")
	}
	if r.Confidence > 0.92 {
		t.Errorf("expected cap 0.92, got %f", r.Confidence)
	}
}

// 83. 默认意图数量
func TestDefaultIntents_Count(t *testing.T) {
	if len(DefaultIntents) < 10 {
		t.Errorf("expected >=10, got %d", len(DefaultIntents))
	}
}

// 84. 默认意图字段
func TestDefaultIntents_Fields(t *testing.T) {
	for _, def := range DefaultIntents {
		if def.Type == "" {
			t.Error("expected non-empty type")
		}
		if def.Name == "" {
			t.Error("expected non-empty name")
		}
		if len(def.Keywords) == 0 {
			t.Errorf("expected keywords, got 0 for %s", def.Type)
		}
	}
}

// 85. 默认意图唯一性
func TestDefaultIntents_Unique(t *testing.T) {
	seen := map[string]bool{}
	for _, def := range DefaultIntents {
		if seen[def.Type] {
			t.Errorf("duplicate: %s", def.Type)
		}
		seen[def.Type] = true
	}
}

// 86. 意图常量
func TestIntentConstants(t *testing.T) {
	if IntentPriceInquiry == "" {
		t.Error("expected non-empty")
	}
	if IntentPurchase == "" {
		t.Error("expected non-empty")
	}
	if IntentUnknown == "" {
		t.Error("expected non-empty")
	}
	if IntentGreeting == "" {
		t.Error("expected non-empty")
	}
}

// 87. 意图常量区别
func TestIntentConstants_Different(t *testing.T) {
	constants := []string{
		IntentPriceInquiry, IntentObjectionPrice, IntentObjectionNeed,
		IntentObjectionTrust, IntentObjectionCompetitor, IntentObjectionTiming,
		IntentPurchase, IntentAskProduct, IntentAskService,
		IntentAfterSale, IntentChurn, IntentSocial, IntentGreeting, IntentComplaint, IntentUnknown,
	}
	seen := map[string]bool{}
	for _, c := range constants {
		if seen[c] {
			t.Errorf("duplicate: %s", c)
		}
		seen[c] = true
	}
}

// 88. LLM 不可用时 兜底 unknown
func TestRecognize_LLMUnavailableFallback(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "随便聊聊废话xxx")
	if r.IntentType != IntentUnknown {
		t.Errorf("expected unknown fallback, got %s", r.IntentType)
	}
	if r.Method != "rule" {
		t.Errorf("expected rule method, got %s", r.Method)
	}
}

// 89. 初始化
func TestInitIntentRecognizer(t *testing.T) {
	db := setupIntentTestDB(t)
	rec1 := InitIntentRecognizer(db, nil, nil)
	rec2 := GetIntentRecognizer()
	if rec1 != rec2 {
		t.Error("expected same instance")
	}
}

// 90. nil dispatcher 不 panic
func TestRecognize_NilDispatcher(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, err := rec.Recognize(context.Background(), "s-1", "u-1", "多少钱")
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if r == nil {
		t.Error("expected non-nil")
	}
}

// 91. nil cache 不 panic
func TestRecognize_NilCache(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, err := rec.Recognize(context.Background(), "s-1", "u-1", "多少钱")
	if err != nil {
		t.Errorf("expected nil error, got %v", err)
	}
	if r == nil {
		t.Error("expected non-nil")
	}
}

// 92. 极长文本
func TestRecognize_LongText(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	longText := strings.Repeat("多少钱 ", 200)
	r, err := rec.Recognize(context.Background(), "s-1", "u-1", longText)
	if err != nil {
		t.Fatal(err)
	}
	if r == nil {
		t.Fatal("expected non-nil")
	}
}

// 93. 单字符
func TestRecognize_SingleChar(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "x")
	if r == nil {
		t.Fatal("expected non-nil")
	}
}

// 94. 中英混合
func TestRecognize_ChineseEnglish(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "Hello 你好")
	if r == nil {
		t.Fatal("expected non-nil")
	}
}

// 95. 表情符号
func TestRecognize_WithEmoji(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "你好😊")
	if r == nil {
		t.Fatal("expected non-nil")
	}
	if r.IntentType != IntentGreeting {
		t.Errorf("expected greeting, got %s", r.IntentType)
	}
}

// 96. 标点符号
func TestRecognize_WithPunctuation(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "多少钱？！！！")
	if r.IntentType != IntentPriceInquiry {
		t.Errorf("expected price_inquiry, got %s", r.IntentType)
	}
}

// 97. 阿拉伯数字
func TestRecognize_WithDigits(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "100元")
	if r == nil {
		t.Fatal("expected non-nil")
	}
}

// 98. unicode 字符
func TestRecognize_Unicode(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "🎉🎁")
	if r == nil {
		t.Fatal("expected non-nil")
	}
}

// 99. 大量重复
func TestRecognize_RepeatText(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "啊啊啊啊啊啊啊啊")
	if r == nil {
		t.Fatal("expected non-nil")
	}
}

// 100. 句子分词 - 含关键词但中间有其他词
func TestRecognize_KeywordInMiddle(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "我觉这个东西不划算")
	if r.IntentType != IntentObjectionPrice {
		t.Errorf("expected objection_price, got %s", r.IntentType)
	}
}

// 101. RecognizeResult 字段填充
func TestRecognizeResult_Fields(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "多少钱？")
	if r.IntentName == "" {
		t.Error("expected non-empty intent_name")
	}
	if r.ConfidenceLevel == "" {
		t.Error("expected non-empty confidence_level")
	}
	if r.Sentiment == "" {
		t.Error("expected non-empty sentiment")
	}
	if r.Method == "" {
		t.Error("expected non-empty method")
	}
}

// 102. records 同步写入
func TestRecognize_RecordSync(t *testing.T) {
	rec, db := newIntentRecognizer(t)
	rec.Recognize(context.Background(), "s-1", "u-1", "多少钱")
	rec.Recognize(context.Background(), "s-1", "u-1", "太贵了")
	for i := 0; i < 20; i++ {
		time.Sleep(50 * time.Millisecond)
		var c int64
		db.Model(&model.IntentRecord{}).Count(&c)
		if c >= 2 {
			return
		}
	}
	var c int64
	db.Model(&model.IntentRecord{}).Count(&c)
	if c < 2 {
		t.Errorf("expected 2 records, got %d", c)
	}
}

// 103. 实体字段
func TestRecognize_EntitiesField(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "多少钱？")
	if r.Method != "rule" {
		t.Logf("method=%s", r.Method)
	}
}

// 104. 客户重复消息
func TestRecognize_SameCustomerMultiple(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	rec.Recognize(context.Background(), "s-1", "u-1", "多少钱")
	rec.Recognize(context.Background(), "s-1", "u-1", "多少钱")
	rec.Recognize(context.Background(), "s-1", "u-1", "多少钱")
	time.Sleep(200 * time.Millisecond)
	list, _ := rec.GetRecentIntents(context.Background(), "u-1", 10)
	if len(list) != 3 {
		t.Errorf("expected 3, got %d", len(list))
	}
}

// 105. intent_subtype 字段
func TestRecognize_IntentSubtypeField(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	r, _ := rec.Recognize(context.Background(), "s-1", "u-1", "多少钱？")
	if r == nil {
		t.Fatal("expected non-nil")
	}
	if r.IntentSubtype != "" {
		t.Logf("subtype=%s", r.IntentSubtype)
	}
}

// 106. 多 session 客户
func TestRecognize_MultiSession(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	rec.Recognize(context.Background(), "s-1", "u-1", "多少钱")
	rec.Recognize(context.Background(), "s-2", "u-1", "多少钱")
	time.Sleep(200 * time.Millisecond)
	list, _ := rec.GetRecentIntents(context.Background(), "u-1", 10)
	if len(list) != 2 {
		t.Errorf("expected 2 across sessions, got %d", len(list))
	}
}

func allIntentConstants() []string {
	return []string{
		IntentObjectionPrice, IntentObjectionNeed, IntentObjectionTrust, IntentObjectionTiming,
		IntentObjectionCompetitor, IntentPurchase, IntentAskProduct, IntentAskService,
		IntentPriceInquiry, IntentAfterSale, IntentChurn, IntentSocial, IntentGreeting,
		IntentComplaint,
	}
}

func TestD07_DictionaryCoversAllIntentConstants(t *testing.T) {
	exempt := map[string]bool{
		IntentUnknown: true,
		IntentClarify: true,
	}
	dict := map[string]bool{}
	for _, def := range DefaultIntents {
		dict[def.Type] = true
	}
	for _, it := range allIntentConstants() {
		if exempt[it] {
			continue
		}
		if !dict[it] {
			t.Errorf("意图常量 %q 未在 DefaultIntents 词典中（G3 漂移复发）", it)
		}
	}
}

func TestD07_GreetingRecognizableByRule(t *testing.T) {
	rec, _ := newIntentRecognizer(t)
	for _, text := range []string{"你好", "hello", "hi", "您好", "早上好"} {
		r, _ := rec.Recognize(context.Background(), "s-d07", "u-d07", text)
		if r == nil || r.IntentType != IntentGreeting {
			t.Errorf("%q: expected greeting, got %+v", text, r)
		}
	}
}
