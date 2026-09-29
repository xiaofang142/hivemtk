package bridge

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// SSE 是桥接的默认下行形态：capabilities 报 sse_enabled 后扩展端连轮询定时器都不启动，
// 一条「连上了但永远空转」的流在客户端日志里和正常流一模一样。
// 因此缺 channel / 缺 account_id / 桥接不承载的渠道必须在发 200 之前判掉，
// 且不能顺手把账号刷成在线（否则补投门会以为这个配置错误的账号还活着）。

func TestHandleOutboxSSE_RejectsBadParamsBeforeCommittingStream(t *testing.T) {
	srv := startSSEServerWithWindow(t, 40*time.Millisecond, 240*time.Millisecond)
	repo := useRecordingRepo(t)

	cases := []struct {
		name       string
		query      string
		wantPhrase string
	}{
		{"缺 channel", "account_id=acc-sse-valid", "channel required"},
		{"空 account_id", "channel=douyin&account_id=", "account_id required"},
		{"桥接不承载的渠道", "channel=wechat&account_id=acc-sse-valid", "unsupported bridge channel"},
	}
	for _, tc := range cases {
		resp, err := http.Get(srv.URL + "/api/bridge/outbox/sse?" + tc.query)
		if err != nil {
			t.Fatalf("%s: 请求失败: %v", tc.name, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			t.Fatalf("%s: 读响应失败: %v", tc.name, readErr)
		}
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("%s: 期望 400（入参错误），实际 %d: %s", tc.name, resp.StatusCode, string(body))
		}
		// 一旦走进流模式，状态码就固定在 200、错误体再也送不出去 ⇒ Content-Type 是"有没有提前收口"的硬证据。
		if ct := resp.Header.Get("Content-Type"); strings.Contains(ct, "text/event-stream") {
			t.Errorf("%s: 响应已被当作 SSE 流发出（Content-Type=%s），错误无法送达客户端", tc.name, ct)
		}
		if !strings.Contains(string(body), tc.wantPhrase) {
			t.Errorf("%s: 文案应含 %q，实际 %s", tc.name, tc.wantPhrase, string(body))
		}
	}

	if n := repo.touches.Load(); n != 0 {
		t.Errorf("被拒的三条请求一条都不许刷在线位，实际刷了 %d 次", n)
	}

	// 正控制：合法入参必须仍然成流，否则上面三条可以靠"一律拒绝"蒙绿。
	okResp, err := http.Get(srv.URL + "/api/bridge/outbox/sse?channel=douyin&account_id=acc-sse-valid")
	if err != nil {
		t.Fatalf("正控制请求失败: %v", err)
	}
	defer okResp.Body.Close()
	okBody, err := io.ReadAll(okResp.Body)
	if err != nil {
		t.Fatalf("读正控制流失败: %v", err)
	}
	if okResp.StatusCode != http.StatusOK {
		t.Fatalf("正控制期望 200，实际 %d", okResp.StatusCode)
	}
	if !strings.Contains(okResp.Header.Get("Content-Type"), "text/event-stream") {
		t.Errorf("正控制 Content-Type 应为 SSE 流，实际 %s", okResp.Header.Get("Content-Type"))
	}
	if !strings.Contains(string(okBody), "retry:") {
		t.Errorf("正控制应收到 retry: 帧，实际 body=%q", string(okBody))
	}
	if n := repo.touches.Load(); n == 0 {
		t.Errorf("正控制的合法流必须刷在线位（否则上面那条 0 次断言没有对照组）")
	}
}

// 渠道别名归一发生在入参校验之前：扩展端历史上传 douyin_web，
// 归一后它和 douyin 是同一条订阅键，绝不能被判成"桥接不承载的渠道"。
func TestHandleOutboxSSE_AliasChannelStillAcceptedAfterValidation(t *testing.T) {
	srv := startSSEServerWithWindow(t, 40*time.Millisecond, 240*time.Millisecond)

	for _, ch := range []string{"douyin_web", "xhs", "kuaishou_web", "xianyu_web", "tiktok_web"} {
		resp, err := http.Get(srv.URL + "/api/bridge/outbox/sse?channel=" + ch + "&account_id=acc-sse-alias")
		if err != nil {
			t.Fatalf("%s: 请求失败: %v", ch, err)
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			t.Fatalf("%s: 读流失败: %v", ch, readErr)
		}
		if resp.StatusCode != http.StatusOK {
			t.Errorf("别名渠道 %s 应成流（200），实际 %d: %s", ch, resp.StatusCode, string(body))
			continue
		}
		if !strings.Contains(string(body), "retry:") {
			t.Errorf("别名渠道 %s 未收到 retry: 帧，body=%q", ch, string(body))
		}
	}
}
