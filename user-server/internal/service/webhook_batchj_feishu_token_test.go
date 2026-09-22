package service

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/httpclient"

	"gorm.io/gorm"
)

// batchJFeishuTokenTransport 只回答「取 tenant_access_token」这一跳。
// 换整个 httpclient.Client 而不是给 open.feishu.cn 加 base 缝：仓内非测试代码里该域有四处
// （feishu.go 的取凭证腿与发送腿、channel_media.go、kb_connectors.go），只为一条腿加缝会留下
// 「同域两个写法」的别扭；钉钉那批加缝是因为它整条媒体链只有两处、且没有别的注入办法。
type batchJFeishuTokenTransport struct {
	bodies []string
	calls  int
}

func (tr *batchJFeishuTokenTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	body := `{"code":0,"msg":"ok","tenant_access_token":"","expire":0}`
	if tr.calls < len(tr.bodies) {
		body = tr.bodies[tr.calls]
	}
	tr.calls++
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Body:       io.NopCloser(bytes.NewReader([]byte(body))),
		Header:     make(http.Header),
	}, nil
}

func batchJFeishuAccount(t *testing.T, database *gorm.DB, label string) *model.FeishuAccount {
	t.Helper()
	acc := &model.FeishuAccount{
		AccountName: label + "-" + time.Now().Format("150405.000000"),
		AppID:       "cli-" + label + "-" + time.Now().Format("150405.000000"),
		AppSecret:   "sec-" + label,
		Status:      1,
	}
	if err := database.Create(acc).Error; err != nil {
		t.Fatalf("create feishu account: %v", err)
	}
	return acc
}

// TestBatchJ_FeishuTokenLegShapes 打的是 §17.7 结果 C 那一格：`code==0` 但
// `tenant_access_token` 为空。飞书是这批里唯一**连判定都没有**的站点（钉钉/QQ 有 `== ""` 那一句），
// 也是六家里唯一把空凭证连同未来过期时间写回账号行的。
//
// 判据分两层，缺一条就会假绿：
//   - 报错本身（否则调用方拿着 "" 继续发一次真请求，现场只剩平台侧错误码，根因在上一跳）；
//   - **缓存里没被写入未来过期时间**（`acc.AccessToken==""` 会让下次仍走取凭证腿，所以「不缓存」
//     只看 calls 数是抓不住的，必须看 TokenExpires）。
func TestBatchJ_FeishuTokenLegShapes(t *testing.T) {
	database := setupFeishuTestDB(t)
	svc := NewFeishuIntegrationService(database)
	orig := httpclient.Client
	t.Cleanup(func() { httpclient.Client = orig })

	t.Run("code=0 且 token 为空：报错且不得写未来过期时间", func(t *testing.T) {
		tr := &batchJFeishuTokenTransport{bodies: []string{
			`{"code":0,"msg":"success","tenant_access_token":"","expire":7200}`,
			`{"code":0,"msg":"success","tenant_access_token":"","expire":7200}`,
		}}
		httpclient.Client = &http.Client{Transport: tr}
		acc := batchJFeishuAccount(t, database, "batchJ-empty")

		for i := 0; i < 2; i++ {
			tk, err := svc.getAccessToken(context.Background(), acc)
			if err == nil {
				t.Fatalf("第 %d 次取用：空凭证必须报错，got token=%q", i+1, tk)
			}
			if tk != "" {
				t.Errorf("第 %d 次取用：报错时不得回凭证，got %q", i+1, tk)
			}
			if !strings.Contains(err.Error(), "code=0") || !strings.Contains(err.Error(), "msg=success") {
				t.Errorf("第 %d 次取用：根因（平台确实答了 code=0）必须留在错误里，got %q", i+1, err)
			}
		}
		if tr.calls != 2 {
			t.Errorf("空凭证不得进缓存：两次取用应各打一次，got calls=%d", tr.calls)
		}
		if acc.TokenExpires != nil {
			t.Errorf("内存里的账号不得被写入未来过期时间（那是「这串凭证可用到几点」的意思），got %v", *acc.TokenExpires)
		}
		var row model.FeishuAccount
		if err := database.Where("id = ?", acc.ID).First(&row).Error; err != nil {
			t.Fatalf("读回账号行: %v", err)
		}
		if row.TokenExpires != nil {
			t.Errorf("落库侧同理：空凭证那一趟不该 UpdateAccount，got expires=%v", *row.TokenExpires)
		}
	})

	t.Run("拿到凭证：写缓存且第二次不再打网络（正例腿）", func(t *testing.T) {
		tr := &batchJFeishuTokenTransport{bodies: []string{
			`{"code":0,"msg":"success","tenant_access_token":"tk-batchJ","expire":7200}`,
		}}
		httpclient.Client = &http.Client{Transport: tr}
		acc := batchJFeishuAccount(t, database, "batchJ-ok")

		for i := 0; i < 2; i++ {
			tk, err := svc.getAccessToken(context.Background(), acc)
			if err != nil {
				t.Fatalf("第 %d 次取用不该报错: %v", i+1, err)
			}
			if tk != "tk-batchJ" {
				t.Fatalf("第 %d 次取用 got %q", i+1, tk)
			}
		}
		if tr.calls != 1 {
			t.Errorf("第二次应命中账号行里的缓存，got calls=%d", tr.calls)
		}
		if acc.TokenExpires == nil {
			t.Fatal("取到凭证必须写下过期时间，否则每条消息都重新取一次")
		}
		// 过期时间要**早于**官方 7200s：实现留了 300s 提前量，防"取到时已过期"的边界。
		if left := time.Until(*acc.TokenExpires); left < 6600*time.Second || left > 7200*time.Second {
			t.Errorf("缓存有效期应落在 6600~7200s（含 300s 提前量），got %v", left)
		}
	})
}
