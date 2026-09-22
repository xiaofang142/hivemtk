// email_open_pixel_cron_test.go 群发路径的打开追踪像素出口：与单封路径同一档 fail-open。
package cron

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

type fakePixelSigner struct {
	url      string
	err      error
	calls    int
	gotEmail string
	gotJobID string
}

func (f *fakePixelSigner) GenerateOpenPixelURL(_ context.Context, email, jobID string) (string, error) {
	f.calls++
	f.gotEmail = email
	f.gotJobID = jobID
	return f.url, f.err
}

const cronPixelURL = "https://crm.example.com/api/email/track/open/abc.def.png"

func pixelRowFixture(t *testing.T, signer *fakePixelSigner) rowFixture {
	t.Helper()
	f := newRowFixture(t)
	f.deps.pixel = signer
	return f
}

// TestDeliverEmailListRowCarriesOpenPixel 群发的正文必须带像素，且归属到本行的 job。
//
// 群发才是打开统计唯一有量的那条路（一天一波、每次上万封），单封路径带而它不带的话，
// 管理端看到的"打开数"会永远只反映即时发送那一小撮。
func TestDeliverEmailListRowCarriesOpenPixel(t *testing.T) {
	signer := &fakePixelSigner{url: cronPixelURL}
	f := pixelRowFixture(t, signer)
	row := emailRowFixture()

	deliverEmailListRow(context.Background(), row, f.deps)

	if len(f.mails.sends) != 1 {
		t.Fatalf("投递 %d 次，期望 1 次", len(f.mails.sends))
	}
	body := f.mails.sends[0].body
	if !strings.Contains(body, "<img") || !strings.Contains(body, cronPixelURL) {
		t.Errorf("群发正文没有像素：%q", body)
	}
	if !strings.Contains(body, "退订此类邮件") {
		t.Errorf("加了像素把退订页脚挤掉了：%q", body)
	}
	if signer.calls != 1 {
		t.Errorf("像素签发 %d 次，期望 1 次", signer.calls)
	}
	if signer.gotJobID != row.JobsID.String() {
		t.Errorf("像素归属 jobID = %q，期望本行的 %s", signer.gotJobID, row.JobsID)
	}
}

// TestDeliverEmailListRowWithoutPixelLinker 没装签发器 ⇒ 正文里不许出现 img。
func TestDeliverEmailListRowWithoutPixelLinker(t *testing.T) {
	f := newRowFixture(t)
	f.deps.pixel = nil
	row := emailRowFixture()

	deliverEmailListRow(context.Background(), row, f.deps)

	if len(f.mails.sends) != 1 {
		t.Fatalf("投递 %d 次，期望 1 次", len(f.mails.sends))
	}
	if strings.Contains(f.mails.sends[0].body, "<img") {
		t.Errorf("没签发器仍往正文塞了像素：%q", f.mails.sends[0].body)
	}
}

// TestDeliverEmailListRowFailOpenOnPixelError 缺 EMAIL_TRACKING_SECRET ⇒ 信照发、退订照带，只是没统计。
func TestDeliverEmailListRowFailOpenOnPixelError(t *testing.T) {
	signer := &fakePixelSigner{err: errors.New("EMAIL_TRACKING_SECRET 未配置")}
	f := pixelRowFixture(t, signer)
	row := emailRowFixture()

	deliverEmailListRow(context.Background(), row, f.deps)

	if len(f.mails.sends) != 1 {
		t.Fatalf("像素签发失败把群发拦住了，投递 %d 次", len(f.mails.sends))
	}
	body := f.mails.sends[0].body
	if strings.Contains(body, "<img") {
		t.Errorf("签发失败仍写了像素：%q", body)
	}
	if !strings.Contains(body, "退订此类邮件") {
		t.Errorf("像素失败连带丢了退订页脚：%q", body)
	}
}

// TestEmailListCronWiresOpenPixelSigner 生产装配必须给 deps.pixel 一个真签发器。
//
// 这是一条静态锁，理由与排水装配那两条同类：`pixel` 只在测试里被赋值的话，
// 线上永远是"签不出像素"这条路，而它的表现是打开数 0 —— 与"没人打开"一模一样，看不出来。
// 摘掉 emaillistcron.go 里那一行必须让本用例红。
func TestEmailListCronWiresOpenPixelSigner(t *testing.T) {
	src, err := os.ReadFile("emaillistcron.go")
	if err != nil {
		t.Fatalf("读不到被测文件: %v", err)
	}
	if n := strings.Count(string(src), "\t\tpixel:"); n != 1 {
		t.Errorf("生产装配里 pixel 赋值出现 %d 次，期望恰好 1 次 ⇒ 群发像素签发器未接线或被摘", n)
	}
	if !strings.Contains(string(src), "pixel:       service.NewEmailOpenTrackerService(nil, nil),") {
		t.Error("pixel 接的不是 EmailOpenTrackerService ⇒ 群发像素签发器接错实现")
	}
}
