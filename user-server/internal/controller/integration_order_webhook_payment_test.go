package controller

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/middleware"
	"hivemtk-user/internal/model"
	"hivemtk-user/internal/service"
)

// T-P7-02 控制器侧的**状态码契约**。
//
// 为什么这一格必须在控制器测：回款腿的 sentinel 在 service 层已经分好类了
// （载荷坏 / 没有这张单 / 单作废了 / 装配缺件 / 落库卡住），但"分好类"对渠道不值钱 ——
// 值钱的是渠道能按状态码决定**要不要重投**：
//   - 4xx ⇒ 重投同一份载荷永远修不好，必须改数据或改载荷，渠道应当停止重试并报警；
//   - 503 ⇒ 我们这一侧的装配问题，渠道重投正是我们要的。
//
// 于是这里的判据有两组：每组错误都要同时证"状态码分类正确"与"镜像已经落库"。
// 后者是本卡的核心不变量：**钱没记上，单还得在**。少了这一条，"回款拒了"就会连带
// 把订单镜像一起丢掉，而重推时镜像那一腿又要从零走一遍。

type stubPaymentSink struct {
	calls   []service.RecordPaymentInput
	receipt *service.PaymentReceipt
	err     error
	avail   bool
}

func (s *stubPaymentSink) Available() bool { return s.avail }

func (s *stubPaymentSink) RecordPayment(_ context.Context, in service.RecordPaymentInput) (*service.PaymentReceipt, error) {
	s.calls = append(s.calls, in)
	if s.err != nil {
		return nil, s.err
	}
	if s.receipt != nil {
		return s.receipt, nil
	}
	return &service.PaymentReceipt{}, nil
}

