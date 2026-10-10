// Package capregistry 平台能力注册表（借鉴 catbus registry/vocab 模式的试点）：
// 「渠道 × 能力」单一事实源，从它生成能力矩阵文档，文档漂移被测试钉死。
package capregistry

import (
	"fmt"
	"sort"
	"strings"
)

// Capability 渠道上的一种能力。
type Capability string

const (
	CapDMReceive Capability = "dm.receive" // 收私信（入站）
	CapDMSend    Capability = "dm.send"    // 发私信（出站）
	CapWebhook   Capability = "webhook"    // 官方回调接入
	CapLeadMine  Capability = "lead.mine"  // 线索挖掘
	CapKBSync    Capability = "kb.sync"    // 知识库外部同步
	CapSMS       Capability = "sms"        // 短信触达
	CapEmail     Capability = "email"      // 邮件触达
)

// Entry 一条注册：平台 + 能力 + 接入方式说明。
type Entry struct {
	Platform string
	Cap      Capability
	Via      string // 接入方式（bridge / webhook / smtp / imasdk…）
	Note     string
}

var registry []Entry

// Register 登记（启动/包初始化时调用；重复登记 panic——注册表是契约）。
func Register(e Entry) {
	for _, old := range registry {
		if old.Platform == e.Platform && old.Cap == e.Cap {
			panic(fmt.Sprintf("capregistry: %s/%s 重复登记", e.Platform, e.Cap))
		}
	}
	registry = append(registry, e)
}

// All 返回全部登记（平台排序稳定）。
func All() []Entry {
	out := append([]Entry(nil), registry...)
	sort.Slice(out, func(i, j int) bool {
		if out[i].Platform != out[j].Platform {
			return out[i].Platform < out[j].Platform
		}
		return out[i].Cap < out[j].Cap
	})
	return out
}

// MatrixMarkdown 生成能力矩阵文档（文档 = 注册表的渲染，禁止手改）。
func MatrixMarkdown() string {
	var b strings.Builder
	b.WriteString("# 渠道能力矩阵\n\n> 本文件由 `internal/capregistry` 生成（capregistry.MatrixMarkdown），")
	b.WriteString("测试钉死与注册表一致，**请勿手改**。改能力先改注册表。\n\n")
	b.WriteString("| 平台 | 能力 | 接入方式 | 说明 |\n|---|---|---|---|\n")
	for _, e := range All() {
		note := strings.ReplaceAll(e.Note, "|", "\\|")
		fmt.Fprintf(&b, "| %s | `%s` | %s | %s |\n", e.Platform, e.Cap, e.Via, note)
	}
	return b.String()
}
