package errhttp

// 契约：「记录不存在」和「上游/渠道调用失败」是**两件事**，不能混。
//
// 坑：判定"记录不存在"是按**错误文本**做的（文本含"不存在"或"not found" ⇒ 404），
// 而渠道侧的原文常常自带这些词 ——
// 阿里云短信回 "Specified access key is not found."、腾讯回 "key not found" ——
// 于是"密钥填错了"被翻译成 404「记录不存在」，
// 把操作者引向完全错误的方向：去查记录，而不是去改密钥。
//
// 修法：错误必须**自己声明**它是上游失败（带 5xxx 平台/内部类业务码的 BizError），
// 判定层据此放行；文本判定只留给本地"记录不存在"类错误。
//
// 本文件锁三条腿：
//  1. 上游失败 ⇒ 不是 404；
//  2. 本地"记录不存在" ⇒ 仍然是 404（不能为了修上游误伤本地语义）；
//  3. 没声明上游失败的裸渠道错误 ⇒ 仍然按老路走（**已知缺口**，
//     见文件末尾说明：彻底修需要渠道错误全部带上声明，见 service/sms.go）。

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	bizerr "hivemtk-user/internal/domain/errors"
)

// TestHandleDBError_UpstreamFailureIsNotNotFound 渠道原文含 "not found" 但已声明上游失败 ⇒ 不是 404。
func TestHandleDBError_UpstreamFailureIsNotNotFound(t *testing.T) {
	ctx, rec := newTestCtx(t)

	// 这条错误串模拟渠道原样返回：里面确实有 "not found"，
	// 若判定层只按文本看，它就会被误判成"记录不存在"。
	upstream := bizerr.Wrap(bizerr.CodePlatformUnavail,
		"发送短信失败（渠道不可达或凭证无效）",
		errors.New("Specified access key is not found."))

	handled := HandleDBError(ctx, upstream, "发送草稿")
	if !handled {
		t.Fatal("error should be handled")
	}
	if rec.Code == http.StatusNotFound {
		t.Fatalf("上游/渠道失败不得映射成 404「记录不存在」，实际 %d，body=%s",
			rec.Code, rec.Body.String())
	}
}

// TestHandleDBError_UpstreamFailureWithChineseNotFound 中文场景同理。
func TestHandleDBError_UpstreamFailureWithChineseNotFound(t *testing.T) {
	ctx, rec := newTestCtx(t)

	upstream := bizerr.Wrap(bizerr.CodePlatformUnavail,
		"发送短信失败", errors.New("凭证不存在"))

	HandleDBError(ctx, upstream, "发送草稿")
	if rec.Code == http.StatusNotFound {
		t.Fatalf("上游/渠道失败不得映射成 404，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
}

// TestHandleDBError_LocalNotFoundStill404 本地"记录不存在"仍然是 404。
func TestHandleDBError_LocalNotFoundStill404(t *testing.T) {
	ctx, rec := newTestCtx(t)

	handled := HandleDBError(ctx, errors.New("记录不存在"), "获取短链")
	if !handled {
		t.Fatal("error should be handled")
	}
	if rec.Code != http.StatusNotFound {
		t.Fatalf("本地记录不存在应映射成 404，实际 %d", rec.Code)
	}
}

// TestIsNotFoundError_UpstreamFailureNotMatched 直接锁判定函数本身。
func TestIsNotFoundError_UpstreamFailureNotMatched(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"上游失败-英文原文含 not found",
			bizerr.Wrap(bizerr.CodePlatformUnavail, "渠道失败", errors.New("Specified access key is not found.")),
			false},
		{"上游失败-中文原文含 不存在",
			bizerr.Wrap(bizerr.CodePlatformUnavail, "渠道失败", errors.New("凭证不存在")),
			false},
		{"本地记录不存在", errors.New("记录不存在"), true},
		{"本地 record not found", errors.New("record not found"), true},
		{"普通错误", errors.New("db boom"), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := isNotFoundError(tc.err); got != tc.want {
				t.Errorf("isNotFoundError(%v) = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

// TestIsNotFoundError_WrappedUpstreamStillNotMatched 上游失败被再包一层（fmt.Errorf %w）后仍然不是 not-found。
func TestIsNotFoundError_WrappedUpstreamStillNotMatched(t *testing.T) {
	inner := bizerr.Wrap(bizerr.CodePlatformUnavail, "渠道失败", errors.New("key not found"))
	wrapped := fmt.Errorf("send sms failed: %w", inner)
	if isNotFoundError(wrapped) {
		t.Errorf("包过一层的上游失败仍不得判成 not-found，实际判成了")
	}
}

// TestHandleServiceError_UpstreamFailureMaps502 渠道故障必须回 502，不能压成 400。
// 2026-11-01 API 全量回归实测：POST /api/sms/resend/{id} 在阿里云密钥失效时回
// 400「重发短信失败（渠道不可达或凭证无效）」，而同族 POST /api/sms/send 走
// ErrorFromDB 回 502 —— 同一故障两种状态码，且 400 的语义把运维引向"请求写错了"。
func TestHandleServiceError_UpstreamFailureMaps502(t *testing.T) {
	ctx, rec := newTestCtx(t)

	upstream := bizerr.Wrap(bizerr.CodePlatformUnavail,
		"重发短信失败（渠道不可达或凭证无效）",
		errors.New("aliyun sms error: Specified access key is not found."))

	if !HandleServiceError(ctx, upstream) {
		t.Fatal("error should be handled")
	}
	if rec.Code == http.StatusNotFound {
		t.Fatalf("上游失败不得映射成 404，实际 %d", rec.Code)
	}
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("上游/渠道失败应映射 502，实际 %d，body=%s", rec.Code, rec.Body.String())
	}
}
