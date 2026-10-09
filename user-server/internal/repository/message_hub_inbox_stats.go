package repository

import (
	"context"

	"time"

	"hivemtk-user/internal/model"

	"gorm.io/gorm"
)

type HubStatsResult struct {
	Total       int64
	Inbound     int64
	Outbound    int64
	Unread      int64
	ByPlatform  map[string]int64
	ByDirection map[string]int64
	ByMsgType   map[string]int64
	ByAccount   map[string]int64
	Recent24h   int64
}

// hubWindow 把调用方给的起止界加到任意一条统计查询上。
//
// 这道门必须套在**每一条**上，而不是只套总数：只套总数时，一次带窗口的请求返回的是
// 「total 属于这一小时、inbound/outbound/unread/各维度分布属于全量」这种混口径的一组数。
// 调用方看不出区别，只会发现各方向加起来对不上总数，以及管理台把全量积压当成"最近 1 小时"
// 报给客户。Recent24h 那一格例外——它的语义本身就写死 24h，不跟请求窗口走。
func hubWindow(q *gorm.DB, start, end *time.Time) *gorm.DB {
	if start != nil {
		q = q.Where("sent_at >= ?", *start)
	}
	if end != nil {
		q = q.Where("sent_at <= ?", *end)
	}
	return q
}

func (r *MessageHubRepository) GetHubStats(ctx context.Context, start, end *time.Time) (*HubStatsResult, error) {
	if r == nil || r.db == nil {
		return &HubStatsResult{
			ByPlatform: map[string]int64{}, ByDirection: map[string]int64{},
			ByMsgType: map[string]int64{}, ByAccount: map[string]int64{},
		}, nil
	}

	var total, inbound, outbound, unread int64

	if err := hubWindow(r.db.WithContext(ctx).Model(&model.MessageHub{}), start, end).Count(&total).Error; err != nil {
		return nil, err
	}
	hubWindow(r.db.WithContext(ctx).Model(&model.MessageHub{}).
		Where("direction = ?", "inbound"), start, end).Count(&inbound)
	hubWindow(r.db.WithContext(ctx).Model(&model.MessageHub{}).
		Where("direction = ?", "outbound"), start, end).Count(&outbound)
	// 未读只算入站消息：出站消息由本系统发出，不存在"待本系统阅读"的语义，
	// 计入会让「未读」大于「接收」，出现逻辑矛盾的读数（实测 297 > 257）。
	hubWindow(r.db.WithContext(ctx).Model(&model.MessageHub{}).
		Where("direction = ?", "inbound").
		Where("(is_read = ? OR is_read IS NULL)", false), start, end).Count(&unread)

	stats := &HubStatsResult{
		Total: total, Inbound: inbound, Outbound: outbound, Unread: unread,
		ByPlatform: map[string]int64{}, ByDirection: map[string]int64{},
		ByMsgType: map[string]int64{}, ByAccount: map[string]int64{},
	}

	type pcount struct {
		Platform string
		C        int64
	}
	var pCounts []pcount
	hubWindow(r.db.WithContext(ctx).Model(&model.MessageHub{}), start, end).
		Select("platform AS platform, COUNT(*) AS c").
		Group("platform").Scan(&pCounts)
	for _, p := range pCounts {
		stats.ByPlatform[p.Platform] = p.C
	}

	type dcount struct {
		Direction string
		C         int64
	}
	var dCounts []dcount
	hubWindow(r.db.WithContext(ctx).Model(&model.MessageHub{}), start, end).
		Select("direction AS direction, COUNT(*) AS c").
		Group("direction").Scan(&dCounts)
	for _, d := range dCounts {
		stats.ByDirection[d.Direction] = d.C
	}

	type tcount struct {
		MsgType string
		C       int64
	}
	var tCounts []tcount
	hubWindow(r.db.WithContext(ctx).Model(&model.MessageHub{}), start, end).
		Select("msg_type AS msg_type, COUNT(*) AS c").
		Group("msg_type").Scan(&tCounts)
	for _, t := range tCounts {
		stats.ByMsgType[t.MsgType] = t.C
	}

	type acount struct {
		AccountID string
		C         int64
	}
	var aCounts []acount
	hubWindow(r.db.WithContext(ctx).Model(&model.MessageHub{}), start, end).
		Select("account_id AS account_id, COUNT(*) AS c").
		Group("account_id").Order("c DESC").Limit(50).Scan(&aCounts)
	for _, a := range aCounts {
		stats.ByAccount[a.AccountID] = a.C
	}

	threshold24h := time.Now().Add(-24 * time.Hour)
	r.db.WithContext(ctx).Model(&model.MessageHub{}).
		Where("sent_at >= ?", threshold24h).
		Count(&stats.Recent24h)

	return stats, nil
}
