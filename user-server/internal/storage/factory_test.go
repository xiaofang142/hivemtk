package storage

import (
	"testing"

	"hivemtk-user/internal/model"
)

func TestValidateLocalEndpoint_RejectsURLShapes(t *testing.T) {
	cases := []struct {
		name     string
		endpoint string
		wantErr  bool
	}{
		{"empty ok", "", false},
		{"absolute path", "/data/uploads", false},
		{"relative default", "./uploads", false},
		{"https url", "https://qiniu.example.com/materials", true},
		{"http url", "http://qiniu.example.com", true},
		{"network path", "//host/share", true},
	}
	for _, tc := range cases {
		err := ValidateLocalEndpoint(tc.endpoint)
		if tc.wantErr && err == nil {
			t.Errorf("%s: 期望拒绝 %q，实际接受", tc.name, tc.endpoint)
		}
		if !tc.wantErr && err != nil {
			t.Errorf("%s: %q 不应被拒: %v", tc.name, tc.endpoint, err)
		}
	}
}

func TestFactory_LocalEndpointURLRejected(t *testing.T) {
	cfg := &model.ObsConfig{Provider: model.ObsProviderLocal, Endpoint: "https://qiniu.example.com/materials"}
	if _, err := Factory(cfg); err == nil {
		t.Fatal("URL 形态 Endpoint 必须被 Factory 拒绝，不得落盘畸形目录（AUD-01）")
	}
}

func TestFactory_LocalEndpointPathAccepted(t *testing.T) {
	cfg := &model.ObsConfig{Provider: model.ObsProviderLocal, Endpoint: "/tmp/audit-obs-local"}
	if _, err := Factory(cfg); err != nil {
		t.Fatalf("合法本地目录被拒: %v", err)
	}
}

func TestObsConfigAccessURL_HttpsAndFailClosed(t *testing.T) {
	cases := []struct {
		name string
		cfg  *model.ObsConfig
		want string
	}{
		{"qiniu no domain fails closed", &model.ObsConfig{Provider: model.ObsProviderQiniu}, "materials/a.png"},
		{"qiniu bare domain gets https", &model.ObsConfig{Provider: model.ObsProviderQiniu, Domain: "qiniu.example.com"}, "https://qiniu.example.com/materials/a.png"},
		{"http domain upgraded", &model.ObsConfig{Provider: model.ObsProviderQiniu, Domain: "http://qiniu.example.com"}, "https://qiniu.example.com/materials/a.png"},
		{"https domain kept", &model.ObsConfig{Provider: model.ObsProviderQiniu, Domain: "https://qiniu.example.com"}, "https://qiniu.example.com/materials/a.png"},
		{"aliyun synthesized https", &model.ObsConfig{Provider: model.ObsProviderAliyun, Bucket: "b", Region: "r"}, "https://b.r.aliyuncs.com/materials/a.png"},
	}
	for _, tc := range cases {
		got := obsConfigAccessURL(tc.cfg, "materials/a.png")
		if got != tc.want {
			t.Errorf("%s: got %q want %q", tc.name, got, tc.want)
		}
	}
}
