package httpget

import (
	"encoding/json"
	"io"

	"hivemtk-user/internal/pkg/httpclient"
)

// GetRequest 发起 HTTP GET 请求并将 JSON 响应体解析为 map
func GetRequest(url string) (map[string]any, error) {
	resp, err := httpclient.Client.Get(url)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	var result map[string]any
	if err := json.Unmarshal(body, &result); err != nil {
		return result, err
	}
	return result, nil
}
