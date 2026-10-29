package service

// reach_customer_zone.go —— J-12 裁决（2026-10-30，选项③）：客户时区数据接入。
//
// 分层口径（裁决原文）：
//   - **静默窗口与次日释放**按收件客户时区计算（本文件的解析链）；
//   - **日配额键**（平台资源口径）与**合规策略**（监管口径）维持 cstZone 东八区，
//     避免把改动放大到配额与合规语义。
//
// 解析优先级：调用方 metadata["timezone"]（单次显式覆盖）>
// 客户档案 custom_attributes["timezone"]（持久档案，写入源=web 访客/bridge 腿
// 的 Intl.DateTimeFormat().resolvedOptions().timeZone 自动值）> cstZone 回退。
// 空/坏值一律回退并保持行为与升级前一致（默认部署无此数据 = 零行为变更）。

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// CustomerTimeZoneLookup 按 customerID 取客户档案的 **custom_attributes 原始
// jsonb 串**（timezone 键由本域统一解析——装配点不重复实现解析规则）。
// 返回 ok=false = 查无此人/查库失败，走回退。nil = 不查档案（此时只剩
// metadata 与回退）。
type CustomerTimeZoneLookup func(ctx context.Context, customerID string) (string, bool)

// CustomerZoneFromAttributes 从客户档案 custom_attributes（jsonb 字符串）解析
// timezone 键并校验 IANA 形状。空档案/缺键/坏值 → nil（调用方回退 cstZone）。
func CustomerZoneFromAttributes(customAttrs string) *time.Location {
	trimmed := strings.TrimSpace(customAttrs)
	if trimmed == "" {
		return nil
	}
	var attrs map[string]any
	if err := json.Unmarshal([]byte(trimmed), &attrs); err != nil {
		return nil
	}
	raw, ok := attrs["timezone"].(string)
	if !ok {
		return nil
	}
	return zoneFromIANA(raw)
}

// zoneFromIANA 校验并加载 IANA 时区名；非法/空 → nil。
func zoneFromIANA(name string) *time.Location {
	name = strings.TrimSpace(name)
	if name == "" || len(name) > 64 {
		return nil
	}
	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil
	}
	return loc
}

// resolveCustomerZone 静默窗口判定的时区解析链（metadata > 档案 > cstZone）。
// lookup 为 nil 或查询失败时不致命——回退保证行为不劣于升级前。
func resolveCustomerZone(ctx context.Context, cfg *SendPipelineConfig, req *ReachSendRequest) *time.Location {
	if req != nil {
		if z := zoneFromIANA(req.Metadata["timezone"]); z != nil {
			return z
		}
	}
	if cfg != nil && cfg.CustomerTimeZoneLookup != nil && req != nil && req.CustomerID != "" {
		if attrs, ok := cfg.CustomerTimeZoneLookup(ctx, req.CustomerID); ok {
			if z := CustomerZoneFromAttributes(attrs); z != nil {
				return z
			}
		}
	}
	return cstZone
}