func TestReceiveOrderWebhook_PaymentStatusCodes(t *testing.T) {
	database := setupOrderWebhookControllerDB(t)

	for i, tc := range []struct {
		name       string
		body       string
		sink       *stubPaymentSink
		injectSink bool
		wantCalls  int // 回款腿该被叫几次（0 = 这一格的判据根本轮不到它）
		wantCode   int
		wantInBody string
		wantMirror int64
	}{
		{
			name:       "旧载荷（无 payment）照旧成功",
			wantCalls:  0,
			body:       `{"order_id":"HTTP-LEGACY","status":"paid"}`,
			wantCode:   http.StatusOK,
			wantInBody: `"payment_present":false`,
			wantMirror: 1,
		},
		{
			name:       "带钱且记上了",
			wantCalls:  1,
			body:       `{"order_id":"HTTP-OK","status":"paid","payment":{"bill_id":"b_1","channel_ref":"r_1","amount":100}}`,
			sink:       &stubPaymentSink{avail: true},
			injectSink: true,
			wantCode:   http.StatusOK,
			wantInBody: `"payment_present":true`,
			wantMirror: 1,
		},
		{
			name:       "载荷里的钱看不懂 ⇒ 400（重投同一份修不好）",
			wantCalls:  0,
			body:       `{"order_id":"HTTP-BAD-AMOUNT","status":"paid","payment":{"bill_id":"b_1","channel_ref":"r_1","amount":"12元"}}`,
			sink:       &stubPaymentSink{avail: true},
			injectSink: true,
			wantCode:   http.StatusBadRequest,
			wantMirror: 1,
		},
		{
			name:       "回款腿未装配 ⇒ 503（要渠道重投）",
			wantCalls:  0,
			body:       `{"order_id":"HTTP-NOSINK","status":"paid","payment":{"bill_id":"b_1","channel_ref":"r_1","amount":100}}`,
			wantCode:   http.StatusServiceUnavailable,
			wantInBody: "镜像",
			wantMirror: 1,
		},
		{
			name:       "回款腿装了但缺件 ⇒ 503",
			wantCalls:  0,
			body:       `{"order_id":"HTTP-UNAVAIL","status":"paid","payment":{"bill_id":"b_1","channel_ref":"r_1","amount":100}}`,
			sink:       &stubPaymentSink{},
			injectSink: true,
			wantCode:   http.StatusServiceUnavailable,
			wantMirror: 1,
		},
		{
			name:       "那张应收不存在 ⇒ 404",
			wantCalls:  1,
			body:       `{"order_id":"HTTP-NOBILL","status":"paid","payment":{"bill_id":"b_x","channel_ref":"r_x","amount":100}}`,
			sink:       &stubPaymentSink{avail: true, err: service.ErrPaymentBillNotFound},
			injectSink: true,
			wantCode:   http.StatusNotFound,
			wantMirror: 1,
		},
		{
			name:       "账单已作废 ⇒ 409",
			wantCalls:  1,
			body:       `{"order_id":"HTTP-VOID","status":"paid","payment":{"bill_id":"b_x","channel_ref":"r_x","amount":100}}`,
			sink:       &stubPaymentSink{avail: true, err: service.ErrPaymentBillVoided},
			injectSink: true,
			wantCode:   http.StatusConflict,
			wantMirror: 1,
		},
		{
			name:       "流水号复用 ⇒ 409",
			wantCalls:  1,
			body:       `{"order_id":"HTTP-REUSE","status":"paid","payment":{"bill_id":"b_x","channel_ref":"r_x","amount":100}}`,
			sink:       &stubPaymentSink{avail: true, err: service.ErrPaymentRefReuse},
			injectSink: true,
			wantCode:   http.StatusConflict,
			wantMirror: 1,
		},
		{
			name:       "币种不符 ⇒ 409",
			wantCalls:  1,
			body:       `{"order_id":"HTTP-CUR","status":"paid","payment":{"bill_id":"b_x","channel_ref":"r_x","amount":100}}`,
			sink:       &stubPaymentSink{avail: true, err: service.ErrPaymentCurrencyMismatch},
			injectSink: true,
			wantCode:   http.StatusConflict,
			wantMirror: 1,
		},
		{
			name:       "重复冲销 ⇒ 409",
			wantCalls:  1,
			body:       `{"order_id":"HTTP-REVD","status":"paid","payment":{"bill_id":"b_x","channel_ref":"r_x","amount":100}}`,
			sink:       &stubPaymentSink{avail: true, err: service.ErrPaymentAlreadyReversed},
			injectSink: true,
			wantCode:   http.StatusConflict,
			wantMirror: 1,
		},
		{
			name:       "无款可冲 ⇒ 409",
			wantCalls:  1,
			body:       `{"order_id":"HTTP-NOREV","status":"paid","payment":{"bill_id":"b_x","channel_ref":"r_x","amount":100}}`,
			sink:       &stubPaymentSink{avail: true, err: service.ErrPaymentNothingToReverse},
			injectSink: true,
			wantCode:   http.StatusConflict,
			wantMirror: 1,
		},
		{
			name:       "状态卡住 ⇒ 500（钱已在库里，须人工看）",
			wantCalls:  1,
			body:       `{"order_id":"HTTP-STUCK","status":"paid","payment":{"bill_id":"b_x","channel_ref":"r_x","amount":100}}`,
			sink:       &stubPaymentSink{avail: true, err: service.ErrPaymentStatusStuck},
			injectSink: true,
			wantCode:   http.StatusInternalServerError,
			wantMirror: 1,
		},
		{
			name:       "别的仓储故障不许被映射成资金判据 ⇒ 500",
			wantCalls:  1,
			body:       `{"order_id":"HTTP-RANDOM","status":"paid","payment":{"bill_id":"b_x","channel_ref":"r_x","amount":100}}`,
			sink:       &stubPaymentSink{avail: true, err: errStubSink},
			injectSink: true,
			wantCode:   http.StatusInternalServerError,
			wantMirror: 1,
		},
	} {
		// 每格一个平台值：本文件与 T-P2-02 那三个用例共用同一个影子库，
		// 平台就是这里的隔离键（镜像计数按它过滤）。
		platform := "tp702-" + strconv.Itoa(i) + "-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		t.Cleanup(func() {
			_ = database.Where("platform = ?", platform).Delete(&model.ExternalOrder{}).Error
			_ = database.Where("platform = ?", platform).Delete(&model.WebhookEvent{}).Error
		})

		t.Run(tc.name, func(t *testing.T) {
			ctrl := NewIntegrationController()
			if tc.injectSink {
				ctrl.integrationService.SetOrderPaymentSink(tc.sink)
			}
			r := setupGinEngine()
			r.POST("/api/integration/order-webhook/:"+middleware.OrderWebhookPlatformParam, ctrl.ReceiveOrderWebhook)

			w := postJSON(r, "/api/integration/order-webhook/"+platform, nil, []byte(tc.body))
			if w.Code != tc.wantCode {
				t.Fatalf("状态码 %d，期望 %d：%s", w.Code, tc.wantCode, w.Body.String())
			}
			if tc.wantInBody != "" && !strings.Contains(w.Body.String(), tc.wantInBody) {
				t.Errorf("响应里没出现 %q：%s", tc.wantInBody, w.Body.String())
			}
			var n int64
			if err := database.Model(&model.ExternalOrder{}).Where("platform = ?", platform).Count(&n).Error; err != nil {
				t.Fatalf("计数失败：%v", err)
			}
			if n != tc.wantMirror {
				t.Errorf("镜像 %d 行，期望 %d（钱记没记上都不许丢单）", n, tc.wantMirror)
			}
			// 次数也是契约：看不懂的钱**根本不该**递到资金腿（0 次），
			// 而载荷合法、只是库里对不上的那些必须递到（1 次，且只一次）。
			if tc.sink != nil {
				if got := len(tc.sink.calls); got != tc.wantCalls {
					t.Errorf("回款腿被叫了 %d 次，期望 %d 次（calls=%+v）", got, tc.wantCalls, tc.sink.calls)
				}
			}
		})
	}
}

