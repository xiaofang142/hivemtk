// Package kbconnector 第三方知识库连接器：类型注册制，把外部知识库文档
// 拉取落库到本地 RAG 管线（下载 → KnowledgeBaseService.IngestLocalFile）。
package kbconnector

import (
	"context"
	"fmt"
	"sort"
	"sync"
)

// ExternalDoc 连接器产出的一个可导入文档。
type ExternalDoc struct {
	// DocKey 第三方侧稳定标识（media_id / URL 等），用于同步幂等。
	DocKey string
	Title  string
	// Fetch 拉取文档字节（实现方可懒加载：只有新文档才会被调用）。
	Fetch func(ctx context.Context) ([]byte, error)
}

// Provider 连接器类型实现。
type Provider interface {
	// List 列出可同步的文档清单（不含内容）。
	List(ctx context.Context, cfg map[string]any) ([]ExternalDoc, error)
}

var (
	providerMu sync.RWMutex
	providers  = map[string]Provider{}
)

// RegisterProvider 注册连接器类型（重名 panic——类型名是配置面契约）。
func RegisterProvider(typ string, p Provider) {
	providerMu.Lock()
	defer providerMu.Unlock()
	if _, dup := providers[typ]; dup {
		panic(fmt.Sprintf("kbconnector: provider %q 已注册", typ))
	}
	providers[typ] = p
}

// LookupProvider 按类型取实现。
func LookupProvider(typ string) (Provider, error) {
	providerMu.RLock()
	defer providerMu.RUnlock()
	p, ok := providers[typ]
	if !ok {
		return nil, fmt.Errorf("不支持的连接器类型 %q（可用: %v）", typ, Types())
	}
	return p, nil
}

// Types 返回已注册类型（稳定排序，供校验文案/前端下拉）。
func Types() []string {
	providerMu.RLock()
	defer providerMu.RUnlock()
	out := make([]string, 0, len(providers))
	for k := range providers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// cfgString 从 provider 配置取字符串字段。
func cfgString(cfg map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := cfg[k].(string); ok && v != "" {
			return v
		}
	}
	return ""
}
