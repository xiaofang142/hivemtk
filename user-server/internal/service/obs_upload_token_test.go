package service

import (
	"context"
	"crypto/hmac"
	"crypto/sha1"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/tenant"
	"hivemtk-user/internal/pkg/testutil"
)

func TestObsUploadBoundary_ScopeAndSigning(t *testing.T) {
	previous := tenant.CurrentMode()
	tenant.SetMode(tenant.ModeOn)
	t.Cleanup(func() { tenant.SetMode(previous) })
	database := testutil.NewTestDB(t, &model.ObsConfig{})
	ctx := tenant.OnlyTenant(context.Background(), 18)
	cfg := &model.ObsConfig{TenantID: 18, Name: "合成上传配置", Provider: model.ObsProviderQiniu,
		AccessKey: "test-ak", SecretKey: "test-sk", Bucket: "tenant-18-bucket", Region: "z0",
		Domain: "https://files.example.test/assets/", Status: model.ObsStatusActive, IsDefault: true}
	if err := database.WithContext(ctx).Create(cfg).Error; err != nil {
		t.Fatal(err)
	}
	otherCtx := tenant.OnlyTenant(context.Background(), 19)
	other := &model.ObsConfig{TenantID: 19, Name: "另租户合成配置", Provider: model.ObsProviderQiniu,
		AccessKey: "other-ak", SecretKey: "other-sk", Bucket: "tenant-19-bucket", Region: "z1",
		Domain: "files-19.example.test", Status: model.ObsStatusActive, IsDefault: true}
	if err := database.WithContext(otherCtx).Create(other).Error; err != nil {
		t.Fatal(err)
	}
	key := "chat/2026/11/synthetic.png"
	result, err := BuildQiniuUploadToken(ctx, key, 20<<20, time.Hour)
	if err != nil || result == nil {
		t.Fatalf("合法租户签发失败：%v", err)
	}
	if result.PublicURL != "https://files.example.test/assets/"+key {
		t.Errorf("已有HTTPS协议不能重复拼接：%s", result.PublicURL)
	}
	if result.UploadURL != "https://up.qiniup.com" || result.ExpiresIn != 3600 {
		t.Error("既有区域或有效期语义改变")
	}
	parts := strings.Split(result.Token, ":")
	if len(parts) != 3 || parts[0] != "test-ak" {
		t.Fatal("上传签名协议或租户AK错误")
	}
	mac := hmac.New(sha1.New, []byte("test-sk"))
	_, _ = mac.Write([]byte(parts[2]))
	if parts[1] != base64.URLEncoding.EncodeToString(mac.Sum(nil)) {
		t.Error("七牛HMAC-SHA1签名不匹配")
	}
	data, err := base64.URLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatal(err)
	}
	var policy struct {
		Scope    string `json:"scope"`
		Deadline int64  `json:"deadline"`
		Limit    int64  `json:"fsizeLimit"`
	}
	if err := json.Unmarshal(data, &policy); err != nil {
		t.Fatal(err)
	}
	if policy.Scope != "tenant-18-bucket:"+key || policy.Limit != 20<<20 || policy.Deadline < time.Now().Add(59*time.Minute).Unix() {
		t.Error("签名策略改变或跨租户")
	}
	otherResult, err := BuildQiniuUploadToken(otherCtx, key, 20<<20, time.Hour)
	if err != nil || otherResult == nil || !strings.HasPrefix(otherResult.Token, "other-ak:") {
		t.Fatalf("另租户不能借第18租户凭证：%v", err)
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	for _, tc := range []struct {
		name string
		ctx  context.Context
	}{
		{name: "空上下文"}, {name: "缺SaaS租户", ctx: context.Background()},
		{name: "系统豁免", ctx: tenant.WithSystemScope(ctx)}, {name: "已取消", ctx: canceled},
	} {
		t.Run(tc.name, func(t *testing.T) {
			defer func() {
				if p := recover(); p != nil {
					t.Errorf("必须返回错误而非崩溃：%v", p)
				}
			}()
			if result, err := BuildQiniuUploadToken(tc.ctx, key, 20<<20, time.Hour); err == nil || result != nil {
				t.Error("未知或取消范围不能获得上传凭证")
			}
		})
	}
}