// TestReceiveOrderWebhook_PaymentRejectedIsNotSilentlyOK 单独钉一次"不许回 200"。
//
// 上面那张表已经逐格判了码，这一条判的是**方向**：把 bookOrderPayment 的错误吞掉
// （最省事的"让测试变绿"写法）会让渠道以为钱收下了而停止重投 —— 那是把一笔钱
// 从"渠道会重试"变成"只有我们的日志知道"。
func TestReceiveOrderWebhook_PaymentRejectedIsNotSilentlyOK(t *testing.T) {
	database := setupOrderWebhookControllerDB(t)
	platform := "tp702sil" + strconv.FormatInt(time.Now().UnixNano()%1_000_000_000, 10)
	t.Cleanup(func() {
		_ = database.Where("platform = ?", platform).Delete(&model.ExternalOrder{}).Error
		_ = database.Where("platform = ?", platform).Delete(&model.WebhookEvent{}).Error
	})

	ctrl := NewIntegrationController()
	sink := &stubPaymentSink{avail: true, err: service.ErrPaymentInputInvalid}
	ctrl.integrationService.SetOrderPaymentSink(sink)
	r := setupGinEngine()
	r.POST("/api/integration/order-webhook/:"+middleware.OrderWebhookPlatformParam, ctrl.ReceiveOrderWebhook)

	w := postJSON(r, "/api/integration/order-webhook/"+platform, nil,
		[]byte(`{"order_id":"SILENT-1","status":"paid","payment":{"bill_id":"b","channel_ref":"r","amount":1}}`))
	if w.Code == http.StatusOK {
		t.Fatalf("回款腿报了错而接口回 200：渠道会停止重投，这笔钱只剩日志里有记录：%s", w.Body.String())
	}
	if w.Code/100 != 4 {
		t.Errorf("载荷坏这类应当是 4xx（让渠道别重投），实际 %d", w.Code)
	}
}

var errStubSink = &stubError{"存储抖动"}

type stubError struct{ msg string }

func (e *stubError) Error() string { return e.msg }
