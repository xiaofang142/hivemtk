package config

import (
	"net/http"
	"testing"
)

func TestGetProxyTransport_ReusesSingletonAndKeepsProxyDecisions(t *testing.T) {
	prev := GetAppConfig()
	defer func() {
		SetAppConfig(&prev)
		proxyTransportMu.Lock()
		proxyTransport, proxyTransportKey = nil, ""
		proxyTransportMu.Unlock()
	}()

	empty := AppConfig{}
	SetAppConfig(&empty)
	for _, k := range []string{"HTTP_PROXY", "HTTPS_PROXY", "http_proxy", "https_proxy"} {
		t.Setenv(k, "")
	}

	a := GetProxyTransport()
	b := GetProxyTransport()
	if a != b {
		t.Error("同一配置下两次调用返回不同实例 ⇒ 每次调用都会带走一组永不超期的空闲连接")
	}
	if a.IdleConnTimeout <= 0 {
		t.Errorf("IdleConnTimeout=%v，必须 >0：0 表示空闲连接永不过期", a.IdleConnTimeout)
	}
	if a.Proxy != nil {
		t.Error("无代理配置却装了 Proxy 决策")
	}

	// 环境变量分支：换了分支必须换实例，且带上 ProxyFromEnvironment
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:8080")
	e := GetProxyTransport()
	if e == a {
		t.Error("切到 env 代理分支仍复用直连实例 ⇒ 代理开关失效")
	}
	if e.Proxy == nil {
		t.Fatal("env 代理分支没有装 Proxy 决策")
	}
	req, err := http.NewRequest("GET", "https://api.telegram.org/bot123/sendMessage", nil)
	if err != nil {
		t.Fatalf("构造请求: %v", err)
	}
	if u, perr := e.Proxy(req); perr != nil || u == nil || u.String() != "http://127.0.0.1:8080" {
		t.Errorf("env 分支代理决策=%v err=%v want http://127.0.0.1:8080", u, perr)
	}
	t.Setenv("HTTPS_PROXY", "")
	if back := GetProxyTransport(); back == e {
		t.Error("代理配置变了却仍复用旧 Transport ⇒ 缓存把决策冻住了")
	}

	// config.yaml 分支：优先级高于环境变量，且按地址分键
	cfgWithProxy := AppConfig{}
	cfgWithProxy.Proxy = ProxyConfig{Enabled: true, HTTPSProxy: "http://127.0.0.1:7890", HTTPProxy: "http://127.0.0.1:7890"}
	SetAppConfig(&cfgWithProxy)
	cfgT := GetProxyTransport()
	if cfgT == a || cfgT == e {
		t.Error("config.yaml 代理分支复用了别的分支实例")
	}
	if cfgT.Proxy == nil {
		t.Fatal("config.yaml 代理分支没有装 Proxy 决策")
	}
	httpsReq, _ := http.NewRequest("GET", "https://api.telegram.org/x", nil)
	u, uerr := cfgT.Proxy(httpsReq)
	if uerr != nil || u == nil || u.String() != "http://127.0.0.1:7890" {
		t.Errorf("https 走代理=%v err=%v want http://127.0.0.1:7890", u, uerr)
	}
}
