package service

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"hivemtk-user/internal/model"
)

type fakeCustomerImportJobRepo struct {
	job *model.CustomerImportJob
}

func (f *fakeCustomerImportJobRepo) Create(_ context.Context, j *model.CustomerImportJob) error {
	f.job = j
	return nil
}
func (f *fakeCustomerImportJobRepo) GetByJobID(_ context.Context, _ string) (*model.CustomerImportJob, error) {
	if f.job == nil {
		return nil, context.DeadlineExceeded
	}
	return f.job, nil
}
func (f *fakeCustomerImportJobRepo) List(_ context.Context, _, _ int) ([]model.CustomerImportJob, int64, error) {
	if f.job == nil {
		return nil, 0, nil
	}
	return []model.CustomerImportJob{*f.job}, 1, nil
}
func (f *fakeCustomerImportJobRepo) UpdateStatus(_ context.Context, _, status string, total, success, failed, skipped, updated int, errReport string) error {
	if f.job != nil {
		f.job.Status = status
		f.job.TotalRows = total
		f.job.SuccessRows = success
		f.job.FailedRows = failed
		f.job.SkippedRows = skipped
		f.job.UpdatedRows = updated
		f.job.ErrorReport = errReport
	}
	return nil
}
func (f *fakeCustomerImportJobRepo) Update(_ context.Context, j *model.CustomerImportJob) error {
	f.job = j
	return nil
}

func TestNormalizeDedupStrategy(t *testing.T) {
	cases := map[string]string{
		"": "skip", "skip": "skip", "update": "update", "error": "error", "SKIP": "skip", "unknown": "skip",
	}
	for in, want := range cases {
		if got := normalizeDedupStrategy(in); got != want {
			t.Fatalf("normalizeDedupStrategy(%q)=%q want=%q", in, got, want)
		}
	}
}

func TestParseMapping(t *testing.T) {
	m, err := parseMapping(`{"客户名":"name","手机号":"phone"}`)
	if err != nil {
		t.Fatalf("parseMapping: %v", err)
	}
	if m["客户名"] != "name" || m["手机号"] != "phone" {
		t.Fatalf("unexpected mapping: %#v", m)
	}
	if _, err := parseMapping(`{bad`); err == nil {
		t.Fatalf("expected error for bad json")
	}
}

func TestValidateCustomerRow(t *testing.T) {
	if errs := validateCustomerRow(map[string]string{"phone": "13800138000"}); len(errs) != 0 {
		t.Fatalf("expected no errs, got %v", errs)
	}
	errs := validateCustomerRow(map[string]string{})
	if len(errs) == 0 {
		t.Fatalf("expected missing identity error")
	}
	errs = validateCustomerRow(map[string]string{"email": "bad-email"})
	if len(errs) == 0 {
		t.Fatalf("expected email format error")
	}
}

func TestCustomerDedupKey(t *testing.T) {
	if got := customerDedupKey(map[string]string{"phone": "138"}); got != "phone:138" {
		t.Fatalf("got %q", got)
	}
	if got := customerDedupKey(map[string]string{"email": "A@B.COM"}); got != "email:a@b.com" {
		t.Fatalf("got %q", got)
	}
	if got := customerDedupKey(map[string]string{}); got != "" {
		t.Fatalf("got %q", got)
	}
}

func TestReadCSVAndMapRow(t *testing.T) {
	csvData := "\xEF\xBB\xBFname,phone,email\n张三,13800138000,zs@example.com\n"
	rows, err := readCSV(strings.NewReader(csvData))
	if err != nil {
		t.Fatalf("readCSV: %v", err)
	}
	if len(rows) != 2 {
		t.Fatalf("rows=%d", len(rows))
	}
	headers := normalizeHeaders(rows[0])
	idx := map[string]int{}
	for i, h := range headers {
		idx[h] = i
	}
	rec := mapRow(headers, idx, map[string]string{}, rows[1])
	if rec["name"] != "张三" || rec["phone"] != "13800138000" {
		t.Fatalf("rec=%#v", rec)
	}
}

func TestTemplateCSV(t *testing.T) {
	svc := &CustomerImportJobService{}
	b, name, err := svc.TemplateCSV("customer")
	if err != nil {
		t.Fatalf("TemplateCSV: %v", err)
	}
	if name != "customer_import_template.csv" {
		t.Fatalf("name=%q", name)
	}
	if !strings.Contains(string(b), "phone") || !strings.Contains(string(b), "name") {
		t.Fatalf("csv missing headers: %s", string(b))
	}
}

func TestErrorReportCSV(t *testing.T) {
	repo := &fakeCustomerImportJobRepo{job: &model.CustomerImportJob{
		JobID:       "imp_test",
		ErrorReport: `[{"row":2,"field":"email","message":"邮箱格式不合法"}]`,
	}}
	svc := &CustomerImportJobService{repo: repo}
	b, name, err := svc.ErrorReportCSV(context.Background(), "imp_test")
	if err != nil {
		t.Fatalf("ErrorReportCSV: %v", err)
	}
	if !strings.Contains(name, "imp_test") {
		t.Fatalf("name=%q", name)
	}
	text := string(b)
	if !strings.Contains(text, "row,field,message") || !strings.Contains(text, "邮箱格式不合法") {
		t.Fatalf("csv=%s", text)
	}
	var errs []ImportPrecheckRowError
	if err := json.Unmarshal([]byte(repo.job.ErrorReport), &errs); err != nil || len(errs) != 1 {
		t.Fatalf("errs=%v err=%v", errs, err)
	}
}
