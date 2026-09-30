package service

import (
	"bytes"
	"encoding/csv"
	"strconv"
)

// BuildDomainsCSV 引用域名聚合 CSV（供 Excel / BI 透视）
func BuildDomainsCSV(res *CitationDomainResult) string {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if res == nil {
		w.Flush()
		return buf.String()
	}
	_ = w.Write([]string{"domain", "category", "count", "query_count", "engine_count"})
	for _, d := range res.Domains {
		_ = w.Write([]string{
			d.Domain, d.Category,
			strconv.Itoa(d.Count), strconv.Itoa(d.QueryCount), strconv.Itoa(d.EngineCount),
		})
	}
	w.Flush()
	return buf.String()
}

// BuildMatrixCSV keyword×domain 矩阵 CSV（宽表：query,total,<domain...>）
func BuildMatrixCSV(res *CitationMatrixResult) string {
	var buf bytes.Buffer
	w := csv.NewWriter(&buf)
	if res == nil {
		_ = w.Write([]string{"query", "total"})
		w.Flush()
		return buf.String()
	}
	header := append([]string{"query", "total"}, res.Domains...)
	_ = w.Write(header)
	for _, r := range res.Rows {
		row := []string{r.Query, strconv.Itoa(r.Total)}
		for _, d := range res.Domains {
			row = append(row, strconv.Itoa(r.Domains[d]))
		}
		_ = w.Write(row)
	}
	w.Flush()
	return buf.String()
}
