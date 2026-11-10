package service

import (
	"context"
	"encoding/json"
	"errors"

	"hivemtk-user/internal/aiagent/agent/portcontract"
	"hivemtk-user/internal/connector"
)

// CourierClient 标准快递轨迹 HTTP 客户端，实现 portcontract.CourierClient。
//
// §3.12 刀2 收编：轨迹查询改走 connector generic 方言（端点=preset 数据
// courierTrackEndpoint，鉴权/参数映射/归一化全部复用连接器既有实现），
// 行为等价重构——切换前经 courier_client_parity_test.go 新旧双跑对拍一致。
//
// 对拍冻结口径（C2-3）：
//   - (nil,nil) 短路语义不变（未配置/空运单号）；
//   - 成功路径：请求形态（method/path/query/鉴权与静态头）一致 + 轨迹输出逐节点逐字段一致；
//   - 失败面：双方 err!=nil 且 tracks=nil 一致；错误文本允许携带更完整信息
//     （旧 "courier api status %d" → 新 "connector generic status %d: body"，调用方
//     logistics_port_adapter 只判 err==nil，错误文本差异不影响行为）；
//   - 网络口径等价：Executor 用 NewExecutorUnrestricted（旧实现无基址校验，内网
//     快递镜像是合法部署形态；本客户端 base_url 归管理员配置、LLM 不可控，与
//     LLM 可控出站面的严格 SSRF 口径分治——那道闸在桥接工具侧，见刀1/刀3）；
//     客户端超时 8s→10s（executor 统一口径，对拍契约不含超时秒数，唯一记录在案
//     的偏差）。
type CourierClient struct {
	exec    *connector.Executor
	baseURL string
	key     string
	secret  string
}

// courierTrackEndpoint 快递轨迹端点 preset 数据（generic 方言）：
// GET /api/track?carrier=...&no=...，能力标签 query_express（供桥接/审计语义）。
// X-Api-Secret 归 preset 静态头（EndpointConfig.Headers，刀2 补的通用面）。
func courierTrackEndpoint(secret string) connector.EndpointConfig {
	ep := connector.EndpointConfig{
		Name:         "track",
		Method:       "GET",
		Path:         "/api/track",
		Capabilities: []string{"query_express"},
		ParamMap:     map[string]string{"carrier": "carrier", "tracking_no": "no"},
	}
	if secret != "" {
		ep.Headers = map[string]string{"X-Api-Secret": secret}
	}
	return ep
}

// NewCourierClientFromConfig 按数据库中的物流集成配置构造。baseURL 为空表示未配置实时物流接口。
func NewCourierClientFromConfig(cfg LogisticsIntegration) *CourierClient {
	// 行为等价（C2-3）：管理面配置的 base_url 保留既有网络口径（含内网快递镜像），
	// 不叠加 LLM 可控出站面的严格 SSRF 闸（那道闸在桥接工具侧）。
	return newCourierClientWithExecutor(connector.NewExecutorUnrestricted(), cfg)
}

// newCourierClientWithExecutor 测试接缝（同包）：注入 Executor——对拍测试经
// NewExecutorWithValidator 放行环回 httptest 上游；生产路径恒 NewExecutor 严格 SSRF。
func newCourierClientWithExecutor(exec *connector.Executor, cfg LogisticsIntegration) *CourierClient {
	return &CourierClient{exec: exec, baseURL: cfg.BaseURL, key: cfg.Key, secret: cfg.Secret}
}

// Configured 实时快递接口是否已配置
func (c *CourierClient) Configured() bool { return c.baseURL != "" }

// Query 按 快递公司 + 运单号 查询实时轨迹。未配置或运单号为空时返回 (nil, nil)。
func (c *CourierClient) Query(ctx context.Context, carrier, trackingNo string) ([]*portcontract.LogisticsTrackView, error) {
	if !c.Configured() || trackingNo == "" {
		return nil, nil
	}
	// 旧实现 key 为空时不带 Authorization 头：条件选鉴权形态保持请求形态等价。
	auth := connector.ConnectorAuth{}
	if c.key != "" {
		auth = connector.ConnectorAuth{Type: "bearer", Key: c.key}
	}
	out, err := c.exec.Call(ctx, connector.ConnectorConfig{
		BaseURL:  c.baseURL,
		Provider: "generic",
		Auth:     auth,
	}, courierTrackEndpoint(c.secret), map[string]any{
		"carrier":     carrier,
		"tracking_no": trackingNo,
	})
	if err != nil {
		return nil, err
	}
	return decodeCourierTracks(out)
}

// decodeCourierTracks 统一信封 → 轨迹视图。形态校验沿旧实现严格解码语义：
// 数组/非 JSON 信封（无 data 键）→ 错误；data=null（body 为 null）→ 空轨迹成功
// （旧 Decode null 为 no-op 同语义）；对象缺 tracks 字段 → 空轨迹成功；tracks 字段
// 类型不符 → 解码错误。
func decodeCourierTracks(out map[string]any) ([]*portcontract.LogisticsTrackView, error) {
	dataAny, present := out["data"]
	if !present {
		return nil, errors.New("courier api 响应形态不符：统一信封无 data 对象")
	}
	if dataAny == nil {
		return []*portcontract.LogisticsTrackView{}, nil
	}
	data, ok := dataAny.(map[string]any)
	if !ok {
		return nil, errors.New("courier api 响应形态不符：信封 data 非对象")
	}
	buf, err := json.Marshal(data)
	if err != nil {
		return nil, err
	}
	var decoded struct {
		Tracks []struct {
			Time        string `json:"time"`
			Status      string `json:"status"`
			Location    string `json:"location"`
			Description string `json:"description"`
		} `json:"tracks"`
	}
	if err := json.Unmarshal(buf, &decoded); err != nil {
		return nil, err
	}
	tracks := make([]*portcontract.LogisticsTrackView, 0, len(decoded.Tracks))
	for _, t := range decoded.Tracks {
		tracks = append(tracks, &portcontract.LogisticsTrackView{
			Time:        t.Time,
			Status:      t.Status,
			Location:    t.Location,
			Description: t.Description,
		})
	}
	return tracks, nil
}
