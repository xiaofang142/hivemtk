// Package kbconnector 第三方知识库连接器：类型注册制，把外部知识库文档
// 拉取落库到本地 RAG 管线（下载 → KnowledgeBaseService.IngestLocalFile）。
package kbconnector

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
)

// ErrInvalidInput 连接器配置或上游清单本身不合法（缺字段、URL 指向网页、
// 清单不是 JSON 等）。这类错误是**调用方/配置问题**，控制器应回 4xx，
// 而不是 500「服务器内部错误」——否则运维只会看到「服务炸了」，不知道
// 是连接器 URL 配错。
var ErrInvalidInput = errors.New("连接器配置或上游清单不合法")

// invalidInput 包一层哨兵错误，保留可读原因供前端直接展示。
func invalidInput(format string, a ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalidInput, fmt.Sprintf(format, a...))
}

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
