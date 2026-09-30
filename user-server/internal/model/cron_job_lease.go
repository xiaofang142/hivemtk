package model

import "time"

// CronJobLease 后台定时任务的跨进程租约（一个任务一行）。
//
// 需要它的原因：后台协程是在进程内起的，而同一套服务可能同时被多个进程跑（本地热重载
// 实例、手工起的临时实例、线上多副本）。没有互斥时每个进程各扫各的，外发动作按进程数
// 倍增——门控清扫曾三进程并存，对同一名未验证成员一天重播入群提示 60+ 次。
// 持有者以 hostname:pid 标识；心跳陈旧即视为持有者已退出，可被其他进程接管。
type CronJobLease struct {
	JobName     string     `gorm:"type:varchar(64);primaryKey" json:"job_name"`
	Owner       string     `gorm:"type:varchar(100);not null;index" json:"owner"`
	HeartbeatAt *time.Time `json:"heartbeat_at"`
}

func (CronJobLease) TableName() string { return "cron_job_leases" }
