package service

import "strings"

// CRMBusinessDictionary 业务字典项（CRM-19）。
type CRMBusinessDictionary []CRMDictionaryItem

type CRMDictionaryItem struct {
	Code    string `json:"code"`
	Enabled bool   `json:"enabled"`
}

func (d CRMBusinessDictionary) AllowsNew(code string) bool {
	for _, it := range d {
		if it.Code == code {
			return it.Enabled
		}
	}
	return false
}

func (d CRMBusinessDictionary) HasHistorical(code string) bool {
	for _, it := range d {
		if it.Code == code {
			return true
		}
	}
	return false
}

// CRMSearchPage 全量与当前页分开（CRM-20/21）。
type CRMSearchPage struct {
	Page     int
	PageSize int
	Total    int64
	Items    int
}

func (p CRMSearchPage) TotalAll() int64  { return p.Total }
func (p CRMSearchPage) TotalOnPage() int { return p.Items }

// CRMSourceRef 带账号作用域的来源引用（CRM-20）。
type CRMSourceRef struct {
	AccountID string
	MessageID string
}

func (r CRMSourceRef) Valid() bool { return r.AccountID != "" && r.MessageID != "" }
func (r CRMSourceRef) ScopeKey() string {
	return strings.TrimSpace(r.AccountID) + ":" + strings.TrimSpace(r.MessageID)
}

// CRMAssistantToolGuard 助手工具默认只读（CRM-22/23）。
type CRMAssistantToolGuard struct {
	Name     string
	ReadOnly bool
}

func (g CRMAssistantToolGuard) Allowed() bool { return g.ReadOnly }

// CRMActionDraft 行动草稿须审批后执行（CRM-23）。
type CRMActionDraft struct {
	Approved bool
}

func (d CRMActionDraft) Executable() bool { return d.Approved }
