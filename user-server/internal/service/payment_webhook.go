// payment_webhook.go T-P7-02：把"订单回调"升级成"订单回调里可能带着一笔钱"。
//
// 本文件只做三件**纯解析**的事（不碰库、不碰全局），所以它能被表驱动用例逐格钉住：
//  1. OrderPaymentSink —— 订单腿对回款腿的最小依赖面（两格：能不能用、记一笔）；
//  2. ParseOrderWebhookPayment —— 载荷里那个 payment 格子 → RecordPaymentInput；
//  3. orderWebhookEventKey —— 由**内容**派生事件号（G15 第⑤条：留痕要记"渠道报了哪几件事"，
//     不是"我们被叫了几次"）。
//
// 为什么解析住在 service 而不下沉到 controller：载荷的字段白名单是**资金判据**的一部分
// （未知格子必须报错，见下），而 controller 只负责把 HTTP 状态映射出去。
package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"strings"

	"hivemtk-user/internal/model"
)

// OrderPaymentSink 订单 webhook 对回款腿的全部依赖。
//
// 只有"记一笔"，**没有**冲销、没有改账单、没有读账单：回调这一腿的职责是把渠道报的钱收进
// 那一格，而"这笔钱算不算结清了"由回款腿自己按整张单求和判（见 RecordPayment）。
// 用接口而不是直接持 *PaymentService 还有一个实际作用：装配失败的环境里
// SetOrderPaymentSink(nil) 之后这一腿必须**明确报错**而不是静默少记一笔钱，
// 而"静默少记"正是本卡 AC① 之外最贵的一种坏法。
type OrderPaymentSink interface {
	Available() bool
	RecordPayment(ctx context.Context, in RecordPaymentInput) (*PaymentReceipt, error)
}

// OrderWebhookResult 一次订单回调的结论（控制器的 data 就是它）。
//
// 两格都是调用方**自己读不出来**的事实：
//   - PaymentPresent 区分"这条回调压根不带钱"与"带着钱但没进账"。前者是正常旧载荷，
//     后者是要人去看的一句话，回 nil 会把两者压成同一个响应；
//   - Payment 是回款腿的结论（含结清后的账单状态）。渠道收到它才知道"这次你们记了多少、
//     那张单还差多少"。
type OrderWebhookResult struct {
	PaymentPresent bool            `json:"payment_present"`
	Payment        *PaymentReceipt `json:"payment,omitempty"`
}

// orderWebhookPaymentKeys 载荷 payment 格子的字段白名单。
//
// 白名单而不是"认识的就取、不认识的原样忽略"：未知格子说明**渠道在按另一份契约推送**，
// 而那多半是同一笔钱的另一种写法（比如 bill_amount 与 amount 并存且不相等）。
// 忽略它 = 按半份契约把钱记进去，而这一格记的是钱。
var orderWebhookPaymentKeys = map[string]bool{
	"bill_id":     true,
	"channel_ref": true,
	"amount":      true,
	"currency":    true,
	"paid_at":     true,
	"status":      true,
}

