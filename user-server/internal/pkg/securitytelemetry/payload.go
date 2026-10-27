// payload.go 未授权访问聚合载荷的**行为**（model 只留数据形状）。
//
// 这些逻辑原先挂在 model.SecurityAccessFailurePayload 上，被架构门禁判为
// 「Model 层业务方法」（GO_FIVE_LAYER_ARCHITECTURE §Model：只放字段、GORM Hook
// 与 Value/Scan）。生产读写方是 middleware/security_recorder.go 与
// service/security_overview.go，两处不在同一层，所以收在这里而不是任一使用方。
package securitytelemetry

import (
	"sort"

	"hivemtk-user/internal/model"
)

// Add 计入一次失败（date=YYYY-MM-DD，pathPrefix 已截好的两段前缀）。
func Add(p *model.SecurityAccessFailurePayload, date, kind, pathPrefix string) {
	if p == nil {
		return
	}
	if p.Days == nil {
		p.Days = map[string]*model.SecurityAccessFailureDay{}
	}
	day := p.Days[date]
	if day == nil {
		day = &model.SecurityAccessFailureDay{Kinds: map[string]int64{}, Paths: map[string]int64{}}
		p.Days[date] = day
	}
	if day.Kinds == nil {
		day.Kinds = map[string]int64{}
	}
	if day.Paths == nil {
		day.Paths = map[string]int64{}
	}
	if kind != "" {
		day.Kinds[kind]++
	}
	if pathPrefix != "" {
		day.Paths[pathPrefix]++
	}
}

// Merge 把另一份载荷（通常是本进程待刷增量）并进当前载荷。
func Merge(p, other *model.SecurityAccessFailurePayload) {
	if p == nil || other == nil {
		return
	}
	for date, day := range other.Days {
		if day == nil {
			continue
		}
		for kind, n := range day.Kinds {
			for i := int64(0); i < n; i++ {
				Add(p, date, kind, "")
			}
		}
		for path, n := range day.Paths {
			for i := int64(0); i < n; i++ {
				Add(p, date, "", path)
			}
		}
	}
}

// Prune 只保留最近 keepDays 天（按日期字符串倒序保留；日期串即字典序）。
func Prune(p *model.SecurityAccessFailurePayload, keepDays int) {
	if p == nil || keepDays <= 0 || len(p.Days) <= keepDays {
		return
	}
	dates := make([]string, 0, len(p.Days))
	for d := range p.Days {
		dates = append(dates, d)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(dates)))
	for _, d := range dates[keepDays:] {
		delete(p.Days, d)
	}
}

// TopPaths 返回按次数降序的前 n 个路径前缀。
func TopPaths(p *model.SecurityAccessFailurePayload, n int) []string {
	if p == nil || n <= 0 {
		return nil
	}
	type kv struct {
		k string
		v int64
	}
	all := map[string]int64{}
	for _, day := range p.Days {
		if day == nil {
			continue
		}
		for path, cnt := range day.Paths {
			all[path] += cnt
		}
	}
	list := make([]kv, 0, len(all))
	for k, v := range all {
		list = append(list, kv{k, v})
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].v != list[j].v {
			return list[i].v > list[j].v
		}
		return list[i].k < list[j].k
	})
	out := make([]string, 0, n)
	for i, item := range list {
		if i >= n {
			break
		}
		out = append(out, item.k)
	}
	return out
}
