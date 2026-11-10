package connector

// kuaidi100.go TB-06b：快递100 方言实例（签名类平台 preset 样板）。
//
// 公开文档面（来源见 docs/research/API-CHANNEL-PUBLIC-PROTOCOLS-2026-11.md）：
// 实时查询=POST form（param=JSON 串+customer+sign=MD5(param+key+customer) 大写）。
// 本方言只实现「签名形态」，真实 customer/key 凭证归🔵外部腿；preset 数据
// （端点/参数映射/能力标签）与方言解耦——换平台只换 preset。

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
)

// Kuaidi100Dialect 快递100 实时查询方言。
type Kuaidi100Dialect struct{}

func (Kuaidi100Dialect) ID() string { return "kuaidi100" }

// BuildRequest POST form：param={...}&customer=...&sign=MD5(param+key+customer) 大写。
// 参数映射（preset ParamMap）：carrier→com / tracking_no→num。
func (Kuaidi100Dialect) BuildRequest(ctx context.Context, cfg ConnectorConfig, ep EndpointConfig, args map[string]any) (*http.Request, error) {
	fullURL := strings.TrimRight(cfg.BaseURL, "/") + ep.Path

	// param JSON：按 key 排序保证签名稳定（MD5 对字节序敏感）
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	b.WriteString("{")
	for i, k := range keys {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "%q:%q", epParamName(ep, k), fmt.Sprintf("%v", args[k]))
	}
	b.WriteString("}")
	paramJSON := b.String()

	sign := md5Sum(paramJSON + cfg.Auth.Key + cfg.Auth.Header)
	form := url.Values{}
	form.Set("param", paramJSON)
	form.Set("customer", cfg.Auth.Header) // 快递100 语义：customer 编号（凭证侧）
	form.Set("sign", sign)

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, fullURL, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return req, nil
}

func md5Sum(s string) string {
	sum := md5.Sum([]byte(s))
	return strings.ToUpper(hex.EncodeToString(sum[:]))
}

// NormalizeResponse 快递100 信封归一化：{status, data:[{time,status,context...}]}
// → 统一 Envelope（items=轨迹节点，newest-first 语义由平台保证）。
func (Kuaidi100Dialect) NormalizeResponse(body []byte, statusCode int) (map[string]any, error) {
	var resp struct {
		Status  int              `json:"status"`
		Message string           `json:"message"`
		Data    []map[string]any `json:"data"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, err
	}
	if resp.Status != 200 {
		return nil, fmt.Errorf("kuaidi100 status=%d message=%s", resp.Status, resp.Message)
	}
	env := Envelope{OK: true, Items: resp.Data}
	return env.ToMap(), nil
}

// init 注册快递100 方言（preset 样板；真实凭证🔵）。
func init() {
	Register(Kuaidi100Dialect{})
}