// ParseOrderWebhookPayment 从订单载荷里取那一笔钱。
//
// 三个返回值合成起来才是一句完整的话：
//   - present=false ⇒ 载荷没有 payment 格子（T-P7-02 之前的旧契约，AC① 必须照旧成功）；
//   - present=true + err ⇒ 渠道**报了钱**而这笔钱进不去，调用方必须把它回给渠道；
//   - present=true + 无 err ⇒ in 可以递给回款腿。
//
// 注意 platform / order_id 是从**路径**上拿的，不是从载荷里：载荷自述的订单号与回调
// 打到哪个平台、哪一单是两件事，前者由推送方填。来路以路径为准（与镜像同一口径）。
func ParseOrderWebhookPayment(body map[string]any, platform, orderID string) (RecordPaymentInput, bool, error) {
	if body == nil {
		return RecordPaymentInput{}, false, nil
	}
	raw, ok := body["payment"]
	if !ok || raw == nil {
		return RecordPaymentInput{}, false, nil
	}
	obj, ok := raw.(map[string]any)
	if !ok {
		return RecordPaymentInput{}, true, fmt.Errorf(
			"%w: payment 格子不是对象（实际 %T）——认不出形状就当没有这一格，等于把渠道报的一笔钱静默丢掉", ErrPaymentInputInvalid, raw)
	}
	for k := range obj {
		if !orderWebhookPaymentKeys[k] {
			return RecordPaymentInput{}, true, fmt.Errorf(
				"%w: payment 里有不认识的字段 %q（认识的只有 bill_id/channel_ref/amount/currency/paid_at/status）", ErrPaymentInputInvalid, k)
		}
	}

	in := RecordPaymentInput{Platform: platform, OrderID: orderID}

	billID, _ := obj["bill_id"].(string)
	billID = strings.TrimSpace(billID)
	if billID == "" {
		return RecordPaymentInput{}, true, fmt.Errorf("%w: payment.bill_id 为空（不知道这笔钱冲的是哪张应收）", ErrPaymentInputInvalid)
	}
	in.BillID = billID

	ref, _ := obj["channel_ref"].(string)
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return RecordPaymentInput{}, true, fmt.Errorf(
			"%w: payment.channel_ref 为空（没有流水号就没有幂等键，重复推送会各记一笔）", ErrPaymentInputInvalid)
	}
	in.ChannelRef = ref

	v, ok := obj["amount"]
	if !ok || v == nil {
		return RecordPaymentInput{}, true, fmt.Errorf("%w: payment.amount 缺失", ErrPaymentInputInvalid)
	}
	amount, ok := webhookAmount(v)
	if !ok {
		return RecordPaymentInput{}, true, fmt.Errorf("%w: payment.amount 不是十进制金额（%T %v）", ErrPaymentInputInvalid, v, v)
	}
	in.Amount = amount

	if cur, exists := obj["currency"]; exists && cur != nil {
		s, isStr := cur.(string)
		if !isStr {
			return RecordPaymentInput{}, true, fmt.Errorf("%w: payment.currency 不是字符串（%T）", ErrPaymentInputInvalid, cur)
		}
		in.Currency = strings.ToUpper(strings.TrimSpace(s)) // 空串留给回款腿解释成"随账单币种"
	}

	if st, exists := obj["status"]; exists && st != nil {
		s, isStr := st.(string)
		if !isStr {
			return RecordPaymentInput{}, true, fmt.Errorf("%w: payment.status 不是字符串（%T）", ErrPaymentInputInvalid, st)
		}
		s = strings.TrimSpace(s)
		if s != "" && !model.PaymentStatusKnown(s) {
			return RecordPaymentInput{}, true, fmt.Errorf(
				"%w: payment.status=%q 不在值域（%s）——按渠道给的词原样入账会让结清求和读不出这一笔算不算数",
				ErrPaymentInputInvalid, s, strings.Join(model.PaymentStatuses, "/"))
		}
		in.Status = s
	}

	if t, exists := obj["paid_at"]; exists && t != nil {
		parsed, ok := parseWebhookTime(t)
		if !ok {
			return RecordPaymentInput{}, true, fmt.Errorf("%w: payment.paid_at 解析不出时间（%T）", ErrPaymentInputInvalid, t)
		}
		in.PaidAt = parsed
	}

	return in, true, nil
}

