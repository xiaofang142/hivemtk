package service

import (
	"encoding/json"
	"fmt"
	"strings"

	"hivemtk-user/internal/ops/model"
)

var filterFieldWhitelist = map[string]map[string]string{
	"sessions": {
		"status":       "status",
		"agent_name":   "agent_name",
		"platform":     "platform",
		"handler_type": "handler_type",
	},
	"messages": {
		"content_type": "COALESCE(content_type, 'unknown')",
		"platform":     "platform",
		"status":       "status",
	},
	"clues": {
		"type":      "type",
		"is_verify": "is_verify",
		"level":     "level",
		"is_group":  "is_group",
	},
	"users": {
		"churn_risk": "churn_risk",
	},
}

var filterOperatorSQL = map[string]string{
	"eq":   "= ?",
	"ne":   "!= ?",
	"gt":   "> ?",
	"lt":   "< ?",
	"like": "ILIKE '%' || ? || '%'",
}

// BuildReportFilterSQL 将报表过滤器解析为安全 SQL 片段与绑定值
//
// dataSource 用于字段白名单校验；返回的 conds 可直接 WHERE 拼接。
// 非法字段/操作符直接跳过该条（宽容降级，不因脏配置炸整个报表）。
func BuildReportFilterSQL(dataSource string, filtersJSON string) ([]string, []any) {
	whitelist, ok := filterFieldWhitelist[dataSource]
	if !ok || strings.TrimSpace(filtersJSON) == "" {
		return nil, nil
	}
	var filters []model.ReportFilter
	if err := json.Unmarshal([]byte(filtersJSON), &filters); err != nil || len(filters) == 0 {
		return nil, nil
	}
	conds := make([]string, 0, len(filters))
	args := make([]any, 0, len(filters))
	for _, f := range filters {
		col, ok := whitelist[f.Field]
		if !ok || f.Field == "" {
			continue
		}
		opSQL, ok := filterOperatorSQL[f.Operator]
		if !ok {
			continue
		}
		if f.Value == nil || fmt.Sprintf("%v", f.Value) == "" {
			continue
		}
		conds = append(conds, col+" "+opSQL)
		args = append(args, normalizeFilterValue(f.Value))
	}
	return conds, args
}

func normalizeFilterValue(v any) any {
	switch t := v.(type) {
	case string:
		var i int64
		if !strings.ContainsAny(t, ".- ") && isAllDigits(t) {
			if _, err := fmt.Sscanf(t, "%d", &i); err == nil {
				return i
			}
		}
		return t
	default:
		return v
	}
}

func isAllDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return len(s) > 0
}

// applyConds 已随 AV-04c 批次7 刀2 下沉 ops 仓储（opsrepo.ApplyConds，谓词组装
// 逻辑逐字保留）；本文件自此只做过滤器 JSON→安全谓词片段的白名单解析，不再持有
// *gorm.DB。唯一调用方 custom_report.go 已同步改为仓储聚合方法。
