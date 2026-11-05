// platform_publish_test.go WordPress XML-RPC 与通用 Webhook 的真实发布契约。
package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"hivemtk-user/internal/geo/dto"
	"hivemtk-user/internal/geo/model"
	"hivemtk-user/internal/geo/repository"
	"hivemtk-user/internal/pkg/testutil"
)

func platformTestService(t *testing.T) (*PlatformService, repository.GeoArticleRepository, repository.GeoPublishRecordRepository) {
	t.Helper()
	db := testutil.NewTestDB(t, &model.GeoPlatformAccount{}, &model.GeoPublishRecord{}, &model.GeoArticle{})
	if db == nil {
		t.Fatal("测试库不可达：平台发布契约判不了")
	}
	accountRepo := repository.NewGeoPlatformAccountRepositoryWithDB(db)
	recordRepo := repository.NewGeoPublishRecordRepositoryWithDB(db)
	articleRepo := repository.NewGeoArticleRepositoryWithDB(db)
	return NewPlatformService(accountRepo, recordRepo, articleRepo), articleRepo, recordRepo
}

func TestPlatformService_PublishWordPress(t *testing.T) {
	t.Setenv("GEO_ALLOW_PRIVATE_PUBLISH_HOSTS", "1")
	var gotBody string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		gotBody = string(raw)
		w.Header().Set("Content-Type", "text/xml")
		_, _ = w.Write([]byte(`<?xml version="1.0"?><methodResponse><params><param><value><string>42</string></value></param></params></methodResponse>`))
	}))
	defer srv.Close()

	svc, articleRepo, recordRepo := platformTestService(t)
	ctx := context.Background()
	if _, err := svc.SaveAccount(ctx, &dto.SavePlatformAccountRequest{
		Platform: "wordpress", AccountName: "main",
		Credentials: map[string]string{
			"endpoint": srv.URL + "/xmlrpc.php",
			"username": "editor",
			"password": "app-password",
		},
	}); err != nil {
		t.Fatalf("保存 WordPress 账号失败: %v", err)
	}
	article := &model.GeoArticle{Title: "GEO 指南", Content: "<p>正文</p>", Keyword: "geo"}
	if err := articleRepo.Create(ctx, article); err != nil {
		t.Fatalf("写文章失败: %v", err)
	}

	resp, err := svc.Publish(ctx, &dto.PublishRequest{ArticleID: article.ID, Platform: "wordpress"})
	if err != nil {
		t.Fatalf("WordPress 发布失败: %v", err)
	}
	if resp.Status != PublishStatusCopied || !strings.Contains(resp.PublishedURL, "?p=42") {
		t.Fatalf("发布结果不对: %+v", resp)
	}
	for _, want := range []string{"wp.newPost", "editor", "app-password", "GEO 指南", "正文"} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("XML-RPC 请求缺少 %q: %s", want, gotBody)
		}
	}
	list, total, err := recordRepo.GetList(ctx, article.ID, "wordpress", 1, 10)
	if err != nil || total != 1 || list[0].Status != PublishStatusCopied {
		t.Fatalf("发布记录未落库: total=%d err=%v", total, err)
	}
}

func TestPlatformService_PublishWebhookSigned(t *testing.T) {
	t.Setenv("GEO_ALLOW_PRIVATE_PUBLISH_HOSTS", "1")
	const secret = "s3cr3t"
	var gotSig string
	var gotPayload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write(raw)
		gotSig = r.Header.Get("X-GEO-Signature")
		_ = json.Unmarshal(raw, &gotPayload)
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if gotSig != want {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = w.Write([]byte(`{"url":"https://published.example/post/1"}`))
	}))
	defer srv.Close()

	svc, articleRepo, _ := platformTestService(t)
	ctx := context.Background()
	if _, err := svc.SaveAccount(ctx, &dto.SavePlatformAccountRequest{
		Platform: "webhook", AccountName: "cms",
		Credentials: map[string]string{"url": srv.URL + "/hook", "secret": secret},
	}); err != nil {
		t.Fatalf("保存 Webhook 账号失败: %v", err)
	}
	article := &model.GeoArticle{Title: "Webhook 测试", Content: "正文"}
	if err := articleRepo.Create(ctx, article); err != nil {
		t.Fatalf("写文章失败: %v", err)
	}

	resp, err := svc.Publish(ctx, &dto.PublishRequest{ArticleID: article.ID, Platform: "webhook"})
	if err != nil {
		t.Fatalf("Webhook 发布失败: %v", err)
	}
	if resp.PublishedURL != "https://published.example/post/1" {
		t.Fatalf("未采用远端返回 URL: %+v", resp)
	}
	art, _ := gotPayload["article"].(map[string]any)
	if art == nil || art["title"] != "Webhook 测试" {
		t.Fatalf("Webhook 负载不对: %+v", gotPayload)
	}
	if !strings.HasPrefix(gotSig, "sha256=") {
		t.Fatalf("缺少 HMAC 签名: %q", gotSig)
	}
}

func TestPlatformService_CapabilitiesHonest(t *testing.T) {
	svc, _, _ := platformTestService(t)
	infos := svc.ListPlatforms(context.Background())
	byName := map[string]dto.PlatformInfo{}
	for _, p := range infos {
		byName[p.Name] = p
	}
	for _, name := range []string{"github_readme", "github_blog", "wordpress", "webhook"} {
		if byName[name].Capability != "real_api" {
			t.Fatalf("%s 能力标记应为 real_api，实际 %q", name, byName[name].Capability)
		}
	}
	if byName["medium"].Capability != "manual" {
		t.Fatalf("Medium 没有开放写接口，应标 manual，实际 %q", byName["medium"].Capability)
	}
}
