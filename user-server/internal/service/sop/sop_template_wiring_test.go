// sop_template_wiring_test.go SOP 模板服务装配回归（AV-04c 批次6 刀2）。
//
// NewSOPTemplateService 收口为 repository.DBReady() 探针 + Default 构造后，
// 「有库即接线、无库两仓储都留 nil」的双向语义不变；读路径的空值防御契约不变。
package sop

import (
	"context"
	"testing"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/pkg/testutil"
	"hivemtk-user/internal/repository"
)

// 无库侧：两仓储留 nil，读路径回空不 panic（与原 `db := repository.GetDB(); if db != nil` 等价）。
func TestNewSOPTemplateService_NilDB_LeavesReposNil(t *testing.T) {
	if repository.DBReady() {
		t.Skip("全局句柄被在途测试占用，无库语义由其余 NilDB 用例覆盖")
	}
	svc := NewSOPTemplateService()
	if svc.repo != nil || svc.bindingRepo != nil {
		t.Fatalf("无库时两仓储应留 nil, got repo=%v binding=%v", svc.repo != nil, svc.bindingRepo != nil)
	}
	tpls, err := svc.MatchByIntent(context.Background(), "议价")
	if err != nil || tpls != nil {
		t.Fatalf("无库 MatchByIntent 应回空: %v %v", tpls, err)
	}
}

// 有库侧：Default 构造把两仓储绑到测试库；匹配查询在真实表上执行。
func TestNewSOPTemplateService_WithDB_WiresDefaultRepos(t *testing.T) {
	db := testutil.NewTestDB(t, &model.SOPTemplate{}, &model.AgentKBBinding{})
	enabledTrue := true
	if err := db.Create(&model.SOPTemplate{Intent: "议价", Stage: "中期", Template: "让利话术", Enabled: &enabledTrue}).Error; err != nil {
		t.Fatalf("seed sop template: %v", err)
	}
	svc := NewSOPTemplateService()
	if svc.repo == nil || svc.bindingRepo == nil {
		t.Fatalf("有库时两仓储都应装配, got repo=%v binding=%v", svc.repo == nil, svc.bindingRepo == nil)
	}
	tpls, err := svc.MatchByIntent(context.Background(), "议价")
	if err != nil {
		t.Fatalf("MatchByIntent: %v", err)
	}
	if len(tpls) != 1 || tpls[0].Intent != "议价" {
		t.Fatalf("应匹配到测试库模板: %+v", tpls)
	}
}
