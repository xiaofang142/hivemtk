package dto

type CreateCronReq struct {
	TaskID   uint   `json:"task_id" binding:"required"`
	CronExpr string `json:"cron_expr" binding:"required,max=128"`
	TimeZone string `json:"time_zone" binding:"omitempty,max=64"` // G20：IANA 名（Asia/Shanghai），空=服务器本地
	Enabled  *bool  `json:"enabled"`
}

type UpdateCronReq struct {
	CronExpr string `json:"cron_expr" binding:"omitempty,max=128"`
	TimeZone string `json:"time_zone" binding:"omitempty,max=64"`
}
