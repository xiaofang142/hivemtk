package service

import (
	"context"
	"testing"

	"hivemtk-user/internal/browser_automation/model"
	"hivemtk-user/internal/browser_automation/repository"
)

func TestNormalizeOutreachURL(t *testing.T) {
	cases := []struct {
		in   string
		want string
		ok   bool
	}{
		{"https://X.Test/p/1#frag", "https://x.test/p/1", true},
		{"HTTPS://X.Test/p/1/", "https://x.test/p/1", true},
		{"https://x.test/p/1?a=1&b=2", "https://x.test/p/1?a=1&b=2", true},
		{"not a url", "", false},
		{"", "", false},
	}
	for _, c := range cases {
		got, ok := normalizeOutreachURL(c.in)
		if ok != c.ok || got != c.want {
			t.Errorf("normalizeOutreachURL(%q) = (%q,%v)，want (%q,%v)", c.in, got, ok, c.want, c.ok)
		}
	}
}

// fakeOutreachDedupeRepo 内存替身：命中可控。
type fakeOutreachDedupeRepo struct {
	repository.BrowserOutreachDedupeRepository
	hits    map[string]*model.BrowserOutreachDedupe
	records []*model.BrowserOutreachDedupe
}

func outreachKey(platform, url, action, hash string) string {
	return platform + "\x00" + url + "\x00" + action + "\x00" + hash
}

func (f *fakeOutreachDedupeRepo) FindOutreachHit(_ context.Context, platform, targetURL, action, copyHash string) (*model.BrowserOutreachDedupe, error) {
	return f.hits[outreachKey(platform, targetURL, action, copyHash)], nil
}

func (f *fakeOutreachDedupeRepo) RecordOutreachSend(_ context.Context, rec *model.BrowserOutreachDedupe) error {
	f.records = append(f.records, rec)
	return nil
}

func TestCheckOutreachDedupeHit(t *testing.T) {
	repo := &fakeOutreachDedupeRepo{hits: map[string]*model.BrowserOutreachDedupe{
		outreachKey("xiaohongshu", "https://x.test/p/1", "post_comment", "abc123"): {TaskID: 7},
	}}
	hit, key, err := checkOutreachDedupe(context.Background(), repo, "xiaohongshu", "https://X.Test/p/1#frag", "post_comment", "abc123")
	if err != nil {
		t.Fatalf("check 不应报错，got %v", err)
	}
	if !hit {
		t.Fatal("同一目标归一化后应命中")
	}
	if key.Platform != "xiaohongshu" || key.TargetURL != "https://x.test/p/1" || key.CopyHash != "abc123" {
		t.Fatalf("key 应为归一化后四元组，got %+v", key)
	}
}

func TestCheckOutreachDedupeMiss(t *testing.T) {
	repo := &fakeOutreachDedupeRepo{hits: map[string]*model.BrowserOutreachDedupe{}}
	hit, _, err := checkOutreachDedupe(context.Background(), repo, "xiaohongshu", "https://x.test/p/2", "post_comment", "abc123")
	if err != nil {
		t.Fatalf("check 不应报错，got %v", err)
	}
	if hit {
		t.Fatal("未发送过应未命中")
	}
}

func TestCheckOutreachDedupeBadURLSkips(t *testing.T) {
	repo := &fakeOutreachDedupeRepo{hits: map[string]*model.BrowserOutreachDedupe{}}
	hit, _, err := checkOutreachDedupe(context.Background(), repo, "xiaohongshu", "not a url", "post_comment", "abc123")
	if err != nil {
		t.Fatalf("非法 URL 应跳过而非报错，got %v", err)
	}
	if hit {
		t.Fatal("非法 URL 应跳过（未命中）")
	}
}