func TestObsUploadBoundary_InvalidInputsCannotSign(t *testing.T) {
	database := testutil.NewTestDB(t, &model.ObsConfig{})
	ctx := tenant.OnlyTenant(context.Background(), tenant.DefaultTenantID)
	cfg := &model.ObsConfig{TenantID: tenant.DefaultTenantID, Name: "合成上传配置", Provider: model.ObsProviderQiniu,
		AccessKey: "test-ak", SecretKey: "test-sk", Bucket: "test-bucket", Domain: "files.example.test", Status: model.ObsStatusActive, IsDefault: true}
	if err := database.WithContext(ctx).Create(cfg).Error; err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, key string
		size      int64
		ttl       time.Duration
	}{
		{name: "空key", size: 1, ttl: time.Hour}, {name: "绝对路径", key: "/chat/file.png", size: 1, ttl: time.Hour},
		{name: "父目录", key: "chat/../file.png", size: 1, ttl: time.Hour}, {name: "反斜线", key: "chat\\file.png", size: 1, ttl: time.Hour},
		{name: "查询注入", key: "chat/file.png?x=1", size: 1, ttl: time.Hour}, {name: "零大小", key: "chat/file.png", ttl: time.Hour},
		{name: "负大小", key: "chat/file.png", size: -1, ttl: time.Hour}, {name: "超既有上限", key: "chat/file.png", size: (20 << 20) + 1, ttl: time.Hour},
		{name: "零期限", key: "chat/file.png", size: 1}, {name: "超既有期限", key: "chat/file.png", size: 1, ttl: time.Hour + time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if result, err := BuildQiniuUploadToken(ctx, tc.key, tc.size, tc.ttl); err == nil || result != nil {
				t.Error("不合法范围仍签发了上传凭证")
			}
		})
	}
}

func TestObsUploadBoundary_PublicURLAndObjectKey(t *testing.T) {
	for _, tc := range []struct {
		name, domain, key, expected string
	}{
		{name: "存量裸域名", domain: "files.example.test", key: "chat/file.png", expected: "https://files.example.test/chat/file.png"},
		{name: "已有协议与前缀", domain: "https://files.example.test/assets/", key: "chat/file.png", expected: "https://files.example.test/assets/chat/file.png"},
		{name: "中文对象路径", domain: "files.example.test", key: "chat/文件.png", expected: "https://files.example.test/chat/%E6%96%87%E4%BB%B6.png"},
		{name: "空公开地址", key: "chat/file.png"},
		{name: "明文HTTP", domain: "http://files.example.test", key: "chat/file.png"},
		{name: "地址含凭证", domain: "https://user:password@files.example.test", key: "chat/file.png"},
		{name: "地址含查询", domain: "https://files.example.test?token=synthetic", key: "chat/file.png"},
		{name: "地址含fragment", domain: "https://files.example.test#part", key: "chat/file.png"},
		{name: "地址含控制字符", domain: "files.example.test\n", key: "chat/file.png"},
		{name: "无主机", domain: "https:///assets", key: "chat/file.png"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := uploadPublicURL(tc.domain, tc.key)
			if tc.expected == "" {
				if err == nil || got != "" {
					t.Fatal("不合法公开地址必须拒绝，不能产生错误或带凭据的URL")
				}
				return
			}
			if err != nil || got != tc.expected {
				t.Fatalf("合法公开地址改变：%q，错误%v", got, err)
			}
		})
	}
	for _, key := range []string{"chat/./file.png", "chat//file.png", "../file.png", "chat/file.png#x", "chat/%2e%2e/file.png", "chat/a:b.png", "chat/file\n.png", "chat/file .png"} {
		if validUploadObjectKey(key) {
			t.Errorf("不合法对象路径被接受：%q", key)
		}
	}
	if !validUploadObjectKey("chat/文件.png") {
		t.Fatal("合法中文对象路径不应被误拒绝")
	}
}