// webhookAmount 把载荷里的金额归到 float64。
//
// 收 float64（encoding/json 的默认形状）、各宽度整数、json.Number，以及**十进制文本**：
// 渠道把 369.99 写成字符串是常态，而"文本金额"要么完整认下、要么明确报错，
// 不许出现"看起来认了、其实丢了小数"。
// 拒 NaN/Inf：strconv.ParseFloat 会把 "Inf"、"1e999" 当成合法浮点，而 numeric(14,2) 存不下，
// 让它们在解析这一层就出局，比让它们走到库里再撞列量程好读得多。
func webhookAmount(v any) (float64, bool) {
	var f float64
	switch n := v.(type) {
	case float64:
		f = n
	case float32:
		f = float64(n)
	case int:
		f = float64(n)
	case int32:
		f = float64(n)
	case int64:
		f = float64(n)
	case json.Number:
		parsed, err := n.Float64()
		if err != nil {
			return 0, false
		}
		f = parsed
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(n), 64)
		if err != nil {
			return 0, false
		}
		f = parsed
	default:
		return 0, false
	}
	if math.IsNaN(f) || math.IsInf(f, 0) {
		return 0, false
	}
	return f, true
}

// webhookMoneyCent 把载荷里的金额落成镜像那三列（bigint，口径是**元取整**）。
//
// 老代码是 int64(v)：向零截断，199.99 → 199（G15 第⑥条）。这一格是运营读订单时看到的
// "收了多少钱"，系统性少记一分的方向恰好是最不该少记的那一侧，改成四舍五入。
// 列型不动：改成 numeric 要在存量表上 ALTER，而这张表上没有任何一处按它做对账
// （对账读 bills/payments，见 model/integration.go 那段）。
func webhookMoneyCent(v any) (int64, bool) {
	f, ok := webhookAmount(v)
	if !ok {
		return 0, false
	}
	return int64(math.Round(f)), true
}

// orderWebhookEventKey 由**内容**派生 webhook 事件号。
//
// 老形状 platform:orderID:UnixNano 永远撞不上唯一索引，于是这张表实际记的是
// "我们被叫了几次"（一次正常重投留三行）。换成内容摘要之后它记的才是"渠道报了哪几件事"：
//   - 同一件事（同平台、同单、同状态、同载荷）重复投递 ⇒ 同一个键 ⇒ 一行；
//   - 状态或载荷变了 ⇒ 另一个键 ⇒ 另一行。
//
// **这个键只用于留痕，绝不当处理的闸门**：见 IntegrationService.recordOrderWebhookEvent
// 那句"撞到唯一键就继续"。拿它当闸门会造出一种更坏的坏法——第一次投递把镜像写坏了、
// 渠道重推时被"这个事件我见过了"挡在门外，那一单永远修不好。
// 定长 44 字符（列是 varchar(100)）：整段一起做摘要，不把 order_id 原样拼进去，
// 否则拼出来的长度由平台给的多长就有多长，而它自己就超列宽。
func orderWebhookEventKey(platform, orderID, status string, raw map[string]any) string {
	h := sha256.New()
	h.Write([]byte(platform))
	h.Write([]byte{0})
	h.Write([]byte(orderID))
	h.Write([]byte{0})
	h.Write([]byte(status))
	h.Write([]byte{0})
	h.Write([]byte(orderWebhookCanonicalText(raw)))
	return "owe_" + hex.EncodeToString(h.Sum(nil))[:40]
}

// orderWebhookCanonicalText 载荷的规范文本形态：事件摘要的输入，也是 RawData 存的那一格。
//
// 两处必须是**同一个函数**的输出：摘要按 A 形态算、库里存 B 形态，
// 那"重放这一条"就没有判据了（重放读的是 RawData，去重读的是摘要）。
// 用 encoding/json 而不是老代码的 fmt.Sprintf("%v")：前者对 map 键排序，
// 所以"同一份载荷"在两个进程里也是同一段字节；后者稳定但没有结构，
// 而这一格是唯一一处"渠道原样说了什么"的存证。
func orderWebhookCanonicalText(raw map[string]any) string {
	if raw == nil {
		return "{}"
	}
	canonical, err := json.Marshal(raw)
	if err != nil {
		// encoding/json 编不出的东西（NaN、chan、func）。退到 %v：
		// 同一份载荷的 %v 仍然稳定，去重判据不失效，只是丢了结构。
		return fmt.Sprintf("%v", raw)
	}
	return string(canonical)
}
