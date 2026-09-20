// opportunity_roster.go 在册销售名单的**生产实现**（T-P4-05 AC③ 的数据源）。
//
// 真源是 sales_events 里 event_type='sales_profile' 的事件，与业绩看板
// （SalesEventStatsService.allProfiles / listSalesIDs）用的是同一处 —— 这既是选择也是代价：
// 优点是不引入第二份"谁在岗"的事实（另一条候选 sales_personas 在本仓**零写入方**，
// 拿它当名单会让分配永远走到"在册销售为空"那一支）；代价是"在册"只能定义为
// "注册过档案"，本仓没有停用类事件，所以离职销售不会被自动移出名单。
// 这条边界在这里写明而不是先建一列 status：停用事件该由哪张卡引入还没定，
// 而现在加的那一列没有任何写者，只会被 AutoMigrate 建成恒为零值的死列。
//
// 与 allProfiles 的**唯一但关键**差别是错误处理：那边把读失败吞成 nil（看板少几行），
// 这边必须原样上抛。理由是分配器区分"确实没人"（(nil,nil) → 规则三，商机照常建但无归属）
// 与"不知道有没有人"（error → 整次转换失败），把这个区分吞掉就等于
// 让一次数据库抖动伪装成一条可信的业务结论，而且它会被写进审计的理由那一格里。
package service

import (
	"context"
	"errors"

	"hivemtk-user/internal/model"
	"hivemtk-user/internal/repository"
)

// ErrSalesRosterUnavailable 名单适配器未装配。
//
// 与 ErrOwnerAssignerUnavailable 分开：那边的修法是"给分配器补句柄"，
// 这边的修法是"给名单补仓库"，而装配漏项必须落进"故障"而不是"空名单"。
var ErrSalesRosterUnavailable = errors.New("opportunity roster: 销售事件仓库未装配")

// salesEventRoster 走事件流的在册销售名单。无状态、无缓存：
// 缓存它会让"新注册的销售"在缓存过期前一律分不到单，而那件事没人会怀疑到缓存头上。
type salesEventRoster struct {
	repo repository.SalesEventRepository
}

// NewSalesEventRoster 构造在册销售名单适配器。
func NewSalesEventRoster(repo repository.SalesEventRepository) SalesRoster {
	return &salesEventRoster{repo: repo}
}

// ActiveSalesIDs 返回全部注册过档案的销售标识（原样、不去重、不排序）。
//
// 洗与排序在 OwnerAssigner 侧（sanitizeSalesIDs），本层只做"取那一列"：
// 两处各做一次会分家，而"名单该不该有序"只该有一个答案。
// owner_id 为空的档案事件不在这儿剔除 —— 那是空白值，剔它属于同一处清洗逻辑。
func (r *salesEventRoster) ActiveSalesIDs(ctx context.Context) ([]string, error) {
	if r == nil || r.repo == nil {
		return nil, ErrSalesRosterUnavailable
	}
	events, err := r.repo.ListByType(ctx, model.SalesEventTypeSalesProfile, "", 0)
	if err != nil {
		return nil, err
	}
	if len(events) == 0 {
		// 返回 nil 而不是空切片：调用方判的是长度，而这里返回哪一种是"确实没人"的
		// 唯一表达方式，制造一个非 nil 的空壳只是让下一层多一种要区分的状态。
		return nil, nil
	}
	ids := make([]string, len(events))
	for i, e := range events {
		ids[i] = e.OwnerID
	}
	return ids, nil
}
