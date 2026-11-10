package connector

// grasp.go TB-06b：管家婆辉煌/财贸/工贸 ERP 方言实例（preset 形态）。
//
// 证据级声明（v5 二次审核口径，如实不夸大）：
//   - 云辉煌/网上管家婆有官方 Open API（REST+APPKEY，来源见
//     docs/research/INTENT-TOOL-UNIFIED-BASE-DESIGN-2026-11.md §五）；
//   - 辉煌单机版（V8.5 等）无公开 API，本地方言面向「云版/本地网关 127.0.0.1:8088
//     部署形态」的 preset（integration/templates/presets.go grasp_erp_default）；
//   - 签名拼接口径=preset AuthConfig（server_no/server_key/sign_method=MD5）形态的
//     **V 级证据**（厂商文档面本轮未逐字取证），signMethod 字段可配，部署实测前
//     不宣称可用（🔵）；方言结构本身（鉴权→请求变形→归一化）与签名算法解耦，
//     实测后只改 signInput 不改调用链。

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// GraspDialect 管家婆 HMAC 方言（MD5 形态，sign_method 可配）。
type GraspDialect struct{}

func (GraspDialect) ID() string { return "grasp_hmac" }

// BuildRequest POST form：server_no + 业务参数 + sign=MD5(server_no+server_key+path) 大写。
// Auth 映射：Type=hmac / Key=server_key / Header=server_no（preset AuthConfig 同构）。
func (GraspDialect) BuildRequest(ctx context.Context, cfg ConnectorConfig, ep EndpointConfig, args map[string]any) (*http.Request, error) {
	fullURL := strings.TrimRight(cfg.BaseURL, "/") + ep.Path

	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	form := url.Values{}
	form.Set("server_no", cfg.Auth.Header)
	for _, k := range keys {
		form.Set(epParamName(ep, k), fmt.Sprintf("%v", args[k]))
	}
	form.Set("sign_method", "MD5")
	signInput := cfg.Auth.Header + cfg.Auth.Key + ep.Path
	form.Set("sign", strings.ToUpper(md5Hex(signInput)))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req, nil
}

// NormalizeResponse 与 generic 同口径（JSON 对象/数组/原文三形态）。
func (GraspDialect) NormalizeResponse(body []byte, statusCode int) (map[string]any, error) {
	return NormalizeJSON(body)
}

func md5Hex(s string) string {
	sum := md5.Sum([]byte(s))
	return hex.EncodeToString(sum[:])
}

// Ping 连通性自检：GET base URL 根——任一 HTTP 响应=网络可达（含 401/403/404，
// 连通性与鉴权分离口径）；网络错误=不可达。凭证不进错误文本。
func (e *Executor) Ping(ctx context.Context, cfg ConnectorConfig) (reachable bool, statusCode int, detail string, err error) {
	if cfg.BaseURL == "" {
		return false, 0, "base_url 未配置", nil
	}
	if e.baseURLValidator != nil {
		if verr := e.baseURLValidator(cfg.BaseURL); verr != nil {
			// 基址不合法=配置错误（SSRF 防线），走 err 面（调用方 400）；
			// 网络不可达才是探针结果（reachable=false）。
			return false, 0, verr.Error(), verr
		}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(cfg.BaseURL, "/")+"/", nil)
	if err != nil {
		return false, 0, err.Error(), nil
	}
	resp, err := e.http.Do(req)
	if err != nil {
		return false, 0, "网络不可达", nil
	}
	defer func() { _ = resp.Body.Close() }()
	return true, resp.StatusCode, "reachable", nil
}

// init 注册管家婆方言（preset 形态；真实凭证🔵）。
func init() {
	Register(GraspDialect{})
}
