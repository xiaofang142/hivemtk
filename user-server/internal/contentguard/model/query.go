package model

// WordQuery 词条列表查询参数，由业务层与仓储共享。
type WordQuery struct {
	ListKind string
	Category string
	Keyword  string
	Enabled  *bool
	Page     int
	PageSize int
}

// RecordQuery 审查记录列表查询参数。
type RecordQuery struct {
	SubjectType  string
	SubjectID    string
	Scene        string
	Verdict      string
	ManualStatus string
	TraceID      string
	Page         int
	PageSize     int
}

// SanctionQuery 处置列表查询参数。
type SanctionQuery struct {
	SubjectType string
	SubjectID   string
	Kind        string
	Level       string
	ActiveOnly  bool
	Page        int
	PageSize    int
}
